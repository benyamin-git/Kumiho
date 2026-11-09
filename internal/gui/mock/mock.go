// Package mock provides an in-memory gui.Daemon that serves canned,
// deterministic daemon data. It backs the dev harness (cmd/kumiho-gui-web)
// and the GUI tests: every scenario reports fixed data, and the harness dev
// panel drives it through SetScenario, SetLatency, FailNext and EmitLog.
//
// The mock is a fixture, not a daemon: mutating calls (connect, logout,
// settings changes) are acknowledged with OK but never change what the mock
// reports. Only SetScenario changes the reported state, immediately and
// explicitly — there are no timers and no randomness. The one bit of call
// recording is LastConnect, kept because the mock's own tests use it.
package mock

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/gui"
	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/logging"
)

// eventsBuffer bounds the Events channel. A full channel drops its oldest
// queued event (see push), so producers never block or deadlock.
const eventsBuffer = 64

// pingRTTMS is the canned latency reported for the first, third, … ping host
// (the inventory's "42 ms" example); every second host times out (nil RTT),
// so both label styles exist deterministically.
const pingRTTMS = 42.0

// Ensure the mock stays in lockstep with the controller's Daemon interface.
var _ gui.Daemon = (*Mock)(nil)

// Mock implements gui.Daemon with canned data. Construct it with New.
type Mock struct {
	mu sync.Mutex

	scenario Scenario
	latency  time.Duration
	failNext error

	// Region state the harness can mutate through the real actions. The
	// token itself is never stored: only its set/clear flag is.
	settings     api.SettingsView
	favoriteRefs []api.LocationRef
	recentRefs   []api.LocationRef
	allowLocked  bool

	lastConnect api.ConnectPayload
	haveConnect bool
	// passExpires is set by Prepare so the handoff card and the dashboard
	// show a live countdown; zero means no prepared pass.
	passExpires int64

	// browserStep is the fixture's current browser flow: "" when none exists,
	// "browser" while waiting for the pasted redirect URL, "done" after a
	// successful LoginComplete and "cancelled" after LoginCancel. The fixture
	// has no timers: only those two verbs advance the flow, so the harness can
	// exercise the waiting card and its paste field. browserSeq makes each
	// flow's ID and URL unique, like the daemon's per-flow URL; the URL is
	// opened only once per flow by the UI.
	browserStep string
	browserSeq  int
	browserID   string
	browserURL  string

	events chan *ipc.Envelope
}

// New returns a mock in ScenarioDisconnected, with no latency and no injected
// failure. The first event appears when SetScenario changes the scenario.
func New() *Mock {
	return &Mock{
		scenario:     ScenarioDisconnected,
		settings:     settingsView(),
		favoriteRefs: locationFavorites(),
		recentRefs:   locationRecents(),
		events:       make(chan *ipc.Envelope, eventsBuffer),
	}
}

// SetScenario switches the reported daemon state immediately. Unknown
// scenarios fall back to ScenarioDisconnected. A switch that actually changes
// the scenario pushes the new status as an unsolicited status event, like the
// real daemon's broadcast on every state change, so a running controller sees
// the transition without polling; switching to the current scenario is a
// no-op. Connect and disconnect never switch the scenario themselves: a
// connecting→connected transition is an explicit later SetScenario call.
func (m *Mock) SetScenario(s Scenario) {
	s = s.normalize()
	m.mu.Lock()
	changed := s != m.scenario
	m.scenario = s
	m.mu.Unlock()
	if !changed {
		return
	}
	if env, err := ipc.NewEnvelope("", ipc.TypeStatus, statusFor(s)); err == nil {
		m.push(env)
	}
}

// SetLatency makes every daemon call take at least d before returning. Calls
// wait on the call's context, so a cancelled or expired context aborts the
// wait and returns the context's error without consuming a pending FailNext.
// A non-positive d disables the delay. The value is read per call and may
// change between calls.
func (m *Mock) SetLatency(d time.Duration) {
	m.mu.Lock()
	m.latency = d
	m.mu.Unlock()
}

// FailNext makes the next daemon call of any kind fail with exactly err —
// returned unchanged (never wrapped), so errors.Is and errors.As see it — and
// then clears. A failed call performs no other work and records nothing.
// FailNext(nil) clears a pending failure. Events is not a daemon call and is
// never affected.
func (m *Mock) FailNext(err error) {
	m.mu.Lock()
	m.failNext = err
	m.mu.Unlock()
}

