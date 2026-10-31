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
	favorites []api.FavoriteLocationPayload
	clears    int
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
		favorites: append([]api.FavoriteLocationPayload(nil), d.favorites...),
		clears:    d.clears,
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
			case ipc.TypeFavoriteLocation:
				var p api.FavoriteLocationPayload
				if err := env.DecodePayload(&p); err != nil {
					_ = conn.Reply(env.ID, ipc.TypeError, api.RemoteError{Code: api.CodeBadRequest, Message: err.Error()})
					continue
				}
				daemon.mu.Lock()
				daemon.favorites = append(daemon.favorites, p)
				ref := api.LocationRef{CountryCode: p.CountryCode, CityCode: p.CityCode, Server: p.Server}
				if p.Favorite {
					daemon.locations.Favorites = append(daemon.locations.Favorites, ref)
				} else {
					out := daemon.locations.Favorites[:0]
					for _, existing := range daemon.locations.Favorites {
						if existing.Server != p.Server {
							out = append(out, existing)
						}
					}
					daemon.locations.Favorites = out
				}
				daemon.mu.Unlock()
				_ = conn.Reply(env.ID, ipc.TypeResult, api.Result{OK: true})
			case ipc.TypeClearRecents:
				daemon.mu.Lock()
				daemon.clears++
				daemon.locations.Recents = nil
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

func addFavoriteRefs(t *testing.T, m *Model) {
	t.Helper()
	m.loc.favorites = []api.LocationRef{
		{CountryCode: "DE", CityCode: "FRA", Server: "fra1.example.net:443"},
		{CountryCode: "REC", CityCode: "rec", Server: "p.m1.fastly-masque.net:2499"},
	}
	m.loc.recents = []api.LocationRef{
		{CountryCode: "US", CityCode: "NYC", Server: "a.example.net:2499"},
		{CountryCode: "DE", CityCode: "FRA", Server: "fra1.example.net:443"},
	}
}

func TestLocationsSectionsOrderPinnedRowsAndSharedState(t *testing.T) {
	m := testLocationsModel()
	addFavoriteRefs(t, m)
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
	wantSections := []locSection{sectionFastest, sectionFavorites, sectionRecent, sectionLocations}
	if len(sections) != len(wantSections) {
		t.Fatalf("sections = %v, want %v", sections, wantSections)
	}
	for i := range wantSections {
		if sections[i] != wantSections[i] {
			t.Fatalf("sections = %v, want %v", sections, wantSections)
		}
	}
	for _, row := range rows {
		if row.address == "fra1.example.net:443" && (!row.favorite || !row.recent) {
			t.Fatalf("shared fra1 state = favorite %v recent %v", row.favorite, row.recent)
		}
	}
	view := m.View()
	for _, want := range []string{"Fastest", "Favorites", "Recent  c clear", "Locations"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing section %q:\n%s", want, view)
		}
	}
}

func TestLocationsEmptySectionsAreOmitted(t *testing.T) {
	m := testLocationsModel()
	view := m.View()
	if strings.Contains(view, "Favorites") || strings.Contains(view, "Recent  c clear") {
		t.Fatalf("empty pinned sections rendered:\n%s", view)
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
	if _, cmd := m.handleLocationsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")}); cmd != nil {
		t.Fatal("Fastest must not be favorited")
	}
}

func TestLocationsFilterOnlyFullListAndLockedRows(t *testing.T) {
	m := testLocationsModel()
	addFavoriteRefs(t, m)
	m.loc.filter = "zzz"
	view := m.View()
	if !strings.Contains(view, "fra1.example.net:443") {
		t.Fatalf("favorite was filtered out:\n%s", view)
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

func TestLocationsSpeedOrderOnlyReordersFullList(t *testing.T) {
	m := testLocationsModel()
	addFavoriteRefs(t, m)
	m.loc.rtts = map[string]*float64{
		"p.m1.fastly-masque.net:2499": floatPtr(100),
		"fra1.example.net:443":        floatPtr(5),
		"a.example.net:2499":          floatPtr(10),
		"edge.example.net:8443":       floatPtr(20),
	}
	m.loc.sortSpeed = true
	m.loc.speedOrderRTTs = m.loc.rtts
	rows := m.locationRows()
	var favoriteAddresses []string
	for _, row := range rows {
		if row.section == sectionFavorites {
			favoriteAddresses = append(favoriteAddresses, row.address)
		}
	}
	if strings.Join(favoriteAddresses, ",") != "fra1.example.net:443,p.m1.fastly-masque.net:2499" {
		t.Fatalf("favorites reordered by speed: %v", favoriteAddresses)
	}
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

func TestLocationsFavoriteOptimisticAndRefetch(t *testing.T) {
	client, daemon := startFakeTuiDaemon(t)
	m := &Model{client: client, screen: screenLocations, loc: locationsState{countries: tuiLocationsResult().Countries}}
	m.handleLocationsMsg(locationsMsg{result: tuiLocationsResult()})
	m.loc.cursor = 1 // first full row: REC
	before := len(m.loc.favorites)
	_, cmd := m.handleLocationsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if len(m.loc.favorites) != before+1 {
		t.Fatalf("optimistic favorite count = %d, want %d", len(m.loc.favorites), before+1)
	}
	if cmd == nil {
		t.Fatal("favorite should send a command")
	}
	runCmds(m, cmd)
	snap := daemon.snapshot()
	if len(snap.favorites) != 1 || !snap.favorites[0].Favorite || snap.favorites[0].Server != "p.m1.fastly-masque.net:2499" {
		t.Fatalf("favorite payloads = %+v", snap.favorites)
	}
	if len(m.loc.favorites) != 1 {
		t.Fatalf("favorites after refetch = %+v", m.loc.favorites)
	}
}

func TestLocationsClearRecentsAndLockedInverse(t *testing.T) {
	client, daemon := startFakeTuiDaemon(t)
	m := &Model{client: client, screen: screenLocations, loc: locationsState{countries: tuiLocationsResult().Countries}}
	result := tuiLocationsResult()
	result.Recents = []api.LocationRef{{CountryCode: "US", CityCode: "NYC", Server: "a.example.net:2499"}}
	m.handleLocationsMsg(locationsMsg{result: result})
	_, cmd := m.handleLocationsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if len(m.loc.recents) != 0 {
		t.Fatalf("recents were not cleared locally: %+v", m.loc.recents)
	}
	runCmds(m, cmd)
	if snap := daemon.snapshot(); snap.clears != 1 {
		t.Fatalf("clear payload count = %d", snap.clears)
	}

	m.loc.allowLocked = false
	_, cmd = m.handleLocationsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("L")})
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
