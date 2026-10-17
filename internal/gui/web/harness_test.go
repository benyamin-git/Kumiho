// External test package: these tests wire the real controller, mock and
// theme/prefs packages, which package web itself must not import (the
// Windows shell imports web; an in-tree test importing internal/gui would
// put a gui → web → gui cycle in the windows test build, which the gate's
// GOOS=windows vet rejects).
package web_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/gui"
	"github.com/benyamin-git/kumiho/internal/gui/mock"
	"github.com/benyamin-git/kumiho/internal/gui/prefs"
	"github.com/benyamin-git/kumiho/internal/gui/theme"
	"github.com/benyamin-git/kumiho/internal/gui/web"
	"github.com/benyamin-git/kumiho/internal/logging"
)

// fixture is one fully wired harness: a real controller over the mock
// daemon, the asset server and the harness, started on an ephemeral
// loopback port with the same callbacks the composition root injects.
type fixture struct {
	srv       *web.Server
	mock      *mock.Mock
	ctrl      *gui.Controller
	prefsPath string
}

// base is the server's URL (with the token segment when configured), ending
// in "/".
func (f *fixture) base() string { return f.srv.URL() }

type fixtureOptions struct {
	dev       bool
	token     string
	push      time.Duration
	heartbeat time.Duration
}

// startFixture composes and starts one harness and registers its teardown.
func startFixture(t *testing.T, opts fixtureOptions) *fixture {
	t.Helper()
	if opts.push <= 0 {
		opts.push = 5 * time.Millisecond
	}
	logger := newSafeLogger(t)

	m := mock.New()
	prefPath := filepath.Join(t.TempDir(), "gui.toml")
	t.Setenv("KUMIHO_GUI_PREFS", prefPath)

	p := prefs.Prefs{Theme: "dark", Accent: "blue", Autostart: true}
	ctrl := gui.NewController(m, gui.Options{
		Theme:     "dark",
		Accent:    "blue",
		Onboarded: p.Onboarded,
	})
	srv := web.New(web.StaticFS(), web.Options{
		Addr:  "127.0.0.1:0",
		Token: opts.token,
		Dev:   opts.dev,
	})
	h, err := web.NewHarness(web.HarnessConfig{
		Server: srv,
		Snapshot: func() ([]byte, error) {
			return gui.EncodeState(ctrl.Snapshot())
		},
		DecodeAction: func(raw []byte) (web.Action, error) {
			a, err := gui.DecodeAction(raw)
			if err != nil {
				return web.Action{}, err
			}
			return web.Action{Name: a.Name, Payload: a.Payload}, nil
		},
		Dispatch: func(a web.Action) error {
			return ctrl.Dispatch(gui.Action{Name: a.Name, Payload: a.Payload})
		},
		SetOnboarded:      ctrl.SetOnboarded,
		Prefs:             p,
		DevControl:        devControl(m),
		PushInterval:      opts.push,
		HeartbeatInterval: opts.heartbeat,
		Logf:              logger.logf,
	})
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	css, err := theme.CSS(theme.Theme(p.Theme), p.Accent)
	if err != nil {
		t.Fatalf("theme.CSS: %v", err)
	}
	srv.SetThemeCSS(css)

	ctx, cancel := context.WithCancel(context.Background())
	go ctrl.Poll(ctx)
	go h.Run(ctx)
	t.Cleanup(func() {
		cancel()
		if err := srv.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		logger.stop()
	})
	return &fixture{srv: srv, mock: m, ctrl: ctrl, prefsPath: prefPath}
}

// devControl builds the /dev endpoints' seam over a mock, the same wiring
// the composition root uses.
func devControl(m *mock.Mock) *web.DevControl {
	return &web.DevControl{
		SetScenario: func(name string) { m.SetScenario(mock.Scenario(name)) },
		SetLatency:  m.SetLatency,
		FailNext:    m.FailNext,
		EmitLog: func(message string) {
			m.EmitLog(logging.Entry{Time: time.Now(), Level: logging.Info, Tag: "dev", Msg: message})
		},
	}
}

