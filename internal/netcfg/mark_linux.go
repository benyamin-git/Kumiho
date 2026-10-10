//go:build linux && !android

package netcfg

import (
	"log"
	"net"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

var markWarnOnce sync.Once

// MarkedDialer returns a dialer that tags every socket with SO_MARK 0x2 so
// the policy-routing mark chain never sends daemon traffic into the tunnel
// (PLAN.md §3.2 rules 1 and 4). Marking is best-effort: without
// CAP_NET_ADMIN it cannot work (and neither can the TUN), so connections
// continue unmarked with a one-time warning.
func MarkedDialer() *net.Dialer {
	return &net.Dialer{
		Control: func(_, _ string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) {
				serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, 0x2)
			}); err != nil {
				return err
			}
			if serr != nil {
				markWarnOnce.Do(func() {
					log.Printf("kumiho: SO_MARK 0x2 failed (%v); control-plane traffic stays unmarked", serr)
				})
			}
			return nil
		},
	}
}
