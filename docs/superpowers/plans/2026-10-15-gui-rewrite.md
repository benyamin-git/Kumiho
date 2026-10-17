# Kumiho GUI Rewrite (v0.3.0) — Implementation Plan

**Spec:** `docs/superpowers/specs/2026-10-15-gui-rewrite-design.md` (approved)
**Status:** Approved
**Date:** 2026-10-15

Parity references (canonical): `docs/gui-feature-inventory.md`,
`docs/cli-tui-abilities.md`. Reference-only implementation history:
`archive/walk-gui` (local branch; never merged).

## Global constraints (verbatim from spec + repo rules)

- Go 1.27.1 (`go.mod`); target **windows/amd64** (GUI) and linux/amd64 (dev
  harness); no cgo; dependencies vendored (`vendor/`, commit `go.mod`,
  `go.sum`, `vendor/` together); builds and tests must not need the network.
- Every commit leaves this gate green:
  `make lint && make test && go test -race -count=1 ./... && make build && GOOS=windows GOARCH=amd64 go vet ./... && GOOS=windows GOARCH=amd64 go build ./... && GOOS=android GOARCH=arm64 go build ./...`
- Protocol v1 byte-identical; daemon/CLI/TUI behavior untouched (only the
  pipe ACL work of D14 and docs change).
- No raw hex outside the theme engine; house style, the twelve bans and the
  accessibility floor bind (per `design` skill reference).
- Names: binaries `kumiho-gui.exe`, `kumiho-gui-web`, `kumiho-gui-probe`;
  packages `internal/gui` (+ `web/`, `mock/`, `theme/`, `prefs/`), `cmd/kumiho-gui`,
  `cmd/kumiho-gui-web`, `cmd/kumiho-gui-probe`; harness port **7407**;
  prefs `%AppData%\kumiho\gui.toml`; host log `%LocalAppData%\kumiho\gui.log`.
- Commit style `type: lowercase imperative summary`; never push/tag;
  `.superpowers/` and `.ai-state` out of commits.
- Brand assets are recovered from `archive/walk-gui` (assets, not code):
  `assets/kumiho.ico`, `assets/kumiho.manifest`, `cmd/kumiho-gui/rsrc_windows.syso`.

## Milestones and tasks

Each task is one commit-sized unit. Verification commands are exact; user-VM
verifications are marked **[user]**.

### M0 — Probe (gates everything; do not start M1 until it passes)

**T0.1 `build: vendor the webview2 binding`**
Files: `go.mod`, `go.sum`, `vendor/github.com/jchv/go-webview2/**`,
`vendor/github.com/jchv/go-winloader/**`, `NOTICE`.
Interface: none (dependency only). `go get github.com/jchv/go-webview2@latest`
then `go mod vendor`.
Verification: `go mod verify`; gate green. Commit: as titled.

