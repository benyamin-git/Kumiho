package fastly

import (
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
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/logging"
)

const (
	originHost     = "api.accounts.test"
	apexHost       = "accounts.test"
	originBase     = "http://" + originHost
	apexBase       = "http://" + apexHost
	challengedPath = "/v1/account/login"
	testPrefix     = "/_fs-ch-Test123"
	cookieName     = "fastly_cookie"
	cookieValue    = "issued"
)

const challengedURL = originBase + challengedPath

// ---- fixture -------------------------------------------------------------

type recordedRequest struct {
	Method   string
	Host     string
	Path     string
	RawQuery string
	Header   http.Header
	Body     []byte
}

type fixture struct {
	t         *testing.T
	server    *httptest.Server
	transport *http.Transport
	mu        sync.Mutex
	requests  []recordedRequest
}

// newFixture starts a server whose handlers route on r.Host and a transport
// that sends every host to that server while preserving the requested Host.
func newFixture(t *testing.T, handler http.HandlerFunc) *fixture {
	t.Helper()
	f := &fixture{t: t}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		f.mu.Lock()
		f.requests = append(f.requests, recordedRequest{
			Method:   r.Method,
			Host:     r.Host,
			Path:     r.URL.Path,
			RawQuery: r.URL.RawQuery,
			Header:   r.Header.Clone(),
			Body:     body,
		})
		f.mu.Unlock()
		handler(w, r)
	}))
	f.transport = &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, f.server.Listener.Addr().String())
		},
	}
	t.Cleanup(func() {
		f.transport.CloseIdleConnections()
		f.server.Close()
	})
	return f
}

func (f *fixture) client() *http.Client {
	return &http.Client{Transport: f.transport}
}

func (f *fixture) newSolver(shared http.CookieJar) *Solver {
	return &Solver{
		HTTP:      f.client(),
		SharedJar: shared,
		sleep:     func(context.Context, time.Duration) error { return nil },
	}
}

func (f *fixture) snapshot() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

func (f *fixture) requestsFor(method, host, path string) []recordedRequest {
	var out []recordedRequest
	for _, req := range f.snapshot() {
		if req.Method == method && req.Host == host && req.Path == path {
			out = append(out, req)
		}
	}
	return out
}

// ---- challenge fixture ---------------------------------------------------

type scenario struct {
	challenges []map[string]any
	token      string
	// script overrides the generated init() script when non-empty.
	script string
	// challenged overrides challenge detection. Nil means "challenge unless
	// the post-back cookie is present".
	challenged func(*http.Request) bool

	pageStatus     int
	scriptStatus   int
	patStatus      int
	patAuth        string
	patBody        string
	postBackStatus int
	// pending makes the first N post-backs return a fresh ch/tok pair.
	pending int
	// successWithoutCookie omits the success Set-Cookie.
	successWithoutCookie bool
	// hostOnlyCookie omits the Domain attribute, so the cookie is host-only
	// for the host that served the post-back (the apex in the fallback test).
	hostOnlyCookie bool
	// clearOnSuccess makes the page clean after a success response even
	// without a cookie.
	clearOnSuccess bool
	afterPostBack  func()
}

