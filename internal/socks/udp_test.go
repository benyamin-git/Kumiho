package socks

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mdns "github.com/miekg/dns"

	"github.com/benyamin-git/kumiho/internal/logging"
)

func startUDPTestServer(t *testing.T, resolve func(context.Context, *mdns.Msg) (*mdns.Msg, error)) *Server {
	return startUDPTestServerLogf(t, resolve, nil)
}

func startUDPTestServerLogf(t *testing.T, resolve func(context.Context, *mdns.Msg) (*mdns.Msg, error), logf func(logging.Level, string, string, ...any)) *Server {
	t.Helper()
	s := &Server{
		Bind:             "127.0.0.1",
		Port:             0,
		MaxConns:         16,
		HandshakeTimeout: 2 * time.Second,
		HalfCloseDrain:   100 * time.Millisecond,
		OpenTimeout:      2 * time.Second,
		Dial: func(context.Context, string, int) (net.Conn, error) {
			return nil, errors.New("test: CONNECT is not expected")
		},
		Resolve: resolve,
		Logf:    logf,
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	return s
}

type socksReply struct {
	code byte
	atyp byte
	ip   net.IP
	port int
}

func openSOCKSControl(t *testing.T, addr string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write([]byte{socksVersion, 1, 0x00}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatal(err)
	}
	if reply[0] != socksVersion || reply[1] != 0x00 {
		t.Fatalf("greeting reply = %v", reply)
	}
	return conn
}

func requestUDPAssociate(t *testing.T, s *Server, targetPort int) (net.Conn, socksReply) {
	t.Helper()
	ctrl := openSOCKSControl(t, s.Addr().String())
	_ = ctrl.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := ctrl.Write(ipRequest(socksCmdUDPAssociate, net.IPv4(127, 0, 0, 1), targetPort)); err != nil {
		t.Fatal(err)
	}
	return ctrl, readSOCKSReply(t, ctrl)
}

func readSOCKSReply(t *testing.T, c net.Conn) socksReply {
	t.Helper()
	head := make([]byte, 4)
	if _, err := io.ReadFull(c, head); err != nil {
		t.Fatal(err)
	}
	if head[0] != socksVersion {
		t.Fatalf("reply version = %d", head[0])
	}
	length := 0
	switch head[3] {
	case socksAtypIPv4:
		length = net.IPv4len
	case socksAtypIPv6:
		length = net.IPv6len
	default:
		t.Fatalf("reply address type = %d", head[3])
	}
	rest := make([]byte, length+2)
	if _, err := io.ReadFull(c, rest); err != nil {
		t.Fatal(err)
	}
	return socksReply{
		code: head[1],
		atyp: head[3],
		ip:   net.IP(append([]byte(nil), rest[:length]...)),
		port: int(binary.BigEndian.Uint16(rest[length:])),
	}
}

func associateUDP(t *testing.T, s *Server, targetPort int) (net.Conn, *net.UDPAddr, *net.UDPConn) {
	t.Helper()
	ctrl, reply := requestUDPAssociate(t, s, targetPort)
	if reply.code != replySucceeded {
		t.Fatalf("UDP ASSOCIATE reply = %#x, want success", reply.code)
	}
	if reply.atyp != socksAtypIPv4 {
		t.Fatalf("UDP ASSOCIATE reply ATYP = %d, want IPv4", reply.atyp)
	}
	if reply.ip.To4() == nil || reply.ip.IsUnspecified() || reply.port == 0 {
		t.Fatalf("UDP ASSOCIATE relay address = %s:%d, want a bound IPv4 address", reply.ip, reply.port)
	}
	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	return ctrl, &net.UDPAddr{IP: reply.ip, Port: reply.port}, client
}

func dnsQuery(id uint16, name string) *mdns.Msg {
	q := new(mdns.Msg)
	q.SetQuestion(name, mdns.TypeA)
	q.Id = id
	return q
}

func udpPayloadPacket(dstPort int, payload []byte) []byte {
	pkt := []byte{0, 0, 0}
	pkt = appendSocksAddr(pkt, net.IPv4(127, 0, 0, 1), dstPort)
	return append(pkt, payload...)
}

func udpQueryPacket(t *testing.T, dstPort int, q *mdns.Msg) []byte {
	t.Helper()
	packed, err := q.Pack()
	if err != nil {
		t.Fatal(err)
	}
	return udpPayloadPacket(dstPort, packed)
}

func echoDNS(calls *atomic.Int32) func(context.Context, *mdns.Msg) (*mdns.Msg, error) {
	return func(_ context.Context, req *mdns.Msg) (*mdns.Msg, error) {
		calls.Add(1)
		resp := new(mdns.Msg)
		resp.SetReply(req)
		// Deliberately wrong: the relay must overwrite it with the query ID.
		resp.Id = req.Id ^ 0xFFFF
		if len(req.Question) == 1 {
			resp.Answer = append(resp.Answer, &mdns.A{
				Hdr: mdns.RR_Header{Name: req.Question[0].Name, Rrtype: mdns.TypeA, Class: mdns.ClassINET, Ttl: 60},
				A:   net.ParseIP("93.184.216.34").To4(),
			})
		}
		return resp, nil
	}
}

type udpReplyPacket struct {
	msg     *mdns.Msg
	srcIP   net.IP
	srcPort int
}

func readUDPResponse(t *testing.T, conn *net.UDPConn, timeout time.Duration) (*udpReplyPacket, bool) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, udpAssocBufferSize)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return nil, false
		}
		t.Fatalf("read UDP response: %v", err)
	}
	if n < 4 || buf[0] != 0 || buf[1] != 0 || buf[2] != 0 {
		t.Fatalf("invalid UDP response header: % x", buf[:min(n, 4)])
	}
	off := 4
	switch buf[3] {
	case socksAtypIPv4:
		off += net.IPv4len
	case socksAtypIPv6:
		off += net.IPv6len
	default:
		t.Fatalf("response address type = %d", buf[3])
	}
	if n < off+2 {
		t.Fatalf("short UDP response (%d bytes)", n)
	}
	srcIP := net.IP(append([]byte(nil), buf[4:off]...))
	srcPort := int(binary.BigEndian.Uint16(buf[off:]))
	off += 2
	var msg mdns.Msg
	if err := msg.Unpack(buf[off:n]); err != nil {
		t.Fatalf("unpack DNS response: %v", err)
	}
	return &udpReplyPacket{msg: &msg, srcIP: srcIP, srcPort: srcPort}, true
}

