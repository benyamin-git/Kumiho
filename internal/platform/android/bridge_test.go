package android

import (
	"errors"
	"strings"
	"testing"
)

type panickingBridge struct{}

func (panickingBridge) AcquireFD(int) (int, error) {
	panic("configure() must be called before acquireFD()")
}

func (panickingBridge) NetworkAvailable() bool { panic("the bridge is gone") }

type staticBridge struct {
	fd        int
	err       error
	available bool
}

func (b staticBridge) AcquireFD(int) (int, error) { return b.fd, b.err }
func (b staticBridge) NetworkAvailable() bool     { return b.available }

func TestBridgePanicsBecomeErrors(t *testing.T) {
	fd, err := acquireFD(panickingBridge{}, 1500)
	if err == nil || !strings.Contains(err.Error(), "configure() must be called before acquireFD()") {
		t.Fatalf("acquireFD panic = (%d, %v), want an error naming the Kotlin failure", fd, err)
	}
	if fd != -1 {
		t.Fatalf("acquireFD fd = %d after a panic, want -1", fd)
	}
	if !networkAvailable(panickingBridge{}) {
		t.Fatal("a panicking bridge must fail open, not tear the tunnel down")
	}
}

func TestBridgeResultsPassThrough(t *testing.T) {
	if fd, err := acquireFD(staticBridge{fd: 42}, 1500); err != nil || fd != 42 {
		t.Fatalf("acquireFD = (%d, %v), want (42, nil)", fd, err)
	}
	if networkAvailable(staticBridge{available: false}) {
		t.Fatal("networkAvailable = true, want false")
	}
	if !networkAvailable(staticBridge{available: true}) {
		t.Fatal("networkAvailable = false, want true")
	}
}

func TestBridgeErrorPassesThrough(t *testing.T) {
	want := errors.New("establish() returned null")
	if _, err := acquireFD(staticBridge{fd: -1, err: want}, 1500); !errors.Is(err, want) {
		t.Fatalf("acquireFD error = %v, want %v", err, want)
	}
}
