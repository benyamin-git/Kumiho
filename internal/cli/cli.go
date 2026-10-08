// Package cli implements the kumiho command-line interface.
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/benyamin-git/kumiho/internal/version"
)

const usage = `Kumiho — unofficial Firefox account VPN client

Usage:
  kumiho [command] [flags]

Commands:
  (no command)  Open the TUI (requires a running daemon)
  version       Print version information
  doctor        Run preflight diagnostics (--json for machine output)
  daemon        Run the daemon in the foreground (systemd runs this)
  connect       Connect the tunnel (--proxy-only keeps system routing untouched)
  disconnect    Tear the tunnel down
  cleanup       Remove stale routes/rules/nftables/DNS state (crash recovery)
  login         Sign in to Firefox Accounts through the daemon
  logout        Sign out and clear stored tokens
  locations     List VPN locations (--ping for latency, --json)
  logs          Print daemon logs (-f follows, --level filters)
  status        Show daemon status (--json for machine output)
  help          Show this help
`

// Run executes the CLI with args (typically os.Args[1:]) and returns the
// process exit code.
func Run(args []string) int {
	return run(args, os.Stdout, os.Stderr)
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return runTUI(stdout, stderr)
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "version", "--version", "-v":
		return runVersion(stdout)
	case "doctor":
		return runDoctor(rest, stdout, stderr)
	case "daemon":
		return runDaemon(rest, stdout, stderr)
	case "login":
		return runLogin(rest, stdout, stderr)
	case "logout":
		return runLogout(rest, stdout, stderr)
	case "connect":
		return runConnect(rest, stdout, stderr)
	case "disconnect":
		return runDisconnect(rest, stdout, stderr)
	case "cleanup":
		return runCleanup(rest, stdout, stderr)
	case "locations":
		return runLocations(rest, stdout, stderr)
	case "logs":
		return runLogs(rest, stdout, stderr)
	case "status":
		return runStatus(rest, stdout, stderr)
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "kumiho: unknown command %q\n\n", cmd)
		fmt.Fprint(stderr, usage)
		return 2
	}
}

func runVersion(stdout io.Writer) int {
	fmt.Fprintf(stdout, "kumiho %s\n", version.String())
	return 0
}
