// Package fxa implements the Firefox Accounts login and OAuth flows used by
// the Firefox VPN entitlement. Behavior mirrors FoxyVPN (MIT) as documented
// in PLAN.md §2.1; no code is copied from unlicensed projects.
package fxa

import (
	"context"
	"fmt"
	"net/http"
)

// Protocol constants (PLAN.md §2.1, verified against the reference client).
const (
	// DefaultBaseURL is the FxA auth server root.
	DefaultBaseURL = "https://api.accounts.firefox.com/v1"
	// DefaultFallbackBaseURL is the same FxA auth server behind Mozilla's
	// OAuth hostname. Fastly can hard-block POST /account/login on the api
	// host (HTTP 406 with an empty body and no challenge page, which no
	// solved challenge can clear) while the oauth host keeps answering with
	// the identical auth-server build. The fallback is tried after a 406 and
	// before any solve, so the normal path never depends on it.
	DefaultFallbackBaseURL = "https://oauth.accounts.firefox.com/v1"
	// ClientID is Firefox Desktop's OAuth client. The password flow uses it,
	// and it is the legacy default for accounts created before browser tokens
	// recorded their own client. Desktop relies on Firefox's in-process
	// WebChannel, so a standalone app cannot receive its URN redirect.
	ClientID = "5882386c6d801776"
	// OAuthClientID is Firefox for Android's public OAuth client. It has an
	// HTTPS success redirect registered on accounts.firefox.com, so any normal
	// browser can complete the flow and show a copyable URL. The browser flow
	// uses this client; the password flow keeps ClientID.
	OAuthClientID = "a2270f727f45f648"
	// Scope requests the VPN entitlement plus profile.
	Scope = "profile https://identity.mozilla.com/apps/vpn"
	// UserAgent identifies as the official Mozilla VPN client (Linux). It is
	// required by Guardian and Remote Settings; Fastly's FxA edge now hard
	// blocks it (empty HTTP 406, no challenge page) on the OAuth endpoints,
	// so the FxA client sends BrowserUserAgent instead.
	UserAgent = "MozillaVPN/2.35.0 (sys:linux; iap:true)"
	// BrowserUserAgent is desktop Firefox's identity, which Firefox's
	// built-in VPN sends to Firefox Accounts. Fastly accepts it on
	// /v1/oauth/token and /v1/token, where the MozillaVPN UA is blocked.
	BrowserUserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:158.0) Gecko/20100101 Firefox/158.0"

	// DefaultAuthorizationEndpoint is the FxA browser consent page.
	DefaultAuthorizationEndpoint = "https://accounts.firefox.com/authorization"
	// DefaultTokenEndpoint exchanges an authorization code for tokens. It is
	// distinct from the auth-server BaseURL used by the password flow and by
	// Refresh. D39 assumes the resulting refresh token is accepted there; that
	// is unproven until Gate G1 signs in, forces a refresh and verifies it.
	// Do not change the refresh endpoint without that evidence.
	DefaultTokenEndpoint = "https://oauth.accounts.firefox.com/v1/token"
	// OAuthRedirectURI is the HTTPS success page registered for OAuthClientID.
	// FxA navigates here with the authorization code and state in the query,
	// which a browser can display and the user can paste back. This replaces
	// Firefox Desktop's URN redirect, which only Firefox's WebChannel can
	// receive and which Chromium browsers silently block.
	OAuthRedirectURI = "https://accounts.firefox.com/oauth/success/" + OAuthClientID
	// DefaultProfileEndpoint returns the signed-in account's profile.
	DefaultProfileEndpoint = "https://profile.accounts.firefox.com/v1/profile"
	// DefaultDestroyEndpoint revokes a refresh token.
	DefaultDestroyEndpoint = "https://oauth.accounts.firefox.com/v1/destroy"

	verificationMethodEmail2FA = "email-2fa"
	errnoInvalidParameter      = 107
	defaultAccessTokenTTL      = 24 * 60 * 60
)

// LoginResult is the outcome of POST /account/login.
type LoginResult struct {
	SessionToken       string
	Verified           bool
	VerificationMethod string
	VerificationReason string
}

// Tokens is the persisted OAuth token set (tokens.json, PLAN.md §8).
type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
	ExpiresAt    int64  `json:"expires_at"`
	// ClientID records which OAuth client issued the refresh token. Browser
	// tokens are issued to OAuthClientID; password tokens and older accounts
	// use ClientID. Refresh must send the same client that issued the token.
	ClientID string `json:"client_id,omitempty"`
}

// APIError is a structured FxA error response.
type APIError struct {
	StatusCode         int
	Errno              int
	Message            string
	VerificationMethod string
	VerificationReason string
}

func (e *APIError) Error() string {
	if e.Errno > 0 {
		return fmt.Sprintf("fxa error %d (HTTP %d): %s", e.Errno, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("fxa error (HTTP %d): %s", e.StatusCode, e.Message)
}

// ResponseError marks an HTTP 2xx OAuth response whose body could not be
// parsed or validated. For a one-shot grant such as an authorization code the
// server may already have consumed the request, so callers must treat the
// response as terminal instead of retrying the same request.
type ResponseError struct {
	Err error
}

func (e *ResponseError) Error() string {
	if e == nil || e.Err == nil {
		return "oauth response is unusable"
	}
	return e.Err.Error()
}

func (e *ResponseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// PermanentRefreshRejection reports whether the stored refresh token is dead
// and the user must sign in again.
func (e *APIError) PermanentRefreshRejection() bool {
	switch e.StatusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
		return true
	}
	return false
}

// ChallengeError marks a Fastly anti-bot challenge (HTTP 406) that the
// request could not get past. It is returned when no solver is configured
// or when the solver gave up; when a solver fails, its own error is joined
// with this marker (errors.Join), so callers can both errors.As the 406
// and errors.Is the underlying cause.
type ChallengeError struct {
	StatusCode int
}

func (e *ChallengeError) Error() string {
	return fmt.Sprintf("anti-bot challenge (HTTP %d)", e.StatusCode)
}

// ChallengeSolver solves an anti-bot challenge so a retried request can
// succeed; challengedURL is the URL that returned 406. Implemented by
// internal/fastly.Solver and wired by the engine.
type ChallengeSolver interface {
	Solve(ctx context.Context, challengedURL string) error
}
