# AGENTS.md

Guidance for coding agents working in the Kumiho repository.

Kumiho is a Go daemon + TUI/CLI/GUI client for the Firefox-account VPN
entitlement, plus a native Android app with the same engine embedded in
process. The daemon owns the desktop tunnel; clients — the TUI, the CLI and a
Windows GUI — talk to it over local IPC. Supported targets: **Ubuntu 24.04
LTS** (amd64/arm64, systemd + Unix socket), **Windows 11 amd64** (service +
Wintun + named pipe), **Android 8.0+** (arm64-v8a/armeabi-v7a, sideloaded
`VpnService` app over gomobile) on phones and Android TV/Google TV. Read
[README.md](README.md) first; the protocol and design live in
[PLAN.md](PLAN.md), user documentation in [docs/](docs/) (Android:
[docs/android.md](docs/android.md),
[docs/android-testing.md](docs/android-testing.md)).

## Commands

Everything is cross-compiled with vendored dependencies; no network access is
needed to build or test.

    make build          # linux/amd64   -> dist/kumiho-linux-amd64
    make build-arm64    # linux/arm64   -> dist/kumiho-linux-arm64
    make build-windows  # windows/amd64 -> dist/kumiho-windows-amd64.exe + dist/kumiho-gui-windows-amd64.exe
    make build-gui      # windows/amd64 -> dist/kumiho-gui-windows-amd64.exe
    make build-probe    # windows/amd64 -> dist/kumiho-gui-probe-windows-amd64.exe
    make build-web      # linux/amd64   -> dist/kumiho-gui-web-linux-amd64 (dev harness)
    make android-setup  # install JDK 17, SDK 36 + 37.2, NDK 28.2, gomobile
    make android-aar    # android/arm + android/arm64 -> android/app/libs/kumiho.aar
    make android-aar-emulator  # android/amd64 -> android/app/libs/kumiho-emulator.aar
    make android-debug  # AAR + debug APK
    make android-test   # JVM tests + lint + debug APK
    make android-emulator-test  # connected tests (emulator running)
    make android-tv-emulator-test  # connected TV tests (Google TV AVD running)
    make android-release KUMIHO_VERSION=0.7.2 KUMIHO_EXPECT_CERT_SHA256=<sha256>
    make android-verify # verify every release APK in dist/
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

Android app changes additionally leave the app gate green:

    make android-test
    # emulator only (AVD kumiho-api36, x86_64, booted):
    make android-emulator-test

The Android app compiles against SDK 37.2 while `targetSdk` stays 36 (the
pinned Compose/Core/Lifecycle/Navigation releases require `minCompileSdk 37`);
the AAR is built with `-androidapi 26`. Gradle never runs Go, so the AAR must
be built first; the `make` targets do that. The unit/lint/assemble and
connected suites are the `android` CI job, not part of the Go gate above; run
them locally for any change under `android/`, `mobile/` or
`internal/platform/android/`. See [docs/android-testing.md](docs/android-testing.md).

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
- `scripts/android-setup.sh [--check] [--ci]` — install or verify JDK 17,
  SDK 36 + 37.2, build-tools 36.0.0, NDK 28.2.13676358, the API 36 x86_64
  system image, the pinned gomobile and the Gradle wrapper bootstrap.
  `scripts/android-env.sh` exports the same toolchain for a shell.
- `scripts/android-build-aar.sh [--emulator]` — build the git-ignored gomobile
  AAR that Gradle links (`make android-aar` /
  `make android-aar-emulator`).
- `scripts/android-verify-apk.sh <apk>` — release gate for badging, ABI,
  16 KB alignment, arm64 `p_align`, signature and universal size; accepts
  `KUMIHO_ALLOW_UNSIGNED=1` for an unsigned CI compile check.
