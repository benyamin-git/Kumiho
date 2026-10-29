# Locations redesign — Design Spec

**Status:** Approved
**Date:** 2026-10-29
**Release:** v0.8.0 Region and location truth (UI component)
**Supersedes (where they conflict):** the Locations UI decisions in
`docs/superpowers/specs/2026-10-23-android-locations-one-level-design.md`,
`docs/superpowers/specs/2026-10-22-v0.5.1-locations-polish-design.md`, the
Android TV Locations portions of
`docs/superpowers/specs/2026-10-26-android-tv-design.md`, and the relevant
Locations decisions in `docs/superpowers/specs/2026-10-21-android-client-design.md`.
The no-background-probe rule, one-run-at-a-time rule, Ping all deduplication and
the existing target caps remain in force.

## Objective / why

Make Locations a single, scannable server list instead of a hierarchy. Every
client currently asks the user to open countries and sometimes cities before
seeing a server; Android and TV use emoji flags, the desktop clients hide the
server context in tree rows, and Android/TV spend space explaining why a saved
location has not taken effect yet. This redesign makes the server the unit of
choice, uses real bundled flag assets on every graphical client, removes the
connected-location note, and gives every client the same explainable ordering:
country order by default, speed order only after a completed Ping all.

## Success criteria / definition of done

1. Android phone, Android TV, the Windows GUI and the TUI show a flat list of
   server rows only: no country headers, city headers, carets, disclosures,
   expand/collapse controls or "toggle all" actions. The CLI is unchanged.
2. Every graphical server row leads with a rectangular 4:3 flag, a star for
   Recommended or a globe for an unknown country, followed by the full country
   and city names and a second detail line of `hostname:port · protocols`.
   Flags appear on every graphical surface listed in D7; the TUI keeps its
   one-line emoji format.
3. The Android/TV connected-location note is gone from UI, resources, tests and
   docs, with no replacement. The selection summary remains and reads
   `Country · City · host:port · protocols`.
4. Before any ping, usable servers order as Recommended first, then country name
   A–Z, city name A–Z and hostname A–Z. After a Ping all that settles with at
   least one result, the list orders fastest first, timeouts/unpinged last and
   ties by country/city/host, with Recommended still first.
5. Speed ordering is applied once after the run completes; no row moves while
   the run is in flight. It lasts only for the current visit and resets on
   leaving/re-entering, refreshing the list or restarting the app. A transport
   error or a run with no results leaves country order and the existing failure
   notice.
6. Ping all measures every usable loaded server even when a search filter is
   active. Test speed, expand/collapse controls and their key bindings/help text
   are removed from every client.
7. Quarantined servers and records without a usable host:port are hidden
   everywhere. The list retains search, Refresh, Ping all, latency labels,
   selected-row checkmarks, empty states and no-match states.
8. Search matches country name/code, city name/code, hostname, `host:port` and
   protocol names, case-insensitively. Its placeholder and no-match copy say
   “country, city, server, or protocol”.
9. Graphical flags use the pinned `flag-icons` 7.6.0 4:3 source (MIT) bundled
   offline; Android uses pre-rendered 192×144 WebP assets and adds no runtime
   SVG dependency; the Windows GUI uses the source SVGs; the TUI uses its
   existing flag emoji. Licensing is recorded in `NOTICE` and
   `third_party/flag-icons/LICENSE`.
10. Phone rows are 56 dp, desktop rows are 48 px and TV keeps its ten-foot
    metrics; flags have a 1 px outline and 2 dp radius. The phone emulator, TV
    emulator, GUI web harness and TUI capture checks all pass.
11. The engine, IPC protocol, server-list data, selection persistence and CLI
    remain unchanged. There is no version bump, tag or release action in this
    UI work.

## Scope

### In scope

- Android phone `ui/locations` and its selection summary, search, Ping all and
  list rows.
- Android TV `ui/tv/locations` and its selection summary, search, Ping all and
  list rows.
- Android dashboard compact location picker and TV connect-options flag
  surfaces.
- Windows GUI Locations screen (flat list, search, Ping all, sorting, flags),
  dashboard location label and embedded flag assets.
- Linux TUI Locations screen (flat one-line rows, emoji, search, Ping all,
  sorting and key help).
- Flag source assets, Android generated assets, licensing, tests, docs and
  screenshots required by the changed UI.
- Removal of the connected-location hint and obsolete Test speed / expansion
  code paths and strings.

### Non-goals (explicitly out)

