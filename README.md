# Kumiho

**Status:** v0.1.0 — a working Linux client: daemon + TUI/CLI over a Unix
socket, Firefox Accounts login (2FA aware), server list with ping, HTTP/2
CONNECT tunnel through Mozilla's Fastly edge, full-tunnel routing with a
kill switch, watchdog with edge rotation, runtime settings, log viewer,
account screen, and a systemd install (`install.sh`).

Kumiho is an unofficial Linux CLI/TUI client for the free Firefox-account VPN
proxy — the same entitlement FoxyVPN for Android uses. It signs into Firefox
Accounts, mints a Guardian proxy pass, resolves the Mozilla server list, and
tunnels system traffic over **HTTP/2 CONNECT** streams to a Fastly edge, with a
local SOCKS5 server in front. On Linux it adds a TUN device + userspace
TCP/IP stack (gVisor via `xjasonlyu/tun2socks`), policy routing that cannot
kill an existing SSH/Tailscale session, an optional nftables kill switch, and a
Bubble Tea TUI plus a systemd daemon so the tunnel survives SSH logouts.

**It is not WireGuard.** No WireGuard config exists for this entitlement.
The transport is HTTP/2 CONNECT: **TCP + DNS only**. Non-DNS UDP (QUIC/HTTP3,
VoIP, games) is blocked on purpose so applications fall back to TCP.

Target platform: **Ubuntu 24.04 LTS amd64/arm64** (systemd daemon + TUI/CLI
over a Unix socket).

## Quick start

    make build                  # dist/kumiho-linux-amd64 (vendored deps)
    sudo ./install.sh           # binary, systemd unit, polkit rule, config
    sudo systemctl start kumiho
    kumiho login                # Firefox Accounts (2FA aware)
    kumiho doctor               # preflight diagnostics
    kumiho                      # TUI

See [docs/manual.md](docs/manual.md) for a plain-language usage manual,
[docs/install.md](docs/install.md) for the details, [docs/usage.md](docs/usage.md)
for the CLI/TUI and settings, and [docs/troubleshooting.md](docs/troubleshooting.md)
for common issues.

## Known limitations

- No anti-bot challenge solver yet; a `406` from Mozilla surfaces as a clear
  error (retry later).
- Non-DNS UDP is intentionally blocked inside the tunnel.
- Using an unofficial client is a ToS gray area (see PLAN.md §13); Kumiho
  never sees or stores your Firefox password, only the resulting tokens.

## Build

Cross-compile from any OS with Go >= 1.27:

    make build          # dist/kumiho-linux-amd64
    make build-arm64    # dist/kumiho-linux-arm64
    make test           # unit tests (run on any OS)
    make vet

Module dependencies are vendored in `vendor/`, so no network access is
needed to build or test.

## Usage

    kumiho                 # TUI (requires a running daemon)
    kumiho daemon          # run the daemon in the foreground
    kumiho login           # prompt-driven Firefox Accounts sign-in (2FA aware)
    kumiho logout
    kumiho status [--json]
    kumiho doctor [--json]
    kumiho version

The full command surface (connect/disconnect flags, locations, logs, cleanup)
is documented in [docs/usage.md](docs/usage.md). The daemon reads its
config/state from `/etc/kumiho` and `/var/lib/kumiho` and listens on
`/run/kumiho/control.sock` (see PLAN.md §8). For development runs without
systemd, override the locations with `KUMIHO_CONFIG`, `KUMIHO_STATE`,
`KUMIHO_TOKENS`, and `KUMIHO_RUNTIME_DIR` (see `scripts/dev-env.sh`).

## Plan and docs

The full design — protocol details, Linux networking, IPC, milestones and
acceptance criteria — lives in [PLAN.md](PLAN.md).

## Disclaimer

Unofficial and not affiliated with or endorsed by Mozilla. Kumiho uses your own
Firefox account entitlement; using an unofficial client is a ToS gray area
(see PLAN.md §13). Review the code before trusting it with your credentials.

## Attribution

See [NOTICE](NOTICE). Protocol behavior reference: FoxyVPN (MIT).
