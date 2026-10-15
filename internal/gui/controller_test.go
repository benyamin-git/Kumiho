package gui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

// fakeDaemon is a hand-written Daemon for the controller tests: every method
// records its calls and returns canned data, each with a failure the test
// can inject.
type fakeDaemon struct {
	mu     sync.Mutex
	events chan *ipc.Envelope
	calls  []string

	status      api.Status
	statusErr   error
	statusCalls int

	connectErr error
	connects   []api.ConnectPayload

	disconnectErr error

	selectErr error
	selects   []api.SelectLocationPayload

	pingErr error
	pings   [][]string

	settings      api.SettingsView
	settingsErr   error
	settingsCalls int
	setErr        error
	sets          []api.SettingsSetPayload

	account      api.AccountInfo
	accountErr   error
	accountCalls int

	locations     api.LocationsResult
	locationsErr  error
	locationCalls int

	logSnapshot    api.LogSnapshot
	logSnapshotErr error
	subscribeErr   error
	unsubscribeErr error
	clearErr       error
	subscribes     []string
	unsubscribes   int
	clears         int

	loginReplies map[string]api.LoginState
	loginErr     error
	loginCalls   []string

	logoutErr error
	logouts   int
}

func newFakeDaemon() *fakeDaemon {
	return &fakeDaemon{
		events:       make(chan *ipc.Envelope, 64),
		status:       api.Status{State: "IDLE", KillSwitch: true, Autoconnect: true},
		loginReplies: make(map[string]api.LoginState),
	}
}

// set mutates the fake under its lock (tests racing the controller's pump).
func (f *fakeDaemon) set(mutate func(*fakeDaemon)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	mutate(f)
}

// emit pushes one daemon event onto the fake's event channel.
func (f *fakeDaemon) emit(t *testing.T, typ string, payload any) {
	t.Helper()
	env, err := ipc.NewEnvelope("", typ, payload)
	if err != nil {
		t.Fatalf("build %s event: %v", typ, err)
	}
	f.events <- env
}

func (f *fakeDaemon) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeDaemon) connectPayloads() []api.ConnectPayload {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]api.ConnectPayload(nil), f.connects...)
}

func (f *fakeDaemon) setPayloads() []api.SettingsSetPayload {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]api.SettingsSetPayload(nil), f.sets...)
}

func (f *fakeDaemon) selectPayloads() []api.SelectLocationPayload {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]api.SelectLocationPayload(nil), f.selects...)
}

func (f *fakeDaemon) pingHosts() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]string, len(f.pings))
	copy(out, f.pings)
	return out
}

func (f *fakeDaemon) subscribeLevels() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.subscribes...)
}

func (f *fakeDaemon) count(field func(*fakeDaemon) int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return field(f)
}

func (f *fakeDaemon) Events() <-chan *ipc.Envelope { return f.events }

func (f *fakeDaemon) Status(context.Context) (api.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "status")
	f.statusCalls++
	return f.status, f.statusErr
}

func (f *fakeDaemon) Connect(_ context.Context, p api.ConnectPayload) (api.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "connect")
	f.connects = append(f.connects, p)
	return api.Result{OK: true}, f.connectErr
}

func (f *fakeDaemon) Disconnect(context.Context) (api.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "disconnect")
	return api.Result{OK: true}, f.disconnectErr
}

func (f *fakeDaemon) SelectLocation(_ context.Context, p api.SelectLocationPayload) (api.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "select_location")
	f.selects = append(f.selects, p)
	return api.Result{OK: true}, f.selectErr
}

func (f *fakeDaemon) Locations(context.Context, bool) (api.LocationsResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "locations")
	f.locationCalls++
	return f.locations, f.locationsErr
}

func (f *fakeDaemon) Ping(_ context.Context, hosts []string) (api.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "ping")
	f.pings = append(f.pings, append([]string(nil), hosts...))
	return api.Result{OK: true}, f.pingErr
}

func (f *fakeDaemon) Settings(context.Context) (api.SettingsView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "settings")
	f.settingsCalls++
	return f.settings, f.settingsErr
}

func (f *fakeDaemon) SetSetting(_ context.Context, p api.SettingsSetPayload) (api.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "set_setting")
	f.sets = append(f.sets, p)
	return api.Result{OK: true}, f.setErr
}

func (f *fakeDaemon) Account(context.Context) (api.AccountInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "account")
	f.accountCalls++
	return f.account, f.accountErr
}

func (f *fakeDaemon) LogsSnapshot(_ context.Context, level string) (api.LogSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "logs_snapshot")
	return f.logSnapshot, f.logSnapshotErr
}

func (f *fakeDaemon) LogsSubscribe(_ context.Context, level string) (api.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "logs_subscribe")
	f.subscribes = append(f.subscribes, level)
	return api.Result{OK: true}, f.subscribeErr
}

func (f *fakeDaemon) LogsUnsubscribe(context.Context) (api.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "logs_unsubscribe")
	f.unsubscribes++
	return api.Result{OK: true}, f.unsubscribeErr
}

func (f *fakeDaemon) LogsClear(context.Context) (api.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "logs_clear")
	f.clears++
	return api.Result{OK: true}, f.clearErr
}

func (f *fakeDaemon) LoginEmail(_ context.Context, p api.LoginEmailPayload) (api.LoginState, error) {
	return f.login("login_email", p.Email)
}

func (f *fakeDaemon) LoginPassword(_ context.Context, p api.LoginPasswordPayload) (api.LoginState, error) {
	return f.login("login_password", p.Password)
}

