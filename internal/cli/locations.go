package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

type locationsOptions struct {
	asJSON  bool
	ping    bool
	refresh bool
}

func runLocations(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("locations", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var opts locationsOptions
	fs.BoolVar(&opts.asJSON, "json", false, "emit machine-readable JSON")
	fs.BoolVar(&opts.ping, "ping", false, "measure TCP latency to every usable server")
	fs.BoolVar(&opts.refresh, "refresh", false, "force a server-list refresh")
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: kumiho locations [--ping] [--refresh] [--json]\n\nLists available VPN locations.\n")
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
	country string
	city    string
	addr    string
}

func writeCountrySummary(stdout io.Writer, res api.LocationsResult) {
	if cand, rtt, ok := fastestVisible(res); ok {
		fmt.Fprintf(stdout, "fastest: %s %s %s · %.0f ms\n", cand.CountryCode, cand.CityCode, cand.Address(), rtt)
	}
	for _, c := range res.Countries {
		fmt.Fprintf(stdout, "  %-4s %-22s %d cities\n", c.Code, c.Name, len(c.Cities))
	}
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

func allRows(res api.LocationsResult) []locationRow {
	var rows []locationRow
	for _, cand := range serverlist.Candidates(res.Countries, "", "") {
		rows = append(rows, locationRow{
			country: cand.CountryCode, city: cand.CityCode, addr: cand.Address(),
		})
	}
	return rows
}

func pingLocations(ctx context.Context, client *ipc.Client, res api.LocationsResult, opts locationsOptions, stdout, stderr io.Writer) int {
	rows := allRows(res)
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
						fmt.Fprintf(stdout, "  %-4s %-6s %-45s no result\n", row.country, row.city, row.addr)
					case rtt == nil:
						fmt.Fprintf(stdout, "  %-4s %-6s %-45s unreachable\n", row.country, row.city, row.addr)
					default:
						fmt.Fprintf(stdout, "  %-4s %-6s %-45s %.0f ms\n", row.country, row.city, row.addr, *rtt)
					}
				}
				return 0
			}
		}
	}
}
