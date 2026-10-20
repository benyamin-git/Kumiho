package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
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
// falls back to the in-memory list, then to the disposable disk cache.
func (c *Controller) Locations(ctx context.Context, refresh bool) ([]serverlist.Country, error) {
	c.mu.Lock()
	cached := c.locations
	cachedAt := c.locationsAt
	c.mu.Unlock()

	if !refresh && cached != nil && time.Since(cachedAt) < locationsCacheTTL {
		return cached, nil
	}

	raw, err := serverlist.FetchRawFrom(ctx, c.httpClient, c.serverListURL)
	if err != nil {
		if cached != nil {
			c.log.Logf(logging.Warn, "serverlist", "refresh failed, keeping cached list: %v", err)
			return cached, nil
		}
		return c.diskCacheFallback(err)
	}
	countries, err := serverlist.Parse(raw)
	if err != nil {
		return nil, err
	}
	serverlist.SortCountries(countries)

	// Cache only a payload that parsed, best effort: the disk cache is a
	// convenience and must never fail a successful refresh.
	if err := serverlist.SaveCache(c.serverListCache, raw); err != nil {
		c.log.Logf(logging.Warn, "serverlist", "could not cache server list: %v", err)
	}

	c.mu.Lock()
	c.locations = countries
	c.locationsAt = time.Now()
	c.mu.Unlock()

	c.log.Logf(logging.Info, "serverlist", "loaded %d locations", len(countries))
	return countries, nil
}

// diskCacheFallback serves a failed fetch from the disposable disk cache. The
// list is returned for this call only: the in-memory list stays empty so the
// next call retries the network instead of extending a stale window. Any
// unusable cache is discarded, and fetchErr is always returned when the cache
// cannot serve the call.
func (c *Controller) diskCacheFallback(fetchErr error) ([]serverlist.Country, error) {
	raw, err := serverlist.LoadCache(c.serverListCache)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			c.log.Logf(logging.Warn, "serverlist", "could not read cached server list: %v", err)
		}
		return nil, fetchErr
	}

	countries, err := serverlist.Parse(raw)
	if err == nil && len(countries) == 0 {
		err = errors.New("no locations")
	}
	if err != nil {
		c.log.Logf(logging.Warn, "serverlist", "discarding unusable cached server list: %v", err)
		if rmErr := os.Remove(c.serverListCache); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			c.log.Logf(logging.Warn, "serverlist", "could not remove cached server list: %v", rmErr)
		}
		return nil, fetchErr
	}

	serverlist.SortCountries(countries)
	c.log.Logf(logging.Warn, "serverlist", "using cached server list")
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
