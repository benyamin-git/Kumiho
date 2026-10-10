//go:build windows

package ipc

import (
	"errors"
	"net"

	"golang.org/x/sys/windows"
)

// peerUID is not available on Windows; identity is the named-pipe client
// process, checked by peerIsAdmin instead.
func peerUID(net.Conn) (uint32, error) {
	return 0, errors.New("peer credentials are not supported on this platform")
}

// peerIsAdmin reports whether the process on the other end of the pipe is in
// the local Administrators group. GetNamedPipeClientProcessId identifies the
// client process, OpenProcess and OpenProcessToken obtain its token, and
// CheckTokenMembership (windows.Token.IsMember) tests the well-known
// Administrators SID. Every failure fails closed.
func peerIsAdmin(nc net.Conn) bool {
	h, ok := pipeHandle(nc)
	if !ok {
		return false
	}
	var pid uint32
	if err := windows.GetNamedPipeClientProcessId(h, &pid); err != nil {
		return false
	}
	proc, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(proc) }()

	var token windows.Token
	if err := windows.OpenProcessToken(proc, windows.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer func() { _ = token.Close() }()

	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return false
	}
	member, err := token.IsMember(admins)
	return err == nil && member
}

// pipeHandle extracts the Win32 handle from a connection created by go-winio.
// Connections from other transports (and net.Pipe test doubles) have no Fd
// method.
func pipeHandle(nc net.Conn) (windows.Handle, bool) {
	f, ok := nc.(interface{ Fd() uintptr })
	if !ok {
		return 0, false
	}
	return windows.Handle(f.Fd()), true
}
