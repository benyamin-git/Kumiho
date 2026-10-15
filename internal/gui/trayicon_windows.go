//go:build windows

package gui

// This file is the tray icon's Win32 transport: the hidden top-level window
// whose procedure receives the notification area's callback messages, the
// Shell_NotifyIconW registration itself, icon loading, the native popup-menu
// primitives, and the "TaskbarCreated" re-add. The behavior behind them —
// providers, menu content, actions — lives in tray_windows.go.

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Messages the tray posts to its own hidden window. trayCallbackMessage is
// the shell's notification message; it must not be WM_APP itself: the
// vendored go-webview2 binding owns exactly WM_APP for its dispatch queue
// (the probe's finding, kept here).
const (
	trayCallbackMessage = 0x8000 + 1
	trayMsgTooltip      = 0x8000 + 2
	trayMsgHint         = 0x8000 + 3

	wmNull        = 0x0000
	wmDestroy     = 0x0002
	wmContextMenu = 0x007B
	wmLButtonUp   = 0x0202
	wmRButtonUp   = 0x0205

	// Notification-area events (NIN_*) the shell reports through the
	// callback message. The tray keeps the probe's version-0 callback (it
	// never calls NIM_SETVERSION), so the event itself is lParam.
	ninKeySelect        = 0x0400 + 1 // WM_USER + 1: keyboard activation
	ninBalloonUserClick = 0x0400 + 5 // WM_USER + 5: the balloon was clicked

	nimAdd    = 0x00000000
	nimModify = 0x00000001
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004
	nifInfo    = 0x00000010

	niifInfo = 0x00000001

	mfString = 0x00000000
	mfGrayed = 0x00000001
	mfPopup  = 0x00000010

	tpmRightButton = 0x0002
	tpmNonotify    = 0x0080
	tpmReturnCmd   = 0x0100

	idiApplication = 32512 // IDI_APPLICATION

	wsPopup        = 0x80000000
	wsExToolWindow = 0x00000080

	// trayIconID is the icon's id within the tray window: one tray, one icon.
	trayIconID = 1
)

// trayClassName is the hidden window's class name (per-process, like every
// window class) and taskbarCreatedMessage is the broadcast Explorer sends
// after it recreates the taskbar, which is how the icon comes back.
const (
	trayClassName         = "KumihoTrayIcon"
	taskbarCreatedMessage = "TaskbarCreated"
)

// errTrayNotInstalled reports a tray message with no icon behind it (Install
// never ran or Remove already took it down); callers log it and move on.
var errTrayNotInstalled = errors.New("tray: not installed")

// The tray's Win32 surface, in addition to the handles chrome_windows.go
// declares once for the package; every DLL is loaded lazily, like the rest of
// the repo. CreateWindowExW's style arguments use the constants above.
var (
	shell32 = windows.NewLazySystemDLL("shell32.dll")

	procRegisterClassExW       = user32.NewProc("RegisterClassExW")
	procCreateWindowExW        = user32.NewProc("CreateWindowExW")
	procDestroyWindow          = user32.NewProc("DestroyWindow")
	procLoadIconW              = user32.NewProc("LoadIconW")
	procGetCursorPos           = user32.NewProc("GetCursorPos")
	procPostMessageW           = user32.NewProc("PostMessageW")
	procRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	procDestroyIcon            = user32.NewProc("DestroyIcon")
	procCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	procAppendMenuW            = user32.NewProc("AppendMenuW")
	procDestroyMenu            = user32.NewProc("DestroyMenu")
	procTrackPopupMenu         = user32.NewProc("TrackPopupMenu")

	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
)

// wndClassExW mirrors WNDCLASSEXW (winuser.h).
type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     windows.Handle
	hIcon         windows.Handle
	hCursor       windows.Handle
	hbrBackground windows.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       windows.Handle
}

// notifyIconDataW mirrors NOTIFYICONDATAW (shellapi.h); the full size keeps
// the call valid on every shell version the shell can meet.
type notifyIconDataW struct {
	cbSize           uint32
	hWnd             windows.HWND
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            windows.Handle
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uTimeout         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         windows.GUID
	hBalloonIcon     windows.Handle
}

// point mirrors POINT (windef.h).
type point struct{ x, y int32 }

