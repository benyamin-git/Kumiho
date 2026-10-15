//go:build !windows

package cli

import (
	"fmt"
	"io"
)

// runGUI reports that the desktop app only ships for Windows and points the
// user at the TUI, which works everywhere this binary does.
func runGUI(stderr io.Writer) int {
	fmt.Fprintln(stderr, "the desktop app is Windows-only; run `kumiho` for the TUI")
	return 2
}
