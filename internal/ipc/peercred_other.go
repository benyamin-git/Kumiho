//go:build !linux || android

package ipc

import (
	"errors"
	"net"
)

func peerUID(net.Conn) (uint32, error) {
	return 0, errors.New("peer credentials are not supported on this platform")
}
