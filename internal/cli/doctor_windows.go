//go:build windows

package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	winio "github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"

	"github.com/benyamin-git/kumiho/internal/netcfg"
	"github.com/benyamin-git/kumiho/internal/platform/auto"
)

// fullTunnelPlatform reports the full-tunnel provider on this OS and the
// detail the platform check prints for it. Windows joined Linux in v0.2.0.
func fullTunnelPlatform(provider string) (string, bool) {
	if provider == "windows" {
		return "windows (full tunnel supported)", true
	}
	return "", false
}

// platformChecks returns the Windows-specific doctor checks appended to the
// common list. Order matters: it is the order they appear in the report.
func platformChecks() []check {
	return []check{
		checkWindowsService(),
		checkWintunDLL(),
		checkWFPService(),
		checkControlPipe(),
		checkTunAdapter(),
		checkAdapterDNS(),
		checkPhysicalDefaultRoute(),
		checkElevation(),
	}
}

// checkWindowsService reports whether the kumiho service is installed, its
// state and its start type. A foreground daemon is a valid setup, so absence
// or a stopped service only warns.
func checkWindowsService() check {
	const name = "windows-service"
	mgr, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return check{Name: name, Status: statusWarn, Detail: fmt.Sprintf("cannot open the service manager: %v", err)}
	}
	defer windows.CloseServiceHandle(mgr)

	h, err := windows.OpenService(mgr, windows.StringToUTF16Ptr("kumiho"),
		windows.SERVICE_QUERY_STATUS|windows.SERVICE_QUERY_CONFIG)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return check{
				Name:        name,
				Status:      statusWarn,
				Detail:      "kumiho service is not installed",
				Remediation: "Install it with install.ps1 from an elevated PowerShell, or run 'kumiho daemon' in the foreground.",
			}
		}
		return check{Name: name, Status: statusWarn, Detail: fmt.Sprintf("cannot open the kumiho service: %v", err)}
	}
	defer windows.CloseServiceHandle(h)

	state, err := serviceState(h)
	if err != nil {
		return check{Name: name, Status: statusWarn, Detail: fmt.Sprintf("cannot query the service state: %v", err)}
	}
	detail := "kumiho service is " + serviceStateName(state)
	if start, err := serviceStartType(h); err == nil {
		detail += "; start type " + serviceStartTypeName(start)
	}
	if state == windows.SERVICE_RUNNING {
		return check{Name: name, Status: statusOK, Detail: detail}
	}
	return check{
		Name:        name,
		Status:      statusWarn,
		Detail:      detail,
		Remediation: "Start it: sc start kumiho (daemon output goes to %ProgramData%\\kumiho\\logs\\kumiho.log).",
	}
}

// serviceState queries the current state of an open service handle.
func serviceState(h windows.Handle) (uint32, error) {
	var status windows.SERVICE_STATUS_PROCESS
	var needed uint32
	err := windows.QueryServiceStatusEx(h, windows.SC_STATUS_PROCESS_INFO,
		(*byte)(unsafe.Pointer(&status)), uint32(unsafe.Sizeof(status)), &needed)
	return status.CurrentState, err
}

// serviceStartType queries the configured start type of an open service
// handle. QueryServiceConfig returns the strings behind the struct in the
// caller buffer, so the buffer stays alive for the whole call.
func serviceStartType(h windows.Handle) (uint32, error) {
	var needed uint32
	if err := windows.QueryServiceConfig(h, nil, 0, &needed); err != nil &&
		!errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
		return 0, err
	}
	if needed == 0 {
		return 0, errors.New("service configuration is empty")
	}
	buf := make([]byte, needed)
	cfg := (*windows.QUERY_SERVICE_CONFIG)(unsafe.Pointer(&buf[0]))
	if err := windows.QueryServiceConfig(h, cfg, uint32(len(buf)), &needed); err != nil {
		return 0, err
	}
	return cfg.StartType, nil
}

func serviceStateName(state uint32) string {
	switch state {
	case windows.SERVICE_STOPPED:
		return "stopped"
	case windows.SERVICE_START_PENDING:
		return "start pending"
	case windows.SERVICE_STOP_PENDING:
		return "stop pending"
	case windows.SERVICE_RUNNING:
		return "running"
	case windows.SERVICE_CONTINUE_PENDING:
		return "continue pending"
	case windows.SERVICE_PAUSE_PENDING:
		return "pause pending"
	case windows.SERVICE_PAUSED:
		return "paused"
	default:
		return fmt.Sprintf("in state %d", state)
	}
}

