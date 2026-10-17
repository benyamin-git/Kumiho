//go:build windows

package gui

import (
	"testing"

	"github.com/benyamin-git/kumiho/internal/gui/prefs"
)

// savedGeometry builds preferences with one saved window rectangle.
func savedGeometry(x, y, w, h int, maximized bool) prefs.Prefs {
	var p prefs.Prefs
	p.Window.X, p.Window.Y = x, y
	p.Window.Width, p.Window.Height = w, h
	p.Window.Maximized = maximized
	return p
}

// TestPlacementIn pins the T4.2 geometry rules: the normal (restored)
// rectangle is clamped into the monitor set, the minimum size is enforced, an
// off-screen rectangle falls back to the default size, and the maximized flag
// survives both the clamp and the fallback so a maximized window reopens
// maximized.
func TestPlacementIn(t *testing.T) {
	const vx, vy, vw, vh = -1920, 0, 3840, 1080 // primary 1920x1080 plus a monitor to the left

	cases := []struct {
		name string
		p    prefs.Prefs
		want windowPlacement
	}{
		{
			name: "negative multi-monitor restore",
			p:    savedGeometry(-1500, 100, 1080, 720, false),
			want: windowPlacement{x: -1500, y: 100, w: 1080, h: 720, place: true},
		},
		{
			name: "clamped into the virtual screen",
			p:    savedGeometry(1800, 1000, 1080, 720, false),
			want: windowPlacement{x: 840, y: 360, w: 1080, h: 720, place: true},
		},
		{
			name: "minimum size enforced",
			p:    savedGeometry(0, 0, 100, 100, false),
			want: windowPlacement{x: 0, y: 0, w: prefs.MinWidth, h: prefs.MinHeight, place: true},
		},
		{
			name: "off-screen falls back to defaults",
			p:    savedGeometry(5000, 0, 1080, 720, false),
			want: windowPlacement{w: prefs.DefaultWidth, h: prefs.DefaultHeight},
		},
		{
			name: "no saved geometry",
			p:    prefs.Prefs{},
			want: windowPlacement{w: prefs.DefaultWidth, h: prefs.DefaultHeight},
		},
		{
			name: "maximized restore keeps the normal rectangle",
			p:    savedGeometry(-100, 50, 1200, 800, true),
			want: windowPlacement{x: -100, y: 50, w: 1200, h: 800, place: true, maximized: true},
		},
		{
			name: "maximized survives an off-screen normal rectangle",
			p:    savedGeometry(9000, 9000, 1200, 800, true),
			want: windowPlacement{w: prefs.DefaultWidth, h: prefs.DefaultHeight, maximized: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := placementIn(tc.p, vx, vy, vw, vh); got != tc.want {
				t.Errorf("placementIn(%+v) = %+v, want %+v", tc.p.Window, got, tc.want)
			}
		})
	}
}

func TestClampContained(t *testing.T) {
	cases := []struct{ v, lo, hi, want int }{
		{5, 0, 10, 5},
		{-5, 0, 10, 0},
		{15, 0, 10, 10},
		{7, 10, 5, 10}, // window larger than the screen pins at lo
	}
	for _, tc := range cases {
		if got := clampContained(tc.v, tc.lo, tc.hi); got != tc.want {
			t.Errorf("clampContained(%d, %d, %d) = %d, want %d", tc.v, tc.lo, tc.hi, got, tc.want)
		}
	}
}
