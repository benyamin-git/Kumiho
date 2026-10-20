package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
)

// settingKind classifies how a Settings row is edited.
type settingKind int

const (
	settingBool settingKind = iota
	settingEnum
	settingInt
	settingText
)

// settingItem describes one row of the Settings screen (PLAN.md §6).
type settingItem struct {
	key     string
	label   string
	kind    settingKind
	options []string // enum values cycled with enter/space
	hint    string   // what it does and when it applies
}

var settingItems = []settingItem{
	{key: "kill_switch", label: "kill switch", kind: settingEnum, options: []string{"on", "off"},
		hint: "block all tunnel traffic while the tunnel is down; applies immediately"},
	{key: "autoconnect", label: "autoconnect", kind: settingEnum, options: []string{"on", "off"},
		hint: "connect automatically when the daemon starts"},
	{key: "log_level", label: "log level", kind: settingEnum, options: []string{"debug", "info", "warn", "error"},
		hint: "minimum level recorded into the log ring; applies immediately"},
	{key: "redact_targets", label: "redact targets", kind: settingBool,
		hint: "mask hostnames and IPs in log messages; applies immediately"},
	{key: "exit_check", label: "exit check", kind: settingBool,
		hint: "probe the public exit IP after connecting; next connect"},
	{key: "doh_provider", label: "DoH provider", kind: settingEnum,
		options: []string{"automatic", "cloudflare", "google", "quad9", "off"},
		hint:    "in-tunnel DNS-over-HTTPS resolver; next connect"},
	{key: "dns_upstream", label: "DNS upstream", kind: settingText,
		hint: "resolver IP for DoH; empty = provider default; next connect"},
	{key: "edge_address", label: "edge address", kind: settingText,
		hint: "pin the Fastly edge IP; empty = resolve normally; next connect"},
	{key: "upstream_proxy", label: "upstream proxy", kind: settingText,
		hint: "chain the edge connection through a proxy URL; empty = direct; next connect"},
	{key: "exclude_cidrs", label: "exclude CIDRs", kind: settingText,
		hint: "comma-separated ranges that bypass the tunnel; next connect"},
	{key: "allow_lan", label: "allow LAN", kind: settingBool,
		hint: "keep the local subnet outside the tunnel; next connect"},
	{key: "mtu", label: "MTU", kind: settingInt,
		hint: "tun device MTU; next connect"},
	{key: "socks_port", label: "SOCKS port", kind: settingInt,
		hint: "loopback SOCKS5 listener; next connect"},
	{key: "quota_poll_minutes", label: "quota poll", kind: settingInt,
		hint: "minutes between quota refreshes while connected; next connect"},
	{key: "tls_split", label: "TLS split", kind: settingEnum,
		options: []string{"off", "first-byte", "sni"},
		hint:    "split the TLS ClientHello across writes; next connect"},
	{key: "http_proxy", label: "HTTP proxy", kind: settingBool,
		hint: "loopback HTTP/CONNECT proxy through the tunnel; next connect"},
	{key: "http_proxy_port", label: "HTTP proxy port", kind: settingInt,
		hint: "loopback HTTP proxy listener; next connect"},
	{key: "solve_challenges", label: "solve challenges", kind: settingBool,
		hint: "solve FxA and edge challenges automatically; applies immediately"},
	{key: "keepalive_profile", label: "keepalive profile", kind: settingEnum,
		options: []string{"standard", "slow"},
		hint:    "keepalive ping timeout profile; next connect"},
}

// settingsState is the Settings screen state (PLAN.md §6).
type settingsState struct {
	loaded  bool
	view    api.SettingsView
	pinned  map[string]bool
	cursor  int
	offset  int // first visible row; the viewport scrolls over settingItems
	editing bool
	editBuf string
	err     string
	notice  string
}

type settingsMsg struct {
	view api.SettingsView
	err  error
}

type settingDoneMsg struct {
	key   string
	reset bool
	err   error
}

func fetchSettings(c *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		env, err := c.Call(ctx, ipc.TypeSettingsGet, nil)
		if err != nil {
			return settingsMsg{err: err}
		}
		var view api.SettingsView
		if err := env.DecodePayload(&view); err != nil {
			return settingsMsg{err: err}
		}
		return settingsMsg{view: view}
	}
}

func applySetting(c *ipc.Client, key, value string, reset bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := c.Call(ctx, ipc.TypeSettingsSet, api.SettingsSetPayload{Key: key, Value: value, Reset: reset})
		return settingDoneMsg{key: key, reset: reset, err: err}
	}
}

