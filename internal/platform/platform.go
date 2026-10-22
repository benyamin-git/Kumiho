// Package platform defines the OS seam of the kumiho core: the Provider
// interface plus the device and network-configuration types the engine
// consumes. platform/linux implements it with the v0.1.0 mechanisms; other
// OSes fall back to the not-implemented provider until their platform cycle.
package platform

import (
	"context"
	"io"
	"net"

	"github.com/benyamin-git/kumiho/internal/settings"
)

// Provider is the per-OS implementation surface the engine is built on. It is
// resolved once at the composition root (internal/platform/auto) and injected
// explicitly into the engine (D21).
type Provider interface {
	Name() string             // "linux", "windows", "android", "unsupported"
	UserAgent() string        // Mozilla API UA (Linux: current string, byte-identical)
	TunDefaults() TunDefaults // {Name, Addr}; zero on Android
	DNSListenAddr() string    // Linux/Windows: host listener (10.8.0.2:53); Android: the routed in-tunnel resolver served in userspace; fake/tests: 127.0.0.1:0
	OpenDevice(ctx context.Context, spec DeviceSpec) (Device, error)
	NetConfig() NetConfig                                    // Apply/Cleanup
	WatchLinks(ctx context.Context) (<-chan struct{}, error) // coalesced wake-ups
	HasDefaultRoute() bool                                   // replaces daemon's netlink RouteGet probe
	BypassDialer() *net.Dialer                               // Linux: SO_MARK 0x2; others: plain
	Paths() settings.Paths                                   // OS defaults + KUMIHO_* env overrides
	ControlEndpoint() string                                 // opaque endpoint for ipc.Transport
	ChownControlEndpoint(endpoint string)                    // Linux: current sudo-chown logic; others no-op
}

// UserspaceDNS is an optional Provider capability. A provider that implements
// it and returns true answers the tunnel DNS inside the packet stack: the
// engine must not bind DNSListenAddr() on the host. Android uses this because
// an unprivileged app cannot bind port 53; DNSListenAddr() then names the
// routed in-tunnel resolver the VpnService advertises, and the tun2socks
// adapter answers queries addressed to it through the shared strict resolver.
type UserspaceDNS interface {
	UserspaceDNS() bool
}

// Device is one packet device (TUN interface or platform equivalent).
type Device interface {
	io.ReadWriteCloser
	Name() string
}

// DeviceSpec describes one packet device request.
type DeviceSpec struct {
	Name string
	MTU  int
	FD   int // >= 0: adopt an existing fd (Android, future); Linux errors
}

// TunDefaults are the interface name and address the full tunnel uses.
type TunDefaults struct{ Name, Addr string }

// NetConfig applies and removes the OS network configuration for the tunnel.
type NetConfig interface {
	Apply(ctx context.Context, spec NetConfigSpec) error
	Cleanup(ctx context.Context, spec NetConfigSpec) // never fails
}

// NetConfigSpec is today's netcfg.Config in platform-neutral terms.
type NetConfigSpec struct {
	TunName, TunAddr string
	TunMTU           int
	KillSwitch       bool
	AllowLAN         bool
	ExcludeCIDRs     []string
	LANSubnets       []string // detected inside the Linux implementation
}
