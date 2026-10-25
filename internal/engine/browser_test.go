package engine

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/fxa"
)

const (
	testBrowserEmail   = "user@example.com"
	testBrowserAccess  = "secret-access-token-9f2"
	testBrowserRefresh = "secret-refresh-token-9f2"
	testBrowserCode    = "secret-code-9f2"
)

const (
	testBrowserTokenBody   = `{"access_token":"` + testBrowserAccess + `","refresh_token":"` + testBrowserRefresh + `","scope":"profile https://identity.mozilla.com/apps/vpn","expires_in":3600}`
	testBrowserProfileBody = `{"uid":"uid-1","email":"` + testBrowserEmail + `"}`
)

// fxaTestServer mocks the three absolute FxA OAuth endpoints the browser flow
// uses. Each response can be changed between calls so tests can model a
// transient failure followed by a successful retry.
type fxaTestServer struct {
	t   *testing.T
	URL string
	srv *httptest.Server

	mu            sync.Mutex
	tokenStatus   int
	tokenBody     string
	tokenCalls    int
	tokenRequest  map[string]string
	profileStatus int
	profileBody   string
	profileCalls  int
	profileAuth   string
	destroyCalls  int
}

func newFXATestServer(t *testing.T) *fxaTestServer {
	t.Helper()
	s := &fxaTestServer{
		t:             t,
		tokenStatus:   http.StatusOK,
		tokenBody:     testBrowserTokenBody,
		profileStatus: http.StatusOK,
		profileBody:   testBrowserProfileBody,
	}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	s.URL = s.srv.URL
	t.Cleanup(s.srv.Close)
	return s
}

func (s *fxaTestServer) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/token":
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.t.Errorf("decode token request: %v", err)
		}
		s.mu.Lock()
		s.tokenCalls++
		s.tokenRequest = body
		status, response := s.tokenStatus, s.tokenBody
		s.mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, response)
	case "/profile":
		s.mu.Lock()
		s.profileCalls++
		s.profileAuth = r.Header.Get("Authorization")
		status, response := s.profileStatus, s.profileBody
		s.mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, response)
	case "/destroy":
		s.mu.Lock()
		s.destroyCalls++
		s.mu.Unlock()
		_, _ = io.WriteString(w, `{}`)
	default:
		http.NotFound(w, r)
	}
}

func (s *fxaTestServer) setToken(status int, body string) {
	s.mu.Lock()
	s.tokenStatus, s.tokenBody = status, body
	s.mu.Unlock()
}

func (s *fxaTestServer) setProfile(status int, body string) {
	s.mu.Lock()
	s.profileStatus, s.profileBody = status, body
	s.mu.Unlock()
}

func (s *fxaTestServer) tokenSnapshot() (calls int, request map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copied := make(map[string]string, len(s.tokenRequest))
	for k, v := range s.tokenRequest {
		copied[k] = v
	}
	return s.tokenCalls, copied
}

func (s *fxaTestServer) profileSnapshot() (calls int, auth string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.profileCalls, s.profileAuth
}

func newBrowserTestController(t *testing.T) (*Controller, *fxaTestServer) {
	t.Helper()
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	srv := newFXATestServer(t)
	ctrl.fxa.TokenEndpoint = srv.URL + "/token"
	ctrl.fxa.ProfileEndpoint = srv.URL + "/profile"
	ctrl.fxa.DestroyEndpoint = srv.URL + "/destroy"
	return ctrl, srv
}

func browserAuthorizationURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse authorization URL %q: %v", raw, err)
	}
	return u
}

func browserAuthorizationState(t *testing.T, raw string) string {
	t.Helper()
	state := browserAuthorizationURL(t, raw).Query().Get("state")
	if state == "" {
		t.Fatalf("authorization URL has no state: %s", raw)
	}
	return state
}

func browserRedirectURL(state, code string) string {
	q := url.Values{"code": {code}, "state": {state}}
	return fxa.OAuthRedirectURI + "?" + q.Encode()
}

func startBrowserLogin(t *testing.T, ctrl *Controller, email, accountID string) api.LoginState {
	t.Helper()
	ls := ctrl.LoginStart(MethodBrowser, email, accountID)
	if ls.Step != "browser" || ls.Method != MethodBrowser || ls.URL == "" || ls.LoginID == "" {
		t.Fatalf("browser LoginStart = %+v", ls)
	}
	return ls
}

func browserStatus(t *testing.T, ctrl *Controller, id string) api.LoginState {
	t.Helper()
	st, err := ctrl.LoginStatus(id)
	if err != nil {
		t.Fatalf("LoginStatus(%q): %v", id, err)
	}
	return st
}

func requireBrowserAccount(t *testing.T, ctrl *Controller) *Account {
	t.Helper()
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	a := ctrl.activeAccountLocked()
	if a == nil {
		t.Fatal("no active account")
	}
	return a
}

func requireNoSecretsInLogs(t *testing.T, ctrl *Controller, secrets ...string) {
	t.Helper()
	for _, entry := range ctrl.log.Snapshot() {
		for _, secret := range secrets {
			if secret != "" && strings.Contains(entry.Msg, secret) {
				t.Fatalf("log entry leaks a sign-in secret: %q", entry.Msg)
			}
		}
	}
}

func requireNoSecretsInError(t *testing.T, err error, secrets ...string) {
	t.Helper()
	if err == nil {
		return
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaks a sign-in secret: %v", err)
		}
	}
}

