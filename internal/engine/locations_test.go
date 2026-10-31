package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/settings"
)

const testServerListJSON = `{"data":[{"code":"DE","name":"Germany","cities":[{"code":"FRA","name":"Frankfurt","servers":[{"hostname":"fra1.example.net","port":2499}]}]}]}`

// locationsCachePath mirrors the controller's derivation of the disk cache
// path so the tests exercise the real location.
func locationsCachePath(paths settings.Paths) string {
	return filepath.Join(filepath.Dir(paths.State), "serverlist.json")
}

// failingServerListServer returns 500 for every request.
func failingServerListServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func servingServerListServer(t *testing.T, payload string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, payload)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func countingServerListServer(t *testing.T, payload string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, payload)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func engineListRecord(code string, locked bool, expr string) map[string]any {
	record := map[string]any{
		"code": code,
		"name": code,
		"cities": []any{
			map[string]any{
				"code": "C1",
				"name": "City",
				"servers": []any{
					map[string]any{"hostname": strings.ToLower(code) + ".example.net", "port": 2499},
				},
			},
		},
	}
	if locked {
		record["locked"] = true
	}
	if expr != "" {
		record["filter_expression"] = expr
	}
	return record
}

func engineListFixture(records ...map[string]any) string {
	data, err := json.Marshal(map[string]any{"data": records})
	if err != nil {
		panic(err)
	}
	return string(data)
}

