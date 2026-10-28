package engine

import (
	"bufio"
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
	"github.com/benyamin-git/kumiho/internal/dns"
	"github.com/benyamin-git/kumiho/internal/guardian"
	"github.com/benyamin-git/kumiho/internal/logging"
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

func TestDefaultDialUpstreamReturnsNilInterfaceOnError(t *testing.T) {
	sess, err := defaultDialUpstream(context.Background(), upstream.Options{})
	if err == nil {
		t.Fatal("defaultDialUpstream succeeded without a host")
	}
	if sess != nil {
		t.Fatalf("defaultDialUpstream session = %#v, want a nil interface", sess)
	}
}

// A dialer that returns a typed-nil *upstream.Session with an error must not
// turn into a data path over a nil session. Before the typed-nil guard this
// panicked in rememberEdgePeer after logging "edge session up".
func TestConnectTypedNilDialerFailsCleanly(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctx := context.Background()
	if st := ctrl.LoginEmail("user@example.com"); st.Step != "password" {
		t.Fatalf("login step = %+v", st)
	}
	if st := ctrl.LoginPassword(ctx, "pw"); st.Step != "done" {
		t.Fatalf("login = %+v", st)
	}
	ctrl.guardian.BaseURL = guardianPassServer(t, "pass-1", new(atomic.Int32)).URL
	ctrl.serverListURL = serverListServer(t).URL
	ctrl.exitCheckURL = ""

	oldMin, oldMax := dialRetryBackoffMin, dialRetryBackoffMax
	dialRetryBackoffMin, dialRetryBackoffMax = time.Millisecond, 2*time.Millisecond
	defer func() { dialRetryBackoffMin, dialRetryBackoffMax = oldMin, oldMax }()

	dialErr := errors.New("upstream: TLS handshake: certificate signed by unknown authority")
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		return (*upstream.Session)(nil), dialErr
	}

	err := ctrl.Connect(ctx, api.ConnectPayload{ProxyOnly: true})
	if err == nil {
		t.Fatal("Connect succeeded with a typed-nil session")
	}
	if !strings.Contains(err.Error(), dialErr.Error()) {
		t.Fatalf("Connect error = %v, want the dial error %v", err, dialErr)
	}
	if st := ctrl.Snapshot(); st.State != string(StateIdle) {
		t.Fatalf("state after the failed connect = %s, want idle", st.State)
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

func TestAcquirePassChallengeMessage(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctx := context.Background()
	if st := ctrl.LoginEmail("user@example.com"); st.Step != "password" {
		t.Fatalf("login step = %+v", st)
	}
	if st := ctrl.LoginPassword(ctx, "pw"); st.Step != "done" {
		t.Fatalf("login = %+v", st)
	}

	challengeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotAcceptable)
	}))
	t.Cleanup(challengeSrv.Close)
	ctrl.guardian.BaseURL = challengeSrv.URL

	_, err := ctrl.acquirePass(ctx)
	const want = "could not solve the anti-bot challenge; retry or disable solve_challenges"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("acquirePass err = %v, want %q", err, want)
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

