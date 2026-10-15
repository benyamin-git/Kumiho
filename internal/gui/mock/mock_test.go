package mock

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/gui"
	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

func entry(msg string) logging.Entry {
	return logging.Entry{Time: time.Unix(1700000000, 0).UTC(), Level: logging.Info, Tag: "test", Msg: msg}
}

func readEvent(t *testing.T, m *Mock) *ipc.Envelope {
	t.Helper()
	select {
	case env := <-m.Events():
		return env
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for an event")
		return nil
	}
}

func assertNoEvent(t *testing.T, m *Mock) {
	t.Helper()
	select {
	case env := <-m.Events():
		t.Fatalf("unexpected event of type %q", env.Type)
	default:
	}
}

func isBadRequest(t *testing.T, err error) {
	t.Helper()
	var remote *api.RemoteError
	if !errors.As(err, &remote) || remote.Code != api.CodeBadRequest {
		t.Fatalf("err = %v, want a bad_request RemoteError", err)
	}
}

// TestScenarioStatuses pins the canned status of every scenario, its
// scenario-specific facts and call-to-call determinism.
func TestScenarioStatuses(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		scenario  Scenario
		state     string
		connected bool
	}{
		{ScenarioDisconnected, "IDLE", false},
		{ScenarioConnected, "CONNECTED", true},
		{ScenarioConnecting, "CONNECTING", false},
		{ScenarioError, "FATAL", false},
	}
	for _, tc := range cases {
		t.Run(string(tc.scenario), func(t *testing.T) {
			m := New()
			m.SetScenario(tc.scenario)
			st1, err := m.Status(ctx)
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if st1.State != tc.state {
				t.Fatalf("state = %q, want %q", st1.State, tc.state)
			}
			if !st1.Authenticated || st1.Email != demoEmail {
				t.Fatalf("authenticated/email = %v/%q", st1.Authenticated, st1.Email)
			}
			if !st1.KillSwitch || st1.Autoconnect {
				t.Fatalf("kill switch/autoconnect = %v/%v, want true/false", st1.KillSwitch, st1.Autoconnect)
			}
			if st1.Location != demoLocation || st1.Server != demoServer {
				t.Fatalf("location/server = %q/%q", st1.Location, st1.Server)
			}
			if st1.Quota == nil || st1.Quota.Limit == nil || *st1.Quota.Limit != quotaLimit {
				t.Fatalf("quota = %+v", st1.Quota)
			}
			if tc.connected {
				if st1.Since <= 0 || st1.PassExpires <= 0 || st1.ExitIP != demoExitIP || st1.ExitCountry != demoExitCountry {
					t.Fatalf("connected facts = since %d pass %d exit %q (%q)", st1.Since, st1.PassExpires, st1.ExitIP, st1.ExitCountry)
				}
				if st1.Totals == nil || st1.Rates == nil {
					t.Fatal("connected status lacks totals/rates")
				}
				if *st1.Quota.Remaining != quotaRemaining {
					t.Fatalf("remaining = %d, want %d", *st1.Quota.Remaining, quotaRemaining)
				}
			} else {
				if st1.ExitIP != "" || st1.Totals != nil || st1.Rates != nil {
					t.Fatalf("non-connected status carries session facts: %+v", st1)
				}
			}
			if tc.scenario == ScenarioError && *st1.Quota.Remaining != errorQuotaRemaining {
				t.Fatalf("error quota remaining = %d, want %d", *st1.Quota.Remaining, errorQuotaRemaining)
			}
			st2, err := m.Status(ctx)
			if err != nil {
				t.Fatalf("Status (second call): %v", err)
			}
			if !reflect.DeepEqual(st1, st2) {
				t.Fatalf("repeated calls differ:\n%+v\n%+v", st1, st2)
			}
		})
	}
}

func TestSetScenarioNormalizesUnknown(t *testing.T) {
	m := New()
	m.SetScenario(Scenario("bogus"))
	st, err := m.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != "IDLE" {
		t.Fatalf("state = %q, want IDLE for an unknown scenario", st.State)
	}
}

