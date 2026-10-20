package tlsplit

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"
)

func TestParseMode(t *testing.T) {
	tests := []struct {
		in   string
		want Mode
	}{
		{"off", Off},
		{"OFF", Off},
		{" off ", Off},
		{"first-byte", FirstByte},
		{" First-Byte\n", FirstByte},
		{"sni", SNI},
		{"\tSNI\t", SNI},
	}
	for _, tt := range tests {
		got, err := ParseMode(tt.in)
		if err != nil {
			t.Errorf("ParseMode(%q) error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseMode(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}

	for _, in := range []string{"", "   ", "\n", "middle", "first_byte", "first byte", "sni!", "offf"} {
		if _, err := ParseMode(in); err == nil {
			t.Errorf("ParseMode(%q) succeeded, want error", in)
		}
	}
}

func TestWrapOffAndUnknownMode(t *testing.T) {
	for _, mode := range []Mode{Off, Mode(99), Mode(-1)} {
		raw := &recordingConn{}
		if got := Wrap(raw, mode); got != raw {
			t.Fatalf("Wrap(conn, %v) = %T, want the original conn", mode, got)
		}
	}
}

func TestFirstByteSynthetic(t *testing.T) {
	hello := hostClientHello("example.com")
	rec := &recordingConn{}

	n, err := Wrap(rec, FirstByte).Write(hello)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(hello) {
		t.Fatalf("Write returned %d, want %d", n, len(hello))
	}
	assertWrites(t, rec, hello[:1], hello[1:])
}

func TestSNISynthetic(t *testing.T) {
	hello := hostClientHello("example.com")
	start := bytes.Index(hello, []byte("example.com"))
	if start < 1 || start+1 >= len(hello) {
		t.Fatalf("hostname offset %d is not an interior byte of a %d-byte hello", start, len(hello))
	}

	rec := &recordingConn{}
	n, err := Wrap(rec, SNI).Write(hello)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(hello) {
		t.Fatalf("Write returned %d, want %d", n, len(hello))
	}
	assertWrites(t, rec, hello[:start], hello[start:start+1], hello[start+1:])

	if got := rec.recorded()[1]; len(got) != 1 || got[0] != 'e' {
		t.Fatalf("split byte = %q, want %q", got, "e")
	}
}

func TestSNIExtensionScanning(t *testing.T) {
	host := "example.com"
	// An unrelated extension before server_name must be skipped, and the first
	// host_name entry wins even when other name types come first.
	hello := clientHelloRecord(
		tlsExtension(0x0010, []byte{0x00, 0x01, 0x00}),
		serverNameExtension(
			nameEntry{typ: 1, name: "ignored.invalid"},
			nameEntry{typ: 0, name: host},
		),
	)
	start := bytes.Index(hello, []byte(host))
	if start < 0 {
		t.Fatal("hostname not present in synthetic hello")
	}

	rec := &recordingConn{}
	if _, err := Wrap(rec, SNI).Write(hello); err != nil {
		t.Fatalf("Write: %v", err)
	}
	assertWrites(t, rec, hello[:start], hello[start:start+1], hello[start+1:])
}

func TestSNIOneByteHostname(t *testing.T) {
	hello := hostClientHello("a")
	start, ok := sniHostOffset(hello)
	if !ok {
		t.Fatal("parser did not find the one-byte hostname")
	}
	if start != len(hello)-1 {
		t.Fatalf("hostname offset = %d, want the final byte %d", start, len(hello)-1)
	}

	rec := &recordingConn{}
	n, err := Wrap(rec, SNI).Write(hello)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(hello) {
		t.Fatalf("Write returned %d, want %d", n, len(hello))
	}

	// The split tail is empty and therefore skipped: two writes, not three.
	writes := rec.recorded()
	if len(writes) != 2 {
		t.Fatalf("underlying writes = %d, want 2", len(writes))
	}
	assertWrites(t, rec, hello[:start], hello[start:])
	if writes[1][0] != 'a' {
		t.Fatalf("split byte = %q, want %q", writes[1], "a")
	}
	reassembled := append(append([]byte(nil), writes[0]...), writes[1]...)
	if !bytes.Equal(reassembled, hello) {
		t.Fatal("split writes do not reassemble the ClientHello")
	}
}

func TestSNIFallback(t *testing.T) {
	valid := hostClientHello("example.com")
	mutate := func(off int, b ...byte) []byte {
		p := append([]byte(nil), valid...)
		copy(p[off:], b)
		return p
	}
	emptyHost := clientHelloRecord(serverNameExtension(nameEntry{typ: 0, name: ""}))
	noServerName := clientHelloRecord(tlsExtension(0x0010, []byte{0x00, 0x01, 0x00}))
	otherNameType := clientHelloRecord(serverNameExtension(nameEntry{typ: 1, name: "example.com"}))

	tests := []struct {
		name string
		p    []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"five byte prefix", valid[:5]},
		{"tls header only", valid[:6]},
		{"truncated record", valid[:len(valid)-1]},
		{"wrong handshake type", mutate(5, 0x02)},
		{"record length overrun", mutate(3, 0xff, 0xff)},
		{"zero record length", mutate(3, 0x00, 0x00)},
		{"handshake length overrun", mutate(6, 0xff, 0xff, 0xff)},
		{"session id overrun", mutate(43, 0x7f)},
		{"cipher suites overrun", mutate(44, 0xff, 0xff)},
		{"compression overrun", mutate(48, 0xff)},
		{"extensions overrun", mutate(50, 0xff, 0xff)},
		{"extension header overrun", mutate(50, 0x00, 0x03)},
		{"extension data overrun", mutate(54, 0xff, 0xff)},
		{"server name list overrun", mutate(56, 0xff, 0xff)},
		{"hostname overrun", mutate(59, 0xff, 0xff)},
		{"missing extensions length", valid[:50]},
		{"empty hostname", emptyHost},
		{"no server_name extension", noServerName},
		{"non host_name entry only", otherNameType},
		{"non tls preface", []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recordingConn{}
			n, err := Wrap(rec, SNI).Write(tt.p)
			if err != nil {
				t.Fatalf("Write: %v", err)
			}
			if n != len(tt.p) {
				t.Fatalf("Write returned %d, want %d", n, len(tt.p))
			}
			if len(tt.p) == 0 {
				if got := rec.recorded(); len(got) != 0 {
					t.Fatalf("empty write made %d underlying calls, want 0", len(got))
				}
				return
			}
			assertWrites(t, rec, tt.p)
		})
	}
}

func TestEveryTruncatedPrefixFallsBack(t *testing.T) {
	hello := hostClientHello("example.com")
	for i := 0; i < len(hello); i++ {
		p := hello[:i]
		rec := &recordingConn{}
		n, err := Wrap(rec, SNI).Write(p)
		if err != nil {
			t.Fatalf("prefix %d: Write: %v", i, err)
		}
		if n != len(p) {
			t.Fatalf("prefix %d: Write returned %d, want %d", i, n, len(p))
		}
		want := 1
		if i == 0 {
			// An empty write consumes the split opportunity without touching
			// the underlying connection.
			want = 0
		}
		if got := rec.recorded(); len(got) != want {
			t.Fatalf("prefix %d: %d underlying writes, want %d", i, len(got), want)
		}
	}
}

func TestRealClientHello(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer clientSide.Close()
	defer serverSide.Close()

	rec := &recordingConn{Conn: clientSide}
	client := tls.Client(Wrap(rec, SNI), &tls.Config{ServerName: "example.com"})

	errCh := make(chan error, 1)
	go func() { errCh <- client.Handshake() }()

	if err := serverSide.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	hello := readTLSRecord(t, serverSide)
	serverSide.Close()
	<-errCh // the handshake cannot finish without a server; the error is irrelevant

	if len(hello) < 9 || hello[5] != 0x01 {
		t.Fatalf("captured record is not a ClientHello: % x", hello[:min(len(hello), 16)])
	}

	start := bytes.Index(hello, []byte("example.com"))
	if start < 0 {
		t.Fatal("SNI example.com not found in captured ClientHello")
	}
	if got, ok := sniHostOffset(hello); !ok || got != start {
		t.Fatalf("sniHostOffset = %d, %v; want %d, true", got, ok, start)
	}

	writes := rec.recorded()
	if len(writes) != 3 {
		t.Fatalf("underlying writes = %d, want 3", len(writes))
	}
	want := [][]byte{hello[:start], hello[start : start+1], hello[start+1:]}
	for i := range want {
		if !bytes.Equal(writes[i], want[i]) {
			t.Fatalf("write %d has %d bytes, want the %d-byte segment at offset %d", i, len(writes[i]), len(want[i]), start)
		}
	}
	if writes[1][0] != 'e' {
		t.Fatalf("split byte = %q, want %q", writes[1], "e")
	}
}

func TestHandshakeModes(t *testing.T) {
	cert, pool := testCertificate(t, "example.com")

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go echoServer(ln)

	for _, tt := range []struct {
		name string
		mode Mode
	}{
		{"off", Off},
		{"first-byte", FirstByte},
		{"sni", SNI},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := net.Dial("tcp", ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			client := tls.Client(Wrap(raw, tt.mode), &tls.Config{
				ServerName: "example.com",
				RootCAs:    pool,
				MinVersion: tls.VersionTLS12,
			})
			defer client.Close()

			if err := client.Handshake(); err != nil {
				t.Fatalf("handshake: %v", err)
			}
			if _, err := client.Write([]byte("ping")); err != nil {
				t.Fatalf("write: %v", err)
			}
			got := make([]byte, 4)
			if _, err := io.ReadFull(client, got); err != nil {
				t.Fatalf("read: %v", err)
			}
			if string(got) != "ping" {
				t.Fatalf("echo = %q, want %q", got, "ping")
			}
		})
	}
}

