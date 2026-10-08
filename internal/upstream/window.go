package upstream

import (
	"context"
	"sync"
)

// window is a flow-control credit counter. Zero or negative credit means the
// sender must wait; add() wakes all waiters.
type window struct {
	mu     sync.Mutex
	avail  int64
	waiter chan struct{}
	closed chan struct{}
	done   bool
}

func newWindow(initial int64) *window {
	return &window{avail: initial, waiter: make(chan struct{}), closed: make(chan struct{})}
}

// shut unblocks every waiter (the session died).
func (w *window) shut() {
	w.mu.Lock()
	if !w.done {
		w.done = true
		close(w.closed)
	}
	w.mu.Unlock()
}

// add credits n bytes (n may be negative when the peer lowers
// SETTINGS_INITIAL_WINDOW_SIZE).
func (w *window) add(n int64) {
	w.mu.Lock()
	w.avail += n
	close(w.waiter)
	w.waiter = make(chan struct{})
	w.mu.Unlock()
}

// waitPositive blocks until the window has credit, ctx is done, or the
// session dies.
func (w *window) waitPositive(ctx context.Context) error {
	for {
		w.mu.Lock()
		if w.done {
			w.mu.Unlock()
			return ErrSessionClosed
		}
		if w.avail > 0 {
			w.mu.Unlock()
			return nil
		}
		ch := w.waiter
		closedCh := w.closed
		w.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-closedCh:
			return ErrSessionClosed
		case <-ch:
		}
	}
}

// tryTake removes and returns up to max credit (0 when none is available).
func (w *window) tryTake(max int64) int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.avail <= 0 {
		return 0
	}
	n := w.avail
	if n > max {
		n = max
	}
	w.avail -= n
	return n
}
