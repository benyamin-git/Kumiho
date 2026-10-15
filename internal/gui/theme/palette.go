package theme

import (
	"fmt"
	"math"
	"strings"
)

// roleSpec is one role of a colour family: the role name plus the OKLCH
// lightness and chroma factor for the light theme and for the dark theme
// (the dark column also serves OLED), exactly as the house-style reference
// documents them.
type roleSpec struct {
	name                string
	lightL, lightFactor float64
	darkL, darkFactor   float64
}

// family applies a seed chroma scale and hue offset before the per-role
// factors: the primary family uses the seed unchanged; secondary scales the
// seed chroma by .28; tertiary scales by .46 and rotates the hue by +60°.
type family struct {
	chromaScale float64
	hueOffset   float64
	roles       []roleSpec
}

var families = []family{
	{1.0, 0, []roleSpec{
		{"primary", 0.4955, 1.0, 0.74, 1.0},
		{"on-primary", 1.0, 0, 0.325, 1.04},
		{"primary-container", 0.918, 0.36, 0.41, 1.02},
		{"on-primary-container", 0.24, 1.05, 0.918, 0.36},
		{"surface-tint", 0.4955, 1.0, 0.74, 1.0},
		{"inverse-primary", 0.835, 0.72, 0.4955, 1.0},
	}},
	{0.28, 0, []roleSpec{
		{"secondary", 0.4859, 1.0, 0.8303, 1.04},
		{"on-secondary", 1.0, 0, 0.3124, 1.01},
		{"secondary-container", 0.9163, 1.02, 0.4005, 0.95},
		{"on-secondary-container", 0.2272, 0.97, 0.9163, 1.02},
	}},
	{0.46, 60, []roleSpec{
		{"tertiary", 0.4904, 1.0, 0.8367, 1.1},
		{"on-tertiary", 1.0, 0, 0.3171, 0.94},
		{"tertiary-container", 0.9184, 0.76, 0.4044, 0.98},
		{"on-tertiary-container", 0.2323, 0.9, 0.9184, 0.76},
	}},
}

// neutrals is the house neutral table. Per the controller ruling the
// neutrals are the table's exact documented hexes — background, on-surface
// and outline are not accent-tinted.
var neutrals = map[Theme]struct {
	background string
	onSurface  string
	outline    string
	// ladderFrom and ladderTo track the light/dark container ladder's
	// endpoints; OLED's ladder is fixed and ignores them.
	ladderFrom string
	ladderTo   string
}{
	Light: {"#fffbfe", "#1c1b1f", "#79747e", "#ffffff", "#e6e0e9"},
	Dark:  {"#1c1b1f", "#e6e1e5", "#938f99", "#0f0d13", "#36343b"},
	OLED:  {"#000000", "#e8e6ea", "#6f6a74", "#000000", "#1f1f23"},
}

// oledLadder is the house table's fixed five-step OLED container ladder.
var oledLadder = [5]string{"#000000", "#0a0a0b", "#101012", "#17171a", "#1f1f23"}

// status is the house status table; status colours are fixed, never tinted.
var status = map[Theme]struct {
	profit  string
	loss    string
	warning string
}{
	Light: {"#146c2e", "#b3261e", "#7a5900"},
	Dark:  {"#6dd58c", "#f2b8b5", "#ffdf9e"},
	OLED:  {"#6dd58c", "#ffb4ab", "#ffdf9e"},
}

// containerLadder returns the five surface-container steps for a theme.
//
// Light and dark interpolate the house table's tracked endpoints
// (#ffffff → #e6e0e9 and #0f0d13 → #36343b): both endpoints are converted
// to OKLCH and L, C and H are linearly interpolated at t = 0, .25, .5, .75,
// 1 over the shorter hue arc (an endpoint with chroma below 1e-6 is
// achromatic — its hue is taken from the other endpoint so the ladder keeps
// the tint direction). OLED returns the house table's fixed five-hex ladder.
func containerLadder(t Theme) [5]string {
	if t == OLED {
		return oledLadder
	}
	spec := neutrals[t]
	from, to := hexToOKLCH(spec.ladderFrom), hexToOKLCH(spec.ladderTo)
	if from.C < 1e-6 {
		from.H = to.H
	}
	if to.C < 1e-6 {
		to.H = from.H
	}
	dh := math.Mod(to.H-from.H+180, 360)
	if dh < 0 {
		dh += 360
	}
	dh -= 180
	var out [5]string
	for i := range out {
		tt := float64(i) / 4
		c := oklch{
			L: from.L + (to.L-from.L)*tt,
			C: from.C + (to.C-from.C)*tt,
			H: from.H + dh*tt,
		}
		out[i] = c.hex()
	}
	return out
}

// cssOrder is the frozen emission order of the custom properties
// (controller ruling 3). color-scheme is emitted before all of them.
var cssOrder = []string{
	"primary", "on-primary", "primary-container", "on-primary-container",
	"surface-tint", "inverse-primary",
	"secondary", "on-secondary", "secondary-container", "on-secondary-container",
	"tertiary", "on-tertiary", "tertiary-container", "on-tertiary-container",
	"background", "on-surface", "outline",
	"surface-container-1", "surface-container-2", "surface-container-3",
	"surface-container-4", "surface-container-5",
	"profit", "loss", "warning",
}

// values assembles the palette for a validated theme and accent, keyed by
// custom-property name.
func values(t Theme, a Accent) map[string]string {
	v := make(map[string]string, len(cssOrder))
	for _, f := range families {
		hue := math.Mod(a.Hue+f.hueOffset, 360)
		for _, r := range f.roles {
			L, factor := r.darkL, r.darkFactor
			if t == Light {
				L, factor = r.lightL, r.lightFactor
			}
			v[r.name] = oklch{L: L, C: a.Chroma * f.chromaScale * factor, H: hue}.hex()
		}
	}
	n := neutrals[t]
	v["background"] = n.background
	v["on-surface"] = n.onSurface
	v["outline"] = n.outline
	for i, hex := range containerLadder(t) {
		v[fmt.Sprintf("surface-container-%d", i+1)] = hex
	}
	s := status[t]
	v["profit"] = s.profit
	v["loss"] = s.loss
	v["warning"] = s.warning
	return v
}

// cssBlock renders the :root{...} block for a validated theme and accent:
// color-scheme first (light for Light, dark for Dark and OLED), then every
// custom property in cssOrder.
func cssBlock(t Theme, a Accent) string {
	scheme := "dark"
	if t == Light {
		scheme = "light"
	}
	v := values(t, a)
	var b strings.Builder
	b.WriteString(":root{color-scheme:")
	b.WriteString(scheme)
	for _, name := range cssOrder {
		b.WriteString(";--")
		b.WriteString(name)
		b.WriteByte(':')
		b.WriteString(v[name])
	}
	b.WriteByte('}')
	return b.String()
}