func TestNonTLSFirstWriteAndLaterWrites(t *testing.T) {
	preface := []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
	tlsLooking := []byte{0x16, 0x03, 0x01, 0x00, 0x2f, 0x01, 0x00, 0x00, 0x2b, 0x03, 0x03}

	for _, mode := range []Mode{FirstByte, SNI} {
		rec := &recordingConn{}
		conn := Wrap(rec, mode)

		if n, err := conn.Write(preface); err != nil || n != len(preface) {
			t.Fatalf("mode %v: first Write = %d, %v; want %d, nil", mode, n, err, len(preface))
		}
		assertWrites(t, rec, preface)

		if n, err := conn.Write(tlsLooking); err != nil || n != len(tlsLooking) {
			t.Fatalf("mode %v: second Write = %d, %v; want %d, nil", mode, n, err, len(tlsLooking))
		}
		assertWrites(t, rec, preface, tlsLooking)
	}
}

func TestEmptyFirstWriteConsumesSplit(t *testing.T) {
	rec := &recordingConn{}
	conn := Wrap(rec, FirstByte)

	if n, err := conn.Write(nil); err != nil || n != 0 {
		t.Fatalf("empty Write = %d, %v; want 0, nil", n, err)
	}
	if got := rec.recorded(); len(got) != 0 {
		t.Fatalf("empty Write made %d underlying calls, want 0", len(got))
	}

	hello := hostClientHello("example.com")
	if n, err := conn.Write(hello); err != nil || n != len(hello) {
		t.Fatalf("second Write = %d, %v; want %d, nil", n, err, len(hello))
	}
	// The empty first write consumes the split; the ClientHello passes as one.
	assertWrites(t, rec, hello)
}

