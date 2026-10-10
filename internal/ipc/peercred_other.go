//go:build (!linux && !windows) || android

package ipc

import (
	"errors"
	"net"
)

func peerUID(net.Conn) (uint32, error) {
	return 0, errors.New("peer credentials are not supported on this platform")
}

func peerIsAdmin(net.Conn) bool {
	return false
}
