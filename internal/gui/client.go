package gui

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/platform/auto"
)

const (
	// maxDialAttempts bounds the dial retries before the control pipe is
	// reported unreachable (inventory §behavioral: 3 attempts).
	maxDialAttempts = 3
	// initialDialBackoff is the delay before the first dial retry; it
	// doubles after every attempt (inventory §behavioral: 200 ms doubling).
	initialDialBackoff = 200 * time.Millisecond
	// eventBuffer mirrors the ipc client's event buffer.
	eventBuffer = 256
)

// ErrDaemonUnreachable reports that the control pipe could not be dialed.
// DaemonClient wraps it on a dial failure; consumers check errors.Is to flip
// the "Daemon unreachable" gate (inventory §behavioral).
var ErrDaemonUnreachable = errors.New("daemon unreachable")

// DaemonClient implements Daemon over the local control pipe. The connection
// is dialed lazily and re-dialed on demand: a request that loses its pipe
// drops the dead connection and reports the failure, and the next request
// re-dials it with the bounded backoff. Requests are never re-sent, because
// the daemon may have processed one before its reply was lost (inventory
// §behavioral: a broken pipe is dropped, the next request re-dials).
type DaemonClient struct {
	dial    func(context.Context) (*ipc.Client, error)
	backoff time.Duration

	mu     sync.Mutex
	conn   *ipc.Client
	closed bool
	events chan *ipc.Envelope
}

// NewDaemonClient returns a client for the current platform's control
// endpoint.
func NewDaemonClient() *DaemonClient {
	return &DaemonClient{
		dial:    dialControlEndpoint,
		backoff: initialDialBackoff,
		events:  make(chan *ipc.Envelope, eventBuffer),
	}
}

// dialControlEndpoint dials the platform's control endpoint.
func dialControlEndpoint(ctx context.Context) (*ipc.Client, error) {
	return ipc.DefaultTransport().Dial(ctx, auto.Current().ControlEndpoint())
}

// Close releases the control connection. The event channel stays open; the
// consumer simply stops receiving events.
func (c *DaemonClient) Close() error {
	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.closed = true
	c.mu.Unlock()
	if conn != nil {
		return conn.Close()
	}
	return nil
}

// Events returns the stable event channel; live events from the current
// connection are relayed into it, so the channel identity never changes.
func (c *DaemonClient) Events() <-chan *ipc.Envelope { return c.events }

// connect returns the live connection, dialing with backoff when needed.
// Concurrent callers share one connection.
func (c *DaemonClient) connect(ctx context.Context) (*ipc.Client, error) {
	c.mu.Lock()
	if c.conn != nil {
		conn := c.conn
		c.mu.Unlock()
		return conn, nil
	}
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("daemon client closed")
	}
	c.mu.Unlock()

	var lastErr error
	delay := c.backoff
	for attempt := 0; attempt < maxDialAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("%w: %w", ErrDaemonUnreachable, lastErr)
			case <-time.After(delay):
				delay *= 2
			}
		}
		conn, err := c.dial(ctx)
		if err == nil {
			return c.adopt(conn)
		}
		lastErr = err
	}
	return nil, fmt.Errorf("%w: %w", ErrDaemonUnreachable, lastErr)
}

// adopt records a freshly dialed connection, or closes it in favor of the
// one another caller already installed.
func (c *DaemonClient) adopt(conn *ipc.Client) (*ipc.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		_ = conn.Close()
		return nil, errors.New("daemon client closed")
	}
	if c.conn != nil {
		_ = conn.Close()
		return c.conn, nil
	}
	c.conn = conn
	go c.forward(conn)
	return conn, nil
}

// forward relays one connection's events into the stable event channel and
// clears the connection when it ends, so the next call re-dials.
func (c *DaemonClient) forward(conn *ipc.Client) {
	for env := range conn.Events() {
		select {
		case c.events <- env:
		default: // slow consumer: drop events rather than block the reader
		}
	}
	c.mu.Lock()
	if c.conn == conn {
		c.conn = nil
	}
	c.mu.Unlock()
}

// call performs one request. A transport failure drops the dead connection
// so the next request re-dials; the request itself is never re-sent. Daemon
// errors pass through unchanged.
func (c *DaemonClient) call(ctx context.Context, typ string, payload any) (*ipc.Envelope, error) {
	conn, err := c.connect(ctx)
	if err != nil {
		return nil, err
	}
	env, err := conn.Call(ctx, typ, payload)
	if err != nil {
		var remote *api.RemoteError
		if !errors.As(err, &remote) && ctx.Err() == nil {
			c.drop(conn)
		}
		return nil, err
	}
	return env, nil
}

func (c *DaemonClient) drop(conn *ipc.Client) {
	c.mu.Lock()
	if c.conn == conn {
		c.conn = nil
	}
	c.mu.Unlock()
	_ = conn.Close()
}

