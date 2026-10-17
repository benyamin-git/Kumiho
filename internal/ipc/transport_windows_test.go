//go:build windows

package ipc

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

const (
	testSIDAlice = "S-1-5-21-1000-2000-3000-1001"
	testSIDBob   = "S-1-5-21-1000-2000-3000-1002"
)

// writePipeUsersFile writes content to a pipe-users file in a fresh temp dir
// and returns its path. Tests never touch the real ProgramData directory.
func writePipeUsersFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), pipeUsersFileName)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParsePipeUsers(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		want      []string
		malformed int
	}{
		{name: "empty", content: "", want: nil},
		{name: "bom tolerated on first line", content: "\uFEFF" + testSIDAlice + "\n", want: []string{testSIDAlice}},
		{name: "blank lines and crlf skipped", content: "\r\n" + testSIDAlice + "\r\n\r\n" + testSIDBob + "\n", want: []string{testSIDAlice, testSIDBob}},
		{name: "surrounding whitespace trimmed", content: "  " + testSIDAlice + " \t\n\t" + testSIDBob + "\t\n", want: []string{testSIDAlice, testSIDBob}},
		{
			name:      "malformed lines skipped but the rest kept",
			content:   testSIDAlice + "\nnot-a-sid\n(A;;GA;;;SY)\nS-1-5-21-oops\n" + testSIDBob + "\n",
			want:      []string{testSIDAlice, testSIDBob},
			malformed: 3,
		},
		{
			name:    "duplicates collapsed",
			content: testSIDAlice + "\n" + testSIDAlice + "\n  " + testSIDAlice + "  \n" + testSIDBob + "\n",
			want:    []string{testSIDAlice, testSIDBob},
		},
		{
			name:      "bom tolerated only on the first line",
			content:   testSIDAlice + "\n\uFEFF" + testSIDBob + "\n",
			want:      []string{testSIDAlice},
			malformed: 1,
		},
		{
			name:      "sddl injection is rejected",
			content:   testSIDAlice + ")(A;;GA;;;WD\n",
			malformed: 1,
		},
		{
			// ConvertStringSidToSid accepts these SDDL aliases; the parser
			// must not, or "WD" would quietly grant Everyone.
			name:      "sddl aliases rejected",
			content:   "BA\nWD\nSY\nAU\n",
			malformed: 4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, malformed := parsePipeUsers([]byte(tt.content))
			if !slices.Equal(got, tt.want) {
				t.Fatalf("parsePipeUsers(%q) = %v, want %v", tt.content, got, tt.want)
			}
			if malformed != tt.malformed {
				t.Fatalf("parsePipeUsers(%q) malformed = %d, want %d", tt.content, malformed, tt.malformed)
			}
			// Every accepted entry must be in Windows' canonical spelling:
			// re-parsing and re-rendering must be idempotent.
			for _, sid := range got {
				parsed, err := windows.StringToSid(sid)
				if err != nil || parsed.String() != sid {
					t.Fatalf("accepted SID %q is not canonical", sid)
				}
			}
		})
	}
}

// Windows decides whether non-canonical standard spellings such as a
// lowercase "s-" prefix parse. Either outcome is safe; the parser must never
// echo the raw line.
func TestParsePipeUsersNeverEchoesInput(t *testing.T) {
	got, _ := parsePipeUsers([]byte("s-1-5-18\n"))
	switch {
	case len(got) == 0:
		// Rejected by this Windows build: fails closed.
	case len(got) == 1 && got[0] == "S-1-5-18":
		// Accepted and canonicalized.
	default:
		t.Fatalf("parsePipeUsers(lowercase SID) = %v, want none or canonical S-1-5-18", got)
	}
}

func TestPipeSDDLAppendsAllowlistAfterGroup(t *testing.T) {
	const group = "S-1-5-21-1-2-3-1001"

	got := pipeSDDL(group, testSIDAlice, testSIDBob)
	want := controlPipeSDDLBase +
		"(A;;GA;;;" + group + ")" +
		"(A;;GA;;;" + testSIDAlice + ")" +
		"(A;;GA;;;" + testSIDBob + ")"
	if got != want {
		t.Fatalf("pipeSDDL(group, users) = %q, want %q", got, want)
	}
	if _, err := windows.SecurityDescriptorFromString(got); err != nil {
		t.Fatalf("built SDDL is not a valid descriptor: %v", err)
	}

	// Without a group entry the allowlist is still appended, after the base.
	got = pipeSDDL("", testSIDAlice)
	want = controlPipeSDDLBase + "(A;;GA;;;" + testSIDAlice + ")"
	if got != want {
		t.Fatalf("pipeSDDL(\"\", user) = %q, want %q", got, want)
	}
	if _, err := windows.SecurityDescriptorFromString(got); err != nil {
		t.Fatalf("built SDDL is not a valid descriptor: %v", err)
	}
}

