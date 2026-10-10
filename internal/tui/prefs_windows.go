//go:build windows

package tui

import (
	"os"
	"path/filepath"
)

// defaultPrefsPath returns %APPDATA%\kumiho\tui.toml, or "" when the user
// config directory cannot be determined.
func defaultPrefsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "kumiho", "tui.toml")
}
