//go:build windows

package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// hostLogName is the shell's log file, %LocalAppData%\kumiho\gui.log (D33).
	hostLogName = "gui.log"
	// hostLogMaxBytes caps the log file. An existing file larger than this is
	// rotated to gui.log.1 (one previous file) when the shell starts; the
	// rotated file is replaced on every rotation, so the log directory never
	// grows beyond roughly twice this cap.
	hostLogMaxBytes = 512 << 10
)

// hostLog is the shell's append-only text log (D33). A log that could not be
// opened (or a nil log) silently discards entries: logging can never block or
// crash the shell. All methods are safe for concurrent use.
type hostLog struct {
	mu sync.Mutex
	f  *os.File
}

// openHostLog opens %LocalAppData%\kumiho\gui.log, rotating a file larger
// than hostLogMaxBytes to gui.log.1 first, in the daemon sink's style.
func openHostLog() *hostLog {
	l := &hostLog{}
	dir, err := os.UserCacheDir()
	if err != nil {
		return l
	}
	dir = filepath.Join(dir, "kumiho")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return l
	}
	path := filepath.Join(dir, hostLogName)
	if info, err := os.Stat(path); err == nil && info.Size() > hostLogMaxBytes {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return l
	}
	l.f = f
	return l
}

// logf appends one UTC-timestamped line.
func (l *hostLog) logf(format string, args ...any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return
	}
	fmt.Fprintf(l.f, "%s %s\n", time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

// close closes the log file; later logf calls are discarded.
func (l *hostLog) close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
}
