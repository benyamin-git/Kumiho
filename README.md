# Kumiho

**Status:** v0.7.2 — working Linux and Windows 11 clients plus a native Android
app for phones and Android TV: daemon + TUI/CLI/GUI over a Unix
socket (Linux) or named pipe (Windows),
Firefox Accounts login (2FA aware), server list with ping and an on-disk cache,
HTTP/2 CONNECT tunnel through Mozilla's Fastly edge, full-tunnel routing with a
kill switch, watchdog with edge rotation, runtime settings, log viewer, account
screen, and a systemd (`install.sh`) or Windows service (`install.ps1`)
install. v0.6.0 adds browser and password sign-in, up to five accounts with
labels, rotation and quota polling, and the same account surface in the CLI,
TUI, Windows GUI and Android app. v0.6.1 (unreleased) replaces the browser
sign-in exchange with direct Firefox Accounts OAuth: Android finishes it in an
in-app WebView, desktop clients paste the browser's redirect URL, and old
opaque browser accounts are removed. v0.7.0 adds Android TV: one APK for
phones and televisions, a Compose for TV shell with remote-only navigation,
full parity for setup, dashboard, locations, account, settings, split
tunneling, logs and diagnostics, a TV launcher banner and an optional Google
TV channel with **Open** and **Connect** cards. v0.7.1 fixes password sign-in
on networks where Mozilla's edge hard-blocks the primary auth host and hardens
terminal sign-in input. v0.7.2 fixes the Android TV setup layout and the
connected test suite. v0.5.2 adds
an opt-in Android setting that keeps LAN connections alive across a VPN
transition and reworks Locations to one level. v0.5.1
adds country flags, Ping all and Expand all/Collapse all to the locations
screen on Android, the Windows GUI and the TUI. v0.5.0 adds the
Android client (sideload-only; physical acceptance is a partial pass with
user-accepted exceptions). v0.6.0, v0.7.0, v0.7.1 and v0.7.2 physical
acceptance is pending; the signed
[v0.7.2 release](https://github.com/benyamin-git/kumiho/releases/tag/v0.7.2)
is published; see [docs/android-testing.md](docs/android-testing.md).
v0.4.0 adds backend
parity with later FoxyVPN releases: strict DoH failover, anti-bot challenge
solving, TLS ClientHello fragmentation, an upstream proxy, a local HTTP proxy,
DNS over SOCKS5 UDP ASSOCIATE, a server-list cache and a slow keepalive
profile. v0.4.1 fixes the Windows test-job assumptions for the server-list
cache; the desktop runtime is unchanged from v0.4.0.

Kumiho is an unofficial Linux, Windows and Android client for the free
Firefox-account VPN proxy — the same entitlement FoxyVPN for Android uses. It
signs into Firefox Accounts, mints a Guardian proxy pass, resolves the Mozilla
server list, and tunnels system traffic over **HTTP/2 CONNECT** streams to a
Fastly edge, with a local SOCKS5 server in front. On Linux it adds a TUN
device + userspace TCP/IP stack (gVisor via `xjasonlyu/tun2socks`), policy
routing that cannot kill an existing SSH/Tailscale session, an optional
nftables kill switch, and a Bubble Tea TUI plus a systemd daemon so the tunnel
survives SSH logouts. On Windows it uses a Wintun adapter, a WFP kill switch
and a LocalSystem service. On Android the same Go engine is embedded in the
app through gomobile, a `VpnService` owns the tunnel, and a Compose UI covers
setup, dashboard, locations, settings, split tunneling, account, logs,
diagnostics, a status notification and a Quick Settings tile.

**It is not WireGuard.** No WireGuard config exists for this entitlement.
The transport is TCP over HTTP/2 CONNECT to the Fastly edge, which accepts
ports 80 and 443 only. Non-DNS UDP (QUIC/HTTP3, VoIP, games) is blocked on
purpose so applications fall back to TCP, and DNS travels as DoH over HTTPS
inside the tunnel — there is no separate plaintext DNS path. The in-tunnel
resolver is strict: it tries the configured provider (or a custom
`dns_upstream`) first, then Cloudflare, then Quad9; a failed endpoint is
skipped for 30 minutes, and if every endpoint fails the query answers
SERVFAIL instead of falling back to plaintext.

Current targets: **Ubuntu 24.04 LTS amd64/arm64** (systemd daemon + TUI/CLI
over a Unix socket), **Windows 11 amd64** (Windows service + Wintun, and
TUI/CLI/GUI over a named pipe) and **Android 8.0+ (API 26+) on arm64-v8a or
armeabi-v7a**, phones and Android TV/Google TV (sideloaded app with the engine
embedded in-process).

## Screenshots

Captured in the Dark theme with sample data. The Windows GUI images are
1280×800 from the Linux dev harness; the Linux TUI views are 100×28.

### Windows GUI

| Dashboard | Locations |
| --- | --- |
| ![Windows GUI dashboard: connected tunnel, quota and session totals](screenshots/windows-gui/dashboard.png) | ![Windows GUI locations: flagged country rows, Test speed / Ping all / Expand all actions and ping times](screenshots/windows-gui/locations.png) |

![Windows GUI settings: theme and accent controls, version, and background service rows](screenshots/windows-gui/settings.png)

### Linux TUI

| Dashboard | Locations |
| --- | --- |
| ![Linux TUI dashboard: connected state, quota bar and session totals](screenshots/linux-tui/dashboard.png) | ![Linux TUI locations: flagged country tree with expanded rows, ping times and expand/collapse-all hints](screenshots/linux-tui/locations.png) |

![Linux TUI settings: all settings rows and the TLS split hint](screenshots/linux-tui/settings.png)

The GUI images are the WebView2 UI assets rather than a native Windows
desktop frame, so they do not include the title bar or tray. The TUI images
are rendered from the real Bubble Tea views. Regenerate both sets with
`scripts/screenshots/capture.sh`; it starts the dev harness on
`0.0.0.0:7407` and uses Playwright. See
[`scripts/screenshots/README.md`](scripts/screenshots/README.md).

## Platform roadmap

The core is platform-pluggable behind `internal/platform` (design:
[docs/superpowers/specs/2026-10-10-multiplatform-core-design.md](docs/superpowers/specs/2026-10-10-multiplatform-core-design.md)).
Linux and Windows 11 ship today; each new platform must reach full parity
before release:

- **Windows 11 amd64** — Windows service + Wintun, TUI client over a named pipe.
- **Android 8.0+** — implemented as a sideloaded VpnService app with the
  engine embedded in-process; physical acceptance is a partial pass with
  user-accepted exceptions, and the signed `v0.6.0` release is published
  (see [docs/android-testing.md](docs/android-testing.md)).
- **Android TV / Google TV** — the same APK and engine with a Compose for TV
  shell, a launcher banner and an optional Google TV channel; see
  [docs/android-tv.md](docs/android-tv.md).
- **Other Linux distros/archs** — planned: pluggable network-configuration backends.

## What's new in v0.7.2

- **TV setup works again** — the side column no longer takes the whole row
  width, so the consent and sign-in steps are visible, and each step starts at
  the top instead of inheriting the previous scroll position.
- **TV connected suite green** — the D-pad focus, dashboard picker and setup
  tests were repaired, and the job now uploads its Gradle reports on failure.
- **CI resilience** — a transient checksum-database drop during the gomobile
  bind is retried once.

## What's new in v0.7.1

- **Password sign-in through a blocked edge** — when Fastly answers
  `POST api.accounts.firefox.com/v1/account/login` with an empty `406` that no
  solved challenge can clear, the shared engine now asks the sibling
  `oauth.accounts.firefox.com/v1` auth host before spending a solve, solves
  each challenged host once, and retries once if the sibling connection drops.
  Every client gets the fix because it lives in the engine.
- **Hardened terminal sign-in** — the TUI drops stray control characters that
  Windows IME composition and unmapped keys deliver as zero runes, and the
  engine rejects control characters or inner whitespace in an email with
  **enter a valid email address** instead of forwarding a body FxA rejects.
- **Development pipe override** — `KUMIHO_CONTROL_ENDPOINT` moves the Windows
  control pipe so a development daemon can run beside the installed service.

## What's new in v0.7.0

- **One APK, phone and TV** — the Android app detects a television at runtime
  and swaps the phone shell for a ten-foot Compose for TV shell. Phone setup
  and layouts are unchanged.
- **Full TV parity** — setup and browser sign-in, Dashboard with connect
  options, Locations with search/ping, Account management, all 21 Settings
  rows and dialogs, split tunneling, Logs and Diagnostics, with remote-only
  D-pad navigation.
- **TV launcher presence** — `LEANBACK_LAUNCHER`, a 320×180 banner, a TV icon
  and a best-effort Google TV preview channel with **Open Kumiho** and
  **Connect** cards (`kumiho://tv/open`, `kumiho://tv/connect`).
- **TV tests and CI** — a Google TV AVD target, `make android-tv-emulator-test`,
  TV connected coverage, screenshot capture and a separate `android-tv` CI job.

## What's new in v0.6.1 (unreleased)

- **Direct Firefox Accounts OAuth** — browser sign-in now uses the project's
  existing Firefox client with PKCE S256, `state` and the
  `profile https://identity.mozilla.com/apps/vpn` scope. The exchange returns
  ordinary FxA access and refresh tokens, the profile lookup supplies the
  UID/email, and refresh works like a password login. The browser page still
  handles captcha, password, TOTP and unblock codes interactively.
- **Android in-app WebView** — **Continue in browser** opens a full-screen
  **Mozilla account** page inside the app; the `urn:` redirect is intercepted
  and the sign-in finishes without leaving the app. The waiting card keeps
  **Open again**, **Paste redirect URL instead** (**Redirect URL** →
  **Continue**), **Use password** and **Cancel**. If the WebView cannot be
  created, the URL opens in a Custom Tab with the paste field available.
- **Desktop redirect-URL paste** — the Linux CLI and Windows GUI open
  Mozilla's page; the TUI prints the URL for any browser. All desktop clients
  ask for the complete redirect URL. Mozilla's “can't open
  this address” page is expected; copy the address-bar URL and paste it. There
  is no loopback listener, so the browser can run on another machine.
- **Upgrade cleanup** — accounts created by the old opaque browser flow are
  removed on first start with the notice **“Old browser sign-in removed. Add
  your Firefox account again.”** Password accounts are untouched.
- **Sign-out revocation** — sign-out best-effort revokes each stored FxA
  refresh token and always completes the local sign-out, even offline. Android
  also clears the in-app WebView's Firefox Accounts cookies.

## What's new in v0.6.0

- **Browser sign-in** — `kumiho login --browser` (or the browser choice in the
  TUI, Windows GUI and Android setup) introduced interactive captcha, TOTP and
  unblock handling. Its v0.6.0 token exchange is replaced in v0.6.1 (above);
  `--no-open` still prints the URL for a headless shell.
- **Password fallback** — email, password, email-link 2FA and TOTP/unblock
  codes still work, including on a headless terminal. No password is stored.
- **Up to five accounts** — the Account screen lists label/email, method,
  active marker, re-auth state and quota; it can add, use, rename, remove and
  re-authenticate without signing out the other accounts.
- **Quota rotation** — `account_rotation` is opt-in and runs only before
  connect. Below `rotation_threshold` (default 10%) the engine picks the
  best eligible account, skips exhausted or expired accounts with a notice, and
  fails clearly only when none can connect.
- **Quota polling** — while connected, `quota_poll_minutes` (default 15)
  refreshes the active account's quota; failures back off to a 30-minute cap
  and never disconnect the tunnel.
- **Storage** — `accounts.json` v2 is authoritative; a legacy `tokens.json` is
  migrated and kept as a synchronized rollback copy until logout. Both are
  mode 0600 and written atomically.

Browser sign-in (direct FxA OAuth using the project's Firefox OAuth client and
Mozilla's account page) and automatic account rotation are a documented ToS
gray area for this unofficial client; see
[PLAN.md §13](PLAN.md#13-risks-and-mitigations) before using multiple accounts.

## What's new in v0.4.0

v0.4.0 brings the tunnel in line with the later FoxyVPN releases. The
transport is unchanged — HTTP/2 CONNECT to the Fastly edge — and the new
controls sit inside it.

- **Strict DoH with failover** — the resolver uses the configured provider
  (or a custom `dns_upstream`), then Cloudflare `1.1.1.1`, then Quad9
  `9.9.9.9`. A failed endpoint is skipped for 30 minutes; when all of them
  fail the query answers SERVFAIL, never plaintext DNS.
- **Anti-bot challenge solving** — Fastly's proof-of-work, PAT and
  clientmetrics challenges on the Firefox Accounts and Guardian control
  planes are answered automatically (`solve_challenges`, default `on`).
  Captchas are not solved; see the limitations.
- **TLS ClientHello fragmentation** — `tls_split` (`off|first-byte|sni`,
  default `off`) splits the first ClientHello across TCP segments to defeat
  naive TLS fingerprinting.
- **Upstream proxy** — `upstream_proxy` can carry the edge connection through
  a SOCKS5, SOCKS5h or HTTP proxy with optional credentials.
- **Local HTTP proxy** — `http_proxy` (default `off`) and `http_proxy_port`
  (default `8080`) add a loopback HTTP proxy next to SOCKS5.
- **DNS over SOCKS5 UDP ASSOCIATE** — the relay is DNS-only: datagrams reach
  the same DoH chain, so proxy-only mode can answer DNS without the TUN
  listener. Non-DNS datagrams are dropped.
- **Server-list cache** — the last good server list is saved as
  `serverlist.json` beside the state file and served if a later fetch fails.
- **Slow keepalive profile** — `keepalive_profile` (`standard|slow`, default
  `standard`) raises the keepalive timeout from 10 s to 25 s for slow links.

These settings live in `config.toml`, in per-user overrides, and in the TUI
and Windows GUI. `tls_split`, `http_proxy`, `http_proxy_port` and
`keepalive_profile` take effect on the next connect; `solve_challenges` is
read per control-plane request, so a change applies immediately. The full
setting reference is in [docs/usage.md](docs/usage.md).

### Upstream proxy

Set `upstream_proxy` in `config.toml` (or in the TUI settings) to carry the
edge connection through a proxy:

    upstream_proxy = "socks5://user:pass@proxy.example.net:1080"

The scheme can be `socks5://`, `socks5h://` or `http://`, the `user:pass@`
credentials are optional, and the port defaults to 1080 for SOCKS and 80 for
HTTP. The proxy carries the edge connection only: the control plane, Remote
Settings, bootstrap and DoH stay direct, and the proxy resolves the edge
hostname (no local DNS lookup). It does not affect the local SOCKS5 or HTTP
listeners that applications use.

### HTTP proxy

Enable the local HTTP proxy in `config.toml`:

    http_proxy = true
    http_proxy_port = 8080

Then point a client at it:

    curl -x http://127.0.0.1:8080 https://example.com

It listens on loopback only, has no authentication, accepts `CONNECT` and
absolute-form plain HTTP, and starts and stops with the tunnel.

## Quick start

Linux (Ubuntu 24.04):

    make build                  # dist/kumiho-linux-amd64 (vendored deps)
    sudo ./install.sh           # binary, systemd unit, polkit rule, config
    sudo systemctl start kumiho
    kumiho login                # browser or password (2FA aware)
    kumiho doctor               # preflight diagnostics
    kumiho                      # TUI

Windows 11, from a release zip in an Administrator PowerShell:

    .\install.ps1               # kumiho.exe + wintun.dll, service, PATH

Then, in a new shell:

    kumiho login                # browser or password (2FA aware)
    kumiho doctor               # preflight diagnostics
    kumiho                      # TUI

From a checkout, run `make build-windows` first: the installer finds the
binary in `dist\` and `wintun.dll` under `third_party\wintun\amd64\`.

Android 8.0+, from a checkout (sideload; no Play/F-Droid):

    bash scripts/android-setup.sh    # JDK 17, SDK 36 + 37.2, NDK 28.2, gomobile
    make android-debug               # AAR + debug APK
    adb install -r android/app/build/outputs/apk/debug/app-debug.apk

See [docs/android.md](docs/android.md) for the app itself and
[docs/android-testing.md](docs/android-testing.md) for the toolchain,
emulator, APK and physical-acceptance details.

See [docs/manual.md](docs/manual.md) for a plain-language usage manual,
[docs/install.md](docs/install.md) for the details, [docs/usage.md](docs/usage.md)
for the CLI/TUI and settings, and [docs/troubleshooting.md](docs/troubleshooting.md)
for common issues.

## Known limitations

- Non-DNS UDP (QUIC/HTTP3, VoIP, games) is intentionally blocked inside the
  tunnel; applications must fall back to TCP.
- The anti-bot solver handles the Fastly proof-of-work/PAT/clientmetrics
  challenge. An interactive captcha cannot be solved by the password flow; use
  browser sign-in, where Mozilla's page handles captcha, TOTP and unblock
  codes. The password flow fails with a clear message instead of hanging.
- TLS fragmentation is best-effort: `first-byte` and `sni` only split the
  first ClientHello on a connection, and malformed or SNI-less hellos pass
  through unsplit.
- The local HTTP proxy is loopback-only and has no authentication; any local
  process can use it while the tunnel is up.
- The default TUN MTU is 8500, configurable from 576 to 65535 and applied on
  the next connect: the TUN device feeds a gVisor userspace network stack that
  handles packets of that size itself, so the usual mobile 1400 rationale
  does not apply.
- On Windows, connecting may reset existing sessions: there is no per-socket
  bypass equivalent to Linux `fwmark` policy routing, so connections opened
  before the tunnel comes up can be dropped. LAN and exclude-CIDR traffic
  bypasses the tunnel as on Linux.
- The Windows binary is unsigned: SmartScreen warns on first run (More
  info → Run anyway), and `install.ps1` needs an Administrator shell.
- On Android, IPv6 is captured and blackholed rather than tunneled, so apps
  can wait for an `AAAA` attempt to fail before falling back to IPv4. The UI
  is English-only and there is no auto-update (the Quick Settings tile covers
  fast access on phones, and the TV launcher banner plus an optional Google TV
  channel cover televisions).
  Always-on and lockdown are Android settings and some OEM battery managers
  delay or refuse Always-on starts. Physical acceptance is a partial pass with
  user-accepted exceptions; the signed `v0.6.0` release is published. See
  [docs/android.md](docs/android.md#limitations).
- Using an unofficial client is a ToS gray area (see PLAN.md §13); automatic
  account rotation is an additional gray area, so it is opt-in and off by
  default. Kumiho never sees or stores your Firefox password, only the
  resulting tokens.

## Build

Cross-compile from any OS with Go >= 1.27:

    make build          # dist/kumiho-linux-amd64
    make build-arm64    # dist/kumiho-linux-arm64
    make build-windows  # dist/kumiho-windows-amd64.exe + dist/kumiho-gui-windows-amd64.exe
    make build-gui      # dist/kumiho-gui-windows-amd64.exe (Windows GUI)
    make build-probe    # dist/kumiho-gui-probe-windows-amd64.exe (diagnostic)
    make build-web      # dist/kumiho-gui-web-linux-amd64 (GUI dev harness)
    make android-setup  # install JDK 17, SDK 36 + 37.2, NDK 28.2, gomobile
    make android-aar    # android/app/libs/kumiho.aar (arm + arm64, git-ignored)
    make android-aar-emulator  # x86_64 AAR for the emulator
    make android-debug  # AAR + debug APK
    make android-test   # JVM tests + lint + debug APK
    make android-emulator-test  # connected tests (emulator running)
    make android-tv-emulator-test  # connected TV tests (Google TV AVD running)
    make android-release KUMIHO_VERSION=0.7.2 KUMIHO_EXPECT_CERT_SHA256=<sha256>
    KUMIHO_EXPECT_CERT_SHA256=<sha256> make android-verify
    make test           # unit tests (run on any OS)
    make vet

The Android app is normally built through the Makefile targets above, which
build the AAR first; the raw Gradle equivalent is
`cd android && ./gradlew :app:assembleDebug`. See
[docs/android-testing.md](docs/android-testing.md) for unit/lint/connected,
signing, verification and the physical checklist. The AAR is a build output
and is never committed.

Module dependencies are vendored in `vendor/`, so no network access is
needed to build or test. The Android Gradle dependencies are not vendored;
they resolve from Google/Maven Central once and then work with `--offline`.
The Windows build needs the signed `wintun.dll`
committed under `third_party/wintun/amd64/` beside the binary at run time;
the release zip and `install.ps1` place it in `%ProgramFiles%\Kumiho`. The
GUI needs no extra file at run time: the WebView2 loader is embedded in
`kumiho-gui.exe`.

## Usage

    kumiho                 # TUI (requires a running daemon)
    kumiho daemon          # run the daemon in the foreground
    kumiho login           # browser or password Firefox Accounts sign-in
    kumiho accounts        # list, add, use, remove, rename, rotate
    kumiho logout          # clear every account; disconnects
    kumiho status [--json]
    kumiho doctor [--json]
    kumiho version

The full command surface (connect/disconnect flags, locations, logs, cleanup)
is documented in [docs/usage.md](docs/usage.md). On Linux the daemon reads its
config/state from `/etc/kumiho` and `/var/lib/kumiho` and listens on
`/run/kumiho/control.sock`; on Windows it reads `%ProgramData%\kumiho` and
listens on the named pipe `\\.\pipe\kumiho\control` (see PLAN.md §8). For
development or alternative locations on either OS, override with
`KUMIHO_CONFIG`, `KUMIHO_STATE`, `KUMIHO_TOKENS`, and `KUMIHO_RUNTIME_DIR`
(see `scripts/dev-env.sh` and `scripts/dev-env.ps1`). On Windows,
`KUMIHO_CONTROL_ENDPOINT` moves the control pipe so a development daemon can
run beside the installed service.

## Windows GUI

Kumiho's Windows 11 desktop app (v0.4.0), `kumiho-gui.exe`, is a small native
shell that renders the interface with the Microsoft Edge WebView2 Runtime,
owns the tray icon and the window, and talks to the same daemon over the same
control pipe. The daemon still does all the tunnel work; the app is a client
that runs per logged-in user. It has six screens:

- **Dashboard** — the connection state, Connect/Disconnect, per-connect
  options (location, proxy only, kill switch) and the live status card;
- **Locations** — a filterable country/city/server tree with country flags,
  Test speed, Ping all and Expand all/Collapse all;
- **Settings** — Light/Dark/OLED theme, six accents, start at login, plus the
  daemon's own settings with per-setting overrides;
- **Account** — the account list (add, use, rename, remove, re-authenticate),
  the entitlement, a rotation switch and threshold, a browser-waiting card,
  and a session-wide sign-out;
- **Logs** — the daemon log with level filter, follow, search and export;
- **Diagnostics** — `kumiho doctor` checks, service start/stop and cleanup
  (the last two via UAC).

A five-step wizard (consent, browser/password sign-in, health check,
autoconnect, finish) runs on first launch. Closing the window hides it to the
tray rather than quitting; the tray keeps Connect/Disconnect, location, Show,
Diagnostics and Quit, and the app starts hidden at login by default.

Install it with the normal Windows installer (see [Quick start](#quick-start)):
`install.ps1` places the app beside the CLI, adds a Start Menu shortcut and a
login entry, and grants your account access to the control pipe. Start it from
the Start Menu, or from a shell with:

    kumiho gui

From a checkout, `make build-windows` produces both the CLI/TUI binary and
`kumiho-gui.exe`, and `make build-gui` builds the app on its own. The app needs
the WebView2 Runtime, which ships with Windows 11; if it is missing, the app
offers Microsoft's download page. The full guide — install, first run, each
screen, the tray, diagnostics and uninstall — is
[docs/gui.md](docs/gui.md).

To develop the UI in a browser on this machine against the mock daemon:

    make build-web
    scripts/dev-gui-web.sh start   # real UI assets on http://<host>:7407/
    scripts/dev-gui-web.sh stop

The harness binds `0.0.0.0:7407` (override the port with
`KUMIHO_GUI_WEB_PORT`) and a `/dev` panel drives mock scenarios, latency and
failures. The GUI is Windows-only and the harness is a dev tool; neither
changes the daemon, CLI or TUI.

## Android app

Kumiho's Android app (v0.7.2) is a native sideloaded client for phones and
Android TV/Google TV, not a front end
for the daemon: the same Go engine is embedded in the app process through a
gomobile AAR, and a `VpnService` owns the tunnel, the foreground status
notification and the Quick Settings tile. It targets Android 8.0 (API 26) and
newer on `arm64-v8a` or `armeabi-v7a`, and is distributed as an APK — there is
no Play or F-Droid build. The direct Firefox Accounts OAuth WebView described
below replaced the v0.6.0 exchange and is in the published v0.7.0 APK.

The app has a four-step setup (consent, browser or password Firefox Accounts
sign-in, VPN and notification permissions, Always-on guidance), then the same
surfaces as the desktop clients: a dashboard with connect options and live
status, locations with country flags, a search, one **Ping all** action that
orders the list by measured speed, and Expand all/Collapse all, and all 21
settings rows (19 engine settings plus the read-only Always-on/lockdown rows
Android owns), with overrides and Reset. Browser sign-in completes in a
full-screen in-app **Mozilla account** WebView (with paste and password
fallbacks). Split tunneling, the five-account
list with browser re-authentication and rotation, logs with sharing, diagnostics
and proxy-only mode (local SOCKS5/HTTP without a VPN interface) are all there
too. On Android TV and Google TV the same screens use the Compose for TV shell:
a left rail, remote-only focus traversal, a launcher banner and an optional
channel with **Open** and **Connect** cards; see
[docs/android-tv.md](docs/android-tv.md).

> **Release status.** The app builds and its unit, lint and assemble gates pass;
> the phone emulator suite and the TV connected suite pass in CI. Physical
> first-connect/DNS/browsing/exit-IP acceptance passed on a Galaxy S25 FE for
> v0.5.2; the remaining checklist items are user-accepted exceptions (a partial
> acceptance, not a full pass), and v0.6.0, v0.7.0, v0.7.1 and v0.7.2 physical
> acceptance is pending. The production-signed
> [v0.7.2 release](https://github.com/benyamin-git/kumiho/releases/tag/v0.7.2)
> carries three APKs and `SHA256SUMS`. Verify a download against the release
> checksums; a local APK is not a published release.

Build it with the toolchain and AAR commands in
[docs/android-testing.md](docs/android-testing.md); the user guide, including
split tunneling, Always-on/lockdown, proxy-only, privacy and troubleshooting,
is [docs/android.md](docs/android.md). The emulator AAR (`make
android-aar-emulator`) is for tests only and is never part of a release.

## Plan and docs

The full design — protocol details, Linux networking, IPC, milestones and
acceptance criteria — lives in [PLAN.md](PLAN.md). The Android client has its
own user guide at [docs/android.md](docs/android.md) and build/test/release
guide at [docs/android-testing.md](docs/android-testing.md).

## Disclaimer

Unofficial and not affiliated with or endorsed by Mozilla. Kumiho uses your own
Firefox account entitlement; using an unofficial client is a ToS gray area
(see PLAN.md §13). Review the code before trusting it with your credentials.

## Attribution

See [NOTICE](NOTICE). Protocol behavior reference: FoxyVPN (MIT).
