//go:build windows

package windows

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/benyamin-git/kumiho/internal/platform"
)

func TestOpenDeviceRejectsFD(t *testing.T) {
	_, err := New().OpenDevice(context.Background(), platform.DeviceSpec{Name: "kumiho-test", MTU: 8500, FD: 0})
	if err == nil || err.Error() != "windows: cannot adopt an existing fd" {
		t.Fatalf("OpenDevice(FD=0) error = %v, want %q", err, "windows: cannot adopt an existing fd")
	}
}

// TestClosedDeviceIO exercises the closed paths without a driver: the device
// is marked closed up front, so Read/Write/Close must never touch the session.
func TestClosedDeviceIO(t *testing.T) {
	d := &device{name: "kumiho-test", mtu: 8500}
	d.closed.Store(true)
	if _, err := d.Read(make([]byte, 2048)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Read on closed device = %v, want os.ErrClosed", err)
	}
	if _, err := d.Write([]byte{0x45, 0x00}); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Write on closed device = %v, want os.ErrClosed", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close on closed device = %v, want nil", err)
	}
}

// TestOpenDevice creates a real adapter; the hosted runner may lack the
// driver signing/admin context, in which case the reason is printed.
func TestOpenDevice(t *testing.T) {
	const name = "kumiho-test-device"
	d, err := openDevice(name, 8500)
	if err != nil {
		t.Skipf("wintun adapter unavailable (CreateAdapter failed): %v", err)
	}
	defer d.Close()
	if d.Name() != name || d.MTU() != 8500 {
		t.Fatalf("Name()=%q MTU()=%d, want %q 8500", d.Name(), d.MTU(), name)
	}
	if d.LUID() == 0 {
		t.Fatal("LUID() = 0, want the adapter's nonzero interface LUID")
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := d.Read(make([]byte, 2048)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Read after Close = %v, want os.ErrClosed", err)
	}
}