func (m *Model) handleSettingsKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := &m.st
	if s.editing {
		switch k.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "esc":
			s.editing = false
			s.editBuf = ""
			return m, nil
		case "enter":
			s.editing = false
			item := settingItems[s.cursor]
			val := strings.TrimSpace(s.editBuf)
			// An empty value drops the override, restoring the default.
			return m, applySetting(m.client, item.key, val, val == "")
		case "backspace":
			s.editBuf = trimLastRune(s.editBuf)
			return m, nil
		}
		if len(k.Runes) > 0 {
			s.editBuf += string(k.Runes)
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
	case "r":
		return m, fetchSettings(m.client)
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(settingItems)-1 {
			s.cursor++
		}
	case "pgup":
		s.cursor -= m.settingsPageSize()
		if s.cursor < 0 {
			s.cursor = 0
		}
	case "pgdown":
		s.cursor += m.settingsPageSize()
		if s.cursor > len(settingItems)-1 {
			s.cursor = len(settingItems) - 1
		}
	case "home":
		s.cursor = 0
	case "end":
		s.cursor = len(settingItems) - 1
	case "d":
		item := settingItems[s.cursor]
		return m, applySetting(m.client, item.key, "", true)
	case "enter", " ":
		return m, m.activateSetting()
	}
	m.ensureSettingsCursorVisible()
	return m, nil
}

// settingsChromeLines is the number of rendered lines around the scrolling
// row list: the three-line app header, the "Settings" title and its following
// blank line, the blank line before the hint, the hint itself, and the blank
// line plus footer.
const settingsChromeLines = 9

// settingsViewHeight is the height budget for the row window, including any
// scroll cues. An unknown terminal height shows every row.
func (m *Model) settingsViewHeight() int {
	total := len(settingItems)
	if m.height <= 0 {
		return total
	}
	avail := m.height - settingsChromeLines
	if m.st.err != "" {
		avail -= 2 // error line + blank
	}
	if m.st.notice != "" {
		avail -= 2 // blank + notice
	}
	if avail < 3 {
		avail = 3
	}
	if avail > total {
		avail = total
	}
	return avail
}

// settingsWindow resolves the visible slice of settingItems for a candidate
// first row. The returned count plus the up/down cues never exceeds the height
// budget; the window is clamped so it never runs past the last row.
func (m *Model) settingsWindow(offset int) (start, count int, up, down bool) {
	total := len(settingItems)
	if total == 0 {
		return 0, 0, false, false
	}
	avail := m.settingsViewHeight()
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		offset = total - 1
	}
	if avail >= total {
		return 0, total, false, false
	}

	rows := avail
	up = offset > 0
	if up {
		rows--
	}
	if offset+rows < total {
		rows--
	}
	if rows < 1 {
		rows = 1
	}
	if offset+rows > total {
		offset = total - rows
	}
	up = offset > 0
	down = offset+rows < total
	return offset, rows, up, down
}

// settingsPageSize is how many rows a pgup/pgdown jump moves the cursor: one
// screenful minus one row of overlap so the page boundary stays visible.
func (m *Model) settingsPageSize() int {
	step := m.settingsViewHeight() - 1
	if step < 1 {
		step = 1
	}
	return step
}

