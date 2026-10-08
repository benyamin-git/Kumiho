//go:build !linux

package daemon

// chownControlSocket is a no-op outside Linux.
func chownControlSocket(string) {}
