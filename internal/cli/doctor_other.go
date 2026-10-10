//go:build !windows

package cli

// fullTunnelPlatform reports the full-tunnel provider on this OS and the
// detail the platform check prints for it. Linux is the only full-tunnel
// provider on non-Windows builds, and the returned detail is byte-identical
// to the v0.2.0 Linux output.
func fullTunnelPlatform(provider string) (string, bool) {
	if provider == "linux" {
		return provider, true
	}
	return "", false
}

// platformChecks returns the OS-specific doctor checks appended to the common
// list. Non-Windows builds have none, so Linux output is unchanged.
func platformChecks() []check { return nil }
