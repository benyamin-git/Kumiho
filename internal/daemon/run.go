package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/serverlist"
	"github.com/benyamin-git/kumiho/internal/settings"
	"github.com/benyamin-git/kumiho/internal/version"
)

// Run starts the daemon and serves the control socket until ctx is done.
func Run(ctx context.Context, paths settings.Paths) error {
	store, err := settings.Open(paths)
	if err != nil {
		return err
	}
	cfg := store.EffectiveConfig()
	level, err := logging.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = logging.Info
	}
	ring := logging.NewRing(logging.Options{
		Capacity: 2000,
		Sanitize: logging.Sanitizer(cfg.RedactTargets),
		Sink:     os.Stderr,
		MinLevel: level,
	})
	defer ring.Close()

	ring.Logf(logging.Info, "daemon", "kumiho %s starting", version.String())

	ctrl := New(store, ring, fxa.NewClient())
	if err := ctrl.Start(ctx); err != nil {
		return fmt.Errorf("start session: %w", err)
	}

	srv, err := ipc.Listen(paths.SocketPath())
	if err != nil {
		return err
	}
	defer srv.Close()
	defer os.Remove(paths.SocketPath())
	chownControlSocket(paths.SocketPath())

	// We own the socket now: crash-recovery cleanup must not wipe a live
	// daemon's configuration.
	if runtime.GOOS == "linux" {
		ctrl.CleanupStaleNetcfg()
	}
	ctrl.UseSocketMarking()

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	h := &control{
		ctrl:     ctrl,
		ring:     ring,
		shutdown: cancel,
		conns:    make(map[*ipc.Conn]struct{}),
	}
	ctrl.OnChange(h.broadcastStatus)

	// Renew a stale access token without blocking startup; a permanent
	// rejection clears the session and broadcasts the change.
	if ctrl.Snapshot().Authenticated {
		go func() {
			if err := ctrl.EnsureFreshToken(runCtx); err != nil && runCtx.Err() == nil {
				ring.Logf(logging.Warn, "auth", "session renewal failed: %v", err)
			}
		}()

		// Autoconnect (PLAN.md §4.3): reboot-safe bring-up. The systemd
		// unit orders after network-online.target; the built-in retries
		// cover a network that comes up late.
		go ctrl.Autoconnect(runCtx)
	}

	ring.Logf(logging.Info, "daemon", "control socket ready at %s", srv.Path())
	err = srv.Serve(runCtx, h.handle)
	// Teardown before exit (PLAN.md §4.3): without this, a stopped service
	// leaves the kill-switch rules and nft table behind.
	ctrl.Shutdown()
	if err != nil {
		return err
	}
	ring.Log(logging.Info, "daemon", "shutdown complete")
	return nil
}

type control struct {
	ctrl     *Controller
	ring     *logging.Ring
	shutdown func()

	mu    sync.Mutex
	conns map[*ipc.Conn]struct{}
}

type connState struct {
	cancelLog func()
}

func (h *control) addConn(c *ipc.Conn) {
	h.mu.Lock()
	h.conns[c] = struct{}{}
	h.mu.Unlock()
}

func (h *control) removeConn(c *ipc.Conn) {
	h.mu.Lock()
	delete(h.conns, c)
	h.mu.Unlock()
}