func TestBrowserLoginStartBuildsAuthorizationURL(t *testing.T) {
	ctrl, _ := newBrowserTestController(t)
	if ctrl.browserTimeout != 10*time.Minute {
		t.Fatalf("browserTimeout = %v, want 10m", ctrl.browserTimeout)
	}

	ls := startBrowserLogin(t, ctrl, "", "")
	defer ctrl.LoginCancel(ls.LoginID)

	u := browserAuthorizationURL(t, ls.URL)
	if u.Scheme != "https" || u.Host != "accounts.firefox.com" || u.Path != "/authorization" {
		t.Fatalf("authorization URL = %s", ls.URL)
	}
	q := u.Query()
	want := map[string]string{
		"client_id":             fxa.ClientID,
		"redirect_uri":          fxa.OAuthRedirectURI,
		"response_type":         "code",
		"scope":                 fxa.Scope,
		"access_type":           "offline",
		"code_challenge_method": "S256",
	}
	for key, value := range want {
		if got := q.Get(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
	if q.Get("state") == "" || q.Get("code_challenge") == "" {
		t.Fatalf("authorization URL lacks state or PKCE challenge: %s", ls.URL)
	}
	if q.Has("email") || q.Has("action") {
		t.Errorf("add-account URL unexpectedly pre-selects an account: %s", ls.URL)
	}
	if q.Has("client_secret") {
		t.Error("authorization URL must not include a client secret")
	}

	ctrl.mu.Lock()
	b := ctrl.browser
	verifier, oauthState := b.verifier, b.oauthState
	ctrl.mu.Unlock()
	if oauthState != q.Get("state") {
		t.Fatalf("stored state = %q, URL state = %q", oauthState, q.Get("state"))
	}
	sum := sha256.Sum256([]byte(verifier))
	if got := base64.RawURLEncoding.EncodeToString(sum[:]); got != q.Get("code_challenge") {
		t.Fatalf("code_challenge = %q, want S256(%q) = %q", q.Get("code_challenge"), verifier, got)
	}

	st := browserStatus(t, ctrl, ls.LoginID)
	if st.Step != "browser" || st.URL != ls.URL || st.LoginID != ls.LoginID {
		t.Fatalf("status before completion = %+v", st)
	}
	snap := ctrl.Snapshot()
	if snap.Login == nil || snap.Login.Step != "browser" || snap.Login.URL != ls.URL {
		t.Fatalf("snapshot login = %+v", snap.Login)
	}
	if _, err := ctrl.LoginStatus("another-flow"); err == nil {
		t.Fatal("LoginStatus accepted a different login id")
	}

	if err := ctrl.LoginCancel(ls.LoginID); err != nil {
		t.Fatal(err)
	}
	withEmail := startBrowserLogin(t, ctrl, "add@example.com", "")
	defer ctrl.LoginCancel(withEmail.LoginID)
	eq := browserAuthorizationURL(t, withEmail.URL).Query()
	if eq.Get("email") != "add@example.com" {
		t.Errorf("add-account email = %q, want add@example.com", eq.Get("email"))
	}
	if eq.Has("action") {
		t.Error("add-account URL must not force auth")
	}
}

func TestBrowserLoginReauthStartUsesStoredEmail(t *testing.T) {
	ctrl, _ := newBrowserTestController(t)
	now := time.Now()
	f := &accountsFile{
		Version: 2,
		Active:  "browser-id",
		Accounts: []*Account{{
			ID:         "browser-id",
			Email:      "stored@example.com",
			Label:      "Stored",
			Method:     MethodBrowser,
			FxA:        &fxa.Tokens{AccessToken: "old-access", ExpiresAt: now.Add(time.Hour).Unix()},
			AddedAt:    now.Add(-time.Hour),
			LastUsedAt: now.Add(-time.Hour),
		}},
	}
	if err := saveAccounts(accountsPath(ctrl.store.Paths()), f); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	ls := startBrowserLogin(t, ctrl, "other@example.com", "browser-id")
	defer ctrl.LoginCancel(ls.LoginID)
	q := browserAuthorizationURL(t, ls.URL).Query()
	if q.Get("email") != "stored@example.com" {
		t.Errorf("re-auth email = %q, want the stored account email", q.Get("email"))
	}
	if q.Get("action") != "force_auth" {
		t.Errorf("re-auth action = %q, want force_auth", q.Get("action"))
	}
}

func TestBrowserLoginCompleteSavesFxA(t *testing.T) {
	ctrl, srv := newBrowserTestController(t)
	ls := startBrowserLogin(t, ctrl, "", "")
	state := browserAuthorizationState(t, ls.URL)
	redirect := browserRedirectURL(state, testBrowserCode)

	// An empty login id addresses the active flow.
	if err := ctrl.LoginComplete("", redirect); err != nil {
		t.Fatalf("LoginComplete: %v", err)
	}
	st := browserStatus(t, ctrl, ls.LoginID)
	if st.Step != "done" || st.Method != MethodBrowser || st.Email != testBrowserEmail ||
		st.LoginID != ls.LoginID || st.Error != "" {
		t.Fatalf("completion state = %+v", st)
	}

	ctrl.mu.Lock()
	b := ctrl.browser
	if b == nil || b.active {
		ctrl.mu.Unlock()
		t.Fatalf("browser flow after completion = %+v", b)
	}
	if b.verifier != "" || b.oauthState != "" || b.tokens != nil {
		ctrl.mu.Unlock()
		t.Fatalf("completed flow kept sign-in material: %+v", b)
	}
	account := ctrl.activeAccountLocked()
	ctrl.mu.Unlock()
	if account == nil {
		t.Fatal("no active account after browser sign-in")
	}
	if account.Method != MethodBrowser || account.Email != testBrowserEmail {
		t.Fatalf("account = %+v", account)
	}
	if account.Browser != nil {
		t.Fatalf("browser sign-in wrote a legacy BrowserToken: %+v", account.Browser)
	}
	if account.ReauthRequired {
		t.Fatal("browser sign-in left the durable re-auth flag set")
	}
	if account.FxA == nil || account.FxA.AccessToken != testBrowserAccess ||
		account.FxA.RefreshToken != testBrowserRefresh {
		t.Fatalf("account FxA tokens = %+v", account.FxA)
	}
	if account.Label != testBrowserEmail {
		t.Fatalf("account label = %q, want %q", account.Label, testBrowserEmail)
	}

	snap := ctrl.Snapshot()
	if snap.State != string(StateIdle) || !snap.Authenticated || snap.Email != testBrowserEmail {
		t.Fatalf("snapshot after browser sign-in = %+v", snap)
	}
	if snap.ActiveAccount != account.ID {
		t.Fatalf("active account = %q, want %q", snap.ActiveAccount, account.ID)
	}
	if view := ctrl.Accounts(); len(view.Accounts) != 1 || !view.Accounts[0].Active {
		t.Fatalf("accounts view = %+v", view)
	}

	loaded, _, err := loadAccounts(accountsPath(ctrl.store.Paths()))
	if err != nil {
		t.Fatal(err)
	}
	persisted := findAccountLocked(loaded, account.ID)
	if persisted == nil || persisted.FxA == nil || persisted.FxA.AccessToken != testBrowserAccess {
		t.Fatalf("persisted account = %+v", persisted)
	}
	if persisted.Browser != nil || persisted.ReauthRequired {
		t.Fatalf("persisted account kept legacy browser state: %+v", persisted)
	}
	tokens, err := loadTokens(ctrl.store.Paths().Tokens)
	if err != nil {
		t.Fatal(err)
	}
	if tokens == nil || tokens.AccessToken != testBrowserAccess || tokens.RefreshToken != testBrowserRefresh {
		t.Fatalf("tokens.json rollback copy = %+v", tokens)
	}

	tokenCalls, tokenRequest := srv.tokenSnapshot()
	if tokenCalls != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", tokenCalls)
	}
	if tokenRequest["grant_type"] != "authorization_code" ||
		tokenRequest["code"] != testBrowserCode ||
		tokenRequest["redirect_uri"] != fxa.OAuthRedirectURI ||
		tokenRequest["client_id"] != fxa.ClientID {
		t.Fatalf("token request = %+v", tokenRequest)
	}
	if tokenRequest["code_verifier"] == "" {
		t.Fatal("token request has no PKCE verifier")
	}
	sum := sha256.Sum256([]byte(tokenRequest["code_verifier"]))
	challenge := browserAuthorizationURL(t, ls.URL).Query().Get("code_challenge")
	if got := base64.RawURLEncoding.EncodeToString(sum[:]); got != challenge {
		t.Fatalf("token verifier does not match the authorization challenge")
	}
	profileCalls, profileAuth := srv.profileSnapshot()
	if profileCalls != 1 || profileAuth != "Bearer "+testBrowserAccess {
		t.Fatalf("profile calls = %d, auth = %q", profileCalls, profileAuth)
	}
	requireNoSecretsInLogs(t, ctrl, testBrowserCode, tokenRequest["code_verifier"], state, redirect, testBrowserAccess, testBrowserRefresh)
}

func TestBrowserLoginBadRedirectKeepsFlowActive(t *testing.T) {
	ctrl, srv := newBrowserTestController(t)
	ls := startBrowserLogin(t, ctrl, "", "")
	state := browserAuthorizationState(t, ls.URL)
	if err := ctrl.LoginComplete("another-flow", browserRedirectURL(state, testBrowserCode)); err == nil {
		t.Fatal("LoginComplete accepted a different login id")
	}
	if !browserFlowActive(ctrl) {
		t.Fatal("a mismatched login id stopped the flow")
	}

	cases := []struct {
		name   string
		url    string
		secret string
		want   string
	}{
		{
			name:   "wrong state",
			url:    browserRedirectURL("not-"+state, testBrowserCode),
			secret: "not-" + state,
			want:   "does not belong",
		},
		{
			name:   "malformed URL",
			url:    "https://example.com/redirect?code=" + testBrowserCode + "&state=" + url.QueryEscape(state),
			secret: "example.com",
			want:   "paste the complete redirect URL",
		},
		{
			name:   "wrong URN",
			url:    "urn:ietf:wg:oauth:2.0:oob:wrong?code=" + testBrowserCode + "&state=" + url.QueryEscape(state),
			secret: "wrong?",
			want:   "paste the complete redirect URL",
		},
		{
			name:   "missing code",
			url:    fxa.OAuthRedirectURI + "?state=" + url.QueryEscape(state),
			secret: "",
			want:   "authorization code",
		},
		{
			name:   "error redirect",
			url:    fxa.OAuthRedirectURI + "?error=access_denied&state=" + url.QueryEscape(state),
			secret: "access_denied",
			want:   "not completed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ctrl.LoginComplete(ls.LoginID, tc.url)
			if err == nil {
				t.Fatalf("LoginComplete(%s) succeeded", tc.name)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
				t.Fatalf("error = %v, want mention of %q", err, tc.want)
			}
			requireNoSecretsInError(t, err, testBrowserCode, state, tc.secret)
			st := browserStatus(t, ctrl, ls.LoginID)
			if st.Step != "browser" || !browserFlowActive(ctrl) {
				t.Fatalf("flow is not active after %s: %+v", tc.name, st)
			}
		})
	}

	if err := ctrl.LoginComplete(ls.LoginID, ""); err == nil ||
		!strings.Contains(err.Error(), "paste the complete redirect URL from the browser") {
		t.Fatalf("empty redirect error = %v", err)
	}
	if calls, _ := srv.tokenSnapshot(); calls != 0 {
		t.Fatalf("token endpoint called %d times for rejected redirects", calls)
	}

	// The same flow still accepts the correct paste.
	if err := ctrl.LoginComplete(ls.LoginID, browserRedirectURL(state, testBrowserCode)); err != nil {
		t.Fatalf("LoginComplete after retries: %v", err)
	}
	if st := browserStatus(t, ctrl, ls.LoginID); st.Step != "done" {
		t.Fatalf("state after a corrected paste = %+v", st)
	}
}

