// Package prefs loads and saves the per-user GUI preferences (theme, accent,
// window geometry, autostart and the one-time onboarding/close-hint flags).
//
// The file is gui.toml: %AppData%\kumiho\gui.toml on Windows and
// os.UserConfigDir()/kumiho/gui.toml elsewhere, following the TUI's per-user
// convention. KUMIHO_GUI_PREFS overrides the path on every platform for
// development, tests and the web harness.
//
// A missing file yields the defaults with a nil error (first run; the caller
// decides onboarding). A malformed file is an error. Unknown theme and accent
// values fall back to their defaults.
package prefs

import (
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"

	"github.com/benyamin-git/kumiho/internal/gui/theme"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// Window sizing (D24): the default and minimum window sizes. A zero-sized
// saved window means "no saved geometry"; T1.7 consumes these constants.
const (
	DefaultWidth  = 1080
	DefaultHeight = 720
	MinWidth      = 900
	MinHeight     = 600
)

const (
	// envPrefs overrides the preferences path on every platform.
	envPrefs = "KUMIHO_GUI_PREFS"

	// themeLight and themeDark are the first-run defaults; OLED is opt-in.
	themeLight = string(theme.Light)
	themeDark  = string(theme.Dark)

	// defaultAccent is the house default accent.
	defaultAccent = "blue"
)

// Prefs mirrors gui.toml (D31). Window geometry is returned as saved, with no
// clamping; the zero value means "no saved geometry".
type Prefs struct {
	Theme  string `toml:"theme"`
	Accent string `toml:"accent"`
	Window struct {
		X         int  `toml:"x"`
		Y         int  `toml:"y"`
		Width     int  `toml:"width"`
		Height    int  `toml:"height"`
		Maximized bool `toml:"maximized"`
	} `toml:"window"`
	Autostart      bool `toml:"autostart"`
	Onboarded      bool `toml:"onboarded"`
	CloseHintShown bool `toml:"close_hint_shown"`
}

// osThemeDefault returns "light" or "dark" from the OS theme preference. It
// is a variable so tests can force either value without touching the
// registry.
var osThemeDefault = platformThemeDefault

// Load reads the per-user preferences. A missing file yields the defaults
// with a nil error (first run; the caller decides onboarding). Unknown theme
// and accent values fall back to their defaults. A malformed file is an
// error.
func Load() (Prefs, error) {
	path, err := resolvePath()
	if err != nil {
		return Prefs{}, err
	}
	p := defaults()
	if _, err := toml.DecodeFile(path, &p); err != nil {
		if os.IsNotExist(err) {
			return p, nil
		}
		return Prefs{}, fmt.Errorf("gui prefs %s: %w", path, err)
	}
	return normalize(p), nil
}

// Save writes the preferences atomically (temp file + rename) with 0644
// permissions, creating parent directories (0700) as needed.
func Save(p Prefs) error {
	path, err := resolvePath()
	if err != nil {
		return err
	}
	data, err := toml.Marshal(p)
	if err != nil {
		return fmt.Errorf("gui prefs: %w", err)
	}
	return settings.WriteFileAtomic(path, data, 0o644)
}

// defaults returns the first-run preferences: the OS theme, the house accent
// and autostart on (D11).
func defaults() Prefs {
	return Prefs{
		Theme:     osThemeDefault(),
		Accent:    defaultAccent,
		Autostart: true,
	}
}

// normalize falls unknown theme and accent values back to their defaults.
func normalize(p Prefs) Prefs {
	switch theme.Theme(p.Theme) {
	case theme.Light, theme.Dark, theme.OLED:
	default:
		p.Theme = osThemeDefault()
	}
	if !knownAccent(p.Accent) {
		p.Accent = defaultAccent
	}
	return p
}

// knownAccent reports whether name is one of the six house accents.
func knownAccent(name string) bool {
	for _, a := range theme.Accents {
		if a.Name == name {
			return true
		}
	}
	return false
}

// resolvePath returns the preferences file path: the KUMIHO_GUI_PREFS
// override when set, otherwise the per-OS default.
func resolvePath() (string, error) {
	if v := os.Getenv(envPrefs); v != "" {
		return v, nil
	}
	if path := defaultPrefsPath(); path != "" {
		return path, nil
	}
	return "", errors.New("gui prefs: cannot determine the user config directory")
}
