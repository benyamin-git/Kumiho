package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/engine"
	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/settings"
	"github.com/benyamin-git/kumiho/internal/version"
)

// facade owns one engine instance and everything Kotlin can observe.
type facade struct {
	ctx    context.Context
	cancel context.CancelFunc

	paths    paths
	store    *settings.Store
	ring     *logging.Ring
	sink     *rotatingWriter
	ctrl     *engine.Controller
	bridge   DeviceBridge
	listener Listener

	// statusSignal coalesces status pushes: a burst of engine changes becomes
	// one snapshot, so the engine callback never blocks on Kotlin.
	statusSignal chan struct{}
	wg           sync.WaitGroup

	subMu     sync.Mutex
	cancelLog func()

	stateMu    sync.Mutex
	lastStatus api.Status

	closed    atomic.Bool
	closeOnce sync.Once
}

// newFacade opens the stores, the log ring and the engine. It performs no
// listener callbacks, so Start can hold the facade lock while building it.
func newFacade(pathsJSON string, bridge DeviceBridge, listener Listener) (*facade, error) {
	if listener == nil {
		return nil, errors.New("kumiho mobile: a listener is required")
	}
	p, err := decodePaths(pathsJSON)
	if err != nil {
		return nil, err
	}
	if err := p.prepare(); err != nil {
		return nil, err
	}

	store, err := settings.Open(p.settings())
	if err != nil {
		return nil, fmt.Errorf("kumiho mobile: open settings: %w", err)
	}

	cfg := store.EffectiveConfig()
	level, err := logging.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = logging.Info
	}
	sink, err := newRotatingWriter(filepath.Join(p.Logs, "kumiho.log"))
	if err != nil {
		return nil, fmt.Errorf("kumiho mobile: open log file: %w", err)
	}
	ring := logging.NewRing(logging.Options{
		Capacity: 2000,
		Sanitize: logging.Sanitizer(cfg.RedactTargets),
		Sink:     sink,
		MinLevel: level,
	})

	ctx, cancel := context.WithCancel(context.Background())
	f := &facade{
		ctx:          ctx,
		cancel:       cancel,
		paths:        p,
		store:        store,
		ring:         ring,
		sink:         sink,
		bridge:       bridge,
		listener:     listener,
		statusSignal: make(chan struct{}, 1),
	}
	f.ctrl = engine.New(store, ring, fxa.NewClient(), newProvider(p.settings(), bridge))
	if err := f.ctrl.Start(ctx); err != nil {
		cancel()
		ring.Close()
		_ = sink.Close()
		return nil, fmt.Errorf("kumiho mobile: start session: %w", err)
	}
	f.ctrl.UseBypassDialer()
	f.ctrl.OnChange(f.pushStatus)
	ring.Logf(logging.Info, "mobile", "kumiho %s starting", version.String())
	return f, nil
}

// start launches the status pump and queues the initial status. It is called
// once, under the facade lock, before Start returns.
func (f *facade) start() {
	if f.ctx.Err() != nil {
		return
	}
	f.wg.Add(1)
	go f.runStatusPump()
	f.pushStatus()
}

// shutdown cancels the root context, tears the engine down and closes the log
// side. It does not wait for the pumps; Close waits after releasing the lock.
func (f *facade) shutdown() {
	f.closeOnce.Do(func() {
		f.closed.Store(true)
		f.cancel()
		f.ctrl.Shutdown()
		f.stopLogSubscription()
		f.ring.Close()
		_ = f.sink.Close()
	})
}

