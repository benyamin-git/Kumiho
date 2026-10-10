//go:build windows

package windows

// This file implements the Windows kill switch as a dynamic Windows Filtering
// Platform (WFP) session on fwpuclnt.dll, WireGuard-style: permit filters for
// loopback, the service process, the tunnel interface, DHCP/NDP, the tunnel
// DNS address and the routing bypass targets, followed by a catch-all block.
//
// Crash caveat: FWPM_SESSION_FLAG_DYNAMIC ties the provider, sublayer and every
// filter to this process. If the daemon crashes, Windows removes them all with
// the session, so protection ends at the crash (nothing persists to clean up).
// Service recovery restarts the daemon, and the start-time stale-netcfg
// cleanup plus the next connect re-apply the filters (D9 trade-off).

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/benyamin-git/kumiho/internal/platform"
)

// errWFPClosed is returned by apply after the blocker's dynamic session has
// been closed: every WFP object it owned is gone.
var errWFPClosed = errors.New("wfp: blocker is closed")

// FWP_ACTION_TYPE / FWP_MATCH_TYPE / FWP_DATA_TYPE values, all defined in
// fwptypes.h (MSDN: fwptypes.h enums FWP_ACTION_TYPE, FWP_MATCH_TYPE,
// FWP_DATA_TYPE). Block and permit are terminating actions: a matching filter
// ends arbitration with its action.
const (
	fwpActionBlock  = 0x00000001 | 0x00001000 // FWP_ACTION_BLOCK
	fwpActionPermit = 0x00000002 | 0x00001000 // FWP_ACTION_PERMIT

	fwpMatchEqual       = 0 // FWP_MATCH_EQUAL
	fwpMatchFlagsAllSet = 6 // FWP_MATCH_FLAGS_ALL_SET

	fwpUint8           = 1   // FWP_UINT8
	fwpUint16          = 2   // FWP_UINT16
	fwpUint32          = 3   // FWP_UINT32
	fwpUint64          = 4   // FWP_UINT64
	fwpByteArray16Type = 11  // FWP_BYTE_ARRAY16_TYPE
	fwpByteBlobType    = 12  // FWP_BYTE_BLOB_TYPE
	fwpV4AddrMask      = 256 // FWP_V4_ADDR_MASK
	fwpV6AddrMask      = 257 // FWP_V6_ADDR_MASK
)

// FWPM_SESSION_FLAG_DYNAMIC (fwpmtypes.h) makes the engine delete the session's
// provider, sublayer and filters when the session (this process) ends.
const fwpmSessionFlagDynamic = 0x00000001

// FWP_CONDITION_FLAG_IS_LOOPBACK (fwptypes.h), matched with
// FWP_MATCH_FLAGS_ALL_SET against FWPM_CONDITION_FLAGS.
const fwpConditionFlagIsLoopback = 0x00000001

// IP protocol numbers used by the permit conditions.
const (
	ipprotoTCP    = 6
	ipprotoUDP    = 17
	ipprotoICMPv6 = 58
)

// RPC_C_AUTHN_WINNT (rpcdce.h): authenticate the engine session with the
// current process token, which is what a LocalSystem service wants.
const rpcCAuthNWinNT = 10

// Filter weights inside kumiho's private sublayer. WFP arbitrates by
// decreasing weight and a terminating action wins, so every permit (>= 1)
// overtakes the catch-all block (0). The brief's permit order is encoded as
// descending weights so the evaluation order matches the one listed there.
const (
	wfpPermitWeightBase = 15
	wfpBlockWeight      = 0
)

// WFP object and condition identifiers from fwpmu.h. They are documented ABI
// constants per symbol below and are vars because windows.GUID is a struct.
var (
	// FWPM_CONDITION_IP_LOCAL_INTERFACE: the LUID of the local interface.
	condIPLocalInterface = windows.GUID{Data1: 0x4cd62a49, Data2: 0x59c3, Data3: 0x4969, Data4: [8]byte{0xb7, 0xf3, 0xbd, 0xa5, 0xd3, 0x28, 0x90, 0xa4}}
	// FWPM_CONDITION_IP_REMOTE_ADDRESS: the remote address (v4/v6).
	condIPRemoteAddress = windows.GUID{Data1: 0xb235ae9a, Data2: 0x1d64, Data3: 0x49b8, Data4: [8]byte{0xa4, 0x4c, 0x5f, 0xf3, 0xd9, 0x09, 0x50, 0x45}}
	// FWPM_CONDITION_IP_PROTOCOL: the IP protocol number.
	condIPProtocol = windows.GUID{Data1: 0x3971ef2b, Data2: 0x623e, Data3: 0x4f9a, Data4: [8]byte{0x8c, 0xb1, 0x6e, 0x79, 0xb8, 0x06, 0xb9, 0xa7}}
	// FWPM_CONDITION_IP_LOCAL_PORT: the local transport port; also
	// FWPM_CONDITION_ICMP_TYPE (WireGuard's fwpmu.h aliasing).
	condIPLocalPort = windows.GUID{Data1: 0x0c1ba1af, Data2: 0x5765, Data3: 0x453f, Data4: [8]byte{0xaf, 0x22, 0xa8, 0xf7, 0x91, 0xac, 0x77, 0x5b}}
	// FWPM_CONDITION_IP_REMOTE_PORT: the remote transport port; also
	// FWPM_CONDITION_ICMP_CODE.
	condIPRemotePort = windows.GUID{Data1: 0xc35a604d, Data2: 0xd22b, Data3: 0x4e1a, Data4: [8]byte{0x91, 0xb4, 0x68, 0xf6, 0x74, 0xee, 0x67, 0x4b}}
	// FWPM_CONDITION_ALE_APP_ID: the application identity (blob) of the flow.
	condALEAppID = windows.GUID{Data1: 0xd78e1e87, Data2: 0x8644, Data3: 0x4ea5, Data4: [8]byte{0x94, 0x37, 0xd8, 0x09, 0xec, 0xef, 0xc9, 0x71}}
	// FWPM_CONDITION_IP_LOCAL_ADDRESS: the local address (v4/v6).
	condIPLocalAddress = windows.GUID{Data1: 0xd9ee00de, Data2: 0xc1ef, Data3: 0x4617, Data4: [8]byte{0xbf, 0xe3, 0xff, 0xd8, 0xf5, 0xa0, 0x89, 0x57}}
	// FWPM_CONDITION_FLAGS: flow flags, e.g. loopback.
	condFlags = windows.GUID{Data1: 0x632ce23b, Data2: 0x5167, Data3: 0x435c, Data4: [8]byte{0x86, 0xd7, 0xe9, 0x03, 0x68, 0x4a, 0xa8, 0x0c}}

	// ICMP type and code reuse the port condition keys (WireGuard's fwpmu.h
	// aliasing; the layer interprets them as ICMP fields).
	condICMPType = condIPLocalPort
	condICMPCode = condIPRemotePort

	// FWPM_LAYER_ALE_AUTH_CONNECT_V4: outbound connection authorization.
	layerALEAuthConnectV4 = windows.GUID{Data1: 0xc38d57d1, Data2: 0x05a7, Data3: 0x4c33, Data4: [8]byte{0x90, 0x4f, 0x7f, 0xbc, 0xee, 0xe6, 0x0e, 0x82}}
	// FWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4: inbound connection acceptance.
	layerALEAuthRecvAcceptV4 = windows.GUID{Data1: 0xe1cd9fe7, Data2: 0xf4b5, Data3: 0x4273, Data4: [8]byte{0x96, 0xc0, 0x59, 0x2e, 0x48, 0x7b, 0x86, 0x50}}
	// FWPM_LAYER_ALE_AUTH_CONNECT_V6: outbound connection authorization.
	layerALEAuthConnectV6 = windows.GUID{Data1: 0x4a72393b, Data2: 0x319f, Data3: 0x44bc, Data4: [8]byte{0x84, 0xc3, 0xba, 0x54, 0xdc, 0xb3, 0xb6, 0xb4}}
	// FWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V6: inbound connection acceptance.
	layerALEAuthRecvAcceptV6 = windows.GUID{Data1: 0xa3b42c97, Data2: 0x9f04, Data3: 0x4672, Data4: [8]byte{0xb8, 0x7e, 0xce, 0xe9, 0xc4, 0x83, 0x25, 0x7f}}
)

