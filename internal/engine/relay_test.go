package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/guardian"
	"github.com/benyamin-git/kumiho/internal/relay"
	"github.com/benyamin-git/kumiho/internal/serverlist"
	"github.com/benyamin-git/kumiho/internal/upstream"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func relayTestResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestRegionBlocked(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {})
	tests := []struct {
		name    string
		err     error
		want    string
		wantNil bool
	}{
		{name: "nil", wantNil: true},
		{name: "no relay", err: &relay.BlockedError{Status: http.StatusUnavailableForLegalReasons}, want: regionBlockedNoRelayMessage},
		{
			name: "relay failed",
			err: &relay.BlockedError{
				Status:          http.StatusUnavailableForLegalReasons,
				RelayConfigured: true,
				RelayTried:      true,
				RelayErr:        errors.New("connection refused"),
			},
			want: "connection refused",
		},
		{
			name: "relay also blocked",
			err: &relay.BlockedError{
				Status:          http.StatusUnavailableForLegalReasons,
				RelayConfigured: true,
				RelayTried:      true,
			},
			want: "also returned HTTP 451",
		},
		{name: "fxa", err: &fxa.APIError{StatusCode: http.StatusUnavailableForLegalReasons}, want: regionBlockedNoRelayMessage},
		{name: "guardian", err: &guardian.HTTPError{Status: http.StatusUnavailableForLegalReasons}, want: regionBlockedNoRelayMessage},
		{name: "server list", err: &serverlist.HTTPError{Status: http.StatusUnavailableForLegalReasons}, want: regionBlockedNoRelayMessage},
		{
			name: "wrapped fxa",
			err:  fmt.Errorf("wrapped: %w", &fxa.APIError{StatusCode: http.StatusUnavailableForLegalReasons}),
			want: regionBlockedNoRelayMessage,
		},
		{name: "other fxa", err: &fxa.APIError{StatusCode: http.StatusInternalServerError}, wantNil: true},
		{name: "canceled", err: context.Canceled, wantNil: true},
		{
			name: "already region blocked",
			err:  &api.RemoteError{Code: api.CodeRegionBlocked, Message: "already blocked"},
			want: "already blocked",
		},
		{name: "other remote error", err: &api.RemoteError{Code: api.CodeInternal, Message: "boom"}, wantNil: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ctrl.regionBlocked(tc.err)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("regionBlocked(%v) = %+v, want nil", tc.err, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("regionBlocked(%v) = nil, want region_blocked", tc.err)
			}
			if got.Code != api.CodeRegionBlocked {
				t.Fatalf("code = %q, want %q", got.Code, api.CodeRegionBlocked)
			}
			if !strings.Contains(got.Message, tc.want) {
				t.Fatalf("message = %q, want it to contain %q", got.Message, tc.want)
			}
		})
	}
}

func TestLocationsRegionBlockedNoRelaySkipsCache(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {})
	ctrl.serverListURL = serverlist.RecordsURL
	calls := 0
	ctrl.httpClient = &http.Client{Transport: relay.NewTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return relayTestResponse(http.StatusUnavailableForLegalReasons, "blocked"), nil
	}), ctrl.relayConfig, nil)}

	// Both caches are populated; a 451 must still be terminal.
	countries, err := serverlist.Parse([]byte(testServerListJSON))
	if err != nil {
		t.Fatal(err)
	}
	ctrl.locations = countries
	ctrl.locationsAt = time.Now().Add(-time.Hour)
	if err := serverlist.SaveCache(ctrl.serverListCache, []byte(testServerListJSON)); err != nil {
		t.Fatal(err)
	}

	got, err := ctrl.Locations(context.Background(), true)
	var re *api.RemoteError
	if !errors.As(err, &re) {
		t.Fatalf("Locations error = %v, want *api.RemoteError", err)
	}
	if re.Code != api.CodeRegionBlocked || re.Message != regionBlockedNoRelayMessage {
		t.Fatalf("region error = %+v", re)
	}
	if got != nil {
		t.Fatalf("Locations returned cached countries on 451: %+v", got)
	}
	if calls != 1 {
		t.Fatalf("requests = %d, want 1", calls)
	}
}

func TestLocationsRegionBlockedDirectHTTPError(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {})
	ctrl.serverListURL = "https://firefox.settings.services.mozilla.com/test"
	ctrl.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return relayTestResponse(http.StatusUnavailableForLegalReasons, "blocked"), nil
	})}
	if _, err := ctrl.Locations(context.Background(), true); err == nil {
		t.Fatal("Locations succeeded on a direct 451")
	} else {
		var re *api.RemoteError
		if !errors.As(err, &re) || re.Code != api.CodeRegionBlocked {
			t.Fatalf("error = %v, want region_blocked", err)
		}
	}
}

