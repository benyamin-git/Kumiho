//go:build windows

package gui

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/benyamin-git/kumiho/internal/gui/prefs"
)

// The Win32 surface of the shell. Every DLL is loaded lazily, like the rest
// of the repo: package initialization touches no Windows API. The handles are
// declared once here for every file of the package.
var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")

	procGetWindowLongPtrW   = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW   = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW     = user32.NewProc("CallWindowProcW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procGetWindowRect       = user32.NewProc("GetWindowRect")
	procGetWindowPlacement  = user32.NewProc("GetWindowPlacement")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procFindWindowW         = user32.NewProc("FindWindowW")
	procIsIconic            = user32.NewProc("IsIconic")
	procIsZoomed            = user32.NewProc("IsZoomed")
	procLoadImageW          = user32.NewProc("LoadImageW")
	procSendMessageW        = user32.NewProc("SendMessageW")

	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
)

// Win32 constants the chrome needs.
const (
	wmClose        = 0x0010
	wmSetIcon      = 0x0080
	wmExitSizeMove = 0x0232

	// GWLP_WNDPROC, the window-procedure slot, as its LONG_PTR value (-4).
	gwlpWndProc = ^uintptr(3)

	iconSmall = 0 // ICON_SMALL
	iconBig   = 1 // ICON_BIG
	imageIcon = 1 // IMAGE_ICON

	// SetWindowPos flags.
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010
	swpNoMove     = 0x0002

	// GetSystemMetrics indices.
	smCXIcon          = 11
	smCYIcon          = 12
	smCXSmIcon        = 49
	smCYSmIcon        = 50
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79

	// DWMWA_USE_IMMERSIVE_DARK_MODE and its pre-20H1 attribute number; the
	// shell tries the current one first and falls back for older builds.
	dwmwaUseImmersiveDarkMode       = 20
	dwmwaUseImmersiveDarkModeLegacy = 19

	// resourceIDIcon is the icon group's (RT_GROUP_ICON) resource ID in
	// rsrc_windows.syso: the recovered asset carries the group under ID 2
	// (its RT_ICON images 3-6 are the 16/32/48/256 entries; ID 1 is
	// RT_VERSION). Both the window chrome and the tray load it.
	resourceIDIcon = 2
)

// rect mirrors RECT (windef.h).
type rect struct{ left, top, right, bottom int32 }

// nativePlacement mirrors WINDOWPLACEMENT (winuser.h): the show state and the
// normal (restored) rectangle GetWindowPlacement reports, even while the
// window is maximized. The field order and sizes match the Win32 struct, whose
// length field must be initialized to its size before the call.
type nativePlacement struct {
	length      uint32
	flags       uint32
	showCmd     uint32
	minPosition point
	maxPosition point
	normal      rect
}

// windowChrome owns the shell window's native behavior: a WNDPROC subclass
// that turns WM_CLOSE into close-to-tray (D10) and persists geometry after a
// user move/resize, plus the icon, native titlebar theme and initial
// placement helpers.
type windowChrome struct {
	hwnd         uintptr
	origProc     uintptr
	saveGeometry func(rect, bool)
	onClose      func()
}

// activeChrome is the process's single subclassed window; the Win32 window
// procedure carries no Go context, and the shell owns exactly one window, so
// a package variable is the honest wiring (the probe's tray uses the same
// pattern).
var activeChrome *windowChrome

// newWindowChrome returns the chrome for hwnd; saveGeometry is called on the
// window thread after every finished user move/resize and on close (with the
// window's restored rectangle and maximized state), and onClose is the
// WM_CLOSE handler (the shell's first-close hint plus hide-to-tray). A nil
// onClose falls back to a plain save-and-hide.
func newWindowChrome(hwnd uintptr, saveGeometry func(rect, bool), onClose func()) *windowChrome {
	return &windowChrome{hwnd: hwnd, saveGeometry: saveGeometry, onClose: onClose}
}

// install subclasses the window, replacing the vendored binding's window
// procedure (which would destroy the window on WM_CLOSE and ignore
// WM_EXITSIZEMOVE) with the shell's. It runs on the window thread, after the
// binding created the window.
func (c *windowChrome) install() error {
	prev, _, callErr := procGetWindowLongPtrW.Call(c.hwnd, gwlpWndProc)
	if prev == 0 {
		return fmt.Errorf("GetWindowLongPtrW(GWLP_WNDPROC): %w", callErr)
	}
	c.origProc = prev
	// Register the routing before swapping the procedure in: the
	// SetWindowLongPtrW call itself may dispatch messages.
	activeChrome = c
	proc := windows.NewCallback(chromeWndProc)
	if r, _, callErr := procSetWindowLongPtrW.Call(c.hwnd, gwlpWndProc, proc); r == 0 {
		activeChrome = nil
		return fmt.Errorf("SetWindowLongPtrW(GWLP_WNDPROC): %w", callErr)
	}
	return nil
}

