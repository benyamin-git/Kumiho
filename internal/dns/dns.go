// Package dns implements the in-daemon resolver (PLAN.md §3.5): a local
// listener on 10.8.0.2:53 (UDP+TCP) whose queries are resolved through the
// tunnel via DoH by IP literal.
//
// The in-tunnel path is strict: NewResolver builds a Resolver that fails
// over between DoH endpoints (the configured provider, Cloudflare and
// Quad9) and never sends plaintext DNS. NewWithResolver attaches a Resolver
// to the local listener.
//
// New remains as the legacy bootstrap/test constructor for the
// outside-the-tunnel bootstrap resolvers in internal/engine/edge.go, which
// intentionally keep their DNS-over-TCP:53 fallback on the marked path;
// tcpQuery exists only for that bootstrap/test path and must never be called
// by the in-tunnel resolver.
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
	endpointTimeout   = 4 * time.Second
	suppressDuration  = 30 * time.Minute
	maxMessageBytes   = 64 << 10
)

// Server is the local DNS listener.
type Server struct {
	Addr string // default 10.8.0.2:53
	Dial Dialer

	// Logf receives operational logs (may be nil).
	Logf func(level logging.Level, tag, format string, args ...any)

	// Resolve overrides the DoH chain (tests).
	Resolve func(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error)

	// resolver is the strict in-tunnel DoH resolver set by NewWithResolver.
	// When set, the listener delegates to it instead of the legacy
	// DoH + TCP:53 fallback below.
	resolver *Resolver

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

// New builds the legacy bootstrap/test resolver server: DoH by provider IP
// with a DNS-over-TCP:53 fallback, both on the Dialer. It is kept for the
// outside-the-tunnel bootstrap resolvers (internal/engine/edge.go); the
// in-tunnel path uses NewResolver + NewWithResolver instead. An empty
// provider IP (provider "off") answers SERVFAIL for everything.
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
			DialContext: dialContextFn(dial),
			// Quad9 answers HTTP/1.1 DoH requests with 505; ALPN h2 is
			// negotiated end-to-end with the provider (the tunnel carries
			// opaque TLS), so all providers accept the requests.
			ForceAttemptHTTP2: true,
		},
	}
	return s, nil
}

// dialContextFn adapts a Dialer to an http.Transport DialContext hook.
func dialContextFn(dial Dialer) func(ctx context.Context, _, addr string) (net.Conn, error) {
	return func(ctx context.Context, _, addr string) (net.Conn, error) {
		host, portStr, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, err
		}
		return dial(ctx, host, port)
	}
}

// endpoint is one DoH endpoint: an IP literal (PLAN.md §3.5 / D20) and the
// https://<ip>/dns-query URL it is queried at.
type endpoint struct {
	ip  string
	url string
}

// Resolver is the strict in-tunnel DNS path. It owns the DoH endpoint chain,
// the 8 s per-query budget (4 s per endpoint), the TTL cache and the
// in-memory per-endpoint suppression map. It never falls back to plaintext
// DNS.
type Resolver struct {
	// Logf receives operational logs (may be nil).
	Logf func(level logging.Level, tag, format string, args ...any)

	dial       Dialer
	endpoints  []endpoint
	client     *http.Client
	cache      *cache
	mu         sync.Mutex
	suppressed map[string]time.Time // endpoint IP -> suppressed until

	now func() time.Time // injectable clock (tests)
}

// NewResolver builds the strict in-tunnel resolver. The chain is the custom
// dns_upstream first when set (always DoH at https://<ip>/dns-query, D20),
// otherwise the configured provider, followed by Cloudflare 1.1.1.1 and Quad9
// 9.9.9.9; duplicates are dropped. Provider "off" without a custom upstream
// yields no endpoints, so every query answers SERVFAIL.
func NewResolver(dial Dialer, provider, upstream string, logf func(level logging.Level, tag, format string, args ...any)) (*Resolver, error) {
	if dial == nil {
		return nil, errors.New("dns: a Dial function is required")
	}
	ip, _, err := ProviderFromConfig(provider, upstream)
	if err != nil {
		return nil, err
	}
	r := &Resolver{
		Logf:       logf,
		dial:       dial,
		cache:      newCache(DefaultMaxEntries),
		suppressed: make(map[string]time.Time),
		now:        time.Now,
	}
	seen := make(map[string]bool)
	add := func(raw string) {
		canonical := canonicalIP(raw)
		if canonical == "" || seen[canonical] {
			return
		}
		seen[canonical] = true
		r.endpoints = append(r.endpoints, endpoint{ip: canonical, url: dohURL(canonical)})
	}
	primary := strings.TrimSpace(upstream)
	if primary == "" {
		primary = ip
	}
	if primary != "" {
		add(primary)
		add("1.1.1.1")
		add("9.9.9.9")
	}
	r.client = &http.Client{
		Transport: &http.Transport{
			DialContext:       dialContextFn(r.dial),
			ForceAttemptHTTP2: true,
		},
	}
	return r, nil
}

