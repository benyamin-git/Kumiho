package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

func TestDashboardShowsState(t *testing.T) {
	m := &Model{
		screen:   screenDashboard,
		haveStat: true,
		status: api.Status{
			State:         "IDLE",
			Authenticated: true,
			Email:         "user@example.com",
			KillSwitch:    true,
			Autoconnect:   true,
		},
	}
	view := m.View()
	for _, want := range []string{"IDLE", "user@example.com", "kill switch", "on"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestLoginPasswordIsMasked(t *testing.T) {
	m := &Model{screen: screenLogin, step: "password", email: "user@example.com", password: "hunter2"}
	view := m.View()
	if strings.Contains(view, "hunter2") {
		t.Fatalf("password leaked in view:\n%s", view)
	}
	if !strings.Contains(view, strings.Repeat("•", 7)) {
		t.Fatalf("masked password missing:\n%s", view)
	}
}

func TestLoginDoneSwitchesToDashboard(t *testing.T) {
	m := &Model{screen: screenLogin, step: "2fa"}
	_, cmd := m.handleLoginState(loginStateMsg{state: api.LoginState{Step: "done", Email: "user@example.com"}})
	if m.screen != screenDashboard {
		t.Fatalf("screen = %v, want dashboard", m.screen)
	}
	if !strings.Contains(m.notice, "user@example.com") {
		t.Fatalf("notice = %q", m.notice)
	}
	if cmd == nil {
		t.Fatal("expected a status refresh command")
	}
}

func TestLocationsView(t *testing.T) {
	m := &Model{
		screen: screenLocations,
		loc: locationsState{
			countries: []serverlist.Country{
				{
					Code: "REC", Name: "Recomended Location",
					Cities: []serverlist.City{{
						Code: "p", Name: "Recommended",
						Servers: []serverlist.Server{{Hostname: "p.m1.fastly-masque.net", Port: 2499}},
					}},
				},
				{
					Code: "DE", Name: "Germany",
					Cities: []serverlist.City{{
						Code: "FRA", Name: "Frankfurt",
						Servers: []serverlist.Server{{Hostname: "fra1.example.net", Port: 2499}},
					}},
				},
			},
			expandedCountries: map[string]bool{"REC": true},
			expandedCities:    map[string]bool{"REC/p": true},
			rtts:              map[string]*float64{},
		},
	}
	view := m.View()
	for _, want := range []string{"Locations", "Recomended Location", "p.m1.fastly-masque.net", "Germany"} {
		if !strings.Contains(view, want) {
			t.Errorf("locations view missing %q:\n%s", want, view)
		}
	}
	// REC (country) + its city + server + DE country = 4 visible rows.
	if rows := m.locationRows(); len(rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rows))
	}
}

func testSettingsModel() *Model {
	return &Model{
		screen: screenDashboard,
		st: settingsState{
			loaded: true,
			view: api.SettingsView{
				SocksPort: 1080, MTU: 8500, ExitCheck: true, DoHProvider: "automatic",
				LogLevel: "info", RedactTargets: true, KillSwitch: "last", Autoconnect: "last",
				KillSwitchEffective: true, AutoconnectEffective: true,
				QuotaPollMinutes: 15, AllowLAN: true, DNSUpstream: "9.9.9.9",
				TLSSplit: "sni", HTTPProxy: true, HTTPProxyPort: 9090,
				SolveChallenges: true, KeepaliveProfile: "slow",
			},
			pinned: map[string]bool{"dns_upstream": true},
		},
	}
}