func TestBrowserLoginExchangeClassification(t *testing.T) {
	t.Run("invalid grant is terminal", func(t *testing.T) {
		ctrl, srv := newBrowserTestController(t)
		srv.setToken(http.StatusBadRequest, `{"errno":106,"message":"invalid authorization code"}`)
		ls := startBrowserLogin(t, ctrl, "", "")
		state := browserAuthorizationState(t, ls.URL)
		redirect := browserRedirectURL(state, testBrowserCode)

		if err := ctrl.LoginComplete(ls.LoginID, redirect); err != nil {
			t.Fatalf("terminal completion must be reported through LoginStatus: %v", err)
		}
		st := browserStatus(t, ctrl, ls.LoginID)
		if st.Step != "error" || !strings.Contains(st.Error, "rejected") {
			t.Fatalf("terminal state = %+v", st)
		}
		ctrl.mu.Lock()
		b := ctrl.browser
		active, tokens := b.active, b.tokens
		ctrl.mu.Unlock()
		if active || tokens != nil {
			t.Fatalf("terminal flow kept active=%v tokens=%+v", active, tokens)
		}
		if view := ctrl.Accounts(); len(view.Accounts) != 0 {
			t.Fatalf("terminal exchange added an account: %+v", view)
		}
		if err := ctrl.LoginComplete(ls.LoginID, redirect); err == nil {
			t.Fatal("a terminal flow accepted another completion")
		}
		requireNoSecretsInLogs(t, ctrl, testBrowserCode, state, redirect, testBrowserAccess)
	})

	for _, tc := range []struct {
		name string
		body string
	}{
		{"malformed success response", `{"access_token":`},
		{"success response without a token", `{"refresh_token":"refresh-1"}`},
		{"success response without a refresh token", `{"access_token":"access-1"}`},
	} {
		t.Run(tc.name+" is terminal", func(t *testing.T) {
			ctrl, srv := newBrowserTestController(t)
			srv.setToken(http.StatusOK, tc.body)
			ls := startBrowserLogin(t, ctrl, "", "")
			state := browserAuthorizationState(t, ls.URL)
			redirect := browserRedirectURL(state, testBrowserCode)

			if err := ctrl.LoginComplete(ls.LoginID, redirect); err != nil {
				t.Fatalf("unusable 2xx response must be terminal, not retryable: %v", err)
			}
			st := browserStatus(t, ctrl, ls.LoginID)
			if st.Step != "error" || !strings.Contains(st.Error, "unusable") {
				t.Fatalf("unusable response state = %+v", st)
			}
			ctrl.mu.Lock()
			b := ctrl.browser
			active, tokens := b.active, b.tokens
			ctrl.mu.Unlock()
			if active || tokens != nil {
				t.Fatalf("unusable response kept active=%v tokens=%+v", active, tokens)
			}
			if calls, _ := srv.tokenSnapshot(); calls != 1 {
				t.Fatalf("token calls = %d, want 1", calls)
			}
			if err := ctrl.LoginComplete(ls.LoginID, redirect); err == nil {
				t.Fatal("a terminal unusable response accepted another completion")
			}
			if calls, _ := srv.tokenSnapshot(); calls != 1 {
				t.Fatalf("terminal response re-exchanged the code: token calls = %d", calls)
			}
			if view := ctrl.Accounts(); len(view.Accounts) != 0 {
				t.Fatalf("unusable response added an account: %+v", view)
			}
			requireNoSecretsInLogs(t, ctrl, testBrowserCode, state, redirect, testBrowserAccess)
		})
	}

	t.Run("server error is retryable", func(t *testing.T) {
		ctrl, srv := newBrowserTestController(t)
		srv.setToken(http.StatusInternalServerError, `{"errno":500,"message":"boom"}`)
		ls := startBrowserLogin(t, ctrl, "", "")
		state := browserAuthorizationState(t, ls.URL)
		redirect := browserRedirectURL(state, testBrowserCode)

		err := ctrl.LoginComplete(ls.LoginID, redirect)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "exchange") {
			t.Fatalf("retryable exchange error = %v", err)
		}
		requireNoSecretsInError(t, err, testBrowserCode, state, redirect)
		if st := browserStatus(t, ctrl, ls.LoginID); st.Step != "browser" || !browserFlowActive(ctrl) {
			t.Fatalf("flow after a 500 = %+v", st)
		}
		if view := ctrl.Accounts(); len(view.Accounts) != 0 {
			t.Fatalf("failed exchange added an account: %+v", view)
		}

		srv.setToken(http.StatusOK, testBrowserTokenBody)
		if err := ctrl.LoginComplete(ls.LoginID, redirect); err != nil {
			t.Fatalf("retry after 500: %v", err)
		}
		if st := browserStatus(t, ctrl, ls.LoginID); st.Step != "done" {
			t.Fatalf("state after retry = %+v", st)
		}
	})

	t.Run("network error is retryable", func(t *testing.T) {
		ctrl, srv := newBrowserTestController(t)
		dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		deadURL := dead.URL
		dead.Close()
		ctrl.fxa.TokenEndpoint = deadURL + "/token"
		ls := startBrowserLogin(t, ctrl, "", "")
		state := browserAuthorizationState(t, ls.URL)
		redirect := browserRedirectURL(state, testBrowserCode)

		err := ctrl.LoginComplete(ls.LoginID, redirect)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "exchange") {
			t.Fatalf("retryable network error = %v", err)
		}
		if st := browserStatus(t, ctrl, ls.LoginID); st.Step != "browser" || !browserFlowActive(ctrl) {
			t.Fatalf("flow after a network error = %+v", st)
		}

		ctrl.fxa.TokenEndpoint = srv.URL + "/token"
		if err := ctrl.LoginComplete(ls.LoginID, redirect); err != nil {
			t.Fatalf("retry after network error: %v", err)
		}
		if st := browserStatus(t, ctrl, ls.LoginID); st.Step != "done" {
			t.Fatalf("state after network retry = %+v", st)
		}
	})
}

