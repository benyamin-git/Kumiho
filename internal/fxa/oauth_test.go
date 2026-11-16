package fxa

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAuthorizationURL(t *testing.T) {
	c := NewClient()
	raw := c.AuthorizationURL("user@example.com", true, "test-state", "test-challenge")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	if u.Scheme != "https" || u.Host != "accounts.firefox.com" || u.Path != "/authorization" {
		t.Fatalf("authorization URL = %s", raw)
	}

	q := u.Query()
	want := map[string]string{
		"client_id":             OAuthClientID,
		"scope":                 Scope,
		"response_type":         "code",
		"redirect_uri":          OAuthRedirectURI,
		"state":                 "test-state",
		"code_challenge":        "test-challenge",
		"code_challenge_method": "S256",
		"access_type":           "offline",
		"email":                 "user@example.com",
		"action":                "force_auth",
	}
	for key, value := range want {
		if got := q.Get(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
	for key := range q {
		if _, ok := want[key]; !ok {
			t.Errorf("unexpected query parameter %q", key)
		}
	}
	if q.Has("client_secret") {
		t.Error("authorization URL must not include a client secret")
	}
	if q.Get("redirect_uri") != OAuthRedirectURI {
		t.Errorf("redirect_uri = %q, want %q", q.Get("redirect_uri"), OAuthRedirectURI)
	}

	// Email and force_auth are optional.
	plain, err := url.Parse(c.AuthorizationURL("", false, "state-2", "challenge-2"))
	if err != nil {
		t.Fatalf("parse optional authorization URL: %v", err)
	}
	pq := plain.Query()
	if pq.Has("email") {
		t.Errorf("email must be omitted, got %q", pq.Get("email"))
	}
	if pq.Has("action") {
		t.Errorf("action must be omitted, got %q", pq.Get("action"))
	}
}

func TestOAuthEndpointDefaults(t *testing.T) {
	c := NewClient()
	for name, got := range map[string]string{
		"AuthorizationEndpoint": c.AuthorizationEndpoint,
		"TokenEndpoint":         c.TokenEndpoint,
		"TokenFallbackEndpoint": c.TokenFallbackEndpoint,
		"ProfileEndpoint":       c.ProfileEndpoint,
		"DestroyEndpoint":       c.DestroyEndpoint,
	} {
		var want string
		switch name {
		case "AuthorizationEndpoint":
			want = DefaultAuthorizationEndpoint
		case "TokenEndpoint":
			want = DefaultTokenEndpoint
		case "TokenFallbackEndpoint":
			want = DefaultTokenFallbackEndpoint
		case "ProfileEndpoint":
			want = DefaultProfileEndpoint
		case "DestroyEndpoint":
			want = DefaultDestroyEndpoint
		}
		if got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestNewPKCE(t *testing.T) {
	pkce, err := NewPKCE(bytes.NewReader(bytes.Repeat([]byte{0x5a}, 48)))
	if err != nil {
		t.Fatal(err)
	}
	if len(pkce.Verifier) != 43 {
		t.Errorf("verifier length = %d, want 43", len(pkce.Verifier))
	}
	if len(pkce.Challenge) != 43 {
		t.Errorf("challenge length = %d, want 43", len(pkce.Challenge))
	}
	sum := sha256.Sum256([]byte(pkce.Verifier))
	if want := base64.RawURLEncoding.EncodeToString(sum[:]); pkce.Challenge != want {
		t.Errorf("challenge = %q, want S256(verifier) = %q", pkce.Challenge, want)
	}
	if pkce.State == "" {
		t.Error("state must not be empty")
	}

	other, err := NewPKCE(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if other.Verifier == pkce.Verifier || other.State == pkce.State {
		t.Error("PKCE values must be random per call")
	}
}

func TestNewPKCERejectsShortReads(t *testing.T) {
	if _, err := NewPKCE(bytes.NewReader(make([]byte, 31))); err == nil {
		t.Error("short verifier read must fail")
	}
	if _, err := NewPKCE(bytes.NewReader(make([]byte, 47))); err == nil {
		t.Error("short state read must fail")
	}
}

func TestExchangeCode(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotContentType string
	var gotBody map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		gotBody = make(map[string]string, len(r.PostForm))
		for key := range r.PostForm {
			gotBody[key] = r.PostForm.Get(key)
		}
		fmt.Fprintf(w, `{"access_token":"access-token","refresh_token":"refresh-token","scope":%q,"expires_in":3600}`, Scope)
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = "http://127.0.0.1:1/v1" // must not be used by the exchange
	c.TokenEndpoint = srv.URL + "/v1/token"
	c.Now = func() time.Time { return time.Unix(3000, 0) }

	tokens, err := c.ExchangeCode(context.Background(), "the-code", "the-verifier", OAuthRedirectURI)
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/token" {
		t.Errorf("request = %s %s, want POST /v1/token", gotMethod, gotPath)
	}
	if gotAuth != "" {
		t.Errorf("exchange must not be Hawk- or bearer-signed, got %q", gotAuth)
	}
	if !strings.HasPrefix(gotContentType, "application/x-www-form-urlencoded") {
		t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", gotContentType)
	}

	wantBody := map[string]string{
		"client_id":     OAuthClientID,
		"grant_type":    "authorization_code",
		"code":          "the-code",
		"code_verifier": "the-verifier",
		"redirect_uri":  OAuthRedirectURI,
	}
	if !reflect.DeepEqual(gotBody, wantBody) {
		t.Errorf("body = %#v, want %#v", gotBody, wantBody)
	}
	if tokens.AccessToken != "access-token" ||
		tokens.RefreshToken != "refresh-token" ||
		tokens.Scope != Scope ||
		tokens.ExpiresAt != 6600 ||
		tokens.ClientID != OAuthClientID {
		t.Errorf("tokens = %+v", tokens)
	}
}

func TestProfile(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotAccept string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		fmt.Fprint(w, `{"uid":"uid-1","email":"user@example.com","displayName":"Test User","avatar":"https://example.com/avatar.png"}`)
	}))
	defer srv.Close()

	c := NewClient()
	c.ProfileEndpoint = srv.URL + "/v1/profile"

	profile, err := c.Profile(context.Background(), "access-token")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet || gotPath != "/v1/profile" {
		t.Errorf("request = %s %s, want GET /v1/profile", gotMethod, gotPath)
	}
	if gotAuth != "Bearer access-token" {
		t.Errorf("Authorization = %q, want bearer token", gotAuth)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
	want := &Profile{
		UID:         "uid-1",
		Email:       "user@example.com",
		DisplayName: "Test User",
		Avatar:      "https://example.com/avatar.png",
	}
	if !reflect.DeepEqual(profile, want) {
		t.Errorf("profile = %+v, want %+v", profile, want)
	}
}

func TestProfileRequiresIdentity(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"missing uid", `{"email":"user@example.com"}`, "uid"},
		{"missing email", `{"uid":"uid-1"}`, "email"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			c := NewClient()
			c.ProfileEndpoint = srv.URL
			_, err := c.Profile(context.Background(), "access-token")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want mention of %q", err, tc.wantErr)
			}
		})
	}
}