func TestSettingsView(t *testing.T) {
	m := testSettingsModel()
	m.screen = screenSettings
	view := m.View()
	for _, want := range []string{
		"Settings", "kill switch", "DNS upstream", "9.9.9.9", "log level", "applies immediately",
		"TLS split", "sni", "HTTP proxy", "9090", "solve challenges", "keepalive profile", "slow",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("settings view missing %q:\n%s", want, view)
		}
	}
	// Hints follow the cursor; connection settings say when they apply.
	m.st.cursor = 6 // dns_upstream
	if view := m.View(); !strings.Contains(view, "next connect") {
		t.Errorf("dns_upstream hint missing 'next connect':\n%s", view)
	}
	// solve_challenges is the one v0.4.0 row that is not connection-scoped.
	m.st.cursor = settingIndex("solve_challenges")
	if view := m.View(); !strings.Contains(view, "applies immediately") {
		t.Errorf("solve_challenges hint missing 'applies immediately':\n%s", view)
	}
}

func settingIndex(key string) int {
	for i, item := range settingItems {
		if item.key == key {
			return i
		}
	}
	return -1
}

// cursorLabel returns the label column of the one row carrying the cursor.
func cursorLabel(t *testing.T, view string) string {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if !strings.HasPrefix(line, "> ") || len(line) < 4+17 {
			continue
		}
		rest := line[4:]
		return strings.TrimRight(rest[:17], " ")
	}
	t.Fatalf("no cursor row in view:\n%s", view)
	return ""
}

// assertViewport checks that the cursor row is rendered and that the view
// never grows past the terminal height.
func assertViewport(t *testing.T, m *Model) {
	t.Helper()
	view := m.View()
	if got, want := cursorLabel(t, view), settingItems[m.st.cursor].label; got != want {
		t.Fatalf("cursor row = %q, want %q:\n%s", got, want, view)
	}
	if m.height > 0 {
		lines := strings.Count(strings.TrimRight(view, "\n"), "\n") + 1
		if lines > m.height {
			t.Fatalf("view has %d lines, terminal height %d:\n%s", lines, m.height, view)
		}
	}
}

func TestSettingsShortHeightShowsCues(t *testing.T) {
	if len(settingItems) != 19 {
		t.Fatalf("settingItems = %d rows, want 19", len(settingItems))
	}
	m := testSettingsModel()
	m.screen = screenSettings
	m.height = 14
	m.st.offset = 0
	m.st.cursor = 0

	// Top of the list: only the down cue.
	m.ensureSettingsCursorVisible()
	view := m.View()
	if !strings.Contains(view, "↓ more") || strings.Contains(view, "↑ more") {
		t.Fatalf("top of list cues wrong:\n%s", view)
	}
	assertViewport(t, m)

	// Middle: both cues.
	m.st.cursor = 9
	m.ensureSettingsCursorVisible()
	view = m.View()
	if !strings.Contains(view, "↑ more") || !strings.Contains(view, "↓ more") {
		t.Fatalf("middle of list cues wrong:\n%s", view)
	}
	assertViewport(t, m)

	// Bottom: only the up cue.
	m.st.cursor = len(settingItems) - 1
	m.ensureSettingsCursorVisible()
	view = m.View()
	if !strings.Contains(view, "↑ more") || strings.Contains(view, "↓ more") {
		t.Fatalf("bottom of list cues wrong:\n%s", view)
	}
	assertViewport(t, m)

	// A tall terminal shows every row and no cues.
	m.height = 40
	m.st.cursor = 0
	m.ensureSettingsCursorVisible()
	view = m.View()
	if strings.Contains(view, "↑ more") || strings.Contains(view, "↓ more") {
		t.Fatalf("full-height list must not show cues:\n%s", view)
	}
	for _, item := range settingItems {
		if !strings.Contains(view, item.label) {
			t.Errorf("full-height view missing %q:\n%s", item.label, view)
		}
	}

	// A notice or error line shrinks the window but the frame still fits.
	m.height = 14
	m.st.cursor = 9
	m.st.notice = "tls_split updated"
	m.ensureSettingsCursorVisible()
	assertViewport(t, m)
	m.st.notice = ""
	m.st.err = "bad_request: tls_split: expected off|first-byte|sni"
	m.ensureSettingsCursorVisible()
	assertViewport(t, m)
}