func browserFlowActive(ctrl *Controller) bool {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	return ctrl.browser != nil && ctrl.browser.active
}

func TestBrowserLoginCompleteSerializesDuplicateSubmissions(t *testing.T) {
	ctrl, _ := newBrowserTestController(t)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseAll)
	var tokenCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls.Add(1)
			entered <- struct{}{}
			<-release
			_, _ = io.WriteString(w, testBrowserTokenBody)
		case "/profile":
			_, _ = io.WriteString(w, testBrowserProfileBody)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	ctrl.fxa.TokenEndpoint = srv.URL + "/token"
	ctrl.fxa.ProfileEndpoint = srv.URL + "/profile"

	ls := startBrowserLogin(t, ctrl, "", "")
	state := browserAuthorizationState(t, ls.URL)
	redirect := browserRedirectURL(state, testBrowserCode)

	results := make(chan error, 2)
	go func() { results <- ctrl.LoginComplete(ls.LoginID, redirect) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("the first completion did not reach the token endpoint")
	}
	go func() { results <- ctrl.LoginComplete(ls.LoginID, redirect) }()
	select {
	case err := <-results:
		if err == nil || !strings.Contains(err.Error(), "already being completed") {
			t.Fatalf("duplicate completion error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the duplicate completion was not rejected")
	}
	releaseAll()
	if err := <-results; err != nil {
		t.Fatalf("first completion: %v", err)
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", got)
	}
	if st := browserStatus(t, ctrl, ls.LoginID); st.Step != "done" {
		t.Fatalf("state after duplicate submissions = %+v", st)
	}
}

