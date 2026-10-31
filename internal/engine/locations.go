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

// filterContext snapshots the values the server-list JEXL expressions read.
func (c *Controller) filterContext() serverlist.Context {
	c.mu.Lock()
	country := c.exitCountry
	c.mu.Unlock()
	return serverlist.Context{FirefoxVersion: c.firefoxVersion, Country: country}
}

// parseServerList evaluates the raw collection with the current context and
// settings. It is called on cache hits too, so allow_locked and a newly known
// exit country take effect without a network fetch.
func (c *Controller) parseServerList(raw []byte) ([]serverlist.Country, error) {
	opts := serverlist.Options{
		Context:     c.filterContext(),
		AllowLocked: c.store.EffectiveConfig().AllowLocked,
	}
	if c.log != nil {
		opts.Logf = func(format string, args ...any) {
			c.log.Logf(logging.Debug, "serverlist", format, args...)
		}
	}
	return serverlist.ParseWithOptions(raw, opts)
}

// Locations returns the visible server list from a fresh raw payload, or
// fetches it. A failed non-451 refresh falls back to the in-memory raw payload,
// then to the disposable disk cache, re-filtering both with current options.
func (c *Controller) Locations(ctx context.Context, refresh bool) ([]serverlist.Country, error) {
	c.mu.Lock()
	raw := c.locationsRaw
	cachedAt := c.locationsAt
	c.mu.Unlock()

	if !refresh && len(raw) > 0 && time.Since(cachedAt) < locationsCacheTTL {
		countries, err := c.parseServerList(raw)
		if err != nil {
			return nil, err
		}
		serverlist.SortCountries(countries)
		c.mu.Lock()
		c.locations = countries
		c.mu.Unlock()
		return countries, nil
	}

	fetched, err := serverlist.FetchRawFrom(ctx, c.httpClient, c.serverListURL)
	if err != nil {
		// A 451 is terminal: do not mask it with a cached list that can no
		// longer connect either.
		if re := c.regionBlocked(err); re != nil {
			return nil, re
		}
		if len(raw) > 0 {
			countries, perr := c.parseServerList(raw)
			if perr == nil {
				serverlist.SortCountries(countries)
				c.mu.Lock()
				c.locations = countries
				c.mu.Unlock()
				c.log.Logf(logging.Warn, "serverlist", "refresh failed, keeping cached list: %v", err)
				return countries, nil
			}
			c.log.Logf(logging.Warn, "serverlist", "cached server list is no longer parseable: %v", perr)
		}
		return c.diskCacheFallback(err)
	}
	countries, err := c.parseServerList(fetched)
	if err != nil {
		return nil, err
	}
	serverlist.SortCountries(countries)

	// Cache only a payload that parsed, best effort: the disk cache is a
	// convenience and must never fail a successful refresh.
	if err := serverlist.SaveCache(c.serverListCache, fetched); err != nil {
		c.log.Logf(logging.Warn, "serverlist", "could not cache server list: %v", err)
	}

	c.mu.Lock()
	c.locations = countries
	c.locationsRaw = append([]byte(nil), fetched...)
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

	countries, err := c.parseServerList(raw)
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

// SelectLocation persists a location choice without connecting.
func (c *Controller) SelectLocation(ctx context.Context, countryCode, cityCode, server string, fastest bool) error {
	if fastest {
		countries, err := c.Locations(ctx, false)
		if err != nil {
			return err
		}
		cand, err := c.fastestCandidate(countries)
		if err != nil {
			return err
		}
		countryCode, cityCode, server = cand.CountryCode, cand.CityCode, cand.Address()
	}
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
	if fastest {
		c.log.Logf(logging.Info, "serverlist", "selected fastest -> %s", server)
	} else {
		c.log.Logf(logging.Info, "serverlist", "selected %s/%s via %s", countryCode, cityCode, server)
	}
	c.notify()
	return nil
}

// LocationsView builds the complete locations reply every client consumes:
// visible countries, resolved favorites and recents, cached latency, and the
// locked-view state.
func (c *Controller) LocationsView(ctx context.Context, refresh bool) (api.LocationsResult, error) {
	countries, err := c.Locations(ctx, refresh)
	if err != nil {
		return api.LocationsResult{}, err
	}
	favorites, recents := c.LocationRefs(countries)
	return api.LocationsResult{
		Countries:   countries,
		Favorites:   favorites,
		Recents:     recents,
		Latency:     c.CachedLatency(countries),
		AllowLocked: c.store.EffectiveConfig().AllowLocked,
	}, nil
}
