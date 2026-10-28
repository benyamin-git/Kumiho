package mobile

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/doctor"
	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/settings"
)

type testEnvelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func command(t *testing.T, typ string, payload any) (string, error) {
	t.Helper()
	req := map[string]any{"type": typ}
	if payload != nil {
		req["payload"] = payload
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return Command(string(raw))
}

func mustCommand(t *testing.T, typ string, payload any) string {
	t.Helper()
	out, err := command(t, typ, payload)
	if err != nil {
		t.Fatalf("%s: %v", typ, err)
	}
	return out
}

func decodeEnvelope(t *testing.T, raw string) testEnvelope {
	t.Helper()
	var env testEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, raw)
	}
	return env
}

func decodeTestPayload(t *testing.T, raw string, v any) {
	t.Helper()
	env := decodeEnvelope(t, raw)
	if len(env.Payload) == 0 {
		t.Fatalf("envelope %q has no payload: %s", env.Type, raw)
	}
	if err := json.Unmarshal(env.Payload, v); err != nil {
		t.Fatalf("decode %q payload: %v\n%s", env.Type, err, env.Payload)
	}
}

func requireErrorCode(t *testing.T, raw string, code string) {
	t.Helper()
	var env struct {
		Error *api.RemoteError `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("decode error envelope: %v\n%s", err, raw)
	}
	if env.Error == nil {
		t.Fatalf("expected an error envelope, got %s", raw)
	}
	if env.Error.Code != code {
		t.Fatalf("code = %q, want %q (%s)", env.Error.Code, code, env.Error.Message)
	}
}

func requireErrorMessage(t *testing.T, raw, want string) {
	t.Helper()
	var env struct {
		Error *api.RemoteError `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("decode error envelope: %v\n%s", err, raw)
	}
	if env.Error == nil {
		t.Fatalf("expected an error envelope, got %s", raw)
	}
	if env.Error.Message != want {
		t.Fatalf("message = %q, want %q", env.Error.Message, want)
	}
}

// browserAuthorizationState extracts the OAuth state from a login_start URL.
func browserAuthorizationState(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse browser URL %q: %v", raw, err)
	}
	state := u.Query().Get("state")
	if state == "" {
		t.Fatalf("browser URL has no state: %s", raw)
	}
	return state
}

func TestStatusCommandAndInitialStatus(t *testing.T) {
	p := testPaths(t)
	l := newTestListener()
	startTest(t, p, nil, l)

	initial := waitOn(t, l.statusCh, "the initial status push")
	var st api.Status
	if err := json.Unmarshal([]byte(initial), &st); err != nil {
		t.Fatalf("initial status is not api.Status: %v", err)
	}
	if st.State != "UNAUTHENTICATED" && st.State != "IDLE" {
		t.Fatalf("initial state = %q", st.State)
	}

	l.drainStatuses()
	out := mustCommand(t, "status", nil)
	env := decodeEnvelope(t, out)
	if env.Type != "status" {
		t.Fatalf("type = %q, want status", env.Type)
	}
	var got api.Status
	decodeTestPayload(t, out, &got)
	if got.State == "" || got.State != st.State {
		t.Fatalf("status state = %q, want %q", got.State, st.State)
	}
	if got.Authenticated {
		t.Fatal("a fresh facade reports authenticated")
	}
	pushed := waitOn(t, l.statusCh, "a status push after the status command")
	if !strings.Contains(pushed, got.State) {
		t.Fatalf("pushed status %s does not contain %q", pushed, got.State)
	}
}

func TestHelloCommand(t *testing.T) {
	startTest(t, testPaths(t), nil, newTestListener())
	out := mustCommand(t, "hello", nil)
	env := decodeEnvelope(t, out)
	if env.Type != "hello" {
		t.Fatalf("type = %q, want hello", env.Type)
	}
	var hello api.HelloResult
	decodeTestPayload(t, out, &hello)
	if hello.Protocol != 1 {
		t.Errorf("protocol = %d, want 1", hello.Protocol)
	}
	if hello.Version == "" || hello.OS == "" || hello.Arch == "" {
		t.Errorf("hello = %+v, want version/os/arch", hello)
	}
}

