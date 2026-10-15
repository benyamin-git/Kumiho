//go:build windows

// Command kumiho-gui-probe is the GUI rewrite's probe (design decision A2):
// a permanent support diagnostic that proves a bare Win32 host can render
// WebView2 content, complete a JS↔Go echo round trip, host a tray icon with
// a Quit menu on the webview's own message pump, and reach the daemon's
// control pipe. It is cross-compiled from Linux (make build-probe) and run
// by hand on the Windows VM; M1 of the GUI rewrite is gated on it passing.
package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"time"
	"unsafe"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/platform/auto"
	"github.com/benyamin-git/kumiho/internal/version"
	"github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

const (
	probeTitle   = "Kumiho GUI probe"
	probeWidth   = 720
	probeHeight  = 520
	helloTimeout = 5 * time.Second

	// downloadURL is Microsoft's stable evergreen link for the WebView2
	// Runtime bootstrapper (D15: no bundle ships one, point at this).
	downloadURL = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"
)

// Win32 constants the probe needs; x/sys only wraps MessageBox and
// ShellExecute of the calls below, the rest go through lazy procedures.
const (
	idiApplication = 32512 // IDI_APPLICATION
	idYes          = 6

	wmNull      = 0x0000
	wmDestroy   = 0x0002
	wmLButtonUp = 0x0202
	wmRButtonUp = 0x0205
	// trayCallbackMessage is the shell's notification message. It must not be
	// WM_APP itself: go-webview2 uses exactly WM_APP for its dispatch queue.
	trayCallbackMessage = 0x8000 + 1

	nimAdd    = 0x00000000
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	mfString       = 0x00000000
	tpmRightButton = 0x0002
	tpmNonotify    = 0x0080
	tpmReturnCmd   = 0x0100

	trayIconID   = 1
	trayMenuQuit = 1
	trayTip      = probeTitle
)

// The two DLLs are loaded lazily, like every other Win32 surface in the
// repo: package initialization touches no Windows API.
var (
	user32  = windows.NewLazySystemDLL("user32.dll")
	shell32 = windows.NewLazySystemDLL("shell32.dll")

	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procLoadIconW           = user32.NewProc("LoadIconW")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32.NewProc("AppendMenuW")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")

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
// the call valid on every shell version the probe can meet.
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

func main() {
	// The webview window, the tray window, and the message pump are
	// thread-affine. go-webview2's edge package locks the thread during
	// package init; the probe locks it again so main stands on its own.
	runtime.LockOSThread()

	fmt.Printf("kumiho-gui-probe %s\n", version.String())
	hello()

	installed, err := webviewloader.GetInstalledVersion()
	if err != nil || installed == "" {
		if err != nil {
			fmt.Fprintf(os.Stderr, "webview2 runtime: detection failed: %v\n", err)
		} else {
			fmt.Fprintln(os.Stderr, "webview2 runtime: not installed")
		}
		runtimeMissingDialog()
		os.Exit(1)
	}
	fmt.Printf("webview2 runtime: %s\n", installed)

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  probeTitle,
			Width:  probeWidth,
			Height: probeHeight,
			Center: true,
		},
	})
	if w == nil {
		fmt.Fprintln(os.Stderr, "webview2: environment creation failed")
		runtimeMissingDialog()
		os.Exit(1)
	}
	probeWebView = w

	if err := w.Bind("hostVersion", hostVersion); err != nil {
		fmt.Fprintf(os.Stderr, "webview2: bind hostVersion: %v\n", err)
		os.Exit(1)
	}
	w.SetHtml(probePage)

	// The tray window is created from a callback the webview's own message
	// loop runs (Dispatch), and its clicks are dispatched by that same loop,
	// so the icon exercises the exact pump the GUI shell will use.
	tray := &trayIcon{}
	w.Dispatch(func() {
		if err := tray.install(); err != nil {
			fmt.Fprintf(os.Stderr, "tray: setup failed: %v\n", err)
		}
	})
	w.Run()

	tray.remove()
	w.Destroy()
}

