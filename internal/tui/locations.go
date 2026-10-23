package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

// pingRun tracks one Test speed or Ping all run until every requested host
// has answered.
type pingRun struct {
	all     bool
	total   int
	pending map[string]bool
}

// locationsState is the Locations screen state (PLAN.md §6).
type locationsState struct {
	countries         []serverlist.Country
	expandedCountries map[string]bool
	expandedCities    map[string]bool
	cursor            int
	filter            string
	filtering         bool
	loading           bool
	err               string
	rtts              map[string]*float64 // host:port -> ms (nil = unreachable)
	ping              *pingRun
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

type locRowKind int

const (
	rowCountry locRowKind = iota
	rowCity
	rowServer
)

type locRow struct {
	kind        locRowKind
	countryCode string
	countryName string
	cityCode    string
	cityName    string
	host        string
	port        int
	cityCount   int
}

func (m *Model) locationRows() []locRow {
	filter := strings.ToLower(strings.TrimSpace(m.loc.filter))
	var rows []locRow
	for _, c := range m.loc.countries {
		if filter != "" &&
			!strings.Contains(strings.ToLower(c.Name), filter) &&
			!strings.Contains(strings.ToLower(c.Code), filter) {
			continue
		}
		rows = append(rows, locRow{kind: rowCountry, countryCode: c.Code, countryName: c.Name, cityCount: len(c.Cities)})
		if !m.loc.expandedCountries[c.Code] {
			continue
		}
		for _, city := range c.Cities {
			rows = append(rows, locRow{kind: rowCity, countryCode: c.Code, countryName: c.Name, cityCode: city.Code, cityName: city.Name})
			if !m.loc.expandedCities[c.Code+"/"+city.Code] {
				continue
			}
			for _, s := range city.Servers {
				host, port, ok := s.ConnectTarget()
				if !ok {
					continue
				}
				rows = append(rows, locRow{kind: rowServer, countryCode: c.Code, cityCode: city.Code, host: host, port: port})
			}
		}
	}
	return rows
}

// allLocationHosts returns every connectable server address in the loaded
// tree, including REC, regardless of the filter or expansion state.
func (m *Model) allLocationHosts() []string {
	seen := map[string]bool{}
	var hosts []string
	for _, c := range m.loc.countries {
		for _, city := range c.Cities {
			for _, s := range city.Servers {
				if s.Quarantined {
					continue
				}
				host, port, ok := s.ConnectTarget()
				if !ok {
					continue
				}
				addr := fmt.Sprintf("%s:%d", host, port)
				key := strings.ToLower(addr)
				if seen[key] {
					continue
				}
				seen[key] = true
				hosts = append(hosts, addr)
			}
		}
	}
	return hosts
}

func (m *Model) anyExpanded() bool {
	for _, expanded := range m.loc.expandedCountries {
		if expanded {
			return true
		}
	}
	for _, expanded := range m.loc.expandedCities {
		if expanded {
			return true
		}
	}
	return false
}

// toggleAllExpanded opens every country and city, or closes them all when any
// group is already open.
func (m *Model) toggleAllExpanded() {
	if len(m.loc.countries) == 0 {
		return
	}
	if m.anyExpanded() {
		m.loc.expandedCountries = map[string]bool{}
		m.loc.expandedCities = map[string]bool{}
		return
	}
	countries := make(map[string]bool)
	cities := make(map[string]bool)
	for _, c := range m.loc.countries {
		countries[c.Code] = true
		for _, city := range c.Cities {
			cities[c.Code+"/"+city.Code] = true
		}
	}
	m.loc.expandedCountries = countries
	m.loc.expandedCities = cities
}

func (m *Model) startPing(all bool, hosts []string) tea.Cmd {
	if m.loc.ping != nil || len(hosts) == 0 {
		return nil
	}
	pending := make(map[string]bool, len(hosts))
	for _, host := range hosts {
		pending[host] = true
	}
	m.loc.ping = &pingRun{all: all, total: len(hosts), pending: pending}
	return pingHosts(m.client, hosts)
}

func (m *Model) ackPing(host string) {
	if m.loc.ping == nil {
		return
	}
	delete(m.loc.ping.pending, host)
	if len(m.loc.ping.pending) == 0 {
		m.loc.ping = nil
	}
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
		return m, fetchLocations(m.client, true)
	case "p":
		if m.loc.ping != nil {
			return m, nil
		}
		var hosts []string
		for _, row := range rows {
			if row.kind == rowServer {
				hosts = append(hosts, fmt.Sprintf("%s:%d", row.host, row.port))
			}
		}
		if len(hosts) == 0 {
			return m, nil
		}
		return m, m.startPing(false, hosts)
	case "P":
		if m.loc.ping != nil {
			return m, nil
		}
		hosts := m.allLocationHosts()
		if len(hosts) == 0 {
			return m, nil
		}
		return m, m.startPing(true, hosts)
	case "x":
		if strings.TrimSpace(m.loc.filter) != "" {
			return m, nil
		}
		m.toggleAllExpanded()
		return m, nil
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
		switch row.kind {
		case rowCountry:
			m.loc.expandedCountries[row.countryCode] = !m.loc.expandedCountries[row.countryCode]
		case rowCity:
			key := row.countryCode + "/" + row.cityCode
			m.loc.expandedCities[key] = !m.loc.expandedCities[key]
		case rowServer:
			return m, selectLocation(m.client, row.countryCode, row.cityCode, fmt.Sprintf("%s:%d", row.host, row.port))
		}
		return m, nil
	}
	return m, nil
}

