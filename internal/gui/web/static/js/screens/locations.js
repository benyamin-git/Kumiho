/* The Locations screen (inventory §Locations): the live filter, the
 * country → city → server tree (REC first as served, drawn carets, the
 * `· selected` and `· 42 ms`/`· timeout` marks), the Refresh, Test-speed,
 * Ping-all and Expand-all/Collapse-all action row, and the tone-coloured
 * status line.
 *
 * Everything renders from the store's state envelopes (the controller
 * projection); actions go through the transport and the screen re-renders
 * from the next envelope — "" means accepted, never completed (T1.7). The
 * screen therefore owns no fetching: the controller loads the server list
 * itself (Poll's ensureLocations), and `refresh` re-runs its autonomous
 * loads. `locations_loaded` distinguishes the first-load placeholder from a
 * settled empty list (T1.2); the wire carries no locations-loading flag, so
 * the "Loading locations..." state is `!locations_loaded` without a load
 * error (reported as a projection gap, T2.3).
 *
 * Ping labels are the controller's: State.RTTs maps host:port to the label
 * rttLabel() formatted (`42 ms` / `timeout`, controller.go) and this screen
 * renders them as-is; the same projection owns the "Speed test failed: …"
 * notice. The busy guard around a Test-speed or Ping-all run is screen-side
 * for the same reason as the dashboard's: the state carries no ping-in-flight
 * signal. It
 * ends when the run shows any completion evidence — the locations notice
 * moved (a failure, or a cleared stale failure), or every requested host
 * gained a label if not every host had one at the click — and otherwise at
 * the settle timer, so a byte-identical repeat run can never stick (T2.2's
 * pattern).
 *
 * The tree follows the walk-era widget it replaces (locationsmodel.go /
 * locations.go at 282d63c): filter match on country and city names/codes
 * (trimmed, case-insensitive), a filtered country's row counts the surviving
 * cities, countries auto-expand while a filter is active (an explicit toggle
 * still flips the stored state and shows through once the filter clears), a
 * city-only match keeps just the matching cities, quarantined or
 * target-less servers never list, first-page/PageDown move by a viewport of
 * rows, and the wheel scrolls three rows per notch. Rows are the inventory's
 * 36-unit tree metric (the house table-row density; the walk-era
 * listRowHeight was the same 36).
 *
 * The router links css/screens/locations.css from the registry entry.
 *
 * Wording authority: the row texts, the `1 city`/`N cities`, the two
 * placeholder lines, and every status line string come from inventory
 * §Locations (including the ellipsis/period quirks documented there).
 */

import { flagFor } from "../flags.js";

// One Test-speed run probes at most this many visible server rows (inventory
// §Locations "up to the first 16 visible server rows").
const MAX_PING_HOSTS = 16;

// Row metrics: the inventory's 36-unit tree rows (the house table-row
// density) and the wheel's three rows per notch.
const ROW_PX = 36;
const WHEEL_NOTCH_MIN = 40; // smaller deltas are trackpad-scale: scroll natively
const WHEEL_ROWS = 3;

// The ping run's settle window: at least this long, and longer for a big
// target list (the daemon probes 24 hosts at a time with a 1.5 s deadline).
const PING_SETTLE_MS = 3000;
const PING_CONCURRENCY = 24;
const PING_PROBE_MS = 1500;
const PING_MARGIN_MS = 1500;

// The filter field / list padding (inventory: the filter is 260 wide; the
// walk-era list padded its rows by 8).
const LIST_PAD_PX = 8;

// unmount()'s teardown, assigned by mount (the router mounts one screen at a
// time, so one slot is enough).
let cleanup = null;

function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

/** "1 city" / "N cities" (inventory §Locations country rows). */
function cityCount(n) {
  return n === 1 ? "1 city" : n + " cities";
}

/** Case-insensitive substring match against the already-lowered filter. */
function matches(text, filter) {
  return String(text).toLowerCase().indexOf(filter) >= 0;
}

/**
 * The server's HTTP/2 CONNECT host:port, mirroring serverlist.Server's
 * ConnectTarget: prefer the "connect" protocol, fall back to hostname:port
 * when the record has no protocols, reject a record whose protocols lack
 * "connect". null means the server is never listed.
 */
