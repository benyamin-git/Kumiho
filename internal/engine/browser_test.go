package engine

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/fxa"
)

func browserTestJWT(exp int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d,"sub":"uid-1"}`, exp)))
	return header + "." + payload + ".signature"
}

func browserTestController(t *testing.T) (*Controller, *httptest.Server, string) {
	t.Helper()
	exp := time.Now().Add(time.Hour).Unix()
	token := browserTestJWT(exp)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/vpn/login/verify" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Code     string `json:"code"`
			Verifier string `json:"code_verifier"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code == "" || body.Verifier == "" {
			http.Error(w, "bad verify request", http.StatusBadRequest)
			return
		}
		fmt.Fprintf(w, `{"user":{"uid":"uid-1","email":"user@example.com","displayName":"User"},"token":%q}`, token)
	}))
	t.Cleanup(srv.Close)
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	ctrl.browserBaseURL = srv.URL
	ctrl.browserPlatform = "linux"
	return ctrl, srv, token
}

func browserTestURL(t *testing.T, loginURL string) *url.URL {
	t.Helper()
	u, err := url.Parse(loginURL)
	if err != nil {
		t.Fatalf("parse login URL %q: %v", loginURL, err)
	}
	return u
}

func browserCallbackURL(t *testing.T, loginURL string) string {
	t.Helper()
	u := browserTestURL(t, loginURL)
	port := u.Query().Get("port")
	if port == "" {
		t.Fatalf("login URL has no loopback port: %s", loginURL)
	}
	return "http://127.0.0.1:" + port + "/"
}

func hitBrowserCallback(t *testing.T, loginURL, code string) {
	t.Helper()
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(browserCallbackURL(t, loginURL) + "?code=" + url.QueryEscape(code))
	if err != nil {
		t.Fatalf("browser callback: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("browser callback status = %d", resp.StatusCode)
	}
}

func assertBrowserListenerClosed(t *testing.T, loginURL string) {
	t.Helper()
	u := browserTestURL(t, loginURL)
	port := u.Query().Get("port")
	if port == "" {
		t.Fatalf("login URL has no loopback port: %s", loginURL)
	}
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 300*time.Millisecond)
	if err == nil {
		conn.Close()
		t.Fatalf("loopback listener on port %s is still accepting connections", port)
	}
}

