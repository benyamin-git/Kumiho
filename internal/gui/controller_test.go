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
	"github.com/benyamin-git/kumiho/internal/doctor"
	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/serverlist"
	"github.com/benyamin-git/kumiho/internal/version"
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

	accounts      api.AccountsView
	accountsErr   error
	accountsCalls int

	accountsAddStartState  api.LoginState
	accountsAddStartErr    error
	accountsAddStarts      []api.LoginStartPayload
	accountsAddStatusState api.LoginState
	accountsAddStatusErr   error
	accountsAddStatuses    []api.LoginStatusPayload
	accountsAddCancelErr   error
	accountsAddCancels     []api.LoginCancelPayload

	accountsRemoveErr   error
	accountsRemoves     []api.AccountsRemovePayload
	accountsUseErr      error
	accountsUses        []api.AccountsUsePayload
	accountsUpdateErr   error
	accountsUpdates     []api.AccountsUpdatePayload
	accountsRotationErr error
	accountsRotations   []api.AccountsRotationPayload

	loginStartState  api.LoginState
	loginStartErr    error
	loginStarts      []api.LoginStartPayload
	loginStatusState api.LoginState
	loginStatusErr   error
	loginStatuses    []api.LoginStatusPayload
	loginCancelErr   error
	loginCancels     []api.LoginCancelPayload
	loginCompleteErr error
	loginCompletes   []api.LoginCompletePayload

	locations         api.LocationsResult
	locationsErr      error
	locationCalls     int
	locationRefreshes []bool

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

func (f *fakeDaemon) locationRefreshFlags() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]bool(nil), f.locationRefreshes...)
}

func (f *fakeDaemon) subscribeLevels() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.subscribes...)
}

func (f *fakeDaemon) accountAddStartPayloads() []api.LoginStartPayload {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]api.LoginStartPayload(nil), f.accountsAddStarts...)
}

func (f *fakeDaemon) accountAddStatusPayloads() []api.LoginStatusPayload {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]api.LoginStatusPayload(nil), f.accountsAddStatuses...)
}

func (f *fakeDaemon) loginCompletePayloads() []api.LoginCompletePayload {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]api.LoginCompletePayload(nil), f.loginCompletes...)
}

func (f *fakeDaemon) accountRemovePayloads() []api.AccountsRemovePayload {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]api.AccountsRemovePayload(nil), f.accountsRemoves...)
}

func (f *fakeDaemon) accountUsePayloads() []api.AccountsUsePayload {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]api.AccountsUsePayload(nil), f.accountsUses...)
}

func (f *fakeDaemon) accountUpdatePayloads() []api.AccountsUpdatePayload {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]api.AccountsUpdatePayload(nil), f.accountsUpdates...)
}

func (f *fakeDaemon) accountRotationPayloads() []api.AccountsRotationPayload {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]api.AccountsRotationPayload(nil), f.accountsRotations...)
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

func (f *fakeDaemon) Locations(_ context.Context, refresh bool) (api.LocationsResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "locations")
	f.locationCalls++
	f.locationRefreshes = append(f.locationRefreshes, refresh)
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

func (f *fakeDaemon) AccountsList(context.Context) (api.AccountsView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "accounts_list")
	f.accountsCalls++
	return f.accounts, f.accountsErr
}

func (f *fakeDaemon) AccountsAddStart(_ context.Context, p api.LoginStartPayload) (api.LoginState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "accounts_add_start")
	f.accountsAddStarts = append(f.accountsAddStarts, p)
	return f.accountsAddStartState, f.accountsAddStartErr
}

func (f *fakeDaemon) AccountsAddStatus(_ context.Context, p api.LoginStatusPayload) (api.LoginState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "accounts_add_status")
	f.accountsAddStatuses = append(f.accountsAddStatuses, p)
	return f.accountsAddStatusState, f.accountsAddStatusErr
}

func (f *fakeDaemon) AccountsAddCancel(_ context.Context, p api.LoginCancelPayload) (api.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "accounts_add_cancel")
	f.accountsAddCancels = append(f.accountsAddCancels, p)
	return api.Result{OK: true}, f.accountsAddCancelErr
}

func (f *fakeDaemon) AccountsRemove(_ context.Context, p api.AccountsRemovePayload) (api.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "accounts_remove")
	f.accountsRemoves = append(f.accountsRemoves, p)
	return api.Result{OK: true}, f.accountsRemoveErr
}

func (f *fakeDaemon) AccountsUse(_ context.Context, p api.AccountsUsePayload) (api.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "accounts_use")
	f.accountsUses = append(f.accountsUses, p)
	return api.Result{OK: true}, f.accountsUseErr
}

func (f *fakeDaemon) AccountsUpdate(_ context.Context, p api.AccountsUpdatePayload) (api.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "accounts_update")
	f.accountsUpdates = append(f.accountsUpdates, p)
	return api.Result{OK: true}, f.accountsUpdateErr
}

func (f *fakeDaemon) AccountsSetRotation(_ context.Context, p api.AccountsRotationPayload) (api.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "accounts_set_rotation")
	f.accountsRotations = append(f.accountsRotations, p)
	return api.Result{OK: true}, f.accountsRotationErr
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

func (f *fakeDaemon) LoginStart(_ context.Context, p api.LoginStartPayload) (api.LoginState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "login_start")
	f.loginStarts = append(f.loginStarts, p)
	return f.loginStartState, f.loginStartErr
}

func (f *fakeDaemon) LoginStatus(_ context.Context, p api.LoginStatusPayload) (api.LoginState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "login_status")
	f.loginStatuses = append(f.loginStatuses, p)
	return f.loginStatusState, f.loginStatusErr
}

func (f *fakeDaemon) LoginCancel(_ context.Context, p api.LoginCancelPayload) (api.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "login_cancel")
	f.loginCancels = append(f.loginCancels, p)
	return api.Result{OK: true}, f.loginCancelErr
}

func (f *fakeDaemon) LoginComplete(_ context.Context, p api.LoginCompletePayload) (api.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "login_complete")
	f.loginCompletes = append(f.loginCompletes, p)
	return api.Result{OK: true}, f.loginCompleteErr
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

func TestNewControllerUpdateCurrent(t *testing.T) {
	ctrl := NewController(newFakeDaemon(), Options{})
	// The Version row shows the running build from the first snapshot,
	// before any check has run (T2.4 ruling).
	if up := ctrl.Snapshot().Update; up.Current != version.Version || up.Latest != "" || up.CheckedAt != "" {
		t.Fatalf("Update = %+v, want just Current=%q", up, version.Version)
	}
}

func TestDispatchUpdateCheck(t *testing.T) {
	f := newFakeDaemon()
	info := UpdateInfo{
		Current:   "v0.3.0",
		Latest:    "v0.4.0",
		URL:       "https://example.test/r",
		CheckedAt: "2026-10-16T00:00:00Z",
	}
	var gotCtx context.Context
	ctrl := NewController(f, Options{
		UpdateCheck: func(ctx context.Context) (UpdateInfo, error) {
			gotCtx = ctx
			return info, nil
		},
	})

	if err := ctrl.Dispatch(Action{Name: ActionUpdateCheck}); err != nil {
		t.Fatalf("Dispatch(update_check) = %v", err)
	}
	if _, ok := gotCtx.Deadline(); !ok {
		t.Error("the injected checker saw no deadline; the action must bound the check")
	}
	if got := ctrl.Snapshot().Update; got != info {
		t.Fatalf("Update = %+v, want %+v", got, info)
	}
	if n := ctrl.Snapshot().SettingsNotice; n == nil || n.Message != "Kumiho v0.4.0 is available" || n.Tone != toneInfo || n.Expires == nil {
		t.Fatalf("settings notice = %+v, want a 10 s availability notice", n)
	}
	// The check talks to GitHub, never the daemon: no daemon call, no
	// reachability change.
	if calls := f.callLog(); len(calls) != 0 {
		t.Fatalf("daemon calls = %v, want none", calls)
	}
	if !ctrl.Snapshot().Reachable {
		t.Error("reachability changed; the update check must not touch it")
	}
}

