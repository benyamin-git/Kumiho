package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/serverlist"
	"github.com/benyamin-git/kumiho/internal/settings"
	"github.com/benyamin-git/kumiho/internal/upstream"
)

func refFixtureCountries() []serverlist.Country {
	return []serverlist.Country{
		{
			Code: "DE", Name: "Germany",
			Cities: []serverlist.City{{
				Code: "FRA", Name: "Frankfurt",
				Servers: []serverlist.Server{
					{Hostname: "fra1.example.net", Port: 2499},
					{Hostname: "fra2.example.net", Port: 2499},
				},
			}},
		},
	}
}

func TestResolveRef(t *testing.T) {
	countries := refFixtureCountries()
	cand, ok := resolveRef(countries, settings.LocationRef{CountryCode: "DE", CityCode: "FRA", Server: "fra2.example.net:2499"})
	if !ok || cand.Host != "fra2.example.net" {
		t.Fatalf("exact server = %+v, %v", cand, ok)
	}
	cand, ok = resolveRef(countries, settings.LocationRef{CountryCode: "de", CityCode: "fra", Server: "fra2.example.net"})
	if !ok || cand.Host != "fra2.example.net" {
		t.Fatalf("case-insensitive/host-only = %+v, %v", cand, ok)
	}
	cand, ok = resolveRef(countries, settings.LocationRef{CountryCode: "DE", CityCode: "FRA", Server: "gone.example.net:2499"})
	if !ok || cand.Host != "fra1.example.net" {
		t.Fatalf("city fallback = %+v, %v", cand, ok)
	}
	if _, ok := resolveRef(countries, settings.LocationRef{CountryCode: "ZZ", CityCode: "FRA", Server: "fra1.example.net:2499"}); ok {
		t.Fatal("missing country resolved")
	}

	// Quarantined and targetless servers are skipped, and a city with only
	// those has no usable candidate.
	broken := []serverlist.Country{{
		Code: "DE", Name: "Germany",
		Cities: []serverlist.City{{
			Code: "FRA", Name: "Frankfurt",
			Servers: []serverlist.Server{
				{Hostname: "quarantine.example.net", Port: 2499, Quarantined: true},
				{Hostname: "targetless.example.net", Port: 2499, Protocols: []serverlist.Protocol{{Name: "other", Host: "targetless.example.net", Port: 2499}}},
			},
		}},
	}}
	if _, ok := resolveRef(broken, settings.LocationRef{CountryCode: "DE", CityCode: "FRA", Server: "quarantine.example.net:2499"}); ok {
		t.Fatal("quarantined server resolved")
	}
}

func TestLocationRefWireConversion(t *testing.T) {
	cand := serverlist.Candidate{CountryCode: "DE", CityCode: "FRA", Host: "fra1.example.net", Port: 2499}
	ref := refFromCandidate(cand)
	if ref.CountryCode != "DE" || ref.CityCode != "FRA" || ref.Server != "fra1.example.net:2499" {
		t.Fatalf("refFromCandidate = %+v", ref)
	}
	wire := toAPIRef(ref)
	if wire.CountryCode != "DE" || wire.CityCode != "FRA" || wire.Server != "fra1.example.net:2499" {
		t.Fatalf("toAPIRef = %+v", wire)
	}
	if got := fromAPIRef(wire); !sameRef(got, ref) {
		t.Fatalf("round trip = %+v, want %+v", got, ref)
	}
}

