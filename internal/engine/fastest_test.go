package engine

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/serverlist"
	"github.com/benyamin-git/kumiho/internal/upstream"
)

type fastestSpec struct {
	country string
	city    string
	host    string
}

func (s fastestSpec) address() string {
	return s.host + ":2499"
}

func fastestCountries(specs ...fastestSpec) []serverlist.Country {
	var out []serverlist.Country
	countryIndex := make(map[string]int)
	for _, spec := range specs {
		key := strings.ToLower(spec.country)
		ci, ok := countryIndex[key]
		if !ok {
			out = append(out, serverlist.Country{Code: spec.country, Name: spec.country})
			ci = len(out) - 1
			countryIndex[key] = ci
		}
		country := &out[ci]
		cityIndex := -1
		for i := range country.Cities {
			if strings.EqualFold(country.Cities[i].Code, spec.city) {
				cityIndex = i
				break
			}
		}
		if cityIndex < 0 {
			country.Cities = append(country.Cities, serverlist.City{Code: spec.city, Name: spec.city})
			cityIndex = len(country.Cities) - 1
		}
		country.Cities[cityIndex].Servers = append(country.Cities[cityIndex].Servers,
			serverlist.Server{Hostname: spec.host, Port: 2499})
	}
	return out
}

func seedPing(t *testing.T, ctrl *Controller, addr string, rtt time.Duration, ok bool, measuredAt time.Time) {
	t.Helper()
	if ctrl.pings == nil {
		ctrl.pings = make(map[string]pingSample)
	}
	if measuredAt.IsZero() {
		measuredAt = time.Now()
	}
	ctrl.pings[strings.ToLower(addr)] = pingSample{RTT: rtt, OK: ok, MeasuredAt: measuredAt}
}

func TestCachedLatencyWireShape(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	specs := []fastestSpec{
		{country: "AA", city: "C1", host: "a.example.net"},
		{country: "BB", city: "C1", host: "b.example.net"},
		{country: "CC", city: "C1", host: "c.example.net"},
	}
	countries := fastestCountries(specs...)
	now := time.Now()
	seedPing(t, ctrl, specs[0].address(), 5*time.Millisecond, true, now)
	seedPing(t, ctrl, specs[1].address(), 0, false, now)
	seedPing(t, ctrl, specs[2].address(), time.Millisecond, true, now.Add(-time.Hour))
	seedPing(t, ctrl, "hidden.example.net:2499", time.Millisecond, true, now)

	got := ctrl.CachedLatency(countries)
	if len(got) != 2 {
		t.Fatalf("latency = %+v, want the two fresh visible samples", got)
	}
	first, ok := got[specs[0].address()]
	if !ok {
		t.Fatalf("missing %s in %+v", specs[0].address(), got)
	}
	if first.RTTMS == nil || *first.RTTMS != 5 {
		t.Fatalf("successful RTT = %v, want 5ms", first.RTTMS)
	}
	if first.MeasuredAt.IsZero() {
		t.Fatal("successful sample has no measurement time")
	}
	failed, ok := got[specs[1].address()]
	if !ok {
		t.Fatalf("missing measured failure in %+v", got)
	}
	if failed.RTTMS != nil {
		t.Fatalf("failed RTT = %v, want nil", *failed.RTTMS)
	}
	if failed.MeasuredAt.IsZero() {
		t.Fatal("failed sample has no measurement time")
	}
	if _, ok := got[specs[2].address()]; ok {
		t.Fatal("stale sample survived")
	}
	if _, ok := ctrl.pings[strings.ToLower(specs[2].address())]; ok {
		t.Fatal("stale sample was not pruned from the cache")
	}
	if _, ok := ctrl.pings["hidden.example.net:2499"]; ok {
		t.Fatal("hidden sample was not pruned from the cache")
	}
}

