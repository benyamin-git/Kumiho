//go:build windows

package gui

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
	"golang.org/x/sys/windows"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/gui/prefs"
	"github.com/benyamin-git/kumiho/internal/gui/theme"
	"github.com/benyamin-git/kumiho/internal/gui/web"
	"github.com/benyamin-git/kumiho/internal/platform/auto"
	"github.com/benyamin-git/kumiho/internal/serverlist"
	"github.com/benyamin-git/kumiho/internal/version"
)

const (
	// pushInterval is the host→UI snapshot cadence (the inventory's 250 ms UI
	// pump): how often the shell checks the controller snapshot for changes.
	pushInterval = 250 * time.Millisecond

	// serviceProbeInterval is how often the shell re-probes the SCM for the
	// service card (T3.3), so a service started or stopped outside the GUI
	// shows up on the Diagnostics screen.
	serviceProbeInterval = 5 * time.Second

	// webviewDownloadURL is Microsoft's stable evergreen link for the
	// WebView2 Runtime bootstrapper (D15: nothing ships one, point at this).
	webviewDownloadURL = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"
)

// procAttachConsole is the console attachment used by --version/--help.
var procAttachConsole = kernel32.NewProc("AttachConsole")

// RunHost runs the Windows shell and returns the process exit code. It owns
// the whole process: prefs → theme → asset server → window → bridge →
// controller, the message pump, close-to-tray and the host log. It is the
// kumiho-gui command's entire behavior.
func RunHost(args []string) int {
	opts, err := parseHostArgs(args)
	if err != nil {
		attachParentConsole()
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if opts.showVersion || opts.showHelp {
		// The GUI subsystem build starts without a console; attach to the
		// invoking one (a no-op when there is none, e.g. Explorer) so both
		// the interactive and the redirected (`> out.txt`) forms work.
		attachParentConsole()
		if opts.showVersion {
			fmt.Fprintf(os.Stdout, "kumiho-gui %s %s\n", version.Version, version.Commit)
		} else {
			fmt.Fprint(os.Stdout, hostUsage)
		}
		return 0
	}

	// The window, the WebView2 control, the message pump and the tray are
	// thread-affine; lock main so the whole shell lives on one thread.
	runtime.LockOSThread()

	log := openHostLog()
	defer log.close()

	if already, err := acquireSingleInstance(); err != nil {
		log.logf("single instance: guard mutex: %v", err)
	} else if already {
		log.logf("single instance: another shell is running; focusing it")
		focusExistingWindow(log)
		return 0
	}

	h := &host{log: log, opts: opts}
	if err := h.run(); err != nil {
		h.log.logf("fatal: %v", err)
		switch {
		case errors.Is(err, errWebView2Missing):
			fatalDialog("Microsoft Edge WebView2 Runtime is not installed.\r\n\r\n"+
				"Kumiho renders its interface with it and cannot start without it.\r\n\r\n"+
				"Open the Microsoft WebView2 Runtime download page in your browser?", true)
		case errors.Is(err, errWebView2Failed):
			fatalDialog("Kumiho could not create its window.\r\n\r\n"+err.Error()+
				"\r\n\r\nOpen the Microsoft WebView2 Runtime download page in your browser?", true)
		default:
			fatalDialog("Kumiho could not start:\r\n\r\n"+err.Error(), false)
		}
		return 1
	}
	return 0
}

// hostOptions are the shell's command line flags.
type hostOptions struct {
	showVersion bool
	showHelp    bool
	trayStart   bool
}

const hostUsage = `Usage: kumiho-gui [options]

Kumiho desktop shell (Windows).

Options:
  --tray-start   start with the window hidden
  --version, -v  print the version and exit
  --help, -h     print this help and exit
`

// parseHostArgs parses the shell's flags; an unknown flag is an error (the
// caller reports usage and exits 2).
func parseHostArgs(args []string) (hostOptions, error) {
	var opts hostOptions
	for _, a := range args {
		switch a {
		case "--version", "-v":
			opts.showVersion = true
		case "--help", "-h":
			opts.showHelp = true
		case "--tray-start":
			opts.trayStart = true
		default:
			return hostOptions{}, fmt.Errorf("kumiho-gui: unknown option %q (see --help)", a)
		}
	}
	return opts, nil
}

// attachParentConsole attaches a --version/--help run to the invoking
// console and reopens the standard output handles on it. Without a parent
// console (Explorer) it does nothing; with stdout redirected to a file, the
// inherited handle is kept as-is. It never blocks or fails the caller.
func attachParentConsole() {
	const attachParentProcess = ^uintptr(0) // (DWORD)-1
	if r, _, _ := procAttachConsole.Call(attachParentProcess); r == 0 {
		return
	}
	reopenStd(windows.STD_OUTPUT_HANDLE, "stdout", &os.Stdout)
	reopenStd(windows.STD_ERROR_HANDLE, "stderr", &os.Stderr)
}

// reopenStd reopens *f on the standard handle when it does not already wrap
// it; the guard keeps a redirected handle (which os.Stdout already holds)
// untouched.
func reopenStd(std uint32, name string, f **os.File) {
	h, err := windows.GetStdHandle(std)
	if err != nil || h == 0 || h == windows.InvalidHandle {
		return
	}
	if (*f).Fd() == uintptr(h) {
		return
	}
	*f = os.NewFile(uintptr(h), name)
}

// errWebView2Missing and errWebView2Failed select the WebView2 fallback
// dialog (D15) for the failures it can fix by installing the runtime.
var (
	errWebView2Missing = errors.New("Microsoft Edge WebView2 Runtime is not installed")
	errWebView2Failed  = errors.New("webview2 environment creation failed")
)

// fatalDialog is the shell's last-resort native dialog (D15): it is shown
// when the window cannot come up at all. With download it names the missing
// WebView2 runtime and offers Microsoft's download page in the default
// browser.
func fatalDialog(body string, download bool) {
	title, _ := windows.UTF16PtrFromString(windowTitle)
	text, _ := windows.UTF16PtrFromString(body)
	flags := uint32(windows.MB_OK | windows.MB_ICONERROR | windows.MB_SETFOREGROUND)
	if download {
		flags = windows.MB_YESNO | windows.MB_ICONWARNING | windows.MB_SETFOREGROUND
	}
	r, err := windows.MessageBox(0, text, title, flags)
	if err != nil || !download || r != 6 { // IDYES
		return
	}
	verb, _ := windows.UTF16PtrFromString("open")
	link, _ := windows.UTF16PtrFromString(webviewDownloadURL)
	_ = windows.ShellExecute(0, verb, link, nil, nil, windows.SW_SHOWNORMAL)
}

// host is the shell's process composition root: it owns the preferences, the
// asset server, the window and its chrome, the control connection, the
// controller and the bridge between the page and the controller.
type host struct {
	log  *hostLog
	opts hostOptions

	prefs   prefs.Prefs
	server  *web.Server
	webview webview2.WebView
	chrome  *windowChrome
	client  *DaemonClient
	ctrl    *Controller
	tray    *Tray
	// trayHook is the tooltip's live state watcher (T4.1); the push loop
	// drives it and it posts refreshes onto the shell's thread.
	trayHook *trayStateHook

	// mu guards prefs (geometry saves on the window thread, prefs_set on the
	// bridge thread).
	mu sync.Mutex

	// autostartMu serialises Run-key writes: the launch-time mirror (its own
	// goroutine) and the Settings toggle (the bridge thread) both go through
	// syncAutostartFromPrefs, which takes autostartMu first and only then
	// reads the flag under mu (lock order autostartMu → mu; nothing takes
	// them the other way), so a slow launch write can never overwrite a
	// newer toggle.
	autostartMu sync.Mutex

	// svcMu guards the shell's last published service snapshot (T3.3); the
	// probe loop, the action completions and the UI-side reads all cross
	// goroutines.
	svcMu   sync.Mutex
	service ServiceState

	// trayBusy guards re-entry on the tray's action entry (T1.8); it is
	// touched from the shell thread (menu popups) and action goroutines.
	trayBusy atomic.Bool

	// serviceBusy serialises the host-side cleanup/service actions (T3.3):
	// one elevated operation at a time; a second click while one is in
	// flight is a logged no-op.
	serviceBusy atomic.Bool

	cancel        context.CancelFunc
	terminateOnce sync.Once
	shutdownOnce  sync.Once
}

// run is the shell's startup sequence and main loop: prefs → theme → asset
// server → WebView2 window → chrome → bridge → controller → pump. It returns
// when the pump ends (quit), after the shutdown sequence.
func (h *host) run() error {
	defer h.shutdown()

	p, err := prefs.Load()
	if err != nil {
		// A malformed gui.toml must not block the shell (support scenario):
		// log it and start from the first-run appearance; the next save
		// rewrites the file in the current schema.
		h.log.logf("prefs: %v (starting from defaults)", err)
		p = prefs.Prefs{Theme: string(theme.Light), Accent: theme.Accents[0].Name, Autostart: true}
	}
	h.prefs = p

	// The stored autostart flag is mirrored into the per-user Run entry at
	// every launch (inventory §Tray/app lifecycle), so the logon registration
	// always matches gui.toml even if it was changed elsewhere. The registry
	// write runs off the startup path and a failure is only logged: the
	// Settings toggle surfaces its own outcome.
	go func() {
		if err := h.syncAutostartFromPrefs(); err != nil {
			h.log.logf("autostart: %v", err)
		}
	}()

	th, accent := effectiveAppearance(h.prefs)
	css, err := theme.CSS(th, accent)
	if err != nil {
		return fmt.Errorf("theme: %w", err)
	}

	h.server = web.New(web.StaticFS(), web.Options{Token: newSessionToken()})
	if err := h.server.Start(); err != nil {
		return fmt.Errorf("asset server: %w", err)
	}
	h.server.SetThemeCSS(css)

	installed, err := webviewloader.GetInstalledVersion()
	if err != nil {
		return fmt.Errorf("webview2 runtime detection: %w", err)
	}
	if installed == "" {
		return errWebView2Missing
	}
	h.log.logf("webview2 runtime: %s", installed)

	placement := placementFor(h.prefs)
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		// D34 hardening: debug off turns the default context menus and the
		// DevTools off in the vendored binding (verified in
		// vendor/github.com/jchv/go-webview2/webview.go, NewWithOptions).
		Debug:     false,
		DataPath:  webviewDataPath(h.log),
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  windowTitle,
			Width:  uint(placement.w),
			Height: uint(placement.h),
		},
	})
	if w == nil {
		// The binding's asynchronous environment callback calls log.Fatalf
		// on some failures (vendored behavior); the synchronous failure path
		// returns nil here.
		return errWebView2Failed
	}
	h.webview = w

	hwnd := uintptr(w.Window())
	h.chrome = newWindowChrome(hwnd, h.saveGeometry, h.handleClose)
	if err := h.chrome.install(); err != nil {
		// Without the subclass, close destroys the window and exits the
		// shell; the window itself is still fully usable.
		h.log.logf("chrome: %v", err)
	}
	applyWindowIcon(hwnd, h.log)
	applyTitlebarTheme(hwnd, isDarkTheme(th))
	applyWindowGeometry(hwnd, placement)
	w.SetSize(prefs.MinWidth, prefs.MinHeight, webview2.HintMin)
	if h.opts.trayStart {
		// The Run entry's logon start (D11) hides immediately after the
		// geometry restore — before the binding and the first navigation —
		// keeping the visible flash the binding's own ShowWindow causes as
		// short as possible.
		showWindow(hwnd, windows.SW_HIDE)
	}

	if err := w.Bind("kumihoAction", h.handleAction); err != nil {
		return fmt.Errorf("webview2 bind: %w", err)
	}
	w.Navigate(h.server.URL())

	h.client = NewDaemonClient()
	autostart := h.prefs.Autostart
	h.ctrl = NewController(h.client, Options{
		Theme:          string(th),
		Accent:         accent,
		Onboarded:      h.prefs.Onboarded,
		Autostart:      &autostart,
		LogsExport:     h.logsExport,
		OpenLogsFolder: h.openLogsFolder,
	})
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	// The tray and its state hook exist before the push loop starts: the loop
	// observes every snapshot to keep the tooltip live, and constructing the
	// hook first is what makes that read race-free. Only the Win32 install is
	// dispatched onto the pump.
	h.installTray()
	go h.ctrl.Poll(ctx)
	go h.pushLoop(ctx)
	go h.serviceLoop(ctx)

	state := "shown"
	if h.opts.trayStart {
		state = "hidden (--tray-start)"
	}
	h.log.logf("start version=%s commit=%s pid=%d theme=%s accent=%s control=%s",
		version.Version, version.Commit, os.Getpid(), th, accent, auto.Current().ControlEndpoint())
	h.log.logf("window %s hwnd=%#x placement=%+v", state, hwnd, placement)

	// Run owns the message pump until quit; on return the deferred shutdown
	// closes the server and the control connection.
	w.Run()
	return nil
}

