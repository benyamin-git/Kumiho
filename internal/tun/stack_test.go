package tun

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/xjasonlyu/tun2socks/v2/metadata"
)

func TestAdapterMapsMetadata(t *testing.T) {
	var gotHost string
	var gotPort int
	a := &adapter{dial: func(_ context.Context, host string, port int) (net.Conn, error) {
		gotHost, gotPort = host, port
		return nil, errors.New("stop here")
	}}

	md := &metadata.Metadata{
		Network: metadata.TCP,
		DstIP:   netip.MustParseAddr("93.184.216.34"),
		DstPort: 443,
	}
	if _, err := a.DialContext(context.Background(), md); err == nil {
		t.Fatal("expected the fake dial error")
	}
	if gotHost != "93.184.216.34" || gotPort != 443 {
		t.Fatalf("dial got %s:%d, want 93.184.216.34:443", gotHost, gotPort)
	}
}

func TestAdapterRejectsBadMetadata(t *testing.T) {
	a := &adapter{dial: func(context.Context, string, int) (net.Conn, error) {
		t.Fatal("dial must not be called")
		return nil, nil
	}}
	if _, err := a.DialContext(context.Background(), &metadata.Metadata{}); err == nil {
		t.Fatal("flow without a destination IP must fail")
	}
	if _, err := a.DialContext(context.Background(), &metadata.Metadata{
		DstIP:   netip.MustParseAddr("1.1.1.1"),
		DstPort: 0,
	}); err == nil {
		t.Fatal("flow without a port must fail")
	}
	if _, err := a.DialUDP(&metadata.Metadata{DstIP: netip.MustParseAddr("1.1.1.1"), DstPort: 53}); err == nil {
		t.Fatal("UDP must be rejected inside the TUN")
	}
}

// blockingDevice models the TUN fd: Read parks until Close. Engine.Stop must
// close the device first, or stack.Wait deadlocks (M4 acceptance regression).
type blockingDevice struct {
	closed chan struct{}
	once   sync.Once
}

func (d *blockingDevice) Read([]byte) (int, error) {
	<-d.closed
	return 0, os.ErrClosed
}

func (d *blockingDevice) Write(p []byte) (int, error) { return len(p), nil }

func (d *blockingDevice) Close() error {
	d.once.Do(func() { close(d.closed) })
	return nil
}

func TestEngineStopUnblocksDeviceReader(t *testing.T) {
	dev := &blockingDevice{closed: make(chan struct{})}
	eng, err := Start(dev, 1500, func(context.Context, string, int) (net.Conn, error) {
		return nil, errors.New("no dial in this test")
	}, nil)
	if err != nil {
		t.Fatalf("start engine: %v", err)
	}

	done := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		eng.Stop()
		done <- time.Since(start)
	}()
	select {
	case d := <-done:
		if d > 5*time.Second {
			t.Fatalf("Engine.Stop took %s; device Close did not unblock the reader", d)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Engine.Stop hung: device Close did not unblock the reader")
	}
}

// scriptedDevice feeds queued packets to the netstack and records what the
// netstack writes back; Close unblocks pending reads (models the TUN).
type scriptedDevice struct {
	in     chan []byte
	closed chan struct{}
	once   sync.Once
	wrote  chan []byte
}

func newScriptedDevice() *scriptedDevice {
	return &scriptedDevice{
		in:     make(chan []byte, 16),
		closed: make(chan struct{}),
		wrote:  make(chan []byte, 16),
	}
}

func (d *scriptedDevice) feed(pkt []byte) {
	select {
	case d.in <- pkt:
	case <-d.closed:
	}
}

func (d *scriptedDevice) Read(p []byte) (int, error) {
	select {
	case pkt := <-d.in:
		return copy(p, pkt), nil
	case <-d.closed:
		return 0, os.ErrClosed
	}
}

func (d *scriptedDevice) Write(p []byte) (int, error) {
	select {
	case d.wrote <- append([]byte(nil), p...):
	default:
	}
	return len(p), nil
}

