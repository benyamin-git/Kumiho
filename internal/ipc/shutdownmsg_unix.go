//go:build !windows

package ipc

// ShutdownForbiddenMessage explains why a shutdown request was refused.
func ShutdownForbiddenMessage() string {
	return "only root may shut down the daemon"
}
