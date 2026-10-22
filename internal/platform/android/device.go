package android

import (
	"io"

	"github.com/benyamin-git/kumiho/internal/platform"
)

// ipv6Filter drops IPv6 packets from an adopted packet device.
//
// Android has no IPv6 path in this tunnel: the Kotlin VpnService.Builder adds
// fd00::2/128 so IPv6 is captured rather than leaking around the VPN, and
// Kumiho blackholes it (D17/A7). Discarding the packets here, before the
// engine's tun2socks sees them, is C5: the engine stays free of any IPv6
// special case and IPv6-capable destinations fall back to IPv4 instead of
// hanging.
//
// The file is deliberately portable so the package builds (and its tests run)
// on the host as well as on Android.
type ipv6Filter struct {
	rw   io.ReadWriteCloser
	name string
}

// newIPv6Filter wraps rw in the IPv6-dropping filter; name is reported by
// Name.
func newIPv6Filter(rw io.ReadWriteCloser, name string) *ipv6Filter {
	return &ipv6Filter{rw: rw, name: name}
}

// Read returns the next IPv4 packet, reading past any IPv6 packet. Only a
// read that returned data can be IPv6 (the version nibble is the high nibble
// of the first byte); errors and empty reads are returned unchanged.
func (f *ipv6Filter) Read(p []byte) (int, error) {
	for {
		n, err := f.rw.Read(p)
		if n > 0 && p[0]>>4 == 6 {
			continue
		}
		return n, err
	}
}

// Write sends one outbound packet to the device.
func (f *ipv6Filter) Write(p []byte) (int, error) { return f.rw.Write(p) }

// Close closes the underlying device. Idempotence is the underlying device's
// concern.
func (f *ipv6Filter) Close() error { return f.rw.Close() }

// Name returns the interface name the device was adopted under.
func (f *ipv6Filter) Name() string { return f.name }

var _ platform.Device = (*ipv6Filter)(nil)
