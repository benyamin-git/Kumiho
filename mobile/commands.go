package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/settings"
	"github.com/benyamin-git/kumiho/internal/version"
)

// Command deadlines from the task table. Zero-timeout commands are
// synchronous and use the facade root context directly.
const (
	locationsTimeout      = 45 * time.Second
	connectTimeout        = 120 * time.Second
	disconnectTimeout     = 20 * time.Second
	settingsSetTimeout    = 20 * time.Second
	relayTimeout          = 20 * time.Second
	regionTimeout         = 25 * time.Second
	prepareTimeout        = 90 * time.Second
	accountTimeout        = 35 * time.Second
	accountsTimeout       = 10 * time.Second
	accountCommandTimeout = 10 * time.Second
	// loginCompleteTimeout covers a token exchange plus a profile lookup, each
	// with its own 30 s engine HTTP deadline. It must not reuse the local 10 s
	// account-command budget.
	loginCompleteTimeout = 90 * time.Second
	loginPasswordTimeout = 60 * time.Second
	login2FATimeout      = 60 * time.Second
	resetStateTimeout    = 10 * time.Second
	doctorTimeout        = 30 * time.Second
	revokeTimeout        = 10 * time.Second

	// maxPingHosts bounds one ping request.
	maxPingHosts = 512
)

// logAppendPayload replays one entry the app captured before a crash.
type logAppendPayload struct {
	Level string `json:"level"`
	Tag   string `json:"tag"`
	Msg   string `json:"msg"`
}

// doctorPayload carries the Android facts Kotlin collected.
type doctorPayload struct {
	Facts doctorFacts `json:"facts"`
}

