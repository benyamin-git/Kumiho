# Kumiho Android TV Support — Design Spec

**Status:** Approved
**Date:** 2026-10-26

## Objective / why

Kumiho's Android client is phone-first. The approved 2026-10-21 Android
client spec explicitly lists Android TV as a non-goal. This cycle makes the
same app a first-class Android TV and Google TV client: one signed APK that
installs on phones and televisions, a remote-first 10-foot UI, the same Go
engine and `VpnService`, full desktop parity, and a Google TV launcher channel
with **Open Kumiho** and **Connect** cards.

The target is a Sony X90L on Android 12 (API 31) plus a Google TV API 36
emulator. The release is v0.7.0. The phone UI, the Go engine, the desktop
clients, IPC protocol v1 and the existing release assets are unchanged.

This spec supersedes the Android TV non-goal in
`docs/superpowers/specs/2026-10-21-android-client-design.md` (D7, line 93).
The older spec remains as historical design.

## Success criteria / definition of done

1. **One APK, two form factors.** The signed v0.7.0 APK installs on the X90L
   (API 31) and on a phone; the phone launch path and all phone behavior are
   unchanged. TV mode is selected by the device, not by a separate package.
2. **Fresh install works from the couch.** On the X90L, a clean install
   completes the four-step setup, signs in with Firefox Accounts, grants VPN
   consent, connects, browses through the tunnel, shows the real exit IP and
   quota, disconnects and reconnects — with a D-pad remote only.
3. **Every control is reachable.** All TV screens, dialogs and setup steps are
   fully navigable with Up/Down/Left/Right, Select and Back. Focus is always
   visible (ring plus subtle scale), no control relies on touch, and content
   stays inside the 48dp/27dp overscan-safe area.
4. **The tunnel survives leaving the app.** A live TV session continues after
   Home, after opening another app, and through a watchdog reconnect. The
   foreground service is not killed by TV background policy. This is a release
   blocker; if the probe cannot make it hold, TV support does not ship.
5. **Sign-in works on a remote.** The Firefox Accounts WebView is navigable by
   D-pad and the system keyboard. If the probe shows it is not, the
   password-first native form becomes the TV sign-in path; account add and
   re-authentication use the same path. WebView cookies are cleared on
   sign-out.
6. **Parity is verifiable.** Every screen and action available on the phone is
   reachable on TV: Home, Locations, Account, Settings, Split tunneling, Logs,
   Diagnostics, connect options, proxy-only, account rotation, logs export and
   the manual update check. Deliberate TV mappings are recorded in this spec.
7. **TV surfaces are honest and useful.** The launcher shows the Kumiho banner
   and TV icon. A default Google TV channel offers **Open Kumiho** and
   **Connect** cards; if Google TV does not display the channel, the channel is
   removed and the limitation is documented. The app never claims a
   notification-tray or Quick Settings surface that TV does not have.
8. **Always-on is truthful.** The setup step and Settings show the real
   `isAlwaysOn()`/`isLockdownEnabled()` state. If the TV exposes no usable
   Always-on setting, the step explains that and still allows finish; no toggle
   is faked.
9. **TV performance holds.** On the X90L: cold start to an interactive TV
   Dashboard ≤3s; status and notification updates ≤500ms after an engine
   change; no periodic UI polling or wakelocks beyond the engine; memory
   ≤280MB on a 1GB-class TV; the universal APK stays within the existing 60MB
   cap.
10. **Quality gate is green.** Go tests, race tests, Kotlin unit tests, Android
    lint, `assembleDebug`, phone connected tests, TV connected tests on Google
    TV API 36 x86_64 (`tv_1080p`), the separate `android-tv` CI job, and the
    physical X90L acceptance checklist all pass. No known P0/P1 defects.
11. **Docs are true.** `docs/android-tv.md` exists; README, `AGENTS.md`,
    `docs/android.md`, `docs/android-testing.md`, `CHANGELOG.md` and `NOTICE`
    describe the TV behavior, toolchain, tests and licenses. The old Android
    spec carries a superseded note.
12. **No collateral change.** No Go engine, IPC, daemon, CLI, TUI, Windows GUI
    or desktop release change. Protocol v1 stays byte-identical. Phone UI and
    data remain compatible. The only new permission is `WRITE_EPG_DATA` for
    the launcher channel.

## Scope

### In scope

