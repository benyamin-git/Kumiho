package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/guardian"
	"github.com/benyamin-git/kumiho/internal/logging"
)

// quotaPollUnit scales the quota poll interval; tests shorten it.
var quotaPollUnit = time.Minute

// quotaPollDelayFn is replaceable so poller tests need no wall-clock waits.
var quotaPollDelayFn = quotaPollDelay

var errQuotaReauth = errors.New("quota poll: re-authentication required")

type rotationSnapshot struct {
	id       string
	email    string
	lastUsed time.Time
	tokens   *fxa.Tokens
	reauth   bool
}

type rotationCandidate struct {
	id       string
	email    string
	lastUsed time.Time
	quota    *api.Quota
	usable   bool
	skip     string
}

type rotationResult struct {
	candidate rotationCandidate
	notices   []string
	quota     *api.Quota
}

// selectAccount prepares the active account and, when rotation is enabled,
// chooses the account with the best usable quota. It never switches
// mid-session: Connect calls it exactly once, while the state is Connecting.
func (c *Controller) selectAccount(ctx context.Context) error {
	c.mu.Lock()
	if c.accounts == nil {
		if err := c.ensureAccountsLocked(); err != nil {
			c.mu.Unlock()
			return err
		}
	}
	if c.activeAccountLocked() == nil {
		if mru := mostRecentlyUsedAccount(c.accounts); mru != nil {
			c.setActiveLocked(mru)
			if err := c.saveAccountsLocked(true); err != nil {
				c.mu.Unlock()
				return err
			}
		}
	}
	gen := c.authGen
	state := c.state
	c.mu.Unlock()
	if state != StateConnecting {
		return errors.New("connect was cancelled")
	}

	prepErr := c.prepareAccount(ctx, gen)
	if err := c.selectionFence(gen); err != nil {
		return err
	}
	cfg := c.store.EffectiveConfig()
	if !cfg.AccountRotation {
		c.mu.Lock()
		active := c.activeAccountLocked()
		usable := c.accountSessionUsable(active)
		email := ""
		if active != nil {
			email = active.Email
		}
		if c.authGen == gen {
			c.rotationNotice = ""
		}
		c.mu.Unlock()
		if active == nil {
			return errors.New("all accounts need sign-in again: no accounts")
		}
		if prepErr != nil {
			return prepErr
		}
		if !usable {
			return fmt.Errorf("%s needs sign-in again", email)
		}
		return nil
	}
	return c.rotateIfNeeded(ctx, gen)
}

// prepareAccount validates or refreshes the active account's FxA token set. A
// durable re-auth flag or a missing FxA token set makes the account unusable;
// a stale access token is refreshed through the one generic path. An unusable
// account is reported rather than failing the whole connect when rotation can
// use another account.
func (c *Controller) prepareAccount(ctx context.Context, gen uint64) error {
	c.mu.Lock()
	active := c.activeAccountLocked()
	if active == nil {
		c.mu.Unlock()
		return errors.New("sign in first")
	}
	id, email := active.ID, active.Email
	tokens := active.Tokens()
	reauth := active.ReauthRequired
	c.mu.Unlock()

	if reauth || tokens == nil {
		return fmt.Errorf("%s needs sign-in again", email)
	}
	if c.fxa.AccessTokenValid(tokens) {
		return nil
	}
	if strings.TrimSpace(tokens.RefreshToken) == "" {
		return fmt.Errorf("%s needs sign-in again", email)
	}
	if err := c.refreshAccountToken(ctx, id, gen); err != nil {
		return fmt.Errorf("refresh %s: %w", email, err)
	}
	return nil
}

