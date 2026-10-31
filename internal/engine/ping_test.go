package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/guardian"
	"github.com/benyamin-git/kumiho/internal/serverlist"
	"github.com/benyamin-git/kumiho/internal/upstream"
)

type pingFakeSession struct {
	mu      sync.Mutex
	targets []string
	open    func(context.Context, string, int) (net.Conn, error)
	closed  atomic.Int32
}

func (s *pingFakeSession) OpenStream(ctx context.Context, host string, port int) (net.Conn, error) {
	s.mu.Lock()
	s.targets = append(s.targets, fmt.Sprintf("%s:%d", host, port))
	s.mu.Unlock()
	if s.open != nil {
		return s.open(ctx, host, port)
	}
	server, client := net.Pipe()
	_ = client.Close()
	return server, nil
}

func (s *pingFakeSession) Totals() (int64, int64) { return 0, 0 }
func (s *pingFakeSession) IsConnected() bool      { return true }
func (s *pingFakeSession) Close() error {
	s.closed.Add(1)
	return nil
}

func (s *pingFakeSession) targetList() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.targets...)
}

func pingFixture(n int) string {
	records := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		records = append(records, engineListRecord(fmt.Sprintf("C%d", i), false, ""))
	}
	return engineListFixture(records...)
}

func pingHost(i int) string {
	return fmt.Sprintf("c%d.example.net:2499", i)
}

func loginPing(t *testing.T, ctrl *Controller) {
	t.Helper()
	ctrl.LoginEmail("user@example.com")
	if ls := ctrl.LoginPassword(context.Background(), "pw"); ls.Step != "done" {
		t.Fatalf("login = %+v", ls)
	}
}

func pingPassServer(t *testing.T, token string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/fpn/token" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		fmt.Fprintf(w, `{"token":%q,"expires_at":%d}`, token, time.Now().Add(time.Hour).Unix())
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestPingHostsMeasuresThroughTemporarySessions(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = servingServerListServer(t, pingFixture(3)).URL
	loginPing(t, ctrl)
	ctrl.pass = &guardian.Pass{Token: "live", ExpiresAt: time.Now().Add(time.Hour).Unix()}

	var mu sync.Mutex
	var sessions []*pingFakeSession
	ctrl.dialUpstream = func(_ context.Context, opts upstream.Options) (upstreamSession, error) {
		sess := &pingFakeSession{}
		if opts.Host == "c1.example.net" {
			sess.open = func(context.Context, string, int) (net.Conn, error) {
				return nil, errors.New("connection refused")
			}
		} else {
			sess.open = func(context.Context, string, int) (net.Conn, error) {
				time.Sleep(10 * time.Millisecond)
				server, client := net.Pipe()
				_ = client.Close()
				return server, nil
			}
		}
		mu.Lock()
		sessions = append(sessions, sess)
		mu.Unlock()
		return sess, nil
	}

	var results []api.PingResult
	done := ctrl.PingHosts(context.Background(), []string{
		"C0.example.net:2499", "c0.example.net:2499", "c1.example.net:2499",
		"unknown.example.net:2499", "c2.example.net:2499",
	}, func(r api.PingResult) { results = append(results, r) })

	if done.Total != 3 || done.Answered != 2 || done.Error != "" {
		t.Fatalf("PingDone = %+v, want total 3 answered 2", done)
	}
	if len(results) != 3 {
		t.Fatalf("results = %+v, want 3", results)
	}
	byHost := make(map[string]api.PingResult, len(results))
	for _, r := range results {
		byHost[r.Host] = r
	}
	if r := byHost["c0.example.net:2499"]; r.RTTMS == nil || *r.RTTMS <= 0 {
		t.Fatalf("c0 result = %+v, want a positive RTT", r)
	}
	if r := byHost["c1.example.net:2499"]; r.RTTMS != nil {
		t.Fatalf("failed result = %+v, want nil RTT", r)
	}
	if r := byHost["c2.example.net:2499"]; r.RTTMS == nil {
		t.Fatalf("c2 result = %+v, want RTT", r)
	}

	mu.Lock()
	gotSessions := append([]*pingFakeSession(nil), sessions...)
	mu.Unlock()
	if len(gotSessions) != 3 {
		t.Fatalf("sessions = %d, want 3", len(gotSessions))
	}
	for _, sess := range gotSessions {
		if sess.closed.Load() != 1 {
			t.Fatalf("session close count = %d, want 1", sess.closed.Load())
		}
		for _, target := range sess.targetList() {
			if target != "vpn.mozilla.org:443" {
				t.Fatalf("probe target = %q, want vpn.mozilla.org:443", target)
			}
		}
	}

	countries, err := ctrl.Locations(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	cached := ctrl.cachedLatency(countries)
	if len(cached) != 3 {
		t.Fatalf("cached samples = %d, want 3", len(cached))
	}
	if sample := cached["c1.example.net:2499"]; sample.OK || sample.RTT != 0 {
		t.Fatalf("failed sample = %+v, want OK=false RTT=0", sample)
	}
}

func TestPingHostsRejectsSignedOutAndUnknownHosts(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = servingServerListServer(t, pingFixture(1)).URL
	var dials atomic.Int32
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		dials.Add(1)
		return &pingFakeSession{}, nil
	}

	done := ctrl.PingHosts(context.Background(), []string{pingHost(0)}, nil)
	if done.Error != "Ping all needs a signed-in account" {
		t.Fatalf("signed-out PingDone = %+v", done)
	}
	if dials.Load() != 0 {
		t.Fatal("signed-out run dialed an edge")
	}

	done = ctrl.PingHosts(context.Background(), []string{"unknown.example.net:2499"}, nil)
	if done.Error != "no visible locations to ping" {
		t.Fatalf("unknown-host PingDone = %+v", done)
	}
}

func TestPingHostsPreflightError(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = failingServerListServer(t).URL
	loginPing(t, ctrl)
	var dials atomic.Int32
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		dials.Add(1)
		return &pingFakeSession{}, nil
	}

	done := ctrl.PingHosts(context.Background(), []string{pingHost(0)}, nil)
	if !strings.Contains(done.Error, "500") {
		t.Fatalf("preflight PingDone = %+v, want the fetch error", done)
	}
	if dials.Load() != 0 {
		t.Fatal("preflight failure dialed an edge")
	}
}

