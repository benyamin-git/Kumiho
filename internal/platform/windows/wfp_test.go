//go:build windows

package windows

import (
	"encoding/binary"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/benyamin-git/kumiho/internal/platform"
)

// --- layout assertions -------------------------------------------------------

// TestWFPStructLayout pins the WFP wire layouts: Windows reads these structs
// directly, so a Go field-order or padding drift is a silent filter-engine
// failure. The 64-bit sizes/offsets come from the Windows SDK headers
// (fwpmtypes.h / fwptypes.h).
func TestWFPStructLayout(t *testing.T) {
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"sizeof(fwpmDisplayData0)", unsafe.Sizeof(fwpmDisplayData0{}), 16},
		{"offsetof(fwpmDisplayData0.description)", unsafe.Offsetof(fwpmDisplayData0{}.description), 8},
		{"sizeof(fwpmSession0)", unsafe.Sizeof(fwpmSession0{}), 72},
		{"offsetof(fwpmSession0.flags)", unsafe.Offsetof(fwpmSession0{}.flags), 32},
		{"offsetof(fwpmSession0.txnWaitTimeoutInMSec)", unsafe.Offsetof(fwpmSession0{}.txnWaitTimeoutInMSec), 36},
		{"offsetof(fwpmSession0.kernelMode)", unsafe.Offsetof(fwpmSession0{}.kernelMode), 64},
		{"sizeof(fwpmProvider0)", unsafe.Sizeof(fwpmProvider0{}), 64},
		{"offsetof(fwpmProvider0.serviceName)", unsafe.Offsetof(fwpmProvider0{}.serviceName), 56},
		{"sizeof(fwpmSublayer0)", unsafe.Sizeof(fwpmSublayer0{}), 72},
		{"offsetof(fwpmSublayer0.weight)", unsafe.Offsetof(fwpmSublayer0{}.weight), 64},
		{"sizeof(fwpmFilter0)", unsafe.Sizeof(fwpmFilter0{}), 200},
		{"offsetof(fwpmFilter0.weight)", unsafe.Offsetof(fwpmFilter0{}.weight), 96},
		{"offsetof(fwpmFilter0.numFilterConditions)", unsafe.Offsetof(fwpmFilter0{}.numFilterConditions), 112},
		{"offsetof(fwpmFilter0.filterCondition)", unsafe.Offsetof(fwpmFilter0{}.filterCondition), 120},
		{"offsetof(fwpmFilter0.action)", unsafe.Offsetof(fwpmFilter0{}.action), 128},
		{"offsetof(fwpmFilter0.providerContextKey)", unsafe.Offsetof(fwpmFilter0{}.providerContextKey), 152},
		{"offsetof(fwpmFilter0.reserved)", unsafe.Offsetof(fwpmFilter0{}.reserved), 168},
		{"offsetof(fwpmFilter0.filterID)", unsafe.Offsetof(fwpmFilter0{}.filterID), 176},
		{"offsetof(fwpmFilter0.effectiveWeight)", unsafe.Offsetof(fwpmFilter0{}.effectiveWeight), 184},
		{"sizeof(fwpmFilterCondition0)", unsafe.Sizeof(fwpmFilterCondition0{}), 40},
		{"offsetof(fwpmFilterCondition0.matchType)", unsafe.Offsetof(fwpmFilterCondition0{}.matchType), 16},
		{"offsetof(fwpmFilterCondition0.conditionValue)", unsafe.Offsetof(fwpmFilterCondition0{}.conditionValue), 24},
		{"sizeof(fwpmAction0)", unsafe.Sizeof(fwpmAction0{}), 20},
		{"offsetof(fwpmAction0.filterType)", unsafe.Offsetof(fwpmAction0{}.filterType), 4},
		{"sizeof(fwpValue0)", unsafe.Sizeof(fwpValue0{}), 16},
		{"offsetof(fwpValue0.value)", unsafe.Offsetof(fwpValue0{}.value), 8},
		{"sizeof(fwpByteBlob)", unsafe.Sizeof(fwpByteBlob{}), 16},
		{"offsetof(fwpByteBlob.data)", unsafe.Offsetof(fwpByteBlob{}.data), 8},
		{"sizeof(fwpV4AddrAndMask)", unsafe.Sizeof(fwpV4AddrAndMask{}), 8},
		{"sizeof(fwpV6AddrAndMask)", unsafe.Sizeof(fwpV6AddrAndMask{}), 17},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d (WFP rejects mismatched layouts or reads garbage)", tc.name, tc.got, tc.want)
		}
	}
}

