package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]Level{
		"debug": Debug, "INFO": Info, "Warn": Warn, "warning": Warn, "error": Error,
	}
	for in, want := range cases {
		got, err := ParseLevel(in)
		if err != nil || got != want {
			t.Errorf("ParseLevel(%q) = (%v, %v), want %v", in, got, err, want)
		}
	}
	if _, err := ParseLevel("loud"); err == nil {
		t.Error("expected error for unknown level")
	}
}

func TestLevelJSON(t *testing.T) {
	b, err := json.Marshal(Warn)
	if err != nil || string(b) != `"warn"` {
		t.Fatalf("marshal = %s, %v", b, err)
	}
	var l Level
	if err := json.Unmarshal([]byte(`"error"`), &l); err != nil || l != Error {
		t.Fatalf("unmarshal = %v, %v", l, err)
	}
	if err := json.Unmarshal([]byte(`"loud"`), &l); err == nil {
		t.Error("expected error for bad level JSON")
	}
}

func TestRingEvictionAndSnapshot(t *testing.T) {
	r := NewRing(Options{Capacity: 3})
	for i := 0; i < 5; i++ {
		r.Logf(Info, "test", "entry %d", i)
	}
	snap := r.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("snapshot len = %d, want 3", len(snap))
	}
	if snap[0].Msg != "entry 2" || snap[2].Msg != "entry 4" {
		t.Fatalf("eviction wrong: %q .. %q", snap[0].Msg, snap[2].Msg)
	}
	r.Clear()
	if len(r.Snapshot()) != 0 {
		t.Fatal("clear did not drop entries")
	}
}

func TestRingMinLevel(t *testing.T) {
	r := NewRing(Options{Capacity: 10, MinLevel: Warn})
	r.Log(Debug, "t", "no")
	r.Log(Info, "t", "no")
	r.Log(Warn, "t", "yes")
	r.Log(Error, "t", "yes")
	snap := r.Snapshot()
	if len(snap) != 2 || snap[0].Level != Warn || snap[1].Level != Error {
		t.Fatalf("min level filter wrong: %+v", snap)
	}
}

func TestSubscribeReceivesAndCancels(t *testing.T) {
	r := NewRing(Options{Capacity: 10})
	ch, cancel := r.Subscribe(Warn)
	r.Log(Info, "t", "dropped by min")
	r.Log(Warn, "t", "delivered")

	select {
	case e := <-ch:
		if e.Msg != "delivered" {
			t.Fatalf("got %q", e.Msg)
		}
	case <-time.After(time.Second):
		t.Fatal("no entry received")
	}

	cancel()
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed after cancel")
	}
}

func TestSinkWritesText(t *testing.T) {
	var buf bytes.Buffer
	r := NewRing(Options{Capacity: 10, Sink: &buf})
	r.Log(Error, "svc", "boom")
	out := buf.String()
	for _, want := range []string{"ERR", "[svc]", "boom"} {
		if !strings.Contains(out, want) {
			t.Errorf("sink output missing %q: %s", want, out)
		}
	}
}

func TestRedaction(t *testing.T) {
	in := "user alice@example.com token 0e87c42ad8741beb52df3f2770b9ce9d5a38d512cfd3c79be16d05a532b57098 jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abcdef"
	out := RedactSecrets(in)
	for _, leak := range []string{"alice@example.com", "0e87c42ad8741beb52df3f2770b9ce9d5a38d512cfd3c79be16d05a532b57098", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abcdef"} {
		if strings.Contains(out, leak) {
			t.Errorf("redaction leaked %q in %q", leak, out)
		}
	}
	if !strings.Contains(out, "***@example.com") {
		t.Errorf("email domain should be preserved: %q", out)
	}
}

func TestSanitizerTargetRedaction(t *testing.T) {
	withTargets := Sanitizer(true)
	if got := withTargets("dialing 203.0.113.7:443"); strings.Contains(got, "203.0.113.7") {
		t.Errorf("target IP not redacted: %q", got)
	}
	withoutTargets := Sanitizer(false)
	if got := withoutTargets("dialing 203.0.113.7:443"); !strings.Contains(got, "203.0.113.7") {
		t.Errorf("target IP should be kept when redact_targets=false: %q", got)
	}
}

func TestSetSanitizeLive(t *testing.T) {
	r := NewRing(Options{Capacity: 10})
	r.Log(Info, "t", "dialing 203.0.113.7")
	if got := r.Snapshot()[0].Msg; !strings.Contains(got, "203.0.113.7") {
		t.Fatalf("default sanitizer changed the message: %q", got)
	}
	r.SetSanitize(Sanitizer(true))
	r.Log(Info, "t", "dialing 203.0.113.7")
	snap := r.Snapshot()
	if got := snap[len(snap)-1].Msg; strings.Contains(got, "203.0.113.7") {
		t.Fatalf("SetSanitize did not apply: %q", got)
	}
	r.SetSanitize(nil)
	r.Log(Info, "t", "dialing 203.0.113.8")
	snap = r.Snapshot()
	if got := snap[len(snap)-1].Msg; !strings.Contains(got, "203.0.113.8") {
		t.Fatalf("nil sanitizer should be the identity: %q", got)
	}
}

func TestEntryLine(t *testing.T) {
	e := Entry{
		Time:  time.Date(2026, 10, 8, 8, 27, 35, 0, time.UTC),
		Level: Warn, Tag: "watchdog", Msg: "upstream session lost",
	}
	want := "2026-10-08T08:27:35Z WRN [watchdog] upstream session lost"
	if got := e.Line(); got != want {
		t.Fatalf("Line = %q, want %q", got, want)
	}
}
