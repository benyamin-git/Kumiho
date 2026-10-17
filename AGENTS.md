# AGENTS.md

Guidance for coding agents working in the Kumiho repository.

Kumiho is a Go daemon + TUI/CLI/GUI client for the Firefox-account VPN
entitlement. The daemon owns the tunnel; clients — the TUI, the CLI and a
Windows GUI — talk to it over local IPC. Supported targets: **Ubuntu 24.04
LTS** (amd64/arm64, systemd + Unix socket) and **Windows 11 amd64** (service +
Wintun + named pipe). Read
[README.md](README.md) first; the protocol and design live in
[PLAN.md](PLAN.md), user documentation in [docs/](docs/).

## Commands

Everything is cross-compiled with vendored dependencies; no network access is
needed to build or test.

    make build          # linux/amd64   -> dist/kumiho-linux-amd64
    make build-arm64    # linux/arm64   -> dist/kumiho-linux-arm64
    make build-windows  # windows/amd64 -> dist/kumiho-windows-amd64.exe + dist/kumiho-gui-windows-amd64.exe
    make build-gui      # windows/amd64 -> dist/kumiho-gui-windows-amd64.exe
    make build-probe    # windows/amd64 -> dist/kumiho-gui-probe-windows-amd64.exe
    make build-web      # linux/amd64   -> dist/kumiho-gui-web-linux-amd64 (dev harness)
    make test           # go test ./...
    make test-integration  # go test -tags integration ./... (real endpoints)
    make lint           # gofmt check + go vet ./...
    make vet            # go vet ./...
    make fmt            # gofmt -w over the tree (skips vendor/)
    make clean          # remove dist/

The Go version is pinned in `go.mod` (1.27.1). Every commit must leave this
gate green:

    make lint && make test && go test -race -count=1 ./... && make build && \
      GOOS=windows GOARCH=amd64 go vet ./... && \
      GOOS=windows GOARCH=amd64 go build ./... && \
      GOOS=android GOARCH=arm64 go build ./...

## Dev scripts

- `scripts/dev-env.sh`, `scripts/dev-env.ps1` — point `KUMIHO_CONFIG`,
  `KUMIHO_STATE`, `KUMIHO_TOKENS` and `KUMIHO_RUNTIME_DIR` at a scratch dev
  directory (`~/.local/state/kumiho-dev`, `%TEMP%\kumiho-dev`); source them
  before running a foreground daemon from `dist/`.
- `scripts/dev-daemon.sh start|stop|log` — Linux dev daemon as root with a
  file log (production capabilities live in the systemd unit).
- `scripts/dev-gui-web.sh start|stop|log` — Linux GUI web harness
  (`make build-web`) on `0.0.0.0:7407` (dashboard-registered; override with
  `KUMIHO_GUI_WEB_PORT`), serving the real UI assets in a browser against the
  mock daemon; the `/dev` panel drives scenarios, latency and failures.
- `scripts/watchdog-retest.sh` — live watchdog heal check against a real edge
  (`ss -K`, needs root).

## CI jobs

- `.github/workflows/ci.yml`
  - `build-test` (ubuntu-24.04): vet, tests, race tests, `make build` and
    `make build-arm64` + version smoke, Windows/Android cross-builds,
    informational doctor smoke.
  - `windows` (windows-latest): vet, tests, CLI + GUI builds and version
    smokes, `install.ps1` parse check.
- `.github/workflows/release.yml` — on `v*` tags: builds linux/amd64,
  linux/arm64 and windows/amd64; packages zips (CLI, `kumiho-gui.exe`,
  `wintun.dll`, `install.sh`/`install.ps1`, systemd units, licenses including
  the WebView2 loader), writes `SHA256SUMS` and creates the GitHub release.

## Architecture map

- **Composition root**: `cmd/kumiho` → `internal/cli`;
  `internal/platform/auto` selects the OS provider once and injects it.
- **The engine is OS-free** (`internal/engine`): states, FxA/Guardian, server
  list, HTTP/2 CONNECT edge session, SOCKS, watchdog, exit check. It consumes
  the frozen `platform.Provider` seam (`internal/platform/platform.go`); do
  not add OS branches to it — platform behavior goes through the provider.
