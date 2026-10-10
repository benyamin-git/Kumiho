//go:build !windows

package tui

import (
	"os"
	"path/filepath"
)

// defaultExportDir returns ~/.local/state/kumiho, or "" when the home
// directory cannot be determined.
func defaultExportDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "kumiho")
}