func TestSettingsAllRowsReachable(t *testing.T) {
	for _, height := range []int{12, 14, 21, 40} {
		t.Run(fmt.Sprintf("height-%d", height), func(t *testing.T) {
			m := testSettingsModel()
			m.screen = screenSettings
			m.height = height

			// Walking down visits every row in order; the viewport follows.
			for i := range settingItems {
				if m.st.cursor != i {
					t.Fatalf("cursor = %d, want %d", m.st.cursor, i)
				}
				assertViewport(t, m)
				m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
			}
			if m.st.cursor != len(settingItems)-1 {
				t.Fatalf("down stops at cursor %d, want %d", m.st.cursor, len(settingItems)-1)
			}
			if view := m.View(); !strings.Contains(view, "keepalive profile") {
				t.Fatalf("last row not rendered:\n%s", view)
			}

			// Walking back up visits every row in reverse.
			for i := len(settingItems) - 1; i >= 0; i-- {
				if m.st.cursor != i {
					t.Fatalf("cursor = %d, want %d", m.st.cursor, i)
				}
				assertViewport(t, m)
				m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyUp})
			}
			if m.st.cursor != 0 || m.st.offset != 0 {
				t.Fatalf("up stops at cursor %d offset %d, want 0/0", m.st.cursor, m.st.offset)
			}
		})
	}
}

func TestSettingsScrollKeys(t *testing.T) {
	m := testSettingsModel()
	m.screen = screenSettings
	m.height = 14

	// pgdown moves a page and scrolls the window with the cursor.
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.st.cursor == 0 {
		t.Fatal("pgdown should move the cursor")
	}
	if m.st.offset == 0 {
		t.Fatal("pgdown should scroll the window")
	}
	assertViewport(t, m)
	afterPage := m.st.cursor

	// pgup returns to the top.
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.st.cursor != 0 || m.st.offset != 0 {
		t.Fatalf("pgup = cursor %d offset %d, want 0/0", m.st.cursor, m.st.offset)
	}
	assertViewport(t, m)

	// pgdown from the top moves exactly one page.
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.st.cursor != afterPage {
		t.Fatalf("pgdown = %d, want %d", m.st.cursor, afterPage)
	}

	// end jumps to the last row; pgdown there stays put.
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnd})
	if m.st.cursor != len(settingItems)-1 {
		t.Fatalf("end = %d, want %d", m.st.cursor, len(settingItems)-1)
	}
	if view := m.View(); !strings.Contains(view, "↑ more") {
		t.Fatalf("end should scroll to the bottom:\n%s", view)
	}
	assertViewport(t, m)
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.st.cursor != len(settingItems)-1 {
		t.Fatalf("pgdown at end = %d, want %d", m.st.cursor, len(settingItems)-1)
	}

	// home jumps back to the first row.
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyHome})
	if m.st.cursor != 0 || m.st.offset != 0 {
		t.Fatalf("home = cursor %d offset %d, want 0/0", m.st.cursor, m.st.offset)
	}
	if view := m.View(); strings.Contains(view, "↑ more") || !strings.Contains(view, "↓ more") {
		t.Fatalf("home cues wrong:\n%s", view)
	}
	assertViewport(t, m)
}

func TestSettingsViewportResize(t *testing.T) {
	m := testSettingsModel()
	m.screen = screenSettings
	m.height = 14
	m.st.cursor = len(settingItems) - 1
	m.ensureSettingsCursorVisible()

	// Growing the terminal must keep the cursor visible.
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	assertViewport(t, m)
	if m.st.offset != 0 {
		t.Fatalf("full-height offset = %d, want 0", m.st.offset)
	}

	// Shrinking it again scrolls the cursor back into view.
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	assertViewport(t, m)
	if m.st.offset == 0 {
		t.Fatal("shrink should scroll the cursor into view")
	}
}

// fakeSettingsDaemon serves settings_get/settings_set over the real control
// protocol so the TUI round-trip exercises the IPC client and JSON mapping.
type fakeSettingsDaemon struct {
	view      api.SettingsView
	overrides map[string]bool
}

