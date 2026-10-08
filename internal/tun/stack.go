// Package tun creates the TUN device and runs the userspace TCP/IP stack
// that turns tunneled flows into HTTP/2 CONNECT streams (PLAN.md §3.1, §3.7).
package tun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/device/iobased"
	t2slog "github.com/xjasonlyu/tun2socks/v2/log"
	"github.com/xjasonlyu/tun2socks/v2/metadata"
	"github.com/xjasonlyu/tun2socks/v2/proxy"
	"github.com/xjasonlyu/tun2socks/v2/proxy/reject"
	"github.com/xjasonlyu/tun2socks/v2/tunnel"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

// Dialer opens one tunnelled TCP stream (upstream.Session.OpenStream).
type Dialer func(ctx context.Context, host string, port int) (net.Conn, error)

// Engine terminates TUN TCP flows in a gVisor netstack and hands them to the
// HTTP/2 session through Dialer — no extra SOCKS hop (PLAN.md §3.7).
type Engine struct {
	stack *stack.Stack
	dev   io.ReadWriteCloser
}

// Start wires the device into a fresh netstack and installs the proxy
// adapter on the shared tun2socks tunnel. The device's Close must unblock a
// concurrent Read (the Linux TUN fd is poller-backed for exactly this);
// logf, when set, receives per-flow diagnostics.
func Start(dev io.ReadWriteCloser, mtu int, dial Dialer, logf func(format string, args ...any)) (*Engine, error) {
	if dev == nil {
		return nil, errors.New("tun: no packet device")
	}
	if mtu <= 0 {
		return nil, fmt.Errorf("tun: invalid MTU %d", mtu)
	}
	quietTun2socksLogs()
	ep, err := iobased.New(dev, uint32(mtu), 0)
	if err != nil {
		return nil, fmt.Errorf("tun: link endpoint: %w", err)
	}
	st, err := core.CreateStack(&core.Config{
		LinkEndpoint:     ep,
		TransportHandler: tunnel.T(),
	})
	if err != nil {
		return nil, fmt.Errorf("tun: netstack: %w", err)
	}
	tunnel.T().SetProxy(&adapter{dial: dial, logf: logf})
	return &Engine{stack: st, dev: dev}, nil
}

// shutdownTimeout bounds the netstack wait so a stuck loop can never wedge
// teardown; in the worst case the wait goroutine exits with the process.
const shutdownTimeout = 10 * time.Second

// quietTun2socksLogs downgrades tun2socks' internal logger from its default
// info level: it prints a JSON line per TCP connection directly to stderr,
// which spams journald. Warnings and errors still surface (and Fatalf keeps
// its exit semantics, unlike a nop logger); per-flow diagnostics come from
// the adapter's logf at debug level.
func quietTun2socksLogs() {
	l, err := t2slog.NewLeveled(t2slog.WarnLevel)
	if err == nil {
		t2slog.SetLogger(l)
	}
}

// Stop detaches the adapter and shuts the netstack down. The device is
// closed FIRST: tun2socks' dispatch loop only returns when a device read
// fails, and that exit is what cancels its outbound loop — waiting for the
// stack before closing the device deadlocks (M4 acceptance regression).
func (e *Engine) Stop() {
	if e == nil {
		return
	}
	tunnel.T().SetProxy(&reject.Reject{})
	if e.dev != nil {
		_ = e.dev.Close()
	}
	st := e.stack
	e.stack = nil
	if st == nil {
		return
	}
	st.Close()
	done := make(chan struct{})
	go func() {
		st.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(shutdownTimeout):
	}
}

// adapter implements tun2socks' proxy.Proxy over Dialer.
type adapter struct {
	dial Dialer
	// logf receives per-flow diagnostics (may be nil).
	logf func(format string, args ...any)
}

func (a *adapter) debug(format string, args ...any) {
	if a.logf != nil {
		a.logf(format, args...)
	}
}

func (a *adapter) DialContext(ctx context.Context, m *metadata.Metadata) (net.Conn, error) {
	if !m.DstIP.IsValid() {
		return nil, errors.New("tun: flow with no destination IP")
	}
	port := int(m.DstPort)
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("tun: flow with invalid destination port %d", port)
	}
	if a.dial == nil {
		return nil, errors.New("tun: no dialer configured")
	}
	a.debug("flow %s:%d: opening tunnel stream", m.DstIP, port)
	conn, err := a.dial(ctx, m.DstIP.String(), port)
	if err != nil {
		a.debug("flow %s:%d: dial failed: %v", m.DstIP, port, err)
		return nil, err
	}
	a.debug("flow %s:%d: stream open", m.DstIP, port)
	return conn, nil
}

// DialUDP rejects UDP inside the TUN on purpose (PLAN.md §3.7): the only DNS
// path is the local 10.8.0.2 listener, and QUIC falls back to TCP.
func (a *adapter) DialUDP(*metadata.Metadata) (net.PacketConn, error) {
	return nil, errors.New("tun: UDP is not tunnelled")
}

var _ proxy.Proxy = (*adapter)(nil)
