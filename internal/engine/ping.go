package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/guardian"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

const (
	pingProbeConcurrency = 4
	pingProbeTimeout     = 10 * time.Second
	pingCacheTTL         = 30 * time.Minute
	pingProbeHost        = "vpn.mozilla.org"
	pingProbePort        = 443
)

// pingSample is one cached real-delay measurement. OK is false when the
// candidate was measured but did not answer, which is distinct from a
// candidate that has never been measured.
type pingSample struct {
	RTT        time.Duration
	OK         bool
	MeasuredAt time.Time
}

// PingHosts measures the visible candidates named by hosts through temporary
// edge sessions. emit is called once per resolved host as it completes; the
// returned PingDone always carries the run totals, and Error for a run-level
// preflight failure.
func (c *Controller) PingHosts(ctx context.Context, hosts []string, emit func(api.PingResult)) api.PingDone {
	countries, err := c.Locations(ctx, false)
	if err != nil {
		return api.PingDone{Error: err.Error()}
	}
	c.prunePings(countries)

	visible := make(map[string]serverlist.Candidate)
	for _, cand := range serverlist.Candidates(countries, "", "") {
		visible[strings.ToLower(cand.Address())] = cand
	}
	seen := make(map[string]bool, len(hosts))
	candidates := make([]serverlist.Candidate, 0, len(hosts))
	for _, host := range hosts {
		key := strings.ToLower(strings.TrimSpace(host))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		cand, ok := visible[key]
		if !ok {
			continue
		}
		candidates = append(candidates, cand)
	}
	if len(candidates) == 0 {
		return api.PingDone{Error: "no visible locations to ping"}
	}

	if c.currentTokens() == nil {
		return api.PingDone{Error: "Ping all needs a signed-in account"}
	}
	c.mu.Lock()
	pass := c.pass
	c.mu.Unlock()
	if pass == nil || (pass.ExpiresAt != 0 &&
		!time.Unix(pass.ExpiresAt, 0).After(time.Now().Add(30*time.Second))) {
		pass, err = c.acquirePass(ctx)
		if err != nil {
			return api.PingDone{Error: err.Error()}
		}
	}

	concurrency := c.pingConcurrency
	if concurrency <= 0 {
		concurrency = pingProbeConcurrency
	}
	if concurrency > len(candidates) {
		concurrency = len(candidates)
	}
	timeout := c.pingTimeout
	if timeout <= 0 {
		timeout = pingProbeTimeout
	}

	var answered atomic.Int64
	var emitMu sync.Mutex
	emitResult := func(result api.PingResult) {
		if emit == nil {
			return
		}
		// A client emitter should never see two callbacks at once, even
		// though probes run concurrently.
		emitMu.Lock()
		defer emitMu.Unlock()
		emit(result)
	}

	jobs := make(chan serverlist.Candidate)
	var workers sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for cand := range jobs {
				c.probeCandidate(ctx, cand, pass, timeout, emitResult, &answered)
			}
		}()
	}
	for _, cand := range candidates {
		jobs <- cand
	}
	close(jobs)
	workers.Wait()

	return api.PingDone{Total: len(candidates), Answered: int(answered.Load())}
}

// probeCandidate measures one candidate. The dial is deliberately outside the
// RTT window; only the CONNECT exchange counts (D19).
func (c *Controller) probeCandidate(ctx context.Context, cand serverlist.Candidate, pass *guardian.Pass, timeout time.Duration, emit func(api.PingResult), answered *atomic.Int64) {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result := api.PingResult{Host: cand.Address()}
	var rtt time.Duration
	var err error

	opts, optErr := c.upstreamOptions(probeCtx, cand, c.passFor(pass))
	if optErr != nil {
		err = optErr
	} else {
		var sess upstreamSession
		sess, err = c.dialUpstream(probeCtx, opts)
		if err == nil && isNilSession(sess) {
			err = errors.New("upstream: dial returned a nil session")
		}
		if err == nil {
			start := time.Now()
			stream, streamErr := sess.OpenStream(probeCtx, pingProbeHost, pingProbePort)
			rtt = time.Since(start)
			if stream != nil {
				_ = stream.Close()
			}
			err = streamErr
		}
		if !isNilSession(sess) {
			_ = sess.Close()
		}
	}

	if err == nil {
		ms := float64(rtt) / float64(time.Millisecond)
		result.RTTMS = &ms
		answered.Add(1)
	}
	c.recordPing(cand, rtt, err == nil)
	emit(result)
}

