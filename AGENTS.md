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

- `scripts/install-release.sh` — one-command Linux install of the newest
  published release; verifies the release `SHA256SUMS` and runs the bundled
  `install.sh`. Supports `--version vX.Y.Z` and forwards `uninstall`/`--purge`.
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
  `v1.0.0`, override with `KUMIHO_CAPTURE_VERSION`), restarts it on
  `0.0.0.0:7407` per GUI screen, and renders the
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
  creates a **draft** GitHub release. The signing material is removed by a
  step trap and an `always()` step. The draft is published manually as the
  latest release after the Android phone and TV builds pass, so the
  `/releases/latest` compatibility links resolve; v0.7.2 and earlier remain
  prereleases. Hyphenated tags are not published.
  Assets use versioned primary names plus stable aliases for
  `/releases/latest` compatibility. User-facing download links (README,
  `README.fa.md`, release notes and handoffs) use the versioned primary names,
  never the stable aliases or an ad-hoc file share. The release description is
  built from the matching `CHANGELOG.md` section plus a download table. The
  manual checklist (including the repository About panel) is
  [docs/releasing.md](docs/releasing.md).
- The Android CI job and release steps are implemented and were exercised by
  the `v0.5.0` release on 2026-10-22, the `v0.5.1` release on 2026-10-23, the
  `v0.5.2` release on 2026-10-23, the `v0.6.0` release on 2026-10-24, the
  `v0.7.0` release on 2026-10-28, the `v0.7.1` release on 2026-10-30, the
  `v0.7.2` release on 2026-10-30, the `v0.8.0` release on 2026-10-30, the
  `v0.8.1` release on 2026-10-30, the `v0.8.2` release on 2026-10-30, the
  `v0.8.3` release on 2026-10-31, the `v0.9.0` release on 2026-10-31, the
  `v0.9.1` release on 2026-11-01, the `v0.9.2` release on 2026-11-02, the
  `v0.9.3` release on 2026-11-03, the `v0.10.0` and `v0.10.1` releases on
  2026-11-05, the `v0.10.2` release on 2026-11-06, the `v0.10.3` release on
  2026-11-09, the `v0.11.0` and `v0.11.1` releases on 2026-11-10, the
  `v0.12.0` language and RTL release on 2026-11-11, the `v0.12.1` release on
  2026-11-12, the `v0.13.0` release on 2026-11-12 and the `v1.0.0` release on
  2026-11-13; each release workflow uploaded the desktop and Android assets.
  See [docs/android-testing.md](docs/android-testing.md).

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
- **In-tunnel DNS** (`internal/dns`): the resolver answers `AAAA` queries
  empty (NOERROR/NODATA) without resolving or caching them, strips `AAAA`
  from `ANY` replies, and leaves IPv6 blackholed on every platform.
- **v0.6.1 sign-in/accounts**: `internal/fxa` owns direct Firefox Accounts
  OAuth (authorization URL, PKCE S256, code exchange, profile lookup and
  best-effort refresh-token destroy) alongside the existing password client
  and refresh. The engine consumes it in `internal/engine/browser.go`: an
  in-memory flow with a ten-minute deadline, no listener, and start/status/
  complete/cancel commands; `login_complete` accepts the complete HTTPS
  success redirect URL.
  Clients only open the returned URL and poll `login_status`/
  `accounts_add_status`; desktop clients paste the success-page URL, while
  Android intercepts it in the in-app WebView. A password login challenged by
  Mozilla's edge is not terminal: the engine drops the password pending state
  and returns this browser flow for the same email/account, so clients must
  accept `step: browser` from `login_password` and poll that flow. Browser
  sign-in uses the public Firefox for Android client `a2270f727f45f648` and its
  HTTPS success page;
  password sign-in keeps `5882386c6d801776`. `Tokens.ClientID` records the
  issuing client so each token refreshes through the right endpoint.
  `internal/webauth` is gone.
  `internal/engine/accounts.go` owns `accounts.json` v2 (five-account cap,
  labels, active id, `last_method`, FxA tokens, durable `reauth_required`) and
  migration from legacy `tokens.json`, which remains a synchronized rollback
  copy until logout; start removes accounts that only carry the superseded
  opaque `Browser` token. `internal/engine/rotation.go` owns opt-in
  before-connect quota rotation and the connected quota poller; it never
  rotates mid-session. `logout` best-effort destroys every stored FxA refresh
  token, then always clears local state.
- **v0.11.0 blocked-region first run**: `internal/engine/passfile.go` owns
  `pass.json` beside `tokens.json` (0600, atomic, account-bound, ten-minute
  TTL), cached-pass reuse while more than 60 seconds remain, startup cleanup,
  and deletion on logout/account switch. `internal/engine/region.go` owns
  `region_check` (six unauthenticated probes, 5 s each/20 s total) and
  `prepare` (server list → profile → pass, only the pass persisted).
  `internal/engine/connect.go` dials the control plane through the local
  SOCKS5 listener while a session is connected, direct otherwise, then retries
  once through the relay on HTTP 451; a failed connect after a prepared pass
  reports `bootstrap_required`. The reusable **Blocked region** guide is a TUI
  modal, a GUI `#/blocked-region` route, and an Android app-scoped overlay
  shared by phone and TV; `kumiho prepare [--json]` and `kumiho guide` are
  additive CLI commands. Protocol v1 stays byte-compatible.
