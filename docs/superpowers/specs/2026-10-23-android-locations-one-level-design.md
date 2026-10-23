# Android Locations One-Level Tree — Design Spec

**Status:** Approved (spec gate #1, 2026-10-23)
**Date:** 2026-10-23

## Objective / why

The Android Locations tab is the one screen where users choose a route, but its
country → city → server tree buries every server under two disclosures and its
two speed actions (`Test speed`, `Ping all`) make users think about *how* to
measure instead of *where* to connect. Other VPN clients present one expandable
country list with live latency order and a single way to measure everything.

This change removes the city level (country → server only, with the city kept
as row context), collapses both speed actions into one manual `Ping all`, shows
each country's best latency on its collapsed row, sorts the list live by
measured speed, and reduces the toolbar to one row. The result is a shorter,
speed-ordered list that needs no knowledge of Kumiho's internal tree.

It is the second change in the shared **0.5.2** scope, alongside the Android
keep-outside-connections fix planned separately. Android-only: the Windows GUI,
the Linux TUI, the engine, the control protocol and the mobile facade are
untouched. The dashboard connect-options sheet keeps its existing
Recommended/Saved + country → city picker.

## Success criteria / definition of done

1. The Locations tab shows one expandable level: a country row expands
   directly to its servers; no city row or city toggle exists anywhere on the
   screen.
2. Country rows show the flag, the country name, an `N servers` subtitle
   (counting every listed server, quarantined included) and, trailing, the
   best latency and the expand/collapse caret.
3. Server rows show the hostname as the title and
   `City · protocols · port` as the subtitle, keep the quarantine mark and the
   per-server latency, and are the only selectable rows.
4. `Test speed` is gone. One manual `Ping all` action probes every usable,
   non-quarantined, connectable server in the loaded list (deduped by
   `host:port`, ignoring filter and expansion) and reads `Pinging N…` while it
   runs. Nothing pings on its own.
5. Country and server order follows measured speed live as results stream in:
   countries with a measured best RTT first, ascending; the rest (untested or
   timed out) after them in the engine's original order; servers inside a
   country the same way; `Recommended` stays pinned first.
6. A country's shown latency is the fastest measured RTT among its servers;
   with no measurement but at least one timeout it reads `timeout`, otherwise
   `—`.
7. The toolbar is one row: Refresh, `Ping all`, `Expand all`/`Collapse all`.
   The `X visible · Y total` helper is gone; the toggle still opens/closes all
   countries and is disabled while a search is active.
8. Search still matches country name/code, city name and server hostname, and
   matching branches auto-open; clearing restores the previous expansion. City
   codes remain unmatched.
9. Selecting a server still persists exactly that `host:port` (with its
   country and city codes) through `select_location` and never connects; the
   selection card still names `Country · City · host`; the connected hint, the
   quarantine mark and every content state (loading, error, empty, no-match,
   refresh failure + retry, ping failure + retry) keep working.
10. No engine, `internal/api`, `internal/ipc`, daemon, mobile-facade, GUI or
    TUI change; control protocol v1 stays byte-compatible.
11. JVM view-model tests and Compose screen tests cover the new rows, the
    single ping action, the sort rule, the latency derivation and the toolbar;
    the screenshot test fixtures are updated; `make android-test` and
    `make android-emulator-test` pass.
12. `docs/android.md` (Locations and parity notes), `CHANGELOG.md` (appended
    to the shared `[0.5.2]` section), the canonical Android design log
    (`D53`), the README Android paragraph and `docs/android-testing.md`'s
    connected-suite description/count describe the shipped behavior. No version
    bump, tag, push or release.

## Scope

### In scope

- The Android Locations tab only: row model, expansion state, filter wiring,
  latency derivation, sorting, toolbar, strings and tags.
- One speed action (`Ping all`) with live speed sorting.
- JVM, Compose and screenshot-fixture test updates.
- Documentation listed in criterion 12.

### Non-goals (explicitly out)

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

## Decision log

| # | Decision | Rationale | Status | Source |
| --- | --- | --- | --- | --- |
| D1 | Android only; GUI, TUI and daemon untouched | The confusion and the fix are specific to the Android tab; desktop trees were not reported as confusing | decided | user |
| D2 | Scope is the Locations tab only; the dashboard connect-options sheet keeps its Recommended/Saved + country → city picker | The sheet is a different flow (per-connect choice); changing both would widen the risk | decided | user |
| D3 | Remove the city level: country → server; a country tap toggles its expansion | "Keep tree, remove one level" — the requested model; predictable disclosure | decided | user |
| D4 | City names survive as the server-row subtitle: `City · protocols · port` | Keeps place context without a selectable level | decided | user |
| D5 | Server rows remain the selection: a tap persists that exact `host:port` (country and city codes travel internally as today) | Preserves the existing selection and quarantine semantics; no engine change | decided | user |
| D6 | One manual `Ping all` replaces `Test speed` + `Ping all`; nothing pings on open | Simplifies the screen; keeps v0.5.0 D24's no-background-probes rule | decided | user |
| D7 | `Ping all` keeps probing every usable, non-quarantined, connectable server (deduped by `host:port`, ignoring filter/expansion) | Existing `allTargets` semantics; the one action must cover the whole list | decided | user |
| D8 | Collapsed country rows show their best latency; the `N servers` subtitle counts every listed server, quarantined included | The sort order must be explainable; the count matches what expanding reveals | decided | user |
| D9 | Country latency = fastest measured RTT of its servers; no measurement but ≥1 timeout → `timeout`; otherwise `—` | Fastest is the honest "best server" number; timeout must not read as untested | assumed | recommendation |
| D10 | The list re-sorts live as results stream in | The user chose live feedback over end-of-run stability | decided | user |
| D11 | Measured countries first (ascending best RTT); untested/timeout countries after, in engine order; servers inside a country the same way; `Recommended` pinned first | Speed order must be stable and never hide unmeasured entries | decided | user |
| D12 | Sorting operates on the filtered view when a search is active (a city search orders countries by that city's servers) | The order should explain the rows actually shown | assumed | recommendation |
| D13 | `Expand all`/`Collapse all` stays, now over countries only, disabled while searching | One level still benefits from a bulk toggle; filter-forced expansion stays untouched | decided | user |
| D14 | Search keeps matching country name/code, city name and server hostname; matching branches auto-open; clearing restores expansion; city codes stay unmatched | Existing filter contract minus the city row | decided | user |
| D15 | Remove the `X visible · Y total` helper line | With one action and one level it no longer explains anything; the button carries the run count | decided | user |
| D16 | Toolbar is one row: Refresh, `Ping all`, `Expand all`/`Collapse all` | Test speed and the helper are gone; two rows would waste space | decided | user |
| D17 | The selection card keeps `Country · City · host`; the connected hint and the never-connect rule are unchanged | City data still exists; the card is the one place the full context belongs | decided | user |
| D18 | No protocol change: `select_location` still receives country, city and server; the app derives the city from the tapped server | The engine requires `country_code` + `server`; the city was already optional and the data is present | assumed | recommendation |
| D19 | Latency and sort order stay session-local (per view model); nothing is persisted | Matches today's behavior; no new settings or state keys | assumed | recommendation |
| D20 | Verification is automated: JVM view-model tests, Compose screen tests, screenshot fixtures, `make android-test` and `make android-emulator-test`; no physical checklist item | UI-only change with no VPN behavior; the user chose automated-only | decided | user |
| D21 | Docs: `docs/android.md` (Locations + parity notes), `CHANGELOG.md` under the shared `[0.5.2]`, canonical design log `D53`, README Android paragraph, `docs/android-testing.md` suite count/description | The chosen doc set plus the two files this change makes stale | decided | user |
| D22 | No version bump/tag/push now; this ships as the second 0.5.2 change sharing the changelog section the keep-outside-connections plan opens | One 0.5.2 release with both changes | decided | user |
| D23 | Internal naming: `LocationRow.CityRow` and the city tags are removed; `LocationsTags.TEST_SPEED`, `TARGETS_HELPER`, `CITY_PREFIX`/`cityRowTag` are removed; the `locations_city_servers` plural becomes `locations_country_servers`; `locations_test_speed`, `locations_testing`, `locations_targets_helper`, `locations_message_test_failed`, `LocationsMessage.TEST_FAILED`, `LocationsNotice.testFailed` and `LocationsNoticeAction.RETRY_TEST` are removed | One vocabulary: countries, servers, `Ping all`; no dead strings or tags | assumed | recommendation |
| D24 | Spec/plan file names: `2026-10-23-android-locations-one-level-design.md` and `2026-10-23-android-locations-one-level.md` | Matches the repo's dated superpowers naming | assumed | recommendation |
| D25 | The Android screenshot test fixtures are updated in code; no committed image regenerates because Android captures live on the device under `files/locations-screenshots` | Nothing Android-related is committed under `screenshots/` today | assumed | recommendation |

## Architecture

### Row model (`LocationTree.kt`)

- `LocationRow` loses `CityRow`; `CountryRow` and `ServerRow` remain.
  `ServerRow` keeps its `country` + `city` + `server` fields (city drives the
  subtitle and the selection request) and its stable key stays
  `server:<country>/<city>/<address>` so the lazy list keeps row identity when
  sorting reorders it.
- `rows(...)` becomes filter → sort → flatten: `LocationFilter.apply` first
  (unchanged rules), then the country order (D11/D12), then per-country server
  order, then emission of a country row plus, when expanded, its server rows.
  Searching still forces every surviving branch open.
- New pure helpers: `countryLatencyOf(latency, country): LatencyState` (D9) and
  the numeric sort keys for countries and servers. Timeouts (`null` values)
  and absent keys never sort above a measurement, and equal keys keep input
  order (stable sort, explicit original index tie-break).
- `pingTargets` (visible-only) is deleted with Test speed; `pingAllTargets`,
  `addressOf`, `latencyOf`, `currentSelection` and the filter are unchanged.

### View model (`LocationsViewModel.kt`)

- State drops `testing`, `expandedCities` and the Test-speed fields; keeps
  `pingingAll`, `pingAllCount` and the run window.
- Actions drop `testSpeed()` and `toggleCity()`; `pingAll()` stays the only
  ping path. `canPingAll` keeps its rule (loaded tree, usable targets, no run
  in flight).
- The ping failure notice (`PING_ALL_FAILED` + retry) remains the one
  measurement failure; the Test-speed notice code and retry are deleted.
- Selection, expansion, refresh, load-error and notice behavior are otherwise
  untouched.

### Screen (`LocationsScreen.kt`)

- Country row: `KumihoRow` with flag prefix, name title, `N servers` subtitle,
  and a trailing row of best-latency text + expand glyph (the glyph keeps its
  Expand/Collapse content description).
- Server row: hostname title; subtitle `City · protocols · port`; quarantine
  mark appended; latency and checkmark stay trailing; unchanged selection
  behavior.
- Toolbar: single row with the Refresh icon, `Ping all` (with `Pinging N…`
  while running) and the expand/collapse toggle. The helper text, Test speed
  button and their tags/strings are removed.

### Constraints

- No engine, IPC, daemon, facade, GUI or TUI file changes; protocol v1 is
  untouched and the selection payload shape is unchanged.
- English-only, existing typography/spacing/touch-target tokens and the house
  `design` skill rules apply; colour is never the only carrier of meaning.
- The filter stays pure and local (no network on typing), and manual ping
  remains the only trigger (D6).

## Risks + rollback

- **Mobile data.** Manual-only pinging keeps D24; the ping probes are the same
  existing TCP probes, now over the whole list (already the `Ping all`
  behavior). No change to probe count per run.
- **Rows moving while reading.** Live sorting (D10) can move a country while
  the user is looking at it. Mitigation: measurements only arrive during a
  manual run, the run is bounded by the existing window, and unmeasured rows
  keep stable engine order.
- **Sort surprise under search.** D12 sorts the filtered view; the assumed
  rule is flagged for review at the spec gate.
- **Lost power-user access to per-city lists.** Cities are no longer a
  selectable level, but their servers and names remain visible in the country
  list and searchable; no capability is removed from the engine.
- **Desktop divergence.** Android now differs from the GUI/TUI trees;
  `docs/android.md` parity notes and the changelog state this deliberately.
- **Rollback.** Revert the implementation commits; there is no persisted
  state, protocol change or migration. The app keeps working with an older
  engine and vice versa.

## Dependencies / ordering

1. **The keep-outside-connections work must be implemented and committed
   first.** It opens the shared `[0.5.2]` changelog section, adds canonical
   design decision `D52`, shifts the Android screenshot index and touches
   `docs/android.md` and `docs/android-testing.md`. This change appends after
   it and must not edit its Settings/bridge files or reuse `D52`.
2. Implementation order: pure row/latency/sort rules + JVM tests → view model
   + tests → screen/toolbar/strings/tags + Compose and screenshot fixtures →
   docs/changelog/design log → full Android and Go gates.
3. Each step is a commit-sized unit; the Go gate stays green after every
   commit (no Go file changes expected).

## Open questions

None. Every decision is `decided` or `assumed`; the seven assumed entries
(D9, D12, D18, D19, D23, D24, D25) are flagged for review.
