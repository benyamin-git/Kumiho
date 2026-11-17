package gui

import (
	"context"
	"testing"

	"github.com/benyamin-git/kumiho/internal/api"
)

// cancelAwareDaemon honours the context like the real IPC client does.
type cancelAwareDaemon struct {
	*fakeDaemon
}

func (d cancelAwareDaemon) Account(ctx context.Context) (api.AccountInfo, error) {
	if err := ctx.Err(); err != nil {
		return api.AccountInfo{}, err
	}
	return d.fakeDaemon.Account(ctx)
}

// TestEnsureAccountSurvivesCallerCancellation covers the background account
// refresh outliving the short-lived context of the action that started it.
func TestEnsureAccountSurvivesCallerCancellation(t *testing.T) {
	ctrl := NewController(cancelAwareDaemon{newFakeDaemon()}, quietOptions())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ctrl.ensureAccount(ctx)

	waitFor(t, "account snapshot", func() bool {
		ctrl.mu.Lock()
		defer ctrl.mu.Unlock()
		return ctrl.accountLoaded
	})
}
