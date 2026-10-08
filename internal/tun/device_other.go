//go:build !linux

package tun

import (
	"errors"
	"io"
)

// Device is a stub on non-Linux hosts; the full tunnel exists only on Linux.
type Device struct{}

// Open always fails outside Linux.
func Open(string, int) (*Device, error) {
	return nil, errors.New("tun: only supported on Linux")
}

// Read always fails outside Linux.
func (d *Device) Read([]byte) (int, error) {
	return 0, errors.New("tun: only supported on Linux")
}

// Write always fails outside Linux.
func (d *Device) Write([]byte) (int, error) {
	return 0, errors.New("tun: only supported on Linux")
}

// Name returns an empty name.
func (d *Device) Name() string { return "" }

// MTU returns 0.
func (d *Device) MTU() int { return 0 }

// Close is a no-op.
func (d *Device) Close() error { return nil }

var _ io.ReadWriteCloser = (*Device)(nil)
