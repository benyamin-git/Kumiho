// Package engine implements the embeddable kumiho core: the session state
// machine, token persistence, and connection control (PLAN.md §4-5).
package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/fastly"
	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/guardian"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/platform"
	"github.com/benyamin-git/kumiho/internal/serverlist"
	"github.com/benyamin-git/kumiho/internal/settings"
	"github.com/benyamin-git/kumiho/internal/socks"
	"github.com/benyamin-git/kumiho/internal/tlsplit"
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

const (
	// errnoAccountUnverified is FxA's login error for an account that needs
	// an emailed unblock code.
	errnoAccountUnverified = 125
)

type pendingLogin struct {
	email              string
	sessionToken       string
	authPW             string
	verificationMethod string
	verificationReason string
	method             string
	step               string
	targetID           string
}

var (
	// errRefreshSessionChanged reports that a token refresh finished after
	// the account session it started under had already changed.
	errRefreshSessionChanged = errors.New("the account session changed while the access token was being renewed")
	// errLoginSessionChanged reports that a sign-in finished after the
	// account session it started under had already changed.
	errLoginSessionChanged = errors.New("the account session changed while signing in")
)

// Controller owns the daemon session state and login state machine.
type Controller struct {
	store *settings.Store
	log   *logging.Ring
	fxa   *fxa.Client

	httpClient      *http.Client
	serverListURL   string
	serverListCache string

	// jar is the one cookie jar shared by every control-plane HTTP client
	// (httpClient, fxa.HTTP, guardian.HTTP) and the challenge solver for
	// the controller's lifetime, so a solved challenge cookie reaches all
	// of them.
	jar http.CookieJar
	// solver is the Fastly anti-bot challenge solver, wired as fxa.Solver
	// and guardian.OnChallenge.
	solver *fastly.Solver

	guardian             *guardian.Client
	rotationBudget       time.Duration
	rotationQuotaTimeout time.Duration
	exitCheckURL         string
	plat                 platform.Provider
	nc                   platform.NetConfig
	userAgent            string

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

	mu     sync.Mutex
	state  State
	since  time.Time
	email  string
	tokens *fxa.Tokens
	// accounts is the v2 account store; activeID selects one account and
	// rotationNotice is shown by the Account screen after a skipped rotation.
	// legacyNotice is shown once after superseded opaque browser accounts are
	// removed on load.
	accounts       *accountsFile
	activeID       string
	rotationNotice string
	legacyNotice   string
	// Browser sign-in is daemon-owned: PKCE material, the exchanged tokens and
	// status polling all live here, never in a client. The flow has no
	// listener; clients hand back the full URN redirect URL.
	browser        *browserLogin
	browserTimeout time.Duration
	// authGen fences network work that started under one account session
	// from publishing into the next. It is bumped on every sign-in and
	// sign-out, under mu.
	authGen  uint64
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
	watchdogDone chan struct{}
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
	// FxA's edge blocks the MozillaVPN UA on its OAuth endpoints; Guardian
	// still requires it, so the two control planes keep separate identities.
	client.UserAgent = fxa.BrowserUserAgent
	c := &Controller{
		store:           store,
		log:             ring,
		fxa:             client,
		plat:            plat,
		nc:              plat.NetConfig(),
		userAgent:       ua,
		httpClient:      &http.Client{Timeout: 20 * time.Second},
		serverListURL:   serverlist.RecordsURL,
		serverListCache: filepath.Join(filepath.Dir(store.Paths().State), "serverlist.json"),
		guardian:        guardian.NewClient(),
		rotationBudget:  15 * time.Second,
		// Quota is one HEAD (or a pass fallback) per account; a short cap
		// keeps a stalled Guardian from holding up the whole connect.
		rotationQuotaTimeout: 5 * time.Second,
		exitCheckURL:         "https://www.cloudflare.com/cdn-cgi/trace",
		browserTimeout:       10 * time.Minute,
		dialUpstream:         defaultDialUpstream,
		state:                StateUnauthenticated,
		since:                time.Now(),

		watchdogInterval: defaultWatchdogInterval,
		redialJitterMin:  defaultRedialJitterMin,
		redialJitterMax:  defaultRedialJitterMax,
		routeProbe:       plat.HasDefaultRoute,
	}
	c.guardian.UserAgent = ua
	if ls, ok := c.nc.(netcfgLogfSetter); ok {
		ls.SetLogf(ringLogf(ring))
	}

	// One cookie jar for the controller's lifetime, shared by every
	// control-plane client and the challenge solver: a cookie installed by
	// a solved challenge must be seen by all of them.
	jar, _ := cookiejar.New(nil) // cannot fail with nil options
	c.jar = jar
	c.httpClient.Jar = jar
	if client.HTTP == nil {
		client.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	c.fxa.HTTP.Jar = jar
	c.guardian.HTTP.Jar = jar

	// Wire the Fastly anti-bot solver into both challenge callbacks. Enabled
	// reads the effective config on every call (D31: solve_challenges applies
	// immediately, not at connect time). Logf is the ring's warn logger with
	// the solver's "fastly" tag; a nil ring must stay a no-op because
	// (*logging.Ring).Logf panics on a nil receiver.
	c.solver = &fastly.Solver{
		HTTP:      c.httpClient,
		SharedJar: jar,
		Enabled: func() bool {
			return c.store.EffectiveConfig().SolveChallenges
		},
	}
	if ring != nil {
		c.solver.Logf = ring.Logf
	}
	c.fxa.Solver = c.solver
	c.guardian.OnChallenge = c.solver.Solve
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

// tlsSplitMode reads the effective TLS fragmentation mode. Config validation
// rejects unknown values, so the error path only guards against a config that
// bypassed it; a bad value disables fragmentation instead of failing dials.
func (c *Controller) tlsSplitMode() tlsplit.Mode {
	cfg := c.store.EffectiveConfig()
	mode, err := tlsplit.ParseMode(cfg.TLSSplit)
	if err != nil {
		c.log.Logf(logging.Warn, "tlsplit", "invalid tls_split, disabling fragmentation: %v", err)
		return tlsplit.Off
	}
	return mode
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

// Start loads the persisted account store. It never performs network I/O;
// call EnsureFreshToken in the background to renew stale access tokens.
func (c *Controller) Start(_ context.Context) error {
	paths := c.store.Paths()
	f, exists, err := loadAccounts(accountsPath(paths))
	if err != nil {
		return err
	}
	if !exists {
		f, err = migrateAccounts(paths, c.store.State())
		if err != nil {
			return err
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.accounts = f
	c.activeID = f.Active
	if _, err := c.removeLegacyAccountsLocked(); err != nil {
		c.accounts = nil
		c.activeID = ""
		return err
	}
	active := c.activeAccountLocked()
	if active == nil && len(f.Accounts) > 0 {
		active = mostRecentlyUsedAccount(f)
		f.Active = active.ID
		if err := saveAccounts(accountsPath(paths), f); err != nil {
			return err
		}
		c.activeID = active.ID
	}
	c.since = time.Now()
	if active == nil {
		f.Active = ""
		c.activeID = ""
		c.email = ""
		c.tokens = nil
		c.state = StateUnauthenticated
		return nil
	}
	c.activeID = active.ID
	c.email = active.Email
	c.tokens = active.Tokens()
	if c.accountSessionUsable(active) {
		c.state = StateIdle
		c.log.Logf(logging.Info, "auth", "restored session for %s", c.email)
	} else {
		c.state = StateUnauthenticated
	}
	return nil
}

// removeLegacyAccountsLocked prunes accounts left by the superseded opaque
// browser flow, persists the pruned store plus the rollback token copy, and
// selects the most recently used remaining account when the active one was
// removed. Callers hold c.mu.
func (c *Controller) removeLegacyAccountsLocked() (bool, error) {
	if c.accounts == nil {
		return false, nil
	}
	clearedFields := 0
	for _, a := range c.accounts.Accounts {
		if a != nil && a.Method == MethodPassword && a.Browser != nil {
			clearedFields++
		}
	}
	removed := removeLegacyBrowserAccounts(c.accounts)
	if removed == 0 && clearedFields == 0 {
		return false, nil
	}
	if removed > 0 && c.activeAccountLocked() == nil {
		if mru := mostRecentlyUsedAccount(c.accounts); mru != nil {
			c.activeID = mru.ID
			c.accounts.Active = mru.ID
		} else {
			c.activeID = ""
			c.accounts.Active = ""
		}
	}
	// Persist the cleanup and refresh the tokens.json rollback copy so a
	// removed opaque token cannot survive an upgrade.
	if err := c.saveAccountsLocked(true); err != nil {
		return true, err
	}
	if removed > 0 {
		c.legacyNotice = legacyBrowserRemovalNotice
		c.log.Logf(logging.Info, "auth", "removed %d legacy browser account(s)", removed)
	}
	return true, nil
}

// accountSessionUsable reports whether Start may present the active account
// as a usable session. Every method needs a live FxA access token or a refresh
// token, and a durable re-authentication flag always makes it unusable.
func (c *Controller) accountSessionUsable(a *Account) bool {
	if a == nil || a.ReauthRequired {
		return false
	}
	tokens := a.Tokens()
	if tokens == nil {
		return false
	}
	return c.fxa.AccessTokenValid(tokens) || strings.TrimSpace(tokens.RefreshToken) != ""
}

func findAccountLocked(f *accountsFile, id string) *Account {
	if f == nil || strings.TrimSpace(id) == "" {
		return nil
	}
	for _, a := range f.Accounts {
		if a != nil && a.ID == id {
			return a
		}
	}
	return nil
}

func mostRecentlyUsedAccount(f *accountsFile) *Account {
	if f == nil {
		return nil
	}
	var best *Account
	for _, a := range f.Accounts {
		if a == nil {
			continue
		}
		if best == nil || a.LastUsedAt.After(best.LastUsedAt) ||
			(a.LastUsedAt.Equal(best.LastUsedAt) && a.AddedAt.After(best.AddedAt)) {
			best = a
		}
	}
	return best
}

func emailAccount(f *accountsFile, email string) *Account {
	if f == nil {
		return nil
	}
	email = strings.TrimSpace(email)
	for _, a := range f.Accounts {
		if a != nil && strings.EqualFold(strings.TrimSpace(a.Email), email) {
			return a
		}
	}
	return nil
}

// tunnelActiveLocked reports whether switching accounts would race a live
// data path. Callers hold c.mu.
func (c *Controller) tunnelActiveLocked() bool {
	return c.session != nil || c.socksSrv != nil || c.resources != nil ||
		c.state == StateConnecting || c.state == StateConnected || c.state == StateProxyOnly
}

// activeAccountLocked returns the selected account. Callers hold c.mu.
func (c *Controller) activeAccountLocked() *Account {
	if c.accounts == nil {
		return nil
	}
	id := c.activeID
	if id == "" {
		id = c.accounts.Active
	}
	return findAccountLocked(c.accounts, id)
}

// setActiveLocked selects a, updates the session fields and bumps the auth
// generation. It does not clear the durable re-authentication flag or persist;
// the caller saves after mutating the file. Callers hold c.mu.
func (c *Controller) setActiveLocked(a *Account) {
	c.authGen++
	now := time.Now()
	c.since = now
	if a == nil {
		c.activeID = ""
		c.email = ""
		c.tokens = nil
		if c.accounts != nil {
			c.accounts.Active = ""
		}
		if !c.tunnelActiveLocked() {
			c.state = StateUnauthenticated
		}
		return
	}
	c.activeID = a.ID
	c.email = a.Email
	c.tokens = a.Tokens()
	a.LastUsedAt = now
	a.LastError = ""
	if c.accounts != nil {
		c.accounts.Active = a.ID
		c.accounts.LastMethod = a.Method
	}
	if !c.tunnelActiveLocked() {
		c.state = StateIdle
	}
}

// ensureAccountsLocked loads accounts.json for entry points that run before
// Start (the embedded Android engine and tests call login directly). Callers
// hold c.mu.
func (c *Controller) ensureAccountsLocked() error {
	if c.accounts != nil {
		return nil
	}
	paths := c.store.Paths()
	f, exists, err := loadAccounts(accountsPath(paths))
	if err != nil {
		return err
	}
	if !exists {
		f, err = migrateAccounts(paths, c.store.State())
		if err != nil {
			return err
		}
	}
	c.accounts = f
	if c.activeID == "" {
		c.activeID = f.Active
	}
	if _, err := c.removeLegacyAccountsLocked(); err != nil {
		return err
	}
	if c.activeAccountLocked() == nil && len(f.Accounts) > 0 {
		if a := mostRecentlyUsedAccount(f); a != nil {
			c.activeID = a.ID
			f.Active = a.ID
		}
	}
	return nil
}

// saveAccountsLocked persists the account file and the legacy state/token
// pair. Callers hold c.mu. A legacy token-copy failure is logged, never
// returned: accounts.json is the authoritative store.
func (c *Controller) saveAccountsLocked(syncLegacy bool) error {
	if c.accounts == nil {
		c.accounts = &accountsFile{Version: 2}
	}
	active := c.activeAccountLocked()
	if active == nil {
		c.accounts.Active = ""
	} else {
		c.accounts.Active = active.ID
	}
	path := accountsPath(c.store.Paths())
	if len(c.accounts.Accounts) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else if err := saveAccounts(path, c.accounts); err != nil {
		return err
	}
	email := ""
	if active != nil {
		email = active.Email
	}
	if err := c.store.UpdateState(func(s *settings.State) { s.Email = email }); err != nil {
		c.log.Logf(logging.Warn, "auth", "could not persist account email: %v", err)
	}
	if syncLegacy {
		c.syncLegacyTokensLocked()
	}
	return nil
}

// syncLegacyTokensLocked rewrites the rollback copy. Callers hold c.mu.
func (c *Controller) syncLegacyTokensLocked() {
	var tokens *fxa.Tokens
	if a := c.activeAccountLocked(); a != nil {
		tokens = a.Tokens()
	}
	if err := syncTokensCopy(c.store.Paths().Tokens, tokens); err != nil {
		c.log.Logf(logging.Warn, "auth", "could not sync tokens.json: %v", err)
	}
}

// EnsureFreshToken renews the active account's stale access token. A browser
// account with a permanently rejected refresh token is marked for re-auth and
// kept; a password account keeps the existing drop-on-rejection behaviour.
func (c *Controller) EnsureFreshToken(ctx context.Context) error {
	c.mu.Lock()
	active := c.activeAccountLocked()
	tokens := c.tokens
	reauth := active != nil && active.ReauthRequired
	c.mu.Unlock()
	if tokens == nil {
		return errors.New("not signed in")
	}
	if reauth {
		return errors.New("the account needs sign-in again (kumiho login)")
	}
	if c.fxa.AccessTokenValid(tokens) {
		return nil
	}
	return c.refreshAccessToken(ctx)
}

// refreshAccessToken force-renews the access token (also used when Guardian
// rejects it mid-session). A MethodBrowser account whose refresh token is
// permanently rejected gets the durable re-auth flag and keeps its row; a
// password account is dropped exactly as before.
func (c *Controller) refreshAccessToken(ctx context.Context) error {
	c.mu.Lock()
	active := c.activeAccountLocked()
	tokens := c.tokens
	gen := c.authGen
	method, id := "", ""
	if active != nil {
		method, id = active.Method, active.ID
	}
	c.mu.Unlock()
	if tokens == nil {
		return errors.New("not signed in")
	}
	if strings.TrimSpace(tokens.RefreshToken) == "" {
		if method == MethodBrowser {
			c.markReauthIf(gen, true, id, "browser sign-in cannot be renewed; sign in again")
			return errors.New("the browser sign-in cannot be renewed; sign in again (kumiho login)")
		}
		c.dropActiveIf(gen, true, "stored session cannot be renewed")
		return errors.New("stored session cannot be renewed; sign in again")
	}

	refreshed, err := c.fxa.Refresh(ctx, tokens.RefreshToken)
	if err != nil {
		var apiErr *fxa.APIError
		if errors.As(err, &apiErr) && apiErr.PermanentRefreshRejection() {
			if method == MethodBrowser {
				c.markReauthIf(gen, true, id, "browser refresh token rejected; sign in again")
			} else {
				c.dropActiveIf(gen, true, "refresh token rejected")
			}
		}
		return err
	}

	c.mu.Lock()
	if c.authGen != gen {
		c.mu.Unlock()
		return errRefreshSessionChanged
	}
	active = c.activeAccountLocked()
	if active == nil {
		// Legacy callers can still hold a token set without an account.
		if err := saveTokens(c.store.Paths().Tokens, refreshed); err != nil {
			c.mu.Unlock()
			return err
		}
	} else {
		old := active.FxA
		active.FxA = refreshed
		// Persist while still holding mu so a sign-out cannot bump the
		// generation and delete the account file between check and write.
		if err := c.saveAccountsLocked(true); err != nil {
			active.FxA = old
			c.mu.Unlock()
			return err
		}
	}
	c.tokens = refreshed
	c.mu.Unlock()
	c.log.Logf(logging.Info, "auth", "renewed FxA access token")
	c.notify()
	return nil
}

// LoginEmail starts the password login state machine. It no longer refuses
// when another account is signed in: accounts switch on completion.
func (c *Controller) LoginEmail(email string) api.LoginState {
	return c.LoginStart(MethodPassword, email, "")
}

// LoginStart starts a browser or password sign-in. accountID selects an
// existing account for re-authentication; an empty value adds/switches to the
// email's account.
func (c *Controller) LoginStart(method, email, accountID string) api.LoginState {
	method = strings.ToLower(strings.TrimSpace(method))
	if method == "" {
		method = MethodPassword
	}
	email = strings.TrimSpace(email)
	accountID = strings.TrimSpace(accountID)
	if method != MethodPassword && method != MethodBrowser {
		return api.LoginState{Step: "error", Error: fmt.Sprintf("unknown sign-in method %q", method)}
	}
	if email != "" && strings.IndexFunc(email, invalidEmailRune) >= 0 {
		return api.LoginState{
			Step:   "email",
			Method: loginMethod(method),
			Email:  email,
			Error:  "enter a valid email address",
		}
	}

	c.mu.Lock()
	if err := c.ensureAccountsLocked(); err != nil {
		c.mu.Unlock()
		return api.LoginState{Step: "error", Method: loginMethod(method), Email: email, Error: err.Error()}
	}
	target := findAccountLocked(c.accounts, accountID)
	if accountID != "" && target == nil {
		c.mu.Unlock()
		return api.LoginState{Step: "error", Method: loginMethod(method), Email: email, Error: "account not found"}
	}
	if target != nil && email == "" {
		email = target.Email
	}
	if c.browser != nil {
		// A flow past its deadline must not block a fresh sign-in of either
		// method even if no status poll has run since it expired.
		c.expireBrowserLocked(c.browser)
		if c.browser.active {
			if method == MethodBrowser {
				state := c.browser.state
				state.Error = "a browser sign-in is already in progress; cancel it first"
				c.mu.Unlock()
				return state
			}
			c.mu.Unlock()
			return api.LoginState{
				Step:   "error",
				Method: "email",
				Email:  email,
				Error:  "cancel the browser sign-in first",
			}
		}
		// A terminal browser state belongs to the previous attempt; clear it
		// before starting a new sign-in of either method.
		c.browser = nil
	}
	if method == MethodBrowser && c.pending != nil {
		c.mu.Unlock()
		return api.LoginState{
			Step:   "error",
			Method: MethodBrowser,
			Email:  email,
			Error:  "finish or cancel the password sign-in first",
		}
	}
	if method == MethodPassword && email == "" {
		c.mu.Unlock()
		return api.LoginState{Step: "email", Method: "email", Error: "email is required"}
	}
	if c.tunnelActiveLocked() && (accountID == "" || accountID != c.activeID) {
		c.mu.Unlock()
		return api.LoginState{
			Step:   "error",
			Method: loginMethod(method),
			Email:  email,
			Error:  "disconnect first; adding or switching accounts needs the tunnel off",
		}
	}
	if method == MethodBrowser {
		pkce, err := fxa.NewPKCE(c.fxa.Rand)
		if err != nil {
			c.mu.Unlock()
			return api.LoginState{
				Step:   "error",
				Method: MethodBrowser,
				Email:  email,
				Error:  friendlyBrowserError(err),
			}
		}
		loginID, err := newAccountID()
		if err != nil {
			c.mu.Unlock()
			return api.LoginState{
				Step:   "error",
				Method: MethodBrowser,
				Email:  email,
				Error:  friendlyBrowserError(err),
			}
		}
		// Re-authentication pins the stored account email and asks FxA to
		// force that identity; adding an account only prefills a supplied
		// email and lets the user change it.
		forceAuth := false
		if accountID != "" && target != nil {
			if stored := strings.TrimSpace(target.Email); stored != "" {
				email = stored
				forceAuth = true
			}
		}
		authorizationURL := c.fxa.AuthorizationURL(email, forceAuth, pkce.State, pkce.Challenge)
		b := &browserLogin{
			id:         loginID,
			state:      api.LoginState{Step: "browser", Method: MethodBrowser, URL: authorizationURL, LoginID: loginID},
			targetID:   accountID,
			active:     true,
			verifier:   pkce.Verifier,
			oauthState: pkce.State,
			created:    time.Now(),
			gen:        c.authGen,
		}
		c.browser = b
		state := b.state
		c.mu.Unlock()
		return state
	}
	// Clearing the old password before replacing the pending state means an
	// in-flight unblock/TOTP retry can never reuse a previous password.
	if c.pending != nil {
		c.pending.authPW = ""
	}
	c.pending = &pendingLogin{
		email:    email,
		method:   "email",
		step:     "password",
		targetID: accountID,
	}
	c.mu.Unlock()
	return api.LoginState{Step: "password", Email: email, Method: "email"}
}

func loginMethod(method string) string {
	if method == MethodBrowser {
		return MethodBrowser
	}
	return "email"
}

// invalidEmailRune reports characters that never belong in an email address:
// control characters (Windows IME noise reaches the daemon as NUL) and
// whitespace.
func invalidEmailRune(r rune) bool { return r < 0x20 || r == 0x7f || unicode.IsSpace(r) }

// LoginPassword performs the FxA login and either completes it or moves to
// the 2FA step.
func (c *Controller) LoginPassword(ctx context.Context, password string) api.LoginState {
	c.mu.Lock()
	pending := c.pending
	gen := c.authGen
	c.mu.Unlock()
	if pending == nil || pending.email == "" {
		return api.LoginState{Step: "email", Method: "email", Error: "enter your email first"}
	}
	if password == "" {
		return api.LoginState{Step: "password", Email: pending.email, Method: "email", Error: "password is required"}
	}

	authPW := fxa.AuthPW(pending.email, password)
	res, err := c.fxa.LoginWithAuthPW(ctx, pending.email, authPW)
	if err != nil {
		var apiErr *fxa.APIError
		if errors.As(err, &apiErr) && apiErr.Errno == errnoAccountUnverified &&
			apiErr.VerificationReason == "unblock-code" {
			c.mu.Lock()
			if c.authGen != gen || c.pending != pending {
				c.mu.Unlock()
				return api.LoginState{Step: "done", Email: pending.email, Error: errLoginSessionChanged.Error()}
			}
			pending.authPW = authPW
			pending.verificationMethod = apiErr.VerificationMethod
			pending.verificationReason = apiErr.VerificationReason
			pending.method = "unblock"
			pending.step = "2fa"
			c.mu.Unlock()

			state := api.LoginState{
				Step:               "2fa",
				Email:              pending.email,
				Method:             "unblock",
				VerificationMethod: apiErr.VerificationMethod,
				VerificationReason: apiErr.VerificationReason,
			}
			if sendErr := c.fxa.SendUnblockCode(ctx, pending.email); sendErr != nil {
				c.log.Logf(logging.Warn, "auth", "could not send unblock code for %s: %v", pending.email, sendErr)
				state.Error = "could not send the unblock code: " + friendlyLoginError(sendErr) +
					"; enter a code if you already have one"
			} else {
				c.log.Logf(logging.Info, "auth", "unblock code sent to %s", pending.email)
			}
			return state
		}
		c.mu.Lock()
		if c.pending == pending {
			pending.authPW = ""
		}
		c.mu.Unlock()
		c.log.Logf(logging.Warn, "auth", "login failed for %s: %v", pending.email, err)
		return api.LoginState{Step: "password", Email: pending.email, Method: "email", Error: friendlyLoginError(err)}
	}
	if !res.Verified {
		method := "2fa"
		if res.VerificationMethod == "totp-2fa" {
			method = "totp"
		}
		c.mu.Lock()
		if c.authGen != gen || c.pending != pending {
			c.mu.Unlock()
			return api.LoginState{Step: "done", Email: pending.email, Error: errLoginSessionChanged.Error()}
		}
		pending.sessionToken = res.SessionToken
		pending.verificationMethod = res.VerificationMethod
		pending.verificationReason = res.VerificationReason
		pending.method = method
		pending.step = "2fa"
		c.mu.Unlock()
		c.log.Logf(logging.Info, "auth", "verification required (%s) for %s", res.VerificationMethod, pending.email)
		return api.LoginState{
			Step:               "2fa",
			Email:              pending.email,
			Method:             method,
			VerificationMethod: res.VerificationMethod,
			VerificationReason: res.VerificationReason,
		}
	}
	return c.completeLogin(ctx, pending.email, res.SessionToken, gen)
}

// Login2FA verifies an authenticator code, an unblock code, the emailed
// code, or with an empty code re-checks the email-link flow via
// GET /session/status.
func (c *Controller) Login2FA(ctx context.Context, code string) api.LoginState {
	c.mu.Lock()
	pending := c.pending
	gen := c.authGen
	c.mu.Unlock()
	if pending == nil || pending.email == "" {
		return api.LoginState{Step: "email", Method: "email", Error: "sign-in was interrupted; start again"}
	}
	code = strings.TrimSpace(code)
	method := pending.method
	if method == "" {
		method = "2fa"
	}

	switch {
	case pending.verificationMethod == "totp-2fa":
		if pending.sessionToken == "" {
			return api.LoginState{Step: "email", Method: "email", Error: "sign-in was interrupted; start again"}
		}
		if code == "" {
			return api.LoginState{Step: "2fa", Email: pending.email, Method: "totp", Error: "enter the authenticator code"}
		}
		if err := c.fxa.VerifyTOTP(ctx, pending.sessionToken, code); err != nil {
			return api.LoginState{Step: "2fa", Email: pending.email, Method: "totp", Error: friendlyLoginError(err)}
		}
	case pending.verificationMethod == "email-2fa" && pending.verificationReason == "unblock-code":
		if code == "" {
			return api.LoginState{Step: "2fa", Email: pending.email, Method: "unblock", Error: "enter the unblock code sent to your email"}
		}
		res, err := c.fxa.LoginUnblock(ctx, pending.email, pending.authPW, code)
		if err != nil {
			return api.LoginState{Step: "2fa", Email: pending.email, Method: "unblock", Error: friendlyLoginError(err)}
		}
		if !res.Verified {
			c.mu.Lock()
			if c.authGen != gen || c.pending != pending {
				c.mu.Unlock()
				return api.LoginState{Step: "done", Email: pending.email, Error: errLoginSessionChanged.Error()}
			}
			pending.sessionToken = res.SessionToken
			if res.VerificationMethod != "" {
				pending.verificationMethod = res.VerificationMethod
			}
			if res.VerificationReason != "" {
				pending.verificationReason = res.VerificationReason
			}
			pending.method = "unblock"
			pending.step = "2fa"
			c.mu.Unlock()
			return api.LoginState{
				Step:               "2fa",
				Email:              pending.email,
				Method:             "unblock",
				VerificationMethod: pending.verificationMethod,
				VerificationReason: pending.verificationReason,
			}
		}
		return c.completeLogin(ctx, pending.email, res.SessionToken, gen)

	default:
		if pending.sessionToken == "" {
			return api.LoginState{Step: "email", Method: "email", Error: "sign-in was interrupted; start again"}
		}
		if code == "" {
			state, err := c.fxa.SessionStatus(ctx, pending.sessionToken)
			if err != nil {
				return api.LoginState{Step: "2fa", Email: pending.email, Method: method, Error: friendlyLoginError(err)}
			}
			if state != "verified" {
				return api.LoginState{
					Step:   "2fa",
					Email:  pending.email,
					Method: method,
					Error:  "email link not confirmed yet; click the link in the email, then press Enter",
				}
			}
		} else if err := c.fxa.VerifyCode(ctx, pending.sessionToken, code); err != nil {
			return api.LoginState{Step: "2fa", Email: pending.email, Method: method, Error: friendlyLoginError(err)}
		}
	}
	return c.completeLogin(ctx, pending.email, pending.sessionToken, gen)
}

func (c *Controller) completeLogin(ctx context.Context, email, sessionToken string, gen uint64) api.LoginState {
	c.mu.Lock()
	pending := c.pending
	targetID := ""
	if pending != nil {
		targetID = pending.targetID
	}
	c.mu.Unlock()

	tokens, err := c.fxa.OAuthToken(ctx, sessionToken)
	if err != nil {
		return api.LoginState{Step: "2fa", Email: email, Error: "could not fetch OAuth tokens: " + friendlyLoginError(err)}
	}

	c.mu.Lock()
	if c.authGen != gen || c.pending != pending {
		c.mu.Unlock()
		return api.LoginState{Step: "done", Email: email, Error: errLoginSessionChanged.Error()}
	}
	if targetID != "" {
		target := findAccountLocked(c.accounts, targetID)
		if target == nil {
			c.pending = nil
			c.mu.Unlock()
			return api.LoginState{Step: "error", Email: email, Error: "account not found"}
		}
		if !strings.EqualFold(strings.TrimSpace(target.Email), strings.TrimSpace(email)) {
			c.pending = nil
			c.mu.Unlock()
			return api.LoginState{
				Step:  "error",
				Email: email,
				Error: "the sign-in email does not match the account being re-authenticated",
			}
		}
	}
	if c.accounts == nil {
		c.accounts = &accountsFile{Version: 2}
	}
	existing := emailAccount(c.accounts, email)
	if c.tunnelActiveLocked() && (existing == nil || existing.ID != c.activeID) {
		c.pending = nil
		c.mu.Unlock()
		return api.LoginState{
			Step:  "error",
			Email: email,
			Error: "disconnect first; the tunnel came up while signing in to another account",
		}
	}
	candidate := &Account{
		Email:      email,
		Method:     MethodPassword,
		FxA:        tokens,
		LastUsedAt: time.Now(),
	}
	account, err := c.accounts.upsert(candidate)
	if errors.Is(err, ErrAccountLimit) {
		c.pending = nil
		c.mu.Unlock()
		return api.LoginState{Step: "done", Email: email, Error: "account limit reached (5); remove one first"}
	}
	if err != nil {
		c.pending = nil
		c.mu.Unlock()
		return api.LoginState{Step: "done", Email: email, Error: "signed in, but saving the account failed: " + err.Error()}
	}
	// A successful password sign-in also clears a stale durable re-auth flag
	// and any leftover opaque browser token on the same email. Only login
	// clears it; switching accounts does not.
	account.ReauthRequired = false
	account.Browser = nil
	account.LastError = ""
	c.accounts.LastMethod = MethodPassword
	// Save the account and rollback token copy before publishing the new
	// session fields. c.mu is held throughout, so no client can observe the
	// temporary active ID.
	previousActive := c.activeID
	c.activeID = account.ID
	account.LastUsedAt = time.Now()
	if err := c.saveAccountsLocked(true); err != nil {
		c.activeID = previousActive
		c.accounts.Active = previousActive
		c.mu.Unlock()
		return api.LoginState{Step: "done", Email: email, Error: "signed in, but persisting the account failed: " + err.Error()}
	}
	c.setActiveLocked(account)
	c.pending = nil
	activeEmail := account.Email
	c.mu.Unlock()

	c.log.Logf(logging.Info, "auth", "signed in as %s", activeEmail)
	c.notify()
	return api.LoginState{Step: "done", Email: activeEmail}
}

func badRequestError(message string) error {
	return &api.RemoteError{Code: api.CodeBadRequest, Message: message}
}

// Accounts returns the Account screen snapshot.
func (c *Controller) Accounts() api.AccountsView {
	c.mu.Lock()
	defer c.mu.Unlock()
	var loadErr error
	if c.accounts == nil {
		loadErr = c.ensureAccountsLocked()
	}
	view := summarizeAccounts(c.accounts, c.store.EffectiveConfig())
	switch {
	case c.rotationNotice != "":
		view.Notice = c.rotationNotice
	case c.legacyNotice != "":
		view.Notice = c.legacyNotice
		c.legacyNotice = ""
	}
	if loadErr != nil {
		view.Error = loadErr.Error()
	}
	return view
}

// UseAccount switches the active account. It refuses while a tunnel is up so
// an account never changes under live streams.
func (c *Controller) UseAccount(id string) error {
	id = strings.TrimSpace(id)
	c.mu.Lock()
	if err := c.ensureAccountsLocked(); err != nil {
		c.mu.Unlock()
		return err
	}
	a := findAccountLocked(c.accounts, id)
	if a == nil {
		c.mu.Unlock()
		return badRequestError("account not found")
	}
	if c.tunnelActiveLocked() {
		c.mu.Unlock()
		return badRequestError("disconnect first; switching accounts needs the tunnel off")
	}
	c.setActiveLocked(a)
	err := c.saveAccountsLocked(true)
	c.mu.Unlock()
	if err != nil {
		return err
	}
	c.notify()
	return nil
}

// RemoveAccount removes one account and selects the most recently used
// remaining account when the active one was removed.
func (c *Controller) RemoveAccount(id string) error {
	id = strings.TrimSpace(id)
	c.mu.Lock()
	if err := c.ensureAccountsLocked(); err != nil {
		c.mu.Unlock()
		return err
	}
	a := findAccountLocked(c.accounts, id)
	if a == nil {
		c.mu.Unlock()
		return badRequestError("account not found")
	}
	activeRemoved := a.ID == c.activeID
	if activeRemoved && c.tunnelActiveLocked() {
		c.mu.Unlock()
		return badRequestError("disconnect first; removing the active account needs the tunnel off")
	}
	if len(c.accounts.Accounts) == 1 {
		c.mu.Unlock()
		c.clearSession("account removed")
		return nil
	}
	kept := c.accounts.Accounts[:0]
	for _, existing := range c.accounts.Accounts {
		if existing.ID != a.ID {
			kept = append(kept, existing)
		}
	}
	c.accounts.Accounts = kept
	if activeRemoved {
		c.setActiveLocked(mostRecentlyUsedAccount(c.accounts))
	}
	if c.pending != nil && c.pending.targetID == a.ID {
		c.pending = nil
	}
	err := c.saveAccountsLocked(true)
	c.mu.Unlock()
	if err != nil {
		return err
	}
	c.notify()
	return nil
}

// UpdateAccount renames one account. An empty label resets it to the email.
func (c *Controller) UpdateAccount(id, label string) error {
	id = strings.TrimSpace(id)
	c.mu.Lock()
	if err := c.ensureAccountsLocked(); err != nil {
		c.mu.Unlock()
		return err
	}
	a := findAccountLocked(c.accounts, id)
	if a == nil {
		c.mu.Unlock()
		return badRequestError("account not found")
	}
	normalized, err := normalizeLabel(label, a.Email)
	if err != nil {
		c.mu.Unlock()
		return badRequestError(err.Error())
	}
	a.Label = normalized
	err = c.saveAccountsLocked(false)
	c.mu.Unlock()
	if err != nil {
		return err
	}
	c.notify()
	return nil
}

// SetAccountRotation changes the next-connect rotation settings. A threshold
// of zero keeps the current value; otherwise it must be 1-50.
func (c *Controller) SetAccountRotation(enabled bool, threshold int) error {
	if threshold < 0 || threshold > 50 {
		return badRequestError("rotation_threshold must be between 1 and 50 (or 0 to keep it)")
	}
	value := "off"
	if enabled {
		value = "on"
	}
	if err := c.store.SetOverride("account_rotation", value); err != nil {
		return accountSettingError("account_rotation", err)
	}
	if threshold > 0 {
		if err := c.store.SetOverride("rotation_threshold", strconv.Itoa(threshold)); err != nil {
			return accountSettingError("rotation_threshold", err)
		}
	}
	c.notify()
	return nil
}

func accountSettingError(key string, err error) error {
	if errors.Is(err, settings.ErrUnknownSetting) {
		return &api.RemoteError{
			Code:    api.CodeUnsupportedSetting,
			Message: fmt.Sprintf("setting %q is not runtime-changeable yet", key),
		}
	}
	return &api.RemoteError{Code: api.CodeBadRequest, Message: err.Error()}
}

// pendingStateLocked returns the sign-in step visible in Snapshot. Callers
// hold c.mu.
func (c *Controller) pendingStateLocked() *api.LoginState {
	if c.browser != nil {
		state := c.browser.state
		return &state
	}
	if c.pending == nil || c.pending.email == "" {
		return nil
	}
	step := c.pending.step
	if step == "" {
		step = "password"
	}
	return &api.LoginState{
		Step:               step,
		Email:              c.pending.email,
		Method:             c.pending.method,
		VerificationMethod: c.pending.verificationMethod,
		VerificationReason: c.pending.verificationReason,
	}
}

// Logout best-effort revokes every stored FxA refresh token, then always
// clears local state and returns to UNAUTHENTICATED. A failed or offline
// revoke never blocks local sign-out.
func (c *Controller) Logout() error {
	c.revokeRefreshTokens()
	c.clearSession("user logout")
	return nil
}

// revokeRefreshTokens calls the FxA destroy endpoint for every distinct
// refresh token in the account store (and the active token set) under one
// five-second budget. Failures are logged and ignored; local sign-out follows
// regardless.
func (c *Controller) revokeRefreshTokens() {
	c.mu.Lock()
	var refreshTokens []string
	seen := make(map[string]struct{})
	add := func(t *fxa.Tokens) {
		if t == nil {
			return
		}
		token := strings.TrimSpace(t.RefreshToken)
		if token == "" {
			return
		}
		if _, ok := seen[token]; ok {
			return
		}
		seen[token] = struct{}{}
		refreshTokens = append(refreshTokens, token)
	}
	if c.accounts != nil {
		for _, a := range c.accounts.Accounts {
			if a != nil {
				add(a.FxA)
			}
		}
	}
	add(c.tokens)
	c.mu.Unlock()
	if len(refreshTokens) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, token := range refreshTokens {
		if err := c.fxa.Destroy(ctx, token); err != nil {
			c.log.Logf(logging.Warn, "auth", "could not revoke a Firefox account refresh token: %v", err)
		}
	}
	c.log.Logf(logging.Info, "auth", "attempted to revoke %d Firefox account refresh token(s)", len(refreshTokens))
}

func (c *Controller) clearSession(reason string) {
	c.clearSessionIf(0, false, reason)
}

// clearSessionIf resets the controller to UNAUTHENTICATED. When requireMatch
// is set it only clears if the auth generation still equals gen, so a stale
// in-flight refresh cannot sign out a newer session.
func (c *Controller) clearSessionIf(gen uint64, requireMatch bool, reason string) bool {
	c.mu.Lock()
	if requireMatch && c.authGen != gen {
		c.mu.Unlock()
		return false
	}
	// A browser flow must not outlive the session: cancel it before the
	// account swap and tunnel teardown. Its Wait result is discarded by the
	// generation fence below even if it completed concurrently.
	c.cancelBrowserLocked()
	// Bump the generation before clearing the token file below: an in-flight
	// refresh that captured the old generation must neither publish nor save
	// after sign-out, whichever side wins the race.
	c.authGen++
	clearedGen := c.authGen
	c.tokens = nil
	c.pending = nil
	c.email = ""
	c.accounts = nil
	c.activeID = ""
	c.rotationNotice = ""
	c.legacyNotice = ""
	c.state = StateUnauthenticated
	c.since = time.Now()
	// Signing out wipes the account and usage view: an unauthenticated
	// snapshot must not expose the previous session's quota, exit, totals,
	// rates or pass (v0.4.0 spec). Clear the full set before the tunnel
	// teardown so a slow teardown cannot leave account data visible.
	c.quota = nil
	c.exitIP = ""
	c.exitCountry = ""
	c.totalsUp = 0
	c.totalsDown = 0
	c.ratesUp = 0
	c.ratesDown = 0
	c.pass = nil
	c.mu.Unlock()

	c.stopTunnel("session cleared")

	var clearErr, accountsErr, stateErr error
	cleared := false
	c.mu.Lock()
	// A new sign-in may have completed while the tunnel was tearing down.
	// Keep its files and persisted email; the old generation must not delete
	// or overwrite the newer session's credentials.
	if c.authGen == clearedGen {
		cleared = true
		clearErr = clearTokens(c.store.Paths().Tokens)
		if err := os.Remove(accountsPath(c.store.Paths())); err != nil && !os.IsNotExist(err) {
			accountsErr = err
		}
		stateErr = c.store.UpdateState(func(s *settings.State) { s.Email = "" })
	}
	c.mu.Unlock()

	if clearErr != nil {
		c.log.Logf(logging.Warn, "auth", "could not remove tokens: %v", clearErr)
	}
	if accountsErr != nil {
		c.log.Logf(logging.Warn, "auth", "could not remove accounts: %v", accountsErr)
	}
	if stateErr != nil {
		c.log.Logf(logging.Warn, "auth", "could not clear stored email: %v", stateErr)
	}
	if cleared {
		c.log.Logf(logging.Info, "auth", "session cleared (%s)", reason)
	} else {
		c.log.Logf(logging.Info, "auth", "session cleared (%s); a newer sign-in kept its credentials", reason)
	}
	c.notify()
	return true
}

// dropActiveIf removes only the active account after a permanent token
// failure and selects the most recently used remaining account. It runs only
// without a live tunnel: an active tunnel is torn down first, so streams never
// switch accounts underneath. When no account remains the whole session is
// cleared. The generation fence prevents an in-flight refresh from dropping a
// newer login.
func (c *Controller) dropActiveIf(gen uint64, requireMatch bool, reason string) bool {
	c.mu.Lock()
	if requireMatch && c.authGen != gen {
		c.mu.Unlock()
		return false
	}
	if c.accounts == nil || len(c.accounts.Accounts) == 0 || c.activeAccountLocked() == nil {
		c.mu.Unlock()
		return c.clearSessionIf(gen, requireMatch, reason)
	}
	if c.tunnelActiveLocked() {
		c.mu.Unlock()
		c.stopTunnel("account removed")
		c.mu.Lock()
		if requireMatch && c.authGen != gen {
			c.mu.Unlock()
			return false
		}
	}
	if c.accounts == nil || len(c.accounts.Accounts) <= 1 || c.activeAccountLocked() == nil {
		c.mu.Unlock()
		return c.clearSessionIf(gen, requireMatch, reason)
	}
	active := c.activeAccountLocked()
	kept := c.accounts.Accounts[:0]
	for _, a := range c.accounts.Accounts {
		if a.ID != active.ID {
			kept = append(kept, a)
		}
	}
	c.accounts.Accounts = kept
	c.setActiveLocked(mostRecentlyUsedAccount(c.accounts))
	if c.pending != nil && c.pending.targetID == active.ID {
		c.pending = nil
	}
	err := c.saveAccountsLocked(true)
	c.mu.Unlock()
	if err != nil {
		c.log.Logf(logging.Warn, "auth", "could not persist account removal: %v", err)
	}
	c.log.Logf(logging.Warn, "auth", "account removed: %s", reason)
	c.notify()
	return true
}

// Snapshot returns the current status for TUI/CLI clients.
func (c *Controller) Snapshot() api.Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	persisted := c.store.State()
	// An unauthenticated snapshot must not expose the previous session's
	// account or usage data, even during a short teardown window in which a
	// live session object is still attached.
	accountVisible := c.state != StateUnauthenticated
	email := c.email
	if !accountVisible {
		email = ""
	}
	st := api.Status{
		State:         string(c.state),
		Authenticated: c.tokens != nil && accountVisible,
		Email:         email,
		Since:         c.since.Unix(),
		KillSwitch:    c.store.EffectiveKillSwitch(),
		Autoconnect:   c.store.EffectiveAutoconnect(),
		Location:      persisted.SelectedLocation,
		Server:        persisted.SelectedServer,
		ProxyOnly:     c.state == StateProxyOnly || (persisted.ProxyOnly != nil && *persisted.ProxyOnly),
	}
	if accountVisible && c.tokens != nil {
		st.TokenExpires = c.tokens.ExpiresAt
	}
	if accountVisible && c.pass != nil {
		st.PassExpires = c.pass.ExpiresAt
	}
	if accountVisible && c.quota != nil {
		st.Quota = c.quota
	}
	if accountVisible && c.session != nil {
		st.ExitIP, st.ExitCountry = c.exitIP, c.exitCountry
		up, down := c.session.Totals()
		if up == 0 && down == 0 {
			up, down = c.totalsUp, c.totalsDown
		}
		st.Totals = &api.ByteCounters{Up: up, Down: down}
		st.Rates = &api.ByteRates{Up: c.ratesUp, Down: c.ratesDown}
	} else if accountVisible && (c.totalsUp > 0 || c.totalsDown > 0) {
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
	view := summarizeAccounts(c.accounts, c.store.EffectiveConfig())
	st.Accounts = view.Accounts
	if active := c.activeAccountLocked(); active != nil {
		st.ActiveAccount = active.ID
	}
	st.Login = c.pendingStateLocked()
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
		TLSSplit:             cfg.TLSSplit,
		HTTPProxy:            cfg.HTTPProxy,
		HTTPProxyPort:        cfg.HTTPProxyPort,
		SolveChallenges:      cfg.SolveChallenges,
		KeepaliveProfile:     cfg.KeepaliveProfile,
		AccountRotation:      cfg.AccountRotation,
		RotationThreshold:    cfg.RotationThreshold,
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
	// Snapshot the prior kill_switch override before it is mutated, so a failed
	// live re-apply can be rolled back instead of leaving the persisted setting
	// describing a policy the running tunnel never adopted.
	var prevKillSwitch *bool
	if st := c.store.State(); st.KillSwitch != nil {
		v := *st.KillSwitch
		prevKillSwitch = &v
	}
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
		if rerr := c.ReapplyNetcfg(context.Background()); rerr != nil {
			c.revertKillSwitchOverride(prevKillSwitch)
			c.notify()
			return &api.RemoteError{
				Code:    api.CodeInternal,
				Message: fmt.Sprintf("kill_switch not applied: %v", rerr),
			}
		}
	case "log_level", "redact_targets":
		c.applyLoggingSettings()
	}
	c.notify()
	return nil
}

// revertKillSwitchOverride restores the kill_switch override captured before a
// failed live re-apply. It is best effort: a revert failure is logged, not
// returned, because the caller is already reporting the apply failure.
func (c *Controller) revertKillSwitchOverride(prev *bool) {
	var err error
	if prev == nil {
		err = c.store.ClearOverride("kill_switch")
	} else if *prev {
		err = c.store.SetOverride("kill_switch", "on")
	} else {
		err = c.store.SetOverride("kill_switch", "off")
	}
	if err != nil {
		c.log.Logf(logging.Warn, "settings", "revert kill_switch override: %v", err)
	}
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
	c.mu.Lock()
	active := c.activeAccountLocked()
	tokens := c.tokens
	activeID := ""
	info := api.AccountInfo{Email: c.email}
	if active != nil {
		activeID = active.ID
		info.ID = active.ID
		info.Label = active.Label
		info.Method = active.Method
		info.ReauthRequired = active.ReauthRequired
		info.Email = active.Email
	}
	c.mu.Unlock()
	if tokens == nil {
		info.Error = "not signed in"
		return info
	}
	info.TokenExpires = tokens.ExpiresAt

	ent, err := c.guardian.Account(ctx, tokens.AccessToken)
	if err != nil && guardian.IsUnauthorized(err) {
		// Every method now uses the same FxA refresh path. A permanent
		// browser rejection sets the durable re-auth flag inside the refresh.
		if rerr := c.refreshAccessToken(ctx); rerr == nil {
			if t := c.currentTokens(); t != nil {
				info.TokenExpires = t.ExpiresAt
				ent, err = c.guardian.Account(ctx, t.AccessToken)
			}
		}
	}
	if err != nil {
		if activeID != "" {
			c.mu.Lock()
			if a := findAccountLocked(c.accounts, activeID); a != nil {
				info.ReauthRequired = a.ReauthRequired
				info.Label = a.Label
				info.Method = a.Method
			}
			c.mu.Unlock()
		}
		info.Error = err.Error()
		return info
	}
	info.UID = ent.UID
	info.Subscribed = ent.Subscribed
	info.MaxBytes = ent.MaxBytes
	info.LimitedBandwidth = ent.LimitedBandwidth
	info.QuotaRemaining = ent.QuotaRemaining
	if activeID != "" {
		c.mu.Lock()
		if a := findAccountLocked(c.accounts, activeID); a != nil {
			if ent.MaxBytes != nil || ent.QuotaRemaining != nil {
				a.Quota = &api.Quota{Limit: ent.MaxBytes, Remaining: ent.QuotaRemaining}
			}
			info.ReauthRequired = a.ReauthRequired
		}
		c.mu.Unlock()
	}
	return info
}

// challengeFailureMessage renders the user-facing wording for a failed
// anti-bot challenge. The two cases are deliberately different: with
// solve_challenges off the challenge never got a solver (fastly.ErrDisabled
// joined by the FxA/Guardian clients), while an enabled solve that failed
// suggests retrying or turning the setting off.
func challengeFailureMessage(err error) string {
	if errors.Is(err, fastly.ErrDisabled) {
		return "anti-bot challenge solving is disabled; enable solve_challenges and try again"
	}
	return "could not solve the anti-bot challenge; retry or disable solve_challenges"
}

func friendlyLoginError(err error) string {
	var apiErr *fxa.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Message
	}
	var chErr *fxa.ChallengeError
	if errors.As(err, &chErr) {
		return challengeFailureMessage(err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "the request timed out; check the host clock and network"
	}
	if errors.Is(err, context.Canceled) {
		return "the request was cancelled"
	}
	return err.Error()
}
