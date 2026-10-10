package engine

import (
	"context"
	"errors"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/logging"
)

// autoconnectDelays are the retry backoffs after a failed startup connect.
// The list bounds how long the daemon keeps trying; var so tests can compress
// the schedule.
var autoconnectDelays = []time.Duration{
	5 * time.Second, 10 * time.Second, 30 * time.Second, 30 * time.Second, time.Minute,
}

// Autoconnect brings the tunnel up on daemon start when the setting is on and
// a session was restored (PLAN.md §4.3, M5 "reboot-safe"). Failed attempts
// retry with backoff because boot can race DHCP/VPN bring-up; user-actionable
// errors (bad request, quota, sign-in) stop immediately. Once a session
// exists the watchdog owns recovery.
func (c *Controller) Autoconnect(ctx context.Context) {
	if !c.store.EffectiveAutoconnect() {
		c.log.Logf(logging.Info, "daemon", "autoconnect is off")
		return
	}
	for attempt := 0; ; attempt++ {
		switch c.Snapshot().State {
		case string(StateIdle):
			// Ours to try.
		case string(StateUnauthenticated), string(StateFatal):
			// Connect reports the reason (sign in / fatal) next; let it.
		default:
			// A connect is already in progress or live; somebody else owns it.
			return
		}

		err := c.Connect(ctx, api.ConnectPayload{})
		if err == nil {
			c.log.Logf(logging.Info, "daemon", "autoconnect: tunnel is up")
			return
		}
		if ctx.Err() != nil {
			return
		}

		var rerr *api.RemoteError
		var ferr *fatalError
		if errors.As(err, &rerr) || errors.As(err, &ferr) || attempt >= len(autoconnectDelays) {
			c.log.Logf(logging.Warn, "daemon", "autoconnect failed: %v", err)
			return
		}
		delay := autoconnectDelays[attempt]
		c.log.Logf(logging.Warn, "daemon", "autoconnect failed: %v (retrying in %s)", err, delay)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
	}
}