func waitForNoAssociations(t *testing.T, s *Server) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		s.mu.Lock()
		n := len(s.udpAssocs)
		s.mu.Unlock()
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d UDP association(s) still registered", n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestUDPAssociateRelaysDNS(t *testing.T) {
	var calls atomic.Int32
	s := startUDPTestServer(t, echoDNS(&calls))
	ctrl, relay, client := associateUDP(t, s, 0)
	defer ctrl.Close()
	defer client.Close()

	query := dnsQuery(0x1234, "example.com.")
	if _, err := client.WriteToUDP(udpQueryPacket(t, 53, query), relay); err != nil {
		t.Fatal(err)
	}
	reply, ok := readUDPResponse(t, client, 2*time.Second)
	if !ok {
		t.Fatal("no DNS response through the UDP relay")
	}
	if calls.Load() != 1 {
		t.Fatalf("resolver calls = %d, want 1", calls.Load())
	}
	if reply.msg.Id != query.Id {
		t.Fatalf("response ID = %#x, want %#x", reply.msg.Id, query.Id)
	}
	if len(reply.msg.Question) != 1 || reply.msg.Question[0].Name != "example.com." {
		t.Fatalf("response question = %+v", reply.msg.Question)
	}
	if len(reply.msg.Answer) != 1 {
		t.Fatalf("response answers = %+v", reply.msg.Answer)
	}
	local := client.LocalAddr().(*net.UDPAddr)
	if !reply.srcIP.Equal(local.IP) || reply.srcPort != local.Port {
		t.Fatalf("response source = %s:%d, want the client %s", reply.srcIP, reply.srcPort, local)
	}
}

func TestUDPAssociateRefusals(t *testing.T) {
	t.Run("non-dns port", func(t *testing.T) {
		s := startUDPTestServer(t, echoDNS(new(atomic.Int32)))
		ctrl, reply := requestUDPAssociate(t, s, 80)
		defer ctrl.Close()
		if reply.code != replyNotAllowed {
			t.Fatalf("port 80 reply = %#x, want not allowed", reply.code)
		}
		s.mu.Lock()
		assocs := len(s.udpAssocs)
		s.mu.Unlock()
		if assocs != 0 {
			t.Fatalf("port 80 left %d UDP association(s) registered", assocs)
		}
	})

	t.Run("nil resolver", func(t *testing.T) {
		s := startUDPTestServer(t, nil)
		ctrl, reply := requestUDPAssociate(t, s, 53)
		defer ctrl.Close()
		if reply.code != replyCmdUnsupported {
			t.Fatalf("nil resolver reply = %#x, want command unsupported", reply.code)
		}
	})
}

