package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
	"golang.org/x/term"
)

const (
	browserStartTimeout    = 10 * time.Second
	browserCompleteTimeout = 60 * time.Second
	browserStatusTimeout   = 10 * time.Second
	browserCancelTimeout   = 5 * time.Second
	browserSignInTimeout   = 10 * time.Minute
	browserPollInterval    = 2 * time.Second

	browserRedirectInstruction = "After signing in, copy the complete redirect URL from the browser's address bar and paste it here."
)

func runLogin(args []string, stdout, stderr io.Writer) int {
	return runSignIn(args, false, stdout, stderr)
}

// runSignIn implements both `kumiho login` and `kumiho accounts add`; the
// adding flag only selects the accounts_add_* IPC aliases and success wording.
func runSignIn(args []string, adding bool, stdout, stderr io.Writer) int {
	command := "login"
	if adding {
		command = "accounts add"
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	useBrowser := fs.Bool("browser", false, "sign in through the browser")
	usePassword := fs.Bool("password", false, "sign in with email and password")
	email := fs.String("email", "", "Firefox Account email")
	noOpen := fs.Bool("no-open", false, "print the browser URL without opening it")
	accountRef := fs.String("account", "", "account id or email to re-authenticate (accounts add)")
	fs.Usage = func() {
		fmt.Fprintf(stderr, `Usage: kumiho %s [--browser|--password] [--email <email>] [--no-open]

Signs in to Firefox Accounts through the daemon. With no method flag and a
terminal, the last method is preselected (browser on first run); without a
terminal the password flow is used for script compatibility.
`, command)
		if adding {
			fmt.Fprintln(stderr, "\n  --account <id|email>  re-authenticate an existing account")
		}
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "kumiho %s: unexpected argument %q\n", command, fs.Arg(0))
		return 2
	}
	if *useBrowser && *usePassword {
		fmt.Fprintf(stderr, "kumiho %s: choose --browser or --password, not both\n", command)
		return 2
	}

	client, code := dialDaemon(stderr)
	if client == nil {
		return code
	}
	defer client.Close()
	ctx, cancel := callCtx()
	defer cancel()

	var view *api.AccountsView
	loadAccounts := func() (*api.AccountsView, error) {
		if view != nil {
			return view, nil
		}
		loaded, err := fetchAccounts(ctx, client)
		if err != nil {
			return nil, err
		}
		view = &loaded
		return view, nil
	}

	var accountID string
	if strings.TrimSpace(*accountRef) != "" {
		loaded, err := loadAccounts()
		if err != nil {
			fmt.Fprintf(stderr, "kumiho %s: %v\n", command, err)
			return 1
		}
		target, err := resolveAccountRef(*loaded, *accountRef)
		if err != nil {
			fmt.Fprintf(stderr, "kumiho %s: %v\n", command, err)
			return 1
		}
		accountID = target.ID
		if *email == "" {
			*email = target.Email
		}
	}

	interactive := term.IsTerminal(int(os.Stdin.Fd()))
	method := ""
	switch {
	case *useBrowser:
		method = "browser"
	case *usePassword:
		method = "password"
	case accountID != "" && view != nil:
		for _, account := range view.Accounts {
			if account.ID == accountID && (account.Method == "browser" || account.Method == "password") {
				method = account.Method
				break
			}
		}
	}
	if method == "" && interactive {
		loaded, err := loadAccounts()
		if err != nil {
			// The daemon may predate accounts_list; fall back to browser.
			loaded = &api.AccountsView{}
		}
		chosen, err := promptLoginMethod(newLoginTerminal(), loaded.LastMethod)
		if err != nil {
			fmt.Fprintf(stderr, "kumiho %s: %v\n", command, err)
			return 2
		}
		method = chosen
	}
	if method == "" {
		method = "password"
	}

	startType, statusType, cancelType := ipc.TypeLoginStart, ipc.TypeLoginStatus, ipc.TypeLoginCancel
	if adding {
		startType, statusType, cancelType = ipc.TypeAccountsAddStart, ipc.TypeAccountsAddStatus, ipc.TypeAccountsAddCancel
	}
	if method == "browser" {
		return runBrowserSignIn(client, startType, statusType, cancelType, *email, accountID, *noOpen, adding, command, os.Stdin, stdout, stderr)
	}
	return runPasswordSignIn(ctx, client, startType, *email, accountID, adding, command, stdout, stderr)
}