// safeLogger forwards harness log lines to t.Logf but goes silent once the
// test's cleanup ran, so a late goroutine can never log after the test
// completed (which testing.T treats as a panic).
type safeLogger struct {
	mu   sync.Mutex
	t    *testing.T
	done bool
}

func newSafeLogger(t *testing.T) *safeLogger { return &safeLogger{t: t} }

func (l *safeLogger) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.done {
		l.t.Logf(format, args...)
	}
}

func (l *safeLogger) stop() {
	l.mu.Lock()
	l.done = true
	l.mu.Unlock()
}

// testClient bounds plain requests; SSE streams use request contexts.
var testClient = &http.Client{Timeout: 5 * time.Second}

type response struct {
	status int
	header http.Header
	body   string
}

// do performs one request and reads its whole body.
func do(t *testing.T, method, url string, body io.Reader) response {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("NewRequest(%s %s): %v", method, url, err)
	}
	resp, err := testClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s: %v", method, url, err)
	}
	return response{status: resp.StatusCode, header: resp.Header, body: string(raw)}
}

// postAction POSTs one action envelope at the harness.
func postAction(t *testing.T, base, envelope string) response {
	t.Helper()
	return do(t, http.MethodPost, base+"action", strings.NewReader(envelope))
}

// postDev POSTs one dev endpoint body.
func postDev(t *testing.T, base, path, body string) response {
	t.Helper()
	return do(t, http.MethodPost, base+path, strings.NewReader(body))
}

// waitFor polls cond until it holds or a deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// sseStream is one open GET /events connection with a line scanner.
type sseStream struct {
	scan *bufio.Scanner
}

// openSSE opens the events stream and leaves it open for the test.
func openSSE(t *testing.T, url string) *sseStream {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d; want 200", url, resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("GET %s Content-Type = %q; want text/event-stream", url, ct)
	}
	return &sseStream{scan: bufio.NewScanner(resp.Body)}
}

// nextData returns the next event frame's data payload, skipping comment
// lines (heartbeats).
func (s *sseStream) nextData(t *testing.T) string {
	t.Helper()
	var data []string
	for s.scan.Scan() {
		line := s.scan.Text()
		switch {
		case line == "":
			if len(data) > 0 {
				return strings.Join(data, "\n")
			}
		case strings.HasPrefix(line, ":"):
			// heartbeat comment
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		default:
			t.Fatalf("unexpected SSE line %q", line)
		}
	}
	t.Fatalf("SSE stream ended: %v", s.scan.Err())
	return ""
}

// stateEnvelope is the wire shape of a host → UI state frame.
type stateEnvelope struct {
	Type  string    `json:"type"`
	State gui.State `json:"state"`
}

// nextState reads frames until one decodes to a state satisfying want.
func (s *sseStream) nextState(t *testing.T, want func(gui.State) bool) gui.State {
	t.Helper()
	for {
		raw := s.nextData(t)
		var env stateEnvelope
		if err := json.Unmarshal([]byte(raw), &env); err != nil {
			t.Fatalf("decode state envelope %q: %v", raw, err)
		}
		if env.Type != "state" {
			t.Fatalf("envelope type = %q; want %q", env.Type, "state")
		}
		if want(env.State) {
			return env.State
		}
	}
}

func TestHarnessSSERoundTrip(t *testing.T) {
	f := startFixture(t, fixtureOptions{})
	stream := openSSE(t, f.base()+"events")

	// The first frame is the current snapshot, sent on connect.
	first := stream.nextState(t, func(gui.State) bool { return true })
	if first.LocationsLoaded && first.Locations == nil {
		t.Error("locations_loaded is true but the list is nil")
	}

	// A mock scenario switch pushes a status event; the controller applies
	// it and the push loop broadcasts the changed state.
	f.mock.SetScenario(mock.ScenarioConnected)
	got := stream.nextState(t, func(s gui.State) bool {
		return s.Status != nil && s.Status.Label == "Connected"
	})
	if !got.Reachable {
		t.Error("connected state reports the daemon unreachable")
	}
	if got.Status.Email != "user@example.com" {
		t.Errorf("connected state email = %q; want the canned mock account", got.Status.Email)
	}
}

