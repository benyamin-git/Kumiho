# Locations Redesign — Implementation Plan

**Spec:** [`docs/superpowers/specs/2026-10-29-locations-redesign-design.md`](../specs/2026-10-29-locations-redesign-design.md)
(approved; commits `716d3b3` and `3352375`)
**Status:** Draft for gate #2
**Date:** 2026-10-29
**Release slice:** v0.8.0 Region and location truth — UI component only
**Scope:** Android phone, Android TV, Windows GUI and TUI; CLI and the daemon
are untouched.

## How to execute this plan

- Read the approved spec first. It is authoritative for behavior; this plan is
  authoritative for sequencing, files, interfaces and verification.
- Load the `design` skill before any UI task and use the house-style checklist.
  Load `test-driven-development` before implementation and
  `verification-before-completion` before every completion claim.
- Each task is one commit. Do not bundle tasks, push, tag, amend published
  commits or open a pull request without an explicit request.
- Every commit leaves the existing Go gate green:

      make lint && make test && go test -race -count=1 ./... && make build && \
        GOOS=windows GOARCH=amd64 go vet ./... && \
        GOOS=windows GOARCH=amd64 go build ./... && \
        GOOS=android GOARCH=arm64 go build ./...

  Android-touching commits also leave `make android-test` green. The two
  Android UI commits additionally run the phone and TV emulator suites.
- The generated AAR, signing material, local SDK paths and `.ai-state` never
  enter a commit.
- Do not bump the version, tag or publish a release in this slice (D35).

## Global constraints

Copied from the approved spec; every task preserves them.

### Behavior

- One flat server-only list on every client. No country/city headers, carets,
  disclosures, expansion state, expand-all/collapse-all action or Toggle all.
- Row line 1 is the full `Country · City` (omit the city when missing or equal
  to the country); row line 2 is `host:port · protocols` (protocols are
  distinct non-empty names in engine order, joined by `, `; omit the separator
  when there are none).
- Recommended is first in country order and starts with a star. Other rows
  start with a real 4:3 flag. Unknown/invalid country codes use a globe.
  Quarantined servers and records without a usable connect target are hidden
  everywhere.
- Default order is Recommended, then country name A–Z, city name A–Z, hostname
  A–Z. Comparisons are case-insensitive and stable.
- Search matches country name/code, city name/code, hostname, `host:port` and
  protocol names, trimmed and case-insensitive. Search does not change the
  sort mode.
- Ping all is the only ping action. It measures every usable loaded server
  regardless of the filter, deduplicated by address, one run at a time, with
  no automatic/background probes. Test speed, `p` and expansion keys/actions
  are removed.
- The order switches to speed order only after a completed run with at least
  one result: Recommended first, finite latency ascending, timeouts/unpinged
  last, ties by country/city/host. A transport error or a run with no results
  leaves the current order unchanged. Refresh and re-entering the screen reset
  to country order; filtering never does.
- The Android/TV saved-location hint is removed completely. The selection
  summary remains and reads `Country · City · host:port · protocols`.
- Selection persistence, `select_location`, engine behavior, IPC v1 and the
  daemon protocol are unchanged.
- The GUI dashboard keeps its native `<select>`. Its option text is text-only
  and one real flag sits beside the control for the current selection (D39).

### Flags, density and accessibility

- Source flags are `flag-icons` 7.6.0 4:3 SVGs (MIT): all ISO 3166-1 alpha-2
  flags, with the source version pinned. GUI serves the SVGs; Android ships
  generated 192×144 lossless WebP files; TUI keeps the existing emoji helper.
- Sizes: 24×18 dp in phone rows, 20×15 in summaries/pickers, 32×24 on TV and
  24×18 in the GUI. A 1 px outline and 2 dp radius keep pale flags visible.
- Flags are decorative (`contentDescription`/`alt` empty or `aria-hidden`).
  Text and row semantics carry the meaning; selection remains announced.
- Phone rows are 56 dp, desktop rows 48 px, TV keeps its ten-foot metrics.
  Touch targets remain at least 48 dp.
- No new runtime SVG decoder, network fetch, palette, font or animation.

## Pre-flight clarifications (resolved; no product-scope change)

These details make the approved decisions executable and are not new product
choices.

### P1 — Display address is the connect target

The row address, the selection summary address and the ping key all use the
same server address the existing clients use for `select_location` and ping:
the `connect` protocol's host/port, with the existing fallback to
`hostname:port` when the record has no protocols. The row still displays
`host:port`; in normal records this is exactly the server hostname and port.
Android gains the equivalent pure helper so its displayed address, selection
and ping key cannot disagree. This is a UI/model correction, not an engine
change.

### P2 — GUI dashboard picker

Keep the native accessible `<select>`. Remove the emoji from option text and
render a separate decorative flag for the currently selected country beside
the select. Dropdown options remain text-only because native options cannot
contain images.

### P3 — GUI latency labels