func TestPingHostsReusesValidPassAndAcquiresOnce(t *testing.T) {
	t.Run("valid live pass is reused", func(t *testing.T) {
		ctrl, _ := newTestController(t, loginOKHandler(t))
		ctrl.serverListURL = servingServerListServer(t, pingFixture(1)).URL
		loginPing(t, ctrl)
		ctrl.pass = &guardian.Pass{Token: "live", ExpiresAt: time.Now().Add(time.Hour).Unix()}
		passSrv, fetchCalls := pingPassServer(t, "unused")
		ctrl.guardian = &guardian.Client{BaseURL: passSrv.URL, HTTP: passSrv.Client()}

		var gotToken atomic.Value
		ctrl.dialUpstream = func(_ context.Context, opts upstream.Options) (upstreamSession, error) {
			gotToken.Store(opts.Pass())
			return &pingFakeSession{}, nil
		}
		done := ctrl.PingHosts(context.Background(), []string{pingHost(0)}, nil)
		if done.Error != "" || done.Answered != 1 {
			t.Fatalf("PingDone = %+v", done)
		}
		if fetchCalls.Load() != 0 {
			t.Fatalf("Guardian fetches = %d, want 0", fetchCalls.Load())
		}
		if got := gotToken.Load(); got != "live" {
			t.Fatalf("probe pass = %v, want live", got)
		}
	})

	t.Run("missing pass acquires once", func(t *testing.T) {
		ctrl, _ := newTestController(t, loginOKHandler(t))
		ctrl.serverListURL = servingServerListServer(t, pingFixture(1)).URL
		loginPing(t, ctrl)
		passSrv, fetchCalls := pingPassServer(t, "acquired")
		ctrl.guardian = &guardian.Client{BaseURL: passSrv.URL, HTTP: passSrv.Client()}

		var gotToken atomic.Value
		ctrl.dialUpstream = func(_ context.Context, opts upstream.Options) (upstreamSession, error) {
			gotToken.Store(opts.Pass())
			return &pingFakeSession{}, nil
		}
		done := ctrl.PingHosts(context.Background(), []string{pingHost(0)}, nil)
		if done.Error != "" || done.Answered != 1 {
			t.Fatalf("PingDone = %+v", done)
		}
		if fetchCalls.Load() != 1 {
			t.Fatalf("Guardian fetches = %d, want 1", fetchCalls.Load())
		}
		if got := gotToken.Load(); got != "acquired" {
			t.Fatalf("probe pass = %v, want acquired", got)
		}
	})

	t.Run("expired pass acquires once", func(t *testing.T) {
		ctrl, _ := newTestController(t, loginOKHandler(t))
		ctrl.serverListURL = servingServerListServer(t, pingFixture(1)).URL
		loginPing(t, ctrl)
		ctrl.pass = &guardian.Pass{Token: "expired", ExpiresAt: time.Now().Add(-time.Minute).Unix()}
		passSrv, fetchCalls := pingPassServer(t, "acquired")
		ctrl.guardian = &guardian.Client{BaseURL: passSrv.URL, HTTP: passSrv.Client()}

		ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
			return &pingFakeSession{}, nil
		}
		done := ctrl.PingHosts(context.Background(), []string{pingHost(0)}, nil)
		if done.Error != "" || done.Answered != 1 {
			t.Fatalf("PingDone = %+v", done)
		}
		if fetchCalls.Load() != 1 {
			t.Fatalf("Guardian fetches = %d, want 1", fetchCalls.Load())
		}
	})
}

