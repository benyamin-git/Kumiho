// Command kumiho-gui-web runs the Linux dev harness for the Kumiho GUI: the
// real UI assets served from the in-process asset server in a browser,
// driven by the mock daemon through the same bridge contract as the Windows
// shell (states out over SSE GET /events, actions in over POST /action, a
// /dev panel to drive mock scenarios and failures).
//
// It binds 0.0.0.0:7407 strictly — the home dashboard's registered port
// (KUMIHO_GUI_WEB_PORT overrides; a busy port fails startup, never falls
// back) — so the dashboard link works from phones and other LAN devices.
// This is a development tool; the Windows shell (cmd/kumiho-gui) is the
// product. Start it through scripts/dev-gui-web.sh, which owns the pidfile
// and the log file.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/benyamin-git/kumiho/internal/gui"
	"github.com/benyamin-git/kumiho/internal/gui/mock"
	"github.com/benyamin-git/kumiho/internal/gui/prefs"
	"github.com/benyamin-git/kumiho/internal/gui/theme"
	"github.com/benyamin-git/kumiho/internal/gui/web"
	"github.com/benyamin-git/kumiho/internal/logging"
)

// defaultPort is the harness's registered port (D26); KUMIHO_GUI_WEB_PORT
// overrides it.
const defaultPort = 7407

func main() {
	log.SetFlags(log.LstdFlags)
	log.SetPrefix("kumiho-gui-web: ")

	port, err := harnessPort(os.Getenv("KUMIHO_GUI_WEB_PORT"))
	if err != nil {
		log.Fatalf("harness: %v", err)
	}
	if err := run(port); err != nil {
		log.Fatalf("harness: %v", err)
	}
}

// harnessPort resolves the listen port: the override when set, otherwise
// defaultPort. Only a plain port number is accepted, so a typo fails loudly
// instead of binding somewhere unintended.
func harnessPort(override string) (int, error) {
	if override == "" {
		return defaultPort, nil
	}
	port, err := strconv.Atoi(override)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("KUMIHO_GUI_WEB_PORT=%q is not a valid port", override)
	}
	return port, nil
}

// run composes the harness and serves until a signal arrives: prefs →
// appearance → asset server + mock daemon + controller + bridge, then the
// controller pump and the state push loop. Shutdown follows the shell's
// order: the pumps stop on the signal context, then the server closes (which
// drops the SSE connections) and the process exits cleanly.
func run(port int) error {
	p, err := prefs.Load()
	if err != nil {
		// A malformed gui.toml must not block the harness (the shell's
		// rule): log it and start from the Linux first-run appearance.
		log.Printf("prefs: %v (starting from defaults)", err)
		p = prefs.Prefs{Theme: string(theme.Dark), Accent: theme.Accents[0].Name, Autostart: true}
	}
	th, accent := effectiveAppearance(p)
	css, err := theme.CSS(th, accent)
	if err != nil {
		return fmt.Errorf("theme: %w", err)
	}

	m := mock.New()
	ctrl := gui.NewController(m, gui.Options{
		Theme:          string(th),
		Accent:         accent,
		Onboarded:      p.Onboarded,
		LogsExport:     devLogsExport,
		OpenLogsFolder: devOpenLogsFolder,
	})
	svc := newDevService(ctrl)

	server := web.New(web.StaticFS(), web.Options{
		Addr: fmt.Sprintf("0.0.0.0:%d", port),
		Dev:  true,
	})
	harness, err := web.NewHarness(web.HarnessConfig{
		Server: server,
		Snapshot: func() ([]byte, error) {
			return gui.EncodeState(ctrl.Snapshot())
		},
		DecodeAction: func(raw []byte) (web.Action, error) {
			a, err := gui.DecodeAction(raw)
			if err != nil {
				return web.Action{}, err
			}
			return web.Action{Name: a.Name, Payload: a.Payload}, nil
		},
		Dispatch: func(a web.Action) error {
			// The shell intercepts the elevated service/cleanup actions
			// (T3.3); the harness mirrors that with its dev service.
			if svc.handles(a.Name) {
				svc.run(a.Name)
				return nil
			}
			return ctrl.Dispatch(gui.Action{Name: a.Name, Payload: a.Payload})
		},
		// The wizard's Open Kumiho flips the flag through prefs_set; the
		// harness mirrors the shell: persist, then push State.Onboarded.
		SetOnboarded: ctrl.SetOnboarded,
		Prefs:        p,
		DevControl: &web.DevControl{
			SetScenario: func(name string) { m.SetScenario(mock.Scenario(name)) },
			SetLatency:  m.SetLatency,
			FailNext:    m.FailNext,
			EmitLog: func(message string) {
				m.EmitLog(logging.Entry{Time: time.Now(), Level: logging.Info, Tag: "dev", Msg: message})
			},
		},
		Logf: log.Printf,
	})
	if err != nil {
		return err
	}
	if err := server.Start(); err != nil {
		return err
	}
	defer func() {
		if err := server.Close(); err != nil {
			log.Printf("close: %v", err)
		}
	}()
	server.SetThemeCSS(css)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go ctrl.Poll(ctx)
	go harness.Run(ctx)

	log.Printf("theme=%s accent=%s prefs=%s", th, accent, prefsPath())
	log.Printf("serving the mock UI on http://0.0.0.0:%d/ (dev panel at /dev)", port)
	<-ctx.Done()
	log.Printf("shutting down")
	return nil
}

