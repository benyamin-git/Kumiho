package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
)

const accountsUsage = `Usage:
  kumiho accounts [list]
  kumiho accounts add [--browser|--password] [--email <email>] [--account <id|email>] [--no-open]
  kumiho accounts use <id|email>
  kumiho accounts remove <id|email> [--yes]
  kumiho accounts rename <id|email> <label>
  kumiho accounts rotation on|off
  kumiho accounts threshold <1-50>
`

func runAccounts(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return runAccountsList(args, stdout, stderr)
	}
	switch args[0] {
	case "list":
		return runAccountsList(args[1:], stdout, stderr)
	case "add":
		return runSignIn(args[1:], true, stdout, stderr)
	case "use":
		return runAccountsUse(args[1:], stdout, stderr)
	case "remove":
		return runAccountsRemove(args[1:], stdout, stderr)
	case "rename":
		return runAccountsRename(args[1:], stdout, stderr)
	case "rotation":
		return runAccountsRotation(args[1:], stdout, stderr)
	case "threshold":
		return runAccountsThreshold(args[1:], stdout, stderr)
	case "help", "--help", "-h":
		fmt.Fprint(stdout, accountsUsage)
		return 0
	default:
		fmt.Fprintf(stderr, "kumiho accounts: unknown subcommand %q\n\n%s", args[0], accountsUsage)
		return 2
	}
}

func runAccountsList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("accounts list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, accountsUsage)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, accountsUsage)
		return 2
	}

	client, code := dialDaemon(stderr)
	if client == nil {
		return code
	}
	defer client.Close()
	ctx, cancel := callCtx()
	defer cancel()

	view, err := fetchAccounts(ctx, client)
	if err != nil {
		fmt.Fprintf(stderr, "kumiho accounts: %v\n", err)
		return 1
	}
	fmt.Fprint(stdout, formatAccounts(view))
	if view.Error != "" {
		return 1
	}
	return 0
}

func runAccountsUse(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("accounts use", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: kumiho accounts use <id|email>\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	client, code := dialDaemon(stderr)
	if client == nil {
		return code
	}
	defer client.Close()
	ctx, cancel := callCtx()
	defer cancel()

	view, err := fetchAccounts(ctx, client)
	if err != nil {
		fmt.Fprintf(stderr, "kumiho accounts use: %v\n", err)
		return 1
	}
	target, err := resolveAccountRef(view, fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "kumiho accounts use: %v\n", err)
		return 1
	}
	if _, err := client.Call(ctx, ipc.TypeAccountsUse, api.AccountsUsePayload{ID: target.ID}); err != nil {
		fmt.Fprintf(stderr, "kumiho accounts use: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Active account: %s.\n", target.Email)
	return 0
}

func runAccountsRemove(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("accounts remove", flag.ContinueOnError)
	fs.SetOutput(stderr)
	yes := fs.Bool("yes", false, "remove without confirmation")
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: kumiho accounts remove <id|email> [--yes]\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	client, code := dialDaemon(stderr)
	if client == nil {
		return code
	}
	defer client.Close()
	ctx, cancel := callCtx()
	defer cancel()

	view, err := fetchAccounts(ctx, client)
	if err != nil {
		fmt.Fprintf(stderr, "kumiho accounts remove: %v\n", err)
		return 1
	}
	target, err := resolveAccountRef(view, fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "kumiho accounts remove: %v\n", err)
		return 1
	}
	if target.Active && len(view.Accounts) > 1 && !*yes {
		fmt.Fprintf(stdout, "Remove the active account %s? [y/N]: ", target.Email)
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && len(line) == 0 {
			fmt.Fprintf(stderr, "kumiho accounts remove: %v\n", err)
			return 1
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
		default:
			fmt.Fprintln(stdout, "Cancelled.")
			return 0
		}
	}
	if _, err := client.Call(ctx, ipc.TypeAccountsRemove, api.AccountsRemovePayload{ID: target.ID}); err != nil {
		fmt.Fprintf(stderr, "kumiho accounts remove: %v\n", err)
		return 1
	}

	after, err := fetchAccounts(ctx, client)
	if err != nil {
		fmt.Fprintf(stdout, "Removed %s.\n", target.Email)
		return 0
	}
	active := ""
	if after.Active != "" {
		active = "an account"
		for _, a := range after.Accounts {
			if a.ID == after.Active {
				active = a.Email
				break
			}
		}
	}
	fmt.Fprint(stdout, removedAccountCopy(target.Email, active))
	return 0
}

// removedAccountCopy is the post-remove wording. Removing the last account is
// not a logout: it clears the local session and disconnects, but performs no
// refresh-token revocation, so the copy says exactly that.
func removedAccountCopy(email, active string) string {
	if active == "" {
		return fmt.Sprintf("Removed %s; no accounts remain, so the local session was cleared.\n", email)
	}
	return fmt.Sprintf("Removed %s; active account is %s.\n", email, active)
}

