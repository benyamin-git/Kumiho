package gui

import (
	"errors"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

func ms(v float64) *float64 { return &v }

func TestLocationsProjectionCarriesPinnedSections(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.locations = api.LocationsResult{
			Countries: []serverlist.Country{{Code: "DE", Name: "Germany"}},
			Favorites: []api.LocationRef{
				{CountryCode: "DE", CityCode: "ber", Server: "de1.example:443"},
			},
			Recents: []api.LocationRef{
				{CountryCode: "DE", CityCode: "fra", Server: "de3.example:443"},
			},
			Latency: map[string]api.CachedPing{
				"de1.example:443": {RTTMS: ms(12.4), MeasuredAt: time.Now()},
			},
			AllowLocked: true,
		}
	})
	ctrl := NewController(f, quietOptions())
	startPoll(t, ctrl)
	waitFor(t, "locations projection", func() bool { return ctrl.Snapshot().LocationsLoaded })

	st := ctrl.Snapshot()
	if len(st.Favorites) != 1 || st.Favorites[0].Server != "de1.example:443" {
		t.Fatalf("favorites = %+v", st.Favorites)
	}
	if len(st.Recents) != 1 || st.Recents[0].Server != "de3.example:443" {
		t.Fatalf("recents = %+v", st.Recents)
	}
	if !st.AllowLocked {
		t.Fatal("allow_locked was not projected")
	}
	sample := st.Latency["de1.example:443"]
	if sample.RTTMS == nil || *sample.RTTMS != 12.4 {
		t.Fatalf("latency = %+v", sample)
	}
	// Snapshots are detached from the controller's live maps.
	*st.Latency["de1.example:443"].RTTMS = 99
	if got := ctrl.Snapshot().Latency["de1.example:443"].RTTMS; got == nil || *got != 12.4 {
		t.Fatalf("snapshot aliased the controller latency: %v", got)
	}
}

func TestFavoriteLocationAndClearRecentsActions(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.locations = api.LocationsResult{Countries: []serverlist.Country{{Code: "DE", Name: "Germany"}}}
	})
	ctrl := NewController(f, quietOptions())
	startPoll(t, ctrl)
	waitFor(t, "initial locations load", func() bool { return ctrl.Snapshot().LocationsLoaded })

	payload := []byte(`{"country_code":"DE","city_code":"ber","server":"de1.example:443","favorite":true}`)
	if err := ctrl.Dispatch(Action{Name: ActionFavoriteLocation, Payload: payload}); err != nil {
		t.Fatalf("Dispatch(favorite_location) = %v", err)
	}
	if got := f.favorites; len(got) != 1 || got[0] != (api.FavoriteLocationPayload{
		CountryCode: "DE", CityCode: "ber", Server: "de1.example:443", Favorite: true,
	}) {
		t.Fatalf("favorite payloads = %+v", got)
	}
	if n := ctrl.Snapshot().LocationsNotice; n == nil || n.Message != "Favorite saved" || n.Tone != toneGood {
		t.Fatalf("favorite notice = %+v", n)
	}
	waitFor(t, "favorite reload", func() bool { return f.count(func(d *fakeDaemon) int { return d.locationCalls }) >= 2 })

	if err := ctrl.Dispatch(Action{Name: ActionClearRecents}); err != nil {
		t.Fatalf("Dispatch(clear_recents) = %v", err)
	}
	if f.count(func(d *fakeDaemon) int { return d.recentClears }) != 1 {
		t.Fatal("clear_recents was not called")
	}
	if n := ctrl.Snapshot().LocationsNotice; n == nil || n.Message != "Recent locations cleared" {
		t.Fatalf("clear notice = %+v", n)
	}
	waitFor(t, "clear reload", func() bool { return f.count(func(d *fakeDaemon) int { return d.locationCalls }) >= 3 })
}

func TestFavoriteLocationErrorNotice(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) { d.favoriteErr = errors.New("bad request") })
	ctrl := NewController(f, Options{})
	err := ctrl.Dispatch(Action{Name: ActionFavoriteLocation, Payload: []byte(`{"country_code":"DE","server":"de1:443"}`)})
	if !errors.Is(err, f.favoriteErr) {
		t.Fatalf("Dispatch(favorite_location) = %v", err)
	}
	if n := ctrl.Snapshot().LocationsNotice; n == nil || n.Tone != toneBad {
		t.Fatalf("notice = %+v", n)
	}
	if f.count(func(d *fakeDaemon) int { return d.locationCalls }) != 0 {
		t.Fatal("a failed favorite reloaded locations")
	}
}