// shutdown tears the shell down exactly once, on every exit path: the tray
// (removed before the control connection, the inventory's order), geometry,
// the controller pump and the control connection, then the asset server.
func (h *host) shutdown() {
	h.shutdownOnce.Do(func() {
		if h.tray != nil {
			h.tray.Remove()
		}
		if h.chrome != nil {
			h.chrome.save()
		}
		if h.cancel != nil {
			h.cancel()
		}
		if h.client != nil {
			_ = h.client.Close()
		}
		if h.server != nil {
			_ = h.server.Close()
		}
		h.log.logf("exit")
	})
}

// handleAction is the UI→host half of the bridge: the page calls
// kumihoAction(<envelope string>) with a {"type":"action",...} envelope.
// window_show, quit, open_external and prefs_set are the shell's own fast
// actions (D34): they run synchronously, and their result is "" on success or
// the error message. Controller actions are accepted only: the binding's
// synchronous callback thread must never block on a daemon call (connect and
// disconnect take up to 90 s, login 60 s), so Dispatch runs on its own
// goroutine — the controller's documented contract for callers that must stay
// responsive — and the page gets "" immediately. Controller failures reach
// the UI through the controller's own state notices; the host only logs
// them. Consequently two rapid UI actions may run concurrently, which the
// mutex-guarded controller supports, and "kumihoAction returned \"\"" means
// accepted, not completed (the T2.1 transport contract).
func (h *host) handleAction(envelope string) string {
	act, err := DecodeAction([]byte(envelope))
	if err != nil {
		h.log.logf("bridge: %v", err)
		return err.Error()
	}
	switch act.Name {
	case ActionWindowShow:
		h.webview.Dispatch(func() { h.chrome.show() })
	case ActionQuit:
		h.requestQuit()
	case ActionOpenExternal:
		err = h.openExternal(act)
	case ActionPrefsSet:
		err = h.applyPrefs(act)
	case ActionCleanup, ActionServiceStart, ActionServiceStop:
		// The elevated service/cleanup actions are the shell's own
		// intercepts (T3.3), like prefs_set: the shell runs them on their
		// own goroutine (UAC consent can take arbitrarily long and the
		// binding's callback thread must stay free) and publishes the
		// outcome through the controller.
		h.serviceAction(act)
		return ""
	default:
		h.dispatchAsync(act)
		return ""
	}
	if err != nil {
		h.logDispatchError(act, err)
		return err.Error()
	}
	return ""
}

