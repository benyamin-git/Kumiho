/* The Dashboard screen (inventory §Dashboard, D13): the Connect/Disconnect +
 * Options + Refresh action row, the Connect options popover, the eleven-row
 * status card with its quota bar, and the persistent notice row with its
 * conditional Retry.
 *
 * Everything renders from the store's state envelopes (the controller
 * projection); actions go through the transport and the screen re-renders
 * from the next envelope — "" means accepted, never completed (T1.7). The
 * primary button's busy guard therefore holds from the click until the
 * controller's completion signals arrive — the shared notice, the
 * reachability gate or the first-status error — and, as the safety net for
 * an action that changes nothing at all (an identical repeat failure: the
 * notice repeats byte-for-byte), until a short settle after the click. The
 * daemon's transitional state word (CONNECTING, RECONNECTING, …) is
 * deliberately NOT a release signal: it arrives while the action is still in
 * flight, and the inventory keeps the primary disabled until the action
 * settles. The controller exposes no busy flag and the State carries none;
 * this screen-side guard is the closest the data allows (reported as a
 * projection gap, T2.2).
 *
 * The router links the screen's stylesheet (css/screens/dashboard.css, from
 * the registry entry) on mount relative to the document — the shell's
 * per-run token path stays intact and CSP default-src 'self' allows the
 * same-origin link.
 *
 * Wording authority: the button label rule, the Retry rule, and the row order
 * come from inventory §Dashboard; connectionStatus (store.js) supplies the
 * state word the header shows — the state word lives in the shell header, not
 * on this screen.
 */

import { t, formatBytes, formatDate, formatDateTime, formatDuration, noticeText } from "../i18n.js";
import { flagIcon } from "../flags.js";

// The busy guard's settle: an action whose signals repeat byte-for-byte (an
// identical repeat failure) stops blocking after this long (see the file
// header).
const BUSY_SETTLE_MS = 3000;

// The status card's rows in the inventory's canonical order.
const ROWS = [
  ["uptime", "dashboard.row.uptime"],
  ["location", "dashboard.row.location"],
  ["exit_ip", "dashboard.row.exit_ip"],
  ["rates", "dashboard.row.rates"],
  ["totals", "dashboard.row.totals"],
  ["kill_switch", "dashboard.row.kill_switch"],
  ["autoconnect", "dashboard.row.autoconnect"],
  ["token_expiry", "dashboard.row.token_expiry"],
  ["pass_expiry", "dashboard.row.pass_expiry"],
  ["quota", "dashboard.row.quota"],
  ["quota_reset", "dashboard.row.quota_reset"],
];

