// Package engine implements the embeddable kumiho core: the session state
// machine, token persistence, and connection control (PLAN.md §4-5).
package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/guardian"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/platform"
	"github.com/benyamin-git/kumiho/internal/serverlist"
	"github.com/benyamin-git/kumiho/internal/settings"
	"github.com/benyamin-git/kumiho/internal/socks"
	"github.com/benyamin-git/kumiho/internal/upstream"
)

// State is the daemon connection state machine (PLAN.md §4.1). Only the
// authentication states are reachable until M3/M4 add the tunnel.
type State string

const (
	StateUnauthenticated State = "UNAUTHENTICATED"
	StateIdle            State = "IDLE"
	StateConnecting      State = "CONNECTING"
	StateConnected       State = "CONNECTED"
	StateReconnecting    State = "RECONNECTING"
	StateWaitingNetwork  State = "WAITING_NETWORK"
	StateProxyOnly       State = "PROXY_ONLY"
	StateFatal           State = "FATAL"
)

type pendingLogin struct {
	email        string
	sessionToken string
}

// Controller owns the daemon session state and login state machine.
type Controller struct {
	store *settings.Store
	log   *logging.Ring
	fxa   *fxa.Client

	httpClient    *http.Client
	serverListURL string

	guardian     *guardian.Client
	exitCheckURL string
	plat         platform.Provider
	nc           platform.NetConfig
	userAgent    string

	// upstreamDialer is the bypass dialer for edge connections (nil until
	// UseBypassDialer runs, which is fine for tests).
	upstreamDialer *net.Dialer

	// bootstrap resolves edge hostnames outside the tunnel (marked sockets);
	// empty when unavailable, in which case the system resolver is used.
	bootstrap []bootstrapResolver
	edgeMu    sync.Mutex
	edgeIPs   map[string]edgeEntry

	// dialUpstream is injectable for tests; it defaults to upstream.Dial.
	dialUpstream      func(ctx context.Context, opts upstream.Options) (upstreamSession, error)
	socksPortOverride int

	mu       sync.Mutex
	state    State
	since    time.Time
	email    string
	tokens   *fxa.Tokens
	pending  *pendingLogin
	onChange []func()

	locations   []serverlist.Country
	locationsAt time.Time

	// Tunnel state (PLAN.md §4.1).
	session      upstreamSession
	socksSrv     *socks.Server
	resources    *tunnelResources
	pass         *guardian.Pass
	quota        *api.Quota
	exitIP       string
	exitCountry  string
	totalsUp     int64
	totalsDown   int64
	ratesUp      float64
	ratesDown    float64
	metricsStop  chan struct{}
	proxyOnly    bool
	cand         serverlist.Candidate
	authRejected bool

	// Watchdog knobs, captured at construction so the connection-lifetime
	// goroutines never read the package-level defaults (tests rewrite those
	// between runs; -race rightly flags that otherwise).
	watchdogInterval time.Duration
	redialJitterMin  time.Duration
	redialJitterMax  time.Duration
	routeProbe       func() bool
}

// netcfgLogfSetter is the optional capability a platform NetConfig implements
// to receive the engine-wired logger.
type netcfgLogfSetter interface {
	SetLogf(func(format string, args ...any))
}

// New creates a controller; call Start before serving requests.
func New(store *settings.Store, ring *logging.Ring, client *fxa.Client, plat platform.Provider) *Controller {
	if plat == nil {
		panic("engine: nil platform provider")
	}
	ua := plat.UserAgent()
	client.UserAgent = ua
	c := &Controller{
		store:         store,
		log:           ring,
		fxa:           client,
		plat:          plat,
		nc:            plat.NetConfig(),
		userAgent:     ua,
		httpClient:    &http.Client{Timeout: 20 * time.Second},
		serverListURL: serverlist.RecordsURL,
		guardian:      guardian.NewClient(),
		exitCheckURL:  "https://www.cloudflare.com/cdn-cgi/trace",
		dialUpstream: func(ctx context.Context, opts upstream.Options) (upstreamSession, error) {
			return upstream.Dial(ctx, opts)
		},
		state: StateUnauthenticated,
		since: time.Now(),

		watchdogInterval: defaultWatchdogInterval,
		redialJitterMin:  defaultRedialJitterMin,
		redialJitterMax:  defaultRedialJitterMax,
		routeProbe:       plat.HasDefaultRoute,
	}
	c.guardian.UserAgent = ua
	if ls, ok := c.nc.(netcfgLogfSetter); ok {
		ls.SetLogf(ringLogf(ring))
	}
	return c
}

