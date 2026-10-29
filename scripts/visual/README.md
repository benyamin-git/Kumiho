# Visual capture (dev-time only)

`capture.mjs` screenshots the real GUI assets in headless Chromium against the
mock daemon served by the **dev web harness** (`scripts/dev-gui-web.sh`,
`cmd/kumiho-gui-web`): every screen × the three themes, the full six-accent
sweep on the dashboard, the key daemon states driven through the harness's
`/dev` endpoints, and the dev panel itself. It exists for design review during
the GUI rewrite (D28); the screen tasks (M2/M3) run it and review the output.

It is **not part of the build gate or CI**, and its output is **never
committed**: it writes to `/tmp/opencode/captures/` by default, and the script
refuses a `KUMIHO_CAPTURES_DIR` inside the repository.

## Prerequisites

- A running harness: `make build-web && scripts/dev-gui-web.sh start`
  (port 7407; `KUMIHO_GUI_WEB_PORT` moves it, `KUMIHO_GUI_URL` overrides the
  whole base URL).
- Node 22+ and `npx`.
- Playwright + Chromium. There is deliberately **no repo `package.json`**
  (D2, no npm), so the script cannot `import "playwright"` by bare specifier
  (Node's ESM resolver ignores `NODE_PATH`). Instead it locates an existing
  install itself and requires it from there, in this order:

  1. `KUMIHO_PLAYWRIGHT=/path/to/node_modules/playwright` (explicit override);
  2. `node_modules/playwright` in the script's ancestor directories;
  3. entries of `NODE_PATH`;
  4. the global npm root (`npm root -g`);
  5. the npx cache (`~/.npm/_npx/<hash>/node_modules/playwright`), newest
     stable version first.

  Priming the npx cache once is enough for the default path:

      npx -y playwright --version        # populates the npx cache
      npx playwright install chromium    # only if the browser is missing

  The Chromium build matching the resolved playwright version must exist in
  `~/.cache/ms-playwright`; a version mismatch fails at launch with an
  install hint.

## Run

    scripts/dev-gui-web.sh start
    node scripts/visual/capture.mjs                # the full 68-capture run
    node scripts/visual/capture.mjs --only dashboard
    node scripts/visual/capture.mjs --list         # manifest + planned files
    scripts/dev-gui-web.sh stop

| Variable | Default | Meaning |
|---|---|---|
| `KUMIHO_GUI_URL` | `http://127.0.0.1:7407/` | Harness base URL |
| `KUMIHO_GUI_WEB_PORT` | `7407` | Port used when the URL is not set |
| `KUMIHO_CAPTURES_DIR` | `/tmp/opencode/captures` | Output directory (must be outside the repo) |
| `KUMIHO_PLAYWRIGHT` | – | Path to the playwright package to import |

`--only <screen>` captures one destination: its three themes, the accent
sweep and states for `dashboard`, this screen's states for the others, and no
dev panel (`--only dev` is the dev panel alone). It is the iteration loop for
a single screen task.

## Output layout

    /tmp/opencode/captures/
      <screen>/<theme>.png                     e.g. dashboard/light.png
      <screen>/<theme>-<accent>.png            e.g. dashboard/dark-teal.png
      <screen>/<theme>[-<accent>]-<state>.png  e.g. dashboard/dark-connected.png
      dev/panel.png                            the harness control panel, once

- The screen × theme matrix uses the default accent (blue), so
  `dashboard/dark.png` and the `dark-blue` capture of the accent sweep are
  intentionally byte-comparable.
- State captures use the dark theme and blue accent unless an entry sets
  `theme`/`accent`; the accent is omitted from the file name only when it is
  blue.
- Before the run, existing `*.png` in the screen directories about to be
  written are removed, so a run never mixes with stale files.
- Until T2.1 lands, the UI is a placeholder that consumes only `color-scheme`
  from the injected theme block, so dark/oled and the accent sweep renders are
  byte-identical (light differs). The harness log confirms every `prefs_set`
  went through; the pixels start differing once the real screens use the
  theme tokens.

## Matrix

Screens (hash routes of the SPA, T2.1's router owns them):

| Screen | Route | Built by |
|---|---|---|
| dashboard | `#/dashboard` | T2.2 |
| locations | `#/locations` | T2.3 |
| settings | `#/settings` | T2.4 |
| account | `#/account` | T3.1 |
| logs | `#/logs` | T3.2 |
| diagnostics | `#/diagnostics` | T3.3 |
| onboarding | `#/onboarding` | T3.4 |

Themes: `light`, `dark`, `oled`. Accents: `blue`, `teal`, `green`, `orange`,
`rose`, `violet` (the accent sweep runs on the dashboard in the dark theme).

States, all driven through the harness (dark theme, blue accent):

| File (<screen>/dark-…) | Setup |
|---|---|
| `disconnected` | `/dev/scenario disconnected` |
| `connected` | `/dev/scenario connected` |
| `connecting` | `/dev/scenario connecting` |
| `error` | `/dev/scenario error` (FATAL, exhausted quota) |
| `connect-options` | route hint `#/dashboard?state=connect-options` |
| `signout-confirm` | route hint `#/account?state=signout-confirm` |
| `account-add` | Add account dialog (Dark) |
| `account-add-password` | Add account → Use password, first form (Light) |
| `account-add-browser` | Add account → Continue in browser, waiting card (OLED) |
| `cleanup-confirm` | route hint `#/diagnostics?state=cleanup-confirm` |
| `cleanup-done` | diagnostics: confirm the Cleanup… dialog; waits for the `Cleanup finished` notice |
| `checking` | diagnostics: Re-run checks clicked; captures the `Checking…` placeholder |
| `copied` | diagnostics: Copy details clicked; waits for the deterministic clipboard write |
| `service-stopped` | diagnostics: Stop clicked (dev service settles to Stopped) |
| `service-started` | diagnostics: Start clicked (dev service settles to Running) |
| `live-entry` | `/dev/log`, then capture the logs screen |
| `speed-order` | real **Ping all** click for every usable server; captures the completed speed order with `42 ms` / `timeout` labels |
| `welcome`, `login`, `login-signed-in`, `login-browser`, `login-choice-password`, `login-password`, `login-2fa`, `health-check`, `autoconnect`, `finish` | wizard-step hints on `#/onboarding?state=…`, dark and light; `prefs_set {"onboarded":false}` arms the real first-run gate (restored after each capture) |
| `login-signed-in` | wizard hint `login` with the default authenticated scenario (the already-signed-in variant) |
| `login-password`, `login-2fa` | wizard hint `login` on the `signed-out` scenario, submitting an email (the password page; the 2FA page via the mock's `2fa@…` rule) |
| `settings-bottom` | the settings rows pane scrolled to its bottom (the pane draws no scrollbar, so the capture scrolls it directly), light/dark/oled — the five v0.4.0 rows live at the end of the list |
| `latency-3s` | `/dev/latency 3000` + `connect` action in flight |
| `injected-failure` | `/dev/fail` armed, then `connect` fails through it |

### Capture hints (M2/M3 contract)

Wizard steps and dialogs are deep-linked as `#/<screen>?state=<name>` — the
router should parse the hash route and query, tolerate unknown `state` values,
and, once a screen owns that state (a wizard step, a confirm dialog, the
Connect options popover), open it directly. Until then the script captures
whatever renders, so the manifest already exists when the screens land and the
run needs no changes.

## Policies

- **Waits:** `page.goto`/`reload` with `waitUntil: "load"` plus a fixed 350 ms
  settle. `networkidle` is deliberately not used: the SPA keeps a permanent
  SSE connection open (`GET /events`), so "no network activity for 500 ms"
  never happens. Entries may carry `waitFor` (a selector awaited after
  navigating, before the actions — e.g. the diagnostics screen's first doctor
  run) and `waitAfter` (a selector awaited after the clicks, before the
  capture — e.g. a completed cleanup notice); both get a 20 s budget.
- **Failures:** no retries. A navigation failure aborts with the harness
  hint (`scripts/dev-gui-web.sh start`). Console errors and uncaught page
  errors are recorded per capture and make the run exit non-zero at the end;
  the browser's automatic `/favicon.ico` 404 is the single ignored noise
  (logged as a warning). Every capture is checked for existence and a
  non-trivial size.
- **Determinism:** 1280×800 viewport at scale 1, `prefers-reduced-motion:
  reduce`, `en-US`/UTC. Theme and accent changes go live through `POST
  /action` `prefs_set` followed by a reload (the harness re-injects the theme
  CSS on the next index render). Each state is preceded by a reset
  (`scenario disconnected`, latency 0, fail cleared); states that leave a
  notice behind (`latency-3s`, `injected-failure`) run last. After the run
  the harness is left at dark/blue and the clean scenario.

## Never commit

Output lives under `/tmp/opencode/captures/` (or another path outside the
repo), is regenerated on demand, and is excluded from every commit. The
runner is a local design-review tool, not a CI artifact.
