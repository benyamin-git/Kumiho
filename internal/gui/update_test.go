package gui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/version"
)

// updateFeedServer serves one canned GitHub release-list reply and records
// what the checker asked for. Every test uses it, so the suite never touches
// the network.
type updateFeedServer struct {
	*httptest.Server
	status int
	body   string
	delay  time.Duration

	method string
	path   string
	query  string
	agent  string
	accept string
}

func newUpdateFeedServer(t *testing.T, status int, body string) *updateFeedServer {
	t.Helper()
	s := &updateFeedServer{status: status, body: body}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.method = r.Method
		s.path = r.URL.Path
		s.query = r.URL.RawQuery
		s.agent = r.Header.Get("User-Agent")
		s.accept = r.Header.Get("Accept")
		if s.delay > 0 {
			select {
			case <-time.After(s.delay):
			case <-r.Context().Done():
			}
		}
		w.WriteHeader(s.status)
		_, _ = w.Write([]byte(s.body))
	}))
	t.Cleanup(s.Close)
	return s
}

// releasesJSON builds the GitHub list reply from one or more release objects.
func releasesJSON(releases ...string) string {
	return "[" + strings.Join(releases, ",") + "]"
}

func TestUpdateCheckerCheck(t *testing.T) {
	const htmlURL = "https://github.com/benyamin-git/Kumiho/releases/tag/v0.4.0"
	srv := newUpdateFeedServer(t, http.StatusOK,
		releasesJSON(`{"tag_name":"v0.4.0","html_url":"`+htmlURL+`","name":"Kumiho v0.4.0"}`))

	before := time.Now().UTC().Add(-time.Second)
	info, err := UpdateChecker{BaseURL: srv.URL, Current: "v0.3.0"}.Check(context.Background())
	if err != nil {
		t.Fatalf("Check = %v, want nil", err)
	}
	if info.Current != "v0.3.0" || info.Latest != "v0.4.0" || info.URL != htmlURL {
		t.Fatalf("info = %+v", info)
	}
	at, err := time.Parse(time.RFC3339, info.CheckedAt)
	if err != nil || at.Before(before) || at.After(time.Now().UTC().Add(time.Second)) {
		t.Fatalf("CheckedAt = %q (%v), want a fresh RFC3339 UTC timestamp", info.CheckedAt, err)
	}
	if srv.method != http.MethodGet {
		t.Errorf("method = %q, want GET", srv.method)
	}
	if srv.path != "/repos/benyamin-git/Kumiho/releases" {
		t.Errorf("path = %q, want the releases list endpoint", srv.path)
	}
	if srv.query != "per_page=10" {
		t.Errorf("query = %q, want per_page=10", srv.query)
	}
	if srv.agent != "kumiho-gui" {
		t.Errorf("User-Agent = %q, want kumiho-gui", srv.agent)
	}
	if srv.accept != "application/vnd.github+json" {
		t.Errorf("Accept = %q, want the GitHub JSON media type", srv.accept)
	}
}

func TestUpdateCheckerSkipsDrafts(t *testing.T) {
	const htmlURL = "https://example.test/r"
	srv := newUpdateFeedServer(t, http.StatusOK, releasesJSON(
		`{"tag_name":"v0.5.0","html_url":"`+htmlURL+`","draft":true}`,
		`{"tag_name":"v0.4.0","html_url":"`+htmlURL+`","prerelease":true}`,
	))
	info, err := UpdateChecker{BaseURL: srv.URL}.Check(context.Background())
	if err != nil {
		t.Fatalf("Check = %v, want nil", err)
	}
	if info.Latest != "v0.4.0" {
		t.Fatalf("Latest = %q, want the first published release v0.4.0", info.Latest)
	}
}