// TestKillSwitchReapplyFailureIsReportedAndReverts is the regression guard for
// the silent-failure path: toggling kill_switch on a live full tunnel when the
// netcfg re-apply fails must (a) surface an error instead of reporting success
// and (b) leave the persisted override at its previous value, so the UI never
// shows a protection that is not actually enforced.
func TestKillSwitchReapplyFailureIsReportedAndReverts(t *testing.T) {
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

	on, off := true, false
	cases := []struct {
		name          string
		prior         *bool  // override before the failed set; nil = admin config applies
		priorSet      string // value written to establish prior ("" => Reset)
		flip          string // value whose apply fails; must not be adopted
		wantEffective bool
	}{
		{"no prior override", nil, "", "off", true},
		{"prior on", &on, "on", "off", true},
		{"prior off", &off, "off", "on", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fp.nc.setApplyErr(nil)
			if tc.prior == nil {
				if err := ctrl.ApplySetting(api.SettingsSetPayload{Key: "kill_switch", Reset: true}); err != nil {
					t.Fatalf("reset override: %v", err)
				}
			} else if err := ctrl.SetSetting("kill_switch", tc.priorSet); err != nil {
				t.Fatalf("establish prior override: %v", err)
			}

			fp.nc.setApplyErr(errors.New("test: kill switch engine unavailable"))
			err := ctrl.SetSetting("kill_switch", tc.flip)
			var re *api.RemoteError
			if !errors.As(err, &re) || re.Code != api.CodeInternal {
				t.Fatalf("failed re-apply error = %v, want RemoteError code %q", err, api.CodeInternal)
			}
			if got := ctrl.Settings().KillSwitchEffective; got != tc.wantEffective {
				t.Fatalf("kill switch effective = %v, want %v (override not reverted)", got, tc.wantEffective)
			}
			st := ctrl.store.State()
			switch {
			case tc.prior == nil && st.KillSwitch != nil:
				t.Fatalf("override = %v, want nil", *st.KillSwitch)
			case tc.prior != nil && (st.KillSwitch == nil || *st.KillSwitch != *tc.prior):
				t.Fatalf("override = %v, want %v", st.KillSwitch, *tc.prior)
			}
		})
	}
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

// dnsCapture observes the constructors startTunnel uses, so the tests can
// assert that one resolver is shared by the local DNS listener and SOCKS.
type dnsCapture struct {
	resolverCalls atomic.Int32
	listenerCalls atomic.Int32
	resolver      *dns.Resolver
	listenerArg   *dns.Resolver
}

func installDNSCapture(t *testing.T) *dnsCapture {
	t.Helper()
	oldResolver, oldListener := newDNSResolver, newDNSListener
	t.Cleanup(func() {
		newDNSResolver, newDNSListener = oldResolver, oldListener
	})
	c := &dnsCapture{}
	newDNSResolver = func(dial dns.Dialer, provider, upstream string, logf func(level logging.Level, tag, format string, args ...any)) (*dns.Resolver, error) {
		c.resolverCalls.Add(1)
		r, err := oldResolver(dial, provider, upstream, logf)
		c.resolver = r
		return r, err
	}
	newDNSListener = func(r *dns.Resolver) *dns.Server {
		c.listenerCalls.Add(1)
		c.listenerArg = r
		return oldListener(r)
	}
	return c
}