func TestSettingsGetCommand(t *testing.T) {
	startTest(t, testPaths(t), nil, newTestListener())
	out := mustCommand(t, "settings_get", nil)
	env := decodeEnvelope(t, out)
	if env.Type != "settings" {
		t.Fatalf("type = %q, want settings", env.Type)
	}
	var view api.SettingsView
	decodeTestPayload(t, out, &view)
	if view.MTU != 1500 {
		t.Errorf("mtu = %d, want the Android default 1500", view.MTU)
	}
	if view.SocksPort != 1080 {
		t.Errorf("socks_port = %d, want 1080", view.SocksPort)
	}
}

func TestSettingsSetCommand(t *testing.T) {
	startTest(t, testPaths(t), nil, newTestListener())
	out := mustCommand(t, "settings_set", map[string]any{"key": "mtu", "value": "1400"})
	if env := decodeEnvelope(t, out); env.Type != "result" {
		t.Fatalf("type = %q, want result", env.Type)
	}
	var view api.SettingsView
	decodeTestPayload(t, mustCommand(t, "settings_get", nil), &view)
	if view.MTU != 1400 {
		t.Fatalf("mtu = %d after settings_set, want 1400", view.MTU)
	}

	out = mustCommand(t, "settings_set", map[string]any{"key": "nope", "value": "1"})
	requireErrorCode(t, out, api.CodeUnsupportedSetting)
}

func TestSelectLocationCommand(t *testing.T) {
	p := testPaths(t)
	startTest(t, p, nil, newTestListener())
	out := mustCommand(t, "select_location", map[string]any{
		"country_code": "US", "city_code": "NYC", "server": "us-nyc.example:443",
	})
	if env := decodeEnvelope(t, out); env.Type != "result" {
		t.Fatalf("type = %q, want result", env.Type)
	}
	raw, err := os.ReadFile(p.State)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var state settings.State
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if state.SelectedLocation != "US" || state.SelectedCity != "NYC" || state.SelectedServer != "us-nyc.example:443" {
		t.Fatalf("persisted selection = %+v", state)
	}

	out = mustCommand(t, "select_location", map[string]any{"country_code": "", "server": ""})
	requireErrorCode(t, out, api.CodeBadRequest)
}

func TestUnknownCommandIsBadRequest(t *testing.T) {
	startTest(t, testPaths(t), nil, newTestListener())
	out, err := command(t, "no_such_command", nil)
	if err != nil {
		t.Fatalf("unknown type returned a Go error: %v", err)
	}
	requireErrorCode(t, out, api.CodeBadRequest)
}

func TestMalformedRequestIsAGoError(t *testing.T) {
	startTest(t, testPaths(t), nil, newTestListener())
	for _, raw := range []string{"", "{", "[]"} {
		out, err := Command(raw)
		if err == nil {
			t.Fatalf("Command(%q) succeeded: %s", raw, out)
		}
		if out != "" {
			t.Fatalf("Command(%q) returned %q with an error", raw, out)
		}
	}
	out, err := Command(`{"payload":{}}`)
	if err == nil {
		t.Fatalf("a request without a type succeeded: %s", out)
	}
}

func TestMalformedPayloadIsBadRequest(t *testing.T) {
	startTest(t, testPaths(t), nil, newTestListener())
	out, err := Command(`{"type":"select_location","payload":"nope"}`)
	if err != nil {
		t.Fatalf("malformed payload returned a Go error: %v", err)
	}
	requireErrorCode(t, out, api.CodeBadRequest)
}

