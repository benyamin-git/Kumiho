package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/guardian"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// testSessionHex is a syntactically valid 64-byte session token.
func testSessionHex() string {
	return strings.Repeat("ab", 64)
}

func newTestController(t *testing.T, handler http.HandlerFunc) (*Controller, settings.Paths) {
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
	ring := logging.NewRing(logging.Options{Capacity: 100})
	return New(store, ring, client), paths
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
