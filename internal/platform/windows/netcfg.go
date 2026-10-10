//go:build windows

package windows

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/benyamin-git/kumiho/internal/netcfg"
	"github.com/benyamin-git/kumiho/internal/platform"
)

// tunnelRouteMetric is the route-offset of the /0 routes through the TUN. The
// tunnel interface metric is forced to zero as well, so the tunnel default has
// the lowest possible effective metric and wins over every physical default.
const tunnelRouteMetric = 0

// bypassRouteMetric is the route-offset of every bypass route. It is higher
// than the tunnel default's metric and deliberately distinctive: Cleanup
// identifies kumiho's bypass routes by this metric and the interface LUID
// alone, so stale routes are removed even after a config change or a crash.
const bypassRouteMetric = 9999

// fixedAdapterGUID is the adapter GUID requested when the Wintun adapter is
// created (device.go). A stable GUID keeps NLA generation deterministic across
// restarts and lets netcfg reach the adapter's DNS settings without walking
// the interface tables. Never change it after release.
var fixedAdapterGUID = windows.GUID{
	Data1: 0x3b511063,
	Data2: 0xbade,
	Data3: 0x458c,
	Data4: [8]byte{0x9d, 0xae, 0x5d, 0x21, 0xb4, 0xf0, 0xc0, 0x40},
}

// tailscaleRanges mirror PLAN.md §3.2 rule 4: Tailscale traffic must never
// enter the tunnel (SSH and peer access must survive). The range values are
// byte-identical to the Linux nft exclusions.
var tailscaleRanges = []string{"100.64.0.0/10", "fd7a:115c:a1e0::/48"}

// netConfig implements platform.NetConfig for Windows.
type netConfig struct {
	// logf receives informational logs; it may be nil.
	logf func(format string, args ...any)
	// wn is the win32 seam; nil means the real IP Helper implementation.
	wn winNet

	// wfpMu guards wfp and the whole open/apply/clear/close sequence: the
	// engine's settings path can re-apply while a connect or teardown runs.
	wfpMu sync.Mutex
	// wfp is the open dynamic kill-switch session, or nil when the kill
	// switch has never been engaged (or was torn down with the tunnel).
	wfp wfpBlockerAPI
	// openWFP opens the kill-switch blocker; nil means openWFPBlocker. Tests
	// inject a fake so Apply runs without a WFP engine.
	openWFP func() (wfpBlockerAPI, error)
}

// SetLogf routes netcfg's informational logs to logf. It is an additive
// capability: the engine opts in via an interface assertion, so the frozen
// platform.NetConfig method set stays untouched (engine.go netcfgLogfSetter).
func (n *netConfig) SetLogf(logf func(format string, args ...any)) {
	n.logf = logf
}

func (n *netConfig) log(format string, args ...any) {
	if n.logf != nil {
		n.logf(format, args...)
	}
}

func (n *netConfig) win() winNet {
	if n.wn == nil {
		return win32Net{}
	}
	return n.wn
}

