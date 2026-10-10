//go:build windows

// Package windows is the Windows provider stub (D13). It is compile-only for
// now: the machine-wide placeholder paths and the not-implemented fallback
// keep the daemon buildable until the Windows cycle adds Wintun, the route
// table and the named-pipe control plane.
package windows
