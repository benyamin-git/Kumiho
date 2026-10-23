# Kumiho Android Client — Design Spec

**Status:** Approved
**Date:** 2026-10-21

## Objective / why

Android is the platform the Firefox-account VPN entitlement was designed for, and
the last first-class client Kumiho is missing. This cycle ships a native Kotlin +
Jetpack Compose app that embeds the existing Go engine in a foreground
`VpnService`, reaches full desktop feature parity, and presents it with the
simplicity of FoxyVPN's home while following the house design rules. It replaces
the compile-only Android stub (`internal/platform/android`) with a real provider
and adds an embeddable mobile facade over the engine. The desktop daemon, IPC
protocol v1, CLI, TUI and GUI must remain behaviorally untouched.

The reference app is FoxyVPN (MIT): native Kotlin/Compose, one power control on
the home screen, server picker, settings and logs. No FoxyVPN code is copied; its
UX is a reference only. Kumiho's home adds quota, session, account and diagnostics
surfaces, and every desktop setting remains reachable.

## Success criteria / definition of done

1. **Fresh install works end to end.** On a physical API 26+ arm64 phone, a clean
   install completes the four-step setup, signs in with Firefox Accounts, grants
   the Android VPN permission, connects, browses through the tunnel, shows the
   real exit IP and quota, disconnects, and reconnects without restarting the app.
2. **Feature parity is verifiable.** Every ability in
   [`docs/cli-tui-abilities.md`](../../cli-tui-abilities.md) and every screen/action
   in [`docs/gui.md`](../../gui.md) is reachable on Android, with the deliberate
   Android mappings recorded in this spec (kill switch → OS lockdown, autoconnect
   → Always-on VPN, no per-app widget). A signed-off parity checklist is part of
   the implementation plan.
3. **Android surfaces work.** The Quick Settings tile connects/disconnects; the
   ongoing notification shows state, location and uptime with a Disconnect
   action; per-app split tunneling supports Off / Exclude / Include with the
   default Off; Always-on and lockdown state are reflected honestly with a deep
   link to system VPN settings; IPv6 is captured and blackholed so it cannot leak.
4. **House design is honored.** Light, Dark and OLED themes; all six accents with
   blue default; no Material You dynamic color; system Roboto; 48dp touch targets;
   4dp spacing; the twelve bans and six accessibility items pass in all three
   themes; bottom navigation on phones and a navigation rail at ≥600dp.
5. **Lifecycle and edge cases hold.** The tunnel survives activity destruction;
   Always-on starts the service after boot and the service auto-connects when
   signed in; `onRevoke` tears down cleanly; a watchdog reconnect swaps the
   upstream session without dropping the VpnService interface, while a new user
   connect after disconnect obtains a fresh fd; a lockdown-driven restart is
   reflected instead of fought; notification and tile state never claim a
   connection that is not there.
6. **Performance targets pass on the test phone.** Cold start to an interactive
   Dashboard ≤2s; status and notification updates ≤500ms after an engine change;
   no periodic UI polling or wakelocks beyond the engine's keepalive; universal
   APK ≤60MB.
7. **Privacy and security hold.** `allowBackup=false` and device-transfer
   exclusion; tokens live only in the app's no-backup private storage; no
   telemetry or analytics; the last uncaught exception is kept locally and
   replayed into the in-app log; local SOCKS/HTTP proxies warn that other apps on
   the device can reach them; no broad package visibility beyond the documented
   split-tunneling need.
8. **Quality gate is green.** Go tests (including the mobile facade and Android
   provider logic that can be tested off-device), `go test -race`, Kotlin unit
   tests, Android lint, `assembleDebug`, Compose UI tests on an API 36 emulator,
   and the physical-device checklist all pass; no known P0/P1 defects.
9. **Release is shippable.** v0.5.0 ships signed per-ABI and universal APKs with
   `SHA256SUMS` on GitHub Releases, with the full Android doc set updated.
10. **No collateral change.** Desktop builds and tests pass unchanged; IPC
    protocol v1 is byte-identical; daemon, CLI, TUI and Windows GUI behavior is
    untouched.

## Scope

### In scope

- Native Android app (`dev.benyamin.kumiho`, label `Kumiho`) built with Kotlin and
  Jetpack Compose, API 26+ (`arm64-v8a`, `armeabi-v7a`).
