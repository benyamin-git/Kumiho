package engine

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/fxa"
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