func serviceStartTypeName(start uint32) string {
	switch start {
	case windows.SERVICE_BOOT_START:
		return "boot"
	case windows.SERVICE_SYSTEM_START:
		return "system"
	case windows.SERVICE_AUTO_START:
		return "auto"
	case windows.SERVICE_DEMAND_START:
		return "demand"
	case windows.SERVICE_DISABLED:
		return "disabled"
	default:
		return fmt.Sprintf("unknown (%d)", start)
	}
}

// checkWintunDLL verifies the Wintun userspace driver ships beside the
// executable (D11) and reports the DLL version.
func checkWintunDLL() check {
	const name = "wintun-dll"
	exe, err := os.Executable()
	if err != nil {
		return check{Name: name, Status: statusWarn, Detail: fmt.Sprintf("cannot locate the executable: %v", err)}
	}
	dll := filepath.Join(filepath.Dir(exe), "wintun.dll")
	if _, err := os.Stat(dll); err != nil {
		return check{
			Name:        name,
			Status:      statusFail,
			Detail:      fmt.Sprintf("%s not found", dll),
			Remediation: "Deploy wintun.dll (0.14.1) next to kumiho.exe; the release zip and install.ps1 do this for you.",
		}
	}
	version := wintun.Version()
	if version == "unknown" {
		return check{Name: name, Status: statusWarn, Detail: "wintun.dll is present beside the executable; its version could not be read"}
	}
	return check{Name: name, Status: statusOK, Detail: fmt.Sprintf("wintun.dll %s beside the executable", version)}
}

// checkWFPService verifies the Base Filtering Engine, which hosts the WFP
// engine the dynamic kill switch programs.
func checkWFPService() check {
	const name = "wfp-service"
	state, err := queryServiceState("BFE")
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return check{
				Name:        name,
				Status:      statusFail,
				Detail:      "Base Filtering Engine (BFE) service not found",
				Remediation: "The WFP kill switch needs the Windows Filtering Platform; restore the built-in BFE service.",
			}
		}
		return check{Name: name, Status: statusWarn, Detail: fmt.Sprintf("cannot query the Base Filtering Engine: %v", err)}
	}
	if state == windows.SERVICE_RUNNING {
		return check{Name: name, Status: statusOK, Detail: "Base Filtering Engine (BFE) is running"}
	}
	return check{
		Name:        name,
		Status:      statusFail,
		Detail:      "Base Filtering Engine (BFE) is " + serviceStateName(state) + "; the WFP kill switch cannot be enforced",
		Remediation: "Start it: sc start BFE (BFE is an automatic service by default).",
	}
}

// queryServiceState opens one service for a status read.
func queryServiceState(name string) (uint32, error) {
	mgr, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return 0, err
	}
	defer windows.CloseServiceHandle(mgr)
	h, err := windows.OpenService(mgr, windows.StringToUTF16Ptr(name), windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return 0, err
	}
	defer windows.CloseServiceHandle(h)
	return serviceState(h)
}

// checkControlPipe dials the daemon's named pipe. A successful dial proves
// the daemon is accepting clients; a missing pipe means it is not running.
func checkControlPipe() check {
	const name = "control-pipe"
	endpoint := auto.Current().ControlEndpoint()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := winio.DialPipeContext(ctx, endpoint)
	if err == nil {
		_ = conn.Close()
		return check{Name: name, Status: statusOK, Detail: endpoint + " is reachable"}
	}
	switch {
	case errors.Is(err, windows.ERROR_FILE_NOT_FOUND):
		return skipped(name, "daemon not running")
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return check{
			Name:        name,
			Status:      statusWarn,
			Detail:      fmt.Sprintf("daemon pipe exists but this user cannot connect: %v", err),
			Remediation: "Run elevated, or add this user to the local 'kumiho' group and sign in again.",
		}
	case errors.Is(err, context.DeadlineExceeded):
		return check{
			Name:        name,
			Status:      statusWarn,
			Detail:      "control pipe is reachable but busy (all instances in use)",
			Remediation: "Another client may be mid-request; retry in a moment.",
		}
	default:
		return check{Name: name, Status: statusWarn, Detail: fmt.Sprintf("cannot dial %s: %v", endpoint, err)}
	}
}

