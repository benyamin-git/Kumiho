package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
)

func settingsTestView() api.SettingsView {
	return api.SettingsView{
		SocksPort:            1080,
		MTU:                  1500,
		ExitCheck:            true,
		DoHProvider:          "automatic",
		DNSUpstream:          "1.1.1.1",
		AllowLAN:             false,
		LogLevel:             "info",
		RedactTargets:        true,
		KillSwitch:           "on",
		Autoconnect:          "off",
		KillSwitchEffective:  true,
		AutoconnectEffective: false,
		QuotaPollMinutes:     10,
		TLSSplit:             "off",
		HTTPProxy:            false,
		HTTPProxyPort:        8080,
		SolveChallenges:      true,
		KeepaliveProfile:     "standard",
		RelayURL:             "https://relay.example.test",
		RelayTokenSet:        true,
		AllowLocked:          true,
	}
}

func TestSettingsGetTextMasksTheToken(t *testing.T) {
	client := startFakeCLI(t, func(conn *ipc.Conn, env *ipc.Envelope) error {
		if env.Type == ipc.TypeSettingsGet {
			return replyFakeCLI(conn, env, ipc.TypeSettings, settingsTestView())
		}
		return nil
	})
	var out, errOut bytes.Buffer
	code := settingsWithClient(context.Background(), client, "get", "", "", false, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	text := out.String()
	for _, want := range []string{
		"allow_locked = true\n",
		"relay_url = https://relay.example.test\n",
		"relay_token_set = true\n",
		"kill_switch = on\n",
		"mtu = 1500\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("settings missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "relay_token =") {
		t.Fatalf("token value was printed:\n%s", text)
	}
}

func TestSettingsGetRelayTokenIsAnAlias(t *testing.T) {
	client := startFakeCLI(t, func(conn *ipc.Conn, env *ipc.Envelope) error {
		if env.Type == ipc.TypeSettingsGet {
			return replyFakeCLI(conn, env, ipc.TypeSettings, settingsTestView())
		}
		return nil
	})
	var out, errOut bytes.Buffer
	code := settingsWithClient(context.Background(), client, "get", "relay_token_set", "", false, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if out.String() != "relay_token_set = true\n" {
		t.Fatalf("output = %q", out.String())
	}
}

func TestSettingsGetJSONEncodesTheView(t *testing.T) {
	client := startFakeCLI(t, func(conn *ipc.Conn, env *ipc.Envelope) error {
		if env.Type == ipc.TypeSettingsGet {
			return replyFakeCLI(conn, env, ipc.TypeSettings, settingsTestView())
		}
		return nil
	})
	var out, errOut bytes.Buffer
	code := settingsWithClient(context.Background(), client, "get", "", "", true, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	var view map[string]any
	if err := json.Unmarshal(out.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view["allow_locked"] != true || view["relay_token_set"] != true {
		t.Fatalf("JSON = %+v", view)
	}
	if _, ok := view["relay_token"]; ok {
		t.Fatalf("JSON leaked a token field: %s", out.String())
	}
}

func TestSettingsSetAndResetPayloads(t *testing.T) {
	var payloads []api.SettingsSetPayload
	client := startFakeCLI(t, func(conn *ipc.Conn, env *ipc.Envelope) error {
		if env.Type != ipc.TypeSettingsSet {
			return nil
		}
		var p api.SettingsSetPayload
		if err := env.DecodePayload(&p); err != nil {
			return err
		}
		payloads = append(payloads, p)
		return replyFakeCLI(conn, env, ipc.TypeResult, api.Result{OK: true})
	})
	var out, errOut bytes.Buffer
	if code := settingsWithClient(context.Background(), client, "set", "allow_locked", "true", false, &out, &errOut); code != 0 {
		t.Fatalf("set code=%d stderr=%s", code, errOut.String())
	}
	if code := settingsWithClient(context.Background(), client, "reset", "relay_token", "", false, &out, &errOut); code != 0 {
		t.Fatalf("reset code=%d stderr=%s", code, errOut.String())
	}
	if len(payloads) != 2 {
		t.Fatalf("payloads = %+v", payloads)
	}
	if payloads[0] != (api.SettingsSetPayload{Key: "allow_locked", Value: "true"}) {
		t.Fatalf("set payload = %+v", payloads[0])
	}
	if payloads[1] != (api.SettingsSetPayload{Key: "relay_token", Reset: true}) {
		t.Fatalf("reset payload = %+v", payloads[1])
	}
	if !strings.Contains(out.String(), "relay_token_set = false") {
		t.Fatalf("reset output = %q", out.String())
	}
}

func TestSettingsRelayTokenPromptReadsOnceAndNeverEchoes(t *testing.T) {
	old := settingsSecretReader
	t.Cleanup(func() { settingsSecretReader = old })
	reads := 0
	var promptOut bytes.Buffer
	settingsSecretReader = func(prompt string, out io.Writer) (string, error) {
		reads++
		promptOut.WriteString(prompt)
		return "  secret-token\n", nil
	}
	var payload api.SettingsSetPayload
	client := startFakeCLI(t, func(conn *ipc.Conn, env *ipc.Envelope) error {
		if env.Type != ipc.TypeSettingsSet {
			return nil
		}
		if err := env.DecodePayload(&payload); err != nil {
			return err
		}
		return replyFakeCLI(conn, env, ipc.TypeResult, api.Result{OK: true})
	})
	var out, errOut bytes.Buffer
	code := settingsWithClient(context.Background(), client, "set", "relay_token", "-", false, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if reads != 1 {
		t.Fatalf("prompt reads = %d, want 1", reads)
	}
	if payload.Key != "relay_token" || payload.Value != "secret-token" {
		t.Fatalf("payload = %+v", payload)
	}
	combined := out.String() + errOut.String() + promptOut.String()
	if strings.Contains(combined, "secret-token") {
		t.Fatalf("token was echoed: %q", combined)
	}
	if !strings.Contains(promptOut.String(), "Relay token: ") {
		t.Fatalf("prompt = %q", promptOut.String())
	}
	if !strings.Contains(out.String(), "relay_token_set = true") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestSettingsRelayTokenPromptRejectsEmpty(t *testing.T) {
	old := settingsSecretReader
	t.Cleanup(func() { settingsSecretReader = old })
	settingsSecretReader = func(string, io.Writer) (string, error) { return " \n", nil }
	client := startFakeCLI(t, func(conn *ipc.Conn, env *ipc.Envelope) error {
		t.Error("empty token reached the daemon")
		return nil
	})
	var out, errOut bytes.Buffer
	code := settingsWithClient(context.Background(), client, "set", "relay_token", "", false, &out, &errOut)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "token must not be empty") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestSettingsUnknownKeyAndBooleanValidationExitTwo(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runSettings([]string{"get", "bogus"}, &out, &errOut); code != 2 {
		t.Fatalf("unknown key code = %d", code)
	}
	if !strings.Contains(errOut.String(), "known:") {
		t.Fatalf("unknown key stderr = %q", errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := runSettings([]string{"set", "allow_locked", "yes"}, &out, &errOut); code != 2 {
		t.Fatalf("bad boolean code = %d", code)
	}
	if !strings.Contains(errOut.String(), "accepts true or false") {
		t.Fatalf("bad boolean stderr = %q", errOut.String())
	}
}