func TestSetScenarioPushesStatusEvent(t *testing.T) {
	m := New()
	// The initial scenario emits nothing; a no-op switch is a no-op.
	assertNoEvent(t, m)
	m.SetScenario(ScenarioDisconnected)
	assertNoEvent(t, m)

	m.SetScenario(ScenarioConnected)
	env := readEvent(t, m)
	if env.Type != ipc.TypeStatus || env.ID != "" || env.V != ipc.ProtocolVersion {
		t.Fatalf("event = %+v, want an unsolicited status envelope", env)
	}
	var st api.Status
	if err := env.DecodePayload(&st); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if st.State != "CONNECTED" {
		t.Fatalf("pushed state = %q, want CONNECTED", st.State)
	}

	m.SetScenario(ScenarioConnected)
	assertNoEvent(t, m)
}

// TestCannedDataIsFreshPerCall mutates every reply and proves the next call
// still returns pristine canned data (no shared pointers or slices).
func TestCannedDataIsFreshPerCall(t *testing.T) {
	ctx := context.Background()
	m := New()
	m.SetScenario(ScenarioConnected)

	st, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	*st.Quota.Remaining = 0
	st.Totals.Up = 1
	st.Rates.Down = 0
	st2, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("Status (second call): %v", err)
	}
	if *st2.Quota.Remaining != quotaRemaining || st2.Totals.Up != 12<<20 || st2.Rates.Down == 0 {
		t.Fatalf("mutations leaked into the canned status: %+v", st2)
	}

	ac, err := m.Account(ctx)
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	*ac.QuotaRemaining = 1
	ac2, err := m.Account(ctx)
	if err != nil {
		t.Fatalf("Account (second call): %v", err)
	}
	if *ac2.QuotaRemaining != quotaRemaining {
		t.Fatalf("mutation leaked into the canned account: %+v", ac2)
	}

	sv, err := m.Settings(ctx)
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	sv.Overridden[0] = "bogus"
	sv2, err := m.Settings(ctx)
	if err != nil {
		t.Fatalf("Settings (second call): %v", err)
	}
	if sv2.Overridden[0] != "kill_switch" {
		t.Fatalf("mutation leaked into the canned settings: %+v", sv2.Overridden)
	}

	loc, err := m.Locations(ctx, false)
	if err != nil {
		t.Fatalf("Locations: %v", err)
	}
	loc.Countries[0].Cities[0].Servers[0].Protocols[0].Name = "bogus"
	loc2, err := m.Locations(ctx, false)
	if err != nil {
		t.Fatalf("Locations (second call): %v", err)
	}
	if got := loc2.Countries[0].Cities[0].Servers[0].Protocols[0].Name; got != "connect" {
		t.Fatalf("mutation leaked into the canned locations: %q", got)
	}

	snap, err := m.LogsSnapshot(ctx, "")
	if err != nil {
		t.Fatalf("LogsSnapshot: %v", err)
	}
	snap.Entries[0].Msg = "bogus"
	snap2, err := m.LogsSnapshot(ctx, "")
	if err != nil {
		t.Fatalf("LogsSnapshot (second call): %v", err)
	}
	if snap2.Entries[0].Msg == "bogus" {
		t.Fatal("mutation leaked into the canned log entries")
	}
}

func TestLocationsShape(t *testing.T) {
	ctx := context.Background()
	m := New()
	res, err := m.Locations(ctx, false)
	if err != nil {
		t.Fatalf("Locations: %v", err)
	}
	if len(res.Countries) != 4 || res.Countries[0].Code != serverlist.RecommendedCode {
		t.Fatalf("countries = %+v, want REC first of 4", res.Countries)
	}
	for i := 2; i < len(res.Countries); i++ {
		if res.Countries[i-1].Name >= res.Countries[i].Name {
			t.Fatalf("countries not sorted by name after REC: %q >= %q", res.Countries[i-1].Name, res.Countries[i].Name)
		}
	}
	servers, quarantined := 0, 0
	for _, c := range res.Countries {
		if len(c.Cities) == 0 {
			t.Fatalf("country %q has no cities", c.Code)
		}
		for _, city := range c.Cities {
			if len(city.Servers) == 0 {
				t.Fatalf("city %q has no servers", city.Code)
			}
			for _, s := range city.Servers {
				if _, _, ok := s.ConnectTarget(); !ok {
					t.Fatalf("server %q has no connect target", s.Hostname)
				}
				servers++
				if s.Quarantined {
					quarantined++
				}
			}
		}
	}
	if servers != 8 || quarantined != 1 {
		t.Fatalf("servers = %d (quarantined %d), want 8 (1)", servers, quarantined)
	}
	res2, err := m.Locations(ctx, true)
	if err != nil {
		t.Fatalf("Locations (refresh): %v", err)
	}
	if !reflect.DeepEqual(res, res2) {
		t.Fatal("locations differ between calls")
	}
}