func (d *fakeSettingsDaemon) overrideKeys() []string {
	keys := make([]string, 0, len(d.overrides))
	for key := range d.overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (d *fakeSettingsDaemon) set(p api.SettingsSetPayload) error {
	if p.Reset {
		switch p.Key {
		case "tls_split":
			d.view.TLSSplit = "off"
		case "http_proxy":
			d.view.HTTPProxy = false
		case "http_proxy_port":
			d.view.HTTPProxyPort = 8080
		case "solve_challenges":
			d.view.SolveChallenges = true
		case "keepalive_profile":
			d.view.KeepaliveProfile = "standard"
		default:
			return fmt.Errorf("unsupported setting %q", p.Key)
		}
		delete(d.overrides, p.Key)
		return nil
	}

	switch p.Key {
	case "tls_split":
		switch p.Value {
		case "off", "first-byte", "sni":
		default:
			return fmt.Errorf("tls_split: expected off|first-byte|sni, got %q", p.Value)
		}
		d.view.TLSSplit = p.Value
	case "keepalive_profile":
		switch p.Value {
		case "standard", "slow":
		default:
			return fmt.Errorf("keepalive_profile: expected standard|slow, got %q", p.Value)
		}
		d.view.KeepaliveProfile = p.Value
	case "http_proxy":
		v, err := fakeOnOff(p.Value)
		if err != nil {
			return fmt.Errorf("http_proxy: %w", err)
		}
		d.view.HTTPProxy = v
	case "solve_challenges":
		v, err := fakeOnOff(p.Value)
		if err != nil {
			return fmt.Errorf("solve_challenges: %w", err)
		}
		d.view.SolveChallenges = v
	case "http_proxy_port":
		n, err := strconv.Atoi(p.Value)
		if err != nil {
			return fmt.Errorf("http_proxy_port: %w", err)
		}
		if n < 1 || n > 65535 {
			return fmt.Errorf("http_proxy_port: out of range: %d", n)
		}
		d.view.HTTPProxyPort = n
	default:
		return fmt.Errorf("unsupported setting %q", p.Key)
	}
	d.overrides[p.Key] = true
	return nil
}

func fakeOnOff(value string) (bool, error) {
	switch value {
	case "on":
		return true, nil
	case "off":
		return false, nil
	}
	return false, fmt.Errorf("expected on|off, got %q", value)
}

func startFakeSettingsDaemon(t *testing.T) *ipc.Client {
	t.Helper()
	daemon := &fakeSettingsDaemon{
		view: api.SettingsView{
			SocksPort: 1080, MTU: 8500, ExitCheck: true, DoHProvider: "automatic",
			LogLevel: "info", RedactTargets: true, KillSwitch: "last", Autoconnect: "last",
			KillSwitchEffective: true, AutoconnectEffective: true,
			QuotaPollMinutes: 15, AllowLAN: true,
			TLSSplit: "off", HTTPProxyPort: 8080, SolveChallenges: true,
			KeepaliveProfile: "standard",
		},
		overrides: map[string]bool{},
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
			case ipc.TypeSettingsGet:
				view := daemon.view
				view.Overridden = daemon.overrideKeys()
				_ = conn.Reply(env.ID, ipc.TypeSettings, view)
			case ipc.TypeSettingsSet:
				var p api.SettingsSetPayload
				if err := env.DecodePayload(&p); err != nil {
					_ = conn.Reply(env.ID, ipc.TypeError, api.RemoteError{Code: api.CodeBadRequest, Message: err.Error()})
					continue
				}
				if err := daemon.set(p); err != nil {
					_ = conn.Reply(env.ID, ipc.TypeError, api.RemoteError{Code: api.CodeBadRequest, Message: err.Error()})
					continue
				}
				_ = conn.Reply(env.ID, ipc.TypeResult, api.Result{OK: true})
			}
		}
	})

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	client, err := ipc.UnixTransport{}.Dial(dialCtx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// runCmds executes a command chain (fetch/set replies) against the model.
func runCmds(m *Model, cmd tea.Cmd) {
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			return
		}
		_, cmd = m.Update(msg)
	}
}