func (f *fakeDaemon) Login2FA(_ context.Context, p api.Login2FAPayload) (api.LoginState, error) {
	return f.login("login_2fa", p.Code)
}

func (f *fakeDaemon) login(method, value string) (api.LoginState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method)
	f.loginCalls = append(f.loginCalls, value)
	return f.loginReplies[method], f.loginErr
}

func (f *fakeDaemon) Logout(context.Context) (api.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "logout")
	f.logouts++
	return api.Result{OK: true}, f.logoutErr
}

// quietOptions keeps the periodic safety refreshes out of the way while
// leaving the UI pump cadence and the startup loads active.
func quietOptions() Options {
	return Options{
		PumpInterval:     time.Millisecond,
		StatusInterval:   time.Hour,
		SettingsInterval: time.Hour,
	}
}

// startPoll runs the controller's pump until the test ends.
func startPoll(t *testing.T, c *Controller) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Poll(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestDispatchConnectPayloadPassthrough(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, Options{})

	payload := []byte(`{"location_code":"DE","city_code":"ber","server":"de1.example:443","proxy_only":true,"kill_switch":"on"}`)
	if err := ctrl.Dispatch(Action{Name: ActionConnect, Payload: payload}); err != nil {
		t.Fatalf("Dispatch(connect) = %v", err)
	}

	// The CLI's settings-then-connect order: kill_switch first, then the
	// connect payload verbatim, then the status refresh.
	if got, want := f.callLog(), []string{"set_setting", "connect", "status"}; !slices.Equal(got, want) {
		t.Fatalf("call order = %v, want %v", got, want)
	}
	wantSet := api.SettingsSetPayload{Key: "kill_switch", Value: "on"}
	if got := f.setPayloads(); len(got) != 1 || got[0] != wantSet {
		t.Fatalf("settings_set payloads = %+v, want [%+v]", got, wantSet)
	}
	wantConnect := api.ConnectPayload{LocationCode: "DE", CityCode: "ber", Server: "de1.example:443", ProxyOnly: true}
	if got := f.connectPayloads(); len(got) != 1 || got[0] != wantConnect {
		t.Fatalf("connect payloads = %+v, want [%+v]", got, wantConnect)
	}
	if n := ctrl.Snapshot().Notice; n == nil || n.Message != "Connected" || n.Tone != toneInfo {
		t.Fatalf("notice = %+v, want Connected/info", n)
	}
}

func TestDispatchConnectKillSwitchFailureAborts(t *testing.T) {
	f := newFakeDaemon()
	f.setErr = errors.New("kill switch refused")
	ctrl := NewController(f, Options{})

	err := ctrl.Dispatch(Action{Name: ActionConnect, Payload: []byte(`{"kill_switch":"on","proxy_only":true}`)})
	if !errors.Is(err, f.setErr) {
		t.Fatalf("Dispatch(connect) = %v, want the settings error", err)
	}
	if got := f.connectPayloads(); len(got) != 0 {
		t.Fatalf("connect called after the kill-switch failure: %+v", got)
	}
	if n := ctrl.Snapshot().Notice; n == nil || n.Message != "could not connect: kill switch refused" || n.Tone != toneBad {
		t.Fatalf("notice = %+v", n)
	}
}

func TestDispatchConnectErrorPropagation(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) { d.connectErr = errors.New("bad_request: no pass") })
	ctrl := NewController(f, Options{})

	err := ctrl.Dispatch(Action{Name: ActionConnect, Payload: []byte(`{"proxy_only":true}`)})
	if !errors.Is(err, f.connectErr) {
		t.Fatalf("Dispatch(connect) = %v", err)
	}
	if n := ctrl.Snapshot().Notice; n == nil || n.Message != "could not connect: bad_request: no pass" || n.Tone != toneBad {
		t.Fatalf("notice = %+v", n)
	}
	if !ctrl.Snapshot().Reachable {
		t.Fatal("a daemon-level error must not flip the unreachable gate")
	}
}

func TestDispatchDisconnect(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, Options{})

	if err := ctrl.Dispatch(Action{Name: ActionDisconnect}); err != nil {
		t.Fatalf("Dispatch(disconnect) = %v", err)
	}
	if n := ctrl.Snapshot().Notice; n == nil || n.Message != "Disconnected" {
		t.Fatalf("notice = %+v", n)
	}
	if got, want := f.callLog(), []string{"disconnect", "status"}; !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}

	f.set(func(d *fakeDaemon) { d.disconnectErr = errors.New("not connected") })
	if err := ctrl.Dispatch(Action{Name: ActionDisconnect}); err == nil {
		t.Fatal("Dispatch(disconnect) = nil, want error")
	}
	if n := ctrl.Snapshot().Notice; n == nil || n.Message != "could not disconnect: not connected" || n.Tone != toneBad {
		t.Fatalf("notice = %+v", n)
	}
}

func TestDispatchSelectLocation(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, Options{})

	payload := []byte(`{"country_code":"DE","city_code":"ber","server":"de1.example:443"}`)
	if err := ctrl.Dispatch(Action{Name: ActionSelectLocation, Payload: payload}); err != nil {
		t.Fatalf("Dispatch(select_location) = %v", err)
	}
	want := api.SelectLocationPayload{CountryCode: "DE", CityCode: "ber", Server: "de1.example:443"}
	if got := f.selectPayloads(); len(got) != 1 || got[0] != want {
		t.Fatalf("select payloads = %+v, want [%+v]", got, want)
	}
	if n := ctrl.Snapshot().Notice; n == nil || n.Message != "Location saved" {
		t.Fatalf("notice = %+v", n)
	}
	if got, want := f.callLog(), []string{"select_location", "status"}; !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}

	f.set(func(d *fakeDaemon) { d.selectErr = errors.New("bad_request") })
	if err := ctrl.Dispatch(Action{Name: ActionSelectLocation, Payload: payload}); err == nil {
		t.Fatal("Dispatch(select_location) = nil, want error")
	}
	if n := ctrl.Snapshot().Notice; n == nil || n.Message != "could not save the location: bad_request" {
		t.Fatalf("notice = %+v", n)
	}
}

