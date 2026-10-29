package engine

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/guardian"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/platform"
	"github.com/benyamin-git/kumiho/internal/serverlist"
	"github.com/benyamin-git/kumiho/internal/settings"
	"github.com/benyamin-git/kumiho/internal/tlsplit"
)

// testSessionHex is a syntactically valid 64-byte session token.
func testSessionHex() string {
	return strings.Repeat("ab", 64)
}

func newTestController(t *testing.T, handler http.HandlerFunc) (*Controller, settings.Paths) {
	return newTestControllerWithProvider(t, handler, newFakeProvider())
}

func newTestControllerWithProvider(t *testing.T, handler http.HandlerFunc, plat platform.Provider) (*Controller, settings.Paths) {
	t.Helper()
	dir := t.TempDir()
	paths := settings.Paths{
		Config:  filepath.Join(dir, "config.toml"),
		State:   filepath.Join(dir, "state.json"),
		Tokens:  filepath.Join(dir, "tokens.json"),
		Runtime: dir,
	}
	store, err := settings.Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := fxa.NewClient()
	client.BaseURL = server.URL + "/v1"
	// Fixtures must never reach the production fallback host; tests that
	// exercise the fallback point it at their own server explicitly.
	client.FallbackBaseURL = ""
	// Keep logout's best-effort token revocation hermetic: never fall through
	// to the production destroy endpoint from a test.
	client.DestroyEndpoint = server.URL + "/v1/oauth/destroy"
	ring := logging.NewRing(logging.Options{Capacity: 100})
	return New(store, ring, client, plat), paths
}

// loginOKHandler completes a login without 2FA.
func loginOKHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/account/login":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode login body: %v", err)
				return
			}
			if body["email"] != "user@example.com" {
				t.Errorf("email = %v", body["email"])
			}
			fmt.Fprintf(w, `{"sessionToken":%q,"verified":true}`, testSessionHex())
		case "/v1/oauth/token":
			fmt.Fprint(w, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600,"scope":"profile"}`)
		default:
			http.NotFound(w, r)
		}
	}
}

func TestLoginFlowWithout2FA(t *testing.T) {
	ctrl, paths := newTestController(t, loginOKHandler(t))
	ctx := context.Background()

	if ls := ctrl.LoginEmail("user@example.com"); ls.Step != "password" {
		t.Fatalf("login_email step = %+v", ls)
	}
	ls := ctrl.LoginPassword(ctx, "pw")
	if ls.Step != "done" || ls.Error != "" {
		t.Fatalf("login_password = %+v", ls)
	}

	st := ctrl.Snapshot()
	if st.State != string(StateIdle) || !st.Authenticated || st.Email != "user@example.com" {
		t.Fatalf("snapshot = %+v", st)
	}
	if st.TokenExpires <= time.Now().Unix() {
		t.Fatalf("token expiry not set: %+v", st)
	}
	if _, err := os.Stat(paths.Tokens); err != nil {
		t.Fatalf("tokens not persisted: %v", err)
	}
	data, err := os.ReadFile(paths.State)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "user@example.com") {
		t.Fatalf("email not persisted in state: %s", data)
	}
}

func TestLoginFlowWith2FACode(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/account/login":
			fmt.Fprintf(w, `{"sessionToken":%q,"verified":false,"verificationMethod":"email-2fa"}`, testSessionHex())
		case "/v1/session/verify_code":
			fmt.Fprint(w, `{}`)
		case "/v1/oauth/token":
			fmt.Fprint(w, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600}`)
		default:
			http.NotFound(w, r)
		}
	}
	ctrl, _ := newTestController(t, handler)
	ctx := context.Background()

	ctrl.LoginEmail("user@example.com")
	ls := ctrl.LoginPassword(ctx, "pw")
	if ls.Step != "2fa" || ls.VerificationMethod != "email-2fa" || ls.Error != "" {
		t.Fatalf("login_password = %+v", ls)
	}
	ls = ctrl.Login2FA(ctx, "123456")
	if ls.Step != "done" || ls.Error != "" {
		t.Fatalf("login_2fa = %+v", ls)
	}
	if st := ctrl.Snapshot(); !st.Authenticated {
		t.Fatalf("not authenticated after 2FA: %+v", st)
	}
}