// DHCPv6 and NDP well-known multicast addresses:
// ff02::1:2 (All_DHCP_Relay_Agents_and_Servers), ff05::1:3 (All_DHCP_Servers)
// and ff02::2 (All_Routers).
var (
	dhcpv6AllRelayAgentsAndServers = [16]byte{0xff, 0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 2}
	dhcpv6AllDHCPServers           = [16]byte{0xff, 0x05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 3}
	ndpAllRouters                  = [16]byte{0xff, 0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2}
)

// ipv6LinkLocal is fe80::/10, the source of router advertisements, redirects
// and the local end of DHCPv6.
var ipv6LinkLocal = net.IPNet{
	IP:   net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	Mask: net.CIDRMask(10, 128),
}

// wfpALELayers are the four ALE authorization layers every filter class is
// installed on: outbound connect and inbound accept for both address families.
var wfpALELayers = []struct {
	name  string
	layer windows.GUID
}{
	{"IPv4 out", layerALEAuthConnectV4},
	{"IPv4 in", layerALEAuthRecvAcceptV4},
	{"IPv6 out", layerALEAuthConnectV6},
	{"IPv6 in", layerALEAuthRecvAcceptV6},
}

// --- raw WFP layouts (64-bit Windows; the build ships windows/amd64 only) ----

// fwpmDisplayData0 mirrors FWPM_DISPLAY_DATA0 (fwpmtypes.h): the name and
// description shown by the firewall UI. The name must not be NULL.
type fwpmDisplayData0 struct {
	name        *uint16
	description *uint16
}

// fwpByteBlob mirrors FWP_BYTE_BLOB (fwptypes.h).
type fwpByteBlob struct {
	size uint32
	_    uint32
	data *byte
}

// fwpValue0 mirrors FWP_VALUE0 / FWP_CONDITION_VALUE0 (fwptypes.h): a data
// type tag plus a pointer-sized union. FWP_UINT8/16/32 store the scalar
// inline; FWP_UINT64 is declared as [unique] UINT64* in fwptypes.idl, and all
// byte-array/mask/blob variants carry a pointer.
type fwpValue0 struct {
	valueType uint32
	_         uint32
	value     uintptr
}

// fwpmFilterCondition0 mirrors FWPM_FILTER_CONDITION0 (fwpmtypes.h). Repeated
// field keys in one filter are ORed; different field keys are ANDed.
type fwpmFilterCondition0 struct {
	fieldKey       windows.GUID
	matchType      uint32
	_              uint32
	conditionValue fwpValue0
}

// fwpmAction0 mirrors FWPM_ACTION0 (fwpmtypes.h). The union member after the
// action type is unused for plain permit/block actions.
type fwpmAction0 struct {
	actionType uint32
	filterType windows.GUID
}

// fwpmFilter0 mirrors FWPM_FILTER0 (fwpmtypes.h). The blank after action is
// the C union's 8-byte alignment pad before providerContextKey (WireGuard's
// generated binding carries the same correction).
type fwpmFilter0 struct {
	filterKey           windows.GUID
	displayData         fwpmDisplayData0
	flags               uint32
	_                   uint32
	providerKey         *windows.GUID
	providerData        fwpByteBlob
	layerKey            windows.GUID
	subLayerKey         windows.GUID
	weight              fwpValue0
	numFilterConditions uint32
	_                   uint32
	filterCondition     *fwpmFilterCondition0
	action              fwpmAction0
	_                   [4]byte
	providerContextKey  windows.GUID
	reserved            *windows.GUID
	filterID            uint64
	effectiveWeight     fwpValue0
}