func TestLaterWriteShortWritesComplete(t *testing.T) {
	preface := []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
	second := []byte("HELLO")
	conn := &scriptedConn{steps: []writeStep{
		{len(preface), nil},
		{len(second) - 1, nil},
		{1, nil},
	}}
	wrapped := Wrap(conn, FirstByte)

	if n, err := wrapped.Write(preface); err != nil || n != len(preface) {
		t.Fatalf("first Write = %d, %v; want %d, nil", n, err, len(preface))
	}
	n, err := wrapped.Write(second)
	if err != nil {
		t.Fatalf("later Write: %v", err)
	}
	if n != len(second) {
		t.Fatalf("later Write returned %d, want %d", n, len(second))
	}

	wantCalls := []int{len(preface), len(second), 1}
	if len(conn.calls) != len(wantCalls) {
		t.Fatalf("underlying calls = %d, want %d", len(conn.calls), len(wantCalls))
	}
	for i, want := range wantCalls {
		if len(conn.calls[i]) != want {
			t.Fatalf("call %d wrote a %d-byte slice, want %d", i, len(conn.calls[i]), want)
		}
	}
}

func TestWriteErrorAccounting(t *testing.T) {
	hello := hostClientHello("example.com")
	start := bytes.Index(hello, []byte("example.com"))
	boom := errors.New("boom")
	preface := []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
	malformed := hello[:len(hello)-1]

	type testCase struct {
		name      string
		mode      Mode
		p         []byte
		steps     []writeStep
		wantN     int
		wantErr   error
		wantCalls []int
	}

	tests := []testCase{
		{
			name: "first-byte first segment fails", mode: FirstByte, p: hello,
			steps: []writeStep{{0, boom}}, wantN: 0, wantErr: boom, wantCalls: []int{1},
		},
		{
			name: "first-byte second segment immediate failure", mode: FirstByte, p: hello,
			steps: []writeStep{{1, nil}, {0, boom}}, wantN: 1, wantErr: boom,
			wantCalls: []int{1, len(hello) - 1},
		},
		{
			name: "first-byte second segment partial failure", mode: FirstByte, p: hello,
			steps: []writeStep{{1, nil}, {3, boom}}, wantN: 4, wantErr: boom,
			wantCalls: []int{1, len(hello) - 1},
		},
		{
			name: "first-byte short writes complete", mode: FirstByte, p: hello,
			steps:     []writeStep{{1, nil}, {2, nil}, {len(hello) - 3, nil}},
			wantN:     len(hello),
			wantCalls: []int{1, len(hello) - 1, len(hello) - 3},
		},
		{
			name: "first-byte zero write without error", mode: FirstByte, p: hello,
			steps: []writeStep{{1, nil}, {0, nil}}, wantN: 1, wantErr: io.ErrShortWrite,
			wantCalls: []int{1, len(hello) - 1},
		},
		{
			name: "first-byte invalid write count", mode: FirstByte, p: hello,
			steps: []writeStep{{len(hello) + 1, nil}}, wantN: 0, wantErr: io.ErrShortWrite,
			wantCalls: []int{1},
		},
		{
			name: "first-byte full segment with error", mode: FirstByte, p: hello,
			steps: []writeStep{{1, nil}, {len(hello) - 1, boom}}, wantN: len(hello), wantErr: boom,
			wantCalls: []int{1, len(hello) - 1},
		},
		{
			name: "sni first segment fails", mode: SNI, p: hello,
			steps: []writeStep{{0, boom}}, wantN: 0, wantErr: boom, wantCalls: []int{start},
		},
		{
			name: "sni split byte fails", mode: SNI, p: hello,
			steps: []writeStep{{start, nil}, {0, boom}}, wantN: start, wantErr: boom,
			wantCalls: []int{start, 1},
		},
		{
			name: "sni tail partial failure", mode: SNI, p: hello,
			steps: []writeStep{{start, nil}, {1, nil}, {2, boom}}, wantN: start + 3, wantErr: boom,
			wantCalls: []int{start, 1, len(hello) - start - 1},
		},
		{
			name: "sni short writes complete", mode: SNI, p: hello,
			steps: []writeStep{
				{start - 1, nil},
				{1, nil},
				{1, nil},
				{1, nil},
				{len(hello) - start - 2, nil},
			},
			wantN: len(hello),
			wantCalls: []int{
				start, 1, 1, len(hello) - start - 1, len(hello) - start - 2,
			},
		},
		{
			name: "sni zero write without error", mode: SNI, p: hello,
			steps: []writeStep{{start, nil}, {0, nil}}, wantN: start, wantErr: io.ErrShortWrite,
			wantCalls: []int{start, 1},
		},
		{
			name: "first-byte non-tls short writes complete", mode: FirstByte, p: preface,
			steps: []writeStep{{len(preface) - 1, nil}, {1, nil}},
			wantN: len(preface), wantCalls: []int{len(preface), 1},
		},
		{
			name: "non-tls zero write without error", mode: FirstByte, p: preface,
			steps: []writeStep{{0, nil}}, wantN: 0, wantErr: io.ErrShortWrite,
			wantCalls: []int{len(preface)},
		},
		{
			name: "sni malformed fallback short writes complete", mode: SNI, p: malformed,
			steps: []writeStep{{len(malformed) - 1, nil}, {1, nil}},
			wantN: len(malformed), wantCalls: []int{len(malformed), 1},
		},
		{
			name: "fallback short then error", mode: SNI, p: preface,
			steps: []writeStep{{len(preface) - 1, nil}, {0, boom}},
			wantN: len(preface) - 1, wantErr: boom,
			wantCalls: []int{len(preface), 1},
		},
		{
			name: "fallback single write error", mode: SNI, p: preface,
			steps: []writeStep{{0, boom}}, wantN: 0, wantErr: boom, wantCalls: []int{len(preface)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := &scriptedConn{steps: tt.steps}
			n, err := Wrap(conn, tt.mode).Write(tt.p)
			if n != tt.wantN {
				t.Fatalf("Write returned %d bytes, want %d", n, tt.wantN)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Write error = %v, want %v", err, tt.wantErr)
			}
			if len(conn.calls) != len(tt.wantCalls) {
				t.Fatalf("underlying calls = %d, want %d", len(conn.calls), len(tt.wantCalls))
			}
			for i, want := range tt.wantCalls {
				if len(conn.calls[i]) != want {
					t.Fatalf("call %d wrote a %d-byte slice, want %d", i, len(conn.calls[i]), want)
				}
			}
		})
	}
}