**T0.2 `feat: add the webview2 probe tool`**
Files: `cmd/kumiho-gui-probe/main.go` (windows-tagged), `Makefile`
(`build-probe` → `dist/kumiho-gui-probe-windows-amd64.exe`).
Probe (kept permanently as a support diagnostic): opens a window without any
repo chrome; page shows `WebView2 OK <runtime version>` from a JS→Go echo
round-trip; a tray icon with a Quit menu appears; a `kumiho` pipe `hello`
result line prints (via `internal/ipc`); if the runtime is missing it shows
the fallback page with the Microsoft link (validates D15's UX early).
Verification: `GOOS=windows GOARCH=amd64 go build ./...`; `make build-probe`;
then **[user]** runs the exe on the Windows VM and reports: window shows OK,
tray icon appears, hello prints. **M1 is gated on this.**

### M1 — Shell, bridge, mock, harness

**T1.1 `feat: add the gui bridge protocol`**
Files: `internal/gui/bridge.go`, `internal/gui/bridge_test.go` (untagged,
linux-testable).
Interfaces: host→UI `{"type":"state","state":{…}}`; UI→host
`{"type":"action","name":"<name>","payload":{…}}`. Action names (frozen for
the whole plan): `connect, disconnect, select_location, ping, settings_get,
settings_set, login_email, login_password, login_2fa, logout,
logs_snapshot, logs_subscribe, logs_unsubscribe, logs_clear, logs_export,
open_logs_folder, cleanup, service_start, service_stop, update_check,
open_external, quit, window_show`. Types: `type Action struct{ Name string;
Payload json.RawMessage }`, `EncodeState(State) ([]byte, error)`,
`DecodeAction([]byte) (Action, error)`.
Verification: `go test ./internal/gui/` (round-trips, unknown-name errors).

**T1.2 `feat: add the gui controller`**
Files: `internal/gui/controller.go`, `internal/gui/client.go`,
`internal/gui/controller_test.go` (all untagged).
Interfaces: `type Daemon interface` — same method vocabulary as the base
protocol (`Status, Connect(api.ConnectPayload), Disconnect, SelectLocation,
Locations(refresh bool), Ping([]string), Settings, SetSetting, Account,
LogsSnapshot, LogsSubscribe, LogsUnsubscribe, LogsClear, LoginEmail,
LoginPassword, Login2FA, Logout`); `client.go` adapts `internal/ipc.Client`.
`NewController(d Daemon, opts Options) *Controller`;
`(*Controller).Snapshot() State`; `(*Controller).Dispatch(Action) error`;
`(*Controller).Poll(ctx)` refreshes status/settings on the inventory's
cadence; state carries notifications with lifetimes per inventory.
`State` = api-derived projections + GUI extras (`Theme, Accent string;
Service ServiceState; Update UpdateInfo`).
Verification: `go test -race ./internal/gui/` with a fake daemon (state
transitions, error propagation, reconnect semantics per inventory §behavioral).

**T1.3 `feat: add the gui theme engine`**
Files: `internal/gui/theme/theme.go`, `theme/oklch.go`, `theme/palette.go`,
`theme/theme_test.go` (untagged).
Interfaces: `type Theme string` (`light`, `dark`, `oled`);
`type Accent struct{ Name string; Hue, Chroma float64 }`; the six house
accents; `func CSS(theme Theme, accent string) (string, error)` emitting
`:root{--primary:…;…}` per the house OKLCH rules (documented role factors,
chroma-halving gamut clip — not a boundary search), neutrals and status
colors per the house tables.
Verification: `go test ./internal/gui/theme/` asserting the documented
primaries (`blue` dark `#7daeec`, light `#35639c`, …), all neutrals/status
hexes, and clip cases.

**T1.4 `feat: add the gui preferences`**
Files: `internal/gui/prefs/prefs.go`, `prefs/prefs_windows.go`,
`prefs/prefs_other.go`, `prefs/prefs_test.go`.
Interfaces: `Prefs{Theme, Accent string; Window struct{X,Y,Width,Height int};
Autostart, Onboarded, CloseHintShown bool}` with TOML tags exactly
(theme, accent, window{x,y,width,height}, autostart, onboarded,
close_hint_shown); `Load() (Prefs, error)`, `Save(Prefs) error` (atomic
write); Windows path `%AppData%\kumiho\gui.toml`; dev override env
`KUMIHO_GUI_PREFS` for the harness. Defaults: theme from OS preference
(light/dark only; OLED opt-in), accent `blue`, autostart `true`.
Verification: `go test ./internal/gui/prefs/` (round-trip, defaults, unknown
theme fallback).

**T1.5 `feat: add the mock daemon`**
Files: `internal/gui/mock/mock.go`, `mock/scenario.go`, `mock/mock_test.go`
(untagged).
Interfaces: implements `gui.Daemon` with canned, deterministic data; scenarios
`disconnected/connected/connecting/error`; injection `SetLatency(d)`,
`FailNext(err)`, `EmitLog(api.LogEntry)`; drives the harness dev panel a
later task adds.
Verification: `go test ./internal/gui/mock/` + controller-against-mock tests.

**T1.6 `feat: add the web asset server`**
Files: `internal/gui/web/server.go`, `web/assets.go`, `web/server_test.go`
(untagged; assets live in `internal/gui/web/static/` — placeholder index for
now).
Interfaces: `New(fs fs.FS, opts Options) *Server` with
`Options{Addr string; Token string; Dev bool}`; `(*Server).Start() error`,
`(*Server).URL() string`. Static GET only, no directory listing, CSP header
`default-src 'self'`, constant-time token check when set; loopback ephemeral
mode (shell) and fixed `:7407` mode (harness) both supported; index.html
served with the theme CSS block injected before load (first-paint, no flash).
Verification: `go test ./internal/gui/web/` (httptest: token enforced, 404s,
no listing, injection present).

**T1.7 `feat: add the gui host shell`**
Files: `cmd/kumiho-gui/main.go`, `internal/gui/host_windows.go`,
`internal/gui/chrome_windows.go`, `internal/gui/singleinstance_windows.go`,
`internal/gui/log_windows.go` (all windows-tagged); recovered brand assets
(`assets/kumiho.ico`, `assets/kumiho.manifest`, `cmd/kumiho-gui/rsrc_windows.syso`
restored from `archive/walk-gui`); `Makefile` (`build-gui` →
`dist/kumiho-gui-windows-amd64.exe`; `build-windows` builds both exes).
Behavior: loads prefs → computes theme → starts asset server (loopback+token)
→ creates window (`go-webview2`, title from prefs), theme-synced native
titlebar (dwmapi lazy load), geometry restore/clamp, close→hide, `--version`
prints `kumiho-gui <version> <commit>` (works under redirection),
`--tray-start` starts hidden; single instance: named mutex + class-name
window find → `ShowWindow(SW_RESTORE)` + foreground, then exit; host log to
`%LocalAppData%\kumiho\gui.log` with rotation cap.
Verification: `GOOS=windows GOARCH=amd64 go vet ./... && go build ./...`;
`make build-gui`; **[user]** at M0-era smoke or M6 — until then cross-build
only.

**T1.8 `feat: add the tray icon`**
Files: `internal/gui/tray_windows.go`, `internal/gui/trayicon_windows.go`
(windows-tagged).
Behavior: `Shell_NotifyIconW` on the shell's thread/pump; tooltip
`Kumiho — <state>`; menu: dynamic Connect/Disconnect, Location submenu (from
controller locations, REC first), Show, Diagnostics, Quit; left-click shows
the window (per inventory); first-close one-time notification support (used
by T4.2).
Verification: `GOOS=windows … go build ./...`; **[user]** in M6.

**T1.9 `feat: add the kumiho gui launcher`**
Files: `internal/cli/gui_windows.go`, `internal/cli/gui_other.go`,
`internal/cli/gui_other_test.go`, `internal/cli/cli.go` (register `gui`).
Behavior: launches `kumiho-gui.exe` (beside the CLI binary, then
`%ProgramFiles%\Kumiho`); the shell's single-instance guard handles focus;
Windows exit codes mirror `kumiho gui` conventions; other OSes exit 2 with a
message.
Verification: `go test ./internal/cli/`; linux `go build ./...`.

**T1.10 `feat: add the gui web harness`**
Files: `cmd/kumiho-gui-web/main.go` (untagged), `internal/gui/web/bridge.go`
(server-side harness transport: **SSE + POST**, stdlib only — no websocket
dependency; `GET /events` streams state envelopes, `POST /action` accepts
actions), `internal/gui/web/dev.go` (dev-only panel to drive mock scenarios),
`scripts/dev-gui-web.sh` (start/stop, logs), `Makefile` (`build-web` →
`dist/kumiho-gui-web-linux-amd64`).
Liveserver compliance (D26): binds `0.0.0.0:7407` strictly (fails if busy),
port overridable via `KUMIHO_GUI_WEB_PORT` (default 7407); registered in
`/srv/stacks/dashboard/html/config.yaml` as `kumiho: 7407`; documented in
`AGENTS.md` dev scripts and `README.md` run commands (done here, refined in
T6.3).
Verification: `go test ./internal/gui/web/` (SSE round-trip, action POST);
`make build-web`; manual: `scripts/dev-gui-web.sh start` serves the mock UI on
7407.

**T1.11 `test: add the visual capture script`**
Files: `scripts/visual/capture.mjs`, `scripts/visual/README.md` (dev-time
only; runs via `npx playwright`; never part of the gate or CI).
Captures (D28): every screen × 3 themes; full 6-accent sweep on the
representative screen (dashboard); key states (connected/connecting/error,
wizard steps, dialogs). Output under `/tmp/opencode/captures/` (never
committed).
Verification: run once against the harness; screenshots reviewed.

### M2 — Core screens (harness-driven; each task ends with captures reviewed)

**T2.1 `feat: add the ui foundation`**
Files: `internal/gui/web/static/index.html`, `static/css/tokens.css`,
`static/css/app.css`, `static/js/transport.js` (postMessage in the shell,
SSE+POST in the harness — same API), `static/js/store.js`,
`static/js/app.js`, `static/js/router.js`.
Behavior: 200px rail (six destinations, 40px rows, 8px radius, active
marked), 64px header (title + status), content column; component primitives
(buttons filled/tonal/text, inputs, cards, dialogs, chips, tables) per house
style; focus rings, themed scrollbars, `prefers-reduced-motion` honors;
semantic HTML + ARIA; desktop-only min width.
Verification: `go test ./...` (embed build); capture script smoke; accessibility
floor items 1–6 checked in review.

**T2.2 `feat: add the dashboard screen`**
Files: `static/js/screens/dashboard.js`, `static/css/screens/dashboard.css`.
Behavior (inventory §Dashboard): state chip, Connect/Disconnect, Refresh,
conditional Retry, 11-row status card with quota bar, persistent notices,
"Connect options" popover (location picker, proxy-only toggle, kill-switch
on/off/last via settings-then-connect — same sequence as the CLI), one-click
connect uses defaults.
Verification: controller projection tests; capture set for its states.

> **Post-ship delta (v0.3.0):** the state chip was dropped after this task —
> the shell header's status word is the single state display (see the
> post-rewrite note in `docs/gui-feature-inventory.md`). The rest of the task
> shipped as written.

