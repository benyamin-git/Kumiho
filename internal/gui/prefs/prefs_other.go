//go:build !windows

package prefs

import (
	"os"
	"path/filepath"
	"strings"
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

// platformSystemLanguage parses the POSIX locale variables in precedence
// order. The first non-empty variable decides: a fa or fa_* primary tag is
// Persian, every other tag is English. The dev harness and Linux runs use the
// same rule.
func platformSystemLanguage() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		tag := strings.ToLower(raw)
		if i := strings.IndexAny(tag, ".@"); i >= 0 {
			tag = tag[:i]
		}
		if tag == "fa" || strings.HasPrefix(tag, "fa_") {
			return "fa"
		}
		return "en"
	}
	return "en"
}

// platformThemeDefault returns the dark theme: the dev harness and Linux
// tests have no OS theme source (the harness overrides via KUMIHO_GUI_PREFS
// and its own theme switch anyway).
func platformThemeDefault() string {
	return themeDark
}
