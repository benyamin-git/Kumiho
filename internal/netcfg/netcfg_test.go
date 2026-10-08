package netcfg

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func goldenConfig() Config {
	return Config{
		TunName:      "foxy0",
		TunAddr:      "10.8.0.2",
		TunMTU:       8500,
		KillSwitch:   true,
		AllowLAN:     true,
		ExcludeCIDRs: []string{"203.0.113.0/24", "2001:db8::/32"},
		LANSubnets:   []string{"192.168.1.0/24"},
	}
}

func TestNFTConfigGolden(t *testing.T) {
	want := `table inet kumiho {
	chain kumiho_mark {
		type route hook output priority mangle; policy accept;
		meta mark != 0x0 return
		oifname "foxy0" return
		ip daddr 10.8.0.2 return
		ip daddr 100.64.0.0/10 return
		ip6 daddr fd7a:115c:a1e0::/48 return
		ip daddr { 127.0.0.0/8, 169.254.0.0/16, 224.0.0.0/4, 255.255.255.255, 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 192.168.1.0/24, 203.0.113.0/24 } return
		ip6 daddr { ::1/128, fe80::/10, fc00::/7, ff00::/8, 2001:db8::/32 } return
		meta mark set ct mark
		ct state new meta mark set 0x1
		ct state new ct mark set 0x1
	}
	chain kumiho_guard {
		type filter hook output priority filter; policy accept;
		meta mark & 0x1 != 0x1 return
		meta l4proto udp udp dport != 53 drop
	}
}
`
	if got := NFTConfig(goldenConfig()); got != want {
		t.Fatalf("nft script mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestNFTConfigKillSwitchOffOmitsGuard(t *testing.T) {
	cfg := goldenConfig()
	cfg.KillSwitch = false
	got := NFTConfig(cfg)
	if strings.Contains(got, "chain kumiho_guard") {
		t.Fatalf("guard chain present with kill switch off:\n%s", got)
	}
}

func TestRuleCommandsKillSwitchOffSkipsFallbackButCleanupKeepsIt(t *testing.T) {
	cfg := goldenConfig()
	cfg.KillSwitch = false
	for _, cmd := range RuleCommands(cfg) {
		if strings.Contains(strings.Join(cmd, " "), "1001") {
			t.Fatalf("fallback rule installed with kill switch off: %v", cmd)
		}
	}
	// Cleanup must still try to remove the fallback: a session can be
	// established with the kill switch on and torn down after a toggle.
	var lines []string
	for _, cmd := range CleanupCommands(cfg) {
		lines = append(lines, strings.Join(cmd, " "))
	}
	if !strings.Contains(strings.Join(lines, "\n"), "1001") {
		t.Fatalf("cleanup must still remove the fallback table:\n%s", strings.Join(lines, "\n"))
	}
}

func TestNFTConfigAllowLANOffKeepsPrivateRangesTunneled(t *testing.T) {
	cfg := goldenConfig()
	cfg.AllowLAN = false
	cfg.LANSubnets = []string{"192.168.1.0/24"}
	got := NFTConfig(cfg)
	for _, banned := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "192.168.1.0/24"} {
		if strings.Contains(got, banned+",") || strings.Contains(got, banned+" }") {
			t.Fatalf("allow_lan=false must not exclude %s:\n%s", banned, got)
		}
	}
	for _, required := range []string{"127.0.0.0/8", "224.0.0.0/4", "169.254.0.0/16"} {
		if !strings.Contains(got, required) {
			t.Fatalf("loopback/link-local must stay excluded (%s missing):\n%s", required, got)
		}
	}
}

func TestNFTConfigDedupesExcludes(t *testing.T) {
	cfg := goldenConfig()
	cfg.ExcludeCIDRs = []string{"10.0.0.0/8"}
	got := NFTConfig(cfg)
	if strings.Count(got, "10.0.0.0/8") != 1 {
		t.Fatalf("10.0.0.0/8 appears %d times:\n%s", strings.Count(got, "10.0.0.0/8"), got)
	}
}

func TestRuleAndCleanupCommandGolden(t *testing.T) {
	cfg := goldenConfig()

	wantRules := [][]string{
		{"ip", "addr", "replace", "10.8.0.2/32", "dev", "foxy0"},
		{"ip", "link", "set", "foxy0", "up", "mtu", "8500"},
		{"ip", "rule", "add", "fwmark", "0x1/0xff", "lookup", "1000", "priority", "1100"},
		{"ip", "route", "replace", "default", "dev", "foxy0", "table", "1000"},
		{"ip", "-6", "rule", "add", "fwmark", "0x1/0xff", "lookup", "1000", "priority", "1100"},
		{"ip", "-6", "route", "replace", "default", "dev", "foxy0", "table", "1000"},
		{"ip", "rule", "add", "fwmark", "0x1/0xff", "lookup", "1001", "priority", "1200"},
		{"ip", "route", "replace", "blackhole", "default", "table", "1001"},
		{"ip", "-6", "rule", "add", "fwmark", "0x1/0xff", "lookup", "1001", "priority", "1200"},
		{"ip", "-6", "route", "replace", "blackhole", "default", "table", "1001"},
	}
	if got := RuleCommands(cfg); !reflect.DeepEqual(got, wantRules) {
		t.Fatalf("rules = %#v, want %#v", got, wantRules)
	}

	wantCleanup := [][]string{
		{"nft", "delete", "table", "inet", "kumiho"},
		{"ip", "rule", "del", "fwmark", "0x1/0xff", "lookup", "1000", "priority", "1100"},
		{"ip", "-6", "rule", "del", "fwmark", "0x1/0xff", "lookup", "1000", "priority", "1100"},
		{"ip", "route", "del", "default", "dev", "foxy0", "table", "1000"},
		{"ip", "-6", "route", "del", "default", "dev", "foxy0", "table", "1000"},
		{"ip", "route", "flush", "table", "1000"},
		{"ip", "-6", "route", "flush", "table", "1000"},
		{"ip", "rule", "del", "fwmark", "0x1/0xff", "lookup", "1001", "priority", "1200"},
		{"ip", "-6", "rule", "del", "fwmark", "0x1/0xff", "lookup", "1001", "priority", "1200"},
		{"ip", "route", "flush", "table", "1001"},
		{"ip", "-6", "route", "flush", "table", "1001"},
	}
	if got := CleanupCommands(cfg); !reflect.DeepEqual(got, wantCleanup) {
		t.Fatalf("cleanup = %#v, want %#v", got, wantCleanup)
	}

	wantResolved := [][]string{
		{"resolvectl", "dns", "foxy0", "10.8.0.2"},
		{"resolvectl", "domain", "foxy0", "~."},
		{"resolvectl", "default-route", "foxy0", "yes"},
	}
	if got := ResolvedCommands("foxy0", "10.8.0.2"); !reflect.DeepEqual(got, wantResolved) {
		t.Fatalf("resolved = %#v, want %#v", got, wantResolved)
	}
	if got := ResolvedRevertCommand("foxy0"); !reflect.DeepEqual(got, []string{"resolvectl", "revert", "foxy0"}) {
		t.Fatalf("revert = %#v", got)
	}
}

type runCall struct {
	stdin string
	args  []string
}

func TestApplierCleansThenApplies(t *testing.T) {
	var calls []runCall
	var failNft bool
	a := &Applier{
		Logf:      func(string, ...any) {},
		DetectLAN: func() []string { return []string{"192.168.1.0/24"} },
		Run: func(_ context.Context, stdin string, args ...string) (string, error) {
			calls = append(calls, runCall{stdin: stdin, args: args})
			if len(args) == 3 && args[0] == "nft" && args[1] == "-f" && args[2] == "-" {
				if failNft {
					return "syntax error", errors.New("exit 1")
				}
				return "", nil
			}
			if args[0] == "resolvectl" && args[1] == "dns" {
				return "", errors.New("resolved unavailable")
			}
			if args[0] == "ip" && args[1] == "rule" && args[2] == "del" {
				return "RTNETLINK answers: No such file or directory", errors.New("exit 2")
			}
			return "", nil
		},
	}

	cfg := Config{KillSwitch: true, AllowLAN: true}
	if err := a.Apply(context.Background(), cfg); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// Cleanup commands must all run, in order, before the nft apply.
	wantCleanup := append(CleanupCommands(cfg), ResolvedRevertCommand("foxy0"))
	if len(calls) < len(wantCleanup)+1 {
		t.Fatalf("too few calls: %d", len(calls))
	}
	for i, want := range wantCleanup {
		if !reflect.DeepEqual(calls[i].args, want) {
			t.Fatalf("cleanup call %d = %v, want %v", i, calls[i].args, want)
		}
	}
	nftIdx := len(wantCleanup)
	if joined := strings.Join(calls[nftIdx].args, " "); joined != "nft -f -" {
		t.Fatalf("call after cleanup = %q, want nft -f -", joined)
	}

	// The nft script was fed via stdin and includes the detected LAN subnet.
	var nftScript string
	for _, call := range calls {
		if strings.Join(call.args, " ") == "nft -f -" {
			nftScript = call.stdin
		}
	}
	if !strings.Contains(nftScript, "192.168.1.0/24") {
		t.Fatalf("detected LAN subnet missing from nft script:\n%s", nftScript)
	}

	// Rule commands ran in order after the nft apply.
	var tail []string
	for _, call := range calls[nftIdx+1:] {
		tail = append(tail, strings.Join(call.args, " "))
	}
	wantTailPrefix := "ip addr replace 10.8.0.2/32 dev foxy0"
	if len(tail) == 0 || tail[0] != wantTailPrefix {
		t.Fatalf("first post-nft command = %v, want %q", tail, wantTailPrefix)
	}

	// resolvectl failures are non-fatal.
	failNft = true
	if err := a.Apply(context.Background(), cfg); err == nil {
		t.Fatal("nft failure must abort apply")
	}
}

func TestApplierRunTimesOutWedgedCommand(t *testing.T) {
	savedTimeout, savedWarn := commandTimeout, slowCommandWarn
	commandTimeout, slowCommandWarn = 20*time.Millisecond, time.Hour
	defer func() { commandTimeout, slowCommandWarn = savedTimeout, savedWarn }()

	a := &Applier{Run: func(ctx context.Context, _ string, _ ...string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}}
	_, err := a.run(context.Background(), "", "nft", "-f", "-")
	if err == nil || !strings.Contains(err.Error(), "timed out after") {
		t.Fatalf("run error = %v, want a timeout", err)
	}
}

func TestApplierRunWatchdogNamesStuckCommand(t *testing.T) {
	savedTimeout, savedWarn := commandTimeout, slowCommandWarn
	commandTimeout, slowCommandWarn = time.Second, 10*time.Millisecond
	defer func() { commandTimeout, slowCommandWarn = savedTimeout, savedWarn }()

	logged := make(chan string, 1)
	release := make(chan struct{})
	a := &Applier{
		Logf: func(format string, args ...any) {
			select {
			case logged <- fmt.Sprintf(format, args...):
			default:
			}
		},
		Run: func(context.Context, string, ...string) (string, error) {
			<-release
			return "", nil
		},
	}
	done := make(chan error, 1)
	go func() {
		_, err := a.run(context.Background(), "", "ip", "rule", "add", "fwmark", "0x1/0xff")
		done <- err
	}()

	select {
	case msg := <-logged:
		if !strings.Contains(msg, "still running after") || !strings.Contains(msg, "ip rule add") {
			t.Fatalf("watchdog log = %q", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not name the stuck command")
	}
	close(release)
	<-done
}
