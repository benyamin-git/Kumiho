package android

import (
	"os"
	"path/filepath"

	"github.com/benyamin-git/kumiho/internal/settings"
)

// defaultPaths returns placeholder paths for New(). It exists so a standalone
// Android build of the daemon still resolves somewhere; the mobile facade
// always calls NewWithPaths with the app-private directories the Kotlin side
// passes in (T5). KUMIHO_* overrides apply.
func defaultPaths() settings.Paths {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = os.TempDir()
	}
	return settings.ApplyEnvOverrides(settings.Paths{
		Config:  filepath.Join(configDir, "kumiho", "config.toml"),
		State:   filepath.Join(configDir, "kumiho", "state.json"),
		Tokens:  filepath.Join(configDir, "kumiho", "tokens.json"),
		Runtime: filepath.Join(os.TempDir(), "kumiho"),
	})
}
