# Android "Keep connections outside the tunnel" — Design Spec

**Status:** Approved
**Date:** 2026-10-23

## Objective / why

Kumiho's Android VPN currently kills every live TCP socket of every
VPN-routed app whenever the interface is established or torn down — even for
LAN destinations that `allow_lan` or `exclude_cidrs` already route outside the
tunnel. Android does this in `ConnectivityService.updateVpnUidRanges()` unless
the VPN is marked bypassable; Kumiho never calls
`VpnService.Builder.allowBypass()`, so the default is the destructive path.

This spec adds one opt-in, app-owned setting that marks the VPN bypassable.
With it on, connections already routed outside the tunnel (an SSH session to a
LAN host, a printer, a local service) survive a connect and a disconnect.
Normal traffic still uses the tunnel, the setting is off by default, and
Android lockdown still forces bypass off. Connections carried through the
tunnel still drop during a transition because their exit IP changes; that
limitation is documented rather than hidden.

## Success criteria / definition of done

1. **Settings → Kumiho app** shows **Keep connections outside the tunnel**
   immediately after **Split tunneling**, with the approved hint and an
   On/Off value beside a switch. It is Off on a fresh install and for existing
   installs with no stored value.
2. When the setting is on, the next full-tunnel connect calls
   `VpnService.Builder.allowBypass()` before `establish()`; when it is off, the
   builder is unchanged. Kotlin unit tests assert both paths.
3. The choice persists in the app's DataStore across process restarts and is
   read synchronously when the interface is built. It applies on the next
   connect; changing it never reconnects or disconnects the live session.
4. While a full tunnel is up, changing the value shows *The tunnel is up.
   Reconnect to apply this setting.* under the row. Idle and proxy-only
   sessions show no such warning.
5. With Android lockdown on, the row stays editable and shows the override
   warning; Android still closes connections because lockdown forces
   `allowBypass` off.
6. A failed store read serves Off; a failed store write returns to the last
   persisted value. Both show a short inline error under the row, with no new
   retry button.
7. On the S25 FE, with the setting on before connecting, an open SSH session to
   a LAN host survives one connect and one subsequent disconnect.
8. Public/tunneled connections still drop on a connect or disconnect; the user
   guide and changelog say so explicitly.
9. Go/Kotlin unit tests, the Compose Settings test and the existing Android
   lint/assemble gate pass. The Go gate from `AGENTS.md` stays green.
10. `docs/android.md`, `docs/android-testing.md`, the canonical Android design
    log (`D52`) and `CHANGELOG.md` describe the shipped behavior. No version
    bump, tag, push or release is part of this change.

## Scope

### In scope

- One new Android-owned boolean setting, its DataStore key, repository,
  synchronous provider, and container/service wiring.
- The conditional `Builder.allowBypass()` call and the `VpnInterfaceSpec` field
  that carries it.
- The Settings row, On/Off value, switch, hint, reconnect warning, lockdown
  warning and store-error line.
- Kotlin unit tests for the repository/provider and the interface spec, plus a
  Compose emulator test for the row and warnings.
- The physical-device LAN SSH acceptance check.
- Documentation, design-log and changelog updates for 0.5.2.

### Non-goals (explicitly out)

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

## Decision log