func TestPingHostsQuotaError(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = servingServerListServer(t, pingFixture(1)).URL
	loginPing(t, ctrl)
	quotaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "quota exhausted", http.StatusTooManyRequests)
	}))
	t.Cleanup(quotaSrv.Close)
	ctrl.guardian = &guardian.Client{BaseURL: quotaSrv.URL, HTTP: quotaSrv.Client()}
	var dials atomic.Int32
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		dials.Add(1)
		return &pingFakeSession{}, nil
	}

	done := ctrl.PingHosts(context.Background(), []string{pingHost(0)}, nil)
	if !strings.Contains(done.Error, "quota is exhausted") {
		t.Fatalf("quota PingDone = %+v", done)
	}
	if dials.Load() != 0 {
		t.Fatal("quota failure dialed an edge")
	}
}

func TestPassForPrefersLiveThenInitial(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	initial := &guardian.Pass{Token: "initial"}
	passFn := ctrl.passFor(initial)
	if got := passFn(); got != "initial" {
		t.Fatalf("passFor before c.pass = %q, want initial", got)
	}
	ctrl.mu.Lock()
	ctrl.pass = &guardian.Pass{Token: "live"}
	ctrl.mu.Unlock()
	if got := passFn(); got != "live" {
		t.Fatalf("passFor after c.pass = %q, want live", got)
	}

	cand := serverlist.Candidate{Host: "edge.example.net", Port: 443}
	opts, err := ctrl.upstreamOptions(context.Background(), cand, passFn)
	if err != nil {
		t.Fatal(err)
	}
	if got := opts.Pass(); got != "live" {
		t.Fatalf("upstreamOptions pass = %q, want live", got)
	}
}

func TestPingHostsConcurrencyLimit(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = servingServerListServer(t, pingFixture(5)).URL
	loginPing(t, ctrl)
	ctrl.pass = &guardian.Pass{Token: "live"}

	started := make(chan struct{}, 5)
	release := make(chan struct{})
	var inFlight, maxInFlight atomic.Int32
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		n := inFlight.Add(1)
		for {
			current := maxInFlight.Load()
			if n <= current || maxInFlight.CompareAndSwap(current, n) {
				break
			}
		}
		started <- struct{}{}
		<-release
		inFlight.Add(-1)
		return &pingFakeSession{}, nil
	}

	doneCh := make(chan api.PingDone, 1)
	go func() {
		doneCh <- ctrl.PingHosts(context.Background(), []string{
			pingHost(0), pingHost(1), pingHost(2), pingHost(3), pingHost(4),
		}, nil)
	}()
	for i := 0; i < 4; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("four dials did not start")
		}
	}
	select {
	case <-started:
		t.Fatal("a fifth dial started while four were in flight")
	case <-time.After(50 * time.Millisecond):
	}
	if got := maxInFlight.Load(); got != 4 {
		t.Fatalf("max in-flight dials = %d, want 4", got)
	}
	close(release)
	done := <-doneCh
	if done.Total != 5 || done.Answered != 5 {
		t.Fatalf("PingDone = %+v", done)
	}
	if got := maxInFlight.Load(); got > 4 {
		t.Fatalf("max in-flight dials = %d, want at most 4", got)
	}
}

