//go:build windows

package windows

import (
	"os"
	"path/filepath"

	"github.com/benyamin-git/kumiho/internal/settings"
)

// windowsPaths returns the machine-wide paths under %ProgramData%\kumiho:
// config.toml, state.json and tokens.json, with the runtime dir being the
// kumiho directory itself (D19). KUMIHO_* overrides apply.
func windowsPaths() settings.Paths {
	return settings.ApplyEnvOverrides(settings.Paths{
		Config:  filepath.Join(os.Getenv("ProgramData"), "kumiho", "config.toml"),
		State:   filepath.Join(os.Getenv("ProgramData"), "kumiho", "state.json"),
		Tokens:  filepath.Join(os.Getenv("ProgramData"), "kumiho", "tokens.json"),
		Runtime: filepath.Join(os.Getenv("ProgramData"), "kumiho"),
	})
}