func TestConcurrentWrites(t *testing.T) {
	rec := &recordingConn{}
	conn := Wrap(rec, FirstByte)
	payload := hostClientHello("example.com")

	const goroutines = 8
	start := make(chan struct{})
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			n, err := conn.Write(payload)
			if err != nil {
				errs <- err
				return
			}
			if n != len(payload) {
				errs <- errors.New("short write")
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Write: %v", err)
	}

	writes := rec.recorded()
	// Exactly one write is split in two; all others pass through as one call.
	if want := goroutines + 1; len(writes) != want {
		t.Fatalf("underlying writes = %d, want %d", len(writes), want)
	}
	total := 0
	for _, w := range writes {
		total += len(w)
	}
	if want := goroutines * len(payload); total != want {
		t.Fatalf("total bytes written = %d, want %d", total, want)
	}
}

// recordingConn records every Write and optionally forwards it to an
// underlying connection.
type recordingConn struct {
	net.Conn

	mu     sync.Mutex
	writes [][]byte
}

func (c *recordingConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.writes = append(c.writes, append([]byte(nil), p...))
	c.mu.Unlock()
	if c.Conn != nil {
		return c.Conn.Write(p)
	}
	return len(p), nil
}

func (c *recordingConn) recorded() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]byte, len(c.writes))
	copy(out, c.writes)
	return out
}

