package mobile

import (
	"encoding/json"
	"os"
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
		"data-line", "data-fill", "data-grid", "glow",
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

// TestWritePaletteFixture regenerates the Android unit-test fixture from the
// real Go palette. It is skipped unless KUMIHO_UPDATE_PALETTE_FIXTURE=1.
func TestWritePaletteFixture(t *testing.T) {
	if os.Getenv("KUMIHO_UPDATE_PALETTE_FIXTURE") != "1" {
		t.Skip("set KUMIHO_UPDATE_PALETTE_FIXTURE=1 to regenerate")
	}
	fixture := map[string]any{
		"_comment": "Generated from the real Go palette: each (theme, accent) pair is the JSON " +
			"returned by mobile.Palette(theme, accent), which wraps internal/gui/theme. " +
			"Themes: light, dark, oled. Accents: blue, teal, green, orange, rose, violet. " +
			"Roles are the 29 frozen house roles. Regenerate with " +
			"KUMIHO_UPDATE_PALETTE_FIXTURE=1 go test ./mobile -run TestWritePaletteFixture; " +
			"do not hand-edit this file.",
	}
	for _, themeName := range []string{"light", "dark", "oled"} {
		pairs := map[string]map[string]string{}
		for _, accent := range []string{"blue", "teal", "green", "orange", "rose", "violet"} {
			out, err := Palette(themeName, accent)
			if err != nil {
				t.Fatalf("Palette(%q, %q): %v", themeName, accent, err)
			}
			var roles map[string]string
			if err := json.Unmarshal([]byte(out), &roles); err != nil {
				t.Fatalf("Palette(%q, %q) is not a JSON object: %v", themeName, accent, err)
			}
			pairs[accent] = roles
		}
		fixture[themeName] = pairs
	}
	b, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	b = append(b, '\n')
	if err := os.WriteFile("../android/app/src/test/resources/palette.json", b, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
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