// hello performs the control-protocol handshake and prints its result. Dial
// and call share one bounded context: the probe must never hang, and it must
// keep going (window and tray still prove themselves) when no daemon runs.
func hello() {
	endpoint := auto.Current().ControlEndpoint()
	ctx, cancel := context.WithTimeout(context.Background(), helloTimeout)
	defer cancel()

	client, err := ipc.DefaultTransport().Dial(ctx, endpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hello failed: daemon unreachable at %s: %v\n", endpoint, err)
		return
	}
	defer client.Close()

	env, err := client.Call(ctx, ipc.TypeHello, api.HelloPayload{ClientVersion: version.Version})
	if err != nil {
		fmt.Fprintf(os.Stderr, "hello failed: %v\n", err)
		return
	}
	var res api.HelloResult
	if err := env.DecodePayload(&res); err != nil {
		fmt.Fprintf(os.Stderr, "hello failed: %v\n", err)
		return
	}
	fmt.Printf("hello kumiho %s protocol %d %s/%s\n", res.Version, res.Protocol, res.OS, res.Arch)
}

// runtimeMissingDialog is the D15 fallback: a native dialog that names the
// missing Microsoft Edge WebView2 Runtime and offers Microsoft's download
// page in the default browser. The probe then exits cleanly.
func runtimeMissingDialog() {
	const body = "Microsoft Edge WebView2 Runtime is not installed.\r\n\r\n" +
		"Kumiho's GUI renders with it, and this probe cannot continue " +
		"without it.\r\n\r\nOpen the Microsoft WebView2 Runtime download " +
		"page in your browser?"
	title, _ := windows.UTF16PtrFromString(probeTitle)
	text, _ := windows.UTF16PtrFromString(body)
	r, err := windows.MessageBox(0, text, title,
		windows.MB_YESNO|windows.MB_ICONWARNING|windows.MB_SETFOREGROUND)
	if err != nil {
		fmt.Fprintf(os.Stderr, "webview2: fallback dialog failed: %v\n", err)
		return
	}
	if r != idYes {
		return
	}
	verb, _ := windows.UTF16PtrFromString("open")
	url, _ := windows.UTF16PtrFromString(downloadURL)
	if err := windows.ShellExecute(0, verb, url, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		fmt.Fprintf(os.Stderr, "webview2: open %s: %v\n", downloadURL, err)
	}
}

// edgeVersion matches the Edg/<version> token in the WebView2 user agent —
// the runtime version the frontend itself can honestly observe.
var edgeVersion = regexp.MustCompile(`Edg/([0-9]+(?:\.[0-9]+)+)`)

// hostVersion is the Go half of the JS→Go echo. The page sends its own
// navigator.userAgent and renders the reply, so the "WebView2 OK" verdict is
// only ever produced by this round trip, never by the static page.
func hostVersion(userAgent string) (string, error) {
	m := edgeVersion.FindStringSubmatch(userAgent)
	if m == nil {
		return "", fmt.Errorf("user agent carries no Edg/<version> token")
	}
	return "WebView2 OK " + m[1], nil
}

