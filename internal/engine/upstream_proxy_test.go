package engine

// Tests for the upstream proxy (T4.1): the edge dial tunnels through a
// configured SOCKS5/HTTP proxy with remote resolution and optional auth.
// Everything is in-process: local proxy listeners, local targets, no fixed
// ports, no external network.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/serverlist"
	"github.com/benyamin-git/kumiho/internal/settings"
	"github.com/benyamin-git/kumiho/internal/upstream"
)

// -- in-process targets ------------------------------------------------------

// startEchoServer runs a plain TCP echo listener on 127.0.0.1:0 and returns
// its address.
func startEchoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	return ln.Addr().String()
}

// startTLSEchoServer runs a TLS echo listener with a self-signed cert and
// records the SNI hostname of the last ClientHello.
func startTLSEchoServer(t *testing.T, sni *string) string {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"test"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	tlsLn := tls.NewListener(ln, &tls.Config{
		Certificates: []tls.Certificate{cert},
		GetConfigForClient: func(chi *tls.ClientHelloInfo) (*tls.Config, error) {
			*sni = chi.ServerName
			return nil, nil
		},
	})
	go func() {
		for {
			conn, err := tlsLn.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	return ln.Addr().String()
}

// restrictedDialer returns a base dialer that can only reach the proxy
// address: any other direct dial fails, so a successful proxied dial to an
// unresolvable hostname proves the client never resolved it locally.
func restrictedDialer(proxyAddr string) *net.Dialer {
	return &net.Dialer{Control: func(network, address string, _ syscall.RawConn) error {
		if address != proxyAddr {
			return fmt.Errorf("test: base dialer refuses direct dial to %s", address)
		}
		return nil
	}}
}

// -- in-process SOCKS5 proxy -------------------------------------------------

type socks5Config struct {
	user, pass   string // credentials the proxy demands ("" = no auth required)
	connectReply byte   // reply status for CONNECT (0 = success)
}

type socks5Record struct {
	methods  []byte
	selected byte
	user     string
	pass     string
	atyp     byte
	host     string
	port     int
}

// startSOCKS5Proxy runs an in-process SOCKS5 proxy. It records the handshake
// and always tunnels to a fixed target (never resolving the requested host),
// which is exactly what a remote-resolving proxy does.
func startSOCKS5Proxy(t *testing.T, cfg socks5Config, target string) (*socks5Record, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	rec := &socks5Record{}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleSOCKS5(conn, cfg, target, rec)
		}
	}()
	return rec, ln.Addr().String()
}

func handleSOCKS5(conn net.Conn, cfg socks5Config, target string, rec *socks5Record) {
	defer conn.Close()
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil || head[0] != 0x05 {
		return
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}
	rec.methods = append([]byte(nil), methods...)
	want := byte(0x00)
	if cfg.user != "" {
		if !bytes.Contains(methods, []byte{0x02}) {
			_, _ = conn.Write([]byte{0x05, 0xFF})
			return
		}
		want = 0x02
	}
	rec.selected = want
	if _, err := conn.Write([]byte{0x05, want}); err != nil {
		return
	}
	if want == 0x02 {
		sub := make([]byte, 2)
		if _, err := io.ReadFull(conn, sub); err != nil || sub[0] != 0x01 {
			return
		}
		user := make([]byte, sub[1])
		if _, err := io.ReadFull(conn, user); err != nil {
			return
		}
		head2 := make([]byte, 1)
		if _, err := io.ReadFull(conn, head2); err != nil {
			return
		}
		pass := make([]byte, head2[0])
		if _, err := io.ReadFull(conn, pass); err != nil {
			return
		}
		rec.user, rec.pass = string(user), string(pass)
		if rec.user != cfg.user || rec.pass != cfg.pass {
			_, _ = conn.Write([]byte{0x01, 0x01}) // auth failure
			return
		}
		if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
			return
		}
	}

	chead := make([]byte, 4)
	if _, err := io.ReadFull(conn, chead); err != nil || chead[0] != 0x05 || chead[1] != 0x01 {
		return
	}
	rec.atyp = chead[3]
	var host []byte
	switch chead[3] {
	case 0x01:
		host = make([]byte, 4)
	case 0x04:
		host = make([]byte, 16)
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			return
		}
		host = make([]byte, l[0])
	default:
		return
	}
	if _, err := io.ReadFull(conn, host); err != nil {
		return
	}
	pb := make([]byte, 2)
	if _, err := io.ReadFull(conn, pb); err != nil {
		return
	}
	// Record the address in textual form (dotted-quad / IPv6 literal).
	switch chead[3] {
	case 0x01, 0x04:
		rec.host = net.IP(host).String()
	default:
		rec.host = string(host)
	}
	rec.port = int(pb[0])<<8 | int(pb[1])

	reply := []byte{0x05, cfg.connectReply, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	if _, err := conn.Write(reply); err != nil || cfg.connectReply != 0 {
		return
	}
	upstreamConn, err := net.Dial("tcp", target)
	if err != nil {
		return
	}
	defer upstreamConn.Close()
	go func() { _, _ = io.Copy(upstreamConn, conn) }()
	_, _ = io.Copy(conn, upstreamConn)
}

