package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/benyamin-git/kumiho/internal/gui/prefs"
	"github.com/benyamin-git/kumiho/internal/gui/theme"
)

// Harness route paths, matched against the token-stripped request path.
const (
	pathEvents = "/events"
	pathAction = "/action"
	pathDev    = "/dev"
)

// Harness wire defaults and bounds.
const (
	// defaultPushInterval mirrors the shell's 250 ms snapshot-change push.
	defaultPushInterval = 250 * time.Millisecond

	// defaultHeartbeatInterval is the SSE keepalive comment cadence.
	defaultHeartbeatInterval = 15 * time.Second

	// subscriberBuffer is one SSE connection's queue. A full queue drops its
	// oldest envelope to keep the newest: state envelopes are complete
	// snapshots, so the newest alone is the current UI state, and a wedged
	// consumer can never block the broadcaster.
	subscriberBuffer = 16

	// maxBodyBytes bounds one action or dev-endpoint request body.
	maxBodyBytes = 1 << 20
)

// Host-side action names the harness handles itself, mirroring the frozen
// vocabulary in internal/gui (package web cannot import it; see the Harness
// doc comment). Every other decoded action — including refresh — goes to the
// controller through Dispatch. DecodeAction validates the names first, so
// these literals can only route actions the bridge vocabulary accepts.
const (
	actionPrefsSet     = "prefs_set"
	actionOpenExternal = "open_external"
	actionWindowShow   = "window_show"
	actionQuit         = "quit"
)

// Action is the harness's copy of one decoded UI → host action envelope
// (internal/gui.Action adapted by the composition root; see the Harness doc
// comment for why this package cannot use the original).
type Action struct {
	Name    string
	Payload json.RawMessage
}

// DevControl is the /dev panel's seam onto the mock daemon (T1.5), injected
// because package web cannot import internal/gui/mock (see the Harness doc
// comment). Every field is required once a harness is built for a dev
// server.
type DevControl struct {
	// SetScenario switches the mock's reported daemon state; name is one of
	// the four scenario names, validated by the endpoint.
	SetScenario func(name string)
	// SetLatency delays every subsequent mock call by d (0 disables).
	SetLatency func(d time.Duration)
	// FailNext arms the next mock call to fail with err (nil clears).
	FailNext func(err error)
	// EmitLog pushes one live log entry carrying the given message.
	EmitLog func(message string)
}

// HarnessConfig configures a Harness. Server, Snapshot, DecodeAction and
// Dispatch are required.
type HarnessConfig struct {
	// Server is the asset server the harness attaches its routes to:
	// GET /events, POST /action and the /dev endpoints — the latter only
	// when the server runs with Options.Dev. It also receives SetThemeCSS
	// when prefs_set changes the theme or accent.
	Server *Server

	// Snapshot returns the encoded current state
	// (gui.EncodeState of the controller's Snapshot): the host → UI half of
	// the bridge.
	Snapshot func() ([]byte, error)

	// DecodeAction parses and validates one UI → host action envelope
	// (gui.DecodeAction). A malformed envelope or a name outside the frozen
	// vocabulary rejects the request with a 400.
	DecodeAction func(raw []byte) (Action, error)

	// Dispatch runs one controller action (gui.Controller.Dispatch). It runs
	// on its own goroutine after the endpoint already answered {"ok":true};
	// a returned error is logged only, because controller failures reach the
	// UI through state notices (the shell's "accepted, not completed"
	// contract).
	Dispatch func(Action) error

	// SetOnboarded applies one prefs_set onboarding-flag change to the host
	// state (T3.4): the composition root wires it to
	// gui.Controller.SetOnboarded, exactly the shell's rule. Optional; nil
	// persists the flag without mirroring it (tests that do not read
	// State.Onboarded).
	SetOnboarded func(bool)

	// Prefs is the current GUI preference set (prefs.Load); prefs_set merges
	// into it.
	Prefs prefs.Prefs

	// DevControl drives the mock daemon from the /dev endpoints. Required
	// when the server runs with Options.Dev.
	DevControl *DevControl

	// PushInterval is the snapshot-change push cadence (default 250 ms, the
	// shell's UI pump). Zero takes the default.
	PushInterval time.Duration

	// HeartbeatInterval is the SSE keepalive comment cadence (default 15 s).
	// Zero takes the default.
	HeartbeatInterval time.Duration

	// Logf receives one line per harness event; nil discards.
	Logf func(format string, args ...any)
}