function connectTarget(server) {
  const protocols = server.protocols || [];
  for (const p of protocols) {
    if (p.name === "connect") {
      const host = p.host || server.hostname;
      const port = p.port || server.port;
      return host && port ? host + ":" + port : null;
    }
  }
  if (protocols.length === 0 && server.hostname && server.port) {
    return server.hostname + ":" + server.port;
  }
  return null;
}

/** A cheap identity for the served server list (see syncData). */
function dataSignature(locations) {
  if (!locations) return "";
  let sig = "";
  for (const country of locations) {
    sig += country.code + "\u0000" + country.name + "\u0001";
    for (const city of country.cities || []) {
      sig += city.code + "\u0000" + city.name + "\u0001";
      for (const server of city.servers || []) {
        const target = connectTarget(server);
        sig += server.hostname + ":" + server.port + (server.quarantined ? "!" : "");
        sig += "=" + (target || "-") + "\u0001";
      }
    }
  }
  return sig;
}

/** The draw-caret (a stroked triangle; a drawn shape, never a font glyph). */
function caret(expanded) {
  const NS = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(NS, "svg");
  svg.setAttribute("viewBox", "0 0 8 8");
  svg.setAttribute("width", "8");
  svg.setAttribute("height", "8");
  svg.setAttribute("aria-hidden", "true");
  svg.setAttribute("focusable", "false");
  svg.classList.add("loc__caret");
  const points = document.createElementNS(NS, "polyline");
  points.setAttribute("points", expanded ? "1,1 4,4 7,1" : "1,1 4,4 1,7");
  points.setAttribute("fill", "none");
  points.setAttribute("stroke", "currentColor");
  points.setAttribute("stroke-width", "1.4");
  svg.append(points);
  return svg;
}

