// Package dns implements the in-daemon resolver (PLAN.md §3.5): a local
// listener on 10.8.0.2:53 (UDP+TCP) whose queries are resolved through the
// tunnel via DoH to a provider IP, falling back to DNS-over-TCP:53 through
// the same tunnel.
package dns

import (
	"bytes"
	"container/list"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	mdns "github.com/miekg/dns"

	"github.com/benyamin-git/kumiho/internal/logging"
)

// Dialer opens one TCP stream through the tunnel (upstream session).
type Dialer func(ctx context.Context, host string, port int) (net.Conn, error)

// Provider IPs from PLAN.md §3.5 / config.doh_provider.
var providerIPs = map[string]string{
	"automatic":  "1.1.1.1",
	"cloudflare": "1.1.1.1",
	"google":     "8.8.8.8",
	"quad9":      "9.9.9.9",
}

// dohOperators are IPs known to speak DoH at /dns-query.
var dohOperators = map[string]bool{
	"1.1.1.1": true, "1.0.0.1": true,
	"8.8.8.8": true, "8.8.4.4": true,
	"9.9.9.9": true, "149.112.112.112": true,
}

const (
	// DefaultListen is the in-TUN resolver address (PLAN.md §3.1).
	DefaultListen = "10.8.0.2:53"
	// DefaultMaxEntries bounds the cache.
	DefaultMaxEntries = 10000
	minTTL            = 5 * time.Second
	maxTTL            = time.Hour
	queryTimeout      = 8 * time.Second
	maxMessageBytes   = 64 << 10
)

// Server is the local DNS listener.
type Server struct {
	Addr string // default 10.8.0.2:53
	Dial Dialer

	// Logf receives operational logs (may be nil).
	Logf func(level logging.Level, tag, format string, args ...any)

	// Resolve overrides the DoH/TCP chain (tests).
	Resolve func(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error)

	providerIP string
	doh        bool
	client     *http.Client
	cache      *cache

	udpSrv *mdns.Server
	tcpSrv *mdns.Server
}

// ProviderFromConfig maps doh_provider + dns_upstream to a resolver IP.
func ProviderFromConfig(provider, upstream string) (ip string, doh bool, err error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	upstream = strings.TrimSpace(upstream)
	if upstream != "" {
		if net.ParseIP(upstream) == nil {
			return "", false, fmt.Errorf("dns: dns_upstream %q is not an IP address", upstream)
		}
		return upstream, dohOperators[upstream], nil
	}
	if provider == "off" {
		return "", false, nil
	}
	ip, ok := providerIPs[provider]
	if !ok {
		return "", false, fmt.Errorf("dns: unknown doh_provider %q", provider)
	}
	return ip, true, nil
}

// New builds a resolver server. An empty provider IP (provider "off")
// answers SERVFAIL for everything.
func New(dial Dialer, provider, upstream string, logf func(level logging.Level, tag, format string, args ...any)) (*Server, error) {
	ip, doh, err := ProviderFromConfig(provider, upstream)
	if err != nil {
		return nil, err
	}
	s := &Server{
		Addr:       DefaultListen,
		Dial:       dial,
		Logf:       logf,
		providerIP: ip,
		doh:        doh,
		cache:      newCache(DefaultMaxEntries),
	}
	s.client = &http.Client{
		Timeout: queryTimeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
				host, portStr, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				port, err := strconv.Atoi(portStr)
				if err != nil {
					return nil, err
				}
				return s.Dial(ctx, host, port)
			},
			// Quad9 answers HTTP/1.1 DoH requests with 505; ALPN h2 is
			// negotiated end-to-end with the provider (the tunnel carries
			// opaque TLS), so all providers accept the requests.
			ForceAttemptHTTP2: true,
		},
	}
	return s, nil
}

func (s *Server) logf(level logging.Level, format string, args ...any) {
	if s.Logf != nil {
		s.Logf(level, "dns", format, args...)
	}
}