func waitBrowserStatus(t *testing.T, ctrl *Controller, id string) api.LoginState {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, err := ctrl.LoginStatus(id)
		if err != nil {
			t.Fatalf("LoginStatus(%q): %v", id, err)
		}
		switch st.Step {
		case "done", "error", "cancelled":
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("browser sign-in did not finish; last state = %+v", st)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func browserAccountByID(t *testing.T, ctrl *Controller, id string) *Account {
	t.Helper()
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	a := findAccountLocked(ctrl.accounts, id)
	if a == nil {
		t.Fatalf("account %q not found", id)
	}
	return a
}

func TestLoginStartBrowserReturnsURL(t *testing.T) {
	ctrl, _, _ := browserTestController(t)
	ls := ctrl.LoginStart(MethodBrowser, "", "")
	defer ctrl.LoginCancel(ls.LoginID)
	if ls.Step != "browser" || ls.Method != MethodBrowser || ls.URL == "" || ls.LoginID == "" {
		t.Fatalf("browser LoginStart = %+v", ls)
	}
	u := browserTestURL(t, ls.URL)
	if u.Path != "/api/v2/vpn/login/linux" {
		t.Fatalf("login path = %q, want /api/v2/vpn/login/linux", u.Path)
	}
	if _, ok := u.Query()["state"]; ok {
		t.Fatalf("login URL unexpectedly contains state: %s", ls.URL)
	}
	if u.Query().Get("code_challenge") == "" || u.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("login URL lacks PKCE parameters: %s", ls.URL)
	}
	st, err := ctrl.LoginStatus(ls.LoginID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Step != "browser" || st.URL != ls.URL || st.LoginID != ls.LoginID {
		t.Fatalf("status before callback = %+v", st)
	}
	snap := ctrl.Snapshot()
	if snap.Login == nil || snap.Login.Step != "browser" || snap.Login.URL != ls.URL || snap.Login.LoginID != ls.LoginID {
		t.Fatalf("snapshot login = %+v", snap.Login)
	}
}

func TestBrowserLoginStartGuards(t *testing.T) {
	ctrl, _, _ := browserTestController(t)

	pending := ctrl.LoginStart(MethodPassword, "password@example.com", "")
	if pending.Step != "password" {
		t.Fatalf("password LoginStart = %+v", pending)
	}
	blocked := ctrl.LoginStart(MethodBrowser, "", "")
	if blocked.Step != "error" || !strings.Contains(blocked.Error, "finish or cancel the password") {
		t.Fatalf("browser start with password pending = %+v", blocked)
	}
	if err := ctrl.LoginCancel(""); err != nil {
		t.Fatal(err)
	}

	first := ctrl.LoginStart(MethodBrowser, "", "")
	if first.Step != "browser" {
		t.Fatalf("browser LoginStart = %+v", first)
	}
	defer ctrl.LoginCancel(first.LoginID)
	duplicate := ctrl.LoginStart(MethodBrowser, "", "")
	if duplicate.Step != "browser" || !strings.Contains(duplicate.Error, "already in progress") || duplicate.LoginID != first.LoginID {
		t.Fatalf("duplicate browser start = %+v", duplicate)
	}
	password := ctrl.LoginStart(MethodPassword, "password@example.com", "")
	if password.Step != "error" || !strings.Contains(password.Error, "cancel the browser sign-in first") {
		t.Fatalf("password start while browser active = %+v", password)
	}
	if st, err := ctrl.LoginStatus(first.LoginID); err != nil || st.Step != "browser" {
		t.Fatalf("browser state after refused password start = %+v, err = %v", st, err)
	}
}

func TestBrowserLoginCompletesAndAddsAccount(t *testing.T) {
	ctrl, _, token := browserTestController(t)
	exp := browserTestJWTExpiry(token, t)
	ls := ctrl.LoginStart(MethodBrowser, "", "")
	if ls.Step != "browser" {
		t.Fatalf("browser LoginStart = %+v", ls)
	}
	hitBrowserCallback(t, ls.URL, "abc")
	st := waitBrowserStatus(t, ctrl, ls.LoginID)
	if st.Step != "done" || st.Method != MethodBrowser || st.Email != "user@example.com" || st.Error != "" {
		t.Fatalf("browser completion = %+v", st)
	}
	ctrl.mu.Lock()
	a := ctrl.activeAccountLocked()
	ctrl.mu.Unlock()
	if a == nil || a.Method != MethodBrowser || a.Browser == nil {
		t.Fatalf("active browser account = %+v", a)
	}
	if a.Email != "user@example.com" || a.Browser.Token != token || a.Browser.ExpiresAt != exp || a.Browser.ReauthRequired {
		t.Fatalf("browser token = %+v", a.Browser)
	}
	if a.FxA != nil {
		t.Fatalf("browser account stored FxA tokens: %+v", a.FxA)
	}
	if len(ctrl.Accounts().Accounts) != 1 || !ctrl.Accounts().Accounts[0].Active {
		t.Fatalf("accounts view = %+v", ctrl.Accounts())
	}
	tokens, err := loadTokens(ctrl.store.Paths().Tokens)
	if err != nil || tokens == nil || tokens.AccessToken != token || tokens.RefreshToken != "" {
		t.Fatalf("tokens.json = %+v, err = %v", tokens, err)
	}
	loaded, _, err := loadAccounts(accountsPath(ctrl.store.Paths()))
	if err != nil || len(loaded.Accounts) != 1 {
		t.Fatalf("accounts.json = %+v, err = %v", loaded, err)
	}
}

func browserTestJWTExpiry(token string, t *testing.T) int64 {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("bad test JWT %q", token)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil || claims.Exp == 0 {
		t.Fatalf("bad test JWT claims: %v, %s", err, raw)
	}
	return claims.Exp
}

func TestBrowserLoginReauthExistingAccount(t *testing.T) {
	ctrl, srv, _ := browserTestController(t)
	srv.Close()
	exp := time.Now().Add(time.Hour).Unix()
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/vpn/login/verify" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"user":{"email":"browser@example.com"},"token":%q}`, browserTestJWT(exp))
	}))
	t.Cleanup(srv.Close)
	ctrl.browserBaseURL = srv.URL

	now := time.Now()
	oldFxA := &fxa.Tokens{AccessToken: "access-old", RefreshToken: "refresh-old", ExpiresAt: now.Add(time.Hour).Unix()}
	f := &accountsFile{
		Version:    2,
		Active:     "password-id",
		LastMethod: MethodPassword,
		Accounts: []*Account{
			{
				ID:         "browser-id",
				Email:      "browser@example.com",
				Label:      "Browser label",
				Method:     MethodBrowser,
				Browser:    &BrowserToken{Token: "old-browser-token", ExpiresAt: exp, ReauthRequired: true},
				FxA:        oldFxA,
				AddedAt:    now.Add(-2 * time.Hour),
				LastUsedAt: now.Add(-time.Hour),
			},
			{
				ID:         "password-id",
				Email:      "password@example.com",
				Label:      "Password",
				Method:     MethodPassword,
				FxA:        &fxa.Tokens{AccessToken: "access-password", ExpiresAt: now.Add(time.Hour).Unix()},
				AddedAt:    now.Add(-time.Hour),
				LastUsedAt: now,
			},
		},
	}
	if err := saveAccounts(accountsPath(ctrl.store.Paths()), f); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	ls := ctrl.LoginStart(MethodBrowser, "", "browser-id")
	if ls.Step != "browser" {
		t.Fatalf("browser LoginStart = %+v", ls)
	}
	hitBrowserCallback(t, ls.URL, "reauth")
	st := waitBrowserStatus(t, ctrl, ls.LoginID)
	if st.Step != "done" || st.Email != "browser@example.com" {
		t.Fatalf("browser reauth = %+v", st)
	}
	a := browserAccountByID(t, ctrl, "browser-id")
	if a.Browser == nil || a.Browser.ReauthRequired || a.Browser.Token == "old-browser-token" {
		t.Fatalf("browser token after reauth = %+v", a.Browser)
	}
	if a.FxA == nil || a.FxA.AccessToken != "access-old" || a.FxA.RefreshToken != "refresh-old" {
		t.Fatalf("browser reauth changed FxA tokens: %+v", a.FxA)
	}
	if a.Label != "Browser label" {
		t.Fatalf("browser reauth changed label to %q", a.Label)
	}
	if got := ctrl.Snapshot().ActiveAccount; got != "browser-id" {
		t.Fatalf("active account = %q, want browser-id", got)
	}
}