func TestFastestCandidateChoosesLowestFreshRTT(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	specs := []fastestSpec{
		{country: "AA", city: "C1", host: "a.example.net"},
		{country: "BB", city: "C1", host: "b.example.net"},
		{country: "CC", city: "C1", host: "c.example.net"},
	}
	countries := fastestCountries(specs...)
	seedPing(t, ctrl, specs[0].address(), 20*time.Millisecond, true, time.Time{})
	seedPing(t, ctrl, specs[1].address(), 4*time.Millisecond, true, time.Time{})
	seedPing(t, ctrl, specs[2].address(), 9*time.Millisecond, true, time.Time{})

	got, err := ctrl.fastestCandidate(countries)
	if err != nil {
		t.Fatal(err)
	}
	if got.Address() != specs[1].address() {
		t.Fatalf("fastest = %s, want %s", got.Address(), specs[1].address())
	}
}

func TestFastestCandidateTieBreaks(t *testing.T) {
	tests := []struct {
		name  string
		specs []fastestSpec
		want  string
	}{
		{
			name: "country",
			specs: []fastestSpec{
				{country: "BB", city: "C1", host: "b.example.net"},
				{country: "AA", city: "C1", host: "z.example.net"},
			},
			want: "z.example.net:2499",
		},
		{
			name: "city",
			specs: []fastestSpec{
				{country: "AA", city: "ZZZ", host: "z.example.net"},
				{country: "AA", city: "AAA", host: "a.example.net"},
			},
			want: "a.example.net:2499",
		},
		{
			name: "host case-insensitive",
			specs: []fastestSpec{
				{country: "AA", city: "C1", host: "Z.example.net"},
				{country: "AA", city: "C1", host: "a.example.net"},
			},
			want: "a.example.net:2499",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl, _ := newTestController(t, loginOKHandler(t))
			countries := fastestCountries(tc.specs...)
			for _, spec := range tc.specs {
				seedPing(t, ctrl, spec.address(), 5*time.Millisecond, true, time.Time{})
			}
			got, err := ctrl.fastestCandidate(countries)
			if err != nil {
				t.Fatal(err)
			}
			if got.Address() != tc.want {
				t.Fatalf("fastest = %s, want %s", got.Address(), tc.want)
			}
		})
	}
}

func TestFastestCandidateRequiresAFreshFiniteSample(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	specs := []fastestSpec{{country: "AA", city: "C1", host: "a.example.net"}}
	countries := fastestCountries(specs...)
	var re *api.RemoteError
	_, err := ctrl.fastestCandidate(countries)
	if !errors.As(err, &re) || re.Code != api.CodeBadRequest || re.Message != "Fastest needs a completed Ping all" {
		t.Fatalf("no-cache error = %v", err)
	}

	seedPing(t, ctrl, specs[0].address(), 0, false, time.Time{})
	_, err = ctrl.fastestCandidate(countries)
	if !errors.As(err, &re) || re.Message != "Fastest needs a completed Ping all" {
		t.Fatalf("failed-sample error = %v", err)
	}

	seedPing(t, ctrl, specs[0].address(), time.Millisecond, true, time.Now().Add(-time.Hour))
	_, err = ctrl.fastestCandidate(countries)
	if !errors.As(err, &re) || re.Message != "Fastest needs a completed Ping all" {
		t.Fatalf("stale-sample error = %v", err)
	}
}

func TestFastestCandidateIgnoresHiddenLockedServers(t *testing.T) {
	payload := engineListFixture(
		engineListRecord("AA", false, ""),
		engineListRecord("BB", true, ""),
	)
	visible, err := serverlist.ParseWithOptions([]byte(payload), serverlist.Options{})
	if err != nil {
		t.Fatal(err)
	}
	all, err := serverlist.ParseWithOptions([]byte(payload), serverlist.Options{AllowLocked: true})
	if err != nil {
		t.Fatal(err)
	}

	ctrl, _ := newTestController(t, loginOKHandler(t))
	seedPing(t, ctrl, "aa.example.net:2499", 20*time.Millisecond, true, time.Time{})
	seedPing(t, ctrl, "bb.example.net:2499", time.Millisecond, true, time.Time{})
	got, err := ctrl.fastestCandidate(visible)
	if err != nil {
		t.Fatal(err)
	}
	if got.CountryCode != "AA" {
		t.Fatalf("visible fastest = %+v, want the unlocked AA", got)
	}

	// CachedLatency pruned the hidden BB sample above; seed it again for the
	// allow_locked view.
	seedPing(t, ctrl, "bb.example.net:2499", time.Millisecond, true, time.Time{})
	got, err = ctrl.fastestCandidate(all)
	if err != nil {
		t.Fatal(err)
	}
	if got.CountryCode != "BB" {
		t.Fatalf("allow_locked fastest = %+v, want the faster locked BB", got)
	}
}

