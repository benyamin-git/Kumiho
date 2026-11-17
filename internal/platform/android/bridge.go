package android

import "fmt"

// Bridge is the Kotlin side of the VpnService handshake. The gomobile
// DeviceBridge interface satisfies it. A method may panic: gomobile turns a
// Kotlin exception into a Go panic, so every call goes through the helpers
// below instead of crossing the JNI boundary bare.
type Bridge interface {
	AcquireFD(mtu int) (int, error)
	NetworkAvailable() bool
}

// acquireFD converts a Kotlin exception from the bridge into an error. Kotlin
// throws when VpnService consent was revoked, another VPN took over, or
// configure() has not run; a panic here would otherwise take down the connect
// path (or the watchdog) and leave the engine mid-session.
func acquireFD(b Bridge, mtu int) (fd int, err error) {
	defer func() {
		if r := recover(); r != nil {
			fd = -1
			err = fmt.Errorf("the bridge panicked acquiring the descriptor: %v", r)
		}
	}()
	return b.AcquireFD(mtu)
}

// networkAvailable converts a bridge panic into "available" (fail open). The
// watchdog must not crash on a dead bridge, and its probes still detect a
// real outage; this callback only guards against a false teardown.
func networkAvailable(b Bridge) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = true
		}
	}()
	return b.NetworkAvailable()
}
