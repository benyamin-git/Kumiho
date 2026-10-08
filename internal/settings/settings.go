// Package settings loads and persists kumiho configuration and state.
//
// Production paths match PLAN.md §8 and can be overridden with environment
// variables for development and tests:
//
//	KUMIHO_CONFIG      /etc/kumiho/config.toml
//	KUMIHO_STATE       /var/lib/kumiho/state.json
//	KUMIHO_RUNTIME_DIR /run/kumiho
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

// Paths resolves the on-disk locations kumiho uses.
type Paths struct {
	Config  string // admin defaults (optional)
	State   string // persisted runtime state
	Tokens  string // OAuth token store
	Runtime string // runtime directory (socket)
}

const (
	EnvConfig  = "KUMIHO_CONFIG"
	EnvState   = "KUMIHO_STATE"
	EnvTokens  = "KUMIHO_TOKENS"
	EnvRuntime = "KUMIHO_RUNTIME_DIR"
)

// DefaultPaths returns the production paths, applying environment overrides.
func DefaultPaths() Paths {
	p := Paths{
		Config:  "/etc/kumiho/config.toml",
		State:   "/var/lib/kumiho/state.json",
		Tokens:  "/var/lib/kumiho/tokens.json",
		Runtime: "/run/kumiho",
	}
	if v := os.Getenv(EnvConfig); v != "" {
		p.Config = v
	}
	if v := os.Getenv(EnvState); v != "" {
		p.State = v
	}
	if v := os.Getenv(EnvTokens); v != "" {
		p.Tokens = v
	}
	if v := os.Getenv(EnvRuntime); v != "" {
		p.Runtime = v
	}
	return p
}

// SocketPath returns the daemon control socket path.
func (p Paths) SocketPath() string {
	return filepath.Join(p.Runtime, "control.sock")
}

// Config mirrors /etc/kumiho/config.toml (PLAN.md §8).
type Config struct {
	SocksBind        string   `toml:"socks_bind"`
	SocksPort        int      `toml:"socks_port"`
	ProxyOnly        bool     `toml:"proxy_only"`
	KillSwitch       string   `toml:"kill_switch"` // last|on|off
	Autoconnect      string   `toml:"autoconnect"` // last|on|off
	ExitCheck        bool     `toml:"exit_check"`
	DoHProvider      string   `toml:"doh_provider"` // automatic|cloudflare|google|quad9|off
	DNSUpstream      string   `toml:"dns_upstream"`
	EdgeAddress      string   `toml:"edge_address"`
	UpstreamProxy    string   `toml:"upstream_proxy"`
	ExcludeCIDRs     []string `toml:"exclude_cidrs"`
	AllowLAN         bool     `toml:"allow_lan"`
	MTU              int      `toml:"mtu"`
	LogLevel         string   `toml:"log_level"`
	RedactTargets    bool     `toml:"redact_targets"`
	QuotaPollMinutes int      `toml:"quota_poll_minutes"`
}

// DefaultConfig returns the built-in defaults (PLAN.md §8).
func DefaultConfig() Config {
	return Config{
		SocksBind:        "127.0.0.1",
		SocksPort:        1080,
		ProxyOnly:        false,
		KillSwitch:       "last",
		Autoconnect:      "last",
		ExitCheck:        true,
		DoHProvider:      "automatic",
		AllowLAN:         true,
		MTU:              8500,
		LogLevel:         "info",
		RedactTargets:    true,
		QuotaPollMinutes: 15,
	}
}