// Apply installs the interface configuration for the full tunnel: address,
// MTU, bypass routes, tunnel defaults and interface DNS. It starts from a
// clean slate so crashes and reconnects are safe (PLAN.md §4.3).
func (n *netConfig) Apply(ctx context.Context, spec platform.NetConfigSpec) error {
	spec = normalizeSpec(spec)
	tunIP := net.ParseIP(spec.TunAddr)
	if tunIP == nil {
		return fmt.Errorf("windows netcfg: tun address %q is not an IP address", spec.TunAddr)
	}
	wn := n.win()
	luid, err := wn.aliasToLUID(spec.TunName)
	if err != nil {
		return fmt.Errorf("windows netcfg: adapter %q: %w", spec.TunName, err)
	}

	// Idempotent crash recovery: stale artifacts must never stack up.
	n.Cleanup(ctx, spec)

	addr := unicastRow(luid, tunIP, addressPrefixLength(tunIP))
	if err := wn.createAddress(&addr); err != nil && !errors.Is(err, windows.ERROR_OBJECT_ALREADY_EXISTS) {
		return fmt.Errorf("windows netcfg: add address %s: %w", spec.TunAddr, err)
	}
	for _, family := range []uint16{windows.AF_INET, windows.AF_INET6} {
		if err := wn.setMTU(luid, family, spec.TunMTU); err != nil {
			return fmt.Errorf("windows netcfg: set MTU %d (family %d): %w", spec.TunMTU, family, err)
		}
	}

	// Bypass routes are installed before the tunnel defaults so excluded
	// traffic never has a chance to match the /0 tunnel route: Tailscale
	// (rule 4), local LAN subnets (rule 5) and exclude_cidrs (rule 6).
	// WFP permits alone would not fix routing; these routes are the routing
	// half of the exclusion.
	adapters, err := wn.adapters()
	if err != nil {
		return fmt.Errorf("windows netcfg: enumerate adapters: %w", err)
	}
	routes, err := wn.routes()
	if err != nil {
		return fmt.Errorf("windows netcfg: enumerate routes: %w", err)
	}
	bypass, skipped := physicalBypass(staticTargets(spec), routes, adapters, luid)
	var lanPermitTargets []net.IPNet
	if spec.AllowLAN {
		lanRoutes := lanBypass(adapters, luid)
		bypass = append(bypass, lanRoutes...)
		lanPermitTargets = lanPermits(lanRoutes)
	}
	for i := range skipped {
		n.log("bypass route %s skipped: no physical default route for its family", skipped[i].String())
	}
	for i := range bypass {
		if err := wn.createRoute(&bypass[i]); err != nil && !errors.Is(err, windows.ERROR_OBJECT_ALREADY_EXISTS) {
			return fmt.Errorf("windows netcfg: add bypass route %s: %w", routeString(&bypass[i]), err)
		}
	}

	// IPv6 blackhole parity: ::/0 goes through the TUN exactly like 0.0.0.0/0,
	// so IPv6 cannot escape around the tunnel.
	for _, family := range []uint16{windows.AF_INET, windows.AF_INET6} {
		row := defaultRouteRow(luid, family, tunnelRouteMetric)
		if err := wn.createRoute(&row); err != nil && !errors.Is(err, windows.ERROR_OBJECT_ALREADY_EXISTS) {
			return fmt.Errorf("windows netcfg: add default route (family %d): %w", family, err)
		}
	}

	if err := wn.setDNS(fixedAdapterGUID, []string{spec.TunAddr}); err != nil {
		// The resolver may fall back to the physical adapter; the doctor
		// surface reports DNS health (Linux resolvectl parity).
		n.log("set adapter DNS failed (DNS may leak): %v", err)
	}

	// The kill switch is (re)programmed last, once every bypass destination is
	// known: turning it on clears any previous program and applies the new
	// one, turning it off only clears.
	if spec.KillSwitch {
		if err := n.applyKillSwitch(spec, luid, lanPermitTargets); err != nil {
			return fmt.Errorf("windows netcfg: %w", err)
		}
	} else {
		n.clearKillSwitch()
	}
	n.log("network configuration applied (%s, kill switch %v)", spec.TunName, spec.KillSwitch)
	return nil
}