// -- in-process HTTP CONNECT proxy -------------------------------------------

type httpConnectRecord struct {
	mu     sync.Mutex
	method string
	target string
	host   string
	auth   string
}

func (r *httpConnectRecord) snapshot() (method, target, host, auth string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.method, r.target, r.host, r.auth
}

// startHTTPProxy runs an in-process HTTP CONNECT proxy answering with the
// given status; extra bytes (if any) are appended right after the response
// headers on a 200. On 200 it tunnels to the fixed target.
func startHTTPProxy(t *testing.T, status int, extra []byte, target string) (*httpConnectRecord, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	rec := &httpConnectRecord{}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				br := bufio.NewReader(conn)
				req, err := http.ReadRequest(br)
				if err != nil {
					return
				}
				rec.mu.Lock()
				rec.method = req.Method
				rec.target = req.URL.Host
				rec.host = req.Host
				rec.auth = req.Header.Get("Proxy-Authorization")
				rec.mu.Unlock()

				var resp bytes.Buffer
				fmt.Fprintf(&resp, "HTTP/1.1 %d %s\r\nContent-Length: 0\r\n\r\n", status, http.StatusText(status))
				resp.Write(extra)
				if _, err := conn.Write(resp.Bytes()); err != nil {
					return
				}
				if status != http.StatusOK {
					return
				}
				upstreamConn, err := net.Dial("tcp", target)
				if err != nil {
					return
				}
				defer upstreamConn.Close()
				go func() { _, _ = io.Copy(upstreamConn, br) }()
				_, _ = io.Copy(conn, upstreamConn)
			}()
		}
	}()
	return rec, ln.Addr().String()
}

// hangProxy accepts connections and never replies, to test that the
// handshake honors context cancellation and does not hang forever.
func hangProxy(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(io.Discard, conn)
			}()
		}
	}()
	return ln.Addr().String()
}

// captureLog returns a logf plus a function that renders everything logged.
func captureLog() (func(level logging.Level, tag, format string, args ...any), func() string) {
	var mu sync.Mutex
	var b strings.Builder
	logf := func(level logging.Level, tag, format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(&b, "%s %s %s\n", level, tag, fmt.Sprintf(format, args...))
	}
	dump := func() string {
		mu.Lock()
		defer mu.Unlock()
		return b.String()
	}
	return logf, dump
}

// roundTrip writes msg through conn and reads the echo back.
func roundTrip(t *testing.T, conn net.Conn, msg string) {
	t.Helper()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte(msg)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != msg {
		t.Fatalf("echo = %q, want %q", buf, msg)
	}
}

