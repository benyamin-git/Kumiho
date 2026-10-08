// Package guardian talks to the Mozilla VPN Guardian entitlement API
// (PLAN.md §2.1): proxy passes, account status and activation.
package guardian

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the Guardian control plane.
const DefaultBaseURL = "https://vpn.mozilla.org"

// UserAgent identifies kumiho to Mozilla's control plane (PLAN.md §2.1).
const UserAgent = "MozillaVPN/2.35.0 (sys:linux; iap:true)"

const maxResponseBytes = 64 << 10

// Pass is a Guardian proxy pass: the bearer token on HTTP/2 CONNECT plus the
// quota headers from the same response.
type Pass struct {
	Token          string `json:"token"`
	ExpiresAt      int64  `json:"expires_at,omitempty"`      // unix seconds; 0 = unknown
	QuotaLimit     *int64 `json:"quota_limit,omitempty"`     // X-Quota-Limit
	QuotaRemaining *int64 `json:"quota_remaining,omitempty"` // X-Quota-Remaining
	QuotaReset     *int64 `json:"quota_reset,omitempty"`     // X-Quota-Reset (unix seconds)
}

// Entitlement is the account view from /fpn/status and /fpn/activate.
type Entitlement struct {
	Subscribed       bool   `json:"subscribed"`
	UID              string `json:"uid,omitempty"`
	MaxBytes         *int64 `json:"max_bytes,omitempty"`
	LimitedBandwidth bool   `json:"limited_bandwidth,omitempty"`
	QuotaRemaining   *int64 `json:"quota_remaining,omitempty"`
}

// HTTPError is a non-2xx Guardian response.
type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("guardian: HTTP %d: %s", e.Status, e.Message)
}

// IsUnauthorized reports whether Guardian rejected the access token (401/403).
func IsUnauthorized(err error) bool {
	var he *HTTPError
	return errors.As(err, &he) &&
		(he.Status == http.StatusUnauthorized || he.Status == http.StatusForbidden)
}

// IsQuotaExceeded reports quota exhaustion (429): fatal until reset.
func IsQuotaExceeded(err error) bool {
	var he *HTTPError
	return errors.As(err, &he) && he.Status == http.StatusTooManyRequests
}

// IsChallenge reports the Fastly anti-bot challenge (406).
func IsChallenge(err error) bool {
	var he *HTTPError
	return errors.As(err, &he) && he.Status == http.StatusNotAcceptable
}

// Client talks to Guardian.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	// OnChallenge, when set, is invoked once to solve a Fastly anti-bot
	// challenge before the request is retried (PLAN.md §2.2).
	OnChallenge func(ctx context.Context) error
}

// NewClient returns a Client with production defaults.
func NewClient() *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// FetchPass requests a fresh proxy pass (GET /api/v1/fpn/token).
func (c *Client) FetchPass(ctx context.Context, accessToken string) (*Pass, error) {
	header, body, err := c.do(ctx, http.MethodGet, "/api/v1/fpn/token", accessToken)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("guardian pass response: %w", err)
	}
	if resp.Token == "" {
		return nil, errors.New("guardian pass response did not contain a token")
	}
	pass := &Pass{Token: resp.Token, ExpiresAt: resp.ExpiresAt}
	if pass.ExpiresAt == 0 {
		if exp, ok := JWTExpiry(resp.Token); ok {
			pass.ExpiresAt = exp
		}
	}
	pass.QuotaLimit = headerInt(header, "X-Quota-Limit")
	pass.QuotaRemaining = headerInt(header, "X-Quota-Remaining")
	pass.QuotaReset = headerInt(header, "X-Quota-Reset")
	return pass, nil
}

// Account fetches the entitlement (GET /api/v1/fpn/status). For limited
// bandwidth accounts the quota comes from a proxy-pass response, matching
// the reference client.
func (c *Client) Account(ctx context.Context, accessToken string) (*Entitlement, error) {
	_, body, err := c.do(ctx, http.MethodGet, "/api/v1/fpn/status", accessToken)
	if err != nil {
		return nil, err
	}
	ent, err := parseEntitlement(body)
	if err != nil {
		return nil, err
	}
	if ent.LimitedBandwidth {
		if pass, err := c.FetchPass(ctx, accessToken); err == nil {
			ent.QuotaRemaining = pass.QuotaRemaining
		}
	}
	return ent, nil
}

// Activate claims the VPN entitlement (POST /api/v1/fpn/activate).
func (c *Client) Activate(ctx context.Context, accessToken string) (*Entitlement, error) {
	_, body, err := c.do(ctx, http.MethodPost, "/api/v1/fpn/activate", accessToken)
	if err != nil {
		return nil, err
	}
	return parseEntitlement(body)
}

func parseEntitlement(body []byte) (*Entitlement, error) {
	var raw struct {
		Subscribed       bool            `json:"subscribed"`
		UID              json.RawMessage `json:"uid"`
		MaxBytes         json.RawMessage `json:"maxBytes"`
		LimitedBandwidth bool            `json:"limited_bandwidth"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("guardian entitlement response: %w", err)
	}
	return &Entitlement{
		Subscribed:       raw.Subscribed,
		UID:              rawUID(raw.UID),
		MaxBytes:         rawInt64(raw.MaxBytes),
		LimitedBandwidth: raw.LimitedBandwidth,
	}, nil
}

// rawUID accepts the uid either as a JSON string or a number (the live
// Guardian API returns a number).
func rawUID(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String()
	}
	return ""
}

// rawInt64 accepts a number encoded as a JSON number or string (the live
// Guardian API encodes maxBytes as a string).
func rawInt64(raw json.RawMessage) *int64 {
	if len(raw) == 0 {
		return nil
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return &n
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			return &v
		}
	}
	return nil
}

// do sends an authorized request, retrying once through OnChallenge on 406.
func (c *Client) do(ctx context.Context, method, path, accessToken string) (http.Header, []byte, error) {
	endpoint := strings.TrimRight(c.baseURL(), "/") + path

	build := func() (*http.Request, error) {
		var reader io.Reader
		if method == http.MethodPost {
			reader = strings.NewReader("")
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", UserAgent)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+accessToken)
		return req, nil
	}

	for attempt := 0; ; attempt++ {
		req, err := build()
		if err != nil {
			return nil, nil, err
		}
		resp, err := c.httpClient().Do(req)
		if err != nil {
			return nil, nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		resp.Body.Close()
		if readErr != nil {
			return nil, nil, readErr
		}

		if resp.StatusCode == http.StatusNotAcceptable {
			if c.OnChallenge == nil || attempt > 0 {
				return nil, nil, &HTTPError{Status: resp.StatusCode, Message: "anti-bot challenge (Fastly)"}
			}
			if err := c.OnChallenge(ctx); err != nil {
				return nil, nil, fmt.Errorf("anti-bot challenge: %w", err)
			}
			continue
		}
		if resp.StatusCode >= 400 {
			return nil, nil, &HTTPError{Status: resp.StatusCode, Message: strings.TrimSpace(string(body))}
		}
		return resp.Header, body, nil
	}
}

// JWTExpiry extracts the exp claim (unix seconds) from a JWT, tolerating
// both raw and padded base64url payloads.
func JWTExpiry(token string) (int64, bool) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return 0, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return 0, false
		}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp <= 0 {
		return 0, false
	}
	return claims.Exp, true
}

func headerInt(h http.Header, key string) *int64 {
	v := strings.TrimSpace(h.Get(key))
	if v == "" {
		return nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return DefaultBaseURL
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}
