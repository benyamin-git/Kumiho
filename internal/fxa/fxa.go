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
	StatusCode int
	Errno      int
	Message    string
}

func (e *APIError) Error() string {
	if e.Errno > 0 {
		return fmt.Sprintf("fxa error %d (HTTP %d): %s", e.Errno, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("fxa error (HTTP %d): %s", e.StatusCode, e.Message)
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

// ChallengeError is returned when the edge demands a Fastly anti-bot
// challenge and no solver is available. The solver lands in milestone M3.
type ChallengeError struct {
	StatusCode int
}

func (e *ChallengeError) Error() string {
	return fmt.Sprintf("anti-bot challenge (HTTP %d); the challenge solver is not wired up yet", e.StatusCode)
}

// ChallengeSolver solves an anti-bot challenge so a retried request can
// succeed. Implemented in internal/fastly (M3).
type ChallengeSolver interface {
	Solve(ctx context.Context) error
}
