package engine

import (
	"net/http"
	"testing"

	"github.com/benyamin-git/kumiho/internal/guardian"
)

// TestPublishRotationDropsTheInheritedPass covers quota rotation during a
// connect: the pass cached for the previous account must not be reused for
// the account the rotation selects.
func TestPublishRotationDropsTheInheritedPass(t *testing.T) {
	c, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {})
	c.mu.Lock()
	c.accounts = &accountsFile{Version: 2, Active: "a", Accounts: []*Account{
		{ID: "a", Email: "a@example.com"},
		{ID: "b", Email: "b@example.com"},
	}}
	c.activeID = "a"
	c.email = "a@example.com"
	c.state = StateConnecting
	c.pass = &guardian.Pass{Token: "inherited"}
	c.passPrepared = true
	gen := c.authGen
	c.mu.Unlock()

	if err := c.publishRotation(gen, &rotationCandidate{id: "b"}, nil, ""); err != nil {
		t.Fatalf("publishRotation: %v", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.activeID != "b" {
		t.Fatalf("active account = %q, want b", c.activeID)
	}
	if c.pass != nil || c.passPrepared {
		t.Fatalf("inherited pass survived the account switch: pass=%+v prepared=%v", c.pass, c.passPrepared)
	}
}
