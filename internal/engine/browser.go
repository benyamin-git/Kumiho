package engine

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/webauth"
)

// browserLogin is one in-flight daemon-owned browser sign-in. All fields are
// guarded by Controller.mu; only session methods are safe to call without it.
type browserLogin struct {
	id       string
	session  *webauth.Session
	state    api.LoginState
	targetID string
	active   bool
	cancel   context.CancelFunc
	done     chan struct{}
	// ctx is cancelled by LoginCancel, Logout and Shutdown so Wait stops even
	// while a verify HTTP request is in flight.
	ctx context.Context
}

// LoginStatus returns the active or most recent sign-in state. An empty id
// addresses the password pending state; the browser state wins while one
// exists.
func (c *Controller) LoginStatus(id string) (api.LoginState, error) {
	id = strings.TrimSpace(id)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.browser != nil && (id == "" || c.browser.id == id) {
		return c.browser.state, nil
	}
	if id == "" && c.pending != nil {
		if state := c.pendingStateLocked(); state != nil {
			return *state, nil
		}
	}
	return api.LoginState{}, badRequestError("sign-in flow not found")
}

// LoginCancel cancels a browser flow by login id, or the password pending
// state when id is empty. Cancelling an already-terminal browser state is a
// no-op so clients can safely repeat a cancel.
func (c *Controller) LoginCancel(id string) error {
	id = strings.TrimSpace(id)
	c.mu.Lock()
	if id == "" {
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
		return badRequestError("sign-in flow not found")
	}
	if !b.active {
		c.mu.Unlock()
		return nil
	}
	b.active = false
	b.state = api.LoginState{Step: "cancelled", Method: MethodBrowser, LoginID: b.id}
	if b.cancel != nil {
		b.cancel()
	}
	b.session.Cancel()
	c.mu.Unlock()
	c.notify()
	return nil
}

// cancelBrowserLocked cancels and forgets the current browser flow. Callers
// hold c.mu. A late Wait result sees c.browser changed and is discarded.
func (c *Controller) cancelBrowserLocked() {
	b := c.browser
	if b == nil {
		return
	}
	if b.active {
		b.active = false
		b.state = api.LoginState{Step: "cancelled", Method: MethodBrowser, LoginID: b.id}
		if b.cancel != nil {
			b.cancel()
		}
		b.session.Cancel()
	}
	c.browser = nil
}

// cancelBrowser cancels and forgets any browser flow.
func (c *Controller) cancelBrowser() {
	c.mu.Lock()
	c.cancelBrowserLocked()
	c.mu.Unlock()
}

func friendlyBrowserError(err error) string {
	if err == nil {
		return "browser sign-in failed"
	}
	message := strings.TrimSpace(err.Error())
	lower := strings.ToLower(message)
	switch {
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(lower, "timed out"):
		return "browser sign-in timed out; try again or use the password flow"
	case errors.Is(err, context.Canceled) || strings.Contains(lower, "cancelled"):
		return "browser sign-in was cancelled"
	default:
		return "browser sign-in failed: " + strings.TrimPrefix(message, "webauth: ")
	}
}

func (c *Controller) runBrowserLogin(b *browserLogin, gen uint64) {
	defer close(b.done)
	defer b.cancel()
	result, err := b.session.Wait(b.ctx)
	if err != nil {
		c.finishBrowserError(b, err)
		return
	}
	c.finishBrowserSuccess(b, gen, result)
}

func (c *Controller) finishBrowserError(b *browserLogin, err error) {
	c.mu.Lock()
	if c.browser != b || !b.active {
		c.mu.Unlock()
		return
	}
	b.active = false
	b.state = api.LoginState{Step: "error", Method: MethodBrowser, Error: friendlyBrowserError(err)}
	c.mu.Unlock()
	c.log.Logf(logging.Warn, "auth", "browser sign-in failed: %s", friendlyBrowserError(err))
	c.notify()
}

