/* The Locations screen: one flat server list, the live search, the Refresh
 * and Ping-all actions, and the tone-coloured status line.
 *
 * The controller owns every fetch and the latency labels. This screen builds
 * the display rows from `state.locations` (connect target, protocols,
 * Recommended flag, search haystack), filters and orders them locally, and
 * sends only actions. The wire carries no locations-loading flag, so the
 * "Loading locations…" state is `!locations_loaded` without a load error.
 *
 * Ping labels are the controller's State.RTTs strings (`42 ms` / `timeout`);
 * this screen parses them for speed ordering and renders them as-is. A run
 * keeps its pre-run order while results arrive; only a completed run with at
 * least one answer switches to speed order. The busy guard is screen-side for
 * the same reason as the Dashboard's: the state carries no ping-in-flight
 * signal. It ends on a speed failure notice, when every requested host has a
 * fresh label, or at the settle timer; a transport refusal ends it without a
 * speed claim.
 *
 * The list is a listbox, not a tree: no levels, carets or expansion. The
 * router links css/screens/locations.css from its registry entry.
 */

import { flagIcon } from "../flags.js";

const SEPARATOR = " · ";
const ROW_PX = 48;
const PING_SETTLE_MS = 3000;
const PING_CONCURRENCY = 24;
const PING_PROBE_MS = 1500;
const PING_MARGIN_MS = 1500;

// unmount()'s teardown, assigned by mount (the router mounts one screen at a
// time, so one slot is enough).
let cleanup = null;

function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function normalizedCode(code) {
  return String(code == null ? "" : code).trim().toLowerCase();
}

function isRecommended(country) {
  return normalizedCode(country && country.code) === "rec";
}

function displayCountryName(country) {
  if (isRecommended(country)) return "Recommended";
  const name = String((country && country.name) || "").trim();
  return name || String((country && country.code) || "").trim().toUpperCase();
}

function protocolNames(server) {
  const seen = new Set();
  const names = [];
  for (const protocol of server.protocols || []) {
    const name = String((protocol && protocol.name) || "").trim();
    if (!name || seen.has(name)) continue;
    seen.add(name);
    names.push(name);
  }
  return names;
}

/**
 * The server's HTTP/2 CONNECT host:port, mirroring serverlist.ConnectTarget:
 * prefer the "connect" protocol, fall back to hostname:port when the record
 * has no protocols at all, and reject a record whose protocols lack
 * "connect". A server without a usable target is never listed, selected or
 * pinged.
 */
function connectTarget(server) {
  const protocols = server.protocols || [];
  const usable = function (host, port) {
    return host && port >= 1 && port <= 65535 ? host + ":" + port : null;
  };
  for (const protocol of protocols) {
    if (!protocol || String(protocol.name || "").trim().toLowerCase() !== "connect") continue;
    const host = String(protocol.host || server.hostname || "").trim();
    const port = Number(protocol.port || server.port || 0);
    return usable(host, port);
  }
  if (protocols.length === 0) {
    return usable(String(server.hostname || "").trim(), Number(server.port || 0));
  }
  return null;
}

/** Every display row in the loaded list, before filtering and sorting. */
function buildRows(locations) {
  const rows = [];
  let index = 0;
  for (const country of locations || []) {
    const countryName = displayCountryName(country);
    const countryCode = String((country && country.code) || "").trim();
    const recommended = isRecommended(country);
    for (const city of (country && country.cities) || []) {
      const cityName = String((city && city.name) || "").trim();
      const cityCode = String((city && city.code) || "").trim();
      const visibleCity = cityName && cityName.toLowerCase() !== countryName.toLowerCase()
        ? cityName
        : "";
      for (const server of (city && city.servers) || []) {
        if (!server || server.quarantined) continue;
        const address = connectTarget(server);
        if (!address) continue;
        const protocols = protocolNames(server);
        const title = visibleCity ? countryName + SEPARATOR + visibleCity : countryName;
        const detail = protocols.length
          ? address + SEPARATOR + protocols.join(", ")
          : address;
        rows.push({
          index: index++,
          countryCode: countryCode,
          countryName: countryName,
          cityCode: cityCode,
          cityName: visibleCity,
          server: server,
          address: address,
          protocols: protocols,
          recommended: recommended,
          title: title,
          detail: detail,
          haystack: [
            String((country && country.name) || ""),
            countryName,
            countryCode,
            cityName,
            cityCode,
            String((server && server.hostname) || ""),
            address,
            protocols.join(" "),
          ].join(" ").toLowerCase(),
          key: normalizedCode(countryCode) + "/" + normalizedCode(cityCode) + "/" + address.toLowerCase(),
        });
      }
    }
  }
  return rows;
}

