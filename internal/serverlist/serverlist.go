// Package serverlist fetches and models the Mozilla VPN server list from
// Remote Settings (PLAN.md §2.1).
package serverlist

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/benyamin-git/kumiho/internal/fxa"
)

// RecordsURL is the Remote Settings collection carrying the VPN server list.
const RecordsURL = "https://firefox.settings.services.mozilla.com/v1/buckets/main/collections/vpn-serverlist/records"

// RecommendedCode is the country code of the "Recommended Location" record.
const RecommendedCode = "REC"

const maxResponseBytes = 4 << 20

// excludedNames are records that are not user-selectable locations
// (FoxyVPN parity).
var excludedNames = []string{"CatchAll Anycast"}

// Protocol is one transport advertisement on a server record.
type Protocol struct {
	Name   string `json:"name"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Scheme string `json:"scheme"`
}

// Server is one proxy node.
type Server struct {
	Hostname    string     `json:"hostname"`
	Port        int        `json:"port"`
	Quarantined bool       `json:"quarantined"`
	Protocols   []Protocol `json:"protocols"`
}

// ConnectTarget resolves the HTTP/2 CONNECT host:port for this server:
// prefer the "connect" protocol; fall back to hostname:port when the record
// has no protocols at all; reject a server whose protocols lack "connect".
// Quarantined servers are rejected by the callers (Candidates).
func (s Server) ConnectTarget() (host string, port int, ok bool) {
	for _, p := range s.Protocols {
		if p.Name == "connect" {
			host, port = p.Host, p.Port
			if host == "" {
				host = s.Hostname
			}
			if port == 0 {
				port = s.Port
			}
			return host, port, host != "" && port != 0
		}
	}
	if len(s.Protocols) == 0 {
		return s.Hostname, s.Port, s.Hostname != "" && s.Port != 0
	}
	return "", 0, false
}

// City groups the servers of one location.
type City struct {
	Code    string   `json:"code"`
	Name    string   `json:"name"`
	Servers []Server `json:"servers"`
}

// Country groups cities (also the REC pseudo-country).
type Country struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Cities []City `json:"cities"`
}

// Candidate is a connectable proxy target.
type Candidate struct {
	CountryCode string
	CountryName string
	CityCode    string
	Host        string
	Port        int
}

// Address formats the candidate as host:port.
func (c Candidate) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// Fetch retrieves the server list from the default Remote Settings URL.
func Fetch(ctx context.Context, client *http.Client) ([]Country, error) {
	return FetchFrom(ctx, client, RecordsURL)
}

// FetchFrom retrieves and parses the server list from url.
func FetchFrom(ctx context.Context, client *http.Client, url string) ([]Country, error) {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", fxa.UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("server list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server list: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("server list: %w", err)
	}
	return Parse(body)
}

// Parse decodes a Remote Settings records response. Records may carry the
// country directly or nested under a "country" object (PLAN.md §2.1).
func Parse(data []byte) ([]Country, error) {
	var doc struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("server list: %w", err)
	}

	countries := make([]Country, 0, len(doc.Data))
	for _, raw := range doc.Data {
		record := raw
		var wrapper struct {
			Country json.RawMessage `json:"country"`
		}
		if err := json.Unmarshal(raw, &wrapper); err == nil &&
			len(wrapper.Country) > 0 && string(wrapper.Country) != "null" {
			record = wrapper.Country
		}

		var c Country
		if err := json.Unmarshal(record, &c); err != nil {
			continue // skip malformed records like the reference client
		}
		if c.Code == "" || len(c.Cities) == 0 {
			continue
		}
		if isExcludedName(c.Name) {
			continue
		}
		countries = append(countries, c)
	}
	return countries, nil
}

func isExcludedName(name string) bool {
	for _, excluded := range excludedNames {
		if strings.EqualFold(name, excluded) {
			return true
		}
	}
	return false
}

// Candidates returns connectable targets, optionally filtered by country and
// city code (case-insensitive; empty matches everything). Quarantined
// servers are skipped.
func Candidates(countries []Country, countryCode, cityCode string) []Candidate {
	var out []Candidate
	for _, c := range countries {
		if countryCode != "" && !strings.EqualFold(c.Code, countryCode) {
			continue
		}
		for _, city := range c.Cities {
			if cityCode != "" && !strings.EqualFold(city.Code, cityCode) {
				continue
			}
			for _, s := range city.Servers {
				if s.Quarantined {
					continue
				}
				host, port, ok := s.ConnectTarget()
				if !ok {
					continue
				}
				out = append(out, Candidate{
					CountryCode: c.Code,
					CountryName: c.Name,
					CityCode:    city.Code,
					Host:        host,
					Port:        port,
				})
			}
		}
	}
	return out
}

// AlternateCandidates returns up to max distinct alternate edges for cur,
// preferring the same city, then the same country, then the recommended pool
// (PLAN.md §2.4 edge rotation). The current host is never returned.
func AlternateCandidates(countries []Country, cur Candidate, max int) []Candidate {
	if max <= 0 {
		return nil
	}
	seen := map[string]bool{cur.Host: true}
	out := make([]Candidate, 0, max)
	add := func(cands []Candidate) {
		for _, c := range cands {
			if len(out) >= max || seen[c.Host] {
				continue
			}
			seen[c.Host] = true
			out = append(out, c)
		}
	}
	add(Candidates(countries, cur.CountryCode, cur.CityCode))
	add(Candidates(countries, cur.CountryCode, ""))
	add(Candidates(countries, RecommendedCode, ""))
	return out
}

// SortCountries orders REC first, then alphabetically by name.
func SortCountries(countries []Country) {
	sort.SliceStable(countries, func(i, j int) bool {
		ri := countries[i].Code == RecommendedCode
		rj := countries[j].Code == RecommendedCode
		if ri != rj {
			return ri
		}
		return countries[i].Name < countries[j].Name
	})
}
