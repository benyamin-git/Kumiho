package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	fi, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("cache mode = %o, want 600", perm)
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
