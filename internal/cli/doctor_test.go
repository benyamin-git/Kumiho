package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/benyamin-git/kumiho/internal/doctor"
)

func TestSummarizeAndExitCodes(t *testing.T) {
	checks := []doctor.Check{
		{Name: "a", Status: doctor.OK},
		{Name: "b", Status: doctor.Warn},
		{Name: "c", Status: doctor.Fail},
		{Name: "d", Status: doctor.Skip},
		{Name: "e", Status: doctor.Fail},
	}
	failed, warned, skipped := doctor.Summarize(checks)
	if failed != 2 || warned != 1 || skipped != 1 {
		t.Fatalf("summarize = (%d,%d,%d), want (2,1,1)", failed, warned, skipped)
	}
	if got := doctor.ExitCode(doctor.Report{Failed: failed}); got != 1 {
		t.Errorf("ExitCode(failed=2) = %d, want 1", got)
	}
	if got := doctor.ExitCode(doctor.Report{}); got != 0 {
		t.Errorf("ExitCode(failed=0) = %d, want 0", got)
	}
}

func TestDoctorJSONRoundTrip(t *testing.T) {
	checks := []doctor.Check{
		{Name: "x", Status: doctor.OK, Detail: "fine"},
		{Name: "y", Status: doctor.Fail, Detail: "broken", Remediation: "fix it"},
	}
	failed, warned, skipped := doctor.Summarize(checks)
	report := doctor.Report{
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

	var decoded doctor.Report
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Checks) != 2 || decoded.Failed != 1 {
		t.Fatalf("round trip mismatch: %+v", decoded)
	}
}

func TestRenderReport(t *testing.T) {
	var buf bytes.Buffer
	renderReport(&buf, doctor.Report{
		Version: "dev",
		Host:    "host",
		OS:      "linux/amd64",
		Checks: []doctor.Check{
			{Name: "x", Status: doctor.Fail, Detail: "broken", Remediation: "fix it"},
			{Name: "y", Status: doctor.Skip, Detail: "n/a"},
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
