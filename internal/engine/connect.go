package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/dns"
	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/guardian"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/netcfg"
	"github.com/benyamin-git/kumiho/internal/serverlist"
	"github.com/benyamin-git/kumiho/internal/settings"
	"github.com/benyamin-git/kumiho/internal/socks"
	"github.com/benyamin-git/kumiho/internal/tun"
	"github.com/benyamin-git/kumiho/internal/upstream"
)

// upstreamSession is the data-plane surface the daemon uses; the concrete
// *upstream.Session implements it (and tests substitute fakes).
type upstreamSession interface {
	OpenStream(ctx context.Context, host string, port int) (net.Conn, error)
	Totals() (up, down int64)
	IsConnected() bool
	Close() error
}

// fatalError stops the tunnel until user action (quota, token).
type fatalError struct{ msg string }

func (e *fatalError) Error() string { return e.msg }

// Dial retry backoff for the initial connect sequence (PLAN.md §2.4); vars
// so tests can compress it.
var (
	dialRetryBackoffMin = 2 * time.Second
	dialRetryBackoffMax = 8 * time.Second
)

// Connect brings up the tunnel (PLAN.md §12): access token → Guardian pass →
// HTTP/2 session; full mode adds TUN + netstack + netcfg + DNS, proxy-only
// mode starts just the local SOCKS5 listener.
func (c *Controller) Connect(ctx context.Context, req api.ConnectPayload) error {
	c.mu.Lock()
	if c.session != nil || c.state == StateConnecting {
		c.mu.Unlock()
		return &api.RemoteError{Code: api.CodeBadRequest, Message: "already connected; disconnect first"}
	}
	if c.tokens == nil {
		c.mu.Unlock()
		return &api.RemoteError{Code: api.CodeNotAuthenticated, Message: "sign in first (kumiho login)"}
	}
	c.setStateLocked(StateConnecting)
	c.mu.Unlock()
	c.notify()

	if err := c.EnsureFreshToken(ctx); err != nil {
		c.connectFailed(fmt.Errorf("refresh access token: %w", err))
		return err
	}

	countries, err := c.Locations(ctx, false)
	if err != nil {
		c.connectFailed(err)
		return err
	}
	cand, err := resolveCandidate(countries, req, c.store.State())
	if err != nil {
		c.connectFailed(err)
		return err
	}

	pass, err := c.acquirePass(ctx)
	if err != nil {
		c.connectFailed(err)
		return err
	}

	// Initial dial: up to 4 attempts with full-jitter backoff, alternating
	// edges — same city, then same country (PLAN.md §2.4).
	sequence := append([]serverlist.Candidate{cand}, serverlist.AlternateCandidates(countries, cand, 3)...)
	var sess upstreamSession
	var lastErr error
	for attempt := 1; attempt <= 4; attempt++ {
		next := sequence[(attempt-1)%len(sequence)]
		sess, err = c.dialUpstream(ctx, c.upstreamOptions(ctx, next, pass))
		if err == nil {
			cand = next
			break
		}
		lastErr = err
		if c.routeProbe() {
			c.noteServerFailure(next.Address())
		}
		if ctx.Err() != nil {
			break
		}
		if attempt < 4 {
			backoff := dialRetryBackoffMin + time.Duration(rand.Int64N(int64(dialRetryBackoffMax-dialRetryBackoffMin)))
			c.log.Logf(logging.Warn, "tunnel", "dial %s attempt %d/4 failed: %v (retrying in %s)",
				next.Address(), attempt, err, backoff.Round(time.Millisecond))
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
			}
		}
	}
	if sess == nil {
		err = fmt.Errorf("connect %s: %w", cand.Address(), lastErr)
		c.connectFailed(err)
		return err
	}
	c.clearServerFailures(cand.Address())
	c.log.Logf(logging.Info, "tunnel", "edge session up (%s); starting the data path", cand.Address())
	c.rememberEdgePeer(cand.Host, sess)

	if err := c.store.UpdateState(func(s *settings.State) {
		s.SelectedLocation = cand.CountryCode
		s.SelectedCity = cand.CityCode
		s.SelectedServer = cand.Address()
	}); err != nil {
		c.log.Logf(logging.Warn, "tunnel", "could not persist the selection: %v", err)
	}

	// Publish the session before the data path starts: DNS, SOCKS and the TUN
	// engine dial through it live, and a later watchdog redial swaps it in
	// place (PLAN.md §4.2).
	c.mu.Lock()
	if c.state != StateConnecting {
		// A disconnect landed while we were dialing; abandon the tunnel.
		c.mu.Unlock()
		_ = sess.Close()
		return errors.New("connect was cancelled by a disconnect")
	}
	c.session = sess
	c.cand = cand
	c.proxyOnly = req.ProxyOnly
	c.mu.Unlock()

	res, err := c.startTunnel(ctx, req.ProxyOnly)
	if err != nil {
		c.mu.Lock()
		if c.session == sess {
			c.session = nil
		}
		c.mu.Unlock()
		_ = sess.Close()
		c.connectFailed(err)
		return err
	}

	stop := make(chan struct{})
	c.mu.Lock()
	if c.state != StateConnecting {
		// A disconnect landed while the data path was starting.
		c.mu.Unlock()
		res.Close()
		_ = sess.Close()
		c.mu.Lock()
		if c.session == sess {
			c.session = nil
		}
		c.mu.Unlock()
		return errors.New("connect was cancelled by a disconnect")
	}
	c.resources = res
	c.socksSrv = res.socks
	c.pass = pass
	c.quota = quotaFromPass(pass)
	c.exitIP, c.exitCountry = "", ""
	c.metricsStop = stop
	c.authRejected = false
	if req.ProxyOnly {
		c.setStateLocked(StateProxyOnly)
	} else {
		c.setStateLocked(StateConnected)
	}
	c.mu.Unlock()

	socksAddr := res.socks.Addr()
	mode := "full tunnel"
	if req.ProxyOnly {
		mode = "proxy-only"
	}
	c.log.Logf(logging.Info, "tunnel", "connected via %s (%s/%s, %s; SOCKS5 on %s)",
		cand.Address(), cand.CountryCode, cand.CityCode, mode, socksAddr)
	c.notify()

	cfg := c.store.EffectiveConfig()
	c.startMetrics(stop)
	c.startPassRenewal(stop, pass)
	c.startWatchdog(stop)
	if cfg.ExitCheck && c.exitCheckURL != "" {
		go c.exitCheck(socksAddr.String(), cand.CountryCode)
	}
	return nil
}

