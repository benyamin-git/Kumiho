package daemon

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/upstream"
)

// While the physical network is down the watchdog parks in WAITING_NETWORK
// without burning dial attempts, and a returning default route resumes it
// (PLAN.md §4.2).
func TestWatchdogParksWhenNetworkIsDown(t *testing.T) {
	compressWatchdog(t)
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.guardian.BaseURL = guardianPassServer(t, "pass-1", new(atomic.Int32)).URL
	ctrl.serverListURL = serverListServer(t).URL
	ctrl.exitCheckURL = ""
	ctrl.socksPortOverride = freePort(t)

	var routeUp atomic.Bool
	ctrl.routeProbe = func() bool { return routeUp.Load() }

	first, second := newFakeSession(), newFakeSession()
	var dials atomic.Int32
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		if dials.Add(1) == 1 {
			return first, nil
		}
		if !routeUp.Load() {
			return nil, errors.New("network is unreachable")
		}
		return second, nil
	}
	loginProxyOnly(t, ctrl)
	defer ctrl.stopTunnel("test cleanup")

	_ = first.Close()
	waitFor(t, 3*time.Second, func() bool {
		return ctrl.Snapshot().State == string(StateWaitingNetwork)
	}, "watchdog did not park in WAITING_NETWORK")

	// No further attempts while parked.
	parked := dials.Load()
	time.Sleep(60 * time.Millisecond)
	if got := dials.Load(); got != parked {
		t.Fatalf("dials while parked = %d, want %d", got, parked)
	}

	// Connectivity returns: wake and reconnect promptly.
	routeUp.Store(true)
	waitFor(t, 3*time.Second, func() bool {
		return ctrl.currentSession() == second &&
			ctrl.Snapshot().State == string(StateProxyOnly)
	}, "watchdog did not resume after the network returned")
}
