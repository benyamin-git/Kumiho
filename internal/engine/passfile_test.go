package engine

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/guardian"
	"github.com/benyamin-git/kumiho/internal/upstream"
)

// signInForPassTest completes a password login so the controller has an
// active account, then returns the account email.
func signInForPassTest(t *testing.T, ctrl *Controller) string {
	t.Helper()
	ctx := context.Background()
	if st := ctrl.LoginEmail("user@example.com"); st.Step != "password" {
		t.Fatalf("login step = %+v", st)
	}
	if st := ctrl.LoginPassword(ctx, "pw"); st.Step != "done" {
		t.Fatalf("login = %+v", st)
	}
	return "user@example.com"
}

func TestPassFileRoundTripAndMode(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	email := signInForPassTest(t, ctrl)
	pass := &guardian.Pass{Token: "pass-secret", ExpiresAt: time.Now().Add(10 * time.Minute).Unix()}
	if err := ctrl.savePassFile(pass, email); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(ctrl.store.PassPath())
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("pass.json mode = %o, want 600", got)
	}
	loaded, ok := ctrl.loadPassFile()
	if !ok || loaded.Token != pass.Token || loaded.ExpiresAt != pass.ExpiresAt {
		t.Fatalf("loadPassFile = %+v, %v; want %+v", loaded, ok, pass)
	}
}

func TestLoadPassFileDeletesExpiredAndForeign(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	email := signInForPassTest(t, ctrl)

	cases := []struct {
		name  string
		pass  *guardian.Pass
		owner string
	}{
		{"expired", &guardian.Pass{Token: "old", ExpiresAt: time.Now().Add(-time.Minute).Unix()}, email},
		{"foreign", &guardian.Pass{Token: "other", ExpiresAt: time.Now().Add(time.Hour).Unix()}, "other@example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ctrl.savePassFile(tc.pass, tc.owner); err != nil {
				t.Fatal(err)
			}
			if _, ok := ctrl.loadPassFile(); ok {
				t.Fatal("loadPassFile accepted an unusable pass")
			}
			if _, err := os.Stat(ctrl.store.PassPath()); !os.IsNotExist(err) {
				t.Fatalf("pass.json still exists after rejection: %v", err)
			}
		})
	}
}

func TestCachedPassReuseWindow(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	email := signInForPassTest(t, ctrl)
	now := time.Now()

	ctrl.mu.Lock()
	ctrl.pass = &guardian.Pass{Token: "nearly-dead", ExpiresAt: now.Add(30 * time.Second).Unix()}
	ctrl.mu.Unlock()
	if got := ctrl.cachedPass(); got != nil {
		t.Fatalf("cachedPass accepted a pass with 30s left: %+v", got)
	}

	valid := &guardian.Pass{Token: "valid", ExpiresAt: now.Add(5 * time.Minute).Unix()}
	if err := ctrl.savePassFile(valid, email); err != nil {
		t.Fatal(err)
	}
	ctrl.mu.Lock()
	ctrl.pass = nil
	ctrl.mu.Unlock()
	if got := ctrl.cachedPass(); got == nil || got.Token != "valid" {
		t.Fatalf("cachedPass = %+v, want the file pass", got)
	}
}

func TestStartLoadsPreparedPass(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	email := signInForPassTest(t, ctrl)
	pass := &guardian.Pass{Token: "restart-pass", ExpiresAt: time.Now().Add(10 * time.Minute).Unix()}
	if err := ctrl.savePassFile(pass, email); err != nil {
		t.Fatal(err)
	}
	ctrl.mu.Lock()
	ctrl.pass = nil
	ctrl.passPrepared = false
	ctrl.mu.Unlock()
	if err := ctrl.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := ctrl.Snapshot(); st.PassExpires != pass.ExpiresAt {
		t.Fatalf("PassExpires = %d, want %d", st.PassExpires, pass.ExpiresAt)
	}
	ctrl.mu.Lock()
	prepared := ctrl.passPrepared
	ctrl.mu.Unlock()
	if !prepared {
		t.Fatal("Start did not remember that a pass was prepared")
	}
}

