//go:build !windows

package tui

import (
	"os"
	"path/filepath"
)

// defaultPrefsPath returns ~/.config/kumiho/tui.toml, or "" when the home
// directory cannot be determined.
func defaultPrefsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "kumiho", "tui.toml")
}
