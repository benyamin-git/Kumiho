package tui

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

// pingRun tracks one Ping all run until every requested host has answered.
type pingRun struct {
	total    int
	pending  map[string]bool
	answered bool
}

// locationsState is the flat Locations screen state (PLAN.md §6).
type locationsState struct {
	countries []serverlist.Country
	sortSpeed bool
	cursor    int
	filter    string
	filtering bool
	loading   bool
	err       string
	rtts      map[string]*float64 // host:port -> ms (nil = unreachable)
	// speedOrderRTTs is the latency snapshot the current speed order was
	// committed against. A later run never reorders rows while it is in
	// flight; a good completion replaces the snapshot once.
	speedOrderRTTs map[string]*float64
	ping           *pingRun
}

type locationsMsg struct {
	countries []serverlist.Country
	err       error
}

type selectDoneMsg struct{ err error }

type pingFailedMsg struct{ err error }

func fetchLocations(c *ipc.Client, refresh bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		env, err := c.Call(ctx, ipc.TypeLocations, api.LocationsPayload{Refresh: refresh})
		if err != nil {
			return locationsMsg{err: err}
		}
		var res api.LocationsResult
		if err := env.DecodePayload(&res); err != nil {
			return locationsMsg{err: err}
		}
		return locationsMsg{countries: res.Countries}
	}
}

func pingHosts(c *ipc.Client, hosts []string) tea.Cmd {
	return func() tea.Msg {
		if len(hosts) == 0 {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := c.Call(ctx, ipc.TypePing, api.PingPayload{Hosts: hosts}); err != nil {
			return pingFailedMsg{err: err}
		}
		return nil
	}
}

func selectLocation(c *ipc.Client, countryCode, cityCode, server string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := c.Call(ctx, ipc.TypeSelectLocation, api.SelectLocationPayload{
			CountryCode: countryCode,
			CityCode:    cityCode,
			Server:      server,
		})
		return selectDoneMsg{err: err}
	}
}

// locRow is one flat server row. The screen never draws country or city
// parent rows, so the country/city fields are only labels and search keys.
type locRow struct {
	countryCode string
	countryName string
	cityCode    string
	cityName    string
	hostname    string
	host        string
	port        int
	address     string
	recommended bool
	search      string
}

// allLocationRows flattens every usable server in the loaded tree, without
// applying the filter. ConnectTarget owns the address, so a quarantined or
// targetless record can never be listed, selected or pinged.
func (m *Model) allLocationRows() []locRow {
	var rows []locRow
	for _, c := range m.loc.countries {
		countryCode := strings.ToUpper(strings.TrimSpace(c.Code))
		countryName := strings.TrimSpace(c.Name)
		if countryName == "" {
			countryName = countryCode
		}
		recommended := strings.EqualFold(countryCode, serverlist.RecommendedCode)
		for _, city := range c.Cities {
			cityCode := strings.ToUpper(strings.TrimSpace(city.Code))
			cityName := strings.TrimSpace(city.Name)
			for _, s := range city.Servers {
				if s.Quarantined {
					continue
				}
				host, port, ok := s.ConnectTarget()
				if !ok {
					continue
				}
				host = strings.TrimSpace(host)
				if port < 1 || port > 65535 {
					continue
				}
				address := fmt.Sprintf("%s:%d", host, port)
				protocols := make([]string, 0, len(s.Protocols))
				for _, protocol := range s.Protocols {
					if name := strings.TrimSpace(protocol.Name); name != "" {
						protocols = append(protocols, name)
					}
				}
				search := strings.Join([]string{
					c.Name,
					countryName,
					countryCode,
					city.Name,
					cityCode,
					cityName,
					s.Hostname,
					address,
					strings.Join(protocols, " "),
				}, " ")
				rows = append(rows, locRow{
					countryCode: countryCode,
					countryName: countryName,
					cityCode:    cityCode,
					cityName:    cityName,
					hostname:    strings.TrimSpace(s.Hostname),
					host:        host,
					port:        port,
					address:     address,
					recommended: recommended,
					search:      strings.ToLower(search),
				})
			}
		}
	}
	return rows
}

