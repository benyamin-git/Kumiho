package mobile

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/doctor"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// stubDoctor replaces the cross-platform probes with a fixed report so the
// Android mixing logic is tested without network access.
func stubDoctor(t *testing.T) {
	t.Helper()
	orig := runDoctor
	runDoctor = func() doctor.Report {
		return doctor.Report{
			Version: "stub-version",
			Host:    "linux-host",
			OS:      "linux/amd64",
			Time:    "2026-01-01T00:00:00Z",
			Checks: []doctor.Check{
				{Name: "platform", Status: doctor.OK, Detail: "linux: full tunnel"},
				{Name: "socks-port", Status: doctor.OK, Detail: "127.0.0.1:1080 is free"},
				{
					Name:        "clock-skew",
					Status:      doctor.Fail,
					Detail:      "clock is off by 2m",
					Remediation: "Enable NTP: sudo timedatectl set-ntp true (check systemd-timesyncd or chrony).",
				},
				{
					Name:        "endpoints",
					Status:      doctor.Fail,
					Detail:      "unreachable: api.accounts.firefox.com",
					Remediation: "Check connectivity, DNS and any upstream proxy. FxA, Guardian and Remote Settings must all be reachable.",
				},
			},
		}
	}
	t.Cleanup(func() { runDoctor = orig })
}

func healthyFacts() doctorFacts {
	return doctorFacts{
		Device:                 "Pixel 9",
		VPNPermission:          true,
		AlwaysOn:               true,
		Lockdown:               false,
		Notifications:          true,
		BatteryOptimized:       false,
		Network:                true,
		DNSServers:             []string{"10.8.0.2"},
		PrivateDNSMode:         "off",
		SocksPortListening:     true,
		HTTPProxyPortListening: false,
		ServiceRunning:         true,
		EngineState:            "CONNECTED",
		SDK:                    34,
	}
}

func checkByName(t *testing.T, r doctor.Report, name string) doctor.Check {
	t.Helper()
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("report has no %q check: %+v", name, r.Checks)
	return doctor.Check{}
}

func TestDoctorReportMixesCrossPlatformAndAndroidChecks(t *testing.T) {
	stubDoctor(t)
	r := buildDoctorReport(healthyFacts(), settings.DefaultConfig())

	if r.Version == "" {
		t.Error("Version is empty")
	}
	if r.Host != "Pixel 9" {
		t.Errorf("Host = %q, want the device fact", r.Host)
	}
	if r.OS != "android/"+runtime.GOARCH {
		t.Errorf("OS = %q, want android/<GOARCH>", r.OS)
	}
	if _, err := time.Parse(time.RFC3339, r.Time); err != nil {
		t.Errorf("Time = %q: %v", r.Time, err)
	}

	for _, dropped := range []string{"platform", "socks-port"} {
		for _, c := range r.Checks {
			if c.Name == dropped {
				t.Errorf("%q check was not dropped", dropped)
			}
		}
	}
	for _, kept := range []string{"clock-skew", "endpoints"} {
		checkByName(t, r, kept)
	}

	clock := checkByName(t, r, "clock-skew")
	if strings.Contains(clock.Remediation, "timedatectl") || !strings.Contains(strings.ToLower(clock.Remediation), "date") {
		t.Errorf("clock-skew remediation = %q, want Android date/time wording", clock.Remediation)
	}
	endpoints := checkByName(t, r, "endpoints")
	if !strings.Contains(endpoints.Remediation, "Private DNS") {
		t.Errorf("endpoints remediation = %q, want Android Private DNS wording", endpoints.Remediation)
	}

	want := map[string]doctor.Status{
		"vpn-permission":       doctor.OK,
		"always-on":            doctor.OK,
		"lockdown":             doctor.OK,
		"notifications":        doctor.OK,
		"battery-optimization": doctor.OK,
		"network":              doctor.OK,
		"dns":                  doctor.OK,
		"local-ports":          doctor.OK,
		"engine-state":         doctor.OK,
	}
	for name, status := range want {
		if got := checkByName(t, r, name); got.Status != status {
			t.Errorf("%s = %q, want %q (%s)", name, got.Status, status, got.Detail)
		}
	}

	failed, warned, skipped := doctor.Summarize(r.Checks)
	if r.Failed != failed || r.Warned != warned || r.Skipped != skipped {
		t.Errorf("counts = %d/%d/%d, want %d/%d/%d", r.Failed, r.Warned, r.Skipped, failed, warned, skipped)
	}
}

