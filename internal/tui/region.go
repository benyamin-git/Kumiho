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

type regionCheckMsg struct {
	result api.RegionCheckResult
	err    error
}

type prepareMsg struct {
	result api.PrepareResult
	err    error
}

type guideTickMsg struct{}

func fetchRegionCheck(c *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		env, err := c.Call(ctx, ipc.TypeRegionCheck, nil)
		if err != nil {
			return regionCheckMsg{err: err}
		}
		var res api.RegionCheckResult
		if err := env.DecodePayload(&res); err != nil {
			return regionCheckMsg{err: err}
		}
		return regionCheckMsg{result: res}
	}
}

func preparePass(c *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		env, err := c.Call(ctx, ipc.TypePrepare, nil)
		if err != nil {
			return prepareMsg{err: err}
		}
		var res api.PrepareResult
		if err := env.DecodePayload(&res); err != nil {
			return prepareMsg{err: err}
		}
		return prepareMsg{result: res}
	}
}

func guideTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return guideTickMsg{} })
}

// openGuide shows the Blocked region guide and starts the reachability check.
func (m *Model) openGuide() tea.Cmd {
	m.guide = true
	m.guideCheck = api.RegionCheckResult{}
	m.guideHaveCheck = false
	m.guideChecking = true
	m.guideErr = ""
	m.guideNotice = ""
	return tea.Batch(fetchRegionCheck(m.client), guideTick())
}

func (m *Model) handleGuideKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc", "enter":
		m.guide = false
		return m, nil
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "r":
		m.guideChecking = true
		m.guideErr = ""
		m.guideHaveCheck = false
		return m, fetchRegionCheck(m.client)
	case "p":
		if !m.status.Authenticated {
			m.guideNotice = "Sign in first (press l)."
			return m, nil
		}
		if m.status.State != "IDLE" {
			m.guideNotice = "Disconnect first, then prepare."
			return m, nil
		}
		m.guideNotice = "Preparing: server list, profile, then the pass..."
		return m, preparePass(m.client)
	case "l":
		m.guide = false
		m.screen = screenLogin
		m.step = "method"
		m.email, m.password, m.code = "", "", ""
		m.loginErr = ""
		m.ac.adding = false
		return m, fetchAccounts(m.client)
	case "s":
		m.guide = false
		m.screen = screenSettings
		m.st.err = ""
		m.st.notice = ""
		return m, fetchSettings(m.client)
	case "c", " ":
		if !m.status.Authenticated {
			m.guideNotice = "Sign in first (press l)."
			return m, nil
		}
		if m.status.PassExpires <= time.Now().Unix() {
			m.guideNotice = "No prepared pass yet. Turn on your other VPN and press p."
			return m, nil
		}
		m.guide = false
		return m, toggleConnection(m.client, false)
	}
	return m, nil
}

func (m *Model) renderGuide(b *strings.Builder) {
	fmt.Fprintf(b, "%s\n\n", titleStyle.Render("Blocked region"))
	if m.guideChecking {
		fmt.Fprintln(b, dimStyle.Render("Checking Mozilla's hosts..."))
	} else if m.guideErr != "" {
		fmt.Fprintf(b, "%s\n", errStyle.Render(m.guideErr))
	} else if m.guideHaveCheck {
		fmt.Fprintln(b, "Mozilla's hosts from this network:")
		for _, h := range m.guideCheck.Hosts {
			style := okStyle
			status := h.Status
			switch h.Status {
			case api.RegionBlocked:
				style = errStyle
			case api.RegionUnknown:
				style = warnStyle
			}
			fmt.Fprintf(b, "  %-40s %s\n", h.Host, style.Render(status))
		}
		fmt.Fprintln(b)
	}

	if m.guidePreparedCard(b) {
		fmt.Fprintln(b)
	}

	fmt.Fprintln(b, titleStyle.Render("Option 1: I have another VPN (recommended)"))
	fmt.Fprintln(b, "  1. Turn on your other VPN.")
	if !m.status.Authenticated {
		fmt.Fprintln(b, "  2. Sign in:                 press l")
	} else {
		fmt.Fprintln(b, "  2. Signed in as "+m.status.Email)
	}
	fmt.Fprintln(b, "  3. Prepare:                 press p")
	fmt.Fprintln(b, "  4. Turn the other VPN off when the pass is ready.")
	fmt.Fprintln(b, "  5. Connect:                 press c or space")
	fmt.Fprintln(b, dimStyle.Render("  The other VPN is only needed again after 10 minutes off."))
	fmt.Fprintln(b)

	fmt.Fprintln(b, titleStyle.Render("Option 2: Use a relay"))
	fmt.Fprintln(b, "  1. Deploy docs/relay.md, or use one you trust.")
	fmt.Fprintln(b, "  2. Set the URL and token:   press s (Settings)")
	fmt.Fprintln(b, "  3. Test it:                 Settings → test relay")
	fmt.Fprintln(b, "  4. Sign in with a password, then press p.")
	fmt.Fprintln(b, dimStyle.Render("  Only blocked Mozilla requests use the relay; VPN traffic does not."))
	fmt.Fprintln(b)
	fmt.Fprintln(b, dimStyle.Render("Browser sign-in still needs another VPN or another device; the relay does not proxy the browser page."))

	if m.guideNotice != "" {
		fmt.Fprintf(b, "\n%s\n", warnStyle.Render(m.guideNotice))
	}
	fmt.Fprintln(b)
	fmt.Fprintln(b, dimStyle.Render("r recheck • p prepare • c connect • s settings • l sign in • esc close"))
}

// guidePreparedCard renders the handoff card with the remaining pass lifetime.
func (m *Model) guidePreparedCard(b *strings.Builder) bool {
	if m.status.PassExpires <= 0 {
		return false
	}
	remaining := time.Until(time.Unix(m.status.PassExpires, 0))
	if remaining <= 0 {
		fmt.Fprintln(b, warnStyle.Render("Prepared pass expired. Turn on your other VPN and press p."))
		return true
	}
	style := okStyle
	if remaining < time.Minute {
		style = warnStyle
	}
	fmt.Fprintln(b, style.Render(fmt.Sprintf("Pass ready — valid for %s. Turn off the other VPN and press c to connect.", shortClock(remaining))))
	return true
}

func shortClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	m := int(d / time.Minute)
	s := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%d:%02d", m, s)
}
