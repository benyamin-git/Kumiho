//go:build windows

package daemon

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/benyamin-git/kumiho/internal/platform"
	"github.com/benyamin-git/kumiho/internal/platform/windows"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// newSinkProvider returns the Windows provider with its runtime dir pointed
// at a fresh temp directory.
func newSinkProvider(t *testing.T) (platform.Provider, settings.Paths) {
	t.Helper()
	t.Setenv(settings.EnvRuntime, t.TempDir())
	plat := windows.New()
	return plat, plat.Paths()
}

// logPath returns the service log file under the runtime dir.
func logPath(paths settings.Paths) string {
	return filepath.Join(paths.Runtime, "logs", "kumiho.log")
}

func TestOpenLogSinkAppends(t *testing.T) {
	plat, paths := newSinkProvider(t)

	for _, line := range []string{"first\n", "second\n"} {
		w, closeSink, err := openLogSink(plat)
		if err != nil {
			t.Fatalf("openLogSink: %v", err)
		}
		if _, err := io.WriteString(w, line); err != nil {
			t.Fatalf("write: %v", err)
		}
		closeSink()
	}

	data, err := os.ReadFile(logPath(paths))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if got, want := string(data), "first\nsecond\n"; got != want {
		t.Fatalf("log contents = %q, want %q", got, want)
	}
}

func TestOpenLogSinkRotatesWhenOversized(t *testing.T) {
	for _, tc := range []struct {
		name       string
		size       int
		wantRotate bool
	}{
		{"above limit", logRotateLimit + 1, true},
		{"at limit", logRotateLimit, false},
		{"below limit", logRotateLimit - 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plat, paths := newSinkProvider(t)
			path := logPath(paths)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("mkdir logs: %v", err)
			}
			full := bytes.Repeat([]byte("x"), tc.size)
			if err := os.WriteFile(path, full, 0o644); err != nil {
				t.Fatalf("seed log: %v", err)
			}
			if err := os.WriteFile(path+".1", []byte("stale\n"), 0o644); err != nil {
				t.Fatalf("seed rotated log: %v", err)
			}

			w, closeSink, err := openLogSink(plat)
			if err != nil {
				t.Fatalf("openLogSink: %v", err)
			}
			if _, err := io.WriteString(w, "fresh\n"); err != nil {
				t.Fatalf("write: %v", err)
			}
			closeSink()

			rotated, err := os.ReadFile(path + ".1")
			if err != nil {
				t.Fatalf("read rotated log: %v", err)
			}
			if !tc.wantRotate {
				if got, want := string(rotated), "stale\n"; got != want {
					t.Fatalf("kumiho.log.1 = %q, want %q (no rotation expected)", got, want)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read log: %v", err)
				}
				want := append(append([]byte(nil), full...), "fresh\n"...)
				if !bytes.Equal(data, want) {
					t.Fatalf("log grew to %d bytes, want %d (append, no rotation)", len(data), len(want))
				}
				return
			}
			if !bytes.Equal(rotated, full) {
				t.Fatalf("kumiho.log.1 = %d bytes, want the previous %d-byte log", len(rotated), len(full))
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read log: %v", err)
			}
			if got, want := string(data), "fresh\n"; got != want {
				t.Fatalf("log contents after rotation = %q, want %q", got, want)
			}
		})
	}
}
