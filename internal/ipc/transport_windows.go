//go:build windows

package ipc

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// WindowsTransport carries the control protocol over a named pipe. The wire
// format is unchanged: byte mode, newline-delimited JSON protocol v1.
type WindowsTransport struct{}

const kumihoGroup = "kumiho"

// controlPipeSDDLBase grants full control to LocalSystem and the built-in
// Administrators group. The kumiho group entry is appended when it resolves.
const controlPipeSDDLBase = "D:P(A;;GA;;;SY)(A;;GA;;;BA)"

// pipeSDDL builds the control pipe DACL; an empty groupSID omits the kumiho
// group entry (the fallback used before the install script creates it).
func pipeSDDL(groupSID string) string {
	if groupSID == "" {
		return controlPipeSDDLBase
	}
	return controlPipeSDDLBase + "(A;;GA;;;" + groupSID + ")"
}

// controlPipeSDDL resolves the local kumiho group with LookupAccountName and
// returns the pipe DACL. A fresh box has no group yet: the pipe then stays
// restricted to SYSTEM and Administrators, and the daemon logs the downgrade
// (standard logger, i.e. the daemon's stderr).
func controlPipeSDDL() string {
	sid, err := winio.LookupSidByName(kumihoGroup)
	if err != nil {
		log.Printf("kumiho ipc: local group %q not found; control pipe ACL restricted to SYSTEM and Administrators only: %v", kumihoGroup, err)
		return pipeSDDL("")
	}
	return pipeSDDL(sid)
}

// Listen creates the control pipe and refuses to steal one that a live daemon
// already serves.
func (WindowsTransport) Listen(endpoint string) (*Server, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if c, err := (WindowsTransport{}).Dial(ctx, endpoint); err == nil {
		_ = c.Close()
		return nil, fmt.Errorf("%s: another kumiho daemon is already listening", endpoint)
	}
	ln, err := winio.ListenPipe(endpoint, &winio.PipeConfig{SecurityDescriptor: controlPipeSDDL()})
	if err != nil {
		return nil, err
	}
	return &Server{path: endpoint, ln: ln, peerUID: WindowsTransport{}.PeerUID, peerAdmin: peerIsAdmin}, nil
}

// Dial connects to the daemon control pipe.
func (WindowsTransport) Dial(ctx context.Context, endpoint string) (*Client, error) {
	conn, err := winio.DialPipeContext(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	return newClient(conn), nil
}

// PeerUID reports false: Windows carries no Unix-style peer credentials.
func (WindowsTransport) PeerUID(net.Conn) (uint32, bool) { return 0, false }

var _ Transport = WindowsTransport{}
