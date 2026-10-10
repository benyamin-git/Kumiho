//go:build windows

package windows

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/benyamin-git/kumiho/internal/platform"
)

// --- fixtures ---------------------------------------------------------------

func mustCIDR(t *testing.T, cidr string) net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatalf("ParseCIDR(%q): %v", cidr, err)
	}
	return *n
}

// fakeWinNet records every mutation so Apply/Cleanup logic can be tested
// without elevation or the Wintun driver. Deletions report not-found, which
// is exactly what a second cleanup against a clean table sees.
type fakeWinNet struct {
	luid      uint64
	luidErr   error
	adlist    []adapterInfo
	routelist []windows.MibIpForwardRow2

	createdAddrs  []windows.MibUnicastIpAddressRow
	deletedAddrs  []windows.MibUnicastIpAddressRow
	mtuCalls      []mtuCall
	createdRoutes []windows.MibIpForwardRow2
	deletedRoutes []windows.MibIpForwardRow2
	dnsCalls      []dnsCall
}

type mtuCall struct {
	luid   uint64
	family uint16
	mtu    int
}

type dnsCall struct {
	guid    windows.GUID
	servers []string
}

func (f *fakeWinNet) aliasToLUID(string) (uint64, error) { return f.luid, f.luidErr }
func (f *fakeWinNet) adapters() ([]adapterInfo, error)   { return f.adlist, nil }
func (f *fakeWinNet) routes() ([]windows.MibIpForwardRow2, error) {
	return f.routelist, nil
}
func (f *fakeWinNet) createAddress(row *windows.MibUnicastIpAddressRow) error {
	f.createdAddrs = append(f.createdAddrs, *row)
	return nil
}
func (f *fakeWinNet) deleteAddress(row *windows.MibUnicastIpAddressRow) error {
	f.deletedAddrs = append(f.deletedAddrs, *row)
	return windows.ERROR_NOT_FOUND
}
func (f *fakeWinNet) setMTU(luid uint64, family uint16, mtu int) error {
	f.mtuCalls = append(f.mtuCalls, mtuCall{luid: luid, family: family, mtu: mtu})
	return nil
}
func (f *fakeWinNet) createRoute(row *windows.MibIpForwardRow2) error {
	f.createdRoutes = append(f.createdRoutes, *row)
	return nil
}
func (f *fakeWinNet) deleteRoute(row *windows.MibIpForwardRow2) error {
	f.deletedRoutes = append(f.deletedRoutes, *row)
	return windows.ERROR_NOT_FOUND
}
func (f *fakeWinNet) setDNS(guid windows.GUID, servers []string) error {
	f.dnsCalls = append(f.dnsCalls, dnsCall{guid: guid, servers: servers})
	return nil
}

// --- pure builders ----------------------------------------------------------

func TestAdapterGUIDStable(t *testing.T) {
	const want = "{3B511063-BADE-458C-9DAE-5D21B4F0C040}"
	if got := fixedAdapterGUID.String(); got != want {
		t.Fatalf("fixedAdapterGUID = %s, want %s (changing it breaks NLA stability across restarts)", got, want)
	}
}

func TestNormalizeSpec(t *testing.T) {
	got := normalizeSpec(platform.NetConfigSpec{})
	if got.TunName == "" || got.TunAddr == "" || got.TunMTU <= 0 {
		t.Fatalf("normalizeSpec(zero) = %+v, want filled defaults", got)
	}
	spec := platform.NetConfigSpec{TunName: "foxy0", TunAddr: "10.8.0.2", TunMTU: 8500}
	if got := normalizeSpec(spec); got.TunName != spec.TunName || got.TunAddr != spec.TunAddr || got.TunMTU != spec.TunMTU {
		t.Fatalf("normalizeSpec(%+v) = %+v, want unchanged", spec, got)
	}
}

