package mobile

import (
	"context"
	"runtime"
	"time"

	"github.com/benyamin-git/kumiho/internal/doctor"
	"github.com/benyamin-git/kumiho/internal/engine"
	"github.com/benyamin-git/kumiho/internal/settings"
	"github.com/benyamin-git/kumiho/internal/version"
)

// tunnelDNSHost is the routed in-tunnel resolver address the VpnService
// installs and the engine answers in userspace. The doctor check reads the
// device's DNS list, so this must stay in sync with the Android provider's
// DNSListenAddr.
const tunnelDNSHost = "10.8.0.1"

// doctorFacts is the Android state Kotlin collects for the doctor command.
// Go cannot see VpnService, notifications or the battery allow-list itself.
type doctorFacts struct {
	Device                 string   `json:"device"`
	VPNPermission          bool     `json:"vpn_permission"`
	AlwaysOn               bool     `json:"always_on"`
	Lockdown               bool     `json:"lockdown"`
	Notifications          bool     `json:"notifications"`
	BatteryOptimized       bool     `json:"battery_optimized"`
	Network                bool     `json:"network"`
	DNSServers             []string `json:"dns_servers"`
	PrivateDNSMode         string   `json:"private_dns_mode"`
	SocksPortListening     bool     `json:"socks_port_listening"`
	HTTPProxyPortListening bool     `json:"http_proxy_port_listening"`
	ServiceRunning         bool     `json:"service_running"`
	EngineState            string   `json:"engine_state"`
	SDK                    int      `json:"sdk"`
}

// runDoctor is a package variable so tests can replace the network probes.
var runDoctor = doctor.Run

// doctorReport runs the cross-platform probes and mixes in the Android facts.
// The report is advisory: it never mutates the engine or the settings.
func (f *facade) doctorReport(ctx context.Context, facts doctorFacts) (doctor.Report, error) {
	if err := ctx.Err(); err != nil {
		return doctor.Report{}, err
	}
	report := buildDoctorReport(facts, f.store.EffectiveConfig())
	if err := ctx.Err(); err != nil {
		return doctor.Report{}, err
	}
	return report, nil
}

// buildDoctorReport drops the Linux-only plumbing checks, rewrites the two
// remediation texts Android users can act on, and appends the Android checks.
func buildDoctorReport(facts doctorFacts, cfg settings.Config) doctor.Report {
	base := runDoctor()
	checks := make([]doctor.Check, 0, len(base.Checks)+9)
	for _, c := range base.Checks {
		switch c.Name {
		case "platform", "socks-port":
			// The engine owns the listener and the Android platform is real.
			continue
		case "clock-skew":
			if c.Remediation != "" {
				c.Remediation = "Set the date and time automatically: Settings > System > Date & time > Set time automatically."
			}
		case "endpoints":
			if c.Remediation != "" {
				c.Remediation = "Check connectivity and Private DNS (Settings > Network & internet > Private DNS). FxA, Guardian and Remote Settings must all be reachable."
			}
		}
		checks = append(checks, c)
	}
	checks = append(checks, androidChecks(facts, cfg)...)

	failed, warned, skipped := doctor.Summarize(checks)
	host := facts.Device
	if host == "" {
		host = "android"
	}
	return doctor.Report{
		Version: version.String(),
		Host:    host,
		OS:      "android/" + runtime.GOARCH,
		Time:    time.Now().UTC().Format(time.RFC3339),
		Checks:  checks,
		Failed:  failed,
		Warned:  warned,
		Skipped: skipped,
	}
}

// fullTunnel reports whether the engine owns a TUN device, which is when the
// tunnel DNS and port checks are meaningful.
func fullTunnel(state string) bool {
	return state == string(engine.StateConnected) || state == string(engine.StateReconnecting)
}

// engineActive reports whether the engine is serving traffic, full tunnel or
// proxy-only.
func engineActive(state string) bool {
	return fullTunnel(state) || state == string(engine.StateProxyOnly)
}

// androidChecks returns the six facts-only checks followed by the three that
// require an active engine.
func androidChecks(f doctorFacts, cfg settings.Config) []doctor.Check {
	checks := []doctor.Check{
		vpnPermissionCheck(f),
		alwaysOnCheck(f),
		lockdownCheck(f),
		notificationsCheck(f),
		batteryOptimizationCheck(f),
		networkCheck(f),
	}
	checks = append(checks, dnsCheck(f), localPortsCheck(f, cfg), engineStateCheck(f))
	return checks
}

func vpnPermissionCheck(f doctorFacts) doctor.Check {
	const name = "vpn-permission"
	if f.VPNPermission {
		return doctor.Check{Name: name, Status: doctor.OK, Detail: "VpnService consent is granted"}
	}
	return doctor.Check{
		Name:        name,
		Status:      doctor.Fail,
		Detail:      "VpnService.prepare would prompt for consent",
		Remediation: "Open Kumiho and approve the VPN connection request.",
	}
}

func alwaysOnCheck(f doctorFacts) doctor.Check {
	const name = "always-on"
	if f.AlwaysOn {
		return doctor.Check{Name: name, Status: doctor.OK, Detail: "Always-on VPN is enabled"}
	}
	return doctor.Check{
		Name:        name,
		Status:      doctor.Warn,
		Detail:      "Always-on VPN is off; the tunnel will not restart after a reboot",
		Remediation: "Enable it in Settings > Network & internet > VPN > Kumiho > Always-on VPN.",
	}
}

