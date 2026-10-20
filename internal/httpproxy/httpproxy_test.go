package httpproxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type dialCall struct {
	host string
	port int
}

// pipeTunnel gives net.Pipe the half-close shape the relay looks for.
type pipeTunnel struct{ net.Conn }

func (p *pipeTunnel) CloseWrite() error { return p.Conn.Close() }

func testServer(dial func(context.Context, string, int) (net.Conn, error)) *Server {
	return &Server{
		Bind:             "127.0.0.1",
		Dial:             dial,
		MaxConns:         16,
		HandshakeTimeout: 2 * time.Second,
		HalfCloseDrain:   500 * time.Millisecond,
		OpenTimeout:      2 * time.Second,
	}
}

func startServer(t *testing.T, s *Server) *Server {
	t.Helper()
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(s.Stop)
	return s
}

func dialProxy(t *testing.T, s *Server) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", s.Addr().String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// stubDial records the requested target and returns a pipe whose peer drains
// whatever the proxy writes.
func stubDial() (func(context.Context, string, int) (net.Conn, error), <-chan dialCall) {
	calls := make(chan dialCall, 16)
	return func(_ context.Context, host string, port int) (net.Conn, error) {
		calls <- dialCall{host: host, port: port}
		tunnel, peer := net.Pipe()
		go func() {
			defer peer.Close()
			_, _ = io.Copy(io.Discard, peer)
		}()
		return &pipeTunnel{Conn: tunnel}, nil
	}, calls
}

// dialTo records the target and connects to addr instead.
func dialTo(addr string) (func(context.Context, string, int) (net.Conn, error), <-chan dialCall) {
	calls := make(chan dialCall, 16)
	return func(ctx context.Context, host string, port int) (net.Conn, error) {
		calls <- dialCall{host: host, port: port}
		var d net.Dialer
		return d.DialContext(ctx, "tcp", addr)
	}, calls
}

func echoServer(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})
	return ln
}

func readHead(t *testing.T, br *bufio.Reader) (string, textproto.MIMEHeader) {
	t.Helper()
	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read status line: %v", err)
	}
	line = strings.TrimSuffix(line, "\r\n")
	h, err := textproto.NewReader(br).ReadMIMEHeader()
	if err != nil {
		t.Fatalf("read headers: %v", err)
	}
	return line, h
}

func expectStatus(t *testing.T, br *bufio.Reader, code int, text string) {
	t.Helper()
	line, h := readHead(t, br)
	want := fmt.Sprintf("HTTP/1.1 %d %s", code, text)
	if line != want {
		t.Fatalf("status line = %q, want %q", line, want)
	}
	if h.Get("Content-Length") != "0" {
		t.Fatalf("Content-Length = %q, want 0", h.Get("Content-Length"))
	}
	if h.Get("Connection") != "close" {
		t.Fatalf("Connection = %q, want close", h.Get("Connection"))
	}
}

func writeRaw(t *testing.T, c net.Conn, raw string) {
	t.Helper()
	if _, err := io.WriteString(c, raw); err != nil {
		t.Fatalf("write request: %v", err)
	}
}