func TestLoginFlowEmailLink(t *testing.T) {
	var statusCalls atomic.Int32
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/account/login":
			fmt.Fprintf(w, `{"sessionToken":%q,"verified":false,"verificationMethod":"email"}`, testSessionHex())
		case "/v1/session/status":
			if statusCalls.Add(1) == 1 {
				fmt.Fprint(w, `{"state":"unverified"}`)
			} else {
				fmt.Fprint(w, `{"state":"verified"}`)
			}
		case "/v1/oauth/token":
			fmt.Fprint(w, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600}`)
		default:
			http.NotFound(w, r)
		}
	}
	ctrl, _ := newTestController(t, handler)
	ctx := context.Background()

	ctrl.LoginEmail("user@example.com")
	if ls := ctrl.LoginPassword(ctx, "pw"); ls.Step != "2fa" {
		t.Fatalf("login_password = %+v", ls)
	}
	ls := ctrl.Login2FA(ctx, "")
	if ls.Step != "2fa" || !strings.Contains(ls.Error, "not confirmed") {
		t.Fatalf("first link check = %+v", ls)
	}
	ls = ctrl.Login2FA(ctx, "")
	if ls.Step != "done" || ls.Error != "" {
		t.Fatalf("second link check = %+v", ls)
	}
}

func TestLogoutClearsSession(t *testing.T) {
	ctrl, paths := newTestController(t, loginOKHandler(t))
	ctx := context.Background()

	ctrl.LoginEmail("user@example.com")
	if ls := ctrl.LoginPassword(ctx, "pw"); ls.Step != "done" {
		t.Fatalf("login = %+v", ls)
	}
	if err := ctrl.Logout(); err != nil {
		t.Fatal(err)
	}

	st := ctrl.Snapshot()
	if st.State != string(StateUnauthenticated) || st.Authenticated || st.Email != "" {
		t.Fatalf("snapshot after logout = %+v", st)
	}
	if _, err := os.Stat(paths.Tokens); !os.IsNotExist(err) {
		t.Fatalf("tokens file should be gone, stat err = %v", err)
	}
}

func TestStartupRefresh(t *testing.T) {
	var refreshCalls atomic.Int32
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth/token" {
			refreshCalls.Add(1)
			fmt.Fprint(w, `{"access_token":"new-access","refresh_token":"refresh-2","expires_in":7200}`)
			return
		}
		http.NotFound(w, r)
	}
	ctrl, paths := newTestController(t, handler)
	ctx := context.Background()

	stale := &fxa.Tokens{
		AccessToken:  "old",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Hour).Unix(),
	}
	if err := saveTokens(paths.Tokens, stale); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if st := ctrl.Snapshot(); st.State != string(StateIdle) {
		t.Fatalf("refreshable session should start IDLE: %+v", st)
	}
	if err := ctrl.EnsureFreshToken(ctx); err != nil {
		t.Fatal(err)
	}
	if refreshCalls.Load() != 1 {
		t.Fatalf("refresh calls = %d, want 1", refreshCalls.Load())
	}
	data, err := os.ReadFile(paths.Tokens)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "new-access") {
		t.Fatalf("tokens not updated: %s", data)
	}
}

func TestPermanentRefreshRejectionClearsSession(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"errno":110,"message":"invalid refresh token"}`)
	}
	ctrl, paths := newTestController(t, handler)
	ctx := context.Background()

	stale := &fxa.Tokens{
		AccessToken:  "old",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Hour).Unix(),
	}
	if err := saveTokens(paths.Tokens, stale); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.EnsureFreshToken(ctx); err == nil {
		t.Fatal("expected refresh error")
	}
	if st := ctrl.Snapshot(); st.State != string(StateUnauthenticated) || st.Authenticated {
		t.Fatalf("snapshot = %+v", st)
	}
	if _, err := os.Stat(paths.Tokens); !os.IsNotExist(err) {
		t.Fatalf("tokens file should be gone, stat err = %v", err)
	}
}

func TestStartWithoutTokens(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	if err := ctrl.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := ctrl.Snapshot()
	if st.State != string(StateUnauthenticated) || st.Authenticated {
		t.Fatalf("snapshot = %+v", st)
	}
	if !st.KillSwitch || !st.Autoconnect {
		t.Fatalf("first-install defaults should be ON: %+v", st)
	}
}

