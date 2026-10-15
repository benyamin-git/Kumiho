package web

import (
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

// testClient bounds requests so a bug fails a test instead of hanging it.
var testClient = &http.Client{Timeout: 5 * time.Second}

// startServer starts a server over assets on loopback and registers teardown.
func startServer(t *testing.T, assets fs.FS, opts Options) *Server {
	t.Helper()
	if opts.Addr == "" {
		opts.Addr = "127.0.0.1:0"
	}
	s := New(assets, opts)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return s
}

// rootURL returns s.URL() with the token segment (when any) stripped: the
// base for both tokened and untokened request URLs in tests.
func rootURL(t *testing.T, s *Server, token string) string {
	t.Helper()
	u := s.URL()
	if u == "" {
		t.Fatal("URL() is empty; server not started")
	}
	if token != "" {
		u = strings.TrimSuffix(u, token+"/")
	}
	return u
}

// pageWithMarker is a minimal test document carrying the theme marker.
func pageWithMarker(body string) string {
	return "<!doctype html><html><head>" + themeMarker +
		"</head><body>" + body + "</body></html>"
}

type response struct {
	status int
	header http.Header
	body   string
}

// do performs one request and returns everything tests assert on.
func do(t *testing.T, method, rawURL string) response {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		t.Fatalf("NewRequest(%s %s): %v", method, rawURL, err)
	}
	resp, err := testClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, rawURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s: %v", method, rawURL, err)
	}
	return response{status: resp.StatusCode, header: resp.Header, body: string(body)}
}

func TestURLBeforeStart(t *testing.T) {
	s := New(StaticFS(), Options{})
	if got := s.URL(); got != "" {
		t.Fatalf("URL() before Start = %q; want \"\"", got)
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close() before Start = %v; want nil", err)
	}
}

func TestURLAndRootWithoutToken(t *testing.T) {
	s := startServer(t, StaticFS(), Options{})
	u := s.URL()
	if !strings.HasPrefix(u, "http://127.0.0.1:") || !strings.HasSuffix(u, "/") {
		t.Fatalf("URL() = %q; want http://127.0.0.1:<port>/", u)
	}
	got := do(t, "GET", u)
	if got.status != http.StatusOK {
		t.Fatalf("GET %s = %d; want 200", u, got.status)
	}
	if !strings.Contains(got.body, "asset placeholder") {
		t.Errorf("root body does not contain the placeholder text")
	}
}

func TestTokenEnforced(t *testing.T) {
	const token = "run-abc123"
	s := startServer(t, StaticFS(), Options{Token: token})
	if u := s.URL(); !strings.HasSuffix(u, "/"+token+"/") {
		t.Fatalf("URL() = %q; want suffix /%s/", u, token)
	}
	root := rootURL(t, s, token)

	cases := []struct {
		rel  string
		want int
		why  string
	}{
		{"", http.StatusNotFound, "root without token"},
		{"index.html", http.StatusNotFound, "file without token"},
		{"wrong/index.html", http.StatusNotFound, "different token"},
		{"run-abc124/index.html", http.StatusNotFound, "same-length wrong token"},
		{"run-abc12/index.html", http.StatusNotFound, "shorter wrong token"},
		{"run-abc1234/index.html", http.StatusNotFound, "longer wrong token"},
		{token + "/", http.StatusOK, "root with token"},
		{token, http.StatusOK, "root with token, no trailing slash"},
		{token + "/index.html", http.StatusOK, "file with token"},
	}
	for _, tc := range cases {
		got := do(t, "GET", root+tc.rel)
		if got.status != tc.want {
			t.Errorf("GET /%s (%s) = %d; want %d", tc.rel, tc.why, got.status, tc.want)
		}
	}

	// The token gate precedes the method check: a wrong or missing token
	// answers 404 even for methods that would otherwise be 405, so the
	// server's identity is never confirmed.
	if got := do(t, "POST", root+"index.html"); got.status != http.StatusNotFound {
		t.Errorf("POST without token = %d; want 404", got.status)
	}
	if got := do(t, "POST", root+token+"/index.html"); got.status != http.StatusMethodNotAllowed {
		t.Errorf("POST with token = %d; want 405", got.status)
	}
}

func TestNotFound(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":   {Data: []byte(pageWithMarker("index"))},
		"dir/file.txt": {Data: []byte("nested")},
	}
	s := startServer(t, assets, Options{})
	root := s.URL()

	cases := []struct {
		rel string
		why string
	}{
		{"missing.css", "missing file"},
		{"dir", "directory request"},
		{"dir/", "directory request with slash"},
		{"../index.html", "literal traversal"},
		{"./index.html", "dot segment"},
		{"a//b.txt", "empty segment"},
	}
	for _, tc := range cases {
		got := do(t, "GET", root+tc.rel)
		if got.status != http.StatusNotFound {
			t.Errorf("GET %s (%s) = %d; want 404", tc.rel, tc.why, got.status)
		}
		if strings.Contains(got.body, "file.txt") {
			t.Errorf("GET %s (%s) body leaks directory entries: %q", tc.rel, tc.why, got.body)
		}
	}

	if got := do(t, "GET", root+"dir/file.txt"); got.status != http.StatusOK || got.body != "nested" {
		t.Errorf("GET dir/file.txt = %d %q; want 200 %q", got.status, got.body, "nested")
	}
}