- One Android app (`dev.benyamin.kumiho`) for phones and Android TV / Google TV,
  API 26+, `arm64-v8a` and `armeabi-v7a`.
- TV detection and a dedicated TV presentation layer under `ui.tv`, sharing
  view models, state and engine clients with the phone UI.
- Compose for TV (`androidx.tv:tv-material:1.1.0`,
  `androidx.tv:tv-foundation:1.0.0`) themed with the existing Kumiho palette.
- TV shell with a left navigation rail (Home, Locations, Account, Settings,
  More), a two-column Dashboard, and TV-specific screens/dialogs for every
  phone destination.
- TV setup: four steps with TV copy, consent, WebView or password sign-in,
  VPN/notification permissions, Always-on explanation and finish.
- TV D-pad focus system: visible ring plus subtle scale, 48dp targets, logical
  traversal, focus restoration, list bring-into-view and Back behavior.
- TV typography and spacing: 16/18/20/24/32/40sp and 48dp/27dp overscan with an
  8dp grid, recorded as a house-style departure.
- TV-specific logs export path and in-app WebView for the manual update check.
- A default Google TV launcher channel with **Open Kumiho** and **Connect**
  cards, `WRITE_EPG_DATA`, an `INITIALIZE_PROGRAMS` receiver, deep links and
  reconciliation.
- TV banner, TV icon, channel logo and poster art derived from the existing
  fox artwork.
- TV emulator setup, a `make android-tv-emulator-test` target, a separate
  `android-tv` CI job, TV connected tests and a physical X90L checklist.
- Documentation, changelog, NOTICE and the old-spec superseded note.

### Non-goals (explicitly out)

- Google Play or F-Droid distribution; a separate TV APK or TV application ID;
  a separate TV-only app.
- Watch Next media recommendations, a media catalog, voice search, gamepad
  mappings, a TV input service, ambient/screensaver control, multi-user
  profiles or a TV-specific backend.
- Changes to the Go engine, the gomobile facade, IPC protocol v1, the daemon,
  the CLI, the TUI, the Windows GUI or desktop release behavior.
- Phone UI restyling or phone navigation changes beyond the additive TV
  branch.
- IPv6 tunneling; IPv6 remains blackholed.
- Auto-update, in-app APK installation, telemetry or remote crash reporting.
- Localization; English-only UI, logical start/end layout retained.
- A custom TV keyboard; the system IME is used.
- A new Gradle module or product flavor.

## Decision log