- CLI output, daemon commands, engine behavior, protocol v1, IPC envelopes,
  server-list fetching/filtering, real-delay ping, Fastest, favorites, recents,
  relay/451 handling or any other v0.8.0 feature.
- Changing the persisted location, how `select_location` works, account or
  settings behavior, or the connected session's server.
- New background pings, automatic pings, ping cancellation or a new latency
  source.
- Physical phone/TV testing; emulator, GUI harness and TUI capture are the
  required manual surfaces.
- Version bump, changelog release entry, tag or GitHub release.
- A TUI image renderer or any new runtime SVG library/dependency.
- Changes to the visual language beyond the list, flags, note removal and
  control cleanup described here.

## Decision log

| # | Decision | Rationale | Status | Source |
| --- | --- | --- | --- | --- |
| D1 | Cover Android phone, Android TV, Windows GUI and TUI; leave the CLI unchanged. | These are the user-facing location clients; the CLI has no graphical Locations surface. | decided | user |
| D2 | Deliver the UI as part of v0.8.0 Region and location truth; do not renumber the roadmap again. | The release already owns location truth; a separate release would force another roadmap shift. | decided | user |
| D3 | Replace every country/city/server tree with one flat server-only list; remove headers, carets, expansion and Toggle all. | The user wants servers, not navigation; the tree adds taps and hides the choice. | decided | user |
| D4 | Graphical primary line: leading flag/star/globe, full `Country · City`, no country code; omit the city when missing or identical to the country. | Full names are readable and the user chose them over codes; omission avoids duplicate text. | decided | user |
| D5 | Graphical detail line: `hostname:port · protocols`, protocols comma-separated and omitted when empty. | Keeps the server identity and protocol visible in the compact second line. | decided | user |
| D6 | TUI row: one line, `🇺🇸 US · New York · host:port`; no protocol text; Recommended starts `★ REC`; keep the existing emoji helper. | A terminal cannot show real flag images; one line stays scannable. | decided | user |
| D7 | Real flags replace emoji on every graphical flag surface: Locations rows, Android/TV selection summary, Android compact picker, TV connect options and the GUI dashboard label. TUI keeps emoji; CLI has none. | The user wants real icons wherever graphical flags already appear, not just on one screen. | decided | user |
| D8 | Use `flag-icons` 7.6.0 4:3 SVGs (MIT), bundle every ISO 3166-1 alpha-2 flag, and pin the version. | The user chose rectangular real flags and offline operation; a pinned MIT source is reproducible. | decided | user |
| D9 | Show a star for REC and a globe for an unknown/invalid country code. | REC is not a country; a globe is an honest fallback rather than a wrong flag. | decided | user |
| D10 | Android packages pre-rendered 192×144 WebP assets and adds no runtime SVG dependency; the GUI serves the source SVGs; generated Android assets are committed. | The user chose predictable assets over a new renderer; 192×144 is ample for phone and TV. | decided | user |
| D11 | Flags are 4:3: 24×18 dp in phone rows, 20×15 in summaries/pickers, 32×24 on TV and 24×18 in the GUI, with a 1 px outline and 2 dp radius. | Keeps white flags visible on all themes and gives the list a consistent leading unit. | assumed | recommendation |
| D12 | Use `internal/gui/web/static/flags/4x3/<cc>.svg`, `android/app/src/main/res/drawable-nodpi/flag_<cc>.webp`, `scripts/flags/generate-android-flags.sh` (ffmpeg), `third_party/flag-icons/LICENSE`, an Android `CountryFlags.kt` plus generated resource map, GUI `flagAsset`/`flagIcon` helpers and the existing TUI `FlagEmoji`. | The user approved the recommended internal defaults. | decided | user |
| D13 | Remove the Android/TV connected-location hint completely, including its resource, tests, screenshot text and documentation; do not replace it with another banner. | It consumes vertical space; the saved-selection behavior itself is unchanged and the summary still shows the choice. | decided | user |
| D14 | Keep the Android/TV selection summary with the flag/star and `Country · City · host:port · protocols`; omit city when missing/identical and protocols when empty. | The user wants the summary kept, and the full server detail matches the row. | decided | user |
| D15 | Default order: Recommended first, then country name A–Z, city name A–Z, hostname A–Z; case-insensitive and stable. | The user wants country order by default, independent of API order. | decided | user |
| D16 | Search matches country name/code, city name/code, hostname, `host:port` and protocol names, case-insensitively and trimmed; use the new “country, city, server, or protocol” copy. | Every visible field should be findable, and the copy names the expanded scope. | decided | user |
| D17 | Keep only Ping all; remove Test speed, expand/collapse actions and their key bindings/help text everywhere; nothing pings automatically on load. | One speed action is enough and the no-background-probe rule remains. | decided | user |
| D18 | Ping all measures every usable loaded server, regardless of the active search filter; it remains one run at a time and deduplicated by address. | A complete result set should not depend on what is currently visible. | decided | user |
| D19 | Switch to speed order only after the run completes, never live as results arrive. | Avoids rows jumping under the user while they read or select. | decided | user |
| D20 | Speed order: Recommended first; finite latency ascending; timeouts/unpinged last; ties by country/city/host; stable. | The user chose a deterministic, explainable order with recommended still pinned. | decided | user |
| D21 | If Ping all ends with a transport error or no results, stay in country order and show the existing failure notice; enter speed order only after a run with at least one result. | A failed measurement cannot justify a speed claim. | decided | user |
| D22 | Speed order lasts only for the current visit; leaving/re-entering, refreshing the list or restarting resets to country order. | The user wants country order as the default and a transient speed view. | decided | user |
| D23 | Open the list at the top and jump to the top after speed ordering. | Predictable start and the fastest result is immediately visible. | decided | user |
| D24 | Hide quarantined servers and records without a usable host:port everywhere; remove the obsolete quarantine row/badge. | The user chose a clean list of usable choices; the old clients disagreed on visibility. | decided | user |
| D25 | Phone rows 56 dp, desktop rows 48 px, TV existing ten-foot metrics; touch targets stay at least 48 dp. | A two-line row needs more height, but the note removal should still leave a dense list. | decided | user |
| D26 | Leave selection persistence, `select_location`, engine, IPC and protocol behavior unchanged; only the UI and its derived rows/flags change. | This is a UI release slice and must not create a protocol or data migration. | assumed | recommendation |
| D27 | Keep latency labels and the selected-row checkmark trailing; a selected server hidden by filter/quarantine still appears in the summary but has no visible row. | Preserves context without pretending a hidden row is in the list. | assumed | recommendation |
| D28 | Preserve existing loading, load-error, empty and no-match states and retry behavior; only the no-match copy and list content change. | Avoids inventing new error UX beyond the requested redesign. | assumed | recommendation |
| D29 | TUI keeps `/` filter, `r` refresh, `P` Ping all, Enter select, `esc` back and `?` help; remove `p` and `x`; update the footer help. | Direct consequence of Ping all only and no expansion; the rest is existing TUI convention. | assumed | recommendation |
| D30 | Update all affected docs, AGENTS design record, NOTICE/licensing, screenshot harness and regenerated screenshots. | The user requires the visible docs and assets to match the shipped UI. | decided | user |
| D31 | Required verification surfaces are the Android phone emulator, Android TV emulator, Windows GUI web harness and TUI capture; no physical-device gate. | These cover the changed clients and the user accepted the v0.8 roadmap gate. | decided | user |
| D32 | Write the spec to `docs/superpowers/specs/2026-10-29-locations-redesign-design.md` and the plan to `docs/superpowers/plans/2026-10-29-locations-redesign.md`; commit each after explicit approval. | The user approved the dated planning paths and the two-gate flow. | decided | user |
| D33 | Use the source SVGs for the GUI, pre-rendered WebP for Android and the existing emoji for TUI; no runtime network fetch and no new runtime SVG library. | The user chose bundled real icons and no new Android renderer. | decided | user |
| D34 | This spec supersedes conflicting Locations UI decisions in the older specs named at the top; keep their no-background-probe, one-run, deduplication and cap rules. | The old tree/emoji/live-sort decisions are intentionally replaced; safety rules are not. | assumed | recommendation |
| D35 | Do not bump the version, tag or release in this UI work; v0.8.0 ships with its other components. | The release process is separate, and this slice must remain independently revertible. | assumed | recommendation |
| D36 | Filtering does not change the sort mode: Ping all still measures all servers and clearing a filter keeps the current speed order until the visit ends. | The filter is a view, not a new measurement or ordering event. | assumed | recommendation |
| D37 | Render protocols as distinct non-empty names joined with `, ` in engine order; omit the separator when there are none. | Matches the existing protocol vocabulary and avoids empty punctuation. | assumed | recommendation |
| D38 | Flags are decorative in accessibility terms (`contentDescription`/`alt` empty); country/city text and row semantics carry the meaning; row selection remains announced. | The icon must not duplicate or replace the accessible name. | assumed | recommendation |
| D39 | Keep the GUI dashboard’s native `<select>`; remove emoji from its option text, keep options text-only, and show one real flag beside the control for the currently selected country. | Native options cannot contain images; this keeps the accessible select while still giving the dashboard a real flag. | decided | user |

