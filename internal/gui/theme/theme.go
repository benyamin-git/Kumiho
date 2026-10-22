// Package theme is the GUI's single source of truth for colour: it turns the
// house-style OKLCH palette rules into the :root custom-property block the
// webview injects before first paint. No raw hex lives outside this package.
package theme

import "fmt"

// Theme identifies one of the three house themes.
type Theme string

const (
	// Light is the light theme.
	Light Theme = "light"
	// Dark is the dark theme.
	Dark Theme = "dark"
	// OLED is the true-black opt-in theme; it uses the dark role factors.
	OLED Theme = "oled"
)

// Accent is one of the six house accents: the OKLCH seed hue in degrees and
// the seed chroma every role family derives from.
type Accent struct {
	Name   string
	Hue    float64
	Chroma float64
}

// Accents lists the six house accents in the house table's order.
var Accents = []Accent{
	{Name: "blue", Hue: 255, Chroma: 0.21},
	{Name: "teal", Hue: 195, Chroma: 0.16},
	{Name: "green", Hue: 145, Chroma: 0.19},
	{Name: "orange", Hue: 45, Chroma: 0.21},
	{Name: "rose", Hue: 15, Chroma: 0.21},
	{Name: "violet", Hue: 300, Chroma: 0.20},
}

// Palette returns the validated semantic roles for a theme and accent.
// Unknown themes and accents are errors: the theme values are exactly
// "light", "dark" and "oled", and accent names are the Accents table's exact
// lowercase names. Keys are the custom-property names without the "--"
// prefix. The map is a copy; callers may mutate it without affecting later
// calls.
func Palette(t Theme, accent string) (map[string]string, error) {
	if _, ok := neutrals[t]; !ok {
		return nil, fmt.Errorf("theme: unknown theme %q", t)
	}
	a, ok := accentByName(accent)
	if !ok {
		return nil, fmt.Errorf("theme: unknown accent %q", accent)
	}
	return values(t, a), nil
}

// CSS returns the :root{...} custom-property block for a theme and accent
// name. Unknown themes and accents are errors, exactly as Palette reports.
func CSS(theme Theme, accent string) (string, error) {
	v, err := Palette(theme, accent)
	if err != nil {
		return "", err
	}
	return cssBlock(theme, v), nil
}

// accentByName resolves an exact accent name.
func accentByName(name string) (Accent, bool) {
	for _, a := range Accents {
		if a.Name == name {
			return a, true
		}
	}
	return Accent{}, false
}
