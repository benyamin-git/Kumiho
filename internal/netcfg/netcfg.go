// Package netcfg renders and applies the Linux policy-routing, nftables and
// systemd-resolved configuration for the full tunnel (PLAN.md §3.2–3.4).
//
// The generators are pure functions so they run (and are golden-tested) on
// any OS; the exec runner is Linux-only.
package netcfg

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Tunnel defaults (PLAN.md §3.1).
const (
	DefaultTunName = "foxy0"
	DefaultTunAddr = "10.8.0.2"
	DefaultTunMTU  = 8500
	RouteTable     = 1000
	RoutePriority  = 1100
	// FallbackTable/FallbackPriority blackhole marked traffic that cannot
	// resolve to the tunnel route (kill switch, PLAN.md §3.3).
	FallbackTable    = 1001
	FallbackPriority = 1200
	MarkKumiho       = "0x1"
	MarkDaemon       = "0x2"
)

// Config describes one full-tunnel network configuration.
type Config struct {
	TunName      string
	TunAddr      string
	TunMTU       int
	KillSwitch   bool
	AllowLAN     bool
	ExcludeCIDRs []string
	LANSubnets   []string // detected by the caller (never user input)
}

// Normalize fills defaults so callers can pass a zero Config.
func (c Config) normalize() Config {
	if c.TunName == "" {
		c.TunName = DefaultTunName
	}
	if c.TunAddr == "" {
		c.TunAddr = DefaultTunAddr
	}
	if c.TunMTU <= 0 {
		c.TunMTU = DefaultTunMTU
	}
	return c
}

