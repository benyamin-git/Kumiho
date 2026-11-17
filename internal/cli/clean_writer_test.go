package cli

import (
	"bytes"
	"testing"
)

// TestCleanWriterStripsEscapes pins the CLI output boundary: remote text must
// not be able to move the cursor, clear the screen or write the clipboard.
func TestCleanWriterStripsEscapes(t *testing.T) {
	var buf bytes.Buffer
	in := "before\x1b[2J\x1b]52;c;Zm9v\x07after\n"
	n, err := cleanWriter{&buf}.Write([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if n != len(in) {
		t.Fatalf("Write reported %d bytes, want %d", n, len(in))
	}
	if got, want := buf.String(), "beforeafter\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}
