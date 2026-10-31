package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
)

func TestControlErrorReply(t *testing.T) {
	region := &api.RemoteError{Code: api.CodeRegionBlocked, Message: "This region is blocking Mozilla's servers (HTTP 451)."}
	code, msg := controlErrorReply(fmt.Errorf("locations: %w", region))
	if code != api.CodeRegionBlocked || msg != region.Message {
		t.Fatalf("region reply = %q, %q", code, msg)
	}

	code, msg = controlErrorReply(errors.New("boom"))
	if code != api.CodeInternal || msg != "boom" {
		t.Fatalf("internal reply = %q, %q", code, msg)
	}
}

func controlTestEndpoint() string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf(`\\.\pipe\kumiho-daemon-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("kumiho-daemon-test-%d-%d.sock", os.Getpid(), time.Now().UnixNano()))
}

func startPingControl(t *testing.T, ping pingRun) *ipc.Client {
	t.Helper()
	endpoint := controlTestEndpoint()
	srv, err := ipc.DefaultTransport().Listen(endpoint)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &control{ping: ping, conns: make(map[*ipc.Conn]struct{})}
	go func() { _ = srv.Serve(ctx, h.handle) }()
	cl, err := ipc.DefaultTransport().Dial(ctx, endpoint)
	if err != nil {
		cancel()
		srv.Close()
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() {
		_ = cl.Close()
		cancel()
		_ = srv.Close()
	})
	return cl
}

func nextControlEvent(t *testing.T, cl *ipc.Client) *ipc.Envelope {
	t.Helper()
	select {
	case ev, ok := <-cl.Events():
		if !ok {
			t.Fatal("event channel closed")
		}
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a control event")
		return nil
	}
}

func callPing(t *testing.T, cl *ipc.Client, hosts []string) api.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := cl.Call(ctx, ipc.TypePing, api.PingPayload{Hosts: hosts})
	if err != nil {
		t.Fatalf("ping call: %v", err)
	}
	var result api.Result
	if err := resp.DecodePayload(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestDaemonPingStreamsResultsThenDone(t *testing.T) {
	cl := startPingControl(t, func(_ context.Context, hosts []string, emit func(api.PingResult)) api.PingDone {
		ms := 1.5
		emit(api.PingResult{Host: hosts[0], RTTMS: &ms})
		emit(api.PingResult{Host: hosts[1], RTTMS: nil})
		return api.PingDone{Total: 2, Answered: 1}
	})
	ack := callPing(t, cl, []string{"a.example.net:443", "b.example.net:443"})
	if !ack.OK {
		t.Fatalf("ack = %+v", ack)
	}

	for i := 0; i < 2; i++ {
		ev := nextControlEvent(t, cl)
		if ev.Type != ipc.TypePingResult {
			t.Fatalf("event %d type = %q, want %q", i, ev.Type, ipc.TypePingResult)
		}
		var result api.PingResult
		if err := ev.DecodePayload(&result); err != nil {
			t.Fatal(err)
		}
		if result.Host == "" {
			t.Fatalf("empty result host: %+v", result)
		}
	}
	ev := nextControlEvent(t, cl)
	if ev.Type != ipc.TypePingDone {
		t.Fatalf("final event type = %q, want %q", ev.Type, ipc.TypePingDone)
	}
	var done api.PingDone
	if err := ev.DecodePayload(&done); err != nil {
		t.Fatal(err)
	}
	if done.Total != 2 || done.Answered != 1 || done.Error != "" {
		t.Fatalf("ping_done = %+v", done)
	}
	select {
	case extra := <-cl.Events():
		t.Fatalf("unexpected extra event: %s", extra.Type)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestDaemonPingPreflightFailureHasNoResults(t *testing.T) {
	cl := startPingControl(t, func(context.Context, []string, func(api.PingResult)) api.PingDone {
		return api.PingDone{Error: "Ping all needs a signed-in account"}
	})
	ack := callPing(t, cl, []string{"a.example.net:443"})
	if !ack.OK {
		t.Fatalf("ack = %+v", ack)
	}
	ev := nextControlEvent(t, cl)
	if ev.Type != ipc.TypePingDone {
		t.Fatalf("event type = %q, want ping_done", ev.Type)
	}
	var done api.PingDone
	if err := ev.DecodePayload(&done); err != nil {
		t.Fatal(err)
	}
	if done.Error != "Ping all needs a signed-in account" {
		t.Fatalf("ping_done = %+v", done)
	}
	select {
	case extra := <-cl.Events():
		t.Fatalf("unexpected result event: %s", extra.Type)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestDaemonPingRejectsASecondRun(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	cl := startPingControl(t, func(_ context.Context, _ []string, _ func(api.PingResult)) api.PingDone {
		close(started)
		<-release
		return api.PingDone{Total: 1, Answered: 1}
	})
	if ack := callPing(t, cl, []string{"a.example.net:443"}); !ack.OK {
		t.Fatalf("ack = %+v", ack)
	}
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := cl.Call(ctx, ipc.TypePing, api.PingPayload{Hosts: []string{"b.example.net:443"}})
	var re *api.RemoteError
	if !errors.As(err, &re) || re.Code != api.CodeBadRequest || re.Message != "a ping run is already in progress" {
		t.Fatalf("second ping error = %v", err)
	}
	close(release)
	ev := nextControlEvent(t, cl)
	if ev.Type != ipc.TypePingDone {
		t.Fatalf("event type = %q, want ping_done", ev.Type)
	}
}

func TestDaemonPingStopsWhenTheConnectionCloses(t *testing.T) {
	started := make(chan struct{})
	finished := make(chan struct{})
	cl := startPingControl(t, func(ctx context.Context, _ []string, _ func(api.PingResult)) api.PingDone {
		close(started)
		<-ctx.Done()
		close(finished)
		return api.PingDone{Error: ctx.Err().Error()}
	})
	if ack := callPing(t, cl, []string{"a.example.net:443"}); !ack.OK {
		t.Fatalf("ack = %+v", ack)
	}
	<-started
	if err := cl.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("ping run did not stop when the connection closed")
	}
}