func ringLogf(ring *logging.Ring) func(format string, args ...any) {
	if ring == nil {
		return nil
	}
	return func(format string, args ...any) {
		ring.Logf(logging.Info, "netcfg", format, args...)
	}
}

func (c *Controller) setStateLocked(s State) {
	c.state = s
	c.since = time.Now()
}

// OnChange registers a callback fired after visible state changes.
func (c *Controller) OnChange(fn func()) {
	c.mu.Lock()
	c.onChange = append(c.onChange, fn)
	c.mu.Unlock()
}

func (c *Controller) notify() {
	c.mu.Lock()
	fns := make([]func(), len(c.onChange))
	copy(fns, c.onChange)
	c.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// currentSession returns the live upstream session (nil when idle).
func (c *Controller) currentSession() upstreamSession {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session
}

// openStream dials one TCP stream through the live upstream session. The TUN
// engine, SOCKS server and DNS resolver all go through this indirection so a
// watchdog redial can swap sessions under them (PLAN.md §4.2).
func (c *Controller) openStream(ctx context.Context, host string, port int) (net.Conn, error) {
	sess := c.currentSession()
	if sess == nil {
		return nil, errors.New("kumiho: not connected")
	}
	return sess.OpenStream(ctx, host, port)
}

// noteAuthRejected recycles the session after the edge refused the proxy
// pass; the watchdog redials with a freshly minted pass (PLAN.md §2.4).
func (c *Controller) noteAuthRejected() {
	c.mu.Lock()
	first := !c.authRejected
	c.authRejected = true
	sess := c.session
	c.mu.Unlock()
	if first {
		c.log.Logf(logging.Warn, "tunnel", "the edge rejected the proxy pass; recycling the session")
	}
	if sess != nil {
		_ = sess.Close()
	}
}

// Start loads the persisted session. It never performs network I/O; call
// EnsureFreshToken in the background to renew stale access tokens.
func (c *Controller) Start(_ context.Context) error {
	tokens, err := loadTokens(c.store.Paths().Tokens)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.email = c.store.State().Email
	c.since = time.Now()
	if tokens != nil && (c.fxa.AccessTokenValid(tokens) || tokens.RefreshToken != "") {
		c.tokens = tokens
		c.state = StateIdle
		c.log.Logf(logging.Info, "auth", "restored session for %s", c.email)
	} else {
		c.state = StateUnauthenticated
	}
	return nil
}

// EnsureFreshToken renews the access token when it is stale, clearing the
// session on a permanent rejection.
func (c *Controller) EnsureFreshToken(ctx context.Context) error {
	tokens := c.currentTokens()
	if tokens == nil {
		return errors.New("not signed in")
	}
	if c.fxa.AccessTokenValid(tokens) {
		return nil
	}
	return c.refreshAccessToken(ctx)
}

// refreshAccessToken force-renews the access token (also used when Guardian
// rejects it mid-session).
func (c *Controller) refreshAccessToken(ctx context.Context) error {
	tokens := c.currentTokens()
	if tokens == nil {
		return errors.New("not signed in")
	}
	if tokens.RefreshToken == "" {
		c.clearSession("stored session cannot be renewed")
		return errors.New("stored session cannot be renewed; sign in again")
	}

	refreshed, err := c.fxa.Refresh(ctx, tokens.RefreshToken)
	if err != nil {
		var apiErr *fxa.APIError
		if errors.As(err, &apiErr) && apiErr.PermanentRefreshRejection() {
			c.clearSession("refresh token rejected")
		}
		return err
	}
	if err := saveTokens(c.store.Paths().Tokens, refreshed); err != nil {
		return err
	}
	c.mu.Lock()
	c.tokens = refreshed
	c.mu.Unlock()
	c.log.Logf(logging.Info, "auth", "renewed FxA access token")
	c.notify()
	return nil
}

// LoginEmail starts the login state machine.
func (c *Controller) LoginEmail(email string) api.LoginState {
	email = strings.TrimSpace(email)
	if email == "" {
		return api.LoginState{Step: "email", Error: "email is required"}
	}
	c.mu.Lock()
	if c.tokens != nil {
		c.mu.Unlock()
		return api.LoginState{Step: "done", Email: c.email, Error: "already signed in; log out first to switch accounts"}
	}
	c.pending = &pendingLogin{email: email}
	c.mu.Unlock()
	return api.LoginState{Step: "password", Email: email}
}

// LoginPassword performs the FxA login and either completes it or moves to
// the 2FA step.
func (c *Controller) LoginPassword(ctx context.Context, password string) api.LoginState {
	c.mu.Lock()
	pending := c.pending
	c.mu.Unlock()
	if pending == nil || pending.email == "" {
		return api.LoginState{Step: "email", Error: "enter your email first"}
	}
	if password == "" {
		return api.LoginState{Step: "password", Email: pending.email, Error: "password is required"}
	}

	res, err := c.fxa.Login(ctx, pending.email, password)
	if err != nil {
		c.log.Logf(logging.Warn, "auth", "login failed for %s: %v", pending.email, err)
		return api.LoginState{Step: "password", Email: pending.email, Error: friendlyLoginError(err)}
	}
	if !res.Verified {
		c.mu.Lock()
		c.pending.sessionToken = res.SessionToken
		c.mu.Unlock()
		c.log.Logf(logging.Info, "auth", "verification required (%s) for %s", res.VerificationMethod, pending.email)
		return api.LoginState{
			Step:               "2fa",
			Email:              pending.email,
			VerificationMethod: res.VerificationMethod,
			VerificationReason: res.VerificationReason,
		}
	}
	return c.completeLogin(ctx, pending.email, res.SessionToken)
}

// Login2FA verifies the emailed code, or with an empty code re-checks the
// email-link flow via GET /session/status.
func (c *Controller) Login2FA(ctx context.Context, code string) api.LoginState {
	c.mu.Lock()
	pending := c.pending
	c.mu.Unlock()
	if pending == nil || pending.sessionToken == "" {
		return api.LoginState{Step: "email", Error: "sign-in was interrupted; start again"}
	}

	if strings.TrimSpace(code) == "" {
		state, err := c.fxa.SessionStatus(ctx, pending.sessionToken)
		if err != nil {
			return api.LoginState{Step: "2fa", Email: pending.email, Error: friendlyLoginError(err)}
		}
		if state != "verified" {
			return api.LoginState{
				Step:  "2fa",
				Email: pending.email,
				Error: "email link not confirmed yet; click the link in the email, then press Enter",
			}
		}
	} else if err := c.fxa.VerifyCode(ctx, pending.sessionToken, strings.TrimSpace(code)); err != nil {
		return api.LoginState{Step: "2fa", Email: pending.email, Error: friendlyLoginError(err)}
	}
	return c.completeLogin(ctx, pending.email, pending.sessionToken)
}

func (c *Controller) completeLogin(ctx context.Context, email, sessionToken string) api.LoginState {
	tokens, err := c.fxa.OAuthToken(ctx, sessionToken)
	if err != nil {
		return api.LoginState{Step: "2fa", Email: email, Error: "could not fetch OAuth tokens: " + friendlyLoginError(err)}
	}
	if err := saveTokens(c.store.Paths().Tokens, tokens); err != nil {
		return api.LoginState{Step: "done", Email: email, Error: "signed in, but persisting tokens failed: " + err.Error()}
	}

	c.mu.Lock()
	c.tokens = tokens
	c.state = StateIdle
	c.email = email
	c.pending = nil
	c.since = time.Now()
	c.mu.Unlock()

	if err := c.store.UpdateState(func(s *settings.State) { s.Email = email }); err != nil {
		c.log.Logf(logging.Warn, "auth", "could not persist account email: %v", err)
	}
	c.log.Logf(logging.Info, "auth", "signed in as %s", email)
	c.notify()
	return api.LoginState{Step: "done", Email: email}
}

// Logout clears tokens and returns to UNAUTHENTICATED.
func (c *Controller) Logout() error {
	c.clearSession("user logout")
	return nil
}

func (c *Controller) clearSession(reason string) {
	c.stopTunnel("session cleared")
	if err := clearTokens(c.store.Paths().Tokens); err != nil {
		c.log.Logf(logging.Warn, "auth", "could not remove tokens: %v", err)
	}
	c.mu.Lock()
	c.tokens = nil
	c.pending = nil
	c.email = ""
	c.state = StateUnauthenticated
	c.since = time.Now()
	c.mu.Unlock()
	if err := c.store.UpdateState(func(s *settings.State) { s.Email = "" }); err != nil {
		c.log.Logf(logging.Warn, "auth", "could not clear stored email: %v", err)
	}
	c.log.Logf(logging.Info, "auth", "session cleared (%s)", reason)
	c.notify()
}

// Snapshot returns the current status for TUI/CLI clients.
func (c *Controller) Snapshot() api.Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	persisted := c.store.State()
	st := api.Status{
		State:         string(c.state),
		Authenticated: c.tokens != nil,
		Email:         c.email,
		Since:         c.since.Unix(),
		KillSwitch:    c.store.EffectiveKillSwitch(),
		Autoconnect:   c.store.EffectiveAutoconnect(),
		Location:      persisted.SelectedLocation,
		Server:        persisted.SelectedServer,
		ProxyOnly:     c.state == StateProxyOnly || (persisted.ProxyOnly != nil && *persisted.ProxyOnly),
	}
	if c.tokens != nil {
		st.TokenExpires = c.tokens.ExpiresAt
	}
	if c.pass != nil {
		st.PassExpires = c.pass.ExpiresAt
	}
	if c.quota != nil {
		st.Quota = c.quota
	}
	if c.session != nil {
		st.ExitIP, st.ExitCountry = c.exitIP, c.exitCountry
		up, down := c.session.Totals()
		if up == 0 && down == 0 {
			up, down = c.totalsUp, c.totalsDown
		}
		st.Totals = &api.ByteCounters{Up: up, Down: down}
		st.Rates = &api.ByteRates{Up: c.ratesUp, Down: c.ratesDown}
	} else if c.totalsUp > 0 || c.totalsDown > 0 {
		// Keep the last session's totals visible after disconnect.
		st.Totals = &api.ByteCounters{Up: c.totalsUp, Down: c.totalsDown}
		st.Rates = &api.ByteRates{}
	}
	switch {
	case c.resources != nil && !c.resources.proxyOnly:
		st.DNS = c.plat.DNSListenAddr() + " (DoH through the tunnel)"
		st.IPv6 = "blackholed"
	case c.session != nil:
		st.DNS = "system (proxy-only mode)"
		st.IPv6 = "untouched"
	}
	return st
}

