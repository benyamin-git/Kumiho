package relay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeTransport struct {
	calls []*http.Request
	fn    func(*http.Request) (*http.Response, error)
}

func (f *fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.calls = append(f.calls, req)
	return f.fn(req)
}

type closeRecorder struct {
	io.Reader
	closed bool
}

func (c *closeRecorder) Close() error {
	c.closed = true
	return nil
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       &closeRecorder{Reader: strings.NewReader(body)},
	}
}

func request(t *testing.T, method, target string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func newTestTransport(base *fakeTransport, cfg Config) *Transport {
	return NewTransport(base, func() Config { return cfg }, nil)
}

func TestTransportPassThrough(t *testing.T) {
	base := &fakeTransport{fn: func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, "ok"), nil
	}}
	tr := newTestTransport(base, Config{URL: "https://relay.example", Token: "tok"})
	req := request(t, http.MethodGet, "https://api.accounts.firefox.com/v1/x", nil)
	req.Header.Set("X-Test", "yes")

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Fatalf("body = %q, want ok", body)
	}
	if len(base.calls) != 1 {
		t.Fatalf("base calls = %d, want 1", len(base.calls))
	}
	if got := base.calls[0].URL.String(); got != req.URL.String() {
		t.Fatalf("base URL = %q, want %q", got, req.URL.String())
	}
	if got := base.calls[0].Header.Get("X-Test"); got != "yes" {
		t.Fatalf("base header X-Test = %q, want yes", got)
	}
}

func TestTransportRetries451ThroughRelay(t *testing.T) {
	blocked := &closeRecorder{Reader: strings.NewReader("blocked")}
	var relayReq *http.Request
	var relayBody string
	base := &fakeTransport{}
	base.fn = func(req *http.Request) (*http.Response, error) {
		if len(base.calls) == 1 {
			return &http.Response{StatusCode: 451, Header: make(http.Header), Body: blocked}, nil
		}
		relayReq = req
		body, _ := io.ReadAll(req.Body)
		relayBody = string(body)
		return response(http.StatusOK, "ok"), nil
	}
	tr := newTestTransport(base, Config{URL: "https://relay.example/", Token: "tok"})
	req := request(t, http.MethodPost, "https://api.accounts.firefox.com/v1/x?q=1", strings.NewReader("payload"))
	req.Header.Set("Authorization", "Bearer abc")
	req.Header.Set(tokenHeader, "old")

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !blocked.closed {
		t.Fatal("first response body was not closed")
	}
	if len(base.calls) != 2 {
		t.Fatalf("base calls = %d, want 2", len(base.calls))
	}
	if relayReq == nil {
		t.Fatal("relay request was not made")
	}
	wantURL := "https://relay.example/api.accounts.firefox.com/v1/x?q=1"
	if got := relayReq.URL.String(); got != wantURL {
		t.Fatalf("relay URL = %q, want %q", got, wantURL)
	}
	if relayReq.Method != http.MethodPost {
		t.Fatalf("relay method = %s, want POST", relayReq.Method)
	}
	if got := relayReq.Header.Get(tokenHeader); got != "tok" {
		t.Fatalf("relay token = %q, want tok", got)
	}
	if got := relayReq.Header.Get("Authorization"); got != "Bearer abc" {
		t.Fatalf("relay authorization = %q, want Bearer abc", got)
	}
	if relayBody != "payload" {
		t.Fatalf("relay body = %q, want payload", relayBody)
	}
	if relayReq.ContentLength != int64(len("payload")) {
		t.Fatalf("relay content length = %d, want %d", relayReq.ContentLength, len("payload"))
	}
}