function matchingRows(rows, query) {
  const filter = String(query || "").trim().toLowerCase();
  if (!filter) return rows;
  return rows.filter(function (row) {
    return row.haystack.indexOf(filter) >= 0;
  });
}

function rttValue(label) {
  const match = String(label == null ? "" : label).trim().match(/^(\d+) ms$/);
  return match ? Number(match[1]) : Infinity;
}

function latencyValue(latency, address) {
  return latency[String(address).toLowerCase()];
}

function compareRows(a, b, sortMode, latency) {
  if (a.recommended !== b.recommended) return a.recommended ? -1 : 1;
  if (sortMode === "speed") {
    const ar = rttValue(latencyValue(latency, a.address));
    const br = rttValue(latencyValue(latency, b.address));
    if (ar !== br) return ar < br ? -1 : 1;
  }
  const country = a.countryName.toLowerCase().localeCompare(b.countryName.toLowerCase());
  if (country !== 0) return country;
  const city = a.cityName.toLowerCase().localeCompare(b.cityName.toLowerCase());
  if (city !== 0) return city;
  const host = String(a.server.hostname || "").toLowerCase().localeCompare(
    String(b.server.hostname || "").toLowerCase(),
  );
  if (host !== 0) return host;
  const address = a.address.toLowerCase().localeCompare(b.address.toLowerCase());
  if (address !== 0) return address;
  return a.index - b.index;
}

function sortRows(rows, sortMode, latency) {
  return rows.slice().sort(function (a, b) {
    return compareRows(a, b, sortMode, latency);
  });
}

/** A cheap identity for the served server list (see syncData). */
function dataSignature(locations) {
  if (!locations) return "";
  let signature = "";
  for (const country of locations) {
    signature += (country.code || "") + "\u0000" + (country.name || "") + "\u0001";
    for (const city of country.cities || []) {
      signature += (city.code || "") + "\u0000" + (city.name || "") + "\u0001";
      for (const server of city.servers || []) {
        signature += (server.hostname || "") + ":" + (server.port || 0);
        signature += (server.quarantined ? "!" : "") + "=" + (connectTarget(server) || "-");
        for (const protocol of server.protocols || []) {
          signature += "|" + (protocol.name || "") + "," + (protocol.host || "") + "," +
            (protocol.port || 0) + "," + (protocol.scheme || "");
        }
        signature += "\u0001";
      }
    }
  }
  return signature;
}

/** Case-insensitive RTT lookup keyed by the lowercased address. */
function latencyMap(snapshot) {
  const map = {};
  const rtts = snapshot.rtts || {};
  for (const key of Object.keys(rtts)) map[String(key).toLowerCase()] = rtts[key];
  return map;
}

function isSelected(row, snapshot) {
  const status = snapshot.status || {};
  if (!status.server) return false;
  const location = normalizedCode(status.location);
  if (location && location !== normalizedCode(row.countryCode)) return false;
  return String(status.server).toLowerCase() === row.address.toLowerCase();
}

/** Build the screen's DOM and return its element references. */
function build(host) {
  const root = el("div", "loc");

  const refresh = el("button", "button button--tonal loc__refresh", "Refresh");
  refresh.type = "button";
  const pingAll = el("button", "button button--tonal loc__ping-all", "Ping all");
  pingAll.type = "button";
  const actions = el("div", "loc__actions");
  actions.append(refresh, pingAll);

  const filterLabel = el("label", "field__label", "Search locations");
  filterLabel.htmlFor = "loc-filter";
  const filter = document.createElement("input");
  filter.type = "text";
  filter.id = "loc-filter";
  filter.className = "input";
  filter.placeholder = "Country, city, server, or protocol";
  filter.autocomplete = "off";
  filter.spellcheck = false;
  const filterField = el("div", "field loc__field");
  filterField.append(filterLabel, filter);

  const list = el("div", "card loc__list");
  list.setAttribute("role", "listbox");
  list.setAttribute("aria-label", "Locations");
  list.tabIndex = 0;
  list.hidden = true;

  const empty = el("p", "loc__empty");
  empty.hidden = true;

  const status = el("p", "loc__status");
  status.setAttribute("aria-live", "polite");
  status.hidden = true;

  root.append(actions, filterField, list, empty, status);
  host.append(root);

  return {
    root: root,
    refresh: refresh,
    pingAll: pingAll,
    filter: filter,
    list: list,
    empty: empty,
    status: status,
  };
}