func runPasswordSignIn(ctx context.Context, client *ipc.Client, startType, email, accountID string, adding bool, command string, stdout, stderr io.Writer) int {
	tty := newLoginTerminal()
	if email == "" {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Fprintf(stderr, "kumiho %s: --email is required without a terminal\n", command)
			return 2
		}
		tty.SetPrompt("Email: ")
		addr, err := tty.ReadLine()
		if err != nil {
			fmt.Fprintf(stderr, "kumiho %s: %v\n", command, err)
			return 1
		}
		email = addr
	}

	ls := loginStartCall(ctx, client, startType, api.LoginStartPayload{
		Method: "password", Email: email, AccountID: accountID,
	}, stderr, command)
	if ls == nil {
		return 1
	}

	for attempt := 0; attempt < 12; attempt++ {
		if ls.Error != "" {
			fmt.Fprintf(stderr, "%s: %s\n", command, ls.Error)
		}
		switch ls.Step {
		case "password":
			tty.SetPrompt("Password: ")
			password, err := tty.ReadPassword("Password: ")
			if err != nil {
				fmt.Fprintf(stderr, "kumiho %s: %v\n", command, err)
				return 1
			}
			ls = loginCall(ctx, client, ipc.TypeLoginPassword, api.LoginPasswordPayload{Password: password}, stderr, command)

		case "2fa":
			tty.SetPrompt(loginCodePrompt(ls))
			line, err := tty.ReadLine()
			if err != nil {
				fmt.Fprintf(stderr, "kumiho %s: %v\n", command, err)
				return 1
			}
			ls = loginCall(ctx, client, ipc.TypeLogin2FA, api.Login2FAPayload{Code: line}, stderr, command)

		case "email":
			tty.SetPrompt("Email: ")
			addr, err := tty.ReadLine()
			if err != nil {
				fmt.Fprintf(stderr, "kumiho %s: %v\n", command, err)
				return 1
			}
			ls = loginStartCall(ctx, client, startType, api.LoginStartPayload{
				Method: "password", Email: addr, AccountID: accountID,
			}, stderr, command)

		case "done":
			if ls.Error != "" {
				return 1
			}
			printSignInDone(stdout, adding, ls.Email)
			return 0

		case "error", "cancelled":
			if ls.Error != "" {
				fmt.Fprintf(stderr, "kumiho %s: %s\n", command, ls.Error)
			}
			return 1

		default:
			fmt.Fprintf(stderr, "kumiho %s: unexpected login step %q\n", command, ls.Step)
			return 1
		}
		if ls == nil {
			return 1
		}
	}
	fmt.Fprintf(stderr, "kumiho %s: too many attempts\n", command)
	return 1
}

