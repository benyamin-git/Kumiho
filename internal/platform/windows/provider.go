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

// Provider implements platform.Provider with the Windows mechanisms.
// Interface configuration lives in netcfg.go, link monitoring in netmon.go.
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

// OpenDevice creates the Wintun adapter. FD adoption is not a Windows
// capability; the engine always passes -1 (Linux parity).
func (p *Provider) OpenDevice(_ context.Context, spec platform.DeviceSpec) (platform.Device, error) {
	if spec.FD >= 0 {
		return nil, errors.New("windows: cannot adopt an existing fd")
	}
	return openDevice(spec.Name, spec.MTU)
}

// NetConfig returns the real route/DNS network configuration.
func (p *Provider) NetConfig() platform.NetConfig { return &netConfig{} }

// BypassDialer returns a plain dialer: Windows has no SO_MARK equivalent.
func (p *Provider) BypassDialer() *net.Dialer { return &net.Dialer{} }

// Paths returns the machine-wide paths with environment overrides applied.
func (p *Provider) Paths() settings.Paths { return windowsPaths() }

// ControlEndpoint returns the named-pipe control endpoint (D7/D19).
func (p *Provider) ControlEndpoint() string { return `\\.\pipe\kumiho\control` }

// ChownControlEndpoint is a no-op: the pipe ACL is applied by the server when
// it creates the pipe.
func (p *Provider) ChownControlEndpoint(string) {}

var _ platform.Provider = (*Provider)(nil)