The GUI bridge already exposes `State.RTTs` as display strings such as
`42 ms`/`timeout`; it does not expose a numeric RTT map. The GUI parses its own
labels (`^\d+ ms$`) for speed ordering. No bridge field or IPC change is added.

### P4 — Speed completion and refresh

A timeout is a result for lifecycle purposes; a run enters speed mode when at
least one requested host answers (a finite RTT or a timeout) and no transport
error was observed. A transport error or no answers leaves the current mode
unchanged. Refresh resets the order and local latency display on all clients;
the GUI's controller clears its own projected `rtts` map when the Locations
refresh passes `locations: true`. This is GUI-local state, not a protocol
change.

### P5 — Android visit reset

`LocationsRoute` calls `beginVisit()` once per screen entry. A `rememberSaveable`
"entered" guard distinguishes a real re-entry from a configuration change, so
rotation preserves the transient speed view while leaving and returning resets
it. The view model also resets on refresh.

### P6 — Assets and token paths

GUI flag URLs are relative (`flags/4x3/<cc>.svg`), not root-absolute, so they
remain under the Windows shell's per-run token path. The GUI CSP stays
`default-src 'self'`; same-origin images are allowed without a new directive.
TUI text emoji is the only emoji that remains.

### P7 — Documentation scope

Historical release notes may keep old wording. Current behavior docs, manuals,
test checklists, screenshots and the AGENTS design record must describe the
new UI. The screenshot harness runs as the target v0.8.0 UI label even though
the application version is not bumped in this slice.

## Task ordering

```
T1 flag source + licenses + generated assets
  -> T2 Android locations model, view model, phone and TV screens
       -> T3 Android compact picker and TV connect-options flags
  -> T4 GUI locations + dashboard flag + GUI tests
  -> T5 TUI flat list + speed order + tests
  -> T6 docs, screenshots and design record
  -> T7 full gates and manual verification (no commit)
```

T4 and T5 are independent after T1 and may be done in either order. T6 needs
all UI tasks. T7 needs T6. If T1's source or rasterizer is unavailable, stop
and report rather than substituting a different flag set or renderer.

## T1 — Flag source, licenses and generated assets

**Depends on:** nothing.
**Commit:** `feat: add bundled country flag assets`

**Files**

- `internal/gui/web/static/flags/4x3/<cc>.svg` — the pinned `flag-icons`
  7.6.0 4:3 source set, one lowercase ISO code per file. Keep only two-letter
  file names; do not copy the `1x1` set or CSS packages.
- `third_party/flag-icons/LICENSE` — upstream MIT text.
- `third_party/flag-icons/README.md` — source URL, version `7.6.0`, npm tarball
  SHA-256, the exact copy command, the ISO-set note and the generated-asset
  derivative note.
- `third_party/flag-icons/SHA256SUMS` — checksums for the committed source
  SVGs (and the license if included in the check).
- `scripts/flags/generate-android-flags.sh` — the deterministic Android
  generator and resource-map writer.
- `android/app/src/main/res/drawable-nodpi/flag_<cc>.webp` — generated
  192×144 lossless WebP files for every two-letter source flag.
- `android/app/src/main/java/dev/benyamin/kumiho/ui/locations/CountryFlagsResources.kt`
  — generated `internal val COUNTRY_FLAG_RESOURCES: Map<String, Int>` with one
  explicit `R.drawable.flag_<cc>` reference per flag.
- `NOTICE` — MIT attribution for `flag-icons`, the 4:3 source SVGs and the
  generated Android WebP derivatives.

**Interfaces**

```bash
# One-time source acquisition (network only during this task):
npm pack flag-icons@7.6.0
tar -xzf flag-icons-7.6.0.tgz
cp package/flags/4x3/*.svg internal/gui/web/static/flags/4x3/
cp package/LICENSE third_party/flag-icons/LICENSE

# Repeatable generation (no network):
scripts/flags/generate-android-flags.sh
# honors FLAG_ICONS_DIR, ANDROID_FLAGS_DIR and COUNTRY_FLAGS_KT
```

The script:

- requires `ffmpeg` with `librsvg` (`ffmpeg -decoders | grep librsvg`);
- discovers sorted `^[a-z]{2}\.svg$` files, fails if any source is unreadable;
- rasterizes each to exactly 192×144 with
  `-vf scale=192:144:flags=lanczos -frames:v 1 -c:v libwebp -lossless 1`;
- writes the Kotlin map sorted by code, with a generated-file header and
  explicit `R.drawable` references (never reflection or dynamic lookup);
- verifies every output with `ffprobe` (192×144, WebP) and exits non-zero on a
  missing or malformed result.

The REC star and unknown globe are not rasterized as country flags: Android
draws a themed Material star or the existing `ic_globe` vector, the GUI draws
inline `currentColor` star/globe SVGs in `flags.js`, and the TUI keeps its text
star/globe fallback.

**Verification**

1. `sha256sum -c third_party/flag-icons/SHA256SUMS` from the repository root
   exits 0.
