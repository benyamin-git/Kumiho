package fxa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoginRetriesWithoutVerificationMethod(t *testing.T) {
	var bodies []map[string]any
	var authHeaders, userAgents []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/account/login" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		userAgents = append(userAgents, r.Header.Get("User-Agent"))

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
			return
		}
		bodies = append(bodies, body)

		if _, ok := body["verificationMethod"]; ok {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"errno":107,"message":"invalid parameter in request body"}`)
			return
		}
		fmt.Fprintf(w, `{"sessionToken":%q,"verified":false,"verificationMethod":"email-2fa"}`, testSessionTokenHex())
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL + "/v1"

	got, err := c.Login(context.Background(), "test@example.com", "password")
	if err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 {
		t.Fatalf("expected 2 login attempts, got %d", len(bodies))
	}
	if _, ok := bodies[0]["verificationMethod"]; !ok {
		t.Error("first attempt must send verificationMethod")
	}
	if _, ok := bodies[1]["verificationMethod"]; ok {
		t.Error("retry must omit verificationMethod")
	}
	if wantPW := "a624b128b0e54c3377fe16ca819c09d867827328bd8c740e620bffe3d1208475"; bodies[0]["authPW"] != wantPW {
		t.Errorf("authPW = %v, want %s", bodies[0]["authPW"], wantPW)
	}
	if got.SessionToken != testSessionTokenHex() || got.Verified || got.VerificationMethod != "email-2fa" {
		t.Errorf("login result = %+v", got)
	}
	for i, h := range authHeaders {
		if h != "" {
			t.Errorf("login attempt %d must not be Hawk-signed, got %q", i, h)
		}
	}
	if userAgents[0] != BrowserUserAgent {
		t.Errorf("User-Agent = %q, want %q", userAgents[0], BrowserUserAgent)
	}
}

func TestOAuthTokenHawkSignedAndParsed(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/oauth/token" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		fmt.Fprint(w, `{"access_token":"atok","refresh_token":"rtok","expires_in":3600,"scope":"profile https://identity.mozilla.com/apps/vpn"}`)
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL + "/v1"
	c.Now = func() time.Time { return time.Unix(1000, 0) }

	toks, err := c.OAuthToken(context.Background(), testSessionTokenHex())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(gotAuth, `Hawk id="`+testHawkID+`", ts="1000", nonce="`) {
		t.Errorf("Authorization = %q, want Hawk with derived id", gotAuth)
	}
	if !strings.Contains(gotBody, `"grant_type":"fxa-credentials"`) ||
		!strings.Contains(gotBody, `"access_type":"offline"`) ||
		!strings.Contains(gotBody, `"client_id":"5882386c6d801776"`) {
		t.Errorf("oauth body = %s", gotBody)
	}
	if toks.AccessToken != "atok" || toks.RefreshToken != "rtok" || toks.ExpiresAt != 4600 {
		t.Errorf("tokens = %+v", toks)
	}
	if !c.AccessTokenValid(toks) {
		t.Error("token should be valid")
	}
}

func TestRefreshSendsNoHawkAndKeepsOldRefreshToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"access_token":"atok2","expires_in":7200}`)
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL + "/v1"
	c.Now = func() time.Time { return time.Unix(2000, 0) }

	toks, err := c.Refresh(context.Background(), "old-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "" {
		t.Errorf("refresh grant must not be Hawk-signed, got %q", gotAuth)
	}
	if toks.RefreshToken != "old-refresh" {
		t.Errorf("refresh token = %q, want fallback to old token", toks.RefreshToken)
	}
	if toks.ExpiresAt != 9200 {
		t.Errorf("ExpiresAt = %d, want 9200", toks.ExpiresAt)
	}
}

func TestRefreshPermanentRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"errno":110,"message":"invalid refresh token"}`)
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL + "/v1"

	_, err := c.Refresh(context.Background(), "stale")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	if apiErr.StatusCode != 401 || apiErr.Errno != 110 {
		t.Errorf("APIError = %+v", apiErr)
	}
	if !apiErr.PermanentRefreshRejection() {
		t.Error("401 must be a permanent refresh rejection")
	}
}

func TestChallengeWithoutSolver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotAcceptable)
		fmt.Fprint(w, "Client Challenge")
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL + "/v1"
	c.FallbackBaseURL = "" // a fixture must never reach the production fallback

	_, err := c.Login(context.Background(), "test@example.com", "password")
	var chErr *ChallengeError
	if !errors.As(err, &chErr) {
		t.Fatalf("error = %v, want *ChallengeError", err)
	}
}

