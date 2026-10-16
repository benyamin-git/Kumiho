package gui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/benyamin-git/kumiho/internal/serverlist"
)

// frozenActions is the plan's frozen action vocabulary plus the three
// controller-ruled additions of 2026-10-15, written out independently of the
// code-side list so drift on either side fails.
var frozenActions = []string{
	"connect",
	"disconnect",
	"select_location",
	"ping",
	"settings_get",
	"settings_set",
	"login_email",
	"login_password",
	"login_2fa",
	"logout",
	"logs_snapshot",
	"logs_subscribe",
	"logs_unsubscribe",
	"logs_clear",
	"logs_export",
	"open_logs_folder",
	"cleanup",
	"service_start",
	"service_stop",
	"update_check",
	"open_external",
	"quit",
	"window_show",
	// Controller-ruled extension, 2026-10-15.
	"refresh",
	"prefs_set",
	"doctor_run",
}

// TestEncodeStateEnvelopeShape pins the host → UI wire shape for the zero
// state: every optional field is omitted; LocationsLoaded is always present
// (its false must be visible to the UI); the GUI extras Service and Update
// appear as empty objects.
func TestEncodeStateEnvelopeShape(t *testing.T) {
	raw, err := EncodeState(State{})
	if err != nil {
		t.Fatalf("EncodeState: %v", err)
	}
	const want = `{"type":"state","state":{"locations_loaded":false,"logs_loaded":false,"service":{},"update":{}}}`
	if string(raw) != want {
		t.Fatalf("EncodeState() = %s, want %s", raw, want)
	}
}

// TestEncodeStatePopulatedShape pins the populated host → UI wire shape:
// field names, JSON types and order for the richer state the controller
// produces.
func TestEncodeStatePopulatedShape(t *testing.T) {
	st := State{
		Reachable:   true,
		StatusError: "",
		Status: &StatusView{
			Label:         "Connected",
			Connected:     true,
			Authenticated: true,
			Email:         "user@example.com",
			Uptime:        "1m23s",
			Location:      "DE",
			Server:        "de1.example:443",
			KillSwitch:    true,
			ExitIP:        "192.0.2.1",
		},
		Login:           &LoginView{Step: "done"},
		LocationsLoaded: true,
		Locations:       []serverlist.Country{{Code: "DE", Name: "Germany"}},
		RTTs:            map[string]string{"de1.example:443": "42 ms"},
		Notice:          &Notice{Message: "Connected", Tone: "info"},
		Theme:           "dark",
		Accent:          "blue",
		Service:         ServiceState{State: "running"},
		Update:          UpdateInfo{Current: "v0.3.0", Latest: "v0.3.1", URL: "https://example.com/v0.3.1", CheckedAt: "2026-10-16T00:00:00Z"},
	}
	raw, err := EncodeState(st)
	if err != nil {
		t.Fatalf("EncodeState: %v", err)
	}
	const want = `{"type":"state","state":{` +
		`"reachable":true,` +
		`"status":{"label":"Connected","connected":true,"authenticated":true,"email":"user@example.com","uptime":"1m23s","location":"DE","server":"de1.example:443","kill_switch":true,"exit_ip":"192.0.2.1"},` +
		`"login":{"step":"done"},` +
		`"locations_loaded":true,` +
		`"logs_loaded":false,` +
		`"locations":[{"code":"DE","name":"Germany","cities":null}],` +
		`"rtts":{"de1.example:443":"42 ms"},` +
		`"notice":{"message":"Connected","tone":"info"},` +
		`"theme":"dark",` +
		`"accent":"blue",` +
		`"service":{"state":"running"},` +
		`"update":{"current":"v0.3.0","latest":"v0.3.1","url":"https://example.com/v0.3.1","checked_at":"2026-10-16T00:00:00Z"}` +
		`}}`
	if string(raw) != want {
		t.Fatalf("EncodeState() = %s, want %s", raw, want)
	}
}

// TestDecodeActionRoundTrip pins that names survive decoding and that the
// payload is handed on as the original raw JSON bytes.
func TestDecodeActionRoundTrip(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantName    string
		wantPayload string
	}{
		{
			name:        "payload object",
			raw:         `{"type":"action","name":"settings_set","payload":{"key":"kill_switch","value":"off"}}`,
			wantName:    ActionSettingsSet,
			wantPayload: `{"key":"kill_switch","value":"off"}`,
		},
		{
			name:        "empty payload object",
			raw:         `{"type":"action","name":"ping","payload":{}}`,
			wantName:    ActionPing,
			wantPayload: `{}`,
		},
		{
			name:        "payload with escapes",
			raw:         `{"type":"action","name":"logs_export","payload":{"path":"C:\\Users\\me\\kumiho.log"}}`,
			wantName:    ActionLogsExport,
			wantPayload: `{"path":"C:\\Users\\me\\kumiho.log"}`,
		},
		{
			name:     "payload omitted",
			raw:      `{"type":"action","name":"quit"}`,
			wantName: ActionQuit,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeAction([]byte(tt.raw))
			if err != nil {
				t.Fatalf("DecodeAction(%s): %v", tt.raw, err)
			}
			if got.Name != tt.wantName {
				t.Fatalf("Name = %q, want %q", got.Name, tt.wantName)
			}
			if string(got.Payload) != tt.wantPayload {
				t.Fatalf("Payload = %s, want %s", got.Payload, tt.wantPayload)
			}
		})
	}
}

// TestDecodeActionRejects pins every rejected input class; each error must
// name the offending value.
func TestDecodeActionRejects(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{"malformed json", `{"type":`, "decode action envelope"},
		{"not an object", `"action"`, "decode action envelope"},
		{"empty input", ``, "decode action envelope"},
		{"missing type", `{"name":"quit"}`, "type"},
		{"wrong type", `{"type":"state","state":{}}`, `"state"`},
		{"empty name", `{"type":"action","name":""}`, "empty name"},
		{"missing name", `{"type":"action"}`, "empty name"},
		{"unknown name", `{"type":"action","name":"reboot"}`, `"reboot"`},
		{"unknown envelope field", `{"type":"action","name":"quit","extra":1}`, "unknown field"},
		{"trailing data", `{"type":"action","name":"quit"}{"type":"action","name":"quit"}`, "trailing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeAction([]byte(tt.raw))
			if err == nil {
				t.Fatalf("DecodeAction(%s) succeeded, want error", tt.raw)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("DecodeAction(%s) error = %q, want substring %q", tt.raw, err, tt.wantErr)
			}
		})
	}
}

// TestFrozenActionVocabulary pins the list to the plan's 23 names plus the
// three controller-ruled additions: same members, same count, and every one
// decodes.
func TestFrozenActionVocabulary(t *testing.T) {
	if len(frozenActions) != 26 {
		t.Fatalf("frozenActions has %d entries, want 26", len(frozenActions))
	}
	if len(actionNames) != len(frozenActions) {
		t.Fatalf("actionNames has %d entries, want %d", len(actionNames), len(frozenActions))
	}

	got := make(map[string]bool, len(actionNames))
	for _, name := range actionNames {
		got[name] = true
	}
	for _, name := range frozenActions {
		if !got[name] {
			t.Errorf("action %q missing from actionNames", name)
			continue
		}
		raw := fmt.Sprintf(`{"type":"action","name":%q}`, name)
		action, err := DecodeAction([]byte(raw))
		if err != nil {
			t.Errorf("DecodeAction(%s): %v", raw, err)
			continue
		}
		if action.Name != name {
			t.Errorf("DecodeAction(%s).Name = %q, want %q", raw, action.Name, name)
		}
	}
}
