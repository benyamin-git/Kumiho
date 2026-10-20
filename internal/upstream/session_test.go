package upstream

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/net/http2"
)

// writeRecorder captures the raw writes a wrapper saw before the TLS stack
// handed them to the socket.
type writeRecorder struct {
	mu     sync.Mutex
	writes [][]byte
}

func (r *writeRecorder) record(p []byte) {
	r.mu.Lock()
	r.writes = append(r.writes, append([]byte(nil), p...))
	r.mu.Unlock()
}

func (r *writeRecorder) first() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.writes) == 0 {
		return nil
	}
	return append([]byte(nil), r.writes[0]...)
}

// recordingConn records every write before delegating to the wrapped conn.
type recordingConn struct {
	net.Conn
	rec *writeRecorder
}

func (c *recordingConn) Write(p []byte) (int, error) {
	c.rec.record(p)
	return c.Conn.Write(p)
}

func dialTestSession(t *testing.T, srv *testServer, tweak func(*Options)) *Session {
	t.Helper()
	opts := srv.dialOptions(t, Options{})
	opts.Pass = func() string { return "test-pass" }
	if tweak != nil {
		tweak(&opts)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := Dial(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-srv.ready:
	case <-time.After(3 * time.Second):
		t.Fatal("test server never became ready")
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

func waitMaxConcurrent(t *testing.T, s *Session, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		got := s.maxConcurrent
		s.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("maxConcurrent never became %d", want)
}

func TestWrapConnAppliedOnceBeforeHandshake(t *testing.T) {
	srv := startTestServer(t, serverHooks{echo: true})
	defer srv.stop()

	rec := &writeRecorder{}
	var wraps atomic.Int32
	sess := dialTestSession(t, srv, func(o *Options) {
		o.WrapConn = func(conn net.Conn) net.Conn {
			wraps.Add(1)
			return &recordingConn{Conn: conn, rec: rec}
		}
	})
	if got := wraps.Load(); got != 1 {
		t.Fatalf("WrapConn called %d times, want 1", got)
	}

	// The wrapper saw the ClientHello, so it was installed before the
	// handshake rather than only for stream traffic.
	first := rec.first()
	if len(first) < 2 || first[0] != 0x16 || first[1] != 0x03 {
		t.Fatalf("first write = %x, want a TLS handshake record (0x16 0x03)", first)
	}

	// The session still works end to end through the wrapper.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := sess.OpenStream(ctx, "www.example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "hello" {
		t.Fatalf("echo = %q", buf)
	}
}

// failingDialer always fails, so a successful dial proves it was never used.
func failingDialer() *net.Dialer {
	return &net.Dialer{Control: func(_, _ string, _ syscall.RawConn) error {
		return errors.New("test: dialer must not be used")
	}}
}

// dialRecordingContext records the dial a custom DialContext performs.
type dialRecordingContext struct {
	network, addr string
	dialed        bool
}

func (d *dialRecordingContext) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	d.network, d.addr, d.dialed = network, addr, true
	return (&net.Dialer{}).DialContext(ctx, network, addr)
}

func TestDialContextWinsOverDialer(t *testing.T) {
	srv := startTestServer(t, serverHooks{echo: true})
	defer srv.stop()

	rec := &dialRecordingContext{}
	sess := dialTestSession(t, srv, func(o *Options) {
		o.DialContext = rec.dial
		o.Dialer = failingDialer()
	})
	if !rec.dialed {
		t.Fatal("DialContext was never called")
	}
	want := "127.0.0.1:" + strconv.Itoa(srv.port)
	if rec.network != "tcp" || rec.addr != want {
		t.Fatalf("DialContext called with %s %q, want tcp %q", rec.network, rec.addr, want)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := sess.OpenStream(ctx, "www.example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "hello" {
		t.Fatalf("echo = %q", buf)
	}
}

func TestDialerUsedWhenNoDialContext(t *testing.T) {
	srv := startTestServer(t, serverHooks{echo: true})
	defer srv.stop()

	// No DialContext: the *net.Dialer still dials (production bypass path);
	// a nil Dialer (zero dialer fallback) is what every other test here uses.
	sess := dialTestSession(t, srv, func(o *Options) {
		o.Dialer = &net.Dialer{}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := sess.OpenStream(ctx, "www.example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
}

func TestWrapConnWrapsDialContextConnOnceBeforeHandshake(t *testing.T) {
	srv := startTestServer(t, serverHooks{echo: true})
	defer srv.stop()

	rec := &writeRecorder{}
	dialer := &dialRecordingContext{}
	var wraps atomic.Int32
	var wrappedFrom net.Conn
	sess := dialTestSession(t, srv, func(o *Options) {
		o.DialContext = dialer.dial
		o.WrapConn = func(conn net.Conn) net.Conn {
			wraps.Add(1)
			wrappedFrom = conn
			return &recordingConn{Conn: conn, rec: rec}
		}
	})
	if got := wraps.Load(); got != 1 {
		t.Fatalf("WrapConn called %d times, want 1", got)
	}
	if !dialer.dialed || wrappedFrom == nil {
		t.Fatal("the DialContext-provided connection never reached WrapConn")
	}
	// The wrapper saw the ClientHello: WrapConn ran after the proxy hop and
	// before the TLS handshake.
	first := rec.first()
	if len(first) < 2 || first[0] != 0x16 || first[1] != 0x03 {
		t.Fatalf("first write = %x, want a TLS handshake record (0x16 0x03)", first)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := sess.OpenStream(ctx, "www.example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("still-works")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len("still-works"))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "still-works" {
		t.Fatalf("echo = %q", buf)
	}
}

func TestWrapConnSameConnIsHarmless(t *testing.T) {
	srv := startTestServer(t, serverHooks{echo: true})
	defer srv.stop()

	sess := dialTestSession(t, srv, func(o *Options) {
		o.WrapConn = func(conn net.Conn) net.Conn { return conn }
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := sess.OpenStream(ctx, "www.example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("still-works")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len("still-works"))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "still-works" {
		t.Fatalf("echo = %q", buf)
	}
}

// closeRecordingConn counts Close calls on the wrapper the dialer installed.
type closeRecordingConn struct {
	net.Conn
	closed atomic.Int32
}

func (c *closeRecordingConn) Close() error {
	c.closed.Add(1)
	return c.Conn.Close()
}

func TestHandshakeFailureClosesWrappedConn(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	wrapped := &closeRecordingConn{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = Dial(ctx, Options{
		Host:           testEdgeHost,
		PinnedIP:       "127.0.0.1",
		Port:           port,
		ConnectTimeout: time.Second,
		WrapConn: func(conn net.Conn) net.Conn {
			wrapped.Conn = conn
			return wrapped
		},
	})
	if err == nil {
		t.Fatal("Dial succeeded against a peer that closed during the handshake")
	}
	if got := wrapped.closed.Load(); got != 1 {
		t.Fatalf("wrapped conn closed %d times, want 1", got)
	}
}

func TestConnectEchoAndHalfClose(t *testing.T) {
	srv := startTestServer(t, serverHooks{echo: true})
	defer srv.stop()
	sess := dialTestSession(t, srv, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := sess.OpenStream(ctx, "www.example.com", 443)
	if err != nil {
		t.Fatal(err)
	}

	ev, ok := srv.waitEvent("connect", 2*time.Second)
	if !ok {
		t.Fatal("server never saw the CONNECT")
	}
	if ev.Authority != "www.example.com:443" {
		t.Fatalf("authority = %q", ev.Authority)
	}
	if ev.Auth != "Bearer test-pass" {
		t.Fatalf("proxy-authorization = %q", ev.Auth)
	}

	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "hello" {
		t.Fatalf("echo = %q", buf)
	}

	cw, ok := conn.(interface{ CloseWrite() error })
	if !ok {
		t.Fatal("stream does not support CloseWrite")
	}
	if err := cw.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if _, ok := srv.waitEvent("endstream", 2*time.Second); !ok {
		t.Fatal("server never saw END_STREAM")
	}
	if _, err := io.ReadFull(conn, make([]byte, 1)); err != io.EOF {
		t.Fatalf("read after half-close = %v, want EOF", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConnectRejectedStatuses(t *testing.T) {
	cases := []struct {
		status      int
		authRej     bool
		unreachable bool
	}{
		{401, true, false},
		{403, true, false},
		{407, true, false},
		{502, false, true},
		{503, false, true},
		{504, false, true},
		{418, false, false},
	}
	for _, tc := range cases {
		status := tc.status
		srv := startTestServer(t, serverHooks{accept: func(string, string) int { return status }})
		sess := dialTestSession(t, srv, nil)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err := sess.OpenStream(ctx, "blocked.example.com", 443)
		cancel()
		srv.stop()

		var rej *ConnectRejected
		if !errors.As(err, &rej) || rej.Status != status {
			t.Fatalf("status %d: err = %v", status, err)
		}
		if IsAuthRejected(err) != tc.authRej {
			t.Fatalf("status %d: IsAuthRejected = %v", status, IsAuthRejected(err))
		}
		if IsTargetUnreachable(err) != tc.unreachable {
			t.Fatalf("status %d: IsTargetUnreachable = %v", status, IsTargetUnreachable(err))
		}
	}
}

func TestGoAwayDrainsSession(t *testing.T) {
	srv := startTestServer(t, serverHooks{})
	defer srv.stop()
	sess := dialTestSession(t, srv, nil)

	srv.sendGoAway()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && sess.IsConnected() {
		time.Sleep(10 * time.Millisecond)
	}
	if sess.IsConnected() {
		t.Fatal("session still accepts streams after GOAWAY")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err := sess.OpenStream(ctx, "after-goaway.example.com", 443)
	if !errors.Is(err, ErrDraining) && !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("OpenStream after GOAWAY = %v", err)
	}
}

func TestClientKeepalivePingIsAcked(t *testing.T) {
	srv := startTestServer(t, serverHooks{})
	defer srv.stop()
	sess := dialTestSession(t, srv, func(o *Options) {
		o.KeepaliveCheck = 20 * time.Millisecond
		o.KeepaliveIdle = 80 * time.Millisecond
		o.KeepaliveTimeout = 500 * time.Millisecond
	})

	ev, ok := srv.waitEvent("ping", 2*time.Second)
	if !ok {
		t.Fatal("client never sent a keepalive ping")
	}
	if string(ev.Ping) != keepalivePingPayload {
		t.Fatalf("ping payload = %q", ev.Ping)
	}
	time.Sleep(300 * time.Millisecond)
	if !sess.IsConnected() {
		t.Fatalf("session died despite ping ack: %v", sess.Err())
	}
}

func TestUnansweredKeepaliveKillsSession(t *testing.T) {
	srv := startTestServer(t, serverHooks{dropPingAcks: true})
	defer srv.stop()
	sess := dialTestSession(t, srv, func(o *Options) {
		o.KeepaliveCheck = 20 * time.Millisecond
		o.KeepaliveIdle = 50 * time.Millisecond
		o.KeepaliveTimeout = 150 * time.Millisecond
	})

	if _, ok := srv.waitEvent("ping", 2*time.Second); !ok {
		t.Fatal("client never sent a keepalive ping")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if sess.Err() != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("session survived an unanswered keepalive ping")
}

func TestServerPingIsAcked(t *testing.T) {
	srv := startTestServer(t, serverHooks{})
	defer srv.stop()
	_ = dialTestSession(t, srv, nil)

	payload := []byte("12345678")
	srv.sendPing(payload)
	ev, ok := srv.waitEvent("ping_ack", 2*time.Second)
	if !ok {
		t.Fatal("client never acked the server PING")
	}
	if !bytes.Equal(ev.Ping, payload) {
		t.Fatalf("ping ack payload = %q", ev.Ping)
	}
}

func TestConcurrentStreamSlotLimit(t *testing.T) {
	srv := startTestServer(t, serverHooks{}, http2.Setting{ID: http2.SettingMaxConcurrentStreams, Val: 1})
	defer srv.stop()
	sess := dialTestSession(t, srv, func(o *Options) {
		o.StreamSlotTimeout = 300 * time.Millisecond
	})
	waitMaxConcurrent(t, sess, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn1, err := sess.OpenStream(ctx, "a.example.com", 443)
	if err != nil {
		t.Fatal(err)
	}

	var timeout *ConnectTimeout
	if _, err := sess.OpenStream(ctx, "b.example.com", 443); !errors.As(err, &timeout) {
		t.Fatalf("second stream = %v, want ConnectTimeout", err)
	}

	if err := conn1.Close(); err != nil {
		t.Fatal(err)
	}
	conn2, err := sess.OpenStream(ctx, "b.example.com", 443)
	if err != nil {
		t.Fatalf("stream after freeing a slot: %v", err)
	}
	_ = conn2.Close()
}

func TestOutboundFlowControl(t *testing.T) {
	srv := startTestServer(t,
		serverHooks{autoCredit: true, creditThreshold: 16 * 1024},
		http2.Setting{ID: http2.SettingInitialWindowSize, Val: 16384},
	)
	defer srv.stop()
	sess := dialTestSession(t, srv, nil)
	waitPeerInitialWindow(t, sess, 16384)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := sess.OpenStream(ctx, "big.example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	ev, ok := srv.waitEvent("connect", 2*time.Second)
	if !ok {
		t.Fatal("no connect event")
	}

	payload := bytes.Repeat([]byte("k"), 256*1024)
	done := make(chan error, 1)
	go func() {
		_, err := conn.Write(payload)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("write: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("write stalled: outbound flow control is broken")
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(srv.streamReceived(ev.StreamID)) == len(payload) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server received %d bytes, want %d", len(srv.streamReceived(ev.StreamID)), len(payload))
}
