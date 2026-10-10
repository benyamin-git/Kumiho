// Package unsupported is the fallback platform provider for OSes without a
// real implementation yet (D14). The non-networking bits are functional so
// the daemon starts and proxy-only mode works; the full tunnel returns clear
// "not implemented yet" errors.
package unsupported

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"

	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/platform"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// Provider is the not-implemented fallback.
type Provider struct {
	name  string
	paths settings.Paths
}

// New returns a fallback provider with the given name and paths.
func New(name string, paths settings.Paths) *Provider {
	return &Provider{name: name, paths: paths}
}

// DefaultPaths returns the placeholder paths for OSes without a provider:
// config under the user config dir, state/tokens under the cache dir and the
// runtime dir in the system temp dir. KUMIHO_* overrides apply.
func DefaultPaths() settings.Paths {
	configDir, _ := os.UserConfigDir()
	cacheDir, _ := os.UserCacheDir()
	return settings.ApplyEnvOverrides(settings.Paths{
		Config:  filepath.Join(configDir, "kumiho", "config.toml"),
		State:   filepath.Join(cacheDir, "kumiho", "state.json"),
		Tokens:  filepath.Join(cacheDir, "kumiho", "tokens.json"),
		Runtime: filepath.Join(os.TempDir(), "kumiho"),
	})
}

// Name returns the provider name.
func (p *Provider) Name() string { return p.name }

// UserAgent returns the current Linux string until a platform cycle defines
// its own (D22).
func (p *Provider) UserAgent() string { return fxa.UserAgent }

// TunDefaults returns zero values: no tunnel is configured here.
func (p *Provider) TunDefaults() platform.TunDefaults { return platform.TunDefaults{} }

// DNSListenAddr returns the loopback placeholder; the full tunnel never
// reaches it because OpenDevice fails first.
func (p *Provider) DNSListenAddr() string { return net.JoinHostPort("127.0.0.1", "53") }

// OpenDevice is not implemented yet.
func (p *Provider) OpenDevice(context.Context, platform.DeviceSpec) (platform.Device, error) {
	return nil, errors.New("platform: not implemented yet")
}

// NetConfig returns the not-implemented network configuration.
func (p *Provider) NetConfig() platform.NetConfig { return netConfig{} }

// WatchLinks returns a channel that never fires.
func (p *Provider) WatchLinks(context.Context) (<-chan struct{}, error) {
	return make(chan struct{}), nil
}

// HasDefaultRoute always reports the network as up.
func (p *Provider) HasDefaultRoute() bool { return true }

// BypassDialer returns a plain dialer.
func (p *Provider) BypassDialer() *net.Dialer { return &net.Dialer{} }

// Paths returns the provider's placeholder paths.
func (p *Provider) Paths() settings.Paths { return p.paths }

// ControlEndpoint returns the control socket path inside the runtime dir.
func (p *Provider) ControlEndpoint() string {
	return filepath.Join(p.paths.Runtime, "control.sock")
}

// ChownControlEndpoint is a no-op.
func (p *Provider) ChownControlEndpoint(string) {}

type netConfig struct{}

func (netConfig) Apply(context.Context, platform.NetConfigSpec) error {
	return errors.New("platform: not implemented yet")
}

// Cleanup is a no-op (D14: proxy-only still works without network config).
func (netConfig) Cleanup(context.Context, platform.NetConfigSpec) {}

var _ platform.Provider = (*Provider)(nil)
