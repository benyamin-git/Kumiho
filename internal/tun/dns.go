package tun

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"sync"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/xjasonlyu/tun2socks/v2/metadata"
)

// DNSHandler answers one parsed DNS query. internal/dns.Server implements it;
// the userspace path shares that server with the SOCKS resolver and its cache.
type DNSHandler interface {
	HandleQuery(req *mdns.Msg, overUDP bool) *mdns.Msg
}

// Option customizes the packet stack at Start.
type Option func(*adapter) error

// WithDNS makes the stack answer DNS queries addressed to the routed
// in-tunnel resolver. addr is the resolver address the VpnService advertises
// (host:53); it must use port 53 and a numeric IP. No host socket is bound:
// the OS routes the query through the TUN and the adapter answers it from the
// shared strict resolver. This is the Android path because apps cannot bind
// port 53; desktop keeps its privileged host listener.
func WithDNS(handler DNSHandler, addr string) Option {
	return func(a *adapter) error {
		if handler == nil {
			return errors.New("tun: a DNS handler is required")
		}
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return fmt.Errorf("tun: DNS address %q: %w", addr, err)
		}
		if port != "53" {
			return fmt.Errorf("tun: DNS address %q must use port 53", addr)
		}
		ip, err := netip.ParseAddr(host)
		if err != nil {
			return fmt.Errorf("tun: DNS address %q: %w", addr, err)
		}
		a.dns = handler
		a.dnsHost = ip.Unmap()
		a.dnsAddr = &net.UDPAddr{IP: net.IP(ip.AsSlice()), Port: 53}
		return nil
	}
}

func (a *adapter) isTunnelDNS(m *metadata.Metadata) bool {
	return a.dns != nil && a.dnsHost.IsValid() && m != nil &&
		m.DstIP.IsValid() && m.DstPort == 53 && m.DstIP.Unmap() == a.dnsHost
}

// maxDNSResponse is the DNS-over-TCP framing limit (the length prefix is a
// uint16). UDP responses stay within the EDNS0 size the server advertises.
const maxDNSResponse = 65535

// maxDNSQueue bounds the responses a single flow can hold when the packet
// stack has not read them yet.
const maxDNSQueue = 64

func answerQuery(handler DNSHandler, req *mdns.Msg, overUDP bool) *mdns.Msg {
	resp := handler.HandleQuery(req, overUDP)
	if resp == nil {
		resp = new(mdns.Msg)
		resp.SetRcode(req, mdns.RcodeServerFailure)
	}
	return resp
}

func packDNSResponse(handler DNSHandler, req *mdns.Msg, overUDP bool) ([]byte, error) {
	raw, err := answerQuery(handler, req, overUDP).Pack()
	if err == nil && len(raw) <= maxDNSResponse {
		return raw, nil
	}
	fail := new(mdns.Msg)
	fail.SetRcode(req, mdns.RcodeServerFailure)
	raw, ferr := fail.Pack()
	if ferr != nil {
		return nil, ferr
	}
	return raw, nil
}

func waitForSignal(changed <-chan struct{}, deadline time.Time) error {
	var timer *time.Timer
	var timerC <-chan time.Time
	if !deadline.IsZero() {
		d := time.Until(deadline)
		if d <= 0 {
			return os.ErrDeadlineExceeded
		}
		timer = time.NewTimer(d)
		timerC = timer.C
		defer timer.Stop()
	}
	select {
	case <-changed:
		return nil
	case <-timerC:
		return os.ErrDeadlineExceeded
	}
}

func signal(changed chan struct{}) {
	select {
	case changed <- struct{}{}:
	default:
	}
}

// dnsPacketConn is the userspace UDP endpoint for the advertised resolver.
// tun2socks' UDP pipe writes one query with WriteTo and reads the answer with
// ReadFrom; the source address must match the flow destination so the
// symmetric-NAT wrapper accepts the packet.
type dnsPacketConn struct {
	handler DNSHandler
	local   *net.UDPAddr

	mu       sync.Mutex
	out      [][]byte
	changed  chan struct{}
	closed   bool
	deadline time.Time
}

func newDNSPacketConn(handler DNSHandler, local *net.UDPAddr) *dnsPacketConn {
	return &dnsPacketConn{
		handler: handler,
		local:   local,
		changed: make(chan struct{}, 1),
	}
}