- **v0.12.0 language and RTL**: `internal/settings` owns the `language` key
  (`system`, `en` or `fa`, default `system`), shared by the Windows GUI and
  Android. GUI strings live in `internal/gui/web/static/i18n/{en,fa}.json`;
  Android strings live in `res/values/strings.xml` and
  `res/values-fa/strings.xml`, with Vazirmatn bundled for Persian. The Android
  root in `ui/App.kt` replaces `LocalContext` with a localized configuration
  context, so it must capture and re-provide
  `LocalActivityResultRegistryOwner`; otherwise setup's VPN and notification
  launchers throw before first paint. The Settings language and theme rows are
  keyed on `LocalLayoutDirection`: Material3 caches the segmented buttons'
  directional outline, so an RTL/LTR switch must remount them or the old
  corners remain. `README.fa.md` and `docs/translations/fa.md` are part of the
  release. The TUI, CLI and logs stay English.
- **v0.9.0 region truth**: `internal/relay` is an `http.RoundTripper` that
  sends a control-plane request through the user's Worker only after HTTP 451,
  with exactly one retry, the five-host allowlist and
  `X-Kumiho-Relay-Token`; `relay.token` lives beside `tokens.json` (0600) and
  is write-only across IPC. `internal/serverlist` evaluates the Firefox
  version and a JEXL subset (`versionCompare`, `env.country`), fails open and
  marks `locked` records; the engine hides them unless `allow_locked` is on.
  Real-edge ping opens a temporary CONNECT session per candidate (4 in flight,
  10 s each), caches RTTs for 30 minutes and closes each run with `ping_done`.
  Favorites and recents stay as engine state (`state.json`) and additive
  protocol commands for older clients; no current client surfaces them.
  Fastest resolves the minimum fresh RTT once and persists the actual server.
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
  the browser flow rejects it. v0.9.0 additively adds `favorite_location`,
  `clear_recents`, `settings_set` keys `allow_locked`/`relay_url`/
  `relay_token`, `relay_test`, the `fastest` connect/select field, cached
  `latency` and `allow_locked` in the locations reply, and the `ping_done`
  event; existing envelopes stay byte-compatible.
  `ipc.DefaultTransport()` selects the
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
  `gui.toml`. The five-item rail has no separate Account page: account
  management, Add account and the session-wide sign-out are the first Settings
  section (`js/account-section.js` plus `css/account-section.css`), and the
  page-level Refresh reloads the account projection. The harness binds
  `0.0.0.0:7407` (dev only, never released).
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
  coordinator, notifications and the QS tile; `widget/` owns the two
  phone-only home-screen providers, the pure `WidgetState` reducer and the
  transparent consent activity; `engine/` is the pure-JVM client
  over the facade; `ui/` is the shell and screens; `data/` holds DataStore
  preferences and split tunneling; `crash/` the local crash log;
  `diagnostics/` the Android facts. Browser sign-in is a full-screen in-app
  WebView over `accounts.firefox.com` that intercepts the HTTPS success
  redirect and submits the complete URL through `login_complete`;
  `ui/account/AccountBrowser.kt`
  is only the validated system-browser fallback when the WebView cannot be
  created, and the waiting card offers paste and password alternatives. WebView
  cookies persist across attempts and are cleared on sign-out.
  `ui/tv/` is the additive ten-foot shell (rail, focus surfaces, TV-native
  screens, setup and account reuse); `tv/TvEnvironment` selects it and
  `tv/TvChannelPublisher` publishes the best-effort Google TV channel. There
  is no Account destination on the phone or TV: account management, Add
  account and the session-wide sign-out are the first Settings group, and the
  entitlement/quota cards are gone. Settings has 24 catalog rows (22 engine
  rows, 21 writes plus the Test relay
  action, and 2 Android-owned read-only rows); the Region group owns
  `allow_locked`, `relay_url`, the write-only `relay_token` and `relay_test`,
  and a **Blocked region** guide action outside the catalog. The app-scoped
  `GuideController`/`AppContainer.guide` overlay is shared by phone and TV and
  is opened from setup consent, Settings → Region and the dashboard notice.
  Fastest and locked are shared engine state rendered by
  `LocationTree.sections`. The app-scoped `update/UpdateManager` owns the
  launch check (24 h throttle), the consented ABI APK download, SHA-256 and
  package/version verification and the system-installer hand-off;
  `ui/update/UpdatePromptHost` is the shared phone/TV consent dialog. Debug
  builds keep the release-page check.
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
- Flags: Android phone/TV and the Windows GUI use the bundled real 4:3
  `flag-icons` assets; the TUI keeps country-flag emoji, an approved departure
  from design ban 3 (v0.5.1). The GUI dashboard keeps its native `<select>`
  with text-only option labels; a real flag sits beside the selected country
  (v0.8.0 D39).
- TV: Compose for TV type scale and remote-first focus (larger type, focus
  ring, overscan-safe frame) is an approved form-factor departure, not a
  second palette. TV assets: `tv_banner`, `ic_launcher_tv`, channel logo.
- Locations: Android phone and Android TV draw every server as its own house
  card (surface-container fill, 1 dp hairline outline, 12 dp radius, 8 dp
  gaps); the selected row adds the primary tint, a matching outline and a
  check, and the TV card keeps the focus ring as its only inner outline
  (v0.9.1).
- Approved: 2026-10-15; locations redesign approved 2026-10-29

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
- `scripts/flags/generate-android-flags.sh` renders the GUI SVGs directly at
  192×144. Size librsvg with `-width 192 -height 144`; its default 100×100
  square surface adds transparent top/bottom bands that look like padding
  inside every flag border.
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
