//go:build windows

package gui

import "testing"

// TestTrayWord pins the tooltip mapping to the header model (inventory §Shell,
// §Tray): unreachable first, then the live state label, then the first-load
// failure, then the loading placeholder.
func TestTrayWord(t *testing.T) {
	tests := []struct {
		name string
		snap State
		want string
	}{
		{"unreachable without status", State{}, "Daemon unreachable"},
		{"unreachable hides a stale connected status", State{Status: &StatusView{Label: "Connected"}}, "Daemon unreachable"},
		{"live zero state", State{Reachable: true, Status: &StatusView{Label: "Off"}}, "Off"},
		{"live connected", State{Reachable: true, Status: &StatusView{Label: "Connected"}}, "Connected"},
		{"live proxy only", State{Reachable: true, Status: &StatusView{Label: "Proxy only"}}, "Proxy only"},
		{"live connecting", State{Reachable: true, Status: &StatusView{Label: "Connecting"}}, "Connecting"},
		{"live reconnecting", State{Reachable: true, Status: &StatusView{Label: "Reconnecting"}}, "Reconnecting"},
		{"live waiting for network", State{Reachable: true, Status: &StatusView{Label: "Waiting for network"}}, "Waiting for network"},
		{"live not signed in", State{Reachable: true, Status: &StatusView{Label: "Not signed in"}}, "Not signed in"},
		{"live error", State{Reachable: true, Status: &StatusView{Label: "Error"}}, "Error"},
		{"first status load failed", State{Reachable: true, StatusError: "dial: refused"}, "Status unavailable"},
		{"first load in flight", State{Reachable: true}, "Loading..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trayWord(tt.snap); got != tt.want {
				t.Fatalf("trayWord(%+v) = %q, want %q", tt.snap, got, tt.want)
			}
		})
	}
}

// TestTrayDisconnect pins the Dashboard button's label rule (inventory
// §Tray): exactly the five active states disconnect; every other word —
// including the loading/unreachable ones and a stale connected word under an
// unreachable daemon — keeps Connect.
func TestTrayDisconnect(t *testing.T) {
	disconnect := map[string]bool{
		"Connected":           true,
		"Proxy only":          true,
		"Connecting":          true,
		"Reconnecting":        true,
		"Waiting for network": true,
		"Off":                 false,
		"Not signed in":       false,
		"Error":               false,
		"Loading...":          false,
		"Daemon unreachable":  false,
		"Status unavailable":  false,
	}
	for word, want := range disconnect {
		if got := trayDisconnect(word); got != want {
			t.Errorf("trayDisconnect(%q) = %v, want %v", word, got, want)
		}
	}
	// The rule is applied to the tooltip word, so an unreachable snapshot
	// with a stale connected status must connect, not disconnect.
	if got := trayDisconnect(trayWord(State{Status: &StatusView{Label: "Connected"}})); got {
		t.Error("an unreachable snapshot with a stale Connected status must not disconnect")
	}
}

// TestTrayStateHookRefreshesOnWordChange pins the hook's only job: the first
// snapshot refreshes (the icon may have been installed from an earlier read),
// unchanged words never repost, and any word change — including one caused by
// reachability alone — posts exactly once. A snapshot changing in fields the
// word does not use (uptime) must not post.
func TestTrayStateHookRefreshesOnWordChange(t *testing.T) {
	refreshes := 0
	h := &trayStateHook{refresh: func() { refreshes++ }}

	h.observe(State{Reachable: true})
	if refreshes != 1 {
		t.Fatalf("first observe refreshed %d times, want 1", refreshes)
	}

	h.observe(State{Reachable: true})
	if refreshes != 1 {
		t.Fatalf("an unchanged word refreshed %d times, want 1", refreshes)
	}

	h.observe(State{Reachable: true, Status: &StatusView{Label: "Connected"}})
	if refreshes != 2 {
		t.Fatalf("a word change refreshed %d times, want 2", refreshes)
	}

	h.observe(State{Reachable: true, Status: &StatusView{Label: "Connected", Uptime: "5s"}})
	if refreshes != 2 {
		t.Fatalf("an uptime-only change refreshed %d times, want 2", refreshes)
	}

	h.observe(State{Reachable: false, Status: &StatusView{Label: "Connected", Uptime: "5s"}})
	if refreshes != 3 {
		t.Fatalf("a reachability change refreshed %d times, want 3", refreshes)
	}
}
