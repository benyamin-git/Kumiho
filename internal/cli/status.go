package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
)

func runStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit machine-readable JSON")
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: kumiho status [--json]\n\nShows the daemon status snapshot.\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	client, code := dialDaemon(stderr)
	if client == nil {
		return code
	}
	defer client.Close()

	ctx, cancel := callCtx()
	defer cancel()
	st, err := client.Status(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "kumiho status: %v\n", err)
		return 1
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(st); err != nil {
			fmt.Fprintf(stderr, "kumiho status: %v\n", err)
			return 1
		}
		return 0
	}

	printStatus(stdout, st)
	return 0
}

// printStatus renders the human-readable status snapshot.
func printStatus(w io.Writer, st api.Status) {
	fmt.Fprintf(w, "state:         %s\n", st.State)
	if st.Email != "" {
		fmt.Fprintf(w, "account:       %s\n", st.Email)
	}
	if st.TokenExpires > 0 {
		fmt.Fprintf(w, "token expires: %s\n", time.Unix(st.TokenExpires, 0).Format(time.RFC3339))
	}
	if st.Location != "" {
		loc := st.Location
		if st.Server != "" {
			loc += " (" + st.Server + ")"
		}
		fmt.Fprintf(w, "location:      %s\n", loc)
	}
	if st.PassExpires > 0 {
		fmt.Fprintf(w, "pass expires:  %s\n", time.Unix(st.PassExpires, 0).Format(time.RFC3339))
	}
	if q := st.Quota; q != nil && (q.Remaining != nil || q.Limit != nil) {
		line := "quota:         "
		switch {
		case q.Remaining != nil && q.Limit != nil:
			line += fmt.Sprintf("%s remaining of %s", humanBytes(*q.Remaining), humanBytes(*q.Limit))
		case q.Remaining != nil:
			line += fmt.Sprintf("%s remaining", humanBytes(*q.Remaining))
		default:
			line += humanBytes(*q.Limit)
		}
		if q.Reset != nil {
			line += fmt.Sprintf(" (resets %s)", time.Unix(*q.Reset, 0).Format(time.RFC3339))
		}
		fmt.Fprintln(w, line)
	}
	if st.ExitIP != "" {
		exit := st.ExitIP
		if st.ExitCountry != "" {
			exit += " (" + st.ExitCountry + ")"
		}
		fmt.Fprintf(w, "exit ip:       %s\n", exit)
	}
	if st.Totals != nil {
		fmt.Fprintf(w, "totals:        up %s / down %s\n", humanBytes(st.Totals.Up), humanBytes(st.Totals.Down))
	}
	if st.Rates != nil && (st.Rates.Up > 0 || st.Rates.Down > 0) {
		fmt.Fprintf(w, "rates:         up %s/s / down %s/s\n", humanBytes(int64(st.Rates.Up)), humanBytes(int64(st.Rates.Down)))
	}
	fmt.Fprintf(w, "kill switch:   %s\n", onoff(st.KillSwitch))
	fmt.Fprintf(w, "autoconnect:   %s\n", onoff(st.Autoconnect))
	if st.Since > 0 {
		fmt.Fprintf(w, "state since:   %s\n", time.Unix(st.Since, 0).Format(time.RFC3339))
	}
}

// humanBytes formats a byte count with binary units.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func runLogout(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("logout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: kumiho logout\n\nSigns out and clears stored tokens.\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	client, code := dialDaemon(stderr)
	if client == nil {
		return code
	}
	defer client.Close()

	ctx, cancel := callCtx()
	defer cancel()
	if _, err := client.Call(ctx, ipc.TypeLogout, nil); err != nil {
		fmt.Fprintf(stderr, "kumiho logout: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "Signed out.")
	return 0
}

func onoff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}