func TestHarnessSSEHeartbeat(t *testing.T) {
	f := startFixture(t, fixtureOptions{heartbeat: 20 * time.Millisecond})
	stream := openSSE(t, f.base()+"events")
	stream.nextState(t, func(gui.State) bool { return true })

	for stream.scan.Scan() {
		if strings.HasPrefix(stream.scan.Text(), ":") {
			return
		}
	}
	t.Fatalf("no heartbeat comment arrived: %v", stream.scan.Err())
}

func TestHarnessActionPostRejects(t *testing.T) {
	f := startFixture(t, fixtureOptions{})
	stream := openSSE(t, f.base()+"events")
	stream.nextState(t, func(gui.State) bool { return true })

	cases := []struct {
		body string
		why  string
	}{
		{"", "empty body"},
		{"{", "malformed JSON"},
		{"[]", "wrong envelope shape"},
		{`{"type":"state","name":"connect"}`, "wrong envelope type"},
		{`{"type":"action","name":""}`, "empty action name"},
		{`{"type":"action","name":"nope"}`, "unknown action name"},
		{`{"type":"action","name":"connect","extra":1}`, "unknown envelope field"},
	}
	for _, tc := range cases {
		got := postAction(t, f.base(), tc.body)
		if got.status != http.StatusBadRequest {
			t.Errorf("POST action %q (%s) = %d; want 400", tc.body, tc.why, got.status)
		}
		if !strings.Contains(got.body, `"error"`) {
			t.Errorf("POST action %q (%s) body = %q; want a JSON error", tc.body, tc.why, got.body)
		}
	}
}

func TestHarnessActionPostAccepted(t *testing.T) {
	f := startFixture(t, fixtureOptions{})
	stream := openSSE(t, f.base()+"events")
	stream.nextState(t, func(gui.State) bool { return true })

	got := postAction(t, f.base(), `{"type":"action","name":"connect"}`)
	if got.status != http.StatusOK {
		t.Fatalf("POST connect = %d (%s); want 200", got.status, got.body)
	}
	if body := strings.TrimSpace(got.body); body != `{"ok":true}` {
		t.Fatalf("POST connect body = %q; want {\"ok\":true}", body)
	}

	// "Accepted" means the controller ran the action: the mock records the
	// connect and the completion notice arrives through the state stream.
	waitFor(t, "the mock's recorded connect", func() bool {
		_, ok := f.mock.LastConnect()
		return ok
	})
	stream.nextState(t, func(s gui.State) bool {
		return s.Notice != nil && s.Notice.Message == "Connected"
	})
}

func TestHarnessHostActions(t *testing.T) {
	f := startFixture(t, fixtureOptions{})

	for _, name := range []string{gui.ActionWindowShow, gui.ActionQuit} {
		got := postAction(t, f.base(), `{"type":"action","name":"`+name+`"}`)
		if got.status != http.StatusOK {
			t.Errorf("POST %s = %d (%s); want 200", name, got.status, got.body)
		}
	}

	ok := postAction(t, f.base(),
		`{"type":"action","name":"`+gui.ActionOpenExternal+`","payload":{"url":"https://example.com/x"}}`)
	if ok.status != http.StatusOK {
		t.Errorf("POST open_external https = %d (%s); want 200", ok.status, ok.body)
	}

	script := postAction(t, f.base(),
		`{"type":"action","name":"`+gui.ActionOpenExternal+`","payload":{"url":"javascript:alert(1)"}}`)
	if script.status != http.StatusBadRequest {
		t.Errorf("POST open_external javascript: = %d (%s); want 400", script.status, script.body)
	}

	empty := postAction(t, f.base(),
		`{"type":"action","name":"`+gui.ActionOpenExternal+`","payload":{}}`)
	if empty.status != http.StatusBadRequest {
		t.Errorf("POST open_external without a url = %d (%s); want 400", empty.status, empty.body)
	}
}