// EmitLog pushes one log entry through Events as an ipc.TypeLog envelope,
// exactly as the daemon streams live entries (the entry is delivered
// unchanged). It never blocks: when the channel is full the oldest queued
// event is dropped to make room, keeping the newest entry.
func (m *Mock) EmitLog(e logging.Entry) {
	env, err := ipc.NewEnvelope("", ipc.TypeLog, e)
	if err != nil {
		return // logging.Entry is always marshalable
	}
	m.push(env)
}

// LastConnect returns the payload of the most recent successful Connect and
// whether one was recorded. A connect that failed through FailNext records
// nothing.
func (m *Mock) LastConnect() (api.ConnectPayload, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastConnect, m.haveConnect
}

// Events returns the mock's event channel: status pushes from SetScenario,
// live entries from EmitLog and ping results from Ping. Like the real
// client's, the channel never closes.
func (m *Mock) Events() <-chan *ipc.Envelope { return m.events }

// begin enforces the injections on every daemon call: it waits out the
// configured latency (honouring ctx) and consumes a pending FailNext.
func (m *Mock) begin(ctx context.Context) error {
	m.mu.Lock()
	d := m.latency
	m.mu.Unlock()
	if d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	m.mu.Lock()
	err := m.failNext
	m.failNext = nil
	m.mu.Unlock()
	return err
}

// push enqueues env without blocking. When the channel is full it drops the
// oldest queued event and retries once, keeping the newest event; a concurrent
// reader can drain the channel in between, in which case the retry simply
// stores the event into the freed space.
func (m *Mock) push(env *ipc.Envelope) {
	select {
	case m.events <- env:
		return
	default:
	}
	select {
	case <-m.events:
	default:
	}
	select {
	case m.events <- env:
	default:
	}
}

// currentScenario returns the reported scenario under the lock.
func (m *Mock) currentScenario() Scenario {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.scenario
}

// Status implements gui.Daemon.
func (m *Mock) Status(ctx context.Context) (api.Status, error) {
	if err := m.begin(ctx); err != nil {
		return api.Status{}, err
	}
	st := statusFor(m.currentScenario())
	m.mu.Lock()
	if m.passExpires > 0 {
		st.PassExpires = m.passExpires
	}
	m.mu.Unlock()
	return st, nil
}

// Connect implements gui.Daemon. It records the payload and acknowledges; it
// does not change the scenario.
func (m *Mock) Connect(ctx context.Context, p api.ConnectPayload) (api.Result, error) {
	if err := m.begin(ctx); err != nil {
		return api.Result{}, err
	}
	m.mu.Lock()
	m.lastConnect = p
	m.haveConnect = true
	m.mu.Unlock()
	return api.Result{OK: true}, nil
}

// Disconnect implements gui.Daemon. It acknowledges; it does not change the
// scenario.
func (m *Mock) Disconnect(ctx context.Context) (api.Result, error) {
	if err := m.begin(ctx); err != nil {
		return api.Result{}, err
	}
	return api.Result{OK: true}, nil
}

// SelectLocation implements gui.Daemon. It acknowledges; it does not change
// the reported status.
func (m *Mock) SelectLocation(ctx context.Context, _ api.SelectLocationPayload) (api.Result, error) {
	if err := m.begin(ctx); err != nil {
		return api.Result{}, err
	}
	return api.Result{OK: true}, nil
}

// Locations implements gui.Daemon. The canned server list is the same in
// every scenario; refresh is ignored.
func (m *Mock) Locations(ctx context.Context, _ bool) (api.LocationsResult, error) {
	if err := m.begin(ctx); err != nil {
		return api.LocationsResult{}, err
	}
	m.mu.Lock()
	favorites := append([]api.LocationRef(nil), m.favoriteRefs...)
	recents := append([]api.LocationRef(nil), m.recentRefs...)
	allowLocked := m.allowLocked
	m.mu.Unlock()
	return api.LocationsResult{
		Countries:   countries(),
		Favorites:   favorites,
		Recents:     recents,
		Latency:     locationLatency(),
		AllowLocked: allowLocked,
	}, nil
}

