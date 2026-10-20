/* The Settings screen (inventory §Settings): the `Kumiho app` rows (theme,
 * accent, start at login, and the version row's manual update check per
 * D9/A3) above the nineteen `Background service` rows with their
 * immediate-vs-next-connect tooltips, `overridden` marks and Reset buttons,
 * the Refresh action row, and the status line.
 *
 * Everything renders from the store's state envelopes (the controller
 * projection); actions go through the transport and the screen re-renders
 * from the next envelope — "" means accepted, never completed (T1.7). The
 * daemon-row mutation guard is screen-side for the same reason as the
 * dashboard's (the state carries no busy flag, reported as a projection
 * gap): every service control is ignored while one change is in flight, and
 * the guard ends on the change's completion evidence — the settings notice
 * moving or the reloaded settings snapshot changing — or at the settle timer,
 * so a byte-identical repeat failure can never stick (T2.2's pattern). The
 * update check and the daemon mutations are mutually exclusive — a check
 * refuses while a change runs and vice versa — because both end their guard
 * on the one shared settings-notice slot (inventory §Settings: one change at
 * a time).
 *
 * Wording authority: the row labels, order, cues, section headings, the
 * `overridden`/Reset treatment, the status-line priority and the tooltip
 * seeds come from inventory §Settings and the walk-era row model
 * (settingsmodel.go at 282d63c); the tooltips are the walk model's hints,
 * sentence-cased. The update row is new in v0.3.0 (D9): the version is the
 * controller's UpdateInfo.Current, the check is manual, its result is the
 * controller's settings notice, and the release link goes through the frozen
 * open_external action.
 *
 * The five v0.4.0 rows (TLS split, HTTP proxy, HTTP proxy port, Solve
 * challenges, Keepalive profile) follow the TUI's row model
 * (internal/tui/settings.go): the same order, kinds and enum orders, with the
 * same hints, sentence-cased like the others.
 *
 * Current-choice marks read the appearance the page actually renders — the
 * server-injected theme block — not State.Theme/State.Accent, which hold the
 * shell's construction values: a live change reloads the page without
 * mutating them (T1.7/T1.10), so they go stale for the rest of the process
 * and would ring the wrong option after every change (reported to the
 * controller). The injected values cannot go stale: they are what the user
 * sees. `Start at login` reads the host's persisted gui.toml flag through
 * State.Autostart (T4.2: the shell seeds it and republishes it after every
 * prefs_set), with the click's own value on top and the page's session record
 * (sessionStorage) only as a fallback for hosts that do not project it (the
 * dev harness). The documented first-run default `on` (D11) remains the last
 * fallback, and the host's `Start at login saved` / failure notices arrive on
 * the shared settings status line below.
 *
 * Scrolling follows the inventory's deliberate treatment: the rows pane never
 * draws a scrollbar, the wheel moves it three rows per notch, and Tab moves
 * focus through every row and brings it into view.
 *
 * The router links css/screens/settings.css from the registry entry.
 */

// The mutation guard's settle (the dashboard/locations T2.2 pattern): a
// change whose completion evidence repeats byte-for-byte stops blocking
// after this long.
const SETTLE_MS = 3000;

// Wheel/scroll metrics: the house 40px control rows, three rows per notch,
// and the trackpad threshold (smaller deltas scroll natively).
const ROW_PX = 40;
const WHEEL_ROWS = 3;
const WHEEL_NOTCH_MIN = 40;

// The `Kumiho app` rows (inventory §Settings table plus the D9 update row).
const THEME_OPTIONS = [
  ["light", "Light"],
  ["dark", "Dark"],
  ["oled", "OLED"],
];
const ACCENT_OPTIONS = [
  ["blue", "Blue"],
  ["teal", "Teal"],
  ["green", "Green"],
  ["orange", "Orange"],
  ["rose", "Rose"],
  ["violet", "Violet"],
];
const AUTOSTART_KEY = "kumiho.gui.autostart";