- `scripts/android-release.sh` — with `KUMIHO_VERSION`,
  `KUMIHO_EXPECT_CERT_SHA256` and `android/keystore.properties`, build the
  AAR, sign the per-ABI and universal APKs into `dist/`, verify them against
  the expected certificate and print SHA-256.
- `scripts/android-connected-test.sh` — source the Android environment and run
  the connected suite; also the emulator action entry point.
- `scripts/screenshots/capture.sh` — regenerate the README's GUI and TUI
  screenshots; builds the web harness with a release version label (default
  `v0.5.1`), restarts it on `0.0.0.0:7407` per GUI screen, and renders the
  TUI captures. Noto Color Emoji is required for the flag captures. See
  `scripts/screenshots/README.md`.

## CI jobs

- `.github/workflows/ci.yml`
  - `build-test` (ubuntu-24.04): vet, tests, race tests, `make build` and
    `make build-arm64` + version smoke, Windows/Android cross-builds,
    informational doctor smoke.
  - `windows` (windows-latest): vet, tests, CLI + GUI builds and version
    smokes, `install.ps1` parse check.
  - `android` (ubuntu-24.04): JDK 17 + SDK/NDK/gomobile, emulator AAR, JVM
    tests/lint/debug APK, the API 36 connected suite, an unsigned
    `assembleRelease`, and the verifier over the unsigned release APKs.
  - `android-tv` (ubuntu-24.04): the same JVM/lint/assemble build and the
    API 36 Google TV connected suite (`tv_1080p`, `scripts/android-tv-connected-test.sh`).
- `.github/workflows/release.yml` — on `v*` tags: builds linux/amd64,
  linux/arm64 and windows/amd64; packages zips (CLI, `kumiho-gui.exe`,
  `wintun.dll`, `install.sh`/`install.ps1`, systemd units, licenses including
  the WebView2 loader), builds and verifies the signed Android APKs with the
  five `ANDROID_*` secrets (including the certificate fingerprint), appends
  the APKs to `SHA256SUMS`, attaches all desktop and Android assets, and
  creates the GitHub release. The signing material is removed by a step trap
  and an `always()` step. During the `v0.x` series every release is published
  as a GitHub prerelease; hyphenated tags are not published.
- The Android CI job and release steps are implemented and were exercised by
  the `v0.5.0` release on 2026-10-22, the `v0.5.1` release on 2026-10-23, the
  `v0.5.2` release on 2026-10-23, the `v0.6.0` release on 2026-10-24, the
  `v0.7.0` release on 2026-10-28, the `v0.7.1` release on 2026-10-30 and the
  `v0.7.2` release on 2026-10-30; each release workflow published the desktop
  and Android assets. See [docs/android-testing.md](docs/android-testing.md).

## Architecture map

- **Composition root**: `cmd/kumiho` → `internal/cli`;
  `internal/platform/auto` selects the OS provider once and injects it.
- **The engine is OS-free** (`internal/engine`): states, FxA/Guardian, server
  list, HTTP/2 CONNECT edge session, SOCKS, watchdog, exit check. It consumes
  the frozen `platform.Provider` seam (`internal/platform/platform.go`); do
  not add OS branches to it — platform behavior goes through the provider.
- **v0.4.0 engine helpers**: `internal/fastly` (Fastly challenge solver,
  stdlib only; isolated per-solve jar, cookies installed into the shared
  control-plane jar), `internal/tlsplit` (best-effort ClientHello
  fragmentation; `off|first-byte|sni`), and `internal/httpproxy` (loopback
  HTTP/CONNECT proxy). The engine owns their lifecycle: the SOCKS5 listener
  also serves the DNS-only `UDP ASSOCIATE` relay, and `http_proxy=on` starts
  the local HTTP proxy alongside the tunnel; both start and stop with the
  tunnel and stay out of the `internal/platform` seam.
