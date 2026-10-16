//go:build windows

package gui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows service control behind the Diagnostics screen (T3.3, D17): the
// SCM state query backs State.Service (the service card), and the elevated
// sc.exe/kumiho calls back Start/Stop and Cleanup. The shell's host_windows.go
// intercepts the three actions, runs them through here and publishes the
// result through Controller.SetServiceState / SetDiagnosticsNotice.

// serviceName is the Windows service the Kumiho installer registers.
const serviceName = "kumiho"

// serviceSnapshot queries the SCM for the kumiho service's presence and
// state. A missing service is a normal snapshot (installed=false,
// "not installed"); any other failure returns a usable "unknown" snapshot
// alongside the error, so callers always have a value to publish.
func serviceSnapshot() (ServiceState, error) {
	unknown := ServiceState{State: "unknown"}
	mgr, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return unknown, fmt.Errorf("open the service manager: %w", err)
	}
	defer windows.CloseServiceHandle(mgr)

	h, err := windows.OpenService(mgr, windows.StringToUTF16Ptr(serviceName), windows.SERVICE_QUERY_STATUS)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return ServiceState{State: "not installed"}, nil
		}
		return unknown, fmt.Errorf("open the %s service: %w", serviceName, err)
	}
	defer windows.CloseServiceHandle(h)

	var status windows.SERVICE_STATUS_PROCESS
	var needed uint32
	if err := windows.QueryServiceStatusEx(h, windows.SC_STATUS_PROCESS_INFO,
		(*byte)(unsafe.Pointer(&status)), uint32(unsafe.Sizeof(status)), &needed); err != nil {
		return ServiceState{Installed: true, State: "unknown"}, fmt.Errorf("query the %s service: %w", serviceName, err)
	}
	return ServiceState{Installed: true, State: serviceStateWord(status.CurrentState)}, nil
}

// serviceStateWord maps an SCM state constant to the card's word (the same
// words the doctor's service check prints).
func serviceStateWord(state uint32) string {
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
		return fmt.Sprintf("state %d", state)
	}
}

// serviceCommand runs `sc.exe <verb> kumiho` elevated (UAC) and returns the
// command's exit code; a declined prompt or a failed launch is an error.
func serviceCommand(verb string) (int, error) {
	return runElevated(scPath(), verb+" "+serviceName, "")
}

// scPath locates the system sc.exe; a PATH lookup is the fallback when
// SystemRoot is unset (ShellExecuteEx searches PATH for a bare name).
func scPath() string {
	if root := os.Getenv("SystemRoot"); root != "" {
		p := filepath.Join(root, "System32", "sc.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "sc.exe"
}

// cleanupCommand runs `kumiho cleanup` elevated (D16), executing from the
// CLI's directory. The cleanup subcommand needs no daemon contact and needs
// Administrator rights, exactly as the CLI documents.
func cleanupCommand() (int, error) {
	exe, err := kumihoExe()
	if err != nil {
		return 0, err
	}
	return runElevated(exe, "cleanup", filepath.Dir(exe))
}

// kumihoExe locates the kumiho CLI: beside the running GUI first (the
// release zip ships kumiho.exe and kumiho-gui.exe together), then PATH.
func kumihoExe() (string, error) {
	if exe, err := os.Executable(); err == nil {
		if p, ok := kumihoExeBeside(filepath.Dir(exe)); ok {
			return p, nil
		}
	}
	if p, err := exec.LookPath("kumiho.exe"); err == nil {
		return p, nil
	}
	return "", errors.New("kumiho.exe was not found next to the Kumiho app")
}

// kumihoExeBeside reports kumiho.exe in the given directory.
func kumihoExeBeside(dir string) (string, bool) {
	p := filepath.Join(dir, "kumiho.exe")
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
		return p, true
	}
	return "", false
}

// serviceOutcomeNotice words one elevated start/stop outcome for the
// Diagnostics notice: the success words, the declined-consent wording, a
// non-zero command exit, or the launch error. code and err come from
// serviceCommand.
func serviceOutcomeNotice(verb string, code int, err error) (string, string) {
	switch {
	case errors.Is(err, errElevationCancelled):
		return "could not " + verb + " the service: administrator approval was cancelled", toneBad
	case err != nil:
		return "could not " + verb + " the service: " + err.Error(), toneBad
	case code != 0:
		return fmt.Sprintf("could not %s the service: the command exited with code %d", verb, code), toneBad
	case verb == "start":
		return "Service started", toneGood
	default:
		return "Service stopped", toneGood
	}
}

// cleanupOutcomeNotice words one elevated cleanup outcome, mirroring
// serviceOutcomeNotice; code and err come from cleanupCommand.
func cleanupOutcomeNotice(code int, err error) (string, string) {
	switch {
	case errors.Is(err, errElevationCancelled):
		return "could not clean up: administrator approval was cancelled", toneBad
	case err != nil:
		return "could not clean up: " + err.Error(), toneBad
	case code != 0:
		return fmt.Sprintf("could not clean up: the command exited with code %d", code), toneBad
	default:
		return "Cleanup finished", toneGood
	}
}