## Architecture

### Shared view semantics

Each client already has its own state layer; this design does not introduce a
cross-language library. Each client implements the same small set of pure rules
against its existing server-list model:

- **Usable server predicate.** A record is visible only when it is not
  quarantined, has a non-empty host and a valid port, and counts as connectable
  under the existing client logic. Records failing the predicate are omitted
  from rows, counts and Ping all targets.
- **Flat row model.** A row carries country code/name, city code/name, server
  host/port, protocol names, the country display string, the detail string and
  a stable row identity. There are no parent/expansion fields.
- **Display strings.** The primary string is the full country name and, when
  present and different, the full city name joined by ` · `; otherwise the
  country name alone. The detail string is `host:port` plus ` · ` and the
  distinct protocol names when any exist. REC uses the display name
  “Recommended”.
- **Filter.** One normalized haystack per row contains country name/code, city
  name/code, host, `host:port` and protocol names. Matching is trimmed and
  case-insensitive.
- **Ordering.** A single comparator supports two modes. Country mode is REC
  first, then country, city and host comparisons. Speed mode is REC first, then
  finite latency ascending, non-finite last, then country/city/host. Comparisons
  are stable and case-insensitive for names.
- **Speed lifecycle.** The screen owns a two-state sort mode, country by
  default. Ping all does not change it while running. When the client observes a
  completed run with at least one latency result, it switches to speed mode and
  scrolls to the top. A run that ends in a transport error or with no results
  leaves the mode untouched. Refresh, re-entering the screen and app restart
  reset the mode to country; the existing latency values may remain cached for
  display, but they no longer drive the order.

