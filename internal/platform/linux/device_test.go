//go:build linux && !android

package linux

import (
	"context"
	"testing"

	"github.com/benyamin-git/kumiho/internal/platform"
)

func TestOpenFailsWithoutCapability(t *testing.T) {
	// Without CAP_NET_ADMIN Open fails with a clear message. Either way it
	// must not panic.
	if dev, err := Open("foxy0", 8500); err == nil {
		_ = dev.Close()
	}
}

// FD adoption is an Android capability; Linux must reject it explicitly.
func TestOpenDeviceRejectsFD(t *testing.T) {
	p := New()
	for _, fd := range []int{0, 3} {
		if _, err := p.OpenDevice(context.Background(), platform.DeviceSpec{Name: "foxy0", MTU: 8500, FD: fd}); err == nil {
			t.Fatalf("OpenDevice with fd %d should fail", fd)
		}
	}
	if dev, err := p.OpenDevice(context.Background(), platform.DeviceSpec{Name: "foxy0", MTU: 8500, FD: -1}); err == nil {
		_ = dev.Close()
	}
}