func TestSelectLocationPersists(t *testing.T) {
	ctrl, paths := newTestController(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })

	if err := ctrl.SelectLocation("DE", "FRA", "fra1.example.net:2499"); err != nil {
		t.Fatal(err)
	}
	st := ctrl.Snapshot()
	if st.Location != "DE" || st.Server != "fra1.example.net:2499" {
		t.Fatalf("status = %+v", st)
	}

	data, err := os.ReadFile(paths.State)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["selected_location"] != "DE" || raw["selected_city"] != "FRA" {
		t.Fatalf("state = %v", raw)
	}

	if err := ctrl.SelectLocation("", "", "x:1"); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestLocationsFetchCacheAndFallback(t *testing.T) {
	var hits atomic.Int32
	var fail atomic.Bool
	listSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"code":"DE","name":"Germany","cities":[{"code":"FRA","name":"Frankfurt","servers":[{"hostname":"fra1.example.net","port":2499}]}]}]}`)
	}))
	defer listSrv.Close()

	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	ctrl.serverListURL = listSrv.URL
	ctx := context.Background()

	countries, err := ctrl.Locations(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(countries) != 1 || countries[0].Code != "DE" {
		t.Fatalf("countries = %+v", countries)
	}

	if _, err := ctrl.Locations(ctx, false); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("cache miss: hits = %d, want 1", hits.Load())
	}

	if _, err := ctrl.Locations(ctx, true); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Fatalf("refresh did not fetch: hits = %d, want 2", hits.Load())
	}

	fail.Store(true)
	got, err := ctrl.Locations(ctx, true)
	if err != nil {
		t.Fatalf("failed refresh should fall back to cache: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("fallback list = %+v", got)
	}
}

func TestSetSetting(t *testing.T) {
	ctrl, paths := newTestController(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })

	if err := ctrl.SetSetting("kill_switch", "off"); err != nil {
		t.Fatal(err)
	}
	if ctrl.Settings().KillSwitchEffective {
		t.Fatal("kill switch should be off")
	}
	data, err := os.ReadFile(paths.State)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["kill_switch"] != false {
		t.Fatalf("state kill_switch = %v", raw["kill_switch"])
	}

	// Connection settings are runtime-changeable overrides (M5).
	if err := ctrl.SetSetting("dns_upstream", "1.1.1.1"); err != nil {
		t.Fatal(err)
	}
	view := ctrl.Settings()
	if view.DNSUpstream != "1.1.1.1" {
		t.Fatalf("dns_upstream = %q", view.DNSUpstream)
	}
	if !containsString(view.Overridden, "dns_upstream") {
		t.Fatalf("dns_upstream not listed as overridden: %v", view.Overridden)
	}

	if err := ctrl.SetSetting("dns_upstream", "not-an-ip"); err == nil {
		t.Fatal("expected validation error for bad IP")
	}
	if got := ctrl.Settings().DNSUpstream; got != "1.1.1.1" {
		t.Fatalf("failed set changed the value: %q", got)
	}

	if err := ctrl.ApplySetting(api.SettingsSetPayload{Key: "dns_upstream", Reset: true}); err != nil {
		t.Fatal(err)
	}
	if got := ctrl.Settings().DNSUpstream; got != "" {
		t.Fatalf("dns_upstream after reset = %q", got)
	}

	err = ctrl.SetSetting("bogus", "1")
	var re *api.RemoteError
	if !errors.As(err, &re) || re.Code != api.CodeUnsupportedSetting {
		t.Fatalf("unsupported setting error = %v", err)
	}
}

// The v0.4.0 keys reach the clients like every other runtime setting: the
// view carries the effective value, the key is listed as overridden while
// the override is persisted, and a reset restores the default.
func TestSetV040Settings(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })

	cases := []struct {
		key   string
		value string
		want  any // effective value in the view after the set
		def   any // effective value in the view after the reset
		get   func(api.SettingsView) any
	}{
		{"tls_split", "sni", "sni", "off", func(v api.SettingsView) any { return v.TLSSplit }},
		{"http_proxy", "on", true, false, func(v api.SettingsView) any { return v.HTTPProxy }},
		{"http_proxy_port", "9090", 9090, 8080, func(v api.SettingsView) any { return v.HTTPProxyPort }},
		{"solve_challenges", "off", false, true, func(v api.SettingsView) any { return v.SolveChallenges }},
		{"keepalive_profile", "slow", "slow", "standard", func(v api.SettingsView) any { return v.KeepaliveProfile }},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			// The built-in default reaches the view before any override.
			if got := tc.get(ctrl.Settings()); got != tc.def {
				t.Fatalf("%s default = %v, want %v", tc.key, got, tc.def)
			}

			if err := ctrl.SetSetting(tc.key, tc.value); err != nil {
				t.Fatal(err)
			}
			view := ctrl.Settings()
			if got := tc.get(view); got != tc.want {
				t.Fatalf("%s = %v, want %v", tc.key, got, tc.want)
			}
			if !containsString(view.Overridden, tc.key) {
				t.Fatalf("%s not listed as overridden: %v", tc.key, view.Overridden)
			}

			if err := ctrl.ApplySetting(api.SettingsSetPayload{Key: tc.key, Reset: true}); err != nil {
				t.Fatal(err)
			}
			view = ctrl.Settings()
			if got := tc.get(view); got != tc.def {
				t.Fatalf("%s after reset = %v, want %v", tc.key, got, tc.def)
			}
			if containsString(view.Overridden, tc.key) {
				t.Fatalf("%s still listed as overridden: %v", tc.key, view.Overridden)
			}
		})
	}
}

// The v0.4.0 wire names are part of control protocol v1: the five keys are
// additive, so an older client that ignores unknown fields keeps working.
func TestV040SettingsWireNames(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })

	sets := map[string]string{
		"tls_split":         "sni",
		"http_proxy":        "on",
		"http_proxy_port":   "9090",
		"solve_challenges":  "off",
		"keepalive_profile": "slow",
	}
	for key, value := range sets {
		if err := ctrl.SetSetting(key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}

	data, err := json.Marshal(ctrl.Settings())
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"tls_split":         "sni",
		"http_proxy":        true,
		"http_proxy_port":   float64(9090),
		"solve_challenges":  false,
		"keepalive_profile": "slow",
	}
	for key, wantValue := range want {
		if got := raw[key]; got != wantValue {
			t.Fatalf("%s = %v, want %v", key, got, wantValue)
		}
	}
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestAccountLookup(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/fpn/status":
			if got := r.Header.Get("Authorization"); got != "Bearer access-1" {
				t.Errorf("authorization = %q", got)
			}
			fmt.Fprint(w, `{"subscribed":true,"uid":"uid-42","maxBytes":53687091200}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctrl.guardian = &guardian.Client{BaseURL: srv.URL, HTTP: srv.Client()}

	ctx := context.Background()
	ctrl.LoginEmail("user@example.com")
	if ls := ctrl.LoginPassword(ctx, "pw"); ls.Step != "done" {
		t.Fatalf("login = %+v", ls)
	}

	info := ctrl.Account(ctx)
	if info.Email != "user@example.com" {
		t.Fatalf("email = %q", info.Email)
	}
	if info.Error != "" {
		t.Fatalf("lookup error = %q", info.Error)
	}
	if info.UID != "uid-42" || !info.Subscribed || info.MaxBytes == nil || *info.MaxBytes != 53687091200 {
		t.Fatalf("account info = %+v", info)
	}
	if info.TokenExpires == 0 {
		t.Fatal("token expiry missing")
	}

	// An offline lookup keeps the local fields and reports the error.
	ctrl.guardian = &guardian.Client{BaseURL: "http://127.0.0.1:1", HTTP: &http.Client{Timeout: time.Second}}
	info = ctrl.Account(ctx)
	if info.Email != "user@example.com" || info.Error == "" {
		t.Fatalf("offline account info = %+v", info)
	}

	// Unauthenticated controllers report "not signed in" without a lookup.
	ctrl2, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {})
	info = ctrl2.Account(ctx)
	if info.Error != "not signed in" {
		t.Fatalf("unauth account info = %+v", info)
	}
}

