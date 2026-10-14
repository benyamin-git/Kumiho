// Package doctor implements the preflight diagnostics behind `kumiho doctor`.
// It owns the checks, the report struct and the exit-code policy so both the
// CLI and the Windows GUI can reuse them without flag parsing or a writer.
package doctor

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/benyamin-git/kumiho/internal/platform/auto"
	"github.com/benyamin-git/kumiho/internal/version"
)

// Status classifies a single doctor check.
type Status string

const (
	OK   Status = "ok"
	Warn Status = "warn"
	Fail Status = "fail"
	Skip Status = "skip"
)

// Check is one preflight diagnostic result.
type Check struct {
	Name        string `json:"name"`
	Status      Status `json:"status"`
	Detail      string `json:"detail"`
	Remediation string `json:"remediation,omitempty"`
}

// Report is the machine-readable result of a doctor run.
type Report struct {
	Version string  `json:"version"`
	Host    string  `json:"host"`
	OS      string  `json:"os"`
	Time    string  `json:"time"`
	Checks  []Check `json:"checks"`
	Failed  int     `json:"failed"`
	Warned  int     `json:"warned"`
	Skipped int     `json:"skipped"`
}

// Run collects all checks and returns the report. It has no side effects
// beyond running the probes themselves.
func Run() Report {
	checks := collectChecks()
	failed, warned, skipped := Summarize(checks)
	return Report{
		Version: version.String(),
		Host:    hostname(),
		OS:      runtime.GOOS + "/" + runtime.GOARCH,
		Time:    time.Now().UTC().Format(time.RFC3339),
		Checks:  checks,
		Failed:  failed,
		Warned:  warned,
		Skipped: skipped,
	}
}

// Summarize counts the checks by outcome.
func Summarize(checks []Check) (failed, warned, skipped int) {
	for _, c := range checks {
		switch c.Status {
		case Fail:
			failed++
		case Warn:
			warned++
		case Skip:
			skipped++
		}
	}
	return failed, warned, skipped
}

// ExitCode maps a report to the CLI exit code: 1 when any check failed.
func ExitCode(r Report) int {
	if r.Failed > 0 {
		return 1
	}
	return 0
}

func collectChecks() []Check {
	checks := []Check{
		checkPlatform(),
		checkTUNDevice(),
		checkNetAdmin(),
		checkNftables(),
		checkSystemdResolved(),
		checkDNSManager(),
		checkTailscale(),
		checkIPv6(),
		checkDefaultRoute(),
		checkSocksPort(),
		checkControlSocket(),
		checkClockSkew(),
		checkReachability(),
	}
	checks = append(checks, platformChecks()...)
	if runtime.GOOS != "linux" {
		checks = withoutLinuxOnly(checks)
	}
	return checks
}

// linuxOnlyCheckNames are the checks that only make sense on a Linux host.
// Off Linux they are dropped from the report entirely rather than shown as
// skips, so the Windows report lists only cross-platform and Windows checks.
var linuxOnlyCheckNames = map[string]bool{
	"tun-device":       true,
	"cap-net-admin":    true,
	"nftables":         true,
	"systemd-resolved": true,
	"dns-manager":      true,
	"tailscale":        true,
	"ipv6":             true,
	"default-route":    true,
	"control-socket":   true,
}

func skipped(name, detail string) Check {
	return Check{Name: name, Status: Skip, Detail: detail}
}