// canonicalIP returns the canonical text form of an IP literal, or "".
func canonicalIP(raw string) string {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return ""
	}
	return ip.String()
}

// dohURL renders the DoH endpoint URL for an IP literal, bracketing IPv6.
func dohURL(ip string) string {
	host := ip
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return "https://" + host + "/dns-query"
}

func (r *Resolver) logf(level logging.Level, format string, args ...any) {
	if r.Logf != nil {
		r.Logf(level, "dns", format, args...)
	}
}

// Resolve answers req through the endpoint chain. Transport, HTTP and unpack
// failures suppress that endpoint for 30 minutes and move on; a valid DNS
// message (including NXDOMAIN or SERVFAIL) is returned as-is. When every
// endpoint is suppressed, the one with the earliest suppression expiry is
// tried as a bounded half-open probe so a recovered network is noticed before
// the 30 minutes elapse. The query has an 8 s budget and each endpoint gets at
// most 4 s of it.
func (r *Resolver) Resolve(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error) {
	if req == nil {
		return nil, errors.New("dns: nil query")
	}
	if len(req.Question) != 1 {
		return nil, fmt.Errorf("dns: %d questions, want exactly 1", len(req.Question))
	}
	key := queryKey(req.Question[0])
	if resp, ok := r.cache.get(key); ok {
		return resp, nil
	}
	if len(r.endpoints) == 0 {
		return nil, errors.New("dns: no DoH endpoints configured")
	}
	packed, err := req.Pack()
	if err != nil {
		return nil, fmt.Errorf("dns: pack query: %w", err)
	}
	// The whole query gets 8 s, whatever deadline the caller supplied.
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	now := r.now()
	// A failure burst can suppress every endpoint at once; probing the one
	// whose suppression ends first recovers from transient outages instead of
	// blacking out DNS for the whole 30 minutes.
	probe := r.probeTarget(now)
	if probe != "" {
		r.logf(logging.Debug, "all DoH endpoints are suppressed; probing %s", probe)
	}

	var lastErr error
	for _, ep := range r.endpoints {
		if ep.ip != probe {
			if until, ok := r.suppressedUntil(ep.ip); ok && now.Before(until) {
				r.logf(logging.Debug, "skipping DoH endpoint %s until %s", ep.ip, until.Format(time.RFC3339))
				continue
			}
		}
		resp, err := r.queryEndpoint(ctx, ep, packed)
		if err != nil {
			lastErr = fmt.Errorf("DoH %s: %w", ep.ip, err)
			// Do not blame the endpoint when the caller or the overall query
			// budget cancelled the attempt.
			if ctx.Err() != nil {
				break
			}
			until := r.suppress(ep.ip)
			r.logf(logging.Warn, "DoH %s failed (%v); suppressing it for %s", ep.ip, err, until.Sub(r.now()).Round(time.Second))
			if ep.ip == probe {
				// A failed probe must not spend the budget on the other
				// still-suppressed endpoints.
				break
			}
			continue
		}
		if ep.ip == probe {
			r.unsuppress(ep.ip)
			r.logf(logging.Info, "DoH probe %s recovered; endpoint restored", ep.ip)
		}
		if resp.Rcode == mdns.RcodeSuccess {
			if ttl, ok := cacheTTL(resp); ok {
				r.cache.put(key, resp, ttl)
			}
		}
		return resp, nil
	}
	if lastErr == nil {
		return nil, errors.New("dns: no DoH endpoint answered")
	}
	return nil, fmt.Errorf("dns: all DoH endpoints failed: %w", lastErr)
}

