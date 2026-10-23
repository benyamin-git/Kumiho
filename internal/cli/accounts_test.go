package cli

import (
	"strings"
	"testing"

	"github.com/benyamin-git/kumiho/internal/api"
)

func int64Ptr(v int64) *int64 { return &v }

func TestResolveAccountRef(t *testing.T) {
	view := api.AccountsView{Accounts: []api.AccountSummary{
		{ID: "id-1", Email: "User@Example.com"},
		{ID: "id-2", Email: "other@example.com"},
		{ID: "id-3", Email: "dup@example.com"},
		{ID: "id-4", Email: "DUP@example.com"},
	}}

	got, err := resolveAccountRef(view, "id-2")
	if err != nil || got.ID != "id-2" {
		t.Fatalf("resolve exact id = %+v, err = %v", got, err)
	}
	got, err = resolveAccountRef(view, "user@example.COM")
	if err != nil || got.ID != "id-1" {
		t.Fatalf("resolve unique email = %+v, err = %v", got, err)
	}
	if _, err := resolveAccountRef(view, "missing@example.com"); err == nil ||
		!strings.Contains(err.Error(), "not found") {
		t.Fatalf("resolve unknown error = %v", err)
	}
	if _, err := resolveAccountRef(view, "dup@example.com"); err == nil ||
		!strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("resolve ambiguous error = %v", err)
	}
}

func TestFormatAccounts(t *testing.T) {
	view := api.AccountsView{
		Accounts: []api.AccountSummary{
			{
				ID: "id-1", Email: "user@example.com", Method: "browser", Active: true,
				Quota: &api.Quota{Limit: int64Ptr(100), Remaining: int64Ptr(80)},
			},
			{
				ID: "id-2", Email: "other@example.com", Label: "Work", Method: "password",
				ReauthRequired: true,
			},
		},
		Active:    "id-1",
		Rotation:  false,
		Threshold: 10,
	}
	got := formatAccounts(view)
	for _, want := range []string{
		"* user@example.com",
		"browser",
		"quota 80%",
		"password",
		"re-auth required",
		"quota unknown",
		"Rotation: off (threshold 10%)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatted accounts missing %q:\n%s", want, got)
		}
	}
}

func TestFormatAccountsRotationOn(t *testing.T) {
	got := formatAccounts(api.AccountsView{Rotation: true, Threshold: 20})
	if !strings.Contains(got, "Rotation: on (threshold 20%)") {
		t.Fatalf("rotation line = %q", got)
	}
}

func TestParseThreshold(t *testing.T) {
	for in, want := range map[string]int{"1": 1, "50": 50} {
		got, err := parseThreshold(in)
		if err != nil {
			t.Fatalf("parseThreshold(%q) error = %v", in, err)
		}
		if got != want {
			t.Fatalf("parseThreshold(%q) = %d, want %d", in, got, want)
		}
	}
	for _, in := range []string{"0", "51", "x", ""} {
		if _, err := parseThreshold(in); err == nil ||
			!strings.Contains(err.Error(), "between 1 and 50") {
			t.Fatalf("parseThreshold(%q) error = %v", in, err)
		}
	}
}
