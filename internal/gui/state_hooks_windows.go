//go:build windows

package gui

import "strings"

// This file is the tray's live-state hook (T4.1): the shell's 250 ms snapshot
// loop hands every fresh snapshot to trayStateHook.observe, which refreshes
// the icon's tooltip when its state word changes. The menu needs no hook —
// the tray reads its providers at every popup, so labels, the location
// submenu and the busy guard are live by construction.
//
// trayStateWord is the single Go source of the tooltip word (the header's
// mapping, inventory §Shell and §Tray: unreachable first, then the live state
// label, then the first-load failure, then the loading placeholder). The
// Connect/Disconnect rule derives from the same state, so the tooltip and the
// menu action can never disagree; the UI's store.connectionStatus mirrors the
// same order and wording for the header.

// trayStateCode maps a snapshot's status to a daemon state code. New
// snapshots carry State directly; the English label fallback keeps older
// test fixtures and any projection that omits State working.
func trayStateCode(snap State) string {
	if snap.Status == nil {
		return ""
	}
	if code := strings.TrimSpace(snap.Status.State); code != "" {
		return code
	}
	switch snap.Status.Label {
	case "", "Off":
		return "IDLE"
	case "Connected":
		return "CONNECTED"
	case "Proxy only":
		return "PROXY_ONLY"
	case "Connecting":
		return "CONNECTING"
	case "Reconnecting":
		return "RECONNECTING"
	case "Waiting for network":
		return "WAITING_NETWORK"
	case "Not signed in":
		return "UNAUTHENTICATED"
	case "Error":
		return "FATAL"
	}
	return ""
}

// trayStateWord derives the tray's state word from a snapshot: unreachable
// first because every screen depends on reachability, then the localized live
// state, then the first-load failure, then the loading placeholder. The same
// mapping feeds the tooltip and the Connect/Disconnect rule.
func trayStateWord(snap State) string {
	language := trayLanguage(snap.Language)
	switch {
	case !snap.Reachable:
		return trayText(language, "unreachable")
	case snap.Status != nil:
		code := trayStateCode(snap)
		if key := "state." + code; code != "" && trayHas(key) {
			return trayText(language, key)
		}
		if snap.Status.Label != "" {
			return snap.Status.Label
		}
		return trayText(language, "loading")
	case snap.StatusError != "":
		return trayText(language, "unavailable")
	default:
		return trayText(language, "loading")
	}
}

// trayDisconnect reports whether the snapshot is one the action entry
// disconnects from. An unreachable snapshot never is: the tooltip and the
// label both lose the stale connected word when the control pipe goes away.
func trayDisconnect(snap State) bool {
	if !snap.Reachable {
		return false
	}
	switch trayStateCode(snap) {
	case "CONNECTED", "PROXY_ONLY", "CONNECTING", "RECONNECTING", "WAITING_NETWORK":
		return true
	}
	return false
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
	word := trayStateWord(snap)
	if h.seen && word == h.word {
		return
	}
	h.seen, h.word = true, word
	h.refresh()
}