// dispatchAsync runs one controller action in the background; Dispatch may
// block for the action's full daemon timeout, and the binding's callback
// thread owns the window's message processing, so it must stay free. The
// controller is mutex-guarded and serialises its state itself.
func (h *host) dispatchAsync(act Action) {
	go func() {
		if err := h.ctrl.Dispatch(act); err != nil {
			h.logDispatchError(act, err)
		}
	}()
}

// logDispatchError records one action failure; a dial failure is logged as
// the control-pipe failure it is (the same wording the push loop uses).
func (h *host) logDispatchError(act Action, err error) {
	if errors.Is(err, ErrDaemonUnreachable) {
		h.log.logf("ipc: %s: %v", act.Name, err)
	} else {
		h.log.logf("action %s: %v", act.Name, err)
	}
}

// requestQuit is the clean exit path (the UI's quit action; T1.8's tray Quit
// joins it): geometry is saved and Terminate is dispatched onto the pump
// thread, because PostQuitMessage is thread-affine. The deferred shutdown
// closes the rest.
func (h *host) requestQuit() {
	h.terminateOnce.Do(func() {
		h.log.logf("quit requested")
		if h.chrome != nil {
			h.chrome.save()
		}
		h.webview.Dispatch(func() { h.webview.Terminate() })
	})
}

