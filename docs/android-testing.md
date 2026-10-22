# Building, testing and releasing the Android app

This page is the maintainer's guide for the Android client: toolchain, AAR,
Gradle commands, APK verification, signing, release automation and the
physical acceptance checklist. The user guide is [android.md](android.md).

> **Status.** The app builds and both test suites are green on the current
> tree: **423 JVM tests / 124 connected tests / lint 0 errors + 6 warnings**.
> The **physical acceptance** below has **not** been run, and the **release
> automation (T17) is not implemented** — there is no Android CI job, release
> workflow, signing script, published APK or `SHA256SUMS` yet. Everything
> marked *(planned)* describes the approved release plan and does not exist in
> the repository. Do not report the checklist as passed or the app as released
> until the boxes are ticked and the sign-off table is filled in.

## Versions

| Piece | Pin | Where |
|---|---|---|
| App | `0.5.0`, `versionCode` 500 | `android/app/build.gradle.kts` (`-PkumihoVersion`) |
| Package | `dev.benyamin.kumiho` (debug: `.debug`) | `android/app/build.gradle.kts` |
| minSdk | 26 (Android 8.0) | `android/app/build.gradle.kts` |
| targetSdk | 36 | `android/app/build.gradle.kts` |
| compileSdk | 37.2 | `android/app/build.gradle.kts` (the pins need `minCompileSdk 37`) |
| NDK | `28.2.13676358` | `android/app/build.gradle.kts`, `scripts/android-env.sh` |
| JDK | 17 | `scripts/android-env.sh` |
| Gradle | wrapper 9.6.0 | `android/gradle/wrapper/gradle-wrapper.properties` |
| gomobile | `golang.org/x/mobile/cmd/gomobile@v0.0.0-20261011034528-197770444867` | `scripts/android-setup.sh` |

The Go version is the repository's `go.mod` (1.27.1). The app, the AAR and the
desktop binaries share one version: the Go build injects `internal/version`
and the Android build derives `versionName`/`versionCode` from
`-PkumihoVersion` (leading `v` stripped).

## Toolchain setup

Everything installs outside the checkout, under `$HOME/Android` (SDK, NDK,
JDK fallback) and `$HOME/go/bin` (gomobile). Nothing is committed.

    bash scripts/android-setup.sh           # install anything missing
    bash scripts/android-setup.sh --check   # verify versions only, no installs
    bash scripts/android-setup.sh --ci      # non-interactive (no prompts, no apt)

`--check` is the fast preflight. It verifies the JDK, `sdkmanager`, the SDK
packages (`platform-tools`, `platforms;android-36`, `platforms;android-37.2`,
`build-tools;36.0.0`, `ndk;28.2.13676358`, `emulator`, the API 36 x86_64 system
image), the pinned gomobile revision, `adb`, and the Gradle wrapper (or the
9.6.0 bootstrap). It exits non-zero on the first missing piece it reports.

Source the environment in every shell that runs Gradle or the AAR script:

    source scripts/android-env.sh
    # android env: JAVA_HOME=... ANDROID_HOME=...

The script only sets `JAVA_HOME`, `ANDROID_HOME`/`ANDROID_SDK_ROOT`,
`ANDROID_NDK_HOME` and `PATH`. It honors an existing `JAVA_HOME`; otherwise it
uses the system OpenJDK 17, then the user-local Temurin JDK that
`android-setup.sh` installed.

The setup script is idempotent, verifies downloaded command-line tools, JDK
and Gradle zips against pinned SHA-256 sums, and never writes inside the
repository.

## Build the AAR

The app links a gomobile archive built from `mobile/` (the Go facade). The AAR
is a **git-ignored build output** under `android/app/libs/`; Gradle never runs
Go. From the repository root:

    make android-aar             # android/arm + android/arm64, release -> app/libs/kumiho.aar
    make android-aar-emulator    # android/amd64 (x86_64), debug    -> app/libs/kumiho-emulator.aar

Both targets call `scripts/android-build-aar.sh`, which:

- sources `scripts/android-env.sh`;
- runs `gomobile bind -target … -androidapi 26 -javapkg=dev.benyamin.kumiho.go
  -trimpath` with the same `VERSION`/`COMMIT`/`DATE` ldflags as the Makefile
  (all three can be overridden from the environment);