// tunnelResources owns every teardown step of one connection attempt.
type tunnelResources struct {
	mu        sync.Mutex
	proxyOnly bool
	applier   *netcfg.Applier
	ncfg      netcfg.Config
	dev       *tun.Device
	engine    *tun.Engine
	dns       *dns.Server
	socks     *socks.Server
}

// Close tears the tunnel down in reverse order; safe to call twice.
func (r *tunnelResources) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	dnsSrv, engine, dev, socksSrv := r.dns, r.engine, r.dev, r.socks
	applier, ncfg, proxyOnly := r.applier, r.ncfg, r.proxyOnly
	r.dns, r.engine, r.dev, r.socks, r.applier = nil, nil, nil, nil, nil
	r.mu.Unlock()

	if dnsSrv != nil {
		dnsSrv.Stop()
	}
	if applier != nil && !proxyOnly {
		// Cleanup while the interface still exists, so `resolvectl revert`
		// succeeds quietly instead of racing the device's removal.
		applier.Cleanup(context.Background(), ncfg)
	}
	if engine != nil {
		engine.Stop() // closes the TUN device, which unblocks its reader
	}
	if dev != nil {
		_ = dev.Close() // idempotent when the engine closed it already
	}
	if socksSrv != nil {
		socksSrv.Stop()
	}
}

// Reapply re-renders the network configuration (used when the kill switch
// is toggled while connected).
func (r *tunnelResources) Reapply(ctx context.Context, cfg netcfg.Config) error {
	r.mu.Lock()
	applier := r.applier
	r.mu.Unlock()
	if applier == nil {
		return errors.New("netcfg: tunnel is not active")
	}
	if err := applier.Apply(ctx, cfg); err != nil {
		return err
	}
	r.mu.Lock()
	r.ncfg = cfg
	r.mu.Unlock()
	return nil
}

// devName returns the TUN interface name (or the default).
func (r *tunnelResources) devName() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dev != nil {
		return r.dev.Name()
	}
	return netcfg.DefaultTunName
}

