package engine

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// Account storage limits and method names.
const (
	MaxAccounts = 5

	MethodBrowser  = "browser"
	MethodPassword = "password"
)

// ErrAccountLimit reports that accounts.json already holds MaxAccounts.
var ErrAccountLimit = errors.New("account limit reached")

// BrowserToken is a Guardian access token obtained through browser sign-in.
// Browser accounts have no refresh token; reauth_required is the durable
// signal that the user must repeat the browser flow.
type BrowserToken struct {
	Token          string `json:"token"`
	ExpiresAt      int64  `json:"expires_at"`
	ReauthRequired bool   `json:"reauth_required"`
}

// Account is one Firefox Account. Quota is in-memory only; the FxA uid is
// deliberately not persisted.
type Account struct {
	ID         string        `json:"id"`
	Email      string        `json:"email"`
	Label      string        `json:"label"`
	Method     string        `json:"method"`
	Browser    *BrowserToken `json:"browser,omitempty"`
	FxA        *fxa.Tokens   `json:"fxa,omitempty"`
	AddedAt    time.Time     `json:"added_at"`
	LastUsedAt time.Time     `json:"last_used_at"`
	LastError  string        `json:"last_error,omitempty"`
	Quota      *api.Quota    `json:"-"`
}

// accountsFile is the v2 accounts.json schema.
type accountsFile struct {
	Version    int        `json:"version"`
	Active     string     `json:"active,omitempty"`
	LastMethod string     `json:"last_method,omitempty"`
	Accounts   []*Account `json:"accounts"`
}

// Tokens returns the copy of the token set selected by Method, falling back
// to the other set when the selected one is absent or empty.
func (a *Account) Tokens() *fxa.Tokens {
	if a == nil {
		return nil
	}
	fxaTokens := cloneTokens(a.FxA)
	browserTokens := browserAsTokens(a.Browser)
	switch a.Method {
	case MethodBrowser:
		if browserTokens != nil {
			return browserTokens
		}
		return fxaTokens
	case MethodPassword:
		if fxaTokens != nil {
			return fxaTokens
		}
		return browserTokens
	default:
		if fxaTokens != nil {
			return fxaTokens
		}
		return browserTokens
	}
}

// HasRefreshToken reports whether the selected token set can refresh.
func (a *Account) HasRefreshToken() bool {
	t := a.Tokens()
	return t != nil && strings.TrimSpace(t.RefreshToken) != ""
}

// ReauthRequired reports whether the selected token is a browser token that
// Guardian rejected. A password-method account never reports it even when a
// stale browser token remains for fallback.
func (a *Account) ReauthRequired() bool {
	if a == nil {
		return false
	}
	switch a.Method {
	case MethodBrowser:
		return a.Browser != nil && a.Browser.ReauthRequired
	case MethodPassword:
		return false
	default:
		return a.FxA == nil && a.Browser != nil && a.Browser.ReauthRequired
	}
}

func cloneTokens(t *fxa.Tokens) *fxa.Tokens {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

func cloneBrowser(t *BrowserToken) *BrowserToken {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

func browserAsTokens(b *BrowserToken) *fxa.Tokens {
	if b == nil || strings.TrimSpace(b.Token) == "" {
		return nil
	}
	return &fxa.Tokens{AccessToken: b.Token, ExpiresAt: b.ExpiresAt}
}

// accountsPath returns accounts.json beside tokens.json.
func accountsPath(paths settings.Paths) string {
	return filepath.Join(filepath.Dir(paths.Tokens), "accounts.json")
}

// loadAccounts reads accounts.json. The bool reports whether the file exists;
// a malformed file is an error and is never rewritten by this function.
func loadAccounts(path string) (*accountsFile, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("accounts.json %s: %w; move it aside to recover (tokens.json is untouched)", path, err)
	}
	var f accountsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, true, fmt.Errorf("accounts.json %s: %w; move it aside to recover (tokens.json is untouched)", path, err)
	}
	if f.Version == 0 {
		f.Version = 2
	}
	if f.Version < 0 || f.Version > 2 {
		return nil, true, fmt.Errorf("accounts.json %s: unsupported version %d; move it aside to recover (tokens.json is untouched)", path, f.Version)
	}
	seen := make(map[string]struct{}, len(f.Accounts))
	for i, a := range f.Accounts {
		if a == nil {
			return nil, true, fmt.Errorf("accounts.json %s: account %d is null; move it aside to recover (tokens.json is untouched)", path, i)
		}
		if strings.TrimSpace(a.ID) == "" {
			return nil, true, fmt.Errorf("accounts.json %s: account %d is missing an id; move it aside to recover (tokens.json is untouched)", path, i)
		}
		if _, ok := seen[a.ID]; ok {
			return nil, true, fmt.Errorf("accounts.json %s: duplicate account id %q; move it aside to recover (tokens.json is untouched)", path, a.ID)
		}
		seen[a.ID] = struct{}{}
	}
	return &f, true, nil
}

// saveAccounts atomically writes accounts.json at mode 0600.
func saveAccounts(path string, f *accountsFile) error {
	if f == nil {
		return errors.New("accounts: nil accounts file")
	}
	f.Version = 2
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return settings.WriteFileAtomic(path, data, 0o600)
}

