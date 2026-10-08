package daemon

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/upstream"
)

func TestDialFailuresClearSelection(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	if err := ctrl.SelectLocation("DE", "FRA", "fra1.example.net:2499"); err != nil {
		t.Fatal(err)
	}
	ctrl.noteServerFailure("fra1.example.net:2499")
	ctrl.noteServerFailure("fra1.example.net:2499")
	if got := ctrl.store.State().SelectedServer; got != "fra1.example.net:2499" {
		t.Fatalf("selection cleared after only 2 failures: %q", got)
	}
	ctrl.noteServerFailure("fra1.example.net:2499")

	st := ctrl.store.State()
	if st.SelectedServer != "" || st.SelectedCity != "" || st.SelectedLocation != "" {
		t.Fatalf("selection not cleared after 3 failures: %+v", st)
	}
	if got := st.Failures["fra1.example.net:2499"]; got != 3 {
		t.Fatalf("failure count = %d, want 3", got)
	}

	ctrl.clearServerFailures("fra1.example.net:2499")
	if _, ok := ctrl.store.State().Failures["fra1.example.net:2499"]; ok {
		t.Fatal("failures not cleared after a successful connect")
	}
}

func TestConnectNotesDialFailures(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctx := context.Background()
	if st := ctrl.LoginEmail("user@example.com"); st.Step != "password" {
		t.Fatalf("login step = %+v", st)
	}
	if st := ctrl.LoginPassword(ctx, "pw"); st.Step != "done" {
		t.Fatalf("login = %+v", st)
	}
	if err := ctrl.SelectLocation("DE", "FRA", "fra1.example.net:2499"); err != nil {
		t.Fatal(err)
	}
	ctrl.guardian.BaseURL = guardianPassServer(t, "pass-1", new(atomic.Int32)).URL
	ctrl.serverListURL = serverListServer(t).URL
	ctrl.exitCheckURL = ""
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		return nil, errors.New("edge unreachable")
	}
	oldMin, oldMax := dialRetryBackoffMin, dialRetryBackoffMax
	dialRetryBackoffMin, dialRetryBackoffMax = time.Millisecond, 2*time.Millisecond
	defer func() { dialRetryBackoffMin, dialRetryBackoffMax = oldMin, oldMax }()

	if err := ctrl.Connect(ctx, ipc.ConnectPayload{}); err == nil {
		t.Fatal("expected the connect to fail")
	}
	st := ctrl.store.State()
	if st.SelectedServer != "" {
		t.Fatalf("selection not cleared: %+v", st)
	}
	if got := st.Failures["fra1.example.net:2499"]; got < 3 {
		t.Fatalf("failure count = %d, want >= 3", got)
	}
	if got := ctrl.Snapshot().State; got != string(StateIdle) {
		t.Fatalf("state = %s, want IDLE", got)
	}
}

func TestConnectClearsPriorFailures(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.noteServerFailure("fra1.example.net:2499")
	ctrl.guardian.BaseURL = guardianPassServer(t, "pass-1", new(atomic.Int32)).URL
	ctrl.serverListURL = serverListServer(t).URL
	ctrl.exitCheckURL = ""
	ctrl.socksPortOverride = freePort(t)
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		return newFakeSession(), nil
	}
	loginProxyOnly(t, ctrl)
	defer ctrl.stopTunnel("test cleanup")

	if _, ok := ctrl.store.State().Failures["fra1.example.net:2499"]; ok {
		t.Fatal("failures not cleared after a successful connect")
	}
}
