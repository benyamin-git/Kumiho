# Kumiho Android TV Support — Implementation Plan

**Spec:** [`docs/superpowers/specs/2026-10-26-android-tv-design.md`](../specs/2026-10-26-android-tv-design.md)
**Status:** Approved
**Date:** 2026-10-26
**Branch:** `android-tv` in `.local/share/opencode/worktree/5444da/android-tv`

## Global constraints

Copied from the approved spec; every task must preserve these.

- **Platform:** Android TV + Google TV, API 26+, `arm64-v8a`/`armeabi-v7a`.
  Physical target: Sony X90L, Android 12 / API 31. Emulator target: Google TV
  API 36 x86_64, `tv_1080p`.
- **One APK:** application ID `dev.benyamin.kumiho`, debug suffix `.debug`;
  phone and TV share the same package. Sideload-only; no Play/F-Droid, no
  separate TV APK or package.
- **Version:** v0.7.0, `versionCode` 700; tag/push remain out of scope.
- **Toolchain:** JDK 17, AGP 9.4.1, Gradle 9.6.0, Kotlin 2.4.21, Compose BOM
  2026.09.00 (Compose 1.12.1), `compileSdk` 37.2, `targetSdk` 36, `minSdk` 26,
  NDK 28.2.13676358.
- **TV libraries:** `androidx.tv:tv-material:1.1.0`,
  `androidx.tv:tv-foundation:1.0.0`, `androidx.tvprovider:tvprovider:1.1.0`.
- **TV design:** 16/18/20/24/32/40sp; 48dp/27dp overscan; 8dp grid; 48dp
  minimum targets; focus ring + subtle scale; Light/Dark/OLED; six accents;
  no dynamic color.
- **Naming:** `ui.tv`, `TvEnvironment`, `TvAppShell`, `TvDashboardRoute`,
  `TvSetupFlow`, `TvChannelPublisher`, `TvLauncherActivity`,
  `make android-tv-emulator-test`, CI job `android-tv`, docs
  `docs/android-tv.md`.
- **Non-goals:** no Watch Next media, voice search, gamepad mapping, TV input
  service, ambient/screensaver control, multi-user profiles, separate TV app,
  or Play distribution.
- **No collateral change:** no Go engine, gomobile facade, IPC v1, daemon, CLI,
  TUI, Windows GUI or desktop release changes. Phone UI and behavior stay
  unchanged.
- **Security:** only new permission is `WRITE_EPG_DATA`; tokens stay
  app-private; `allowBackup=false`; no telemetry; WebView cookies cleared on
  sign-out.
- **Commits:** one logical change per task; `type: lowercase imperative
  summary`; never push/tag unless asked. Every commit leaves the existing Go
  and Android gates green.

## Task ordering

```
T1 dependency/toolchain probe
  -> G1 background VPN probe (release blocker; no commit)
  -> T2 entry point + manifest + assets
  -> T3 TV theme + focus
  -> T4 TV shell + navigation
  -> T5 Dashboard
  -> T6 Setup + sign-in
  -> T7 Account
  -> T8 Locations
  -> T9 Settings + split tunneling
  -> T10 Logs + Diagnostics + update
  -> T11 launcher channel (probe-gated)
  -> T12 complete TV connected coverage
  -> T13 CI job
  -> T14 version bump
  -> T15 docs
  -> T16 full gates + physical X90L acceptance
```

T7-T10 are independent of each other after T6 and may be reordered, but each
must keep the phone suite green. T11 must not block T7-T10. If the T1, T6 or
T11 probe fails, stop and follow the risk gate rather than forcing the task.

## T1 — TV dependencies, emulator harness and probe

**Depends on:** nothing.
**Commit:** `feat: add android tv dependencies and emulator harness`

**Files**
- `android/gradle/libs.versions.toml`: add versions `tv = "1.1.0"`,
  `tvFoundation = "1.0.0"`, `tvProvider = "1.1.0"`; libraries
  `androidx-tv-material`, `androidx-tv-foundation`, `androidx-tvprovider`.
- `android/app/build.gradle.kts`: add the three `implementation` dependencies.
- `scripts/android-setup.sh`: add
  `system-images;android-36;google-tv;x86_64` to every package list
  (`ensure_sdk_packages`, the `--ci` install path and `check_mode`).
- `scripts/android-tv-connected-test.sh`: new wrapper like
  `scripts/android-connected-test.sh` with
  `-Pandroid.testInstrumentationRunnerArguments.package=dev.benyamin.kumiho.tv`.
- `Makefile`: add `android-tv-emulator-test` to `.PHONY` and the target:
  build the emulator AAR, then run the TV connected script.
- `android/app/src/androidTest/java/dev/benyamin/kumiho/tv/TvDependencyProbeTest.kt`:
  classpath compile probe; assert
  `TvContractCompat.Channels.CONTENT_URI.authority == "android.media.tv"` and
  include an unused `@Composable` that references
  `androidx.tv.material3.MaterialTheme` so tv-material compiles.

**Interfaces**
- `scripts/android-tv-connected-test.sh` honors `GRADLE` and `GRADLE_FLAGS`
  exactly like the phone wrapper.
