//go:build android

package android

import (
	"os"
	"path/filepath"

	"github.com/benyamin-git/kumiho/internal/platform/unsupported"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// New returns the Android provider. It delegates to the not-implemented
// fallback until the Android cycle lands.
func New() *unsupported.Provider {
	return unsupported.New("android", androidPaths())
}

// androidPaths returns the placeholder paths under the user config dir and
// the temp dir; the Android cycle replaces them with app-private dirs.
// KUMIHO_* overrides apply.
func androidPaths() settings.Paths {
	configDir, _ := os.UserConfigDir()
	return settings.ApplyEnvOverrides(settings.Paths{
		Config:  filepath.Join(configDir, "kumiho", "config.toml"),
		State:   filepath.Join(configDir, "kumiho", "state.json"),
		Tokens:  filepath.Join(configDir, "kumiho", "tokens.json"),
		Runtime: filepath.Join(os.TempDir(), "kumiho"),
	})
}