func TestDispatchPingAndResults(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, quietOptions())
	startPoll(t, ctrl)

	if err := ctrl.Dispatch(Action{Name: ActionPing, Payload: []byte(`{"hosts":["a:1","b:2"]}`)}); err != nil {
		t.Fatalf("Dispatch(ping) = %v", err)
	}
	if got := f.pingHosts(); len(got) != 1 || !slices.Equal(got[0], []string{"a:1", "b:2"}) {
		t.Fatalf("ping hosts = %v", got)
	}

	rtt := 42.4
	f.emit(t, ipc.TypePingResult, api.PingResult{Host: "a:1", RTTMS: &rtt})
	f.emit(t, ipc.TypePingResult, api.PingResult{Host: "b:2"})
	waitFor(t, "ping results", func() bool {
		rtts := ctrl.Snapshot().RTTs
		return rtts["a:1"] == "42 ms" && rtts["b:2"] == "timeout"
	})

	// A failed run reports "Speed test failed" until the next successful run.
	f.set(func(d *fakeDaemon) { d.pingErr = errors.New("no route") })
	if err := ctrl.Dispatch(Action{Name: ActionPing, Payload: []byte(`{"hosts":["a:1"]}`)}); err == nil {
		t.Fatal("Dispatch(ping) = nil, want error")
	}
	if n := ctrl.Snapshot().LocationsNotice; n == nil || n.Message != "Speed test failed: no route" || n.Tone != toneBad {
		t.Fatalf("locations notice = %+v", n)
	}
	f.set(func(d *fakeDaemon) { d.pingErr = nil })
	if err := ctrl.Dispatch(Action{Name: ActionPing, Payload: []byte(`{"hosts":["a:1"]}`)}); err != nil {
		t.Fatalf("Dispatch(ping) = %v", err)
	}
	if n := ctrl.Snapshot().LocationsNotice; n != nil {
		t.Fatalf("locations notice = %+v, want cleared by the next run", n)
	}
}

func TestDispatchSettingsGetAndSet(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.settings = api.SettingsView{MTU: 1400, KillSwitchEffective: true, Overridden: []string{"mtu"}}
	})
	ctrl := NewController(f, Options{})

	if err := ctrl.Dispatch(Action{Name: ActionSettingsGet}); err != nil {
		t.Fatalf("Dispatch(settings_get) = %v", err)
	}
	if s := ctrl.Snapshot().Settings; s == nil || s.MTU != 1400 || !slices.Equal(s.Overridden, []string{"mtu"}) {
		t.Fatalf("settings = %+v", s)
	}

	if err := ctrl.Dispatch(Action{Name: ActionSettingsSet, Payload: []byte(`{"key":"mtu","value":"1500"}`)}); err != nil {
		t.Fatalf("Dispatch(settings_set) = %v", err)
	}
	want := api.SettingsSetPayload{Key: "mtu", Value: "1500"}
	if got := f.setPayloads(); len(got) != 1 || got[0] != want {
		t.Fatalf("settings_set payloads = %+v", got)
	}
	if n := ctrl.Snapshot().SettingsNotice; n == nil || n.Message != "MTU saved" || n.Tone != toneGood || n.Expires == nil {
		t.Fatalf("settings notice = %+v, want a 10 s MTU saved notice", n)
	}
	// Mutation success refreshes the status and reloads the settings.
	if got, want := f.callLog(), []string{"settings", "set_setting", "status", "settings"}; !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}

	f.set(func(d *fakeDaemon) { d.setErr = errors.New("bad_request: mtu") })
	if err := ctrl.Dispatch(Action{Name: ActionSettingsSet, Payload: []byte(`{"key":"mtu","value":"99"}`)}); err == nil {
		t.Fatal("Dispatch(settings_set) = nil, want error")
	}
	if n := ctrl.Snapshot().SettingsNotice; n == nil || n.Message != "could not change the setting: bad_request: mtu" || n.Tone != toneBad || n.Expires != nil {
		t.Fatalf("settings notice = %+v, want a persistent error notice", n)
	}
}

func TestDispatchSettingsSetResetNotice(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, Options{})
	if err := ctrl.Dispatch(Action{Name: ActionSettingsSet, Payload: []byte(`{"key":"mtu","reset":true}`)}); err != nil {
		t.Fatalf("Dispatch(settings_set) = %v", err)
	}
	if n := ctrl.Snapshot().SettingsNotice; n == nil || n.Message != "MTU reset to the configured value" {
		t.Fatalf("settings notice = %+v", n)
	}
}

func TestDispatchSettingsLoadFailure(t *testing.T) {
	f := newFakeDaemon()
	f.settingsErr = errors.New("boom")
	ctrl := NewController(f, Options{})
	if err := ctrl.Dispatch(Action{Name: ActionSettingsGet}); err == nil {
		t.Fatal("Dispatch(settings_get) = nil, want error")
	}
	if n := ctrl.Snapshot().SettingsNotice; n == nil || n.Message != "could not load the settings: boom" || n.Tone != toneBad {
		t.Fatalf("settings notice = %+v", n)
	}
}

