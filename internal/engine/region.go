package engine

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/relay"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

// Region probe timings: a bounded per-host budget and a whole-run ceiling so
// a captive portal or a black-holed host cannot stall the first run.
var (
	regionProbeTimeout = 5 * time.Second
	regionTotalTimeout = 20 * time.Second
)

// regionHosts are the five engine control-plane hosts plus the browser page.
// The browser page is not proxied by the relay; it tells the guide whether
// browser sign-in needs another route.
var regionHosts = []string{
	"api.accounts.firefox.com",
	"oauth.accounts.firefox.com",
	"profile.accounts.firefox.com",
	"vpn.mozilla.org",
	"firefox.settings.services.mozilla.com",
	"accounts.firefox.com",
}

const regionBrowserHost = "accounts.firefox.com"

// RegionCheck probes every control-plane host without credentials and
// without writing state. It uses the direct transport: the relay must not
// make a blocked region look reachable.
func (c *Controller) RegionCheck(ctx context.Context) api.RegionCheckResult {
	ctx, cancel := context.WithTimeout(ctx, regionTotalTimeout)
	defer cancel()

	hosts := make([]api.RegionHostStatus, len(regionHosts))
	client := c.directClient()
	var wg sync.WaitGroup
	for i, host := range regionHosts {
		wg.Add(1)
		go func(i int, host string) {
			defer wg.Done()
			hosts[i] = probeRegionHost(ctx, client, host)
		}(i, host)
	}
	wg.Wait()

	result := api.RegionCheckResult{Hosts: hosts}
	for _, h := range hosts {
		switch h.Status {
		case api.RegionBlocked:
			result.Blocked = append(result.Blocked, h.Host)
			if h.Host == regionBrowserHost {
				result.BrowserBlocked = true
			}
		}
	}
	cfg := c.relayConfig()
	result.RelayConfigured = strings.TrimSpace(cfg.URL) != "" && strings.TrimSpace(cfg.Token) != ""
	return result
}

// probeRegionHost classifies one unauthenticated GET: 451 is blocked, any
// other HTTP response is reachable, and a transport failure is unknown.
func probeRegionHost(ctx context.Context, client *http.Client, host string) api.RegionHostStatus {
	pctx, cancel := context.WithTimeout(ctx, regionProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(pctx, http.MethodGet, "https://"+host+"/", nil)
	if err != nil {
		return api.RegionHostStatus{Host: host, Status: api.RegionUnknown, Detail: err.Error()}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
	resp, err := client.Do(req)
	if err != nil {
		return api.RegionHostStatus{Host: host, Status: api.RegionUnknown, Detail: "could not connect"}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnavailableForLegalReasons {
		return api.RegionHostStatus{Host: host, Status: api.RegionBlocked, Detail: "HTTP 451"}
	}
	return api.RegionHostStatus{Host: host, Status: api.RegionReachable, Detail: fmt.Sprintf("HTTP %d", resp.StatusCode)}
}

// directClient returns a client over the same bypass transport without the
// relay wrapper. Region probes must measure the user's network, not a relay
// that hides the block.
func (c *Controller) directClient() *http.Client {
	var rt http.RoundTripper
	if c.httpClient != nil {
		rt = c.httpClient.Transport
	}
	if wrapper, ok := rt.(*relay.Transport); ok {
		rt = wrapper.Unwrap()
	}
	return &http.Client{Transport: rt, Timeout: regionTotalTimeout}
}

// Prepare fetches everything a blocked first run needs in one handoff: a
// fresh server list, the account profile, then the Guardian pass last. Only
// the pass is persisted; the token never crosses IPC.
func (c *Controller) Prepare(ctx context.Context) (api.PrepareResult, error) {
	c.mu.Lock()
	connected := c.tunnelActiveLocked()
	tokens := c.tokens
	email := c.email
	c.mu.Unlock()
	if connected {
		return api.PrepareResult{}, &api.RemoteError{Code: api.CodeBadRequest, Message: "Disconnect first, then prepare."}
	}
	if tokens == nil {
		return api.PrepareResult{}, &api.RemoteError{Code: api.CodeNotAuthenticated, Message: "Sign in first, then prepare."}
	}
	if err := c.EnsureFreshToken(ctx); err != nil {
		return api.PrepareResult{}, err
	}

	countries, err := c.Locations(ctx, true)
	if err != nil {
		return api.PrepareResult{}, err
	}

	c.mu.Lock()
	tokens = c.tokens
	c.mu.Unlock()
	if tokens == nil {
		return api.PrepareResult{}, &api.RemoteError{Code: api.CodeNotAuthenticated, Message: "Sign in first, then prepare."}
	}
	profile, err := c.fxa.Profile(ctx, tokens.AccessToken)
	if err != nil {
		if re := c.regionBlocked(err); re != nil {
			return api.PrepareResult{}, re
		}
		return api.PrepareResult{}, fmt.Errorf("refresh the Firefox Account profile: %w", err)
	}
	c.mu.Lock()
	c.profile = profile
	email = c.email
	c.mu.Unlock()

	c.relayUsed.Store(false)
	pass, err := c.fetchPass(ctx)
	if err != nil {
		if re := c.regionBlocked(err); re != nil {
			return api.PrepareResult{}, re
		}
		return api.PrepareResult{}, fmt.Errorf("fetch the Guardian pass: %w", err)
	}
	c.mu.Lock()
	c.pass = pass
	c.passPrepared = true
	account := c.email
	c.quota = quotaFromPass(pass)
	c.mu.Unlock()
	if err := c.savePassFile(pass, account); err != nil {
		return api.PrepareResult{}, fmt.Errorf("save the prepared pass: %w", err)
	}
	c.log.Logf(logging.Info, "guardian", "prepared pass for %s (expires %s)", email,
		time.Unix(pass.ExpiresAt, 0).Format(time.RFC3339))
	c.notify()

	return api.PrepareResult{
		Email:               email,
		ExpiresAt:           pass.ExpiresAt,
		Quota:               quotaFromPass(pass),
		Locations:           len(countries),
		Servers:             len(serverlist.Candidates(countries, "", "")),
		ServerListRefreshed: true,
		ProfileRefreshed:    true,
		RelayUsed:           c.relayUsed.Load(),
	}, nil
}