func TestUDPAssociateOnePerPeerIP(t *testing.T) {
	s := startUDPTestServer(t, echoDNS(new(atomic.Int32)))

	ctrl, _, client := associateUDP(t, s, 0)
	defer client.Close()

	second, reply := requestUDPAssociate(t, s, 0)
	if reply.code != replyNotAllowed {
		_ = second.Close()
		t.Fatalf("second association reply = %#x, want not allowed", reply.code)
	}
	_ = second.Close()

	_ = ctrl.Close()
	waitForNoAssociations(t, s)

	third, _, thirdClient := associateUDP(t, s, 0)
	defer third.Close()
	defer thirdClient.Close()
}

func TestUDPAssociateSourcePredicate(t *testing.T) {
	a := &udpAssociation{srv: &Server{}, clientIP: net.ParseIP("127.0.0.1")}
	if !a.validSource(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4444}) {
		t.Fatal("the TCP peer must be accepted when no port was requested")
	}
	if a.validSource(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 4444}) {
		t.Fatal("a foreign source address must be rejected")
	}
	a.clientPort = 53
	if !a.validSource(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53}) {
		t.Fatal("the requested source port must be accepted")
	}
	if a.validSource(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4444}) {
		t.Fatal("a different source port must be rejected")
	}
}

func TestUDPAssociateWrongSourcePortDropped(t *testing.T) {
	var calls atomic.Int32
	s := startUDPTestServer(t, echoDNS(&calls))

	ctrl, relay, client := associateUDP(t, s, 53)
	defer ctrl.Close()
	defer client.Close()

	if local := client.LocalAddr().(*net.UDPAddr); local.Port == 53 {
		t.Skip("the test socket unexpectedly bound source port 53")
	}
	if _, err := client.WriteToUDP(udpQueryPacket(t, 53, dnsQuery(1, "port.example.com.")), relay); err != nil {
		t.Fatal(err)
	}
	if reply, ok := readUDPResponse(t, client, 250*time.Millisecond); ok {
		t.Fatalf("datagram from the wrong source port was answered: %+v", reply.msg)
	}
	if calls.Load() != 0 {
		t.Fatalf("resolver calls = %d, want 0 for the wrong source port", calls.Load())
	}

	// A request naming port 0 accepts any source port. The first association
	// must close first: only one association per peer IP is allowed.
	_ = ctrl.Close()
	_ = client.Close()
	waitForNoAssociations(t, s)

	ctrl2, relay2, client2 := associateUDP(t, s, 0)
	defer ctrl2.Close()
	defer client2.Close()
	if _, err := client2.WriteToUDP(udpQueryPacket(t, 53, dnsQuery(2, "any.example.com.")), relay2); err != nil {
		t.Fatal(err)
	}
	if _, ok := readUDPResponse(t, client2, 2*time.Second); !ok {
		t.Fatal("port 0 association did not accept an ephemeral source port")
	}
	if calls.Load() != 1 {
		t.Fatalf("resolver calls = %d, want 1", calls.Load())
	}
}

func TestUDPAssociateForeignSourceDropped(t *testing.T) {
	foreign, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 0})
	if err != nil {
		t.Skipf("cannot bind 127.0.0.2 on this platform: %v", err)
	}
	defer foreign.Close()

	var calls atomic.Int32
	s := startUDPTestServer(t, echoDNS(&calls))
	ctrl, relay, client := associateUDP(t, s, 0)
	defer ctrl.Close()
	defer client.Close()

	if _, err := foreign.WriteToUDP(udpQueryPacket(t, 53, dnsQuery(1, "foreign.example.com.")), relay); err != nil {
		t.Fatal(err)
	}
	if reply, ok := readUDPResponse(t, foreign, 250*time.Millisecond); ok {
		t.Fatalf("foreign-source datagram was answered: %+v", reply.msg)
	}
	if calls.Load() != 0 {
		t.Fatalf("resolver calls = %d, want 0 for a foreign source", calls.Load())
	}

	// The association stays healthy for the real peer.
	if _, err := client.WriteToUDP(udpQueryPacket(t, 53, dnsQuery(2, "peer.example.com.")), relay); err != nil {
		t.Fatal(err)
	}
	if _, ok := readUDPResponse(t, client, 2*time.Second); !ok {
		t.Fatal("the peer's query was not answered after a foreign datagram")
	}
	if calls.Load() != 1 {
		t.Fatalf("resolver calls = %d, want 1", calls.Load())
	}
}

