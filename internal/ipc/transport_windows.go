//go:build windows

package ipc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// WindowsTransport carries the control protocol over a named pipe. The wire
// format is unchanged: byte mode, newline-delimited JSON protocol v1.
type WindowsTransport struct{}

const kumihoGroup = "kumiho"

// controlPipeSDDLBase grants full control to LocalSystem and the built-in
// Administrators group. The kumiho group entry is appended when it resolves,
// followed by the pipe-users allowlist (D14).
const controlPipeSDDLBase = "D:P(A;;GA;;;SY)(A;;GA;;;BA)"

// pipeUsersFileName is the per-machine allowlist of extra principals granted
// control pipe access, stored beside the other machine-wide state files:
// %ProgramData%\kumiho\pipe-users (D14). The directory is admin-only
// (install.ps1 strips inheritance and grants only SYSTEM and Administrators),
// so the file is admin-controlled input.
const pipeUsersFileName = "pipe-users"

// maxPipeUsersSize caps how much of pipe-users is trusted. A canonical SID is
// at most 184 characters, so 64 KiB holds hundreds of entries; a larger file
// is treated like any other unreadable input and contributes no SIDs (fail
// closed) rather than being partially applied.
const maxPipeUsersSize = 64 << 10

// pipeSDDL builds the control pipe DACL from trusted, canonical SID strings
// only; an empty groupSID omits the kumiho group entry (the fallback used
// before the install script creates it). Entry order is fixed: SYSTEM,
// Administrators, the kumiho group, then the allowlist SIDs in file order.
// Every entry is an allow full-control ACE, so the allowlist cannot deny or
// widen beyond the listed principals.
func pipeSDDL(groupSID string, allowlist ...string) string {
	sddl := controlPipeSDDLBase
	if groupSID != "" {
		sddl += "(A;;GA;;;" + groupSID + ")"
	}
	for _, sid := range allowlist {
		sddl += "(A;;GA;;;" + sid + ")"
	}
	return sddl
}

// pipeUsersPath returns %ProgramData%\kumiho\pipe-users. An unset ProgramData
// yields the empty path and the allowlist is skipped: resolving a relative
// path would read whatever working directory the daemon was started from,
// which a non-admin could plant.
func pipeUsersPath() string {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		return ""
	}
	return filepath.Join(programData, "kumiho", pipeUsersFileName)
}

// isStandardSIDString reports whether s uses the standard S-R-I-S-S... SID
// notation. ConvertStringSidToSid also accepts SDDL string constants such as
// "BA" (Administrators) or "WD" (Everyone); those are rejected here so the
// file can only name principals as explicit SIDs (hardening: an alias like
// "WD" would otherwise grant Everyone through a shorthand an administrator
// might not expect).
func isStandardSIDString(s string) bool {
	return len(s) >= 3 && (s[0] == 'S' || s[0] == 's') && s[1] == '-'
}

// parsePipeUsers parses pipe-users content: one SID per line, with a UTF-8
// BOM tolerated on the first line, blank lines skipped, and malformed lines
// skipped (reported via the malformed count) without discarding the rest.
// Each accepted line is canonicalized through Windows itself
// (windows.StringToSid parses, SID.String re-renders in canonical form) and
// deduplicated; the raw line text is never returned, so no unvalidated input
// can reach the DACL.
func parsePipeUsers(data []byte) (sids []string, malformed int) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	seen := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !isStandardSIDString(line) {
			malformed++
			continue
		}
		sid, err := windows.StringToSid(line)
		if err != nil {
			malformed++
			continue
		}
		canonical := sid.String()
		if canonical == "" {
			malformed++
			continue
		}
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		sids = append(sids, canonical)
	}
	return sids, malformed
}

// readPipeUsers reads the allowlist at path and returns its canonical SIDs.
// A missing file is the normal pre-install state: no SIDs, no error. Any
// other failure - permissions, I/O, a lock, an oversized file, an unset
// %ProgramData% - fails closed by returning no SIDs and an error, so the
// caller keeps the previous group-only ACL and never applies a partial set.
func readPipeUsers(path string) ([]string, error) {
	if path == "" {
		return nil, errors.New("%ProgramData% is not set")
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, maxPipeUsersSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxPipeUsersSize {
		return nil, fmt.Errorf("%s is larger than the %d-byte limit", path, maxPipeUsersSize)
	}
	sids, malformed := parsePipeUsers(data)
	if malformed > 0 {
		log.Printf("kumiho ipc: %s: skipped %d malformed line(s)", path, malformed)
	}
	return sids, nil
}

// controlPipeSDDLFor resolves the local kumiho group and the pipe-users
// allowlist and returns the pipe DACL. A fresh box has no group yet: the pipe
// then lacks that entry but the allowlist is still applied, and the daemon
// logs the downgrade (standard logger, i.e. the daemon's stderr). An
// unreadable allowlist likewise logs and contributes nothing; access is never
// widened on any failure path.
func controlPipeSDDLFor(allowlistPath string) string {
	groupSID, err := winio.LookupSidByName(kumihoGroup)
	if err != nil {
		log.Printf("kumiho ipc: local group %q not found; control pipe ACL falls back to SYSTEM, Administrators and any %s entries: %v", kumihoGroup, pipeUsersFileName, err)
		groupSID = ""
	}
	users, err := readPipeUsers(allowlistPath)
	if err != nil {
		log.Printf("kumiho ipc: control pipe allowlist %q unavailable; ACL falls back to SYSTEM, Administrators and the %q group: %v", allowlistPath, kumihoGroup, err)
		users = nil
	}
	return pipeSDDL(groupSID, users...)
}

// controlPipeSDDL returns the pipe DACL for this machine's allowlist path.
func controlPipeSDDL() string {
	return controlPipeSDDLFor(pipeUsersPath())
}

// Listen creates the control pipe and refuses to steal one that a live daemon
// already serves.
func (WindowsTransport) Listen(endpoint string) (*Server, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if c, err := (WindowsTransport{}).Dial(ctx, endpoint); err == nil {
		_ = c.Close()
		return nil, fmt.Errorf("%s: another kumiho daemon is already listening", endpoint)
	}
	ln, err := winio.ListenPipe(endpoint, &winio.PipeConfig{SecurityDescriptor: controlPipeSDDL()})
	if err != nil {
		return nil, err
	}
	return &Server{path: endpoint, ln: ln, peerUID: WindowsTransport{}.PeerUID, peerAdmin: peerIsAdmin}, nil
}

// Dial connects to the daemon control pipe.
func (WindowsTransport) Dial(ctx context.Context, endpoint string) (*Client, error) {
	conn, err := winio.DialPipeContext(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	return newClient(conn), nil
}

// PeerUID reports false: Windows carries no Unix-style peer credentials.
func (WindowsTransport) PeerUID(net.Conn) (uint32, bool) { return 0, false }

var _ Transport = WindowsTransport{}