func TestUpdateCheckerCurrentDefaultsToVersion(t *testing.T) {
	srv := newUpdateFeedServer(t, http.StatusOK, releasesJSON(
		`{"tag_name":"v0.9.9","html_url":"https://example.test/r"}`,
	))
	info, err := UpdateChecker{BaseURL: srv.URL}.Check(context.Background())
	if err != nil {
		t.Fatalf("Check = %v, want nil", err)
	}
	if info.Current != version.Version {
		t.Fatalf("Current = %q, want internal/version's %q", info.Current, version.Version)
	}
}

func TestUpdateCheckerErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"status", http.StatusInternalServerError, `[]`, "HTTP 500"},
		{"malformed json", http.StatusOK, `[`, "releases"},
		{"empty list", http.StatusOK, `[]`, "no published release"},
		{"only drafts", http.StatusOK, releasesJSON(`{"tag_name":"v0.4.0","html_url":"https://example.test/r","draft":true}`), "no published release"},
		{"missing tag", http.StatusOK, releasesJSON(`{"html_url":"https://example.test/r"}`), "tag_name"},
		{"missing url", http.StatusOK, releasesJSON(`{"tag_name":"v0.4.0"}`), "html_url"},
		{"unsafe url", http.StatusOK, releasesJSON(`{"tag_name":"v0.4.0","html_url":"file:///etc/passwd"}`), "html_url"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newUpdateFeedServer(t, tc.status, tc.body)
			_, err := UpdateChecker{BaseURL: srv.URL}.Check(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Check = %v, want an error mentioning %q", err, tc.want)
			}
		})
	}
}

func TestUpdateCheckerTimeout(t *testing.T) {
	srv := newUpdateFeedServer(t, http.StatusOK, releasesJSON(
		`{"tag_name":"v0.4.0","html_url":"https://example.test/r"}`,
	))
	srv.delay = 5 * time.Second
	srv.status = http.StatusOK

	start := time.Now()
	_, err := UpdateChecker{BaseURL: srv.URL, Timeout: 50 * time.Millisecond}.Check(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Check = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Check took %v, want the injected 50 ms timeout to apply", elapsed)
	}
}

func TestUpdateCheckerHonorsCallerContext(t *testing.T) {
	srv := newUpdateFeedServer(t, http.StatusOK, releasesJSON(
		`{"tag_name":"v0.4.0","html_url":"https://example.test/r"}`,
	))
	srv.delay = 5 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := UpdateChecker{BaseURL: srv.URL}.Check(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Check = %v, want context.DeadlineExceeded", err)
	}
}

func TestSameRelease(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.3.0", "v0.3.0", true},
		{"v0.3.0", "0.3.0", true},
		{"0.3.0", "V0.3.0", true},
		{" v0.3.0 ", "v0.3.0", true},
		{"v0.3.0", "v0.4.0", false},
		{"dev", "v0.3.0", false},
		{"", "v0.3.0", false},
		{"v0.3.0", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		if got := sameRelease(tc.a, tc.b); got != tc.want {
			t.Errorf("sameRelease(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestUpdateInfoJSONShape pins the wire shape State.Update rides in: the
// shell's State carries the field verbatim, and the web UI reads these names.
func TestUpdateInfoJSONShape(t *testing.T) {
	raw, err := json.Marshal(UpdateInfo{
		Current: "v0.3.0", Latest: "v0.4.0",
		URL: "https://example.test/r", CheckedAt: "2026-10-16T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("Marshal = %v", err)
	}
	want := `{"current":"v0.3.0","latest":"v0.4.0","url":"https://example.test/r","checked_at":"2026-10-16T00:00:00Z"}`
	if string(raw) != want {
		t.Fatalf("UpdateInfo JSON = %s, want %s", raw, want)
	}
	var zero UpdateInfo
	raw, err = json.Marshal(zero)
	if err != nil {
		t.Fatalf("Marshal zero = %v", err)
	}
	if string(raw) != "{}" {
		t.Fatalf("zero UpdateInfo JSON = %s, want {}", raw)
	}
}