func (f *facade) stopLogSubscription() {
	f.subMu.Lock()
	cancel := f.cancelLog
	f.cancelLog = nil
	f.subMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// command validates the envelope and dispatches it. Panics are recovered into
// an internal facade error and a log line rather than crashing Kotlin's JNI
// call.
func (f *facade) command(requestJSON string) (string, error) {
	return f.guard("command", func() (string, error) {
		var req request
		if err := json.Unmarshal([]byte(requestJSON), &req); err != nil {
			return "", fmt.Errorf("kumiho mobile: malformed request: %w", err)
		}
		if req.Type == "" {
			return "", errors.New("kumiho mobile: the request has no type")
		}
		return f.dispatch(f.ctx, &req)
	})
}

// guard runs fn and converts a panic into an internal facade error.
func (f *facade) guard(what string, fn func() (string, error)) (resp string, err error) {
	defer func() {
		if r := recover(); r != nil {
			message := fmt.Sprintf("%s panicked: %v", what, r)
			f.logf(logging.Error, "mobile", "%s", message)
			resp, err = "", &facadeError{code: api.CodeInternal, message: message}
		}
	}()
	return fn()
}

// pushStatus queues one status snapshot without blocking. The pump coalesces
// bursts; the engine's OnChange callback therefore never waits on Kotlin.
func (f *facade) pushStatus() {
	select {
	case f.statusSignal <- struct{}{}:
	default:
	}
}

func (f *facade) runStatusPump() {
	defer f.wg.Done()
	for {
		select {
		case <-f.ctx.Done():
			return
		case <-f.statusSignal:
			f.emitStatus()
		}
	}
}

// emitStatus marshals the snapshot, emits transition notices and calls
// Listener.OnStatus. Only the pump computes notices, so a command's status
// push cannot double-report a transition.
func (f *facade) emitStatus() {
	if f.closed.Load() {
		return
	}
	st := f.ctrl.Snapshot()
	raw, err := json.Marshal(st)
	if err != nil {
		f.logf(logging.Warn, "mobile", "encode status: %v", err)
		return
	}
	f.noteTransitions(st)
	status := string(raw)
	f.emit(func() { f.listener.OnStatus(status) })
}

// noteTransitions emits the two advisory notices: a background move from
// authenticated to unauthenticated, and a move into FATAL.
func (f *facade) noteTransitions(st api.Status) {
	f.stateMu.Lock()
	prev := f.lastStatus
	f.lastStatus = st
	f.stateMu.Unlock()

	if prev.Authenticated && !st.Authenticated {
		f.sendNotice(notice{
			Type:    "signed_out",
			State:   st.State,
			Message: "the session ended; sign in again to reconnect",
		})
	}
	if prev.State != string(engine.StateFatal) && st.State == string(engine.StateFatal) {
		f.sendNotice(notice{
			Type:    "fatal",
			State:   st.State,
			Message: f.lastLogMessage(),
		})
	}
}

func (f *facade) sendNotice(n notice) {
	raw, err := json.Marshal(n)
	if err != nil {
		return
	}
	text := string(raw)
	f.emit(func() { f.listener.OnNotice(text) })
}

// lastLogMessage is the latest warning-or-worse entry, used as the FATAL
// notice text. The notice is advisory; the status snapshot stays canonical.
func (f *facade) lastLogMessage() string {
	entries := f.ring.Snapshot()
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Level >= logging.Warn {
			return entries[i].Msg
		}
	}
	if len(entries) > 0 {
		return entries[len(entries)-1].Msg
	}
	return "the tunnel stopped after a fatal error"
}

// emit runs one listener callback under a panic guard. gomobile surfaces a
// Kotlin exception as a Go panic, so swallowing it protects the process.
func (f *facade) emit(fn func()) {
	if f.closed.Load() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			f.logf(logging.Warn, "mobile", "listener callback panicked: %v", r)
		}
	}()
	fn()
}

// subscribeLogs replaces the current log subscription and starts its pump.
func (f *facade) subscribeLogs(level logging.Level) {
	f.subMu.Lock()
	prev := f.cancelLog
	f.cancelLog = nil
	ch, cancel := f.ring.Subscribe(level)
	f.cancelLog = cancel
	f.subMu.Unlock()
	if prev != nil {
		prev()
	}
	if f.ctx.Err() != nil {
		cancel()
		return
	}
	f.wg.Add(1)
	go f.runLogPump(ch)
}

func (f *facade) runLogPump(ch <-chan logging.Entry) {
	defer f.wg.Done()
	for {
		select {
		case <-f.ctx.Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			raw, err := json.Marshal(e)
			if err != nil {
				f.logf(logging.Warn, "mobile", "encode log entry: %v", err)
				continue
			}
			entry := string(raw)
			f.emit(func() { f.listener.OnLog(entry) })
		}
	}
}

func (f *facade) logf(level logging.Level, tag, format string, args ...any) {
	if f.ring == nil {
		return
	}
	f.ring.Logf(level, tag, format, args...)
}

// debugf is the route helper's logger: IPv6 exclusions are ignored with a
// debug line, matching the Settings UI which rejects them first.
func (f *facade) debugf(format string, args ...any) {
	f.logf(logging.Debug, "mobile", format, args...)
}
