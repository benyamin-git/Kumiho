//go:build windows

package windows

import (
	"context"
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// registerInterfaceWatch and cancelInterfaceWatch are the IP Helper
// notification pair. They are package variables so the windows tests can
// substitute a fake and observe registration and cancellation without waiting
// for a driver event.
var (
	registerInterfaceWatch = notifyIPInterfaceChange
	cancelInterfaceWatch   = windows.CancelMibChangeNotify2
)

// linkWatcher is the state one WatchLinks registration delivers into. Its
// address is handed to the IP Helper as the callback's caller context, so it
// must stay reachable until the registration is cancelled; the cancel
// goroutine's reference is what keeps it alive.
type linkWatcher struct {
	events chan struct{}
	handle windows.Handle
}

// interfaceChangedCallback is the process-wide C trampoline shared by every
// registration. Callback slots are never released, and the daemon registers
// one watcher per connection lifetime, so allocating a callback per
// WatchLinks would leak a slot per connect; registrations are told apart by
// their caller context instead. Allocation is lazy, like the IP Helper
// procedures: package initialization touches no Windows API.
var interfaceChangedCallback = sync.OnceValue(func() uintptr {
	return windows.NewCallback(interfaceChanged)
})

// interfaceChanged receives an interface add/delete/update notification and
// collapses it into one pending wake-up. The send must never block: the
// callback runs on an arbitrary system thread and the watchdog only needs to
// know that something changed. Row and notification type are deliberately
// unused; any interface change can move the default route.
func interfaceChanged(callerContext unsafe.Pointer, _ *windows.MibIpInterfaceRow, _ uint32) uintptr {
	w := (*linkWatcher)(callerContext)
	select {
	case w.events <- struct{}{}:
	default: // a burst collapses into one pending wake-up
	}
	return 0
}

// WatchLinks subscribes to interface-change notifications until ctx is done.
// The returned channel has capacity one and coalesces bursts: several changes
// while the watchdog has not consumed the previous wake-up collapse into a
// single pending event. Registration failure is returned to the caller, whose
// watchdog then falls back to its poll tick (Linux parity).
func (p *Provider) WatchLinks(ctx context.Context) (<-chan struct{}, error) {
	w := &linkWatcher{events: make(chan struct{}, 1)}
	handle, err := registerInterfaceWatch(windows.AF_UNSPEC, interfaceChangedCallback(), unsafe.Pointer(w), false)
	if err != nil {
		return nil, fmt.Errorf("windows: watch interfaces: %w", err)
	}
	w.handle = handle
	// Capture the cancel function before the goroutine starts: the seam is a
	// package variable, and the goroutine must not observe a later swap.
	cancel := cancelInterfaceWatch

	go func() {
		<-ctx.Done()
		// CancelMibChangeNotify2 waits for an in-flight callback to return,
		// so afterwards the caller context is no longer touched by Windows
		// and w can be collected.
		_ = cancel(w.handle)
	}()
	return w.events, nil
}

// tunnelAdapterLUID resolves the route-table LUID of Kumiho's tunnel adapter
// from the fixed GUID that creates it (device.go). A missing adapter yields
// zero: no route table row ever carries LUID 0, so nothing is filtered and
// the probe behaves as if no tunnel exists — which is true.
func tunnelAdapterLUID() uint64 {
	luid, err := convertInterfaceGUIDToLUID(fixedAdapterGUID)
	if err != nil {
		return 0
	}
	return luid
}

// HasDefaultRoute reports whether the physical network has a default route.
// The scan skips the tunnel adapter's own /0 rows: while connected, Kumiho's
// metric-0 tunnel default wins route lookup but is not physical connectivity.
// This mirrors the Linux probe, whose main-table lookup never sees Kumiho's
// own routing tables. Any enumeration failure reads as "no route", exactly
// like a failed Linux RouteGet.
func (p *Provider) HasDefaultRoute() bool {
	routes, err := getIPForwardTable2(windows.AF_UNSPEC)
	if err != nil {
		return false
	}
	return hasPhysicalDefaultRoute(routes, tunnelAdapterLUID())
}

// hasPhysicalDefaultRoute reports whether the table contains a 0.0.0.0/0 or
// ::/0 row on an interface other than the tunnel's. Rows with an unspecified
// LUID are not route entries and cannot count.
func hasPhysicalDefaultRoute(routes []windows.MibIpForwardRow2, tunnelLUID uint64) bool {
	for i := range routes {
		row := &routes[i]
		if row.DestinationPrefix.PrefixLength != 0 {
			continue
		}
		if row.InterfaceLuid == 0 || row.InterfaceLuid == tunnelLUID {
			continue
		}
		switch row.DestinationPrefix.Prefix.Family {
		case windows.AF_INET, windows.AF_INET6:
			return true
		}
	}
	return false
}