func TestSettingsViewIsCanned(t *testing.T) {
	ctx := context.Background()
	m := New()
	view, err := m.Settings(ctx)
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if view.SocksPort != 1080 || view.MTU != 1500 || view.LogLevel != "info" {
		t.Fatalf("settings = %+v", view)
	}
	if view.KillSwitch != "on" || !view.KillSwitchEffective || view.Autoconnect != "off" {
		t.Fatalf("kill switch/autoconnect = %+v", view)
	}
	if len(view.Overridden) != 1 || view.Overridden[0] != "kill_switch" {
		t.Fatalf("overridden = %v, want [kill_switch]", view.Overridden)
	}
	if _, err := m.SetSetting(ctx, api.SettingsSetPayload{Key: "log_level", Value: "warn"}); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	view2, err := m.Settings(ctx)
	if err != nil {
		t.Fatalf("Settings (after set): %v", err)
	}
	if view2.LogLevel != "info" {
		t.Fatalf("canned settings changed after SetSetting: %q", view2.LogLevel)
	}
}

func TestAccountPerScenario(t *testing.T) {
	ctx := context.Background()
	for _, scenario := range []Scenario{ScenarioDisconnected, ScenarioConnected, ScenarioConnecting, ScenarioError} {
		t.Run(string(scenario), func(t *testing.T) {
			m := New()
			m.SetScenario(scenario)
			info, err := m.Account(ctx)
			if err != nil {
				t.Fatalf("Account: %v", err)
			}
			if info.Email != demoEmail || info.TokenExpires <= 0 {
				t.Fatalf("local session facts = %+v", info)
			}
			if scenario == ScenarioError {
				if info.Error != "guardian: HTTP 503" || info.UID != "" || info.MaxBytes != nil {
					t.Fatalf("error account = %+v, want the lookup failure only", info)
				}
				return
			}
			if info.Error != "" || !info.Subscribed || info.UID != demoUID {
				t.Fatalf("account = %+v, want an entitlement", info)
			}
			if info.MaxBytes == nil || *info.MaxBytes != quotaLimit || info.QuotaRemaining == nil || *info.QuotaRemaining != quotaRemaining {
				t.Fatalf("entitlement = %+v", info)
			}
		})
	}
}