// Validate checks the config for invalid or out-of-range values.
func (c Config) Validate() error {
	if c.SocksBind != "127.0.0.1" {
		return fmt.Errorf("socks_bind: only 127.0.0.1 is supported in v1, got %q", c.SocksBind)
	}
	if c.SocksPort < 1 || c.SocksPort > 65535 {
		return fmt.Errorf("socks_port: out of range: %d", c.SocksPort)
	}
	if err := validateEnum("kill_switch", c.KillSwitch, "last", "on", "off"); err != nil {
		return err
	}
	if err := validateEnum("autoconnect", c.Autoconnect, "last", "on", "off"); err != nil {
		return err
	}
	if err := validateEnum("doh_provider", c.DoHProvider, "automatic", "cloudflare", "google", "quad9", "off"); err != nil {
		return err
	}
	if c.DNSUpstream != "" && net.ParseIP(c.DNSUpstream) == nil {
		return fmt.Errorf("dns_upstream: not an IP address: %q", c.DNSUpstream)
	}
	if c.EdgeAddress != "" && net.ParseIP(c.EdgeAddress) == nil {
		return fmt.Errorf("edge_address: not an IP address: %q", c.EdgeAddress)
	}
	if c.UpstreamProxy != "" {
		u, err := url.Parse(c.UpstreamProxy)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("upstream_proxy: not a URL: %q", c.UpstreamProxy)
		}
	}
	for _, cidr := range c.ExcludeCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("exclude_cidrs: %q is not a CIDR", cidr)
		}
	}
	if c.MTU < 576 || c.MTU > 65535 {
		return fmt.Errorf("mtu: out of range: %d", c.MTU)
	}
	if err := validateEnum("log_level", c.LogLevel, "debug", "info", "warn", "error"); err != nil {
		return err
	}
	if c.QuotaPollMinutes < 1 || c.QuotaPollMinutes > 24*60 {
		return fmt.Errorf("quota_poll_minutes: out of range: %d", c.QuotaPollMinutes)
	}
	return nil
}

func validateEnum(key, value string, allowed ...string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return fmt.Errorf("%s: %q is not one of %s", key, value, strings.Join(allowed, "|"))
}

// State is persisted to /var/lib/kumiho/state.json (PLAN.md §8). Pointer
// fields distinguish "never set" (apply config or first-install defaults)
// from an explicit false. The *override* fields carry runtime settings
// changed from the TUI/CLI on top of the admin config.
type State struct {
	SelectedLocation string         `json:"selected_location,omitempty"`
	SelectedCity     string         `json:"selected_city,omitempty"`
	SelectedServer   string         `json:"selected_server,omitempty"`
	Email            string         `json:"email,omitempty"`
	Failures         map[string]int `json:"failures,omitempty"`
	KillSwitch       *bool          `json:"kill_switch,omitempty"`
	Autoconnect      *bool          `json:"autoconnect,omitempty"`
	ProxyOnly        *bool          `json:"proxy_only,omitempty"`

	// Runtime setting overrides (PLAN.md §8).
	ExitCheck        *bool     `json:"exit_check,omitempty"`
	LogLevel         *string   `json:"log_level,omitempty"`
	RedactTargets    *bool     `json:"redact_targets,omitempty"`
	DoHProvider      *string   `json:"doh_provider,omitempty"`
	DNSUpstream      *string   `json:"dns_upstream,omitempty"`
	EdgeAddress      *string   `json:"edge_address,omitempty"`
	UpstreamProxy    *string   `json:"upstream_proxy,omitempty"`
	ExcludeCIDRs     *[]string `json:"exclude_cidrs,omitempty"`
	AllowLAN         *bool     `json:"allow_lan,omitempty"`
	MTU              *int      `json:"mtu,omitempty"`
	SocksPort        *int      `json:"socks_port,omitempty"`
	QuotaPollMinutes *int      `json:"quota_poll_minutes,omitempty"`
}

// Store owns the config/state pair and persists state atomically.
type Store struct {
	paths Paths

	mu     sync.Mutex
	config Config
	state  State
}

// Open loads config and state; missing files fall back to defaults.
func Open(paths Paths) (*Store, error) {
	s := &Store{paths: paths}

	cfg := DefaultConfig()
	if _, err := toml.DecodeFile(paths.Config, &cfg); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("config %s: %w", paths.Config, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config %s: %w", paths.Config, err)
	}
	s.config = cfg

	if data, err := os.ReadFile(paths.State); err == nil {
		if err := json.Unmarshal(data, &s.state); err != nil {
			return nil, fmt.Errorf("state %s: %w", paths.State, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("state %s: %w", paths.State, err)
	}
	return s, nil
}

// Paths returns the store's paths.
func (s *Store) Paths() Paths { return s.paths }

// Config returns a copy of the loaded configuration.
func (s *Store) Config() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config
}

// State returns a copy of the persisted state.
func (s *Store) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// UpdateState mutates state and persists it atomically.
func (s *Store) UpdateState(fn func(*State)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.state)
	return s.saveStateLocked()
}