func (c *dnsPacketConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	req := new(mdns.Msg)
	if err := req.Unpack(p); err != nil {
		return 0, fmt.Errorf("tun: DNS query: %w", err)
	}
	raw, err := packDNSResponse(c.handler, req, true)
	if err != nil {
		return 0, err
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, net.ErrClosed
	}
	if len(c.out) >= maxDNSQueue {
		c.out = c.out[1:]
	}
	c.out = append(c.out, raw)
	c.mu.Unlock()
	signal(c.changed)
	return len(p), nil
}

func (c *dnsPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		c.mu.Lock()
		if len(c.out) > 0 {
			raw := c.out[0]
			c.out = c.out[1:]
			n := copy(p, raw)
			c.mu.Unlock()
			return n, c.local, nil
		}
		if c.closed {
			c.mu.Unlock()
			return 0, nil, net.ErrClosed
		}
		deadline := c.deadline
		c.mu.Unlock()

		if err := waitForSignal(c.changed, deadline); err != nil {
			return 0, nil, err
		}
	}
}

func (c *dnsPacketConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	signal(c.changed)
	return nil
}

func (c *dnsPacketConn) LocalAddr() net.Addr { return c.local }

func (c *dnsPacketConn) SetDeadline(t time.Time) error { return c.SetReadDeadline(t) }

func (c *dnsPacketConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadline = t
	c.mu.Unlock()
	signal(c.changed)
	return nil
}

func (c *dnsPacketConn) SetWriteDeadline(time.Time) error { return nil }

var _ net.PacketConn = (*dnsPacketConn)(nil)

// dnsTCPConn answers DNS-over-TCP for clients that retry after a truncated
// UDP response. It implements net.Conn over the length-prefixed stream that
// tun2socks' TCP pipe copies.
type dnsTCPConn struct {
	handler DNSHandler
	local   *net.UDPAddr

	mu       sync.Mutex
	in       bytes.Buffer
	out      bytes.Buffer
	changed  chan struct{}
	closed   bool
	deadline time.Time
}

func newDNSTCPConn(handler DNSHandler, local *net.UDPAddr) *dnsTCPConn {
	return &dnsTCPConn{
		handler: handler,
		local:   local,
		changed: make(chan struct{}, 1),
	}
}

func (c *dnsTCPConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, net.ErrClosed
	}
	c.in.Write(p)
	c.mu.Unlock()

	for {
		c.mu.Lock()
		if c.in.Len() < 2 {
			c.mu.Unlock()
			break
		}
		frame := c.in.Bytes()
		n := int(binary.BigEndian.Uint16(frame[:2]))
		if n == 0 {
			c.mu.Unlock()
			return 0, errors.New("tun: empty DNS TCP query")
		}
		if c.in.Len() < 2+n {
			c.mu.Unlock()
			break
		}
		query := make([]byte, n)
		copy(query, frame[2:2+n])
		c.in.Next(2 + n)
		c.mu.Unlock()

		req := new(mdns.Msg)
		if err := req.Unpack(query); err != nil {
			return 0, fmt.Errorf("tun: DNS TCP query: %w", err)
		}
		raw, err := packDNSResponse(c.handler, req, false)
		if err != nil {
			return 0, err
		}

		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return 0, net.ErrClosed
		}
		var hdr [2]byte
		binary.BigEndian.PutUint16(hdr[:], uint16(len(raw)))
		c.out.Write(hdr[:])
		c.out.Write(raw)
		c.mu.Unlock()
		signal(c.changed)
	}
	return len(p), nil
}

func (c *dnsTCPConn) Read(p []byte) (int, error) {
	for {
		c.mu.Lock()
		if c.out.Len() > 0 {
			n, _ := c.out.Read(p)
			c.mu.Unlock()
			return n, nil
		}
		if c.closed {
			c.mu.Unlock()
			return 0, io.EOF
		}
		deadline := c.deadline
		c.mu.Unlock()

		if err := waitForSignal(c.changed, deadline); err != nil {
			return 0, err
		}
	}
}

func (c *dnsTCPConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	signal(c.changed)
	return nil
}

func (c *dnsTCPConn) LocalAddr() net.Addr { return c.local }

func (c *dnsTCPConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4zero, Port: 0}
}

func (c *dnsTCPConn) SetDeadline(t time.Time) error { return c.SetReadDeadline(t) }

func (c *dnsTCPConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadline = t
	c.mu.Unlock()
	signal(c.changed)
	return nil
}

func (c *dnsTCPConn) SetWriteDeadline(time.Time) error { return nil }

var _ net.Conn = (*dnsTCPConn)(nil)