| # | Decision | Rationale | Status | Source |
|---|---|---|---|---|
| D1 | Target Android TV + Google TV, API 26+, existing `arm64-v8a`/`armeabi-v7a` ABIs | Matches the app floor and the two TV families | decided | user |
| D2 | Full parity: setup, sign-in, connect, locations, all settings, account, logs, diagnostics | Matches the project's parity bar | decided | user |
| D3 | Dedicated 10-foot TV UI, not the phone layout | Remote-first and readable from the couch | decided | user |
| D4 | One APK installs on phones and TVs | No second package or release stream | decided | user |
| D5 | Android TV ships in v0.7.0 | Significant feature with its own release notes | decided | user |
| D6 | Sideload-only; no Play/F-Droid and no separate TV package | Existing distribution policy | decided | project |
| D7 | Non-goals: Watch Next media rows, voice search, gamepad-specific mapping, TV input service, ambient/screensaver control, multi-user profiles | Keeps TV support focused on the app | decided | user |
| D8 | Add a default Google TV launcher channel with **Open Kumiho** and **Connect** cards; requires `WRITE_EPG_DATA`; drop it if the launcher ignores it | User's app-shortcut-as-recommendation request; a channel is the supported mechanism | decided | user |
| D9 | Physical TV: Sony X90L, Android 12 / API 31; emulator: Google TV API 36 x86_64 | User's device plus a current TV OS | decided | user |
| D10 | Require TV emulator tests, physical X90L acceptance and a separate CI job | Catches both layout and real-device behavior | decided | user |
| D11 | Use `androidx.tv:tv-material:1.1.0` and `tv-foundation:1.0.0`, themed with the Kumiho palette | Stable TV focus-aware components | decided | user |
| D12 | Separate TV presentation under `ui.tv`; share view models, state and engine clients | TV needs different layout and focus, not different business logic | decided | user |
| D13 | Left navigation rail with Home, Locations, Account, Settings, More; More opens a TV page | TV-standard navigation with all destinations visible | decided | user |
| D14 | Dashboard is two-column: power/status left, location/quota/session right | Uses 16:9 width and reduces scrolling | decided | user |
| D15 | Setup stays four steps with TV copy; keep the notification step honest; Always-on explains unsupported state and still allows finish | Predictable setup without fake OS claims | decided | user |
| D16 | Firefox Accounts sign-in uses the in-app WebView and system keyboard; password-first fallback if D-pad navigation fails | Keeps the current flow with a practical fallback | decided | user |
| D17 | TV type scale: 16/18/20/24/32/40sp, recorded as a house-style departure | 12sp body text is unreadable at ten feet | decided | user |
| D18 | 48dp horizontal / 27dp vertical overscan margins with an 8dp spacing grid | Android TV safe-area guidance plus roomier layout | decided | user |
| D19 | Focus ring plus subtle scale-up; 48dp minimum targets | Visible focus is the TV accessibility floor | decided | user |
| D20 | TV logs export to a visible app path; update check opens an in-app WebView | TV may have no share target or browser | decided | user |
| D21 | Keep the required foreground notification and QS tile code, but never rely on a TV notification tray; explain Always-on honestly | TV has no phone-style tray or Quick Settings | decided | user |
| D22 | TV banner uses the existing fox + "Kumiho" wordmark at 320x180 xhdpi; add a 160x160 xhdpi TV icon; derive channel logo and poster art | Brand continuity without new artwork | decided | user |
| D23 | Documentation bundle: new `docs/android-tv.md`, plus README, `AGENTS.md`, `docs/android.md`, `docs/android-testing.md`, `CHANGELOG.md`, `NOTICE` | Keeps the canonical docs true | decided | user |
| D24 | Naming: `ui.tv`, `TvAppShell`, `TvDashboardRoute`, `TvEnvironment`, `make android-tv-emulator-test`, CI job `android-tv` | Clear TV boundary and discoverable commands | decided | user |
| D25 | TV performance targets: cold start <=3s on X90L, status updates <=500ms, no idle polling, memory <=280MB on 1GB-class TV, APK cap unchanged | TV-specific budgets without weakening phone targets | decided | user |
| D26 | Background VPN survival is a release blocker; probe before UI work and stop/re-plan if the tunnel dies on Home | A VPN that stops in the background is not usable on TV | decided | user |
| D27 | Channel visibility is probe-gated; remove the channel if Google TV does not show it | Avoid shipping an invisible surface | decided | user |
| D28 | If `tv-material` 1.1.0 is incompatible with the project's Compose 1.12.1, fall back to `tv-foundation` plus house Material3 visuals | Preserves the TV design without a library fork | assumed | recommendation |
| D29 | Connect card is best-effort direct: start the connect path; if setup, sign-in or VPN consent is missing, open the app at the relevant step; never fail silently | Makes the card useful without hiding errors | assumed | recommendation |
| D30 | If already connected, the Connect card opens the dashboard instead of toggling the tunnel | Predictable, avoids accidental disconnects | assumed | recommendation |
| D31 | The default channel auto-appears; respect a user hiding or removing it and do not force reinsertion | Follows Android TV channel lifecycle rules | assumed | recommendation |
| D32 | Channel programs use `TYPE_CLIP` with poster art and an 80dp channel logo, because TvProvider has no app-shortcut type | Closest supported card type | assumed | recommendation |
| D33 | TV logs go to `cacheDir/exports/`; show the path in a dialog and offer sharing only when a target exists | Reuses the existing export file without a broken share sheet | assumed | recommendation |
| D34 | If the update WebView cannot load, show the version and release URL as text | Keeps the check useful without a browser | assumed | recommendation |
| D35 | TV tests live in `dev.benyamin.kumiho.tv`, skip on phones, and the TV job filters by package; existing phone tests stay unchanged | Keeps both suites honest without a flavor split | assumed | recommendation |
| D36 | Add a separate `android-tv` CI job using `ReactiveCircus/android-emulator-runner` with `google-tv`, `tv_1080p`, API 36, x86_64; install the TV image in `android-setup.sh`; add `make android-tv-emulator-test` | The action supports Google TV images and gives a real TV launcher | assumed | recommendation |
| D37 | Use instrumentation package filtering for the TV suite; no new Gradle module or product flavor | Avoids changing release packaging | assumed | recommendation |
| D38 | Release assets stay the same three APKs; `versionCode` becomes 700; tagging and pushing remain out of scope | One app, one release stream | assumed | recommendation |
| D39 | No Go engine, IPC, desktop client or protocol changes; Android-only code | The engine is already platform-clean | decided | project |
| D40 | Security/privacy unchanged: app-private tokens, `allowBackup=false`, no telemetry, WebView cookies cleared on sign-out; only `WRITE_EPG_DATA` is new | The channel is the only new permission | decided | project |
| D41 | Rollback: remove TV package, assets, dependencies and manifest entries; phone behavior remains untouched | Additive worktree branch | assumed | recommendation |
| D42 | Add a superseded note to the old Android spec's TV non-goal; update all docs | Avoid contradictory design records | decided | user |
| D43 | TV UI remains English-only and uses the system IME; no custom keyboard | Existing localization policy | assumed | recommendation |
| D44 | Back from a tab exits to launcher; Back from More returns to the tab; setup Back moves one step | Standard TV behavior | assumed | recommendation |
| D45 | TV split tunneling, account and settings reuse existing view models and DataStore | No new persistence or engine surface | assumed | recommendation |
| D46 | Channel code only activates on TV devices; phones never publish it | Keeps phone behavior unchanged | assumed | recommendation |
| D47 | API 31 has no runtime notification prompt; API 33+ requests as today; no tray reliance | Honest permission behavior across TV OS versions | assumed | recommendation |
| D48 | TV orientation stays unlocked, so phones keep portrait and TVs use landscape | No manifest orientation lock | assumed | recommendation |
| D49 | Banner, TV icon, channel logo and poster art are derived from the existing fox; no new illustration | User's chosen banner direction | decided | user |
| D50 | The app stays one Gradle module; no new module or dynamic feature | Simplest build and release | assumed | recommendation |

