package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/guardian"
	"github.com/benyamin-git/kumiho/internal/upstream"
)

// TestStaleConnectAttemptCannotPublish covers a disconnect while a dial is in
// flight followed by a new connect: the cancelled attempt must close its edge
// session instead of publishing into the newer attempt's CONNECTING state.
func TestStaleConnectAttemptCannotPublish(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	email := signInForPassTest(t, ctrl)
	ctrl.serverListURL = serverListServer(t).URL
	ctrl.exitCheckURL = ""
	ctrl.socksPortOverride = freePort(t)
	if _, err := ctrl.Locations(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.savePassFile(&guardian.Pass{
		Token:     "prepared",
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
	}, email); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ctrl.stopTunnel("test cleanup") })

	sessA, sessB := newFakeSession(), newFakeSession()
	releaseA := make(chan struct{})
	releaseB := make(chan struct{})
	enteredA := make(chan struct{})
	enteredB := make(chan struct{})
	var calls atomic.Int32
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		switch calls.Add(1) {
		case 1:
			close(enteredA)
			<-releaseA
			return sessA, nil
		default:
			close(enteredB)
			<-releaseB
			return sessB, nil
		}
	}

	errA := make(chan error, 1)
	go func() {
		errA <- ctrl.Connect(context.Background(), api.ConnectPayload{ProxyOnly: true})
	}()
	<-enteredA

	// The disconnect cancels attempt A while its dial is still blocked, then
	// attempt B starts and owns the state machine.
	if err := ctrl.Disconnect(); err != nil {
		t.Fatalf("Disconnect during connect: %v", err)
	}
	errB := make(chan error, 1)
	go func() {
		errB <- ctrl.Connect(context.Background(), api.ConnectPayload{ProxyOnly: true})
	}()
	<-enteredB

	close(releaseA)
	select {
	case err := <-errA:
		if err == nil {
			t.Fatal("the cancelled attempt published its session")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the cancelled attempt did not return")
	}
	if !sessA.closed.Load() {
		t.Fatal("the cancelled attempt left its edge session open")
	}

	close(releaseB)
	if err := <-errB; err != nil {
		t.Fatalf("second connect: %v", err)
	}
	if st := ctrl.Snapshot(); st.State != string(StateProxyOnly) {
		t.Fatalf("state = %s, want proxy-only", st.State)
	}
	if sessB.closed.Load() {
		t.Fatal("the winning session was closed")
	}
	ctrl.mu.Lock()
	live := ctrl.session == sessB
	ctrl.mu.Unlock()
	if !live {
		t.Fatal("the winning session was not published")
	}
}