2. `find internal/gui/web/static/flags/4x3 -name '*.svg' | wc -l` equals the
   two-letter source count from the pinned package (at least the 249 ISO
   codes); `find ... -name '*.webp' | wc -l` matches it.
3. `ffprobe -v error -select_streams v:0 -show_entries stream=width,height,codec_name -of csv=p=0 android/app/src/main/res/drawable-nodpi/flag_us.webp`
   prints `192,144,webp` (or the equivalent ordered CSV) for the sample.
4. Run the generator twice and `git diff --exit-code` must be clean.
5. `go test ./internal/gui/web ./...` and `make android-test` pass; the new
   assets are embedded and the generated Kotlin map compiles.
6. `du -sh internal/gui/web/static/flags android/app/src/main/res/drawable-nodpi`
   is recorded in the task notes. If the Android asset set exceeds the agreed
   review budget, stop and re-plan the resolution; do not silently lower it.

**Stop/go:** if the pinned package cannot be fetched, its checksum does not
match, or `ffmpeg` cannot rasterize SVG, stop. Do not use a different flag
library or a runtime SVG decoder.

## T2 — Android locations model, view model and phone/TV screens

**Depends on:** T1.
**Commit:** `feat: redesign android locations as a flat server list`

**Files**

- `android/app/src/main/java/dev/benyamin/kumiho/ui/locations/LocationTree.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/locations/LocationFilter.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/locations/CountryFlags.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/locations/CountryFlag.kt`
  (new composable)
- `android/app/src/main/java/dev/benyamin/kumiho/ui/locations/LocationsViewModel.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/locations/LocationsScreen.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/locations/TvLocationsScreen.kt`
- `android/app/src/main/res/values/strings.xml`
- `android/app/src/test/java/dev/benyamin/kumiho/ui/locations/LocationTreeTest.kt`
- `android/app/src/test/java/dev/benyamin/kumiho/ui/locations/LocationFilterTest.kt`
- `android/app/src/test/java/dev/benyamin/kumiho/ui/locations/CountryFlagsTest.kt`
- `android/app/src/test/java/dev/benyamin/kumiho/ui/locations/LocationsViewModelTest.kt`
- `android/app/src/androidTest/java/dev/benyamin/kumiho/ui/locations/LocationsScreenTest.kt`
- `android/app/src/androidTest/java/dev/benyamin/kumiho/ui/locations/LocationsScreenshotsTest.kt`
- `android/app/src/androidTest/java/dev/benyamin/kumiho/tv/TvLocationsScreenTest.kt`
  (new) and `TvScreenshotsTest.kt`

**Model interface**

```kotlin
enum class LocationSortMode { COUNTRY, SPEED }

data class LocationRow(
    val country: Country,
    val city: City,
    val server: Server,
    val address: String,
    val protocols: List<String>,
    val recommended: Boolean,
    val key: String,
) {
    val countryCode: String get() = country.code
    val cityCode: String get() = city.code
}

object LocationTree {
    fun rows(
        countries: List<Country>,
        query: String = "",
        sortMode: LocationSortMode = LocationSortMode.COUNTRY,
        latency: Map<String, Double?> = emptyMap(),
    ): List<LocationRow>

    fun pingAllTargets(countries: List<Country>): List<String>
    fun connectTarget(server: Server): String? // P1 semantics
    fun protocolNames(server: Server): List<String> // trimmed, distinct, order kept
    fun currentSelection(
        countries: List<Country>,
        location: String?,
        server: String?,
    ): LocationSelection?
    fun latencyOf(address: String, latency: Map<String, Double?>): LatencyState
}

object LocationFilter {
    fun matches(row: LocationRow, query: String): Boolean
}
```

Rules pinned by the pure tests:

- `rows` flattens every city/server, excludes quarantine and no-connect-target
  records, applies the filter, then sorts. No parent/level/expanded fields.
- Country mode compares `recommended` first, then `country.name`, `city.name`,
  `server.hostname`, then the address; all names use `Locale.ROOT` lowercase
  and comparisons are stable.
- Speed mode compares `recommended` first, finite RTT ascending, then
  non-finite (timeout/unpinged) last, then the country-mode tie-breakers.
- `addressKey` is trimmed lowercase; `connectTarget` follows P1; protocol names
  are trimmed, non-empty and distinct in engine order.
- The filter haystack contains country name/code, city name/code, hostname,
  address (`host:port`) and every protocol name. No protocol port is matched.
- `pingAllTargets` walks the loaded tree independently of the query and
  expansion, skips unusable records and deduplicates by lowercased address,
  keeping the first spelling.
- `currentSelection` resolves the reported country/server against the loaded
  tree by connect target; a hidden or unresolved server remains the honest raw
  address. `LocationSelection` gains `serverAddress: String?` and
  `protocols: List<String>`; `marks` uses the row address.
- `LatencyState` stays `Untested` / `Timeout` / `Measured`; country latency and
  `LocationRow.CountryRow` are removed.