| # | Decision | Rationale | Status | Source |
| --- | --- | --- | --- | --- |
| D1 | Add an opt-in Android setting that calls `Builder.allowBypass()`; no unconditional bypass | Preserves the strict full tunnel by default and makes the privacy trade-off explicit | decided | user |
| D2 | Label: **Keep connections outside the tunnel** | Names the outcome and scopes the promise to connections already outside the tunnel | decided | user |
| D3 | Default **Off** for fresh and existing installs | No silent privacy weakening; current behavior stays until the user opts in | decided | user |
| D4 | Place the row in the **Kumiho app** group, immediately after **Split tunneling** | App-owned DataStore preference, reachable while engine settings load or fail; the Android VPN group stays read-only OS facts | decided | user |
| D5 | Independent of `allow_lan` and split tunneling; always enabled | One stable switch; the hint explains when it has an effect | decided | user |
| D6 | Persist immediately; apply on the next connect; never auto-reconnect | `allowBypass` is fixed when the interface is built; reconnecting would interrupt the session | decided | user |
| D7 | Inline reconnect warning only when a full tunnel is live and the interface used a different value | Prevents a false sense that the current session is protected | decided | user |
| D8 | Under lockdown keep the toggle editable and show an override warning | The stored choice survives; Android still forces bypass off | decided | user |
| D9 | Hint copy: **Keeps LAN and other already-bypassed connections open when Kumiho connects or disconnects. Normal traffic still uses the tunnel, but apps that deliberately bind to Wi-Fi or mobile data can bypass it. Android lockdown still overrides this; applies on the next connect.** | Discloses the effect, the bind-to-another-network caveat, lockdown and next-connect timing | decided | user |
| D10 | Reconnect copy: **The tunnel is up. Reconnect to apply this setting.** | Mirrors the split screen's existing sentence | assumed | recommendation |
| D11 | Lockdown copy: **Lockdown is on, so Android still closes connections when the VPN changes.** | States the OS override plainly | assumed | recommendation |
| D12 | User docs state explicitly that public/tunneled connections still drop | TCP cannot migrate to a new exit IP; no overpromising | decided | user |
| D13 | Docs: canonical Android design `D52`, `docs/android.md`, `docs/android-testing.md`, `CHANGELOG.md` | Records rationale, user behavior and the physical check | decided | user |
| D14 | Add a `0.5.2` changelog heading now, date later; no versionName/versionCode bump | The next change joins this release; bump at release time | decided | user |
| D15 | Tests: repository/provider unit, interface-spec unit, Compose emulator test, manual phone acceptance | Covers persistence, builder input and the row; OS behavior needs real hardware | decided | user |
| D16 | Physical acceptance: setting on, one LAN SSH session survives one connect and one disconnect on the S25 FE | The reported failure and the chosen outcome | decided | user |
| D17 | Settings only; no Diagnostics or dashboard surface | Smallest surface; the row and warnings suffice | decided | user |
| D18 | Internal names: `allow_bypass`, `AllowBypassRepository`, `BypassSettingsProvider` | Mirrors the Android API and root cause; the UI label stays separate | decided | user |
| D19 | Spec/plan slug: `android-keep-outside-connections` | Matches the user-facing name | decided | user |
| D20 | `VpnInterfaceSpec` carries `allowBypass`; the bridge calls `Builder.allowBypass()` only when true | Keeps the builder input pure and testable | assumed | recommendation |
| D21 | App-scoped repository with synchronous `snapshot()` and an `applied` value marked after a successful `establish()` | `acquireFD` never waits on DataStore; the warning reflects the real interface | assumed | recommendation |
| D22 | No engine, protocol, daemon, mobile-facade or desktop changes | `allowBypass` is a `VpnService.Builder` property, not an engine setting | assumed | recommendation |
| D23 | Store failure: serve Off, show a short inline error, no retry control | Safe default and honest state without a new retry surface | decided | user |
| D24 | Same behavior on API 26–32 and 33+ | `allowBypass` exists on every supported level; route math untouched | assumed | recommendation |
| D25 | Proxy-only sessions never show the reconnect warning | No TUN interface is built, so there is nothing to apply | assumed | recommendation |
| D26 | Reuse the split screen's full-tunnel states (`CONNECTED`, `CONNECTING`, `RECONNECTING`, `WAITING`) for the warning | One predicate, no new state logic | assumed | recommendation |
| D27 | Show On/Off text beside the switch, reusing the existing settings strings | Matches every boolean engine row; position is not the only cue | assumed | recommendation |
| D28 | The setting changes no route math and no `allow_lan`/split behavior | It only permits bypass and stops socket teardown | assumed | recommendation |
| D29 | No setup-wizard entry, first-run prompt or migration | Settings is the only surface; default Off needs none | assumed | recommendation |
| D30 | Error copy is fixed user-facing text; exception details are not shown | Keeps Settings readable and avoids leaking internals | assumed | recommendation |
| D31 | Disable the switch only during the initial load; after a load failure it stays enabled, so a toggle can retry the write | Safe default with a recovery path and no extra button | assumed | recommendation |

## Architecture

### The failure path being addressed

- `KumihoDeviceBridge.acquireFD()` builds the interface from `VpnConfig` and
  `SplitSettings` and never calls `Builder.allowBypass()`.
- `com.android.internal.net.VpnConfig.allowBypass` is `false` by default, so
  `NetworkAgentConfig.allowBypass` is false.
