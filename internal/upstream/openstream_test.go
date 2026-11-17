package upstream

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestGoAwayWakesPendingOpenStream holds the CONNECT response and then drains
// the session: the pending OpenStream must return the drain error instead of
// waiting for its own timeout.
func TestGoAwayWakesPendingOpenStream(t *testing.T) {
	srv := startTestServer(t, serverHooks{accept: func(string, string) int { return -1 }})
	defer srv.stop()
	sess := dialTestSession(t, srv, nil)

	errCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, err := sess.OpenStream(ctx, "held.example.com", 443)
		errCh <- err
	}()
	if _, ok := srv.waitEvent("connect", 2*time.Second); !ok {
		t.Fatal("edge never received the CONNECT")
	}

	srv.sendGoAway()

	select {
	case err := <-errCh:
		if !errors.Is(err, ErrDraining) {
			t.Fatalf("OpenStream error = %v, want ErrDraining", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OpenStream stayed blocked after GOAWAY")
	}
}

// TestOpenStreamTimeoutResetsTheStream checks that a CONNECT that never gets
// a response cancels its HTTP/2 stream rather than only forgetting it.
func TestOpenStreamTimeoutResetsTheStream(t *testing.T) {
	srv := startTestServer(t, serverHooks{accept: func(string, string) int { return -1 }})
	defer srv.stop()
	sess := dialTestSession(t, srv, func(o *Options) {
		o.OpenStreamTimeout = 200 * time.Millisecond
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := sess.OpenStream(ctx, "held.example.com", 443)

	var timeout *ConnectTimeout
	if !errors.As(err, &timeout) {
		t.Fatalf("OpenStream error = %v, want *ConnectTimeout", err)
	}
	ev, ok := srv.waitEvent("rst", 2*time.Second)
	if !ok {
		t.Fatal("edge never saw an RST_STREAM for the timed-out CONNECT")
	}
	if ev.StreamID == 0 {
		t.Fatal("RST_STREAM carried stream id 0")
	}
}
