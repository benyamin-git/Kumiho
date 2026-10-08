package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

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
}

type locationsMsg struct {
	countries []serverlist.Country
	err       error
}

type selectDoneMsg struct{ err error }

func fetchLocations(c *ipc.Client, refresh bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		env, err := c.Call(ctx, ipc.TypeLocations, ipc.LocationsPayload{Refresh: refresh})
		if err != nil {
			return locationsMsg{err: err}
		}
		var res ipc.LocationsResult
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
		_, _ = c.Call(ctx, ipc.TypePing, ipc.PingPayload{Hosts: hosts})
		return nil
	}
}

func selectLocation(c *ipc.Client, countryCode, cityCode, server string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := c.Call(ctx, ipc.TypeSelectLocation, ipc.SelectLocationPayload{
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
		var hosts []string
		for _, row := range rows {
			if row.kind == rowServer {
				hosts = append(hosts, fmt.Sprintf("%s:%d", row.host, row.port))
			}
		}
		return m, pingHosts(m.client, hosts)
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
			fmt.Fprintf(b, "%s%s %s (%s) - %d cities\n", cursor, mark, row.countryName, row.countryCode, row.cityCount)
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
	fmt.Fprintln(b, dimStyle.Render("enter expand/select • p ping • r refresh • / filter • esc back • ? help"))
}
