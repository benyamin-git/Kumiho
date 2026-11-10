//go:build windows

package prefs

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
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

var (
	kernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	getUserDefaultUILanguage = kernel32.NewProc("GetUserDefaultUILanguage")
)

// platformSystemLanguage reads the user's Windows UI language. The primary
// language id is the low 10 bits of the LANGID; 0x29 is Persian. Any failure
// or other language maps to English.
func platformSystemLanguage() string {
	id, _, _ := getUserDefaultUILanguage.Call()
	const persian = 0x29
	if uint16(id)&0x3ff == persian {
		return "fa"
	}
	return "en"
}