// dispatch is the whole command table. Domain failures go through f.fail and
// are encoded as an error envelope with a nil Go error.
func (f *facade) dispatch(ctx context.Context, req *request) (string, error) {
	switch req.Type {
	case "hello":
		return encodeResponse("hello", api.HelloResult{
			Version:  version.String(),
			Protocol: 1,
			OS:       runtime.GOOS,
			Arch:     runtime.GOARCH,
		})

	case "status":
		st := f.ctrl.Snapshot()
		f.pushStatus()
		return encodeResponse("status", st)

	case "locations":
		var p api.LocationsPayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		lctx, cancel := context.WithTimeout(ctx, locationsTimeout)
		defer cancel()
		res, err := f.ctrl.LocationsView(lctx, p.Refresh)
		if err != nil {
			return f.fail(err)
		}
		return encodeResponse("locations", res)

	case "select_location":
		var p api.SelectLocationPayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		if err := f.ctrl.SelectLocation(ctx, p.CountryCode, p.CityCode, p.Server, p.Fastest); err != nil {
			return f.fail(err)
		}
		f.pushStatus()
		return okResponse()

	case "favorite_location":
		var p api.FavoriteLocationPayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		if _, err := f.ctrl.SetFavorite(ctx, settings.LocationRef{
			CountryCode: p.CountryCode,
			CityCode:    p.CityCode,
			Server:      p.Server,
		}, p.Favorite); err != nil {
			return f.fail(err)
		}
		f.pushStatus()
		return okResponse()

	case "clear_recents":
		if err := f.ctrl.ClearRecents(); err != nil {
			return f.fail(err)
		}
		f.pushStatus()
		return okResponse()

	case "relay_test":
		rctx, cancel := context.WithTimeout(ctx, relayTimeout)
		defer cancel()
		return encodeResponse("relay_test", f.ctrl.TestRelay(rctx))

	case "region_check":
		rctx, cancel := context.WithTimeout(ctx, regionTimeout)
		defer cancel()
		return encodeResponse("region_check", f.ctrl.RegionCheck(rctx))

	case "prepare":
		pctx, cancel := context.WithTimeout(ctx, prepareTimeout)
		defer cancel()
		res, err := f.ctrl.Prepare(pctx)
		if err != nil {
			return f.fail(err)
		}
		f.pushStatus()
		return encodeResponse("prepare", res)

	case "ping":
		var p api.PingPayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		if len(p.Hosts) > maxPingHosts {
			return f.fail(badRequest("too many ping hosts: %d (limit %d)", len(p.Hosts), maxPingHosts))
		}
		if err := f.startPing(p.Hosts); err != nil {
			return f.fail(err)
		}
		return okResponse()

	case "connect":
		var p api.ConnectPayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		cctx, cancel := context.WithTimeout(ctx, connectTimeout)
		defer cancel()
		if !p.ProxyOnly {
			if err := f.configureVpn(); err != nil {
				return f.fail(err)
			}
		}
		if err := f.ctrl.Connect(cctx, p); err != nil {
			return f.fail(err)
		}
		f.pushStatus()
		return okResponse()

	case "disconnect":
		dctx, cancel := context.WithTimeout(ctx, disconnectTimeout)
		defer cancel()
		if err := f.ctrl.Disconnect(); err != nil {
			return f.fail(err)
		}
		_ = dctx
		f.pushStatus()
		return okResponse()

	case "settings_get":
		return encodeResponse("settings", f.ctrl.Settings())

	case "settings_set":
		var p api.SettingsSetPayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		sctx, cancel := context.WithTimeout(ctx, settingsSetTimeout)
		defer cancel()
		_ = sctx
		if err := f.ctrl.ApplySetting(p); err != nil {
			return f.fail(err)
		}
		f.pushStatus()
		return okResponse()

	case "account":
		actx, cancel := context.WithTimeout(ctx, accountTimeout)
		defer cancel()
		return encodeResponse("account_info", f.ctrl.Account(actx))

	case "login_email":
		var p api.LoginEmailPayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		state := f.ctrl.LoginEmail(p.Email)
		f.pushStatus()
		return encodeResponse("login_state", state)

	case "login_password":
		var p api.LoginPasswordPayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		lctx, cancel := context.WithTimeout(ctx, loginPasswordTimeout)
		defer cancel()
		state := f.ctrl.LoginPassword(lctx, p.Password)
		f.pushStatus()
		return encodeResponse("login_state", state)

	case "login_2fa":
		var p api.Login2FAPayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		lctx, cancel := context.WithTimeout(ctx, login2FATimeout)
		defer cancel()
		state := f.ctrl.Login2FA(lctx, p.Code)
		f.pushStatus()
		return encodeResponse("login_state", state)

	case "accounts_list":
		actx, cancel := context.WithTimeout(ctx, accountsTimeout)
		defer cancel()
		_ = actx
		return encodeResponse("accounts_info", f.ctrl.Accounts())

	case "accounts_add_start", "login_start":
		var p api.LoginStartPayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		actx, cancel := context.WithTimeout(ctx, accountCommandTimeout)
		defer cancel()
		_ = actx
		state := f.ctrl.LoginStart(p.Method, p.Email, p.AccountID)
		f.pushStatus()
		return encodeResponse("login_state", state)

	case "login_complete":
		var p api.LoginCompletePayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		if p.RedirectURL == "" && p.Code != "" {
			return f.fail(badRequest("paste the complete redirect URL from the browser"))
		}
		actx, cancel := context.WithTimeout(ctx, loginCompleteTimeout)
		defer cancel()
		_ = actx
		if err := f.ctrl.LoginComplete(p.LoginID, p.RedirectURL); err != nil {
			f.pushStatus()
			return f.fail(err)
		}
		f.pushStatus()
		return okResponse()

	case "accounts_add_status", "login_status":
		var p api.LoginStatusPayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		actx, cancel := context.WithTimeout(ctx, accountCommandTimeout)
		defer cancel()
		_ = actx
		state, err := f.ctrl.LoginStatus(p.LoginID)
		if err != nil {
			return f.fail(err)
		}
		return encodeResponse("login_state", state)

	case "accounts_add_cancel", "login_cancel":
		var p api.LoginCancelPayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		actx, cancel := context.WithTimeout(ctx, accountCommandTimeout)
		defer cancel()
		_ = actx
		if err := f.ctrl.LoginCancel(p.LoginID); err != nil {
			return f.fail(err)
		}
		f.pushStatus()
		return okResponse()

	case "accounts_remove":
		var p api.AccountsRemovePayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		actx, cancel := context.WithTimeout(ctx, accountCommandTimeout)
		defer cancel()
		_ = actx
		if err := f.ctrl.RemoveAccount(p.ID); err != nil {
			return f.fail(err)
		}
		f.pushStatus()
		return okResponse()

	case "accounts_use":
		var p api.AccountsUsePayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		actx, cancel := context.WithTimeout(ctx, accountCommandTimeout)
		defer cancel()
		_ = actx
		if err := f.ctrl.UseAccount(p.ID); err != nil {
			return f.fail(err)
		}
		f.pushStatus()
		return okResponse()

	case "accounts_update":
		var p api.AccountsUpdatePayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		actx, cancel := context.WithTimeout(ctx, accountCommandTimeout)
		defer cancel()
		_ = actx
		if err := f.ctrl.UpdateAccount(p.ID, p.Label); err != nil {
			return f.fail(err)
		}
		f.pushStatus()
		return okResponse()

	case "accounts_set_rotation":
		var p api.AccountsRotationPayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		actx, cancel := context.WithTimeout(ctx, accountCommandTimeout)
		defer cancel()
		_ = actx
		if err := f.ctrl.SetAccountRotation(p.Enabled, p.Threshold); err != nil {
			return f.fail(err)
		}
		f.pushStatus()
		return okResponse()

	case "logout":
		if err := f.ctrl.Logout(); err != nil {
			return f.fail(err)
		}
		f.pushStatus()
		return okResponse()

	case "logs_snapshot":
		var p api.LogsSnapshotPayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		min := logging.Debug
		if p.Level != "" {
			parsed, err := logging.ParseLevel(p.Level)
			if err != nil {
				return f.fail(badRequest("%v", err))
			}
			min = parsed
		}
		entries := filterLogs(f.ring.Snapshot(), min)
		return encodeResponse("log_snapshot", api.LogSnapshot{Entries: entries})

	case "logs_subscribe":
		var p api.LogsSubscribePayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		level := logging.Info
		if p.Level != "" {
			parsed, err := logging.ParseLevel(p.Level)
			if err != nil {
				return f.fail(badRequest("%v", err))
			}
			level = parsed
		}
		f.subscribeLogs(level)
		return okResponse()

	case "logs_unsubscribe":
		f.stopLogSubscription()
		return okResponse()

	case "logs_clear":
		f.ring.Clear()
		if err := f.sink.Reset(); err != nil {
			f.logf(logging.Warn, "mobile", "reset the log file: %v", err)
		}
		return okResponse()

	case "log_append":
		var p logAppendPayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		level := logging.Info
		if p.Level != "" {
			parsed, err := logging.ParseLevel(p.Level)
			if err != nil {
				return f.fail(badRequest("%v", err))
			}
			level = parsed
		}
		if strings.TrimSpace(p.Msg) == "" {
			return f.fail(badRequest("log_append needs a message"))
		}
		tag := strings.TrimSpace(p.Tag)
		if tag == "" {
			tag = "app"
		}
		f.ring.Log(level, tag, p.Msg)
		return okResponse()

	case "reset_state":
		rctx, cancel := context.WithTimeout(ctx, resetStateTimeout)
		defer cancel()
		if err := f.resetState(rctx); err != nil {
			return f.fail(err)
		}
		return okResponse()

	case "doctor":
		var p doctorPayload
		if err := decodePayload(req, &p); err != nil {
			return f.fail(err)
		}
		dctx, cancel := context.WithTimeout(ctx, doctorTimeout)
		defer cancel()
		report, err := f.doctorReport(dctx, p.Facts)
		if err != nil {
			return f.fail(err)
		}
		return encodeResponse("doctor", report)

	case "revoke":
		rctx, cancel := context.WithTimeout(ctx, revokeTimeout)
		defer cancel()
		_ = rctx
		f.ctrl.Shutdown()
		f.pushStatus()
		return okResponse()

	default:
		return f.fail(badRequest("unknown request type %q", req.Type))
	}
}