func TestDispatchUpdateCheckUpToDate(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, Options{
		UpdateCheck: func(context.Context) (UpdateInfo, error) {
			return UpdateInfo{Current: "v0.3.0", Latest: "V0.3.0", URL: "https://example.test/r"}, nil
		},
	})
	if err := ctrl.Dispatch(Action{Name: ActionUpdateCheck}); err != nil {
		t.Fatalf("Dispatch(update_check) = %v", err)
	}
	if n := ctrl.Snapshot().SettingsNotice; n == nil || n.Message != "Kumiho is up to date" || n.Tone != toneGood {
		t.Fatalf("settings notice = %+v, want the up-to-date notice", n)
	}
}

func TestDispatchUpdateCheckFailure(t *testing.T) {
	f := newFakeDaemon()
	boom := errors.New("dial tcp: no route")
	ctrl := NewController(f, Options{
		UpdateCheck: func(context.Context) (UpdateInfo, error) {
			return UpdateInfo{}, boom
		},
	})
	err := ctrl.Dispatch(Action{Name: ActionUpdateCheck})
	if !errors.Is(err, boom) {
		t.Fatalf("Dispatch(update_check) = %v, want the checker error", err)
	}
	// A failure keeps the previous projection and is a persistent settings
	// notice; it never flips reachability.
	if got := ctrl.Snapshot().Update; got.Current != version.Version || got.Latest != "" {
		t.Fatalf("Update = %+v, want the construction-time projection", got)
	}
	if n := ctrl.Snapshot().SettingsNotice; n == nil || n.Message != "could not check for updates: dial tcp: no route" || n.Tone != toneBad || n.Expires != nil {
		t.Fatalf("settings notice = %+v, want a persistent error notice", n)
	}
	if !ctrl.Snapshot().Reachable {
		t.Error("reachability flipped on an update-check failure")
	}
}

func TestDispatchUpdateCheckFillsCurrent(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, Options{
		UpdateCheck: func(context.Context) (UpdateInfo, error) {
			return UpdateInfo{Latest: "v0.4.0", URL: "https://example.test/r"}, nil
		},
	})
	if err := ctrl.Dispatch(Action{Name: ActionUpdateCheck}); err != nil {
		t.Fatalf("Dispatch(update_check) = %v", err)
	}
	if got := ctrl.Snapshot().Update.Current; got != version.Version {
		t.Fatalf("Current = %q, want internal/version's %q", got, version.Version)
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

	// The export and open-folder handlers are daemon-free and leave the
	// subscribe lifecycle alone: with the screen gone the retained buffer
	// still exports, and a later visit's subscribe still appends.
	path := filepath.Join(t.TempDir(), "kumiho-logs-20261016-120000.log")
	var exported []string
	ctrl.opts.LogsExport = func(content string) (string, error) {
		exported = append(exported, content)
		return path, nil
	}
	ctrl.opts.OpenLogsFolder = func() (string, error) { return filepath.Dir(path), nil }
	if err := ctrl.Dispatch(Action{Name: ActionLogsSnapshot, Payload: []byte(`{"level":"info"}`)}); err != nil {
		t.Fatalf("Dispatch(logs_snapshot) = %v", err)
	}
	if err := ctrl.Dispatch(Action{Name: ActionLogsExport}); err != nil {
		t.Fatalf("Dispatch(logs_export) = %v", err)
	}
	if len(exported) != 1 || !strings.Contains(exported[0], "boot") {
		t.Fatalf("exported content = %q, want the retained boot entry", exported)
	}
	if err := ctrl.Dispatch(Action{Name: ActionOpenLogsFolder}); err != nil {
		t.Fatalf("Dispatch(open_logs_folder) = %v", err)
	}
	if got := ctrl.Snapshot().LogsNotice; got == nil || got.Message != "Opened "+filepath.Dir(path) || got.Tone != toneGood {
		t.Fatalf("logs notice = %+v, want the opened folder", got)
	}
	// A re-visit (snapshot + subscribe) streams live entries again.
	if err := ctrl.Dispatch(Action{Name: ActionLogsSubscribe, Payload: []byte(`{"level":"info"}`)}); err != nil {
		t.Fatalf("Dispatch(logs_subscribe) = %v", err)
	}
	if got := f.subscribeLevels(); len(got) != 2 || got[1] != "info" {
		t.Fatalf("subscribe levels = %v, want two visits", got)
	}
	f.emit(t, ipc.TypeLog, logging.Entry{Level: logging.Info, Tag: "daemon", Msg: "again"})
	waitFor(t, "second visit live entry", func() bool {
		for _, e := range ctrl.Snapshot().Logs {
			if e.Msg == "again" {
				return true
			}
		}
		return false
	})
	if err := ctrl.Dispatch(Action{Name: ActionLogsUnsubscribe}); err != nil {
		t.Fatalf("Dispatch(logs_unsubscribe) = %v", err)
	}
	if got := f.count(func(d *fakeDaemon) int { return d.unsubscribes }); got != 2 {
		t.Fatalf("unsubscribes = %d, want 2 (one per visit; no leaked subscription)", got)
	}
}

func TestDispatchLogsExport(t *testing.T) {
	f := newFakeDaemon()
	base := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	f.set(func(d *fakeDaemon) {
		d.logSnapshot = api.LogSnapshot{Entries: []logging.Entry{
			{Time: base, Level: logging.Debug, Tag: "daemon", Msg: "cache hit"},
			{Time: base.Add(time.Second), Level: logging.Info, Tag: "tunnel", Msg: "connect via CONNECT"},
			{Time: base.Add(2 * time.Second), Level: logging.Warn, Tag: "watchdog", Msg: "probe timed out"},
		}}
	})
	path := filepath.Join(t.TempDir(), "kumiho-logs-20261016-100000.log")
	var contents []string
	ctrl := NewController(f, Options{
		LogsExport: func(content string) (string, error) {
			contents = append(contents, content)
			return path, nil
		},
	})

	if err := ctrl.Dispatch(Action{Name: ActionLogsSnapshot, Payload: []byte(`{"level":"debug"}`)}); err != nil {
		t.Fatalf("Dispatch(logs_snapshot) = %v", err)
	}

	// The level filter: info drops the debug entry; the payload is the
	// screen's local view state.
	if err := ctrl.Dispatch(Action{Name: ActionLogsExport, Payload: []byte(`{"level":"info"}`)}); err != nil {
		t.Fatalf("Dispatch(logs_export) = %v", err)
	}
	want := "2026-10-16T10:00:01Z INF [tunnel] connect via CONNECT\n" +
		"2026-10-16T10:00:02Z WRN [watchdog] probe timed out\n"
	if len(contents) != 1 || contents[0] != want {
		t.Fatalf("exported content = %q, want %q", contents, want)
	}
	n := ctrl.Snapshot().LogsNotice
	if n == nil || n.Message != "Exported to "+path || n.Tone != toneGood || n.Expires == nil {
		t.Fatalf("logs notice = %+v, want the exported path", n)
	}

	// The search filter: case-insensitive on tag or message, applied on top
	// of the level.
	if err := ctrl.Dispatch(Action{Name: ActionLogsExport, Payload: []byte(`{"level":"debug","search":"CACHE"}`)}); err != nil {
		t.Fatalf("Dispatch(logs_export, search) = %v", err)
	}
	if len(contents) != 2 || contents[1] != "2026-10-16T10:00:00Z DBG [daemon] cache hit\n" {
		t.Fatalf("searched content = %q", contents)
	}
	if err := ctrl.Dispatch(Action{Name: ActionLogsExport, Payload: []byte(`{"level":"debug","search":"watchdog probe"}`)}); err != nil {
		t.Fatalf("Dispatch(logs_export, phrase search) = %v", err)
	}
	if len(contents) != 3 || contents[2] != "2026-10-16T10:00:02Z WRN [watchdog] probe timed out\n" {
		t.Fatalf("phrase-searched content = %q", contents)
	}

	// Nothing visible: the inventory's notice, no writer call.
	if err := ctrl.Dispatch(Action{Name: ActionLogsExport, Payload: []byte(`{"level":"error"}`)}); err != nil {
		t.Fatalf("Dispatch(logs_export, empty) = %v", err)
	}
	n = ctrl.Snapshot().LogsNotice
	if n == nil || n.Message != "Nothing to export" || n.Tone != toneInfo || n.Expires == nil {
		t.Fatalf("logs notice = %+v, want Nothing to export", n)
	}
	if len(contents) != 3 {
		t.Fatalf("writer called for an empty export: %q", contents)
	}

	// Strict decoding, like every action payload.
	if err := ctrl.Dispatch(Action{Name: ActionLogsExport, Payload: []byte(`{"bogus":true}`)}); err == nil {
		t.Fatal("Dispatch(logs_export, unknown field) = nil, want a decode error")
	}
	if err := ctrl.Dispatch(Action{Name: ActionLogsExport, Payload: []byte(`{"level":"trace"}`)}); err == nil {
		t.Fatal("Dispatch(logs_export, unknown level) = nil, want a parse error")
	}

	// A writer failure is the logs error notice and the returned error.
	boom := errors.New("disk full")
	ctrl.opts.LogsExport = func(string) (string, error) { return "", boom }
	err := ctrl.Dispatch(Action{Name: ActionLogsExport, Payload: []byte(`{"level":"info"}`)})
	if !errors.Is(err, boom) {
		t.Fatalf("Dispatch(logs_export) = %v, want the writer error", err)
	}
	if n := ctrl.Snapshot().LogsNotice; n == nil || n.Message != "could not export the logs: disk full" || n.Tone != toneBad || n.Expires != nil {
		t.Fatalf("logs notice = %+v", n)
	}
}