// recordingConn captures writes in memory; the splitter only calls Write, so
// the embedded conn can stay nil.
type recordingConn struct {
	net.Conn
	mu     sync.Mutex
	writes [][]byte
}

func (c *recordingConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.writes = append(c.writes, append([]byte(nil), p...))
	c.mu.Unlock()
	return len(p), nil
}

func (c *recordingConn) recordedWrites() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]byte, len(c.writes))
	for i := range c.writes {
		out[i] = append([]byte(nil), c.writes[i]...)
	}
	return out
}

// syntheticClientHello builds a minimal, well-formed TLS ClientHello record
// carrying one server_name extension, enough for tlsplit's split points.
func syntheticClientHello(host string) []byte {
	name := []byte(host)
	serverName := []byte{
		byte((3 + len(name)) >> 8), byte(3 + len(name)),
		0x00, // host_name
		byte(len(name) >> 8), byte(len(name)),
	}
	serverName = append(serverName, name...)
	extensions := []byte{
		0x00, 0x00, // server_name
		byte(len(serverName) >> 8), byte(len(serverName)),
	}
	extensions = append(extensions, serverName...)

	body := []byte{0x03, 0x03} // client_version
	body = append(body, make([]byte, 32)...)
	body = append(body, 0x00)                   // session id length
	body = append(body, 0x00, 0x02, 0x13, 0x01) // cipher suites
	body = append(body, 0x01, 0x00)             // compression methods
	body = append(body, byte(len(extensions)>>8), byte(len(extensions)))
	body = append(body, extensions...)

	hello := []byte{0x01, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}
	hello = append(hello, body...)
	record := []byte{0x16, 0x03, 0x01, byte(len(hello) >> 8), byte(len(hello))}
	return append(record, hello...)
}

