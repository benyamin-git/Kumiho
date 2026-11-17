package tui

import (
	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/logging"
)

// safeText strips terminal control sequences from daemon- or remote-supplied
// text before it reaches the screen.
func safeText(s string) string { return logging.SanitizeLogLine(s) }

// sanitizeStatus cleans the remote-derived text in a status snapshot.
func sanitizeStatus(st api.Status) api.Status {
	st.State = safeText(st.State)
	st.Email = safeText(st.Email)
	st.Location = safeText(st.Location)
	st.Server = safeText(st.Server)
	st.ExitIP = safeText(st.ExitIP)
	st.ExitCountry = safeText(st.ExitCountry)
	st.DNS = safeText(st.DNS)
	st.IPv6 = safeText(st.IPv6)
	return st
}