func waitForConns(t *testing.T, s *Server, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		s.mu.Lock()
		got := len(s.conns)
		s.mu.Unlock()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("tracked connections = %d, want %d", got, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDefaults(t *testing.T) {
	s := &Server{}
	s.normalize()
	if s.Bind != "127.0.0.1" {
		t.Errorf("Bind = %q", s.Bind)
	}
	if s.MaxConns != 512 {
		t.Errorf("MaxConns = %d", s.MaxConns)
	}
	if s.HandshakeTimeout != 10*time.Second {
		t.Errorf("HandshakeTimeout = %v", s.HandshakeTimeout)
	}
	if s.HalfCloseDrain != 2*time.Minute {
		t.Errorf("HalfCloseDrain = %v", s.HalfCloseDrain)
	}
	if s.OpenTimeout != 30*time.Second {
		t.Errorf("OpenTimeout = %v", s.OpenTimeout)
	}
}

func TestStartRequiresDial(t *testing.T) {
	s := &Server{}
	if err := s.Start(); err == nil {
		s.Stop()
		t.Fatal("Start without Dial succeeded")
	}
}

func TestHandshakeTimeoutAnswers400(t *testing.T) {
	dial, _ := stubDial()
	s := testServer(dial)
	s.HandshakeTimeout = 100 * time.Millisecond
	startServer(t, s)

	c := dialProxy(t, s)
	br := bufio.NewReader(c)
	// A partial request line leaves the handler waiting for the deadline.
	writeRaw(t, c, "GET /")
	expectStatus(t, br, http.StatusBadRequest, "Bad Request")
}

func TestStartPortConflict(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	dial, _ := stubDial()
	s := testServer(dial)
	s.Port = port
	if err := s.Start(); err == nil {
		s.Stop()
		t.Fatal("Start on an occupied port succeeded")
	}
}

func TestConnectRelayEcho(t *testing.T) {
	echo := echoServer(t)
	dial, calls := dialTo(echo.Addr().String())
	s := startServer(t, testServer(dial))

	c := dialProxy(t, s)
	br := bufio.NewReader(c)
	writeRaw(t, c, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")
	line, _ := readHead(t, br)
	if line != "HTTP/1.1 200 Connection Established" {
		t.Fatalf("status line = %q", line)
	}
	call := <-calls
	if call.host != "example.com" || call.port != 443 {
		t.Fatalf("dial target = %+v", call)
	}

	writeRaw(t, c, "ping")
	got := make([]byte, 4)
	if _, err := io.ReadFull(br, got); err != nil {
		t.Fatalf("read echoed payload: %v", err)
	}
	if string(got) != "ping" {
		t.Fatalf("echo = %q", got)
	}

	writeRaw(t, c, "pong")
	if _, err := io.ReadFull(br, got); err != nil {
		t.Fatalf("read second echo: %v", err)
	}
	if string(got) != "pong" {
		t.Fatalf("echo = %q", got)
	}
}

// TestConnectBufferedPayload sends the CONNECT headers and payload in one
// write. The payload is buffered by the request reader and must be relayed
// from there, not from the bare client connection.
func TestConnectBufferedPayload(t *testing.T) {
	s := testServer(func(context.Context, string, int) (net.Conn, error) {
		tunnel, peer := net.Pipe()
		go func() {
			defer peer.Close()
			_, _ = io.Copy(peer, peer)
		}()
		return &pipeTunnel{Conn: tunnel}, nil
	})
	client, serverConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		s.handleConn(serverConn)
		close(done)
	}()
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))

	payload := "buffered-payload"
	writeRaw(t, client, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"+payload)
	br := bufio.NewReader(client)
	line, _ := readHead(t, br)
	if line != "HTTP/1.1 200 Connection Established" {
		t.Fatalf("status line = %q", line)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(br, got); err != nil {
		t.Fatalf("read buffered payload: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("payload = %q, want %q", got, payload)
	}

	_ = client.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not finish")
	}
}

func TestConnectTargetPorts(t *testing.T) {
	dial, calls := stubDial()
	s := startServer(t, testServer(dial))

	cases := []struct {
		authority string
		host      string
		port      int
	}{
		{"example.com", "example.com", 443},
		{"example.com:8443", "example.com", 8443},
		{"[2001:db8::1]", "2001:db8::1", 443},
		{"[2001:db8::1]:8443", "2001:db8::1", 8443},
		{"2001:db8::1", "2001:db8::1", 443},
	}
	for _, tc := range cases {
		c := dialProxy(t, s)
		br := bufio.NewReader(c)
		writeRaw(t, c, "CONNECT "+tc.authority+" HTTP/1.1\r\nHost: "+tc.authority+"\r\n\r\n")
		line, _ := readHead(t, br)
		if line != "HTTP/1.1 200 Connection Established" {
			t.Fatalf("%s: status line = %q", tc.authority, line)
		}
		call := <-calls
		if call.host != tc.host || call.port != tc.port {
			t.Fatalf("%s: dial target = %+v, want %s:%d", tc.authority, call, tc.host, tc.port)
		}
		_ = c.Close()
	}
}

func TestConnectLocalRefused(t *testing.T) {
	var dials atomic.Int32
	s := startServer(t, testServer(func(context.Context, string, int) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("must not dial")
	}))

	cases := []string{
		"127.0.0.1:80",
		"10.1.2.3:443",
		"172.16.0.1:443",
		"192.168.1.1",
		"169.254.1.1:80",
		"100.64.0.1:80",
		"[::1]:443",
		"[fc00::1]:443",
		"::1",
	}
	for _, authority := range cases {
		c := dialProxy(t, s)
		br := bufio.NewReader(c)
		writeRaw(t, c, "CONNECT "+authority+" HTTP/1.1\r\nHost: "+authority+"\r\n\r\n")
		expectStatus(t, br, http.StatusForbidden, "Forbidden")
		_ = c.Close()
	}
	if dials.Load() != 0 {
		t.Fatalf("dialer called %d times for local destinations", dials.Load())
	}
}

func TestConnectTargetErrors(t *testing.T) {
	var dials atomic.Int32
	s := startServer(t, testServer(func(context.Context, string, int) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("must not dial")
	}))

	cases := []struct {
		target string
		code   int
		text   string
	}{
		{"http://example.com/", http.StatusMethodNotAllowed, "Method Not Allowed"},
		{"/path", http.StatusMethodNotAllowed, "Method Not Allowed"},
		{"example.com/path", http.StatusMethodNotAllowed, "Method Not Allowed"},
		{":443", http.StatusBadRequest, "Bad Request"},
		{"example.com:abc", http.StatusBadRequest, "Bad Request"},
		{"example.com:0", http.StatusBadRequest, "Bad Request"},
		{"example.com:65536", http.StatusBadRequest, "Bad Request"},
		{"[example.com]:443", http.StatusBadRequest, "Bad Request"},
		{"[2001:db8::1", http.StatusBadRequest, "Bad Request"},
		{"2001:db8::zz", http.StatusBadRequest, "Bad Request"},
	}
	for _, tc := range cases {
		c := dialProxy(t, s)
		br := bufio.NewReader(c)
		writeRaw(t, c, "CONNECT "+tc.target+" HTTP/1.1\r\nHost: example.com\r\n\r\n")
		expectStatus(t, br, tc.code, tc.text)
		_ = c.Close()
	}
	if dials.Load() != 0 {
		t.Fatalf("dialer called %d times for refused targets", dials.Load())
	}
}

