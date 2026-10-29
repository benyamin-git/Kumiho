package gui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/doctor"
	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/logging"
	"github.com/benyamin-git/kumiho/internal/serverlist"
	"github.com/benyamin-git/kumiho/internal/version"
)

// Request timeouts, exactly the inventory's "Active timeouts"
// (docs/gui-feature-inventory.md §behavioral inventory).
const (
	connectTimeout         = 90 * time.Second
	disconnectTimeout      = 90 * time.Second
	statusTimeout          = 15 * time.Second
	locationsTimeout       = 30 * time.Second
	pingTimeout            = 10 * time.Second
	selectLocationTimeout  = 10 * time.Second
	settingsTimeout        = 15 * time.Second
	accountTimeout         = 35 * time.Second
	loginTimeout           = 60 * time.Second
	logoutTimeout          = 15 * time.Second
	logsSnapshotTimeout    = 15 * time.Second
	logsSubscribeTimeout   = 10 * time.Second
	logsUnsubscribeTimeout = 5 * time.Second
	logsClearTimeout       = 5 * time.Second
)

// Controller cadences and defaults.
const (
	// defaultPumpInterval is the inventory's 250 ms UI pump: the cadence at
	// which expired notices retire.
	defaultPumpInterval = 250 * time.Millisecond
	// defaultStatusInterval is the status snapshot's slow safety tick. The
	// daemon pushes status events on every change and while connected
	// (metrics every 2 s); the tick only covers the gaps (initial load,
	// recovery after the daemon restarted, dropped events).
	defaultStatusInterval = 15 * time.Second
	// defaultSettingsInterval keeps the settings snapshot truthful against
	// changes made outside the GUI (CLI/TUI) without user interaction.
	defaultSettingsInterval = 15 * time.Second
	// defaultNoticeTTL is the inventory's 10 s screen-local notice lifetime.
	defaultNoticeTTL = 10 * time.Second
	// logsCapacity bounds the client-side log buffer, mirroring the daemon
	// ring and the TUI.
	logsCapacity = 2000
)

// Notice tones carried in State (the UI maps them to theme colours).
const (
	toneInfo = "info"
	toneGood = "good"
	toneBad  = "bad"
	toneWarn = "warn"
)

// Daemon is the control-plane surface the controller consumes. DaemonClient
// implements it over the IPC control protocol; the mock and tests substitute
// their own implementations.
type Daemon interface {
	Status(context.Context) (api.Status, error)
	Connect(context.Context, api.ConnectPayload) (api.Result, error)
	Disconnect(context.Context) (api.Result, error)
	SelectLocation(context.Context, api.SelectLocationPayload) (api.Result, error)
	Locations(context.Context, bool) (api.LocationsResult, error)
	Ping(context.Context, []string) (api.Result, error)
	Settings(context.Context) (api.SettingsView, error)
	SetSetting(context.Context, api.SettingsSetPayload) (api.Result, error)
	Account(context.Context) (api.AccountInfo, error)
	AccountsList(context.Context) (api.AccountsView, error)
	AccountsAddStart(context.Context, api.LoginStartPayload) (api.LoginState, error)
	AccountsAddStatus(context.Context, api.LoginStatusPayload) (api.LoginState, error)
	AccountsAddCancel(context.Context, api.LoginCancelPayload) (api.Result, error)
	AccountsRemove(context.Context, api.AccountsRemovePayload) (api.Result, error)
	AccountsUse(context.Context, api.AccountsUsePayload) (api.Result, error)
	AccountsUpdate(context.Context, api.AccountsUpdatePayload) (api.Result, error)
	AccountsSetRotation(context.Context, api.AccountsRotationPayload) (api.Result, error)
	LogsSnapshot(context.Context, string) (api.LogSnapshot, error)
	LogsSubscribe(context.Context, string) (api.Result, error)
	LogsUnsubscribe(context.Context) (api.Result, error)
	LogsClear(context.Context) (api.Result, error)
	LoginEmail(context.Context, api.LoginEmailPayload) (api.LoginState, error)
	LoginPassword(context.Context, api.LoginPasswordPayload) (api.LoginState, error)
	Login2FA(context.Context, api.Login2FAPayload) (api.LoginState, error)
	LoginStart(context.Context, api.LoginStartPayload) (api.LoginState, error)
	LoginStatus(context.Context, api.LoginStatusPayload) (api.LoginState, error)
	LoginCancel(context.Context, api.LoginCancelPayload) (api.Result, error)
	LoginComplete(context.Context, api.LoginCompletePayload) (api.Result, error)
	Logout(context.Context) (api.Result, error)
	// Events yields the daemon's unsolicited, pushed events (status
	// snapshots, ping results, live log entries) that drive live refreshes.
	// The channel never closes.
	Events() <-chan *ipc.Envelope
}

// Notice is one user-visible action message carried in State. A nil Expires
// means the notice persists until another action replaces it (the shared
// view-model notices of Dashboard/Locations/Onboarding, and every error);
// otherwise it is a screen-local success notice that retires when Expires
// passes (the inventory's 10 s "Notices / toasts" lifetime).
type Notice struct {
	Message string     `json:"message,omitempty"`
	Tone    string     `json:"tone,omitempty"` // info|good|bad|warn
	Expires *time.Time `json:"expires,omitempty"`
}

// StatusView is the controller's projection of api.Status: the state word
// and the derived labels the Dashboard card renders (inventory §Dashboard).
type StatusView struct {
	Label          string `json:"label"` // Off, Connected, Proxy only, ...
	Connected      bool   `json:"connected,omitempty"`
	ProxyOnly      bool   `json:"proxy_only,omitempty"`
	Authenticated  bool   `json:"authenticated,omitempty"`
	Email          string `json:"email,omitempty"`
	Uptime         string `json:"uptime,omitempty"` // only while connected
	Location       string `json:"location,omitempty"`
	Server         string `json:"server,omitempty"`
	KillSwitch     bool   `json:"kill_switch,omitempty"`
	Autoconnect    bool   `json:"autoconnect,omitempty"`
	ExitIP         string `json:"exit_ip,omitempty"`
	ExitCountry    string `json:"exit_country,omitempty"`
	Rates          string `json:"rates,omitempty"`
	Totals         string `json:"totals,omitempty"`
	TokenExpiry    string `json:"token_expiry,omitempty"`
	PassExpiry     string `json:"pass_expiry,omitempty"`
	QuotaText      string `json:"quota_text,omitempty"`
	QuotaRemaining *int64 `json:"quota_remaining,omitempty"`
	QuotaLimit     *int64 `json:"quota_limit,omitempty"`
	QuotaReset     string `json:"quota_reset,omitempty"`
}

// LoginView is the daemon-driven sign-in projection (inventory §Onboarding).
type LoginView struct {
	Step               string `json:"step,omitempty"` // email|password|2fa|browser|done|error|cancelled
	Email              string `json:"email,omitempty"`
	Method             string `json:"method,omitempty"` // email|browser
	VerificationMethod string `json:"verification_method,omitempty"`
	VerificationReason string `json:"verification_reason,omitempty"`
	URL                string `json:"url,omitempty"`
	ID                 string `json:"id,omitempty"`
	Error              string `json:"error,omitempty"`
}

// ServiceState is the GUI-side projection of the background service; T3.3
// (D17) fills it from the Windows shell's SCM probes (or the harness's dev
// stub) through Controller.SetServiceState. It is not a protocol type.
type ServiceState struct {
	// Installed reports whether the kumiho service exists on this host.
	Installed bool `json:"installed,omitempty"`
	// State is the service state word: running, stopped, start pending, stop
	// pending, paused, not installed or unknown.
	State string `json:"state,omitempty"`
	// Busy is true while a start/stop action is in flight: the service card
	// shows the pending word and keeps its buttons inert.
	Busy bool `json:"busy,omitempty"`
}