// startTunnel builds the data path: full mode adds TUN + netstack + netcfg +
// the DNS resolver; both modes start the local SOCKS5 listener. All stream
// dialers go through c.openStream so a watchdog redial swaps sessions under
// the running data path (PLAN.md §4.2).
func (c *Controller) startTunnel(ctx context.Context, proxyOnly bool) (*tunnelResources, error) {
	cfg := c.store.EffectiveConfig()
	res := &tunnelResources{proxyOnly: proxyOnly}
	fail := func(err error) (*tunnelResources, error) {
		// Log before teardown so a stuck teardown step can never hide the
		// real cause (it did during M4 acceptance).
		c.log.Logf(logging.Warn, "tunnel", "setup failed, tearing down: %v", err)
		res.Close()
		return nil, err
	}

	if !proxyOnly {
		c.log.Logf(logging.Info, "tunnel", "creating TUN %s (MTU %d)", netcfg.DefaultTunName, cfg.MTU)
		dev, err := tun.Open(netcfg.DefaultTunName, cfg.MTU)
		if err != nil {
			return fail(err)
		}
		res.dev = dev

		engine, err := tun.Start(dev, cfg.MTU, c.openStream, func(format string, args ...any) {
			c.log.Logf(logging.Debug, "tun", format, args...)
		})
		if err != nil {
			return fail(fmt.Errorf("start netstack: %w", err))
		}
		res.engine = engine

		res.applier = c.applier
		res.ncfg = c.netcfgConfig(dev.Name())
		c.log.Logf(logging.Info, "tunnel", "applying network configuration (kill switch %v)", res.ncfg.KillSwitch)
		if err := c.applier.Apply(ctx, res.ncfg); err != nil {
			return fail(fmt.Errorf("apply network configuration: %w", err))
		}

		dnsSrv, err := dns.New(c.openStream, cfg.DoHProvider, cfg.DNSUpstream, c.log.Logf)
		if err != nil {
			return fail(err)
		}
		dnsSrv.Addr = net.JoinHostPort(netcfg.DefaultTunAddr, "53")
		if err := dnsSrv.Start(); err != nil {
			return fail(fmt.Errorf("start DNS resolver: %w", err))
		}
		res.dns = dnsSrv
		c.log.Logf(logging.Info, "tunnel", "resolver up on %s (DoH through the tunnel)", dnsSrv.Addr)
	}

	port := cfg.SocksPort
	if c.socksPortOverride > 0 {
		port = c.socksPortOverride
	}
	socksSrv := &socks.Server{
		Bind:           cfg.SocksBind,
		Port:           port,
		Dial:           c.openStream,
		OnAuthRejected: c.noteAuthRejected,
		Logf:           c.log.Logf,
	}
	if err := socksSrv.Start(); err != nil {
		return fail(err)
	}
	res.socks = socksSrv
	return res, nil
}

// CleanupStaleNetcfg removes routes/rules/nft/resolved artifacts a crashed
// daemon may have left behind (PLAN.md §4.3). Called once at startup, only
// after this daemon owns the control socket.
func (c *Controller) CleanupStaleNetcfg() {
	c.applier.Cleanup(context.Background(), c.netcfgConfig(netcfg.DefaultTunName))
}

// netcfgConfig renders the current policy-routing configuration.
func (c *Controller) netcfgConfig(tunName string) netcfg.Config {
	cfg := c.store.EffectiveConfig()
	return netcfg.Config{
		TunName:      tunName,
		TunAddr:      netcfg.DefaultTunAddr,
		TunMTU:       cfg.MTU,
		KillSwitch:   c.store.EffectiveKillSwitch(),
		AllowLAN:     cfg.AllowLAN,
		ExcludeCIDRs: cfg.ExcludeCIDRs,
	}
}

// ReapplyNetcfg re-renders the nftables/routing configuration for the active
// full tunnel (kill-switch toggle). No-op in proxy-only or idle states.
func (c *Controller) ReapplyNetcfg(ctx context.Context) {
	c.mu.Lock()
	res := c.resources
	c.mu.Unlock()
	if res == nil || res.proxyOnly {
		return
	}
	if err := res.Reapply(ctx, c.netcfgConfig(res.devName())); err != nil {
		c.log.Logf(logging.Warn, "netcfg", "re-apply failed: %v", err)
	}
}