func TestStartTunnelSharesDNSResolver(t *testing.T) {
	t.Run("proxy-only builds one resolver with no DNS listener", func(t *testing.T) {
		ctrl, _ := newTestController(t, loginOKHandler(t))
		ctrl.socksPortOverride = freePort(t)
		ctrl.mu.Lock()
		ctrl.session = newFakeSession()
		ctrl.mu.Unlock()

		capture := installDNSCapture(t)
		res, err := ctrl.startTunnel(context.Background(), true)
		if err != nil {
			t.Fatalf("startTunnel(proxy-only): %v", err)
		}
		defer res.Close()

		if got := capture.resolverCalls.Load(); got != 1 {
			t.Fatalf("resolver constructions = %d, want exactly 1", got)
		}
		if got := capture.listenerCalls.Load(); got != 0 {
			t.Fatalf("DNS listeners started = %d, want 0 in proxy-only mode", got)
		}
		if res.dns != nil {
			t.Fatal("proxy-only mode started the local DNS listener")
		}
		if res.socks == nil || res.socks.Resolve == nil {
			t.Fatal("proxy-only SOCKS server has no resolver wired")
		}
		if capture.resolver == nil {
			t.Fatal("startTunnel did not build a resolver")
		}
	})

	t.Run("full starts the listener with the same resolver", func(t *testing.T) {
		ctrl, _ := newTestController(t, loginOKHandler(t))
		fp := ctrl.plat.(*fakeProvider)
		fp.dnsAddr = "127.0.0.1:0"
		ctrl.socksPortOverride = freePort(t)
		ctrl.mu.Lock()
		ctrl.session = newFakeSession()
		ctrl.mu.Unlock()

		capture := installDNSCapture(t)
		res, err := ctrl.startTunnel(context.Background(), false)
		if err != nil {
			t.Fatalf("startTunnel(full): %v", err)
		}
		defer res.Close()

		if got := capture.resolverCalls.Load(); got != 1 {
			t.Fatalf("resolver constructions = %d, want exactly 1", got)
		}
		if got := capture.listenerCalls.Load(); got != 1 {
			t.Fatalf("DNS listener constructions = %d, want 1", got)
		}
		if capture.listenerArg == nil || capture.listenerArg != capture.resolver {
			t.Fatal("the DNS listener was not built with the shared resolver")
		}
		if res.dns == nil {
			t.Fatal("full mode did not start the local DNS listener")
		}
		if res.socks == nil || res.socks.Resolve == nil {
			t.Fatal("full-mode SOCKS server has no resolver wired")
		}
	})

	t.Run("userspace DNS does not bind the host listener", func(t *testing.T) {
		ctrl, _ := newTestController(t, loginOKHandler(t))
		fp := ctrl.plat.(*fakeProvider)
		fp.dnsAddr = "10.8.0.1:53"
		ctrl.plat = userspaceDNSProvider{fp}
		ctrl.socksPortOverride = freePort(t)
		ctrl.mu.Lock()
		ctrl.session = newFakeSession()
		ctrl.mu.Unlock()

		capture := installDNSCapture(t)
		res, err := ctrl.startTunnel(context.Background(), false)
		if err != nil {
			t.Fatalf("startTunnel(full, userspace DNS): %v", err)
		}
		defer res.Close()

		if got := capture.resolverCalls.Load(); got != 1 {
			t.Fatalf("resolver constructions = %d, want exactly 1", got)
		}
		if got := capture.listenerCalls.Load(); got != 1 {
			t.Fatalf("DNS listener constructions = %d, want 1", got)
		}
		if res.dns != nil {
			t.Fatal("userspace DNS started a privileged host listener")
		}
		if capture.listenerArg == nil || capture.listenerArg != capture.resolver {
			t.Fatal("the userspace DNS server was not built with the shared resolver")
		}
		if res.socks == nil || res.socks.Resolve == nil {
			t.Fatal("userspace DNS SOCKS server has no resolver wired")
		}
	})

	t.Run("DNS start failure tears the full path down", func(t *testing.T) {
		occupied, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
		if err != nil {
			t.Fatal(err)
		}
		defer occupied.Close()

		ctrl, _ := newTestController(t, loginOKHandler(t))
		fp := ctrl.plat.(*fakeProvider)
		fp.dnsAddr = occupied.LocalAddr().String()
		ctrl.socksPortOverride = freePort(t)
		ctrl.mu.Lock()
		ctrl.session = newFakeSession()
		ctrl.mu.Unlock()

		capture := installDNSCapture(t)
		res, err := ctrl.startTunnel(context.Background(), false)
		if err == nil || !strings.Contains(err.Error(), "start DNS resolver") {
			t.Fatalf("startTunnel error = %v, want a DNS listener failure", err)
		}
		if res != nil {
			res.Close()
			t.Fatal("failed startTunnel returned resources")
		}
		if got := capture.resolverCalls.Load(); got != 1 {
			t.Fatalf("resolver constructions = %d, want 1", got)
		}
		if got := capture.listenerCalls.Load(); got != 1 {
			t.Fatalf("DNS listener constructions = %d, want 1", got)
		}
		if applies, cleanups := fp.nc.counts(); applies != 1 || cleanups != 1 {
			t.Fatalf("netcfg apply/cleanup = %d/%d, want 1/1", applies, cleanups)
		}
		events := strings.Join(fp.eventList(), ",")
		if !strings.Contains(events, "device.close") {
			t.Fatalf("provider events = %v, want a device close", fp.eventList())
		}
		socksAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(ctrl.socksPortOverride))
		if conn, derr := net.DialTimeout("tcp", socksAddr, 200*time.Millisecond); derr == nil {
			_ = conn.Close()
			t.Fatal("SOCKS listener was started despite the DNS failure")
		}
	})

	t.Run("SOCKS listen failure still built the resolver", func(t *testing.T) {
		held, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer held.Close()

		ctrl, _ := newTestController(t, loginOKHandler(t))
		ctrl.socksPortOverride = held.Addr().(*net.TCPAddr).Port
		ctrl.mu.Lock()
		ctrl.session = newFakeSession()
		ctrl.mu.Unlock()

		capture := installDNSCapture(t)
		res, err := ctrl.startTunnel(context.Background(), true)
		if err == nil || !strings.Contains(err.Error(), "listen") {
			t.Fatalf("startTunnel error = %v, want a SOCKS listen failure", err)
		}
		if res != nil {
			res.Close()
			t.Fatal("failed startTunnel returned resources")
		}
		if got := capture.resolverCalls.Load(); got != 1 {
			t.Fatalf("resolver constructions = %d, want 1 before the SOCKS branch", got)
		}
	})
}