func TestSettingsNewKeysRoundTrip(t *testing.T) {
	m := &Model{client: startFakeSettingsDaemon(t), screen: screenSettings, height: 30}
	runCmds(m, fetchSettings(m.client))
	if !m.st.loaded || m.st.err != "" {
		t.Fatalf("loaded = %v, err = %q", m.st.loaded, m.st.err)
	}
	if v := m.st.view; v.TLSSplit != "off" || v.HTTPProxy || v.HTTPProxyPort != 8080 ||
		!v.SolveChallenges || v.KeepaliveProfile != "standard" {
		t.Fatalf("defaults = %+v", v)
	}

	// Enum rows cycle: tls_split off -> first-byte.
	m.st.cursor = settingIndex("tls_split")
	runCmds(m, m.activateSetting())
	if got := m.st.view.TLSSplit; got != "first-byte" {
		t.Fatalf("tls_split = %q, want first-byte", got)
	}
	if !m.st.pinned["tls_split"] {
		t.Fatal("tls_split should be pinned after the override")
	}

	// keepalive_profile standard -> slow.
	m.st.cursor = settingIndex("keepalive_profile")
	runCmds(m, m.activateSetting())
	if got := m.st.view.KeepaliveProfile; got != "slow" {
		t.Fatalf("keepalive_profile = %q, want slow", got)
	}
	if !m.st.pinned["keepalive_profile"] {
		t.Fatal("keepalive_profile should be pinned after the override")
	}

	// Boolean rows toggle: http_proxy off -> on, solve_challenges on -> off.
	m.st.cursor = settingIndex("http_proxy")
	runCmds(m, m.activateSetting())
	if !m.st.view.HTTPProxy {
		t.Fatal("http_proxy = off, want on")
	}
	m.st.cursor = settingIndex("solve_challenges")
	runCmds(m, m.activateSetting())
	if m.st.view.SolveChallenges {
		t.Fatal("solve_challenges = on, want off")
	}

	// Number row edits inline, prefilled with the effective value.
	m.st.cursor = settingIndex("http_proxy_port")
	if cmd := m.activateSetting(); cmd != nil {
		t.Fatal("number rows must enter edit mode, not apply")
	}
	if !m.st.editing || m.st.editBuf != "8080" {
		t.Fatalf("editing = %v, buf = %q, want 8080", m.st.editing, m.st.editBuf)
	}
	for range "8080" {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	for _, r := range "9090" {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should send the edited number")
	}
	runCmds(m, cmd)
	if m.st.editing {
		t.Fatal("editing should end after enter")
	}
	if got := m.st.view.HTTPProxyPort; got != 9090 {
		t.Fatalf("http_proxy_port = %d, want 9090", got)
	}
	if !m.st.pinned["http_proxy_port"] || !m.st.pinned["http_proxy"] || !m.st.pinned["solve_challenges"] {
		t.Fatalf("pinned = %v", m.st.pinned)
	}

	// Resetting one key drops only that override.
	_, cmd = m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if cmd == nil {
		t.Fatal("d should send a reset")
	}
	runCmds(m, cmd)
	if got := m.st.view.HTTPProxyPort; got != 8080 {
		t.Fatalf("http_proxy_port after reset = %d, want 8080", got)
	}
	if m.st.pinned["http_proxy_port"] {
		t.Fatal("http_proxy_port should no longer be pinned")
	}
}

func TestSettingsTextEditFlow(t *testing.T) {
	m := testSettingsModel()
	m.screen = screenSettings
	for i, item := range settingItems {
		if item.key == "dns_upstream" {
			m.st.cursor = i
		}
	}

	_, cmd := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("text rows must not apply on enter; expected edit mode")
	}
	if !m.st.editing || m.st.editBuf != "9.9.9.9" {
		t.Fatalf("editing = %v, buf = %q", m.st.editing, m.st.editBuf)
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyBackspace})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("8")})
	if m.st.editBuf != "9.9.9.8" {
		t.Fatalf("editBuf = %q", m.st.editBuf)
	}
	// esc cancels without applying.
	if _, cmd := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Fatal("esc during edit must not apply")
	}
	if m.st.editing || m.screen != screenSettings {
		t.Fatalf("esc should leave edit mode, still on settings screen")
	}
}