func TestUpstreamOptionsCarriesTLSSplitMode(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {})
	// Avoid any DNS lookup while building the options.
	if err := ctrl.store.SetOverride("edge_address", "203.0.113.7"); err != nil {
		t.Fatal(err)
	}
	cand := serverlist.Candidate{Host: "edge.example.net", Port: 443}
	hello := syntheticClientHello("sni.example")
	sniStart := bytes.Index(hello, []byte("sni.example"))
	if sniStart < 0 {
		t.Fatal("synthetic ClientHello does not contain the SNI hostname")
	}

	t.Run("off", func(t *testing.T) {
		if err := ctrl.store.SetOverride("tls_split", "off"); err != nil {
			t.Fatal(err)
		}
		opts, err := ctrl.upstreamOptions(context.Background(), cand, nil)
		if err != nil {
			t.Fatal(err)
		}
		if opts.WrapConn == nil {
			t.Fatal("WrapConn is nil")
		}
		rec := &recordingConn{}
		if wrapped := opts.WrapConn(rec); wrapped != rec {
			t.Fatalf("off wrapped the connection: %T", wrapped)
		}
	})

	t.Run("first-byte", func(t *testing.T) {
		if err := ctrl.store.SetOverride("tls_split", "first-byte"); err != nil {
			t.Fatal(err)
		}
		opts, err := ctrl.upstreamOptions(context.Background(), cand, nil)
		if err != nil {
			t.Fatal(err)
		}
		rec := &recordingConn{}
		wrapped := opts.WrapConn(rec)
		n, err := wrapped.Write(hello)
		if err != nil || n != len(hello) {
			t.Fatalf("write = %d, %v; want %d, nil", n, err, len(hello))
		}
		writes := rec.recordedWrites()
		if len(writes) != 2 {
			t.Fatalf("underlying writes = %d, want 2", len(writes))
		}
		if !bytes.Equal(writes[0], hello[:1]) {
			t.Fatalf("first write = %x, want %x", writes[0], hello[:1])
		}
		if !bytes.Equal(bytes.Join(writes, nil), hello) {
			t.Fatal("split writes do not reassemble to the ClientHello")
		}
	})

	t.Run("sni", func(t *testing.T) {
		if err := ctrl.store.SetOverride("tls_split", "sni"); err != nil {
			t.Fatal(err)
		}
		opts, err := ctrl.upstreamOptions(context.Background(), cand, nil)
		if err != nil {
			t.Fatal(err)
		}
		rec := &recordingConn{}
		wrapped := opts.WrapConn(rec)
		n, err := wrapped.Write(hello)
		if err != nil || n != len(hello) {
			t.Fatalf("write = %d, %v; want %d, nil", n, err, len(hello))
		}
		writes := rec.recordedWrites()
		if len(writes) != 3 {
			t.Fatalf("underlying writes = %d, want 3", len(writes))
		}
		if !bytes.Equal(writes[0], hello[:sniStart]) {
			t.Fatalf("first write = %x, want %x", writes[0], hello[:sniStart])
		}
		if !bytes.Equal(writes[1], hello[sniStart:sniStart+1]) {
			t.Fatalf("second write = %x, want %x", writes[1], hello[sniStart:sniStart+1])
		}
		if !bytes.Equal(writes[2], hello[sniStart+1:]) {
			t.Fatalf("third write = %x, want %x", writes[2], hello[sniStart+1:])
		}
		if !bytes.Equal(bytes.Join(writes, nil), hello) {
			t.Fatal("split writes do not reassemble to the ClientHello")
		}
	})
}

func TestControlPlaneDialReadsModePerDial(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {})
	hello := syntheticClientHello("sni.example")
	sniStart := bytes.Index(hello, []byte("sni.example"))
	if sniStart < 0 {
		t.Fatal("synthetic ClientHello does not contain the SNI hostname")
	}

	var calls int
	var recs []*recordingConn
	base := func(_ context.Context, network, addr string) (net.Conn, error) {
		calls++
		if network != "tcp" || addr != "control.example:443" {
			t.Errorf("base dial = %q %q, want tcp control.example:443", network, addr)
		}
		rec := &recordingConn{}
		recs = append(recs, rec)
		return rec, nil
	}
	// Build the wrapped dial once: changing the override afterwards must
	// affect subsequent calls, proving the mode is read inside the closure.
	dial := ctrl.controlPlaneDial(base)
	ctx := context.Background()
	setMode := func(mode string) {
		t.Helper()
		if err := ctrl.store.SetOverride("tls_split", mode); err != nil {
			t.Fatal(err)
		}
	}

	setMode("off")
	conn, err := dial(ctx, "tcp", "control.example:443")
	if err != nil {
		t.Fatal(err)
	}
	if conn != recs[0] {
		t.Fatalf("off conn = %T, want the base connection", conn)
	}

	setMode("first-byte")
	conn, err = dial(ctx, "tcp", "control.example:443")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%T", conn); got != "*tlsplit.splitConn" {
		t.Fatalf("first-byte conn = %s, want *tlsplit.splitConn", got)
	}
	n, err := conn.Write(hello)
	if err != nil || n != len(hello) {
		t.Fatalf("write = %d, %v; want %d, nil", n, err, len(hello))
	}
	writes := recs[1].recordedWrites()
	if len(writes) != 2 {
		t.Fatalf("first-byte underlying writes = %d, want 2", len(writes))
	}
	if !bytes.Equal(writes[0], hello[:1]) || !bytes.Equal(bytes.Join(writes, nil), hello) {
		t.Fatalf("first-byte writes do not split and reassemble the ClientHello")
	}

	setMode("sni")
	conn, err = dial(ctx, "tcp", "control.example:443")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%T", conn); got != "*tlsplit.splitConn" {
		t.Fatalf("sni conn = %s, want *tlsplit.splitConn", got)
	}
	n, err = conn.Write(hello)
	if err != nil || n != len(hello) {
		t.Fatalf("write = %d, %v; want %d, nil", n, err, len(hello))
	}
	writes = recs[2].recordedWrites()
	if len(writes) != 3 {
		t.Fatalf("sni underlying writes = %d, want 3", len(writes))
	}
	if !bytes.Equal(writes[0], hello[:sniStart]) ||
		!bytes.Equal(writes[1], hello[sniStart:sniStart+1]) ||
		!bytes.Equal(writes[2], hello[sniStart+1:]) {
		t.Fatalf("sni writes are not split at the first hostname byte")
	}
	if !bytes.Equal(bytes.Join(writes, nil), hello) {
		t.Fatal("sni writes do not reassemble to the ClientHello")
	}

	if calls != 3 {
		t.Fatalf("base dial calls = %d, want 3", calls)
	}
}