// activeTray is the process's single tray; a Win32 window procedure carries
// no Go context and the shell owns exactly one tray, so a package variable is
// the honest wiring (the probe's and activeChrome's pattern).
var activeTray *Tray

// trayIcon is the notification-area icon's Win32 half: the hidden top-level
// window the shell delivers the icon's messages to, the Shell_NotifyIconW
// registration and the loaded HICON. Every handle here belongs to the shell's
// thread: install creates them, remove tears them down, and the window
// procedure's messages arrive on that thread's pump.
type trayIcon struct {
	// mu guards hwnd, the one field other goroutines read to post tooltip
	// and hint refreshes onto the shell's thread.
	mu   sync.Mutex
	hwnd uintptr

	// icon is the notification area's HICON. ownsIcon reports whether the
	// tray loaded it (LoadImageW) or borrowed the shared system application
	// icon, which must never be destroyed.
	icon     windows.Handle
	ownsIcon bool

	// taskbarCreated is the "TaskbarCreated" broadcast's message id (0 when
	// its registration failed). install writes it and the window procedure
	// reads it, both on the shell's thread.
	taskbarCreated uint32
}

// install creates the hidden window, loads the icon and adds it to the
// notification area. It must run on the shell's thread — the window is
// thread-affine — and leaves activeTray set so the window procedure can reach
// the tray.
func (ic *trayIcon) install(t *Tray, tip string) error {
	var hinstance windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &hinstance); err != nil {
		return fmt.Errorf("tray: module handle: %w", err)
	}
	className, err := windows.UTF16PtrFromString(trayClassName)
	if err != nil {
		return fmt.Errorf("tray: class name: %w", err)
	}

	wc := wndClassExW{
		lpfnWndProc:   windows.NewCallback(trayWndProc),
		hInstance:     hinstance,
		lpszClassName: className,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	if r, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("tray: RegisterClassExW: %w", callErr)
	}

	// Route the window procedure before the window exists: creation sends
	// messages (WM_NCCREATE onward) and the procedure looks the tray up in
	// this variable.
	activeTray = t

	// A hidden top-level popup rather than a message-only window: only
	// top-level windows receive the "TaskbarCreated" broadcast, and that
	// broadcast is how the icon returns after an Explorer restart.
	hwnd, _, callErr := procCreateWindowExW.Call(
		wsExToolWindow,
		uintptr(unsafe.Pointer(className)),
		0,
		wsPopup,
		0, 0, 0, 0,
		0, 0, uintptr(hinstance), 0)
	if hwnd == 0 {
		activeTray = nil
		return fmt.Errorf("tray: CreateWindowExW: %w", callErr)
	}

	icon, owns := loadTrayIcon(hinstance)
	if icon == 0 {
		procDestroyWindow.Call(hwnd)
		activeTray = nil
		return errors.New("tray: no notification-area icon available")
	}

	name, _ := windows.UTF16PtrFromString(taskbarCreatedMessage)
	id, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(name)))
	ic.taskbarCreated = uint32(id)

	if err := ic.add(hwnd, icon, tip); err != nil {
		procDestroyWindow.Call(hwnd)
		if owns {
			procDestroyIcon.Call(uintptr(icon))
		}
		activeTray = nil
		return err
	}

	ic.mu.Lock()
	ic.hwnd, ic.icon, ic.ownsIcon = hwnd, icon, owns
	ic.mu.Unlock()
	return nil
}

// add registers the icon with the shell for hwnd; install and the Explorer
// restart re-add share it (a restart takes the old icon down with the
// taskbar).
func (ic *trayIcon) add(hwnd uintptr, icon windows.Handle, tip string) error {
	nid := notifyIconDataW{
		hWnd:             windows.HWND(hwnd),
		uID:              trayIconID,
		uFlags:           nifMessage | nifIcon | nifTip,
		uCallbackMessage: trayCallbackMessage,
		hIcon:            icon,
	}
	utf16Into(nid.szTip[:], tip)
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	if r, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); r == 0 {
		// Shell_NotifyIcon does not set a meaningful last error.
		return errors.New("tray: Shell_NotifyIconW(NIM_ADD) failed")
	}
	return nil
}