### Client boundaries

- **Android phone (`ui/locations`, `ui/dashboard`).** `LocationTree` becomes the
  pure flat-row/filter/order model and carries the tests. `LocationsViewModel`
  owns filter, selected server, latency map, ping run and the transient sort
  mode. `LocationsScreen` renders the summary and a `LazyColumn` of two-line
  rows; `LocationFilter` is reduced to the flat matcher. The dashboard compact
  picker and TV connect options consume the same country-flag composable.
- **Android TV (`ui/tv/locations`, `ui/tv/dashboard`).** Reuses the phone view
  model/model rules and swaps in the ten-foot row, summary and focus metrics.
  D-pad focus moves between server rows; no disclosure or expand-all remains.
- **Windows GUI (`internal/gui/web/static`).** `locations.js` flattens the
  projection into server rows, owns the filter/ping run/sort mode and renders
  the row markup. `flags.js` exposes flag asset/icon helpers instead of the
  emoji string helper. `dashboard.js` keeps the native `<select>` and inserts
  the current-selection flag beside it; its option text is text-only (D39).
  `locations.css` owns the 48 px row, flags,
  two-line typography, selected state and focus treatment.
- **TUI (`internal/tui/locations.go`).** The tree model becomes a flat row
  slice; the viewport renders one line per server, keeps the existing emoji
  helper, adds the country code and city before `host:port`, and removes the
  expansion/Test speed paths. `P` starts the all-servers run; when it completes
  with a result the row slice sorts to speed mode until the screen is left.
- **CLI.** Untouched.

### Flags and assets

- **Source.** `flag-icons` 7.6.0’s 4:3 SVGs live under
  `internal/gui/web/static/flags/4x3/`, named by lowercase ISO alpha-2 code.
  They are embedded by `internal/gui/web/assets.go` and served locally by the
  GUI; there is no runtime download.
- **Android generation.** `scripts/flags/generate-android-flags.sh` uses
  `ffmpeg` to rasterize each source SVG to a 192×144 lossless WebP under
  `android/app/src/main/res/drawable-nodpi/flag_<cc>.webp`. Generated files are
  committed; Gradle and the Go build never fetch or render flags. A generated
  `CountryFlags.kt` resource map keeps every `R.drawable.flag_*` reference
  visible to resource shrinking. REC uses a small star vector; unknown or
  invalid codes use the existing globe vector (or a GUI/TV equivalent).