// fakeSolver records the challenged URL it was handed and optionally fails
// with err, so tests can pin the callback contract and the error shape.
type fakeSolver struct {
	calls   int
	lastURL string
	err     error
}

func (f *fakeSolver) Solve(_ context.Context, challengedURL string) error {
	f.calls++
	f.lastURL = challengedURL
	return f.err
}

func TestChallengeWithSolverRetries(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}
		fmt.Fprintf(w, `{"sessionToken":%q,"verified":true}`, testSessionTokenHex())
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL + "/v1"
	c.FallbackBaseURL = "" // a fixture must never reach the production fallback
	solver := &fakeSolver{}
	c.Solver = solver

	got, err := c.Login(context.Background(), "test@example.com", "password")
	if err != nil {
		t.Fatal(err)
	}
	if solver.calls != 1 || calls != 2 || !got.Verified {
		t.Fatalf("solver calls=%d, http calls=%d, result=%+v", solver.calls, calls, got)
	}
	if want := srv.URL + "/v1/account/login"; solver.lastURL != want {
		t.Fatalf("challenged URL = %q, want %q", solver.lastURL, want)
	}
}

// A solver failure keeps the *ChallengeError marker AND the solver error
// itself, so callers can errors.As the 406 and errors.Is the cause — the
// same joined shape guardian uses for OnChallenge failures.
func TestChallengeSolverFailureKeepsMarker(t *testing.T) {
	solverErr := errors.New("solver exploded")
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotAcceptable)
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL + "/v1"
	c.FallbackBaseURL = ""
	solver := &fakeSolver{err: solverErr}
	c.Solver = solver

	_, err := c.Login(context.Background(), "test@example.com", "password")
	var chErr *ChallengeError
	if !errors.As(err, &chErr) {
		t.Fatalf("error = %v, want *ChallengeError", err)
	}
	if chErr.StatusCode != http.StatusNotAcceptable {
		t.Fatalf("ChallengeError.StatusCode = %d, want 406", chErr.StatusCode)
	}
	if !errors.Is(err, solverErr) {
		t.Fatalf("error = %v, want errors.Is(err, solverErr)", err)
	}
	if calls != 1 || solver.calls != 1 {
		t.Fatalf("http calls = %d, solver calls = %d; want 1 and 1 (the failure ends the loop)",
			calls, solver.calls)
	}
}

// A solver-disabled error (fastly.ErrDisabled in production; a local
// sentinel here so package fxa need not import internal/fastly) reaches the
// caller unchanged through the join, so the engine can render the disabled
// wording.
func TestChallengeSolverDisabledReachesCaller(t *testing.T) {
	disabled := errors.New("challenge solving is disabled")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotAcceptable)
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL + "/v1"
	c.FallbackBaseURL = ""
	solver := &fakeSolver{err: disabled}
	c.Solver = solver

	_, err := c.Login(context.Background(), "test@example.com", "password")
	if !errors.Is(err, disabled) {
		t.Fatalf("error = %v, want errors.Is(err, disabled)", err)
	}
	var chErr *ChallengeError
	if !errors.As(err, &chErr) {
		t.Fatalf("error = %v, want *ChallengeError", err)
	}
	if solver.calls != 1 {
		t.Fatalf("solver calls = %d, want 1", solver.calls)
	}
}

// A host that stays challenged after a solve is never reached when the
// sibling answers: Fastly can hard-block one hostname's path while the other
// auth-server hostname keeps serving, and solving a hard block cannot
// succeed, so the sibling is tried before any solve.
func TestChallengeFallsBackBeforeSolving(t *testing.T) {
	var primaryHits, fallbackHits int
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryHits++
		w.WriteHeader(http.StatusNotAcceptable)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/account/login" {
			t.Errorf("fallback path = %s", r.URL.Path)
		}
		fallbackHits++
		fmt.Fprintf(w, `{"sessionToken":%q,"verified":true}`, testSessionTokenHex())
	}))
	defer fallback.Close()

	c := NewClient()
	c.BaseURL = primary.URL + "/v1"
	c.FallbackBaseURL = fallback.URL + "/v1"
	solver := &fakeSolver{}
	c.Solver = solver

	got, err := c.Login(context.Background(), "test@example.com", "password")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Verified || got.SessionToken != testSessionTokenHex() {
		t.Fatalf("login result = %+v", got)
	}
	if solver.calls != 0 {
		t.Fatalf("solver calls = %d, want 0 (the sibling answered first)", solver.calls)
	}
	if primaryHits != 1 {
		t.Fatalf("primary hits = %d, want 1", primaryHits)
	}
	if fallbackHits != 1 {
		t.Fatalf("fallback hits = %d, want 1", fallbackHits)
	}
}