func TestUnicastRow(t *testing.T) {
	row := unicastRow(0x42, net.ParseIP("10.8.0.2"), 32)
	if row.InterfaceLuid != 0x42 || row.OnLinkPrefixLength != 32 {
		t.Fatalf("row = %+v, want luid 0x42 prefix 32", row)
	}
	if row.Address.Family != windows.AF_INET {
		t.Fatalf("Address.Family = %d, want AF_INET", row.Address.Family)
	}
	if got := [4]byte(row.Address.Addr[:4]); got != [4]byte{10, 8, 0, 2} {
		t.Fatalf("Address = %v, want 10.8.0.2", got)
	}
	if row.DadState != windows.IpDadStatePreferred {
		t.Fatalf("DadState = %d, want preferred", row.DadState)
	}
	if row.ValidLifetime != 0xffffffff || row.PreferredLifetime != 0xffffffff {
		t.Fatalf("lifetimes = %d/%d, want infinite", row.ValidLifetime, row.PreferredLifetime)
	}

	row6 := unicastRow(0x43, net.ParseIP("fd7a:115c:a1e0::1"), 128)
	if row6.Address.Family != windows.AF_INET6 || row6.OnLinkPrefixLength != 128 {
		t.Fatalf("v6 row = %+v, want AF_INET6/128", row6)
	}
	wantV6 := net.ParseIP("fd7a:115c:a1e0::1").To16()
	if got := [16]byte(row6.Address.Addr); got != [16]byte(wantV6) {
		t.Fatalf("v6 Address = %v, want %v", got, wantV6)
	}
}

func TestRouteRow(t *testing.T) {
	row := routeRow(0x42, net.IPv4(100, 64, 0, 0), 10, net.IPv4(192, 168, 1, 1), bypassRouteMetric)
	if row.InterfaceLuid != 0x42 || row.DestinationPrefix.PrefixLength != 10 || row.Metric != bypassRouteMetric {
		t.Fatalf("row = %+v, want luid 0x42 prefix 10 metric %d", row, bypassRouteMetric)
	}
	if row.ValidLifetime != 0xffffffff || row.PreferredLifetime != 0xffffffff {
		t.Fatalf("lifetimes = %d/%d, want infinite", row.ValidLifetime, row.PreferredLifetime)
	}
	dst := (*windows.RawSockaddrInet4)(unsafe.Pointer(&row.DestinationPrefix.Prefix))
	if dst.Family != windows.AF_INET || dst.Addr != [4]byte{100, 64, 0, 0} {
		t.Fatalf("destination = family %d addr %v, want AF_INET 100.64.0.0", dst.Family, dst.Addr)
	}
	nh := (*windows.RawSockaddrInet4)(unsafe.Pointer(&row.NextHop))
	if nh.Family != windows.AF_INET || nh.Addr != [4]byte{192, 168, 1, 1} {
		t.Fatalf("next hop = family %d addr %v, want AF_INET 192.168.1.1", nh.Family, nh.Addr)
	}

	row6 := routeRow(0x43, net.ParseIP("fd7a:115c:a1e0::"), 48, net.ParseIP("2001:db8::1"), bypassRouteMetric)
	dst6 := (*windows.RawSockaddrInet6)(unsafe.Pointer(&row6.DestinationPrefix.Prefix))
	if dst6.Family != windows.AF_INET6 || dst6.Addr != [16]byte(net.ParseIP("fd7a:115c:a1e0::").To16()) {
		t.Fatalf("v6 destination = family %d addr %v", dst6.Family, dst6.Addr)
	}
	if row6.DestinationPrefix.PrefixLength != 48 {
		t.Fatalf("v6 prefix = %d, want 48", row6.DestinationPrefix.PrefixLength)
	}
}

func TestDefaultRouteRow(t *testing.T) {
	row4 := defaultRouteRow(0x42, windows.AF_INET, tunnelRouteMetric)
	if row4.DestinationPrefix.PrefixLength != 0 || row4.Metric != tunnelRouteMetric {
		t.Fatalf("v4 default = %+v, want /0 metric %d", row4, tunnelRouteMetric)
	}
	nh4 := (*windows.RawSockaddrInet4)(unsafe.Pointer(&row4.NextHop))
	if nh4.Family != windows.AF_INET || nh4.Addr != [4]byte{} {
		t.Fatalf("v4 next hop = %v, want 0.0.0.0", nh4.Addr)
	}
	row6 := defaultRouteRow(0x42, windows.AF_INET6, tunnelRouteMetric)
	nh6 := (*windows.RawSockaddrInet6)(unsafe.Pointer(&row6.NextHop))
	if nh6.Family != windows.AF_INET6 || nh6.Addr != [16]byte{} {
		t.Fatalf("v6 next hop = %v, want ::", nh6.Addr)
	}
	if (*windows.RawSockaddrInet6)(unsafe.Pointer(&row6.DestinationPrefix.Prefix)).Family != windows.AF_INET6 {
		t.Fatal("v6 default destination is not AF_INET6")
	}
}