- `CountryFlags.flagResource(code): Int?` normalizes trim/uppercase, returns
  `null` for REC/blank/malformed/unknown, and otherwise the generated map
  lookup. `FlagIcon(code, modifier)` draws the flag image, a themed star for
  REC, or the existing globe for unknown; it is decorative.

**State/view-model interface**

`LocationsState` removes `expandedCountries`, `anyExpanded`, `canToggleAll`
and `countryLatency`; it adds:

```kotlin
val sortMode: LocationSortMode = LocationSortMode.COUNTRY
val listResetToken: Int = 0
```

The `rows` getter passes `query`, `sortMode` and `latency`. `showEmpty` and
`showNoMatch` use the visible row set (`showEmpty` only when not searching;
`showNoMatch` only when searching). `canPingAll` uses `allTargets`.
`sessionActive` is removed from the UI state: the saved-location hint is gone,
while `select_location` behavior is unchanged.

`LocationsViewModel` gains:

```kotlin
fun beginVisit()          // reset order on real re-entry, preserve on rotation guard
```

Removed methods: `toggle`, `toggleCountry`, `toggleAll`.

Ping lifecycle:

- `pingAll()` starts the all-target run and clears only a stale retry notice.
- `startPing` records `runAnswered = false` and keeps the existing 12 s floor /
  24-host window.
- `onPing` sets `runAnswered = true` for any result (RTT or timeout) and stores
  the RTT.
- Normal completion with `runAnswered` sets `sortMode = SPEED`, increments
  `listResetToken`, and leaves the current selection alone.
- A command failure clears the run, keeps the current mode, clears any pending
  timers and shows `PING_ALL_FAILED`; an empty/no-answer completion changes no
  mode.
- `refresh()` and the initial load reset `sortMode = COUNTRY`, clear local
  latency and increment `listResetToken`; retry/failure behavior stays as it is.
- `beginVisit` resets order and the list token; `LocationsRoute` uses a
  `rememberSaveable` entered guard so rotation does not count as a new visit
  (P5).

**Screen interface and layout**

- `LocationsActions` removes `onToggleCountry` and `onToggleAll`; the remaining
  fields are query, clear, select, refresh, retry, ping all, dismiss and retry
  notice.
- Phone `LocationsScreen`: no connected hint, no `InlineBanner`, no country
  rows, no carets. One `LazyColumn` with a `LazyListState`; a
  `LaunchedEffect(listResetToken)` scrolls to index 0 when the token changes.
  Rows use `Dimens.RowHeight` (56 dp), a 24×18 flag, two text lines, trailing
  latency/check/saving spinner, and the existing selection semantics.
- TV `TvLocationsScreen`: same flat rows in the existing focusable surface,
  full-width without indentation, 32×24 flags, 20×15 summary flag, no hint, no
  expand button. The first row keeps the existing initial focus behavior.
- Summary: a real `FlagIcon` (20×15) beside a plain `selectionText` that now
  joins country, city, `host:port` and protocol names with ` · `. REC uses the
  localized “Recommended”; city omitted when absent/equal; protocols omitted
  when empty. The flag is not part of the accessibility text.
- Toolbar phone: Refresh icon plus Ping all; the empty slot left by the
  expansion action is not replaced with anything. TV: Refresh plus Ping all.
- Strings: remove `locations_connected_hint`, `locations_expand_all`,
  `locations_collapse_all`, `locations_expand`, `locations_collapse`,
  `locations_quarantined`, the country-count plural and other tree-only copy
  once no references remain. Update `locations_no_match_message` to name
  country, city, server and protocol. Keep loading/error/empty/retry/summary/
  latency copy.
- Tags: remove `TOGGLE_ALL` and `CONNECTED_HINT`; keep `LIST`, `SUMMARY`,
  `PING_ALL`, `REFRESH`; add summary/server flag tags if needed for
  instrumentation tests.

**Tests**

Pure JVM tests must cover at least:

- `LocationTreeTest`: flat server rows; country/city/host ordering; REC pinned
  first; speed ordering with finite RTT, timeout, untested and ties; filter by
  country name/code, city name/code, hostname, `host:port`, protocol name;
  quarantine and targetless hiding; deduplicated all-targets independent of
  filter/sort; connect-target address; selection resolution including raw
  addresses; protocol list order/distinctness; no mutation.
- `LocationFilterTest`: blank query, each haystack field, trim/case folding,
  protocol/port matches, no-match, no mutation.
- `CountryFlagsTest`: two-letter normalization, uppercase/lowercase/spaces,
  known resource, REC/blank/malformed/unknown nulls.
- `LocationsViewModelTest`: no ping on load; Ping all sends every usable target
  while filtered; one run at a time; no live reorder during a run; speed order
  after completion; timeout-only run behavior; transport failure/no-result
  keeps the prior mode and notice; refresh resets order/latency; filter does
  not change mode; `beginVisit` reset; selection/save behavior remains.