func TestBrowserLoginProfileRetry(t *testing.T) {
	ctrl, srv := newBrowserTestController(t)
	srv.setProfile(http.StatusInternalServerError, `{"errno":500,"message":"boom"}`)
	ls := startBrowserLogin(t, ctrl, "", "")
	state := browserAuthorizationState(t, ls.URL)
	redirect := browserRedirectURL(state, testBrowserCode)

	err := ctrl.LoginComplete(ls.LoginID, redirect)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "profile") {
		t.Fatalf("retryable profile error = %v", err)
	}
	requireNoSecretsInError(t, err, testBrowserCode, state, redirect)
	if st := browserStatus(t, ctrl, ls.LoginID); st.Step != "browser" || !browserFlowActive(ctrl) {
		t.Fatalf("flow after a profile 500 = %+v", st)
	}
	ctrl.mu.Lock()
	b := ctrl.browser
	tokens := b.tokens
	profile := b.profile
	ctrl.mu.Unlock()
	if tokens == nil || profile != nil {
		t.Fatalf("flow after profile failure: tokens=%+v profile=%+v", tokens, profile)
	}
	if calls, _ := srv.tokenSnapshot(); calls != 1 {
		t.Fatalf("token calls = %d, want 1", calls)
	}
	if calls, _ := srv.profileSnapshot(); calls != 1 {
		t.Fatalf("profile calls = %d, want 1", calls)
	}

	srv.setProfile(http.StatusOK, testBrowserProfileBody)
	if err := ctrl.LoginComplete(ls.LoginID, redirect); err != nil {
		t.Fatalf("profile retry: %v", err)
	}
	if st := browserStatus(t, ctrl, ls.LoginID); st.Step != "done" {
		t.Fatalf("state after profile retry = %+v", st)
	}
	if calls, _ := srv.tokenSnapshot(); calls != 1 {
		t.Fatalf("profile retry re-exchanged the code: token calls = %d", calls)
	}
	if calls, _ := srv.profileSnapshot(); calls != 2 {
		t.Fatalf("profile calls = %d, want 2", calls)
	}
}

func TestBrowserLoginProfileNonUnauthorizedIsRetryable(t *testing.T) {
	ctrl, srv := newBrowserTestController(t)
	srv.setProfile(http.StatusForbidden, `{"errno":403,"message":"Forbidden"}`)
	ls := startBrowserLogin(t, ctrl, "", "")
	state := browserAuthorizationState(t, ls.URL)
	redirect := browserRedirectURL(state, testBrowserCode)

	err := ctrl.LoginComplete(ls.LoginID, redirect)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "profile") {
		t.Fatalf("profile 403 should be retryable, got %v", err)
	}
	if st := browserStatus(t, ctrl, ls.LoginID); st.Step != "browser" || !browserFlowActive(ctrl) {
		t.Fatalf("flow after a profile 403 = %+v", st)
	}
	ctrl.mu.Lock()
	tokens := ctrl.browser.tokens
	ctrl.mu.Unlock()
	if tokens == nil {
		t.Fatal("profile 403 discarded the exchanged tokens")
	}
	if calls, _ := srv.tokenSnapshot(); calls != 1 {
		t.Fatalf("token calls = %d, want 1", calls)
	}

	srv.setProfile(http.StatusOK, testBrowserProfileBody)
	if err := ctrl.LoginComplete(ls.LoginID, redirect); err != nil {
		t.Fatalf("profile retry after 403: %v", err)
	}
	if st := browserStatus(t, ctrl, ls.LoginID); st.Step != "done" {
		t.Fatalf("state after profile 403 retry = %+v", st)
	}
	if calls, _ := srv.tokenSnapshot(); calls != 1 {
		t.Fatalf("profile 403 retry re-exchanged the code: token calls = %d", calls)
	}
	if calls, _ := srv.profileSnapshot(); calls != 2 {
		t.Fatalf("profile calls = %d, want 2", calls)
	}
}