func TestConnectAndDisconnectErrors(t *testing.T) {
	startTest(t, testPaths(t), nil, newTestListener())

	out := mustCommand(t, "connect", map[string]any{"proxy_only": true})
	requireErrorCode(t, out, api.CodeNotAuthenticated)

	out = mustCommand(t, "connect", map[string]any{})
	requireErrorCode(t, out, api.CodeBadRequest)

	out = mustCommand(t, "disconnect", nil)
	requireErrorCode(t, out, api.CodeBadRequest)
}

func TestRevokeCommand(t *testing.T) {
	startTest(t, testPaths(t), nil, newTestListener())
	out := mustCommand(t, "revoke", nil)
	if env := decodeEnvelope(t, out); env.Type != "result" {
		t.Fatalf("type = %q, want result", env.Type)
	}
}

func TestPingCommandAcksImmediately(t *testing.T) {
	startTest(t, testPaths(t), nil, newTestListener())
	out := mustCommand(t, "ping", map[string]any{"hosts": []string{}})
	var result api.Result
	decodeTestPayload(t, out, &result)
	if !result.OK {
		t.Fatal("ping ack is not ok")
	}

	hosts := make([]string, 513)
	for i := range hosts {
		hosts[i] = "host:443"
	}
	out = mustCommand(t, "ping", map[string]any{"hosts": hosts})
	requireErrorCode(t, out, api.CodeBadRequest)
}

func TestLogAppendSnapshotAndClear(t *testing.T) {
	p := testPaths(t)
	startTest(t, p, nil, newTestListener())

	mustCommand(t, "log_append", map[string]any{"level": "warn", "tag": "test", "msg": "hello log"})
	out := mustCommand(t, "logs_snapshot", map[string]any{"level": "debug"})
	var snap api.LogSnapshot
	decodeTestPayload(t, out, &snap)
	found := false
	for _, e := range snap.Entries {
		if e.Msg == "hello log" && e.Tag == "test" && e.Level == logging.Warn {
			found = true
		}
	}
	if !found {
		t.Fatalf("snapshot lacks the appended entry: %+v", snap.Entries)
	}

	// The entry reached the rotating file sink.
	logPath := filepath.Join(p.Logs, "kumiho.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(data), "hello log") {
		t.Fatalf("log file lacks the entry:\n%s", data)
	}

	// Level filtering drops debug entries above the asked level.
	mustCommand(t, "log_append", map[string]any{"level": "debug", "tag": "test", "msg": "hidden"})
	out = mustCommand(t, "logs_snapshot", map[string]any{"level": "warn"})
	snap = api.LogSnapshot{}
	decodeTestPayload(t, out, &snap)
	for _, e := range snap.Entries {
		if e.Msg == "hidden" {
			t.Fatal("logs_snapshot with level=warn returned a debug entry")
		}
	}

	out = mustCommand(t, "logs_clear", nil)
	if env := decodeEnvelope(t, out); env.Type != "result" {
		t.Fatalf("logs_clear type = %q, want result", env.Type)
	}
	snap = api.LogSnapshot{}
	decodeTestPayload(t, mustCommand(t, "logs_snapshot", nil), &snap)
	if len(snap.Entries) != 0 {
		t.Fatalf("snapshot after logs_clear = %+v", snap.Entries)
	}
	fi, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("stat log file: %v", err)
	}
	if fi.Size() != 0 {
		t.Fatalf("log file size after logs_clear = %d, want 0", fi.Size())
	}
}

func TestLogAppendRejectsInvalidLevel(t *testing.T) {
	startTest(t, testPaths(t), nil, newTestListener())
	out := mustCommand(t, "log_append", map[string]any{"level": "loud", "tag": "test", "msg": "x"})
	requireErrorCode(t, out, api.CodeBadRequest)
	out = mustCommand(t, "log_append", map[string]any{"level": "info", "tag": "test"})
	requireErrorCode(t, out, api.CodeBadRequest)
}

