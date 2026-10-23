package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/guardian"
	"github.com/benyamin-git/kumiho/internal/settings"
)

func accountTestPaths(t *testing.T) settings.Paths {
	t.Helper()
	dir := t.TempDir()
	return settings.Paths{
		Config:  filepath.Join(dir, "config.toml"),
		State:   filepath.Join(dir, "state.json"),
		Tokens:  filepath.Join(dir, "tokens.json"),
		Runtime: dir,
	}
}

func TestAccountsRoundTrip(t *testing.T) {
	paths := accountTestPaths(t)
	added := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	used := added.Add(5 * time.Minute)
	f := &accountsFile{
		Active:     "browser-id",
		LastMethod: MethodBrowser,
		Accounts: []*Account{
			{
				ID:         "browser-id",
				Email:      "browser@example.com",
				Label:      "Browser",
				Method:     MethodBrowser,
				Browser:    &BrowserToken{Token: "browser-token", ExpiresAt: 123, ReauthRequired: true},
				AddedAt:    added,
				LastUsedAt: used,
				Quota:      &api.Quota{},
			},
			{
				ID:         "password-id",
				Email:      "password@example.com",
				Label:      "Password",
				Method:     MethodPassword,
				FxA:        &fxa.Tokens{AccessToken: "access", RefreshToken: "refresh", ExpiresAt: 456},
				AddedAt:    added.Add(time.Minute),
				LastUsedAt: used.Add(time.Minute),
			},
		},
	}
	path := accountsPath(paths)
	if err := saveAccounts(path, f); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}

	got, exists, err := loadAccounts(path)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("accounts file should exist")
	}
	if got.Version != 2 || got.Active != "browser-id" || got.LastMethod != MethodBrowser {
		t.Fatalf("file = %+v", got)
	}
	if len(got.Accounts) != 2 {
		t.Fatalf("accounts = %d", len(got.Accounts))
	}
	if got.Accounts[0].Label != "Browser" || !got.Accounts[0].AddedAt.Equal(added) || !got.Accounts[0].LastUsedAt.Equal(used) {
		t.Fatalf("browser account = %+v", got.Accounts[0])
	}
	if got.Accounts[0].Browser == nil || got.Accounts[0].Browser.Token != "browser-token" ||
		got.Accounts[0].Browser.ExpiresAt != 123 || !got.Accounts[0].Browser.ReauthRequired {
		t.Fatalf("browser token = %+v", got.Accounts[0].Browser)
	}
	if got.Accounts[0].Quota != nil {
		t.Fatal("quota must not be persisted")
	}
	if got.Accounts[1].FxA == nil || got.Accounts[1].FxA.AccessToken != "access" ||
		got.Accounts[1].FxA.RefreshToken != "refresh" || got.Accounts[1].FxA.ExpiresAt != 456 {
		t.Fatalf("fxa tokens = %+v", got.Accounts[1].FxA)
	}
}

func TestMigrateLegacyTokensCopiesAndKeepsOriginal(t *testing.T) {
	paths := accountTestPaths(t)
	legacy := &fxa.Tokens{AccessToken: "a", RefreshToken: "r", ExpiresAt: 123}
	if err := saveTokens(paths.Tokens, legacy); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(paths.Tokens)
	if err != nil {
		t.Fatal(err)
	}
	f, err := migrateAccounts(paths, settings.State{Email: "user@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Accounts) != 1 {
		t.Fatalf("accounts = %d", len(f.Accounts))
	}
	a := f.Accounts[0]
	if f.Active != a.ID || f.LastMethod != MethodPassword {
		t.Fatalf("file = %+v", f)
	}
	if a.Method != MethodPassword || a.Email != "user@example.com" || a.Label != "user@example.com" {
		t.Fatalf("account = %+v", a)
	}
	if a.FxA == nil || a.FxA.AccessToken != "a" || a.FxA.RefreshToken != "r" {
		t.Fatalf("migrated tokens = %+v", a.FxA)
	}
	after, err := os.ReadFile(paths.Tokens)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("tokens.json changed during migration")
	}
	if _, err := os.Stat(accountsPath(paths)); err != nil {
		t.Fatalf("accounts.json not written: %v", err)
	}
}

