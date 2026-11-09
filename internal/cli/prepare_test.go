package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
)

func TestPrepareTextOutput(t *testing.T) {
	expires := time.Now().Add(9*time.Minute + 42*time.Second)
	client := startFakeCLI(t, func(conn *ipc.Conn, env *ipc.Envelope) error {
		return replyFakeCLI(conn, env, ipc.TypePrepare, api.PrepareResult{
			Email:     "user@example.com",
			ExpiresAt: expires.Unix(),
			Locations: 12,
			Servers:   64,
		})
	})
	var stdout, stderr bytes.Buffer
	code := prepareWithClient(context.Background(), client, false, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("prepareWithClient = %d, stderr = %s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"Prepared.", "12 locations", "64 servers", "kumiho connect"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "pass token") || strings.Contains(out, "secret") {
		t.Fatalf("output leaked a token: %s", out)
	}
}

func TestPrepareJSONOutput(t *testing.T) {
	client := startFakeCLI(t, func(conn *ipc.Conn, env *ipc.Envelope) error {
		return replyFakeCLI(conn, env, ipc.TypePrepare, api.PrepareResult{
			Email:     "user@example.com",
			ExpiresAt: 1234,
			Locations: 1,
			Servers:   2,
			RelayUsed: true,
		})
	})
	var stdout, stderr bytes.Buffer
	code := prepareWithClient(context.Background(), client, true, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("prepareWithClient = %d, stderr = %s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{`"expires_at": 1234`, `"servers": 2`, `"relay_used": true`} {
		if !strings.Contains(out, want) {
			t.Fatalf("JSON missing %q:\n%s", want, out)
		}
	}
}

func TestGuideOutputAndArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runGuide(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("runGuide = %d", code)
	}
	out := stdout.String()
	for _, want := range []string{"Blocked region", "Option 1: I have another VPN", "Option 2: Use a relay", "Browser sign-in still needs"} {
		if !strings.Contains(out, want) {
			t.Fatalf("guide missing %q:\n%s", want, out)
		}
	}
	if code := runGuide([]string{"extra"}, &stdout, &stderr); code != 2 {
		t.Fatalf("runGuide with an argument = %d, want 2", code)
	}
}

func TestConnectPrintsRegionGuidance(t *testing.T) {
	for _, code := range []string{api.CodeRegionBlocked, api.CodeBootstrapRequired} {
		t.Run(code, func(t *testing.T) {
			client := startFakeCLI(t, func(conn *ipc.Conn, env *ipc.Envelope) error {
				if env.Type != ipc.TypeConnect {
					return nil
				}
				return replyFakeCLIError(conn, env, &api.RemoteError{Code: code, Message: "blocked"})
			})
			var stdout, stderr bytes.Buffer
			got := connectWithClient(context.Background(), client, connectOptions{}, &stdout, &stderr)
			if got != 1 {
				t.Fatalf("connectWithClient = %d, want 1", got)
			}
			if !strings.Contains(stderr.String(), "kumiho guide") || !strings.Contains(stderr.String(), "kumiho prepare") {
				t.Fatalf("missing guidance:\n%s", stderr.String())
			}
		})
	}
}
