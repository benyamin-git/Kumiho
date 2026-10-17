#!/usr/bin/env node
// Playwright capture of the Kumiho GUI web harness (T1.11): screenshots of
// the real UI assets in a browser against the mock daemon, for design review
// during the GUI rewrite. Dev-time only: never part of the build gate or CI,
// and its output is never committed (the one rule below enforces that).
//
// Usage and the manifest contract are documented in scripts/visual/README.md:
//
//   node scripts/visual/capture.mjs [--only <screen>] [--list]
//
// The harness must be running (scripts/dev-gui-web.sh start) and playwright
// must be resolvable (see resolvePlaywright; no repo package.json exists).

import { execFileSync } from "node:child_process";
import {
  existsSync,
  mkdirSync,
  readdirSync,
  readFileSync,
  rmSync,
  statSync,
} from "node:fs";
import { createRequire } from "node:module";
import { homedir } from "node:os";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = path.resolve(SCRIPT_DIR, "..", "..");

// Harness address: scripts/dev-gui-web.sh's port by default; KUMIHO_GUI_URL
// overrides (e.g. a different host for LAN review).
const HARNESS_URL = (
  process.env.KUMIHO_GUI_URL ||
  `http://127.0.0.1:${process.env.KUMIHO_GUI_WEB_PORT || "7407"}`
).replace(/\/+$/, "") + "/";

const DEFAULT_CAPTURES_DIR = "/tmp/opencode/captures";
const CAPTURES_DIR = path.resolve(
  process.env.KUMIHO_CAPTURES_DIR || DEFAULT_CAPTURES_DIR,
);

// Desktop-only app (T2.1); a fixed viewport and scale keep captures
// comparable between runs. reducedMotion makes animated states settle.
const VIEWPORT = { width: 1280, height: 800 };
const DEVICE_SCALE_FACTOR = 1;

// Load event + settle delay. networkidle is deliberately not used: the SPA
// holds a permanent SSE connection (/events), so "no network for 500 ms"
// never happens. A fixed settle covers the SSE snapshot and first paint.
const SETTLE_MS = 350;
const NAV_TIMEOUT_MS = 15000;
// waitFor/waitAfter waits get a longer budget: the diagnostics capture waits
// for a real doctor run (network probes) and for the dev cleanup/service
// actions to land their notices.
const WAIT_TIMEOUT_MS = 20000;

const DEFAULT_THEME = "light";
const DEFAULT_ACCENT = "blue";

// The SPA's hash-route destinations (the rail's six destinations plus the
// onboarding wizard). T2.1's router owns these; the capture script only
// navigates them. The wizard steps and dialogs are deep-linked with a
// `?state=<name>` capture hint on the screen's route — the router should
// tolerate (and later honor) the parameter; see README.md.
//
// `wait` is an optional selector every capture of the screen waits for after
// navigating: the diagnostics screen runs its first doctor suite on the
// first visit, and the captures must show the finished check cards, not the
// `Checking…` placeholder.
const SCREENS = [
  { name: "dashboard", hash: "#/dashboard" },
  { name: "locations", hash: "#/locations" },
  { name: "settings", hash: "#/settings" },
  { name: "account", hash: "#/account" },
  { name: "logs", hash: "#/logs" },
  { name: "diagnostics", hash: "#/diagnostics", wait: ".diag__check" },
  { name: "onboarding", hash: "#/onboarding" },
];

const THEMES = ["light", "dark", "oled"];

const ACCENTS = ["blue", "teal", "green", "orange", "rose", "violet"];

