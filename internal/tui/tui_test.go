package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

func TestDashboardShowsState(t *testing.T) {
	m := &Model{
		screen:   screenDashboard,
		haveStat: true,
		status: ipc.Status{
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
	_, cmd := m.handleLoginState(loginStateMsg{state: ipc.LoginState{Step: "done", Email: "user@example.com"}})
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
			view: ipc.SettingsView{
				SocksPort: 1080, MTU: 8500, ExitCheck: true, DoHProvider: "automatic",
				LogLevel: "info", RedactTargets: true, KillSwitch: "last", Autoconnect: "last",
				KillSwitchEffective: true, AutoconnectEffective: true,
				QuotaPollMinutes: 15, AllowLAN: true, DNSUpstream: "9.9.9.9",
			},
			pinned: map[string]bool{"dns_upstream": true},
		},
	}
}

func TestSettingsView(t *testing.T) {
	m := testSettingsModel()
	m.screen = screenSettings
	view := m.View()
	for _, want := range []string{"Settings", "kill switch", "DNS upstream", "9.9.9.9", "log level", "applies immediately"} {
		if !strings.Contains(view, want) {
			t.Errorf("settings view missing %q:\n%s", want, view)
		}
	}
	// Hints follow the cursor; connection settings say when they apply.
	m.st.cursor = 6 // dns_upstream
	if view := m.View(); !strings.Contains(view, "next connect") {
		t.Errorf("dns_upstream hint missing 'next connect':\n%s", view)
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
			info: ipc.AccountInfo{
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
	m := &Model{screen: screenAccount, ac: accountState{loaded: true, info: ipc.AccountInfo{Email: "x@y"}}}
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
		m := &Model{screen: screenDashboard, status: ipc.Status{Authenticated: true}}
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
	m := &Model{screen: screenDashboard, haveStat: true, status: ipc.Status{
		State: "CONNECTED", Authenticated: true,
		Since: time.Now().Add(-90 * time.Second).Unix(),
		Quota: &ipc.Quota{Limit: &limit, Remaining: &remaining, Reset: &reset},
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