// Settings returns the runtime settings snapshot (effective values plus the
// list of keys currently overridden from the admin config).
func (c *Controller) Settings() api.SettingsView {
	cfg := c.store.EffectiveConfig()
	return api.SettingsView{
		SocksPort:            cfg.SocksPort,
		MTU:                  cfg.MTU,
		ExitCheck:            cfg.ExitCheck,
		DoHProvider:          cfg.DoHProvider,
		DNSUpstream:          cfg.DNSUpstream,
		EdgeAddress:          cfg.EdgeAddress,
		UpstreamProxy:        cfg.UpstreamProxy,
		ExcludeCIDRs:         cfg.ExcludeCIDRs,
		AllowLAN:             cfg.AllowLAN,
		LogLevel:             cfg.LogLevel,
		RedactTargets:        cfg.RedactTargets,
		KillSwitch:           cfg.KillSwitch,
		Autoconnect:          cfg.Autoconnect,
		KillSwitchEffective:  c.store.EffectiveKillSwitch(),
		AutoconnectEffective: c.store.EffectiveAutoconnect(),
		QuotaPollMinutes:     cfg.QuotaPollMinutes,
		Overridden:           c.store.OverriddenKeys(),
	}
}

// SetSetting changes one runtime setting (no override reset).
func (c *Controller) SetSetting(key, value string) error {
	return c.ApplySetting(api.SettingsSetPayload{Key: key, Value: value})
}

