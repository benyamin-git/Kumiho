package daemon

import (
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// Failure bookkeeping for the server selection (PLAN.md §2.4): the persisted
// server stays in use until it fails to connect 3 times in a row, at which
// point the selection is cleared so the next connect auto-picks (REC random,
// else the first country). A successful connect clears the server's count.
//
// Only dial failures with a usable physical network are counted; failures
// while the network is down are not the server's fault (the watchdog parks in
// WAITING_NETWORK instead).

const (
	selectionFailureLimit = 3
	failureCountCap       = 9
	failureMapCap         = 32
)

// noteServerFailure records one failed dial for addr.
func (c *Controller) noteServerFailure(addr string) {
	if addr == "" {
		return
	}
	cleared := false
	var count int
	err := c.store.UpdateState(func(s *settings.State) {
		if s.Failures == nil {
			s.Failures = map[string]int{}
		}
		count = s.Failures[addr] + 1
		if count > failureCountCap {
			count = failureCountCap
		}
		s.Failures[addr] = count
		if len(s.Failures) > failureMapCap {
			for k := range s.Failures {
				if k != addr {
					delete(s.Failures, k)
					if len(s.Failures) <= failureMapCap {
						break
					}
				}
			}
		}
		if count >= selectionFailureLimit && s.SelectedServer == addr {
			s.SelectedServer, s.SelectedCity, s.SelectedLocation = "", "", ""
			cleared = true
		}
	})
	if err != nil {
		c.log.Logf(logging.Warn, "serverlist", "could not persist dial failures: %v", err)
		return
	}
	if cleared {
		c.log.Logf(logging.Warn, "serverlist",
			"%s failed %d times; cleared the selection (the next connect auto-picks)", addr, count)
	}
}

// clearServerFailures forgets the failures recorded for addr after a
// successful connect.
func (c *Controller) clearServerFailures(addr string) {
	if addr == "" {
		return
	}
	err := c.store.UpdateState(func(s *settings.State) {
		delete(s.Failures, addr)
	})
	if err != nil {
		c.log.Logf(logging.Warn, "serverlist", "could not persist dial failures: %v", err)
	}
}
