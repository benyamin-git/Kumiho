// Package socks implements the local SOCKS5 server (PLAN.md §2.4): no-auth
// greeting, CONNECT tunnelled through the HTTP/2 session, local-destination
// refusals, refusal backoff for destinations the edge declines, and
// half-close draining. DNS-only UDP ASSOCIATE lands with the M4 resolver.
package socks

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/upstream"
)

// Defaults mirror PLAN.md §2.4.
const (
	DefaultMaxConns         = 512
	DefaultHandshakeTimeout = 10 * time.Second
	DefaultHalfCloseDrain   = 2 * time.Minute
	DefaultOpenTimeout      = 30 * time.Second

	relayBufferSize = 32 * 1024

	refusalTTLBase        = 30 * time.Second
	refusalTTLCap         = 10 * time.Minute
	refusalCacheCap       = 256
	refusalsBeforePolicy  = 3
	acceptedReplyAddrV4   = 4
	socksVersion          = 5
	socksCmdConnect       = 1
	socksCmdUDPAssociate  = 3
	socksAtypIPv4         = 1
	socksAtypDomain       = 3
	socksAtypIPv6         = 4
	replySucceeded        = 0x00
	replyGeneralFailure   = 0x01
	replyNotAllowed       = 0x02
	replyHostUnreachable  = 0x04
	replyConnectionRefuse = 0x05
	replyTTLExpired       = 0x06
	replyCmdUnsupported   = 0x07
	replyAddrUnsupported  = 0x08
)

// Server accepts SOCKS5 clients on the loopback interface.
type Server struct {
	Bind string
	Port int

	// Dial opens the tunnelled stream for a CONNECT (usually
	// upstream.Session.OpenStream). Required.
	Dial func(ctx context.Context, host string, port int) (net.Conn, error)

	// Logf receives operational logs (may be nil).
	Logf func(level logging.Level, tag, format string, args ...any)

	MaxConns         int
	HandshakeTimeout time.Duration
	HalfCloseDrain   time.Duration
	OpenTimeout      time.Duration

	// OnAuthRejected, when set, is invoked when the edge refuses a CONNECT
	// with 401/403/407 so the daemon can recycle the session (PLAN.md §2.4).
	OnAuthRejected func()

	ln       net.Listener
	mu       sync.Mutex
	conns    map[net.Conn]struct{}
	closed   bool
	refusals map[string]*refusal
	slots    chan struct{}
}

type refusal struct {
	strikes int
	expires time.Time
}

