//go:build windows

package gui

import (
	"errors"
	"os"
	"path/filepath"

	"testing"
)

// TestServiceStateWord pins the SCM-to-card word mapping, including the
// unknown-state fallback.
func TestServiceStateWord(t *testing.T) {
	tests := []struct {
		state uint32
		want  string
	}{
		{0x00000001, "stopped"},       // SERVICE_STOPPED
		{0x00000002, "start pending"}, // SERVICE_START_PENDING
		{0x00000003, "stop pending"},  // SERVICE_STOP_PENDING
		{0x00000004, "running"},       // SERVICE_RUNNING
		{0x00000005, "continue pending"},
		{0x00000006, "pause pending"},
		{0x00000007, "paused"},
		{0x0000007f, "state 127"},
	}
	for _, tt := range tests {
		if got := serviceStateWord(tt.state); got != tt.want {
			t.Errorf("serviceStateWord(%d) = %q, want %q", tt.state, got, tt.want)
		}
	}
}

// TestKumihoExeBeside pins the CLI lookup beside the running app: a
// kumiho.exe in the directory wins, a directory or a missing file does not.
func TestKumihoExeBeside(t *testing.T) {
	dir := t.TempDir()
	if p, ok := kumihoExeBeside(dir); ok || p != "" {
		t.Fatalf("kumihoExeBeside(empty) = (%q, %v), want not found", p, ok)
	}

	exe := filepath.Join(dir, "kumiho.exe")
	if err := os.WriteFile(exe, []byte("stub"), 0o600); err != nil {
		t.Fatal(err)
	}
	if p, ok := kumihoExeBeside(dir); !ok || p != exe {
		t.Fatalf("kumihoExeBeside = (%q, %v), want (%q, true)", p, ok, exe)
	}

	// A directory named kumiho.exe is not an executable.
	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(exe, 0o700); err != nil {
		t.Fatal(err)
	}
	if p, ok := kumihoExeBeside(dir); ok {
		t.Fatalf("kumihoExeBeside(dir-named-exe) = %q, want not found", p)
	}
}

// TestServiceOutcomeNotice pins the start/stop notice wording: the success
// words, the declined-consent wording, the non-zero exit code and the launch
// error.
func TestServiceOutcomeNotice(t *testing.T) {
	tests := []struct {
		name    string
		verb    string
		code    int
		err     error
		wantMsg string
		wantT   string
	}{
		{"started", "start", 0, nil, "Service started", toneGood},
		{"stopped", "stop", 0, nil, "Service stopped", toneGood},
		{"cancelled", "start", 0, errElevationCancelled,
			"could not start the service: administrator approval was cancelled", toneBad},
		{"launch error", "stop", 0, errors.New("no such file"),
			"could not stop the service: no such file", toneBad},
		{"exit code", "start", 1060, nil,
			"could not start the service: the command exited with code 1060", toneBad},
	}
	for _, tt := range tests {
		msg, tone := serviceOutcomeNotice(tt.verb, tt.code, tt.err)
		if msg != tt.wantMsg || tone != tt.wantT {
			t.Errorf("%s: serviceOutcomeNotice = (%q, %q), want (%q, %q)", tt.name, msg, tone, tt.wantMsg, tt.wantT)
		}
	}
}

// TestCleanupOutcomeNotice pins the cleanup notice wording: the inventory's
// `Cleanup finished` success and the failure wording.
func TestCleanupOutcomeNotice(t *testing.T) {
	tests := []struct {
		name    string
		code    int
		err     error
		wantMsg string
		wantT   string
	}{
		{"finished", 0, nil, "Cleanup finished", toneGood},
		{"cancelled", 0, errElevationCancelled,
			"could not clean up: administrator approval was cancelled", toneBad},
		{"launch error", 0, errors.New("kumiho.exe was not found next to the Kumiho app"),
			"could not clean up: kumiho.exe was not found next to the Kumiho app", toneBad},
		{"exit code", 1, nil, "could not clean up: the command exited with code 1", toneBad},
	}
	for _, tt := range tests {
		msg, tone := cleanupOutcomeNotice(tt.code, tt.err)
		if msg != tt.wantMsg || tone != tt.wantT {
			t.Errorf("%s: cleanupOutcomeNotice = (%q, %q), want (%q, %q)", tt.name, msg, tone, tt.wantMsg, tt.wantT)
		}
	}
}

// TestServiceSnapshotLiveShape exercises the real read-only SCM query: on a
// normal host it answers "not installed" (the CI image has no kumiho
// service) or a real state for an installed service; a refused SCM access
// must still return a usable word.
func TestServiceSnapshotLiveShape(t *testing.T) {
	snap, err := serviceSnapshot()
	if err != nil {
		if snap.State == "" {
			t.Fatalf("failure snapshot = %+v, want a state word", snap)
		}
		return
	}
	switch snap.State {
	case "not installed":
		if snap.Installed {
			t.Fatalf("snapshot = %+v, want installed=false", snap)
		}
	case "running", "stopped", "start pending", "stop pending",
		"paused", "continue pending", "pause pending":
		if !snap.Installed {
			t.Fatalf("snapshot = %+v, want installed=true", snap)
		}
	default:
		t.Fatalf("snapshot = %+v, want a known state word", snap)
	}
}
