# Building, testing and releasing the Android app

This page is the maintainer's guide for the Android client: toolchain, AAR,
Gradle commands, APK verification, signing, release automation and the
physical acceptance checklist. The user guide is [android.md](android.md).

> **Status.** The app builds and both test suites are green on the current
> tree: **423 JVM tests / 124 connected tests / lint 0 errors + 6 warnings**.
> Physical acceptance is a **partial pass with user-accepted exceptions** on a
> Galaxy S25 FE; the sign-off is recorded below. The release automation is
> implemented: `android-verify-apk.sh`, `android-release.sh`, the five Makefile
> targets, an Android CI job and Android signing steps in the release workflow.
> The scripts were exercised end to end with a throwaway signing key, then the
> production-signed `v0.5.0`
> [release](https://github.com/benyamin-git/kumiho/releases/tag/v0.5.0) was
> published on 2026-10-22 with three Android APKs and `SHA256SUMS`. The
> production signing key and GitHub Actions secret values remain user-owned.

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
`jni/x86_64/libgojni.so` instead and is **never** part of a release. Device and
release packaging excludes `lib/x86/**` and `lib/x86_64/**`; the universal
release APK contains both arm ABIs and no x86 engine or dependency libraries.
The verifier enforces this on every release APK.

If you do not want to use `make`:

    scripts/android-build-aar.sh
    scripts/android-build-aar.sh --emulator

Build the AAR again after any Go change under `mobile/` or `internal/`. A
plain Gradle build without `android/app/libs/kumiho.aar` fails; with only the
emulator AAR it fails to resolve `libs/kumiho.aar`.

## Gradle commands

All commands run from `android/` after sourcing the environment. Add
`--offline` once the Gradle cache is warm; the first resolve needs network
access. The Makefile wrappers run from the repository root and build the AAR
first:

    make android-debug
    make android-test                 # JVM tests + lint + debug APK
    make android-emulator-test        # connected suite; emulator must be running
    make android-release KUMIHO_VERSION=0.5.0 KUMIHO_EXPECT_CERT_SHA256=<fingerprint>
    KUMIHO_EXPECT_CERT_SHA256=<fingerprint> make android-verify

Pass `GRADLE_FLAGS=--offline` to any of the Gradle-backed targets once the
cache is warm. The unit/lint/assemble gate directly:

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

`scripts/android-verify-apk.sh` is the release gate. It checks the badging
(package, version, min/target SDK), the exact ABI contents, absence of
x86/x86_64 native libraries, `zipalign -c -P 16 -v 4`, the arm64 `libgojni.so`
`p_align >= 0x4000`, the signature via `apksigner`, and the universal size cap:

    source scripts/android-env.sh
    scripts/android-verify-apk.sh dist/kumiho-v0.5.0-android-universal.apk

    # Unsigned output from a compile-check assembleRelease:
    KUMIHO_ALLOW_UNSIGNED=1 scripts/android-verify-apk.sh \
      android/app/build/outputs/apk/release/app-universal-release-unsigned.apk

Use `--help` for the `KUMIHO_EXPECT_*` overrides and the default 60 MiB
universal cap. The verifier is release-focused; it rejects the x86_64 emulator
build, and a debug APK needs the `dev.benyamin.kumiho.debug` package override.
`make android-verify` runs it over every `dist/kumiho-*-android-*.apk`.

The equivalent manual commands (use the build-tools directory because
`aapt2`/`zipalign` are not always on `PATH`):

    source scripts/android-env.sh
    BT="$ANDROID_HOME/build-tools/36.0.0"
    APK=android/app/build/outputs/apk/debug/app-debug.apk

    "$BT/aapt2" dump badging "$APK" | head -5
    unzip -l "$APK" | grep libgojni
    "$BT/zipalign" -c -P 16 -v 4 "$APK"

Expect `aapt2` to report `package: name='dev.benyamin.kumiho.debug'`,
`versionCode='500'`, `versionName='0.5.0-debug'`, `minSdkVersion:'26'` and
`targetSdkVersion:'36'`; `zipalign` to say `Verification successful`; and the
archive to contain only the expected ABI(s). For a signed APK the signature
check is:

    "$BT/apksigner" verify --print-certs "$APK"

## Signing and release

### Signing configuration

- `android/app/build.gradle.kts` reads `android/keystore.properties` **only if
  it is present**; keys: `storeFile`, `storePassword`, `keyAlias`,
  `keyPassword`. The file is git-ignored (`android/.gitignore`), as are
  `*.jks`/`*.keystore` at the repository root. The committed
  `android/keystore.properties.example` shows the shape.
- With the file absent, `:app:assembleRelease` stays unsigned — a compile
  check, not a release.
- With the file present, the release build type uses the `release` signing
  config and the APKs are signed.
- `scripts/android-release.sh` requires the file and refuses to start without
  a non-empty value for all four keys and an existing `storeFile`. It also
  requires `KUMIHO_EXPECT_CERT_SHA256`, the public SHA-256 fingerprint of the
  release certificate, so a wrong or rotated key fails before publishing.

### Signed release build

    cp android/keystore.properties.example android/keystore.properties
    # edit the four values; use an absolute storeFile path
    CERT_SHA256=$(keytool -list -v \
      -keystore ~/kumiho-android-release.jks -alias kumiho \
      | awk '/SHA256:/ { print $2; exit }' | tr -d ':')
    make android-release KUMIHO_VERSION=0.5.0 \
      KUMIHO_EXPECT_CERT_SHA256="$CERT_SHA256"
    KUMIHO_EXPECT_CERT_SHA256="$CERT_SHA256" make android-verify

`android-release.sh` validates the semantic version, builds the arm/arm64 AAR
with the release label, runs `:app:assembleRelease`, copies the three APKs to
`dist/`, verifies each one and prints its SHA-256. It refuses a dirty worktree
unless `KUMIHO_ALLOW_DIRTY=1` is set for a local experiment. The outputs are:

    dist/kumiho-v<version>-android-arm64-v8a.apk
    dist/kumiho-v<version>-android-armeabi-v7a.apk
    dist/kumiho-v<version>-android-universal.apk

The per-ABI names are the release assets; the universal APK is capped at 60 MiB
by the verifier. No Android APK or Android checksum has been published yet.

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

    storeFile=/home/you/kumiho-android-release.jks
    storePassword=<store password>
    keyAlias=kumiho
    keyPassword=<key password>

`storeFile` is resolved with `rootProject.file(...)`: an absolute path is used
as-is; a relative path is relative to `android/`. Then run
`make android-release KUMIHO_VERSION=0.5.0 KUMIHO_EXPECT_CERT_SHA256=<fingerprint>`
as shown above. The fingerprint is public metadata, not a secret.

### GitHub Actions secrets and tagged release

The release workflow expects five repository secrets:

| Secret | Value |
|---|---|
| `ANDROID_KEYSTORE_BASE64` | `base64 -w0` of the release keystore |
| `ANDROID_KEYSTORE_PASSWORD` | keystore password |
| `ANDROID_KEY_ALIAS` | key alias (`kumiho` in the example) |
| `ANDROID_KEY_PASSWORD` | key password |
| `ANDROID_CERT_SHA256` | certificate SHA-256 fingerprint (public) |

`.github/workflows/release.yml` decodes the keystore into `$RUNNER_TEMP`,
writes `android/keystore.properties` with mode 600, runs
`scripts/android-release.sh`, appends the three APKs to `SHA256SUMS`, and
attaches them to the GitHub release. A step trap removes the temporary
keystore and properties, and an `always()` step is a backstop. The workflow
runs on stable `vMAJOR.MINOR.PATCH` tags; prerelease tags such as `v0.5.0-rc.1`
are intentionally skipped until a prerelease/versionCode policy exists. Tag
`v0.5.0` ran the workflow successfully on 2026-10-22 and published the desktop
and Android assets.

### Published v0.5.0 release

Tag `v0.5.0` points at merge commit `7202426`. The
[release workflow](https://github.com/benyamin-git/kumiho/actions/runs/39819623775)
passed and the [release](https://github.com/benyamin-git/kumiho/releases/tag/v0.5.0)
contains Linux amd64/arm64 and Windows amd64 zips, the three signed Android
APKs, and `SHA256SUMS`. The APKs were verified after download: package
`dev.benyamin.kumiho`, version `0.5.0`/500, min/target SDK 26/36, exact arm
ABIs, 16 KB alignment, arm64 `p_align=0x4000`, and the production certificate
`4bfbd2690546a969d1416e0729c2d5b93d39211d2ceb67068b03705b5bff707e`; the
universal APK is 25.51 MiB.

### Release checks

Before any release, the local script and CI gate on: `aapt2` badging
(package/version/min/target), an exact arm-only ABI set (no x86/x86_64 or other
native ABI directories), `zipalign -c -P 16 -v 4`, `llvm-readelf`
`p_align >= 0x4000` on **every** arm64 native library, `apksigner
verify --print-certs` with the expected certificate fingerprint, and a
universal APK at or under 60 MiB with per-ABI sizes recorded.
`make android-verify` reruns the structural checks over `dist/`; pass
`KUMIHO_EXPECT_CERT_SHA256=<fingerprint>` to pin the certificate there too.

## Physical acceptance checklist

The checklist has been partially run on a physical S25 FE. The user explicitly
accepted the remaining untested items as exceptions on 2026-10-22; the
exception sign-off is recorded below. This is not a full checklist pass. It is
the release gate and the T11 first-connect gate; any P0/P1 failure stops the
release. Record one result per device.

### Recorded evidence (not a sign-off)

On 2026-10-22, a Samsung Galaxy S25 FE (SM-S731B, Android SDK 37, debug package
`dev.benyamin.kumiho.debug`) completed the first-connect DNS check after the
userspace resolver fix: `CONNECTED`, VPN DNS `10.8.0.1`, Private DNS off,
fresh-domain browsing, and the Exit IP card showing the proxy address. The
build was `0.5.0-debug` with the pre-commit Go label
`v0.4.1-36-g3157d64-dirty`; its source tree is the content committed as
`78237c9`. The clean sign-off candidate is the rebuild from that commit:
`0.5.0-debug` (versionCode 500), SHA-256
`53b08383ca34bebb33329350d782efa42eb140bb4b2cde6a80535d9378a02b8a`.

These are the only physical results recorded so far. Every box below and the
sign-off table remain open.

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
      `10.8.0.1`; the engine log/notification shows the session.
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
      covering `10.0.0.0/8` (the DNS host `10.8.0.1` stays routed).
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

The user accepted the remaining unchecked boxes as release exceptions on
2026-10-22. The row below is a **partial pass**, not a full checklist pass;
future releases should complete the open boxes. Attach logs/screenshots
elsewhere.

| Date | Device | Android version / API | App version / build | Tester | Result |
|---|---|---|---|---|---|
| 2026-10-22 | Samsung SM-S731B (Galaxy S25 FE) | SDK 37 | 0.5.0-debug (500), source 78237c9 | user | Partial pass with exceptions: connect/DNS/browse/exit IP verified; remaining checklist waived; no P0/P1 reported |
|  |  |  |  |  |  |
|  |  |  |  |  |  |

The T11 physical first-connect gate is passed on this device. T18 is accepted
with the exceptions above; T17 (release signing/CI/release workflow) is
implemented and the `v0.5.0` release was published on 2026-10-22. The
production keystore and GitHub secret values remain user-owned.

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
- **Release rollback** — if a signed release is bad, unpublish or mark the
  release and publish a new `versionCode` built from a fixed tree; never
  re-sign with a different key. Restore the keystore from its offline backup
  only.

## Related documentation

- [android.md](android.md) — the user guide.
- [usage.md](usage.md) — desktop CLI, TUI and the full settings reference.
- [windows-testing.md](windows-testing.md) — the equivalent manual checklist
  for Windows.
- [troubleshooting.md](troubleshooting.md) — desktop troubleshooting and
  `kumiho doctor`.