func TestMigrationTable(t *testing.T) {
	t.Run("no tokens or email", func(t *testing.T) {
		paths := accountTestPaths(t)
		f, err := migrateAccounts(paths, settings.State{})
		if err != nil {
			t.Fatal(err)
		}
		if f.Version != 2 || len(f.Accounts) != 0 || f.Active != "" {
			t.Fatalf("file = %+v", f)
		}
		if _, err := os.Stat(accountsPath(paths)); !os.IsNotExist(err) {
			t.Fatalf("accounts.json should not exist, stat err = %v", err)
		}
	})

	t.Run("tokens and email", func(t *testing.T) {
		paths := accountTestPaths(t)
		if err := saveTokens(paths.Tokens, &fxa.Tokens{AccessToken: "a", RefreshToken: "r", ExpiresAt: 123}); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(paths.Tokens)
		f, err := migrateAccounts(paths, settings.State{Email: "user@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		if len(f.Accounts) != 1 || f.Active == "" || f.Accounts[0].Label != "user@example.com" {
			t.Fatalf("file = %+v", f)
		}
		loaded, exists, err := loadAccounts(accountsPath(paths))
		if err != nil || !exists {
			t.Fatalf("load: exists=%v err=%v", exists, err)
		}
		if loaded.Version != 2 {
			t.Fatalf("version = %d", loaded.Version)
		}
		after, _ := os.ReadFile(paths.Tokens)
		if !bytes.Equal(before, after) {
			t.Fatal("tokens.json changed")
		}
	})

	t.Run("tokens and empty email", func(t *testing.T) {
		paths := accountTestPaths(t)
		if err := saveTokens(paths.Tokens, &fxa.Tokens{AccessToken: "a"}); err != nil {
			t.Fatal(err)
		}
		f, err := migrateAccounts(paths, settings.State{})
		if err != nil {
			t.Fatal(err)
		}
		if len(f.Accounts) != 1 || f.Accounts[0].Label != "Firefox account" {
			t.Fatalf("file = %+v", f)
		}
	})

	t.Run("empty tokens file", func(t *testing.T) {
		paths := accountTestPaths(t)
		if err := os.WriteFile(paths.Tokens, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		f, err := migrateAccounts(paths, settings.State{Email: "user@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		if len(f.Accounts) != 0 || f.Active != "" {
			t.Fatalf("file = %+v", f)
		}
		if _, err := os.Stat(accountsPath(paths)); !os.IsNotExist(err) {
			t.Fatalf("accounts.json should not exist, stat err = %v", err)
		}
	})

	t.Run("malformed tokens file", func(t *testing.T) {
		paths := accountTestPaths(t)
		bad := []byte("{bad")
		if err := os.WriteFile(paths.Tokens, bad, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := migrateAccounts(paths, settings.State{Email: "user@example.com"})
		if err == nil || !strings.Contains(err.Error(), paths.Tokens) || !strings.Contains(err.Error(), "tokens.json") {
			t.Fatalf("error = %v", err)
		}
		if _, statErr := os.Stat(accountsPath(paths)); !os.IsNotExist(statErr) {
			t.Fatalf("accounts.json should not exist, stat err = %v", statErr)
		}
		after, _ := os.ReadFile(paths.Tokens)
		if !bytes.Equal(bad, after) {
			t.Fatal("malformed tokens.json was modified")
		}
	})
}

func TestAccountLimit(t *testing.T) {
	f := &accountsFile{Version: 2}
	for i := 0; i < MaxAccounts; i++ {
		f.Accounts = append(f.Accounts, &Account{ID: string(rune('a' + i)), Email: "u" + string(rune('a'+i)) + "@example.com"})
	}
	_, err := f.upsert(&Account{Email: "sixth@example.com"})
	if !errors.Is(err, ErrAccountLimit) {
		t.Fatalf("error = %v, want ErrAccountLimit", err)
	}
	if len(f.Accounts) != MaxAccounts {
		t.Fatalf("accounts = %d, want %d", len(f.Accounts), MaxAccounts)
	}
}

func TestDuplicateEmailAtLimitUpdatesExisting(t *testing.T) {
	added := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	f := &accountsFile{Version: 2}
	for i := 0; i < MaxAccounts; i++ {
		f.Accounts = append(f.Accounts, &Account{
			ID:      string(rune('a' + i)),
			Email:   "u" + string(rune('a'+i)) + "@example.com",
			Label:   "Existing",
			AddedAt: added,
		})
	}
	got, err := f.upsert(&Account{
		Email:      "UA@example.com",
		Label:      "",
		Method:     MethodPassword,
		FxA:        &fxa.Tokens{AccessToken: "new"},
		LastUsedAt: added.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Accounts) != MaxAccounts {
		t.Fatalf("accounts = %d, want %d", len(f.Accounts), MaxAccounts)
	}
	if got.ID != "a" || !got.AddedAt.Equal(added) || got.Label != "Existing" {
		t.Fatalf("updated account = %+v", got)
	}
	if got.FxA == nil || got.FxA.AccessToken != "new" {
		t.Fatalf("tokens not replaced: %+v", got.FxA)
	}
}

func TestAccountTokensPrefersMethod(t *testing.T) {
	browser := &Account{
		Method:  MethodBrowser,
		Browser: &BrowserToken{Token: "browser-token"},
		FxA:     &fxa.Tokens{AccessToken: "fxa-token"},
	}
	if got := browser.Tokens(); got == nil || got.AccessToken != "browser-token" {
		t.Fatalf("browser tokens = %+v", got)
	}
	password := &Account{
		Method:  MethodPassword,
		Browser: &BrowserToken{Token: "browser-token"},
		FxA:     &fxa.Tokens{AccessToken: "fxa-token"},
	}
	if got := password.Tokens(); got == nil || got.AccessToken != "fxa-token" {
		t.Fatalf("password tokens = %+v", got)
	}
	browserFallback := &Account{
		Method: MethodBrowser,
		FxA:    &fxa.Tokens{AccessToken: "fxa-token"},
	}
	if got := browserFallback.Tokens(); got == nil || got.AccessToken != "fxa-token" {
		t.Fatalf("fallback tokens = %+v", got)
	}
}

func TestReauthOnlyForSelectedBrowserMethod(t *testing.T) {
	cases := []struct {
		name string
		a    *Account
		want bool
	}{
		{name: "password method", a: &Account{Method: MethodPassword, Browser: &BrowserToken{ReauthRequired: true}, FxA: &fxa.Tokens{AccessToken: "x"}}, want: false},
		{name: "browser method", a: &Account{Method: MethodBrowser, Browser: &BrowserToken{ReauthRequired: true}, FxA: &fxa.Tokens{AccessToken: "x"}}, want: true},
		{name: "fallback browser", a: &Account{Browser: &BrowserToken{ReauthRequired: true}}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.ReauthRequired(); got != tc.want {
				t.Fatalf("ReauthRequired = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLabelsDefaultTrimAndRejectControl(t *testing.T) {
	if got := defaultLabel("user@example.com"); got != "user@example.com" {
		t.Fatalf("defaultLabel = %q", got)
	}
	if got := defaultLabel(""); got != "Firefox account" {
		t.Fatalf("defaultLabel empty = %q", got)
	}
	if got := defaultLabel(strings.Repeat("x", 80)); len([]rune(got)) != 64 {
		t.Fatalf("defaultLabel length = %d", len([]rune(got)))
	}
	if got, err := normalizeLabel("", "fallback"); err != nil || got != "fallback" {
		t.Fatalf("normalize empty = %q, %v", got, err)
	}
	if got, err := normalizeLabel("  Personal  ", "fallback"); err != nil || got != "Personal" {
		t.Fatalf("normalize trim = %q, %v", got, err)
	}
	if _, err := normalizeLabel(strings.Repeat("a", 64), "fallback"); err != nil {
		t.Fatalf("64-rune label rejected: %v", err)
	}
	if _, err := normalizeLabel(strings.Repeat("a", 65), "fallback"); err == nil {
		t.Fatal("65-rune label should be rejected")
	}
	for _, bad := range []string{"\x00", "a\nb", "a\rb", "a\tb"} {
		if _, err := normalizeLabel(bad, "fallback"); err == nil {
			t.Fatalf("control label %q should be rejected", bad)
		}
	}
}

func TestCorruptAccountsLeavesLegacyTokens(t *testing.T) {
	paths := accountTestPaths(t)
	bad := []byte("{bad")
	if err := os.WriteFile(accountsPath(paths), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveTokens(paths.Tokens, &fxa.Tokens{AccessToken: "a", RefreshToken: "r"}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(paths.Tokens)
	_, _, err := loadAccounts(accountsPath(paths))
	if err == nil || !strings.Contains(err.Error(), accountsPath(paths)) ||
		!strings.Contains(err.Error(), "tokens.json") || !strings.Contains(err.Error(), "move it aside") {
		t.Fatalf("error = %v", err)
	}
	after, _ := os.ReadFile(paths.Tokens)
	if !bytes.Equal(before, after) {
		t.Fatal("tokens.json changed")
	}
}

func TestLoadAccountsRejectsNilEmptyAndDuplicateEntries(t *testing.T) {
	valid := `{"id":"a","email":"a@example.com","method":"password"}`
	tests := []struct {
		name string
		body string
	}{
		{name: "nil account", body: `{"version":2,"accounts":[null]}`},
		{name: "nil before valid", body: `{"version":2,"accounts":[null,` + valid + `]}`},
		{name: "empty id", body: `{"version":2,"accounts":[{"email":"a@example.com"}]}`},
		{name: "duplicate id", body: `{"version":2,"accounts":[` + valid + `,` + valid + `]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths := accountTestPaths(t)
			path := accountsPath(paths)
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, _, err := loadAccounts(path)
			if err == nil || !strings.Contains(err.Error(), "move it aside") ||
				!strings.Contains(err.Error(), "tokens.json") {
				t.Fatalf("loadAccounts error = %v, want recoverable rejection", err)
			}
		})
	}
}

func TestMigratedAccountIDsAreStable(t *testing.T) {
	paths := accountTestPaths(t)
	if err := saveTokens(paths.Tokens, &fxa.Tokens{AccessToken: "a"}); err != nil {
		t.Fatal(err)
	}
	f, err := migrateAccounts(paths, settings.State{Email: "user@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	id, added := f.Accounts[0].ID, f.Accounts[0].AddedAt
	if err := saveAccounts(accountsPath(paths), f); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := loadAccounts(accountsPath(paths))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Accounts[0].ID != id || !loaded.Accounts[0].AddedAt.Equal(added) {
		t.Fatalf("account changed after round trip: %+v", loaded.Accounts[0])
	}
}

// multiLoginHandler completes password logins for any email and mints a fresh
// access token for every OAuth call.
func multiLoginHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	var tokenSeq atomic.Int32
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/account/login":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode login body: %v", err)
				return
			}
			if body["email"] == nil || body["email"] == "" {
				t.Errorf("login email missing: %v", body)
			}
			fmt.Fprintf(w, `{"sessionToken":%q,"verified":true}`, testSessionHex())
		case "/v1/oauth/token":
			n := tokenSeq.Add(1)
			fmt.Fprintf(w, `{"access_token":"access-%d","refresh_token":"refresh-%d","expires_in":3600}`, n, n)
		default:
			http.NotFound(w, r)
		}
	}
}

func loginAs(t *testing.T, ctrl *Controller, email string) api.LoginState {
	t.Helper()
	if ls := ctrl.LoginEmail(email); ls.Step != "password" {
		t.Fatalf("login_email(%s) = %+v", email, ls)
	}
	ls := ctrl.LoginPassword(context.Background(), "pw")
	if ls.Step != "done" || ls.Error != "" {
		t.Fatalf("login_password(%s) = %+v", email, ls)
	}
	return ls
}

func findAccountInView(view api.AccountsView, email string) *api.AccountSummary {
	for i := range view.Accounts {
		if strings.EqualFold(view.Accounts[i].Email, email) {
			return &view.Accounts[i]
		}
	}
	return nil
}

func TestStartMigratesLegacySession(t *testing.T) {
	ctrl, paths := newTestController(t, multiLoginHandler(t))
	ctx := context.Background()
	if err := saveTokens(paths.Tokens, &fxa.Tokens{
		AccessToken:  "legacy-access",
		RefreshToken: "legacy-refresh",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.store.UpdateState(func(s *settings.State) { s.Email = "user@example.com" }); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(paths.Tokens)
	if err != nil {
		t.Fatal(err)
	}

	if err := ctrl.Start(ctx); err != nil {
		t.Fatal(err)
	}
	st := ctrl.Snapshot()
	if st.State != string(StateIdle) || !st.Authenticated || st.Email != "user@example.com" {
		t.Fatalf("snapshot = %+v", st)
	}
	view := ctrl.Accounts()
	if len(view.Accounts) != 1 || view.Active == "" {
		t.Fatalf("accounts = %+v", view)
	}
	if view.Accounts[0].Email != "user@example.com" || view.Accounts[0].Method != MethodPassword ||
		view.Accounts[0].Label != "user@example.com" || !view.Accounts[0].Active {
		t.Fatalf("migrated account = %+v", view.Accounts[0])
	}
	if st.ActiveAccount != view.Active {
		t.Fatalf("snapshot active = %q, accounts active = %q", st.ActiveAccount, view.Active)
	}
	if _, err := os.Stat(accountsPath(paths)); err != nil {
		t.Fatalf("accounts.json: %v", err)
	}
	after, err := os.ReadFile(paths.Tokens)
	if err != nil {
		t.Fatalf("tokens.json after migration: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("Start rewrote tokens.json during migration")
	}
}

func TestStartFailsOnCorruptAccounts(t *testing.T) {
	ctrl, paths := newTestController(t, multiLoginHandler(t))
	bad := []byte("{bad accounts")
	if err := os.WriteFile(accountsPath(paths), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveTokens(paths.Tokens, &fxa.Tokens{AccessToken: "legacy", RefreshToken: "refresh"}); err != nil {
		t.Fatal(err)
	}
	beforeTokens, _ := os.ReadFile(paths.Tokens)

	err := ctrl.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), accountsPath(paths)) ||
		!strings.Contains(err.Error(), "move it aside") || !strings.Contains(err.Error(), "tokens.json") {
		t.Fatalf("Start error = %v", err)
	}
	afterAccounts, readErr := os.ReadFile(accountsPath(paths))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(bad, afterAccounts) {
		t.Fatal("Start rewrote malformed accounts.json")
	}
	afterTokens, readErr := os.ReadFile(paths.Tokens)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(beforeTokens, afterTokens) {
		t.Fatal("Start rewrote tokens.json after an accounts.json error")
	}
	if st := ctrl.Snapshot(); st.Authenticated {
		t.Fatalf("corrupt-account controller is authenticated: %+v", st)
	}
}

func TestSecondPasswordLoginKeepsFirstAccount(t *testing.T) {
	ctrl, paths := newTestController(t, multiLoginHandler(t))

	loginAs(t, ctrl, "user1@example.com")
	first, err := loadTokens(paths.Tokens)
	if err != nil || first == nil {
		t.Fatalf("first tokens = %+v, err = %v", first, err)
	}
	loginAs(t, ctrl, "user2@example.com")
	second, err := loadTokens(paths.Tokens)
	if err != nil || second == nil {
		t.Fatalf("second tokens = %+v, err = %v", second, err)
	}
	if first.AccessToken == second.AccessToken {
		t.Fatal("second login did not replace tokens.json")
	}

	view := ctrl.Accounts()
	if len(view.Accounts) != 2 {
		t.Fatalf("accounts = %+v", view)
	}
	active := findAccountInView(view, "user2@example.com")
	if active == nil || !active.Active || view.Active != active.ID {
		t.Fatalf("active account = %+v (view active %q)", active, view.Active)
	}
	if st := ctrl.Snapshot(); st.ActiveAccount != active.ID || st.Email != "user2@example.com" {
		t.Fatalf("snapshot = %+v", st)
	}
	ctrl.mu.Lock()
	got := ctrl.activeAccountLocked()
	ctrl.mu.Unlock()
	if got == nil || got.FxA == nil || got.FxA.AccessToken != second.AccessToken {
		t.Fatalf("active account tokens = %+v", got)
	}
}

func TestUseAccountSwitchesAndRefusesWhileConnected(t *testing.T) {
	ctrl, paths := newTestController(t, multiLoginHandler(t))

	loginAs(t, ctrl, "first@example.com")
	firstView := ctrl.Accounts()
	firstID := firstView.Active
	firstTokens, err := loadTokens(paths.Tokens)
	if err != nil || firstTokens == nil {
		t.Fatalf("first tokens = %+v, err = %v", firstTokens, err)
	}
	loginAs(t, ctrl, "second@example.com")
	secondView := ctrl.Accounts()
	secondID := secondView.Active

	if err := ctrl.UseAccount(firstID); err != nil {
		t.Fatal(err)
	}
	if st := ctrl.Snapshot(); st.Email != "first@example.com" || st.ActiveAccount != firstID {
		t.Fatalf("snapshot after switch = %+v", st)
	}
	switched, err := loadTokens(paths.Tokens)
	if err != nil || switched == nil || switched.AccessToken != firstTokens.AccessToken {
		t.Fatalf("tokens after switch = %+v, err = %v", switched, err)
	}

	ctrl.mu.Lock()
	ctrl.state = StateConnected
	ctrl.mu.Unlock()
	err = ctrl.UseAccount(secondID)
	var remote *api.RemoteError
	if !errors.As(err, &remote) || remote.Code != api.CodeBadRequest || !strings.Contains(remote.Message, "disconnect first") {
		t.Fatalf("switch while connected = %v", err)
	}
	if st := ctrl.Snapshot(); st.ActiveAccount != firstID || st.Email != "first@example.com" {
		t.Fatalf("snapshot changed after refused switch = %+v", st)
	}
}

func TestRemoveActiveSelectsMostRecent(t *testing.T) {
	ctrl, paths := newTestController(t, multiLoginHandler(t))
	for _, email := range []string{"a@example.com", "b@example.com", "c@example.com"} {
		loginAs(t, ctrl, email)
	}

	now := time.Now()
	ctrl.mu.Lock()
	a := emailAccount(ctrl.accounts, "a@example.com")
	b := emailAccount(ctrl.accounts, "b@example.com")
	c := emailAccount(ctrl.accounts, "c@example.com")
	if a == nil || b == nil || c == nil {
		ctrl.mu.Unlock()
		t.Fatalf("seeded accounts = %+v", ctrl.accounts)
	}
	a.LastUsedAt = now.Add(-3 * time.Hour)
	b.LastUsedAt = now.Add(-time.Hour)
	c.LastUsedAt = now.Add(-2 * time.Hour)
	ctrl.setActiveLocked(b)
	bID, cID, aID := b.ID, c.ID, a.ID
	ctrl.mu.Unlock()

	if err := ctrl.RemoveAccount(bID); err != nil {
		t.Fatal(err)
	}
	if st := ctrl.Snapshot(); st.ActiveAccount != cID || st.Email != "c@example.com" {
		t.Fatalf("after removing active: %+v", st)
	}
	if view := ctrl.Accounts(); len(view.Accounts) != 2 || view.Active != cID {
		t.Fatalf("accounts after remove = %+v", view)
	}

	if err := ctrl.RemoveAccount(cID); err != nil {
		t.Fatal(err)
	}
	if st := ctrl.Snapshot(); st.ActiveAccount != aID || st.Email != "a@example.com" {
		t.Fatalf("after removing second active: %+v", st)
	}
	if err := ctrl.RemoveAccount(aID); err != nil {
		t.Fatal(err)
	}
	st := ctrl.Snapshot()
	if st.State != string(StateUnauthenticated) || st.Authenticated || st.Email != "" ||
		st.ActiveAccount != "" || len(st.Accounts) != 0 || st.Login != nil {
		t.Fatalf("after removing last account: %+v", st)
	}
	for _, path := range []string{accountsPath(paths), paths.Tokens} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s should be gone, stat err = %v", path, err)
		}
	}
}

func TestLogoutClearsEveryAccount(t *testing.T) {
	ctrl, paths := newTestController(t, multiLoginHandler(t))
	loginAs(t, ctrl, "one@example.com")
	loginAs(t, ctrl, "two@example.com")

	if err := ctrl.Logout(); err != nil {
		t.Fatal(err)
	}
	st := ctrl.Snapshot()
	if st.State != string(StateUnauthenticated) || st.Authenticated || st.Email != "" ||
		st.ActiveAccount != "" || len(st.Accounts) != 0 || st.Login != nil {
		t.Fatalf("snapshot after logout = %+v", st)
	}
	for _, path := range []string{accountsPath(paths), paths.Tokens} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s should be gone, stat err = %v", path, err)
		}
	}
}

func TestLoginStartBlockedWhileConnected(t *testing.T) {
	ctrl, _ := newTestController(t, multiLoginHandler(t))
	loginAs(t, ctrl, "active@example.com")
	activeID := ctrl.Accounts().Active
	ctrl.mu.Lock()
	ctrl.state = StateConnected
	ctrl.mu.Unlock()

	ls := ctrl.LoginStart(MethodPassword, "other@example.com", "")
	if ls.Step != "error" || !strings.Contains(strings.ToLower(ls.Error), "disconnect first") {
		t.Fatalf("switch login while connected = %+v", ls)
	}
	ls = ctrl.LoginStart(MethodPassword, "", activeID)
	if ls.Step != "password" || ls.Email != "active@example.com" || ls.Method != "email" {
		t.Fatalf("reauth login while connected = %+v", ls)
	}
}

func TestAccountLimitLeavesActiveSession(t *testing.T) {
	ctrl, paths := newTestController(t, multiLoginHandler(t))
	now := time.Now()
	f := &accountsFile{Version: 2, LastMethod: MethodPassword}
	for i := 0; i < MaxAccounts; i++ {
		email := fmt.Sprintf("user%d@example.com", i)
		f.Accounts = append(f.Accounts, &Account{
			ID:         fmt.Sprintf("id-%d", i),
			Email:      email,
			Label:      email,
			Method:     MethodPassword,
			FxA:        &fxa.Tokens{AccessToken: fmt.Sprintf("access-%d", i), RefreshToken: fmt.Sprintf("refresh-%d", i), ExpiresAt: now.Add(time.Hour).Unix()},
			AddedAt:    now.Add(time.Duration(i) * time.Minute),
			LastUsedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}
	f.Active = f.Accounts[0].ID
	if err := saveAccounts(accountsPath(paths), f); err != nil {
		t.Fatal(err)
	}
	if err := saveTokens(paths.Tokens, f.Accounts[0].FxA); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	beforeAccounts, err := os.ReadFile(accountsPath(paths))
	if err != nil {
		t.Fatal(err)
	}
	beforeTokens, err := os.ReadFile(paths.Tokens)
	if err != nil {
		t.Fatal(err)
	}

	if ls := ctrl.LoginStart(MethodPassword, "sixth@example.com", ""); ls.Step != "password" {
		t.Fatalf("login_start = %+v", ls)
	}
	ls := ctrl.LoginPassword(context.Background(), "pw")
	if ls.Step != "done" || !strings.Contains(ls.Error, "account limit reached") {
		t.Fatalf("sixth login = %+v", ls)
	}
	afterAccounts, _ := os.ReadFile(accountsPath(paths))
	afterTokens, _ := os.ReadFile(paths.Tokens)
	if !bytes.Equal(beforeAccounts, afterAccounts) || !bytes.Equal(beforeTokens, afterTokens) {
		t.Fatal("rejected sixth login changed accounts.json or tokens.json")
	}
	st := ctrl.Snapshot()
	if st.ActiveAccount != f.Accounts[0].ID || st.Email != f.Accounts[0].Email || len(st.Accounts) != MaxAccounts {
		t.Fatalf("active session after rejected login = %+v", st)
	}
}

func TestDuplicateEmailReauthUpdatesInPlace(t *testing.T) {
	ctrl, paths := newTestController(t, multiLoginHandler(t))
	loginAs(t, ctrl, "user@example.com")
	view := ctrl.Accounts()
	if len(view.Accounts) != 1 {
		t.Fatalf("initial accounts = %+v", view)
	}
	id := view.Active
	ctrl.mu.Lock()
	added := ctrl.accounts.Accounts[0].AddedAt
	ctrl.mu.Unlock()
	if err := ctrl.UpdateAccount(id, "Work"); err != nil {
		t.Fatal(err)
	}
	firstTokens, err := loadTokens(paths.Tokens)
	if err != nil || firstTokens == nil {
		t.Fatalf("first tokens = %+v, err = %v", firstTokens, err)
	}

	loginAs(t, ctrl, "USER@example.com")
	view = ctrl.Accounts()
	if len(view.Accounts) != 1 {
		t.Fatalf("duplicate email created accounts = %+v", view)
	}
	got := view.Accounts[0]
	if got.ID != id || got.Label != "Work" || !got.Active {
		t.Fatalf("updated account = %+v, want id %q label Work", got, id)
	}
	ctrl.mu.Lock()
	account := ctrl.activeAccountLocked()
	gotAdded := account.AddedAt
	ctrl.mu.Unlock()
	if !gotAdded.Equal(added) {
		t.Fatalf("AddedAt changed: %v -> %v", added, gotAdded)
	}
	secondTokens, err := loadTokens(paths.Tokens)
	if err != nil || secondTokens == nil {
		t.Fatalf("second tokens = %+v, err = %v", secondTokens, err)
	}
	if secondTokens.AccessToken == firstTokens.AccessToken {
		t.Fatal("re-authentication did not replace the account token")
	}
}

func TestAccountLabelValidation(t *testing.T) {
	ctrl, _ := newTestController(t, multiLoginHandler(t))
	loginAs(t, ctrl, "label@example.com")
	id := ctrl.Accounts().Active

	if err := ctrl.UpdateAccount(id, "  Personal  "); err != nil {
		t.Fatal(err)
	}
	if got := ctrl.Accounts().Accounts[0].Label; got != "Personal" {
		t.Fatalf("trimmed label = %q", got)
	}
	if err := ctrl.UpdateAccount(id, "   "); err != nil {
		t.Fatal(err)
	}
	if got := ctrl.Accounts().Accounts[0].Label; got != "label@example.com" {
		t.Fatalf("empty label = %q", got)
	}
	for _, bad := range []string{strings.Repeat("x", 65), "a\nb", "a\tb"} {
		if err := ctrl.UpdateAccount(id, bad); err == nil {
			t.Fatalf("label %q should be rejected", bad)
		}
		if got := ctrl.Accounts().Accounts[0].Label; got != "label@example.com" {
			t.Fatalf("label %q changed stored label to %q", bad, got)
		}
	}
	if err := ctrl.UpdateAccount("missing", "Nope"); err == nil {
		t.Fatal("unknown account update should fail")
	}
}

func seedBrowserAccount(t *testing.T, ctrl *Controller, paths settings.Paths, token string, expires int64) string {
	t.Helper()
	now := time.Now()
	const id = "browser-id"
	f := &accountsFile{
		Version:    2,
		Active:     id,
		LastMethod: MethodBrowser,
		Accounts: []*Account{{
			ID:         id,
			Email:      "browser@example.com",
			Label:      "Browser",
			Method:     MethodBrowser,
			Browser:    &BrowserToken{Token: token, ExpiresAt: expires},
			AddedAt:    now,
			LastUsedAt: now,
		}},
	}
	if err := saveAccounts(accountsPath(paths), f); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestBrowserTokenWithoutExpiryIsUsableUntil401(t *testing.T) {
	var oauthCalls atomic.Int32
	ctrl, paths := newTestController(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth/token" {
			oauthCalls.Add(1)
		}
		http.NotFound(w, r)
	})
	id := seedBrowserAccount(t, ctrl, paths, "browser-token", 0)
	ctx := context.Background()

	if err := ctrl.EnsureFreshToken(ctx); err != nil {
		t.Fatalf("browser token without expiry should be usable: %v", err)
	}
	if oauthCalls.Load() != 0 {
		t.Fatalf("oauth calls before 401 = %d", oauthCalls.Load())
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"invalid token"}`)
	}))
	defer srv.Close()
	ctrl.guardian = &guardian.Client{BaseURL: srv.URL, HTTP: srv.Client()}
	info := ctrl.Account(ctx)
	if info.ID != id || info.Label != "Browser" || info.Method != MethodBrowser || !info.ReauthRequired {
		t.Fatalf("browser account info = %+v", info)
	}
	if info.Error == "" || !strings.Contains(info.Error, "401") {
		t.Fatalf("browser 401 error = %q", info.Error)
	}
	if oauthCalls.Load() != 0 {
		t.Fatalf("browser 401 used /v1/oauth/token: calls = %d", oauthCalls.Load())
	}
	loaded, _, err := loadAccounts(accountsPath(paths))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Accounts[0].Browser == nil || !loaded.Accounts[0].Browser.ReauthRequired {
		t.Fatalf("persisted browser token = %+v", loaded.Accounts[0].Browser)
	}
	view := ctrl.Accounts()
	if len(view.Accounts) != 1 || !view.Accounts[0].ReauthRequired {
		t.Fatalf("accounts view after 401 = %+v", view)
	}
}

func TestBrowserRefreshTokenNeverUsed(t *testing.T) {
	var oauthCalls atomic.Int32
	ctrl, paths := newTestController(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth/token" {
			oauthCalls.Add(1)
		}
		http.NotFound(w, r)
	})
	seedBrowserAccount(t, ctrl, paths, "expired-browser-token", time.Now().Add(-time.Hour).Unix())

	err := ctrl.EnsureFreshToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), "sign in again") {
		t.Fatalf("expired browser token error = %v", err)
	}
	if oauthCalls.Load() != 0 {
		t.Fatalf("expired browser token called /v1/oauth/token %d times", oauthCalls.Load())
	}
	loaded, _, err := loadAccounts(accountsPath(paths))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Accounts[0].Browser == nil || !loaded.Accounts[0].Browser.ReauthRequired ||
		loaded.Accounts[0].Browser.Token != "expired-browser-token" {
		t.Fatalf("persisted browser token = %+v", loaded.Accounts[0].Browser)
	}
	if view := ctrl.Accounts(); len(view.Accounts) != 1 || !view.Accounts[0].ReauthRequired {
		t.Fatalf("accounts view = %+v", view)
	}
}

func TestSnapshotIncludesAccountsAndLogin(t *testing.T) {
	ctrl, _ := newTestController(t, multiLoginHandler(t))
	loginAs(t, ctrl, "first@example.com")
	loginAs(t, ctrl, "second@example.com")
	activeID := ctrl.Accounts().Active

	ls := ctrl.LoginStart(MethodPassword, "third@example.com", "")
	if ls.Step != "password" || ls.Method != "email" {
		t.Fatalf("login_start = %+v", ls)
	}
	st := ctrl.Snapshot()
	if st.Login == nil || st.Login.Step != "password" || st.Login.Method != "email" ||
		st.Login.Email != "third@example.com" {
		t.Fatalf("snapshot login = %+v", st.Login)
	}
	if len(st.Accounts) != 2 || st.ActiveAccount != activeID {
		t.Fatalf("snapshot accounts = %+v, active = %q", st.Accounts, st.ActiveAccount)
	}
	active := findAccountInView(api.AccountsView{Accounts: st.Accounts}, "second@example.com")
	if active == nil || !active.Active {
		t.Fatalf("second account = %+v", active)
	}
}

func TestAccountRotationSettings(t *testing.T) {
	ctrl, _ := newTestController(t, multiLoginHandler(t))
	if err := ctrl.SetAccountRotation(true, 0); err != nil {
		t.Fatal(err)
	}
	cfg := ctrl.store.EffectiveConfig()
	if !cfg.AccountRotation || cfg.RotationThreshold != 10 {
		t.Fatalf("rotation config = %+v", cfg)
	}
	if err := ctrl.SetAccountRotation(true, 25); err != nil {
		t.Fatal(err)
	}
	cfg = ctrl.store.EffectiveConfig()
	if cfg.RotationThreshold != 25 {
		t.Fatalf("threshold = %d", cfg.RotationThreshold)
	}
	if view := ctrl.Accounts(); !view.Rotation || view.Threshold != 25 {
		t.Fatalf("accounts view rotation = %+v", view)
	}
	for _, bad := range []int{-1, 51} {
		err := ctrl.SetAccountRotation(true, bad)
		var remote *api.RemoteError
		if !errors.As(err, &remote) || remote.Code != api.CodeBadRequest {
			t.Fatalf("threshold %d error = %v", bad, err)
		}
	}
	if cfg := ctrl.store.EffectiveConfig(); cfg.RotationThreshold != 25 {
		t.Fatalf("invalid threshold changed config: %+v", cfg)
	}
}

func TestPermanentRefreshDropsOnlyActiveAccount(t *testing.T) {
	ctrl, paths := newTestController(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth/token" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"errno":110,"message":"invalid refresh token"}`)
			return
		}
		http.NotFound(w, r)
	})
	now := time.Now()
	f := &accountsFile{
		Version:    2,
		Active:     "account-a",
		LastMethod: MethodPassword,
		Accounts: []*Account{
			{
				ID:         "account-a",
				Email:      "a@example.com",
				Label:      "A",
				Method:     MethodPassword,
				FxA:        &fxa.Tokens{AccessToken: "stale-a", RefreshToken: "refresh-a", ExpiresAt: now.Add(-time.Hour).Unix()},
				AddedAt:    now.Add(-time.Hour),
				LastUsedAt: now,
			},
			{
				ID:         "account-b",
				Email:      "b@example.com",
				Label:      "B",
				Method:     MethodPassword,
				FxA:        &fxa.Tokens{AccessToken: "access-b", RefreshToken: "refresh-b", ExpiresAt: now.Add(time.Hour).Unix()},
				AddedAt:    now.Add(-2 * time.Hour),
				LastUsedAt: now.Add(-time.Hour),
			},
		},
	}
	if err := saveAccounts(accountsPath(paths), f); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.EnsureFreshToken(context.Background()); err == nil {
		t.Fatal("expected the permanent refresh rejection")
	}
	st := ctrl.Snapshot()
	if st.State != string(StateIdle) || !st.Authenticated || st.ActiveAccount != "account-b" ||
		st.Email != "b@example.com" || len(st.Accounts) != 1 {
		t.Fatalf("snapshot after dropping active account = %+v", st)
	}
	tokens, err := loadTokens(paths.Tokens)
	if err != nil || tokens == nil || tokens.AccessToken != "access-b" {
		t.Fatalf("tokens.json after switch = %+v, err = %v", tokens, err)
	}
	loaded, _, err := loadAccounts(accountsPath(paths))
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Accounts) != 1 || loaded.Accounts[0].ID != "account-b" || loaded.Active != "account-b" {
		t.Fatalf("accounts.json after dropping active = %+v", loaded)
	}
}
