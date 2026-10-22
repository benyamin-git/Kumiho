# Using the Windows GUI

Kumiho's desktop app is the Windows 11 (amd64) face of the same daemon the
CLI and TUI talk to. It is a small native shell that renders the interface
with the Microsoft Edge WebView2 Runtime, owns the tray icon and the window,
and speaks the same local control protocol as every other client — the daemon
still does all the tunnel work. The app is a **client**, not a service: it
runs per logged-in user, while the `kumiho` service keeps running in the
background.

The app is versioned and shipped with the daemon, the CLI and `wintun.dll` in
the release zip; the installer is described in
[install.md](install.md#windows-11). This guide covers what a user sees:
installing, the first-run wizard, each screen, the tray, diagnostics and
uninstalling.

> **Reading the current UI.** This guide describes the app shipping with
> v0.4.0. The older walk-era inventory in
> [gui-feature-inventory.md](gui-feature-inventory.md) describes an earlier
> native build and differs in places; where they disagree, the running app is
> authoritative.

## Requirements

- Windows 11 amd64.
- The **Microsoft Edge WebView2 Runtime** (Evergreen). It ships with Windows
  11 and with Microsoft 365/Edge, so most machines already have it. If it is
  missing, the app cannot start and shows a dialog offering Microsoft's
  download page (see [Troubleshooting](#the-app-will-not-start)).
- The Kumiho service running (`Get-Service kumiho`). The app can start
  without it, but the header reads `Daemon unreachable` and nothing loads
  until the service is back.

## Installing

Download `kumiho-v<version>-windows-amd64.zip`, extract it, and run the
installer. For a double-click install use `install.cmd`; it self-elevates and
passes your account to the installer. From an already-administrator PowerShell,
run:

    .\install.ps1

The installer is idempotent. Beyond installing the service, the CLI and
`wintun.dll` (see [install.md](install.md#windows-11)), it also:

- installs `kumiho-gui.exe` into `%ProgramFiles%\Kumiho`,
- creates a **Start Menu** shortcut named `Kumiho` with the fox icon and the
  description `Kumiho VPN`,
- adds a **login entry** so the app starts in the tray when you sign in (a
  `Kumiho GUI` value under `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
  running `"<install>\kumiho-gui.exe" --tray-start`),
- grants the installing user access to the daemon's control pipe, so the app
  can reach the daemon without a sign-out.

Start the app from the Start Menu, or from a shell with:

    kumiho gui

`kumiho gui` looks for `kumiho-gui.exe` beside the CLI executable first, then
in `%ProgramFiles%\Kumiho`, and starts it detached, returning immediately. If
the binary is not found in either place it prints an error naming the searched
directories and pointing at `install.cmd`, and exits nonzero. The desktop app
is Windows-only; on other platforms `kumiho gui` prints a note and points at
the TUI.

Only one window runs per user. Launching a second time focuses the existing
window instead of opening another (see [Tray and lifecycle](#tray-and-lifecycle)).

## First run: the setup wizard

Before the first connect, the app shows a five-step setup wizard instead of
the screens. The rail and header are hidden while it runs. Its steps are:

1. **Welcome to Kumiho** — the consent gate. Read the two panels (*What
   Kumiho does* and *What to keep in mind*), then tick **I understand** to
   unlock **Continue**. The tick itself is not persisted: if you quit before
   finishing, the wizard starts at this step again next launch.
2. **Sign in** — sign in with the Firefox account that holds the VPN. Kumiho
   stores the resulting tokens on this PC and never your password. Depending
   on the account, this is email → password → a 2FA code, or a "check your
   email and open the Mozilla sign-in link" page; if tokens already exist, the
   step offers to continue or to sign out first.
3. **Health check** — the daemon's preflight (`kumiho doctor`). Fix anything
   marked `Failed` before connecting; warnings are usually fine.
4. **Almost done** — an **Autoconnect** toggle. If on, the daemon brings the
   tunnel up whenever it starts; you can change this later in Settings.
5. **You're all set** — **Open Kumiho** finishes setup and lands on the
   Dashboard. From the Dashboard, press `Connect`.

When you finish, the app records that setup is complete, and later launches
go straight to the shell.

## The shell

The window has a header and a left rail; one screen is shown at a time.

- **Header.** The screen title on the left; the daemon status word on the
  right. The word is one of:
  - `Loading...` — before the first status arrives;
  - `Daemon unreachable` — the control pipe could not be reached (shown in
    the warning tone);
  - `Status unavailable` — the first status load failed for another reason
    (error tone);
  - otherwise the live connection state: `Off`, `Connected`, `Proxy only`,
    `Connecting`, `Reconnecting`, `Waiting for network`, `Not signed in` or
    `Error`.
- **Rail.** `Dashboard`, `Locations`, `Settings`, `Account`, `Logs`,
  `Diagnostics`.

## Dashboard

**Reached:** the first rail item after setup. The header's status word (see
above) is the live connection state; the screen itself holds the action row,
the status card and the notice.

The action row holds:

- **Connect** — or **Disconnect** once a connection exists (shown for
  `Connected`, `Proxy only`, `Connecting`, `Reconnecting` and
  `Waiting for network`; every other state keeps `Connect`). The button is
  held disabled from the click until the action settles.
- **Options** — a small popover with the per-connect choices:
  - **Location** — a `Saved selection` entry plus every country the daemon
    serves, as `Name (CC)` prefixed with the country's flag;
  - **Proxy only** — *Keep system routing untouched; SOCKS5 proxy only.*;
  - **Kill switch** — `On`, `Off` or `Last` (*Last keeps the value currently
    in effect*), seeded from the current setting;
  - its own **Connect** button, which applies the choices to one connect.
- **Refresh** — ask the daemon for a fresh status.

Below the actions is the **status card**, whose eleven rows appear as data
becomes available and stay hidden until then:

`Uptime`, `Location` (`CC` or `CC (server)`), `Exit IP` (`IP` or
`IP (country)`), `Rates`, `Totals`, `Kill switch` (`On`/`Off`), `Autoconnect`
(`On`/`Off`), `Token expiry`, `Pass expiry`, `Quota`, `Quota resets`.

The `Quota` row shows a progress bar of remaining quota when both the
remaining and limit values are known.

A **notice row** appears under the card for the latest daemon message (for
example a connect failure), with a **Retry** button when the failure was a
failed status load on a reachable daemon. When the daemon is unreachable the
header already says so and the card stays empty.

## Locations

**Reached:** the `Locations` rail item. A filter box at the top narrows the
tree live as you type. Countries contain their cities, and cities their
servers; the recommended region is listed first and country rows carry their
country-flag emoji (Recommended, cities and servers do not). The current
selection is marked `· selected`, and each server may show a latency such as
`· 42 ms` or `· timeout`.

The action row has **Refresh**, **Test speed**, **Ping all** and
**Expand all**/**Collapse all**. Test speed probes the visible servers (up to
16 hosts) and fills in the latency figures. Ping all probes every usable
server in the loaded list — countries, cities and Recommended, deduped by
address and skipping quarantined entries — regardless of the filter, and its
status line reads `Pinging N servers...`. Expand all opens every country and
city; the label flips to Collapse all while anything is open, and the control
is disabled while the filter is non-empty. A status line below reports the
last action's result.

## Settings

**Reached:** the `Settings` rail item. The screen has two sections and a
**Refresh** action.

**Kumiho app** — per-user choices that apply immediately:

- **Theme** — `Light`, `Dark` or `OLED`.
- **Accent** — `Blue`, `Teal`, `Green`, `Orange`, `Rose` or `Violet`.
- **Start at login** — `on`/`off`; whether the app opens in the tray when you
  sign in.
- **Version** — the running version, a **Check for updates** button, and the
  result (`Kumiho <v> is available` with an **Open release page** link, or
  `Kumiho is up to date`). The check is manual.

**Background service** — the daemon's settings. Each row applies either
immediately or on the next connect, as its tooltip says. A value you change in
the app is persisted as an override; a row whose value is an override shows an
`overridden` mark and a **Reset** button, and Reset drops the override so the
row returns to the admin `config.toml` (or built-in) default. The rows are:

| Row | Notes |
|---|---|
| Kill switch | on/off; applies immediately |
| Autoconnect | on/off |
| Log level | `debug`, `info`, `warn`, `error`; applies immediately |
| Redact targets | on/off; applies immediately |
| Exit check | on/off; next connect |
| DoH provider | `automatic`, `cloudflare`, `google`, `quad9`, `off`; next connect |
| DNS upstream | resolver IP; empty = provider default; next connect |
| Edge address | pin the Fastly edge IP; empty = resolve normally; next connect |
| Upstream proxy | proxy URL; empty = direct; next connect |
| Exclude CIDRs | comma-separated ranges that bypass the tunnel; next connect |
| Allow LAN | keep the local subnet outside the tunnel; next connect |
| MTU | tun device MTU; next connect |
| SOCKS port | loopback SOCKS5 listener; next connect |
| Quota poll | minutes between quota refreshes while connected; next connect |
| TLS split | `off`, `first-byte`, `sni`; splits the TLS ClientHello across writes within one record; next connect |
| HTTP proxy | on/off; loopback HTTP/CONNECT proxy through the tunnel; next connect |
| HTTP proxy port | loopback HTTP proxy listener; next connect |
| Solve challenges | on/off; answers the Firefox Accounts and edge challenges automatically; applies immediately |
| Keepalive profile | `standard`, `slow`; keepalive ping timeout profile; next connect |

A status line under the rows reports the last change, including `Applying...`
while a change is in flight. Only one change runs at a time.

## Account

**Reached:** the `Account` rail item, which has a **Refresh** action. The card
shows the entitlement:

`Email`, `UID`, `Subscription` (`Yes`/`No`/`Unknown`), `Quota` (for example
`38.0 GiB remaining of 512.0 GiB`), `Bandwidth` (`Limited` or `Unlimited`) and
`Token expiry`.

A tonal **Sign out** button opens a confirmation dialog — *Sign out of
Kumiho?* / *The stored sign-in tokens are cleared and the tunnel disconnects.*
— before it clears the tokens. Signing out does not reopen the wizard: you
stay on the Account screen, its card disappears, and the status line reads
`Not signed in`. The wizard only reappears for a daemon that still reports
setup as incomplete.

## Logs

**Reached:** the `Logs` rail item. The level filter narrows by `debug`,
`info`, `warn` or `error`; **Follow** keeps the newest line in view;
**Refresh** reloads. A search row has a **Search** box plus **Clear**
(clears the console), **Export** and **Open log folder**. The console shows
the daemon's recent log lines; Export writes a snapshot to
`%LOCALAPPDATA%\kumiho\logs\kumiho-logs-<YYYYMMDD-HHMMSS>.log`. The in-app
console keeps the last 2000 lines.

## Diagnostics

**Reached:** the `Diagnostics` rail item. The action row holds **Re-run
checks**, **Copy details**, **Open log folder** and **Cleanup…**. The last
opens a confirmation dialog and then runs the full `kumiho cleanup`; the
daemon also runs a stale-only cleanup of its own networking state when the
service starts.

- The **results list** renders `kumiho doctor`'s checks as chips: `OK`,
  `Warning`, `Failed` or `Skipped`. On the first visit each launch the checks
  run automatically; afterwards use **Re-run checks**.
- The **Kumiho service** card shows the Windows service's state with **Start**
  and **Stop** buttons. These need administrator rights, so Windows shows a
  UAC prompt.
- **Copy details** copies the shown report to the clipboard.

## Tray and lifecycle

The app lives in the notification area (tray). Its tooltip is
`Kumiho — <status word>`, mirroring the header.

Right-clicking the icon opens:

- **Connect** / **Disconnect** — the same rule as the Dashboard button;
- **Location** — a submenu of the daemon's locations (recommended first), or a
  disabled `Location: (none)` when none are known; picking one persists the
  selection;
- **Show Kumiho** — bring the window back;
- **Diagnostics** — open the screen;
- **Quit** — exit the app cleanly.

**Closing the window** (the `X`) does not exit the app: it hides the window
and keeps running in the tray, so the tunnel and the tray stay available. The
first time you close it, a one-time notification says *Kumiho is still running.
Right-click the tray icon to connect or quit.*

**Starting at login** is on by default and launches the app hidden
(`--tray-start`); **Quit** from the tray exits fully. A second launch focuses
the existing window rather than starting a second copy.

The app's own preferences (window size and position, theme, accent, autostart,
the tray hint) live in `%APPDATA%\kumiho\gui.toml`; override the location with
`KUMIHO_GUI_PREFS`. The daemon's own config and state stay under
`%ProgramData%\kumiho` (see [usage.md](usage.md#files)).

## Diagnostics tools

- **`kumiho doctor`** — the same checks the Diagnostics screen runs, from a
  shell (add `--json` for machine-readable output).
- **`kumiho logs -f`** — stream the daemon log over IPC, from any client.
- **The GUI support log** — the shell writes
  `%LOCALAPPDATA%\kumiho\gui.log` (a file over 512 KiB is rotated to
  `gui.log.1` when the shell starts). This holds shell
  errors that never reach the daemon log, such as a refused action or a
  WebView2 failure.
- **The probe tool** — a tiny self-contained diagnostic that proves the
  native host works on a given machine. Build it on the build host with:

      make build-probe

  and run `dist\kumiho-gui-probe-windows-amd64.exe` on the Windows machine.
  It opens a `Kumiho GUI probe` window, renders WebView2 content, completes a
  JS↔Go echo round trip, puts an icon with a **Quit** menu in the tray, and
  reports whether it reached the daemon's control pipe. It needs no daemon and
  changes nothing.

## Uninstalling

    .\install.ps1 -Uninstall          # keeps %ProgramData%\kumiho (tokens/state)
    .\install.ps1 -Uninstall -Purge   # also removes state and the local group

Uninstall stops and deletes the `kumiho` service, removes
`%ProgramFiles%\Kumiho` (the CLI, the GUI, `wintun.dll` and the installer
copies) and its `PATH` entry, removes the `Kumiho` Start Menu shortcut and the
`Kumiho GUI` login entry, and revokes your pipe access. Per-user preferences
in `%APPDATA%\kumiho\gui.toml` (and the TUI's `tui.toml`) are left alone;
delete them by hand for a clean slate. `-Purge` additionally removes
`%ProgramData%\kumiho` and the `kumiho` local group.

## Troubleshooting

### The app will not start

- A dialog saying **Microsoft Edge WebView2 Runtime is not installed** means
  the runtime is missing. Choose **Yes** in the dialog to open Microsoft's
  download page, install the Evergreen Runtime, and start the app again.
- If the dialog instead says **Kumiho could not create its window**, the same
  download page is offered; installing or repairing the WebView2 Runtime
  usually fixes it.

### The header says `Daemon unreachable`

The app is running but cannot reach the service. Check `Get-Service kumiho`,
then **Start** it (or use the Diagnostics screen's Service card). Your account
must be allowed on the control pipe — the installer grants it; a second
account may need adding to the `kumiho` group. Only one daemon can own the TUN
device, so stop the service before running a foreground `kumiho daemon`. See
[troubleshooting.md](troubleshooting.md#the-daemon-is-unreachable).

### The window closed but the icon is still there

That is expected: closing the window hides it to the tray. Use **Show Kumiho**
to bring it back, or **Quit** to exit. The first close shows a one-time hint.

### A second launch does nothing

The app is single-instance per user; the second launch focuses the running
window. If no window appears, the first copy may be mid-start — try again, or
**Quit** from the tray and relaunch.

### UAC is declined on Start/Stop or Cleanup

The Diagnostics service controls and `Cleanup…` need administrator rights. If
the UAC prompt is declined, the action is cancelled and an error appears on
the screen's status line; no partial change is made.

### A screen is empty and the status line shows an error

The app could not load that screen's data. Press **Refresh**; if it persists,
run `kumiho doctor`, then check the daemon log at
`%ProgramData%\kumiho\logs\kumiho.log` and the shell log at
`%LOCALAPPDATA%\kumiho\gui.log`.

## Related documentation

- [install.md](install.md) — installing, upgrading and removing Kumiho.
- [usage.md](usage.md) — the full command surface and file locations.
- [cli-tui-abilities.md](cli-tui-abilities.md) — the command/ability matrix.
- [troubleshooting.md](troubleshooting.md) — common problems and log locations.
- [windows-testing.md](windows-testing.md) — the manual acceptance checklist
  for a Windows build.
