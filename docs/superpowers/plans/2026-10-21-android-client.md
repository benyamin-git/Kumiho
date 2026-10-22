# Kumiho Android Client — Implementation Plan

**Spec:** `docs/superpowers/specs/2026-10-21-android-client-design.md` (approved, commit `e2c5eea`)
**Status:** Approved (plan gate #2, 2026-10-21)
**Date:** 2026-10-21
**Scope:** v0.5.0 Android client, shared repository and release line

## How to execute this plan

- Read the approved spec first, then this plan. The spec is authoritative for
  behavior; this plan is authoritative for sequencing, files and verification.
- Load the `design` skill before any UI task and use
  `~/.config/opencode/skills/design/references/house-style.md` as the review
  checklist. Load `test-driven-development` before implementation work and
  `verification-before-completion` before every completion claim.
- Tasks are commit-sized. One commit per task, after that task's gate is green.
  Do not bundle tasks. Do not push, tag, amend published commits or open PRs
  without an explicit request.
- A task marked **STOP/GO** must not be worked around. If it fails, stop and
  report the evidence; the fallback requires a re-plan, not a local hack.
- Mark `[user]` checks explicitly. Those need the physical phone, GitHub
  secrets, or a push/tag the agent must not perform alone.
- No desktop behavior change. `make lint && make test`, the race suite, the
  Windows cross-build and the existing Android `arm64` cross-build stay green
  after every Go change.
- The generated AAR, signing material, local SDK paths and `.ai-state` never
  enter a commit.

## Pre-flight clarifications (resolved here, none open)

The following are implementation details needed to make the approved decisions
coherent. They do not change the product scope. **C1 was confirmed at plan gate
#2**; the rest follow directly from the approved spec.

- **C1 — the tunnel DNS address is never excluded (confirmed at gate #2).**
  `allow_lan` (default on) excludes RFC1918, which includes `10.8.0.2`, the
  engine's in-tunnel DNS listener. Excluding it can black-hole DNS. The route
  helper therefore subtracts the reserved host prefix `10.8.0.2/32` from every
  exclusion set before applying `allow_lan` or `exclude_cidrs`. The tunnel DNS
  remains inside the VPN; every other LAN destination keeps the behavior in
  D49. This is tested in Go, in the Kotlin interface-spec tests, and on the
  first real connect. `exclude_cidrs` containing `10.8.0.2/32` is accepted but
  that host remains routed inside, with a Settings hint.
- **C2 — setup is the four steps in D21:** consent, Firefox Accounts sign-in,
  Android VPN + notification permissions, optional Always-on guidance. There is
  no separate health-check step; Diagnostics is always available after setup.
- **C3 — release ABIs stay arm-only.** The release AAR is `android/arm` +
  `android/arm64`. A debug/emulator AAR may contain `android/amd64` solely so
  Compose and the AAR probe can run on the CI x86_64 emulator. No x86/x86_64
  library ships in any release artifact.
- **C4 — the AAR probe covers load, callback and fd adoption off-device.** The
  real VpnService establish → DNS bind → browse check is the first-connect gate
  in T11, because it needs a live signed-in session and a real interface. Both
  are stop/go gates.
- **C5 — IPv6 is dropped by the Android fd wrapper, not by the engine.** The
  `internal/platform/android` provider wraps the adopted fd in an `ipv6Filter`
  that discards IPv6 reads before gVisor sees them. This implements D17/A7 and
  keeps A18 (no engine change).
- **C6 — the service owns the session; the app-scoped coordinator owns the
  binding.** `AppContainer` creates `VpnServiceCoordinator`; it binds to
  `KumihoVpnService` with `BIND_AUTO_CREATE`. The service creates the
  `EngineClient`/`mobile` session. The UI observes the binder's `StateFlow` and
  never creates a session. This matches D8/A4 while allowing setup and settings
  before a tunnel exists.
- **C7 — documentation names are exactly the spec's:** `docs/android.md` and
  `docs/android-testing.md`, plus README/AGENTS/CHANGELOG/NOTICE updates.

## Global constraints (apply to every task)

### Go and desktop

- Go 1.27.1 (`go.mod`). Dependencies stay vendored; no new Go dependency and no
  `vendor/` change. `golang.org/x/mobile` is installed as a build tool, not
  added to `go.mod`.
- Existing gate after every Go-touching task:

      make lint && make test && go test -race -count=1 ./... && make build && \
        GOOS=windows GOARCH=amd64 go vet ./... && \
        GOOS=windows GOARCH=amd64 go build ./... && \
        GOOS=android GOARCH=arm64 go build ./...

- Android 32-bit additions are checked with
  `GOOS=android GOARCH=arm go build ./internal/...` and by the AAR build; the
  full `GOOS=android GOARCH=arm go build ./...` cannot link the desktop `main`
  packages without cgo/external linking, so it is not used as a gate.
- `internal/ipc` protocol v1 is byte-identical. No envelope, command or existing
  field changes. New Android vocabulary lives only in `internal/mobile`.
- Desktop daemon/CLI/TUI/GUI behavior is untouched. The only existing Go file
  whose public surface changes is `internal/gui/theme` (additive `Palette`).
- Commit style: `type: lowercase imperative summary`; types in use: `feat`,
  `fix`, `refactor`, `test`, `docs`, `ci`, `chore`.

### Android build

- `android/` is the Gradle root; `android/app` is the only module.
- AGP 9.4.1, Gradle 9.6.0, Kotlin 2.4.21, Compose BOM 2026.09.00, Navigation
  2.10.2, Lifecycle 2.11.0, JDK 17, NDK `28.2.13676358`, compile/target SDK 36,
  min SDK 26.
- AGP 9 uses built-in Kotlin: **do not apply `org.jetbrains.kotlin.android`**.
  Pin KGP 2.4.21 through the root buildscript classpath and apply
  `org.jetbrains.kotlin.plugin.compose` 2.4.21.
- App ID `dev.benyamin.kumiho`, label `Kumiho`.
- Release ABIs: `arm64-v8a` and `armeabi-v7a`, plus one universal APK. The
  universal APK must be ≤60 MB. x86/x86_64 is debug/test-only.
- AAR output goes to `android/app/libs/` and is git-ignored. Gradle never
  downloads or builds Go; `scripts/android-build-aar.sh` does.
- No secrets in the repository. `android/keystore.properties` is local and
  ignored.

### UI and privacy

- House design only: six accents (blue default), Light/Dark/OLED, no Material
  You/dynamic color, system Roboto only, 12/14/16/20/24/32 type scale, 4dp
  spacing, 48dp touch targets, logical start/end, visible focus, no color-only
  meaning, one accent per screen, no raw hex in composables, no decorative
  gradients/blur/shadows/emoji/stock art.
- First run follows system Light/Dark; OLED is opt-in. No first-paint flash.
- Kill switch is displayed as Android Always-on/lockdown and is read-only.
  Autoconnect is displayed as Always-on. Never call `ctrl.Autoconnect`.
- IPv6 is captured and dropped. DNS is `10.8.0.2:53` in the tunnel. Default MTU
  is 1500.
- Tokens in `noBackupFilesDir`, `allowBackup=false`, no telemetry, no remote
  crash upload, local crash file only.
- No FoxyVPN code is copied. AndroidX/Compose/Kotlin/gomobile licenses go into
  `NOTICE`.

## Task map

| Task | Commit | Depends on | Stop/go |
|---|---|---|---|
| T1 toolchain | `chore: add the android toolchain setup` | — | check |
| T2 Gradle skeleton | `feat: scaffold the android gradle app` | T1 | — |
| T3 palette map | `feat: expose the house palette as a map` | — | — |
| T4 Android provider | `feat: implement the android platform provider` | T1 | — |
| T5 mobile facade | `feat: add the mobile engine facade` | T3, T4 | — |
| T6 AAR + probe | `feat: build and probe the gomobile aar` | T2, T5 | **STOP/GO** |
| T7 shell + theme | `feat: add the android shell and house theme` | T2, T3, T6 | — |
| T8 engine client | `feat: add the android engine client` | T6, T7 | — |
| T9 VpnService | `feat: add the android vpn service` | T7, T8 | — |
| T10 setup | `feat: add the android setup flow` | T9 | — |
| T11 dashboard + first connect | `feat: add the android dashboard` | T9, T10 | **STOP/GO** |
| T12 locations | `feat: add android locations` | T11 | — |
| T13 settings | `feat: add android settings` | T11, T12 | — |
| T14 split tunneling | `feat: add android split tunneling` | T9, T13 | — |
| T15 account/logs/diagnostics | `feat: add android account logs and diagnostics` | T11, T12, T13 | — |
| T16 tile/notification/proxy-only | `feat: add the android quick settings tile` | T9, T11, T15 | — |
| T17 build/CI/release | `ci: build and release the android app` | T16 | check |
| T18 docs + acceptance | `docs: document the android client` | T17 | **STOP/GO** |

T3 and T4 may run in parallel after T1; T2 may run in parallel with T3/T4.
Everything from T6 on is sequential unless explicitly noted. T11 uses the saved
or REC selection plus a compact country/city picker backed by the engine's
`locations` command; it does not depend on the full Locations screen. T12
lands immediately after and reuses the same data. There is no dependency cycle.

---

## T1 — Toolchain setup

**Why:** the dev box has only Go today. No code task can start until JDK 17,
SDK 36, NDK 28.2, Gradle wrapper bootstrap and gomobile are present.

**Files**

- new `scripts/android-env.sh`
- new `scripts/android-setup.sh`
- modify `Makefile` (add `android-setup`; no other target yet)

**Behavior**

- `android-env.sh` is source-only and exports:
  - `ANDROID_HOME=${ANDROID_HOME:-$HOME/Android/Sdk}`
  - `ANDROID_SDK_ROOT=$ANDROID_HOME`
  - `ANDROID_NDK_HOME=$ANDROID_HOME/ndk/28.2.13676358`
  - `JAVA_HOME=${JAVA_HOME:-/usr/lib/jvm/java-17-openjdk-amd64}`
  - `PATH` with JDK, `cmdline-tools/latest/bin`, `platform-tools`, `emulator`,
    `$HOME/go/bin`.
- `android-setup.sh` is idempotent and supports `--check` and `--ci`:
  - verify `java -version` is 17; install `openjdk-17-jdk-headless` only when
    absent (Linux/apt path).
  - install Google command-line tools under `$ANDROID_HOME/cmdline-tools/latest`
    if `sdkmanager` is absent. Pin the zip URL and SHA-256 in the script;
    verify the checksum before unzipping.
  - accept licenses and install `platform-tools`, `platforms;android-36`,
    `build-tools;36.0.0`, `ndk;28.2.13676358`, `emulator`, and
    `system-images;android-36;google_apis;x86_64`.
  - install the pinned gomobile:
    `go install golang.org/x/mobile/cmd/gomobile@v0.0.0-20261011034528-197770444867`
    then `gomobile init`.
  - bootstrap Gradle 9.6.0 into a local tools directory only when the wrapper
    does not exist yet; T2 generates and commits the wrapper.
  - `--check` prints the resolved versions and exits non-zero on any mismatch.
  - `--ci` skips interactive prompts and apt installs already present.
- `make android-setup` runs `bash scripts/android-setup.sh`.

**Verification**

    bash scripts/android-setup.sh --check
    source scripts/android-env.sh
    java -version 2>&1 | grep '17\.'
    sdkmanager --list_installed | grep -E 'platforms;android-36|build-tools;36.0.0|ndk;28.2.13676358'
    gomobile version
    adb version

Expected: all five checks succeed; `make lint && make test` still passes; no
tracked file other than the three above changes.

**Commit:** `chore: add the android toolchain setup`

---

## T2 — Gradle skeleton

**Files**

- new `android/settings.gradle.kts`
- new `android/build.gradle.kts`
- new `android/gradle.properties`
- new `android/gradle/libs.versions.toml`
- new `android/gradle/wrapper/gradle-wrapper.properties`
- new `android/gradle/wrapper/gradle-wrapper.jar`
- new `android/gradlew`, `android/gradlew.bat`
- new `android/app/build.gradle.kts`
- new `android/app/proguard-rules.pro`
- new `android/app/src/main/AndroidManifest.xml`
- new `android/app/src/main/java/dev/benyamin/kumiho/KumihoApplication.kt`
- new `android/app/src/main/java/dev/benyamin/kumiho/MainActivity.kt`
- new `android/app/src/main/res/values/strings.xml`
- new `android/app/src/main/res/values/themes.xml`
- new `android/app/src/main/res/values/colors.xml`
- new `android/app/src/main/res/xml/backup_rules.xml`
- new `android/app/src/main/res/xml/data_extraction_rules.xml`
- new `android/.gitignore`
- modify root `.gitignore`

**Build configuration**

- `settings.gradle.kts`: `pluginManagement` with `google()` and
  `mavenCentral()`; `dependencyResolutionManagement` with `FAIL_ON_PROJECT_REPOS`;
  root name `KumihoAndroid`; include `:app`.
- Root `build.gradle.kts`: buildscript classpath pins
  `org.jetbrains.kotlin:kotlin-gradle-plugin:2.4.21`; plugins
  `com.android.application` 9.4.1 and
  `org.jetbrains.kotlin.plugin.compose` 2.4.21, both `apply false`.
- `libs.versions.toml` pins:

      agp = "9.4.1"
      kotlin = "2.4.21"
      composeBom = "2026.09.00"
      coreKtx = "1.19.1"
      activityCompose = "1.13.0"
      lifecycle = "2.11.0"
      navigation = "2.10.2"
      datastore = "1.2.1"
      splashscreen = "1.2.0"
      coroutines = "1.11.0"
      junit = "4.13.2"
      androidxTestJunit = "1.3.0"
      espresso = "3.7.0"
      testRunner = "1.7.0"
      testCore = "1.7.0"
      json = "20260814"

- `app/build.gradle.kts`:
  - `namespace = "dev.benyamin.kumiho"`, `applicationId` the same,
    `minSdk = 26`, `targetSdk = 36`, `compileSdk = 36`.
  - `versionName` from `-PkumihoVersion` (default `0.5.0`);
    `versionCode = major*10000 + minor*100 + patch` (v0.5.0 → 500).
  - debug `applicationIdSuffix = ".debug"` and `versionNameSuffix = "-debug"`.
  - release `isMinifyEnabled = true`, `isShrinkResources = true`, proguard
    rules; signing config only when `android/keystore.properties` exists.
  - ABI splits for release: `arm64-v8a`, `armeabi-v7a`, `isUniversalApk = true`.
    Splits are disabled when `-PkumihoEmulator=true` so the debug APK can carry
    the x86_64 emulator AAR.
  - `compileOptions` and Kotlin `jvmTarget` 17.
  - `buildFeatures { compose = true; buildConfig = true }`.
  - `packaging { jniLibs { useLegacyPackaging = false } }`.
  - Dependencies: Compose BOM (ui, ui-graphics, ui-tooling-preview,
    material3, material-icons-core), activity-compose, core-ktx,
    core-splashscreen, lifecycle-runtime-compose, lifecycle-viewmodel-compose,
    lifecycle-runtime-ktx, navigation-compose, datastore-preferences,
    coroutines-android; test: junit, androidx test ext junit, espresso, runner,
    core, coroutines-test, `org.json:json`; debug: ui-tooling, ui-test-manifest.
- `gradle.properties`: AndroidX, non-transitive R, parallel, configuration
  cache, 4 GB heap, official Kotlin style.
- Manifest: `android:name=".KumihoApplication"`, theme `@style/Theme.Kumiho`,
  label `@string/app_name`, `allowBackup=false`, `fullBackupContent=false`,
  `dataExtractionRules`, `supportsRtl=true`,
  `enableOnBackInvokedCallback=true`, `usesCleartextTraffic=false`;
  `MainActivity` exported with MAIN/LAUNCHER. Declare `INTERNET`, `ACCESS_NETWORK_STATE`,
  `POST_NOTIFICATIONS`, `FOREGROUND_SERVICE`,
  `FOREGROUND_SERVICE_SPECIAL_USE`, and `QUERY_ALL_PACKAGES` now. The service
  and tile are declared in T9/T16.
- Backup/extraction XML excludes the whole app data root for cloud and device
  transfer.
- `KumihoApplication` is minimal for now (crash handler is added in T15);
  `MainActivity` renders a placeholder `Text("Kumiho")` with edge-to-edge and a
  `Theme.Kumiho` parent `Theme.Material.NoActionBar`; no Material You colors.
- Add ignored paths: `android/app/libs/*.aar`, `android/.gradle/`,
  `android/.kotlin/`, `android/build/`, `android/app/build/`,
  `android/local.properties`, `android/keystore.properties`, `*.jks`,
  `*.keystore`, `android/captures/`, `.idea/`, `*.iml`.

**Verification**

    source scripts/android-env.sh
    cd android
    ./gradlew --version
    ./gradlew :app:assembleDebug :app:testDebugUnitTest :app:lintDebug
    $ANDROID_HOME/build-tools/36.0.0/aapt2 dump badging \
      app/build/outputs/apk/debug/app-debug.apk | head -5

Expected: Gradle 9.6.0; package `dev.benyamin.kumiho.debug`, minSdk 26,
targetSdk 36; no signing secrets; `flutter`/IDE files absent. Then the full Go
gate.

**Commit:** `feat: scaffold the android gradle app`

---

## T3 — Palette bridge

**Files**

- modify `internal/gui/theme/palette.go`
- modify `internal/gui/theme/theme_test.go` (or add `palette_test.go`)

**Interface**

```go
// Palette returns the validated semantic roles for a theme and accent.
// Unknown themes/accents are errors. The map is a copy; callers may mutate it.
func Palette(t Theme, accent string) (map[string]string, error)
```

- Validate through the existing `neutrals` / `accentByName` rules.
- Return a copy of `values(theme, accent)`; keys are the existing
  custom-property names without the `--` prefix (`background`, `on-surface`,
  `primary`, `surface-container-1`, `profit`, `loss`, `warning`, …).
- Refactor `CSS` to call `Palette` and render `cssOrder` from the map. CSS
  output, ordering, `color-scheme`, and error text must be byte-identical.

**Tests**

- Every theme × accent returns the exact key set and expected role values
  (table-driven; spot-check primary, background, container ladder, status).
- Invalid theme/accent errors.
- Returned map is a copy (mutating it does not affect another call).
- Existing CSS golden strings remain unchanged; run
  `go test ./internal/gui/... ./internal/gui/theme/`.

**Gate:** the full Go gate. **Commit:** `feat: expose the house palette as a map`

---

## T4 — Real Android platform provider

**Files**

- replace `internal/platform/android/provider.go`
- modify `internal/platform/android/doc.go`
- new `internal/platform/android/device.go`
- new `internal/platform/android/device_test.go`
- new `internal/platform/android/adopt_linux.go` (`//go:build linux || android`)
- new `internal/platform/android/adopt_other.go` (`//go:build !linux && !android`)
- new `internal/platform/android/adopt_linux_test.go` (`//go:build linux`)
- new `internal/platform/android/netconfig.go`
- new `internal/platform/android/provider_android_test.go` (`//go:build android`)
- new `internal/platform/android/paths.go` (placeholder paths for `New()`)

**Interfaces**

```go
// Bridge is the Kotlin side of the VpnService handshake. The gomobile
// DeviceBridge interface satisfies it.
type Bridge interface {
    AcquireFD(mtu int) (int, error)
    NetworkAvailable() bool
}

type Provider struct { /* name, paths, bridge */ }

func New() *Provider
func NewWithPaths(paths settings.Paths, bridge Bridge) *Provider
```

- `Name()` = `"android"`, `UserAgent()` = `fxa.UserAgent`,
  `TunDefaults()` = zero, `DNSListenAddr()` = `"10.8.0.2:53"`,
  `BypassDialer()` = plain `&net.Dialer{}` (Kumiho is excluded from its own VPN),
  `Paths()` = passed paths, `ControlEndpoint()` = `filepath.Join(paths.Runtime,
  "control.sock")`, `ChownControlEndpoint` no-op.
- `OpenDevice(ctx, spec)`:
  - if `spec.FD >= 0`, adopt that fd (test/probe path);
  - else require `bridge != nil`, call `bridge.AcquireFD(spec.MTU)`, adopt the
    returned fd;
  - wrap in `ipv6Filter`, return `Device` with `Name()` = spec.Name (or `"tun"`
    when empty).
- `adoptFD(fd, name)` (Linux/Android):
  - validate fd ≥ 0;
  - `unix.SetNonblock(fd, true)`;
  - `os.NewFile(uintptr(fd), name)`;
  - on any error close the fd;
  - return the filtered device.
- `ipv6Filter`:
  - wraps `io.ReadWriteCloser` + name;
  - `Read` loops: read one packet; if `n > 0 && p[0]>>4 == 6`, discard and
    read again; otherwise return the packet;
  - if read returns an error with data, return the data first on the next call?
    Keep the simple contract: discard only when `n > 0` and the packet is IPv6;
    return `(n, err)` otherwise;
  - `Write`, `Close`, `Name` delegate.
- `NetConfig` is a no-op: `Apply` returns nil, `Cleanup` does nothing, with a
  doc comment explaining the Kotlin `VpnService.Builder` already applied
  addresses/routes/DNS/split/exclusions.
- `WatchLinks` emits on a 2 s ticker; `HasDefaultRoute` delegates to
  `bridge.NetworkAvailable()` (true when no bridge, e.g. proxy-only/tests).
- Non-Android compilation must still work: `filter.go`/`device.go` are
  portable; `adopt_other.go` is a stub; `provider.go` stays Android-tagged.
  `New()` uses placeholder paths only for the compile-only daemon; the mobile
  facade always calls `NewWithPaths`.

**Tests**

- `ipv6Filter`: scripted reader returns `v6, v4, v6, v4`; Read yields only the
  two v4 packets; `Write`/`Close`/`Name` delegate; error propagation.
- `adoptFD` on Linux with `unix.Pipe2`: write a v6 packet then a v4 packet,
  read returns the v4; closing the device closes the read end.
- Android-tagged compile test asserts DNS address, zero `TunDefaults`, no-op
  NetConfig accepts a spec, nil bridge errors on `OpenDevice` when fd < 0.
- Non-Android `go test ./...` and `GOOS=windows` builds still pass.

**Gate:** the full Go gate, plus
`GOOS=android GOARCH=arm go build ./internal/platform/android`. **Commit:**
`feat: implement the android platform provider`

---

## T5 — Mobile engine facade

**Files**

- new `internal/mobile/doc.go`
- new `internal/mobile/mobile.go` (package API and singleton)
- new `internal/mobile/facade.go`
- new `internal/mobile/commands.go`
- new `internal/mobile/bridge.go` (interfaces, paths, envelopes)
- new `internal/mobile/paths.go`
- new `internal/mobile/routes.go`
- new `internal/mobile/routes_test.go`
- new `internal/mobile/logfile.go`
- new `internal/mobile/logfile_test.go`
- new `internal/mobile/doctor.go`
- new `internal/mobile/doctor_test.go`
- new `internal/mobile/provider_android.go` (`//go:build android`)
- new `internal/mobile/provider_other.go` (`//go:build !android`)
- new `internal/mobile/session_test.go`
- new `internal/mobile/commands_test.go`
- new `internal/mobile/palette_test.go`

**Exact package API** (gomobile generates static `Mobile.*` methods)

```go
type DeviceBridge interface {
    Configure(configJSON string) error
    AcquireFD(mtu int) (int, error)
    NetworkAvailable() bool
}

type Listener interface {
    OnStatus(statusJSON string)
    OnLog(entryJSON string)
    OnPing(resultJSON string)
    OnNotice(noticeJSON string)
}

func Start(pathsJSON string, bridge DeviceBridge, listener Listener) error
func Command(requestJSON string) (string, error)
func Palette(theme, accent string) (string, error)
func Version() string
func Close() error
```

- One process-wide facade guarded by an `RWMutex`; `Command` holds a read lock,
  `Close` takes the write lock, cancels the root context and calls
  `ctrl.Shutdown`. `Start` after `Close` creates a fresh facade.
- `Palette` and `Version` do not require `Start`.
- `Command` returns a JSON envelope and a Go error only for facade failures
  (not started, malformed request, panic). Domain failures are encoded as
  `{"error":{"code":"...","message":"..."}}` with a nil Go error, so Kotlin
  keeps the code. Panics are recovered into an `internal` error and a log line.
- Request envelope `{ "type": "...", "payload": {...} }`; responses
  `{ "type": "...", "payload": {...} }` or the error form.

**Start behavior**

- Decode paths JSON:
  `{ "config", "state", "tokens", "runtime", "logs" }`; all must be absolute
  and non-empty; create dirs 0700. A nil listener is an error; a nil bridge is
  allowed for proxy-only and tests, but full-tunnel connect then fails clearly.
- If `config` does not exist, write `mtu = 1500\n` (0600) so Android's default
  is 1500. Do not overwrite an existing config.
- `settings.Open`; create a `logging.Ring` with capacity 2000, the configured
  sanitizer and min level, and a rotating file sink at
  `logs/kumiho.log` (3 × 1 MB, 0600). Keep the file handle for `Close`.
- Provider: Android build uses
  `android.NewWithPaths(paths.Paths, bridge)`; host build uses
  `unsupported.New("unsupported", paths.Paths)` for tests.
- `engine.New(store, ring, fxa.NewClient(), provider)`, `ctrl.Start(ctx)`,
  `ctrl.UseBypassDialer()`, `ctrl.OnChange(pushStatus)`, then push one initial
  status. Do not call `Autoconnect`.
- Start the coalesced status pump; it marshals `ctrl.Snapshot()` and calls
  `listener.OnStatus`. Never block the engine's `OnChange` callback on Kotlin.
- `Start` is idempotent only in the sense that a second call returns a clear
  "already started" error; it does not replace a live session.

**Command table**

| Type | Payload | Behavior | Timeout |
|---|---|---|---|
| `hello` | — | version, protocol 1, OS/arch | sync |
| `status` | — | `ctrl.Snapshot()`; also push to listener | sync |
| `locations` | `{refresh}` | `ctrl.Locations(ctx, refresh)` | 45 s |
| `select_location` | `{country_code,city_code,server}` | validate + persist; push status | sync |
| `ping` | `{hosts}` | ack immediately; 24-way `serverlist.PingTCP`; `OnPing` per host | per-host 10 s |
| `connect` | `{location_code,city_code,server,proxy_only}` | full: `bridge.Configure(networkJSON)` then `ctrl.Connect`; proxy-only: skip Configure | 120 s |
| `disconnect` | — | `ctrl.Disconnect()` | 20 s |
| `settings_get` | — | `ctrl.Settings()` | sync |
| `settings_set` | `{key,value,reset}` | `ctrl.ApplySetting`; push status | 20 s |
| `account` | — | `ctrl.Account(ctx)` | 35 s |
| `login_email` | `{email}` | `ctrl.LoginEmail` | sync |
| `login_password` | `{password}` | `ctrl.LoginPassword` | 60 s |
| `login_2fa` | `{code}` | `ctrl.Login2FA` (empty = re-check email link) | 60 s |
| `logout` | — | `ctrl.Logout`; push status | sync |
| `logs_snapshot` | `{level}` | filtered `ring.Snapshot()` (default debug) | sync |
| `logs_subscribe` | `{level}` | replace subscription; pump `OnLog` | sync |
| `logs_unsubscribe` | — | cancel subscription | sync |
| `logs_clear` | — | `ring.Clear()` + truncate/remove log files | sync |
| `log_append` | `{level,tag,msg}` | replay a local crash entry into the ring/file | sync |
| `reset_state` | — | remove `serverlist.json`, clear `State.Failures`, clear logs; never tokens | 10 s |
| `doctor` | `{facts}` | mix Android facts with cross-platform doctor checks | 30 s |
| `revoke` | — | `ctrl.Shutdown()` (VpnService revoked; final disconnect) | 10 s |
| unknown | — | `bad_request` | sync |

- `connect` network JSON:
  `{ "mtu", "allow_lan", "exclude_cidrs", "ipv4_excludes", "ipv4_routes" }`.
  It is built from `store.EffectiveConfig()` and the D49 rules plus C1.
- `ping` uses `context.WithTimeout(root, 10*time.Second)` per host; the command
  response is the ack, events may follow. Cap hosts at 512 to bound abuse.
- `logs_subscribe` level parsing uses `logging.ParseLevel`; an invalid level is
  `bad_request`. The pump exits on cancel/Close.
- `doctor` is described in its own subsection below.
- `reset_state` uses `store.UpdateState` to set `Failures = nil`; it does not
  clear `SelectedLocation`, `Email`, tokens, or overrides.
- `revoke` may race a connect; the root context cancellation and engine lock
  handle it. The service also stops foreground.
- `OnNotice` is emitted on a background transition from authenticated to
  unauthenticated and on a FATAL transition with the latest log message; it is
  advisory, never the source of truth.

**Route helper (`routes.go`)**

- Parse `allow_lan` and `exclude_cidrs` as IPv4 prefixes; ignore IPv6 prefixes
  with a debug log (the Settings UI rejects them first).
- Base exclusions when `allow_lan`: `10.0.0.0/8`, `172.16.0.0/12`,
  `192.168.0.0/16`, `169.254.0.0/16`, `224.0.0.0/4`.
- Add user CIDRs; de-duplicate and merge adjacent/covered prefixes.
- Reject `0.0.0.0/0`.
- **C1:** subtract `10.8.0.2/32` from the exclusion set, splitting the
  containing prefix as needed, so the DNS listener is never excluded.
- `ipv4_excludes` = the sanitized exclusion list (for API 33+
  `excludeRoute`); `ipv4_routes` = the complement of `0.0.0.0/0` minus the
  exclusions (for API 26–32 `addRoute`).
- Cap each list at 256; return a `bad_request` "too many route exclusions"
  error when exceeded. IPv6 is never in either list.
- If no exclusions remain, `ipv4_excludes` is empty and `ipv4_routes` is
  `["0.0.0.0/0"]`.

**Doctor (`doctor.go`)**

- Decode `facts`:
  `{device,vpn_permission,always_on,lockdown,notifications,battery_optimized,
  network,dns_servers,private_dns_mode,socks_port_listening,
  http_proxy_port_listening,service_running,engine_state,sdk}`.
- Call `doctor.Run()` for the cross-platform probes, then drop `platform` and
  `socks-port` (the engine owns the port and the Android platform is real).
  Keep `clock-skew` and `endpoints`, rewriting their remediation text for
  Android (automatic date/time; connectivity/Private DNS).
- Append checks:
  - `vpn-permission` — fail/warn when `VpnService.prepare` would prompt.
  - `always-on` — OK when on; warn with the system settings path when off.
  - `lockdown` — OK with an explanatory detail; never claim enforcement.
  - `notifications` — fail when disabled on API 33+ (remediation: app
    notification settings).
  - `battery-optimization` — warn when optimized; remediation: unrestricted.
  - `network` — fail when no validated non-VPN network.
  - `dns` — when connected, require `10.8.0.2` in `dns_servers`; warn on
    `private_dns_mode=hostname`; skip when not connected.
  - `local-ports` — when connected, require the SOCKS listener and, if
    `http_proxy` is on, the HTTP listener; skip when not connected.
  - `engine-state` — OK with the state; fail if `service_running` is false
    while connected.
- Build `doctor.Report` with `Version`, `Host` from `device`, `OS` =
  `android/<GOARCH>`, `Time`, checks, and `doctor.Summarize` counts.

**Log file**

- `rotatingWriter` opens `logs/kumiho.log` append 0600, rotates at 1 MB:
  `kumiho.log` → `kumiho.log.1` → `kumiho.log.2`, deleting the oldest.
- `Reset` truncates current and removes rotations.
- Thread-safe; errors are returned to the ring (which logs them? avoid loops —
  swallow sink errors after one warn).

**Tests**

- `Start` writes `mtu=1500` only when absent; refuses bad paths; `Close`
  idempotent; `Start` after `Close` works; `Command` before `Start` errors.
- `status`, `settings_get`, `hello`, unknown type, malformed JSON, panic
  recovery, concurrent commands vs Close.
- Route table tests: no exclusions; allow_lan; user CIDR; overlapping/adjacent;
  `0/0` rejection; IPv6 ignored; C1 10.8.0.2 never excluded; complement
  expected set; 256 cap; DNS host present in routes.
- Log rotation: 2.5 MB write yields three files, no more; reset removes.
- Doctor facts table: each check status; missing facts skip; no panic.
- Palette function returns valid JSON object.
- `go test -race ./internal/mobile` and the full Go gate.

**Commit:** `feat: add the mobile engine facade`

---

## T6 — AAR build and probe (STOP/GO)

**Files**

- new `scripts/android-build-aar.sh`
- modify `Makefile` (add `android-aar`, `android-aar-emulator`)
- modify `android/app/build.gradle.kts` (AAR dependency selection)
- modify `android/app/proguard-rules.pro`
- new `android/app/src/androidTest/java/dev/benyamin/kumiho/MobileProbeTest.kt`
- modify `android/.gitignore` if needed

**Script**

- `scripts/android-build-aar.sh` sources `android-env.sh`.
- Default target `android/arm,android/arm64`; output
  `android/app/libs/kumiho.aar`.
- `--emulator` target `android/amd64`; output
  `android/app/libs/kumiho-emulator.aar`.
- Always `-androidapi 26`, `-javapkg=dev.benyamin.kumiho.go`,
  `-ldflags` with version/commit/date, `-trimpath` where supported.
- After the bind: `unzip -l` must show `classes.jar`,
  `jni/arm64-v8a/libgojni.so` and `jni/armeabi-v7a/libgojni.so` (or
  `jni/x86_64/libgojni.so` for the emulator AAR). Check arm64 `p_align` with
  the NDK `llvm-readelf`; it must be at least `0x4000`.
- Never commit the AAR.

**Gradle**

```kotlin
val emulator = providers.gradleProperty("kumihoEmulator")
    .orNull?.toBoolean() == true
implementation(files("libs/" + if (emulator) "kumiho-emulator.aar" else "kumiho.aar"))
```

- When `kumihoEmulator=true`, disable ABI splits so the debug APK includes the
  x86_64 native library. Release builds never set it.
- ProGuard keeps the generated package and JNI/callback members:

      -keep class dev.benyamin.kumiho.go.** { *; }
      -keep class * implements dev.benyamin.kumiho.go.DeviceBridge { *; }
      -keep class * implements dev.benyamin.kumiho.go.Listener { *; }
      -keepclasseswithmembers,includedescriptorclasses class * { native <methods>; }

**Probe test**

`MobileProbeTest` runs as an instrumented test and asserts:

1. `Mobile.version()` is non-empty.
2. `Mobile.palette("dark", "blue")` returns JSON containing `"primary"` and
   `"on-surface"`.
3. `Mobile.start(pathsJson, bridge, listener)` succeeds with temp app-private
   paths and a fake `DeviceBridge` that records calls; `Mobile.command` with a
   `status` request returns a `status` envelope.
4. The `Listener.onStatus` callback fires (latch) after the status command; a
   Kotlin→Go→Kotlin round trip works.
5. `Mobile.close()` succeeds and a second close is harmless.

The fake bridge's `AcquireFD` throws "not used"; the probe does not connect. Fd
adoption is covered by the Go pipe test in T4. The real establish/DNS check is
T11.

**Verification**

    source scripts/android-env.sh
    scripts/android-build-aar.sh --emulator
    cd android
    ./gradlew -PkumihoEmulator=true :app:assembleDebug :app:assembleDebugAndroidTest
    ./gradlew -PkumihoEmulator=true :app:connectedDebugAndroidTest \
      -Pandroid.testInstrumentationRunnerArguments.class=dev.benyamin.kumiho.MobileProbeTest
    cd ..
    scripts/android-build-aar.sh
    unzip -l android/app/libs/kumiho.aar | grep -E 'classes.jar|libgojni.so'

- If no emulator is available, the compile/install step is `[user]`; the AAR
  contents and readelf checks still run locally.
- **STOP/GO:** if the AAR does not load, the static methods are missing, the
  callback does not fire, or the fd/pipe probe fails, stop. Report the
  `gomobile`/NDK output and the fallback options (hand-written JNI/shared
  library or separate process) for a re-plan. Do not start T7.

**Commit:** `feat: build and probe the gomobile aar`

---

## T7 — Compose shell, theme and icons

**Files**

- new `android/app/src/main/java/dev/benyamin/kumiho/ui/theme/KumihoTheme.kt`
- new `android/app/src/main/java/dev/benyamin/kumiho/ui/theme/KumihoColors.kt`
- new `android/app/src/main/java/dev/benyamin/kumiho/ui/theme/Type.kt`
- new `android/app/src/main/java/dev/benyamin/kumiho/ui/theme/Motion.kt`
- new `android/app/src/main/java/dev/benyamin/kumiho/ui/theme/Dimens.kt`
- new `.../ui/shell/AppShell.kt`, `Destination.kt`, `TopBar.kt`, `MoreSheet.kt`
- new `.../ui/components/` shared components (power button, cards, rows,
  setting row, chips, empty/error states, quota bar, segmented control)
- new `.../ui/screens/PlaceholderScreen.kt`
- new `.../data/AppPrefs.kt`
- new `.../data/ThemeRepository.kt`
- new `.../ui/App.kt`
- modify `MainActivity.kt` and `KumihoApplication.kt`
- new icon resources under `android/app/src/main/res/` (adaptive icon,
  foreground/monochrome PNGs, `values/colors.xml`, `mipmap-anydpi-v26/`)
- new `android/app/src/main/res/values-night/themes.xml` (dark splash surface,
  so first paint never flashes light)
- new `.../ui/theme/PaletteJsonTest.kt`
- new `.../ui/shell/ShellNavigationTest.kt`

**Palette and theme**

- `ThemeRepository` reads DataStore (`theme`, `accent`); empty theme defaults
  from `Configuration.uiMode` (night → Dark, else Light), never OLED; empty
  accent defaults blue. It calls `Mobile.palette(theme, accent)` on
  `Dispatchers.Default` and caches the result. Errors render an explicit
  palette-error surface; never fall back to raw colors.
- `KumihoColors` contains every role used by Compose, including status
  `profit/loss/warning`. Build a full `ColorScheme` constructor (no default
  Material purple), deriving only role-based container variants where the
  house map has no standalone token (`errorContainer = loss.copy(alpha=0.16f)`,
  `outlineVariant = outline.copy(alpha=0.5f)`, surfaces from the container
  ladder). No hex literals in composables.
- `Typography` uses `FontFamily.Default` only and the 12/14/16/20/24/32 scale.
  `Shapes` uses 6 (chips/inputs), 8 (buttons), 12 (cards), 16
  (dialogs/sheets), full pills.
- `Motion` exposes an expressive spring for the power control and restrained
  springs/tweens elsewhere. `ValueAnimator.areAnimatorsEnabled()` (or
  `ANIMATOR_DURATION_SCALE == 0`) selects snap transitions. Honor system
  reduced motion.
- First paint: use the splash screen theme, load the stored theme with
  `DataStore.first()` and compute the palette before rendering the shell; do
  not render a light Material default first. OLED is never automatic.

**Shell**

- One `Scaffold` with a 64dp top bar, content, and navigation:
  - <600dp: bottom bar with Home, Locations, Account, Settings, More.
  - ≥600dp (via `BoxWithConstraints`): navigation rail with the same five
    items; content constrained to a comfortable measure.
- More opens a modal sheet with Logs and Diagnostics; those are routes, not
  bottom-bar destinations. Keep More marked while on them. Back returns.
- Routes: `home`, `locations`, `account`, `settings`, `logs`, `diagnostics`;
  setup is outside the shell (T10). Placeholder screens show honest "Not
  connected" / "Loading" states, never fabricated data.
- Insets are handled once in the shell; screens do not add status/navigation
  padding. Use logical start/end only.

**Icons**

- Extract the existing fox from `assets/kumiho.ico` (4 frames) with `ffmpeg`:
  dump frames, pick the 256×256 frame, and generate the adaptive foreground at
  108dp for `mdpi/hdpi/xhdpi/xxhdpi/xxxhdpi` with the fox inside the 66dp safe
  zone. Generate a monochrome/alpha layer for Android 13+.
- Adaptive XML: background `@color/ic_launcher_background` (the house blue
  light primary, named resource, not a component hex), foreground, monochrome;
  round icon aliases the same. Commit the generated PNGs; no build-time icon
  tool dependency.
- Custom vector drawables for power/gauge/globe/shield/split/tile where the
  core Material icon set lacks a glyph. Do not add
  `material-icons-extended`.

**Tests and review**

- `PaletteJsonTest`: parse a palette fixture; assert all required keys and
  role-based variants; no component needs a fallback.
- `ShellNavigationTest` with a fake state: bottom bar under 600dp, rail at
  600dp, all five destinations reachable, More sheet opens Logs/Diagnostics.
- Design self-check in all three themes: Bans 1–12, contrast, 48dp, one family,
  focus order, no color-only status, logical direction. Capture the shell in
  Light/Dark/OLED for review (sample data only).

**Gate:** `./gradlew :app:testDebugUnitTest :app:lintDebug :app:assembleDebug`
plus the Go gate. **Commit:** `feat: add the android shell and house theme`

---

## T8 — Engine client and models

**Files**

- new `android/app/src/main/java/dev/benyamin/kumiho/engine/EngineTransport.kt`
- new `.../engine/GomobileEngineTransport.kt`
- new `.../engine/EngineClient.kt`
- new `.../engine/EngineModels.kt`
- new `.../engine/EngineJson.kt`
- new `.../engine/EngineException.kt`
- new `.../engine/EnginePaths.kt`
- new tests `.../engine/EngineJsonTest.kt`, `EngineClientTest.kt`
- new `.../test/MainDispatcherRule.kt` (JVM test utility)

**Interfaces**

```kotlin
interface EngineTransport {
    fun start(pathsJson: String, bridge: DeviceBridge, listener: Listener)
    fun command(requestJson: String): String
    fun close()
}
```

- `GomobileEngineTransport` imports `dev.benyamin.kumiho.go.Mobile`,
  `DeviceBridge` and `Listener`, and delegates to `Mobile.start`,
  `Mobile.command`, `Mobile.close`.
- `EngineClient` owns a transport, a `SupervisorJob` scope, and exposes:
  - `StateFlow<EngineStatus?> status`
  - `StateFlow<List<LogEntry>> logs` (bounded 2000)
  - `SharedFlow<PingResult> pings` (buffer 512)
  - `SharedFlow<Notice> notices`
  - `suspend fun start(paths: EnginePaths, bridge: DeviceBridge)`
  - `suspend fun command(type: String, payload: JSONObject? = null):
    JSONObject`
  - convenience methods for status/connect/disconnect/settings/account/login/
    locations/select/ping/logs/doctor/reset/close.
- `EnginePaths` builds exactly the paths JSON from app-private dirs:
  config/state/logs under `filesDir`; tokens under `noBackupFilesDir`;
  runtime under `cacheDir`.
- Listener callbacks parse JSON and update thread-safe flows; never touch
  Compose. Parse failures emit a `Notice` and are logged at most once per
  second.
- `command` runs on `Dispatchers.IO`, parses the envelope, and throws
  `EngineException(code, message)` for an `error` envelope. Cancellation and
  `Close` are safe.
- `EngineStatus` mirrors `api.Status` (state, authenticated, email, since,
  token/pass expiry, kill/autoconnect, location/server, proxy_only, quota,
  DNS, IPv6, exit IP/country, totals, rates). `Settings` mirrors
  `SettingsView`; `Location` mirrors `Country/City/Server`; `Account` mirrors
  `AccountInfo`; `LogEntry` mirrors `logging.Entry`; `LoginState`; `PingResult`;
  `DoctorReport/Check`; `Notice`.
- `EngineJson` helpers tolerate missing/null fields (old/new engine).

**Tests**

- Fake transport returns canned envelopes/events; assert status/log/ping flow
  updates, error code mapping, malformed JSON handling, close idempotence.
- Paths JSON keys/dirs.
- Settings/login/quota parsing, including null quota and zero values.
- No Android/native calls in JVM tests.

**Gate:** `./gradlew :app:testDebugUnitTest :app:lintDebug :app:assembleDebug`.
**Commit:** `feat: add the android engine client`

---

## T9 — VpnService, bridge, foreground lifecycle

**Files**

- new `.../vpn/KumihoVpnService.kt`
- new `.../vpn/KumihoDeviceBridge.kt`
- new `.../vpn/VpnServiceCoordinator.kt`
- new `.../vpn/VpnModels.kt`
- new `.../vpn/VpnInterfaceSpec.kt`
- new `.../vpn/AlwaysOnPolicy.kt`
- new `.../vpn/NotificationFactory.kt`
- new `.../vpn/SplitSettingsProvider.kt` (default Off; implemented in T14)
- new `.../vpn/VpnStateReducer.kt`
- new `.../permissions/PermissionController.kt`
- modify `AppContainer.kt`, `AndroidManifest.xml`, `strings.xml`
- new tests `VpnInterfaceSpecTest.kt`, `AlwaysOnPolicyTest.kt`,
  `VpnStateReducerTest.kt`

**Service and coordinator**

- `AppContainer` creates `VpnServiceCoordinator` and binds with
  `Context.BIND_AUTO_CREATE` using the application context; it survives
  activity recreation. It exposes `StateFlow<ServiceState>` (binding,
  engine-ready, status, alwaysOn, lockdown, error).
- `KumihoVpnService`:
  - creates `EngineClient` and `KumihoDeviceBridge` in `onCreate`; starts the
    engine with `EnginePaths` and the bridge; `ready` is a
    `CompletableDeferred`;
  - binder exposes the client and state; no AIDL;
  - `onBind` returns the local binder; `onCreate` does not start foreground;
  - `onStartCommand` actions: `ACTION_CONNECT`, `ACTION_DISCONNECT`,
    `ACTION_STOP`, `ACTION_REVOKE`; null intent = Always-on/restart;
  - for any connect path, call `ServiceCompat.startForeground` immediately
    with `FOREGROUND_SERVICE_TYPE_SPECIAL_USE` on API 34+; otherwise type 0;
  - `ACTION_CONNECT` payload: country/city/server/proxyOnly. If signed out,
    post a sign-in notification and stop; if VPN consent is missing, set
    `NeedsPermission`, stop foreground and stop;
  - `ACTION_DISCONNECT`: engine disconnect; remove foreground; if not bound and
    not started, `stopSelf`;
  - null intent + `isAlwaysOn()` + authenticated → auto-connect with the saved
    selection or REC (D51); signed out → sign-in notification, leave lockdown
    blocking, stop;
  - `onRevoke`: engine `revoke`, remove foreground, stop; status refresh;
  - `onDestroy`: `engine.close()`, cancel scope; fd is closed by Go;
  - `onTaskRemoved`: keep connected; otherwise stop.
  - return `START_STICKY` only while connected/connecting; else
    `START_NOT_STICKY`.
- Manifest service: `android:permission="android.permission.BIND_VPN_SERVICE"`,
  `android:exported="true"` (the system must bind; the signature permission
  protects it), `android:stopWithTask="false"`,
  `android:foregroundServiceType="specialUse"`, `tools:targetApi="34"`,
  `intent-filter` action `android.net.VpnService`, property
  `android.app.PROPERTY_SPECIAL_USE_FGS_SUBTYPE="vpn"`.
- Basic foreground notification in T9 (state + Disconnect/Open); the rich
  variant and tile land in T16.

**Device bridge**

- `configure(json)` parses
  `{mtu,allow_lan,exclude_cidrs,ipv4_excludes,ipv4_routes}` and stores it
  under a lock. Bad JSON throws to Go.
- `acquireFD(mtu)`:
  - `VpnService.Builder().setSession("Kumiho").setMtu(mtu)`
  - addresses `10.8.0.2/32` and `fd00::2/128`
  - DNS `10.8.0.2`
  - routes `0.0.0.0/0`, `::/0`; API 33+: `excludeRoute` for each
    `ipv4_excludes`; API 26–32: `addRoute` for each `ipv4_routes` instead of
    the default. C1 means `10.8.0.2/32` is never in the exclude list.
  - split: Off → `addDisallowedApplication(packageName)` only; Exclude →
    selected packages + Kumiho; Include → selected packages only, Kumiho never
    added; empty Include falls back to disallowed-Kumiho to avoid a VPN with no
    apps. Never mix allowed/disallowed.
  - `establish()`; `detachFd()`; close the PFD; return the fd. Catch
    `NameNotFoundException` per package and skip it; surface other failures to
    Go.
  - synchronize the builder and the split snapshot; the Go thread may call
    this more than once only on a new user connect (watchdog never does).
- `networkAvailable()`: true when any non-VPN network has `INTERNET` and
  `VALIDATED`; false otherwise. Do not treat the VPN itself as underlying.
- `KumihoDeviceBridge` is long-lived and owned by the service; keep a strong
  reference for the engine's lifetime.

**Interface spec (pure, testable)**

`VpnInterfaceSpec` contains addresses, DNS, routes, excluded routes, allowed
apps, and disallowed apps. `buildVpnInterfaceSpec` takes API level, config,
split settings, and the app package. The DNS host is never excluded (C1);
there is no separate pin flag. Tests cover:

- API 26–32: routes are the Go complement; no `excludeRoute`; DNS host present.
- API 33+: default route + excludes; no complement; DNS host present.
- IPv6 route always `::/0`; no IPv6 exclusion.
- Off/Exclude/Include package lists; Kumiho always outside; no mixing; empty
  Include fallback.
- C1: `allow_lan` and a user `10.8.0.2/32` do not remove the DNS host.
- App-package `NameNotFoundException` is handled.

**Always-on policy**

- Pure function: auto-connect only when `isAlwaysOn()` is true, the engine is
  authenticated, and the state is a settled non-connected state
  (`IDLE`, `WAITING_NETWORK`, `FATAL` after a fresh start). Never auto-connect
  in `CONNECTED`, `CONNECTING`, `RECONNECTING`, `PROXY_ONLY`, or when signed
  out. Signed out → sign-in notification. Lockdown state is displayed, never
  fought; a disconnect is allowed with an explanation.
- `PermissionController`: `VpnService.prepare` ActivityResult, notification
  permission request, battery settings intent, notification settings intent,
  VPN settings intent (`Settings.ACTION_VPN_SETTINGS`), and
  `VpnService.isAlwaysOn()`/`isLockdownEnabled()` reads.

**Tests**

- Interface spec table as above.
- Always-on policy table; signed-out notification; no connect under lockdown
  without auth.
- State reducer: Off/Connected/Proxy only/Connecting/Reconnecting/Waiting/Not
  signed in/Error, service unavailable, permission needed.
- Manifest merge/lint; service does not create a notification while merely
  bound for setup.

**Gate:** `./gradlew :app:testDebugUnitTest :app:lintDebug :app:assembleDebug`.
**Commit:** `feat: add the android vpn service`

---

## T10 — Four-step setup

**Files**

- new `.../ui/setup/SetupFlow.kt`
- new `.../ui/setup/SetupViewModel.kt`
- new `.../ui/setup/ConsentStep.kt`, `SignInStep.kt`, `PermissionsStep.kt`,
  `AlwaysOnStep.kt`
- new `.../ui/setup/SignInPane.kt` (reused by Account in T15)
- new `.../ui/setup/SetupState.kt`
- modify `MainActivity.kt`/`App.kt` to choose setup vs shell
- new tests `SetupViewModelTest.kt`, `SetupFlowTest.kt`

**Behavior**

1. **Consent** — explain what Kumiho routes, what stays local, no telemetry,
   and the local proxy warning. Required tick; Continue disabled until ticked.
2. **Sign in** — native Compose FxA:
   - email → `login_email`;
   - password → `login_password`; clear the password field immediately and
     never log it;
   - `login_2fa`: show code entry and, when the method is email-link, a
     "Re-check" action that calls `login_2fa` with an empty code; poll no more
     than every 3s while the screen is resumed, with a visible waiting state;
   - `done` → next; `error` shown inline; if already authenticated, show the
     account and Continue;
   - no browser fallback; captcha limitation is named in the error.
3. **Permissions** — VPN permission card (`VpnService.prepare` via
   ActivityResult), notification permission card (API 33+), status of each.
   Denial is surfaced in place and still allows Continue (A13); Connect later
   re-prompts. Never claim permission was granted.
4. **Always-on** — explain Always-on VPN and lockdown, open
   `Settings.ACTION_VPN_SETTINGS`, show the current state. Finish persists
   `setup_complete=true` in DataStore and navigates to Home. Offer "Connect
   now" only if signed in and VPN permission is granted; otherwise Finish.
- Step indicator uses real sequence numbers (Ban 11 carve-out). 48dp targets,
  logical layout, back behavior: no silent skip of a failed step; the user can
  go back and finish.
- `SetupViewModel` is a pure state machine over `EngineClient` and
  `PermissionController`; no direct Go/Android calls in the composable.

**Tests**

- Consent gating; denied permission still reaches Finish; sign-in step
  transitions and password clearing; email-link re-check; already-authenticated
  path; setup completion persists; back navigation.
- Compose test with a fake client: all four steps, no fake connection state.

**Gate:** unit/lint/assemble. **Commit:** `feat: add the android setup flow`

---

## T11 — Dashboard and first real connect (STOP/GO)

**Files**

- new `.../ui/dashboard/DashboardScreen.kt`
- new `.../ui/dashboard/DashboardViewModel.kt`
- new `.../ui/dashboard/PowerHero.kt`
- new `.../ui/dashboard/SummaryCards.kt`
- new `.../ui/dashboard/StatusDetails.kt`
- new `.../ui/dashboard/ConnectOptionsSheet.kt`
- new `.../ui/dashboard/CompactLocationPicker.kt`
- new `.../ui/dashboard/DashboardFormat.kt`
- new `.../ui/dashboard/DashboardState.kt`
- modify `App.kt` (Home route)
- new tests `DashboardStateTest.kt`, `DashboardFormatTest.kt`,
  `DashboardScreenTest.kt`

**Behavior**

- Power hero: one large power control, spring animation, state word from the
  engine mapping, since/uptime, location, proxy-only badge. The action is
  disabled while a connect/disconnect is in flight; no double submits.
- Four cards:
  - Location (selected country/city/server, REC → "Recommended");
  - Exit IP (IP and country when available);
  - Quota (remaining/limit, bar, reset);
  - Session (up/down totals, rates, uptime).
  Cards hide until their data exists; no placeholders that look real.
- Details: kill switch row shows Always-on and lockdown from the OS, read-only,
  with "Open VPN settings"; autoconnect row maps to Always-on; token/pass
  expiry; DNS; IPv6 ("blackholed").
- Notice row for the latest connect/status error with Retry where meaningful;
  status load failure does not blank existing data.
- Options sheet: compact location picker (REC, saved, country → city), proxy
  only toggle, lockdown shortcut, Connect. The full tree remains in Locations.
- Connect path:
  1. If `VpnService.prepare(context)` returns an Intent, launch it; if denied,
     show an explanation and keep the app usable.
  2. On grant, call `VpnServiceCoordinator.connect(...)` (foreground start).
  3. Observe binder state; surface `NeedsPermission`, `Error`, and
     `Waiting for network`.
  Disconnect is allowed under lockdown with an explanation; the OS may restart
  the service, and the UI reflects the real state (D31).
- Never use engine `kill_switch`/`autoconnect` values; never call
  `ctrl.Autoconnect`.
- First-connect gate `[user]` on a physical arm64 phone:
  - install the debug or signed release APK; complete setup and sign in;
  - connect; `adb shell dumpsys connectivity` shows the VPN with DNS
    `10.8.0.2`; engine logs show the DNS listener;
  - browse an HTTPS site through the tunnel; the exit IP card updates;
  - IPv6 check (`test-ipv6.com` or an IPv6 ping) shows no IPv6 path; no leak;
  - disconnect and reconnect with a fresh fd; no re-establish during a
    watchdog redial (network toggle while connected);
  - if DNS resolution fails while the tunnel is up, **STOP**: report the
    establish → bind → query evidence and re-plan the DNS delivery. Do not
    paper over it with Private DNS or a fallback resolver.
- This gate must pass before T12–T16 UI work is considered complete. T12 may
  start while waiting for the phone, but no release work starts until it passes.

**Tests**

- State mapping table for all engine states + binding/loading/permission.
- Formatting: bytes, rates, quota, uptime, expiry, missing values.
- Connect/disconnect button enablement and one-in-flight.
- Options sheet selection; proxy-only flag; lockdown shortcut action.
- Compose test: power hero + four cards with fake data; notice; permission
  state.

**Gate:** unit/lint/assemble + `[user]` physical DNS/IPv6 check.
**Commit:** `feat: add the android dashboard`

---

## T12 — Locations

**Files**

- new `.../ui/locations/LocationsScreen.kt`
- new `.../ui/locations/LocationsViewModel.kt`
- new `.../ui/locations/LocationTree.kt`
- new `.../ui/locations/LocationFilter.kt`
- new tests `LocationsViewModelTest.kt`, `LocationFilterTest.kt`

**Behavior**

- Full country/city/server tree; REC first; current selection marked; server
  rows show protocol/port and latency (`42 ms`, `timeout`).
- Filter matches country, city, server hostname/country code live; no network
  on keystroke.
- `select_location` persists and updates the dashboard; selection does not
  auto-connect.
- Refresh (`locations {refresh:true}`) with loading/notice; Test speed sends
  visible `host:port` targets to `ping`, 24-way concurrency, streams results,
  and keeps last values. No background probes when the screen is not visible.
- 48dp rows, search field with a visible label, empty/error states, logical
  layout.

**Tests**

- Tree flatten/filter/selection; REC ordering; ping map replacement;
  refresh failure preserves the old list; no duplicate hosts.

**Gate:** unit/lint/assemble. **Commit:** `feat: add android locations`

---

## T13 — Settings shell, app settings and all 19 engine rows

**Files**

- new `.../ui/settings/SettingsScreen.kt`
- new `.../ui/settings/SettingsViewModel.kt`
- new `.../ui/settings/SettingsGroups.kt`
- new `.../ui/settings/SettingEditors.kt`
- new `.../ui/settings/AndroidStateRows.kt`
- new `.../ui/settings/ThemeSettings.kt`
- new `.../ui/settings/UpdateChecker.kt`
- new `.../ui/settings/SettingsState.kt`
- new tests `SettingsMapperTest.kt`, `UpdateCheckerTest.kt`,
  `SettingsViewModelTest.kt`

**Behavior**

- Grouped list with a collapsed **Advanced** group. All 19 engine rows are
  reachable exactly once:
  `socks_port`, `mtu`, `exit_check`, `doh_provider`, `dns_upstream`,
  `edge_address`, `upstream_proxy`, `exclude_cidrs`, `allow_lan`, `log_level`,
  `redact_targets`, `kill_switch`, `autoconnect`, `quota_poll_minutes`,
  `tls_split`, `http_proxy`, `http_proxy_port`, `solve_challenges`,
  `keepalive_profile`.
- `kill_switch`/`autoconnect` are read-only Android state rows: show
  Always-on/lockdown, never call `settings_set`; a button opens the system VPN
  settings. Engine values are ignored (A10).
- Other rows: enum dropdown, on/off, integer, or text editor; send
  `settings_set` with `on/off` where applicable; `exclude_cidrs` is a
  comma-separated IPv4 editor; show the engine's validation error inline.
  Each row shows the overridden mark and Reset; Reset drops the override. Only
  one change in flight ("Applying…"); a failed change keeps the old value.
- App section: Theme (Light/Dark/OLED), Accent (six), version (app + engine),
  manual Check for updates (GitHub latest, compare semver, open release page;
  no download), notification state, battery optimization state, Always-on
  shortcut, Split tunneling shortcut (T14). `Start at login` is intentionally
  absent (no boot receiver; Always-on is the mapping).
- `UpdateChecker` is a testable interface around an HTTP client; User-Agent
  `Kumiho-Android`; parse `tag_name`; ignore pre-releases; no background calls.
- Settings hints match apply timing: log level/redact/solve apply immediately
  (engine behavior); MTU/SOCKS/DNS/proxy/TLS/split/exclusions/keepalive next
  connect.
- Add an Android-specific hint: `allow_lan` excludes private ranges but the
  tunnel DNS `10.8.0.2` stays inside (C1); IPv6 ranges are rejected (always
  blackholed).

**Tests**

- Mapping for every key: type, current value, override, reset, serialized
  value.
- Boolean uses on/off; port/MTU range errors; enum rejects; list parse.
- Read-only kill/autoconnect never calls settings_set.
- Update checker: newer/equal/older/pre-release/network error/malformed JSON.
- Theme/accent immediate persistence.
- Compose test: all 19 rows reachable, Advanced collapsed by default.

**Gate:** unit/lint/assemble. **Commit:** `feat: add android settings`

---

## T14 — Split tunneling and route exclusions

**Files**

- new `.../ui/settings/SplitTunnelScreen.kt`
- new `.../ui/settings/SplitTunnelViewModel.kt`
- new `.../data/SplitTunnelRepository.kt`
- new `.../data/InstalledAppsRepository.kt`
- new `.../ui/components/AppIcon.kt`
- new `.../ui/settings/CidrEditor.kt`
- modify `KumihoDeviceBridge.kt`/`SplitSettingsProvider.kt` to read the
  DataStore-backed snapshot
- new tests `SplitTunnelTest.kt`, `CidrValidatorTest.kt`,
  `InstalledAppsFilterTest.kt`, `SplitInterfaceSpecTest.kt`

**Behavior**

- Modes `Off` (default), `Exclude`, `Include`; persisted in DataStore with the
  package set. Changes apply on the next connect; if connected, show
  "Reconnect to apply".
- Package picker:
  - `QUERY_ALL_PACKAGES`; default list is launchable apps; a "System apps"
    toggle reveals non-launchable/system apps; search by label/package.
  - Kumiho is always disabled/omitted: Include omits it; Exclude adds it
    automatically and shows it as locked; Off adds it as disallowed.
  - 48dp rows, app icon via `AndroidView`/bitmap cache; no Accompanist.
  - Uninstalled packages are dropped on load; `NameNotFoundException` during
    establish is skipped.
- Android allows only one of allowed/disallowed; the pure interface spec
  enforces no mixing. Empty Include falls back to disallowed-Kumiho; warn in
  the UI.
- Route exclusions editor in Settings:
  - IPv4 CIDR list; single IP becomes /32; reject `0.0.0.0/0`, IPv6, malformed
    values; allow `10.8.0.2/32` but hint that the DNS host remains routed.
  - `allow_lan` switch with the RFC1918/link-local/multicast explanation.
  - The Go route helper (T5) computes excludes/complement; the UI surfaces
    "too many route exclusions" from connect.
  - API 33+ uses `excludeRoute`; API 26–32 uses complement routes; IPv6 never
    excluded.
- Diagnostics/Settings show the active split mode and app count.

**Tests**

- Repository round-trip/default Off/uninstall cleanup.
- Package filter: launchable vs system, Kumiho locked, search.
- CIDR validator table.
- Interface spec: Off/Exclude/Include package lists; no mixing; empty Include;
  Kumiho excluded; the route lists contain the DNS host and never exclude it;
  no IPv6.
- `KumihoDeviceBridge.configure` parses the Go JSON and uses the repo snapshot.

**Gate:** unit/lint/assemble. **Commit:** `feat: add android split tunneling`

---

## T15 — Account, Logs, Diagnostics, More, crash log

**Files**

- new `.../ui/account/AccountScreen.kt`, `AccountViewModel.kt`
- new `.../ui/logs/LogsScreen.kt`, `LogsViewModel.kt`
- new `.../ui/diagnostics/DiagnosticsScreen.kt`, `DiagnosticsViewModel.kt`
- new `.../ui/more/MoreSheet.kt` (content wiring)
- new `.../crash/CrashHandler.kt`
- new `.../data/LogExporter.kt`
- new `.../diagnostics/AndroidFacts.kt`
- new `android/app/src/main/res/xml/file_paths.xml`
- modify `AndroidManifest.xml` (FileProvider)
- modify `KumihoApplication.kt` (install crash handler)
- modify `KumihoVpnService.kt` (replay crash file after engine ready)
- new tests `LogFilterTest.kt`, `AndroidFactsTest.kt`,
  `CrashFormatTest.kt`, `AccountStateTest.kt`, `DiagnosticsReducerTest.kt`

**Account**

- Mirror the desktop card: email, UID, subscription, quota (remaining/limit),
  bandwidth (limited/unlimited), token expiry. Refresh action with 35 s
  engine timeout; local fields survive an online lookup error.
- Sign out opens a confirmation ("tokens are cleared and the tunnel
  disconnects"); call `logout`; stay on Account; signed-out state shows the
  shared `SignInPane`; no wizard restart.
- `SignInPane` from T10 is reused; login errors inline.

**Logs**

- 2000-line in-app console; snapshot on open + `logs_subscribe` while visible;
  unsubscribe on dispose. Level filter (debug/info/warn/error), search, Follow,
  Clear. Clear calls `logs_clear` (ring + file).
- Export writes a snapshot to `cacheDir/exports/kumiho-logs-<timestamp>.log`
  and opens the system share sheet via FileProvider. "Open log folder" is
  represented as "Share log file" (Android has no universal folder intent).
- No monospace family (one family rule); use default with a readable size and
  tabular-ish spacing. Copy line(s) where useful.

**Diagnostics**

- On first visit each launch, collect Android facts and call `doctor`; render
  chips OK/Warn/Fail/Skip, detail, remediation, copy all, re-run.
- VPN service card: service bound/running, engine state, Always-on/lockdown,
  notification permission, battery optimization, network, DNS, Private DNS,
  local ports, clock skew, endpoints, engine state.
- "Reset local state…" confirmation calls `reset_state` (server cache, logs,
  failures; never tokens) and refreshes.
- System-settings shortcuts: VPN settings, app notification settings, battery
  optimization. No admin/service start-stop (no desktop daemon on Android).
- `AndroidFacts` gathers only what Go cannot: `VpnService.prepare` state,
  Always-on/lockdown, notification/battery, connectivity (non-VPN), VPN link
  DNS/Private DNS, local port probes, service/engine state, device/SDK.
- The report is cached per visit; no periodic polling.

**Crash log**

- `CrashHandler` writes `filesDir/crash.log` with timestamp, thread, exception
  and stack trace, then delegates to the previous handler. It must not swallow
  the crash or block long.
- After engine start, `KumihoVpnService` reads the file once, calls
  `log_append` at error/tag `crash`, then deletes it. A malformed/oversized
  file is truncated; no tokens are read from it. Local only; no upload.

**Tests**

- Log filter/search/follow/clear; export file name/content; 2000 cap.
- Android facts mapping; doctor checks; no check for desktop-only concepts.
- Crash formatting and replay/delete.
- Account reducer (signed out, error, quota missing); sign-out dialog.
- Compose test: More sheet → Logs/Diagnostics; diagnostics chips; account
  signed-out.

**Gate:** unit/lint/assemble. **Commit:**
`feat: add android account logs and diagnostics`

---

## T16 — Rich notification, Quick Settings tile, proxy-only polish

**Files**

- modify `.../vpn/NotificationFactory.kt`
- new `.../vpn/QuickConnectTileService.kt`
- new `.../vpn/TileState.kt`
- new `.../vpn/VpnStatusCache.kt`
- modify `AndroidManifest.xml` (tile service)
- new `.../res/drawable/ic_tile.xml`
- new tests `TileStateTest.kt`, `NotificationTextTest.kt`,
  `ProxyOnlyStateTest.kt`

**Behavior**

- Foreground notification: state, location, uptime, up/down totals; actions
  Disconnect and Open app; no Connect action (D30). Update on state change and
  once per minute while connected; `setOnlyAlertOnce(true)`, ongoing, low
  importance; no sound. A separate default-importance channel handles sign-in
  and errors.
- Quick Settings tile:
  - label Kumiho; icon state active/inactive; subtitle from the cached status;
  - click connects if idle and consent exists, otherwise uses
    `startActivityAndWait` for the VPN consent or `unlockAndRun`; connected
    click disconnects; no lying state when the service is not running;
  - `TileService.requestListeningState` when the service state changes;
    `VpnStatusCache` is process-local and cleared when the engine stops;
  - manifest `BIND_QUICK_SETTINGS_TILE`, exported, intent filter
    `android.service.quicksettings.action.QS_TILE`.
- Proxy-only:
  - Dashboard option and service path start no TUN interface; engine state
    `PROXY_ONLY`; notification says "Proxy only"; local SOCKS/HTTP warnings.
  - No per-app split or Always-on interface implications in proxy-only; the
    service stays foreground.
- Always-on/lockdown polish:
  - the notification and dashboard reflect OS restarts; a disconnect under
    lockdown explains the OS may restart and opens VPN settings;
  - no fake "kill switch on/off" toggle; no engine `Autoconnect`.

**Tests**

- Tile state reducer: disconnected, connecting, connected, proxy-only,
  reconnecting, service unavailable, signed out, consent needed.
- Notification text for all states + uptime; no Connect action.
- Proxy-only connect path builds no VpnInterfaceSpec.
- Compose semantics: tile is not part of the app UI, test via reducer.

**Gate:** unit/lint/assemble. **Commit:**
`feat: add the android quick settings tile`

---

## T17 — Build, CI, signing and release

**Files**

- modify `Makefile` (final Android targets)
- new `scripts/android-release.sh`
- new `scripts/android-verify-apk.sh`
- new `android/keystore.properties.example`
- modify `android/app/build.gradle.kts` (signing config final)
- modify `.github/workflows/ci.yml`
- modify `.github/workflows/release.yml`
- modify `.gitignore` if needed
- new `CHANGELOG.md` entry is T18

**Makefile targets**

    android-setup          # T1
    android-aar            # release arm/arm64 AAR
    android-aar-emulator   # debug amd64 AAR
    android-debug          # AAR + assembleDebug
    android-test           # AAR + unit + lint
    android-emulator-test  # AAR emulator + connected tests
    android-release        # AAR + signed release APKs
    android-verify         # badging, ABIs, 16KB, signatures, sizes

**Signing**

- `android/keystore.properties` (ignored) keys: `storeFile`, `storePassword`,
  `keyAlias`, `keyPassword`. `build.gradle.kts` reads it only if present;
  release without it is unsigned and only used for local compile checks.
- Release script requires `KUMIHO_VERSION` (`v` stripped), a keystore,
  `apksigner` and `zipalign`; builds:
  - `app-arm64-v8a-release.apk` → `kumiho-v<version>-android-arm64-v8a.apk`
  - `app-armeabi-v7a-release.apk` →
    `kumiho-v<version>-android-armeabi-v7a.apk`
  - `app-universal-release.apk` → `kumiho-v<version>-android-universal.apk`
- Verify:
  - `aapt2 dump badging` package/version/min/target;
  - `unzip -l` contains only the expected ABI(s); universal has both and no
    x86/x86_64;
  - `zipalign -c -P 16 -v 4` each APK;
  - `llvm-readelf` `p_align >= 0x4000` for arm64 `libgojni.so`;
  - `apksigner verify --print-certs` each signed APK;
  - universal size ≤60 MB; per-ABI sizes recorded.
- `scripts/android-verify-apk.sh` accepts an APK path and performs the checks
  so CI and local use the same commands.

**CI (`ci.yml`)**

- Add an `android` job on `ubuntu-latest`:
  - checkout; setup Go 1.27.1; setup JDK 17; setup Android SDK; run
    `scripts/android-setup.sh --ci`;
  - cache Go/Gradle; `scripts/android-build-aar.sh --emulator`;
  - `cd android && ./gradlew -PkumihoEmulator=true :app:testDebugUnitTest
    :app:lintDebug :app:assembleDebug`;
  - emulator runner API 36 `google_apis` x86_64, KVM, animations disabled,
    `./gradlew -PkumihoEmulator=true :app:connectedDebugAndroidTest`;
  - build the release AAR and `:app:assembleRelease` (unsigned on push);
  - run the 16KB/alignment/ABI checks on the debug/release artifacts.
- Add a 16KB API 36 emulator job (or a second matrix entry) if the runner's
  `sdkmanager` provides `google_apis_16k`; otherwise keep the `zipalign`/
  `p_align` checks and record the physical 16KB check in T18. Do not silently
  skip the 16KB gate; name the fallback in CI logs.
- Keep the existing `build-test` and `windows` jobs unchanged. Do not duplicate
  the full Go gate in a way that changes its meaning.

**Release (`release.yml`)**

- Add JDK 17 + Android SDK/NDK setup and `scripts/android-setup.sh --ci` to the
  existing build job (or a job that feeds it), before packaging.
- Decode `ANDROID_KEYSTORE_BASE64` to a temp keystore, write
  `android/keystore.properties` from the four secrets, build
  `scripts/android-build-aar.sh` and `scripts/android-release.sh`.
- Copy the three APKs into `dist/`, include them in `SHA256SUMS`, and add them
  to `gh release create`:
  `kumiho-${GITHUB_REF_NAME}-android-arm64-v8a.apk`,
  `...-armeabi-v7a.apk`, `...-universal.apk`.
- Never echo secrets; remove the keystore/properties in an `always()` step.
- `[user]`: create the release keystore offline, back it up, set the four
  GitHub Actions secrets. Example:
  `keytool -genkeypair -v -keystore ~/kumiho-android-release.jks -alias kumiho -keyalg RSA -keysize 4096 -validity 10000`
  then base64 it and `gh secret set`. Never commit the keystore.

**Verification**

    source scripts/android-env.sh
    make android-test
    make android-aar
    cd android && ./gradlew :app:assembleRelease
    cd .. && scripts/android-release.sh   # requires the local keystore
    scripts/android-verify-apk.sh dist/kumiho-v0.5.0-android-universal.apk
    python3 - <<'PY'
    import yaml
    for p in ['.github/workflows/ci.yml', '.github/workflows/release.yml']:
        yaml.safe_load(open(p))
    print('workflows parse')
    PY

- `[user]`: push to a branch and confirm the Android CI job and emulator tests
  are green; do not push without an explicit request. Tag/release checks are
  `[user]` too.

**Commit:** `ci: build and release the android app`

---

## T18 — Documentation, parity sign-off and physical acceptance

**Files**

- new `docs/android.md`
- new `docs/android-testing.md`
- modify `README.md` (support matrix, install, build commands, docs links)
- modify `AGENTS.md` (Android commands, architecture map, vendor policy,
  every-commit gate)
- modify `CHANGELOG.md` (0.5.0)
- modify `NOTICE` (AndroidX/Compose/Kotlin/gomobile licenses)
- optionally modify `docs/usage.md` to link the Android guide
- no `.ai-state` commit; update it locally after wrap-up

**`docs/android.md` (user guide)**

- Requirements: Android 8.0 (API 26)+, arm64-v8a or armeabi-v7a; sideload from
  GitHub Releases; no Play/F-Droid; APK names and SHA256SUMS.
- Install and first run: four-step setup, VPN permission, notification
  permission, Always-on guidance; how to update manually.
- Dashboard, connect options, Locations, Settings (all 19 rows and Android
  mappings), Account, Logs, Diagnostics, More.
- Split tunneling: Off/Exclude/Include, Kumiho always outside, next-connect
  behavior, API 33 vs 26–32 route handling, the DNS host carve-out (C1), IPv6
  blackhole.
- Always-on/lockdown: what Kumiho can and cannot enforce; boot and restart
  behavior; how to open system VPN settings; disconnect under lockdown.
- Proxy-only and local SOCKS/HTTP proxies, device-wide warning, ports.
- Privacy: no telemetry, no backup, tokens in no-backup storage, local crash
  log, log sharing, private DNS interaction.
- Troubleshooting: no DNS while connected, Private DNS, battery killers,
  OEM Always-on quirks, notification/tile missing, permission revoked,
  uninstall/state removal; `adb logcat` / `dumpsys` pointers.
- Limitations: IPv6 blackholed, English-only, no widget/shortcuts, no browser
  login, captcha limitation, no auto-update.

**`docs/android-testing.md` (build and test guide)**

- Toolchain install/check (`scripts/android-setup.sh --check`), environment,
  NDK/gomobile pin, Gradle wrapper.
- AAR build (arm/arm64 release; amd64 emulator), Gradle variants, Makefile
  targets.
- Unit/lint/assemble/emulator test commands; API 26 and API 36 coverage;
  16KB page-size checks; APK verification.
- Signing/keystore generation, GitHub secrets, offline backup/recovery;
  release workflow and asset names/checksums.
- Physical acceptance checklist with checkboxes and a sign-off table (date,
  device, Android version/API, build/version, tester). Include the T11 DNS/IPv6
  gate, Always-on boot, lockdown restart, QS tile, notification action, both
  split modes, route exclusions on API 26–32 and 33+, proxy-only, local
  proxies, battery diagnostics, permission denial/revocation, reconnect.
- Rollback/uninstall.

**README/AGENTS/CHANGELOG/NOTICE**

- README: add Android to the supported targets, link the two docs, add
  `make android-*` commands, remove "Android planned" claims.
- AGENTS: Android architecture map (`internal/mobile`, real
  `internal/platform/android`, `android/`), build/test commands, vendor policy
  (AAR not committed; AndroidX/Compose in NOTICE), every-commit gate includes
  Android cross-build and the Android job.
- CHANGELOG: `[0.5.0] - 2026-10-21` with the Android client, shared version,
  no desktop behavior change, link the docs.
- NOTICE: Apache-2.0 AndroidX/Compose/Kotlin; BSD-3-Clause gomobile; preserve
  existing gVisor/tun2socks/FoxyVPN entries; no FoxyVPN code is copied.

**Physical acceptance `[user]`**

- API 33+ arm64 phone: setup, sign-in, connect, browse, DNS, exit IP, quota,
  IPv6 blackhole, disconnect/reconnect, watchdog redial, Always-on boot,
  lockdown restart, QS tile, notification actions, both split modes, proxy-only,
  local proxies, battery/notification diagnostics, denial/revocation.
- API 26–32 phone or emulator: route-complement connection, allow_lan, user
  exclusions, split in both modes, DNS still resolves (C1).
- 16KB-page device/emulator if available: install, connect, verify `.so`
  alignment and no crash.
- Record results in `docs/android-testing.md`; any P0/P1 failure stops the
  release.

**Final gate**

    make lint && make test && go test -race -count=1 ./... && make build && \
      GOOS=windows GOARCH=amd64 go vet ./... && \
      GOOS=windows GOARCH=amd64 go build ./... && \
      GOOS=android GOARCH=arm64 go build ./... && \
      GOOS=android GOARCH=arm go build ./internal/... && \
      make android-test && make android-aar && \
      cd android && ./gradlew :app:assembleRelease && cd ..

- `make android-aar` must produce both arm `.so` files and pass the 16KB
  alignment check.
- All docs links resolve; `grep -r "Android planned"` returns nothing.
- Update local `.ai-state` to one line, e.g. `android v0.5.0 client implemented;
  awaiting physical sign-off`.

**Commit:** `docs: document the android client`

---

## Dependency graph

```
T1 ──┬── T2 ────────────────┬── T6 ── T7 ── T8 ── T9 ── T10 ── T11 ── T12 ── T13 ── T14 ── T15 ── T16 ── T17 ── T18
     ├── T3 ── T5 ──────────┘
     └── T4 ── T5
```

- T3/T4 are independent after T1.
- T2 can be done in parallel with T3/T4, but T6 waits for T5.
- T9 is the first task that needs the AAR, T7 and T8.
- T11 is the first real connect; do not let UI scope outrun a failing DNS
  gate.
- T14 can start after T9/T13; T15/T16 can overlap with T14 only if the service
  contract is frozen.
- T17 is last because signing/release depends on a stable build and UI surface.
- T18 is not complete until the physical checklist is signed.

## Verification matrix

| Layer | Command | Gate |
|---|---|---|
| Go host | `make lint && make test && go test -race -count=1 ./...` | every Go task |
| Go cross | `make build && GOOS=windows ... && GOOS=android GOARCH=arm64 go build ./...` | every Go task |
| Go 32-bit | `GOOS=android GOARCH=arm go build ./internal/...` | T4/T5/T17 |
| AAR | `scripts/android-build-aar.sh` + `unzip`/`readelf` | T6/T17 |
| Kotlin unit | `./gradlew :app:testDebugUnitTest` | T7–T16 |
| Android lint | `./gradlew :app:lintDebug` | T7–T16 |
| Debug APK | `./gradlew :app:assembleDebug` | T2, T7–T16 |
| Instrumented | `./gradlew -PkumihoEmulator=true :app:connectedDebugAndroidTest` | T6/T17 |
| Release build | `./gradlew :app:assembleRelease` | T17 |
| Signing/verify | `scripts/android-release.sh` + `android-verify-apk.sh` | T17 `[user]` |
| Physical | checklist in `docs/android-testing.md` | T11/T18 `[user]` |
| Docs | links/grep review | T18 |

## Parity checklist (desktop ability → Android surface)

| Desktop/CLI ability | Android equivalent | Task |
|---|---|---|
| Setup wizard | Four-step setup (consent, sign-in, permissions, Always-on) | T10 |
| FxA email/password/2FA/email link | Native Compose sign-in, same engine state machine | T10/T15 |
| Logout | Account confirmation + `logout` | T15 |
| Connect/disconnect, saved/REC selection | Dashboard power control + options sheet | T11 |
| Per-connect location, proxy-only, kill-switch choice | Location picker, proxy-only toggle, OS lockdown shortcut | T11 |
| Status: 11 rows | Power hero + four cards + details | T11 |
| Locations tree, refresh, Test speed | Locations screen, 24-way ping | T12 |
| 19 settings rows, overrides/reset | Settings groups + collapsed Advanced | T13 |
| Theme/accent/update check | App settings + manual GitHub check | T13 |
| Account entitlement + sign out | Account screen | T15 |
| Logs: 2000 lines, filter/follow/search/clear/export | Logs screen + share sheet + file | T15 |
| Diagnostics: doctor, copy, cleanup | Diagnostics + Android facts + Reset local state | T15 |
| Tray/lifecycle/window | Foreground service, notification, QS tile, Always-on | T9/T16 |
| Kill switch/autoconnect | Read-only Always-on/lockdown, system settings link | T11/T13 |
| Split tunneling | Off/Exclude/Include + per-app picker | T14 |
| IPv6 | Captured + blackholed; DNS AAAA falls back | T4/T9/T11 |
| Local SOCKS/HTTP proxies | Settings rows + device-wide warning | T13/T16 |
| Version/update | App + engine version, manual check | T13 |
| Crash/diagnostics | Local crash file replayed into logs | T15 |
| No telemetry/backups | Manifest/SDK constraints + private paths | T2/T5/T18 |
| Protocol v1/desktop untouched | No IPC changes; additive mobile/provider/theme | all |

## Decision coverage

| Decisions | Landed by |
|---|---|
| D1–D10, D37–D40 (parity, ABIs, sideload, stack, service, shell, dashboard) | T2, T7, T9–T17 |
| D11–D14, D21, D23, D30–D34, D47, D49, D51 (Android surfaces, split, Always-on, proxy) | T9–T11, T14, T16 |
| D15–D20, D35, D50 (DNS, IPv6, palette, type, motion) | T3–T5, T7, T9, T11 |
| D22, D24, D25, D28, D29, D41–D48 (settings, locations, logs, login, tests, version, docs) | T10–T18 |
| A1–A18 (DataStore, MVVM, layout, versions, fd, no-op netconfig, QUERY_ALL_PACKAGES, reset, CI, docs) | T2, T5, T9, T12–T18 |

## Risks and stop/go gates

| Risk | Mitigation / gate |
|---|---|
| `gomobile` AAR or Kotlin callback fails | T6 **STOP/GO**; fallback JNI/shared lib/separate process needs a re-plan |
| VpnService fd adoption or real interface fails | T4 pipe test + T11 first-connect gate |
| In-tunnel DNS `10.8.0.2:53` is unreachable when Kumiho is excluded | T11 **STOP/GO**; do not fall back to Private DNS or a different resolver |
| `allow_lan`/`exclude_cidrs` excludes the DNS host | C1 subtraction + Go/Kotlin tests + T11 |
| API 26–32 route complement expands too far | 256 cap, `0.0.0.0/0` rejection, reference tests, API 26/33 physical checks |
| IPv6 leaks or AAAA stalls apps | fd filter + on-device IPv6 test; document fallback delay |
| Watchdog redial drops the interface | T9 bridge lock/generation; T11 network-toggle test; engine only swaps upstream |
| Always-on/lockdown restart loops or lies | T9 policy + T16 state cache; reflect OS, never fight; OEM limitations documented |
| Split × lockdown ignored by the OS | Apply at establish; test combination; show real mode |
| Universal APK >60 MB | release shrinking, `-s -w`, per-ABI splits; T17 size gate; if over, stop and re-plan |
| 16KB page size | NDK 28.2, `p_align`/`zipalign -P 16`, 16KB emulator/device check |
| Release key loss | offline backup + documented recovery; never commit |
| CI emulator unavailable | AAR/unit/lint still gate; physical test `[user]`; do not skip silently |
| X86 test AAR leaks into release | Gradle property only in debug/emulator; T17 verifies no x86 in release |
| Private DNS bypasses Kumiho | diagnostics warn; docs explain; no auto-change |
| Captcha in FxA | clear error naming the limitation; no browser fallback |
| No phone available | T11/T18 stay open; release cannot be declared done |

**Rollback:** all work is additive. Revert T1–T18 commits; delete `android/` and
`internal/mobile`; restore the previous `internal/platform/android` stub; keep
or revert the additive `theme.Palette` helper. No data migration, no desktop
state change, no protocol change. AARs are untracked. If T6/T11 fails, stop
before UI/release work and re-plan the bridge or DNS path.

## Open questions

None. The spec and this plan are approved; C1 was confirmed at gate #2. All
assumptions A1–A18 remain as recorded in the spec.

## Approval record

Approved at plan gate #2 on 2026-10-21, including C1 (the tunnel DNS host is
subtracted from LAN/user exclusions) and the T6/T11 stop/go gates. The plan is
committed as `docs: add the android client implementation plan`; implementation
starts with T1 only after an explicit go-ahead.
