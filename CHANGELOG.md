# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.4.1] - 2026-10-21

Test-only patch: the Windows test job now matches the platform's file-mode
behavior. The shipped runtime is unchanged from v0.4.0.

### Fixed

- The server-list cache tests no longer assert Unix `0600` permission bits on
  Windows, where `os.Stat` reports `0666`; payload and atomic-overwrite checks
  still run on every platform.

## [0.4.0] - 2026-10-20

Backend parity with the FoxyVPN releases after the one we ported: strict DoH
with endpoint failover, anti-censorship controls, a working upstream proxy and
a local HTTP proxy, and the settings for all of it across the TUI and GUI.

### Added

- In-tunnel DNS failover: a query tries the configured provider (or a custom
  `dns_upstream`), then Cloudflare `1.1.1.1`, then Quad9 `9.9.9.9`, with a
  failing endpoint suppressed for 30 minutes and no plaintext DNS fallback —
  when the whole chain fails the query answers SERVFAIL.
- A server-list cache in the state directory: a successful fetch is written
  atomically (`serverlist.json`, mode 0600) and served when a later fetch
  fails, including the first fetch after a daemon restart. A corrupt cache is
  discarded with a warning and never blocks a connect that could otherwise
  proceed.
- A Fastly anti-bot challenge solver for the Firefox Accounts and Guardian
  control planes: with `solve_challenges=on` (the default) a challenge page is
  answered and its cookies are installed in a shared jar, including the
  related origins, so the request can be retried. A captcha or an exhausted
  budget produces a clear error instead of a silent hang; `solve_challenges=off`
  turns solving off.
- TLS ClientHello fragmentation for the edge and control-plane handshakes:
  `tls_split=off` (default), `first-byte` writes the first byte of the record
  separately, and `sni` splits around the first hostname byte. ClientHello
  contents are unchanged; the record is split across two TCP writes, and the
  split applies per connection, so a new value takes effect on the next dial.
- A functional `upstream_proxy`: the edge dial can go through a SOCKS5,
  SOCKS5h or HTTP proxy with optional credentials (`socks5://`, `socks5h://`
  or `http://`), hands the edge hostname to the proxy unresolved so there is
  no local DNS lookup, and composes with `edge_address` pinning. Only the edge
  dial uses the hop; control-plane, DoH and bootstrap traffic stay direct.
- A local HTTP proxy alongside SOCKS5: when `http_proxy=on`, a loopback
  listener on `http_proxy_port` (default `8080`) accepts `CONNECT` and
  absolute-form plain HTTP through the same tunnel, with the same local-target
  refusals as SOCKS. It starts and stops with the SOCKS listener.
- DNS over SOCKS5 `UDP ASSOCIATE`: DNS-only datagrams are relayed through the
  same in-tunnel DoH chain, so proxy-only mode can answer DNS without the TUN
  listener. Non-DNS and foreign datagrams are dropped.
- `keepalive_profile=standard|slow` for slow links: the standard profile times
  out an unanswered keepalive after 10 s, the slow profile after 25 s.
- The five new runtime settings (`tls_split`, `http_proxy`, `http_proxy_port`,
  `solve_challenges`, `keepalive_profile`) in `config.toml`, `state.json`
  overrides and the control protocol, in the TUI settings screen (now
  scrollable) and in the Windows GUI.

### Changed

- DNS inside the tunnel is strict DoH: the plaintext TCP/UDP 53 fallback is
  gone, and `doh_provider=off` without a custom `dns_upstream` disables
  in-tunnel DNS (every query answers SERVFAIL) instead of falling back to
  plaintext. The outside-the-tunnel bootstrap path keeps its TCP:53 fallback.
- Documentation now matches the service: the Fastly edge accepts TCP 80 and
  443 only (CONNECT to 53, 853 and 444 is refused), DNS is described as DoH
  only, and the Windows acceptance checklist covers restoring DoH and
  reconnecting afterwards.

### Fixed

- Guardian entitlement challenges are retried up to five times, with the
  solver when it is enabled, before failing — matching the FxA challenge loop.
- Signing out now wipes the account and usage view (quota, exit IP and
  country, pass, totals and rates), and an in-flight pass renewal or redial
  can no longer resurrect that state after teardown.
- `upstream_proxy` is now actually used by the edge dialer instead of being
  validated and stored but ignored.

## [0.3.0] - 2026-10-17

The Windows 11 desktop app: a WebView2 GUI, its installer integration and the
release/CI packaging for it.

### Added

- Windows 11 desktop app `kumiho-gui.exe`: a native shell that renders the UI
  with the Microsoft Edge WebView2 Runtime, owns the tray icon and window, and
  talks to the same daemon over the same local control protocol — the daemon
  still does all the tunnel work and the app runs per logged-in user.
- Six screens: Dashboard (connect with per-connect options, live status),
  Locations (filter and speed test), Settings (theme, accent, start
  at login, and the daemon's settings with per-setting overrides), Account,
  Logs (level filter, follow, search, export) and Diagnostics (`kumiho doctor`
  checks, service start/stop and cleanup, the last two via UAC).
- Five-step first-run wizard: consent, Firefox Accounts sign-in, health check,
  autoconnect and finish.
- Tray and lifecycle: connect/disconnect and location menus, close hides to
  the tray, quit from the tray, start at login hidden, single-instance focus,
  and a remembered window size and position.
- `kumiho gui` launcher for the app (Windows; on other platforms it points at
  the TUI).
- Installer: `install.ps1` (and a new double-click `install.cmd`) install the
  GUI beside the CLI, add a Start Menu shortcut and a per-user login entry, and
  grant the installing user access to the daemon's control pipe so the app
  works without a re-login; `-Uninstall` removes the shortcut, login entry and
  pipe access.
- Release/CI: `v*` zips now ship `kumiho-gui.exe` beside `kumiho.exe` and
  `wintun.dll` (the WebView2 loader is embedded, so no new runtime file is
  needed), `SHA256SUMS` covers it, and the Windows CI job version-smokes the
  GUI.
- Docs: `docs/gui.md` user guide and a v0.3.0 GUI section in
  `docs/windows-testing.md`.

### Changed

- The Windows GUI is rebuilt on web technologies: hand-written HTML/CSS/JS
  embedded in the binary, with the palette (Light/Dark/OLED, six accents,
  default blue) computed in Go and the system UI font (Segoe UI Variable on
  Windows 11) used throughout. It replaces the earlier native-toolkit GUI,
  which never shipped.
- A dev-only web harness serves the real UI assets on `0.0.0.0:7407` against a
  mock daemon for local work (override the port with `KUMIHO_GUI_WEB_PORT`);
  neither the harness nor the probe tool ships in a release.

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

[Unreleased]: https://github.com/benyamin-git/kumiho/compare/v0.4.1...HEAD
[0.4.1]: https://github.com/benyamin-git/kumiho/releases/tag/v0.4.1
[0.4.0]: https://github.com/benyamin-git/kumiho/releases/tag/v0.4.0
[0.3.0]: https://github.com/benyamin-git/kumiho/releases/tag/v0.3.0
[0.2.0]: https://github.com/benyamin-git/kumiho/releases/tag/v0.2.0
[0.1.0]: https://github.com/benyamin-git/kumiho/releases/tag/v0.1.0
