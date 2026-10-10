//go:build !linux || android

package netcfg

import "net"

// MarkedDialer on non-Linux hosts is a plain dialer (packet marks and the
// full tunnel are Linux-only).
func MarkedDialer() *net.Dialer {
	return &net.Dialer{}
}
