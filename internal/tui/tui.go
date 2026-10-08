// Package tui implements the Bubble Tea terminal UI (PLAN.md §6).
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/logging"
)

type screen int

const (
	screenDashboard screen = iota
	screenLogin
	screenLocations
	screenSettings
	screenLogs
	screenAccount
)

type statusMsg struct {
	status ipc.Status
	err    error
}

type eventMsg struct {
	env *ipc.Envelope
}

type eventsClosedMsg struct{}

type loginStateMsg struct {
	state ipc.LoginState
	err   error
}

type tunnelDoneMsg struct {
	disconnected bool
	err          error
}

// Run starts the TUI against a connected daemon client.
func Run(client *ipc.Client) error {
	applyTheme(loadPrefs().Theme)
	m := &Model{
		client: client,
		screen: screenDashboard,
		step:   "email",
		lg:     logsState{follow: true, minLevel: logging.Info},
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// Model is the Bubble Tea application state.
type Model struct {
	client *ipc.Client

	screen   screen
	status   ipc.Status
	haveStat bool
	notice   string

	loc locationsState
	st  settingsState
	lg  logsState
	ac  accountState

	width, height int

	step      string
	email     string
	password  string
	code      string
	twoFAMeth string
	busy      bool
	loginErr  string

	help     bool
	quitting bool
}

// Init subscribes to status snapshots and live status events.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(fetchStatus(m.client), waitEvent(m.client))
}

func fetchStatus(c *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		st, err := c.Status(ctx)
		return statusMsg{status: st, err: err}
	}
}

func waitEvent(c *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		env, ok := <-c.Events()
		if !ok {
			return eventsClosedMsg{}
		}
		return eventMsg{env: env}
	}
}

// toggleConnection connects the full tunnel (or proxy-only when asked) or
// disconnects.
func toggleConnection(c *ipc.Client, disconnect bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		var err error
		if disconnect {
			_, err = c.Call(ctx, ipc.TypeDisconnect, nil)
		} else {
			_, err = c.Call(ctx, ipc.TypeConnect, ipc.ConnectPayload{})
		}
		return tunnelDoneMsg{disconnected: disconnect, err: err}
	}
}

func callLogin(c *ipc.Client, typ string, payload any) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		env, err := c.Call(ctx, typ, payload)
		if err != nil {
			return loginStateMsg{err: err}
		}
		var ls ipc.LoginState
		if err := env.DecodePayload(&ls); err != nil {
			return loginStateMsg{err: err}
		}
		return loginStateMsg{state: ls}
	}
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case statusMsg:
		if msg.err != nil {
			m.notice = "daemon error: " + msg.err.Error()
			return m, nil
		}
		m.status = msg.status
		m.haveStat = true
		return m, nil
	case eventMsg:
		switch msg.env.Type {
		case ipc.TypeStatus:
			var st ipc.Status
			if err := msg.env.DecodePayload(&st); err == nil {
				m.status = st
				m.haveStat = true
			}
		case ipc.TypePingResult:
			var pr ipc.PingResult
			if err := msg.env.DecodePayload(&pr); err == nil {
				if m.loc.rtts == nil {
					m.loc.rtts = map[string]*float64{}
				}
				m.loc.rtts[pr.Host] = pr.RTTMS
			}
		case ipc.TypeLog:
			var e logging.Entry
			if err := msg.env.DecodePayload(&e); err == nil {
				m.lg.append(e)
			}
		}
		return m, waitEvent(m.client)
	case eventsClosedMsg:
		return m, tea.Quit
	case loginStateMsg:
		return m.handleLoginState(msg)
	case locationsMsg:
		return m.handleLocationsMsg(msg)
	case selectDoneMsg:
		if msg.err != nil {
			m.notice = "selection failed: " + msg.err.Error()
		} else {
			m.notice = "location saved"
		}
		return m, fetchStatus(m.client)
	case tunnelDoneMsg:
		switch {
		case msg.err != nil:
			m.notice = "tunnel: " + msg.err.Error()
		case msg.disconnected:
			m.notice = "disconnected"
		default:
			m.notice = "connected"
		}
		return m, fetchStatus(m.client)
	case settingsMsg:
		m.st.loaded = true
		if msg.err != nil {
			m.st.err = msg.err.Error()
			return m, nil
		}
		m.st.err = ""
		m.st.view = msg.view
		m.st.pinned = make(map[string]bool, len(msg.view.Overridden))
		for _, key := range msg.view.Overridden {
			m.st.pinned[key] = true
		}
		return m, nil
	case settingDoneMsg:
		if msg.err != nil {
			m.st.err = msg.err.Error()
			m.st.notice = ""
			return m, nil
		}
		if msg.reset {
			m.st.notice = msg.key + " reset to default"
		} else {
			m.st.notice = msg.key + " updated"
		}
		m.st.err = ""
		return m, fetchSettings(m.client)
	case logsSnapshotMsg:
		m.lg.loading = false
		if msg.err != nil {
			m.lg.err = "logs: " + msg.err.Error()
			return m, nil
		}
		m.lg.err = ""
		m.lg.entries = msg.entries
		m.lg.offset = 0
		m.lg.follow = true
		return m, logsSubscribe(m.client)
	case logsSubscribedMsg:
		if msg.err != nil {
			m.lg.err = "logs: " + msg.err.Error()
		}
		return m, nil
	case logsClearedMsg:
		if msg.err != nil {
			m.lg.err = "logs: " + msg.err.Error()
		}
		return m, nil
	case accountMsg:
		m.ac.loaded = true
		if msg.err != nil {
			m.ac.err = msg.err.Error()
			return m, nil
		}
		m.ac.err = ""
		m.ac.info = msg.info
		return m, nil
	case logoutDoneMsg:
		if msg.err != nil {
			m.ac.err = "sign out: " + msg.err.Error()
			m.ac.notice = ""
			return m, nil
		}
		m.ac = accountState{loaded: true}
		m.screen = screenDashboard
		m.notice = "signed out"
		return m, fetchStatus(m.client)
	}
	return m, nil
}