- **TUI.** Keeps `serverlist.FlagEmoji`; no image asset is added. REC begins
  with `★ REC`, unknown codes show their code without a flag.
- **Licensing.** Add `third_party/flag-icons/LICENSE`, a short source/version
  note and checksums, and a `NOTICE` entry for the MIT-licensed assets and the
  generated WebP derivatives. The star/globe vectors are project assets.

### Screen behavior

- The list opens at the top of country order. The summary (Android/TV) or the
  dashboard label is the only persistent selection context; there is no
  connected-location note.
- Search filters immediately without network activity and without changing the
  sort mode. Clearing it restores the matching rows in the current order.
- Ping all is disabled while a run is active and reports the existing
  `Pinging N…` state. Results fill the existing latency labels; only a
  completed, non-failed run switches the order.
- Refresh clears latency/order state as part of starting a fresh visit and
  returns to country mode. A refresh/load error leaves the previous state as
  the client already does, with the existing notice.
- Selecting a server persists the same country/city/server tuple as before; the
  row selected/check state follows that selection.

### Accessibility, density and performance

- Flags are decorative images; the country/city text is the accessible
  information. The star/globe must not introduce a false country name.
- Phone rows are 56 dp, desktop rows 48 px, with at least 48 dp targets; TV uses
  the existing ten-foot type/focus scale. Flags keep 4:3, a 1 px outline and
  2 dp radius so pale flags remain visible on light, dark and OLED themes.
- Android uses `LazyColumn` and resource-backed painters; the GUI reuses one
  local SVG per country and does not inject 249 copies of markup; the TUI draws
  only visible rows. No new network calls, background probes or asset decoders
  are introduced.

## Risks + rollback

- **Asset size.** 249 192×144 WebP files add repository/APK weight. Mitigate
  with lossless WebP, only the ISO set, a size check in the release verifier and
  a generator that fails if a source flag is missing. If the size is
  unacceptable, lower the generated resolution or use density-specific assets;
  that is a plan change, not a data/engine change.
- **SVG rendering fidelity.** `flag-icons` uses strokes and markers for some
  flags. The generator must verify every source renders at the target size and
  spot-check complex flags (for example the United States) in the GUI and
  Android. A broken asset falls back to the globe until fixed; it must never
  show a wrong flag.
- **Cross-client sorting drift.** Android, GUI and TUI each implement the
  comparator. Use the same fixtures for REC, equal latencies, failures,
  accents/case, missing cities and protocols; the plan must test each client’s
  order against the same expected rows.
- **Layout overflow.** Two-line rows with long hostnames/protocols can overflow
  narrow phones or TV overscan. Use ellipsis on the detail line, keep the
  trailing latency/check visible, and validate at the specified densities.
- **Resource shrinking.** Dynamic flag lookup can be stripped. Generate a map
  with explicit `R.drawable` references and verify a release/minified build.
- **Control removal regressions.** Test speed, expansion and their key help are
  referenced by tests/docs; remove them in the same task as the UI change and
  update the screenshot harness so stale captures cannot pass.
- **Licensing/attribution.** Flag assets and generated derivatives need the
  MIT notice in `NOTICE` and a source/version record; verify before release.

Rollback is a pure UI revert: restore the previous screen/model branches, remove
the generated flag assets and the added license/NOTICE entries and re-run the
affected Go/Android suites. There is no protocol, account, settings or data
migration to reverse. Because this is a UI-only slice of v0.8.0, it can be
reverted independently of the engine/relay/DNS work.

## Dependencies / ordering

1. Pin and vendor the `flag-icons` 7.6.0 source SVGs and license, then add the
   GUI assets and the Android generator/output map.
2. Implement the shared row/filter/order rules and the speed-mode state machine
   in the Android model, GUI projection and TUI model; verify them with pure
   tests before screen work.
3. Replace the four screen surfaces (phone, TV, GUI, TUI) and remove the note,
   Test speed and expansion controls.
4. Update docs, screenshots and NOTICE; run the Android phone/TV, GUI harness
   and TUI capture checks.
5. The other v0.8.0 location truth work (real-delay ping, 451/relay, JEXL,
   Fastest, favorites) is independent; this UI slice consumes the existing
   ping/latency and server-list surfaces. If those features change the latency
   source before this slice lands, the UI still consumes the same latency map
   and keeps these ordering rules.

## Open questions

None. All decisions above are `decided` or explicitly `assumed` and flagged for
review; there are no TBDs.
