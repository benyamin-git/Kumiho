# Android Locations One-Level Tree — Implementation Plan

**Spec:** `docs/superpowers/specs/2026-10-23-android-locations-one-level-design.md` (approved, commit `5acb2a6`)
**Status:** Approved (plan gate #2, 2026-10-23)
**Date:** 2026-10-23
**Scope:** Android-only rework of the Locations tab: one expandable level
(country → server, city as row context), one manual `Ping all`, best-latency
country rows and live speed sorting with `Recommended` pinned; no engine,
protocol, daemon, mobile-facade, Windows GUI or TUI change, and no version
bump.

## How to execute this plan

- Read the approved spec first, then this plan. The spec is authoritative for
  behavior; this plan is authoritative for sequencing, files and verification.
- Load `test-driven-development` before Task 1 and use it for every code task:
  add or update the listed tests first, run them, see the expected red state,
  then implement until green.
- Load `design` before Task 2 and use the house style as the review checklist.
  This change reuses the existing `KumihoRow`, `ToolbarAction`, `Dimens`,
  typography and latency strings; it introduces no new component language.
- Load `readme` before editing `README.md` in Task 4.
- Load `verification-before-completion` before every completion claim. A task
  is done only after its listed commands have passed.
- If a gate fails in a way the error does not explain, stop and use
  `systematic-debugging`; do not paper over it.
- Tasks are commit-sized. One commit per task, after that task's gate is green.
  Do not bundle tasks. Do not push, tag, amend published commits or open PRs
  without an explicit request.
- **Execution precondition:** the keep-outside-connections work must be
  implemented and committed first. Before Task 1, run
  `git log --oneline -8` and confirm the four task commits from
  `2026-10-23-android-keep-outside-connections.md` and that `CHANGELOG.md`
  contains `## [0.5.2]`. If either is missing, **stop and wait**; do not edit
  its files (`AppPrefs.kt`, `AllowBypassRepository.kt`,
  `BypassSettingsProvider.kt`, `VpnInterfaceSpec.kt`, `KumihoDeviceBridge.kt`,
  `KumihoVpnService.kt`, the Settings screen/state, canonical `D52`).
- After gate #2 approval, first update this plan's `Status` to
  `Approved (plan gate #2, 2026-10-23)` and commit it with
  `docs: add the android locations one-level plan`. Then start Task 1.
- No estimates are given; this plan describes order and evidence, not
  duration.

## Global constraints

### Non-goals (verbatim from the spec)

- The dashboard connect-options sheet's picker, the dashboard location cards,
  the notification, the Quick Settings tile and Diagnostics.
- The Windows GUI and TUI locations screens; desktop parity is intentionally
  broken here and documented.
- Engine, `internal/api`, `internal/ipc`, `internal/daemon`, `internal/mobile`
  or gomobile changes; no new protocol fields, commands or settings keys.
- Automatic pings on screen open, on resume, on filter change or on refresh
  (v0.5.0 D24 behavior stands: no background probes on mobile data).
- Persisting latency results or sort order across app restarts or engine
  rebinds; they stay session-local view state.
- New persisted UI state (expansion remains session-local, as today).
- A physical-device acceptance checklist item for this UI-only change.
- Version bump, tag, release PR or release publication for 0.5.2.

### Fixed names, copy and rules

- Country rows: flag prefix, name, `N servers` subtitle counting **every**
  listed server (quarantined included; D8), trailing best-latency text +
  caret. The plural is the renamed `locations_country_servers`:
  `%1$d server` / `%1$d servers`.
- Server rows: hostname title, subtitle `City · protocols · port` composed as
  new string `locations_server_city` = `%1$s · %2$s` around the existing
  `locations_server_protocols` / `locations_server_port` strings; the
  quarantine suffix keeps its existing separator and copy. Latency and the
  check mark stay trailing; servers are the only selectable rows (D3–D5).
- One action: `Ping all` / `Pinging %1$d…` while running. Nothing pings on
  open, resume, filter change, expansion or refresh (D6, D7).
- Country latency (D9): fastest measured RTT among the country's usable,
  non-quarantined servers; else `timeout` when at least one valid entry
  answered `null`/NaN/negative; else `—`. Latency copy is reused:
  `%1$d ms`, `timeout`, `—`.
- Sort rule (D11, D12): `Recommended` pinned first; then measured countries
  ascending by best RTT; then unmeasured/timeout countries in the engine's
  original order; servers inside an open country the same way. Equal keys keep
  input order (stable, explicit original-index tie-break). When a search is
  active the sort operates on the filtered view, so a city search orders
  countries by that city's servers.
- Search contract (D14): matches country name/code, city name and server
  hostname; matching branches auto-open without touching stored expansion;
  city codes stay unmatched.
- Toolbar (D15, D16): one row — Refresh, `Ping all`,
  `Expand all`/`Collapse all`. The `X visible · Y total` helper is gone; the
  toggle opens/closes countries only and stays disabled while searching.
- Selection (D17): unchanged — the card still shows `Country · City · host`,
  a tap persists the exact `host:port` with country and city codes through
  `select_location`, and Kumiho never connects from this screen.
- Removed names (D23): `LocationRow.CityRow`, city row tags
  (`LocationsTags.CITY_PREFIX`, `cityRowTag`), `LocationsTags.TEST_SPEED`,
  `LocationsTags.TARGETS_HELPER`, the strings `locations_targets_helper`,
  `locations_test_speed`, `locations_testing`,
  `locations_message_test_failed`, `LocationsMessage.TEST_FAILED`,
  `LocationsNotice.testFailed`, `LocationsNoticeAction.RETRY_TEST`,
  `LocationTree.pingTargets`, `LocationTree.visibleServers`.
- Android 8.0+/API 26+; existing typography, spacing and touch-target tokens;
  English-only; colour is never the only carrier of meaning.
- No version bump, tag or push; the shared `[0.5.2]` changelog section is
  appended, never duplicated.

## Implementation clarifications (resolved, none open)

These are internal details needed to make the approved decisions coherent.
They do not change product scope; they are flagged for the plan gate.

- **C1 — one new string composes the city subtitle.**
  `locations_server_city` = `%1$s · %2$s` is added next to the existing server
  strings, so the subtitle reads `New York · connect, quic · port 2499`. The
  quarantine suffix is appended exactly as today.
- **C2 — `CountryRow` keeps its shape; latency arrives via the state.** The
  data class stays `CountryRow(country, expanded)`; the screen reads
  `state.countryLatency(row.country)`, a new one-line helper over
  `LocationTree.countryLatencyOf`. Row keys stay unchanged, including
  `ServerRow`'s `server:<country>/<city>/<address>` (cityKey survives for
  this and only this purpose).