- uses `GOFLAGS=-mod=readonly` so the bind reads `go.mod`/`go.sum` instead of
  the vendor tree and writes neither;
- checks the archive contains `classes.jar` and one
  `jni/<abi>/libgojni.so` per target;
- for the release AAR, extracts the arm64 `.so` and fails unless every LOAD
  segment has `p_align >= 0x4000` (16 KB pages).

Expected release layout: `classes.jar`, `jni/armeabi-v7a/libgojni.so`,
`jni/arm64-v8a/libgojni.so`. The emulator archive carries
`jni/x86_64/libgojni.so` instead and is **never** part of a release. The
release APKs must contain only arm ABIs and no x86/x86_64.

If you do not want to use `make`:

    scripts/android-build-aar.sh
    scripts/android-build-aar.sh --emulator

Build the AAR again after any Go change under `mobile/` or `internal/`. A
plain Gradle build without `android/app/libs/kumiho.aar` fails; with only the
emulator AAR it fails to resolve `libs/kumiho.aar`.

## Gradle commands

All commands run from `android/` after sourcing the environment. Add
`--offline` once the Gradle cache is warm; the first resolve needs network
access. The unit/lint/assemble gate:

    source scripts/android-env.sh
    cd android
    ./gradlew --offline :app:testDebugUnitTest :app:lintDebug :app:assembleDebug

Current expected result on the T16 tree:

- **423 JVM tests, 0 failures, 0 errors, 0 skipped**;
- **lint 0 errors, 6 warnings** — the pre-existing `OldTargetApi`,
  `UnusedAttribute`, `ObsoleteSdkInt` and three `UnusedResources`;
- `app/build/outputs/apk/debug/app-debug.apk`.

The debug build is one APK containing the AAR's arm libraries and installs as
`dev.benyamin.kumiho.debug` beside a release build. The emulator build selects
the x86_64 AAR and turns off ABI splits:

    ./gradlew --offline -PkumihoEmulator=true :app:assembleDebug

Release packaging (per-ABI plus universal) turns splits on automatically:

    cd android && ./gradlew :app:assembleRelease