// ensureSettingsCursorVisible scrolls the window until the cursor row is on
// screen and normalizes offset inside the current bounds.
func (m *Model) ensureSettingsCursorVisible() {
	s := &m.st
	total := len(settingItems)
	if total == 0 {
		return
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
	if s.cursor >= total {
		s.cursor = total - 1
	}
	offset := s.offset
	if offset < 0 {
		offset = 0
	}
	for i := 0; i <= total; i++ {
		start, count, _, _ := m.settingsWindow(offset)
		switch {
		case s.cursor < start:
			offset = s.cursor
		case s.cursor >= start+count:
			offset = s.cursor - count + 1
		default:
			s.offset = start
			return
		}
	}
	s.offset = offset
}

// activateSetting cycles enums and toggles booleans; text and number rows
// enter inline edit mode.
func (m *Model) activateSetting() tea.Cmd {
	s := &m.st
	item := settingItems[s.cursor]
	switch item.kind {
	case settingBool:
		return applySetting(m.client, item.key, onoff(!s.boolValue(item.key)), false)
	case settingEnum:
		return applySetting(m.client, item.key, s.nextEnum(item), false)
	default:
		s.editing = true
		s.editBuf = s.currentValue(item.key)
		return nil
	}
}

func (s *settingsState) boolValue(key string) bool {
	switch key {
	case "kill_switch":
		return s.view.KillSwitchEffective
	case "autoconnect":
		return s.view.AutoconnectEffective
	case "redact_targets":
		return s.view.RedactTargets
	case "exit_check":
		return s.view.ExitCheck
	case "allow_lan":
		return s.view.AllowLAN
	case "http_proxy":
		return s.view.HTTPProxy
	case "solve_challenges":
		return s.view.SolveChallenges
	}
	return false
}

// currentValue is the raw editable effective value of a setting.
func (s *settingsState) currentValue(key string) string {
	switch key {
	case "kill_switch", "autoconnect", "redact_targets", "exit_check", "allow_lan",
		"http_proxy", "solve_challenges":
		return onoff(s.boolValue(key))
	case "log_level":
		return s.view.LogLevel
	case "doh_provider":
		return s.view.DoHProvider
	case "tls_split":
		return s.view.TLSSplit
	case "keepalive_profile":
		return s.view.KeepaliveProfile
	case "dns_upstream":
		return s.view.DNSUpstream
	case "edge_address":
		return s.view.EdgeAddress
	case "upstream_proxy":
		return s.view.UpstreamProxy
	case "exclude_cidrs":
		return strings.Join(s.view.ExcludeCIDRs, ", ")
	case "mtu":
		return strconv.Itoa(s.view.MTU)
	case "socks_port":
		return strconv.Itoa(s.view.SocksPort)
	case "http_proxy_port":
		return strconv.Itoa(s.view.HTTPProxyPort)
	case "quota_poll_minutes":
		return strconv.Itoa(s.view.QuotaPollMinutes)
	}
	return ""
}

// displayValue renders the value column (styling empties and on/off).
func (s *settingsState) displayValue(key string) string {
	raw := s.currentValue(key)
	switch key {
	case "dns_upstream":
		// The effective config is already applied, so empty means either the
		// override or the provider default; only the latter is common.
		if raw == "" {
			return dimStyle.Render("(provider default)")
		}
	case "edge_address":
		if raw == "" {
			return dimStyle.Render("(resolve)")
		}
	case "upstream_proxy":
		if raw == "" {
			return dimStyle.Render("(direct)")
		}
	case "exclude_cidrs":
		if raw == "" {
			return dimStyle.Render("(none)")
		}
	case "kill_switch", "autoconnect", "redact_targets", "exit_check", "allow_lan",
		"http_proxy", "solve_challenges":
		return onoffLabel(s.boolValue(key))
	}
	return raw
}

func (s *settingsState) nextEnum(item settingItem) string {
	cur := s.currentValue(item.key)
	for i, opt := range item.options {
		if opt == cur {
			return item.options[(i+1)%len(item.options)]
		}
	}
	return item.options[0]
}

func (m *Model) renderSettings(b *strings.Builder) {
	fmt.Fprintf(b, "%s\n\n", titleStyle.Render("Settings"))
	s := &m.st
	if !s.loaded {
		fmt.Fprintln(b, dimStyle.Render("loading settings..."))
		if s.err != "" {
			fmt.Fprintf(b, "\n%s\n", errStyle.Render(s.err))
		}
		return
	}
	if s.err != "" {
		fmt.Fprintln(b, errStyle.Render(s.err))
		fmt.Fprintln(b)
	}

	start, count, up, down := m.settingsWindow(s.offset)
	if up {
		fmt.Fprintln(b, dimStyle.Render("↑ more"))
	}
	for i := start; i < start+count && i < len(settingItems); i++ {
		item := settingItems[i]
		cursor := "  "
		if i == s.cursor {
			cursor = "> "
		}
		mark := " "
		if s.pinned[item.key] {
			mark = "*"
		}
		val := s.displayValue(item.key)
		if s.editing && i == s.cursor {
			val = s.editBuf + "_"
		}
		fmt.Fprintf(b, "%s%s %-17s %s\n", cursor, mark, item.label, val)
	}
	if down {
		fmt.Fprintln(b, dimStyle.Render("↓ more"))
	}

	fmt.Fprintln(b)
	if s.editing {
		fmt.Fprintln(b, dimStyle.Render("type a value, enter save, esc cancel; empty resets to the default"))
	} else {
		fmt.Fprintln(b, dimStyle.Render(settingItems[s.cursor].hint))
	}
	if s.notice != "" {
		fmt.Fprintf(b, "\n%s\n", okStyle.Render(s.notice))
	}
	fmt.Fprintln(b)
	footer := "enter change • d reset to default • r refresh • esc back • ? help • q quit"
	if len(s.pinned) > 0 {
		footer += " • * changed from config"
	}
	fmt.Fprintln(b, dimStyle.Render(footer))
}

func onoff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}