func TestLocationsAllowLockedRefiltersFromRaw(t *testing.T) {
	records := make([]map[string]any, 0, 35)
	for i := 0; i < 35; i++ {
		records = append(records, engineListRecord(fmt.Sprintf("C%02d", i), i >= 5, ""))
	}
	payload := engineListFixture(records...)
	srv, hits := countingServerListServer(t, payload)
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = srv.URL

	got, err := ctrl.Locations(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("default visible = %d, want 5", len(got))
	}
	if hits.Load() != 1 {
		t.Fatalf("requests = %d, want 1", hits.Load())
	}

	if err := ctrl.SetSetting("allow_locked", "on"); err != nil {
		t.Fatal(err)
	}
	got, err = ctrl.Locations(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 35 {
		t.Fatalf("allow_locked visible = %d, want 35", len(got))
	}
	locked := 0
	for _, c := range got {
		if c.Locked {
			locked++
		}
	}
	if locked != 30 {
		t.Fatalf("locked records = %d, want 30", locked)
	}
	if hits.Load() != 1 {
		t.Fatalf("allow_locked toggle made a request: %d", hits.Load())
	}

	if err := ctrl.ApplySetting(api.SettingsSetPayload{Key: "allow_locked", Reset: true}); err != nil {
		t.Fatal(err)
	}
	got, err = ctrl.Locations(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("reset visible = %d, want 5", len(got))
	}
	if hits.Load() != 1 {
		t.Fatalf("allow_locked reset made a request: %d", hits.Load())
	}
}

func TestLocationsVersionContextRefiltersFromRaw(t *testing.T) {
	modern := `env.version|versionCompare("151.0a1") >= 0`
	legacy := `env.version|versionCompare("151.0a1") < 0`
	payload := engineListFixture(
		engineListRecord("US", false, modern),
		engineListRecord("US", false, legacy),
	)
	srv, hits := countingServerListServer(t, payload)
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = srv.URL
	ctrl.firefoxVersion = "158.0"

	got, err := ctrl.Locations(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FilterExpression != modern {
		t.Fatalf("158.0 = %+v, want the modern record", got)
	}

	ctrl.firefoxVersion = "150.0"
	got, err = ctrl.Locations(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FilterExpression != legacy {
		t.Fatalf("150.0 = %+v, want the legacy record", got)
	}

	ctrl.firefoxVersion = ""
	got, err = ctrl.Locations(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("empty version = %d records, want both (fail open)", len(got))
	}
	if !logHas(ctrl, "env.version is unavailable") {
		t.Fatalf("missing debug filter log: %+v", ctrl.log.Snapshot())
	}
	if hits.Load() != 1 {
		t.Fatalf("version changes made requests: %d", hits.Load())
	}
}

func TestLocationsCountryContextRefiltersFromRaw(t *testing.T) {
	payload := engineListFixture(
		engineListRecord("US", false, `env.country == "US"`),
		engineListRecord("DE", false, `env.country == "DE"`),
	)
	srv, hits := countingServerListServer(t, payload)
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = srv.URL

	got, err := ctrl.Locations(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("empty country = %d records, want both (fail open)", len(got))
	}
	if !logHas(ctrl, "env.country is unavailable") {
		t.Fatalf("missing debug filter log: %+v", ctrl.log.Snapshot())
	}

	ctrl.mu.Lock()
	ctrl.exitCountry = "US"
	ctrl.mu.Unlock()
	got, err = ctrl.Locations(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Code != "US" {
		t.Fatalf("US country = %+v", got)
	}

	ctrl.mu.Lock()
	ctrl.exitCountry = "DE"
	ctrl.mu.Unlock()
	got, err = ctrl.Locations(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Code != "DE" {
		t.Fatalf("DE country = %+v", got)
	}
	if hits.Load() != 1 {
		t.Fatalf("country changes made requests: %d", hits.Load())
	}
}

func TestLocationsDiskCacheAppliesLockedFilter(t *testing.T) {
	records := make([]map[string]any, 0, 35)
	for i := 0; i < 35; i++ {
		records = append(records, engineListRecord(fmt.Sprintf("C%02d", i), i >= 5, ""))
	}
	payload := engineListFixture(records...)
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = failingServerListServer(t).URL
	if err := os.WriteFile(ctrl.serverListCache, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ctrl.Locations(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("disk fallback visible = %d, want 5", len(got))
	}

	if err := ctrl.SetSetting("allow_locked", "on"); err != nil {
		t.Fatal(err)
	}
	got, err = ctrl.Locations(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 35 {
		t.Fatalf("disk fallback allow_locked = %d, want 35", len(got))
	}
	if _, err := os.Stat(ctrl.serverListCache); err != nil {
		t.Fatalf("disk cache was removed: %v", err)
	}
}

func TestSettingsViewAllowLocked(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	if ctrl.Settings().AllowLocked {
		t.Fatal("allow_locked should default to false")
	}
	if err := ctrl.SetSetting("allow_locked", "on"); err != nil {
		t.Fatal(err)
	}
	if !ctrl.Settings().AllowLocked {
		t.Fatal("SettingsView.AllowLocked not updated")
	}
	if !slices.Contains(ctrl.Settings().Overridden, "allow_locked") {
		t.Fatalf("overridden keys = %v", ctrl.Settings().Overridden)
	}
	if err := ctrl.ApplySetting(api.SettingsSetPayload{Key: "allow_locked", Reset: true}); err != nil {
		t.Fatal(err)
	}
	if ctrl.Settings().AllowLocked {
		t.Fatal("AllowLocked reset did not restore the default")
	}
}

func TestParseFirefoxVersion(t *testing.T) {
	if got := parseFirefoxVersion(fxa.BrowserUserAgent); got != "158.0" {
		t.Fatalf("BrowserUserAgent version = %q, want 158.0", got)
	}
	for _, ua := range []string{"", "Mozilla/5.0", "Firefox/", "Firefox/abc"} {
		if got := parseFirefoxVersion(ua); got != "" {
			t.Fatalf("parseFirefoxVersion(%q) = %q, want empty", ua, got)
		}
	}
	ctrl, _ := newTestController(t, loginOKHandler(t))
	if ctrl.firefoxVersion != "158.0" {
		t.Fatalf("controller firefoxVersion = %q, want 158.0", ctrl.firefoxVersion)
	}
}

func TestLocationsWritesDiskCache(t *testing.T) {
	ctrl, paths := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = servingServerListServer(t, testServerListJSON).URL

	countries, err := ctrl.Locations(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(countries) != 1 || countries[0].Code != "DE" {
		t.Fatalf("countries = %+v", countries)
	}

	want := locationsCachePath(paths)
	if ctrl.serverListCache != want {
		t.Fatalf("cache path = %q, want %q", ctrl.serverListCache, want)
	}
	raw, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("cache not written: %v", err)
	}
	if string(raw) != testServerListJSON {
		t.Fatalf("cache payload = %s, want the raw response %s", raw, testServerListJSON)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(want)
		if err != nil {
			t.Fatal(err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Fatalf("cache mode = %o, want 600", perm)
		}
	}
}

func TestLocationsDoesNotCacheUnparseablePayload(t *testing.T) {
	ctrl, paths := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = servingServerListServer(t, "not json").URL

	if _, err := ctrl.Locations(context.Background(), false); err == nil {
		t.Fatal("unparseable payload must fail the fetch")
	}
	if _, statErr := os.Stat(locationsCachePath(paths)); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("unparseable payload must not be cached: %v", statErr)
	}
}

func TestLocationsFallsBackToDiskCache(t *testing.T) {
	ctrl, paths := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = failingServerListServer(t).URL
	cachePath := locationsCachePath(paths)
	if err := os.WriteFile(cachePath, []byte(testServerListJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	countries, err := ctrl.Locations(context.Background(), false)
	if err != nil {
		t.Fatalf("failed fetch should fall back to the disk cache: %v", err)
	}
	if len(countries) != 1 || countries[0].Code != "DE" {
		t.Fatalf("countries = %+v", countries)
	}
	if !logHas(ctrl, "using cached server list") {
		t.Fatalf("missing fallback log entry: %+v", ctrl.log.Snapshot())
	}
	if ctrl.locations != nil || !ctrl.locationsAt.IsZero() {
		t.Fatal("disk fallback must not populate the in-memory cache")
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("readable cache should be kept: %v", err)
	}
}

func TestLocationsDiscardsCorruptDiskCache(t *testing.T) {
	ctrl, paths := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = failingServerListServer(t).URL
	cachePath := locationsCachePath(paths)
	if err := os.WriteFile(cachePath, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := ctrl.Locations(context.Background(), false)
	if err == nil {
		t.Fatal("corrupt cache must not hide the fetch failure")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("err = %v, want the original fetch error", err)
	}
	if !logHas(ctrl, "unusable cached server list") {
		t.Fatalf("missing corrupt-cache warning: %+v", ctrl.log.Snapshot())
	}
	if logHas(ctrl, "using cached server list") {
		t.Fatal("corrupt cache must not be used")
	}
	if _, statErr := os.Stat(cachePath); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("corrupt cache not removed: %v", statErr)
	}
}

func TestLocationsDiscardsEmptyDiskCache(t *testing.T) {
	ctrl, paths := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = failingServerListServer(t).URL
	cachePath := locationsCachePath(paths)
	if err := os.WriteFile(cachePath, []byte(`{"data":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := ctrl.Locations(context.Background(), false)
	if err == nil {
		t.Fatal("empty cache must not hide the fetch failure")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("err = %v, want the original fetch error", err)
	}
	if !logHas(ctrl, "unusable cached server list") {
		t.Fatalf("missing empty-cache warning: %+v", ctrl.log.Snapshot())
	}
	if _, statErr := os.Stat(cachePath); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("empty cache not removed: %v", statErr)
	}
}

func TestLocationsWithoutCachePreservesFetchError(t *testing.T) {
	ctrl, paths := newTestController(t, loginOKHandler(t))
	ctrl.serverListURL = failingServerListServer(t).URL

	_, err := ctrl.Locations(context.Background(), false)
	if err == nil {
		t.Fatal("failed fetch without a cache must return an error")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("err = %v, want the original fetch error", err)
	}
	if _, statErr := os.Stat(locationsCachePath(paths)); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("no cache should be created on failure: %v", statErr)
	}
	if logHas(ctrl, "using cached server list") {
		t.Fatal("no cache should be used")
	}
}
