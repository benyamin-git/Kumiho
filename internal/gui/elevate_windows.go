//go:build windows

package gui

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The UAC elevation helper (T3.3): ShellExecuteExW with the "runas" verb
// shows the consent prompt, SEE_MASK_NOCLOSEPROCESS hands back the process
// handle, and runElevated waits for the process and returns its exit code.
// service_windows.go drives `sc.exe start/stop` and `kumiho cleanup` with
// it. Everything here is windows-only and testable only on Windows (the
// layout and the non-elevating paths are pinned by elevate_windows_test.go).

const (
	// seeMaskNoCloseProcess keeps the launched process's handle in
	// SHELLEXECUTEINFO.hProcess; without it the handle is closed and the
	// exit code unreadable.
	seeMaskNoCloseProcess = 0x00000040
	// seeMaskNoAsync makes ShellExecuteExW not return before the operation
	// completed (the documented runas companion flag).
	seeMaskNoAsync = 0x00000100
	// elevatedWaitTimeout bounds the wait on the elevated process. `sc start`
	// waits for the service to run and `kumiho cleanup` is quick; two
	// minutes is far beyond both, and a stuck process must not pin the shell
	// forever.
	elevatedWaitTimeout = 2 * time.Minute
)

// errElevationCancelled reports a declined UAC consent prompt
// (ERROR_CANCELLED); callers word it distinctly from a real failure.
var errElevationCancelled = errors.New("administrator approval was cancelled")

// shellExecuteInfo mirrors SHELLEXECUTEINFOW (winuser.h) for windows/amd64.
// Field order and widths match the C struct exactly; the compile-time size
// assertion below catches layout drift, and elevate_windows_test.go pins the
// field offsets.
type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         windows.Handle
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     windows.Handle
	lpIDList     unsafe.Pointer
	lpClass      *uint16
	hkeyClass    windows.Handle
	dwHotKey     uint32
	hIcon        windows.Handle
	hProcess     windows.Handle
}

// The struct must stay exactly SHELLEXECUTEINFOW's amd64 size (112 bytes):
// indexing a one-element array with the deviation as a constant is a
// compile-time assertion — any other size is a negative or out-of-range
// index.
var _ = [1]struct{}{}[unsafe.Sizeof(shellExecuteInfo{})-112]

var (
	modshell32          = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteExW = modshell32.NewProc("ShellExecuteExW")
)

// runElevated launches file with params through the ShellExecuteExW "runas"
// verb — Windows shows the UAC consent prompt — hiding the console window,
// waits for the process to exit and returns its exit code. A declined prompt
// returns errElevationCancelled; any other launch failure returns the
// shell's error. dir, when non-empty, becomes the process's working
// directory.
func runElevated(file, params, dir string) (int, error) {
	if file == "" {
		return 0, errors.New("runElevated: no executable")
	}
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return 0, err
	}
	filePtr, err := windows.UTF16PtrFromString(file)
	if err != nil {
		return 0, err
	}
	var paramsPtr, dirPtr *uint16
	if params != "" {
		if paramsPtr, err = windows.UTF16PtrFromString(params); err != nil {
			return 0, err
		}
	}
	if dir != "" {
		if dirPtr, err = windows.UTF16PtrFromString(dir); err != nil {
			return 0, err
		}
	}
	sei := shellExecuteInfo{
		cbSize:       uint32(unsafe.Sizeof(shellExecuteInfo{})),
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync,
		lpVerb:       verb,
		lpFile:       filePtr,
		lpParameters: paramsPtr,
		lpDirectory:  dirPtr,
		// The elevated tools are console programs: hide their windows; the
		// outcome is reported in-app.
		nShow: windows.SW_HIDE,
	}
	if err := shellExecuteEx(&sei); err != nil {
		return 0, err
	}
	if sei.hProcess == 0 {
		return 0, errors.New("the elevated process could not be tracked")
	}
	defer windows.CloseHandle(sei.hProcess)

	event, err := windows.WaitForSingleObject(sei.hProcess, uint32(elevatedWaitTimeout/time.Millisecond))
	if err != nil {
		return 0, fmt.Errorf("wait for the elevated process: %w", err)
	}
	if event == uint32(windows.WAIT_TIMEOUT) {
		return 0, errors.New("the elevated command did not finish in time")
	}
	var code uint32
	if err := windows.GetExitCodeProcess(sei.hProcess, &code); err != nil {
		return 0, fmt.Errorf("read the elevated process exit code: %w", err)
	}
	return int(code), nil
}

// shellExecuteEx calls ShellExecuteExW; a declined consent prompt maps to
// errElevationCancelled.
func shellExecuteEx(sei *shellExecuteInfo) error {
	r, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(sei)))
	if r == 0 {
		return shellExecuteError(callErr)
	}
	return nil
}

// shellExecuteError maps one ShellExecuteExW failure: a declined consent
// prompt is the distinguished errElevationCancelled; everything else is the
// shell's error.
func shellExecuteError(err error) error {
	if errors.Is(err, windows.ERROR_CANCELLED) {
		return errElevationCancelled
	}
	return fmt.Errorf("ShellExecuteEx: %w", err)
}