func TestDispatchLogsExportUnavailableByDefault(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.logSnapshot = api.LogSnapshot{Entries: []logging.Entry{{Level: logging.Info, Tag: "daemon", Msg: "boot"}}}
	})
	ctrl := NewController(f, Options{}) // no LogsExport seam

	if err := ctrl.Dispatch(Action{Name: ActionLogsSnapshot, Payload: []byte(`{"level":"info"}`)}); err != nil {
		t.Fatalf("Dispatch(logs_snapshot) = %v", err)
	}
	err := ctrl.Dispatch(Action{Name: ActionLogsExport, Payload: []byte(`{"level":"info"}`)})
	if err == nil || !strings.Contains(err.Error(), "log export is not available") {
		t.Fatalf("Dispatch(logs_export) = %v, want the unavailable error", err)
	}
	if n := ctrl.Snapshot().LogsNotice; n == nil || n.Message != "could not export the logs: log export is not available" || n.Tone != toneBad {
		t.Fatalf("logs notice = %+v", n)
	}
	// The unavailable seam is only reached with something visible: an empty
	// visible set still reports the inventory's Nothing to export.
	if err := ctrl.Dispatch(Action{Name: ActionLogsClear}); err != nil {
		t.Fatalf("Dispatch(logs_clear) = %v", err)
	}
	if err := ctrl.Dispatch(Action{Name: ActionLogsExport, Payload: []byte(`{"level":"info"}`)}); err != nil {
		t.Fatalf("Dispatch(logs_export, empty) = %v", err)
	}
	if n := ctrl.Snapshot().LogsNotice; n == nil || n.Message != "Nothing to export" {
		t.Fatalf("logs notice = %+v, want Nothing to export", n)
	}
}

func TestDispatchOpenLogsFolder(t *testing.T) {
	f := newFakeDaemon()
	dir := filepath.Join(t.TempDir(), "kumiho", "logs")
	ctrl := NewController(f, Options{
		OpenLogsFolder: func() (string, error) { return dir, nil },
	})

	if err := ctrl.Dispatch(Action{Name: ActionOpenLogsFolder}); err != nil {
		t.Fatalf("Dispatch(open_logs_folder) = %v", err)
	}
	if n := ctrl.Snapshot().LogsNotice; n == nil || n.Message != "Opened "+dir || n.Tone != toneGood || n.Expires == nil {
		t.Fatalf("logs notice = %+v, want the opened folder", n)
	}

	boom := errors.New("no file manager")
	ctrl.opts.OpenLogsFolder = func() (string, error) { return "", boom }
	err := ctrl.Dispatch(Action{Name: ActionOpenLogsFolder})
	if !errors.Is(err, boom) {
		t.Fatalf("Dispatch(open_logs_folder) = %v, want the open error", err)
	}
	if n := ctrl.Snapshot().LogsNotice; n == nil || n.Message != "could not open the log folder: no file manager" || n.Tone != toneBad {
		t.Fatalf("logs notice = %+v", n)
	}

	// The default seam refuses deterministically.
	ctrl.opts.OpenLogsFolder = nil
	err = ctrl.Dispatch(Action{Name: ActionOpenLogsFolder})
	if err == nil || !strings.Contains(err.Error(), "opening the log folder is not available") {
		t.Fatalf("Dispatch(open_logs_folder, default) = %v", err)
	}
	if got := f.callLog(); len(got) != 0 {
		t.Fatalf("export/open handlers called the daemon: %v", got)
	}
}

// TestDispatchOpenLogsFolderNoticeRouting pins the open_logs_folder payload's
// notice surface: the default and "logs" land on the Logs notice, and
// "diagnostics" (the Diagnostics screen's request, T3.3) lands on the
// diagnostics notice with the same wording; an unknown surface is refused.
func TestDispatchOpenLogsFolderNoticeRouting(t *testing.T) {
	f := newFakeDaemon()
	dir := filepath.Join(t.TempDir(), "kumiho", "logs")
	ctrl := NewController(f, Options{
		OpenLogsFolder: func() (string, error) { return dir, nil },
	})

	if err := ctrl.Dispatch(Action{
		Name:    ActionOpenLogsFolder,
		Payload: []byte(`{"notice":"diagnostics"}`),
	}); err != nil {
		t.Fatalf("Dispatch(open_logs_folder, diagnostics) = %v", err)
	}
	snap := ctrl.Snapshot()
	if n := snap.DiagnosticsNotice; n == nil || n.Message != "Opened "+dir || n.Tone != toneGood {
		t.Fatalf("diagnostics notice = %+v, want the opened folder", n)
	}
	if n := snap.LogsNotice; n != nil {
		t.Fatalf("logs notice = %+v, want the diagnostics routing to leave it alone", n)
	}

	if err := ctrl.Dispatch(Action{
		Name:    ActionOpenLogsFolder,
		Payload: []byte(`{"notice":"logs"}`),
	}); err != nil {
		t.Fatalf("Dispatch(open_logs_folder, logs) = %v", err)
	}
	if n := ctrl.Snapshot().LogsNotice; n == nil || n.Message != "Opened "+dir {
		t.Fatalf("logs notice = %+v, want the opened folder", n)
	}

	if err := ctrl.Dispatch(Action{
		Name:    ActionOpenLogsFolder,
		Payload: []byte(`{"notice":"account"}`),
	}); err == nil || !strings.Contains(err.Error(), `unknown notice surface "account"`) {
		t.Fatalf("Dispatch(open_logs_folder, account) = %v, want a refusal", err)
	}
}

// testDoctorReport is a canned two-check doctor report: a mapped OK check
// and an unmapped failure.
func testDoctorReport() doctor.Report {
	return doctor.Report{
		Version: "kumiho test",
		Host:    "test-host",
		OS:      "linux/amd64",
		Time:    "2026-10-16T00:00:00Z",
		Checks: []doctor.Check{
			{Name: "platform", Status: doctor.OK, Detail: "linux (full tunnel supported)"},
			{Name: "brand-new-check", Status: doctor.Fail, Detail: "something happened", Remediation: "do the thing"},
		},
		Failed: 1,
	}
}

