# Kumiho GUI Rewrite (v0.3.0) — Design Spec

**Status:** Approved
**Date:** 2026-10-15

## Objective / why

Replace the deleted walk-based Windows GUI with a fresh, web-technology GUI for
Kumiho: a thin native Go shell hosting a WebView2 view whose UI is hand-written
HTML/CSS/JS embedded in the binary. The rewrite exists because the old stack
(native Win32 toolkit with fully owner-drawn controls) made every fix slow and
was impossible to run or see outside a Windows VM; the new stack keeps the UI
fully local — the same assets run in a browser on the Linux dev box against a
mock daemon — while shipping a real, native-feeling Windows 11 application. The
result must exceed the old GUI in polish and expose every ability the CLI and
TUI have, with no corner cuts.

Reference documents (canonical, not duplicated here):

- `docs/gui-feature-inventory.md` — every user-visible ability of the old GUI.
- `docs/cli-tui-abilities.md` — every CLI/TUI ability and the IPC surface.
- `archive/walk-gui` (local branch) — the deleted implementation, for reference
  only; never merged back.

## Success criteria / definition of done

1. **Parity + polish, verified on hardware.** On a Windows 11 amd64 box,
   `kumiho-gui.exe` performs every item in `docs/gui-feature-inventory.md` and
   gives access to every ability in `docs/cli-tui-abilities.md`, with zero known
   bugs; the product-polish pass (first-run, empty states, error copy,
   installer quality) is complete. Verification: the v0.3.0 GUI sections of
   `docs/windows-testing.md`.
2. **Local proof before every handoff.** All screens render in the dev harness
   under the mock daemon in all three themes; a full accent sweep and the key
   state set are captured via Playwright and reviewed; the accessibility floor
   and the twelve bans pass in Light, Dark and OLED.
3. **Install experience.** Fresh install on Win11: service installed, tray
   running after first launch, autostart entry present, pipe access works for
   the installing user without a re-login; uninstall leaves no GUI residue.
4. **Release.** v0.3.0 zip ships `kumiho-gui.exe` beside `kumiho.exe` with
   `SHA256SUMS`; the every-commit gate and CI cross-build the GUI.
5. **No collateral change.** Daemon, IPC protocol, CLI and TUI are
   behaviorally untouched: protocol v1 stays byte-identical, and the existing
   test suites pass unmodified (docs updates excepted).

## Scope

### In scope

- New Windows-only GUI: shell host, tray, lifecycle, web UI, mock harness.
- Installer, pipe access, `kumiho gui` launcher, release packaging, CI, gate.
- Documentation set: GUI guide, testing checklist, README/AGENTS/CHANGELOG.
- Design system record (house style) written into `AGENTS.md` after approval.

### Non-goals (explicitly out)

- No Linux end-user GUI (the harness is a dev tool only).
- No telemetry, no background update checks, no auto-update.
- No localization; English only (logical CSS properties still used).
- No changes to daemon networking, engine, or TUI/CLI behavior.
- No migration of any GUI state (the old GUI never shipped; nothing to migrate).

## Decision log

