package tui

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

type fakeTuiDaemon struct {
	mu        sync.Mutex
	locations api.LocationsResult
	settings  api.SettingsView
	relay     api.RelayTestResult
	locked    []api.SettingsSetPayload
	selected  []api.SelectLocationPayload
	ping      []string
}

func (d *fakeTuiDaemon) snapshot() fakeTuiDaemon {
	d.mu.Lock()
	defer d.mu.Unlock()
	return fakeTuiDaemon{
		locations: d.locations,
		settings:  d.settings,
		relay:     d.relay,
		locked:    append([]api.SettingsSetPayload(nil), d.locked...),
		selected:  append([]api.SelectLocationPayload(nil), d.selected...),
		ping:      append([]string(nil), d.ping...),
	}
}

func startFakeTuiDaemon(t *testing.T) (*ipc.Client, *fakeTuiDaemon) {
	t.Helper()
	result := tuiLocationsResult()
	daemon := &fakeTuiDaemon{
		locations: result,
		settings: api.SettingsView{
			SocksPort: 1080, MTU: 1500, LogLevel: "info",
			KillSwitchEffective: true, AutoconnectEffective: false,
			AllowLocked: false,
		},
		relay: api.RelayTestResult{OK: true, Status: 200, LatencyMS: floatPtr(7)},
	}
	path := filepath.Join(t.TempDir(), "control.sock")
	srv, err := ipc.UnixTransport{}.Listen(path)
	if err != nil {
		t.Skipf("unix sockets unavailable on this platform: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		srv.Close()
	})
	go srv.Serve(ctx, func(_ context.Context, conn *ipc.Conn) error {
		for {
			env, err := conn.Read()
			if err != nil {
				return nil
			}
			switch env.Type {
			case ipc.TypeLocations:
				daemon.mu.Lock()
				result := daemon.locations
				daemon.mu.Unlock()
				_ = conn.Reply(env.ID, ipc.TypeLocations, result)
			case ipc.TypeSettingsGet:
				daemon.mu.Lock()
				view := daemon.settings
				daemon.mu.Unlock()
				_ = conn.Reply(env.ID, ipc.TypeSettings, view)
			case ipc.TypeSettingsSet:
				var p api.SettingsSetPayload
				if err := env.DecodePayload(&p); err != nil {
					_ = conn.Reply(env.ID, ipc.TypeError, api.RemoteError{Code: api.CodeBadRequest, Message: err.Error()})
					continue
				}
				daemon.mu.Lock()
				daemon.locked = append(daemon.locked, p)
				switch p.Key {
				case "allow_locked":
					on := p.Value == "on" || p.Value == "true"
					if p.Reset {
						on = false
					}
					daemon.settings.AllowLocked = on
					daemon.locations.AllowLocked = on
				case "relay_url":
					daemon.settings.RelayURL = p.Value
					if p.Reset {
						daemon.settings.RelayURL = ""
					}
				case "relay_token":
					daemon.settings.RelayTokenSet = !p.Reset
				}
				daemon.mu.Unlock()
				_ = conn.Reply(env.ID, ipc.TypeResult, api.Result{OK: true})
			case ipc.TypeSelectLocation:
				var p api.SelectLocationPayload
				if err := env.DecodePayload(&p); err != nil {
					_ = conn.Reply(env.ID, ipc.TypeError, api.RemoteError{Code: api.CodeBadRequest, Message: err.Error()})
					continue
				}
				daemon.mu.Lock()
				daemon.selected = append(daemon.selected, p)
				daemon.mu.Unlock()
				_ = conn.Reply(env.ID, ipc.TypeResult, api.Result{OK: true})
			case ipc.TypePing:
				var p api.PingPayload
				if err := env.DecodePayload(&p); err != nil {
					_ = conn.Reply(env.ID, ipc.TypeError, api.RemoteError{Code: api.CodeBadRequest, Message: err.Error()})
					continue
				}
				daemon.mu.Lock()
				daemon.ping = append(daemon.ping, p.Hosts...)
				daemon.mu.Unlock()
				if err := conn.Reply(env.ID, ipc.TypeResult, api.Result{OK: true}); err != nil {
					return err
				}
				_ = conn.Event(ipc.TypePingDone, api.PingDone{Total: len(p.Hosts), Answered: len(p.Hosts)})
			case ipc.TypeStatus:
				_ = conn.Reply(env.ID, ipc.TypeStatus, api.Status{State: "IDLE"})
			case ipc.TypeRelayTest:
				daemon.mu.Lock()
				result := daemon.relay
				daemon.mu.Unlock()
				_ = conn.Reply(env.ID, ipc.TypeRelayTest, result)
			}
		}
	})

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	client, err := ipc.UnixTransport{}.Dial(dialCtx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, daemon
}

func tuiLocationsResult() api.LocationsResult {
	model := testLocationsModel()
	return api.LocationsResult{
		Countries: model.loc.countries,
		Latency: map[string]api.CachedPing{
			"fra1.example.net:443": {RTTMS: floatPtr(12)},
		},
	}
}

func TestLocationsSectionsOrderFastestThenFullList(t *testing.T) {
	m := testLocationsModel()
	m.loc.rtts = map[string]*float64{
		"p.m1.fastly-masque.net:2499": floatPtr(20),
		"fra1.example.net:443":        floatPtr(5),
		"a.example.net:2499":          floatPtr(10),
	}
	rows := m.locationRows()
	if !rows[0].fastest || rows[0].disabled || rows[0].address != "fra1.example.net:443" {
		t.Fatalf("fastest row = %+v", rows[0])
	}
	var sections []locSection
	for _, row := range rows {
		if len(sections) == 0 || sections[len(sections)-1] != row.section {
			sections = append(sections, row.section)
		}
	}
	wantSections := []locSection{sectionFastest, sectionLocations}
	if len(sections) != len(wantSections) {
		t.Fatalf("sections = %v, want %v", sections, wantSections)
	}
	for i := range wantSections {
		if sections[i] != wantSections[i] {
			t.Fatalf("sections = %v, want %v", sections, wantSections)
		}
	}
	view := m.View()
	for _, want := range []string{"Fastest", "Locations"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing section %q:\n%s", want, view)
		}
	}
}

