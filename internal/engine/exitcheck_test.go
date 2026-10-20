package engine

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/socks"
)

// An exit check belongs to one connection. Once its stop channel is closed,
// a late trace response must not overwrite the current (or next) session.
func TestExitCheckDropsLateResultAfterSessionEnd(t *testing.T) {
	reached := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case reached <- struct{}{}:
		default:
		}
		<-release
		fmt.Fprint(w, "ip=203.0.113.77\nloc=DE\n")
	}))
	defer srv.Close()

	socksSrv := &socks.Server{
		Bind: "127.0.0.1",
		Dial: func(ctx context.Context, host string, port int) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		},
	}
	if err := socksSrv.Start(); err != nil {
		t.Fatal(err)
	}
	defer socksSrv.Stop()

	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	ctrl.exitCheckURL = srv.URL
	// The SOCKS server refuses literal loopback destinations, so address the
	// same test listener by name; the server's Dial resolves it locally.
	if _, port, err := net.SplitHostPort(srv.Listener.Addr().String()); err == nil {
		ctrl.exitCheckURL = "http://localhost:" + port + "/"
	}

	// A newer session has already published its own exit.
	ctrl.mu.Lock()
	ctrl.exitIP, ctrl.exitCountry = "192.0.2.1", "NL"
	ctrl.mu.Unlock()

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		ctrl.exitCheck(stop, socksSrv.Addr().String(), "DE")
		close(done)
	}()

	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("exit check did not reach the trace endpoint")
	}
	close(stop)
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("exit check did not finish")
	}

	ctrl.mu.Lock()
	ip, country := ctrl.exitIP, ctrl.exitCountry
	ctrl.mu.Unlock()
	if ip != "192.0.2.1" || country != "NL" {
		t.Fatalf("exit after a stopped connection's late trace = %q/%q, want 192.0.2.1/NL", ip, country)
	}
}
