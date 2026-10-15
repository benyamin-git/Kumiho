//go:build windows

package gui

import (
	"errors"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The shell window's class name is the vendored go-webview2 binding's own
// class: vendor/github.com/jchv/go-webview2/webview.go registers the class
// name "webview" (windows.UTF16PtrFromString("webview")) for every window it
// creates, so a second launch finds the first instance's window by that
// class name plus the shell's fixed title. The window title itself is set
// once, in the shell's window creation.
const (
	webviewWindowClass = "webview"
	windowTitle        = "Kumiho"
)

// singleInstanceMutexName is the single-instance guard's named mutex (D10).
// The Local\ prefix scopes it to the logon session, matching the per-user
// GUI: two users may each run their own shell.
const singleInstanceMutexName = `Local\KumihoGuiSingleInstance`

const (
	// focusRetryInterval and focusRetryTimeout bound the second launch's
	// search for the first instance's window: the running shell may still be
	// creating it (or still starting), so the find retries briefly.
	focusRetryInterval = 100 * time.Millisecond
	focusRetryTimeout  = 5 * time.Second
)

// singleInstanceHandle keeps the guard mutex open for the process lifetime;
// the mutex is released by the kernel at process exit.
var singleInstanceHandle windows.Handle

// acquireSingleInstance creates the guard mutex. alreadyRunning is true when
// another shell instance already owns it. A missing guard (the error case)
// must not block startup: the shell logs it and continues, matching the
// "single instance is a convenience, not a safety property" reading of D10.
func acquireSingleInstance() (alreadyRunning bool, err error) {
	name, err := windows.UTF16PtrFromString(singleInstanceMutexName)
	if err != nil {
		return false, err
	}
	h, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			return true, nil
		}
		return false, err
	}
	singleInstanceHandle = h
	return false, nil
}

// focusExistingWindow finds the first instance's window (class "webview",
// title "Kumiho") and restores and focuses it, retrying while the window is
// still being created. It is the second launch's whole behavior; the second
// process then exits 0.
func focusExistingWindow(log *hostLog) {
	class, err := windows.UTF16PtrFromString(webviewWindowClass)
	if err != nil {
		log.logf("single instance: class name: %v", err)
		return
	}
	title, err := windows.UTF16PtrFromString(windowTitle)
	if err != nil {
		log.logf("single instance: window title: %v", err)
		return
	}
	deadline := time.Now().Add(focusRetryTimeout)
	for {
		hwnd, _, _ := procFindWindowW.Call(
			uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)))
		if hwnd != 0 {
			showWindow(uintptr(hwnd), windows.SW_RESTORE)
			setForegroundWindow(uintptr(hwnd))
			log.logf("single instance: focused the running shell window %#x", hwnd)
			return
		}
		if time.Now().After(deadline) {
			log.logf("single instance: the running shell has no window to focus")
			return
		}
		time.Sleep(focusRetryInterval)
	}
}