func TestLocationsEmptySectionsAreOmitted(t *testing.T) {
	m := testLocationsModel()
	view := m.View()
	if strings.Contains(view, "Favorites") || strings.Contains(view, "Recent") {
		t.Fatalf("removed sections rendered:\n%s", view)
	}
	if !strings.Contains(view, "Fastest") || !strings.Contains(view, "Run Ping all to enable Fastest") {
		t.Fatalf("disabled Fastest missing:\n%s", view)
	}
}

func TestLocationsFastestDisabledIsANoop(t *testing.T) {
	m := testLocationsModel()
	m.loc.cursor = 0
	if _, cmd := m.handleLocationsKey(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("disabled Fastest must not select")
	}
	if !strings.Contains(m.notice, "Run Ping all to enable Fastest") {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestLocationsFilterAndLockedRows(t *testing.T) {
	m := testLocationsModel()
	m.loc.filter = "zzz"
	view := m.View()
	if strings.Contains(view, "fra1.example.net:443") {
		t.Fatalf("filtered row still rendered:\n%s", view)
	}
	if !strings.Contains(view, "no locations match the filter") {
		t.Fatalf("no-match copy missing:\n%s", view)
	}

	locked := serverlist.Country{
		Code: "JP", Name: "Japan", Locked: true,
		Cities: []serverlist.City{{
			Code: "TYO", Name: "Tokyo",
			Servers: []serverlist.Server{{Hostname: "jp.example.net", Port: 443}},
		}},
	}
	m.loc.countries = append(m.loc.countries, locked)
	m.loc.filter = ""
	if strings.Contains(m.View(), "jp.example.net:443") {
		t.Fatalf("locked row rendered with allow_locked off:\n%s", m.View())
	}
	m.loc.allowLocked = true
	if !strings.Contains(m.View(), "jp.example.net:443") || !strings.Contains(m.View(), "locked") {
		t.Fatalf("locked row/badge missing with allow_locked on:\n%s", m.View())
	}
}

func TestLocationsSpeedOrderReordersFullList(t *testing.T) {
	m := testLocationsModel()
	m.loc.rtts = map[string]*float64{
		"p.m1.fastly-masque.net:2499": floatPtr(100),
		"fra1.example.net:443":        floatPtr(5),
		"a.example.net:2499":          floatPtr(10),
		"edge.example.net:8443":       floatPtr(20),
	}
	m.loc.sortSpeed = true
	m.loc.speedOrderRTTs = m.loc.rtts
	if got := locationAddresses(m); len(got) < 2 || got[0] != "p.m1.fastly-masque.net:2499" || got[1] != "fra1.example.net:443" {
		t.Fatalf("full list not speed ordered after REC: %v", got)
	}
}

func TestLocationsPingDoneEventCommitsSpeedOrder(t *testing.T) {
	m := testLocationsModel()
	m.loc.ping = &pingRun{total: 1, hosts: map[string]bool{locationAddressKey("fra1.example.net:443"): true}}
	m.loc.rtts = map[string]*float64{"fra1.example.net:443": floatPtr(5)}
	m.ackPing("fra1.example.net:443")
	env, err := ipc.NewEnvelope("", ipc.TypePingDone, api.PingDone{Total: 1, Answered: 1})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(eventMsg{env: env})
	if m.loc.ping != nil || !m.loc.sortSpeed {
		t.Fatalf("ping done did not commit: ping=%+v sort=%v", m.loc.ping, m.loc.sortSpeed)
	}
}

func TestLocationsPingAllWithoutCandidatesShowsNotice(t *testing.T) {
	m := testLocationsModel()
	m.loc.countries = nil
	if _, cmd := m.handleLocationsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")}); cmd != nil {
		t.Fatal("empty Ping all should not send")
	}
	if !strings.Contains(m.notice, "No locations to ping") {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestLocationsLockedInverse(t *testing.T) {
	client, daemon := startFakeTuiDaemon(t)
	m := &Model{client: client, screen: screenLocations, loc: locationsState{countries: tuiLocationsResult().Countries}}
	m.handleLocationsMsg(locationsMsg{result: tuiLocationsResult()})

	m.loc.allowLocked = false
	_, cmd := m.handleLocationsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("L")})
	if cmd == nil {
		t.Fatal("L should send settings_set")
	}
	runCmds(m, cmd)
	snap := daemon.snapshot()
	if len(snap.locked) != 1 || snap.locked[0].Key != "allow_locked" || snap.locked[0].Value != "on" {
		t.Fatalf("locked payload = %+v", snap.locked)
	}
	if !m.loc.allowLocked {
		t.Fatal("refetched allow_locked = false, want true")
	}
}
