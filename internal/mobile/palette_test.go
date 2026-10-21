package mobile

import (
	"encoding/json"
	"regexp"
	"testing"
)

func TestPaletteReturnsJSONObject(t *testing.T) {
	out, err := Palette("dark", "blue")
	if err != nil {
		t.Fatalf("Palette: %v", err)
	}
	var roles map[string]string
	if err := json.Unmarshal([]byte(out), &roles); err != nil {
		t.Fatalf("Palette output is not a JSON object: %v\n%s", err, out)
	}
	hex := regexp.MustCompile(`^#[0-9a-f]{6}$`)
	for _, key := range []string{
		"primary", "on-primary", "primary-container", "secondary", "tertiary",
		"background", "on-surface", "outline", "surface-container-1",
		"surface-container-5", "profit", "loss", "warning",
	} {
		v, ok := roles[key]
		if !ok {
			t.Errorf("palette lacks %q", key)
			continue
		}
		if !hex.MatchString(v) {
			t.Errorf("%s = %q, want a lowercase hex colour", key, v)
		}
	}
}

func TestPaletteAllThemesAndAccents(t *testing.T) {
	for _, themeName := range []string{"light", "dark", "oled"} {
		for _, accent := range []string{"blue", "teal", "green", "orange", "rose", "violet"} {
			if _, err := Palette(themeName, accent); err != nil {
				t.Errorf("Palette(%q, %q): %v", themeName, accent, err)
			}
		}
	}
}

func TestPaletteWorksWithoutStart(t *testing.T) {
	if err := Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := Palette("oled", "rose"); err != nil {
		t.Errorf("Palette before Start: %v", err)
	}
}

func TestPaletteRejectsUnknownValues(t *testing.T) {
	for _, tc := range []struct{ theme, accent string }{
		{"neon", "blue"},
		{"", "blue"},
		{"dark", "mauve"},
		{"dark", ""},
	} {
		if _, err := Palette(tc.theme, tc.accent); err == nil {
			t.Errorf("Palette(%q, %q) succeeded, want an error", tc.theme, tc.accent)
		}
	}
}

func TestVersionNonEmpty(t *testing.T) {
	if got := Version(); got == "" {
		t.Fatal("Version is empty")
	}
}