// TestFailNextAppliesToEveryCall checks the injection across the whole
// daemon surface: exactly one call fails with the sentinel unchanged, the
// next call succeeds.
func TestFailNextAppliesToEveryCall(t *testing.T) {
	ctx := context.Background()
	calls := []struct {
		name string
		call func(*Mock) error
	}{
		{"status", func(m *Mock) error { _, err := m.Status(ctx); return err }},
		{"connect", func(m *Mock) error { _, err := m.Connect(ctx, api.ConnectPayload{LocationCode: "DE"}); return err }},
		{"disconnect", func(m *Mock) error { _, err := m.Disconnect(ctx); return err }},
		{"select_location", func(m *Mock) error { _, err := m.SelectLocation(ctx, api.SelectLocationPayload{}); return err }},
		{"locations", func(m *Mock) error { _, err := m.Locations(ctx, false); return err }},
		{"ping", func(m *Mock) error { _, err := m.Ping(ctx, []string{"de1.edge.example:443"}); return err }},
		{"settings", func(m *Mock) error { _, err := m.Settings(ctx); return err }},
		{"set_setting", func(m *Mock) error { _, err := m.SetSetting(ctx, api.SettingsSetPayload{}); return err }},
		{"account", func(m *Mock) error { _, err := m.Account(ctx); return err }},
		{"logs_snapshot", func(m *Mock) error { _, err := m.LogsSnapshot(ctx, ""); return err }},
		{"logs_subscribe", func(m *Mock) error { _, err := m.LogsSubscribe(ctx, ""); return err }},
		{"logs_unsubscribe", func(m *Mock) error { _, err := m.LogsUnsubscribe(ctx); return err }},
		{"logs_clear", func(m *Mock) error { _, err := m.LogsClear(ctx); return err }},
		{"login_email", func(m *Mock) error { _, err := m.LoginEmail(ctx, api.LoginEmailPayload{}); return err }},
		{"login_password", func(m *Mock) error { _, err := m.LoginPassword(ctx, api.LoginPasswordPayload{}); return err }},
		{"login_2fa", func(m *Mock) error { _, err := m.Login2FA(ctx, api.Login2FAPayload{}); return err }},
		{"logout", func(m *Mock) error { _, err := m.Logout(ctx); return err }},
	}
	sentinel := errors.New("mock failure")
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			m := New()
			m.FailNext(sentinel)
			err := tc.call(m)
			if err != sentinel {
				t.Fatalf("failed call = %v, want the sentinel returned unchanged", err)
			}
			if err := tc.call(m); err != nil {
				t.Fatalf("call after the injection = %v, want success", err)
			}
		})
	}
}

func TestFailNextClearsWithNil(t *testing.T) {
	m := New()
	m.FailNext(errors.New("boom"))
	m.FailNext(nil)
	if _, err := m.Status(context.Background()); err != nil {
		t.Fatalf("Status = %v, want success after FailNext(nil)", err)
	}
}

func TestFailNextFailedCallRecordsNothing(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("mock failure")
	m := New()
	m.FailNext(sentinel)
	if _, err := m.Connect(ctx, api.ConnectPayload{LocationCode: "DE"}); err != sentinel {
		t.Fatalf("Connect = %v, want the sentinel", err)
	}
	if _, ok := m.LastConnect(); ok {
		t.Fatal("a failed connect must not be recorded")
	}
	m.FailNext(sentinel)
	if _, err := m.Ping(ctx, []string{"de1.edge.example:443"}); err != sentinel {
		t.Fatalf("Ping = %v, want the sentinel", err)
	}
	assertNoEvent(t, m)

	if _, err := m.Connect(ctx, api.ConnectPayload{LocationCode: "DE"}); err != nil {
		t.Fatalf("Connect = %v, want success", err)
	}
	got, ok := m.LastConnect()
	if !ok || got.LocationCode != "DE" {
		t.Fatalf("LastConnect = %+v, %v", got, ok)
	}
}

func TestLatencyHonoursContext(t *testing.T) {
	ctx := context.Background()
	m := New()

	m.SetLatency(40 * time.Millisecond)
	start := time.Now()
	if _, err := m.Status(ctx); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Fatalf("call returned after %v, want >= the injected latency", elapsed)
	}

	// A cancelled context aborts the wait and must not consume a pending
	// failure: the call never reached the daemon.
	sentinel := errors.New("mock failure")
	m.FailNext(sentinel)
	m.SetLatency(time.Hour)
	cctx, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
	defer cancel()
	if _, err := m.Status(cctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Status = %v, want context.DeadlineExceeded", err)
	}
	m.SetLatency(0)
	if _, err := m.Status(ctx); err != sentinel {
		t.Fatalf("Status = %v, want the still-pending sentinel", err)
	}
	if _, err := m.Status(ctx); err != nil {
		t.Fatalf("Status = %v, want success", err)
	}

	// Zero disables the delay.
	m.SetLatency(time.Hour)
	m.SetLatency(0)
	start = time.Now()
	if _, err := m.Status(ctx); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("zero-latency call took %v", elapsed)
	}
}

