package engine

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/guardian"
)

// TestPersistPassAfterSignOutWritesNothing covers a pass fetch that finishes
// after logout: the bearer must not be published or written to disk.
func TestPersistPassAfterSignOutWritesNothing(t *testing.T) {
	ctrl, _ := newTestController(t, func(http.ResponseWriter, *http.Request) {})
	ctrl.mu.Lock()
	ctrl.tokens = nil
	ctrl.email = "user@example.com"
	ctrl.mu.Unlock()

	ctrl.persistPass(&guardian.Pass{Token: "late", ExpiresAt: time.Now().Add(10 * time.Minute).Unix()})

	if _, err := os.Stat(ctrl.store.PassPath()); !os.IsNotExist(err) {
		t.Fatalf("pass file exists after sign-out: %v", err)
	}
	ctrl.mu.Lock()
	pass := ctrl.pass
	ctrl.mu.Unlock()
	if pass != nil {
		t.Fatalf("live pass = %q after sign-out, want none", pass.Token)
	}
}
