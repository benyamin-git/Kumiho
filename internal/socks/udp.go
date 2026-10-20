package socks

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"time"

	mdns "github.com/miekg/dns"

	"github.com/benyamin-git/kumiho/internal/logging"
)

const (
	// udpAssocBufferSize is the datagram receive buffer (DNS over UDP fits).
	udpAssocBufferSize = 64 << 10
	// udpMaxInflight bounds concurrent resolver calls per association.
	udpMaxInflight = 32
	// udpQueryTimeout is the per-query resolution budget.
	udpQueryTimeout = 8 * time.Second
)

// udpAssociation is one DNS-only UDP relay. The SOCKS client keeps its TCP
// control connection open; when that closes (or the server stops) the relay
// socket is closed and every in-flight query is cancelled.
type udpAssociation struct {
	srv        *Server
	conn       *net.UDPConn // guarded by srv.mu during setup/teardown
	clientIP   net.IP       // the TCP peer; datagram sources must match
	clientPort int          // requested source port; 0 means any port

	ctx    context.Context
	cancel context.CancelFunc
	slots  chan struct{}
	wg     sync.WaitGroup

	closeOnce sync.Once
}

// handleUDPAssociate speaks the DNS-only UDP ASSOCIATE path: it binds a
// loopback relay and keeps the control connection open until the client
// closes it.
func (s *Server) handleUDPAssociate(client net.Conn, atyp byte) {
	target, err := readTarget(client, atyp)
	if err != nil {
		_ = writeReply(client, replyAddrUnsupported, nil)
		return
	}
	if target.port != 53 && target.port != 0 {
		_ = writeReply(client, replyNotAllowed, nil)
		return
	}
	if s.Resolve == nil {
		_ = writeReply(client, replyCmdUnsupported, nil)
		return
	}
	peer, ok := client.RemoteAddr().(*net.TCPAddr)
	if !ok {
		s.logf(logging.Debug, "socks", "refusing UDP ASSOCIATE: peer is not a TCP address")
		_ = writeReply(client, replyNotAllowed, nil)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	assoc := &udpAssociation{
		srv:        s,
		clientIP:   peer.IP,
		clientPort: target.port,
		ctx:        ctx,
		cancel:     cancel,
		slots:      make(chan struct{}, udpMaxInflight),
	}
	// Register before binding so Server.Stop cannot return while a relay
	// socket is still being created.
	if !s.registerUDPAssociation(assoc) {
		cancel()
		_ = writeReply(client, replyNotAllowed, nil)
		return
	}
	defer s.releaseUDPAssociation(assoc)

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		s.logf(logging.Warn, "socks", "could not bind the DNS UDP relay: %v", err)
		_ = writeReply(client, replyGeneralFailure, nil)
		return
	}
	s.mu.Lock()
	assoc.conn = conn
	s.mu.Unlock()

	// The request target address is informational (RFC 1928); reply with the
	// real relay endpoint, never 0.0.0.0:0.
	if err := writeReply(client, replySucceeded, conn.LocalAddr()); err != nil {
		return
	}
	_ = client.SetDeadline(time.Time{})

	assoc.wg.Add(1)
	go assoc.serve()

	// Keep the association alive until the control connection closes.
	_, _ = io.Copy(io.Discard, client)
}

func (s *Server) registerUDPAssociation(a *udpAssociation) bool {
	key := a.clientIP.String()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	if s.udpAssocs == nil {
		s.udpAssocs = make(map[string]*udpAssociation)
	}
	if _, exists := s.udpAssocs[key]; exists {
		return false
	}
	s.udpAssocs[key] = a
	s.udpWG.Add(1)
	return true
}

// releaseUDPAssociation closes the relay and waits for every goroutine that
// belongs to it, so nothing outlives a Stop (or a control-connection close).
func (s *Server) releaseUDPAssociation(a *udpAssociation) {
	a.closeOnce.Do(func() {
		a.cancel()
		s.mu.Lock()
		conn := a.conn
		key := a.clientIP.String()
		if s.udpAssocs[key] == a {
			delete(s.udpAssocs, key)
		}
		s.mu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}
		a.wg.Wait()
		s.udpWG.Done()
	})
}

func (a *udpAssociation) serve() {
	defer a.wg.Done()
	buf := make([]byte, udpAssocBufferSize)
	for {
		n, src, err := a.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		a.handleDatagram(buf[:n], src)
	}
}