// Start binds UDP and TCP and serves until Stop.
func (s *Server) Start() error {
	if s.Dial == nil && s.Resolve == nil {
		return errors.New("dns: a Dial or Resolve function is required")
	}
	addr := s.Addr
	if addr == "" {
		addr = DefaultListen
	}
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return fmt.Errorf("dns: listen udp %s: %w", addr, err)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		_ = pc.Close()
		return fmt.Errorf("dns: listen tcp %s: %w", addr, err)
	}

	mux := mdns.NewServeMux()
	mux.HandleFunc(".", s.serve)
	s.udpSrv = &mdns.Server{PacketConn: pc, Handler: mux}
	s.tcpSrv = &mdns.Server{Listener: ln, Handler: mux}
	go func() {
		if err := s.udpSrv.ActivateAndServe(); err != nil && !errors.Is(err, net.ErrClosed) {
			s.logf(logging.Warn, "udp server stopped: %v", err)
		}
	}()
	go func() {
		if err := s.tcpSrv.ActivateAndServe(); err != nil && !errors.Is(err, net.ErrClosed) {
			s.logf(logging.Warn, "tcp server stopped: %v", err)
		}
	}()
	s.logf(logging.Info, "resolver listening on %s (UDP+TCP)", addr)
	return nil
}

// Stop shuts both listeners down.
func (s *Server) Stop() {
	if s.udpSrv != nil {
		_ = s.udpSrv.Shutdown()
		s.udpSrv = nil
	}
	if s.tcpSrv != nil {
		_ = s.tcpSrv.Shutdown()
		s.tcpSrv = nil
	}
}

func (s *Server) serve(w mdns.ResponseWriter, req *mdns.Msg) {
	overUDP := false
	if _, ok := w.RemoteAddr().(*net.UDPAddr); ok {
		overUDP = true
	}
	resp := s.HandleQuery(req, overUDP)
	if resp == nil {
		resp = new(mdns.Msg)
		resp.SetRcode(req, mdns.RcodeServerFailure)
	}
	if err := w.WriteMsg(resp); err != nil {
		s.logf(logging.Debug, "write response: %v", err)
	}
}

// HandleQuery answers one DNS message. It is exported for tests.
func (s *Server) HandleQuery(req *mdns.Msg, overUDP bool) *mdns.Msg {
	if req == nil {
		return nil
	}
	if req.Opcode != mdns.OpcodeQuery {
		resp := new(mdns.Msg)
		resp.SetRcode(req, mdns.RcodeNotImplemented)
		return resp
	}
	if len(req.Question) != 1 {
		resp := new(mdns.Msg)
		resp.SetRcode(req, mdns.RcodeFormatError)
		return resp
	}
	q := req.Question[0]
	key := strings.ToLower(q.Name) + "|" + strconv.Itoa(int(q.Qtype))

	resp, ok := s.cache.get(key)
	if !ok {
		ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
		defer cancel()
		var err error
		resp, err = s.resolve(ctx, req)
		if err != nil {
			s.logf(logging.Debug, "resolve %s failed: %v", q.Name, err)
			resp = new(mdns.Msg)
			resp.SetRcode(req, mdns.RcodeServerFailure)
		}
		if resp != nil && resp.Rcode == mdns.RcodeSuccess {
			if ttl, ok := cacheTTL(resp); ok {
				s.cache.put(key, resp, ttl)
			}
		}
	}

	if resp == nil {
		resp = new(mdns.Msg)
		resp.SetRcode(req, mdns.RcodeServerFailure)
	}
	resp.Id = req.Id

	limit := 512
	if opt := req.IsEdns0(); opt != nil {
		if size := int(opt.UDPSize()); size > 512 {
			limit = size
		}
		if limit > 4096 {
			limit = 4096
		}
		resp.SetEdns0(uint16(limit), false)
	}
	if overUDP {
		if packed, err := resp.Pack(); err != nil || len(packed) > limit {
			trunc := new(mdns.Msg)
			trunc.SetReply(req)
			trunc.Truncated = true
			if req.IsEdns0() != nil {
				trunc.SetEdns0(uint16(limit), false)
			}
			return trunc
		}
	}
	return resp
}

// resolve tries DoH first (when configured) and falls back to TCP:53.
func (s *Server) resolve(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error) {
	if s.Resolve != nil {
		return s.Resolve(ctx, req)
	}
	if s.providerIP == "" {
		return nil, errors.New("no DNS provider configured")
	}
	if s.doh {
		resp, err := s.dohQuery(ctx, req)
		if err == nil {
			return resp, nil
		}
		s.logf(logging.Warn, "DoH to %s failed (%v); falling back to TCP:53", s.providerIP, err)
	}
	return s.tcpQuery(ctx, req)
}

