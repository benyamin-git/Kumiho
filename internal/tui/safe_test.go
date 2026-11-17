package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/benyamin-git/kumiho/internal/api"
)

// TestViewStripsRemoteTerminalEscapes feeds escape-laden remote text through
// the status and error paths and asserts the rendered screen is plain.
func TestViewStripsRemoteTerminalEscapes(t *testing.T) {
	evil := "x\x1b[2J\x1b]52;c;Zm9v\x07y"

	m := &Model{screen: screenDashboard}
	next, _ := m.Update(statusMsg{status: api.Status{
		State:       "IDLE",
		Email:       evil,
		Location:    evil,
		Server:      evil,
		ExitIP:      evil,
		ExitCountry: evil,
	}})
	view := next.(*Model).View()
	for _, bad := range []string{"\x1b[2J", "\x1b]52;", "\x07"} {
		if strings.Contains(view, bad) {
			t.Fatalf("status view kept %q:\n%s", bad, view)
		}
	}

	m2 := &Model{screen: screenDashboard}
	next, _ = m2.Update(statusMsg{err: errors.New(evil)})
	view = next.(*Model).View()
	for _, bad := range []string{"\x1b[2J", "\x1b]52;", "\x07"} {
		if strings.Contains(view, bad) {
			t.Fatalf("error notice kept %q:\n%s", bad, view)
		}
	}
}