// readd puts the icon back after Explorer restarted (TaskbarCreated); it
// runs on the shell's thread.
func (ic *trayIcon) readd(tip string) error {
	hwnd := ic.window()
	if hwnd == 0 {
		return errTrayNotInstalled
	}
	return ic.add(hwnd, ic.icon, tip)
}

// remove takes the icon out of the notification area and destroys the hidden
// window and the owned icon, the probe's teardown order. It runs on the
// shell's thread, after the message loop stopped; calling it before install
// (or twice) is a no-op.
func (ic *trayIcon) remove() {
	ic.mu.Lock()
	hwnd, icon, owns := ic.hwnd, ic.icon, ic.ownsIcon
	ic.hwnd, ic.icon, ic.ownsIcon = 0, 0, false
	ic.mu.Unlock()
	if hwnd == 0 {
		return
	}

	nid := notifyIconDataW{hWnd: windows.HWND(hwnd), uID: trayIconID}
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	procDestroyWindow.Call(hwnd)
	if owns && icon != 0 {
		procDestroyIcon.Call(uintptr(icon))
	}
	activeTray = nil
}

// setTip pushes the tooltip; it runs on the shell's thread (a trayMsgTooltip
// post handled by the window procedure).
func (ic *trayIcon) setTip(tip string) error {
	hwnd := ic.window()
	if hwnd == 0 {
		return errTrayNotInstalled
	}
	nid := notifyIconDataW{
		hWnd:   windows.HWND(hwnd),
		uID:    trayIconID,
		uFlags: nifTip,
	}
	utf16Into(nid.szTip[:], tip)
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	if r, _, _ := procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid))); r == 0 {
		return errors.New("tray: Shell_NotifyIconW(NIM_MODIFY tip) failed")
	}
	return nil
}

// showBalloon shows a notification balloon for the icon; a click on it comes
// back through the callback message as ninBalloonUserClick. It runs on the
// shell's thread.
func (ic *trayIcon) showBalloon(title, text string) error {
	hwnd := ic.window()
	if hwnd == 0 {
		return errTrayNotInstalled
	}
	nid := notifyIconDataW{
		hWnd:        windows.HWND(hwnd),
		uID:         trayIconID,
		uFlags:      nifInfo,
		dwInfoFlags: niifInfo,
	}
	utf16Into(nid.szInfoTitle[:], title)
	utf16Into(nid.szInfo[:], text)
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	if r, _, _ := procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid))); r == 0 {
		return errors.New("tray: Shell_NotifyIconW(NIM_MODIFY info) failed")
	}
	return nil
}

// window returns the hidden window's handle (0 before install or after
// remove); safe from any goroutine.
func (ic *trayIcon) window() uintptr {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	return ic.hwnd
}

// postMessage queues a tray message on the shell's thread; safe from any
// goroutine and a no-op while the tray is not installed.
func (ic *trayIcon) postMessage(msg uint32) {
	if hwnd := ic.window(); hwnd != 0 {
		procPostMessageW.Call(hwnd, uintptr(msg), 0, 0)
	}
}

// loadTrayIcon loads the notification-area icon: the embedded fox (icon group
// ID 1 of cmd/kumiho-gui/rsrc_windows.syso) scaled to the small-icon metrics,
// falling back to the shared system application icon when the resource is
// missing. owns reports whether the handle is the tray's to destroy.
func loadTrayIcon(hinstance windows.Handle) (windows.Handle, bool) {
	if r, _, _ := procLoadImageW.Call(
		uintptr(hinstance), resourceIDIcon, imageIcon,
		uintptr(systemMetric(smCXSmIcon)), uintptr(systemMetric(smCYSmIcon)), 0); r != 0 {
		return windows.Handle(r), true
	}
	r, _, _ := procLoadIconW.Call(0, idiApplication)
	return windows.Handle(r), false
}