// Options configures a Controller. Zero values take the defaults.
type Options struct {
	// Theme and Accent are the initial house appearance carried in State.
	Theme  string
	Accent string
	// Onboarded is the initial onboarding flag from gui.toml (T3.4): while
	// false the UI shows the first-run wizard. The shell and the harness
	// pass prefs.Onboarded at construction and flip it through SetOnboarded
	// when prefs_set {"onboarded":…} persists the flag.
	Onboarded bool
	// Autostart is the persisted Start at login preference from gui.toml
	// (T4.2): the shell seeds it at construction and republishes it through
	// SetAutostart after every prefs_set. Nil leaves State.Autostart absent,
	// so the Settings row keeps its session record (the harness and tests
	// that do not host the Run entry).
	Autostart *bool
	// PumpInterval is the UI pump cadence: how often expired notices retire.
	PumpInterval time.Duration
	// StatusInterval is the status snapshot's slow safety tick.
	StatusInterval time.Duration
	// SettingsInterval is the background settings refresh cadence.
	SettingsInterval time.Duration
	// NoticeTTL is the screen-local success notice lifetime (10 s).
	NoticeTTL time.Duration
	// UpdateCheck runs one manual update check (update_check). Nil uses the
	// GitHub checker (UpdateChecker.Check); tests inject their own.
	UpdateCheck func(ctx context.Context) (UpdateInfo, error)
	// LogsExport writes the given plain-text log content to a new file in
	// the host's per-user log folder and returns the resulting path
	// (logs_export, inventory §Logs). The shell wires its writer; nil — the
	// default — refuses the action with a clear error, so tests and
	// non-host callers behave deterministically and the engine stays
	// OS-free.
	LogsExport func(content string) (string, error)
	// OpenLogsFolder creates the host's per-user log folder if needed and
	// opens it in the OS file manager, returning the folder path
	// (open_logs_folder, inventory §Logs). Nil — the default — refuses the
	// action with a clear error.
	OpenLogsFolder func() (string, error)
	// DoctorRun runs the diagnostics check suite in-process (doctor_run,
	// T3.3; D19: no IPC addition). Nil uses internal/doctor's Run. Tests
	// inject canned reports; the seam keeps the suite's network probes out
	// of the test lane.
	DoctorRun func() doctor.Report
}

func (o Options) withDefaults() Options {
	if o.PumpInterval <= 0 {
		o.PumpInterval = defaultPumpInterval
	}
	if o.StatusInterval <= 0 {
		o.StatusInterval = defaultStatusInterval
	}
	if o.SettingsInterval <= 0 {
		o.SettingsInterval = defaultSettingsInterval
	}
	if o.NoticeTTL <= 0 {
		o.NoticeTTL = defaultNoticeTTL
	}
	if o.DoctorRun == nil {
		o.DoctorRun = doctor.Run
	}
	return o
}

// Controller is the GUI's orchestration core: it consumes UI actions, talks
// to the daemon through a Daemon and produces the State snapshots the shell
// pushes to the webview. All methods are safe for concurrent use.
type Controller struct {
	d    Daemon
	opts Options

	mu sync.Mutex

	// Reachability of the control pipe; false flips the shell to the
	// Set up Kumiho gate (inventory §behavioral).
	reachable bool

	// Status projection state.
	status        api.Status
	statusLoaded  bool
	statusError   string
	lastStatusTry time.Time

	login *LoginView
	// accountFlow marks a password/browser sign-in started through
	// accounts_add_start. Its password steps reuse login_password/login_2fa
	// (the protocol has no account-specific variants), so the terminal
	// success still belongs on the Account screen's notice.
	accountFlow bool

	// Account snapshot and the authentication it was loaded for.
	account        *api.AccountInfo
	accountLoaded  bool
	accountAuth    bool
	accountLoading bool

	// Account-list snapshot (v0.6.0). accountsFetch serialises the snapshot
	// loads; the main mutex protects the fields.
	accounts       *api.AccountsView
	accountsLoaded bool
	accountsFetch  sync.Mutex

	// Server list snapshot.
	locations        []serverlist.Country
	locationsLoaded  bool
	locationsLoading bool

	// Settings snapshot.
	settings        *api.SettingsView
	lastSettingsTry time.Time

	rtts           map[string]string
	logs           []logging.Entry
	logsSubscribed bool
	logsLoaded     bool

	notice          *Notice
	locationsNotice *Notice
	settingsNotice  *Notice
	logsNotice      *Notice
	accountNotice   *Notice

	// doctor is the last doctor run's projection (T3.3); non-nil once a run
	// started, so the screen can tell `Checking…` from `No checks yet.`
	doctor *DoctorView

	// diagnosticsNotice is the Diagnostics screen's notice (cleanup and
	// service outcomes).
	diagnosticsNotice *Notice

	theme  string
	accent string

	// onboarded carries the first-run wizard flag (T3.4); see
	// Options.Onboarded and SetOnboarded.
	onboarded bool

	// autostart carries the persisted Start at login preference (T4.2); nil
	// means the host does not project it. See Options.Autostart and
	// SetAutostart.
	autostart *bool

	service ServiceState
	update  UpdateInfo
}

// NewController returns a controller over the given daemon. The returned
// controller is inert until Poll is started; Snapshot and Dispatch work
// immediately.
func NewController(d Daemon, opts Options) *Controller {
	opts = opts.withDefaults()
	if opts.UpdateCheck == nil {
		checker := UpdateChecker{}
		opts.UpdateCheck = checker.Check
	}
	var autostart *bool
	if opts.Autostart != nil {
		v := *opts.Autostart
		autostart = &v
	}
	return &Controller{
		d:         d,
		opts:      opts,
		reachable: true, // ready until a dial failure proves otherwise
		theme:     opts.Theme,
		accent:    opts.Accent,
		onboarded: opts.Onboarded,
		autostart: autostart,
		// The Version row shows the running build from the first snapshot,
		// before any check has run (T2.4 ruling).
		update: UpdateInfo{Current: version.Version},
	}
}

// Snapshot returns the current state. Expired notices are dropped and uptime
// is recomputed from the last status snapshot on every call, so a shell that
// repaints on its own cadence sees the connected duration tick (inventory
// §Shell "UI refresh").
func (c *Controller) Snapshot() State {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	retireExpired(&c.notice, now)
	retireExpired(&c.locationsNotice, now)
	retireExpired(&c.settingsNotice, now)
	retireExpired(&c.logsNotice, now)
	retireExpired(&c.accountNotice, now)
	retireExpired(&c.diagnosticsNotice, now)

	var autostart *bool
	if c.autostart != nil {
		v := *c.autostart
		autostart = &v
	}

	return State{
		Reachable:         c.reachable,
		StatusError:       c.statusError,
		Status:            c.statusViewLocked(now),
		Login:             copyLogin(c.login),
		Doctor:            copyDoctor(c.doctor),
		Settings:          copySettings(c.settings),
		Account:           copyAccount(c.account),
		Accounts:          copyAccounts(c.accounts),
		LocationsLoaded:   c.locationsLoaded,
		Locations:         c.locations,
		RTTs:              copyRTTs(c.rtts),
		LogsLoaded:        c.logsLoaded,
		Onboarded:         c.onboarded,
		Autostart:         autostart,
		Logs:              c.logs,
		Notice:            liveNotice(c.notice, now),
		LocationsNotice:   liveNotice(c.locationsNotice, now),
		SettingsNotice:    liveNotice(c.settingsNotice, now),
		LogsNotice:        liveNotice(c.logsNotice, now),
		AccountNotice:     liveNotice(c.accountNotice, now),
		DiagnosticsNotice: liveNotice(c.diagnosticsNotice, now),
		Theme:             c.theme,
		Accent:            c.accent,
		Service:           c.service,
		Update:            c.update,
	}
}

// Dispatch routes one UI action and runs it synchronously; connect and
// disconnect may take up to 90 s, so callers that must stay responsive run
// Dispatch on their own goroutine. A daemon failure is returned unchanged
// (errors.Is still sees ErrDaemonUnreachable) and is also recorded as the
// action's notice per the inventory.
//
// Handled here: connect, disconnect, select_location, ping, settings_get,
// settings_set, login_email, login_password, login_2fa, login_start,
// login_status, login_cancel, login_complete, accounts_list,
// accounts_add_start, accounts_add_status, accounts_add_cancel,
// accounts_remove, accounts_use, accounts_update, accounts_set_rotation,
// logout, logs_snapshot, logs_subscribe, logs_unsubscribe, logs_clear,
// logs_export, open_logs_folder, doctor_run, refresh, update_check.
// window_show, quit and open_external are recognized no-ops: the shell owns
// window visibility, process exit and opening links (D34). cleanup,
// service_start, service_stop and prefs_set are host-side actions the shell
// (or the harness) intercepts before Dispatch (T3.3/T1.7); reaching them here
// is an "unsupported action" error instead of silence.
func (c *Controller) Dispatch(act Action) error {
	switch act.Name {
	case ActionConnect:
		return c.dispatchConnect(act)
	case ActionDisconnect:
		return c.dispatchDisconnect()
	case ActionSelectLocation:
		return c.dispatchSelectLocation(act)
	case ActionPing:
		return c.dispatchPing(act)
	case ActionSettingsGet:
		return c.dispatchSettingsGet()
	case ActionSettingsSet:
		return c.dispatchSettingsSet(act)
	case ActionUpdateCheck:
		return c.dispatchUpdateCheck()
	case ActionLoginEmail, ActionLoginPassword, ActionLogin2FA,
		ActionLoginStart, ActionLoginStatus, ActionLoginCancel,
		ActionLoginComplete,
		ActionAccountsAddStart, ActionAccountsAddStatus, ActionAccountsAddCancel:
		return c.dispatchLogin(act)
	case ActionAccountsList:
		return c.dispatchAccountsList()
	case ActionAccountsUse:
		return c.dispatchAccountsUse(act)
	case ActionAccountsRemove:
		return c.dispatchAccountsRemove(act)
	case ActionAccountsUpdate:
		return c.dispatchAccountsUpdate(act)
	case ActionAccountsSetRotation:
		return c.dispatchAccountsSetRotation(act)
	case ActionLogout:
		return c.dispatchLogout()
	case ActionLogsSnapshot:
		return c.dispatchLogsSnapshot(act)
	case ActionLogsSubscribe:
		return c.dispatchLogsSubscribe(act)
	case ActionLogsUnsubscribe:
		return c.dispatchLogsUnsubscribe()
	case ActionLogsClear:
		return c.dispatchLogsClear()
	case ActionLogsExport:
		return c.dispatchLogsExport(act)
	case ActionOpenLogsFolder:
		return c.dispatchOpenLogsFolder(act)
	case ActionDoctorRun:
		return c.dispatchDoctorRun()
	case ActionRefresh:
		return c.dispatchRefresh(act)
	case ActionPrefsSet:
		// Deferred: prefs_set is host-side (T1.7/T1.10/T4.2).
		return fmt.Errorf("unsupported action %q", act.Name)
	case ActionCleanup, ActionServiceStart, ActionServiceStop:
		// Host-side actions (T3.3): the Windows shell intercepts them for
		// the UAC-elevated cleanup and sc.exe start/stop, and the harness
		// intercepts them for its dev stubs. A caller that reaches Dispatch
		// with one (a miswired composition root) gets a refusal instead of
		// silence.
		return fmt.Errorf("unsupported action %q", act.Name)
	case ActionWindowShow, ActionQuit, ActionOpenExternal:
		return nil
	default:
		return fmt.Errorf("unsupported action %q", act.Name)
	}
}