func (sc scenario) handler(t *testing.T) http.HandlerFunc {
	t.Helper()

	token := sc.token
	if token == "" {
		token = "test-token"
	}
	script := sc.script
	if script == "" {
		script = initScript(t, sc.challenges, token)
	}
	challengesJSON := mustJSON(t, sc.challenges)
	patAuth := sc.patAuth
	if patAuth == "" {
		patAuth = "pat-auth"
	}
	patBody := sc.patBody
	if patBody == "" {
		patBody = `{"auth":` + jsonString(patAuth) + `}`
	}

	var pending atomic.Int32
	pending.Store(int32(sc.pending))
	var cleared atomic.Bool

	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			if sc.pageStatus != 0 {
				w.WriteHeader(sc.pageStatus)
				return
			}
			if cleared.Load() {
				writeCleanPage(w)
				return
			}
			challenged := true
			if sc.challenged != nil {
				challenged = sc.challenged(r)
			} else if _, err := r.Cookie(cookieName); err == nil {
				challenged = false
			}
			if challenged {
				writeChallengePage(w)
			} else {
				writeCleanPage(w)
			}

		case testPrefix + "/script.js":
			if sc.scriptStatus != 0 {
				w.WriteHeader(sc.scriptStatus)
				return
			}
			w.Header().Set("Content-Type", "application/javascript")
			io.WriteString(w, script)

		case testPrefix + "/pat":
			if sc.patStatus != 0 && sc.patStatus != http.StatusOK {
				w.WriteHeader(sc.patStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, patBody)

		case testPrefix + "/fst-post-back":
			if sc.postBackStatus != 0 {
				w.WriteHeader(sc.postBackStatus)
				return
			}
			if pending.Load() > 0 {
				n := pending.Add(-1)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"status":"pending","ch":%s,"tok":%s}`,
					challengesJSON, jsonString(fmt.Sprintf("next-token-%d", n)))
				if sc.afterPostBack != nil {
					sc.afterPostBack()
				}
				return
			}
			if !sc.successWithoutCookie {
				cookie := &http.Cookie{
					Name:  cookieName,
					Value: cookieValue,
					Path:  "/",
				}
				if !sc.hostOnlyCookie {
					cookie.Domain = apexHost
				}
				http.SetCookie(w, cookie)
			}
			if sc.clearOnSuccess {
				cleared.Store(true)
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"status":"success"}`)
			if sc.afterPostBack != nil {
				sc.afterPostBack()
			}

		default:
			http.NotFound(w, r)
		}
	}
}