- Real Android platform provider: VpnService fd adoption, app-private paths,
  no-op netconfig (the Kotlin builder applies the interface), Android doctor
  checks, IPv6 blackhole.
- `gomobile` AAR embedding the Go engine in-process, with a JSON command facade
  and a Kotlin-implemented device bridge.
- Screens: setup, Dashboard, Locations, Settings, Account, Logs, Diagnostics,
  More sheet, connect-options sheet, dialogs.
- Android surfaces: foreground service, rich notification, Quick Settings tile,
  per-app split tunneling (both directions), Always-on/lockdown integration,
  battery-optimization diagnostics, manual update check.
- App preferences in DataStore, independent from desktop settings.
- Build scripts, local CLI toolchain setup, CI job, signing, release packaging.
- Tests and documentation.

### Non-goals (explicitly out)

- Google Play or F-Droid distribution; sideloaded GitHub Releases only.
- Home-screen widget, launcher long-press shortcuts, Android TV, Wear OS, Auto.
- iOS, macOS, or any non-Android platform.
- Localization; English-only UI (logical start/end layout is still used).
- Material You / dynamic color.
- Config import/export or cross-device sync.
- IPv6 tunneling; IPv6 is blackholed, not carried.
- Telemetry, analytics, or remote crash reporting.
- A background daemon, local IPC server, or protocol changes on Android.
- Desktop kill-switch or autoconnect enforcement on Android; the OS owns those.
- Auto-update or in-app APK installation.
- Android 5.x–7.x, x86/x86_64 release ABIs, or tablets as a separate app.
- Any change to the existing daemon, IPC v1, CLI, TUI, GUI, or their behavior.

## Decision log