func TestConnectDialFailure(t *testing.T) {
	s := startServer(t, testServer(func(context.Context, string, int) (net.Conn, error) {
		return nil, errors.New("edge unavailable")
	}))
	c := dialProxy(t, s)
	br := bufio.NewReader(c)
	writeRaw(t, c, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")
	expectStatus(t, br, http.StatusBadGateway, "Bad Gateway")
}

type originRequest struct {
	method     string
	requestURI string
	host       string
	header     http.Header
	body       string
	close      bool
}

func TestAbsoluteFormGET(t *testing.T) {
	seen := make(chan originRequest, 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen <- originRequest{
			method:     r.Method,
			requestURI: r.RequestURI,
			host:       r.Host,
			header:     r.Header.Clone(),
			body:       string(body),
			close:      r.Close,
		}
		w.Header().Set("X-Origin", "yes")
		_, _ = io.WriteString(w, "hello")
	}))
	defer origin.Close()

	dial, calls := dialTo(origin.Listener.Addr().String())
	s := startServer(t, testServer(dial))

	c := dialProxy(t, s)
	br := bufio.NewReader(c)
	writeRaw(t, c, "GET http://origin.test/path?q=1 HTTP/1.1\r\n"+
		"Host: origin.test\r\n"+
		"Proxy-Connection: keep-alive\r\n"+
		"Proxy-Authorization: Basic c2VjcmV0\r\n"+
		"Connection: X-Hop\r\n"+
		"Keep-Alive: timeout=5\r\n"+
		"X-Hop: drop-me\r\n"+
		"X-Keep: keep-me\r\n"+
		"\r\n")

	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if string(body) != "hello" {
		t.Fatalf("body = %q", body)
	}
	if resp.Header.Get("X-Origin") != "yes" {
		t.Fatalf("origin response header missing: %v", resp.Header)
	}

	call := <-calls
	if call.host != "origin.test" || call.port != 80 {
		t.Fatalf("dial target = %+v, want origin.test:80", call)
	}
	got := <-seen
	if got.method != http.MethodGet {
		t.Errorf("origin method = %q", got.method)
	}
	if got.requestURI != "/path?q=1" {
		t.Errorf("origin request-target = %q, want origin-form", got.requestURI)
	}
	if got.host != "origin.test" {
		t.Errorf("origin Host = %q", got.host)
	}
	if !got.close {
		t.Error("origin request did not carry Connection: close")
	}
	if got.header.Get("Connection") != "close" {
		t.Errorf("origin Connection = %q, want close", got.header.Get("Connection"))
	}
	for _, name := range []string{"Proxy-Connection", "Proxy-Authorization", "Connection", "Keep-Alive", "X-Hop"} {
		if name == "Connection" {
			continue
		}
		if v := got.header.Get(name); v != "" {
			t.Errorf("origin received stripped header %s: %q", name, v)
		}
	}
	if got.header.Get("X-Keep") != "keep-me" {
		t.Errorf("origin lost end-to-end header X-Keep: %q", got.header.Get("X-Keep"))
	}
}