func TestEmitLogDelivers(t *testing.T) {
	m := New()
	e := logging.Entry{Time: time.Unix(1700000000, 0).UTC(), Level: logging.Warn, Tag: "tunnel", Msg: "live entry"}
	m.EmitLog(e)
	env := readEvent(t, m)
	if env.Type != ipc.TypeLog || env.ID != "" || env.V != ipc.ProtocolVersion {
		t.Fatalf("event = %+v, want an unsolicited log envelope", env)
	}
	var got logging.Entry
	if err := env.DecodePayload(&got); err != nil {
		t.Fatalf("decode log entry: %v", err)
	}
	if !got.Time.Equal(e.Time) || got.Level != e.Level || got.Tag != e.Tag || got.Msg != e.Msg {
		t.Fatalf("entry = %+v, want %+v", got, e)
	}
}

// TestEmitLogDropsOldestAndNeverBlocks pins the full-channel policy: the
// oldest queued event is dropped, the newest is kept, and bursts with no
// reader neither block nor grow the buffer.
func TestEmitLogDropsOldestAndNeverBlocks(t *testing.T) {
	m := New()
	for i := 0; i < eventsBuffer; i++ {
		m.EmitLog(entry("queued-" + strconv.Itoa(i)))
	}
	m.EmitLog(entry("newest"))
	if got := len(m.Events()); got != eventsBuffer {
		t.Fatalf("buffered %d events, want %d", got, eventsBuffer)
	}
	var last logging.Entry
	for i := 0; i < eventsBuffer; i++ {
		var e logging.Entry
		if err := readEvent(t, m).DecodePayload(&e); err != nil {
			t.Fatalf("decode log entry: %v", err)
		}
		if i == 0 && e.Msg != "queued-1" {
			t.Fatalf("oldest surviving entry = %q, want queued-1 (queued-0 dropped)", e.Msg)
		}
		last = e
	}
	if last.Msg != "newest" {
		t.Fatalf("newest entry = %q, want the overflow entry to survive", last.Msg)
	}

	for i := 0; i < 3*eventsBuffer; i++ {
		m.EmitLog(entry("burst"))
	}
	if got := len(m.Events()); got != eventsBuffer {
		t.Fatalf("buffered %d events after a burst, want %d", got, eventsBuffer)
	}
}

func TestPingStreamsDeterministicResults(t *testing.T) {
	m := New()
	hosts := []string{"rec1.edge.example:443", "de1.edge.example:443", "jp1.edge.example:443"}
	if _, err := m.Ping(context.Background(), hosts); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	for i, host := range hosts {
		env := readEvent(t, m)
		if env.Type != ipc.TypePingResult {
			t.Fatalf("event type = %q, want %q", env.Type, ipc.TypePingResult)
		}
		var pr api.PingResult
		if err := env.DecodePayload(&pr); err != nil {
			t.Fatalf("decode ping result: %v", err)
		}
		if pr.Host != host {
			t.Fatalf("host = %q, want %q", pr.Host, host)
		}
		if i%2 == 0 {
			if pr.RTTMS == nil || *pr.RTTMS != pingRTTMS {
				t.Fatalf("rtt for %q = %v, want %v", host, pr.RTTMS, pingRTTMS)
			}
		} else if pr.RTTMS != nil {
			t.Fatalf("rtt for %q = %v, want a timeout", host, *pr.RTTMS)
		}
	}
	assertNoEvent(t, m)
}

func TestLogsSnapshotFiltersAndOrder(t *testing.T) {
	ctx := context.Background()
	m := New()
	all, err := m.LogsSnapshot(ctx, "")
	if err != nil {
		t.Fatalf("LogsSnapshot: %v", err)
	}
	if len(all.Entries) != 7 {
		t.Fatalf("entries = %d, want 7", len(all.Entries))
	}
	for i := 1; i < len(all.Entries); i++ {
		if !all.Entries[i].Time.After(all.Entries[i-1].Time) {
			t.Fatalf("entries not oldest-first at %d: %v", i, all.Entries[i].Time)
		}
	}
	for _, tc := range []struct {
		level string
		want  int
	}{
		{"debug", 7},
		{"info", 5},
		{"warn", 2},
		{"error", 1},
	} {
		snap, err := m.LogsSnapshot(ctx, tc.level)
		if err != nil {
			t.Fatalf("LogsSnapshot(%q): %v", tc.level, err)
		}
		if len(snap.Entries) != tc.want {
			t.Fatalf("entries at %q = %d, want %d", tc.level, len(snap.Entries), tc.want)
		}
	}
	if _, err := m.LogsSnapshot(ctx, "bogus"); err == nil {
		t.Fatal("LogsSnapshot(bogus) = nil, want an error")
	} else {
		isBadRequest(t, err)
	}
}

