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
	if userAgents[0] != UserAgent {
		t.Errorf("User-Agent = %q, want %q", userAgents[0], UserAgent)
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