func TestControlPipeSDDLForReadsAllowlist(t *testing.T) {
	path := writePipeUsersFile(t, testSIDAlice+"\nnot-a-sid\n"+testSIDBob+"\n")
	got := controlPipeSDDLFor(path)

	// The kumiho group may or may not exist on this machine, so the fixed
	// prefix is the base; at most one group ACE may sit between it and the
	// allowlist.
	if !strings.HasPrefix(got, controlPipeSDDLBase) {
		t.Fatalf("SDDL %q does not start with %q", got, controlPipeSDDLBase)
	}
	aliceACE := "(A;;GA;;;" + testSIDAlice + ")"
	bobACE := "(A;;GA;;;" + testSIDBob + ")"
	ia := strings.Index(got, aliceACE)
	ib := strings.Index(got, bobACE)
	if ia < len(controlPipeSDDLBase) || ib != ia+len(aliceACE) {
		t.Fatalf("allowlist entries missing or out of order in %q", got)
	}
	if mid := got[len(controlPipeSDDLBase):ia]; strings.Count(mid, "(A;;GA;;;") > 1 {
		t.Fatalf("more than the group entry precedes the allowlist in %q", got)
	}

	// Every ACE is an allow full-control ACE: the allowlist cannot add
	// denies, other rights, or other trustees.
	if n, ga := strings.Count(got, "(A;;"), strings.Count(got, "(A;;GA;;;"); n != ga {
		t.Fatalf("SDDL %q has %d ACEs but only %d allow-GA ACEs", got, n, ga)
	}
	if strings.Contains(got, "not-a-sid") {
		t.Fatalf("malformed input leaked into the SDDL: %q", got)
	}
	if _, err := windows.SecurityDescriptorFromString(got); err != nil {
		t.Fatalf("built SDDL is not a valid descriptor: %v", err)
	}
}

func TestReadPipeUsersMissingFileIsNotAnError(t *testing.T) {
	got, err := readPipeUsers(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("readPipeUsers(missing) error = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("readPipeUsers(missing) = %v, want no SIDs", got)
	}
}

func TestReadPipeUsersUnreadableFailsClosed(t *testing.T) {
	path := writePipeUsersFile(t, testSIDAlice+"\n")

	// Hold the file open without sharing so any second open fails. This
	// simulates an unreadable file (permissions, I/O error, a lock) without
	// touching real ACLs.
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("hold %s open: %v", path, err)
	}
	defer func() { _ = windows.CloseHandle(h) }()

	got, err := readPipeUsers(path)
	if err == nil {
		t.Fatal("readPipeUsers(unreadable) error = nil, want failure (fail closed)")
	}
	if len(got) != 0 {
		t.Fatalf("readPipeUsers(unreadable) = %v, want no SIDs", got)
	}
}

func TestReadPipeUsersEmptyPathFailsClosed(t *testing.T) {
	got, err := readPipeUsers("")
	if err == nil {
		t.Fatal("readPipeUsers(\"\") error = nil, want failure (fail closed)")
	}
	if len(got) != 0 {
		t.Fatalf("readPipeUsers(\"\") = %v, want no SIDs", got)
	}
}

func TestReadPipeUsersSizeCap(t *testing.T) {
	line := testSIDAlice + "\n"
	oversized := strings.Repeat(line, maxPipeUsersSize/len(line)+2)
	if len(oversized) <= maxPipeUsersSize {
		t.Fatalf("test setup: content %d bytes does not exceed the cap %d", len(oversized), maxPipeUsersSize)
	}
	got, err := readPipeUsers(writePipeUsersFile(t, oversized))
	if err == nil {
		t.Fatal("readPipeUsers(oversized) error = nil, want failure (fail closed)")
	}
	if len(got) != 0 {
		t.Fatalf("readPipeUsers(oversized) = %v, want no SIDs", got)
	}
}

func TestReadPipeUsersAtCapBoundary(t *testing.T) {
	content := testSIDAlice + "\n" + strings.Repeat("\n", maxPipeUsersSize-len(testSIDAlice)-1)
	if len(content) != maxPipeUsersSize {
		t.Fatalf("test setup: content is %d bytes, want exactly %d", len(content), maxPipeUsersSize)
	}
	got, err := readPipeUsers(writePipeUsersFile(t, content))
	if err != nil {
		t.Fatalf("readPipeUsers(at cap) error = %v, want nil", err)
	}
	if want := []string{testSIDAlice}; !slices.Equal(got, want) {
		t.Fatalf("readPipeUsers(at cap) = %v, want %v", got, want)
	}
}

func TestPipeUsersPath(t *testing.T) {
	t.Setenv("ProgramData", `C:\ProgramData`)
	if got, want := pipeUsersPath(), filepath.Join(`C:\ProgramData`, "kumiho", pipeUsersFileName); got != want {
		t.Fatalf("pipeUsersPath() = %q, want %q", got, want)
	}
	t.Setenv("ProgramData", "")
	if got := pipeUsersPath(); got != "" {
		t.Fatalf("pipeUsersPath() with unset ProgramData = %q, want \"\" (no relative fallback)", got)
	}
}