// Cleanup removes exactly what Apply created — the tunnel default routes, the
// bypass routes, the TUN address and the adapter DNS — and never fails. The
// adapter device itself is deliberately not deleted here: Wintun removes
// create-created adapters when device.Close ends the session, and the fixed
// GUID lets a crashed run's adapter be reused instead of accumulating; this
// only clears the adapter's configuration residue.
func (n *netConfig) Cleanup(_ context.Context, spec platform.NetConfigSpec) {
	spec = normalizeSpec(spec)
	wn := n.win()
	tunnelLUID, err := wn.aliasToLUID(spec.TunName)
	if err != nil {
		// The adapter is gone, taking its routes and address with it; only
		// bypass residue on physical interfaces can remain.
		tunnelLUID = 0
	}

	if routes, err := wn.routes(); err != nil {
		n.log("cleanup: enumerate routes: %v", err)
	} else {
		for i := range routes {
			row := &routes[i]
			if !ownedRoute(row, tunnelLUID) {
				continue
			}
			if err := wn.deleteRoute(row); err != nil && !isNotFound(err) {
				n.log("cleanup: delete route %s: %v", routeString(row), err)
			}
		}
	}

	if tunnelLUID != 0 {
		if ip := net.ParseIP(spec.TunAddr); ip != nil {
			row := unicastRow(tunnelLUID, ip, addressPrefixLength(ip))
			if err := wn.deleteAddress(&row); err != nil && !isNotFound(err) {
				n.log("cleanup: delete address %s: %v", spec.TunAddr, err)
			}
		}
	}

	if err := wn.setDNS(fixedAdapterGUID, nil); err != nil && !isNotFound(err) {
		n.log("cleanup: clear adapter DNS: %v", err)
	}

	// The kill switch is dynamic WFP state: clear it (surfacing an engine
	// problem in the log) and close the session, which removes every object
	// even if the clear failed. Cleanup never fails.
	n.clearKillSwitch()
	n.closeKillSwitch()
}

// CleanupStale removes what a crashed run can leave behind beyond what
// Cleanup already clears with a live config: the default routes through the
// foxy0 adapter and the adapter device itself. `kumiho cleanup` calls it
// after the normal Cleanup path; like Cleanup it never fails and only logs.
func (n *netConfig) CleanupStale(_ context.Context) {
	spec := normalizeSpec(platform.NetConfigSpec{})
	wn := n.win()
	if luid, err := wn.aliasToLUID(spec.TunName); err == nil && luid != 0 {
		if routes, err := wn.routes(); err != nil {
			n.log("stale cleanup: enumerate routes: %v", err)
		} else {
			for i := range routes {
				row := &routes[i]
				if row.InterfaceLuid != luid || row.DestinationPrefix.PrefixLength != 0 {
					continue
				}
				if err := wn.deleteRoute(row); err != nil && !isNotFound(err) {
					n.log("stale cleanup: delete default route %s: %v", routeString(row), err)
				}
			}
		}
	}
	if err := wn.removeAdapter(spec.TunName); err != nil && !isNotFound(err) {
		n.log("stale cleanup: remove adapter %s: %v", spec.TunName, err)
	}
}

// guidDevClassNet is GUID_DEVCLASS_NET, the network adapter setup class.
var guidDevClassNet = windows.GUID{
	Data1: 0x4d36e972,
	Data2: 0xe325,
	Data3: 0x11ce,
	Data4: [8]byte{0xbf, 0xc1, 0x08, 0x00, 0x2b, 0xe1, 0x03, 0x18},
}

// removeAdapter deletes the named Wintun adapter device if it is present.
// Only a network adapter whose friendly name matches and whose driver service
// is wintun is removed, so a same-named non-Wintun device is never touched.
// A missing adapter is not an error.
func removeAdapter(alias string) error {
	set, err := windows.SetupDiGetClassDevsEx(&guidDevClassNet, "", 0, windows.DIGCF_PRESENT, 0, "")
	if err != nil {
		return fmt.Errorf("enumerate network adapters: %w", err)
	}
	defer set.Close()

	for i := 0; ; i++ {
		data, err := windows.SetupDiEnumDeviceInfo(set, i)
		if errors.Is(err, windows.ERROR_NO_MORE_ITEMS) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("enumerate network adapters: %w", err)
		}
		friendly, err := deviceProperty(set, data, windows.SPDRP_FRIENDLYNAME)
		if err != nil || !strings.EqualFold(friendly, alias) {
			continue
		}
		service, err := deviceProperty(set, data, windows.SPDRP_SERVICE)
		if err != nil || !strings.EqualFold(service, "wintun") {
			continue
		}
		if err := set.CallClassInstaller(windows.DIF_REMOVE, data); err != nil {
			return fmt.Errorf("remove adapter %s: %w", alias, err)
		}
		return nil
	}
}