| # | Decision | Rationale | Status | Source |
| --- | --- | --- | --- | --- |
| D1 | Hand-rolled Go host; `go-webview2` (pure Go, loader embedded); native window; own tray | Repo stays pure Go, vendored, hermetic; no frameworks whose internals bit us before | decided | user |
| D2 | Hand-written HTML/CSS/JS via `go:embed`; no npm, no build step | Everything readable, diffable, hermetic | decided | user |
| D3 | One cutover release; parity with old GUI + full CLI/TUI ability parity + product polish | No corner cuts; single mental model | decided | user |
| D4 | Walk era deleted (reset to `7c8a760`), preserved in `archive/walk-gui`; inventories committed | Clean history; reference kept | decided | user |
| D5 | One implementation plan with ordered milestones | Reviewable checkpoints without two documents | decided | user |
| D6 | Spec → `docs/superpowers/specs/2026-10-15-gui-rewrite-design.md`; plan → `docs/superpowers/plans/2026-10-15-gui-rewrite.md` | Existing convention | decided | user |
| D7 | `docs/gui-feature-inventory.md` + `docs/cli-tui-abilities.md` stay canonical; spec/plan link them | Single source for parity claims | decided | user |
| D8 | Non-goals: no Linux GUI, no telemetry, no localization | Scope control | decided | user |
| D9 | Updates: manual "Check for updates" only (official GitHub releases feed) | No background network calls | decided | user |
| D10 | Close hides to tray; quit via tray; single-instance guard (second launch focuses) | Standard tray-app lifecycle, one tray icon | decided | user |
| D11 | Autostart default on via per-user "Kumiho GUI" Run entry; removable in Settings/uninstall | Matches old behavior; expected for a VPN client | decided | user |
| D12 | Wizard: same five steps and capabilities; copy/visuals polished | Polish without changing the flow | decided | user |
| D13 | Dashboard "Connect options" popover: location, proxy-only, kill-switch (on/off/last); defaults from settings; one-click connect uses defaults | Full CLI connect surface, tucked away | decided | user |
| D14 | Pipe access: rebuild per-user allowlist (`%ProgramData%\kumiho\pipe-users`, hardened parsing from day one) + keep `kumiho` group fallback | Works immediately after install, no re-login | decided | user |
| D15 | WebView2 runtime: require preinstalled (Windows 11); graceful page + Microsoft link if missing; no bootstrapper | Win11 always has it; lean zip | decided | user |
| D16 | Cleanup: Diagnostics action → UAC-elevated `kumiho cleanup`; result shown in-app | Parity with CLI, right place | decided | user |
| D17 | Service: GUI shows state; Start/Stop via UAC; install/uninstall stays installer-only | Honest service visibility | decided | user |
| D18 | Autostart starts hidden in tray; one-time first-close notification | No surprise window at logon; close is not quit | decided | user |
| D19 | Protocol v1 unchanged, byte-identical; no IPC additions (login/2FA, proxy-only, kill-switch all exist; doctor in-process via `internal/doctor`; cleanup/service external + elevated) | Verified against base protocol and CLI/TUI code | decided | recommendation, verified |
| D20 | Typeface: `system-ui` → Segoe UI Variable, one family | Native Win11 rendering, zero assets | decided | user |
| D21 | Type scale: 12/14/16/20/24/32 px | House closed scale | decided | user |
| D22 | Shell: 200px left rail (six destinations, 40px rows, 8px radius), 64px header; desktop-only | Established layout, house-restyled | decided | user |
| D23 | Native titlebar, theme-synced via `DwmSetWindowAttribute` | Free snap/shadow/accessibility | decided | user |
| D24 | Window: 1080×720 default, 900×600 min; remember size/position (clamped to monitors) | Comfortable on the provider box; remembers choices | decided | user |
| D25 | Themes: Go computes the full OKLCH palette per theme+accent and injects CSS custom properties | One source of truth; no generated files; instant switching | decided | user |
| D26 | Harness on port 7407, dashboard-registered, liveserver conventions, `KUMIHO_GUI_WEB_PORT` override | Follows the home-server port policy | decided | user |
| D27 | Harness uses a mock daemon only; the mock doubles as the Go test fixture | Safest dev loop; no real tunnel on the dev box | decided | user |
| D28 | Visual verification: every screen × 3 themes + full accent sweep on a representative screen + key states, via Playwright; screenshots reviewed but not committed | Strong local proof | decided | user |
| D29 | Release v0.3.0; `kumiho gui` launcher returns (Windows-only; exit 2 elsewhere) | Continues the planned version; CLI convenience | decided | user |
| D30 | Full doc set: `docs/gui.md`, windows-testing GUI checklist, README, AGENTS.md, CHANGELOG | Product polish includes docs | decided | user |
| D31 | Prefs: `%AppData%\kumiho\gui.toml` — old keys + window geometry + one-time-hint flag | Go-readable at startup for flash-free theming | decided | user |
| D32 | Assets: in-process loopback server (127.0.0.1, ephemeral, token URL, static only); shared with harness | Proven, tiny surface, code reuse | decided | user |
| D33 | Host log: `%LocalAppData%\kumiho\gui.log`, rotation cap | Supportability | decided | user |
| D34 | Hardening bundle: no DevTools, context menu (text inputs exempt), Ctrl+wheel zoom, or drag-drop in release; external links open the system browser | Small, documented surface | decided | user |
| D35 | Milestones: probe → host+bridge+mock → core screens → remaining screens → tray+lifecycle → installer/pipe/launcher → packaging; additive rollback (daemon/CLI/TUI work regardless) | De-risked ordering | decided | user |
| A1 | House style defaults: six accents (blue default), Light/Dark/OLED with OS-follow first run and OLED opt-in, 40px controls, 4px spacing grid, themed scrollbars, accessibility floor and twelve bans never bend | House rules; flagged for override | assumed | recommendation |
| A2 | Probe = window + webview render + JS↔Go echo + tray icon + pipe `hello` ping, run once by the user on the VM | De-risks every host integration at once | assumed | recommendation |
| A3 | Update check: `releases/latest` on `github.com/benyamin-git/Kumiho`, unauthenticated, on demand only | Simple, no token | assumed | recommendation |
| A4 | Log view caps at inventory parity; virtualize only if measured need | Don't gold-plate | assumed | recommendation |
| A5 | Packaging: same zip layout + `kumiho-gui.exe`; no new runtime files (loader embedded); gate and CI gain the GUI cross-build | Consistent with repo conventions | assumed | recommendation |
| A6 | Repo layout: `cmd/kumiho-gui`; `internal/gui` (host, windows-tagged), `internal/gui/web` (assets), `internal/gui/mock` | Mirrors the repo's shape | assumed | recommendation |
| A7 | Mock daemon supports state, latency and error injection for tests and the harness | Needed for D27/D28 | assumed | recommendation |