- `make android-tv-emulator-test` runs:
  `bash scripts/android-build-aar.sh --emulator` then the TV wrapper.

**Verification**
1. `source scripts/android-env.sh && bash scripts/android-setup.sh --check`
   exits 0 and reports the TV image installed.
2. `./gradlew :app:dependencies --configuration debugRuntimeClasspath | grep -E 'androidx.tv:(tv-material|tv-foundation)|androidx.tvprovider'`
   shows all three (run without `--offline` the first time so Gradle can
   resolve the new Maven artifacts).
3. `make android-test` succeeds (unit tests, lint, debug APK; androidTest
   compiles).
4. Create the local AVD once:
   `echo no | avdmanager create avd -n kumiho-tv-api36 -k "system-images;android-36;google-tv;x86_64" -d tv_1080p --force`;
   boot it with `emulator -avd kumiho-tv-api36 -no-window -no-audio -no-boot-anim -gpu swiftshader_indirect -no-snapshot`.
5. `make android-tv-emulator-test` exits 0 and runs the probe test.

If step 2 or 3 exposes a Compose-for-TV/Compose 1.12 incompatibility, execute
the D28 fallback before proceeding: keep `tv-foundation`, remove
`tv-material` UI usage from T3, and implement the same focus ring with house
Material3 components. Record the fallback in the spec/plan notes.

## G1 — Background VPN probe (release blocker, no commit)

**Depends on:** T1. Run this before T2; it is the go/no-go gate for the whole
feature.

Use the current phone UI on the TV (the TV entry point does not exist yet) to
sign in and connect; the probe is about Android TV's service lifecycle, not the
layout.

1. Build and install the debug APK on the Google TV emulator and the X90L:
   `make android-debug`;
   `adb install -r android/app/build/outputs/apk/debug/app-debug.apk`;
   `adb shell am start -n dev.benyamin.kumiho.debug/dev.benyamin.kumiho.MainActivity`
   (the TV launcher entry does not exist until T2).
2. Sign in with Firefox Accounts, grant VPN consent, connect, and confirm real
   traffic/exit IP.
3. Press Home, open a streaming app or the TV launcher, wait at least 60s.
4. Verify the service is alive:
   `adb shell dumpsys activity services dev.benyamin.kumiho.debug | grep -A4 KumihoVpnService`.
