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
}

// knownActions is the set of accepted names, built from actionNames.
var knownActions = func() map[string]struct{} {
	set := make(map[string]struct{}, len(actionNames))
	for _, name := range actionNames {
		set[name] = struct{}{}
	}
	return set
}()

// State is the payload of a host → UI state envelope. It is deliberately
// empty in this task; the controller snapshot fields arrive with T1.2.
type State struct{}

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
