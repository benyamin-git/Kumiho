// Package logging provides the daemon's log ring buffer, journald text
// output, and redaction for secrets and (optionally) target addresses.
package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Level is a log severity.
type Level int

const (
	Debug Level = iota
	Info
	Warn
	Error
)

// ParseLevel parses a level name.
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return Debug, nil
	case "info":
		return Info, nil
	case "warn", "warning":
		return Warn, nil
	case "error":
		return Error, nil
	}
	return Info, fmt.Errorf("unknown log level %q", s)
}

func (l Level) String() string {
	switch l {
	case Debug:
		return "debug"
	case Info:
		return "info"
	case Warn:
		return "warn"
	case Error:
		return "error"
	}
	return "info"
}

// Tag3 returns the three-letter level tag used in text logs.
func (l Level) Tag3() string {
	switch l {
	case Debug:
		return "DBG"
	case Info:
		return "INF"
	case Warn:
		return "WRN"
	case Error:
		return "ERR"
	}
	return "INF"
}

// MarshalJSON encodes the level as its name for the IPC protocol.
func (l Level) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

// UnmarshalJSON decodes a level name.
func (l *Level) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseLevel(s)
	if err != nil {
		return err
	}
	*l = parsed
	return nil
}

// Entry is one log record.
type Entry struct {
	Time  time.Time `json:"ts"`
	Level Level     `json:"level"`
	Tag   string    `json:"tag"`
	Msg   string    `json:"msg"`
}

// Line renders the entry in the daemon's text log format.
func (e Entry) Line() string {
	return fmt.Sprintf("%s %s [%s] %s", e.Time.Format(time.RFC3339), e.Level.Tag3(), e.Tag, e.Msg)
}

// Options configures a Ring.
type Options struct {
	Capacity int                 // max retained entries (default 1000)
	Sanitize func(string) string // nil = no sanitizing
	Sink     io.Writer           // journald/stdout text sink; nil = discard
	MinLevel Level               // entries below this are dropped (default Debug)
}

type subscriber struct {
	min Level
	ch  chan Entry
}

// Ring is a bounded, concurrency-safe log buffer with live subscriptions.
type Ring struct {
	mu       sync.Mutex
	capacity int
	sanitize func(string) string
	sink     io.Writer
	minLevel Level

	entries []Entry
	subs    map[int]*subscriber
	nextID  int
}

// NewRing creates a ring buffer.
func NewRing(opts Options) *Ring {
	r := &Ring{
		capacity: opts.Capacity,
		sanitize: opts.Sanitize,
		sink:     opts.Sink,
		minLevel: opts.MinLevel,
		subs:     make(map[int]*subscriber),
	}
	if r.capacity <= 0 {
		r.capacity = 1000
	}
	if r.sanitize == nil {
		r.sanitize = func(s string) string { return s }
	}
	return r
}

// SetMinLevel adjusts the minimum level retained and streamed.
func (r *Ring) SetMinLevel(l Level) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.minLevel = l
}

// SetSanitize swaps the message sanitizer (e.g. when the redact_targets
// setting changes at runtime). A nil function disables sanitizing.
func (r *Ring) SetSanitize(fn func(string) string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if fn == nil {
		fn = func(s string) string { return s }
	}
	r.sanitize = fn
}

// Log appends an entry, streams it to subscribers, and writes the sink line.
func (r *Ring) Log(level Level, tag, msg string) {
	msg = r.sanitize(msg)

	r.mu.Lock()
	defer r.mu.Unlock()
	if level < r.minLevel {
		return
	}
	e := Entry{Time: time.Now().UTC(), Level: level, Tag: tag, Msg: msg}

	r.entries = append(r.entries, e)
	if len(r.entries) > r.capacity {
		r.entries = append(r.entries[:0], r.entries[len(r.entries)-r.capacity:]...)
	}

	for _, s := range r.subs {
		if level < s.min {
			continue
		}
		select {
		case s.ch <- e:
		default: // slow viewer: drop rather than block the daemon
		}
	}

	if r.sink != nil {
		fmt.Fprintln(r.sink, e.Line())
	}
}

// Logf is Log with printf formatting.
func (r *Ring) Logf(level Level, tag, format string, args ...any) {
	r.Log(level, tag, fmt.Sprintf(format, args...))
}

// Snapshot returns a copy of the retained entries, oldest first.
func (r *Ring) Snapshot() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Entry, len(r.entries))
	copy(out, r.entries)
	return out
}

// Clear drops all retained entries.
func (r *Ring) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = nil
}

// Subscribe returns a channel receiving entries at or above min, and a
// cancel function. The channel is closed by the cancel function.
func (r *Ring) Subscribe(min Level) (<-chan Entry, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.nextID
	r.nextID++
	ch := make(chan Entry, 256)
	r.subs[id] = &subscriber{min: min, ch: ch}
	cancel := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if s, ok := r.subs[id]; ok {
			delete(r.subs, id)
			close(s.ch)
		}
	}
	return ch, cancel
}

// Close closes all subscriptions.
func (r *Ring) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, s := range r.subs {
		delete(r.subs, id)
		close(s.ch)
	}
}

var (
	reJWT   = regexp.MustCompile(`eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`)
	reHex   = regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`)
	reEmail = regexp.MustCompile(`([A-Za-z0-9._%+\-]+)@([A-Za-z0-9.\-]+\.[A-Za-z]{2,})`)
	reIPv4  = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
)

// RedactSecrets masks emails, hex tokens, and JWTs.
func RedactSecrets(msg string) string {
	msg = reJWT.ReplaceAllString(msg, "[redacted]")
	msg = reHex.ReplaceAllString(msg, "[redacted]")
	msg = reEmail.ReplaceAllString(msg, "***@$2")
	return msg
}

// RedactIPv4 masks IPv4 addresses.
func RedactIPv4(msg string) string {
	return reIPv4.ReplaceAllString(msg, "x.x.x.x")
}

// Sanitizer returns the message sanitizer for the given redact_targets
// setting; secrets are always redacted.
func Sanitizer(redactTargets bool) func(string) string {
	if redactTargets {
		return func(s string) string { return RedactIPv4(RedactSecrets(s)) }
	}
	return RedactSecrets
}
