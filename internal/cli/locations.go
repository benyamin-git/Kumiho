package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

func runLocations(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("locations", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit machine-readable JSON")
	doPing := fs.Bool("ping", false, "measure TCP latency to every server")
	refresh := fs.Bool("refresh", false, "force a server-list refresh")
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: kumiho locations [--ping] [--refresh] [--json]\n\nLists available VPN locations.\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	client, code := dialDaemon(stderr)
	if client == nil {
		return code
	}
	defer client.Close()

	ctx, cancel := callCtx()
	defer cancel()
	env, err := client.Call(ctx, ipc.TypeLocations, ipc.LocationsPayload{Refresh: *refresh})
	if err != nil {
		fmt.Fprintf(stderr, "kumiho locations: %v\n", err)
		return 1
	}
	var res ipc.LocationsResult
	if err := env.DecodePayload(&res); err != nil {
		fmt.Fprintf(stderr, "kumiho locations: %v\n", err)
		return 1
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res.Countries); err != nil {
			fmt.Fprintf(stderr, "kumiho locations: %v\n", err)
			return 1
		}
		return 0
	}

	if *doPing {
		return pingLocations(ctx, client, res.Countries, stdout, stderr)
	}

	for _, c := range res.Countries {
		fmt.Fprintf(stdout, "%-4s %-22s %d cities\n", c.Code, c.Name, len(c.Cities))
	}
	return 0
}

type locationTarget struct {
	country string
	city    string
	addr    string
}

func pingLocations(ctx context.Context, client *ipc.Client, countries []serverlist.Country, stdout, stderr io.Writer) int {
	var targets []locationTarget
	for _, c := range countries {
		for _, city := range c.Cities {
			for _, s := range city.Servers {
				if s.Quarantined {
					continue
				}
				host, port, ok := s.ConnectTarget()
				if !ok {
					continue
				}
				targets = append(targets, locationTarget{
					country: c.Code,
					city:    city.Code,
					addr:    fmt.Sprintf("%s:%d", host, port),
				})
			}
		}
	}
	hosts := make([]string, 0, len(targets))
	for _, t := range targets {
		hosts = append(hosts, t.addr)
	}

	if _, err := client.Call(ctx, ipc.TypePing, ipc.PingPayload{Hosts: hosts}); err != nil {
		fmt.Fprintf(stderr, "kumiho locations: ping: %v\n", err)
		return 1
	}

	// The daemon streams one ping_result event per target.
	rtts := make(map[string]*float64, len(hosts))
	deadline := time.After(6 * time.Second)
collect:
	for len(rtts) < len(hosts) {
		select {
		case env, ok := <-client.Events():
			if !ok {
				fmt.Fprintln(stderr, "kumiho locations: daemon connection closed")
				return 1
			}
			if env.Type != ipc.TypePingResult {
				continue
			}
			var pr ipc.PingResult
			if err := env.DecodePayload(&pr); err == nil {
				rtts[pr.Host] = pr.RTTMS
			}
		case <-deadline:
			break collect
		}
	}

	for _, t := range targets {
		rtt, ok := rtts[t.addr]
		switch {
		case !ok:
			fmt.Fprintf(stdout, "%-4s %-6s %-45s no result\n", t.country, t.city, t.addr)
		case rtt == nil:
			fmt.Fprintf(stdout, "%-4s %-6s %-45s unreachable\n", t.country, t.city, t.addr)
		default:
			fmt.Fprintf(stdout, "%-4s %-6s %-45s %.0f ms\n", t.country, t.city, t.addr, *rtt)
		}
	}
	return 0
}
