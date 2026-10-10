//go:build !windows

package ipc

// DefaultTransport returns the control transport for the current platform.
func DefaultTransport() Transport { return UnixTransport{} }
