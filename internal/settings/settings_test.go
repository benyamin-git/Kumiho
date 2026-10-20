package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
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
		"socks_bind":         mutate(func(c *Config) { c.SocksBind = "0.0.0.0" }),
		"socks_port":         mutate(func(c *Config) { c.SocksPort = 0 }),
		"kill_switch":        mutate(func(c *Config) { c.KillSwitch = "sometimes" }),
		"autoconnect":        mutate(func(c *Config) { c.Autoconnect = "always" }),
		"doh_provider":       mutate(func(c *Config) { c.DoHProvider = "yandex" }),
		"dns_upstream":       mutate(func(c *Config) { c.DNSUpstream = "not-an-ip" }),
		"edge_address":       mutate(func(c *Config) { c.EdgeAddress = "edge.example.com" }),
		"upstream_proxy":     mutate(func(c *Config) { c.UpstreamProxy = "socks5://" }),
		"exclude_cidrs":      mutate(func(c *Config) { c.ExcludeCIDRs = []string{"10.0.0.1"} }),
		"mtu":                mutate(func(c *Config) { c.MTU = 100 }),
		"log_level":          mutate(func(c *Config) { c.LogLevel = "verbose" }),
		"quota_poll":         mutate(func(c *Config) { c.QuotaPollMinutes = 0 }),
		"tls_split":          mutate(func(c *Config) { c.TLSSplit = "middle" }),
		"keepalive":          mutate(func(c *Config) { c.KeepaliveProfile = "fast" }),
		"http_proxy_port":    mutate(func(c *Config) { c.HTTPProxyPort = 0 }),
		"http_proxy_port_hi": mutate(func(c *Config) { c.HTTPProxyPort = 65536 }),
	}
	for name, cfg := range cases {
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestValidateUpstreamProxy(t *testing.T) {
	valid := []string{
		"socks5://127.0.0.1:1080",
		"socks5://127.0.0.1", // port optional (default 1080)
		"SOCKS5://127.0.0.1", // scheme is case-insensitive
		"socks5h://proxy.example.net:1080",
		"socks5h://proxy.example.net",
		"socks5://user:pass@127.0.0.1:1080", // credentials allowed
		"socks5://us%40er:p%40ss@127.0.0.1", // URL-escaped credentials
		"http://127.0.0.1:8080",
		"http://proxy.example.net", // port optional (default 80)
		"HTTP://proxy.example.net",
		"http://user:pass@proxy.example.net:3128",
		"socks5://[::1]:1080",
		"socks5://[::1]",
	}
	for _, raw := range valid {
		cfg := DefaultConfig()
		cfg.UpstreamProxy = raw
		if err := cfg.Validate(); err != nil {
			t.Errorf("%q: unexpected validation error: %v", raw, err)
		}
	}

	invalid := []string{
		"https://127.0.0.1:1080",   // wrong scheme
		"socks4://127.0.0.1:1080",  // unsupported scheme
		"SOCKS4://127.0.0.1",       // unsupported scheme, any case
		"socks://127.0.0.1",        // not a supported alias
		"",                         // handled as "not set" upstream; see below
		"not a url",                // unparseable / no scheme
		"%zz",                      // unparseable escape
		"socks5://",                // missing host
		"http://",                  // missing host
		"socks5://user:secret@",    // missing host with credentials
		"socks5://127.0.0.1:99999", // port out of range
	}
	for _, raw := range invalid {
		if raw == "" {
			continue // empty means "no proxy configured"
		}
		cfg := DefaultConfig()
		cfg.UpstreamProxy = raw
		err := cfg.Validate()
		if err == nil {
			t.Errorf("%q: expected a validation error", raw)
			continue
		}
		if strings.Contains(err.Error(), "secret") {
			t.Errorf("%q: validation error leaks credentials: %v", raw, err)
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

func TestV040SettingsDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.TLSSplit != "off" {
		t.Errorf("TLSSplit = %q, want off", cfg.TLSSplit)
	}
	if cfg.HTTPProxy {
		t.Error("HTTPProxy should default to false")
	}
	if cfg.HTTPProxyPort != 8080 {
		t.Errorf("HTTPProxyPort = %d, want 8080", cfg.HTTPProxyPort)
	}
	if !cfg.SolveChallenges {
		t.Error("SolveChallenges should default to true")
	}
	if cfg.KeepaliveProfile != "standard" {
		t.Errorf("KeepaliveProfile = %q, want standard", cfg.KeepaliveProfile)
	}
}

func TestV040SettingsConfigFile(t *testing.T) {
	paths := testPaths(t)
	content := "tls_split = \"sni\"\nhttp_proxy = true\nhttp_proxy_port = 3128\n" +
		"solve_challenges = false\nkeepalive_profile = \"slow\"\n"
	if err := os.WriteFile(paths.Config, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg := st.Config()
	if cfg.TLSSplit != "sni" || !cfg.HTTPProxy || cfg.HTTPProxyPort != 3128 || cfg.SolveChallenges || cfg.KeepaliveProfile != "slow" {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestV040SettingsConfigFileKeepsDefaults(t *testing.T) {
	paths := testPaths(t)
	if err := os.WriteFile(paths.Config, []byte("socks_port = 1081\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg := st.Config()
	want := DefaultConfig()
	if cfg.TLSSplit != want.TLSSplit || cfg.HTTPProxy != want.HTTPProxy ||
		cfg.HTTPProxyPort != want.HTTPProxyPort || cfg.SolveChallenges != want.SolveChallenges ||
		cfg.KeepaliveProfile != want.KeepaliveProfile {
		t.Fatalf("config = %+v, want the v0.4.0 defaults %+v", cfg, want)
	}
}

func TestV040SettingsRoundTrip(t *testing.T) {
	paths := testPaths(t)
	st, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range [][2]string{
		{"tls_split", "first-byte"},
		{"http_proxy", "on"},
		{"http_proxy_port", "3128"},
		{"solve_challenges", "off"},
		{"keepalive_profile", "slow"},
	} {
		if err := st.SetOverride(kv[0], kv[1]); err != nil {
			t.Fatalf("SetOverride(%q, %q): %v", kv[0], kv[1], err)
		}
	}

	cfg := st.EffectiveConfig()
	if cfg.TLSSplit != "first-byte" || !cfg.HTTPProxy || cfg.HTTPProxyPort != 3128 || cfg.SolveChallenges || cfg.KeepaliveProfile != "slow" {
		t.Fatalf("effective config = %+v", cfg)
	}
	admin := st.Config()
	if admin.TLSSplit != "off" || admin.HTTPProxy || admin.HTTPProxyPort != 8080 || !admin.SolveChallenges || admin.KeepaliveProfile != "standard" {
		t.Fatalf("admin config must stay untouched: %+v", admin)
	}

	reopened, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	rcfg := reopened.EffectiveConfig()
	if rcfg.TLSSplit != "first-byte" || !rcfg.HTTPProxy || rcfg.HTTPProxyPort != 3128 || rcfg.SolveChallenges || rcfg.KeepaliveProfile != "slow" {
		t.Fatalf("overrides did not survive reopen: %+v", rcfg)
	}
	want := []string{"http_proxy", "http_proxy_port", "keepalive_profile", "solve_challenges", "tls_split"}
	if got := reopened.OverriddenKeys(); !reflect.DeepEqual(got, want) {
		t.Fatalf("OverriddenKeys = %v, want %v", got, want)
	}

	for _, key := range want {
		if err := reopened.ClearOverride(key); err != nil {
			t.Fatalf("ClearOverride(%q): %v", key, err)
		}
	}
	if got := reopened.OverriddenKeys(); len(got) != 0 {
		t.Fatalf("overrides after reset = %v, want none", got)
	}
	def := reopened.EffectiveConfig()
	if def.TLSSplit != "off" || def.HTTPProxy || def.HTTPProxyPort != 8080 || !def.SolveChallenges || def.KeepaliveProfile != "standard" {
		t.Fatalf("reset did not restore the defaults: %+v", def)
	}
}

func TestV040SetOverrideRejects(t *testing.T) {
	paths := testPaths(t)
	st, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ key, value string }{
		{"tls_split", "middle"},
		{"tls_split", ""},
		{"keepalive_profile", "fast"},
		{"keepalive_profile", ""},
		{"http_proxy_port", "0"},
		{"http_proxy_port", "65536"},
		{"http_proxy_port", "abc"},
		{"http_proxy", "maybe"},
		{"solve_challenges", "maybe"},
	} {
		if err := st.SetOverride(tc.key, tc.value); err == nil {
			t.Errorf("SetOverride(%q, %q): expected error", tc.key, tc.value)
		}
	}
	// A failed set must not persist anything.
	if keys := st.OverriddenKeys(); len(keys) != 0 {
		t.Fatalf("overrides after failures = %v", keys)
	}
}