func TestTransportRelayAlso451(t *testing.T) {
	first := &closeRecorder{Reader: strings.NewReader("first")}
	second := &closeRecorder{Reader: strings.NewReader("second")}
	base := &fakeTransport{}
	base.fn = func(*http.Request) (*http.Response, error) {
		if len(base.calls) == 1 {
			return &http.Response{StatusCode: 451, Header: make(http.Header), Body: first}, nil
		}
		return &http.Response{StatusCode: 451, Header: make(http.Header), Body: second}, nil
	}
	tr := newTestTransport(base, Config{URL: "https://relay.example", Token: "tok"})
	req := request(t, http.MethodGet, "https://api.accounts.firefox.com/v1/x", nil)

	_, err := tr.RoundTrip(req)
	var be *BlockedError
	if !errors.As(err, &be) {
		t.Fatalf("error = %T %v, want *BlockedError", err, err)
	}
	if be.Status != 451 || !be.RelayConfigured || !be.RelayTried || be.RelayErr != nil {
		t.Fatalf("BlockedError = %+v", be)
	}
	if !first.closed || !second.closed {
		t.Fatalf("bodies closed = %v/%v, want true/true", first.closed, second.closed)
	}
	if len(base.calls) != 2 {
		t.Fatalf("base calls = %d, want 2", len(base.calls))
	}
}

func TestTransportNoRelay(t *testing.T) {
	body := &closeRecorder{Reader: strings.NewReader("blocked")}
	base := &fakeTransport{fn: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 451, Header: make(http.Header), Body: body}, nil
	}}
	tr := newTestTransport(base, Config{})
	req := request(t, http.MethodGet, "https://api.accounts.firefox.com/v1/x", nil)

	_, err := tr.RoundTrip(req)
	var be *BlockedError
	if !errors.As(err, &be) {
		t.Fatalf("error = %T %v, want *BlockedError", err, err)
	}
	if be.Status != 451 || be.RelayConfigured || be.RelayTried {
		t.Fatalf("BlockedError = %+v", be)
	}
	if !body.closed {
		t.Fatal("first response body was not closed")
	}
	if len(base.calls) != 1 {
		t.Fatalf("base calls = %d, want 1", len(base.calls))
	}
}

func TestTransportRelayError(t *testing.T) {
	first := &closeRecorder{Reader: strings.NewReader("blocked")}
	boom := errors.New("boom")
	base := &fakeTransport{}
	base.fn = func(*http.Request) (*http.Response, error) {
		if len(base.calls) == 1 {
			return &http.Response{StatusCode: 451, Header: make(http.Header), Body: first}, nil
		}
		return nil, boom
	}
	tr := newTestTransport(base, Config{URL: "https://relay.example", Token: "tok"})
	req := request(t, http.MethodGet, "https://api.accounts.firefox.com/v1/x", nil)

	_, err := tr.RoundTrip(req)
	var be *BlockedError
	if !errors.As(err, &be) {
		t.Fatalf("error = %T %v, want *BlockedError", err, err)
	}
	if be.Status != 451 || !be.RelayConfigured || !be.RelayTried {
		t.Fatalf("BlockedError = %+v", be)
	}
	if !errors.Is(be.RelayErr, boom) {
		t.Fatalf("RelayErr = %v, want boom", be.RelayErr)
	}
	if !first.closed {
		t.Fatal("first response body was not closed")
	}
	if len(base.calls) != 2 {
		t.Fatalf("base calls = %d, want 2", len(base.calls))
	}
}

func TestTransportNonAllowlisted(t *testing.T) {
	body := &closeRecorder{Reader: strings.NewReader("blocked")}
	base := &fakeTransport{fn: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 451, Header: make(http.Header), Body: body}, nil
	}}
	tr := newTestTransport(base, Config{URL: "https://relay.example", Token: "tok"})
	req := request(t, http.MethodGet, "https://example.com/v1/x", nil)

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 451 {
		t.Fatalf("status = %d, want 451", resp.StatusCode)
	}
	if body.closed {
		t.Fatal("original response body was closed")
	}
	if len(base.calls) != 1 {
		t.Fatalf("base calls = %d, want 1", len(base.calls))
	}
	_ = resp.Body.Close()
}

