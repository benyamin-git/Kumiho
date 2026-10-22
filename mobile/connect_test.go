package mobile

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/benyamin-git/kumiho/internal/api"
)

// recordingBridge records every DeviceBridge.Configure call so a test can
// prove the VpnService plan is handed over before the engine dials.
type recordingBridge struct {
	mu      sync.Mutex
	configs []string
}

func (b *recordingBridge) Configure(configJSON string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.configs = append(b.configs, configJSON)
	return nil
}

func (b *recordingBridge) AcquireFD(int) (int, error) { return -1, nil }

func (b *recordingBridge) NetworkAvailable() bool { return true }

func (b *recordingBridge) snapshot() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.configs...)
}

func errorMessage(t *testing.T, raw string) string {
	t.Helper()
	var env struct {
		Error *api.RemoteError `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("decode error envelope: %v\n%s", err, raw)
	}
	if env.Error == nil {
		t.Fatalf("expected an error envelope, got %s", raw)
	}
	return env.Error.Message
}

func TestConnectConfiguresTheBridgeBeforeConnecting(t *testing.T) {
	bridge := &recordingBridge{}
	startTest(t, testPaths(t), bridge, newTestListener())

	out := mustCommand(t, "connect", map[string]any{})
	// A signed-out engine stops the connect, but only after the bridge has
	// been configured; a reversed order would record nothing.
	requireErrorCode(t, out, api.CodeNotAuthenticated)

	configs := bridge.snapshot()
	if len(configs) != 1 {
		t.Fatalf("Configure called %d times, want exactly once", len(configs))
	}
	var nc networkConfig
	if err := json.Unmarshal([]byte(configs[0]), &nc); err != nil {
		t.Fatalf("Configure payload is not networkConfig JSON: %v\n%s", err, configs[0])
	}
	if nc.MTU != 1500 {
		t.Errorf("mtu = %d, want the seeded Android default 1500", nc.MTU)
	}
	if !nc.AllowLAN {
		t.Error("allow_lan = false, want the default true")
	}
	if !covers(t, nc.IPv4Excludes, "10.0.0.1") {
		t.Errorf("ipv4_excludes lacks the allow_lan range: %v", nc.IPv4Excludes)
	}
	if !covers(t, nc.IPv4Routes, "10.8.0.1") {
		t.Errorf("ipv4_routes does not carry the DNS host: %v", nc.IPv4Routes)
	}
}

func TestProxyOnlyConnectSkipsTheBridge(t *testing.T) {
	bridge := &recordingBridge{}
	startTest(t, testPaths(t), bridge, newTestListener())

	out := mustCommand(t, "connect", map[string]any{"proxy_only": true})
	requireErrorCode(t, out, api.CodeNotAuthenticated)
	if configs := bridge.snapshot(); len(configs) != 0 {
		t.Fatalf("proxy-only connect called Configure %d times: %v", len(configs), configs)
	}
}

func TestConnectWithoutABridgeMentionsProxyOnly(t *testing.T) {
	startTest(t, testPaths(t), nil, newTestListener())

	out := mustCommand(t, "connect", map[string]any{})
	requireErrorCode(t, out, api.CodeBadRequest)
	if msg := errorMessage(t, out); !strings.Contains(msg, "proxy_only") {
		t.Fatalf("error message = %q, want a proxy_only hint", msg)
	}
}
