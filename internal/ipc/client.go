package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
)

// Client is a control connection to the daemon.
type Client struct {
	conn net.Conn
	enc  *json.Encoder
	dec  *json.Decoder

	writeMu sync.Mutex
	mu      sync.Mutex
	closed  bool
	err     error
	pending map[string]chan *Envelope
	events  chan *Envelope

	nextID    atomic.Uint64
	closeOnce sync.Once
}

// Dial connects to the daemon control socket.
func Dial(ctx context.Context, path string) (*Client, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	c := &Client{
		conn:    conn,
		enc:     json.NewEncoder(conn),
		dec:     json.NewDecoder(conn),
		pending: make(map[string]chan *Envelope),
		events:  make(chan *Envelope, 256),
	}
	go c.readLoop()
	return c, nil
}

// Close terminates the connection.
func (c *Client) Close() error {
	err := c.conn.Close()
	c.fail(errors.New("connection closed"))
	return err
}

// Events yields unsolicited server events (status, log). The channel is
// closed when the connection ends.
func (c *Client) Events() <-chan *Envelope {
	return c.events
}

// Call sends a request and waits for the matching response. A structured
// daemon error is returned as *RemoteError.
func (c *Client) Call(ctx context.Context, typ string, payload any) (*Envelope, error) {
	id := strconv.FormatUint(c.nextID.Add(1), 10)
	env, err := NewEnvelope(id, typ, payload)
	if err != nil {
		return nil, err
	}

	ch := make(chan *Envelope, 1)
	c.mu.Lock()
	if c.closed {
		err := c.err
		c.mu.Unlock()
		if err == nil {
			err = errors.New("connection closed")
		}
		return nil, err
	}
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.write(env); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case resp, ok := <-ch:
		if !ok || resp == nil {
			c.mu.Lock()
			err := c.err
			c.mu.Unlock()
			if err == nil {
				err = errors.New("connection closed")
			}
			return nil, err
		}
		if resp.Type == TypeError {
			var e ErrorPayload
			if err := resp.DecodePayload(&e); err != nil {
				return nil, err
			}
			return nil, &e
		}
		return resp, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (c *Client) write(env *Envelope) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.enc.Encode(env)
}

func (c *Client) readLoop() {
	for {
		var env Envelope
		if err := c.dec.Decode(&env); err != nil {
			c.fail(err)
			return
		}
		if env.ID != "" {
			c.mu.Lock()
			ch := c.pending[env.ID]
			delete(c.pending, env.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- &env
			}
			continue
		}
		select {
		case c.events <- &env:
		default: // slow consumer: drop events rather than block the reader
		}
	}
}

func (c *Client) fail(err error) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.err = err
		for id, ch := range c.pending {
			delete(c.pending, id)
			close(ch)
		}
		c.mu.Unlock()
		close(c.events)
	})
}

// DecodePayload is a convenience wrapper erroring with context on failure.
func decodeStatus(env *Envelope) (Status, error) {
	var st Status
	if err := env.DecodePayload(&st); err != nil {
		return st, fmt.Errorf("status response: %w", err)
	}
	return st, nil
}

// Status performs a status request and decodes the snapshot.
func (c *Client) Status(ctx context.Context) (Status, error) {
	env, err := c.Call(ctx, TypeStatus, nil)
	if err != nil {
		return Status{}, err
	}
	return decodeStatus(env)
}
