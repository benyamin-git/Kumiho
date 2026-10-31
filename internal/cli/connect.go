package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

type connectOptions struct {
	to         string
	proxyOnly  bool
	fastest    bool
	killSwitch string
}

func runConnect(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var opts connectOptions
	fs.StringVar(&opts.to, "to", "", "country code, city code, or server host (default: saved selection)")
	fs.BoolVar(&opts.proxyOnly, "proxy-only", false, "keep system routing untouched (SOCKS5 proxy only)")
	fs.StringVar(&opts.killSwitch, "kill-switch", "", "on|off|last (persisted before connecting)")
	fs.BoolVar(&opts.fastest, "fastest", false, "connect to the fastest location from the last Ping all")
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: kumiho connect [--to CODE|CITY|HOST | --fastest] [--proxy-only] [--kill-switch=on|off|last]\n\nConnects the tunnel (full system tunnel by default).\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "kumiho connect: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if opts.fastest && opts.to != "" {
		fmt.Fprintln(stderr, "kumiho connect: choose --to or --fastest, not both")
		fs.Usage()
		return 2
	}

	client, code := dialDaemon(stderr)
	if client == nil {
		return code
	}
	defer client.Close()

	// Connect can spend up to four dial attempts with backoff.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return connectWithClient(ctx, client, opts, stdout, stderr)
}

func connectWithClient(ctx context.Context, client *ipc.Client, opts connectOptions, stdout, stderr io.Writer) int {
	if opts.killSwitch != "" {
		if _, err := client.Call(ctx, ipc.TypeSettingsSet, api.SettingsSetPayload{Key: "kill_switch", Value: opts.killSwitch}); err != nil {
			fmt.Fprintf(stderr, "kumiho connect: kill-switch: %v\n", err)
			return 1
		}
	}

	payload := api.ConnectPayload{ProxyOnly: opts.proxyOnly, Fastest: opts.fastest}
	if opts.to != "" {
		env, err := client.Call(ctx, ipc.TypeLocations, api.LocationsPayload{})
		if err != nil {
			fmt.Fprintf(stderr, "kumiho connect: %v\n", err)
			return 1
		}
		var res api.LocationsResult
		if err := env.DecodePayload(&res); err != nil {
			fmt.Fprintf(stderr, "kumiho connect: %v\n", err)
			return 1
		}
		resolved, err := resolveTo(res.Countries, opts.to)
		if err != nil {
			fmt.Fprintf(stderr, "kumiho connect: %v\n", err)
			return 1
		}
		payload = resolved
		payload.ProxyOnly = opts.proxyOnly
	}

	fmt.Fprintln(stderr, "connecting (this can take up to ~30s)...")
	if _, err := client.Call(ctx, ipc.TypeConnect, payload); err != nil {
		fmt.Fprintf(stderr, "kumiho connect: %v\n", err)
		return 1
	}

	st, err := client.Status(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "kumiho connect: %v\n", err)
		return 1
	}
	printStatus(stdout, st)
	return 0
}

func runDisconnect(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("disconnect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: kumiho disconnect\n\nTears the tunnel down.\n")
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
	if _, err := client.Call(ctx, ipc.TypeDisconnect, nil); err != nil {
		fmt.Fprintf(stderr, "kumiho disconnect: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "Disconnected.")
	return 0
}

// resolveTo turns --to into a connect payload using the server list.
func resolveTo(countries []serverlist.Country, to string) (api.ConnectPayload, error) {
	to = strings.TrimSpace(to)
	for _, c := range countries {
		if strings.EqualFold(c.Code, to) {
			return api.ConnectPayload{LocationCode: c.Code}, nil
		}
	}
	for _, c := range countries {
		for _, city := range c.Cities {
			if strings.EqualFold(city.Code, to) {
				return api.ConnectPayload{LocationCode: c.Code, CityCode: city.Code}, nil
			}
		}
	}
	for _, cand := range serverlist.Candidates(countries, "", "") {
		if cand.Address() == to || cand.Host == to || strings.HasPrefix(cand.Host, to) {
			return api.ConnectPayload{
				LocationCode: cand.CountryCode,
				CityCode:     cand.CityCode,
				Server:       cand.Address(),
			}, nil
		}
	}
	lower := strings.ToLower(to)
	var matches []string
	for _, c := range countries {
		if strings.Contains(strings.ToLower(c.Name), lower) {
			matches = append(matches, c.Code)
		}
	}
	switch len(matches) {
	case 1:
		return api.ConnectPayload{LocationCode: matches[0]}, nil
	case 0:
		return api.ConnectPayload{}, fmt.Errorf("no location or server matches %q", to)
	default:
		return api.ConnectPayload{}, fmt.Errorf("%q matches %s; use one country code", to, strings.Join(matches, ", "))
	}
}