func TestBrowserLoginProfileUnauthorizedIsTerminal(t *testing.T) {
	ctrl, srv := newBrowserTestController(t)
	srv.setProfile(http.StatusUnauthorized, `{"errno":401,"message":"Unauthorized"}`)
	ls := startBrowserLogin(t, ctrl, "", "")
	state := browserAuthorizationState(t, ls.URL)
	redirect := browserRedirectURL(state, testBrowserCode)

	if err := ctrl.LoginComplete(ls.LoginID, redirect); err != nil {
		t.Fatalf("terminal profile failure must be reported through LoginStatus: %v", err)
	}
	st := browserStatus(t, ctrl, ls.LoginID)
	if st.Step != "error" || !strings.Contains(st.Error, "rejected") {
		t.Fatalf("terminal profile state = %+v", st)
	}
	ctrl.mu.Lock()
	b := ctrl.browser
	active, tokens := b.active, b.tokens
	ctrl.mu.Unlock()
	if active || tokens != nil {
		t.Fatalf("profile 401 kept active=%v tokens=%+v", active, tokens)
	}
	if view := ctrl.Accounts(); len(view.Accounts) != 0 {
		t.Fatalf("profile 401 added an account: %+v", view)
	}
	requireNoSecretsInLogs(t, ctrl, testBrowserCode, state, redirect, testBrowserAccess)
}

func seedBrowserAccount(t *testing.T, ctrl *Controller, id, email string, reauth bool) *fxa.Tokens {
	t.Helper()
	now := time.Now()
	old := &fxa.Tokens{
		AccessToken:  "old-access-" + id,
		RefreshToken: "old-refresh-" + id,
		ExpiresAt:    now.Add(time.Hour).Unix(),
	}
	f := &accountsFile{
		Version: 2,
		Active:  id,
		Accounts: []*Account{{
			ID:             id,
			Email:          email,
			Label:          "Stored label",
			Method:         MethodBrowser,
			FxA:            old,
			ReauthRequired: reauth,
			AddedAt:        now.Add(-time.Hour),
			LastUsedAt:     now.Add(-time.Hour),
		}},
	}
	if err := saveAccounts(accountsPath(ctrl.store.Paths()), f); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	return old
}

func TestBrowserLoginReauthSucceeds(t *testing.T) {
	ctrl, srv := newBrowserTestController(t)
	seedBrowserAccount(t, ctrl, "browser-id", "browser@example.com", true)
	srv.setProfile(http.StatusOK, `{"uid":"uid-1","email":"browser@example.com"}`)
	ls := startBrowserLogin(t, ctrl, "", "browser-id")
	state := browserAuthorizationState(t, ls.URL)

	if err := ctrl.LoginComplete(ls.LoginID, browserRedirectURL(state, testBrowserCode)); err != nil {
		t.Fatalf("re-auth completion: %v", err)
	}
	if st := browserStatus(t, ctrl, ls.LoginID); st.Step != "done" || st.Email != "browser@example.com" {
		t.Fatalf("re-auth state = %+v", st)
	}
	a := requireBrowserAccount(t, ctrl)
	if a.ID != "browser-id" || a.Label != "Stored label" {
		t.Fatalf("re-auth changed account identity: %+v", a)
	}
	if a.ReauthRequired {
		t.Fatal("re-auth did not clear the durable flag")
	}
	if a.FxA == nil || a.FxA.AccessToken != testBrowserAccess || a.FxA.RefreshToken != testBrowserRefresh {
		t.Fatalf("re-auth tokens = %+v", a.FxA)
	}
	if a.Browser != nil {
		t.Fatalf("re-auth wrote a legacy BrowserToken: %+v", a.Browser)
	}
}

func TestBrowserLoginReauthMismatchLeavesAccount(t *testing.T) {
	ctrl, srv := newBrowserTestController(t)
	old := seedBrowserAccount(t, ctrl, "browser-id", "browser@example.com", true)
	path := accountsPath(ctrl.store.Paths())
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	srv.setProfile(http.StatusOK, `{"uid":"uid-2","email":"other@example.com"}`)
	ls := startBrowserLogin(t, ctrl, "", "browser-id")
	state := browserAuthorizationState(t, ls.URL)

	if err := ctrl.LoginComplete(ls.LoginID, browserRedirectURL(state, testBrowserCode)); err != nil {
		t.Fatalf("mismatch completion returned an error instead of a terminal state: %v", err)
	}
	st := browserStatus(t, ctrl, ls.LoginID)
	if st.Step != "error" || !strings.Contains(strings.ToLower(st.Error), "different account") {
		t.Fatalf("mismatch state = %+v", st)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("re-auth mismatch rewrote accounts.json")
	}
	a := findAccountLocked(ctrl.accounts, "browser-id")
	if a == nil || a.FxA == nil || a.FxA.AccessToken != old.AccessToken || !a.ReauthRequired {
		t.Fatalf("re-auth mismatch changed the stored account: %+v", a)
	}
	if view := ctrl.Accounts(); len(view.Accounts) != 1 {
		t.Fatalf("re-auth mismatch added an account: %+v", view)
	}
}

