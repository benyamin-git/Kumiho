package api

import (
	"encoding/json"
	"reflect"
	"testing"
)

// statusV052 is the v0.5.2 Status wire shape. New fields must stay additive.
type statusV052 struct {
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

func TestStatusKeepsV052Fields(t *testing.T) {
	limit, remaining := int64(1000), int64(800)
	want := statusV052{
		State:         "connected",
		Authenticated: true,
		Email:         "user@example.com",
		Since:         100,
		TokenExpires:  200,
		KillSwitch:    true,
		Autoconnect:   false,
		Location:      "us",
		Server:        "edge.example.com",
		ProxyOnly:     true,
		PassExpires:   300,
		Quota:         &Quota{Limit: &limit, Remaining: &remaining},
		DNS:           "doh",
		IPv6:          "off",
		ExitIP:        "203.0.113.7",
		ExitCountry:   "US",
		Totals:        &ByteCounters{Up: 10, Down: 20},
		Rates:         &ByteRates{Up: 1.5, Down: 2.5},
	}
	status := Status{
		State: want.State, Authenticated: want.Authenticated, Email: want.Email,
		Since: want.Since, TokenExpires: want.TokenExpires, KillSwitch: want.KillSwitch,
		Autoconnect: want.Autoconnect, Location: want.Location, Server: want.Server,
		ProxyOnly: want.ProxyOnly, PassExpires: want.PassExpires, Quota: want.Quota,
		DNS: want.DNS, IPv6: want.IPv6, ExitIP: want.ExitIP, ExitCountry: want.ExitCountry,
		Totals: want.Totals, Rates: want.Rates,
		Accounts:      []AccountSummary{{ID: "a", Email: "other@example.com"}},
		ActiveAccount: "a",
		Login:         &LoginState{Step: "browser", URL: "https://example.com", LoginID: "l"},
	}
	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	var got statusV052
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("old status fields changed:\n got %+v\nwant %+v\njson %s", got, want, raw)
	}
}

type loginStateV052 struct {
	Step               string `json:"step"`
	Email              string `json:"email,omitempty"`
	VerificationMethod string `json:"verification_method,omitempty"`
	VerificationReason string `json:"verification_reason,omitempty"`
	Error              string `json:"error,omitempty"`
}

func TestLoginStateKeepsV052Fields(t *testing.T) {
	want := loginStateV052{
		Step:               "2fa",
		Email:              "user@example.com",
		VerificationMethod: "totp-2fa",
		VerificationReason: "totp",
		Error:              "enter code",
	}
	state := LoginState{
		Step: want.Step, Email: want.Email, Method: "totp",
		VerificationMethod: want.VerificationMethod,
		VerificationReason: want.VerificationReason,
		URL:                "https://example.com/signin",
		LoginID:            "login-1",
		Error:              want.Error,
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var got loginStateV052
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("old login fields changed:\n got %+v\nwant %+v\njson %s", got, want, raw)
	}
}

func TestAccountPayloadRoundTrips(t *testing.T) {
	cases := []struct {
		name     string
		payload  any
		wantKeys []string
	}{
		{
			name:    "login start",
			payload: LoginStartPayload{Method: "browser", Email: "user@example.com", AccountID: "id-1"},
			wantKeys: []string{
				"method", "email", "account_id",
			},
		},
		{
			name:     "login status",
			payload:  LoginStatusPayload{LoginID: "login-1"},
			wantKeys: []string{"login_id"},
		},
		{
			name:     "login cancel",
			payload:  LoginCancelPayload{LoginID: "login-1"},
			wantKeys: []string{"login_id"},
		},
		{
			name: "login complete",
			payload: LoginCompletePayload{
				LoginID:     "login-1",
				RedirectURL: "urn:ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel?code=abc&state=def",
			},
			wantKeys: []string{"login_id", "redirect_url", "code"},
		},
		{
			name:     "login complete legacy code",
			payload:  LoginCompletePayload{LoginID: "login-1", Code: "legacy-code"},
			wantKeys: []string{"login_id", "code"},
		},
		{
			name:     "accounts remove",
			payload:  AccountsRemovePayload{ID: "id-1"},
			wantKeys: []string{"id"},
		},
		{
			name:     "accounts use",
			payload:  AccountsUsePayload{ID: "id-1"},
			wantKeys: []string{"id"},
		},
		{
			name:     "accounts update",
			payload:  AccountsUpdatePayload{ID: "id-1", Label: "Work"},
			wantKeys: []string{"id", "label"},
		},
		{
			name:     "accounts rotation",
			payload:  AccountsRotationPayload{Enabled: true, Threshold: 20},
			wantKeys: []string{"enabled", "rotation_threshold"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.payload)
			if err != nil {
				t.Fatal(err)
			}
			fresh := reflect.New(reflect.TypeOf(tc.payload))
			if err := json.Unmarshal(raw, fresh.Interface()); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(tc.payload, fresh.Elem().Interface()) {
				t.Fatalf("round trip = %+v, want %+v (json %s)", fresh.Elem().Interface(), tc.payload, raw)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			if len(fields) != len(tc.wantKeys) {
				t.Fatalf("keys = %v, want %v", fields, tc.wantKeys)
			}
			for _, key := range tc.wantKeys {
				if _, ok := fields[key]; !ok {
					t.Fatalf("missing key %q in %s", key, raw)
				}
			}
		})
	}
}

func TestAccountsViewRoundTrips(t *testing.T) {
	limit, remaining := int64(100), int64(80)
	want := AccountsView{
		Accounts: []AccountSummary{
			{
				ID: "id-1", Email: "user@example.com", Label: "Home", Method: "browser",
				Active: true, LastUsedAt: 100,
				Quota: &Quota{Limit: &limit, Remaining: &remaining},
			},
			{
				ID: "id-2", Email: "other@example.com", Label: "Work", Method: "password",
				ReauthRequired: true, LastUsedAt: 50,
			},
		},
		Active:     "id-1",
		Rotation:   true,
		Threshold:  20,
		LastMethod: "browser",
		Notice:     "skipping other@example.com: sign in again",
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got AccountsView
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("accounts view round trip:\n got %+v\nwant %+v\njson %s", got, want, raw)
	}
}
