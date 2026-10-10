//go:build linux && !android

package linux

import (
	"context"
	"testing"
	"time"
)

// WatchLinks must return a usable coalesced channel and stop its netlink
// subscriptions when the context is cancelled.
func TestWatchLinksReturnsChannel(t *testing.T) {
	p := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := p.WatchLinks(ctx)
	if err != nil {
		t.Fatalf("WatchLinks: %v", err)
	}
	if ch == nil {
		t.Fatal("WatchLinks returned a nil channel")
	}

	select {
	case <-ch:
	default:
	}

	cancel()
}

// TestCoalesceCollapsesBurst pins the cap-1 guarantee: a burst of source
// events must produce exactly one observable wake-up and nothing more.
func TestCoalesceCollapsesBurst(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	src := make(chan struct{})
	events := coalesce(done, src)

	for i := 0; i < 3; i++ {
		src <- struct{}{}
	}

	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("no wake-up after a burst of three events")
	}

	select {
	case <-events:
		t.Fatal("more than one wake-up after a burst of three events")
	case <-time.After(50 * time.Millisecond):
	}
}

// TestCoalesceStopsOnDone pins that closing done terminates delivery even if
// a source still has events queued.
func TestCoalesceStopsOnDone(t *testing.T) {
	done := make(chan struct{})
	src := make(chan struct{})
	events := coalesce(done, src)

	close(done)

	// The coalescer commits to done when it closes, so this send must not be
	// received; non-blocking so the test cannot hang either way.
	select {
	case src <- struct{}{}:
	default:
	}

	select {
	case <-events:
		t.Fatal("wake-up delivered after done was closed")
	case <-time.After(50 * time.Millisecond):
	}
}

// HasDefaultRoute probes the main routing table and must not panic on any host.
func TestHasDefaultRoute(t *testing.T) {
	p := New()
	_ = p.HasDefaultRoute()
}