- **C3 — aggregation uses the same "usable server" predicate as Ping all.**
  Non-quarantined, non-blank hostname, port > 0. A country of only quarantined
  servers therefore shows `—` and sorts as unmeasured; a quarantined server
  can never lower a country's number or reorder a list. An entry present with
  `null`/NaN/negative is a timeout; an absent key is untested (the existing
  `latencyOf` distinction, preserved).
- **C4 — comparators are explicit and stable.**
  Countries: `(recommended ? 0 : 1, measured ? 0 : 1, bestRtt ?: 0.0,
  originalIndex)`; servers: `(measuredRtt ?: +∞, flattenedIndex)`.
  `Recommended` is pinned first even when unmeasured. Country ordering lands
  in Task 1 (pure); server ordering lands in Task 2 with the flatten that
  removes the city level — while city rows exist, servers cannot sort across
  them, so Task 1 does not emit a half-working server sort.
- **C5 — the code change is three commits so every commit compiles.** Task 1
  adds the pure ordering/latency rules and their test file while the city
  level and Test speed still work; Task 2 removes the city level and wires
  the sort; Task 3 removes Test speed and shrinks the toolbar. Existing tests
  that assert city rows or Test speed are updated in the task that removes
  them, not before.
- **C6 — a new pure test file owns the new rules.** `LocationTreeTest.kt`
  (JVM) tests the sort keys and `countryLatencyOf` directly; the view-model
  test keeps testing wiring and state transitions. There is no existing
  `LocationTreeTest.kt` today.
- **C7 — toolbar geometry.** Single `Row`: Refresh `IconButton` fixed at
  `Dimens.TouchTarget`, then `Ping all` and the toggle as equal members
  (`Modifier.weight(1f)`), `Arrangement.spacedBy(Dimens.Space1)`, the same
  screen padding. Country-row latency text is always visible (it is data);
  the caret is hidden while searching (the row is not clickable then).
- **C8 — the screenshot fixture shows the sort.** In
  `LocationsScreenshotsTest.treeState()`, `BER_ADDRESS` becomes `17.0` so the
  capture shows `REC` pinned first, then `Germany` (17 ms) before
  `United States` (42 ms) — the speed order is visible in the artifact.

## Task 1 — Pure speed ordering and country latency rules

**Files**

- Modify `android/app/src/main/java/dev/benyamin/kumiho/ui/locations/LocationTree.kt`.
- Create `android/app/src/test/java/dev/benyamin/kumiho/ui/locations/LocationTreeTest.kt`.

**Interfaces**

`rows(...)` gains a trailing `latency` parameter (C4: default keeps every
existing call site compiling). `expandedCities` still exists in this task.

```kotlin
fun rows(
    countries: List<Country>,
    query: String = "",
    expandedCountries: Set<String> = emptySet(),
    expandedCities: Set<String> = emptySet(),
    latency: Map<String, Double?> = emptyMap(),
): List<LocationRow>
```

```kotlin
/** The LatencyState a country row shows (D9). */
fun countryLatencyOf(latency: Map<String, Double?>, country: Country): LatencyState
```

Private helpers in the same object:

```kotlin
private fun measuredRtt(latency: Map<String, Double?>, server: Server): Double?
private fun bestMeasuredRtt(latency: Map<String, Double?>, country: Country): Double?
private fun orderedCountries(countries: List<Country>, latency: Map<String, Double?>): List<Country>
```

Server-level ordering is deliberately absent here: while city rows exist the
servers cannot sort across them, so the server comparator lands in Task 2
together with the flatten (C4).

**Behavior**

- `rows` becomes filter → country sort → flatten. `LocationFilter.apply`
  first (unchanged), then `orderedCountries`, then the existing city-level
  emission (a `CountryRow`, its `CityRow`s and server rows) exactly as today,
  so servers still appear in input order inside a city.