// checkTunAdapter reports the Wintun adapter. The tunnel creates it on
// connect and removes it on disconnect, so absence is normal.
func checkTunAdapter() check {
	const name = "tun-adapter"
	iface, err := net.InterfaceByName(netcfg.DefaultTunName)
	if err != nil {
		return skipped(name, netcfg.DefaultTunName+" adapter not present (the tunnel creates it on connect)")
	}
	return check{Name: name, Status: statusOK,
		Detail: fmt.Sprintf("%s adapter present (up %v)", iface.Name, iface.Flags&net.FlagUp != 0)}
}

// checkAdapterDNS verifies the tunnel adapter resolves through the in-tunnel
// resolver (SetInterfaceDnsSettings parity with Linux resolvectl).
func checkAdapterDNS() check {
	const name = "adapter-dns"
	servers, present, err := adapterDNSServers(netcfg.DefaultTunName)
	switch {
	case err != nil:
		return check{Name: name, Status: statusWarn, Detail: fmt.Sprintf("cannot enumerate adapters: %v", err)}
	case !present:
		return skipped(name, netcfg.DefaultTunName+" adapter not present (the tunnel creates it on connect)")
	case len(servers) == 1 && servers[0] == netcfg.DefaultTunAddr:
		return check{Name: name, Status: statusOK, Detail: fmt.Sprintf("%s DNS -> %s", netcfg.DefaultTunName, netcfg.DefaultTunAddr)}
	case len(servers) == 0:
		return check{
			Name:        name,
			Status:      statusWarn,
			Detail:      netcfg.DefaultTunName + " has no DNS servers configured; DNS may leak",
			Remediation: "Reconnect the tunnel; if it persists, run 'kumiho cleanup' and reconnect.",
		}
	default:
		return check{
			Name:        name,
			Status:      statusWarn,
			Detail:      fmt.Sprintf("%s DNS -> %s, want %s", netcfg.DefaultTunName, strings.Join(servers, ", "), netcfg.DefaultTunAddr),
			Remediation: "Reconnect the tunnel; if it persists, run 'kumiho cleanup' and reconnect.",
		}
	}
}

// adapterDNSServers finds an adapter by its interface alias and returns its
// configured DNS servers. present reports whether the adapter exists.
func adapterDNSServers(alias string) (servers []string, present bool, err error) {
	flags := uint32(windows.GAA_FLAG_SKIP_UNICAST | windows.GAA_FLAG_SKIP_ANYCAST | windows.GAA_FLAG_SKIP_MULTICAST)
	var size uint32
	err = windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, nil, &size)
	if err == nil {
		return nil, false, nil
	}
	if !errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) {
		return nil, false, err
	}
	buf := make([]byte, size)
	err = windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0,
		(*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])), &size)
	if err != nil {
		return nil, false, err
	}
	for aa := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])); aa != nil; aa = aa.Next {
		if !strings.EqualFold(windows.UTF16PtrToString(aa.FriendlyName), alias) {
			continue
		}
		for dns := aa.FirstDnsServerAddress; dns != nil; dns = dns.Next {
			if ip := dns.Address.IP(); ip != nil {
				servers = append(servers, ip.String())
			}
		}
		return servers, true, nil
	}
	return nil, false, nil
}

// checkPhysicalDefaultRoute reports whether a non-tunnel default route exists
// (the engine's bootstrap path).
func checkPhysicalDefaultRoute() check {
	const name = "physical-default-route"
	if auto.Current().HasDefaultRoute() {
		return check{Name: name, Status: statusOK, Detail: "a physical default route is present"}
	}
	return check{
		Name:        name,
		Status:      statusWarn,
		Detail:      "no physical IPv4 or IPv6 default route",
		Remediation: "Bring the host online first; kumiho needs a route for bootstrap DNS and the edge.",
	}
}

// checkElevation notes the shell's privilege level: repair commands need an
// Administrator, read-only checks work anywhere.
func checkElevation() check {
	const name = "elevation"
	if windows.GetCurrentProcessToken().IsElevated() {
		return check{Name: name, Status: statusOK, Detail: "running elevated; service and adapter repairs are available"}
	}
	return check{
		Name:        name,
		Status:      statusWarn,
		Detail:      "not elevated; service and adapter checks are read-only",
		Remediation: "Re-run from an elevated PowerShell for repair-level checks (install, cleanup).",
	}
}
