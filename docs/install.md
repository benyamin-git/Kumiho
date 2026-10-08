# Installing Kumiho

Kumiho is a systemd daemon plus a TUI/CLI talking to it over a Unix socket.
The supported target is **Ubuntu 24.04 LTS (amd64 or arm64)** with systemd,
systemd-resolved and nftables (all default on Ubuntu Server).

## Requirements

- Linux with systemd; `/dev/net/tun` available (absent on some VPS images —
  `kumiho doctor` tells you early; proxy-only mode still works there).
- systemd-resolved active (the default on Ubuntu). Kumiho never writes
  `/etc/resolv.conf`; it configures DNS per-link through resolvectl.
- The `nft` binary (nftables ≥ 1.0.9) for the kill switch.
- A Firefox account with the built-in VPN entitlement.
- Optional: Tailscale (Kumiho coexists with it: existing SSH and Tailscale
  sessions are never interrupted by connect/disconnect).

## Build

    make build          # dist/kumiho-linux-amd64
    make build-arm64    # dist/kumiho-linux-arm64

Dependencies are vendored; no network access is needed.

## Install

    sudo ./install.sh

The installer is idempotent and does the following:

- creates the `kumiho` system user (reused if it already exists),
- installs `/usr/local/bin/kumiho`,
- installs `/etc/systemd/system/kumiho.service` (runs as `kumiho` with
  `AmbientCapabilities=CAP_NET_ADMIN`; children inherit it, which file
  capabilities cannot do),
- installs the polkit rule `50-kumiho-resolved.rules` so the daemon can
  manage per-link DNS without a prompt,
- writes a commented `/etc/kumiho/config.toml` (only if absent),
- enables the unit and adds your user (`$SUDO_USER`) to the `kumiho` group so
  the CLI can reach the control socket (log out and back in).

Then, for the first run:

    sudo systemctl start kumiho
    kumiho login        # Firefox Accounts, 2FA aware
    kumiho doctor       # preflight diagnostics with remediation hints

If **autoconnect** is enabled (the first-install default), the daemon brings
the tunnel up on every start — including after a reboot.

## Upgrade

Pull the new source, `make build` and re-run `sudo ./install.sh`: it replaces
the binary and unit and restarts the running service.

## Uninstall

    sudo ./install.sh uninstall          # keeps /var/lib/kumiho (tokens/state)
    sudo ./install.sh uninstall --purge  # also removes state and the
                                         # installer-created system user

Uninstall removes the unit, polkit rule, binary and `/etc/kumiho`. The user is
only deleted by `--purge` when it looks installer-created (system account with
home `/var/lib/kumiho`); a regular account named `kumiho` is always kept.

## Development installs (no systemd)

`scripts/dev-env.sh` (bash) and `scripts/dev-env.ps1` (Windows) point Kumiho
at `~/.local/state/kumiho-dev`, and `scripts/dev-daemon.sh start|stop|log`
runs the daemon from the checkout with logs in that directory. Stop the
systemd service first — only one daemon can own the TUN device and
`127.0.0.1:1080`:

    sudo systemctl stop kumiho
    scripts/dev-daemon.sh start
