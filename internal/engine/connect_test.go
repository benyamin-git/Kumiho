package engine

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/guardian"
	"github.com/benyamin-git/kumiho/internal/serverlist"
	"github.com/benyamin-git/kumiho/internal/settings"
	"github.com/benyamin-git/kumiho/internal/upstream"
)

// fakeSession implements upstreamSession for daemon tests.
type fakeSession struct {
	mu       sync.Mutex
	calls    [][2]string
	clientCh chan net.Conn
	up       atomic.Int64
	down     atomic.Int64
	closed   atomic.Bool
}

func newFakeSession() *fakeSession {
	return &fakeSession{clientCh: make(chan net.Conn, 4)}
}

func (f *fakeSession) OpenStream(_ context.Context, host string, port int) (net.Conn, error) {
	f.mu.Lock()
	f.calls = append(f.calls, [2]string{host, strconv.Itoa(port)})
	f.mu.Unlock()
	server, client := net.Pipe()
	select {
	case f.clientCh <- client:
	default:
		_ = client.Close()
	}
	return server, nil
}

func (f *fakeSession) Totals() (int64, int64) { return f.up.Load(), f.down.Load() }
func (f *fakeSession) IsConnected() bool      { return !f.closed.Load() }
func (f *fakeSession) Close() error           { f.closed.Store(true); return nil }
func (f *fakeSession) callsList() [][2]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][2]string(nil), f.calls...)
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func testCountries() []serverlist.Country {
	return []serverlist.Country{
		{
			Code: "REC", Name: "Recomended Location",
			Cities: []serverlist.City{{Code: "p", Name: "Recommended", Servers: []serverlist.Server{{Hostname: "p.example.net", Port: 2499}}}},
		},
		{
			Code: "DE", Name: "Germany",
			Cities: []serverlist.City{{Code: "FRA", Name: "Frankfurt", Servers: []serverlist.Server{{Hostname: "fra1.example.net", Port: 2499}}}},
		},
		{
			Code: "AR", Name: "Argentina",
			Cities: []serverlist.City{{Code: "EZE", Name: "Buenos Aires", Servers: []serverlist.Server{{Hostname: "eze1.example.net", Port: 2499}}}},
		},
	}
}

func TestResolveCandidatePrecedence(t *testing.T) {
	countries := testCountries()

	cand, err := resolveCandidate(countries, api.ConnectPayload{LocationCode: "DE", CityCode: "FRA", Server: "fra1.example.net:2499"}, settings.State{})
	if err != nil || cand.Host != "fra1.example.net" || cand.CountryCode != "DE" {
		t.Fatalf("explicit server: %+v, %v", cand, err)
	}

	cand, err = resolveCandidate(countries, api.ConnectPayload{LocationCode: "AR"}, settings.State{})
	if err != nil || cand.CountryCode != "AR" || cand.CityCode != "EZE" {
		t.Fatalf("explicit location: %+v, %v", cand, err)
	}

	cand, err = resolveCandidate(countries, api.ConnectPayload{}, settings.State{SelectedLocation: "DE", SelectedCity: "FRA", SelectedServer: "fra1.example.net:2499"})
	if err != nil || cand.Host != "fra1.example.net" {
		t.Fatalf("persisted server: %+v, %v", cand, err)
	}

	cand, err = resolveCandidate(countries, api.ConnectPayload{}, settings.State{SelectedLocation: "AR", SelectedCity: "EZE"})
	if err != nil || cand.CountryCode != "AR" {
		t.Fatalf("persisted location: %+v, %v", cand, err)
	}

	cand, err = resolveCandidate(countries, api.ConnectPayload{}, settings.State{})
	if err != nil || cand.CountryCode != "REC" {
		t.Fatalf("default should be REC: %+v, %v", cand, err)
	}

	if _, err := resolveCandidate(countries, api.ConnectPayload{Server: "nope.example.net:1"}, settings.State{}); err == nil {
		t.Fatal("unknown server should fail")
	}
	if _, err := resolveCandidate(countries, api.ConnectPayload{LocationCode: "FR"}, settings.State{}); err == nil {
		t.Fatal("unknown location should fail")
	}
}