// SaveState persists the current state.
func (s *Store) SaveState() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveStateLocked()
}

func (s *Store) saveStateLocked() error {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return WriteFileAtomic(s.paths.State, data, 0o600)
}

// EffectiveKillSwitch resolves config kill_switch + remembered state.
// First install defaults to ON (PLAN.md §1 #9, confirmed 2026-10-05).
func (s *Store) EffectiveKillSwitch() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.effectiveKillSwitchLocked()
}

// EffectiveAutoconnect resolves config autoconnect + remembered state.
// First install defaults to ON (PLAN.md §1 #14, updated 2026-10-05).
func (s *Store) EffectiveAutoconnect() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.effectiveAutoconnectLocked()
}

func (s *Store) effectiveKillSwitchLocked() bool {
	switch s.config.KillSwitch {
	case "on":
		return true
	case "off":
		return false
	}
	if s.state.KillSwitch != nil {
		return *s.state.KillSwitch
	}
	return true
}

func (s *Store) effectiveAutoconnectLocked() bool {
	switch s.config.Autoconnect {
	case "on":
		return true
	case "off":
		return false
	}
	if s.state.Autoconnect != nil {
		return *s.state.Autoconnect
	}
	return true
}

// EffectiveConfig returns the admin config with persisted runtime overrides
// applied. The daemon reads connection settings through this.
func (s *Store) EffectiveConfig() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.effectiveConfigLocked()
}

func (s *Store) effectiveConfigLocked() Config {
	cfg := s.config
	st := s.state
	if st.ExitCheck != nil {
		cfg.ExitCheck = *st.ExitCheck
	}
	if st.LogLevel != nil {
		cfg.LogLevel = *st.LogLevel
	}
	if st.RedactTargets != nil {
		cfg.RedactTargets = *st.RedactTargets
	}
	if st.DoHProvider != nil {
		cfg.DoHProvider = *st.DoHProvider
	}
	if st.DNSUpstream != nil {
		cfg.DNSUpstream = *st.DNSUpstream
	}
	if st.EdgeAddress != nil {
		cfg.EdgeAddress = *st.EdgeAddress
	}
	if st.UpstreamProxy != nil {
		cfg.UpstreamProxy = *st.UpstreamProxy
	}
	if st.ExcludeCIDRs != nil {
		cfg.ExcludeCIDRs = append([]string(nil), (*st.ExcludeCIDRs)...)
	}
	if st.AllowLAN != nil {
		cfg.AllowLAN = *st.AllowLAN
	}
	if st.MTU != nil {
		cfg.MTU = *st.MTU
	}
	if st.SocksPort != nil {
		cfg.SocksPort = *st.SocksPort
	}
	if st.QuotaPollMinutes != nil {
		cfg.QuotaPollMinutes = *st.QuotaPollMinutes
	}
	return cfg
}

// OverriddenKeys lists setting keys whose effective value comes from a
// persisted runtime override rather than the admin config.
func (s *Store) OverriddenKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var keys []string
	st := s.state
	add := func(key string, set bool) {
		if set {
			keys = append(keys, key)
		}
	}
	add("kill_switch", st.KillSwitch != nil)
	add("autoconnect", st.Autoconnect != nil)
	add("proxy_only", st.ProxyOnly != nil)
	add("exit_check", st.ExitCheck != nil)
	add("log_level", st.LogLevel != nil)
	add("redact_targets", st.RedactTargets != nil)
	add("doh_provider", st.DoHProvider != nil)
	add("dns_upstream", st.DNSUpstream != nil)
	add("edge_address", st.EdgeAddress != nil)
	add("upstream_proxy", st.UpstreamProxy != nil)
	add("exclude_cidrs", st.ExcludeCIDRs != nil)
	add("allow_lan", st.AllowLAN != nil)
	add("mtu", st.MTU != nil)
	add("socks_port", st.SocksPort != nil)
	add("quota_poll_minutes", st.QuotaPollMinutes != nil)
	sort.Strings(keys)
	return keys
}

// ErrUnknownSetting reports a setting key that has no runtime override.
var ErrUnknownSetting = errors.New("unknown setting")