// TestDispatchDoctorRunProjectsReport pins the doctor_run projection into
// State.Doctor: the friendly title mapping, the raw passthrough, the chip
// words/tones, the details payload and the finished (not running) flag.
func TestDispatchDoctorRunProjectsReport(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, Options{DoctorRun: testDoctorReport})

	if d := ctrl.Snapshot().Doctor; d != nil {
		t.Fatalf("doctor = %+v before any run, want nil", d)
	}
	if err := ctrl.Dispatch(Action{Name: ActionDoctorRun}); err != nil {
		t.Fatalf("Dispatch(doctor_run) = %v", err)
	}
	d := ctrl.Snapshot().Doctor
	if d == nil || d.Running || len(d.Checks) != 2 {
		t.Fatalf("doctor = %+v, want a finished two-check projection", d)
	}
	first := d.Checks[0]
	if first.Name != "platform" || first.Title != "Platform" || first.Explanation == "" {
		t.Fatalf("check[0] = %+v, want the friendly Platform mapping", first)
	}
	if first.Status != "ok" || first.StatusWord != "OK" || first.Tone != toneGood {
		t.Fatalf("check[0] chip = (%s, %s, %s), want (ok, OK, good)", first.Status, first.StatusWord, first.Tone)
	}
	second := d.Checks[1]
	if second.Title != "brand-new-check" || second.Explanation != "" {
		t.Fatalf("check[1] = %+v, want the raw name passed through", second)
	}
	if second.StatusWord != "Failed" || second.Tone != toneBad || second.Remediation != "do the thing" {
		t.Fatalf("check[1] chip = %+v, want (Failed, bad) with remediation", second)
	}
	if !strings.HasPrefix(d.Details, "Kumiho "+version.Version+" diagnostics\n\n") || !strings.HasSuffix(d.Details, "\n") {
		t.Fatalf("details = %q, want the versioned header and trailing newline", d.Details)
	}
	if !strings.Contains(d.Details, `"brand-new-check"`) {
		t.Fatalf("details = %q, want the full report JSON", d.Details)
	}
	if got := f.callLog(); len(got) != 0 {
		t.Fatalf("doctor_run called the daemon: %v", got)
	}
}

// TestDispatchDoctorRunNeverInterrupts pins the re-run guard: while a run is
// in flight the screen reads running=true and a second doctor_run is a no-op
// that never starts a second suite (inventory §Diagnostics).
func TestDispatchDoctorRunNeverInterrupts(t *testing.T) {
	f := newFakeDaemon()
	started := make(chan struct{})
	release := make(chan struct{})
	var runs atomic.Int32
	ctrl := NewController(f, Options{DoctorRun: func() doctor.Report {
		if runs.Add(1) == 1 {
			close(started)
		}
		<-release
		return testDoctorReport()
	}})

	done := make(chan error, 1)
	go func() { done <- ctrl.Dispatch(Action{Name: ActionDoctorRun}) }()
	<-started

	d := ctrl.Snapshot().Doctor
	if d == nil || !d.Running || len(d.Checks) != 0 {
		t.Fatalf("doctor = %+v while running, want running with no checks", d)
	}
	if err := ctrl.Dispatch(Action{Name: ActionDoctorRun}); err != nil {
		t.Fatalf("second Dispatch(doctor_run) = %v, want a no-op", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Dispatch(doctor_run) = %v", err)
	}
	if got := runs.Load(); got != 1 {
		t.Fatalf("doctor suite ran %d times, want 1 (the second request must not interrupt)", got)
	}
	if d := ctrl.Snapshot().Doctor; d == nil || d.Running || len(d.Checks) != 2 {
		t.Fatalf("doctor = %+v after the run, want the finished projection", d)
	}
}

// TestDispatchDoctorRunClearsDiagnosticsNotice pins the run-start reset: a
// fresh run clears the previous report and any cleanup/service error, so the
// screen shows `Checking…` and the new summary is never masked.
func TestDispatchDoctorRunClearsDiagnosticsNotice(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, Options{DoctorRun: testDoctorReport})
	ctrl.SetDiagnosticsNotice("could not clean up: boom", toneBad)
	if n := ctrl.Snapshot().DiagnosticsNotice; n == nil {
		t.Fatal("diagnostics notice missing after SetDiagnosticsNotice")
	}
	if err := ctrl.Dispatch(Action{Name: ActionDoctorRun}); err != nil {
		t.Fatalf("Dispatch(doctor_run) = %v", err)
	}
	if n := ctrl.Snapshot().DiagnosticsNotice; n != nil {
		t.Fatalf("diagnostics notice = %+v, want it cleared by the run", n)
	}
}

// TestSetServiceStateAndDiagnosticsNotice pins the minimal host API (T3.3):
// SetServiceState lands in State.Service; SetDiagnosticsNotice applies the
// screen-local lifetime rules (bad persists, good expires at NoticeTTL).
func TestSetServiceStateAndDiagnosticsNotice(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, Options{DoctorRun: testDoctorReport})

	ctrl.SetServiceState(ServiceState{Installed: true, State: "running"})
	if got := ctrl.Snapshot().Service; got != (ServiceState{Installed: true, State: "running"}) {
		t.Fatalf("service = %+v, want the published snapshot", got)
	}

	ctrl.SetDiagnosticsNotice("Cleanup finished", toneGood)
	n := ctrl.Snapshot().DiagnosticsNotice
	if n == nil || n.Message != "Cleanup finished" || n.Tone != toneGood || n.Expires == nil {
		t.Fatalf("diagnostics notice = %+v, want a 10 s success", n)
	}

	ctrl.SetDiagnosticsNotice("could not clean up: boom", toneBad)
	n = ctrl.Snapshot().DiagnosticsNotice
	if n == nil || n.Message != "could not clean up: boom" || n.Tone != toneBad || n.Expires != nil {
		t.Fatalf("diagnostics notice = %+v, want a persistent failure", n)
	}
}

// TestOnboardedProjection pins the onboarding flag (T3.4): Options.Onboarded
// seeds it from gui.toml, SetOnboarded flips it exactly as the shell and the
// harness do after prefs_set {"onboarded":true}, and the Snapshot always
// carries the current value.
func TestOnboardedProjection(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, Options{Onboarded: true})
	if !ctrl.Snapshot().Onboarded {
		t.Fatal("onboarded = false, want the Options seed")
	}

	ctrl.SetOnboarded(false)
	if ctrl.Snapshot().Onboarded {
		t.Fatal("onboarded = true after SetOnboarded(false)")
	}
	ctrl.SetOnboarded(true)
	if !ctrl.Snapshot().Onboarded {
		t.Fatal("onboarded = false after SetOnboarded(true)")
	}

	// The zero value is the first run: the wizard shows.
	zero := NewController(newFakeDaemon(), Options{})
	if zero.Snapshot().Onboarded {
		t.Fatal("onboarded = true on a zero controller, want false")
	}
}

// TestSetSettingsNoticeAndAutostart pins the T4.2 host API: the persisted
// Start at login flag is projected into State.Autostart (a pointer, so false
// survives the envelope) and SetSettingsNotice applies the Settings
// screen-local lifetime rules (good expires at NoticeTTL, bad persists).
func TestSetSettingsNoticeAndAutostart(t *testing.T) {
	f := newFakeDaemon()
	on := true
	ctrl := NewController(f, Options{Autostart: &on})

	snap := ctrl.Snapshot()
	if snap.Autostart == nil || !*snap.Autostart {
		t.Fatalf("autostart = %v, want the projected true", snap.Autostart)
	}
	// The constructor copies the option: a caller-side flip cannot change
	// the published flag.
	on = false
	if got := ctrl.Snapshot().Autostart; got == nil || !*got {
		t.Fatalf("autostart = %v after mutating Options.Autostart, want true", got)
	}

	ctrl.SetAutostart(false)
	snap = ctrl.Snapshot()
	if snap.Autostart == nil || *snap.Autostart {
		t.Fatalf("autostart = %v after SetAutostart(false), want the projected false", snap.Autostart)
	}
	raw, err := EncodeState(snap)
	if err != nil {
		t.Fatalf("EncodeState: %v", err)
	}
	if !strings.Contains(string(raw), `"autostart":false`) {
		t.Errorf("envelope %s does not carry the off value", raw)
	}

	ctrl.SetSettingsNotice("Start at login saved", toneGood)
	n := ctrl.Snapshot().SettingsNotice
	if n == nil || n.Message != "Start at login saved" || n.Tone != toneGood || n.Expires == nil {
		t.Fatalf("settings notice = %+v, want a 10 s success", n)
	}
	if shared := ctrl.Snapshot().Notice; shared != nil {
		t.Fatalf("shared notice = %+v, want the settings slot only", shared)
	}

	ctrl.SetSettingsNotice("could not change the login setting: boom", toneBad)
	n = ctrl.Snapshot().SettingsNotice
	if n == nil || n.Message != "could not change the login setting: boom" ||
		n.Tone != toneBad || n.Expires != nil {
		t.Fatalf("settings notice = %+v, want a persistent failure", n)
	}

	// A controller whose host does not project the flag leaves it absent, so
	// the page keeps its session record.
	zero := NewController(newFakeDaemon(), Options{})
	if got := zero.Snapshot().Autostart; got != nil {
		t.Fatalf("autostart = %v on an unprojected controller, want nil", got)
	}
}

