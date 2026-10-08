package upstream

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

const testEdgeHost = "edge.test"

type serverEvent struct {
	Kind      string // connect|data|endstream|ping|ping_ack|rst|window_update|goaway
	StreamID  uint32
	Data      []byte
	Authority string
	Auth      string
	Ping      []byte
	Status    int
}

type serverHooks struct {
	// accept returns the CONNECT status (0 means 200, -1 means "hold the
	// response until respond() is called").
	accept          func(authority, auth string) int
	dropPingAcks    bool
	echo            bool
	autoCredit      bool
	creditThreshold int // bytes accumulated before WINDOW_UPDATEs are sent
}

type testStream struct {
	id          uint32
	recv        []byte
	clientEnded bool
}

type testServer struct {
	t       *testing.T
	ln      net.Listener
	addr    string
	port    int
	rootCAs *x509.CertPool
	closed  chan struct{}
	ready   chan struct{}

	mu        sync.Mutex
	hooks     serverHooks
	events    []serverEvent
	streams   map[uint32]*testStream
	creditAcc map[uint32]int

	conn     net.Conn
	framer   *http2.Framer
	sendMu   sync.Mutex
	hpackEnc *hpack.Encoder
	hpackBuf []byte

	settings []http2.Setting
}

func startTestServer(t *testing.T, hooks serverHooks, settings ...http2.Setting) *testServer {
	t.Helper()
	cert, pool := newTestCert(t, testEdgeHost)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	srv := &testServer{
		t:         t,
		ln:        ln,
		addr:      ln.Addr().String(),
		port:      port,
		rootCAs:   pool,
		closed:    make(chan struct{}),
		ready:     make(chan struct{}),
		hooks:     hooks,
		streams:   make(map[uint32]*testStream),
		creditAcc: make(map[uint32]int),
		settings:  settings,
	}
	srv.hpackEnc = hpack.NewEncoder((*byteBuffer)(&srv.hpackBuf))
	go srv.serve(cert)
	return srv
}

func (s *testServer) stop() {
	_ = s.ln.Close()
	s.mu.Lock()
	if s.conn != nil {
		_ = s.conn.Close()
	}
	s.mu.Unlock()
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
}

func (s *testServer) dialOptions(t *testing.T, opts Options) Options {
	t.Helper()
	opts.Host = testEdgeHost
	opts.PinnedIP = "127.0.0.1"
	opts.Port = s.port
	opts.RootCAs = s.rootCAs
	return opts
}

func (s *testServer) serve(cert tls.Certificate) {
	conn, err := s.ln.Accept()
	if err != nil {
		return
	}
	tlsConn := tls.Server(conn, &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h2"},
	})
	if err := tlsConn.Handshake(); err != nil {
		s.t.Logf("test server handshake: %v", err)
		_ = conn.Close()
		return
	}
	if got := tlsConn.ConnectionState().NegotiatedProtocol; got != "h2" {
		s.t.Errorf("test server negotiated ALPN %q, want h2", got)
		_ = tlsConn.Close()
		return
	}

	s.mu.Lock()
	s.conn = tlsConn
	s.mu.Unlock()
	framer := http2.NewFramer(tlsConn, tlsConn)
	framer.ReadMetaHeaders = hpack.NewDecoder(4096, nil)
	s.framer = framer

	preface := make([]byte, len(http2Preface))
	if _, err := io.ReadFull(tlsConn, preface); err != nil {
		s.t.Logf("test server preface: %v", err)
		_ = tlsConn.Close()
		return
	}
	if string(preface) != http2Preface {
		s.t.Errorf("bad client preface %q", string(preface))
		_ = tlsConn.Close()
		return
	}
	s.sendSettings(s.settings...)
	close(s.ready)

	for {
		frame, err := framer.ReadFrame()
		if err != nil {
			return
		}
		s.handleFrame(frame)
	}
}

