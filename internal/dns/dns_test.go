package dns

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

func TestStartStopRace(t *testing.T) {
	s := &Server{
		Addr:  "127.0.0.1:0",
		cache: newCache(10),
		Resolve: func(_ context.Context, req *mdns.Msg) (*mdns.Msg, error) {
			return aResponse(t, req.Question[0].Name, 60, 0), nil
		},
	}
	for i := 0; i < 50; i++ {
		if err := s.Start(); err != nil {
			t.Fatal(err)
		}
		s.Stop()
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

// roundTripperFunc adapts a function to an http.RoundTripper; it is the test
// seam for the resolver's DoH client.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// dohAnswer reads the DoH request and answers its question with an A record.
func dohAnswer(t *testing.T, req *http.Request) *http.Response {
	t.Helper()
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read DoH request: %v", err)
	}
	var q mdns.Msg
	if err := q.Unpack(body); err != nil {
		t.Fatalf("unpack DoH request: %v", err)
	}
	msg := new(mdns.Msg)
	msg.SetReply(&q)
	if len(q.Question) == 1 {
		name := q.Question[0].Name
		msg.Answer = append(msg.Answer, &mdns.A{
			Hdr: mdns.RR_Header{Name: name, Rrtype: mdns.TypeA, Class: mdns.ClassINET, Ttl: 60},
			A:   net.ParseIP("93.184.216.34").To4(),
		})
	}
	return dohResponse(t, req, msg)
}

func dohResponse(t *testing.T, req *http.Request, msg *mdns.Msg) *http.Response {
	t.Helper()
	packed, err := msg.Pack()
	if err != nil {
		t.Fatalf("pack DoH response: %v", err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/dns-message"}},
		Body:       io.NopCloser(bytes.NewReader(packed)),
		Request:    req,
	}
}

func dohStatus(req *http.Request, code int) *http.Response {
	return &http.Response{
		StatusCode: code,
		Status:     fmt.Sprintf("%d status", code),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("error")),
		Request:    req,
	}
}

func dohGarbage(t *testing.T, req *http.Request) *http.Response {
	t.Helper()
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("not a dns message")),
		Request:    req,
	}
}

func question(name string) *mdns.Msg {
	q := new(mdns.Msg)
	q.SetQuestion(name, mdns.TypeA)
	return q
}

func unusedDial(context.Context, string, int) (net.Conn, error) {
	return nil, errors.New("dial not expected")
}

func TestResolverChainOrder(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		upstream string
		want     []string
		wantErr  bool
	}{
		{
			name:     "provider first then cloudflare and quad9",
			provider: "google",
			want:     []string{"https://8.8.8.8/dns-query", "https://1.1.1.1/dns-query", "https://9.9.9.9/dns-query"},
		},
		{
			name:     "provider duplicates are dropped",
			provider: "automatic",
			want:     []string{"https://1.1.1.1/dns-query", "https://9.9.9.9/dns-query"},
		},
		{
			name:     "quad9 provider is not repeated",
			provider: "quad9",
			want:     []string{"https://9.9.9.9/dns-query", "https://1.1.1.1/dns-query"},
		},
		{
			name:     "custom upstream is always DoH",
			provider: "automatic",
			upstream: "10.11.12.13",
			want:     []string{"https://10.11.12.13/dns-query", "https://1.1.1.1/dns-query", "https://9.9.9.9/dns-query"},
		},
		{
			name:     "custom upstream that matches cloudflare is deduplicated",
			provider: "cloudflare",
			upstream: "1.1.1.1",
			want:     []string{"https://1.1.1.1/dns-query", "https://9.9.9.9/dns-query"},
		},
		{
			name:     "custom IPv6 upstream is bracketed",
			provider: "off",
			upstream: "2606:4700:4700::1111",
			want:     []string{"https://[2606:4700:4700::1111]/dns-query", "https://1.1.1.1/dns-query", "https://9.9.9.9/dns-query"},
		},
		{
			name:     "off has no endpoints",
			provider: "off",
		},
		{
			name:     "unknown provider is rejected",
			provider: "yandex",
			wantErr:  true,
		},
		{
			name:     "non-IP upstream is rejected",
			provider: "automatic",
			upstream: "dns.example.com",
			wantErr:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := NewResolver(unusedDial, tc.provider, tc.upstream, nil)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %s/%q", tc.provider, tc.upstream)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewResolver(%q, %q): %v", tc.provider, tc.upstream, err)
			}
			got := make([]string, len(r.endpoints))
			for i, ep := range r.endpoints {
				got[i] = ep.url
			}
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Fatalf("chain = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolverRequiresDialer(t *testing.T) {
	if _, err := NewResolver(nil, "automatic", "", nil); err == nil {
		t.Fatal("NewResolver(nil) must fail")
	}
}