// TestDispatchLogsLoadedProjection pins the logs_loaded flag the screen's
// "Loading logs..." state reads: false until the first successful snapshot,
// true afterwards including after a clear (the ring is settled-empty, not
// loading), and false again only for a fresh controller whose snapshot
// never succeeded.
func TestDispatchLogsLoadedProjection(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, Options{})
	if ctrl.Snapshot().LogsLoaded {
		t.Fatal("logs_loaded = true before the first snapshot")
	}

	f.set(func(d *fakeDaemon) { d.logSnapshotErr = errors.New("ring gone") })
	if err := ctrl.Dispatch(Action{Name: ActionLogsSnapshot}); err == nil {
		t.Fatal("Dispatch(logs_snapshot) = nil, want the failure")
	}
	if ctrl.Snapshot().LogsLoaded {
		t.Fatal("logs_loaded = true after a failed snapshot")
	}

	f.set(func(d *fakeDaemon) { d.logSnapshotErr = nil })
	if err := ctrl.Dispatch(Action{Name: ActionLogsSnapshot}); err != nil {
		t.Fatalf("Dispatch(logs_snapshot) = %v", err)
	}
	// An empty successful snapshot is settled: loaded with no entries.
	st := ctrl.Snapshot()
	if !st.LogsLoaded || len(st.Logs) != 0 {
		t.Fatalf("logs_loaded = %v, logs = %+v, want settled-empty", st.LogsLoaded, st.Logs)
	}
	if err := ctrl.Dispatch(Action{Name: ActionLogsClear}); err != nil {
		t.Fatalf("Dispatch(logs_clear) = %v", err)
	}
	if !ctrl.Snapshot().LogsLoaded {
		t.Fatal("logs_loaded = false after a clear, want the settled-empty state")
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

	// The host-side actions are intercepted by the shell (or the harness)
	// before Dispatch; a miswired caller gets a refusal, not silence.
	for _, name := range []string{
		ActionCleanup, ActionServiceStart, ActionServiceStop, ActionPrefsSet,
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

// TestDispatchRefreshForcesLocationsDownload covers the {"locations":true}
// payload: the Locations screen's Refresh asks the daemon to re-download the
// server list even when one is already loaded, with the refresh flag set,
// and the fresh list replaces the stale one.
func TestDispatchRefreshForcesLocationsDownload(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.locations = api.LocationsResult{Countries: []serverlist.Country{{Code: "DE", Name: "Germany"}}}
	})
	ctrl := NewController(f, quietOptions())
	startPoll(t, ctrl)
	waitFor(t, "initial locations load", func() bool { return ctrl.Snapshot().LocationsLoaded })

	f.set(func(d *fakeDaemon) {
		d.locations = api.LocationsResult{Countries: []serverlist.Country{{Code: "JP", Name: "Japan"}}}
	})
	if err := ctrl.Dispatch(Action{Name: ActionRefresh, Payload: []byte(`{"locations":true}`)}); err != nil {
		t.Fatalf("Dispatch(refresh, locations) = %v", err)
	}
	waitFor(t, "forced locations reload", func() bool {
		s := ctrl.Snapshot()
		return len(s.Locations) == 1 && s.Locations[0].Code == "JP"
	})
	if flags := f.locationRefreshFlags(); len(flags) != 2 || flags[0] != false || flags[1] != true {
		t.Fatalf("Locations refresh flags = %v, want [false true]", flags)
	}
}

// TestDispatchRefreshLocationsFailureKeepsRows covers a failed forced
// re-download: the loaded rows stay and the raw error lands on the locations
// notice (inventory §Locations "a failed refresh keeps the rows and reports
// the error in the status line").
func TestDispatchRefreshLocationsFailureKeepsRows(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.locations = api.LocationsResult{Countries: []serverlist.Country{{Code: "DE", Name: "Germany"}}}
	})
	ctrl := NewController(f, quietOptions())
	startPoll(t, ctrl)
	waitFor(t, "initial locations load", func() bool { return ctrl.Snapshot().LocationsLoaded })

	f.set(func(d *fakeDaemon) { d.locationsErr = errors.New("list down") })
	if err := ctrl.Dispatch(Action{Name: ActionRefresh, Payload: []byte(`{"locations":true}`)}); err != nil {
		t.Fatalf("Dispatch(refresh, locations) = %v", err)
	}
	waitFor(t, "locations error notice", func() bool {
		n := ctrl.Snapshot().LocationsNotice
		return n != nil && n.Message == "list down" && n.Tone == toneBad
	})
	s := ctrl.Snapshot()
	if !s.LocationsLoaded || len(s.Locations) != 1 || s.Locations[0].Code != "DE" {
		t.Fatalf("locations after failed refresh = %+v (loaded=%v), want the stale DE list", s.Locations, s.LocationsLoaded)
	}
}

// TestDispatchRefreshWithoutLocationsKeepsGuard covers the plain payload
// (the Dashboard's Refresh/Retry): the autonomous meaning is unchanged, so a
// loaded list is never re-downloaded, and an unknown payload field is a
// strict-decoding error.
func TestDispatchRefreshWithoutLocationsKeepsGuard(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.locations = api.LocationsResult{Countries: []serverlist.Country{{Code: "DE", Name: "Germany"}}}
	})
	ctrl := NewController(f, quietOptions())
	startPoll(t, ctrl)
	waitFor(t, "initial locations load", func() bool { return ctrl.Snapshot().LocationsLoaded })

	if err := ctrl.Dispatch(Action{Name: ActionRefresh, Payload: []byte(`{}`)}); err != nil {
		t.Fatalf("Dispatch(refresh) = %v", err)
	}
	if got := f.count(func(d *fakeDaemon) int { return d.locationCalls }); got != 1 {
		t.Fatalf("Locations calls = %d, want 1 (the initial load; a plain refresh must not re-fetch)", got)
	}
	if err := ctrl.Dispatch(Action{Name: ActionRefresh, Payload: []byte(`{"bogus":true}`)}); err == nil {
		t.Fatal("Dispatch(refresh, unknown field) = nil, want a decode error")
	}
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

// TestControllerProjectsAccounts pins the account-list projection: the rows,
// the active ID, rotation settings and the daemon's notice all ride the
// state envelope unchanged.
func TestControllerProjectsAccounts(t *testing.T) {
	f := newFakeDaemon()
	limit, remaining := int64(50<<30), int64(41<<30)
	f.set(func(d *fakeDaemon) {
		d.accounts = api.AccountsView{
			Accounts: []api.AccountSummary{
				{ID: "a1", Email: "user@example.com", Method: "browser", Active: true, Quota: &api.Quota{Limit: &limit, Remaining: &remaining}},
				{ID: "a2", Email: "work@example.com", Label: "Work", Method: "password", ReauthRequired: true},
			},
			Active: "a1", Rotation: true, Threshold: 20, LastMethod: "browser", Notice: "2 accounts",
		}
	})
	ctrl := NewController(f, Options{})

	if err := ctrl.Dispatch(Action{Name: ActionAccountsList}); err != nil {
		t.Fatalf("Dispatch(accounts_list) = %v", err)
	}
	got := ctrl.Snapshot().Accounts
	if got == nil || len(got.Accounts) != 2 || got.Active != "a1" || !got.Rotation || got.Threshold != 20 || got.Notice != "2 accounts" {
		t.Fatalf("accounts = %+v", got)
	}
	if !got.Accounts[0].Active || got.Accounts[0].Quota == nil || *got.Accounts[0].Quota.Remaining != remaining {
		t.Fatalf("active account = %+v", got.Accounts[0])
	}
	if !got.Accounts[1].ReauthRequired || got.Accounts[1].Label != "Work" {
		t.Fatalf("reauth account = %+v", got.Accounts[1])
	}
}

