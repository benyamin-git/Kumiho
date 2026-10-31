package cli

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/ipc"
)

// startFakeCLI starts an in-process control server and returns a client for
// command helpers that already have their daemon connection.
func startFakeCLI(t *testing.T, handle func(*ipc.Conn, *ipc.Envelope) error) *ipc.Client {
	t.Helper()
	path := filepath.Join(t.TempDir(), "control.sock")
	srv, err := ipc.UnixTransport{}.Listen(path)
	if err != nil {
		if runtime.GOOS != "linux" {
			t.Skipf("unix sockets unavailable on this platform: %v", err)
		}
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = srv.Close()
	})
	go srv.Serve(ctx, func(_ context.Context, conn *ipc.Conn) error {
		for {
			env, err := conn.Read()
			if err != nil {
				return nil
			}
			if err := handle(conn, env); err != nil {
				return err
			}
		}
	})

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	client, err := ipc.UnixTransport{}.Dial(dialCtx, path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func replyFakeCLI(conn *ipc.Conn, env *ipc.Envelope, typ string, payload any) error {
	return conn.Reply(env.ID, typ, payload)
}

func replyFakeCLIError(conn *ipc.Conn, env *ipc.Envelope, err error) error {
	return conn.Reply(env.ID, ipc.TypeError, err)
}
