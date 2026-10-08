package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/logging"
)

func runLogs(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	follow := fs.Bool("f", false, "follow the live log stream")
	level := fs.String("level", "info", "minimum level: debug|info|warn|error")
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: kumiho logs [-f] [--level info|warn|error]\n\nPrints retained daemon logs; -f keeps streaming them.\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if _, err := logging.ParseLevel(*level); err != nil {
		fmt.Fprintf(stderr, "kumiho logs: %v\n", err)
		return 2
	}

	client, code := dialDaemon(stderr)
	if client == nil {
		return code
	}
	defer client.Close()

	ctx, cancel := callCtx()
	defer cancel()
	env, err := client.Call(ctx, ipc.TypeLogsSnapshot, ipc.LogsSnapshotPayload{Level: *level})
	if err != nil {
		fmt.Fprintf(stderr, "kumiho logs: %v\n", err)
		return 1
	}
	var snap ipc.LogSnapshot
	if err := env.DecodePayload(&snap); err != nil {
		fmt.Fprintf(stderr, "kumiho logs: %v\n", err)
		return 1
	}
	for _, e := range snap.Entries {
		fmt.Fprintln(stdout, e.Line())
	}
	if !*follow {
		return 0
	}

	subCtx, cancelSub := context.WithTimeout(context.Background(), 10*time.Second)
	_, err = client.Call(subCtx, ipc.TypeLogsSubscribe, ipc.LogsSubscribePayload{Level: *level})
	cancelSub()
	if err != nil {
		fmt.Fprintf(stderr, "kumiho logs: %v\n", err)
		return 1
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	for {
		select {
		case <-sigCtx.Done():
			return 0
		case env, ok := <-client.Events():
			if !ok {
				return 0
			}
			if env.Type != ipc.TypeLog {
				continue
			}
			var e logging.Entry
			if err := env.DecodePayload(&e); err != nil {
				continue
			}
			fmt.Fprintln(stdout, e.Line())
		}
	}
}
