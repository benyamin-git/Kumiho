package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

func cliLocationsResult() api.LocationsResult {
	ms12, ms4 := 12.0, 4.0
	return api.LocationsResult{
		Countries: []serverlist.Country{
			{Code: "REC", Name: "Recommended Location", Cities: []serverlist.City{{
				Code: "p", Name: "Recommended", Servers: []serverlist.Server{{Hostname: "p.example.net", Port: 2499}},
			}}},
			{Code: "DE", Name: "Germany", Cities: []serverlist.City{{
				Code: "FRA", Name: "Frankfurt", Servers: []serverlist.Server{
					{Hostname: "fra1.example.net", Port: 2499},
					{Hostname: "fra2.example.net", Port: 2499},
				},
			}}},
			{Code: "AR", Name: "Argentina", Cities: []serverlist.City{{
				Code: "EZE", Name: "Buenos Aires", Servers: []serverlist.Server{{Hostname: "eze.example.net", Port: 2499}},
			}}},
		},
		Favorites: []api.LocationRef{
			{CountryCode: "DE", CityCode: "FRA", Server: "fra1.example.net:2499"},
			{CountryCode: "AR", CityCode: "EZE", Server: "eze.example.net:2499"},
		},
		Recents: []api.LocationRef{
			{CountryCode: "DE", CityCode: "FRA", Server: "fra2.example.net:2499"},
			{CountryCode: "REC", CityCode: "p", Server: "p.example.net:2499"},
		},
		Latency: map[string]api.CachedPing{
			"fra1.example.net:2499": {RTTMS: &ms12},
			"fra2.example.net:2499": {RTTMS: nil},
			"eze.example.net:2499":  {RTTMS: &ms4},
		},
	}
}

func TestLocationsSummaryAndFastest(t *testing.T) {
	var out bytes.Buffer
	writeCountrySummary(&out, cliLocationsResult())
	text := out.String()
	if !strings.HasPrefix(text, "fastest: AR EZE eze.example.net:2499 · 4 ms\n") {
		t.Fatalf("fastest line missing:\n%s", text)
	}
	for _, want := range []string{
		"  REC",
		"  DE",
		"  AR",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary missing %q:\n%s", want, text)
		}
	}
	if strings.ContainsAny(text, "*~") {
		t.Fatalf("summary still carries location markers:\n%s", text)
	}
}

func TestLocationsJSONEncodesWholeResult(t *testing.T) {
	client := startFakeCLI(t, func(conn *ipc.Conn, env *ipc.Envelope) error {
		if env.Type != ipc.TypeLocations {
			return nil
		}
		return replyFakeCLI(conn, env, ipc.TypeLocations, cliLocationsResult())
	})
	var out, errOut bytes.Buffer
	code := locationsWithClient(context.Background(), client, locationsOptions{asJSON: true}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	for _, key := range []string{"countries", "favorites", "recents", "latency", "allow_locked"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("JSON missing %q: %s", key, out.String())
		}
	}
	var countries []serverlist.Country
	if err := json.Unmarshal(got["countries"], &countries); err != nil || len(countries) != 3 {
		t.Fatalf("countries JSON = %s, %v", got["countries"], err)
	}
}