- `LocationsScreenTest` (rewrite tree/expansion tests): flat list has no
  country header/caret/toggle; row text/detail/latency; REC star; real flags
  are decorative; summary text and flag; no connected hint; search; hidden
  unusable rows; toolbar only refresh/ping; 56 dp/48 dp targets; loading,
  error, empty and no-match states.
- New `TvLocationsScreenTest`: flat rows, focusable selection, TV flag sizes,
  summary, no hint/no toggle, search/filter and no-match.
- Update `LocationsScreenshotsTest` and `TvScreenshotsTest` fixtures to remove
  expansion fields and render flat lists in all three themes.

**Verification**

1. `make android-test` (JVM tests, lint, debug APK; androidTest compiles).
2. Boot the existing `kumiho-api36` AVD and run `make android-emulator-test`.
3. Boot `kumiho-tv-api36` and run `make android-tv-emulator-test`.
4. Inspect the phone and TV screenshot outputs (light/dark/OLED) for flags,
   two-line rows, summary and no hint. Pull them with
   `adb pull /sdcard/Download/...` or the existing test output path.
5. Run the full Go gate above.

## T3 — Android compact picker and TV connect-options flags

**Depends on:** T2.
**Commit:** `feat: show real flags in android location pickers`

**Files**

- `android/app/src/main/java/dev/benyamin/kumiho/ui/components/Rows.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/dashboard/CompactLocationPicker.kt`
- `android/app/src/main/java/dev/benyamin/kumiho/ui/tv/dashboard/TvConnectOptionsPage.kt`
- `android/app/src/androidTest/java/dev/benyamin/kumiho/ui/dashboard/DashboardScreenTest.kt`
- `android/app/src/androidTest/java/dev/benyamin/kumiho/tv/TvDashboardTest.kt`
  (or a focused picker test if that file grows too broad)
- `android/app/src/androidTest/java/dev/benyamin/kumiho/ui/dashboard/DashboardScreenshotsTest.kt`
- `android/app/src/androidTest/java/dev/benyamin/kumiho/tv/TvScreenshotsTest.kt`

**Interface**

Replace `KumihoRow.titlePrefix: String?` with:

```kotlin
leading: (@Composable () -> Unit)? = null
```

Render `leading` before `icon`/the title column. The only current
`titlePrefix` caller is the compact picker; remove `TitleLine` and the prefix
path so no emoji helper remains in `KumihoRow`.

`CompactLocationPicker` passes a 20×15 `FlagIcon(country.code)` as the leading
slot. `TvConnectOptionsPage` replaces its emoji `Text` with a 32×24
`FlagIcon`. City rows, Saved and Recommended rows stay text/check-only.

**Tests/verification**

- Compose tests assert a country picker row exposes a flag node (tagged,
  decorative) and that the real flag replaces emoji text; the row remains
  clickable and the selected mark is unchanged.
- TV dashboard test covers the connect-options flag and focus order.
- Screenshot fixtures update the dashboard connect-options captures.
- `make android-test`, `make android-emulator-test`,
  `make android-tv-emulator-test`, then the full Go gate.

## T4 — GUI flag surfaces, locations list and dashboard picker

**Depends on:** T1.
**Commit:** `feat: redesign gui locations and flag surfaces`

**Files**

- `internal/gui/web/static/js/flags.js`
- `internal/gui/web/static/js/screens/dashboard.js`
- `internal/gui/web/static/js/screens/locations.js`
- `internal/gui/web/static/css/screens/dashboard.css`
- `internal/gui/web/static/css/screens/locations.css`
- `internal/gui/controller.go` and `internal/gui/controller_test.go` (clear the
  GUI-local RTT projection on a Locations refresh, P4)
- `internal/gui/web/server_test.go` (embedded flag route/type test)

**`flags.js` interface**

```js
// Relative to the page, so the Windows shell's token path is preserved.
export function flagAsset(code) -> string | null;
// Decorative node: <img> for a country, inline currentColor SVG for
// Recommended (star) or unknown (globe). The caller adds position classes.
export function flagIcon(code) -> HTMLElement;
```

`flagAsset` trims/uppercases, accepts exactly two ASCII letters, rejects REC
and returns `"flags/4x3/" + cc.toLowerCase() + ".svg"`. Remove `flagFor` once
both screens use the new helpers. The inline star/globe are project assets,
not upstream flag-icons.

**GUI locations behavior**

`locations.js` becomes a flat listbox:

- Build a pure row model from `state.locations`: country/city/server, connect
  target address, protocols, REC flag, primary `Country · City`, detail
  `address · protocols`, search haystack. Rows without a server/connect target
  or with `quarantined` are not built.
- `role="listbox"` on the container; `role="option"` rows; `aria-selected` and
  `aria-activedescendant` retain the current selection/keyboard behavior.
  There are no levels, carets, expansion attributes or tree semantics.