func (h *control) broadcastStatus() {
	st := h.ctrl.Snapshot()
	h.mu.Lock()
	conns := make([]*ipc.Conn, 0, len(h.conns))
	for c := range h.conns {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	for _, c := range conns {
		_ = c.Event(ipc.TypeStatus, st)
	}
}

func (h *control) handle(ctx context.Context, conn *ipc.Conn) error {
	h.addConn(conn)
	defer h.removeConn(conn)

	cs := &connState{}
	defer func() {
		if cs.cancelLog != nil {
			cs.cancelLog()
		}
	}()

	for {
		env, err := conn.Read()
		if err != nil {
			return nil
		}
		h.dispatch(ctx, conn, cs, env)
	}
}

func (h *control) dispatch(ctx context.Context, conn *ipc.Conn, cs *connState, env *ipc.Envelope) {
	replyErr := func(code, msg string) {
		_ = conn.Reply(env.ID, ipc.TypeError, ipc.ErrorPayload{Code: code, Message: msg})
	}
	replyOK := func() {
		_ = conn.Reply(env.ID, ipc.TypeResult, ipc.Result{OK: true})
	}

	switch env.Type {
	case ipc.TypeHello:
		_ = conn.Reply(env.ID, ipc.TypeHello, ipc.HelloResult{
			Version:  version.String(),
			Protocol: ipc.ProtocolVersion,
			OS:       runtime.GOOS,
			Arch:     runtime.GOARCH,
		})

	case ipc.TypeStatus:
		_ = conn.Reply(env.ID, ipc.TypeStatus, h.ctrl.Snapshot())

	case ipc.TypeLocations:
		var p ipc.LocationsPayload
		if err := env.DecodePayload(&p); err != nil {
			replyErr(ipc.CodeBadRequest, err.Error())
			return
		}
		countries, err := h.ctrl.Locations(ctx, p.Refresh)
		if err != nil {
			replyErr(ipc.CodeInternal, err.Error())
			return
		}
		_ = conn.Reply(env.ID, ipc.TypeLocations, ipc.LocationsResult{Countries: countries})

	case ipc.TypeSelectLocation:
		var p ipc.SelectLocationPayload
		if err := env.DecodePayload(&p); err != nil {
			replyErr(ipc.CodeBadRequest, err.Error())
			return
		}
		if err := h.ctrl.SelectLocation(p.CountryCode, p.CityCode, p.Server); err != nil {
			var re *ipc.RemoteError
			if errors.As(err, &re) {
				replyErr(re.Code, re.Message)
			} else {
				replyErr(ipc.CodeInternal, err.Error())
			}
			return
		}
		replyOK()
		h.broadcastStatus()

	case ipc.TypePing:
		var p ipc.PingPayload
		if err := env.DecodePayload(&p); err != nil {
			replyErr(ipc.CodeBadRequest, err.Error())
			return
		}
		replyOK()
		h.pingHosts(ctx, conn, p.Hosts)

	case ipc.TypeLoginEmail:
		var p ipc.LoginEmailPayload
		if err := env.DecodePayload(&p); err != nil {
			replyErr(ipc.CodeBadRequest, err.Error())
			return
		}
		_ = conn.Reply(env.ID, ipc.TypeLoginState, h.ctrl.LoginEmail(p.Email))
		h.broadcastStatus()

	case ipc.TypeLoginPassword:
		var p ipc.LoginPasswordPayload
		if err := env.DecodePayload(&p); err != nil {
			replyErr(ipc.CodeBadRequest, err.Error())
			return
		}
		_ = conn.Reply(env.ID, ipc.TypeLoginState, h.ctrl.LoginPassword(ctx, p.Password))
		h.broadcastStatus()

	case ipc.TypeLogin2FA:
		var p ipc.Login2FAPayload
		if err := env.DecodePayload(&p); err != nil {
			replyErr(ipc.CodeBadRequest, err.Error())
			return
		}
		_ = conn.Reply(env.ID, ipc.TypeLoginState, h.ctrl.Login2FA(ctx, p.Code))
		h.broadcastStatus()

	case ipc.TypeLogout:
		if err := h.ctrl.Logout(); err != nil {
			replyErr(ipc.CodeInternal, err.Error())
			return
		}
		replyOK()
		h.broadcastStatus()

	case ipc.TypeSettingsGet:
		_ = conn.Reply(env.ID, ipc.TypeSettings, h.ctrl.Settings())

	case ipc.TypeSettingsSet:
		var p ipc.SettingsSetPayload
		if err := env.DecodePayload(&p); err != nil {
			replyErr(ipc.CodeBadRequest, err.Error())
			return
		}
		if err := h.ctrl.ApplySetting(p); err != nil {
			var re *ipc.RemoteError
			if errors.As(err, &re) {
				replyErr(re.Code, re.Message)
			} else {
				replyErr(ipc.CodeInternal, err.Error())
			}
			return
		}
		replyOK()
		h.broadcastStatus()

	case ipc.TypeLogsSubscribe:
		var p ipc.LogsSubscribePayload
		if err := env.DecodePayload(&p); err != nil {
			replyErr(ipc.CodeBadRequest, err.Error())
			return
		}
		level := logging.Info
		if p.Level != "" {
			parsed, err := logging.ParseLevel(p.Level)
			if err != nil {
				replyErr(ipc.CodeBadRequest, err.Error())
				return
			}
			level = parsed
		}
		if cs.cancelLog != nil {
			cs.cancelLog()
		}
		ch, cancel := h.ring.Subscribe(level)
		cs.cancelLog = cancel
		replyOK()
		go pumpLogs(ctx, conn, ch)

	case ipc.TypeLogsUnsubscribe:
		if cs.cancelLog != nil {
			cs.cancelLog()
			cs.cancelLog = nil
		}
		replyOK()

	case ipc.TypeLogsClear:
		h.ring.Clear()
		replyOK()

	case ipc.TypeLogsSnapshot:
		var p ipc.LogsSnapshotPayload
		if err := env.DecodePayload(&p); err != nil {
			replyErr(ipc.CodeBadRequest, err.Error())
			return
		}
		min := logging.Debug
		if p.Level != "" {
			parsed, err := logging.ParseLevel(p.Level)
			if err != nil {
				replyErr(ipc.CodeBadRequest, err.Error())
				return
			}
			min = parsed
		}
		_ = conn.Reply(env.ID, ipc.TypeLogSnapshot, ipc.LogSnapshot{
			Entries: filterLogs(h.ring.Snapshot(), min),
		})

	case ipc.TypeAccount:
		actx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		_ = conn.Reply(env.ID, ipc.TypeAccountInfo, h.ctrl.Account(actx))

	case ipc.TypeShutdown:
		uid, ok := conn.PeerUID()
		if !ok || uid != 0 {
			replyErr(ipc.CodeForbidden, "only root may shut down the daemon")
			return
		}
		_ = conn.Reply(env.ID, ipc.TypeResult, ipc.Result{OK: true, Note: "shutting down"})
		if h.shutdown != nil {
			h.shutdown()
		}

	case ipc.TypeConnect:
		var p ipc.ConnectPayload
		if err := env.DecodePayload(&p); err != nil {
			replyErr(ipc.CodeBadRequest, err.Error())
			return
		}
		if err := h.ctrl.Connect(ctx, p); err != nil {
			var re *ipc.RemoteError
			if errors.As(err, &re) {
				replyErr(re.Code, re.Message)
			} else {
				replyErr(ipc.CodeInternal, err.Error())
			}
			return
		}
		replyOK()
		h.broadcastStatus()

	case ipc.TypeDisconnect:
		if err := h.ctrl.Disconnect(); err != nil {
			var re *ipc.RemoteError
			if errors.As(err, &re) {
				replyErr(re.Code, re.Message)
			} else {
				replyErr(ipc.CodeInternal, err.Error())
			}
			return
		}
		replyOK()
		h.broadcastStatus()

	default:
		replyErr(ipc.CodeBadRequest, fmt.Sprintf("unknown request type %q", env.Type))
	}
}

func pumpLogs(ctx context.Context, conn *ipc.Conn, ch <-chan logging.Entry) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-conn.Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.Event(ipc.TypeLog, e); err != nil {
				return
			}
		}
	}
}

// filterLogs returns entries at or above min.
func filterLogs(entries []logging.Entry, min logging.Level) []logging.Entry {
	out := entries[:0]
	for _, e := range entries {
		if e.Level >= min {
			out = append(out, e)
		}
	}
	return out
}

// pingConcurrency caps parallel TCP probes.
const pingConcurrency = 24

// pingHosts measures TCP latency to each host:port target and streams one
// ping_result event per target to the requesting connection.
func (h *control) pingHosts(ctx context.Context, conn *ipc.Conn, hosts []string) {
	sem := make(chan struct{}, pingConcurrency)
	for _, addr := range hosts {
		addr := addr
		go func() {
			sem <- struct{}{}
			defer func() { <-sem }()

			result := ipc.PingResult{Host: addr}
			if host, portStr, err := net.SplitHostPort(addr); err == nil {
				if port, perr := strconv.Atoi(portStr); perr == nil {
					if d, derr := serverlist.PingTCP(ctx, host, port, 0); derr == nil {
						ms := float64(d.Microseconds()) / 1000.0
						result.RTTMS = &ms
					}
				}
			}
			_ = conn.Event(ipc.TypePingResult, result)
		}()
	}
}
