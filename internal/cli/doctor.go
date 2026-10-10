package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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
	"github.com/benyamin-git/kumiho/internal/settings"
	"github.com/benyamin-git/kumiho/internal/version"
)

// checkStatus classifies a single doctor check.
type checkStatus string

const (
	statusOK   checkStatus = "ok"
	statusWarn checkStatus = "warn"
	statusFail checkStatus = "fail"
	statusSkip checkStatus = "skip"
)

// check is one preflight diagnostic result.
type check struct {
	Name        string      `json:"name"`
	Status      checkStatus `json:"status"`
	Detail      string      `json:"detail"`
	Remediation string      `json:"remediation,omitempty"`
}

// doctorReport is the machine-readable output of `kumiho doctor --json`.
type doctorReport struct {
	Version string  `json:"version"`
	Host    string  `json:"host"`
	OS      string  `json:"os"`
	Time    string  `json:"time"`
	Checks  []check `json:"checks"`
	Failed  int     `json:"failed"`
	Warned  int     `json:"warned"`
	Skipped int     `json:"skipped"`
}

func runDoctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit machine-readable JSON")
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: kumiho doctor [--json]\n\nRuns preflight diagnostics and prints remediation steps for failures.\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	checks := collectChecks()
	failed, warned, skipped := summarize(checks)
	report := doctorReport{
		Version: version.String(),
		Host:    hostname(),
		OS:      runtime.GOOS + "/" + runtime.GOARCH,
		Time:    time.Now().UTC().Format(time.RFC3339),
		Checks:  checks,
		Failed:  failed,
		Warned:  warned,
		Skipped: skipped,
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		renderReport(stdout, report)
	}
	return exitCodeFor(failed)
}

