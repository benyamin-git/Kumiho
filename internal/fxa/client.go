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
	BaseURL         string
	FallbackBaseURL string // tried after a BaseURL 406, before solving
	HTTP            *http.Client
	Now             func() time.Time // injectable clock for tests
	Rand            io.Reader        // injectable nonce source for tests
	Solver          ChallengeSolver  // optional anti-bot solver (internal/fastly)
	UserAgent       string           // platform UA; empty uses the package default

	// Browser OAuth endpoints; empty values fall back to the package defaults.
	AuthorizationEndpoint string
	TokenEndpoint         string
	ProfileEndpoint       string
	DestroyEndpoint       string
}

// NewClient returns a Client with production defaults.
func NewClient() *Client {
	return &Client{
		BaseURL:               DefaultBaseURL,
		FallbackBaseURL:       DefaultFallbackBaseURL,
		HTTP:                  &http.Client{Timeout: 30 * time.Second},
		Now:                   time.Now,
		Rand:                  rand.Reader,
		AuthorizationEndpoint: DefaultAuthorizationEndpoint,
		TokenEndpoint:         DefaultTokenEndpoint,
		ProfileEndpoint:       DefaultProfileEndpoint,
		DestroyEndpoint:       DefaultDestroyEndpoint,
	}
}

// Login performs POST /account/login with a password.
func (c *Client) Login(ctx context.Context, email, password string) (*LoginResult, error) {
	return c.LoginWithAuthPW(ctx, email, AuthPW(email, password))
}

