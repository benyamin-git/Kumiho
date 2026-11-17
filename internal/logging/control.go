package logging

import (
	"strings"
	"unicode/utf8"
)

// SanitizeControl removes terminal control sequences from untrusted text
// (remote error bodies, server names, account labels). ESC-introduced
// sequences are dropped, other C0/C1 controls and DEL become spaces, and
// newline, tab and printable text are kept.
func SanitizeControl(s string) string {
	if !hasControl(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			i = skipEscape(s, i)
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '\t' || r == '\n':
			b.WriteString(s[i : i+size])
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			b.WriteByte(' ')
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

func hasControl(s string) bool {
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			return true
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if (r < 0x20 && r != '\t' && r != '\n') || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return true
		}
		i += size
	}
	return false
}

// skipEscape returns the index just past the escape sequence that starts at
// the ESC at i.
func skipEscape(s string, i int) int {
	i++ // ESC
	if i >= len(s) {
		return i
	}
	switch s[i] {
	case '[': // CSI: parameters and intermediates, then a final byte
		i++
		for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
			i++
		}
		if i < len(s) {
			i++
		}
		return i
	case ']', 'P', 'X', '^', '_': // OSC/DCS/SOS/PM/APC end at BEL or ST
		i++
		for i < len(s) {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
			i++
		}
		return i
	default: // two-byte escape (ESC c, ESC 7, ...)
		return i + 1
	}
}

// SanitizeLogLine is SanitizeControl for one log entry: newlines become spaces
// so remote text cannot forge extra lines in a log view.
func SanitizeLogLine(s string) string {
	s = SanitizeControl(s)
	if !strings.Contains(s, "\n") {
		return s
	}
	return strings.ReplaceAll(s, "\n", " ")
}
