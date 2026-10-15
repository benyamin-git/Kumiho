//go:build windows

// Command kumiho-gui is the Kumiho desktop shell: a Windows-only native host
// that owns the window, the WebView2 control, the in-process asset server,
// the process composition root (control client + GUI controller + bridge)
// and the host log. It is built with the GUI subsystem and, for --version
// and --help, attaches to the invoking console; see internal/gui's
// host_windows.go.
package main

import (
	"os"

	"github.com/benyamin-git/kumiho/internal/gui"
)

func main() {
	os.Exit(gui.RunHost(os.Args[1:]))
}