**T2.3 `feat: add the locations screen`**
Files: `static/js/screens/locations.js`, `static/css/screens/locations.css`.
Behavior (inventory §Locations): filter, country/city/server tree, REC-first,
`· selected` marks, Test-speed capped at 16 visible hosts with `42 ms`/
`timeout` labels, refresh, select.
Verification: mock-driven tests + captures.

**T2.4 `feat: add the settings screen and update check`**
Files: `static/js/screens/settings.js`, `static/css/screens/settings.css`,
`internal/gui/update.go`, `internal/gui/update_test.go`.
Behavior (inventory §Settings): `Kumiho app` rows (theme switcher
Light/Dark/OLED, accent switcher with the six accents, autostart toggle,
Check for updates + version display) above the 14 `Background service` rows
with immediate-vs-next-connect apply, overridden marks and Reset; wheel/Tab
scrolling without a native scrollbar look. Update check (A3): on demand only,
`GET https://api.github.com/repos/benyamin-git/Kumiho/releases/latest` with
User-Agent `kumiho-gui`, 10s timeout, parses tag + html_url; result as a
notice; link opens the system browser.
Verification: `go test ./internal/gui/`; captures in all three themes and two
accents.

### M3 — Remaining screens

**T3.1 `feat: add the account screen`**
Files: `static/js/screens/account.js`, `static/css/screens/account.css`.
Behavior (inventory §Account): entitlement rows, sign-out with confirm dialog
(destructive styling), states.
Verification: captures + mock tests.