func TestDoctorAndroidCheckTable(t *testing.T) {
	stubDoctor(t)
	tests := []struct {
		name   string
		check  string
		status doctor.Status
		modify func(*doctorFacts)
		cfg    func(*settings.Config)
	}{
		{
			name:   "vpn permission missing",
			check:  "vpn-permission",
			status: doctor.Fail,
			modify: func(f *doctorFacts) { f.VPNPermission = false },
		},
		{
			name:   "always-on off",
			check:  "always-on",
			status: doctor.Warn,
			modify: func(f *doctorFacts) { f.AlwaysOn = false },
		},
		{
			name:   "lockdown on is informational",
			check:  "lockdown",
			status: doctor.OK,
			modify: func(f *doctorFacts) { f.Lockdown = true },
		},
		{
			name:   "notifications disabled on api 33",
			check:  "notifications",
			status: doctor.Fail,
			modify: func(f *doctorFacts) { f.Notifications = false },
		},
		{
			name:   "notifications not required before api 33",
			check:  "notifications",
			status: doctor.Skip,
			modify: func(f *doctorFacts) { f.Notifications = false; f.SDK = 32 },
		},
		{
			name:   "battery optimized",
			check:  "battery-optimization",
			status: doctor.Warn,
			modify: func(f *doctorFacts) { f.BatteryOptimized = true },
		},
		{
			name:   "no validated network",
			check:  "network",
			status: doctor.Fail,
			modify: func(f *doctorFacts) { f.Network = false },
		},
		{
			name:   "dns server missing",
			check:  "dns",
			status: doctor.Fail,
			modify: func(f *doctorFacts) { f.DNSServers = []string{"1.1.1.1"} },
		},
		{
			name:   "private dns hostname mode",
			check:  "dns",
			status: doctor.Warn,
			modify: func(f *doctorFacts) { f.PrivateDNSMode = "hostname" },
		},
		{
			name:   "socks listener missing",
			check:  "local-ports",
			status: doctor.Fail,
			modify: func(f *doctorFacts) { f.SocksPortListening = false },
		},
		{
			name:   "http proxy listener missing",
			check:  "local-ports",
			status: doctor.Fail,
			modify: func(f *doctorFacts) { f.HTTPProxyPortListening = false },
			cfg:    func(c *settings.Config) { c.HTTPProxy = true },
		},
		{
			name:   "http proxy not configured needs no listener",
			check:  "local-ports",
			status: doctor.OK,
			modify: func(f *doctorFacts) { f.HTTPProxyPortListening = false },
		},
		{
			name:   "service stopped while connected",
			check:  "engine-state",
			status: doctor.Fail,
			modify: func(f *doctorFacts) { f.ServiceRunning = false },
		},
		{
			name:   "proxy-only counts as active",
			check:  "engine-state",
			status: doctor.OK,
			modify: func(f *doctorFacts) { f.EngineState = "PROXY_ONLY" },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			facts := healthyFacts()
			tc.modify(&facts)
			cfg := settings.DefaultConfig()
			if tc.cfg != nil {
				tc.cfg(&cfg)
			}
			r := buildDoctorReport(facts, cfg)
			if got := checkByName(t, r, tc.check); got.Status != tc.status {
				t.Fatalf("%s = %q (%s), want %q", tc.check, got.Status, got.Detail, tc.status)
			}
		})
	}
}

func TestDoctorSkipsTunnelChecksWhenNotConnected(t *testing.T) {
	stubDoctor(t)
	facts := healthyFacts()
	facts.EngineState = "IDLE"
	r := buildDoctorReport(facts, settings.DefaultConfig())
	for _, name := range []string{"dns", "local-ports", "engine-state"} {
		if got := checkByName(t, r, name); got.Status != doctor.Skip {
			t.Errorf("%s = %q, want skip when idle", name, got.Status)
		}
	}
}

func TestDoctorMissingFactsDoNotPanic(t *testing.T) {
	stubDoctor(t)
	r := buildDoctorReport(doctorFacts{}, settings.DefaultConfig())
	if len(r.Checks) == 0 {
		t.Fatal("no checks in the report")
	}
	for _, name := range []string{"dns", "local-ports", "engine-state"} {
		if got := checkByName(t, r, name); got.Status != doctor.Skip {
			t.Errorf("%s = %q, want skip for missing facts", name, got.Status)
		}
	}
	// Notifications cannot be judged without an SDK fact; it must not fail.
	if got := checkByName(t, r, "notifications"); got.Status != doctor.Skip {
		t.Errorf("notifications = %q, want skip for a missing SDK", got.Status)
	}
}
