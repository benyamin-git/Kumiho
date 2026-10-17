// Package gui defines the transport-agnostic JSON vocabulary of the GUI
// bridge: state envelopes pushed by the host to the webview and action
// envelopes sent by the webview back to the host. The Windows shell
// (WebView2 messages), the dev harness (SSE + POST), and tests all carry
// the same bytes.
package gui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/serverlist"
)

// Envelope types carried by the bridge.
const (
	typeState  = "state"
	typeAction = "action"
)

// UI → host action names, frozen for the whole GUI plan.
const (
	ActionConnect         = "connect"
	ActionDisconnect      = "disconnect"
	ActionSelectLocation  = "select_location"
	ActionPing            = "ping"
	ActionSettingsGet     = "settings_get"
	ActionSettingsSet     = "settings_set"
	ActionLoginEmail      = "login_email"
	ActionLoginPassword   = "login_password"
	ActionLogin2FA        = "login_2fa"
	ActionLogout          = "logout"
	ActionLogsSnapshot    = "logs_snapshot"
	ActionLogsSubscribe   = "logs_subscribe"
	ActionLogsUnsubscribe = "logs_unsubscribe"
	ActionLogsClear       = "logs_clear"
	ActionLogsExport      = "logs_export"
	ActionOpenLogsFolder  = "open_logs_folder"
	ActionCleanup         = "cleanup"
	ActionServiceStart    = "service_start"
	ActionServiceStop     = "service_stop"
	ActionUpdateCheck     = "update_check"
	ActionOpenExternal    = "open_external"
	ActionQuit            = "quit"
	ActionWindowShow      = "window_show"
)

// Action names added by a controller ruling on 2026-10-15, appended after
// the original frozen 23 (whose names and order are untouched): refresh
// re-runs the controller's autonomous loads; prefs_set writes the GUI
// preferences (host handlers land with T1.7/T1.10/T4.2); doctor_run runs the
// diagnostics suite (T3.3).
const (
	ActionRefresh   = "refresh"
	ActionPrefsSet  = "prefs_set"
	ActionDoctorRun = "doctor_run"
)

// actionNames lists every action the bridge accepts, in plan order.
var actionNames = []string{
	ActionConnect,
	ActionDisconnect,
	ActionSelectLocation,
	ActionPing,
	ActionSettingsGet,
	ActionSettingsSet,
	ActionLoginEmail,
	ActionLoginPassword,
	ActionLogin2FA,
	ActionLogout,
	ActionLogsSnapshot,
	ActionLogsSubscribe,
	ActionLogsUnsubscribe,
	ActionLogsClear,
	ActionLogsExport,
	ActionOpenLogsFolder,
	ActionCleanup,
	ActionServiceStart,
	ActionServiceStop,
	ActionUpdateCheck,
	ActionOpenExternal,
	ActionQuit,
	ActionWindowShow,
	ActionRefresh,
	ActionPrefsSet,
	ActionDoctorRun,
}

// knownActions is the set of accepted names, built from actionNames.
var knownActions = func() map[string]struct{} {
	set := make(map[string]struct{}, len(actionNames))
	for _, name := range actionNames {
		set[name] = struct{}{}
	}
	return set
}()

