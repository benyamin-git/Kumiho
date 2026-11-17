package logging

import (
	"strings"
	"testing"
)

// TestRingLogStripsTerminalEscapes keeps remote text (error bodies, server
// names) from driving the terminal that renders the log stream.
func TestRingLogStripsTerminalEscapes(t *testing.T) {
	r := NewRing(Options{})
	r.Log(Info, "tui", "remote\x1b[2Jbody\x1b]52;c;Zm9v\x07line\x00end")

	got := r.Snapshot()[0].Msg
	for _, bad := range []string{"\x1b", "\x00", "\x07", "2J", "]52;"} {
		if strings.Contains(got, bad) {
			t.Fatalf("log message kept %q: %q", bad, got)
		}
	}
	if !strings.Contains(got, "remote") || !strings.Contains(got, "end") {
		t.Fatalf("log message lost its text: %q", got)
	}
}

func TestSanitizeLogLineFlattensNewlines(t *testing.T) {
	if got := SanitizeLogLine("a\nb\rc"); got != "a b c" {
		t.Fatalf("SanitizeLogLine = %q, want %q", got, "a b c")
	}
}

func TestSanitizeControlSequences(t *testing.T) {
	cases := map[string]string{
		"plain":                  "plain",
		"a\tb":                   "a\tb",
		"café":                   "café",
		"a\x1b[31mred\x1b[0m":    "ared",
		"a\x1b]0;title\x07b":     "ab",
		"a\x1b]52;c;Zm9v\x1b\\b": "ab",
		"a\x1bPdata\x1b\\b":      "ab",
		"a\x1b7b":                "ab",
		"a\nb":                   "a\nb",
		"a\u009bb":               "a b",
		"truncated\x1b[":         "truncated",
		"truncated\x1b]no end":   "truncated",
	}
	for in, want := range cases {
		if got := SanitizeControl(in); got != want {
			t.Errorf("SanitizeControl(%q) = %q, want %q", in, got, want)
		}
	}
}
