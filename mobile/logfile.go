package mobile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
)

const (
	// logMaxBytes is the rotation threshold for one log file.
	logMaxBytes = 1 << 20
	// logFiles is the total number of files kept, including the live one:
	// kumiho.log, kumiho.log.1 and kumiho.log.2.
	logFiles = 3
)

// rotatingWriter is the app's text log sink: it appends to logs/kumiho.log,
// rotates at 1 MB into .1/.2 and deletes the oldest file. It is safe for
// concurrent use.
//
// Write errors are swallowed after one warning on stderr: the caller is the
// logging ring, which writes to this sink while holding its own lock, so
// logging an error back through the ring would deadlock.
type rotatingWriter struct {
	mu   sync.Mutex
	path string
	max  int64
	keep int

	file *os.File
	size int64

	warned bool
}

func newRotatingWriter(path string) (*rotatingWriter, error) {
	w := &rotatingWriter{path: path, max: logMaxBytes, keep: logFiles}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.openLocked(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *rotatingWriter) openLocked() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.file = f
	w.size = fi.Size()
	return nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		if err := w.openLocked(); err != nil {
			w.warnLocked(err)
			return 0, err
		}
	}
	if w.max > 0 && w.size > 0 && w.size+int64(len(p)) > w.max {
		w.rotateLocked()
	}
	if w.file == nil {
		return 0, os.ErrClosed
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	if err != nil {
		w.warnLocked(err)
	}
	return n, err
}

// rotateLocked shifts kumiho.log.1 -> .2, deletes the oldest, moves the live
// file to .1 and opens a fresh live file.
func (w *rotatingWriter) rotateLocked() {
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			w.warnLocked(err)
		}
		w.file = nil
	}
	// Delete the oldest rotation first so the shift never renames onto a
	// stale .3.
	if err := os.Remove(w.path + "." + strconv.Itoa(w.keep-1)); err != nil && !errors.Is(err, os.ErrNotExist) {
		w.warnLocked(err)
	}
	for i := w.keep - 2; i >= 1; i-- {
		from := w.path + "." + strconv.Itoa(i)
		to := w.path + "." + strconv.Itoa(i+1)
		if err := os.Rename(from, to); err != nil && !errors.Is(err, os.ErrNotExist) {
			w.warnLocked(err)
		}
	}
	if err := os.Rename(w.path, w.path+".1"); err != nil && !errors.Is(err, os.ErrNotExist) {
		w.warnLocked(err)
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		w.warnLocked(err)
		return
	}
	w.file = f
	w.size = 0
}

// Reset truncates the live file and removes the rotations.
func (w *rotatingWriter) Reset() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			w.warnLocked(err)
		}
		w.file = nil
	}
	var firstErr error
	for i := 0; i < w.keep; i++ {
		name := w.path
		if i > 0 {
			name += "." + strconv.Itoa(i)
		}
		if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	if err := w.openLocked(); err != nil {
		return err
	}
	return firstErr
}

// Close closes the live file; later writes reopen it.
func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *rotatingWriter) warnLocked(err error) {
	if w.warned || err == nil {
		return
	}
	w.warned = true
	_, _ = fmt.Fprintf(os.Stderr, "kumiho mobile: log file %s: %v\n", w.path, err)
}

var _ io.Writer = (*rotatingWriter)(nil)
