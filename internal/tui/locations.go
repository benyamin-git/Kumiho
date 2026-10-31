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

// pingRun tracks one Ping all run until the daemon's ping_done event.
type pingRun struct {
	total    int
	hosts    map[string]bool
	answered bool
}

type locSection int

const (
	sectionFastest locSection = iota
	sectionFavorites
	sectionRecent
	sectionLocations
)

// locationsState is the Locations screen state (PLAN.md §6).
type locationsState struct {
	countries   []serverlist.Country
	favorites   []api.LocationRef
	recents     []api.LocationRef
	allowLocked bool
	sortSpeed   bool
	cursor      int
	filter      string
	filtering   bool
	loading     bool
	err         string
	rtts        map[string]*float64 // host:port -> ms (nil = unreachable)
	// speedOrderRTTs is the latency snapshot the current speed order was
	// committed against. A later run never reorders rows while it is in
	// flight; ping_done replaces the snapshot once.
	speedOrderRTTs map[string]*float64
	ping           *pingRun
}

type locationsMsg struct {
	result    api.LocationsResult
	countries []serverlist.Country
	refresh   bool
	err       error
}

type selectDoneMsg struct {
	err              error
	refreshLocations bool
}
type pingFailedMsg struct{ err error }
type pingDoneMsg struct{ done api.PingDone }
type favoriteDoneMsg struct {
	err error
}
type clearRecentsDoneMsg struct{ err error }
type lockedDoneMsg struct {
	on  bool
	err error
}

func fetchLocations(c *ipc.Client, refresh bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		env, err := c.Call(ctx, ipc.TypeLocations, api.LocationsPayload{Refresh: refresh})
		if err != nil {
			return locationsMsg{err: err, refresh: refresh}
		}
		var res api.LocationsResult
		if err := env.DecodePayload(&res); err != nil {
			return locationsMsg{err: err, refresh: refresh}
		}
		return locationsMsg{result: res, countries: res.Countries, refresh: refresh}
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

func selectLocation(c *ipc.Client, countryCode, cityCode, server string, fastest bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := c.Call(ctx, ipc.TypeSelectLocation, api.SelectLocationPayload{
			CountryCode: countryCode,
			CityCode:    cityCode,
			Server:      server,
			Fastest:     fastest,
		})
		return selectDoneMsg{err: err, refreshLocations: fastest}
	}
}

func favoriteLocation(c *ipc.Client, ref api.LocationRef, favorite bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := c.Call(ctx, ipc.TypeFavoriteLocation, api.FavoriteLocationPayload{
			CountryCode: ref.CountryCode,
			CityCode:    ref.CityCode,
			Server:      ref.Server,
			Favorite:    favorite,
		})
		return favoriteDoneMsg{err: err}
	}
}

func clearRecents(c *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := c.Call(ctx, ipc.TypeClearRecents, nil)
		return clearRecentsDoneMsg{err: err}
	}
}

func setLockedLocations(c *ipc.Client, on bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		value := "off"
		if on {
			value = "on"
		}
		_, err := c.Call(ctx, ipc.TypeSettingsSet, api.SettingsSetPayload{Key: "allow_locked", Value: value})
		return lockedDoneMsg{on: on, err: err}
	}
}