func writeChallengePage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html><head><title>Client Challenge</title></head>`+
		`<body><p>Client Challenge</p><script src="%s/script.js?reload=true"></script></body></html>`,
		testPrefix)
}

func writeCleanPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, `<!doctype html><html><body>signed in</body></html>`)
}

func powChallenge(base, suffix string) map[string]any {
	sum := sha256.Sum256([]byte(base + suffix))
	return map[string]any{
		"ty": "pow",
		"data": map[string]any{
			"base":    base,
			"hash":    hex.EncodeToString(sum[:]),
			"hmac":    "hmac-value",
			"expires": 1893456000,
		},
	}
}

func initScript(t *testing.T, challenges []map[string]any, token string) string {
	t.Helper()
	return fmt.Sprintf("window.init(%s, %s, %s);",
		mustJSON(t, challenges), jsonString(token), jsonString(testPrefix))
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal fixture JSON: %v", err)
	}
	return string(raw)
}

func jsonString(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

// ---- assertions ----------------------------------------------------------

func newJar(t *testing.T) http.CookieJar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("new cookie jar: %v", err)
	}
	return jar
}

func requireCookie(t *testing.T, jar http.CookieJar, rawURL string) {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	for _, c := range jar.Cookies(u) {
		if c.Name == cookieName && c.Value == cookieValue {
			return
		}
	}
	t.Errorf("jar has no %s=%s cookie for %s (got %v)", cookieName, cookieValue, rawURL, jar.Cookies(u))
}

func requireNoCookie(t *testing.T, jar http.CookieJar, rawURL string) {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	if cookies := jar.Cookies(u); len(cookies) != 0 {
		t.Errorf("jar unexpectedly has cookies for %s: %v", rawURL, cookies)
	}
}

type postBackBody struct {
	Token string           `json:"token"`
	Data  []map[string]any `json:"data"`
}

func decodePostBack(t *testing.T, req recordedRequest) postBackBody {
	t.Helper()
	var body postBackBody
	if err := json.Unmarshal(req.Body, &body); err != nil {
		t.Fatalf("decode post-back body %q: %v", req.Body, err)
	}
	return body
}

func requireStatusError(t *testing.T, err error, contains string) {
	t.Helper()
	if err == nil {
		t.Fatal("Solve returned nil error")
	}
	if !strings.Contains(err.Error(), contains) {
		t.Fatalf("error = %q, want substring %q", err, contains)
	}
}

// ---- tests ---------------------------------------------------------------

func TestPowSuccess(t *testing.T) {
	sc := scenario{
		challenges: []map[string]any{powChallenge("base-1", "Z9")},
		token:      "tok-pow",
	}
	f := newFixture(t, sc.handler(t))
	shared := newJar(t)
	original := newJar(t)

	s := f.newSolver(shared)
	s.HTTP.Jar = original

	var logs []string
	s.Logf = func(level logging.Level, tag, format string, args ...any) {
		if level != logging.Warn || tag != logTag {
			t.Errorf("log = level %v tag %q, want warn/fastly", level, tag)
		}
		logs = append(logs, fmt.Sprintf(format, args...))
	}

	if err := s.Solve(context.Background(), challengedURL); err != nil {
		t.Fatalf("Solve: %v", err)
	}

	pages := f.requestsFor("GET", originHost, "/")
	if len(pages) != 2 {
		t.Fatalf("origin page requests = %d, want 2 (challenge + verification)", len(pages))
	}
	if got := pages[0].Header.Get("User-Agent"); got != defaultUserAgent {
		t.Errorf("page User-Agent = %q, want the default solver UA", got)
	}
	if got := pages[0].Header.Get("Accept"); got != htmlAccept {
		t.Errorf("page Accept = %q, want %q", got, htmlAccept)
	}
	if got := pages[0].Header.Get("Cookie"); got != "" {
		t.Errorf("initial page Cookie = %q, want none", got)
	}
	if got := pages[1].Header.Get("Cookie"); !strings.Contains(got, cookieName+"="+cookieValue) {
		t.Errorf("verification Cookie = %q, want the issued challenge cookie", got)
	}

	scripts := f.requestsFor("GET", originHost, testPrefix+"/script.js")
	if len(scripts) != 1 {
		t.Fatalf("script requests = %d, want 1", len(scripts))
	}
	if got := scripts[0].RawQuery; got != "reload=true" {
		t.Errorf("script query = %q, want reload=true", got)
	}
	if got := scripts[0].Header.Get("Accept"); got != htmlAccept {
		t.Errorf("script Accept = %q, want %q", got, htmlAccept)
	}
	if got := scripts[0].Header.Get("User-Agent"); got != defaultUserAgent {
		t.Errorf("script User-Agent = %q, want the default solver UA", got)
	}

	posts := f.requestsFor("POST", originHost, testPrefix+"/fst-post-back")
	if len(posts) != 1 {
		t.Fatalf("post-back requests = %d, want 1", len(posts))
	}
	if got := posts[0].Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("post-back Content-Type = %q", got)
	}
	if got := posts[0].Header.Get("Accept"); got != "application/json" {
		t.Errorf("post-back Accept = %q", got)
	}
	if got := posts[0].Header.Get("Origin"); got != originBase {
		t.Errorf("post-back Origin = %q, want %q", got, originBase)
	}
	if got := posts[0].Header.Get("User-Agent"); got != defaultUserAgent {
		t.Errorf("post-back User-Agent = %q, want the default solver UA", got)
	}

	pb := decodePostBack(t, posts[0])
	if pb.Token != "tok-pow" {
		t.Errorf("post-back token = %q, want tok-pow", pb.Token)
	}
	if len(pb.Data) != 1 {
		t.Fatalf("post-back answers = %d, want 1", len(pb.Data))
	}
	answer := pb.Data[0]
	if answer["ty"] != "pow" || answer["base"] != "base-1" || answer["answer"] != "Z9" {
		t.Errorf("pow answer = %#v", answer)
	}
	if answer["hmac"] != "hmac-value" {
		t.Errorf("pow hmac = %#v, want the received string", answer["hmac"])
	}
	if answer["expires"] != float64(1893456000) {
		t.Errorf("pow expires = %#v (%T), want the received JSON number", answer["expires"], answer["expires"])
	}

	requireCookie(t, shared, originBase+"/")
	requireCookie(t, shared, apexBase+"/")
	requireCookie(t, shared, challengedURL)
	requireNoCookie(t, original, originBase+"/")

	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "solving Fastly challenge") {
		t.Errorf("logs do not record the attempt: %v", logs)
	}
	if !strings.Contains(joined, "challenge round 1/3") {
		t.Errorf("logs do not record the round: %v", logs)
	}
}

func TestApexFallback(t *testing.T) {
	cases := []struct {
		name     string
		hostOnly bool
	}{
		{name: "domain cookie"},
		{name: "host-only apex cookie", hostOnly: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sc := scenario{
				challenges:     []map[string]any{powChallenge("base-apex", "aa")},
				token:          "tok-apex",
				hostOnlyCookie: tc.hostOnly,
				challenged: func(r *http.Request) bool {
					if r.Host == originHost {
						return false
					}
					_, err := r.Cookie(cookieName)
					return err != nil
				},
			}
			f := newFixture(t, sc.handler(t))
			shared := newJar(t)
			s := f.newSolver(shared)

			if err := s.Solve(context.Background(), challengedURL); err != nil {
				t.Fatalf("Solve: %v", err)
			}

			if got := len(f.requestsFor("GET", originHost, "/")); got != 1 {
				t.Errorf("origin page requests = %d, want 1", got)
			}
			if got := len(f.requestsFor("GET", originHost, testPrefix+"/script.js")); got != 0 {
				t.Errorf("origin script requests = %d, want 0", got)
			}
			if got := len(f.requestsFor("GET", apexHost, "/")); got != 2 {
				t.Errorf("apex page requests = %d, want 2 (challenge + verification)", got)
			}
			if got := len(f.requestsFor("GET", apexHost, testPrefix+"/script.js")); got != 1 {
				t.Errorf("apex script requests = %d, want 1", got)
			}
			posts := f.requestsFor("POST", apexHost, testPrefix+"/fst-post-back")
			if len(posts) != 1 {
				t.Fatalf("apex post-back requests = %d, want 1", len(posts))
			}
			if got := posts[0].Header.Get("Origin"); got != apexBase {
				t.Errorf("apex post-back Origin = %q, want %q", got, apexBase)
			}

			requireCookie(t, shared, originBase+"/")
			requireCookie(t, shared, apexBase+"/")
			requireCookie(t, shared, challengedURL)
		})
	}
}

func TestNoChallenge(t *testing.T) {
	sc := scenario{
		challenges: []map[string]any{powChallenge("unused", "aa")},
		challenged: func(*http.Request) bool { return false },
	}
	f := newFixture(t, sc.handler(t))
	shared := newJar(t)
	s := f.newSolver(shared)

	err := s.Solve(context.Background(), challengedURL)
	if !errors.Is(err, ErrNoChallenge) {
		t.Fatalf("error = %v, want ErrNoChallenge", err)
	}
	if got := len(f.requestsFor("GET", originHost, "/")); got != 1 {
		t.Errorf("origin page requests = %d, want 1", got)
	}
	if got := len(f.requestsFor("GET", apexHost, "/")); got != 1 {
		t.Errorf("apex page requests = %d, want 1", got)
	}
	if got := len(f.requestsFor("GET", originHost, testPrefix+"/script.js")); got != 0 {
		t.Errorf("script requests = %d, want 0", got)
	}
	if got := len(f.requestsFor("POST", originHost, testPrefix+"/fst-post-back")); got != 0 {
		t.Errorf("post-back requests = %d, want 0", got)
	}
	requireNoCookie(t, shared, originBase+"/")
}

func TestOriginFailureDoesNotTryApex(t *testing.T) {
	cases := []struct {
		name       string
		challenges []map[string]any
		wantErr    error
		wantMsg    string
	}{
		{
			name:       "captcha",
			challenges: []map[string]any{{"ty": "captcha", "data": map[string]any{}}},
			wantErr:    ErrUnsupported,
			wantMsg:    "captcha",
		},
		{
			name: "malformed pow",
			challenges: []map[string]any{{
				"ty": "pow",
				"data": map[string]any{
					"base": "base",
					"hash": "not-hex",
				},
			}},
			wantMsg: "proof-of-work hash",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sc := scenario{challenges: tc.challenges, token: "tok-origin"}
			f := newFixture(t, sc.handler(t))
			s := f.newSolver(newJar(t))

			err := s.Solve(context.Background(), challengedURL)
			if err == nil {
				t.Fatal("Solve returned nil error")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("error = %v, want %v", err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error = %q, want substring %q", err, tc.wantMsg)
			}
			if got := len(f.requestsFor("GET", apexHost, "/")); got != 0 {
				t.Errorf("apex page requests = %d, want 0 after an origin solve failure", got)
			}
		})
	}
}

func TestPAT(t *testing.T) {
	const token = "tok en+1"

	cases := []struct {
		name     string
		status   int
		wantAuth string
	}{
		{name: "ok", status: http.StatusOK, wantAuth: "pat-auth"},
		{name: "bad request", status: http.StatusBadRequest, wantAuth: ""},
		{name: "unauthorized", status: http.StatusUnauthorized, wantAuth: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sc := scenario{
				challenges: []map[string]any{{"ty": "pat", "data": map[string]any{}}},
				token:      token,
				patStatus:  tc.status,
				patAuth:    "pat-auth",
			}
			f := newFixture(t, sc.handler(t))
			shared := newJar(t)
			s := f.newSolver(shared)
			s.UserAgent = "kumiho-test/1.0"

			if err := s.Solve(context.Background(), challengedURL); err != nil {
				t.Fatalf("Solve: %v", err)
			}

			pats := f.requestsFor("POST", originHost, testPrefix+"/pat")
			if len(pats) != 1 {
				t.Fatalf("pat requests = %d, want 1", len(pats))
			}
			pat := pats[0]
			if got := pat.RawQuery; got != "token=tok+en%2B1" {
				t.Errorf("pat query = %q, want the URL-encoded token", got)
			}
			if got := pat.Header.Get("Accept"); got != "text/plain" {
				t.Errorf("pat Accept = %q, want text/plain", got)
			}
			if got := pat.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("pat Content-Type = %q, want application/json", got)
			}
			if got := pat.Header.Get("Origin"); got != originBase {
				t.Errorf("pat Origin = %q, want %q", got, originBase)
			}
			if got := pat.Header.Get("User-Agent"); got != "kumiho-test/1.0" {
				t.Errorf("pat User-Agent = %q, want the configured UA", got)
			}
			if len(pat.Body) != 0 {
				t.Errorf("pat body = %q, want empty", pat.Body)
			}

			posts := f.requestsFor("POST", originHost, testPrefix+"/fst-post-back")
			if len(posts) != 1 {
				t.Fatalf("post-back requests = %d, want 1", len(posts))
			}
			pb := decodePostBack(t, posts[0])
			if pb.Token != token {
				t.Errorf("post-back token = %q, want %q", pb.Token, token)
			}
			if len(pb.Data) != 1 {
				t.Fatalf("post-back answers = %d, want 1", len(pb.Data))
			}
			if got := pb.Data[0]["auth"]; got != tc.wantAuth {
				t.Errorf("pat answer auth = %#v, want %q", got, tc.wantAuth)
			}
			requireCookie(t, shared, originBase+"/")
		})
	}
}

func TestPATErrors(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		wantMsg   string
		wantPosts int
	}{
		{name: "http 500", status: http.StatusInternalServerError, wantMsg: "pat request", wantPosts: 0},
		{name: "empty auth", status: http.StatusOK, body: `{"auth":""}`, wantMsg: "empty pat auth", wantPosts: 0},
		{name: "invalid json", status: http.StatusOK, body: `not json`, wantMsg: "parse pat response", wantPosts: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sc := scenario{
				challenges: []map[string]any{{"ty": "pat", "data": map[string]any{}}},
				token:      "tok-pat",
				patStatus:  tc.status,
				patBody:    tc.body,
			}
			f := newFixture(t, sc.handler(t))
			s := f.newSolver(newJar(t))

			err := s.Solve(context.Background(), challengedURL)
			requireStatusError(t, err, tc.wantMsg)
			if got := len(f.requestsFor("POST", originHost, testPrefix+"/fst-post-back")); got != tc.wantPosts {
				t.Errorf("post-back requests = %d, want %d", got, tc.wantPosts)
			}
		})
	}
}

func TestClientMetrics(t *testing.T) {
	sc := scenario{
		challenges: []map[string]any{{"ty": "clientmetrics", "data": map[string]any{}}},
		token:      "tok-metrics",
	}
	f := newFixture(t, sc.handler(t))
	s := f.newSolver(newJar(t))

	if err := s.Solve(context.Background(), challengedURL); err != nil {
		t.Fatalf("Solve: %v", err)
	}

	posts := f.requestsFor("POST", originHost, testPrefix+"/fst-post-back")
	if len(posts) != 1 {
		t.Fatalf("post-back requests = %d, want 1", len(posts))
	}
	pb := decodePostBack(t, posts[0])
	if len(pb.Data) != 1 {
		t.Fatalf("post-back answers = %d, want 1", len(pb.Data))
	}
	want := map[string]any{
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
		"v":                float64(2),
	}
	if !reflect.DeepEqual(pb.Data[0], want) {
		t.Errorf("clientmetrics answer = %#v, want the static reference payload %#v", pb.Data[0], want)
	}
}

func TestUnsupportedTypes(t *testing.T) {
	for _, ty := range []string{"captcha", "unknown"} {
		t.Run(ty, func(t *testing.T) {
			sc := scenario{
				challenges: []map[string]any{{"ty": ty, "data": map[string]any{}}},
				token:      "tok-unsupported",
			}
			f := newFixture(t, sc.handler(t))
			s := f.newSolver(newJar(t))

			err := s.Solve(context.Background(), challengedURL)
			if !errors.Is(err, ErrUnsupported) {
				t.Fatalf("error = %v, want ErrUnsupported", err)
			}
			if !strings.Contains(err.Error(), ty) {
				t.Errorf("error = %q, want the challenge type %q", err, ty)
			}
			if got := len(f.requestsFor("POST", originHost, testPrefix+"/fst-post-back")); got != 0 {
				t.Errorf("post-back requests = %d, want 0", got)
			}
		})
	}
}

func TestDisabled(t *testing.T) {
	var roundTrips atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		roundTrips.Add(1)
		return nil, errors.New("unexpected request")
	})}

	s := &Solver{
		HTTP:      client,
		SharedJar: nil, // must not be validated before Enabled
		Enabled:   func() bool { return false },
	}
	err := s.Solve(context.Background(), "not-even-a-url")
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("error = %v, want ErrDisabled", err)
	}
	if got := roundTrips.Load(); got != 0 {
		t.Errorf("round trips = %d, want 0", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRoundCapAndJitter(t *testing.T) {
	sc := scenario{
		challenges: []map[string]any{powChallenge("base-rounds", "aa")},
		token:      "tok-rounds",
		pending:    10,
	}
	f := newFixture(t, sc.handler(t))
	s := f.newSolver(newJar(t))
	fixed := time.Unix(1_700_000_000, 0)
	s.Now = func() time.Time { return fixed }
	s.Rand = rand.New(rand.NewSource(7))

	var sleeps []time.Duration
	s.sleep = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}

	err := s.Solve(context.Background(), challengedURL)
	requireStatusError(t, err, "did not complete within 3 post-back rounds")

	if got := len(f.requestsFor("POST", originHost, testPrefix+"/fst-post-back")); got != maxPostBackRounds {
		t.Errorf("post-back requests = %d, want %d", got, maxPostBackRounds)
	}
	if len(sleeps) != maxPostBackRounds-1 {
		t.Fatalf("jitter sleeps = %d, want %d", len(sleeps), maxPostBackRounds-1)
	}
	for i, d := range sleeps {
		if d < 250*time.Millisecond || d > 750*time.Millisecond {
			t.Errorf("jitter[%d] = %v, want [250ms, 750ms]", i, d)
		}
	}
}

func TestBudgetCap(t *testing.T) {
	var advanced atomic.Bool
	sc := scenario{
		challenges: []map[string]any{powChallenge("base-budget", "aa")},
		token:      "tok-budget",
		pending:    1,
		afterPostBack: func() {
			advanced.Store(true)
		},
	}
	f := newFixture(t, sc.handler(t))
	s := f.newSolver(newJar(t))
	base := time.Unix(1_700_000_000, 0)
	s.Now = func() time.Time {
		if advanced.Load() {
			return base.Add(solveBudget + time.Second)
		}
		return base
	}

	err := s.Solve(context.Background(), challengedURL)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want a deadline/budget error", err)
	}
	if !strings.Contains(err.Error(), "budget") {
		t.Errorf("error = %q, want it to mention the budget", err)
	}
	if got := len(f.requestsFor("POST", originHost, testPrefix+"/fst-post-back")); got != 1 {
		t.Errorf("post-back requests = %d, want 1 before the budget trip", got)
	}
}

func TestInitParsingErrors(t *testing.T) {
	cases := []struct {
		name   string
		script string
		want   string
	}{
		{name: "no init", script: `console.log("no challenge here");`, want: "init() call not found"},
		{name: "empty list", script: `window.init([], "token", "/_fs-ch-x");`, want: "empty challenge list"},
		{name: "empty token", script: `window.init([{"ty":"pow","data":{}}], "", "/_fs-ch-x");`, want: "empty challenge token"},
		{name: "invalid json", script: `window.init([not-json], "token", "/_fs-ch-x");`, want: "parse challenge list"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sc := scenario{
				challenges: []map[string]any{powChallenge("unused", "aa")},
				script:     tc.script,
			}
			f := newFixture(t, sc.handler(t))
			s := f.newSolver(newJar(t))

			err := s.Solve(context.Background(), challengedURL)
			requireStatusError(t, err, tc.want)
			if got := len(f.requestsFor("POST", originHost, testPrefix+"/fst-post-back")); got != 0 {
				t.Errorf("post-back requests = %d, want 0", got)
			}
		})
	}
}

func TestLastInitWins(t *testing.T) {
	first := initScript(t, []map[string]any{{"ty": "captcha", "data": map[string]any{}}}, "first-token")
	second := initScript(t, []map[string]any{powChallenge("base-last", "aa")}, "last-token")

	sc := scenario{
		challenges: []map[string]any{powChallenge("unused", "aa")},
		script:     first + "\n" + second,
	}
	f := newFixture(t, sc.handler(t))
	s := f.newSolver(newJar(t))

	if err := s.Solve(context.Background(), challengedURL); err != nil {
		t.Fatalf("Solve: %v", err)
	}
	posts := f.requestsFor("POST", originHost, testPrefix+"/fst-post-back")
	if len(posts) != 1 {
		t.Fatalf("post-back requests = %d, want 1", len(posts))
	}
	pb := decodePostBack(t, posts[0])
	if pb.Token != "last-token" {
		t.Errorf("post-back token = %q, want last-token", pb.Token)
	}
	if len(pb.Data) != 1 || pb.Data[0]["ty"] != "pow" {
		t.Errorf("post-back answers = %#v, want the pow challenge from the last init call", pb.Data)
	}
}

func TestSolvePow(t *testing.T) {
	base := "direct-base"
	answer, err := solvePow(base, hex.EncodeToString(powHash(base, "Z9")))
	if err != nil {
		t.Fatalf("solvePow: %v", err)
	}
	if answer != "Z9" {
		t.Errorf("solvePow answer = %q, want Z9", answer)
	}

	cases := []struct {
		name string
		hash string
		want string
	}{
		{name: "invalid hex", hash: "zz", want: "invalid proof-of-work"},
		{name: "wrong length", hash: "abcd", want: "want 32"},
		{name: "no solution", hash: strings.Repeat("00", sha256.Size), want: "no proof-of-work solution"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := solvePow(base, tc.hash)
			requireStatusError(t, err, tc.want)
		})
	}
}

func powHash(base, suffix string) []byte {
	sum := sha256.Sum256([]byte(base + suffix))
	return sum[:]
}

func TestHTTPErrors(t *testing.T) {
	cases := []struct {
		name           string
		pageStatus     int
		scriptStatus   int
		postBackStatus int
		wantMsg        string
		wantScripts    int
		wantPostBacks  int
	}{
		{
			name:          "page 500",
			pageStatus:    http.StatusInternalServerError,
			wantMsg:       "HTTP 500",
			wantPostBacks: 0,
		},
		{
			name:          "script 500",
			scriptStatus:  http.StatusInternalServerError,
			wantMsg:       "HTTP 500",
			wantScripts:   1,
			wantPostBacks: 0,
		},
		{
			name:           "post-back 500",
			postBackStatus: http.StatusInternalServerError,
			wantMsg:        "HTTP 500",
			wantScripts:    1,
			wantPostBacks:  1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sc := scenario{
				challenges:     []map[string]any{powChallenge("base-http", "aa")},
				token:          "tok-http",
				pageStatus:     tc.pageStatus,
				scriptStatus:   tc.scriptStatus,
				postBackStatus: tc.postBackStatus,
			}
			f := newFixture(t, sc.handler(t))
			s := f.newSolver(newJar(t))

			err := s.Solve(context.Background(), challengedURL)
			requireStatusError(t, err, tc.wantMsg)
			if got := len(f.requestsFor("GET", originHost, testPrefix+"/script.js")); got != tc.wantScripts {
				t.Errorf("script requests = %d, want %d", got, tc.wantScripts)
			}
			if got := len(f.requestsFor("POST", originHost, testPrefix+"/fst-post-back")); got != tc.wantPostBacks {
				t.Errorf("post-back requests = %d, want %d", got, tc.wantPostBacks)
			}
		})
	}
}

func TestNoCookiesIssued(t *testing.T) {
	sc := scenario{
		challenges:           []map[string]any{powChallenge("base-nocookie", "aa")},
		token:                "tok-nocookie",
		successWithoutCookie: true,
		clearOnSuccess:       true,
	}
	f := newFixture(t, sc.handler(t))
	shared := newJar(t)
	s := f.newSolver(shared)

	err := s.Solve(context.Background(), challengedURL)
	requireStatusError(t, err, "no cookies were issued")
	if got := len(f.requestsFor("GET", originHost, "/")); got != 2 {
		t.Errorf("page requests = %d, want 2", got)
	}
	requireNoCookie(t, shared, originBase+"/")
}

func TestChallengeCookieRejected(t *testing.T) {
	sc := scenario{
		challenges: []map[string]any{powChallenge("base-rejected", "aa")},
		token:      "tok-rejected",
		challenged: func(*http.Request) bool { return true },
	}
	f := newFixture(t, sc.handler(t))
	shared := newJar(t)
	s := f.newSolver(shared)

	err := s.Solve(context.Background(), challengedURL)
	requireStatusError(t, err, "not accepted")
	if got := len(f.requestsFor("GET", originHost, "/")); got != 2 {
		t.Errorf("page requests = %d, want 2", got)
	}
	requireNoCookie(t, shared, originBase+"/")
}

func TestValidation(t *testing.T) {
	jar := newJar(t)
	client := &http.Client{}
	cases := []struct {
		name    string
		solver  Solver
		url     string
		wantMsg string
	}{
		{
			name:    "missing HTTP",
			solver:  Solver{SharedJar: jar},
			url:     challengedURL,
			wantMsg: "HTTP is required",
		},
		{
			name:    "missing SharedJar",
			solver:  Solver{HTTP: client},
			url:     challengedURL,
			wantMsg: "SharedJar is required",
		},
		{
			name:    "relative URL",
			solver:  Solver{HTTP: client, SharedJar: jar},
			url:     "not a url",
			wantMsg: "absolute http or https",
		},
		{
			name:    "ftp URL",
			solver:  Solver{HTTP: client, SharedJar: jar},
			url:     "ftp://example.com/file",
			wantMsg: "absolute http or https",
		},
		{
			name:    "missing host",
			solver:  Solver{HTTP: client, SharedJar: jar},
			url:     "https:///path",
			wantMsg: "absolute http or https",
		},
		{
			name:    "unparsable URL",
			solver:  Solver{HTTP: client, SharedJar: jar},
			url:     "http://[::1",
			wantMsg: "parse challenged URL",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.solver.Solve(context.Background(), tc.url)
			requireStatusError(t, err, tc.wantMsg)
			if errors.Is(err, ErrDisabled) {
				t.Errorf("nil Enabled must mean enabled, got ErrDisabled")
			}
		})
	}
}

func TestApexOrigin(t *testing.T) {
	cases := []struct {
		raw    string
		want   string
		wantOK bool
	}{
		{raw: "https://api.accounts.firefox.com/v1/x", want: "https://accounts.firefox.com", wantOK: true},
		{raw: "https://www.example.com", want: "https://example.com", wantOK: true},
		{raw: "https://api.example.com:8443/x", want: "https://example.com:8443", wantOK: true},
		{raw: "https://API.Example.COM/x", want: "https://Example.COM", wantOK: true},
		{raw: "https://example.com/x"},
		{raw: "https://www./x"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			u, err := url.Parse(tc.raw)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got, ok := apexOrigin(u)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("apexOrigin(%q) = %q, %v; want %q, %v", tc.raw, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestConcurrentSolve(t *testing.T) {
	challenge := powChallenge("concurrent-base", "aa")
	challengesJSON := mustJSON(t, []map[string]any{challenge})
	script := initScript(t, []map[string]any{challenge}, "concurrent-token")
	pendingBody := fmt.Sprintf(`{"status":"pending","ch":%s,"tok":"concurrent-token"}`, challengesJSON)

	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			if _, err := r.Cookie(cookieName); err == nil {
				writeCleanPage(w)
				return
			}
			writeChallengePage(w)
		case testPrefix + "/script.js":
			w.Header().Set("Content-Type", "application/javascript")
			io.WriteString(w, script)
		case testPrefix + "/fst-post-back":
			if _, err := r.Cookie("round"); err != nil {
				http.SetCookie(w, &http.Cookie{Name: "round", Value: "1", Path: "/"})
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, pendingBody)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name:   cookieName,
				Value:  cookieValue,
				Path:   "/",
				Domain: apexHost,
			})
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"status":"success"}`)
		default:
			http.NotFound(w, r)
		}
	})

	shared := newJar(t)
	s := &Solver{
		HTTP:      f.client(),
		SharedJar: shared,
		Rand:      rand.New(rand.NewSource(99)),
		sleep:     func(context.Context, time.Duration) error { return nil },
	}

	const goroutines = 8
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.Solve(context.Background(), challengedURL)
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("concurrent Solve: %v", err)
		}
	}
	requireCookie(t, shared, originBase+"/")
}
