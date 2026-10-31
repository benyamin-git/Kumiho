package mobile

import (
	"context"
	"encoding/json"
	"fmt"
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
	l := newTestListener()
	f := newBareFacade(t, l)
	reachable, unreachable := "edge-a.example.net:443", "edge-b.example.net:443"
	f.pingFn = func(_ context.Context, hosts []string, emit func(api.PingResult)) api.PingDone {
		ms := 4.2
		emit(api.PingResult{Host: hosts[0], RTTMS: &ms})
		emit(api.PingResult{Host: hosts[1], RTTMS: nil})
		return api.PingDone{Total: 2, Answered: 1}
	}

	out, err := f.command(fmt.Sprintf(`{"type":"ping","payload":{"hosts":["%s","%s"]}}`, reachable, unreachable))
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	var ack api.Result
	decodeTestPayload(t, out, &ack)
	if !ack.OK {
		t.Fatal("ping ack is not ok")
	}

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
	if rtt := got[reachable]; rtt == nil || *rtt != 4.2 {
		t.Fatalf("reachable result = %v, want 4.2", rtt)
	}
	if rtt := got[unreachable]; rtt != nil {
		t.Fatalf("unreachable result = %v, want null", *rtt)
	}

	rawDone := l.waitPingDone(t)
	var done api.PingDone
	if err := json.Unmarshal([]byte(rawDone), &done); err != nil {
		t.Fatalf("OnPingDone is not JSON: %v\n%s", err, rawDone)
	}
	if done.Total != 2 || done.Answered != 1 || done.Error != "" {
		t.Fatalf("ping_done = %+v", done)
	}

	// No third result and exactly one completion callback.
	select {
	case raw := <-l.pingCh:
		t.Fatalf("unexpected extra OnPing: %s", raw)
	case <-time.After(150 * time.Millisecond):
	}
	select {
	case raw := <-l.pingDoneCh:
		t.Fatalf("unexpected extra OnPingDone: %s", raw)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestPingPreflightFailureEmitsDone(t *testing.T) {
	l := newTestListener()
	f := newBareFacade(t, l)
	f.pingFn = func(context.Context, []string, func(api.PingResult)) api.PingDone {
		return api.PingDone{Error: "Ping all needs a signed-in account"}
	}
	if _, err := f.command(`{"type":"ping","payload":{"hosts":["edge.example.net:443"]}}`); err != nil {
		t.Fatal(err)
	}
	var done api.PingDone
	if err := json.Unmarshal([]byte(l.waitPingDone(t)), &done); err != nil {
		t.Fatal(err)
	}
	if done.Error != "Ping all needs a signed-in account" {
		t.Fatalf("ping_done = %+v", done)
	}
	select {
	case raw := <-l.pingCh:
		t.Fatalf("unexpected OnPing on a preflight failure: %s", raw)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestPingRejectsASecondRunWhileBusy(t *testing.T) {
	l := newTestListener()
	f := newBareFacade(t, l)
	release := make(chan struct{})
	f.pingFn = func(ctx context.Context, _ []string, _ func(api.PingResult)) api.PingDone {
		select {
		case <-release:
			return api.PingDone{Total: 1, Answered: 1}
		case <-ctx.Done():
			return api.PingDone{Error: ctx.Err().Error()}
		}
	}
	if _, err := f.command(`{"type":"ping","payload":{"hosts":["edge.example.net:443"]}}`); err != nil {
		t.Fatal(err)
	}
	out, err := f.command(`{"type":"ping","payload":{"hosts":["edge.example.net:443"]}}`)
	if err != nil {
		t.Fatal(err)
	}
	requireErrorCode(t, out, api.CodeBadRequest)
	requireErrorMessage(t, out, "a ping run is already in progress")
	close(release)
	if done := l.waitPingDone(t); done == "" {
		t.Fatal("first run did not finish")
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
