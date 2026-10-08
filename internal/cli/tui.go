package cli

import (
	"fmt"
	"io"

	"github.com/benyamin-git/kumiho/internal/tui"
)

func runTUI(stdout, stderr io.Writer) int {
	_ = stdout
	client, code := dialDaemon(stderr)
	if client == nil {
		return code
	}
	defer client.Close()

	if err := tui.Run(client); err != nil {
		fmt.Fprintf(stderr, "kumiho: %v\n", err)
		return 1
	}
	return 0
}