// Key states beyond the plain screens. Each entry:
//   name        file-name suffix and manifest label
//   screen      SCREENS destination
//   hint        `?state=<hint>` capture hint (dialogs, wizard steps)
//   prefs       prefs_set payload applied before navigating; the appearance
//               baseline (onboarded true) is restored after the capture
//   scenario    /dev/scenario value to apply before navigating
//   latencyMs   /dev/latency value to apply before navigating
//   failMessage /dev/fail arm (message) before navigating
//   waitFor     selector waited for after navigating, before the actions
//   waitAfter   selector waited for after the clicks, before capturing
//   action      /action envelope name (and optional payload) after navigating
//   emitLog     /dev/log message after navigating
//   fill        {selector, value} typed into a screen control after the action
//   click       selectors clicked in order after the action/fill (screen
//               controls like the logs level button)
//   afterMs     extra wait after the capture (in-flight daemon calls)
//
// Order matters only through the notices actions leave behind: the
// notice-producing states (latency-3s, injected-failure) run last.
const STATES = [
  { name: "disconnected", screen: "dashboard", scenario: "disconnected" },
  { name: "connected", screen: "dashboard", scenario: "connected" },
  { name: "connecting", screen: "dashboard", scenario: "connecting" },
  { name: "error", screen: "dashboard", scenario: "error" },
  { name: "connect-options", screen: "dashboard", hint: "connect-options" },
  { name: "signout-confirm", screen: "account", hint: "signout-confirm" },
  {
    // The cleanup confirmation (T3.3, hint-driven, scripts/visual/README.md).
    name: "cleanup-confirm",
    screen: "diagnostics",
    hint: "cleanup-confirm",
    waitFor: "dialog[open]",
  },
  {
    // A completed cleanup: the dev service settles in under a second and the
    // inventory's `Cleanup finished` notice lands on the status line.
    name: "cleanup-done",
    screen: "diagnostics",
    waitFor: ".diag__check",
    click: ["#diag-cleanup", "#diag-cleanup-confirm"],
    waitAfter: '.diag__status:has-text("Cleanup finished")',
  },
  {
    // A re-run in flight: the list clears to the `Checking…` placeholder and
    // Re-run is inert.
    name: "checking",
    screen: "diagnostics",
    waitFor: ".diag__check",
    click: ["#diag-rerun"],
    waitAfter: ".diag__empty:not([hidden])",
  },
  {
    // Copy details: the clipboard payload lands and the success notice
    // shows. Runs after `checking`, whose re-run finishes first.
    name: "copied",
    screen: "diagnostics",
    waitFor: ".diag__check",
    click: ["#diag-copy"],
    waitAfter: '.diag__status:has-text("Details copied")',
  },
  {
    // A completed service stop: the card reads Stopped and the notice shows.
    name: "service-stopped",
    screen: "diagnostics",
    waitFor: ".diag__check",
    click: ["#diag-service-stop"],
    waitAfter: '.diag__status:has-text("Service stopped")',
  },
  {
    // A completed service start: everything is back to the initial state.
    name: "service-started",
    screen: "diagnostics",
    waitFor: ".diag__check",
    click: ["#diag-service-start"],
    waitAfter: '.diag__status:has-text("Service started")',
  },
  {
    name: "live-entry",
    screen: "logs",
    emitLog: "dev: capture live entry",
  },
  {
    // The level filter clicked round to debug (info → warn → error →
    // debug): all seven mock entries visible. Runs before the states that
    // fill the search box (the screen keeps its filters for the session).
    name: "level-debug",
    screen: "logs",
    click: ["#logs-level", "#logs-level", "#logs-level"],
  },
  {
    // A live search: only the entries whose tag or message match remain.
    name: "filtered",
    screen: "logs",
    fill: { selector: "#logs-search", value: "tunnel" },
  },
  {
    // The debug stop again in OLED (the accent-change reload resets the
    // session's filters): the fix-round contrast check for the debug tint.
    name: "level-debug",
    screen: "logs",
    theme: "oled",
    click: ["#logs-level", "#logs-level", "#logs-level"],
  },
  {
    // A completed export through the real button: the Exported to <path>
    // notice on the status line (the search box is cleared first so the
    // whole visible stream exports).
    name: "exported",
    screen: "logs",
    fill: { selector: "#logs-search", value: "" },
    click: ["#logs-export"],
  },
  {
    // A cleared ring: the quiet empty-stream line and the Logs cleared
    // notice.
    name: "cleared",
    screen: "logs",
    click: ["#logs-clear"],
  },
  {
    // The locations screen with Test-speed labels on its auto-revealed
    // selection (DE/Berlin): the mock's 42 ms / timeout pair.
    name: "speed-test",
    screen: "locations",
    action: {
      name: "ping",
      payload: { hosts: ["de1.edge.example:443", "de2.edge.example:443"] },
    },
  },
  // The onboarding wizard (T3.4): every step, dark and light, with
  // gui.toml's `onboarded` flag false so the first-run gate is the real one;
  // the route hint selects the step. The baseline gate state is restored
  // after each capture so later states show their own screens, not the
  // wizard.
  {
    name: "welcome",
    screen: "onboarding",
    hint: "welcome",
    prefs: { onboarded: false },
  },
  {
    // The signed-out daemon (T3.4 mock scenario) shows the email page; the
    // mock's blank-email login email resets its login machine first, so the
    // capture is deterministic from any prior state.
    name: "login",
    screen: "onboarding",
    hint: "login",
    scenario: "signed-out",
    prefs: { onboarded: false },
    action: { name: "login_email", payload: { email: "" } },
    waitAfter: "#onb-email",
  },
  {
    // The already-signed-in variant: the same reset against a live session.
    name: "login-signed-in",
    screen: "onboarding",
    hint: "login",
    prefs: { onboarded: false },
    action: { name: "login_email", payload: { email: "" } },
    waitAfter: ".onb__signed",
  },
  {
    // The password page: the daemon asks for one.
    name: "login-password",
    screen: "onboarding",
    hint: "login",
    scenario: "signed-out",
    prefs: { onboarded: false },
    action: { name: "login_email", payload: { email: "user@example.com" } },
    waitAfter: "#onb-password",
  },
  {
    // The 2FA page (the mock's 2fa@… rule).
    name: "login-2fa",
    screen: "onboarding",
    hint: "login",
    scenario: "signed-out",
    prefs: { onboarded: false },
    action: { name: "login_email", payload: { email: "2fa@example.com" } },
    waitAfter: "#onb-code",
  },
  {
    name: "health-check",
    screen: "onboarding",
    hint: "health-check",
    prefs: { onboarded: false },
    waitFor: ".onb__check",
  },
  {
    name: "autoconnect",
    screen: "onboarding",
    hint: "autoconnect",
    prefs: { onboarded: false },
  },
  {
    name: "finish",
    screen: "onboarding",
    hint: "finish",
    prefs: { onboarded: false },
  },
  {
    name: "welcome",
    screen: "onboarding",
    hint: "welcome",
    theme: "light",
    prefs: { onboarded: false },
  },
  {
    name: "login",
    screen: "onboarding",
    hint: "login",
    theme: "light",
    scenario: "signed-out",
    prefs: { onboarded: false },
    action: { name: "login_email", payload: { email: "" } },
    waitAfter: "#onb-email",
  },
  {
    name: "health-check",
    screen: "onboarding",
    hint: "health-check",
    theme: "light",
    prefs: { onboarded: false },
    waitFor: ".onb__check",
  },
  {
    name: "autoconnect",
    screen: "onboarding",
    hint: "autoconnect",
    theme: "light",
    prefs: { onboarded: false },
  },
  {
    name: "finish",
    screen: "onboarding",
    hint: "finish",
    theme: "light",
    prefs: { onboarded: false },
  },
  {
    // A completed daemon-setting change on Settings: the MTU success notice
    // on the status line (the overridden kill-switch row is already visible
    // in every settings capture, mock-served).
    name: "saved",
    screen: "settings",
    action: { name: "settings_set", payload: { key: "mtu", value: "1500" } },
  },
  {
    name: "latency-3s",
    screen: "dashboard",
    latencyMs: 3000,
    action: { name: "connect" },
    afterMs: 3300,
  },
  {
    name: "injected-failure",
    screen: "dashboard",
    failMessage: "guardian: HTTP 503",
    action: { name: "connect" },
    afterMs: 300,
  },
];