func TestLocationsRelaySuccess(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {})
	ctrl.serverListURL = serverlist.RecordsURL
	if err := ctrl.SetSetting("relay_url", "https://relay.example"); err != nil {
		t.Fatal(err)
	}
	const token = "relay-test-token"
	if err := ctrl.SetSetting("relay_token", token); err != nil {
		t.Fatal(err)
	}

	type request struct {
		url   string
		token string
	}
	var mu sync.Mutex
	var requests []request
	ctrl.httpClient = &http.Client{Transport: relay.NewTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		requests = append(requests, request{url: r.URL.String(), token: r.Header.Get("X-Kumiho-Relay-Token")})
		mu.Unlock()
		switch r.URL.Host {
		case "firefox.settings.services.mozilla.com":
			return relayTestResponse(http.StatusUnavailableForLegalReasons, "blocked"), nil
		case "relay.example":
			return relayTestResponse(http.StatusOK, testServerListJSON), nil
		default:
			return relayTestResponse(http.StatusInternalServerError, "unexpected host"), nil
		}
	}), ctrl.relayConfig, nil)}

	got, err := ctrl.Locations(context.Background(), true)
	if err != nil {
		t.Fatalf("Locations: %v", err)
	}
	if len(got) != 1 || got[0].Code != "DE" {
		t.Fatalf("Locations = %+v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	if requests[0].url != serverlist.RecordsURL {
		t.Fatalf("first URL = %q", requests[0].url)
	}
	wantRelayURL := "https://relay.example/firefox.settings.services.mozilla.com/v1/buckets/main/collections/vpn-serverlist/records"
	if requests[1].url != wantRelayURL {
		t.Fatalf("relay URL = %q, want %q", requests[1].url, wantRelayURL)
	}
	if requests[1].token != token {
		t.Fatalf("relay token header = %q", requests[1].token)
	}
}

func TestLocationsRelayFailure(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {})
	ctrl.serverListURL = serverlist.RecordsURL
	if err := ctrl.SetSetting("relay_url", "https://relay.example"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	ctrl.httpClient = &http.Client{Transport: relay.NewTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host == "relay.example" {
			return nil, errors.New("relay unavailable")
		}
		return relayTestResponse(http.StatusUnavailableForLegalReasons, "blocked"), nil
	}), ctrl.relayConfig, nil)}

	_, err := ctrl.Locations(context.Background(), true)
	var re *api.RemoteError
	if !errors.As(err, &re) {
		t.Fatalf("Locations error = %v, want *api.RemoteError", err)
	}
	if re.Code != api.CodeRegionBlocked || !strings.Contains(re.Message, "configured relay failed") ||
		!strings.Contains(re.Message, "relay unavailable") {
		t.Fatalf("region error = %+v", re)
	}
	if calls != 2 {
		t.Fatalf("requests = %d, want 2", calls)
	}
}

func TestConnectRegionBlockedNoRelay(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctx := context.Background()
	if st := ctrl.LoginEmail("user@example.com"); st.Step != "password" {
		t.Fatalf("login step = %+v", st)
	}
	if st := ctrl.LoginPassword(ctx, "pw"); st.Step != "done" {
		t.Fatalf("login = %+v", st)
	}
	countries, err := serverlist.Parse([]byte(testServerListJSON))
	if err != nil {
		t.Fatal(err)
	}
	ctrl.locations = countries
	ctrl.locationsAt = time.Now()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", http.StatusUnavailableForLegalReasons)
	}))
	t.Cleanup(srv.Close)
	ctrl.guardian.BaseURL = srv.URL
	dials := 0
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		dials++
		return newFakeSession(), nil
	}

	err = ctrl.Connect(ctx, api.ConnectPayload{ProxyOnly: true})
	var re *api.RemoteError
	if !errors.As(err, &re) {
		t.Fatalf("Connect error = %v, want *api.RemoteError", err)
	}
	if re.Code != api.CodeRegionBlocked || re.Message != regionBlockedNoRelayMessage {
		t.Fatalf("region error = %+v", re)
	}
	if dials != 0 {
		t.Fatalf("dialed the edge %d times after a 451", dials)
	}
	if got := ctrl.Snapshot().State; got != string(StateIdle) {
		t.Fatalf("state = %q, want IDLE", got)
	}
}

func TestBrowserLoginRegionBlocked(t *testing.T) {
	t.Run("exchange", func(t *testing.T) {
		ctrl, srv := newBrowserTestController(t)
		ls := startBrowserLogin(t, ctrl, "", "")
		state := browserAuthorizationState(t, ls.URL)
		srv.setToken(http.StatusUnavailableForLegalReasons, "blocked")
		if err := ctrl.LoginComplete("", browserRedirectURL(state, testBrowserCode)); err != nil {
			t.Fatalf("LoginComplete: %v", err)
		}
		st := browserStatus(t, ctrl, ls.LoginID)
		if st.Step != "error" || !strings.Contains(st.Error, "This region is blocking") {
			t.Fatalf("login state = %+v", st)
		}
		if calls, _ := srv.tokenSnapshot(); calls != 1 {
			t.Fatalf("token calls = %d, want 1", calls)
		}
		if calls, _ := srv.profileSnapshot(); calls != 0 {
			t.Fatalf("profile calls = %d, want 0", calls)
		}
	})

	t.Run("profile", func(t *testing.T) {
		ctrl, srv := newBrowserTestController(t)
		ls := startBrowserLogin(t, ctrl, "", "")
		state := browserAuthorizationState(t, ls.URL)
		srv.setProfile(http.StatusUnavailableForLegalReasons, "blocked")
		if err := ctrl.LoginComplete("", browserRedirectURL(state, testBrowserCode)); err != nil {
			t.Fatalf("LoginComplete: %v", err)
		}
		st := browserStatus(t, ctrl, ls.LoginID)
		if st.Step != "error" || !strings.Contains(st.Error, "This region is blocking") {
			t.Fatalf("login state = %+v", st)
		}
		if calls, _ := srv.tokenSnapshot(); calls != 1 {
			t.Fatalf("token calls = %d, want 1", calls)
		}
		if calls, _ := srv.profileSnapshot(); calls != 1 {
			t.Fatalf("profile calls = %d, want 1", calls)
		}
	})
}
