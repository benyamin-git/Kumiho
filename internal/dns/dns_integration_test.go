//go:build integration

package dns

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
)

// TestIntegrationFailoverFromDeadEndpoint needs a real network path to
// 1.1.1.1:443 (Cloudflare DoH) and is skipped when offline. Run with:
//
//	go test -tags integration ./internal/dns -run Integration -v
func TestIntegrationFailoverFromDeadEndpoint(t *testing.T) {
	conn, err := net.DialTimeout("tcp", "1.1.1.1:443", 3*time.Second)
	if err != nil {
		t.Skipf("no network path to 1.1.1.1:443: %v", err)
	}
	_ = conn.Close()

	dial := func(ctx context.Context, host string, port int) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	}
	// 192.0.2.1 is TEST-NET-1 (RFC 5737): routable nowhere, so the first
	// endpoint must fail and the chain has to move on to Cloudflare.
	r, err := NewResolver(dial, "automatic", "192.0.2.1", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	resp, err := r.Resolve(ctx, question("example.com."))
	if err != nil {
		t.Fatalf("resolve through the failover chain: %v", err)
	}
	if resp.Rcode != mdns.RcodeSuccess || len(resp.Answer) == 0 {
		t.Fatalf("response = %+v", resp)
	}
	if _, ok := r.suppressedUntil("192.0.2.1"); !ok {
		t.Fatal("dead endpoint was not suppressed")
	}

	// A second query must skip the suppressed endpoint and still resolve.
	resp, err = r.Resolve(ctx, question("example.org."))
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if resp.Rcode != mdns.RcodeSuccess || len(resp.Answer) == 0 {
		t.Fatalf("second response = %+v", resp)
	}
}