func TestLogsSubscribeAndUnsubscribe(t *testing.T) {
	l := newTestListener()
	startTest(t, testPaths(t), nil, l)

	if env := decodeEnvelope(t, mustCommand(t, "logs_subscribe", map[string]any{"level": "debug"})); env.Type != "result" {
		t.Fatalf("logs_subscribe type = %q", env.Type)
	}
	mustCommand(t, "log_append", map[string]any{"level": "info", "tag": "test", "msg": "streamed"})
	entry := l.waitLog(t)
	var e logging.Entry
	if err := json.Unmarshal([]byte(entry), &e); err != nil {
		t.Fatalf("OnLog is not a logging.Entry: %v", err)
	}
	if e.Msg != "streamed" || e.Tag != "test" {
		t.Fatalf("OnLog = %+v", e)
	}

	if env := decodeEnvelope(t, mustCommand(t, "logs_unsubscribe", nil)); env.Type != "result" {
		t.Fatalf("logs_unsubscribe type = %q", env.Type)
	}
	before := l.countLogs()
	mustCommand(t, "log_append", map[string]any{"level": "info", "tag": "test", "msg": "after unsubscribe"})
	time.Sleep(150 * time.Millisecond)
	if got := l.countLogs(); got != before {
		t.Fatalf("OnLog fired %d times after unsubscribe", got-before)
	}
}

func TestLogsSubscribeRejectsInvalidLevel(t *testing.T) {
	startTest(t, testPaths(t), nil, newTestListener())
	out := mustCommand(t, "logs_subscribe", map[string]any{"level": "loud"})
	requireErrorCode(t, out, api.CodeBadRequest)
}

func TestResetStateCommand(t *testing.T) {
	p := testPaths(t)
	if err := os.MkdirAll(filepath.Dir(p.State), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	state := `{"failures":{"a.example:443":3},"selected_location":"US","selected_city":"NYC","selected_server":"a.example:443","email":"user@example.com"}`
	if err := os.WriteFile(p.State, []byte(state), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	cache := filepath.Join(filepath.Dir(p.State), "serverlist.json")
	if err := os.WriteFile(cache, []byte(`{"stale":true}`), 0o600); err != nil {
		t.Fatalf("write cache: %v", err)
	}
	startTest(t, p, nil, newTestListener())

	mustCommand(t, "log_append", map[string]any{"level": "warn", "tag": "test", "msg": "reset me"})
	out := mustCommand(t, "reset_state", nil)
	if env := decodeEnvelope(t, out); env.Type != "result" {
		t.Fatalf("type = %q, want result", env.Type)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatalf("serverlist.json was not removed: %v", err)
	}

	raw, err := os.ReadFile(p.State)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var got settings.State
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if len(got.Failures) != 0 {
		t.Errorf("failures = %v, want cleared", got.Failures)
	}
	if got.SelectedLocation != "US" || got.SelectedServer != "a.example:443" || got.Email != "user@example.com" {
		t.Errorf("reset_state cleared the wrong fields: %+v", got)
	}
	var snap api.LogSnapshot
	decodeTestPayload(t, mustCommand(t, "logs_snapshot", nil), &snap)
	if len(snap.Entries) != 0 {
		t.Errorf("reset_state left %d log entries", len(snap.Entries))
	}
}

func TestDoctorCommand(t *testing.T) {
	stubDoctor(t)
	startTest(t, testPaths(t), nil, newTestListener())
	out := mustCommand(t, "doctor", map[string]any{"facts": map[string]any{
		"device":       "Pixel 9",
		"engine_state": "IDLE",
		"sdk":          34,
	}})
	env := decodeEnvelope(t, out)
	if env.Type != "doctor" {
		t.Fatalf("type = %q, want doctor", env.Type)
	}
	var report doctor.Report
	decodeTestPayload(t, out, &report)
	if report.Host != "Pixel 9" {
		t.Errorf("host = %q", report.Host)
	}
	for _, c := range report.Checks {
		if c.Name == "platform" || c.Name == "socks-port" {
			t.Errorf("doctor kept the %q check", c.Name)
		}
	}
	if len(report.Checks) == 0 {
		t.Fatal("doctor returned no checks")
	}
}

func TestPanicRecovery(t *testing.T) {
	l := newTestListener()
	startTest(t, testPaths(t), nil, l)

	mu.RLock()
	f := current
	mu.RUnlock()
	if f == nil {
		t.Fatal("no facade after Start")
	}
	resp, err := f.guard("command", func() (string, error) {
		panic("boom")
	})
	if err == nil {
		t.Fatalf("guard returned no error, resp = %q", resp)
	}
	var fe *facadeError
	if !errors.As(err, &fe) || fe.code != api.CodeInternal {
		t.Fatalf("guard error = %v, want an internal facade error", err)
	}
	if resp != "" {
		t.Fatalf("guard returned %q with an error", resp)
	}

	found := false
	for _, e := range f.ring.Snapshot() {
		if strings.Contains(e.Msg, "boom") {
			found = true
		}
	}
	if !found {
		t.Fatal("the panic was not logged")
	}

	// The recovered panic must leave a stack in the log: without it a nil
	// dereference on a device is undebuggable from the exported log.
	stacked := false
	for _, e := range f.ring.Snapshot() {
		if strings.Contains(e.Msg, "boom") && strings.Contains(e.Msg, "goroutine") {
			stacked = true
		}
	}
	if !stacked {
		t.Fatal("the panic log has no stack trace")
	}
}

func TestConcurrentCommandsAndClose(t *testing.T) {
	startTest(t, testPaths(t), nil, newTestListener())

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				out, err := Command(`{"type":"status"}`)
				if err != nil {
					if !strings.Contains(err.Error(), "not started") {
						t.Errorf("concurrent Command: %v", err)
					}
					return
				}
				if out == "" {
					t.Errorf("empty response")
					return
				}
			}
		}()
	}
	if err := Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	wg.Wait()
}