// locRow is one flat server row. The screen never draws country or city
// parent rows, so the country/city fields are only labels and search keys.
type locRow struct {
	section     locSection
	ref         api.LocationRef
	favorite    bool
	recent      bool
	locked      bool
	disabled    bool
	fastest     bool
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

// locationAddressKey is the shared star/clock key for duplicate records.
func locationAddressKey(address string) string {
	return strings.ToLower(strings.TrimSpace(address))
}

// allLocationRows flattens every usable server in the loaded tree, without
// applying the filter. ConnectTarget owns the address, so a quarantined or
// targetless record can never be listed, selected or pinged.
func (m *Model) allLocationRows() []locRow {
	favorites := make(map[string]bool)
	for _, ref := range m.loc.favorites {
		if cand, ok := resolveLocationRef(m.loc.countries, ref); ok {
			favorites[locationAddressKey(cand.Address())] = true
		}
	}
	recents := make(map[string]bool)
	for _, ref := range m.loc.recents {
		if cand, ok := resolveLocationRef(m.loc.countries, ref); ok {
			recents[locationAddressKey(cand.Address())] = true
		}
	}

	var rows []locRow
	for _, c := range m.loc.countries {
		if c.Locked && !m.loc.allowLocked {
			continue
		}
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
				key := locationAddressKey(address)
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
					section:     sectionLocations,
					ref:         api.LocationRef{CountryCode: countryCode, CityCode: cityCode, Server: address},
					favorite:    favorites[key],
					recent:      recents[key],
					locked:      c.Locked,
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

func resolveLocationRef(countries []serverlist.Country, ref api.LocationRef) (serverlist.Candidate, bool) {
	candidates := serverlist.Candidates(countries, ref.CountryCode, ref.CityCode)
	for _, cand := range candidates {
		if cand.Address() == ref.Server || cand.Host == ref.Server {
			return cand, true
		}
	}
	if len(candidates) > 0 {
		return candidates[0], true
	}
	return serverlist.Candidate{}, false
}

// fullLocationRows applies the search filter and speed order to the full tree
// only. Pinned sections never depend on either.
func (m *Model) fullLocationRows() []locRow {
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

func (m *Model) fastestVisibleRow() (locRow, bool) {
	var best locRow
	bestRTT := 0.0
	found := false
	for _, row := range m.allLocationRows() {
		rtt, ok := rttAt(m.loc.rtts, row.address)
		value, finite := finiteRTT(rtt)
		if !ok || !finite {
			continue
		}
		if !found || value < bestRTT {
			best, bestRTT, found = row, value, true
		}
	}
	if !found {
		return locRow{section: sectionFastest, fastest: true, disabled: true}, false
	}
	best.section = sectionFastest
	best.fastest = true
	best.disabled = false
	return best, true
}

func (m *Model) refRows(refs []api.LocationRef, section locSection, favorite, recent bool) []locRow {
	byAddress := make(map[string]locRow)
	for _, row := range m.allLocationRows() {
		key := locationAddressKey(row.address)
		if _, ok := byAddress[key]; !ok {
			byAddress[key] = row
		}
	}
	seen := make(map[string]bool)
	var out []locRow
	for _, ref := range refs {
		cand, ok := resolveLocationRef(m.loc.countries, ref)
		if !ok {
			continue
		}
		key := locationAddressKey(cand.Address())
		if seen[key] {
			continue
		}
		seen[key] = true
		row, ok := byAddress[key]
		if !ok {
			row = locRow{
				countryCode: strings.ToUpper(cand.CountryCode),
				cityCode:    strings.ToUpper(cand.CityCode),
				host:        cand.Host,
				port:        cand.Port,
				address:     cand.Address(),
			}
		}
		row.section = section
		row.ref = api.LocationRef{CountryCode: cand.CountryCode, CityCode: cand.CityCode, Server: cand.Address()}
		row.favorite = favorite || m.addressInFavorites(cand.Address())
		row.recent = recent || m.addressInRecents(cand.Address())
		out = append(out, row)
	}
	if section == sectionFavorites {
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].countryCode != out[j].countryCode {
				return out[i].countryCode < out[j].countryCode
			}
			if out[i].cityCode != out[j].cityCode {
				return out[i].cityCode < out[j].cityCode
			}
			return out[i].address < out[j].address
		})
	}
	return out
}

// locationRows returns the selectable rows in section order: Fastest (always
// one row), Favorites, Recent, then the filtered/sorted full tree.
func (m *Model) locationRows() []locRow {
	rows := make([]locRow, 0, 1+len(m.loc.favorites)+len(m.loc.recents)+len(m.loc.countries))
	if fastest, ok := m.fastestVisibleRow(); ok {
		rows = append(rows, fastest)
	} else {
		rows = append(rows, locRow{section: sectionFastest, fastest: true, disabled: true})
	}
	if len(m.loc.favorites) > 0 {
		rows = append(rows, m.refRows(m.loc.favorites, sectionFavorites, true, false)...)
	}
	if len(m.loc.recents) > 0 {
		rows = append(rows, m.refRows(m.loc.recents, sectionRecent, false, true)...)
	}
	rows = append(rows, m.fullLocationRows()...)
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
		key := locationAddressKey(row.address)
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
	targets := make(map[string]bool, len(hosts))
	for _, host := range hosts {
		targets[locationAddressKey(host)] = true
	}
	m.loc.ping = &pingRun{total: len(hosts), hosts: targets}
	return pingHosts(m.client, hosts)
}

// ackPing records that at least one requested result arrived. ping_done owns
// completion, so an early result never ends the run.
func (m *Model) ackPing(host string) {
	if run := m.loc.ping; run != nil && run.hosts[locationAddressKey(host)] {
		run.answered = true
	}
}

func (m *Model) finishPing(done api.PingDone) {
	run := m.loc.ping
	m.loc.ping = nil
	if done.Error != "" {
		m.notice = "ping: " + done.Error
		return
	}
	if run != nil && run.answered {
		m.loc.sortSpeed = true
		m.loc.speedOrderRTTs = cloneRTTs(m.loc.rtts)
		m.loc.cursor = 0
	}
}

func (m *Model) addressInFavorites(address string) bool {
	for _, ref := range m.loc.favorites {
		if strings.EqualFold(ref.Server, address) {
			return true
		}
		if cand, ok := resolveLocationRef(m.loc.countries, ref); ok && strings.EqualFold(cand.Address(), address) {
			return true
		}
	}
	return false
}

func (m *Model) addressInRecents(address string) bool {
	for _, ref := range m.loc.recents {
		if strings.EqualFold(ref.Server, address) {
			return true
		}
		if cand, ok := resolveLocationRef(m.loc.countries, ref); ok && strings.EqualFold(cand.Address(), address) {
			return true
		}
	}
	return false
}

func (m *Model) toggleLocalFavorite(row locRow, favorite bool) {
	if favorite {
		ref := api.LocationRef{CountryCode: row.countryCode, CityCode: row.cityCode, Server: row.address}
		for _, existing := range m.loc.favorites {
			if strings.EqualFold(existing.Server, row.address) {
				return
			}
		}
		m.loc.favorites = append(m.loc.favorites, ref)
		return
	}
	out := m.loc.favorites[:0]
	for _, existing := range m.loc.favorites {
		if strings.EqualFold(existing.Server, row.address) {
			continue
		}
		out = append(out, existing)
	}
	m.loc.favorites = out
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
			m.notice = "No locations to ping"
			return m, nil
		}
		return m, m.startPing(hosts)
	case "f":
		if len(rows) == 0 {
			return m, nil
		}
		row := rows[m.loc.cursor]
		if row.disabled {
			m.notice = "Run Ping all to enable Fastest"
			return m, nil
		}
		if row.fastest {
			m.notice = "Fastest is not a server; select one below"
			return m, nil
		}
		favorite := !row.favorite
		m.toggleLocalFavorite(row, favorite)
		return m, favoriteLocation(m.client, row.ref, favorite)
	case "c":
		if len(m.loc.recents) == 0 {
			return m, nil
		}
		m.loc.recents = nil
		return m, clearRecents(m.client)
	case "L":
		return m, setLockedLocations(m.client, !m.loc.allowLocked)
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
		if row.disabled {
			m.notice = "Run Ping all to enable Fastest"
			return m, nil
		}
		return m, selectLocation(m.client, row.countryCode, row.cityCode, row.address, row.fastest)
	}
	// p (visible-only ping) and x (expand/collapse) were removed; they are
	// harmless no-ops so an old muscle-memory key cannot change the list.
	return m, nil
}