- Row DOM: decorative flag, primary and detail text in a two-line block,
  trailing latency and selected check. No `· selected` text is needed when the
  check is present.
- Search box placeholder `Country, city, server, or protocol`; matching uses
  the haystack above. The no-match copy remains `No locations match the
  filter.` in the placeholder and the no-period status line.
- Actions are Refresh and Ping all only. Remove Test speed, Expand all,
  Collapse all and their `MAX_PING_HOSTS`/expansion code.
- Per-mount state: `sortMode = "country"`, `current`, `pingBusy`, `pingHosts`,
  `pingBaseline`, `pingAnswered`, `pingTimer`. No auto-reveal of the persisted
  selection; the list opens at the top.
- `allTargets()` walks the whole loaded list, ignoring the filter, skipping
  unusable records and deduplicating by lowercased address. Ping all sends all
  targets in one run; no automatic ping.
- `rttValue(label)` parses `^\d+ ms$`; `timeout`/unparsed/untested are
  positive infinity for sorting. `compareRows` implements country and speed
  order with the same case-insensitive tie-breakers as Android/TUI.
- On a completed run with at least one answered host and no transport error,
  set `sortMode = "speed"`, reset the list scroll to top, and keep the filter.
  On a transport error or no answers, leave the current mode. On data signature
  change (refresh/first load), reset country order and scroll top. On filter
  changes, keep the mode. `finishPing` must rebuild the row order after the
  sort switch, not just relabel the old order.
- Refresh sends the existing `refresh` action with `{locations:true}`; the
  controller clears `rtts` before reloading so the new visit starts with no
  stale labels. A failed refresh keeps the old rows and shows the existing
  notice.

**GUI dashboard behavior**

- Keep the native `<select>` and its `Location` label. `rebuildLocations`
  builds option text `Country Name (CC)` with no emoji.
- Add a decorative `<span class="dash__flag">` beside the select; render the
  current selected country's `flagIcon` in `renderOptions` and after change.
  Hide it for `Saved selection` or an unknown code; use the globe fallback.
- CSS: a flex row around the select/flag, 24×18 flag, 1 px outline, 2 px
  radius, no layout jump when the flag hides. Keep the select's native
  accessibility and keyboard behavior.

**CSS**

`locations.css` removes caret/level/indent/36 px rules; adds a 48 px row,
24×18 flag, two-line text block, 48 px target, selected/current/focus
treatments derived from the existing roles. Keep the card, filter, empty and
status primitives. No literal colors or shadows.

**Tests/verification**

1. `node --input-type=module --check < internal/gui/web/static/js/flags.js`
   and the same for `dashboard.js`/`locations.js`.
2. `go test ./internal/gui/...` including the refresh-clears-RTTs test and the
   embedded SVG route test (200, `image/svg+xml`, non-empty body, CSP intact).
3. `scripts/dev-gui-web.sh start`; open
   `http://127.0.0.1:7407/#/locations` and verify: flat rows, real flags, REC
   star, two-line details, search fields/protocol, no carets, only Refresh/Ping
   all; Ping all -> speed order at top; filter then clear keeps order; refresh
   resets order and labels; failed refresh notice.
4. Open the dashboard Options popover: native select options are text-only;
   the real current-country flag sits beside it; changing selection updates
   the flag.
5. Capture `node scripts/visual/capture.mjs --only dashboard` and
   `--only locations`; inspect the generated dark screenshots for light/dark/
   OLED legibility and no overflow.
6. Run the full Go gate.

## T5 — TUI flat list and speed order

**Depends on:** T1.
**Commit:** `feat: flatten the tui locations list`

**Files**

- `internal/tui/locations.go`
- `internal/tui/tui.go` (the `o` entry reset)
- `internal/tui/help.go`
- `internal/tui/tui_test.go`
- `internal/tui/capture_test.go`

**Model/interface**

`locationsState` removes `expandedCountries`, `expandedCities`; adds
`sortSpeed bool`. `pingRun` removes `all`, adds `answered bool`:

```go
type pingRun struct {
    total    int
    pending  map[string]bool
    answered bool
}
```

`locRow` becomes a flat server row carrying country code/name, city name,
host, port, address and recommended. `locationRows()`:

- iterates every city/server, uses `ConnectTarget`, skips quarantine/failure
  and targetless records;
- filters on country name/code, city name/code, hostname, address and protocol
  names (trimmed/lowercase substring);
- sorts country mode by REC, country, city, host; speed mode by REC, finite
  RTT, then country/city/host, with timeouts/unpinged last;
- no expansion or auto-expand.

`allLocationHosts()` derives from the unfiltered flat set and deduplicates by
lowercased address.

**Keys/rendering**

- `P` is the only ping key; remove `p` and its visible-row target path.
- Remove `x`; remove `enter` expansion branches. `enter` on a server persists
  the same tuple as before; arrow/j/k movement remains.
- `r` clears `sortSpeed`, `rtts`, `cursor` and reloads; `o` resets
  `sortSpeed` and `cursor` when entering the screen; leaving the screen does
  not need to preserve speed order.