// deviceProperty reads one string device registry property.
func deviceProperty(set windows.DevInfo, data *windows.DevInfoData, property windows.SPDRP) (string, error) {
	value, err := windows.SetupDiGetDeviceRegistryProperty(set, data, property)
	if err != nil {
		return "", err
	}
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("device property %d is not a string", property)
	}
	return s, nil
}

// lanPermits returns the destination prefixes of the LAN bypass routes: the
// WFP kill-switch permits and the routing table must agree on what bypasses
// the tunnel (spec.AllowLAN=false therefore also blocks LAN at the firewall).
func lanPermits(rows []windows.MibIpForwardRow2) []net.IPNet {
	out := make([]net.IPNet, 0, len(rows))
	for i := range rows {
		ip := sockaddrIP(&rows[i].DestinationPrefix.Prefix)
		if ip == nil || ip.To4() == nil {
			continue
		}
		out = append(out, net.IPNet{
			IP:   append(net.IP(nil), ip...),
			Mask: net.CIDRMask(int(rows[i].DestinationPrefix.PrefixLength), 32),
		})
	}
	return out
}

// applyKillSwitch opens the dynamic WFP session on first use and installs the
// permit filters plus the catch-all block. It is called on every Apply with
// KillSwitch true, so it atomically replaces the previous program (clear then
// apply): repeated applications are idempotent. A failure fails the connect —
// a requested kill switch that cannot be enforced must not pass traffic.
func (n *netConfig) applyKillSwitch(spec platform.NetConfigSpec, luid uint64, lanTargets []net.IPNet) error {
	appPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("kill switch: locate current executable: %w", err)
	}
	permits := buildPermitFilters(spec, luid, appPath, lanTargets)

	n.wfpMu.Lock()
	defer n.wfpMu.Unlock()
	if n.wfp == nil {
		blocker, err := n.openBlocker()
		if err != nil {
			return fmt.Errorf("kill switch: %w", err)
		}
		n.wfp = blocker
	}
	if err := n.wfp.clear(); err != nil {
		return fmt.Errorf("kill switch: clear: %w", err)
	}
	if err := n.wfp.apply(permits, true); err != nil {
		return fmt.Errorf("kill switch: apply: %w", err)
	}
	n.log("kill switch engaged (%d permit groups, block-all active)", len(permits))
	return nil
}

// openBlocker returns the injected blocker factory or the real WFP engine.
func (n *netConfig) openBlocker() (wfpBlockerAPI, error) {
	if n.openWFP != nil {
		return n.openWFP()
	}
	return openWFPBlocker()
}

// clearKillSwitch removes every kill-switch filter without tearing the session
// down. It never fails: callers only log.
func (n *netConfig) clearKillSwitch() {
	n.wfpMu.Lock()
	defer n.wfpMu.Unlock()
	if n.wfp == nil {
		return
	}
	if err := n.wfp.clear(); err != nil {
		n.log("kill switch: clear: %v", err)
	}
}

// closeKillSwitch ends the dynamic WFP session. Dynamic objects vanish with
// the session, so this is also the strongest possible clear.
func (n *netConfig) closeKillSwitch() {
	n.wfpMu.Lock()
	defer n.wfpMu.Unlock()
	if n.wfp == nil {
		return
	}
	n.wfp.close()
	n.wfp = nil
}