func (m *Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.help {
		switch k.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		default:
			m.help = false
			return m, nil
		}
	}
	// "?" opens the help overlay everywhere except while free-text input is
	// active (login fields, settings edits, log search, location filter).
	if k.String() == "?" &&
		m.screen != screenLogin && !m.st.editing && !m.lg.searching && !m.loc.filtering {
		m.help = true
		return m, nil
	}
	if m.screen == screenLocations {
		return m.handleLocationsKey(k)
	}
	if m.screen == screenSettings {
		return m.handleSettingsKey(k)
	}
	if m.screen == screenLogs {
		return m.handleLogsKey(k)
	}
	if m.screen == screenAccount {
		return m.handleAccountKey(k)
	}
	if m.screen == screenLogin {
		switch k.String() {
		case "esc":
			m.screen = screenDashboard
			m.password, m.code = "", ""
			m.loginErr = ""
			return m, nil
		case "enter":
			if m.busy {
				return m, nil
			}
			return m.advanceLogin()
		case "backspace":
			m.trimField()
			return m, nil
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}
		if len(k.Runes) > 0 {
			m.appendRunes(k.Runes)
		}
		return m, nil
	}

	switch k.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "l":
		if !m.status.Authenticated {
			m.screen = screenLogin
			m.step = "email"
			m.email, m.password, m.code = "", "", ""
			m.loginErr = ""
			m.notice = ""
		}
		return m, nil
	case "o":
		m.screen = screenLocations
		m.loc.err = ""
		if m.loc.countries == nil {
			m.loc.loading = true
			return m, fetchLocations(m.client, false)
		}
		return m, nil
	case "s":
		m.screen = screenSettings
		m.st.err = ""
		m.st.notice = ""
		return m, fetchSettings(m.client)
	case "a":
		m.screen = screenAccount
		m.ac.err = ""
		m.ac.notice = ""
		return m, fetchAccount(m.client)
	case "g":
		m.screen = screenLogs
		m.lg.err = ""
		m.lg.loading = true
		return m, logsSnapshot(m.client)
	case " ":
		if !m.status.Authenticated {
			m.notice = "sign in first (l)"
			return m, nil
		}
		switch m.status.State {
		case "PROXY_ONLY", "CONNECTED", "CONNECTING", "RECONNECTING", "WAITING_NETWORK":
			return m, toggleConnection(m.client, true)
		default:
			return m, toggleConnection(m.client, false)
		}
	case "r":
		return m, fetchStatus(m.client)
	}
	return m, nil
}

func (m *Model) appendRunes(runes []rune) {
	s := string(runes)
	switch m.step {
	case "email":
		m.email += s
	case "password":
		m.password += s
	case "2fa":
		m.code += s
	}
}

func (m *Model) trimField() {
	switch m.step {
	case "email":
		m.email = trimLastRune(m.email)
	case "password":
		m.password = trimLastRune(m.password)
	case "2fa":
		m.code = trimLastRune(m.code)
	}
}

