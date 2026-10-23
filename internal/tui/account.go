package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
)

// accountState is the Account screen state (PLAN.md §6).
type accountState struct {
	loaded bool
	info   api.AccountInfo
	view   api.AccountsView
	err    string
	notice string

	confirmLogout bool
	selected      int

	browserURL string
	browserID  string
	polling    bool

	confirmRemove *api.AccountSummary
	rename        bool
	renameText    string
	adding        bool
}

type accountMsg struct {
	info api.AccountInfo
	err  error
}

type accountsMsg struct {
	view    api.AccountsView
	info    api.AccountInfo
	err     error
	infoErr error
}

type accountActionMsg struct {
	notice string
	err    error
}

type browserStartMsg struct {
	state  api.LoginState
	adding bool
	err    error
}

type browserPollMsg struct {
	state  api.LoginState
	adding bool
	err    error
}

type browserCancelMsg struct{ err error }

type logoutDoneMsg struct{ err error }

func fetchAccount(c *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		env, err := c.Call(ctx, ipc.TypeAccount, nil)
		if err != nil {
			return accountMsg{err: err}
		}
		var info api.AccountInfo
		if err := env.DecodePayload(&info); err != nil {
			return accountMsg{err: err}
		}
		return accountMsg{info: info}
	}
}

// fetchAccounts loads the account list and the active entitlement card.
func fetchAccounts(c *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		env, err := c.Call(ctx, ipc.TypeAccountsList, nil)
		if err != nil {
			return accountsMsg{err: err}
		}
		var view api.AccountsView
		if err := env.DecodePayload(&view); err != nil {
			return accountsMsg{err: err}
		}
		infoEnv, infoErr := c.Call(ctx, ipc.TypeAccount, nil)
		if infoErr != nil {
			return accountsMsg{view: view, infoErr: infoErr}
		}
		var info api.AccountInfo
		if err := infoEnv.DecodePayload(&info); err != nil {
			return accountsMsg{view: view, infoErr: err}
		}
		return accountsMsg{view: view, info: info}
	}
}

func accountCall(c *ipc.Client, typ string, payload any, notice string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := c.Call(ctx, typ, payload)
		return accountActionMsg{notice: notice, err: err}
	}
}

func useAccount(c *ipc.Client, id string) tea.Cmd {
	return accountCall(c, ipc.TypeAccountsUse, api.AccountsUsePayload{ID: id}, "active account changed")
}

func removeAccount(c *ipc.Client, id string) tea.Cmd {
	return accountCall(c, ipc.TypeAccountsRemove, api.AccountsRemovePayload{ID: id}, "account removed")
}

func renameAccount(c *ipc.Client, id, label string) tea.Cmd {
	return accountCall(c, ipc.TypeAccountsUpdate, api.AccountsUpdatePayload{ID: id, Label: label}, "account label updated")
}

func setRotation(c *ipc.Client, enabled bool, threshold int) tea.Cmd {
	return accountCall(c, ipc.TypeAccountsSetRotation, api.AccountsRotationPayload{
		Enabled: enabled, Threshold: threshold,
	}, fmt.Sprintf("rotation %s at %d%%", onoffLabel(enabled), threshold))
}

func startBrowserLogin(c *ipc.Client, targetID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		env, err := c.Call(ctx, ipc.TypeAccountsAddStart, api.LoginStartPayload{
			Method: "browser", AccountID: targetID,
		})
		if err != nil {
			return browserStartMsg{adding: true, err: err}
		}
		var state api.LoginState
		if err := env.DecodePayload(&state); err != nil {
			return browserStartMsg{adding: true, err: err}
		}
		return browserStartMsg{state: state, adding: true}
	}
}

func startLoginBrowser(c *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		env, err := c.Call(ctx, ipc.TypeLoginStart, api.LoginStartPayload{Method: "browser"})
		if err != nil {
			return browserStartMsg{err: err}
		}
		var state api.LoginState
		if err := env.DecodePayload(&state); err != nil {
			return browserStartMsg{err: err}
		}
		return browserStartMsg{state: state}
	}
}

// pollBrowserLogin polls an account-add browser flow once a second.
func pollBrowserLogin(c *ipc.Client, id string) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return browserPoll(c, id, ipc.TypeAccountsAddStatus, true)()
	})
}

func pollLoginBrowser(c *ipc.Client, id string) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return browserPoll(c, id, ipc.TypeLoginStatus, false)()
	})
}