func TestBrowserLoginTargetMismatch(t *testing.T) {
	ctrl, srv, _ := browserTestController(t)
	srv.Close()
	exp := time.Now().Add(time.Hour).Unix()
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/vpn/login/verify" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"user":{"email":"other@example.com"},"token":%q}`, browserTestJWT(exp))
	}))
	t.Cleanup(srv.Close)
	ctrl.browserBaseURL = srv.URL

	now := time.Now()
	f := &accountsFile{
		Version:    2,
		Active:     "user1-id",
		LastMethod: MethodPassword,
		Accounts: []*Account{{
			ID:         "user1-id",
			Email:      "user1@example.com",
			Label:      "User One",
			Method:     MethodPassword,
			FxA:        &fxa.Tokens{AccessToken: "access-user1", ExpiresAt: now.Add(time.Hour).Unix()},
			AddedAt:    now,
			LastUsedAt: now,
		}},
	}
	paths := ctrl.store.Paths()
	if err := saveAccounts(accountsPath(paths), f); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(accountsPath(paths))
	if err != nil {
		t.Fatal(err)
	}

	ls := ctrl.LoginStart(MethodBrowser, "", "user1-id")
	if ls.Step != "browser" {
		t.Fatalf("browser LoginStart = %+v", ls)
	}
	hitBrowserCallback(t, ls.URL, "wrong-user")
	st := waitBrowserStatus(t, ctrl, ls.LoginID)
	if st.Step != "error" || !strings.Contains(strings.ToLower(st.Error), "different account") {
		t.Fatalf("target mismatch state = %+v", st)
	}
	after, err := os.ReadFile(accountsPath(paths))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("target mismatch rewrote accounts.json")
	}
	a := browserAccountByID(t, ctrl, "user1-id")
	if a.Browser != nil {
		t.Fatalf("target mismatch added a browser token: %+v", a.Browser)
	}
	snap := ctrl.Snapshot()
	if snap.ActiveAccount != "user1-id" || snap.Email != "user1@example.com" {
		t.Fatalf("target mismatch changed the session: %+v", snap)
	}
}

