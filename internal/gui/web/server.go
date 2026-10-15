// Package web serves the GUI's static assets over an in-process HTTP server.
//
// The Windows shell (T1.7) binds loopback on an ephemeral port and gates every
// request behind a per-run token carried as the first URL path segment; the
// Linux dev harness (T1.10) reuses the same server on a fixed port with the
// token check disabled. The server is static-only: GET/HEAD for files in the
// injected fs.FS, no directory listings, and a strict bind (a busy address
// fails Start; nothing ever falls back to another port).
//
// index.html is special: the server replaces the theme marker (themeMarker)
// with the current theme CSS block on every render, so the first paint is
// already themed. The block is set with SetThemeCSS, typically from
// internal/gui/theme's CSS().
package web

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

// themeMarker is the placeholder the server substitutes with the theme CSS
// block in every index.html render. It must stay a single self-contained HTML
// comment inside <head> and before any stylesheet, <link> or <script>, so the
// injected block lands before first paint. The real UI (T2.1) must keep it.
const themeMarker = "<!--kumiho:theme-css-->"

// indexFile is the document served for the (token-prefixed) root path.
const indexFile = "index.html"

// cspBase is the Content-Security-Policy sent on every response. Responses
// that carry a theme block append a style-src nonce directive: CSP falls
// style-src back to default-src, and without the nonce that fallback would
// block the injected inline <style>.
const cspBase = "default-src 'self'"

// Options configures a Server. All fields are read once by New.
type Options struct {
	// Addr is the strict TCP listen address. Empty means loopback on an
	// ephemeral port ("127.0.0.1:0"), the shell's mode; the harness passes
	// "0.0.0.0:7407".
	Addr string

	// Token, when non-empty, must be the first URL path segment of every
	// request; URL() includes it so the page and its assets stay same-origin.
	// Missing or mismatched tokens get the same 404 as a missing file (never
	// 403 — the server does not confirm it exists). Empty disables the check
	// (harness default). The token must be one URL-path-safe segment; the
	// shell generates it.
	Token string

	// Dev turns on development-only behavior: responses carry
	// Cache-Control: no-store so asset edits show up immediately. It stays
	// this small — the harness's dev panel lives in the harness (T1.10), not
	// here.
	Dev bool
}

// Server is an in-process HTTP server for the GUI assets. The zero value is
// not usable; construct one with New, then Start.
type Server struct {
	fs   fs.FS
	opts Options
	tok  []byte // opts.Token as bytes for the constant-time compare

	mu       sync.Mutex
	ln       net.Listener
	http     *http.Server
	themeCSS string
	route    routeHandler // optional dynamic route hook (the dev harness)
}

// routeHandler is the server's optional dynamic route hook. It is the dev
// harness's entry point into the same listener (bridge.go), so package web
// serves both the static assets and the harness's SSE/action/dev endpoints.
//
// A hook is consulted with the token-stripped request path after the token
// gate and before the static file handling. Reporting true means the hook
// owned the request and wrote its response; the static GET/HEAD-only policy
// never applies to it. Reporting false leaves the request to the unchanged
// static path, so a hook can only claim paths it knows — it cannot weaken the
// token or method rules for assets. The zero value (no hook) keeps the server
// static-only, exactly as before. The harness's route is the only
// implementation.
type routeHandler func(w http.ResponseWriter, r *http.Request, path string) bool

// New returns a Server serving files from fsys (StaticFS in production,
// another fs.FS in tests). It does not bind until Start.
func New(fsys fs.FS, opts Options) *Server {
	return &Server{fs: fsys, opts: opts, tok: []byte(opts.Token)}
}