// connectRequest is the connect action payload: the api.ConnectPayload
// fields plus an optional pre-connect kill-switch change (the CLI's
// settings-then-connect sequence for --kill-switch).
type connectRequest struct {
	api.ConnectPayload
	KillSwitch string `json:"kill_switch,omitempty"` // on|off|last
}

func (c *Controller) dispatchConnect(act Action) error {
	req, err := decodePayload[connectRequest](act)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	if req.KillSwitch != "" {
		if _, err := c.d.SetSetting(ctx, api.SettingsSetPayload{Key: "kill_switch", Value: req.KillSwitch}); err != nil {
			c.observe(err)
			c.setNotice(slotShared, "could not connect: "+err.Error(), toneBad, 0)
			return err
		}
		c.observe(nil)
	}
	if _, err := c.d.Connect(ctx, req.ConnectPayload); err != nil {
		c.observe(err)
		c.setNotice(slotShared, "could not connect: "+err.Error(), toneBad, 0)
		return err
	}
	c.observe(nil)
	c.setNotice(slotShared, "Connected", toneInfo, 0)
	c.refreshStatus(ctx)
	return nil
}

func (c *Controller) dispatchDisconnect() error {
	ctx, cancel := context.WithTimeout(context.Background(), disconnectTimeout)
	defer cancel()
	if _, err := c.d.Disconnect(ctx); err != nil {
		c.observe(err)
		c.setNotice(slotShared, "could not disconnect: "+err.Error(), toneBad, 0)
		return err
	}
	c.observe(nil)
	c.setNotice(slotShared, "Disconnected", toneInfo, 0)
	c.refreshStatus(ctx)
	return nil
}

func (c *Controller) dispatchSelectLocation(act Action) error {
	p, err := decodePayload[api.SelectLocationPayload](act)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), selectLocationTimeout)
	defer cancel()
	_, err = c.d.SelectLocation(ctx, p)
	if err != nil {
		c.observe(err)
		c.setNotice(slotShared, "could not save the location: "+err.Error(), toneBad, 0)
	} else {
		c.observe(nil)
		c.setNotice(slotShared, "Location saved", toneInfo, 0)
	}
	// The selection is broadcast as a status event either way; refresh keeps
	// the snapshot truthful without waiting for the event (TUI parity).
	c.refreshStatus(ctx)
	return err
}

func (c *Controller) dispatchPing(act Action) error {
	p, err := decodePayload[api.PingPayload](act)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()
	if _, err := c.d.Ping(ctx, p.Hosts); err != nil {
		c.observe(err)
		c.setNotice(slotLocations, "Speed test failed: "+err.Error(), toneBad, 0)
		return err
	}
	c.observe(nil)
	c.clearNoticeError(slotLocations)
	return nil
}

func (c *Controller) dispatchSettingsGet() error {
	ctx, cancel := context.WithTimeout(context.Background(), settingsTimeout)
	defer cancel()
	return c.loadSettings(ctx)
}

func (c *Controller) dispatchSettingsSet(act Action) error {
	p, err := decodePayload[api.SettingsSetPayload](act)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), settingsTimeout)
	defer cancel()
	if _, err := c.d.SetSetting(ctx, p); err != nil {
		c.observe(err)
		c.setNotice(slotSettings, "could not change the setting: "+err.Error(), toneBad, 0)
		return err
	}
	c.observe(nil)
	label := settingLabel(p.Key)
	if p.Reset {
		c.setNotice(slotSettings, label+" reset to the configured value", toneGood, c.opts.NoticeTTL)
	} else {
		c.setNotice(slotSettings, label+" saved", toneGood, c.opts.NoticeTTL)
	}
	// The inventory's mutation sequence: refresh the status and reload the
	// settings so values and override marks stay truthful.
	c.refreshStatus(ctx)
	c.loadSettings(ctx)
	return nil
}

// dispatchUpdateCheck runs the manual update check (D9: on demand only). The
// result lands in State.Update; the outcome is also the settings notice —
// "Kumiho vX is available", "Kumiho is up to date" for 10 s, or a persistent
// "could not check for updates" on failure. The check talks to GitHub, never
// the daemon, so it deliberately leaves the reachability flag alone: a feed
// failure must not flip the shell to the Set up Kumiho gate.
func (c *Controller) dispatchUpdateCheck() error {
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()
	info, err := c.opts.UpdateCheck(ctx)
	if err != nil {
		c.setNotice(slotSettings, "could not check for updates: "+err.Error(), toneBad, 0)
		return err
	}
	if info.Current == "" {
		info.Current = version.Version
	}
	c.mu.Lock()
	c.update = info
	c.mu.Unlock()
	if sameRelease(info.Current, info.Latest) {
		c.setNotice(slotSettings, "Kumiho is up to date", toneGood, c.opts.NoticeTTL)
	} else {
		c.setNotice(slotSettings, "Kumiho "+info.Latest+" is available", toneInfo, c.opts.NoticeTTL)
	}
	return nil
}