// ownedRoute reports whether a route table row was created by Apply: the
// tunnel's own /0 routes, or bypass routes (distinctive metric on a
// non-tunnel interface). On-link LAN routes, other VPNs' routes and user
// static routes don't match and are left untouched.
func ownedRoute(row *windows.MibIpForwardRow2, tunnelLUID uint64) bool {
	if row.DestinationPrefix.PrefixLength == 0 {
		return tunnelLUID != 0 && row.InterfaceLuid == tunnelLUID
	}
	return row.InterfaceLuid != 0 && row.InterfaceLuid != tunnelLUID && row.Metric == bypassRouteMetric
}

// normalizeSpec fills the tunnel defaults so a zero spec is usable.
func normalizeSpec(spec platform.NetConfigSpec) platform.NetConfigSpec {
	if spec.TunName == "" {
		spec.TunName = netcfg.DefaultTunName
	}
	if spec.TunAddr == "" {
		spec.TunAddr = netcfg.DefaultTunAddr
	}
	if spec.TunMTU <= 0 {
		spec.TunMTU = netcfg.DefaultTunMTU
	}
	return spec
}

func addressPrefixLength(ip net.IP) uint8 {
	if ip.To4() != nil {
		return 32
	}
	return 128
}

// unicastRow builds a MIB_UNICASTIPADDRESS_ROW for one address on one
// interface. DAD is set to preferred up front (Windows 10+ optimistic DAD),
// so the address is usable immediately instead of after the probe delay.
func unicastRow(luid uint64, ip net.IP, prefixLen uint8) windows.MibUnicastIpAddressRow {
	row := windows.MibUnicastIpAddressRow{
		InterfaceLuid:      luid,
		ValidLifetime:      0xffffffff,
		PreferredLifetime:  0xffffffff,
		OnLinkPrefixLength: prefixLen,
		DadState:           windows.IpDadStatePreferred,
	}
	// Address is a SOCKADDR_INET union. Its IPv4 arm puts sin_addr at byte
	// offset 4 while the IPv6 arm puts sin6_addr at offset 8, so an IPv4
	// address must be written through the RawSockaddrInet4 view — writing
	// row.Address.Addr[:4] would leave sin_addr at 0.0.0.0 and
	// CreateUnicastIpAddressEntry would fail.
	if v4 := ip.To4(); v4 != nil {
		sa := (*windows.RawSockaddrInet4)(unsafe.Pointer(&row.Address))
		sa.Family = windows.AF_INET
		copy(sa.Addr[:], v4)
	} else if v6 := ip.To16(); v6 != nil {
		row.Address.Family = windows.AF_INET6
		copy(row.Address.Addr[:], v6)
	}
	return row
}

// routeRow builds a MIB_IPFORWARD_ROW2 for one prefix through one interface.
func routeRow(luid uint64, dst net.IP, prefixLen int, nextHop net.IP, metric uint32) windows.MibIpForwardRow2 {
	row := windows.MibIpForwardRow2{
		InterfaceLuid:     luid,
		ValidLifetime:     0xffffffff,
		PreferredLifetime: 0xffffffff,
		Metric:            metric,
	}
	row.DestinationPrefix.PrefixLength = uint8(prefixLen)
	setSockaddr(&row.DestinationPrefix.Prefix, dst)
	setSockaddr(&row.NextHop, nextHop)
	return row
}

// defaultRouteRow builds the 0.0.0.0/0 or ::/0 route through the tunnel. The
// next hop is unspecified: the TUN endpoint is on-link.
func defaultRouteRow(luid uint64, family uint16, metric uint32) windows.MibIpForwardRow2 {
	if family == windows.AF_INET {
		return routeRow(luid, net.IPv4zero, 0, net.IPv4zero, metric)
	}
	return routeRow(luid, net.IPv6zero, 0, net.IPv6zero, metric)
}