// TestTraversalRejectedAtHandler exercises decoded path shapes (encoded dot
// segments, absolute prefixes) directly on the handler, where they survive a
// client round trip byte-for-byte.
func TestTraversalRejectedAtHandler(t *testing.T) {
	for _, p := range []string{
		"/..", "/../secret", "/a/../../b", "//etc/passwd", "/%2e%2e/secret", "/a/./b",
	} {
		s := New(StaticFS(), Options{})
		req := httptest.NewRequest("GET", "/", nil)
		req.URL.Path = p
		rec := httptest.NewRecorder()
		s.handle(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("handle path %q = %d; want 404", p, rec.Code)
		}
	}

	s := New(StaticFS(), Options{Token: "tok"})
	req := httptest.NewRequest("GET", "/", nil)
	req.URL.Path = "/tok/../secret"
	rec := httptest.NewRecorder()
	s.handle(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("handle tokened traversal = %d; want 404", rec.Code)
	}
}

func TestMethodRestrictions(t *testing.T) {
	s := startServer(t, StaticFS(), Options{})
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		got := do(t, m, s.URL())
		if got.status != http.StatusMethodNotAllowed {
			t.Errorf("%s / = %d; want 405", m, got.status)
		}
		if allow := got.header.Get("Allow"); allow != "GET, HEAD" {
			t.Errorf("%s / Allow = %q; want %q", m, allow, "GET, HEAD")
		}
		if csp := got.header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
			t.Errorf("%s / CSP = %q; want default-src 'self'", m, csp)
		}
	}
}

func TestHead(t *testing.T) {
	s := startServer(t, StaticFS(), Options{})
	got := do(t, "HEAD", s.URL())
	if got.status != http.StatusOK {
		t.Fatalf("HEAD / = %d; want 200", got.status)
	}
	if got.body != "" {
		t.Errorf("HEAD / returned a %d-byte body; want none", len(got.body))
	}
}

func TestSecurityHeaders(t *testing.T) {
	s := startServer(t, StaticFS(), Options{})
	for _, rel := range []string{"", "index.html", "missing.css"} {
		got := do(t, "GET", s.URL()+rel)
		if csp := got.header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
			t.Errorf("GET /%s CSP = %q; want default-src 'self'", rel, csp)
		}
		if x := got.header.Get("X-Content-Type-Options"); x != "nosniff" {
			t.Errorf("GET /%s X-Content-Type-Options = %q; want nosniff", rel, x)
		}
	}
}

func TestThemeInjection(t *testing.T) {
	const css = ":root{--primary:#7daeec}"
	assets := fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html><html><head>" + themeMarker +
			`<link rel="stylesheet" href="css/app.css"></head>` +
			`<body><script src="js/app.js"></script></body></html>`)},
	}
	s := startServer(t, assets, Options{})
	s.SetThemeCSS(css)

	got := do(t, "GET", s.URL())
	if got.status != http.StatusOK {
		t.Fatalf("GET / = %d; want 200", got.status)
	}
	if strings.Contains(got.body, themeMarker) {
		t.Error("theme marker was not replaced")
	}
	if !strings.Contains(got.body, css) {
		t.Errorf("body does not contain the theme CSS %q", css)
	}
	style := strings.Index(got.body, "<style")
	link := strings.Index(got.body, "<link")
	script := strings.Index(got.body, "<script")
	if style < 0 || link < 0 || script < 0 {
		t.Fatalf("marker/style/link/script missing: style=%d link=%d script=%d", style, link, script)
	}
	if style > link || style > script {
		t.Errorf("injected style at %d; want before link (%d) and script (%d)", style, link, script)
	}

	csp := got.header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "style-src 'self' 'nonce-") {
		t.Fatalf("CSP = %q; want a style-src nonce directive", csp)
	}
	nonce := csp[strings.Index(csp, "'nonce-")+len("'nonce-"):]
	nonce = nonce[:strings.Index(nonce, "'")]
	if !strings.Contains(got.body, `<style nonce="`+nonce+`">`) {
		t.Errorf("style element does not carry the header nonce %q", nonce)
	}
}

func TestSetThemeCSSHonoredOnReRequest(t *testing.T) {
	s := startServer(t, StaticFS(), Options{})
	before := do(t, "GET", s.URL())
	if strings.Contains(before.body, "<style nonce") {
		t.Error("index served a style block before SetThemeCSS")
	}
	if strings.Contains(before.body, themeMarker) {
		t.Error("theme marker was not dropped without a theme")
	}

	s.SetThemeCSS(":root{--one:1}")
	first := do(t, "GET", s.URL())
	if !strings.Contains(first.body, ":root{--one:1}") {
		t.Error("first SetThemeCSS value was not served")
	}

	s.SetThemeCSS(":root{--two:2}")
	second := do(t, "GET", s.URL())
	if strings.Contains(second.body, "--one:1") || !strings.Contains(second.body, ":root{--two:2}") {
		t.Error("index did not pick up the new theme CSS on re-request")
	}
}