// Start listens and serves until Stop.
func (s *Server) Start() error {
	s.normalize()
	if s.Dial == nil {
		return errors.New("socks: Dial is required")
	}
	addr := net.JoinHostPort(s.Bind, strconv.Itoa(s.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("socks: listen %s: %w", addr, err)
	}
	s.mu.Lock()
	s.ln = ln
	s.conns = make(map[net.Conn]struct{})
	s.refusals = make(map[string]*refusal)
	s.mu.Unlock()
	s.logf(logging.Info, "socks", "listening on %s", ln.Addr())
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

// Stop closes the listener and every active client connection.
func (s *Server) Stop() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	ln := s.ln
	conns := make([]net.Conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()

	if ln != nil {
		_ = ln.Close()
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
			s.logf(logging.Warn, "socks", "rejecting client: connection limit (%d) reached", s.MaxConns)
			_ = conn.Close()
			continue
		}
		s.track(conn)
		go func() {
			defer func() { <-s.slots }()
			s.handleConn(conn)
		}()
	}
}

func (s *Server) track(c net.Conn) {
	s.mu.Lock()
	if s.conns == nil {
		s.conns = make(map[net.Conn]struct{})
	}
	s.conns[c] = struct{}{}
	s.mu.Unlock()
}

func (s *Server) untrack(c net.Conn) {
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
}

func (s *Server) logf(level logging.Level, tag, format string, args ...any) {
	if s.Logf != nil {
		s.Logf(level, tag, format, args...)
	}
}

// handleConn speaks SOCKS5 to one client.
func (s *Server) handleConn(client net.Conn) {
	s.normalize()
	defer s.untrack(client)
	defer client.Close()

	_ = client.SetDeadline(time.Now().Add(s.HandshakeTimeout))

	var greeting [2]byte
	if _, err := io.ReadFull(client, greeting[:]); err != nil {
		return
	}
	if greeting[0] != socksVersion {
		s.logf(logging.Debug, "socks", "rejecting client: SOCKS version %d", greeting[0])
		return
	}
	methods := make([]byte, int(greeting[1]))
	if _, err := io.ReadFull(client, methods); err != nil {
		return
	}
	if !bytes.Contains(methods, []byte{0x00}) {
		_, _ = client.Write([]byte{socksVersion, 0xFF})
		return
	}
	if _, err := client.Write([]byte{socksVersion, 0x00}); err != nil {
		return
	}

	var req [4]byte
	if _, err := io.ReadFull(client, req[:]); err != nil {
		return
	}
	if req[0] != socksVersion {
		_ = writeReply(client, replyCmdUnsupported, nil)
		return
	}
	switch req[1] {
	case socksCmdConnect:
		s.handleConnect(client, req[3])
	case socksCmdUDPAssociate:
		s.handleUDPAssociate(client, req[3])
	default:
		_ = writeReply(client, replyCmdUnsupported, nil)
	}
}

type targetAddr struct {
	host   string
	port   int
	ip     net.IP
	domain bool
}

func readTarget(r io.Reader, atyp byte) (targetAddr, error) {
	var t targetAddr
	switch atyp {
	case socksAtypIPv4:
		raw := make([]byte, 4)
		if _, err := io.ReadFull(r, raw); err != nil {
			return t, err
		}
		t.ip = net.IP(raw)
		t.host = t.ip.String()
	case socksAtypIPv6:
		raw := make([]byte, 16)
		if _, err := io.ReadFull(r, raw); err != nil {
			return t, err
		}
		t.ip = net.IP(raw)
		t.host = t.ip.String()
	case socksAtypDomain:
		var length [1]byte
		if _, err := io.ReadFull(r, length[:]); err != nil {
			return t, err
		}
		if length[0] == 0 {
			return t, errors.New("empty SOCKS5 domain name")
		}
		raw := make([]byte, int(length[0]))
		if _, err := io.ReadFull(r, raw); err != nil {
			return t, err
		}
		t.host = string(raw)
		t.domain = true
	default:
		return t, fmt.Errorf("unsupported SOCKS5 address type %d", atyp)
	}
	var portRaw [2]byte
	if _, err := io.ReadFull(r, portRaw[:]); err != nil {
		return t, err
	}
	t.port = int(binary.BigEndian.Uint16(portRaw[:]))
	return t, nil
}

func (s *Server) handleConnect(client net.Conn, atyp byte) {
	target, err := readTarget(client, atyp)
	if err != nil {
		_ = writeReply(client, replyAddrUnsupported, nil)
		return
	}

	if !target.domain && isLocalDestination(target.ip) {
		s.logf(logging.Debug, "socks", "refusing %s: local-network address", targetKey(target))
		_ = writeReply(client, replyNotAllowed, nil)
		return
	}

	key := targetKey(target)
	if s.knownUnreachable(key) {
		s.logf(logging.Debug, "socks", "refusing %s: the edge declined this destination moments ago", key)
		_ = writeReply(client, replyHostUnreachable, nil)
		return
	}

	_ = client.SetDeadline(time.Time{})
	ctx, cancel := context.WithTimeout(context.Background(), s.OpenTimeout)
	defer cancel()

	tunnel, err := s.Dial(ctx, target.host, target.port)
	if err != nil {
		if upstream.IsTargetUnreachable(err) {
			s.rememberUnreachable(key, target.port)
		}
		if upstream.IsAuthRejected(err) && s.OnAuthRejected != nil {
			s.OnAuthRejected()
		}
		s.logf(logging.Debug, "socks", "open upstream stream to %s failed: %v", key, err)
		_ = writeReply(client, replyCodeFor(err), nil)
		return
	}
	s.forgetUnreachable(key)

	if err := writeReply(client, replySucceeded, client.LocalAddr()); err != nil {
		_ = tunnel.Close()
		return
	}
	s.relay(client, tunnel)
}

// relay copies both directions, half-closing the tunnel writer when the
// client finishes, and draining for up to HalfCloseDrain.
func (s *Server) relay(client net.Conn, tunnel net.Conn) {
	toTunnel := make(chan struct{})
	go func() {
		defer close(toTunnel)
		_, _ = io.CopyBuffer(tunnel, client, make([]byte, relayBufferSize))
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
		s.logf(logging.Debug, "socks", "closing half-closed tunnel after drain timeout")
		_ = client.Close()
		_ = tunnel.Close()
		<-toTunnel
	}
	_ = tunnel.Close()
}

func (s *Server) handleUDPAssociate(client net.Conn, atyp byte) {
	target, err := readTarget(client, atyp)
	if err != nil {
		_ = writeReply(client, replyAddrUnsupported, nil)
		return
	}
	if target.port != 53 && target.port != 0 {
		_ = writeReply(client, replyNotAllowed, nil)
		return
	}
	// The DNS relay needs the in-daemon resolver (PLAN.md §3.5, M4).
	s.logf(logging.Warn, "socks", "UDP ASSOCIATE for DNS arrives with the M4 resolver; refusing")
	_ = writeReply(client, replyCmdUnsupported, nil)
}

func targetKey(t targetAddr) string {
	return net.JoinHostPort(t.host, strconv.Itoa(t.port))
}

func writeReply(w io.Writer, code byte, addr net.Addr) error {
	b := make([]byte, 0, 22)
	b = append(b, socksVersion, code, 0x00)
	ip := net.IPv4zero
	port := 0
	if tcp, ok := addr.(*net.TCPAddr); ok {
		ip = tcp.IP
		port = tcp.Port
	}
	if v4 := ip.To4(); v4 != nil {
		b = append(b, socksAtypIPv4)
		b = append(b, v4...)
	} else {
		b = append(b, socksAtypIPv6)
		b = append(b, ip.To16()...)
	}
	b = append(b, byte(port>>8), byte(port))
	_, err := w.Write(b)
	return err
}

func replyCodeFor(err error) byte {
	var timeout *upstream.ConnectTimeout
	if errors.As(err, &timeout) || errors.Is(err, context.DeadlineExceeded) {
		return replyTTLExpired
	}
	if upstream.IsTargetUnreachable(err) {
		return replyHostUnreachable
	}
	if upstream.IsAuthRejected(err) {
		return replyGeneralFailure
	}
	return replyGeneralFailure
}

// isLocalDestination reports whether ip must never leave the device
// (loopback, RFC1918, link-local, multicast, CGNAT, ULA; PLAN.md §2.4).
func isLocalDestination(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 0:
			return true
		case v4[0] == 10:
			return true
		case v4[0] == 100 && v4[1]&0xC0 == 64:
			return true // 100.64.0.0/10 CGNAT
		case v4[0] == 172 && v4[1]&0xF0 == 16:
			return true // 172.16.0.0/12
		case v4[0] == 192 && v4[1] == 168:
			return true // 192.168.0.0/16
		case v4[0] == 255 && v4[1] == 255 && v4[2] == 255 && v4[3] == 255:
			return true
		}
		return false
	}
	if len(ip) == net.IPv6len && ip[0]&0xFE == 0xFC {
		return true // fc00::/7 ULA
	}
	return false
}

func (s *Server) knownUnreachable(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.refusals[key]
	return rec != nil && time.Now().Before(rec.expires)
}

func (s *Server) rememberUnreachable(key string, targetPort int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refusals == nil {
		s.refusals = make(map[string]*refusal)
	}
	rec := s.refusals[key]
	if rec == nil {
		rec = &refusal{}
		s.refusals[key] = rec
		if len(s.refusals) > refusalCacheCap {
			for k := range s.refusals {
				delete(s.refusals, k)
				break
			}
		}
	}
	rec.strikes++
	ttl := refusalTTLBase << min(rec.strikes-1, 16)
	if ttl > refusalTTLCap || ttl <= 0 {
		ttl = refusalTTLCap
	}
	rec.expires = time.Now().Add(ttl)
	if rec.strikes == refusalsBeforePolicy {
		s.logf(logging.Debug, "socks", "the edge refused %s %d times; backing off (port %d)", key, rec.strikes, targetPort)
	}
}

func (s *Server) forgetUnreachable(key string) {
	s.mu.Lock()
	delete(s.refusals, key)
	s.mu.Unlock()
}