func TestTransportNonReplayableBody(t *testing.T) {
	body := &closeRecorder{Reader: strings.NewReader("blocked")}
	base := &fakeTransport{fn: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 451, Header: make(http.Header), Body: body}, nil
	}}
	tr := newTestTransport(base, Config{URL: "https://relay.example", Token: "tok"})
	req := request(t, http.MethodPost, "https://api.accounts.firefox.com/v1/x", io.NopCloser(strings.NewReader("payload")))
	req.GetBody = nil

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 451 {
		t.Fatalf("status = %d, want 451", resp.StatusCode)
	}
	if body.closed {
		t.Fatal("original response body was closed")
	}
	if len(base.calls) != 1 {
		t.Fatalf("base calls = %d, want 1", len(base.calls))
	}
	_ = resp.Body.Close()
}

func TestTransportDoesNotRelayToRelayHost(t *testing.T) {
	body := &closeRecorder{Reader: strings.NewReader("blocked")}
	base := &fakeTransport{fn: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 451, Header: make(http.Header), Body: body}, nil
	}}
	tr := newTestTransport(base, Config{URL: "https://relay.example", Token: "tok"})
	req := request(t, http.MethodGet, "https://relay.example/foo", nil)

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 451 {
		t.Fatalf("status = %d, want 451", resp.StatusCode)
	}
	if len(base.calls) != 1 {
		t.Fatalf("base calls = %d, want 1", len(base.calls))
	}
	_ = resp.Body.Close()
}

func TestTransportEmptyToken(t *testing.T) {
	var relayReq *http.Request
	base := &fakeTransport{}
	base.fn = func(req *http.Request) (*http.Response, error) {
		if len(base.calls) == 1 {
			return &http.Response{StatusCode: 451, Body: io.NopCloser(strings.NewReader("blocked"))}, nil
		}
		relayReq = req
		return response(http.StatusOK, "ok"), nil
	}
	tr := newTestTransport(base, Config{URL: "https://relay.example"})
	req := request(t, http.MethodGet, "https://api.accounts.firefox.com/v1/x", nil)
	req.Header.Set(tokenHeader, "old")

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if relayReq == nil {
		t.Fatal("relay request was not made")
	}
	if got := relayReq.Header.Get(tokenHeader); got != "" {
		t.Fatalf("relay token = %q, want empty", got)
	}
}

func TestTransportInvalidRelayURL(t *testing.T) {
	body := &closeRecorder{Reader: strings.NewReader("blocked")}
	base := &fakeTransport{fn: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 451, Header: make(http.Header), Body: body}, nil
	}}
	tr := newTestTransport(base, Config{URL: "http://relay.example", Token: "tok"})
	req := request(t, http.MethodGet, "https://api.accounts.firefox.com/v1/x", nil)

	_, err := tr.RoundTrip(req)
	var be *BlockedError
	if !errors.As(err, &be) {
		t.Fatalf("error = %T %v, want *BlockedError", err, err)
	}
	if !be.RelayConfigured || be.RelayTried || be.RelayErr == nil {
		t.Fatalf("BlockedError = %+v", be)
	}
	if !body.closed {
		t.Fatal("first response body was not closed")
	}
	if len(base.calls) != 1 {
		t.Fatalf("base calls = %d, want 1", len(base.calls))
	}
}

