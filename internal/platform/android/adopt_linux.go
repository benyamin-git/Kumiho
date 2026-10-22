//go:build linux || android

package android

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// adoptFD takes ownership of the packet-device descriptor the Kotlin
// VpnService created (or a test pipe on Linux) and wraps it in the IPv6
// filter.
//
// The descriptor is switched to non-blocking so os.NewFile registers it with
// the runtime poller: a pending Read then unblocks with os.ErrClosed as soon
// as Close runs, which is what lets the engine's IO loops terminate.
//
// Ownership passes to the returned device; every failure path closes fd so
// the caller never has to.
func adoptFD(fd int, name string) (*ipv6Filter, error) {
	if fd < 0 {
		return nil, fmt.Errorf("android: invalid device fd %d", fd)
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("android: set non-blocking on device fd %d: %w", fd, err)
	}
	f := os.NewFile(uintptr(fd), name)
	if f == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("android: device fd %d is not a valid descriptor", fd)
	}
	return newIPv6Filter(f, name), nil
}
