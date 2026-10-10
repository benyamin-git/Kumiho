// Package api defines the control-plane data transfer objects and error
// codes exchanged between the kumiho daemon and its CLI/TUI clients
// (PLAN.md §5).
package api

import (
	"fmt"

	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

// Error codes for RemoteError.
const (
	CodeBadRequest         = "bad_request"
	CodeNotImplemented     = "not_implemented"
	CodeNotAuthenticated   = "not_authenticated"
	CodeForbidden          = "forbidden"
	CodeUnsupportedSetting = "unsupported_setting"
	CodeInternal           = "internal"
)

// RemoteError is returned by Client.Call for structured daemon errors.
type RemoteError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *RemoteError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Result acknowledges a command.
type Result struct {
	OK   bool   `json:"ok"`
	Note string `json:"note,omitempty"`
}

// HelloPayload is sent by clients on connect.
type HelloPayload struct {
	ClientVersion string `json:"client_version,omitempty"`
}

// HelloResult is the daemon's handshake response.
type HelloResult struct {
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
}

// LoginEmailPayload starts the login state machine.
type LoginEmailPayload struct {
	Email string `json:"email"`
}

// LoginPasswordPayload supplies the password (daemon derives authPW).
type LoginPasswordPayload struct {
	Password string `json:"password"`
}

// Login2FAPayload supplies the emailed code, or an empty code to re-check
// the email-link verification flow.
type Login2FAPayload struct {
	Code string `json:"code"`
}

// LoginState is the reply to every login step.
type LoginState struct {
	Step               string `json:"step"` // email|password|2fa|done
	Email              string `json:"email,omitempty"`
	VerificationMethod string `json:"verification_method,omitempty"`
	VerificationReason string `json:"verification_reason,omitempty"`
	Error              string `json:"error,omitempty"`
}

// Status is the daemon dashboard snapshot (PLAN.md §5).
type Status struct {
	State         string        `json:"state"`
	Authenticated bool          `json:"authenticated"`
	Email         string        `json:"email,omitempty"`
	Since         int64         `json:"since,omitempty"`
	TokenExpires  int64         `json:"token_expires,omitempty"`
	KillSwitch    bool          `json:"kill_switch"`
	Autoconnect   bool          `json:"autoconnect"`
	Location      string        `json:"location,omitempty"`
	Server        string        `json:"server,omitempty"`
	ProxyOnly     bool          `json:"proxy_only,omitempty"`
	PassExpires   int64         `json:"pass_expires,omitempty"`
	Quota         *Quota        `json:"quota,omitempty"`
	DNS           string        `json:"dns,omitempty"`
	IPv6          string        `json:"ipv6,omitempty"`
	ExitIP        string        `json:"exit_ip,omitempty"`
	ExitCountry   string        `json:"exit_country,omitempty"`
	Totals        *ByteCounters `json:"totals,omitempty"`
	Rates         *ByteRates    `json:"rates,omitempty"`
}

// Quota is the account's proxy quota as reported by Guardian headers.
type Quota struct {
	Limit     *int64 `json:"limit,omitempty"`
	Remaining *int64 `json:"remaining,omitempty"`
	Reset     *int64 `json:"reset,omitempty"` // unix seconds
}

// ByteCounters carries per-direction totals for the current tunnel session.
type ByteCounters struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

// ByteRates carries per-second byte rates (2 s samples).
type ByteRates struct {
	Up   float64 `json:"up"`
	Down float64 `json:"down"`
}

// SettingsView is the daemon's runtime settings snapshot.
type SettingsView struct {
	SocksPort            int      `json:"socks_port"`
	MTU                  int      `json:"mtu"`
	ExitCheck            bool     `json:"exit_check"`
	DoHProvider          string   `json:"doh_provider"`
	DNSUpstream          string   `json:"dns_upstream,omitempty"`
	EdgeAddress          string   `json:"edge_address,omitempty"`
	UpstreamProxy        string   `json:"upstream_proxy,omitempty"`
	ExcludeCIDRs         []string `json:"exclude_cidrs,omitempty"`
	AllowLAN             bool     `json:"allow_lan"`
	LogLevel             string   `json:"log_level"`
	RedactTargets        bool     `json:"redact_targets"`
	KillSwitch           string   `json:"kill_switch"`
	Autoconnect          string   `json:"autoconnect"`
	KillSwitchEffective  bool     `json:"kill_switch_effective"`
	AutoconnectEffective bool     `json:"autoconnect_effective"`
	QuotaPollMinutes     int      `json:"quota_poll_minutes"`
	Overridden           []string `json:"overridden,omitempty"`
}

// SettingsSetPayload changes one runtime setting. Reset drops the persisted
// override so the admin config (or built-in default) applies again.
type SettingsSetPayload struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Reset bool   `json:"reset,omitempty"`
}

// LocationsPayload requests the server list.
type LocationsPayload struct {
	Refresh bool `json:"refresh,omitempty"`
}

// PingPayload requests TCP latency for the given hosts.
type PingPayload struct {
	Hosts []string `json:"hosts"`
}

// PingResult carries one latency measurement; nil RTT means failure.
type PingResult struct {
	Host  string   `json:"host"`
	RTTMS *float64 `json:"rtt_ms"`
}

// ConnectPayload is the connect request (used from M3 on).
type ConnectPayload struct {
	LocationCode string `json:"location_code,omitempty"`
	CityCode     string `json:"city_code,omitempty"`
	Server       string `json:"server,omitempty"`
	ProxyOnly    bool   `json:"proxy_only,omitempty"`
}

// LocationsResult is the reply to a locations request.
type LocationsResult struct {
	Countries []serverlist.Country `json:"countries"`
}

// SelectLocationPayload persists a location choice without connecting yet.
type SelectLocationPayload struct {
	CountryCode string `json:"country_code"`
	CityCode    string `json:"city_code,omitempty"`
	Server      string `json:"server"`
}

// LogsSubscribePayload selects the minimum log level to stream.
type LogsSubscribePayload struct {
	Level string `json:"level,omitempty"`
}

// LogsSnapshotPayload selects the minimum level in a retained-logs reply.
type LogsSnapshotPayload struct {
	Level string `json:"level,omitempty"`
}

// LogSnapshot is the reply to logs_snapshot.
type LogSnapshot struct {
	Entries []logging.Entry `json:"entries"`
}

// AccountInfo is the Account screen snapshot (PLAN.md §6): local session
// facts plus a best-effort Guardian entitlement lookup.
type AccountInfo struct {
	Email            string `json:"email,omitempty"`
	UID              string `json:"uid,omitempty"`
	Subscribed       bool   `json:"subscribed"`
	MaxBytes         *int64 `json:"max_bytes,omitempty"`
	LimitedBandwidth bool   `json:"limited_bandwidth,omitempty"`
	QuotaRemaining   *int64 `json:"quota_remaining,omitempty"`
	TokenExpires     int64  `json:"token_expires,omitempty"`
	// Error carries a non-fatal failure of the online entitlement lookup;
	// the local fields above stay valid.
	Error string `json:"error,omitempty"`
}