// refreshAccountToken refreshes one account without dropping it on a
// permanent rejection. A MethodBrowser rejection sets the durable re-auth flag
// first; rotation treats any failed refresh as a skipped account.
func (c *Controller) refreshAccountToken(ctx context.Context, id string, gen uint64) error {
	c.mu.Lock()
	a := findAccountLocked(c.accounts, id)
	if a == nil || a.FxA == nil {
		c.mu.Unlock()
		return errors.New("account has no refreshable session")
	}
	refreshToken := strings.TrimSpace(a.FxA.RefreshToken)
	method := a.Method
	if refreshToken == "" {
		c.mu.Unlock()
		return errors.New("account has no refresh token")
	}
	c.mu.Unlock()

	refreshed, err := c.fxa.Refresh(ctx, refreshToken)
	if err != nil {
		var apiErr *fxa.APIError
		if errors.As(err, &apiErr) && apiErr.PermanentRefreshRejection() &&
			method == MethodBrowser {
			c.markReauthIf(gen, true, id, "browser refresh token rejected; sign in again")
		}
		return err
	}

	c.mu.Lock()
	if c.authGen != gen {
		c.mu.Unlock()
		return errRefreshSessionChanged
	}
	a = findAccountLocked(c.accounts, id)
	if a == nil {
		c.mu.Unlock()
		return errors.New("account was removed while refreshing")
	}
	old := a.FxA
	a.FxA = refreshed
	active := c.activeID == id
	if active {
		c.tokens = refreshed
	}
	if err := c.saveAccountsLocked(active); err != nil {
		a.FxA = old
		if active {
			c.tokens = a.Tokens()
		}
		c.mu.Unlock()
		return err
	}
	email := a.Email
	c.mu.Unlock()
	c.log.Logf(logging.Info, "auth", "renewed FxA access token for %s", email)
	c.notify()
	return nil
}

// rotateIfNeeded evaluates all usable accounts and publishes the best one.
func (c *Controller) rotateIfNeeded(ctx context.Context, gen uint64) error {
	c.mu.Lock()
	if c.authGen != gen || c.state != StateConnecting {
		c.mu.Unlock()
		return errors.New("connect was cancelled")
	}
	cfg := c.store.EffectiveConfig()
	threshold := cfg.RotationThreshold
	if threshold < 1 || threshold > 50 {
		threshold = 10
	}
	c.rotationNotice = ""
	activeID := c.activeID
	snapshots := make([]rotationSnapshot, 0, len(c.accounts.Accounts))
	for _, a := range c.accounts.Accounts {
		if a == nil {
			continue
		}
		snapshots = append(snapshots, rotationSnapshot{
			id:       a.ID,
			email:    a.Email,
			lastUsed: a.LastUsedAt,
			tokens:   a.Tokens(),
			reauth:   a.ReauthRequired,
		})
	}
	c.mu.Unlock()

	budget := c.rotationBudget
	if budget <= 0 {
		budget = 15 * time.Second
	}
	rotationCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	results := make([]rotationResult, len(snapshots))
	errs := make([]error, len(snapshots))
	var wg sync.WaitGroup
	for i := range snapshots {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := c.selectionFence(gen); err != nil {
				errs[i] = err
				return
			}
			results[i], errs[i] = c.rotationCandidate(rotationCtx, gen, snapshots[i])
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}

	candidates := make([]rotationCandidate, 0, len(results))
	var notices []string
	quotas := make(map[string]*api.Quota)
	for _, result := range results {
		notices = append(notices, result.notices...)
		candidates = append(candidates, result.candidate)
		if result.quota != nil {
			quotas[result.candidate.id] = result.quota
		}
	}

	if err := c.selectionFence(gen); err != nil {
		return err
	}
	chosen := chooseRotation(candidates, activeID, threshold)
	notice := strings.Join(notices, "; ")
	if chosen == nil {
		c.setRotationNotice(gen, notice)
		return c.rotationFailure(candidates)
	}
	return c.publishRotation(gen, chosen, quotas, notice)
}