// The `Background service` rows in the inventory's canonical order: key,
// label, control kind, enum order, cue and tooltip (the walk row model's
// hints, sentence-cased; the inventory lists all 19 rows).
const SERVICE_ROWS = [
  { key: "kill_switch", label: "Kill switch", kind: "toggle",
    hint: "Block all tunnel traffic while the tunnel is down; applies immediately" },
  { key: "autoconnect", label: "Autoconnect", kind: "toggle",
    hint: "Connect automatically when the daemon starts" },
  { key: "log_level", label: "Log level", kind: "cycle", options: ["debug", "info", "warn", "error"],
    hint: "Minimum level recorded into the log ring; applies immediately" },
  { key: "redact_targets", label: "Redact targets", kind: "toggle",
    hint: "Mask hostnames and IPs in log messages; applies immediately" },
  { key: "exit_check", label: "Exit check", kind: "toggle",
    hint: "Probe the public exit IP after connecting; next connect" },
  { key: "doh_provider", label: "DoH provider", kind: "cycle",
    options: ["automatic", "cloudflare", "google", "quad9", "off"],
    hint: "In-tunnel DNS-over-HTTPS resolver; next connect" },
  { key: "dns_upstream", label: "DNS upstream", kind: "text", cue: "(provider default)",
    hint: "Resolver IP for DoH; empty = provider default; next connect" },
  { key: "edge_address", label: "Edge address", kind: "text", cue: "(resolve)",
    hint: "Pin the Fastly edge IP; empty = resolve normally; next connect" },
  { key: "upstream_proxy", label: "Upstream proxy", kind: "text", cue: "(direct)",
    hint: "Chain the edge connection through a proxy URL; empty = direct; next connect" },
  { key: "exclude_cidrs", label: "Exclude CIDRs", kind: "text", cue: "(none)",
    hint: "Comma-separated ranges that bypass the tunnel; next connect" },
  { key: "allow_lan", label: "Allow LAN", kind: "toggle",
    hint: "Keep the local subnet outside the tunnel; next connect" },
  { key: "mtu", label: "MTU", kind: "number",
    hint: "Tun device MTU; next connect" },
  { key: "socks_port", label: "SOCKS port", kind: "number",
    hint: "Loopback SOCKS5 listener; next connect" },
  { key: "quota_poll_minutes", label: "Quota poll", kind: "number",
    hint: "Minutes between quota refreshes while connected; next connect" },
  { key: "tls_split", label: "TLS split", kind: "cycle",
    options: ["off", "first-byte", "sni"],
    hint: "Split the TLS ClientHello across writes; next connect" },
  { key: "http_proxy", label: "HTTP proxy", kind: "toggle",
    hint: "Loopback HTTP/CONNECT proxy through the tunnel; next connect" },
  { key: "http_proxy_port", label: "HTTP proxy port", kind: "number",
    hint: "Loopback HTTP proxy listener; next connect" },
  { key: "solve_challenges", label: "Solve challenges", kind: "toggle",
    hint: "Solve FxA and edge challenges automatically; applies immediately" },
  { key: "keepalive_profile", label: "Keepalive profile", kind: "cycle",
    options: ["standard", "slow"],
    hint: "Keepalive ping timeout profile; next connect" },
];

// The six house accents, as the sRGB hue of their primary colour (the house
// table's documented primary values; each accent's light and dark variants
// sit within a few degrees). The applied --primary is matched to the nearest
// reference; the references are >= ~20deg apart, so the mapping is
// unambiguous. The colours themselves are never restated here — only read
// back from the page (ban 9).
const ACCENT_HUES = [
  ["teal", 180],
  ["green", 125],
  ["blue", 213],
  ["violet", 264],
  ["rose", 355],
  ["orange", 19],
];

// unmount()'s teardown, assigned by mount (the router mounts one screen at a
// time, so one slot is enough).
let cleanup = null;

function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function heading(text) {
  return el("h2", "set__heading", text);
}

/** "#rgb"/"#rrggbb"/"rgb(…)" to [r, g, b], or null. */
function parseColor(value) {
  const text = String(value || "").trim();
  let m = /^#([0-9a-f]{3})$/i.exec(text);
  if (m) {
    return [
      parseInt(m[1][0], 16) * 17,
      parseInt(m[1][1], 16) * 17,
      parseInt(m[1][2], 16) * 17,
    ];
  }
  m = /^#([0-9a-f]{6})$/i.exec(text);
  if (m) {
    return [
      parseInt(m[1].slice(0, 2), 16),
      parseInt(m[1].slice(2, 4), 16),
      parseInt(m[1].slice(4, 6), 16),
    ];
  }
  m = /^rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)/.exec(text);
  if (m) return [Number(m[1]), Number(m[2]), Number(m[3])];
  return null;
}