func TestSelectLocationFastestPersistsTheActualServer(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = servingServerListServer(t, pingFixture(3)).URL
	countries, err := ctrl.Locations(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(countries) != 3 {
		t.Fatalf("countries = %d", len(countries))
	}
	seedPing(t, ctrl, pingHost(1), 2*time.Millisecond, true, time.Time{})
	if err := ctrl.SelectLocation(context.Background(), "", "", "", true); err != nil {
		t.Fatal(err)
	}
	st := ctrl.store.State()
	if st.SelectedLocation != "C1" || st.SelectedServer != pingHost(1) {
		t.Fatalf("state = %+v, want the fastest C1 server", st)
	}

	seedPing(t, ctrl, pingHost(1), 100*time.Millisecond, true, time.Time{})
	seedPing(t, ctrl, pingHost(2), time.Millisecond, true, time.Time{})
	if err := ctrl.SelectLocation(context.Background(), "", "", "", true); err != nil {
		t.Fatal(err)
	}
	st = ctrl.store.State()
	if st.SelectedLocation != "C2" || st.SelectedServer != pingHost(2) {
		t.Fatalf("state after reselection = %+v, want C2", st)
	}
}

func TestConnectFastestUsesAndPersistsTheWinner(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctx := context.Background()
	if st := ctrl.LoginEmail("user@example.com"); st.Step != "password" {
		t.Fatalf("login step = %+v", st)
	}
	if st := ctrl.LoginPassword(ctx, "pw"); st.Step != "done" {
		t.Fatalf("login = %+v", st)
	}

	var quotaCalls atomic.Int32
	ctrl.guardian.BaseURL = guardianPassServer(t, "pass-1", &quotaCalls).URL
	ctrl.serverListURL = servingServerListServer(t, pingFixture(3)).URL
	if _, err := ctrl.Locations(ctx, false); err != nil {
		t.Fatal(err)
	}
	seedPing(t, ctrl, pingHost(0), 50*time.Millisecond, true, time.Time{})
	seedPing(t, ctrl, pingHost(1), 3*time.Millisecond, true, time.Time{})
	seedPing(t, ctrl, pingHost(2), 12*time.Millisecond, true, time.Time{})

	fake := newFakeSession()
	var dialed atomic.Value
	ctrl.dialUpstream = func(_ context.Context, opts upstream.Options) (upstreamSession, error) {
		dialed.Store(opts.Host)
		return fake, nil
	}
	ctrl.exitCheckURL = ""
	ctrl.socksPortOverride = freePort(t)

	err := ctrl.Connect(ctx, api.ConnectPayload{
		LocationCode: "C0",
		CityCode:     "C1",
		Server:       pingHost(0),
		ProxyOnly:    true,
		Fastest:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.stopTunnel("test cleanup")

	if got := dialed.Load(); got != "c1.example.net" {
		t.Fatalf("dialed host = %v, want c1.example.net", got)
	}
	st := ctrl.Snapshot()
	if st.Location != "C1" || st.Server != pingHost(1) {
		t.Fatalf("status selection = %+v, want the fastest C1 server", st)
	}
	persisted := ctrl.store.State()
	if persisted.SelectedLocation != "C1" || persisted.SelectedServer != pingHost(1) {
		t.Fatalf("persisted state = %+v", persisted)
	}
}
