package socks

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/upstream"
)

type pipeTunnel struct{ net.Conn }

func (p *pipeTunnel) CloseWrite() error { return p.Conn.Close() }

func testServer(dial func(ctx context.Context, host string, port int) (net.Conn, error)) *Server {
	s := &Server{
		MaxConns:         8,
		HandshakeTimeout: 2 * time.Second,
		HalfCloseDrain:   500 * time.Millisecond,
		OpenTimeout:      2 * time.Second,
		Dial:             dial,
	}
	s.normalize()
	return s
}

func startHandler(t *testing.T, s *Server) (net.Conn, chan struct{}) {
	t.Helper()
	client, srvConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		s.handleConn(srvConn)
		close(done)
	}()
	return client, done
}

func doGreeting(t *testing.T, c net.Conn) {
	t.Helper()
	if _, err := c.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(c, reply); err != nil {
		t.Fatal(err)
	}
	if reply[0] != 5 || reply[1] != 0 {
		t.Fatalf("greeting reply = %v", reply)
	}
}

func domainRequest(cmd byte, host string, port int) []byte {
	req := []byte{5, cmd, 0, 3, byte(len(host))}
	req = append(req, host...)
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	return req
}

func ipRequest(cmd byte, ip net.IP, port int) []byte {
	raw := ip.To4()
	atyp := byte(1)
	if raw == nil {
		raw = ip.To16()
		atyp = 4
	}
	req := []byte{5, cmd, 0, atyp}
	req = append(req, raw...)
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	return req
}

func readReplyCode(t *testing.T, c net.Conn) byte {
	t.Helper()
	buf := make([]byte, 4)
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatal(err)
	}
	if buf[0] != 5 {
		t.Fatalf("reply version = %d", buf[0])
	}
	rest := make([]byte, 6)
	if buf[3] == 4 {
		rest = make([]byte, 18)
	}
	if _, err := io.ReadFull(c, rest); err != nil {
		t.Fatal(err)
	}
	return buf[1]
}

func TestConnectRelayEcho(t *testing.T) {
	var dials atomic.Int32
	s := testServer(func(_ context.Context, host string, port int) (net.Conn, error) {
		dials.Add(1)
		if host != "example.com" || port != 443 {
			return nil, fmt.Errorf("unexpected target %s:%d", host, port)
		}
		serverSide, clientSide := net.Pipe()
		go func() {
			defer clientSide.Close()
			buf := make([]byte, 4096)
			for {
				n, err := clientSide.Read(buf)
				if n > 0 {
					if _, werr := clientSide.Write(buf[:n]); werr != nil {
						return
					}
				}
				if err != nil {
					return
				}
			}
		}()
		return &pipeTunnel{Conn: serverSide}, nil
	})

	client, done := startHandler(t, s)
	defer client.Close()
	doGreeting(t, client)
	if _, err := client.Write(domainRequest(1, "example.com", 443)); err != nil {
		t.Fatal(err)
	}
	if code := readReplyCode(t, client); code != replySucceeded {
		t.Fatalf("reply = %#x", code)
	}
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "ping" {
		t.Fatalf("echo = %q", buf)
	}
	if _, err := client.Write([]byte("pong")); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "pong" {
		t.Fatalf("echo = %q", buf)
	}
	if dials.Load() != 1 {
		t.Fatalf("dials = %d", dials.Load())
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not finish after client close")
	}
}

func TestLocalDestinationsRefused(t *testing.T) {
	var dials atomic.Int32
	s := testServer(func(context.Context, string, int) (net.Conn, error) {
		dials.Add(1)
		return nil, fmt.Errorf("must not dial")
	})

	cases := []net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("10.1.2.3"),
		net.ParseIP("172.16.0.1"),
		net.ParseIP("192.168.0.5"),
		net.ParseIP("169.254.1.1"),
		net.ParseIP("100.64.0.1"),
		net.ParseIP("224.0.0.1"),
		net.ParseIP("0.0.0.0"),
		net.ParseIP("fc00::1"),
		net.ParseIP("::1"),
	}
	for _, ip := range cases {
		client, _ := startHandler(t, s)
		doGreeting(t, client)
		if _, err := client.Write(ipRequest(1, ip, 80)); err != nil {
			t.Fatal(err)
		}
		if code := readReplyCode(t, client); code != replyNotAllowed {
			t.Fatalf("%s: reply = %#x, want local refusal", ip, code)
		}
		// An IP literal target never leaves the device.
		_ = client.Close()
	}
	if dials.Load() != 0 {
		t.Fatalf("dialer called %d times for local destinations", dials.Load())
	}
}