func okResponse() (string, error) {
	return encodeResponse("result", api.Result{OK: true})
}

// fail encodes a domain failure as an error envelope. It returns a non-nil Go
// error only when the envelope itself cannot be encoded.
func (f *facade) fail(err error) (string, error) {
	var re *api.RemoteError
	switch {
	case errors.As(err, &re):
		return encodeError(re.Code, re.Message)
	case errors.Is(err, context.DeadlineExceeded):
		return encodeError(api.CodeInternal, "the request timed out")
	case errors.Is(err, context.Canceled):
		return encodeError(api.CodeInternal, "the request was cancelled")
	}
	return encodeError(api.CodeInternal, err.Error())
}

// configureVpn builds the network plan and hands it to the Kotlin bridge
// before the engine opens the device. Proxy-only connects skip this, which is
// why a nil bridge still supports proxy-only mode.
func (f *facade) configureVpn() error {
	if f.bridge == nil {
		return badRequest("full-tunnel connect needs the Android VpnService bridge; use proxy_only to connect without it")
	}
	raw, err := buildNetworkConfig(f.store.EffectiveConfig(), f.debugf)
	if err != nil {
		return err
	}
	if err := f.callBridgeConfigure(raw); err != nil {
		return &api.RemoteError{
			Code:    api.CodeInternal,
			Message: fmt.Sprintf("configure the VpnService: %v", err),
		}
	}
	return nil
}

