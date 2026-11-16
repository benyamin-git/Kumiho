package fxa

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	pkceVerifierBytes = 32
	pkceStateBytes    = 16
)

// PKCE carries the proof key, its S256 challenge and the OAuth state nonce
// for one browser authorization attempt.
type PKCE struct {
	Verifier  string
	Challenge string
	State     string
}

// Profile is the identity returned by the FxA profile endpoint.
type Profile struct {
	UID         string
	Email       string
	DisplayName string
	Avatar      string
}

// NewPKCE reads 32 random bytes for the verifier and 16 for the state from r
// (crypto/rand when r is nil). Short reads are an error.
func NewPKCE(r io.Reader) (PKCE, error) {
	if r == nil {
		r = rand.Reader
	}

	verifierRaw := make([]byte, pkceVerifierBytes)
	if _, err := io.ReadFull(r, verifierRaw); err != nil {
		return PKCE{}, fmt.Errorf("pkce verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierRaw)
	sum := sha256.Sum256([]byte(verifier))

	stateRaw := make([]byte, pkceStateBytes)
	if _, err := io.ReadFull(r, stateRaw); err != nil {
		return PKCE{}, fmt.Errorf("pkce state: %w", err)
	}
	return PKCE{
		Verifier:  verifier,
		Challenge: base64.RawURLEncoding.EncodeToString(sum[:]),
		State:     base64.RawURLEncoding.EncodeToString(stateRaw),
	}, nil
}

// AuthorizationURL builds the browser consent URL for the authorization-code
// flow. Email and force_auth are optional; no client secret is included.
//
// The URL uses Firefox for Android's public OAuth client and its HTTPS success
// redirect. Unlike Firefox Desktop's WebChannel URN, the HTTPS page is a normal
// navigation in any browser, and the complete redirect URL is copyable.
func (c *Client) AuthorizationURL(email string, forceAuth bool, state, challenge string) string {
	q := url.Values{
		"client_id":             {OAuthClientID},
		"scope":                 {Scope},
		"response_type":         {"code"},
		"redirect_uri":          {OAuthRedirectURI},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"access_type":           {"offline"},
	}
	if email != "" {
		q.Set("email", email)
	}
	if forceAuth {
		q.Set("action", "force_auth")
	}

	endpoint := c.authorizationEndpoint()
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint + "?" + q.Encode()
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// ExchangeSession exchanges a verified session token for OAuth tokens
// without the legacy auth-server /oauth/token route. POST /oauth/authorization
// mints an authorization code from the session (Hawk), then the standard PKCE
// code exchange at the OAuth server's form endpoint returns the tokens. Fastly
// hard-blocks /oauth/token with an empty 406 that no challenge can clear; the
// authorization-code endpoint is still reachable on the oauth sibling, and
// the form endpoint is tried on both hosts.
func (c *Client) ExchangeSession(ctx context.Context, sessionToken string) (*Tokens, error) {
	pkce, err := NewPKCE(c.Rand)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"response_type":         "code",
		"client_id":             OAuthClientID,
		"redirect_uri":          OAuthRedirectURI,
		"scope":                 Scope,
		"state":                 pkce.State,
		"access_type":           "offline",
		"code_challenge":        pkce.Challenge,
		"code_challenge_method": "S256",
	}
	raw, err := c.doJSON(ctx, http.MethodPost, "/oauth/authorization", sessionToken, body)
	if err != nil {
		return nil, err
	}
	var res struct {
		Code  string `json:"code"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("oauth authorization response: %w", err)
	}
	if res.Code == "" {
		return nil, errors.New("oauth authorization response is missing code")
	}
	if res.State != pkce.State {
		return nil, errors.New("oauth authorization response did not match the request state")
	}
	return c.ExchangeCode(ctx, res.Code, pkce.Verifier, OAuthRedirectURI)
}

// ExchangeCode trades an authorization code and PKCE verifier for tokens at
// the OAuth token endpoint. A challenged, region-blocked or unreachable host
// moves to its sibling before failing; a real OAuth error (for example an
// invalid code) is final.
func (c *Client) ExchangeCode(ctx context.Context, code, verifier, redirectURI string) (*Tokens, error) {
	// The FxA OAuth token endpoint follows RFC 6749 and expects a form body.
	// A JSON body is accepted by the auth server's /oauth/token route used by
	// password accounts, but not reliably by the public OAuth endpoint.
	body := url.Values{
		"client_id":     {OAuthClientID},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	}
	var lastErr error
	for _, endpoint := range c.tokenEndpoints() {
		raw, err := c.doEndpointForm(ctx, endpoint, body)
		if err == nil {
			tokens, parseErr := c.tokensFromResponse(raw, "")
			if parseErr != nil {
				return nil, parseErr
			}
			tokens.ClientID = OAuthClientID
			// The browser flow stores Account.FxA and only refreshes it through the
			// ordinary refresh path; an access-only response would silently become
			// dead at expiry, so reject it as a terminal unusable response.
			if strings.TrimSpace(tokens.RefreshToken) == "" {
				return nil, &ResponseError{Err: errors.New("oauth response is missing refresh_token")}
			}
			return tokens, nil
		}
		var chErr *ChallengeError
		if !errors.As(err, &chErr) && !isTransportError(err) && !isRegionBlocked(err) {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

// tokenEndpoints returns the configured OAuth token endpoint followed by its
// sibling, skipping duplicates. A custom endpoint without a fallback stays a
// single attempt.
func (c *Client) tokenEndpoints() []string {
	primary := c.tokenEndpoint()
	if c.TokenFallbackEndpoint == "" || c.TokenFallbackEndpoint == primary {
		return []string{primary}
	}
	return []string{primary, c.TokenFallbackEndpoint}
}

// Profile fetches the signed-in account's identity with a bearer token.
func (c *Client) Profile(ctx context.Context, accessToken string) (*Profile, error) {
	raw, err := c.doEndpointJSON(ctx, http.MethodGet, c.profileEndpoint(), accessToken, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		UID         string `json:"uid"`
		Email       string `json:"email"`
		DisplayName string `json:"displayName"`
		Avatar      string `json:"avatar"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("profile response: %w", err)
	}
	if resp.UID == "" {
		return nil, errors.New("profile response is missing uid")
	}
	if resp.Email == "" {
		return nil, errors.New("profile response is missing email")
	}
	return &Profile{
		UID:         resp.UID,
		Email:       resp.Email,
		DisplayName: resp.DisplayName,
		Avatar:      resp.Avatar,
	}, nil
}

// Destroy revokes a refresh token. Logout calls it best-effort.
func (c *Client) Destroy(ctx context.Context, refreshToken string) error {
	_, err := c.doEndpointJSON(ctx, http.MethodPost, c.destroyEndpoint(), "",
		map[string]string{"refresh_token": refreshToken})
	return err
}

func (c *Client) authorizationEndpoint() string {
	if c.AuthorizationEndpoint != "" {
		return c.AuthorizationEndpoint
	}
	return DefaultAuthorizationEndpoint
}

func (c *Client) tokenEndpoint() string {
	if c.TokenEndpoint != "" {
		return c.TokenEndpoint
	}
	return DefaultTokenEndpoint
}

func (c *Client) profileEndpoint() string {
	if c.ProfileEndpoint != "" {
		return c.ProfileEndpoint
	}
	return DefaultProfileEndpoint
}

func (c *Client) destroyEndpoint() string {
	if c.DestroyEndpoint != "" {
		return c.DestroyEndpoint
	}
	return DefaultDestroyEndpoint
}