func (d *scriptedDevice) Close() error {
	d.once.Do(func() { close(d.closed) })
	return nil
}

// TestEngineForwardsTCPFlowToDialer completes a TCP handshake through the
// netstack and asserts the flow reaches the proxy dialer. gVisor's TCP
// forwarder only hands the connection over after the client's final ACK, so
// the test answers the SYN-ACK that the netstack writes back.
func TestEngineForwardsTCPFlowToDialer(t *testing.T) {
	const (
		clientISN = 1
		srcPort   = 40000
		dstPort   = 443
	)
	src := [4]byte{10, 8, 0, 2}
	dst := [4]byte{192, 0, 2, 1}

	dev := newScriptedDevice()
	defer dev.Close()
	dev.feed(tcpPacket(src, dst, srcPort, dstPort, clientISN, 0, 0x02)) // SYN

	dialed := make(chan string, 1)
	eng, err := Start(dev, 1500, func(_ context.Context, host string, port int) (net.Conn, error) {
		select {
		case dialed <- net.JoinHostPort(host, strconv.Itoa(port)):
		default:
		}
		return nil, errors.New("dial stub")
	}, nil)
	if err != nil {
		t.Fatalf("start engine: %v", err)
	}
	defer eng.Stop()

	// Answer the netstack's SYN-ACK to complete the handshake.
	go func() {
		for {
			select {
			case pkt := <-dev.wrote:
				if len(pkt) >= 40 && pkt[9] == 6 && pkt[33]&0x12 == 0x12 {
					ack := binary.BigEndian.Uint32(pkt[24:28]) + 1
					dev.feed(tcpPacket(src, dst, srcPort, dstPort, clientISN+1, ack, 0x10))
				}
			case <-dev.closed:
				return
			}
		}
	}()

	select {
	case got := <-dialed:
		if got != "192.0.2.1:443" {
			t.Fatalf("dialer got %q, want 192.0.2.1:443", got)
		}
	case <-time.After(10 * time.Second):
		for {
			select {
			case pkt := <-dev.wrote:
				t.Logf("netstack wrote %d bytes: % x", len(pkt), pkt)
			default:
				t.Fatal("the TCP flow never reached the proxy dialer")
			}
		}
	}
}

// tcpPacket builds a minimal valid IPv4 TCP segment.
func tcpPacket(src, dst [4]byte, sport, dport uint16, seq, ack uint32, flags byte) []byte {
	pkt := make([]byte, 40)
	pkt[0] = 0x45 // IPv4, IHL 5
	binary.BigEndian.PutUint16(pkt[2:], uint16(len(pkt)))
	binary.BigEndian.PutUint16(pkt[4:], 1)
	binary.BigEndian.PutUint16(pkt[6:], 0x4000) // DF
	pkt[8] = 64
	pkt[9] = 6 // TCP
	copy(pkt[12:16], src[:])
	copy(pkt[16:20], dst[:])
	binary.BigEndian.PutUint16(pkt[10:], l3checksum(pkt[:20]))

	binary.BigEndian.PutUint16(pkt[20:], sport)
	binary.BigEndian.PutUint16(pkt[22:], dport)
	binary.BigEndian.PutUint32(pkt[24:], seq)
	binary.BigEndian.PutUint32(pkt[28:], ack)
	pkt[32] = 5 << 4 // data offset
	pkt[33] = flags
	binary.BigEndian.PutUint16(pkt[34:], 0xffff)

	var sum []byte
	sum = append(sum, src[:]...)
	sum = append(sum, dst[:]...)
	sum = append(sum, 0, 6)
	sum = append(sum, byte(len(pkt[20:])>>8), byte(len(pkt[20:])))
	sum = append(sum, pkt[20:]...)
	binary.BigEndian.PutUint16(pkt[36:], l3checksum(sum))
	return pkt
}

// l3checksum computes the 16-bit one's complement checksum.
func l3checksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum)
}