/** The hue (0..360) of an RGB triple. */
function rgbHue(rgb) {
  const r = rgb[0] / 255;
  const g = rgb[1] / 255;
  const b = rgb[2] / 255;
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const d = max - min;
  if (d === 0) return 0;
  let h;
  if (max === r) h = 60 * (((g - b) / d) % 6);
  else if (max === g) h = 60 * ((b - r) / d + 2);
  else h = 60 * ((r - g) / d + 4);
  return (h + 360) % 360;
}

/**
 * The appearance the page actually renders: the theme from the injected
 * color-scheme (Light is the only light theme; of the two dark ones only
 * OLED's background is pure black) and the accent from the applied
 * --primary's hue. Unreadable values come back empty and the caller falls
 * back to the state's names.
 */
function appliedAppearance() {
  const out = { theme: "", accent: "" };
  try {
    const root = getComputedStyle(document.documentElement);
    const scheme = root.colorScheme;
    if (scheme === "light") {
      out.theme = "light";
    } else if (scheme === "dark") {
      const bg = parseColor(getComputedStyle(document.body).backgroundColor);
      out.theme = bg && bg[0] === 0 && bg[1] === 0 && bg[2] === 0 ? "oled" : "dark";
    }
    const primary = parseColor(root.getPropertyValue("--primary"));
    if (primary) {
      const hue = rgbHue(primary);
      let best = "";
      let bestDist = 361;
      for (const [name, ref] of ACCENT_HUES) {
        let dist = Math.abs(hue - ref);
        if (dist > 180) dist = 360 - dist;
        if (dist < bestDist) {
          bestDist = dist;
          best = name;
        }
      }
      out.accent = best;
    }
  } catch (err) {
    // Fall through to the state fallback.
  }
  return out;
}

/** Whether two release tags name the same version (leading "v" ignored). */
function sameRelease(a, b) {
  const norm = (v) => String(v || "").trim().toLowerCase().replace(/^v/, "");
  const x = norm(a);
  return x !== "" && x === norm(b);
}

function storedAutostart() {
  try {
    const v = sessionStorage.getItem(AUTOSTART_KEY);
    return v === "on" || v === "off" ? v : "";
  } catch (err) {
    return "";
  }
}

function storeAutostart(v) {
  try {
    sessionStorage.setItem(AUTOSTART_KEY, v);
  } catch (err) {
    // Session storage is a convenience; the click's value still shows.
  }
}

/**
 * The host's persisted Start at login value from State.Autostart (T4.2):
 * "on"/"off", or "" when the host does not project it (the harness). This is
 * authoritative over the page's session record.
 */
function projectedAutostart(snapshot) {
  if (snapshot && snapshot.autostart === true) return "on";
  if (snapshot && snapshot.autostart === false) return "off";
  return "";
}

/** The daemon setting's display/effective value from a settings snapshot. */
function settingValue(settings, key) {
  switch (key) {
    case "kill_switch":
      return settings.kill_switch_effective ? "on" : "off";
    case "autoconnect":
      return settings.autoconnect_effective ? "on" : "off";
    case "log_level":
      return settings.log_level || "";
    case "redact_targets":
      return settings.redact_targets ? "on" : "off";
    case "exit_check":
      return settings.exit_check ? "on" : "off";
    case "doh_provider":
      return settings.doh_provider || "";
    case "dns_upstream":
      return settings.dns_upstream || "";
    case "edge_address":
      return settings.edge_address || "";
    case "upstream_proxy":
      return settings.upstream_proxy || "";
    case "exclude_cidrs":
      return (settings.exclude_cidrs || []).join(", ");
    case "allow_lan":
      return settings.allow_lan ? "on" : "off";
    case "mtu":
      return String(settings.mtu);
    case "socks_port":
      return String(settings.socks_port);
    case "quota_poll_minutes":
      return String(settings.quota_poll_minutes);
    case "tls_split":
      return settings.tls_split || "";
    case "http_proxy":
      return settings.http_proxy ? "on" : "off";
    case "http_proxy_port":
      return String(settings.http_proxy_port);
    case "solve_challenges":
      return settings.solve_challenges ? "on" : "off";
    case "keepalive_profile":
      return settings.keepalive_profile || "";
  }
  return "";
}