func TestResolverFailover(t *testing.T) {
	cases := []struct {
		name string
		fail func(t *testing.T, req *http.Request) (*http.Response, error)
	}{
		{
			name: "transport failure",
			fail: func(*testing.T, *http.Request) (*http.Response, error) {
				return nil, errors.New("connection refused")
			},
		},
		{
			name: "http failure",
			fail: func(_ *testing.T, req *http.Request) (*http.Response, error) {
				return dohStatus(req, http.StatusBadGateway), nil
			},
		},
		{
			name: "unpack failure",
			fail: func(t *testing.T, req *http.Request) (*http.Response, error) {
				return dohGarbage(t, req), nil
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := NewResolver(unusedDial, "automatic", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			var hosts []string
			r.client = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				hosts = append(hosts, req.URL.Hostname())
				if req.URL.Hostname() == "1.1.1.1" {
					return tc.fail(t, req)
				}
				return dohAnswer(t, req), nil
			})}

			resp, err := r.Resolve(context.Background(), question("failover.example.com."))
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if resp.Rcode != mdns.RcodeSuccess || len(resp.Answer) != 1 {
				t.Fatalf("response = %+v", resp)
			}
			if got := strings.Join(hosts, ","); got != "1.1.1.1,9.9.9.9" {
				t.Fatalf("endpoint order = %s, want 1.1.1.1,9.9.9.9", got)
			}
			if _, ok := r.suppressedUntil("1.1.1.1"); !ok {
				t.Fatal("failed endpoint was not suppressed")
			}
			if _, ok := r.suppressedUntil("9.9.9.9"); ok {
				t.Fatal("healthy endpoint must not be suppressed")
			}
		})
	}
}

func TestResolverSuppressionWindow(t *testing.T) {
	current := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	primaryDown := true
	var hosts []string

	r, err := NewResolver(unusedDial, "automatic", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.now = func() time.Time { return current }
	r.client = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		hosts = append(hosts, req.URL.Hostname())
		if req.URL.Hostname() == "1.1.1.1" && primaryDown {
			return nil, errors.New("connection refused")
		}
		return dohAnswer(t, req), nil
	})}

	resolve := func(name string) *mdns.Msg {
		t.Helper()
		resp, err := r.Resolve(context.Background(), question(name))
		if err != nil {
			t.Fatalf("resolve %s: %v", name, err)
		}
		return resp
	}

	resolve("a.example.com.")
	if got := strings.Join(hosts, ","); got != "1.1.1.1,9.9.9.9" {
		t.Fatalf("first query endpoints = %s", got)
	}
	until, ok := r.suppressedUntil("1.1.1.1")
	if !ok || !until.Equal(current.Add(suppressDuration)) {
		t.Fatalf("suppressed until %v (present=%v), want %v", until, ok, current.Add(suppressDuration))
	}

	hosts = nil
	resolve("b.example.com.")
	if got := strings.Join(hosts, ","); got != "9.9.9.9" {
		t.Fatalf("query inside the window used %s, want only 9.9.9.9", got)
	}

	current = current.Add(suppressDuration - time.Second)
	hosts = nil
	resolve("c.example.com.")
	if got := strings.Join(hosts, ","); got != "9.9.9.9" {
		t.Fatalf("query at 30m-1s used %s, want only 9.9.9.9", got)
	}

	primaryDown = false
	current = current.Add(2 * time.Second)
	hosts = nil
	resolve("d.example.com.")
	if got := strings.Join(hosts, ","); got != "1.1.1.1" {
		t.Fatalf("query after the window used %s, want 1.1.1.1 first", got)
	}
}

func TestResolverAllEndpointsFail(t *testing.T) {
	r, err := NewResolver(unusedDial, "automatic", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.client = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})}

	if _, err := r.Resolve(context.Background(), question("down.example.com.")); err == nil {
		t.Fatal("expected an error when every endpoint fails")
	} else if !strings.Contains(err.Error(), "all DoH endpoints failed") {
		t.Fatalf("error = %v, want an all-endpoints-failed error", err)
	}

	// Both endpoints are now suppressed: the next query fails without dialing.
	if _, err := r.Resolve(context.Background(), question("down2.example.com.")); err == nil {
		t.Fatal("expected an error while every endpoint is suppressed")
	} else if !strings.Contains(err.Error(), "suppressed") {
		t.Fatalf("error = %v, want a suppression error", err)
	}
}