function routeFor(state) {
  const screen = SCREENS.find((s) => s.name === state.screen);
  if (!screen) throw new Error(`state ${state.name}: unknown screen ${state.screen}`);
  return state.hint ? `${screen.hash}?state=${state.hint}` : screen.hash;
}

function stateFile(state) {
  const theme = state.theme || "dark";
  const accent = state.accent || DEFAULT_ACCENT;
  const accentPart = accent !== DEFAULT_ACCENT ? `${accent}-` : "";
  return `${state.screen}/${theme}-${accentPart}${state.name}.png`;
}

// ---- capture plan ---------------------------------------------------------

// With --only, the default run is filtered to one destination: the screen's
// three themes, the dashboard accent sweep for dashboard, its states (plus
// account/diagnostics/logs hints), and no dev panel. --only dev is the dev
// panel alone.
function buildPlan(only) {
  const plan = [];
  const wanted = (screen) => !only || only === screen;
  for (const theme of THEMES) {
    for (const screen of SCREENS) {
      if (!wanted(screen.name)) continue;
      plan.push({
        file: `${screen.name}/${theme}.png`,
        label: `${screen.name} ${theme}`,
        route: screen.hash,
        theme,
        accent: DEFAULT_ACCENT,
        wait: screen.wait,
      });
    }
  }
  if (wanted("dashboard")) {
    for (const accent of ACCENTS) {
      plan.push({
        file: `dashboard/dark-${accent}.png`,
        label: `dashboard dark/${accent}`,
        route: "#/dashboard",
        theme: "dark",
        accent,
      });
    }
  }
  // The settings screen's accent variants (T2.4): the theme sweep above
  // covers blue; two more accents prove the choice marks track the applied
  // appearance.
  if (wanted("settings")) {
    for (const accent of ["teal", "violet"]) {
      plan.push({
        file: `settings/dark-${accent}.png`,
        label: `settings dark/${accent}`,
        route: "#/settings",
        theme: "dark",
        accent,
      });
    }
  }
  for (const state of STATES) {
    if (!wanted(state.screen)) continue;
    plan.push({
      file: stateFile(state),
      label: `${state.screen} ${state.theme || "dark"}-${state.name}`,
      route: routeFor(state),
      theme: state.theme || "dark",
      accent: state.accent || DEFAULT_ACCENT,
      state,
    });
  }
  if (!only || only === "dev") {
    plan.push({
      file: "dev/panel.png",
      label: "dev panel",
      route: "/dev",
      theme: null,
      accent: null,
    });
  }
  const seen = new Set();
  for (const item of plan) {
    if (seen.has(item.file)) throw new Error(`duplicate capture path ${item.file}`);
    seen.add(item.file);
  }
  return plan;
}