func TestUnsupportedCommand(t *testing.T) {
	s := testServer(func(context.Context, string, int) (net.Conn, error) {
		t.Fatal("must not dial")
		return nil, nil
	})
	client, _ := startHandler(t, s)
	defer client.Close()
	doGreeting(t, client)
	// Unsupported commands are refused without reading the address.
	if _, err := client.Write([]byte{5, 2, 0, 3}); err != nil {
		t.Fatal(err)
	}
	if code := readReplyCode(t, client); code != replyCmdUnsupported {
		t.Fatalf("reply = %#x", code)
	}
}

func TestUDPAssociateOnlyDNS(t *testing.T) {
	s := testServer(func(context.Context, string, int) (net.Conn, error) {
		t.Fatal("UDP associate must not dial")
		return nil, nil
	})

	client, _ := startHandler(t, s)
	doGreeting(t, client)
	if _, err := client.Write(domainRequest(3, "1.1.1.1", 80)); err != nil {
		t.Fatal(err)
	}
	if code := readReplyCode(t, client); code != replyNotAllowed {
		t.Fatalf("port 80 reply = %#x", code)
	}
	_ = client.Close()

	client2, _ := startHandler(t, s)
	defer client2.Close()
	doGreeting(t, client2)
	if _, err := client2.Write(domainRequest(3, "1.1.1.1", 53)); err != nil {
		t.Fatal(err)
	}
	if code := readReplyCode(t, client2); code != replyCmdUnsupported {
		t.Fatalf("port 53 reply = %#x", code)
	}
}

func TestAuthRejectionReply(t *testing.T) {
	s := testServer(func(context.Context, string, int) (net.Conn, error) {
		return nil, &upstream.ConnectRejected{Status: http.StatusForbidden, Authority: "example.com:443"}
	})
	client, _ := startHandler(t, s)
	defer client.Close()
	doGreeting(t, client)
	if _, err := client.Write(domainRequest(1, "example.com", 443)); err != nil {
		t.Fatal(err)
	}
	if code := readReplyCode(t, client); code != replyGeneralFailure {
		t.Fatalf("reply = %#x", code)
	}
}

func TestUnreachableRefusalCache(t *testing.T) {
	var dials atomic.Int32
	s := testServer(func(context.Context, string, int) (net.Conn, error) {
		dials.Add(1)
		return nil, &upstream.ConnectRejected{Status: http.StatusBadGateway, Authority: "dead.example.com:443"}
	})

	client, _ := startHandler(t, s)
	defer client.Close()
	doGreeting(t, client)
	if _, err := client.Write(domainRequest(1, "dead.example.com", 443)); err != nil {
		t.Fatal(err)
	}
	if code := readReplyCode(t, client); code != replyHostUnreachable {
		t.Fatalf("first reply = %#x", code)
	}

	client2, _ := startHandler(t, s)
	defer client2.Close()
	doGreeting(t, client2)
	if _, err := client2.Write(domainRequest(1, "dead.example.com", 443)); err != nil {
		t.Fatal(err)
	}
	if code := readReplyCode(t, client2); code != replyHostUnreachable {
		t.Fatalf("cached reply = %#x", code)
	}
	if dials.Load() != 1 {
		t.Fatalf("dials = %d, want 1 (second refusal should come from cache)", dials.Load())
	}
}

func TestEmptyDomainRejected(t *testing.T) {
	s := testServer(func(context.Context, string, int) (net.Conn, error) {
		t.Fatal("must not dial")
		return nil, nil
	})
	client, _ := startHandler(t, s)
	defer client.Close()
	doGreeting(t, client)
	// Empty domain: version, CONNECT, reserved, domain atyp, zero length.
	if _, err := client.Write([]byte{5, 1, 0, 3, 0}); err != nil {
		t.Fatal(err)
	}
	if code := readReplyCode(t, client); code != replyAddrUnsupported {
		t.Fatalf("reply = %#x", code)
	}
}
