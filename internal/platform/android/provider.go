//go:build android

package android

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"time"

	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/platform"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// Bridge is the Kotlin side of the VpnService handshake. The gomobile
// DeviceBridge interface satisfies it.
type Bridge interface {
	AcquireFD(mtu int) (int, error)
	NetworkAvailable() bool
}

// Provider implements platform.Provider on Android (D13). The Kotlin
// VpnService owns the interface: it applies addresses, routes, DNS and the
// split tunnel, then hands the tunnel descriptor to the engine through the
// Bridge, which this provider adopts and wraps in the IPv6 filter (C5). The
// engine runs in-process inside the app (D8).
type Provider struct {
	name   string
	paths  settings.Paths
	bridge Bridge
}

// New returns the Android provider with placeholder paths and no bridge. It
// exists for a standalone (compile-only) Android build of the daemon; the
// mobile facade always constructs the provider with NewWithPaths so the
// app-private directories and the Kotlin bridge are wired in.
func New() *Provider { return NewWithPaths(defaultPaths(), nil) }

// NewWithPaths returns the Android provider for the app-private paths and the
// Kotlin VpnService bridge. A nil bridge is allowed: proxy-only mode and
// tests work without one, and a full-tunnel connect then fails with a clear
// error.
func NewWithPaths(paths settings.Paths, bridge Bridge) *Provider {
	return &Provider{name: "android", paths: paths, bridge: bridge}
}

// Name returns the provider name.
func (p *Provider) Name() string { return p.name }

// UserAgent returns the Mozilla API UA. Android identifies as the official
// client, byte-identical to the other platforms (D12).
func (p *Provider) UserAgent() string { return fxa.UserAgent }

// TunDefaults returns zero values: the VpnService, not the engine, owns the
// interface name and addresses.
func (p *Provider) TunDefaults() platform.TunDefaults { return platform.TunDefaults{} }

// DNSListenAddr returns the routed in-tunnel resolver address the
// VpnService.Builder installs as the tunnel DNS server. It must not be the
// TUN interface address: Android routes queries to this non-local address
// through the TUN, where the engine answers them from the userspace stack.
// Apps cannot bind port 53, so the engine never opens a host socket here.
func (p *Provider) DNSListenAddr() string { return "10.8.0.1:53" }

// UserspaceDNS reports that the tunnel DNS is served from the packet stack
// rather than a privileged host listener.
func (p *Provider) UserspaceDNS() bool { return true }

// OpenDevice adopts the packet descriptor the Kotlin VpnService created. A
// spec with FD >= 0 is adopted directly (the AAR probe and tests); otherwise
// the bridge is asked to establish the interface and hand back its
// descriptor. The returned device drops IPv6 (C5).
func (p *Provider) OpenDevice(_ context.Context, spec platform.DeviceSpec) (platform.Device, error) {
	name := spec.Name
	if name == "" {
		name = "tun"
	}
	fd := spec.FD
	if fd < 0 {
		if p.bridge == nil {
			return nil, errors.New("android: no device bridge: the VpnService permission and service are required for the full tunnel")
		}
		acquired, err := p.bridge.AcquireFD(spec.MTU)
		if err != nil {
			return nil, fmt.Errorf("android: acquire vpn descriptor: %w", err)
		}
		fd = acquired
	}
	dev, err := adoptFD(fd, name)
	if err != nil {
		return nil, err
	}
	return dev, nil
}

// NetConfig returns the no-op network configuration; the VpnService.Builder
// already applied it.
func (p *Provider) NetConfig() platform.NetConfig { return netConfig{} }

// WatchLinks emits a wake-up every 2 s until ctx is done. Android has no
// netlink: the connectivity callback behind the bridge is the source of truth
// for link state, and the periodic wake-up is enough for the watchdog to
// re-check route availability.
func (p *Provider) WatchLinks(ctx context.Context) (<-chan struct{}, error) {
	events := make(chan struct{}, 1)
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case events <- struct{}{}:
				default: // a burst collapses into one pending wake-up
				}
			}
		}
	}()
	return events, nil
}

// HasDefaultRoute reports whether the physical network is usable, from the
// Android connectivity callback. Without a bridge (proxy-only mode and tests)
// the network is assumed up.
func (p *Provider) HasDefaultRoute() bool {
	if p.bridge == nil {
		return true
	}
	return p.bridge.NetworkAvailable()
}

// BypassDialer returns a plain dialer: Kumiho excludes itself from its own
// VPN, so the control plane never needs a bypass mark.
func (p *Provider) BypassDialer() *net.Dialer { return &net.Dialer{} }

// Paths returns the app-private paths passed to NewWithPaths.
func (p *Provider) Paths() settings.Paths { return p.paths }

// ControlEndpoint returns the control socket path inside the runtime dir.
func (p *Provider) ControlEndpoint() string {
	return filepath.Join(p.paths.Runtime, "control.sock")
}

// ChownControlEndpoint is a no-op: the app owns its private runtime dir.
func (p *Provider) ChownControlEndpoint(string) {}

var _ platform.Provider = (*Provider)(nil)
var _ platform.UserspaceDNS = (*Provider)(nil)