5. Verify traffic still uses the tunnel (browse in the other app or check the
   app's status/exit IP after returning); confirm no `onRevoke`/stop.
6. Repeat once with a watchdog redial (toggle Wi-Fi/network briefly).

**Expected:** the service remains alive, the tunnel survives, and reconnecting
after disconnect still gets a fresh fd. If the TV kills the service or the
tunnel drops, **stop**: do not start T2. Re-plan with system Always-on binding
or another sound mechanism. Do not add a fake media foreground-service type.

## T2 — TV entry point, manifest and launcher assets

**Depends on:** T1, G1.
**Commit:** `feat: add android tv entry point and launcher metadata`

**Files**
- `android/app/src/main/java/dev/benyamin/kumiho/tv/TvEnvironment.kt`
- `android/app/src/test/java/dev/benyamin/kumiho/tv/TvEnvironmentTest.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/MainActivity.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/App.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/TvRoot.kt` (temporary
  themed TV placeholder; replaced in T4)
- `android/app/src/main/AndroidManifest.xml`
- `android/app/src/main/res/drawable-xhdpi/tv_banner.png` (320x180)
- `android/app/src/main/res/mipmap-xhdpi/ic_launcher_tv.png` (160x160 fallback;
  the existing adaptive icon remains the primary `android:icon`)
- `android/app/src/main/res/values/strings.xml` (any TV placeholder copy)

**Interfaces**
```kotlin
object TvEnvironment {
    fun isTelevision(context: Context): Boolean
    fun isTelevision(uiMode: Int, hasLeanback: Boolean): Boolean
}
```
`isTelevision` is true when
`uiMode and Configuration.UI_MODE_TYPE_MASK == UI_MODE_TYPE_TELEVISION` or the
package manager has `FEATURE_LEANBACK`.

```kotlin
@Composable
fun KumihoApp(
    container: AppContainer,
    isTv: Boolean,
    tvConnectRequested: Boolean = false,
)
```
`MainActivity` computes `TvEnvironment.isTelevision(this)` and passes it;
`tvConnectRequested` is wired in T11 but defaults to false here. The phone
branch remains behaviorally identical; the TV branch renders `TvRoot`
(placeholder until T4).

Manifest additions:
- `<uses-feature android:name="android.software.leanback" android:required="false" />`
- `<uses-feature android:name="android.hardware.touchscreen" android:required="false" />`
- `android:banner="@drawable/tv_banner"` on `<application>`
- `LEANBACK_LAUNCHER` category alongside the existing `LAUNCHER` filter on
  `MainActivity`
- no orientation lock.

Banner/icon: derive from
`android/app/src/main/res/mipmap-xxxhdpi/ic_launcher_foreground.png`; house blue
`#35639c` background; banner includes the fox and the word **Kumiho**.

**Verification**
1. `make android-test` succeeds.
2. `adb -s emulator-5554 shell cmd package query-activities --brief -a android.intent.action.MAIN -c android.intent.category.LEANBACK_LAUNCHER`
   lists `dev.benyamin.kumiho.debug/.MainActivity`.
3. `identify -format '%wx%h' android/app/src/main/res/drawable-xhdpi/tv_banner.png`
   prints `320x180`; the TV icon prints `160x160`.
4. Install on the TV emulator and confirm the launcher shows the Kumiho banner
   and the app opens to the themed placeholder.
5. `make android-emulator-test` still passes the phone shell suite.

## T3 — TV theme, tokens and focus components

**Depends on:** T2.
**Commit:** `feat: add tv theme and focus components`

**Files**
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/theme/TvTheme.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/theme/TvTypography.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/theme/TvDimens.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/components/TvFocus.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/components/TvComponents.kt`
- `android/app/src/test/java/dev/benyamin/kumiho/ui/tv/theme/TvTypographyTest.kt`
- `android/app/src/androidTest/java/dev/benyamin/kumiho/tv/TvFocusTest.kt`

**Interfaces**
```kotlin
@Composable
fun TvKumihoTheme(colors: KumihoColors, content: @Composable () -> Unit)

val TvTypography: androidx.tv.material3.Typography

object TvDimens {
    val OverscanHorizontal: Dp = 48.dp
    val OverscanVertical: Dp = 27.dp
    val Space1: Dp = 8.dp
    val Space2: Dp = 16.dp
    val Space3: Dp = 24.dp
    val Space4: Dp = 32.dp
    val MinTarget: Dp = 48.dp
}

@Composable
fun TvFocusableSurface(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    shape: Shape = MaterialTheme.shapes.medium,
    content: @Composable () -> Unit,
)
```
`TvKumihoTheme` maps every semantic `KumihoColors` role into the TV Material
color scheme; no raw hex. `TvTypography` uses exactly 16/18/20/24/32/40sp.
`TvFocusableSurface` draws a 2dp `primary` ring and scales to 1.03 on focus,
returns to 1.0 when unfocused, and exposes a button role. If the D28 fallback
is active, provide the same signatures with house Material3 components.

**Verification**
1. `make android-test` succeeds.
2. `TvTypographyTest` asserts the six font sizes and the TV font family.
3. `TvFocusTest` on the TV emulator renders three focusable surfaces, sends
   `KEYCODE_DPAD_RIGHT`/`DOWN`, and asserts the focused node changes in visual
   order and the focused node has a non-zero border/scale state.
4. A screenshot harness renders Light, Dark and OLED; the design self-check
   confirms the ring and text are visible.

## T4 — TV shell and navigation

**Depends on:** T3.
**Commit:** `feat: add tv shell and navigation`

**Files**
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/TvRoot.kt`
  (replace placeholder with palette/setup gate + shell)
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/TvAppShell.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/TvTopBar.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/TvMorePage.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/TvPlaceholderScreen.kt`
- `android/app/src/androidTest/java/dev/benyamin/kumiho/tv/TvShellTest.kt`

**Interfaces**
```kotlin
@Composable
fun TvRoot(
    container: AppContainer,
    tvConnectRequested: Boolean = false,
    modifier: Modifier = Modifier,
)

@Composable
fun TvAppShell(
    nav: ShellNavigation,
    home: @Composable () -> Unit,
    locations: @Composable () -> Unit,
    account: @Composable () -> Unit,
    settings: @Composable () -> Unit,
    logs: @Composable () -> Unit,
    diagnostics: @Composable () -> Unit,
    tvConnectRequested: Boolean = false,
    modifier: Modifier = Modifier,
)
```
`TvAppShell` owns the only TV `Scaffold`, the left rail (Home, Locations,
Account, Settings, More), the 48dp/27dp safe frame and the top bar. More opens
`TvMorePage`, not a bottom sheet. Back from a tab exits to the launcher; Back
from More returns to the tab. Initial focus is Home in the rail, then the
content.

**Verification**
1. `make android-test` succeeds.
2. `TvShellTest` on the TV emulator launches the shell, traverses all five rail
   items with D-pad, opens More, opens Logs and Diagnostics placeholders, and
   verifies Back returns.
3. A TV screenshot shows the rail and content inside the overscan frame.
4. Phone shell tests are unchanged.

## T5 — TV Dashboard and connect options

**Depends on:** T4.
**Commit:** `feat: add tv dashboard`

**Files**
- `android/app/src/main/java/dev/benyamin/kumiho/ui/dashboard/DashboardScreen.kt`
  (additive `content` slot to `DashboardRoute`)
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/dashboard/TvDashboardRoute.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/dashboard/TvDashboardScreen.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/dashboard/TvDashboardCards.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/dashboard/TvConnectOptionsPage.kt`
- `android/app/src/androidTest/java/dev/benyamin/kumiho/tv/TvDashboardTest.kt`

**Interfaces**
```kotlin
@Composable
fun DashboardRoute(
    container: AppContainer,
    modifier: Modifier = Modifier,
    content: @Composable (DashboardState, DashboardActions) -> Unit = { state, actions ->
        DashboardScreen(state = state, actions = actions, modifier = modifier)
    },
)

@Composable
fun TvDashboardRoute(
    container: AppContainer,
    modifier: Modifier = Modifier,
    connectRequested: Boolean = false,
)
```
`TvDashboardRoute` calls `DashboardRoute` with `TvDashboardScreen`.
`connectRequested` is false until T11 wires the launcher Connect card; T11
consumes it once after the engine state is ready. The TV screen
is two columns: left power hero/state/uptime/options; right Location, Quota and
Session cards. `TvConnectOptionsPage` replaces the modal bottom sheet and uses
the same `DashboardState`/`DashboardActions`. The phone route and screen are
untouched otherwise.

**Verification**
1. `make android-test` succeeds.
2. `TvDashboardTest` on the TV emulator asserts the power control, state word,
   three cards, options page, and the disconnect-first hint; D-pad reaches
   every action.
3. Phone `DashboardScreenTest` and screenshots still pass.
4. TV screenshots for connected, off, permission-needed in Light/Dark/OLED.

## T6 — TV setup and sign-in

**Depends on:** T4.
**Commit:** `feat: add tv setup and sign-in`

**Files**
- `android/app/src/main/java/dev/benyamin/kumiho/ui/setup/SetupFlow.kt`
  (extract `SetupHost`; add `flow` slot to `SetupRoute`)
- `android/app/src/main/java/dev/benyamin/kumiho/ui/setup/SetupHost.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/setup/TvSetupRoute.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/setup/TvSetupChrome.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/setup/TvStepContent.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/account/MozillaAccountBrowserDialog.kt`
  / `MozillaAccountWebView.kt` (TV focus handling; shared by phone)
- `android/app/src/androidTest/java/dev/benyamin/kumiho/tv/TvSetupTest.kt`

**Interfaces**
```kotlin
@Composable
fun SetupHost(
    viewModel: SetupViewModel,
    content: @Composable (SetupState, SetupViewModel) -> Unit,
)

@Composable
fun SetupRoute(
    container: AppContainer,
    modifier: Modifier = Modifier,
    flow: @Composable (SetupViewModel) -> Unit = { SetupFlow(it, modifier) },
)

@Composable
fun TvSetupFlow(viewModel: SetupViewModel, modifier: Modifier = Modifier)

@Composable
fun TvSetupRoute(container: AppContainer, modifier: Modifier = Modifier)
```
`TvRoot` calls `TvSetupRoute` while setup is incomplete; `TvSetupRoute` calls
`SetupRoute(container, flow = { TvSetupFlow(it) })`. `SetupHost` owns the VPN
consent launcher, notification launcher, lifecycle
resume refresh, BackHandler and browser dialog. Phone `SetupFlow` becomes a
thin phone chrome over `SetupHost`; behavior and test tags stay identical. TV
chrome renders the same four steps with TV copy, type, spacing and focus. The
notification step remains; API 31 has no runtime prompt. Always-on shows the
real state and permits finish when unavailable.

**Sign-in probe gate:** before declaring T6 done, run the actual Firefox
Accounts WebView on the TV emulator and on the X90L:
- D-pad reaches the email field, submit, password field and submit; the system
  IME opens for text entry.
- If the probe fails, switch `TvStepContent` to the native password form as the
  default TV sign-in and keep the WebView only as the recovery path. Record the
  result in the spec/plan notes and docs.

**Verification**
1. `make android-test` succeeds; phone `SetupFlowTest` passes unchanged.
2. `TvSetupTest` on the TV emulator walks consent -> sign-in -> permissions ->
   Always-on with D-pad, verifies the consent gate, a denied permission still
   permits finish, and Connect-now is offered only when allowed.
3. Manual probe above succeeds, or the password-first fallback is committed and
   the same test walks the native form.
4. Browser dialog returns focus to the sign-in step when closed.

## T7 — TV Account

**Depends on:** T6.
**Commit:** `feat: add tv account screen`

**Files**
- `android/app/src/main/java/dev/benyamin/kumiho/ui/account/AccountScreen.kt`
  (additive `content` slot on `AccountRoute`)
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/account/TvAccountRoute.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/account/TvAccountScreen.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/account/TvAccountDialogs.kt`
- `android/app/src/androidTest/java/dev/benyamin/kumiho/tv/TvAccountTest.kt`

**Interfaces**
```kotlin
@Composable
fun AccountRoute(
    container: AppContainer,
    modifier: Modifier = Modifier,
    content: @Composable (AccountUiState, AccountActions) -> Unit = { state, actions ->
        AccountScreen(state = state, actions = actions, modifier = modifier)
    },
)
```
TV screen uses the same account state/actions: five-account list, add, use,
re-auth, rename, remove, rotation and threshold. Add/re-auth calls the T6 TV
sign-in path. Sign-out confirmation and notices are TV focus-trapped dialogs.

**Verification**
1. `make android-test` succeeds; phone account tests pass.
2. `TvAccountTest` walks the account list, opens add, uses the TV sign-in path,
   opens rotation, and confirms a removal dialog with D-pad.
3. Manual TV check: account actions remain reachable while the engine snapshot
   is loading or failed.

## T8 — TV Locations

**Depends on:** T4.
**Commit:** `feat: add tv locations screen`

**Files**
- `android/app/src/main/java/dev/benyamin/kumiho/ui/locations/LocationsScreen.kt`
  (additive `content` slot on `LocationsRoute`)
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/locations/TvLocationsRoute.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/locations/TvLocationsScreen.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/locations/TvLocationTree.kt`
- `android/app/src/androidTest/java/dev/benyamin/kumiho/tv/TvLocationsTest.kt`

**Interfaces**
```kotlin
@Composable
fun LocationsRoute(
    container: AppContainer,
    modifier: Modifier = Modifier,
    content: @Composable (LocationsState, LocationsActions) -> Unit = { state, actions ->
        LocationsScreen(state = state, actions = actions, modifier = modifier)
    },
)
```
TV screen keeps search, Ping all, expand/collapse, recommended sorting, country
-> server tree and the connected hint. Search opens the system IME; focus
restoration returns to the search/trigger after closing it.

**Verification**
1. `make android-test` succeeds; phone Locations tests pass.
2. `TvLocationsTest` with a long fake tree: D-pad traverses country rows and
   server rows, Ping all updates values without losing focus, search opens and
   clears, selected row is reachable.
3. No touch-only affordance is required; every row is selectable by D-pad.

## T9 — TV Settings and Split tunneling

**Depends on:** T4.
**Commit:** `feat: add tv settings and split tunneling`

**Files**
- `android/app/src/main/java/dev/benyamin/kumiho/ui/settings/SettingsScreen.kt`
  (additive `content` slot on `SettingsRoute`; `openRelease` parameter)
- `android/app/src/main/java/dev/benyamin/kumiho/ui/settings/SplitTunnelScreen.kt`
  (additive `content` slot on `SplitTunnelRoute`)
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/settings/TvSettingsRoute.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/settings/TvSettingsScreen.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/settings/TvSettingEditors.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/settings/TvSplitTunnelRoute.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/settings/TvSplitTunnelScreen.kt`
- `android/app/src/androidTest/java/dev/benyamin/kumiho/tv/TvSettingsTest.kt`

**Interfaces**
```kotlin
@Composable
fun SettingsRoute(
    container: AppContainer,
    modifier: Modifier = Modifier,
    openRelease: ((String) -> Unit)? = null,
    content: @Composable (SettingsState, SettingsAppState, SettingsActions) -> Unit = { state, appState, actions ->
        SettingsScreen(state = state, appState = appState, actions = actions, modifier = modifier)
    },
)

@Composable
fun SplitTunnelRoute(
    container: AppContainer,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
    content: @Composable (SplitTunnelState, Boolean, SplitTunnelActions) -> Unit = { state, reconnect, actions ->
        SplitTunnelScreen(state = state, reconnectRequired = reconnect, actions = actions, modifier = modifier)
    },
)
```
`openRelease` defaults to the existing external browser behavior; TV passes an
in-app WebView opener (T10). TV Settings renders all 21 catalog rows, grouped,
with the same Advanced behavior and Android-owned rows. TV setting editors use
TV dialogs and the system IME. Split tunneling keeps both modes, search,
include/exclude lists and system-app toggle; Back returns to Settings.

**Verification**
1. `make android-test` succeeds; phone Settings/Split tests pass.
2. `TvSettingsTest` counts/reaches all app, connection, Android VPN and Advanced
   rows; opens an editor, saves a value, opens split tunneling, toggles a mode
   and returns.
3. D-pad traversal never traps in the Advanced group or a long split list.
4. No phone-only modal bottom sheet is used.

## T10 — TV Logs, Diagnostics and update

**Depends on:** T4, T9.
**Commit:** `feat: add tv logs diagnostics and updates`

**Files**
- `android/app/src/main/java/dev/benyamin/kumiho/ui/logs/LogsScreen.kt`
  (additive `content` slot on `LogsRoute`)
- `android/app/src/main/java/dev/benyamin/kumiho/ui/diagnostics/DiagnosticsScreen.kt`
  (additive `content` slot on `DiagnosticsRoute`)
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/logs/TvLogsRoute.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/logs/TvLogsScreen.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/logs/TvLogExport.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/diagnostics/TvDiagnosticsRoute.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/diagnostics/TvDiagnosticsScreen.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/settings/TvUpdateWebView.kt`
- `android/app/src/androidTest/java/dev/benyamin/kumiho/tv/TvLogsDiagnosticsTest.kt`
- `android/app/src/test/java/dev/benyamin/kumiho/ui/tv/TvLogExportTest.kt`

**Interfaces**
```kotlin
@Composable
fun LogsRoute(
    container: AppContainer,
    modifier: Modifier = Modifier,
    content: @Composable (LogsUiState, LogsActions, String?) -> Unit = { state, actions, launchError ->
        LogsScreen(state = state, actions = actions, launchError = launchError, modifier = modifier)
    },
)

@Composable
fun DiagnosticsRoute(
    container: AppContainer,
    modifier: Modifier = Modifier,
    content: @Composable (
        DiagnosticsUiState,
        Boolean,
        SplitMode,
        Int,
        DiagnosticsActions,
        String?,
    ) -> Unit = { state, serviceBound, splitMode, splitAppCount, actions, launchError ->
        DiagnosticsScreen(
            state = state,
            serviceBound = serviceBound,
            splitMode = splitMode,
            splitAppCount = splitAppCount,
            actions = actions,
            modifier = modifier,
            launchError = launchError,
        )
    },
)

object TvLogExport {
    fun write(context: Context, entries: List<LogEntry>): File
}
```
`TvLogExport.write` uses `LogExporter.writeTo(LogExporter.exportsDir(context.cacheDir), entries, Instant.now())`
and returns the file. The TV Logs screen shows the absolute path in a focusable
dialog and offers **Copy path**; it offers Share only when
`Intent.createChooser` resolves to an activity. Diagnostics shows the existing
doctor report plus TV facts (UI mode, Leanback feature, model, OS/API,
Always-on/lockdown). The update check keeps the existing manual network call;
TV opens `TvUpdateWebView` for the release URL and falls back to version + URL
text if the WebView cannot load.

**Verification**
1. `make android-test` succeeds; phone Logs/Diagnostics tests pass.
2. `TvLogExportTest` writes a temp file and asserts non-empty, private app path.
3. `TvLogsDiagnosticsTest` opens Logs, exports, sees the path dialog, opens
   Diagnostics, runs the report and toggles reset confirmation with D-pad.
4. TV update dialog shows the current version and opens the WebView; a forced
   load failure shows the text fallback.
5. Manual X90L: logs path is readable, update page loads, no external browser
   is required.

## T11 — Google TV launcher channel (probe-gated)

**Depends on:** T2, T4, T6.
**Commit:** `feat: add google tv launcher channel` (only if the probe passes;
otherwise do not commit and record the drop)

**Files**
- `android/app/src/main/java/dev/benyamin/kumiho/tv/TvChannelPublisher.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/tv/TvChannelReceiver.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/tv/TvLauncherActivity.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/data/AppPrefs.kt`
  (`tvChannelInitialized` boolean preference)
- `android/app/src/main/java/dev/benyamin/kumiho/tv/TvLaunchAction.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/MainActivity.kt` (extra handling)
- `android/app/src/main/java/dev/benyamin/kumiho/ui/App.kt` / `ui/tv/TvRoot.kt`
  (one-shot connect request)
- `android/app/src/main/AndroidManifest.xml`
  (`com.android.providers.tv.permission.WRITE_EPG_DATA`, receiver, transparent
  launcher activity, deep-link filters, and `android:launchMode="singleTop"` on
  `MainActivity`)
- `android/app/src/main/res/values/themes.xml` (transparent TV launcher theme)
- `android/app/src/main/res/drawable-nodpi/tv_channel_logo.png` (80dp)
- `android/app/src/main/res/drawable-nodpi/tv_channel_open.png`
- `android/app/src/main/res/drawable-nodpi/tv_channel_connect.png`
- `android/app/src/test/java/dev/benyamin/kumiho/tv/TvLaunchActionTest.kt`
- `android/app/src/androidTest/java/dev/benyamin/kumiho/tv/TvChannelTest.kt`

**Interfaces**
```kotlin
enum class TvLaunchAction {
    OPEN,
    CONNECT,
    ;

    companion object {
        fun fromUri(uri: Uri?): TvLaunchAction?
    }
}

class TvChannelPublisher(private val context: Context) {
    fun ensureChannel(): Long?
    fun ensurePrograms(channelId: Long)
}

const val EXTRA_TV_CONNECT = "dev.benyamin.kumiho.extra.TV_CONNECT"
```
Channel: display name **Kumiho**, `TYPE_PREVIEW`, app-link
`kumiho://tv/open`, 80dp logo. Programs: `TYPE_CLIP`, **Open Kumiho**
(`kumiho://tv/open`) and **Connect** (`kumiho://tv/connect`) with poster art.
`TvChannelReceiver` handles `android.media.tv.action.INITIALIZE_PROGRAMS` and
creates the default channel once. `TvChannelPublisher` records
`tvChannelInitialized` after a successful insert and reconciles the two
programs by internal provider id on later TV starts; if the user hides or removes
the channel, it is not forced back. `TvLauncherActivity` validates only the two URIs,
starts `MainActivity` with `EXTRA_TV_CONNECT` for Connect, and finishes.
`MainActivity` keeps `tvConnectRequested` in activity state and updates it in
`onNewIntent`; `KumihoApp` passes it to `TvRoot`, then `TvAppShell`, then
`TvDashboardRoute(connectRequested = ...)`. The dashboard consumes it once: if
the session is already active, no-op; otherwise invoke the existing power
action, which handles sign-in and VPN permission. If setup is incomplete, setup
is shown.

**Channel probe gate:** on the Google TV emulator and the X90L:
1. Install the app and run the receiver:
   `adb shell am broadcast -a android.media.tv.action.INITIALIZE_PROGRAMS -n dev.benyamin.kumiho.debug/dev.benyamin.kumiho.tv.TvChannelReceiver`.
2. Query `content://android.media.tv/channel` and `.../preview_program` for the
   two cards.
3. Verify the default channel and both cards appear in the launcher; select
   Open and Connect.
4. If the channel or cards are invisible on either target, remove T11 code,
   `WRITE_EPG_DATA`, receiver, activity, assets and tests; document the
   limitation. The rest of TV support ships.

**Verification**
1. `make android-test` succeeds; `TvLaunchActionTest` covers both URIs, null and
   malformed input.
2. `TvChannelTest` inserts/queries the channel and both programs on the TV
   emulator.
3. Manual probe above passes on emulator and X90L; Connect opens the app and
   auto-connects, or lands on setup when prerequisites are missing.
4. Phone install has no channel and no new permission prompt; phone tests pass.

## T12 — Complete TV connected coverage and screenshots

**Depends on:** T5-T11.
**Commit:** `test: complete tv connected coverage`

**Files**
- `android/app/src/androidTest/java/dev/benyamin/kumiho/tv/TvTestBase.kt`
- `android/app/src/androidTest/java/dev/benyamin/kumiho/tv/TvTraversalTest.kt`
- `android/app/src/androidTest/java/dev/benyamin/kumiho/tv/TvScreenshotsTest.kt`
- `android/app/src/androidTest/java/dev/benyamin/kumiho/tv/TvChannelTest.kt`
  (if not already added in T11)
- Any missing focused tests from T5-T10.

**Interfaces**
`TvTestBase` provides `assumeTrue(TvEnvironment.isTelevision(context))` so TV
tests skip on phone, and a `createAndroidComposeRule<ComponentActivity>` helper.
`TvScreenshotsTest` writes PNGs to
`/sdcard/Android/data/dev.benyamin.kumiho.debug/files/tv-screenshots` for Home,
setup, Locations and Settings in Light/Dark/OLED.

**Verification**
1. `make android-tv-emulator-test` runs the TV package; expected: all TV tests
   pass, 0 failures, 0 errors.
2. `make android-emulator-test` runs the phone package; expected: the existing
   phone suite passes and the TV tests are skipped.
3. `adb -s emulator-5554 pull /sdcard/Android/data/dev.benyamin.kumiho.debug/files/tv-screenshots`
   then review every image for overscan, focus and contrast.
4. `git diff --check` is clean.

## T13 — Separate Android TV CI job

**Depends on:** T12.
**Commit:** `ci: add android tv emulator job`

**Files**
- `.github/workflows/ci.yml`: new `android-tv` job, independent of `android`.
- `scripts/android-setup.sh`: already installs the TV image in T1; no further
  change unless the CI job needs a flag.
- `scripts/android-tv-connected-test.sh`: used by the job.

**Job shape**
- `runs-on: ubuntu-24.04`, `timeout-minutes: 60`.
- Checkout, JDK 17, Go, `bash scripts/android-setup.sh --ci`.
- Build the emulator AAR.
- `./gradlew --no-daemon -PkumihoEmulator=true :app:testDebugUnitTest :app:lintDebug :app:assembleDebug`.
- Enable KVM.
- `ReactiveCircus/android-emulator-runner@v2` with:
  `api-level: 36`, `target: google-tv`, `arch: x86_64`,
  `profile: tv_1080p`, `disable-animations: true`,
  `emulator-options: -no-window -no-audio -no-boot-anim -gpu swiftshader_indirect`,
  `script: bash scripts/android-tv-connected-test.sh`.

**Verification**
1. `bash -n scripts/android-tv-connected-test.sh scripts/android-setup.sh` exits 0.
2. `make android-tv-emulator-test` passes locally on the running Google TV AVD.
3. `python3 -c 'import yaml; yaml.safe_load(open(".github/workflows/ci.yml"))'`
   exits 0 (if PyYAML is available; otherwise use the repo's YAML tooling and
   record the command).
4. The job has no dependency on the phone `android` job and does not modify
   release assets.

## T14 — Version bump to v0.7.0

**Depends on:** T13.
**Commit:** `chore: bump android version to v0.7.0`

**Files**
- `android/app/build.gradle.kts`: default `kumihoVersion` from `0.6.0` to
  `0.7.0` (versionCode becomes 700).
- Any version string that the release tooling reads (not release tags).

**Verification**
1. `./gradlew --offline :app:assembleDebug` succeeds.
2. `"$ANDROID_HOME/build-tools/36.0.0/aapt" dump badging android/app/build/outputs/apk/debug/app-debug.apk | grep package:`
   shows `versionName='0.7.0'` and `versionCode='700'`.
3. `make android-test` succeeds.

## T15 — Documentation, changelog, NOTICE and superseded note

**Depends on:** T14.
**Commit:** `docs: document android tv support`

**Files**
- `docs/android-tv.md` (new): requirements, install, launcher channel, setup,
  remote navigation, TV limitations, background behavior, performance, test
  commands, X90L checklist.
- `README.md`: status, target list, Android section, build commands.
- `AGENTS.md`: target/platform map, Android architecture, CI jobs, design
  section TV type-scale departure, vendor/NOTICE note.
- `docs/android.md`: TV behavior, channel, sign-in fallback, logs/update,
  Always-on limits, troubleshooting.
- `docs/android-testing.md`: TV AVD creation, connected suite, screenshots,
  physical checklist, test counts.
- `CHANGELOG.md`: `## [0.7.0] - 2026-10-26` with Added/Changed/Notes.
- `NOTICE`: Compose for TV, TvProvider Apache-2.0 entries.
- `docs/superpowers/specs/2026-10-21-android-client-design.md`: superseded note
  on the Android TV non-goal (line 93), pointing to the new spec.

**Verification**
1. `git diff --check` is clean.
2. `grep -RIn "Android 8.0.*phone\|phones only\|Android TV.*non-goal" README.md docs AGENTS.md`
   has no stale claim; the only non-goal mention is the historical note.
3. All new commands in docs run as written: `make android-tv-emulator-test`,
   the AVD create/boot commands, the connected test command.
4. `make test` and `make lint` remain green.

## T16 — Full gates and physical X90L acceptance

**Depends on:** T15.
**Commit:** `docs: record android tv acceptance` (only after every check passes)

**Files**
- `docs/android-tv.md` checklist: tick each accepted item with the date and
  device/OS.

**Verification (all must pass)**
1. Go gate exactly as in `AGENTS.md`:
   `make lint && make test && go test -race -count=1 ./... && make build && GOOS=windows GOARCH=amd64 go vet ./... && GOOS=windows GOARCH=amd64 go build ./... && GOOS=android GOARCH=arm64 go build ./...`
2. `make android-test` (JVM tests, lint, debug APK).
3. `make android-emulator-test` on the phone AVD: existing suite passes; TV
   tests skip.
4. `make android-tv-emulator-test` on the Google TV API 36 AVD: every TV test
   passes.
5. Unsigned release compile/verify:
   `bash scripts/android-build-aar.sh && (cd android && ./gradlew --no-daemon :app:assembleRelease)`;
   `KUMIHO_ALLOW_UNSIGNED=1 bash scripts/android-verify-apk.sh <apk>` for each
   generated release APK; expected pass or documented unsigned-only limitation.
6. Physical X90L (API 31): install the debug APK, then complete the checklist:
   launcher banner/icon; channel Open + Connect (or documented drop); four-step
   setup; WebView/password sign-in with D-pad; VPN consent; connect/browse/
   exit IP/quota; Home and app-switch background survival; watchdog reconnect;
   disconnect/reconnect; Always-on/lockdown truth; split tunneling; proxy-only;
   logs export path; update WebView; remote-only traversal of every screen;
   cold start <=3s; status <=500ms; memory <=280MB on a 1GB-class emulator
   profile, with the X90L actual measured and recorded.
7. If any check fails, do not commit T16; fix the owning task or follow the
   risk gate.

## Risks + rollback

- **TV background-service kill (P0).** The T2/T6 probe must prove the tunnel
  survives Home and app switching. If not, stop; do not fake a media foreground
  service. Re-plan with the system Always-on binding or another sound mechanism.
- **WebView D-pad navigation (P1).** T6 probe; fallback to the native password
  form, keeping the WebView for recovery. The setup must never trap the remote.
- **Compose for TV compatibility (P1).** T1 compile probe; D28 fallback to
  `tv-foundation` + house Material3 components with the same focus ring.
- **Google TV channel visibility (P2).** T11 probe; remove the channel,
  permission, receiver, activity, assets and tests if invisible. The rest of TV
  support ships.
- **API spread (P2).** X90L API 31 vs emulator API 36; the physical checklist
  covers API 31, connected tests cover API 36.
- **Text entry (P2).** System IME on every TV text field; test email, password,
  CIDR, MTU and search on the X90L.
- **Long-list focus (P2).** Bring-into-view, focus restoration and D-pad tests
  for locations, settings, split tunneling and logs.
- **Memory/performance (P2).** Measure on X90L; keep the existing APK cap and
  avoid image-heavy TV surfaces.
- **Rollback.** Remove `ui/tv`, `tv/`, TV assets, TV dependencies and manifest
  additions (including `WRITE_EPG_DATA`); the phone app and all existing tests
  remain. No data migration; same package/signing keeps user data.

## Open questions

None. D28-D50 in the spec are approved assumptions; any change re-opens only
the affected task.

## Self-review

- **Spec objective/success criteria:** T2 (one APK), G1 (background VPN),
  T6 (setup/sign-in), T4/T5 (remote UI), T7-T10 (parity), T11 (channel),
  T16 (physical acceptance), T15 (docs), T14 (version).
- **Scope in:** T1-T16 cover every in-scope bullet; T11 is explicitly
  probe-gated.
- **Non-goals:** no task adds Watch Next media, voice, gamepad, TV input,
  ambient, profiles, a separate package or Play distribution.
- **Architecture:** `ui.tv` + shared route content slots in T5-T10; `TvEnvironment`
  in T2; `TvChannelPublisher`/`TvLauncherActivity` in T11; no engine or IPC
  changes anywhere.
- **Risks:** each P0/P1 risk has a named probe and fallback; T11 has a drop
  rule.
- **Names/signatures:** `TvEnvironment`, `TvKumihoTheme`, `TvAppShell`,
  `TvDashboardRoute`, `TvSetupFlow`, `TvChannelPublisher`, `TvLauncherActivity`,
  `TvLogExport`, `EXTRA_TV_CONNECT` are consistent across tasks.
- **Verification:** every task has an exact command or manual check with an
  expected result; no task says only “test it”.
- **Open items:** none.