func TestSettingsEnumToggles(t *testing.T) {
	m := testSettingsModel()
	m.screen = screenSettings
	m.st.cursor = 0 // kill switch
	next := (&m.st).nextEnum(settingItems[0])
	if next != "off" {
		t.Fatalf("kill switch next = %q, want off", next)
	}
	m.st.cursor = 3 // redact targets (bool)
	if !(&m.st).boolValue("redact_targets") {
		t.Fatal("redact_targets should be on")
	}
}

func TestLogsViewFiltersAndFollows(t *testing.T) {
	m := &Model{
		screen: screenLogs,
		lg:     logsState{follow: true, minLevel: logging.Info},
	}
	m.lg.append(logging.Entry{Level: logging.Debug, Tag: "d", Msg: "hidden debug"})
	m.lg.append(logging.Entry{Level: logging.Info, Tag: "a", Msg: "hello info"})
	m.lg.append(logging.Entry{Level: logging.Warn, Tag: "b", Msg: "warn line"})

	view := m.View()
	if strings.Contains(view, "hidden debug") {
		t.Fatalf("debug entry not filtered:\n%s", view)
	}
	for _, want := range []string{"hello info", "warn line", "following"} {
		if !strings.Contains(view, want) {
			t.Errorf("logs view missing %q:\n%s", want, view)
		}
	}

	// Cycling the level filter (info → warn) hides info entries.
	m.handleLogsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	view = m.View()
	if strings.Contains(view, "hello info") || !strings.Contains(view, "warn line") {
		t.Fatalf("level filter wrong after cycle:\n%s", view)
	}
}

func TestLogsSearch(t *testing.T) {
	m := &Model{screen: screenLogs, lg: logsState{follow: true, minLevel: logging.Debug}}
	m.lg.append(logging.Entry{Level: logging.Info, Msg: "alpha line"})
	m.lg.append(logging.Entry{Level: logging.Info, Msg: "beta line"})

	m.handleLogsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	for _, r := range "bet" {
		m.handleLogsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if !m.lg.searching || m.lg.search != "bet" {
		t.Fatalf("searching = %v, search = %q", m.lg.searching, m.lg.search)
	}
	view := m.View()
	if strings.Contains(view, "alpha line") || !strings.Contains(view, "beta line") {
		t.Fatalf("search filter wrong:\n%s", view)
	}
	// esc in search mode clears it.
	m.handleLogsKey(tea.KeyMsg{Type: tea.KeyEsc})
	view = m.View()
	if m.lg.searching || m.lg.search != "" || !strings.Contains(view, "alpha line") {
		t.Fatalf("search not cleared:\n%s", view)
	}
}

func TestLogsScrollDisablesFollow(t *testing.T) {
	m := &Model{screen: screenLogs, lg: logsState{follow: true, minLevel: logging.Debug}}
	for i := 0; i < 50; i++ {
		m.lg.append(logging.Entry{Level: logging.Info, Msg: "line"})
	}
	m.handleLogsKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.lg.follow {
		t.Fatal("scrolling up should disable follow")
	}
	m.handleLogsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if !m.lg.follow || m.lg.offset != 0 {
		t.Fatal("G should return to the tail and re-enable follow")
	}
}

func TestAccountView(t *testing.T) {
	quota := int64(30 << 30)
	m := &Model{
		screen: screenAccount,
		ac: accountState{
			loaded: true,
			info: api.AccountInfo{
				Email: "user@example.com", UID: "uid-1", Subscribed: true,
				MaxBytes: &quota, TokenExpires: 1790000000,
			},
		},
	}
	view := m.View()
	for _, want := range []string{"user@example.com", "uid-1", "yes", "token expiry"} {
		if !strings.Contains(view, want) {
			t.Errorf("account view missing %q:\n%s", want, view)
		}
	}
}

func TestAccountLogoutConfirm(t *testing.T) {
	m := &Model{screen: screenAccount, ac: accountState{loaded: true, info: api.AccountInfo{Email: "x@y"}}}
	m.handleAccountKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if !m.ac.confirmLogout {
		t.Fatal("x should ask for confirmation")
	}
	m.handleAccountKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.ac.confirmLogout {
		t.Fatal("n should cancel the confirmation")
	}
}

func TestDashboardOpensNewScreens(t *testing.T) {
	cases := []struct {
		key    rune
		screen screen
	}{
		{'s', screenSettings},
		{'a', screenAccount},
		{'g', screenLogs},
	}
	for _, tc := range cases {
		m := &Model{screen: screenDashboard, status: api.Status{Authenticated: true}}
		_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tc.key}})
		if m.screen != tc.screen {
			t.Errorf("%q: screen = %v, want %v", tc.key, m.screen, tc.screen)
		}
		if cmd == nil {
			t.Errorf("%q: expected a fetch command", tc.key)
		}
	}
}

