package gui

import (
	"encoding/json"
	"strings"

	"github.com/benyamin-git/kumiho/internal/doctor"
	"github.com/benyamin-git/kumiho/internal/version"
)

// The diagnostics projection (T3.3): doctor.go maps one internal/doctor
// report to the Diagnostics screen's wire shape. It carries the walk-era
// doctorview table (friendly title and one-sentence explanation per check),
// the chip word and tone, and the Copy details clipboard payload. Unmapped
// check names pass through unchanged instead of being dropped, so a check
// added to internal/doctor can never disappear from the screen.

// DoctorCheck is one projected doctor check (inventory §Diagnostics cards):
// the friendly title and one-sentence explanation, the raw status and its
// chip word/tone, and the check's own detail and fix.
type DoctorCheck struct {
	// Name is the stable doctor check name, kept so the raw report stays
	// traceable.
	Name string `json:"name"`
	// Title is the plain-language name; unmapped checks keep the raw name.
	Title string `json:"title"`
	// Explanation is one sentence about what the check verifies; empty for
	// an unmapped check.
	Explanation string `json:"explanation,omitempty"`
	// Status is the raw doctor outcome (ok|warn|fail|skip); the screen shows
	// a fix only for warnings and failures.
	Status string `json:"status"`
	// StatusWord is the chip's word: OK, Warning, Failed or Skipped, so the
	// state never rests on colour alone.
	StatusWord string `json:"status_word"`
	// Tone is the chip's emphasis: good|warn|bad, empty for neutral.
	Tone string `json:"tone,omitempty"`
	// Detail is the check's own result line.
	Detail string `json:"detail,omitempty"`
	// Remediation is what to do when the check is not OK; may be empty.
	Remediation string `json:"remediation,omitempty"`
}

// DoctorView is the Diagnostics screen's projection of the last doctor run
// (T3.3). It is not a protocol type.
type DoctorView struct {
	// Running is true while a run is in flight; the screen shows its
	// `Checking…` placeholder and keeps Re-run inert.
	Running bool `json:"running,omitempty"`
	// Checks is the ordered projection of the report's checks.
	Checks []DoctorCheck `json:"checks,omitempty"`
	// Details is the Copy details clipboard payload: `Kumiho <version>
	// diagnostics`, a blank line, the pretty-printed full report JSON and a
	// trailing newline (inventory §Diagnostics).
	Details string `json:"details,omitempty"`
}

// friendlyCheck is the plain-language presentation of one check name.
type friendlyCheck struct {
	title       string
	explanation string
}

