//go:build integration

// Integration tests against the real Mozilla stack (PLAN.md §11). They need
// a live Firefox account and run manually:
//
//	KUMIHO_TEST_EMAIL=you@example.com KUMIHO_TEST_PASSWORD=... \
//	  go test -tags integration -run Integration -v ./internal/engine
package engine

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/guardian"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/serverlist"
	"github.com/benyamin-git/kumiho/internal/upstream"
)

// TestIntegrationProxyOnlyTunnel is the M3 acceptance path end to end:
// login → server list → proxy pass → HTTP/2 CONNECT → exit check.
func TestIntegrationProxyOnlyTunnel(t *testing.T) {
	email := os.Getenv("KUMIHO_TEST_EMAIL")
	password := os.Getenv("KUMIHO_TEST_PASSWORD")
	if email == "" || password == "" {
		t.Skip("set KUMIHO_TEST_EMAIL and KUMIHO_TEST_PASSWORD to run this test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client := fxa.NewClient()
	res, err := client.Login(ctx, email, password)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if !res.Verified {
		t.Fatalf("account needs email verification (%s); complete it manually first", res.VerificationMethod)
	}
	tokens, err := client.OAuthToken(ctx, res.SessionToken)
	if err != nil {
		t.Fatalf("oauth token: %v", err)
	}

	countries, err := serverlist.Fetch(ctx, nil)
	if err != nil {
		t.Fatalf("server list: %v", err)
	}
	cands := serverlist.Candidates(countries, serverlist.RecommendedCode, "")
	if len(cands) == 0 {
		t.Fatalf("no recommended servers; list has %d countries", len(countries))
	}
	cand := cands[rand.IntN(len(cands))]
	t.Logf("connecting via %s (%s)", cand.Address(), cand.CountryCode)

	pass, err := guardian.NewClient().FetchPass(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatalf("guardian pass: %v", err)
	}
	t.Logf("proxy pass acquired (expires %s, quota remaining %v)", time.Unix(pass.ExpiresAt, 0), pass.QuotaRemaining)

	sess, err := upstream.Dial(ctx, upstream.Options{
		Host: cand.Host,
		Port: cand.Port,
		Pass: func() string { return pass.Token },
		Logf: func(_ logging.Level, tag, format string, args ...any) {
			t.Logf("[%s] %s", tag, fmt.Sprintf(format, args...))
		},
	})
	if err != nil {
		t.Fatalf("upstream dial: %v", err)
	}
	defer sess.Close()

	// Exit check through a CONNECT stream: TLS to Cloudflare, HTTP/1.1 trace.
	conn, err := sess.OpenStream(ctx, "www.cloudflare.com", 443)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer conn.Close()
	tlsConn := tls.Client(conn, &tls.Config{ServerName: "www.cloudflare.com", MinVersion: tls.VersionTLS12})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		t.Fatalf("tunneled TLS handshake: %v", err)
	}
	req := "GET /cdn-cgi/trace HTTP/1.1\r\nHost: www.cloudflare.com\r\nUser-Agent: kumiho-integration\r\nConnection: close\r\n\r\n"
	if _, err := io.WriteString(tlsConn, req); err != nil {
		t.Fatalf("trace request: %v", err)
	}
	body, err := io.ReadAll(io.LimitReader(tlsConn, 64<<10))
	if err != nil {
		t.Fatalf("trace read: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "ip=") || !strings.Contains(text, "loc=") {
		t.Fatalf("trace response has no ip/loc:\n%s", truncate(text, 400))
	}
	ip, country := parseCloudflareTrace(text)
	t.Logf("exit IP %s (%s)", ip, country)

	if !strings.EqualFold(cand.CountryCode, serverlist.RecommendedCode) &&
		!strings.EqualFold(country, cand.CountryCode) &&
		country != "" {
		t.Errorf("exit country %s does not match selected %s", country, cand.CountryCode)
	}

	// Also verify the full SOCKS path used by the daemon.
	if err := socksRoundTrip(ctx, sess, t); err != nil {
		t.Fatalf("socks path: %v", err)
	}
}

func socksRoundTrip(ctx context.Context, sess *upstream.Session, t *testing.T) error {
	t.Helper()
	conn, err := sess.OpenStream(ctx, "example.com", 80)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "GET / HTTP/1.0\r\nHost: example.com\r\n\r\n"); err != nil {
		return err
	}
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil && err != io.EOF {
		return err
	}
	if !strings.Contains(string(buf[:n]), "HTTP/") {
		return fmt.Errorf("unexpected response: %q", buf[:n])
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
