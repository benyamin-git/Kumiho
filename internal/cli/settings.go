package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
	"golang.org/x/term"
)

// cliSettingKeys mirrors the TUI order and adds the v0.9.0 region settings.
var cliSettingKeys = []string{
	"kill_switch",
	"autoconnect",
	"log_level",
	"language",
	"redact_targets",
	"exit_check",
	"doh_provider",
	"dns_upstream",
	"edge_address",
	"upstream_proxy",
	"exclude_cidrs",
	"allow_lan",
	"mtu",
	"socks_port",
	"quota_poll_minutes",
	"tls_split",
	"http_proxy",
	"http_proxy_port",
	"solve_challenges",
	"keepalive_profile",
	"allow_locked",
	"relay_url",
	"relay_token_set",
}

var cliBoolSettings = map[string]bool{
	"redact_targets":   true,
	"exit_check":       true,
	"allow_lan":        true,
	"http_proxy":       true,
	"solve_challenges": true,
	"allow_locked":     true,
}

// settingsSecretReader is replaceable in tests. It writes only the prompt.
var settingsSecretReader = func(prompt string, out io.Writer) (string, error) {
	fmt.Fprint(out, prompt)
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(out)
	return string(value), err
}

func runSettings(args []string, stdout, stderr io.Writer) int {
	action := "get"
	key := ""
	value := ""
	asJSON := false
	rest := args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		switch rest[0] {
		case "get", "set", "reset", "help":
			action = rest[0]
			rest = rest[1:]
		}
	}
	if action == "help" {
		printSettingsUsage(stdout)
		return 0
	}
	// Keep the parser deliberately small: after `set`, values may legitimately
	// start with a dash (the relay token prompt uses "-").
	var positional []string
	for _, arg := range rest {
		if arg == "-h" || arg == "--help" {
			printSettingsUsage(stdout)
			return 0
		}
		if action == "get" && arg == "--json" {
			asJSON = true
			continue
		}
		if action == "get" && strings.HasPrefix(arg, "-") {
			fmt.Fprintf(stderr, "kumiho settings: unknown flag %q\n", arg)
			return 2
		}
		positional = append(positional, arg)
	}
	switch action {
	case "get":
		if len(positional) > 1 {
			fmt.Fprintln(stderr, "kumiho settings: usage: kumiho settings get [key] [--json]")
			return 2
		}
		if len(positional) == 1 {
			key = normalizedSettingKey(positional[0])
		}
	case "set":
		if len(positional) < 1 || len(positional) > 2 {
			fmt.Fprintln(stderr, "kumiho settings: usage: kumiho settings set <key> [value]")
			return 2
		}
		key = positional[0]
		if len(positional) == 2 {
			value = positional[1]
		}
	case "reset":
		if len(positional) != 1 {
			fmt.Fprintln(stderr, "kumiho settings: usage: kumiho settings reset <key>")
			return 2
		}
		key = positional[0]
	default:
		fmt.Fprintf(stderr, "kumiho settings: unknown action %q\n", action)
		return 2
	}

	if key != "" && !knownSettingKey(key) {
		fmt.Fprintf(stderr, "kumiho settings: unknown setting %q; known: %s\n", key, strings.Join(knownSettingKeys(), ", "))
		return 2
	}
	if action == "get" && key == "relay_token" {
		key = "relay_token_set"
	}
	if action != "get" && key == "relay_token_set" {
		fmt.Fprintln(stderr, "kumiho settings: relay_token_set is read-only; set or reset relay_token")
		return 2
	}
	if action == "set" && cliBoolSettings[key] {
		v := strings.ToLower(strings.TrimSpace(value))
		if v != "true" && v != "false" {
			fmt.Fprintf(stderr, "kumiho settings: %s accepts true or false\n", key)
			return 2
		}
		value = v
	}

	client, code := dialDaemon(stderr)
	if client == nil {
		return code
	}
	defer client.Close()
	ctx, cancel := callCtx()
	defer cancel()
	return settingsWithClient(ctx, client, action, key, value, asJSON, stdout, stderr)
}

func normalizedSettingKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "relay_token" {
		return "relay_token_set"
	}
	return key
}

func knownSettingKey(key string) bool {
	if key == "relay_token" {
		return true
	}
	for _, candidate := range cliSettingKeys {
		if candidate == key {
			return true
		}
	}
	return false
}

func knownSettingKeys() []string {
	out := append([]string(nil), cliSettingKeys...)
	out = append(out, "relay_token")
	return out
}

