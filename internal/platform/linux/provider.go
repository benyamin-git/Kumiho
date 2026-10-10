//go:build linux && !android

package linux

import (
	"context"
	"errors"
	"net"
	"path/filepath"

	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/netcfg"
	"github.com/benyamin-git/kumiho/internal/platform"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// Provider implements platform.Provider with the v0.1.0 Linux mechanisms.
type Provider struct{}

// New returns the Linux provider.
func New() *Provider { return &Provider{} }

// Name returns the provider name.
func (p *Provider) Name() string { return "linux" }

// UserAgent returns the byte-identical Mozilla API UA (D12).
func (p *Provider) UserAgent() string { return fxa.UserAgent }

// TunDefaults returns the Linux tunnel interface name and address.
func (p *Provider) TunDefaults() platform.TunDefaults {
	return platform.TunDefaults{Name: netcfg.DefaultTunName, Addr: netcfg.DefaultTunAddr}
}

// DNSListenAddr returns the in-TUN resolver address.
func (p *Provider) DNSListenAddr() string {
	return net.JoinHostPort(netcfg.DefaultTunAddr, "53")
}

// OpenDevice creates the TUN interface. FD adoption is not a Linux capability.
func (p *Provider) OpenDevice(_ context.Context, spec platform.DeviceSpec) (platform.Device, error) {
	if spec.FD >= 0 {
		return nil, errors.New("adopting a device fd is not supported on Linux")
	}
	return Open(spec.Name, spec.MTU)
}

// NetConfig returns the adapter over the v0.1.0 netcfg applier.
func (p *Provider) NetConfig() platform.NetConfig {
	return &netConfig{applier: netcfg.DefaultApplier(nil)}
}

// BypassDialer returns the SO_MARK 0x2 dialer for control-plane sockets.
func (p *Provider) BypassDialer() *net.Dialer { return netcfg.MarkedDialer() }

// Paths returns the production paths with environment overrides applied.
func (p *Provider) Paths() settings.Paths { return settings.DefaultPaths() }

// ControlEndpoint returns the Unix control socket path.
func (p *Provider) ControlEndpoint() string {
	return filepath.Join(p.Paths().Runtime, "control.sock")
}

var _ platform.Provider = (*Provider)(nil)
