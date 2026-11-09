package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/benyamin-git/kumiho/internal/api"
)

func TestRegionNoticeEnterOpensGuide(t *testing.T) {
	m := &Model{screen: screenDashboard, noticeRegion: true, notice: "tunnel: blocked"}
	m.status = api.Status{Authenticated: true, Email: "user@example.com", State: "IDLE",
		PassExpires: time.Now().Add(5 * time.Minute).Unix()}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.guide {
		t.Fatal("Enter on the region notice did not open the guide")
	}
	view := m.View()
	for _, want := range []string{"Blocked region", "Option 1: I have another VPN", "Option 2: Use a relay", "Pass ready"} {
		if !strings.Contains(view, want) {
			t.Fatalf("guide missing %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "4:5") {
		t.Fatalf("guide missing the countdown:\n%s", view)
	}
}

func TestSettingsBlockedRegionRowOpensGuide(t *testing.T) {
	m := &Model{client: nil, screen: screenSettings}
	for i, item := range settingItems {
		if item.key == "blocked_region" {
			m.st.cursor = i
		}
	}
	if settingItems[m.st.cursor].key != "blocked_region" {
		t.Fatal("blocked_region row is missing from Settings")
	}
	_ = m.activateSetting()
	if !m.guide {
		t.Fatal("the Settings row did not open the guide")
	}
}

func TestGuideRecheckAndClose(t *testing.T) {
	m := &Model{guide: true, screen: screenDashboard}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if !m.guideChecking {
		t.Fatal("r did not start a recheck")
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.guide {
		t.Fatal("esc did not close the guide")
	}
}

func TestDashboardPassCountdown(t *testing.T) {
	m := &Model{haveStat: true, status: api.Status{State: "IDLE", PassExpires: time.Now().Add(90 * time.Second).Unix()}}
	view := m.View()
	if !strings.Contains(view, "pass expiry:") || !strings.Contains(view, "1:2") {
		t.Fatalf("dashboard missing the pass countdown:\n%s", view)
	}
}
