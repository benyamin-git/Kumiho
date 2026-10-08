package tui

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/lipgloss"
)

// prefs mirrors ~/.config/kumiho/tui.toml (PLAN.md §8).
type prefs struct {
	// Theme is "dark" (default, 256-color fox accents) or "mono" (no colors,
	// for terminals without color support).
	Theme string `toml:"theme"`
}

// loadPrefs reads the per-user TUI preferences. A missing or unreadable file
// falls back to the defaults.
func loadPrefs() prefs {
	p := prefs{Theme: "dark"}
	path := os.Getenv("KUMIHO_TUI_CONFIG")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		path = filepath.Join(home, ".config", "kumiho", "tui.toml")
	}
	if _, err := os.Stat(path); err != nil {
		return p
	}
	if _, err := toml.DecodeFile(path, &p); err != nil {
		return prefs{Theme: "dark"}
	}
	if p.Theme == "" {
		p.Theme = "dark"
	}
	return p
}

// applyTheme rebinds the package styles. Unknown names fall back to dark.
func applyTheme(theme string) {
	switch theme {
	case "mono":
		titleStyle = lipgloss.NewStyle().Bold(true)
		okStyle = lipgloss.NewStyle()
		warnStyle = lipgloss.NewStyle().Bold(true)
		errStyle = lipgloss.NewStyle().Bold(true)
		dimStyle = lipgloss.NewStyle()
	default:
		titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("208"))
		okStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
		warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		errStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
		dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	}
}
