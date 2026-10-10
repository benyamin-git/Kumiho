//go:build !windows

package cli

import "io"

// runAsServiceIfNeeded is a no-op outside Windows: the process is never
// started by the Windows Service Control Manager, so the caller always runs
// the foreground daemon.
func runAsServiceIfNeeded(stdout, stderr io.Writer) (int, bool) {
	return 0, false
}
