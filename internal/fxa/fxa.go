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
	// ClientID is the Firefox VPN OAuth client.
	ClientID = "5882386c6d801776"
	// Scope requests the VPN entitlement plus profile.
	Scope = "profile https://identity.mozilla.com/apps/vpn"
	// UserAgent identifies as the official Mozilla VPN client (Linux).
	UserAgent = "MozillaVPN/2.35.0 (sys:linux; iap:true)"

	// DefaultAuthorizationEndpoint is the FxA browser consent page.
	DefaultAuthorizationEndpoint = "https://accounts.firefox.com/authorization"
	// DefaultTokenEndpoint exchanges an authorization code for tokens. It is
	// distinct from the auth-server BaseURL used by the password flow and by
	// Refresh. D39 assumes the resulting refresh token is accepted there; that
	// is unproven until Gate G1 signs in, forces a refresh and verifies it.
	// Do not change the refresh endpoint without that evidence.
	DefaultTokenEndpoint = "https://oauth.accounts.firefox.com/v1/token"
	// OAuthRedirectURI is the client's registered out-of-band redirect.
	OAuthRedirectURI = "urn:ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel"
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
