package upstream

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrSessionClosed is returned once the session is dead.
var ErrSessionClosed = errors.New("upstream session is closed")

// ErrDraining is returned when the edge sent GOAWAY and no new streams may
// be opened on this session.
var ErrDraining = errors.New("upstream session is draining (GOAWAY received)")

// ErrStreamRefused marks a REFUSED_STREAM reset, which is safe to retry.
var ErrStreamRefused = errors.New("upstream: edge refused the stream (REFUSED_STREAM)")

// ConnectRejected reports a non-2xx CONNECT response from the edge.
type ConnectRejected struct {
	Status    int
	Authority string
}

func (e *ConnectRejected) Error() string {
	return fmt.Sprintf("upstream rejected CONNECT %s: HTTP %d", e.Authority, e.Status)
}

// ConnectTimeout reports that opening a stream or receiving the CONNECT
// response timed out (maps to SOCKS reply 0x06, like the reference client).
type ConnectTimeout struct {
	Authority string
	Err       error
}

func (e *ConnectTimeout) Error() string {
	return fmt.Sprintf("upstream CONNECT %s timed out: %v", e.Authority, e.Err)
}

func (e *ConnectTimeout) Unwrap() error { return e.Err }

// IsAuthRejected reports whether the edge refused the proxy pass
// (401/403/407). The SOCKS layer latches this and asks for a redial.
func IsAuthRejected(err error) bool {
	var rej *ConnectRejected
	return errors.As(err, &rej) &&
		(rej.Status == http.StatusUnauthorized ||
			rej.Status == http.StatusForbidden ||
			rej.Status == http.StatusProxyAuthRequired)
}

// IsTargetUnreachable reports edge status codes meaning "this destination
// cannot be reached" (502/503/504); the SOCKS layer caches these briefly.
func IsTargetUnreachable(err error) bool {
	var rej *ConnectRejected
	return errors.As(err, &rej) &&
		(rej.Status == http.StatusBadGateway ||
			rej.Status == http.StatusServiceUnavailable ||
			rej.Status == http.StatusGatewayTimeout)
}
