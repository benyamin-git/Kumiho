//go:build !windows

package cli

import "os/exec"

// openBrowserURL opens a browser through the desktop opener. A missing opener
// is not an error: the URL was already printed for the user.
func openBrowserURL(url string) error {
	return exec.Command("xdg-open", url).Start()
}
