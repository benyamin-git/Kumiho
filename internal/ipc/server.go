package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// MaxFrameSize bounds one request envelope on the control socket. A real
// command is a few KiB; the ceiling stops a local client from making the
// daemon allocate without limit.
const MaxFrameSize = 8 << 20

// defaultWriteTimeout bounds one reply or event. A client that stops reading
// (a suspended TUI, a wedged GUI) is closed instead of pinning the daemon's
// broadcast goroutine forever.
const defaultWriteTimeout = 5 * time.Second

// Server accepts control connections from a transport listener: a Unix
// socket on Linux, a named pipe on Windows.
type Server struct {
	path      string
	ln        net.Listener
	peerUID   func(net.Conn) (uint32, bool)
	peerAdmin func(net.Conn) bool
}

// Serve accepts connections until ctx is done or the listener closes. Each
// connection is handled in its own goroutine.
func (s *Server) Serve(ctx context.Context, handler func(context.Context, *Conn) error) error {
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
		case <-done:
		}
		s.ln.Close()
	}()

	for {
		nc, err := s.ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go func(nc net.Conn) {
			conn := newConn(nc, s.peerUID, s.peerAdmin)
			defer conn.Close()
			_ = handler(ctx, conn)
		}(nc)
	}
}

// Close stops the listener and removes the socket file it created, if any
// (best effort; a named pipe disappears with its listener).
func (s *Server) Close() error {
	err := s.ln.Close()
	_ = os.Remove(s.path)
	return err
}

// Path returns the control endpoint path (a Unix socket path on Linux, a
// named-pipe path on Windows).
func (s *Server) Path() string {
	return s.path
}

// Conn is one server-side client connection.
type Conn struct {
	nc           net.Conn
	enc          *json.Encoder
	br           *bufio.Reader
	writeTimeout time.Duration
	peerUID      func(net.Conn) (uint32, bool)
	peerAdmin    func(net.Conn) bool

	mu        sync.Mutex
	done      chan struct{}
	closeOnce sync.Once
}

func newConn(nc net.Conn, peerUID func(net.Conn) (uint32, bool), peerAdmin func(net.Conn) bool) *Conn {
	return &Conn{
		nc:           nc,
		enc:          json.NewEncoder(nc),
		br:           bufio.NewReaderSize(nc, 64<<10),
		writeTimeout: defaultWriteTimeout,
		peerUID:      peerUID,
		peerAdmin:    peerAdmin,
		done:         make(chan struct{}),
	}
}

// Read waits for the next request. The protocol is newline-delimited JSON;
// one frame larger than MaxFrameSize is a protocol error, not an allocation.
func (c *Conn) Read() (*Envelope, error) {
	line, err := readFrame(c.br, MaxFrameSize)
	if err != nil {
		return nil, err
	}
	var env Envelope
	if err := json.Unmarshal(line, &env); err != nil {
		return nil, err
	}
	return &env, nil
}

// readFrame reads one newline-terminated frame, refusing to grow past max.
func readFrame(br *bufio.Reader, max int) ([]byte, error) {
	var frame []byte
	for {
		frag, err := br.ReadSlice('\n')
		if len(frame)+len(frag) > max {
			return nil, fmt.Errorf("ipc: frame exceeds %d bytes", max)
		}
		frame = append(frame, frag...)
		if err == bufio.ErrBufferFull {
			continue
		}
		// A final value without a trailing newline is still a frame.
		if err == io.EOF && len(frame) > 0 {
			return frame, nil
		}
		if err != nil {
			return nil, err
		}
		return frame, nil
	}
}

// Reply sends a response correlated to a request ID.
func (c *Conn) Reply(id, typ string, payload any) error {
	env, err := NewEnvelope(id, typ, payload)
	if err != nil {
		return err
	}
	return c.write(env)
}

// Event sends an unsolicited event (empty ID).
func (c *Conn) Event(typ string, payload any) error {
	env, err := NewEnvelope("", typ, payload)
	if err != nil {
		return err
	}
	return c.write(env)
}

// Done is closed when the connection terminates.
func (c *Conn) Done() <-chan struct{} {
	return c.done
}

// Close terminates the connection.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() { close(c.done) })
	return c.nc.Close()
}

// PeerUID returns the effective UID of the connecting process. It reports
// false when the transport carries no peer-credential support.
func (c *Conn) PeerUID() (uint32, bool) {
	if c.peerUID == nil {
		return 0, false
	}
	return c.peerUID(c.nc)
}

// IsAdmin reports whether the connecting process may perform administrative
// requests such as shutdown. It reports false when the transport carries no
// admin check.
func (c *Conn) IsAdmin() bool {
	if c.peerAdmin == nil {
		return false
	}
	return c.peerAdmin(c.nc)
}

func (c *Conn) write(env *Envelope) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.writeTimeout > 0 {
		_ = c.nc.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	}
	if err := c.enc.Encode(env); err != nil {
		// The peer stopped reading or vanished. Close so the connection's
		// read loop reaps it and the next broadcast skips it.
		_ = c.Close()
		return err
	}
	return nil
}
