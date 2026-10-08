package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func testPaths(t *testing.T) Paths {
	t.Helper()
	dir := t.TempDir()
	return Paths{
		Config:  filepath.Join(dir, "config.toml"),
		State:   filepath.Join(dir, "state.json"),
		Runtime: dir,
	}
}

func TestDefaultConfigValid(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
}

func TestLoadConfigPartialKeepsDefaults(t *testing.T) {
	paths := testPaths(t)
	content := "socks_port = 1081\nexclude_cidrs = [\"10.0.0.0/8\", \"fd00::/8\"]\n"
	if err := os.WriteFile(paths.Config, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg := st.Config()
	if cfg.SocksPort != 1081 {
		t.Errorf("SocksPort = %d, want 1081", cfg.SocksPort)
	}
	if len(cfg.ExcludeCIDRs) != 2 {
		t.Errorf("ExcludeCIDRs = %v, want 2 entries", cfg.ExcludeCIDRs)
	}
	if cfg.MTU != DefaultConfig().MTU {
		t.Errorf("MTU = %d, want default %d", cfg.MTU, DefaultConfig().MTU)
	}
	if cfg.SocksBind != "127.0.0.1" {
		t.Errorf("SocksBind = %q, want default", cfg.SocksBind)
	}
}

func TestValidateRejects(t *testing.T) {
	mutate := func(fn func(*Config)) Config {
		c := DefaultConfig()
		fn(&c)
		return c
	}
	cases := map[string]Config{
		"socks_bind":     mutate(func(c *Config) { c.SocksBind = "0.0.0.0" }),
		"socks_port":     mutate(func(c *Config) { c.SocksPort = 0 }),
		"kill_switch":    mutate(func(c *Config) { c.KillSwitch = "sometimes" }),
		"autoconnect":    mutate(func(c *Config) { c.Autoconnect = "always" }),
		"doh_provider":   mutate(func(c *Config) { c.DoHProvider = "yandex" }),
		"dns_upstream":   mutate(func(c *Config) { c.DNSUpstream = "not-an-ip" }),
		"edge_address":   mutate(func(c *Config) { c.EdgeAddress = "edge.example.com" }),
		"upstream_proxy": mutate(func(c *Config) { c.UpstreamProxy = "socks5://" }),
		"exclude_cidrs":  mutate(func(c *Config) { c.ExcludeCIDRs = []string{"10.0.0.1"} }),
		"mtu":            mutate(func(c *Config) { c.MTU = 100 }),
		"log_level":      mutate(func(c *Config) { c.LogLevel = "verbose" }),
		"quota_poll":     mutate(func(c *Config) { c.QuotaPollMinutes = 0 }),
	}
	for name, cfg := range cases {
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestStateRoundTripAndFirstInstallDefaults(t *testing.T) {
	paths := testPaths(t)
	st, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !st.EffectiveKillSwitch() {
		t.Error("first install: kill switch should default to ON")
	}
	if !st.EffectiveAutoconnect() {
		t.Error("first install: autoconnect should default to ON")
	}

	off := false
	if err := st.UpdateState(func(s *State) { s.KillSwitch = &off }); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.EffectiveKillSwitch() {
		t.Error("kill switch OFF state did not persist")
	}
	if !reopened.EffectiveAutoconnect() {
		t.Error("autoconnect should still default to ON")
	}
	if runtime.GOOS == "linux" {
		if fi, err := os.Stat(paths.State); err != nil {
			t.Fatal(err)
		} else if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Errorf("state file mode = %o, want 600", perm)
		}
	}
}

func TestConfigOverridesState(t *testing.T) {
	paths := testPaths(t)
	if err := os.WriteFile(paths.Config, []byte("kill_switch = \"off\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	on := true
	st, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateState(func(s *State) { s.KillSwitch = &on }); err != nil {
		t.Fatal(err)
	}
	if st.EffectiveKillSwitch() {
		t.Error("admin config kill_switch=off must override remembered state")
	}
}

func TestInvalidConfigFileFailsOpen(t *testing.T) {
	paths := testPaths(t)
	if err := os.WriteFile(paths.Config, []byte("socks_port = 70000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(paths); err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestRuntimeOverrides(t *testing.T) {
	paths := testPaths(t)
	st, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}

	if err := st.SetOverride("dns_upstream", "9.9.9.9"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetOverride("log_level", "debug"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetOverride("socks_port", "1085"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetOverride("exclude_cidrs", "10.0.0.0/8, 192.168.0.0/16 ,"); err != nil {
		t.Fatal(err)
	}

	cfg := st.EffectiveConfig()
	if cfg.DNSUpstream != "9.9.9.9" || cfg.LogLevel != "debug" || cfg.SocksPort != 1085 {
		t.Fatalf("effective config = %+v", cfg)
	}
	if len(cfg.ExcludeCIDRs) != 2 || cfg.ExcludeCIDRs[0] != "10.0.0.0/8" || cfg.ExcludeCIDRs[1] != "192.168.0.0/16" {
		t.Fatalf("effective exclude_cidrs = %v", cfg.ExcludeCIDRs)
	}
	admin := st.Config()
	if admin.DNSUpstream != "" || admin.LogLevel != "info" || admin.SocksPort != 1080 {
		t.Fatalf("admin config must stay untouched: %+v", admin)
	}

	reopened, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.EffectiveConfig().SocksPort != 1085 {
		t.Fatal("override did not survive reopen")
	}
	want := []string{"dns_upstream", "exclude_cidrs", "log_level", "socks_port"}
	if got := reopened.OverriddenKeys(); !reflect.DeepEqual(got, want) {
		t.Fatalf("OverriddenKeys = %v, want %v", got, want)
	}

	if err := reopened.ClearOverride("socks_port"); err != nil {
		t.Fatal(err)
	}
	if reopened.EffectiveConfig().SocksPort != 1080 {
		t.Fatal("clear did not restore the default")
	}
	if !contains(reopened.OverriddenKeys(), "dns_upstream") {
		t.Fatal("clearing one key must not drop the others")
	}
	if err := reopened.ClearOverride("nope"); err != ErrUnknownSetting {
		t.Fatalf("clear unknown: err = %v", err)
	}
}

func TestSetOverrideValidation(t *testing.T) {
	paths := testPaths(t)
	st, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ key, value string }{
		{"dns_upstream", "not-an-ip"},
		{"edge_address", "edge.example.com"},
		{"upstream_proxy", "socks5://"},
		{"socks_port", "99999"},
		{"mtu", "100"},
		{"quota_poll_minutes", "0"},
		{"doh_provider", "yandex"},
		{"log_level", "verbose"},
		{"exclude_cidrs", "10.0.0.1"},
		{"exit_check", "maybe"},
		{"kill_switch", "maybe"},
		{"nonsense", "1"},
	} {
		if err := st.SetOverride(tc.key, tc.value); err == nil {
			t.Errorf("SetOverride(%q, %q): expected error", tc.key, tc.value)
		}
	}
	// A failed set must not persist anything.
	if keys := st.OverriddenKeys(); len(keys) != 0 {
		t.Fatalf("overrides after failures = %v", keys)
	}

	if err := st.SetOverride("kill_switch", "off"); err != nil {
		t.Fatal(err)
	}
	if st.EffectiveKillSwitch() {
		t.Fatal("kill switch override not applied")
	}
	if err := st.SetOverride("kill_switch", "last"); err != nil {
		t.Fatal(err)
	}
	if st.EffectiveKillSwitch() {
		t.Fatal("'last' must pin the current effective value (off)")
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