// fwpmProvider0 mirrors FWPM_PROVIDER0 (fwpmtypes.h).
type fwpmProvider0 struct {
	providerKey  windows.GUID
	displayData  fwpmDisplayData0
	flags        uint32
	_            uint32
	providerData fwpByteBlob
	serviceName  *uint16
}

// fwpmSublayer0 mirrors FWPM_SUBLAYER0 (fwpmtypes.h). A high weight makes the
// sublayer's filters arbitrate before lower-weighted sublayers.
type fwpmSublayer0 struct {
	subLayerKey  windows.GUID
	displayData  fwpmDisplayData0
	flags        uint32
	_            uint32
	providerKey  *windows.GUID
	providerData fwpByteBlob
	weight       uint16
	_            [6]byte
}

// fwpmSession0 mirrors FWPM_SESSION0 (fwpmtypes.h).
type fwpmSession0 struct {
	sessionKey           windows.GUID
	displayData          fwpmDisplayData0
	flags                uint32
	txnWaitTimeoutInMSec uint32
	processId            uint32
	_                    uint32
	sid                  *windows.SID
	username             *uint16
	kernelMode           int32
	_                    uint32
}

// fwpV4AddrAndMask mirrors FWP_V4_ADDR_AND_MASK (fwptypes.h): addr and mask in
// host order, i.e. the numeric value of the network-order bytes.
type fwpV4AddrAndMask struct {
	addr uint32
	mask uint32
}

// fwpV6AddrAndMask mirrors FWP_V6_ADDR_AND_MASK (fwptypes.h): network-order
// address bytes followed by the prefix length (17 bytes, no padding).
type fwpV6AddrAndMask struct {
	addr         [16]byte
	prefixLength uint8
}

// --- fwpuclnt.dll bindings --------------------------------------------------

// The WFP user-mode API lives in fwpuclnt.dll. x/sys/windows types the FWP_E_*
// error codes but none of the fwpmu.h functions or fwpmtypes.h structs, so the
// procedures are loaded lazily here (no Windows API is touched at package
// initialization).
var (
	fwpuclnt = windows.NewLazySystemDLL("fwpuclnt.dll")

	procFwpmEngineOpen0           = fwpuclnt.NewProc("FwpmEngineOpen0")
	procFwpmEngineClose0          = fwpuclnt.NewProc("FwpmEngineClose0")
	procFwpmTransactionBegin0     = fwpuclnt.NewProc("FwpmTransactionBegin0")
	procFwpmTransactionCommit0    = fwpuclnt.NewProc("FwpmTransactionCommit0")
	procFwpmTransactionAbort0     = fwpuclnt.NewProc("FwpmTransactionAbort0")
	procFwpmProviderAdd0          = fwpuclnt.NewProc("FwpmProviderAdd0")
	procFwpmSubLayerAdd0          = fwpuclnt.NewProc("FwpmSubLayerAdd0")
	procFwpmFilterAdd0            = fwpuclnt.NewProc("FwpmFilterAdd0")
	procFwpmFilterDeleteByKey0    = fwpuclnt.NewProc("FwpmFilterDeleteByKey0")
	procFwpmGetAppIdFromFileName0 = fwpuclnt.NewProc("FwpmGetAppIdFromFileName0")
	procFwpmFreeMemory0           = fwpuclnt.NewProc("FwpmFreeMemory0")
)

// fwpmEngineOpen0 opens an engine session; a nil server name and
// RPC_C_AUTHN_WINNT use the local engine and the process token.
func fwpmEngineOpen0(serverName *uint16, authnService uint32, authIdentity unsafe.Pointer, session *fwpmSession0, handle *windows.Handle) error {
	r, _, _ := procFwpmEngineOpen0.Call(
		uintptr(unsafe.Pointer(serverName)),
		uintptr(authnService),
		uintptr(authIdentity),
		uintptr(unsafe.Pointer(session)),
		uintptr(unsafe.Pointer(handle)),
	)
	if r != 0 {
		return windows.Errno(r)
	}
	return nil
}

// fwpmEngineClose0 closes an engine session. Dynamic sessions take their
// provider, sublayer and filters with them.
func fwpmEngineClose0(handle windows.Handle) {
	procFwpmEngineClose0.Call(uintptr(handle))
}

func fwpmTransactionBegin0(handle windows.Handle, flags uint32) error {
	r, _, _ := procFwpmTransactionBegin0.Call(uintptr(handle), uintptr(flags))
	if r != 0 {
		return windows.Errno(r)
	}
	return nil
}

func fwpmTransactionCommit0(handle windows.Handle) error {
	r, _, _ := procFwpmTransactionCommit0.Call(uintptr(handle))
	if r != 0 {
		return windows.Errno(r)
	}
	return nil
}

func fwpmTransactionAbort0(handle windows.Handle) {
	procFwpmTransactionAbort0.Call(uintptr(handle))
}

func fwpmProviderAdd0(handle windows.Handle, provider *fwpmProvider0, sd uintptr) error {
	r, _, _ := procFwpmProviderAdd0.Call(uintptr(handle), uintptr(unsafe.Pointer(provider)), sd)
	if r != 0 {
		return windows.Errno(r)
	}
	return nil
}

func fwpmSubLayerAdd0(handle windows.Handle, sublayer *fwpmSublayer0, sd uintptr) error {
	r, _, _ := procFwpmSubLayerAdd0.Call(uintptr(handle), uintptr(unsafe.Pointer(sublayer)), sd)
	if r != 0 {
		return windows.Errno(r)
	}
	return nil
}

func fwpmFilterAdd0(handle windows.Handle, filter *fwpmFilter0, sd uintptr, id *uint64) error {
	r, _, _ := procFwpmFilterAdd0.Call(uintptr(handle), uintptr(unsafe.Pointer(filter)), sd, uintptr(unsafe.Pointer(id)))
	if r != 0 {
		return windows.Errno(r)
	}
	return nil
}