// FavoriteLocation implements gui.Daemon. The fixture stores the ref so the
// next Locations reply is authoritative after the optimistic star.
func (m *Mock) FavoriteLocation(ctx context.Context, p api.FavoriteLocationPayload) (api.Result, error) {
	if err := m.begin(ctx); err != nil {
		return api.Result{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	ref := api.LocationRef{CountryCode: p.CountryCode, CityCode: p.CityCode, Server: p.Server}
	out := m.favoriteRefs[:0]
	existed := false
	for _, existing := range m.favoriteRefs {
		if existing.Server == p.Server {
			existed = true
			if p.Favorite {
				out = append(out, ref)
			}
			continue
		}
		out = append(out, existing)
	}
	if p.Favorite && !existed {
		out = append(out, ref)
	}
	m.favoriteRefs = out
	return api.Result{OK: true}, nil
}

// ClearRecents implements gui.Daemon.
func (m *Mock) ClearRecents(ctx context.Context) (api.Result, error) {
	if err := m.begin(ctx); err != nil {
		return api.Result{}, err
	}
	m.mu.Lock()
	m.recentRefs = nil
	m.mu.Unlock()
	return api.Result{OK: true}, nil
}

// Ping implements gui.Daemon: it acknowledges and streams one ping_result
// event per requested host (see pingRTTMS for the deterministic RTT rule),
// then the ping_done event that ends the run.
func (m *Mock) Ping(ctx context.Context, hosts []string) (api.Result, error) {
	if err := m.begin(ctx); err != nil {
		return api.Result{}, err
	}
	answered := 0
	for i, host := range hosts {
		var rtt *float64
		if i%2 == 0 {
			v := pingRTTMS
			rtt = &v
			answered++
		}
		env, err := ipc.NewEnvelope("", ipc.TypePingResult, api.PingResult{Host: host, RTTMS: rtt})
		if err != nil {
			continue
		}
		m.push(env)
	}
	if env, err := ipc.NewEnvelope("", ipc.TypePingDone, api.PingDone{
		Total: len(hosts), Answered: answered,
	}); err == nil {
		m.push(env)
	}
	return api.Result{OK: true}, nil
}

// Settings implements gui.Daemon.
func (m *Mock) Settings(ctx context.Context) (api.SettingsView, error) {
	if err := m.begin(ctx); err != nil {
		return api.SettingsView{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return copySettingsView(m.settings), nil
}

// SetSetting implements gui.Daemon. Region keys are mutable so the harness
// can exercise the controls; every other canned setting stays fixed.
func (m *Mock) SetSetting(ctx context.Context, p api.SettingsSetPayload) (api.Result, error) {
	if err := m.begin(ctx); err != nil {
		return api.Result{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch p.Key {
	case "allow_locked":
		on := p.Value == "on" || p.Value == "true"
		if p.Reset {
			on = false
		}
		m.settings.AllowLocked = on
		m.allowLocked = on
	case "relay_url":
		m.settings.RelayURL = p.Value
		if p.Reset {
			m.settings.RelayURL = ""
		}
	case "relay_token":
		m.settings.RelayTokenSet = !p.Reset && p.Value != ""
	}
	return api.Result{OK: true}, nil
}

// RelayTest implements gui.Daemon with a deterministic fast success.
func (m *Mock) RelayTest(ctx context.Context) (api.RelayTestResult, error) {
	if err := m.begin(ctx); err != nil {
		return api.RelayTestResult{}, err
	}
	ms := 42.0
	return api.RelayTestResult{OK: true, Status: 200, LatencyMS: &ms}, nil
}

// RegionCheck implements gui.Daemon with a deterministic blocked fixture:
// the VPN and account API hosts answer 451, the browser page is blocked, and
// the relay is configured only after the harness saves URL and token.
func (m *Mock) RegionCheck(ctx context.Context) (api.RegionCheckResult, error) {
	if err := m.begin(ctx); err != nil {
		return api.RegionCheckResult{}, err
	}
	m.mu.Lock()
	relay := m.settings.RelayURL != "" && m.settings.RelayTokenSet
	m.mu.Unlock()
	hosts := []api.RegionHostStatus{
		{Host: "api.accounts.firefox.com", Status: api.RegionBlocked, Detail: "HTTP 451"},
		{Host: "oauth.accounts.firefox.com", Status: api.RegionReachable, Detail: "HTTP 200"},
		{Host: "profile.accounts.firefox.com", Status: api.RegionReachable, Detail: "HTTP 200"},
		{Host: "vpn.mozilla.org", Status: api.RegionBlocked, Detail: "HTTP 451"},
		{Host: "firefox.settings.services.mozilla.com", Status: api.RegionReachable, Detail: "HTTP 200"},
		{Host: "accounts.firefox.com", Status: api.RegionBlocked, Detail: "HTTP 451"},
	}
	return api.RegionCheckResult{
		Hosts:           hosts,
		Blocked:         []string{"api.accounts.firefox.com", "vpn.mozilla.org", "accounts.firefox.com"},
		BrowserBlocked:  true,
		RelayConfigured: relay,
	}, nil
}

// Prepare implements gui.Daemon: it sets a ten-minute pass and returns the
// no-token result the guide's handoff card renders.
func (m *Mock) Prepare(ctx context.Context) (api.PrepareResult, error) {
	if err := m.begin(ctx); err != nil {
		return api.PrepareResult{}, err
	}
	expires := mockEpoch.Add(10 * time.Minute).Unix()
	m.mu.Lock()
	m.passExpires = expires
	q := quota(quotaRemaining)
	m.mu.Unlock()
	return api.PrepareResult{
		Email:               demoEmail,
		ExpiresAt:           expires,
		Quota:               q,
		Locations:           94,
		Servers:             412,
		ServerListRefreshed: true,
		ProfileRefreshed:    true,
	}, nil
}

// Account implements gui.Daemon: the ScenarioError scenario reports a failed
// entitlement lookup (see accountFor).
func (m *Mock) Account(ctx context.Context) (api.AccountInfo, error) {
	if err := m.begin(ctx); err != nil {
		return api.AccountInfo{}, err
	}
	return accountFor(m.currentScenario()), nil
}

// AccountsList implements gui.Daemon: the canned account rows for the
// current scenario (see accountsFor).
func (m *Mock) AccountsList(ctx context.Context) (api.AccountsView, error) {
	if err := m.begin(ctx); err != nil {
		return api.AccountsView{}, err
	}
	return accountsFor(m.currentScenario()), nil
}

// AccountsAddStart implements gui.Daemon. Browser sign-in reports a browser
// step with a deterministic browser-shaped URL; password sign-in asks for
// the email first, then the password, exactly like the real daemon's flow.
func (m *Mock) AccountsAddStart(ctx context.Context, p api.LoginStartPayload) (api.LoginState, error) {
	if err := m.begin(ctx); err != nil {
		return api.LoginState{}, err
	}
	return m.startLogin(p), nil
}

// AccountsAddStatus implements gui.Daemon: the browser flow stays on the
// waiting step until LoginComplete (or Cancel) advances it, so the harness can
// exercise the paste field.
func (m *Mock) AccountsAddStatus(ctx context.Context, _ api.LoginStatusPayload) (api.LoginState, error) {
	if err := m.begin(ctx); err != nil {
		return api.LoginState{}, err
	}
	return m.browserStatus(), nil
}

// AccountsAddCancel implements gui.Daemon: it ends the browser flow as
// cancelled and acknowledges.
func (m *Mock) AccountsAddCancel(ctx context.Context, _ api.LoginCancelPayload) (api.Result, error) {
	if err := m.begin(ctx); err != nil {
		return api.Result{}, err
	}
	m.mu.Lock()
	if m.browserStep == "browser" {
		m.browserStep = "cancelled"
	}
	m.mu.Unlock()
	return api.Result{OK: true}, nil
}

// AccountsRemove implements gui.Daemon. It acknowledges; the canned list
// does not change (switch scenarios for different reported state).
func (m *Mock) AccountsRemove(ctx context.Context, _ api.AccountsRemovePayload) (api.Result, error) {
	if err := m.begin(ctx); err != nil {
		return api.Result{}, err
	}
	return api.Result{OK: true}, nil
}

// AccountsUse implements gui.Daemon. It acknowledges; the canned list keeps
// its active row.
func (m *Mock) AccountsUse(ctx context.Context, _ api.AccountsUsePayload) (api.Result, error) {
	if err := m.begin(ctx); err != nil {
		return api.Result{}, err
	}
	return api.Result{OK: true}, nil
}

// AccountsUpdate implements gui.Daemon. It acknowledges; the canned list
// keeps its labels.
func (m *Mock) AccountsUpdate(ctx context.Context, _ api.AccountsUpdatePayload) (api.Result, error) {
	if err := m.begin(ctx); err != nil {
		return api.Result{}, err
	}
	return api.Result{OK: true}, nil
}

// AccountsSetRotation implements gui.Daemon. It acknowledges; the canned
// rotation settings do not change.
func (m *Mock) AccountsSetRotation(ctx context.Context, _ api.AccountsRotationPayload) (api.Result, error) {
	if err := m.begin(ctx); err != nil {
		return api.Result{}, err
	}
	return api.Result{OK: true}, nil
}

// LogsSnapshot implements gui.Daemon: the canned entries at or above level.
// A level of "" means everything; an unknown level is a bad_request, exactly
// like the daemon.
func (m *Mock) LogsSnapshot(ctx context.Context, level string) (api.LogSnapshot, error) {
	if err := m.begin(ctx); err != nil {
		return api.LogSnapshot{}, err
	}
	minLevel, err := parseMinLevel(level, logging.Debug)
	if err != nil {
		return api.LogSnapshot{}, err
	}
	entries := logEntries()
	out := entries[:0]
	for _, e := range entries {
		if e.Level >= minLevel {
			out = append(out, e)
		}
	}
	return api.LogSnapshot{Entries: out}, nil
}

// LogsSubscribe implements gui.Daemon. It validates the level like the
// daemon ("" means info) and acknowledges. The mock keeps no subscription
// state: EmitLog delivers whatever entry it is given.
func (m *Mock) LogsSubscribe(ctx context.Context, level string) (api.Result, error) {
	if err := m.begin(ctx); err != nil {
		return api.Result{}, err
	}
	if _, err := parseMinLevel(level, logging.Info); err != nil {
		return api.Result{}, err
	}
	return api.Result{OK: true}, nil
}

// LogsUnsubscribe implements gui.Daemon. It acknowledges.
func (m *Mock) LogsUnsubscribe(ctx context.Context) (api.Result, error) {
	if err := m.begin(ctx); err != nil {
		return api.Result{}, err
	}
	return api.Result{OK: true}, nil
}

// LogsClear implements gui.Daemon. It acknowledges; the canned entries are
// not cleared (the controller's own buffer empties).
func (m *Mock) LogsClear(ctx context.Context) (api.Result, error) {
	if err := m.begin(ctx); err != nil {
		return api.Result{}, err
	}
	return api.Result{OK: true}, nil
}

// LoginEmail implements gui.Daemon: it asks for the password next, or for the
// emailed 2FA code when the address starts with "2fa" (e.g. 2fa@example.com),
// so the harness can drive both wizard paths deterministically. A blank email
// resets the fixture's login machine to the email step with no error — the
// harness's deterministic reset between capture states (the fixture keeps its
// last reply, unlike a daemon whose pending login expires).
func (m *Mock) LoginEmail(ctx context.Context, p api.LoginEmailPayload) (api.LoginState, error) {
	if err := m.begin(ctx); err != nil {
		return api.LoginState{}, err
	}
	email := strings.TrimSpace(p.Email)
	if email == "" {
		return api.LoginState{Step: "email", Method: "email"}, nil
	}
	if strings.HasPrefix(strings.ToLower(email), "2fa") {
		return api.LoginState{Step: "2fa", Email: email, Method: "email", VerificationMethod: "email-otp"}, nil
	}
	return api.LoginState{Step: "password", Email: email, Method: "email"}, nil
}

// LoginPassword implements gui.Daemon: the flow signs in immediately.
func (m *Mock) LoginPassword(ctx context.Context, _ api.LoginPasswordPayload) (api.LoginState, error) {
	if err := m.begin(ctx); err != nil {
		return api.LoginState{}, err
	}
	return api.LoginState{Step: "done"}, nil
}

// Login2FA implements gui.Daemon: the flow signs in immediately.
func (m *Mock) Login2FA(ctx context.Context, _ api.Login2FAPayload) (api.LoginState, error) {
	if err := m.begin(ctx); err != nil {
		return api.LoginState{}, err
	}
	return api.LoginState{Step: "done"}, nil
}

// LoginStart implements gui.Daemon: it starts the browser or password flow
// with the same deterministic replies as AccountsAddStart.
func (m *Mock) LoginStart(ctx context.Context, p api.LoginStartPayload) (api.LoginState, error) {
	if err := m.begin(ctx); err != nil {
		return api.LoginState{}, err
	}
	return m.startLogin(p), nil
}

// LoginStatus implements gui.Daemon: the browser flow stays on the waiting
// step until LoginComplete (or Cancel) advances it.
func (m *Mock) LoginStatus(ctx context.Context, _ api.LoginStatusPayload) (api.LoginState, error) {
	if err := m.begin(ctx); err != nil {
		return api.LoginState{}, err
	}
	return m.browserStatus(), nil
}

// LoginCancel implements gui.Daemon: it ends the browser flow as cancelled
// and acknowledges.
func (m *Mock) LoginCancel(ctx context.Context, _ api.LoginCancelPayload) (api.Result, error) {
	if err := m.begin(ctx); err != nil {
		return api.Result{}, err
	}
	m.mu.Lock()
	if m.browserStep == "browser" {
		m.browserStep = "cancelled"
	}
	m.mu.Unlock()
	return api.Result{OK: true}, nil
}

// LoginComplete implements gui.Daemon: a pasted redirect URL finishes the
// waiting browser flow, which the next status poll reports as done. A blank
// redirect is refused like the daemon; an unknown or inactive flow is a
// bad_request.
func (m *Mock) LoginComplete(ctx context.Context, p api.LoginCompletePayload) (api.Result, error) {
	if err := m.begin(ctx); err != nil {
		return api.Result{}, err
	}
	if strings.TrimSpace(p.RedirectURL) == "" {
		return api.Result{}, &api.RemoteError{
			Code: api.CodeBadRequest, Message: "paste the complete redirect URL from the browser",
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.browserStep != "browser" || (p.LoginID != "" && p.LoginID != m.browserID) {
		return api.Result{}, &api.RemoteError{
			Code: api.CodeBadRequest, Message: "no browser sign-in in progress",
		}
	}
	m.browserStep = "done"
	return api.Result{OK: true}, nil
}

// browserStatus reports the fixture's current browser flow.
func (m *Mock) browserStatus() api.LoginState {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch m.browserStep {
	case "browser":
		return api.LoginState{
			Step:    "browser",
			Method:  "browser",
			URL:     m.browserURL,
			LoginID: m.browserID,
		}
	case "done":
		return api.LoginState{Step: "done", Email: demoEmail}
	case "cancelled":
		return api.LoginState{Step: "cancelled"}
	default:
		return api.LoginState{Step: "error", Error: "no browser sign-in in progress"}
	}
}

// startLogin resets the fixture's browser progress and gives each browser
// flow a unique ID and URL (the UI opens a URL once per flow).
func (m *Mock) startLogin(p api.LoginStartPayload) api.LoginState {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := loginStartState(p)
	m.browserStep = ""
	if st.Step == "browser" {
		m.browserSeq++
		m.browserID = fmt.Sprintf("mock-browser-%d", m.browserSeq)
		m.browserURL = fmt.Sprintf("https://example.test/mozilla-account/signin?flow=%d", m.browserSeq)
		m.browserStep = "browser"
		st.LoginID = m.browserID
		st.URL = m.browserURL
	}
	return st
}

// loginStartState is the deterministic sign-in machine shared by the login
// and account-add verbs.
func loginStartState(p api.LoginStartPayload) api.LoginState {
	email := strings.TrimSpace(p.Email)
	switch strings.ToLower(strings.TrimSpace(p.Method)) {
	case "browser":
		return api.LoginState{
			Step:    "browser",
			Method:  "browser",
			URL:     "https://example.test/mozilla-account/signin",
			LoginID: "mock-browser-1",
		}
	case "", "password":
		if email == "" {
			return api.LoginState{Step: "email", Method: "email"}
		}
		return api.LoginState{Step: "password", Email: email, Method: "email"}
	default:
		return api.LoginState{Step: "error", Error: "unknown sign-in method"}
	}
}

// Logout implements gui.Daemon. It acknowledges; the reported session does not
// change (switch scenarios for different reported state).
func (m *Mock) Logout(ctx context.Context) (api.Result, error) {
	if err := m.begin(ctx); err != nil {
		return api.Result{}, err
	}
	return api.Result{OK: true}, nil
}

// parseMinLevel parses a logs level; an empty string means def. An invalid
// name is a bad_request RemoteError, mirroring the daemon's reply.
func parseMinLevel(level string, def logging.Level) (logging.Level, error) {
	if level == "" {
		return def, nil
	}
	lvl, err := logging.ParseLevel(level)
	if err != nil {
		return 0, &api.RemoteError{Code: api.CodeBadRequest, Message: err.Error()}
	}
	return lvl, nil
}

func copySettingsView(v api.SettingsView) api.SettingsView {
	v.ExcludeCIDRs = append([]string(nil), v.ExcludeCIDRs...)
	v.Overridden = append([]string(nil), v.Overridden...)
	return v
}
