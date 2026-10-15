//go:build windows

package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

const guiExeName = "kumiho-gui.exe"

// runGUI starts the Windows desktop app, kumiho-gui.exe, and returns as soon
// as it is running; it never waits for the app, which outlives the calling
// terminal. The app's own single-instance guard focuses an existing window
// when one is already open.
//
// The executable is looked up beside the running CLI binary first (the
// release zip and installer layouts), then in the installer's
// %ProgramFiles%\Kumiho.
func runGUI(stderr io.Writer) int {
	dirs, err := guiSearchDirs()
	if err != nil {
		fmt.Fprintf(stderr, "kumiho gui: cannot locate the running executable: %v\n", err)
		return 1
	}
	for _, dir := range dirs {
		gui := filepath.Join(dir, guiExeName)
		if info, err := os.Stat(gui); err != nil || info.IsDir() {
			continue
		}
		if err := startGUI(gui); err != nil {
			fmt.Fprintf(stderr, "kumiho gui: cannot start the desktop app: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stderr, "kumiho gui: %s is missing from %s; run install.cmd to install the desktop app\n",
		guiExeName, strings.Join(dirs, " and "))
	return 1
}

// guiSearchDirs returns the directories searched for kumiho-gui.exe, in
// order: the directory of the running CLI binary, then %ProgramFiles%\Kumiho
// (where the installer places both executables).
func guiSearchDirs() ([]string, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	dirs := []string{filepath.Dir(exe)}
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		dirs = append(dirs, filepath.Join(pf, "Kumiho"))
	}
	return dirs, nil
}

// startGUI launches gui detached (no console, outlives this process) and
// returns without waiting for it to exit.
func startGUI(gui string) error {
	cmd := exec.Command(gui)
	cmd.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS}
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	return nil
}