func TestHarnessPrefsSetLiveTheme(t *testing.T) {
	f := startFixture(t, fixtureOptions{})

	before := do(t, http.MethodGet, f.base(), nil)
	if !strings.Contains(before.body, "color-scheme:dark") {
		t.Fatalf("initial index does not carry the dark theme: %q", before.body)
	}

	got := postAction(t, f.base(),
		`{"type":"action","name":"`+gui.ActionPrefsSet+`","payload":{"theme":"light","accent":"teal"}}`)
	if got.status != http.StatusOK {
		t.Fatalf("POST prefs_set = %d (%s); want 200", got.status, got.body)
	}

	after := do(t, http.MethodGet, f.base(), nil)
	if !strings.Contains(after.body, "color-scheme:light") {
		t.Errorf("index does not carry the new light theme after prefs_set")
	}

	saved, err := prefs.Load()
	if err != nil {
		t.Fatalf("prefs.Load: %v", err)
	}
	if saved.Theme != "light" || saved.Accent != "teal" {
		t.Errorf("saved prefs = theme %q accent %q; want light/teal", saved.Theme, saved.Accent)
	}

	unknown := postAction(t, f.base(),
		`{"type":"action","name":"`+gui.ActionPrefsSet+`","payload":{"theme":"neon"}}`)
	if unknown.status != http.StatusBadRequest {
		t.Errorf("POST prefs_set with an unknown theme = %d (%s); want 400", unknown.status, unknown.body)
	}

	malformed := postAction(t, f.base(),
		`{"type":"action","name":"`+gui.ActionPrefsSet+`","payload":{"theme":42}}`)
	if malformed.status != http.StatusBadRequest {
		t.Errorf("POST prefs_set with a mistyped value = %d (%s); want 400", malformed.status, malformed.body)
	}
}

// TestHarnessPrefsSetOnboarded pins the T3.4 wiring: the wizard's
// `prefs_set {"onboarded":true}` persists gui.toml and mirrors the flag into
// the host state (State.Onboarded), exactly as the shell does — the wizard
// gate's release evidence.
func TestHarnessPrefsSetOnboarded(t *testing.T) {
	f := startFixture(t, fixtureOptions{})
	stream := openSSE(t, f.base()+"events")

	first := stream.nextState(t, func(gui.State) bool { return true })
	if first.Onboarded {
		t.Fatal("onboarded = true before the wizard finished, want false")
	}

	got := postAction(t, f.base(),
		`{"type":"action","name":"`+gui.ActionPrefsSet+`","payload":{"onboarded":true}}`)
	if got.status != http.StatusOK {
		t.Fatalf("POST prefs_set onboarded = %d (%s); want 200", got.status, got.body)
	}

	flipped := stream.nextState(t, func(s gui.State) bool { return s.Onboarded })
	if !flipped.Onboarded {
		t.Fatal("state.onboarded did not flip after prefs_set")
	}

	saved, err := prefs.Load()
	if err != nil {
		t.Fatalf("prefs.Load: %v", err)
	}
	if !saved.Onboarded {
		t.Error("saved prefs have onboarded = false; want true")
	}

	malformed := postAction(t, f.base(),
		`{"type":"action","name":"`+gui.ActionPrefsSet+`","payload":{"onboarded":"yes"}}`)
	if malformed.status != http.StatusBadRequest {
		t.Errorf("POST prefs_set with a mistyped onboarded = %d (%s); want 400", malformed.status, malformed.body)
	}
}

func TestHarnessDevEndpointsDisabled(t *testing.T) {
	f := startFixture(t, fixtureOptions{dev: false})

	cases := []struct{ method, path string }{
		{http.MethodGet, "dev"},
		{http.MethodGet, "dev/scenario"},
		{http.MethodPost, "dev/scenario"},
		{http.MethodPost, "dev/latency"},
		{http.MethodPost, "dev/fail"},
		{http.MethodPost, "dev/log"},
	}
	for _, tc := range cases {
		got := do(t, tc.method, f.base()+tc.path, strings.NewReader("{}"))
		if got.status != http.StatusNotFound {
			t.Errorf("%s %s (dev off) = %d; want 404", tc.method, tc.path, got.status)
		}
	}

	if got := do(t, http.MethodGet, f.base(), nil); got.status != http.StatusOK {
		t.Errorf("GET / (dev off) = %d; want 200", got.status)
	}
}