## Architecture

### Boundaries and composition root

This is an Android-only change. The Go engine, the gomobile facade, the VPN
data path, IPC v1, the daemon and the desktop clients are untouched. The
Android app remains one Gradle module and one `VpnService`.

`TvEnvironment` answers one question: is this device a television? It checks
`Configuration.UI_MODE_TYPE_TELEVISION` and `PackageManager.FEATURE_LEANBACK`;
the result is read once at the UI root. `KumihoApp` chooses between the existing
phone `AppShell` and the new `TvAppShell`; the setup gate chooses between
`SetupFlow` and `TvSetupFlow`. Both paths share `AppContainer`, the engine, the
`VpnServiceCoordinator` and every view model. No phone composable is
restyled to fit TV.

`MainActivity` remains the single launcher activity for the app UI. TV deep
links from the launcher channel are handled by a small dedicated
`TvLauncherActivity` (transparent, no UI) that routes **Open Kumiho** to
`MainActivity` and **Connect** to the connect path or the relevant setup step.
The activity is exported because the TV launcher must start it; it never
exposes credentials and only starts the app's own service/activity.

### TV presentation layer (`ui.tv`)

The new package contains:

- `TvAppShell`: the only TV `Scaffold`, the left navigation rail (Home,
  Locations, Account, Settings, More), the TV top bar/back affordance, insets
  and overscan padding.
- `TvDashboardRoute`/`TvDashboardScreen`: power hero and live state in the left
  column; location, quota and session cards in the right column; a TV connect
  options page instead of the phone modal bottom sheet.
- TV routes/screens for Locations, Account, Settings, Split tunneling, Logs,
  Diagnostics and the More page. They consume the existing view models and
  expose the same actions; presentation, focus order and dialogs are TV
  specific.
- `TvSetupFlow`: four steps with TV chrome and copy, reusing the existing
  `SetupViewModel`, sign-in engine, permission controller and browser flow.
- TV design tokens and components: `TvTheme` (maps `KumihoColors` into the TV
  Material color scheme), `TvTypography` (16/18/20/24/32/40sp),
  `TvDimens` (8dp grid, 48dp/27dp overscan, 48dp targets), TV focus ring and
  scale, TV cards/rows/buttons/dialogs, and a TV page header.
- `TvEnvironment` and small TV-only Android helpers (channel publisher,
  launcher activity/receiver). Non-UI TV code may live in
  `dev.benyamin.kumiho.tv`; the UI package stays `ui.tv`.

The phone `ui/shell`, `ui/dashboard`, `ui/setup` and screen packages remain the
phone implementation. Shared business logic is not duplicated.