// Start binds the listener synchronously — a busy address or any other bind
// failure is returned immediately and nothing is served — then serves in a
// background goroutine until Close. A second Start before Close is an error;
// Start after Close binds again.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		return errors.New("web: server already started")
	}
	addr := s.opts.Addr
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("web: listen %s: %w", addr, err)
	}
	srv := &http.Server{
		Handler:           http.HandlerFunc(s.handle),
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.ln = ln
	s.http = srv
	go func() { _ = srv.Serve(ln) }() // returns http.ErrServerClosed on Close
	return nil
}

// URL returns the absolute base URL to load, ending in "/", with the token
// segment included when configured. It returns "" before a successful Start
// and again after Close.
func (s *Server) URL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return ""
	}
	base := "http://" + s.ln.Addr().String() + "/"
	if s.opts.Token != "" {
		base += s.opts.Token + "/"
	}
	return base
}

// SetThemeCSS sets the CSS block (internal/gui/theme's CSS() output) injected
// at the theme marker in every subsequent index.html render. It is safe to
// call concurrently with requests and takes effect on the next render.
//
// Every "</style" sequence (case-insensitive, as in HTML) is removed first.
// The block is Go-generated and trusted, so in practice the filter is a no-op;
// it exists so the substitution is safe by construction even if CSS() ever
// gains untrusted input. Dropping the sequence is the cheapest way to make it
// impossible to terminate the injected <style> element early; the resulting
// stray ">" is inert CSS.
func (s *Server) SetThemeCSS(css string) {
	s.mu.Lock()
	s.themeCSS = stripStyleClose(css)
	s.mu.Unlock()
}

// setRouteHandler installs the dynamic route hook. It is unexported so the
// exported surface stays New/Start/URL/SetThemeCSS/Close; its only caller is
// the harness in this package (NewHarness). Installing a second hook is an
// error — one dynamic route owner per server, so routes can never be
// silently clobbered.
func (s *Server) setRouteHandler(h routeHandler) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.route != nil {
		return errors.New("web: server already has a route handler")
	}
	s.route = h
	return nil
}

// Close stops the server: the listener is closed, active connections are
// dropped and the serving goroutine returns. It is idempotent and safe to
// call before Start; both report nil. Start may be called again afterwards.
func (s *Server) Close() error {
	s.mu.Lock()
	srv := s.http
	s.http = nil
	s.ln = nil
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Close()
}

// handle serves one request. It is deliberately unexported: Server's exported
// surface stays New/Start/URL/SetThemeCSS/Close.
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Security-Policy", cspBase)
	h.Set("X-Content-Type-Options", "nosniff")
	if s.opts.Dev {
		h.Set("Cache-Control", "no-store")
	}

	// The token gate runs first, whatever the method: a request without the
	// right token must look like a missing file even to POST/OPTIONS, so the
	// server never confirms it is there.
	p, ok := s.stripToken(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	// The dynamic hook (the dev harness) may claim the request; unclaimed
	// requests fall through to the static rules below unchanged.
	s.mu.Lock()
	route := s.route
	s.mu.Unlock()
	if route != nil && route(w, r, p) {
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name, ok := resolveAsset(p)
	if !ok {
		http.NotFound(w, r)
		return
	}
	info, err := fs.Stat(s.fs, name)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(s.fs, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if name == indexFile {
		data, ok = s.injectTheme(w, data)
		if !ok {
			return
		}
	}

	h.Set("Content-Type", contentType(name))
	http.ServeContent(w, r, name, info.ModTime(), bytes.NewReader(data))
}

// stripToken consumes the token segment of a request path when one is
// configured, returning the remainder. The comparison is constant time; a
// missing or mismatched token reports false.
func (s *Server) stripToken(urlPath string) (string, bool) {
	if len(s.tok) == 0 {
		return urlPath, true
	}
	seg, rest := splitFirstSegment(urlPath)
	if subtle.ConstantTimeCompare([]byte(seg), s.tok) != 1 {
		return "", false
	}
	return rest, true
}

// resolveAsset maps a token-stripped request path to a file name in the fs.
// The root maps to index.html. Everything that is not a clean relative path —
// traversal (".."), empty segments, absolute or trailing-slash shapes — is
// rejected, so directory requests and listings can never reach the filesystem
// layer.
func resolveAsset(p string) (string, bool) {
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return indexFile, true
	}
	if !fs.ValidPath(p) {
		return "", false
	}
	return p, true
}

// splitFirstSegment splits a request path into its first segment and the
// remainder, which keeps its leading "/" when one follows.
func splitFirstSegment(p string) (string, string) {
	p = strings.TrimPrefix(p, "/")
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i], p[i:]
	}
	return p, ""
}