func TestBrowserLoginAccountLimit(t *testing.T) {
	ctrl, srv := newBrowserTestController(t)
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
	path := accountsPath(ctrl.store.Paths())
	if err := saveAccounts(path, f); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	srv.setProfile(http.StatusOK, `{"uid":"uid-6","email":"sixth@example.com"}`)

	ls := startBrowserLogin(t, ctrl, "", "")
	state := browserAuthorizationState(t, ls.URL)
	if err := ctrl.LoginComplete(ls.LoginID, browserRedirectURL(state, testBrowserCode)); err != nil {
		t.Fatalf("limit completion: %v", err)
	}
	st := browserStatus(t, ctrl, ls.LoginID)
	if st.Step != "done" || !strings.Contains(st.Error, "account limit reached") {
		t.Fatalf("limit state = %+v", st)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("rejected sixth browser sign-in rewrote accounts.json")
	}
	if view := ctrl.Accounts(); len(view.Accounts) != MaxAccounts || view.Active != f.Active {
		t.Fatalf("accounts after limit = %+v", view)
	}
}

func TestBrowserLoginTunnelGuard(t *testing.T) {
	ctrl, _ := newBrowserTestController(t)
	ls := startBrowserLogin(t, ctrl, "", "")
	state := browserAuthorizationState(t, ls.URL)

	// The tunnel comes up while the browser flow is waiting.
	ctrl.mu.Lock()
	ctrl.setStateLocked(StateConnected)
	ctrl.mu.Unlock()

	if err := ctrl.LoginComplete(ls.LoginID, browserRedirectURL(state, testBrowserCode)); err != nil {
		t.Fatalf("tunnel guard completion: %v", err)
	}
	st := browserStatus(t, ctrl, ls.LoginID)
	if st.Step != "error" || !strings.Contains(st.Error, "disconnect first") {
		t.Fatalf("tunnel guard state = %+v", st)
	}
	if view := ctrl.Accounts(); len(view.Accounts) != 0 {
		t.Fatalf("tunnel guard added an account: %+v", view)
	}
}

func TestBrowserLoginAccountSwitchFence(t *testing.T) {
	ctrl, srv := newBrowserTestController(t)
	seedBrowserAccount(t, ctrl, "browser-id", "browser@example.com", false)
	// Add a second account to switch to.
	ctrl.mu.Lock()
	now := time.Now()
	second := &Account{
		ID:         "second-id",
		Email:      "second@example.com",
		Method:     MethodPassword,
		FxA:        &fxa.Tokens{AccessToken: "second-access", ExpiresAt: now.Add(time.Hour).Unix()},
		AddedAt:    now,
		LastUsedAt: now,
	}
	ctrl.accounts.Accounts = append(ctrl.accounts.Accounts, second)
	if err := ctrl.saveAccountsLocked(true); err != nil {
		ctrl.mu.Unlock()
		t.Fatal(err)
	}
	ctrl.mu.Unlock()
	srv.setProfile(http.StatusOK, `{"uid":"uid-1","email":"browser@example.com"}`)

	ls := startBrowserLogin(t, ctrl, "", "browser-id")
	state := browserAuthorizationState(t, ls.URL)
	if err := ctrl.UseAccount("second-id"); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.LoginComplete(ls.LoginID, browserRedirectURL(state, testBrowserCode)); err != nil {
		t.Fatalf("account switch completion: %v", err)
	}
	st := browserStatus(t, ctrl, ls.LoginID)
	if st.Step != "error" || !strings.Contains(strings.ToLower(st.Error), "account changed") {
		t.Fatalf("account switch state = %+v", st)
	}
	a := findAccountLocked(ctrl.accounts, "browser-id")
	if a == nil || a.FxA == nil || a.FxA.AccessToken != "old-access-browser-id" {
		t.Fatalf("account switch overwrote the old account: %+v", a)
	}
	if snap := ctrl.Snapshot(); snap.ActiveAccount != "second-id" {
		t.Fatalf("active account after fence = %q, want second-id", snap.ActiveAccount)
	}
}

func TestBrowserLoginCancel(t *testing.T) {
	ctrl, _ := newBrowserTestController(t)
	ls := startBrowserLogin(t, ctrl, "", "")
	state := browserAuthorizationState(t, ls.URL)

	if err := ctrl.LoginCancel(ls.LoginID); err != nil {
		t.Fatal(err)
	}
	st := browserStatus(t, ctrl, ls.LoginID)
	if st.Step != "cancelled" || st.Method != MethodBrowser || st.LoginID != ls.LoginID {
		t.Fatalf("cancelled state = %+v", st)
	}
	ctrl.mu.Lock()
	b := ctrl.browser
	active, verifier, tokens := b.active, b.verifier, b.tokens
	ctrl.mu.Unlock()
	if active || verifier != "" || tokens != nil {
		t.Fatalf("cancelled flow kept state: active=%v verifier=%q tokens=%+v", active, verifier, tokens)
	}
	if err := ctrl.LoginComplete(ls.LoginID, browserRedirectURL(state, testBrowserCode)); err == nil {
		t.Fatal("a cancelled flow accepted a completion")
	}
	if err := ctrl.LoginCancel(ls.LoginID); err != nil {
		t.Fatalf("repeating cancel: %v", err)
	}

	next := startBrowserLogin(t, ctrl, "", "")
	if next.LoginID == ls.LoginID {
		t.Fatal("a new flow reused the cancelled login id")
	}
	if err := ctrl.LoginCancel(next.LoginID); err != nil {
		t.Fatal(err)
	}
}

