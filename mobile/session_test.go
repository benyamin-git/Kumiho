package mobile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// testListener records every listener callback and mirrors them onto buffered
// channels so tests never sleep on a particular event.
type testListener struct {
	mu sync.Mutex

	statuses []string
	logs     []string
	pings    []string
	notices  []string

	statusCh chan string
	logCh    chan string
	pingCh   chan string
	noticeCh chan string
}

func newTestListener() *testListener {
	return &testListener{
		statusCh: make(chan string, 128),
		logCh:    make(chan string, 128),
		pingCh:   make(chan string, 128),
		noticeCh: make(chan string, 32),
	}
}

func (l *testListener) record(dst *[]string, ch chan string, raw string) {
	l.mu.Lock()
	*dst = append(*dst, raw)
	l.mu.Unlock()
	select {
	case ch <- raw:
	default:
	}
}

func (l *testListener) OnStatus(statusJSON string) { l.record(&l.statuses, l.statusCh, statusJSON) }
func (l *testListener) OnLog(entryJSON string)     { l.record(&l.logs, l.logCh, entryJSON) }
func (l *testListener) OnPing(resultJSON string)   { l.record(&l.pings, l.pingCh, resultJSON) }
func (l *testListener) OnNotice(noticeJSON string) { l.record(&l.notices, l.noticeCh, noticeJSON) }

func (l *testListener) countLogs() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.logs)
}

func waitOn[T any](t *testing.T, ch chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

func (l *testListener) waitStatus(t *testing.T) string {
	return waitOn(t, l.statusCh, "a status callback")
}
func (l *testListener) waitLog(t *testing.T) string  { return waitOn(t, l.logCh, "a log callback") }
func (l *testListener) waitPing(t *testing.T) string { return waitOn(t, l.pingCh, "a ping callback") }
func (l *testListener) waitNotice(t *testing.T) string {
	return waitOn(t, l.noticeCh, "a notice callback")
}

func (l *testListener) drainStatuses() {
	for {
		select {
		case <-l.statusCh:
		default:
			return
		}
	}
}

// testPaths builds app-private paths under a fresh temp dir, mirroring the
// Kotlin EnginePaths layout (files/ for config, state and logs; no-backup for
// tokens; cache for the runtime dir).
func testPaths(t *testing.T) paths {
	t.Helper()
	dir := t.TempDir()
	return paths{
		Config:  filepath.Join(dir, "files", "config.toml"),
		State:   filepath.Join(dir, "files", "state.json"),
		Tokens:  filepath.Join(dir, "no-backup", "tokens.json"),
		Runtime: filepath.Join(dir, "cache", "runtime"),
		Logs:    filepath.Join(dir, "files", "logs"),
	}
}

func (p paths) json(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal paths: %v", err)
	}
	return string(raw)
}

// startTest starts the facade for p and closes it at test cleanup.
func startTest(t *testing.T, p paths, bridge DeviceBridge, l Listener) {
	t.Helper()
	if err := Start(p.json(t), bridge, l); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = Close() })
}

func TestStartWritesDefaultMTUOnlyWhenAbsent(t *testing.T) {
	p := testPaths(t)
	l := newTestListener()
	startTest(t, p, nil, l)

	data, err := os.ReadFile(p.Config)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) != "mtu = 1500\n" {
		t.Fatalf("config = %q, want mtu = 1500", data)
	}
	fi, err := os.Stat(p.Config)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %o, want 600", fi.Mode().Perm())
	}
	for _, dir := range []string{
		filepath.Dir(p.Config), p.Logs, p.Runtime, filepath.Dir(p.Tokens),
	} {
		di, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if !di.IsDir() {
			t.Fatalf("%s is not a directory", dir)
		}
		if di.Mode().Perm() != 0o700 {
			t.Errorf("%s mode = %o, want 700", dir, di.Mode().Perm())
		}
	}
	logPath := filepath.Join(p.Logs, "kumiho.log")
	li, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("stat log file: %v", err)
	}
	if li.Mode().Perm() != 0o600 {
		t.Errorf("log mode = %o, want 600", li.Mode().Perm())
	}
	if err := Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// An existing config is never overwritten.
	if err := os.WriteFile(p.Config, []byte("mtu = 9000\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	startTest(t, p, nil, newTestListener())
	data, err = os.ReadFile(p.Config)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) != "mtu = 9000\n" {
		t.Fatalf("existing config was overwritten: %q", data)
	}
	out := mustCommand(t, "settings_get", nil)
	var view map[string]any
	decodeTestPayload(t, out, &view)
	if mtu, _ := view["mtu"].(float64); mtu != 9000 {
		t.Fatalf("settings mtu = %v, want 9000", view["mtu"])
	}
}

func TestStartRejectsBadPaths(t *testing.T) {
	good := testPaths(t)
	abs := func(name string) string { return filepath.Join(t.TempDir(), name) }
	tests := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"malformed", "{"},
		{"empty object", "{}"},
		{"relative config", `{"config":"rel/config.toml","state":"` + abs("state.json") + `","tokens":"` + abs("tokens.json") + `","runtime":"` + abs("runtime") + `","logs":"` + abs("logs") + `"}`},
		{"missing logs", `{"config":"` + good.Config + `","state":"` + good.State + `","tokens":"` + good.Tokens + `","runtime":"` + good.Runtime + `"}`},
		{"relative logs", `{"config":"` + good.Config + `","state":"` + good.State + `","tokens":"` + good.Tokens + `","runtime":"` + good.Runtime + `","logs":"logs"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := Start(tc.raw, nil, newTestListener()); err == nil {
				_ = Close()
				t.Fatalf("Start(%q) succeeded, want an error", tc.raw)
			}
			if _, err := Command(`{"type":"status"}`); err == nil {
				t.Fatal("Command succeeded after a failed Start")
			}
		})
	}

	t.Run("nil listener", func(t *testing.T) {
		if err := Start(good.json(t), nil, nil); err == nil {
			_ = Close()
			t.Fatal("Start with a nil listener succeeded")
		}
	})
}

func TestStartTwiceIsAnError(t *testing.T) {
	p := testPaths(t)
	l := newTestListener()
	startTest(t, p, nil, l)
	err := Start(p.json(t), nil, l)
	if err == nil || !strings.Contains(err.Error(), "already started") {
		t.Fatalf("second Start = %v, want an already started error", err)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	p := testPaths(t)
	startTest(t, p, nil, newTestListener())
	if err := Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := Command(`{"type":"status"}`); err == nil {
		t.Fatal("Command after Close did not fail")
	}
}

func TestStartAfterCloseCreatesFreshFacade(t *testing.T) {
	p := testPaths(t)
	startTest(t, p, nil, newTestListener())
	if err := Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	l := newTestListener()
	startTest(t, p, nil, l)
	out := mustCommand(t, "status", nil)
	if env := decodeEnvelope(t, out); env.Type != "status" {
		t.Fatalf("type = %q, want status", env.Type)
	}
	status := waitOn(t, l.statusCh, "a status callback from the fresh facade")
	if status == "" {
		t.Fatal("empty status")
	}
}

func TestCommandBeforeStart(t *testing.T) {
	if err := Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	out, err := Command(`{"type":"status"}`)
	if err == nil {
		t.Fatal("Command before Start did not fail")
	}
	if out != "" {
		t.Fatalf("Command before Start returned %q", out)
	}
	if !strings.Contains(err.Error(), "not started") {
		t.Fatalf("error = %v, want a not started error", err)
	}
}