// --- pure builders -----------------------------------------------------------

func TestBuildPermitFilters(t *testing.T) {
	spec := platform.NetConfigSpec{
		TunAddr:      "10.8.0.2",
		AllowLAN:     true,
		ExcludeCIDRs: []string{"203.0.113.0/24", "fd00::/8"},
	}
	lan := []net.IPNet{mustCIDR(t, "192.168.1.0/24")}
	permits := buildPermitFilters(spec, 0x21, `C:\Program Files\Kumiho\kumiho.exe`, lan)
	wantKinds := []wfpPermitKind{permitLoopback, permitApp, permitInterface, permitDHCP, permitDNS, permitDestinations}
	if len(permits) != len(wantKinds) {
		t.Fatalf("permits = %d classes, want %d: %+v", len(permits), len(wantKinds), permits)
	}
	for i, kind := range wantKinds {
		if permits[i].kind != kind {
			t.Fatalf("permit[%d] = %v, want %v (filter order is load-bearing)", i, permits[i].kind, kind)
		}
	}
	if permits[1].appPath != `C:\Program Files\Kumiho\kumiho.exe` {
		t.Fatalf("app path = %q", permits[1].appPath)
	}
	if permits[2].luid != 0x21 {
		t.Fatalf("interface luid = %#x, want 0x21", permits[2].luid)
	}
	if got := fmtNetworks(permits[4].addresses); got != "10.8.0.2/32" {
		t.Fatalf("DNS permits = %s, want 10.8.0.2/32", got)
	}
	targets := fmtNetworks(permits[5].addresses)
	for _, want := range []string{
		"100.64.0.0/10",       // Tailscale CGNAT (PLAN §3.2 rule 4 parity)
		"fd7a:115c:a1e0::/48", // Tailscale ULA
		"203.0.113.0/24",      // exclude_cidrs
		"fd00::/8",            // exclude_cidrs
		"192.168.1.0/24",      // detected LAN, only with AllowLAN
	} {
		if !strings.Contains(targets, want) {
			t.Fatalf("destination permits %q missing %s", targets, want)
		}
	}

	// AllowLAN=false drops the LAN targets but keeps Tailscale and excludes.
	spec.AllowLAN = false
	permits = buildPermitFilters(spec, 0x21, "kumiho.exe", lan)
	if got := fmtNetworks(permits[len(permits)-1].addresses); strings.Contains(got, "192.168.1.0/24") {
		t.Fatalf("AllowLAN=false still permits the LAN: %s", got)
	} else if !strings.Contains(got, "100.64.0.0/10") || !strings.Contains(got, "203.0.113.0/24") {
		t.Fatalf("AllowLAN=false dropped the static targets: %s", got)
	}

	// An unparsable tunnel address has no DNS permit; IPv6 is /128.
	permits = buildPermitFilters(platform.NetConfigSpec{TunAddr: "not-an-ip"}, 0, "kumiho.exe", nil)
	for _, p := range permits {
		if p.kind == permitDNS {
			t.Fatalf("DNS permit built for an invalid tunnel address: %+v", p)
		}
	}
	v6 := buildPermitFilters(platform.NetConfigSpec{TunAddr: "fd7a:115c:a1e0::2"}, 0, "kumiho.exe", nil)
	if got := fmtNetworks(v6[4].addresses); got != "fd7a:115c:a1e0::2/128" {
		t.Fatalf("v6 DNS permit = %s, want /128 host", got)
	}
}