func TestControllerBrowserLoginAction(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.accounts = api.AccountsView{
			Accounts: []api.AccountSummary{{ID: "a1", Email: "user@example.com", Active: true}},
			Active:   "a1", Notice: "2 accounts",
		}
		d.accountsAddStartState = api.LoginState{
			Step: "browser", Method: "browser",
			URL: "https://example.test/signin", LoginID: "login-1",
		}
		d.accountsAddStatusState = api.LoginState{Step: "done", Email: "new@example.com"}
	})
	ctrl := NewController(f, Options{})

	if err := ctrl.Dispatch(Action{Name: ActionAccountsAddStart, Payload: []byte(`{"method":"browser"}`)}); err != nil {
		t.Fatalf("Dispatch(accounts_add_start) = %v", err)
	}
	login := ctrl.Snapshot().Login
	if login == nil || login.Step != "browser" || login.Method != "browser" ||
		login.URL != "https://example.test/signin" || login.ID != "login-1" {
		t.Fatalf("login = %+v", login)
	}
	if got := f.accountAddStartPayloads(); len(got) != 1 || got[0].Method != "browser" || got[0].AccountID != "" {
		t.Fatalf("accounts_add_start payloads = %+v", got)
	}

	if err := ctrl.Dispatch(Action{Name: ActionAccountsAddStatus, Payload: []byte(`{"login_id":"login-1"}`)}); err != nil {
		t.Fatalf("Dispatch(accounts_add_status) = %v", err)
	}
	login = ctrl.Snapshot().Login
	if login == nil || login.Step != "done" || login.Email != "new@example.com" {
		t.Fatalf("login after done = %+v", login)
	}
	if got := f.accountAddStatusPayloads(); len(got) != 1 || got[0].LoginID != "login-1" {
		t.Fatalf("accounts_add_status payloads = %+v", got)
	}
	if got := f.count(func(d *fakeDaemon) int { return d.accountsCalls }); got < 1 {
		t.Fatalf("accounts_list calls = %d, want the add completion reload", got)
	}
	if got := ctrl.Snapshot().Accounts; got == nil || got.Notice != "2 accounts" {
		t.Fatalf("accounts after add = %+v", got)
	}
	if n := ctrl.Snapshot().AccountNotice; n == nil || n.Message != "Signed in as new@example.com" || n.Tone != toneGood {
		t.Fatalf("account notice after browser add = %+v", n)
	}
}

// TestControllerLoginCompleteAccountFlow pins the Account screen's paste
// flow: login_complete carries the exact redirect URL, the controller reads
// the terminal accounts_add_status reply immediately, claims the Account
// notice and clears the account-flow ownership.
func TestControllerLoginCompleteAccountFlow(t *testing.T) {
	const redirect = "urn:ietf:wg:oauth:2.0:oob?state=state-1&code=code-1"
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.accountsAddStartState = api.LoginState{
			Step: "browser", Method: "browser",
			URL: "https://example.test/signin", LoginID: "login-1",
		}
		d.accountsAddStatusState = api.LoginState{Step: "done", Email: "new@example.com"}
		d.accounts = api.AccountsView{
			Accounts: []api.AccountSummary{{ID: "a1", Email: "new@example.com", Active: true}},
			Active:   "a1", Notice: "2 accounts",
		}
		d.status = api.Status{State: "IDLE", Authenticated: true, Email: "new@example.com"}
	})
	ctrl := NewController(f, Options{})
	if err := ctrl.Dispatch(Action{Name: ActionAccountsAddStart, Payload: []byte(`{"method":"browser"}`)}); err != nil {
		t.Fatalf("Dispatch(accounts_add_start) = %v", err)
	}
	payload := fmt.Sprintf(`{"login_id":"login-1","redirect_url":%q}`, redirect)
	if err := ctrl.Dispatch(Action{Name: ActionLoginComplete, Payload: []byte(payload)}); err != nil {
		t.Fatalf("Dispatch(login_complete) = %v", err)
	}

	got := f.loginCompletePayloads()
	if len(got) != 1 || got[0].LoginID != "login-1" || got[0].RedirectURL != redirect || got[0].Code != "" {
		t.Fatalf("login_complete payloads = %+v", got)
	}
	if login := ctrl.Snapshot().Login; login == nil || login.Step != "done" || login.Email != "new@example.com" {
		t.Fatalf("login after completion = %+v", login)
	}
	if n := ctrl.Snapshot().AccountNotice; n == nil || n.Message != "Signed in as new@example.com" || n.Tone != toneGood {
		t.Fatalf("account notice after completion = %+v", n)
	}
	// The terminal reply reloads the list (the add-completion path).
	waitFor(t, "account reload after completion", func() bool {
		return f.count(func(d *fakeDaemon) int { return d.accountsCalls }) >= 1
	})
	// The account flow is over: no later onboarding verb can claim its notice.
	ctrl.mu.Lock()
	flow := ctrl.accountFlow
	ctrl.mu.Unlock()
	if flow {
		t.Fatal("accountFlow still set after the terminal completion")
	}
	// The immediate read is the account-screen verb, never the onboarding one.
	if calls := f.callLog(); !slices.Contains(calls, "accounts_add_status") || slices.Contains(calls, "login_status") {
		t.Fatalf("daemon calls = %v, want accounts_add_status and no login_status", calls)
	}
}

// TestControllerLoginCompleteOnboardingFlow pins the wizard's paste flow: the
// completion reads login_status, reports on the shared notice and never
// claims the Account screen's notice.
func TestControllerLoginCompleteOnboardingFlow(t *testing.T) {
	const redirect = "urn:ietf:wg:oauth:2.0:oob?state=state-2&code=code-2"
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.loginStartState = api.LoginState{
			Step: "browser", Method: "browser",
			URL: "https://example.test/signin", LoginID: "login-2",
		}
		d.loginStatusState = api.LoginState{Step: "done", Email: "user@example.com"}
		d.status = api.Status{State: "IDLE", Authenticated: true, Email: "user@example.com"}
	})
	ctrl := NewController(f, Options{})
	if err := ctrl.Dispatch(Action{Name: ActionLoginStart, Payload: []byte(`{"method":"browser"}`)}); err != nil {
		t.Fatalf("Dispatch(login_start) = %v", err)
	}
	payload := fmt.Sprintf(`{"login_id":"login-2","redirect_url":%q}`, redirect)
	if err := ctrl.Dispatch(Action{Name: ActionLoginComplete, Payload: []byte(payload)}); err != nil {
		t.Fatalf("Dispatch(login_complete) = %v", err)
	}

	got := f.loginCompletePayloads()
	if len(got) != 1 || got[0].LoginID != "login-2" || got[0].RedirectURL != redirect || got[0].Code != "" {
		t.Fatalf("login_complete payloads = %+v", got)
	}
	if login := ctrl.Snapshot().Login; login == nil || login.Step != "done" || login.Email != "user@example.com" {
		t.Fatalf("login after completion = %+v", login)
	}
	if n := ctrl.Snapshot().Notice; n == nil || n.Message != "Signed in as user@example.com" || n.Tone != toneInfo {
		t.Fatalf("shared notice after completion = %+v", n)
	}
	if n := ctrl.Snapshot().AccountNotice; n != nil {
		t.Fatalf("onboarding completion claimed the account notice: %+v", n)
	}
	if calls := f.callLog(); !slices.Contains(calls, "login_status") || slices.Contains(calls, "accounts_add_status") {
		t.Fatalf("daemon calls = %v, want login_status and no accounts_add_status", calls)
	}
}