func TestRelayTestAction(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.relayResult = api.RelayTestResult{OK: true, Status: 200, LatencyMS: ms(7.6)}
	})
	ctrl := NewController(f, Options{})
	if err := ctrl.Dispatch(Action{Name: ActionRelayTest}); err != nil {
		t.Fatalf("Dispatch(relay_test) = %v", err)
	}
	st := ctrl.Snapshot()
	if st.RelayTest == nil || !st.RelayTest.OK || st.RelayTest.LatencyMS == nil || *st.RelayTest.LatencyMS != 7.6 {
		t.Fatalf("relay test = %+v", st.RelayTest)
	}
	if n := st.SettingsNotice; n == nil || n.Message != "Relay OK — 8 ms" || n.Tone != toneGood {
		t.Fatalf("settings notice = %+v", n)
	}

	f.set(func(d *fakeDaemon) { d.relayResult = api.RelayTestResult{Status: 401} })
	if err := ctrl.Dispatch(Action{Name: ActionRelayTest}); err != nil {
		t.Fatalf("Dispatch(relay_test rejected) = %v", err)
	}
	if n := ctrl.Snapshot().SettingsNotice; n == nil || n.Message != "Relay rejected the token" || n.Tone != toneBad {
		t.Fatalf("rejected notice = %+v", n)
	}

	f.set(func(d *fakeDaemon) { d.relayErr = errors.New("pipe closed") })
	if err := ctrl.Dispatch(Action{Name: ActionRelayTest}); err == nil {
		t.Fatal("Dispatch(relay_test error) = nil")
	}
	if n := ctrl.Snapshot().SettingsNotice; n == nil || n.Message != "Relay unreachable: pipe closed" {
		t.Fatalf("transport notice = %+v", n)
	}
}

func TestPingBusyLifecycleAndDoneSequence(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.locations = api.LocationsResult{Countries: []serverlist.Country{{Code: "DE", Name: "Germany"}}}
	})
	ctrl := NewController(f, quietOptions())
	startPoll(t, ctrl)
	waitFor(t, "initial locations load", func() bool { return ctrl.Snapshot().LocationsLoaded })

	if err := ctrl.Dispatch(Action{Name: ActionPing, Payload: []byte(`{"hosts":["a:1","b:2"]}`)}); err != nil {
		t.Fatalf("Dispatch(ping) = %v", err)
	}
	if !ctrl.Snapshot().PingBusy {
		t.Fatal("ping_busy is false while the run is open")
	}
	if seq := ctrl.Snapshot().PingDoneSeq; seq != 0 {
		t.Fatalf("ping_done_seq = %d before done", seq)
	}
	f.emit(t, ipc.TypePingResult, api.PingResult{Host: "a:1", RTTMS: ms(42)})
	waitFor(t, "ping result", func() bool { return ctrl.Snapshot().RTTs["a:1"] == "42 ms" })
	if !ctrl.Snapshot().PingBusy {
		t.Fatal("a ping result ended the run")
	}
	f.emit(t, ipc.TypePingDone, api.PingDone{Total: 2, Answered: 1})
	waitFor(t, "ping done", func() bool {
		st := ctrl.Snapshot()
		return !st.PingBusy && st.PingDoneSeq == 1
	})
	waitFor(t, "post-ping locations reload", func() bool {
		return f.count(func(d *fakeDaemon) int { return d.locationCalls }) >= 2
	})
	flags := f.locationRefreshFlags()
	if flags[len(flags)-1] {
		t.Fatal("ping_done reloaded the server list with refresh=true")
	}
}

func TestPingCallErrorClearsBusy(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) { d.pingErr = errors.New("no route") })
	ctrl := NewController(f, Options{})
	if err := ctrl.Dispatch(Action{Name: ActionPing, Payload: []byte(`{"hosts":["a:1"]}`)}); err == nil {
		t.Fatal("Dispatch(ping) = nil, want error")
	}
	if ctrl.Snapshot().PingBusy {
		t.Fatal("a failed ping call left ping_busy set")
	}
}

