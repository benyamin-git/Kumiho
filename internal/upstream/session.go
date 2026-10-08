// Package upstream implements the HTTP/2 CONNECT data plane to a Fastly
// edge (PLAN.md §2.3): one TLS/ALPN-h2 session carrying one HTTP/2 stream per
// proxied flow, with keepalive pings, flow control, concurrent-stream limits
// and in-place proxy-pass swapping.
package upstream

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"

	"github.com/benyamin-git/kumiho/internal/logging"
)

const (
	// Client settings (PLAN.md §2.3): 16 MiB windows, 256 KiB frames, no push.
	defaultInitialWindow = 16 << 20
	defaultMaxFrameSize  = 256 << 10

	// keepalivePingPayload is the reference client's PING payload:
	// 0x00466F787956504E ("\x00FoxyVPN").
	keepalivePingPayload = "\x00FoxyVPN"

	http2Preface = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"
)

// Options configures a Session.
type Options struct {
	Host     string // edge hostname from the server list (TLS SNI + verification)
	Port     int
	PinnedIP string        // dial this IP while still presenting Host (optional)
	Pass     func() string // returns the current proxy pass; may be swapped any time

	Dialer  *net.Dialer    // optional dialer (M4 installs the SO_MARK hook here)
	RootCAs *x509.CertPool // tests

	KeepaliveCheck    time.Duration // default 3s
	KeepaliveIdle     time.Duration // default 15s
	KeepaliveTimeout  time.Duration // default 10s
	OpenStreamTimeout time.Duration // default 20s
	ConnectTimeout    time.Duration // default 15s
	StreamSlotTimeout time.Duration // default 15s

	Logf func(level logging.Level, tag, format string, args ...any)
}

// Session is a live HTTP/2 session to one edge.
type Session struct {
	opts   Options
	conn   net.Conn
	framer *http2.Framer

	sendMu   sync.Mutex
	hpackEnc *hpack.Encoder
	hpackBuf []byte

	mu              sync.Mutex
	streams         map[uint32]*stream
	nextStreamID    uint32
	maxConcurrent   int
	activeStreams   int
	slotSignal      chan struct{}
	draining        bool
	closed          bool
	err             error
	pingOutstanding bool
	pingSentAt      time.Time

	closedCh  chan struct{}
	closeOnce sync.Once

	lastActivityNs     atomic.Int64
	totalUp, totalDown atomic.Int64

	connSendWindow   *window
	peerMaxFrameSize atomic.Uint32
	peerInitialWin   int64
}

// Dial opens a TLS/ALPN-h2 session to the edge.
func Dial(ctx context.Context, opts Options) (*Session, error) {
	if opts.Host == "" || opts.Port == 0 {
		return nil, errors.New("upstream: host and port are required")
	}
	if opts.Pass == nil {
		opts.Pass = func() string { return "" }
	}
	if opts.KeepaliveCheck <= 0 {
		opts.KeepaliveCheck = 3 * time.Second
	}
	if opts.KeepaliveIdle <= 0 {
		opts.KeepaliveIdle = 15 * time.Second
	}
	if opts.KeepaliveTimeout <= 0 {
		opts.KeepaliveTimeout = 10 * time.Second
	}
	if opts.OpenStreamTimeout <= 0 {
		opts.OpenStreamTimeout = 20 * time.Second
	}
	if opts.ConnectTimeout <= 0 {
		opts.ConnectTimeout = 15 * time.Second
	}
	if opts.StreamSlotTimeout <= 0 {
		opts.StreamSlotTimeout = 15 * time.Second
	}

	dialHost := opts.Host
	if opts.PinnedIP != "" {
		dialHost = opts.PinnedIP
	}
	addr := net.JoinHostPort(dialHost, strconv.Itoa(opts.Port))

	dialer := opts.Dialer
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	rawConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("upstream: dial %s: %w", addr, err)
	}

	tlsConn := tls.Client(rawConn, &tls.Config{
		ServerName: opts.Host,
		NextProtos: []string{"h2"},
		MinVersion: tls.VersionTLS12,
		RootCAs:    opts.RootCAs,
	})
	hsCtx, cancel := context.WithTimeout(ctx, opts.ConnectTimeout)
	defer cancel()
	if err := tlsConn.HandshakeContext(hsCtx); err != nil {
		_ = rawConn.Close()
		return nil, fmt.Errorf("upstream: TLS handshake with %s: %w", addr, err)
	}
	if proto := tlsConn.ConnectionState().NegotiatedProtocol; proto != "h2" {
		_ = tlsConn.Close()
		return nil, fmt.Errorf("upstream: %s did not negotiate HTTP/2 (ALPN %q)", opts.Host, proto)
	}

	s := &Session{
		opts:           opts,
		conn:           tlsConn,
		framer:         http2.NewFramer(tlsConn, tlsConn),
		streams:        make(map[uint32]*stream),
		nextStreamID:   1,
		maxConcurrent:  int(^uint32(0) >> 1),
		slotSignal:     make(chan struct{}),
		closedCh:       make(chan struct{}),
		connSendWindow: newWindow(65535),
	}
	s.framer.ReadMetaHeaders = hpack.NewDecoder(4096, nil)
	s.hpackEnc = hpack.NewEncoder((*byteBuffer)(&s.hpackBuf))
	s.peerMaxFrameSize.Store(16384)
	s.peerInitialWin = 65535

	if err := s.writePreface(); err != nil {
		_ = tlsConn.Close()
		return nil, fmt.Errorf("upstream: session preface: %w", err)
	}
	s.touch()
	go s.readLoop()
	go s.keepaliveLoop()
	return s, nil
}

