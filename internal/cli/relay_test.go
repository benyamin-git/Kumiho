package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
)

func TestRelayResultOutput(t *testing.T) {
	tests := []struct {
		name string
		res  api.RelayTestResult
		want string
		code int
	}{
		{
			name: "success",
			res:  api.RelayTestResult{OK: true, Status: 200, LatencyMS: floatPtr(12.4)},
			want: "Relay OK — 12 ms\n",
			code: 0,
		},
		{
			name: "rejected",
			res:  api.RelayTestResult{Status: 401, Error: "relay test: HTTP 401"},
			want: "Relay rejected the token\n",
			code: 1,
		},
		{
			name: "forbidden",
			res:  api.RelayTestResult{Status: 403, Error: "relay test: HTTP 403"},
			want: "Relay rejected the token\n",
			code: 1,
		},
		{
			name: "unset",
			res:  api.RelayTestResult{Error: "relay_url is not set"},
			want: "relay_url is not set\n",
			code: 1,
		},
		{
			name: "unreachable",
			res:  api.RelayTestResult{Error: "dial tcp: refused"},
			want: "Relay unreachable: dial tcp: refused\n",
			code: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if code := printRelayResult(&out, tc.res); code != tc.code {
				t.Fatalf("code = %d, want %d", code, tc.code)
			}
			if out.String() != tc.want {
				t.Fatalf("output = %q, want %q", out.String(), tc.want)
			}
		})
	}
}

func TestRelayTestWithClient(t *testing.T) {
	client := startFakeCLI(t, func(conn *ipc.Conn, env *ipc.Envelope) error {
		if env.Type == ipc.TypeRelayTest {
			return replyFakeCLI(conn, env, ipc.TypeRelayTest, api.RelayTestResult{
				OK: true, Status: 200, LatencyMS: floatPtr(7),
			})
		}
		return nil
	})
	var out, errOut bytes.Buffer
	if code := relayTestWithClient(context.Background(), client, &out, &errOut); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if out.String() != "Relay OK — 7 ms\n" {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRelayUnknownCommandExitsTwo(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runRelay([]string{"frobnicate"}, &out, &errOut); code != 2 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(errOut.String(), "unknown command") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func floatPtr(v float64) *float64 { return &v }
