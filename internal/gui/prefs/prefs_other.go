//go:build !windows

package prefs

import (
	"os"
	"path/filepath"
)

// defaultPrefsPath returns os.UserConfigDir()/kumiho/gui.toml, or "" when the
// user config directory cannot be determined.
func defaultPrefsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "kumiho", "gui.toml")
}

// platformThemeDefault returns the dark theme: the dev harness and Linux
// tests have no OS theme source (the harness overrides via KUMIHO_GUI_PREFS
// and its own theme switch anyway).
func platformThemeDefault() string {
	return themeDark
}