| # | Decision | Rationale | Status | Source |
| --- | --- | --- | --- | --- |
| D1 | v1 bar is full desktop parity plus mobile polish; no corner cuts | User's explicit bar; parity is the project's standard | decided | user |
| D2 | API 26+, `arm64-v8a` + `armeabi-v7a` | Matches FoxyVPN's floor and covers old phones and both common ABIs | decided | user |
| D3 | Phone-first, tablet-aware: bottom bar on phones, navigation rail at ≥600dp | One codebase, deliberate large-screen behavior | decided | user |
| D4 | GitHub Releases sideload only; no Play/F-Droid | Avoids store policy/privacy overhead for a personal client | decided | user |
| D5 | Same repository, shared v0.5.0 version | One engine, one release, one documentation set | decided | user |
| D6 | Kotlin + Jetpack Compose (Material 3) | Native gestures, accessibility, notifications; FoxyVPN's proven stack | decided | user |
| D7 | Go engine embedded via a `gomobile` AAR, exposed through a JSON command facade | Reuses the tested engine; keeps the JNI surface small | decided | user |
| D8 | Foreground `VpnService` owns the engine and state; UI binds to it | Survives UI destruction and matches Android VPN expectations | decided | user |
| D9 | Bottom bar Home/Locations/Account/Settings + More (Logs, Diagnostics) | All six destinations stay thumb-reachable | decided | user |
| D10 | Dashboard is a power hero plus Location, Exit IP, Quota and Session cards | FoxyVPN's simplicity with Kumiho's data | decided | user |
| D11 | v1 includes QS tile, rich notification, per-app split tunneling and Always-on integration; no widget | High-utility Android surfaces, bounded scope | decided | user |
| D12 | Kill switch is shown as Android lockdown state, read-only, with a system-settings link | Only the OS can enforce lockdown; no false claims | decided | user |
| D13 | Autoconnect maps to Android Always-on VPN; no boot receiver | Reliable and policy-compliant | decided | user |
| D14 | Split tunneling supports both Exclude and Include modes; default Off | User wants both; Off is the safe first-run default | decided | user |
| D15 | DNS goes to Kumiho's in-tunnel DoH resolver; no Android Private DNS | Same strict failover chain as desktop | decided | user |
| D16 | Android default MTU is 1500 | Desktop's 8500 is inappropriate for mobile links | decided | user |
| D17 | IPv6 is captured via the VPN and blackholed; no leak | Prevents IPv6 bypass without an IPv6 tunnel | decided | user |
| D18 | House palette only: six accents, blue default, Light/Dark/OLED, no dynamic color | House system wins over Material You | decided | user |
| D19 | System Roboto, one family; house type scale | Native rendering, no bundled font | decided | user |
| D20 | Palette is computed by Go and delivered to Compose as JSON at runtime | One source of truth for colour | decided | user |
| D21 | Four-step setup: consent, sign-in, VPN + notification permissions, optional Always-on | Explains before asking, completes to Dashboard | decided | user |
| D22 | Settings grouped into sections with a collapsible Advanced group | All 19 rows reachable without a wall of switches | decided | user |
| D23 | Options bottom sheet: location, proxy-only, lockdown shortcut; compact country/city picker | One-tap common case, depth on the Locations screen | decided | user |
| D24 | Locations keeps the full country/city/server tree, filter and manual Test speed | Desktop parity; avoids mobile-data background probes | decided | user |
| D25 | Logs keep 2000 lines in memory, mirror to a rotating private file, export via share sheet | Supportability without storage permissions | decided | user |
| D26 | Diagnostics uses adapted doctor checks plus Reset local state | Android has no nftables/routes to clean; state reset is the useful analogue | decided | user |
| D27 | Tokens are app-private, no-backup, unencrypted in the sandbox; all backup disabled | Same model as desktop; avoids a second token store | decided | user |
| D28 | Native Compose Firefox Accounts flow; no browser fallback | Reuses the engine state machine and challenge solver | decided | user |
| D29 | No telemetry; keep a local uncaught-exception log and share logs manually | Matches Kumiho's privacy posture | decided | user |
| D30 | Notification shows state/location/uptime with Disconnect + open app; no Connect action | Honest while the service is not running | decided | user |
| D31 | Disconnect while lockdown is active is allowed, with an explanation and real-state refresh | Predictable; the OS may restart the VPN | decided | user |
| D32 | Manual GitHub releases update check; open the release page in a browser | No background calls or self-update | decided | user |
| D33 | Keep proxy-only and local SOCKS/HTTP proxies under Advanced, with a warning | Full parity; the warning is required because loopback is device-wide | decided | user |
| D34 | Battery optimization is a Diagnostics row, not a setup prompt | Foreground service is primary; no nagging | decided | user |
| D35 | Expressive spring motion, with reduced-motion honored | User's choice; departure from the house's restrained default | decided | user |
| D36 | Ship `docs/android.md`, `docs/android-testing.md`, and update README/AGENTS/CHANGELOG/NOTICE | Full supportability | decided | user |
| D37 | Application ID `dev.benyamin.kumiho`, launcher label `Kumiho` | Stable namespace; matches product | decided | user |
| D38 | Reuse the existing fox artwork as an adaptive icon with a monochrome layer | Brand continuity | decided | user |
| D39 | Android settings are independent; no import/export/sync | No migration or sync surface | decided | user |
| D40 | English-only UI | Matches desktop; avoids double copy review | decided | user |
| D41 | Testing: Go unit + Kotlin unit + Compose emulator tests + physical-device checklist | VPN behavior needs a real device | decided | user |
| D42 | CI builds/tests Android on every push; tags also build and sign release APKs | Catch breakage early, automate releases | decided | user |
| D43 | Local CLI toolchain: JDK 17, SDK 36, NDK 28.2, gomobile, setup script; no IDE required | Keeps the dev box headless | decided | user |
| D44 | Dedicated release keystore in GitHub Actions secrets with an offline backup | Safe upgrades | decided | user |
| D45 | Per-ABI APKs plus one universal fallback APK | Small common download, safe fallback | decided | user |
| D46 | Explicit performance targets: cold start ≤2s, updates ≤500ms, no idle polling, universal APK ≤60MB | Measurable acceptance | decided | user |
| D47 | No launcher long-press shortcuts | QS tile covers fast access | decided | user |
| D48 | Local crash log replayed into the in-app log on next start | Debuggability without telemetry | decided | user |
| D49 | Route exclusions: `allow_lan` excludes RFC1918, link-local and multicast; `exclude_cidrs` excludes the listed IPv4 prefixes. API 33+ uses `Builder.excludeRoute`; API 26–32 uses the computed complement routes. IPv6 stays blackholed and is never excluded. Include mode omits Kumiho from the allowed list; Exclude mode adds it to the disallowed list (Android allows only one list) | Full parity on every supported API without leaking IPv6 | decided | user |
| D50 | The palette bridge adds `theme.Palette(theme, accent)` returning the semantic roles as a map; `CSS` is refactored to use it and desktop output is unchanged | One source of truth without parsing CSS in Kotlin | decided | user |
| D51 | When Android Always-on starts the service, it auto-connects if signed in (the engine resolves the saved selection or REC); otherwise it posts a sign-in notification and leaves lockdown blocking | Always-on is only useful if the service establishes the interface | decided | user |
| D52 | Add an opt-in **Keep connections outside the tunnel** Android setting that calls `VpnService.Builder.allowBypass()`; Off by default, saved immediately, applied on the next connect, and Android lockdown still forces bypass off | Android destroys live TCP sockets of VPN-routed UIDs on every connect/disconnect unless the VPN is bypassable; `allow_lan`/`exclude_cidrs` only change routing. The setting preserves LAN and route-excluded sessions without weakening the default tunnel, and the docs state that public/tunneled connections still drop | decided | user |
| A1 | App preferences use Jetpack DataStore Preferences; no Room or SharedPreferences | Small typed key/value set, coroutine-friendly | assumed | recommendation |
| A2 | MVVM with StateFlow, one Activity, Navigation Compose 2.10.2 and a manual AppContainer; no Hilt/Koin | One module; least build complexity | assumed | recommendation |
| A3 | Layout: `android/` Gradle root, `android/app` module; Go facade `internal/mobile`; real provider `internal/platform/android`; generated AAR under `android/app/libs/` and not committed | Matches repo shape and hermetic build policy | assumed | recommendation |
| A4 | Toolchain versions: AGP 9.4.1, Gradle 9.6.0, Kotlin 2.4.21, Compose BOM 2026.09.00, Navigation 2.10.2, Lifecycle 2.11.0, JDK 17, NDK 28.2.13676358, compile/target SDK 36, min SDK 26 | Current stable, mutually compatible | assumed | recommendation |
| A5 | VpnService handshake: Kotlin builds the interface and passes a detached fd through a Go `DeviceBridge`; the Android provider adopts it; a user-level connect after disconnect requests a new fd, while a watchdog redial keeps the existing interface/fd; Kumiho is excluded from its own VPN | The only reliable way to combine a Kotlin VpnService with a Go TUN consumer | assumed | recommendation |
| A6 | Android provider's NetConfig is a no-op because the Kotlin builder already applied routes/DNS/MTU/split/route exclusions; paths are passed from Kotlin | Avoids a Java-call-from-Go dependency for the builder | assumed | recommendation |
| A7 | IPv6 is dropped in the Android data path even though DNS may return AAAA; apps fall back to IPv4 | No IPv6 leak and no IPv6 tunnel | assumed | recommendation |
| A8 | Split app lists live in DataStore; `QUERY_ALL_PACKAGES` is declared, the default list filters to launchable apps, and a toggle reveals system apps; Kumiho itself is always excluded | Meets the chosen picker behavior without Play constraints | assumed | recommendation |
| A9 | Log file rotates at 3×1MB; Reset local state clears server cache, logs and failure counters, never tokens | Bounded storage and a safe reset | assumed | recommendation |
| A10 | Android UI ignores engine `kill_switch`/`autoconnect` values and displays `VpnService.isAlwaysOn()`/`isLockdownEnabled()`; engine values remain for desktop | Honest Android state without changing engine semantics | assumed | recommendation |
| A11 | Update check calls `https://api.github.com/repos/benyamin-git/Kumiho/releases/latest` and opens the release page; no download | Simple, token-free, on demand | assumed | recommendation |
| A12 | Version code is `major*10000 + minor*100 + patch`; assets are `kumiho-v<version>-android-<abi>.apk` and `...-universal.apk` plus `SHA256SUMS`; secrets are `ANDROID_KEYSTORE_BASE64`, `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD` | Stable ordering and repeatable releases | assumed | recommendation |
| A13 | Consent copy is adapted from the desktop wizard; the tick is required; a denied VPN permission still completes setup but Connect re-prompts | Keeps setup honest and recoverable | assumed | recommendation |
| A14 | Accessibility floor and logical start/end layout apply in every screen, even though v1 is English-only | House hard floor and RTL readiness | assumed | recommendation |
| A15 | FoxyVPN is a UX reference only (MIT); no code is copied; AndroidX/Compose licenses are added to NOTICE | License hygiene | assumed | recommendation |
| A16 | The app is one Gradle module; no dynamic features; no Play policy constraints are assumed | Sideload-only scope | assumed | recommendation |
| A17 | CI uses `ubuntu-latest`, Go 1.27.1, `setup-android` and an API 36 emulator; the physical phone test is manual | Available tooling and the chosen testing bar | assumed | recommendation |
| A18 | Desktop code is untouched except additive `internal/mobile`, real `internal/platform/android` files, and the additive `theme.Palette` helper; protocol v1 stays byte-identical | No collateral change | assumed | recommendation |

