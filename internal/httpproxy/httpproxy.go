// Package httpproxy implements the local HTTP proxy (PLAN.md §2.4, D11):
// CONNECT tunnelling and absolute-form plain HTTP through the same tunnel as
// SOCKS, with the same local-destination policy, connection limit, handshake
// timeout and half-close drain.
package httpproxy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/socks"
)

// Defaults mirror the SOCKS server (D11).
const (
	DefaultMaxConns         = socks.DefaultMaxConns
	DefaultHandshakeTimeout = socks.DefaultHandshakeTimeout
	DefaultHalfCloseDrain   = socks.DefaultHalfCloseDrain
	DefaultOpenTimeout      = socks.DefaultOpenTimeout

	relayBufferSize = 32 * 1024
	maxRequestLine  = 8 * 1024
	tag             = "httpproxy"
)

var errRequestLineTooLong = errors.New("httpproxy: request line too long")

// Server accepts CONNECT and absolute-form HTTP clients on the loopback
// interface.
type Server struct {
	Bind string
	Port int

	// Dial opens the tunnelled stream for a target (usually
	// upstream.Session.OpenStream). Required.
	Dial func(ctx context.Context, host string, port int) (net.Conn, error)

	// Logf receives operational logs (may be nil).
	Logf func(level logging.Level, tag, format string, args ...any)

	MaxConns         int
	HandshakeTimeout time.Duration
	HalfCloseDrain   time.Duration
	OpenTimeout      time.Duration

	ln      net.Listener
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	conns   map[net.Conn]struct{}
	tunnels map[net.Conn]struct{}
	closed  bool
	slots   chan struct{}
}

// Start listens and serves until Stop.
func (s *Server) Start() error {
	s.normalize()
	if s.Dial == nil {
		return errors.New("httpproxy: Dial is required")
	}
	ctx, cancel := context.WithCancel(context.Background())
	addr := net.JoinHostPort(s.Bind, strconv.Itoa(s.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		cancel()
		return fmt.Errorf("httpproxy: listen %s: %w", addr, err)
	}
	s.mu.Lock()
	s.ctx = ctx
	s.cancel = cancel
	s.ln = ln
	s.conns = make(map[net.Conn]struct{})
	s.tunnels = make(map[net.Conn]struct{})
	s.mu.Unlock()
	s.logf(logging.Info, "listening on %s", ln.Addr())
	go s.acceptLoop()
	return nil
}

// Addr returns the bound address (after Start).
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return nil
	}
	return s.ln.Addr()
}

// Stop closes the listener, every active client connection and every tunnel.
// It is idempotent and does not wait for a handler blocked in Dial.
func (s *Server) Stop() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	ln := s.ln
	cancel := s.cancel
	conns := make([]net.Conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	tunnels := make([]net.Conn, 0, len(s.tunnels))
	for c := range s.tunnels {
		tunnels = append(tunnels, c)
	}
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if ln != nil {
		_ = ln.Close()
	}
	for _, c := range tunnels {
		_ = c.Close()
	}
	for _, c := range conns {
		_ = c.Close()
	}
}

func (s *Server) normalize() {
	if s.Bind == "" {
		s.Bind = "127.0.0.1"
	}
	if s.MaxConns <= 0 {
		s.MaxConns = DefaultMaxConns
	}
	if s.HandshakeTimeout <= 0 {
		s.HandshakeTimeout = DefaultHandshakeTimeout
	}
	if s.HalfCloseDrain <= 0 {
		s.HalfCloseDrain = DefaultHalfCloseDrain
	}
	if s.OpenTimeout <= 0 {
		s.OpenTimeout = DefaultOpenTimeout
	}
	if s.slots == nil {
		s.slots = make(chan struct{}, s.MaxConns)
	}
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		select {
		case s.slots <- struct{}{}:
		default:
			s.logf(logging.Warn, "rejecting client: connection limit (%d) reached", s.MaxConns)
			_ = conn.Close()
			continue
		}
		if !s.admitConn(conn) {
			<-s.slots
			_ = conn.Close()
			continue
		}
		go func() {
			defer func() { <-s.slots }()
			s.handleConn(conn)
		}()
	}
}

