package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/serverlist"
	"github.com/benyamin-git/kumiho/internal/settings"
)

const maxRecents = 5

func toAPIRef(ref settings.LocationRef) api.LocationRef {
	return api.LocationRef{
		CountryCode: ref.CountryCode,
		CityCode:    ref.CityCode,
		Server:      ref.Server,
	}
}

func fromAPIRef(ref api.LocationRef) settings.LocationRef {
	return settings.LocationRef{
		CountryCode: ref.CountryCode,
		CityCode:    ref.CityCode,
		Server:      ref.Server,
	}
}

func refFromCandidate(cand serverlist.Candidate) settings.LocationRef {
	return settings.LocationRef{
		CountryCode: cand.CountryCode,
		CityCode:    cand.CityCode,
		Server:      cand.Address(),
	}
}

func sameRef(a, b settings.LocationRef) bool {
	return a.CountryCode == b.CountryCode && a.CityCode == b.CityCode && a.Server == b.Server
}

// resolveRef maps a persisted reference onto the current visible list. The
// exact server wins; otherwise the first usable server in the city is used so
// a rotated edge keeps the favorite working.
func resolveRef(countries []serverlist.Country, ref settings.LocationRef) (serverlist.Candidate, bool) {
	candidates := serverlist.Candidates(countries, ref.CountryCode, ref.CityCode)
	if ref.Server != "" {
		for _, cand := range candidates {
			if cand.Address() == ref.Server || cand.Host == ref.Server {
				return cand, true
			}
		}
	}
	if len(candidates) > 0 {
		return candidates[0], true
	}
	return serverlist.Candidate{}, false
}

func (c *Controller) locationsSnapshot() []serverlist.Country {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]serverlist.Country(nil), c.locations...)
}

func (c *Controller) refMatches(existing, requested settings.LocationRef, requestedCand serverlist.Candidate, countries []serverlist.Country) bool {
	if sameRef(existing, requested) {
		return true
	}
	if cand, ok := resolveRef(countries, existing); ok && cand.Address() == requestedCand.Address() {
		return true
	}
	return false
}

// SetFavorite resolves and toggles one favorite. Persisted refs are shared by
// every account and survive a server rotation through city resolution.
func (c *Controller) SetFavorite(ctx context.Context, ref settings.LocationRef, favorite bool) ([]api.LocationRef, error) {
	countries, err := c.Locations(ctx, false)
	if err != nil {
		return nil, err
	}
	cand, ok := resolveRef(countries, ref)
	if !ok {
		return nil, &api.RemoteError{Code: api.CodeBadRequest, Message: "location is no longer available"}
	}
	err = c.store.UpdateState(func(s *settings.State) {
		next := make([]settings.LocationRef, 0, len(s.Favorites)+1)
		matched := false
		for _, existing := range s.Favorites {
			if c.refMatches(existing, ref, cand, countries) {
				matched = true
				if favorite {
					next = append(next, existing)
				}
				continue
			}
			next = append(next, existing)
		}
		if favorite && !matched {
			next = append(next, refFromCandidate(cand))
		}
		s.Favorites = next
	})
	if err != nil {
		return nil, fmt.Errorf("persist favorites: %w", err)
	}
	c.notify()
	favorites, _ := c.LocationRefs(countries)
	return favorites, nil
}

// ClearRecents removes every recent location. It is deliberately not a
// favorite toggle and does not need confirmation at this layer.
func (c *Controller) ClearRecents() error {
	if err := c.store.UpdateState(func(s *settings.State) {
		s.Recents = nil
	}); err != nil {
		return fmt.Errorf("persist recents: %w", err)
	}
	return nil
}

// recordRecent moves a successful connect target to the front of the recent
// list. Persistence is best effort: a read-only state file must not turn a
// working tunnel into a failed connect.
func (c *Controller) recordRecent(cand serverlist.Candidate) {
	ref := refFromCandidate(cand)
	countries := c.locationsSnapshot()
	if err := c.store.UpdateState(func(s *settings.State) {
		next := make([]settings.LocationRef, 0, maxRecents)
		next = append(next, ref)
		for _, existing := range s.Recents {
			if sameRef(existing, ref) {
				continue
			}
			if existingCand, ok := resolveRef(countries, existing); ok && existingCand.Address() == cand.Address() {
				continue
			}
			if len(next) >= maxRecents {
				break
			}
			next = append(next, existing)
		}
		s.Recents = next
	}); err != nil {
		c.log.Logf(logging.Warn, "serverlist", "could not persist recent locations: %v", err)
	}
}

// LocationRefs resolves favorites and recents for the locations view. The
// stored refs are never rewritten here: a temporarily absent country is
// omitted from the reply but kept for the next refresh.
func (c *Controller) LocationRefs(countries []serverlist.Country) (favorites, recents []api.LocationRef) {
	st := c.store.State()

	ordered := append([]serverlist.Country(nil), countries...)
	serverlist.SortCountries(ordered)
	countryRank := make(map[string]int, len(ordered))
	cityNames := make(map[string]string)
	for rank, country := range ordered {
		countryRank[strings.ToLower(country.Code)] = rank
		for _, city := range country.Cities {
			cityNames[strings.ToLower(country.Code)+"\x00"+strings.ToLower(city.Code)] = city.Name
		}
	}

	favoriteCands := make([]serverlist.Candidate, 0, len(st.Favorites))
	for _, ref := range st.Favorites {
		if cand, ok := resolveRef(countries, ref); ok {
			favoriteCands = append(favoriteCands, cand)
		}
	}
	sort.SliceStable(favoriteCands, func(i, j int) bool {
		return favoriteLess(favoriteCands[i], favoriteCands[j], countryRank, cityNames)
	})
	favorites = candidateRefs(favoriteCands)

	recentCands := make([]serverlist.Candidate, 0, len(st.Recents))
	for _, ref := range st.Recents {
		if cand, ok := resolveRef(countries, ref); ok {
			recentCands = append(recentCands, cand)
		}
	}
	recents = candidateRefs(recentCands)
	return favorites, recents
}

func favoriteLess(a, b serverlist.Candidate, countryRank map[string]int, cityNames map[string]string) bool {
	ar, aok := countryRank[strings.ToLower(a.CountryCode)]
	br, bok := countryRank[strings.ToLower(b.CountryCode)]
	if aok != bok {
		return aok
	}
	if ar != br {
		return ar < br
	}
	an := cityNames[strings.ToLower(a.CountryCode)+"\x00"+strings.ToLower(a.CityCode)]
	bn := cityNames[strings.ToLower(b.CountryCode)+"\x00"+strings.ToLower(b.CityCode)]
	if an == "" {
		an = a.CityCode
	}
	if bn == "" {
		bn = b.CityCode
	}
	if an != bn {
		return an < bn
	}
	if a.CityCode != b.CityCode {
		return strings.ToLower(a.CityCode) < strings.ToLower(b.CityCode)
	}
	return false
}

// candidateRefs converts candidates to the wire form and collapses duplicate
// servers, keeping the first occurrence.
func candidateRefs(cands []serverlist.Candidate) []api.LocationRef {
	out := make([]api.LocationRef, 0, len(cands))
	seen := make(map[string]bool, len(cands))
	for _, cand := range cands {
		key := strings.ToLower(cand.Address())
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, toAPIRef(refFromCandidate(cand)))
	}
	return out
}