func settingsWithClient(ctx context.Context, client *ipc.Client, action, key, value string, asJSON bool, stdout, stderr io.Writer) int {
	switch action {
	case "get":
		env, err := client.Call(ctx, ipc.TypeSettingsGet, nil)
		if err != nil {
			fmt.Fprintf(stderr, "kumiho settings: %v\n", err)
			return 1
		}
		var view api.SettingsView
		if err := env.DecodePayload(&view); err != nil {
			fmt.Fprintf(stderr, "kumiho settings: %v\n", err)
			return 1
		}
		if asJSON {
			enc := json.NewEncoder(stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(view); err != nil {
				fmt.Fprintf(stderr, "kumiho settings: %v\n", err)
				return 1
			}
			return 0
		}
		if key != "" {
			fmt.Fprintf(stdout, "%s = %s\n", key, settingTextValue(view, key))
			return 0
		}
		for _, k := range cliSettingKeys {
			fmt.Fprintf(stdout, "%s = %s\n", k, settingTextValue(view, k))
		}
		return 0

	case "set", "reset":
		if action == "set" && key == "relay_token" && (value == "" || value == "-") {
			secret, err := settingsSecretReader("Relay token: ", stderr)
			if err != nil {
				fmt.Fprintf(stderr, "kumiho settings: relay_token: %v\n", err)
				return 1
			}
			value = strings.TrimSpace(secret)
			if value == "" {
				fmt.Fprintln(stderr, "kumiho settings: relay_token: token must not be empty")
				return 1
			}
		}
		payload := api.SettingsSetPayload{Key: key, Value: value, Reset: action == "reset"}
		if _, err := client.Call(ctx, ipc.TypeSettingsSet, payload); err != nil {
			fmt.Fprintf(stderr, "kumiho settings: %v\n", err)
			return 1
		}
		switch {
		case key == "relay_token" && action == "set":
			fmt.Fprintln(stdout, "relay_token_set = true")
		case key == "relay_token" && action == "reset":
			fmt.Fprintln(stdout, "relay_token_set = false")
		case action == "reset":
			fmt.Fprintf(stdout, "%s = reset\n", key)
		default:
			fmt.Fprintf(stdout, "%s = %s\n", key, value)
		}
		return 0
	default:
		fmt.Fprintf(stderr, "kumiho settings: unknown action %q\n", action)
		return 2
	}
}

func printSettingsUsage(out io.Writer) {
	fmt.Fprint(out, `Usage: kumiho settings [get [key] [--json]]
       kumiho settings set <key> [value]
       kumiho settings reset <key>

Prints or changes runtime settings. relay_token is write-only: omit the value
(or pass -) to enter it without echo.
`)
}

func settingTextValue(view api.SettingsView, key string) string {
	switch key {
	case "kill_switch":
		return onOffText(view.KillSwitchEffective)
	case "autoconnect":
		return onOffText(view.AutoconnectEffective)
	case "log_level":
		return view.LogLevel
	case "language":
		return view.Language
	case "redact_targets":
		return strconv.FormatBool(view.RedactTargets)
	case "exit_check":
		return strconv.FormatBool(view.ExitCheck)
	case "doh_provider":
		return view.DoHProvider
	case "dns_upstream":
		return view.DNSUpstream
	case "edge_address":
		return view.EdgeAddress
	case "upstream_proxy":
		return view.UpstreamProxy
	case "exclude_cidrs":
		return strings.Join(view.ExcludeCIDRs, ", ")
	case "allow_lan":
		return strconv.FormatBool(view.AllowLAN)
	case "mtu":
		return strconv.Itoa(view.MTU)
	case "socks_port":
		return strconv.Itoa(view.SocksPort)
	case "quota_poll_minutes":
		return strconv.Itoa(view.QuotaPollMinutes)
	case "tls_split":
		return view.TLSSplit
	case "http_proxy":
		return strconv.FormatBool(view.HTTPProxy)
	case "http_proxy_port":
		return strconv.Itoa(view.HTTPProxyPort)
	case "solve_challenges":
		return strconv.FormatBool(view.SolveChallenges)
	case "keepalive_profile":
		return view.KeepaliveProfile
	case "allow_locked":
		return strconv.FormatBool(view.AllowLocked)
	case "relay_url":
		return view.RelayURL
	case "relay_token_set":
		return strconv.FormatBool(view.RelayTokenSet)
	}
	return ""
}

func onOffText(v bool) string {
	if v {
		return "on"
	}
	return "off"
}