func TestDNSRequest(t *testing.T) {
	req, err := dnsRequest([]string{"10.8.0.2"})
	if err != nil {
		t.Fatalf("dnsRequest: %v", err)
	}
	if req.Version != dnsSettingsVersion1 || req.Flags != dnsSettingNameServer {
		t.Fatalf("version/flags = %d/%#x, want %d/%#x", req.Version, req.Flags, dnsSettingsVersion1, dnsSettingNameServer)
	}
	if got := windows.UTF16PtrToString(req.NameServer); got != "10.8.0.2" {
		t.Fatalf("NameServer = %q, want 10.8.0.2", got)
	}

	clear, err := dnsRequest(nil)
	if err != nil {
		t.Fatalf("dnsRequest(nil): %v", err)
	}
	if got := windows.UTF16PtrToString(clear.NameServer); got != "" {
		t.Fatalf("clear NameServer = %q, want empty", got)
	}
}

// --- target and route selection ---------------------------------------------

func TestStaticTargets(t *testing.T) {
	spec := platform.NetConfigSpec{ExcludeCIDRs: []string{"203.0.113.0/24", "fd00::/8", "not-a-cidr"}}
	got := staticTargets(spec)
	want := []string{"100.64.0.0/10", "fd7a:115c:a1e0::/48", "203.0.113.0/24", "fd00::/8"}
	if len(got) != len(want) {
		t.Fatalf("staticTargets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i].String() != want[i] {
			t.Fatalf("staticTargets[%d] = %s, want %s", i, got[i].String(), want[i])
		}
	}
}

func TestLanBypass(t *testing.T) {
	adapters := []adapterInfo{
		{luid: 7, operUp: true, addresses: []net.IPNet{mustCIDR(t, "10.8.0.2/32")}},
		{luid: 22, operUp: false, addresses: []net.IPNet{mustCIDR(t, "192.168.9.0/24")}},
		{luid: 33, operUp: true, ifType: windows.IF_TYPE_SOFTWARE_LOOPBACK, addresses: []net.IPNet{mustCIDR(t, "127.0.0.0/8")}},
		{luid: 44, operUp: true, addresses: []net.IPNet{
			mustCIDR(t, "192.168.1.0/24"),
			mustCIDR(t, "10.0.0.5/32"),
			mustCIDR(t, "172.16.4.0/30"),
			mustCIDR(t, "192.168.1.5/31"),
		}},
		{luid: 55, operUp: true, addresses: []net.IPNet{mustCIDR(t, "192.168.1.0/24")}},
	}
	rows := lanBypass(adapters, 7)
	if len(rows) != 2 {
		t.Fatalf("lanBypass = %d rows, want 2: %v", len(rows), rows)
	}
	got := map[string]bool{}
	for _, row := range rows {
		if row.InterfaceLuid != 44 || row.Metric != bypassRouteMetric {
			t.Fatalf("row = %+v, want luid 44 metric %d", row, bypassRouteMetric)
		}
		if nh := (*windows.RawSockaddrInet4)(unsafe.Pointer(&row.NextHop)); nh.Addr != [4]byte{} {
			t.Fatalf("LAN bypass next hop = %v, want on-link 0.0.0.0", nh.Addr)
		}
		got[fmt.Sprintf("%s/%d", sockaddrIP(&row.DestinationPrefix.Prefix), row.DestinationPrefix.PrefixLength)] = true
	}
	if !got["192.168.1.0/24"] || !got["172.16.4.0/30"] {
		t.Fatalf("lanBypass prefixes = %v, want 192.168.1.0/24 and 172.16.4.0/30", got)
	}
}