// injectTheme substitutes the theme marker in an index.html document. With no
// theme set the marker is dropped; otherwise it becomes the CSS block inside
// an inline <style> carrying a fresh per-response nonce, and the response CSP
// gains the matching style-src directive. It reports false after writing a
// 500, which happens only if crypto/rand fails.
func (s *Server) injectTheme(w http.ResponseWriter, data []byte) ([]byte, bool) {
	s.mu.Lock()
	css := s.themeCSS
	s.mu.Unlock()

	if css == "" {
		return bytes.Replace(data, []byte(themeMarker), nil, 1), true
	}
	nonce, err := newNonce()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, false
	}
	w.Header().Set("Content-Security-Policy",
		cspBase+"; style-src 'self' 'nonce-"+nonce+"'")
	block := `<style nonce="` + nonce + `">` + css + `</style>`
	return bytes.Replace(data, []byte(themeMarker), []byte(block), 1), true
}

// styleClose is the closing sequence of the injected <style> element.
const styleClose = "</style"

// stripStyleClose removes every "</style" sequence from css; see SetThemeCSS
// for why.
func stripStyleClose(css string) string {
	if !containsASCIIFold(css, styleClose) {
		return css
	}
	var b strings.Builder
	b.Grow(len(css))
	for i := 0; i < len(css); {
		if matchASCIIFold(css[i:], styleClose) {
			b.WriteByte(' ') // keep tokens apart; a stray '>' is inert CSS
			i += len(styleClose)
			continue
		}
		b.WriteByte(css[i])
		i++
	}
	return b.String()
}

// containsASCIIFold reports whether s contains want under ASCII case folding.
func containsASCIIFold(s, want string) bool {
	for i := 0; i+len(want) <= len(s); i++ {
		if matchASCIIFold(s[i:], want) {
			return true
		}
	}
	return false
}

// matchASCIIFold reports whether s begins with want under ASCII case folding,
// the comparison HTML uses for tag names. want must be ASCII and s may hold
// arbitrary bytes: matching is byte-wise, so non-ASCII case-mapping quirks
// cannot shift it (unlike strings.ToLower, which may change byte lengths).
func matchASCIIFold(s, want string) bool {
	if len(s) < len(want) {
		return false
	}
	for i := 0; i < len(want); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != want[i] {
			return false
		}
	}
	return true
}

// newNonce returns a fresh 128-bit URL-safe nonce for the injected style
// element. crypto/rand cannot realistically fail on the supported platforms;
// if it ever does, the request fails with 500 rather than serving an unthemed
// page.
func newNonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// shippedContentTypes is the explicit table for the extensions the GUI ships,
// so response types are identical on every platform. It takes precedence over
// mime.TypeByExtension, which consults the host MIME database (the Linux
// mime.types files and the Windows registry disagree, e.g. on .js). The list
// is extended alongside the assets (T2.1).
var shippedContentTypes = map[string]string{
	".css":   "text/css; charset=utf-8",
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".json":  "application/json",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".ico":   "image/vnd.microsoft.icon",
	".woff2": "font/woff2",
}

// contentType maps a file name to a response content type: the shipped table
// first, then the host's MIME database, then application/octet-stream.
func contentType(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if ct, ok := shippedContentTypes[ext]; ok {
		return ct
	}
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}
