package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/benyamin-git/kumiho/internal/doctor"
)

func runDoctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit machine-readable JSON")
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: kumiho doctor [--json]\n\nRuns preflight diagnostics and prints remediation steps for failures.\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	report := doctor.Run()
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		renderReport(stdout, report)
	}
	return doctor.ExitCode(report)
}

func renderReport(w io.Writer, r doctor.Report) {
	fmt.Fprintf(w, "kumiho doctor - %s\n", r.Version)
	fmt.Fprintf(w, "host: %s (%s)\n", r.Host, r.OS)
	fmt.Fprintf(w, "time: %s\n\n", r.Time)

	for _, c := range r.Checks {
		fmt.Fprintf(w, "  %-4s %s: %s\n", statusLabel(c.Status), c.Name, c.Detail)
		if c.Remediation != "" && c.Status != doctor.OK && c.Status != doctor.Skip {
			fmt.Fprintf(w, "       -> %s\n", c.Remediation)
		}
	}

	failed, warned, skipped := doctor.Summarize(r.Checks)
	ok := len(r.Checks) - failed - warned - skipped
	fmt.Fprintf(w, "\nSummary: %d ok, %d warn, %d fail, %d skipped\n", ok, warned, failed, skipped)
	if failed > 0 {
		fmt.Fprint(w, "Some checks failed - follow the remediation steps above.\n")
	}
}

func statusLabel(s doctor.Status) string {
	switch s {
	case doctor.OK:
		return "OK"
	case doctor.Warn:
		return "WARN"
	case doctor.Fail:
		return "FAIL"
	case doctor.Skip:
		return "SKIP"
	default:
		return "?"
	}
}