func (m *Model) handleLocationsMsg(msg locationsMsg) (tea.Model, tea.Cmd) {
	m.loc.loading = false
	if msg.err != nil {
		m.loc.err = msg.err.Error()
		return m, nil
	}
	m.loc.countries = msg.countries
	if m.loc.expandedCountries == nil {
		m.loc.expandedCountries = map[string]bool{}
	}
	if m.loc.expandedCities == nil {
		m.loc.expandedCities = map[string]bool{}
	}
	if m.loc.rtts == nil {
		m.loc.rtts = map[string]*float64{}
	}
	m.autoExpandSelection()
	return m, nil
}

// autoExpandSelection reveals the persisted selection on first load.
func (m *Model) autoExpandSelection() {
	if m.status.Location == "" {
		return
	}
	m.loc.expandedCountries[m.status.Location] = true
	if m.status.Server == "" {
		return
	}
	for _, c := range m.loc.countries {
		if c.Code != m.status.Location {
			continue
		}
		for _, city := range c.Cities {
			for _, s := range city.Servers {
				host, port, ok := s.ConnectTarget()
				if ok && fmt.Sprintf("%s:%d", host, port) == m.status.Server {
					m.loc.expandedCities[c.Code+"/"+city.Code] = true
				}
			}
		}
	}
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
		fmt.Fprintln(b, dimStyle.Render("no locations loaded"))
	}
	for i, row := range rows {
		cursor := "  "
		if i == m.loc.cursor {
			cursor = "> "
		}
		switch row.kind {
		case rowCountry:
			mark := " "
			if m.status.Location == row.countryCode {
				mark = "*"
			}
			flag := serverlist.FlagEmoji(row.countryCode)
			if flag != "" {
				flag += " "
			}
			fmt.Fprintf(b, "%s%s %s%s (%s) - %d cities\n", cursor, mark, flag, row.countryName, row.countryCode, row.cityCount)
		case rowCity:
			fmt.Fprintf(b, "%s    %s (%s)\n", cursor, row.cityName, row.cityCode)
		case rowServer:
			addr := fmt.Sprintf("%s:%d", row.host, row.port)
			mark := " "
			if m.status.Server == addr {
				mark = "*"
			}
			suffix := ""
			if rtt, ok := m.loc.rtts[addr]; ok {
				if rtt == nil {
					suffix = dimStyle.Render("  timeout")
				} else {
					suffix = okStyle.Render(fmt.Sprintf("  %.0f ms", *rtt))
				}
			}
			fmt.Fprintf(b, "%s    %s %s%s\n", cursor, mark, addr, suffix)
		}
	}

	if m.notice != "" {
		fmt.Fprintf(b, "\n%s\n", okStyle.Render(m.notice))
	}
	fmt.Fprintln(b)
	if run := m.loc.ping; run != nil && run.all {
		fmt.Fprintln(b, dimStyle.Render(fmt.Sprintf("pinging %d servers...", run.total)))
	}
	fmt.Fprintln(b, dimStyle.Render("enter expand/select • p ping • P ping all • / filter"))
	fmt.Fprintln(b, dimStyle.Render("x expand/collapse all • r refresh • esc back • ? help"))
}
