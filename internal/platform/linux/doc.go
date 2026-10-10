// Package linux implements platform.Provider with the v0.1.0 Linux mechanisms:
// the /dev/net/tun device, nftables/policy-routing configuration, netlink link
// monitoring and the sudo control-socket chown. The engine only sees the
// platform interfaces; this package holds the OS specifics.
//
// This untagged doc file keeps the package (and ./...) buildable on non-Linux
// GOOS; the implementation files carry the linux && !android build tag.
package linux
