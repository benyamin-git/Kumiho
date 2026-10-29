//go:build windows

package windows

import (
	"path/filepath"
	"testing"

	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/netcfg"
	"github.com/benyamin-git/kumiho/internal/platform"
	"github.com/benyamin-git/kumiho/internal/settings"
)

func TestPaths(t *testing.T) {
	t.Setenv("ProgramData", `C:\ProgramData`)
	want := settings.Paths{
		Config:  filepath.Join(`C:\ProgramData`, "kumiho", "config.toml"),
		State:   filepath.Join(`C:\ProgramData`, "kumiho", "state.json"),
		Tokens:  filepath.Join(`C:\ProgramData`, "kumiho", "tokens.json"),
		Runtime: filepath.Join(`C:\ProgramData`, "kumiho"),
	}
	if got := New().Paths(); got != want {
		t.Fatalf("Paths() = %+v, want %+v", got, want)
	}
}

func TestPathsEnvOverrides(t *testing.T) {
	t.Setenv("ProgramData", `C:\ProgramData`)
	t.Setenv(settings.EnvConfig, `D:\cfg\config.toml`)
	t.Setenv(settings.EnvState, `D:\state\state.json`)
	t.Setenv(settings.EnvTokens, `D:\tokens\tokens.json`)
	t.Setenv(settings.EnvRuntime, `D:\run`)
	want := settings.Paths{
		Config:  `D:\cfg\config.toml`,
		State:   `D:\state\state.json`,
		Tokens:  `D:\tokens\tokens.json`,
		Runtime: `D:\run`,
	}
	if got := New().Paths(); got != want {
		t.Fatalf("Paths() = %+v, want %+v", got, want)
	}
}

func TestUserAgent(t *testing.T) {
	if got := New().UserAgent(); got != fxa.UserAgent {
		t.Fatalf("UserAgent() = %q, want %q", got, fxa.UserAgent)
	}
}

func TestControlEndpoint(t *testing.T) {
	t.Setenv(settings.EnvControlEndpoint, "")
	const want = `\\.\pipe\kumiho\control`
	if got := New().ControlEndpoint(); got != want {
		t.Fatalf("ControlEndpoint() = %q, want %q", got, want)
	}
}

func TestControlEndpointEnvOverride(t *testing.T) {
	const want = `\\.\pipe\kumiho-dev\control`
	t.Setenv(settings.EnvControlEndpoint, want)
	if got := New().ControlEndpoint(); got != want {
		t.Fatalf("ControlEndpoint() = %q, want %q", got, want)
	}
}

func TestTunDefaults(t *testing.T) {
	want := platform.TunDefaults{Name: netcfg.DefaultTunName, Addr: netcfg.DefaultTunAddr}
	if got := New().TunDefaults(); got != want {
		t.Fatalf("TunDefaults() = %+v, want %+v", got, want)
	}
}