/** A cheap identity for the settings snapshot (see syncService). */
function settingsSignature(settings) {
  return settings ? JSON.stringify(settings) : "";
}

/** The notice identity used by the mutation guards. */
function noticeMark(notice) {
  return notice && notice.message ? notice.message + "\u0000" + (notice.tone || "") : "";
}

/** Build one app-section row: label, control area, no override treatment. */
function appRow(labelText, hint, control) {
  const row = el("div", "set__row");
  row.title = hint;
  const label = el("span", "set__label", labelText);
  const area = el("div", "set__control");
  area.append(control);
  row.append(label, area);
  return { row: row, control: area };
}

/** Build one choice-button group: accessible names `<Row>: <Option>`. */
function choiceGroup(labelText, options, onChoose) {
  const group = el("div", "set__choices");
  group.setAttribute("role", "group");
  group.setAttribute("aria-label", labelText);
  const buttons = new Map();
  for (const [value, text] of options) {
    const button = el("button", "button button--tonal set__choice", text);
    button.type = "button";
    button.dataset.value = value;
    button.setAttribute("aria-label", labelText + ": " + text);
    button.addEventListener("click", () => onChoose(value));
    group.append(button);
    buttons.set(value, button);
  }
  return { group: group, buttons: buttons };
}

/** Build the screen's DOM and return its element references. */
function build(host, handlers) {
  const root = el("div", "set");

  const refresh = el("button", "button button--tonal set__refresh", "Refresh");
  refresh.type = "button";
  const actions = el("div", "set__actions");
  actions.append(refresh);

  const pane = el("div", "set__rows");
  pane.hidden = true;

  const appSection = el("div", "set__section");
  appSection.append(heading("Kumiho app"));

  // Theme row.
  const theme = choiceGroup("Theme", THEME_OPTIONS, handlers.onTheme);
  const themeRow = appRow("Theme", "Kumiho's color theme; changes immediately", theme.group);
  appSection.append(themeRow.row);

  // Accent row.
  const accent = choiceGroup("Accent", ACCENT_OPTIONS, handlers.onAccent);
  const accentRow = appRow("Accent", "Kumiho's accent color; changes immediately", accent.group);
  appSection.append(accentRow.row);

  // Start at login row.
  const autostart = el("button", "button button--tonal set__toggle", "on");
  autostart.type = "button";
  autostart.addEventListener("click", handlers.onAutostart);
  const autostartRow = appRow(
    "Start at login",
    "Start Kumiho in the tray when you sign in to Windows",
    autostart
  );
  appSection.append(autostartRow.row);

  // Version and the manual update check (D9).
  const version = el("span", "set__version");
  const updateCheck = el("button", "button button--tonal set__update-check", "Check for updates");
  updateCheck.type = "button";
  updateCheck.addEventListener("click", handlers.onUpdateCheck);
  const updateResult = el("span", "set__update-result");
  updateResult.hidden = true;
  const updateLink = el("button", "button button--text set__update-link", "Open release page");
  updateLink.type = "button";
  updateLink.hidden = true;
  updateLink.addEventListener("click", handlers.onUpdateLink);
  const updateArea = el("div", "set__control set__control--wrap");
  updateArea.append(version, updateCheck, updateResult, updateLink);
  const versionRow = appRow(
    "Version",
    "The running Kumiho version; check the GitHub releases page for updates",
    updateArea
  );
  appSection.append(versionRow.row);

  // The Background service rows in the inventory's canonical order.
  const serviceSection = el("div", "set__section");
  serviceSection.append(heading("Background service"));
  const serviceRows = [];
  for (const spec of SERVICE_ROWS) {
    const row = el("div", "set__row");
    row.title = spec.hint;
    const label = el("span", "set__label", spec.label);
    const area = el("div", "set__control");
    const entry = { spec: spec, row: row, value: "" };
    if (spec.kind === "toggle" || spec.kind === "cycle") {
      const button = el("button", "button button--tonal set__toggle");
      button.type = "button";
      button.addEventListener("click", () => handlers.onActivate(entry));
      entry.button = button;
      area.append(button);
    } else {
      const input = document.createElement("input");
      input.className = "input set__edit";
      input.type = spec.kind === "number" ? "number" : "text";
      input.autocomplete = "off";
      input.spellcheck = false;
      input.id = "set-" + spec.key;
      input.placeholder = spec.cue || "";
      input.setAttribute("aria-label", spec.label);
      input.addEventListener("keydown", (event) => {
        if (event.key === "Enter") {
          event.preventDefault();
          handlers.onApply(entry);
        }
      });
      const apply = el("button", "button button--tonal set__apply", "Apply");
      apply.type = "button";
      apply.setAttribute("aria-label", "Apply " + spec.label);
      apply.addEventListener("click", () => handlers.onApply(entry));
      entry.input = input;
      entry.apply = apply;
      area.append(input, apply);
    }
    const mark = el("span", "set__override", "overridden");
    mark.hidden = true;
    const reset = el("button", "button button--tonal set__reset", "Reset");
    reset.type = "button";
    reset.setAttribute("aria-label", "Reset " + spec.label);
    reset.hidden = true;
    reset.addEventListener("click", () => handlers.onReset(entry));
    entry.mark = mark;
    entry.reset = reset;
    row.append(label, area, mark, reset);
    serviceSection.append(row);
    serviceRows.push(entry);
  }

  pane.append(appSection, serviceSection);

  const status = el("p", "set__status");
  status.setAttribute("aria-live", "polite");
  status.hidden = true;

  root.append(actions, pane, status);
  host.append(root);

  return {
    root: root,
    refresh: refresh,
    pane: pane,
    themeButtons: theme.buttons,
    accentButtons: accent.buttons,
    autostart: autostart,
    version: version,
    updateCheck: updateCheck,
    updateResult: updateResult,
    updateLink: updateLink,
    serviceRows: serviceRows,
    status: status,
  };
}