function printManifest() {
  console.log("screens:  " + SCREENS.map((s) => s.name).join(", "));
  console.log("themes:   " + THEMES.join(", "));
  console.log("accents:  " + ACCENTS.join(", "));
  console.log("states:   " + STATES.map((s) => `${s.screen}/${s.name}`).join(", "));
  console.log("dev panel: dev/panel.png (captured once)");
}

// ---- argument handling ----------------------------------------------------

function parseArgs(argv) {
  const opts = { only: null, list: false, help: false };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "--list") opts.list = true;
    else if (arg === "--help" || arg === "-h") opts.help = true;
    else if (arg === "--only") {
      const value = argv[++i];
      if (!value) throw new Error("--only needs a screen name");
      opts.only = value;
    }
    else if (arg.startsWith("--only=")) opts.only = arg.slice("--only=".length);
    else throw new Error(`unknown argument ${arg}`);
  }
  if (opts.only) {
    const valid = new Set([...SCREENS.map((s) => s.name), "dev"]);
    if (!valid.has(opts.only)) {
      throw new Error(`--only ${opts.only}: expected one of ${[...valid].join(", ")}`);
    }
  }
  return opts;
}

function usage() {
  console.log(`usage: node scripts/visual/capture.mjs [--only <screen>] [--list]

  --only <screen>  capture one destination (${SCREENS.map((s) => s.name).join(", ")}, dev)
  --list           print the manifest and the planned captures, then exit
  --help           this text

env:
  KUMIHO_GUI_URL      harness base URL (default http://127.0.0.1:7407/)
  KUMIHO_CAPTURES_DIR output directory (default ${DEFAULT_CAPTURES_DIR})
  KUMIHO_PLAYWRIGHT   path to the playwright package to import

The harness must be running first: scripts/dev-gui-web.sh start`);
}

// ---- playwright resolution -------------------------------------------------