// UseSocketMarking tags every daemon-owned socket with SO_MARK 0x2 so the
// policy-routing mark chain never routes control-plane traffic into the
// tunnel (PLAN.md §3.2). Called once at daemon start.
func (c *Controller) UseSocketMarking() {
	dial := netcfg.MarkedDialer()
	newClient := func(timeout time.Duration) *http.Client {
		return &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext:       dial.DialContext,
				ForceAttemptHTTP2: true,
			},
		}
	}
	c.httpClient = newClient(20 * time.Second)
	c.fxa.HTTP = newClient(30 * time.Second)
	c.guardian.HTTP = newClient(30 * time.Second)
	c.upstreamDialer = dial

	// Bootstrap resolvers for edge hostnames (PLAN.md §3.5 role 1): marked
	// sockets keep working when the tunnel is down, unlike the system
	// resolver, which then points at the dead in-tunnel resolver.
	cfg := c.store.EffectiveConfig()
	c.bootstrap = newBootstrapResolvers(dial.DialContext, cfg.DoHProvider, cfg.DNSUpstream, c.log.Logf)
}

// renewalDelay mirrors PLAN.md §2.3: half the remaining lifetime, clamped to
// [15 s, 30 min], never later than expiry−30 s; 4 min when unknown.
func renewalDelay(pass *guardian.Pass, now time.Time) time.Duration {
	if pass == nil || pass.ExpiresAt == 0 {
		return 4 * time.Minute
	}
	remaining := time.Unix(pass.ExpiresAt, 0).Sub(now)
	if remaining <= 0 {
		return 15 * time.Second
	}
	delay := remaining / 2
	if limit := remaining - 30*time.Second; delay > limit {
		delay = limit
	}
	delay = delay.Round(time.Second)
	switch {
	case delay < 15*time.Second:
		return 15 * time.Second
	case delay > 30*time.Minute:
		return 30 * time.Minute
	default:
		return delay
	}
}

// startPassRenewal swaps fresh proxy passes into the live session in place
// (existing streams keep running; new streams pick up the new bearer).
func (c *Controller) startPassRenewal(stop <-chan struct{}, initial *guardian.Pass) {
	go func() {
		current := initial
		for {
			delay := renewalDelay(current, time.Now())
			timer := time.NewTimer(delay)
			select {
			case <-stop:
				timer.Stop()
				return
			case <-timer.C:
			}

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			renewed, err := c.acquirePass(ctx)
			cancel()
			if err != nil {
				var fatal *fatalError
				if errors.As(err, &fatal) {
					c.fatalStop("pass renewal failed: " + fatal.Error())
					return
				}
				c.log.Logf(logging.Warn, "tunnel", "pass renewal failed, retrying in 30s: %v", err)
				retry := time.NewTimer(30 * time.Second)
				select {
				case <-stop:
					retry.Stop()
					return
				case <-retry.C:
				}
				continue
			}
			c.mu.Lock()
			c.pass = renewed
			c.quota = quotaFromPass(renewed)
			c.mu.Unlock()
			current = renewed
			c.log.Logf(logging.Info, "tunnel", "proxy pass renewed (expires %s)",
				time.Unix(renewed.ExpiresAt, 0).Format(time.RFC3339))
			c.notify()
		}
	}()
}

// Disconnect tears the tunnel down (idempotent for FATAL states).
func (c *Controller) Disconnect() error {
	c.mu.Lock()
	active := c.session != nil || c.socksSrv != nil || c.state == StateConnecting || c.state == StateFatal
	c.mu.Unlock()
	if !active {
		return &api.RemoteError{Code: api.CodeBadRequest, Message: "not connected"}
	}
	c.stopTunnel("user disconnect")
	return nil
}

// Shutdown tears the data path down on daemon exit (PLAN.md §4.3): stop the
// tunnel, delete its rules/nft table, revert DNS and stop the SOCKS listener.
// Unlike Disconnect it is silent when nothing is up, so Run can always call
// it; a stopped service must leave no residue behind (the kill-switch rules
// would otherwise outlive the daemon).
func (c *Controller) Shutdown() {
	c.stopTunnel("daemon shutdown")
}

// stopTunnel closes the data path and returns the state machine to IDLE.
func (c *Controller) stopTunnel(reason string) {
	c.mu.Lock()
	sess, res := c.session, c.resources
	stop := c.metricsStop
	wasConnected := sess != nil
	c.session, c.socksSrv, c.resources, c.metricsStop = nil, nil, nil, nil
	c.proxyOnly = false
	c.cand = serverlist.Candidate{}
	c.authRejected = false
	if stop != nil {
		close(stop)
	}
	c.exitIP, c.exitCountry = "", ""
	c.ratesUp, c.ratesDown = 0, 0
	if c.state != StateUnauthenticated {
		c.setStateLocked(StateIdle)
	}
	c.mu.Unlock()

	if res != nil {
		res.Close()
	}
	if sess != nil {
		_ = sess.Close()
	}
	if wasConnected {
		c.log.Logf(logging.Info, "tunnel", "disconnected (%s)", reason)
	}
	c.notify()
}