// Harness serves the GUI bridge over HTTP against the asset server: the SSE
// state stream, the action endpoint and — when the server is in dev mode —
// the mock control panel. Construct it with NewHarness before the server
// starts.
//
// The harness is the browser twin of the Windows shell's WebView2 bridge
// (T1.10): the same JSON envelopes run over HTTP instead of the webview
// message channel.
//
//   - host → UI: GET /events is a Server-Sent Events stream; every event's
//     data is one state envelope ({"type":"state",...}), the exact bytes
//     gui.EncodeState produces and the shell evaluates through
//     window.kumihoReceive(<json>).
//   - UI → host: POST /action accepts one action envelope
//     ({"type":"action","name":...,"payload":...}, the exact bytes the
//     shell's kumihoAction binding receives) and answers {"ok":true} as soon
//     as the action is accepted — never that it completed. Controller
//     failures surface through the controller's own state notices, exactly
//     like the shell; the harness only logs them.
//
// The harness attaches its routes to the asset server (server.go's route
// hook), so the UI, the state stream and the action endpoints are
// same-origin, and the token gate and the static asset rules stay untouched.
//
// Import rule: this package must never import internal/gui or
// internal/gui/mock. The Windows shell (internal/gui/host_windows.go) imports
// web, so a back-import would form a gui → web → gui cycle on windows and
// break the cross-build gate (verified: "import cycle not allowed"). The
// gui-typed pieces are therefore injected as closures by the composition root
// (cmd/kumiho-gui-web) and by tests; web stays a leaf package. The
// internal/gui prefs and theme packages are fine (they do not import gui or
// web).
type Harness struct {
	cfg       HarnessConfig
	pushEvery time.Duration
	heartbeat time.Duration
	dev       bool

	mu    sync.Mutex
	subs  map[*subscriber]struct{}
	prefs prefs.Prefs
}

// subscriber is one live SSE connection's queue.
type subscriber struct {
	ch chan []byte
}

// NewHarness validates cfg, attaches the harness routes to cfg.Server and
// returns the harness; Run then drives the state push loop. Routes are
// attached once: a second NewHarness over the same server fails.
func NewHarness(cfg HarnessConfig) (*Harness, error) {
	switch {
	case cfg.Server == nil:
		return nil, errors.New("web: harness: Server is required")
	case cfg.Snapshot == nil:
		return nil, errors.New("web: harness: Snapshot is required")
	case cfg.DecodeAction == nil:
		return nil, errors.New("web: harness: DecodeAction is required")
	case cfg.Dispatch == nil:
		return nil, errors.New("web: harness: Dispatch is required")
	case cfg.DevControl != nil &&
		(cfg.DevControl.SetScenario == nil || cfg.DevControl.SetLatency == nil ||
			cfg.DevControl.FailNext == nil || cfg.DevControl.EmitLog == nil):
		return nil, errors.New("web: harness: DevControl fields must all be set")
	case cfg.Server.opts.Dev && cfg.DevControl == nil:
		return nil, errors.New("web: harness: server is in dev mode but DevControl is nil")
	}

	h := &Harness{
		cfg:       cfg,
		pushEvery: cfg.PushInterval,
		heartbeat: cfg.HeartbeatInterval,
		dev:       cfg.Server.opts.Dev,
		subs:      make(map[*subscriber]struct{}),
		prefs:     cfg.Prefs,
	}
	if h.pushEvery <= 0 {
		h.pushEvery = defaultPushInterval
	}
	if h.heartbeat <= 0 {
		h.heartbeat = defaultHeartbeatInterval
	}
	if err := cfg.Server.setRouteHandler(h.route); err != nil {
		return nil, err
	}
	return h, nil
}

