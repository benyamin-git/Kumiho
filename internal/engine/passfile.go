package engine

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/guardian"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// passReuseWindow is the minimum remaining lifetime for a cached pass. A
// minute leaves room for the edge to accept a CONNECT and for the renewal
// timer to run without ever handing the edge an almost-dead bearer.
const passReuseWindow = 60 * time.Second

// passFile is the prepared Guardian pass persisted beside tokens.json. The
// token is a bearer secret: the file is 0600, it never crosses IPC, and it is
// deleted on expiry, logout and account switch.
type passFile struct {
	Token      string `json:"token"`
	ExpiresAt  int64  `json:"expires_at"`
	AccountID  string `json:"account_id"`
	PreparedAt int64  `json:"prepared_at"`
}

// savePassFile writes the prepared pass atomically. A nil or tokenless pass
// is not written.
func (c *Controller) savePassFile(pass *guardian.Pass, account string) error {
	if pass == nil || strings.TrimSpace(pass.Token) == "" {
		return nil
	}
	account = strings.TrimSpace(account)
	if account == "" {
		return nil
	}
	data, err := json.Marshal(passFile{
		Token:      pass.Token,
		ExpiresAt:  pass.ExpiresAt,
		AccountID:  account,
		PreparedAt: time.Now().Unix(),
	})
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return settings.WriteFileAtomic(c.store.PassPath(), data, 0o600)
}

// persistPass saves a freshly fetched pass for the active account and
// publishes it in memory. A write failure is logged, never fatal: the
// in-memory pass still works.
func (c *Controller) persistPass(pass *guardian.Pass) {
	c.mu.Lock()
	account := c.email
	c.pass = pass
	c.passPrepared = true
	c.mu.Unlock()
	if err := c.savePassFile(pass, account); err != nil {
		c.log.Logf(logging.Warn, "guardian", "could not save the prepared pass: %v", err)
	}
}

// loadPassFile reads the prepared pass for the active account. A missing,
// malformed, expired or foreign pass is deleted and reported as absent.
func (c *Controller) loadPassFile() (*guardian.Pass, bool) {
	c.mu.Lock()
	account := c.email
	c.mu.Unlock()
	data, err := os.ReadFile(c.store.PassPath())
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			c.log.Logf(logging.Warn, "guardian", "could not read the prepared pass: %v", err)
		}
		return nil, false
	}
	var f passFile
	if err := json.Unmarshal(data, &f); err != nil || strings.TrimSpace(f.Token) == "" {
		c.deletePassFile()
		return nil, false
	}
	if account == "" || !strings.EqualFold(strings.TrimSpace(f.AccountID), account) ||
		f.ExpiresAt <= time.Now().Unix() {
		c.deletePassFile()
		return nil, false
	}
	return &guardian.Pass{Token: f.Token, ExpiresAt: f.ExpiresAt}, true
}

// deletePassFile removes the prepared pass; a missing file is not an error.
func (c *Controller) deletePassFile() {
	err := os.Remove(c.store.PassPath())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		c.log.Logf(logging.Warn, "guardian", "could not remove the prepared pass: %v", err)
	}
}

// startPassLocked loads the prepared pass during Start. It records that a
// pass was prepared even when the file is expired or foreign, so a later
// failed connect reports bootstrap_required instead of region_blocked.
// Callers hold c.mu.
func (c *Controller) startPassLocked() {
	data, err := os.ReadFile(c.store.PassPath())
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			c.log.Logf(logging.Warn, "guardian", "could not read the prepared pass: %v", err)
		}
		return
	}
	c.passPrepared = true
	var f passFile
	if jsonErr := json.Unmarshal(data, &f); jsonErr != nil || strings.TrimSpace(f.Token) == "" ||
		f.ExpiresAt <= time.Now().Unix() || c.email == "" ||
		!strings.EqualFold(strings.TrimSpace(f.AccountID), c.email) {
		_ = os.Remove(c.store.PassPath())
		return
	}
	c.pass = &guardian.Pass{Token: f.Token, ExpiresAt: f.ExpiresAt}
}

// mapPassError converts a failed pass fetch into the client-facing error. A
// user who prepared before gets bootstrap_required so the UI can offer the
// guide; a first-time blocked user keeps region_blocked. Fatal errors and
// authentication errors keep their own code.
func (c *Controller) mapPassError(err error) error {
	if err == nil {
		return nil
	}
	var fatal *fatalError
	if errors.As(err, &fatal) {
		return err
	}
	var re *api.RemoteError
	if errors.As(err, &re) && (re.Code == api.CodeNotAuthenticated || re.Code == api.CodeBootstrapRequired) {
		return err
	}
	c.mu.Lock()
	prepared := c.passPrepared
	c.mu.Unlock()
	if prepared {
		return &api.RemoteError{Code: api.CodeBootstrapRequired, Message: bootstrapRequiredMessage}
	}
	if blocked := c.regionBlocked(err); blocked != nil {
		return blocked
	}
	return err
}

// cachedPass returns a usable pass without touching the network: the live
// pass first, then the prepared file. A pass must have more than
// passReuseWindow left.
func (c *Controller) cachedPass() *guardian.Pass {
	now := time.Now().Unix()
	c.mu.Lock()
	pass := c.pass
	c.mu.Unlock()
	if pass != nil && pass.ExpiresAt > now+int64(passReuseWindow/time.Second) {
		return pass
	}
	if file, ok := c.loadPassFile(); ok && file.ExpiresAt > now+int64(passReuseWindow/time.Second) {
		c.mu.Lock()
		c.pass = file
		c.mu.Unlock()
		return file
	}
	return nil
}
