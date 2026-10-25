package engine

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/logging"
)

// Stable browser sign-in messages. None of them contains a code, verifier,
// redirect URL or token; clients may show them and logs may repeat them.
const (
	browserTimeoutMessage        = "browser sign-in timed out; start again"
	browserRedirectRequired      = "paste the complete redirect URL from the browser"
	browserRedirectMismatch      = "the redirect URL does not belong to this sign-in; paste the URL from this sign-in"
	browserRedirectError         = "the browser sign-in was not completed; paste the redirect URL from a successful sign-in"
	browserRedirectMissingCode   = "the redirect URL has no authorization code; paste the complete URL from the browser"
	browserExchangeRetryMessage  = "the browser token exchange failed; check the connection and try again"
	browserExchangeRejected      = "the browser authorization code was rejected or already used; start again"
	browserExchangeUnusable      = "the browser token exchange returned an unusable response; start again"
	browserProfileRetryMessage   = "the browser profile lookup failed; try again"
	browserProfileRejected       = "the browser sign-in was rejected; start again"
	browserCompletingMessage     = "the browser sign-in is already being completed; try again"
	browserAccountChangedMessage = "the account changed while signing in; start again"
	browserFlowMissingMessage    = "sign-in flow not found"
	browserNoEmailMessage        = "browser sign-in returned no account email; start again"
	browserAccountRemovedMessage = "the account being re-authenticated was removed; start again"
	browserMismatchMessage       = "the browser sign-in returned a different account; sign in again with the selected account"
	browserTunnelGuardMessage    = "disconnect first; the tunnel came up while signing in to another account"
)

// browserLogin is one in-flight daemon-owned browser sign-in. All fields are
// guarded by Controller.mu. There is no listener, goroutine or browser
// dependency: clients hand the full URN redirect URL back to LoginComplete.
type browserLogin struct {
	id         string
	state      api.LoginState
	targetID   string
	active     bool
	verifier   string
	oauthState string
	created    time.Time
	tokens     *fxa.Tokens
	profile    *fxa.Profile
	profileErr error
	// completing serializes LoginComplete calls for one flow so two clients
	// cannot exchange the same code twice.
	completing bool
	// gen fences a completion that started under one account session from
	// publishing into the next (account switches bump authGen).
	gen uint64
}

// LoginStatus returns the active or most recent sign-in state. An empty id
// addresses the password pending state; the browser state wins while one
// exists. A browser flow older than browserTimeout becomes a terminal error.
func (c *Controller) LoginStatus(id string) (api.LoginState, error) {
	id = strings.TrimSpace(id)
	c.mu.Lock()
	if b := c.browser; b != nil && (id == "" || b.id == id) {
		expired := c.expireBrowserLocked(b)
		state := b.state
		c.mu.Unlock()
		if expired {
			c.log.Logf(logging.Warn, "auth", "browser sign-in failed: %s", browserTimeoutMessage)
			c.notify()
		}
		return state, nil
	}
	if id == "" && c.pending != nil {
		if state := c.pendingStateLocked(); state != nil {
			c.mu.Unlock()
			return *state, nil
		}
	}
	c.mu.Unlock()
	return api.LoginState{}, badRequestError(browserFlowMissingMessage)
}

