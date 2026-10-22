package mobile

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/engine"
	"github.com/benyamin-git/kumiho/internal/logging"
)

// newBareFacade builds a facade without the singleton or the status pump, so
// the test drives noteTransitions on a single goroutine and observes every
// notice synchronously.
func newBareFacade(t *testing.T, l Listener) *facade {
	t.Helper()
	f, err := newFacade(testPaths(t).json(t), nil, l)
	if err != nil {
		t.Fatalf("newFacade: %v", err)
	}
	t.Cleanup(func() {
		f.shutdown()
		f.wg.Wait()
	})
	return f
}

func noticeAt(t *testing.T, l *testListener, i int) notice {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if i >= len(l.notices) {
		t.Fatalf("notice %d is missing; got %d", i, len(l.notices))
	}
	var n notice
	if err := json.Unmarshal([]byte(l.notices[i]), &n); err != nil {
		t.Fatalf("OnNotice is not a notice: %v\n%s", err, l.notices[i])
	}
	return n
}

func requireNoticeCount(t *testing.T, l *testListener, want int) {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.notices) != want {
		t.Fatalf("OnNotice fired %d times, want %d: %v", len(l.notices), want, l.notices)
	}
}

// decodePingPayload asserts the callback payload is exactly the two-field
// PingResult shape, with null for a failed probe.
func decodePingPayload(t *testing.T, raw string) (host string, rttMS *float64) {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		t.Fatalf("OnPing is not JSON: %v\n%s", err, raw)
	}
	if len(fields) != 2 {
		t.Fatalf("OnPing payload has %d fields, want exactly host and rtt_ms: %s", len(fields), raw)
	}
	hostRaw, ok := fields["host"]
	if !ok {
		t.Fatalf("OnPing payload lacks host: %s", raw)
	}
	if err := json.Unmarshal(hostRaw, &host); err != nil {
		t.Fatalf("OnPing host is not a string: %v\n%s", err, hostRaw)
	}
	rttRaw, ok := fields["rtt_ms"]
	if !ok {
		t.Fatalf("OnPing payload lacks rtt_ms: %s", raw)
	}
	if string(rttRaw) == "null" {
		return host, nil
	}
	var ms float64
	if err := json.Unmarshal(rttRaw, &ms); err != nil {
		t.Fatalf("OnPing rtt_ms is neither a number nor null: %v\n%s", err, rttRaw)
	}
	return host, &ms
}

func TestPingEmitsOneCallbackPerHost(t *testing.T) {
	reachableLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer reachableLn.Close()
	reachable := reachableLn.Addr().String()

	// Bind and close a port so the refusal is immediate and the address is
	// known to have been free a moment ago.
	closedLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	unreachable := closedLn.Addr().String()
	if err := closedLn.Close(); err != nil {
		t.Fatalf("close unreachable listener: %v", err)
	}

	l := newTestListener()
	startTest(t, testPaths(t), nil, l)
	mustCommand(t, "ping", map[string]any{"hosts": []string{reachable, unreachable}})

	got := map[string]*float64{}
	for i := 0; i < 2; i++ {
		raw := l.waitPing(t)
		host, rtt := decodePingPayload(t, raw)
		if _, dup := got[host]; dup {
			t.Fatalf("duplicate OnPing for %s: %s", host, raw)
		}
		got[host] = rtt
	}
	if len(got) != 2 {
		t.Fatalf("got %d hosts, want 2: %v", len(got), got)
	}
	rtt, ok := got[reachable]
	if !ok {
		t.Fatalf("no OnPing for the reachable host %s: %v", reachable, got)
	}
	if rtt == nil {
		t.Errorf("rtt_ms for the reachable host %s is null", reachable)
	} else if *rtt < 0 {
		t.Errorf("rtt_ms for %s = %v, want a non-negative duration", reachable, *rtt)
	}
	rtt, ok = got[unreachable]
	if !ok {
		t.Fatalf("no OnPing for the unreachable host %s: %v", unreachable, got)
	}
	if rtt != nil {
		t.Errorf("rtt_ms for the unreachable host %s = %v, want null", unreachable, *rtt)
	}

	// No third callback: exactly one per requested host.
	select {
	case raw := <-l.pingCh:
		t.Fatalf("unexpected extra OnPing: %s", raw)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestNoticeOnSignedOutAndFatalTransitions(t *testing.T) {
	l := newTestListener()
	f := newBareFacade(t, l)

	// Baseline: the zero previous status has no transition to report.
	f.noteTransitions(api.Status{Authenticated: true, State: string(engine.StateConnected)})
	requireNoticeCount(t, l, 0)

	f.noteTransitions(api.Status{Authenticated: false, State: string(engine.StateUnauthenticated)})
	requireNoticeCount(t, l, 1)
	n := noticeAt(t, l, 0)
	if n.Type != "signed_out" || n.State != string(engine.StateUnauthenticated) {
		t.Fatalf("signed-out notice = %+v", n)
	}
	if n.Message == "" {
		t.Error("signed-out notice has no message")
	}

	// An unchanged unauthenticated state is not a transition.
	f.noteTransitions(api.Status{Authenticated: false, State: string(engine.StateUnauthenticated)})
	requireNoticeCount(t, l, 1)

	// The FATAL notice text comes from the latest warn-or-worse ring entry.
	f.logf(logging.Warn, "test", "edge session failed")
	f.noteTransitions(api.Status{Authenticated: false, State: string(engine.StateFatal)})
	requireNoticeCount(t, l, 2)
	n = noticeAt(t, l, 1)
	if n.Type != "fatal" || n.State != string(engine.StateFatal) {
		t.Fatalf("fatal notice = %+v", n)
	}
	if n.Message != "edge session failed" {
		t.Fatalf("fatal message = %q, want the warn ring entry", n.Message)
	}

	// Staying in FATAL does not repeat the notice.
	f.noteTransitions(api.Status{Authenticated: false, State: string(engine.StateFatal)})
	requireNoticeCount(t, l, 2)
}