func TestResolverReturnsErrorResponses(t *testing.T) {
	for _, rcode := range []int{mdns.RcodeNameError, mdns.RcodeServerFailure} {
		var hosts []string
		r, err := NewResolver(unusedDial, "automatic", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		r.client = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			hosts = append(hosts, req.URL.Hostname())
			body, _ := io.ReadAll(req.Body)
			var q mdns.Msg
			if err := q.Unpack(body); err != nil {
				t.Fatalf("unpack request: %v", err)
			}
			msg := new(mdns.Msg)
			msg.SetRcode(&q, rcode)
			return dohResponse(t, req, msg), nil
		})}

		resp, err := r.Resolve(context.Background(), question("missing.example.com."))
		if err != nil {
			t.Fatalf("rcode %d must be returned as-is: %v", rcode, err)
		}
		if resp.Rcode != rcode {
			t.Fatalf("rcode = %d, want %d", resp.Rcode, rcode)
		}
		if len(hosts) != 1 || hosts[0] != "1.1.1.1" {
			t.Fatalf("endpoints = %v, want only the first one", hosts)
		}
	}
}

func TestResolverNeverDialsPort53(t *testing.T) {
	type target struct {
		host string
		port int
	}
	var mu sync.Mutex
	var dials []target
	dialer := func(_ context.Context, host string, port int) (net.Conn, error) {
		mu.Lock()
		dials = append(dials, target{host: host, port: port})
		mu.Unlock()
		return nil, errors.New("connection refused")
	}
	r, err := NewResolver(dialer, "automatic", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Resolve(context.Background(), question("strict.example.com.")); err == nil {
		t.Fatal("expected the endpoint chain to fail")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(dials) == 0 {
		t.Fatal("the strict resolver did not dial any endpoint")
	}
	for _, d := range dials {
		if d.port == 53 {
			t.Fatalf("strict resolver dialed plaintext DNS at %s:%d", d.host, d.port)
		}
		if d.port != 443 {
			t.Fatalf("strict resolver dialed %s:%d, want port 443", d.host, d.port)
		}
	}
}

func TestOffProviderServesSERVFAIL(t *testing.T) {
	r, err := NewResolver(unusedDial, "off", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.endpoints) != 0 {
		t.Fatalf("provider off has %d endpoints, want none", len(r.endpoints))
	}
	s := NewWithResolver(r)
	s.Addr = "127.0.0.1:0"
	// A resolver-backed server has no Dial of its own; Start must accept it.
	if err := s.Start(); err != nil {
		t.Fatalf("start resolver-backed server: %v", err)
	}
	defer s.Stop()

	client := &mdns.Client{Net: "udp"}
	resp, _, err := client.Exchange(question("example.com."), s.udpSrv.PacketConn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Rcode != mdns.RcodeServerFailure {
		t.Fatalf("rcode = %d, want SERVFAIL", resp.Rcode)
	}
}

func TestStartServesThroughResolver(t *testing.T) {
	r, err := NewResolver(unusedDial, "automatic", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.client = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return dohAnswer(t, req), nil
	})}
	s := NewWithResolver(r)
	s.Addr = "127.0.0.1:0"
	if err := s.Start(); err != nil {
		t.Fatalf("start resolver-backed server: %v", err)
	}
	defer s.Stop()

	client := &mdns.Client{Net: "udp"}
	resp, _, err := client.Exchange(question("example.com."), s.udpSrv.PacketConn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Rcode != mdns.RcodeSuccess || len(resp.Answer) != 1 {
		t.Fatalf("response = %+v", resp)
	}
}

func TestLegacyNewKeepsTCPFallback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mux := mdns.NewServeMux()
	mux.HandleFunc(".", func(w mdns.ResponseWriter, req *mdns.Msg) {
		_ = w.WriteMsg(aResponse(t, req.Question[0].Name, 60, 0))
	})
	srv := &mdns.Server{Listener: ln, Handler: mux}
	go func() { _ = srv.ActivateAndServe() }()
	defer srv.Shutdown()

	// DoH (port 443) is unreachable; the bootstrap path must fall back to
	// plaintext TCP:53 on the marked dialer.
	dial := func(ctx context.Context, _ string, port int) (net.Conn, error) {
		if port != 53 {
			return nil, errors.New("DoH unreachable")
		}
		var d net.Dialer
		return d.DialContext(ctx, "tcp", ln.Addr().String())
	}
	s, err := New(dial, "automatic", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp := s.HandleQuery(question("legacy.example.com."), false)
	if resp.Rcode != mdns.RcodeSuccess || len(resp.Answer) != 1 {
		t.Fatalf("bootstrap fallback response = %+v", resp)
	}
}