func TestUDPAssociateDropsInvalidDatagrams(t *testing.T) {
	validQuery := dnsQuery(7, "drop.example.com.")
	valid := udpQueryPacket(t, 53, validQuery)
	withByte := func(index int, value byte) []byte {
		pkt := append([]byte(nil), valid...)
		pkt[index] = value
		return pkt
	}

	cases := []struct {
		name string
		pkt  []byte
	}{
		{"reserved bytes", withByte(1, 1)},
		{"fragmentation", withByte(2, 1)},
		{"non-dns destination port", udpQueryPacket(t, 80, validQuery)},
		{"malformed dns", udpPayloadPacket(53, []byte{0x12, 0x34, 0x01})},
		{"non-dns payload", udpPayloadPacket(53, []byte("not a dns message"))},
		{"short header", []byte{0, 0}},
		{"bad address type", []byte{0, 0, 0, 9, 0, 53}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			s := startUDPTestServer(t, echoDNS(&calls))
			ctrl, relay, client := associateUDP(t, s, 0)
			defer ctrl.Close()
			defer client.Close()

			if _, err := client.WriteToUDP(tc.pkt, relay); err != nil {
				t.Fatal(err)
			}
			if reply, ok := readUDPResponse(t, client, 200*time.Millisecond); ok {
				t.Fatalf("invalid datagram was answered: ID %#x rcode %d", reply.msg.Id, reply.msg.Rcode)
			}
			if calls.Load() != 0 {
				t.Fatalf("resolver calls = %d, want 0 for an invalid datagram", calls.Load())
			}
		})
	}
}

func TestUDPAssociateResolverFailureAnswersSERVFAIL(t *testing.T) {
	cases := []struct {
		name    string
		resolve func(context.Context, *mdns.Msg) (*mdns.Msg, error)
	}{
		{
			name: "resolver error",
			resolve: func(context.Context, *mdns.Msg) (*mdns.Msg, error) {
				return nil, errors.New("test: resolver unavailable")
			},
		},
		{
			name: "nil response",
			resolve: func(context.Context, *mdns.Msg) (*mdns.Msg, error) {
				return nil, nil
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			resolve := tc.resolve
			s := startUDPTestServer(t, func(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error) {
				calls.Add(1)
				return resolve(ctx, req)
			})
			ctrl, relay, client := associateUDP(t, s, 0)
			defer ctrl.Close()
			defer client.Close()

			query := dnsQuery(0xBEEF, "fail.example.com.")
			if _, err := client.WriteToUDP(udpQueryPacket(t, 53, query), relay); err != nil {
				t.Fatal(err)
			}
			reply, ok := readUDPResponse(t, client, 2*time.Second)
			if !ok {
				t.Fatal("no SERVFAIL response")
			}
			if calls.Load() != 1 {
				t.Fatalf("resolver calls = %d, want 1", calls.Load())
			}
			if reply.msg.Id != query.Id {
				t.Fatalf("SERVFAIL ID = %#x, want %#x", reply.msg.Id, query.Id)
			}
			if reply.msg.Rcode != mdns.RcodeServerFailure {
				t.Fatalf("rcode = %d, want SERVFAIL", reply.msg.Rcode)
			}
			if len(reply.msg.Question) != 1 || reply.msg.Question[0].Name != "fail.example.com." {
				t.Fatalf("SERVFAIL question = %+v", reply.msg.Question)
			}
		})
	}
}

