//go:build linux

package daemon

import (
	"os"
	"strconv"
)

// chownControlSocket hands the control socket to the sudo-invoking user.
// This is a development convenience (running the daemon under sudo gives
// nft/ip the privileges they need): the operator's CLI then works without
// sudo. The M5 systemd unit instead owns its RuntimeDirectory via
// RuntimeDirectory=/User=kumiho and needs no chown.
func chownControlSocket(path string) {
	if os.Geteuid() != 0 {
		return
	}
	uidStr, gidStr := os.Getenv("SUDO_UID"), os.Getenv("SUDO_GID")
	if uidStr == "" || gidStr == "" {
		return
	}
	uid, err1 := strconv.Atoi(uidStr)
	gid, err2 := strconv.Atoi(gidStr)
	if err1 != nil || err2 != nil {
		return
	}
	_ = os.Chown(path, uid, gid) // best effort
}