func TestPingHostsTimeout(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = servingServerListServer(t, pingFixture(1)).URL
	loginPing(t, ctrl)
	ctrl.pass = &guardian.Pass{Token: "live"}
	ctrl.pingTimeout = 20 * time.Millisecond

	sess := &pingFakeSession{}
	sess.open = func(ctx context.Context, _ string, _ int) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		return sess, nil
	}

	var results []api.PingResult
	start := time.Now()
	done := ctrl.PingHosts(context.Background(), []string{pingHost(0)}, func(r api.PingResult) {
		results = append(results, r)
	})
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timeout run took %v", elapsed)
	}
	if done.Total != 1 || done.Answered != 0 {
		t.Fatalf("PingDone = %+v", done)
	}
	if len(results) != 1 || results[0].RTTMS != nil {
		t.Fatalf("timeout results = %+v, want one nil RTT", results)
	}
	if sess.closed.Load() != 1 {
		t.Fatalf("session close count = %d, want 1", sess.closed.Load())
	}
}

func TestPingHostsLeavesConnectedSessionUntouched(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = servingServerListServer(t, pingFixture(1)).URL
	loginPing(t, ctrl)
	ctrl.pass = &guardian.Pass{Token: "live"}

	active := &pingFakeSession{}
	ctrl.mu.Lock()
	ctrl.session = active
	ctrl.state = StateConnected
	ctrl.mu.Unlock()

	probe := &pingFakeSession{}
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		return probe, nil
	}
	var results []api.PingResult
	done := ctrl.PingHosts(context.Background(), []string{pingHost(0)}, func(r api.PingResult) {
		results = append(results, r)
	})
	if done.Error != "" || len(results) != 1 {
		t.Fatalf("PingDone = %+v results=%+v", done, results)
	}

	ctrl.mu.Lock()
	sess := ctrl.session
	state := ctrl.state
	ctrl.mu.Unlock()
	if sess != active || state != StateConnected {
		t.Fatalf("active session/state changed: %v %s", sess, state)
	}
	if active.closed.Load() != 0 {
		t.Fatal("active session was closed by a ping run")
	}
	if probe.closed.Load() != 1 {
		t.Fatalf("probe session close count = %d, want 1", probe.closed.Load())
	}
}

func TestPingCachePrunesStaleAndHidden(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	countries, err := serverlist.Parse([]byte(pingFixture(3)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ctrl.pings = map[string]pingSample{
		"c0.example.net:2499":     {RTT: 5 * time.Millisecond, OK: true, MeasuredAt: now},
		"c1.example.net:2499":     {OK: false, MeasuredAt: now},
		"c2.example.net:2499":     {RTT: time.Millisecond, OK: true, MeasuredAt: now.Add(-time.Hour)},
		"hidden.example.net:2499": {RTT: time.Millisecond, OK: true, MeasuredAt: now},
	}
	got := ctrl.cachedLatency(countries)
	if len(got) != 2 {
		t.Fatalf("cached latency = %+v, want the two fresh visible samples", got)
	}
	if _, ok := got["c0.example.net:2499"]; !ok {
		t.Fatal("fresh success pruned")
	}
	if sample, ok := got["c1.example.net:2499"]; !ok || sample.OK {
		t.Fatalf("fresh failure = %+v, want retained with OK=false", sample)
	}
	if _, ok := ctrl.pings["c2.example.net:2499"]; ok {
		t.Fatal("stale sample not pruned")
	}
	if _, ok := ctrl.pings["hidden.example.net:2499"]; ok {
		t.Fatal("hidden sample not pruned")
	}
}

func TestPingRunsDoNotDuplicateCacheEntries(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = servingServerListServer(t, pingFixture(1)).URL
	loginPing(t, ctrl)
	ctrl.pass = &guardian.Pass{Token: "live"}
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		return &pingFakeSession{}, nil
	}
	for i := 0; i < 2; i++ {
		done := ctrl.PingHosts(context.Background(), []string{pingHost(0)}, nil)
		if done.Error != "" {
			t.Fatalf("run %d: %+v", i, done)
		}
	}
	if len(ctrl.pings) != 1 {
		t.Fatalf("cache entries = %d, want 1", len(ctrl.pings))
	}
}
