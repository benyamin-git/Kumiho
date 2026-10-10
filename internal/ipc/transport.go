package ipc

import (
	"context"
	"net"
)

// Transport opens and accepts control-protocol connections over a local
// endpoint. UnixTransport is the v0.1.0 implementation; other platforms
// provide their own.
type Transport interface {
	Listen(endpoint string) (*Server, error)
	Dial(ctx context.Context, endpoint string) (*Client, error)
	PeerUID(nc net.Conn) (uint32, bool)
}
