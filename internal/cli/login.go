package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/benyamin-git/kumiho/internal/ipc"
	"golang.org/x/term"
)

func runLogin(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: kumiho login\n\nSigns in to Firefox Accounts through the daemon.\n")
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

	tty := term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{os.Stdin, os.Stdout}, "")

	ctx, cancel := callCtx()
	defer cancel()

	tty.SetPrompt("Email: ")
	email, err := tty.ReadLine()
	if err != nil {
		fmt.Fprintf(stderr, "kumiho login: %v\n", err)
		return 1
	}

	ls := loginCall(ctx, client, ipc.TypeLoginEmail, ipc.LoginEmailPayload{Email: email}, stderr)
	if ls == nil {
		return 1
	}

	for attempt := 0; attempt < 12; attempt++ {
		if ls.Error != "" {
			fmt.Fprintf(stderr, "login: %s\n", ls.Error)
		}
		switch ls.Step {
		case "password":
			password, err := tty.ReadPassword("Password: ")
			if err != nil {
				fmt.Fprintf(stderr, "kumiho login: %v\n", err)
				return 1
			}
			ls = loginCall(ctx, client, ipc.TypeLoginPassword, ipc.LoginPasswordPayload{Password: password}, stderr)

		case "2fa":
			if ls.VerificationMethod == "email" {
				fmt.Fprintln(stdout, "Check your email and open the sign-in link, then press Enter (leave the code empty).")
			} else {
				fmt.Fprintln(stdout, "Enter the code from the email you received.")
			}
			tty.SetPrompt("Code: ")
			line, err := tty.ReadLine()
			if err != nil {
				fmt.Fprintf(stderr, "kumiho login: %v\n", err)
				return 1
			}
			ls = loginCall(ctx, client, ipc.TypeLogin2FA, ipc.Login2FAPayload{Code: line}, stderr)

		case "email":
			tty.SetPrompt("Email: ")
			addr, err := tty.ReadLine()
			if err != nil {
				fmt.Fprintf(stderr, "kumiho login: %v\n", err)
				return 1
			}
			ls = loginCall(ctx, client, ipc.TypeLoginEmail, ipc.LoginEmailPayload{Email: addr}, stderr)

		case "done":
			fmt.Fprintf(stdout, "Signed in as %s.\n", ls.Email)
			return 0

		default:
			fmt.Fprintf(stderr, "kumiho login: unexpected login step %q\n", ls.Step)
			return 1
		}
		if ls == nil {
			return 1
		}
	}
	fmt.Fprintln(stderr, "kumiho login: too many attempts")
	return 1
}

func loginCall(ctx context.Context, client *ipc.Client, typ string, payload any, stderr io.Writer) *ipc.LoginState {
	env, err := client.Call(ctx, typ, payload)
	if err != nil {
		fmt.Fprintf(stderr, "kumiho login: %v\n", err)
		return nil
	}
	var ls ipc.LoginState
	if err := env.DecodePayload(&ls); err != nil {
		fmt.Fprintf(stderr, "kumiho login: %v\n", err)
		return nil
	}
	return &ls
}