// LoginWithAuthPW performs POST /account/login with a precomputed AuthPW.
// On errno 107 it retries without the verificationMethod hint, matching the
// reference client.
func (c *Client) LoginWithAuthPW(ctx context.Context, email, authPW string) (*LoginResult, error) {
	body := map[string]any{
		"email":              email,
		"authPW":             authPW,
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
	return parseLoginResult(raw)
}

// LoginUnblock retries a login that was blocked by the unblock-code
// verification reason. It deliberately omits verificationMethod.
func (c *Client) LoginUnblock(ctx context.Context, email, authPW, unblockCode string) (*LoginResult, error) {
	raw, err := c.doJSON(ctx, http.MethodPost, "/account/login", "", map[string]any{
		"email":       email,
		"authPW":      authPW,
		"unblockCode": unblockCode,
	})
	if err != nil {
		return nil, err
	}
	return parseLoginResult(raw)
}

// SendUnblockCode emails a new unblock code.
func (c *Client) SendUnblockCode(ctx context.Context, email string) error {
	_, err := c.doJSON(ctx, http.MethodPost, "/account/login/send_unblock_code", "", map[string]string{"email": email})
	return err
}

func parseLoginResult(raw []byte) (*LoginResult, error) {
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

// VerifyTOTP submits an authenticator code
// (POST /session/verify/totp), Hawk-signed like VerifyCode.
func (c *Client) VerifyTOTP(ctx context.Context, sessionToken, code string) error {
	_, err := c.doJSON(ctx, http.MethodPost, "/session/verify/totp", sessionToken, map[string]string{"code": code})
	return err
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
// grant_type=refresh_token, no Hawk). Browser accounts use this existing path;
// Gate G1 must prove the OAuth exchange's refresh token is accepted here before
// any endpoint unification is considered.
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
		return nil, &ResponseError{Err: fmt.Errorf("oauth response: %w", err)}
	}
	if resp.AccessToken == "" {
		return nil, &ResponseError{Err: errors.New("oauth response is missing access_token")}
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

// fallbackRetryDelay is the pause before retrying the sibling host once after
// a transport-level failure (a reset or EOF). The edge occasionally drops the
// first connection after challenging the primary; the login is safe to repeat.
const fallbackRetryDelay = 500 * time.Millisecond

// doJSON sends a request to a BaseURL-relative path, signs it with Hawk when a
// session token is given, and retries HTTP 406 through the challenge solver.
// The sibling FallbackBaseURL (when configured and distinct) is tried before
// any solve: Fastly can hard-block one host and path while the other hostname
// serves the same auth server, and a solve against a hard block cannot
// succeed. Only when every host answers 406 does each one get a solve.
func (c *Client) doJSON(ctx context.Context, method, path, sessionToken string, payload any) ([]byte, error) {
	bases := c.baseURLs()
	if len(bases) == 1 {
		return c.doRequest(ctx, method, strings.TrimRight(bases[0], "/")+path, sessionToken, "", payload, true)
	}

	var lastErr error

	// First pass: probe every base without solving. An HTTP answer that is
	// not a challenge is authoritative and ends the loop; a transport failure
	// does not, because the sibling host may still answer.
	for _, base := range bases {
		raw, err := c.doJSONBase(ctx, base, method, path, sessionToken, payload, false, lastErr != nil)
		if err == nil {
			return raw, nil
		}
		var chErr *ChallengeError
		if !errors.As(err, &chErr) && !isTransportError(err) {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, err
		}
		lastErr = err
	}
	if c.Solver == nil {
		return nil, lastErr
	}

	// Second pass: every host stayed challenged, so each gets one solve. A
	// transport failure on one host still lets the other host's solve run.
	for _, base := range bases {
		raw, err := c.doJSONBase(ctx, base, method, path, sessionToken, payload, true, lastErr != nil)
		if err == nil {
			return raw, nil
		}
		var chErr *ChallengeError
		if !errors.As(err, &chErr) && !isTransportError(err) {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

// doJSONBase issues one BaseURL-relative attempt. afterChallenge reports that
// an earlier base already answered 406; in that case a transport-level failure
// is retried once, because the edge can reset a connection right after
// challenging the primary.
func (c *Client) doJSONBase(ctx context.Context, base, method, path, sessionToken string, payload any, solve, afterChallenge bool) ([]byte, error) {
	endpoint := strings.TrimRight(base, "/") + path
	raw, err := c.doRequest(ctx, method, endpoint, sessionToken, "", payload, solve)
	if err == nil || !afterChallenge || ctx.Err() != nil || !isTransportError(err) {
		return raw, err
	}
	timer := time.NewTimer(fallbackRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return raw, err
	case <-timer.C:
	}
	return c.doRequest(ctx, method, endpoint, sessionToken, "", payload, solve)
}

// isTransportError reports whether err is a net/http transport failure (dial,
// TLS, connection reset or EOF) rather than a challenge or API response.
func isTransportError(err error) bool {
	var urlErr *url.Error
	return errors.As(err, &urlErr)
}

// doEndpointJSON sends a request to a full endpoint, optionally authenticated
// with a bearer token. Browser OAuth endpoints are absolute URLs and do not
// go through BaseURL.
func (c *Client) doEndpointJSON(ctx context.Context, method, endpoint, bearerToken string, payload any) ([]byte, error) {
	return c.doRequest(ctx, method, endpoint, "", bearerToken, payload, true)
}

// doRequest sends a request to endpoint, signs it with Hawk when a session
// token is given or with a bearer token when one is given, and retries HTTP
// 406 through the challenge solver when one is configured and solve is set.
func (c *Client) doRequest(ctx context.Context, method, endpoint, sessionToken, bearerToken string, payload any, solve bool) ([]byte, error) {
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}

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
		} else if bearerToken != "" {
			req.Header.Set("Authorization", "Bearer "+bearerToken)
		}
		return req, nil
	}

	challengeSolves := 0
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
			// One Solve already runs up to fastly's maxPostBackRounds
			// post-back rounds and verifies the page it solved, so a retry
			// that still 406s means this host cannot be cleared (a Fastly
			// hard block, for example). Stop early instead of five pointless
			// solves; doJSON decides which host, if any, still gets a chance.
			if !solve || c.Solver == nil || challengeSolves >= 1 {
				return nil, &ChallengeError{StatusCode: resp.StatusCode}
			}
			challengeSolves++
			// Mirror guardian's shape: the returned error is both a
			// *ChallengeError marker and errors.Is the solver's error.
			if err := c.Solver.Solve(ctx, req.URL.String()); err != nil {
				return nil, errors.Join(&ChallengeError{StatusCode: resp.StatusCode}, err)
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
		Errno              int    `json:"errno"`
		Message            string `json:"message"`
		VerificationMethod string `json:"verificationMethod"`
		VerificationReason string `json:"verificationReason"`
	}
	if json.Unmarshal(body, &parsed) == nil {
		e.Errno = parsed.Errno
		if parsed.Message != "" {
			e.Message = parsed.Message
		}
		e.VerificationMethod = parsed.VerificationMethod
		e.VerificationReason = parsed.VerificationReason
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

// baseURLs returns the ordered bases for a BaseURL-relative request: the
// primary first, then the fallback when it is configured and distinct.
func (c *Client) baseURLs() []string {
	primary := c.baseURL()
	fallback := strings.TrimRight(c.FallbackBaseURL, "/")
	if fallback == "" || fallback == strings.TrimRight(primary, "/") {
		return []string{primary}
	}
	return []string{primary, fallback}
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
