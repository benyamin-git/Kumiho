package tun

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/xjasonlyu/tun2socks/v2/metadata"
)

type stubDNSHandler struct {
	udpCalls int
	tcpCalls int
}

func (h *stubDNSHandler) HandleQuery(req *mdns.Msg, overUDP bool) *mdns.Msg {
	if overUDP {
		h.udpCalls++
	} else {
		h.tcpCalls++
	}
	resp := new(mdns.Msg)
	resp.SetReply(req)
	resp.Answer = []mdns.RR{&mdns.A{
		Hdr: mdns.RR_Header{
			Name:   req.Question[0].Name,
			Rrtype: mdns.TypeA,
			Class:  mdns.ClassINET,
			Ttl:    60,
		},
		A: net.IPv4(203, 0, 113, 7),
	}}
	return resp
}

func testDNSQuery(t *testing.T) []byte {
	t.Helper()
	req := new(mdns.Msg)
	req.SetQuestion("example.com.", mdns.TypeA)
	raw, err := req.Pack()
	if err != nil {
		t.Fatalf("pack query: %v", err)
	}
	return raw
}

func assertDNSAnswer(t *testing.T, raw []byte) {
	t.Helper()
	resp := new(mdns.Msg)
	if err := resp.Unpack(raw); err != nil {
		t.Fatalf("unpack response: %v", err)
	}
	if resp.Rcode != mdns.RcodeSuccess || len(resp.Answer) != 1 {
		t.Fatalf("response = rcode %d, %d answers; want success with one answer", resp.Rcode, len(resp.Answer))
	}
	a, ok := resp.Answer[0].(*mdns.A)
	if !ok || !a.A.Equal(net.IPv4(203, 0, 113, 7)) {
		t.Fatalf("answer = %v, want 203.0.113.7", resp.Answer[0])
	}
}

func testDNSAddr() *net.UDPAddr {
	return &net.UDPAddr{IP: net.IPv4(10, 8, 0, 1), Port: 53}
}

func TestDNSPacketConnAnswers(t *testing.T) {
	h := &stubDNSHandler{}
	pc := newDNSPacketConn(h, testDNSAddr())
	defer pc.Close()

	if _, err := pc.WriteTo(testDNSQuery(t), pc.LocalAddr()); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if err := pc.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	buf := make([]byte, 1500)
	n, from, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	udp, ok := from.(*net.UDPAddr)
	if !ok || !udp.IP.Equal(net.IPv4(10, 8, 0, 1)) || udp.Port != 53 {
		t.Fatalf("ReadFrom source = %v, want 10.8.0.1:53", from)
	}
	assertDNSAnswer(t, buf[:n])
	if h.udpCalls != 1 || h.tcpCalls != 0 {
		t.Fatalf("handler calls = udp %d, tcp %d; want 1/0", h.udpCalls, h.tcpCalls)
	}
}

func TestDNSPacketConnDeadline(t *testing.T) {
	pc := newDNSPacketConn(&stubDNSHandler{}, testDNSAddr())
	defer pc.Close()

	if err := pc.SetReadDeadline(time.Now().Add(10 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	if _, _, err := pc.ReadFrom(make([]byte, 32)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("ReadFrom error = %v, want the read deadline", err)
	}
}

func TestDNSTCPConnAnswers(t *testing.T) {
	h := &stubDNSHandler{}
	conn := newDNSTCPConn(h, testDNSAddr())
	defer conn.Close()

	query := testDNSQuery(t)
	frame := make([]byte, 2+len(query))
	binary.BigEndian.PutUint16(frame, uint16(len(query)))
	copy(frame[2:], query)
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	var hdr [2]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		t.Fatalf("read response length: %v", err)
	}
	resp := make([]byte, int(binary.BigEndian.Uint16(hdr[:])))
	if _, err := io.ReadFull(conn, resp); err != nil {
		t.Fatalf("read response: %v", err)
	}
	assertDNSAnswer(t, resp)
	if h.tcpCalls != 1 || h.udpCalls != 0 {
		t.Fatalf("handler calls = udp %d, tcp %d; want 0/1", h.udpCalls, h.tcpCalls)
	}
}

func TestAdapterRoutesTunnelDNS(t *testing.T) {
	h := &stubDNSHandler{}
	a := &adapter{dns: h, dnsHost: netip.MustParseAddr("10.8.0.1"), dnsAddr: testDNSAddr()}

	pc, err := a.DialUDP(&metadata.Metadata{DstIP: netip.MustParseAddr("10.8.0.1"), DstPort: 53})
	if err != nil {
		t.Fatalf("DialUDP for the tunnel DNS: %v", err)
	}
	_ = pc.Close()

	if _, err := a.DialUDP(&metadata.Metadata{DstIP: netip.MustParseAddr("1.1.1.1"), DstPort: 53}); err == nil {
		t.Fatal("DialUDP accepted a non-DNS UDP flow")
	}

	conn, err := a.DialContext(context.Background(), &metadata.Metadata{DstIP: netip.MustParseAddr("10.8.0.1"), DstPort: 53})
	if err != nil {
		t.Fatalf("DialContext for the tunnel DNS: %v", err)
	}
	_ = conn.Close()
}

func TestWithDNSValidatesAddress(t *testing.T) {
	a := &adapter{}
	if err := WithDNS(&stubDNSHandler{}, "10.8.0.1:53")(a); err != nil {
		t.Fatalf("WithDNS: %v", err)
	}
	if a.dns == nil || !a.dnsHost.IsValid() || a.dnsHost.String() != "10.8.0.1" {
		t.Fatalf("adapter DNS = %v / %v, want 10.8.0.1", a.dns, a.dnsHost)
	}
	for _, addr := range []string{"", "10.8.0.1:5353", "not-an-ip:53"} {
		if err := WithDNS(&stubDNSHandler{}, addr)(&adapter{}); err == nil {
			t.Fatalf("WithDNS(%q) succeeded, want an error", addr)
		}
	}
	if err := WithDNS(nil, "10.8.0.1:53")(&adapter{}); err == nil {
		t.Fatal("WithDNS(nil) succeeded, want an error")
	}
}
