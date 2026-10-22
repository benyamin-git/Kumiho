package theme

import "testing"

// TestPaletteKeySet checks that every theme × accent returns exactly the
// cssOrder key set, without the -- prefix.
func TestPaletteKeySet(t *testing.T) {
	for _, th := range []Theme{Light, Dark, OLED} {
		for _, a := range Accents {
			v, err := Palette(th, a.Name)
			if err != nil {
				t.Fatalf("Palette(%s, %s) error = %v", th, a.Name, err)
			}
			if len(v) != len(cssOrder) {
				t.Errorf("Palette(%s, %s) has %d keys; want %d",
					th, a.Name, len(v), len(cssOrder))
			}
			for _, name := range cssOrder {
				if _, ok := v[name]; !ok {
					t.Errorf("Palette(%s, %s) missing key %q", th, a.Name, name)
				}
			}
		}
	}
}

// TestPaletteKeySetExact checks there are no keys outside cssOrder.
func TestPaletteKeySetExact(t *testing.T) {
	allowed := make(map[string]bool, len(cssOrder))
	for _, name := range cssOrder {
		allowed[name] = true
	}
	for _, th := range []Theme{Light, Dark, OLED} {
		for _, a := range Accents {
			v, err := Palette(th, a.Name)
			if err != nil {
				t.Fatalf("Palette(%s, %s) error = %v", th, a.Name, err)
			}
			for name := range v {
				if !allowed[name] {
					t.Errorf("Palette(%s, %s) has unexpected key %q", th, a.Name, name)
				}
			}
		}
	}
}

// TestPaletteRoleValues spot-checks the documented role values (primary,
// background, the container ladder and the status table) for representative
// theme/accent pairs.
func TestPaletteRoleValues(t *testing.T) {
	cases := []struct {
		theme  Theme
		accent string
		want   map[string]string
	}{
		{Light, "blue", map[string]string{
			"primary":             "#35639c",
			"background":          "#fffbfe",
			"on-surface":          "#1c1b1f",
			"outline":             "#79747e",
			"surface-container-1": "#ffffff",
			"surface-container-5": "#e6e0e9",
			"profit":              "#146c2e",
			"loss":                "#b3261e",
			"warning":             "#7a5900",
		}},
		{Dark, "blue", map[string]string{
			"primary":             "#7daeec",
			"background":          "#1c1b1f",
			"on-surface":          "#e6e1e5",
			"outline":             "#938f99",
			"surface-container-1": "#0f0d13",
			"surface-container-5": "#36343b",
			"profit":              "#6dd58c",
			"loss":                "#f2b8b5",
			"warning":             "#ffdf9e",
		}},
		{OLED, "blue", map[string]string{
			"primary":             "#7daeec",
			"background":          "#000000",
			"on-surface":          "#e8e6ea",
			"outline":             "#6f6a74",
			"surface-container-1": "#000000",
			"surface-container-5": "#1f1f23",
			"profit":              "#6dd58c",
			"loss":                "#ffb4ab",
			"warning":             "#ffdf9e",
		}},
		{Light, "green", map[string]string{"primary": "#3c703f"}},
		{Dark, "teal", map[string]string{"primary": "#69bbba"}},
	}
	for _, tc := range cases {
		v, err := Palette(tc.theme, tc.accent)
		if err != nil {
			t.Fatalf("Palette(%s, %s) error = %v", tc.theme, tc.accent, err)
		}
		for name, want := range tc.want {
			if got := v[name]; got != want {
				t.Errorf("Palette(%s, %s)[%q] = %s; want %s",
					tc.theme, tc.accent, name, got, want)
			}
		}
	}
}

// TestPaletteMatchesCSS checks that Palette returns exactly the custom
// properties CSS emits, for all themes and accents.
func TestPaletteMatchesCSS(t *testing.T) {
	for _, th := range []Theme{Light, Dark, OLED} {
		for _, a := range Accents {
			v, err := Palette(th, a.Name)
			if err != nil {
				t.Fatalf("Palette(%s, %s) error = %v", th, a.Name, err)
			}
			vars := cssVars(t, th, a.Name)
			for _, key := range cssOrder {
				if v[key] != vars["--"+key] {
					t.Errorf("Palette(%s, %s)[%q] = %s; CSS --%s = %s",
						th, a.Name, key, v[key], key, vars["--"+key])
				}
			}
		}
	}
}

// TestPaletteErrors checks that Palette rejects the same unknown themes and
// accents as CSS.
func TestPaletteErrors(t *testing.T) {
	cases := []struct {
		theme  Theme
		accent string
	}{
		{"sepia", "blue"},
		{Theme(""), "blue"},
		{Theme("Light"), "blue"},
		{Light, ""},
		{Light, "pink"},
		{Light, "Blue"},
		{Light, "BLUE"},
	}
	for _, tc := range cases {
		if _, err := Palette(tc.theme, tc.accent); err == nil {
			t.Errorf("Palette(%q, %q) error = nil; want error", tc.theme, tc.accent)
		}
	}
}

// TestPaletteErrorText pins the exact error strings so the bridge stays
// byte-compatible with CSS.
func TestPaletteErrorText(t *testing.T) {
	if _, err := Palette("sepia", "blue"); err == nil || err.Error() != `theme: unknown theme "sepia"` {
		t.Errorf("Palette(sepia, blue) error = %v; want %q", err, `theme: unknown theme "sepia"`)
	}
	if _, err := Palette(Light, "pink"); err == nil || err.Error() != `theme: unknown accent "pink"` {
		t.Errorf("Palette(light, pink) error = %v; want %q", err, `theme: unknown accent "pink"`)
	}
}

// TestPaletteCopySemantics checks that the returned map is a copy: mutating
// it does not affect a later call.
func TestPaletteCopySemantics(t *testing.T) {
	first, err := Palette(Light, "blue")
	if err != nil {
		t.Fatalf("Palette(light, blue) error = %v", err)
	}
	want := first["primary"]
	first["primary"] = "mutated"
	delete(first, "background")

	second, err := Palette(Light, "blue")
	if err != nil {
		t.Fatalf("Palette(light, blue) error = %v", err)
	}
	if got := second["primary"]; got != want {
		t.Errorf("second Palette primary = %s; want %s", got, want)
	}
	if _, ok := second["background"]; !ok {
		t.Errorf("second Palette missing background; mutation leaked")
	}
	if first["primary"] == second["primary"] {
		t.Errorf("returned map is shared; want a copy")
	}
}