func TestNoticeLifetimes(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, Options{NoticeTTL: 15 * time.Millisecond})

	// Screen-local successes expire after the configured TTL.
	if err := ctrl.Dispatch(Action{Name: ActionSettingsSet, Payload: []byte(`{"key":"mtu","value":"1400"}`)}); err != nil {
		t.Fatalf("Dispatch(settings_set) = %v", err)
	}
	if n := ctrl.Snapshot().SettingsNotice; n == nil {
		t.Fatal("settings notice vanished immediately")
	}
	waitFor(t, "settings notice expiry", func() bool { return ctrl.Snapshot().SettingsNotice == nil })

	// Screen-local errors persist until a later action replaces them.
	f.set(func(d *fakeDaemon) { d.setErr = errors.New("nope") })
	if err := ctrl.Dispatch(Action{Name: ActionSettingsSet, Payload: []byte(`{"key":"mtu","value":"1"}`)}); err == nil {
		t.Fatal("Dispatch(settings_set) = nil, want error")
	}
	time.Sleep(50 * time.Millisecond)
	if n := ctrl.Snapshot().SettingsNotice; n == nil || n.Tone != toneBad {
		t.Fatalf("error notice = %+v, want it to persist", n)
	}

	// The shared notice persists until another action replaces it.
	if err := ctrl.Dispatch(Action{Name: ActionConnect, Payload: []byte(`{}`)}); err != nil {
		t.Fatalf("Dispatch(connect) = %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if n := ctrl.Snapshot().Notice; n == nil || n.Message != "Connected" {
		t.Fatalf("shared notice = %+v, want it to persist past the TTL", n)
	}
}

func TestDispatchLoginFlow(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.loginReplies["login_email"] = api.LoginState{Step: "password", Email: "user@example.com"}
		d.loginReplies["login_password"] = api.LoginState{Step: "done", Email: "user@example.com"}
		d.account = api.AccountInfo{Email: "user@example.com", Subscribed: true}
	})
	ctrl := NewController(f, Options{})

	if err := ctrl.Dispatch(Action{Name: ActionLoginEmail, Payload: []byte(`{"email":"user@example.com"}`)}); err != nil {
		t.Fatalf("Dispatch(login_email) = %v", err)
	}
	if l := ctrl.Snapshot().Login; l == nil || l.Step != "password" || l.Email != "user@example.com" {
		t.Fatalf("login = %+v", l)
	}

	f.set(func(d *fakeDaemon) {
		d.status = api.Status{State: "IDLE", Authenticated: true, Email: "user@example.com"}
	})
	if err := ctrl.Dispatch(Action{Name: ActionLoginPassword, Payload: []byte(`{"password":"hunter2"}`)}); err != nil {
		t.Fatalf("Dispatch(login_password) = %v", err)
	}
	st := ctrl.Snapshot()
	if l := st.Login; l == nil || l.Step != "done" || l.Error != "" {
		t.Fatalf("login = %+v", l)
	}
	if st.Notice == nil || st.Notice.Message != "Signed in as user@example.com" {
		t.Fatalf("notice = %+v", st.Notice)
	}
	waitFor(t, "account reload after login", func() bool {
		s := ctrl.Snapshot()
		return s.Account != nil && s.Account.Email == "user@example.com"
	})
}

func TestDispatchLoginTransportError(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) { d.loginErr = errors.New("pipe broke") })
	ctrl := NewController(f, Options{})

	err := ctrl.Dispatch(Action{Name: ActionLoginPassword, Payload: []byte(`{"password":"x"}`)})
	if !errors.Is(err, f.loginErr) {
		t.Fatalf("Dispatch(login_password) = %v", err)
	}
	if l := ctrl.Snapshot().Login; l == nil || l.Error != "pipe broke" {
		t.Fatalf("login = %+v", l)
	}
	if n := ctrl.Snapshot().Notice; n == nil || n.Message != "sign-in failed: pipe broke" || n.Tone != toneBad {
		t.Fatalf("notice = %+v", n)
	}
}

func TestDispatchLoginAlreadySignedInRecovers(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.status = api.Status{State: "IDLE", Authenticated: true, Email: "user@example.com"}
		d.loginReplies["login_email"] = api.LoginState{Step: "done", Error: "already signed in; log out first to switch accounts"}
	})
	ctrl := NewController(f, Options{})

	if err := ctrl.Dispatch(Action{Name: ActionLoginEmail, Payload: []byte(`{"email":"other@example.com"}`)}); err != nil {
		t.Fatalf("Dispatch(login_email) = %v", err)
	}
	// The terminal reply refreshes the status; the authenticated snapshot
	// clears the stale refusal so the wizard shows the live session.
	l := ctrl.Snapshot().Login
	if l == nil || l.Step != "done" || l.Error != "" {
		t.Fatalf("login = %+v, want the refusal cleared by the fresh status", l)
	}
	waitFor(t, "account load after the terminal reply", func() bool {
		return slices.Contains(f.callLog(), "account")
	})
}