// dnsRequest builds the VERSION1 SetInterfaceDnsSettings payload for one
// adapter DNS server list. An empty list clears the adapter's resolver
// configuration (WireGuard flush parity).
func dnsRequest(servers []string) (*dnsInterfaceSettings, error) {
	ptr, err := windows.UTF16PtrFromString(strings.Join(servers, ","))
	if err != nil {
		return nil, err
	}
	return &dnsInterfaceSettings{
		Version:    dnsSettingsVersion1,
		Flags:      dnsSettingNameServer,
		NameServer: ptr,
	}, nil
}

// staticTargets returns the CIDRs that bypass the tunnel regardless of LAN
// state: the hardcoded Tailscale ranges (PLAN.md §3.2 rule 4) plus the user's
// exclude_cidrs (rule 6).
func staticTargets(spec platform.NetConfigSpec) []net.IPNet {
	var out []net.IPNet
	for _, cidr := range append(append([]string(nil), tailscaleRanges...), spec.ExcludeCIDRs...) {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue // settings.Validate rejects these before they get here
		}
		out = append(out, *network)
	}
	return out
}

// lanBypass returns one explicit route per on-link IPv4 subnet of every up,
// non-loopback adapter except the tunnel (PLAN.md §3.2 rule 5, Windows
// edition). The stack's on-link routes usually already cover these, in which
// case CreateIpForwardEntry2 reports ERROR_OBJECT_ALREADY_EXISTS and the
// bypass guarantee already holds; /31 and /32 links are skipped because they
// carry no interesting neighbours (Linux DetectLANSubnets parity).
func lanBypass(adapters []adapterInfo, tunnelLUID uint64) []windows.MibIpForwardRow2 {
	seen := make(map[string]bool)
	var out []windows.MibIpForwardRow2
	for _, a := range adapters {
		if !a.operUp || a.ifType == windows.IF_TYPE_SOFTWARE_LOOPBACK || a.luid == tunnelLUID {
			continue
		}
		for _, n := range a.addresses {
			if n.IP.To4() == nil {
				continue
			}
			ones, bits := n.Mask.Size()
			if bits != 32 || ones > 30 {
				continue
			}
			network := n.IP.Mask(n.Mask)
			key := prefixKey(network, ones)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, routeRow(a.luid, network, ones, net.IPv4zero, bypassRouteMetric))
		}
	}
	return out
}

// physicalBypass turns static bypass targets into routes through the
// interface the traffic would have used without the tunnel: the default route
// with the lowest effective metric (route metric offset plus interface
// metric) that is not on the tunnel. Targets whose family has no physical
// default route are returned as skipped.
func physicalBypass(targets []net.IPNet, routes []windows.MibIpForwardRow2, adapters []adapterInfo, tunnelLUID uint64) (rows []windows.MibIpForwardRow2, skipped []net.IPNet) {
	type path struct {
		luid    uint64
		nextHop net.IP
		ok      bool
	}
	best := make(map[uint16]path)
	for _, t := range targets {
		family := familyOf(t.IP)
		p, done := best[family]
		if !done {
			luid, nextHop, ok := physicalDefault(routes, adapters, tunnelLUID, family)
			p = path{luid: luid, nextHop: nextHop, ok: ok}
			best[family] = p
		}
		if !p.ok {
			skipped = append(skipped, t)
			continue
		}
		ones, _ := t.Mask.Size()
		rows = append(rows, routeRow(p.luid, t.IP, ones, p.nextHop, bypassRouteMetric))
	}
	return rows, skipped
}

