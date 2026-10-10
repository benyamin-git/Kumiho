//go:build linux && !android

package linux

import (
	"fmt"
	"io"
	"os"
	"sync"

	"golang.org/x/sys/unix"

	"github.com/benyamin-git/kumiho/internal/platform"
)

// Device is a Linux TUN interface (IFF_TUN | IFF_NO_PI, non-persistent).
//
// The fd is opened non-blocking so os.NewFile registers it with the runtime
// poller: a pending Read then returns os.ErrClosed as soon as Close runs,
// which is what lets tun2socks' IO loops (and Engine.Stop) terminate.
type Device struct {
	mu   sync.Mutex
	file *os.File
	name string
	mtu  int
}

// Open creates the TUN interface; the kernel destroys it when Close runs.
// Requires CAP_NET_ADMIN (the systemd unit or setcap provides it).
func Open(name string, mtu int) (*Device, error) {
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("tun: open /dev/net/tun: %w", err)
	}
	ifr, err := unix.NewIfreq(name)
	if err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("tun: interface name %q: %w", name, err)
	}
	ifr.SetUint16(unix.IFF_TUN | unix.IFF_NO_PI)
	if err := unix.IoctlIfreq(fd, unix.TUNSETIFF, ifr); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("tun: TUNSETIFF %q (CAP_NET_ADMIN required): %w", name, err)
	}
	return &Device{file: os.NewFile(uintptr(fd), "/dev/net/tun"), name: ifr.Name(), mtu: mtu}, nil
}

// Read returns the next inbound packet. A concurrent Close unblocks it with
// os.ErrClosed (the poller-backed fd makes that deterministic).
func (d *Device) Read(p []byte) (int, error) {
	d.mu.Lock()
	f := d.file
	d.mu.Unlock()
	if f == nil {
		return 0, os.ErrClosed
	}
	return f.Read(p)
}

// Write sends one outbound packet to the kernel.
func (d *Device) Write(p []byte) (int, error) {
	d.mu.Lock()
	f := d.file
	d.mu.Unlock()
	if f == nil {
		return 0, os.ErrClosed
	}
	return f.Write(p)
}

// Name returns the actual interface name.
func (d *Device) Name() string { return d.name }

// MTU returns the configured MTU.
func (d *Device) MTU() int { return d.mtu }

// Close destroys the interface and unblocks any pending Read. Idempotent.
func (d *Device) Close() error {
	d.mu.Lock()
	f := d.file
	d.file = nil
	d.mu.Unlock()
	if f == nil {
		return nil
	}
	return f.Close()
}

var _ io.ReadWriteCloser = (*Device)(nil)
var _ platform.Device = (*Device)(nil)
