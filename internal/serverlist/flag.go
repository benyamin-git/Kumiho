package serverlist

import "strings"

// FlagEmoji returns the ISO 3166-1 alpha-2 flag emoji for code, or "" when the
// code is RecommendedCode, blank, or not exactly two ASCII letters after
// trimming spaces.
func FlagEmoji(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == RecommendedCode || len(code) != 2 {
		return ""
	}
	for i := 0; i < 2; i++ {
		if code[i] < 'A' || code[i] > 'Z' {
			return ""
		}
	}
	const base = 0x1F1E6
	return string([]rune{
		rune(base + int(code[0]-'A')),
		rune(base + int(code[1]-'A')),
	})
}