func fwpmFilterDeleteByKey0(handle windows.Handle, key *windows.GUID) error {
	r, _, _ := procFwpmFilterDeleteByKey0.Call(uintptr(handle), uintptr(unsafe.Pointer(key)))
	if r != 0 {
		return windows.Errno(r)
	}
	return nil
}

// fwpmGetAppIdFromFileName0 returns the ALE application identity blob for one
// executable path; the caller frees it with fwpmFreeMemory0.
func fwpmGetAppIdFromFileName0(fileName *uint16, appID unsafe.Pointer) error {
	r, _, _ := procFwpmGetAppIdFromFileName0.Call(uintptr(unsafe.Pointer(fileName)), uintptr(appID))
	if r != 0 {
		return windows.Errno(r)
	}
	return nil
}

// fwpmFreeMemory0 releases a buffer allocated by the filter engine. The API
// takes void**, so the pointer variable's address is passed.
func fwpmFreeMemory0(p unsafe.Pointer) {
	procFwpmFreeMemory0.Call(uintptr(p))
}

// --- pure permit and filter specs -------------------------------------------

// wfpPermitKind selects which selector a wfpPermit carries.
type wfpPermitKind uint8

const (
	permitLoopback     wfpPermitKind = iota // FWPM_CONDITION_FLAGS has FWP_CONDITION_FLAG_IS_LOOPBACK
	permitApp                               // FWPM_CONDITION_ALE_APP_ID == appPath
	permitInterface                         // FWPM_CONDITION_IP_LOCAL_INTERFACE == luid
	permitDHCP                              // DHCPv4/DHCPv6 and NDP
	permitDNS                               // UDP/TCP port 53 to the addresses
	permitDestinations                      // traffic to the addresses
)

// wfpPermit is one permit class for the kill switch: the tunnel interface LUID,
// the service executable path, or a destination address set, tagged by kind.
// buildPermitFilters returns them in the brief's filter order; buildFilterSpecs
// expands each class into concrete per-layer filters.
type wfpPermit struct {
	kind      wfpPermitKind
	luid      uint64
	appPath   string
	addresses []net.IPNet
}

// wfpCondition is one FWPM_FILTER_CONDITION0 in portable, testable form. The
// payload is carried by exactly one field per valueType: number for the scalar
// types (and the FWP_UINT64 LUID, whose pointer is materialized at apply time),
// data for the byte-array/address-mask types, and appPath for the application
// identity blob resolved with FwpmGetAppIdFromFileName0.
type wfpCondition struct {
	field     windows.GUID
	matchType uint32
	valueType uint32
	number    uint64
	data      []byte
	appPath   string
}

// wfpFilterSpec is one FWPM_FILTER0 to install, independent of the engine.
type wfpFilterSpec struct {
	name       string
	layer      windows.GUID
	weight     uint8
	action     uint32
	conditions []wfpCondition
}

// buildPermitFilters assembles the permit classes in the kill switch's filter
// order: loopback, the service process, the tunnel interface, DHCP/NDP, DNS to
// the configured tunnel resolver, then the routing bypass targets (the
// hardcoded Tailscale ranges and spec.ExcludeCIDRs, plus the detected LAN
// subnets when spec.AllowLAN). appPath is the current executable; lanTargets
// comes from the LAN bypass routes so the firewall and the routing table agree
// on what bypasses the tunnel.
func buildPermitFilters(spec platform.NetConfigSpec, luid uint64, appPath string, lanTargets []net.IPNet) []wfpPermit {
	permits := []wfpPermit{
		{kind: permitLoopback},
		{kind: permitApp, appPath: appPath},
		{kind: permitInterface, luid: luid},
		{kind: permitDHCP},
	}
	if dns := hostNetworks(spec.TunAddr); len(dns) > 0 {
		permits = append(permits, wfpPermit{kind: permitDNS, addresses: dns})
	}
	targets := staticTargets(spec)
	if spec.AllowLAN {
		targets = append(targets, lanTargets...)
	}
	if len(targets) > 0 {
		permits = append(permits, wfpPermit{kind: permitDestinations, addresses: targets})
	}
	return permits
}

// buildFilterSpecs expands the permit classes (and, when requested, the
// WireGuard-style catch-all block) into concrete filters. Permits keep the
// order of the slice and descending weights; the block filters are appended
// last at weight 0 so every permit is evaluated first.
func buildFilterSpecs(permits []wfpPermit, blockAll bool) []wfpFilterSpec {
	var out []wfpFilterSpec
	for i := range permits {
		p := &permits[i]
		weight := permitWeight(i)
		switch p.kind {
		case permitLoopback:
			out = append(out, layerFilters(weight, "Permit loopback", []wfpCondition{{
				field:     condFlags,
				matchType: fwpMatchFlagsAllSet,
				valueType: fwpUint32,
				number:    fwpConditionFlagIsLoopback,
			}})...)
		case permitApp:
			out = append(out, layerFilters(weight, "Permit service process", []wfpCondition{{
				field:     condALEAppID,
				matchType: fwpMatchEqual,
				valueType: fwpByteBlobType,
				appPath:   p.appPath,
			}})...)
		case permitInterface:
			out = append(out, layerFilters(weight, "Permit tunnel interface", []wfpCondition{{
				field:     condIPLocalInterface,
				matchType: fwpMatchEqual,
				valueType: fwpUint64,
				number:    p.luid,
			}})...)
		case permitDHCP:
			out = append(out, dhcpFilterSpecs(weight)...)
		case permitDNS:
			out = append(out, dnsFilterSpecs(p.addresses, weight)...)
		case permitDestinations:
			out = append(out, destinationFilterSpecs(p.addresses, weight)...)
		}
	}
	if blockAll {
		for _, l := range wfpALELayers {
			out = append(out, wfpFilterSpec{
				name:   fmt.Sprintf("Block all (%s)", l.name),
				layer:  l.layer,
				weight: wfpBlockWeight,
				action: fwpActionBlock,
			})
		}
	}
	return out
}