// TestControllerLoginCompleteRetryableError pins the retry contract: a
// retryable completion error leaves the browser step and its URL on screen,
// shows the error inline on the login view, writes the action's notice on the
// owning screen and accepts a corrected paste afterwards.
func TestControllerLoginCompleteRetryableError(t *testing.T) {
	const redirect = "urn:ietf:wg:oauth:2.0:oob?state=state-1&code=code-1"
	const daemonErr = "the redirect URL does not belong to this sign-in flow"
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.accountsAddStartState = api.LoginState{
			Step: "browser", Method: "browser",
			URL: "https://example.test/signin", LoginID: "login-1",
		}
		d.loginCompleteErr = errors.New(daemonErr)
	})
	ctrl := NewController(f, Options{})
	if err := ctrl.Dispatch(Action{Name: ActionAccountsAddStart, Payload: []byte(`{"method":"browser"}`)}); err != nil {
		t.Fatalf("Dispatch(accounts_add_start) = %v", err)
	}

	payload := fmt.Sprintf(`{"login_id":"login-1","redirect_url":%q}`, redirect)
	err := ctrl.Dispatch(Action{Name: ActionLoginComplete, Payload: []byte(payload)})
	if !errors.Is(err, f.loginCompleteErr) {
		t.Fatalf("Dispatch(login_complete, retryable) = %v, want the daemon error", err)
	}
	login := ctrl.Snapshot().Login
	if login == nil || login.Step != "browser" || login.ID != "login-1" ||
		login.URL != "https://example.test/signin" || login.Error != daemonErr {
		t.Fatalf("login after retryable error = %+v, want the flow kept with an inline error", login)
	}
	n := ctrl.Snapshot().AccountNotice
	if n == nil || n.Message != "could not finish sign-in: "+daemonErr || n.Tone != toneBad || n.Expires != nil {
		t.Fatalf("account notice after retryable error = %+v", n)
	}

	// The waiting card's status poll carries no error of its own: it must not
	// erase the inline completion error before the user can act on it.
	f.set(func(d *fakeDaemon) {
		d.accountsAddStatusState = api.LoginState{
			Step: "browser", Method: "browser",
			URL: "https://example.test/signin", LoginID: "login-1",
		}
	})
	if err := ctrl.Dispatch(Action{Name: ActionAccountsAddStatus, Payload: []byte(`{"login_id":"login-1"}`)}); err != nil {
		t.Fatalf("Dispatch(accounts_add_status) = %v", err)
	}
	if login := ctrl.Snapshot().Login; login == nil || login.Step != "browser" ||
		login.ID != "login-1" || login.Error != daemonErr {
		t.Fatalf("login after the waiting poll = %+v, want the inline error kept", login)
	}

	// The flow is still active: a corrected paste completes it and clears the
	// inline error.
	f.set(func(d *fakeDaemon) {
		d.loginCompleteErr = nil
		d.accountsAddStatusState = api.LoginState{Step: "done", Email: "new@example.com"}
	})
	if err := ctrl.Dispatch(Action{Name: ActionLoginComplete, Payload: []byte(payload)}); err != nil {
		t.Fatalf("Dispatch(login_complete, retry) = %v", err)
	}
	if login := ctrl.Snapshot().Login; login == nil || login.Step != "done" || login.Error != "" {
		t.Fatalf("login after the retry = %+v", login)
	}
}

// TestControllerLoginCompleteLeavesTheWaitingCardPolling pins the non-
// terminal reply window: when the immediate status read still reports the
// browser step, the flow must stay intact so the screen's poll can land the
// daemon's later done/error reply.
func TestControllerLoginCompleteLeavesTheWaitingCardPolling(t *testing.T) {
	const redirect = "urn:ietf:wg:oauth:2.0:oob?state=state-3&code=code-3"
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.loginStartState = api.LoginState{
			Step: "browser", Method: "browser",
			URL: "https://example.test/signin", LoginID: "login-3",
		}
		d.loginStatusState = api.LoginState{
			Step: "browser", Method: "browser",
			URL: "https://example.test/signin", LoginID: "login-3",
		}
	})
	ctrl := NewController(f, Options{})
	if err := ctrl.Dispatch(Action{Name: ActionLoginStart, Payload: []byte(`{"method":"browser"}`)}); err != nil {
		t.Fatalf("Dispatch(login_start) = %v", err)
	}
	payload := fmt.Sprintf(`{"login_id":"login-3","redirect_url":%q}`, redirect)
	if err := ctrl.Dispatch(Action{Name: ActionLoginComplete, Payload: []byte(payload)}); err != nil {
		t.Fatalf("Dispatch(login_complete) = %v", err)
	}
	if login := ctrl.Snapshot().Login; login == nil || login.Step != "browser" ||
		login.ID != "login-3" || login.URL != "https://example.test/signin" {
		t.Fatalf("login after a pending completion read = %+v", login)
	}

	f.set(func(d *fakeDaemon) {
		d.loginStatusState = api.LoginState{Step: "done", Email: "user@example.com"}
	})
	if err := ctrl.Dispatch(Action{Name: ActionLoginStatus, Payload: []byte(`{"login_id":"login-3"}`)}); err != nil {
		t.Fatalf("Dispatch(login_status) = %v", err)
	}
	if login := ctrl.Snapshot().Login; login == nil || login.Step != "done" || login.Email != "user@example.com" {
		t.Fatalf("login after the poll = %+v", login)
	}
}

// TestControllerLoginCompleteStatusFailureKeepsFlow pins that a failed
// immediate status read after a successful completion is not reported as a
// sign-in failure and never erases the waiting flow: the completion succeeded,
// so the screen's poll keeps retrying.
func TestControllerLoginCompleteStatusFailureKeepsFlow(t *testing.T) {
	const redirect = "urn:ietf:wg:oauth:2.0:oob?state=state-4&code=code-4"
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.loginStartState = api.LoginState{
			Step: "browser", Method: "browser",
			URL: "https://example.test/signin", LoginID: "login-4",
		}
		d.loginStatusErr = errors.New("status unavailable")
	})
	ctrl := NewController(f, Options{})
	if err := ctrl.Dispatch(Action{Name: ActionLoginStart, Payload: []byte(`{"method":"browser"}`)}); err != nil {
		t.Fatalf("Dispatch(login_start) = %v", err)
	}
	payload := fmt.Sprintf(`{"login_id":"login-4","redirect_url":%q}`, redirect)
	if err := ctrl.Dispatch(Action{Name: ActionLoginComplete, Payload: []byte(payload)}); err != nil {
		t.Fatalf("Dispatch(login_complete) = %v, want nil (the completion succeeded)", err)
	}
	if login := ctrl.Snapshot().Login; login == nil || login.Step != "browser" ||
		login.ID != "login-4" || login.URL != "https://example.test/signin" {
		t.Fatalf("login after a status failure = %+v, want the waiting flow intact", login)
	}
	if n := ctrl.Snapshot().AccountNotice; n != nil {
		t.Fatalf("status failure after completion claimed the account notice: %+v", n)
	}
}

