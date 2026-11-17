package engine

import (
	"context"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/platform"
)

// TestCloseWaitsForInFlightNetcfgReapply pins the teardown ordering: a
// settings re-apply that is already running must finish before Cleanup, so a
// disconnected host cannot be left with kill-switch or routing state.
func TestCloseWaitsForInFlightNetcfgReapply(t *testing.T) {
	fp := newFakeProvider()
	nc := fp.NetConfig().(*fakeNetConfig)

	applyStarted := make(chan struct{})
	release := make(chan struct{})
	cleanupReached := make(chan struct{})
	nc.onApply = func(platform.NetConfigSpec) {
		close(applyStarted)
		<-release
	}
	nc.onCleanup = func() {
		close(cleanupReached)
	}

	res := &tunnelResources{nc: nc}
	reapplyDone := make(chan struct{})
	go func() {
		_ = res.Reapply(context.Background(), platform.NetConfigSpec{TunName: "foxy0"})
		close(reapplyDone)
	}()
	<-applyStarted

	closeDone := make(chan struct{})
	go func() {
		res.Close()
		close(closeDone)
	}()

	select {
	case <-cleanupReached:
		t.Fatal("Cleanup ran while a netcfg re-apply was still in flight")
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	<-reapplyDone
	<-closeDone

	// A re-apply that starts after teardown must not touch the platform.
	nc.mu.Lock()
	before := nc.applies
	nc.mu.Unlock()
	if err := res.Reapply(context.Background(), platform.NetConfigSpec{TunName: "foxy0"}); err == nil {
		t.Fatal("Reapply succeeded after the resources were closed")
	}
	nc.mu.Lock()
	after := nc.applies
	nc.mu.Unlock()
	if after != before {
		t.Fatal("Reapply applied netcfg after teardown")
	}
}