func TestDispatchLogout(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.account = api.AccountInfo{Email: "user@example.com", Subscribed: true}
		d.status = api.Status{State: "IDLE", Authenticated: true, Email: "user@example.com"}
	})
	ctrl := NewController(f, Options{})

	if err := ctrl.Dispatch(Action{Name: ActionLogout}); err != nil {
		t.Fatalf("Dispatch(logout) = %v", err)
	}
	st := ctrl.Snapshot()
	if n := st.AccountNotice; n == nil || n.Message != "Signed out" || n.Tone != toneGood || n.Expires == nil {
		t.Fatalf("account notice = %+v", n)
	}
	// Sign-out reloads the status and the account snapshot.
	waitFor(t, "account reload after logout", func() bool {
		return f.count(func(d *fakeDaemon) int { return d.accountCalls }) >= 1
	})

	f.set(func(d *fakeDaemon) { d.logoutErr = errors.New("internal") })
	if err := ctrl.Dispatch(Action{Name: ActionLogout}); err == nil {
		t.Fatal("Dispatch(logout) = nil, want error")
	}
	if n := ctrl.Snapshot().AccountNotice; n == nil || n.Message != "could not sign out: internal" || n.Tone != toneBad {
		t.Fatalf("account notice = %+v", n)
	}
}

func TestDispatchLogsLifecycle(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.logSnapshot = api.LogSnapshot{Entries: []logging.Entry{{Level: logging.Info, Tag: "daemon", Msg: "boot"}}}
	})
	ctrl := NewController(f, quietOptions())
	startPoll(t, ctrl)

	if err := ctrl.Dispatch(Action{Name: ActionLogsSnapshot, Payload: []byte(`{"level":"info"}`)}); err != nil {
		t.Fatalf("Dispatch(logs_snapshot) = %v", err)
	}
	if got := ctrl.Snapshot().Logs; len(got) != 1 || got[0].Msg != "boot" {
		t.Fatalf("logs = %+v", got)
	}

	if err := ctrl.Dispatch(Action{Name: ActionLogsSubscribe, Payload: []byte(`{"level":"info"}`)}); err != nil {
		t.Fatalf("Dispatch(logs_subscribe) = %v", err)
	}
	if got := f.subscribeLevels(); len(got) != 1 || got[0] != "info" {
		t.Fatalf("subscribe levels = %v", got)
	}

	f.emit(t, ipc.TypeLog, logging.Entry{Level: logging.Warn, Tag: "edge", Msg: "retry"})
	waitFor(t, "live log entry", func() bool { return len(ctrl.Snapshot().Logs) == 2 })

	if err := ctrl.Dispatch(Action{Name: ActionLogsClear}); err != nil {
		t.Fatalf("Dispatch(logs_clear) = %v", err)
	}
	st := ctrl.Snapshot()
	if len(st.Logs) != 0 {
		t.Fatalf("logs = %+v after clear", st.Logs)
	}
	if n := st.LogsNotice; n == nil || n.Message != "Logs cleared" || n.Tone != toneGood || n.Expires == nil {
		t.Fatalf("logs notice = %+v", n)
	}

	// Leaving the screen unsubscribes; events that race in afterwards are
	// not appended.
	if err := ctrl.Dispatch(Action{Name: ActionLogsUnsubscribe}); err != nil {
		t.Fatalf("Dispatch(logs_unsubscribe) = %v", err)
	}
	f.emit(t, ipc.TypeLog, logging.Entry{Level: logging.Info, Tag: "daemon", Msg: "late"})
	time.Sleep(20 * time.Millisecond)
	if got := ctrl.Snapshot().Logs; len(got) != 0 {
		t.Fatalf("logs = %+v, want the unsubscribed event dropped", got)
	}
}

func TestDispatchLogsFailures(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.logSnapshotErr = errors.New("ring gone")
		d.subscribeErr = errors.New("nope")
		d.clearErr = errors.New("readonly")
	})
	ctrl := NewController(f, Options{})

	if err := ctrl.Dispatch(Action{Name: ActionLogsSnapshot}); err == nil {
		t.Fatal("Dispatch(logs_snapshot) = nil, want error")
	}
	if n := ctrl.Snapshot().LogsNotice; n == nil || n.Message != "could not load the logs: ring gone" {
		t.Fatalf("logs notice = %+v", n)
	}
	if err := ctrl.Dispatch(Action{Name: ActionLogsSubscribe}); err == nil {
		t.Fatal("Dispatch(logs_subscribe) = nil, want error")
	}
	if n := ctrl.Snapshot().LogsNotice; n == nil || n.Message != "could not subscribe to the logs: nope" {
		t.Fatalf("logs notice = %+v", n)
	}
	if err := ctrl.Dispatch(Action{Name: ActionLogsClear}); err == nil {
		t.Fatal("Dispatch(logs_clear) = nil, want error")
	}
	if n := ctrl.Snapshot().LogsNotice; n == nil || n.Message != "could not clear the logs: readonly" {
		t.Fatalf("logs notice = %+v", n)
	}

	// A working snapshot heals an earlier error notice.
	f.set(func(d *fakeDaemon) { d.logSnapshotErr = nil })
	if err := ctrl.Dispatch(Action{Name: ActionLogsSnapshot}); err != nil {
		t.Fatalf("Dispatch(logs_snapshot) = %v", err)
	}
	if n := ctrl.Snapshot().LogsNotice; n != nil {
		t.Fatalf("logs notice = %+v, want it healed by the snapshot", n)
	}
}