// State is the payload of a host → UI state envelope: the controller's
// projection of the daemon (status, login, settings, account, server list,
// RTTs, logs) plus GUI-local extras (notices, theme, service, doctor,
// update). Every field is optional, so the zero State is an empty object;
// the projection types live in controller.go and doctor.go.
type State struct {
	// Reachable reports the control pipe's reachability: it flips false on a
	// dial failure (the shell shows the Set up Kumiho gate) and true on the
	// next successful request (inventory §behavioral).
	Reachable bool `json:"reachable,omitempty"`

	// StatusError is the failed first status load's error. Until the first
	// status arrives the header reads "Loading..."; once this is set, and
	// the daemon is reachable, it reads "Status unavailable" (inventory
	// §Shell).
	StatusError string `json:"status_error,omitempty"`

	// Status is the daemon status projection; nil until the first snapshot.
	Status *StatusView `json:"status,omitempty"`

	// Login is the daemon-driven sign-in projection; nil until the first
	// login reply.
	Login *LoginView `json:"login,omitempty"`

	// Doctor is the Diagnostics screen's projection of the last doctor run
	// (T3.3); nil until the first run starts.
	Doctor *DoctorView `json:"doctor,omitempty"`

	// Settings is the runtime settings snapshot; nil until loaded.
	Settings *api.SettingsView `json:"settings,omitempty"`

	// Account is the account snapshot; nil until loaded.
	Account *api.AccountInfo `json:"account,omitempty"`

	// LocationsLoaded reports whether the server list has been loaded at
	// all. The Locations screen must distinguish "Loading locations…" from a
	// settled empty list ("No locations loaded", inventory §Locations), and
	// an omitted slice cannot say which; the flag is always present in the
	// envelope.
	LocationsLoaded bool `json:"locations_loaded"`

	// LogsLoaded reports whether the log snapshot has been loaded at all
	// (T3.2). The Logs screen must distinguish "Loading logs..." from a
	// settled empty ring ("No log entries yet."), and an omitted buffer
	// cannot say which; the flag is always present in the envelope, exactly
	// like LocationsLoaded.
	LogsLoaded bool `json:"logs_loaded"`

	// Onboarded reports whether the first-run wizard is finished (T3.4):
	// while false the UI shows the wizard full-screen. It starts from
	// gui.toml (the shell and harness pass prefs.Onboarded into the
	// controller) and flips true when the wizard's `Open Kumiho` persists
	// prefs_set {"onboarded":true}. It is always present in the envelope,
	// exactly like LocationsLoaded: its false gates the whole shell.
	Onboarded bool `json:"onboarded"`

	// Autostart is the persisted Start at login preference (T4.2): the
	// shell seeds it from gui.toml and republishes it after every
	// prefs_set. A pointer keeps false ("off") distinguishable from "not
	// projected" (the harness and tests that do not host the Run entry);
	// the Settings row falls back to its session record when it is absent.
	Autostart *bool `json:"autostart,omitempty"`

	// Locations is the server list; nil until loaded.
	Locations []serverlist.Country `json:"locations,omitempty"`

	// RTTs maps host:port to its test-speed label ("42 ms" or "timeout").
	RTTs map[string]string `json:"rtts,omitempty"`

	// Logs is the retained log buffer (the daemon snapshot merged with live
	// entries, capped at 2000).
	Logs []logging.Entry `json:"logs,omitempty"`

	// Notice is the shared view-model notice (Dashboard, Locations,
	// Onboarding): it persists until another action replaces it.
	Notice *Notice `json:"notice,omitempty"`

	// Screen-local notices per the inventory's status-line rules: 10-second
	// successes (good) or persistent errors (bad).
	LocationsNotice *Notice `json:"locations_notice,omitempty"`
	SettingsNotice  *Notice `json:"settings_notice,omitempty"`
	LogsNotice      *Notice `json:"logs_notice,omitempty"`
	AccountNotice   *Notice `json:"account_notice,omitempty"`

	// DiagnosticsNotice is the Diagnostics screen's notice (T3.3): cleanup
	// and service outcomes, 10-second successes and persistent failures.
	DiagnosticsNotice *Notice `json:"diagnostics_notice,omitempty"`

	// Theme and Accent carry the active house appearance for the webview.
	Theme  string `json:"theme,omitempty"`
	Accent string `json:"accent,omitempty"`

	// Service and Update are GUI-side projections filled by their tasks
	// (service control, the manual update check); they are not protocol
	// types.
	Service ServiceState `json:"service"`
	Update  UpdateInfo   `json:"update"`
}

// Action is a decoded UI → host message: a frozen action name plus the
// still-encoded, action-specific JSON payload. A nil Payload means the
// envelope carried none.
type Action struct {
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// stateEnvelope is the host → UI wire shape.
type stateEnvelope struct {
	Type  string `json:"type"`
	State State  `json:"state"`
}

// actionEnvelope is the UI → host wire shape.
type actionEnvelope struct {
	Type    string          `json:"type"`
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload"`
}

// EncodeState marshals a host → UI state envelope:
// {"type":"state","state":{…}}.
func EncodeState(state State) ([]byte, error) {
	raw, err := json.Marshal(stateEnvelope{Type: typeState, State: state})
	if err != nil {
		return nil, fmt.Errorf("encode state envelope: %w", err)
	}
	return raw, nil
}

// DecodeAction parses a UI → host action envelope:
// {"type":"action","name":"<name>","payload":{…}}. The payload is kept as
// raw bytes. Unknown envelope fields, a type other than "action", a
// missing or empty name, and names outside the frozen set are errors.
func DecodeAction(raw []byte) (Action, error) {
	var env actionEnvelope
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&env); err != nil {
		return Action{}, fmt.Errorf("decode action envelope: %w", err)
	}
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		return Action{}, errors.New("decode action envelope: unexpected trailing data")
	}
	if env.Type != typeAction {
		return Action{}, fmt.Errorf("action envelope type = %q, want %q", env.Type, typeAction)
	}
	if env.Name == "" {
		return Action{}, errors.New("action envelope has an empty name")
	}
	if _, ok := knownActions[env.Name]; !ok {
		return Action{}, fmt.Errorf("unknown action name %q", env.Name)
	}
	return Action{Name: env.Name, Payload: env.Payload}, nil
}
