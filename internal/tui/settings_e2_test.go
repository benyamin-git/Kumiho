package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
)

func TestSettingsRegionRowsRendered(t *testing.T) {
	m := testSettingsModel()
	m.screen = screenSettings
	m.st.view.RelayURL = "https://relay.example.test"
	m.st.view.RelayTokenSet = true
	m.st.view.AllowLocked = true
	view := m.View()
	for _, want := range []string{
		"allow locked", "relay URL", "https://relay.example.test",
		"relay token", "set", "test relay", "enter to test",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("region settings missing %q:\n%s", want, view)
		}
	}
}

func TestSettingsRegionHintsMention451AndBrowser(t *testing.T) {
	m := testSettingsModel()
	m.screen = screenSettings
	for _, key := range []string{"relay_url", "relay_token", "relay_test"} {
		m.st.cursor = settingIndex(key)
		view := m.View()
		if !strings.Contains(view, "HTTP 451") {
			t.Fatalf("%s hint missing HTTP 451:\n%s", key, view)
		}
		if key != "relay_url" && !strings.Contains(view, "does not proxy the browser sign-in page") {
			t.Fatalf("%s hint missing the browser sign-in note:\n%s", key, view)
		}
	}
}

func TestSettingsSecretInputIsMaskedAndSaved(t *testing.T) {
	client := startFakeSettingsDaemon(t)
	m := &Model{client: client, screen: screenSettings, height: 40}
	runCmds(m, fetchSettings(m.client))
	m.st.cursor = settingIndex("relay_token")
	if cmd := m.activateSetting(); cmd != nil {
		t.Fatal("relay_token enter should start editing, not apply")
	}
	if !m.st.editing || m.st.editBuf != "" {
		t.Fatalf("editing = %v buf = %q, want empty secret input", m.st.editing, m.st.editBuf)
	}
	for _, r := range "hunter2" {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	view := m.View()
	if strings.Contains(view, "hunter2") {
		t.Fatalf("secret echoed in view:\n%s", view)
	}
	if !strings.Contains(view, "*******_") {
		t.Fatalf("masked input missing:\n%s", view)
	}
	_, cmd := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter with a non-empty secret should send it")
	}
	runCmds(m, cmd)
	if !m.st.view.RelayTokenSet {
		t.Fatal("token-set state was not reloaded")
	}
	if view := m.View(); strings.Contains(view, "hunter2") {
		t.Fatalf("secret leaked after save:\n%s", view)
	}
	if view := m.View(); !strings.Contains(view, "set") {
		t.Fatalf("token-set row missing:\n%s", view)
	}
}

func TestSettingsSecretEmptyCancels(t *testing.T) {
	m := testSettingsModel()
	m.screen = screenSettings
	m.st.view.RelayTokenSet = true
	m.st.cursor = settingIndex("relay_token")
	m.activateSetting()
	_, cmd := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("empty secret enter must cancel, not reset")
	}
	if m.st.editing {
		t.Fatal("editing should end")
	}
	if !m.st.view.RelayTokenSet {
		t.Fatal("empty enter cleared the token-set state")
	}
	if !strings.Contains(m.st.notice, "unchanged") {
		t.Fatalf("notice = %q", m.st.notice)
	}
}

func TestSettingsSecretResetClearsOnlyOnD(t *testing.T) {
	client := startFakeSettingsDaemon(t)
	m := &Model{client: client, screen: screenSettings, height: 40}
	runCmds(m, fetchSettings(m.client))
	// Set a token through the fake daemon first.
	if _, err := client.Call(t.Context(), ipc.TypeSettingsSet, api.SettingsSetPayload{Key: "relay_token", Value: "token"}); err != nil {
		t.Fatal(err)
	}
	runCmds(m, fetchSettings(m.client))
	if !m.st.view.RelayTokenSet {
		t.Fatal("token not set")
	}
	m.st.cursor = settingIndex("relay_token")
	_, cmd := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if cmd == nil {
		t.Fatal("d should clear the token")
	}
	runCmds(m, cmd)
	if m.st.view.RelayTokenSet {
		t.Fatal("d did not clear the token")
	}
}

func TestSettingsRelayTestActionNotice(t *testing.T) {
	client := startFakeSettingsDaemon(t)
	m := &Model{client: client, screen: screenSettings, height: 40}
	runCmds(m, fetchSettings(m.client))
	m.st.cursor = settingIndex("relay_test")
	cmd := m.activateSetting()
	if cmd == nil {
		t.Fatal("relay test action should return a command")
	}
	runCmds(m, cmd)
	if !strings.Contains(m.st.notice, "Relay OK — 7 ms") {
		t.Fatalf("relay notice = %q", m.st.notice)
	}
}

func TestRelayTestNotice(t *testing.T) {
	ms := 12.4
	tests := []struct {
		name string
		res  api.RelayTestResult
		want string
	}{
		{"ok", api.RelayTestResult{OK: true, LatencyMS: &ms}, "Relay OK — 12 ms"},
		{"rejected", api.RelayTestResult{Status: 401}, "Relay rejected the token"},
		{"unset", api.RelayTestResult{Error: "relay_url is not set"}, "relay_url is not set"},
		{"unreachable", api.RelayTestResult{Error: "dial tcp: refused"}, "Relay unreachable: dial tcp: refused"},
	}
	for _, tc := range tests {
		if got := relayTestNotice(tc.res); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
		}
	}
}
