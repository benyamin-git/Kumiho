// Package relay retries HTTP 451 control-plane requests through a
// user-configured relay (PLAN.md §2.1).
package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const tokenHeader = "X-Kumiho-Relay-Token"

// allowedTargets is the client-side copy of the Worker allowlist (D9).
var allowedTargets = map[string]bool{
	"api.accounts.firefox.com":              true,
	"oauth.accounts.firefox.com":            true,
	"profile.accounts.firefox.com":          true,
	"vpn.mozilla.org":                       true,
	"firefox.settings.services.mozilla.com": true,
}

// Config is the relay endpoint and its optional shared secret.
type Config struct {
	URL   string
	Token string
}

// ValidateURL accepts only an HTTPS origin: no userinfo, path, query or
// fragment. A trailing slash is normalized away. Empty means no relay.
func ValidateURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("relay URL is invalid")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return "", errors.New("relay URL must use https")
	}
	if u.Host == "" || u.Hostname() == "" {
		return "", errors.New("relay URL has no host")
	}
	if u.User != nil {
		return "", errors.New("relay URL must not contain userinfo")
	}
	if u.Path != "" && u.Path != "/" {
		return "", errors.New("relay URL must be an origin without a path")
	}
	if u.RawQuery != "" || u.ForceQuery {
		return "", errors.New("relay URL must not contain a query")
	}
	if u.Fragment != "" {
		return "", errors.New("relay URL must not contain a fragment")
	}
	if strings.HasSuffix(u.Host, ":") {
		return "", errors.New("relay URL has an empty port")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", errors.New("relay URL has an invalid port")
		}
	}
	u.Scheme = "https"
	u.Path = ""
	u.RawPath = ""
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String(), nil
}

// BlockedError reports a terminal HTTP 451 after the one permitted relay
// attempt. RelayErr is set when the attempt itself failed.
type BlockedError struct {
	Status          int
	RelayConfigured bool
	RelayTried      bool
	RelayErr        error
}

func (e *BlockedError) Error() string {
	switch {
	case !e.RelayConfigured:
		return fmt.Sprintf("HTTP %d blocked with no relay configured", e.Status)
	case e.RelayErr != nil:
		return fmt.Sprintf("HTTP %d blocked and the relay failed: %v", e.Status, e.RelayErr)
	default:
		return fmt.Sprintf("HTTP %d blocked through the relay", e.Status)
	}
}

func (e *BlockedError) Unwrap() error { return e.RelayErr }

// Transport is an http.RoundTripper that retries one 451 through the relay.
type Transport struct {
	base  http.RoundTripper
	cfg   func() Config
	logFn func(string, ...any)
	onUse func()
}

// NewTransport wraps base. cfg is read on every request so a settings change
// applies without rebuilding clients. logf may be nil.
func NewTransport(base http.RoundTripper, cfg func() Config, logf func(string, ...any)) *Transport {
	if base == nil {
		base = http.DefaultTransport
	}
	if cfg == nil {
		cfg = func() Config { return Config{} }
	}
	return &Transport{base: base, cfg: cfg, logFn: logf}
}

// Unwrap exposes the wrapped transport for callers that need the underlying
// dialer (tests and transport inspection).
func (t *Transport) Unwrap() http.RoundTripper { return t.base }