func TestPhysicalDefaultPrefersLowestEffectiveMetric(t *testing.T) {
	adapters := []adapterInfo{
		{luid: 7, operUp: true, metric4: 0},
		{luid: 22, operUp: true, metric4: 25},
		{luid: 33, operUp: true, metric4: 5},
	}
	routes := []windows.MibIpForwardRow2{
		routeRow(7, net.IPv4zero, 0, net.IPv4zero, 0),
		routeRow(22, net.IPv4zero, 0, net.IPv4(192, 168, 1, 1), 0),
		routeRow(33, net.IPv4zero, 0, net.IPv4(192, 168, 2, 1), 5),
	}
	luid, nextHop, ok := physicalDefault(routes, adapters, 7, windows.AF_INET)
	if !ok || luid != 33 || nextHop.String() != "192.168.2.1" {
		t.Fatalf("physicalDefault = (%#x, %v, %v), want (0x21, 192.168.2.1, true)", luid, nextHop, ok)
	}

	if _, _, ok := physicalDefault(routes, adapters, 7, windows.AF_INET6); ok {
		t.Fatal("physicalDefault for IPv6 succeeded without an IPv6 default route")
	}
}

func TestPhysicalBypassSkipsMissingFamily(t *testing.T) {
	targets := []net.IPNet{mustCIDR(t, "100.64.0.0/10"), mustCIDR(t, "fd7a:115c:a1e0::/48")}
	routes := []windows.MibIpForwardRow2{
		routeRow(22, net.IPv4zero, 0, net.IPv4(192, 168, 1, 1), 0),
	}
	adapters := []adapterInfo{{luid: 22, operUp: true, metric4: 25}}
	rows, skipped := physicalBypass(targets, routes, adapters, 7)
	if len(rows) != 1 || len(skipped) != 1 {
		t.Fatalf("physicalBypass = %d rows / %d skipped, want 1/1", len(rows), len(skipped))
	}
	if row := rows[0]; row.InterfaceLuid != 22 || row.DestinationPrefix.PrefixLength != 10 || row.Metric != bypassRouteMetric {
		t.Fatalf("bypass row = %+v, want luid 22 /10 metric %d", row, bypassRouteMetric)
	}
	if skipped[0].String() != "fd7a:115c:a1e0::/48" {
		t.Fatalf("skipped = %s, want the IPv6 target", skipped[0].String())
	}
}

func TestOwnedRoute(t *testing.T) {
	tunnelDefault := routeRow(7, net.IPv4zero, 0, net.IPv4zero, tunnelRouteMetric)
	foreignDefault := routeRow(22, net.IPv4zero, 0, net.IPv4(192, 168, 1, 1), tunnelRouteMetric)
	bypass := routeRow(22, net.IPv4(100, 64, 0, 0), 10, net.IPv4(192, 168, 1, 1), bypassRouteMetric)
	userRoute := routeRow(22, net.IPv4(100, 64, 0, 0), 10, net.IPv4(192, 168, 1, 1), 0)
	onLink := routeRow(22, net.IPv4(192, 168, 1, 0), 24, net.IPv4zero, 0)

	cases := []struct {
		name string
		row  windows.MibIpForwardRow2
		want bool
	}{
		{"tunnel default", tunnelDefault, true},
		{"physical default", foreignDefault, false},
		{"bypass metric", bypass, true},
		{"user route", userRoute, false},
		{"on-link", onLink, false},
	}
	for _, tc := range cases {
		if got := ownedRoute(&tc.row, 7); got != tc.want {
			t.Fatalf("%s: ownedRoute = %v, want %v", tc.name, got, tc.want)
		}
	}
	if ownedRoute(&tunnelDefault, 0) {
		t.Fatal("tunnel default claimed owned when the tunnel LUID is unknown")
	}
	if ownedRoute(&bypass, 0) != true {
		t.Fatal("bypass route not claimed owned when the tunnel adapter is gone")
	}
}

// --- orchestration ----------------------------------------------------------

