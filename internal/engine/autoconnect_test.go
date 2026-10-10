package engine

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/guardian"
)

func logHas(c *Controller, substr string) bool {
	for _, e := range c.log.Snapshot() {
		if strings.Contains(e.Msg, substr) {
			return true
		}
	}
	return false
}

func countLog(c *Controller, substr string) int {
	n := 0
	for _, e := range c.log.Snapshot() {
		if strings.Contains(e.Msg, substr) {
			n++
		}
	}
	return n
}

func TestAutoconnectOffSkips(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	if err := ctrl.SetSetting("autoconnect", "off"); err != nil {
		t.Fatal(err)
	}
	ctrl.Autoconnect(context.Background())
	if got := ctrl.Snapshot().State; got != string(StateUnauthenticated) {
		t.Fatalf("state = %s, want UNAUTHENTICATED (no attempt)", got)
	}
	if !logHas(ctrl, "autoconnect is off") {
		t.Fatal("missing 'autoconnect is off' log line")
	}
}

func TestAutoconnectRetriesThenGivesUp(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctx := context.Background()
	ctrl.LoginEmail("user@example.com")
	if ls := ctrl.LoginPassword(ctx, "pw"); ls.Step != "done" {
		t.Fatalf("login = %+v", ls)
	}
	// Serve the server list from cache and make Guardian unreachable so each
	// attempt fails fast with a retryable error.
	ctrl.locations = testCountries()
	ctrl.locationsAt = time.Now()
	ctrl.guardian = &guardian.Client{BaseURL: "http://127.0.0.1:1", HTTP: &http.Client{Timeout: time.Second}}

	old := autoconnectDelays
	autoconnectDelays = []time.Duration{0}
	defer func() { autoconnectDelays = old }()

	ctrl.Autoconnect(ctx)

	if attempts := countLog(ctrl, "autoconnect failed"); attempts != 2 {
		t.Fatalf("attempts = %d, want 2 (one retry)", attempts)
	}
	if got := ctrl.Snapshot().State; got != string(StateIdle) {
		t.Fatalf("state after give-up = %s, want IDLE", got)
	}
}

func TestAutoconnectStopsOnSignInError(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctx := context.Background()
	ctrl.LoginEmail("user@example.com")
	if ls := ctrl.LoginPassword(ctx, "pw"); ls.Step != "done" {
		t.Fatalf("login = %+v", ls)
	}
	// Signed-in state with no tokens: Connect reports a RemoteError, which
	// must stop the retry loop after a single attempt.
	ctrl.mu.Lock()
	ctrl.tokens = nil
	ctrl.mu.Unlock()

	old := autoconnectDelays
	autoconnectDelays = []time.Duration{0}
	defer func() { autoconnectDelays = old }()

	ctrl.Autoconnect(ctx)

	if attempts := countLog(ctrl, "autoconnect failed"); attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (RemoteError stops retries)", attempts)
	}
}
