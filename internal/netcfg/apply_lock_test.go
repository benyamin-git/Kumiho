package netcfg

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestApplyAndCleanupDoNotOverlap holds the first command of Apply and starts
// a Cleanup: the teardown must wait for the half-applied configuration.
func TestApplyAndCleanupDoNotOverlap(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	started := make(chan struct{})
	second := make(chan struct{})
	release := make(chan struct{})

	a := &Applier{
		Run: func(context.Context, string, ...string) (string, error) {
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			switch n {
			case 1:
				close(started)
				<-release
			case 2:
				close(second)
			}
			return "", nil
		},
	}

	ctx := context.Background()
	applyDone := make(chan struct{})
	go func() {
		_ = a.Apply(ctx, Config{})
		close(applyDone)
	}()
	<-started

	cleanupDone := make(chan struct{})
	go func() {
		a.Cleanup(ctx, Config{})
		close(cleanupDone)
	}()

	select {
	case <-second:
		t.Fatal("Cleanup ran a command while Apply was in flight")
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	<-applyDone
	<-cleanupDone
}