func TestDestroy(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	c := NewClient()
	c.DestroyEndpoint = srv.URL + "/v1/destroy"

	if err := c.Destroy(context.Background(), "refresh-token"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/destroy" {
		t.Errorf("request = %s %s, want POST /v1/destroy", gotMethod, gotPath)
	}
	if gotAuth != "" {
		t.Errorf("destroy must not be signed, got %q", gotAuth)
	}
	want := map[string]string{"refresh_token": "refresh-token"}
	if !reflect.DeepEqual(gotBody, want) {
		t.Errorf("body = %#v, want %#v", gotBody, want)
	}
}

func TestExchangeCodeUnusableResponse(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"malformed JSON", `{"access_token":`},
		{"missing access token", `{"refresh_token":"refresh-1"}`},
		{"missing refresh token", `{"access_token":"access-1"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()

			c := NewClient()
			c.TokenEndpoint = srv.URL
			_, err := c.ExchangeCode(context.Background(), "the-code", "the-verifier", OAuthRedirectURI)
			var responseErr *ResponseError
			if !errors.As(err, &responseErr) {
				t.Fatalf("error = %v, want *ResponseError", err)
			}
			var apiErr *APIError
			if errors.As(err, &apiErr) {
				t.Fatalf("error = %v, must not be an APIError", err)
			}
			if responseErr.Error() == "" {
				t.Fatal("ResponseError has an empty message")
			}
		})
	}
}

func TestOAuthErrorParsing(t *testing.T) {
	t.Run("exchange 400", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"errno":106,"message":"invalid authorization code"}`)
		}))
		defer srv.Close()

		c := NewClient()
		c.TokenEndpoint = srv.URL
		_, err := c.ExchangeCode(context.Background(), "bad-code", "bad-verifier", OAuthRedirectURI)
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error = %v, want *APIError", err)
		}
		if apiErr.StatusCode != http.StatusBadRequest ||
			apiErr.Errno != 106 ||
			apiErr.Message != "invalid authorization code" {
			t.Errorf("APIError = %+v", apiErr)
		}
	})

	t.Run("profile 401", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"errno":401,"message":"Unauthorized"}`)
		}))
		defer srv.Close()

		c := NewClient()
		c.ProfileEndpoint = srv.URL
		_, err := c.Profile(context.Background(), "stale-token")
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error = %v, want *APIError", err)
		}
		if apiErr.StatusCode != http.StatusUnauthorized || apiErr.Message != "Unauthorized" {
			t.Errorf("APIError = %+v", apiErr)
		}
	})
}

// Refresh and the authorization-code exchange share the OAuth server's
// standard form endpoint. The legacy auth-server /v1/oauth/token route is now
// hard-blocked by Fastly (empty 406, no challenge page); a real
// password-issued refresh token is accepted at api.accounts.firefox.com/v1/token.
func TestRefreshUsesStandardTokenEndpoint(t *testing.T) {
	var gotPath, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		fmt.Fprint(w, `{"access_token":"access-2","expires_in":3600}`)
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL + "/v1"
	c.FallbackBaseURL = ""
	c.TokenEndpoint = "https://oauth.invalid/v1/token"
	if _, err := c.Refresh(context.Background(), "the-refresh-token"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/token" {
		t.Fatalf("Refresh path = %q, want the OAuth server /v1/token", gotPath)
	}
	if !strings.HasPrefix(gotContentType, "application/x-www-form-urlencoded") {
		t.Fatalf("Content-Type = %q, want a form grant", gotContentType)
	}
	if DefaultTokenEndpoint != "https://api.accounts.firefox.com/v1/token" {
		t.Fatalf("DefaultTokenEndpoint = %q, want the answering api host", DefaultTokenEndpoint)
	}
}

// ExchangeSession replaces the legacy auth-server /oauth/token
// fxa-credentials exchange, which Fastly hard-blocks with an empty 406. It
// mints an authorization code from the verified session and redeems it at
// the OAuth server's form endpoint.
func TestExchangeSessionUsesAuthorizationCode(t *testing.T) {
	var gotAuth string
	var gotAuthorize map[string]any
	var gotForm url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/oauth/authorization":
			if r.Method != http.MethodPost {
				t.Errorf("authorization method = %s, want POST", r.Method)
			}
			gotAuth = r.Header.Get("Authorization")
			if err := json.NewDecoder(r.Body).Decode(&gotAuthorize); err != nil {
				t.Errorf("decode authorization body: %v", err)
				return
			}
			state, _ := gotAuthorize["state"].(string)
			fmt.Fprintf(w, `{"code":"the-code","state":%q,"redirect":"https://accounts.firefox.com/oauth/success/%s?code=the-code&state=%s"}`, state, OAuthClientID, state)
		case "/v1/token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
				return
			}
			gotForm = r.PostForm
			fmt.Fprintf(w, `{"access_token":"access-token","refresh_token":"refresh-token","scope":%q,"expires_in":3600}`, Scope)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL + "/v1"
	c.FallbackBaseURL = ""
	c.TokenEndpoint = srv.URL + "/v1/token"
	c.TokenFallbackEndpoint = ""
	c.Now = func() time.Time { return time.Unix(1000, 0) }

	tokens, err := c.ExchangeSession(context.Background(), testSessionTokenHex())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(gotAuth, `Hawk id="`+testHawkID+`", ts="1000", nonce="`) {
		t.Fatalf("Authorization = %q, want a Hawk session signature", gotAuth)
	}
	if gotAuthorize["client_id"] != OAuthClientID ||
		gotAuthorize["response_type"] != "code" ||
		gotAuthorize["redirect_uri"] != OAuthRedirectURI ||
		gotAuthorize["scope"] != Scope ||
		gotAuthorize["access_type"] != "offline" ||
		gotAuthorize["code_challenge_method"] != "S256" {
		t.Fatalf("authorization body = %#v", gotAuthorize)
	}
	state, _ := gotAuthorize["state"].(string)
	challenge, _ := gotAuthorize["code_challenge"].(string)
	if state == "" || challenge == "" {
		t.Fatalf("authorization body is missing state/challenge: %#v", gotAuthorize)
	}
	if gotForm.Get("grant_type") != "authorization_code" ||
		gotForm.Get("code") != "the-code" ||
		gotForm.Get("client_id") != OAuthClientID ||
		gotForm.Get("redirect_uri") != OAuthRedirectURI ||
		gotForm.Get("code_verifier") == "" {
		t.Fatalf("token form = %#v", gotForm)
	}
	sum := sha256.Sum256([]byte(gotForm.Get("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
		t.Fatal("token code_verifier does not match the authorization challenge")
	}
	if tokens.AccessToken != "access-token" || tokens.RefreshToken != "refresh-token" ||
		tokens.ClientID != OAuthClientID || tokens.ExpiresAt != 4600 {
		t.Fatalf("tokens = %+v", tokens)
	}
}

func TestExchangeSessionRejectsStateMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":"the-code","state":"not-the-state"}`)
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL + "/v1"
	c.FallbackBaseURL = ""
	if _, err := c.ExchangeSession(context.Background(), testSessionTokenHex()); err == nil ||
		!strings.Contains(err.Error(), "state") {
		t.Fatalf("error = %v, want a state mismatch", err)
	}
}