// byteBuffer adapts []byte to io.Writer for hpack.Encoder (which requires an
// io.Writer at construction but is written to synchronously).
type byteBuffer []byte

func (b *byteBuffer) Write(p []byte) (int, error) {
	*b = append(*b, p...)
	return len(p), nil
}

func (s *Session) writePreface() error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if _, err := io.WriteString(s.conn, http2Preface); err != nil {
		return err
	}
	if err := s.framer.WriteSettings(
		http2.Setting{ID: http2.SettingInitialWindowSize, Val: defaultInitialWindow},
		http2.Setting{ID: http2.SettingMaxFrameSize, Val: defaultMaxFrameSize},
		http2.Setting{ID: http2.SettingEnablePush, Val: 0},
	); err != nil {
		return err
	}
	// SETTINGS_INITIAL_WINDOW_SIZE applies to streams only; widen the
	// connection window from its fixed 65535-byte default.
	return s.framer.WriteWindowUpdate(0, defaultInitialWindow-65535)
}

// Close tears the session (and every open flow) down.
func (s *Session) Close() error {
	s.fail(ErrSessionClosed)
	return nil
}

// IsConnected reports whether the session can accept new streams.
func (s *Session) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && !s.draining
}

// Err returns the terminal session error, if any.
func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Totals returns the total DATA payload bytes written and read.
// RemoteAddr returns the edge TCP address (used by the daemon's edge-IP
// cache so redials can skip DNS).
func (s *Session) RemoteAddr() net.Addr { return s.conn.RemoteAddr() }

func (s *Session) Totals() (up, down int64) {
	return s.totalUp.Load(), s.totalDown.Load()
}

// OpenStream opens one CONNECT flow to host:port. The proxy pass is read at
// open time, so a swapped token applies to subsequent flows.
func (s *Session) OpenStream(ctx context.Context, host string, port int) (net.Conn, error) {
	authority := net.JoinHostPort(host, strconv.Itoa(port))
	if err := s.acquireSlot(ctx, authority); err != nil {
		return nil, err
	}

	s.mu.Lock()
	if s.closed {
		err := s.err
		s.mu.Unlock()
		s.releaseSlot()
		if err == nil {
			err = ErrSessionClosed
		}
		return nil, err
	}
	if s.draining {
		s.mu.Unlock()
		s.releaseSlot()
		return nil, ErrDraining
	}
	id := s.nextStreamID
	s.nextStreamID += 2
	st := newStream(s, id, authority, s.peerInitialWin)
	s.streams[id] = st
	s.mu.Unlock()

	if err := s.writeHeaders(id, authority); err != nil {
		s.removeStream(st)
		return nil, err
	}

	timer := time.NewTimer(s.opts.OpenStreamTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		s.removeStream(st)
		return nil, ctx.Err()
	case <-timer.C:
		s.removeStream(st)
		return nil, &ConnectTimeout{Authority: authority, Err: errors.New("no CONNECT response from the edge")}
	case err := <-st.resp:
		if err != nil {
			s.removeStream(st)
			return nil, err
		}
		return st, nil
	}
}

