//go:build !windows

package cli

// daemonStartHint tells the user how to start the daemon on this OS. The
// returned string is the v0.2.0 Linux output, byte-identical.
func daemonStartHint() string {
	return "start it with: sudo systemctl start kumiho  (or run: kumiho daemon)"
}

// cleanupSuccessMessage reports the result of `kumiho cleanup`. The returned
// string is the v0.2.0 Linux output, byte-identical.
func cleanupSuccessMessage() string {
	return "Stale kumiho routing, nftables and DNS state were removed (best effort)."
}
