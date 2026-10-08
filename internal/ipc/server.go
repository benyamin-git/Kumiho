package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Server accepts control connections on a Unix socket.
type Server struct {
	path string
	ln   net.Listener
}

// Listen creates the control socket with mode 0660, removing stale sockets
// left by a crashed daemon and refusing to steal a live daemon's socket.
func Listen(path string) (*Server, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err == nil {
		d := net.Dialer{Timeout: 500 * time.Millisecond}
		if conn, err := d.Dial("unix", path); err == nil {
			conn.Close()
			return nil, fmt.Errorf("%s: another kumiho daemon is already listening", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o660); err != nil {
		ln.Close()
		return nil, err
	}
	return &Server{path: path, ln: ln}, nil
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
			conn := newConn(nc)
			defer conn.Close()
			_ = handler(ctx, conn)
		}(nc)
	}
}

// Close stops the listener.
func (s *Server) Close() error {
	return s.ln.Close()
}

// Path returns the socket path.
func (s *Server) Path() string {
	return s.path
}

// Conn is one server-side client connection.
type Conn struct {
	nc  net.Conn
	enc *json.Encoder
	dec *json.Decoder

	mu        sync.Mutex
	done      chan struct{}
	closeOnce sync.Once
}

func newConn(nc net.Conn) *Conn {
	return &Conn{
		nc:   nc,
		enc:  json.NewEncoder(nc),
		dec:  json.NewDecoder(nc),
		done: make(chan struct{}),
	}
}

// Read waits for the next request.
func (c *Conn) Read() (*Envelope, error) {
	var env Envelope
	if err := c.dec.Decode(&env); err != nil {
		return nil, err
	}
	return &env, nil
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

// PeerUID returns the effective UID of the connecting process (Linux only).
func (c *Conn) PeerUID() (uint32, bool) {
	uid, err := peerUID(c.nc)
	if err != nil {
		return 0, false
	}
	return uid, true
}

func (c *Conn) write(env *Envelope) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enc.Encode(env)
}
