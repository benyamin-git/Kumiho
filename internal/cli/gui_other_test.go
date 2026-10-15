//go:build !windows

package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunGUINonWindows(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run([]string{"gui"}, &out, &errBuf); code != 2 {
		t.Fatalf("run(gui) = %d, want 2", code)
	}
	got := errBuf.String()
	if !strings.Contains(got, "Windows-only") {
		t.Errorf("stderr = %q, want a Windows-only message", got)
	}
	if !strings.Contains(got, "TUI") {
		t.Errorf("stderr = %q, want a pointer to the TUI", got)
	}
}

func TestRunGUIListedInHelp(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run([]string{"help"}, &out, &errBuf); code != 0 {
		t.Fatalf("run(help) = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "\n  gui ") {
		t.Errorf("help output missing the gui command:\n%s", out.String())
	}
}
