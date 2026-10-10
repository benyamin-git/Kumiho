//go:build windows

package windows

import (
	"os"
	"path/filepath"

	"github.com/benyamin-git/kumiho/internal/platform/unsupported"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// New returns the Windows provider. It delegates to the not-implemented
// fallback until the Windows cycle lands.
func New() *unsupported.Provider {
	return unsupported.New("windows", windowsPaths())
}

// windowsPaths returns the placeholder paths: %ProgramData%\kumiho for the
// persistent files and a temp dir for the runtime dir (the Windows cycle
// refines both). KUMIHO_* overrides apply.
func windowsPaths() settings.Paths {
	return settings.ApplyEnvOverrides(settings.Paths{
		Config:  filepath.Join(os.Getenv("ProgramData"), "kumiho", "config.toml"),
		State:   filepath.Join(os.Getenv("ProgramData"), "kumiho", "state.json"),
		Tokens:  filepath.Join(os.Getenv("ProgramData"), "kumiho", "tokens.json"),
		Runtime: filepath.Join(os.TempDir(), "kumiho"),
	})
}