func TestSetFavoriteTogglesAndPersists(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = servingServerListServer(t, pingFixture(3)).URL
	ref := settings.LocationRef{CountryCode: "c0", CityCode: "c1", Server: pingHost(0)}

	favorites, err := ctrl.SetFavorite(context.Background(), ref, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(favorites) != 1 || favorites[0].CountryCode != "C0" || favorites[0].Server != pingHost(0) {
		t.Fatalf("favorites = %+v", favorites)
	}
	if got := ctrl.store.State().Favorites; len(got) != 1 || got[0].CountryCode != "C0" {
		t.Fatalf("persisted favorites = %+v", got)
	}

	reopened, err := settings.Open(ctrl.store.Paths())
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.State().Favorites; len(got) != 1 || got[0].Server != pingHost(0) {
		t.Fatalf("reopened favorites = %+v", got)
	}

	// Adding the same resolved server again does not duplicate it.
	favorites, err = ctrl.SetFavorite(context.Background(), ref, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(favorites) != 1 {
		t.Fatalf("duplicate favorites = %+v", favorites)
	}

	favorites, err = ctrl.SetFavorite(context.Background(), ref, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(favorites) != 0 {
		t.Fatalf("after remove = %+v", favorites)
	}
	if got := ctrl.store.State().Favorites; len(got) != 0 {
		t.Fatalf("remove did not persist: %+v", got)
	}

	var re *api.RemoteError
	_, err = ctrl.SetFavorite(context.Background(), settings.LocationRef{CountryCode: "ZZ", Server: "x:1"}, true)
	if !errors.As(err, &re) || re.Code != api.CodeBadRequest || re.Message != "location is no longer available" {
		t.Fatalf("unresolved error = %v", err)
	}
}

func refCityPayload(host string) string {
	return fmt.Sprintf(`{"data":[{"code":"DE","name":"Germany","cities":[{"code":"FRA","name":"Frankfurt","servers":[{"hostname":%q,"port":2499}]}]}]}`, host)
}

func TestSetFavoriteTogglesAfterServerRotation(t *testing.T) {
	var payload atomic.Value
	payload.Store(refCityPayload("fra1.example.net"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, payload.Load().(string))
	}))
	t.Cleanup(srv.Close)

	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = srv.URL
	if _, err := ctrl.SetFavorite(context.Background(), settings.LocationRef{
		CountryCode: "DE", CityCode: "FRA", Server: "fra1.example.net:2499",
	}, true); err != nil {
		t.Fatal(err)
	}

	payload.Store(refCityPayload("fra2.example.net"))
	if _, err := ctrl.Locations(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	favorites, err := ctrl.SetFavorite(context.Background(), settings.LocationRef{
		CountryCode: "DE", CityCode: "FRA", Server: "fra2.example.net:2499",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(favorites) != 0 {
		t.Fatalf("favorites = %+v, want the rotated favorite removed", favorites)
	}
}

func TestLocationRefsViewOrderingAndRetention(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	countries := []serverlist.Country{
		{Code: "DE", Name: "Germany", Cities: []serverlist.City{
			{Code: "MUC", Name: "Munich", Servers: []serverlist.Server{{Hostname: "muc.example.net", Port: 2499}}},
			{Code: "FRA", Name: "Frankfurt", Servers: []serverlist.Server{{Hostname: "fra.example.net", Port: 2499}}},
		}},
		{Code: "AR", Name: "Argentina", Cities: []serverlist.City{
			{Code: "EZE", Name: "Buenos Aires", Servers: []serverlist.Server{{Hostname: "eze.example.net", Port: 2499}}},
		}},
		{Code: "REC", Name: "Recommended Location", Cities: []serverlist.City{
			{Code: "p", Name: "Recommended", Servers: []serverlist.Server{{Hostname: "p.example.net", Port: 2499}}},
		}},
	}
	unresolved := settings.LocationRef{CountryCode: "ZZ", CityCode: "NONE", Server: "zz.example.net:2499"}
	if err := ctrl.store.UpdateState(func(s *settings.State) {
		s.Favorites = []settings.LocationRef{
			{CountryCode: "DE", CityCode: "MUC", Server: "muc.example.net:2499"},
			{CountryCode: "AR", CityCode: "EZE", Server: "eze.example.net:2499"},
			{CountryCode: "DE", CityCode: "FRA", Server: "fra.example.net:2499"},
			{CountryCode: "REC", CityCode: "p", Server: "p.example.net:2499"},
			unresolved,
		}
		s.Recents = []settings.LocationRef{
			{CountryCode: "DE", CityCode: "MUC", Server: "muc.example.net:2499"},
			{CountryCode: "AR", CityCode: "EZE", Server: "eze.example.net:2499"},
			{CountryCode: "DE", CityCode: "MUC", Server: "muc.example.net:2499"},
			unresolved,
		}
	}); err != nil {
		t.Fatal(err)
	}

	favorites, recents := ctrl.LocationRefs(countries)
	wantFavorites := []string{"REC", "AR", "DE", "DE"}
	if len(favorites) != len(wantFavorites) {
		t.Fatalf("favorites = %+v", favorites)
	}
	for i, want := range wantFavorites {
		if favorites[i].CountryCode != want {
			t.Fatalf("favorite %d country = %q, want %q (all %+v)", i, favorites[i].CountryCode, want, favorites)
		}
	}
	if favorites[2].CityCode != "FRA" || favorites[3].CityCode != "MUC" {
		t.Fatalf("city order = %+v, want FRA then MUC", favorites)
	}
	if len(recents) != 2 || recents[0].CityCode != "MUC" || recents[1].CityCode != "EZE" {
		t.Fatalf("recents = %+v", recents)
	}
	// The unresolved favorite and recent stay in state for a later refresh.
	state := ctrl.store.State()
	if len(state.Favorites) != 5 || len(state.Recents) != 4 {
		t.Fatalf("state refs were rewritten: favorites=%+v recents=%+v", state.Favorites, state.Recents)
	}
}

func TestRecordRecentOrderDedupAndClear(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	countries := fastestCountries(
		fastestSpec{country: "C0", city: "C1", host: "c0.example.net"},
		fastestSpec{country: "C1", city: "C1", host: "c1.example.net"},
		fastestSpec{country: "C2", city: "C1", host: "c2.example.net"},
		fastestSpec{country: "C3", city: "C1", host: "c3.example.net"},
		fastestSpec{country: "C4", city: "C1", host: "c4.example.net"},
		fastestSpec{country: "C5", city: "C1", host: "c5.example.net"},
	)
	ctrl.locations = countries
	candidates := serverlist.Candidates(countries, "", "")
	for _, cand := range candidates {
		ctrl.recordRecent(cand)
	}
	state := ctrl.store.State()
	if len(state.Recents) != maxRecents {
		t.Fatalf("recents = %d, want cap %d: %+v", len(state.Recents), maxRecents, state.Recents)
	}
	if state.Recents[0].Server != "c5.example.net:2499" || state.Recents[4].Server != "c1.example.net:2499" {
		t.Fatalf("recents order = %+v", state.Recents)
	}

	// Re-reporting an older server moves it to the front without duplicating.
	ctrl.recordRecent(candidates[1])
	state = ctrl.store.State()
	if len(state.Recents) != maxRecents || state.Recents[0].Server != "c1.example.net:2499" {
		t.Fatalf("dedupe recents = %+v", state.Recents)
	}
	count := 0
	for _, ref := range state.Recents {
		if ref.Server == "c1.example.net:2499" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("duplicate recent entries = %+v", state.Recents)
	}

	if err := ctrl.ClearRecents(); err != nil {
		t.Fatal(err)
	}
	if got := ctrl.store.State().Recents; got != nil {
		t.Fatalf("recents after clear = %+v", got)
	}
}

func TestRecordRecentDedupesAStaleServerReference(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	countries := []serverlist.Country{{
		Code: "DE", Name: "Germany",
		Cities: []serverlist.City{{
			Code: "FRA", Name: "Frankfurt",
			Servers: []serverlist.Server{{Hostname: "fra2.example.net", Port: 2499}},
		}},
	}}
	ctrl.locations = countries
	if err := ctrl.store.UpdateState(func(s *settings.State) {
		s.Recents = []settings.LocationRef{{CountryCode: "DE", CityCode: "FRA", Server: "fra1.example.net:2499"}}
	}); err != nil {
		t.Fatal(err)
	}
	cand := serverlist.Candidate{CountryCode: "DE", CityCode: "FRA", Host: "fra2.example.net", Port: 2499}
	ctrl.recordRecent(cand)
	if got := ctrl.store.State().Recents; len(got) != 1 || got[0].Server != "fra2.example.net:2499" {
		t.Fatalf("recents = %+v, want the stale ref replaced", got)
	}
}

func TestRecordRecentPersistenceFailureIsNonFatal(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	dir := t.TempDir()
	ro := filepath.Join(dir, "ro")
	if err := os.Mkdir(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
	st, err := settings.Open(settings.Paths{
		Config: filepath.Join(dir, "config.toml"),
		State:  filepath.Join(ro, "state.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctrl.store = st
	ctrl.locations = refFixtureCountries()
	ctrl.recordRecent(serverlist.Candidate{CountryCode: "DE", CityCode: "FRA", Host: "fra1.example.net", Port: 2499})
	if got := ctrl.store.State().Recents; len(got) != 1 {
		t.Fatalf("recordRecent dropped the in-memory update: %+v", got)
	}
}

func TestConnectRecordsSuccessAndSkipsFailure(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctx := context.Background()
	ctrl.LoginEmail("user@example.com")
	if st := ctrl.LoginPassword(ctx, "pw"); st.Step != "done" {
		t.Fatalf("login = %+v", st)
	}
	var quotaCalls atomic.Int32
	ctrl.guardian.BaseURL = guardianPassServer(t, "pass-1", &quotaCalls).URL
	ctrl.serverListURL = serverListServer(t).URL
	ctrl.exitCheckURL = ""
	ctrl.socksPortOverride = freePort(t)

	// A failed edge leaves recents empty.
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		return nil, &upstream.ConnectRejected{Status: 451, Authority: "vpn.mozilla.org:443"}
	}
	if err := ctrl.Connect(ctx, api.ConnectPayload{ProxyOnly: true}); err == nil {
		t.Fatal("expected the edge 451 to fail the connect")
	}
	if got := ctrl.store.State().Recents; got != nil {
		t.Fatalf("failed connect recorded recents: %+v", got)
	}

	// The next successful connect records the actual winner.
	fake := newFakeSession()
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		return fake, nil
	}
	if err := ctrl.Connect(ctx, api.ConnectPayload{ProxyOnly: true}); err != nil {
		t.Fatal(err)
	}
	defer ctrl.stopTunnel("test cleanup")
	state := ctrl.store.State()
	if len(state.Recents) != 1 || state.Recents[0].CountryCode != "DE" || state.Recents[0].Server != "fra1.example.net:2499" {
		t.Fatalf("recents = %+v", state.Recents)
	}
}

func TestLocationRefsNoNetworkWhenCountriesProvided(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = failingServerListServer(t).URL
	countries := fastestCountries(fastestSpec{country: "AA", city: "C1", host: "a.example.net"})
	if got, err := ctrl.Locations(context.Background(), false); err == nil {
		t.Fatalf("test setup expected a failed fetch, got %+v", got)
	}
	// LocationRefs is a pure view over the supplied countries; a broken
	// server-list URL must not matter.
	if favorites, recents := ctrl.LocationRefs(countries); len(favorites) != 0 || len(recents) != 0 {
		t.Fatalf("favorites=%+v recents=%+v", favorites, recents)
	}
}