func TestAbsoluteFormPorts(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer origin.Close()

	dial, calls := dialTo(origin.Listener.Addr().String())
	s := startServer(t, testServer(dial))

	cases := []struct {
		target string
		host   string
		port   int
	}{
		{"http://origin.test/", "origin.test", 80},
		{"http://origin.test:8081/x", "origin.test", 8081},
		{"http://[2001:db8::2]/", "2001:db8::2", 80},
	}
	for _, tc := range cases {
		c := dialProxy(t, s)
		br := bufio.NewReader(c)
		writeRaw(t, c, "GET "+tc.target+" HTTP/1.1\r\nHost: origin.test\r\n\r\n")
		resp, err := http.ReadResponse(br, nil)
		if err != nil {
			t.Fatalf("%s: read response: %v", tc.target, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		call := <-calls
		if call.host != tc.host || call.port != tc.port {
			t.Fatalf("%s: dial target = %+v, want %s:%d", tc.target, call, tc.host, tc.port)
		}
		_ = c.Close()
	}
}

func TestAbsoluteFormPOSTBody(t *testing.T) {
	seen := make(chan originRequest, 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen <- originRequest{method: r.Method, requestURI: r.RequestURI, host: r.Host, header: r.Header.Clone(), body: string(body), close: r.Close}
		_, _ = fmt.Fprintf(w, "%d", len(body))
	}))
	defer origin.Close()

	dial, _ := dialTo(origin.Listener.Addr().String())
	s := startServer(t, testServer(dial))

	c := dialProxy(t, s)
	br := bufio.NewReader(c)
	payload := "hello world"
	writeRaw(t, c, "POST http://origin.test/upload HTTP/1.1\r\n"+
		"Host: origin.test\r\n"+
		"Content-Type: text/plain\r\n"+
		"Content-Length: "+strconv.Itoa(len(payload))+"\r\n"+
		"\r\n"+
		payload)

	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if string(body) != strconv.Itoa(len(payload)) {
		t.Fatalf("response body = %q", body)
	}
	got := <-seen
	if got.method != http.MethodPost {
		t.Errorf("origin method = %q", got.method)
	}
	if got.body != payload {
		t.Errorf("origin body = %q, want %q", got.body, payload)
	}
}

func TestAbsoluteFormDefaultAndLocalRefused(t *testing.T) {
	var dials atomic.Int32
	s := startServer(t, testServer(func(context.Context, string, int) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("must not dial")
	}))

	cases := []string{
		"http://127.0.0.1/",
		"http://10.1.2.3:8080/",
		"http://192.168.1.1/",
		"http://[::1]/",
		"http://[fc00::1]:8080/",
	}
	for _, target := range cases {
		c := dialProxy(t, s)
		br := bufio.NewReader(c)
		writeRaw(t, c, "GET "+target+" HTTP/1.1\r\nHost: local.test\r\n\r\n")
		expectStatus(t, br, http.StatusForbidden, "Forbidden")
		_ = c.Close()
	}
	if dials.Load() != 0 {
		t.Fatalf("dialer called %d times for local destinations", dials.Load())
	}
}

func TestAbsoluteFormRequestErrors(t *testing.T) {
	var dials atomic.Int32
	s := startServer(t, testServer(func(context.Context, string, int) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("must not dial")
	}))

	cases := []string{
		"GET / HTTP/1.1\r\nHost: origin.test\r\n\r\n",
		"GET https://origin.test/ HTTP/1.1\r\nHost: origin.test\r\n\r\n",
		"GET ftp://origin.test/ HTTP/1.1\r\nHost: origin.test\r\n\r\n",
		"GET http://origin.test:0/ HTTP/1.1\r\nHost: origin.test\r\n\r\n",
		"GET http://origin.test:65536/ HTTP/1.1\r\nHost: origin.test\r\n\r\n",
		"GET http://origin.test:abc/ HTTP/1.1\r\nHost: origin.test\r\n\r\n",
		"GET http:// HTTP/1.1\r\nHost: origin.test\r\n\r\n",
		"not a request\r\n\r\n",
	}
	for _, raw := range cases {
		c := dialProxy(t, s)
		br := bufio.NewReader(c)
		writeRaw(t, c, raw)
		expectStatus(t, br, http.StatusBadRequest, "Bad Request")
		_ = c.Close()
	}
	if dials.Load() != 0 {
		t.Fatalf("dialer called %d times for malformed requests", dials.Load())
	}
}

