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
> v0.13.0. The older walk-era inventory in
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

## Language and RTL

The app ships in English and Persian. The language is an engine setting
(`language` = `system`, `en` or `fa`, default `system`); every client of a
given daemon reads the same key, so the GUI and the CLI agree on what is
configured even though only the GUI renders Persian.

- **First run.** The welcome/consent step starts with the inline language
  control: **System**, **English** or **فارسی**. Switching takes effect
  immediately and keeps the current step and consent tick. The wizard still
  has five steps.
- **Settings.** The first row of **Settings → Kumiho app**, above Theme, is
  **Language**. The same control appears in the setup wizard; there is no
  separate language dialog.
- **System** resolves to Persian only when the Windows user-interface
  language is Persian (LANGID `0x29`). Every other UI language resolves to
  English. When **System** is selected, the row shows the language it
  currently resolves to, so it is never ambiguous.
- **First paint.** The effective language is cached in `gui.toml`. The shell
  uses that cache for the first paint instead of flashing English, then
  reconciles with the engine's `language` setting when the controller state
  arrives. An explicit `?lang=fa` or `?lang=en` query parameter is a
  capture/dev override: it forces the catalog for that page load and skips
  the controller reconciliation.
- **Immediate switching.** Changing the language does not reconnect the
  tunnel, reload the app or reset setup. The same screen is rendered again in
  the new catalog, including the tray menu, status words and tooltips.
- **Full RTL.** Persian lays the shell out right-to-left: the rail moves to
  the right, navigation order mirrors, directional icons and progress bars
  mirror, and the onboarding bullets sit on the right. Flags, the Kumiho
  logo, the power glyph and platform logos stay unmirrored.
- **Typography.** Persian uses the bundled Vazirmatn 33.003 family (regular,
  medium, semibold and bold WOFF2), with more body line height for Persian
  shaping. English keeps the system UI font; no font is fetched from the
  network.
- **Mixed text.** Persian digits and Jalali dates are used for user-facing
  values. IP addresses, ports, versions, hostnames, emails, logs, diagnostic
  facts and engine messages stay Latin and are isolated from the surrounding
  Persian so bidi ordering cannot scramble them.
- **Errors.** The visible summary and action are Persian. Known error codes
  map to a Persian message; unknown codes use the generic Persian error.
  The raw English detail stays available in an expandable, left-to-right
  block. The tray notification text follows the effective language.

## Installing

Download `Kumiho-Windows-amd64.zip`, extract it, and run the
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

1. **Welcome to Kumiho** — the consent gate. The inline **System / English /
   فارسی** control is at the top of this step; changing it keeps the step and
   the tick. Read the two panels (*What Kumiho does* and *What to keep in
   mind*), then tick **I understand** to unlock **Continue**. Continue first
   probes Mozilla's control-plane hosts;
   if this network answers HTTP 451, the reusable **Blocked region** screen
   opens before sign-in. The tick itself is not persisted: if you quit before
   finishing, the wizard starts at this step again next launch.
2. **Sign in** — sign in with the Firefox account that holds the VPN. The
   first page offers **Continue in browser** (the default) or **Use
   password**, and remembers the last successful method. Browser sign-in opens
   Mozilla's Firefox Accounts page for captcha, password, TOTP or unblock
   codes. Mozilla finishes on its HTTPS success page; copy the complete URL
   from the address bar, paste it into the waiting card's **Redirect URL**
   field and choose **Continue**. The card also offers **Open again** and
   **Cancel** and checks
   every second. There is no loopback listener, so the browser may run on
   another machine. Password sign-in keeps the
   in-app email → password → 2FA/email-link pages. Kumiho stores the resulting
   tokens on this PC and never your password. If tokens already exist, the
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