// admitConn atomically rejects new clients once Stop has run and otherwise
// tracks the connection. The accept loop closes and releases the slot when
// admission fails, so a handler is never started for a rejected connection.
func (s *Server) admitConn(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	if s.conns == nil {
		s.conns = make(map[net.Conn]struct{})
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *Server) untrack(c net.Conn) {
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
}

// admitTunnel tracks an upstream stream, refusing it once Stop has run.
func (s *Server) admitTunnel(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	if s.tunnels == nil {
		s.tunnels = make(map[net.Conn]struct{})
	}
	s.tunnels[c] = struct{}{}
	return true
}

func (s *Server) untrackTunnel(c net.Conn) {
	s.mu.Lock()
	delete(s.tunnels, c)
	s.mu.Unlock()
}

// dialContext derives a per-dial timeout from the server context so Stop
// unblocks handlers waiting in Dial.
func (s *Server) dialContext() (context.Context, context.CancelFunc) {
	s.mu.Lock()
	base := s.ctx
	s.mu.Unlock()
	if base == nil {
		base = context.Background()
	}
	return context.WithTimeout(base, s.OpenTimeout)
}

// redactRequestURI removes authority userinfo before a request target is
// logged. It handles absolute-form URLs and CONNECT authority-form targets.
func redactRequestURI(raw string) string {
	start := 0
	if i := strings.Index(raw, "://"); i >= 0 {
		start = i + 3
	}
	end := len(raw)
	if i := strings.IndexAny(raw[start:], "/?#"); i >= 0 {
		end = start + i
	}
	if at := strings.LastIndex(raw[start:end], "@"); at >= 0 {
		return raw[:start] + "[redacted]@" + raw[start+at+1:]
	}
	return raw
}

func (s *Server) logf(level logging.Level, format string, args ...any) {
	if s.Logf != nil {
		s.Logf(level, tag, format, args...)
	}
}

// handleConn reads one request and serves it. The client connection closes
// after the response (or after the CONNECT relay ends).
func (s *Server) handleConn(client net.Conn) {
	s.normalize()
	defer s.untrack(client)
	defer client.Close()

	_ = client.SetReadDeadline(time.Now().Add(s.HandshakeTimeout))
	br := bufio.NewReader(client)
	req, reader, err := readRequest(br)
	if err != nil {
		s.logf(logging.Debug, "request parse failure from %v: %v", client.RemoteAddr(), err)
		_ = writeStatus(client, http.StatusBadRequest)
		return
	}
	_ = client.SetReadDeadline(time.Time{})

	if req.Method == http.MethodConnect {
		s.handleConnect(client, reader, req)
		return
	}
	s.handleHTTP(client, req)
}

// readRequest parses one request, normalizing a CONNECT request whose target
// is an unbracketed IPv6 literal. http.ReadRequest rejects that authority
// form (it prepends a scheme and then fails on the colons), but the proxy
// accepts it. The returned reader keeps any payload buffered past the request
// headers so CONNECT relays do not lose it.
func readRequest(br *bufio.Reader) (*http.Request, *bufio.Reader, error) {
	line, err := readLineLimited(br, maxRequestLine)
	if err != nil {
		return nil, br, err
	}
	line = rewriteUnbracketedConnect(line)
	br = bufio.NewReader(io.MultiReader(bytes.NewReader(line), br))
	req, err := http.ReadRequest(br)
	return req, br, err
}

// readLineLimited reads one full request line, draining bufio buffer-full
// fragments, and rejects lines beyond max.
func readLineLimited(br *bufio.Reader, max int) ([]byte, error) {
	var line []byte
	for {
		fragment, err := br.ReadSlice('\n')
		if len(line)+len(fragment) > max {
			return nil, errRequestLineTooLong
		}
		line = append(line, fragment...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return line, nil
	}
}

// rewriteUnbracketedConnect brackets an unbracketed IPv6 authority in a
// CONNECT request line. Any target with multiple colons that parses as an IP
// is an IPv6 literal, including IPv4-mapped addresses.
func rewriteUnbracketedConnect(line []byte) []byte {
	trimmed := bytes.TrimSuffix(line, []byte("\n"))
	trimmed = bytes.TrimSuffix(trimmed, []byte("\r"))
	fields := bytes.Split(trimmed, []byte(" "))
	if len(fields) != 3 || string(fields[0]) != http.MethodConnect {
		return line
	}
	target := string(fields[1])
	if strings.HasPrefix(target, "[") || strings.Count(target, ":") < 2 {
		return line
	}
	if net.ParseIP(target) == nil {
		return line
	}
	return []byte("CONNECT [" + target + "] " + string(fields[2]) + "\r\n")
}

func (s *Server) handleConnect(client net.Conn, clientReader io.Reader, req *http.Request) {
	if req.URL == nil {
		_ = writeStatus(client, http.StatusBadRequest)
		return
	}
	if req.URL.Scheme != "" || req.URL.Path != "" || req.URL.RawQuery != "" || req.URL.Fragment != "" {
		s.logf(logging.Debug, "refusing CONNECT %q: not an authority-form target", redactRequestURI(req.RequestURI))
		_ = writeStatus(client, http.StatusMethodNotAllowed)
		return
	}
	target, err := parseAuthority(req.URL.Host, 443)
	if err != nil {
		s.logf(logging.Debug, "refusing CONNECT %q: %v", redactRequestURI(req.RequestURI), err)
		_ = writeStatus(client, http.StatusBadRequest)
		return
	}
	key := net.JoinHostPort(target.host, strconv.Itoa(target.port))
	if isLocalIP(target.host) {
		s.logf(logging.Debug, "refusing %s: local-network address", key)
		_ = writeStatus(client, http.StatusForbidden)
		return
	}

	ctx, cancel := s.dialContext()
	defer cancel()
	tunnel, err := s.Dial(ctx, target.host, target.port)
	if err != nil {
		s.logf(logging.Debug, "open upstream stream to %s failed: %v", key, err)
		_ = writeStatus(client, http.StatusBadGateway)
		return
	}
	if !s.admitTunnel(tunnel) {
		_ = tunnel.Close()
		return
	}
	defer s.untrackTunnel(tunnel)

	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		_ = tunnel.Close()
		return
	}
	s.relay(client, clientReader, tunnel)
}

func (s *Server) handleHTTP(client net.Conn, req *http.Request) {
	if req.URL == nil || req.URL.Scheme != "http" || req.URL.Host == "" {
		s.logf(logging.Debug, "refusing %s %q: not an absolute-form http request", req.Method, redactRequestURI(req.RequestURI))
		_ = writeStatus(client, http.StatusBadRequest)
		return
	}
	target, err := parseAuthority(req.URL.Host, 80)
	if err != nil {
		s.logf(logging.Debug, "refusing %s %q: %v", req.Method, redactRequestURI(req.RequestURI), err)
		_ = writeStatus(client, http.StatusBadRequest)
		return
	}
	key := net.JoinHostPort(target.host, strconv.Itoa(target.port))
	if isLocalIP(target.host) {
		s.logf(logging.Debug, "refusing %s: local-network address", key)
		_ = writeStatus(client, http.StatusForbidden)
		return
	}

	ctx, cancel := s.dialContext()
	defer cancel()
	tunnel, err := s.Dial(ctx, target.host, target.port)
	if err != nil {
		s.logf(logging.Debug, "open upstream stream to %s failed: %v", key, err)
		_ = writeStatus(client, http.StatusBadGateway)
		return
	}
	if !s.admitTunnel(tunnel) {
		_ = tunnel.Close()
		return
	}
	defer s.untrackTunnel(tunnel)
	defer tunnel.Close()

	// The client is waiting for the interim response before sending the body;
	// answer it here and hide the expectation from the origin.
	if hasExpectContinue(req.Header) {
		if _, err := io.WriteString(client, "HTTP/1.1 100 Continue\r\n\r\n"); err != nil {
			return
		}
		req.Header.Del("Expect")
	}

	stripHopByHop(req)
	req.Close = true
	req.RequestURI = ""
	req.URL.Scheme = ""
	req.URL.Host = ""

	if err := req.Write(tunnel); err != nil {
		s.logf(logging.Debug, "forward %s %s failed: %v", req.Method, key, err)
		return
	}
	_, _ = io.CopyBuffer(client, tunnel, make([]byte, relayBufferSize))
}

// relay copies both directions, half-closing the tunnel writer when the
// client finishes, and draining for up to HalfCloseDrain. clientReader is the
// buffered request reader so payload sent with the CONNECT headers survives.
func (s *Server) relay(client net.Conn, clientReader io.Reader, tunnel net.Conn) {
	toTunnel := make(chan struct{})
	go func() {
		defer close(toTunnel)
		_, _ = io.CopyBuffer(tunnel, clientReader, make([]byte, relayBufferSize))
		if cw, ok := tunnel.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
	}()

	_, _ = io.CopyBuffer(client, tunnel, make([]byte, relayBufferSize))
	if cw, ok := client.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}

	select {
	case <-toTunnel:
	case <-time.After(s.HalfCloseDrain):
		s.logf(logging.Debug, "closing half-closed tunnel after drain timeout")
		_ = client.Close()
		_ = tunnel.Close()
		<-toTunnel
	}
	_ = tunnel.Close()
}