func (c *Controller) finishBrowserSuccess(b *browserLogin, gen uint64, result *webauth.Result) {
	if result == nil {
		c.finishBrowserError(b, errors.New("browser sign-in returned no result"))
		return
	}
	email := strings.TrimSpace(result.Email)

	c.mu.Lock()
	if c.browser != b || !b.active || c.authGen != gen {
		c.mu.Unlock()
		return
	}
	if email == "" {
		b.active = false
		b.state = api.LoginState{
			Step:   "error",
			Method: MethodBrowser,
			Error:  "browser sign-in returned no account email; try again",
		}
		c.mu.Unlock()
		c.log.Logf(logging.Warn, "auth", "browser sign-in returned no account email")
		c.notify()
		return
	}
	if b.targetID != "" {
		target := findAccountLocked(c.accounts, b.targetID)
		if target == nil || !strings.EqualFold(strings.TrimSpace(target.Email), email) {
			b.active = false
			b.state = api.LoginState{
				Step:   "error",
				Method: MethodBrowser,
				Error:  "the browser sign-in returned a different account; sign in again with the selected account",
			}
			c.mu.Unlock()
			c.log.Logf(logging.Warn, "auth", "browser re-authentication returned a different account")
			c.notify()
			return
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
			b.active = false
			b.state = api.LoginState{
				Step:   "error",
				Method: MethodBrowser,
				Error:  "disconnect first; the tunnel came up while signing in to another account",
			}
			c.mu.Unlock()
			c.log.Logf(logging.Warn, "auth", "browser sign-in finished while connected to another account")
			c.notify()
			return
		}
	}

	candidate := &Account{
		Email:      email,
		Method:     MethodBrowser,
		Browser:    &BrowserToken{Token: result.Token, ExpiresAt: result.ExpiresAt},
		LastUsedAt: time.Now(),
	}
	account, err := c.accounts.upsert(candidate)
	if errors.Is(err, ErrAccountLimit) {
		b.active = false
		b.state = api.LoginState{
			Step:   "done",
			Method: MethodBrowser,
			Email:  email,
			Error:  "account limit reached (5); remove one first",
		}
		c.mu.Unlock()
		c.log.Logf(logging.Warn, "auth", "browser sign-in rejected: account limit reached")
		c.notify()
		return
	}
	if err != nil {
		b.active = false
		b.state = api.LoginState{
			Step:   "error",
			Method: MethodBrowser,
			Error:  "browser sign-in could not save the account: " + err.Error(),
		}
		c.mu.Unlock()
		c.log.Logf(logging.Warn, "auth", "browser sign-in account error: %v", err)
		c.notify()
		return
	}
	// Transitional old flow: preserve any FxA fallback tokens already on the
	// account, and clear both the legacy and the durable re-auth flag only
	// after a successful sign-in. Task 3 replaces this flow with FxA tokens.
	if account.Browser == nil {
		account.Browser = &BrowserToken{}
	}
	account.Browser.ReauthRequired = false
	account.ReauthRequired = false
	account.LastError = ""
	c.accounts.LastMethod = MethodBrowser

	// Save accounts.json and the compatibility tokens.json before publishing
	// the new active session. c.mu keeps the temporary active id invisible.
	previousActive := c.activeID
	c.activeID = account.ID
	account.LastUsedAt = time.Now()
	if err := c.saveAccountsLocked(true); err != nil {
		c.activeID = previousActive
		c.accounts.Active = previousActive
		b.active = false
		b.state = api.LoginState{
			Step:   "error",
			Method: MethodBrowser,
			Error:  "browser sign-in succeeded, but persisting the account failed: " + err.Error(),
		}
		c.mu.Unlock()
		c.log.Logf(logging.Warn, "auth", "browser sign-in persistence failed: %v", err)
		c.notify()
		return
	}
	c.setActiveLocked(account)
	b.active = false
	b.state = api.LoginState{Step: "done", Method: MethodBrowser, Email: account.Email}
	activeEmail := account.Email
	c.mu.Unlock()

	c.log.Logf(logging.Info, "auth", "signed in as %s (browser)", activeEmail)
	c.notify()
}