The window has a header and a navigation rail; one screen is shown at a time.
The rail is on the left in English and on the right in Persian, and the list
order mirrors with it.

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
- **Rail.** `Dashboard`, `Locations`, `Settings`, `Logs`, `Diagnostics`.

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
  - **Location** — a native select with a `Saved selection` entry plus every
    country the daemon serves as text-only `Name (CC)` options. A real 4:3
    country flag sits beside the select and follows the chosen country; Saved
    selection shows the globe fallback. Option text never contains the flag
    (D39);
  - **Proxy only** — *Keep system routing untouched; SOCKS5 proxy only.*;
  - **Kill switch** — `On`, `Off` or `Last` (*Last keeps the value currently
    in effect*), seeded from the current setting;
  - its own **Connect** button, which applies the choices to one connect.
- **Refresh** — ask the daemon for a fresh status.

Below the actions is the **status card**, whose nine rows appear as data
becomes available and stay hidden until then:

`Uptime`, `Location` (`CC` or `CC (server)`), `Exit IP` (`IP` or
`IP (country)`), `Rates`, `Totals`, `Kill switch` (`On`/`Off`), `Autoconnect`
(`On`/`Off`), `Token expiry`, `Pass expiry`.

A **notice row** appears under the card for the latest daemon message (for
example a connect failure), with a **Retry** button when the failure was a
failed status load on a reachable daemon. When the daemon is unreachable the
header already says so and the card stays empty.

## Locations

**Reached:** the `Locations` rail item. The screen has a pinned **Fastest** row
and a **Locations** section above the full flat server-only list; there are no
country or city header rows, carets or expand/collapse controls. The full
list is ordered Recommended first, then country A–Z, city A–Z and hostname
A–Z. Each row shows a real 4:3 SVG country flag (a star for Recommended, a
globe for an unknown code), the full `Country · City` on the first line and
`hostname:port · protocols` on the second. The latency or the
current-selection check trails the row. Locked rows carry a text **Locked**
badge. Quarantined and targetless servers are never listed.

**Fastest** is one pinned row. It is disabled with **Run Ping all to enable
Fastest** until a fresh finite cached RTT exists, then shows the minimum RTT;
selecting it resolves once to that actual server and persists it.

The action row is **Search** (a magnifying-glass icon), **Refresh**, **Ping
all** and **Show/Hide locked locations**. The icon expands the search field
below the row and focuses it; the field stays open for the visit. Search
narrows the full list live as you type. Its placeholder is **Country, city,
server, or protocol**, and it matches country name/code, city name/code,
hostname, `host:port` and protocol names, case-insensitively. A no-match result
keeps the list area's `No locations match the filter.` copy and the status
line's `No locations match the filter`. The pinned Fastest row is not filtered.

Ping all opens a temporary real-edge session per visible server,
regardless of the filter, deduped by address and skipping quarantined,
targetless and hidden-locked entries; its status line reads `Pinging N
servers...` and the button stays disabled until the daemon's `ping_done`
arrives. Per-row labels update as results stream in and the daemon caches them
for 30 minutes. Only a completed run with at least one result switches the
**full list** to speed order: finite RTTs ascending, failures and untested
rows last, and Recommended always first. Fastest keeps its pinned order. A
timeout counts as a result; a transport failure leaves the current order
alone. A new run keeps the current order while it is in flight. Refresh
restores country order; a changed list is also a new visit and its cached
fresh RTTs still label rows. A status line below reports the last action's
result.

The locked action flips `allow_locked` and reloads the list. When it is off,
locked servers are hidden from the list and from Ping all; when on, they show
the badge. Clicking a server row saves it for the **next** connection (status
`Location saved`) without connecting; the current session is unchanged.

## Settings

**Reached:** the `Settings` rail item. The screen opens with the **Account**
section, then the two panes — **Kumiho app** and **Background service**, the
latter ending in a **Region** group — and a **Refresh** action. Each section is
a card with a distinct header band and a primary-tinted outline icon: person
for Account, sliders for Kumiho app, a server for Background service and a
globe for Region.

**Account** — the Firefox account management that used to be its own page.
It has **Add account** and a session-wide **Sign out** action, the account
list, the browser waiting card and the sign-out dialog. The page-level
**Refresh** reloads the account projection too.