// The raw controller states whose primary label is `Disconnect` (inventory
// §Dashboard action row). Every other state — IDLE, FATAL, UNAUTHENTICATED,
// and the pre-status/unreachable words — keeps `Connect`.
const DISCONNECT_STATES = [
  "CONNECTED",
  "PROXY_ONLY",
  "CONNECTING",
  "RECONNECTING",
  "WAITING_NETWORK",
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

/** Build the screen's DOM and return its element references. */
function build(host) {
  const root = el("div", "dash");

  const primary = el("button", "button button--filled dash__primary", t("dashboard.connect"));
  primary.type = "button";

  // The Connect options disclosure (D13): a plain aria-expanded + controls
  // trigger, not a menu — the popover holds a small form, and Tab/Esc are its
  // whole keyboard story.
  const trigger = el("button", "button button--tonal", t("dashboard.options"));
  trigger.type = "button";
  trigger.setAttribute("aria-expanded", "false");
  trigger.setAttribute("aria-controls", "dash-connect-options");

  const popover = el("div", "dash__options");
  popover.id = "dash-connect-options";
  popover.setAttribute("role", "group");
  popover.setAttribute("aria-label", t("dashboard.connect_options_aria"));
  popover.hidden = true;

  const locationLabel = el("label", "field__label", t("dashboard.location_label"));
  locationLabel.htmlFor = "dash-opt-location";
  const location = document.createElement("select");
  location.className = "input";
  location.id = "dash-opt-location";
  const locationFlag = el("span", "dash__flag");
  locationFlag.setAttribute("aria-hidden", "true");
  locationFlag.hidden = true;
  const locationControl = el("div", "dash__location");
  locationControl.append(location, locationFlag);
  const locationField = el("div", "field");
  locationField.append(locationLabel, locationControl);

  const proxyInput = document.createElement("input");
  proxyInput.type = "checkbox";
  proxyInput.id = "dash-opt-proxy";
  const proxyLabel = el("label", null);
  proxyLabel.htmlFor = "dash-opt-proxy";
  proxyLabel.append(t("dashboard.proxy_only"), el("span", "dash__hint", t("dashboard.proxy_hint")));
  const proxyRow = el("div", "dash__check");
  proxyRow.append(proxyInput, proxyLabel);

  const killGroup = document.createElement("fieldset");
  killGroup.className = "dash__group";
  const killLegend = el("legend", "field__label", t("dashboard.kill_switch"));
  const killRadios = [];
  const killOptions = el("div", "dash__radios");
  for (const [value, text] of [["on", t("common.on")], ["off", t("common.off")], ["last", t("common.last")]]) {
    const wrap = el("label", "dash__radio");
    const input = document.createElement("input");
    input.type = "radio";
    input.name = "dash-kill-switch";
    input.value = value;
    wrap.append(input, text);
    killOptions.append(wrap);
    killRadios.push(input);
  }
  const killHint = el("span", "dash__hint", t("dashboard.kill_switch_hint"));
  killGroup.append(killLegend, killOptions, killHint);

  const optConnect = el("button", "button button--filled", t("dashboard.connect"));
  optConnect.type = "button";

  popover.append(locationField, proxyRow, killGroup, optConnect);

  const menu = el("div", "dash__menu");
  menu.append(trigger, popover);

  const refresh = el("button", "button button--tonal dash__refresh", t("common.refresh"));
  refresh.type = "button";

  const actions = el("div", "dash__actions");
  actions.append(primary, menu, refresh);

  const card = document.createElement("dl");
  card.className = "card dash__card";
  const rows = new Map();
  for (const [key, labelKey] of ROWS) {
    const dt = el("dt", null, t(labelKey));
    const dd = document.createElement("dd");
    const value = el("span", "dash__value");
    dd.append(value);
    let bar = null;
    let fill = null;
    if (key === "quota") {
      bar = el("div", "dash__bar");
      bar.setAttribute("role", "progressbar");
      bar.setAttribute("aria-label", t("dashboard.quota_aria"));
      fill = el("div", "dash__bar-fill");
      bar.append(fill);
      bar.hidden = true;
      dd.append(bar);
    }
    // Rows are always present in canonical order and hidden while empty.
    dt.hidden = true;
    dd.hidden = true;
    card.append(dt, dd);
    rows.set(key, { dt: dt, dd: dd, value: value, bar: bar, fill: fill });
  }

  const notice = el("p", "dash__notice");
  notice.setAttribute("aria-live", "polite");
  notice.hidden = true;
  const retry = el("button", "button button--tonal dash__retry", t("common.retry"));
  retry.type = "button";
  retry.hidden = true;
  const guide = el("button", "button button--tonal dash__retry", t("dashboard.blocked_region_guide"));
  guide.type = "button";
  guide.hidden = true;
  guide.addEventListener("click", function () {
    ctx.navigate("blocked-region");
  });
  const noticeRow = el("div", "dash__notice-row");
  noticeRow.append(notice, retry, guide);
  const details = el("p", "dash__details");
  details.dir = "ltr";
  details.hidden = true;

  root.append(actions, card, noticeRow, details);
  host.append(root);

  return {
    root: root,
    primary: primary,
    trigger: trigger,
    popover: popover,
    location: location,
    locationFlag: locationFlag,
    proxyInput: proxyInput,
    killRadios: killRadios,
    optConnect: optConnect,
    refresh: refresh,
    rows: rows,
    notice: notice,
    retry: retry,
    guide: guide,
    details: details,
  };
}

/** "CC" or "CC (server)" (inventory §Dashboard Location row). */
function locationText(status) {
  if (!status.location) return "";
  return status.server ? status.location + " (" + status.server + ")" : status.location;
}

/** "IP" or "IP (country)" (inventory §Dashboard Exit IP row). */
function exitText(status) {
  if (!status.exit_ip) return "";
  return status.exit_country ? status.exit_ip + " (" + status.exit_country + ")" : status.exit_ip;
}

/** A cheap identity for the served country list (see rebuildLocations). */
function locationsSignature(locations) {
  if (!locations) return "";
  let signature = "";
  for (const country of locations) signature += country.code + "\u0000" + country.name + "\u0001";
  return signature;
}

export default {
  mount(host, ctx) {
    const ui = build(host);

    // Per-mount view state. The store hands parsed snapshots (a fresh object
    // graph per envelope), so identity checks use signatures, never
    // references.
    const view = {
      busy: false,
      busyMark: "",
      busyTimer: 0,
      disconnect: false,
      open: false,
      locationsSignature: null,
      locations: null,
      options: {
        location: "",
        proxyOnly: false,
        killSwitch: "",
        killSwitchTouched: false,
        killSwitchBase: "",
      },
    };

    // The controller's connect/disconnect completion signals: the shared
    // notice, the reachability gate and the first-status error. Everything
    // else on this screen ticks (uptime) or is the action itself, so one of
    // these bytes changing is what ends the busy guard early; the settle
    // timer covers an action that moves none of them. The daemon's
    // transitional state word is deliberately excluded — it moves while the
    // action is still in flight (inventory §Dashboard keeps the primary
    // disabled until the action settles).
    function fingerprint(snapshot) {
      const notice = snapshot.notice || {};
      return [
        notice.message || "",
        notice.tone || "",
        snapshot.reachable === true,
        snapshot.status_error || "",
      ].join("\u0000");
    }

    function endBusy() {
      view.busy = false;
      clearTimeout(view.busyTimer);
      view.busyTimer = 0;
    }

    function beginBusy() {
      view.busy = true;
      view.busyMark = fingerprint(ctx.store.get());
      clearTimeout(view.busyTimer);
      view.busyTimer = setTimeout(function () {
        view.busyTimer = 0;
        if (view.busy) {
          endBusy();
          render();
        }
      }, BUSY_SETTLE_MS);
      // Disable the button in the click's own turn: another click before the
      // next envelope must find it disabled.
      render();
    }

    // run() sends one action; a non-empty reply is a bridge-level refusal
    // (the action never reached the controller), which clears the guard and
    // is logged, never shown as a daemon notice.
    function run(name, payload, busy) {
      if (busy) beginBusy();
      ctx.send(name, payload).then(function (err) {
        if (!err || !busy) return;
        endBusy();
        render();
        console.warn("kumiho: dashboard " + name + " refused: " + err);
      });
    }

    function render() {
      const snapshot = ctx.store.get();
      if (view.busy && fingerprint(snapshot) !== view.busyMark) endBusy();
      renderActions(snapshot);
      renderOptions(snapshot);
      renderRows(snapshot.status || null);
      renderNotice(snapshot);
    }

    function renderActions(snapshot) {
      const state = snapshot.status && snapshot.status.state
        ? String(snapshot.status.state).toUpperCase()
        : "";
      view.disconnect = DISCONNECT_STATES.indexOf(state) >= 0;
      ui.primary.textContent = t(view.disconnect ? "dashboard.disconnect" : "dashboard.connect");
      ui.optConnect.textContent = t("dashboard.connect");
      ui.primary.disabled = view.busy;
      ui.optConnect.disabled = view.busy || view.disconnect;
    }

    function renderOptions(snapshot) {
      const signature = locationsSignature(snapshot.locations);
      if (signature !== view.locationsSignature) {
        view.locationsSignature = signature;
        view.locations = snapshot.locations || [];
        rebuildLocations();
      }
      // The kill-switch choice defaults to the configured setting until the
      // user touches it (D13 "defaults from settings"); only an actual change
      // is sent with Connect, mirroring the CLI's optional --kill-switch.
      const killSwitch = snapshot.settings && snapshot.settings.kill_switch;
      if (killSwitch && !view.options.killSwitchTouched) {
        view.options.killSwitch = killSwitch;
        view.options.killSwitchBase = killSwitch;
      }
      ui.location.value = view.options.location;
      renderLocationFlag();
      ui.proxyInput.checked = view.options.proxyOnly;
      for (const radio of ui.killRadios) radio.checked = radio.value === view.options.killSwitch;
    }

    // renderLocationFlag draws the decorative companion beside the native
    // select; the option text itself stays text-only.
    function renderLocationFlag() {
      const code = ui.location.value;
      ui.locationFlag.replaceChildren();
      if (!code) {
        // Reserve the flag slot so hiding it never shifts the native select.
        ui.locationFlag.hidden = true;
        return;
      }
      ui.locationFlag.hidden = false;
      ui.locationFlag.append(flagIcon(code));
    }

    // rebuildLocations renders "Saved selection" plus every country as the
    // daemon serves it (REC first, inventory §Locations), keeping the user's
    // choice while it survives the list.
    function rebuildLocations() {
      let found = false;
      const options = [el("option", null, t("dashboard.saved_selection"))];
      options[0].value = "";
      for (const country of view.locations) {
        const option = el("option", null, country.name + " (" + country.code + ")");
        option.value = country.code;
        if (country.code === view.options.location) found = true;
        options.push(option);
      }
      ui.location.replaceChildren.apply(ui.location, options);
      if (!found) view.options.location = "";
      ui.location.value = view.options.location;
      renderLocationFlag();
    }

    function setRow(entry, text, tone) {
      entry.value.textContent = text;
      if (tone) entry.value.dataset.tone = tone;
      else delete entry.value.dataset.tone;
      const show = text !== "";
      entry.dt.hidden = !show;
      entry.dd.hidden = !show;
    }

    function uptimeText(status) {
      if (!status || !status.since) return "";
      return formatDuration(Math.max(0, Date.now() / 1000 - Number(status.since)));
    }

    function ratesText(status) {
      if (!status) return "";
      if (typeof status.rates_up !== "number" && typeof status.rates_down !== "number") return "";
      return t("dashboard.rates_value", {
        up: formatBytes(status.rates_up || 0),
        down: formatBytes(status.rates_down || 0),
      });
    }

    function totalsText(status) {
      if (!status) return "";
      if (typeof status.totals_up !== "number" && typeof status.totals_down !== "number") return "";
      return t("dashboard.totals_value", {
        up: formatBytes(status.totals_up || 0),
        down: formatBytes(status.totals_down || 0),
      });
    }

    function expiryText(seconds) {
      return typeof seconds === "number" && seconds > 0 ? formatDateTime(seconds) : "";
    }

    function quotaText(status) {
      if (!status) return "";
      const remaining = status.quota_remaining;
      const limit = status.quota_limit;
      if (typeof remaining === "number" && typeof limit === "number") {
        return t("dashboard.quota_remaining_of", {
          remaining: formatBytes(remaining),
          limit: formatBytes(limit),
        });
      }
      if (typeof remaining === "number") {
        return t("dashboard.quota_remaining", { remaining: formatBytes(remaining) });
      }
      if (typeof limit === "number") {
        return t("dashboard.quota_limit", { limit: formatBytes(limit) });
      }
      return "";
    }

    function quotaResetText(status) {
      if (!status || typeof status.quota_reset_unix !== "number" || status.quota_reset_unix <= 0) return "";
      return formatDate(status.quota_reset_unix);
    }

    function renderRows(status) {
      setRow(ui.rows.get("uptime"), uptimeText(status));
      setRow(ui.rows.get("location"), status ? locationText(status) : "");
      setRow(ui.rows.get("exit_ip"), status ? exitText(status) : "");
      setRow(ui.rows.get("rates"), ratesText(status));
      setRow(ui.rows.get("totals"), totalsText(status));
      // Kill switch and Autoconnect are hidden until the first status; a
      // false bool is omitted from the wire, so the off word is the default.
      setRow(ui.rows.get("kill_switch"), status ? (status.kill_switch ? t("common.on") : t("common.off")) : "", status ? (status.kill_switch ? "good" : "warn") : "");
      setRow(ui.rows.get("autoconnect"), status ? (status.autoconnect ? t("common.on") : t("common.off")) : "");
      setRow(ui.rows.get("token_expiry"), expiryText(status && status.token_expires));
      setRow(ui.rows.get("pass_expiry"), expiryText(status && status.pass_expires));
      setRow(ui.rows.get("quota"), quotaText(status));
      setRow(ui.rows.get("quota_reset"), quotaResetText(status));

      // The quota bar: only when both numbers are known and the limit is
      // positive; the fill is remaining/limit clamped to 0..1.
      const quota = ui.rows.get("quota");
      const remaining = status ? status.quota_remaining : undefined;
      const limit = status ? status.quota_limit : undefined;
      const known = typeof remaining === "number" && typeof limit === "number" && limit > 0;
      quota.bar.hidden = !known;
      if (known) {
        const ratio = Math.min(1, Math.max(0, remaining / limit));
        quota.fill.style.setProperty("inline-size", (ratio * 100).toFixed(1) + "%");
        quota.bar.setAttribute("aria-valuemin", "0");
        quota.bar.setAttribute("aria-valuemax", String(limit));
        quota.bar.setAttribute("aria-valuenow", String(remaining));
      }
    }

    function renderNotice(snapshot) {
      const notice = snapshot.notice;
      const presentation = noticeText(notice);
      if (presentation.text) {
        ui.notice.textContent = presentation.text;
        ui.notice.title = presentation.text;
        if (presentation.tone) ui.notice.dataset.tone = presentation.tone;
        else delete ui.notice.dataset.tone;
        ui.notice.hidden = false;
      } else {
        ui.notice.textContent = "";
        ui.notice.title = "";
        delete ui.notice.dataset.tone;
        ui.notice.hidden = true;
      }
      ui.details.textContent = presentation.detail;
      ui.details.hidden = presentation.detail === "";
      // Retry (same action as Refresh) only for a failed first status load
      // on a reachable daemon; the unreachable case is the shell's gate.
      ui.retry.hidden = !(snapshot.status === undefined && notice && snapshot.reachable === true);
      // The region fix lives in the reusable guide: offer it beside the
      // notice when the daemon asks for the blocked-region flow or the raw
      // connect error names the 451/prepared-pass path.
      const code = (notice && notice.code) || "";
      const blocked = code === "dashboard.region.failed" || code === "dashboard.prepare.failed" ||
        (code === "dashboard.connect.failed" && /HTTP 451|prepared pass|region/i.test(presentation.detail));
      ui.guide.hidden = !(notice && notice.tone === "bad" && blocked);
    }

    function optionsPayload() {
      const payload = {};
      if (view.options.location) payload.location_code = view.options.location;
      if (view.options.proxyOnly) payload.proxy_only = true;
      if (view.options.killSwitchTouched && view.options.killSwitch && view.options.killSwitch !== view.options.killSwitchBase) {
        payload.kill_switch = view.options.killSwitch;
      }
      return payload;
    }

    function openPopover(focus) {
      if (view.open) return;
      view.open = true;
      ui.popover.hidden = false;
      ui.trigger.setAttribute("aria-expanded", "true");
      if (focus) ui.location.focus();
    }

    function closePopover(focus) {
      if (!view.open) return;
      view.open = false;
      ui.popover.hidden = true;
      ui.trigger.setAttribute("aria-expanded", "false");
      if (focus) ui.trigger.focus();
    }

    function onPrimary() {
      if (view.disconnect) run("disconnect", {}, true);
      else run("connect", {}, true);
    }

    function onOptionsConnect() {
      closePopover(true);
      run("connect", optionsPayload(), true);
    }

    function onTrigger() {
      if (view.open) closePopover(true);
      else openPopover(true);
    }

    function onLocation() {
      view.options.location = ui.location.value;
      renderLocationFlag();
    }

    function onProxy() {
      view.options.proxyOnly = ui.proxyInput.checked;
    }

    function onKillSwitch(event) {
      if (!event.target.checked) return;
      view.options.killSwitch = event.target.value;
      view.options.killSwitchTouched = true;
    }

    function onDocPointer(event) {
      if (!view.open) return;
      if (ui.popover.contains(event.target) || ui.trigger.contains(event.target)) return;
      closePopover(false);
    }

    function onDocKey(event) {
      if (!view.open || event.key !== "Escape") return;
      closePopover(true);
    }

    function onRefresh() {
      run("refresh", {});
    }

    ui.primary.addEventListener("click", onPrimary);
    ui.optConnect.addEventListener("click", onOptionsConnect);
    ui.refresh.addEventListener("click", onRefresh);
    ui.retry.addEventListener("click", onRefresh);
    ui.trigger.addEventListener("click", onTrigger);
    ui.location.addEventListener("change", onLocation);
    ui.proxyInput.addEventListener("change", onProxy);
    for (const radio of ui.killRadios) radio.addEventListener("change", onKillSwitch);
    document.addEventListener("pointerdown", onDocPointer);
    document.addEventListener("keydown", onDocKey);

    const unsubscribe = ctx.store.subscribe(render);

    // Capture hint: #/dashboard?state=connect-options opens the popover
    // directly (scripts/visual/README.md M2/M3 contract).
    if (ctx.hint === "connect-options") openPopover(false);

    // Teardown: the document listeners outlive the screen's nodes, so they
    // must go explicitly; the node listeners die with the router's
    // replaceChildren and need no removal.
    cleanup = function () {
      unsubscribe();
      clearTimeout(view.busyTimer);
      document.removeEventListener("pointerdown", onDocPointer);
      document.removeEventListener("keydown", onDocKey);
    };
  },

  unmount() {
    if (cleanup) cleanup();
    cleanup = null;
  },
};