// handleClose is the window's WM_CLOSE path (D10): on the first close it
// records close_hint_shown in gui.toml and shows the tray's one-time
// notification (T1.8's ShowFirstCloseHint); every close saves the geometry
// and hides the window. The shell keeps running; the tray and the UI's quit
// action are the exit paths. The prefs write is one small atomic file write
// (temp + rename), bounded enough for the pump thread, and a failure is
// logged without losing the hint for the session.
func (h *host) handleClose() {
	h.mu.Lock()
	first := !h.prefs.CloseHintShown
	if first {
		h.prefs.CloseHintShown = true
		if err := prefs.Save(h.prefs); err != nil {
			h.log.logf("prefs: save close hint: %v", err)
		}
	}
	h.mu.Unlock()
	h.chrome.hide()
	if first && h.tray != nil {
		h.tray.ShowFirstCloseHint()
	}
}

// installTray creates the notification-area icon and its state hook (T1.8,
// T4.1). The window, the icon handle and the menu's modal loop are
// thread-affine, so the install itself is dispatched onto the pump (the
// probe's pattern); the shell thread callback runs before Run processes any
// click. The providers read the controller's snapshot when they are called —
// at creation, at every menu popup and at every tooltip refresh — and the
// actions reuse the shell's existing exit and dispatch paths. A failure is
// logged: the shell runs on without an icon.
func (h *host) installTray() {
	h.tray = NewTray(TrayProviders{
		Tooltip:          h.trayStateWord,
		Connected:        h.trayConnected,
		Busy:             h.trayBusy.Load,
		Locations:        h.trayLocations,
		OnShow:           h.showFromTray,
		OnDiagnostics:    h.showDiagnosticsFromTray,
		OnQuit:           h.requestQuit,
		OnConnect:        func() { h.trayAction(Action{Name: ActionConnect}) },
		OnDisconnect:     func() { h.trayAction(Action{Name: ActionDisconnect}) },
		OnSelectLocation: h.traySelectLocation,
	}, h.log)
	h.trayHook = newTrayStateHook(h.tray)
	h.webview.Dispatch(func() {
		if err := h.tray.Install(); err != nil {
			h.log.logf("tray: install: %v", err)
		}
	})
}