// TestControllerAccountPasswordFlowKeepsAccountNotice pins the ownership of a
// password sign-in started from Add account: its terminal login_password reply
// reuses the shared login verb, but the success belongs on the Account screen.
// A later onboarding sign-in must not inherit that ownership.
func TestControllerAccountPasswordFlowKeepsAccountNotice(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.accountsAddStartState = api.LoginState{Step: "password", Email: "new@example.com", Method: "email"}
		d.loginReplies["login_password"] = api.LoginState{Step: "done", Email: "new@example.com"}
		d.accounts = api.AccountsView{
			Accounts: []api.AccountSummary{{ID: "a1", Email: "new@example.com", Active: true}},
			Active:   "a1", Notice: "2 accounts",
		}
	})
	ctrl := NewController(f, Options{})

	if err := ctrl.Dispatch(Action{Name: ActionAccountsAddStart, Payload: []byte(`{"method":"password","email":"new@example.com"}`)}); err != nil {
		t.Fatalf("Dispatch(accounts_add_start) = %v", err)
	}
	if login := ctrl.Snapshot().Login; login == nil || login.Step != "password" {
		t.Fatalf("login after start = %+v", login)
	}
	if err := ctrl.Dispatch(Action{Name: ActionLoginPassword, Payload: []byte(`{"password":"hunter2"}`)}); err != nil {
		t.Fatalf("Dispatch(login_password) = %v", err)
	}
	if login := ctrl.Snapshot().Login; login == nil || login.Step != "done" {
		t.Fatalf("login after password = %+v", login)
	}
	if n := ctrl.Snapshot().AccountNotice; n == nil || n.Message != "Signed in as new@example.com" || n.Tone != toneGood {
		t.Fatalf("account notice after password add = %+v", n)
	}
	if got := f.count(func(d *fakeDaemon) int { return d.accountsCalls }); got < 1 {
		t.Fatalf("accounts_list calls = %d, want the password completion reload", got)
	}

	// Onboarding owns its own terminal notice. Reset the fake's reply and use
	// a fresh controller so the old Account notice cannot mask a leak.
	f.set(func(d *fakeDaemon) {
		d.loginStartState = api.LoginState{Step: "password", Email: "other@example.com", Method: "email"}
		d.loginReplies["login_password"] = api.LoginState{Step: "done", Email: "other@example.com"}
	})
	onboarding := NewController(f, Options{})
	if err := onboarding.Dispatch(Action{Name: ActionLoginStart, Payload: []byte(`{"method":"password","email":"other@example.com"}`)}); err != nil {
		t.Fatalf("Dispatch(login_start) = %v", err)
	}
	if err := onboarding.Dispatch(Action{Name: ActionLoginPassword, Payload: []byte(`{"password":"hunter2"}`)}); err != nil {
		t.Fatalf("Dispatch(login_password, onboarding) = %v", err)
	}
	if n := onboarding.Snapshot().AccountNotice; n != nil {
		t.Fatalf("onboarding password sign-in claimed the account notice: %+v", n)
	}
}

func TestControllerAccountActions(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, Options{})
	if err := ctrl.Dispatch(Action{Name: ActionAccountsList}); err != nil {
		t.Fatalf("Dispatch(accounts_list) = %v", err)
	}

	if err := ctrl.Dispatch(Action{Name: ActionAccountsUse, Payload: []byte(`{"id":"a2"}`)}); err != nil {
		t.Fatalf("Dispatch(accounts_use) = %v", err)
	}
	if got := f.accountUsePayloads(); len(got) != 1 || got[0].ID != "a2" {
		t.Fatalf("accounts_use payloads = %+v", got)
	}

	if err := ctrl.Dispatch(Action{Name: ActionAccountsUpdate, Payload: []byte(`{"id":"a2","label":"Home"}`)}); err != nil {
		t.Fatalf("Dispatch(accounts_update) = %v", err)
	}
	if got := f.accountUpdatePayloads(); len(got) != 1 || got[0].ID != "a2" || got[0].Label != "Home" {
		t.Fatalf("accounts_update payloads = %+v", got)
	}

	if err := ctrl.Dispatch(Action{Name: ActionAccountsSetRotation, Payload: []byte(`{"enabled":true,"rotation_threshold":20}`)}); err != nil {
		t.Fatalf("Dispatch(accounts_set_rotation) = %v", err)
	}
	if got := f.accountRotationPayloads(); len(got) != 1 || !got[0].Enabled || got[0].Threshold != 20 {
		t.Fatalf("accounts_set_rotation payloads = %+v", got)
	}

	if err := ctrl.Dispatch(Action{Name: ActionAccountsRemove, Payload: []byte(`{"id":"a2"}`)}); err != nil {
		t.Fatalf("Dispatch(accounts_remove) = %v", err)
	}
	if got := f.accountRemovePayloads(); len(got) != 1 || got[0].ID != "a2" {
		t.Fatalf("accounts_remove payloads = %+v", got)
	}

	f.set(func(d *fakeDaemon) { d.accountsRemoveErr = errors.New("still active") })
	err := ctrl.Dispatch(Action{Name: ActionAccountsRemove, Payload: []byte(`{"id":"a2"}`)})
	if !errors.Is(err, f.accountsRemoveErr) {
		t.Fatalf("Dispatch(accounts_remove, failure) = %v", err)
	}
	if n := ctrl.Snapshot().AccountNotice; n == nil || n.Message != "could not remove the account: still active" || n.Tone != toneBad {
		t.Fatalf("account notice = %+v", n)
	}
}

func TestControllerAccountActionsRefuseWhileConnected(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) { d.status = api.Status{State: "CONNECTED"} })
	ctrl := NewController(f, Options{})
	if err := ctrl.refreshStatus(context.Background()); err != nil {
		t.Fatalf("refreshStatus: %v", err)
	}
	for _, act := range []Action{
		{Name: ActionAccountsUse, Payload: []byte(`{"id":"a2"}`)},
		{Name: ActionAccountsRemove, Payload: []byte(`{"id":"a2"}`)},
	} {
		err := ctrl.Dispatch(act)
		if err == nil || !strings.Contains(err.Error(), "disconnect") {
			t.Fatalf("Dispatch(%s) = %v, want the client-side connected refusal", act.Name, err)
		}
	}
	if got := f.callLog(); slices.Contains(got, "accounts_use") || slices.Contains(got, "accounts_remove") {
		t.Fatalf("connected guard reached the daemon: %v", got)
	}
}

func TestControllerLoginObject(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.loginReplies["login_email"] = api.LoginState{Step: "password", Email: "user@example.com", Method: "email"}
		d.loginStartState = api.LoginState{
			Step: "browser", Method: "browser", URL: "https://example.test/login",
			LoginID: "login-2", VerificationReason: "browser sign-in",
		}
	})
	ctrl := NewController(f, Options{})

	if err := ctrl.Dispatch(Action{Name: ActionLoginEmail, Payload: []byte(`{"email":"user@example.com"}`)}); err != nil {
		t.Fatalf("Dispatch(login_email) = %v", err)
	}
	login := ctrl.Snapshot().Login
	if login == nil || login.Step != "password" || login.Method != "email" || login.Email != "user@example.com" {
		t.Fatalf("password login = %+v", login)
	}

	if err := ctrl.Dispatch(Action{Name: ActionLoginStart, Payload: []byte(`{"method":"browser"}`)}); err != nil {
		t.Fatalf("Dispatch(login_start) = %v", err)
	}
	login = ctrl.Snapshot().Login
	if login == nil || login.Step != "browser" || login.Method != "browser" ||
		login.URL != "https://example.test/login" || login.ID != "login-2" ||
		login.VerificationReason != "browser sign-in" {
		t.Fatalf("browser login = %+v", login)
	}
}

// TestAccountActionNamesAreAccepted pins the bridge vocabulary: the new
// actions are appended after the existing frozen list, and every one is
// accepted by DecodeAction's known set.
func TestAccountActionNamesAreAccepted(t *testing.T) {
	want := []string{
		ActionAccountsList,
		ActionAccountsAddStart,
		ActionAccountsAddStatus,
		ActionAccountsAddCancel,
		ActionAccountsRemove,
		ActionAccountsUse,
		ActionAccountsUpdate,
		ActionAccountsSetRotation,
		ActionLoginStart,
		ActionLoginStatus,
		ActionLoginCancel,
		ActionLoginComplete,
	}
	if len(actionNames) < len(want) {
		t.Fatalf("actionNames has %d entries, want at least %d", len(actionNames), len(want))
	}
	tail := actionNames[len(actionNames)-len(want):]
	if !slices.Equal(tail, want) {
		t.Fatalf("appended action names = %v, want %v", tail, want)
	}
	for _, name := range want {
		if _, ok := knownActions[name]; !ok {
			t.Errorf("knownActions is missing %q", name)
		}
	}
}