// The OAuth server's form endpoint can be hard-blocked on either host; the
// second endpoint is tried on a challenge, exactly like refresh.
func TestExchangeCodeFallsBackToSiblingHost(t *testing.T) {
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotAcceptable)
	}))
	defer blocked.Close()
	var answeringHits int
	answering := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answeringHits++
		fmt.Fprintf(w, `{"access_token":"access-token","refresh_token":"refresh-token","scope":%q,"expires_in":3600}`, Scope)
	}))
	defer answering.Close()

	c := NewClient()
	c.BaseURL = "http://127.0.0.1:1/v1"
	c.FallbackBaseURL = ""
	c.TokenEndpoint = blocked.URL + "/v1/token"
	c.TokenFallbackEndpoint = answering.URL + "/v1/token"

	tokens, err := c.ExchangeCode(context.Background(), "the-code", "the-verifier", OAuthRedirectURI)
	if err != nil {
		t.Fatal(err)
	}
	if answeringHits != 1 || tokens.AccessToken != "access-token" {
		t.Fatalf("answering hits = %d, tokens = %+v", answeringHits, tokens)
	}
}

func TestExchangeCodeFallsBackToSiblingHostOn451(t *testing.T) {
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnavailableForLegalReasons)
	}))
	defer blocked.Close()
	var answeringHits int
	answering := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answeringHits++
		fmt.Fprintf(w, `{"access_token":"access-token","refresh_token":"refresh-token","scope":%q,"expires_in":3600}`, Scope)
	}))
	defer answering.Close()

	c := NewClient()
	c.BaseURL = "http://127.0.0.1:1/v1"
	c.FallbackBaseURL = ""
	c.TokenEndpoint = blocked.URL + "/v1/token"
	c.TokenFallbackEndpoint = answering.URL + "/v1/token"

	tokens, err := c.ExchangeCode(context.Background(), "the-code", "the-verifier", OAuthRedirectURI)
	if err != nil {
		t.Fatal(err)
	}
	if answeringHits != 1 || tokens.AccessToken != "access-token" {
		t.Fatalf("answering hits = %d, tokens = %+v", answeringHits, tokens)
	}
}