// LoginComplete finishes a browser sign-in from the complete URN redirect URL.
// A nil return means the flow either completed or reached a terminal state;
// LoginStatus reports the terminal done/error state. Retryable bad input (a
// malformed URL, wrong state, or a transient exchange/profile failure) returns
// an error and leaves the flow active for another paste.
func (c *Controller) LoginComplete(id, redirectURL string) error {
	id = strings.TrimSpace(id)
	c.mu.Lock()
	b := c.browser
	if b == nil || (id != "" && b.id != id) {
		c.mu.Unlock()
		return badRequestError(browserFlowMissingMessage)
	}
	if !b.active {
		terminal := ""
		if b.state.Step == "error" {
			terminal = b.state.Error
		}
		c.mu.Unlock()
		if terminal != "" {
			return badRequestError(terminal)
		}
		return badRequestError(browserFlowMissingMessage)
	}
	if c.expireBrowserLocked(b) {
		c.mu.Unlock()
		c.log.Logf(logging.Warn, "auth", "browser sign-in failed: %s", browserTimeoutMessage)
		c.notify()
		return nil
	}
	if b.completing {
		c.mu.Unlock()
		return badRequestError(browserCompletingMessage)
	}
	b.completing = true
	verifier, oauthState, gen := b.verifier, b.oauthState, b.gen
	tokens := b.tokens
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		b.completing = false
		c.mu.Unlock()
	}()

	code, state, err := parseBrowserRedirect(redirectURL)
	if err != nil {
		return badRequestError(err.Error())
	}
	if state != oauthState {
		return badRequestError(browserRedirectMismatch)
	}

	if tokens == nil {
		exchanged, exchangeErr := c.fxa.ExchangeCode(context.Background(), code, verifier, fxa.OAuthRedirectURI)
		c.mu.Lock()
		if c.browser != b || !b.active {
			c.mu.Unlock()
			return badRequestError(browserFlowMissingMessage)
		}
		if exchangeErr != nil {
			// A concurrent completion may already have exchanged the same
			// code; its token set wins.
			if b.tokens == nil {
				if browserExchangeRetryable(exchangeErr) {
					c.mu.Unlock()
					return badRequestError(browserExchangeRetryMessage)
				}
				message := c.failBrowserLocked(b, "error", browserExchangeFailureMessage(exchangeErr))
				c.mu.Unlock()
				c.log.Logf(logging.Warn, "auth", "browser sign-in failed: %s", message)
				c.notify()
				return nil
			}
		} else if b.tokens == nil {
			b.tokens = exchanged
		}
		tokens = b.tokens
		c.mu.Unlock()
	}

	c.mu.Lock()
	if c.browser != b || !b.active {
		c.mu.Unlock()
		return badRequestError(browserFlowMissingMessage)
	}
	tokens = b.tokens
	profile := b.profile
	c.mu.Unlock()
	if tokens == nil {
		// A retryable exchange error above leaves the flow active without
		// tokens; reaching here would be an internal state bug.
		return badRequestError(browserExchangeRetryMessage)
	}

	if profile == nil {
		fetched, profileErr := c.fxa.Profile(context.Background(), tokens.AccessToken)
		c.mu.Lock()
		if c.browser != b || !b.active {
			c.mu.Unlock()
			return badRequestError(browserFlowMissingMessage)
		}
		if profileErr != nil {
			if b.profile == nil {
				if browserProfileRetryable(profileErr) {
					b.profileErr = profileErr
					c.mu.Unlock()
					return badRequestError(browserProfileRetryMessage)
				}
				b.profileErr = profileErr
				message := c.failBrowserLocked(b, "error", browserProfileRejected)
				c.mu.Unlock()
				c.log.Logf(logging.Warn, "auth", "browser sign-in failed: %s", message)
				c.notify()
				return nil
			}
		} else {
			b.profile = fetched
			b.profileErr = nil
		}
		profile = b.profile
		c.mu.Unlock()
	}
	if profile == nil {
		return badRequestError(browserProfileRetryMessage)
	}
	return c.finishBrowserSuccess(b, gen, tokens, profile)
}

