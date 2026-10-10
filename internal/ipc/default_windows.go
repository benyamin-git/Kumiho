//go:build windows

package ipc

// DefaultTransport returns the control transport for the current platform.
// For now it is the Unix transport; T13 replaces it with WindowsTransport
// once the named-pipe implementation lands.
func DefaultTransport() Transport { return UnixTransport{} }
