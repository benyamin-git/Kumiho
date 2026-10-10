package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/serverlist"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// locationsCacheTTL bounds how stale the in-memory server list may be before
// a normal (non-refresh) request re-fetches it.
const locationsCacheTTL = 30 * time.Minute

// Locations returns the server list, from cache when fresh. A failed refresh
// falls back to the cached list when one exists.
func (c *Controller) Locations(ctx context.Context, refresh bool) ([]serverlist.Country, error) {
	c.mu.Lock()
	cached := c.locations
	cachedAt := c.locationsAt
	c.mu.Unlock()

	if !refresh && cached != nil && time.Since(cachedAt) < locationsCacheTTL {
		return cached, nil
	}

	countries, err := serverlist.FetchFrom(ctx, c.httpClient, c.serverListURL)
	if err != nil {
		if cached != nil {
			c.log.Logf(logging.Warn, "serverlist", "refresh failed, keeping cached list: %v", err)
			return cached, nil
		}
		return nil, err
	}
	serverlist.SortCountries(countries)

	c.mu.Lock()
	c.locations = countries
	c.locationsAt = time.Now()
	c.mu.Unlock()

	c.log.Logf(logging.Info, "serverlist", "loaded %d locations", len(countries))
	return countries, nil
}

// SelectLocation persists a location choice without connecting (connect
// arrives in M3; reconnect/auto-pick logic lives there too).
func (c *Controller) SelectLocation(countryCode, cityCode, server string) error {
	if countryCode == "" || server == "" {
		return &api.RemoteError{Code: api.CodeBadRequest, Message: "country_code and server are required"}
	}
	if err := c.store.UpdateState(func(s *settings.State) {
		s.SelectedLocation = countryCode
		s.SelectedCity = cityCode
		s.SelectedServer = server
	}); err != nil {
		return fmt.Errorf("persist selection: %w", err)
	}
	c.log.Logf(logging.Info, "serverlist", "selected %s/%s via %s", countryCode, cityCode, server)
	c.notify()
	return nil
}