func TestDispatchUnsupportedAndNoOpActions(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, Options{})

	for _, name := range []string{
		ActionLogsExport, ActionOpenLogsFolder, ActionCleanup,
		ActionServiceStart, ActionServiceStop, ActionUpdateCheck,
		ActionPrefsSet, ActionDoctorRun,
	} {
		err := ctrl.Dispatch(Action{Name: name})
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("unsupported action %q", name)) {
			t.Fatalf("Dispatch(%s) = %v, want an unsupported action error", name, err)
		}
	}
	for _, name := range []string{ActionWindowShow, ActionQuit, ActionOpenExternal} {
		if err := ctrl.Dispatch(Action{Name: name}); err != nil {
			t.Fatalf("Dispatch(%s) = %v, want a no-op", name, err)
		}
	}
	if got := f.callLog(); len(got) != 0 {
		t.Fatalf("no-op/unsupported actions called the daemon: %v", got)
	}
}

func TestDispatchPayloadDecodeError(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, Options{})
	err := ctrl.Dispatch(Action{Name: ActionConnect, Payload: []byte(`{"proxy_only":"yes"}`)})
	if err == nil || !strings.Contains(err.Error(), "decode connect payload") {
		t.Fatalf("Dispatch(connect) = %v, want a decode error", err)
	}
	if err := ctrl.Dispatch(Action{Name: ActionSettingsSet, Payload: []byte(`{"key":"mtu","bogus":1}`)}); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Dispatch(settings_set) = %v, want an unknown-field error", err)
	}
}

func TestPollProjectsStatusEvents(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, quietOptions())
	startPoll(t, ctrl)
	waitFor(t, "first status load", func() bool { return ctrl.Snapshot().Status != nil })

	f.emit(t, ipc.TypeStatus, api.Status{State: "RECONNECTING"})
	waitFor(t, "reconnecting", func() bool {
		s := ctrl.Snapshot().Status
		return s != nil && s.Label == "Reconnecting"
	})

	limit, remaining, reset := int64(50<<30), int64(4<<30), time.Now().Add(72*time.Hour).Unix()
	f.emit(t, ipc.TypeStatus, api.Status{
		State:         "CONNECTED",
		Authenticated: true,
		Email:         "user@example.com",
		Since:         time.Now().Add(-2*time.Minute - 500*time.Millisecond).Unix(),
		Location:      "DE",
		Server:        "de1.example:443",
		Quota:         &api.Quota{Limit: &limit, Remaining: &remaining, Reset: &reset},
		Totals:        &api.ByteCounters{Up: 12 << 20, Down: 80 << 20},
		Rates:         &api.ByteRates{Up: 1536, Down: 3072},
	})
	waitFor(t, "connected projection", func() bool {
		s := ctrl.Snapshot().Status
		return s != nil && s.Label == "Connected" && s.Connected
	})
	s := ctrl.Snapshot().Status
	if !strings.HasPrefix(s.Uptime, "2m") {
		t.Fatalf("uptime = %q, want a 2m duration", s.Uptime)
	}
	if s.QuotaText != "4.0 GiB remaining of 50.0 GiB" {
		t.Fatalf("quota = %q", s.QuotaText)
	}
	if s.Rates != "up 1.5 KiB/s / down 3.0 KiB/s" {
		t.Fatalf("rates = %q", s.Rates)
	}
	if s.Totals != "up 12.0 MiB / down 80.0 MiB" {
		t.Fatalf("totals = %q", s.Totals)
	}
	if s.Location != "DE" || s.Server != "de1.example:443" || s.Email != "user@example.com" {
		t.Fatalf("status = %+v", s)
	}
}

func TestPollUnreachableAndRecovery(t *testing.T) {
	f := newFakeDaemon()
	f.statusErr = fmt.Errorf("%w: dial failed", ErrDaemonUnreachable)
	ctrl := NewController(f, Options{
		PumpInterval:     time.Millisecond,
		StatusInterval:   2 * time.Millisecond,
		SettingsInterval: time.Hour,
	})
	startPoll(t, ctrl)

	waitFor(t, "unreachable gate", func() bool { return !ctrl.Snapshot().Reachable })

	// The daemon comes back; the safety tick re-dials and clears the gate.
	f.set(func(d *fakeDaemon) {
		d.statusErr = nil
		d.status = api.Status{State: "IDLE", KillSwitch: true}
	})
	waitFor(t, "recovery", func() bool {
		s := ctrl.Snapshot()
		return s.Reachable && s.Status != nil && s.Status.Label == "Off"
	})
}

func TestInitialStatusFailureNotice(t *testing.T) {
	f := newFakeDaemon()
	f.statusErr = errors.New("boom")
	ctrl := NewController(f, Options{})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	ctrl.Poll(ctx)

	st := ctrl.Snapshot()
	if st.Status != nil {
		t.Fatalf("status = %+v, want nil before the first successful load", st.Status)
	}
	if st.StatusError != "boom" {
		t.Fatalf("status error = %q, want boom", st.StatusError)
	}
	if n := st.Notice; n == nil || n.Message != "could not refresh the status: boom" || n.Tone != toneBad {
		t.Fatalf("notice = %+v", n)
	}
}

func TestPollAutoLoadsAccountAndLocations(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.account = api.AccountInfo{Email: "user@example.com", Subscribed: true}
		d.locations = api.LocationsResult{Countries: []serverlist.Country{{Code: "DE", Name: "Germany"}}}
	})
	ctrl := NewController(f, quietOptions())
	startPoll(t, ctrl)

	waitFor(t, "account and locations loads", func() bool {
		s := ctrl.Snapshot()
		return s.Account != nil && s.Account.Email == "user@example.com" &&
			s.LocationsLoaded && len(s.Locations) == 1 && s.Locations[0].Code == "DE"
	})
}