func TestCleanupIdempotent(t *testing.T) {
	f := &fakeWinNet{
		luid: 7,
		routelist: []windows.MibIpForwardRow2{
			routeRow(7, net.IPv4zero, 0, net.IPv4zero, tunnelRouteMetric),
			defaultRouteRow(7, windows.AF_INET6, tunnelRouteMetric),
			routeRow(22, net.IPv4(100, 64, 0, 0), 10, net.IPv4(192, 168, 1, 1), bypassRouteMetric),
			routeRow(22, net.IPv4zero, 0, net.IPv4(192, 168, 1, 1), tunnelRouteMetric),
			routeRow(22, net.IPv4(100, 64, 0, 0), 10, net.IPv4(192, 168, 1, 1), 0),
			routeRow(22, net.IPv4(192, 168, 1, 0), 24, net.IPv4zero, 0),
		},
	}
	nc := &netConfig{wn: f}
	spec := platform.NetConfigSpec{TunName: "foxy0", TunAddr: "10.8.0.2"}

	nc.Cleanup(context.Background(), spec)
	if got := len(f.deletedRoutes); got != 3 {
		t.Fatalf("first Cleanup deleted %d routes, want 3 (2 defaults + 1 bypass)", got)
	}
	firstRun := len(f.deletedRoutes)

	nc.Cleanup(context.Background(), spec)
	if got := len(f.deletedRoutes) - firstRun; got != 3 {
		t.Fatalf("second Cleanup deleted %d routes, want 3 (idempotent)", got)
	}
	for _, r := range f.deletedRoutes {
		if r.Metric != tunnelRouteMetric && r.Metric != bypassRouteMetric {
			t.Fatalf("Cleanup deleted a foreign route: %v", r)
		}
	}
	if len(f.deletedAddrs) != 2 {
		t.Fatalf("address deletions = %d, want 2 (one per Cleanup)", len(f.deletedAddrs))
	}
	if len(f.dnsCalls) != 2 || f.dnsCalls[0].servers != nil {
		t.Fatalf("DNS clears = %+v, want two empty clears", f.dnsCalls)
	}
	if f.dnsCalls[0].guid != fixedAdapterGUID {
		t.Fatalf("DNS clear GUID = %s, want the fixed adapter GUID", f.dnsCalls[0].guid)
	}
}

func TestCleanupWithoutAdapterStillDeletesBypassResidue(t *testing.T) {
	f := &fakeWinNet{
		luidErr: windows.ERROR_NOT_FOUND,
		routelist: []windows.MibIpForwardRow2{
			routeRow(22, net.IPv4(100, 64, 0, 0), 10, net.IPv4(192, 168, 1, 1), bypassRouteMetric),
			routeRow(22, net.IPv4zero, 0, net.IPv4(192, 168, 1, 1), tunnelRouteMetric),
		},
	}
	nc := &netConfig{wn: f}
	nc.Cleanup(context.Background(), platform.NetConfigSpec{})
	if len(f.deletedRoutes) != 1 || f.deletedRoutes[0].DestinationPrefix.PrefixLength != 10 {
		t.Fatalf("deleted routes = %v, want just the bypass residue", f.deletedRoutes)
	}
	if len(f.deletedAddrs) != 0 {
		t.Fatalf("address deletions = %d, want 0 without an adapter", len(f.deletedAddrs))
	}
}