- `ConnectivityService.updateVpnUidRanges()` calls `maybeCloseSockets()` around
  both adding and removing the VPN's UID ranges. `maybeCloseSockets()` invokes
  `destroyLiveTcpSockets()` on Android 14+ (or `netd.socketDestroy()` below
  that) for every UID in the ranges unless `networkAgentConfig.allowBypass` is
  true.
- `Vpn.java` computes the agent's bypassable flag as
  `mConfig.allowBypass && !mLockdown`.
- `allow_lan` and `exclude_cidrs` only change routing. The socket teardown is
  per-UID and destination-agnostic, so a LAN socket is destroyed even though its
  packets were already leaving outside the tunnel.

### Persistence and provider

- `AppPrefs` gains a boolean Flow and writer for the `allow_bypass` key. An
  absent key means `false`. No migration is needed.
- A new `BypassSettingsStore` seam mirrors `SplitTunnelStore`, so JVM tests can
  use an in-memory fake.
- `AllowBypassRepository` is app-scoped and implements `BypassSettingsProvider`.
  Its state is:

  ```kotlin
  data class AllowBypassState(
      val enabled: Boolean = false,
      val applied: Boolean = false,
      val loading: Boolean = true,
      val error: Boolean = false,
  )
  ```

  The exact error field may be a nullable message internally; the screen shows
  fixed copy. `enabled` is the stored choice, `applied` is the value the live
  interface was built with, and `loading` is the first-read flag.
- `snapshot(): Boolean` returns the current `enabled` synchronously and records
  it as pending. `markApplied()` commits that pending value to `applied`; the
  bridge calls it only after `establish()` succeeds, so a failed interface does
  not look applied. This is the one extra provider method beyond the split
  seam.
- `setEnabled(value)` updates the in-memory state immediately and writes to
  DataStore. On a failed write it restores the last successfully persisted
  value and sets the error flag. On a failed read it serves `false` and sets the
  same flag; the switch stays enabled after the failure so the next toggle can
  retry the write.
- `start()` is idempotent and restarts a collector that previously failed.
  `SettingsRoute` calls it when Settings is entered, so reopening Settings is a
  recovery path without a retry button.
- `AppContainer` owns the repository, starts it with the other app-scoped
  repositories, and exposes it as `bypassSettingsProvider`.
  `KumihoVpnService.onCreate()` passes it to `KumihoDeviceBridge` alongside the
  split provider.

### Interface construction

- `VpnInterfaceSpec` gains `allowBypass: Boolean`.
- `buildVpnInterfaceSpec(apiLevel, config, split, allowBypass, appPackage)`
  copies the flag into the spec. Route, DNS, app-list and IPv6 rules are
  untouched; the existing tests continue to pin them.
- `KumihoDeviceBridge` takes the provider as a constructor dependency. Inside
  the same lock that reads `config` and `splitSettings`, `acquireFD()` reads
  `bypassSettings.snapshot()`, builds the spec, and calls
  `builder.allowBypass()` only when the spec says true.
- After `builder.establish()` returns a descriptor, the bridge calls
  `markApplied()` before detaching the fd. A null descriptor or a thrown build
  error does not mark anything applied.
- No ordering dependency exists between `allowBypass()` and the address, DNS,
  route, exclusion or app-list calls; it is set before `establish()`.

### Settings UI and state

- `SettingsAppState` gains the app-owned values needed by the stateless screen:
  `bypassEnabled`, `bypassLoaded`, `bypassNeedsReconnect`, `bypassError`.
  `lockdown` already exists.
- `SettingsActions` gains `onToggleBypass: (Boolean) -> Unit`; the route wires
  it to `AllowBypassRepository::setEnabled`. `SettingsRoute` also calls
  `repository.start()` on entry and collects its state.
- `SettingsList` adds one item after `SplitTunnelRow` and before the
  `Connection` header. A new stateless composable renders the title, hint,
  On/Off value and switch, then any of:
  - the reconnect warning, when `bypassNeedsReconnect` is true;
  - the lockdown warning, when `lockdown` is true;
  - the fixed store-error line, when `bypassError` is true.
- The switch is disabled while the repository is still loading. After a load
  failure it is enabled (D31). The On/Off word remains visible in every state.
- `bypassNeedsReconnect` is computed in the route with the same full-tunnel
  predicate the split screen uses (`splitReconnectRequired`): the stored value
  differs from `applied`, the repository has loaded, and the reduced state is
  `CONNECTED`, `CONNECTING`, `RECONNECTING` or `WAITING`. `PROXY_ONLY` and idle
  states never show it.