// -- SOCKS5 behavior ---------------------------------------------------------

func TestUpstreamProxySOCKS5RemoteResolutionAndTunnel(t *testing.T) {
	target := startEchoServer(t)
	rec, proxyAddr := startSOCKS5Proxy(t, socks5Config{}, target)
	logf, dump := captureLog()

	p, err := newProxyDialer("socks5://"+proxyAddr, restrictedDialer(proxyAddr), logf)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// edge.example never resolves locally; the base dialer refuses every
	// direct dial except the proxy, so success proves remote resolution.
	conn, err := p.DialContext(ctx, "tcp", "edge.example:2499")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if rec.atyp != 0x03 {
		t.Fatalf("ATYP = %d, want 3 (domain)", rec.atyp)
	}
	if rec.host != "edge.example" || rec.port != 2499 {
		t.Fatalf("CONNECT target = %q:%d, want edge.example:2499", rec.host, rec.port)
	}
	if !bytes.Contains(rec.methods, []byte{0x00}) {
		t.Fatalf("offered methods = %v, want no-auth (0)", rec.methods)
	}
	roundTrip(t, conn, "hello-through-socks5")
	_ = dump() // proxy activity is logged; the next tests assert no creds
}

func TestUpstreamProxySOCKS5WithCredentials(t *testing.T) {
	target := startEchoServer(t)
	rec, proxyAddr := startSOCKS5Proxy(t, socks5Config{user: "us@er", pass: "p ss:1"}, target)
	logf, dump := captureLog()

	// URL-escaped credentials must be decoded before the RFC 1929 exchange.
	raw := "socks5://us%40er:p%20ss%3A1@" + proxyAddr
	p, err := newProxyDialer(raw, restrictedDialer(proxyAddr), logf)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := p.DialContext(ctx, "tcp", "edge.example:2499")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if rec.selected != 0x02 {
		t.Fatalf("selected method = %d, want 2 (user/pass)", rec.selected)
	}
	if rec.user != "us@er" || rec.pass != "p ss:1" {
		t.Fatalf("sub-negotiation credentials = %q/%q", rec.user, rec.pass)
	}
	roundTrip(t, conn, "authed")

	if logs := dump(); strings.Contains(logs, "us@er") || strings.Contains(logs, "p ss:1") {
		t.Fatalf("credentials leaked into logs:\n%s", logs)
	}
}

func TestUpstreamProxySOCKS5AuthRejected(t *testing.T) {
	target := startEchoServer(t)
	_, proxyAddr := startSOCKS5Proxy(t, socks5Config{user: "correct", pass: "horse"}, target)
	logf, dump := captureLog()

	p, err := newProxyDialer("socks5://wrong:stapler@"+proxyAddr, restrictedDialer(proxyAddr), logf)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := p.DialContext(ctx, "tcp", "edge.example:2499")
	if err == nil {
		_ = conn.Close()
		t.Fatal("dial succeeded against a proxy that rejected the credentials")
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("error = %v, want it to say the credentials were rejected", err)
	}
	if strings.Contains(err.Error(), "stapler") {
		t.Fatalf("error leaks the password: %v", err)
	}
	if logs := dump(); strings.Contains(logs, "stapler") {
		t.Fatalf("password leaked into logs:\n%s", logs)
	}
}