### Focus and navigation

Focus is the primary TV state. Every interactive composable gets a visible
focus ring plus a subtle scale (1.03-1.06) and, where useful, a container
highlight. Color is never the only focus signal. The focus components use
Compose focus modifiers (`focusRequester`, `focusProperties`,
`onFocusChanged`, `focusable`, `onKeyEvent`) or the Compose for TV surface
indicator, depending on the D28 probe.

Rules:

- Initial focus is the primary action on the screen (power on Home, the first
  row on Locations, the primary button in a dialog).
- Focus order follows visual order: rail -> content; left -> right; top ->
  bottom. Lists bring the focused item into view.
- Back from a tab exits to the TV launcher; Back from More returns to the
  previous tab; Back in setup moves one step; Back closes a dialog before
  leaving the screen.
- Modal bottom sheets are replaced by full-screen TV pages or focus-trapped
  dialogs. Drag gestures are never required.
- Text fields use the system IME; the on-screen keyboard is navigable by
  D-pad. No custom keyboard is added.
- A focus-restoration policy returns focus to the triggering row/card after a
  dialog, page or WebView closes.
- TV screens honor `WindowInsets` where present but always add the 48dp/27dp
  overscan padding, because TV safe areas are not reliably exposed as insets.

### TV theme, type and density

The same Go palette remains the source of truth: Light, Dark and OLED, six
accents, blue default, no Material You. `TvTheme` maps the semantic roles into
the TV Material color scheme; no raw hex appears in TV composables.

The TV type scale is 16/18/20/24/32/40sp, with 18sp body, 24sp titles and 40sp
display. It is a deliberate departure from the phone's 12/14/16/20/24/32sp
scale, recorded in `AGENTS.md` and the design notes. The TV spacing grid is 8dp
inside a 48dp/27dp overscan-safe frame. Controls are at least 48dp tall; the
power control and primary actions are larger.

### Screens and behavior

**Setup.** Four numbered steps: consent, sign-in, permissions, Always-on. The
TV layout uses a readable single column or a two-column form where it fits,
with the primary action fixed and focusable. Step 3 shows the real VPN consent
and notification states; on API 31 there is no runtime notification prompt,
while API 33+ requests it as today. Step 4 shows the real Always-on/lockdown
state, opens Android VPN settings when available, and allows finish without
connecting when the TV does not expose the setting. A finish failure stays on
step 4 with an inline error.

**Dashboard.** Two columns. Left: the TV power control, state word, connection
line, uptime, proxy-only badge and the Connect options action. Right: Location,
Quota and Session cards with the same facts as the phone dashboard. Connecting,
reconnecting, waiting, error, permission-needed and service-unavailable states
are identical in meaning. Location changes while connected still require an
explicit disconnect first.

**Locations.** The one-level country -> server tree, city context, search,
Ping all, expand/collapse and live speed sorting. Focus moves through country
rows and server rows; search opens the system IME; ping results and the
recommended choice update without moving focus unexpectedly.

**Account.** The same account list, add, re-authenticate, remove, use, label
and rotation actions. Add/re-auth uses the TV sign-in path (WebView or the
password fallback). Account notices remain visible on the TV screen.

**Settings.** All 21 catalog rows, grouped as on the phone, including the
Advanced group, overrides/reset, split tunneling and the Android-owned
Always-on/lockdown rows. TV-specific text editors (CIDR, MTU, exclusions) use
the system IME and preserve validation. No setting is silently hidden.

**More.** A TV page (not a bottom sheet) with Logs and Diagnostics. Back
returns to the previous tab.

**Logs.** The existing in-memory/rotating log view, filter and clear/reset
behavior. Export writes the existing log snapshot under `cacheDir/exports/`
and shows the full path in a TV dialog; a share action is offered only when a
share target exists. The path is copyable for support.

**Diagnostics.** The adapted doctor report, Android facts and TV facts (UI
mode, Leanback feature, model, OS/API, Always-on/lockdown state), reset local
state, and the battery/VPN checks that apply. It never claims a check the TV
cannot perform.

**Update check.** The existing manual check remains. On TV, the release page
opens in an in-app WebView; if the WebView cannot load, the dialog shows the
current version and the release URL as text. No background polling.

**Notification/tile.** The foreground service still posts its required
notification. TV has no phone-style tray, so the UI never depends on it; the
tile service remains compiled but is not a TV surface. The app shows status
in-app and after returning from the background.