func runBrowserSignIn(client *ipc.Client, startType, statusType, cancelType, email, accountID string, noOpen, adding bool, command string, in io.Reader, stdout, stderr io.Writer) int {
	startCtx, cancelStart := context.WithTimeout(context.Background(), browserStartTimeout)
	ls := loginStartCall(startCtx, client, startType, api.LoginStartPayload{
		Method: "browser", Email: email, AccountID: accountID,
	}, stderr, command)
	cancelStart()
	if ls == nil {
		return 1
	}
	switch ls.Step {
	case "done":
		if ls.Error != "" {
			fmt.Fprintf(stderr, "kumiho %s: %s\n", command, ls.Error)
			return 1
		}
		printSignInDone(stdout, adding, ls.Email)
		return 0
	case "cancelled":
		fmt.Fprintln(stderr, "Sign-in cancelled.")
		return 1
	case "browser":
	default:
		if ls.Error != "" {
			fmt.Fprintf(stderr, "kumiho %s: %s\n", command, ls.Error)
		} else {
			fmt.Fprintf(stderr, "kumiho %s: unexpected login step %q\n", command, ls.Step)
		}
		return 1
	}
	if ls.URL == "" {
		fmt.Fprintf(stderr, "kumiho %s: daemon returned no browser URL\n", command)
		return 1
	}
	if ls.Error != "" {
		fmt.Fprintf(stderr, "%s: %s\n", command, ls.Error)
	}
	fmt.Fprintf(stdout, "Open this URL to sign in:\n%s\n", ls.URL)
	if !noOpen {
		_ = openBrowserURL(ls.URL)
	}
	fmt.Fprintln(stdout, browserRedirectInstruction)

	// The daemon owns a ten-minute flow; the CLI stops at the same deadline.
	deadline := time.Now().Add(browserSignInTimeout)
	reader := bufio.NewReader(in)
	for {
		if !time.Now().Before(deadline) {
			cancelBrowserLogin(client, cancelType, ls.LoginID)
			fmt.Fprintf(stderr, "kumiho %s: timed out waiting for the browser sign-in\n", command)
			return 1
		}
		line, err := promptBrowserRedirect(reader, stdout)
		if err != nil {
			cancelBrowserLogin(client, cancelType, ls.LoginID)
			if errors.Is(err, io.EOF) {
				fmt.Fprintf(stderr, "kumiho %s: no redirect URL was pasted\n", command)
			} else {
				fmt.Fprintf(stderr, "kumiho %s: %v\n", command, err)
			}
			return 1
		}
		if line == "" {
			fmt.Fprintf(stderr, "kumiho %s: paste the complete redirect URL from the browser\n", command)
			continue
		}
		if !time.Now().Before(deadline) {
			cancelBrowserLogin(client, cancelType, ls.LoginID)
			fmt.Fprintf(stderr, "kumiho %s: timed out waiting for the browser sign-in\n", command)
			return 1
		}

		// A nil reply means the daemon reached a terminal state (done or
		// error); a returned error is reconciled with login_status because
		// the flow may already be finished or gone (lost reply, cancel,
		// restart, or a client-side timeout while the daemon completed).
		completeCtx, cancelComplete := context.WithTimeout(context.Background(), boundedTimeout(deadline, browserCompleteTimeout))
		_, err = client.Call(completeCtx, ipc.TypeLoginComplete, api.LoginCompletePayload{
			LoginID: ls.LoginID, RedirectURL: line,
		})
		cancelComplete()
		if err != nil {
			retry, code := reconcileBrowserCompleteError(client, statusType, ls.LoginID, command, adding, stdout, stderr)
			if retry {
				fmt.Fprintf(stderr, "kumiho %s: %v\n", command, err)
				continue
			}
			return code
		}
		return pollBrowserSignIn(client, statusType, cancelType, ls.LoginID, command, deadline, adding, stdout, stderr)
	}
}

// promptBrowserRedirect prints the input prompt and returns one trimmed line.
// EOF with no input is reported as io.EOF; a partial final line is accepted.
func promptBrowserRedirect(reader *bufio.Reader, out io.Writer) (string, error) {
	fmt.Fprint(out, "Redirect URL: ")
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" && errors.Is(err, io.EOF) {
		return "", io.EOF
	}
	return line, nil
}

// reconcileBrowserCompleteError checks login_status once after a failed
// login_complete. A finished flow wins over the client-side error (a lost
// reply, timeout, cancel or restart); retry reports whether the flow is still
// active and the caller should prompt again.
func reconcileBrowserCompleteError(client *ipc.Client, statusType, loginID, command string, adding bool, stdout, stderr io.Writer) (retry bool, code int) {
	ls, ok := callBrowserStatus(client, statusType, loginID, command, stderr)
	if !ok {
		return false, 1
	}
	active, code := reportBrowserStatus(ls, command, adding, stdout, stderr)
	if active {
		return true, 0
	}
	return false, code
}

// pollBrowserSignIn waits for the daemon to publish the terminal state after
// a successful login_complete. It polls immediately because LoginComplete
// publishes done/error before it returns.
func pollBrowserSignIn(client *ipc.Client, statusType, cancelType, loginID, command string, deadline time.Time, adding bool, stdout, stderr io.Writer) int {
	for {
		ls, ok := callBrowserStatus(client, statusType, loginID, command, stderr)
		if !ok {
			return 1
		}
		active, code := reportBrowserStatus(ls, command, adding, stdout, stderr)
		if !active {
			return code
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			cancelBrowserLogin(client, cancelType, loginID)
			fmt.Fprintf(stderr, "kumiho %s: timed out waiting for the browser sign-in\n", command)
			return 1
		}
		wait := browserPollInterval
		if remaining < wait {
			wait = remaining
		}
		time.Sleep(wait)
	}
}

// callBrowserStatus fetches one login_status state. It reports false after
// printing a transport or payload error.
func callBrowserStatus(client *ipc.Client, statusType, loginID, command string, stderr io.Writer) (api.LoginState, bool) {
	statusCtx, cancel := context.WithTimeout(context.Background(), browserStatusTimeout)
	defer cancel()
	env, err := client.Call(statusCtx, statusType, api.LoginStatusPayload{LoginID: loginID})
	if err != nil {
		fmt.Fprintf(stderr, "kumiho %s: %v\n", command, err)
		return api.LoginState{}, false
	}
	var ls api.LoginState
	if err := env.DecodePayload(&ls); err != nil {
		fmt.Fprintf(stderr, "kumiho %s: %v\n", command, err)
		return api.LoginState{}, false
	}
	return ls, true
}