func runAccountsRename(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("accounts rename", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: kumiho accounts rename <id|email> <label>\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() < 2 {
		fs.Usage()
		return 2
	}
	ref := fs.Arg(0)
	label := strings.TrimSpace(strings.Join(fs.Args()[1:], " "))
	if label == "" {
		fmt.Fprintln(stderr, "kumiho accounts rename: label is required")
		return 2
	}

	client, code := dialDaemon(stderr)
	if client == nil {
		return code
	}
	defer client.Close()
	ctx, cancel := callCtx()
	defer cancel()

	view, err := fetchAccounts(ctx, client)
	if err != nil {
		fmt.Fprintf(stderr, "kumiho accounts rename: %v\n", err)
		return 1
	}
	target, err := resolveAccountRef(view, ref)
	if err != nil {
		fmt.Fprintf(stderr, "kumiho accounts rename: %v\n", err)
		return 1
	}
	if _, err := client.Call(ctx, ipc.TypeAccountsUpdate, api.AccountsUpdatePayload{ID: target.ID, Label: label}); err != nil {
		fmt.Fprintf(stderr, "kumiho accounts rename: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Renamed %s to %q.\n", target.Email, label)
	return 0
}

func runAccountsRotation(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("accounts rotation", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: kumiho accounts rotation on|off\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	var enabled bool
	switch strings.ToLower(strings.TrimSpace(fs.Arg(0))) {
	case "on":
		enabled = true
	case "off":
		enabled = false
	default:
		fs.Usage()
		return 2
	}

	client, code := dialDaemon(stderr)
	if client == nil {
		return code
	}
	defer client.Close()
	ctx, cancel := callCtx()
	defer cancel()

	if _, err := client.Call(ctx, ipc.TypeAccountsSetRotation, api.AccountsRotationPayload{Enabled: enabled}); err != nil {
		fmt.Fprintf(stderr, "kumiho accounts rotation: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Rotation %s.\n", onoff(enabled))
	return 0
}

func runAccountsThreshold(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("accounts threshold", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: kumiho accounts threshold <1-50>\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	threshold, err := parseThreshold(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "kumiho accounts threshold: %v\n", err)
		return 2
	}

	client, code := dialDaemon(stderr)
	if client == nil {
		return code
	}
	defer client.Close()
	ctx, cancel := callCtx()
	defer cancel()

	view, err := fetchAccounts(ctx, client)
	if err != nil {
		fmt.Fprintf(stderr, "kumiho accounts threshold: %v\n", err)
		return 1
	}
	if _, err := client.Call(ctx, ipc.TypeAccountsSetRotation, api.AccountsRotationPayload{
		Enabled: view.Rotation, Threshold: threshold,
	}); err != nil {
		fmt.Fprintf(stderr, "kumiho accounts threshold: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Rotation threshold set to %d%% (rotation %s).\n", threshold, onoff(view.Rotation))
	return 0
}

func fetchAccounts(ctx context.Context, client *ipc.Client) (api.AccountsView, error) {
	env, err := client.Call(ctx, ipc.TypeAccountsList, nil)
	if err != nil {
		return api.AccountsView{}, err
	}
	var view api.AccountsView
	if err := env.DecodePayload(&view); err != nil {
		return api.AccountsView{}, err
	}
	return view, nil
}

func resolveAccountRef(view api.AccountsView, ref string) (api.AccountSummary, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return api.AccountSummary{}, errors.New("account id or email is required")
	}
	for _, account := range view.Accounts {
		if account.ID == ref {
			return account, nil
		}
	}
	var matches []api.AccountSummary
	for _, account := range view.Accounts {
		if strings.EqualFold(account.Email, ref) {
			matches = append(matches, account)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return api.AccountSummary{}, fmt.Errorf("account %q not found", ref)
	default:
		return api.AccountSummary{}, fmt.Errorf("account email %q is ambiguous; use the account id", ref)
	}
}

func formatAccounts(view api.AccountsView) string {
	var b strings.Builder
	if len(view.Accounts) == 0 {
		b.WriteString("No accounts.\n")
	}
	for _, account := range view.Accounts {
		marker := " "
		if account.Active {
			marker = "*"
		}
		name := account.Email
		if account.Label != "" && !strings.EqualFold(account.Label, account.Email) {
			name = fmt.Sprintf("%s (%s)", account.Email, account.Label)
		}
		method := account.Method
		if method == "" {
			method = "unknown"
		}
		fmt.Fprintf(&b, "%s %s  %s  %s", marker, name, method, formatQuota(account.Quota))
		if account.ReauthRequired {
			b.WriteString("  re-auth required")
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "Rotation: %s (threshold %d%%)\n", onoff(view.Rotation), view.Threshold)
	if view.Notice != "" {
		fmt.Fprintf(&b, "Notice: %s\n", view.Notice)
	}
	if view.Error != "" {
		fmt.Fprintf(&b, "Error: %s\n", view.Error)
	}
	return b.String()
}

func formatQuota(q *api.Quota) string {
	if q == nil || q.Remaining == nil {
		return "quota unknown"
	}
	if *q.Remaining <= 0 {
		return "quota exhausted"
	}
	if q.Limit == nil || *q.Limit <= 0 {
		return "quota unknown"
	}
	percent := *q.Remaining * 100 / *q.Limit
	if percent > 100 {
		percent = 100
	}
	return fmt.Sprintf("quota %d%%", percent)
}

func parseThreshold(raw string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 || n > 50 {
		return 0, errors.New("rotation threshold must be between 1 and 50")
	}
	return n, nil
}