func TestUDPAssociateInflightCap(t *testing.T) {
	const total = 40
	release := make(chan struct{})
	started := make(chan struct{}, total)
	var active, peak atomic.Int32
	var calls atomic.Int32
	resolve := func(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error) {
		calls.Add(1)
		n := active.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		active.Add(-1)
		resp := new(mdns.Msg)
		resp.SetReply(req)
		return resp, nil
	}

	var drops atomic.Int32
	s := startUDPTestServerLogf(t, resolve, func(_ logging.Level, tag, format string, _ ...any) {
		if tag == "socks" && strings.Contains(format, "in-flight query limit") {
			drops.Add(1)
		}
	})
	ctrl, relay, client := associateUDP(t, s, 0)
	defer ctrl.Close()
	defer client.Close()

	for i := 0; i < total; i++ {
		query := dnsQuery(uint16(i+1), "cap.example.com.")
		if _, err := client.WriteToUDP(udpQueryPacket(t, 53, query), relay); err != nil {
			t.Fatal(err)
		}
	}

	deadline := time.Now().Add(5 * time.Second)
	for drops.Load() != total-udpMaxInflight {
		if time.Now().After(deadline) {
			t.Fatalf("drops = %d, want %d", drops.Load(), total-udpMaxInflight)
		}
		time.Sleep(5 * time.Millisecond)
	}
	for i := 0; i < udpMaxInflight; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d resolver calls started, want %d", i, udpMaxInflight)
		}
	}
	select {
	case <-started:
		t.Fatal("more than the in-flight cap reached the resolver")
	case <-time.After(100 * time.Millisecond):
	}
	if got := peak.Load(); got != udpMaxInflight {
		t.Fatalf("peak concurrency = %d, want %d", got, udpMaxInflight)
	}
	if got := calls.Load(); got != udpMaxInflight {
		t.Fatalf("resolver calls = %d, want %d", got, udpMaxInflight)
	}

	close(release)
	seen := make(map[uint16]bool)
	for i := 0; i < udpMaxInflight; i++ {
		reply, ok := readUDPResponse(t, client, 3*time.Second)
		if !ok {
			t.Fatalf("received %d/%d responses", i, udpMaxInflight)
		}
		seen[reply.msg.Id] = true
	}
	if len(seen) != udpMaxInflight {
		t.Fatalf("distinct response IDs = %d, want %d", len(seen), udpMaxInflight)
	}
	if reply, ok := readUDPResponse(t, client, 250*time.Millisecond); ok {
		t.Fatalf("extra response for a dropped datagram: ID %#x", reply.msg.Id)
	}
	if got := calls.Load(); got != udpMaxInflight {
		t.Fatalf("resolver calls after release = %d, want %d", got, udpMaxInflight)
	}
}

func TestUDPAssociateControlCloseTearsDown(t *testing.T) {
	var calls atomic.Int32
	s := startUDPTestServer(t, echoDNS(&calls))
	ctrl, relay, client := associateUDP(t, s, 0)
	defer client.Close()

	_ = ctrl.Close()
	waitForNoAssociations(t, s)

	if _, err := client.WriteToUDP(udpQueryPacket(t, 53, dnsQuery(9, "closed.example.com.")), relay); err != nil {
		t.Fatal(err)
	}
	if reply, ok := readUDPResponse(t, client, 250*time.Millisecond); ok {
		t.Fatalf("datagram was answered after the control connection closed: %+v", reply.msg)
	}
	if calls.Load() != 0 {
		t.Fatalf("resolver calls = %d, want 0 after teardown", calls.Load())
	}
}

func TestServerStopClosesUDPAssociations(t *testing.T) {
	started := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	var calls atomic.Int32
	resolve := func(ctx context.Context, _ *mdns.Msg) (*mdns.Msg, error) {
		calls.Add(1)
		started <- struct{}{}
		<-ctx.Done()
		cancelled <- struct{}{}
		return nil, ctx.Err()
	}

	s := startUDPTestServer(t, resolve)
	ctrl, relay, client := associateUDP(t, s, 0)
	defer ctrl.Close()
	defer client.Close()

	if _, err := client.WriteToUDP(udpQueryPacket(t, 53, dnsQuery(10, "stop.example.com.")), relay); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the resolver was never called")
	}

	stopped := make(chan struct{})
	go func() {
		s.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("Server.Stop hung with an in-flight UDP query")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel the in-flight resolver call")
	}

	s.mu.Lock()
	assocs := len(s.udpAssocs)
	s.mu.Unlock()
	if assocs != 0 {
		t.Fatalf("%d UDP association(s) survived Stop", assocs)
	}

	// The control connection and the relay are both closed.
	_ = ctrl.SetReadDeadline(time.Now().Add(time.Second))
	var one [1]byte
	if _, err := ctrl.Read(one[:]); err == nil {
		t.Fatal("the control connection is still readable after Stop")
	}
	if _, err := client.WriteToUDP(udpQueryPacket(t, 53, dnsQuery(11, "after-stop.example.com.")), relay); err != nil {
		t.Fatal(err)
	}
	if reply, ok := readUDPResponse(t, client, 250*time.Millisecond); ok {
		t.Fatalf("the relay answered after Stop: %+v", reply.msg)
	}
	if calls.Load() != 1 {
		t.Fatalf("resolver calls = %d, want 1 (no new query after Stop)", calls.Load())
	}
}
