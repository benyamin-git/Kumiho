package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSummarizeAndExitCodes(t *testing.T) {
	checks := []check{
		{Name: "a", Status: statusOK},
		{Name: "b", Status: statusWarn},
		{Name: "c", Status: statusFail},
		{Name: "d", Status: statusSkip},
		{Name: "e", Status: statusFail},
	}
	failed, warned, skipped := summarize(checks)
	if failed != 2 || warned != 1 || skipped != 1 {
		t.Fatalf("summarize = (%d,%d,%d), want (2,1,1)", failed, warned, skipped)
	}
	if got := exitCodeFor(failed); got != 1 {
		t.Errorf("exitCodeFor(2) = %d, want 1", got)
	}
	if got := exitCodeFor(0); got != 0 {
		t.Errorf("exitCodeFor(0) = %d, want 0", got)
	}
}

func TestParseCapEff(t *testing.T) {
	status := "Name:\tkumiho\nCapEff:\t0000000000001000\n"
	caps, ok := parseCapEff(status)
	if !ok {
		t.Fatal("parseCapEff did not find CapEff")
	}
	const capNetAdmin = 12
	if caps&(1<<capNetAdmin) == 0 {
		t.Fatalf("CapEff %#x does not include CAP_NET_ADMIN", caps)
	}
	if _, ok := parseCapEff("no capabilities here"); ok {
		t.Fatal("parseCapEff returned ok for content without CapEff")
	}
}

func TestDoctorJSONRoundTrip(t *testing.T) {
	checks := []check{
		{Name: "x", Status: statusOK, Detail: "fine"},
		{Name: "y", Status: statusFail, Detail: "broken", Remediation: "fix it"},
	}
	failed, warned, skipped := summarize(checks)
	report := doctorReport{
		Version: "dev",
		Host:    "host",
		OS:      "linux/amd64",
		Time:    "2026-10-05T00:00:00Z",
		Checks:  checks,
		Failed:  failed,
		Warned:  warned,
		Skipped: skipped,
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var decoded doctorReport
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Checks) != 2 || decoded.Failed != 1 {
		t.Fatalf("round trip mismatch: %+v", decoded)
	}
}

// names extracts the check names in report order; shared by the per-OS tests.
func names(checks []check) []string {
	out := make([]string, len(checks))
	for i, c := range checks {
		out[i] = c.Name
	}
	return out
}

// TestWithoutLinuxOnlyDropsLinuxChecks pins which checks are Linux-only and
// that filtering them preserves the report order of the rest.
func TestWithoutLinuxOnlyDropsLinuxChecks(t *testing.T) {
	in := []check{
		{Name: "platform"},
		{Name: "tun-device"},
		{Name: "cap-net-admin"},
		{Name: "nftables"},
		{Name: "systemd-resolved"},
		{Name: "dns-manager"},
		{Name: "tailscale"},
		{Name: "ipv6"},
		{Name: "default-route"},
		{Name: "socks-port"},
		{Name: "control-socket"},
		{Name: "clock-skew"},
		{Name: "endpoints"},
	}
	want := []string{"platform", "socks-port", "clock-skew", "endpoints"}
	got := names(withoutLinuxOnly(in))
	if len(got) != len(want) {
		t.Fatalf("withoutLinuxOnly kept %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("withoutLinuxOnly kept %v, want %v", got, want)
		}
	}
}

func TestRenderReport(t *testing.T) {
	var buf bytes.Buffer
	renderReport(&buf, doctorReport{
		Version: "dev",
		Host:    "host",
		OS:      "linux/amd64",
		Checks: []check{
			{Name: "x", Status: statusFail, Detail: "broken", Remediation: "fix it"},
			{Name: "y", Status: statusSkip, Detail: "n/a"},
		},
	})
	out := buf.String()
	for _, want := range []string{"FAIL", "x", "fix it", "SKIP", "1 fail", "1 skipped"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderReport output missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "Summary: 0 ok, 0 warn, 1 fail, 1 skipped") {
		t.Errorf("summary counts wrong:\n%s", out)
	}
}
