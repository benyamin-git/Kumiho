# Development

This is the technical companion to the [README](../README.md). User
documentation lives in [docs/](.) and the full design in [PLAN.md](../PLAN.md).

## Requirements

- Go 1.27.1 (pinned in `go.mod`).
- Node 22+ with Playwright for screenshot capture.
- Android builds: JDK 17, SDK 37.2, NDK 28.2 and gomobile; run
  `make android-setup` once.

Go dependencies are vendored under `vendor/`, so builds and tests need no
network. Android Gradle dependencies resolve from Google/Maven Central once and
then work with `--offline`.

## Build

    make build          # linux/amd64   -> dist/kumiho-linux-amd64
    make build-arm64    # linux/arm64   -> dist/kumiho-linux-arm64
    make build-windows  # windows/amd64 -> CLI + kumiho-gui.exe
    make build-gui      # windows/amd64 -> dist/kumiho-gui-windows-amd64.exe
    make build-probe    # windows/amd64 -> support probe
    make build-web      # linux/amd64   -> GUI dev harness

The Windows build needs the signed `wintun.dll` from
`third_party/wintun/amd64/` beside the binary at run time. The GUI embeds the
WebView2 loader; no extra DLL ships with it.

## Test and lint

    make lint           # gofmt check + go vet
    make test           # unit tests
    make test-integration  # real endpoints, opt-in

Every commit must leave this gate green:

    make lint && make test && go test -race -count=1 ./... && make build && \
      GOOS=windows GOARCH=amd64 go vet ./... && \
      GOOS=windows GOARCH=amd64 go build ./... && \
      GOOS=android GOARCH=arm64 go build ./...

## Android

Gradle never runs Go. Build the gomobile AAR first, then the app:

    make android-aar            # arm + arm64 AAR (git-ignored)
    make android-aar-emulator   # x86_64 AAR for tests only
    make android-debug          # AAR + debug APK
    make android-test           # JVM tests + lint + debug APK
    make android-emulator-test  # connected phone tests
    make android-tv-emulator-test  # connected Google TV tests

Signing and release verification are covered in
[android-testing.md](android-testing.md). The release workflow builds the
per-version APKs, renames them to the stable names used by README downloads and
publishes them with `SHA256SUMS`.

## Windows GUI development

    make build-web
    scripts/dev-gui-web.sh start   # http://<host>:7407/
    scripts/dev-gui-web.sh stop

The harness serves the real UI assets in a browser against the mock daemon. The
`/dev` panel drives scenarios, latency and failures. It binds `0.0.0.0:7407`
and is never part of a release.

## Screenshots

The committed images under `screenshots/` are generated from the real clients:

    scripts/screenshots/capture.sh

That script renders the Windows GUI and Linux TUI. Android phone and TV
captures come from the connected screenshot tests
(`DashboardScreenshotsTest`, `LocationsScreenshotsTest`,
`SettingsScreenshotsTest`, `TvScreenshotsTest`) and are pulled from the app's
external files directory. See
[scripts/screenshots/README.md](../scripts/screenshots/README.md).

## Architecture

- `internal/engine` is OS-free: FxA/OAuth, Guardian, server list, HTTP/2
  CONNECT, SOCKS, watchdog and exit check.
- `internal/platform` is the OS seam: Linux TUN/routing/nftables, Windows
  Wintun/WFP/route table, Android fd adoption and userspace tunnel DNS.
- `internal/ipc` is control protocol v1 (newline-delimited JSON) over a Unix
  socket or Windows named pipe.
- `internal/gui` is the Windows GUI client; `mobile/` embeds the engine for the
  Android gomobile AAR; `android/` is the Kotlin/Compose app.
- `internal/tui` is the Bubble Tea client.

The full protocol, routing, milestones and acceptance criteria are in
[PLAN.md](../PLAN.md). Repository-specific agent guidance is in
[AGENTS.md](../AGENTS.md).
