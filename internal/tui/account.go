package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/benyamin-git/kumiho/internal/ipc"
)

// accountState is the Account screen state (PLAN.md §6).
type accountState struct {
	loaded        bool
	info          ipc.AccountInfo
	err           string
	notice        string
	confirmLogout bool
}

type accountMsg struct {
	info ipc.AccountInfo
	err  error
}

type logoutDoneMsg struct{ err error }

func fetchAccount(c *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		env, err := c.Call(ctx, ipc.TypeAccount, nil)
		if err != nil {
			return accountMsg{err: err}
		}
		var info ipc.AccountInfo
		if err := env.DecodePayload(&info); err != nil {
			return accountMsg{err: err}
		}
		return accountMsg{info: info}
	}
}

func doLogout(c *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := c.Call(ctx, ipc.TypeLogout, nil)
		return logoutDoneMsg{err: err}
	}
}

func (m *Model) handleAccountKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	ac := &m.ac
	if ac.confirmLogout {
		switch k.String() {
		case "y", "Y":
			ac.confirmLogout = false
			return m, doLogout(m.client)
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		default:
			ac.confirmLogout = false
			return m, nil
		}
	}

	switch k.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.screen = screenDashboard
		return m, nil
	case "r":
		return m, fetchAccount(m.client)
	case "x":
		if ac.loaded && ac.info.Email != "" {
			ac.confirmLogout = true
		}
	}
	return m, nil
}

func (m *Model) renderAccount(b *strings.Builder) {
	fmt.Fprintf(b, "%s\n\n", titleStyle.Render("Account"))
	ac := &m.ac

	if !ac.loaded {
		fmt.Fprintln(b, dimStyle.Render("loading account..."))
		if ac.err != "" {
			fmt.Fprintf(b, "\n%s\n", errStyle.Render(ac.err))
		}
		return
	}

	info := ac.info
	if info.Email == "" && ac.err == "" {
		fmt.Fprintln(b, dimStyle.Render("not signed in — press l on the dashboard to sign in"))
	} else {
		fmt.Fprintf(b, "email:         %s\n", info.Email)
		if info.UID != "" {
			fmt.Fprintf(b, "uid:           %s\n", info.UID)
		}
		if info.Error != "" {
			fmt.Fprintf(b, "subscribed:    %s\n", warnStyle.Render("unknown ("+info.Error+")"))
		} else {
			fmt.Fprintf(b, "subscribed:    %s\n", yesNo(info.Subscribed))
		}
		switch {
		case info.QuotaRemaining != nil && info.MaxBytes != nil:
			fmt.Fprintf(b, "quota:         %s remaining of %s\n", humanBytes(*info.QuotaRemaining), humanBytes(*info.MaxBytes))
		case info.QuotaRemaining != nil:
			fmt.Fprintf(b, "quota:         %s remaining\n", humanBytes(*info.QuotaRemaining))
		case info.MaxBytes != nil:
			fmt.Fprintf(b, "quota:         %s per period\n", humanBytes(*info.MaxBytes))
		}
		if info.LimitedBandwidth {
			fmt.Fprintf(b, "bandwidth:     %s\n", warnStyle.Render("limited"))
		}
		if info.TokenExpires > 0 {
			fmt.Fprintf(b, "token expiry:  %s\n", time.Unix(info.TokenExpires, 0).UTC().Format(time.RFC3339))
		}
	}

	if ac.confirmLogout {
		fmt.Fprintln(b)
		fmt.Fprintln(b, warnStyle.Render("sign out and clear the stored tokens? (y/N)"))
	}
	if ac.notice != "" {
		fmt.Fprintf(b, "\n%s\n", okStyle.Render(ac.notice))
	}
	fmt.Fprintln(b)
	fmt.Fprintln(b, dimStyle.Render("r refresh • x sign out • esc back • ? help • q quit"))
}

func yesNo(v bool) string {
	if v {
		return okStyle.Render("yes")
	}
	return dimStyle.Render("no")
}
