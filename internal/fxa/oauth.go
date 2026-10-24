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
func (c *Client) AuthorizationURL(email string, forceAuth bool, state, challenge string) string {
	q := url.Values{
		"client_id":             {ClientID},
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

// ExchangeCode trades an authorization code and PKCE verifier for tokens at
// the OAuth token endpoint.
func (c *Client) ExchangeCode(ctx context.Context, code, verifier, redirectURI string) (*Tokens, error) {
	body := map[string]string{
		"client_id":     ClientID,
		"grant_type":    "authorization_code",
		"code":          code,
		"code_verifier": verifier,
		"redirect_uri":  redirectURI,
	}
	raw, err := c.doEndpointJSON(ctx, http.MethodPost, c.tokenEndpoint(), "", body)
	if err != nil {
		return nil, err
	}
	return c.tokensFromResponse(raw, "")
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