func TestUseBypassDialerWrapsEveryControlPlaneClient(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})

	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {})
	ctrl.UseBypassDialer()
	// The transports must pick this up per dial, not at UseBypassDialer time.
	if err := ctrl.store.SetOverride("tls_split", "first-byte"); err != nil {
		t.Fatal(err)
	}

	clients := []struct {
		name   string
		client *http.Client
	}{
		{"http", ctrl.httpClient},
		{"fxa", ctrl.fxa.HTTP},
		{"guardian", ctrl.guardian.HTTP},
	}
	for _, tc := range clients {
		t.Run(tc.name, func(t *testing.T) {
			tr, ok := tc.client.Transport.(*http.Transport)
			if !ok {
				t.Fatalf("transport = %T, want *http.Transport", tc.client.Transport)
			}
			conn, err := tr.DialContext(context.Background(), "tcp", ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if got := fmt.Sprintf("%T", conn); got != "*tlsplit.splitConn" {
				t.Fatalf("dialed conn = %s, want *tlsplit.splitConn", got)
			}
		})
	}
}

func TestTLSSplitModeInvalidFallsBackToOff(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {})
	bogus := "bogus"
	if err := ctrl.store.UpdateState(func(st *settings.State) { st.TLSSplit = &bogus }); err != nil {
		t.Fatal(err)
	}
	if got := ctrl.tlsSplitMode(); got != tlsplit.Off {
		t.Fatalf("tlsSplitMode = %v, want Off", got)
	}
	for _, e := range ctrl.log.Snapshot() {
		if e.Level == logging.Warn && e.Tag == "tlsplit" && strings.Contains(e.Msg, "invalid tls_split") {
			return
		}
	}
	t.Fatalf("no tlsplit warning logged: %+v", ctrl.log.Snapshot())
}

func TestUseBypassDialerTLSSplitControlPlane(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "control-plane-ok")
	}))
	defer srv.Close()

	for _, mode := range []string{"first-byte", "off"} {
		t.Run(mode, func(t *testing.T) {
			ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {})
			ctrl.UseBypassDialer()
			// DialContext reads the mode per dial, so setting it after the
			// clients were built must still affect the next request.
			if err := ctrl.store.SetOverride("tls_split", mode); err != nil {
				t.Fatal(err)
			}
			tr, ok := ctrl.httpClient.Transport.(*http.Transport)
			if !ok {
				t.Fatalf("transport = %T", ctrl.httpClient.Transport)
			}
			tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // test server certificate
			defer tr.CloseIdleConnections()

			resp, err := ctrl.httpClient.Get(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			if string(body) != "control-plane-ok" {
				t.Fatalf("body = %q", body)
			}
		})
	}
}

