//go:build windows

package tui

import (
	"os"
	"path/filepath"
)

// defaultExportDir returns %LOCALAPPDATA%\kumiho\logs, or "" when the user
// cache directory cannot be determined.
func defaultExportDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "kumiho", "logs")
}