func (c *Controller) pingTTL() time.Duration {
	if c.pingCacheTTL > 0 {
		return c.pingCacheTTL
	}
	return pingCacheTTL
}

func (c *Controller) recordPing(cand serverlist.Candidate, rtt time.Duration, ok bool) {
	if !ok {
		rtt = 0
	}
	c.pingMu.Lock()
	if c.pings == nil {
		c.pings = make(map[string]pingSample)
	}
	c.pings[strings.ToLower(cand.Address())] = pingSample{
		RTT:        rtt,
		OK:         ok,
		MeasuredAt: time.Now(),
	}
	c.pingMu.Unlock()
}

// prunePings drops stale samples and samples for candidates that are no longer
// visible under the current allow_locked/filter view.
func (c *Controller) prunePings(countries []serverlist.Country) {
	visible := make(map[string]bool)
	for _, cand := range serverlist.Candidates(countries, "", "") {
		visible[strings.ToLower(cand.Address())] = true
	}
	cutoff := time.Now().Add(-c.pingTTL())
	c.pingMu.Lock()
	defer c.pingMu.Unlock()
	for key, sample := range c.pings {
		if sample.MeasuredAt.Before(cutoff) || !visible[key] {
			delete(c.pings, key)
		}
	}
}

// cachedLatency returns a copy of the fresh samples keyed by the canonical
// visible address. It is the C3 wire accessor's engine-side source.
func (c *Controller) cachedLatency(countries []serverlist.Country) map[string]pingSample {
	c.prunePings(countries)
	visible := make(map[string]serverlist.Candidate)
	for _, cand := range serverlist.Candidates(countries, "", "") {
		visible[strings.ToLower(cand.Address())] = cand
	}
	c.pingMu.Lock()
	defer c.pingMu.Unlock()
	out := make(map[string]pingSample, len(c.pings))
	for key, sample := range c.pings {
		if cand, ok := visible[key]; ok {
			out[cand.Address()] = sample
		}
	}
	return out
}

// CachedLatency exposes the fresh samples on the wire, keyed by the canonical
// visible address. A measured failure carries a nil RTT; a server that was
// never measured has no entry.
func (c *Controller) CachedLatency(countries []serverlist.Country) map[string]api.CachedPing {
	samples := c.cachedLatency(countries)
	out := make(map[string]api.CachedPing, len(samples))
	for addr, sample := range samples {
		var rtt *float64
		if sample.OK {
			ms := float64(sample.RTT) / float64(time.Millisecond)
			rtt = &ms
		}
		out[addr] = api.CachedPing{RTTMS: rtt, MeasuredAt: sample.MeasuredAt}
	}
	return out
}

// fastestCandidate picks the lowest fresh finite RTT among visible servers.
// Ties break by country, city and host so the choice is deterministic.
func (c *Controller) fastestCandidate(countries []serverlist.Country) (serverlist.Candidate, error) {
	samples := c.cachedLatency(countries)
	var best serverlist.Candidate
	var bestRTT time.Duration
	found := false
	for _, cand := range serverlist.Candidates(countries, "", "") {
		sample, ok := samples[cand.Address()]
		if !ok || !sample.OK {
			continue
		}
		if !found || sample.RTT < bestRTT ||
			(sample.RTT == bestRTT && candidateTieLess(cand, best)) {
			best = cand
			bestRTT = sample.RTT
			found = true
		}
	}
	if !found {
		return serverlist.Candidate{}, &api.RemoteError{
			Code:    api.CodeBadRequest,
			Message: "Fastest needs a completed Ping all",
		}
	}
	return best, nil
}

func candidateTieLess(a, b serverlist.Candidate) bool {
	if ac, bc := strings.ToLower(a.CountryCode), strings.ToLower(b.CountryCode); ac != bc {
		return ac < bc
	}
	if ac, bc := strings.ToLower(a.CityCode), strings.ToLower(b.CityCode); ac != bc {
		return ac < bc
	}
	if ah, bh := strings.ToLower(a.Host), strings.ToLower(b.Host); ah != bh {
		return ah < bh
	}
	return a.Port < b.Port
}