func TestLogsSubscribeValidatesLevel(t *testing.T) {
	ctx := context.Background()
	m := New()
	for _, level := range []string{"", "debug", "warn"} {
		if _, err := m.LogsSubscribe(ctx, level); err != nil {
			t.Fatalf("LogsSubscribe(%q) = %v", level, err)
		}
	}
	if _, err := m.LogsSubscribe(ctx, "bogus"); err == nil {
		t.Fatal("LogsSubscribe(bogus) = nil, want an error")
	} else {
		isBadRequest(t, err)
	}
}

// TestConcurrentUse exercises the injections and the event channel from many
// goroutines; run under -race it guards the field access.
func TestConcurrentUse(t *testing.T) {
	m := New()
	scenarios := []Scenario{ScenarioDisconnected, ScenarioConnected, ScenarioConnecting, ScenarioError}
	stop := make(chan struct{})
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for {
			select {
			case <-m.Events():
			case <-stop:
				return
			}
		}
	}()

	ctx := context.Background()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				m.SetLatency(time.Duration(i%3) * time.Millisecond)
				_, _ = m.Status(ctx)
				_, _ = m.Account(ctx)
				_, _ = m.Locations(ctx, i%2 == 0)
				_, _ = m.Ping(ctx, []string{"de1.edge.example:443"})
				m.EmitLog(entry("goroutine"))
				m.SetScenario(scenarios[(g+i)%len(scenarios)])
				m.FailNext(nil)
			}
		}(g)
	}
	wg.Wait()
	close(stop)
	<-drained
}

// --- Controller against the mock ---

func quietOptions() gui.Options {
	return gui.Options{
		PumpInterval:     time.Millisecond,
		StatusInterval:   time.Hour,
		SettingsInterval: time.Hour,
	}
}

func startPoll(t *testing.T, c *gui.Controller) {
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

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	return raw
}

func TestControllerProjectsScenarios(t *testing.T) {
	cases := []struct {
		scenario  Scenario
		label     string
		connected bool
	}{
		{ScenarioDisconnected, "Off", false},
		{ScenarioConnected, "Connected", true},
		{ScenarioConnecting, "Connecting", false},
		{ScenarioError, "Error", false},
	}
	for _, tc := range cases {
		t.Run(string(tc.scenario), func(t *testing.T) {
			m := New()
			m.SetScenario(tc.scenario)
			ctrl := gui.NewController(m, quietOptions())
			if err := ctrl.Dispatch(gui.Action{Name: gui.ActionRefresh}); err != nil {
				t.Fatalf("Dispatch(refresh) = %v", err)
			}
			snap := ctrl.Snapshot()
			if snap.Status == nil {
				t.Fatal("status not projected")
			}
			if snap.Status.Label != tc.label {
				t.Fatalf("label = %q, want %q", snap.Status.Label, tc.label)
			}
			if snap.Status.Connected != tc.connected {
				t.Fatalf("connected = %v, want %v", snap.Status.Connected, tc.connected)
			}
			if snap.Status.QuotaRemaining == nil || snap.Status.QuotaLimit == nil {
				t.Fatalf("quota not projected: %+v", snap.Status)
			}
			if tc.connected && (snap.Status.Uptime == "" || snap.Status.ExitIP != demoExitIP) {
				t.Fatalf("connected facts missing: %+v", snap.Status)
			}
			if tc.scenario == ScenarioError && *snap.Status.QuotaRemaining != 0 {
				t.Fatalf("error quota remaining = %d, want 0", *snap.Status.QuotaRemaining)
			}
			waitFor(t, "account and locations", func() bool {
				s := ctrl.Snapshot()
				return s.Account != nil && s.LocationsLoaded
			})
			snap = ctrl.Snapshot()
			if len(snap.Locations) != 4 || snap.Locations[0].Code != serverlist.RecommendedCode {
				t.Fatalf("locations = %+v", snap.Locations)
			}
			if tc.scenario == ScenarioError {
				if snap.Account.Error != "guardian: HTTP 503" {
					t.Fatalf("account error = %q", snap.Account.Error)
				}
			} else if !snap.Account.Subscribed {
				t.Fatalf("account = %+v, want an entitlement", snap.Account)
			}
		})
	}
}

