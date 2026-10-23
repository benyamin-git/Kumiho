# Android "Keep connections outside the tunnel" — Implementation Plan

**Spec:** `docs/superpowers/specs/2026-10-23-android-keep-outside-connections-design.md` (approved, commit `5db510c`)
**Status:** Approved (plan gate #2, 2026-10-23)
**Date:** 2026-10-23
**Scope:** Android-only opt-in setting that marks Kumiho's VPN bypassable, so
LAN and route-excluded connections survive a connect/disconnect; no engine,
protocol, daemon, mobile-facade or desktop change, and no version bump.

## How to execute this plan

- Read the approved spec first, then this plan. The spec is authoritative for
  behavior; this plan is authoritative for sequencing, files and verification.
- Load `test-driven-development` before Task 1 and use it for every code task:
  add the listed test first, run it, see the expected failure, then implement.
- Load `design` before Task 3 and use the house style as the review checklist.
  This row must reuse the existing settings typography, spacing, `Switch` and
  warning tones; it introduces no new palette or component language.
- Load `verification-before-completion` before every completion claim. A task
  is done only after its listed command (or manual check) has passed.
- If the physical LAN SSH check still drops the session, stop and use
  `systematic-debugging`. Do not paper over it with a reconnect or a second
  setting.
- Tasks are commit-sized. One commit per task, after that task's gate is green.
  Do not bundle tasks. Do not push, tag, amend published commits or open PRs
  without an explicit request.
- After gate #2 approval, first update this plan's `Status` to
  `Approved (plan gate #2, 2026-10-23)` and commit it with
  `docs: add the keep-outside-connections plan`. Then start Task 1.
- No estimates are given; this plan describes order and evidence, not duration.

## Global constraints

### Non-goals (verbatim from the spec)

- No unconditional `allowBypass()`; the default stays strict.
- No engine, Go, control-protocol, daemon, mobile-facade, Windows GUI or TUI
  change. No new engine settings key.
- No change to `allow_lan`, `exclude_cidrs`, split tunneling, route
  computation, DNS handling, IPv6 policy or the VPN handshake.
- No automatic reconnect, no connect/disconnect interception, no prompt on
  connect.
- No new setup-wizard step, first-run prompt, diagnostics card, dashboard
  Details row, notification or Quick Settings action.
- No attempt to keep public, tunneled TCP sessions alive across a transition.
- No versionName/versionCode bump, tag, push or GitHub release. The second
  0.5.2 change is planned separately and will share the changelog section.
- No migration: an absent key is Off, which is already the safe behavior.

### Fixed names, copy and platform facts

- UI label: **Keep connections outside the tunnel**.
- Hint (D9): **Keeps LAN and other already-bypassed connections open when
  Kumiho connects or disconnects. Normal traffic still uses the tunnel, but
  apps that deliberately bind to Wi-Fi or mobile data can bypass it. Android
  lockdown still overrides this; applies on the next connect.**
- Reconnect line (D10): **The tunnel is up. Reconnect to apply this setting.**
- Lockdown line (D11): **Lockdown is on, so Android still closes connections
  when the VPN changes.**
- Internal names (D18): DataStore key `allow_bypass`,
  `AllowBypassRepository`, `BypassSettingsProvider`.
- New tags live under `settings_`: `BYPASS_ROW`, `BYPASS_VALUE`,
  `BYPASS_SWITCH`, `BYPASS_RECONNECT`, `BYPASS_LOCKDOWN`, `BYPASS_ERROR`.
- Android 8.0+/API 26+; the same behavior on API 26–32 and 33+ (D24). No
  route-math difference.
- Default Off for fresh and existing installs; no migration. The value is
  saved immediately and applies on the next connect (D3, D6).
- Android lockdown keeps the toggle editable and overrides the OS bypass
  (D8). The stored choice survives.
- `allow_lan` and split tunneling are independent of this setting (D5, D28).
- Public/tunneled connections still drop on a transition; only LAN and other
  route-excluded connections can survive (D12, spec success criterion 8).
- 0.5.2 changelog heading now, without a date; the next 0.5.2 change shares
  the section (D14).
- Tests: repository/provider unit, interface-spec unit, Compose emulator test,
  and the S25 FE LAN SSH acceptance (D15, D16).

## Implementation clarifications (resolved, none open)

These are implementation details needed to make the approved decisions
coherent. They do not change the product scope; they are flagged here for the
plan gate.

- **C1 — the builder parameter is appended, not inserted.** The spec's
  architecture sketch shows `buildVpnInterfaceSpec(apiLevel, config, split,
  allowBypass, appPackage)`. This plan keeps the existing positional signature
  and appends `allowBypass: Boolean = false` after `appPackage`. The only
  production caller (the bridge) passes it by name, and the default is the
  strict path, so existing call sites and tests keep compiling without
  silently enabling bypass. If the spec's exact order is preferred, every
  call site can pass the flag explicitly instead; behavior is identical.
- **C2 — the error field is a small enum, not a boolean.** The spec allows a
  nullable internal error field while the screen shows fixed copy. This plan
  uses `BypassError { LOAD, SAVE }` in `AllowBypassState`, so the two fixed
  lines are selected without showing exception text (D30).
- **C3 — the save-error line tells the truth about the restored value.** A
  failed write returns to the last persisted value, which can be **On**; the
  line therefore says the previous value is still in use instead of claiming
  Off. The load-error line keeps the spec's "stays off", because a failed read
  serves Off. This preserves success criterion 6.
- **C4 — reuse the split predicate directly.** `bypassReconnectRequired` calls
  the existing `splitReconnectRequired` rather than copying the full-tunnel
  state list. This is D26's one predicate.
- **C5 — `start()` is the recovery path; there is no `retry()` method.**
  `start()` is idempotent and restarts a collector that previously failed.
  `SettingsRoute` calls it on entry and `AppContainer.start()` also starts it.
- **C6 — `markApplied()` runs after `establish()` and before `detachFd()`.**
  This is the spec's order. The descriptor is closed in `finally`, so a
  failure in `markApplied()` cannot leak it.
- **C7 — no `SettingsCatalog` entry.** This is a bespoke app-owned row, not an
  engine key; the 19-key catalog and its reachability test stay unchanged. The
  screenshot index shifts from 19 to 20 because the row is inserted before the
  Connection header.

## Task 1 — Persistence and the synchronous provider

**Files**

- Modify `android/app/src/main/java/dev/benyamin/kumiho/data/AppPrefs.kt`.
- Create `android/app/src/main/java/dev/benyamin/kumiho/data/AllowBypassRepository.kt`.
- Create `android/app/src/main/java/dev/benyamin/kumiho/vpn/BypassSettingsProvider.kt`.
- Create `android/app/src/test/java/dev/benyamin/kumiho/data/AllowBypassTest.kt`.

**Interfaces**

`AppPrefs` implements the new store seam and adds the key. An absent key is
`false`.

```kotlin
class AppPrefs(context: Context) :
    ThemePreferences,
    SplitTunnelStore,
    BypassSettingsStore {

    override val allowBypass: Flow<Boolean> =
        store.data.map { it[KEY_ALLOW_BYPASS] ?: false }

    override suspend fun setAllowBypass(enabled: Boolean) {
        store.edit { it[KEY_ALLOW_BYPASS] = enabled }
    }

    private companion object {
        // ... existing keys ...
        val KEY_ALLOW_BYPASS = booleanPreferencesKey("allow_bypass")
    }
}
```

`AllowBypassRepository.kt` owns the store seam, state, error enum and
repository:

```kotlin
interface BypassSettingsStore {
    val allowBypass: Flow<Boolean>

    suspend fun setAllowBypass(enabled: Boolean)
}

enum class BypassError { LOAD, SAVE }

data class AllowBypassState(
    val enabled: Boolean = false,
    val applied: Boolean = false,
    val loading: Boolean = true,
    val error: BypassError? = null,
)

class AllowBypassRepository(
    private val store: BypassSettingsStore,
    private val scope: CoroutineScope,
) : BypassSettingsProvider {

    val state: StateFlow<AllowBypassState>

    fun start()

    override fun snapshot(): Boolean

    override fun markApplied()

    fun setEnabled(value: Boolean)
}
```

`vpn/BypassSettingsProvider.kt` mirrors `SplitSettingsProvider`:

```kotlin
interface BypassSettingsProvider {
    /** The cached value, synchronously; Off until the first load. */
    fun snapshot(): Boolean

    /** Commits the value used by the interface that was just established. */
    fun markApplied()
}
```

**Behavior**

- The repository holds `stateFlow = MutableStateFlow(AllowBypassState())`, a
  `persisted` boolean (last successfully stored value) and a
  `pendingApplied: Boolean?`. Keep the two mutable fields thread-safe
  (`@Volatile`, or otherwise confined): `snapshot()` and `markApplied()` are
  called from the bridge's connect goroutine, while the collector and writes
  run on the app scope.
- `start()` is idempotent: if the observation job is active, return; otherwise
  launch a collector. The collector sets `persisted = stored` and publishes
  `enabled = stored, loading = false, error = null`. A thrown read is caught
  (rethrowing `CancellationException`) and publishes
  `enabled = false, loading = false, error = BypassError.LOAD`.
- `snapshot()` returns `stateFlow.value.enabled` and records that value in
  `pendingApplied`. It never suspends and never reads DataStore.
- `markApplied()` consumes `pendingApplied` and copies it to `applied`; a
  missing pending value is a no-op. The bridge calls it only after
  `establish()` succeeds.
- `setEnabled(value)` updates `enabled` immediately with `error = null`, then
  launches the write. On success it sets `persisted = value`. On failure it
  restores `enabled = persisted` and sets `error = BypassError.SAVE`;
  cancellation is rethrown. It does nothing while `loading` is true (the switch
  is disabled during that window).
- Follow the existing `SplitTunnelRepository` write pattern; do not add a
  queue, a retry button or a second write path.
- This task is self-contained: no UI or bridge wiring yet, and the app still
  behaves exactly as before because the default is Off.

**Tests (write first)**

`AllowBypassTest.kt` uses `MainDispatcherRule(StandardTestDispatcher())`, an
in-memory `FakeStore`, and a `BrokenStore` whose flow fails once. Cover:

1. `a fresh repository is Off and loading` — initial state is
   `AllowBypassState()`; `snapshot()` is false.
2. `start loads the stored value` — initial true is published after
   `runCurrent()`; `loading` becomes false.
3. `a toggle is mirrored immediately and persisted` — after `setEnabled(true)`
   the state is true before the write runs; `runCurrent()` records the write
   and updates the fake.
4. `snapshot records the pending value until markApplied` — enabled true,
   applied false; `snapshot()` true; applied still false; `markApplied()` sets
   applied true.
5. `a failed load serves Off and stays retryable` — `BrokenStore` publishes
   `error = LOAD`, `loading = false`, `enabled = false`; after repairing and
   calling `start()` again the collector runs, clears the error and serves the
   stored value.
6. `a failed write restores the persisted value and reports save` — start with
   stored true; set `failWrites = true`; `setEnabled(false)` is optimistic,
   then `runCurrent()` restores true and sets `error = SAVE`; a later
   successful toggle clears the error.
7. `start is idempotent` — two `start()` calls while active increment the fake
   collection count once.

Expected: `BUILD SUCCESSFUL`, all seven tests pass.

**Verification**

```bash
make android-aar
. scripts/android-env.sh >/dev/null && cd android && \
  ./gradlew --offline :app:testDebugUnitTest \
  --tests 'dev.benyamin.kumiho.data.AllowBypassTest'
```

`make android-aar` is required before Gradle because Gradle never runs Go.
Expected: the targeted unit suite passes and the app still compiles.

**Commit:** `feat: add allow-bypass preference repository`

## Task 2 — Interface spec, bridge, service and container wiring

**Files**

- Modify `android/app/src/main/java/dev/benyamin/kumiho/vpn/VpnInterfaceSpec.kt`.
- Modify `android/app/src/main/java/dev/benyamin/kumiho/vpn/KumihoDeviceBridge.kt`.
- Modify `android/app/src/main/java/dev/benyamin/kumiho/vpn/KumihoVpnService.kt`.
- Modify `android/app/src/main/java/dev/benyamin/kumiho/AppContainer.kt`.
- Modify `android/app/src/test/java/dev/benyamin/kumiho/vpn/VpnInterfaceSpecTest.kt`.

**Interfaces**

Add the flag to the pure spec and builder input. The trailing default is C1.

```kotlin
data class VpnInterfaceSpec(
    val addresses: List<String>,
    val dnsServers: List<String>,
    val routes: List<String>,
    val excludedRoutes: List<String>,
    val allowedApps: List<String>,
    val disallowedApps: List<String>,
    val allowBypass: Boolean = false,
)

fun buildVpnInterfaceSpec(
    apiLevel: Int,
    config: VpnConfig,
    split: SplitSettings,
    appPackage: String,
    allowBypass: Boolean = false,
): VpnInterfaceSpec
```

`buildVpnInterfaceSpec` copies `allowBypass = allowBypass` into the returned
`VpnInterfaceSpec`; every other list is computed exactly as before.
`KumihoDeviceBridge` takes the provider as a third constructor dependency:

```kotlin
class KumihoDeviceBridge(
    private val service: VpnService,
    private val splitSettings: SplitSettingsProvider,
    private val bypassSettings: BypassSettingsProvider,
) : DeviceBridge
```

**Bridge behavior**

- Inside the existing `synchronized(lock)` block, read
  `bypassSettings.snapshot()` alongside `splitSettings.snapshot()` and pass it
  to `buildVpnInterfaceSpec(..., allowBypass = ...)`.
- After `.setMtu(mtu.toInt())`, call `builder.allowBypass()` **only** when
  `spec.allowBypass` is true. When false, make no bypass call and leave the
  builder's strict default.
- After `builder.establish()` returns a non-null descriptor, call
  `bypassSettings.markApplied()` before detaching the fd. Keep the existing
  `finally { descriptor.close() }`; no null or thrown build marks applied.
- Do not change route, DNS, app-list, exclusion or IPv6 ordering.

**Container and service wiring**

- `AppContainer` creates the app-scoped repository and exposes the seam:

  ```kotlin
  val allowBypassRepository = AllowBypassRepository(
      store = appPrefs,
      scope = scope,
  )
  val bypassSettingsProvider: BypassSettingsProvider = allowBypassRepository
  ```

- `AppContainer.start()` calls `allowBypassRepository.start()` after the
  existing repositories. It is idempotent.
- `KumihoVpnService.onCreate()` reads the container once and passes both
  providers:

  ```kotlin
  val container = (application as KumihoApplication).container
  bridge = KumihoDeviceBridge(
      this,
      container.splitSettingsProvider,
      container.bypassSettingsProvider,
  )
  ```

**Tests (write first)**

Extend `VpnInterfaceSpecTest.kt` with:

```kotlin
@Test
fun allowBypassIsOffByDefaultAndCopiedWhenRequested() {
    assertFalse(spec(33).allowBypass)
    assertTrue(
        buildVpnInterfaceSpec(
            apiLevel = 33,
            config = config(),
            split = SplitSettings(),
            appPackage = appPackage,
            allowBypass = true,
        ).allowBypass,
    )
}
```

The existing route/split tests must keep passing unchanged; they pin that the
new flag does not alter any list. Expected: all tests pass.

**Verification**

```bash
make android-aar
. scripts/android-env.sh >/dev/null && cd android && \
  ./gradlew --offline :app:testDebugUnitTest
```

Expected: the full JVM unit suite passes, including the new false/true
assertions and the existing `SplitInterfaceSpecTest` route/split guards;
`KumihoVpnService` and `AppContainer` compile.

**Commit:** `feat: mark android vpn bypassable on request`

## Task 3 — Settings row, state, warnings and Compose tests

**Files**

- Modify `android/app/src/main/java/dev/benyamin/kumiho/ui/settings/SettingsState.kt`.
- Modify `android/app/src/main/java/dev/benyamin/kumiho/ui/settings/SettingsScreen.kt`.
- Modify `android/app/src/main/res/values/strings.xml`.
- Modify `android/app/src/androidTest/java/dev/benyamin/kumiho/ui/settings/SettingsScreenTest.kt`.
- Modify `android/app/src/androidTest/java/dev/benyamin/kumiho/ui/settings/SettingsScreenshotsTest.kt`.
- Modify `android/app/src/androidTest/java/dev/benyamin/kumiho/ui/settings/SplitTunnelScreenshotsTest.kt`.
- Modify `android/app/src/test/java/dev/benyamin/kumiho/vpn/SplitInterfaceSpecTest.kt`
  (reconnect-predicate coverage).

**State and actions**

`SettingsState.kt` imports `dev.benyamin.kumiho.data.BypassError` and adds:

```kotlin
data class SettingsAppState(
    // ... existing fields ...
    val splitMode: SplitMode = SplitMode.OFF,
    val splitAppCount: Int = 0,
    /** The saved allow-bypass choice; Off until loaded. */
    val bypassEnabled: Boolean = false,
    /** The initial load finished, so the switch may be used. */
    val bypassLoaded: Boolean = false,
    /** A full tunnel is live and was built with a different value. */
    val bypassNeedsReconnect: Boolean = false,
    /** A fixed load/save error under the row, if any. */
    val bypassError: BypassError? = null,
)

data class SettingsActions(
    // ... existing required actions ...
    val onDismissLaunchError: () -> Unit,
    val onToggleBypass: (Boolean) -> Unit,
    val onOpenSplitTunneling: (() -> Unit)? = null,
)

object SettingsTags {
    // ... existing tags ...
    const val BYPASS_ROW = "settings_bypass_row"
    const val BYPASS_VALUE = "settings_bypass_value"
    const val BYPASS_SWITCH = "settings_bypass_switch"
    const val BYPASS_RECONNECT = "settings_bypass_reconnect"
    const val BYPASS_LOCKDOWN = "settings_bypass_lockdown"
    const val BYPASS_ERROR = "settings_bypass_error"
}
```

The new action is required, so every `SettingsActions(...)` call site is
updated deliberately: the route, `SettingsScreenTest`, `SettingsScreenshotsTest`
and `SplitTunnelScreenshotsTest`.

**Reconnect predicate**

Add next to `SettingsRoute` (same file, same package):

```kotlin
/**
 * A live full tunnel built with a different allow-bypass value. Reuse the
 * split screen's full-tunnel states; proxy-only and idle sessions never warn.
 */
internal fun bypassReconnectRequired(
    state: ServiceState,
    bypass: AllowBypassState,
): Boolean = !bypass.loading &&
    bypass.enabled != bypass.applied &&
    splitReconnectRequired(state)
```

No new state machine and no copied state list (C4). `ServiceState` comes from
`dev.benyamin.kumiho.vpn`; `AllowBypassState` comes from
`dev.benyamin.kumiho.data`.

**Route wiring**

- At the top of `SettingsRoute`, before the `splitOpen` early return, add:

  ```kotlin
  LaunchedEffect(Unit) { container.allowBypassRepository.start() }
  ```

  This makes re-entering Settings a read-recovery path without a retry button.
- Collect the state after the existing split state:

  ```kotlin
  val bypassState by container.allowBypassRepository.state
      .collectAsStateWithLifecycle()
  ```

- Compute the warning with `remember(serviceState, bypassState) {
  bypassReconnectRequired(serviceState, bypassState) }`.
- Pass to `SettingsAppState`:

  ```kotlin
  bypassEnabled = bypassState.enabled,
  bypassLoaded = !bypassState.loading,
  bypassNeedsReconnect = bypassReconnect,
  bypassError = bypassState.error,
  ```

  `bypassLoaded` is true after a load failure, so the switch stays enabled
  (D31).
- Wire `onToggleBypass = container.allowBypassRepository::setEnabled`.
- In `SettingsList`, insert one item after `SplitTunnelRow` and before the
  `Connection` header:

  ```kotlin
  item(key = "bypass") {
      BypassSettingRow(
          appState = appState,
          onToggle = actions.onToggleBypass,
      )
  }
  ```

  Extend the comment above the split item to say the app-level DataStore rows
  stay reachable while engine settings load or fail.

**Row rendering**

Add a stateless `BypassSettingRow` in `SettingsScreen.kt`:

- Outer `Column`, `fillMaxWidth()`, `testTag(BYPASS_ROW)`, horizontal
  `Dimens.ScreenPadding`, vertical `Dimens.Space2`.
- A `Row` with a weighted `Column` for the title (bodyLarge, onSurface) and
  hint (bodySmall, onSurfaceVariant), then the On/Off `Text`
  (`testTag(BYPASS_VALUE)`, `settings_on` / `settings_off`) and a `Switch`
  (`testTag(BYPASS_SWITCH)`, `checked = bypassEnabled`,
  `enabled = bypassLoaded`, `onCheckedChange = onToggle`).
- Under the row, in this order:
  - `BYPASS_RECONNECT` with the D10 text, `Icons.Filled.Info` and the primary
    tone;
  - `BYPASS_LOCKDOWN` with the D11 text, `Icons.Filled.Warning` and
    `LocalKumihoColors.current.warning`, shown whenever `lockdown` is true;
  - `BYPASS_ERROR` with the fixed load/save line, `Icons.Filled.Warning` and
    `LocalKumihoColors.current.loss`.
- The warning/error text is always present as words; colour is never the only
  cue. The switch remains visible and the On/Off word remains visible while
  loading or after an error.
- Use the existing `Dimens`, typography and icon sizes; add only the imports
  this row needs (`Switch`, `LaunchedEffect`, `Info`, and the state/error
  types). No new reusable component is required.

**Strings**

Add after the split-summary plurals in `strings.xml`:

```xml
<string name="settings_bypass_title">Keep connections outside the tunnel</string>
<string name="settings_bypass_hint">Keeps LAN and other already-bypassed connections open when Kumiho connects or disconnects. Normal traffic still uses the tunnel, but apps that deliberately bind to Wi-Fi or mobile data can bypass it. Android lockdown still overrides this; applies on the next connect.</string>
<string name="settings_bypass_reconnect">The tunnel is up. Reconnect to apply this setting.</string>
<string name="settings_bypass_lockdown">Lockdown is on, so Android still closes connections when the VPN changes.</string>
<string name="settings_bypass_load_failed">This setting could not be loaded, so it stays off.</string>
<string name="settings_bypass_save_failed">This setting could not be saved. The previous value is still in use.</string>
```

`settings_bypass_save_failed` is C3; the load line keeps the spec's wording.
Reuse `settings_on` / `settings_off`.

**Tests (write first)**

JVM — extend the reconnect section of `SplitInterfaceSpecTest.kt` with a
`changed = AllowBypassState(enabled = true, applied = false, loading = false)`
and assert:

- true for `CONNECTED`, `CONNECTING`, `RECONNECTING`, `WAITING_NETWORK`;
- false for idle, `PROXY_ONLY`, `FATAL`, unauthenticated, `loading = true`, and
  `enabled == applied`.

Compose — update the `appState()` helper in `SettingsScreenTest.kt` with
`bypassEnabled = false`, `bypassLoaded = true`, `bypassNeedsReconnect = false`,
`bypassError: BypassError? = null`; update `actions()` with
`onToggleBypass = { calls += "bypass-$it" }`. Add:

1. `theBypassRowShowsOffAndIsEnabled` — scroll to `BYPASS_ROW`; title and hint
   exist; `BYPASS_VALUE` shows `settings_off`; switch is enabled.
2. `theBypassSwitchWritesThroughAndNamesOn` — `bypassEnabled = true`; scroll to
   `BYPASS_ROW`; value shows On; click the switch; calls contain
   `bypass-false`.
3. `theBypassSwitchIsDisabledWhileLoading` — `bypassLoaded = false`; scroll to
   `BYPASS_ROW`; switch is not enabled; value still shows Off.
4. `theBypassReconnectWarningAppears` — `bypassNeedsReconnect = true`; scroll
   to `BYPASS_ROW`; the D10 text and `BYPASS_RECONNECT` exist.
5. `theBypassReconnectWarningIsAbsentByDefault` — scroll to `BYPASS_ROW`; the
   tag does not exist.
6. `theBypassLockdownWarningIsShown` — `lockdown = true`; scroll to
   `BYPASS_ROW`; the D11 text and `BYPASS_LOCKDOWN` exist.
7. `aBypassLoadErrorIsShown` — `bypassError = LOAD`, `bypassLoaded = true`;
   scroll to `BYPASS_ROW`; the load text and `BYPASS_ERROR` exist; switch stays
   enabled (D31).
8. `aBypassSaveErrorIsShown` — `bypassError = SAVE`, `bypassEnabled = true`
   (the restored persisted On value); scroll to `BYPASS_ROW`; the save text and
   `BYPASS_ERROR` exist.
9. `theBypassRowStaysReachableWhenEngineSettingsFail` — no snapshot, load
   error; scroll to `BYPASS_ROW`; the row is reachable and `GROUP_CONNECTION`
   is absent.

The row is inserted between the split item and the Connection header; the
tests assert reachability and content, not pixel order. The screenshot test
covers the visual placement.

Screenshots — in `SettingsScreenshotsTest.kt`:

- Add `bypassLoaded = true` to `appState`.
- Add `onToggleBypass = {}` to `noOpActions`.
- Change `ANDROID_START_INDEX` from 19 to 20 and update its comment to count
  the new app-owned row.

In `SplitTunnelScreenshotsTest.kt`, add `bypassLoaded = true` to `appState`
and `onToggleBypass = {}` to `settingsActions` so the Settings fixture still
renders a loaded row.

**Verification**

```bash
make android-test
make android-emulator-test
```

`make android-test` builds the AAR, runs all JVM unit tests, lint and the debug
APK. `make android-emulator-test` requires the booted AVD `kumiho-api36`; it
runs the connected suite including `SettingsScreenTest` and the screenshot
test. Expected: both commands exit 0 with no lint errors and no failed
connected tests. Do not claim Task 3 complete without both.

**Commit:** `feat: add keep-outside-connections setting`

## Task 4 — Docs, canonical design and 0.5.2 changelog

**Files**

- Modify `docs/android.md`.
- Modify `docs/android-testing.md`.
- Modify `docs/superpowers/specs/2026-10-21-android-client-design.md`.
- Modify `CHANGELOG.md`.

**`docs/android.md`**

1. Replace the settings-catalog paragraph (currently says "19 rows in total")
   with the catalog-plus-app-owned distinction:

   ```markdown
   Settings has four groups. The catalog is the desktop engine's settings, one
   row per key, plus two Android-owned read-only rows; there are **19 catalog
   rows** (17 engine settings and the 2 Android-owned Always-on/lockdown rows).
   The Kumiho app group also has the Split tunneling shortcut and the app-owned
   Keep connections outside the tunnel setting.
   ```

2. Add this bullet after **Split tunneling** in the Kumiho app group:

   ```markdown
   - **Keep connections outside the tunnel** — off by default. When on, Android
     treats the VPN as bypassable, so LAN and other route-excluded connections
     (for example an SSH session to a local host) stay open when Kumiho connects
     or disconnects. Normal traffic still uses the tunnel. Android lockdown
     overrides it, and it applies on the next connect.
   ```

3. Add a subsection after **Route exclusions**, before **Always-on VPN and
   lockdown**:

   ```markdown
   ### Keeping connections open across a transition

   Android closes every live TCP socket belonging to a VPN-routed app when the
   VPN's UID ranges change, unless the VPN is marked bypassable. `allow_lan`
   and `exclude_cidrs` change routing only; they do not stop that per-UID
   socket teardown. That is why an SSH session to a LAN host could drop when
   Kumiho connected or disconnected even though its packets never entered the
   tunnel.

   **Keep connections outside the tunnel** (Kumiho app, off by default) asks
   Android to treat the VPN as bypassable. With it on, a LAN or other
   route-excluded connection keeps the same local and remote addresses across a
   connect or disconnect and can stay open. Normal traffic still uses the
   tunnel; the trade-off is that an app can deliberately bind a socket or
   process to another network and leave the VPN. Android lockdown overrides the
   setting and still forces bypass off, and the row stays visible and editable
   in that state.

   The value is saved immediately and applies on the **next** connect. While a
   full tunnel is up, changing it shows *The tunnel is up. Reconnect to apply
   this setting.*; Kumiho never reconnects on its own. A proxy-only session
   builds no interface, so the setting has no effect there and shows no
   warning.

   Connections carried **through** the tunnel still drop on a connect or
   disconnect because their exit IP changes; TCP cannot migrate the stream.
   Only LAN and other route-excluded connections survive. The setting is
   independent of split tunneling and `allow_lan`.
   ```

4. In **Always-on VPN and lockdown**, add after the split/lockdown bullet:

   ```markdown
   - Lockdown forces the VPN to be non-bypassable. **Keep connections outside
     the tunnel** stays stored and editable, but Android still closes
     connections when the VPN changes while lockdown is on.
   ```

5. Leave the desktop-parity table's "19 settings rows" line alone: it refers
   to the engine rows, which do not change.

**`docs/android-testing.md`**

Add after the split-tunneling checklist, before the account/logs section:

```markdown
### Keeping connections open across a transition

This is the physical acceptance for **Keep connections outside the tunnel**
(Android-only; no engine or protocol change).

- [ ] On the S25 FE, with Always-on on and lockdown off, turn **Keep
      connections outside the tunnel** on **before** connecting. Open an SSH
      session to a LAN host, run a command, connect Kumiho's full tunnel, run
      the command again, disconnect, and run it again. The same session must
      survive both transitions. Record the SSH host, client app and Android
      build.
- [ ] With the setting off, the same LAN session drops on connect/disconnect
      (the previous behavior). With it on, a public SSH session carried
      through the tunnel still drops (its exit IP changes); this is expected.
- [ ] With lockdown on, the setting stays visible and editable, the override
      line is shown, and Android still drops the session when the VPN changes.
```

**Canonical design**

Append one row immediately after `D51` and before the `A1` assumptions:

```markdown
| D52 | Add an opt-in **Keep connections outside the tunnel** Android setting that calls `VpnService.Builder.allowBypass()`; Off by default, saved immediately, applied on the next connect, and Android lockdown still forces bypass off | Android destroys live TCP sockets of VPN-routed UIDs on every connect/disconnect unless the VPN is bypassable; `allow_lan`/`excludeRoute` only change routing. The setting preserves LAN and route-excluded sessions without weakening the default tunnel, and the docs state that public/tunneled connections still drop | decided | user |
```

**`CHANGELOG.md`**

Insert above `## [0.5.1]` (no date; D14):

```markdown
## [0.5.2]

Android: keep LAN connections alive across a VPN transition.

### Added

- **Keep connections outside the tunnel** on Android: an opt-in setting in
  Settings → Kumiho app that marks the VPN bypassable, so LAN and other
  route-excluded connections — for example an open SSH session to a local
  host — stay open when Kumiho connects or disconnects. It is off by default,
  applies on the next connect, and Android lockdown still overrides it.

### Fixed

- Android no longer destroys every live TCP socket of VPN-routed apps on a
  connect or disconnect when **Keep connections outside the tunnel** is on.
  Connections carried through the tunnel still drop on a transition because
  their exit IP changes; only LAN and other route-excluded connections
  survive.

### Notes

- Android-only: no engine, IPC, daemon, mobile-facade or desktop change; the
  DataStore key is absent (Off) on existing installs. No version bump yet; the
  next 0.5.2 change shares this section.
```

Add the link definition above `[Unreleased]` so the heading is a real link
until the release:

```markdown
[0.5.2]: https://github.com/benyamin-git/kumiho/compare/v0.5.1...HEAD
```

Leave the existing `[Unreleased]` link as it is. Do not add a date,
versionName/versionCode bump, tag or second changelog heading for this fix.
At release, change `[0.5.2]` to the release tag and `[Unreleased]` to
`v0.5.2...HEAD`.

**Verification**

```bash
git diff --check
grep -n "Keep connections outside the tunnel" \
  docs/android.md docs/android-testing.md CHANGELOG.md \
  docs/superpowers/specs/2026-10-21-android-client-design.md
grep -n "## \[0.5.2\]" CHANGELOG.md
grep -n "^\[0.5.2\]:" CHANGELOG.md
awk '/^## \[0.5.2\]/{a=NR} /^## \[0.5.1\]/{b=NR} END{exit !(a && b && a<b)}' CHANGELOG.md
```

Expected: `git diff --check` is silent; the phrase appears in all four files;
`0.5.2` appears and is above `0.5.1`; the link definition exists; the `awk`
exits 0. Read the new section once for the public-connection limitation and the
lockdown override.

**Commit:** `docs: document the keep-outside-connections setting`

## Task 5 — Full gates and physical acceptance

This task makes no tracked-file change and no commit; it is the final stop/go
gate. The only local write is the git-ignored `.ai-state` note at the end.

**Automated gates**

Run the complete Go gate from `AGENTS.md` even though no Go file changed:

```bash
make lint && make test && go test -race -count=1 ./... && make build && \
  GOOS=windows GOARCH=amd64 go vet ./... && \
  GOOS=windows GOARCH=amd64 go build ./... && \
  GOOS=android GOARCH=arm64 go build ./...
```

Run the Android gates:

```bash
make android-test
make android-emulator-test
```

Expected: every command exits 0. `make android-emulator-test` includes the
Compose Settings tests and the screenshot test; the AVD `kumiho-api36` must be
booted.

**Physical acceptance on the S25 FE**

Build and install the debug APK:

```bash
make android-debug
adb install -r android/app/build/outputs/apk/debug/app-debug.apk
```

Then, on the phone:

1. Keep Always-on on and lockdown off, matching the reproduction.
2. Turn **Keep connections outside the tunnel** on **before** connecting.
3. Open an SSH session to a LAN host (router, NAS or another LAN machine) and
   run a command such as `uptime`.
4. Connect Kumiho's full tunnel; run the command in the **same** session.
5. Disconnect; run the command again.
6. Expected: all three commands answer in the same session; no reconnect, no
   new SSH handshake. This is D16 and success criterion 7.

Do not claim the fix complete until the user reports this result. If the
session still drops, stop and debug; do not change the setting's meaning or add
an automatic reconnect. The documented expectations remain: with the setting
off, LAN sessions drop as before; public/tunneled sessions still drop on a
transition; lockdown overrides the setting.

After the manual check, verify the repository state:

```bash
git status --short
git log --oneline 5db510c..HEAD
```

Expected: a clean working tree and the plan commit plus the four task commits.
Rewrite the git-ignored `.ai-state` as one line, for example
`implemented keep-outside-connections; automated gates green; S25 FE result reported`.
Report the spec and plan paths, both planning commit hashes, the four task
commits, and the 13 assumed decisions.

**Commit:** none (acceptance gate; if the check fails, the fix is a new task
and commit, not an amendment of the four task commits).

## Dependencies / ordering

1. Task 1 (DataStore key, store seam, repository/provider and unit tests).
2. Task 2 (spec field, conditional builder call, bridge/service/container,
   interface-spec test).
3. Task 3 (Settings state, row, strings, reconnect predicate, Compose tests,
   screenshot index).
4. Task 4 (user guide, testing checklist, canonical `D52`, 0.5.2 changelog).
5. Task 5 (Go gate, Android gate, S25 FE acceptance).

Task 2 depends on Task 1's repository/provider. Task 3 depends on Task 2's
provider wiring and the repository state. Task 4 follows Task 3 so the docs
describe the final copy. Task 5 follows all four. Nothing depends on the
second 0.5.2 change.

## Risks and rollback

- **Privacy trade-off.** An app that deliberately binds to another network can
  leave the VPN when this is on. Mitigations: default Off, the D9 hint, the
  lockdown override and the user guide. This is the setting the user asked for.
- **Misleading expectation.** Users may expect public SSH sessions to survive.
  The hint, the user guide, the testing checklist and the changelog say
  explicitly that only LAN/route-excluded connections can survive; tunneled
  connections still drop because their exit IP changes.
- **Stale applied value.** `applied` is marked only after `establish()`.
  A failed build cannot suppress the reconnect warning. If the repository
  loads after a connect, the warning may appear for a session built with the
  default Off; that is accurate.
- **Store failure.** A failed read serves Off; a failed write restores the last
  persisted value and shows the fixed save-error line. The switch stays enabled
  after a load failure so the next toggle can retry the write; reopening
  Settings restarts the collector.
- **Builder default.** C1's trailing default is the strict path, and the only
  production caller passes the snapshot explicitly. If a future builder path
  forgets the flag, it stays strict rather than silently bypassable.
- **Compose screenshot index.** Inserting the row shifts the Android group by
  one; `ANDROID_START_INDEX` becomes 20. The connected suite catches compile
  errors, but the index is a visual framing detail; review the captured scene.
- **OEM variance.** The bypass flag is part of the `VpnService` contract, but
  OEM VPN stacks can differ. The S25 FE check is the acceptance evidence; the
  docs make no universal promise.
- **Rollback.** Revert the four task commits in reverse order. The DataStore
  key is ignored by older builds, absent means Off, and there is no migration,
  protocol change or persisted engine state to clean up.

## Definition of done checklist

- [ ] The Settings row appears immediately after **Split tunneling** in the
      Kumiho app group, with the D9 hint, On/Off text and a switch; fresh and
      existing installs default to Off.
- [ ] The row stays reachable while engine settings load or fail; it is not a
      `SettingsCatalog` key.
- [ ] When the setting is on, the next full-tunnel build calls
      `Builder.allowBypass()` before `establish()`; when off, the builder is
      unchanged; the interface-spec unit test covers both values.
- [ ] The choice is persisted immediately and applied on the next connect; the
      app never auto-reconnects.
- [ ] A live full tunnel with a different stored value shows the D10 reconnect
      line; idle and proxy-only sessions do not.
- [ ] Lockdown keeps the toggle editable and shows the D11 override line;
      Android still closes connections.
- [ ] A failed load serves Off; a failed write returns to the last persisted
      value; both show the fixed inline line and no retry button; the switch
      stays enabled after a load failure.
- [ ] On the S25 FE, with the setting on before connecting, one LAN SSH session
      survives one connect and one disconnect.
- [ ] The user guide, testing checklist, canonical `D52` and `CHANGELOG.md`
      describe the behavior and state that public/tunneled connections still
      drop.
- [ ] The repository/provider unit tests, interface-spec unit test, Compose
      Settings tests, `make android-test`, `make android-emulator-test` and the
      full Go gate pass.
- [ ] No engine/protocol/daemon/mobile-facade/desktop change; no version bump,
      tag, push or release.

## Open questions

None. The spec's 13 assumed decisions remain flagged for review: D10, D11,
D20, D21, D22, D24, D25, D26, D27, D28, D29, D30 and D31. The implementation
clarifications C1–C7 above are resolved and are the only new details for the
plan gate.
