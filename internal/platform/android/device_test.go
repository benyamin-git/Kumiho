package android

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// packet builds a fake IP packet whose first byte carries the version nibble.
func packet(version byte, n int) []byte {
	p := make([]byte, n)
	p[0] = version << 4
	return p
}

// scriptedDevice is a read/write/close recorder returning canned reads.
type scriptedDevice struct {
	reads  [][]byte
	writes [][]byte
	closed int
}

func (s *scriptedDevice) Read(p []byte) (int, error) {
	if len(s.reads) == 0 {
		return 0, io.EOF
	}
	pkt := s.reads[0]
	s.reads = s.reads[1:]
	return copy(p, pkt), nil
}

func (s *scriptedDevice) Write(p []byte) (int, error) {
	s.writes = append(s.writes, append([]byte(nil), p...))
	return len(p), nil
}

func (s *scriptedDevice) Close() error {
	s.closed++
	return nil
}

// The filter must hand back only IPv4 packets, reading past any IPv6 packet.
func TestIPv6FilterDropsIPv6(t *testing.T) {
	v4a, v4b := packet(4, 20), packet(4, 60)
	src := &scriptedDevice{reads: [][]byte{packet(6, 40), v4a, packet(6, 40), v4b}}
	f := newIPv6Filter(src, "tun")

	buf := make([]byte, 2048)
	n, err := f.Read(buf)
	if err != nil {
		t.Fatalf("first Read: %v", err)
	}
	if n != len(v4a) || buf[0] != 0x40 {
		t.Fatalf("first Read = %d bytes starting %#x, want the %d-byte IPv4 packet", n, buf[0], len(v4a))
	}

	n, err = f.Read(buf)
	if err != nil {
		t.Fatalf("second Read: %v", err)
	}
	if n != len(v4b) || buf[0] != 0x40 {
		t.Fatalf("second Read = %d bytes starting %#x, want the %d-byte IPv4 packet", n, buf[0], len(v4b))
	}

	if _, err := f.Read(buf); !errors.Is(err, io.EOF) {
		t.Fatalf("Read after exhaustion = %v, want io.EOF", err)
	}
}

func TestIPv6FilterDelegatesWriteCloseName(t *testing.T) {
	src := &scriptedDevice{}
	f := newIPv6Filter(src, "tun0")

	if got := f.Name(); got != "tun0" {
		t.Errorf("Name() = %q, want %q", got, "tun0")
	}

	payload := []byte{0x45, 0x00, 0x00, 0x14}
	n, err := f.Write(payload)
	if err != nil || n != len(payload) {
		t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, len(payload))
	}
	if len(src.writes) != 1 || !bytes.Equal(src.writes[0], payload) {
		t.Errorf("writes = %v, want one copy of %v", src.writes, payload)
	}

	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if src.closed != 1 {
		t.Errorf("closed %d times, want 1", src.closed)
	}
}

// errDevice returns canned (data, err) reads.
type errDevice struct {
	data []byte
	err  error
}

func (e errDevice) Read(p []byte) (int, error) {
	if e.data == nil {
		return 0, e.err
	}
	return copy(p, e.data), e.err
}

func (errDevice) Write(p []byte) (int, error) { return len(p), nil }
func (errDevice) Close() error                { return nil }

func TestIPv6FilterPropagatesReadErrors(t *testing.T) {
	boom := errors.New("boom")

	if _, err := newIPv6Filter(errDevice{err: boom}, "tun").Read(make([]byte, 64)); !errors.Is(err, boom) {
		t.Fatalf("Read error = %v, want %v", err, boom)
	}

	// An IPv4 packet that arrives together with an error is returned as-is.
	v4 := packet(4, 20)
	buf := make([]byte, 64)
	n, err := newIPv6Filter(errDevice{data: v4, err: boom}, "tun").Read(buf)
	if n != len(v4) || !errors.Is(err, boom) {
		t.Fatalf("Read = (%d, %v), want (%d, %v)", n, err, len(v4), boom)
	}
}
