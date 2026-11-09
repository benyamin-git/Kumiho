package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/socks"
)

func TestRegionCheckClassification(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "vpn.mozilla.org", "api.accounts.firefox.com", "accounts.firefox.com":
			return &http.Response{StatusCode: http.StatusUnavailableForLegalReasons, Body: io.NopCloser(strings.NewReader(""))}, nil
		case "oauth.accounts.firefox.com":
			return nil, errors.New("connection refused")
		default:
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
	})
	if err := ctrl.store.SetOverride("relay_url", "https://relay.example.test"); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.store.SetRelayToken("relay-token"); err != nil {
		t.Fatal(err)
	}
	ctrl.reloadRelayToken()

	res := ctrl.RegionCheck(context.Background())
	status := make(map[string]string, len(res.Hosts))
	for _, h := range res.Hosts {
		status[h.Host] = h.Status
	}
	if status["vpn.mozilla.org"] != api.RegionBlocked || status["api.accounts.firefox.com"] != api.RegionBlocked {
		t.Fatalf("blocked statuses = %+v", status)
	}
	if status["oauth.accounts.firefox.com"] != api.RegionUnknown {
		t.Fatalf("transport error status = %q, want unknown", status["oauth.accounts.firefox.com"])
	}
	if status["profile.accounts.firefox.com"] != api.RegionReachable {
		t.Fatalf("reachable status = %q", status["profile.accounts.firefox.com"])
	}
	if !res.BrowserBlocked {
		t.Fatal("BrowserBlocked = false, want true")
	}
	if !res.RelayConfigured {
		t.Fatal("RelayConfigured = false, want true")
	}
	if len(res.Blocked) != 3 {
		t.Fatalf("Blocked = %v, want 3 hosts", res.Blocked)
	}
}

func TestPrepareOrderAndResult(t *testing.T) {
	var mu sync.Mutex
	var order []string
	record := func(step string) {
		mu.Lock()
		order = append(order, step)
		mu.Unlock()
	}
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/profile":
			record("profile")
			fmt.Fprint(w, `{"uid":"uid-1","email":"user@example.com"}`)
		default:
			loginOKHandler(t)(w, r)
		}
	}
	ctrl, _ := newTestController(t, handler)
	email := signInForPassTest(t, ctrl)

	list := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record("list")
		fmt.Fprint(w, `{"data":[{"code":"DE","name":"Germany","cities":[{"code":"FRA","name":"Frankfurt","servers":[{"hostname":"fra1.example.net","port":2499},{"hostname":"fra2.example.net","port":2499}]}]}]}`)
	}))
	defer list.Close()
	ctrl.serverListURL = list.URL

	pass := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record("pass")
		fmt.Fprintf(w, `{"token":"prepared-pass","expires_at":%d}`, time.Now().Add(10*time.Minute).Unix())
	}))
	defer pass.Close()
	ctrl.guardian.BaseURL = pass.URL
	ctrl.fxa.ProfileEndpoint = ctrl.fxa.BaseURL + "/profile"
	ctrl.exitCheckURL = ""

	res, err := ctrl.Prepare(context.Background())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	mu.Lock()
	got := strings.Join(order, ",")
	mu.Unlock()
	if got != "list,profile,pass" {
		t.Fatalf("prepare order = %q, want list,profile,pass", got)
	}
	if res.Email != email || res.Locations != 1 || res.Servers != 2 {
		t.Fatalf("PrepareResult = %+v", res)
	}
	if res.ExpiresAt <= time.Now().Unix() {
		t.Fatalf("ExpiresAt = %d", res.ExpiresAt)
	}
	if !res.ServerListRefreshed || !res.ProfileRefreshed {
		t.Fatalf("refresh flags = %+v", res)
	}
	data, err := os.ReadFile(ctrl.store.PassPath())
	if err != nil {
		t.Fatalf("pass.json: %v", err)
	}
	if !strings.Contains(string(data), "prepared-pass") {
		t.Fatalf("pass.json = %s", data)
	}
	if strings.Contains(fmt.Sprintf("%+v", res), "prepared-pass") {
		t.Fatal("PrepareResult leaked the pass token")
	}
}

func TestPrepareRefusals(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	var re *api.RemoteError
	if _, err := ctrl.Prepare(context.Background()); !errors.As(err, &re) || re.Code != api.CodeNotAuthenticated {
		t.Fatalf("signed-out Prepare = %v", err)
	}
	signInForPassTest(t, ctrl)
	ctrl.mu.Lock()
	ctrl.session = newFakeSession()
	ctrl.mu.Unlock()
	if _, err := ctrl.Prepare(context.Background()); !errors.As(err, &re) || re.Code != api.CodeBadRequest {
		t.Fatalf("connected Prepare = %v", err)
	}
}

func TestControlPlaneDialUsesTunnelWhileConnected(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))

	clientEnd, serverEnd := net.Pipe()
	var mu sync.Mutex
	var target string
	srv := &socks.Server{
		Bind: "127.0.0.1",
		Port: 0,
		Dial: func(_ context.Context, host string, port int) (net.Conn, error) {
			mu.Lock()
			target = fmt.Sprintf("%s:%d", host, port)
			mu.Unlock()
			return serverEnd, nil
		},
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	defer clientEnd.Close()

	ctrl.mu.Lock()
	ctrl.socksSrv = srv
	ctrl.session = newFakeSession()
	ctrl.mu.Unlock()

	var baseCalled atomic.Bool
	base := func(context.Context, string, string) (net.Conn, error) {
		baseCalled.Store(true)
		return nil, errors.New("base dial must not run while connected")
	}
	conn, err := ctrl.controlPlaneDial(base)(context.Background(), "tcp", "vpn.mozilla.org:443")
	if err != nil {
		t.Fatalf("tunnel dial: %v", err)
	}
	defer conn.Close()
	mu.Lock()
	got := target
	mu.Unlock()
	if got != "vpn.mozilla.org:443" {
		t.Fatalf("tunnel target = %q", got)
	}
	if baseCalled.Load() {
		t.Fatal("the direct base ran while the tunnel was live")
	}

	// Disconnecting reverts the same closure to the direct hop without
	// rebuilding any client.
	ctrl.mu.Lock()
	ctrl.session = nil
	ctrl.socksSrv = nil
	ctrl.mu.Unlock()
	var directUsed atomic.Bool
	direct, err := ctrl.controlPlaneDial(func(context.Context, string, string) (net.Conn, error) {
		directUsed.Store(true)
		c, _ := net.Pipe()
		return c, nil
	})(context.Background(), "tcp", "vpn.mozilla.org:443")
	if err != nil {
		t.Fatalf("direct dial: %v", err)
	}
	direct.Close()
	if !directUsed.Load() {
		t.Fatal("the direct hop was not used after disconnect")
	}
}