**T3.2 `feat: add the logs screen`**
Files: `static/js/screens/logs.js`, `static/css/screens/logs.css`, plus hosts
actions `logs_export` / `open_logs_folder` in `internal/gui/controller.go`.
Behavior (inventory §Logs, caps per A4): level filter, search, follow toggle,
clear, export (host writes the file, parity path from inventory), open folder
(Explorer), console newest-at-bottom, snapshot+subscribe on visit with clean
unsubscribe on leave.
Verification: controller tests (subscribe lifecycle); captures.

**T3.3 `feat: add the diagnostics screen`**
Files: `static/js/screens/diagnostics.js`, `static/css/screens/diagnostics.css`,
`internal/gui/elevate_windows.go` (UAC helper: `ShellExecuteEx` `runas` +
wait + exit code), `internal/gui/service_windows.go` (SCM state query +
elevated `sc.exe start/stop`).
Behavior (inventory §Diagnostics + D16/D17): first-visit doctor run using
`internal/doctor` in-process with the inventory's friendly check cards and
status chips, Copy details, Re-run, Cleanup… (UAC `kumiho cleanup`, result
notice), service state card with Start/Stop via UAC.
Verification: `go vet` windows; captures.

**T3.4 `feat: add the onboarding wizard`**
Files: `static/js/screens/onboarding.js`, `static/css/screens/onboarding.css`.
Behavior (D12/inventory §wizard): five steps — welcome + consent → login
(email/password/2FA over existing protocol) → health check → autoconnect
choice → finish (`Open Kumiho`); auto-shown while `onboarded=false`; polished
copy; no dead-ends when already signed in.
Verification: mock-driven flow tests + captures of every step in Dark and
Light.