export default {
  mount(host, ctx) {
    // Per-mount view state. The store hands parsed snapshots (a fresh object
    // graph per envelope), so identity checks use signatures, never
    // references.
    const view = {
      busy: false,
      busyMark: "",
      busyTimer: 0,
      updateBusy: false,
      updateMark: "",
      updateTimer: 0,
      settingsMark: null,
      // The click's own choice, shown until the page re-reads the applied
      // appearance (theme/accent) or the session ends (autostart).
      picked: { theme: "", accent: "", autostart: "" },
      statusText: null,
      statusTone: null,
    };

    function setBusyTimer(which) {
      if (which === "update") {
        clearTimeout(view.updateTimer);
        view.updateTimer = setTimeout(function () {
          view.updateTimer = 0;
          if (view.updateBusy) {
            endUpdate();
            render();
          }
        }, SETTLE_MS);
      } else {
        clearTimeout(view.busyTimer);
        view.busyTimer = setTimeout(function () {
          view.busyTimer = 0;
          if (view.busy) {
            endBusy();
            render();
          }
        }, SETTLE_MS);
      }
    }

    // beginBusy/endBusy guard the daemon-row mutations: one change at a time,
    // every service control ignored until it settles (inventory §Settings).
    function beginBusy() {
      view.busy = true;
      view.busyMark = fingerprint(ctx.store.get());
      setBusyTimer("mutate");
      render();
    }

    function endBusy() {
      view.busy = false;
      clearTimeout(view.busyTimer);
      view.busyTimer = 0;
    }

    function beginUpdate() {
      view.updateBusy = true;
      view.updateMark = updateFingerprint(ctx.store.get());
      setBusyTimer("update");
      render();
    }

    function endUpdate() {
      view.updateBusy = false;
      clearTimeout(view.updateTimer);
      view.updateTimer = 0;
    }

    // The daemon mutation's completion evidence: the settings notice moving
    // (success or failure) or the reloaded snapshot changing (values/marks).
    // A byte-identical repeat moves neither and ends at the settle timer.
    function fingerprint(snapshot) {
      return (
        noticeMark(snapshot.settings_notice) + "\u0000" + settingsSignature(snapshot.settings)
      );
    }

    // The update check's completion evidence: the settings notice (its
    // result) or the Update projection changing.
    function updateFingerprint(snapshot) {
      return noticeMark(snapshot.settings_notice) + "\u0000" + JSON.stringify(snapshot.update || null);
    }

    function run(name, payload) {
      return ctx.send(name, payload).then(function (err) {
        if (err) console.warn("kumiho: settings " + name + " refused: " + err);
        return err;
      });
    }

    function render() {
      const snapshot = ctx.store.get();
      if (view.busy && fingerprint(snapshot) !== view.busyMark) endBusy();
      if (view.updateBusy && updateFingerprint(snapshot) !== view.updateMark) endUpdate();
      renderActions();
      renderPane(snapshot);
      renderStatus(snapshot);
    }

    function renderActions() {
      // Both guards are visible on the update button: while either an update
      // check or a daemon change is in flight the check is ignored, and the
      // button says so (since the mutual refusals below are silent).
      ui.updateCheck.disabled = view.updateBusy || view.busy;
    }

    function renderPane(snapshot) {
      const settings = snapshot.settings || null;
      // The rows pane is hidden entirely until the first successful snapshot
      // (inventory §Settings "Rows pane states").
      const hidden = !settings;
      if (ui.pane.hidden !== hidden) ui.pane.hidden = hidden;
      renderApp(snapshot);
      if (settings && settingsSignature(settings) !== view.settingsMark) {
        view.settingsMark = settingsSignature(settings);
        syncService(settings);
      }
      renderServiceDisabled();
    }

    /**
     * The appearance marks: the page's applied values (which survive the
     * shell's reload) with the click's own choice on top, and the state's
     * names as the last-resort fallback.
     */
    function renderApp(snapshot) {
      const applied = appliedAppearance();
      const theme = view.picked.theme || applied.theme || snapshot.theme || "";
      const accent = view.picked.accent || applied.accent || snapshot.accent || "";
      for (const [value, button] of ui.themeButtons) {
        const selected = value === theme;
        button.setAttribute("aria-pressed", selected ? "true" : "false");
      }
      for (const [value, button] of ui.accentButtons) {
        const selected = value === accent;
        button.setAttribute("aria-pressed", selected ? "true" : "false");
      }

      const autostart =
        view.picked.autostart || projectedAutostart(snapshot) || storedAutostart() || "on";
      ui.autostart.textContent = autostart;
      ui.autostart.setAttribute("aria-label", "Start at login: " + autostart);

      const up = snapshot.update || {};
      ui.version.textContent = up.current || "";
      if (up.checked_at && up.latest && !sameRelease(up.current, up.latest)) {
        ui.updateResult.textContent = "Kumiho " + up.latest + " is available";
        ui.updateResult.hidden = false;
        ui.updateLink.hidden = !up.url;
      } else if (up.checked_at) {
        ui.updateResult.textContent = "Kumiho is up to date";
        ui.updateResult.hidden = false;
        ui.updateLink.hidden = true;
      } else {
        ui.updateResult.hidden = true;
        ui.updateLink.hidden = true;
      }
    }

    /** Push one settings snapshot into the service rows (values and marks). */
    function syncService(settings) {
      const overridden = {};
      for (const key of settings.overridden || []) overridden[key] = true;
      for (const entry of ui.serviceRows) {
        const value = settingValue(settings, entry.spec.key);
        entry.value = value;
        if (entry.button) {
          entry.button.textContent = value;
          entry.button.setAttribute("aria-label", entry.spec.label + ": " + value);
        }
        if (entry.input && entry.input.value !== value) entry.input.value = value;
        const marked = !!overridden[entry.spec.key];
        entry.mark.hidden = !marked;
        entry.reset.hidden = !marked;
      }
    }

    /**
     * Enable/disable the service controls with the mutation guard: every
     * daemon-row control is ignored while one change is in flight
     * (inventory §Settings).
     */
    function renderServiceDisabled() {
      for (const entry of ui.serviceRows) {
        if (entry.button) entry.button.disabled = view.busy;
        if (entry.input) entry.input.disabled = view.busy;
        if (entry.apply) entry.apply.disabled = view.busy;
        if (entry.reset) entry.reset.disabled = view.busy;
      }
    }

    function renderStatus(snapshot) {
      const notice = snapshot.settings_notice;
      const error = notice && notice.tone === "bad" ? notice.message : "";
      let text = "";
      let tone = "";
      if (view.busy) {
        text = "Applying...";
      } else if (view.updateBusy) {
        text = "Checking for updates...";
      } else if (!snapshot.settings && !error) {
        text = "Loading settings...";
      } else if (error) {
        text = error;
        tone = "bad";
      } else if (notice && notice.message) {
        text = notice.message;
        if (notice.tone === "good") tone = "good";
        else if (notice.tone === "warn") tone = "warn";
      }
      if (text === view.statusText && tone === view.statusTone) return;
      view.statusText = text;
      view.statusTone = tone;
      ui.status.textContent = text;
      ui.status.title = text;
      if (tone) ui.status.dataset.tone = tone;
      else delete ui.status.dataset.tone;
      ui.status.hidden = text === "";
    }

    /**
     * One daemon-row mutation through the busy guard. A mutation is refused
     * while an update check is in flight as well: the two share the settings
     * notice slot, so letting them overlap would let one completion release
     * the other's guard (inventory §Settings: one change at a time).
     */
    function mutate(payload) {
      if (view.busy || view.updateBusy) return;
      beginBusy();
      run("settings_set", payload).then(function (err) {
        if (!err) return;
        endBusy();
        render();
      });
    }

    const handlers = {
      onTheme(value) {
        view.picked.theme = value;
        render();
        run("prefs_set", { theme: value }).then(function (err) {
          if (!err) return;
          view.picked.theme = "";
          render();
        });
      },

      onAccent(value) {
        view.picked.accent = value;
        render();
        run("prefs_set", { accent: value }).then(function (err) {
          if (!err) return;
          view.picked.accent = "";
          render();
        });
      },

      onAutostart() {
        const current =
          view.picked.autostart || projectedAutostart(ctx.store.get()) || storedAutostart() || "on";
        const next = current === "on" ? false : true;
        view.picked.autostart = next ? "on" : "off";
        storeAutostart(view.picked.autostart);
        render();
        run("prefs_set", { autostart: next }).then(function (err) {
          if (!err) return;
          // A host refusal means nothing was saved: show the prior value
          // again (the session record included).
          view.picked.autostart = current;
          storeAutostart(current);
          render();
        });
      },

      onUpdateCheck() {
        // The check is refused while a daemon change is in flight (the
        // mutation guard's mirror; see mutate).
        if (view.updateBusy || view.busy) return;
        beginUpdate();
        run("update_check", {}).then(function (err) {
          if (!err) return;
          endUpdate();
          render();
        });
      },

      onUpdateLink() {
        const url = (ctx.store.get().update || {}).url;
        if (!url) return;
        run("open_external", { url: url });
      },

      onActivate(entry) {
        if (view.busy || view.updateBusy) return;
        const spec = entry.spec;
        if (spec.kind === "toggle") {
          mutate({ key: spec.key, value: entry.value === "on" ? "off" : "on" });
          return;
        }
        const index = spec.options.indexOf(entry.value);
        const next = spec.options[(index + 1) % spec.options.length];
        mutate({ key: spec.key, value: next });
      },

      onApply(entry) {
        // The row's raw text; the daemon validates (inventory §Settings).
        mutate({ key: entry.spec.key, value: entry.input.value });
      },

      onReset(entry) {
        mutate({ key: entry.spec.key, reset: true });
      },
    };

    const ui = build(host, handlers);

    // Refresh restores every row's stored value (the walk screen's rule):
    // re-sync from the last known snapshot immediately, then ask for a fresh
    // one.
    function onRefresh() {
      view.settingsMark = null;
      render();
      run("settings_get", {});
    }

    // The rows pane's deliberate scrolling: three rows per notch, small
    // deltas (trackpads) native, and Tab revealing the focused row.
    function onWheel(event) {
      if (Math.abs(event.deltaY) < WHEEL_NOTCH_MIN) return;
      event.preventDefault();
      ui.pane.scrollTop += (event.deltaY > 0 ? 1 : -1) * WHEEL_ROWS * ROW_PX;
    }

    function onFocusIn(event) {
      if (!ui.pane.contains(event.target)) return;
      event.target.scrollIntoView({ block: "nearest" });
    }

    ui.refresh.addEventListener("click", onRefresh);
    ui.pane.addEventListener("wheel", onWheel, { passive: false });
    ui.pane.addEventListener("focusin", onFocusIn);

    const unsubscribe = ctx.store.subscribe(render);

    cleanup = function () {
      unsubscribe();
      clearTimeout(view.busyTimer);
      clearTimeout(view.updateTimer);
    };
  },

  unmount() {
    if (cleanup) cleanup();
    cleanup = null;
  },
};
