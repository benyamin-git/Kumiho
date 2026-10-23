package mock

import (
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

// Scenario names the canned daemon states the mock can report.
type Scenario string

const (
	// ScenarioDisconnected reports a signed-in daemon with the tunnel off.
	ScenarioDisconnected Scenario = "disconnected"
	// ScenarioConnected reports an established tunnel with live-looking
	// counters.
	ScenarioConnected Scenario = "connected"
	// ScenarioConnecting reports a connect attempt in flight.
	ScenarioConnecting Scenario = "connecting"
	// ScenarioError reports a FATAL state with an exhausted quota and a
	// failed entitlement lookup.
	ScenarioError Scenario = "error"
	// ScenarioSignedOut reports a daemon with no stored session (T3.4): the
	// onboarding wizard's login step shows its credential pages and the
	// Account screen its "Not signed in" state.
	ScenarioSignedOut Scenario = "signed-out"
)

// normalize maps unknown scenarios to ScenarioDisconnected, the "off" state,
// so the mock never reports a state the screens cannot label.
func (s Scenario) normalize() Scenario {
	switch s {
	case ScenarioDisconnected, ScenarioConnected, ScenarioConnecting, ScenarioError, ScenarioSignedOut:
		return s
	}
	return ScenarioDisconnected
}

// The canned data is fixed per process, not per call: mockEpoch anchors every
// timestamp (connected-since, expiries, quota reset, log times) to package
// start, so the projected uptime and expiry dates look live in the harness
// while repeated calls return identical data. There is no randomness.
var mockEpoch = time.Now()

const (
	gib = int64(1) << 30

	// demoEmail uses the RFC 2606 reserved domain; the UID is obviously
	// synthetic. No real personal data appears anywhere in the fixtures.
	demoEmail = "user@example.com"
	demoUID   = "0123456789abcdef0123456789abcdef"

	demoLocation = "DE"
	demoServer   = "de1.edge.example:443"
	// demoExitIP is TEST-NET-3 (RFC 5737), reserved for documentation.
	demoExitIP      = "203.0.113.7"
	demoExitCountry = "Germany"

	quotaLimit            = 50 * gib
	quotaRemaining        = 38 * gib
	accountQuotaRemaining = 41 * gib // 82% of the 50 GiB plan
	demoSecondEmail       = "work@example.com"

	// ScenarioError's FATAL state reports an exhausted quota.
	errorQuotaRemaining = 0
)

func i64(v int64) *int64 { return &v }

// quota builds a fresh api.Quota so no two replies share pointers.
func quota(remaining int64) *api.Quota {
	return &api.Quota{
		Limit:     i64(quotaLimit),
		Remaining: i64(remaining),
		Reset:     i64(mockEpoch.Add(12 * 24 * time.Hour).Unix()),
	}
}

// statusFor builds the canned api.Status for one scenario. The disconnected,
// connecting and error states keep the persisted location and the cached
// quota but carry no exit IP or counters; only connected adds the session
// facts (since, pass expiry, exit, totals, rates).
func statusFor(s Scenario) api.Status {
	if s.normalize() == ScenarioSignedOut {
		// No stored session: the wizard's sign-in pages and the "Not signed
		// in" state word; the kill-switch value stays the daemon's.
		return api.Status{State: "UNAUTHENTICATED", KillSwitch: true}
	}
	st := api.Status{
		Authenticated: true,
		Email:         demoEmail,
		TokenExpires:  mockEpoch.Add(30 * 24 * time.Hour).Unix(),
		KillSwitch:    true,
		Autoconnect:   false,
		Location:      demoLocation,
		Server:        demoServer,
		Quota:         quota(quotaRemaining),
	}
	switch s.normalize() {
	case ScenarioConnected:
		st.State = "CONNECTED"
		st.Since = mockEpoch.Add(-87 * time.Minute).Unix()
		st.PassExpires = mockEpoch.Add(6 * time.Hour).Unix()
		st.ExitIP = demoExitIP
		st.ExitCountry = demoExitCountry
		st.Totals = &api.ByteCounters{Up: 12 << 20, Down: 80 << 20}
		st.Rates = &api.ByteRates{Up: 1.5 * 1024 * 1024, Down: 3 * 1024 * 1024}
	case ScenarioConnecting:
		st.State = "CONNECTING"
	case ScenarioError:
		st.State = "FATAL"
		st.Quota = quota(errorQuotaRemaining)
	default:
		st.State = "IDLE"
	}
	return st
}

// accountsFor builds the canned account list for one scenario. The fixtures
// exercise every row state the screen renders: one active browser account
// with an 82%-left quota, one labelled password account that needs re-auth,
// and rotation on at 20%. The signed-out scenario has no accounts.
func accountsFor(s Scenario) api.AccountsView {
	if s.normalize() == ScenarioSignedOut {
		return api.AccountsView{Threshold: 10, LastMethod: "browser"}
	}
	remaining := accountQuotaRemaining
	if s.normalize() == ScenarioError {
		remaining = errorQuotaRemaining
	}
	return api.AccountsView{
		Accounts: []api.AccountSummary{
			{
				ID: "primary", Email: demoEmail, Label: "Home", Method: "browser",
				Active: true, LastUsedAt: mockEpoch.Add(-2 * time.Hour).Unix(),
				Quota: quota(remaining),
			},
			{
				ID: "work", Email: demoSecondEmail, Label: "Work", Method: "password",
				ReauthRequired: true, LastUsedAt: mockEpoch.Add(-72 * time.Hour).Unix(),
				Quota: quota(quotaRemaining),
			},
		},
		Active: "primary", Rotation: true, Threshold: 20, LastMethod: "browser",
	}
}

// accountFor builds the canned api.AccountInfo for one scenario. The error
// scenario models a failed online entitlement lookup: the local session facts
// stay valid, the subscription facts do not, and Error carries the cause.
func accountFor(s Scenario) api.AccountInfo {
	if s.normalize() == ScenarioSignedOut {
		// No local session facts at all: an anonymous snapshot.
		return api.AccountInfo{}
	}
	info := api.AccountInfo{
		Email:        demoEmail,
		TokenExpires: mockEpoch.Add(30 * 24 * time.Hour).Unix(),
	}
	if s.normalize() == ScenarioError {
		info.Error = "guardian: HTTP 503"
		return info
	}
	info.UID = demoUID
	info.Subscribed = true
	info.MaxBytes = i64(quotaLimit)
	info.QuotaRemaining = i64(quotaRemaining)
	return info
}

// settingsView builds a fresh, realistic settings snapshot: the house
// defaults (inventory §Settings) plus one overridden key so the Reset mark is
// visible.
func settingsView() api.SettingsView {
	return api.SettingsView{
		SocksPort:            1080,
		MTU:                  1500,
		ExitCheck:            true,
		DoHProvider:          "automatic",
		AllowLAN:             false,
		LogLevel:             "info",
		RedactTargets:        true,
		KillSwitch:           "on",
		Autoconnect:          "off",
		KillSwitchEffective:  true,
		AutoconnectEffective: false,
		QuotaPollMinutes:     15,
		TLSSplit:             "off",
		HTTPProxy:            false,
		HTTPProxyPort:        8080,
		SolveChallenges:      true,
		KeepaliveProfile:     "standard",
		Overridden:           []string{"kill_switch"},
	}
}

// countries builds the canned server list: REC first, then alphabetical by
// name, exactly as the daemon serves it. Every listed server has a "connect"
// protocol; one US server is quarantined so the screens' "quarantined servers
// are never listed" rule is exercisable.
func countries() []serverlist.Country {
	return []serverlist.Country{
		{
			Code: serverlist.RecommendedCode,
			Name: "Recommended Location",
			Cities: []serverlist.City{{
				Code: "rec", Name: "Recommended",
				Servers: []serverlist.Server{server("rec1.edge.example", 443, false)},
			}},
		},
		{
			Code: "DE", Name: "Germany",
			Cities: []serverlist.City{
				{Code: "ber", Name: "Berlin", Servers: []serverlist.Server{
					server("de1.edge.example", 443, false),
					server("de2.edge.example", 443, false),
				}},
				{Code: "fra", Name: "Frankfurt", Servers: []serverlist.Server{
					server("de3.edge.example", 443, false),
				}},
			},
		},
		{
			Code: "JP", Name: "Japan",
			Cities: []serverlist.City{{
				Code: "tyo", Name: "Tokyo",
				Servers: []serverlist.Server{server("jp1.edge.example", 443, false)},
			}},
		},
		{
			Code: "US", Name: "United States",
			Cities: []serverlist.City{
				{Code: "nyc", Name: "New York", Servers: []serverlist.Server{
					server("us1.edge.example", 443, false),
					server("us2.edge.example", 443, true),
				}},
				{Code: "sfo", Name: "San Francisco", Servers: []serverlist.Server{
					server("us3.edge.example", 443, false),
				}},
			},
		},
	}
}

func server(host string, port int, quarantined bool) serverlist.Server {
	return serverlist.Server{
		Hostname:    host,
		Port:        port,
		Quarantined: quarantined,
		Protocols: []serverlist.Protocol{
			{Name: "connect", Host: host, Port: port, Scheme: "https"},
		},
	}
}

// logEntries builds the canned retained log, oldest first: one line per real
// daemon subsystem tag, every level represented, times fixed relative to
// mockEpoch. Messages carry no secrets or personal data.
func logEntries() []logging.Entry {
	at := func(minutesAgo int) time.Time {
		return mockEpoch.Add(-time.Duration(minutesAgo) * time.Minute).UTC()
	}
	return []logging.Entry{
		{Time: at(7), Level: logging.Debug, Tag: "daemon", Msg: "server list cache hit (4 countries)"},
		{Time: at(6), Level: logging.Info, Tag: "auth", Msg: "session restored"},
		{Time: at(5), Level: logging.Info, Tag: "tunnel", Msg: "connect: de1.edge.example:443 via HTTP/2 CONNECT"},
		{Time: at(4), Level: logging.Info, Tag: "guardian", Msg: "quota refreshed: 38.0 GiB remaining of 50.0 GiB"},
		{Time: at(3), Level: logging.Warn, Tag: "watchdog", Msg: "exit check: probe timed out, keeping the tunnel up"},
		{Time: at(2), Level: logging.Error, Tag: "tunnel", Msg: "dial de2.edge.example:443 attempt 2/4 failed: connection reset (retrying)"},
		{Time: at(1), Level: logging.Debug, Tag: "exitcheck", Msg: "exit ip 203.0.113.7 (Germany)"},
	}
}
