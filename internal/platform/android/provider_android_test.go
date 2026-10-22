//go:build android

package android

import (
	"context"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/benyamin-git/kumiho/internal/platform"
	"github.com/benyamin-git/kumiho/internal/settings"
)

func testPaths() settings.Paths {
	return settings.Paths{
		Config:  "/data/data/dev.benyamin.kumiho/config.toml",
		State:   "/data/data/dev.benyamin.kumiho/state.json",
		Tokens:  "/data/data/dev.benyamin.kumiho/tokens.json",
		Runtime: "/data/data/dev.benyamin.kumiho/run",
	}
}

func TestProviderIdentity(t *testing.T) {
	p := NewWithPaths(testPaths(), nil)

	if got := p.Name(); got != "android" {
		t.Errorf("Name() = %q, want %q", got, "android")
	}
	if got := p.DNSListenAddr(); got != "10.8.0.1:53" {
		t.Errorf("DNSListenAddr() = %q, want %q", got, "10.8.0.1:53")
	}
	if !p.UserspaceDNS() {
		t.Error("UserspaceDNS() = false, want true")
	}
	if got := p.TunDefaults(); got != (platform.TunDefaults{}) {
		t.Errorf("TunDefaults() = %+v, want the zero value", got)
	}
	if got := p.Paths(); got != testPaths() {
		t.Errorf("Paths() = %+v, want %+v", got, testPaths())
	}
	if got, want := p.ControlEndpoint(), "/data/data/dev.benyamin.kumiho/run/control.sock"; got != want {
		t.Errorf("ControlEndpoint() = %q, want %q", got, want)
	}
	if p.BypassDialer() == nil {
		t.Error("BypassDialer() = nil, want a plain dialer")
	}
	p.ChownControlEndpoint(p.ControlEndpoint()) // no-op, must not panic
	if !p.HasDefaultRoute() {
		t.Error("HasDefaultRoute() without a bridge = false, want true")
	}
}

func TestNetConfigIsNoOp(t *testing.T) {
	spec := platform.NetConfigSpec{
		TunName:      "tun",
		TunAddr:      "10.8.0.2",
		TunMTU:       1500,
		KillSwitch:   true,
		AllowLAN:     true,
		ExcludeCIDRs: []string{"10.0.0.0/8"},
	}
	nc := NewWithPaths(testPaths(), nil).NetConfig()
	if err := nc.Apply(context.Background(), spec); err != nil {
		t.Errorf("Apply = %v, want nil", err)
	}
	nc.Cleanup(context.Background(), spec) // must not panic
}

func TestOpenDeviceRequiresABridge(t *testing.T) {
	p := NewWithPaths(testPaths(), nil)
	if _, err := p.OpenDevice(context.Background(), platform.DeviceSpec{Name: "tun", MTU: 1500, FD: -1}); err == nil {
		t.Fatal("OpenDevice without a bridge succeeded, want an error")
	}
}

// fakeBridge is the Kotlin side of the handshake: it records the requested
// MTU and hands back a pre-opened descriptor.
type fakeBridge struct {
	fd  int
	mtu int
}

func (b *fakeBridge) AcquireFD(mtu int) (int, error) {
	b.mtu = mtu
	return b.fd, nil
}

func (b *fakeBridge) NetworkAvailable() bool { return true }

func TestOpenDeviceAdoptsTheBridgeFD(t *testing.T) {
	fds := []int{0, 0}
	if err := unix.Pipe2(fds, unix.O_CLOEXEC); err != nil {
		t.Skipf("Pipe2 unavailable: %v", err)
	}
	r, w := fds[0], fds[1]
	defer unix.Close(w)

	v4 := packet(4, 20)
	if _, err := unix.Write(w, v4); err != nil {
		t.Fatalf("write IPv4 packet: %v", err)
	}

	bridge := &fakeBridge{fd: r}
	p := NewWithPaths(testPaths(), bridge)
	dev, err := p.OpenDevice(context.Background(), platform.DeviceSpec{Name: "kumiho0", MTU: 1500, FD: -1})
	if err != nil {
		t.Fatalf("OpenDevice: %v", err)
	}
	defer dev.Close()

	if bridge.mtu != 1500 {
		t.Errorf("AcquireFD mtu = %d, want 1500", bridge.mtu)
	}
	if got := dev.Name(); got != "kumiho0" {
		t.Errorf("Name() = %q, want %q", got, "kumiho0")
	}
	buf := make([]byte, 64)
	n, err := dev.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n != len(v4) || buf[0]>>4 != 4 {
		t.Fatalf("Read = %d bytes starting %#x, want the %d-byte IPv4 packet", n, buf[0], len(v4))
	}
}

func TestOpenDeviceDefaultsTheName(t *testing.T) {
	fds := []int{0, 0}
	if err := unix.Pipe2(fds, unix.O_CLOEXEC); err != nil {
		t.Skipf("Pipe2 unavailable: %v", err)
	}
	r, w := fds[0], fds[1]
	defer unix.Close(w)

	p := NewWithPaths(testPaths(), &fakeBridge{fd: r})
	dev, err := p.OpenDevice(context.Background(), platform.DeviceSpec{MTU: 1500, FD: -1})
	if err != nil {
		t.Fatalf("OpenDevice: %v", err)
	}
	defer dev.Close()
	if got := dev.Name(); got != "tun" {
		t.Errorf("Name() = %q, want %q", got, "tun")
	}
}