// effectiveAppearance resolves the stored theme and accent to a valid pair,
// falling back to dark/blue (the Linux first-run default) when gui.toml
// carried unknown values.
func effectiveAppearance(p prefs.Prefs) (theme.Theme, string) {
	th := theme.Theme(p.Theme)
	if _, err := theme.CSS(th, p.Accent); err != nil {
		return theme.Dark, theme.Accents[0].Name
	}
	return th, p.Accent
}

// devLogFolder is the harness's per-user log folder for the Logs screen's
// export and open-folder actions: os.UserCacheDir()/kumiho/logs, the Linux
// analogue of the shell's %LOCALAPPDATA%\kumiho\logs (UserCacheDir honours
// XDG_CACHE_HOME, so a scratch cache is one env var away).
func devLogFolder() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", errors.New("could not determine the log folder")
	}
	dir = filepath.Join(dir, "kumiho", "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("could not create the log folder: %w", err)
	}
	return dir, nil
}

// devLogsExport is the harness's gui.Options.LogsExport: the Linux twin of
// the shell's writer — same filename shape and 0600 mode, one export file
// per click.
func devLogsExport(content string) (string, error) {
	dir, err := devLogFolder()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "kumiho-logs-"+time.Now().Format("20060102-150405")+".log")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("could not write the log export: %w", err)
	}
	log.Printf("logs_export: %s (%d bytes)", path, len(content))
	return path, nil
}

// devOpenLogsFolder is the harness's gui.Options.OpenLogsFolder: it creates
// the folder and logs it instead of spawning a file manager, returning the
// path the UI's "Opened <dir>" notice carries.
func devOpenLogsFolder() (string, error) {
	dir, err := devLogFolder()
	if err != nil {
		return "", err
	}
	log.Printf("open_logs_folder: %s", dir)
	return dir, nil
}

// devService is the harness's stand-in for the Windows shell's elevated
// service/cleanup actions (T3.3): no elevation and no SCM — it drives
// State.Service and the diagnostics notices the same way the shell does, so
// the browser harness and its captures can exercise the whole screen.
// The dev service starts out running (the plausible dev state).
type devService struct {
	ctrl *gui.Controller

	mu   sync.Mutex
	busy bool
}

// newDevService returns the dev service and publishes its initial state.
func newDevService(ctrl *gui.Controller) *devService {
	d := &devService{ctrl: ctrl}
	ctrl.SetServiceState(gui.ServiceState{Installed: true, State: "running"})
	return d
}

// handles reports whether one action belongs to the dev service (the same
// three the shell intercepts).
func (d *devService) handles(name string) bool {
	switch name {
	case gui.ActionCleanup, gui.ActionServiceStart, gui.ActionServiceStop:
		return true
	}
	return false
}

// run performs one dev action synchronously (the harness already runs
// Dispatch on its own goroutine): one action at a time, the pending state
// first, then the settled state and the inventory-shaped notice, with short
// delays so the pending states are real.
func (d *devService) run(name string) {
	d.mu.Lock()
	if d.busy {
		d.mu.Unlock()
		log.Printf("dev service: %s ignored: an action is already in flight", name)
		return
	}
	d.busy = true
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.busy = false
		d.mu.Unlock()
	}()

	switch name {
	case gui.ActionServiceStart:
		d.publish(gui.ServiceState{Installed: true, State: "start pending", Busy: true})
		time.Sleep(700 * time.Millisecond)
		d.publish(gui.ServiceState{Installed: true, State: "running"})
		d.ctrl.SetDiagnosticsNotice("Service started", "good")
	case gui.ActionServiceStop:
		d.publish(gui.ServiceState{Installed: true, State: "stop pending", Busy: true})
		time.Sleep(700 * time.Millisecond)
		d.publish(gui.ServiceState{Installed: true, State: "stopped"})
		d.ctrl.SetDiagnosticsNotice("Service stopped", "good")
	default: // cleanup
		time.Sleep(900 * time.Millisecond)
		d.ctrl.SetDiagnosticsNotice("Cleanup finished", "good")
	}
	log.Printf("dev service: %s done", name)
}

// publish pushes one dev service snapshot into the controller's state.
func (d *devService) publish(s gui.ServiceState) {
	d.ctrl.SetServiceState(s)
}

// prefsPath names the preferences file for the startup log: the
// KUMIHO_GUI_PREFS override when set, otherwise the per-OS default (the
// dev script points it into the harness scratch directory).
func prefsPath() string {
	if v := os.Getenv("KUMIHO_GUI_PREFS"); v != "" {
		return v
	}
	return "(per-OS default)"
}
