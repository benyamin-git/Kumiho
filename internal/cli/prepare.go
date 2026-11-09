package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
)

func runPrepare(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("prepare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit machine-readable JSON")
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: kumiho prepare [--json]\n\nFetches the server list, profile and Guardian pass through the current route.\nTurn on your other VPN first when this region blocks Mozilla's servers.\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "kumiho prepare: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	client, code := dialDaemon(stderr)
	if client == nil {
		return code
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return prepareWithClient(ctx, client, *asJSON, stdout, stderr)
}

func prepareWithClient(ctx context.Context, client *ipc.Client, asJSON bool, stdout, stderr io.Writer) int {
	env, err := client.Call(ctx, ipc.TypePrepare, nil)
	if err != nil {
		fmt.Fprintf(stderr, "kumiho prepare: %v\n", err)
		return 1
	}
	var res api.PrepareResult
	if err := env.DecodePayload(&res); err != nil {
		fmt.Fprintf(stderr, "kumiho prepare: %v\n", err)
		return 1
	}
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			fmt.Fprintf(stderr, "kumiho prepare: %v\n", err)
			return 1
		}
		return 0
	}

	expires := time.Unix(res.ExpiresAt, 0)
	remaining := time.Until(expires).Round(time.Second)
	fmt.Fprintf(stdout, "Prepared. Pass valid until %s (%s). Servers: %d locations, %d servers.\n",
		expires.Format("15:04:05"), humanDuration(remaining), res.Locations, res.Servers)
	fmt.Fprintln(stdout, "Turn off the other VPN, then connect: kumiho connect")
	if res.RelayUsed {
		fmt.Fprintln(stdout, "The pass was fetched through the relay.")
	}
	return 0
}

func humanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	m := int(d / time.Minute)
	s := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm %ds", m, s)
}
