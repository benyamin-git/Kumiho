//go:build windows

package gui

// This file is the tray's behavior: the providers the shell injects, the
// exact tooltip wording, the menu built at popup time, the notification
// routing and the exported seam T4.1 (live wiring) and T4.2 (the first-close
// hint) consume. The Win32 transport — hidden window, Shell_NotifyIconW,
// icon loading, menu primitives — lives in trayicon_windows.go.

import (
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

// TrayProviders is the tray's entire view of the shell: the state it renders
// (read at creation and at every menu popup, on the shell's thread) and the
// actions its menu runs. T1.8 wires it to the controller snapshot; T4.1 adds
// the live state hooks on top.
type TrayProviders struct {
	// Tooltip returns the state word for the icon's hover text; the tray
	// renders it as "Kumiho — <word>" (the inventory's exact format).
	Tooltip func() string

	// Connected reports whether the action entry reads "Disconnect" and runs
	// OnDisconnect instead of "Connect"/OnConnect. The inventory's rule is
	// the Dashboard button's: true for Connected, Proxy only, Connecting,
	// Reconnecting and Waiting for network.
	Connected func() bool

	// Busy reports whether a tray action is in flight; the action entry is
	// disabled while true (a second click before the refresh is a no-op).
	Busy func() bool

	// Locations returns the selectable locations. The order is display
	// order (REC first, as the engine sorts); the tray sorts a copy
	// defensively.
	Locations func() []serverlist.Country

	// OnShow restores and focuses the window: the left-click behavior and
	// the first-close hint's click action.
	OnShow func()

	// OnDiagnostics opens the Diagnostics screen; T4.1 wires the screen
	// switch (T1.8 ships it landing on the restored window).
	OnDiagnostics func()

	// OnQuit exits the shell cleanly — the tray's Quit, the same path as
	// closing the window.
	OnQuit func()

	// OnConnect and OnDisconnect run the dynamic action entry.
	OnConnect    func()
	OnDisconnect func()

	// OnSelectLocation persists the picked country's code (e.g. "DE", or
	// the REC pseudo-country).
	OnSelectLocation func(countryCode string)
}

// The menu's command ids. The menu is rebuilt at every popup, so the location
// entries take consecutive ids from trayCmdLocation up, in submenu order.
const (
	trayCmdAction      = 1
	trayCmdShow        = 2
	trayCmdDiagnostics = 3
	trayCmdQuit        = 4

	trayCmdLocation = 100
)

// trayTipPrefix is the tooltip's fixed head: the inventory's exact format
// (em dash, spaces around it).
const trayTipPrefix = "Kumiho — "

// The first-close hint's copy. T4.2 owns when it shows (the close_hint_shown
// preference); T1.8 ships only the mechanism.
const (
	firstCloseHintTitle = "Kumiho"
	firstCloseHintText  = "Kumiho is still running. Right-click the tray icon to connect or quit."
)

// Tray is the shell's notification-area icon: one static fox icon whose
// hidden window lives on the shell's own message pump (the probe's proven
// pattern), a tooltip carrying the current state word, and a menu rebuilt
// from the providers at every popup. NewTray creates it inert; Install
// starts it on the shell's thread; Remove tears it down on the way out.
type Tray struct {
	icon trayIcon
	p    TrayProviders
	log  *hostLog
}

// NewTray returns a tray over the providers. It owns no Win32 state until
// Install runs.
func NewTray(p TrayProviders, log *hostLog) *Tray {
	return &Tray{p: p, log: log}
}

// Install loads the application icon and adds the tray icon to the
// notification area, creating the hidden window that receives the shell's
// callback messages. It must run on the shell's thread — the window, and the
// menu's modal loop, are thread-affine — which the shell arranges with
// WebView.Dispatch (the probe's pattern).
func (t *Tray) Install() error {
	tip := t.tipText()
	if err := t.icon.install(t, tip); err != nil {
		return err
	}
	t.log.logf("tray: icon installed tooltip=%q", tip)
	return nil
}

// Remove takes the icon out of the notification area and destroys its window
// and icon handles, in the probe's teardown order (NIM_DELETE, then
// DestroyWindow). It must run on the shell's thread, with the message loop
// already stopped; it is a safe no-op when Install never ran (or failed).
func (t *Tray) Remove() {
	t.icon.remove()
}

// RefreshTooltip re-reads the Tooltip provider and updates the icon. Safe
// from any goroutine: the update is posted to the tray's window and applied
// on the shell's thread. T4.1 calls it whenever the pushed state changes.
func (t *Tray) RefreshTooltip() {
	t.icon.postMessage(trayMsgTooltip)
}

// ShowFirstCloseHint shows the one-time close notification: a tray balloon
// whose click restores and focuses the window. T4.2 owns when to call it
// (the close_hint_shown flag); T1.8 ships only the mechanism. Safe from any
// goroutine; a no-op while the tray is not installed.
func (t *Tray) ShowFirstCloseHint() {
	t.icon.postMessage(trayMsgHint)
}

// tipText renders the tooltip for the current provider state: the exact
// "Kumiho — <state word>" format. The shell's 128-unit tip cap is enforced
// at the Win32 edge (utf16Into), so any provider word truncates safely.
func (t *Tray) tipText() string {
	word := "Loading..."
	if t.p.Tooltip != nil {
		if w := t.p.Tooltip(); w != "" {
			word = w
		}
	}
	return trayTipPrefix + word
}

// handleNotification routes one callback event from the shell (runs on the
// shell's thread): a left-click or the balloon's click shows the window (the
// ruled click model), a right-click or keyboard activation opens the menu.
func (t *Tray) handleNotification(event uint32) {
	switch event {
	case wmLButtonUp, ninBalloonUserClick:
		if t.p.OnShow != nil {
			t.p.OnShow()
		}
	case wmRButtonUp, ninKeySelect:
		t.showMenu()
	}
}

// applyTooltip pushes the current tooltip to the notification area (runs on
// the shell's thread, from a RefreshTooltip post).
func (t *Tray) applyTooltip() {
	if err := t.icon.setTip(t.tipText()); err != nil {
		t.log.logf("tray: tooltip: %v", err)
	}
}

// applyFirstCloseHint shows the balloon (runs on the shell's thread, from a
// ShowFirstCloseHint post).
func (t *Tray) applyFirstCloseHint() {
	if err := t.icon.showBalloon(firstCloseHintTitle, firstCloseHintText); err != nil {
		t.log.logf("tray: close hint: %v", err)
	}
}

// readdIcon puts the icon back after Explorer restarted (runs on the shell's
// thread, from the TaskbarCreated broadcast).
func (t *Tray) readdIcon() {
	if err := t.icon.readd(t.tipText()); err != nil {
		t.log.logf("tray: re-add after Explorer restart: %v", err)
	}
}

// showMenu builds the menu from the providers and runs its modal loop (runs
// on the shell's thread, inside the pump). Each dynamic value is read once,
// at popup time, so a label and its action can never disagree mid-menu.
func (t *Tray) showMenu() {
	hwnd := t.icon.window()
	if hwnd == 0 {
		return
	}
	m, err := newTrayMenu()
	if err != nil {
		t.log.logf("tray: %v", err)
		return
	}
	defer m.destroy()

	connected := t.p.Connected != nil && t.p.Connected()
	busy := t.p.Busy != nil && t.p.Busy()
	if connected {
		m.addItem("Disconnect", trayCmdAction, busy)
	} else {
		m.addItem("Connect", trayCmdAction, busy)
	}

	// The Location submenu: the locations in display order (REC first). An
	// empty list leaves a disabled entry in the same slot, so the command
	// set never changes shape (the inventory's stability rule).
	locs := t.locations()
	if len(locs) > 0 {
		sub, err := newTrayMenu()
		if err != nil {
			t.log.logf("tray: %v", err)
			return
		}
		defer sub.destroy()
		for i, c := range locs {
			sub.addItem(locationLabel(c), trayCmdLocation+uint32(i), false)
		}
		m.addPopup("Location", sub, false)
	} else {
		m.addItem("Location: (none)", 0, true)
	}

	m.addItem("Show Kumiho", trayCmdShow, false)
	m.addItem("Diagnostics", trayCmdDiagnostics, false)
	m.addItem("Quit", trayCmdQuit, false)

	cmd := m.track(hwnd)
	switch {
	case cmd == trayCmdAction && connected:
		if t.p.OnDisconnect != nil {
			t.p.OnDisconnect()
		}
	case cmd == trayCmdAction:
		if t.p.OnConnect != nil {
			t.p.OnConnect()
		}
	case cmd == trayCmdShow:
		if t.p.OnShow != nil {
			t.p.OnShow()
		}
	case cmd == trayCmdDiagnostics:
		if t.p.OnDiagnostics != nil {
			t.p.OnDiagnostics()
		}
	case cmd == trayCmdQuit:
		if t.p.OnQuit != nil {
			t.p.OnQuit()
		}
	case cmd >= trayCmdLocation && int(cmd-trayCmdLocation) < len(locs):
		code := locs[cmd-trayCmdLocation].Code
		if t.p.OnSelectLocation != nil {
			t.p.OnSelectLocation(code)
		}
	}
}

// locations returns the provider's list sorted for display: REC first, then
// by name (serverlist.SortCountries). The copy keeps the sort from touching
// the controller's slice, which other goroutines read.
func (t *Tray) locations() []serverlist.Country {
	if t.p.Locations == nil {
		return nil
	}
	locs := append([]serverlist.Country(nil), t.p.Locations()...)
	serverlist.SortCountries(locs)
	return locs
}

// locationLabel renders one submenu entry: the display name with its code,
// the TUI's country format. The code alone stands in for a nameless record.
func locationLabel(c serverlist.Country) string {
	if c.Name == "" {
		return c.Code
	}
	return c.Name + " (" + c.Code + ")"
}