// withoutLinuxOnly returns the checks with the Linux-only entries removed,
// preserving report order.
func withoutLinuxOnly(checks []Check) []Check {
	kept := make([]Check, 0, len(checks))
	for _, c := range checks {
		if !linuxOnlyCheckNames[c.Name] {
			kept = append(kept, c)
		}
	}
	return kept
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

func runCmd(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// checkPlatform reports the selected platform provider. OSes without a real
// provider run proxy-only; the full tunnel says so explicitly (D14).
func checkPlatform() Check {
	const name = "platform"
	provider := auto.Current().Name()
	if detail, ok := fullTunnelPlatform(provider); ok {
		return Check{Name: name, Status: OK, Detail: detail}
	}
	return Check{
		Name:   name,
		Status: Warn,
		Detail: provider + ": full tunnel is not implemented yet (proxy-only mode works)",
	}
}

func checkTUNDevice() Check {
	const name = "tun-device"
	if runtime.GOOS != "linux" {
		return skipped(name, "/dev/net/tun is Linux-only")
	}
	fi, err := os.Stat("/dev/net/tun")
	if err != nil {
		return Check{
			Name:        name,
			Status:      Fail,
			Detail:      "/dev/net/tun not found",
			Remediation: "Load the tun module (sudo modprobe tun) or enable TUN/TAP on the VPS; in containers run with --device /dev/net/tun.",
		}
	}
	if fi.Mode()&os.ModeCharDevice == 0 {
		return Check{
			Name:        name,
			Status:      Fail,
			Detail:      "/dev/net/tun is not a character device",
			Remediation: "Recreate it: sudo mknod /dev/net/tun c 10 200 && sudo chmod 666 /dev/net/tun",
		}
	}
	return Check{Name: name, Status: OK, Detail: fmt.Sprintf("/dev/net/tun present (mode %s)", fi.Mode().Perm())}
}

func checkNetAdmin() Check {
	const name = "cap-net-admin"
	if runtime.GOOS != "linux" {
		return skipped(name, "Linux capabilities are Linux-only")
	}
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return Check{Name: name, Status: Warn, Detail: fmt.Sprintf("cannot read /proc/self/status: %v", err)}
	}
	caps, ok := parseCapEff(string(data))
	if !ok {
		return Check{Name: name, Status: Warn, Detail: "CapEff not found in /proc/self/status"}
	}
	const capNetAdmin = 12
	if caps&(1<<capNetAdmin) != 0 {
		return Check{Name: name, Status: OK, Detail: "CAP_NET_ADMIN is in the effective set"}
	}
	return Check{
		Name:        name,
		Status:      Fail,
		Detail:      "CAP_NET_ADMIN is missing from the effective set",
		Remediation: "Run as root once to verify, or install the daemon (systemd grants CAP_NET_ADMIN only); check with: sudo capsh --print",
	}
}

// parseCapEff extracts the effective capability bitmask from
// /proc/self/status content.
func parseCapEff(status string) (uint64, bool) {
	sc := bufio.NewScanner(strings.NewReader(status))
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "CapEff:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "CapEff:"))
		v, err := strconv.ParseUint(raw, 16, 64)
		if err != nil {
			return 0, false
		}
		return v, true
	}
	return 0, false
}

func checkNftables() Check {
	const name = "nftables"
	if runtime.GOOS != "linux" {
		return skipped(name, "nf_tables is Linux-only")
	}
	if _, err := exec.LookPath("nft"); err != nil {
		return Check{
			Name:        name,
			Status:      Fail,
			Detail:      "nft binary not found",
			Remediation: "Install nftables: sudo apt install nftables",
		}
	}
	ver, err := runCmd(5*time.Second, "nft", "--version")
	if err != nil {
		return Check{
			Name:        name,
			Status:      Fail,
			Detail:      fmt.Sprintf("nft --version failed: %v", err),
			Remediation: "Reinstall nftables: sudo apt install --reinstall nftables",
		}
	}
	out, err := runCmd(5*time.Second, "nft", "list", "tables")
	if err != nil {
		msg := strings.ToLower(out + " " + err.Error())
		if strings.Contains(msg, "not permitted") || strings.Contains(msg, "permission") {
			return Check{
				Name:        name,
				Status:      Warn,
				Detail:      fmt.Sprintf("%s; cannot list tables as this user (daemon holds CAP_NET_ADMIN)", ver),
				Remediation: "Run 'sudo kumiho doctor' for a root-level probe.",
			}
		}
		return Check{
			Name:        name,
			Status:      Fail,
			Detail:      fmt.Sprintf("%s; 'nft list tables' failed: %v", ver, err),
			Remediation: "Verify kernel nf_tables support (CONFIG_NF_TABLES) and that the nftables package matches the running kernel.",
		}
	}
	return Check{Name: name, Status: OK, Detail: ver + "; kernel nf_tables operational"}
}

func checkSystemdResolved() Check {
	const name = "systemd-resolved"
	if runtime.GOOS != "linux" {
		return skipped(name, "systemd-resolved is Linux-only")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return skipped(name, "systemd/systemctl not available")
	}
	state, err := runCmd(5*time.Second, "systemctl", "is-active", "systemd-resolved")
	if err != nil {
		return Check{
			Name:        name,
			Status:      Fail,
			Detail:      fmt.Sprintf("systemd-resolved is %q", state),
			Remediation: "Enable it: sudo systemctl enable --now systemd-resolved",
		}
	}
	if _, err := exec.LookPath("resolvectl"); err != nil {
		return Check{
			Name:        name,
			Status:      Fail,
			Detail:      "systemd-resolved is active but resolvectl is missing",
			Remediation: "Install it: sudo apt install systemd-resolved",
		}
	}
	return Check{Name: name, Status: OK, Detail: "systemd-resolved active; resolvectl available"}
}

