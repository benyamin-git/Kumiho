//go:build windows

package gui

import "testing"

// TestTrayStateWord pins the tooltip mapping to the header model (inventory
// §Shell, §Tray): unreachable first, then the live state, then the first-load
// failure, then the loading placeholder. Persian is resolved from the same
// raw codes, not from the English label.
func TestTrayStateWord(t *testing.T) {
	tests := []struct {
		name string
		snap State
		want string
	}{
		{"unreachable without status", State{}, "Daemon unreachable"},
		{"unreachable hides a stale connected status", State{Status: &StatusView{Label: "Connected"}}, "Daemon unreachable"},
		{"live zero state", State{Reachable: true, Status: &StatusView{State: "IDLE", Label: "Off"}}, "Off"},
		{"live connected", State{Reachable: true, Status: &StatusView{State: "CONNECTED", Label: "Connected"}}, "Connected"},
		{"live proxy only", State{Reachable: true, Status: &StatusView{State: "PROXY_ONLY", Label: "Proxy only"}}, "Proxy only"},
		{"live connecting", State{Reachable: true, Status: &StatusView{State: "CONNECTING", Label: "Connecting"}}, "Connecting"},
		{"live reconnecting", State{Reachable: true, Status: &StatusView{State: "RECONNECTING", Label: "Reconnecting"}}, "Reconnecting"},
		{"live waiting for network", State{Reachable: true, Status: &StatusView{State: "WAITING_NETWORK", Label: "Waiting for network"}}, "Waiting for network"},
		{"live not signed in", State{Reachable: true, Status: &StatusView{State: "UNAUTHENTICATED", Label: "Not signed in"}}, "Not signed in"},
		{"live error", State{Reachable: true, Status: &StatusView{State: "FATAL", Label: "Error"}}, "Error"},
		{"label fallback", State{Reachable: true, Status: &StatusView{Label: "Connected"}}, "Connected"},
		{"first status load failed", State{Reachable: true, StatusError: "dial: refused"}, "Status unavailable"},
		{"first load in flight", State{Reachable: true}, "Loading..."},
		{"persian connected", State{Reachable: true, Language: "fa", Status: &StatusView{State: "CONNECTED", Label: "Connected"}}, "متصل"},
		{"persian waiting", State{Reachable: true, Language: "fa", Status: &StatusView{State: "WAITING_NETWORK", Label: "Waiting for network"}}, "در انتظار شبکه"},
		{"persian unreachable", State{Language: "fa"}, "دیمن در دسترس نیست"},
		{"unknown language resolves to english", State{Reachable: true, Language: "de"}, "Loading..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trayStateWord(tt.snap); got != tt.want {
				t.Fatalf("trayStateWord(%+v) = %q, want %q", tt.snap, got, tt.want)
			}
		})
	}
}

// TestTrayDisconnect pins the Dashboard button's label rule (inventory
// §Tray): exactly the five active states disconnect; every other state —
// including loading and unreachable — keeps Connect.
func TestTrayDisconnect(t *testing.T) {
	tests := []struct {
		name string
		snap State
		want bool
	}{
		{"connected", State{Reachable: true, Status: &StatusView{State: "CONNECTED"}}, true},
		{"proxy only", State{Reachable: true, Status: &StatusView{State: "PROXY_ONLY"}}, true},
		{"connecting", State{Reachable: true, Status: &StatusView{State: "CONNECTING"}}, true},
		{"reconnecting", State{Reachable: true, Status: &StatusView{State: "RECONNECTING"}}, true},
		{"waiting for network", State{Reachable: true, Status: &StatusView{State: "WAITING_NETWORK"}}, true},
		{"off", State{Reachable: true, Status: &StatusView{State: "IDLE"}}, false},
		{"not signed in", State{Reachable: true, Status: &StatusView{State: "UNAUTHENTICATED"}}, false},
		{"error", State{Reachable: true, Status: &StatusView{State: "FATAL"}}, false},
		{"loading", State{Reachable: true}, false},
		{"unreachable", State{}, false},
		{"label fallback", State{Reachable: true, Status: &StatusView{Label: "Connected"}}, true},
		{"persian connected", State{Reachable: true, Language: "fa", Status: &StatusView{State: "CONNECTED"}}, true},
		{"stale connected under unreachable", State{Status: &StatusView{State: "CONNECTED"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trayDisconnect(tt.snap); got != tt.want {
				t.Errorf("trayDisconnect(%+v) = %v, want %v", tt.snap, got, tt.want)
			}
		})
	}
}

// TestTrayStateHookRefreshesOnWordChange pins the hook's only job: the first
// snapshot refreshes (the icon may have been installed from an earlier read),
// unchanged words never repost, and any word change — including one caused by
// reachability or the UI language alone — posts exactly once. A snapshot
// changing in fields the word does not use (uptime) must not post.
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

	h.observe(State{Reachable: true, Status: &StatusView{State: "CONNECTED", Label: "Connected"}})
	if refreshes != 2 {
		t.Fatalf("a word change refreshed %d times, want 2", refreshes)
	}

	h.observe(State{Reachable: true, Status: &StatusView{State: "CONNECTED", Label: "Connected", Uptime: "5s"}})
	if refreshes != 2 {
		t.Fatalf("an uptime-only change refreshed %d times, want 2", refreshes)
	}

	h.observe(State{Reachable: true, Language: "fa", Status: &StatusView{State: "CONNECTED", Label: "Connected", Uptime: "5s"}})
	if refreshes != 3 {
		t.Fatalf("a language change refreshed %d times, want 3", refreshes)
	}

	h.observe(State{Reachable: false, Language: "fa", Status: &StatusView{State: "CONNECTED", Label: "Connected", Uptime: "5s"}})
	if refreshes != 4 {
		t.Fatalf("a reachability change refreshed %d times, want 4", refreshes)
	}
}