// probePage is deliberately bare (no repo chrome): it waits for the echo and
// only then renders the verdict.
const probePage = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>Kumiho GUI probe</title>
<style>
  body { font-family: "Segoe UI", system-ui, sans-serif; margin: 2.5rem; color: #1f2328; }
  h1 { font-size: 1.05rem; font-weight: 600; margin: 0 0 1rem; }
  #echo { font-size: 1.4rem; font-weight: 600; color: #6e7781; margin: 0 0 1.25rem; }
  #echo.ok { color: #1a7f37; }
  #echo.fail { color: #cf222e; }
  p { font-size: .85rem; color: #57606a; margin: 0; }
  code { font-family: Consolas, monospace; background: #f6f8fa; padding: .1rem .3rem; border-radius: 4px; }
</style>
</head>
<body>
<h1>Kumiho GUI probe</h1>
<p id="echo">waiting for the JS &rarr; Go echo&hellip;</p>
<p>The verdict above is parsed from <code>navigator.userAgent</code> by the Go
host and returned through the bound <code>hostVersion</code> call.</p>
<script>
  const echo = document.getElementById('echo');
  hostVersion(navigator.userAgent).then(
    function (result) {
      echo.textContent = result;
      echo.className = 'ok';
    },
    function (err) {
      echo.textContent = 'WebView2 FAILED: ' + err;
      echo.className = 'fail';
    });
</script>
</body>
</html>
`

// probeWebView is how the tray window procedure reaches the window a Quit
// terminates. A Win32 wndproc carries no Go context, and the probe is a
// single-window process, so a package variable is the honest wiring.
var probeWebView webview2.WebView

// trayIcon owns the notify-icon window: a hidden top-level window whose
// procedure handles the shell's callback message and whose menu quits the
// probe. It is created on the GUI thread inside the webview's own message
// pump (see main) and removed after Run returns.
type trayIcon struct {
	hwnd uintptr
}

func (t *trayIcon) install() error {
	var hinstance windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &hinstance); err != nil {
		return fmt.Errorf("module handle: %w", err)
	}
	className, err := windows.UTF16PtrFromString(probeTitle + " tray")
	if err != nil {
		return err
	}

	wc := wndClassExW{
		lpfnWndProc:   windows.NewCallback(trayWndProc),
		hInstance:     hinstance,
		lpszClassName: className,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	if r, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassExW: %w", callErr)
	}

	hwnd, _, callErr := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(className)), 0, 0,
		0, 0, 0, 0,
		0, 0, uintptr(hinstance), 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW: %w", callErr)
	}

	// IDI_APPLICATION is the system application icon; brand assets arrive
	// with the GUI shell (T1.7).
	icon, _, callErr := procLoadIconW.Call(0, idiApplication)
	if icon == 0 {
		procDestroyWindow.Call(hwnd)
		return fmt.Errorf("LoadIconW(IDI_APPLICATION): %w", callErr)
	}

	nid := notifyIconDataW{
		hWnd:             windows.HWND(hwnd),
		uID:              trayIconID,
		uFlags:           nifMessage | nifIcon | nifTip,
		uCallbackMessage: trayCallbackMessage,
		hIcon:            windows.Handle(icon),
	}
	tip, err := windows.UTF16FromString(trayTip)
	if err != nil {
		procDestroyWindow.Call(hwnd)
		return err
	}
	copy(nid.szTip[:], tip)
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	if r, _, callErr := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); r == 0 {
		procDestroyWindow.Call(hwnd)
		return fmt.Errorf("Shell_NotifyIconW(NIM_ADD): %w", callErr)
	}
	t.hwnd = hwnd
	return nil
}

func (t *trayIcon) remove() {
	if t.hwnd == 0 {
		return
	}
	nid := notifyIconDataW{
		hWnd: windows.HWND(t.hwnd),
		uID:  trayIconID,
	}
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	procDestroyWindow.Call(t.hwnd)
	t.hwnd = 0
}

// trayWndProc handles the shell's callback on the window thread; every other
// message falls through to the default procedure.
func trayWndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case trayCallbackMessage:
		if lparam == wmLButtonUp || lparam == wmRButtonUp {
			showTrayMenu(hwnd)
		}
		return 0
	case wmDestroy:
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func showTrayMenu(hwnd uintptr) {
	menu, _, callErr := procCreatePopupMenu.Call()
	if menu == 0 {
		fmt.Fprintf(os.Stderr, "tray: CreatePopupMenu: %v\n", callErr)
		return
	}
	defer procDestroyMenu.Call(menu)

	quit, _ := windows.UTF16PtrFromString("Quit")
	if r, _, callErr := procAppendMenuW.Call(menu, mfString, trayMenuQuit, uintptr(unsafe.Pointer(quit))); r == 0 {
		fmt.Fprintf(os.Stderr, "tray: AppendMenuW: %v\n", callErr)
		return
	}

	var pt point
	if r, _, callErr := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); r == 0 {
		fmt.Fprintf(os.Stderr, "tray: GetCursorPos: %v\n", callErr)
		return
	}

	// The classic foreground dance: the popup only dismisses when its owner
	// is foreground; the WM_NULL afterwards is its documented chaser.
	procSetForegroundWindow.Call(hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(
		menu, tpmRightButton|tpmReturnCmd|tpmNonotify,
		uintptr(int64(pt.x)), uintptr(int64(pt.y)), 0, hwnd, 0)
	procPostMessageW.Call(hwnd, wmNull, 0, 0)
	if cmd == trayMenuQuit && probeWebView != nil {
		probeWebView.Terminate()
	}
}
