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
		"locations    arrows/hjkl move • enter expand/select • p ping • / filter • r refresh • esc back",
		"settings     arrows move • enter change • d reset to default • r refresh • esc back",
		"logs         f follow • v level • / search • c clear • e export • esc back",
		"account      r refresh • x sign out • esc back",
		"login        enter submits each step (password masked); esc back",
	}
	for _, l := range lines {
		fmt.Fprintln(b, dimStyle.Render(l))
	}
	fmt.Fprintln(b)
	fmt.Fprintln(b, dimStyle.Render("press any key to close (q quits)"))
}