func TestBrowserLoginLimit(t *testing.T) {
	ctrl, srv, _ := browserTestController(t)
	srv.Close()
	exp := time.Now().Add(time.Hour).Unix()
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/vpn/login/verify" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"user":{"email":"sixth@example.com"},"token":%q}`, browserTestJWT(exp))
	}))
	t.Cleanup(srv.Close)
	ctrl.browserBaseURL = srv.URL

	now := time.Now()
	f := &accountsFile{Version: 2, LastMethod: MethodPassword}
	for i := 0; i < MaxAccounts; i++ {
		email := fmt.Sprintf("user%d@example.com", i)
		f.Accounts = append(f.Accounts, &Account{
			ID:         fmt.Sprintf("id-%d", i),
			Email:      email,
			Label:      email,
			Method:     MethodPassword,
			FxA:        &fxa.Tokens{AccessToken: fmt.Sprintf("access-%d", i), ExpiresAt: now.Add(time.Hour).Unix()},
			AddedAt:    now.Add(time.Duration(i) * time.Minute),
			LastUsedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}
	f.Active = f.Accounts[0].ID
	paths := ctrl.store.Paths()
	if err := saveAccounts(accountsPath(paths), f); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(accountsPath(paths))
	if err != nil {
		t.Fatal(err)
	}

	ls := ctrl.LoginStart(MethodBrowser, "", "")
	if ls.Step != "browser" {
		t.Fatalf("browser LoginStart = %+v", ls)
	}
	hitBrowserCallback(t, ls.URL, "sixth")
	st := waitBrowserStatus(t, ctrl, ls.LoginID)
	if st.Step != "done" || !strings.Contains(st.Error, "account limit reached") {
		t.Fatalf("sixth browser login = %+v", st)
	}
	after, err := os.ReadFile(accountsPath(paths))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("rejected sixth browser login rewrote accounts.json")
	}
	if view := ctrl.Accounts(); len(view.Accounts) != MaxAccounts {
		t.Fatalf("accounts after limit = %+v", view)
	}
	if snap := ctrl.Snapshot(); snap.ActiveAccount != f.Active {
		t.Fatalf("active account after limit = %q, want %q", snap.ActiveAccount, f.Active)
	}
}

