package gui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/benyamin-git/kumiho/internal/doctor"
	"github.com/benyamin-git/kumiho/internal/version"
)

// mappedCheckNames is the independently written list of every check name
// internal/doctor can produce (cross-platform + Linux + Windows), so the
// friendly table cannot silently lose one. The list is pinned here instead
// of derived from a live doctor.Run() because that suite performs network
// probes (up to four 8 s requests) — unmapped names still render with their
// raw title, so the guard protects the copy, not the render.
var mappedCheckNames = []string{
	// Cross-platform.
	"platform", "socks-port", "clock-skew", "endpoints",
	// Linux.
	"tun-device", "cap-net-admin", "nftables", "systemd-resolved",
	"dns-manager", "tailscale", "ipv6", "default-route", "control-socket",
	// Windows.
	"windows-service", "wintun-dll", "wfp-service", "control-pipe",
	"tun-adapter", "adapter-dns", "physical-default-route", "elevation",
}

// TestFriendlyChecksCoverDoctorChecks asserts every known check name has a
// friendly title and a one-sentence explanation (the inventory's item 3
// list).
func TestFriendlyChecksCoverDoctorChecks(t *testing.T) {
	for _, name := range mappedCheckNames {
		f, ok := friendlyChecks[name]
		if !ok {
			t.Errorf("doctor check %q has no friendly mapping", name)
			continue
		}
		if f.title == "" || f.explanation == "" {
			t.Errorf("doctor check %q maps to an empty title or explanation", name)
		}
	}
}

// TestProjectDoctorMapsKnownChecks pins a known check's mapping: friendly
// title and explanation, chip word and tone, and the check's own detail.
func TestProjectDoctorMapsKnownChecks(t *testing.T) {
	view := projectDoctor(doctor.Report{Checks: []doctor.Check{
		{Name: "clock-skew", Status: doctor.OK, Detail: "within 1s of Mozilla time"},
	}})
	if len(view.Checks) != 1 {
		t.Fatalf("projectDoctor kept %d checks, want 1", len(view.Checks))
	}
	c := view.Checks[0]
	if c.Name != "clock-skew" || c.Title != "System clock" {
		t.Errorf("projection = (%q, %q), want the friendly System clock title", c.Name, c.Title)
	}
	if c.Explanation == "" || c.Status != "ok" || c.StatusWord != "OK" || c.Tone != toneGood {
		t.Errorf("projection = %+v, want an OK/good mapped check", c)
	}
	if c.Detail != "within 1s of Mozilla time" {
		t.Errorf("detail = %q, want the check detail", c.Detail)
	}
}

// TestProjectDoctorPassesUnknownChecksThrough pins the passthrough contract:
// a check name without a mapping keeps its raw name as the title, its own
// status (uppercased word), detail and remediation, and never disappears.
func TestProjectDoctorPassesUnknownChecksThrough(t *testing.T) {
	view := projectDoctor(doctor.Report{Checks: []doctor.Check{
		{Name: "brand-new-check", Status: doctor.Warn, Detail: "something happened", Remediation: "do the thing"},
	}})
	if len(view.Checks) != 1 {
		t.Fatalf("projectDoctor dropped an unmapped check: kept %d, want 1", len(view.Checks))
	}
	c := view.Checks[0]
	if c.Title != "brand-new-check" || c.Explanation != "" {
		t.Errorf("unmapped item = (%q, %q), want the raw name passed through", c.Title, c.Explanation)
	}
	if c.Status != "warn" || c.StatusWord != "Warning" || c.Tone != toneWarn {
		t.Errorf("unmapped chip = (%s, %s, %s), want (warn, Warning, warn)", c.Status, c.StatusWord, c.Tone)
	}
	if c.Detail != "something happened" || c.Remediation != "do the thing" {
		t.Errorf("unmapped item lost its detail or remediation: %+v", c)
	}
}

// TestProjectDoctorUnknownStatus pins the unmapped-status fallback: the word
// is the uppercased raw status and the tone is neutral.
func TestProjectDoctorUnknownStatus(t *testing.T) {
	view := projectDoctor(doctor.Report{Checks: []doctor.Check{
		{Name: "mystery", Status: doctor.Status("weird"), Detail: "d"},
	}})
	c := view.Checks[0]
	if c.StatusWord != "WEIRD" || c.Tone != "" {
		t.Fatalf("chip = (%q, %q), want the uppercased raw status with a neutral tone", c.StatusWord, c.Tone)
	}
}

// TestDoctorChipWords pins the four chip words and their tones (inventory
// §Diagnostics).
func TestDoctorChipWords(t *testing.T) {
	tests := []struct {
		status doctor.Status
		word   string
		tone   string
	}{
		{doctor.OK, "OK", toneGood},
		{doctor.Warn, "Warning", toneWarn},
		{doctor.Fail, "Failed", toneBad},
		{doctor.Skip, "Skipped", ""},
	}
	for _, tt := range tests {
		word, tone := doctorChip(tt.status)
		if word != tt.word || tone != tt.tone {
			t.Errorf("doctorChip(%s) = (%q, %q), want (%q, %q)", tt.status, word, tone, tt.word, tt.tone)
		}
	}
}

// TestProjectDoctorPreservesReportOrder guards that a mapped and an unmapped
// check never reorder their report.
func TestProjectDoctorPreservesReportOrder(t *testing.T) {
	view := projectDoctor(doctor.Report{Checks: []doctor.Check{
		{Name: "endpoints", Status: doctor.OK},
		{Name: "mystery", Status: doctor.Skip},
		{Name: "platform", Status: doctor.OK},
	}})
	if len(view.Checks) != 3 {
		t.Fatalf("projectDoctor kept %d checks, want 3", len(view.Checks))
	}
	for i, want := range []string{"endpoints", "mystery", "platform"} {
		if view.Checks[i].Name != want {
			t.Errorf("check %d = %q, want %q", i, view.Checks[i].Name, want)
		}
	}
}

// TestDoctorDetailsPayload pins the Copy details text: `Kumiho <version>
// diagnostics`, a blank line, the pretty-printed full report JSON and a
// trailing newline (inventory §Diagnostics).
func TestDoctorDetailsPayload(t *testing.T) {
	report := doctor.Report{
		Version: "kumiho v0.3.0 (commit abc, built today)",
		Host:    "test-host",
		OS:      "linux/amd64",
		Time:    "2026-10-16T00:00:00Z",
		Checks:  []doctor.Check{{Name: "platform", Status: doctor.OK, Detail: "ok"}},
	}
	got := doctorDetails(report)
	header := "Kumiho " + version.Version + " diagnostics"
	if !strings.HasPrefix(got, header+"\n\n") || !strings.HasSuffix(got, "\n") {
		t.Fatalf("details = %q, want the %q header, a blank line and a trailing newline", got, header)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(got, header+"\n\n"), "\n")
	var back doctor.Report
	if err := json.Unmarshal([]byte(body), &back); err != nil {
		t.Fatalf("details body is not the report JSON: %v", err)
	}
	if back.Host != "test-host" || len(back.Checks) != 1 || back.Checks[0].Name != "platform" {
		t.Fatalf("round-tripped report = %+v, want the full report", back)
	}
	if !strings.Contains(body, "\n  ") {
		t.Fatalf("details body is not pretty-printed: %q", body)
	}
}