// physicalDefault picks the best default route for one family that is not on
// the tunnel interface.
func physicalDefault(routes []windows.MibIpForwardRow2, adapters []adapterInfo, tunnelLUID uint64, family uint16) (uint64, net.IP, bool) {
	interfaceMetric := func(luid uint64) uint32 {
		for _, a := range adapters {
			if a.luid != luid {
				continue
			}
			if family == windows.AF_INET {
				return a.metric4
			}
			return a.metric6
		}
		return 0
	}
	bestIdx := -1
	var bestMetric uint64
	for i := range routes {
		row := &routes[i]
		if row.DestinationPrefix.PrefixLength != 0 || row.DestinationPrefix.Prefix.Family != family {
			continue
		}
		if row.InterfaceLuid == 0 || row.InterfaceLuid == tunnelLUID {
			continue
		}
		if sockaddrIP(&row.NextHop) == nil {
			continue
		}
		effective := uint64(row.Metric) + uint64(interfaceMetric(row.InterfaceLuid))
		if bestIdx == -1 || effective < bestMetric ||
			(effective == bestMetric && row.InterfaceIndex < routes[bestIdx].InterfaceIndex) {
			bestIdx = i
			bestMetric = effective
		}
	}
	if bestIdx == -1 {
		return 0, nil, false
	}
	row := &routes[bestIdx]
	return row.InterfaceLuid, sockaddrIP(&row.NextHop), true
}

// adapterInfo is the slice of one adapter netcfg cares about.
type adapterInfo struct {
	luid      uint64
	ifType    uint32
	operUp    bool
	metric4   uint32
	metric6   uint32
	addresses []net.IPNet
}

const adapterAddressFlags = windows.GAA_FLAG_SKIP_ANYCAST | windows.GAA_FLAG_SKIP_MULTICAST |
	windows.GAA_FLAG_SKIP_DNS_SERVER | windows.GAA_FLAG_INCLUDE_PREFIX

// collectAdapters snapshots the adapter table (via GetAdaptersAddresses).
func collectAdapters() ([]adapterInfo, error) {
	var size uint32
	err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, adapterAddressFlags, 0, nil, &size)
	if err == nil {
		return nil, nil
	}
	if !errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) {
		return nil, err
	}
	buf := make([]byte, size)
	err = windows.GetAdaptersAddresses(windows.AF_UNSPEC, adapterAddressFlags, 0,
		(*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])), &size)
	if err != nil {
		return nil, err
	}
	var out []adapterInfo
	for aa := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])); aa != nil; aa = aa.Next {
		info := adapterInfo{
			luid:    aa.Luid,
			ifType:  aa.IfType,
			operUp:  aa.OperStatus == windows.IfOperStatusUp,
			metric4: aa.Ipv4Metric,
			metric6: aa.Ipv6Metric,
		}
		for ua := aa.FirstUnicastAddress; ua != nil; ua = ua.Next {
			ip := ua.Address.IP()
			if ip == nil {
				continue
			}
			bits := 128
			if v4 := ip.To4(); v4 != nil {
				ip, bits = v4, 32
			} else {
				ip = ip.To16()
			}
			info.addresses = append(info.addresses, net.IPNet{
				IP:   append(net.IP(nil), ip...),
				Mask: net.CIDRMask(int(ua.OnLinkPrefixLength), bits),
			})
		}
		out = append(out, info)
	}
	return out, nil
}

// familyOf returns the winsock family of an IP.
func familyOf(ip net.IP) uint16 {
	if ip.To4() != nil {
		return windows.AF_INET
	}
	return windows.AF_INET6
}

// setSockaddr fills a SOCKADDR_INET union with one address.
func setSockaddr(dst *windows.RawSockaddrInet, ip net.IP) {
	if v4 := ip.To4(); v4 != nil {
		sa := (*windows.RawSockaddrInet4)(unsafe.Pointer(dst))
		sa.Family = windows.AF_INET
		copy(sa.Addr[:], v4)
		return
	}
	if v6 := ip.To16(); v6 != nil {
		sa := (*windows.RawSockaddrInet6)(unsafe.Pointer(dst))
		sa.Family = windows.AF_INET6
		copy(sa.Addr[:], v6)
	}
}

