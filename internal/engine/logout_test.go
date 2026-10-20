package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/guardian"
	"github.com/benyamin-git/kumiho/internal/upstream"
)

// Sign-out must leave nothing account- or usage-shaped in the snapshot: the
// unauthenticated status carries no quota, exit, totals, rates or pass
// (v0.4.0 spec). A plain disconnect keeps its totals (see the next test).
func TestLogoutWipesAccountAndUsageState(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctx := context.Background()
	ctrl.LoginEmail("user@example.com")
	if ls := ctrl.LoginPassword(ctx, "pw"); ls.Step != "done" {
		t.Fatalf("login = %+v", ls)
	}

	limit := int64(10 << 30)
	ctrl.mu.Lock()
	ctrl.quota = &api.Quota{Limit: &limit}
	ctrl.exitIP = "198.51.100.9"
	ctrl.exitCountry = "DE"
	ctrl.totalsUp = 1234
	ctrl.totalsDown = 5678
	ctrl.ratesUp = 12.5
	ctrl.ratesDown = 34.5
	ctrl.pass = &guardian.Pass{Token: "pass-1", ExpiresAt: 4102444800} // 2100-01-01
	ctrl.mu.Unlock()

	// Precondition: the populated state is visible before sign-out, so a
	// passing assertion afterwards cannot be vacuous.
	before := ctrl.Snapshot()
	if before.Quota == nil || before.PassExpires != 4102444800 {
		t.Fatalf("precondition: account state not visible: %+v", before)
	}
	if before.Totals == nil || before.Totals.Up != 1234 || before.Totals.Down != 5678 {
		t.Fatalf("precondition: totals not visible: %+v", before)
	}

	if err := ctrl.Logout(); err != nil {
		t.Fatal(err)
	}

	st := ctrl.Snapshot()
	if st.State != string(StateUnauthenticated) || st.Authenticated || st.Email != "" {
		t.Errorf("snapshot after logout = %+v, want UNAUTHENTICATED with no account", st)
	}
	if st.TokenExpires != 0 {
		t.Errorf("token expiry after logout = %d, want 0", st.TokenExpires)
	}
	if st.Quota != nil {
		t.Errorf("quota after logout = %+v, want nil", st.Quota)
	}
	if st.ExitIP != "" || st.ExitCountry != "" {
		t.Errorf("exit after logout = %q/%q, want empty", st.ExitIP, st.ExitCountry)
	}
	if st.Totals != nil {
		t.Errorf("totals after logout = %+v, want nil", st.Totals)
	}
	if st.Rates != nil {
		t.Errorf("rates after logout = %+v, want nil", st.Rates)
	}
	if st.PassExpires != 0 {
		t.Errorf("pass expiry after logout = %d, want 0", st.PassExpires)
	}

	ctrl.mu.Lock()
	quota, pass := ctrl.quota, ctrl.pass
	exitIP, exitCountry := ctrl.exitIP, ctrl.exitCountry
	totalsUp, totalsDown := ctrl.totalsUp, ctrl.totalsDown
	ratesUp, ratesDown := ctrl.ratesUp, ctrl.ratesDown
	ctrl.mu.Unlock()
	if quota != nil || pass != nil {
		t.Errorf("quota/pass fields after logout = %+v/%+v, want nil", quota, pass)
	}
	if exitIP != "" || exitCountry != "" {
		t.Errorf("exit fields after logout = %q/%q, want empty", exitIP, exitCountry)
	}
	if totalsUp != 0 || totalsDown != 0 || ratesUp != 0 || ratesDown != 0 {
		t.Errorf("usage fields after logout = %d/%d, %v/%v, want zero",
			totalsUp, totalsDown, ratesUp, ratesDown)
	}
}

// Disconnecting is not signing out: the last session's totals stay visible so
// the UI can still report what the session moved.
func TestDisconnectKeepsSessionTotals(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctx := context.Background()
	ctrl.LoginEmail("user@example.com")
	if ls := ctrl.LoginPassword(ctx, "pw"); ls.Step != "done" {
		t.Fatalf("login = %+v", ls)
	}

	// Wire a live fake session so Disconnect accepts the request; its own
	// counters are zero, so Snapshot falls back to the controller totals.
	ctrl.mu.Lock()
	ctrl.session = newFakeSession()
	ctrl.state = StateConnected
	ctrl.totalsUp = 111
	ctrl.totalsDown = 222
	ctrl.mu.Unlock()

	if err := ctrl.Disconnect(); err != nil {
		t.Fatal(err)
	}

	st := ctrl.Snapshot()
	if st.State != string(StateIdle) || !st.Authenticated {
		t.Fatalf("snapshot after disconnect = %+v", st)
	}
	if st.Totals == nil || st.Totals.Up != 111 || st.Totals.Down != 222 {
		t.Fatalf("totals after disconnect = %+v, want 111/222", st.Totals)
	}
}