// SetUseHook installs a callback fired whenever a request is actually
// retried through the relay. The engine uses it to report relay_used without
// inspecting request paths.
func (t *Transport) SetUseHook(fn func()) { t.onUse = fn }

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp == nil || resp.StatusCode != http.StatusUnavailableForLegalReasons {
		return resp, err
	}

	cfg := t.cfg()
	origin, verr := ValidateURL(cfg.URL)
	if verr != nil {
		discardBody(resp.Body)
		return nil, &BlockedError{
			Status:          resp.StatusCode,
			RelayConfigured: true,
			RelayErr:        verr,
		}
	}
	if origin == "" {
		discardBody(resp.Body)
		return nil, &BlockedError{Status: resp.StatusCode}
	}
	if isRelayHost(req.URL, origin) {
		return resp, nil
	}
	if req.URL == nil || !allowedHost(req.URL.Hostname()) {
		return resp, nil
	}

	out := req.Clone(req.Context())
	if req.Body != nil && req.Body != http.NoBody {
		if req.GetBody == nil {
			return resp, nil
		}
		body, berr := req.GetBody()
		if berr != nil || body == nil {
			if body != nil {
				_ = body.Close()
			}
			return resp, nil
		}
		out.Body = body
	}
	target, uerr := relayURL(origin, req.URL)
	if uerr != nil {
		return resp, nil
	}
	out.URL = target
	out.Host = ""
	if out.Header == nil {
		out.Header = make(http.Header)
	}
	if cfg.Token != "" {
		out.Header.Set(tokenHeader, cfg.Token)
	} else {
		out.Header.Del(tokenHeader)
	}

	discardBody(resp.Body)
	t.logf("HTTP 451 from %s; retrying through the relay", req.URL.Host)
	if t.onUse != nil {
		t.onUse()
	}
	relayResp, relayErr := t.base.RoundTrip(out)
	if relayErr != nil {
		if req.Context().Err() != nil {
			// A canceled request is not a region block.
			return nil, req.Context().Err()
		}
		t.logf("relay retry for %s failed", req.URL.Host)
		return nil, &BlockedError{
			Status:          resp.StatusCode,
			RelayConfigured: true,
			RelayTried:      true,
			RelayErr:        relayErr,
		}
	}
	if relayResp != nil && relayResp.StatusCode == http.StatusUnavailableForLegalReasons {
		discardBody(relayResp.Body)
		t.logf("relay retry for %s returned HTTP 451", req.URL.Host)
		return nil, &BlockedError{
			Status:          resp.StatusCode,
			RelayConfigured: true,
			RelayTried:      true,
		}
	}
	if relayResp != nil {
		t.logf("relay retry for %s returned HTTP %d", req.URL.Host, relayResp.StatusCode)
	}
	return relayResp, nil
}

func (t *Transport) logf(format string, args ...any) {
	if t.logFn != nil {
		t.logFn(format, args...)
	}
}

// relayURL maps a target URL to <relay-origin>/<target-host><target-path>.
func relayURL(origin string, target *url.URL) (*url.URL, error) {
	raw := origin + "/" + target.Host + target.EscapedPath()
	if target.RawQuery != "" || target.ForceQuery {
		raw += "?" + target.RawQuery
	}
	return url.Parse(raw)
}

func isRelayHost(target *url.URL, origin string) bool {
	if target == nil {
		return false
	}
	o, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(target.Host, o.Host)
}

func allowedHost(host string) bool {
	return allowedTargets[strings.ToLower(host)]
}

// Test performs one direct request through the relay to the server-list head.
// It does not require a 451 and never follows redirects.
func Test(ctx context.Context, client *http.Client, cfg Config) (Result, error) {
	if client == nil {
		return Result{}, errors.New("relay test: no HTTP client")
	}
	origin, err := ValidateURL(cfg.URL)
	if err != nil {
		return Result{}, err
	}
	if origin == "" {
		return Result{}, errors.New("relay URL is not configured")
	}
	target, err := url.Parse("https://firefox.settings.services.mozilla.com/v1/buckets/main/collections/vpn-serverlist/records?limit=1")
	if err != nil {
		return Result{}, err
	}
	targetURL, err := relayURL(origin, target)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL.String(), nil)
	if err != nil {
		return Result{}, err
	}
	if cfg.Token != "" {
		req.Header.Set(tokenHeader, cfg.Token)
	}
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	start := time.Now()
	resp, err := c.Do(req)
	res := Result{Latency: time.Since(start)}
	if err != nil {
		if resp != nil {
			discardBody(resp.Body)
		}
		return res, err
	}
	res.Status = resp.StatusCode
	discardBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return res, fmt.Errorf("relay test: HTTP %d", resp.StatusCode)
	}
	return res, nil
}

// Result is one relay test outcome.
type Result struct {
	Status  int
	Latency time.Duration
}

func discardBody(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 4<<10))
	_ = body.Close()
}