// reportBrowserStatus prints the result of one status state. It reports true
// while the flow is still active; otherwise the exit code is final.
func reportBrowserStatus(ls api.LoginState, command string, adding bool, stdout, stderr io.Writer) (active bool, code int) {
	switch ls.Step {
	case "browser":
		return true, 0
	case "done":
		if ls.Error != "" {
			fmt.Fprintf(stderr, "kumiho %s: %s\n", command, ls.Error)
			return false, 1
		}
		printSignInDone(stdout, adding, ls.Email)
		return false, 0
	case "cancelled":
		fmt.Fprintln(stderr, "Sign-in cancelled.")
		return false, 1
	case "error":
		if ls.Error != "" {
			fmt.Fprintf(stderr, "kumiho %s: %s\n", command, ls.Error)
		} else {
			fmt.Fprintf(stderr, "kumiho %s: browser sign-in failed\n", command)
		}
		return false, 1
	default:
		if ls.Error != "" {
			fmt.Fprintf(stderr, "kumiho %s: %s\n", command, ls.Error)
			return false, 1
		}
		fmt.Fprintf(stderr, "kumiho %s: unexpected login step %q\n", command, ls.Step)
		return false, 1
	}
}

func cancelBrowserLogin(client *ipc.Client, cancelType, loginID string) {
	ctx, cancel := context.WithTimeout(context.Background(), browserCancelTimeout)
	defer cancel()
	_, _ = client.Call(ctx, cancelType, api.LoginCancelPayload{LoginID: loginID})
}

// boundedTimeout keeps one call inside the sign-in deadline but no longer
// than max.
func boundedTimeout(deadline time.Time, max time.Duration) time.Duration {
	remaining := time.Until(deadline)
	if remaining < max {
		return remaining
	}
	return max
}

func newLoginTerminal() *term.Terminal {
	return term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{os.Stdin, os.Stdout}, "")
}

func promptLoginMethod(tty *term.Terminal, lastMethod string) (string, error) {
	def, hint := "browser", "[b]"
	if strings.EqualFold(lastMethod, "password") {
		def, hint = "password", "[p]"
	}
	tty.SetPrompt(fmt.Sprintf("Sign in with [b]rowser or [p]assword? %s: ", hint))
	line, err := tty.ReadLine()
	if err != nil {
		return "", err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "":
		return def, nil
	case "b", "browser":
		return "browser", nil
	case "p", "password":
		return "password", nil
	default:
		return "", errors.New("enter b for browser or p for password")
	}
}

func loginCodePrompt(ls *api.LoginState) string {
	switch ls.Method {
	case "totp":
		return "Authenticator code: "
	case "unblock":
		return "Unblock code: "
	}
	if ls.VerificationMethod == "email" {
		fmt.Fprintln(os.Stdout, "Check your email and open the sign-in link, then press Enter (leave the code empty).")
		return "Code: "
	}
	return "Code: "
}

func loginStartCall(ctx context.Context, client *ipc.Client, typ string, payload any, stderr io.Writer, command string) *api.LoginState {
	env, err := client.Call(ctx, typ, payload)
	if err != nil {
		fmt.Fprintf(stderr, "kumiho %s: %v\n", command, err)
		return nil
	}
	var ls api.LoginState
	if err := env.DecodePayload(&ls); err != nil {
		fmt.Fprintf(stderr, "kumiho %s: %v\n", command, err)
		return nil
	}
	return &ls
}

// printSignInDone reports a finished login with the wording that matches the
// command: `login` signs in, `accounts add` adds an account.
func printSignInDone(w io.Writer, adding bool, email string) {
	if adding {
		fmt.Fprintf(w, "Added account %s.\n", email)
		return
	}
	fmt.Fprintf(w, "Signed in as %s.\n", email)
}

func loginCall(ctx context.Context, client *ipc.Client, typ string, payload any, stderr io.Writer, command string) *api.LoginState {
	env, err := client.Call(ctx, typ, payload)
	if err != nil {
		fmt.Fprintf(stderr, "kumiho %s: %v\n", command, err)
		return nil
	}
	var ls api.LoginState
	if err := env.DecodePayload(&ls); err != nil {
		fmt.Fprintf(stderr, "kumiho %s: %v\n", command, err)
		return nil
	}
	return &ls
}
