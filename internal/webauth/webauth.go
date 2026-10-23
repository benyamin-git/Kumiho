// Package webauth implements the browser sign-in flow used by Kumiho: PKCE,
// a one-shot loopback callback, and the Mozilla VPN login/verify exchange.
package webauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/benyamin-git/kumiho/internal/guardian"
)

const (
	defaultBaseURL   = "https://vpn.mozilla.org"
	defaultTimeout   = 5 * time.Minute
	maxResponseBytes = 1 << 20
	verifyPath       = "/api/v2/vpn/login/verify"
	callbackPage     = "Kumiho sign-in complete. You can close this tab and return to Kumiho."
)

// Config controls a browser sign-in flow.
type Config struct {
	BaseURL   string
	Platform  string
	UserAgent string
	HTTP      *http.Client
	Solver    ChallengeSolver
	Now       func() time.Time
	Rand      io.Reader
	Timeout   time.Duration
}

// ChallengeSolver solves a control-plane anti-bot challenge for a URL.
type ChallengeSolver interface {
	Solve(ctx context.Context, url string) error
}

// Flow is a configured browser sign-in factory.
type Flow struct {
	cfg Config
}

// Result is a completed browser sign-in.
type Result struct {
	UID         string
	Email       string
	DisplayName string
	Token       string
	ExpiresAt   int64
}

// Session is one in-flight browser sign-in.
type Session struct {
	URL string
	ID  string

	flow       *Flow
	listener   net.Listener
	server     *http.Server
	verifier   string
	callbacks  chan callbackEvent
	done       chan struct{}
	cancelOnce sync.Once
	closeOnce  sync.Once
}

type callbackEvent struct {
	code string
	err  error
}

// New returns a Flow with production defaults.
func New(cfg Config) *Flow {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.HTTP == nil {
		cfg.HTTP = http.DefaultClient
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	return &Flow{cfg: cfg}
}

// Start binds a one-shot loopback listener and returns the browser URL.
func (f *Flow) Start(ctx context.Context) (*Session, error) {
	if f.cfg.Platform == "" {
		return nil, errors.New("webauth: platform is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	verifierRaw := make([]byte, 96)
	if _, err := io.ReadFull(f.cfg.Rand, verifierRaw); err != nil {
		return nil, fmt.Errorf("webauth: generate verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierRaw)

	idRaw := make([]byte, 12)
	if _, err := io.ReadFull(f.cfg.Rand, idRaw); err != nil {
		return nil, fmt.Errorf("webauth: generate login id: %w", err)
	}
	id := hex.EncodeToString(idRaw)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("webauth: listen: %w", err)
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		ln.Close()
		return nil, errors.New("webauth: loopback listener has no TCP address")
	}

	s := &Session{
		URL:       loginURL(f.cfg, s256Challenge(verifier), addr.Port),
		ID:        id,
		flow:      f,
		listener:  ln,
		verifier:  verifier,
		callbacks: make(chan callbackEvent, 1),
		done:      make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleCallback)
	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		_ = s.server.Serve(ln)
	}()
	return s, nil
}

func loginURL(cfg Config, codeChallenge string, port int) string {
	base := strings.TrimRight(cfg.BaseURL, "/")
	query := strings.Join([]string{
		"code_challenge=" + url.QueryEscape(codeChallenge),
		"code_challenge_method=S256",
		"user_agent=" + url.QueryEscape(cfg.UserAgent),
		"port=" + url.QueryEscape(strconv.Itoa(port)),
		"utm_medium=vpn-client",
		"utm_source=kumiho",
	}, "&")
	return base + "/api/v2/vpn/login/" + url.PathEscape(cfg.Platform) + "?" + query
}

func (s *Session) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if message := r.URL.Query().Get("error"); message != "" {
		s.deliver(callbackEvent{err: fmt.Errorf("webauth: sign-in failed: %s", message)})
		writeCallbackPage(w)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	s.deliver(callbackEvent{code: code})
	writeCallbackPage(w)
}

func (s *Session) deliver(cb callbackEvent) {
	select {
	case s.callbacks <- cb:
	default:
	}
}

func writeCallbackPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "<!doctype html><html><body><p>"+callbackPage+"</p></body></html>")
}

// Wait blocks for the callback, exchanges the code, and returns the result.
func (s *Session) Wait(ctx context.Context) (*Result, error) {
	defer s.close()

	timer := time.NewTimer(s.flow.cfg.Timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, fmt.Errorf("webauth: sign-in cancelled: %w", context.Canceled)
	case <-timer.C:
		return nil, fmt.Errorf("webauth: sign-in timed out after %s: %w", s.flow.cfg.Timeout, context.DeadlineExceeded)
	case cb := <-s.callbacks:
		if cb.err != nil {
			return nil, cb.err
		}
		return s.exchange(ctx, cb.code)
	}
}

// Cancel stops the flow and unblocks Wait. It is safe to call more than once.
func (s *Session) Cancel() {
	s.cancelOnce.Do(func() {
		close(s.done)
		s.close()
	})
}

func (s *Session) close() {
	s.closeOnce.Do(func() {
		_ = s.server.Close()
		_ = s.listener.Close()
	})
}

func (s *Session) exchange(ctx context.Context, code string) (*Result, error) {
	verifyURL := strings.TrimRight(s.flow.cfg.BaseURL, "/") + verifyPath
	body, err := json.Marshal(struct {
		Code     string `json:"code"`
		Verifier string `json:"code_verifier"`
	}{Code: code, Verifier: s.verifier})
	if err != nil {
		return nil, fmt.Errorf("webauth: encode verify request: %w", err)
	}

	for attempt := 1; attempt <= 5; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, verifyURL, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("webauth: create verify request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", s.flow.cfg.UserAgent)

		resp, err := s.flow.cfg.HTTP.Do(req)
		if err != nil {
			return nil, fmt.Errorf("webauth: verify request: %w", err)
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("webauth: read verify response: %w", readErr)
		}

		if resp.StatusCode == http.StatusNotAcceptable {
			if s.flow.cfg.Solver == nil || attempt == 5 {
				return nil, fmt.Errorf("webauth: verify HTTP %d", resp.StatusCode)
			}
			if err := s.flow.cfg.Solver.Solve(ctx, verifyURL); err != nil {
				return nil, fmt.Errorf("webauth: solve challenge: %w", err)
			}
			continue
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return nil, fmt.Errorf("webauth: verify HTTP %d", resp.StatusCode)
		}
		return parseResult(data)
	}
	return nil, errors.New("webauth: verify failed")
}

func parseResult(data []byte) (*Result, error) {
	var raw struct {
		User struct {
			UID         json.RawMessage `json:"uid"`
			Email       string          `json:"email"`
			DisplayName string          `json:"displayName"`
		} `json:"user"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("webauth: verify response: %w", err)
	}
	if strings.TrimSpace(raw.Token) == "" || strings.TrimSpace(raw.User.Email) == "" {
		return nil, errors.New("webauth: verify response is missing user data")
	}
	uid, err := uidString(raw.User.UID)
	if err != nil {
		return nil, fmt.Errorf("webauth: verify response uid: %w", err)
	}
	expires, _ := guardian.JWTExpiry(raw.Token)
	return &Result{
		UID:         uid,
		Email:       raw.User.Email,
		DisplayName: raw.User.DisplayName,
		Token:       raw.Token,
		ExpiresAt:   expires,
	}, nil
}

func uidString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	if raw[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", err
		}
		return value, nil
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return "", err
	}
	return number.String(), nil
}

func s256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