- **Platform seam** (`internal/platform/`): `linux/` (TUN + tun2socks, policy
  routing/SO_MARK, nftables kill switch, resolvectl DNS, netlink watch),
  `windows/` (Wintun device, IP Helper routes + interface DNS, dynamic WFP
  kill switch, IP Helper link watch + `GetIpForwardTable2`), `android/` and
  `unsupported/` stubs. Prefer additive interface methods over breaking the
  seam.
- **IPC**: `internal/ipc` is control protocol v1 (newline-delimited JSON) and
  must stay byte-compatible. `ipc.DefaultTransport()` selects the platform
  transport (`default_unix.go` / `default_windows.go`): Unix socket vs a
  `go-winio` named pipe with the `kumiho` group ACL. Windows `shutdown` is
  administrators-only (Linux root parity).
- **Windows GUI** (`internal/gui`, `cmd/kumiho-gui*`): a Windows-only
  (build-tagged `*_windows.go`) WebView2 shell + tray client, plus an untagged
  dev harness (`cmd/kumiho-gui-web`) and a support probe (`cmd/kumiho-gui-probe`).
  It is purely additive: a client over the same control protocol and platform
  transport that reuses `internal/doctor` and `internal/ipc`, so the engine and
  protocol v1 stay OS-free and byte-identical. The UI is hand-written
  HTML/CSS/JS embedded with `go:embed` and served from an in-process loopback
  server; `internal/gui/mock` backs the harness and the Go tests,
  `internal/gui/theme` computes the palette, `internal/gui/prefs` owns
  `gui.toml`. The harness binds `0.0.0.0:7407` (dev only, never released).
- **Deliberate OS branches outside the seam** (all small and paired):
  `internal/cli/doctor.go` (`runtime.GOOS` branches) plus
  `doctor_{windows,other}.go` (doctor checks),
  `internal/cli/service_{windows,other}.go` (SCM host vs foreground),
  `internal/daemon/sink_{unix,windows}.go` (log sink),
  `internal/tui/export_*.go` and `prefs_*.go` (per-user paths), and
  `internal/netcfg/exec_*.go` / `mark_*.go`. `kumiho cleanup` reaches
  `Cleanup`/`CleanupStale` through interfaces, not OS switches.
- **Settings/paths**: `internal/settings` owns the per-OS config/state/tokens
  layout plus the `KUMIHO_*` overrides.

## Design

- Palette: skill `design` — six accents, default blue
- Themes: Light / Dark / OLED; first run follows prefers-color-scheme; OLED opt-in
- Type: system-ui (Segoe UI Variable on Windows 11), one family
- Approved: 2026-10-15

## Vendor policy

- Go dependencies are vendored under `vendor/` (`go mod vendor`); commit
  `go.mod`, `go.sum` and `vendor/` together. Builds and tests must not need
  the network.
- `third_party/wintun/` commits the signed Wintun 0.14.1 `amd64/wintun.dll`
  with its Prebuilt Binaries license and checksums so Windows builds stay
  hermetic (decision D11). It ships side-by-side with `kumiho.exe`; never
  embed it or load it from memory. Verify with
  `cd third_party/wintun && sha256sum -c SHA256SUMS`; the replacement
  procedure is in `third_party/wintun/README.md`.
- `github.com/jchv/go-webview2` (and its `jchv/go-winloader` dependency) is
  vendored for the Windows GUI; the build embeds Microsoft's WebView2Loader
  DLL from its `webviewloader/sdk/x64/` payload, so no loader DLL ships as a
  separate file. `third_party/webview2-loader/` keeps the Microsoft 3-Clause
  BSD license text (pruned from the vendored tree) and checksums; verify with
  `cd third_party/webview2-loader && sha256sum -c SHA256SUMS`.
- Update `NOTICE` when adding third-party code or binaries.

## Commit style

- One logical change per commit; every commit leaves the gate above green.
- Subject: `type: lowercase imperative summary`; types in use: `feat`, `fix`,
  `refactor`, `test`, `docs`, `ci`, `chore` (see `git log`).
- **Never push without being asked.** Do not create tags, amend published
  commits, force-push, or open PRs unless explicitly requested.
- `.superpowers/` and `.ai-state` are local scratch; keep them out of commits.