// finishBrowserSuccess saves the account after a successful exchange and
// profile lookup, or publishes a terminal error state for a permanent failure.
func (c *Controller) finishBrowserSuccess(b *browserLogin, gen uint64, tokens *fxa.Tokens, profile *fxa.Profile) error {
	email := strings.TrimSpace(profile.Email)

	c.mu.Lock()
	if c.browser != b || !b.active {
		c.mu.Unlock()
		return badRequestError(browserFlowMissingMessage)
	}
	if c.authGen != gen {
		message := c.failBrowserLocked(b, "error", browserAccountChangedMessage)
		c.mu.Unlock()
		c.log.Logf(logging.Warn, "auth", "browser sign-in failed: %s", message)
		c.notify()
		return nil
	}
	if email == "" {
		message := c.failBrowserLocked(b, "error", browserNoEmailMessage)
		c.mu.Unlock()
		c.log.Logf(logging.Warn, "auth", "browser sign-in failed: %s", message)
		c.notify()
		return nil
	}
	if b.targetID != "" {
		target := findAccountLocked(c.accounts, b.targetID)
		if target == nil {
			message := c.failBrowserLocked(b, "error", browserAccountRemovedMessage)
			c.mu.Unlock()
			c.log.Logf(logging.Warn, "auth", "browser sign-in failed: %s", message)
			c.notify()
			return nil
		}
		if !strings.EqualFold(strings.TrimSpace(target.Email), email) {
			c.failBrowserLocked(b, "error", browserMismatchMessage)
			c.mu.Unlock()
			c.log.Logf(logging.Warn, "auth", "browser re-authentication returned a different account")
			c.notify()
			return nil
		}
	}
	if c.accounts == nil {
		c.accounts = &accountsFile{Version: 2}
	}
	existing := emailAccount(c.accounts, email)
	if c.tunnelActiveLocked() {
		allowed := false
		if b.targetID != "" {
			allowed = b.targetID == c.activeID
		} else if existing != nil {
			allowed = existing.ID == c.activeID
		}
		if !allowed {
			c.failBrowserLocked(b, "error", browserTunnelGuardMessage)
			c.mu.Unlock()
			c.log.Logf(logging.Warn, "auth", "browser sign-in finished while connected to another account")
			c.notify()
			return nil
		}
	}

	candidate := &Account{
		Email:      email,
		Method:     MethodBrowser,
		FxA:        tokens,
		LastUsedAt: time.Now(),
	}
	account, err := c.accounts.upsert(candidate)
	if errors.Is(err, ErrAccountLimit) {
		c.failBrowserLocked(b, "done", "account limit reached (5); remove one first")
		b.state.Email = email
		c.mu.Unlock()
		c.log.Logf(logging.Warn, "auth", "browser sign-in rejected: account limit reached")
		c.notify()
		return nil
	}
	if err != nil {
		message := "browser sign-in could not save the account: " + err.Error()
		c.failBrowserLocked(b, "error", message)
		c.mu.Unlock()
		c.log.Logf(logging.Warn, "auth", "browser sign-in account error: %v", err)
		c.notify()
		return nil
	}
	// A successful browser sign-in clears the durable re-auth flag and the
	// decode-only legacy token field.
	account.ReauthRequired = false
	account.Browser = nil
	account.LastError = ""
	previousMethod := c.accounts.LastMethod
	c.accounts.LastMethod = MethodBrowser

	// Save accounts.json and the compatibility tokens.json before publishing
	// the new active session. c.mu keeps the temporary active id invisible.
	previousActive := c.activeID
	c.activeID = account.ID
	account.LastUsedAt = time.Now()
	if err := c.saveAccountsLocked(true); err != nil {
		c.activeID = previousActive
		c.accounts.Active = previousActive
		c.accounts.LastMethod = previousMethod
		message := "browser sign-in succeeded, but persisting the account failed: " + err.Error()
		c.failBrowserLocked(b, "error", message)
		c.mu.Unlock()
		c.log.Logf(logging.Warn, "auth", "browser sign-in persistence failed: %v", err)
		c.notify()
		return nil
	}
	c.setActiveLocked(account)
	b.active = false
	b.tokens = nil
	b.profile = nil
	b.profileErr = nil
	b.verifier = ""
	b.oauthState = ""
	b.state = api.LoginState{Step: "done", Method: MethodBrowser, Email: account.Email, LoginID: b.id}
	activeEmail := account.Email
	c.mu.Unlock()

	c.log.Logf(logging.Info, "auth", "signed in as %s (browser)", activeEmail)
	c.notify()
	return nil
}

// failBrowserLocked marks a flow terminal and clears its sign-in material.
// Callers hold c.mu. The returned message is safe to log and show.
func (c *Controller) failBrowserLocked(b *browserLogin, step, message string) string {
	b.active = false
	b.tokens = nil
	b.profile = nil
	b.profileErr = nil
	b.verifier = ""
	b.oauthState = ""
	b.state = api.LoginState{Step: step, Method: MethodBrowser, LoginID: b.id, Error: message}
	return message
}

// expireBrowserLocked marks an over-age active flow terminal. Callers hold
// c.mu and must notify after unlocking when it reports true.
func (c *Controller) expireBrowserLocked(b *browserLogin) bool {
	if b == nil || !b.active || c.browserTimeout <= 0 || b.created.IsZero() ||
		time.Since(b.created) <= c.browserTimeout {
		return false
	}
	c.failBrowserLocked(b, "error", browserTimeoutMessage)
	return true
}

