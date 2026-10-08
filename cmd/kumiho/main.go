// Command kumiho is the CLI/TUI client for the Mozilla Firefox account VPN
// entitlement. See PLAN.md for the design.
package main

import (
	"os"

	"github.com/benyamin-git/kumiho/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