// sockaddrIP extracts the address from a SOCKADDR_INET union, or nil.
func sockaddrIP(sa *windows.RawSockaddrInet) net.IP {
	switch sa.Family {
	case windows.AF_INET:
		v4 := (*windows.RawSockaddrInet4)(unsafe.Pointer(sa))
		return net.IP(v4.Addr[:])
	case windows.AF_INET6:
		v6 := (*windows.RawSockaddrInet6)(unsafe.Pointer(sa))
		return net.IP(v6.Addr[:])
	}
	return nil
}

func prefixKey(ip net.IP, ones int) string {
	bits := 128
	if v4 := ip.To4(); v4 != nil {
		ip, bits = v4, 32
	}
	return ip.Mask(net.CIDRMask(ones, bits)).String() + "/" + strconv.Itoa(ones)
}

func routeString(row *windows.MibIpForwardRow2) string {
	return fmt.Sprintf("%s/%d via %s (luid %#x, metric %d)",
		sockaddrIP(&row.DestinationPrefix.Prefix), row.DestinationPrefix.PrefixLength,
		sockaddrIP(&row.NextHop), row.InterfaceLuid, row.Metric)
}

// isNotFound reports harmless delete misses: cleanup treats them the same as
// a successful removal.
func isNotFound(err error) bool {
	return errors.Is(err, windows.ERROR_NOT_FOUND) || errors.Is(err, windows.ERROR_FILE_NOT_FOUND)
}

// winNet is the slice of the IP Helper surface netConfig drives. The real
// implementation is win32Net; tests inject fakes so Apply/Cleanup run on
// non-elevated runners.
type winNet interface {
	aliasToLUID(alias string) (uint64, error)
	adapters() ([]adapterInfo, error)
	routes() ([]windows.MibIpForwardRow2, error)
	createAddress(row *windows.MibUnicastIpAddressRow) error
	deleteAddress(row *windows.MibUnicastIpAddressRow) error
	setMTU(luid uint64, family uint16, mtu int) error
	createRoute(row *windows.MibIpForwardRow2) error
	deleteRoute(row *windows.MibIpForwardRow2) error
	setDNS(guid windows.GUID, servers []string) error
	removeAdapter(name string) error
}

// win32Net is the real IP Helper implementation of winNet.
type win32Net struct{}

func (win32Net) aliasToLUID(alias string) (uint64, error) {
	return convertInterfaceAliasToLUID(alias)
}

func (win32Net) adapters() ([]adapterInfo, error) { return collectAdapters() }

func (win32Net) routes() ([]windows.MibIpForwardRow2, error) {
	return getIPForwardTable2(windows.AF_UNSPEC)
}

func (win32Net) createAddress(row *windows.MibUnicastIpAddressRow) error {
	return createUnicastIPAddressEntry(row)
}

func (win32Net) deleteAddress(row *windows.MibUnicastIpAddressRow) error {
	return deleteUnicastIPAddressEntry(row)
}

// setMTU writes the interface row for one family: MTU plus the forced zero
// metric that makes the tunnel's /0 route win over physical defaults
// (WireGuard's addressconfig.go does the same).
func (win32Net) setMTU(luid uint64, family uint16, mtu int) error {
	row := windows.MibIpInterfaceRow{}
	initializeIPInterfaceEntry(&row)
	row.Family = family
	row.InterfaceLuid = luid
	row.NlMtu = uint32(mtu)
	row.UseAutomaticMetric = 0
	row.Metric = 0
	return setIPInterfaceEntry(&row)
}

func (win32Net) createRoute(row *windows.MibIpForwardRow2) error {
	return createIPForwardEntry2(row)
}

func (win32Net) deleteRoute(row *windows.MibIpForwardRow2) error {
	return deleteIPForwardEntry2(row)
}

func (win32Net) setDNS(guid windows.GUID, servers []string) error {
	req, err := dnsRequest(servers)
	if err != nil {
		return err
	}
	return setInterfaceDNSSettings(guid, req)
}

func (win32Net) removeAdapter(name string) error { return removeAdapter(name) }

var _ platform.NetConfig = (*netConfig)(nil)