// permitWeight maps the brief's permit position to a filter weight: earlier
// classes win arbitration, every permit stays above the block's 0.
func permitWeight(i int) uint8 {
	if i >= wfpPermitWeightBase {
		return 1
	}
	return wfpPermitWeightBase - uint8(i)
}

// layerFilters clones one condition set onto all four ALE layers.
func layerFilters(weight uint8, name string, conditions []wfpCondition) []wfpFilterSpec {
	out := make([]wfpFilterSpec, 0, len(wfpALELayers))
	for _, l := range wfpALELayers {
		out = append(out, wfpFilterSpec{
			name:       fmt.Sprintf("%s (%s)", name, l.name),
			layer:      l.layer,
			weight:     weight,
			action:     fwpActionPermit,
			conditions: conditions,
		})
	}
	return out
}

// dhcpFilterSpecs permits the DHCPv4/DHCPv6 handshakes and IPv6 neighbour
// discovery (WireGuard's condition sets): without them the physical stack
// cannot renew its lease or resolve neighbours while the kill switch is on.
func dhcpFilterSpecs(weight uint8) []wfpFilterSpec {
	spec := func(name string, layer windows.GUID, conditions ...wfpCondition) wfpFilterSpec {
		return wfpFilterSpec{name: name, layer: layer, weight: weight, action: fwpActionPermit, conditions: conditions}
	}
	out := []wfpFilterSpec{
		spec("Permit outbound DHCP request (IPv4)", layerALEAuthConnectV4,
			scalarCondition(condIPProtocol, fwpUint8, ipprotoUDP),
			scalarCondition(condIPLocalPort, fwpUint16, 68),
			scalarCondition(condIPRemotePort, fwpUint16, 67),
			scalarCondition(condIPRemoteAddress, fwpUint32, 0xffffffff)),
		spec("Permit inbound DHCP response (IPv4)", layerALEAuthRecvAcceptV4,
			scalarCondition(condIPProtocol, fwpUint8, ipprotoUDP),
			scalarCondition(condIPLocalPort, fwpUint16, 68),
			scalarCondition(condIPRemotePort, fwpUint16, 67)),
		spec("Permit outbound DHCP request (IPv6)", layerALEAuthConnectV6,
			scalarCondition(condIPProtocol, fwpUint8, ipprotoUDP),
			address16Condition(condIPRemoteAddress, dhcpv6AllRelayAgentsAndServers),
			address16Condition(condIPRemoteAddress, dhcpv6AllDHCPServers),
			scalarCondition(condIPRemotePort, fwpUint16, 547),
			maskCondition(condIPLocalAddress, ipv6LinkLocal),
			scalarCondition(condIPLocalPort, fwpUint16, 546)),
		spec("Permit inbound DHCP response (IPv6)", layerALEAuthRecvAcceptV6,
			scalarCondition(condIPProtocol, fwpUint8, ipprotoUDP),
			maskCondition(condIPRemoteAddress, ipv6LinkLocal),
			scalarCondition(condIPRemotePort, fwpUint16, 547),
			maskCondition(condIPLocalAddress, ipv6LinkLocal),
			scalarCondition(condIPLocalPort, fwpUint16, 546)),
	}
	ndp := func(name string, layer windows.GUID, icmpType uint64, extra ...wfpCondition) wfpFilterSpec {
		conditions := []wfpCondition{
			scalarCondition(condIPProtocol, fwpUint8, ipprotoICMPv6),
			scalarCondition(condICMPType, fwpUint16, icmpType),
			scalarCondition(condICMPCode, fwpUint16, 0),
		}
		return spec(name, layer, append(conditions, extra...)...)
	}
	return append(out,
		ndp("Permit NDP router solicitation (133)", layerALEAuthConnectV6, 133, address16Condition(condIPRemoteAddress, ndpAllRouters)),
		ndp("Permit NDP router advertisement (134)", layerALEAuthRecvAcceptV6, 134, maskCondition(condIPRemoteAddress, ipv6LinkLocal)),
		ndp("Permit NDP neighbor solicitation (135, out)", layerALEAuthConnectV6, 135),
		ndp("Permit NDP neighbor solicitation (135, in)", layerALEAuthRecvAcceptV6, 135),
		ndp("Permit NDP neighbor advertisement (136, out)", layerALEAuthConnectV6, 136),
		ndp("Permit NDP neighbor advertisement (136, in)", layerALEAuthRecvAcceptV6, 136),
		ndp("Permit NDP redirect (137)", layerALEAuthRecvAcceptV6, 137, maskCondition(condIPRemoteAddress, ipv6LinkLocal)),
	)
}

// dnsFilterSpecs permits UDP and TCP port 53 to the configured resolver
// address on both the connect and the accept layers, mirroring WireGuard's
// DNS-exception filters.
func dnsFilterSpecs(addrs []net.IPNet, weight uint8) []wfpFilterSpec {
	v4, v6 := splitNetworks(addrs)
	var out []wfpFilterSpec
	if len(v4) > 0 {
		conditions := []wfpCondition{
			scalarCondition(condIPRemotePort, fwpUint16, 53),
			scalarCondition(condIPProtocol, fwpUint8, ipprotoUDP),
			scalarCondition(condIPProtocol, fwpUint8, ipprotoTCP),
		}
		for i := range v4 {
			conditions = append(conditions, maskCondition(condIPRemoteAddress, v4[i]))
		}
		out = append(out,
			wfpFilterSpec{name: "Permit DNS to the tunnel resolver (IPv4 out)", layer: layerALEAuthConnectV4, weight: weight, action: fwpActionPermit, conditions: conditions},
			wfpFilterSpec{name: "Permit DNS to the tunnel resolver (IPv4 in)", layer: layerALEAuthRecvAcceptV4, weight: weight, action: fwpActionPermit, conditions: conditions},
		)
	}
	if len(v6) > 0 {
		conditions := []wfpCondition{
			scalarCondition(condIPRemotePort, fwpUint16, 53),
			scalarCondition(condIPProtocol, fwpUint8, ipprotoUDP),
			scalarCondition(condIPProtocol, fwpUint8, ipprotoTCP),
		}
		for i := range v6 {
			conditions = append(conditions, maskCondition(condIPRemoteAddress, v6[i]))
		}
		out = append(out,
			wfpFilterSpec{name: "Permit DNS to the tunnel resolver (IPv6 out)", layer: layerALEAuthConnectV6, weight: weight, action: fwpActionPermit, conditions: conditions},
			wfpFilterSpec{name: "Permit DNS to the tunnel resolver (IPv6 in)", layer: layerALEAuthRecvAcceptV6, weight: weight, action: fwpActionPermit, conditions: conditions},
		)
	}
	return out
}