// There is no repo package.json (D2, no npm), so `import "playwright"` cannot
// resolve from the script itself, and NODE_PATH does not apply to ESM. The
// script instead finds an existing playwright install and requires it from
// that location: KUMIHO_PLAYWRIGHT, an ancestor node_modules, NODE_PATH, the
// global npm root, or the npx cache (~/.npm/_npx/<hash>/node_modules), which
// is where `npx playwright ...` leaves it. Stable versions win over
// pre-releases; the newest wins among equals.
function resolvePlaywright() {
  const candidates = [];
  const add = (dir) => {
    if (dir) candidates.push(path.resolve(dir));
  };

  const explicit = process.env.KUMIHO_PLAYWRIGHT;
  if (explicit) {
    const mod = loadPlaywright(path.resolve(explicit));
    if (!mod) {
      throw new Error(
        `KUMIHO_PLAYWRIGHT=${explicit} is not a loadable playwright package`,
      );
    }
    return { mod, dir: path.resolve(explicit) };
  }

  for (let dir = SCRIPT_DIR; ; ) {
    add(path.join(dir, "node_modules", "playwright"));
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  for (const root of (process.env.NODE_PATH || "").split(path.delimiter)) {
    if (root) add(path.join(root, "playwright"));
  }
  try {
    const npm = execFileSync(
      process.platform === "win32" ? "npm.cmd" : "npm",
      ["root", "-g"],
      { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] },
    ).trim();
    add(path.join(npm, "playwright"));
  } catch {
    // npm missing: the other candidates still apply.
  }
  const npxCache = path.join(homedir(), ".npm", "_npx");
  if (existsSync(npxCache)) {
    for (const entry of readdirSync(npxCache).sort()) {
      add(path.join(npxCache, entry, "node_modules", "playwright"));
    }
  }

  const found = [];
  for (const dir of candidates) {
    const manifest = path.join(dir, "package.json");
    if (!existsSync(manifest)) continue;
    let meta;
    try {
      meta = JSON.parse(readFileSync(manifest, "utf8"));
    } catch {
      continue;
    }
    if (meta.name !== "playwright") continue;
    found.push({ dir, version: String(meta.version || "0.0.0") });
  }
  if (found.length === 0) {
    throw new Error(
      `playwright is not resolvable from ${SCRIPT_DIR}.\n` +
        "  prime the npx cache:  npx -y playwright --version\n" +
        "  or install it yourself and point KUMIHO_PLAYWRIGHT at the package",
    );
  }
  const isStable = (v) => !v.includes("-");
  found.sort(
    (a, b) =>
      Number(isStable(b.version)) - Number(isStable(a.version)) ||
      compareVersions(b.version, a.version),
  );
  const pick = found[0];
  const mod = loadPlaywright(pick.dir);
  if (!mod) throw new Error(`playwright at ${pick.dir} failed to load`);
  return { mod, dir: pick.dir, version: pick.version };
}

function loadPlaywright(dir) {
  try {
    const require = createRequire(path.join(dir, "package.json"));
    return require(dir);
  } catch {
    return null;
  }
}

function compareVersions(a, b) {
  const parts = (v) => v.split("-")[0].split(".").map((n) => parseInt(n, 10) || 0);
  const pa = parts(a);
  const pb = parts(b);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const d = (pa[i] || 0) - (pb[i] || 0);
    if (d !== 0) return d;
  }
  return 0;
}

// ---- harness access --------------------------------------------------------

async function preflight() {
  const probe = async (pathname) => {
    try {
      const res = await fetch(HARNESS_URL + pathname, {
        signal: AbortSignal.timeout(3000),
      });
      await res.arrayBuffer().catch(() => {});
      return res;
    } catch {
      return null;
    }
  };
  const root = await probe("");
  if (!root || !root.ok) {
    throw new Error(
      `the harness is not reachable at ${HARNESS_URL} — start it first:\n` +
        "  scripts/dev-gui-web.sh start",
    );
  }
  const dev = await probe("dev");
  if (!dev || !dev.ok) {
    throw new Error(
      `${HARNESS_URL} answers but has no dev panel — is this the kumiho ` +
        "gui web harness?\n  scripts/dev-gui-web.sh start",
    );
  }
}

async function postJSON(pathname, body) {
  const res = await fetch(HARNESS_URL + pathname, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(10000),
  });
  const text = await res.text();
  if (!res.ok) {
    throw new Error(
      `POST ${pathname} ${JSON.stringify(body)} -> ${res.status} ${text.trim()}`,
    );
  }
}

