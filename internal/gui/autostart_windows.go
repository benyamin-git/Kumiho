//go:build windows

package gui

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

// The per-user Windows login registration (D11): a value under HKCU's Run
// key, in the hive of whoever launched the GUI (the installer's entry, T5.2,
// lives in the installing user's hive; this file keeps it truthful for the
// running user). The command carries --tray-start, so a logon launch starts
// hidden in the tray.
const (
	autostartRunKey    = `Software\Microsoft\Windows\CurrentVersion\Run`
	autostartValueName = "Kumiho GUI"
	autostartFlag      = "--tray-start"
)

// syncAutostart mirrors the stored autostart preference into the per-user Run
// key. It is a variable so the host's tests can substitute a recorder instead
// of touching the real HKCU value; production always uses syncAutostartRun.
var syncAutostart = syncAutostartRun

// syncAutostartRun implements the Run key mirror: enabled writes
// `"<exe path>" --tray-start`, disabled removes the value. It is called at
// every launch (off the startup path) and by the Settings toggle; the caller
// turns a failure into the inventory's `could not change the login setting:
// <err>` notice. A missing value on disable is success, and the Run key
// itself is created on demand.
func syncAutostartRun(enabled bool) error {
	if !enabled {
		return removeAutostart()
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate the application: %w", err)
	}
	if exe == "" {
		return errors.New("locate the application: empty path")
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, autostartRunKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open the login key: %w", err)
	}
	defer key.Close()
	if err := key.SetStringValue(autostartValueName, autostartCommand(exe)); err != nil {
		return fmt.Errorf("write the login entry: %w", err)
	}
	return nil
}

// removeAutostart deletes the Run value; a missing key or value is already
// the desired state.
func removeAutostart() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, autostartRunKey, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open the login key: %w", err)
	}
	defer key.Close()
	if err := key.DeleteValue(autostartValueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("remove the login entry: %w", err)
	}
	return nil
}

// autostartCommand renders the Run entry's command line: the quoted
// executable path plus the logon flag. Run entries are raw command lines, so
// the quotes are what keep a path with spaces intact.
func autostartCommand(exe string) string {
	return `"` + exe + `" ` + autostartFlag
}