// NFTConfig renders the `nft -f -` script for the `inet kumiho` table.
func NFTConfig(cfg Config) string {
	cfg = cfg.normalize()

	var b strings.Builder
	b.WriteString("table inet kumiho {\n")
	// Chain names are kumiho_-prefixed: bare `mark` is a reserved word in
	// nftables >= 1.0.9 (the meta-mark data type), and `guard` could collide
	// with future keywords too.
	b.WriteString("\tchain kumiho_mark {\n")
	// `type route` makes the kernel re-run the routing decision when the
	// mark changes; without it, marks set in output are ignored by ip rules.
	b.WriteString("\t\ttype route hook output priority mangle; policy accept;\n")
	b.WriteString("\t\tmeta mark != 0x0 return\n")
	fmt.Fprintf(&b, "\t\toifname %q return\n", cfg.TunName)
	fmt.Fprintf(&b, "\t\tip daddr %s return\n", cfg.TunAddr)
	b.WriteString("\t\tip daddr 100.64.0.0/10 return\n")        // Tailscale CGNAT
	b.WriteString("\t\tip6 daddr fd7a:115c:a1e0::/48 return\n") // Tailscale ULA

	v4Always := []string{"127.0.0.0/8", "169.254.0.0/16", "224.0.0.0/4", "255.255.255.255"}
	if cfg.AllowLAN {
		v4Always = append(v4Always, "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16")
		v4Always = appendUnique(v4Always, cfg.LANSubnets...)
	}
	v4Excluded, v6Excluded := splitCIDRs(cfg.ExcludeCIDRs)
	v4Always = appendUnique(v4Always, v4Excluded...)
	if line := nftDaddrSet(v4Always); line != "" {
		fmt.Fprintf(&b, "\t\t%s\n", line)
	}
	v6Always := append([]string{"::1/128", "fe80::/10", "fc00::/7", "ff00::/8"}, v6Excluded...)
	if line := nftDaddrSet(v6Always); line != "" {
		fmt.Fprintf(&b, "\t\t%s\n", strings.Replace(line, "ip daddr", "ip6 daddr", 1))
	}
	// Persist the tunnel mark in conntrack and restore it on every packet of
	// the flow: marking only new packets would leave established flows
	// unmarked, but each packet needs the mark for the ip-rule lookup.
	b.WriteString("\t\tmeta mark set ct mark\n")
	fmt.Fprintf(&b, "\t\tct state new meta mark set %s\n", MarkKumiho)
	fmt.Fprintf(&b, "\t\tct state new ct mark set %s\n", MarkKumiho)
	b.WriteString("\t}\n")

	if cfg.KillSwitch {
		b.WriteString("\tchain kumiho_guard {\n")
		b.WriteString("\t\ttype filter hook output priority filter; policy accept;\n")
		// No oifname check here: the re-route performed by the mark chain is
		// not visible to later chains in the same output hook (verified on
		// Ubuntu 24.04 / nftables 1.0.9 - an `oifname foxy0` rule never
		// matched and the guard dropped every marked packet). Marked traffic
		// cannot leave via another interface: routing rule 1100 -> table 1000
		// is its only path, and the fallback rule 1200 blackholes it when the
		// tunnel route is unavailable (see RuleCommands).
		fmt.Fprintf(&b, "\t\tmeta mark & %s != %s return\n", MarkKumiho, MarkKumiho)
		b.WriteString("\t\tmeta l4proto udp udp dport != 53 drop\n")
		b.WriteString("\t}\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// nftDaddrSet renders `ip daddr { a, b } return`, or "" for an empty set.
func nftDaddrSet(cidrs []string) string {
	if len(cidrs) == 0 {
		return ""
	}
	return fmt.Sprintf("ip daddr { %s } return", strings.Join(cidrs, ", "))
}

// RuleCommands installs the TUN address, policy rules and default routes.
// Commands are idempotent when run after CleanupCommands.
func RuleCommands(cfg Config) [][]string {
	cfg = cfg.normalize()
	table := strconv.Itoa(RouteTable)
	prio := strconv.Itoa(RoutePriority)
	cmds := [][]string{
		{"ip", "addr", "replace", cfg.TunAddr + "/32", "dev", cfg.TunName},
		{"ip", "link", "set", cfg.TunName, "up", "mtu", strconv.Itoa(cfg.TunMTU)},
		{"ip", "rule", "add", "fwmark", MarkKumiho + "/0xff", "lookup", table, "priority", prio},
		{"ip", "route", "replace", "default", "dev", cfg.TunName, "table", table},
		{"ip", "-6", "rule", "add", "fwmark", MarkKumiho + "/0xff", "lookup", table, "priority", prio},
		{"ip", "-6", "route", "replace", "default", "dev", cfg.TunName, "table", table},
	}
	if cfg.KillSwitch {
		// Kill switch at the routing layer (PLAN.md §3.3): marked traffic that
		// cannot resolve to the TUN route falls through rule 1100 into a
		// blackhole table instead of leaking out the main table.
		fb := strconv.Itoa(FallbackTable)
		fbPrio := strconv.Itoa(FallbackPriority)
		cmds = append(cmds,
			[]string{"ip", "rule", "add", "fwmark", MarkKumiho + "/0xff", "lookup", fb, "priority", fbPrio},
			[]string{"ip", "route", "replace", "blackhole", "default", "table", fb},
			[]string{"ip", "-6", "rule", "add", "fwmark", MarkKumiho + "/0xff", "lookup", fb, "priority", fbPrio},
			[]string{"ip", "-6", "route", "replace", "blackhole", "default", "table", fb},
		)
	}
	return cmds
}

// CleanupCommands removes everything Apply installs. Every command is
// best-effort: missing rules/routes are expected on the first run.
func CleanupCommands(cfg Config) [][]string {
	cfg = cfg.normalize()
	table := strconv.Itoa(RouteTable)
	prio := strconv.Itoa(RoutePriority)
	fb := strconv.Itoa(FallbackTable)
	fbPrio := strconv.Itoa(FallbackPriority)
	// The fallback is removed unconditionally: a session can be established
	// with the kill switch on and torn down after a toggle.
	return [][]string{
		{"nft", "delete", "table", "inet", "kumiho"},
		{"ip", "rule", "del", "fwmark", MarkKumiho + "/0xff", "lookup", table, "priority", prio},
		{"ip", "-6", "rule", "del", "fwmark", MarkKumiho + "/0xff", "lookup", table, "priority", prio},
		{"ip", "route", "del", "default", "dev", cfg.TunName, "table", table},
		{"ip", "-6", "route", "del", "default", "dev", cfg.TunName, "table", table},
		{"ip", "route", "flush", "table", table},
		{"ip", "-6", "route", "flush", "table", table},
		{"ip", "rule", "del", "fwmark", MarkKumiho + "/0xff", "lookup", fb, "priority", fbPrio},
		{"ip", "-6", "rule", "del", "fwmark", MarkKumiho + "/0xff", "lookup", fb, "priority", fbPrio},
		{"ip", "route", "flush", "table", fb},
		{"ip", "-6", "route", "flush", "table", fb},
	}
}

// ResolvedCommands configures systemd-resolved to send all queries to the
// in-daemon resolver on the TUN link (PLAN.md §3.4). Tailscale's more
// specific split domains keep working.
func ResolvedCommands(tunName, dnsAddr string) [][]string {
	return [][]string{
		{"resolvectl", "dns", tunName, dnsAddr},
		{"resolvectl", "domain", tunName, "~."},
		{"resolvectl", "default-route", tunName, "yes"},
	}
}

// ResolvedRevertCommand undoes ResolvedCommands.
func ResolvedRevertCommand(tunName string) []string {
	return []string{"resolvectl", "revert", tunName}
}

// Applier runs network configuration commands. Run is injectable for tests;
// on Linux it defaults to exec.
type Applier struct {
	// Logf receives best-effort progress (may be nil).
	Logf func(format string, args ...any)
	// Run executes one command, returning combined output. stdin may be "".
	Run func(ctx context.Context, stdin string, args ...string) (string, error)
	// DetectLAN returns on-link subnets to exclude (Linux implementation).
	DetectLAN func() []string
}

func (a *Applier) logf(format string, args ...any) {
	if a.Logf != nil {
		a.Logf(format, args...)
	}
}

// Apply installs the configuration, starting from a clean slate so crashes
// and reconnects are safe (PLAN.md §3.2/§4.3).
func (a *Applier) Apply(ctx context.Context, cfg Config) error {
	cfg = cfg.normalize()
	if a.DetectLAN != nil {
		cfg.LANSubnets = a.DetectLAN()
	}

	// Idempotent crash recovery: stale artifacts must never stack up.
	a.Cleanup(ctx, cfg)

	if _, err := a.run(ctx, NFTConfig(cfg), "nft", "-f", "-"); err != nil {
		return fmt.Errorf("netcfg: apply nftables: %w%s", err, permissionHint(err))
	}
	for _, cmd := range RuleCommands(cfg) {
		if _, err := a.run(ctx, "", cmd...); err != nil {
			return fmt.Errorf("netcfg: %s: %w%s", strings.Join(cmd, " "), err, permissionHint(err))
		}
	}
	for _, cmd := range ResolvedCommands(cfg.TunName, cfg.TunAddr) {
		if _, err := a.run(ctx, "", cmd...); err != nil {
			// resolved may be absent; the doctor/TUI surface that separately.
			a.logf("resolvectl %s failed (DNS may leak): %v", strings.Join(cmd[1:], " "), err)
		}
	}
	a.logf("network configuration applied (%s, kill switch %v)", cfg.TunName, cfg.KillSwitch)
	return nil
}

// Cleanup removes rules, routes, the nft table and resolved link config.
// It never fails: leftover cleanup happens on every daemon start.
func (a *Applier) Cleanup(ctx context.Context, cfg Config) {
	cfg = cfg.normalize()
	for _, cmd := range CleanupCommands(cfg) {
		if out, err := a.run(ctx, "", cmd...); err != nil {
			a.logf("cleanup: %s: %v (%s)", strings.Join(cmd, " "), err, strings.TrimSpace(out))
		}
	}
	if _, err := a.run(ctx, "", ResolvedRevertCommand(cfg.TunName)...); err != nil {
		a.logf("cleanup: resolvectl revert: %v", err)
	}
}

// Helper commands normally finish in milliseconds. Bound every invocation so a
// wedged nft/ip/resolvectl cannot hang a connect forever, and log the command
// by name while it is still stuck (a post-mortem needs to know which one).
// Vars (not consts) so tests can shorten them.
var (
	commandTimeout  = 30 * time.Second
	slowCommandWarn = 5 * time.Second
)

func (a *Applier) run(ctx context.Context, stdin string, args ...string) (string, error) {
	if a.Run == nil {
		return "", fmt.Errorf("netcfg: no runner configured")
	}
	cmdline := strings.Join(args, " ")
	runCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	done := make(chan struct{})
	// Capture the warning delay for the watcher goroutine: the vars are
	// writable test knobs, and a lingering watcher reading them after a test
	// restores the defaults is a data race (-race flags exactly that).
	warn := slowCommandWarn
	go func() {
		select {
		case <-done:
		case <-time.After(warn):
			a.logf("still running after %s: %s", warn, cmdline)
		}
	}()

	start := time.Now()
	out, err := a.Run(runCtx, stdin, args...)
	close(done)

	if ctx.Err() == nil && runCtx.Err() != nil {
		return out, fmt.Errorf("timed out after %s", commandTimeout)
	}
	if err == nil {
		if d := time.Since(start); d > slowCommandWarn {
			a.logf("finished after %s: %s", d.Round(time.Millisecond), cmdline)
		}
	}
	return out, err
}

// permissionHint explains EPERM failures from nft/ip: file capabilities on
// the daemon do not propagate to exec'd helpers, so those need real root or
// ambient capabilities (systemd AmbientCapabilities).
func permissionHint(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "Operation not permitted") || strings.Contains(msg, "you must be root") {
		return " (hint: nft/ip need CAP_NET_ADMIN and it does not propagate to child processes; start the daemon as root or with ambient capabilities)"
	}
	return ""
}

// splitCIDRs partitions a mixed CIDR list into IPv4 and IPv6 entries.
func splitCIDRs(cidrs []string) (v4, v6 []string) {
	for _, cidr := range cidrs {
		if strings.Contains(cidr, ":") {
			v6 = append(v6, cidr)
		} else {
			v4 = append(v4, cidr)
		}
	}
	sort.Strings(v4)
	sort.Strings(v6)
	return v4, v6
}

func appendUnique(base []string, extra ...string) []string {
	seen := make(map[string]bool, len(base)+len(extra))
	out := base[:0]
	for _, item := range append(append([]string(nil), base...), extra...) {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}