The list shows up to five accounts: label/email, method (`browser` or
`password`), active marker and a re-authentication warning. The row overflow
menu offers **Use**, **Rename**, **Re-authenticate** (browser) and **Remove**.
**Add account** opens a dialog with **Continue in browser** (the default) or
**Use password**; the password form is the onboarding credential flow inline.
A browser flow shows the waiting card (engine URL, **Redirect URL**,
**Continue**, **Open again** and **Cancel**), opens the URL once automatically,
polls `accounts_add_status` every second and finishes when the pasted redirect
URL is accepted. Removing one account leaves the others signed in; removing
the active one selects the most recently used remaining account. After an
upgrade from v0.6.0, accounts from the old opaque browser flow are removed and
the status line shows **“Old browser sign-in removed. Add your Firefox account
again.”**

The tonal **Sign out** button opens a confirmation dialog — *Sign out of
Kumiho?* / *The stored sign-in tokens are cleared and the tunnel disconnects.*
/ *Kumiho will also try to revoke its Mozilla sign-in; local sign-out
completes offline.* — before it clears **every** account and disconnects. The
revocation is best effort: an offline or failed request never blocks the local
sign-out. Signing out does not reopen the wizard: you stay in Settings, the
account rows disappear, and the status line reads `Not signed in`. The
wizard only reappears for a daemon that still reports setup as incomplete.

The entitlement card (email, UID, subscription, quota, bandwidth and token
expiry) and the quota-rotation card were removed once Firefox dropped the
50 GB limit; the daemon still exposes those fields for the CLI and TUI.

**Kumiho app** — per-user choices that apply immediately:

- **Language** — `System`, `English` or `فارسی`; the first row in this group,
  above Theme. **System** shows the language it resolves to (see
  [Language and RTL](#language-and-rtl)).
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
| TLS split | `off`, `first-byte`, `sni`; splits the TLS ClientHello across writes within one record; next connect |
| HTTP proxy | on/off; loopback HTTP/CONNECT proxy through the tunnel; next connect |
| HTTP proxy port | loopback HTTP proxy listener; next connect |
| Solve challenges | on/off; answers the Firefox Accounts and edge challenges automatically; applies immediately |
| Keepalive profile | `standard`, `slow`; keepalive ping timeout profile; next connect |
| Allow locked locations | on/off; reveals the server-list records marked locked; applies immediately |
| Blocked region | opens the reusable guide: another VPN or the relay, then Prepare and Connect |
| Control-plane relay | HTTPS origin used only after HTTP 451; empty = none; applies immediately |
| Relay token | write-only shared secret; **Set** opens a masked field, **Clear** deletes the stored `relay.token`; never displays its value |
| Test relay | one-shot probe; shows `Relay OK — N ms`, `Relay rejected the token`, `relay_url is not set` or `Relay unreachable: <detail>`; never writes a setting |

The Region block's help text says the relay is used only after HTTP 451 and
does not proxy the browser sign-in page. A relay URL must be an HTTPS origin
with no path, query, fragment or userinfo. The token file is `relay.token`
beside `tokens.json` (mode 0600) and the GUI only ever receives
`relay_token_set`.

**Blocked region** opens the guide described in [usage.md](usage.md#blocked-regions-and-the-first-run):
the six host results, Option 1 (another VPN) first, Option 2 (relay) second,
a **Prepare** action, and a live pass countdown with **Connect**. The guide is
also reached from the dashboard notice after a region error and from the
setup consent preflight. It is a route (`#/blocked-region`) outside the
numbered wizard, so the wizard still has five steps.

A status line under the rows reports the last change, including `Applying...`
while a change is in flight. Only one change runs at a time.

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

The app's own preferences (window size and position, theme, accent, the last
effective UI language, autostart and the tray hint) live in
`%APPDATA%\kumiho\gui.toml`; override the location with `KUMIHO_GUI_PREFS`.
The daemon's own config and state stay under `%ProgramData%\kumiho` (see
[usage.md](usage.md#files)). The `gui.toml` language is only a first-paint
cache; the engine setting is authoritative and every settings load rewrites
the cache.

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
