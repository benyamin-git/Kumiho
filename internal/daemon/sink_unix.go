//go:build !windows

package daemon

import (
	"io"
	"os"

	"github.com/benyamin-git/kumiho/internal/platform"
)

// openLogSink returns the daemon's text log sink for non-Windows platforms:
// stderr, which systemd captures into the journal. There is nothing to close.
func openLogSink(_ platform.Provider) (io.Writer, func(), error) {
	return os.Stderr, func() {}, nil
}