func TestHelpOverlay(t *testing.T) {
	m := &Model{screen: screenDashboard}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if !m.help {
		t.Fatal("? should open the help overlay")
	}
	if view := m.View(); !strings.Contains(view, "Keys") {
		t.Fatalf("help overlay missing:\n%s", view)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if m.help {
		t.Fatal("any key should close the overlay")
	}
}

func TestHelpDoesNotHijackTextInput(t *testing.T) {
	m := testSettingsModel()
	m.screen = screenSettings
	m.st.editing = true
	m.st.editBuf = ""
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if m.help {
		t.Fatal("? during settings editing must be typed, not open help")
	}
	if m.st.editBuf != "?" {
		t.Fatalf("editBuf = %q", m.st.editBuf)
	}
}

func TestQuotaBar(t *testing.T) {
	if got := quotaBar(50, 100, 10); got != "[█████░░░░░]" {
		t.Fatalf("quotaBar = %q", got)
	}
	if got := quotaBar(0, 0, 10); got != "" {
		t.Fatalf("quotaBar(no limit) = %q", got)
	}
	if got := quotaBar(200, 100, 4); got != "[████]" {
		t.Fatalf("quotaBar(clamped) = %q", got)
	}
}

func TestDashboardShowsUptimeAndQuotaReset(t *testing.T) {
	limit, remaining, reset := int64(100<<30), int64(50<<30), int64(1794000000)
	m := &Model{screen: screenDashboard, haveStat: true, status: api.Status{
		State: "CONNECTED", Authenticated: true,
		Since: time.Now().Add(-90 * time.Second).Unix(),
		Quota: &api.Quota{Limit: &limit, Remaining: &remaining, Reset: &reset},
	}}
	view := m.View()
	for _, want := range []string{"uptime:", "quota reset:", "["} {
		if !strings.Contains(view, want) {
			t.Errorf("dashboard missing %q:\n%s", want, view)
		}
	}
}

func TestLoadPrefs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tui.toml")
	if err := os.WriteFile(path, []byte("theme = \"mono\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUMIHO_TUI_CONFIG", path)
	if p := loadPrefs(); p.Theme != "mono" {
		t.Fatalf("theme = %q, want mono", p.Theme)
	}
	t.Setenv("KUMIHO_TUI_CONFIG", filepath.Join(dir, "missing.toml"))
	if p := loadPrefs(); p.Theme != "dark" {
		t.Fatalf("fallback theme = %q, want dark", p.Theme)
	}
}