// writeStep is one scripted result for scriptedConn.Write.
type writeStep struct {
	n   int
	err error
}

// scriptedConn returns preprogrammed results from Write and records the
// lengths of the slices it was asked to write.
type scriptedConn struct {
	mu    sync.Mutex
	steps []writeStep
	calls [][]byte
}

func (c *scriptedConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, append([]byte(nil), p...))
	if len(c.steps) == 0 {
		return 0, errors.New("tlsplit test: unexpected Write")
	}
	step := c.steps[0]
	c.steps = c.steps[1:]
	return step.n, step.err
}

func (c *scriptedConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (c *scriptedConn) Close() error                     { return nil }
func (c *scriptedConn) LocalAddr() net.Addr              { return testAddr("local") }
func (c *scriptedConn) RemoteAddr() net.Addr             { return testAddr("remote") }
func (c *scriptedConn) SetDeadline(time.Time) error      { return nil }
func (c *scriptedConn) SetReadDeadline(time.Time) error  { return nil }
func (c *scriptedConn) SetWriteDeadline(time.Time) error { return nil }

type testAddr string

func (a testAddr) Network() string { return "test" }
func (a testAddr) String() string  { return string(a) }

func assertWrites(t *testing.T, rec *recordingConn, want ...[]byte) {
	t.Helper()
	got := rec.recorded()
	if len(got) != len(want) {
		lengths := make([]int, len(got))
		for i := range got {
			lengths[i] = len(got[i])
		}
		t.Fatalf("%d underlying writes with lengths %v, want %d", len(got), lengths, len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Fatalf("write %d = % x, want % x", i, got[i], want[i])
		}
	}
}

func readTLSRecord(t *testing.T, r io.Reader) []byte {
	t.Helper()
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		t.Fatalf("read record header: %v", err)
	}
	body := make([]byte, int(header[3])<<8|int(header[4]))
	if _, err := io.ReadFull(r, body); err != nil {
		t.Fatalf("read record body: %v", err)
	}
	return append(header[:], body...)
}