func checkDNSManager() Check {
	const name = "dns-manager"
	if runtime.GOOS != "linux" {
		return skipped(name, "Linux DNS layout is Linux-only")
	}
	target, err := os.Readlink("/etc/resolv.conf")
	if err != nil {
		if _, serr := os.Stat("/etc/resolv.conf"); serr != nil {
			return Check{
				Name:        name,
				Status:      Warn,
				Detail:      "/etc/resolv.conf not found",
				Remediation: "Install and enable systemd-resolved. Kumiho never writes /etc/resolv.conf.",
			}
		}
		return Check{
			Name:        name,
			Status:      Warn,
			Detail:      "/etc/resolv.conf is not a symlink (DNS is not managed by systemd-resolved)",
			Remediation: "DNS queries may leak outside the tunnel. After enabling systemd-resolved, link it: sudo ln -sf ../run/systemd/resolve/stub-resolv.conf /etc/resolv.conf (Kumiho will not do this for you).",
		}
	}
	if strings.Contains(target, "systemd") {
		return Check{Name: name, Status: OK, Detail: fmt.Sprintf("/etc/resolv.conf -> %s", target)}
	}
	return Check{
		Name:        name,
		Status:      Warn,
		Detail:      fmt.Sprintf("/etc/resolv.conf -> %s (not managed by systemd-resolved)", target),
		Remediation: "DNS queries may leak outside the tunnel; see systemd-resolved setup.",
	}
}

func checkTailscale() Check {
	const name = "tailscale"
	if runtime.GOOS != "linux" {
		return skipped(name, "Tailscale integration is Linux-only")
	}
	if _, err := net.InterfaceByName("tailscale0"); err != nil {
		return skipped(name, "Tailscale not detected (kumiho works without it)")
	}
	detail := "interface tailscale0 present"
	var problems []string

	if out, err := runCmd(5*time.Second, "ip", "route", "show", "table", "52"); err == nil && out != "" {
		detail += "; table 52 has routes"
	} else {
		problems = append(problems, "no routes in table 52")
	}
	if out, err := runCmd(5*time.Second, "ip", "rule", "show"); err == nil && strings.Contains(out, "0x80000") {
		detail += "; fwmark 0x80000 rules present"
	} else {
		problems = append(problems, "no 0x80000 fwmark rule")
	}
	if out, err := runCmd(5*time.Second, "resolvectl", "status"); err == nil && strings.Contains(out, "ts.net") {
		detail += "; MagicDNS (~ts.net) registered"
	} else {
		problems = append(problems, "MagicDNS split domain not found")
	}

	if len(problems) == 0 {
		return Check{Name: name, Status: OK, Detail: detail}
	}
	return Check{
		Name:        name,
		Status:      Warn,
		Detail:      detail + "; missing: " + strings.Join(problems, ", "),
		Remediation: "Check 'tailscale status'; retry once it is healthy. Kumiho never modifies Tailscale marks or tables.",
	}
}

func checkIPv6() Check {
	const name = "ipv6"
	if runtime.GOOS != "linux" {
		return skipped(name, "IPv6 stack handling is Linux-only")
	}
	data, err := os.ReadFile("/proc/sys/net/ipv6/conf/all/disable_ipv6")
	if err != nil {
		return Check{Name: name, Status: OK, Detail: "IPv6 not present in the kernel; nothing to blackhole"}
	}
	if strings.TrimSpace(string(data)) == "1" {
		return Check{Name: name, Status: OK, Detail: "IPv6 disabled on this host"}
	}
	return Check{Name: name, Status: OK, Detail: "IPv6 enabled; kumiho will blackhole it (no AAAA stripping)"}
}

func checkDefaultRoute() Check {
	const name = "default-route"
	if runtime.GOOS != "linux" {
		return skipped(name, "iproute2 layout is Linux-only")
	}
	out, err := runCmd(5*time.Second, "ip", "-4", "route", "show", "default")
	if err != nil {
		return Check{
			Name:        name,
			Status:      Warn,
			Detail:      fmt.Sprintf("could not query routes: %v", err),
			Remediation: "Install iproute2: sudo apt install iproute2",
		}
	}
	if out == "" {
		return Check{
			Name:        name,
			Status:      Warn,
			Detail:      "no IPv4 default route",
			Remediation: "Bring the host online first; kumiho needs a route for bootstrap DNS and the edge.",
		}
	}
	return Check{Name: name, Status: OK, Detail: strings.Join(strings.Fields(out), " ")}
}

