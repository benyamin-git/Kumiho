package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/benyamin-git/kumiho/internal/daemon"
	"github.com/benyamin-git/kumiho/internal/settings"
)

func runDaemon(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: kumiho daemon\n\nRuns the daemon in the foreground until SIGTERM/SIGINT.\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	_ = stdout

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := daemon.Run(ctx, settings.DefaultPaths()); err != nil {
		fmt.Fprintf(stderr, "kumiho daemon: %v\n", err)
		return 1
	}
	return 0
}
