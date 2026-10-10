//go:build windows

package windows

import (
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"

	"github.com/benyamin-git/kumiho/internal/platform"
)

// ringCapacity is the Wintun ring-buffer size (8 MiB, wireguard-go's proven
// value; Wintun requires a power of two in [0x20000, 0x4000000]).
const ringCapacity = 0x800000

// device is a Wintun adapter exposed as a packet device. It owns one Wintun
// session; the ring is pumped from the caller's goroutines (tun2socks' IO
// loops), so Read and Write each assume a single caller.
type device struct {
	name string
	mtu  int
	luid uint64

	adapter  *wintun.Adapter
	session  wintun.Session
	readWait windows.Handle

	closed atomic.Bool
	// rw guards the session lifetime: Read holds RLock across a blocking wait
	// so Close can wake it and only then end the session.
	rw sync.RWMutex
}

// openDevice creates the Wintun adapter and starts its packet session. The
// adapter is created with the fixed netcfg GUID so restarts reuse the same
// adapter identity (deterministic NLA generation). mtu is recorded, not
// applied: as on Linux, netcfg owns the interface configuration and applies
// the MTU via setIPInterfaceEntry.
func openDevice(name string, mtu int) (*device, error) {
	adapter, err := wintun.CreateAdapter(name, "Kumiho", &fixedAdapterGUID)
	if err != nil {
		return nil, fmt.Errorf("wintun: create adapter %q: %w", name, err)
	}
	session, err := adapter.StartSession(ringCapacity)
	if err != nil {
		_ = adapter.Close()
		return nil, fmt.Errorf("wintun: start session on %q: %w", name, err)
	}
	return &device{
		name:     name,
		mtu:      mtu,
		luid:     adapter.LUID(),
		adapter:  adapter,
		session:  session,
		readWait: session.ReadWaitEvent(),
	}, nil
}

// Read returns the next inbound packet, blocking while the ring is empty. A
// concurrent Close signals the read-wait event and makes it return
// os.ErrClosed (Windows parity with the poller-backed Linux fd).
func (d *device) Read(p []byte) (int, error) {
	if d.closed.Load() {
		return 0, os.ErrClosed
	}
	d.rw.RLock()
	defer d.rw.RUnlock()
	if d.closed.Load() {
		return 0, os.ErrClosed
	}
	for {
		packet, err := d.session.ReceivePacket()
		if err == nil {
			n := copy(p, packet)
			d.session.ReleaseReceivePacket(packet)
			return n, nil
		}
		if err == windows.ERROR_HANDLE_EOF {
			return 0, os.ErrClosed
		}
		if err != windows.ERROR_NO_MORE_ITEMS {
			return 0, fmt.Errorf("wintun: receive packet: %w", err)
		}
		if d.closed.Load() {
			return 0, os.ErrClosed
		}
		if _, err := windows.WaitForSingleObject(d.readWait, windows.INFINITE); err != nil {
			return 0, fmt.Errorf("wintun: wait for packet: %w", err)
		}
	}
}

// Write sends one outbound packet. A full ring drops the packet (Wintun's
// documented ERROR_BUFFER_OVERFLOW behavior) instead of stalling the path.
func (d *device) Write(p []byte) (int, error) {
	d.rw.RLock()
	defer d.rw.RUnlock()
	if d.closed.Load() {
		return 0, os.ErrClosed
	}
	packet, err := d.session.AllocateSendPacket(len(p))
	switch err {
	case nil:
		copy(packet, p)
		d.session.SendPacket(packet)
		return len(p), nil
	case windows.ERROR_BUFFER_OVERFLOW:
		return len(p), nil
	case windows.ERROR_HANDLE_EOF:
		return 0, os.ErrClosed
	default:
		return 0, fmt.Errorf("wintun: allocate send packet: %w", err)
	}
}

// Name returns the adapter name.
func (d *device) Name() string { return d.name }

// MTU returns the MTU recorded at open (netcfg applies it, not the device).
func (d *device) MTU() int { return d.mtu }

// LUID returns the adapter's Windows interface LUID.
func (d *device) LUID() uint64 { return d.luid }

// Close ends the session and removes the adapter. It signals the read-wait
// event first: WintunEndSession frees the session and closes that event, so a
// reader parked in Read must be woken and drained before the session ends.
// Idempotent.
func (d *device) Close() error {
	if d.closed.Swap(true) {
		return nil
	}
	_ = windows.SetEvent(d.readWait)
	d.rw.Lock()
	defer d.rw.Unlock()
	d.session.End()
	return d.adapter.Close()
}

var _ io.ReadWriteCloser = (*device)(nil)
var _ platform.Device = (*device)(nil)