// Run drives the host → UI push loop until ctx is done: every PushInterval
// it encodes the current state and broadcasts it to the SSE subscribers only
// when it changed since the last push (the shell's snapshot-change pump).
// Callers run it in its own goroutine.
func (h *Harness) Run(ctx context.Context) {
	ticker := time.NewTicker(h.pushEvery)
	defer ticker.Stop()
	var last []byte
	for {
		raw, err := h.cfg.Snapshot()
		if err != nil {
			h.logf("bridge: encode state: %v", err)
		} else if !bytes.Equal(raw, last) {
			last = raw
			h.broadcast(raw)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// route is the server's dynamic route hook: it claims the harness's paths and
// reports false for everything else, so every asset request keeps the
// static handler's exact behavior. It always claims the /dev namespace, so a
// harness built over a non-dev server answers 404 for the dev surface
// instead of leaking it into the static path.
func (h *Harness) route(w http.ResponseWriter, r *http.Request, p string) bool {
	switch {
	case p == pathEvents:
		h.serveEvents(w, r)
	case p == pathAction:
		h.serveAction(w, r)
	case p == pathDev || strings.HasPrefix(p, pathDev+"/"):
		h.serveDev(w, r, p)
	default:
		return false
	}
	return true
}

// serveEvents is GET /events: one Server-Sent Events stream per subscriber.
// The current snapshot is sent immediately on connect, then every broadcast
// state change; a comment line every heartbeat interval keeps idle
// connections alive. Delivery is per-connection and drop-oldest (see
// subscriberBuffer), so a slow consumer never blocks the broadcaster.
func (h *Harness) serveEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	// Subscribe and snapshot under one lock so no event the subscriber
	// receives predates the snapshot it was sent first.
	sub, raw, err := h.subscribe()
	if err != nil {
		h.logf("bridge: initial snapshot: %v", err)
	}
	defer h.unsubscribe(sub)

	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-store")

	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	if raw != nil {
		if err := writeSSE(w, raw); err != nil {
			return
		}
		flusher.Flush()
	}

	ticker := time.NewTicker(h.heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case raw := <-sub.ch:
			if err := writeSSE(w, raw); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := w.Write([]byte(": heartbeat\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// subscribe registers a subscriber and takes the current snapshot under the
// same lock (see serveEvents). A snapshot failure is reported; the
// subscription stays registered so later pushes still reach the connection.
func (h *Harness) subscribe() (*subscriber, []byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	sub := &subscriber{ch: make(chan []byte, subscriberBuffer)}
	h.subs[sub] = struct{}{}
	raw, err := h.cfg.Snapshot()
	if err != nil {
		return sub, nil, err
	}
	return sub, raw, nil
}

// unsubscribe drops a subscriber. The queue channel is left open: broadcasts
// only ever send, subscriberBuffer-bounded, so closing it would risk a panic
// for no benefit.
func (h *Harness) unsubscribe(sub *subscriber) {
	h.mu.Lock()
	delete(h.subs, sub)
	h.mu.Unlock()
}

// broadcast enqueues raw to every subscriber. A full queue drops its oldest
// envelope to make room, keeping the newest: state envelopes are complete
// snapshots, so an intermediate frame is worthless once a newer one is
// queued, and a wedged SSE consumer only ever slows itself down.
func (h *Harness) broadcast(raw []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		select {
		case sub.ch <- raw:
			continue
		default:
		}
		select {
		case <-sub.ch:
		default:
		}
		select {
		case sub.ch <- raw:
		default:
		}
	}
}

// writeSSE writes one event frame: the envelope bytes as the frame's data
// (one data: line per line; gui.EncodeState's compact JSON is a single line
// in practice), terminated by the blank line that dispatches the event.
func writeSSE(w http.ResponseWriter, raw []byte) error {
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if _, err := w.Write(append([]byte("data: "), line...)); err != nil {
			return err
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			return err
		}
	}
	_, err := w.Write([]byte("\n"))
	if err != nil {
		return err
	}
	return nil
}

// serveAction is POST /action: one action envelope in, {"ok":true} out as
// soon as it is accepted. A malformed envelope or an unknown name is a 400
// with the decode error. Host-side actions (prefs_set, open_external,
// window_show, quit) run synchronously and can reject with a 400; everything
// else goes to the controller on its own goroutine, the shell's rule —
// connect and disconnect may take up to 90 s and must never block the
// endpoint.
func (h *Harness) serveAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	raw, err := readBody(w, r)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	act, err := h.cfg.DecodeAction(raw)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch act.Name {
	case actionPrefsSet:
		if err := h.applyPrefs(act.Payload); err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
	case actionOpenExternal:
		if err := h.openExternal(act.Payload); err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
	case actionWindowShow, actionQuit:
		h.logf("action %s: no-op in the web harness", act.Name)
	default:
		h.dispatch(act)
	}
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

// dispatch runs one controller action in the background: Dispatch may block
// for the action's full daemon timeout, and the response must not.
func (h *Harness) dispatch(act Action) {
	go func() {
		if err := h.cfg.Dispatch(act); err != nil {
			h.logf("action %s: %v", act.Name, err)
		}
	}()
}

// pushNow broadcasts the current state immediately, without the change
// filter the Run loop applies; prefs_set uses it so the UI always sees the
// action completed.
func (h *Harness) pushNow() {
	raw, err := h.cfg.Snapshot()
	if err != nil {
		h.logf("bridge: encode state: %v", err)
		return
	}
	h.broadcast(raw)
}

// prefsSetPayload is the prefs_set action payload: any subset of the GUI
// preferences the page may change (the shell's shape).
type prefsSetPayload struct {
	Theme     string `json:"theme,omitempty"`
	Accent    string `json:"accent,omitempty"`
	Autostart *bool  `json:"autostart,omitempty"`
	Onboarded *bool  `json:"onboarded,omitempty"`
}

// applyPrefs handles prefs_set host-side, mirroring the Windows shell: merge
// the payload into the current preferences, validate the resulting
// appearance (unknown theme or accent names reject the action), persist
// gui.toml through prefs.Save — which honours the KUMIHO_GUI_PREFS override —
// and re-inject the theme CSS, so the change is live for the next index
// render without a restart (the shell's Navigate becomes the browser's
// reload). An onboarded change is pushed into the host state through
// SetOnboarded once the file is saved, the shell's rule (T3.4). The current
// state is pushed either way, so the UI sees the completed action. The
// controller's State.Theme/State.Accent stay at their construction values,
// exactly as in the shell.
func (h *Harness) applyPrefs(raw json.RawMessage) error {
	p, err := decodePayload[prefsSetPayload](raw)
	if err != nil {
		return fmt.Errorf("prefs_set: %w", err)
	}
	h.mu.Lock()
	current := h.prefs
	h.mu.Unlock()

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
	if updated != current {
		if err := prefs.Save(updated); err != nil {
			return fmt.Errorf("prefs_set: %w", err)
		}
		h.mu.Lock()
		h.prefs = updated
		h.mu.Unlock()
		if updated.Onboarded != current.Onboarded && h.cfg.SetOnboarded != nil {
			h.cfg.SetOnboarded(updated.Onboarded)
		}
		if updated.Theme != current.Theme || updated.Accent != current.Accent {
			h.cfg.Server.SetThemeCSS(css)
		}
		h.logf("prefs_set: theme=%s accent=%s autostart=%v onboarded=%v",
			updated.Theme, updated.Accent, updated.Autostart, updated.Onboarded)
	}
	h.pushNow()
	return nil
}

// openExternalPayload is the open_external action payload.
type openExternalPayload struct {
	URL string `json:"url"`
}

// openExternal validates the link exactly like the shell (http/https only;
// the payload is untrusted page input) and logs it. The harness never opens
// a browser on the dev box.
func (h *Harness) openExternal(raw json.RawMessage) error {
	p, err := decodePayload[openExternalPayload](raw)
	if err != nil {
		return fmt.Errorf("open_external: %w", err)
	}
	u, err := url.Parse(p.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("open_external: refusing non-http(s) URL")
	}
	// The authorization URL carries the OAuth state and PKCE challenge; log
	// the host only, never the complete URL.
	h.logf("open_external: %s", u.Host)
	return nil
}

// decodePayload decodes one action or dev payload strictly (unknown fields
// are errors, the bridge's convention); an absent payload yields the zero
// value.
func decodePayload[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 {
		return v, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, err
	}
	return v, nil
}

// readBody reads one bounded request body; an oversized body is an error.
func readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return raw, nil
}

// okResponse is every accepted endpoint's body.
type okResponse struct {
	OK bool `json:"ok"`
}

// errorResponse is the harness's rejection body; the message is the same
// text the shell's kumihoAction binding returns for a refused action.
type errorResponse struct {
	Error string `json:"error"`
}

// writeJSON writes one JSON response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// httpError writes the harness's JSON error shape.
func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// logf writes one harness log line through the configured logger.
func (h *Harness) logf(format string, args ...any) {
	if h.cfg.Logf != nil {
		h.cfg.Logf(format, args...)
	}
}

// themeCSS returns the server's current theme block, for the dev page.
func (h *Harness) themeCSS() string {
	s := h.cfg.Server
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.themeCSS
}