// utf16Into writes s into dst — a fixed-size NOTIFYICONDATAW string field
// (szTip 128 units, szInfo 256, szInfoTitle 64, NUL included) — as a
// NUL-terminated UTF-16 string. Truncation at the field's cap keeps the
// terminator, so the shell never reads past the buffer, and drops a dangling
// high surrogate, so it never renders half a character pair.
func utf16Into(dst []uint16, s string) {
	u := utf16.Encode([]rune(s))
	if len(u) > len(dst)-1 {
		u = u[:len(dst)-1]
	}
	n := len(u)
	if n > 0 && u[n-1] >= 0xD800 && u[n-1] <= 0xDBFF {
		n--
	}
	for i := 0; i < n; i++ {
		dst[i] = u[i]
	}
	for i := n; i < len(dst); i++ {
		dst[i] = 0
	}
}

// trayWndProc is the hidden tray window's procedure; it runs on the shell's
// thread inside the webview's pump. Everything it does not handle falls
// through to the default procedure.
func trayWndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	t := activeTray
	if t == nil {
		return defWindowProc(hwnd, msg, wparam, lparam)
	}
	switch msg {
	case trayCallbackMessage:
		// Version-0 callback: lParam carries the event itself (a mouse
		// message, NIN_KEYSELECT, the balloon click, ...).
		t.handleNotification(uint32(lparam) & 0xFFFF)
		return 0
	case wmContextMenu:
		// The shell delivers the keyboard context menu this way on some
		// versions instead of through the callback message.
		t.showMenu()
		return 0
	case trayMsgTooltip:
		t.applyTooltip()
		return 0
	case trayMsgHint:
		t.applyFirstCloseHint()
		return 0
	case wmDestroy:
		return 0
	}
	if id := t.icon.taskbarCreated; id != 0 && uint32(msg) == id {
		t.readdIcon()
		return 0
	}
	return defWindowProc(hwnd, msg, wparam, lparam)
}

// trayMenu is a native popup menu under construction; every method runs on
// the shell's thread. A submenu is destroyed by its own destroy, after (not
// through) the parent's.
type trayMenu struct{ h uintptr }

// newTrayMenu creates an empty popup menu.
func newTrayMenu() (*trayMenu, error) {
	h, _, callErr := procCreatePopupMenu.Call()
	if h == 0 {
		return nil, fmt.Errorf("tray: CreatePopupMenu: %w", callErr)
	}
	return &trayMenu{h: h}, nil
}

// addItem appends one command entry with the given id; disabled entries are
// greyed (the inventory's busy/informational rendering).
func (m *trayMenu) addItem(label string, id uint32, disabled bool) {
	flags := uintptr(mfString)
	if disabled {
		flags |= mfGrayed
	}
	procAppendMenuW.Call(m.h, flags, uintptr(id), uintptr(unsafe.Pointer(menuLabel(label))))
}

// addPopup appends sub as a submenu; sub stays the caller's to destroy.
func (m *trayMenu) addPopup(label string, sub *trayMenu, disabled bool) {
	flags := uintptr(mfPopup | mfString)
	if disabled {
		flags |= mfGrayed
	}
	procAppendMenuW.Call(m.h, flags, sub.h, uintptr(unsafe.Pointer(menuLabel(label))))
}

// track runs the menu's modal loop and returns the chosen command id (0 when
// dismissed). owner is the icon's hidden window: the menu needs it
// foreground for as long as it is up (the classic TrackPopupMenu dance) and
// gets the documented WM_NULL chaser so the next click-outside dismisses.
func (m *trayMenu) track(owner uintptr) uint32 {
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(owner)
	cmd, _, _ := procTrackPopupMenu.Call(
		m.h, tpmRightButton|tpmReturnCmd|tpmNonotify,
		uintptr(int64(pt.x)), uintptr(int64(pt.y)), 0, owner, 0)
	procPostMessageW.Call(owner, wmNull, 0, 0)
	return uint32(cmd)
}

// destroy releases the menu.
func (m *trayMenu) destroy() {
	procDestroyMenu.Call(m.h)
}

// menuLabel converts a menu label to the UTF-16 pointer AppendMenuW reads.
// Labels can carry daemon-controlled location names, and a JSON string may
// legally hold NULs, which UTF16PtrFromString refuses; they are stripped so
// a hostile record can only degrade its own menu entry.
func menuLabel(label string) *uint16 {
	if strings.IndexByte(label, 0) >= 0 {
		label = strings.ReplaceAll(label, "\x00", "")
	}
	p, _ := windows.UTF16PtrFromString(label)
	return p
}