func TestLocationsLoadedState(t *testing.T) {
	f := newFakeDaemon() // an empty server list: a settled, loaded state
	ctrl := NewController(f, quietOptions())

	if s := ctrl.Snapshot(); s.LocationsLoaded {
		t.Fatal("locations reported as loaded before the pump started")
	}
	startPoll(t, ctrl)
	waitFor(t, "empty locations load", func() bool { return ctrl.Snapshot().LocationsLoaded })

	s := ctrl.Snapshot()
	if len(s.Locations) != 0 {
		t.Fatalf("locations = %+v, want an empty list", s.Locations)
	}
}

func TestDispatchRefresh(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.account = api.AccountInfo{Email: "user@example.com"}
		d.locations = api.LocationsResult{Countries: []serverlist.Country{{Code: "DE", Name: "Germany"}}}
	})
	ctrl := NewController(f, Options{})

	if err := ctrl.Dispatch(Action{Name: ActionRefresh}); err != nil {
		t.Fatalf("Dispatch(refresh) = %v", err)
	}
	// The status snapshot reloads synchronously; the account and server-list
	// loads re-run through the same paths Poll uses.
	if s := ctrl.Snapshot().Status; s == nil {
		t.Fatal("status not loaded by the refresh")
	}
	waitFor(t, "refresh loads", func() bool {
		s := ctrl.Snapshot()
		return s.Account != nil && s.LocationsLoaded && len(s.Locations) == 1
	})
}

func TestDispatchRefreshFailureNotice(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, Options{})
	if err := ctrl.Dispatch(Action{Name: ActionRefresh}); err != nil {
		t.Fatalf("Dispatch(refresh) = %v", err)
	}

	f.set(func(d *fakeDaemon) { d.statusErr = errors.New("boom") })
	err := ctrl.Dispatch(Action{Name: ActionRefresh})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Dispatch(refresh) = %v, want the status error", err)
	}
	if n := ctrl.Snapshot().Notice; n == nil || n.Message != "could not refresh the status: boom" || n.Tone != toneBad {
		t.Fatalf("notice = %+v", n)
	}
}

func TestDispatchRefreshConcurrentWithPoll(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.account = api.AccountInfo{Email: "user@example.com"}
		d.locations = api.LocationsResult{Countries: []serverlist.Country{{Code: "DE", Name: "Germany"}}}
	})
	ctrl := NewController(f, Options{
		PumpInterval:     time.Millisecond,
		StatusInterval:   time.Millisecond,
		SettingsInterval: time.Hour,
	})
	startPoll(t, ctrl)

	for i := 0; i < 20; i++ {
		if err := ctrl.Dispatch(Action{Name: ActionRefresh}); err != nil {
			t.Fatalf("Dispatch(refresh) #%d = %v", i, err)
		}
	}
	waitFor(t, "concurrent refresh state", func() bool {
		s := ctrl.Snapshot()
		return s.Status != nil && s.Account != nil && s.LocationsLoaded
	})
}

func TestPollAutoLoadFailuresSetNotices(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.accountErr = errors.New("guardian down")
		d.locationsErr = errors.New("no list")
	})
	ctrl := NewController(f, quietOptions())
	startPoll(t, ctrl)

	waitFor(t, "load failure notices", func() bool {
		s := ctrl.Snapshot()
		return s.AccountNotice != nil && s.LocationsNotice != nil
	})
	s := ctrl.Snapshot()
	if s.AccountNotice.Message != "could not load the account: guardian down" {
		t.Fatalf("account notice = %+v", s.AccountNotice)
	}
	if s.LocationsNotice.Message != "no list" {
		t.Fatalf("locations notice = %+v, want the raw load error", s.LocationsNotice)
	}
}

func TestPollAppliesSettingsCadence(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, Options{
		PumpInterval:     time.Millisecond,
		StatusInterval:   time.Hour,
		SettingsInterval: 2 * time.Millisecond,
	})
	startPoll(t, ctrl)

	waitFor(t, "periodic settings refresh", func() bool {
		return f.count(func(d *fakeDaemon) int { return d.settingsCalls }) >= 2
	})
	if s := ctrl.Snapshot().Settings; s == nil {
		t.Fatal("settings never projected")
	}
}

func TestProjectionHelpers(t *testing.T) {
	states := map[string]string{
		"":                "Off",
		"IDLE":            "Off",
		"CONNECTED":       "Connected",
		"PROXY_ONLY":      "Proxy only",
		"CONNECTING":      "Connecting",
		"RECONNECTING":    "Reconnecting",
		"WAITING_NETWORK": "Waiting for network",
		"UNAUTHENTICATED": "Not signed in",
		"FATAL":           "Error",
	}
	for in, want := range states {
		if got := stateLabel(in); got != want {
			t.Errorf("stateLabel(%q) = %q, want %q", in, got, want)
		}
	}
	if got := humanBytes(0); got != "0 B" {
		t.Errorf("humanBytes(0) = %q", got)
	}
	if got := humanBytes(4 << 30); got != "4.0 GiB" {
		t.Errorf("humanBytes(4GiB) = %q", got)
	}
	limit, remaining := int64(100), int64(40)
	if text, rem, lim := quotaView(&api.Quota{Limit: &limit, Remaining: &remaining}); text != "40 B remaining of 100 B" || rem != &remaining || lim != &limit {
		t.Errorf("quotaView = %q", text)
	}
	if got := rttLabel(nil); got != "timeout" {
		t.Errorf("rttLabel(nil) = %q", got)
	}
	ms := 42.4
	if got := rttLabel(&ms); got != "42 ms" {
		t.Errorf("rttLabel = %q", got)
	}
	if got := settingLabel("mtu"); got != "MTU" {
		t.Errorf("settingLabel(mtu) = %q", got)
	}
	if got := settingLabel("socks_bind"); got != "socks_bind" {
		t.Errorf("settingLabel(unknown) = %q", got)
	}
}

