//go:build windows

package daemon

import (
	"io"
	"os"
	"path/filepath"

	"github.com/benyamin-git/kumiho/internal/platform"
)

// logRotateLimit is the size above which the log file is rotated at open.
const logRotateLimit = 5 << 20 // 5 MiB

// openLogSink opens the Windows service log at
// %ProgramData%\kumiho\logs\kumiho.log, rotating an existing file larger
// than logRotateLimit to kumiho.log.1 first. The returned func closes it.
func openLogSink(plat platform.Provider) (io.Writer, func(), error) {
	dir := filepath.Join(plat.Paths().Runtime, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	path := filepath.Join(dir, "kumiho.log")
	if info, err := os.Stat(path); err == nil && info.Size() > logRotateLimit {
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, nil, err
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { _ = f.Close() }, nil
}