func TestBuildFilterSpecs(t *testing.T) {
	spec := platform.NetConfigSpec{
		TunAddr:      "10.8.0.2",
		AllowLAN:     true,
		ExcludeCIDRs: []string{"203.0.113.0/24", "fd00::/8"},
	}
	permits := buildPermitFilters(spec, 0x21, `C:\kumiho.exe`, []net.IPNet{mustCIDR(t, "192.168.1.0/24")})
	specs := buildFilterSpecs(permits, true)

	// 6 permits: 4 layers each for loopback/app/interface, 11 for DHCP+NDP,
	// 2 for the v4-only DNS address, 4 destination filters (3 v4 + 2 v6
	// prefixes on both directions); plus the 4 catch-all block layers.
	if len(specs) != 33 {
		t.Fatalf("filters = %d, want 33", len(specs))
	}
	var weights []uint8
	for _, s := range specs {
		if len(weights) == 0 || s.weight != weights[len(weights)-1] {
			weights = append(weights, s.weight)
		}
	}
	wantWeights := []uint8{15, 14, 13, 12, 11, 10, 0}
	if len(weights) != len(wantWeights) {
		t.Fatalf("weights = %v, want %v (permits above block)", weights, wantWeights)
	}
	for i := range wantWeights {
		if weights[i] != wantWeights[i] {
			t.Fatalf("weights = %v, want %v", weights, wantWeights)
		}
	}

	// Loopback first, on the four ALE layers in order.
	for i, layer := range []windows.GUID{layerALEAuthConnectV4, layerALEAuthRecvAcceptV4, layerALEAuthConnectV6, layerALEAuthRecvAcceptV6} {
		s := specs[i]
		if s.action != fwpActionPermit || s.weight != 15 || s.layer != layer {
			t.Fatalf("loopback filter %d = %+v, want permit weight 15 on its layer", i, s)
		}
		if len(s.conditions) != 1 || s.conditions[0].field != condFlags ||
			s.conditions[0].matchType != fwpMatchFlagsAllSet || s.conditions[0].number != fwpConditionFlagIsLoopback {
			t.Fatalf("loopback condition = %+v", s.conditions)
		}
	}
	if c := specs[4].conditions[0]; c.valueType != fwpByteBlobType || c.appPath != `C:\kumiho.exe` {
		t.Fatalf("app condition = %+v, want a byte-blob app id", c)
	}
	if c := specs[8].conditions[0]; c.field != condIPLocalInterface || c.valueType != fwpUint64 || c.number != 0x21 {
		t.Fatalf("interface condition = %+v, want the LUID as FWP_UINT64", c)
	}

	dhcpv4 := specs[12]
	if dhcpv4.layer != layerALEAuthConnectV4 || len(dhcpv4.conditions) != 4 {
		t.Fatalf("DHCPv4 outbound = %+v", dhcpv4)
	}
	wantDHCP := []wfpCondition{
		{field: condIPProtocol, valueType: fwpUint8, number: ipprotoUDP},
		{field: condIPLocalPort, valueType: fwpUint16, number: 68},
		{field: condIPRemotePort, valueType: fwpUint16, number: 67},
		{field: condIPRemoteAddress, valueType: fwpUint32, number: 0xffffffff},
	}
	for i, want := range wantDHCP {
		got := dhcpv4.conditions[i]
		if got.field != want.field || got.valueType != want.valueType || got.number != want.number {
			t.Fatalf("DHCPv4 condition[%d] = %+v, want %+v", i, got, want)
		}
	}
	ndp133 := specs[16]
	if ndp133.name != "Permit NDP router solicitation (133)" || len(ndp133.conditions) != 4 {
		t.Fatalf("NDP 133 = %+v", ndp133)
	}
	if last := ndp133.conditions[3]; last.valueType != fwpByteArray16Type || string(last.data) != string(ndpAllRouters[:]) {
		t.Fatalf("NDP 133 destination = %+v, want ff02::2", last)
	}

	dnsOut := specs[23]
	if dnsOut.layer != layerALEAuthConnectV4 || len(dnsOut.conditions) != 4 {
		t.Fatalf("DNS outbound = %+v", dnsOut)
	}
	if c := dnsOut.conditions[0]; c.field != condIPRemotePort || c.number != 53 {
		t.Fatalf("DNS port condition = %+v", c)
	}
	if p := dnsOut.conditions[1]; p.number != ipprotoUDP {
		t.Fatalf("DNS protocol[0] = %+v, want UDP", p)
	}
	if p := dnsOut.conditions[2]; p.number != ipprotoTCP {
		t.Fatalf("DNS protocol[1] = %+v, want TCP", p)
	}
	if c := dnsOut.conditions[3]; c.field != condIPRemoteAddress || c.valueType != fwpV4AddrMask || len(c.data) != 8 {
		// The exact byte assertion lives in TestV4MaskData.
		t.Fatalf("DNS address condition = %+v", c)
	}

	destV4 := specs[25]
	if destV4.layer != layerALEAuthConnectV4 || len(destV4.conditions) != 3 {
		t.Fatalf("destination v4 = %+v", destV4)
	}
	destV6 := specs[27]
	if destV6.layer != layerALEAuthConnectV6 || len(destV6.conditions) != 2 {
		t.Fatalf("destination v6 = %+v", destV6)
	}
	for _, c := range destV6.conditions {
		if c.field != condIPRemoteAddress || c.valueType != fwpV6AddrMask || len(c.data) != 17 {
			t.Fatalf("destination v6 condition = %+v", c)
		}
	}

	for i := 29; i < 33; i++ {
		s := specs[i]
		if s.action != fwpActionBlock || s.weight != wfpBlockWeight || len(s.conditions) != 0 {
			t.Fatalf("block filter %d = %+v, want catch-all block at weight 0", i, s)
		}
	}

	noBlock := buildFilterSpecs(permits, false)
	if len(noBlock) != 29 {
		t.Fatalf("filters without block = %d, want 29", len(noBlock))
	}
	for _, s := range noBlock {
		if s.action == fwpActionBlock {
			t.Fatalf("block filter present with blockAll=false: %+v", s)
		}
	}
}