/** Build the screen's DOM and return its element references. */
function build(host) {
  const root = el("div", "loc");

  const refresh = el("button", "button button--tonal loc__refresh", "Refresh");
  refresh.type = "button";
  const test = el("button", "button button--tonal loc__test", "Test speed");
  test.type = "button";
  const pingAll = el("button", "button button--tonal loc__ping-all", "Ping all");
  const toggle = el("button", "button button--tonal loc__toggle", "Expand all");
  pingAll.type = "button";
  toggle.type = "button";
  const actions = el("div", "loc__actions");
  actions.append(refresh, test, pingAll, toggle);

  const filterLabel = el("label", "field__label", "Filter");
  filterLabel.htmlFor = "loc-filter";
  const filter = document.createElement("input");
  filter.type = "text";
  filter.id = "loc-filter";
  filter.className = "input";
  filter.placeholder = "Country or city";
  filter.autocomplete = "off";
  filter.spellcheck = false;
  const filterField = el("div", "field loc__field");
  filterField.append(filterLabel, filter);

  const tree = el("div", "card loc__tree");
  tree.setAttribute("role", "tree");
  tree.setAttribute("aria-label", "Locations");
  tree.tabIndex = 0;
  tree.hidden = true;

  const empty = el("p", "loc__empty");
  empty.hidden = true;

  const status = el("p", "loc__status");
  status.setAttribute("aria-live", "polite");
  status.hidden = true;

  root.append(actions, filterField, tree, empty, status);
  host.append(root);

  return {
    root: root,
    refresh: refresh,
    test: test,
    pingAll: pingAll,
    toggle: toggle,
    filter: filter,
    tree: tree,
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
      dataSignature: "",
      locations: [],
      expandedCountries: {},
      expandedCities: {},
      filter: "",
      rows: [],
      structureKey: null,
      rowEls: [],
      current: 0,
      // Selection reveal (the walk model's setSelection): re-runs when the
      // served data or the persisted selection changes.
      revealKey: null,
      // Test-speed / Ping-all run state.
      pingBusy: false,
      pingMode: "",
      pingCount: 0,
      pingHosts: [],
      pingAllLabelledAtStart: false,
      pingNoticeMark: "",
      pingTimer: 0,
      // DOM-render bookkeeping so envelopes only touch what changed.
      emptyShown: null,
      placeholder: null,
      statusText: null,
      statusTone: null,
    };

    function noticeMark(notice) {
      return notice && notice.message ? notice.message + "\u0000" + (notice.tone || "") : "";
    }

    function isSpeedFailure(notice) {
      return !!notice && typeof notice.message === "string" && notice.message.indexOf("Speed test failed: ") === 0;
    }

    // The load error is the locations notice when it is not a Test-speed
    // failure (the controller writes both to the same slot; the walk screen
    // kept them apart).
    function loadError(snapshot) {
      const notice = snapshot.locations_notice;
      return notice && notice.tone === "bad" && !isSpeedFailure(notice) ? notice.message : "";
    }

    /** Adopt new served data and re-reveal the persisted selection. */
    function syncData(snapshot) {
      const signature = dataSignature(snapshot.locations);
      if (signature !== view.dataSignature) {
        view.dataSignature = signature;
        view.locations = snapshot.locations || [];
      }
      const status = snapshot.status || {};
      const revealKey =
        view.dataSignature + "\u0000" + (status.location || "") + "\u0000" + (status.server || "");
      if (revealKey !== view.revealKey) {
        view.revealKey = revealKey;
        revealSelection(status);
      }
    }

    /**
     * Expand the country and city holding the persisted selection, as the
     * walk model's reveal did: the selected country by code, the city by
     * scanning that country's servers for the selected host:port.
     */
    function revealSelection(status) {
      const location = status.location || "";
      if (!location) return;
      const wanted = location.toLowerCase();
      view.expandedCountries[location] = true;
      for (const country of view.locations) {
        if (String(country.code).toLowerCase() !== wanted) continue;
        view.expandedCountries[country.code] = true;
        if (!status.server) continue;
        for (const city of country.cities || []) {
          for (const server of city.servers || []) {
            if (!server.quarantined && connectTarget(server) === status.server) {
              view.expandedCities[country.code + "/" + city.code] = true;
            }
          }
        }
      }
    }

    /**
     * The flattened visible rows, exactly the walk model's rules: filter on
     * country and city names/codes, a surviving country's row counting its
     * surviving cities, countries auto-expanded while a filter is active,
     * cities per their stored state, quarantined or target-less servers
     * never listed.
     */
    function flatten() {
      const filter = view.filter.trim().toLowerCase();
      const filterActive = view.filter !== "";
      const rows = [];
      for (const country of view.locations) {
        const countryMatch =
          filter === "" || matches(country.name, filter) || matches(country.code, filter);
        let cities = country.cities || [];
        if (!countryMatch) {
          cities = cities.filter(function (city) {
            return matches(city.name, filter) || matches(city.code, filter);
          });
          if (cities.length === 0) continue;
        }
        const countryExpanded = filterActive || !!view.expandedCountries[country.code];
        rows.push({
          kind: "country",
          level: 1,
          countryCode: country.code,
          countryName: country.name,
          cityCount: cities.length,
          expanded: countryExpanded,
        });
        if (!countryExpanded) continue;
        for (const city of cities) {
          const cityKey = country.code + "/" + city.code;
          const cityExpanded = !!view.expandedCities[cityKey];
          rows.push({
            kind: "city",
            level: 2,
            countryCode: country.code,
            cityCode: city.code,
            cityName: city.name,
            expanded: cityExpanded,
          });
          if (!cityExpanded) continue;
          for (const server of city.servers || []) {
            if (server.quarantined) continue; // never listed
            const address = connectTarget(server);
            if (!address) continue; // no connect target: never listed
            rows.push({
              kind: "server",
              level: 3,
              countryCode: country.code,
              cityCode: city.code,
              address: address,
            });
          }
        }
      }
      return rows;
    }

    /**
     * Every connectable server address in the loaded tree (including REC),
     * regardless of filter or expansion; quarantined and target-less servers
     * are skipped and duplicates keep the first spelling.
     */
    function allTargets() {
      const seen = {};
      const hosts = [];
      for (const country of view.locations) {
        for (const city of country.cities || []) {
          for (const server of city.servers || []) {
            if (server.quarantined) continue;
            const address = connectTarget(server);
            if (!address) continue;
            const key = address.toLowerCase();
            if (seen[key]) continue;
            seen[key] = true;
            hosts.push(address);
          }
        }
      }
      return hosts;
    }

    function anyExpanded() {
      for (const code in view.expandedCountries) {
        if (view.expandedCountries[code]) return true;
      }
      for (const key in view.expandedCities) {
        if (view.expandedCities[key]) return true;
      }
      return false;
    }

    /** Open every country and city, or close them all if any is open. */
    function toggleAll() {
      if (view.filter !== "" || view.locations.length === 0) return;
      if (anyExpanded()) {
        view.expandedCountries = {};
        view.expandedCities = {};
        return;
      }
      const countries = {};
      const cities = {};
      for (const country of view.locations) {
        countries[country.code] = true;
        for (const city of country.cities || []) {
          cities[country.code + "/" + city.code] = true;
        }
      }
      view.expandedCountries = countries;
      view.expandedCities = cities;
    }

    /**
     * The persisted selection's server row (the walk model: both the
     * selected country code, case-insensitively, and the host:port match).
     */
    function isSelected(row, snapshot) {
      const status = snapshot.status || {};
      return !!(
        status.server &&
        status.server === row.address &&
        String(status.location || "").toLowerCase() === String(row.countryCode).toLowerCase()
      );
    }

    /** The decorative flag for country rows only (never cities or servers). */
    function rowFlag(row) {
      return row.kind === "country" ? flagFor(row.countryCode) : "";
    }

    /** The row's single display line (the walk model's Text()). */
    function rowText(row, snapshot) {
      if (row.kind === "country") {
        return row.countryName + " (" + row.countryCode + ") · " + cityCount(row.cityCount);
      }
      if (row.kind === "city") {
        return row.cityName + " (" + row.cityCode + ")";
      }
      let text = row.address;
      const rtt = (snapshot.rtts || {})[row.address];
      if (rtt) text += " · " + rtt;
      if (isSelected(row, snapshot)) text += " · selected";
      return text;
    }

    /** The structure identity: row kinds, identities and expansion state. */
    function structureKey() {
      let key = "";
      for (const row of view.rows) {
        key +=
          row.kind + "|" + row.countryCode + "|" + (row.cityCode || "") + "|" +
          (row.address || "") + "|" + (row.expanded ? "1" : "0") + "\n";
      }
      return key;
    }

    function rebuildRows() {
      const frag = document.createDocumentFragment();
      const els = [];
      view.rows.forEach(function (row, index) {
        const rowEl = el("div", "loc__row");
        rowEl.setAttribute("role", "treeitem");
        rowEl.setAttribute("aria-level", String(row.level));
        if (row.kind !== "server") {
          rowEl.setAttribute("aria-expanded", row.expanded ? "true" : "false");
          rowEl.append(caret(row.expanded));
        }
        const flag = el("span", "loc__flag");
        flag.setAttribute("aria-hidden", "true");
        rowEl.append(flag);
        const label = el("span", "loc__label");
        rowEl.append(label);
        rowEl.id = "loc-row-" + index;
        rowEl.dataset.index = String(index);
        frag.append(rowEl);
        els.push({ el: rowEl, flag: flag, label: label });
      });
      ui.tree.replaceChildren(frag);
      view.rowEls = els;
    }

    function clampCurrent() {
      if (view.rows.length === 0) {
        view.current = 0;
        return;
      }
      view.current = Math.max(0, Math.min(view.current, view.rows.length - 1));
    }

    /** Per-envelope row updates: text, selected marks and the current row. */
    function renderRows(snapshot) {
      for (let i = 0; i < view.rows.length; i++) {
        const row = view.rows[i];
        const entry = view.rowEls[i];
        const text = rowText(row, snapshot);
        if (entry.label.textContent !== text) entry.label.textContent = text;
        const flag = rowFlag(row);
        if (entry.flag.textContent !== flag) entry.flag.textContent = flag;
        if (entry.flag.hidden !== (flag === "")) entry.flag.hidden = flag === "";
        if (row.kind === "server") {
          const want = isSelected(row, snapshot) ? "true" : "false";
          if (entry.el.getAttribute("aria-selected") !== want) {
            entry.el.setAttribute("aria-selected", want);
          }
        }
        const current = i === view.current ? "true" : "false";
        if (entry.el.dataset.current !== current) entry.el.dataset.current = current;
      }
      if (view.rows.length > 0 && view.rowEls[view.current]) {
        ui.tree.setAttribute("aria-activedescendant", view.rowEls[view.current].el.id);
      } else {
        ui.tree.removeAttribute("aria-activedescendant");
      }
    }

    function renderList(snapshot) {
      const empty = view.rows.length === 0;
      if (empty !== view.emptyShown) {
        view.emptyShown = empty;
        ui.tree.hidden = empty;
        ui.empty.hidden = !empty;
      }
      let placeholder = "";
      if (empty) {
        // The two lines are the inventory's exact strings (ellipsis and
        // trailing period included): loading wins while the first fetch has
        // no error to show; otherwise the settled empty-set copy.
        placeholder = !snapshot.locations_loaded && !loadError(snapshot)
          ? "Loading locations…"
          : "No locations match the filter.";
      }
      if (placeholder !== view.placeholder) {
        view.placeholder = placeholder;
        ui.empty.textContent = placeholder;
      }
    }

    function statusLine(snapshot) {
      const error = loadError(snapshot);
      const notice = snapshot.locations_notice;
      if (!snapshot.locations_loaded && !error) return { text: "Loading locations...", tone: "" };
      if (error) return { text: error, tone: "bad" };
      if (view.pingBusy) {
        return {
          text: view.pingMode === "all" ? "Pinging " + view.pingCount + " servers..." : "Testing speed...",
          tone: "",
        };
      }
      if (isSpeedFailure(notice)) return { text: notice.message, tone: "bad" };
      if (snapshot.locations_loaded && view.rows.length === 0 && view.filter !== "") {
        return { text: "No locations match the filter", tone: "" };
      }
      if (snapshot.locations_loaded && view.rows.length === 0) {
        return { text: "No locations loaded", tone: "" };
      }
      // The shared view-model notice (Location saved / could not save the
      // location): the walk screen rendered it in the neutral tone.
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
      ui.test.disabled = busy;
      ui.pingAll.disabled = busy || allTargets().length === 0;
      ui.toggle.disabled = view.filter !== "" || view.locations.length === 0;
      const label = anyExpanded() ? "Collapse all" : "Expand all";
      if (ui.toggle.textContent !== label) ui.toggle.textContent = label;
    }

    // endPing ends a run's guard; the caller re-renders (or is mid-render
    // already).
    function endPing() {
      view.pingBusy = false;
      view.pingMode = "";
      view.pingCount = 0;
      view.pingHosts = [];
      view.pingAllLabelledAtStart = false;
      clearTimeout(view.pingTimer);
      view.pingTimer = 0;
    }

    /**
     * pollPing ends a run's guard on its completion evidence: the locations
     * notice moved (a fresh failure, or a cleared stale one), or every
     * requested host now carries a label where not every host did at the
     * click (the first run's natural end). A run whose results are all
     * repeats moves nothing and ends at the settle timer instead.
     */
    function pollPing(snapshot) {
      if (!view.pingBusy) return;
      const rtts = snapshot.rtts || {};
      const allLabelled = view.pingHosts.every(function (host) {
        return rtts[host] !== undefined;
      });
      const moved = noticeMark(snapshot.locations_notice) !== view.pingNoticeMark;
      if (moved || (!view.pingAllLabelledAtStart && allLabelled)) endPing();
    }

    function render() {
      const snapshot = ctx.store.get();
      syncData(snapshot);
      view.rows = flatten();
      const key = structureKey();
      if (key !== view.structureKey) {
        view.structureKey = key;
        rebuildRows();
      }
      clampCurrent();
      renderRows(snapshot);
      pollPing(snapshot);
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

    function beginPing(hosts, mode) {
      const snapshot = ctx.store.get();
      const rtts = snapshot.rtts || {};
      view.pingBusy = true;
      view.pingMode = mode;
      view.pingCount = hosts.length;
      view.pingHosts = hosts.slice();
      view.pingNoticeMark = noticeMark(snapshot.locations_notice);
      view.pingAllLabelledAtStart = hosts.every(function (host) {
        return rtts[host] !== undefined;
      });
      clearTimeout(view.pingTimer);
      view.pingTimer = setTimeout(function () {
        view.pingTimer = 0;
        endPing();
        render();
      }, pingWindowMs(hosts.length));
      // Disable the buttons in the click's own turn: another click before the
      // next envelope must find them disabled.
      render();
    }

    function onTestSpeed() {
      if (view.pingBusy) return;
      const hosts = [];
      for (const row of view.rows) {
        if (row.kind !== "server") continue;
        hosts.push(row.address);
        if (hosts.length === MAX_PING_HOSTS) break;
      }
      if (hosts.length === 0) return; // nothing visible to probe
      beginPing(hosts, "speed");
      ctx.send("ping", { hosts: hosts }).then(function (err) {
        if (!err) return;
        endPing();
        render();
        console.warn("kumiho: locations ping refused: " + err);
      });
    }

    function onPingAll() {
      if (view.pingBusy) return;
      const hosts = allTargets();
      if (hosts.length === 0) return;
      beginPing(hosts, "all");
      ctx.send("ping", { hosts: hosts }).then(function (err) {
        if (!err) return;
        endPing();
        render();
        console.warn("kumiho: locations ping all refused: " + err);
      });
    }

    function onToggleAll() {
      if (view.filter !== "" || view.locations.length === 0) return;
      toggleAll();
      render();
    }

    // Refresh asks the daemon to re-download the server list: the ruled
    // {"locations":true} payload forces the fetch through the controller's
    // loaded guard. The screen keeps its stale rows while it runs and after
    // a failure (it renders from the state envelope and never clears them).
    function onRefresh() {
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

    function activate(index) {
      const row = view.rows[index];
      if (!row) return;
      if (row.kind === "country") {
        // The stored toggle always flips; while a filter is active the
        // effective state stays expanded and the flip shows once it clears.
        view.expandedCountries[row.countryCode] = !view.expandedCountries[row.countryCode];
        return;
      }
      if (row.kind === "city") {
        const key = row.countryCode + "/" + row.cityCode;
        view.expandedCities[key] = !view.expandedCities[key];
        return;
      }
      selectServer(row);
    }

    /** Keyboard navigation metrics: full rows inside the card's padding. */
    function viewportRows() {
      const height = ui.tree.clientHeight - 2 * LIST_PAD_PX;
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

    function onTreeKey(event) {
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
          activate(view.current);
          break;
        default:
          return;
      }
      event.preventDefault();
      render();
      scrollCurrentIntoView();
    }

    // Press moves the current row (the walk widget's mouse-down), release on
    // the same row activates it (the click event's target is the shared
    // ancestor when the two land on different rows, so closest() rejects
    // those). The wheel scrolls three rows per notch; trackpad-scale deltas
    // scroll natively.
    function onTreePointerDown(event) {
      if (event.button !== 0) return;
      const rowEl = event.target.closest(".loc__row");
      if (!rowEl || !ui.tree.contains(rowEl)) return;
      view.current = Number(rowEl.dataset.index);
      ui.tree.focus({ preventScroll: true });
      render();
    }

    function onTreeClick(event) {
      const rowEl = event.target.closest(".loc__row");
      if (!rowEl || !ui.tree.contains(rowEl)) return;
      view.current = Number(rowEl.dataset.index);
      activate(view.current);
      render();
    }

    function onTreeWheel(event) {
      if (Math.abs(event.deltaY) < WHEEL_NOTCH_MIN) return;
      event.preventDefault();
      ui.tree.scrollTop += (event.deltaY > 0 ? 1 : -1) * WHEEL_ROWS * ROW_PX;
    }

    function onFilter() {
      view.filter = ui.filter.value;
      view.current = 0;
      render();
    }

    ui.refresh.addEventListener("click", onRefresh);
    ui.test.addEventListener("click", onTestSpeed);
    ui.pingAll.addEventListener("click", onPingAll);
    ui.toggle.addEventListener("click", onToggleAll);
    ui.filter.addEventListener("input", onFilter);
    ui.tree.addEventListener("keydown", onTreeKey);
    ui.tree.addEventListener("pointerdown", onTreePointerDown);
    ui.tree.addEventListener("click", onTreeClick);
    ui.tree.addEventListener("wheel", onTreeWheel, { passive: false });

    const unsubscribe = ctx.store.subscribe(render);

    // Teardown: only the timer and the subscription outlive the screen's
    // nodes (every listener above dies with the router's replaceChildren).
    cleanup = function () {
      unsubscribe();
      clearTimeout(view.pingTimer);
    };
  },

  unmount() {
    if (cleanup) cleanup();
    cleanup = null;
  },
};