func TestStartDeletesExpiredPassButRemembersPrepare(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	email := signInForPassTest(t, ctrl)
	if err := ctrl.savePassFile(&guardian.Pass{Token: "old", ExpiresAt: time.Now().Add(-time.Minute).Unix()}, email); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctrl.mu.Lock()
	prepared := ctrl.passPrepared
	pass := ctrl.pass
	ctrl.mu.Unlock()
	if !prepared || pass != nil {
		t.Fatalf("prepared=%v pass=%+v, want prepared with no live pass", prepared, pass)
	}
	if _, err := os.Stat(ctrl.store.PassPath()); !os.IsNotExist(err) {
		t.Fatalf("expired pass file survived Start: %v", err)
	}
}

func TestMapPassError(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))

	plain := errors.New("network unreachable")
	if got := ctrl.mapPassError(plain); got != plain {
		t.Fatalf("never-prepared error = %v, want the original", got)
	}

	ctrl.mu.Lock()
	ctrl.passPrepared = true
	ctrl.mu.Unlock()
	var re *api.RemoteError
	if err := ctrl.mapPassError(plain); !errors.As(err, &re) || re.Code != api.CodeBootstrapRequired {
		t.Fatalf("prepared error = %v, want bootstrap_required", err)
	}
}

func TestBootstrapRequiredMessage(t *testing.T) {
	if !strings.Contains(bootstrapRequiredMessage, "Blocked region") {
		t.Fatalf("bootstrap message = %q, want the guide name", bootstrapRequiredMessage)
	}
}

func TestConnectReusesPreparedPassWithoutGuardian(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	signInForPassTest(t, ctrl)
	ctrl.serverListURL = serverListServer(t).URL
	ctrl.exitCheckURL = ""
	ctrl.socksPortOverride = freePort(t)
	if _, err := ctrl.Locations(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	email := "user@example.com"
	if err := ctrl.savePassFile(&guardian.Pass{Token: "prepared", ExpiresAt: time.Now().Add(10 * time.Minute).Unix()}, email); err != nil {
		t.Fatal(err)
	}
	ctrl.guardian.BaseURL = "http://127.0.0.1:1"
	ctrl.guardian.HTTP = &http.Client{Timeout: time.Second}
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		return newFakeSession(), nil
	}
	if err := ctrl.Connect(context.Background(), api.ConnectPayload{ProxyOnly: true}); err != nil {
		t.Fatalf("Connect with a prepared pass: %v", err)
	}
	defer ctrl.stopTunnel("test cleanup")
	if st := ctrl.Snapshot(); st.State != string(StateProxyOnly) {
		t.Fatalf("state = %s, want proxy-only", st.State)
	}
	ctrl.mu.Lock()
	token := ""
	if ctrl.pass != nil {
		token = ctrl.pass.Token
	}
	ctrl.mu.Unlock()
	if token != "prepared" {
		t.Fatalf("live pass = %q, want the prepared pass", token)
	}
}

func TestConnectReportsBootstrapRequiredAfterPrepare(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	signInForPassTest(t, ctrl)
	ctrl.serverListURL = serverListServer(t).URL
	ctrl.exitCheckURL = ""
	ctrl.socksPortOverride = freePort(t)
	if _, err := ctrl.Locations(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	ctrl.mu.Lock()
	ctrl.passPrepared = true
	ctrl.pass = nil
	ctrl.mu.Unlock()
	ctrl.deletePassFile()
	ctrl.guardian.BaseURL = "http://127.0.0.1:1"
	ctrl.guardian.HTTP = &http.Client{Timeout: time.Second}
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		return newFakeSession(), nil
	}

	err := ctrl.Connect(context.Background(), api.ConnectPayload{ProxyOnly: true})
	var re *api.RemoteError
	if !errors.As(err, &re) || re.Code != api.CodeBootstrapRequired {
		t.Fatalf("Connect = %v, want bootstrap_required", err)
	}
}