func (s *Server) dohQuery(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error) {
	packed, err := req.Pack()
	if err != nil {
		return nil, err
	}
	url := "https://" + s.providerIP + "/dns-query"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(packed))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/dns-message")
	httpReq.Header.Set("Accept", "application/dns-message")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DoH HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMessageBytes))
	if err != nil {
		return nil, err
	}
	var msg mdns.Msg
	if err := msg.Unpack(body); err != nil {
		return nil, fmt.Errorf("DoH response: %w", err)
	}
	return &msg, nil
}

func (s *Server) tcpQuery(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error) {
	packed, err := req.Pack()
	if err != nil {
		return nil, err
	}
	conn, err := s.Dial(ctx, s.providerIP, 53)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline := time.Now().Add(queryTimeout)
	_ = conn.SetDeadline(deadline)

	var lb [2]byte
	binary.BigEndian.PutUint16(lb[:], uint16(len(packed)))
	if _, err := conn.Write(append(lb[:], packed...)); err != nil {
		return nil, err
	}
	var rb [2]byte
	if _, err := io.ReadFull(conn, rb[:]); err != nil {
		return nil, err
	}
	size := int(binary.BigEndian.Uint16(rb[:]))
	if size == 0 || size > maxMessageBytes {
		return nil, fmt.Errorf("invalid DNS-over-TCP response size %d", size)
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, err
	}
	var msg mdns.Msg
	if err := msg.Unpack(buf); err != nil {
		return nil, fmt.Errorf("DNS-over-TCP response: %w", err)
	}
	return &msg, nil
}

// cacheTTL returns the smallest TTL across the message, clamped.
func cacheTTL(msg *mdns.Msg) (time.Duration, bool) {
	min := time.Duration(0)
	found := false
	for _, rr := range msg.Answer {
		if ttl := time.Duration(rr.Header().Ttl) * time.Second; !found || ttl < min {
			min, found = ttl, true
		}
	}
	for _, rr := range msg.Ns {
		if ttl := time.Duration(rr.Header().Ttl) * time.Second; !found || ttl < min {
			min, found = ttl, true
		}
	}
	if !found {
		return 0, false
	}
	if min < minTTL {
		min = minTTL
	}
	if min > maxTTL {
		min = maxTTL
	}
	return min, true
}

// cache is a small LRU with TTL decay on read.
type cache struct {
	mu  sync.Mutex
	max int
	m   map[string]*list.Element
	lru *list.List
}

type cacheEntry struct {
	key    string
	msg    *mdns.Msg
	stored time.Time
}

func newCache(max int) *cache {
	return &cache{max: max, m: make(map[string]*list.Element), lru: list.New()}
}

func (c *cache) get(key string) (*mdns.Msg, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.m[key]
	if !ok {
		return nil, false
	}
	entry := el.Value.(*cacheEntry)
	if ttl, ok := cacheTTL(entry.msg); !ok || time.Since(entry.stored) >= ttl {
		c.removeLocked(el)
		return nil, false
	}
	c.lru.MoveToFront(el)
	return decay(entry.msg, time.Since(entry.stored)), true
}

func (c *cache) put(key string, msg *mdns.Msg, _ time.Duration) {
	copied := msg.Copy()
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.m[key]; ok {
		el.Value.(*cacheEntry).msg = copied
		el.Value.(*cacheEntry).stored = time.Now()
		c.lru.MoveToFront(el)
		return
	}
	el := c.lru.PushFront(&cacheEntry{key: key, msg: copied, stored: time.Now()})
	c.m[key] = el
	for c.lru.Len() > c.max {
		c.removeLocked(c.lru.Back())
	}
}

func (c *cache) removeLocked(el *list.Element) {
	entry := el.Value.(*cacheEntry)
	delete(c.m, entry.key)
	c.lru.Remove(el)
}

// decay returns a copy whose record TTLs are reduced by elapsed.
func decay(msg *mdns.Msg, elapsed time.Duration) *mdns.Msg {
	copied := msg.Copy()
	sub := uint32(elapsed / time.Second)
	adjust := func(rrs []mdns.RR) {
		for _, rr := range rrs {
			h := rr.Header()
			if h.Ttl > sub {
				h.Ttl -= sub
			} else {
				h.Ttl = 1
			}
		}
	}
	adjust(copied.Answer)
	adjust(copied.Ns)
	adjust(copied.Extra)
	return copied
}