func (c *Controller) dispatchLogin(act Action) error {
	ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
	defer cancel()
	var (
		st  api.LoginState
		err error
	)
	accountAction := strings.HasPrefix(act.Name, "accounts_add_")
	statusAction := act.Name == ActionLoginStatus || act.Name == ActionAccountsAddStatus
	c.mu.Lock()
	accountFlow := c.accountFlow
	c.mu.Unlock()
	// login_complete is shared by both screens (the frozen vocabulary has no
	// account-specific spelling): the add started through accounts_add_start
	// owns it until its terminal reply, exactly like the password steps that
	// reuse login_password.
	if act.Name == ActionLoginComplete && accountFlow {
		accountAction = true
	}
	switch act.Name {
	case ActionLoginEmail:
		var p api.LoginEmailPayload
		if p, err = decodePayload[api.LoginEmailPayload](act); err != nil {
			return err
		}
		// The onboarding password flow is separate from an Account-screen
		// add; an interrupted add must not claim its terminal reply.
		c.mu.Lock()
		c.accountFlow = false
		c.mu.Unlock()
		st, err = c.d.LoginEmail(ctx, p)
	case ActionLoginPassword:
		var p api.LoginPasswordPayload
		if p, err = decodePayload[api.LoginPasswordPayload](act); err != nil {
			return err
		}
		st, err = c.d.LoginPassword(ctx, p)
	case ActionLoginStart, ActionAccountsAddStart:
		var p api.LoginStartPayload
		if p, err = decodePayload[api.LoginStartPayload](act); err != nil {
			return err
		}
		if accountAction {
			c.mu.Lock()
			c.accountFlow = true
			c.mu.Unlock()
			st, err = c.d.AccountsAddStart(ctx, p)
		} else {
			c.mu.Lock()
			c.accountFlow = false
			c.mu.Unlock()
			st, err = c.d.LoginStart(ctx, p)
		}
	case ActionLoginStatus, ActionAccountsAddStatus:
		var p api.LoginStatusPayload
		if p, err = decodePayload[api.LoginStatusPayload](act); err != nil {
			return err
		}
		if accountAction {
			st, err = c.d.AccountsAddStatus(ctx, p)
		} else {
			st, err = c.d.LoginStatus(ctx, p)
		}
	case ActionLoginComplete:
		var p api.LoginCompletePayload
		if p, err = decodePayload[api.LoginCompletePayload](act); err != nil {
			return err
		}
		if _, err = c.d.LoginComplete(ctx, p); err != nil {
			// Retryable: the daemon keeps the flow active, so the browser
			// step and its URL stay on screen and the error is shown inline;
			// the account-flow ownership survives for the next paste.
			c.observe(err)
			c.mu.Lock()
			c.ensureLoginLocked().Error = err.Error()
			c.mu.Unlock()
			c.loginActionError(act.Name, accountFlow, err)
			return err
		}
		c.observe(nil)
		// A nil completion means the flow reached a terminal state; the
		// terminal reply itself rides login_status/accounts_add_status. Read
		// it once so the waiting card settles without waiting for a poll. A
		// failed read must not be reported as a sign-in failure nor erase the
		// flow: the screen's poll retries.
		if accountAction {
			st, err = c.d.AccountsAddStatus(ctx, api.LoginStatusPayload{LoginID: p.LoginID})
		} else {
			st, err = c.d.LoginStatus(ctx, api.LoginStatusPayload{LoginID: p.LoginID})
		}
		if err != nil {
			c.observe(err)
			return nil
		}
	case ActionLoginCancel, ActionAccountsAddCancel:
		var p api.LoginCancelPayload
		if p, err = decodePayload[api.LoginCancelPayload](act); err != nil {
			return err
		}
		if accountAction {
			_, err = c.d.AccountsAddCancel(ctx, p)
		} else {
			_, err = c.d.LoginCancel(ctx, p)
		}
		if err != nil {
			c.observe(err)
			c.loginActionError(act.Name, accountFlow, err)
			return err
		}
		c.observe(nil)
		c.mu.Lock()
		login := c.ensureLoginLocked()
		login.Step = "cancelled"
		login.URL = ""
		login.ID = ""
		login.Error = ""
		c.accountFlow = false
		c.mu.Unlock()
		slot := slotShared
		if accountAction {
			slot = slotAccount
		}
		c.setNotice(slot, "Sign-in cancelled", toneInfo, c.opts.NoticeTTL)
		return nil
	default:
		var p api.Login2FAPayload
		if p, err = decodePayload[api.Login2FAPayload](act); err != nil {
			return err
		}
		st, err = c.d.Login2FA(ctx, p)
	}
	if err != nil {
		c.observe(err)
		c.mu.Lock()
		if accountAction {
			c.accountFlow = false
		}
		login := c.ensureLoginLocked()
		login.Error = err.Error()
		if statusAction {
			// A failed poll ends the browser wait: the UI keys its polling
			// on the browser step, and a transient error must not spin
			// forever with a stale URL on screen.
			login.Step = "error"
		}
		c.mu.Unlock()
		c.loginActionError(act.Name, accountFlow, err)
		return err
	}
	c.observe(nil)
	terminal := st.Step == "done" || st.Step == "error" || st.Step == "cancelled"
	owned := accountAction || accountFlow
	if c.mergeLogin(st) {
		if owned && st.Step == "done" && st.Error != "" {
			// The engine publishes done+error for a terminal failure (account
			// limit, failed save). Never turn it into a success notice; keep
			// the error on the owning screen until another action replaces it.
			slot := slotShared
			if accountFlow {
				slot = slotAccount
			}
			c.setNotice(slot, st.Error, toneBad, 0)
		} else if owned && st.Step == "done" {
			message := "Account added"
			if st.Email != "" {
				message = "Signed in as " + st.Email
			}
			c.setNotice(slotAccount, message, toneGood, c.opts.NoticeTTL)
		}
		// A terminal reply refreshes the status and both account snapshots:
		// a fresh authenticated snapshot clears the stale "already signed
		// in" error so the wizard can show the live session instead of
		// dead-ending (inventory §behavioral), and the account list learns
		// the new account, active selection and quota.
		c.refreshStatus(ctx)
		c.ensureAccount(ctx)
		_ = c.loadAccounts(ctx)
	}
	if terminal && owned {
		c.mu.Lock()
		c.accountFlow = false
		c.mu.Unlock()
	}
	return nil
}

// loginActionError writes the sign-in action's notice. Account-add actions
// and the completion of an account-owned browser flow report on the Account
// screen; the onboarding verbs keep the shared notice (the wizard renders it
// on its status line).
func (c *Controller) loginActionError(name string, accountFlow bool, err error) {
	prefix := "sign-in failed: "
	slot := slotShared
	switch name {
	case ActionAccountsAddStart:
		prefix, slot = "could not start sign-in: ", slotAccount
	case ActionAccountsAddStatus:
		prefix, slot = "could not check sign-in: ", slotAccount
	case ActionAccountsAddCancel:
		prefix, slot = "could not cancel sign-in: ", slotAccount
	case ActionLoginComplete:
		prefix = "could not finish sign-in: "
		if accountFlow {
			slot = slotAccount
		}
	case ActionLoginStart:
		prefix = "could not start sign-in: "
	case ActionLoginStatus:
		prefix = "could not check sign-in: "
	case ActionLoginCancel:
		prefix = "could not cancel sign-in: "
	}
	c.setNotice(slot, prefix+err.Error(), toneBad, 0)
}

// dispatchAccountsList refreshes the account list and active selection.
func (c *Controller) dispatchAccountsList() error {
	return c.loadAccounts(context.Background())
}

// loadAccounts fetches the accounts_list snapshot. The fetch mutex serialises
// concurrent loads (the pump and an explicit refresh) so an older reply can
// never overwrite a newer one.
func (c *Controller) loadAccounts(ctx context.Context) error {
	c.accountsFetch.Lock()
	defer c.accountsFetch.Unlock()
	actx, cancel := context.WithTimeout(ctx, accountTimeout)
	defer cancel()
	view, err := c.d.AccountsList(actx)
	if err != nil {
		c.observe(err)
		c.setNotice(slotAccount, "could not load accounts: "+err.Error(), toneBad, 0)
		return err
	}
	c.observe(nil)
	c.mu.Lock()
	c.accounts = copyAccounts(&view)
	c.accountsLoaded = true
	c.accountNotice = clearAccountListNotice(c.accountNotice)
	c.mu.Unlock()
	return nil
}

// ensureAccounts loads the account list once in the background. The Account
// screen's explicit accounts_list action always reloads it; this only covers
// the first paint and the onboarding wizard's last-method hint.
func (c *Controller) ensureAccounts(ctx context.Context) {
	c.mu.Lock()
	loaded := c.accountsLoaded
	c.mu.Unlock()
	if loaded {
		return
	}
	go func() { _ = c.loadAccounts(ctx) }()
}

// accountChangeBlocked reports whether the tunnel is up. Switching or
// removing an account would tear the live session out from under the tunnel;
// the daemon enforces the same rule, but the GUI refuses first so the user
// sees the reason without a round trip.
func (c *Controller) accountChangeBlocked() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.statusLoaded && (c.status.State == "CONNECTED" || c.status.State == "PROXY_ONLY" || c.status.ProxyOnly)
}

func (c *Controller) dispatchAccountsUse(act Action) error {
	p, err := decodePayload[api.AccountsUsePayload](act)
	if err != nil {
		return err
	}
	if c.accountChangeBlocked() {
		err := errors.New("disconnect before switching accounts")
		c.setNotice(slotAccount, err.Error(), toneBad, 0)
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), accountTimeout)
	defer cancel()
	if _, err := c.d.AccountsUse(ctx, p); err != nil {
		c.observe(err)
		c.setNotice(slotAccount, "could not switch account: "+err.Error(), toneBad, 0)
		return err
	}
	c.observe(nil)
	c.setNotice(slotAccount, "Account switched", toneGood, c.opts.NoticeTTL)
	c.afterAccountChange(ctx)
	return c.loadAccounts(ctx)
}

func (c *Controller) dispatchAccountsRemove(act Action) error {
	p, err := decodePayload[api.AccountsRemovePayload](act)
	if err != nil {
		return err
	}
	if c.accountChangeBlocked() {
		err := errors.New("disconnect before removing an account")
		c.setNotice(slotAccount, err.Error(), toneBad, 0)
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), accountTimeout)
	defer cancel()
	if _, err := c.d.AccountsRemove(ctx, p); err != nil {
		c.observe(err)
		c.setNotice(slotAccount, "could not remove the account: "+err.Error(), toneBad, 0)
		return err
	}
	c.observe(nil)
	c.setNotice(slotAccount, "Account removed", toneGood, c.opts.NoticeTTL)
	c.afterAccountChange(ctx)
	return c.loadAccounts(ctx)
}