// TestBuildFilterSpecsMinimal covers a proxy-less spec with no LAN and no
// excludes and a v6 resolver: DNS/family splitting must stay correct and the
// destination permits must still carry the always-present Tailscale ranges.
func TestBuildFilterSpecsMinimal(t *testing.T) {
	permits := buildPermitFilters(platform.NetConfigSpec{TunAddr: "fd00::2"}, 1, "kumiho.exe", nil)
	specs := buildFilterSpecs(permits, false)
	// Loopback + app + interface = 12, DHCP/NDP = 11, DNS v6 = 2, Tailscale
	// destinations = 4.
	dst := 0
	for _, s := range specs {
		if strings.Contains(s.name, "excluded destinations") {
			dst++
		}
	}
	if dst != 4 {
		t.Fatalf("Tailscale destination filters = %d, want 4 (staticTargets is never empty)", dst)
	}
	if got := len(specs); got != 12+11+2+4 {
		t.Fatalf("filters = %d, want %d", got, 12+11+2+4)
	}
}

func TestV4MaskData(t *testing.T) {
	data := v4MaskData(mustCIDR(t, "192.168.1.0/24"))
	if len(data) != 8 {
		t.Fatalf("v4 mask bytes = %d, want 8", len(data))
	}
	// FWP_V4_ADDR_AND_MASK is host order: the native uint32 equals the
	// big-endian (human) value of the address bytes.
	if got, want := binary.NativeEndian.Uint32(data[0:4]), binary.BigEndian.Uint32(net.IPv4(192, 168, 1, 0).To4()); got != want {
		t.Fatalf("addr = %#x, want %#x", got, want)
	}
	if got := binary.NativeEndian.Uint32(data[4:8]); got != 0xffffff00 {
		t.Fatalf("mask = %#x, want 0xffffff00", got)
	}
	host := v4MaskData(net.IPNet{IP: net.IPv4(10, 8, 0, 2).To4(), Mask: net.CIDRMask(32, 32)})
	if got := binary.NativeEndian.Uint32(host[0:4]); got != 0x0a080002 {
		t.Fatalf("host addr = %#x, want 0x0a080002", got)
	}
	if data := v4MaskData(mustCIDR(t, "fd00::/8")); data != nil {
		t.Fatalf("v4MaskData(v6) = %v, want nil", data)
	}
}

