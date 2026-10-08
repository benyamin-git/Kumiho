package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/logging"
)

// logsCapacity bounds the client-side log buffer.
const logsCapacity = 2000

// logsState is the Logs screen state (PLAN.md §6).
type logsState struct {
	entries   []logging.Entry
	loading   bool
	err       string
	notice    string
	follow    bool
	minLevel  logging.Level
	search    string
	searching bool
	offset    int // lines scrolled up from the tail
}

type logsSnapshotMsg struct {
	entries []logging.Entry
	err     error
}

type logsSubscribedMsg struct{ err error }

type logsClearedMsg struct{ err error }

func logsSnapshot(c *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		env, err := c.Call(ctx, ipc.TypeLogsSnapshot, ipc.LogsSnapshotPayload{Level: "debug"})
		if err != nil {
			return logsSnapshotMsg{err: err}
		}
		var snap ipc.LogSnapshot
		if err := env.DecodePayload(&snap); err != nil {
			return logsSnapshotMsg{err: err}
		}
		return logsSnapshotMsg{entries: snap.Entries}
	}
}

func logsSubscribe(c *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := c.Call(ctx, ipc.TypeLogsSubscribe, ipc.LogsSubscribePayload{Level: "debug"})
		return logsSubscribedMsg{err: err}
	}
}

func logsUnsubscribe(c *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = c.Call(ctx, ipc.TypeLogsUnsubscribe, nil)
		return nil
	}
}

func logsClear(c *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := c.Call(ctx, ipc.TypeLogsClear, nil)
		return logsClearedMsg{err: err}
	}
}

func (lg *logsState) append(e logging.Entry) {
	lg.entries = append(lg.entries, e)
	if len(lg.entries) > logsCapacity {
		lg.entries = append(lg.entries[:0], lg.entries[len(lg.entries)-logsCapacity:]...)
	}
}

// filtered returns the entries matching the display level and search.
func (lg *logsState) filtered() []logging.Entry {
	q := strings.ToLower(lg.search)
	out := make([]logging.Entry, 0, len(lg.entries))
	for _, e := range lg.entries {
		if e.Level < lg.minLevel {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.Tag+" "+e.Msg), q) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (m *Model) logsViewHeight() int {
	h := m.height - 7 // title block, status line, hint, footer
	if h < 5 {
		h = 5
	}
	if h > 40 {
		h = 40
	}
	return h
}

func (m *Model) clampLogsOffset() {
	lg := &m.lg
	max := len(lg.filtered()) - 1
	if max < 0 {
		max = 0
	}
	if lg.offset > max {
		lg.offset = max
	}
	if lg.offset < 0 {
		lg.offset = 0
	}
}

func (m *Model) handleLogsKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	lg := &m.lg
	if lg.searching {
		switch k.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "esc":
			lg.searching = false
			lg.search = ""
			lg.offset = 0
			return m, nil
		case "enter":
			lg.searching = false
			return m, nil
		case "backspace":
			lg.search = trimLastRune(lg.search)
			lg.offset = 0
			return m, nil
		}
		if len(k.Runes) > 0 {
			lg.search += string(k.Runes)
			lg.offset = 0
		}
		return m, nil
	}

	switch k.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.screen = screenDashboard
		return m, logsUnsubscribe(m.client)
	case "f":
		lg.follow = !lg.follow
		if lg.follow {
			lg.offset = 0
		}
	case "v":
		lg.minLevel = (lg.minLevel + 1) % 4
		lg.offset = 0
	case "/":
		lg.searching = true
	case "c":
		lg.entries = nil
		lg.offset = 0
		return m, logsClear(m.client)
	case "e":
		path, err := m.exportLogs()
		if err != nil {
			lg.err = "export failed: " + err.Error()
		} else {
			lg.notice = "exported to " + path
		}
		return m, nil
	case "up", "k":
		lg.offset++
		lg.follow = false
		m.clampLogsOffset()
	case "down", "j":
		if lg.offset > 0 {
			lg.offset--
		}
		if lg.offset == 0 {
			lg.follow = true
		}
	case "pgup":
		lg.offset += m.logsViewHeight()
		lg.follow = false
		m.clampLogsOffset()
	case "pgdown":
		lg.offset -= m.logsViewHeight()
		if lg.offset <= 0 {
			lg.offset = 0
			lg.follow = true
		}
	case "g", "home":
		lg.follow = false
		m.clampLogsOffset()
		if n := len(lg.filtered()); n > 0 {
			lg.offset = n - 1
		}
	case "G", "end":
		lg.offset = 0
		lg.follow = true
	case "r":
		lg.loading = true
		return m, logsSnapshot(m.client)
	}
	return m, nil
}

// exportLogs writes the currently visible (filtered) entries to a local file.
func (m *Model) exportLogs() (string, error) {
	rows := m.lg.filtered()
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".local", "state", "kumiho")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "kumiho-logs-"+time.Now().Format("20060102-150405")+".log")
	var b strings.Builder
	for _, e := range rows {
		b.WriteString(e.Line())
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func (m *Model) renderLogs(b *strings.Builder) {
	lg := &m.lg
	status := "paused"
	if lg.follow {
		status = "following"
	}
	fmt.Fprintf(b, "%s  %s\n\n", titleStyle.Render("Logs"),
		dimStyle.Render(fmt.Sprintf("%s · level ≥ %s · %d entries", status, lg.minLevel.String(), len(lg.entries))))

	if lg.err != "" {
		fmt.Fprintln(b, errStyle.Render(lg.err))
		fmt.Fprintln(b)
	}
	if lg.searching || lg.search != "" {
		suffix := ""
		if lg.searching {
			suffix = "_"
		}
		fmt.Fprintf(b, "search: %s%s\n\n", lg.search, suffix)
	}

	rows := lg.filtered()
	h := m.logsViewHeight()
	offset := lg.offset
	if offset > len(rows) {
		offset = len(rows)
	}
	end := len(rows) - offset
	start := end - h
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}
	visible := rows[start:end]
	if len(visible) == 0 {
		fmt.Fprintln(b, dimStyle.Render("(no matching log entries)"))
	}
	for _, e := range visible {
		fmt.Fprintln(b, renderLogEntry(e))
	}

	if lg.notice != "" {
		fmt.Fprintf(b, "\n%s\n", okStyle.Render(lg.notice))
	}
	fmt.Fprintln(b)
	fmt.Fprintln(b, dimStyle.Render("f follow • v level • / search • c clear • e export • esc back • ? help • q quit"))
}

func renderLogEntry(e logging.Entry) string {
	line := fmt.Sprintf("%s %s [%s] %s", e.Time.Format("15:04:05"), e.Level.Tag3(), e.Tag, e.Msg)
	switch e.Level {
	case logging.Warn:
		return warnStyle.Render(line)
	case logging.Error:
		return errStyle.Render(line)
	case logging.Debug:
		return dimStyle.Render(line)
	}
	return line
}