// TestControllerScenarioSwitchPushesStatus proves a scenario switch reaches a
// running controller through the mock's status event, with no polling call.
func TestControllerScenarioSwitchPushesStatus(t *testing.T) {
	m := New()
	ctrl := gui.NewController(m, quietOptions())
	startPoll(t, ctrl)
	waitFor(t, "initial Off status", func() bool {
		s := ctrl.Snapshot()
		return s.Status != nil && s.Status.Label == "Off"
	})
	m.SetScenario(ScenarioConnected)
	waitFor(t, "pushed Connected status", func() bool {
		s := ctrl.Snapshot()
		return s.Status != nil && s.Status.Label == "Connected" && s.Status.Uptime != ""
	})
	m.SetScenario(ScenarioError)
	waitFor(t, "pushed Error status", func() bool {
		s := ctrl.Snapshot()
		return s.Status != nil && s.Status.Label == "Error"
	})
}

// TestControllerConnectFlow pins the explicit-transition contract: connect
// passes the payload through and is acknowledged, but only SetScenario moves
// the projected state.
func TestControllerConnectFlow(t *testing.T) {
	m := New()
	m.SetScenario(ScenarioConnecting)
	ctrl := gui.NewController(m, quietOptions())
	want := api.ConnectPayload{LocationCode: "DE", CityCode: "ber", Server: demoServer, ProxyOnly: true}
	if err := ctrl.Dispatch(gui.Action{Name: gui.ActionConnect, Payload: mustJSON(t, want)}); err != nil {
		t.Fatalf("Dispatch(connect) = %v", err)
	}
	got, ok := m.LastConnect()
	if !ok || got != want {
		t.Fatalf("LastConnect = %+v, %v, want %+v", got, ok, want)
	}
	snap := ctrl.Snapshot()
	if snap.Notice == nil || snap.Notice.Message != "Connected" {
		t.Fatalf("notice = %+v, want Connected", snap.Notice)
	}
	if snap.Status == nil || snap.Status.Label != "Connecting" {
		t.Fatalf("connect changed the scenario: %+v", snap.Status)
	}

	m.SetScenario(ScenarioConnected)
	if err := ctrl.Dispatch(gui.Action{Name: gui.ActionRefresh}); err != nil {
		t.Fatalf("Dispatch(refresh) = %v", err)
	}
	if s := ctrl.Snapshot().Status; s == nil || s.Label != "Connected" {
		t.Fatalf("status = %+v, want Connected", s)
	}

	if err := ctrl.Dispatch(gui.Action{Name: gui.ActionDisconnect}); err != nil {
		t.Fatalf("Dispatch(disconnect) = %v", err)
	}
	if n := ctrl.Snapshot().Notice; n == nil || n.Message != "Disconnected" {
		t.Fatalf("notice = %+v, want Disconnected", n)
	}
}

func TestControllerFailNextSurfacesOnce(t *testing.T) {
	m := New()
	ctrl := gui.NewController(m, quietOptions())
	sentinel := errors.New("pipe broken")
	m.FailNext(sentinel)
	err := ctrl.Dispatch(gui.Action{Name: gui.ActionRefresh})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Dispatch(refresh) = %v, want the injected error", err)
	}
	snap := ctrl.Snapshot()
	if snap.StatusError != "pipe broken" {
		t.Fatalf("status error = %q", snap.StatusError)
	}
	if snap.Notice == nil || snap.Notice.Message != "could not refresh the status: pipe broken" {
		t.Fatalf("notice = %+v", snap.Notice)
	}
	if !snap.Reachable {
		t.Fatal("a daemon-level error must not flip the unreachable gate")
	}
	if err := ctrl.Dispatch(gui.Action{Name: gui.ActionRefresh}); err != nil {
		t.Fatalf("Dispatch(refresh) after the injection = %v", err)
	}
	if got := ctrl.Snapshot().StatusError; got != "" {
		t.Fatalf("status error = %q after a successful refresh, want empty", got)
	}
}

