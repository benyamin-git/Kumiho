package serverlist

import "testing"

func TestFlagEmoji(t *testing.T) {
	tests := []struct {
		name string
		code string
		want string
	}{
		{name: "uppercase", code: "DE", want: "🇩🇪"},
		{name: "lowercase", code: "de", want: "🇩🇪"},
		{name: "surrounding spaces", code: " us ", want: "🇺🇸"},
		{name: "italy", code: "IT", want: "🇮🇹"},
		{name: "recommended uppercase", code: "REC", want: ""},
		{name: "recommended lowercase", code: "rec", want: ""},
		{name: "recommended padded", code: " Rec ", want: ""},
		{name: "blank", code: "", want: ""},
		{name: "too long", code: "USA", want: ""},
		{name: "too short", code: "D", want: ""},
		{name: "digit", code: "D1", want: ""},
		{name: "inner space", code: "D E", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FlagEmoji(tt.code); got != tt.want {
				t.Fatalf("FlagEmoji(%q) = %q, want %q", tt.code, got, tt.want)
			}
		})
	}
}
