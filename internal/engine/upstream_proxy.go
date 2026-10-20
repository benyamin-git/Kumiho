package engine

// The upstream proxy (T4.1, PLAN.md §8 upstream_proxy): the edge dial
// tunnels through a configured SOCKS5/HTTP proxy so the edge hostname is
// resolved remotely (no local DNS lookup, no bootstrap race) and the tunnel
// itself can sit behind an authenticated proxy. Only the edge dial goes
// through this hop — control-plane, Remote Settings, DoH and bootstrap dials
// stay on the marked direct path.

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/benyamin-git/kumiho/internal/logging"
)

// proxyConfigError marks an upstream_proxy misconfiguration. Validation
// normally rejects these values before they persist, so reaching it at dial
// time means a hand-edited state file or a stale config: retrying cannot
// heal it, and callers surface it instead of backing off as if transient.
type proxyConfigError struct{ msg string }

func (e *proxyConfigError) Error() string { return "upstream_proxy: " + e.msg }

// proxyDialer tunnels edge dials through one configured upstream proxy.
// The URL is parsed once in newProxyDialer; DialContext performs the proxy
// handshake per dial. Credentials never appear in logs or error text.
type proxyDialer struct {
	scheme string      // socks5 | socks5h | http
	proxy  string      // host:port of the proxy (scheme default port filled in)
	user   string      // optional username (URL-decoded)
	pass   string      // optional password (URL-decoded)
	base   *net.Dialer // dialer used to reach the proxy (bypass path); nil = zero dialer
	logf   func(level logging.Level, tag, format string, args ...any)
}

// newProxyDialer parses rawURL once. base is the marked bypass dialer so the
// hop to the proxy itself stays outside the tunnel; nil means a zero
// net.Dialer (tests, or before UseBypassDialer runs).
func newProxyDialer(rawURL string, base *net.Dialer, logf func(level logging.Level, tag, format string, args ...any)) (*proxyDialer, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, &proxyConfigError{"not a valid URL (want e.g. socks5://host[:port])"}
	}
	scheme := strings.ToLower(u.Scheme)
	var defPort int
	switch scheme {
	case "socks5", "socks5h":
		defPort = 1080 // Ruling 18
	case "http":
		defPort = 80 // Ruling 18
	default:
		// Only the scheme is safe to echo; the raw URL may carry credentials.
		return nil, &proxyConfigError{fmt.Sprintf("unsupported scheme %q (want socks5|socks5h|http)", u.Scheme)}
	}
	if u.Host == "" {
		return nil, &proxyConfigError{"missing host"}
	}
	proxy := u.Host
	if u.Port() == "" {
		// u.Host is "[v6]" or a bare hostname here; JoinHostPort adds the
		// brackets back for IPv6.
		proxy = net.JoinHostPort(strings.Trim(u.Host, "[]"), strconv.Itoa(defPort))
	} else if n, err := strconv.Atoi(u.Port()); err != nil || n < 1 || n > 65535 {
		return nil, &proxyConfigError{"port out of range"}
	}
	p := &proxyDialer{scheme: scheme, proxy: proxy, base: base, logf: logf}
	if u.User != nil {
		p.user = u.User.Username()
		p.pass, _ = u.User.Password()
	}
	return p, nil
}

// DialContext dials addr through the proxy. Only reaching the proxy uses
// base/network; the target is handed to the proxy unresolved so it resolves
// remotely. The proxy connection is closed on every error path, and a
// cancelled context always unblocks the handshake.
func (p *proxyDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	base := p.base
	if base == nil {
		base = &net.Dialer{}
	}
	conn, err := base.DialContext(ctx, "tcp", p.proxy)
	if err != nil {
		return nil, fmt.Errorf("dial proxy %s: %w", p.proxy, err)
	}
	// Best-effort no-delay so the handshake (and the later fragmented
	// ClientHello) writes are not coalesced by Nagle.
	if tc, ok := conn.(interface{ SetNoDelay(bool) error }); ok {
		_ = tc.SetNoDelay(true)
	}

	// The handshake runs in a goroutine so context cancellation (or a proxy
	// that accepts and then goes silent) cannot hang the dial: cancelling
	// closes the socket, which unblocks every handshake read/write.
	type result struct {
		br  *bufio.Reader
		err error
	}
	done := make(chan result, 1)
	go func() {
		hbr, herr := p.handshake(conn, addr)
		done <- result{hbr, herr}
	}()
	var br *bufio.Reader
	select {
	case <-ctx.Done():
		_ = conn.Close()
		<-done // wait for the goroutine to observe the closed socket
		return nil, fmt.Errorf("proxy handshake with %s: %w", p.proxy, ctx.Err())
	case r := <-done:
		if r.err != nil {
			_ = conn.Close()
			return nil, r.err
		}
		br = r.br
	}
	if p.scheme == "http" {
		// Preserve bytes the proxy buffered after the response headers; the
		// returned conn must read them before touching the socket.
		conn = &bufferedConn{Conn: conn, r: br}
	}
	p.logfSafe(logging.Debug, "dial %s via %s proxy hop established", addr, p.scheme)
	return conn, nil
}

// logfSafe keeps logging nil-safe.
func (p *proxyDialer) logfSafe(level logging.Level, format string, args ...any) {
	if p.logf != nil {
		p.logf(level, "proxy", format, args...)
	}
}

// handshake performs the proxy protocol exchange for addr and returns the
// buffered reader (HTTP only, to preserve buffered bytes).
func (p *proxyDialer) handshake(conn net.Conn, addr string) (*bufio.Reader, error) {
	switch p.scheme {
	case "socks5", "socks5h":
		if err := p.socks5Handshake(conn, addr); err != nil {
			return nil, err
		}
		return nil, nil
	case "http":
		return p.httpHandshake(conn, addr)
	default:
		// Defensive; validation and newProxyDialer reject anything else.
		return nil, &proxyConfigError{fmt.Sprintf("unsupported scheme %q", p.scheme)}
	}
}

