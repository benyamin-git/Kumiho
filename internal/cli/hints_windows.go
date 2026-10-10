//go:build windows

package cli

// daemonStartHint tells the user how to start the daemon on this OS: the
// installed service, or a foreground daemon from an elevated shell.
func daemonStartHint() string {
	return "start it with: Start-Service kumiho  (or run: kumiho daemon as Administrator)"
}

// cleanupSuccessMessage reports the result of `kumiho cleanup`; the Windows
// kill switch is WFP, so there is no nftables wording here.
func cleanupSuccessMessage() string {
	return "Stale kumiho routing, firewall and DNS state were removed (best effort)."
}
