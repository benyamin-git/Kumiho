// Package ipc implements the newline-delimited JSON control protocol
// between the kumiho daemon and TUI/CLI clients over a Unix socket
// (PLAN.md §5).
package ipc

import (
	"encoding/json"
	"fmt"
)

// ProtocolVersion is the wire protocol version carried in every envelope.
const ProtocolVersion = 1

// Client → server message types.
const (
	TypeHello           = "hello"
	TypeLoginEmail      = "login_email"
	TypeLoginPassword   = "login_password"
	TypeLogin2FA        = "login_2fa"
	TypeLogout          = "logout"
	TypeConnect         = "connect"
	TypeDisconnect      = "disconnect"
	TypeStatus          = "status"
	TypeLocations       = "locations"
	TypePing            = "ping"
	TypeSelectLocation  = "select_location"
	TypeSettingsGet     = "settings_get"
	TypeSettingsSet     = "settings_set"
	TypeLogsSnapshot    = "logs_snapshot"
	TypeLogsSubscribe   = "logs_subscribe"
	TypeLogsUnsubscribe = "logs_unsubscribe"
	TypeLogsClear       = "logs_clear"
	TypeAccount         = "account"
	TypeShutdown        = "shutdown"
)

// Server → client message types.
const (
	TypeResult      = "result"
	TypeError       = "error"
	TypeLoginState  = "login_state"
	TypeSettings    = "settings"
	TypeLog         = "log"
	TypeLogSnapshot = "log_snapshot"
	TypeAccountInfo = "account_info"
	TypePingResult  = "ping_result"
)

// Envelope wraps every request, response, and event. Requests and responses
// carry an ID; unsolicited events have an empty ID.
type Envelope struct {
	V       int             `json:"v"`
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// NewEnvelope builds an envelope with the current protocol version. A nil
// payload produces an empty payload field.
func NewEnvelope(id, typ string, payload any) (*Envelope, error) {
	env := &Envelope{V: ProtocolVersion, ID: id, Type: typ}
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode %s payload: %w", typ, err)
		}
		env.Payload = raw
	}
	return env, nil
}

// DecodePayload unmarshals the envelope payload into v. An empty payload
// leaves v untouched.
func (e *Envelope) DecodePayload(v any) error {
	if len(e.Payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(e.Payload, v); err != nil {
		return fmt.Errorf("decode %s payload: %w", e.Type, err)
	}
	return nil
}