// A challenged login (HTTP 406) runs the real fastly solver against a
// fixture challenge page: the solve succeeds, the login retry carries the
// solved cookie, and that cookie lands in the ONE shared jar used by every
// control-plane client and by the solver itself.
func TestChallengeSolverEndToEndInstallsSharedCookie(t *testing.T) {
	const prefix = "/_fs-ch-abc123"
	var loginHits, solverHits atomic.Int32
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/account/login":
			loginHits.Add(1)
			if _, err := r.Cookie("fk"); err != nil {
				w.WriteHeader(http.StatusNotAcceptable)
				fmt.Fprint(w, "Client Challenge")
				return
			}
			fmt.Fprintf(w, `{"sessionToken":%q,"verified":true}`, testSessionHex())
		case r.URL.Path == "/v1/oauth/token":
			fmt.Fprint(w, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600}`)
		case r.URL.Path == "/":
			// The solver's isolated jar carries the challenge cookie, so
			// the verification re-fetch must come back clean.
			solverHits.Add(1)
			if _, err := r.Cookie("fk"); err == nil {
				fmt.Fprint(w, "<html>verified</html>")
				return
			}
			w.Header().Set("Set-Cookie", "fk=1; Path=/")
			fmt.Fprintf(w, `<html><body>Client Challenge <script src="%s/script.js"></script></body></html>`, prefix)
		case r.URL.Path == prefix+"/script.js":
			solverHits.Add(1)
			fmt.Fprintf(w, `init([{"ty":"clientmetrics"}], "tok-1", %q)`, prefix)
		case r.URL.Path == prefix+"/fst-post-back":
			solverHits.Add(1)
			fmt.Fprint(w, `{"status":"success","tok":"tok-1"}`)
		default:
			http.NotFound(w, r)
		}
	}
	ctrl, _ := newTestController(t, handler)
	ctx := context.Background()

	// New attaches one shared jar to every control-plane client and the
	// solver, and wires the solver into both challenge callbacks.
	if ctrl.httpClient.Jar == nil {
		t.Fatal("httpClient.Jar is nil")
	}
	if ctrl.fxa.HTTP.Jar != ctrl.httpClient.Jar || ctrl.guardian.HTTP.Jar != ctrl.httpClient.Jar {
		t.Fatal("the three control-plane clients must share one cookie jar")
	}
	if ctrl.solver == nil || ctrl.solver.SharedJar != ctrl.httpClient.Jar {
		t.Fatal("the solver must share the controller's jar")
	}
	if ctrl.solver.HTTP != ctrl.httpClient {
		t.Fatal("the solver must use the engine's control-plane client")
	}
	if ctrl.fxa.Solver == nil || ctrl.guardian.OnChallenge == nil {
		t.Fatal("the solver must be wired as fxa.Solver and guardian.OnChallenge")
	}

	if st := ctrl.LoginEmail("user@example.com"); st.Step != "password" {
		t.Fatalf("login step = %+v", st)
	}
	if ls := ctrl.LoginPassword(ctx, "pw"); ls.Step != "done" || ls.Error != "" {
		t.Fatalf("challenged login = %+v", ls)
	}
	if got := loginHits.Load(); got != 2 {
		t.Fatalf("login calls = %d, want 2 (406, then the solved retry)", got)
	}
	if got := solverHits.Load(); got < 4 {
		t.Fatalf("solver fixture hits = %d, want >= 4 (page, script, post-back, verify)", got)
	}

	// The solved cookie is visible for the challenged URL on every jar.
	base := strings.TrimSuffix(ctrl.fxa.BaseURL, "/v1")
	challengedURL, err := url.Parse(base + "/v1/account/login")
	if err != nil {
		t.Fatal(err)
	}
	cookiesPresent := func(j http.CookieJar) bool {
		for _, c := range j.Cookies(challengedURL) {
			if c.Name == "fk" && c.Value == "1" {
				return true
			}
		}
		return false
	}
	for name, j := range map[string]http.CookieJar{
		"httpClient": ctrl.httpClient.Jar,
		"fxa":        ctrl.fxa.HTTP.Jar,
		"guardian":   ctrl.guardian.HTTP.Jar,
		"solverJar":  ctrl.solver.SharedJar,
	} {
		if !cookiesPresent(j) {
			t.Fatalf("%s jar has no solved cookie for the challenged URL", name)
		}
	}
}

// solve_challenges is consulted on every Solve call (D31): toggling it
// between attempts changes the outcome immediately; off does no solver I/O
// and fails the first 406, and the two user-facing messages must differ.
func TestChallengeSolveSettingConsultedPerCall(t *testing.T) {
	var loginHits, solverHits atomic.Int32
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account/login" {
			loginHits.Add(1)
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}
		solverHits.Add(1) // anything outside /v1 is the solver probing the fixture
		http.NotFound(w, r)
	}
	ctrl, _ := newTestController(t, handler)
	ctx := context.Background()

	if st := ctrl.LoginEmail("user@example.com"); st.Step != "password" {
		t.Fatalf("login step = %+v", st)
	}
	login := func() string {
		t.Helper()
		ls := ctrl.LoginPassword(ctx, "pw")
		if ls.Step != "password" {
			t.Fatalf("login step = %+v", ls)
		}
		return ls.Error
	}

	const (
		enabledMsg  = "could not solve the anti-bot challenge; retry or disable solve_challenges"
		disabledMsg = "anti-bot challenge solving is disabled; enable solve_challenges and try again"
	)

	// Off: the first 406 fails immediately, before any solver I/O.
	if err := ctrl.SetSetting("solve_challenges", "off"); err != nil {
		t.Fatal(err)
	}
	if msg := login(); !strings.Contains(msg, disabledMsg) {
		t.Fatalf("disabled message = %q, want %q", msg, disabledMsg)
	}
	if gotL, gotS := loginHits.Load(), solverHits.Load(); gotL != 1 || gotS != 0 {
		t.Fatalf("off: login hits = %d, solver hits = %d; want 1 and 0", gotL, gotS)
	}

	// On (same controller, next attempt): the solver runs and fails with
	// the enabled wording.
	if err := ctrl.SetSetting("solve_challenges", "on"); err != nil {
		t.Fatal(err)
	}
	enabled := login()
	if !strings.Contains(enabled, enabledMsg) {
		t.Fatalf("enabled message = %q, want %q", enabled, enabledMsg)
	}
	if solverHits.Load() == 0 {
		t.Fatal("on: the solver must do I/O against the fixture")
	}
	solverAfterEnabled := solverHits.Load()

	// Toggling back off applies immediately: no further solver I/O.
	if err := ctrl.SetSetting("solve_challenges", "off"); err != nil {
		t.Fatal(err)
	}
	disabled := login()
	if !strings.Contains(disabled, disabledMsg) {
		t.Fatalf("disabled message = %q, want %q", disabled, disabledMsg)
	}
	if got := solverHits.Load(); got != solverAfterEnabled {
		t.Fatalf("off: solver I/O happened: hits = %d, want %d", got, solverAfterEnabled)
	}
	if got := loginHits.Load(); got != 3 {
		t.Fatalf("login hits = %d, want 3 (one per attempt; no retries)", got)
	}
	if enabled == disabled {
		t.Fatalf("enabled and disabled messages must differ, both %q", enabled)
	}
}

// A Fastly hard block on the primary auth host (406 with an empty body that
// no challenge page can clear) must not strand a password login: the FxA
// client replays the request against the sibling auth-server hostname.
func TestLoginFallsBackWhenPrimaryHostIsHardBlocked(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotAcceptable)
	})
	fallback := httptest.NewServer(loginOKHandler(t))
	t.Cleanup(fallback.Close)
	ctrl.fxa.FallbackBaseURL = fallback.URL + "/v1"

	if st := ctrl.LoginEmail("user@example.com"); st.Step != "password" {
		t.Fatalf("login step = %+v", st)
	}
	st := ctrl.LoginPassword(context.Background(), "pw")
	if st.Step != "done" || st.Error != "" {
		t.Fatalf("login = %+v, want a completed sign-in through the fallback", st)
	}
	accounts := ctrl.Accounts()
	if len(accounts.Accounts) != 1 || accounts.Accounts[0].Email != "user@example.com" {
		t.Fatalf("accounts = %+v", accounts)
	}
}

// A NUL or whitespace from a Windows IME/keyboard event must be rejected
// before it reaches FxA, which answers such a body with errno 107.
func TestLoginStartRejectsControlCharactersInEmail(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request to %s", r.URL.Path)
	})
	st := ctrl.LoginStart(MethodPassword, "user@example.com\x00\x00", "")
	if st.Step != "email" || st.Error != "enter a valid email address" {
		t.Fatalf("state = %+v, want the email step with a validation error", st)
	}
}

// With solve_challenges=off the Guardian path also fails on the first 406
// (no retries) and renders the disabled wording, not the enabled one.
func TestAcquirePassChallengeDisabledMessage(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctx := context.Background()
	if st := ctrl.LoginEmail("user@example.com"); st.Step != "password" {
		t.Fatalf("login step = %+v", st)
	}
	if st := ctrl.LoginPassword(ctx, "pw"); st.Step != "done" {
		t.Fatalf("login = %+v", st)
	}
	if err := ctrl.SetSetting("solve_challenges", "off"); err != nil {
		t.Fatal(err)
	}

	var challengeHits atomic.Int32
	challengeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		challengeHits.Add(1)
		w.WriteHeader(http.StatusNotAcceptable)
	}))
	t.Cleanup(challengeSrv.Close)
	ctrl.guardian.BaseURL = challengeSrv.URL

	_, err := ctrl.acquirePass(ctx)
	const want = "anti-bot challenge solving is disabled; enable solve_challenges and try again"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("acquirePass err = %v, want %q", err, want)
	}
	if strings.Contains(err.Error(), "could not solve the anti-bot challenge") {
		t.Fatalf("acquirePass err = %v, want the disabled wording, not the enabled one", err)
	}
	if got := challengeHits.Load(); got != 1 {
		t.Fatalf("challenge hits = %d, want 1 (fail on the first 406)", got)
	}
}

// UseBypassDialer rebuilds the control-plane clients: the one shared jar
// must survive on all of them, and the solver must follow the rebuilt
// control-plane client while keeping the same jar.
func TestUseBypassDialerKeepsSharedJarAndSolver(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {})
	jar := ctrl.httpClient.Jar
	if jar == nil || ctrl.fxa.HTTP.Jar != jar || ctrl.guardian.HTTP.Jar != jar {
		t.Fatal("New must attach one shared jar to all three clients")
	}
	if ctrl.solver == nil || ctrl.solver.HTTP != ctrl.httpClient || ctrl.solver.SharedJar != jar {
		t.Fatal("New must point the solver at the control-plane client and the shared jar")
	}

	ctrl.UseBypassDialer()

	if ctrl.httpClient.Jar != jar || ctrl.fxa.HTTP.Jar != jar || ctrl.guardian.HTTP.Jar != jar {
		t.Fatal("UseBypassDialer must re-attach the same jar to the rebuilt clients")
	}
	if ctrl.solver.HTTP != ctrl.httpClient {
		t.Fatal("the solver must follow the rebuilt control-plane client")
	}
	if ctrl.solver.SharedJar != jar {
		t.Fatal("the solver must keep the controller's shared jar")
	}
}