func TestV6MaskData(t *testing.T) {
	data := v6MaskData(mustCIDR(t, "fe80::/10"))
	if len(data) != 17 {
		t.Fatalf("v6 mask bytes = %d, want 17", len(data))
	}
	want := net.ParseIP("fe80::").To16()
	if string(data[:16]) != string(want) {
		t.Fatalf("v6 mask addr = %v, want %v", data[:16], want)
	}
	if data[16] != 10 {
		t.Fatalf("v6 prefix = %d, want 10", data[16])
	}
	if data := v6MaskData(mustCIDR(t, "192.168.1.0/24")); data != nil {
		t.Fatalf("v6MaskData(v4) = %v, want nil", data)
	}
}

// --- blocker behavior without an engine --------------------------------------

func TestWFPClosedBlocker(t *testing.T) {
	b := &wfpBlocker{}
	if err := b.apply(nil, true); !errors.Is(err, errWFPClosed) {
		t.Fatalf("apply on a closed blocker = %v, want errWFPClosed", err)
	}
	if err := b.clear(); err != nil {
		t.Fatalf("clear on a closed blocker = %v, want nil (nothing to clear)", err)
	}
	b.close()
	b.close()
}

// --- live engine (skips on runners that cannot open WFP) ---------------------

// TestWFPApplyLive exercises the real dynamic session: clear/apply/clear are
// idempotent and a non-elevated context surfaces a clear, wrapped Windows
// error instead of a panic. The catch-all block is deliberately never
// installed here — a unit test must not cut the runner's network; the block
// filters are asserted by TestBuildFilterSpecs.
func TestWFPApplyLive(t *testing.T) {
	b, err := openWFPBlocker()
	if err != nil {
		t.Skipf("cannot open the WFP engine on this runner (CI windows-latest may deny it): %v", err)
	}
	defer b.close()

	if err := b.clear(); err != nil {
		t.Fatalf("clear on a fresh blocker: %v", err)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	permits := buildPermitFilters(platform.NetConfigSpec{TunAddr: "10.8.0.2"}, 0x21, exe, nil)
	if err := b.apply(permits, false); err != nil {
		var errno windows.Errno
		if !errors.As(err, &errno) {
			t.Fatalf("apply error %v is not a wrapped Windows error", err)
		}
		t.Skipf("not permitted to install WFP filters on this runner: %v", err)
	}
	if err := b.apply(permits, false); err != nil {
		t.Fatalf("re-apply (idempotency): %v", err)
	}
	if err := b.clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if err := b.clear(); err != nil {
		t.Fatalf("second clear (idempotency): %v", err)
	}
}

// TestWFPOpenDeniedIsClear is documentation-by-test: on an unelevated context
// the engine open fails with a wrapped Errno rather than a panic. It skips
// when the engine opens (elevated runner).
func TestWFPOpenDeniedIsClear(t *testing.T) {
	b, err := openWFPBlocker()
	if err == nil {
		b.close()
		t.Skip("WFP engine opened; this runner is elevated")
	}
	var errno windows.Errno
	if !errors.As(err, &errno) {
		t.Fatalf("open error %v is not a wrapped Windows error", err)
	}
	t.Skipf("WFP engine open correctly denied: %v", err)
}

// --- helpers -----------------------------------------------------------------

func fmtNetworks(addrs []net.IPNet) string {
	parts := make([]string, 0, len(addrs))
	for i := range addrs {
		parts = append(parts, addrs[i].String())
	}
	return strings.Join(parts, ",")
}