func TestUpstreamProxySOCKS5NoAcceptableAuth(t *testing.T) {
	target := startEchoServer(t)
	_, proxyAddr := startSOCKS5Proxy(t, socks5Config{user: "correct", pass: "horse"}, target)

	// The client has no credentials to offer for a proxy that demands them.
	p, err := newProxyDialer("socks5://"+proxyAddr, restrictedDialer(proxyAddr), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := p.DialContext(ctx, "tcp", "edge.example:2499")
	if err == nil {
		_ = conn.Close()
		t.Fatal("dial succeeded without offering the demanded auth method")
	}
	if !strings.Contains(err.Error(), "acceptable") {
		t.Fatalf("error = %v, want a clear no-acceptable-method message", err)
	}
}

func TestUpstreamProxySOCKS5PinnedIPAtypAndSNI(t *testing.T) {
	target := startEchoServer(t)
	rec, proxyAddr := startSOCKS5Proxy(t, socks5Config{}, target)
	p, err := newProxyDialer("socks5://"+proxyAddr, restrictedDialer(proxyAddr), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// IPv4 literal: ATYP 1, the proxy receives the IP, not the hostname.
	conn, err := p.DialContext(ctx, "tcp", "192.0.2.10:2499")
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if rec.atyp != 0x01 || rec.host != "192.0.2.10" || rec.port != 2499 {
		t.Fatalf("IPv4 CONNECT = atyp %d %q:%d, want 1 192.0.2.10:2499", rec.atyp, rec.host, rec.port)
	}

	// IPv6 literal: ATYP 4.
	conn, err = p.DialContext(ctx, "tcp", "[2001:db8::7]:2499")
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if rec.atyp != 0x04 || rec.host != "2001:db8::7" {
		t.Fatalf("IPv6 CONNECT = atyp %d %q, want 4 2001:db8::7", rec.atyp, rec.host)
	}

	// Pinned IP plus TLS: the proxy sees the IP while the TLS handshake
	// still presents the edge hostname as SNI.
	var sni string
	tlsTarget := startTLSEchoServer(t, &sni)
	_, tlsProxyAddr := startSOCKS5Proxy(t, socks5Config{}, tlsTarget)
	tp, err := newProxyDialer("socks5h://"+tlsProxyAddr, restrictedDialer(tlsProxyAddr), nil)
	if err != nil {
		t.Fatal(err)
	}
	rawConn, err := tp.DialContext(ctx, "tcp", "192.0.2.10:2499")
	if err != nil {
		t.Fatal(err)
	}
	defer rawConn.Close()
	tlsConn := tls.Client(rawConn, &tls.Config{
		ServerName:         "edge.example.net",
		InsecureSkipVerify: true, // test-only self-signed cert
		MinVersion:         tls.VersionTLS12,
	})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		t.Fatal(err)
	}
	if sni != "edge.example.net" {
		t.Fatalf("SNI = %q, want edge.example.net", sni)
	}
	roundTrip(t, tlsConn, "sni-through-proxy")
}

func TestUpstreamProxySOCKS5FailureReply(t *testing.T) {
	target := startEchoServer(t)
	_, refusingAddr := startSOCKS5Proxy(t, socks5Config{connectReply: 0x05}, target)
	p, err := newProxyDialer("socks5://"+refusingAddr, restrictedDialer(refusingAddr), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := p.DialContext(ctx, "tcp", "edge.example:2499")
	if err == nil {
		_ = conn.Close()
		t.Fatal("dial succeeded despite a CONNECT failure reply")
	}
	if !strings.Contains(err.Error(), "SOCKS5 status 5") {
		t.Fatalf("error = %v, want the SOCKS5 status", err)
	}
}

func TestUpstreamProxyHonorsContextCancellation(t *testing.T) {
	addr := hangProxy(t)
	p, err := newProxyDialer("socks5://"+addr, &net.Dialer{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	conn, err := p.DialContext(ctx, "tcp", "edge.example:2499")
	if err == nil {
		_ = conn.Close()
		t.Fatal("dial succeeded against a proxy that never replies")
	}
	if !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("error = %v, want the context deadline", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("cancellation took %s, want it prompt", elapsed)
	}
}

// closingProxy records when the client hangs up mid-handshake.
func closingProxy(t *testing.T, closed chan struct{}) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				// Greet with a bogus version so the client errors out.
				if _, err := conn.Write([]byte{0x09, 0x00}); err != nil {
					return
				}
				// The client must close the connection on the error path.
				// Drain whatever it wrote before noticing the bad reply
				// (its greeting) and watch for the hang-up; a read timeout
				// means it kept the socket and the test fails.
				buf := make([]byte, 256)
				for {
					_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
					if _, err := conn.Read(buf); err != nil {
						if ne, ok := err.(net.Error); ok && ne.Timeout() {
							return // the client never closed: signal nothing
						}
						close(closed)
						return
					}
				}
			}()
		}
	}()
	return ln.Addr().String()
}

