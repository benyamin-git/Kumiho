//go:build linux

package android

import (
	"testing"

	"golang.org/x/sys/unix"
)

// adoptFD takes ownership of a raw descriptor; the IPv6 filter in front of it
// must drop IPv6 packets and Close must close the underlying read end.
func TestAdoptFDAdoptsAPipe(t *testing.T) {
	fds := []int{0, 0}
	if err := unix.Pipe2(fds, unix.O_CLOEXEC); err != nil {
		t.Fatalf("Pipe2: %v", err)
	}
	r, w := fds[0], fds[1]
	defer unix.Close(w)

	dev, err := adoptFD(r, "tun")
	if err != nil {
		t.Fatalf("adoptFD: %v", err)
	}
	if got := dev.Name(); got != "tun" {
		t.Errorf("Name() = %q, want %q", got, "tun")
	}

	v6 := packet(6, 40)
	v4 := packet(4, 20)
	if _, err := unix.Write(w, v6); err != nil {
		t.Fatalf("write IPv6 packet: %v", err)
	}
	if _, err := unix.Write(w, v4); err != nil {
		t.Fatalf("write IPv4 packet: %v", err)
	}

	// The read buffer holds exactly one maximum-size packet, so the first
	// underlying read can only return the IPv6 packet: the filter discards it
	// and the next read yields the buffered IPv4 packet.
	buf := make([]byte, 40)
	n, err := dev.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n != len(v4) || buf[0]>>4 != 4 {
		t.Fatalf("Read = %d bytes starting %#x, want the %d-byte IPv4 packet", n, buf[0], len(v4))
	}

	if err := dev.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := dev.Read(buf); err == nil {
		t.Error("Read after Close succeeded, want os.ErrClosed")
	}
	if _, err := unix.Write(w, v4); err == nil {
		t.Error("write to the pipe after Close succeeded, want EPIPE")
	}
}

func TestAdoptFDRejectsNegativeFD(t *testing.T) {
	if _, err := adoptFD(-1, "tun"); err == nil {
		t.Fatal("adoptFD(-1) succeeded, want an error")
	}
}
