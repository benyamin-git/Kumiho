package daemon

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/upstream"
)

// Shutdown must tear an active tunnel down (the systemd stop path); before
// this existed, stopping the service left the kill-switch rules and the nft
// table installed.
func TestShutdownTearsDownTunnel(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctx := context.Background()
	if st := ctrl.LoginEmail("user@example.com"); st.Step != "password" {
		t.Fatalf("login step = %+v", st)
	}
	if st := ctrl.LoginPassword(ctx, "pw"); st.Step != "done" {
		t.Fatalf("login = %+v", st)
	}
	var quotaCalls atomic.Int32
	ctrl.guardian.BaseURL = guardianPassServer(t, "pass-1", &quotaCalls).URL
	ctrl.serverListURL = serverListServer(t).URL
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		return newFakeSession(), nil
	}
	ctrl.exitCheckURL = ""
	ctrl.socksPortOverride = freePort(t)

	if err := ctrl.Connect(ctx, ipc.ConnectPayload{ProxyOnly: true}); err != nil {
		t.Fatal(err)
	}
	if st := ctrl.Snapshot(); st.State != string(StateProxyOnly) {
		t.Fatalf("state = %s", st.State)
	}

	ctrl.Shutdown()

	if st := ctrl.Snapshot(); st.State != string(StateIdle) {
		t.Fatalf("state after shutdown = %s", st.State)
	}
	ctrl.mu.Lock()
	socksSrv, session := ctrl.socksSrv, ctrl.session
	ctrl.mu.Unlock()
	if socksSrv != nil || session != nil {
		t.Fatalf("resources survived shutdown (socks=%v, session=%v)", socksSrv != nil, session != nil)
	}

	// Shutdown is idempotent and silent when nothing is up.
	ctrl.Shutdown()
	if st := ctrl.Snapshot(); st.State != string(StateIdle) {
		t.Fatalf("state after second shutdown = %s", st.State)
	}
}
