# Using Kumiho on Android

Kumiho's Android app is a native client for the same Firefox-account VPN
entitlement as the desktop daemon. The Go engine is embedded in the app
process through a gomobile library; there is no daemon, no IPC socket and no
separate service to install. A `VpnService` owns the tunnel and its
foreground notification, and the UI is Jetpack Compose over that service.

This guide describes the app as implemented for **v0.5.1**
(`versionCode` 501) plus the unreleased **0.5.2** Android change noted under
Settings. Read the honesty note below before relying on it.

> **Status.** The app and its unit/instrumented test suites are implemented and
> green. Physical first-connect/DNS browsing/exit-IP acceptance passed on a
> Galaxy S25 FE; the remaining checklist items were explicitly accepted by the
> user as exceptions, so this is a **partial acceptance**, not a full pass. The
> production-signed
> [v0.5.1 release](https://github.com/benyamin-git/kumiho/releases/tag/v0.5.1)
> is published with the three Android APKs and `SHA256SUMS`; the signing key and
> GitHub Actions secret values remain user-owned. Local builds are test builds,
> not published releases.

## Requirements

- **Android 8.0 (API 26) or newer**, on `arm64-v8a` or `armeabi-v7a`
  (`arm64-v8a` is the normal choice on a modern phone). The app is not built
  for `x86`/`x86_64` devices; the x86_64 AAR exists only for the emulator.
- A **Firefox Account with the built-in VPN entitlement** — the same account
  and entitlement the desktop clients and FoxyVPN use.
- A working internet connection for sign-in and for the tunnel. The control
  plane reaches Firefox Accounts, Guardian and Mozilla's server list
  directly.
- **Sideloading**: Kumiho is distributed as an APK, not through Google Play
  or F-Droid. Android asks you to allow installs from your file manager or
  `adb` the first time.
- **16 KB page-size devices** are supported by the pinned NDK and the AAR
  build, which checks the arm64 shared object's `p_align`; the release
  verifier also checks `zipalign -P 16` on every APK. A physical 16 KB device
  run remains an unticked acceptance item.

The app is English-only, uses logical start/end layout, and needs no Google
Play services.

## Installing and updating

### From a local build (current path)

For a local test build, build the app from a checkout:

    bash scripts/android-setup.sh
    make android-debug
    adb install -r android/app/build/outputs/apk/debug/app-debug.apk

The debug build installs side by side with a release build: its package is
`dev.benyamin.kumiho.debug` (version name suffixed `-debug`), while a release
build is `dev.benyamin.kumiho`. The full toolchain, emulator and release
instructions are in [android-testing.md](android-testing.md).

### From a release

The [v0.5.1 release](https://github.com/benyamin-git/kumiho/releases/tag/v0.5.1)
is published. Each GitHub release carries three APKs and the usual
`SHA256SUMS`:

| Asset | ABI | Use |
|---|---|---|
| `kumiho-v<version>-android-arm64-v8a.apk` | arm64-v8a | Most phones |
| `kumiho-v<version>-android-armeabi-v7a.apk` | armeabi-v7a | Older 32-bit phones |
| `kumiho-v<version>-android-universal.apk` | both | One file if unsure |

Those names are produced by `scripts/android-release.sh` and attached by
`.github/workflows/release.yml`. Download from the project's GitHub Releases
and check the APK against the `SHA256SUMS` published with it. Never install an
APK from an untrusted mirror.

### Updating

There is **no auto-update and no in-app APK installer**. Settings → Software
updates → **Check for updates** makes one request to GitHub's latest stable
release when you tap it, compares versions and offers **Open release page**.
It downloads nothing.

To update manually, install the newer APK over the old one with the same
signing key; app data (sign-in, settings, split selection) is kept. Android
refuses an update signed with a different key — uninstalling first erases the
app's private state. A debug build cannot replace a release build (different
package), and vice versa.

## First run: the four-step setup

The setup flow runs before the shell and has four numbered steps. The bottom
action is fixed, and the consent tick on step 1 shares that surface with the
disabled **Continue** so it is always visible.

1. **Consent.** Read what Kumiho routes (the traffic of apps that use the
   tunnel goes to a Kumiho proxy server), what stays on the phone (tokens,
   settings, server list and logs in private storage; no telemetry) and the
   local-proxy warning. Tick *I understand what Kumiho routes…* to enable
   **Continue**. The tick is not persisted; quitting before finishing starts
   the wizard here again.
2. **Sign in.** Enter the Firefox Account email, then the password, then a
   verification code if Firefox Accounts asks for one. If Firefox Accounts
   sends a confirmation link instead, the app waits and re-checks every three
   seconds while the step is open; **Re-check** polls immediately. This is a
   native sign-in with no browser hand-off — which also means an interactive
   captcha cannot be completed in the app (see [Limitations](#limitations)).
   Errors appear inline and the form stays usable.
3. **Permissions.** **Allow VPN** opens Android's VPN consent dialog;
   **Allow notifications** requests `POST_NOTIFICATIONS` on Android 13+ and
   otherwise shows the current state. Both are optional here: denying either
   still lets you finish, and Kumiho asks again when you connect. The cards
   show the real OS state, not the result code of the dialog.
4. **Always-on.** Kumiho explains Android's Always-on VPN and lockdown, shows
   their real state and links to **Open Android VPN settings**. It never
   changes either flag. **Finish and connect** is offered only when you are
   signed in and the VPN permission is granted; otherwise **Finish without
   connecting** saves the setup and opens the dashboard.

Finish saves first: if writing the setup state fails, the app stays on step 4
with an inline error and does not connect.

## The shell

Five tabs — **Home**, **Locations**, **Account**, **Settings** and **More** —
with a bottom bar on a phone and a navigation rail from 600 dp wide. **More**
is a sheet with **Logs** and **Diagnostics**, which open as pages with a back
arrow. Light, Dark and OLED themes and six accents are in Settings; the first
run follows the system's light/dark preference, and OLED is opt-in.

## Dashboard

The power control is the primary action: tap to connect or disconnect. The
state word under it is the service's own state:

- *Off*, *Connected*, *Proxy only*, *Connecting*, *Reconnecting*, *Waiting for
  network*, *Sign in required*, *Connection error*, *Service unavailable*,
  *VPN permission needed*, and *Starting…* while the app waits for the bound
  service. A proxy-only session also carries a **No VPN tunnel** badge.

While connected the hero shows the real location and server and the session
uptime. Four cards appear only when their data exists:

- **Location** — the country flag, country and city when the engine's server
  is known, with the `host:port` beneath it;
- **Exit IP** — the public address the engine observed;
- **Quota** — remaining and limit, with the reset date when reported;
- **Session** — uptime and up/down totals.

**Details** shows Always-on, lockdown, autoconnect, a shortcut to Android's
VPN settings, token and subscription expiry, the in-tunnel DNS address, and
IPv6 as **Blackholed**. The autoconnect row says *Follows Android's Always-on
VPN*: the engine's desktop `autoconnect` setting is not used on Android.

A connection command is one-way: the UI follows the service and never invents
a connection. The app allows one session at a time, so while a session is
active the options sheet says **Disconnect first to change location** and
Connect is disabled. A failed connect is reported with the engine's own words
and a Retry or Dismiss action; a transient status-refresh failure never
replaces a live tunnel with an error.

### Connect options

The options sheet (from the power control's entry point) has:

- a **compact location picker** — Recommended, the saved location if there is
  one, and flagged countries expanding to cities;
- **Proxy only** — start the local proxies without the VPN interface;
- a **Lockdown** row that explains it is managed in Android VPN settings and
  opens them;
- **Connect**, with an honest disabled reason when a session exists, a
  command is in flight, you are signed out, or the service is unavailable.

Recommended maps to the engine's `rec` selection; Saved sends no location so
the engine keeps its persisted choice; a picked country/city lets the engine
resolve and persist the first candidate. The sheet never calls
`select_location`. If Android's VPN consent is missing, Connect opens the
system dialog; a denial closes the sheet and shows the reason.

### Locations

**Locations** loads the full country/server list, with the engine's
recommended entry first. Country rows carry their country-flag emoji, and so
do the connect-sheet country picker and the current-selection summary;
Recommended and servers stay unprefixed. A country row expands directly to
its servers — there is no city level — and its subtitle counts them
(`N servers`, quarantined included). Server rows name the city as context:
`City · protocols · port`, with the per-server latency trailing. Search
matches country name/code, city name and server hostname, case-insensitively;
matching branches open while the query is active and closing the search
restores the previous expansion. Refresh keeps the previous tree on failure
and offers one Retry.

The toolbar is one row: Refresh, **Ping all** and
**Expand all**/**Collapse all**. Ping all pings every usable server in the
loaded list regardless of the filter or expansion, deduped by `host:port`
and skipping quarantined entries; its button reads **Pinging N…**, and the
button stays disabled until the run ends. A measured round trip shows
milliseconds, an unanswered probe shows **timeout**, and an untested host
shows **—**; nothing pings on load, typing, expanding, refreshing or
resuming. The run window is 12 seconds for a normal list and scales with
the target count (1.5 seconds per 24-host batch plus margin). Expand all
opens every country, and the label flips to Collapse all while anything is
open; the control is disabled while the search field is non-empty.

Collapsed country rows show their best latency — the fastest measured RTT
among their servers, or **timeout** when only timeouts came back, or **—**
before any run — and the list re-sorts live as results arrive: measured
countries first, ascending, then the rest in engine order, with
Recommended pinned first; servers inside a country the same way.

Selecting a row saves it for the **next** connection — the current session
keeps its server and exit IP — so the screen says to disconnect on Home and
connect again. A server the engine quarantined (three failed connects) is
marked, skipped by Ping all and not selectable.

## Settings

Settings has four groups. The catalog is the desktop engine's settings, one
row per key, plus two Android-owned read-only rows; there are **19 catalog
rows** (17 engine settings and the 2 Android-owned Always-on/lockdown rows).
The Kumiho app group also has the Split tunneling shortcut and the app-owned
Keep connections outside the tunnel setting.

### Kumiho app

- **Theme** — Light, Dark or OLED; applies immediately.
- **Accent** — Blue (default), Teal, Green, Orange, Rose, Violet.
- **Version** — the app version and the embedded engine version.
- **Software updates** — **Check for updates** only on tap, as described
  above.
- **Notifications** and **Battery optimization** — the real OS state, with
  **Open notification settings** and **Open battery settings**. Both re-read
  on resume, and a system page that cannot be opened shows an inline error.
- **Always-on VPN** — the OS state and **Open Android VPN settings**.
- **Split tunneling** — the next section's entry point, with its Off /
  Excluding N apps / Including N apps summary. It stays reachable even when
  engine settings fail to load.
- **Keep connections outside the tunnel** — off by default. When on, Android
  treats the VPN as bypassable, so LAN and other route-excluded connections
  (for example an SSH session to a local host) stay open when Kumiho connects
  or disconnects. Normal traffic still uses the tunnel. Android lockdown
  overrides it, and it applies on the next connect.

### Connection (7 engine rows)

| Row | Values | Default | Applies |
|---|---|---|---|
| `mtu` | 576–65535 | 8500 | next connect |
| `socks_port` | 1–65535 | 1080 | next connect |
| `exit_check` | on/off | on | next connect |
| `doh_provider` | automatic, cloudflare, google, quad9, off | automatic | next connect |
| `dns_upstream` | resolver IP | empty (provider default) | next connect |
| `allow_lan` | on/off | on | next connect |
| `quota_poll_minutes` | 1–1440 | 15 | next connect |

`allow_lan` bypasses the tunnel for `10.0.0.0/8`, `172.16.0.0/12`,
`192.168.0.0/16`, `169.254.0.0/16` and `224.0.0.0/4`. The tunnel DNS address
`10.8.0.1` always stays inside the tunnel, even when one of those ranges or a
user exclusion would otherwise cover it.

### Android VPN (2 read-only rows)

| Row | What it shows | Why it is read-only |
|---|---|---|
| `kill_switch` | Android's lockdown: Lockdown / Always-on only / Off | Android owns the kill switch; the engine does not enforce one here |
| `autoconnect` | On (Always-on) / Off | Android starts the VPN through Always-on; the engine's desktop `autoconnect` is not used |

Both rows reflect the **service's** view of the OS flags, never the engine's
snapshot, and both open Android's VPN settings. There is deliberately no fake
in-app kill-switch or autoconnect switch; the engine's `kill_switch` and
`autoconnect` values are never written from the app.

On Android 8.0–9 the two flags are not exposed to apps, so these rows read
**Off** even if the system setting is on. Android 10 (API 29) and newer report
the real state.

### Advanced (10 engine rows, collapsed by default)

| Row | Values | Default | Applies |
|---|---|---|---|
| `edge_address` | host or IP | empty (resolve normally) | next connect |
| `upstream_proxy` | `socks5://`, `socks5h://`, `http://` URL | empty (direct) | next connect |
| `exclude_cidrs` | comma-separated IPv4 CIDRs | empty | next connect |
| `tls_split` | off, first-byte, sni | off | next connect |
| `http_proxy` | on/off | off | next connect |
| `http_proxy_port` | 1–65535 | 8080 | next connect |
| `solve_challenges` | on/off | on | immediately |
| `keepalive_profile` | standard, slow | standard | next connect |
| `log_level` | debug, info, warn, error | info | immediately |
| `redact_targets` | on/off | on | immediately |

Every row opens a modal editor: a radio list for enums, an outlined field for
text and numbers, and a multi-line field for `exclude_cidrs`. Client
validation mirrors the engine ranges (MTU, SOCKS/HTTP ports, quota), enum
membership and IPv4-only CIDRs, so a rejected value stays in the editor with
the reason instead of reaching the engine. A row changed away from
`config.toml` shows an **Overridden** chip and a **Reset** action; engine
errors render under the row. Only one change is in flight at a time — every
row is disabled and the active row reads **Applying…** until a fresh settings
snapshot arrives. A successful set triggers a reload; a failed set keeps the
old values and shows the engine's words; a failed reload after a successful
set is a dismissible notice with Retry.

## Split tunneling

**Settings → Split tunneling** chooses which apps use the tunnel:

- **Off** (default) — every app uses the tunnel.
- **Exclude apps** — the selected apps bypass the tunnel; everything else
  uses it.
- **Include apps** — only the selected apps use the tunnel; everything else
  bypasses it.

There is no Apply button: the mode and the whole package selection are saved
immediately and **apply on the next connect**. While a full tunnel is up, the
screen shows *The tunnel is up. Reconnect to apply this selection.* (proxy-only
and idle sessions do not show it). The selection is a Kumiho app preference,
not an engine setting, so it is stored even when the engine is unavailable.

**Kumiho always stays outside its own tunnel** in every mode. Off and Exclude
show a locked **Always outside** row; Include omits Kumiho from the list. An
empty Include selection would tunnel every app, so it shows a warning and
falls back to the same "Kumiho outside, everything else through the tunnel"
behaviour rather than pretending nothing is tunneled. Apps you uninstall are
removed from the saved selection; a failed package query keeps the stored
value.

The picker lists launchable apps by default, with a **System apps** switch for
system and non-launchable packages and a case-insensitive search over labels
and package names (system apps and packages are shown only when you ask for
them). Selecting is disabled in Off mode with an explanation.

### Route exclusions

`allow_lan` and `exclude_cidrs` (Advanced) are validated twice — in the app
and again by the Go route computation at connect:

- IPv4 only. IPv6 is **captured and blackholed**, never excluded and never
  tunneled. A bare address becomes `/32`; host bits are masked
  (`10.1.2.3/8` becomes `10.0.0.0/8`); malformed values and leading-zero
  octets are rejected.
- `0.0.0.0/0` and combinations that cover the whole IPv4 internet (such as
  `0.0.0.0/1` plus `128.0.0.0/1`) are rejected, because excluding them would
  blackhole the device.
- The computed lists are capped at 256 prefixes, which bounds the
  `VpnService.Builder` work.
- The tunnel DNS host `10.8.0.1` is subtracted from the exclusions.
  It stays routed to the in-tunnel resolver even when `allow_lan` or a user
  CIDR covers its range, so DNS cannot be blackholed by a legitimate
  exclusion.

On **Android 13 (API 33) and newer**, the interface is built with `0.0.0.0/0`
and each exclusion is passed to `Builder.excludeRoute(IpPrefix)`. On
**Android 8.0–12L (API 26–32)** the app instead adds the computed complement
routes, because `excludeRoute` does not exist there; the complement is
computed by the engine, not by Kotlin, and the UI still shows the same
exclusions. Both paths always add `::/0` so IPv6 is captured and dropped.
The API 26–32 complement path is covered by unit tests and an emulator/phone
acceptance step, not yet by a physical device.

Per-app rules and route exclusions are established when the interface is
created; changing either while connected takes effect on the next connect. The
OS may apply its own policy under lockdown; see the next section.

### Keeping connections open across a transition

Android closes every live TCP socket belonging to a VPN-routed app when the
VPN's UID ranges change, unless the VPN is marked bypassable. `allow_lan`
and `exclude_cidrs` change routing only; they do not stop that per-UID
socket teardown. That is why an SSH session to a LAN host could drop when
Kumiho connected or disconnected even though its packets never entered the
tunnel.

**Keep connections outside the tunnel** (Kumiho app, off by default) asks
Android to treat the VPN as bypassable. With it on, a LAN or other
route-excluded connection keeps the same local and remote addresses across a
connect or disconnect and can stay open. Normal traffic still uses the
tunnel; the trade-off is that an app can deliberately bind a socket or
process to another network and leave the VPN. Android lockdown overrides the
setting and still forces bypass off, and the row stays visible and editable
in that state.

The value is saved immediately and applies on the **next** connect. On a
cold start that connects before the saved choice has loaded, that first
interface uses Off (the safe default); the reconnect line appears when the
choice loads, and reconnecting applies it. While a full tunnel is up, changing
it shows *The tunnel is up. Reconnect to apply this setting.*; Kumiho never
reconnects on its own. A proxy-only session builds no interface, so the
setting has no effect there and shows no warning.

Connections carried **through** the tunnel still drop on a connect or
disconnect because their exit IP changes; TCP cannot migrate the stream.
Only LAN and other route-excluded connections survive. The setting is
independent of split tunneling and `allow_lan`.

## Always-on VPN and lockdown

Both are **Android settings**, not Kumiho settings. Kumiho reads the state,
shows it, and links to **Open Android VPN settings**; it never turns either on
or off, and there is no engine-side kill switch or autoconnect on Android.

- **Always-on VPN** lets Android start Kumiho's VPN whenever no VPN is
  connected and after a reboot. Kumiho starts its foreground service, then:
  if you are signed in it connects; if you are signed out it posts a sign-in
  notification instead of fighting lockdown; from a fresh boot it may
  auto-connect a previous fatal state once. A restart while a session is
  already active does nothing. Because Android owns the trigger, the app can
  only reflect the result — an OEM that delays or refuses the start is a real
  limitation, not something the app can override.
- **Lockdown** is the real kill switch. While Kumiho is not connected, Android
  blocks all traffic, and Kumiho never works around it. Disconnecting is still
  allowed, but the app first warns: Android may restart the tunnel on its own,
  and until it connects again apps on the phone have no network access. The
  dialog offers **Disconnect** and **Open VPN settings**; **Keep connected**
  cancels.
- Under lockdown, a disabled session, a failed connect or a sign-out leaves
  the phone without network access. That is the OS doing what lockdown says,
  not a Kumiho error.
- The dashboard and notification reflect OS restarts; a service restart does
  not create a second tunnel. Revoking the VPN permission (`onRevoke`) ends
  the session with the engine's `revoke`, removes the foreground notification
  and stops the service.
- Split tunneling and lockdown are independent OS inputs. Kumiho applies the
  per-app rules when it establishes the interface and shows the mode it
  actually sent; Android may still ignore per-app filters or restart the
  service under lockdown. This combination is on the physical checklist.
- Lockdown forces the VPN to be non-bypassable. **Keep connections outside
  the tunnel** stays stored and editable, but Android still closes
  connections when the VPN changes while lockdown is on.

## Proxy-only and local proxies

**Proxy only** in the connect options starts the local SOCKS5 (and, if
enabled, HTTP) proxy **without** creating a VPN interface. It needs a signed-in
account but **no VPN permission**, because it never calls `establish()`. The
service stays in the foreground with its notification, the dashboard shows
**Proxy only** with the **No VPN tunnel** badge, and the Details row still says
IPv6 is blackholed — but there is no tunnel to blackhole IPv6 in: apps that
do not use the proxy are simply outside it.

The local listeners are loopback-only:

- **SOCKS5** on `127.0.0.1:<socks_port>` — default `1080`, configurable in
  Connection. It accepts TCP and a DNS-only `UDP ASSOCIATE` relay.
- **HTTP/CONNECT** on `127.0.0.1:<http_proxy_port>` when `http_proxy` is on —
  default `8080`, configurable in Advanced.

They have **no authentication** and are **device-wide**: any app on this phone
can use them while the tunnel is up. Other devices on your network cannot
reach them. Point a client at them explicitly, for example:

    curl -x http://127.0.0.1:8080 https://example.com

Proxy-only has no per-app split and no Always-on interface: those controls
concern the VPN interface, which does not exist in this mode. Non-DNS UDP
(QUIC/HTTP3, VoIP, games) is still blocked on purpose; DNS over the SOCKS5
proxy uses the same in-tunnel DoH chain through `UDP ASSOCIATE`. The local
proxies start and stop with the session.

## Account

**Account** shows the live Guardian entitlement: email, UID, subscription,
quota remaining and limit, bandwidth (limited or unlimited) and token expiry.
**Refresh** re-reads it with a 35-second timeout; on failure the last known
fields stay visible with a notice. **Sign out** asks for confirmation, clears
the tokens, disconnects the tunnel and leaves you on Account in the signed-out
state, where the same native sign-in pane lets you sign back in. Sign-out
never touches your Firefox Account itself.

## Logs

**More → Logs** shows the engine's log: a snapshot when the screen opens plus
live lines while it is visible, capped at the newest **2000 entries**. Filter
by **All / Debug / Info / Warn / Error**, search with the field, toggle
**Follow**, **Clear** the engine ring, and **Share** the current buffer. Each
line can be copied.

**Share** writes `kumiho-logs-<timestamp>.log` to the app's
`cacheDir/exports/` and opens the Android share sheet with a `FileProvider`
URI. Nothing is uploaded: the file leaves the phone only through the app you
pick. Clear empties the engine's ring; the console also hides the lines that
were present at the moment you cleared, so it does not repaint them from its
own mirror.

## Diagnostics

**More → Diagnostics** collects Android facts and runs the Go `doctor` report
once per visit (no polling), caching the result until you press **Re-run**:

- a report header with the app/engine version and the device;
- a **VPN service** card: service running, engine state, Always-on, lockdown,
  notifications, battery optimization, the validated network, the VPN link's
  DNS servers, Private DNS mode, the loopback SOCKS/HTTP ports, and the
  doctor's clock-skew and endpoint checks;
- a **split tunneling** card with the active mode and selected app count, and
  the note that changes apply on the next connect;
- status chips — **OK / Warn / Fail / Skip** — with detail and a **Fix:** line
  from the doctor where one exists, plus **Copy all** for the plain-text
  report;
- shortcuts to Android's VPN, notification and battery settings;
- **Reset local state…**, which clears the cached server list, the log ring
  and the failure counters. It **never** signs you out and never touches your
  Firefox Account tokens.

Diagnostics deliberately filters desktop-only doctor checks the phone cannot
act on. It does not change Private DNS, the VPN flags or any system setting;
the shortcuts are the only writes, and they open the system page for you.

## Privacy and security

- **No telemetry, analytics or crash reporting.** The app sends nothing to the
  Kumiho developers. The only network calls are the engine's Firefox
  Accounts/Guardian/server-list/DoH traffic and the one GitHub update check you
  trigger.
- **Tokens** are written to the app's no-backup storage
  (`noBackupFilesDir/tokens.json`), and Android backup and device transfer are
  disabled for the app (`allowBackup=false`, `fullBackupContent=false`,
  extraction rules excluding every domain). Passwords and verification codes
  are held only for the request that uses them and are not stored.
- **Settings, the server list and logs** are private app files. A log leaves
  the phone only when you choose **Share**.
- **Crashes** are written to `filesDir/crash.log` (capped at 64 KiB, no tokens
  read) and replayed once into the engine log on the next engine start; the
  file is deleted only after a successful append. There is no crash upload.
- **The FileProvider** exposes only `cacheDir/exports/`, is not exported, and
  grants read access per URI. It cannot serve arbitrary app files.
- **`QUERY_ALL_PACKAGES`** is required to list every installed app for the
  split-tunnel picker, including packages with no intent filters. It is
  acceptable for this sideload-only app; it is not a Play/F-Droid build. The
  package list stays on the device.
- **Android Private DNS** is diagnosed, not changed. Diagnostics reports the
  mode and the VPN link's DNS; Kumiho does not silently switch resolvers or
  fall back to plaintext DNS. See the troubleshooting section.
- **No browser fallback and no captcha** support; see Limitations.

## Troubleshooting

### No DNS while connected

The tunnel advertises `10.8.0.1` as its DNS server and the engine answers it
inside the tunnel's userspace stack with strict DoH. If lookups fail while the
tunnel is up:

1. Open **More → Diagnostics** and check the VPN service card: the network's
   DNS should include the VPN link's `10.8.0.1`, and Private DNS should be
   reported honestly.
2. Check the split selection and `exclude_cidrs`. The app protects
   `10.8.0.1/32` from `allow_lan` and user exclusions, so a correctly built
   interface should still reach the resolver; a failure here is exactly the
   stop condition in the physical acceptance checklist, not something to paper
   over by switching to Private DNS or another resolver.
3. Collect **Share**d logs (and `adb logcat` if you are at a computer) before
   changing anything.

`adb logcat | grep -i kumiho` shows the app and engine's process logs;
`adb shell dumpsys connectivity` shows the VPN network and its DNS servers for
a connected session.

### Private DNS

Android's Private DNS can be Off, Opportunistic or a hostname. Kumiho reports
which in Diagnostics and does not change it. With a hostname configured, the
system may prefer that encrypted resolver for some lookups, which can make a
DNS problem look like a tunnel problem. Compare the diagnostic's Private DNS
row with the VPN link's DNS, then test with Private DNS set to Automatic if you
are diagnosing a failure — and put it back afterwards. Do not treat a Private
DNS change as a fix for the tunnel DNS path.

### Battery optimization and OEMs

The foreground service is the primary defence, but Android and some vendors
still delay or kill background work. **Settings → Battery optimization**
shows the real state and opens the system page; setting Kumiho to
**Unrestricted** (or the vendor's equivalent) helps. Some OEM skins also delay
or refuse Always-on VPN starts after a reboot, or restart the service late.
Kumiho reflects what actually happened and never claims enforcement the OS did
not provide. If Always-on does not start after a reboot, check the vendor's
auto-start/background list before filing a Kumiho bug.

### Notification is missing

The status notification lives in the **Connection status** channel
(low-importance, silent, ongoing) and only appears while the service is in the
foreground. If notifications are off, the tunnel still runs but Android cannot
show the status or its **Disconnect** action. Use **Settings →
Notifications → Open notification settings** and allow the channel. Sign-in
prompts and one-off connection failures use the separate, default-importance
**Sign-in and connection alerts** channel. The status notification has
**Disconnect** and **Open**, and deliberately no **Connect**.

### Quick Settings tile is missing or does nothing

Add **Kumiho** from the Quick Settings edit screen. The tile's label is always
`Kumiho`; its subtitle (Connected · location, Connecting…, Proxy only, Sign in
required, VPN permission needed, and so on) needs **Android 10 (API 29) or
newer** — on Android 8.0–9 only the tile's active/inactive state and label are
shown.

A tile tap **connects the saved selection** (the engine's persisted server,
or REC if none is saved) or disconnects an active session. It never starts
proxy-only; that choice belongs to the dashboard's options sheet. The first
tap that needs VPN consent opens Android's dialog; when the grant is really
returned, the connect completes on the tile's next listening pass or on a
re-tap. A second tap while a connect is in flight is ignored, and a signed-out
or unavailable tile opens the app instead of attempting a doomed connect. The
tile can disconnect an existing proxy-only session (it shows *Proxy only*).

### VPN permission was revoked

Revoking Kumiho's VPN access in Android settings ends the session: the service
tells the engine to revoke, refreshes the status, removes the foreground
notification and stops. The dashboard then shows **VPN permission needed**;
the next connect asks again. Nothing is silently re-established.

### The engine will not start

If every screen says *The local engine is unavailable* or *Kumiho cannot
start*, close Kumiho from the recents screen and open it again. This is a
process-level failure (the embedded engine could not start), not a network
problem; Diagnostics cannot run without the engine, and the app never fakes a
report.

### A location change did not apply

The app allows one session at a time. A location selected while connected is
saved for the next connection and the current session keeps its server and
exit IP; disconnect on Home, then connect again. The same is true for split
tunneling and route exclusions.

### The sign-in fails with a captcha

Firefox Accounts sometimes presents an interactive captcha. The engine solves
the Fastly proof-of-work/PAT/clientmetrics challenges automatically but cannot
solve a captcha, and v1 has no browser fallback. The error names the
limitation. Sign in from a network/account state where the challenge is not
presented, or use the desktop client to confirm the account, then retry.

### Uninstalling and removing state

`adb uninstall dev.benyamin.kumiho` (or Settings → Apps → Kumiho → Uninstall)
removes the app and all of its private state — tokens, settings, logs, cached
server list and split selection. A debug build is
`dev.benyamin.kumiho.debug` and is removed separately. If you used Always-on
or lockdown, turn them off in Android's VPN settings first: an uninstalled VPN
app leaves the system setting pointing at nothing and, under lockdown, can
leave you without network access. Uninstalling does not touch the desktop
daemon or your Firefox Account.

## Limitations

- **IPv6 is captured and blackholed**, not tunneled. DNS may still return
  `AAAA` records, so an app that tries IPv6 first can wait for its connection
  attempt to fail before falling back to IPv4. There is no IPv6 leak, but
  there is also no IPv6 connectivity.
- **English-only** UI (logical start/end layout, but no translations).
- **No home-screen widget and no launcher long-press shortcuts**; the Quick
  Settings tile is the fast path.
- **No browser sign-in and no captcha solving.** Firefox Accounts is native
  only.
- **No auto-update and no in-app APK install.** The update check only opens
  the release page.
- **Sideload only** — no Play/F-Droid distribution, so no store update
  channel.
- **The Quick Settings tile's first consent needs one extra step** (the
  dialog cannot be awaited from a tile), and **API 26–28 tiles have no
  subtitle**.
- **OEM Always-on and battery managers** can delay or refuse starts; Kumiho
  cannot override them.
- **Proxy-only has no VPN interface**, so per-app split and Always-on
  interface behaviour do not apply in that mode; the local proxies are
  loopback-only, unauthenticated and device-wide.
- **Non-DNS UDP is blocked** inside the tunnel (QUIC/HTTP3 falls back to TCP;
  VoIP and games may not work). DNS over the SOCKS5 proxy is the exception.
- **Physical acceptance is a partial pass with user-accepted exceptions.**
  The signed `v0.5.1` release is published, but the untested checklist items
  remain exceptions; see [android-testing.md](android-testing.md).

## Desktop parity

The app is a client for the same engine. This is the intended parity map; the
"Status" column says how it is delivered on Android, not that a physical
sign-off has happened.

| Desktop / CLI ability | Android equivalent |
|---|---|
| Setup wizard | Four-step setup: consent, sign-in, permissions, Always-on |
| FxA email/password/2FA/email link | Native Compose sign-in over the same engine state machine |
| Logout | Account confirmation, `logout`, stays on Account |
| Connect/disconnect, saved/REC selection | Dashboard power control and connect options |
| Per-connect location, proxy-only, kill-switch choice | Location picker, proxy-only toggle, OS lockdown shortcut |
| Status rows | Power hero, four data cards and Details |
| Locations tree, refresh, Ping all, Expand all | Locations one-level list, live filter, speed order, 24-way ping |
| 19 settings rows, overrides/reset | Settings groups with the same 19 rows, override chips and Reset |
| Theme/accent/update check | App settings and a manual GitHub release check |
| Account entitlement and sign-out | Account screen |
| Logs: 2000 lines, filter/follow/search/clear/export | Logs screen, share sheet and a log file |
| Diagnostics: doctor, copy, cleanup | Diagnostics with Android facts, Copy all and Reset local state |
| Tray/lifecycle/window | Foreground service, notification, Quick Settings tile, Always-on |
| Kill switch/autoconnect | Read-only Always-on/lockdown rows and Android VPN settings |
| Split tunneling | Off / Exclude / Include with a per-app picker |
| IPv6 | Captured and blackholed; DNS `AAAA` falls back to IPv4 |
| Local SOCKS/HTTP proxies | Engine settings rows with a device-wide warning |
| Version/update | App and engine version, manual update check |
| Crash/diagnostics | Local `crash.log` replayed into the engine log |
| No telemetry/backups | No telemetry, no backup, no-backup token storage |
| Protocol v1 / desktop untouched | No IPC change; the mobile facade and Android provider are additive |

Android deliberately diverges in one place: the Locations tab is one expandable
level (country → server, city as row context) and re-sorts by measured speed,
while the desktop trees keep country → city → server.

## Related documentation

- [android-testing.md](android-testing.md) — toolchain, AAR, tests, APK
  verification and the physical acceptance checklist.
- [usage.md](usage.md) — the desktop CLI, TUI and settings reference.
- [troubleshooting.md](troubleshooting.md) — desktop troubleshooting and
  `kumiho doctor`.
- [install.md](install.md) — Linux and Windows installs.
