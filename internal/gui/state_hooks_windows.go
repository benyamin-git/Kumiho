//go:build windows

package gui

// This file is the tray's live-state hook (T4.1): the shell's 250 ms snapshot
// loop hands every fresh snapshot to trayStateHook.observe, which refreshes
// the icon's tooltip when its state word changes. The menu needs no hook —
// the tray reads its providers at every popup, so labels, the location
// submenu and the busy guard are live by construction.
//
// trayWord is the single Go source of the tooltip word (the header's mapping,
// inventory §Shell and §Tray: unreachable first, then the live state label,
// then the first-load failure, then the loading placeholder). The
// Connect/Disconnect rule derives from the same word, so the tooltip and the
// menu action can never disagree; the UI's store.connectionStatus mirrors the
// same order and wording for the header.

// trayDisconnectWords are the state words whose menu action is `Disconnect`
// (inventory §Tray: the Dashboard button's rule): Connected, Proxy only,
// Connecting, Reconnecting and Waiting for network.
var trayDisconnectWords = map[string]struct{}{
	"Connected":           {},
	"Proxy only":          {},
	"Connecting":          {},
	"Reconnecting":        {},
	"Waiting for network": {},
}

// trayWord derives the tray's state word from a snapshot, exactly the header
// model the shell has always pushed: unreachable first because every screen
// depends on reachability, then the live state label, then the first-load
// failure, then the loading placeholder. An empty label cannot occur in
// practice (stateLabel maps every daemon state, the zero one to "Off"); the
// tray's tipText still defaults an empty provider word to "Loading...".
func trayWord(snap State) string {
	switch {
	case !snap.Reachable:
		return "Daemon unreachable"
	case snap.Status != nil:
		return snap.Status.Label
	case snap.StatusError != "":
		return "Status unavailable"
	default:
		return "Loading..."
	}
}

// trayDisconnect reports whether a state word is one the action entry
// disconnects from. An unreachable word never is: the tooltip and the label
// both lose the stale connected word when the control pipe goes away, the
// same way the Dashboard chip does.
func trayDisconnect(word string) bool {
	_, ok := trayDisconnectWords[word]
	return ok
}

// trayStateHook watches the tooltip's state word: observe compares each
// snapshot's word with the last one pushed and refreshes only on a change, so
// the 250 ms loop does not repost an identical tooltip. refresh posts the
// update onto the shell's thread (Tray.RefreshTooltip); it is injected so the
// change-only behavior is testable without a Win32 tray.
type trayStateHook struct {
	refresh func()
	seen    bool
	word    string
}

// newTrayStateHook returns the hook for t. The shell constructs it before
// starting the push loop, and the loop calls observe on its own goroutine
// only, so the fields need no lock.
func newTrayStateHook(t *Tray) *trayStateHook {
	return &trayStateHook{refresh: t.RefreshTooltip}
}

// observe applies one snapshot to the tooltip. The first snapshot always
// posts — the icon may have been installed from an earlier provider read —
// and every later one only when the word actually changed, which is what
// keeps the tooltip and the header in lockstep without a per-tick NIM_MODIFY.
func (h *trayStateHook) observe(snap State) {
	word := trayWord(snap)
	if h.seen && word == h.word {
		return
	}
	h.seen, h.word = true, word
	h.refresh()
}