### M4 — Tray and lifecycle polish

**T4.1 `feat: wire the tray to the controller`**
Files: `internal/gui/tray_windows.go` (+ `internal/gui/state_hooks_windows.go`).
Behavior: live tooltip state words, Connect/Disconnect enablement and actions,
Location submenu content, Show focuses/un-minimizes, Diagnostics opens the
screen, Quit exits the process cleanly.
Verification: cross-build; **[user]** M6.

**T4.2 `feat: polish the app lifecycle`**
Files: `internal/gui/host_windows.go`, `internal/gui/chrome_windows.go`,
`internal/gui/autostart_windows.go` (Run entry write/remove for the Settings
toggle), `internal/gui/prefs` wiring.
Behavior: `--tray-start` at logon (Run entry carries it) starts hidden;
first-close one-time notification sets `close_hint_shown`; geometry saved on
move/resize and clamped to the monitor set on restore; theme change
re-syncs the titlebar; second launch focuses the window (guard verified
end-to-end).
Verification: cross-build; **[user]** M6 (checklist covers each item).

### M5 — Installer, pipe access, packaging

**T5.1 `feat: grant the installing user on the control pipe`**
Files: `internal/ipc/transport_windows.go`, `internal/ipc/transport_windows_test.go`.
Behavior (D14, hardened semantics from day one): read
`%ProgramData%\kumiho\pipe-users` (one SID per line; skip blanks/malformed;
UTF-8 BOM tolerated; duplicates collapsed; canonicalize via `go-winio`;
fail-closed on unreadable), append resolved SIDs to the pipe SDDL after the
`kumiho` group entry; log downgrades when the group is missing.
Verification: `GOOS=windows go vet ./...`; the windows CI job runs
`go test` for this package (**[CI]**; note: windows-only tests do not run on
linux).

**T5.2 `feat: install the gui and the launcher`**
Files: `install.ps1`, `install.cmd` (new; CRLF rule added to `.gitattributes`
with the cmd.exe label-scan comment), `.github/workflows/ci.yml` (parse
checks unchanged).
Behavior: installs `kumiho-gui.exe` beside `kumiho.exe`, Start-menu shortcut
"Kumiho", HKCU Run entry `Kumiho GUI --tray-start` (default on), appends the
installing user's canonical SID to `pipe-users`; `-Uninstall` removes the
entry, shortcut and the user's SID line, keeps `%ProgramData%\kumiho`;
messages mirror the base script's tone.
Verification: `install.ps1` parse check in the windows CI job; **[user]**
install/uninstall section of the M6 checklist.

