package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

type locationsOptions struct {
	asJSON    bool
	ping      bool
	refresh   bool
	favorites bool
	recent    bool
}

func runLocations(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("locations", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var opts locationsOptions
	fs.BoolVar(&opts.asJSON, "json", false, "emit machine-readable JSON")
	fs.BoolVar(&opts.ping, "ping", false, "measure TCP latency to the selected servers")
	fs.BoolVar(&opts.refresh, "refresh", false, "force a server-list refresh")
	fs.BoolVar(&opts.favorites, "favorites", false, "show favorite locations")
	fs.BoolVar(&opts.recent, "recent", false, "show recent locations")
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: kumiho locations [--ping] [--refresh] [--json] [--favorites] [--recent]\n\nLists available VPN locations.\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "kumiho locations: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	client, code := dialDaemon(stderr)
	if client == nil {
		return code
	}
	defer client.Close()
	ctx, cancel := callCtx()
	defer cancel()
	return locationsWithClient(ctx, client, opts, stdout, stderr)
}

func locationsWithClient(ctx context.Context, client *ipc.Client, opts locationsOptions, stdout, stderr io.Writer) int {
	res, err := fetchLocations(ctx, client, opts.refresh)
	if err != nil {
		fmt.Fprintf(stderr, "kumiho locations: %v\n", err)
		return 1
	}
	if opts.ping {
		return pingLocations(ctx, client, res, opts, stdout, stderr)
	}
	if opts.asJSON {
		return encodeLocations(stdout, stderr, res)
	}
	if opts.favorites || opts.recent {
		rows := selectedRows(res, opts.favorites, opts.recent)
		writeLocationRows(stdout, rows, res.Latency)
		return 0
	}
	writeCountrySummary(stdout, res)
	return 0
}

func fetchLocations(ctx context.Context, client *ipc.Client, refresh bool) (api.LocationsResult, error) {
	env, err := client.Call(ctx, ipc.TypeLocations, api.LocationsPayload{Refresh: refresh})
	if err != nil {
		return api.LocationsResult{}, err
	}
	var res api.LocationsResult
	if err := env.DecodePayload(&res); err != nil {
		return api.LocationsResult{}, err
	}
	return res, nil
}

func encodeLocations(stdout, stderr io.Writer, res api.LocationsResult) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		fmt.Fprintf(stderr, "kumiho locations: %v\n", err)
		return 1
	}
	return 0
}

type locationRow struct {
	marker  string
	country string
	city    string
	addr    string
}

func writeCountrySummary(stdout io.Writer, res api.LocationsResult) {
	favorites, recents := locationMarkers(res)
	if cand, rtt, ok := fastestVisible(res); ok {
		fmt.Fprintf(stdout, "fastest: %s %s %s · %.0f ms\n", cand.CountryCode, cand.CityCode, cand.Address(), rtt)
	}
	for _, c := range res.Countries {
		marker := " "
		key := strings.ToLower(c.Code)
		if favorites[key] {
			marker = "*"
		} else if recents[key] {
			marker = "~"
		}
		fmt.Fprintf(stdout, "%s %-4s %-22s %d cities\n", marker, c.Code, c.Name, len(c.Cities))
	}
}

func locationMarkers(res api.LocationsResult) (favorites, recents map[string]bool) {
	favorites = make(map[string]bool)
	recents = make(map[string]bool)
	for _, ref := range res.Favorites {
		if cand, ok := resolveLocationRef(res.Countries, ref); ok {
			favorites[strings.ToLower(cand.CountryCode)] = true
		}
	}
	for _, ref := range res.Recents {
		if cand, ok := resolveLocationRef(res.Countries, ref); ok {
			recents[strings.ToLower(cand.CountryCode)] = true
		}
	}
	return favorites, recents
}

func fastestVisible(res api.LocationsResult) (serverlist.Candidate, float64, bool) {
	var best serverlist.Candidate
	var bestRTT float64
	found := false
	for _, cand := range serverlist.Candidates(res.Countries, "", "") {
		sample, ok := res.Latency[cand.Address()]
		if !ok || sample.RTTMS == nil {
			continue
		}
		if !found || *sample.RTTMS < bestRTT {
			best, bestRTT, found = cand, *sample.RTTMS, true
		}
	}
	return best, bestRTT, found
}

