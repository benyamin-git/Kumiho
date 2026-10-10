//go:build linux && !android

package linux

import (
	"context"
	"testing"
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

	cancel()
}

// HasDefaultRoute probes the main routing table and must not panic on any host.
func TestHasDefaultRoute(t *testing.T) {
	p := New()
	_ = p.HasDefaultRoute()
}