### VPN lifecycle on TV

The existing `KumihoVpnService` remains the only tunnel owner. The service is
started on connect, enters the foreground before awaiting, keeps the engine
alive, and follows the existing `onTaskRemoved`, `START_STICKY`, revoke and
watchdog behavior. On TV:

- The service must continue after Home and after another app takes the
  foreground. The implementation may rely on the system's VPN binding and
  `START_STICKY`; it must not use a fake media foreground-service type.
- If Android TV's non-audio foreground-service policy kills the service, the
  release is blocked. The fallback investigation is the system Always-on VPN
  binding (when available) or another sound mechanism; that is a re-plan, not
  a silent workaround.
- The TV notification is still built and posted for Android's foreground
  requirement; no TV feature reads it.
- Always-on and lockdown are read from the OS and shown honestly. The app
  never turns them on itself.
- A watchdog redial keeps the VpnService interface/fd; a user reconnect after
  disconnect gets a fresh fd, exactly as on the phone.

### Launcher channel and deep links

A default `TvProvider` channel named **Kumiho** is published on TV devices. It
contains two `TYPE_CLIP` programs with poster art:

- **Open Kumiho** -> `kumiho://tv/open` -> `TvLauncherActivity` ->
  `MainActivity` (normal dashboard).
- **Connect** -> `kumiho://tv/connect` -> `TvLauncherActivity`:
  - if VPN consent and setup are ready, start the existing connect path;
  - if setup, sign-in or consent is missing, open `MainActivity` at the
    relevant setup step;
  - if already connected, open the dashboard;
  - never fail silently.

The channel is created/reconciled from an `INITIALIZE_PROGRAMS` receiver and
from a foreground app start. The channel logo is 80dp and opaque. The default
channel auto-appears; if the user hides or removes it, the app respects that
and does not force it back. `TvProvider` access is wrapped in try/catch
because the system can revoke provider permission.

The channel is probe-gated. If the Google TV launcher does not display the
default channel or the cards on either the emulator or the X90L, the channel
code and `WRITE_EPG_DATA` are removed before release and the limitation is
documented. The rest of TV support ships independently.

Security: the channel intents only open the app or start the user's own VPN
connect path. They carry no tokens, logs or account data. The exported
launcher activity accepts only the two known URIs and ignores anything else.
Connect still requires the system VPN consent and a signed-in account; a
missing prerequisite opens the app rather than toggling the tunnel.

### Testing and CI

- **Go:** unchanged; existing unit and race suites stay green.
- **Kotlin unit:** TV environment detection, channel reconciliation, Connect
  routing, TV log path/export, update-check state, TV typography/dimension
  mapping, back/focus reducers that can be tested off-device.
- **Phone connected:** the existing suite and phone UI remain unchanged.
- **TV connected:** a `dev.benyamin.kumiho.tv` test package, skipped on phones;
  the TV job runs it with an instrumentation package filter. It covers launch,
  setup, D-pad traversal of the rail and Dashboard, Locations focus, Settings
  and a dialog, Back behavior and the channel publisher. Screenshot tests cover
  Home, setup, Locations and Settings on TV in Light/Dark/OLED.
- **Physical X90L (API 31):** fresh install, launcher banner/icon, channel
  Open/Connect, setup, WebView/Password sign-in, VPN consent, connect/browse/
  exit IP/quota, Home and app-switch background survival, watchdog reconnect,
  disconnect/reconnect, Always-on/lockdown truth, split tunneling, proxy-only,
  logs export path, update WebView, remote-only traversal and performance/memory.
- **CI:** a separate `android-tv` job on ubuntu-24.04: JDK 17, Go, Android
  setup with the Google TV image, emulator AAR, JVM tests/lint/assemble, then
  `ReactiveCircus/android-emulator-runner` with `target: google-tv`,
  `profile: tv_1080p`, `api-level: 36`, `arch: x86_64` and the TV connected
  script. The phone `android` job stays as it is.

### Build, release and docs

`android-setup.sh` installs `system-images;android-36;google-tv;x86_64` in
addition to the phone image. A new `scripts/android-tv-connected-test.sh`
wraps the Gradle connected run with the TV package filter.
`make android-tv-emulator-test` builds the emulator AAR and runs it.