type target struct {
	host string
	port int
}

// parseAuthority splits an authority-form target, defaulting the port when it
// is absent. Bracketed IPv6 literals and unbracketed IPv6 literals without a
// port are accepted; a port must be 1-65535.
func parseAuthority(authority string, defaultPort int) (target, error) {
	var t target
	if authority == "" {
		return t, errors.New("empty authority")
	}
	host := authority
	port := defaultPort
	hasPort := false
	portText := ""

	switch {
	case strings.HasPrefix(authority, "["):
		end := strings.IndexByte(authority, ']')
		if end < 0 {
			return t, fmt.Errorf("malformed authority %q: missing ']'", authority)
		}
		host = authority[1:end]
		rest := authority[end+1:]
		if rest != "" {
			if !strings.HasPrefix(rest, ":") {
				return t, fmt.Errorf("malformed authority %q", authority)
			}
			hasPort = true
			portText = rest[1:]
		}
		if net.ParseIP(host) == nil {
			return t, fmt.Errorf("malformed authority %q: invalid bracketed host", authority)
		}
	case strings.Count(authority, ":") > 1:
		// An unbracketed IPv6 literal cannot carry a port; accept the literal.
		if net.ParseIP(authority) == nil {
			return t, fmt.Errorf("malformed authority %q", authority)
		}
		host = authority
	case strings.Contains(authority, ":"):
		i := strings.LastIndexByte(authority, ':')
		host = authority[:i]
		hasPort = true
		portText = authority[i+1:]
	}

	if host == "" {
		return t, fmt.Errorf("malformed authority %q: empty host", authority)
	}
	if hasPort {
		n, err := strconv.Atoi(portText)
		if err != nil || n < 1 || n > 65535 {
			return t, fmt.Errorf("malformed authority %q: invalid port", authority)
		}
		port = n
	}
	return target{host: host, port: port}, nil
}

func isLocalIP(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && socks.IsLocalDestination(ip)
}

func hasExpectContinue(h http.Header) bool {
	for _, value := range h.Values("Expect") {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "100-continue") {
				return true
			}
		}
	}
	return false
}

// stripHopByHop removes proxy credentials, proxy-only headers, the standard
// hop-by-hop headers and every header named by the request's Connection
// tokens.
func stripHopByHop(req *http.Request) {
	for _, value := range req.Header.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			if token = strings.TrimSpace(token); token != "" {
				req.Header.Del(token)
			}
		}
	}
	for _, name := range []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Proxy-Connection",
		"TE",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	} {
		req.Header.Del(name)
	}
	req.Trailer = nil
}

// writeStatus writes the minimal response used for refusals and parse errors.
func writeStatus(w io.Writer, code int) error {
	_, err := fmt.Fprintf(w, "HTTP/1.1 %d %s\r\nContent-Length: 0\r\nConnection: close\r\n\r\n",
		code, http.StatusText(code))
	return err
}
