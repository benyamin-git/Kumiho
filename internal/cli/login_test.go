package cli

import (
	"bytes"
	"testing"
)

func TestPrintSignInDone(t *testing.T) {
	tests := []struct {
		name   string
		adding bool
		want   string
	}{
		{name: "login", adding: false, want: "Signed in as user@example.com.\n"},
		{name: "add account", adding: true, want: "Added account user@example.com.\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			printSignInDone(&out, tt.adding, "user@example.com")
			if got := out.String(); got != tt.want {
				t.Fatalf("printSignInDone() = %q, want %q", got, tt.want)
			}
		})
	}
}