func (c *Controller) dispatchAccountsUpdate(act Action) error {
	p, err := decodePayload[api.AccountsUpdatePayload](act)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), accountTimeout)
	defer cancel()
	if _, err := c.d.AccountsUpdate(ctx, p); err != nil {
		c.observe(err)
		c.setNotice(slotAccount, "could not rename the account: "+err.Error(), toneBad, 0)
		return err
	}
	c.observe(nil)
	c.setNotice(slotAccount, "Account renamed", toneGood, c.opts.NoticeTTL)
	c.afterAccountChange(ctx)
	return c.loadAccounts(ctx)
}

func (c *Controller) dispatchAccountsSetRotation(act Action) error {
	p, err := decodePayload[api.AccountsRotationPayload](act)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), accountTimeout)
	defer cancel()
	if _, err := c.d.AccountsSetRotation(ctx, p); err != nil {
		c.observe(err)
		c.setNotice(slotAccount, "could not save rotation settings: "+err.Error(), toneBad, 0)
		return err
	}
	c.observe(nil)
	state := "off"
	if p.Enabled {
		state = fmt.Sprintf("on at %d%%", p.Threshold)
	}
	c.setNotice(slotAccount, "Account rotation "+state, toneGood, c.opts.NoticeTTL)
	return c.loadAccounts(ctx)
}

// afterAccountChange refreshes the session and entitlement projections that
// follow the active account, without turning their failures into the account
// action's error: the mutation already succeeded, and the account screen
// shows the reload failure on its own status line.
func (c *Controller) afterAccountChange(ctx context.Context) {
	_ = c.refreshStatus(ctx)
	c.ensureAccount(ctx)
}

func (c *Controller) dispatchLogout() error {
	ctx, cancel := context.WithTimeout(context.Background(), logoutTimeout)
	defer cancel()
	if _, err := c.d.Logout(ctx); err != nil {
		c.observe(err)
		c.setNotice(slotAccount, "could not sign out: "+err.Error(), toneBad, 0)
		return err
	}
	c.observe(nil)
	c.mu.Lock()
	c.accountFlow = false
	// Sign-out clears any browser/password projection still on screen; a
	// stale waiting card must not survive the session it belonged to.
	c.login = nil
	c.mu.Unlock()
	c.setNotice(slotAccount, "Signed out", toneGood, c.opts.NoticeTTL)
	c.refreshStatus(ctx)
	c.ensureAccount(ctx)
	_ = c.loadAccounts(ctx)
	return nil
}

func (c *Controller) dispatchLogsSnapshot(act Action) error {
	p, err := decodePayload[api.LogsSnapshotPayload](act)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), logsSnapshotTimeout)
	defer cancel()
	snap, err := c.d.LogsSnapshot(ctx, p.Level)
	if err != nil {
		c.observe(err)
		c.setNotice(slotLogs, "could not load the logs: "+err.Error(), toneBad, 0)
		return err
	}
	c.observe(nil)
	entries := snap.Entries
	if len(entries) > logsCapacity {
		entries = entries[len(entries)-logsCapacity:]
	}
	c.mu.Lock()
	c.logs = entries
	c.logsLoaded = true
	c.logsNotice = clearNoticeError(c.logsNotice)
	c.mu.Unlock()
	return nil
}

func (c *Controller) dispatchLogsSubscribe(act Action) error {
	p, err := decodePayload[api.LogsSubscribePayload](act)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), logsSubscribeTimeout)
	defer cancel()
	if _, err := c.d.LogsSubscribe(ctx, p.Level); err != nil {
		c.observe(err)
		c.setNotice(slotLogs, "could not subscribe to the logs: "+err.Error(), toneBad, 0)
		return err
	}
	c.observe(nil)
	c.mu.Lock()
	c.logsSubscribed = true
	c.logsNotice = clearNoticeError(c.logsNotice)
	c.mu.Unlock()
	return nil
}

func (c *Controller) dispatchLogsUnsubscribe() error {
	ctx, cancel := context.WithTimeout(context.Background(), logsUnsubscribeTimeout)
	defer cancel()
	if _, err := c.d.LogsUnsubscribe(ctx); err != nil {
		c.observe(err)
		c.setNotice(slotLogs, "could not unsubscribe from the logs: "+err.Error(), toneBad, 0)
		return err
	}
	c.observe(nil)
	c.mu.Lock()
	c.logsSubscribed = false
	c.logsNotice = clearNoticeError(c.logsNotice)
	c.mu.Unlock()
	return nil
}

func (c *Controller) dispatchLogsClear() error {
	ctx, cancel := context.WithTimeout(context.Background(), logsClearTimeout)
	defer cancel()
	if _, err := c.d.LogsClear(ctx); err != nil {
		c.observe(err)
		c.setNotice(slotLogs, "could not clear the logs: "+err.Error(), toneBad, 0)
		return err
	}
	c.observe(nil)
	c.mu.Lock()
	c.logs = nil
	c.mu.Unlock()
	c.setNotice(slotLogs, "Logs cleared", toneGood, c.opts.NoticeTTL)
	return nil
}

// logsExportPayload is the logs_export action's payload: the screen's local
// level and search filters, so the host exports exactly the currently
// visible entries (inventory §Logs "Export"). An empty level means every
// entry; the payload is page input, and the exported content is derived
// from the controller's retained buffer only.
type logsExportPayload struct {
	Level  string `json:"level,omitempty"`
	Search string `json:"search,omitempty"`
}

// dispatchLogsExport renders the visible (level+search filtered) buffer
// entries in the daemon's text log format and hands them to the host's
// export writer (Options.LogsExport; the shell writes the parity path
// `%LOCALAPPDATA%\kumiho\logs\kumiho-logs-<YYYYMMDD-HHMMSS>.log`). The
// outcome is the logs notice: `Exported to <path>` (10 s), `Nothing to
// export`, or `could not export the logs: <err>`. It never talks to the
// daemon, so it leaves reachability alone.
func (c *Controller) dispatchLogsExport(act Action) error {
	p, err := decodePayload[logsExportPayload](act)
	if err != nil {
		return err
	}
	minLevel := logging.Debug
	if p.Level != "" {
		if minLevel, err = logging.ParseLevel(p.Level); err != nil {
			return err
		}
	}
	c.mu.Lock()
	entries := append([]logging.Entry(nil), c.logs...)
	c.mu.Unlock()
	visible := visibleLogs(entries, minLevel, p.Search)
	if len(visible) == 0 {
		c.setNotice(slotLogs, "Nothing to export", toneInfo, c.opts.NoticeTTL)
		return nil
	}
	if c.opts.LogsExport == nil {
		err := errors.New("log export is not available")
		c.setNotice(slotLogs, "could not export the logs: "+err.Error(), toneBad, 0)
		return err
	}
	path, err := c.opts.LogsExport(renderLogText(visible))
	if err != nil {
		c.setNotice(slotLogs, "could not export the logs: "+err.Error(), toneBad, 0)
		return err
	}
	c.setNotice(slotLogs, "Exported to "+path, toneGood, c.opts.NoticeTTL)
	return nil
}

// openLogsFolderRequest is the open_logs_folder action's optional payload:
// the notice surface the screen wants the outcome on. The frozen action
// carries no screen parameter, so `notice` names the slot: "" (or "logs")
// is the Logs screen's own notice; "diagnostics" routes the same wording to
// the Diagnostics screen, which reuses the action but must show the notice
// on its own status line (inventory §Diagnostics: "Open log folder behaves
// and notices exactly as on Logs").
type openLogsFolderRequest struct {
	Notice string `json:"notice,omitempty"`
}

// dispatchOpenLogsFolder opens the host's per-user log folder through
// Options.OpenLogsFolder. The outcome is the calling screen's notice:
// `Opened <dir>` (10 s) or `could not open the log folder: <err>`. Daemon-free
// like the export.
func (c *Controller) dispatchOpenLogsFolder(act Action) error {
	p, err := decodePayload[openLogsFolderRequest](act)
	if err != nil {
		return err
	}
	slot := slotLogs
	switch p.Notice {
	case "", "logs":
	case "diagnostics":
		slot = slotDiagnostics
	default:
		return fmt.Errorf("open_logs_folder: unknown notice surface %q", p.Notice)
	}
	if c.opts.OpenLogsFolder == nil {
		err := errors.New("opening the log folder is not available")
		c.setNotice(slot, "could not open the log folder: "+err.Error(), toneBad, 0)
		return err
	}
	dir, err := c.opts.OpenLogsFolder()
	if err != nil {
		c.setNotice(slot, "could not open the log folder: "+err.Error(), toneBad, 0)
		return err
	}
	c.setNotice(slot, "Opened "+dir, toneGood, c.opts.NoticeTTL)
	return nil
}