func (m *Model) seedLatency(latency map[string]api.CachedPing) {
	if m.loc.rtts == nil {
		m.loc.rtts = map[string]*float64{}
	}
	for host, sample := range latency {
		if sample.RTTMS == nil {
			m.loc.rtts[host] = nil
			continue
		}
		value := *sample.RTTMS
		m.loc.rtts[host] = &value
	}
}

func (m *Model) handleLocationsMsg(msg locationsMsg) (tea.Model, tea.Cmd) {
	m.loc.loading = false
	if msg.err != nil {
		m.loc.err = msg.err.Error()
		return m, nil
	}
	res := msg.result
	if len(res.Countries) == 0 && len(msg.countries) > 0 {
		res.Countries = msg.countries
	}
	m.loc.countries = res.Countries
	m.loc.favorites = res.Favorites
	m.loc.recents = res.Recents
	m.loc.allowLocked = res.AllowLocked
	m.loc.rtts = map[string]*float64{}
	m.seedLatency(res.Latency)
	if m.loc.rtts == nil {
		m.loc.rtts = map[string]*float64{}
	}
	if m.loc.cursor >= len(m.locationRows()) {
		m.loc.cursor = 0
	}
	return m, nil
}

func sectionTitle(section locSection) string {
	switch section {
	case sectionFastest:
		return "Fastest"
	case sectionFavorites:
		return "Favorites"
	case sectionRecent:
		return "Recent  c clear"
	default:
		return "Locations"
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
	lastSection := locSection(-1)
	for i, row := range rows {
		if row.section != lastSection {
			if lastSection != -1 {
				fmt.Fprintln(b)
			}
			fmt.Fprintln(b, titleStyle.Render(sectionTitle(row.section)))
			lastSection = row.section
		}
		cursor := "  "
		if i == m.loc.cursor {
			cursor = "> "
		}
		if row.disabled {
			fmt.Fprintf(b, "%s%s\n", cursor, dimStyle.Render("Run Ping all to enable Fastest"))
			continue
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
		if row.fastest {
			parts = append([]string{"Fastest"}, parts...)
		}
		suffix := ""
		if row.locked {
			suffix += dimStyle.Render("  locked")
		}
		if row.favorite {
			suffix += okStyle.Render("  *")
		}
		if row.recent {
			suffix += dimStyle.Render("  ~")
		}
		if rtt, ok := rttAt(m.loc.rtts, row.address); ok {
			if rtt == nil {
				suffix += dimStyle.Render("  timeout")
			} else {
				suffix += okStyle.Render(fmt.Sprintf("  %.0f ms", *rtt))
			}
		}
		fmt.Fprintf(b, "%s%s %s%s\n", cursor, mark, strings.Join(parts, " · "), suffix)
	}

	if strings.TrimSpace(m.loc.filter) != "" && len(m.fullLocationRows()) == 0 {
		if lastSection != sectionLocations {
			if lastSection != -1 {
				fmt.Fprintln(b)
			}
			fmt.Fprintln(b, titleStyle.Render("Locations"))
		}
		fmt.Fprintln(b, dimStyle.Render("no locations match the filter"))
	} else if len(m.allLocationRows()) == 0 && strings.TrimSpace(m.loc.filter) == "" {
		if lastSection != sectionLocations {
			if lastSection != -1 {
				fmt.Fprintln(b)
			}
			fmt.Fprintln(b, titleStyle.Render("Locations"))
		}
		fmt.Fprintln(b, dimStyle.Render("no locations loaded"))
	}

	if m.notice != "" {
		fmt.Fprintf(b, "\n%s\n", okStyle.Render(m.notice))
	}
	fmt.Fprintln(b)
	if run := m.loc.ping; run != nil {
		fmt.Fprintln(b, dimStyle.Render(fmt.Sprintf("pinging %d servers...", run.total)))
	}
	fmt.Fprintln(b, dimStyle.Render("enter select • f favorite • c clear recents • L show/hide locked • P ping all • / filter • r refresh • esc back • ? help • q quit"))
}