- On a normal run completion with `answered`, set `sortSpeed = true` and
  `cursor = 0`. A `pingFailedMsg` clears the run without changing order.
- Row: cursor, selection mark, emoji flag (normal country), `★ REC` for
  Recommended, ` CC · City · host:port`, then latency/timeout. No protocols.
  Normal rows use the code (`US`), not the full country name. Omit the city
  when empty/equal to the country. Unknown codes omit the flag.
- Footer: `enter select • P ping all • / filter • r refresh • esc back • ? help
  • q quit`. Help overlay: `locations arrows/hjkl move • enter select • P ping
  all • / filter • r refresh • esc back`.
- Empty loaded + active filter renders a no-match line; no filter renders
  `no locations loaded`; loading/error behavior stays.

**Tests/verification**

- Rewrite `TestLocationsView` for flat rows and one-line text; remove
  `TestLocationsToggleAll`, `TestLocationsToggleAllIgnoredWhileFiltering` and
  the visible-only ping tests.
- Add tests for flat rows/hidden quarantine/targetless, protocol/port search,
  country and speed order, no live reorder, timeout last, refresh reset,
  leaving/re-entering reset, `P`-only, `p`/`x` no-ops, footer/help text.
- `go test ./internal/tui -count=1`.
- Capture the TUI locations view:
  `KUMIHO_TUI_CAPTURE_DIR=/tmp/opencode/kumiho-tui-capture go test ./internal/tui -run TestCaptureViews -count=1`
  then `node scripts/screenshots/render-tui.mjs ...` and inspect.
- Run the full Go gate.

## T6 — Documentation, design record and screenshots

**Depends on:** T2–T5.
**Commit:** `docs: update locations documentation and screenshots`

**Code/screenshot harness files**

- `scripts/visual/capture.mjs` — rename the locations `speed-test` state to
  `speed-order` (or equivalent) and update its comment; the action still pings
  the two mock hosts, and the post-run list is speed-ordered. No auto-reveal
  assumption remains.
- `scripts/screenshots/capture.sh` — copy
  `locations/dark-speed-order.png` to `screenshots/windows-gui/locations.png`;
  set the default capture label to the v0.8.0 UI target (P7) without changing
  the app version.
- `scripts/screenshots/README.md` — update the source table, prerequisites
  (GUI real SVGs, TUI emoji font), and the state name.
- `scripts/visual/README.md` if the state name is documented.
- `internal/tui/capture_test.go` — flat sample data, no expansion fields,
  cursor at the top, a useful latency fixture; regenerate
  `screenshots/linux-tui/locations.png`.
- `screenshots/windows-gui/locations.png`,
  `screenshots/linux-tui/locations.png`, and any dashboard screenshot that
  changes because of the new flag; regenerate all six via
  `scripts/screenshots/capture.sh` so the set is consistent.

**Documentation files**

- `README.md` — feature list, GUI/TUI screenshot alt text and current Locations
  description; keep historical release notes intact.
- `AGENTS.md` — Design section: real 4:3 flags on graphical surfaces, TUI
  emoji exception, D39 GUI select rule. Update the architecture map if it
  names the old emoji helper.
- `docs/android.md` — phone Locations section, summary/detail, search, Ping
  all, hidden unusable servers, no hint, and the divergence table.
- `docs/android-tv.md` — TV Locations checklist and screenshots.
- `docs/android-testing.md` — current test counts, connected-suite Locations
  coverage, screenshot capture notes and manual checklist.
- `docs/gui.md` — dashboard picker and Locations section.
- `docs/gui-feature-inventory.md` — replace the old tree/Test-speed/Expand
  inventory with the flat list and native-select flag rule (keep historical
  version notes clearly historical).
- `docs/cli-tui-abilities.md` — Locations screen, keys, help overlay and
  footer.
- `docs/usage.md` — TUI key list.
- `docs/windows-testing.md` — GUI and TUI Locations checklists, picker flag,
  Ping all/speed order, removal of Test speed/expansion.
- `NOTICE` — verify the T1 `flag-icons` attribution remains accurate; no
  duplicate entry.

**Consistency checks**

```bash
rg -n 'Test speed|Expand all|Collapse all|country[/ -]city[/ -]server tree|connected-location|locations_connected_hint|dark-speed-test' \
  README.md AGENTS.md docs scripts internal android
git diff --check
```

Allow historical release-note hits and the removed-code test names; every
current-behavior hit must be updated. Run the screenshot script and inspect all
six committed images at 1280×800/100×28 for flags, two-line rows, no carets,
summary and speed order. Then run the full gates.

## T7 — Full gates and manual verification

**Depends on:** T6. **No commit.**

### Automated gates

```bash
make lint
make test
go test -race -count=1 ./...
make build
GOOS=windows GOARCH=amd64 go vet ./...
GOOS=windows GOARCH=amd64 go build ./...
GOOS=android GOARCH=arm64 go build ./...
make android-test
```

