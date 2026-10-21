package mobile

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/benyamin-git/kumiho/internal/gui/theme"
	"github.com/benyamin-git/kumiho/internal/version"
)

// errNotStarted is returned by Command before Start and after Close.
var errNotStarted = errors.New("kumiho mobile: the engine is not started")

// There is exactly one facade per process. Command holds the read lock for the
// whole dispatch, so a command and Close never overlap; Close takes the write
// lock, cancels the root context and shuts the engine down. Start after Close
// builds a fresh facade.
var (
	mu      sync.RWMutex
	current *facade
)

// Start opens the engine for the app-private paths JSON, wires the Kotlin
// DeviceBridge and Listener, and pushes one initial status. A second Start
// returns an "already started" error and leaves the live session alone; call
// Close first to replace it. Start never autoconnects.
func Start(pathsJSON string, bridge DeviceBridge, listener Listener) error {
	mu.Lock()
	defer mu.Unlock()
	if current != nil {
		return errors.New("kumiho mobile: already started; close the engine first")
	}
	f, err := newFacade(pathsJSON, bridge, listener)
	if err != nil {
		return err
	}
	current = f
	f.start()
	return nil
}

// Command dispatches one JSON request and returns a JSON response envelope.
// A domain failure is an {"error":{...}} envelope with a nil error; the Go
// error is reserved for facade failures: not started, a malformed request or
// a panic.
func Command(requestJSON string) (string, error) {
	mu.RLock()
	defer mu.RUnlock()
	f := current
	if f == nil {
		return "", errNotStarted
	}
	return f.command(requestJSON)
}

// Close shuts the engine down: it cancels the root context, stops the tunnel
// and the pumps, and closes the log file. Close is idempotent, and a later
// Start creates a fresh facade.
func Close() error {
	mu.Lock()
	f := current
	current = nil
	if f != nil {
		f.shutdown()
	}
	mu.Unlock()
	if f != nil {
		// Wait outside the lock: a listener callback that re-enters Command
		// must get the "not started" error instead of deadlocking on mu.
		f.wg.Wait()
	}
	return nil
}

// Palette returns the house palette for a theme and accent as a JSON object
// of semantic roles (custom-property names without the "--" prefix). It does
// not require Start. Unknown themes and accents are Go errors.
func Palette(themeName, accent string) (string, error) {
	roles, err := theme.Palette(theme.Theme(themeName), accent)
	if err != nil {
		return "", fmt.Errorf("kumiho mobile: %w", err)
	}
	raw, err := json.Marshal(roles)
	if err != nil {
		return "", fmt.Errorf("kumiho mobile: encode palette: %w", err)
	}
	return string(raw), nil
}

// Version returns the build version with its commit and build date.
func Version() string { return version.String() }