// friendlyChecks maps every check name internal/doctor can produce to its
// friendly title and explanation (the inventory's §Diagnostics list; the
// walk-era doctorview table). doctor_test.go pins the known names, and an
// unmapped name still renders with its raw name.
var friendlyChecks = map[string]friendlyCheck{
	// Cross-platform checks.
	"platform": {
		title:       "Platform",
		explanation: "Tells you which tunnel implementation this PC uses.",
	},
	"socks-port": {
		title:       "Local proxy port",
		explanation: "Checks that the local proxy port (127.0.0.1:1080) is free or already used by Kumiho itself.",
	},
	"clock-skew": {
		title:       "System clock",
		explanation: "Checks that this PC's clock is close enough to Mozilla's time for sign-in to work.",
	},
	"endpoints": {
		title:       "Mozilla servers",
		explanation: "Checks that Mozilla's sign-in and VPN servers can be reached from this PC.",
	},

	// Linux checks (they never appear on Windows).
	"tun-device": {
		title:       "Tunnel device",
		explanation: "Checks that the Linux TUN device Kumiho needs for the tunnel exists.",
	},
	"cap-net-admin": {
		title:       "Network permissions",
		explanation: "Checks that Kumiho may change network settings (root or CAP_NET_ADMIN).",
	},
	"nftables": {
		title:       "Firewall tools",
		explanation: "Checks that the nftables firewall tool used by the kill switch is installed and usable.",
	},
	"systemd-resolved": {
		title:       "DNS service",
		explanation: "Checks that systemd-resolved, which handles DNS inside the tunnel, is active.",
	},
	"dns-manager": {
		title:       "DNS settings",
		explanation: "Checks that system DNS is managed by systemd-resolved, so lookups cannot leak outside the tunnel.",
	},
	"tailscale": {
		title:       "Tailscale",
		explanation: "Checks that the optional Tailscale integration still has everything it needs.",
	},
	"ipv6": {
		title:       "IPv6 handling",
		explanation: "Reports how Kumiho treats IPv6 traffic, which the tunnel cannot carry.",
	},
	"default-route": {
		title:       "Internet route",
		explanation: "Checks that this PC has an IPv4 route for the first connection.",
	},
	"control-socket": {
		title:       "Control socket",
		explanation: "Checks that the local control socket has the expected permissions.",
	},

	// Windows checks.
	"windows-service": {
		title:       "Kumiho service",
		explanation: "Checks that the Kumiho background service is installed and running.",
	},
	"wintun-dll": {
		title:       "Tunnel driver",
		explanation: "Checks that the Wintun driver ships next to the Kumiho program.",
	},
	"wfp-service": {
		title:       "Windows firewall service",
		explanation: "Checks that the Windows Filtering Platform service that powers the kill switch is running.",
	},
	"control-pipe": {
		title:       "Service connection",
		explanation: "Checks that Kumiho can reach its background service over the local pipe.",
	},
	"tun-adapter": {
		title:       "Tunnel adapter",
		explanation: "Checks whether the Kumiho network adapter exists; it only appears while connected.",
	},
	"adapter-dns": {
		title:       "Tunnel DNS",
		explanation: "Checks that the Kumiho adapter uses Kumiho's own DNS while connected.",
	},
	"physical-default-route": {
		title:       "Internet route",
		explanation: "Checks that this PC has a normal internet route for the first connection.",
	},
	"elevation": {
		title:       "Administrator rights",
		explanation: "Checks whether Kumiho is running with administrator rights; those are only needed for repairs.",
	},
}

// projectDoctor maps a finished report to the screen's projection, preserving
// report order, and renders the Copy details payload once per run.
func projectDoctor(r doctor.Report) DoctorView {
	checks := make([]DoctorCheck, 0, len(r.Checks))
	for _, c := range r.Checks {
		checks = append(checks, projectDoctorCheck(c))
	}
	return DoctorView{Checks: checks, Details: doctorDetails(r)}
}

// projectDoctorCheck maps one check; see projectDoctor.
func projectDoctorCheck(c doctor.Check) DoctorCheck {
	f, ok := friendlyChecks[c.Name]
	title, explanation := c.Name, ""
	if ok {
		title, explanation = f.title, f.explanation
	}
	word, tone := doctorChip(c.Status)
	return DoctorCheck{
		Name:        c.Name,
		Title:       title,
		Explanation: explanation,
		Status:      string(c.Status),
		StatusWord:  word,
		Tone:        tone,
		Detail:      c.Detail,
		Remediation: c.Remediation,
	}
}

// doctorChip maps a doctor status to its chip word and tone (the same words
// the walk era and the onboarding health step use).
func doctorChip(s doctor.Status) (string, string) {
	switch s {
	case doctor.OK:
		return "OK", toneGood
	case doctor.Warn:
		return "Warning", toneWarn
	case doctor.Fail:
		return "Failed", toneBad
	case doctor.Skip:
		return "Skipped", ""
	default:
		return strings.ToUpper(string(s)), ""
	}
}

// doctorDetails renders the Copy details clipboard payload: the app version
// followed by the full report JSON (inventory §Diagnostics).
func doctorDetails(r doctor.Report) string {
	header := "Kumiho diagnostics"
	if version.Version != "" {
		header = "Kumiho " + version.Version + " diagnostics"
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return header
	}
	return header + "\n\n" + string(data) + "\n"
}
