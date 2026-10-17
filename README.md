# Kumiho

**Status:** v0.3.0 — working Linux and Windows 11 clients: daemon + TUI/CLI/GUI
over a Unix socket (Linux) or named pipe (Windows), Firefox Accounts login
(2FA aware), server list with ping, HTTP/2 CONNECT tunnel through Mozilla's
Fastly edge, full-tunnel routing with a kill switch, watchdog with edge
rotation, runtime settings, log viewer, account screen, and a systemd
(`install.sh`) or Windows service (`install.ps1`) install.

Kumiho is an unofficial Linux and Windows CLI/TUI client for the free
Firefox-account VPN proxy — the same entitlement FoxyVPN for Android uses. It
signs into Firefox Accounts, mints a Guardian proxy pass, resolves the Mozilla
server list, and tunnels system traffic over **HTTP/2 CONNECT** streams to a
Fastly edge, with a local SOCKS5 server in front. On Linux it adds a TUN
device + userspace TCP/IP stack (gVisor via `xjasonlyu/tun2socks`), policy
routing that cannot kill an existing SSH/Tailscale session, an optional
nftables kill switch, and a Bubble Tea TUI plus a systemd daemon so the tunnel
survives SSH logouts. On Windows it uses a Wintun adapter, a WFP kill switch
and a LocalSystem service.

**It is not WireGuard.** No WireGuard config exists for this entitlement.
The transport is HTTP/2 CONNECT: **TCP + DNS only**. Non-DNS UDP (QUIC/HTTP3,
VoIP, games) is blocked on purpose so applications fall back to TCP.

Current targets: **Ubuntu 24.04 LTS amd64/arm64** (systemd daemon + TUI/CLI
over a Unix socket) and **Windows 11 amd64** (Windows service + Wintun, and
TUI/CLI/GUI over a named pipe).

## Platform roadmap

The core is platform-pluggable behind `internal/platform` (design:
[docs/superpowers/specs/2026-10-10-multiplatform-core-design.md](docs/superpowers/specs/2026-10-10-multiplatform-core-design.md)).
Linux and Windows 11 ship today; each new platform must reach full parity
before release:

- **Windows 11 amd64** — Windows service + Wintun, TUI client over a named pipe.
- **Android** — planned: VpnService app with the engine embedded in-process, simple GUI.
- **Other Linux distros/archs** — planned: pluggable network-configuration backends.

## Quick start

Linux (Ubuntu 24.04):

    make build                  # dist/kumiho-linux-amd64 (vendored deps)
    sudo ./install.sh           # binary, systemd unit, polkit rule, config
    sudo systemctl start kumiho
    kumiho login                # Firefox Accounts (2FA aware)
    kumiho doctor               # preflight diagnostics
    kumiho                      # TUI

Windows 11, from a release zip in an Administrator PowerShell:

    .\install.ps1               # kumiho.exe + wintun.dll, service, PATH

Then, in a new shell:

    kumiho login                # Firefox Accounts (2FA aware)
    kumiho doctor               # preflight diagnostics
    kumiho                      # TUI

From a checkout, run `make build-windows` first: the installer finds the
binary in `dist\` and `wintun.dll` under `third_party\wintun\amd64\`.

See [docs/manual.md](docs/manual.md) for a plain-language usage manual,
[docs/install.md](docs/install.md) for the details, [docs/usage.md](docs/usage.md)
for the CLI/TUI and settings, and [docs/troubleshooting.md](docs/troubleshooting.md)
for common issues.

## Known limitations

- No anti-bot challenge solver yet; a `406` from Mozilla surfaces as a clear
  error (retry later).
- Non-DNS UDP is intentionally blocked inside the tunnel.
- On Windows, connecting may reset existing sessions: there is no per-socket
  bypass equivalent to Linux `fwmark` policy routing, so connections opened
  before the tunnel comes up can be dropped. LAN and exclude-CIDR traffic
  bypasses the tunnel as on Linux.
- The Windows binary is unsigned: SmartScreen warns on first run (More
  info → Run anyway), and `install.ps1` needs an Administrator shell.
- Using an unofficial client is a ToS gray area (see PLAN.md §13); Kumiho
  never sees or stores your Firefox password, only the resulting tokens.

## Build

Cross-compile from any OS with Go >= 1.27:

    make build          # dist/kumiho-linux-amd64
    make build-arm64    # dist/kumiho-linux-arm64
    make build-windows  # dist/kumiho-windows-amd64.exe + dist/kumiho-gui-windows-amd64.exe
    make build-gui      # dist/kumiho-gui-windows-amd64.exe (Windows GUI)
    make build-probe    # dist/kumiho-gui-probe-windows-amd64.exe (diagnostic)
    make build-web      # dist/kumiho-gui-web-linux-amd64 (GUI dev harness)
    make test           # unit tests (run on any OS)
    make vet

Module dependencies are vendored in `vendor/`, so no network access is
needed to build or test. The Windows build needs the signed `wintun.dll`
committed under `third_party/wintun/amd64/` beside the binary at run time;
the release zip and `install.ps1` place it in `%ProgramFiles%\Kumiho`. The
GUI needs no extra file at run time: the WebView2 loader is embedded in
`kumiho-gui.exe`.

## Usage

    kumiho                 # TUI (requires a running daemon)
    kumiho daemon          # run the daemon in the foreground
    kumiho login           # prompt-driven Firefox Accounts sign-in (2FA aware)
    kumiho logout
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
(see `scripts/dev-env.sh` and `scripts/dev-env.ps1`).

## Windows GUI

Kumiho's Windows 11 desktop app (v0.3.0), `kumiho-gui.exe`, is a small native
shell that renders the interface with the Microsoft Edge WebView2 Runtime,
owns the tray icon and the window, and talks to the same daemon over the same
control pipe. The daemon still does all the tunnel work; the app is a client
that runs per logged-in user. It has six screens:

- **Dashboard** — the connection state, Connect/Disconnect, per-connect
  options (location, proxy only, kill switch) and the live status card;
- **Locations** — a filterable country/city/server tree with a speed test;
- **Settings** — Light/Dark/OLED theme, six accents, start at login, plus the
  daemon's own settings with per-setting overrides;
- **Account** — the entitlement and a tonal sign-out;
- **Logs** — the daemon log with level filter, follow, search and export;
- **Diagnostics** — `kumiho doctor` checks, service start/stop and cleanup
  (the last two via UAC).

A five-step wizard (consent, sign-in, health check, autoconnect, finish) runs
on first launch. Closing the window hides it to the tray rather than quitting;
the tray keeps Connect/Disconnect, location, Show, Diagnostics and Quit, and
the app starts hidden at login by default.

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

## Plan and docs

The full design — protocol details, Linux networking, IPC, milestones and
acceptance criteria — lives in [PLAN.md](PLAN.md).

## Disclaimer

Unofficial and not affiliated with or endorsed by Mozilla. Kumiho uses your own
Firefox account entitlement; using an unofficial client is a ToS gray area
(see PLAN.md §13). Review the code before trusting it with your credentials.

## Attribution

See [NOTICE](NOTICE). Protocol behavior reference: FoxyVPN (MIT).
