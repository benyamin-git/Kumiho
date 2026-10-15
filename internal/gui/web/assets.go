package web

import (
	"embed"
	"io/fs"
)

// staticFiles holds the GUI assets (the T2.1 foundation: shell, tokens and
// primitives). Keep the tree self-contained (no network): the UI is
// hand-written HTML/CSS/JS served by this package.
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