// -- SOCKS5 (RFC 1928 + RFC 1929); both socks5 and socks5h resolve remotely --

func (p *proxyDialer) socks5Handshake(conn net.Conn, addr string) error {
	methods := []byte{0x00} // no-auth always offered
	if p.user != "" || p.pass != "" {
		methods = append(methods, 0x02) // username/password when credentials exist
	}
	greet := append([]byte{0x05, byte(len(methods))}, methods...)
	if _, err := conn.Write(greet); err != nil {
		return fmt.Errorf("SOCKS5 greeting: %w", err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return fmt.Errorf("SOCKS5 greeting reply: %w", err)
	}
	if reply[0] != 0x05 {
		return fmt.Errorf("SOCKS5 greeting reply version %d, want 5", reply[0])
	}
	switch reply[1] {
	case 0x00: // no authentication required
	case 0x02: // username/password sub-negotiation (RFC 1929)
		if err := p.socks5UserPass(conn); err != nil {
			return err
		}
	case 0xFF:
		return errors.New("the proxy offers no acceptable authentication method")
	default:
		return fmt.Errorf("the proxy selected unexpected auth method %d", reply[1])
	}
	return p.socks5Connect(conn, addr)
}

func (p *proxyDialer) socks5UserPass(conn net.Conn) error {
	if len(p.user) > 255 || len(p.pass) > 255 {
		return errors.New("username or password longer than 255 bytes")
	}
	req := []byte{0x01, byte(len(p.user))}
	req = append(req, p.user...)
	req = append(req, byte(len(p.pass)))
	req = append(req, p.pass...)
	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("SOCKS5 auth write: %w", err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return fmt.Errorf("SOCKS5 auth reply: %w", err)
	}
	if reply[1] != 0x00 {
		// Credentials are never echoed.
		return errors.New("the proxy rejected the configured username/password")
	}
	return nil
}

func (p *proxyDialer) socks5Connect(conn net.Conn, addr string) error {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("bad CONNECT target %q", addr)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("bad CONNECT target port in %q", addr)
	}
	req := []byte{0x05, 0x01, 0x00} // CONNECT
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			req = append(req, 0x01) // IPv4
			req = append(req, v4...)
		} else {
			req = append(req, 0x04) // IPv6
			req = append(req, ip.To16()...)
		}
	} else {
		if len(host) == 0 || len(host) > 255 {
			return fmt.Errorf("bad CONNECT target host in %q", addr)
		}
		req = append(req, 0x03, byte(len(host))) // domain: the proxy resolves it
		req = append(req, host...)
	}
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("SOCKS5 CONNECT write: %w", err)
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return fmt.Errorf("SOCKS5 CONNECT reply: %w", err)
	}
	if head[0] != 0x05 {
		return fmt.Errorf("SOCKS5 CONNECT reply version %d, want 5", head[0])
	}
	if head[1] != 0x00 {
		return fmt.Errorf("the proxy could not reach %s (SOCKS5 status %d)", addr, head[1])
	}
	var addrLen int
	switch head[3] {
	case 0x01:
		addrLen = 4
	case 0x04:
		addrLen = 16
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			return fmt.Errorf("SOCKS5 CONNECT reply: %w", err)
		}
		addrLen = int(l[0])
	default:
		return fmt.Errorf("SOCKS5 CONNECT reply has unknown address type %d", head[3])
	}
	if _, err := io.ReadFull(conn, make([]byte, addrLen+2)); err != nil {
		return fmt.Errorf("SOCKS5 CONNECT reply: %w", err)
	}
	return nil
}

// -- HTTP CONNECT ------------------------------------------------------------

func (p *proxyDialer) httpHandshake(conn net.Conn, addr string) (*bufio.Reader, error) {
	var req strings.Builder
	fmt.Fprintf(&req, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n", addr, addr)
	if p.user != "" || p.pass != "" {
		cred := base64.StdEncoding.EncodeToString([]byte(p.user + ":" + p.pass))
		fmt.Fprintf(&req, "Proxy-Authorization: Basic %s\r\n", cred)
	}
	req.WriteString("Proxy-Connection: keep-alive\r\n\r\n")
	// The request body is sent as one write so the proxy sees headers and
	// (future) body contiguously; no credentials are ever logged.
	if _, err := io.WriteString(conn, req.String()); err != nil {
		return nil, fmt.Errorf("HTTP CONNECT write: %w", err)
	}
	br := bufio.NewReader(conn)
	connectReq, err := http.NewRequest(http.MethodConnect, "https://"+addr, nil)
	if err != nil {
		return nil, &proxyConfigError{fmt.Sprintf("bad CONNECT target %q", addr)}
	}
	resp, err := http.ReadResponse(br, connectReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP CONNECT reply: %w", err)
	}
	// resp.Body is a lazy reader over br that nothing ever reads; closing it
	// could try to drain an unbounded body, and the conn below is ours to
	// keep or close outright.
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusProxyAuthRequired {
			return nil, errors.New("the proxy requires authentication (HTTP 407)")
		}
		return nil, fmt.Errorf("the proxy rejected CONNECT (%s)", resp.Status)
	}
	return br, nil
}

// bufferedConn reads bytes the proxy already buffered past the HTTP CONNECT
// response before touching the socket.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	if c.r.Buffered() > 0 {
		return c.r.Read(p)
	}
	return c.Conn.Read(p)
}