// chromeWndProc is the shell's window procedure. Everything it does not
// handle is forwarded to the binding's original procedure (which resizes the
// WebView2 control, forwards menu shortcuts and paints the frame).
func chromeWndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	c := activeChrome
	if c == nil || hwnd != c.hwnd {
		return defWindowProc(hwnd, msg, wparam, lparam)
	}
	switch msg {
	case wmClose:
		// Close hides to tray (D10): the shell keeps running, the tray
		// (T1.8) and the UI's quit action are the exit paths. The shell's
		// onClose owns the one-time hint and the flag save (T4.2).
		if c.onClose != nil {
			c.onClose()
		} else {
			c.hide()
		}
		return 0
	case wmExitSizeMove:
		// The end of a user move/resize is the debounce point (D24).
		c.save()
	}
	return callWindowProc(c.origProc, hwnd, msg, wparam, lparam)
}

// hide hides the window, persisting the current geometry first.
func (c *windowChrome) hide() {
	c.save()
	showWindow(c.hwnd, windows.SW_HIDE)
}

// show restores, shows and focuses the window; SW_RESTORE also un-minimizes.
func (c *windowChrome) show() {
	showWindow(c.hwnd, windows.SW_RESTORE)
	setForegroundWindow(c.hwnd)
}

// save captures the window's restored (normal) rectangle and maximized state
// into the shell preferences, as long as the window is not minimized
// (minimized positions are ignored, per the inventory). GetWindowPlacement
// supplies the normal rectangle even while the window is maximized, so a
// maximized window can never persist its full-monitor rectangle (the
// password to the T1.7 review's geometry bug); GetWindowRect is the fallback
// when the placement query fails.
func (c *windowChrome) save() {
	if iconic(c.hwnd) {
		return
	}
	r, maximized, ok := placementOf(c.hwnd)
	if !ok {
		r, ok = windowRectOf(c.hwnd)
		maximized = false
	}
	if ok && c.saveGeometry != nil {
		c.saveGeometry(r, maximized)
	}
}

// placementOf returns the window's normal rectangle and maximized state
// through GetWindowPlacement; IsZoomed is the belt-and-braces guard for the
// show command.
func placementOf(hwnd uintptr) (r rect, maximized, ok bool) {
	p := nativePlacement{length: uint32(unsafe.Sizeof(nativePlacement{}))}
	if res, _, _ := procGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&p))); res == 0 {
		return rect{}, false, false
	}
	return p.normal, p.showCmd == windows.SW_SHOWMAXIMIZED || zoomed(hwnd), true
}

func defWindowProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func callWindowProc(proc, hwnd, msg, wparam, lparam uintptr) uintptr {
	r, _, _ := procCallWindowProcW.Call(proc, hwnd, msg, wparam, lparam)
	return r
}

func showWindow(hwnd uintptr, cmd uint32) {
	procShowWindow.Call(hwnd, uintptr(cmd))
}

func setForegroundWindow(hwnd uintptr) {
	procSetForegroundWindow.Call(hwnd)
}

func iconic(hwnd uintptr) bool {
	r, _, _ := procIsIconic.Call(hwnd)
	return r != 0
}

func zoomed(hwnd uintptr) bool {
	r, _, _ := procIsZoomed.Call(hwnd)
	return r != 0
}

func windowRectOf(hwnd uintptr) (rect, bool) {
	var r rect
	ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r, ok != 0
}

func systemMetric(index int32) int {
	r, _, _ := procGetSystemMetrics.Call(uintptr(index))
	return int(r)
}

// applyWindowIcon assigns the shell's embedded fox icon (icon group 1 of
// cmd/kumiho-gui/rsrc_windows.syso) to the window's big and small icon slots,
// so the taskbar and titlebar carry the brand mark. A missing resource is
// logged and left to the exe's own icon.
func applyWindowIcon(hwnd uintptr, log *hostLog) {
	var hinstance windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &hinstance); err != nil {
		log.logf("window icon: module handle: %v", err)
		return
	}
	big, _, _ := procLoadImageW.Call(uintptr(hinstance), resourceIDIcon, imageIcon,
		uintptr(systemMetric(smCXIcon)), uintptr(systemMetric(smCYIcon)), 0)
	small, _, _ := procLoadImageW.Call(uintptr(hinstance), resourceIDIcon, imageIcon,
		uintptr(systemMetric(smCXSmIcon)), uintptr(systemMetric(smCYSmIcon)), 0)
	if big != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconBig, big)
	}
	if small != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, small)
	}
	if big == 0 && small == 0 {
		log.logf("window icon: LoadImageW found no icon resource")
	}
}

