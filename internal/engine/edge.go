package engine

import (
	"context"
	"net"
	"strconv"
	"time"

	mdns "github.com/miekg/dns"

	"github.com/benyamin-git/kumiho/internal/dns"
	"github.com/benyamin-git/kumiho/internal/logging"
)

// bootstrapResolver answers DNS messages over the marked (outside-tunnel)
// path. *dns.Server satisfies it via HandleQuery.
type bootstrapResolver interface {
	HandleQuery(req *mdns.Msg, overUDP bool) *mdns.Msg
}

// Bootstrap timing follows PLAN.md §3.5 role 1: 3 s provider timeouts and a
// 5 min edge-answer cache. IPs captured from live sessions stay usable longer
// because dialing them just worked.
const (
	bootstrapDialTimeout = 3 * time.Second
	bootstrapLookupLimit = 6 * time.Second
	edgeCacheTTL         = 5 * time.Minute
	edgePeerCacheTTL     = 30 * time.Minute
	systemLookupLimit    = 5 * time.Second
)

type edgeEntry struct {
	ip      string
	expires time.Time
}

// newBootstrapResolvers builds one resolver per provider over the marked
// dialer (PLAN.md §3.5 role 1): DoH by IP literal with a TCP:53 fallback,
// both outside the tunnel, so edge names resolve while the tunnel is down.
// The configured provider is raced alongside the well-known ones because a
// single provider may be blocked on the physical network (found live: 1.1.1.1
// was unreachable directly from a test host).
func newBootstrapResolvers(
	dial func(ctx context.Context, network, addr string) (net.Conn, error),
	provider, upstream string,
	logf func(level logging.Level, tag, format string, args ...any),
) []bootstrapResolver {
	var ips []string
	seen := map[string]bool{}
	add := func(ip string) {
		if ip == "" || seen[ip] {
			return
		}
		seen[ip] = true
		ips = append(ips, ip)
	}
	if ip, _, err := dns.ProviderFromConfig(provider, upstream); err == nil {
		add(ip)
	}
	for _, ip := range []string{"1.1.1.1", "8.8.8.8", "9.9.9.9"} {
		add(ip)
	}

	out := make([]bootstrapResolver, 0, len(ips))
	for _, ip := range ips {
		marked := func(ctx context.Context, host string, port int) (net.Conn, error) {
			capped, cancel := context.WithTimeout(ctx, bootstrapDialTimeout)
			defer cancel()
			return dial(capped, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		}
		// Provider blocking on the physical network is expected — the race
		// exists because of it — so bootstrap failures are debug-level and
		// tagged "edge" rather than polluting the [dns] warn stream. The
		// aggregate "bootstrap DNS could not resolve" warn still surfaces
		// when every provider fails.
		bootstrapLogf := func(level logging.Level, _, format string, args ...any) {
			if logf == nil {
				return
			}
			if level == logging.Warn {
				level = logging.Debug
			}
			logf(level, "edge", "bootstrap: "+format, args...)
		}
		srv, err := dns.New(marked, "", ip, bootstrapLogf)
		if err != nil {
			continue
		}
		out = append(out, srv)
	}
	return out
}

// resolveEdge resolves an edge hostname so redials keep working while the
// tunnel is down (PLAN.md §3.5 role 1). Order: IP cache (session peers and
// previous answers), bootstrap providers raced outside the tunnel, then the
// system resolver — whose own answers are cached so later redials do not
// depend on it. Returns "" when nothing resolves; the dialer then falls back
// to the system resolver.
func (c *Controller) resolveEdge(ctx context.Context, host string) string {
	if host == "" || net.ParseIP(host) != nil {
		return host
	}
	if ip := c.cachedEdgeIP(host); ip != "" {
		return ip
	}

	if len(c.bootstrap) > 0 {
		if ip := raceBootstrap(c.bootstrap, host); ip != "" {
			c.cacheEdgeIP(host, ip, edgeCacheTTL)
			c.log.Logf(logging.Debug, "edge", "resolved %s via bootstrap DNS: %s", host, ip)
			return ip
		}
		c.log.Logf(logging.Warn, "edge", "bootstrap DNS could not resolve %s; falling back to the system resolver", host)
	}

	lookupCtx, cancel := context.WithTimeout(ctx, systemLookupLimit)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(lookupCtx, host)
	if err != nil {
		return ""
	}
	ip := preferIPv4(addrs)
	if ip == "" {
		return ""
	}
	c.cacheEdgeIP(host, ip, edgeCacheTTL)
	c.log.Logf(logging.Debug, "edge", "resolved %s via the system resolver: %s", host, ip)
	return ip
}

// raceBootstrap queries every provider in parallel and returns the first
// answer.
func raceBootstrap(resolvers []bootstrapResolver, host string) string {
	results := make(chan string, len(resolvers))
	for _, res := range resolvers {
		go func(res bootstrapResolver) {
			results <- queryBootstrap(res, host)
		}(res)
	}

	deadline := time.After(bootstrapLookupLimit)
	for range resolvers {
		select {
		case ip := <-results:
			if ip != "" {
				return ip
			}
		case <-deadline:
			return ""
		}
	}
	return ""
}

// queryBootstrap asks one provider for A, then AAAA.
func queryBootstrap(res bootstrapResolver, host string) string {
	for _, qtype := range []uint16{mdns.TypeA, mdns.TypeAAAA} {
		req := new(mdns.Msg)
		req.SetQuestion(mdns.Fqdn(host), qtype)
		if ip := firstAddress(res.HandleQuery(req, false)); ip != "" {
			return ip
		}
	}
	return ""
}

// rememberEdgePeer caches the IP a live session actually uses, so a redial
// to the same edge skips resolution entirely — the edge was reachable
// seconds ago even if every resolver is now impaired.
func (c *Controller) rememberEdgePeer(host string, sess upstreamSession) {
	type remoteAddr interface{ RemoteAddr() net.Addr }
	ra, ok := sess.(remoteAddr)
	if !ok {
		return
	}
	tcp, ok := ra.RemoteAddr().(*net.TCPAddr)
	if !ok || tcp.IP == nil {
		return
	}
	c.cacheEdgeIP(host, tcp.IP.String(), edgePeerCacheTTL)
}

func (c *Controller) cachedEdgeIP(host string) string {
	c.edgeMu.Lock()
	defer c.edgeMu.Unlock()
	e, ok := c.edgeIPs[host]
	if !ok || time.Now().After(e.expires) {
		return ""
	}
	return e.ip
}

func (c *Controller) cacheEdgeIP(host, ip string, ttl time.Duration) {
	c.edgeMu.Lock()
	defer c.edgeMu.Unlock()
	if c.edgeIPs == nil {
		c.edgeIPs = make(map[string]edgeEntry)
	}
	c.edgeIPs[host] = edgeEntry{ip: ip, expires: time.Now().Add(ttl)}
}

// preferIPv4 returns the first IPv4 address, or the first address otherwise.
func preferIPv4(addrs []string) string {
	var fallback string
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip != nil && ip.To4() != nil {
			return a
		} else if fallback == "" && a != "" {
			fallback = a
		}
	}
	return fallback
}

// firstAddress returns the first A/AAAA answer address of a successful reply.
func firstAddress(resp *mdns.Msg) string {
	if resp == nil || resp.Rcode != mdns.RcodeSuccess {
		return ""
	}
	for _, rr := range resp.Answer {
		switch a := rr.(type) {
		case *mdns.A:
			return a.A.String()
		case *mdns.AAAA:
			return a.AAAA.String()
		}
	}
	return ""
}
