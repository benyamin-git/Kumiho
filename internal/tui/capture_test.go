package tui

// Dev-only screenshot source for the README: renders the real Bubble Tea
// views with fixed sample data and writes the raw ANSI to a scratch
// directory. The normal test run skips it; scripts/screenshots/capture.sh
// sets KUMIHO_TUI_CAPTURE_DIR, then render-tui.mjs turns the ANSI into PNGs.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

const (
	captureWidth  = 100
	captureHeight = 28
)

func captureNow() time.Time {
	return time.Now().Truncate(time.Second)
}

func captureI64(v int64) *int64 { return &v }

func captureStatus(now time.Time) api.Status {
	return api.Status{
		State:         "CONNECTED",
		Authenticated: true,
		Email:         "user@example.com",
		Since:         now.Add(-90 * time.Minute).Unix(),
		TokenExpires:  now.Add(30 * 24 * time.Hour).Unix(),
		Location:      "DE",
		Server:        "de1.edge.example:443",
		PassExpires:   now.Add(6 * time.Hour).Unix(),
		Quota: &api.Quota{
			Limit:     captureI64(50 << 30),
			Remaining: captureI64(38 << 30),
			Reset:     captureI64(now.Add(12 * 24 * time.Hour).Unix()),
		},
		ExitIP:      "203.0.113.7",
		ExitCountry: "Germany",
		Totals:      &api.ByteCounters{Up: 12 << 20, Down: 80 << 20},
		Rates:       &api.ByteRates{Up: 1.5 * 1024 * 1024, Down: 3 * 1024 * 1024},
		KillSwitch:  true,
		Autoconnect: false,
	}
}

func captureLocations() locationsState {
	rtt := 42.0
	recommended := 58.0
	return locationsState{
		countries: []serverlist.Country{
			{
				Code: serverlist.RecommendedCode, Name: "Recommended Location",
				Cities: []serverlist.City{{
					Code: "rec", Name: "Recommended",
					Servers: []serverlist.Server{{Hostname: "p.m1.fastly-masque.net", Port: 2499}},
				}},
			},
			{
				Code: "DE", Name: "Germany",
				Cities: []serverlist.City{
					{
						Code: "BER", Name: "Berlin",
						Servers: []serverlist.Server{
							{Hostname: "de1.edge.example", Port: 443},
							{Hostname: "de2.edge.example", Port: 443},
						},
					},
					{
						Code: "FRA", Name: "Frankfurt",
						Servers: []serverlist.Server{{Hostname: "fra1.edge.example", Port: 443}},
					},
				},
			},
			{
				Code: "JP", Name: "Japan",
				Cities: []serverlist.City{{
					Code: "TYO", Name: "Tokyo",
					Servers: []serverlist.Server{{Hostname: "jp1.edge.example", Port: 443}},
				}},
			},
			{
				Code: "US", Name: "United States",
				Cities: []serverlist.City{{
					Code: "NYC", Name: "New York",
					Servers: []serverlist.Server{{Hostname: "us1.edge.example", Port: 443}},
				}},
			},
		},
		cursor: 0,
		rtts: map[string]*float64{
			"p.m1.fastly-masque.net:2499": &recommended,
			"de1.edge.example:443":        &rtt,
			"de2.edge.example:443":        nil,
		},
	}
}

func captureSettings() settingsState {
	return settingsState{
		loaded: true,
		view: api.SettingsView{
			SocksPort:           1080,
			MTU:                 8500,
			ExitCheck:           true,
			DoHProvider:         "automatic",
			AllowLAN:            false,
			LogLevel:            "info",
			RedactTargets:       true,
			KillSwitch:          "on",
			Autoconnect:         "off",
			KillSwitchEffective: true, AutoconnectEffective: false,
			QuotaPollMinutes: 15,
			TLSSplit:         "off",
			HTTPProxy:        false,
			HTTPProxyPort:    8080,
			SolveChallenges:  true,
			KeepaliveProfile: "standard",
		},
		pinned: map[string]bool{"kill_switch": true},
		cursor: settingIndex("tls_split"),
	}
}

// TestCaptureViews writes the README's TUI screenshots as raw ANSI. It is a
// no-op unless KUMIHO_TUI_CAPTURE_DIR is set, so `go test ./...` stays clean.
func TestCaptureViews(t *testing.T) {
	dir := os.Getenv("KUMIHO_TUI_CAPTURE_DIR")
	if dir == "" {
		t.Skip("set KUMIHO_TUI_CAPTURE_DIR to write the README TUI captures")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create capture dir: %v", err)
	}

	// ANSI256 is termenv's numeric profile value. Importing termenv only for
	// the constant would promote an indirect module to a direct dependency.
	lipgloss.SetColorProfile(1)
	applyTheme("dark")

	now := captureNow()
	status := captureStatus(now)
	models := []struct {
		name string
		m    *Model
	}{
		{
			name: "dashboard",
			m: &Model{
				screen: screenDashboard, haveStat: true, status: status,
				width: captureWidth, height: captureHeight,
			},
		},
		{
			name: "locations",
			m: &Model{
				screen: screenLocations, haveStat: true, status: status,
				loc: captureLocations(), width: captureWidth, height: captureHeight,
			},
		},
		{
			name: "settings",
			m: &Model{
				screen: screenSettings, haveStat: true, status: status,
				st: captureSettings(), width: captureWidth, height: captureHeight,
			},
		},
	}

	for _, item := range models {
		view := item.m.View()
		path := filepath.Join(dir, item.name+".ansi")
		if err := os.WriteFile(path, []byte(view), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		fmt.Printf("wrote %s (%d bytes)\n", path, len(view))
	}
}