func TestAbsoluteFormDialFailure(t *testing.T) {
	s := startServer(t, testServer(func(context.Context, string, int) (net.Conn, error) {
		return nil, errors.New("edge unavailable")
	}))
	c := dialProxy(t, s)
	br := bufio.NewReader(c)
	writeRaw(t, c, "GET http://origin.test/ HTTP/1.1\r\nHost: origin.test\r\n\r\n")
	expectStatus(t, br, http.StatusBadGateway, "Bad Gateway")
}

func TestAbsoluteFormExpect100Continue(t *testing.T) {
	seen := make(chan originRequest, 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen <- originRequest{method: r.Method, requestURI: r.RequestURI, host: r.Host, header: r.Header.Clone(), body: string(body), close: r.Close}
		_, _ = io.WriteString(w, "ok")
	}))
	defer origin.Close()

	dial, _ := dialTo(origin.Listener.Addr().String())
	s := startServer(t, testServer(dial))

	c := dialProxy(t, s)
	br := bufio.NewReader(c)
	payload := "hello"
	writeRaw(t, c, "POST http://origin.test/expect HTTP/1.1\r\n"+
		"Host: origin.test\r\n"+
		"Content-Length: "+strconv.Itoa(len(payload))+"\r\n"+
		"Expect: 100-continue\r\n"+
		"\r\n")

	line, _ := readHead(t, br)
	if line != "HTTP/1.1 100 Continue" {
		t.Fatalf("interim status line = %q", line)
	}
	writeRaw(t, c, payload)

	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("final response = %d %q", resp.StatusCode, body)
	}
	got := <-seen
	if got.body != payload {
		t.Fatalf("origin body = %q, want %q", got.body, payload)
	}
	if v := got.header.Get("Expect"); v != "" {
		t.Fatalf("origin received Expect: %q", v)
	}
}

func TestMaxConns(t *testing.T) {
	dial, _ := stubDial()
	s := testServer(dial)
	s.MaxConns = 1
	startServer(t, s)

	first := dialProxy(t, s)
	waitForConns(t, s, 1)

	extra := dialProxy(t, s)
	_ = extra.SetReadDeadline(time.Now().Add(2 * time.Second))
	var buf [1]byte
	if _, err := extra.Read(buf[:]); err == nil {
		t.Fatal("extra connection was served despite MaxConns")
	}
	_ = extra.Close()

	_ = first.Close()
	waitForConns(t, s, 0)

	next := dialProxy(t, s)
	br := bufio.NewReader(next)
	writeRaw(t, next, "GET / HTTP/1.1\r\nHost: origin.test\r\n\r\n")
	expectStatus(t, br, http.StatusBadRequest, "Bad Request")
}

func TestOneRequestPerConnection(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "once")
	}))
	defer origin.Close()

	dial, _ := dialTo(origin.Listener.Addr().String())
	s := startServer(t, testServer(dial))

	c := dialProxy(t, s)
	br := bufio.NewReader(c)
	writeRaw(t, c, "GET http://origin.test/ HTTP/1.1\r\nHost: origin.test\r\n\r\n")
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "once" {
		t.Fatalf("body = %q", body)
	}
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := br.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("read after response = %v, want EOF", err)
	}
}

func TestStopUnblocksIdleConnection(t *testing.T) {
	dial, _ := stubDial()
	s := startServer(t, testServer(dial))

	c := dialProxy(t, s)
	waitForConns(t, s, 1)

	start := time.Now()
	s.Stop()
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Stop took %v", elapsed)
	}
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var buf [1]byte
	if _, err := c.Read(buf[:]); err == nil {
		t.Fatal("idle client was not closed by Stop")
	}
	s.Stop() // idempotent
}

func TestStopDoesNotWaitForBlockedDial(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	s := startServer(t, testServer(func(context.Context, string, int) (net.Conn, error) {
		once.Do(func() { close(started) })
		<-release
		return nil, errors.New("released")
	}))

	c := dialProxy(t, s)
	writeRaw(t, c, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("dial was not attempted")
	}

	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("Stop waited for a blocked dial")
	}
	close(release)
}
