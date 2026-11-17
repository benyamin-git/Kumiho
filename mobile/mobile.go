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

// There is exactly one facade per process. Command only snapshots the
// pointer: a long engine call must not hold a lock that Close needs (the
// Android main thread calls Close during onDestroy). Close clears the pointer
// and detaches the teardown; Start waits for that teardown before building a
// fresh facade.
var (
	lifeMu   sync.Mutex
	current  *facade
	teardown chan struct{}
)

// Start opens the engine for the app-private paths JSON, wires the Kotlin
// DeviceBridge and Listener, and pushes one initial status. A second Start
// returns an "already started" error and leaves the live session alone; call
// Close first to replace it. Start never autoconnects.
func Start(pathsJSON string, bridge DeviceBridge, listener Listener) error {
	lifeMu.Lock()
	if current != nil {
		lifeMu.Unlock()
		return errors.New("kumiho mobile: already started; close the engine first")
	}
	previous := teardown
	lifeMu.Unlock()
	if previous != nil {
		// A close is still releasing the previous engine (tunnel rules, log
		// file). Opening a second controller over the same state would race it.
		<-previous
	}

	f, err := newFacade(pathsJSON, bridge, listener)
	if err != nil {
		return err
	}

	lifeMu.Lock()
	if current != nil {
		// Two Starts raced while the teardown completed; keep the winner.
		lifeMu.Unlock()
		f.shutdown()
		return errors.New("kumiho mobile: already started; close the engine first")
	}
	current = f
	teardown = nil
	f.start()
	lifeMu.Unlock()
	return nil
}

// Command dispatches one JSON request and returns a JSON response envelope.
// A domain failure is an {"error":{...}} envelope with a nil error; the Go
// error is reserved for facade failures: not started, a malformed request or
// a panic.
func Command(requestJSON string) (string, error) {
	lifeMu.Lock()
	f := current
	lifeMu.Unlock()
	if f == nil {
		return "", errNotStarted
	}
	return f.command(requestJSON)
}

// Close retires the engine: it cancels the root context, then tears the
// tunnel, the pumps and the log file down on a background goroutine so an
// Android main-thread caller never waits for netcfg. Close is idempotent, and
// a later Start waits for that teardown before creating a fresh facade.
func Close() error {
	lifeMu.Lock()
	f := current
	current = nil
	if f == nil {
		lifeMu.Unlock()
		return nil
	}
	done := make(chan struct{})
	teardown = done
	lifeMu.Unlock()

	// Teardown can wait on the tunnel and the log sink. Never make the
	// caller (Android's main thread, in onDestroy) wait for it; Start is the
	// only caller that needs the completion, and it waits on teardown.
	go func() {
		defer close(done)
		f.shutdown()
		f.wg.Wait()
	}()
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