func TestUpstreamProxyClosesProxyConnOnHandshakeError(t *testing.T) {
	closed := make(chan struct{})
	addr := closingProxy(t, closed)
	p, err := newProxyDialer("socks5://"+addr, &net.Dialer{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := p.DialContext(ctx, "tcp", "edge.example:2499")
	if err == nil {
		_ = conn.Close()
		t.Fatal("dial succeeded against a bogus SOCKS5 greeting reply")
	}
	select {
	case <-closed:
	case <-time.After(6 * time.Second):
		t.Fatal("the proxy connection was not closed on the error path")
	}
}

// -- HTTP CONNECT behavior ---------------------------------------------------

func TestUpstreamProxyHTTPConnectAndAuth(t *testing.T) {
	target := startEchoServer(t)
	rec, proxyAddr := startHTTPProxy(t, http.StatusOK, nil, target)
	logf, dump := captureLog()

	p, err := newProxyDialer("http://us%40er:p%40ss@"+proxyAddr, restrictedDialer(proxyAddr), logf)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := p.DialContext(ctx, "tcp", "edge.example:2499")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	method, targetName, host, auth := rec.snapshot()
	if method != http.MethodConnect {
		t.Fatalf("method = %q, want CONNECT", method)
	}
	if targetName != "edge.example:2499" || host != "edge.example:2499" {
		t.Fatalf("target/host = %q/%q, want edge.example:2499", targetName, host)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("us@er:p@ss"))
	if auth != wantAuth {
		t.Fatalf("Proxy-Authorization = %q, want %q", auth, wantAuth)
	}
	roundTrip(t, conn, "hello-through-http")

	if logs := dump(); strings.Contains(logs, "p@ss") || strings.Contains(logs, "us@er") {
		t.Fatalf("credentials leaked into logs:\n%s", logs)
	}
}

func TestUpstreamProxyHTTPProxyAuthRequired(t *testing.T) {
	target := startEchoServer(t)
	_, proxyAddr := startHTTPProxy(t, http.StatusProxyAuthRequired, nil, target)
	logf, dump := captureLog()

	p, err := newProxyDialer("http://user:topsecret@"+proxyAddr, restrictedDialer(proxyAddr), logf)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := p.DialContext(ctx, "tcp", "edge.example:2499")
	if err == nil {
		_ = conn.Close()
		t.Fatal("dial succeeded against a proxy answering 407")
	}
	if !strings.Contains(err.Error(), "407") {
		t.Fatalf("error = %v, want the 407 status in it", err)
	}
	if strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("error leaks the password: %v", err)
	}
	if logs := dump(); strings.Contains(logs, "topsecret") {
		t.Fatalf("password leaked into logs:\n%s", logs)
	}
}

func TestUpstreamProxyHTTPErrorStatus(t *testing.T) {
	target := startEchoServer(t)
	_, proxyAddr := startHTTPProxy(t, http.StatusBadGateway, nil, target)

	p, err := newProxyDialer("http://"+proxyAddr, restrictedDialer(proxyAddr), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := p.DialContext(ctx, "tcp", "edge.example:2499")
	if err == nil {
		_ = conn.Close()
		t.Fatal("dial succeeded against a proxy answering 502")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Fatalf("error = %v, want the 502 status in it", err)
	}
}

func TestUpstreamProxyHTTPBufferedBytesComeFirst(t *testing.T) {
	target := startEchoServer(t)
	// The proxy smuggles payload bytes immediately after the 200 headers in
	// the same write; the returned conn must surface them before socket data.
	_, proxyAddr := startHTTPProxy(t, http.StatusOK, []byte("EXTRA"), target)

	p, err := newProxyDialer("http://"+proxyAddr, restrictedDialer(proxyAddr), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := p.DialContext(ctx, "tcp", "edge.example:2499")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "EXTRA" {
		t.Fatalf("first read = %q, want the proxy-buffered %q", buf, "EXTRA")
	}
	roundTrip(t, conn, "after-extra")
}

// -- URL parsing -------------------------------------------------------------

func TestNewProxyDialerURLHandling(t *testing.T) {
	t.Run("rejects", func(t *testing.T) {
		bad := []string{
			"https://127.0.0.1:1080",         // wrong scheme
			"socks4://127.0.0.1:1080",        // unsupported scheme
			"SOCKS4://user:secret@host:1080", // unsupported scheme with creds
			"socks5://",                      // no host
			"http://user:secret@",            // no host with creds
			"not a url",                      // unparseable
			"%zz",                            // unparseable escape
			"socks5://127.0.0.1:notaport",    // invalid port
		}
		for _, raw := range bad {
			p, err := newProxyDialer(raw, nil, nil)
			if err == nil {
				t.Errorf("%q: expected an error, got %+v", raw, p)
				continue
			}
			var cfgErr *proxyConfigError
			if !errors.As(err, &cfgErr) {
				t.Errorf("%q: error %v is not a *proxyConfigError", raw, err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Errorf("%q: error leaks credentials: %v", raw, err)
			}
		}
	})

	t.Run("defaults and decoding", func(t *testing.T) {
		cases := []struct{ raw, wantProxy string }{
			{"socks5://proxy.example", "proxy.example:1080"},
			{"socks5h://proxy.example", "proxy.example:1080"},
			{"SOCKS5://proxy.example", "proxy.example:1080"}, // case-insensitive scheme
			{"http://proxy.example", "proxy.example:80"},
			{"socks5://proxy.example:1081", "proxy.example:1081"},
			{"http://proxy.example:3128", "proxy.example:3128"},
			{"socks5://[::1]:1080", "[::1]:1080"},
			{"socks5://[::1]", "[::1]:1080"},
		}
		for _, tc := range cases {
			p, err := newProxyDialer(tc.raw, nil, nil)
			if err != nil {
				t.Errorf("%q: %v", tc.raw, err)
				continue
			}
			if p.proxy != tc.wantProxy {
				t.Errorf("%q: proxy = %q, want %q", tc.raw, p.proxy, tc.wantProxy)
			}
		}

		p, err := newProxyDialer("socks5://us%40er:p%40ss@proxy.example", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if p.user != "us@er" || p.pass != "p@ss" {
			t.Fatalf("decoded credentials = %q/%q", p.user, p.pass)
		}
	})
}

// -- upstreamOptions wiring --------------------------------------------------

func TestUpstreamOptionsProxySkipsLocalResolution(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {})
	if err := ctrl.store.SetOverride("upstream_proxy", "socks5://127.0.0.1:1080"); err != nil {
		t.Fatal(err)
	}
	// "localhost" resolves to 127.0.0.1 through any local resolver; a
	// pinned IP would show up in PinnedIP if even one lookup ran. The
	// bootstrap resolvers are absent, so the system resolver is the only
	// fallback — and it must never be reached either.
	opts, err := ctrl.upstreamOptions(context.Background(),
		serverlist.Candidate{Host: "localhost", Port: 2499}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.PinnedIP != "" {
		t.Fatalf("PinnedIP = %q, want empty (remote resolution only)", opts.PinnedIP)
	}
	if opts.DialContext == nil {
		t.Fatal("DialContext is nil; the proxy dialer is not wired")
	}
	if opts.WrapConn == nil {
		t.Fatal("WrapConn must stay set so TLS splitting applies after the proxy hop")
	}
}

func TestUpstreamOptionsProxyKeepsPinnedEdgeAddress(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {})
	if err := ctrl.store.SetOverride("upstream_proxy", "socks5://127.0.0.1:1080"); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.store.SetOverride("edge_address", "203.0.113.9"); err != nil {
		t.Fatal(err)
	}
	opts, err := ctrl.upstreamOptions(context.Background(),
		serverlist.Candidate{Host: "edge.example.net", Port: 2499}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.PinnedIP != "203.0.113.9" {
		t.Fatalf("PinnedIP = %q, want the pinned edge address", opts.PinnedIP)
	}
	if opts.DialContext == nil {
		t.Fatal("DialContext is nil; the proxy dialer is not wired")
	}
	if opts.Host != "edge.example.net" {
		t.Fatalf("Host = %q; TLS SNI must keep the edge hostname", opts.Host)
	}
}

func TestUpstreamOptionsWithoutProxyHasNoDialContext(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {})
	if err := ctrl.store.SetOverride("edge_address", "203.0.113.9"); err != nil {
		t.Fatal(err)
	}
	opts, err := ctrl.upstreamOptions(context.Background(),
		serverlist.Candidate{Host: "edge.example.net", Port: 2499}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.DialContext != nil {
		t.Fatal("DialContext set although no proxy is configured")
	}
	if opts.PinnedIP != "203.0.113.9" {
		t.Fatalf("PinnedIP = %q, want the pinned edge address", opts.PinnedIP)
	}
}

func TestUpstreamOptionsSurfacesProxyConfigError(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {})
	// UpdateState persists without validation (unlike SetOverride), which is
	// exactly how an invalid value can reach the dial path: a hand-edited
	// state.json. The dial must surface a clear error instead of dialing
	// direct or panicking.
	bad := "gopher://127.0.0.1:1080"
	if err := ctrl.store.UpdateState(func(s *settings.State) { s.UpstreamProxy = &bad }); err != nil {
		t.Fatal(err)
	}
	_, err := ctrl.upstreamOptions(context.Background(),
		serverlist.Candidate{Host: "edge.example.net", Port: 2499}, nil)
	if err == nil {
		t.Fatal("upstreamOptions accepted a malformed proxy URL")
	}
	var cfgErr *proxyConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("error = %v, want *proxyConfigError", err)
	}
}

func TestConnectSurfacesProxyConfigErrorWithoutDialing(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctx := context.Background()
	if st := ctrl.LoginEmail("user@example.com"); st.Step != "password" {
		t.Fatalf("login step = %+v", st)
	}
	if st := ctrl.LoginPassword(ctx, "pw"); st.Step != "done" {
		t.Fatalf("login = %+v", st)
	}
	ctrl.guardian.BaseURL = guardianPassServer(t, "pass-1", new(atomic.Int32)).URL
	ctrl.serverListURL = serverListServer(t).URL

	var dials atomic.Int32
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		dials.Add(1)
		return newFakeSession(), nil
	}

	bad := "gopher://127.0.0.1:1080"
	if err := ctrl.store.UpdateState(func(s *settings.State) { s.UpstreamProxy = &bad }); err != nil {
		t.Fatal(err)
	}
	err := ctrl.Connect(ctx, api.ConnectPayload{ProxyOnly: true})
	if err == nil {
		defer ctrl.stopTunnel("test cleanup")
		t.Fatal("connect succeeded with a malformed upstream proxy")
	}
	if !strings.Contains(err.Error(), "upstream_proxy") {
		t.Fatalf("error = %v, want it to name upstream_proxy", err)
	}
	if dials.Load() != 0 {
		t.Fatalf("upstream dialed %d times, want 0", dials.Load())
	}
	if st := ctrl.Snapshot(); st.State != string(StateIdle) {
		t.Fatalf("state after the failed connect = %s, want IDLE", st.State)
	}
}