// dispatchDoctorRun runs the doctor suite in-process (T3.3; D19: doctor
// stays off the IPC protocol) and replaces State.Doctor with the projection.
// A run already in flight is never interrupted: a second request is a no-op
// (inventory §Diagnostics: "a second click/no-op never interrupts"). Starting
// a run clears the previous report and the diagnostics notice, so the screen
// shows its `Checking…` placeholder and a stale cleanup/service error cannot
// mask the fresh summary.
func (c *Controller) dispatchDoctorRun() error {
	c.mu.Lock()
	if c.doctor != nil && c.doctor.Running {
		c.mu.Unlock()
		return nil
	}
	c.doctor = &DoctorView{Running: true}
	c.diagnosticsNotice = nil
	c.mu.Unlock()

	report := c.opts.DoctorRun()

	c.mu.Lock()
	view := projectDoctor(report)
	c.doctor = &view
	c.mu.Unlock()
	return nil
}

// SetServiceState applies one host-probed service snapshot (T3.3, D17). The
// Windows shell publishes SCM probes and elevated start/stop completions
// through it, and the harness publishes its dev service model; the
// controller itself never touches the SCM, so the engine stays OS-free. It
// is safe for concurrent use.
func (c *Controller) SetServiceState(s ServiceState) {
	c.mu.Lock()
	c.service = s
	c.mu.Unlock()
}

// SetOnboarded applies the first-run wizard flag the host persisted through
// prefs_set {"onboarded":…} (T3.4): the shell and the dev harness call it
// after prefs.Save succeeds, and while it is false the UI shows the wizard.
// It is safe for concurrent use.
func (c *Controller) SetOnboarded(v bool) {
	c.mu.Lock()
	c.onboarded = v
	c.mu.Unlock()
}

// SetAutostart publishes the persisted Start at login preference (T4.2). The
// shell calls it after prefs_set has saved gui.toml, whether or not the Run
// key write succeeded: the stored flag is authoritative (inventory §Tray/app
// lifecycle), and the Settings row reads State.Autostart instead of its
// page-session record. It is safe for concurrent use.
func (c *Controller) SetAutostart(v bool) {
	c.mu.Lock()
	c.autostart = &v
	c.mu.Unlock()
}

// SetSettingsNotice publishes one Settings-screen result notice on the host's
// behalf (T4.2's Start at login outcome). It follows the screen-local notice
// rules: a bad-tone failure persists until the next action replaces it,
// everything else carries the 10 s success lifetime (inventory §Settings).
// The Settings screen already renders State.SettingsNotice on its status line,
// so no extra UI wiring is needed. It is safe for concurrent use.
func (c *Controller) SetSettingsNotice(message, tone string) {
	ttl := c.opts.NoticeTTL
	if tone == toneBad {
		ttl = 0
	}
	c.setNotice(slotSettings, message, tone, ttl)
}

// SetDiagnosticsNotice publishes one Diagnostics-screen result notice (T3.3;
// the shell's cleanup and service outcomes). The screen-local notice rules
// apply: a bad-tone failure persists until the next run or action replaces
// it, everything else carries the 10 s success lifetime (inventory
// §Diagnostics status line). It is safe for concurrent use.
func (c *Controller) SetDiagnosticsNotice(message, tone string) {
	ttl := c.opts.NoticeTTL
	if tone == toneBad {
		ttl = 0
	}
	c.setNotice(slotDiagnostics, message, tone, ttl)
}

// visibleLogs returns the buffer entries matching the screen's local level
// and search filters, exactly the walk-era and TUI rule (filterLogEntries):
// entries at or above the level whose tag or message contains the trimmed,
// case-insensitive search text, in stream order.
func visibleLogs(entries []logging.Entry, minLevel logging.Level, search string) []logging.Entry {
	q := strings.ToLower(strings.TrimSpace(search))
	out := make([]logging.Entry, 0, len(entries))
	for _, e := range entries {
		if e.Level < minLevel {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.Tag+" "+e.Msg), q) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// renderLogText renders entries in the daemon's text log format (Entry.Line),
// one newline-terminated line each: the TUI's and the walk GUI's export
// format, so every export file greps the same way.
func renderLogText(entries []logging.Entry) string {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.Line())
		b.WriteByte('\n')
	}
	return b.String()
}

// refreshRequest is the refresh action's payload. The frozen action set has
// no dedicated server-list verb, so the Locations screen's Refresh rides
// {"locations":true} here; an absent or empty payload keeps the autonomous
// meaning. Unknown fields are errors, like every action payload.
type refreshRequest struct {
	Locations bool `json:"locations,omitempty"`
}

// dispatchRefresh re-runs the controller's autonomous loads, reusing the
// same paths Poll uses: the status snapshot reloads synchronously (a failure
// becomes the shared "could not refresh the status" notice — the Dashboard's
// Refresh/Retry wording), and the account and server-list loads re-run when
// the controller considers them absent or stale. A failed status refresh
// short-circuits, exactly like Poll's tick. Settings stay behind
// settings_get. Safe to call while Poll runs: the loading guards and the
// state mutex serialise every fetch.
//
// The optional payload {"locations":true} additionally forces a fresh
// server-list download through Daemon.Locations with the refresh flag set,
// even when a list is already loaded: the Locations screen's Refresh asks
// the daemon to re-download the server list (inventory §Locations). The
// controller only replaces the list on success, so the screen's stale rows
// stay visible while the fetch runs and after a failure; a failure reports
// the raw error on the locations notice exactly like the first-load path.
// An empty payload (the Dashboard's Refresh/Retry) keeps the guarded
// behavior above — it must never trigger a network re-download.
func (c *Controller) dispatchRefresh(act Action) error {
	req, err := decodePayload[refreshRequest](act)
	if err != nil {
		return err
	}
	if req.Locations {
		// A Locations refresh is a new visit: the screen returns to country
		// order, and the controller drops its GUI-local latency projection so
		// the reload cannot re-show labels from the previous run.
		c.mu.Lock()
		c.rtts = nil
		c.mu.Unlock()
	}
	err = c.refreshStatus(context.Background())
	if err != nil {
		c.setNotice(slotShared, "could not refresh the status: "+err.Error(), toneBad, 0)
		return err
	}
	c.ensureAccount(context.Background())
	c.ensureAccounts(context.Background())
	if req.Locations {
		c.loadLocations(context.Background(), true, true)
	} else {
		c.loadLocations(context.Background(), false, false)
	}
	return nil
}

// Poll runs the controller's daemon pump until ctx is done; callers run it
// in its own goroutine. It loads the first status/settings snapshots, starts
// the account and server-list loads in the background (their screens have no
// action in the frozen vocabulary), then applies daemon-pushed events as
// they arrive and, on the pump cadence, retires expired notices and runs the
// status/settings safety refreshes.
func (c *Controller) Poll(ctx context.Context) {
	c.refreshStatus(ctx)
	c.loadSettings(ctx)
	c.ensureAccount(ctx)
	c.ensureAccounts(ctx)
	c.ensureLocations(ctx)

	ticker := time.NewTicker(c.opts.PumpInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case env, ok := <-c.d.Events():
			if !ok {
				return
			}
			c.applyEvent(ctx, env)
		case now := <-ticker.C:
			c.tick(ctx, now)
		}
	}
}

func (c *Controller) tick(ctx context.Context, now time.Time) {
	c.retireNotices(now)
	if c.statusDue(now) {
		if c.refreshStatus(ctx) == nil {
			c.ensureAccount(ctx)
			c.ensureAccounts(ctx)
			c.ensureLocations(ctx)
		}
	}
	if c.settingsDue(now) {
		c.loadSettings(ctx)
	}
}

func (c *Controller) statusDue(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return now.Sub(c.lastStatusTry) >= c.opts.StatusInterval
}

func (c *Controller) settingsDue(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return now.Sub(c.lastSettingsTry) >= c.opts.SettingsInterval
}

// refreshStatus loads a status snapshot. The first-load failure records
// StatusError and the shared "could not refresh the status" notice — the
// Dashboard's Status unavailable + Retry case; once loaded, background
// failures leave the live snapshot and the notice alone.
func (c *Controller) refreshStatus(ctx context.Context) error {
	c.mu.Lock()
	c.lastStatusTry = time.Now()
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	st, err := c.d.Status(ctx)
	if err != nil {
		c.observe(err)
		c.mu.Lock()
		if !c.statusLoaded {
			c.statusError = err.Error()
			c.notice = newNotice(0, "could not refresh the status: "+err.Error(), toneBad)
		}
		c.mu.Unlock()
		return err
	}
	c.applyStatus(st)
	c.observe(nil)
	return nil
}

// applyStatus replaces the status projection with a fresh snapshot, from a
// reply or a pushed event. An authenticated snapshot clears a stale login
// failure: it proves the daemon holds a live session (e.g. the "already
// signed in" refusal of a second login).
func (c *Controller) applyStatus(st api.Status) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = st
	c.statusLoaded = true
	c.statusError = ""
	if st.Authenticated && c.login != nil && c.login.Error != "" {
		c.login.Error = ""
	}
}