## Architecture

### Processes and lifecycle

One app process hosts the UI and the engine. `KumihoApplication` creates the
manual `AppContainer`. `MainActivity` hosts Compose and binds to
`KumihoVpnService`, a foreground service declared with
`android:foregroundServiceType="specialUse"` and the
`android.app.PROPERTY_SPECIAL_USE_FGS_SUBTYPE="vpn"` property. The service owns
the `mobile.Session` (the embedded engine) and publishes state to the UI as a
`StateFlow`; the UI never owns tunnel state. The service is started for both
full-tunnel and proxy-only operation. A `QuickConnectTileService` drives the same
service. There is no boot receiver: Android Always-on VPN starts the service.
When `isAlwaysOn()` is true on a service start and the user is signed in, the
service immediately runs the same Connect path the UI uses (the engine resolves
the saved selection or REC); if signed out it posts a sign-in notification and
lockdown keeps blocking.

`VpnService.prepare()` is called before the first connect and again if consent
was revoked. A denied permission leaves the app usable but every Connect path
re-prompts and explains. `onRevoke` disconnects the engine, stops the foreground
state and refreshes the UI.

### Engine boundary (`internal/mobile`)

`gomobile bind` builds an AAR from a narrow Go package. The exposed surface is:

- `Start(pathsJSON string, bridge DeviceBridge, listener Listener) error`
- `Command(requestJSON string) (responseJSON string, err error)`
- `Palette(theme, accent string) (paletteJSON string, err error)`
- `Version() string`
- `Close() error`

