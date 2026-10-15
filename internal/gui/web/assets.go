package web

import (
	"embed"
	"io/fs"
)

// staticFiles holds the GUI assets. T2.1 replaces the placeholder under
// static/ with the real UI; keep the tree self-contained (no network) until
// then.
//
//go:embed static
var staticFiles embed.FS

// StaticFS returns the embedded assets rooted at static/, ready to pass to
// New. The Windows shell and the dev harness use it; tests pass their own
// fs.FS.
func StaticFS() fs.FS {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		// Impossible: static/ is embedded above.
		panic("web: embedded static/: " + err.Error())
	}
	return sub
}
