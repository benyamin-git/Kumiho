//go:build windows

package windows

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// defaultRow builds a route table row whose destination is a /0 prefix
// (default route) of the requested family on the requested interface.
func defaultRow(luid uint64, v6 bool) windows.MibIpForwardRow2 {
	dst := net.IPv4zero
	if v6 {
		dst = net.IPv6zero
	}
	return routeRow(luid, dst, 0, dst, 0)
}

// TestHasPhysicalDefaultRoute pins the LUID filter: only a /0 row on an
// interface other than the tunnel's counts as physical connectivity.
func TestHasPhysicalDefaultRoute(t *testing.T) {
	const tunnel = uint64(0x100)
	other := uint64(0x200)

	tests := []struct {
		name   string
		routes []windows.MibIpForwardRow2
		want   bool
	}{
		{"empty table", nil, false},
		{"tunnel ipv4 default only", []windows.MibIpForwardRow2{defaultRow(tunnel, false)}, false},
		{"tunnel ipv6 default only", []windows.MibIpForwardRow2{defaultRow(tunnel, true)}, false},
		{"physical ipv4 default", []windows.MibIpForwardRow2{defaultRow(other, false)}, true},
		{"physical ipv6 default", []windows.MibIpForwardRow2{defaultRow(other, true)}, true},
		{"tunnel plus physical default", []windows.MibIpForwardRow2{
			defaultRow(tunnel, false),
			defaultRow(other, false),
		}, true},
		{"non-default route on another interface", []windows.MibIpForwardRow2{
			routeRow(other, net.IPv4zero, 8, net.IPv4zero, 0),
		}, false},
		{"default on an unspecified luid", []windows.MibIpForwardRow2{defaultRow(0, false)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasPhysicalDefaultRoute(tt.routes, tunnel); got != tt.want {
				t.Fatalf("hasPhysicalDefaultRoute(%+v, %#x) = %v, want %v", tt.routes, tunnel, got, tt.want)
			}
		})
	}
}

// TestInterfaceChangedCoalesces pins the cap-1 contract directly on the
// callback: two changes while the first wake-up is unconsumed must leave
// exactly one pending event.
func TestInterfaceChangedCoalesces(t *testing.T) {
	w := &linkWatcher{events: make(chan struct{}, 1)}
	interfaceChanged(unsafe.Pointer(w), nil, 0)
	interfaceChanged(unsafe.Pointer(w), nil, 0)

	select {
	case <-w.events:
	default:
		t.Fatal("no wake-up delivered after an interface change")
	}
	select {
	case <-w.events:
		t.Fatal("more than one wake-up delivered for a burst of two changes")
	default:
	}
}

// TestWatchLinksReturnsChannel uses the real IP Helper registration: it must
// succeed without elevation and return a usable channel. Driver events are
// deliberately not asserted (they depend on the host's network state).
func TestWatchLinksReturnsChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := New().WatchLinks(ctx)
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

// TestWatchLinksUnregistersOnCancel substitutes the registration pair so the
// cancellation contract is observable without a network change: the shared
// callback is registered with InitialNotification false, and cancelling the
// context must unregister exactly the handle that was returned.
func TestWatchLinksUnregistersOnCancel(t *testing.T) {
	origRegister, origCancel := registerInterfaceWatch, cancelInterfaceWatch
	t.Cleanup(func() {
		registerInterfaceWatch, cancelInterfaceWatch = origRegister, origCancel
	})

	type registration struct {
		family   uint16
		callback uintptr
		context  unsafe.Pointer
		initial  bool
	}
	const handle = windows.Handle(0x1234)
	registered := make(chan registration, 1)
	unregistered := make(chan windows.Handle, 1)
	registerInterfaceWatch = func(family uint16, callback uintptr, callerContext unsafe.Pointer, initialNotification bool) (windows.Handle, error) {
		registered <- registration{family, callback, callerContext, initialNotification}
		return handle, nil
	}
	cancelInterfaceWatch = func(h windows.Handle) error {
		unregistered <- h
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := New().WatchLinks(ctx)
	if err != nil {
		t.Fatalf("WatchLinks: %v", err)
	}
	if ch == nil {
		t.Fatal("WatchLinks returned a nil channel")
	}
	select {
	case <-ch:
		t.Fatal("wake-up delivered before any interface change")
	default:
	}

	select {
	case reg := <-registered:
		if reg.family != windows.AF_UNSPEC {
			t.Errorf("registered family = %d, want AF_UNSPEC", reg.family)
		}
		if reg.callback != interfaceChangedCallback() {
			t.Errorf("registered callback = %#x, want the shared trampoline %#x", reg.callback, interfaceChangedCallback())
		}
		if reg.context == nil {
			t.Error("registered with a nil caller context")
		}
		if reg.initial {
			t.Error("registered with InitialNotification true, want false")
		}
	case <-time.After(time.Second):
		t.Fatal("WatchLinks did not register for notifications")
	}

	cancel()

	select {
	case got := <-unregistered:
		if got != handle {
			t.Fatalf("unregistered handle = %#x, want %#x", got, handle)
		}
	case <-time.After(time.Second):
		t.Fatal("WatchLinks did not unregister after cancel")
	}
}

// TestWatchLinksRegistrationFailure pins the fallback contract: a failed
// registration is returned to the caller (the watchdog then polls) and no
// cancellation is ever issued for a handle that was never created.
func TestWatchLinksRegistrationFailure(t *testing.T) {
	origRegister, origCancel := registerInterfaceWatch, cancelInterfaceWatch
	t.Cleanup(func() {
		registerInterfaceWatch, cancelInterfaceWatch = origRegister, origCancel
	})

	wantErr := errors.New("notification registration failed")
	cancelCalled := false
	registerInterfaceWatch = func(uint16, uintptr, unsafe.Pointer, bool) (windows.Handle, error) {
		return 0, wantErr
	}
	cancelInterfaceWatch = func(windows.Handle) error {
		cancelCalled = true
		return nil
	}

	ch, err := New().WatchLinks(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("WatchLinks error = %v, want %v", err, wantErr)
	}
	if ch != nil {
		t.Fatalf("WatchLinks channel = %v, want nil on registration failure", ch)
	}
	if cancelCalled {
		t.Fatal("cancel called without a successful registration")
	}
}
