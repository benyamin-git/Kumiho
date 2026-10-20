// Package fastly solves Fastly "Client Challenge" anti-bot pages that the
// Firefox Accounts and Guardian control planes may return from a tunnel exit.
//
// It is a stdlib-only port of the algorithm in FoxyVPN v1.0.7's
// FastlyChallengeSolver.kt (MIT). Kumiho never uses browser automation: the
// solver answers the proof-of-work, pat and clientmetrics challenges, posts
// the answers back, verifies that the challenge is gone and installs the
// resulting cookies into a shared jar so the control plane can retry.
package fastly

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/benyamin-git/kumiho/internal/logging"
)

const (
	// solveBudget is the total wall-clock budget for one Solve call.
	solveBudget = 60 * time.Second
	// maxPostBackRounds caps how many times answers may be posted back.
	maxPostBackRounds = 3
	// maxBodyBytes bounds every response read so a hostile page cannot
	// exhaust memory.
	maxBodyBytes = 2 << 20

	logTag = "fastly"

	// defaultUserAgent is the Chrome UA used by the reference implementation.
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"
	htmlAccept       = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
	// powAlphabet is the 62-character alphabet used for the two-character
	// proof-of-work suffix, in the reference implementation's order.
	powAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

var (
	// ErrDisabled reports that challenge solving is turned off.
	ErrDisabled = errors.New("fastly: challenge solving is disabled")
	// ErrNoChallenge reports that neither the challenged origin nor its
	// related apex served a challenge page.
	ErrNoChallenge = errors.New("fastly: host did not serve a Fastly challenge page")
	// ErrUnsupported reports a challenge type that cannot be solved without
	// a browser (captcha or an unknown type). It is wrapped with the type.
	ErrUnsupported = errors.New("fastly: unsupported challenge type")
)

var (
	challengePrefixRE = regexp.MustCompile(`/_fs-ch-[A-Za-z0-9]+`)
	initCallRE        = regexp.MustCompile(`init\((\[[^\]]*\]),\s*"([^"]*)",\s*"([^"]+)"`)

	// randMu guards an injected *rand.Rand shared by concurrent Solvers.
	// The package-level math/rand functions are already concurrency-safe.
	randMu sync.Mutex
)

// errNoChallengePage distinguishes "this origin is not challenged" from an
// actual failure while solving, so only absence falls back to the apex.
var errNoChallengePage = errors.New("no challenge page")

// Solver answers Fastly client challenges and installs the resulting cookies
// into SharedJar. The zero value is not usable: HTTP and SharedJar are
// required. All fields are read-only once Solve is called; a Solver may be
// used concurrently.
type Solver struct {
	// HTTP is the control-plane client. Each candidate solve clones it and
	// attaches an isolated cookie jar, so the original client is never
	// mutated. The clone shares the transport, which is safe.
	HTTP *http.Client
	// SharedJar receives the challenge cookies after a verified success.
	SharedJar http.CookieJar
	// UserAgent overrides the solver user agent; empty uses the reference
	// Chrome UA.
	UserAgent string
	// Enabled, when non-nil, is consulted before any other validation or
	// I/O. Returning false makes Solve fail with ErrDisabled.
	Enabled func() bool
	// Logf receives warn-level progress with the tag "fastly"; nil is a
	// no-op.
	Logf func(level logging.Level, tag, format string, args ...any)
	// Now is the clock used for the solve budget; nil uses time.Now.
	Now func() time.Time
	// Rand supplies the 250-750 ms jitter; nil uses the package-level
	// math/rand source. An injected *rand.Rand is used under a mutex so
	// concurrent Solve calls are race-free.
	Rand *rand.Rand

	// sleep is a test seam. Nil uses a context-aware timer.
	sleep func(context.Context, time.Duration) error
}

// Solve fetches the challenge page for challengedURL's origin (falling back
// to the related apex only when the origin served no challenge), answers its
// challenges, verifies the challenge is gone and copies the resulting cookies
// into SharedJar.
func (s *Solver) Solve(ctx context.Context, challengedURL string) error {
	if s.Enabled != nil && !s.Enabled() {
		return ErrDisabled
	}
	if s.HTTP == nil {
		return errors.New("fastly: Solver.HTTP is required")
	}
	if s.SharedJar == nil {
		return errors.New("fastly: Solver.SharedJar is required")
	}

	u, err := url.Parse(challengedURL)
	if err != nil {
		return fmt.Errorf("fastly: parse challenged URL %q: %w", challengedURL, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" {
		return fmt.Errorf("fastly: challenged URL %q must be an absolute http or https URL with a host", challengedURL)
	}

	ctx, cancel := context.WithTimeout(ctx, solveBudget)
	defer cancel()
	start := s.now()

	origin := u.Scheme + "://" + u.Host
	candidates := []string{origin}
	cookieTargets := []string{origin + "/"}

	apex, hasApex := apexOrigin(u)
	if hasApex && apex != origin {
		candidates = append(candidates, apex)
		cookieTargets = append(cookieTargets, apex+"/")
	}
	cookieTargets = append(cookieTargets, challengedURL)

	for _, candidate := range candidates {
		s.warnf("solving Fastly challenge on %s", candidate)
		err := s.solveOnHost(ctx, candidate, start, cookieTargets)
		if errors.Is(err, errNoChallengePage) {
			s.warnf("%s served no challenge page", candidate)
			continue
		}
		return err
	}
	return ErrNoChallenge
}

// solveOnHost runs the challenge flow against one origin with an isolated
// cookie jar.
func (s *Solver) solveOnHost(ctx context.Context, candidate string, start time.Time, cookieTargets []string) error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return fmt.Errorf("fastly: create challenge cookie jar: %w", err)
	}
	client := *s.HTTP
	client.Jar = jar

	pageURL := candidate + "/"
	page, challenged, err := s.fetchChallengePage(ctx, &client, pageURL)
	if err != nil {
		return err
	}
	if !challenged {
		return errNoChallengePage
	}

	prefix := challengePrefixRE.FindString(string(page))
	if prefix == "" {
		return fmt.Errorf("fastly: challenge asset prefix not found on %s", candidate)
	}
	prefixURL := candidate + prefix

	scriptURL := prefixURL + "/script.js?reload=true"
	script, err := s.fetchText(ctx, &client, scriptURL, htmlAccept)
	if err != nil {
		return err
	}
	challenges, token, err := parseInit(script)
	if err != nil {
		return fmt.Errorf("fastly: %w in %s", err, scriptURL)
	}

	postBackURL := prefixURL + "/fst-post-back"
	for round := 0; round < maxPostBackRounds; round++ {
		if err := s.checkBudget(start); err != nil {
			return err
		}
		s.warnf("challenge round %d/%d on %s", round+1, maxPostBackRounds, candidate)

		answers := make([]map[string]any, 0, len(challenges))
		for _, challenge := range challenges {
			answer, err := s.answerChallenge(ctx, &client, prefixURL, candidate, token, challenge)
			if err != nil {
				return err
			}
			answers = append(answers, answer)
		}

		postBody, err := json.Marshal(map[string]any{"token": token, "data": answers})
		if err != nil {
			return fmt.Errorf("fastly: encode post-back: %w", err)
		}
		status, respBody, err := s.request(ctx, &client, http.MethodPost, postBackURL,
			"application/json", "application/json", candidate, postBody)
		if err != nil {
			return err
		}
		if status < 200 || status > 299 {
			return fmt.Errorf("fastly: post-back to %s returned HTTP %d", postBackURL, status)
		}

		var resp struct {
			Status     string           `json:"status"`
			Challenges []map[string]any `json:"ch"`
			Token      string           `json:"tok"`
		}
		if err := json.Unmarshal(respBody, &resp); err != nil {
			return fmt.Errorf("fastly: parse post-back response from %s: %w", postBackURL, err)
		}

		if resp.Status == "success" {
			_, stillChallenged, err := s.fetchChallengePage(ctx, &client, pageURL)
			if err != nil {
				return fmt.Errorf("fastly: verify challenge on %s: %w", candidate, err)
			}
			if stillChallenged {
				return fmt.Errorf("fastly: challenge cookie was not accepted on %s", candidate)
			}
			n, err := installCookies(jar, s.SharedJar, cookieTargets)
			if err != nil {
				return err
			}
			s.warnf("challenge solved on %s; installed %d cookies", candidate, n)
			return nil
		}

		if len(resp.Challenges) == 0 || resp.Token == "" {
			return fmt.Errorf("fastly: unexpected post-back response from %s: %q", postBackURL, snippet(respBody))
		}
		challenges = resp.Challenges
		token = resp.Token

		if round+1 < maxPostBackRounds {
			if err := s.pause(ctx, s.jitter()); err != nil {
				return fmt.Errorf("fastly: wait between challenge rounds: %w", err)
			}
		}
	}
	return fmt.Errorf("fastly: challenge did not complete within %d post-back rounds", maxPostBackRounds)
}