// twoFreePorts returns distinct free loopback ports: SOCKS first, HTTP proxy
// second.
func twoFreePorts(t *testing.T) (socksPort, httpPort int) {
	t.Helper()
	socksPort = freePort(t)
	for httpPort = freePort(t); httpPort == socksPort; httpPort = freePort(t) {
	}
	return socksPort, httpPort
}

func enableHTTPProxy(t *testing.T, ctrl *Controller, port int) {
	t.Helper()
	if err := ctrl.store.SetOverride("http_proxy", "on"); err != nil {
		t.Fatalf("enable http_proxy: %v", err)
	}
	if err := ctrl.store.SetOverride("http_proxy_port", strconv.Itoa(port)); err != nil {
		t.Fatalf("set http_proxy_port: %v", err)
	}
}

func setLiveSession(ctrl *Controller) *fakeSession {
	sess := newFakeSession()
	ctrl.mu.Lock()
	ctrl.session = sess
	ctrl.mu.Unlock()
	return sess
}

func assertListening(t *testing.T, port int) net.Conn {
	t.Helper()
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("nothing listening on %s: %v", addr, err)
	}
	return conn
}

func assertPortClosed(t *testing.T, port int) {
	t.Helper()
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatalf("listener still accepts on %s", addr)
	}
}

// TestStartTunnelHTTPProxyOn proves the gated listener is the HTTP proxy: an
// origin-form request (not a proxy request) is answered with 400, and Close
// stops both listeners.
func TestStartTunnelHTTPProxyOn(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	socksPort, httpPort := twoFreePorts(t)
	ctrl.socksPortOverride = socksPort
	enableHTTPProxy(t, ctrl, httpPort)
	setLiveSession(ctrl)

	res, err := ctrl.startTunnel(context.Background(), true)
	if err != nil {
		t.Fatalf("startTunnel(proxy-only): %v", err)
	}
	if res.httpProxy == nil {
		res.Close()
		t.Fatal("http_proxy=on did not start the HTTP proxy")
	}
	wantAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(httpPort))
	if got := res.httpProxy.Addr(); got == nil || got.String() != wantAddr {
		res.Close()
		t.Fatalf("HTTP proxy addr = %v, want %s", got, wantAddr)
	}

	conn := assertListening(t, httpPort)
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"); err != nil {
		_ = conn.Close()
		res.Close()
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		_ = conn.Close()
		res.Close()
		t.Fatalf("read proxy response: %v", err)
	}
	_ = resp.Body.Close()
	_ = conn.Close()
	if resp.StatusCode != http.StatusBadRequest {
		res.Close()
		t.Fatalf("origin-form response = %d, want 400", resp.StatusCode)
	}

	res.Close()
	res.Close() // idempotent
	assertPortClosed(t, httpPort)
	assertPortClosed(t, socksPort)
}