// loadSettings fetches the settings snapshot. Failures keep the loaded rows
// and report the status-line error (inventory §Settings).
func (c *Controller) loadSettings(ctx context.Context) error {
	c.mu.Lock()
	c.lastSettingsTry = time.Now()
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, settingsTimeout)
	defer cancel()
	view, err := c.d.Settings(ctx)
	if err != nil {
		c.observe(err)
		c.setNotice(slotSettings, "could not load the settings: "+err.Error(), toneBad, 0)
		return err
	}
	c.observe(nil)
	c.mu.Lock()
	cp := view
	c.settings = &cp
	c.settingsNotice = clearNoticeError(c.settingsNotice)
	c.mu.Unlock()
	return nil
}

// ensureAccount fetches the account snapshot in the background when none is
// loaded or the session's authentication changed since the last load (a
// login, a logout, an external sign-in). The Account screen has no action in
// the frozen vocabulary, so the controller keeps its snapshot fresh itself.
func (c *Controller) ensureAccount(ctx context.Context) {
	c.mu.Lock()
	if c.accountLoading || (c.accountLoaded && c.accountAuth == c.status.Authenticated) {
		c.mu.Unlock()
		return
	}
	c.accountLoading = true
	c.mu.Unlock()

	go func() {
		defer func() {
			c.mu.Lock()
			c.accountLoading = false
			c.mu.Unlock()
		}()
		actx, cancel := context.WithTimeout(ctx, accountTimeout)
		defer cancel()
		info, err := c.d.Account(actx)
		if err != nil {
			c.observe(err)
			c.setNotice(slotAccount, "could not load the account: "+err.Error(), toneBad, 0)
			return
		}
		c.observe(nil)
		c.mu.Lock()
		cp := info
		c.account = &cp
		c.accountLoaded = true
		c.accountAuth = c.status.Authenticated
		// Only the entitlement-load error is stale here. A terminal sign-in
		// failure shares this slot and must survive the background refresh
		// (the Account screen keeps it until another action replaces it).
		c.accountNotice = clearAccountLoadNotice(c.accountNotice)
		c.mu.Unlock()
	}()
}

// ensureLocations fetches the server list in the background when none is
// loaded. The Locations screen has no load action in the frozen vocabulary,
// so the controller keeps its snapshot fresh itself.
func (c *Controller) ensureLocations(ctx context.Context) {
	c.loadLocations(ctx, false, false)
}

// loadLocations fetches the server list in the background. refresh asks the
// daemon to re-download it (the inventory's 30 s refresh); force loads even
// when a list is already loaded (the refresh action's {"locations":true}).
// The loading guard keeps one fetch in flight at a time either way. A failed
// fetch keeps the loaded rows and writes the raw error to the locations
// notice (inventory §Locations); a successful one replaces the list and
// clears a stale error.
func (c *Controller) loadLocations(ctx context.Context, refresh, force bool) {
	c.mu.Lock()
	if c.locationsLoading || (!force && c.locationsLoaded) {
		c.mu.Unlock()
		return
	}
	c.locationsLoading = true
	c.mu.Unlock()

	go func() {
		defer func() {
			c.mu.Lock()
			c.locationsLoading = false
			c.mu.Unlock()
		}()
		actx, cancel := context.WithTimeout(ctx, locationsTimeout)
		defer cancel()
		res, err := c.d.Locations(actx, refresh)
		if err != nil {
			c.observe(err)
			// The status line shows the raw load error (inventory §Locations).
			c.setNotice(slotLocations, err.Error(), toneBad, 0)
			return
		}
		c.observe(nil)
		c.mu.Lock()
		c.locations = res.Countries
		c.locationsLoaded = true
		c.locationsNotice = clearNoticeError(c.locationsNotice)
		c.mu.Unlock()
	}()
}

// applyEvent applies one daemon-pushed event: a status snapshot, a ping
// result or a live log entry (inventory §Shell "UI refresh").
func (c *Controller) applyEvent(ctx context.Context, env *ipc.Envelope) {
	if env == nil {
		return
	}
	switch env.Type {
	case ipc.TypeStatus:
		var st api.Status
		if env.DecodePayload(&st) == nil {
			c.applyStatus(st)
			c.observe(nil)
			if c.accountStale() {
				c.ensureAccount(ctx)
			}
		}
	case ipc.TypePingResult:
		var pr api.PingResult
		if env.DecodePayload(&pr) == nil {
			c.mu.Lock()
			if c.rtts == nil {
				c.rtts = make(map[string]string)
			}
			c.rtts[pr.Host] = rttLabel(pr.RTTMS)
			c.mu.Unlock()
		}
	case ipc.TypeLog:
		var e logging.Entry
		if env.DecodePayload(&e) == nil {
			c.appendLog(e)
		}
	}
}

func (c *Controller) accountStale() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.accountLoaded && c.accountAuth != c.status.Authenticated
}

// appendLog appends one live entry to the bounded buffer as long as the
// daemon subscription is active. The full-slice append keeps previously
// returned snapshots from aliasing the buffer's spare capacity.
func (c *Controller) appendLog(e logging.Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.logsSubscribed {
		return
	}
	c.logs = append(c.logs[:len(c.logs):len(c.logs)], e)
	if len(c.logs) > logsCapacity {
		c.logs = append([]logging.Entry(nil), c.logs[len(c.logs)-logsCapacity:]...)
	}
}

// mergeLogin merges one daemon login-state reply into the projection. It
// reports whether the reply is terminal (step "done").
func (c *Controller) mergeLogin(st api.LoginState) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	login := c.ensureLoginLocked()
	previousStep, previousID := login.Step, login.ID
	if st.Step != "" {
		login.Step = st.Step
	}
	if st.Email != "" {
		login.Email = st.Email
	}
	login.Method = st.Method
	login.VerificationMethod = st.VerificationMethod
	login.VerificationReason = st.VerificationReason
	login.URL = st.URL
	login.ID = st.LoginID
	// A status poll for the same waiting browser flow carries no error; it
	// must not erase the retryable completion error the waiting card shows
	// inline. The error clears on the next completion attempt (which replaces
	// it), on a new flow, on a cancel or on the terminal reply.
	sameWaitingFlow := st.Step == "browser" && st.Error == "" &&
		previousStep == "browser" && previousID != "" && previousID == st.LoginID
	if !sameWaitingFlow {
		login.Error = st.Error
	}
	if st.Step == "done" && st.Error == "" {
		if login.Email != "" {
			c.notice = newNotice(0, "Signed in as "+login.Email, toneInfo)
		} else {
			c.notice = newNotice(0, "Signed in", toneInfo)
		}
	}
	// A done+error reply is still terminal: refreshing the status lets an
	// "already signed in" refusal resolve against the live session, while the
	// error stays on the login view and suppresses the success notice above.
	return st.Step == "done"
}

func (c *Controller) ensureLoginLocked() *LoginView {
	if c.login == nil {
		c.login = &LoginView{}
	}
	return c.login
}

// noticeSlot selects one of the state's notice surfaces.
type noticeSlot int

const (
	slotShared noticeSlot = iota
	slotLocations
	slotSettings
	slotLogs
	slotAccount
	slotDiagnostics
)

// setNotice replaces one notice surface. A ttl of 0 makes the notice persist
// until another action replaces it (errors and the shared notices); a
// positive ttl is the inventory's screen-local success lifetime.
func (c *Controller) setNotice(slot noticeSlot, msg, tone string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := newNotice(ttl, msg, tone)
	switch slot {
	case slotShared:
		c.notice = n
	case slotLocations:
		c.locationsNotice = n
	case slotSettings:
		c.settingsNotice = n
	case slotLogs:
		c.logsNotice = n
	case slotAccount:
		c.accountNotice = n
	case slotDiagnostics:
		c.diagnosticsNotice = n
	}
}

// clearNoticeError drops a screen's error notice after a success heals it
// (a working snapshot clears an earlier subscribe failure, inventory §Logs);
// success notices keep their 10 s lifetime.
func (c *Controller) clearNoticeError(slot noticeSlot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch slot {
	case slotShared:
		c.notice = clearNoticeError(c.notice)
	case slotLocations:
		c.locationsNotice = clearNoticeError(c.locationsNotice)
	case slotSettings:
		c.settingsNotice = clearNoticeError(c.settingsNotice)
	case slotLogs:
		c.logsNotice = clearNoticeError(c.logsNotice)
	case slotAccount:
		c.accountNotice = clearNoticeError(c.accountNotice)
	case slotDiagnostics:
		c.diagnosticsNotice = clearNoticeError(c.diagnosticsNotice)
	}
}

// clearAccountListNotice clears only the account-list load error. The Account
// screen shares one notice slot with the entitlement load, so a successful
// accounts_list must not erase a Guardian failure (and vice versa).
func clearAccountListNotice(n *Notice) *Notice {
	if n != nil && n.Tone == toneBad && strings.HasPrefix(n.Message, "could not load accounts: ") {
		return nil
	}
	return n
}