func TestApply(t *testing.T) {
	f := &fakeWinNet{
		luid: 7,
		adlist: []adapterInfo{{
			luid:    22,
			operUp:  true,
			metric4: 25,
			metric6: 5,
			addresses: []net.IPNet{
				mustCIDR(t, "192.168.1.0/24"),
			},
		}},
		routelist: []windows.MibIpForwardRow2{
			routeRow(22, net.IPv4zero, 0, net.IPv4(192, 168, 1, 1), 0),
			routeRow(22, net.IPv6zero, 0, net.ParseIP("2001:db8::1"), 0),
		},
	}
	nc := &netConfig{wn: f}
	var logs []string
	nc.SetLogf(func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) })

	spec := platform.NetConfigSpec{
		TunName:    "foxy0",
		TunAddr:    "10.8.0.2",
		TunMTU:     8500,
		AllowLAN:   true,
		KillSwitch: true,
	}
	if err := nc.Apply(context.Background(), spec); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(f.createdAddrs) != 1 {
		t.Fatalf("created addresses = %d, want 1", len(f.createdAddrs))
	}
	addr := f.createdAddrs[0]
	if addr.InterfaceLuid != 7 || addr.OnLinkPrefixLength != 32 || addr.Address.Family != windows.AF_INET {
		t.Fatalf("address row = %+v, want luid 7 /32 AF_INET", addr)
	}

	if len(f.mtuCalls) != 2 {
		t.Fatalf("MTU calls = %+v, want one per family", f.mtuCalls)
	}
	for _, c := range f.mtuCalls {
		if c.luid != 7 || c.mtu != 8500 || (c.family != windows.AF_INET && c.family != windows.AF_INET6) {
			t.Fatalf("MTU call = %+v, want luid 7 mtu 8500 for AF_INET/AF_INET6", c)
		}
	}

	if len(f.deletedRoutes) != 0 {
		t.Fatalf("pre-apply cleanup deleted routes: %v", f.deletedRoutes)
	}
	if len(f.createdRoutes) != 5 {
		t.Fatalf("created routes = %d, want 5 (3 bypass + 2 defaults): %v", len(f.createdRoutes), f.createdRoutes)
	}
	want := map[string]bool{
		"100.64.0.0/10":       false,
		"fd7a:115c:a1e0::/48": false,
		"192.168.1.0/24":      false,
		"0.0.0.0/0":           false,
		"::/0":                false,
	}
	for i := range f.createdRoutes {
		row := &f.createdRoutes[i]
		key := fmt.Sprintf("%s/%d", sockaddrIP(&row.DestinationPrefix.Prefix), row.DestinationPrefix.PrefixLength)
		if _, ok := want[key]; !ok {
			t.Fatalf("unexpected route %s", key)
		}
		want[key] = true
		if row.DestinationPrefix.PrefixLength == 0 {
			if row.InterfaceLuid != 7 || row.Metric != tunnelRouteMetric {
				t.Fatalf("tunnel default %s = luid %#x metric %d, want luid 7 metric %d", key, row.InterfaceLuid, row.Metric, tunnelRouteMetric)
			}
			continue
		}
		if row.InterfaceLuid != 22 || row.Metric != bypassRouteMetric {
			t.Fatalf("bypass %s = luid %#x metric %d, want luid 22 metric %d", key, row.InterfaceLuid, row.Metric, bypassRouteMetric)
		}
	}
	for key, seen := range want {
		if !seen {
			t.Fatalf("route %s was not created", key)
		}
	}

	if len(f.dnsCalls) != 2 {
		t.Fatalf("DNS calls = %d, want 2 (cleanup clear + apply set)", len(f.dnsCalls))
	}
	set := f.dnsCalls[len(f.dnsCalls)-1]
	if set.guid != fixedAdapterGUID || strings.Join(set.servers, ",") != "10.8.0.2" {
		t.Fatalf("DNS set = %+v, want fixed GUID + [10.8.0.2]", set)
	}

	foundKillSwitch := false
	for _, line := range logs {
		if line == "kill switch not yet implemented on windows" {
			foundKillSwitch = true
		}
	}
	if !foundKillSwitch {
		t.Fatalf("kill-switch log missing from %q", logs)
	}
}

// TestApplyLive exercises the real IP Helper path against a real Wintun
// adapter. It skips (printing why) on runners without the driver or without
// elevation; the hosted windows-latest job usually lacks both.
func TestApplyLive(t *testing.T) {
	const name = "kumiho-test-netcfg"
	d, err := openDevice(name, 8500)
	if err != nil {
		t.Skipf("wintun adapter unavailable (CreateAdapter failed): %v", err)
	}
	defer d.Close()

	const tunAddr = "10.8.0.3"
	nc := &netConfig{}
	spec := platform.NetConfigSpec{TunName: name, TunAddr: tunAddr, TunMTU: 8500}
	if err := nc.Apply(context.Background(), spec); err != nil {
		nc.Cleanup(context.Background(), spec)
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			t.Skipf("not elevated, skipping live Apply: %v", err)
		}
		t.Fatalf("Apply: %v", err)
	}
	nc.Cleanup(context.Background(), spec)
}