func (s *Session) lookup(id uint32) *stream {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.streams[id]
}

// removeStream deletes a stream, releases its concurrency slot exactly once
// and refunds any unread inbound credit to the connection window.
func (s *Session) removeStream(st *stream) {
	s.mu.Lock()
	delete(s.streams, st.id)
	release := !st.slotDone
	st.slotDone = true
	s.mu.Unlock()
	if release {
		s.releaseSlot()
	}

	st.mu.Lock()
	refund := st.received - st.credited
	st.credited = st.received
	st.mu.Unlock()
	if refund > 0 {
		s.sendConnWindowUpdate(int(refund))
	}
}

// creditInbound returns flow-control credit after the application reads n
// bytes (connection level always; stream level while the flow is open).
func (s *Session) creditInbound(streamID uint32, n int) {
	if n <= 0 {
		return
	}
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if err := s.framer.WriteWindowUpdate(0, uint32(n)); err != nil {
		return
	}
	s.mu.Lock()
	_, open := s.streams[streamID]
	s.mu.Unlock()
	if open {
		_ = s.framer.WriteWindowUpdate(streamID, uint32(n))
	}
}

func (s *Session) sendConnWindowUpdate(n int) {
	if n <= 0 || s.isClosed() {
		return
	}
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	_ = s.framer.WriteWindowUpdate(0, uint32(n))
}

func (s *Session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Session) acquireSlot(ctx context.Context, authority string) error {
	deadline := time.Now().Add(s.opts.StreamSlotTimeout)
	for {
		s.mu.Lock()
		if s.closed {
			err := s.err
			s.mu.Unlock()
			if err == nil {
				err = ErrSessionClosed
			}
			return err
		}
		if s.draining {
			s.mu.Unlock()
			return ErrDraining
		}
		if s.activeStreams < s.maxConcurrent {
			s.activeStreams++
			s.mu.Unlock()
			return nil
		}
		ch := s.slotSignal
		s.mu.Unlock()

		timer := time.NewTimer(time.Until(deadline))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
			return &ConnectTimeout{Authority: authority, Err: errors.New("timed out waiting for a free concurrent-stream slot")}
		case <-ch:
			timer.Stop()
		}
	}
}

func (s *Session) releaseSlot() {
	s.mu.Lock()
	if s.activeStreams > 0 {
		s.activeStreams--
	}
	s.wakeSlotsLocked()
	s.mu.Unlock()
}

func (s *Session) wakeSlotsLocked() {
	close(s.slotSignal)
	s.slotSignal = make(chan struct{})
}

func (s *Session) fail(err error) {
	s.closeOnce.Do(func() {
		if err == nil {
			err = ErrSessionClosed
		}
		s.mu.Lock()
		s.closed = true
		if s.err == nil {
			s.err = err
		}
		streams := make([]*stream, 0, len(s.streams))
		for _, st := range s.streams {
			streams = append(streams, st)
		}
		s.wakeSlotsLocked()
		s.mu.Unlock()

		close(s.closedCh)
		_ = s.conn.Close()
		s.connSendWindow.shut()
		for _, st := range streams {
			st.sendWindow.shut()
			st.appendError(fmt.Errorf("upstream: session down: %w", err))
		}
	})
}

func (s *Session) touch() {
	s.lastActivityNs.Store(time.Now().UnixNano())
	s.mu.Lock()
	s.pingOutstanding = false
	s.mu.Unlock()
}

func (s *Session) idleFor() time.Duration {
	last := s.lastActivityNs.Load()
	if last == 0 {
		return 0
	}
	return time.Since(time.Unix(0, last))
}

func (s *Session) maxSendFrameSize() uint32 {
	return s.peerMaxFrameSize.Load()
}

