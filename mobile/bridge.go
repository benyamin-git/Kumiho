package mobile

import (
	"encoding/json"
	"fmt"

	"github.com/benyamin-git/kumiho/internal/api"
)

// DeviceBridge is the Kotlin side of the VpnService handshake. Kotlin owns
// the interface: Configure stores the route/MTU plan, AcquireFD builds the
// interface and returns its detached descriptor, and NetworkAvailable reports
// a validated non-VPN network (never the VPN itself).
type DeviceBridge interface {
	Configure(configJSON string) error
	AcquireFD(mtu int) (int, error)
	NetworkAvailable() bool
}

// Listener receives engine events. Every callback runs on a Go goroutine and
// must not block: Kotlin parses the JSON and updates its flows. Status and
// notices are advisory, never the source of truth; call status for that.
type Listener interface {
	OnStatus(statusJSON string)
	OnLog(entryJSON string)
	OnPing(resultJSON string)
	OnNotice(noticeJSON string)
}

// request is the JSON request envelope.
type request struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// response is the JSON response envelope.
type response struct {
	Type    string `json:"type"`
	Payload any    `json:"payload,omitempty"`
}

// errorEnvelope is the domain-failure form; it carries no type so Kotlin can
// tell it apart by the presence of "error".
type errorEnvelope struct {
	Error api.RemoteError `json:"error"`
}

// notice is one advisory Listener.OnNotice event.
type notice struct {
	Type    string `json:"type"`
	State   string `json:"state,omitempty"`
	Message string `json:"message,omitempty"`
}

// facadeError is a Go error for a facade-level failure; it is never encoded
// into an error envelope.
type facadeError struct {
	code    string
	message string
}

func (e *facadeError) Error() string { return "kumiho mobile: " + e.message }

func encodeResponse(typ string, payload any) (string, error) {
	raw, err := json.Marshal(response{Type: typ, Payload: payload})
	if err != nil {
		return "", fmt.Errorf("kumiho mobile: encode %s response: %w", typ, err)
	}
	return string(raw), nil
}

func encodeError(code, message string) (string, error) {
	raw, err := json.Marshal(errorEnvelope{Error: api.RemoteError{Code: code, Message: message}})
	if err != nil {
		return "", fmt.Errorf("kumiho mobile: encode error response: %w", err)
	}
	return string(raw), nil
}

// badRequest builds the domain error used for invalid payloads and settings.
func badRequest(format string, args ...any) error {
	return &api.RemoteError{Code: api.CodeBadRequest, Message: fmt.Sprintf(format, args...)}
}

// decodePayload unmarshals req.Payload into v. An absent or null payload
// leaves v at its zero value, matching internal/ipc.
func decodePayload(req *request, v any) error {
	if len(req.Payload) == 0 || string(req.Payload) == "null" {
		return nil
	}
	if err := json.Unmarshal(req.Payload, v); err != nil {
		return badRequest("invalid %s payload: %v", req.Type, err)
	}
	return nil
}
