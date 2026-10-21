package mobile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/benyamin-git/kumiho/internal/settings"
)

// paths is the app-private layout Kotlin passes to Start. It mirrors the
// Kotlin EnginePaths object: config/state/logs under filesDir, tokens under
// noBackupFilesDir and the runtime directory under cacheDir.
type paths struct {
	Config  string `json:"config"`
	State   string `json:"state"`
	Tokens  string `json:"tokens"`
	Runtime string `json:"runtime"`
	Logs    string `json:"logs"`
}

// decodePaths parses and validates the paths JSON. Every path must be
// absolute and non-empty: the app owns these directories and a relative path
// would resolve against an arbitrary process working directory.
func decodePaths(raw string) (paths, error) {
	var p paths
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return paths{}, fmt.Errorf("kumiho mobile: decode paths: %w", err)
	}
	fields := []struct {
		name  string
		value string
	}{
		{"config", p.Config},
		{"state", p.State},
		{"tokens", p.Tokens},
		{"runtime", p.Runtime},
		{"logs", p.Logs},
	}
	for _, f := range fields {
		if f.value == "" {
			return paths{}, fmt.Errorf("kumiho mobile: paths.%s is required", f.name)
		}
		if !filepath.IsAbs(f.value) {
			return paths{}, fmt.Errorf("kumiho mobile: paths.%s must be absolute, got %q", f.name, f.value)
		}
	}
	return p, nil
}

// settings returns the settings.Paths view (logs are not part of it).
func (p paths) settings() settings.Paths {
	return settings.Paths{
		Config:  p.Config,
		State:   p.State,
		Tokens:  p.Tokens,
		Runtime: p.Runtime,
	}
}

// prepare creates the app-private directories with 0700 permissions and seeds
// the Android MTU default only when no config exists yet. An existing config,
// including one the user edited, is never rewritten.
func (p paths) prepare() error {
	dirs := []string{
		filepath.Dir(p.Config),
		filepath.Dir(p.State),
		filepath.Dir(p.Tokens),
		p.Runtime,
		p.Logs,
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("kumiho mobile: create %s: %w", dir, err)
		}
	}
	if _, err := os.Stat(p.Config); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("kumiho mobile: stat config: %w", err)
		}
		if err := settings.WriteFileAtomic(p.Config, []byte("mtu = 1500\n"), 0o600); err != nil {
			return fmt.Errorf("kumiho mobile: write default config: %w", err)
		}
	}
	return nil
}