func TestHarnessDevEndpoints(t *testing.T) {
	f := startFixture(t, fixtureOptions{dev: true})

	page := do(t, http.MethodGet, f.base()+"dev", nil)
	if page.status != http.StatusOK {
		t.Fatalf("GET /dev = %d; want 200", page.status)
	}
	if !strings.Contains(page.body, "Kumiho web harness") {
		t.Errorf("GET /dev body does not contain the panel heading")
	}
	if strings.Contains(page.body, "{{THEME}}") || strings.Contains(page.body, "{{NONCE}}") {
		t.Errorf("GET /dev left a placeholder unsubstituted")
	}
	if csp := page.header.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'nonce-") {
		t.Errorf("GET /dev CSP = %q; want a script-src nonce", csp)
	}

	// Scenario switch: the controller sees the new state.
	if got := postDev(t, f.base(), "dev/scenario", `{"scenario":"connected"}`); got.status != http.StatusOK {
		t.Fatalf("POST /dev/scenario = %d (%s); want 200", got.status, got.body)
	}
	waitFor(t, "the controller to report Connected", func() bool {
		s := f.ctrl.Snapshot()
		return s.Status != nil && s.Status.Label == "Connected"
	})
	if got := postDev(t, f.base(), "dev/scenario", `{"scenario":"party"}`); got.status != http.StatusBadRequest {
		t.Errorf("POST /dev/scenario unknown = %d (%s); want 400", got.status, got.body)
	}

	// Latency: the mock's calls cannot finish faster than the delay.
	if got := postDev(t, f.base(), "dev/latency", `{"ms":150}`); got.status != http.StatusOK {
		t.Fatalf("POST /dev/latency = %d (%s); want 200", got.status, got.body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	if _, err := f.mock.Status(ctx); err != nil {
		t.Fatalf("mock.Status under latency: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 120*time.Millisecond {
		t.Errorf("latency 150ms not applied: mock call took %v", elapsed)
	}
	if got := postDev(t, f.base(), "dev/latency", `{"ms":-1}`); got.status != http.StatusBadRequest {
		t.Errorf("POST /dev/latency negative = %d (%s); want 400", got.status, got.body)
	}
	if got := postDev(t, f.base(), "dev/latency", `{"ms":0}`); got.status != http.StatusOK {
		t.Errorf("POST /dev/latency reset = %d (%s); want 200", got.status, got.body)
	}

	// Fail-next: armed once, consumed by the next mock call, clearable.
	if got := postDev(t, f.base(), "dev/fail", `{"on":true,"message":"dev boom"}`); got.status != http.StatusOK {
		t.Fatalf("POST /dev/fail on = %d (%s); want 200", got.status, got.body)
	}
	if _, err := f.mock.Status(ctx); err == nil || err.Error() != "dev boom" {
		t.Errorf("armed failure = %v; want \"dev boom\"", err)
	}
	if _, err := f.mock.Status(ctx); err != nil {
		t.Errorf("failure was not consumed once: %v", err)
	}
	if got := postDev(t, f.base(), "dev/fail", `{"on":true}`); got.status != http.StatusOK {
		t.Fatalf("POST /dev/fail default message = %d (%s); want 200", got.status, got.body)
	}
	if got := postDev(t, f.base(), "dev/fail", `{"on":false}`); got.status != http.StatusOK {
		t.Errorf("POST /dev/fail off = %d (%s); want 200", got.status, got.body)
	}
	if _, err := f.mock.Status(ctx); err != nil {
		t.Errorf("cleared failure still fails: %v", err)
	}

	// Emit-log: visible once the UI has subscribed to the live log stream.
	if err := f.ctrl.Dispatch(gui.Action{
		Name:    gui.ActionLogsSubscribe,
		Payload: json.RawMessage(`{"level":"debug"}`),
	}); err != nil {
		t.Fatalf("logs_subscribe: %v", err)
	}
	if got := postDev(t, f.base(), "dev/log", `{"message":"capture probe"}`); got.status != http.StatusOK {
		t.Fatalf("POST /dev/log = %d (%s); want 200", got.status, got.body)
	}
	waitFor(t, "the emitted log entry", func() bool {
		for _, e := range f.ctrl.Snapshot().Logs {
			if e.Msg == "capture probe" {
				return true
			}
		}
		return false
	})

	// Method and path rules.
	if got := do(t, http.MethodPost, f.base()+"dev", strings.NewReader("{}")); got.status != http.StatusMethodNotAllowed {
		t.Errorf("POST /dev = %d; want 405", got.status)
	}
	if got := do(t, http.MethodGet, f.base()+"dev/scenario", nil); got.status != http.StatusMethodNotAllowed {
		t.Errorf("GET /dev/scenario = %d; want 405", got.status)
	}
	if got := postDev(t, f.base(), "dev/nope", `{}`); got.status != http.StatusNotFound {
		t.Errorf("POST /dev/nope = %d; want 404", got.status)
	}
	if got := postDev(t, f.base(), "dev/scenario", `{`); got.status != http.StatusBadRequest {
		t.Errorf("POST /dev/scenario malformed = %d; want 400", got.status)
	}
}

func TestHarnessTokenGate(t *testing.T) {
	const token = "run-tok-1"
	f := startFixture(t, fixtureOptions{token: token})
	base := f.base()
	root := strings.TrimSuffix(base, token+"/")

	// Without the token every harness path looks like a missing file.
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "events"},
		{http.MethodPost, "action"},
		{http.MethodGet, "dev"},
		{http.MethodPost, "dev/scenario"},
	} {
		got := do(t, tc.method, root+tc.path, strings.NewReader("{}"))
		if got.status != http.StatusNotFound {
			t.Errorf("%s %s without the token = %d; want 404", tc.method, tc.path, got.status)
		}
	}

	// With it, the stream and the action endpoint serve.
	stream := openSSE(t, base+"events")
	stream.nextState(t, func(gui.State) bool { return true })
	if got := postAction(t, base, `{"type":"action","name":"connect"}`); got.status != http.StatusOK {
		t.Errorf("POST action with the token = %d (%s); want 200", got.status, got.body)
	}
}

