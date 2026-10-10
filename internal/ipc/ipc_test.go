package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	env, err := NewEnvelope("42", TypeSettingsSet, api.SettingsSetPayload{Key: "kill_switch", Value: "off"})
	if err != nil {
		t.Fatal(err)
	}
	if env.V != ProtocolVersion {
		t.Fatalf("version = %d, want %d", env.V, ProtocolVersion)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	var got Envelope
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	var p api.SettingsSetPayload
	if err := got.DecodePayload(&p); err != nil {
		t.Fatal(err)
	}
	if p.Key != "kill_switch" || p.Value != "off" {
		t.Fatalf("payload = %+v", p)
	}

	var empty api.SettingsSetPayload
	if err := (&Envelope{Type: "x"}).DecodePayload(&empty); err != nil {
		t.Fatal(err)
	}
	if empty.Key != "" {
		t.Fatal("empty payload should not modify target")
	}
}

// testServer starts a socket server for tests; unsupported platforms skip.
func testServer(t *testing.T, handler func(context.Context, *Conn) error) (*Server, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "control.sock")
	srv, err := UnixTransport{}.Listen(path)
	if err != nil {
		if runtime.GOOS != "linux" {
			t.Skipf("unix sockets unavailable on this platform: %v", err)
		}
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		srv.Close()
	})
	go srv.Serve(ctx, handler)
	return srv, path
}

func TestClientServerExchange(t *testing.T) {
	_, path := testServer(t, func(ctx context.Context, c *Conn) error {
		for {
			env, err := c.Read()
			if err != nil {
				return nil
			}
			switch env.Type {
			case "ping":
				_ = c.Reply(env.ID, "pong", api.Result{OK: true, Note: "hi"})
				_ = c.Event(TypeStatus, api.Status{State: "IDLE", Authenticated: true})
			case "boom":
				_ = c.Reply(env.ID, TypeError, api.RemoteError{Code: api.CodeBadRequest, Message: "nope"})
			}
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cl, err := UnixTransport{}.Dial(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()

	resp, err := cl.Call(ctx, "ping", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Type != "pong" {
		t.Fatalf("response type = %q", resp.Type)
	}
	var r api.Result
	if err := resp.DecodePayload(&r); err != nil {
		t.Fatal(err)
	}
	if !r.OK || r.Note != "hi" {
		t.Fatalf("result = %+v", r)
	}

	select {
	case ev := <-cl.Events():
		if ev.Type != TypeStatus {
			t.Fatalf("event type = %q", ev.Type)
		}
		var st api.Status
		if err := ev.DecodePayload(&st); err != nil {
			t.Fatal(err)
		}
		if st.State != "IDLE" || !st.Authenticated {
			t.Fatalf("status = %+v", st)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event received")
	}

	_, err = cl.Call(ctx, "boom", nil)
	var re *api.RemoteError
	if !errors.As(err, &re) {
		t.Fatalf("error = %v, want *RemoteError", err)
	}
	if re.Code != api.CodeBadRequest || re.Message != "nope" {
		t.Fatalf("remote error = %+v", re)
	}
}

func TestListenRefusesLiveDaemon(t *testing.T) {
	_, path := testServer(t, func(ctx context.Context, c *Conn) error { return nil })
	_, err := (UnixTransport{}).Listen(path)
	if err == nil {
		t.Fatal("expected error when a live daemon owns the socket")
	}
	if want := path + ": another kumiho daemon is already listening"; err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// A nil peer-credential hook must report no credentials rather than guess.
func TestServerNilPeerUID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.sock")
	srv, err := UnixTransport{}.Listen(path)
	if err != nil {
		if runtime.GOOS != "linux" {
			t.Skipf("unix sockets unavailable on this platform: %v", err)
		}
		t.Fatalf("Listen: %v", err)
	}
	srv.peerUID = nil // set before Serve starts, so there is no concurrent read
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		srv.Close()
	})

	type creds struct {
		uid uint32
		ok  bool
	}
	reported := make(chan creds, 1)
	go srv.Serve(ctx, func(ctx context.Context, c *Conn) error {
		uid, ok := c.PeerUID()
		reported <- creds{uid, ok}
		return nil
	})

	cl, err := (UnixTransport{}).Dial(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()

	select {
	case got := <-reported:
		if got.uid != 0 || got.ok {
			t.Fatalf("PeerUID() = (%d, %v), want (0, false)", got.uid, got.ok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never reported peer credentials")
	}
}

// Server.Close must remove the socket file it created.
func TestServerCloseRemovesSocket(t *testing.T) {
	srv, path := testServer(t, func(context.Context, *Conn) error { return nil })

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("socket missing before Close: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket still present after Close: stat err = %v", err)
	}
}