// migrateAccounts copies a legacy tokens.json into one password account. It
// never modifies or removes tokens.json, keeping the old release as a
// rollback path. With no usable legacy tokens it returns an empty v2 file and
// writes no accounts.json until a login succeeds.
func migrateAccounts(paths settings.Paths, state settings.State) (*accountsFile, error) {
	tokens, err := loadTokens(paths.Tokens)
	if err != nil {
		return nil, err
	}
	if tokens == nil {
		return &accountsFile{Version: 2}, nil
	}
	id, err := newAccountID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	email := strings.TrimSpace(state.Email)
	account := &Account{
		ID:         id,
		Email:      email,
		Label:      defaultLabel(email),
		Method:     MethodPassword,
		FxA:        tokens,
		AddedAt:    now,
		LastUsedAt: now,
	}
	f := &accountsFile{
		Version:    2,
		Active:     id,
		LastMethod: MethodPassword,
		Accounts:   []*Account{account},
	}
	if err := saveAccounts(accountsPath(paths), f); err != nil {
		return nil, err
	}
	return f, nil
}

// upsert updates an account by case-folded email or appends a new one. The
// caller owns the file-level lock; the five-account cap is enforced here.
func (f *accountsFile) upsert(a *Account) (*Account, error) {
	if f == nil || a == nil {
		return nil, errors.New("account is required")
	}
	email := strings.TrimSpace(a.Email)
	if email == "" {
		return nil, errors.New("account email is required")
	}
	for _, existing := range f.Accounts {
		if existing == nil || !strings.EqualFold(strings.TrimSpace(existing.Email), email) {
			continue
		}
		if a.Label != "" {
			label, err := normalizeLabel(a.Label, existing.Label)
			if err != nil {
				return nil, err
			}
			existing.Label = label
		}
		if a.Method != "" {
			existing.Method = a.Method
		}
		switch a.Method {
		case MethodBrowser:
			if a.Browser != nil {
				existing.Browser = cloneBrowser(a.Browser)
			}
		case MethodPassword:
			if a.FxA != nil {
				existing.FxA = cloneTokens(a.FxA)
			}
		default:
			if a.Browser != nil {
				existing.Browser = cloneBrowser(a.Browser)
			}
			if a.FxA != nil {
				existing.FxA = cloneTokens(a.FxA)
			}
		}
		if a.LastUsedAt.IsZero() {
			existing.LastUsedAt = time.Now()
		} else {
			existing.LastUsedAt = a.LastUsedAt
		}
		existing.LastError = a.LastError
		return existing, nil
	}

	if len(f.Accounts) >= MaxAccounts {
		return nil, ErrAccountLimit
	}
	if a.ID == "" {
		id, err := newAccountID()
		if err != nil {
			return nil, err
		}
		a.ID = id
	}
	a.Email = email
	label, err := normalizeLabel(a.Label, defaultLabel(email))
	if err != nil {
		return nil, err
	}
	a.Label = label
	if a.AddedAt.IsZero() {
		a.AddedAt = time.Now()
	}
	if a.LastUsedAt.IsZero() {
		a.LastUsedAt = a.AddedAt
	}
	f.Accounts = append(f.Accounts, a)
	return a, nil
}

func newAccountID() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate account id: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

func defaultLabel(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return "Firefox account"
	}
	runes := []rune(email)
	if len(runes) > 64 {
		runes = runes[:64]
	}
	return string(runes)
}

func normalizeLabel(label, fallback string) (string, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return fallback, nil
	}
	runes := []rune(label)
	if len(runes) > 64 {
		return "", errors.New("label must be 1-64 characters")
	}
	for _, r := range runes {
		if unicode.IsControl(r) {
			return "", errors.New("label contains control characters")
		}
	}
	return label, nil
}

// summarizeAccounts returns the Account screen view ordered by most recently
// used, then most recently added.
func summarizeAccounts(f *accountsFile, cfg settings.Config) api.AccountsView {
	view := api.AccountsView{
		Rotation:  cfg.AccountRotation,
		Threshold: cfg.RotationThreshold,
	}
	if f == nil {
		return view
	}
	view.Active = f.Active
	view.LastMethod = f.LastMethod
	accounts := append([]*Account(nil), f.Accounts...)
	sort.SliceStable(accounts, func(i, j int) bool {
		if accounts[i] == nil {
			return false
		}
		if accounts[j] == nil {
			return true
		}
		if accounts[i].LastUsedAt.Equal(accounts[j].LastUsedAt) {
			return accounts[i].AddedAt.After(accounts[j].AddedAt)
		}
		return accounts[i].LastUsedAt.After(accounts[j].LastUsedAt)
	})
	view.Accounts = make([]api.AccountSummary, 0, len(accounts))
	for _, a := range accounts {
		if a == nil {
			continue
		}
		var lastUsed int64
		if !a.LastUsedAt.IsZero() {
			lastUsed = a.LastUsedAt.Unix()
		}
		view.Accounts = append(view.Accounts, api.AccountSummary{
			ID:             a.ID,
			Email:          a.Email,
			Label:          a.Label,
			Method:         a.Method,
			Active:         a.ID == f.Active,
			ReauthRequired: a.ReauthRequired(),
			LastUsedAt:     lastUsed,
			Quota:          a.Quota,
		})
	}
	return view
}