// destinationFilterSpecs permits traffic to the routing bypass destinations
// (Tailscale ranges, exclude_cidrs, LAN subnets) on both directions of each
// address family.
func destinationFilterSpecs(addrs []net.IPNet, weight uint8) []wfpFilterSpec {
	v4, v6 := splitNetworks(addrs)
	var out []wfpFilterSpec
	if len(v4) > 0 {
		conditions := make([]wfpCondition, 0, len(v4))
		for i := range v4 {
			conditions = append(conditions, maskCondition(condIPRemoteAddress, v4[i]))
		}
		out = append(out,
			wfpFilterSpec{name: "Permit LAN and excluded destinations (IPv4 out)", layer: layerALEAuthConnectV4, weight: weight, action: fwpActionPermit, conditions: conditions},
			wfpFilterSpec{name: "Permit LAN and excluded destinations (IPv4 in)", layer: layerALEAuthRecvAcceptV4, weight: weight, action: fwpActionPermit, conditions: conditions},
		)
	}
	if len(v6) > 0 {
		conditions := make([]wfpCondition, 0, len(v6))
		for i := range v6 {
			conditions = append(conditions, maskCondition(condIPRemoteAddress, v6[i]))
		}
		out = append(out,
			wfpFilterSpec{name: "Permit LAN and excluded destinations (IPv6 out)", layer: layerALEAuthConnectV6, weight: weight, action: fwpActionPermit, conditions: conditions},
			wfpFilterSpec{name: "Permit LAN and excluded destinations (IPv6 in)", layer: layerALEAuthRecvAcceptV6, weight: weight, action: fwpActionPermit, conditions: conditions},
		)
	}
	return out
}

// scalarCondition builds an inline-scalar condition (FWP_UINT8/16/32 values and
// the FWP_UINT64 LUID, whose number is pointered at apply time).
func scalarCondition(field windows.GUID, valueType uint32, value uint64) wfpCondition {
	return wfpCondition{field: field, matchType: fwpMatchEqual, valueType: valueType, number: value}
}

// address16Condition builds an exact FWP_BYTE_ARRAY16_TYPE address condition.
func address16Condition(field windows.GUID, addr [16]byte) wfpCondition {
	return wfpCondition{field: field, matchType: fwpMatchEqual, valueType: fwpByteArray16Type, data: addr[:]}
}

// maskCondition builds an address-prefix condition, using the family-specific
// WFP address-and-mask value type.
func maskCondition(field windows.GUID, n net.IPNet) wfpCondition {
	if n.IP.To4() != nil {
		return wfpCondition{field: field, matchType: fwpMatchEqual, valueType: fwpV4AddrMask, data: v4MaskData(n)}
	}
	return wfpCondition{field: field, matchType: fwpMatchEqual, valueType: fwpV6AddrMask, data: v6MaskData(n)}
}

// v4MaskData renders FWP_V4_ADDR_AND_MASK for one IPv4 prefix. Both members
// hold the host-order numeric value of the network-order bytes (MSDN), so the
// bytes are encoded in this machine's byte order.
func v4MaskData(n net.IPNet) []byte {
	ip := n.IP.To4()
	if ip == nil {
		return nil
	}
	ones, bits := n.Mask.Size()
	if bits != 32 {
		return nil
	}
	data := make([]byte, unsafe.Sizeof(fwpV4AddrAndMask{}))
	binary.NativeEndian.PutUint32(data[0:4], binary.BigEndian.Uint32(ip))
	binary.NativeEndian.PutUint32(data[4:8], binary.BigEndian.Uint32(net.CIDRMask(ones, 32)))
	return data
}

// v6MaskData renders FWP_V6_ADDR_AND_MASK for one IPv6 prefix: the 16
// network-order address bytes followed by the prefix length.
func v6MaskData(n net.IPNet) []byte {
	if n.IP.To4() != nil {
		return nil
	}
	ip := n.IP.To16()
	if ip == nil {
		return nil
	}
	ones, bits := n.Mask.Size()
	if bits != 128 {
		return nil
	}
	data := make([]byte, unsafe.Sizeof(fwpV6AddrAndMask{}))
	copy(data[:16], ip)
	data[16] = uint8(ones)
	return data
}

// splitNetworks partitions mixed prefixes into families; entries that are
// neither parse keep neither list.
func splitNetworks(addrs []net.IPNet) (v4, v6 []net.IPNet) {
	for i := range addrs {
		switch {
		case addrs[i].IP.To4() != nil:
			v4 = append(v4, addrs[i])
		case addrs[i].IP.To16() != nil:
			v6 = append(v6, addrs[i])
		}
	}
	return v4, v6
}

// hostNetworks turns one host address into its /32 or /128 prefix form for
// remote-address matching.
func hostNetworks(addr string) []net.IPNet {
	ip := net.ParseIP(addr)
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return []net.IPNet{{IP: append(net.IP(nil), v4...), Mask: net.CIDRMask(32, 32)}}
	}
	return []net.IPNet{{IP: append(net.IP(nil), ip.To16()...), Mask: net.CIDRMask(128, 128)}}
}

// --- dynamic session blocker -------------------------------------------------

// wfpBlockerAPI is the netConfig-facing behavior of the kill-switch blocker.
// *wfpBlocker is the real implementation; tests inject fakes so Apply stays
// hermetic.
type wfpBlockerAPI interface {
	apply(permit []wfpPermit, blockAll bool) error
	clear() error
	close()
}