**T5.3 `feat: ship the gui in the release and ci`**
Files: `.github/workflows/release.yml`, `.github/workflows/ci.yml`
(windows job: `kumiho-gui.exe --version` smoke via redirection), `Makefile`
if needed.
Behavior: release zips gain `kumiho-gui.exe` (no new runtime files — loader
embedded, D15/A5); `SHA256SUMS` covers it; probe/harness never ship.
Verification: `ci.yml` changes parse; a dry local packaging run
(`make build-windows` + zip script) produces a 9-entry zip whose
`sha256sum -c` passes.

### M6 — Docs and acceptance

**T6.1 `docs: add the windows gui guide`** — `docs/gui.md`: install →
onboarding → each screen → tray/lifecycle → diagnostics (incl. the probe
tool) → uninstall → troubleshooting.
Verification: links checked; reviewed against the UI.

**T6.2 `docs: add the gui checklist to the windows testing doc`** —
`docs/windows-testing.md`: a v0.3.0 GUI section covering install/pipe/Run
entry, every inventory screen item, tray, wizard, cleanup/service UAC,
close-to-tray, single instance, update check, uninstall residue; this is the
**[user]** acceptance script.
Verification: section reviewed; **[user]** runs it on the VM.

**T6.3 `docs: describe the gui in readme, agents and changelog`** —
`README.md` (GUI section + run commands), `AGENTS.md` (architecture map adds
the GUI subsystem; Commands add `build-gui`/`build-web`/`build-probe`; Dev
scripts add the harness; Vendor policy notes `go-webview2`; the **Design**
record per the design skill: palette source, themes, type, approved date),
`CHANGELOG.md` v0.3.0 entry.
Verification: doc review; gate green.

**Acceptance (not a commit):** build the release kit, hand it to the user,
user runs T6.2's checklist on the Windows VM. Fixes land as separate
`fix:` commits. Release tag left to the user.

## Dependencies and ordering

- M0 gates M1 (probe result). M1 gates M2 (harness + foundation). M2 gates
  M3 (shared component primitives). M3 gates M4 (tray opens screens) and the
  M6 checklist content. T5.1/T5.2 are independent of screens; T5.3 depends on
  M1's build target. M6.2 depends on all screens; T6.1–T6.3 depend on the
  final UI. Acceptance depends on all of M6.
- T1.11 precedes M2's capture reviews. T1.9 (`kumiho gui`) is independent but
  meaningful once the shell exists (T1.7).
- Every task may be paused after its commit; the tree always builds, and the
  CLI/TUI/daemon never depend on GUI progress.

## Risks and rollback

- **Probe fails on the VM** → stop before M1; revisit the host approach
  (fallback: Wails under the same design decisions) — decision re-opened per
  the planning rules.
- **Playwright vs WebView2 rendering divergence** — both are Chromium; low
  risk; the VM checklist is authoritative anyway.
- **Manifest/DPI** — the restored `kumiho.manifest` is reviewed for
  PerMonitorV2 in T1.7; the provider box is 125%.
- **UAC flows can't be tested locally** — they are VM-only checklist items;
  errors surface through host-log + in-app notices.
- **Win11 notification behavior for the first-close hint** — banner instead
  of balloon is fine; checklist confirms.
- **Rollback** — every commit keeps the gate green and the GUI is additive;
  `archive/walk-gui` and the previous daemon-only release remain fallbacks;
  no data migration exists.

## Self-review

Spec sections map: objective/success → M0–M6 + acceptance; D1/D2 → M1; D3 →
all; D6/D7 → this plan + refs; D9/A3 → T2.4; D10/D11/D18 → T1.7/T4.2/T5.2;
D12 → T3.4; D13 → T2.2; D14 → T5.1/T5.2; D15/A2 → T0.2; D16/D17 → T3.3;
D19 → no task (verified no change needed); D20–D25/A1 → T1.3/T1.7/T2.1/T4.2;
D26/D27/A7 → T1.5/T1.10; D28 → T1.11; D29 → T1.9/T5.3; D30 → M6; D31 → T1.4;
D32 → T1.6; D33 → T1.7; D34 → T1.7 + T0.2; A4 → T3.2; A5 → T5.3; A6 →
global constraints. Names, ports and paths are consistent across tasks; every
task states exact files, interfaces, verification and commit.

## Open questions

None.