// trayStateWord is the tray tooltip's state word: the exact header wording
// (the inventory's rule), derived by trayWord (state_hooks_windows.go) — the
// single mapping the state hook shares, so the tooltip and the refresh can
// never drift apart.
func (h *host) trayStateWord() string {
	return trayWord(h.ctrl.Snapshot())
}

// trayConnected reports whether the tray's action entry disconnects instead
// of connects: the Dashboard button's rule (the inventory's "same rule as the
// Dashboard button"), read off the very state word the tooltip shows. A
// stale connected status under an unreachable daemon therefore reads
// "Connect", exactly like the header status.
func (h *host) trayConnected() bool {
	return trayDisconnect(trayWord(h.ctrl.Snapshot()))
}

// trayLocations is the menu's location list: the controller's server list
// (the engine sorts it REC first). The tray copies and re-sorts before
// displaying, so this slice is only ever read.
func (h *host) trayLocations() []serverlist.Country {
	return h.ctrl.Snapshot().Locations
}

// showFromTray is the tray's Show Kumiho / left-click behavior: restore and
// focus the window on the pump thread, the same call the bridge's
// window_show action makes. chrome.show() uses SW_RESTORE, so a minimized
// window comes back at its previous size.
func (h *host) showFromTray() {
	h.webview.Dispatch(func() { h.chrome.show() })
}

// showDiagnosticsFromTray is the tray's Diagnostics entry (inventory §Tray):
// restore/focus the window, then send the page's existing hash router to the
// Diagnostics screen. Both steps run in one dispatch on the pump, the shell's
// thread discipline for Eval (the same pattern as pushState), and the hash is
// the router's published contract — no new JS surface. Clicking it while the
// first-run wizard still gates the page lands on the wizard, as every route
// does until onboarding finishes.
func (h *host) showDiagnosticsFromTray() {
	h.webview.Dispatch(func() {
		h.chrome.show()
		h.webview.Eval("location.hash='#/diagnostics'")
	})
}

// trayAction runs one tray menu action: the busy flag disables the entry at
// the next popup and makes a second click a no-op, and the controller call
// itself runs on its own goroutine — connect and disconnect take up to 90 s
// and the pump must stay free (the T2.1 transport contract).
func (h *host) trayAction(act Action) {
	if !h.trayBusy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer h.trayBusy.Store(false)
		if err := h.ctrl.Dispatch(act); err != nil {
			h.logDispatchError(act, err)
		}
	}()
}

// traySelectLocation persists one menu location pick. select_location wants a
// concrete server (the engine validates it), so the country's first
// connectable candidate rides along; a country with no usable server is a
// logged no-op.
func (h *host) traySelectLocation(code string) {
	snap := h.ctrl.Snapshot()
	var server string
	for _, c := range snap.Locations {
		if c.Code != code {
			continue
		}
		if cands := serverlist.Candidates([]serverlist.Country{c}, code, ""); len(cands) > 0 {
			server = cands[0].Address()
		}
		break
	}
	if server == "" {
		h.log.logf("tray: location %q has no connectable server", code)
		return
	}
	raw, err := json.Marshal(api.SelectLocationPayload{CountryCode: code, Server: server})
	if err != nil {
		h.log.logf("tray: location %q: %v", code, err)
		return
	}
	// The busy guard covers the connect/disconnect entry only (the
	// inventory's rule), so a location pick runs through the regular async
	// dispatch instead of trayAction.
	h.dispatchAsync(Action{Name: ActionSelectLocation, Payload: raw})
}

// serviceAction runs one host-side elevated action (T3.3): cleanup,
// service_start or service_stop. The binding's callback thread must never
// block (the UAC consent and the elevated process can take arbitrarily
// long), so the work runs on its own goroutine, and one action at a time is
// enforced — a second click while one is in flight is a logged no-op. The
// screen's buttons disable from State.Service.Busy meanwhile; this guard is
// the authoritative one.
func (h *host) serviceAction(act Action) {
	if !h.serviceBusy.CompareAndSwap(false, true) {
		h.log.logf("action %s ignored: a service action is already in flight", act.Name)
		return
	}
	go func() {
		defer h.serviceBusy.Store(false)
		h.runServiceAction(act)
	}()
}

// runServiceAction dispatches one intercepted action to its elevated
// implementation.
func (h *host) runServiceAction(act Action) {
	switch act.Name {
	case ActionCleanup:
		h.runCleanup()
	case ActionServiceStart:
		h.runServiceStartStop(true)
	case ActionServiceStop:
		h.runServiceStartStop(false)
	}
}