// queryEndpoint performs one DoH POST, bounded to endpointTimeout.
func (r *Resolver) queryEndpoint(ctx context.Context, ep endpoint, packed []byte) (*mdns.Msg, error) {
	ctx, cancel := context.WithTimeout(ctx, endpointTimeout)
	defer cancel()
	return dohExchange(ctx, r.client, ep.url, packed)
}

func (r *Resolver) suppressedUntil(ip string) (time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	until, ok := r.suppressed[ip]
	return until, ok
}

func (r *Resolver) suppress(ip string) time.Time {
	until := r.now().Add(suppressDuration)
	r.mu.Lock()
	r.suppressed[ip] = until
	r.mu.Unlock()
	return until
}

// unsuppress clears an endpoint's suppression after a successful probe.
func (r *Resolver) unsuppress(ip string) {
	r.mu.Lock()
	delete(r.suppressed, ip)
	r.mu.Unlock()
}

// probeTarget returns the endpoint with the earliest suppression expiry when
// every endpoint is suppressed at now, and "" otherwise.
func (r *Resolver) probeTarget(now time.Time) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	target := ""
	var earliest time.Time
	for _, ep := range r.endpoints {
		until, ok := r.suppressed[ep.ip]
		if !ok || !now.Before(until) {
			return ""
		}
		if target == "" || until.Before(earliest) {
			target, earliest = ep.ip, until
		}
	}
	return target
}

// NewWithResolver returns a listener that delegates resolution to r. The
// resolver owns dialing, so the server's Dial stays nil.
func NewWithResolver(r *Resolver) *Server {
	s := &Server{Addr: DefaultListen, resolver: r}
	if r != nil {
		s.Logf = r.Logf
		s.cache = r.cache
	} else {
		s.cache = newCache(DefaultMaxEntries)
	}
	return s
}

// queryKey builds the cache key for one question.
func queryKey(q mdns.Question) string {
	return strings.ToLower(q.Name) + "|" + strconv.Itoa(int(q.Qtype))
}

func (s *Server) logf(level logging.Level, format string, args ...any) {
	if s.Logf != nil {
		s.Logf(level, "dns", format, args...)
	}
}

// Start binds UDP and TCP and serves until Stop.
func (s *Server) Start() error {
	if s.Dial == nil && s.Resolve == nil && s.resolver == nil {
		return errors.New("dns: a Dial, Resolve or Resolver is required")
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
	udpSrv, tcpSrv := s.udpSrv, s.tcpSrv
	go func() {
		if err := udpSrv.ActivateAndServe(); err != nil && !errors.Is(err, net.ErrClosed) {
			s.logf(logging.Warn, "udp server stopped: %v", err)
		}
	}()
	go func() {
		if err := tcpSrv.ActivateAndServe(); err != nil && !errors.Is(err, net.ErrClosed) {
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
	key := queryKey(q)

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

// resolve routes one query: the test override first, then the strict
// in-tunnel resolver, then the legacy bootstrap DoH + TCP:53 path.
func (s *Server) resolve(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error) {
	if s.Resolve != nil {
		return s.Resolve(ctx, req)
	}
	if s.resolver != nil {
		return s.resolver.Resolve(ctx, req)
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

// dohQuery is the legacy bootstrap/test DoH exchange against the single
// provider IP configured on New; the strict path uses Resolver instead.
func (s *Server) dohQuery(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error) {
	packed, err := req.Pack()
	if err != nil {
		return nil, err
	}
	return dohExchange(ctx, s.client, dohURL(s.providerIP), packed)
}

// dohExchange posts one DNS message to a DoH endpoint and unpacks the answer.
func dohExchange(ctx context.Context, client *http.Client, url string, packed []byte) (*mdns.Msg, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(packed))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/dns-message")
	httpReq.Header.Set("Accept", "application/dns-message")

	resp, err := client.Do(httpReq)
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

// tcpQuery is the plaintext DNS-over-TCP:53 fallback. It exists only for the
// outside-the-tunnel bootstrap resolvers (internal/engine/edge.go) and for
// legacy tests; no in-tunnel path may call it.
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
