package theme

import (
	"fmt"
	"strings"
	"testing"
)

// The values pinned in this file are the house-style reference's documented
// palette (design skill, references/house-style.md): the twelve table
// primaries, the neutral table including both ladder endpoints and the fixed
// OLED ladder, and the status table. Values the reference leaves implicit
// (secondary/tertiary families, interpolated light/dark ladder middle steps)
// were produced by the same documented factors in an independent reference
// implementation and are pinned so factor drift fails here.

func TestThemeConstants(t *testing.T) {
	if Light != "light" || Dark != "dark" || OLED != "oled" {
		t.Fatalf("theme values = %q, %q, %q; want %q, %q, %q",
			Light, Dark, OLED, "light", "dark", "oled")
	}
}

func TestAccentsTable(t *testing.T) {
	want := []Accent{
		{Name: "blue", Hue: 255, Chroma: 0.21},
		{Name: "teal", Hue: 195, Chroma: 0.16},
		{Name: "green", Hue: 145, Chroma: 0.19},
		{Name: "orange", Hue: 45, Chroma: 0.21},
		{Name: "rose", Hue: 15, Chroma: 0.21},
		{Name: "violet", Hue: 300, Chroma: 0.20},
	}
	if len(Accents) != len(want) {
		t.Fatalf("Accents has %d entries; want %d", len(Accents), len(want))
	}
	for i := range want {
		if Accents[i] != want[i] {
			t.Errorf("Accents[%d] = %+v; want %+v", i, Accents[i], want[i])
		}
	}
}

// TestDocumentedPrimaries checks the house table's "Dark primary" and
// "Light primary" columns for all six accents.
func TestDocumentedPrimaries(t *testing.T) {
	cases := []struct {
		theme  Theme
		accent string
		want   string
	}{
		{Light, "blue", "#35639c"},
		{Light, "teal", "#127070"},
		{Light, "green", "#3c703f"},
		{Light, "orange", "#924c2b"},
		{Light, "rose", "#94464e"},
		{Light, "violet", "#7638c0"},
		{Dark, "blue", "#7daeec"},
		{Dark, "teal", "#69bbba"},
		{Dark, "green", "#4bc957"},
		{Dark, "orange", "#e29572"},
		{Dark, "rose", "#e58f95"},
		{Dark, "violet", "#b49ce1"},
	}
	for _, tc := range cases {
		vars := cssVars(t, tc.theme, tc.accent)
		if got := vars["--primary"]; got != tc.want {
			t.Errorf("CSS(%s, %s) --primary = %s; want %s", tc.theme, tc.accent, got, tc.want)
		}
	}
}

// TestNeutralsAndContainers checks the neutral table (background, on-surface,
// outline) and the full five-step container ladder for each theme. The
// light/dark middle steps are the OKLCH interpolation of the table's
// endpoints; OLED's ladder is the fixed house ladder.
func TestNeutralsAndContainers(t *testing.T) {
	cases := []struct {
		theme    Theme
		bg       string
		onS      string
		outline  string
		containr [5]string
	}{
		{Light, "#fffbfe", "#1c1b1f", "#79747e",
			[5]string{"#ffffff", "#f9f7f9", "#f2eff4", "#ece8ee", "#e6e0e9"}},
		{Dark, "#1c1b1f", "#e6e1e5", "#938f99",
			[5]string{"#0f0d13", "#18161c", "#222026", "#2c2a30", "#36343b"}},
		{OLED, "#000000", "#e8e6ea", "#6f6a74",
			[5]string{"#000000", "#0a0a0b", "#101012", "#17171a", "#1f1f23"}},
	}
	for _, tc := range cases {
		vars := cssVars(t, tc.theme, "blue")
		if got := vars["--background"]; got != tc.bg {
			t.Errorf("CSS(%s, blue) --background = %s; want %s", tc.theme, got, tc.bg)
		}
		if got := vars["--on-surface"]; got != tc.onS {
			t.Errorf("CSS(%s, blue) --on-surface = %s; want %s", tc.theme, got, tc.onS)
		}
		if got := vars["--outline"]; got != tc.outline {
			t.Errorf("CSS(%s, blue) --outline = %s; want %s", tc.theme, got, tc.outline)
		}
		for i, want := range tc.containr {
			name := fmt.Sprintf("--surface-container-%d", i+1)
			if got := vars[name]; got != want {
				t.Errorf("CSS(%s, blue) %s = %s; want %s", tc.theme, name, got, want)
			}
		}
	}
}