- `measuredRtt` returns the entry only when the key exists, the value is not
  null/NaN/negative, and the server is usable (C3); absent and timeout both
  return `null`.
- `bestMeasuredRtt` is the minimum of `measuredRtt` over the country's
  servers (absent when none).
- `countryLatencyOf`: measured best → `Measured(Math.round(best))`; else
  `Timeout` when at least one usable server has a **present** key whose value
  is null/NaN/negative (use `containsKey`; `Map.get` cannot distinguish absent
  from null); else `Untested`.
- `orderedCountries` sorts with the C4 comparator (stable, explicit
  original-index tie-break).
- `rows` never mutates its inputs and keeps the existing `CityRow` emission
  (removed in Task 2) so this commit compiles and all existing tests pass
  unchanged.

**Tests (write first)**

`LocationTreeTest.kt` — local fixtures: `REC` (city `rec`, server `auto`),
`US` (`nyc`: `edge:2499`, `share:443`; `sfo`: `west:443`), `DE` (`ber`:
`ber:443`, `quarantined:443` quarantined), `FR` (`par`: `par:443`), all built
with `LocationTree.addressKey`. Cover:

1. `unmeasured rows keep the engine order with Recommended pinned first` —
   `rows(listOf(US, REC, DE))` country keys are
   `[country:rec, country:us, country:de]`; expanding `us` yields the server
   rows `[edge, share, west]` in input order (city rows still between them in
   this task).
2. `measured countries sort first ascending and unmeasured keep engine order`
   — latency `US=80.0, DE=20.0` → `[rec, de, us]`; with `FR` added, `FR`
   (unmeasured) stays after both.
3. `Recommended stays pinned first even when others are measured` — latency
   `US=20.0` → `[country:rec, country:us]`.
4. `timeouts do not count as measurements` — latency `US=edge: null` (only),
   `DE=ber:30.0` → `[rec, de, us]`.
5. `countryLatencyOf takes the fastest measurement` — `edge=42.4,
   share=9.0` → `Measured(9)`; `edge=42.4, share=null` → `Measured(42)`.
6. `countryLatencyOf reads timeout when only timeouts answered` — `edge=null`
   → `Timeout`; nothing present → `Untested`.
7. `NaN and negative results are timeouts not measurements` — `edge=NaN`,
   `share=-1.0` → `Timeout` and the country still sorts as unmeasured.
8. `quarantined and unusable servers never set a country latency` —
   `quarantined=1.0` alone → `Untested`; `quarantined=null, ber=50.0` →
   `Measured(50)`; a server with blank hostname/port 0 carrying `1.0` is
   ignored (build a one-off country copy).
9. `searching sorts the filtered view` — latency `edge=10.0, west=200.0,
   par=50.0`; full-tree country keys `[rec, us, fr, de]`; with
   `query = "c"` the filter narrows `US` to San Francisco (city name match,
   country names do not match) and keeps `REC` and `FR`; filtered keys
   `[rec, fr, us]`, and `countryLatencyOf` of the filtered US is
   `Measured(200)` while the original US is `Measured(10)` (D12).
10. `host and city searches still auto-open the matching branch` — query
    `"berlin"` emits `country:de` plus its city and server rows (quarantined
    included); query `"edge.example"` emits `country:us` plus only the `edge`
    server row.

The server-ordering cases (measured-first, ties keep input order) are added
to this file in Task 2, when the flatten makes server rows sortable.

Expected: the new class is green; the existing `LocationsViewModelTest` still
passes unchanged (no behavior changes while `latency` is empty).

**Verification**

```bash
make android-aar
. scripts/android-env.sh >/dev/null && cd android && \
  ./gradlew --offline :app:testDebugUnitTest \
  --tests 'dev.benyamin.kumiho.ui.locations.LocationTreeTest' \
  --tests 'dev.benyamin.kumiho.ui.locations.LocationsViewModelTest'
```

`make android-aar` is required before Gradle because Gradle never runs Go.
Expected: `BUILD SUCCESSFUL`; all new tests pass; the existing Locations tests
pass untouched.

**Commit:** `refactor: add speed ordering to the android location tree`

## Task 2 — Flatten the tree (country → server) and wire the sort

**Files**

- Modify `android/app/src/main/java/dev/benyamin/kumiho/ui/locations/LocationTree.kt`.
- Modify `android/app/src/main/java/dev/benyamin/kumiho/ui/locations/LocationsViewModel.kt`.
- Modify `android/app/src/main/java/dev/benyamin/kumiho/ui/locations/LocationsScreen.kt`.
- Modify `android/app/src/main/res/values/strings.xml`.
- Modify `android/app/src/test/java/dev/benyamin/kumiho/ui/locations/LocationTreeTest.kt`.
- Modify `android/app/src/test/java/dev/benyamin/kumiho/ui/locations/LocationsViewModelTest.kt`.
- Modify `android/app/src/androidTest/java/dev/benyamin/kumiho/ui/locations/LocationsScreenTest.kt`.
- Modify `android/app/src/androidTest/java/dev/benyamin/kumiho/ui/locations/LocationsScreenshotsTest.kt`.

**Interfaces**