func (s *testServer) handleFrame(frame http2.Frame) {
	switch f := frame.(type) {
	case *http2.SettingsFrame:
		if f.IsAck() {
			return
		}
		s.sendMu.Lock()
		_ = s.framer.WriteSettingsAck()
		s.sendMu.Unlock()

	case *http2.MetaHeadersFrame:
		authority, auth := "", ""
		for _, hf := range f.Fields {
			switch hf.Name {
			case ":authority":
				authority = hf.Value
			case ":method":
				if hf.Value != "CONNECT" {
					s.t.Errorf("method = %s, want CONNECT", hf.Value)
				}
			case "proxy-authorization":
				auth = hf.Value
			}
		}
		s.mu.Lock()
		s.streams[f.StreamID] = &testStream{id: f.StreamID}
		accept := s.hooks.accept
		s.mu.Unlock()

		s.emit(serverEvent{Kind: "connect", StreamID: f.StreamID, Authority: authority, Auth: auth})

		status := 200
		if accept != nil {
			status = accept(authority, auth)
		}
		if status == -1 {
			return // held; the test calls respond()
		}
		if status == 0 {
			status = 200
		}
		if status >= 200 && status <= 299 {
			s.respond(f.StreamID, status)
		} else {
			s.respond(f.StreamID, status)
		}

	case *http2.DataFrame:
		data := f.Data()
		s.mu.Lock()
		st := s.streams[f.StreamID]
		if st != nil {
			st.recv = append(st.recv, data...)
			if f.StreamEnded() {
				st.clientEnded = true
			}
		}
		echo := s.hooks.echo
		autoCredit := s.hooks.autoCredit
		threshold := s.hooks.creditThreshold
		s.mu.Unlock()

		if len(data) > 0 {
			s.emit(serverEvent{Kind: "data", StreamID: f.StreamID, Data: data})
		}
		if f.StreamEnded() {
			s.emit(serverEvent{Kind: "endstream", StreamID: f.StreamID})
		}
		if autoCredit && len(data) > 0 {
			s.credit(f.StreamID, len(data), threshold)
		}
		if echo && len(data) > 0 {
			s.sendData(f.StreamID, data, false)
		}
		if echo && f.StreamEnded() {
			s.sendData(f.StreamID, nil, true)
		}

	case *http2.PingFrame:
		if f.IsAck() {
			s.emit(serverEvent{Kind: "ping_ack", Ping: append([]byte(nil), f.Data[:]...)})
			return
		}
		s.emit(serverEvent{Kind: "ping", Ping: append([]byte(nil), f.Data[:]...)})
		s.mu.Lock()
		drop := s.hooks.dropPingAcks
		s.mu.Unlock()
		if !drop {
			data := f.Data
			s.sendMu.Lock()
			_ = s.framer.WritePing(true, data)
			s.sendMu.Unlock()
		}

	case *http2.WindowUpdateFrame:
		s.emit(serverEvent{Kind: "window_update", StreamID: f.StreamID})

	case *http2.RSTStreamFrame:
		s.emit(serverEvent{Kind: "rst", StreamID: f.StreamID, Status: int(f.ErrCode)})

	case *http2.GoAwayFrame:
		s.emit(serverEvent{Kind: "goaway"})
	}
}

func (s *testServer) emit(ev serverEvent) {
	s.mu.Lock()
	s.events = append(s.events, ev)
	s.mu.Unlock()
}

// waitEvent returns the next event of the given kind, skipping others.
func (s *testServer) waitEvent(kind string, timeout time.Duration) (serverEvent, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		for i, ev := range s.events {
			if ev.Kind == kind {
				s.events = append(s.events[:i], s.events[i+1:]...)
				s.mu.Unlock()
				return ev, true
			}
		}
		s.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	return serverEvent{}, false
}

func (s *testServer) respond(id uint32, status int) {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	s.hpackBuf = s.hpackBuf[:0]
	_ = s.hpackEnc.WriteField(hpack.HeaderField{Name: ":status", Value: fmt.Sprintf("%d", status)})
	endStream := status < 200 || status > 299
	_ = s.framer.WriteHeaders(http2.HeadersFrameParam{
		StreamID:      id,
		BlockFragment: s.hpackBuf,
		EndHeaders:    true,
		EndStream:     endStream,
	})
}

func (s *testServer) sendData(id uint32, data []byte, endStream bool) {
	s.sendMu.Lock()
	_ = s.framer.WriteData(id, endStream, data)
	s.sendMu.Unlock()
}

func (s *testServer) sendSettings(settings ...http2.Setting) {
	s.sendMu.Lock()
	_ = s.framer.WriteSettings(settings...)
	s.sendMu.Unlock()
}

func (s *testServer) sendGoAway() {
	s.sendMu.Lock()
	_ = s.framer.WriteGoAway(0, http2.ErrCodeNo, nil)
	s.sendMu.Unlock()
}

func (s *testServer) sendPing(data []byte) {
	var payload [8]byte
	copy(payload[:], data)
	s.sendMu.Lock()
	_ = s.framer.WritePing(false, payload)
	s.sendMu.Unlock()
}

func (s *testServer) sendWindowUpdate(id uint32, n int) {
	s.sendMu.Lock()
	_ = s.framer.WriteWindowUpdate(id, uint32(n))
	s.sendMu.Unlock()
}

// credit sends WINDOW_UPDATEs once threshold bytes accumulate for a stream.
func (s *testServer) credit(id uint32, n, threshold int) {
	s.mu.Lock()
	s.creditAcc[id] += n
	send := 0
	if threshold <= 0 || s.creditAcc[id] >= threshold {
		send = s.creditAcc[id]
		s.creditAcc[id] = 0
	}
	s.mu.Unlock()
	if send > 0 {
		s.sendWindowUpdate(id, send)
		s.sendWindowUpdate(0, send)
	}
}

func (s *testServer) streamReceived(id uint32) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.streams[id]; st != nil {
		return append([]byte(nil), st.recv...)
	}
	return nil
}

func (s *testServer) streamEnded(id uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.streams[id]
	return st != nil && st.clientEnded
}

func newTestCert(t *testing.T, host string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(parsed)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: parsed}, pool
}

// waitPeerInitialWindow waits until the client has applied the server's
// SETTINGS (deterministic setup for flow-control tests).
func waitPeerInitialWindow(t *testing.T, s *Session, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		got := s.peerInitialWin
		s.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("peer initial window never became %d", want)
}
