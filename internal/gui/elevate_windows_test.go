//go:build windows

package gui

import (
	"errors"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TestShellExecuteInfoLayout pins the SHELLEXECUTEINFOW ABI on windows/amd64:
// the exact size and every field offset the ShellExecuteExW call depends on.
// (These tests run in the Windows CI job; the linux gate only cross-checks
// that they compile.)
func TestShellExecuteInfoLayout(t *testing.T) {
	if got := unsafe.Sizeof(shellExecuteInfo{}); got != 112 {
		t.Fatalf("sizeof(shellExecuteInfo) = %d, want 112", got)
	}
	offsets := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"cbSize", unsafe.Offsetof(shellExecuteInfo{}.cbSize), 0},
		{"fMask", unsafe.Offsetof(shellExecuteInfo{}.fMask), 4},
		{"hwnd", unsafe.Offsetof(shellExecuteInfo{}.hwnd), 8},
		{"lpVerb", unsafe.Offsetof(shellExecuteInfo{}.lpVerb), 16},
		{"lpFile", unsafe.Offsetof(shellExecuteInfo{}.lpFile), 24},
		{"lpParameters", unsafe.Offsetof(shellExecuteInfo{}.lpParameters), 32},
		{"lpDirectory", unsafe.Offsetof(shellExecuteInfo{}.lpDirectory), 40},
		{"nShow", unsafe.Offsetof(shellExecuteInfo{}.nShow), 48},
		{"hInstApp", unsafe.Offsetof(shellExecuteInfo{}.hInstApp), 56},
		{"lpIDList", unsafe.Offsetof(shellExecuteInfo{}.lpIDList), 64},
		{"lpClass", unsafe.Offsetof(shellExecuteInfo{}.lpClass), 72},
		{"hkeyClass", unsafe.Offsetof(shellExecuteInfo{}.hkeyClass), 80},
		{"dwHotKey", unsafe.Offsetof(shellExecuteInfo{}.dwHotKey), 88},
		{"hIcon", unsafe.Offsetof(shellExecuteInfo{}.hIcon), 96},
		{"hProcess", unsafe.Offsetof(shellExecuteInfo{}.hProcess), 104},
	}
	for _, o := range offsets {
		if o.got != o.want {
			t.Errorf("offsetof(%s) = %d, want %d", o.name, o.got, o.want)
		}
	}
}

// TestRunElevatedRejectsEmptyFile pins the fast refusal path (no UAC prompt
// is ever shown for a programming error).
func TestRunElevatedRejectsEmptyFile(t *testing.T) {
	if _, err := runElevated("", "", ""); err == nil {
		t.Fatal("runElevated(\"\") = nil, want an error")
	}
}

// TestShellExecuteErrorMapping pins the cancellation mapping: a declined
// UAC prompt (ERROR_CANCELLED) becomes errElevationCancelled — the wording
// key — and any other failure is wrapped as the shell's error.
func TestShellExecuteErrorMapping(t *testing.T) {
	if err := shellExecuteError(windows.ERROR_CANCELLED); !errors.Is(err, errElevationCancelled) {
		t.Fatalf("shellExecuteError(ERROR_CANCELLED) = %v, want errElevationCancelled", err)
	}
	boom := errors.New("the file was not found")
	err := shellExecuteError(boom)
	if errors.Is(err, errElevationCancelled) {
		t.Fatalf("shellExecuteError(%v) = errElevationCancelled, want the shell error", boom)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("shellExecuteError(%v) = %v, want the original error wrapped", boom, err)
	}
}