func (c *Controller) connectFailed(err error) {
	var fatal *fatalError
	if errors.As(err, &fatal) {
		c.mu.Lock()
		c.setStateLocked(StateFatal)
		c.mu.Unlock()
		c.log.Logf(logging.Error, "tunnel", "fatal: %v", err)
		c.notify()
		return
	}
	c.mu.Lock()
	if c.state != StateUnauthenticated {
		c.setStateLocked(StateIdle)
	}
	c.mu.Unlock()
	c.log.Logf(logging.Warn, "tunnel", "connect failed: %v", err)
	c.notify()
}

// acquirePass fetches a Guardian proxy pass, mirroring the reference retry
// chain: 401/403 → refresh access token → retry → activate → retry once.
func (c *Controller) acquirePass(ctx context.Context) (*guardian.Pass, error) {
	tokens := c.currentTokens()
	if tokens == nil {
		return nil, &api.RemoteError{Code: api.CodeNotAuthenticated, Message: "sign in first (kumiho login)"}
	}

	pass, err := c.guardian.FetchPass(ctx, tokens.AccessToken)
	if err == nil {
		return pass, nil
	}
	switch {
	case guardian.IsQuotaExceeded(err):
		return nil, &fatalError{"the account's VPN quota is exhausted; the tunnel stays off until the quota resets"}
	case guardian.IsUnauthorized(err):
		if rerr := c.refreshAccessToken(ctx); rerr != nil {
			return nil, fmt.Errorf("Guardian rejected the access token and refreshing failed: %w", rerr)
		}
		tokens = c.currentTokens()
		pass, err = c.guardian.FetchPass(ctx, tokens.AccessToken)
		if err == nil {
			return pass, nil
		}
		if !guardian.IsUnauthorized(err) {
			return nil, err
		}
		if _, aerr := c.guardian.Activate(ctx, tokens.AccessToken); aerr != nil {
			if guardian.IsUnauthorized(aerr) {
				return nil, &fatalError{"Mozilla rejected the account session; sign in again (kumiho login)"}
			}
			return nil, fmt.Errorf("activate Guardian entitlement: %w", aerr)
		}
		c.log.Logf(logging.Info, "guardian", "activated the VPN entitlement after a token rejection")
		pass, err = c.guardian.FetchPass(ctx, tokens.AccessToken)
		if err != nil {
			return nil, err
		}
		return pass, nil
	case guardian.IsChallenge(err):
		return nil, errors.New("Mozilla's edge sent an anti-bot challenge to Guardian; the solver is not implemented yet (PLAN.md §2.2)")
	default:
		return nil, err
	}
}

func (c *Controller) currentTokens() *fxa.Tokens {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tokens
}

func (c *Controller) upstreamOptions(ctx context.Context, cand serverlist.Candidate, pass *guardian.Pass) upstream.Options {
	cfg := c.store.EffectiveConfig()
	pinned := cfg.EdgeAddress
	if pinned == "" {
		// Resolve edge names outside the tunnel: the system resolver points
		// at the in-tunnel server while connected and cannot answer during
		// an outage (PLAN.md §3.5 role 1).
		pinned = c.resolveEdge(ctx, cand.Host)
	}
	return upstream.Options{
		Host:     cand.Host,
		Port:     cand.Port,
		PinnedIP: pinned,
		Dialer:   c.upstreamDialer,
		Pass: func() string {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.pass != nil {
				return c.pass.Token
			}
			return ""
		},
		Logf: c.log.Logf,
	}
}