// fetchChallengePage fetches an origin page and reports whether it is a
// challenge. A non-challenge page is not an error here: callers use the false
// result to fall back to the apex, or to verify that a solved page is clean.
func (s *Solver) fetchChallengePage(ctx context.Context, client *http.Client, pageURL string) ([]byte, bool, error) {
	page, err := s.fetchText(ctx, client, pageURL, htmlAccept)
	if err != nil {
		return nil, false, err
	}
	return page, isChallengePage(page), nil
}

// fetchText performs a GET and requires a 2xx status.
func (s *Solver) fetchText(ctx context.Context, client *http.Client, rawURL, accept string) ([]byte, error) {
	status, body, err := s.request(ctx, client, http.MethodGet, rawURL, accept, "", "", nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status > 299 {
		return nil, fmt.Errorf("fastly: GET %s returned HTTP %d", rawURL, status)
	}
	return body, nil
}

// request performs one solver request with the solver UA and bounded response
// reads.
func (s *Solver) request(ctx context.Context, client *http.Client, method, rawURL, accept, contentType, origin string, body []byte) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rdr)
	if err != nil {
		return 0, nil, fmt.Errorf("fastly: build %s %s request: %w", method, rawURL, err)
	}
	req.Header.Set("User-Agent", s.userAgent())
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("fastly: %s %s: %w", method, rawURL, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("fastly: read %s %s response: %w", method, rawURL, err)
	}
	if len(data) > maxBodyBytes {
		return resp.StatusCode, nil, fmt.Errorf("fastly: %s %s response exceeds %d bytes", method, rawURL, maxBodyBytes)
	}
	return resp.StatusCode, data, nil
}