// SetOverride validates and persists one runtime setting override. Boolean
// keys accept on/off/true/false/yes/no; kill_switch and autoconnect also
// accept "last", which pins their current effective value. Connection-scoped
// overrides take effect on the next connect.
func (s *Store) SetOverride(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	value = strings.TrimSpace(value)
	cand := s.effectiveConfigLocked()
	st := s.state

	switch key {
	case "kill_switch", "autoconnect", "proxy_only":
		var v bool
		switch value {
		case "on", "true", "yes":
			v = true
		case "off", "false", "no":
			v = false
		case "last":
			switch key {
			case "kill_switch":
				v = s.effectiveKillSwitchLocked()
			case "autoconnect":
				v = s.effectiveAutoconnectLocked()
			case "proxy_only":
				v = st.ProxyOnly != nil && *st.ProxyOnly
			}
		default:
			return fmt.Errorf("%s: expected on|off|last, got %q", key, value)
		}
		switch key {
		case "kill_switch":
			st.KillSwitch = &v
		case "autoconnect":
			st.Autoconnect = &v
		case "proxy_only":
			st.ProxyOnly = &v
		}
		s.state = st
		return s.saveStateLocked()

	case "exit_check", "redact_targets", "allow_lan":
		v, err := parseOnOff(value)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		switch key {
		case "exit_check":
			cand.ExitCheck = v
			st.ExitCheck = &v
		case "redact_targets":
			cand.RedactTargets = v
			st.RedactTargets = &v
		case "allow_lan":
			cand.AllowLAN = v
			st.AllowLAN = &v
		}

	case "log_level":
		cand.LogLevel = value
		st.LogLevel = &value
	case "doh_provider":
		cand.DoHProvider = value
		st.DoHProvider = &value
	case "dns_upstream":
		cand.DNSUpstream = value
		st.DNSUpstream = &value
	case "edge_address":
		cand.EdgeAddress = value
		st.EdgeAddress = &value
	case "upstream_proxy":
		cand.UpstreamProxy = value
		st.UpstreamProxy = &value
	case "exclude_cidrs":
		var list []string
		for _, part := range strings.Split(value, ",") {
			if p := strings.TrimSpace(part); p != "" {
				list = append(list, p)
			}
		}
		cand.ExcludeCIDRs = list
		st.ExcludeCIDRs = &list

	case "mtu", "socks_port", "quota_poll_minutes":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		switch key {
		case "mtu":
			cand.MTU = n
			st.MTU = &n
		case "socks_port":
			cand.SocksPort = n
			st.SocksPort = &n
		case "quota_poll_minutes":
			cand.QuotaPollMinutes = n
			st.QuotaPollMinutes = &n
		}

	default:
		return ErrUnknownSetting
	}

	if err := cand.Validate(); err != nil {
		return err
	}
	s.state = st
	return s.saveStateLocked()
}

// ClearOverride drops one runtime override so the config value applies again.
func (s *Store) ClearOverride(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state
	switch key {
	case "kill_switch":
		st.KillSwitch = nil
	case "autoconnect":
		st.Autoconnect = nil
	case "proxy_only":
		st.ProxyOnly = nil
	case "exit_check":
		st.ExitCheck = nil
	case "log_level":
		st.LogLevel = nil
	case "redact_targets":
		st.RedactTargets = nil
	case "doh_provider":
		st.DoHProvider = nil
	case "dns_upstream":
		st.DNSUpstream = nil
	case "edge_address":
		st.EdgeAddress = nil
	case "upstream_proxy":
		st.UpstreamProxy = nil
	case "exclude_cidrs":
		st.ExcludeCIDRs = nil
	case "allow_lan":
		st.AllowLAN = nil
	case "mtu":
		st.MTU = nil
	case "socks_port":
		st.SocksPort = nil
	case "quota_poll_minutes":
		st.QuotaPollMinutes = nil
	default:
		return ErrUnknownSetting
	}
	s.state = st
	return s.saveStateLocked()
}

func parseOnOff(value string) (bool, error) {
	switch strings.ToLower(value) {
	case "on", "true", "yes":
		return true, nil
	case "off", "false", "no":
		return false, nil
	}
	return false, fmt.Errorf("expected on|off, got %q", value)
}

// WriteFileAtomic writes data to path via a temp file + rename so a crash
// never leaves a truncated file.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