func trimLastRune(s string) string {
	if s == "" {
		return s
	}
	_, size := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-size]
}

func (m *Model) advanceLogin() (tea.Model, tea.Cmd) {
	m.loginErr = ""
	switch m.step {
	case "email":
		if strings.TrimSpace(m.email) == "" {
			m.loginErr = "email is required"
			return m, nil
		}
		m.busy = true
		return m, callLogin(m.client, ipc.TypeLoginEmail, ipc.LoginEmailPayload{Email: m.email})
	case "password":
		if m.password == "" {
			m.loginErr = "password is required"
			return m, nil
		}
		m.busy = true
		return m, callLogin(m.client, ipc.TypeLoginPassword, ipc.LoginPasswordPayload{Password: m.password})
	case "2fa":
		m.busy = true
		return m, callLogin(m.client, ipc.TypeLogin2FA, ipc.Login2FAPayload{Code: m.code})
	}
	return m, nil
}

func (m *Model) handleLoginState(msg loginStateMsg) (tea.Model, tea.Cmd) {
	m.busy = false
	if msg.err != nil {
		m.loginErr = msg.err.Error()
		return m, nil
	}
	ls := msg.state
	if ls.Error != "" {
		m.loginErr = ls.Error
	}
	switch ls.Step {
	case "done":
		if ls.Error == "" {
			m.screen = screenDashboard
			m.notice = "signed in as " + ls.Email
			m.password, m.code = "", ""
			return m, fetchStatus(m.client)
		}
		return m, nil
	case "email", "password", "2fa":
		m.step = ls.Step
		m.twoFAMeth = ls.VerificationMethod
		if ls.Email != "" {
			m.email = ls.Email
		}
		if ls.Step == "password" {
			m.password = ""
		}
		if ls.Step == "2fa" {
			m.code = ""
		}
		return m, nil
	}
	return m, nil
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("208"))
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

// View implements tea.Model.
func (m *Model) View() string {
	if m.quitting {
		return ""
	}
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render("Kumiho"))
	fmt.Fprintln(&b, dimStyle.Render("unofficial Firefox account VPN client"))
	fmt.Fprintln(&b)

	if m.help {
		m.renderHelp(&b)
		return b.String()
	}
	if m.screen == screenLogin {
		m.renderLogin(&b)
		return b.String()
	}
	if m.screen == screenLocations {
		m.renderLocations(&b)
		return b.String()
	}
	if m.screen == screenSettings {
		m.renderSettings(&b)
		return b.String()
	}
	if m.screen == screenLogs {
		m.renderLogs(&b)
		return b.String()
	}
	if m.screen == screenAccount {
		m.renderAccount(&b)
		return b.String()
	}
	m.renderDashboard(&b)
	return b.String()
}