With `kumiho-api36` booted:

```bash
make android-emulator-test
```

With `kumiho-tv-api36` booted:

```bash
make android-tv-emulator-test
```

Also run:

```bash
go test ./internal/gui/... ./internal/tui -count=1
sha256sum -c third_party/flag-icons/SHA256SUMS
node --input-type=module --check < internal/gui/web/static/js/flags.js
node --input-type=module --check < internal/gui/web/static/js/screens/dashboard.js
node --input-type=module --check < internal/gui/web/static/js/screens/locations.js
```

### Manual verification matrix

| Surface | Required evidence |
| --- | --- |
| Android phone emulator | Flat list, real flags at all three themes, REC star, two-line row, summary detail, search fields/protocol, Ping all speed order, refresh reset, no hint/toggle, 56 dp target |
| Android TV emulator | Flat focusable rows, 32×24 flags, summary flag, D-pad focus, no hint/toggle, Ping all speed order, search/no-match |
| Windows GUI web harness | Native dashboard select with text-only options and real current flag; flat list, real SVG flags, search, Ping all/speed order, refresh reset; all themes/accent sweep |
| TUI capture | One-line `🇺🇸 US · New York · host:port`, `★ REC`, no protocols, no `p`/`x`, `P` Ping all, speed order, footer/help |

No physical-device gate is required (D31). Do not claim completion without the
actual command output and screenshot paths; record the AVD names, test counts,
screenshot locations and any warnings in the final task notes.

## Risks and rollback

- **Asset weight.** 249+ 192×144 lossless WebP files add APK/repository size.
  The generator verifies dimensions and fails on missing sources; record the
  size. If the budget is unacceptable, re-plan the resolution (or use
  density-specific assets) rather than lowering silently.
- **SVG fidelity.** `flag-icons` uses strokes/markers for some flags; verify
  the US and other complex flags in the GUI and Android. A broken asset uses
  the globe fallback, never a wrong flag.
- **Cross-client drift.** Android, GUI and TUI each implement the comparator.
  Use the same fixtures for REC, equal RTTs, timeouts, case/accent differences,
  missing cities and protocols; verify each client's order separately.
- **GUI token paths.** Root-absolute flag URLs would bypass the Windows
  shell's token segment. Keep relative URLs and assert the embedded SVG route
  in `server_test.go`.
- **R8/resource shrinking.** The generated Kotlin map contains explicit
  `R.drawable` references; run a minified/release compile check if the existing
  Android release path is available, and keep the map generated rather than
  reflective.
- **Layout overflow.** Long hostnames/protocols can overflow phones and TV
  overscan; ellipsize the detail (and primary only when necessary), keep
  latency/check trailing, and inspect all themes at the specified densities.
- **Stale controls/docs.** Remove Test speed, expansion and their help/tests in
  the same task as the UI; the screenshot harness must use the new state name
  so an old capture cannot pass.
- **GUI refresh race.** Clear `Controller.rtts` under the controller mutex
  before the locations reload; do not clear it on a non-locations dashboard
  refresh.

Rollback is a pure UI revert: restore the previous `LocationTree`/view-model/
screen branches, `locations.js`, `dashboard.js`, TUI model, docs and
screenshots; remove the generated flag assets and the added license/NOTICE
entry. There is no protocol, account, settings, migration or engine state to
reverse. The GUI controller RTT clear is local UI state and reverts with the
GUI commit.

## Open questions

None. All decisions in the approved spec are implemented or covered by the
pre-flight clarifications above; no task has a TBD.

## Self-review against the spec

- D3/D4/D5/D15/D16/D17/D18/D20/D24: flat model, full display strings, search
  haystack, Ping all only, country order and hidden unusable rows (T2/T4/T5).
- D6: TUI one-line text with emoji and `★ REC` (T5).
- D7/D8/D9/D10/D11/D12/D33/D38: real flags on graphical surfaces, source
  pinning, star/globe, sizes, generated Android assets, GUI helpers, TUI emoji,
  decorative accessibility (T1–T4).
- D13/D14/D27: hint removed; summary retained with flag/address/protocols;
  hidden selection remains summarized (T2/T3).
- D19/D21/D22/D23/D36: deferred speed switch, failure/no-result behavior,
  visit/refresh reset, scroll top, filter does not reset order (T2/T4/T5).
- D25/D28/D29/D30/D31/D34/D35/D37/D39: density, preserved states, TUI keys,
  docs/screenshots, emulator verification, no background safety-rule change,
  no version bump, protocol rendering, native GUI picker with leading flag
  (T2–T7).
- No task changes the engine, `internal/ipc`, `internal/daemon`, `internal/api`,
  `mobile/`, the CLI or the Android gomobile facade.

**Approval requested:** review this plan and its pre-flight clarifications
(P1–P7). On approval I will implement T1 first; the plan will not be committed
until the gate is approved.