// ApplySetting validates and persists one runtime setting change and pushes
// it to its live consumers. Connection-scoped settings are picked up on the
// next connect; log_level and redact_targets apply immediately. Reset drops
// the override so the admin config applies again.
func (c *Controller) ApplySetting(p api.SettingsSetPayload) error {
	key := strings.TrimSpace(p.Key)
	var err error
	if p.Reset {
		err = c.store.ClearOverride(key)
	} else {
		err = c.store.SetOverride(key, p.Value)
	}
	switch {
	case errors.Is(err, settings.ErrUnknownSetting):
		return &api.RemoteError{
			Code:    api.CodeUnsupportedSetting,
			Message: fmt.Sprintf("setting %q is not runtime-changeable yet", key),
		}
	case err != nil:
		return &api.RemoteError{Code: api.CodeBadRequest, Message: err.Error()}
	}

	switch key {
	case "kill_switch":
		c.ReapplyNetcfg(context.Background())
	case "log_level", "redact_targets":
		c.applyLoggingSettings()
	}
	c.notify()
	return nil
}

// applyLoggingSettings pushes the effective log level and sanitizer into the
// live ring (both are always-on consumers of their settings).
func (c *Controller) applyLoggingSettings() {
	cfg := c.store.EffectiveConfig()
	if lvl, err := logging.ParseLevel(cfg.LogLevel); err == nil {
		c.log.SetMinLevel(lvl)
	}
	c.log.SetSanitize(logging.Sanitizer(cfg.RedactTargets))
}

