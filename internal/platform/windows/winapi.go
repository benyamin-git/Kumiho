//go:build windows

package windows

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The IP Helper DLL is loaded lazily: no Windows API is touched at package
// initialization, and a machine without the tunnel never pays for it. The
// functions x/sys/windows already wraps (GetAdaptersAddresses,
// GetIpForwardTable2, FreeMibTable, NotifyIpInterfaceChange) are reused from
// there; the procedures below are the ones x/sys does not type.
var (
	iphlpapi = windows.NewLazySystemDLL("iphlpapi.dll")

	procConvertInterfaceAliasToLuid = iphlpapi.NewProc("ConvertInterfaceAliasToLuid")
	procCreateUnicastIPAddressEntry = iphlpapi.NewProc("CreateUnicastIpAddressEntry")
	procDeleteUnicastIPAddressEntry = iphlpapi.NewProc("DeleteUnicastIpAddressEntry")
	procInitializeIPInterfaceEntry  = iphlpapi.NewProc("InitializeIpInterfaceEntry")
	procSetIPInterfaceEntry         = iphlpapi.NewProc("SetIpInterfaceEntry")
	procCreateIPForwardEntry2       = iphlpapi.NewProc("CreateIpForwardEntry2")
	procDeleteIPForwardEntry2       = iphlpapi.NewProc("DeleteIpForwardEntry2")
	procSetInterfaceDNSSettings     = iphlpapi.NewProc("SetInterfaceDnsSettings")
)

// convertInterfaceAliasToLUID maps an adapter alias ("foxy0") to its
// interface LUID.
func convertInterfaceAliasToLUID(alias string) (uint64, error) {
	alias16, err := windows.UTF16PtrFromString(alias)
	if err != nil {
		return 0, err
	}
	var luid uint64
	r, _, _ := procConvertInterfaceAliasToLuid.Call(
		uintptr(unsafe.Pointer(alias16)), uintptr(unsafe.Pointer(&luid)))
	if r != 0 {
		return 0, windows.Errno(r)
	}
	return luid, nil
}

// createUnicastIPAddressEntry adds one address to an interface.
func createUnicastIPAddressEntry(row *windows.MibUnicastIpAddressRow) error {
	r, _, _ := procCreateUnicastIPAddressEntry.Call(uintptr(unsafe.Pointer(row)))
	if r != 0 {
		return windows.Errno(r)
	}
	return nil
}

// deleteUnicastIPAddressEntry removes one address from an interface. The match
// is on address and interface; OnLinkPrefixLength is ignored.
func deleteUnicastIPAddressEntry(row *windows.MibUnicastIpAddressRow) error {
	r, _, _ := procDeleteUnicastIPAddressEntry.Call(uintptr(unsafe.Pointer(row)))
	if r != 0 {
		return windows.Errno(r)
	}
	return nil
}

// initializeIPInterfaceEntry resets a row to API defaults (family AF_UNSPEC,
// LUID unspecified, all other fields zero).
func initializeIPInterfaceEntry(row *windows.MibIpInterfaceRow) {
	procInitializeIPInterfaceEntry.Call(uintptr(unsafe.Pointer(row)))
}

// setIPInterfaceEntry writes an interface row: family and LUID select the
// interface, the remaining fields are the desired values.
func setIPInterfaceEntry(row *windows.MibIpInterfaceRow) error {
	r, _, _ := procSetIPInterfaceEntry.Call(uintptr(unsafe.Pointer(row)))
	if r != 0 {
		return windows.Errno(r)
	}
	return nil
}

// createIPForwardEntry2 adds one route.
func createIPForwardEntry2(row *windows.MibIpForwardRow2) error {
	r, _, _ := procCreateIPForwardEntry2.Call(uintptr(unsafe.Pointer(row)))
	if r != 0 {
		return windows.Errno(r)
	}
	return nil
}

// deleteIPForwardEntry2 removes the route matching destination, next hop and
// interface.
func deleteIPForwardEntry2(row *windows.MibIpForwardRow2) error {
	r, _, _ := procDeleteIPForwardEntry2.Call(uintptr(unsafe.Pointer(row)))
	if r != 0 {
		return windows.Errno(r)
	}
	return nil
}

// getIPForwardTable2 returns a copy of the routing table for family. The
// table buffer is owned by the API and is released before returning; callers
// must be able to use the rows after it is freed.
func getIPForwardTable2(family uint16) ([]windows.MibIpForwardRow2, error) {
	var table *windows.MibIpForwardTable2
	if err := windows.GetIpForwardTable2(family, &table); err != nil {
		return nil, err
	}
	defer freeMibTable(unsafe.Pointer(table))
	return append([]windows.MibIpForwardRow2(nil), table.Rows()...), nil
}

// freeMibTable releases a table buffer allocated by one of the IP Helper
// table functions.
func freeMibTable(memory unsafe.Pointer) {
	windows.FreeMibTable(memory)
}

// dnsInterfaceSettings mirrors DNS_INTERFACE_SETTINGS (netioapi.h). Version 1
// is the only layout SetInterfaceDnsSettings needs here; the four-byte pad
// after Version matches the C alignment of the 64-bit Flags field.
type dnsInterfaceSettings struct {
	Version             uint32
	_                   [4]byte
	Flags               uint64
	Domain              *uint16
	NameServer          *uint16
	SearchList          *uint16
	RegistrationEnabled uint32
	RegisterAdapterName uint32
	EnableLLMNR         uint32
	QueryAdapterName    uint32
	ProfileNameServer   *uint16
}

const (
	dnsSettingsVersion1  = 1
	dnsSettingNameServer = 0x0002
)

// setInterfaceDNSSettings sets per-interface DNS settings. The GUID is passed
// by value, which the Windows ABI words differently per architecture: by
// reference on amd64, in two registers on arm64, and as four words on
// 386/arm. Only amd64 ships, but the branches keep cross-builds meaningful.
// The unsafe.Pointer conversions are inlined in the Call arguments (WireGuard
// style) so settings stays reachable for the duration of the call.
func setInterfaceDNSSettings(guid windows.GUID, settings *dnsInterfaceSettings) error {
	var r uintptr
	switch runtime.GOARCH {
	case "amd64":
		r, _, _ = procSetInterfaceDNSSettings.Call(uintptr(unsafe.Pointer(&guid)), uintptr(unsafe.Pointer(settings)))
	case "arm64":
		words := (*[2]uintptr)(unsafe.Pointer(&guid))
		r, _, _ = procSetInterfaceDNSSettings.Call(words[0], words[1], uintptr(unsafe.Pointer(settings)))
	case "arm", "386":
		words := (*[4]uintptr)(unsafe.Pointer(&guid))
		r, _, _ = procSetInterfaceDNSSettings.Call(words[0], words[1], words[2], words[3], uintptr(unsafe.Pointer(settings)))
	default:
		return fmt.Errorf("windows netcfg: SetInterfaceDnsSettings is unsupported on GOARCH %s", runtime.GOARCH)
	}
	if r != 0 {
		return windows.Errno(r)
	}
	return nil
}

// notifyIPInterfaceChange registers for interface-change notifications and
// returns the handle used to cancel the registration. Callers are expected in
// T12 (link monitoring).
func notifyIPInterfaceChange(family uint16, callback uintptr, callerContext unsafe.Pointer, initialNotification bool) (windows.Handle, error) {
	var handle windows.Handle
	if err := windows.NotifyIpInterfaceChange(family, callback, callerContext, initialNotification, &handle); err != nil {
		return 0, err
	}
	return handle, nil
}
