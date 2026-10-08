package upstream

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/net/http2"
)

// stream is one HTTP/2 CONNECT flow, exposed as a net.Conn. Reads consume
// inbound flow-control credit lazily: the window is only replenished when
// the application reads, which bounds per-stream buffering.
type stream struct {
	sess      *Session
	id        uint32
	authority string

	mu         sync.Mutex
	cond       *sync.Cond
	buf        []byte
	off        int
	eof        bool
	err        error
	gotResp    bool
	resp       chan error
	writeEnded bool
	remoteDone bool
	finished   bool
	slotDone   bool

	readDeadline  time.Time
	writeDeadline time.Time
	readTimer     *time.Timer

	sendWindow *window

	received int64 // bytes received from the edge
	credited int64 // bytes already returned to the inbound window
}

func newStream(sess *Session, id uint32, authority string, sendWindow int64) *stream {
	st := &stream{
		sess:       sess,
		id:         id,
		authority:  authority,
		resp:       make(chan error, 1),
		sendWindow: newWindow(sendWindow),
	}
	st.cond = sync.NewCond(&st.mu)
	return st
}

// signalResp delivers the CONNECT response outcome exactly once.
func (st *stream) signalResp(err error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.gotResp {
		return
	}
	st.gotResp = true
	st.resp <- err
}

func (st *stream) hasResp() bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.gotResp
}

func (st *stream) appendData(p []byte) {
	st.mu.Lock()
	st.buf = append(st.buf, p...)
	st.received += int64(len(p))
	st.cond.Broadcast()
	st.mu.Unlock()
}

func (st *stream) appendEOF() {
	st.mu.Lock()
	st.eof = true
	st.remoteDone = true
	st.cond.Broadcast()
	st.mu.Unlock()
}

func (st *stream) appendError(err error) {
	st.mu.Lock()
	if st.err == nil {
		st.err = err
	}
	st.remoteDone = true
	st.cond.Broadcast()
	st.mu.Unlock()
}

// Read implements net.Conn.
func (st *stream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	st.mu.Lock()
	for {
		if st.off < len(st.buf) {
			n := copy(p, st.buf[st.off:])
			st.off += n
			if st.off == len(st.buf) {
				st.buf = st.buf[:0]
				st.off = 0
			}
			st.credited += int64(n)
			st.mu.Unlock()
			st.sess.creditInbound(st.id, n)
			return n, nil
		}
		if st.err != nil {
			err := st.err
			st.mu.Unlock()
			return 0, err
		}
		if st.eof {
			st.mu.Unlock()
			return 0, io.EOF
		}
		if !st.readDeadline.IsZero() && !time.Now().Before(st.readDeadline) {
			st.mu.Unlock()
			return 0, os.ErrDeadlineExceeded
		}
		st.armReadTimerLocked()
		st.cond.Wait()
	}
}

func (st *stream) armReadTimerLocked() {
	if st.readDeadline.IsZero() {
		if st.readTimer != nil {
			st.readTimer.Stop()
			st.readTimer = nil
		}
		return
	}
	d := time.Until(st.readDeadline)
	if d < 0 {
		d = 0
	}
	if st.readTimer != nil {
		st.readTimer.Stop()
	}
	st.readTimer = time.AfterFunc(d, func() {
		st.mu.Lock()
		st.cond.Broadcast()
		st.mu.Unlock()
	})
}

// Write implements net.Conn. It splits payloads into DATA frames that
// respect both flow-control windows and the peer's maximum frame size.
func (st *stream) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		st.mu.Lock()
		if st.writeEnded {
			st.mu.Unlock()
			return written, errors.New("upstream: write side is closed")
		}
		if st.finished {
			st.mu.Unlock()
			return written, net.ErrClosed
		}
		deadline := st.writeDeadline
		st.mu.Unlock()

		ctx := context.Background()
		cancel := context.CancelFunc(func() {})
		if !deadline.IsZero() {
			ctx, cancel = context.WithDeadline(ctx, deadline)
		}

		chunk := int64(len(p))
		if maxFrame := int64(st.sess.maxSendFrameSize()); chunk > maxFrame {
			chunk = maxFrame
		}
		if err := st.sess.connSendWindow.waitPositive(ctx); err != nil {
			cancel()
			return written, writeDeadlineErr(err)
		}
		if err := st.sendWindow.waitPositive(ctx); err != nil {
			cancel()
			return written, writeDeadlineErr(err)
		}
		n := st.sess.connSendWindow.tryTake(chunk)
		if n == 0 {
			cancel()
			continue
		}
		m := st.sendWindow.tryTake(n)
		if m < n {
			st.sess.connSendWindow.add(n - m)
		}
		if m == 0 {
			cancel()
			continue
		}
		if err := st.sess.writeData(st.id, p[:m]); err != nil {
			cancel()
			return written, err
		}
		written += int(m)
		p = p[m:]
		cancel()
	}
	return written, nil
}

func writeDeadlineErr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return os.ErrDeadlineExceeded
	}
	return err
}

// CloseWrite half-closes the flow by sending an empty DATA frame with
// END_STREAM, keeping the read side open.
func (st *stream) CloseWrite() error {
	st.mu.Lock()
	if st.writeEnded || st.finished {
		st.mu.Unlock()
		return nil
	}
	st.writeEnded = true
	st.mu.Unlock()
	return st.sess.writeEmptyDataEndStream(st.id)
}

// Close tears the flow down (RST_STREAM unless the edge already ended it).
func (st *stream) Close() error {
	st.mu.Lock()
	if st.finished {
		st.mu.Unlock()
		return nil
	}
	st.finished = true
	if st.err == nil {
		st.err = net.ErrClosed
	}
	remoteDone := st.remoteDone
	st.cond.Broadcast()
	st.mu.Unlock()

	if !remoteDone {
		_ = st.sess.writeRST(st.id, http2.ErrCodeCancel)
	}
	st.sess.removeStream(st)
	return nil
}

// SetDeadline implements net.Conn.
func (st *stream) SetDeadline(t time.Time) error {
	_ = st.SetReadDeadline(t)
	return st.SetWriteDeadline(t)
}

// SetReadDeadline implements net.Conn.
func (st *stream) SetReadDeadline(t time.Time) error {
	st.mu.Lock()
	st.readDeadline = t
	st.cond.Broadcast()
	st.mu.Unlock()
	return nil
}

// SetWriteDeadline implements net.Conn.
func (st *stream) SetWriteDeadline(t time.Time) error {
	st.mu.Lock()
	st.writeDeadline = t
	st.mu.Unlock()
	return nil
}

type streamAddr struct{ addr string }

func (a streamAddr) Network() string { return "h2connect" }
func (a streamAddr) String() string  { return a.addr }

// LocalAddr implements net.Conn.
func (st *stream) LocalAddr() net.Addr { return streamAddr{addr: st.authority} }

// RemoteAddr implements net.Conn.
func (st *stream) RemoteAddr() net.Addr { return streamAddr{addr: st.authority} }