// locationRows returns the visible flat rows in the current order. Speed
// order uses the committed snapshot while a later run is in flight, so
// results never make rows jump under the cursor.
func (m *Model) locationRows() []locRow {
	filter := strings.ToLower(strings.TrimSpace(m.loc.filter))
	rows := m.allLocationRows()
	if filter != "" {
		visible := rows[:0]
		for _, row := range rows {
			if strings.Contains(row.search, filter) {
				visible = append(visible, row)
			}
		}
		rows = visible
	}
	sortLatency := normalizedRTTs(m.loc.rtts)
	if m.loc.sortSpeed && m.loc.speedOrderRTTs != nil {
		sortLatency = normalizedRTTs(m.loc.speedOrderRTTs)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return locationRowLess(rows[i], rows[j], m.loc.sortSpeed, sortLatency)
	})
	return rows
}

func locationRowLess(a, b locRow, sortSpeed bool, latency map[string]*float64) bool {
	if a.recommended != b.recommended {
		return a.recommended
	}
	if sortSpeed {
		ar, aok := finiteRTT(latency[strings.ToLower(a.address)])
		br, bok := finiteRTT(latency[strings.ToLower(b.address)])
		if aok != bok {
			return aok
		}
		if aok && ar != br {
			return ar < br
		}
	}
	if country := compareFold(a.countryName, b.countryName); country != 0 {
		return country < 0
	}
	if city := compareFold(a.cityName, b.cityName); city != 0 {
		return city < 0
	}
	if host := compareFold(a.host, b.host); host != 0 {
		return host < 0
	}
	return compareFold(a.address, b.address) < 0
}

func compareFold(a, b string) int {
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}

func normalizedRTTs(rtts map[string]*float64) map[string]*float64 {
	normalized := make(map[string]*float64, len(rtts))
	for host, rtt := range rtts {
		normalized[strings.ToLower(strings.TrimSpace(host))] = rtt
	}
	return normalized
}

// rttAt resolves a row's label case-insensitively; a duplicate record with a
// different hostname spelling still shares the first spelling's result.
func rttAt(rtts map[string]*float64, address string) (*float64, bool) {
	if rtt, ok := rtts[address]; ok {
		return rtt, true
	}
	for host, rtt := range rtts {
		if strings.EqualFold(host, address) {
			return rtt, true
		}
	}
	return nil, false
}

func finiteRTT(rtt *float64) (float64, bool) {
	if rtt == nil {
		return 0, false
	}
	value := *rtt
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, false
	}
	return value, true
}

func cloneRTTs(rtts map[string]*float64) map[string]*float64 {
	clone := make(map[string]*float64, len(rtts))
	for host, rtt := range rtts {
		clone[host] = rtt
	}
	return clone
}

// allLocationHosts returns every connectable server address in the loaded
// tree, including REC, regardless of the filter.
func (m *Model) allLocationHosts() []string {
	seen := map[string]bool{}
	var hosts []string
	for _, row := range m.allLocationRows() {
		key := strings.ToLower(row.address)
		if seen[key] {
			continue
		}
		seen[key] = true
		hosts = append(hosts, row.address)
	}
	return hosts
}

func (m *Model) startPing(hosts []string) tea.Cmd {
	if m.loc.ping != nil || len(hosts) == 0 {
		return nil
	}
	pending := make(map[string]bool, len(hosts))
	for _, host := range hosts {
		pending[host] = true
	}
	m.loc.ping = &pingRun{total: len(hosts), pending: pending}
	return pingHosts(m.client, hosts)
}

// ackPing records one answer (including a timeout) and commits speed order
// exactly once, when the last requested host has answered. An unrequested
// result is ignored.
func (m *Model) ackPing(host string) {
	run := m.loc.ping
	if run == nil || !run.pending[host] {
		return
	}
	run.answered = true
	delete(run.pending, host)
	if len(run.pending) != 0 {
		return
	}
	if run.answered {
		m.loc.sortSpeed = true
		m.loc.speedOrderRTTs = cloneRTTs(m.loc.rtts)
		m.loc.cursor = 0
	}
	m.loc.ping = nil
}