// TestStatusColors checks the fixed status table for all three themes.
func TestStatusColors(t *testing.T) {
	cases := []struct {
		theme   Theme
		profit  string
		loss    string
		warning string
	}{
		{Light, "#146c2e", "#b3261e", "#7a5900"},
		{Dark, "#6dd58c", "#f2b8b5", "#ffdf9e"},
		{OLED, "#6dd58c", "#ffb4ab", "#ffdf9e"},
	}
	for _, tc := range cases {
		vars := cssVars(t, tc.theme, "blue")
		if got := vars["--profit"]; got != tc.profit {
			t.Errorf("CSS(%s, blue) --profit = %s; want %s", tc.theme, got, tc.profit)
		}
		if got := vars["--loss"]; got != tc.loss {
			t.Errorf("CSS(%s, blue) --loss = %s; want %s", tc.theme, got, tc.loss)
		}
		if got := vars["--warning"]; got != tc.warning {
			t.Errorf("CSS(%s, blue) --warning = %s; want %s", tc.theme, got, tc.warning)
		}
	}
}

// TestCSSBlock pins the complete emitted block for the default accent in all
// three themes: the declaration order, the exact-value format and the
// color-scheme declaration are frozen by the controller ruling.
func TestCSSBlock(t *testing.T) {
	cases := []struct {
		theme Theme
		want  string
	}{
		{Light, ":root{color-scheme:light;--primary:#35639c;--on-primary:#ffffff;--primary-container:#d4e6fd;--on-primary-container:#0b2039;--surface-tint:#35639c;--inverse-primary:#a8ccfa;--secondary:#486180;--on-secondary:#ffffff;--secondary-container:#d6e5f8;--on-secondary-container:#071c36;--tertiary:#754f86;--on-tertiary:#ffffff;--tertiary-container:#eeddf5;--on-tertiary-container:#2c0c39;--background:#fffbfe;--on-surface:#1c1b1f;--outline:#79747e;--surface-container-1:#ffffff;--surface-container-2:#f9f7f9;--surface-container-3:#f2eff4;--surface-container-4:#ece8ee;--surface-container-5:#e6e0e9;--profit:#146c2e;--loss:#b3261e;--warning:#7a5900}"},
		{Dark, ":root{color-scheme:dark;--primary:#7daeec;--on-primary:#203550;--primary-container:#1b4a83;--on-primary-container:#d4e6fd;--surface-tint:#7daeec;--inverse-primary:#35639c;--secondary:#adcaef;--on-secondary:#1b324e;--secondary-container:#334966;--on-secondary-container:#d6e5f8;--tertiary:#e4b5f9;--on-tertiary:#432151;--tertiary-container:#5c376c;--on-tertiary-container:#eeddf5;--background:#1c1b1f;--on-surface:#e6e1e5;--outline:#938f99;--surface-container-1:#0f0d13;--surface-container-2:#18161c;--surface-container-3:#222026;--surface-container-4:#2c2a30;--surface-container-5:#36343b;--profit:#6dd58c;--loss:#f2b8b5;--warning:#ffdf9e}"},
		{OLED, ":root{color-scheme:dark;--primary:#7daeec;--on-primary:#203550;--primary-container:#1b4a83;--on-primary-container:#d4e6fd;--surface-tint:#7daeec;--inverse-primary:#35639c;--secondary:#adcaef;--on-secondary:#1b324e;--secondary-container:#334966;--on-secondary-container:#d6e5f8;--tertiary:#e4b5f9;--on-tertiary:#432151;--tertiary-container:#5c376c;--on-tertiary-container:#eeddf5;--background:#000000;--on-surface:#e8e6ea;--outline:#6f6a74;--surface-container-1:#000000;--surface-container-2:#0a0a0b;--surface-container-3:#101012;--surface-container-4:#17171a;--surface-container-5:#1f1f23;--profit:#6dd58c;--loss:#ffb4ab;--warning:#ffdf9e}"},
	}
	for _, tc := range cases {
		got, err := CSS(tc.theme, "blue")
		if err != nil {
			t.Fatalf("CSS(%s, blue) error = %v", tc.theme, err)
		}
		if got != tc.want {
			t.Errorf("CSS(%s, blue) =\n%s\nwant\n%s", tc.theme, got, tc.want)
		}
	}
}

