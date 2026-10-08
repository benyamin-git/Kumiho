//go:build integration

package fxa

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLoginIntegration exercises the real FxA endpoints. Run it manually on a
// host with a real Firefox account (never in CI):
//
//	KUMIHO_TEST_EMAIL=you@example.com KUMIHO_TEST_PASSWORD='...' \
//	  go test -tags integration ./internal/fxa -run TestLoginIntegration -v
//
// Accounts with 2FA enabled cannot complete this non-interactive flow; use
// `kumiho login` or the TUI for those.
func TestLoginIntegration(t *testing.T) {
	email := os.Getenv("KUMIHO_TEST_EMAIL")
	password := os.Getenv("KUMIHO_TEST_PASSWORD")
	if email == "" || password == "" {
		t.Skip("set KUMIHO_TEST_EMAIL and KUMIHO_TEST_PASSWORD to run")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	c := NewClient()
	res, err := c.Login(ctx, email, password)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if !res.Verified {
		t.Skipf("account requires verification (%s); complete the flow with `kumiho login`", res.VerificationMethod)
	}

	tokens, err := c.OAuthToken(ctx, res.SessionToken)
	if err != nil {
		t.Fatalf("oauth token: %v", err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatal("oauth response missing tokens")
	}
	if !c.AccessTokenValid(tokens) {
		t.Fatal("issued access token is not valid")
	}

	refreshed, err := c.Refresh(ctx, tokens.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshed.AccessToken == "" {
		t.Fatal("refresh returned no access token")
	}
	t.Logf("login, oauth grant, and refresh OK; new access token expires %s",
		time.Unix(refreshed.ExpiresAt, 0).UTC().Format(time.RFC3339))
}