func TestStripStyleClose(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{":root{--a:1}", ":root{--a:1}"},
		{"</style", " "},
		{"</STYLE>", " >"},
		{"a</StYlE b", "a  b"},
		{"\u0130</style", "\u0130 "}, // case folding must not shift byte offsets
	}
	for _, tc := range cases {
		if got := stripStyleClose(tc.in); got != tc.want {
			t.Errorf("stripStyleClose(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestStyleBreakoutGuarded(t *testing.T) {
	s := startServer(t, StaticFS(), Options{})
	s.SetThemeCSS(`:root{--x:1}</style><script>alert(1)</script>`)
	got := do(t, "GET", s.URL())

	if n := strings.Count(strings.ToLower(got.body), "</style"); n != 1 {
		t.Fatalf("body has %d </style sequences; want only the injected block's", n)
	}
	script := strings.Index(got.body, "<script")
	closing := strings.Index(strings.ToLower(got.body), "</style")
	if script < 0 {
		t.Fatal("expected the sanitized text to remain inside the style element")
	}
	if script > closing {
		t.Errorf("<script at %d is outside the style element closing at %d", script, closing)
	}
}

func TestSetThemeCSSConcurrent(t *testing.T) {
	s := startServer(t, StaticFS(), Options{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				s.SetThemeCSS(fmt.Sprintf(":root{--v:%d}", i))
			}
		}(i)
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				resp, err := testClient.Get(s.URL())
				if err != nil {
					t.Errorf("GET during SetThemeCSS: %v", err)
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()
}

func TestCloseIdempotent(t *testing.T) {
	s := New(StaticFS(), Options{Addr: "127.0.0.1:0"})
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	rawURL := s.URL()
	if err := s.Close(); err != nil {
		t.Fatalf("first Close() = %v; want nil", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close() = %v; want nil", err)
	}
	if got := s.URL(); got != "" {
		t.Errorf("URL() after Close = %q; want \"\"", got)
	}
	if _, err := testClient.Get(rawURL); err == nil {
		t.Error("request after Close succeeded; want a connection error")
	}
}

func TestStartBusyPortFailsStrictly(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	s := New(StaticFS(), Options{Addr: ln.Addr().String()})
	if err := s.Start(); err == nil {
		_ = s.Close()
		t.Fatal("Start() on a busy address succeeded; want an error")
	}
	if got := s.URL(); got != "" {
		t.Errorf("URL() after failed Start = %q; want \"\"", got)
	}
}

func TestDevNoStore(t *testing.T) {
	dev := startServer(t, StaticFS(), Options{Dev: true})
	if cc := do(t, "GET", dev.URL()).header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("dev Cache-Control = %q; want no-store", cc)
	}
	prod := startServer(t, StaticFS(), Options{})
	if cc := do(t, "GET", prod.URL()).header.Get("Cache-Control"); cc != "" {
		t.Errorf("non-dev Cache-Control = %q; want none", cc)
	}
}

func TestContentTypes(t *testing.T) {
	assets := fstest.MapFS{
		"index.html": {Data: []byte(pageWithMarker(""))},
		"app.css":    {Data: []byte(":root{}")},
		"app.js":     {Data: []byte("// app")},
		"app.json":   {Data: []byte("{}")},
		"logo.svg":   {Data: []byte("<svg/>")},
		"LICENSE":    {Data: []byte("license text")},
	}
	s := startServer(t, assets, Options{})
	cases := []struct{ rel, want string }{
		{"index.html", "text/html; charset=utf-8"},
		{"app.css", "text/css; charset=utf-8"},
		{"app.js", "text/javascript; charset=utf-8"},
		{"app.json", "application/json"},
		{"logo.svg", "image/svg+xml"},
		{"LICENSE", "application/octet-stream"},
	}
	for _, tc := range cases {
		got := do(t, "GET", s.URL()+tc.rel)
		if got.status != http.StatusOK {
			t.Errorf("GET %s = %d; want 200", tc.rel, got.status)
			continue
		}
		if ct := got.header.Get("Content-Type"); ct != tc.want {
			t.Errorf("GET %s Content-Type = %q; want %q", tc.rel, ct, tc.want)
		}
	}
}

func TestPlaceholderIndex(t *testing.T) {
	data, err := fs.ReadFile(StaticFS(), "index.html")
	if err != nil {
		t.Fatalf("read embedded index.html: %v", err)
	}
	page := string(data)
	if !strings.Contains(page, "asset placeholder") {
		t.Error("placeholder body text missing")
	}
	if strings.Contains(page, "<script") {
		t.Error("placeholder index must not contain scripts")
	}
	marker := strings.Index(page, themeMarker)
	head := strings.Index(page, "</head>")
	if marker < 0 || head < 0 || marker > head {
		t.Errorf("theme marker at %d must be inside <head> (ends at %d)", marker, head)
	}
}