func TestAllowLockedSettingReloadsLocations(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) {
		d.locations = api.LocationsResult{Countries: []serverlist.Country{{Code: "DE", Name: "Germany"}}}
	})
	ctrl := NewController(f, quietOptions())
	startPoll(t, ctrl)
	waitFor(t, "initial locations load", func() bool { return ctrl.Snapshot().LocationsLoaded })

	if err := ctrl.Dispatch(Action{Name: ActionSettingsSet, Payload: []byte(`{"key":"allow_locked","value":"on"}`)}); err != nil {
		t.Fatalf("Dispatch(settings_set allow_locked) = %v", err)
	}
	waitFor(t, "allow_locked reload", func() bool {
		return f.count(func(d *fakeDaemon) int { return d.locationCalls }) >= 2
	})
	flags := f.locationRefreshFlags()
	if flags[len(flags)-1] {
		t.Fatal("allow_locked reloaded with network refresh")
	}
	if got := f.setPayloads(); len(got) != 1 || got[0].Key != "allow_locked" {
		t.Fatalf("settings payloads = %+v", got)
	}
}

func TestRelaySettingsClearProbeAndReloadSettings(t *testing.T) {
	f := newFakeDaemon()
	f.set(func(d *fakeDaemon) { d.relayResult = api.RelayTestResult{OK: true} })
	ctrl := NewController(f, Options{})
	if err := ctrl.Dispatch(Action{Name: ActionRelayTest}); err != nil {
		t.Fatal(err)
	}
	if ctrl.Snapshot().RelayTest == nil {
		t.Fatal("relay test result missing")
	}
	before := f.count(func(d *fakeDaemon) int { return d.settingsCalls })
	if err := ctrl.Dispatch(Action{Name: ActionSettingsSet, Payload: []byte(`{"key":"relay_url","value":"https://relay.example.test"}`)}); err != nil {
		t.Fatalf("Dispatch(settings_set relay_url) = %v", err)
	}
	if ctrl.Snapshot().RelayTest != nil {
		t.Fatal("relay test result was not cleared by a relay setting change")
	}
	if got := f.count(func(d *fakeDaemon) int { return d.settingsCalls }); got <= before {
		t.Fatalf("settings reloads = %d, want more than %d", got, before)
	}
}

func TestRelayTestNoticeFormatting(t *testing.T) {
	tests := []struct {
		name string
		res  api.RelayTestResult
		want string
	}{
		{"ok", api.RelayTestResult{OK: true, LatencyMS: ms(12.4)}, "Relay OK — 12 ms"},
		{"ok no latency", api.RelayTestResult{OK: true}, "Relay OK"},
		{"forbidden", api.RelayTestResult{Status: 403}, "Relay rejected the token"},
		{"error", api.RelayTestResult{Error: "dial refused"}, "Relay unreachable: dial refused"},
		{"status", api.RelayTestResult{Status: 500}, "Relay unreachable: HTTP 500"},
		{"empty", api.RelayTestResult{}, "Relay unreachable"},
	}
	for _, tc := range tests {
		if got := relayTestNotice(tc.res); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestFavoriteActionRejectsUnknownFields(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, Options{})
	if err := ctrl.Dispatch(Action{Name: ActionFavoriteLocation, Payload: []byte(`{"country_code":"DE","server":"x","extra":true}`)}); err == nil {
		t.Fatal("unknown favorite payload field was accepted")
	}
	if f.count(func(d *fakeDaemon) int { return len(d.favorites) }) != 0 {
		t.Fatal("favorite called after a decode error")
	}
}

func TestPingResultUpdatesAreDetached(t *testing.T) {
	f := newFakeDaemon()
	ctrl := NewController(f, quietOptions())
	startPoll(t, ctrl)
	f.emit(t, ipc.TypePingResult, api.PingResult{Host: "a:1", RTTMS: ms(42)})
	waitFor(t, "ping result", func() bool { return ctrl.Snapshot().RTTs["a:1"] == "42 ms" })
	st := ctrl.Snapshot()
	st.RTTs["a:1"] = "mutated"
	if got := ctrl.Snapshot().RTTs["a:1"]; got != "42 ms" {
		t.Fatalf("snapshot aliased RTTs: %q", got)
	}
}
