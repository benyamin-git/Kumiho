//go:build windows

package ipc

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
)

// testPipeEndpoint returns a pipe path unique to this test process and call.
func testPipeEndpoint() string {
	return fmt.Sprintf(`\\.\pipe\kumiho-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
}

// testPipeServer starts a named-pipe server for tests.
func testPipeServer(t *testing.T, handler func(context.Context, *Conn) error) (*Server, string) {
	t.Helper()
	endpoint := testPipeEndpoint()
	srv, err := (WindowsTransport{}).Listen(endpoint)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		srv.Close()
	})
	go srv.Serve(ctx, handler)
	return srv, endpoint
}

func TestWindowsPipeRoundTrip(t *testing.T) {
	_, endpoint := testPipeServer(t, func(ctx context.Context, c *Conn) error {
		env, err := c.Read()
		if err != nil {
			return err
		}
		if env.Type != TypeHello {
			return fmt.Errorf("first request type = %q, want %q", env.Type, TypeHello)
		}
		return c.Reply(env.ID, TypeHello, api.HelloResult{Version: "test", Protocol: ProtocolVersion})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cl, err := (WindowsTransport{}).Dial(ctx, endpoint)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer cl.Close()

	resp, err := cl.Call(ctx, TypeHello, api.HelloPayload{ClientVersion: "test"})
	if err != nil {
		t.Fatalf("hello: %v", err)
	}
	if resp.Type != TypeHello {
		t.Fatalf("response type = %q, want %q", resp.Type, TypeHello)
	}
	var hello api.HelloResult
	if err := resp.DecodePayload(&hello); err != nil {
		t.Fatal(err)
	}
	if hello.Protocol != ProtocolVersion {
		t.Fatalf("hello protocol = %d, want %d", hello.Protocol, ProtocolVersion)
	}
}

func TestPipeSDDL(t *testing.T) {
	const group = "S-1-5-21-1-2-3-1001"
	if got, want := pipeSDDL(group), "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;"+group+")"; got != want {
		t.Fatalf("pipeSDDL(group) = %q, want %q", got, want)
	}
	if got, want := pipeSDDL(""), "D:P(A;;GA;;;SY)(A;;GA;;;BA)"; got != want {
		t.Fatalf("pipeSDDL(\"\") = %q, want %q", got, want)
	}
}

// A connection that is not a named pipe carries no client process to check;
// peerIsAdmin must fail closed.
func TestPeerIsAdminFalse(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	if peerIsAdmin(server) {
		t.Fatal("peerIsAdmin(net.Pipe server) = true, want false")
	}
	if peerIsAdmin(client) {
		t.Fatal("peerIsAdmin(net.Pipe client) = true, want false")
	}
}

func TestWindowsListenRefusesLiveDaemon(t *testing.T) {
	_, endpoint := testPipeServer(t, func(context.Context, *Conn) error { return nil })
	_, err := (WindowsTransport{}).Listen(endpoint)
	if err == nil {
		t.Fatal("expected error when a live daemon owns the pipe")
	}
	if want := endpoint + ": another kumiho daemon is already listening"; err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}