// A sign-out that races an in-flight redial must still leave a clean snapshot:
// the redial's pass fetch completes after the session is cleared and must not
// publish its pass/quota into the signed-out controller.
func TestLogoutDuringRedialKeepsSnapshotClean(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctx := context.Background()
	ctrl.LoginEmail("user@example.com")
	if ls := ctrl.LoginPassword(ctx, "pw"); ls.Step != "done" {
		t.Fatalf("login = %+v", ls)
	}

	// The Guardian fetch signals that it is in flight, then blocks until the
	// test has signed out. Quota headers make a leaked publish visible.
	reached := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case reached <- struct{}{}:
		default:
		}
		<-release
		w.Header().Set("X-Quota-Limit", "1000")
		w.Header().Set("X-Quota-Remaining", "900")
		fmt.Fprint(w, `{"token":"pass-1","expires_at":4102444800}`)
	}))
	defer srv.Close()
	ctrl.guardian = &guardian.Client{BaseURL: srv.URL, HTTP: srv.Client()}

	// The connection's stop channel, as a live watchdog would hold it.
	stop := make(chan struct{})
	ctrl.mu.Lock()
	ctrl.metricsStop = stop
	ctrl.mu.Unlock()

	dialed := make(chan struct{}, 1)
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		select {
		case dialed <- struct{}{}:
		default:
		}
		return newFakeSession(), nil
	}

	done := make(chan error, 1)
	go func() {
		_, err := ctrl.redialOnce(stop)
		done <- err
	}()

	<-reached
	if err := ctrl.Logout(); err != nil {
		t.Fatal(err)
	}
	close(release)

	err := <-done
	if !errors.Is(err, errSessionEnded) {
		t.Fatalf("redial after logout = %v, want errSessionEnded", err)
	}
	select {
	case <-dialed:
		t.Fatal("redial dialed the edge after logout")
	default:
	}

	st := ctrl.Snapshot()
	if st.Quota != nil || st.PassExpires != 0 || st.Totals != nil {
		t.Fatalf("snapshot after racing logout = %+v, want no account/usage state", st)
	}
}

// A sign-out during an in-flight token refresh must win: the refresh may
// finish, but it must neither republish the account nor recreate tokens.json.
func TestLogoutDuringTokenRefreshKeepsSnapshotClean(t *testing.T) {
	var oauthCalls atomic.Int32
	reached := make(chan struct{})
	var reachedOnce sync.Once
	release := make(chan struct{})
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/account/login":
			fmt.Fprintf(w, `{"sessionToken":%q,"verified":true}`, testSessionHex())
		case "/v1/oauth/token":
			if oauthCalls.Add(1) == 1 {
				fmt.Fprint(w, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600}`)
				return
			}
			reachedOnce.Do(func() { close(reached) })
			<-release
			fmt.Fprint(w, `{"access_token":"new-access","refresh_token":"refresh-2","expires_in":7200}`)
		default:
			http.NotFound(w, r)
		}
	}
	ctrl, paths := newTestController(t, handler)
	ctx := context.Background()
	ctrl.LoginEmail("user@example.com")
	if ls := ctrl.LoginPassword(ctx, "pw"); ls.Step != "done" {
		t.Fatalf("login = %+v", ls)
	}

	done := make(chan error, 1)
	go func() { done <- ctrl.refreshAccessToken(ctx) }()
	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh request did not reach the fake FxA endpoint")
	}

	if err := ctrl.Logout(); err != nil {
		t.Fatal(err)
	}
	close(release)

	if err := <-done; !errors.Is(err, errRefreshSessionChanged) {
		t.Fatalf("refresh after logout = %v, want errRefreshSessionChanged", err)
	}
	st := ctrl.Snapshot()
	if st.State != string(StateUnauthenticated) || st.Authenticated || st.Email != "" || st.TokenExpires != 0 {
		t.Fatalf("snapshot after racing logout = %+v, want UNAUTHENTICATED with no account", st)
	}
	if _, err := os.Stat(paths.Tokens); !os.IsNotExist(err) {
		t.Fatalf("tokens file after racing logout: stat err = %v, want not-exist", err)
	}
}

// The same fence must cover sign-in: a login that was in flight when the user
// signed out cannot publish its tokens afterwards.
func TestLogoutDuringLoginDoesNotRestoreSession(t *testing.T) {
	loginReached := make(chan struct{})
	var reachedOnce sync.Once
	release := make(chan struct{})
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/account/login":
			reachedOnce.Do(func() { close(loginReached) })
			<-release
			fmt.Fprintf(w, `{"sessionToken":%q,"verified":true}`, testSessionHex())
		case "/v1/oauth/token":
			fmt.Fprint(w, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600}`)
		default:
			http.NotFound(w, r)
		}
	}
	ctrl, paths := newTestController(t, handler)
	ctrl.LoginEmail("user@example.com")

	done := make(chan api.LoginState, 1)
	go func() { done <- ctrl.LoginPassword(context.Background(), "pw") }()
	select {
	case <-loginReached:
	case <-time.After(5 * time.Second):
		t.Fatal("login request did not reach the fake FxA endpoint")
	}

	if err := ctrl.Logout(); err != nil {
		t.Fatal(err)
	}
	close(release)

	ls := <-done
	if ls.Step != "done" || ls.Error != errLoginSessionChanged.Error() {
		t.Fatalf("login after logout = %+v, want the cancelled-session error", ls)
	}
	st := ctrl.Snapshot()
	if st.State != string(StateUnauthenticated) || st.Authenticated || st.Email != "" || st.TokenExpires != 0 {
		t.Fatalf("snapshot after racing login = %+v, want UNAUTHENTICATED with no account", st)
	}
	if _, err := os.Stat(paths.Tokens); !os.IsNotExist(err) {
		t.Fatalf("tokens file after racing login: stat err = %v, want not-exist", err)
	}
}