Requests and snapshots use the existing `internal/api` JSON vocabulary
(`status`, `locations`, `select_location`, `ping`, `connect`, `disconnect`,
`settings_get`, `settings_set`, `account`, `login_*`, `logout`, `logs_*`,
`doctor`). The facade is a thin dispatcher over `engine.Controller`; it adds no
tunnel logic. `Listener` pushes status, ping results, log lines and one-shot
notices to Kotlin.

The Android platform provider lives in `internal/platform/android` behind the
`android` build tag. Stub files remain for non-Android compilation. `OpenDevice`
calls the Kotlin bridge to establish the VpnService interface and adopts the
returned fd with `os.NewFile` after setting it non-blocking. `NetConfig.Apply`
and `Cleanup` are no-ops because the Kotlin `VpnService.Builder` already applied
the interface; this is recorded in the provider's documentation. `Paths()` comes
from the app-private directories passed at `Start`. `BypassDialer` is a plain
dialer; Kumiho excludes its own package from the VPN so engine dials do not loop.

### VpnService interface and data path

Kotlin builds the interface with:

- session `Kumiho`, MTU from settings (default 1500);
- `10.8.0.2/32` and `fd00::2/128` addresses;
- `0.0.0.0/0` and `::/0` routes (IPv6 captured, then dropped by the engine);
- DNS server `10.8.0.2` (the engine's in-tunnel DoH listener);
- split-tunneling app filters: Include mode adds only the selected packages, so
  Kumiho is excluded automatically; Exclude mode adds every selected package
  plus Kumiho itself (Android forbids mixing the two lists);
- route exclusions for `allow_lan` and `exclude_cidrs`: `excludeRoute` on API
  33+, computed complement routes on API 26–32; IPv6 is never excluded.

`establish()` returns a `ParcelFileDescriptor`; Kotlin detaches the fd and passes
it to Go. The engine starts its gVisor netstack over that fd, the SOCKS5 listener,
the optional HTTP proxy, the DNS listener on `10.8.0.2:53`, the watchdog, pass
renewal and quota polling. On disconnect or revoke, Go closes the fd and the
service stops. A watchdog redial only replaces the upstream HTTP/2 session; the
VpnService interface and fd stay up, so Android keeps routing apps into the
tunnel without a re-establish or a lockdown gap. A new user connect after a
disconnect asks the bridge for a fresh fd.

The Android-only `doctor` checks validate VPN permission, Always-on/lockdown
state, notification permission, battery optimization, network reachability, DNS,
local ports, clock skew and engine state. Kotlin supplies Android facts to the
facade; Go keeps the cross-platform checks. The report is rendered by the
Diagnostics screen.

### Theme and UI

Go's `internal/gui/theme` remains the palette source. `Palette(theme, accent)`
returns the semantic roles as JSON; Kotlin maps them into a Compose
`KumihoColors` object and never uses raw hex in composables. First run follows
the system Light/Dark choice; OLED is opt-in. All six accents are available.
Roboto is the only family. The type scale is the house 12/14/16/20/24/32sp set.

The shell uses a single `Scaffold`: bottom bar under 600dp, navigation rail at
600dp and wider. Destinations are Home, Locations, Account, Settings, and More
(Logs, Diagnostics). The Dashboard is the power hero plus four cards. Connect
options open in a modal bottom sheet with a compact country/city picker,
proxy-only toggle and lockdown shortcut. Locations keeps the full tree, filter
and manual speed test. Settings is a grouped list with a collapsed Advanced
group. Account, Logs and Diagnostics follow the desktop inventories with
Android-native affordances (share sheet, system settings links, chips).

Setup is four steps: consent (tick required), Firefox Accounts sign-in, Android
VPN + notification permissions, and optional Always-on guidance. It completes to
the Dashboard. A denied permission is surfaced in place, not swallowed.

Motion uses expressive springs for the power control and standard Compose
transitions elsewhere; `prefers-reduced-motion`/Android's animator scale is
honored.

### Storage, security and privacy

The engine owns `config.toml`, `state.json`, `tokens.json`, `serverlist.json`
and its logs under the app's no-backup private storage. DataStore owns
theme/accent, onboarding completion, split mode/app lists, last update check and
other UI-only preferences. `allowBackup=false` and data-extraction rules exclude
everything; there is no cloud or device-to-device transfer. Tokens are not
Keystore-encrypted in v1; the Android sandbox is the boundary, matching the
desktop model. No telemetry, analytics or remote crash upload exists. An
uncaught exception is written to a local crash file and replayed into the log on
next start; the user can share a log snapshot through the system share sheet.

Local SOCKS5 and HTTP proxies bind loopback only and are documented as
device-wide: any app on the phone can use them while the tunnel is up. Proxy-only
mode uses the same foreground service without a TUN interface.

### Build, CI and release

The `android/` Gradle project uses the wrapper pinned to Gradle 9.6.0. A script
builds the AAR with `gomobile bind` for `android/arm` and `android/arm64` at
`-androidapi 26`, copies it to `android/app/libs/`, and Gradle assembles debug
and release variants. A setup script installs JDK 17, SDK 36, build tools, NDK
28.2 and gomobile on the dev box. Makefile targets expose AAR, debug APK, release
APK and test builds.

CI adds an Android job to every push: Go tests, AAR build, Kotlin unit tests,
lint, `assembleDebug`, and Compose emulator tests on API 36. Tag builds sign the
release with the dedicated keystore from secrets and produce the per-ABI and
universal APKs plus `SHA256SUMS`; no signing material is committed.

### Testing

- Go: facade dispatch, Android provider fd adoption, IPv6 drop, route-complement
  calculation, path handling, doctor facts and theme JSON; `go test -race ./...`
  remains green.
- Kotlin: view models, settings mapping, split-mode state, notification and tile
  state reducers, update-check parsing, VpnService builder inputs.
- Compose: setup, Dashboard states, Locations filter, Settings grouping, Logs
  and Diagnostics on an emulator in all three themes.
- Physical phone: connect/disconnect, Always-on boot, lockdown restart, QS tile,
  notification action, split tunneling in both modes, route exclusions on an
  API 26–32 device and an API 33+ device, IPv6 leak check, proxy-only, local
  proxies, battery diagnostics, permission denial/revocation and reconnect.
- Desktop regression: the existing every-commit gate remains unchanged.

## Risks + rollback

- **`gomobile` is experimental.** The AAR, Kotlin-implemented Go interface and
  JSON facade are the riskiest integration. Mitigation: make a probe the first
  milestone (build the AAR, call a method, receive a callback, adopt a test fd,
  bind the in-tunnel DNS listener and answer a query);
  pin `golang.org/x/mobile`; keep the facade small. If it fails, the fallback is
  a hand-written JNI/C-shared wrapper or a separate-process design, both of which
  require a re-plan before UI work continues.
- **Callback lifetime and threading.** A Kotlin bridge object passed to Go must be
  kept alive and called on a safe thread. Mitigation: one long-lived bridge owned
  by the service, main-thread dispatch in Kotlin, and tests that destroy the
  activity while connected.
- **16KB page-size devices.** Go arm64 links with large alignment, but the AAR
  packaging and zip alignment must be verified. Mitigation: build an API 36 16KB
  emulator image, run `zipalign -c -P 16`, and inspect `p_align` on the `.so`.
- **VpnService fd/reconnect lifecycle.** The interface/fd must survive upstream
  watchdog redials and be replaced only on a user reconnect; `onRevoke` can race
  teardown. Mitigation: one bridge-owned lock, generation checks mirroring the
  engine, and a device test that kills the network mid-session and one that
  revokes consent while connected.
- **Pre-33 route complement.** A broad `exclude_cidrs` can expand into many
  routes; a bug can strand the tunnel or leak traffic. Mitigation: unit-test the
  prefix complement against a reference implementation, cap the result at 256
  routes, reject `0.0.0.0/0`, and verify on API 26 and API 36 devices.
- **In-tunnel DNS bind.** The engine's resolver is a normal socket on
  `10.8.0.2:53`. If Android will not deliver the VPN interface's own address to
  an app that is excluded from its own VPN, full mode cannot resolve. Mitigation:
  make establish → bind → query from a second app part of the first probe; stop
  and re-plan if it fails.
- **OEM Always-on and battery killers.** Some vendors delay or refuse Always-on
  starts. Mitigation: reflect real state, document the limitation, and keep the
  foreground service. Never claim enforcement the OS did not provide.
- **Split tunneling × lockdown.** The OS may ignore per-app filters under
  lockdown or restart the service. Mitigation: apply filters at `establish`, show
  the active mode, and test the combination on the physical phone.
- **IPv6 fallback.** Dropping IPv6 can delay apps that try AAAA first.
  Mitigation: keep the blackhole, document it, and measure on the phone; if
  needed, suppress AAAA in the resolver as a follow-up.
- **APK size.** The Go runtime plus gVisor is large. Mitigation: per-ABI splits,
  release shrinking, and the 60MB universal cap.
- **Firefox Accounts captcha.** The engine solver handles PoW/PAT/clientmetrics
  but not an interactive captcha. Mitigation: a clear error naming the
  limitation; no browser fallback in v1.
- **Release-key loss.** Losing the keystore prevents upgrades. Mitigation: offline
  backup plus documented recovery.
- **Local toolchain absence.** The dev box has no Java/SDK/NDK today. Mitigation:
  the setup script is the first plan task and is verified before code work.
- **Rollback.** Everything is additive: `android/`, `internal/mobile`, the real
  `internal/platform/android` files and the additive `theme.Palette` helper.
  Removing them restores the compile-only stub; desktop builds, tests, protocol
  v1 and releases are unaffected. No data migration exists; uninstalling the
  Android app removes its private state.

## Dependencies / ordering

1. Toolchain setup and a `gomobile` + fd + DNS probe.
2. Android provider + mobile facade + engine tests.
3. Compose shell, palette bridge and theme.
4. Setup, Dashboard, Locations, Settings.
5. Account, Logs, Diagnostics, More/Options sheets.
6. Split tunneling, Always-on/lockdown, notification, QS tile, proxy-only.
7. Build scripts, CI, signing, release assets.
8. Docs and physical-device acceptance.

The provider/facade must land before any screen that connects. The palette bridge
must land before theme work. Split filters must be part of the VpnService builder
before the split UI is tested. CI signing is last because it depends on a stable
release build.

## Open questions

None. Every decision above is decided or explicitly assumed; assumed items are
flagged for review at the spec gate.