// TestStartTunnelHTTPProxyOff keeps the default off: the configured port stays
// silent even though http_proxy_port is set.
func TestStartTunnelHTTPProxyOff(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	socksPort, httpPort := twoFreePorts(t)
	ctrl.socksPortOverride = socksPort
	if err := ctrl.store.SetOverride("http_proxy_port", strconv.Itoa(httpPort)); err != nil {
		t.Fatalf("set http_proxy_port: %v", err)
	}
	setLiveSession(ctrl)

	res, err := ctrl.startTunnel(context.Background(), true)
	if err != nil {
		t.Fatalf("startTunnel(proxy-only): %v", err)
	}
	defer res.Close()
	if res.httpProxy != nil {
		t.Fatal("http_proxy=off still started the HTTP proxy")
	}
	assertPortClosed(t, httpPort)
}

// TestStartTunnelHTTPProxyPortConflict asserts the failure is attributed to
// the HTTP proxy and the already-started listeners are torn down.
func TestStartTunnelHTTPProxyPortConflict(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	httpPort := held.Addr().(*net.TCPAddr).Port
	socksPort := freePort(t)
	for socksPort == httpPort {
		socksPort = freePort(t)
	}

	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.socksPortOverride = socksPort
	enableHTTPProxy(t, ctrl, httpPort)
	setLiveSession(ctrl)

	res, err := ctrl.startTunnel(context.Background(), true)
	if err == nil {
		if res != nil {
			res.Close()
		}
		t.Fatal("startTunnel succeeded despite an occupied HTTP proxy port")
	}
	if !strings.Contains(err.Error(), "start http proxy:") {
		t.Fatalf("startTunnel error = %v, want the start http proxy prefix", err)
	}
	if res != nil {
		res.Close()
		t.Fatal("failed startTunnel returned resources")
	}

	// The failure path released the SOCKS listener it had already started.
	assertPortClosed(t, socksPort)

	// The held listener is untouched and still usable.
	conn, err := net.DialTimeout("tcp", held.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("held listener stopped accepting: %v", err)
	}
	tcpHeld := held.(*net.TCPListener)
	if err := tcpHeld.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	accepted, err := tcpHeld.Accept()
	if err != nil {
		_ = conn.Close()
		t.Fatalf("held listener accept: %v", err)
	}
	_ = accepted.Close()
	_ = conn.Close()
}

// TestConnectHTTPProxyStopsOnDisconnect drives a real Connect/Disconnect and
// asserts both listeners stop and the upstream session closes.
func TestConnectHTTPProxyStopsOnDisconnect(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctx := context.Background()
	if st := ctrl.LoginEmail("user@example.com"); st.Step != "password" {
		t.Fatalf("login step = %+v", st)
	}
	if st := ctrl.LoginPassword(ctx, "pw"); st.Step != "done" {
		t.Fatalf("login = %+v", st)
	}

	ctrl.guardian.BaseURL = guardianPassServer(t, "pass-1", new(atomic.Int32)).URL
	ctrl.serverListURL = serverListServer(t).URL
	fake := newFakeSession()
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		return fake, nil
	}
	ctrl.exitCheckURL = ""
	socksPort, httpPort := twoFreePorts(t)
	ctrl.socksPortOverride = socksPort
	enableHTTPProxy(t, ctrl, httpPort)

	if err := ctrl.Connect(ctx, api.ConnectPayload{ProxyOnly: true}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	for _, port := range []int{socksPort, httpPort} {
		conn := assertListening(t, port)
		_ = conn.Close()
	}

	if err := ctrl.Disconnect(); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	assertPortClosed(t, socksPort)
	assertPortClosed(t, httpPort)
	if !fake.closed.Load() {
		t.Fatal("session was not closed on disconnect")
	}
}