func TestConnectValidation(t *testing.T) {
	// The full-tunnel branch below must run its assertions on every host, so
	// OpenDevice is wired to fail deterministically instead of depending on
	// the host's ability to create TUN devices.
	fp := newFakeProvider()
	fp.openErr = errors.New("test: device open refused")
	ctrl, _ := newTestControllerWithProvider(t, loginOKHandler(t), fp)
	ctx := context.Background()

	// Not signed in yet.
	err := ctrl.Connect(ctx, api.ConnectPayload{ProxyOnly: true})
	var re *api.RemoteError
	if !errors.As(err, &re) || re.Code != api.CodeNotAuthenticated {
		t.Fatalf("unauthenticated connect = %v", err)
	}

	if st := ctrl.LoginEmail("user@example.com"); st.Step != "password" {
		t.Fatalf("login step = %+v", st)
	}
	if st := ctrl.LoginPassword(ctx, "pw"); st.Step != "done" {
		t.Fatalf("login = %+v", st)
	}

	// Full-tunnel mode runs the whole chain and must surface the provider's
	// OpenDevice failure (the M3-era "full tunnel arrives in M4" guard must be
	// gone), then return the state machine to IDLE.
	ctrl.guardian.BaseURL = guardianPassServer(t, "pass-1", new(atomic.Int32)).URL
	ctrl.serverListURL = serverListServer(t).URL
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		return newFakeSession(), nil
	}
	ctrl.exitCheckURL = ""
	if err := ctrl.Connect(ctx, api.ConnectPayload{}); err == nil {
		defer ctrl.stopTunnel("test cleanup")
		t.Fatal("full-tunnel connect succeeded despite a failing OpenDevice")
	} else if errors.As(err, &re) && re.Code == api.CodeNotImplemented {
		t.Fatalf("full-tunnel connect short-circuited: %v", err)
	}
	if st := ctrl.Snapshot(); st.State != string(StateIdle) {
		t.Fatalf("state after failed full-tunnel connect = %s", st.State)
	}

	// Not connected yet: disconnect is an error.
	if err := ctrl.Disconnect(); !errors.As(err, &re) || re.Code != api.CodeBadRequest {
		t.Fatalf("disconnect while idle = %v", err)
	}
}

func guardianPassServer(t *testing.T, token string, quota *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/fpn/token" {
			http.NotFound(w, r)
			return
		}
		quota.Add(1)
		w.Header().Set("X-Quota-Limit", "1000")
		w.Header().Set("X-Quota-Remaining", "900")
		w.Header().Set("X-Quota-Reset", "1790000000")
		fmt.Fprintf(w, `{"token":%q,"expires_at":%d}`, token, time.Now().Add(2*time.Hour).Unix())
	}))
	t.Cleanup(srv.Close)
	return srv
}

func serverListServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"code":"DE","name":"Germany","cities":[{"code":"FRA","name":"Frankfurt","servers":[{"hostname":"fra1.example.net","port":2499}]}]}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestConnectProxyOnlyFlow(t *testing.T) {
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
	fake := newFakeSession()
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		return fake, nil
	}
	ctrl.exitCheckURL = ""
	ctrl.socksPortOverride = freePort(t)

	if err := ctrl.Connect(ctx, api.ConnectPayload{ProxyOnly: true}); err != nil {
		t.Fatal(err)
	}
	defer ctrl.stopTunnel("test cleanup")

	st := ctrl.Snapshot()
	if st.State != string(StateProxyOnly) || !st.ProxyOnly {
		t.Fatalf("status = %+v", st)
	}
	if st.Location != "DE" || st.Server != "fra1.example.net:2499" {
		t.Fatalf("selection = %+v", st)
	}
	if st.PassExpires == 0 {
		t.Fatalf("pass expiry not surfaced: %+v", st)
	}
	if st.Quota == nil || st.Quota.Remaining == nil || *st.Quota.Remaining != 900 {
		t.Fatalf("quota = %+v", st.Quota)
	}

	// Connect again: refused.
	var re *api.RemoteError
	if err := ctrl.Connect(ctx, api.ConnectPayload{ProxyOnly: true}); !errors.As(err, &re) || re.Code != api.CodeBadRequest {
		t.Fatalf("second connect = %v", err)
	}

	// Drive the real SOCKS listener.
	socksAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(ctrl.socksSrv.Addr().(*net.TCPAddr).Port))
	socksConn := socksConnect(t, socksAddr, "example.com", 443)
	defer socksConn.Close()

	var upstreamEnd net.Conn
	select {
	case upstreamEnd = <-fake.clientCh:
	case <-time.After(2 * time.Second):
		t.Fatal("fake session never saw OpenStream")
	}
	defer upstreamEnd.Close()

	if calls := fake.callsList(); len(calls) != 1 || calls[0][0] != "example.com" || calls[0][1] != "443" {
		t.Fatalf("upstream calls = %v", calls)
	}

	// Relay both ways through the SOCKS connection.
	if _, err := socksConn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(upstreamEnd, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "ping" {
		t.Fatalf("upstream received %q", buf)
	}
	if _, err := upstreamEnd.Write([]byte("pong")); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(socksConn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "pong" {
		t.Fatalf("socks client received %q", buf)
	}

	if err := ctrl.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if got := ctrl.Snapshot().State; got != string(StateIdle) {
		t.Fatalf("state after disconnect = %s", got)
	}
	if !fake.closed.Load() {
		t.Fatal("session was not closed on disconnect")
	}
}

// TestConnectFullTunnelOverFakeProvider drives the full tunnel against the
// fake provider and asserts the documented call and teardown order:
//
//	OpenDevice → NetConfig.Apply → DNS start → SOCKS start
//	DNS stop → NetConfig.Cleanup → device Close → SOCKS stop
func TestConnectFullTunnelOverFakeProvider(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	fp := ctrl.plat.(*fakeProvider)
	fp.dnsAddr = net.JoinHostPort("127.0.0.1", strconv.Itoa(freePort(t)))
	fp.dev = newFakeDevice(fp, fp.tun.Name)
	ctrl.guardian.BaseURL = guardianPassServer(t, "pass-1", new(atomic.Int32)).URL
	ctrl.serverListURL = serverListServer(t).URL
	ctrl.exitCheckURL = ""
	ctrl.socksPortOverride = freePort(t)
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		return newFakeSession(), nil
	}

	ctx := context.Background()
	if st := ctrl.LoginEmail("user@example.com"); st.Step != "password" {
		t.Fatalf("login step = %+v", st)
	}
	if st := ctrl.LoginPassword(ctx, "pw"); st.Step != "done" {
		t.Fatalf("login = %+v", st)
	}
	if err := ctrl.Connect(ctx, api.ConnectPayload{}); err != nil {
		t.Fatalf("full-tunnel connect: %v", err)
	}
	defer ctrl.Shutdown()

	// Start order: the provider saw OpenDevice, then NetConfig.Apply, then
	// the DNS address request that precedes dns.Server.Start. (Snapshot also
	// asks for the DNS address, so assert the prefix here.)
	wantStart := []string{"device.open", "netcfg.apply", "dns.addr"}
	if got := fp.eventList(); len(got) < len(wantStart) || !reflect.DeepEqual(got[:len(wantStart)], wantStart) {
		t.Fatalf("provider calls = %v, want prefix %v", got, wantStart)
	}

	if st := ctrl.Snapshot(); st.State != string(StateConnected) {
		t.Fatalf("state after connect = %s", st.State)
	}
	applies, _ := fp.nc.counts()
	if applies != 1 {
		t.Fatalf("NetConfig.Apply calls = %d, want 1", applies)
	}
	spec := fp.nc.lastSpec()
	if spec.TunName != "foxy0" || spec.TunAddr != "10.8.0.2" || spec.TunMTU != 8500 || !spec.KillSwitch || !spec.AllowLAN {
		t.Fatalf("applied spec = %+v", spec)
	}

	// DNS starts before SOCKS: both listeners log their bind into the ring.
	dnsIdx, socksIdx := -1, -1
	for i, e := range ctrl.log.Snapshot() {
		switch {
		case e.Tag == "dns" && strings.Contains(e.Msg, "resolver listening") && dnsIdx < 0:
			dnsIdx = i
		case e.Tag == "socks" && strings.Contains(e.Msg, "listening on") && socksIdx < 0:
			socksIdx = i
		}
	}
	if dnsIdx < 0 || socksIdx < 0 {
		t.Fatalf("start logs missing (dns=%d, socks=%d)", dnsIdx, socksIdx)
	}
	if dnsIdx > socksIdx {
		t.Fatalf("SOCKS started before DNS (dns log %d, socks log %d)", dnsIdx, socksIdx)
	}

	// Both listeners are live.
	dnsConn, err := net.DialTimeout("tcp", fp.dnsAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("DNS resolver not listening on %s: %v", fp.dnsAddr, err)
	}
	_ = dnsConn.Close()
	socksAddr := ctrl.socksSrv.Addr().String()
	socksConn, err := net.DialTimeout("tcp", socksAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("SOCKS listener not up on %s: %v", socksAddr, err)
	}
	_ = socksConn.Close()

	// Teardown witnesses: the DNS resolver must be stopped before Cleanup
	// runs, Cleanup before the device closes, and the device must close while
	// SOCKS is still listening (SOCKS stops last).
	fp.nc.onCleanup = func() {
		if c, err := net.DialTimeout("tcp", fp.dnsAddr, time.Second); err == nil {
			_ = c.Close()
			t.Error("NetConfig.Cleanup ran before the DNS resolver stopped")
		}
	}
	fp.dev.onClose = func() {
		if _, cleanups := fp.nc.counts(); cleanups == 0 {
			t.Error("device closed before NetConfig.Cleanup ran")
		}
		if c, err := net.DialTimeout("tcp", socksAddr, time.Second); err != nil {
			t.Errorf("device closed after the SOCKS listener stopped: %v", err)
		} else {
			_ = c.Close()
		}
	}

	ctrl.Shutdown()

	// Teardown provider calls close the sequence: Cleanup, then device Close.
	events := fp.eventList()
	wantTail := []string{"netcfg.cleanup", "device.close"}
	if len(events) < len(wantTail) || !reflect.DeepEqual(events[len(events)-len(wantTail):], wantTail) {
		t.Fatalf("teardown provider calls = %v, want suffix %v", events, wantTail)
	}
	if _, cleanups := fp.nc.counts(); cleanups != 1 {
		t.Fatalf("NetConfig.Cleanup calls = %d, want 1", cleanups)
	}
	if c, err := net.DialTimeout("tcp", socksAddr, time.Second); err == nil {
		_ = c.Close()
		t.Fatal("SOCKS listener still up after shutdown")
	}
}