// runServiceStartStop starts or stops the service through one elevated
// sc.exe call (D17) and publishes the outcome: the pending state keeps the
// card honest while the command runs, then the fresh SCM probe lands, then
// the diagnostics notice. The exit-code/notice wording lives in
// serviceOutcomeNotice; both publishes go through the atomic updateService.
func (h *host) runServiceStartStop(start bool) {
	verb := "start"
	if !start {
		verb = "stop"
	}
	h.updateService(func(cur ServiceState) ServiceState {
		cur.State = verb + " pending"
		cur.Busy = true
		return cur
	})

	code, err := serviceCommand(verb)

	// The completion's SCM query runs inside the critical section too, so the
	// snapshot it publishes can never predate a concurrent probe.
	var final ServiceState
	h.updateService(func(ServiceState) ServiceState {
		snap, perr := serviceSnapshot()
		if perr != nil {
			h.log.logf("service: %v", perr)
		}
		snap.Busy = false
		final = snap
		return snap
	})

	msg, tone := serviceOutcomeNotice(verb, code, err)
	h.ctrl.SetDiagnosticsNotice(msg, tone)
	h.log.logf("service %s: code=%d err=%v state=%+v", verb, code, err, final)
}

// runCleanup runs `kumiho cleanup` elevated (D16) and publishes the outcome
// as the diagnostics notice; cleanup changes no service state, so the card
// is only re-probed by the periodic loop.
func (h *host) runCleanup() {
	code, err := cleanupCommand()
	msg, tone := cleanupOutcomeNotice(code, err)
	h.ctrl.SetDiagnosticsNotice(msg, tone)
	h.log.logf("cleanup: code=%d err=%v", code, err)
}

// serviceLoop keeps State.Service fresh (T3.3): one probe at startup, then a
// probe every serviceProbeInterval, so the card reflects a service started
// or stopped outside the GUI (SCM, Task Manager, the installer).
func (h *host) serviceLoop(ctx context.Context) {
	ticker := time.NewTicker(serviceProbeInterval)
	defer ticker.Stop()
	h.probeService()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.probeService()
		}
	}
}

// probeService publishes one SCM probe. The query and the preserve-Busy
// update both run inside the snapshot's critical section (see updateService),
// so a probe can neither overwrite a completion's Busy=false nor briefly
// revert a pending word, whatever the interleave with an action.
func (h *host) probeService() {
	h.updateService(func(cur ServiceState) ServiceState {
		snap, err := serviceSnapshot()
		if err != nil {
			h.log.logf("service: %v", err)
		}
		snap.Busy = cur.Busy
		return snap
	})
}

// updateService is the single writer of the shell's service snapshot (T3.3):
// the read-modify-write AND the controller publish happen in one svcMu
// critical section. That atomicity is the fix for the stuck-Busy interleave
// (T3.3 review): a probe's preserve-Busy either lands before an action
// completion — which then clears Busy — or after it and reads Busy=false; it
// can never publish a preservation of a stale Busy=true after the
// completion. Lock order is svcMu → the controller's own mutex; nothing
// takes them the other way. An update that changes nothing is not
// republished (every assignment is paired with a publish under this lock, so
// an unchanged h.service means the controller already holds the value).
func (h *host) updateService(update func(cur ServiceState) ServiceState) {
	h.svcMu.Lock()
	defer h.svcMu.Unlock()
	next := update(h.service)
	if next == h.service {
		return
	}
	h.service = next
	h.ctrl.SetServiceState(next)
}

