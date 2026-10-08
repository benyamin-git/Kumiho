package dns

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	mdns "github.com/miekg/dns"
)

func aResponse(t *testing.T, name string, ttl uint32, extraTXT int) *mdns.Msg {
	t.Helper()
	req := new(mdns.Msg)
	req.SetQuestion(name, mdns.TypeA)
	msg := new(mdns.Msg)
	msg.SetReply(req)
	msg.Answer = append(msg.Answer, &mdns.A{
		Hdr: mdns.RR_Header{Name: name, Rrtype: mdns.TypeA, Class: mdns.ClassINET, Ttl: ttl},
		A:   net.ParseIP("93.184.216.34").To4(),
	})
	for i := 0; i < extraTXT; i++ {
		msg.Answer = append(msg.Answer, &mdns.TXT{
			Hdr: mdns.RR_Header{Name: name, Rrtype: mdns.TypeTXT, Class: mdns.ClassINET, Ttl: ttl},
			Txt: []string{strings.Repeat("x", 200), fmt.Sprintf("id-%d", i)},
		})
	}
	return msg
}

func TestCacheServesSecondQuery(t *testing.T) {
	var calls atomic.Int32
	s := &Server{
		cache: newCache(100),
		Resolve: func(_ context.Context, req *mdns.Msg) (*mdns.Msg, error) {
			calls.Add(1)
			return aResponse(t, req.Question[0].Name, 60, 0), nil
		},
	}
	query := new(mdns.Msg)
	query.SetQuestion("example.com.", mdns.TypeA)

	resp := s.HandleQuery(query, false)
	if resp.Rcode != mdns.RcodeSuccess || len(resp.Answer) != 1 {
		t.Fatalf("first response = %+v", resp)
	}
	resp2 := s.HandleQuery(query, false)
	if calls.Load() != 1 {
		t.Fatalf("resolver calls = %d, want 1 (second query must hit the cache)", calls.Load())
	}
	if len(resp2.Answer) != 1 {
		t.Fatalf("cached response = %+v", resp2)
	}
	if ttl := resp2.Answer[0].Header().Ttl; ttl > 60 {
		t.Fatalf("cached TTL = %d, want <= 60", ttl)
	}
}

func TestUDPTruncationAndTCPPassthrough(t *testing.T) {
	s := &Server{
		cache: newCache(100),
		Resolve: func(_ context.Context, req *mdns.Msg) (*mdns.Msg, error) {
			return aResponse(t, req.Question[0].Name, 60, 20), nil
		},
	}
	query := new(mdns.Msg)
	query.SetQuestion("big.example.com.", mdns.TypeTXT)

	udp := s.HandleQuery(query, true)
	if !udp.Truncated || len(udp.Answer) != 0 {
		t.Fatalf("UDP response should be truncated: %+v", udp)
	}
	tcp := s.HandleQuery(query, false)
	if tcp.Truncated || len(tcp.Answer) == 0 {
		t.Fatalf("TCP response must not be truncated: %+v", tcp)
	}
}

func TestEDNS0RaisesUDPLimit(t *testing.T) {
	s := &Server{
		cache: newCache(100),
		Resolve: func(_ context.Context, req *mdns.Msg) (*mdns.Msg, error) {
			return aResponse(t, req.Question[0].Name, 60, 5), nil
		},
	}
	query := new(mdns.Msg)
	query.SetQuestion("medium.example.com.", mdns.TypeTXT)
	query.SetEdns0(4096, false)

	resp := s.HandleQuery(query, true)
	if resp.Truncated {
		t.Fatalf("EDNS0 response must not be truncated: %+v", resp)
	}
}

func TestFormatAndOpcodeErrors(t *testing.T) {
	s := &Server{cache: newCache(10)}
	bad := new(mdns.Msg)
	if resp := s.HandleQuery(bad, false); resp.Rcode != mdns.RcodeFormatError {
		t.Fatalf("no-question rcode = %d", resp.Rcode)
	}
	op := new(mdns.Msg)
	op.Id = 7
	op.Opcode = mdns.OpcodeStatus
	op.Question = []mdns.Question{{Name: "example.com.", Qtype: mdns.TypeA, Qclass: mdns.ClassINET}}
	if resp := s.HandleQuery(op, false); resp.Rcode != mdns.RcodeNotImplemented {
		t.Fatalf("status-opcode rcode = %d", resp.Rcode)
	}
}

func TestProviderFromConfig(t *testing.T) {
	cases := []struct {
		provider, upstream string
		ip                 string
		doh                bool
		wantErr            bool
	}{
		{"automatic", "", "1.1.1.1", true, false},
		{"cloudflare", "", "1.1.1.1", true, false},
		{"google", "", "8.8.8.8", true, false},
		{"quad9", "", "9.9.9.9", true, false},
		{"off", "", "", false, false},
		{"automatic", "8.8.4.4", "8.8.4.4", true, false},
		{"automatic", "10.11.12.13", "10.11.12.13", false, false},
		{"bogus", "", "", false, true},
		{"automatic", "not-an-ip", "", false, true},
	}
	for _, tc := range cases {
		ip, doh, err := ProviderFromConfig(tc.provider, tc.upstream)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%s/%s: expected error", tc.provider, tc.upstream)
			}
			continue
		}
		if err != nil || ip != tc.ip || doh != tc.doh {
			t.Fatalf("%s/%s: got %q doh=%v err=%v, want %q doh=%v", tc.provider, tc.upstream, ip, doh, err, tc.ip, tc.doh)
		}
	}
}

func TestServerFailureWithoutProvider(t *testing.T) {
	s := &Server{cache: newCache(10)}
	query := new(mdns.Msg)
	query.SetQuestion("example.com.", mdns.TypeA)
	if resp := s.HandleQuery(query, false); resp.Rcode != mdns.RcodeServerFailure {
		t.Fatalf("rcode = %d, want SERVFAIL", resp.Rcode)
	}
}

func TestStartStopServesRealUDP(t *testing.T) {
	s := &Server{
		Addr:  "127.0.0.1:0",
		cache: newCache(10),
		Resolve: func(_ context.Context, req *mdns.Msg) (*mdns.Msg, error) {
			return aResponse(t, req.Question[0].Name, 60, 0), nil
		},
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	addr := s.udpSrv.PacketConn.LocalAddr().String()
	query := new(mdns.Msg)
	query.SetQuestion("example.com.", mdns.TypeA)
	client := &mdns.Client{Net: "udp"}
	resp, _, err := client.Exchange(query, addr)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Rcode != mdns.RcodeSuccess || len(resp.Answer) != 1 {
		t.Fatalf("response = %+v", resp)
	}
	if resp.Id != query.Id {
		t.Fatalf("response id = %d, want %d", resp.Id, query.Id)
	}
}
