//go:build windows

package gui

import "testing"

// TestAutostartCommand pins the D11 Run entry command line: the quoted
// executable path followed by the logon flag, so a path with spaces survives
// and a logon launch starts hidden in the tray.
func TestAutostartCommand(t *testing.T) {
	got := autostartCommand(`C:\Program Files\Kumiho\kumiho-gui.exe`)
	want := `"C:\Program Files\Kumiho\kumiho-gui.exe" --tray-start`
	if got != want {
		t.Errorf("autostartCommand = %q, want %q", got, want)
	}
}