Without signing material this produces `app-arm64-v8a-release-unsigned.apk`,
`app-armeabi-v7a-release-unsigned.apk` and `app-universal-release-unsigned.apk`
and is only a compile check. See [Signing and release](#signing-and-release)
before treating any release APK as shippable.

You can override the version for a one-off build:

    ./gradlew -PkumihoVersion=v0.5.0 :app:assembleDebug

### Instrumented tests on the emulator

The connected suite runs on the x86_64 emulator with the emulator AAR. Create
the AVD once (T6 did this on the development machine):

    avdmanager create avd -n kumiho-api36 \
      -k "system-images;android-36;google_apis;x86_64" \
      -d medium_phone --force

Boot it headless:

    emulator -avd kumiho-api36 -no-window -no-audio -no-boot-anim \
      -gpu swiftshader_indirect -no-snapshot -port 5554
    adb -s emulator-5554 wait-for-device
    adb -s emulator-5554 shell getprop sys.boot_completed   # 1

Then run:

    source scripts/android-env.sh
    cd android
    ./gradlew --offline -PkumihoEmulator=true :app:connectedDebugAndroidTest

Current expected result: **124 tests, 0 failures, 0 errors, 0 skipped**. The
suite includes the AAR probe, setup, dashboard, locations, settings, split
tunneling, account, logs, diagnostics, notification/tile reducers and the
screenshot capture tests. A connected run uninstalls the app and its external
files when it finishes.

### API 26 and API 36 coverage

- The JVM suite covers the pure logic, including the API 33
  `excludeRoute`/API 26–32 complement split, the 256-prefix cap, the
  `0.0.0.0/0` and whole-IPv4 rejections, the DNS-host carve-out and the tile's
  consent reducer.
- The connected suite runs on an **API 36** x86_64 AVD. There is no API 26
  emulator image in this repository's setup script, and no API 26 run has been
  recorded.
- The route-complement connection path, `allow_lan`, user exclusions and the
  tile's API 26–28 behaviour still need a real **API 26–32** device or a
  manually created emulator; they are in the physical checklist.
- The 16 KB page-size checks are the AAR `p_align` check plus `zipalign -P 16`
  on each APK. A 16 KB emulator/device run is optional and pending; do not
  claim it.

## APK verification

These commands work on the current local APKs. Use the build-tools directory
because `aapt2`/`zipalign` are not always on `PATH`.

    source scripts/android-env.sh
    BT="$ANDROID_HOME/build-tools/36.0.0"
    APK=android/app/build/outputs/apk/debug/app-debug.apk

    "$BT/aapt2" dump badging "$APK" | head -5
    unzip -l "$APK" | grep libgojni
    "$BT/zipalign" -c -P 16 -v 4 "$APK"

Expect `aapt2` to report `package: name='dev.benyamin.kumiho.debug'`,
`versionCode='500'`, `versionName='0.5.0-debug'`, `minSdkVersion:'26'` and
`targetSdkVersion:'36'`; `zipalign` to say `Verification successful`; and the
archive to contain only the expected ABI(s). For a release APK, replace
`debug` with the release path and check that there is no `x86`/`x86_64`
library. The check sequence above is the same one the planned
`scripts/android-verify-apk.sh` would wrap *(planned)*; that script does not
exist yet.

For a signed APK the signature check is:

    "$BT/apksigner" verify --print-certs "$APK"

This only makes sense once the APK has been signed (below).

## Signing and release

### What exists today

- `android/app/build.gradle.kts` reads `android/keystore.properties` **only if
  it is present**; keys: `storeFile`, `storePassword`, `keyAlias`,
  `keyPassword`. The file is git-ignored (`android/.gitignore`), as are
  `*.jks`/`*.keystore` at the repository root.
- With the file absent, `:app:assembleRelease` stays unsigned — a compile
  check, not a release.
- With the file present, the release build type uses the `release` signing
  config and the APKs are signed.
- Gradle can emit the per-ABI and universal APKs, but **no release script,
  signature/ABI/size gate, CI job or GitHub release step exists**. The
  approved T17 plan is blocked by the T11 physical first-connect gate.

### What is planned (do not claim it is implemented)

The approved plan adds `scripts/android-release.sh` and
`scripts/android-verify-apk.sh`, `android/keystore.properties.example`, the
final Makefile targets (`android-debug`, `android-test`,
`android-emulator-test`, `android-release`, `android-verify`), an Android CI
job and Android steps in the release workflow. None of those targets, scripts
or jobs exist today; `make android-test`, `make android-release`,
`make android-verify` and `scripts/android-release.sh` will fail with "No
rule"/"No such file". Do not document or run them as if they were current.

The planned release asset names are:

    kumiho-v<version>-android-arm64-v8a.apk
    kumiho-v<version>-android-armeabi-v7a.apk
    kumiho-v<version>-android-universal.apk

They would be copied to `dist/`, added to the existing `SHA256SUMS`, and
attached to the GitHub release. No Android APK or Android checksum has been
published.

### Creating a release keystore (offline, once)

Do this on a trusted offline machine, then back the keystore and its passwords
up separately. **Losing the key means no future update can install over the
existing app** (Android rejects a different signing certificate); there is no
recovery beyond uninstalling, which erases app data.

    keytool -genkeypair -v \
      -keystore ~/kumiho-android-release.jks \
      -alias kumiho \
      -keyalg RSA -keysize 4096 -validity 10000

Never commit the keystore or passwords. For a local signed build, create
`android/keystore.properties` (untracked):

    storeFile=../kumiho-android-release.jks
    storePassword=<store password>
    keyAlias=kumiho
    keyPassword=<key password>

`storeFile` is resolved with `rootProject.file(...)`, i.e. relative to
`android/`. Then:

    cd android && ./gradlew :app:assembleRelease

The outputs are `app-arm64-v8a-release.apk`, `app-armeabi-v7a-release.apk` and
`app-universal-release.apk` under
`android/app/build/outputs/apk/release/`.

### GitHub Actions secrets (planned)

The T17 plan calls for four repository secrets — `ANDROID_KEYSTORE_BASE64`
(the base64-encoded keystore) plus the store password, key alias and key
password — decoded into a temporary keystore in the release job and removed in
an `always()` step. **The exact names of the three companion secrets and the
workflow itself do not exist yet**; set none of them until the workflow lands.

### Release checks (planned)

Before any future release, the plan gates on: `aapt2` badging
(package/version/min/target), an ABI check (arm only; no x86 in release),
`zipalign -c -P 16 -v 4`, `llvm-readelf` `p_align >= 0x4000` on the arm64
`libgojni.so`, `apksigner verify --print-certs`, and a universal APK at or
under 60 MB with per-ABI sizes recorded. The same checks should be runnable
locally with the verify script once T17 exists.

## Physical acceptance checklist

The checklist has **not been run**. It is the release gate and the T11
first-connect gate; any P0/P1 failure stops the release. Record one result per
device and leave the sign-off table blank until every applicable box is ticked.

### Devices and prerequisites

- [ ] An **API 33+ arm64 phone** with a working SIM/Wi-Fi connection and a
      Firefox Account holding the VPN entitlement.
- [ ] An **API 26–32 phone or emulator** for the route-complement path. (There
      is no API 26 AVD in the repo today; create one if no phone is available.)
- [ ] A **16 KB page-size device or emulator**, if one is available. If not,
      record that explicitly and rely on the `zipalign`/`p_align` checks — do
      not silently skip it.
- [ ] The APK under test is built from the commit being signed off; record
      the commit, app version and `versionCode`. Note whether it is a debug
      or release build.
- [ ] Baseline before installing: record the phone's public exit IP and DNS
      servers with Kumiho disconnected.
- [ ] `adb` access to the phone (for `dumpsys`, `logcat` and screenshots).
- [ ] No Private DNS hostname configured (or record it and test again after
      restoring it).

### Install, setup and sign-in

- [ ] Fresh install succeeds (`adb install -r <apk>` or the file manager) and
      the app launches to the four-step setup.
- [ ] The consent step requires the tick; **Continue** is disabled until it is
      ticked, and the tick is visible with the action on first paint.
- [ ] Sign-in completes with email → password; a 2FA account completes the
      code step; an email-link account shows the waiting state and signs in
      after the link is confirmed or **Re-check**.
- [ ] A wrong password and a captcha error are shown inline; the form stays
      usable and no browser opens.
- [ ] Denying VPN consent and notification permission still allows **Finish**;
      the state reads *Not granted* / *Not allowed*, not a fake grant.
- [ ] Granting both and using **Finish and connect** reaches the dashboard and
      starts a session; **Finish without connecting** reaches the dashboard
      disconnected.
- [ ] Always-on/lockdown rows show the real Android state (on an Android 10+
      device) and **Open Android VPN settings** opens the right page.

### First real connect (T11 STOP/GO)

- [ ] Connect from the dashboard or the tile; the state moves through
      *Connecting* to *Connected* and the foreground notification appears.
- [ ] `adb shell dumpsys connectivity` shows a VPN network with DNS
      `10.8.0.2`; the engine log/notification shows the session.
- [ ] Browse several HTTPS sites through the tunnel; pages load and the
      **Exit IP** card shows the proxy address, not the carrier address.
- [ ] DNS resolution works for new domains (not just cached ones), including
      from an app other than the browser.
- [ ] `test-ipv6.com` (or an equivalent IPv6 check) shows no IPv6 path and no
      leak; an IPv6-only target fails rather than bypassing the tunnel.
- [ ] The **Quota** and **Session** cards populate with real numbers.
- [ ] Disconnect, confirm the notification disappears, then reconnect and
      confirm a fresh session and a fresh exit IP where expected.
- [ ] While connected, toggle Wi-Fi/mobile data: the watchdog redials the
      upstream without dropping the interface, and the UI follows the real
      state (no duplicate tunnel, no stuck spinner).
- [ ] With lockdown on, disconnect through the confirmation dialog: the phone
      has no traffic until a reconnect, and **Open VPN settings** works.
- [ ] **STOP** if in-tunnel DNS fails while the tunnel is up: collect the
      establish → bind → query evidence, do not switch to Private DNS or a
      fallback resolver, and re-plan the DNS delivery.

### Notification, Quick Settings tile and proxy-only (T16)

- [ ] The status notification shows state · location, uptime and ↑/↓ totals;
      it is silent/ongoing and updates in place.
- [ ] It offers **Disconnect** and **Open**, and **no Connect**; Disconnect
      ends the session, Open brings the app forward.
- [ ] Signing out while Always-on is set posts the sign-in prompt on the
      default-importance alerts channel; a failed connect posts a one-shot
      error alert.
- [ ] Add the **Kumiho** tile, then tap it disconnected: it connects the saved
      selection (not an arbitrary server) and never starts proxy-only.
- [ ] On first use the tile opens Android's VPN consent dialog; after granting
      it, the connect completes on the next tile sighting or a re-tap (the
      extra step is expected).
- [ ] On **API 29+** the tile shows subtitles (Connected · location,
      Connecting…, Sign in required, and so on); on **API 26–28** only
      label/state change and no subtitle is expected.
- [ ] Tapping an active tile disconnects; a signed-out or service-unavailable
      tile opens the app instead of attempting a connect.
- [ ] **Proxy only** from the options sheet connects with **no VPN consent
      prompt** and creates **no VPN interface** (`dumpsys connectivity` shows
      no VPN network); the dashboard reads *Proxy only* and the service stays
      in the foreground.
- [ ] The local SOCKS5 proxy works (`curl --socks5-hostname 127.0.0.1:1080
      https://example.com`) and, with `http_proxy` on, the HTTP proxy works
      (`curl -x http://127.0.0.1:8080 https://example.com`).
- [ ] A second app on the phone can use those proxies without extra setup
      (device-wide); another device on the LAN cannot reach them.
- [ ] Proxy-only has no per-app split and no Always-on interface claim; the
      disconnect action still works.

### Always-on and lockdown

- [ ] Turn Always-on on in Android VPN settings; kill the app/service, and
      confirm Android starts Kumiho and it connects while signed in.
- [ ] Reboot the phone with Always-on on: Kumiho starts after boot and
      connects; the dashboard/Details says Android may start it after a
      reboot.
- [ ] Sign out, then let Always-on restart the service: a sign-in notification
      appears and it does **not** fight lockdown.
- [ ] Turn lockdown on, disconnect, and reboot: Android restarts the tunnel;
      connectivity returns when it connects, and the UI reflects the restart
      rather than claiming a manual session.
- [ ] Disconnecting under lockdown warns about losing connectivity, offers
      **Keep connected**, and the confirm path leaves the phone offline until
      the restart.
- [ ] No in-app kill-switch or autoconnect toggle exists; the read-only rows
      open Android VPN settings. On Android 8.0–9 both rows read **Off**
      because the OS does not expose the flags.

### Split tunneling and route exclusions

- [ ] **Off**: every app uses the tunnel; Kumiho itself stays outside (the
      locked *Always outside* row).
- [ ] **Exclude apps**: selected apps bypass the tunnel (verify with their
      public IP) while everything else is tunneled; Kumiho stays outside.
- [ ] **Include apps**: only selected apps are tunneled; everything else
      bypasses; Kumiho is omitted from the list.
- [ ] **Empty Include**: the warning appears, and every app is tunneled with
      Kumiho outside rather than nothing being tunneled.
- [ ] Changing the mode or selection while connected shows *The tunnel is up.
      Reconnect to apply this selection.*; reconnecting applies it; the
      dashboard and Diagnostics split card show the real mode and count.
- [ ] Uninstalling a selected app removes it from the saved selection after
      the app list reloads.
- [ ] `allow_lan` on: LAN addresses (router, printer, local SSH) are reachable;
      turn it off and they stop being reachable while the internet still works.
- [ ] `exclude_cidrs` accepts a bare address, masks host bits, rejects IPv6,
      `/0` and whole-IPv4 unions; a valid exclusion bypasses the tunnel.
- [ ] The tunnel DNS still resolves with `allow_lan` on and with a user CIDR
      covering `10.0.0.0/8` (C1: `10.8.0.2` stays routed).
- [ ] On **API 33+**: exclusions use `excludeRoute` and behave correctly.
- [ ] On **API 26–32**: the complement-route path connects, `allow_lan` and
      user exclusions work, and DNS resolves. Record the device/API.
- [ ] Split × lockdown: the OS still restarts the service and the app shows
      the mode it actually sent; record any OS behaviour that ignores the
      filters.

### Account, logs, diagnostics and lifecycle

- [ ] Account shows email, UID, subscription, quota, bandwidth and token
      expiry; **Refresh** works; a failed refresh keeps the last fields.
- [ ] Sign out confirms, clears the session, stays on Account and lets you
      sign back in.
- [ ] Logs show the snapshot + live lines, the level filter, search, Follow
      and Clear; the 2000-line cap note is truthful.
- [ ] **Share** writes a log file and opens the share sheet; the file contains
      no tokens or passwords; nothing is uploaded automatically.
- [ ] Diagnostics runs the Android facts + doctor, shows OK/Warn/Fail/Skip and
      **Copy all**, and **Re-run** reflects a settings change.
- [ ] **Reset local state** clears the cache/log ring/failure counters but
      leaves you signed in.
- [ ] The battery and notification shortcuts open the right pages; battery
      state and notification permission match Android settings.
- [ ] Revoke the VPN permission while connected: the session ends, the
      notification is removed and the dashboard asks for permission again.
- [ ] Deny the VPN prompt on a connect: the notice is honest and a subsequent
      grant lets the next connect succeed.
- [ ] Kill and reopen the app mid-session: the session and UI state recover
      from the bound service rather than restarting the tunnel.
- [ ] Force a crash once (debug build) and confirm `crash.log` is replayed
      into the engine log on the next start and then removed.

### 16 KB page size

- [ ] On a 16 KB device/emulator (if available): the APK installs, the app
      starts, connects and does not crash; `unzip -l` shows the arm64 `.so`
      and `llvm-readelf -lW` reports `p_align 0x4000` for every LOAD segment.
- [ ] Every APK passes `zipalign -c -P 16 -v 4`.
- [ ] If no 16 KB device is available, record that here and keep the AAR
      alignment check as the only evidence.

## Sign-off

Leave this table blank until the checklist has actually been run. One row per
device; attach the logs/screenshots elsewhere. A pass requires every applicable
box above and no P0/P1 failure.

| Date | Device | Android version / API | App version / build | Tester | Result |
|---|---|---|---|---|---|
|  |  |  |  |  |  |
|  |  |  |  |  |  |
|  |  |  |  |  |  |

The T11 physical first-connect gate and this T18 acceptance are `[user]`
steps. T17 (release signing/CI/release workflow) stays blocked until they
pass.

## Rollback and uninstall

The Android client is additive; desktop state, protocol v1 and the existing
Go gate are unaffected. To roll back:

- **Uninstall the app** — `adb uninstall dev.benyamin.kumiho` (release) or
  `adb uninstall dev.benyamin.kumiho.debug` (debug), or Settings → Apps →
  Kumiho → Uninstall. This removes all app-private state: tokens, settings,
  logs, cached server list and split selection. It does not touch the desktop
  daemon or your Firefox Account.
- **Turn off Always-on/lockdown first** in Android VPN settings if they were
  enabled. An uninstalled VPN app leaves the system setting pointing at
  nothing, and lockdown can leave the phone without network access.
- **Downgrade** by installing an older APK signed with the **same** key over
  the newer one. Android refuses a lower `versionCode` unless you uninstall
  (`adb install -r -d <apk>` allows a downgrade on a debuggable build); a
  different signing key always requires an uninstall and loses app data.
- **Build rollback** — revert the Android commits and delete `android/`,
  `mobile/` and the real `internal/platform/android` provider; the previous
  compile-only stub and the desktop gate remain. AARs and APKs are untracked
  build outputs, so there is no artifact to migrate.
- **Release rollback (planned)** — if a signed release is bad, unpublish or
  mark the release and publish a new `versionCode` built from a fixed tree;
  never re-sign with a different key. Restore the keystore from its offline
  backup only.

## Related documentation

- [android.md](android.md) — the user guide.
- [usage.md](usage.md) — desktop CLI, TUI and the full settings reference.
- [windows-testing.md](windows-testing.md) — the equivalent manual checklist
  for Windows.
- [troubleshooting.md](troubleshooting.md) — desktop troubleshooting and
  `kumiho doctor`.