func TestHarnessStaticRoutesUnchanged(t *testing.T) {
	f := startFixture(t, fixtureOptions{})

	if got := do(t, http.MethodGet, f.base(), nil); got.status != http.StatusOK {
		t.Errorf("GET / = %d; want 200", got.status)
	}
	if got := do(t, http.MethodGet, f.base()+"index.html", nil); got.status != http.StatusOK {
		t.Errorf("GET /index.html = %d; want 200", got.status)
	}
	if got := do(t, http.MethodGet, f.base()+"nope.css", nil); got.status != http.StatusNotFound {
		t.Errorf("GET /nope.css = %d; want 404", got.status)
	}
	if got := do(t, http.MethodPost, f.base(), strings.NewReader("{}")); got.status != http.StatusMethodNotAllowed {
		t.Errorf("POST / = %d; want 405", got.status)
	}
	if got := do(t, http.MethodPost, f.base()+"index.html", strings.NewReader("{}")); got.status != http.StatusMethodNotAllowed {
		t.Errorf("POST /index.html = %d; want 405", got.status)
	}

	if got := do(t, http.MethodGet, f.base()+"action", nil); got.status != http.StatusMethodNotAllowed ||
		got.header.Get("Allow") != http.MethodPost {
		t.Errorf("GET /action = %d Allow %q; want 405 POST", got.status, got.header.Get("Allow"))
	}
	if got := do(t, http.MethodPost, f.base()+"events", strings.NewReader("{}")); got.status != http.StatusMethodNotAllowed ||
		got.header.Get("Allow") != http.MethodGet {
		t.Errorf("POST /events = %d Allow %q; want 405 GET", got.status, got.header.Get("Allow"))
	}
}
