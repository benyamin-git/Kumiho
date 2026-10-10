package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// UnixTransport carries the control protocol over a Unix-domain socket.
type UnixTransport struct{}

// Listen creates the control socket with mode 0660, removing stale sockets
// left by a crashed daemon and refusing to steal a live daemon's socket.
func (UnixTransport) Listen(endpoint string) (*Server, error) {
	if err := os.MkdirAll(filepath.Dir(endpoint), 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(endpoint); err == nil {
		d := net.Dialer{Timeout: 500 * time.Millisecond}
		if conn, err := d.Dial("unix", endpoint); err == nil {
			conn.Close()
			return nil, fmt.Errorf("%s: another kumiho daemon is already listening", endpoint)
		}
		if err := os.Remove(endpoint); err != nil {
			return nil, err
		}
	}
	ln, err := net.Listen("unix", endpoint)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(endpoint, 0o660); err != nil {
		ln.Close()
		return nil, err
	}
	return &Server{path: endpoint, ln: ln, peerUID: UnixTransport{}.PeerUID}, nil
}

// Dial connects to the daemon control socket.
func (UnixTransport) Dial(ctx context.Context, endpoint string) (*Client, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", endpoint)
	if err != nil {
		return nil, err
	}
	c := &Client{
		conn:    conn,
		enc:     json.NewEncoder(conn),
		dec:     json.NewDecoder(conn),
		pending: make(map[string]chan *Envelope),
		events:  make(chan *Envelope, 256),
	}
	go c.readLoop()
	return c, nil
}

// PeerUID returns the effective UID of the peer process. It reports false
// when the platform cannot provide peer credentials.
func (UnixTransport) PeerUID(nc net.Conn) (uint32, bool) {
	uid, err := peerUID(nc)
	if err != nil {
		return 0, false
	}
	return uid, true
}