`LocationTree.kt`:

```kotlin
fun rows(
    countries: List<Country>,
    query: String = "",
    expandedCountries: Set<String> = emptySet(),
    latency: Map<String, Double?> = emptyMap(),
): List<LocationRow>
```

- Remove `expandedCities` and `LocationRow.CityRow`.
- `rows` becomes filter → country sort → flatten: after `orderedCountries`,
  each country emits its `CountryRow` and, when
  `searching || countryKey in expandedCountries`, its server rows from a new
  private helper:

```kotlin
private fun orderedServers(country: Country, latency: Map<String, Double?>): List<Pair<City, Server>>
```

  It flattens all cities in input order, sorts by
  `measuredRtt ?: Double.POSITIVE_INFINITY` with the flattened index as
  tie-break (C4), and returns `(city, server)` pairs. The `ServerRow` key
  (`server:<country>/<city>/<address>`) is unchanged, so row identity — and
  the lazy list's scroll position — survives a reorder.
- Keep `cityKey` (used by `ServerRow.key`), `CountryRow`, `ServerRow`,
  `latencyOf`, `pingAllTargets`, `pingTargets`, `visibleServers`
  (`pingTargets`/`visibleServers` are deleted in Task 3).
- Remove `LocationsTags.CITY_PREFIX` and the `cityRowTag(...)` function.
- Delete the `CityRow` emission from `rows` and update the object KDoc.

`LocationsViewModel.kt`:

- `LocationsState`: remove `expandedCities`; the `rows` getter becomes
  `LocationTree.rows(countries, query, expandedCountries, latency)`;
  `anyExpanded` becomes `expandedCountries.isNotEmpty()`; add:

```kotlin
fun countryLatency(country: Country): LatencyState =
    LocationTree.countryLatencyOf(latency, country)
```

- Remove `toggleCity(...)`; `toggleAll()` opens/clears
  `expandedCountries` only. Test speed state/actions stay for Task 3.

`LocationsScreen.kt`:

- `LocationsActions`: remove `onToggleCity` (route stops passing it).
- `Tree()`: drop the `CityRow` branch; pass `state.countryLatency(row.country)`
  to `CountryRow`.
- `CountryRow(row, searching, latency, onToggle)`: subtitle
  `pluralStringResource(R.plurals.locations_country_servers, count, count)`
  where `count = row.country.cities.sumOf { it.servers.size }`; trailing is a
  `Row` (spaced by `Dimens.Space1`) of `latencyText(latency)` plus, only when
  `!searching`, `ExpandGlyph(row.expanded)` (C7).
- Delete the `CityRow` composable.
- `ServerDetail(server, city)`: prefix the existing detail string with
  `stringResource(R.string.locations_server_city, city.name, detail)`; keep
  the quarantine suffix row unchanged. `ServerRow` passes `row.city`.

`strings.xml`:

- Rename `<plurals name="locations_city_servers">` to
  `<plurals name="locations_country_servers">` (same items).
- Add after `locations_server_port`:
  `<string name="locations_server_city">%1$s · %2$s</string>`.

**Tests (write first)**