func browserPoll(c *ipc.Client, id, typ string, adding bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		env, err := c.Call(ctx, typ, api.LoginStatusPayload{LoginID: id})
		if err != nil {
			return browserPollMsg{adding: adding, err: err}
		}
		var state api.LoginState
		if err := env.DecodePayload(&state); err != nil {
			return browserPollMsg{adding: adding, err: err}
		}
		return browserPollMsg{state: state, adding: adding}
	}
}

func cancelBrowserLogin(c *ipc.Client, id string, adding bool) tea.Cmd {
	typ := ipc.TypeLoginCancel
	if adding {
		typ = ipc.TypeAccountsAddCancel
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := c.Call(ctx, typ, api.LoginCancelPayload{LoginID: id})
		return browserCancelMsg{err: err}
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
	if ac.polling {
		switch k.String() {
		case "esc":
			id, adding := ac.browserID, ac.adding
			ac.polling, ac.browserURL, ac.browserID = false, "", ""
			return m, cancelBrowserLogin(m.client, id, adding)
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	}

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

	if ac.confirmRemove != nil {
		switch k.String() {
		case "y", "Y":
			target := *ac.confirmRemove
			ac.confirmRemove = nil
			return m, removeAccount(m.client, target.ID)
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		default:
			ac.confirmRemove = nil
			return m, nil
		}
	}

	if ac.rename {
		switch k.String() {
		case "esc":
			ac.rename, ac.renameText = false, ""
			return m, nil
		case "enter":
			account, ok := selectedAccount(ac)
			if !ok {
				ac.rename = false
				return m, nil
			}
			label := strings.TrimSpace(ac.renameText)
			ac.rename, ac.renameText = false, ""
			return m, renameAccount(m.client, account.ID, label)
		case "backspace":
			ac.renameText = trimLastRune(ac.renameText)
			return m, nil
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}
		if len(k.Runes) > 0 {
			ac.renameText += string(k.Runes)
		}
		return m, nil
	}

	switch k.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.screen = screenDashboard
		return m, nil
	case "up", "k":
		if ac.selected > 0 {
			ac.selected--
		}
		return m, nil
	case "down", "j":
		if ac.selected < len(ac.view.Accounts)-1 {
			ac.selected++
		}
		return m, nil
	case "r":
		return m, fetchAccounts(m.client)
	case "u":
		if account, ok := selectedAccount(ac); ok {
			return m, useAccount(m.client, account.ID)
		}
	case "d":
		if account, ok := selectedAccount(ac); ok {
			target := account
			ac.confirmRemove = &target
		}
	case "a":
		ac.adding = true
		ac.err, ac.notice = "", ""
		return m, startBrowserLogin(m.client, "")
	case "R":
		if account, ok := selectedAccount(ac); ok {
			ac.adding = true
			ac.err, ac.notice = "", ""
			return m, startBrowserLogin(m.client, account.ID)
		}
	case "p":
		ac.adding = true
		m.screen = screenLogin
		m.step = "email"
		m.email, m.password, m.code = "", "", ""
		m.loginErr = ""
		return m, nil
	case "n":
		if account, ok := selectedAccount(ac); ok {
			ac.rename = true
			ac.renameText = account.Label
		}
	case "t":
		ac.err, ac.notice = "", ""
		return m, setRotation(m.client, !ac.view.Rotation, ac.view.Threshold)
	case "+", "=":
		threshold := ac.view.Threshold + 1
		if threshold > 50 {
			return m, nil
		}
		return m, setRotation(m.client, ac.view.Rotation, threshold)
	case "-", "_":
		threshold := ac.view.Threshold - 1
		if threshold < 1 {
			return m, nil
		}
		return m, setRotation(m.client, ac.view.Rotation, threshold)
	case "x":
		if ac.loaded && (ac.info.Email != "" || len(ac.view.Accounts) > 0) {
			ac.confirmLogout = true
		}
	}
	return m, nil
}

func selectedAccount(ac *accountState) (api.AccountSummary, bool) {
	if ac.selected < 0 || ac.selected >= len(ac.view.Accounts) {
		return api.AccountSummary{}, false
	}
	return ac.view.Accounts[ac.selected], true
}

func (m *Model) renderAccount(b *strings.Builder) {
	fmt.Fprintf(b, "%s\n\n", titleStyle.Render("Account"))
	ac := &m.ac

	if !ac.loaded {
		fmt.Fprintln(b, dimStyle.Render("loading accounts..."))
		if ac.err != "" {
			fmt.Fprintf(b, "\n%s\n", errStyle.Render(ac.err))
		}
		return
	}
	if ac.polling {
		m.renderBrowserWaiting(b)
		fmt.Fprintln(b)
		fmt.Fprintln(b, dimStyle.Render("press Esc to cancel"))
		return
	}

	if len(ac.view.Accounts) == 0 {
		fmt.Fprintln(b, dimStyle.Render("No accounts on this device."))
	} else {
		fmt.Fprintln(b, dimStyle.Render("accounts"))
		for i, account := range ac.view.Accounts {
			fmt.Fprintln(b, accountRow(account, i == ac.selected))
		}
	}

	fmt.Fprintln(b)
	renderEntitlementCard(b, ac.info)
	renderAccountPrompt(b, ac)

	errText := ac.err
	if errText == "" {
		errText = ac.view.Error
	}
	if errText != "" {
		fmt.Fprintf(b, "\n%s\n", errStyle.Render(errText))
	}
	notice := ac.notice
	if notice == "" {
		notice = ac.view.Notice
	}
	if notice != "" {
		fmt.Fprintf(b, "\n%s\n", okStyle.Render(notice))
	}

	fmt.Fprintln(b)
	fmt.Fprintln(b, dimStyle.Render("↑/↓ select • u use • d remove • a add browser • p add password • R sign in again • n rename"))
	fmt.Fprintln(b, dimStyle.Render("t rotation • +/- threshold • r refresh • x sign out • esc back • ? help • q quit"))
}

func accountRow(account api.AccountSummary, selected bool) string {
	prefix := "  "
	if selected {
		prefix = "> "
	}
	active := " "
	if account.Active {
		active = "*"
	}
	name := account.Email
	if account.Label != "" && !strings.EqualFold(account.Label, account.Email) {
		name = fmt.Sprintf("%s (%s)", account.Email, account.Label)
	}
	method := account.Method
	if method == "" {
		method = "unknown"
	}
	line := fmt.Sprintf("%s%s %s  %s  %s", prefix, active, name, method, accountQuota(account.Quota))
	if account.ReauthRequired {
		line += "  " + warnStyle.Render("needs sign-in")
	}
	return line
}

func accountQuota(q *api.Quota) string {
	if q == nil || q.Limit == nil || q.Remaining == nil || *q.Limit <= 0 {
		return "quota unknown"
	}
	if *q.Remaining <= 0 {
		return "quota exhausted"
	}
	percent := *q.Remaining * 100 / *q.Limit
	if percent > 100 {
		percent = 100
	}
	return fmt.Sprintf("%d%% left", percent)
}

func renderEntitlementCard(b *strings.Builder, info api.AccountInfo) {
	if info.Email == "" {
		if info.Error != "" {
			fmt.Fprintf(b, "%s %s\n", dimStyle.Render("entitlement:"), warnStyle.Render("unknown ("+info.Error+")"))
		}
		return
	}
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

func renderAccountPrompt(b *strings.Builder, ac *accountState) {
	switch {
	case ac.confirmRemove != nil:
		fmt.Fprintln(b)
		fmt.Fprintln(b, warnStyle.Render(removeConfirmCopy(*ac.confirmRemove, len(ac.view.Accounts))))
	case ac.rename:
		fmt.Fprintln(b)
		email := "account"
		if account, ok := selectedAccount(ac); ok {
			email = account.Email
		}
		fmt.Fprintf(b, "new label for %s: %s_\n", email, ac.renameText)
	case ac.confirmLogout:
		fmt.Fprintln(b)
		if len(ac.view.Accounts) > 1 {
			fmt.Fprintln(b, warnStyle.Render("Sign out of every account? (y/N)"))
		} else {
			fmt.Fprintln(b, warnStyle.Render("sign out and clear the stored tokens? (y/N)"))
		}
	}
}

func removeConfirmCopy(target api.AccountSummary, total int) string {
	switch {
	case target.Active && total > 1:
		return fmt.Sprintf("remove active account %s? the next account will be used (y/N)", target.Email)
	case total <= 1:
		return fmt.Sprintf("remove %s? this signs out (y/N)", target.Email)
	default:
		return fmt.Sprintf("remove %s? (y/N)", target.Email)
	}
}

func (m *Model) renderBrowserWaiting(b *strings.Builder) {
	fmt.Fprintln(b, "Open this URL in any browser:")
	fmt.Fprintln(b, m.ac.browserURL)
	fmt.Fprintln(b)
	fmt.Fprintln(b, dimStyle.Render("waiting for the sign-in to finish..."))
}

func yesNo(v bool) string {
	if v {
		return okStyle.Render("yes")
	}
	return dimStyle.Render("no")
}
