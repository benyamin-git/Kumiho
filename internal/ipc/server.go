package ipc

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"sync"
)

// Server accepts control connections on a Unix socket.
type Server struct {
	path    string
	ln      net.Listener
	peerUID func(net.Conn) (uint32, bool)
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
			conn := newConn(nc, s.peerUID)
			defer conn.Close()
			_ = handler(ctx, conn)
		}(nc)
	}
}

// Close stops the listener and removes the socket file (best effort).
func (s *Server) Close() error {
	err := s.ln.Close()
	_ = os.Remove(s.path)
	return err
}

// Path returns the socket path.
func (s *Server) Path() string {
	return s.path
}

// Conn is one server-side client connection.
type Conn struct {
	nc      net.Conn
	enc     *json.Encoder
	dec     *json.Decoder
	peerUID func(net.Conn) (uint32, bool)

	mu        sync.Mutex
	done      chan struct{}
	closeOnce sync.Once
}

func newConn(nc net.Conn, peerUID func(net.Conn) (uint32, bool)) *Conn {
	return &Conn{
		nc:      nc,
		enc:     json.NewEncoder(nc),
		dec:     json.NewDecoder(nc),
		peerUID: peerUID,
		done:    make(chan struct{}),
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

// PeerUID returns the effective UID of the connecting process. It reports
// false when the transport carries no peer-credential support.
func (c *Conn) PeerUID() (uint32, bool) {
	if c.peerUID == nil {
		return 0, false
	}
	return c.peerUID(c.nc)
}

func (c *Conn) write(env *Envelope) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enc.Encode(env)
}