func TestLocationsPingUsesAllHostsAndOrdersResults(t *testing.T) {
	var pingHosts []string
	client := startFakeCLI(t, func(conn *ipc.Conn, env *ipc.Envelope) error {
		switch env.Type {
		case ipc.TypeLocations:
			return replyFakeCLI(conn, env, ipc.TypeLocations, cliLocationsResult())
		case ipc.TypePing:
			var p api.PingPayload
			if err := env.DecodePayload(&p); err != nil {
				return err
			}
			pingHosts = append(pingHosts, p.Hosts...)
			if err := replyFakeCLI(conn, env, ipc.TypeResult, api.Result{OK: true}); err != nil {
				return err
			}
			rtt := 9.0
			_ = conn.Event(ipc.TypePingResult, api.PingResult{Host: "eze.example.net:2499", RTTMS: &rtt})
			_ = conn.Event(ipc.TypePingResult, api.PingResult{Host: "fra1.example.net:2499", RTTMS: nil})
			return conn.Event(ipc.TypePingDone, api.PingDone{Total: 2, Answered: 1})
		}
		return nil
	})
	var out, errOut bytes.Buffer
	code := locationsWithClient(context.Background(), client, locationsOptions{ping: true}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if strings.Join(pingHosts, ",") != "p.example.net:2499,fra1.example.net:2499,fra2.example.net:2499,eze.example.net:2499" {
		t.Fatalf("ping hosts = %v", pingHosts)
	}
	text := out.String()
	if strings.Index(text, "fra1.example.net:2499") > strings.Index(text, "eze.example.net:2499") {
		t.Fatalf("results not in target order:\n%s", text)
	}
	if !strings.Contains(text, "unreachable") || !strings.Contains(text, "9 ms") || !strings.Contains(text, "no result") {
		t.Fatalf("ping rows:\n%s", text)
	}
}

func TestLocationsPingDoneErrorExitsOne(t *testing.T) {
	client := startFakeCLI(t, func(conn *ipc.Conn, env *ipc.Envelope) error {
		switch env.Type {
		case ipc.TypeLocations:
			return replyFakeCLI(conn, env, ipc.TypeLocations, cliLocationsResult())
		case ipc.TypePing:
			if err := replyFakeCLI(conn, env, ipc.TypeResult, api.Result{OK: true}); err != nil {
				return err
			}
			return conn.Event(ipc.TypePingDone, api.PingDone{Error: "Ping all needs a signed-in account"})
		}
		return nil
	})
	var out, errOut bytes.Buffer
	code := locationsWithClient(context.Background(), client, locationsOptions{ping: true}, &out, &errOut)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "kumiho locations: ping: Ping all needs a signed-in account") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestLocationsPingClosedEventsExitOne(t *testing.T) {
	client := startFakeCLI(t, func(conn *ipc.Conn, env *ipc.Envelope) error {
		switch env.Type {
		case ipc.TypeLocations:
			return replyFakeCLI(conn, env, ipc.TypeLocations, cliLocationsResult())
		case ipc.TypePing:
			if err := replyFakeCLI(conn, env, ipc.TypeResult, api.Result{OK: true}); err != nil {
				return err
			}
			return conn.Close()
		}
		return nil
	})
	var out, errOut bytes.Buffer
	code := locationsWithClient(context.Background(), client, locationsOptions{ping: true}, &out, &errOut)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "daemon connection closed") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestLocationsPingJSONRefetchesAfterDone(t *testing.T) {
	locationsCalls := 0
	client := startFakeCLI(t, func(conn *ipc.Conn, env *ipc.Envelope) error {
		switch env.Type {
		case ipc.TypeLocations:
			locationsCalls++
			res := cliLocationsResult()
			if locationsCalls > 1 {
				rtt := 1.0
				res.Latency["eze.example.net:2499"] = api.CachedPing{RTTMS: &rtt, MeasuredAt: time.Now()}
			}
			return replyFakeCLI(conn, env, ipc.TypeLocations, res)
		case ipc.TypePing:
			if err := replyFakeCLI(conn, env, ipc.TypeResult, api.Result{OK: true}); err != nil {
				return err
			}
			return conn.Event(ipc.TypePingDone, api.PingDone{Total: 1, Answered: 1})
		}
		return nil
	})
	var out, errOut bytes.Buffer
	code := locationsWithClient(context.Background(), client, locationsOptions{ping: true, asJSON: true}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if locationsCalls != 2 {
		t.Fatalf("locations calls = %d, want initial + refetch", locationsCalls)
	}
	var got api.LocationsResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if sample := got.Latency["eze.example.net:2499"]; sample.RTTMS == nil || *sample.RTTMS != 1 {
		t.Fatalf("refetched latency = %+v", sample)
	}
}
