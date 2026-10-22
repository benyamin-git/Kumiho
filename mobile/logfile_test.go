package mobile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// requirePerm checks the Unix permission bits. Windows only emulates the
// read-only bit, so os.Stat reports 0666/0777 regardless of Chmod.
func requirePerm(t *testing.T, fi os.FileInfo, want os.FileMode, name string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	if got := fi.Mode().Perm(); got != want {
		t.Errorf("%s mode = %o, want %o", name, got, want)
	}
}

func listLogFiles(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "kumiho.log*"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	return matches
}

func TestRotatingWriterRotatesAtOneMegabyte(t *testing.T) {
	dir := t.TempDir()
	w, err := newRotatingWriter(filepath.Join(dir, "kumiho.log"))
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	line := []byte(strings.Repeat("x", 99) + "\n") // 100 bytes
	for i := 0; i < 25000; i++ {                   // 2.5 MB
		if _, err := w.Write(line); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	files := listLogFiles(t, dir)
	if len(files) != 3 {
		t.Fatalf("got %d log files %v, want exactly 3", len(files), files)
	}
	if _, err := os.Stat(filepath.Join(dir, "kumiho.log.3")); !os.IsNotExist(err) {
		t.Fatalf("kumiho.log.3 exists: %v", err)
	}
	var total int64
	for _, name := range files {
		fi, err := os.Stat(name)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		requirePerm(t, fi, 0o600, name)
		if fi.Size() > 1<<20 {
			t.Errorf("%s is %d bytes, over the 1 MB cap", name, fi.Size())
		}
		total += fi.Size()
	}
	if total != int64(len(line))*25000 {
		t.Errorf("kept %d bytes across rotations, want %d", total, len(line)*25000)
	}
}

func TestRotatingWriterResetRemovesRotations(t *testing.T) {
	dir := t.TempDir()
	w, err := newRotatingWriter(filepath.Join(dir, "kumiho.log"))
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	defer w.Close()

	line := []byte(strings.Repeat("y", 99) + "\n")
	for i := 0; i < 15000; i++ { // 1.5 MB: one rotation
		if _, err := w.Write(line); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if len(listLogFiles(t, dir)) < 2 {
		t.Fatalf("expected a rotation before reset, got %v", listLogFiles(t, dir))
	}

	if err := w.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	files := listLogFiles(t, dir)
	if len(files) != 1 || filepath.Base(files[0]) != "kumiho.log" {
		t.Fatalf("after Reset got %v, want only kumiho.log", files)
	}
	fi, err := os.Stat(files[0])
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Size() != 0 {
		t.Fatalf("after Reset size = %d, want 0", fi.Size())
	}
	// A write after Reset lands in the fresh file.
	if _, err := w.Write([]byte("after reset\n")); err != nil {
		t.Fatalf("write after Reset: %v", err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "after reset\n" {
		t.Fatalf("file content = %q", data)
	}
}

func TestRotatingWriterConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	w, err := newRotatingWriter(filepath.Join(dir, "kumiho.log"))
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	defer w.Close()

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				if _, err := w.Write([]byte(strings.Repeat("z", 99) + "\n")); err != nil {
					t.Errorf("write: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if n := len(listLogFiles(t, dir)); n != 2 {
		t.Fatalf("got %d log files, want 2", n)
	}
}