// writeHeaders encodes and sends the CONNECT request for one stream.
func (s *Session) writeHeaders(id uint32, authority string) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()

	s.hpackBuf = s.hpackBuf[:0]
	fields := []hpack.HeaderField{
		{Name: ":method", Value: "CONNECT"},
		{Name: ":authority", Value: authority},
		{Name: "proxy-authorization", Value: "Bearer " + s.opts.Pass()},
	}
	for _, f := range fields {
		if err := s.hpackEnc.WriteField(f); err != nil {
			return fmt.Errorf("upstream: encode CONNECT headers: %w", err)
		}
	}
	if err := s.framer.WriteHeaders(http2.HeadersFrameParam{
		StreamID:      id,
		BlockFragment: s.hpackBuf,
		EndHeaders:    true,
	}); err != nil {
		return fmt.Errorf("upstream: write CONNECT headers: %w", err)
	}
	return nil
}

func (s *Session) writeData(id uint32, p []byte) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if err := s.framer.WriteData(id, false, p); err != nil {
		return fmt.Errorf("upstream: write DATA: %w", err)
	}
	s.totalUp.Add(int64(len(p)))
	return nil
}

func (s *Session) writeEmptyDataEndStream(id uint32) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.framer.WriteData(id, true, nil)
}

func (s *Session) writeRST(id uint32, code http2.ErrCode) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.framer.WriteRSTStream(id, code)
}

// -- frame read loop ---------------------------------------------------------

func (s *Session) readLoop() {
	for {
		frame, err := s.framer.ReadFrame()
		if err != nil {
			s.fail(fmt.Errorf("read frame: %w", err))
			return
		}
		s.touch()
		switch f := frame.(type) {
		case *http2.SettingsFrame:
			if f.IsAck() {
				continue
			}
			s.applySettings(f)
			s.writeControl(func() error { return s.framer.WriteSettingsAck() })
		case *http2.WindowUpdateFrame:
			s.applyWindowUpdate(f)
		case *http2.PingFrame:
			if f.IsAck() {
				continue
			}
			data := f.Data
			s.writeControl(func() error { return s.framer.WritePing(true, data) })
		case *http2.GoAwayFrame:
			s.handleGoAway(f)
		case *http2.MetaHeadersFrame:
			s.handleHeaders(f)
		case *http2.DataFrame:
			s.handleData(f)
		case *http2.RSTStreamFrame:
			s.handleRST(f)
		default:
			// PRIORITY, PUSH_PROMISE (rejected via SETTINGS) and unknown
			// frame types are ignored.
		}
	}
}

// writeControl writes a control frame, ignoring failures (the read loop
// notices a dead connection and fails the session).
func (s *Session) writeControl(fn func() error) {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	_ = fn()
}

func (s *Session) applySettings(f *http2.SettingsFrame) {
	var newInitial int64
	haveInitial := false
	_ = f.ForeachSetting(func(setting http2.Setting) error {
		switch setting.ID {
		case http2.SettingInitialWindowSize:
			newInitial = int64(setting.Val)
			haveInitial = true
		case http2.SettingMaxFrameSize:
			s.peerMaxFrameSize.Store(setting.Val)
		case http2.SettingMaxConcurrentStreams:
			s.setMaxConcurrent(setting.Val)
		}
		return nil
	})

	s.mu.Lock()
	if !haveInitial || newInitial == s.peerInitialWin {
		s.mu.Unlock()
		return
	}
	delta := newInitial - s.peerInitialWin
	s.peerInitialWin = newInitial
	streams := make([]*stream, 0, len(s.streams))
	for _, st := range s.streams {
		streams = append(streams, st)
	}
	s.mu.Unlock()
	for _, st := range streams {
		st.sendWindow.add(delta)
	}
}

func (s *Session) setMaxConcurrent(v uint32) {
	// RFC 9113 §6.5.2: 0 means the peer will not process new streams; like
	// the reference client we keep at least one slot so the queue drains
	// when a later SETTINGS raises the limit again.
	n := int(v)
	if n < 1 {
		n = 1
	}
	s.mu.Lock()
	if n != s.maxConcurrent {
		s.maxConcurrent = n
		s.wakeSlotsLocked()
	}
	s.mu.Unlock()
}

func (s *Session) applyWindowUpdate(f *http2.WindowUpdateFrame) {
	if f.StreamID == 0 {
		s.connSendWindow.add(int64(f.Increment))
		return
	}
	if st := s.lookup(f.StreamID); st != nil {
		st.sendWindow.add(int64(f.Increment))
	}
}

