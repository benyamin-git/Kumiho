//go:build windows

package cli

import (
	"reflect"
	"testing"
)

// TestPlatformChecksSmoke locks the Windows check names and report order.
// Statuses and details are deliberately not asserted: they depend on the
// machine's service, driver and network state.
func TestPlatformChecksSmoke(t *testing.T) {
	wantNames := []string{
		"windows-service",
		"wintun-dll",
		"wfp-service",
		"control-pipe",
		"tun-adapter",
		"adapter-dns",
		"physical-default-route",
		"elevation",
	}
	checks := platformChecks()
	if len(checks) != len(wantNames) {
		t.Fatalf("platformChecks returned %d checks, want %d", len(checks), len(wantNames))
	}
	for i, c := range checks {
		if c.Name != wantNames[i] {
			t.Errorf("platformChecks[%d].Name = %q, want %q", i, c.Name, wantNames[i])
		}
		if c.Detail == "" {
			t.Errorf("platformChecks[%d] (%s) has an empty detail", i, c.Name)
		}
		switch c.Status {
		case statusOK, statusWarn, statusFail, statusSkip:
		default:
			t.Errorf("platformChecks[%d] (%s) has unknown status %q", i, c.Name, c.Status)
		}
	}
}

// TestPlatformChecksAppended verifies collectChecks keeps only the
// cross-platform checks first (Linux-only checks are dropped on Windows) and
// appends the platform checks, with no duplicate names.
func TestPlatformChecksAppended(t *testing.T) {
	all := collectChecks()
	platform := platformChecks()
	if len(all) < len(platform) {
		t.Fatalf("collectChecks returned %d checks, want at least %d", len(all), len(platform))
	}
	common := all[:len(all)-len(platform)]
	wantCommon := []string{"platform", "socks-port", "clock-skew", "endpoints"}
	if got := names(common); !reflect.DeepEqual(got, wantCommon) {
		t.Fatalf("common checks = %v, want %v (Linux-only checks must be dropped on Windows)", got, wantCommon)
	}
	if got := all[len(all)-len(platform):]; !reflect.DeepEqual(got, platform) {
		t.Fatalf("collectChecks tail = %+v, want platformChecks %+v", got, platform)
	}
	seen := make(map[string]bool, len(all))
	for _, c := range all {
		if seen[c.Name] {
			t.Errorf("duplicate check name %q", c.Name)
		}
		seen[c.Name] = true
	}
}

func TestCheckPlatformWindows(t *testing.T) {
	c := checkPlatform()
	if c.Status != statusOK || c.Detail != "windows (full tunnel supported)" {
		t.Fatalf("checkPlatform() = %+v, want ok/windows (full tunnel supported)", c)
	}
}
