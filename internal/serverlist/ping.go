package serverlist

import (
	"context"
	"net"
	"strconv"
	"time"
)

// DefaultPingTimeout mirrors the reference client's TCP ping timeout.
const DefaultPingTimeout = 1500 * time.Millisecond

// PingTCP measures TCP connect latency to host:port (direct connect, same
// behavior as the reference PingUtil).
func PingTCP(ctx context.Context, host string, port int, timeout time.Duration) (time.Duration, error) {
	if timeout <= 0 {
		timeout = DefaultPingTimeout
	}
	dialer := net.Dialer{Timeout: timeout}
	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return 0, err
	}
	_ = conn.Close()
	return time.Since(start), nil
}
