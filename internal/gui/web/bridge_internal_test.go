// Internal tests: package web tests must not import internal/gui or
// internal/gui/mock (the windows cross-vet would fail on the gui → web → gui
// cycle), so anything needing those packages lives in the external
// harness_test.go. This file covers the harness internals with stub
// callbacks: constructor validation and the broadcaster's slow-consumer
// policy.
package web

import (
	"errors"
	"strconv"
	"testing"
	"time"
)

// stubHarness builds a harness with inert callbacks (internal tests only
// need the constructor and the broadcaster).
func stubHarness(t *testing.T, opts Options) *Harness {
	t.Helper()
	h, err := NewHarness(HarnessConfig{
		Server:       New(StaticFS(), opts),
		Snapshot:     func() ([]byte, error) { return []byte(`{"type":"state","state":{}}`), nil },
		DecodeAction: func([]byte) (Action, error) { return Action{}, errors.New("stub") },
		Dispatch:     func(Action) error { return nil },
		DevControl: &DevControl{
			SetScenario: func(string) {},
			SetLatency:  func(d time.Duration) {},
			FailNext:    func(error) {},
			EmitLog:     func(string) {},
		},
	})
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	return h
}

func TestNewHarnessValidation(t *testing.T) {
	base := HarnessConfig{
		Server:       New(StaticFS(), Options{}),
		Snapshot:     func() ([]byte, error) { return nil, nil },
		DecodeAction: func([]byte) (Action, error) { return Action{}, nil },
		Dispatch:     func(Action) error { return nil },
	}

	cases := []struct {
		name string
		cfg  HarnessConfig
	}{
		{"missing server", HarnessConfig{Snapshot: base.Snapshot, DecodeAction: base.DecodeAction, Dispatch: base.Dispatch}},
		{"missing snapshot", HarnessConfig{Server: New(StaticFS(), Options{}), DecodeAction: base.DecodeAction, Dispatch: base.Dispatch}},
		{"missing decode", HarnessConfig{Server: New(StaticFS(), Options{}), Snapshot: base.Snapshot, Dispatch: base.Dispatch}},
		{"missing dispatch", HarnessConfig{Server: New(StaticFS(), Options{}), Snapshot: base.Snapshot, DecodeAction: base.DecodeAction}},
		{"dev without control", HarnessConfig{Server: New(StaticFS(), Options{Dev: true}), Snapshot: base.Snapshot, DecodeAction: base.DecodeAction, Dispatch: base.Dispatch}},
		{"partial dev control", HarnessConfig{Server: New(StaticFS(), Options{}), Snapshot: base.Snapshot, DecodeAction: base.DecodeAction, Dispatch: base.Dispatch, DevControl: &DevControl{SetScenario: func(string) {}}}},
	}
	for _, tc := range cases {
		if _, err := NewHarness(tc.cfg); err == nil {
			t.Errorf("NewHarness(%s) succeeded; want an error", tc.name)
		}
	}

	srv := New(StaticFS(), Options{})
	if _, err := NewHarness(HarnessConfig{Server: srv, Snapshot: base.Snapshot, DecodeAction: base.DecodeAction, Dispatch: base.Dispatch}); err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	if _, err := NewHarness(HarnessConfig{Server: srv, Snapshot: base.Snapshot, DecodeAction: base.DecodeAction, Dispatch: base.Dispatch}); err == nil {
		t.Error("second NewHarness over the same server succeeded; want an error")
	}
}

func TestSubscribeSendsInitialSnapshot(t *testing.T) {
	h := stubHarness(t, Options{})
	sub, raw, err := h.subscribe()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer h.unsubscribe(sub)
	if len(raw) == 0 {
		t.Fatal("subscribe returned no initial snapshot")
	}

	// A snapshot failure is reported but keeps the subscription alive.
	h.cfg.Snapshot = func() ([]byte, error) { return nil, errors.New("boom") }
	sub2, raw2, err := h.subscribe()
	if err == nil || raw2 != nil {
		t.Fatalf("subscribe with a failing snapshot = (%q, %v); want (nil, error)", raw2, err)
	}
	defer h.unsubscribe(sub2)
	h.broadcast([]byte("later"))
	select {
	case got := <-sub2.ch:
		if string(got) != "later" {
			t.Errorf("post-failure event = %q; want later", got)
		}
	default:
		t.Error("a failed initial snapshot dropped the subscription")
	}
}

func TestBroadcastDropsOldestForSlowSubscriber(t *testing.T) {
	h := stubHarness(t, Options{})
	sub, _, err := h.subscribe()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer h.unsubscribe(sub)

	total := subscriberBuffer + 4
	for i := 0; i < total; i++ {
		h.broadcast([]byte(strconv.Itoa(i)))
	}

	var got []string
	for {
		select {
		case v := <-sub.ch:
			got = append(got, string(v))
			continue
		default:
		}
		break
	}
	if len(got) != subscriberBuffer {
		t.Fatalf("slow subscriber holds %d events; want %d", len(got), subscriberBuffer)
	}
	if got[0] != "4" {
		t.Errorf("oldest kept event = %q; want %q (the first four dropped)", got[0], "4")
	}
	if want := strconv.Itoa(total - 1); got[len(got)-1] != want {
		t.Errorf("newest event = %q; want %q", got[len(got)-1], want)
	}
}