func selectedRows(res api.LocationsResult, favorites, recents bool) []locationRow {
	rows := make([]locationRow, 0, len(res.Favorites)+len(res.Recents))
	seen := make(map[string]bool)
	add := func(refs []api.LocationRef, marker string) {
		for _, ref := range refs {
			cand, ok := resolveLocationRef(res.Countries, ref)
			if !ok {
				continue
			}
			key := strings.ToLower(cand.Address())
			if seen[key] {
				continue
			}
			seen[key] = true
			rows = append(rows, locationRow{
				marker: marker, country: cand.CountryCode, city: cand.CityCode, addr: cand.Address(),
			})
		}
	}
	if favorites {
		add(res.Favorites, "*")
	}
	if recents {
		add(res.Recents, "~")
	}
	return rows
}

func allRows(res api.LocationsResult) []locationRow {
	favorites, recents := locationMarkers(res)
	var rows []locationRow
	for _, cand := range serverlist.Candidates(res.Countries, "", "") {
		marker := " "
		key := strings.ToLower(cand.CountryCode)
		if favorites[key] {
			marker = "*"
		} else if recents[key] {
			marker = "~"
		}
		rows = append(rows, locationRow{
			marker: marker, country: cand.CountryCode, city: cand.CityCode, addr: cand.Address(),
		})
	}
	return rows
}

func resolveLocationRef(countries []serverlist.Country, ref api.LocationRef) (serverlist.Candidate, bool) {
	candidates := serverlist.Candidates(countries, ref.CountryCode, ref.CityCode)
	for _, cand := range candidates {
		if cand.Address() == ref.Server || cand.Host == ref.Server {
			return cand, true
		}
	}
	if len(candidates) > 0 {
		return candidates[0], true
	}
	return serverlist.Candidate{}, false
}

func writeLocationRows(stdout io.Writer, rows []locationRow, latency map[string]api.CachedPing) {
	for _, row := range rows {
		fmt.Fprintf(stdout, "%s %-4s %-6s %-45s %s\n", row.marker, row.country, row.city, row.addr, rttLabel(latency, row.addr))
	}
}

func rttLabel(latency map[string]api.CachedPing, addr string) string {
	sample, ok := latency[addr]
	switch {
	case !ok:
		return "-"
	case sample.RTTMS == nil:
		return "timeout"
	default:
		return fmt.Sprintf("%.0f ms", *sample.RTTMS)
	}
}

func pingLocations(ctx context.Context, client *ipc.Client, res api.LocationsResult, opts locationsOptions, stdout, stderr io.Writer) int {
	var rows []locationRow
	if opts.favorites || opts.recent {
		rows = selectedRows(res, opts.favorites, opts.recent)
	} else {
		rows = allRows(res)
	}
	hosts := make([]string, 0, len(rows))
	for _, row := range rows {
		hosts = append(hosts, row.addr)
	}

	if _, err := client.Call(ctx, ipc.TypePing, api.PingPayload{Hosts: hosts}); err != nil {
		fmt.Fprintf(stderr, "kumiho locations: ping: %v\n", err)
		return 1
	}

	rtts := make(map[string]*float64, len(hosts))
	for {
		select {
		case env, ok := <-client.Events():
			if !ok {
				fmt.Fprintln(stderr, "kumiho locations: daemon connection closed")
				return 1
			}
			switch env.Type {
			case ipc.TypePingResult:
				var pr api.PingResult
				if err := env.DecodePayload(&pr); err == nil {
					rtts[pr.Host] = pr.RTTMS
				}
			case ipc.TypePingDone:
				var done api.PingDone
				if err := env.DecodePayload(&done); err != nil {
					fmt.Fprintf(stderr, "kumiho locations: ping: %v\n", err)
					return 1
				}
				if done.Error != "" {
					fmt.Fprintf(stderr, "kumiho locations: ping: %s\n", done.Error)
					return 1
				}
				if opts.asJSON {
					updated, err := fetchLocations(ctx, client, false)
					if err != nil {
						fmt.Fprintf(stderr, "kumiho locations: %v\n", err)
						return 1
					}
					return encodeLocations(stdout, stderr, updated)
				}
				for _, row := range rows {
					rtt, ok := rtts[row.addr]
					switch {
					case !ok:
						fmt.Fprintf(stdout, "%s %-4s %-6s %-45s no result\n", row.marker, row.country, row.city, row.addr)
					case rtt == nil:
						fmt.Fprintf(stdout, "%s %-4s %-6s %-45s unreachable\n", row.marker, row.country, row.city, row.addr)
					default:
						fmt.Fprintf(stdout, "%s %-4s %-6s %-45s %.0f ms\n", row.marker, row.country, row.city, row.addr, *rtt)
					}
				}
				return 0
			}
		}
	}
}