// wfpBlocker owns one dynamic WFP engine session with kumiho's provider,
// sublayer and the currently installed kill-switch filters, addressed by their
// generated keys so apply and clear can replace them idempotently.
//
// See the file comment for the crash caveat: a dynamic session dies with this
// process, and service recovery plus the start-time cleanup re-apply.
type wfpBlocker struct {
	mu          sync.Mutex
	session     windows.Handle
	providerKey windows.GUID
	sublayerKey windows.GUID
	filterKeys  []windows.GUID
}

var _ wfpBlockerAPI = (*wfpBlocker)(nil)

// openWFPBlocker opens the dynamic session and registers kumiho's provider and
// sublayer. The session needs elevation (ERROR_ACCESS_DENIED otherwise), which
// the caller surfaces as a connect failure: a requested kill switch that
// cannot be enforced must not silently pass traffic.
func openWFPBlocker() (*wfpBlocker, error) {
	providerKey, err := windows.GenerateGUID()
	if err != nil {
		return nil, fmt.Errorf("wfp: generate provider key: %w", err)
	}
	sublayerKey, err := windows.GenerateGUID()
	if err != nil {
		return nil, fmt.Errorf("wfp: generate sublayer key: %w", err)
	}
	name, err := windows.UTF16PtrFromString("Kumiho kill switch")
	if err != nil {
		return nil, fmt.Errorf("wfp: session name: %w", err)
	}
	description, err := windows.UTF16PtrFromString("dynamic kill-switch session")
	if err != nil {
		return nil, fmt.Errorf("wfp: session description: %w", err)
	}
	session := fwpmSession0{
		displayData:          fwpmDisplayData0{name: name, description: description},
		flags:                fwpmSessionFlagDynamic,
		txnWaitTimeoutInMSec: windows.INFINITE,
	}
	var handle windows.Handle
	if err := fwpmEngineOpen0(nil, rpcCAuthNWinNT, nil, &session, &handle); err != nil {
		return nil, fmt.Errorf("wfp: open engine: %w", err)
	}
	b := &wfpBlocker{session: handle, providerKey: providerKey, sublayerKey: sublayerKey}
	if err := b.register(); err != nil {
		b.close()
		return nil, err
	}
	return b, nil
}

// register adds the provider and the sublayer in one transaction.
func (b *wfpBlocker) register() error {
	providerName, err := windows.UTF16PtrFromString("Kumiho")
	if err != nil {
		return fmt.Errorf("wfp: provider name: %w", err)
	}
	providerDescription, err := windows.UTF16PtrFromString("Kumiho kill-switch provider")
	if err != nil {
		return fmt.Errorf("wfp: provider description: %w", err)
	}
	sublayerName, err := windows.UTF16PtrFromString("Kumiho kill switch")
	if err != nil {
		return fmt.Errorf("wfp: sublayer name: %w", err)
	}
	sublayerDescription, err := windows.UTF16PtrFromString("Kumiho kill-switch filters")
	if err != nil {
		return fmt.Errorf("wfp: sublayer description: %w", err)
	}
	provider := fwpmProvider0{
		providerKey: b.providerKey,
		displayData: fwpmDisplayData0{name: providerName, description: providerDescription},
	}
	sublayer := fwpmSublayer0{
		subLayerKey: b.sublayerKey,
		displayData: fwpmDisplayData0{name: sublayerName, description: sublayerDescription},
		providerKey: &b.providerKey,
		weight:      ^uint16(0),
	}
	err = wfpTransaction(b.session, func() error {
		if err := fwpmProviderAdd0(b.session, &provider, 0); err != nil {
			return fmt.Errorf("wfp: add provider: %w", err)
		}
		if err := fwpmSubLayerAdd0(b.session, &sublayer, 0); err != nil {
			return fmt.Errorf("wfp: add sublayer: %w", err)
		}
		return nil
	})
	runtime.KeepAlive(providerName)
	runtime.KeepAlive(providerDescription)
	runtime.KeepAlive(sublayerName)
	runtime.KeepAlive(sublayerDescription)
	runtime.KeepAlive(b)
	return err
}

// apply replaces the installed filters with the program built from permit (and
// the catch-all block when blockAll is set). Replacing happens in one
// transaction, so a failure leaves the previous program in place and repeated
// applications are idempotent.
func (b *wfpBlocker) apply(permit []wfpPermit, blockAll bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session == 0 {
		return errWFPClosed
	}

	specs := buildFilterSpecs(permit, blockAll)
	appIDs, err := resolveAppIDs(specs)
	if err != nil {
		return err
	}
	defer appIDs.free()

	newKeys := make([]windows.GUID, 0, len(specs))
	var refs []any
	err = wfpTransaction(b.session, func() error {
		for i := range specs {
			key, err := windows.GenerateGUID()
			if err != nil {
				return fmt.Errorf("wfp: generate filter key: %w", err)
			}
			raw, keep, err := b.rawFilter(&specs[i], key, appIDs)
			if err != nil {
				return err
			}
			refs = append(refs, keep...)
			if err := fwpmFilterAdd0(b.session, raw, 0, nil); err != nil {
				return fmt.Errorf("wfp: add filter %q: %w", specs[i].name, err)
			}
			newKeys = append(newKeys, key)
		}
		for i := range b.filterKeys {
			if err := deleteWFPFilter(b.session, &b.filterKeys[i]); err != nil {
				return err
			}
		}
		return nil
	})
	runtime.KeepAlive(refs)
	if err != nil {
		return err
	}
	b.filterKeys = newKeys
	return nil
}