func (m *Model) handleLocationsKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.loc.filtering {
		switch k.String() {
		case "esc":
			m.loc.filtering = false
			m.loc.filter = ""
			m.loc.cursor = 0
			return m, nil
		case "enter":
			m.loc.filtering = false
			m.loc.cursor = 0
			return m, nil
		case "backspace":
			m.loc.filter = trimLastRune(m.loc.filter)
			m.loc.cursor = 0
			return m, nil
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}
		if len(k.Runes) > 0 {
			m.loc.filter += string(k.Runes)
			m.loc.cursor = 0
		}
		return m, nil
	}

	rows := m.locationRows()
	switch k.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.screen = screenDashboard
		return m, nil
	case "/":
		m.loc.filtering = true
		return m, nil
	case "r":
		m.loc.loading = true
		m.loc.err = ""
		m.loc.sortSpeed = false
		m.loc.speedOrderRTTs = nil
		m.loc.rtts = map[string]*float64{}
		m.loc.cursor = 0
		m.loc.ping = nil // ignore results from a run started before the refresh
		return m, fetchLocations(m.client, true)
	case "P":
		if m.loc.ping != nil {
			return m, nil
		}
		hosts := m.allLocationHosts()
		if len(hosts) == 0 {
			return m, nil
		}
		return m, m.startPing(hosts)
	case "up", "k":
		if m.loc.cursor > 0 {
			m.loc.cursor--
		}
		return m, nil
	case "down", "j":
		if m.loc.cursor < len(rows)-1 {
			m.loc.cursor++
		}
		return m, nil
	case "enter":
		if len(rows) == 0 {
			return m, nil
		}
		row := rows[m.loc.cursor]
		return m, selectLocation(m.client, row.countryCode, row.cityCode, row.address)
	}
	// p (visible-only ping) and x (expand/collapse) were removed; they are
	// harmless no-ops so an old muscle-memory key cannot change the list.
	return m, nil
}

func (m *Model) handleLocationsMsg(msg locationsMsg) (tea.Model, tea.Cmd) {
	m.loc.loading = false
	if msg.err != nil {
		m.loc.err = msg.err.Error()
		return m, nil
	}
	m.loc.countries = msg.countries
	if m.loc.rtts == nil {
		m.loc.rtts = map[string]*float64{}
	}
	if m.loc.cursor >= len(m.locationRows()) {
		m.loc.cursor = 0
	}
	return m, nil
}

func (m *Model) renderLocations(b *strings.Builder) {
	fmt.Fprintf(b, "%s\n\n", titleStyle.Render("Locations"))

	if m.loc.loading {
		fmt.Fprintln(b, dimStyle.Render("loading locations..."))
		return
	}
	if m.loc.err != "" {
		fmt.Fprintln(b, errStyle.Render(m.loc.err))
		fmt.Fprintln(b)
	}
	if m.loc.filtering || m.loc.filter != "" {
		fmt.Fprintf(b, "filter: %s_\n\n", m.loc.filter)
	}

	rows := m.locationRows()
	if len(rows) == 0 {
		empty := "no locations loaded"
		if strings.TrimSpace(m.loc.filter) != "" {
			empty = "no locations match the filter"
		}
		fmt.Fprintln(b, dimStyle.Render(empty))
	}
	for i, row := range rows {
		cursor := "  "
		if i == m.loc.cursor {
			cursor = "> "
		}
		mark := " "
		if strings.EqualFold(m.status.Server, row.address) {
			mark = "*"
		}
		label := row.countryCode
		if label == "" {
			label = row.countryName
		}
		if row.recommended {
			label = "★ " + label
		} else if flag := serverlist.FlagEmoji(row.countryCode); flag != "" {
			label = flag + " " + label
		}
		parts := []string{label}
		if !row.recommended && row.cityName != "" &&
			!strings.EqualFold(row.cityName, row.countryName) {
			parts = append(parts, row.cityName)
		}
		parts = append(parts, row.address)
		suffix := ""
		if rtt, ok := rttAt(m.loc.rtts, row.address); ok {
			if rtt == nil {
				suffix = dimStyle.Render("  timeout")
			} else {
				suffix = okStyle.Render(fmt.Sprintf("  %.0f ms", *rtt))
			}
		}
		fmt.Fprintf(b, "%s%s %s%s\n", cursor, mark, strings.Join(parts, " · "), suffix)
	}

	if m.notice != "" {
		fmt.Fprintf(b, "\n%s\n", okStyle.Render(m.notice))
	}
	fmt.Fprintln(b)
	if run := m.loc.ping; run != nil {
		fmt.Fprintln(b, dimStyle.Render(fmt.Sprintf("pinging %d servers...", run.total)))
	}
	fmt.Fprintln(b, dimStyle.Render("enter select • P ping all • / filter • r refresh • esc back • ? help • q quit"))
}