// TestStartTunnelHTTPProxyFullMode keeps the full-tunnel lifecycle light: the
// same resource object starts and stops the HTTP proxy.
func TestStartTunnelHTTPProxyFullMode(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	fp := ctrl.plat.(*fakeProvider)
	fp.dnsAddr = "127.0.0.1:0"
	socksPort, httpPort := twoFreePorts(t)
	ctrl.socksPortOverride = socksPort
	enableHTTPProxy(t, ctrl, httpPort)
	setLiveSession(ctrl)

	res, err := ctrl.startTunnel(context.Background(), false)
	if err != nil {
		t.Fatalf("startTunnel(full): %v", err)
	}
	if res.httpProxy == nil {
		res.Close()
		t.Fatal("full mode with http_proxy=on did not start the HTTP proxy")
	}
	conn := assertListening(t, httpPort)
	_ = conn.Close()

	res.Close()
	assertPortClosed(t, httpPort)
	assertPortClosed(t, socksPort)
}

// assertKeepalive pins the full keepalive contract of one upstream.Options.
func assertKeepalive(t *testing.T, opts upstream.Options, wantTimeout time.Duration) {
	t.Helper()
	if opts.KeepaliveTimeout != wantTimeout {
		t.Errorf("KeepaliveTimeout = %s, want %s", opts.KeepaliveTimeout, wantTimeout)
	}
	if opts.KeepaliveIdle != 15*time.Second {
		t.Errorf("KeepaliveIdle = %s, want 15s", opts.KeepaliveIdle)
	}
	if opts.KeepaliveCheck != 3*time.Second {
		t.Errorf("KeepaliveCheck = %s, want 3s", opts.KeepaliveCheck)
	}
}

func TestKeepaliveProfiles(t *testing.T) {
	cases := []struct {
		name    string
		profile string // empty leaves the default config untouched
		timeout time.Duration
	}{
		{name: "default", timeout: 10 * time.Second},
		{name: "standard", profile: "standard", timeout: 10 * time.Second},
		{name: "slow", profile: "slow", timeout: 25 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl, _ := newTestController(t, loginOKHandler(t))
			// Keep the edge resolution off the network.
			ctrl.bootstrap = []bootstrapResolver{fakeBootstrap{ip: "203.0.113.7"}}
			if tc.profile != "" {
				if err := ctrl.store.SetOverride("keepalive_profile", tc.profile); err != nil {
					t.Fatalf("set keepalive_profile=%s: %v", tc.profile, err)
				}
			}

			opts, err := ctrl.upstreamOptions(context.Background(),
				serverlist.Candidate{Host: "p.m1.example.net", Port: 2499}, nil)
			if err != nil {
				t.Fatal(err)
			}
			assertKeepalive(t, opts, tc.timeout)
			if opts.PinnedIP != "203.0.113.7" {
				t.Errorf("PinnedIP = %q, want the bootstrap address", opts.PinnedIP)
			}
		})
	}
}

func TestKeepaliveProfileReadAtConnectTime(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.bootstrap = []bootstrapResolver{fakeBootstrap{ip: "203.0.113.7"}}
	cand := serverlist.Candidate{Host: "p.m1.example.net", Port: 2499}

	before, err := ctrl.upstreamOptions(context.Background(), cand, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertKeepalive(t, before, 10*time.Second)

	if err := ctrl.store.SetOverride("keepalive_profile", "slow"); err != nil {
		t.Fatalf("set keepalive_profile=slow: %v", err)
	}

	after, err := ctrl.upstreamOptions(context.Background(), cand, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertKeepalive(t, after, 25*time.Second)
	if after.KeepaliveIdle != before.KeepaliveIdle || after.KeepaliveCheck != before.KeepaliveCheck {
		t.Fatalf("idle/check changed with the profile: before=%s/%s after=%s/%s",
			before.KeepaliveIdle, before.KeepaliveCheck, after.KeepaliveIdle, after.KeepaliveCheck)
	}
}