- **v0.6.1 sign-in/accounts**: `internal/fxa` owns direct Firefox Accounts
  OAuth (authorization URL, PKCE S256, code exchange, profile lookup and
  best-effort refresh-token destroy) alongside the existing password client
  and refresh. The engine consumes it in `internal/engine/browser.go`: an
  in-memory flow with a ten-minute deadline, no listener, and start/status/
  complete/cancel commands; `login_complete` accepts the complete `urn:`
  redirect URL.
  Clients only open the returned URL and poll `login_status`/
  `accounts_add_status`; desktop clients paste the redirect URL, while Android
  intercepts it in the in-app WebView. `internal/webauth` is gone.
  `internal/engine/accounts.go` owns `accounts.json` v2 (five-account cap,
  labels, active id, `last_method`, FxA tokens, durable `reauth_required`) and
  migration from legacy `tokens.json`, which remains a synchronized rollback
  copy until logout; start removes accounts that only carry the superseded
  opaque `Browser` token. `internal/engine/rotation.go` owns opt-in
  before-connect quota rotation and the connected quota poller; it never
  rotates mid-session. `logout` best-effort destroys every stored FxA refresh
  token, then always clears local state.
- **Platform seam** (`internal/platform/`): `linux/` (TUN + tun2socks, policy
  routing/SO_MARK, nftables kill switch, resolvectl DNS, netlink watch),
  `windows/` (Wintun device, IP Helper routes + interface DNS, dynamic WFP
  kill switch, IP Helper link watch + `GetIpForwardTable2`), `android/` (fd
  adoption, IPv6 blackhole, no-op net config, userspace tunnel DNS — the
  VpnService advertises a routed `10.8.0.1` answered in the tun2socks stack
  because apps cannot bind port 53; the OS VPN interface is built by the
  Kotlin `VpnService`, not by Go) and `unsupported/` stubs. Prefer additive
  interface methods over breaking the seam.
- **IPC**: `internal/ipc` is control protocol v1 (newline-delimited JSON) and
  must stay byte-compatible. v0.6.0 adds the additive command types
  `login_start`/`login_status`/`login_cancel`,
  `accounts_list`/`accounts_add_start`/`accounts_add_status`/
  `accounts_add_cancel`, `accounts_remove`, `accounts_use`,
  `accounts_update`, `accounts_set_rotation` and `accounts_info`; existing
  envelopes and fields are unchanged. v0.6.1 additively adds `redirect_url` to
  `LoginCompletePayload`; `code` stays in the struct for protocol shape, but
  the browser flow rejects it. `ipc.DefaultTransport()` selects the
  platform transport (`default_unix.go` / `default_windows.go`): Unix socket
  vs a `go-winio` named pipe with the `kumiho` group ACL. Windows `shutdown` is
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
- **Android app** (`mobile/`, `internal/platform/android/`, `android/`): the
  Go engine is embedded in the app process, not exposed over IPC. `mobile/`
  (module root, package `mobile`) is the gomobile facade: `Mobile.start`
  with a five-key paths JSON, `command` taking the same control-protocol v1
  JSON envelopes as the daemon, `palette`/`version`, and the Kotlin
  `DeviceBridge`/`Listener` callbacks. `scripts/android-build-aar.sh` binds it
  for `android/arm,android/arm64` (release) or `android/amd64` (emulator) into
  a git-ignored AAR under `android/app/libs/`; Gradle never runs Go. The app
  (`android/app/src/main/java/dev/benyamin/kumiho/`) is Kotlin/Compose:
  `vpn/` owns `KumihoVpnService`, the bridge, the interface spec, the
  coordinator, notifications and the QS tile; `engine/` is the pure-JVM client
  over the facade; `ui/` is the shell and screens; `data/` holds DataStore
  preferences and split tunneling; `crash/` the local crash log;
  `diagnostics/` the Android facts. Browser sign-in is a full-screen in-app
  WebView over `accounts.firefox.com` that intercepts the `urn:` redirect and
  submits the complete URL through `login_complete`; `ui/account/AccountBrowser.kt`
  is only the validated system-browser fallback when the WebView cannot be
  created, and the waiting card offers paste and password alternatives. WebView
  cookies persist across attempts and are cleared on sign-out.
  `ui/tv/` is the additive ten-foot shell (rail, focus surfaces, TV-native
  screens, setup and account reuse); `tv/TvEnvironment` selects it and
  `tv/TvChannelPublisher` publishes the best-effort Google TV channel.
  `res/xml/network_security_config.xml` permits cleartext only for
  `127.0.0.1`/`localhost`, while app-wide cleartext stays off. The Android
  provider adopts the detached
  fd, drops IPv6, leaves interface route/DNS work to Kotlin and answers the
  advertised tunnel DNS in userspace (apps cannot bind port 53).
  Protocol v1, `internal/ipc`, the daemon and the desktop clients are
  untouched and stay byte-compatible.