## Architecture

**Processes.** The daemon (`kumiho.exe`) is untouched. The GUI is one new
Windows binary, `kumiho-gui.exe`, plus a dev-only harness (`kumiho-gui-web`,
untagged, never released). The CLI/TUI remain the other clients.

**Shell host (Go, windows-only).** `go-webview2` creates the native window and
the WebView2 control; the host adds: single-instance guard (focuses the
existing window), tray icon on the same thread and message pump (status word
in tooltip; menu: Connect/Disconnect, location submenu, Show, Diagnostics,
Quit), close-to-tray with one-time hint, geometry memory, theme-synced native
titlebar, host log file, and the startup sequence (load prefs → compute theme
→ start asset server → create webview → navigate).

**Bridge.** Two JSON directions over the WebView2 message channel. Host→UI:
state snapshots (status, locations, settings, account, logs — the same
information model as the old view-model; pushed on change and a slow tick).
UI→host: a fixed action vocabulary (connect with options, disconnect, select
location, ping, set setting, login steps, sign out, logs subscribe/clear,
cleanup request, service start/stop, update check, open external). The UI never
speaks IPC and holds no authority; the daemon remains the only authority and
the Go side the only orchestrator. Elevated actions (cleanup, service
start/stop) run as UAC-elevated external calls from the host, with results fed
back into the same state stream.

**Assets.** The UI is `index.html` + CSS + JS per screen, embedded via
`go:embed`; the host serves them from an in-process server bound to 127.0.0.1
on an ephemeral port with a per-run token in the URL (static files only; CSP
`default-src 'self'`). The harness reuses the same server code on 7407.

**Theme engine.** Go computes every palette (background, surfaces, outline,
primary/secondary/tertiary families, status colors) from the house OKLCH rules
— including the documented gamut-clip (halve chroma to first in-gamut) — for
the active accent and theme, and injects them as CSS custom properties before
first paint (no flash) and on change. Components reference roles only; no raw
hex outside the theme engine.

**Mock daemon + harness.** The mock implements the same interface the real
controller exposes to the bridge, with canned data plus state/latency/error
injection. The harness serves the UI on 7407 with a dev-only panel to drive
states; the same mock backs Go unit tests.

**Installer & access.** `install.ps1`/`install.cmd` gain: install
`kumiho-gui.exe`, Start-menu shortcut, per-user Run entry, and the hardened
`pipe-users` append (canonicalized SID; daemon parses lines fail-closed,
dedupes, skips malformed input). Uninstall removes the entry, the user's
`pipe-users` line, and shortcuts, keeping `%ProgramData%\kumiho` state per the
current convention. `kumiho gui` launches/focuses the GUI.

## Risks + rollback

- **WebView2 on the provider VM** — mitigated by the probe (A2) as the very
  first milestone; fallback message page if the runtime is absent (D15).
- **Tray + webview message-pump integration** — same-thread integration is the
  known-good pattern; the probe includes the tray.
- **DPI/125% scaling and Segoe UI Variable rendering** — Playwright cannot see
  Windows rendering; mitigated by theme/font stack choices and the user's VM
  pass; the id checklist covers the 125% box.
- **Mock-vs-real divergence** — mitigated by keeping the mock behind the same
  interface and by the windows-testing checklist exercising the real pipe.
- **Silent failures** — the host log (D33) captures them for support.
- **Rollback.** The release is purely additive: daemon/CLI/TUI work with or
  without the GUI, protocol is untouched, and `archive/walk-gui` plus the
  previous daemon-only release remain fallbacks. No data migration exists to
  roll back.

## Dependencies / ordering

- Milestone order (D35): probe → host shell + bridge + mock harness → core
  screens (dashboard, locations, settings) → remaining screens (account, logs,
  diagnostics, wizard) → tray + lifecycle → installer/pipe/launcher → release
  packaging. The probe gates the host design; the harness gates all screen
  work; the installer work gates the release.
- `internal/doctor` (already in tree) provides doctor checks in-process.
- No new runtime dependencies beyond `go-webview2` (+ its `go-winloader`),
  vendored with `go.mod`/`go.sum`/`vendor/` and NOTICE updates.
- Installer/pipe work depends on nothing external; final verification depends
  on the user's Windows VM runs.

## Open questions

None.