func lockdownCheck(f doctorFacts) doctor.Check {
	const name = "lockdown"
	detail := "Lockdown mode is off; Kumiho's own kill switch still blocks leaks"
	if f.Lockdown {
		detail = "Lockdown mode is on; Kumiho does not rely on it for the kill switch"
	}
	return doctor.Check{Name: name, Status: doctor.OK, Detail: detail}
}

func notificationsCheck(f doctorFacts) doctor.Check {
	const name = "notifications"
	switch {
	case f.SDK <= 0:
		return doctor.Check{Name: name, Status: doctor.Skip, Detail: "the Android SDK level is unknown"}
	case f.SDK < 33:
		return doctor.Check{Name: name, Status: doctor.Skip, Detail: "notifications are not required before Android 13 (API 33)"}
	case f.Notifications:
		return doctor.Check{Name: name, Status: doctor.OK, Detail: "notifications are enabled for the foreground service"}
	default:
		return doctor.Check{
			Name:        name,
			Status:      doctor.Fail,
			Detail:      "notifications are disabled, so the foreground service notice is hidden",
			Remediation: "Allow notifications in Settings > Apps > Kumiho > Notifications.",
		}
	}
}

func batteryOptimizationCheck(f doctorFacts) doctor.Check {
	const name = "battery-optimization"
	if !f.BatteryOptimized {
		return doctor.Check{Name: name, Status: doctor.OK, Detail: "battery optimization does not restrict Kumiho"}
	}
	return doctor.Check{
		Name:        name,
		Status:      doctor.Warn,
		Detail:      "Android may throttle the tunnel in the background",
		Remediation: "Set Settings > Apps > Kumiho > Battery > Unrestricted.",
	}
}

func networkCheck(f doctorFacts) doctor.Check {
	const name = "network"
	if f.Network {
		return doctor.Check{Name: name, Status: doctor.OK, Detail: "a validated non-VPN network is available"}
	}
	return doctor.Check{
		Name:        name,
		Status:      doctor.Fail,
		Detail:      "no validated non-VPN network is available",
		Remediation: "Connect to Wi-Fi or mobile data and retry.",
	}
}

func dnsCheck(f doctorFacts) doctor.Check {
	const name = "dns"
	if !fullTunnel(f.EngineState) {
		return doctor.Check{Name: name, Status: doctor.Skip, Detail: "not connected; the tunnel resolver is not installed"}
	}
	if f.PrivateDNSMode == "hostname" {
		return doctor.Check{
			Name:        name,
			Status:      doctor.Warn,
			Detail:      "Private DNS is set to a hostname, which can bypass the tunnel resolver",
			Remediation: "Set Settings > Network & internet > Private DNS to Off or Automatic while connected.",
		}
	}
	for _, server := range f.DNSServers {
		if server == tunnelDNSHost {
			return doctor.Check{Name: name, Status: doctor.OK, Detail: "DNS points at the tunnel resolver " + tunnelDNSHost}
		}
	}
	return doctor.Check{
		Name:        name,
		Status:      doctor.Fail,
		Detail:      "the tunnel resolver " + tunnelDNSHost + " is missing from the device DNS servers",
		Remediation: "Reconnect; if the resolver stays missing, file a bug.",
	}
}

func localPortsCheck(f doctorFacts, cfg settings.Config) doctor.Check {
	const name = "local-ports"
	if !engineActive(f.EngineState) {
		return doctor.Check{Name: name, Status: doctor.Skip, Detail: "not connected; the local listeners are not running"}
	}
	if !f.SocksPortListening {
		return doctor.Check{
			Name:        name,
			Status:      doctor.Fail,
			Detail:      "the SOCKS listener is not accepting connections",
			Remediation: "Reconnect; the engine restarts the listener with the tunnel.",
		}
	}
	if cfg.HTTPProxy && !f.HTTPProxyPortListening {
		return doctor.Check{
			Name:        name,
			Status:      doctor.Fail,
			Detail:      "http_proxy is on but the HTTP proxy listener is not accepting connections",
			Remediation: "Reconnect; if it persists, turn http_proxy off or file a bug.",
		}
	}
	return doctor.Check{Name: name, Status: doctor.OK, Detail: "the local proxy listeners accept connections"}
}

func engineStateCheck(f doctorFacts) doctor.Check {
	const name = "engine-state"
	if !engineActive(f.EngineState) {
		return doctor.Check{Name: name, Status: doctor.Skip, Detail: "the engine is " + stateOrUnknown(f.EngineState)}
	}
	if !f.ServiceRunning {
		return doctor.Check{
			Name:        name,
			Status:      doctor.Fail,
			Detail:      "the service is not running while the engine reports " + f.EngineState,
			Remediation: "Start the foreground service and reconnect.",
		}
	}
	return doctor.Check{Name: name, Status: doctor.OK, Detail: "the engine is " + f.EngineState + " and the service is running"}
}

func stateOrUnknown(state string) string {
	if state == "" {
		return "not started"
	}
	return state
}