// When every host is challenged, each gets one solve and the first success
// wins: the primary stays blocked, the fallback clears after its solve.
func TestChallengeSolvesEachHostWhenBothFail(t *testing.T) {
	var primaryHits, fallbackHits int
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryHits++
		w.WriteHeader(http.StatusNotAcceptable)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits++
		if fallbackHits <= 2 {
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}
		fmt.Fprintf(w, `{"sessionToken":%q,"verified":true}`, testSessionTokenHex())
	}))
	defer fallback.Close()

	c := NewClient()
	c.BaseURL = primary.URL + "/v1"
	c.FallbackBaseURL = fallback.URL + "/v1"
	solver := &fakeSolver{}
	c.Solver = solver

	got, err := c.Login(context.Background(), "test@example.com", "password")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Verified {
		t.Fatalf("login result = %+v", got)
	}
	if solver.calls != 2 {
		t.Fatalf("solver calls = %d, want 2 (one per challenged host)", solver.calls)
	}
	if want := fallback.URL + "/v1/account/login"; solver.lastURL != want {
		t.Fatalf("last challenged URL = %q, want %q", solver.lastURL, want)
	}
	if primaryHits != 3 || fallbackHits != 3 {
		t.Fatalf("primary hits = %d, fallback hits = %d; want 3 and 3 (probe, challenged retry, solved retry)",
			primaryHits, fallbackHits)
	}
}

// A transport reset on the sibling host is retried once: the edge can drop
// the first connection right after challenging the primary.
func TestFallbackTransportErrorRetried(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotAcceptable)
	}))
	defer primary.Close()
	var fallbackHits int
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits++
		fmt.Fprintf(w, `{"sessionToken":%q,"verified":true}`, testSessionTokenHex())
	}))
	defer fallback.Close()

	transport := &failOnceTransport{host: strings.TrimPrefix(fallback.URL, "http://"), base: http.DefaultTransport}
	c := NewClient()
	c.HTTP = &http.Client{Transport: transport, Timeout: 5 * time.Second}
	c.BaseURL = primary.URL + "/v1"
	c.FallbackBaseURL = fallback.URL + "/v1"

	if _, err := c.Login(context.Background(), "test@example.com", "password"); err != nil {
		t.Fatal(err)
	}
	if !transport.failed || fallbackHits != 1 {
		t.Fatalf("dropped=%v, fallback hits=%d; want one dropped connection and one success",
			transport.failed, fallbackHits)
	}
}

// failOnceTransport drops the first connection to host with an EOF.
type failOnceTransport struct {
	base   http.RoundTripper
	host   string
	failed bool
}

func (f *failOnceTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host == f.host && !f.failed {
		f.failed = true
		return nil, io.EOF
	}
	return f.base.RoundTrip(r)
}

// A transport failure on the primary host does not hide a healthy sibling:
// the fallback is tried even when the primary drops every connection.
func TestPrimaryTransportErrorFallsBackToSibling(t *testing.T) {
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"sessionToken":%q,"verified":true}`, testSessionTokenHex())
	}))
	defer fallback.Close()

	c := NewClient()
	c.HTTP = &http.Client{Timeout: 5 * time.Second}
	c.BaseURL = "http://127.0.0.1:1/v1"
	c.FallbackBaseURL = fallback.URL + "/v1"

	got, err := c.Login(context.Background(), "test@example.com", "password")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Verified {
		t.Fatalf("login result = %+v", got)
	}
}

// Without a solver the first 406 is terminal for the primary host, but the
// fallback still gets its chance.
func TestChallengeFallsBackWithoutSolver(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotAcceptable)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"sessionToken":%q,"verified":true}`, testSessionTokenHex())
	}))
	defer fallback.Close()

	c := NewClient()
	c.BaseURL = primary.URL + "/v1"
	c.FallbackBaseURL = fallback.URL + "/v1"

	if _, err := c.Login(context.Background(), "test@example.com", "password"); err != nil {
		t.Fatal(err)
	}
}

// When the fallback is challenged too, the caller still sees the
// *ChallengeError marker (the engine renders the anti-bot wording from it).
func TestFallbackChallengeKeepsMarker(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusNotAcceptable)
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL + "/v1a"
	c.FallbackBaseURL = srv.URL + "/v1b"

	_, err := c.Login(context.Background(), "test@example.com", "password")
	var chErr *ChallengeError
	if !errors.As(err, &chErr) {
		t.Fatalf("error = %v, want *ChallengeError", err)
	}
	if hits != 2 {
		t.Fatalf("hits = %d, want 2 (one per base, no retry loop)", hits)
	}
}