// applyTitlebarTheme syncs the native title bar and frame with the house
// theme (Dark/OLED get the dark chrome on supporting Windows builds). The
// call is best-effort: an unsupported attribute is ignored.
func applyTitlebarTheme(hwnd uintptr, dark bool) {
	v := int32(0)
	if dark {
		v = 1
	}
	for _, attr := range []uintptr{dwmwaUseImmersiveDarkMode, dwmwaUseImmersiveDarkModeLegacy} {
		if r, _, _ := procDwmSetWindowAttribute.Call(hwnd, attr, uintptr(unsafe.Pointer(&v)), unsafe.Sizeof(v)); r == 0 {
			return
		}
	}
}

// windowPlacement is the initial window geometry: x/y are meaningful only
// when place is true (a saved rectangle was restored), and maximized asks the
// caller to re-enter the maximized state after applying the rectangle.
type windowPlacement struct {
	x, y, w, h int
	place      bool
	maximized  bool
}

// placementFor computes the initial window geometry from the saved
// preferences (D24): the saved rectangle is restored and clamped into the
// virtual screen; a rectangle that intersects no monitor falls back to the
// default size and position. The minimum size is always enforced. The saved
// maximized state carries through either way, so a window closed maximized
// reopens maximized even after its normal rectangle left the monitor set.
func placementFor(p prefs.Prefs) windowPlacement {
	vx, vy, vw, vh := virtualScreen()
	return placementIn(p, vx, vy, vw, vh)
}

// placementIn is placementFor's pure core: the restore/clamp rules against an
// explicit monitor-set rectangle (virtualScreen in production).
func placementIn(p prefs.Prefs, vx, vy, vw, vh int) windowPlacement {
	maximized := p.Window.Maximized
	w, h := p.Window.Width, p.Window.Height
	if w <= 0 || h <= 0 {
		return windowPlacement{w: prefs.DefaultWidth, h: prefs.DefaultHeight, maximized: maximized}
	}
	if w < prefs.MinWidth {
		w = prefs.MinWidth
	}
	if h < prefs.MinHeight {
		h = prefs.MinHeight
	}
	x, y := p.Window.X, p.Window.Y
	if !rectsIntersect(x, y, w, h, vx, vy, vw, vh) {
		return windowPlacement{w: prefs.DefaultWidth, h: prefs.DefaultHeight, maximized: maximized}
	}
	return windowPlacement{
		x:         clampContained(x, vx, vx+vw-w),
		y:         clampContained(y, vy, vy+vh-h),
		w:         w,
		h:         h,
		place:     true,
		maximized: maximized,
	}
}

// virtualScreen returns the bounding box of every attached monitor, the
// monitor set expressed through GetSystemMetrics (x/sys does not wrap
// EnumDisplayMonitors). It is the clamp/collision region for saved geometry.
func virtualScreen() (x, y, w, h int) {
	return systemMetric(smXVirtualScreen), systemMetric(smYVirtualScreen),
		systemMetric(smCXVirtualScreen), systemMetric(smCYVirtualScreen)
}

func rectsIntersect(x1, y1, w1, h1, x2, y2, w2, h2 int) bool {
	return x1 < x2+w2 && x2 < x1+w1 && y1 < y2+h2 && y2 < y1+h1
}

// clampContained clamps v into [lo, hi]; a hi below lo (the window is larger
// than the screen) pins v at lo.
func clampContained(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// applyWindowGeometry moves and sizes the window to the placement; without a
// saved position it only sizes the window and keeps the system's default
// placement. A saved maximized state is re-entered last, after the restored
// rectangle is in place, so un-maximizing lands on the right normal size.
func applyWindowGeometry(hwnd uintptr, p windowPlacement) {
	flags := uintptr(swpNoZOrder | swpNoActivate)
	if p.place {
		procSetWindowPos.Call(hwnd, 0,
			uintptr(int64(p.x)), uintptr(int64(p.y)),
			uintptr(p.w), uintptr(p.h), flags)
	} else {
		procSetWindowPos.Call(hwnd, 0, 0, 0,
			uintptr(p.w), uintptr(p.h), flags|swpNoMove)
	}
	if p.maximized {
		showWindow(hwnd, windows.SW_SHOWMAXIMIZED)
	}
}