// answerChallenge turns one parsed challenge into its post-back answer object.
func (s *Solver) answerChallenge(ctx context.Context, client *http.Client, prefixURL, origin, token string, challenge map[string]any) (map[string]any, error) {
	ty, _ := challenge["ty"].(string)
	data, _ := challenge["data"].(map[string]any)
	if data == nil {
		data = map[string]any{}
	}

	switch ty {
	case "pow":
		base, _ := data["base"].(string)
		targetHex, _ := data["hash"].(string)
		answer, err := solvePow(base, targetHex)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"ty":      "pow",
			"base":    base,
			"answer":  answer,
			"hmac":    data["hmac"],
			"expires": data["expires"],
		}, nil
	case "pat":
		auth, err := s.fetchPAT(ctx, client, prefixURL, origin, token)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ty": "pat", "auth": auth}, nil
	case "clientmetrics":
		return clientMetricsAnswer(), nil
	default:
		return nil, fmt.Errorf("%w %q (captcha cannot be solved automatically)", ErrUnsupported, ty)
	}
}

// fetchPAT requests the pat challenge token. A 400 or 401 means the exit IP is
// not allowed an auth token and yields an empty (but valid) auth answer.
func (s *Solver) fetchPAT(ctx context.Context, client *http.Client, prefixURL, origin, token string) (string, error) {
	patURL := prefixURL + "/pat?token=" + url.QueryEscape(token)
	status, body, err := s.request(ctx, client, http.MethodPost, patURL,
		"text/plain", "application/json", origin, []byte{})
	if err != nil {
		return "", err
	}
	if status == http.StatusBadRequest || status == http.StatusUnauthorized {
		return "", nil
	}
	if status < 200 || status > 299 {
		return "", fmt.Errorf("fastly: pat request to %s returned HTTP %d", patURL, status)
	}

	var resp struct {
		Auth string `json:"auth"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("fastly: parse pat response from %s: %w", patURL, err)
	}
	if resp.Auth == "" {
		return "", fmt.Errorf("fastly: empty pat auth token from %s", patURL)
	}
	return resp.Auth, nil
}

// parseInit returns the challenge list and token from the last
// init([…], "token", "prefix") call in script.
func parseInit(script []byte) ([]map[string]any, string, error) {
	matches := initCallRE.FindAllSubmatch(script, -1)
	if len(matches) == 0 {
		return nil, "", errors.New("challenge init() call not found")
	}
	last := matches[len(matches)-1]

	var challenges []map[string]any
	if err := json.Unmarshal(last[1], &challenges); err != nil {
		return nil, "", fmt.Errorf("parse challenge list: %w", err)
	}
	if len(challenges) == 0 {
		return nil, "", errors.New("empty challenge list")
	}
	token := string(last[2])
	if token == "" {
		return nil, "", errors.New("empty challenge token")
	}
	return challenges, token, nil
}

// solvePow finds the two-character suffix for which SHA-256(base+suffix)
// equals the 32-byte target. It tries the reference alphabet in order.
func solvePow(base, targetHex string) (string, error) {
	target, err := hex.DecodeString(targetHex)
	if err != nil {
		return "", fmt.Errorf("fastly: invalid proof-of-work hash %q: %w", targetHex, err)
	}
	if len(target) != sha256.Size {
		return "", fmt.Errorf("fastly: proof-of-work hash %q is %d bytes, want %d", targetHex, len(target), sha256.Size)
	}
	for i := 0; i < len(powAlphabet); i++ {
		for j := 0; j < len(powAlphabet); j++ {
			suffix := powAlphabet[i:i+1] + powAlphabet[j:j+1]
			sum := sha256.Sum256([]byte(base + suffix))
			if bytes.Equal(sum[:], target) {
				return suffix, nil
			}
		}
	}
	return "", fmt.Errorf("fastly: no proof-of-work solution found for base %q", base)
}

// clientMetricsAnswer is the reference implementation's static non-bot
// clientmetrics payload. It is deliberately not a browser fingerprint.
func clientMetricsAnswer() map[string]any {
	return map[string]any{
		"ty":        "clientmetrics",
		"webdriver": false,
		"bot_detection_result": map[string]any{
			"bot_detected": false,
			"bot_kind":     nil,
		},
		"browser_metrics": map[string]any{
			"client_data": "{}",
			"error_trace": nil,
		},
		"detector_results": map[string]any{},
		"v":                2,
	}
}

// installCookies copies the isolated jar's cookies into the shared jar for
// every target URL. It fails when no cookies were issued at all.
//
// Cookies are collected as a union across the targets and then installed for
// every target. This is what lets an apex fallback authenticate the original
// challenged host: a host-only cookie issued by the apex does not apply to the
// api host in the isolated jar, so copying per-target applicability alone
// would leave the control plane challenged.
func installCookies(from, to http.CookieJar, targets []string) (int, error) {
	urls := make([]*url.URL, 0, len(targets))
	seen := make(map[string]bool)
	var union []*http.Cookie
	for _, target := range targets {
		u, err := url.Parse(target)
		if err != nil {
			return 0, fmt.Errorf("fastly: parse cookie target %q: %w", target, err)
		}
		urls = append(urls, u)
		for _, c := range from.Cookies(u) {
			// net/http/cookiejar returns only Name and Value. Installing
			// with an explicit root path collapses the derived per-URL
			// paths into one entry per host; without it the challenged URL
			// (whose default path is /v1) sees the same cookie twice.
			// Dropping the other attributes keeps the installed cookie
			// host-only for each target URL, mirroring the reference
			// implementation's normalize-to-apex behavior.
			key := c.Name + "\x00" + c.Value
			if seen[key] {
				continue
			}
			seen[key] = true
			union = append(union, &http.Cookie{Name: c.Name, Value: c.Value, Path: "/"})
		}
	}
	if len(union) == 0 {
		return 0, errors.New("fastly: challenge completed but no cookies were issued")
	}
	for _, u := range urls {
		to.SetCookies(u, union)
	}
	return len(union), nil
}

// apexOrigin returns the challenged origin with a leading api. or www. label
// dropped, preserving the scheme and any port.
func apexOrigin(u *url.URL) (string, bool) {
	host := u.Hostname()
	if host == "" {
		return "", false
	}
	lower := strings.ToLower(host)
	var rest string
	switch {
	case strings.HasPrefix(lower, "api."):
		rest = host[len("api."):]
	case strings.HasPrefix(lower, "www."):
		rest = host[len("www."):]
	default:
		return "", false
	}
	if rest == "" {
		return "", false
	}
	if port := u.Port(); port != "" {
		rest = net.JoinHostPort(rest, port)
	}
	return u.Scheme + "://" + rest, true
}

func isChallengePage(body []byte) bool {
	s := string(body)
	return strings.Contains(s, "/_fs-ch-") && strings.Contains(s, "Client Challenge")
}

func (s *Solver) userAgent() string {
	if s.UserAgent != "" {
		return s.UserAgent
	}
	return defaultUserAgent
}

func (s *Solver) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Solver) warnf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(logging.Warn, logTag, format, args...)
	}
}

// checkBudget enforces the 60 s budget with the injectable clock, independently
// of the context deadline.
func (s *Solver) checkBudget(start time.Time) error {
	if s.now().Sub(start) >= solveBudget {
		return fmt.Errorf("fastly: challenge exceeded its %s budget: %w", solveBudget, context.DeadlineExceeded)
	}
	return nil
}

// jitter returns a duration in [250ms, 750ms].
func (s *Solver) jitter() time.Duration {
	const minMS, maxMS = 250, 750
	span := int64(maxMS - minMS + 1)
	var n int64
	if s.Rand != nil {
		randMu.Lock()
		n = s.Rand.Int63n(span)
		randMu.Unlock()
	} else {
		n = rand.Int63n(span)
	}
	return minMS*time.Millisecond + time.Duration(n)*time.Millisecond
}

// pause waits for d or until ctx is done. Tests replace sleep.
func (s *Solver) pause(ctx context.Context, d time.Duration) error {
	if s.sleep != nil {
		return s.sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func snippet(body []byte) string {
	const limit = 256
	if len(body) > limit {
		return string(body[:limit]) + "..."
	}
	return string(body)
}
