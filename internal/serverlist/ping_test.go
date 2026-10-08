package serverlist

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestPingTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	d, err := PingTCP(context.Background(), "127.0.0.1", addr.Port, 0)
	if err != nil {
		t.Fatalf("ping loopback: %v", err)
	}
	if d < 0 || d > 2*time.Second {
		t.Fatalf("implausible RTT: %s", d)
	}
}

func TestPingTCPFailure(t *testing.T) {
	// Grab a port and close it again so connections are refused.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	if _, err := PingTCP(context.Background(), "127.0.0.1", port, 500*time.Millisecond); err == nil {
		t.Fatal("expected ping error for closed port")
	}
}