- New tags under `settings_`: `BYPASS_ROW`, `BYPASS_VALUE`, `BYPASS_SWITCH`,
  `BYPASS_RECONNECT`, `BYPASS_LOCKDOWN`, `BYPASS_ERROR`.
- New strings:
  - title: `Keep connections outside the tunnel`
  - hint: the D9 text
  - reconnect: the D10 text
  - lockdown: the D11 text
  - load error: a short fixed line saying the setting could not be loaded and
    stays off
  - save error: a short fixed line saying the setting could not be saved and
    stays off
  - On/Off: reuse the existing `settings_on` / `settings_off`

### Data flow

1. The user flips the switch in Settings.
2. `AllowBypassRepository.setEnabled()` updates its StateFlow and writes
   `allow_bypass` to DataStore. The screen shows the new value immediately.
3. On the next full-tunnel connect, the engine calls
   `KumihoDeviceBridge.acquireFD()`.
4. The bridge reads `snapshot()`, builds a `VpnInterfaceSpec` with
   `allowBypass`, and conditionally calls `Builder.allowBypass()`.
5. `establish()` creates the interface; the bridge calls `markApplied()`.
6. Android records `NetworkAgentConfig.allowBypass = true` (unless lockdown)
   and `ConnectivityService.maybeCloseSockets()` no longer destroys the live TCP
   sockets of the VPN-routed UIDs.
7. Normal traffic still uses the tunnel. Traffic to route-excluded destinations
   continues on the underlying network, which is why LAN sessions survive.
8. Toggling the value while connected only changes the warning; the live
   interface keeps the value it was built with until the next connect.

### Security and limitations

- `allowBypass()` is not a routing change for normal apps. Its visible security
  cost is that an app can explicitly bind a socket or process to another
  network (`ConnectivityManager.bindProcessToNetwork`, `Network.bindSocket`)
  and leave the VPN. The default is Off, the hint discloses this, and lockdown
  forces the flag off.
- Only connections whose path does not change can survive. A LAN or
  `exclude_cidrs` destination keeps the same local and remote addresses across
  the transition. A public connection carried through the tunnel changes its
  exit IP, so its TCP stream still breaks even though the socket is not
  destroyed. The docs state this explicitly.
- No TCP state is migrated, no connection is proxied across the transition and
  no app is restarted.
- The setting has no effect on proxy-only sessions because no interface is
  built.
- The OS behavior is AOSP `ConnectivityService`; an OEM could differ. The S25
  FE check is the acceptance evidence, and the docs do not promise behavior
  beyond Android's bypassable-VPN contract.

## Risks + rollback

- **Privacy trade-off.** A user who turns the setting on allows apps that
  deliberately bind outside the VPN to do so. Mitigations: default Off,
  explicit hint, lockdown override, and documentation. The user asked for a
  setting rather than an unconditional bypass.
- **Misleading expectation.** Users may expect every SSH session to survive.
  The hint, docs and changelog name the LAN/route-excluded scope and the
  public-connection limitation.
- **Stale applied value.** `applied` is marked only after a successful
  `establish()`, so a failed build cannot suppress the reconnect warning. If
  the repository loads after a connect, the warning may appear for a session
  that was built with the default Off; that is accurate, not a false positive.
- **Store failure.** Off is the safe fallback, and the inline error keeps the
  row honest. A failed write returns to the last persisted value; reopening
  Settings retries the read.
- **OEM variance.** The bypass flag is part of the `VpnService` contract, but
  OEM VPN stacks can differ. The physical test is the acceptance gate and the
  docs do not claim universal behavior.
- **Rollback.** Revert the commit series. The DataStore key is ignored by older
  builds, default Off reproduces the previous behavior, and there is no
  migration, protocol change or persisted engine state to clean up.

## Dependencies / ordering

1. DataStore key, store seam, repository/provider and their unit tests.
2. Interface-spec field and builder call, bridge/service/container wiring, and
   their unit tests.
3. Settings state, actions, row, strings and Compose test.
4. Documentation, canonical `D52`, `0.5.2` changelog entry.
5. Android gate, Go gate, and the S25 FE LAN SSH acceptance check.

Each step is independently verifiable and commit-sized. No step depends on the
second 0.5.2 change.

## Open questions

None. Every decision above is `decided` or `assumed`; the assumed entries are
flagged for review.