// clearAccountLoadNotice clears only the entitlement load error. Sign-in and
// account-add failures use the same slot and must survive a successful
// background account refresh.
func clearAccountLoadNotice(n *Notice) *Notice {
	if n != nil && n.Tone == toneBad && strings.HasPrefix(n.Message, "could not load the account: ") {
		return nil
	}
	return n
}

func clearNoticeError(n *Notice) *Notice {
	if n != nil && n.Tone == toneBad {
		return nil
	}
	return n
}

func newNotice(ttl time.Duration, msg, tone string) *Notice {
	if msg == "" {
		return nil
	}
	n := &Notice{Message: msg, Tone: tone}
	if ttl > 0 {
		expires := time.Now().Add(ttl)
		n.Expires = &expires
	}
	return n
}

func expired(n *Notice, now time.Time) bool {
	return n != nil && n.Expires != nil && !now.Before(*n.Expires)
}

// liveNotice returns a copy of n unless it has expired.
func liveNotice(n *Notice, now time.Time) *Notice {
	if n == nil || expired(n, now) {
		return nil
	}
	cp := *n
	return &cp
}

func retireExpired(n **Notice, now time.Time) {
	if expired(*n, now) {
		*n = nil
	}
}

// retireNotices drops every expired screen-local notice; the shell's pump
// runs this on the UI cadence (inventory: notices are retired by the pump).
func (c *Controller) retireNotices(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	retireExpired(&c.notice, now)
	retireExpired(&c.locationsNotice, now)
	retireExpired(&c.settingsNotice, now)
	retireExpired(&c.logsNotice, now)
	retireExpired(&c.accountNotice, now)
	retireExpired(&c.diagnosticsNotice, now)
}

// observe records one daemon call's transport health: any success proves the
// control pipe is up; a dial failure flips the reachability flag the shell
// uses for the Set up Kumiho gate (inventory §behavioral).
func (c *Controller) observe(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case err == nil:
		c.reachable = true
	case errors.Is(err, ErrDaemonUnreachable):
		c.reachable = false
	}
}

func (c *Controller) statusViewLocked(now time.Time) *StatusView {
	if !c.statusLoaded {
		return nil
	}
	st := c.status
	v := &StatusView{
		Label:         stateLabel(st.State),
		Connected:     st.State == "CONNECTED" || st.State == "PROXY_ONLY",
		ProxyOnly:     st.State == "PROXY_ONLY" || st.ProxyOnly,
		Authenticated: st.Authenticated,
		Email:         st.Email,
		Location:      st.Location,
		Server:        st.Server,
		KillSwitch:    st.KillSwitch,
		Autoconnect:   st.Autoconnect,
		ExitIP:        st.ExitIP,
		ExitCountry:   st.ExitCountry,
	}
	if st.Since > 0 && v.Connected {
		v.Uptime = now.Sub(time.Unix(st.Since, 0)).Round(time.Second).String()
	}
	if st.TokenExpires > 0 {
		v.TokenExpiry = time.Unix(st.TokenExpires, 0).UTC().Format(time.RFC3339)
	}
	if st.PassExpires > 0 {
		v.PassExpiry = time.Unix(st.PassExpires, 0).UTC().Format(time.RFC3339)
	}
	v.QuotaText, v.QuotaRemaining, v.QuotaLimit = quotaView(st.Quota)
	if st.Quota != nil && st.Quota.Reset != nil {
		v.QuotaReset = time.Unix(*st.Quota.Reset, 0).UTC().Format("2006-01-02")
	}
	v.Rates = ratesLabel(st.Rates)
	v.Totals = totalsLabel(st.Totals)
	return v
}

func copyLogin(l *LoginView) *LoginView {
	if l == nil {
		return nil
	}
	cp := *l
	return &cp
}

// copyDoctor returns a copy of the doctor projection with a detached checks
// slice, so a snapshot never aliases the controller's state.
func copyDoctor(d *DoctorView) *DoctorView {
	if d == nil {
		return nil
	}
	cp := *d
	cp.Checks = append([]DoctorCheck(nil), d.Checks...)
	return &cp
}

func copySettings(s *api.SettingsView) *api.SettingsView {
	if s == nil {
		return nil
	}
	cp := *s
	return &cp
}

func copyAccount(a *api.AccountInfo) *api.AccountInfo {
	if a == nil {
		return nil
	}
	cp := *a
	return &cp
}

// copyAccounts returns a detached account-list projection: the slice and
// every row's quota pointers, so a snapshot can never alias the controller's
// live state.
func copyAccounts(v *api.AccountsView) *api.AccountsView {
	if v == nil {
		return nil
	}
	cp := *v
	cp.Accounts = append([]api.AccountSummary(nil), v.Accounts...)
	for i := range cp.Accounts {
		if q := cp.Accounts[i].Quota; q != nil {
			qc := *q
			if q.Limit != nil {
				limit := *q.Limit
				qc.Limit = &limit
			}
			if q.Remaining != nil {
				remaining := *q.Remaining
				qc.Remaining = &remaining
			}
			if q.Reset != nil {
				reset := *q.Reset
				qc.Reset = &reset
			}
			cp.Accounts[i].Quota = &qc
		}
	}
	return &cp
}

func copyRTTs(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	cp := make(map[string]string, len(m))
	for host, label := range m {
		cp[host] = label
	}
	return cp
}

// decodePayload decodes an action's raw JSON payload strictly (unknown
// fields are errors, matching the envelope's strict decoding); a missing
// payload yields the zero value.
func decodePayload[T any](act Action) (T, error) {
	var v T
	if len(act.Payload) == 0 {
		return v, nil
	}
	dec := json.NewDecoder(bytes.NewReader(act.Payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, fmt.Errorf("decode %s payload: %w", act.Name, err)
	}
	return v, nil
}

// settingLabels maps the daemon setting keys to the inventory's row labels.
var settingLabels = map[string]string{
	"kill_switch":        "Kill switch",
	"autoconnect":        "Autoconnect",
	"log_level":          "Log level",
	"redact_targets":     "Redact targets",
	"exit_check":         "Exit check",
	"doh_provider":       "DoH provider",
	"dns_upstream":       "DNS upstream",
	"edge_address":       "Edge address",
	"upstream_proxy":     "Upstream proxy",
	"exclude_cidrs":      "Exclude CIDRs",
	"allow_lan":          "Allow LAN",
	"mtu":                "MTU",
	"socks_port":         "SOCKS port",
	"quota_poll_minutes": "Quota poll",
	"tls_split":          "TLS split",
	"http_proxy":         "HTTP proxy",
	"http_proxy_port":    "HTTP proxy port",
	"solve_challenges":   "Solve challenges",
	"keepalive_profile":  "Keepalive profile",
}

func settingLabel(key string) string {
	if label, ok := settingLabels[key]; ok {
		return label
	}
	return key
}

// stateLabel maps a daemon state to the inventory's state word.
func stateLabel(state string) string {
	switch state {
	case "", "IDLE":
		return "Off"
	case "CONNECTED":
		return "Connected"
	case "PROXY_ONLY":
		return "Proxy only"
	case "CONNECTING":
		return "Connecting"
	case "RECONNECTING":
		return "Reconnecting"
	case "WAITING_NETWORK":
		return "Waiting for network"
	case "UNAUTHENTICATED":
		return "Not signed in"
	case "FATAL":
		return "Error"
	}
	return state
}

// rttLabel renders one ping result ("42 ms" or "timeout").
func rttLabel(ms *float64) string {
	if ms == nil {
		return "timeout"
	}
	return fmt.Sprintf("%.0f ms", *ms)
}

// quotaView renders the quota label and passes the raw numbers through for
// the progress bar.
func quotaView(q *api.Quota) (string, *int64, *int64) {
	if q == nil {
		return "", nil, nil
	}
	var text string
	switch {
	case q.Remaining != nil && q.Limit != nil:
		text = fmt.Sprintf("%s remaining of %s", humanBytes(*q.Remaining), humanBytes(*q.Limit))
	case q.Remaining != nil:
		text = fmt.Sprintf("%s remaining", humanBytes(*q.Remaining))
	case q.Limit != nil:
		text = humanBytes(*q.Limit)
	}
	return text, q.Remaining, q.Limit
}

func ratesLabel(r *api.ByteRates) string {
	if r == nil || (r.Up == 0 && r.Down == 0) {
		return ""
	}
	return fmt.Sprintf("up %s/s / down %s/s", humanBytes(int64(r.Up)), humanBytes(int64(r.Down)))
}

func totalsLabel(t *api.ByteCounters) string {
	if t == nil {
		return ""
	}
	return fmt.Sprintf("up %s / down %s", humanBytes(t.Up), humanBytes(t.Down))
}

// humanBytes formats a byte count with binary units (house wording matches
// the CLI and TUI).
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
