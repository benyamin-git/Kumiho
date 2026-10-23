package webauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

type verifyRequest struct {
	Method      string
	ContentType string
	UserAgent   string
	Code        string
	Verifier    string
}

func testJWT(exp int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp)))
	return header + "." + payload + "."
}

func validVerifyBody(exp int64) string {
	return fmt.Sprintf(`{"user":{"uid":"42","email":"user@example.com","displayName":"User"},"token":%q}`, testJWT(exp))
}

func newVerifyServer(t *testing.T, handle func(w http.ResponseWriter, r *http.Request, req verifyRequest)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/vpn/login/verify" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Code     string `json:"code"`
			Verifier string `json:"code_verifier"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		handle(w, r, verifyRequest{
			Method:      r.Method,
			ContentType: r.Header.Get("Content-Type"),
			UserAgent:   r.Header.Get("User-Agent"),
			Code:        body.Code,
			Verifier:    body.Verifier,
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func callback(t *testing.T, session *Session, rawQuery string) {
	t.Helper()
	u, err := url.Parse(session.URL)
	if err != nil {
		t.Fatalf("parse login URL: %v", err)
	}
	port := u.Query().Get("port")
	if port == "" {
		t.Fatal("login URL has no port")
	}
	resp, err := http.Get("http://127.0.0.1:" + port + "/?" + rawQuery)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	resp.Body.Close()
}

func TestStartBuildsPKCEURL(t *testing.T) {
	f := New(Config{BaseURL: "https://vpn.example", Platform: "linux", UserAgent: "UA"})
	s, err := f.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Cancel()
	u, _ := url.Parse(s.URL)
	if u.Path != "/api/v2/vpn/login/linux" {
		t.Fatalf("path = %s", u.Path)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" {
		t.Fatalf("method = %q", q.Get("code_challenge_method"))
	}
	if len(q.Get("code_challenge")) != 43 {
		t.Fatalf("challenge length = %d", len(q.Get("code_challenge")))
	}
	if q.Get("user_agent") != "UA" {
		t.Fatalf("user_agent = %q", q.Get("user_agent"))
	}
	if q.Get("utm_medium") != "vpn-client" || q.Get("utm_source") != "kumiho" {
		t.Fatalf("utm = %v", q)
	}
	if q.Get("port") == "" {
		t.Fatal("missing port")
	}
	if _, ok := q["state"]; ok {
		t.Fatal("state must not be sent (R1)")
	}
}

func TestVerifierIs128Chars(t *testing.T) {
	var (
		mu  sync.Mutex
		req verifyRequest
	)
	srv := newVerifyServer(t, func(w http.ResponseWriter, r *http.Request, got verifyRequest) {
		mu.Lock()
		req = got
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, validVerifyBody(1893456000))
	})
	f := New(Config{BaseURL: srv.URL, Platform: "linux", UserAgent: "UA"})
	s, err := f.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(s.URL)
	callback(t, s, "code=abc")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := req
	mu.Unlock()
	if len(got.Verifier) != 128 {
		t.Fatalf("verifier length = %d", len(got.Verifier))
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(got.Verifier) {
		t.Fatalf("verifier has invalid characters: %q", got.Verifier)
	}
	sum := sha256.Sum256([]byte(got.Verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if u.Query().Get("code_challenge") != want {
		t.Fatalf("challenge = %q, want S256(verifier) = %q", u.Query().Get("code_challenge"), want)
	}
}

func TestS256ChallengeVector(t *testing.T) {
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	const want = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := s256Challenge(verifier); got != want {
		t.Fatalf("challenge = %q, want %q", got, want)
	}
}

func TestWaitExchangesCode(t *testing.T) {
	var (
		mu  sync.Mutex
		req verifyRequest
	)
	srv := newVerifyServer(t, func(w http.ResponseWriter, r *http.Request, got verifyRequest) {
		mu.Lock()
		req = got
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, validVerifyBody(1893456000))
	})
	f := New(Config{BaseURL: srv.URL, Platform: "linux", UserAgent: "KumihoTest/1.0"})
	s, err := f.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	callback(t, s, "code=abc")
	result, err := s.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.UID != "42" || result.Email != "user@example.com" || result.DisplayName != "User" {
		t.Fatalf("result = %+v", result)
	}
	if result.Token != testJWT(1893456000) {
		t.Fatalf("token = %q", result.Token)
	}
	if result.ExpiresAt != 1893456000 {
		t.Fatalf("expires = %d", result.ExpiresAt)
	}
	mu.Lock()
	got := req
	mu.Unlock()
	if got.Method != http.MethodPost {
		t.Fatalf("method = %s", got.Method)
	}
	if !strings.HasPrefix(got.ContentType, "application/json") {
		t.Fatalf("content-type = %q", got.ContentType)
	}
	if got.UserAgent != "KumihoTest/1.0" {
		t.Fatalf("user-agent = %q", got.UserAgent)
	}
	if got.Code != "abc" {
		t.Fatalf("code = %q", got.Code)
	}
	if len(got.Verifier) != 128 {
		t.Fatalf("verifier length = %d", len(got.Verifier))
	}
}

func TestWaitRejectsErrorCallback(t *testing.T) {
	srv := newVerifyServer(t, func(w http.ResponseWriter, r *http.Request, req verifyRequest) {
		t.Fatal("verify must not be called for an error callback")
	})
	f := New(Config{BaseURL: srv.URL, Platform: "linux", UserAgent: "UA"})
	s, err := f.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	callback(t, s, "error=access_denied")
	_, err = s.Wait(context.Background())
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("error = %v", err)
	}
}

func TestWaitAcceptsTokenWithoutExpiry(t *testing.T) {
	srv := newVerifyServer(t, func(w http.ResponseWriter, r *http.Request, req verifyRequest) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"user":{"uid":42,"email":"user@example.com"},"token":"opaque-token"}`)
	})
	f := New(Config{BaseURL: srv.URL, Platform: "linux", UserAgent: "UA"})
	s, err := f.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	callback(t, s, "code=abc")
	result, err := s.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.UID != "42" || result.Email != "user@example.com" {
		t.Fatalf("result = %+v", result)
	}
	if result.ExpiresAt != 0 {
		t.Fatalf("expires = %d, want 0", result.ExpiresAt)
	}
}

