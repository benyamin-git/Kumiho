package daemon

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

// Watchdog timing (PLAN.md §4.2): 2 s poll; redial with full-jitter 3-60 s;
// rotate the edge every 2 failures. These are construction-time defaults:
// New copies them into the Controller so the connection-lifetime goroutines
// read only controller state (tests rewrite these vars between runs).
var (
	defaultWatchdogInterval = 2 * time.Second
	defaultRedialJitterMin  = 3 * time.Second
	defaultRedialJitterMax  = 60 * time.Second
)

// defaultRouteProbe is the physical-network probe (netlink on Linux).
// Controller.routeProbe exists so tests can simulate outages per controller.
var defaultRouteProbe = hasDefaultRoute

// startWatchdog watches the live session for the whole connection lifetime
// and redials when it dies (PLAN.md §4.2). Redials swap the session under the
// running TUN/SOCKS/DNS through Controller.openStream; an unrecoverable error
// parks the state machine in FATAL. While the physical network is down the
// watchdog parks in WAITING_NETWORK (netlink events wake it), so an outage
// does not burn redial attempts and edge rotations.
func (c *Controller) startWatchdog(stop <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(c.watchdogInterval)
		defer ticker.Stop()
		mon := startNetMonitor(stop)

		failures := 0
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
			case <-mon.C():
				// A link/route change; the checks below decide what to do.
			}

			sess := c.currentSession()
			if sess != nil && sess.IsConnected() {
				failures = 0
				continue
			}

			// Parked while the physical network is down: stay put until a
			// default route exists again.
			if c.stateIs(StateWaitingNetwork) {
				if !c.routeProbe() {
					continue
				}
				c.resumeFromWaitingNetwork()
			}

			if !c.enterReconnecting() {
				// The connection generation is over (teardown or FATAL).
				return
			}

			next, err := c.redialOnce()
			if err != nil {
				select {
				case <-stop:
					return
				default:
				}

				var fatal *fatalError
				if errors.As(err, &fatal) {
					c.fatalStop("redial failed: " + fatal.Error())
					return
				}
				if !c.routeProbe() {
					// The physical network is down: park instead of burning
					// redial attempts, rotations and failure counts.
					c.enterWaitingNetwork()
					continue
				}
				failures++
				c.log.Logf(logging.Warn, "watchdog", "redial failed (failure %d): %v", failures, err)
				c.mu.Lock()
				cand := c.cand
				c.mu.Unlock()
				c.noteServerFailure(cand.Address())
				if failures%2 == 0 {
					c.rotateEdge()
				}
				if !sleepOrStop(stop, fullJitter(c.redialJitterMin, c.redialJitterMax)) {
					return
				}
				continue
			}

			select {
			case <-stop:
				_ = next.Close()
				return
			default:
			}
			if !c.installSession(next) {
				return
			}
			failures = 0
		}
	}()
}

// stateIs reports whether the state machine is in s.
func (c *Controller) stateIs(s State) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state == s
}

// enterWaitingNetwork parks a reconnecting connection when the physical
// network has no default route left.
func (c *Controller) enterWaitingNetwork() {
	c.mu.Lock()
	changed := c.state == StateReconnecting
	if changed {
		c.setStateLocked(StateWaitingNetwork)
	}
	c.mu.Unlock()
	if changed {
		c.log.Logf(logging.Warn, "watchdog", "network is down; waiting for connectivity")
		c.notify()
	}
}

// resumeFromWaitingNetwork returns a parked connection to RECONNECTING.
func (c *Controller) resumeFromWaitingNetwork() {
	c.mu.Lock()
	changed := c.state == StateWaitingNetwork
	if changed {
		c.setStateLocked(StateReconnecting)
	}
	c.mu.Unlock()
	if changed {
		c.log.Logf(logging.Info, "watchdog", "network is back; reconnecting")
		c.notify()
	}
}

// enterReconnecting moves a live connection into RECONNECTING. It returns
// false when another path already ended the generation (teardown, FATAL).
func (c *Controller) enterReconnecting() bool {
	c.mu.Lock()
	changed := false
	switch c.state {
	case StateConnected, StateProxyOnly:
		c.setStateLocked(StateReconnecting)
		changed = true
	case StateReconnecting:
	default:
		c.mu.Unlock()
		return false
	}
	c.mu.Unlock()
	if changed {
		c.log.Logf(logging.Warn, "watchdog", "upstream session lost; reconnecting")
		c.notify()
	}
	return true
}

// redialOnce makes one redial attempt: a fresh Guardian pass (this also
// surfaces quota/token fatals and heals a stale bearer), then one dial to the
// current edge (PLAN.md §2.4).
func (c *Controller) redialOnce() (upstreamSession, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pass, err := c.acquirePass(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.pass = pass
	c.quota = quotaFromPass(pass)
	cand := c.cand
	c.mu.Unlock()

	sess, err := c.dialUpstream(ctx, c.upstreamOptions(ctx, cand, pass))
	if err == nil {
		c.rememberEdgePeer(cand.Host, sess)
	}
	return sess, err
}

// rotateEdge switches to the next alternate edge (same city, then same
// country — PLAN.md §2.4). The alternates come from the cached server list.
func (c *Controller) rotateEdge() {
	c.mu.Lock()
	countries, cur := c.locations, c.cand
	c.mu.Unlock()

	alts := serverlist.AlternateCandidates(countries, cur, 3)
	if len(alts) == 0 {
		c.log.Logf(logging.Warn, "watchdog", "no alternate edge for %s", cur.Address())
		return
	}
	c.mu.Lock()
	c.cand = alts[0]
	c.mu.Unlock()
	c.log.Logf(logging.Info, "watchdog", "rotating edge %s -> %s", cur.Address(), alts[0].Address())
}

// installSession publishes a redialed session and returns to CONNECTED. It
// refuses to publish when the generation already ended (disconnect/FATAL).
func (c *Controller) installSession(sess upstreamSession) bool {
	c.mu.Lock()
	if c.state != StateReconnecting {
		c.mu.Unlock()
		_ = sess.Close()
		return false
	}
	old := c.session
	c.session = sess
	if c.proxyOnly {
		c.setStateLocked(StateProxyOnly)
	} else {
		c.setStateLocked(StateConnected)
	}
	c.authRejected = false
	cand := c.cand
	c.mu.Unlock()

	if old != nil && old != sess {
		_ = old.Close()
	}
	c.clearServerFailures(cand.Address())
	c.log.Logf(logging.Info, "watchdog", "reconnected via %s", cand.Address())
	c.notify()
	return true
}

// fatalStop tears the tunnel down and parks the state machine in FATAL so the
// UI can surface the cause (quota/token — PLAN.md §4.2).
func (c *Controller) fatalStop(msg string) {
	c.stopTunnel("fatal: " + msg)
	c.mu.Lock()
	c.setStateLocked(StateFatal)
	c.mu.Unlock()
	c.log.Logf(logging.Error, "tunnel", "fatal: %s", msg)
	c.notify()
}

// sleepOrStop sleeps d, returning false when the connection generation ended.
func sleepOrStop(stop <-chan struct{}, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-stop:
		return false
	case <-timer.C:
		return true
	}
}

// fullJitter returns a uniform random duration in [min, max).
func fullJitter(min, max time.Duration) time.Duration {
	if max <= min {
		return min
	}
	return min + time.Duration(rand.Int64N(int64(max-min)))
}
