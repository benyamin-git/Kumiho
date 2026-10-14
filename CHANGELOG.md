# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- Toggling `kill_switch` on a live tunnel no longer reports success when the
  network policy re-apply fails: the change returns an error and the persisted
  override is rolled back, so the UI never shows a kill switch that is not
  actually enforced.

## [0.2.0] - 2026-10-14

Windows 11 client, multiplatform core and hygiene fixes. Verified on real
Windows 11 amd64 hardware (install, Windows service, doctor, `kumiho version`
and the TUI full tunnel); see
[docs/windows-testing.md](docs/windows-testing.md) for the checklist.

### Added

- Windows 11 amd64 support: one `kumiho.exe` serves the service, CLI and TUI.
- Full tunnel on Windows: Wintun adapter `foxy0` (10.8.0.2/32, MTU 8500),
  bypass routes for Tailscale/exclude-CIDR/LAN traffic, default routes with
  the tunnel winning the metric race, exit check and an IPv6 blackhole.
- Windows service `kumiho`: installs via `install.ps1` as LocalSystem with
  automatic start, crash recovery (restart after 5 s, 30 s, 60 s),
  autoconnect (default on, remembers the last state), and clean
  `-Uninstall [-Purge]`.
- Named-pipe control transport `\\.\pipe\kumiho\control` over `go-winio`
  (byte-mode, newline JSON v1) with an ACL for LocalSystem, Administrators
  and the local `kumiho` group; `shutdown` is administrators-only.
- Windows DNS parity: adapter-scoped resolver at `10.8.0.2`, DoH through the
  tunnel with TCP:53 fallback, custom `doh_provider`/`dns_upstream`, no
  system-wide settings touched.
- Windows kill switch: dynamic WFP filters with loopback/service/tunnel/DHCP/
  DNS/LAN/exclude-CIDR permits, cleanup on start and stop, and the crash
  caveat documented and mitigated by service recovery.
- Windows link watching (IP Helper notifications with polling fallback) and
  default-route probing (`GetIpForwardTable2`).
- Windows doctor/cleanup checks: service, `wintun.dll`, Base Filtering
  Engine, control pipe, `foxy0` adapter, adapter DNS, physical default route,
  elevation; `kumiho cleanup` also removes a crashed run's adapter and
  default routes.
- Release workflow: `v*` tags build linux/amd64, linux/arm64 and
  windows/amd64 zips (binary, `wintun.dll`, `install.sh`/`install.ps1`,
  systemd units, licenses) with `SHA256SUMS` and a GitHub release.
- `third_party/wintun/`: signed Wintun 0.14.1 amd64 DLL, Prebuilt Binaries
  license and checksums, committed for hermetic builds.
- Docs: `AGENTS.md`, `CHANGELOG.md`, `docs/windows-testing.md`, Windows
  content throughout README/docs, and the wintun license note in `NOTICE`.

### Changed

- Multiplatform core (ships in this version): the engine moved to
  `internal/engine` behind the `internal/platform.Provider` seam, with a real
  Windows provider, a per-platform IPC transport seam and OS-aware
  config/state/tokens paths.
- CI: actions on current majors (checkout v6, setup-go v7); new
  `windows-latest` job (vet, tests, build, version smoke, `install.ps1` parse
  check); the build job cross-compiles Windows and Android.

### Fixed

- `internal/dns`: data race between server Start and Stop.
- `kumiho doctor` on Windows no longer lists the nine Linux-only checks as
  `SKIP`; they are dropped from the report off Linux.
- Release binaries now stamp `Commit` and `Date` as well as `Version`, so
  `kumiho version` no longer prints `commit unknown, built unknown`.
- Android/unsupported providers: fall back to the temp directory when
  `os.UserConfigDir` fails; platform `doc.go` files stay untagged so
  `go build ./...` sees them.
- TUI: fall back when the per-user config directory is unavailable.
- Stale comments and wording in the engine and IPC packages.

## [0.1.0] - 2026-10-08

Initial release: a working Linux client (Ubuntu 24.04 amd64/arm64).

### Added

- Daemon + TUI/CLI over a Unix socket, with Firefox Accounts login (2FA
  aware), Guardian entitlement and proxy pass, and the Mozilla server list
  with ping.
- HTTP/2 CONNECT tunnel to Mozilla's Fastly edge with a local SOCKS5 server;
  TCP + DNS only (non-DNS UDP is dropped on purpose).
- Full-tunnel networking: TUN device + tun2socks, policy routing that leaves
  existing SSH/Tailscale sessions alone, optional nftables kill switch,
  per-link DNS through systemd-resolved, and IPv6 blackholing.
- Watchdog with edge rotation and failover, pass renewal, runtime settings,
  log viewer, account screen, and `kumiho doctor`/`kumiho cleanup`.
- systemd install (`install.sh`) with a polkit rule, `kumiho` group access
  and autoconnect on boot.

[Unreleased]: https://github.com/benyamin-git/kumiho/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/benyamin-git/kumiho/releases/tag/v0.2.0
[0.1.0]: https://github.com/benyamin-git/kumiho/releases/tag/v0.1.0