func TestWaitRejectsMissingUserData(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "missing email", body: `{"user":{"uid":"42"},"token":"t"}`},
		{name: "missing token", body: `{"user":{"uid":"42","email":"user@example.com"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newVerifyServer(t, func(w http.ResponseWriter, r *http.Request, req verifyRequest) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, tc.body)
			})
			f := New(Config{BaseURL: srv.URL, Platform: "linux", UserAgent: "UA"})
			s, err := f.Start(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			callback(t, s, "code=abc")
			result, err := s.Wait(context.Background())
			if err == nil || !strings.Contains(err.Error(), "missing user data") {
				t.Fatalf("error = %v", err)
			}
			if result != nil {
				t.Fatalf("result = %+v, want nil", result)
			}
		})
	}
}

func TestWaitRejectsExchangeFailure(t *testing.T) {
	srv := newVerifyServer(t, func(w http.ResponseWriter, r *http.Request, req verifyRequest) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	f := New(Config{BaseURL: srv.URL, Platform: "linux", UserAgent: "UA"})
	s, err := f.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	callback(t, s, "code=SECRET_CODE")
	_, err = s.Wait(context.Background())
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "SECRET_CODE") {
		t.Fatalf("error leaks code: %v", err)
	}
}

func TestCancelStopsListener(t *testing.T) {
	f := New(Config{BaseURL: "https://vpn.example", Platform: "linux", UserAgent: "UA", Timeout: time.Second})
	s, err := f.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	s.Cancel()
	if _, err := s.Wait(context.Background()); err == nil {
		t.Fatal("Wait returned nil after Cancel")
	}
	if elapsed := time.Since(start); elapsed >= 900*time.Millisecond {
		t.Fatalf("Cancel did not unblock Wait promptly: %s", elapsed)
	}
}

func TestTimeout(t *testing.T) {
	f := New(Config{BaseURL: "https://vpn.example", Platform: "linux", UserAgent: "UA", Timeout: 20 * time.Millisecond})
	s, err := f.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Wait(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
}

func TestChallengeRetry(t *testing.T) {
	var (
		mu      sync.Mutex
		attempt int
		solves  []string
	)
	srv := newVerifyServer(t, func(w http.ResponseWriter, r *http.Request, req verifyRequest) {
		mu.Lock()
		attempt++
		n := attempt
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, validVerifyBody(1893456000))
	})
	f := New(Config{
		BaseURL:   srv.URL,
		Platform:  "linux",
		UserAgent: "UA",
		Solver: ChallengeSolverFunc(func(ctx context.Context, challengedURL string) error {
			mu.Lock()
			solves = append(solves, challengedURL)
			mu.Unlock()
			return nil
		}),
	})
	s, err := f.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	callback(t, s, "code=abc")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	gotAttempt, gotSolves := attempt, append([]string(nil), solves...)
	mu.Unlock()
	if gotAttempt != 2 {
		t.Fatalf("verify attempts = %d, want 2", gotAttempt)
	}
	if len(gotSolves) != 1 {
		t.Fatalf("solver calls = %d, want 1", len(gotSolves))
	}
	if gotSolves[0] != srv.URL+"/api/v2/vpn/login/verify" {
		t.Fatalf("solver URL = %q", gotSolves[0])
	}
}

type ChallengeSolverFunc func(ctx context.Context, url string) error

func (f ChallengeSolverFunc) Solve(ctx context.Context, url string) error {
	return f(ctx, url)
}