export default {
  mount(host, ctx) {
    const ui = build(host);

    // Per-mount view state. The store hands parsed snapshots (a fresh object
    // graph per envelope), so identity checks use signatures, never
    // references.
    const view = {
      dataSignature: null,
      locations: [],
      filter: "",
      rows: [],
      rowsSignature: "",
      rowEls: [],
      current: 0,
      sortMode: "country",
      scrollToTop: false,
      // Ping-all run state.
      pingBusy: false,
      pingHosts: [],
      pingBaseline: {},
      pingAnswered: false,
      pingTimer: 0,
      frozenLatency: null,
      // DOM-render bookkeeping so envelopes only touch what changed.
      emptyShown: null,
      placeholder: null,
      statusText: null,
      statusTone: null,
    };

    function isSpeedFailure(notice) {
      return !!notice && typeof notice.message === "string" &&
        notice.message.indexOf("Speed test failed: ") === 0;
    }

    function loadError(snapshot) {
      const notice = snapshot.locations_notice;
      return notice && notice.tone === "bad" && !isSpeedFailure(notice) ? notice.message : "";
    }

    /**
     * Adopt new served data. A changed list is a new visit: country order,
     * top of the list and no stale local latency ordering. A refresh with the
     * same data is reset by onRefresh (the controller also clears its RTTs).
     */
    function syncData(snapshot) {
      const signature = dataSignature(snapshot.locations);
      if (signature === view.dataSignature) return;
      view.dataSignature = signature;
      view.locations = snapshot.locations || [];
      view.sortMode = "country";
      view.current = 0;
      view.scrollToTop = true;
      view.frozenLatency = null;
    }

    function currentRows(snapshot) {
      const live = latencyMap(snapshot);
      const sortLatency = view.pingBusy && view.frozenLatency ? view.frozenLatency : live;
      const rows = sortRows(
        matchingRows(buildRows(view.locations), view.filter),
        view.sortMode,
        sortLatency,
      );
      return { rows: rows, live: live };
    }

    function allTargets() {
      const seen = new Set();
      const hosts = [];
      for (const row of buildRows(view.locations)) {
        const key = row.address.toLowerCase();
        if (seen.has(key)) continue;
        seen.add(key);
        hosts.push(row.address);
      }
      return hosts;
    }

    function rebuildRows() {
      const frag = document.createDocumentFragment();
      const els = [];
      view.rows.forEach(function (row, index) {
        const rowEl = el("div", "loc__row");
        rowEl.id = "loc-row-" + index;
        rowEl.dataset.index = String(index);
        rowEl.setAttribute("role", "option");
        rowEl.setAttribute("aria-selected", "false");

        const flag = el("span", "loc__flag");
        flag.setAttribute("aria-hidden", "true");
        flag.append(flagIcon(row.countryCode));
        rowEl.append(flag);

        const main = el("span", "loc__main");
        main.append(
          el("span", "loc__title", row.title),
          el("span", "loc__detail", row.detail),
        );
        rowEl.append(main);

        const trailing = el("span", "loc__trailing");
        const latency = el("span", "loc__latency");
        const check = el("span", "loc__check", "✓");
        check.setAttribute("aria-hidden", "true");
        check.hidden = true;
        trailing.append(latency, check);
        rowEl.append(trailing);

        frag.append(rowEl);
        els.push({ el: rowEl, latency: latency, check: check });
      });
      ui.list.replaceChildren(frag);
      view.rowEls = els;
    }

    function clampCurrent() {
      if (view.rows.length === 0) {
        view.current = 0;
        return;
      }
      view.current = Math.max(0, Math.min(view.current, view.rows.length - 1));
    }

    /** Per-envelope row updates: latency, selected mark and current row. */
    function renderRowState(snapshot, live) {
      for (let i = 0; i < view.rows.length; i++) {
        const row = view.rows[i];
        const entry = view.rowEls[i];
        const label = latencyValue(live, row.address);
        const latency = label === undefined || label === "" ? "—" : String(label);
        if (entry.latency.textContent !== latency) entry.latency.textContent = latency;
        const selected = isSelected(row, snapshot);
        const wantSelected = selected ? "true" : "false";
        if (entry.el.getAttribute("aria-selected") !== wantSelected) {
          entry.el.setAttribute("aria-selected", wantSelected);
        }
        entry.check.hidden = !selected;
        const current = i === view.current ? "true" : "false";
        if (entry.el.dataset.current !== current) entry.el.dataset.current = current;
      }
      if (view.rows.length > 0 && view.rowEls[view.current]) {
        ui.list.setAttribute("aria-activedescendant", view.rowEls[view.current].el.id);
      } else {
        ui.list.removeAttribute("aria-activedescendant");
      }
    }

    function renderList(snapshot) {
      const empty = view.rows.length === 0;
      if (empty !== view.emptyShown) {
        view.emptyShown = empty;
        ui.list.hidden = empty;
        ui.empty.hidden = !empty;
      }
      let placeholder = "";
      if (empty) {
        placeholder = !snapshot.locations_loaded && !loadError(snapshot)
          ? "Loading locations…"
          : view.filter.trim() !== ""
            ? "No locations match the filter."
            : "No locations loaded";
      }
      if (placeholder !== view.placeholder) {
        view.placeholder = placeholder;
        ui.empty.textContent = placeholder;
      }
      if (view.scrollToTop) {
        view.scrollToTop = false;
        ui.list.scrollTop = 0;
      }
    }

    function statusLine(snapshot) {
      const error = loadError(snapshot);
      const notice = snapshot.locations_notice;
      if (!snapshot.locations_loaded && !error) return { text: "Loading locations...", tone: "" };
      if (error) return { text: error, tone: "bad" };
      if (view.pingBusy) return { text: "Pinging " + view.pingHosts.length + " servers...", tone: "" };
      if (isSpeedFailure(notice)) return { text: notice.message, tone: "bad" };
      if (snapshot.locations_loaded && view.rows.length === 0 && view.filter.trim() !== "") {
        return { text: "No locations match the filter", tone: "" };
      }
      if (snapshot.locations_loaded && view.rows.length === 0) {
        return { text: "No locations loaded", tone: "" };
      }
      const shared = snapshot.notice;
      if (shared && shared.message) return { text: shared.message, tone: "" };
      return { text: "", tone: "" };
    }

    function renderStatus(snapshot) {
      const line = statusLine(snapshot);
      if (line.text === view.statusText && line.tone === view.statusTone) return;
      view.statusText = line.text;
      view.statusTone = line.tone;
      ui.status.textContent = line.text;
      ui.status.title = line.text;
      if (line.tone) ui.status.dataset.tone = line.tone;
      else delete ui.status.dataset.tone;
      ui.status.hidden = line.text === "";
    }

    function renderActions() {
      const busy = view.pingBusy;
      ui.refresh.disabled = busy;
      ui.pingAll.disabled = busy || allTargets().length === 0;
    }

    function endPing() {
      view.pingBusy = false;
      view.pingHosts = [];
      view.pingBaseline = {};
      view.pingAnswered = false;
      view.frozenLatency = null;
      clearTimeout(view.pingTimer);
      view.pingTimer = 0;
    }

    function finishPing(failed) {
      if (!view.pingBusy) return;
      const answered = view.pingAnswered && !failed;
      endPing();
      if (answered) {
        view.sortMode = "speed";
        view.current = 0;
        view.scrollToTop = true;
      }
    }

    /**
     * pollPing ends a run on a speed-failure notice, or when every requested
     * host has a fresh label and at least one host answered. A run whose
     * results are all byte-identical to the baseline moves nothing and ends
     * at the settle timer instead.
     */
    function pollPing(snapshot) {
      if (!view.pingBusy) return;
      if (isSpeedFailure(snapshot.locations_notice)) {
        finishPing(true);
        return;
      }
      const rtts = latencyMap(snapshot);
      let allLabelled = true;
      for (const host of view.pingHosts) {
        const key = host.toLowerCase();
        const now = rtts[key];
        if (now === undefined) {
          allLabelled = false;
          continue;
        }
        if (view.pingBaseline[key] === undefined || now !== view.pingBaseline[key]) {
          view.pingAnswered = true;
        }
      }
      if (view.pingAnswered && allLabelled) finishPing(false);
    }

    function render() {
      const snapshot = ctx.store.get();
      syncData(snapshot);
      pollPing(snapshot);

      const current = currentRows(snapshot);
      view.rows = current.rows;
      const signature = view.rows.map(function (row) {
        return row.key + "\u0000" + row.title + "\u0000" + row.detail;
      }).join("\n");
      if (signature !== view.rowsSignature) {
        view.rowsSignature = signature;
        rebuildRows();
      }
      clampCurrent();
      renderRowState(snapshot, current.live);
      renderList(snapshot);
      renderStatus(snapshot);
      renderActions();
    }

    function pingWindowMs(count) {
      return Math.max(
        PING_SETTLE_MS,
        Math.ceil(count / PING_CONCURRENCY) * PING_PROBE_MS + PING_MARGIN_MS,
      );
    }

    function beginPing(hosts) {
      const snapshot = ctx.store.get();
      const live = latencyMap(snapshot);
      const baseline = {};
      for (const host of hosts) baseline[host.toLowerCase()] = live[host.toLowerCase()];
      view.pingBusy = true;
      view.pingHosts = hosts.slice();
      view.pingBaseline = baseline;
      view.pingAnswered = false;
      view.frozenLatency = live;
      clearTimeout(view.pingTimer);
      view.pingTimer = setTimeout(function () {
        view.pingTimer = 0;
        finishPing(false);
        render();
      }, pingWindowMs(hosts.length));
      // Disable the buttons in the click's own turn: another click before the
      // next envelope must find them disabled.
      render();
    }

    function onPingAll() {
      if (view.pingBusy) return;
      const hosts = allTargets();
      if (hosts.length === 0) return;
      beginPing(hosts);
      ctx.send("ping", { hosts: hosts }).then(function (err) {
        if (!err) return;
        if (view.pingBusy) {
          finishPing(true);
          render();
        }
        console.warn("kumiho: locations ping all refused: " + err);
      });
    }

    // Refresh asks the daemon to re-download the server list. The controller
    // clears its RTT projection; this screen immediately returns to country
    // order (a new visit), keeps the old rows while the fetch runs, and shows
    // a failed refresh through the existing status line.
    function onRefresh() {
      if (view.pingBusy) return;
      view.sortMode = "country";
      view.current = 0;
      view.frozenLatency = null;
      view.scrollToTop = true;
      render();
      ctx.send("refresh", { locations: true }).then(function (err) {
        if (err) console.warn("kumiho: locations refresh refused: " + err);
      });
    }

    function selectServer(row) {
      ctx.send("select_location", {
        country_code: row.countryCode,
        city_code: row.cityCode,
        server: row.address,
      }).then(function (err) {
        if (err) console.warn("kumiho: locations select_location refused: " + err);
      });
    }

    function viewportRows() {
      const height = ui.list.clientHeight - 2 * 8;
      return Math.max(1, Math.floor(height / ROW_PX));
    }

    function setCurrent(index) {
      if (view.rows.length === 0) return;
      view.current = Math.max(0, Math.min(index, view.rows.length - 1));
    }

    function scrollCurrentIntoView() {
      const entry = view.rowEls[view.current];
      if (entry) entry.el.scrollIntoView({ block: "nearest" });
    }

    function onListKey(event) {
      if (event.altKey || event.ctrlKey || event.metaKey) return;
      switch (event.key) {
        case "ArrowDown":
          setCurrent(view.current + 1);
          break;
        case "ArrowUp":
          setCurrent(view.current - 1);
          break;
        case "PageDown":
          setCurrent(view.current + Math.max(1, viewportRows() - 1));
          break;
        case "PageUp":
          setCurrent(view.current - Math.max(1, viewportRows() - 1));
          break;
        case "Home":
          setCurrent(0);
          break;
        case "End":
          setCurrent(view.rows.length - 1);
          break;
        case "Enter":
        case " ":
          if (view.rows[view.current]) selectServer(view.rows[view.current]);
          break;
        default:
          return;
      }
      event.preventDefault();
      render();
      scrollCurrentIntoView();
    }

    function onListClick(event) {
      const rowEl = event.target.closest(".loc__row");
      if (!rowEl || !ui.list.contains(rowEl)) return;
      view.current = Number(rowEl.dataset.index);
      ui.list.focus({ preventScroll: true });
      selectServer(view.rows[view.current]);
      render();
    }

    function onFilter() {
      view.filter = ui.filter.value;
      view.current = 0;
      view.scrollToTop = true;
      render();
    }

    ui.refresh.addEventListener("click", onRefresh);
    ui.pingAll.addEventListener("click", onPingAll);
    ui.filter.addEventListener("input", onFilter);
    ui.list.addEventListener("keydown", onListKey);
    ui.list.addEventListener("click", onListClick);

    const unsubscribe = ctx.store.subscribe(render);

    // Teardown: only the timer and the subscription outlive the screen's
    // nodes (every listener above dies with the router's replaceChildren).
    cleanup = function () {
      unsubscribe();
      clearTimeout(view.pingTimer);
      view.pingTimer = 0;
    };
  },

  unmount() {
    if (cleanup) cleanup();
    cleanup = null;
  },
};
