package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
)

func runRelay(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		printRelayUsage(stdout)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	if args[0] != "test" {
		fmt.Fprintf(stderr, "kumiho relay: unknown command %q\n", args[0])
		printRelayUsage(stderr)
		return 2
	}
	if len(args) != 1 {
		fmt.Fprintf(stderr, "kumiho relay test: unexpected argument %q\n", args[1])
		return 2
	}

	client, code := dialDaemon(stderr)
	if client == nil {
		return code
	}
	defer client.Close()
	ctx, cancel := callCtx()
	defer cancel()
	return relayTestWithClient(ctx, client, stdout, stderr)
}

func relayTestWithClient(ctx context.Context, client *ipc.Client, stdout, stderr io.Writer) int {
	env, err := client.Call(ctx, ipc.TypeRelayTest, nil)
	if err != nil {
		fmt.Fprintf(stderr, "kumiho relay test: %v\n", err)
		return 1
	}
	var res api.RelayTestResult
	if err := env.DecodePayload(&res); err != nil {
		fmt.Fprintf(stderr, "kumiho relay test: %v\n", err)
		return 1
	}
	return printRelayResult(stdout, res)
}

func printRelayResult(stdout io.Writer, res api.RelayTestResult) int {
	if res.OK {
		if res.LatencyMS != nil {
			fmt.Fprintf(stdout, "Relay OK — %.0f ms\n", *res.LatencyMS)
		} else {
			fmt.Fprintln(stdout, "Relay OK")
		}
		return 0
	}
	if res.Status == 401 || res.Status == 403 {
		fmt.Fprintln(stdout, "Relay rejected the token")
		return 1
	}
	if res.Error == "relay_url is not set" {
		fmt.Fprintln(stdout, res.Error)
		return 1
	}
	detail := res.Error
	if detail == "" {
		detail = fmt.Sprintf("HTTP %d", res.Status)
	}
	fmt.Fprintf(stdout, "Relay unreachable: %s\n", detail)
	return 1
}

func printRelayUsage(out io.Writer) {
	fmt.Fprint(out, "Usage: kumiho relay test\n\nTests the configured control-plane relay.\n")
}