// callBridgeConfigure recovers a Kotlin exception (surfaced by gomobile as a
// Go panic) into a bridge error.
func (f *facade) callBridgeConfigure(raw string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("the bridge panicked: %v", r)
		}
	}()
	return f.bridge.Configure(raw)
}

// startPing starts one run off the command path and streams one OnPing per
// result followed by exactly one OnPingDone. The facade root context is the
// run context, so Close stops outstanding probes.
func (f *facade) startPing(hosts []string) error {
	if f.pingFn == nil {
		return &api.RemoteError{Code: api.CodeInternal, Message: "ping is unavailable"}
	}
	f.pingMu.Lock()
	if f.pinging {
		f.pingMu.Unlock()
		return badRequest("a ping run is already in progress")
	}
	f.pinging = true
	f.pingMu.Unlock()

	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		defer func() {
			f.pingMu.Lock()
			f.pinging = false
			f.pingMu.Unlock()
		}()
		done := f.pingFn(f.ctx, hosts, func(result api.PingResult) {
			raw, err := json.Marshal(result)
			if err != nil {
				f.logf(logging.Warn, "mobile", "encode ping result: %v", err)
				return
			}
			text := string(raw)
			f.emit(func() { f.listener.OnPing(text) })
		})
		raw, err := json.Marshal(done)
		if err != nil {
			f.logf(logging.Warn, "mobile", "encode ping done: %v", err)
			return
		}
		text := string(raw)
		f.emit(func() { f.listener.OnPingDone(text) })
	}()
	return nil
}

// resetState drops the disposable server-list cache, the per-server failure
// counters and the retained logs. It never touches tokens, the selection, the
// account email or the settings overrides.
func (f *facade) resetState(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cache := filepath.Join(filepath.Dir(f.paths.State), "serverlist.json")
	if err := os.Remove(cache); err != nil && !errors.Is(err, os.ErrNotExist) {
		return &api.RemoteError{Code: api.CodeInternal, Message: fmt.Sprintf("remove %s: %v", cache, err)}
	}
	if err := f.store.UpdateState(func(s *settings.State) { s.Failures = nil }); err != nil {
		return &api.RemoteError{Code: api.CodeInternal, Message: err.Error()}
	}
	f.ring.Clear()
	if err := f.sink.Reset(); err != nil {
		f.logf(logging.Warn, "mobile", "reset the log file: %v", err)
	}
	return ctx.Err()
}

// filterLogs returns entries at or above min, matching the daemon's behavior.
func filterLogs(entries []logging.Entry, min logging.Level) []logging.Entry {
	out := make([]logging.Entry, 0, len(entries))
	for _, e := range entries {
		if e.Level >= min {
			out = append(out, e)
		}
	}
	return out
}