// resolveCandidate picks the connect target from the request, the persisted
// selection, or the recommended location (PLAN.md §2.4).
func resolveCandidate(countries []serverlist.Country, req api.ConnectPayload, persisted settings.State) (serverlist.Candidate, error) {
	if req.Server != "" {
		for _, cand := range serverlist.Candidates(countries, req.LocationCode, req.CityCode) {
			if cand.Address() == req.Server || cand.Host == req.Server {
				return cand, nil
			}
		}
		return serverlist.Candidate{}, &api.RemoteError{
			Code:    api.CodeBadRequest,
			Message: fmt.Sprintf("server %q is not in the server list; refresh with: kumiho locations --refresh", req.Server),
		}
	}
	if req.LocationCode != "" {
		cands := serverlist.Candidates(countries, req.LocationCode, req.CityCode)
		if len(cands) == 0 {
			return serverlist.Candidate{}, &api.RemoteError{
				Code:    api.CodeBadRequest,
				Message: fmt.Sprintf("no servers for location %q", req.LocationCode),
			}
		}
		if strings.EqualFold(req.LocationCode, serverlist.RecommendedCode) {
			return cands[rand.IntN(len(cands))], nil
		}
		return cands[0], nil
	}

	if persisted.SelectedServer != "" {
		for _, cand := range serverlist.Candidates(countries, persisted.SelectedLocation, persisted.SelectedCity) {
			if cand.Address() == persisted.SelectedServer {
				return cand, nil
			}
		}
	}
	if persisted.SelectedLocation != "" {
		if cands := serverlist.Candidates(countries, persisted.SelectedLocation, persisted.SelectedCity); len(cands) > 0 {
			return cands[0], nil
		}
	}

	if cands := serverlist.Candidates(countries, serverlist.RecommendedCode, ""); len(cands) > 0 {
		return cands[rand.IntN(len(cands))], nil
	}
	for _, country := range countries {
		if cands := serverlist.Candidates(countries, country.Code, ""); len(cands) > 0 {
			return cands[0], nil
		}
	}
	return serverlist.Candidate{}, &api.RemoteError{Code: api.CodeInternal, Message: "the server list has no usable servers"}
}

func quotaFromPass(pass *guardian.Pass) *api.Quota {
	if pass == nil {
		return nil
	}
	if pass.QuotaLimit == nil && pass.QuotaRemaining == nil && pass.QuotaReset == nil {
		return nil
	}
	return &api.Quota{
		Limit:     pass.QuotaLimit,
		Remaining: pass.QuotaRemaining,
		Reset:     pass.QuotaReset,
	}
}

// startMetrics samples session totals every 2 s (PLAN.md §4.2). It follows
// the live session so a watchdog redial does not stall the rates.
func (c *Controller) startMetrics(stop <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		var lastUp, lastDown int64
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
			sess := c.currentSession()
			var up, down int64
			if sess != nil {
				up, down = sess.Totals()
				if up < lastUp || down < lastDown {
					// The session was swapped; restart the rate baseline.
					lastUp, lastDown = up, down
				}
			}
			c.mu.Lock()
			c.ratesUp = float64(up-lastUp) / 2
			c.ratesDown = float64(down-lastDown) / 2
			c.totalsUp, c.totalsDown = up, down
			c.mu.Unlock()
			lastUp, lastDown = up, down
			c.notify()
		}
	}()
}

// exitCheck fetches Cloudflare's trace through the local SOCKS listener
// (PLAN.md §2.4: once per connect, non-fatal).
func (c *Controller) exitCheck(socksAddr, expectedCountry string) {
	proxyURL, err := url.Parse("socks5://" + socksAddr)
	if err != nil {
		return
	}
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	client := &http.Client{Transport: transport, Timeout: 20 * time.Second}
	defer client.CloseIdleConnections()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.exitCheckURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", guardian.UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		c.log.Logf(logging.Debug, "exitcheck", "exit check failed (non-fatal): %v", err)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		c.log.Logf(logging.Debug, "exitcheck", "exit check read failed (non-fatal): %v", err)
		return
	}
	ip, country := parseCloudflareTrace(string(body))
	if ip == "" && country == "" {
		c.log.Logf(logging.Debug, "exitcheck", "exit check returned no usable trace data")
		return
	}

	c.mu.Lock()
	c.exitIP, c.exitCountry = ip, country
	c.mu.Unlock()
	c.log.Logf(logging.Info, "exitcheck", "exit IP %s (%s)", ip, country)
	if country != "" &&
		!strings.EqualFold(expectedCountry, serverlist.RecommendedCode) &&
		!strings.EqualFold(country, expectedCountry) {
		c.log.Logf(logging.Warn, "exitcheck", "exit country %s does not match the selected %s", country, expectedCountry)
	}
	c.notify()
}

func parseCloudflareTrace(body string) (ip, country string) {
	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(line, "ip="):
			ip = strings.TrimSpace(strings.TrimPrefix(line, "ip="))
		case strings.HasPrefix(line, "loc="):
			country = strings.TrimSpace(strings.TrimPrefix(line, "loc="))
		}
	}
	return ip, country
}
