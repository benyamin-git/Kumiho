//go:build windows

package gui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/benyamin-git/kumiho/internal/gui/prefs"
)

// useTempPrefs points prefs.Save/Load at a scratch gui.toml.
func useTempPrefs(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gui.toml")
	t.Setenv("KUMIHO_GUI_PREFS", path)
	return path
}

// stubAutostart replaces the Run key writer for one test and records its
// calls.
func stubAutostart(t *testing.T, err error) *[]bool {
	t.Helper()
	calls := []bool{}
	prev := syncAutostart
	syncAutostart = func(enabled bool) error {
		calls = append(calls, enabled)
		return err
	}
	t.Cleanup(func() { syncAutostart = prev })
	return &calls
}

// TestSaveGeometryPersistsMaximized pins the T4.2 geometry write: the
// restored rectangle and the maximized flag are persisted together and an
// un-maximize clears the flag.
func TestSaveGeometryPersistsMaximized(t *testing.T) {
	useTempPrefs(t)
	h := &host{prefs: prefs.Prefs{Theme: "dark", Accent: "blue"}}

	h.saveGeometry(rect{left: -100, top: 20, right: 1180, bottom: 740}, true)
	saved, err := prefs.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if saved.Window.X != -100 || saved.Window.Y != 20 ||
		saved.Window.Width != 1280 || saved.Window.Height != 720 || !saved.Window.Maximized {
		t.Fatalf("saved window = %+v, want the restored rect and maximized", saved.Window)
	}

	h.saveGeometry(rect{left: -100, top: 20, right: 1180, bottom: 740}, false)
	saved, err = prefs.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if saved.Window.Maximized {
		t.Fatalf("saved window = %+v, want maximized cleared", saved.Window)
	}
}

// newAutostartHost builds the shell fragment applyPrefs needs for an
// autostart-only change: the in-memory prefs and a real controller over the
// fake daemon.
func newAutostartHost(t *testing.T, startOn bool) (*host, *Controller) {
	t.Helper()
	useTempPrefs(t)
	h := &host{prefs: prefs.Prefs{Theme: "dark", Accent: "blue", Autostart: startOn}}
	h.ctrl = NewController(newFakeDaemon(), Options{
		Theme:     "dark",
		Accent:    "blue",
		Autostart: &startOn,
	})
	return h, h.ctrl
}

// TestApplyPrefsAutostart pins the T4.2 prefs_set wiring: the flag is saved
// first, mirrored into the Run key, projected into State.Autostart, and the
// outcome is the inventory's Settings notice. A Run key failure keeps the
// stored flag (the next launch re-syncs) and still reports the failure; a
// gui.toml write failure rejects the action and leaves the old projection.
func TestApplyPrefsAutostart(t *testing.T) {
	action := Action{Name: ActionPrefsSet, Payload: []byte(`{"autostart":false}`)}

	t.Run("success", func(t *testing.T) {
		h, ctrl := newAutostartHost(t, true)
		calls := stubAutostart(t, nil)

		if err := h.applyPrefs(action); err != nil {
			t.Fatalf("applyPrefs: %v", err)
		}
		if len(*calls) != 1 || (*calls)[0] {
			t.Fatalf("syncAutostart calls = %v, want [false]", *calls)
		}
		if h.currentPrefs().Autostart {
			t.Fatal("in-memory autostart = true, want false")
		}
		saved, err := prefs.Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if saved.Autostart {
			t.Fatal("saved autostart = true, want false")
		}
		snap := ctrl.Snapshot()
		if snap.Autostart == nil || *snap.Autostart {
			t.Fatalf("state autostart = %v, want the projected false", snap.Autostart)
		}
		n := snap.SettingsNotice
		if n == nil || n.Message != "Start at login saved" || n.Tone != toneGood || n.Expires == nil {
			t.Fatalf("settings notice = %+v, want the 10 s success", n)
		}
	})

	t.Run("run key failure keeps the stored flag", func(t *testing.T) {
		h, ctrl := newAutostartHost(t, true)
		stubAutostart(t, errors.New("access denied"))

		if err := h.applyPrefs(action); err != nil {
			t.Fatalf("applyPrefs = %v, want nil (the flag is saved)", err)
		}
		saved, err := prefs.Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if saved.Autostart {
			t.Fatal("saved autostart = true, want the chosen false")
		}
		snap := ctrl.Snapshot()
		if snap.Autostart == nil || *snap.Autostart {
			t.Fatalf("state autostart = %v, want the persisted false", snap.Autostart)
		}
		n := snap.SettingsNotice
		if n == nil || n.Tone != toneBad || n.Expires != nil ||
			!strings.HasPrefix(n.Message, "could not change the login setting: ") ||
			!strings.Contains(n.Message, "access denied") {
			t.Fatalf("settings notice = %+v, want the persistent login failure", n)
		}
	})

	t.Run("gui.toml failure rejects the change", func(t *testing.T) {
		h, ctrl := newAutostartHost(t, true)
		calls := stubAutostart(t, nil)
		blocker := filepath.Join(t.TempDir(), "blocker")
		if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("KUMIHO_GUI_PREFS", filepath.Join(blocker, "gui.toml"))

		if err := h.applyPrefs(action); err == nil {
			t.Fatal("applyPrefs succeeded with an unwritable gui.toml")
		}
		if len(*calls) != 0 {
			t.Fatalf("syncAutostart calls = %v, want none", *calls)
		}
		if !h.currentPrefs().Autostart {
			t.Fatal("in-memory autostart changed despite the failed save")
		}
		snap := ctrl.Snapshot()
		if snap.Autostart == nil || !*snap.Autostart {
			t.Fatalf("state autostart = %v, want the old true", snap.Autostart)
		}
		n := snap.SettingsNotice
		if n == nil || n.Tone != toneBad ||
			!strings.HasPrefix(n.Message, "could not change the login setting: ") {
			t.Fatalf("settings notice = %+v, want the persistent login failure", n)
		}
	})
}