// TestGamutClipHalvesChroma covers two roles whose raw chroma is outside the
// sRGB gamut at their role lightness, so the first-in-gamut halving rule has
// to be exercised. The assertion ladder (raw out, first halving still out,
// second halving in) proves the kept value is the first in-gamut chroma and
// not a boundary search.
//
//  1. Light primary family, on-primary-container, blue accent: L .24,
//     C .21×1.05 = .2205, hue 255. At that lightness the linear red and green
//     channels of the raw colour are negative; C .11025 still has negative
//     red; C .055125 is the first in-gamut value. Result #0b2039.
//  2. Dark primary family (serves OLED), primary-container, teal accent:
//     L .41, C .16×1.02 = .1632, hue 195. Raw red channel is negative;
//     C .0816 is still out (negative red); C .0408 is in gamut. Result #2e5151.
func TestGamutClipHalvesChroma(t *testing.T) {
	cases := []struct {
		name string
		raw  oklch
		want string
	}{
		{"light blue on-primary-container", oklch{L: 0.24, C: 0.21 * 1.05, H: 255}, "#0b2039"},
		{"dark teal primary-container", oklch{L: 0.41, C: 0.16 * 1.02, H: 195}, "#2e5151"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			half := oklch{L: tc.raw.L, C: tc.raw.C / 2, H: tc.raw.H}
			quarter := oklch{L: tc.raw.L, C: tc.raw.C / 4, H: tc.raw.H}
			if tc.raw.inGamut() {
				t.Fatalf("raw %+v is in gamut; the case is not a clip case", tc.raw)
			}
			if half.inGamut() {
				t.Fatalf("first halving %+v is in gamut; the case does not need two halvings", half)
			}
			if !quarter.inGamut() {
				t.Fatalf("second halving %+v is out of gamut; want first in-gamut value", quarter)
			}
			if got := tc.raw.hex(); got != tc.want {
				t.Errorf("oklch hex = %s; want %s", got, tc.want)
			}
		})
	}
}

// TestCSSErrors checks that unknown themes and accents are rejected: the
// theme and accent names are exact lowercase house values.
func TestCSSErrors(t *testing.T) {
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
		if _, err := CSS(tc.theme, tc.accent); err == nil {
			t.Errorf("CSS(%q, %q) error = nil; want error", tc.theme, tc.accent)
		}
	}
}

// cssVars calls CSS and splits the emitted block into its custom properties.
func cssVars(t *testing.T, theme Theme, accent string) map[string]string {
	t.Helper()
	block, err := CSS(theme, accent)
	if err != nil {
		t.Fatalf("CSS(%s, %s) error = %v", theme, accent, err)
	}
	if !strings.HasPrefix(block, ":root{") || !strings.HasSuffix(block, "}") {
		t.Fatalf("CSS(%s, %s) = %q; want a :root{...} block", theme, accent, block)
	}
	vars := make(map[string]string)
	body := block[len(":root{") : len(block)-1]
	for _, decl := range strings.Split(body, ";") {
		name, value, ok := strings.Cut(decl, ":")
		if !ok {
			t.Fatalf("CSS(%s, %s) declaration %q has no colon", theme, accent, decl)
		}
		vars[name] = value
	}
	return vars
}
