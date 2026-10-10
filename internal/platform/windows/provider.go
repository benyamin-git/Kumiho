//go:build windows

package windows

import (
	"context"
	"errors"
	"net"

	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/netcfg"
	"github.com/benyamin-git/kumiho/internal/platform"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// Provider implements platform.Provider with the Windows mechanisms. The
// device, network configuration and link monitoring still carry interim
// behavior until T9/T10/T12 land.
type Provider struct{}

// New returns the Windows provider.
func New() *Provider { return &Provider{} }

// Name returns the provider name.
func (p *Provider) Name() string { return "windows" }

// UserAgent returns the frozen Mozilla API UA (D25).
func (p *Provider) UserAgent() string { return fxa.UserAgent }

// TunDefaults returns the Windows tunnel interface name and address.
func (p *Provider) TunDefaults() platform.TunDefaults {
	return platform.TunDefaults{Name: netcfg.DefaultTunName, Addr: netcfg.DefaultTunAddr}
}

// DNSListenAddr returns the in-TUN resolver address.
func (p *Provider) DNSListenAddr() string {
	return net.JoinHostPort(netcfg.DefaultTunAddr, "53")
}

// OpenDevice returns the interim not-implemented error (replaced in T9).
func (p *Provider) OpenDevice(context.Context, platform.DeviceSpec) (platform.Device, error) {
	return nil, errors.New("windows: not implemented yet")
}

// NetConfig returns the interim not-implemented network configuration
// (replaced in T10).
func (p *Provider) NetConfig() platform.NetConfig { return netConfig{} }

// WatchLinks returns a channel that never fires (replaced in T12).
func (p *Provider) WatchLinks(context.Context) (<-chan struct{}, error) {
	return make(chan struct{}), nil
}

// HasDefaultRoute always reports the network as up (replaced in T12).
func (p *Provider) HasDefaultRoute() bool { return true }

// BypassDialer returns a plain dialer: Windows has no SO_MARK equivalent.
func (p *Provider) BypassDialer() *net.Dialer { return &net.Dialer{} }

// Paths returns the machine-wide paths with environment overrides applied.
func (p *Provider) Paths() settings.Paths { return windowsPaths() }

// ControlEndpoint returns the named-pipe control endpoint (D7/D19).
func (p *Provider) ControlEndpoint() string { return `\\.\pipe\kumiho\control` }

// ChownControlEndpoint is a no-op: the pipe ACL is applied by the server when
// it creates the pipe.
func (p *Provider) ChownControlEndpoint(string) {}

type netConfig struct{}

// Apply is the interim not-implemented stub (replaced in T10).
func (netConfig) Apply(context.Context, platform.NetConfigSpec) error {
	return errors.New("windows: not implemented yet")
}

// Cleanup is a no-op so proxy-only mode still works.
func (netConfig) Cleanup(context.Context, platform.NetConfigSpec) {}

var _ platform.Provider = (*Provider)(nil)