func TestConnectQuotaFatal(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctx := context.Background()
	if st := ctrl.LoginEmail("user@example.com"); st.Step != "password" {
		t.Fatalf("login step = %+v", st)
	}
	if st := ctrl.LoginPassword(ctx, "pw"); st.Step != "done" {
		t.Fatalf("login = %+v", st)
	}

	quotaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(quotaSrv.Close)
	ctrl.guardian.BaseURL = quotaSrv.URL
	ctrl.serverListURL = serverListServer(t).URL
	ctrl.exitCheckURL = ""

	err := ctrl.Connect(ctx, api.ConnectPayload{ProxyOnly: true})
	if err == nil {
		t.Fatal("expected quota error")
	}
	if st := ctrl.Snapshot(); st.State != string(StateFatal) {
		t.Fatalf("state = %s, want FATAL", st.State)
	}

	// Disconnect clears the fatal state.
	if err := ctrl.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if st := ctrl.Snapshot(); st.State != string(StateIdle) {
		t.Fatalf("state after clearing fatal = %s", st.State)
	}
}

func TestConnectRefreshesOnGuardianRejection(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctx := context.Background()
	if st := ctrl.LoginEmail("user@example.com"); st.Step != "password" {
		t.Fatalf("login step = %+v", st)
	}
	if st := ctrl.LoginPassword(ctx, "pw"); st.Step != "done" {
		t.Fatalf("login = %+v", st)
	}

	var tokenCalls atomic.Int32
	guardSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/fpn/token":
			if tokenCalls.Add(1) == 1 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			fmt.Fprintf(w, `{"token":"pass-after-refresh","expires_at":%d}`, time.Now().Add(time.Hour).Unix())
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(guardSrv.Close)
	ctrl.guardian.BaseURL = guardSrv.URL
	ctrl.serverListURL = serverListServer(t).URL
	fake := newFakeSession()
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		return fake, nil
	}
	ctrl.exitCheckURL = ""
	ctrl.socksPortOverride = freePort(t)

	if err := ctrl.Connect(ctx, api.ConnectPayload{ProxyOnly: true}); err != nil {
		t.Fatalf("connect after refresh: %v", err)
	}
	defer ctrl.stopTunnel("test cleanup")
	if tokenCalls.Load() != 2 {
		t.Fatalf("guardian token calls = %d, want 2 (rejected, then retried)", tokenCalls.Load())
	}
}

// socksConnect performs a SOCKS5 no-auth handshake and CONNECT request.
func socksConnect(t *testing.T, addr, host string, port int) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatal(err)
	}
	if reply[0] != 5 || reply[1] != 0 {
		t.Fatalf("greeting reply = %v", reply)
	}
	req := []byte{5, 1, 0, 3, byte(len(host))}
	req = append(req, host...)
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	if _, err := conn.Write(req); err != nil {
		t.Fatal(err)
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		t.Fatal(err)
	}
	if head[1] != 0 {
		t.Fatalf("CONNECT reply = %#x", head[1])
	}
	rest := make([]byte, 6)
	if head[3] == 4 {
		rest = make([]byte, 18)
	}
	if _, err := io.ReadFull(conn, rest); err != nil {
		t.Fatal(err)
	}
	return conn
}

func TestRenewalDelay(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		pass *guardian.Pass
		want time.Duration
	}{
		{"nil pass", nil, 4 * time.Minute},
		{"unknown expiry", &guardian.Pass{}, 4 * time.Minute},
		{"ten minute pass", &guardian.Pass{ExpiresAt: now.Add(10 * time.Minute).Unix()}, 5 * time.Minute},
		{"expires soon clamps to 15s", &guardian.Pass{ExpiresAt: now.Add(40 * time.Second).Unix()}, 15 * time.Second},
		{"expired", &guardian.Pass{ExpiresAt: now.Add(-time.Minute).Unix()}, 15 * time.Second},
		{"two hour pass caps at 30m", &guardian.Pass{ExpiresAt: now.Add(2 * time.Hour).Unix()}, 30 * time.Minute},
	}
	for _, tc := range cases {
		if got := renewalDelay(tc.pass, now); got != tc.want {
			t.Fatalf("%s: delay = %s, want %s", tc.name, got, tc.want)
		}
	}
}
