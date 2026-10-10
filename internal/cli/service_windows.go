//go:build windows

package cli

import (
	"context"
	"fmt"
	"io"

	"golang.org/x/sys/windows/svc"

	"github.com/benyamin-git/kumiho/internal/daemon"
)

// runAsServiceIfNeeded runs the daemon under the Windows Service Control
// Manager when the process was started by the SCM, returning its exit code
// with true. Everywhere else it returns (0, false), and the caller runs the
// foreground daemon unchanged. A service has no console, so writes to the
// passed writers are inert; daemon output goes to the log sink
// (internal/daemon).
func runAsServiceIfNeeded(stdout, stderr io.Writer) (int, bool) {
	isService, err := svc.IsWindowsService()
	if err != nil || !isService {
		return 0, false
	}
	h := &serviceHandler{stderr: stderr}
	if err := svc.Run("kumiho", h); err != nil {
		fmt.Fprintf(stderr, "kumiho service: %v\n", err)
		return 1, true
	}
	return h.exitCode, true
}

// serviceHandler implements svc.Handler for the kumiho daemon. Execute is
// called by svc.Run's dispatcher goroutine and returns only after Execute
// has finished, so exitCode is read safely by runAsServiceIfNeeded.
type serviceHandler struct {
	stderr   io.Writer
	exitCode int
}

// Execute reports StartPending, runs daemon.Run until the SCM sends Stop or
// Shutdown (or the daemon exits on its own), then reports Stopped. The SCM
// exit code is 1 when daemon.Run returned an error, 0 otherwise.
func (h *serviceHandler) Execute(args []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepts = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- daemon.Run(ctx) }()

	status <- svc.Status{State: svc.Running, Accepts: accepts}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				status <- svc.Status{State: svc.Running, Accepts: accepts}
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				return h.finish(<-done, status)
			}
		case err := <-done:
			return h.finish(err, status)
		}
	}
}

// finish reports Stopped and converts the daemon's result into the SCM exit
// code, mirroring runDaemon's foreground reporting.
func (h *serviceHandler) finish(err error, status chan<- svc.Status) (bool, uint32) {
	h.exitCode = 0
	if err != nil {
		fmt.Fprintf(h.stderr, "kumiho daemon: %v\n", err)
		h.exitCode = 1
	}
	status <- svc.Status{State: svc.Stopped}
	return false, uint32(h.exitCode)
}