// clear removes every installed filter, leaving the provider and sublayer in
// place. It is a no-op on an empty program and on a closed blocker.
func (b *wfpBlocker) clear() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session == 0 || len(b.filterKeys) == 0 {
		return nil
	}
	err := wfpTransaction(b.session, func() error {
		for i := range b.filterKeys {
			if err := deleteWFPFilter(b.session, &b.filterKeys[i]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	b.filterKeys = nil
	return nil
}

// close ends the dynamic session, removing the provider, sublayer and any
// remaining filters with it. Safe to call more than once.
func (b *wfpBlocker) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session == 0 {
		return
	}
	fwpmEngineClose0(b.session)
	b.session = 0
	b.filterKeys = nil
}

// rawFilter converts one pure filter spec into its FWPM_FILTER0 wire form. The
// returned slice holds the allocations the struct points at (uintptr fields
// are invisible to the GC) and must stay reachable until the filter has been
// added.
func (b *wfpBlocker) rawFilter(spec *wfpFilterSpec, key windows.GUID, appIDs *appIDSet) (*fwpmFilter0, []any, error) {
	name, err := windows.UTF16PtrFromString(spec.name)
	if err != nil {
		return nil, nil, fmt.Errorf("wfp: filter name: %w", err)
	}
	keep := []any{name, spec.conditions}
	raw := &fwpmFilter0{
		filterKey:   key,
		displayData: fwpmDisplayData0{name: name},
		providerKey: &b.providerKey,
		layerKey:    spec.layer,
		subLayerKey: b.sublayerKey,
		weight:      fwpValue0{valueType: fwpUint8, value: uintptr(spec.weight)},
		action:      fwpmAction0{actionType: spec.action},
	}
	if len(spec.conditions) == 0 {
		return raw, keep, nil
	}
	conditions := make([]fwpmFilterCondition0, len(spec.conditions))
	for i := range spec.conditions {
		c := &spec.conditions[i]
		rawCondition := &conditions[i]
		rawCondition.fieldKey = c.field
		rawCondition.matchType = c.matchType
		rawCondition.conditionValue.valueType = c.valueType
		switch c.valueType {
		case fwpUint8, fwpUint16, fwpUint32:
			rawCondition.conditionValue.value = uintptr(c.number)
		case fwpUint64:
			value := new(uint64)
			*value = c.number
			keep = append(keep, value)
			rawCondition.conditionValue.value = uintptr(unsafe.Pointer(value))
		case fwpByteArray16Type, fwpV4AddrMask, fwpV6AddrMask:
			if len(c.data) == 0 {
				return nil, nil, fmt.Errorf("wfp: filter %q: empty condition payload", spec.name)
			}
			rawCondition.conditionValue.value = uintptr(unsafe.Pointer(&c.data[0]))
		case fwpByteBlobType:
			blob := appIDs.lookup(c.appPath)
			if blob == nil {
				return nil, nil, fmt.Errorf("wfp: filter %q: no application id for %q", spec.name, c.appPath)
			}
			rawCondition.conditionValue.value = uintptr(unsafe.Pointer(blob))
		default:
			return nil, nil, fmt.Errorf("wfp: filter %q: unsupported condition value type %d", spec.name, c.valueType)
		}
	}
	raw.numFilterConditions = uint32(len(conditions))
	raw.filterCondition = &conditions[0]
	keep = append(keep, conditions)
	return raw, keep, nil
}

// wfpTransaction runs fn inside one engine transaction: any error aborts it,
// so the filter program changes atomically.
func wfpTransaction(session windows.Handle, fn func() error) error {
	if err := fwpmTransactionBegin0(session, 0); err != nil {
		return fmt.Errorf("wfp: begin transaction: %w", err)
	}
	if err := fn(); err != nil {
		fwpmTransactionAbort0(session)
		return err
	}
	if err := fwpmTransactionCommit0(session); err != nil {
		fwpmTransactionAbort0(session)
		return fmt.Errorf("wfp: commit transaction: %w", err)
	}
	return nil
}

// deleteWFPFilter removes one tracked filter; a stale key (already gone with a
// previous session, say) is a successful outcome.
func deleteWFPFilter(session windows.Handle, key *windows.GUID) error {
	err := fwpmFilterDeleteByKey0(session, key)
	if err != nil && err != windows.Errno(windows.FWP_E_FILTER_NOT_FOUND) {
		return fmt.Errorf("wfp: delete filter: %w", err)
	}
	return nil
}

// appIDEntry is one resolved FWP_BYTE_BLOB_TYPE application identity.
type appIDEntry struct {
	path string
	blob *fwpByteBlob
}

// appIDSet caches the application identity blobs a filter program needs.
type appIDSet struct {
	entries []appIDEntry
}

// resolveAppIDs resolves every distinct appPath in specs with
// FwpmGetAppIdFromFileName0. The caller must free the set.
func resolveAppIDs(specs []wfpFilterSpec) (*appIDSet, error) {
	set := &appIDSet{}
	for i := range specs {
		for j := range specs[i].conditions {
			c := &specs[i].conditions[j]
			if c.valueType != fwpByteBlobType || set.lookup(c.appPath) != nil {
				continue
			}
			blob, err := appIDFromFileName(c.appPath)
			if err != nil {
				set.free()
				return nil, err
			}
			set.entries = append(set.entries, appIDEntry{path: c.appPath, blob: blob})
		}
	}
	return set, nil
}

func (s *appIDSet) lookup(path string) *fwpByteBlob {
	for i := range s.entries {
		if s.entries[i].path == path {
			return s.entries[i].blob
		}
	}
	return nil
}

// free releases every engine-allocated blob.
func (s *appIDSet) free() {
	for i := range s.entries {
		fwpmFreeMemory0(unsafe.Pointer(&s.entries[i].blob))
	}
	s.entries = nil
}

// appIDFromFileName wraps FwpmGetAppIdFromFileName0.
func appIDFromFileName(path string) (*fwpByteBlob, error) {
	path16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("wfp: application path: %w", err)
	}
	var blob *fwpByteBlob
	if err := fwpmGetAppIdFromFileName0(path16, unsafe.Pointer(&blob)); err != nil {
		return nil, fmt.Errorf("wfp: application id for %q: %w", path, err)
	}
	return blob, nil
}