// pushLoop is the host→UI half of the bridge: every pushInterval it encodes
// the controller snapshot and evaluates window.kumihoReceive(<json>), but
// only when the envelope changed since the last push (plus once at startup).
// The same tick drives the tray's live state hook (T4.1), which refreshes the
// icon's tooltip when the state word changes. The eval is dispatched onto the
// pump thread, the binding's own reply pattern. It also logs control-pipe
// reachability transitions (the shell's view of IPC connect failures).
func (h *host) pushLoop(ctx context.Context) {
	ticker := time.NewTicker(pushInterval)
	defer ticker.Stop()
	var last []byte
	reachable := true
	first := true
	for {
		snap := h.ctrl.Snapshot()
		h.trayHook.observe(snap)
		raw, err := EncodeState(snap)
		if err != nil {
			h.log.logf("bridge: encode state: %v", err)
		} else if first || !bytes.Equal(raw, last) {
			last = raw
			h.pushState(raw)
		}
		first = false
		if snap.Reachable != reachable {
			reachable = snap.Reachable
			if reachable {
				h.log.logf("ipc: control pipe reachable")
			} else {
				h.log.logf("ipc: control pipe unreachable")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// pushState evaluates one state envelope in the page.
func (h *host) pushState(raw []byte) {
	script := "window.kumihoReceive(" + string(raw) + ")"
	h.webview.Dispatch(func() { h.webview.Eval(script) })
}

// openExternalPayload is the open_external action's payload.
type openExternalPayload struct {
	URL string `json:"url"`
}

// openExternal opens an http/https link in the system browser (D34). Any
// other scheme is refused; the payload is untrusted page input.
func (h *host) openExternal(act Action) error {
	p, err := decodePayload[openExternalPayload](act)
	if err != nil {
		return err
	}
	u, err := url.Parse(p.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("open_external: refusing non-http(s) URL")
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(p.URL)
	if err != nil {
		return err
	}
	if err := windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("open_external: %w", err)
	}
	h.log.logf("open_external: %s", p.URL)
	return nil
}

// prefsSetPayload is the prefs_set action's payload: any subset of the GUI
// preferences the page may change.
type prefsSetPayload struct {
	Theme     string `json:"theme,omitempty"`
	Accent    string `json:"accent,omitempty"`
	Autostart *bool  `json:"autostart,omitempty"`
	Onboarded *bool  `json:"onboarded,omitempty"`
}

// applyPrefs handles prefs_set host-side: persist gui.toml, then apply a
// changed appearance by re-injecting the theme CSS and reloading the view
// (SetThemeCSS + Navigate, the ruled mechanism) and re-syncing the native
// titlebar.
//
// The controller's State carries the theme/accent it was constructed with
// (Options are constructor input; the controller has no appearance setter and
// T1.7 must not change it), so a live change reaches the UI through the
// reloaded page's injected CSS block, not through State.Theme/State.Accent;
// those stay at their startup values until the shell restarts. Autostart
// (T4.2) is persisted, mirrored into the per-user Run key (D11), pushed into
// the controller as the State.Autostart projection, and its outcome is the
// Settings notice: `Start at login saved` (10 s) or `could not change the
// login setting: <err>`. The stored flag survives a Run-key failure — the
// next launch re-syncs from gui.toml (inventory §Tray/app lifecycle) — and
// prefs are written before the registry so a Save failure leaves nothing
// half-applied. Onboarded (T3.4) is persisted and pushed into the controller
// through SetOnboarded, so the wizard's `Open Kumiho` reaches the UI as the
// ratified State.Onboarded projection immediately.
func (h *host) applyPrefs(act Action) error {
	p, err := decodePayload[prefsSetPayload](act)
	if err != nil {
		return err
	}
	current := h.currentPrefs()
	updated := current
	if p.Theme != "" {
		updated.Theme = p.Theme
	}
	if p.Accent != "" {
		updated.Accent = p.Accent
	}
	if p.Autostart != nil {
		updated.Autostart = *p.Autostart
	}
	if p.Onboarded != nil {
		updated.Onboarded = *p.Onboarded
	}
	css, err := theme.CSS(theme.Theme(updated.Theme), updated.Accent)
	if err != nil {
		return fmt.Errorf("prefs_set: %w", err)
	}
	if updated == current {
		return nil
	}
	if err := prefs.Save(updated); err != nil {
		if updated.Autostart != current.Autostart {
			h.ctrl.SetSettingsNotice("could not change the login setting: "+err.Error(), toneBad)
		}
		return fmt.Errorf("prefs_set: %w", err)
	}
	h.mu.Lock()
	h.prefs = updated
	h.mu.Unlock()
	if updated.Onboarded != current.Onboarded {
		h.ctrl.SetOnboarded(updated.Onboarded)
	}
	if updated.Autostart != current.Autostart {
		h.ctrl.SetAutostart(updated.Autostart)
		if err := h.syncAutostartFromPrefs(); err != nil {
			// The stored flag is kept (inventory §Tray/app lifecycle): the
			// row shows the choice and the next launch re-syncs the entry.
			h.log.logf("autostart: %v", err)
			h.ctrl.SetSettingsNotice("could not change the login setting: "+err.Error(), toneBad)
		} else {
			h.ctrl.SetSettingsNotice("Start at login saved", toneGood)
		}
	}
	if updated.Theme != current.Theme || updated.Accent != current.Accent {
		h.server.SetThemeCSS(css)
		th := theme.Theme(updated.Theme)
		h.webview.Dispatch(func() {
			applyTitlebarTheme(h.chrome.hwnd, isDarkTheme(th))
			h.webview.Navigate(h.server.URL())
		})
	}
	h.log.logf("prefs_set: theme=%s accent=%s autostart=%v onboarded=%v",
		updated.Theme, updated.Accent, updated.Autostart, updated.Onboarded)
	return nil
}

// saveGeometry persists the window's restored rectangle and maximized state
// into gui.toml; the shell calls it when the user finishes a move/resize and
// on the close/quit paths. A rectangle and state equal to the saved ones are
// a no-op, so the debounced saves do not rewrite the file. One small atomic
// write on the window thread; the pump stays responsive.
func (h *host) saveGeometry(r rect, maximized bool) {
	w, ht := int(r.right-r.left), int(r.bottom-r.top)
	if w <= 0 || ht <= 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	saved := h.prefs
	if saved.Window.X == int(r.left) && saved.Window.Y == int(r.top) &&
		saved.Window.Width == w && saved.Window.Height == ht &&
		saved.Window.Maximized == maximized {
		return
	}
	saved.Window.X, saved.Window.Y = int(r.left), int(r.top)
	saved.Window.Width, saved.Window.Height = w, ht
	saved.Window.Maximized = maximized
	if err := prefs.Save(saved); err != nil {
		h.log.logf("prefs: save geometry: %v", err)
		return
	}
	h.prefs = saved
}

// syncAutostartFromPrefs is the single writer of the per-user Run entry. It
// takes autostartMu before reading the flag, so a launch-time mirror that was
// delayed past a Settings toggle reads — and writes — the toggle's value,
// never a stale capture; the newest stored flag always wins the last registry
// write.
func (h *host) syncAutostartFromPrefs() error {
	h.autostartMu.Lock()
	defer h.autostartMu.Unlock()
	h.mu.Lock()
	enabled := h.prefs.Autostart
	h.mu.Unlock()
	return syncAutostart(enabled)
}

// currentPrefs returns a consistent copy of the preferences.
func (h *host) currentPrefs() prefs.Prefs {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.prefs
}

// effectiveAppearance resolves the stored theme and accent to a valid pair,
// falling back to light/blue when gui.toml carried unknown values (the prefs
// package already normalizes; this also covers a load failure's zero value).
func effectiveAppearance(p prefs.Prefs) (theme.Theme, string) {
	th := theme.Theme(p.Theme)
	if _, err := theme.CSS(th, p.Accent); err != nil {
		return theme.Light, theme.Accents[0].Name
	}
	return th, p.Accent
}

// isDarkTheme reports whether a theme wants the dark native chrome.
func isDarkTheme(t theme.Theme) bool {
	return t == theme.Dark || t == theme.OLED
}

// logsExportDir returns %LOCALAPPDATA%\kumiho\logs, the per-user log folder
// the Logs screen's export and open-folder actions share (the TUI's export
// folder and the inventory's parity path; the daemon's %ProgramData%\kumiho
// log directory is admin-only by ACL, so a user cannot browse it).
func logsExportDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", errors.New("could not determine the log folder")
	}
	return filepath.Join(dir, "kumiho", "logs"), nil
}

// logsExport implements Options.LogsExport: it writes one log export file
// (kumiho-logs-<YYYYMMDD-HHMMSS>.log, 0600) into the per-user log folder and
// returns its path, so the controller can word the "Exported to <path>"
// notice. The content is the controller's rendered visible entries.
func (h *host) logsExport(content string) (string, error) {
	dir, err := logsExportDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("could not create the log folder: %w", err)
	}
	path := filepath.Join(dir, "kumiho-logs-"+time.Now().Format("20060102-150405")+".log")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("could not write the log export: %w", err)
	}
	h.log.logf("logs_export: %s (%d bytes)", path, len(content))
	return path, nil
}