func checkSocksPort() Check {
	const name = "socks-port"
	ln, err := net.Listen("tcp", "127.0.0.1:1080")
	if err == nil {
		_ = ln.Close()
		return Check{Name: name, Status: OK, Detail: "127.0.0.1:1080 is free"}
	}
	if _, serr := os.Stat(auto.Current().ControlEndpoint()); serr == nil {
		return Check{Name: name, Status: Warn, Detail: "127.0.0.1:1080 is in use (kumiho daemon appears to be running)"}
	}
	return Check{
		Name:        name,
		Status:      Fail,
		Detail:      fmt.Sprintf("127.0.0.1:1080 is in use: %v", err),
		Remediation: "Find the owner: ss -ltnp 'sport = :1080'",
	}
}

func checkControlSocket() Check {
	const name = "control-socket"
	if runtime.GOOS != "linux" {
		return skipped(name, "control socket is Linux-only")
	}
	fi, err := os.Stat(auto.Current().ControlEndpoint())
	if err != nil {
		return skipped(name, "daemon not running")
	}
	if perm := fi.Mode().Perm(); perm != 0o660 {
		return Check{
			Name:        name,
			Status:      Warn,
			Detail:      fmt.Sprintf("%s has mode %o, want 660", auto.Current().ControlEndpoint(), perm),
			Remediation: "Restart the daemon; if the mode persists, report a bug.",
		}
	}
	return Check{Name: name, Status: OK, Detail: auto.Current().ControlEndpoint() + " mode 660"}
}

func checkClockSkew() Check {
	const name = "clock-skew"
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get("https://api.accounts.firefox.com/")
	if err != nil {
		return Check{
			Name:        name,
			Status:      Fail,
			Detail:      fmt.Sprintf("cannot reach api.accounts.firefox.com: %v", err),
			Remediation: "Get the host online; Hawk and JWT validation need both reachability and a roughly correct clock.",
		}
	}
	defer resp.Body.Close()

	date := resp.Header.Get("Date")
	if date == "" {
		return Check{Name: name, Status: Warn, Detail: "server response had no Date header"}
	}
	serverTime, err := http.ParseTime(date)
	if err != nil {
		return Check{Name: name, Status: Warn, Detail: fmt.Sprintf("cannot parse server Date %q", date)}
	}
	skew := time.Since(serverTime)
	if skew < 0 {
		skew = -skew
	}
	switch {
	case skew > time.Minute:
		return Check{
			Name:        name,
			Status:      Fail,
			Detail:      fmt.Sprintf("clock is off by %s", skew.Round(time.Second)),
			Remediation: "Enable NTP: sudo timedatectl set-ntp true (check systemd-timesyncd or chrony).",
		}
	case skew > 30*time.Second:
		return Check{
			Name:        name,
			Status:      Warn,
			Detail:      fmt.Sprintf("clock is off by %s", skew.Round(time.Second)),
			Remediation: "Enable NTP before logging in: sudo timedatectl set-ntp true",
		}
	default:
		return Check{Name: name, Status: OK, Detail: fmt.Sprintf("within %s of Mozilla time", skew.Round(time.Second))}
	}
}

func checkReachability() Check {
	const name = "endpoints"
	targets := []string{
		"https://api.accounts.firefox.com/",
		"https://vpn.mozilla.org/api/v1/fpn/status",
		"https://firefox.settings.services.mozilla.com/v1/buckets/main/collections/vpn-serverlist/records",
	}
	client := &http.Client{Timeout: 8 * time.Second}
	var reachable, unreachable []string
	for _, target := range targets {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			unreachable = append(unreachable, hostOf(target))
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			unreachable = append(unreachable, hostOf(target))
			continue
		}
		resp.Body.Close()
		reachable = append(reachable, fmt.Sprintf("%s (HTTP %d)", hostOf(target), resp.StatusCode))
	}
	if len(unreachable) > 0 {
		return Check{
			Name:        name,
			Status:      Fail,
			Detail:      "unreachable: " + strings.Join(unreachable, ", "),
			Remediation: "Check connectivity, DNS and any upstream proxy. FxA, Guardian and Remote Settings must all be reachable.",
		}
	}
	return Check{Name: name, Status: OK, Detail: "reachable: " + strings.Join(reachable, ", ")}
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Host
}