func collectChecks() []check {
	return []check{
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
}

func skipped(name, detail string) check {
	return check{Name: name, Status: statusSkip, Detail: detail}
}

func summarize(checks []check) (failed, warned, skipped int) {
	for _, c := range checks {
		switch c.Status {
		case statusFail:
			failed++
		case statusWarn:
			warned++
		case statusSkip:
			skipped++
		}
	}
	return failed, warned, skipped
}

func exitCodeFor(failed int) int {
	if failed > 0 {
		return 1
	}
	return 0
}

func renderReport(w io.Writer, r doctorReport) {
	fmt.Fprintf(w, "kumiho doctor - %s\n", r.Version)
	fmt.Fprintf(w, "host: %s (%s)\n", r.Host, r.OS)
	fmt.Fprintf(w, "time: %s\n\n", r.Time)

	for _, c := range r.Checks {
		fmt.Fprintf(w, "  %-4s %s: %s\n", statusLabel(c.Status), c.Name, c.Detail)
		if c.Remediation != "" && c.Status != statusOK && c.Status != statusSkip {
			fmt.Fprintf(w, "       -> %s\n", c.Remediation)
		}
	}

	failed, warned, skipped := summarize(r.Checks)
	ok := len(r.Checks) - failed - warned - skipped
	fmt.Fprintf(w, "\nSummary: %d ok, %d warn, %d fail, %d skipped\n", ok, warned, failed, skipped)
	if failed > 0 {
		fmt.Fprint(w, "Some checks failed - follow the remediation steps above.\n")
	}
}

func statusLabel(s checkStatus) string {
	switch s {
	case statusOK:
		return "OK"
	case statusWarn:
		return "WARN"
	case statusFail:
		return "FAIL"
	case statusSkip:
		return "SKIP"
	default:
		return "?"
	}
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
func checkPlatform() check {
	const name = "platform"
	provider := auto.Current().Name()
	if provider == "linux" {
		return check{Name: name, Status: statusOK, Detail: provider}
	}
	return check{
		Name:   name,
		Status: statusWarn,
		Detail: provider + ": full tunnel is not implemented yet (proxy-only mode works)",
	}
}

func checkTUNDevice() check {
	const name = "tun-device"
	if runtime.GOOS != "linux" {
		return skipped(name, "/dev/net/tun is Linux-only")
	}
	fi, err := os.Stat("/dev/net/tun")
	if err != nil {
		return check{
			Name:        name,
			Status:      statusFail,
			Detail:      "/dev/net/tun not found",
			Remediation: "Load the tun module (sudo modprobe tun) or enable TUN/TAP on the VPS; in containers run with --device /dev/net/tun.",
		}
	}
	if fi.Mode()&os.ModeCharDevice == 0 {
		return check{
			Name:        name,
			Status:      statusFail,
			Detail:      "/dev/net/tun is not a character device",
			Remediation: "Recreate it: sudo mknod /dev/net/tun c 10 200 && sudo chmod 666 /dev/net/tun",
		}
	}
	return check{Name: name, Status: statusOK, Detail: fmt.Sprintf("/dev/net/tun present (mode %s)", fi.Mode().Perm())}
}

func checkNetAdmin() check {
	const name = "cap-net-admin"
	if runtime.GOOS != "linux" {
		return skipped(name, "Linux capabilities are Linux-only")
	}
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return check{Name: name, Status: statusWarn, Detail: fmt.Sprintf("cannot read /proc/self/status: %v", err)}
	}
	caps, ok := parseCapEff(string(data))
	if !ok {
		return check{Name: name, Status: statusWarn, Detail: "CapEff not found in /proc/self/status"}
	}
	const capNetAdmin = 12
	if caps&(1<<capNetAdmin) != 0 {
		return check{Name: name, Status: statusOK, Detail: "CAP_NET_ADMIN is in the effective set"}
	}
	return check{
		Name:        name,
		Status:      statusFail,
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

func checkNftables() check {
	const name = "nftables"
	if runtime.GOOS != "linux" {
		return skipped(name, "nf_tables is Linux-only")
	}
	if _, err := exec.LookPath("nft"); err != nil {
		return check{
			Name:        name,
			Status:      statusFail,
			Detail:      "nft binary not found",
			Remediation: "Install nftables: sudo apt install nftables",
		}
	}
	ver, err := runCmd(5*time.Second, "nft", "--version")
	if err != nil {
		return check{
			Name:        name,
			Status:      statusFail,
			Detail:      fmt.Sprintf("nft --version failed: %v", err),
			Remediation: "Reinstall nftables: sudo apt install --reinstall nftables",
		}
	}
	out, err := runCmd(5*time.Second, "nft", "list", "tables")
	if err != nil {
		msg := strings.ToLower(out + " " + err.Error())
		if strings.Contains(msg, "not permitted") || strings.Contains(msg, "permission") {
			return check{
				Name:        name,
				Status:      statusWarn,
				Detail:      fmt.Sprintf("%s; cannot list tables as this user (daemon holds CAP_NET_ADMIN)", ver),
				Remediation: "Run 'sudo kumiho doctor' for a root-level probe.",
			}
		}
		return check{
			Name:        name,
			Status:      statusFail,
			Detail:      fmt.Sprintf("%s; 'nft list tables' failed: %v", ver, err),
			Remediation: "Verify kernel nf_tables support (CONFIG_NF_TABLES) and that the nftables package matches the running kernel.",
		}
	}
	return check{Name: name, Status: statusOK, Detail: ver + "; kernel nf_tables operational"}
}

func checkSystemdResolved() check {
	const name = "systemd-resolved"
	if runtime.GOOS != "linux" {
		return skipped(name, "systemd-resolved is Linux-only")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return skipped(name, "systemd/systemctl not available")
	}
	state, err := runCmd(5*time.Second, "systemctl", "is-active", "systemd-resolved")
	if err != nil {
		return check{
			Name:        name,
			Status:      statusFail,
			Detail:      fmt.Sprintf("systemd-resolved is %q", state),
			Remediation: "Enable it: sudo systemctl enable --now systemd-resolved",
		}
	}
	if _, err := exec.LookPath("resolvectl"); err != nil {
		return check{
			Name:        name,
			Status:      statusFail,
			Detail:      "systemd-resolved is active but resolvectl is missing",
			Remediation: "Install it: sudo apt install systemd-resolved",
		}
	}
	return check{Name: name, Status: statusOK, Detail: "systemd-resolved active; resolvectl available"}
}

func checkDNSManager() check {
	const name = "dns-manager"
	if runtime.GOOS != "linux" {
		return skipped(name, "Linux DNS layout is Linux-only")
	}
	target, err := os.Readlink("/etc/resolv.conf")
	if err != nil {
		if _, serr := os.Stat("/etc/resolv.conf"); serr != nil {
			return check{
				Name:        name,
				Status:      statusWarn,
				Detail:      "/etc/resolv.conf not found",
				Remediation: "Install and enable systemd-resolved. Kumiho never writes /etc/resolv.conf.",
			}
		}
		return check{
			Name:        name,
			Status:      statusWarn,
			Detail:      "/etc/resolv.conf is not a symlink (DNS is not managed by systemd-resolved)",
			Remediation: "DNS queries may leak outside the tunnel. After enabling systemd-resolved, link it: sudo ln -sf ../run/systemd/resolve/stub-resolv.conf /etc/resolv.conf (Kumiho will not do this for you).",
		}
	}
	if strings.Contains(target, "systemd") {
		return check{Name: name, Status: statusOK, Detail: fmt.Sprintf("/etc/resolv.conf -> %s", target)}
	}
	return check{
		Name:        name,
		Status:      statusWarn,
		Detail:      fmt.Sprintf("/etc/resolv.conf -> %s (not managed by systemd-resolved)", target),
		Remediation: "DNS queries may leak outside the tunnel; see systemd-resolved setup.",
	}
}

func checkTailscale() check {
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
		return check{Name: name, Status: statusOK, Detail: detail}
	}
	return check{
		Name:        name,
		Status:      statusWarn,
		Detail:      detail + "; missing: " + strings.Join(problems, ", "),
		Remediation: "Check 'tailscale status'; retry once it is healthy. Kumiho never modifies Tailscale marks or tables.",
	}
}

func checkIPv6() check {
	const name = "ipv6"
	if runtime.GOOS != "linux" {
		return skipped(name, "IPv6 stack handling is Linux-only")
	}
	data, err := os.ReadFile("/proc/sys/net/ipv6/conf/all/disable_ipv6")
	if err != nil {
		return check{Name: name, Status: statusOK, Detail: "IPv6 not present in the kernel; nothing to blackhole"}
	}
	if strings.TrimSpace(string(data)) == "1" {
		return check{Name: name, Status: statusOK, Detail: "IPv6 disabled on this host"}
	}
	return check{Name: name, Status: statusOK, Detail: "IPv6 enabled; kumiho will blackhole it (no AAAA stripping)"}
}

func checkDefaultRoute() check {
	const name = "default-route"
	if runtime.GOOS != "linux" {
		return skipped(name, "iproute2 layout is Linux-only")
	}
	out, err := runCmd(5*time.Second, "ip", "-4", "route", "show", "default")
	if err != nil {
		return check{
			Name:        name,
			Status:      statusWarn,
			Detail:      fmt.Sprintf("could not query routes: %v", err),
			Remediation: "Install iproute2: sudo apt install iproute2",
		}
	}
	if out == "" {
		return check{
			Name:        name,
			Status:      statusWarn,
			Detail:      "no IPv4 default route",
			Remediation: "Bring the host online first; kumiho needs a route for bootstrap DNS and the edge.",
		}
	}
	return check{Name: name, Status: statusOK, Detail: strings.Join(strings.Fields(out), " ")}
}

func checkSocksPort() check {
	const name = "socks-port"
	ln, err := net.Listen("tcp", "127.0.0.1:1080")
	if err == nil {
		_ = ln.Close()
		return check{Name: name, Status: statusOK, Detail: "127.0.0.1:1080 is free"}
	}
	if _, serr := os.Stat(settings.DefaultPaths().SocketPath()); serr == nil {
		return check{Name: name, Status: statusWarn, Detail: "127.0.0.1:1080 is in use (kumiho daemon appears to be running)"}
	}
	return check{
		Name:        name,
		Status:      statusFail,
		Detail:      fmt.Sprintf("127.0.0.1:1080 is in use: %v", err),
		Remediation: "Find the owner: ss -ltnp 'sport = :1080'",
	}
}

func checkControlSocket() check {
	const name = "control-socket"
	if runtime.GOOS != "linux" {
		return skipped(name, "control socket is Linux-only")
	}
	fi, err := os.Stat(settings.DefaultPaths().SocketPath())
	if err != nil {
		return skipped(name, "daemon not running")
	}
	if perm := fi.Mode().Perm(); perm != 0o660 {
		return check{
			Name:        name,
			Status:      statusWarn,
			Detail:      fmt.Sprintf("%s has mode %o, want 660", settings.DefaultPaths().SocketPath(), perm),
			Remediation: "Restart the daemon; if the mode persists, report a bug.",
		}
	}
	return check{Name: name, Status: statusOK, Detail: settings.DefaultPaths().SocketPath() + " mode 660"}
}

func checkClockSkew() check {
	const name = "clock-skew"
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get("https://api.accounts.firefox.com/")
	if err != nil {
		return check{
			Name:        name,
			Status:      statusFail,
			Detail:      fmt.Sprintf("cannot reach api.accounts.firefox.com: %v", err),
			Remediation: "Get the host online; Hawk and JWT validation need both reachability and a roughly correct clock.",
		}
	}
	defer resp.Body.Close()

	date := resp.Header.Get("Date")
	if date == "" {
		return check{Name: name, Status: statusWarn, Detail: "server response had no Date header"}
	}
	serverTime, err := http.ParseTime(date)
	if err != nil {
		return check{Name: name, Status: statusWarn, Detail: fmt.Sprintf("cannot parse server Date %q", date)}
	}
	skew := time.Since(serverTime)
	if skew < 0 {
		skew = -skew
	}
	switch {
	case skew > time.Minute:
		return check{
			Name:        name,
			Status:      statusFail,
			Detail:      fmt.Sprintf("clock is off by %s", skew.Round(time.Second)),
			Remediation: "Enable NTP: sudo timedatectl set-ntp true (check systemd-timesyncd or chrony).",
		}
	case skew > 30*time.Second:
		return check{
			Name:        name,
			Status:      statusWarn,
			Detail:      fmt.Sprintf("clock is off by %s", skew.Round(time.Second)),
			Remediation: "Enable NTP before logging in: sudo timedatectl set-ntp true",
		}
	default:
		return check{Name: name, Status: statusOK, Detail: fmt.Sprintf("within %s of Mozilla time", skew.Round(time.Second))}
	}
}

func checkReachability() check {
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
		return check{
			Name:        name,
			Status:      statusFail,
			Detail:      "unreachable: " + strings.Join(unreachable, ", "),
			Remediation: "Check connectivity, DNS and any upstream proxy. FxA, Guardian and Remote Settings must all be reachable.",
		}
	}
	return check{Name: name, Status: statusOK, Detail: "reachable: " + strings.Join(reachable, ", ")}
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Host
}