func (m *Model) renderDashboard(b *strings.Builder) {
	if !m.haveStat {
		fmt.Fprintln(b, "loading status...")
	} else {
		style := warnStyle
		switch m.status.State {
		case "IDLE", "PROXY_ONLY", "CONNECTED":
			style = okStyle
		case "FATAL":
			style = errStyle
		}
		fmt.Fprintf(b, "state:        %s\n", style.Render(m.status.State))
		if m.status.Since > 0 && (m.status.State == "CONNECTED" || m.status.State == "PROXY_ONLY") {
			fmt.Fprintf(b, "uptime:       %s\n", time.Since(time.Unix(m.status.Since, 0)).Round(time.Second))
		}
		if m.status.Email != "" {
			fmt.Fprintf(b, "account:      %s\n", m.status.Email)
		}
		if m.status.TokenExpires > 0 {
			fmt.Fprintf(b, "token expiry: %s\n", time.Unix(m.status.TokenExpires, 0).UTC().Format(time.RFC3339))
		}
		if m.status.Location != "" {
			loc := m.status.Location
			if m.status.Server != "" {
				loc += " (" + m.status.Server + ")"
			}
			fmt.Fprintf(b, "location:     %s\n", loc)
		}
		if m.status.PassExpires > 0 {
			fmt.Fprintf(b, "pass expiry:  %s\n", time.Unix(m.status.PassExpires, 0).UTC().Format(time.RFC3339))
		}
		if q := m.status.Quota; q != nil && (q.Remaining != nil || q.Limit != nil) {
			line := "quota:        "
			switch {
			case q.Remaining != nil && q.Limit != nil:
				line += fmt.Sprintf("%s %s of %s", quotaBar(*q.Remaining, *q.Limit, 20), humanBytes(*q.Remaining), humanBytes(*q.Limit))
			case q.Remaining != nil:
				line += fmt.Sprintf("%s remaining", humanBytes(*q.Remaining))
			default:
				line += humanBytes(*q.Limit)
			}
			fmt.Fprintln(b, line)
			if q.Reset != nil {
				fmt.Fprintf(b, "quota reset:  %s\n", time.Unix(*q.Reset, 0).UTC().Format("2006-01-02"))
			}
		}
		if m.status.ExitIP != "" {
			exit := m.status.ExitIP
			if m.status.ExitCountry != "" {
				exit += " (" + m.status.ExitCountry + ")"
			}
			fmt.Fprintf(b, "exit ip:      %s\n", exit)
		}
		if t := m.status.Totals; t != nil {
			fmt.Fprintf(b, "totals:       up %s / down %s\n", humanBytes(t.Up), humanBytes(t.Down))
		}
		if r := m.status.Rates; r != nil && (r.Up > 0 || r.Down > 0) {
			fmt.Fprintf(b, "rates:        up %s/s / down %s/s\n", humanBytes(int64(r.Up)), humanBytes(int64(r.Down)))
		}
		if m.status.DNS != "" {
			fmt.Fprintf(b, "dns:          %s\n", m.status.DNS)
		}
		if m.status.IPv6 != "" {
			fmt.Fprintf(b, "ipv6:         %s\n", m.status.IPv6)
		}
		fmt.Fprintf(b, "kill switch:  %s\n", onoffLabel(m.status.KillSwitch))
		fmt.Fprintf(b, "autoconnect:  %s\n", onoffLabel(m.status.Autoconnect))
	}

	if m.notice != "" {
		fmt.Fprintf(b, "\n%s\n", okStyle.Render(m.notice))
	}
	fmt.Fprintln(b)
	connectHint := "space connect"
	switch m.status.State {
	case "PROXY_ONLY", "CONNECTED", "CONNECTING", "RECONNECTING", "WAITING_NETWORK":
		connectHint = "space disconnect"
	}
	if m.status.Authenticated {
		fmt.Fprintln(b, dimStyle.Render(connectHint+" • o locations • s settings • a account • g logs • r refresh • ? help • q quit"))
	} else {
		fmt.Fprintln(b, dimStyle.Render("l login • o locations • r refresh • ? help • q quit"))
	}
}

// quotaBar renders a fixed-width progress bar for the remaining quota.
func quotaBar(remaining, limit int64, width int) string {
	if limit <= 0 || width <= 0 {
		return ""
	}
	frac := float64(remaining) / float64(limit)
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	filled := int(frac*float64(width) + 0.5)
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
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

func (m *Model) renderLogin(b *strings.Builder) {
	fmt.Fprintln(b, titleStyle.Render("Sign in to Firefox Accounts"))
	fmt.Fprintln(b)

	switch m.step {
	case "email":
		fmt.Fprintf(b, "Email:    %s\n", m.email+"_")
	case "password":
		fmt.Fprintf(b, "Email:    %s\n", dimStyle.Render(m.email))
		fmt.Fprintf(b, "Password: %s\n", strings.Repeat("•", utf8.RuneCountInString(m.password))+"_")
	case "2fa":
		fmt.Fprintf(b, "Email:    %s\n", dimStyle.Render(m.email))
		if m.twoFAMeth == "email" {
			fmt.Fprintln(b, dimStyle.Render("Open the sign-in link in your email, then press Enter (leave the code empty)."))
		} else {
			fmt.Fprintln(b, dimStyle.Render("Enter the code from the email you received."))
		}
		fmt.Fprintf(b, "Code:     %s\n", m.code+"_")
	}

	if m.busy {
		fmt.Fprintln(b, dimStyle.Render("contacting Mozilla Accounts..."))
	}
	if m.loginErr != "" {
		fmt.Fprintf(b, "\n%s\n", errStyle.Render(m.loginErr))
	}
	fmt.Fprintln(b)
	fmt.Fprintln(b, dimStyle.Render("enter continue • esc cancel"))

	if m.notice != "" {
		fmt.Fprintf(b, "\n%s\n", okStyle.Render(m.notice))
	}
}

func onoffLabel(v bool) string {
	if v {
		return okStyle.Render("on")
	}
	return dimStyle.Render("off")
}