// Ordinary API errors are final: a 4xx that is not a challenge must not be
// replayed against the fallback.
func TestNonChallengeErrorDoesNotFallBack(t *testing.T) {
	var fallbackHits int
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"errno":102,"message":"Unknown account"}`)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits++
	}))
	defer fallback.Close()

	c := NewClient()
	c.BaseURL = primary.URL + "/v1"
	c.FallbackBaseURL = fallback.URL + "/v1"

	_, err := c.Login(context.Background(), "test@example.com", "password")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Errno != 102 {
		t.Fatalf("error = %v, want APIError errno 102", err)
	}
	if fallbackHits != 0 {
		t.Fatalf("fallback hits = %d, want 0", fallbackHits)
	}
}

// The fallback is a last resort: a healthy primary never contacts it.
func TestFallbackUnusedWhenPrimarySucceeds(t *testing.T) {
	var fallbackHits int
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"sessionToken":%q,"verified":true}`, testSessionTokenHex())
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits++
	}))
	defer fallback.Close()

	c := NewClient()
	c.BaseURL = primary.URL + "/v1"
	c.FallbackBaseURL = fallback.URL + "/v1"

	if _, err := c.Login(context.Background(), "test@example.com", "password"); err != nil {
		t.Fatal(err)
	}
	if fallbackHits != 0 {
		t.Fatalf("fallback hits = %d, want 0", fallbackHits)
	}
}

func TestLoginWithAuthPWPreservesAuthPW(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/account/login" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		fmt.Fprintf(w, `{"sessionToken":%q,"verified":true}`, testSessionTokenHex())
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL + "/v1"
	got, err := c.LoginWithAuthPW(context.Background(), "user@example.com", "deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	if body["authPW"] != "deadbeef" {
		t.Fatalf("authPW = %v, want deadbeef", body["authPW"])
	}
	if body["email"] != "user@example.com" {
		t.Fatalf("email = %v", body["email"])
	}
	if body["verificationMethod"] != verificationMethodEmail2FA {
		t.Fatalf("verificationMethod = %v", body["verificationMethod"])
	}
	if got.SessionToken != testSessionTokenHex() || !got.Verified {
		t.Fatalf("login result = %+v", got)
	}
}

func TestLoginUnblockSendsUnblockCode(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/account/login" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		fmt.Fprintf(w, `{"sessionToken":%q,"verified":true}`, testSessionTokenHex())
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL + "/v1"
	if _, err := c.LoginUnblock(context.Background(), "user@example.com", "deadbeef", "ABCD-1234"); err != nil {
		t.Fatal(err)
	}
	if body["email"] != "user@example.com" || body["authPW"] != "deadbeef" || body["unblockCode"] != "ABCD-1234" {
		t.Fatalf("body = %#v", body)
	}
	if _, ok := body["verificationMethod"]; ok {
		t.Fatalf("unblock login must omit verificationMethod: %#v", body)
	}
}

func TestSendUnblockCode(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/account/login/send_unblock_code" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL + "/v1"
	if err := c.SendUnblockCode(context.Background(), "user@example.com"); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || body["email"] != "user@example.com" {
		t.Fatalf("body = %#v", body)
	}
}

func TestVerifyTOTPUsesHawkSession(t *testing.T) {
	var authHeader, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/session/verify/totp" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		authHeader = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL + "/v1"
	if err := c.VerifyTOTP(context.Background(), testSessionTokenHex(), "123456"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(authHeader, `Hawk id="`+testHawkID+`", ts="`) {
		t.Fatalf("Authorization = %q", authHeader)
	}
	if !strings.Contains(body, `"code":"123456"`) {
		t.Fatalf("body = %s", body)
	}
}

func TestAPIErrorParsesVerificationFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"errno":125,"message":"locked","verificationMethod":"email-2fa","verificationReason":"unblock-code"}`)
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL + "/v1"
	_, err := c.LoginWithAuthPW(context.Background(), "user@example.com", "deadbeef")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest || apiErr.Errno != 125 || apiErr.Message != "locked" {
		t.Fatalf("APIError = %+v", apiErr)
	}
	if apiErr.VerificationMethod != "email-2fa" || apiErr.VerificationReason != "unblock-code" {
		t.Fatalf("verification fields = %q / %q", apiErr.VerificationMethod, apiErr.VerificationReason)
	}
}