func TestBrowserLoginCancel(t *testing.T) {
	ctrl, _, _ := browserTestController(t)
	ls := ctrl.LoginStart(MethodBrowser, "", "")
	if ls.Step != "browser" {
		t.Fatalf("browser LoginStart = %+v", ls)
	}
	if err := ctrl.LoginCancel(ls.LoginID); err != nil {
		t.Fatal(err)
	}
	st, err := ctrl.LoginStatus(ls.LoginID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Step != "cancelled" || st.Method != MethodBrowser || st.LoginID != ls.LoginID {
		t.Fatalf("cancelled state = %+v", st)
	}
	assertBrowserListenerClosed(t, ls.URL)
	next := ctrl.LoginStart(MethodBrowser, "", "")
	if next.Step != "browser" || next.LoginID == "" || next.LoginID == ls.LoginID {
		t.Fatalf("start after cancel = %+v", next)
	}
	if err := ctrl.LoginCancel(next.LoginID); err != nil {
		t.Fatal(err)
	}
}

func TestBrowserLoginTimeout(t *testing.T) {
	ctrl, _, _ := browserTestController(t)
	ctrl.browserTimeout = 30 * time.Millisecond
	ls := ctrl.LoginStart(MethodBrowser, "", "")
	if ls.Step != "browser" {
		t.Fatalf("browser LoginStart = %+v", ls)
	}
	st := waitBrowserStatus(t, ctrl, ls.LoginID)
	if st.Step != "error" || !strings.Contains(strings.ToLower(st.Error), "timed out") {
		t.Fatalf("timeout state = %+v", st)
	}
	if view := ctrl.Accounts(); len(view.Accounts) != 0 {
		t.Fatalf("timeout added an account: %+v", view)
	}
}

func TestShutdownCancelsBrowserFlow(t *testing.T) {
	ctrl, _, _ := browserTestController(t)
	ls := ctrl.LoginStart(MethodBrowser, "", "")
	if ls.Step != "browser" {
		t.Fatalf("browser LoginStart = %+v", ls)
	}
	ctrl.Shutdown()
	assertBrowserListenerClosed(t, ls.URL)
}

func TestLogoutCancelsBrowserFlow(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	exp := time.Now().Add(time.Hour).Unix()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/vpn/login/verify" {
			http.NotFound(w, r)
			return
		}
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		fmt.Fprintf(w, `{"user":{"email":"late@example.com"},"token":%q}`, browserTestJWT(exp))
	}))
	t.Cleanup(srv.Close)
	ctrl, paths := newTestController(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	ctrl.browserBaseURL = srv.URL
	ctrl.browserPlatform = "linux"

	ls := ctrl.LoginStart(MethodBrowser, "", "")
	if ls.Step != "browser" {
		t.Fatalf("browser LoginStart = %+v", ls)
	}
	hitBrowserCallback(t, ls.URL, "late")
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("verify exchange did not start")
	}
	if err := ctrl.Logout(); err != nil {
		t.Fatal(err)
	}
	assertBrowserListenerClosed(t, ls.URL)
	close(release)
	time.Sleep(150 * time.Millisecond)

	st := ctrl.Snapshot()
	if st.State != string(StateUnauthenticated) || st.Authenticated || len(st.Accounts) != 0 {
		t.Fatalf("late browser success changed the session: %+v", st)
	}
	for _, path := range []string{accountsPath(paths), paths.Tokens} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s should be absent after logout, stat err = %v", path, err)
		}
	}
}

func TestLoginStatusPasswordPending(t *testing.T) {
	ctrl, _, _ := browserTestController(t)
	ls := ctrl.LoginStart(MethodPassword, "user@example.com", "")
	if ls.Step != "password" {
		t.Fatalf("password LoginStart = %+v", ls)
	}
	st, err := ctrl.LoginStatus("")
	if err != nil {
		t.Fatal(err)
	}
	if st.Step != "password" || st.Method != "email" || st.Email != "user@example.com" {
		t.Fatalf("password pending status = %+v", st)
	}
	if err := ctrl.LoginCancel(""); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.LoginStatus(""); err == nil {
		t.Fatal("LoginStatus after cancel should report no pending flow")
	}
}