// rotationCandidate resolves one account's tokens and quota. It runs for every
// account concurrently; callers correlate results by slice index so the
// notice order stays stable.
func (c *Controller) rotationCandidate(ctx context.Context, gen uint64, snap rotationSnapshot) (rotationResult, error) {
	candidate := rotationCandidate{id: snap.id, email: snap.email, lastUsed: snap.lastUsed}
	var notices []string
	skip := func(reason string) (rotationResult, error) {
		candidate.skip = reason
		notices = append(notices, fmt.Sprintf("skipping %s: %s", candidate.email, reason))
		return rotationResult{candidate: candidate, notices: notices}, nil
	}
	unknownQuota := func() (rotationResult, error) {
		notices = append(notices, fmt.Sprintf("quota unknown for %s", candidate.email))
		candidate.usable = true
		return rotationResult{candidate: candidate, notices: notices}, nil
	}

	if snap.reauth {
		return skip("sign in again")
	}
	tokens := snap.tokens
	if tokens == nil || strings.TrimSpace(tokens.AccessToken) == "" {
		return skip("sign in again")
	}
	if !c.fxa.AccessTokenValid(tokens) {
		if strings.TrimSpace(tokens.RefreshToken) == "" {
			return skip("sign in again")
		}
		if err := c.refreshAccountToken(ctx, snap.id, gen); err != nil {
			if fenceErr := c.selectionFence(gen); fenceErr != nil {
				return rotationResult{}, fenceErr
			}
			c.log.Logf(logging.Warn, "auth", "account %s token refresh failed: %v", snap.email, err)
			return skip("sign in again")
		}
		tokens = c.accountTokens(snap.id)
		if tokens == nil || strings.TrimSpace(tokens.AccessToken) == "" {
			return skip("sign in again")
		}
	}
	accessToken := tokens.AccessToken
	refreshable := strings.TrimSpace(tokens.RefreshToken) != ""

	quota, err := c.rotationQuota(ctx, accessToken)
	if err != nil && guardian.IsUnauthorized(err) && refreshable {
		if refreshErr := c.refreshAccountToken(ctx, snap.id, gen); refreshErr != nil {
			if fenceErr := c.selectionFence(gen); fenceErr != nil {
				return rotationResult{}, fenceErr
			}
			c.log.Logf(logging.Warn, "auth", "account %s token refresh after 401 failed: %v", snap.email, refreshErr)
			candidate.skip = "sign in again"
		} else if tokens := c.accountTokens(snap.id); tokens == nil || strings.TrimSpace(tokens.AccessToken) == "" {
			candidate.skip = "sign in again"
		} else {
			quota, err = c.rotationQuota(ctx, tokens.AccessToken)
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return rotationResult{}, ctx.Err()
		}
		if guardian.IsUnauthorized(err) {
			c.log.Logf(logging.Warn, "auth", "Guardian rejected the token for %s", snap.email)
			return skip("sign in again")
		}
		c.log.Logf(logging.Warn, "guardian", "quota lookup for %s failed: %v", snap.email, err)
		return unknownQuota()
	}
	candidate.usable = true
	candidate.quota = apiQuotaFromGuardian(quota)
	return rotationResult{candidate: candidate, notices: notices, quota: candidate.quota}, nil
}

func (c *Controller) rotationQuota(ctx context.Context, accessToken string) (*guardian.Quota, error) {
	timeout := c.rotationQuotaTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	quotaCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return c.guardian.Quota(quotaCtx, accessToken)
}

func (c *Controller) accountTokens(id string) *fxa.Tokens {
	c.mu.Lock()
	defer c.mu.Unlock()
	a := findAccountLocked(c.accounts, id)
	if a == nil {
		return nil
	}
	return a.Tokens()
}

func (c *Controller) selectionFence(gen uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.authGen != gen || c.state != StateConnecting {
		return errors.New("connect was cancelled")
	}
	return nil
}

func (c *Controller) setRotationNotice(gen uint64, notice string) {
	c.mu.Lock()
	if c.authGen == gen {
		c.rotationNotice = notice
	}
	c.mu.Unlock()
}

// quotaEligible reports threshold eligibility and whether the quota is known.
// Unknown quota is eligible but sorts after every known eligible account.
func (c *Controller) quotaEligible(limit, remaining *int64, threshold int) (eligible, known bool) {
	return quotaEligibleValue(limit, remaining, threshold)
}

func quotaEligibleValue(limit, remaining *int64, threshold int) (eligible, known bool) {
	if limit == nil || remaining == nil || *limit <= 0 {
		return true, false
	}
	return *remaining*100 >= *limit*int64(threshold), true
}

func chooseRotation(candidates []rotationCandidate, activeID string, threshold int) *rotationCandidate {
	var active *rotationCandidate
	for i := range candidates {
		if candidates[i].id == activeID {
			active = &candidates[i]
			break
		}
	}
	eligible := func(candidate *rotationCandidate) (bool, bool) {
		if candidate == nil || !candidate.usable || candidate.quota == nil {
			return true, false
		}
		if candidate.quota.Remaining != nil && *candidate.quota.Remaining <= 0 {
			return false, true
		}
		return quotaEligibleValue(candidate.quota.Limit, candidate.quota.Remaining, threshold)
	}
	if ok, known := eligible(active); ok && known {
		return active
	}

	// Highest known eligible quota, ties by least recently used.
	var best *rotationCandidate
	for i := range candidates {
		candidate := &candidates[i]
		ok, known := eligible(candidate)
		if !ok || !known {
			continue
		}
		if best == nil || betterRotationCandidate(candidate, best, true) {
			best = candidate
		}
	}
	if best != nil {
		return best
	}

	// Keep a usable active account with positive or unknown quota.
	if active != nil && active.usable && rotationPositiveOrUnknown(active) {
		return active
	}

	// Highest remaining quota above zero.
	best = nil
	for i := range candidates {
		candidate := &candidates[i]
		if !candidate.usable || candidate.quota == nil || candidate.quota.Remaining == nil || *candidate.quota.Remaining <= 0 {
			continue
		}
		if best == nil || betterRotationCandidate(candidate, best, true) {
			best = candidate
		}
	}
	if best != nil {
		return best
	}

	// Unknown quota, least recently used.
	best = nil
	for i := range candidates {
		candidate := &candidates[i]
		if !candidate.usable || (candidate.quota != nil && candidate.quota.Remaining != nil) {
			continue
		}
		if best == nil || betterRotationCandidate(candidate, best, false) {
			best = candidate
		}
	}
	return best
}

