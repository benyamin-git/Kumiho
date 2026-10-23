package tui

import (
	"fmt"
	"strings"
)

// renderHelp draws the "?" overlay (PLAN.md §6).
func (m *Model) renderHelp(b *strings.Builder) {
	fmt.Fprintf(b, "%s\n\n", titleStyle.Render("Keys"))
	lines := []string{
		"global       ? help • q / ctrl+c quit",
		"dashboard    space connect/disconnect • l login • o locations • s settings • a account • g logs • r refresh",
		"locations    arrows/hjkl move • enter expand/select • p ping shown • P ping all",
		"             x expand/collapse all • / filter • r refresh • esc back",
		"settings     arrows move • enter change • d reset to default • r refresh • esc back",
		"logs         f follow • v level • / search • c clear • e export • esc back",
		"account      ↑/↓ select • u use • d remove • a add browser • p add password • R sign in again",
		"             n rename • t rotation • +/- threshold • r refresh • x sign out • esc back",
		"login        b browser • p password; enter submits each password step; esc back",
	}
	for _, l := range lines {
		fmt.Fprintln(b, dimStyle.Render(l))
	}
	fmt.Fprintln(b)
	fmt.Fprintln(b, dimStyle.Render("press any key to close (q quits)"))
}