// --- DaemonClient (client.go) tests ---

func TestClientDialFailureWrapsUnreachable(t *testing.T) {
	var dials atomic.Int32
	c := NewDaemonClient()
	c.backoff = time.Millisecond
	c.dial = func(context.Context) (*ipc.Client, error) {
		dials.Add(1)
		return nil, errors.New("no pipe")
	}
	defer c.Close()

	_, err := c.Status(context.Background())
	if !errors.Is(err, ErrDaemonUnreachable) {
		t.Fatalf("Status error = %v, want ErrDaemonUnreachable", err)
	}
	if got := dials.Load(); got != maxDialAttempts {
		t.Errorf("dial attempts = %d, want %d", got, maxDialAttempts)
	}
	if c.Events() == nil {
		t.Fatal("Events() = nil")
	}
}

func TestClientCanceledContextStopsRetrying(t *testing.T) {
	c := NewDaemonClient()
	c.backoff = time.Hour
	c.dial = func(context.Context) (*ipc.Client, error) { return nil, errors.New("no pipe") }
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Status(ctx)
	if !errors.Is(err, ErrDaemonUnreachable) {
		t.Fatalf("Status error = %v, want ErrDaemonUnreachable", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("retried for %v despite the canceled context", elapsed)
	}
}

// newTestServer starts a Unix-socket control server; non-Linux hosts skip
// like the ipc package's own tests.
func newTestServer(t *testing.T, handler func(context.Context, *ipc.Conn) error) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "control.sock")
	srv, err := ipc.UnixTransport{}.Listen(path)
	if err != nil {
		if runtime.GOOS != "linux" {
			t.Skipf("unix sockets unavailable on this platform: %v", err)
		}
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		srv.Close()
	})
	go srv.Serve(ctx, handler)
	return path
}

// waitForNoConn blocks until the adapter has noticed a dropped connection,
// so tests that exercise the lazy re-dial do not race the drop.
func waitForNoConn(t *testing.T, c *DaemonClient) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("adapter never dropped the dead connection")
}

func TestClientRelaysEventsAndRedials(t *testing.T) {
	var conns atomic.Int32
	path := newTestServer(t, func(_ context.Context, conn *ipc.Conn) error {
		first := conns.Add(1) == 1
		for {
			env, err := conn.Read()
			if err != nil {
				return nil
			}
			if env.Type != ipc.TypeStatus {
				continue
			}
			_ = conn.Reply(env.ID, ipc.TypeStatus, api.Status{State: "CONNECTED", Authenticated: true})
			if first {
				_ = conn.Event(ipc.TypeStatus, api.Status{State: "CONNECTED", Authenticated: true})
				return nil // drop the first connection to force a re-dial
			}
		}
	})

	var dials atomic.Int32
	c := NewDaemonClient()
	c.backoff = time.Millisecond
	c.dial = func(ctx context.Context) (*ipc.Client, error) {
		dials.Add(1)
		return ipc.UnixTransport{}.Dial(ctx, path)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	st, err := c.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != "CONNECTED" || !st.Authenticated {
		t.Fatalf("status = %+v", st)
	}

	select {
	case ev := <-c.Events():
		var got api.Status
		if err := ev.DecodePayload(&got); err != nil {
			t.Fatalf("DecodePayload: %v", err)
		}
		if got.State != "CONNECTED" {
			t.Fatalf("event status = %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no event forwarded")
	}

	waitForNoConn(t, c)
	if _, err := c.Status(ctx); err != nil {
		t.Fatalf("Status after drop: %v", err)
	}
	if got := dials.Load(); got < 2 {
		t.Errorf("dial attempts = %d, want a re-dial", got)
	}
}

// A request whose reply is lost must not be re-sent: the daemon may have
// already executed it (a login step, a settings change, a log clear). The
// dead connection is dropped and the next request re-dials.
func TestClientNoResendAfterLostReply(t *testing.T) {
	var conns, clears atomic.Int32
	path := newTestServer(t, func(_ context.Context, conn *ipc.Conn) error {
		first := conns.Add(1) == 1
		for {
			env, err := conn.Read()
			if err != nil {
				return nil
			}
			if env.Type != ipc.TypeLogsClear {
				continue
			}
			clears.Add(1)
			if first {
				return nil // process the request, then lose the reply
			}
			_ = conn.Reply(env.ID, ipc.TypeResult, api.Result{OK: true})
		}
	})

	c := NewDaemonClient()
	c.backoff = time.Millisecond
	c.dial = func(ctx context.Context) (*ipc.Client, error) {
		return ipc.UnixTransport{}.Dial(ctx, path)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := c.LogsClear(ctx); err == nil {
		t.Fatal("LogsClear error = nil after a lost reply, want a transport error")
	}
	if got := clears.Load(); got != 1 {
		t.Fatalf("LogsClear executed %d times, want exactly 1 (a lost reply must not resend it)", got)
	}

	waitForNoConn(t, c)
	if _, err := c.LogsClear(ctx); err != nil {
		t.Fatalf("LogsClear after re-dial: %v", err)
	}
	if got := clears.Load(); got != 2 {
		t.Fatalf("LogsClear executed %d times, want 2", got)
	}
	if got := conns.Load(); got < 2 {
		t.Errorf("connections = %d, want a re-dial", got)
	}
}