func rotationPositiveOrUnknown(candidate *rotationCandidate) bool {
	if candidate == nil || !candidate.usable {
		return false
	}
	if candidate.quota == nil || candidate.quota.Remaining == nil {
		return true
	}
	return *candidate.quota.Remaining > 0
}

func betterRotationCandidate(candidate, best *rotationCandidate, highestRemaining bool) bool {
	if highestRemaining {
		var a, b int64
		if candidate.quota != nil && candidate.quota.Remaining != nil {
			a = *candidate.quota.Remaining
		}
		if best.quota != nil && best.quota.Remaining != nil {
			b = *best.quota.Remaining
		}
		if a != b {
			return a > b
		}
	}
	if candidate.lastUsed.Equal(best.lastUsed) {
		return candidate.id < best.id
	}
	return candidate.lastUsed.Before(best.lastUsed)
}

func (c *Controller) rotationFailure(candidates []rotationCandidate) error {
	var exhausted, needAuth []string
	for _, candidate := range candidates {
		if candidate.usable {
			if candidate.quota != nil && candidate.quota.Remaining != nil && *candidate.quota.Remaining <= 0 {
				exhausted = append(exhausted, candidate.email)
			}
			continue
		}
		needAuth = append(needAuth, candidate.email)
	}
	switch {
	case len(exhausted) > 0 && len(needAuth) > 0:
		return &fatalError{fmt.Sprintf("no usable account: %s have no remaining quota; %s need sign-in again",
			strings.Join(exhausted, ", "), strings.Join(needAuth, ", "))}
	case len(exhausted) > 0:
		return &fatalError{fmt.Sprintf("no usable account: %s have no remaining quota", strings.Join(exhausted, ", "))}
	case len(needAuth) > 0:
		return fmt.Errorf("all accounts need sign-in again: %s", strings.Join(needAuth, ", "))
	default:
		return errors.New("no eligible account: every account has no usable quota")
	}
}

