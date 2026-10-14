package doctor

import "testing"

func TestParseCapEff(t *testing.T) {
	status := "Name:\tkumiho\nCapEff:\t0000000000001000\n"
	caps, ok := parseCapEff(status)
	if !ok {
		t.Fatal("parseCapEff did not find CapEff")
	}
	const capNetAdmin = 12
	if caps&(1<<capNetAdmin) == 0 {
		t.Fatalf("CapEff %#x does not include CAP_NET_ADMIN", caps)
	}
	if _, ok := parseCapEff("no capabilities here"); ok {
		t.Fatal("parseCapEff returned ok for content without CapEff")
	}
}

// names extracts the check names in report order; shared by the per-OS tests.
func names(checks []Check) []string {
	out := make([]string, len(checks))
	for i, c := range checks {
		out[i] = c.Name
	}
	return out
}

// TestWithoutLinuxOnlyDropsLinuxChecks pins which checks are Linux-only and
// that filtering them preserves the report order of the rest.
func TestWithoutLinuxOnlyDropsLinuxChecks(t *testing.T) {
	in := []Check{
		{Name: "platform"},
		{Name: "tun-device"},
		{Name: "cap-net-admin"},
		{Name: "nftables"},
		{Name: "systemd-resolved"},
		{Name: "dns-manager"},
		{Name: "tailscale"},
		{Name: "ipv6"},
		{Name: "default-route"},
		{Name: "socks-port"},
		{Name: "control-socket"},
		{Name: "clock-skew"},
		{Name: "endpoints"},
	}
	want := []string{"platform", "socks-port", "clock-skew", "endpoints"}
	got := names(withoutLinuxOnly(in))
	if len(got) != len(want) {
		t.Fatalf("withoutLinuxOnly kept %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("withoutLinuxOnly kept %v, want %v", got, want)
		}
	}
}