func (s *Session) handleHeaders(f *http2.MetaHeadersFrame) {
	st := s.lookup(f.StreamID)
	if st == nil {
		return
	}
	if !st.hasResp() {
		status := 0
		for _, hf := range f.Fields {
			if hf.Name == ":status" {
				status, _ = strconv.Atoi(hf.Value)
				break
			}
		}
		if status < 200 || status > 299 {
			rej := &ConnectRejected{Status: status, Authority: st.authority}
			st.signalResp(rej)
			st.appendError(rej)
			if s.opts.Logf != nil {
				s.opts.Logf(logging.Debug, "upstream", "CONNECT %s rejected: HTTP %d", st.authority, status)
			}
			if f.StreamEnded() {
				st.appendEOF()
			}
			return
		}
		st.signalResp(nil)
	}
	if f.StreamEnded() {
		st.appendEOF()
	}
}

func (s *Session) handleData(f *http2.DataFrame) {
	data := f.Data()
	s.totalDown.Add(int64(len(data)))
	st := s.lookup(f.StreamID)
	if st == nil {
		// The flow is already gone; return the connection-level credit so
		// the inbound window stays balanced.
		s.sendConnWindowUpdate(len(data))
		return
	}
	if len(data) > 0 {
		st.appendData(data)
	}
	if f.StreamEnded() {
		st.appendEOF()
	}
}

func (s *Session) handleRST(f *http2.RSTStreamFrame) {
	st := s.lookup(f.StreamID)
	if st == nil {
		return
	}
	var err error
	switch f.ErrCode {
	case http2.ErrCodeRefusedStream:
		err = ErrStreamRefused
	case http2.ErrCodeCancel:
		err = io.ErrClosedPipe
	default:
		err = fmt.Errorf("upstream: stream reset (code %d)", f.ErrCode)
	}
	st.appendError(err)
}

func (s *Session) handleGoAway(f *http2.GoAwayFrame) {
	s.mu.Lock()
	s.draining = true
	last := f.LastStreamID
	streams := make([]*stream, 0, len(s.streams))
	for _, st := range s.streams {
		streams = append(streams, st)
	}
	s.wakeSlotsLocked()
	s.mu.Unlock()

	if s.opts.Logf != nil {
		s.opts.Logf(logging.Warn, "upstream", "edge sent GOAWAY (code %d, last stream %d); draining", f.ErrCode, last)
	}
	for _, st := range streams {
		if st.id > last {
			st.appendError(fmt.Errorf("upstream: %w", ErrDraining))
		}
	}
	go s.drainWatcher()
}

// drainWatcher closes the session once a GOAWAY'd session is idle.
func (s *Session) drainWatcher() {
	for {
		select {
		case <-s.closedCh:
			return
		case <-time.After(5 * time.Second):
		}
		s.mu.Lock()
		active := len(s.streams)
		s.mu.Unlock()
		if active == 0 {
			s.fail(errors.New("upstream: drained after GOAWAY"))
			return
		}
		if s.idleFor() > 2*time.Minute {
			s.fail(errors.New("upstream: GOAWAY drain idle timeout"))
			return
		}
	}
}

func (s *Session) keepaliveLoop() {
	ticker := time.NewTicker(s.opts.KeepaliveCheck)
	defer ticker.Stop()
	for {
		select {
		case <-s.closedCh:
			return
		case <-ticker.C:
		}
		s.mu.Lock()
		awaiting := s.pingOutstanding
		sentAt := s.pingSentAt
		s.mu.Unlock()

		if awaiting {
			if time.Since(sentAt) >= s.opts.KeepaliveTimeout {
				s.fail(errors.New("upstream: keepalive ping went unanswered"))
				return
			}
			continue
		}
		if s.idleFor() >= s.opts.KeepaliveIdle {
			s.mu.Lock()
			if !s.pingOutstanding {
				s.pingOutstanding = true
				s.pingSentAt = time.Now()
			}
			s.mu.Unlock()
			var payload [8]byte
			copy(payload[:], keepalivePingPayload)
			s.writeControl(func() error { return s.framer.WritePing(false, payload) })
		}
	}
}