`LocationTreeTest.kt` (extend Task 1's file):

- `servers sort measured first then keep input order` — expand `us`, latency
  `share=90.0, west=10.0` → server row keys `[west, share, edge]` after the
  country row.
- `equal measurements keep input order` — latency `share=42.0, west=42.0` →
  `[share, west, edge]`.

`LocationsViewModelTest.kt`:

- Replace the expansion test with `expanding and collapsing a country drives
  the visible rows`: `toggleCountry("US")` → keys
  `[country:rec, country:us, server:us/nyc/edge.example.net:2499,
  server:us/nyc/share.example.net:443, server:us/sfo/share.example.net:443,
  server:us/sfo/west.example.net:443, country:de]` (Flatten order, no
  latency), and collapsing returns 3 rows.
- `a query narrows the tree and clearing it restores the expansion`: open
  `us`, remember the keys, type `"germany"` → first key `country:de`, rows
  contain `LocationRow.ServerRow`s (BER + quarantined), `testTargets ==
  ["ber.example.net:443"]` (still valid this task); clearing restores the
  remembered keys.
- `Expand all opens every country and Collapse all closes them`: expanded
  `setOf("rec", "us", "de")`; collapsed → empty; no city set anywhere.
- Keep `a query with no match...` unchanged.
- Add `a country's latency is its fastest measured server`: run `pingAll()`,
  emit `EDGE 42.4`, `SHARE 9.0`, `WEST null`, `AUTO 20.0`,
  `BER_ADDRESS null`; expect `countryLatency(US) == Measured(9)`,
  `countryLatency(REC) == Measured(20)`, `countryLatency(DE) == Timeout`; and
  the country-row order is `[country:rec, country:us, country:de]` (REC
  pinned; US measured before unmeasured DE).
- Add `live results reorder the countries by measured speed`: run `pingAll()`,
  emit `AUTO 20.0`, `EDGE 42.0`, `SHARE null`, `WEST null`,
  `BER_ADDRESS 5.0`; the country keys become
  `[country:rec, country:de, country:us]` — DE moves ahead of US despite the
  engine order, proving `state.rows` consumes `latency`.

`LocationsScreenTest.kt`:

- Fixture: remove the `expandedCities` parameter and default
  `expandedCountries = setOf("us")`; `LocationsActions` loses
  `onToggleCity`.
- `theTreeIsRECFirstAndNamesTheRecommendedCountry`: use
  `expandedCountries = setOf("rec", "us")`.
- `aServerRowShowsItsHostProtocolsPortAndLatency`: assert the new subtitle
  `"New York · connect, quic · port 2499"`; scope latency assertions to row
  tags (`serverRowTag("us", "nyc", EDGE_ADDRESS)` shows `42 ms`;
  `countryRowTag("us")` shows `42 ms`) because the country row now carries the
  same number; keep the `—` first-match assertion and the `timeout` and
  `1 234 ms` assertions (each still unique).
- Add `aCountryRowShowsItsServerCount`: default state — `countryRowTag("us")`
  contains the `locations_country_servers` quantity string for 5 (NYC's four
  including the quarantined one plus SFO's one), `countryRowTag("rec")` and
  `countryRowTag("de")` each for 1.
- Add `anExpandedOrCollapsedCountryRowShowsItsBestLatency`: latency
  `EDGE=10.0, SHARE=5.0, WEST=null, BER=61.0` with
  `expandedCountries = setOf("us")` — `countryRowTag("us")` contains `5 ms`;
  `countryRowTag("de")` (collapsed) contains `61 ms`.
- Add `aServerRowNamesItsCity`: `serverRowTag("us", "nyc", SHARE_ADDRESS)`
  contains `"New York · connect · port 443"`.
- Remove `tappingACityRequestsItsToggle`; keep the country toggle test.

`LocationsScreenshotsTest.kt`:

- `treeState()`: remove `expandedCities`; set the `BER_ADDRESS` fixture to
  `17.0` (C8); keep the rest.
- `noOpActions()`: remove `onToggleCity`.

**Verification**

```bash
make android-aar
. scripts/android-env.sh >/dev/null && cd android && \
  ./gradlew --offline :app:testDebugUnitTest
make android-emulator-test   # AVD kumiho-api36 must be booted
```

Expected: the full JVM suite and the full connected suite pass. The connected
suite includes `LocationsScreenTest`, `LocationsScreenshotsTest` and every
other module; it must be green before this task is complete. Boot instructions
for the AVD are in `docs/android-testing.md` (section "Instrumented tests on
the emulator"); the screenshot PNGs are pulled from
`files/locations-screenshots` if a visual check is wanted.

**Commit:** `feat: flatten the android locations tree`

## Task 3 — One ping action and a one-row toolbar

**Files**

- Modify `android/app/src/main/java/dev/benyamin/kumiho/ui/locations/LocationTree.kt`.
- Modify `android/app/src/main/java/dev/benyamin/kumiho/ui/locations/LocationsViewModel.kt`.
- Modify `android/app/src/main/java/dev/benyamin/kumiho/ui/locations/LocationsScreen.kt`.
- Modify `android/app/src/main/res/values/strings.xml`.
- Modify `android/app/src/test/java/dev/benyamin/kumiho/ui/locations/LocationsViewModelTest.kt`.
- Modify `android/app/src/androidTest/java/dev/benyamin/kumiho/ui/locations/LocationsScreenTest.kt`.
- Modify `android/app/src/androidTest/java/dev/benyamin/kumiho/ui/locations/LocationsScreenshotsTest.kt`.

**Interfaces**

`LocationsViewModel.kt`:

- `LocationsState`: remove `testing` and `testTargets`; `canTest` is deleted;
  `canPingAll` becomes
  `!pingingAll && !loading && loadError == null && allTargets.isNotEmpty()`.
- Remove `testSpeed()`; `pingAll() = startPing(allTargets)`; the private
  `startPing(targets: List<String>)` takes one argument: the notice it clears
  is `RETRY_PING_ALL`, and a failure publishes
  `LocationsNotice.pingAllFailed(detail)`. `stopPing()` clears `pingingAll`
  and `pingAllCount` only. The run window and `pendingPings` logic are
  unchanged.
- Remove `LocationsMessage.TEST_FAILED`, `LocationsNotice.testFailed`,
  `LocationsNoticeAction.RETRY_TEST` and the `retryNotice` branch for it.

`LocationTree.kt`: delete `visibleServers(rows)` and `pingTargets(rows)`
(nothing else calls them once Test speed is gone; `pingAllTargets` stays).
Delete `LocationsTags.TEST_SPEED` and `LocationsTags.TARGETS_HELPER`.

`LocationsScreen.kt`:

- `LocationsActions`: remove `onTestSpeed` (route stops passing it); drop the
  `TEST_FAILED` arm of `messageResource`.
- `Toolbar`: replace the two-row `Column` with one `Row` (C7): the Refresh
  `IconButton` (own spinner while refreshing, `LocationsTags.REFRESH`), then
  `Ping all` (`LocationsTags.PING_ALL`; label `Pinging %1$d…` with
  `state.pingAllCount` while `state.pingingAll`, enabled by
  `state.canPingAll`) and the expand/collapse `ToolbarAction`
  (`LocationsTags.TOGGLE_ALL`, unchanged labels and `state.canToggleAll`) both
  with `Modifier.weight(1f)`. The helper `Text` and its `Spacer` are removed
  with the old second row.

`strings.xml`: delete `locations_test_speed`, `locations_testing`,
`locations_targets_helper`, `locations_message_test_failed` (see the removed
names list in the constraints).

**Tests (write first)**

`LocationsViewModelTest.kt`:

- Delete the Test-speed tests: `Test speed sends one ping command for the
  visible targets`, `Test speed with nothing visible is a no-op`, `a failed
  ping command stops testing and offers a retry`, `Test speed and Ping all are
  mutually exclusive in both orders`.
- Rename `nothing pings until Test speed is tapped` to
  `nothing pings until Ping all is tapped`: same interactions, then
  `gateway.pingCalls.isEmpty()`, `!pingingAll`, `latency.isEmpty()`.
- Drive the surviving run tests with `pingAll()` and drop every
  `testing`/`canTest`/`testTargets` assertion:
  `a null round trip is a timeout and distinct from an untested host`,
  `a run ends when every target has answered`, `a run ends after the window
  even without answers`, `Ping all sends every loaded target even when the
  filter shows one city` (drop the `testTargets` line),
  `Ping all keeps its count until the last host answers` (drop
  `assertFalse(running.testing)`), `a failed Ping all offers a retry that
  repeats the whole tree`, `the ping window scales with the target count`.
- Rewrite `a new test replaces the hosts it probed and keeps the others` as
  `a new run replaces its hosts' previous results`: run `pingAll`, answer all
  five (`EDGE 42.0`, `SHARE null`, `WEST 88.0`, `AUTO 20.0`,
  `BER_ADDRESS 61.0`), run `pingAll` again, answer `EDGE 7.0` only, and assert
  `latencyOf(EDGE_SERVER) == Measured(7)` while the other four keep their
  values.
- Keep `Expand all ...` and the country-latency test from Task 2; they are
  already pingAll-driven.

`LocationsScreenTest.kt`:

- Delete `TestSpeedIsOfferedForTheVisibleServers`,
  `TestSpeedIsDisabledAndNamedWhileItRuns`,
  `TestSpeedIsDisabledWhenTheQueryMatchesNothing`,
  `theToolbarCountsVisibleAndTotalTargets`,
  `PingAllIsDisabledWhileTestSpeedRuns`,
  `TestSpeedIsDisabledWhilePingAllRuns`.
- Update `PingAllTestsTheWholeTreeEvenWhenTheQueryShowsNothing`: drop the
  helper/Test-speed lines; keep the query state, assert `PING_ALL` enabled and
  that the click records `ping-all`.
- Update `PingAllIsDisabledAndNamedWhileItRuns`: unchanged except the fixture
  loses `testing` support.
- Add `theToolbarIsOneRowWithRefreshPingAllAndTheToggle`: all three tags
  exist; the literal tags `"locations_test_speed"` and
  `"locations_targets_helper"` do not; and the tops of `REFRESH`, `PING_ALL`
  and `TOGGLE_ALL` are within 2 px of each other
  (`assertEquals(topA, topB, 2f)` on `boundsInRoot`).
- `everyPrimaryControlMeetsTheTouchTargetFloor`: fixture notice becomes
  `LocationsNotice.pingAllFailed("no probes")`; drop the `TEST_SPEED` tag.
- Fixture: remove the `testing` parameter; `actions()` drops `onTestSpeed`.

`LocationsScreenshotsTest.kt`: `noOpActions()` drops `onTestSpeed` (fixture
otherwise unchanged).

**Verification**

```bash
make android-aar
. scripts/android-env.sh >/dev/null && cd android && \
  ./gradlew --offline :app:testDebugUnitTest
make android-emulator-test   # AVD kumiho-api36 must be booted
```

Expected: both suites green. Additionally confirm no stragglers:

```bash
grep -rn "testSpeed\|TEST_SPEED\|testTargets\|canTest\|pingTargets\|visibleServers" \
  android/app/src/main android/app/src/test android/app/src/androidTest
```

Expected: no output.

**Commit:** `refactor: reduce android location pings to one action`

## Task 4 — Docs, canonical design and the shared 0.5.2 changelog

**Files**

- Modify `docs/android.md`.
- Modify `docs/android-testing.md`.
- Modify `docs/superpowers/specs/2026-10-21-android-client-design.md`.
- Modify `CHANGELOG.md`.
- Modify `README.md` (load the `readme` skill first).

**`docs/android.md`**

1. Replace the **Locations** section (the country/city/server tree paragraph,
   the two toolbar rows and the Test-speed paragraph, up to but not including
   the "Selecting a row saves it…" paragraph) with:

   ```markdown
   **Locations** loads the full country/server list, with the engine's
   recommended entry first. Country rows carry their country-flag emoji, and so
   do the connect-sheet country picker and the current-selection summary;
   Recommended and servers stay unprefixed. A country row expands directly to
   its servers — there is no city level — and its subtitle counts them
   (`N servers`, quarantined included). Server rows name the city as context:
   `City · protocols · port`, with the per-server latency trailing. Search
   matches country name/code, city name and server hostname, case-insensitively;
   matching branches open while the query is active and closing the search
   restores the previous expansion. Refresh keeps the previous tree on failure
   and offers one Retry.

   The toolbar is one row: Refresh, **Ping all** and
   **Expand all**/**Collapse all**. Ping all pings every usable server in the
   loaded list regardless of the filter or expansion, deduped by `host:port`
   and skipping quarantined entries; its button reads **Pinging N…**, and the
   button stays disabled until the run ends. A measured round trip shows
   milliseconds, an unanswered probe shows **timeout**, and an untested host
   shows **—**; nothing pings on load, typing, expanding, refreshing or
   resuming. The run window is 12 seconds for a normal list and scales with
   the target count (1.5 seconds per 24-host batch plus margin). Expand all
   opens every country, and the label flips to Collapse all while anything is
   open; the control is disabled while the search field is non-empty.

   Collapsed country rows show their best latency — the fastest measured RTT
   among their servers, or **timeout** when only timeouts came back, or **—**
   before any run — and the list re-sorts live as results arrive: measured
   countries first, ascending, then the rest in engine order, with
   Recommended pinned first; servers inside a country the same way.
   ```

   The "Selecting a row saves it…" paragraph then keeps its facts with one
   change: "skipped by the speed test" becomes "skipped by Ping all".
2. Parity table: change the row to
   `| Locations tree, refresh, Ping all, Expand all | Locations one-level list, live filter, speed order, 24-way ping |`
   and add one sentence after the table:
   `Android deliberately diverges in one place: the Locations tab is one expandable level (country → server, city as row context) and re-sorts by measured speed, while the desktop trees keep country → city → server.`

**`docs/android-testing.md`**

- Run the connected suite (this task's verification), read the reported
  total, and update
  `Current expected result: **133 tests, 0 failures, 0 errors, 0 skipped**.`
  to the measured number. No other wording in that section changes.

**Canonical design log**

Insert one row immediately after `D52` (parallel work owns `D52`; appending
after it is required):

```markdown
| D53 | Android Locations is one expandable level (country → server, city as server-row context) with a single manual **Ping all**, best-latency country rows and live speed sorting (**Recommended** pinned first); the dashboard connect picker and the desktop trees keep country → city → server | Removes a disclosure level and the Test speed/Ping all split; the latency order explains the list, and pings stay manual (the no-background-probes rule) | decided | user |
```

**`CHANGELOG.md`**

Inside the existing `## [0.5.2]` section (created by the
keep-outside-connections work), insert between its `### Added` and
`### Fixed` blocks:

```markdown
### Changed

- Android Locations is now one expandable level: countries open directly to
  their servers, the city name moves into each server row, the
  `X visible · Y total` helper and the separate `Test speed` action are gone,
  and a single **Ping all** still measures the whole list.
- Android country rows show their best latency and server count, and the list
  re-sorts live by measured speed while **Ping all** runs: measured countries
  first, ascending, with **Recommended** pinned and unmeasured or timed-out
  entries keeping their original order.
```

Append to that section's `### Notes`:

```markdown
- The Android list now shows one expansion level and sorts by measured speed;
  the desktop trees keep country → city → server. Latency is session-local and
  never persisted.
```

Do not duplicate the `## [0.5.2]` heading or its link definition, do not add
a date, and do not change the parallel work's Added/Fixed/Notes entries.

**`README.md`**

- In the Android paragraph (the "same surfaces as the desktop clients"
  sentence), replace
  `locations with country flags, a filter, Test speed/Ping all and Expand all/Collapse all`
  with
  `locations with country flags, a search, one **Ping all** action that orders the list by measured speed, and Expand all/Collapse all`.
- Leave the v0.5.1 release-news lines and the Windows GUI/TUI bullets
  untouched (they are true for the desktop release and its screenshots).

**Verification**

```bash
git diff --check
grep -n "Test speed\|X visible\|targets_helper" docs/android.md   # expect no output
grep -n "orders the list by measured speed" README.md
grep -n "Test speed" README.md                                    # only the desktop bullets remain
grep -n "| D53 |" docs/superpowers/specs/2026-10-21-android-client-design.md
grep -n "## \[0.5.2\]" CHANGELOG.md
awk '/^## \[0.5.2\]/{a=NR} /^## \[0.5.1\]/{b=NR} END{exit !(a && b && a<b)}' CHANGELOG.md
grep -rn "133 tests" docs/ || true
make android-emulator-test   # record the printed total for the count edit
```

Expected: `git diff --check` is silent; the `docs/android.md` grep reports
nothing (the Android guide no longer mentions Test speed or the helper); the
README's Android paragraph contains the new sentence while the only remaining
"Test speed" mentions are the Windows GUI/TUI bullets; D53 exists after D52;
`0.5.2` sits above `0.5.1` and the `awk` exits 0; the old `133 tests` text
appears nowhere after the edit; the connected run is green and its printed
total matches the number written into `docs/android-testing.md`.

**Commit:** `docs: document the android one-level locations list`

## Task 5 — Full gates

This task makes no tracked-file change and no commit; it is the final stop/go
gate. The only local write is the git-ignored `.ai-state` note at the end.

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

Expected: every command exits 0 with no lint errors and no failed tests. The
AVD `kumiho-api36` must be booted for the connected suite.

Then verify the repository state:

```bash
git status --short
git log --oneline 5acb2a6..HEAD
```

Expected: a clean working tree (`.ai-state` is ignored) and the plan commit
plus the four task commits on top of `5acb2a6`.

Rewrite the git-ignored `.ai-state` as a single line, for example
`implemented android locations one-level list; automated gates green`. Report
the spec and plan paths, both planning commit hashes, the four task commits,
the measured connected-suite count and the seven assumed decisions
(D9, D12, D18, D19, D23, D24, D25).

**Commit:** none (acceptance gate; a failure becomes a new task and commit,
not an amendment of the four task commits).

## Dependencies / ordering

1. **Precondition:** the keep-outside-connections work is implemented and
   committed (four task commits), so `## [0.5.2]` and `D52` exist and its
   files are frozen.
2. Task 1 (pure ordering + latency rules, `LocationTreeTest`).
3. Task 2 (flatten the tree, wire the sort, UI/strings/tags, JVM + Compose +
   screenshot fixtures).
4. Task 3 (remove Test speed, one-row toolbar, notice cleanup).
5. Task 4 (user guide, parity note, testing count, canonical `D53`, shared
   changelog, README).
6. Task 5 (full Go and Android gates).

Task 2 depends on Task 1's rules; Task 3 depends on Task 2's flattened rows
(the surviving ping tests are already pingAll-driven); Task 4 follows Task 3
so the docs describe the final copy and it runs after the tests that change
the connected-suite count; Task 5 follows all four.

## Risks and rollback

- **Rows moving while reading (D10).** Live sorting can move a country while
  the user is looking at it. Bounded by the manual run and the existing run
  window; unmeasured rows keep engine order, and `Recommended` never moves.
- **Sort surprise under search (D12, assumed).** The filtered view sorts by
  the filtered servers; flagged at the spec gate and pinned by the pure
  `searching sorts the filtered view` test.
- **Duplicate latency text on screen.** A country row and its fastest server
  can show the same number; Compose tests must assert through row tags, not
  bare `onNodeWithText`, to avoid ambiguity. Task 2's test updates do this.
- **Stable sort mistakes.** `Map.get` cannot distinguish absent from `null`;
  `countryLatencyOf` uses `containsKey`, and both comparators carry an
  explicit original-index tie-break. Task 1's tests pin ties, timeouts and
  quarantined handling.
- **Test-count drift.** `docs/android-testing.md` pins an exact connected
  count; Task 4 measures it with a real run after all test changes and writes
  the measured number. Do not guess it.
- **Dead strings/tags.** Removed names are deleted in the same task that
  removes the last reference; Task 3's grep guards against stragglers, and
  Android lint would flag unused resources.
- **Coordination.** Do not touch the keep-outside-connections files or reuse
  `D52`; do not duplicate the `[0.5.2]` heading. The edit regions in
  `CHANGELOG.md`, `docs/android.md` and `docs/android-testing.md` are
  disjoint from that work once it is committed.
- **Rollback.** Revert the four task commits in reverse order. Nothing is
  persisted: latency and sort order are session-local view state, no protocol
  or facade change exists, and older engines and apps stay compatible.

## Definition of done checklist

- [ ] The Locations tab shows one expandable level; no city row or city
      toggle exists anywhere on the screen.
- [ ] Country rows show flag, name, `N servers` (quarantined included) and
      trailing best latency + caret.
- [ ] Server rows show hostname and `City · protocols · port`, keep the
      quarantine mark and per-server latency, and are the only selectable
      rows.
- [ ] `Test speed` and the `X visible · Y total` helper are gone; one
      `Ping all` probes every usable, non-quarantined server, deduped by
      `host:port`, ignoring filter and expansion, and reads `Pinging N…`.
- [ ] Nothing pings on load, typing, expanding, refreshing or resuming.
- [ ] The list re-sorts live: measured countries first ascending, then
      unmeasured/timeout countries in engine order, `Recommended` pinned;
      servers inside a country the same way.
- [ ] Country latency is the fastest measured RTT, else `timeout` when only
      timeouts answered, else `—`.
- [ ] The toolbar is one row: Refresh, `Ping all`,
      `Expand all`/`Collapse all`; the toggle acts on countries only and is
      disabled while searching.
- [ ] Search still matches country name/code, city name and server hostname,
      auto-opens matches, and clearing restores the previous expansion.
- [ ] Selection persists the exact `host:port` with country and city codes,
      never connects, and the selection card still names
      `Country · City · host`; the connected hint, quarantine mark and every
      content state still work.
- [ ] No engine, `internal/api`, `internal/ipc`, daemon, mobile-facade, GUI
      or TUI change; protocol v1 is untouched.
- [ ] JVM and Compose tests cover the new rows, the single ping action, the
      sort rule, the latency derivation and the toolbar; screenshot fixtures
      are updated; `make android-test` and `make android-emulator-test` pass.
- [ ] `docs/android.md`, `docs/android-testing.md` (measured count), the
      canonical `D53`, `README.md` and the shared `[0.5.2]` changelog
      describe the shipped behavior.
- [ ] No version bump, tag, push or release.

## Open questions

None. The spec's seven assumed decisions (D9, D12, D18, D19, D23, D24, D25)
remain flagged for review. The implementation clarifications C1–C8 above are
resolved and are the only new details for the plan gate.
