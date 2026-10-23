//go:build windows

package cli

import "os/exec"

// openBrowserURL opens a browser through the Windows shell. A missing opener
// is not an error: the URL was already printed for the user.
func openBrowserURL(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