func (c *Controller) publishRotation(gen uint64, chosen *rotationCandidate, quotas map[string]*api.Quota, notice string) error {
	c.mu.Lock()
	if c.authGen != gen || c.state != StateConnecting {
		c.mu.Unlock()
		return errors.New("connect was cancelled")
	}
	a := findAccountLocked(c.accounts, chosen.id)
	if a == nil {
		c.mu.Unlock()
		return errors.New("selected account disappeared during connect")
	}
	for id, quota := range quotas {
		if account := findAccountLocked(c.accounts, id); account != nil {
			account.Quota = quota
		}
	}
	oldID, oldEmail := c.activeID, c.email
	oldTokens := c.tokens
	oldActive := ""
	if c.accounts != nil {
		oldActive = c.accounts.Active
	}
	c.setActiveLocked(a)
	c.quota = a.Quota
	c.rotationNotice = notice
	if err := c.saveAccountsLocked(true); err != nil {
		c.activeID, c.email, c.tokens = oldID, oldEmail, oldTokens
		if c.accounts != nil {
			c.accounts.Active = oldActive
		}
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	if oldID != a.ID {
		c.log.Logf(logging.Info, "auth", "rotated from %s to %s for quota", oldEmail, a.Email)
	}
	c.notify()
	return nil
}

func apiQuotaFromGuardian(q *guardian.Quota) *api.Quota {
	if q == nil {
		return nil
	}
	return &api.Quota{Limit: q.Limit, Remaining: q.Remaining, Reset: q.Reset}
}

// startQuotaPoll refreshes the active account's quota while connected. Errors
// back off and never disconnect; a permanent browser re-auth stops this
// poller.
func (c *Controller) startQuotaPoll(stop <-chan struct{}) {
	cfg := c.store.EffectiveConfig()
	minutes := cfg.QuotaPollMinutes
	if minutes < 1 {
		minutes = 15
	}
	normal := time.Duration(minutes) * quotaPollUnit
	go func() {
		failures := 0
		for {
			delay := normal
			if failures > 0 {
				delay = quotaPollDelayFn(normal, failures)
			}
			timer := time.NewTimer(delay)
			select {
			case <-stop:
				timer.Stop()
				return
			case <-timer.C:
			}
			if stopped(stop) {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			err := c.pollQuotaOnce(ctx)
			cancel()
			if stopped(stop) {
				return
			}
			if err == nil {
				failures = 0
				continue
			}
			if errors.Is(err, errQuotaReauth) {
				c.log.Logf(logging.Info, "tunnel", "quota polling stopped: the account needs re-authentication")
				return
			}
			failures++
			c.log.Logf(logging.Warn, "tunnel", "quota poll failed, retrying with backoff: %v", err)
		}
	}()
}

func quotaPollDelay(normal time.Duration, failures int) time.Duration {
	if normal <= 0 {
		return 0
	}
	maxDelay := 30 * quotaPollUnit
	if maxDelay <= 0 {
		maxDelay = 30 * time.Minute
	}
	delay := normal
	for i := 0; i < failures; i++ {
		if delay >= maxDelay {
			return maxDelay
		}
		delay *= 2
	}
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}

func (c *Controller) pollQuotaOnce(ctx context.Context) error {
	c.mu.Lock()
	active := c.activeAccountLocked()
	tokens := c.tokens
	activeID := c.activeID
	gen := c.authGen
	refreshable := false
	if active != nil {
		activeID = active.ID
		refreshable = active.HasRefreshToken()
	}
	c.mu.Unlock()
	if active == nil || tokens == nil {
		return errors.New("quota poll: not signed in")
	}

	quota, err := c.guardian.Quota(ctx, tokens.AccessToken)
	if err != nil && guardian.IsUnauthorized(err) && refreshable {
		if refreshErr := c.refreshAccountToken(ctx, activeID, gen); refreshErr != nil {
			c.log.Logf(logging.Warn, "tunnel", "quota poll refresh failed: %v", refreshErr)
			if c.accountReauthRequired(activeID) {
				return errQuotaReauth
			}
			return refreshErr
		}
		tokens = c.accountTokens(activeID)
		if tokens == nil {
			return errors.New("quota poll: account disappeared")
		}
		quota, err = c.guardian.Quota(ctx, tokens.AccessToken)
	}
	if err != nil {
		if guardian.IsUnauthorized(err) && c.accountReauthRequired(activeID) {
			return errQuotaReauth
		}
		return err
	}

	c.mu.Lock()
	if c.activeID != activeID || c.authGen != gen {
		c.mu.Unlock()
		return nil
	}
	if account := findAccountLocked(c.accounts, activeID); account != nil {
		account.Quota = apiQuotaFromGuardian(quota)
		c.quota = account.Quota
	} else {
		c.quota = apiQuotaFromGuardian(quota)
	}
	c.mu.Unlock()
	c.notify()
	return nil
}

// markReauth persists the durable top-level re-auth flag for a browser
// account without touching the tunnel or tokens.json.
func (c *Controller) markReauth(id, message string) {
	c.markReauthIf(0, false, id, message)
}

// markReauthIf is markReauth optionally fenced to one auth generation so a
// refresh that started before a newer sign-in cannot mark the new session.
func (c *Controller) markReauthIf(gen uint64, requireMatch bool, id, message string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	c.mu.Lock()
	if requireMatch && c.authGen != gen {
		c.mu.Unlock()
		return
	}
	a := findAccountLocked(c.accounts, id)
	if a == nil || a.Method != MethodBrowser {
		c.mu.Unlock()
		return
	}
	if a.ReauthRequired && a.LastError == message {
		c.mu.Unlock()
		return
	}
	a.ReauthRequired = true
	a.LastError = message
	email := a.Email
	err := c.saveAccountsLocked(false)
	c.mu.Unlock()
	if err != nil {
		c.log.Logf(logging.Warn, "auth", "could not persist re-authentication flag for %s: %v", email, err)
	}
	c.log.Logf(logging.Warn, "auth", "%s needs re-authentication: %s", email, message)
	c.notify()
}

// accountReauthRequired reports whether the stored account carries the
// durable re-auth flag. Callers use it to translate a failed refresh into the
// quota poller's stop signal.
func (c *Controller) accountReauthRequired(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	a := findAccountLocked(c.accounts, id)
	return a != nil && a.ReauthRequired
}
