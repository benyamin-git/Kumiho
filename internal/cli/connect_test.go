package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
)

func TestConnectFastestPayload(t *testing.T) {
	var payload api.ConnectPayload
	client := startFakeCLI(t, func(conn *ipc.Conn, env *ipc.Envelope) error {
		switch env.Type {
		case ipc.TypeConnect:
			if err := env.DecodePayload(&payload); err != nil {
				return err
			}
			return replyFakeCLI(conn, env, ipc.TypeResult, api.Result{OK: true})
		case ipc.TypeStatus:
			return replyFakeCLI(conn, env, ipc.TypeStatus, api.Status{State: "CONNECTED"})
		}
		return nil
	})
	var out, errOut bytes.Buffer
	code := connectWithClient(context.Background(), client, connectOptions{fastest: true, proxyOnly: true}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if !payload.Fastest || !payload.ProxyOnly || payload.LocationCode != "" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestConnectFastestDaemonErrorCopy(t *testing.T) {
	client := startFakeCLI(t, func(conn *ipc.Conn, env *ipc.Envelope) error {
		if env.Type != ipc.TypeConnect {
			return nil
		}
		return replyFakeCLIError(conn, env, &api.RemoteError{
			Code:    api.CodeBadRequest,
			Message: "Fastest needs a completed Ping all",
		})
	})
	var out, errOut bytes.Buffer
	code := connectWithClient(context.Background(), client, connectOptions{fastest: true}, &out, &errOut)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "Fastest needs a completed Ping all") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestConnectFastestConflictsWithToBeforeDialing(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runConnect([]string{"--fastest", "--to", "DE"}, &out, &errOut)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "choose --to or --fastest") {
		t.Fatalf("stderr = %q", errOut.String())
	}
	if strings.Contains(errOut.String(), "cannot reach the daemon") {
		t.Fatalf("conflict reached the daemon: %q", errOut.String())
	}
}