func parseBrowserRedirect(raw string) (code, state string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", errors.New(browserRedirectRequired)
	}
	u, parseErr := url.Parse(raw)
	if parseErr != nil || !strings.EqualFold(u.Scheme, "urn") ||
		u.Opaque != strings.TrimPrefix(fxa.OAuthRedirectURI, "urn:") {
		return "", "", errors.New(browserRedirectRequired)
	}
	q := u.Query()
	if q.Has("error") {
		return "", "", errors.New(browserRedirectError)
	}
	code = strings.TrimSpace(q.Get("code"))
	state = strings.TrimSpace(q.Get("state"))
	if code == "" {
		return "", "", errors.New(browserRedirectMissingCode)
	}
	return code, state, nil
}

// browserExchangeRetryable reports whether a token exchange may be retried
// with the same code. Network errors, anti-bot challenges, 408/429 and 5xx are
// transient. A 4xx OAuth rejection (invalid_grant, expired/used code, wrong
// client) and a 2xx response that cannot be parsed are terminal: the latter
// may already have consumed the one-shot code.
func browserExchangeRetryable(err error) bool {
	if err == nil {
		return false
	}
	var responseErr *fxa.ResponseError
	if errors.As(err, &responseErr) {
		return false
	}
	var apiErr *fxa.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusNotAcceptable:
			return true
		}
		return apiErr.StatusCode >= 500
	}
	return true
}

// browserExchangeFailureMessage picks the terminal message for a permanent
// exchange failure without exposing the underlying error text.
func browserExchangeFailureMessage(err error) string {
	var responseErr *fxa.ResponseError
	if errors.As(err, &responseErr) {
		return browserExchangeUnusable
	}
	return browserExchangeRejected
}

// browserProfileRetryable reports whether the profile call may be retried with
// the tokens already in hand. Only a 401 means the access token is dead;
// network errors, anti-bot challenges, other 4xx responses, unusable responses
// and 5xx all keep the exchanged tokens and may be retried.
func browserProfileRetryable(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *fxa.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized {
		return false
	}
	return true
}

// friendlyBrowserError turns an internal start failure into a stable message
// without echoing raw error text.
func friendlyBrowserError(err error) string {
	switch {
	case err == nil:
		return "browser sign-in failed; try again"
	case errors.Is(err, context.DeadlineExceeded):
		return browserTimeoutMessage
	case errors.Is(err, context.Canceled):
		return "browser sign-in was cancelled"
	default:
		return "browser sign-in failed; try again"
	}
}

// LoginCancel cancels a browser flow by login id, or the password pending
// state when id is empty. Cancelling an already-terminal browser state is a
// no-op so clients can safely repeat a cancel.
func (c *Controller) LoginCancel(id string) error {
	id = strings.TrimSpace(id)
	c.mu.Lock()
	if id == "" {
		// An empty id is the clients' generic cancel: it clears the password
		// pending state and the one active browser flow, if any.
		if b := c.browser; b != nil && b.active {
			b.active = false
			b.tokens = nil
			b.profile = nil
			b.profileErr = nil
			b.verifier = ""
			b.oauthState = ""
			b.state = api.LoginState{Step: "cancelled", Method: MethodBrowser, LoginID: b.id}
		}
		if c.pending != nil {
			c.pending.authPW = ""
			c.pending = nil
		}
		c.mu.Unlock()
		c.notify()
		return nil
	}
	b := c.browser
	if b == nil || b.id != id {
		c.mu.Unlock()
		return badRequestError(browserFlowMissingMessage)
	}
	if !b.active {
		c.mu.Unlock()
		return nil
	}
	b.active = false
	b.tokens = nil
	b.profile = nil
	b.profileErr = nil
	b.verifier = ""
	b.oauthState = ""
	b.state = api.LoginState{Step: "cancelled", Method: MethodBrowser, LoginID: b.id}
	c.mu.Unlock()
	c.notify()
	return nil
}

// cancelBrowserLocked cancels and forgets the current browser flow. Callers
// hold c.mu.
func (c *Controller) cancelBrowserLocked() {
	b := c.browser
	if b == nil {
		return
	}
	if b.active {
		b.active = false
		b.state = api.LoginState{Step: "cancelled", Method: MethodBrowser, LoginID: b.id}
	}
	b.tokens = nil
	b.profile = nil
	b.profileErr = nil
	b.verifier = ""
	b.oauthState = ""
	c.browser = nil
}

// cancelBrowser cancels and forgets any browser flow.
func (c *Controller) cancelBrowser() {
	c.mu.Lock()
	c.cancelBrowserLocked()
	c.mu.Unlock()
}
