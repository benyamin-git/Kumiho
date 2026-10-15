//go:build windows

package prefs

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// defaultPrefsPath returns %AppData%\kumiho\gui.toml, or "" when the user
// config directory cannot be determined.
func defaultPrefsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "kumiho", "gui.toml")
}

// platformThemeDefault reads the per-user app theme preference:
// AppsUseLightTheme 0 is dark, 1 is light. A missing value or a read error
// falls back to light, matching the Windows default app mode.
func platformThemeDefault() string {
	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`,
		registry.QUERY_VALUE)
	if err != nil {
		return themeLight
	}
	defer key.Close()
	value, _, err := key.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		return themeLight
	}
	if value == 0 {
		return themeDark
	}
	return themeLight
}