// dec performs a request and decodes its payload into out.
func (c *DaemonClient) dec(ctx context.Context, typ string, payload any, out any) error {
	env, err := c.call(ctx, typ, payload)
	if err != nil {
		return err
	}
	return env.DecodePayload(out)
}

// Status implements Daemon.
func (c *DaemonClient) Status(ctx context.Context) (api.Status, error) {
	var st api.Status
	err := c.dec(ctx, ipc.TypeStatus, nil, &st)
	return st, err
}

// Connect implements Daemon.
func (c *DaemonClient) Connect(ctx context.Context, p api.ConnectPayload) (api.Result, error) {
	var res api.Result
	err := c.dec(ctx, ipc.TypeConnect, p, &res)
	return res, err
}

// Disconnect implements Daemon.
func (c *DaemonClient) Disconnect(ctx context.Context) (api.Result, error) {
	var res api.Result
	err := c.dec(ctx, ipc.TypeDisconnect, nil, &res)
	return res, err
}

// SelectLocation implements Daemon.
func (c *DaemonClient) SelectLocation(ctx context.Context, p api.SelectLocationPayload) (api.Result, error) {
	var res api.Result
	err := c.dec(ctx, ipc.TypeSelectLocation, p, &res)
	return res, err
}

// Locations implements Daemon.
func (c *DaemonClient) Locations(ctx context.Context, refresh bool) (api.LocationsResult, error) {
	var res api.LocationsResult
	err := c.dec(ctx, ipc.TypeLocations, api.LocationsPayload{Refresh: refresh}, &res)
	return res, err
}

// Ping implements Daemon.
func (c *DaemonClient) Ping(ctx context.Context, hosts []string) (api.Result, error) {
	var res api.Result
	err := c.dec(ctx, ipc.TypePing, api.PingPayload{Hosts: hosts}, &res)
	return res, err
}

// Settings implements Daemon.
func (c *DaemonClient) Settings(ctx context.Context) (api.SettingsView, error) {
	var view api.SettingsView
	err := c.dec(ctx, ipc.TypeSettingsGet, nil, &view)
	return view, err
}

// SetSetting implements Daemon.
func (c *DaemonClient) SetSetting(ctx context.Context, p api.SettingsSetPayload) (api.Result, error) {
	var res api.Result
	err := c.dec(ctx, ipc.TypeSettingsSet, p, &res)
	return res, err
}

// Account implements Daemon.
func (c *DaemonClient) Account(ctx context.Context) (api.AccountInfo, error) {
	var info api.AccountInfo
	err := c.dec(ctx, ipc.TypeAccount, nil, &info)
	return info, err
}

// LogsSnapshot implements Daemon.
func (c *DaemonClient) LogsSnapshot(ctx context.Context, level string) (api.LogSnapshot, error) {
	var snap api.LogSnapshot
	err := c.dec(ctx, ipc.TypeLogsSnapshot, api.LogsSnapshotPayload{Level: level}, &snap)
	return snap, err
}

// LogsSubscribe implements Daemon.
func (c *DaemonClient) LogsSubscribe(ctx context.Context, level string) (api.Result, error) {
	var res api.Result
	err := c.dec(ctx, ipc.TypeLogsSubscribe, api.LogsSubscribePayload{Level: level}, &res)
	return res, err
}

// LogsUnsubscribe implements Daemon.
func (c *DaemonClient) LogsUnsubscribe(ctx context.Context) (api.Result, error) {
	var res api.Result
	err := c.dec(ctx, ipc.TypeLogsUnsubscribe, nil, &res)
	return res, err
}

// LogsClear implements Daemon.
func (c *DaemonClient) LogsClear(ctx context.Context) (api.Result, error) {
	var res api.Result
	err := c.dec(ctx, ipc.TypeLogsClear, nil, &res)
	return res, err
}

// LoginEmail implements Daemon.
func (c *DaemonClient) LoginEmail(ctx context.Context, p api.LoginEmailPayload) (api.LoginState, error) {
	var st api.LoginState
	err := c.dec(ctx, ipc.TypeLoginEmail, p, &st)
	return st, err
}

// LoginPassword implements Daemon.
func (c *DaemonClient) LoginPassword(ctx context.Context, p api.LoginPasswordPayload) (api.LoginState, error) {
	var st api.LoginState
	err := c.dec(ctx, ipc.TypeLoginPassword, p, &st)
	return st, err
}

// Login2FA implements Daemon.
func (c *DaemonClient) Login2FA(ctx context.Context, p api.Login2FAPayload) (api.LoginState, error) {
	var st api.LoginState
	err := c.dec(ctx, ipc.TypeLogin2FA, p, &st)
	return st, err
}

// Logout implements Daemon.
func (c *DaemonClient) Logout(ctx context.Context) (api.Result, error) {
	var res api.Result
	err := c.dec(ctx, ipc.TypeLogout, nil, &res)
	return res, err
}