// openLogsFolder implements Options.OpenLogsFolder: it creates the per-user
// log folder if needed and opens it in File Explorer through ShellExecute,
// returning the folder path for the "Opened <dir>" notice.
func (h *host) openLogsFolder() (string, error) {
	dir, err := logsExportDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("could not create the log folder: %w", err)
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return "", fmt.Errorf("could not open the log folder: %w", err)
	}
	target, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return "", fmt.Errorf("could not open the log folder: %w", err)
	}
	if err := windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		return "", fmt.Errorf("could not open the log folder: %w", err)
	}
	h.log.logf("open_logs_folder: %s", dir)
	return dir, nil
}

// webviewDataPath returns the WebView2 user-data directory,
// %LocalAppData%\kumiho\webview2, creating it if needed; an empty string
// falls back to the binding's default when the directory cannot be made.
func webviewDataPath(log *hostLog) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		log.logf("webview2 datapath: %v", err)
		return ""
	}
	path := filepath.Join(dir, "kumiho", "webview2")
	if err := os.MkdirAll(path, 0o700); err != nil {
		log.logf("webview2 datapath: %v", err)
		return ""
	}
	return path
}

// newSessionToken returns the asset server's per-run URL token: 128 bits of
// crypto/rand, URL-path-safe (the same shape the asset server's nonces use).
// The token gates the ephemeral loopback port for the process's lifetime.
func newSessionToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand cannot realistically fail; a fixed token still keeps
		// the URL path shape the server expects.
		return "kumiho"
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