func TestLoginCompleteHasItsOwnLongBudget(t *testing.T) {
	if loginCompleteTimeout < 60*time.Second {
		t.Fatalf("loginCompleteTimeout = %v, want at least 60 s", loginCompleteTimeout)
	}
	if loginCompleteTimeout <= accountCommandTimeout {
		t.Fatalf("loginCompleteTimeout = %v, must exceed the %v local command budget", loginCompleteTimeout, accountCommandTimeout)
	}
}

func TestMobileAccountCommands(t *testing.T) {
	p := testPaths(t)
	if err := os.MkdirAll(filepath.Dir(p.Tokens), 0o700); err != nil {
		t.Fatalf("create no-backup dir: %v", err)
	}
	accounts := `{
  "version": 2,
  "active": "a1",
  "last_method": "browser",
  "accounts": [
    {
      "id": "a1",
      "email": "user@example.com",
      "label": "Home",
      "method": "browser",
      "fxa": {"access_token": "access-token", "refresh_token": "refresh-token", "expires_at": 9999999999},
      "added_at": "2026-10-23T12:00:00Z",
      "last_used_at": "2026-10-23T12:00:00Z"
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(filepath.Dir(p.Tokens), "accounts.json"), []byte(accounts), 0o600); err != nil {
		t.Fatalf("write accounts.json: %v", err)
	}
	startTest(t, p, nil, newTestListener())

	out := mustCommand(t, "accounts_list", nil)
	if env := decodeEnvelope(t, out); env.Type != "accounts_info" {
		t.Fatalf("accounts_list type = %q, want %q", env.Type, "accounts_info")
	}
	var view api.AccountsView
	decodeTestPayload(t, out, &view)
	if len(view.Accounts) != 1 || view.Active != "a1" || view.LastMethod != "browser" {
		t.Fatalf("accounts view = %+v", view)
	}

	out = mustCommand(t, "accounts_add_start", map[string]any{"method": "browser"})
	if env := decodeEnvelope(t, out); env.Type != "login_state" {
		t.Fatalf("accounts_add_start type = %q, want login_state", env.Type)
	}
	var add api.LoginState
	decodeTestPayload(t, out, &add)
	if add.Step != "browser" || add.LoginID == "" || add.URL == "" {
		t.Fatalf("accounts_add_start state = %+v", add)
	}

	// A complete redirect URL is routed to the engine. This one has no code,
	// so the engine rejects it before any network call and keeps the flow
	// alive for another paste.
	state := browserAuthorizationState(t, add.URL)
	redirect := fxa.OAuthRedirectURI + "?state=" + url.QueryEscape(state)
	out = mustCommand(t, "login_complete", map[string]any{
		"login_id": add.LoginID, "redirect_url": redirect,
	})
	requireErrorCode(t, out, api.CodeBadRequest)
	if !strings.Contains(out, "authorization code") {
		t.Fatalf("login_complete error = %s, want a missing-code message", out)
	}
	out = mustCommand(t, "accounts_add_status", map[string]any{"login_id": add.LoginID})
	if env := decodeEnvelope(t, out); env.Type != "login_state" {
		t.Fatalf("accounts_add_status type = %q, want login_state", env.Type)
	}
	var afterBadPaste api.LoginState
	decodeTestPayload(t, out, &afterBadPaste)
	if afterBadPaste.Step != "browser" {
		t.Fatalf("flow after a rejected redirect = %+v", afterBadPaste)
	}

	// The legacy code field is rejected with the stable full-URL message.
	out = mustCommand(t, "login_complete", map[string]any{
		"login_id": add.LoginID, "code": "legacy-code",
	})
	requireErrorCode(t, out, api.CodeBadRequest)
	requireErrorMessage(t, out, "paste the complete redirect URL from the browser")

	out = mustCommand(t, "accounts_add_cancel", map[string]any{"login_id": add.LoginID})
	if env := decodeEnvelope(t, out); env.Type != "result" {
		t.Fatalf("accounts_add_cancel type = %q, want result", env.Type)
	}

	out = mustCommand(t, "login_start", map[string]any{"method": "browser"})
	if env := decodeEnvelope(t, out); env.Type != "login_state" {
		t.Fatalf("login_start type = %q, want login_state", env.Type)
	}
	var login api.LoginState
	decodeTestPayload(t, out, &login)
	if login.Step != "browser" || login.LoginID == "" {
		t.Fatalf("login_start state = %+v", login)
	}
	out = mustCommand(t, "login_status", map[string]any{"login_id": login.LoginID})
	if env := decodeEnvelope(t, out); env.Type != "login_state" {
		t.Fatalf("login_status type = %q, want login_state", env.Type)
	}
	out = mustCommand(t, "login_complete", map[string]any{
		"login_id": login.LoginID, "code": "legacy-code",
	})
	requireErrorCode(t, out, api.CodeBadRequest)
	out = mustCommand(t, "login_cancel", map[string]any{"login_id": login.LoginID})
	if env := decodeEnvelope(t, out); env.Type != "result" {
		t.Fatalf("login_cancel type = %q, want result", env.Type)
	}

	for _, step := range []struct {
		typ     string
		payload any
	}{
		{"accounts_use", map[string]any{"id": "a1"}},
		{"accounts_update", map[string]any{"id": "a1", "label": "Work"}},
		{"accounts_set_rotation", map[string]any{"enabled": true, "rotation_threshold": 20}},
		{"accounts_remove", map[string]any{"id": "a1"}},
	} {
		out = mustCommand(t, step.typ, step.payload)
		if env := decodeEnvelope(t, out); env.Type != "result" {
			t.Fatalf("%s type = %q, want result", step.typ, env.Type)
		}
	}
}