The release remains one Gradle module and the existing three APKs
(`arm64-v8a`, `armeabi-v7a`, universal) with the same signing flow. The version
is v0.7.0, `versionCode` 700. No new release asset or tag is required by this
spec; tagging/pushing remains a user action.

Docs: new `docs/android-tv.md` (requirements, install, launcher channel, setup,
remote navigation, TV limitations, testing); updates to README, `AGENTS.md`,
`docs/android.md`, `docs/android-testing.md`, `CHANGELOG.md`, `NOTICE`
(Compose for TV and TvProvider licenses), and the old spec's TV non-goal note.

## Risks + rollback

- **TV background-service kill (P0).** Android TV restricts non-audio
  foreground services when the app leaves. The VPN must survive. Mitigation:
  the first probe is connect -> Home -> another app -> wait -> verify tunnel
  and service; if it fails, stop and re-plan. Never fake a media FGS type.
- **WebView D-pad navigation (P1).** WebView focus has historically been
  fragile on TV. Mitigation: probe the Firefox Accounts page early; fall back
  to the native password form, with the WebView retained for accounts that
  need it. The sign-in step must never trap the remote.
- **Compose for TV compatibility (P1).** `tv-material` 1.1.0 is built against
  Compose 1.10.3 while the app uses 1.12.1. Mitigation: compile probe first;
  if it fails, use `tv-foundation` with house Material3 components and the
  same focus ring (D28). Pin versions in the catalog.
- **Google TV channel visibility (P2).** Third-party channels may be hidden or
  ignored. Mitigation: probe the default channel and both cards on the
  emulator and X90L; remove the channel and permission if it is not visible.
- **API spread (P2).** X90L is API 31; the emulator is API 36. Mitigation:
  physical acceptance on API 31, emulator on API 36, and targeted checks for
  notification permission, Always-on flags and foreground-service behavior.
- **Text entry on a remote (P2).** Password, email, CIDR and filter fields
  need the system IME. Mitigation: TV-specific field sizing, focus order,
  visible keyboard state and tests on the X90L.
- **Complex focus surfaces (P2).** Long location trees, split-tunnel lists,
  settings groups and dialogs can lose focus. Mitigation: shared TV focus
  components, bring-into-view, focus restoration and connected tests for the
  long lists.
- **TV memory/performance (P2).** The Go engine is large. Mitigation: measure
  on the X90L, keep the 60MB APK cap, avoid image-heavy TV surfaces, and meet
  the 280MB/1GB-class memory target or narrow the TV feature set.
- **Channel/provider permission (P3).** `WRITE_EPG_DATA` may be revoked or
  ignored. Mitigation: try/catch, no channel dependency for core VPN use, and
  the drop rule.
- **CI emulator cost (P3).** A second emulator job is slow. Mitigation: a
  separate job keeps the phone job fast; cache Gradle and the AAR; the TV job
  runs only the TV package.
- **Rollback.** Everything is additive: `ui.tv`, TV assets, TV manifest
  entries, `WRITE_EPG_DATA`, TV dependencies and the channel code. Removing
  them restores the phone-only app. No data migration exists; the same package
  and signing key keep user data on upgrade. Phone UI, engine and protocol v1
  are untouched.

## Dependencies / ordering

1. **Probe the unknown platform behavior first:** Compose for TV dependency
   resolution and a minimal TV shell; background VPN survival; WebView D-pad
   sign-in; Google TV channel visibility. The background probe is a go/no-go
   gate for the whole feature.
2. **Manifest, banner/icon assets, TV detection and theme tokens.** TV mode
   must be selectable and themed before screens.
3. **TV shell, focus components and Dashboard.** The primary navigation and
   the most-used screen.
4. **Setup, sign-in and Account.** The TV sign-in path and account surfaces.
5. **Locations, Settings and Split tunneling.** The long-list focus surfaces.
6. **Logs, Diagnostics, update check and TV log export.** The remaining parity
   surfaces.
7. **Launcher channel.** Only after the channel probe; keep it isolated so it
   can be dropped.
8. **TV emulator script, connected tests, CI job and physical checklist.**
9. **Docs, changelog, NOTICE and the old-spec note.**
10. **Full Go/Android gates and v0.7.0 release checks.**

The probes must land before the screens they de-risk. The channel must not
block the core VPN UI. The phone app must remain green after every task.

## Open questions

None. D28-D50 are explicitly assumed and were approved with the decision log;
any of them may be revisited at the spec or plan gate.