// Account returns the Account screen snapshot: local session facts plus a
// best-effort Guardian entitlement lookup (PLAN.md §6). Failures of the
// online lookup are reported in Error without hiding the local fields.
func (c *Controller) Account(ctx context.Context) api.AccountInfo {
	tokens := c.currentTokens()
	c.mu.Lock()
	info := api.AccountInfo{Email: c.email}
	c.mu.Unlock()
	if tokens == nil {
		info.Error = "not signed in"
		return info
	}
	info.TokenExpires = tokens.ExpiresAt

	ent, err := c.guardian.Account(ctx, tokens.AccessToken)
	if err != nil && guardian.IsUnauthorized(err) {
		if rerr := c.refreshAccessToken(ctx); rerr == nil {
			if t := c.currentTokens(); t != nil {
				info.TokenExpires = t.ExpiresAt
				ent, err = c.guardian.Account(ctx, t.AccessToken)
			}
		}
	}
	if err != nil {
		info.Error = err.Error()
		return info
	}
	info.UID = ent.UID
	info.Subscribed = ent.Subscribed
	info.MaxBytes = ent.MaxBytes
	info.LimitedBandwidth = ent.LimitedBandwidth
	info.QuotaRemaining = ent.QuotaRemaining
	return info
}

func friendlyLoginError(err error) string {
	var apiErr *fxa.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Message
	}
	var chErr *fxa.ChallengeError
	if errors.As(err, &chErr) {
		return "Mozilla's edge sent an anti-bot challenge; automatic solving arrives in M3. Try again later or from a different network."
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "the request timed out; check the host clock and network"
	}
	if errors.Is(err, context.Canceled) {
		return "the request was cancelled"
	}
	return err.Error()
}
