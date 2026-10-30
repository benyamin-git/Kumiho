# Installing Kumiho

Kumiho runs as a background service that owns the tunnel, with a TUI/CLI
talking to it over local IPC: a systemd daemon and Unix socket on Linux, a
Windows service and named pipe on Windows. Supported targets are **Ubuntu
24.04 LTS** (amd64 or arm64) and **Windows 11** (amd64).

## Linux (Ubuntu 24.04)

### Requirements

- Linux with systemd; `/dev/net/tun` available (absent on some VPS images —
  `kumiho doctor` tells you early; proxy-only mode still works there).
- systemd-resolved active (the default on Ubuntu). Kumiho never writes
  `/etc/resolv.conf`; it configures DNS per-link through resolvectl.
- The `nft` binary (nftables ≥ 1.0.9) for the kill switch.
- A Firefox account with the built-in VPN entitlement.
- Optional: Tailscale (Kumiho coexists with it: existing SSH and Tailscale
  sessions are never interrupted by connect/disconnect).

### One-command install

On Ubuntu 24.04 amd64 or arm64, this downloads the newest published release
(prereleases included), verifies it against the release `SHA256SUMS`, and runs
the bundled installer:

    curl -fsSL https://raw.githubusercontent.com/benyamin-git/Kumiho/main/scripts/install-release.sh | sudo bash

Pass a tag to install a specific release, or run the uninstaller:

    curl -fsSL https://raw.githubusercontent.com/benyamin-git/Kumiho/main/scripts/install-release.sh | sudo bash -s -- --version v0.7.2
    curl -fsSL https://raw.githubusercontent.com/benyamin-git/Kumiho/main/scripts/install-release.sh | sudo bash -s -- uninstall
    curl -fsSL https://raw.githubusercontent.com/benyamin-git/Kumiho/main/scripts/install-release.sh | sudo bash -s -- uninstall --purge

The script needs `curl`, `unzip` and `sha256sum` (standard on Ubuntu) and must
run as root; it refuses unsupported architectures.

### Build

    make build          # dist/kumiho-linux-amd64
    make build-arm64    # dist/kumiho-linux-arm64

Dependencies are vendored; no network access is needed.

### Install from the checkout

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
    kumiho login        # browser or password, 2FA/captcha aware
    kumiho doctor       # preflight diagnostics with remediation hints

If **autoconnect** is enabled (the first-install default), the daemon brings
the tunnel up on every start — including after a reboot.

### Upgrade

Re-run the one-command installer to move to the newest release, or pull the
new source, `make build` and re-run `sudo ./install.sh`. Either path replaces
the binary and unit and restarts the running service.

### Uninstall

    sudo ./install.sh uninstall          # keeps /var/lib/kumiho (tokens/state)
    sudo ./install.sh uninstall --purge  # also removes state and the
                                         # installer-created system user

Uninstall removes the unit, polkit rule, binary and `/etc/kumiho`. The user is
only deleted by `--purge` when it looks installer-created (system account with
home `/var/lib/kumiho`); a regular account named `kumiho` is always kept.

## Windows 11

Download `Kumiho-Windows-amd64.zip` from the releases page and
extract it, or build from a checkout with `make build-windows` (the installer
then finds the binary in `dist\`). Run everything below in PowerShell **as
Administrator**.

### Install

    .\install.ps1

The installer is idempotent and does the following:

- creates the local group `kumiho` (reused if it already exists),
- installs `kumiho.exe` and the signed `wintun.dll` to `%ProgramFiles%\Kumiho`,
- adds `%ProgramFiles%\Kumiho` to the machine `PATH` (new shells only),
- writes a commented `%ProgramData%\kumiho\config.toml` (only if absent),
- registers the service `kumiho` (LocalSystem, auto-start) with crash
  recovery: restart after 5 s, 30 s, then 60 s,
- adds your user to the `kumiho` group so the CLI can reach the control pipe
  (sign out and back in),
- starts the service.

Then, for the first run (in a new shell):

    kumiho login        # browser or password, 2FA/captcha aware
    kumiho doctor       # preflight diagnostics with remediation hints

If **autoconnect** is enabled (the first-install default), the daemon brings
the tunnel up on every start — including after a reboot. Windows also needs
the Base Filtering Engine service (on by default; `kumiho doctor` checks it)
for the kill switch.

Options: `-Binary PATH` selects the binary to install and `-User NAME` the
account added to the `kumiho` group.

### Upgrade

Re-run `.\install.ps1` with the new binary in place: it stops the service,
replaces `kumiho.exe` and `wintun.dll`, and starts it again. Config and state
under `%ProgramData%\kumiho` are kept.

### Uninstall

    .\install.ps1 -Uninstall          # keeps %ProgramData%\kumiho (tokens/state)
    .\install.ps1 -Uninstall -Purge   # also removes state and the local group

Uninstall stops and deletes the service and removes `%ProgramFiles%\Kumiho`
and its `PATH` entry. Per-user TUI preferences in `%APPDATA%\kumiho\tui.toml`
are left alone.

## Development installs (no service)

`scripts/dev-env.sh` (bash) and `scripts/dev-env.ps1` (Windows) point Kumiho
at a dev state directory (`~/.local/state/kumiho-dev` on Linux,
`%TEMP%\kumiho-dev` on Windows), and `scripts/dev-daemon.sh start|stop|log`
runs the daemon from the checkout with logs in that directory (Linux). On
Windows, run the foreground daemon in its own terminal:

    . .\scripts\dev-env.ps1
    .\dist\kumiho-windows-amd64.exe daemon

Stop the installed service first (`sudo systemctl stop kumiho` on Linux,
`Stop-Service kumiho` on Windows) — only one daemon can own the TUN device and
`127.0.0.1:1080`:

    sudo systemctl stop kumiho
    scripts/dev-daemon.sh start