// handleDatagram validates one SOCKS5 UDP datagram and dispatches a DNS query.
// Invalid datagrams are dropped with a debug log and never answered.
func (a *udpAssociation) handleDatagram(pkt []byte, src *net.UDPAddr) {
	if !a.validSource(src) {
		return
	}
	req, reason := parseUDPRequest(pkt)
	if reason != "" {
		a.srv.logf(logging.Debug, "socks", "dropping UDP datagram: %s", reason)
		return
	}
	if req.port != 53 {
		a.srv.logf(logging.Debug, "socks", "dropping UDP datagram: destination port %d is not DNS", req.port)
		return
	}
	var query mdns.Msg
	if err := query.Unpack(req.payload); err != nil {
		a.srv.logf(logging.Debug, "socks", "dropping UDP datagram: payload is not DNS")
		return
	}
	select {
	case a.slots <- struct{}{}:
	default:
		a.srv.logf(logging.Debug, "socks", "dropping DNS datagram: in-flight query limit (%d) reached", udpMaxInflight)
		return
	}
	a.wg.Add(1)
	client := *src
	go func() {
		defer a.wg.Done()
		defer func() { <-a.slots }()
		a.resolve(&query, client)
	}()
}

// validSource enforces the RFC 1928 source rules: the datagram must come from
// the TCP peer, and from the requested port when the request named one.
func (a *udpAssociation) validSource(src *net.UDPAddr) bool {
	if !src.IP.Equal(a.clientIP) {
		a.srv.logf(logging.Debug, "socks", "dropping UDP datagram: source address is not the TCP peer")
		return false
	}
	if a.clientPort != 0 && src.Port != a.clientPort {
		a.srv.logf(logging.Debug, "socks", "dropping UDP datagram: source port does not match the request")
		return false
	}
	return true
}

type udpRequest struct {
	port    int
	payload []byte
}

// parseUDPRequest parses RSV, FRAG and DST.ADDR/PORT from a SOCKS5 UDP
// datagram. A non-empty reason means the datagram must be dropped.
func parseUDPRequest(pkt []byte) (udpRequest, string) {
	var req udpRequest
	if len(pkt) < 4 {
		return req, "short header"
	}
	if pkt[0] != 0 || pkt[1] != 0 {
		return req, "reserved bytes are not zero"
	}
	if pkt[2] != 0 {
		return req, "fragmentation is not supported"
	}
	off := 4
	switch pkt[3] {
	case socksAtypIPv4:
		off += net.IPv4len
	case socksAtypIPv6:
		off += net.IPv6len
	case socksAtypDomain:
		if len(pkt) < off+1 {
			return req, "short domain address"
		}
		length := int(pkt[off])
		off++
		if length == 0 {
			return req, "empty domain address"
		}
		off += length
	default:
		return req, "unsupported address type"
	}
	if len(pkt) < off+2 {
		return req, "short destination port"
	}
	req.port = int(binary.BigEndian.Uint16(pkt[off:]))
	off += 2
	req.payload = pkt[off:]
	return req, ""
}

// resolve answers one query through the shared resolver. Errors and nil
// responses become SERVFAIL with the request's ID.
func (a *udpAssociation) resolve(query *mdns.Msg, client net.UDPAddr) {
	ctx, cancel := context.WithTimeout(a.ctx, udpQueryTimeout)
	defer cancel()

	resp, err := a.srv.Resolve(ctx, query)
	if err != nil || resp == nil {
		if err != nil && a.ctx.Err() == nil {
			a.srv.logf(logging.Debug, "socks", "DNS resolution failed; answering SERVFAIL: %v", err)
		}
		resp = new(mdns.Msg)
		resp.SetRcode(query, mdns.RcodeServerFailure)
	} else {
		resp.Id = query.Id
	}
	packed, err := resp.Pack()
	if err != nil {
		a.srv.logf(logging.Debug, "socks", "could not pack DNS response: %v", err)
		return
	}
	if a.ctx.Err() != nil {
		return
	}

	packet := make([]byte, 0, 3+1+net.IPv6len+2+len(packed))
	packet = append(packet, 0, 0, 0) // RSV, RSV, FRAG
	packet = appendSocksAddr(packet, client.IP, client.Port)
	packet = append(packet, packed...)
	if _, err := a.conn.WriteToUDP(packet, &client); err != nil && a.ctx.Err() == nil {
		a.srv.logf(logging.Debug, "socks", "could not send DNS response: %v", err)
	}
}

// appendSocksAddr appends an ATYP/address/port triple.
func appendSocksAddr(b []byte, ip net.IP, port int) []byte {
	if v4 := ip.To4(); v4 != nil {
		b = append(b, socksAtypIPv4)
		b = append(b, v4...)
	} else {
		v6 := ip.To16()
		if v6 == nil {
			v6 = net.IPv6zero
		}
		b = append(b, socksAtypIPv6)
		b = append(b, v6...)
	}
	return binary.BigEndian.AppendUint16(b, uint16(port))
}
