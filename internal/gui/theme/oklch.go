package theme

import (
	"fmt"
	"math"
	"strconv"
)

// This file holds the OKLCH colour math: the standard OKLab formulas
// (Björn Ottosson's reference) over linear-light sRGB, performed with
// stdlib math only. No colour library is involved.

// oklch is a colour in the cylindrical OKLCH form of OKLab: perceptual
// lightness L in [0,1], chroma C, and hue H in degrees.
type oklch struct {
	L, C, H float64
}

// linearRGB converts the OKLCH colour to linear-light sRGB without clipping.
func (c oklch) linearRGB() (r, g, b float64) {
	h := c.H * math.Pi / 180
	oa := c.C * math.Cos(h)
	ob := c.C * math.Sin(h)
	l := c.L + 0.3963377774*oa + 0.2158037573*ob
	m := c.L - 0.1055613458*oa - 0.0638541728*ob
	s := c.L - 0.0894841775*oa - 1.2914855480*ob
	l3, m3, s3 := l*l*l, m*m*m, s*s*s
	return 4.0767416621*l3 - 3.3077115913*m3 + 0.2309699292*s3,
		-1.2684380046*l3 + 2.6097574011*m3 - 0.3413193965*s3,
		-0.0041960863*l3 - 0.7034186147*m3 + 1.7076147010*s3
}

// inGamut reports whether the colour's linear-light sRGB channels all sit in
// [0,1]. The sRGB transfer function is monotone, so checking linear values
// is equivalent to checking the encoded ones.
func (c oklch) inGamut() bool {
	r, g, b := c.linearRGB()
	return r >= 0 && r <= 1 && g >= 0 && g <= 1 && b >= 0 && b <= 1
}

// clip applies the house gamut rule: out-of-gamut colours halve their chroma
// repeatedly until the first in-gamut value is reached. It never searches
// back up toward the gamut boundary; the documented primaries depend on this
// exact rule.
func (c oklch) clip() oklch {
	for c.C > 0 && !c.inGamut() {
		c.C /= 2
	}
	return c
}

// hex renders the colour as a lowercase "#rrggbb" literal: clipped per the
// house rule, gamma-encoded to sRGB and rounded to the nearest 8-bit value
// per channel.
func (c oklch) hex() string {
	c = c.clip()
	r, g, b := c.linearRGB()
	q := func(v float64) uint8 {
		v = linearToSRGB(v)
		v = math.Min(1, math.Max(0, v))
		return uint8(math.Round(v * 255))
	}
	return fmt.Sprintf("#%02x%02x%02x", q(r), q(g), q(b))
}

// hexToOKLCH converts a "#rrggbb" literal to OKLCH. The only inputs are the
// package's fixed house-table endpoints, so a malformed value is a
// programming error.
func hexToOKLCH(hex string) oklch {
	if len(hex) != 7 || hex[0] != '#' {
		panic("theme: malformed colour literal " + hex)
	}
	channel := func(s string) float64 {
		v, err := strconv.ParseUint(s, 16, 8)
		if err != nil {
			panic("theme: malformed colour literal " + hex)
		}
		return srgbToLinear(float64(v) / 255)
	}
	r := channel(hex[1:3])
	g := channel(hex[3:5])
	b := channel(hex[5:7])

	l := 0.4122214708*r + 0.5363325363*g + 0.0514459929*b
	m := 0.2119034982*r + 0.6806995451*g + 0.1073969566*b
	s := 0.0883024619*r + 0.2817188376*g + 0.6299787005*b
	l3, m3, s3 := math.Cbrt(l), math.Cbrt(m), math.Cbrt(s)
	L := 0.2104542553*l3 + 0.7936177850*m3 - 0.0040720468*s3
	oa := 1.9779984951*l3 - 2.4285922050*m3 + 0.4505937099*s3
	ob := 0.0259040371*l3 + 0.7827717662*m3 - 0.8086757660*s3
	C := math.Hypot(oa, ob)
	H := math.Atan2(ob, oa) * 180 / math.Pi
	if H < 0 {
		H += 360
	}
	return oklch{L: L, C: C, H: H}
}

// srgbToLinear is the standard sRGB transfer function, encoded to linear.
func srgbToLinear(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

// linearToSRGB is the inverse sRGB transfer function, linear to encoded.
func linearToSRGB(v float64) float64 {
	if v <= 0.0031308 {
		return 12.92 * v
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}