func TestTransportContextCanceledRelayError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	base := &fakeTransport{}
	base.fn = func(*http.Request) (*http.Response, error) {
		if len(base.calls) == 1 {
			return &http.Response{StatusCode: 451, Body: io.NopCloser(strings.NewReader("blocked"))}, nil
		}
		cancel()
		return nil, context.Canceled
	}
	tr := newTestTransport(base, Config{URL: "https://relay.example", Token: "tok"})
	req := request(t, http.MethodGet, "https://api.accounts.firefox.com/v1/x", nil)
	req = req.WithContext(ctx)

	_, err := tr.RoundTrip(req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"empty", "", "", false},
		{"spaces", "  ", "", false},
		{"origin", "https://relay.example", "https://relay.example", false},
		{"trailing slash", "https://relay.example/", "https://relay.example", false},
		{"port", "https://relay.example:8443", "https://relay.example:8443", false},
		{"port and slash", "https://relay.example:443/", "https://relay.example:443", false},
		{"http", "http://relay.example", "", true},
		{"ftp", "ftp://relay.example", "", true},
		{"userinfo", "https://user:pass@relay.example", "", true},
		{"path", "https://relay.example/api", "", true},
		{"double slash", "https://relay.example//", "", true},
		{"query", "https://relay.example?x=1", "", true},
		{"force query", "https://relay.example?", "", true},
		{"fragment", "https://relay.example#x", "", true},
		{"no host", "https://", "", true},
		{"empty host path", "https:///path", "", true},
		{"bad port", "https://relay.example:abc", "", true},
		{"zero port", "https://relay.example:0", "", true},
		{"high port", "https://relay.example:65536", "", true},
		{"empty port", "https://relay.example:", "", true},
		{"no host port", "https://:443", "", true},
		{"no scheme", "relay.example", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateURL(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidateURL(%q) = %q, nil; want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateURL(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ValidateURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestAllowedHost(t *testing.T) {
	for _, host := range []string{
		"api.accounts.firefox.com",
		"oauth.accounts.firefox.com",
		"profile.accounts.firefox.com",
		"vpn.mozilla.org",
		"firefox.settings.services.mozilla.com",
		"API.ACCOUNTS.FIREFOX.COM",
	} {
		if !allowedHost(host) {
			t.Errorf("allowedHost(%q) = false, want true", host)
		}
	}
	if allowedHost("example.com") {
		t.Error("allowedHost(example.com) = true, want false")
	}
}

func TestBlockedError(t *testing.T) {
	boom := errors.New("boom")
	be := &BlockedError{Status: 451, RelayConfigured: true, RelayTried: true, RelayErr: boom}
	if !strings.Contains(be.Error(), "boom") {
		t.Fatalf("Error() = %q, want relay error", be.Error())
	}
	if !errors.Is(be, boom) {
		t.Fatal("Unwrap does not expose RelayErr")
	}
	if got := (&BlockedError{Status: 451}).Error(); !strings.Contains(got, "no relay") {
		t.Fatalf("Error() = %q, want no-relay copy", got)
	}
}

func TestTestSuccess(t *testing.T) {
	var gotPath, gotQuery, gotToken string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotToken = r.Header.Get(tokenHeader)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	res, err := Test(context.Background(), srv.Client(), Config{URL: srv.URL, Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Status)
	}
	if res.Latency <= 0 {
		t.Fatalf("latency = %v, want a positive measurement", res.Latency)
	}
	wantPath := "/firefox.settings.services.mozilla.com/v1/buckets/main/collections/vpn-serverlist/records"
	if gotPath != wantPath {
		t.Fatalf("path = %q, want %q", gotPath, wantPath)
	}
	if gotQuery != "limit=1" {
		t.Fatalf("query = %q, want limit=1", gotQuery)
	}
	if gotToken != "tok" {
		t.Fatalf("token = %q, want tok", gotToken)
	}
}

func TestTestFailures(t *testing.T) {
	for _, status := range []int{http.StatusUnavailableForLegalReasons, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()

			res, err := Test(context.Background(), srv.Client(), Config{URL: srv.URL})
			if err == nil {
				t.Fatal("Test() error = nil, want non-nil")
			}
			if res.Status != status {
				t.Fatalf("status = %d, want %d", res.Status, status)
			}
		})
	}
}

func TestTestDoesNotFollowRedirects(t *testing.T) {
	var hits int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path == "/else" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/else", http.StatusFound)
	}))
	defer srv.Close()

	res, err := Test(context.Background(), srv.Client(), Config{URL: srv.URL})
	if err == nil {
		t.Fatal("Test() error = nil, want non-nil")
	}
	if res.Status != http.StatusFound {
		t.Fatalf("status = %d, want 302", res.Status)
	}
	if hits != 1 {
		t.Fatalf("server hits = %d, want 1 (redirect followed)", hits)
	}
}

func TestTestRequiresRelayURL(t *testing.T) {
	if _, err := Test(context.Background(), http.DefaultClient, Config{}); err == nil {
		t.Fatal("Test() error = nil, want unset-relay error")
	}
	if _, err := Test(context.Background(), nil, Config{URL: "https://relay.example"}); err == nil {
		t.Fatal("Test() with nil client error = nil, want error")
	}
}
