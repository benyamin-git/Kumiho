package fxa

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 1 << 20

// Client talks to the FxA auth server.
type Client struct {
	BaseURL   string
	HTTP      *http.Client
	Now       func() time.Time // injectable clock for tests
	Rand      io.Reader        // injectable nonce source for tests
	Solver    ChallengeSolver  // optional anti-bot solver (M3)
	UserAgent string           // platform UA; empty uses the package default
}

// NewClient returns a Client with production defaults.
func NewClient() *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
		Now:     time.Now,
		Rand:    rand.Reader,
	}
}

// Login performs POST /account/login. On errno 107 it retries without the
// verificationMethod hint, matching the reference client.
func (c *Client) Login(ctx context.Context, email, password string) (*LoginResult, error) {
	body := map[string]any{
		"email":              email,
		"authPW":             AuthPW(email, password),
		"verificationMethod": verificationMethodEmail2FA,
	}

	raw, err := c.doJSON(ctx, http.MethodPost, "/account/login", "", body)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Errno == errnoInvalidParameter {
			delete(body, "verificationMethod")
			raw, err = c.doJSON(ctx, http.MethodPost, "/account/login", "", body)
		}
		if err != nil {
			return nil, err
		}
	}

	var resp struct {
		SessionToken       string `json:"sessionToken"`
		Verified           bool   `json:"verified"`
		VerificationMethod string `json:"verificationMethod"`
		VerificationReason string `json:"verificationReason"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("login response: %w", err)
	}
	if resp.SessionToken == "" {
		return nil, errors.New("login response is missing sessionToken")
	}
	return &LoginResult{
		SessionToken:       resp.SessionToken,
		Verified:           resp.Verified,
		VerificationMethod: resp.VerificationMethod,
		VerificationReason: resp.VerificationReason,
	}, nil
}

// VerifyCode submits the emailed 2FA code (POST /session/verify_code).
func (c *Client) VerifyCode(ctx context.Context, sessionToken, code string) error {
	_, err := c.doJSON(ctx, http.MethodPost, "/session/verify_code", sessionToken, map[string]string{"code": code})
	return err
}

// SessionStatus returns the session state (GET /session/status), e.g.
// "verified" or "unverified". Used for the email-link verification flow.
func (c *Client) SessionStatus(ctx context.Context, sessionToken string) (string, error) {
	raw, err := c.doJSON(ctx, http.MethodGet, "/session/status", sessionToken, nil)
	if err != nil {
		return "", err
	}
	var resp struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("session status response: %w", err)
	}
	return resp.State, nil
}

// OAuthToken exchanges a verified session for OAuth tokens
// (POST /oauth/token, grant_type=fxa-credentials, Hawk-signed).
func (c *Client) OAuthToken(ctx context.Context, sessionToken string) (*Tokens, error) {
	body := map[string]string{
		"client_id":   ClientID,
		"grant_type":  "fxa-credentials",
		"scope":       Scope,
		"access_type": "offline",
	}
	raw, err := c.doJSON(ctx, http.MethodPost, "/oauth/token", sessionToken, body)
	if err != nil {
		return nil, err
	}
	return c.tokensFromResponse(raw, "")
}

// Refresh renews the access token (POST /oauth/token,
// grant_type=refresh_token, no Hawk).
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*Tokens, error) {
	body := map[string]string{
		"client_id":     ClientID,
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"scope":         Scope,
	}
	raw, err := c.doJSON(ctx, http.MethodPost, "/oauth/token", "", body)
	if err != nil {
		return nil, err
	}
	return c.tokensFromResponse(raw, refreshToken)
}

// AccessTokenValid reports whether the token set stays valid for at least
// 60 more seconds.
func (c *Client) AccessTokenValid(t *Tokens) bool {
	if t == nil || t.ExpiresAt <= 0 {
		return false
	}
	return t.ExpiresAt-c.now().Unix() > 60
}

func (c *Client) tokensFromResponse(raw []byte, fallbackRefresh string) (*Tokens, error) {
	var resp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("oauth response: %w", err)
	}
	if resp.AccessToken == "" {
		return nil, errors.New("oauth response is missing access_token")
	}
	ttl := resp.ExpiresIn
	if ttl <= 0 {
		ttl = defaultAccessTokenTTL
	}
	refresh := resp.RefreshToken
	if refresh == "" {
		refresh = fallbackRefresh
	}
	return &Tokens{
		AccessToken:  resp.AccessToken,
		RefreshToken: refresh,
		Scope:        resp.Scope,
		ExpiresAt:    c.now().Unix() + int64(ttl),
	}, nil
}

// doJSON sends a request, signs it with Hawk when a session token is given,
// and retries HTTP 406 through the challenge solver when one is configured.
func (c *Client) doJSON(ctx context.Context, method, path, sessionToken string, payload any) ([]byte, error) {
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}

	endpoint := strings.TrimRight(c.baseURL(), "/") + path
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}

	var creds *HawkCredentials
	if sessionToken != "" {
		hc, err := HawkCredentialsFromSessionToken(sessionToken)
		if err != nil {
			return nil, err
		}
		creds = &hc
	}

	buildRequest := func() (*http.Request, error) {
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
		if err != nil {
			return nil, err
		}
		ua := c.UserAgent
		if ua == "" {
			ua = UserAgent
		}
		req.Header.Set("User-Agent", ua)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if creds != nil {
			nonce := make([]byte, 6)
			if _, err := io.ReadFull(c.randReader(), nonce); err != nil {
				return nil, fmt.Errorf("hawk nonce: %w", err)
			}
			header, err := hawkHeader(method, u, *creds, body, c.now().Unix(), nonce)
			if err != nil {
				return nil, err
			}
			req.Header.Set("Authorization", header)
		}
		return req, nil
	}

	challengeTries := 0
	for {
		req, err := buildRequest()
		if err != nil {
			return nil, err
		}
		resp, err := c.httpClient().Do(req)
		if err != nil {
			return nil, err
		}
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}

		if resp.StatusCode == http.StatusNotAcceptable {
			challengeTries++
			if c.Solver == nil || challengeTries >= 5 {
				return nil, &ChallengeError{StatusCode: resp.StatusCode}
			}
			if err := c.Solver.Solve(ctx); err != nil {
				return nil, fmt.Errorf("anti-bot challenge: %w", err)
			}
			continue
		}
		if resp.StatusCode >= 400 {
			return nil, apiErrorFrom(resp.StatusCode, respBody)
		}
		return respBody, nil
	}
}

func apiErrorFrom(status int, body []byte) *APIError {
	e := &APIError{StatusCode: status, Message: strings.TrimSpace(string(body))}
	var parsed struct {
		Errno   int    `json:"errno"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &parsed) == nil {
		e.Errno = parsed.Errno
		if parsed.Message != "" {
			e.Message = parsed.Message
		}
	}
	if e.Message == "" {
		e.Message = http.StatusText(status)
	}
	return e
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return DefaultBaseURL
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) randReader() io.Reader {
	if c.Rand != nil {
		return c.Rand
	}
	return rand.Reader
}