func TestControllerPingAndSelectLocation(t *testing.T) {
	m := New()
	ctrl := gui.NewController(m, quietOptions())
	startPoll(t, ctrl)
	hosts := []string{"rec1.edge.example:443", "de1.edge.example:443"}
	if err := ctrl.Dispatch(gui.Action{Name: gui.ActionPing, Payload: mustJSON(t, api.PingPayload{Hosts: hosts})}); err != nil {
		t.Fatalf("Dispatch(ping) = %v", err)
	}
	waitFor(t, "ping labels", func() bool {
		rtts := ctrl.Snapshot().RTTs
		return rtts[hosts[0]] == "42 ms" && rtts[hosts[1]] == "timeout"
	})
	sel := api.SelectLocationPayload{CountryCode: "DE", CityCode: "ber", Server: demoServer}
	if err := ctrl.Dispatch(gui.Action{Name: gui.ActionSelectLocation, Payload: mustJSON(t, sel)}); err != nil {
		t.Fatalf("Dispatch(select_location) = %v", err)
	}
	if n := ctrl.Snapshot().Notice; n == nil || n.Message != "Location saved" {
		t.Fatalf("notice = %+v, want Location saved", n)
	}
}

func TestControllerLogsFlow(t *testing.T) {
	m := New()
	ctrl := gui.NewController(m, quietOptions())
	startPoll(t, ctrl)
	if err := ctrl.Dispatch(gui.Action{Name: gui.ActionLogsSnapshot, Payload: mustJSON(t, api.LogsSnapshotPayload{Level: "info"})}); err != nil {
		t.Fatalf("Dispatch(logs_snapshot) = %v", err)
	}
	waitFor(t, "canned snapshot", func() bool { return len(ctrl.Snapshot().Logs) == 5 })
	if err := ctrl.Dispatch(gui.Action{Name: gui.ActionLogsSubscribe, Payload: mustJSON(t, api.LogsSubscribePayload{Level: "info"})}); err != nil {
		t.Fatalf("Dispatch(logs_subscribe) = %v", err)
	}
	live := logging.Entry{Time: time.Now().UTC(), Level: logging.Info, Tag: "mock", Msg: "live entry from the mock"}
	m.EmitLog(live)
	waitFor(t, "pushed log entry", func() bool {
		logs := ctrl.Snapshot().Logs
		return len(logs) == 6 && logs[len(logs)-1].Msg == live.Msg
	})
}

func TestControllerSettingsStateless(t *testing.T) {
	m := New()
	ctrl := gui.NewController(m, quietOptions())
	if err := ctrl.Dispatch(gui.Action{Name: gui.ActionSettingsGet}); err != nil {
		t.Fatalf("Dispatch(settings_get) = %v", err)
	}
	if s := ctrl.Snapshot().Settings; s == nil || s.SocksPort != 1080 || s.KillSwitch != "on" {
		t.Fatalf("settings = %+v", s)
	}
	payload := api.SettingsSetPayload{Key: "log_level", Value: "warn"}
	if err := ctrl.Dispatch(gui.Action{Name: gui.ActionSettingsSet, Payload: mustJSON(t, payload)}); err != nil {
		t.Fatalf("Dispatch(settings_set) = %v", err)
	}
	snap := ctrl.Snapshot()
	if n := snap.SettingsNotice; n == nil || n.Message != "Log level saved" {
		t.Fatalf("settings notice = %+v", snap.SettingsNotice)
	}
	if snap.Settings.LogLevel != "info" {
		t.Fatalf("canned settings changed: %q", snap.Settings.LogLevel)
	}
}
