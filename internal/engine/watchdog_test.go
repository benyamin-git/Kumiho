package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/upstream"
)

// compressWatchdog shrinks the watchdog schedule so tests run in milliseconds.
// It must run before newTestController: New copies these defaults into the
// controller, which is what the watchdog goroutines actually read.
func compressWatchdog(t *testing.T) {
	t.Helper()
	prevInterval, prevMin, prevMax := defaultWatchdogInterval, defaultRedialJitterMin, defaultRedialJitterMax
	defaultWatchdogInterval, defaultRedialJitterMin, defaultRedialJitterMax = 10*time.Millisecond, time.Millisecond, 2*time.Millisecond
	t.Cleanup(func() {
		defaultWatchdogInterval, defaultRedialJitterMin, defaultRedialJitterMax = prevInterval, prevMin, prevMax
	})
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(msg)
}

// loginProxyOnly signs in and connects in proxy-only mode. The HTTP backends
// and dialUpstream must be configured by the caller first.
func loginProxyOnly(t *testing.T, ctrl *Controller) {
	t.Helper()
	ctx := context.Background()
	if st := ctrl.LoginEmail("user@example.com"); st.Step != "password" {
		t.Fatalf("login step = %+v", st)
	}
	if st := ctrl.LoginPassword(ctx, "pw"); st.Step != "done" {
		t.Fatalf("login = %+v", st)
	}
	if err := ctrl.Connect(ctx, api.ConnectPayload{ProxyOnly: true}); err != nil {
		t.Fatal(err)
	}
}

func TestWatchdogRedialsDeadSession(t *testing.T) {
	compressWatchdog(t)
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.guardian.BaseURL = guardianPassServer(t, "pass-1", new(atomic.Int32)).URL
	ctrl.serverListURL = serverListServer(t).URL
	ctrl.exitCheckURL = ""
	ctrl.socksPortOverride = freePort(t)

	first, second := newFakeSession(), newFakeSession()
	var dials atomic.Int32
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		if dials.Add(1) == 1 {
			return first, nil
		}
		return second, nil
	}
	loginProxyOnly(t, ctrl)
	defer ctrl.stopTunnel("test cleanup")

	_ = first.Close() // the edge connection dies

	waitFor(t, 3*time.Second, func() bool {
		return ctrl.currentSession() == second &&
			ctrl.Snapshot().State == string(StateProxyOnly)
	}, "watchdog did not redial the dead session")
	if dials.Load() != 2 {
		t.Fatalf("dial attempts = %d, want 2", dials.Load())
	}
}

func TestWatchdogRotatesEdgeAfterTwoFailures(t *testing.T) {
	compressWatchdog(t)
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.guardian.BaseURL = guardianPassServer(t, "pass-1", new(atomic.Int32)).URL
	ctrl.serverListURL = altServerListServer(t).URL
	ctrl.exitCheckURL = ""
	ctrl.socksPortOverride = freePort(t)

	live := newFakeSession()
	var mu sync.Mutex
	var hosts []string
	var dials atomic.Int32
	ctrl.dialUpstream = func(_ context.Context, opts upstream.Options) (upstreamSession, error) {
		mu.Lock()
		hosts = append(hosts, opts.Host)
		mu.Unlock()
		if dials.Add(1) == 1 {
			return live, nil
		}
		return nil, errors.New("edge unreachable")
	}
	loginProxyOnly(t, ctrl)
	defer ctrl.stopTunnel("test cleanup")

	_ = live.Close()

	waitFor(t, 3*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(hosts) >= 4
	}, "watchdog did not attempt enough redials")

	mu.Lock()
	got := append([]string(nil), hosts...)
	mu.Unlock()
	// hosts[0] is the initial connect; redials 1-2 retry the same edge,
	// redial 3 rotates (PLAN.md §2.4: rotate every 2 failures).
	want := []string{"fra1.example.net", "fra1.example.net", "fra1.example.net", "fra2.example.net"}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("dial hosts = %v, want prefix %v", got, want)
		}
	}
}

func TestWatchdogFatalStopsTunnel(t *testing.T) {
	compressWatchdog(t)
	ctrl, _ := newTestController(t, loginOKHandler(t))

	var passCalls atomic.Int32
	guardSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/fpn/token" {
			http.NotFound(w, r)
			return
		}
		if passCalls.Add(1) == 1 {
			fmt.Fprintf(w, `{"token":"pass-1","expires_at":%d}`, time.Now().Add(2*time.Hour).Unix())
			return
		}
		w.WriteHeader(http.StatusTooManyRequests) // quota exhausted mid-redial
	}))
	t.Cleanup(guardSrv.Close)
	ctrl.guardian.BaseURL = guardSrv.URL
	ctrl.serverListURL = serverListServer(t).URL
	ctrl.exitCheckURL = ""
	ctrl.socksPortOverride = freePort(t)

	live := newFakeSession()
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		return live, nil
	}
	loginProxyOnly(t, ctrl)

	_ = live.Close()

	waitFor(t, 3*time.Second, func() bool {
		return ctrl.Snapshot().State == string(StateFatal)
	}, "watchdog did not stop on a fatal pass error")
	if sess := ctrl.currentSession(); sess != nil {
		t.Fatal("session not torn down on fatal stop")
	}
}

// altServerListServer serves one city with two edges (for rotation tests).
func altServerListServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"code":"DE","name":"Germany","cities":[{"code":"FRA","name":"Frankfurt","servers":[{"hostname":"fra1.example.net","port":2499},{"hostname":"fra2.example.net","port":2499}]}]}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}
