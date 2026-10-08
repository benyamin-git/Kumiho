package guardian

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func makeJWT(t *testing.T, claims map[string]any, padded bool) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString(payload)
	if padded {
		enc = base64.URLEncoding.EncodeToString(payload)
	}
	return "header." + enc + ".sig"
}

func TestFetchPassPrefersExpiresAtAndParsesQuota(t *testing.T) {
	var gotAuth, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/fpn/token" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("X-Quota-Limit", "1000000")
		w.Header().Set("X-Quota-Remaining", "750000")
		w.Header().Set("X-Quota-Reset", "1790000000")
		token := makeJWT(t, map[string]any{"exp": 1700000000}, false)
		_ = json.NewEncoder(w).Encode(map[string]any{"token": token, "expires_at": 1780000000})
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	pass, err := c.FetchPass(context.Background(), "access-token")
	if err != nil {
		t.Fatal(err)
	}
	if pass.Token == "" || pass.ExpiresAt != 1780000000 {
		t.Fatalf("pass = %+v", pass)
	}
	if gotAuth != "Bearer access-token" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotUA != UserAgent {
		t.Fatalf("User-Agent = %q", gotUA)
	}
	if pass.QuotaLimit == nil || *pass.QuotaLimit != 1000000 ||
		pass.QuotaRemaining == nil || *pass.QuotaRemaining != 750000 ||
		pass.QuotaReset == nil || *pass.QuotaReset != 1790000000 {
		t.Fatalf("quota headers = %+v", pass)
	}
}

func TestFetchPassFallsBackToJWTExpiry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := makeJWT(t, map[string]any{"exp": 1780000123}, true) // padded base64url
		_ = json.NewEncoder(w).Encode(map[string]any{"token": token})
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	pass, err := c.FetchPass(context.Background(), "t")
	if err != nil {
		t.Fatal(err)
	}
	if pass.ExpiresAt != 1780000123 {
		t.Fatalf("ExpiresAt = %d, want JWT exp", pass.ExpiresAt)
	}
	if pass.QuotaLimit != nil || pass.QuotaRemaining != nil || pass.QuotaReset != nil {
		t.Fatalf("missing quota headers must stay nil: %+v", pass)
	}
}

func TestFetchPassRejectsTokenlessBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"token":""}`))
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	if _, err := c.FetchPass(context.Background(), "t"); err == nil {
		t.Fatal("expected an error for a tokenless response")
	}
}

func TestAccountAndActivate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/fpn/status":
			// Live shape: uid is a number, maxBytes is a string.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"subscribed":        true,
				"uid":               12345,
				"maxBytes":          "53687091200",
				"limited_bandwidth": true,
			})
		case "/api/v1/fpn/token":
			w.Header().Set("X-Quota-Remaining", "4242")
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "t", "expires_at": 1780000000})
		case "/api/v1/fpn/activate":
			if r.Method != http.MethodPost {
				t.Errorf("activate method = %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"subscribed": true, "uid": "uid-1"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	ent, err := c.Account(context.Background(), "t")
	if err != nil {
		t.Fatal(err)
	}
	if !ent.Subscribed || ent.UID != "12345" || ent.MaxBytes == nil || *ent.MaxBytes != 53687091200 {
		t.Fatalf("entitlement = %+v", ent)
	}
	if ent.QuotaRemaining == nil || *ent.QuotaRemaining != 4242 {
		t.Fatalf("limited-bandwidth quota = %+v", ent.QuotaRemaining)
	}

	act, err := c.Activate(context.Background(), "t")
	if err != nil {
		t.Fatal(err)
	}
	if !act.Subscribed {
		t.Fatalf("activate = %+v", act)
	}
}

func TestRawUID(t *testing.T) {
	if got := rawUID(json.RawMessage(`12345`)); got != "12345" {
		t.Errorf("numeric uid = %q", got)
	}
	if got := rawUID(json.RawMessage(`"abc"`)); got != "abc" {
		t.Errorf("string uid = %q", got)
	}
	if got := rawUID(nil); got != "" {
		t.Errorf("missing uid = %q", got)
	}
}

func TestRawInt64(t *testing.T) {
	if got := rawInt64(json.RawMessage(`53687091200`)); got == nil || *got != 53687091200 {
		t.Errorf("numeric maxBytes = %v", got)
	}
	if got := rawInt64(json.RawMessage(`"53687091200"`)); got == nil || *got != 53687091200 {
		t.Errorf("string maxBytes = %v", got)
	}
	if got := rawInt64(json.RawMessage(`"junk"`)); got != nil {
		t.Errorf("junk maxBytes = %v", got)
	}
	if got := rawInt64(nil); got != nil {
		t.Errorf("missing maxBytes = %v", got)
	}
}

func TestErrorClassification(t *testing.T) {
	cases := []struct {
		status  int
		check   func(error) bool
		message string
	}{
		{http.StatusUnauthorized, IsUnauthorized, "401"},
		{http.StatusForbidden, IsUnauthorized, "403"},
		{http.StatusTooManyRequests, IsQuotaExceeded, "429"},
		{http.StatusNotAcceptable, IsChallenge, "406"},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
		}))
		c := NewClient()
		c.BaseURL = srv.URL
		_, err := c.FetchPass(context.Background(), "t")
		srv.Close()
		if err == nil || !tc.check(err) {
			t.Fatalf("status %s: err = %v", tc.message, err)
		}
		var he *HTTPError
		if !errors.As(err, &he) || he.Status != tc.status {
			t.Fatalf("status %s: %v", tc.message, err)
		}
	}
}

func TestChallengeSolverRetriesOnce(t *testing.T) {
	solves := 0
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "t", "expires_at": 1})
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	c.OnChallenge = func(context.Context) error {
		solves++
		return nil
	}
	if _, err := c.FetchPass(context.Background(), "t"); err != nil {
		t.Fatal(err)
	}
	if solves != 1 || calls != 2 {
		t.Fatalf("solves = %d, calls = %d", solves, calls)
	}
}

func TestJWTExpiryRejectsGarbage(t *testing.T) {
	for _, token := range []string{"", "abc", "a.b", "a.!!!.c", "a." + base64.RawURLEncoding.EncodeToString([]byte("not-json")) + ".c"} {
		if _, ok := JWTExpiry(token); ok {
			t.Fatalf("JWTExpiry(%q) = ok", token)
		}
	}
}