function postAction(name, payload) {
  return postJSON("action", { type: "action", name, payload: payload || {} });
}

// ---- the run ---------------------------------------------------------------

function assertOutsideRepo(dir) {
  const rel = path.relative(REPO_ROOT, dir);
  if (rel === "" || (!rel.startsWith("..") && !path.isAbsolute(rel))) {
    throw new Error(
      `refusing to capture into ${dir}: it is inside the repository.\n` +
        "  captures are never committed; use the default " +
        `${DEFAULT_CAPTURES_DIR} or another path outside the repo`,
    );
  }
}

function prepareDirs(plan) {
  const dirs = new Set(plan.map((item) => path.dirname(item.file)));
  for (const dir of dirs) {
    const full = path.join(CAPTURES_DIR, dir);
    mkdirSync(full, { recursive: true });
    for (const entry of readdirSync(full)) {
      if (entry.endsWith(".png")) rmSync(path.join(full, entry));
    }
  }
}

async function main() {
  const opts = parseArgs(process.argv.slice(2));
  if (opts.help) {
    usage();
    return;
  }
  const plan = buildPlan(opts.only);
  if (opts.list) {
    printManifest();
    console.log(`\n${plan.length} planned captures under ${CAPTURES_DIR}:`);
    for (const item of plan) console.log(`  ${item.file}`);
    return;
  }

  assertOutsideRepo(CAPTURES_DIR);
  const { mod: pw, dir: pwDir, version: pwVersion } = resolvePlaywright();
  console.log(`harness:  ${HARNESS_URL}`);
  console.log(`output:   ${CAPTURES_DIR}`);
  console.log(
    `playwright: ${pwVersion || "unknown version"} (${pwDir})`,
  );
  await preflight();
  prepareDirs(plan);

  let browser;
  try {
    browser = await pw.chromium.launch({ headless: true });
  } catch (err) {
    if (/executable doesn'?t exist|is not found|browserType.launch/i.test(err.message)) {
      throw new Error(
        `${err.message}\n  install the browser first: npx playwright install chromium`,
      );
    }
    throw err;
  }

  const problems = [];
  const results = [];
  let currentLabel = "startup";
  let current = { theme: null, accent: null };

  try {
    const context = await browser.newContext({
      viewport: VIEWPORT,
      deviceScaleFactor: DEVICE_SCALE_FACTOR,
      reducedMotion: "reduce",
      locale: "en-US",
      timezoneId: "UTC",
      // The diagnostics Copy details capture writes to the clipboard; grant
      // the permissions up front so the headless run is deterministic.
      permissions: ["clipboard-read", "clipboard-write"],
    });
    const page = await context.newPage();

    page.on("console", (msg) => {
      if (msg.type() !== "error") return;
      const url = msg.location().url || "";
      if (url.endsWith("/favicon.ico")) {
        console.warn(`  . ignored favicon console noise [${currentLabel}]`);
        return;
      }
      problems.push(`[${currentLabel}] console error: ${msg.text()}`);
      console.error(`  ! [${currentLabel}] console error: ${msg.text()}`);
    });
    page.on("pageerror", (err) => {
      problems.push(`[${currentLabel}] page error: ${err.message}`);
      console.error(`  ! [${currentLabel}] page error: ${err.message}`);
    });

    const navigate = async (target, what) => {
      try {
        await page.goto(target, { waitUntil: "load", timeout: NAV_TIMEOUT_MS });
      } catch (err) {
        throw new Error(
          `${what} failed: ${err.message}\n` +
            "  is the harness still running? start: scripts/dev-gui-web.sh start",
        );
      }
    };
    const reload = async () => {
      try {
        await page.reload({ waitUntil: "load", timeout: NAV_TIMEOUT_MS });
      } catch (err) {
        throw new Error(
          `reload failed: ${err.message}\n` +
            "  is the harness still running? start: scripts/dev-gui-web.sh start",
        );
      }
    };
    const settle = () => page.waitForTimeout(SETTLE_MS);

    // Appearance changes go through prefs_set (live in the harness: the next
    // index render carries the new theme CSS), so a change forces a reload;
    // a same-appearance open is a hash change only, which the SPA router sees.
    const open = async (route, theme, accent) => {
      if (theme !== current.theme || accent !== current.accent) {
        await postAction("prefs_set", { theme, accent });
        current = { theme, accent };
        await reload();
      }
      await page.evaluate((hash) => {
        if (location.hash !== hash) location.hash = hash;
      }, route);
    };

    const capture = async (item) => {
      await settle();
      const full = path.join(CAPTURES_DIR, item.file);
      await page.screenshot({ path: full, type: "png" });
      const bytes = statSync(full).size;
      results.push({ file: item.file, bytes });
      console.log(`  ${item.file} (${bytes} bytes)`);
    };

    const resetDevState = async () => {
      await postJSON("dev/latency", { ms: 0 });
      await postJSON("dev/fail", { on: false });
      await postJSON("dev/scenario", { scenario: "disconnected" });
    };

    // Baseline: a known appearance (light/blue), the onboarded gate lifted
    // (the wizard lives behind it since T3.4) and a clean scenario, so the
    // run is deterministic regardless of what the harness last served.
    await postAction("prefs_set", { theme: DEFAULT_THEME, accent: DEFAULT_ACCENT, onboarded: true });
    current = { theme: DEFAULT_THEME, accent: DEFAULT_ACCENT };
    await resetDevState();
    await navigate(HARNESS_URL, "initial navigation");

    console.log(`\n${plan.length} captures:`);
    for (const item of plan) {
      currentLabel = item.label;
      if (item.state) {
        const state = item.state;
        await resetDevState();
        if (state.scenario) {
          await postJSON("dev/scenario", { scenario: state.scenario });
        }
        if (state.latencyMs) {
          await postJSON("dev/latency", { ms: state.latencyMs });
        }
        if (state.failMessage) {
          await postJSON("dev/fail", { on: true, message: state.failMessage });
        }
        if (state.prefs) {
          await postAction("prefs_set", state.prefs);
        }
        await open(item.route, item.theme, item.accent);
        if (state.waitFor) {
          await page.waitForSelector(state.waitFor, { timeout: WAIT_TIMEOUT_MS });
        }
        if (state.emitLog) await postJSON("dev/log", { message: state.emitLog });
        if (state.action) {
          await postAction(state.action.name, state.action.payload);
        }
        if (state.fill) {
          await page.fill(state.fill.selector, state.fill.value);
        }
        for (const selector of state.click || []) {
          await page.click(selector);
        }
        if (state.waitAfter) {
          await page.waitForSelector(state.waitAfter, { timeout: WAIT_TIMEOUT_MS });
        }
        await capture(item);
        if (state.prefs) {
          // Restore the appearance baseline (the gate lifted) so later states
          // capture their own screens, not the wizard.
          await postAction("prefs_set", { onboarded: true });
        }
        if (state.afterMs) await page.waitForTimeout(state.afterMs);
      } else if (item.route === "/dev") {
        await navigate(HARNESS_URL + "dev", "dev panel navigation");
        await capture(item);
      } else {
        await open(item.route, item.theme, item.accent);
        if (item.wait) {
          await page.waitForSelector(item.wait, { timeout: WAIT_TIMEOUT_MS });
        }
        await capture(item);
      }
    }

    // Leave the harness as found, at the house-default appearance and a clean
    // scenario, so the next run (or a human at the panel) starts neutral.
    await postAction("prefs_set", { theme: "dark", accent: DEFAULT_ACCENT, onboarded: true });
    await resetDevState();
  } finally {
    await browser.close();
  }

  let total = 0;
  for (const r of results) {
    total += r.bytes;
    if (r.bytes < 512) {
      problems.push(`${r.file} is suspiciously small (${r.bytes} bytes)`);
    }
  }
  const expected = plan.length;
  if (results.length !== expected) {
    problems.push(`captured ${results.length} of ${expected} planned files`);
  }
  console.log(
    `\n${results.length}/${expected} captures, ${(total / 1024).toFixed(1)} KiB total`,
  );
  if (problems.length > 0) {
    console.error(`\n${problems.length} problem(s):`);
    for (const p of problems) console.error(`  ${p}`);
    throw new Error("capture run completed with errors; see above");
  }
  console.log("capture run clean");
}

main().catch((err) => {
  console.error(`\ncapture failed: ${err.message}`);
  process.exit(1);
});