func echoServer(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			buf := make([]byte, 4)
			if _, err := io.ReadFull(conn, buf); err != nil {
				return
			}
			_, _ = conn.Write(buf)
		}()
	}
}

func testCertificate(t *testing.T, host string) (tls.Certificate, *x509.CertPool) {
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

type nameEntry struct {
	typ  byte
	name string
}

func tlsExtension(typ uint16, data []byte) []byte {
	ext := []byte{byte(typ >> 8), byte(typ), byte(len(data) >> 8), byte(len(data))}
	return append(ext, data...)
}

func serverNameExtension(entries ...nameEntry) []byte {
	var list []byte
	for _, e := range entries {
		list = append(list, e.typ, byte(len(e.name)>>8), byte(len(e.name)))
		list = append(list, e.name...)
	}
	data := []byte{byte(len(list) >> 8), byte(len(list))}
	data = append(data, list...)
	return tlsExtension(0, data)
}

func hostClientHello(host string) []byte {
	return clientHelloRecord(serverNameExtension(nameEntry{typ: 0, name: host}))
}

func clientHelloRecord(exts ...[]byte) []byte {
	body := []byte{0x03, 0x03} // client_version TLS 1.2
	body = append(body, bytes.Repeat([]byte{0x42}, 32)...)
	body = append(body, 0x00)                   // session id length
	body = append(body, 0x00, 0x02, 0x13, 0x01) // cipher suites
	body = append(body, 0x01, 0x00)             // compression methods

	var extensions []byte
	for _, ext := range exts {
		extensions = append(extensions, ext...)
	}
	body = append(body, byte(len(extensions)>>8), byte(len(extensions)))
	body = append(body, extensions...)

	handshake := []byte{0x01, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}
	handshake = append(handshake, body...)

	record := []byte{0x16, 0x03, 0x01, byte(len(handshake) >> 8), byte(len(handshake))}
	return append(record, handshake...)
}
