package engine

import (
	"context"
	"testing"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/guardian"
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