func TestBrowserLoginCancelWithoutID(t *testing.T) {
	ctrl, _ := newBrowserTestController(t)
	ls := startBrowserLogin(t, ctrl, "", "")

	if err := ctrl.LoginCancel(""); err != nil {
		t.Fatal(err)
	}
	st := browserStatus(t, ctrl, ls.LoginID)
	if st.Step != "cancelled" || st.LoginID != ls.LoginID {
		t.Fatalf("empty-id cancel state = %+v", st)
	}
	if browserFlowActive(ctrl) {
		t.Fatal("an empty-id cancel left the browser flow active")
	}
}

func TestBrowserLoginTimeout(t *testing.T) {
	ctrl, srv := newBrowserTestController(t)
	ls := startBrowserLogin(t, ctrl, "", "")
	state := browserAuthorizationState(t, ls.URL)

	// Nine minutes is still inside the deadline.
	ctrl.mu.Lock()
	ctrl.browser.created = time.Now().Add(-9 * time.Minute)
	ctrl.mu.Unlock()
	if st := browserStatus(t, ctrl, ls.LoginID); st.Step != "browser" {
		t.Fatalf("state before the deadline = %+v", st)
	}

	// Past ten minutes the next status marks the flow terminal.
	ctrl.mu.Lock()
	ctrl.browser.created = time.Now().Add(-11 * time.Minute)
	ctrl.mu.Unlock()
	st := browserStatus(t, ctrl, ls.LoginID)
	if st.Step != "error" || st.Error != "browser sign-in timed out; start again" {
		t.Fatalf("timeout state = %+v", st)
	}
	if browserFlowActive(ctrl) {
		t.Fatal("timed-out flow is still active")
	}
	if calls, _ := srv.tokenSnapshot(); calls != 0 {
		t.Fatalf("timed-out flow called the token endpoint %d times", calls)
	}
	if err := ctrl.LoginComplete(ls.LoginID, browserRedirectURL(state, testBrowserCode)); err == nil ||
		!strings.Contains(err.Error(), "timed out") {
		t.Fatalf("completion after timeout = %v", err)
	}
	if view := ctrl.Accounts(); len(view.Accounts) != 0 {
		t.Fatalf("timed-out flow added an account: %+v", view)
	}

	next := startBrowserLogin(t, ctrl, "", "")
	if st := browserStatus(t, ctrl, next.LoginID); st.Step != "browser" {
		t.Fatalf("new flow after timeout = %+v", st)
	}
	if err := ctrl.LoginCancel(next.LoginID); err != nil {
		t.Fatal(err)
	}

	// An expired but never-polled flow must not block a fresh start either.
	expired := startBrowserLogin(t, ctrl, "", "")
	ctrl.mu.Lock()
	ctrl.browser.created = time.Now().Add(-11 * time.Minute)
	ctrl.mu.Unlock()
	fresh := startBrowserLogin(t, ctrl, "", "")
	if fresh.LoginID == expired.LoginID {
		t.Fatal("start after an unpolled timeout reused the expired flow")
	}
	if err := ctrl.LoginCancel(fresh.LoginID); err != nil {
		t.Fatal(err)
	}
}

func TestBrowserLoginCompleteTimeoutIsTerminal(t *testing.T) {
	ctrl, srv := newBrowserTestController(t)
	ls := startBrowserLogin(t, ctrl, "", "")
	state := browserAuthorizationState(t, ls.URL)

	// Expire the flow without polling LoginStatus first, so LoginComplete
	// takes the active-flow timeout branch directly.
	ctrl.mu.Lock()
	ctrl.browser.created = time.Now().Add(-11 * time.Minute)
	ctrl.mu.Unlock()

	if err := ctrl.LoginComplete(ls.LoginID, browserRedirectURL(state, testBrowserCode)); err != nil {
		t.Fatalf("terminal timeout must not be returned as a retryable error: %v", err)
	}
	st := browserStatus(t, ctrl, ls.LoginID)
	if st.Step != "error" || st.Error != "browser sign-in timed out; start again" {
		t.Fatalf("timeout state = %+v", st)
	}
	if browserFlowActive(ctrl) {
		t.Fatal("timed-out flow is still active")
	}
	if calls, _ := srv.tokenSnapshot(); calls != 0 {
		t.Fatalf("timed-out flow called the token endpoint %d times", calls)
	}
	if view := ctrl.Accounts(); len(view.Accounts) != 0 {
		t.Fatalf("timed-out flow added an account: %+v", view)
	}
}

func TestBrowserLoginStartGuards(t *testing.T) {
	ctrl, _ := newBrowserTestController(t)

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

	first := startBrowserLogin(t, ctrl, "", "")
	defer ctrl.LoginCancel(first.LoginID)
	duplicate := ctrl.LoginStart(MethodBrowser, "", "")
	if duplicate.Step != "browser" || !strings.Contains(duplicate.Error, "already in progress") ||
		duplicate.LoginID != first.LoginID {
		t.Fatalf("duplicate browser start = %+v", duplicate)
	}
	password := ctrl.LoginStart(MethodPassword, "password@example.com", "")
	if password.Step != "error" || !strings.Contains(password.Error, "cancel the browser sign-in first") {
		t.Fatalf("password start while browser active = %+v", password)
	}
	if st := browserStatus(t, ctrl, first.LoginID); st.Step != "browser" {
		t.Fatalf("browser state after refused password start = %+v", st)
	}
}

func TestLoginStatusPasswordPending(t *testing.T) {
	ctrl, _ := newBrowserTestController(t)
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

func TestShutdownClearsBrowserFlow(t *testing.T) {
	ctrl, _ := newBrowserTestController(t)
	ls := startBrowserLogin(t, ctrl, "", "")
	ctrl.Shutdown()
	if _, err := ctrl.LoginStatus(ls.LoginID); err == nil {
		t.Fatal("shutdown left a browser flow addressable")
	}
}