- **Deliberate OS branches outside the seam** (all small and paired):
  `internal/cli/doctor.go` (`runtime.GOOS` branches) plus
  `doctor_{windows,other}.go` (doctor checks),
  `internal/cli/service_{windows,other}.go` (SCM host vs foreground),
  `internal/daemon/sink_{unix,windows}.go` (log sink),
  `internal/tui/export_*.go` and `prefs_*.go` (per-user paths), and
  `internal/netcfg/exec_*.go` / `mark_*.go`. `kumiho cleanup` reaches
  `Cleanup`/`CleanupStale` through interfaces, not OS switches.
- **Settings/paths**: `internal/settings` owns the per-OS config/state/tokens
  layout plus the `KUMIHO_*` overrides; `accounts.json` lives beside
  `tokens.json` (the legacy rollback copy). The Android app passes its own
  app-private paths (`filesDir`, `noBackupFilesDir`, `cacheDir`) through the
  facade.

## Design

- Palette: skill `design` — six accents, default blue
- Themes: Light / Dark / OLED; first run follows prefers-color-scheme; OLED opt-in
- Type: system-ui (Segoe UI Variable on Windows 11), one family
- Emoji: country-flag emoji in locations are a deliberate, user-approved
  departure from design ban 3 (v0.5.1); Windows renders regional-indicator
  letters.
- TV: Compose for TV type scale and remote-first focus (larger type, focus
  ring, overscan-safe frame) is an approved form-factor departure, not a
  second palette. TV assets: `tv_banner`, `ic_launcher_tv`, channel logo.
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
- The Android gomobile AAR is a **build output**, never committed
  (`android/app/libs/*.aar` is ignored). It is rebuilt from the vendored Go
  tree by `scripts/android-build-aar.sh`; `golang.org/x/mobile` is a pinned Go
  tool dependency (`go.mod`/`vendor/`), and the emulator AAR (`x86_64`) is for
  tests only — never ship it.
- Android app dependencies (AndroidX, Jetpack Compose, Kotlin, kotlinx
  coroutines) are Maven artifacts resolved by Gradle from Google/Maven
  Central, not vendored. The versions are pinned in
  `android/gradle/libs.versions.toml`; `--offline` works after the first
  resolve. Their Apache-2.0/BSD notices are recorded in `NOTICE`.
- Update `NOTICE` when adding third-party code or binaries.

## Commit style

- One logical change per commit; every commit leaves the gate above green.
- Subject: `type: lowercase imperative summary`; types in use: `feat`, `fix`,
  `refactor`, `test`, `docs`, `ci`, `chore` (see `git log`).
- **Never push without being asked.** Do not create tags, amend published
  commits, force-push, or open PRs unless explicitly requested.
- `.superpowers/` and `.ai-state` are local scratch; keep them out of commits.

## T3 Code

T3 Code is the primary harness. Do not restart `kumiho.service`, its emulator,
or any Gradle build from a T3 thread; follow the service rules above.
