/* The Locations screen: pinned Fastest / Favorites / Recent sections above
 * the full server list, the live search, Refresh and Ping-all actions, the
 * locked-list footer action, and the tone-coloured status line.
 *
 * The controller owns every fetch and the latency projection. This screen
 * builds the display rows from `state.locations` (connect target, protocols,
 * Recommended flag, search haystack), resolves the persisted favorite/recent
 * refs, filters and orders the full list locally, and sends only actions.
 * The Fastest row and the pinned sections never participate in speed order.
 *
 * Ping labels read `state.rtts` while a run is busy and the controller's
 * `state.latency` cache when idle. The run ends only on the daemon's
 * `ping_done_seq` change; there is no settle timer. A completed run with at
 * least one answer switches only the full list to speed order.
 *
 * The list is a listbox, not a tree. The router links
 * css/screens/locations.css from its registry entry.
 */

import { flagIcon } from "../flags.js";
import { t, formatNumber, noticeText } from "../i18n.js";

const SEPARATOR = " · ";
const ROW_PX = 48;

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
  if (isRecommended(country)) return t("locations.recommended");
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
    const rawCountryName = String((country && country.name) || "").trim();
    const countryCode = String((country && country.code) || "").trim();
    const recommended = isRecommended(country);
    const locked = !!country.locked;
    for (const city of (country && country.cities) || []) {
      const cityName = String((city && city.name) || "").trim();
      const cityCode = String((city && city.code) || "").trim();
      const visibleCity = !recommended &&
        cityName &&
        cityName.toLowerCase() !== countryName.toLowerCase() &&
        cityName.toLowerCase() !== rawCountryName.toLowerCase()
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
          locked: locked,
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

function latencySamples(snapshot) {
  const map = {};
  const latency = (snapshot && snapshot.latency) || {};
  for (const key of Object.keys(latency)) {
    map[String(key).toLowerCase()] = latency[key];
  }
  return map;
}

function liveRTTs(snapshot) {
  const map = {};
  const rtts = (snapshot && snapshot.rtts) || {};
  for (const key of Object.keys(rtts)) map[String(key).toLowerCase()] = rtts[key];
  return map;
}

function sampleValue(sample) {
  if (!sample || sample.rtt_ms == null) return Infinity;
  const value = Number(sample.rtt_ms);
  return Number.isFinite(value) ? value : Infinity;
}

function latencyLabel(snapshot, row, busy) {
  const key = String(row.address || "").toLowerCase();
  if (busy) {
    const live = liveRTTs(snapshot)[key];
    return live === undefined ? "—" : formatNumber(live);
  }
  const sample = latencySamples(snapshot)[key];
  if (sample === undefined) return "—";
  return sample.rtt_ms == null
    ? t("locations.timeout")
    : formatNumber(Math.round(Number(sample.rtt_ms))) + " " + t("locations.ms");
}

function compareRows(a, b, sortMode, latency) {
  if (a.recommended !== b.recommended) return a.recommended ? -1 : 1;
  if (sortMode === "speed") {
    const ar = sampleValue(latency[String(a.address).toLowerCase()]);
    const br = sampleValue(latency[String(b.address).toLowerCase()]);
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
    signature += (country.code || "") + "\u0000" + (country.name || "") +
      (country.locked ? "!" : "") + "\u0001";
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

function refKey(ref) {
  return [
    normalizedCode(ref && ref.country_code),
    normalizedCode(ref && ref.city_code),
    String((ref && ref.server) || "").toLowerCase(),
  ].join("/");
}

function resolvedRefRows(refs, rows) {
  const out = [];
  const seen = new Set();
  for (const ref of refs || []) {
    if (!ref) continue;
    const server = String(ref.server || "").toLowerCase();
    let row = rows.find(function (candidate) {
      return candidate.address.toLowerCase() === server;
    });
    if (!row) {
      const country = normalizedCode(ref.country_code);
      const city = normalizedCode(ref.city_code);
      row = rows.find(function (candidate) {
        return normalizedCode(candidate.countryCode) === country &&
          (!city || normalizedCode(candidate.cityCode) === city);
      });
    }
    if (!row) continue;
    const key = row.address.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(row);
  }
  return out;
}

function withState(row, section, favorite, recent) {
  return Object.assign({}, row, {
    section: section,
    favorite: !!favorite,
    recent: !!recent,
  });
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

  const refresh = el("button", "button button--tonal loc__refresh", t("common.refresh"));
  refresh.type = "button";
  const pingAll = el("button", "button button--tonal loc__ping-all", t("locations.ping_all"));
  pingAll.type = "button";
  const locked = el("button", "button button--text loc__locked-action", t("locations.show_locked"));
  locked.type = "button";
  const actions = el("div", "loc__actions");
  actions.append(refresh, pingAll, locked);

  const filterLabel = el("label", "field__label", t("locations.search_label"));
  filterLabel.htmlFor = "loc-filter";
  const filter = document.createElement("input");
  filter.type = "text";
  filter.id = "loc-filter";
  filter.className = "input";
  filter.placeholder = t("locations.search_placeholder");
  filter.autocomplete = "off";
  filter.spellcheck = false;
  const filterField = el("div", "field loc__field");
  filterField.append(filterLabel, filter);

  const list = el("div", "card loc__list");
  list.setAttribute("role", "listbox");
  list.setAttribute("aria-label", t("locations.list_aria"));
  list.tabIndex = 0;
  list.hidden = true;

  const empty = el("p", "loc__empty");
  empty.hidden = true;

  const status = el("p", "loc__status");
  status.setAttribute("aria-live", "polite");
  status.hidden = true;
  const details = el("p", "loc__details");
  details.dir = "ltr";
  details.hidden = true;

  root.append(actions, filterField, list, empty, status, details);
  host.append(root);

  return {
    root: root,
    refresh: refresh,
    pingAll: pingAll,
    locked: locked,
    filter: filter,
    list: list,
    empty: empty,
    status: status,
    details: details,
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
      sections: [],
      rows: [],
      fullRows: [],
      rowsSignature: "",
      rowEls: [],
      current: 0,
      sortMode: "country",
      scrollToTop: false,
      // Ping-all run state. The daemon's ping_done_seq ends it.
      pingBusy: false,
      pingSawBusy: false,
      pingHosts: [],
      pingBaseline: {},
      pingAnswered: false,
      pingDoneSeq: 0,
      frozenLatency: null,
      favoriteOverrides: {},
      // DOM-render bookkeeping so envelopes only touch what changed.
      placeholder: null,
      statusText: null,
      statusTone: null,
      statusDetail: null,
    };

    function isSpeedFailure(notice) {
      return !!notice && notice.code === "locations.ping.failed";
    }

    function loadError(snapshot) {
      const notice = snapshot.locations_notice;
      if (!notice || notice.tone !== "bad" || isSpeedFailure(notice)) return "";
      return noticeText(notice).text;
    }

    function visibleRows(snapshot) {
      const allowLocked = !!snapshot.allow_locked;
      return buildRows(view.locations).filter(function (row) {
        return !row.locked || allowLocked;
      });
    }

    function sectionsFor(snapshot) {
      const rows = visibleRows(snapshot);
      const latency = latencySamples(snapshot);
      const favorites = resolvedRefRows(snapshot.favorites, rows);
      favorites.sort(function (a, b) {
        const country = a.countryName.toLowerCase().localeCompare(b.countryName.toLowerCase());
        if (country !== 0) return country;
        const city = a.cityName.toLowerCase().localeCompare(b.cityName.toLowerCase());
        if (city !== 0) return city;
        return a.address.toLowerCase().localeCompare(b.address.toLowerCase());
      });
      const recents = resolvedRefRows(snapshot.recents, rows);
      const favoriteKeys = new Set(favorites.map(function (row) { return row.address.toLowerCase(); }));
      const recentKeys = new Set(recents.map(function (row) { return row.address.toLowerCase(); }));
      // Drop optimistic stars once the authoritative state agrees.
      for (const address of Object.keys(view.favoriteOverrides)) {
        if (favoriteKeys.has(address) === view.favoriteOverrides[address]) {
          delete view.favoriteOverrides[address];
        }
      }
      const isFavorite = function (row) {
        const key = row.address.toLowerCase();
        return Object.prototype.hasOwnProperty.call(view.favoriteOverrides, key)
          ? view.favoriteOverrides[key]
          : favoriteKeys.has(key);
      };

      let fastest = null;
      let fastestValue = Infinity;
      for (const row of rows) {
        const value = sampleValue(latency[row.address.toLowerCase()]);
        if (value < fastestValue) {
          fastestValue = value;
          fastest = row;
        }
      }
      const fastestRows = [fastest
        ? withState(fastest, "fastest", false, false)
        : {
          footer: false,
          address: "",
          title: t("locations.fastest"),
          detail: t("locations.fastest_empty_detail"),
          disabled: true,
          fastest: true,
          section: "fastest",
          favorite: false,
          recent: false,
        }];
      if (fastest) {
        fastestRows[0].fastest = true;
        fastestRows[0].disabled = false;
        fastestRows[0].title = t("locations.fastest_with", { title: fastest.title });
      }

      const favoriteRows = favorites.map(function (row) {
        return withState(row, "favorites", isFavorite(row), recentKeys.has(row.address.toLowerCase()));
      });
      const recentRows = recents.map(function (row) {
        return withState(row, "recent", isFavorite(row), true);
      });

      const sortLatency = view.pingBusy && view.frozenLatency ? view.frozenLatency : latency;
      const full = sortRows(
        matchingRows(rows, view.filter),
        view.sortMode,
        sortLatency,
      ).map(function (row) {
        return withState(
          row,
          "locations",
          isFavorite(row),
          recentKeys.has(row.address.toLowerCase()),
        );
      });

      const sections = [{ id: "fastest", title: t("locations.fastest"), rows: fastestRows }];
      if (favoriteRows.length > 0) {
        sections.push({ id: "favorites", title: t("locations.favorites"), rows: favoriteRows });
      }
      if (recentRows.length > 0) {
        sections.push({ id: "recent", title: t("locations.recent"), rows: recentRows, clearable: true });
      }
      sections.push({ id: "locations", title: t("locations.locations"), rows: full });
      return { sections: sections, full: full };
    }

    function allTargets() {
      const snapshot = ctx.store.get();
      const seen = new Set();
      const hosts = [];
      for (const row of visibleRows(snapshot)) {
        const key = row.address.toLowerCase();
        if (seen.has(key)) continue;
        seen.add(key);
        hosts.push(row.address);
      }
      return hosts;
    }

    /**
     * Adopt new served data. A changed list is a new visit: country order,
     * top of the list and no stale local latency ordering. Pinned state
     * changes arrive in the same state envelope and update markers without
     * resetting the list.
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

    function rebuildRows() {
      const frag = document.createDocumentFragment();
      const els = [];
      let lastSection = null;
      view.rows.forEach(function (row, index) {
        if (row.section !== lastSection) {
          lastSection = row.section;
          const section = view.sections.find(function (entry) { return entry.id === row.section; });
          const heading = el("div", "loc__heading");
          heading.setAttribute("role", "presentation");
          const title = el("h3", "loc__heading-title", section ? section.title : "");
          heading.append(title);
          if (section && section.clearable) {
            const clear = el("button", "button button--text loc__clear", t("locations.clear"));
            clear.type = "button";
            clear.setAttribute("aria-label", t("locations.clear_recent_aria"));
            clear.addEventListener("click", onClearRecents);
            heading.append(clear);
          }
          frag.append(heading);
        }

        const rowEl = el("div", "loc__row");
        if (row.disabled) rowEl.classList.add("loc__row--disabled");
        rowEl.id = "loc-row-" + index;
        rowEl.dataset.index = String(index);
        rowEl.setAttribute("role", "option");
        rowEl.setAttribute("aria-selected", "false");
        if (row.disabled) rowEl.setAttribute("aria-disabled", "true");

        const flag = el("span", "loc__flag icon--keep");
        flag.setAttribute("aria-hidden", "true");
        if (row.countryCode) flag.append(flagIcon(row.countryCode));
        rowEl.append(flag);

        const main = el("span", "loc__main");
        const detail = el("span", "loc__detail");
        detail.append(el("bdi", null, row.detail));
        main.append(
          el("span", "loc__title", row.title),
          detail,
        );
        rowEl.append(main);

        const trailing = el("span", "loc__trailing");
        if (row.locked) {
          const badge = el("span", "loc__locked-badge", t("locations.locked"));
          badge.setAttribute("aria-label", t("locations.locked"));
          trailing.append(badge);
        }
        const latency = el("span", "loc__latency");
        const latencyText = el("bdi");
        latency.append(latencyText);
        const star = el("button", "button loc__star icon--keep");
        star.type = "button";
        star.hidden = !!row.fastest;
        star.addEventListener("click", function (event) {
          event.stopPropagation();
          onFavorite(row);
        });
        const check = el("span", "loc__check icon--keep", "✓");
        check.setAttribute("aria-hidden", "true");
        check.hidden = true;
        trailing.append(latency, star, check);
        rowEl.append(trailing);

        frag.append(rowEl);
        els.push({ el: rowEl, latency: latencyText, star: star, check: check });
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

    /** Per-envelope row updates: latency, selected mark, star and current. */
    function renderRowState(snapshot, live) {
      for (let i = 0; i < view.rows.length; i++) {
        const row = view.rows[i];
        const entry = view.rowEls[i];
        const label = latencyLabel(snapshot, row, view.pingBusy);
        const latency = label || "—";
        if (entry.latency.textContent !== latency) entry.latency.textContent = latency;

        if (!entry.star.hidden) {
          const pressed = row.favorite ? "true" : "false";
          if (entry.star.getAttribute("aria-pressed") !== pressed) {
            entry.star.setAttribute("aria-pressed", pressed);
          }
          const want = row.favorite ? "★" : "☆";
          if (entry.star.textContent !== want) entry.star.textContent = want;
          const name = [row.countryName, row.cityName].filter(Boolean).join(" ");
          const labelText = row.favorite
            ? t("locations.unfavorite", { name: name })
            : t("locations.favorite", { name: name });
          if (entry.star.getAttribute("aria-label") !== labelText) {
            entry.star.setAttribute("aria-label", labelText);
          }
        }

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
      ui.list.hidden = empty;
      ui.empty.hidden = !empty;
      let placeholder = "";
      if (empty) {
        placeholder = !snapshot.locations_loaded && !loadError(snapshot)
          ? t("locations.loading")
          : view.filter.trim() !== ""
            ? t("locations.no_match")
            : t("locations.empty");
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
      if (!snapshot.locations_loaded && !error) return { text: t("locations.loading_status"), tone: "", detail: "" };
      if (error) {
        return { text: error, tone: "bad", detail: noticeText(notice).detail };
      }
      if (view.pingBusy) {
        return { text: t("locations.pinging", { count: formatNumber(view.pingHosts.length) }), tone: "", detail: "" };
      }
      if (isSpeedFailure(notice)) {
        const failed = noticeText(notice);
        return { text: failed.text, tone: "bad", detail: failed.detail };
      }
      if (snapshot.locations_loaded && view.fullRows.length === 0 && view.filter.trim() !== "") {
        return { text: t("locations.no_match_status"), tone: "", detail: "" };
      }
      const shared = snapshot.notice;
      if (shared) {
        const presentation = noticeText(shared);
        if (presentation.text) return { text: presentation.text, tone: shared.tone || "", detail: presentation.detail };
      }
      return { text: "", tone: "", detail: "" };
    }

    function renderStatus(snapshot) {
      const line = statusLine(snapshot);
      if (line.text === view.statusText && line.tone === view.statusTone && line.detail === view.statusDetail) return;
      view.statusText = line.text;
      view.statusTone = line.tone;
      view.statusDetail = line.detail;
      ui.status.textContent = line.text;
      ui.status.title = line.text;
      if (line.tone) ui.status.dataset.tone = line.tone;
      else delete ui.status.dataset.tone;
      ui.status.hidden = line.text === "";
      ui.details.textContent = line.detail;
      ui.details.hidden = line.detail === "";
    }

    function renderActions(snapshot) {
      const busy = view.pingBusy;
      ui.refresh.disabled = busy;
      ui.pingAll.disabled = busy || allTargets().length === 0;
      const showLocked = !!snapshot.allow_locked;
      const label = t(showLocked ? "locations.hide_locked" : "locations.show_locked");
      if (ui.locked.textContent !== label) ui.locked.textContent = label;
      ui.locked.setAttribute("aria-pressed", showLocked ? "true" : "false");
    }

    function endPing() {
      view.pingBusy = false;
      view.pingHosts = [];
      view.pingBaseline = {};
      view.pingAnswered = false;
      view.frozenLatency = null;
    }

    function finishPing(answered) {
      if (!view.pingBusy) return;
      endPing();
      if (answered) {
        view.sortMode = "speed";
        view.current = 0;
        view.scrollToTop = true;
      }
    }

    /**
     * pollPing ends the run only on a ping_done_seq change. Results merely
     * update labels and record whether anything answered.
     */
    function pollPing(snapshot) {
      if (!view.pingBusy) return;
      const seq = Number(snapshot.ping_done_seq || 0);
      if (seq !== view.pingDoneSeq) {
        finishPing(view.pingAnswered);
        return;
      }
      const busy = !!snapshot.ping_busy;
      if (busy) {
        view.pingSawBusy = true;
      } else if (view.pingSawBusy) {
        finishPing(view.pingAnswered);
        return;
      }
      const rtts = liveRTTs(snapshot);
      for (const host of view.pingHosts) {
        const key = host.toLowerCase();
        const now = rtts[key];
        if (now === undefined) continue;
        if (view.pingBaseline[key] === undefined || now !== view.pingBaseline[key]) {
          view.pingAnswered = true;
        }
      }
    }

    function render() {
      const snapshot = ctx.store.get();
      syncData(snapshot);
      pollPing(snapshot);

      const built = sectionsFor(snapshot);
      view.sections = built.sections;
      view.rows = built.sections.flatMap(function (section) { return section.rows; });
      view.fullRows = built.full;
      const signature = view.rows.map(function (row) {
        return [
          row.section,
          row.key || row.address,
          row.title,
          row.detail,
          row.favorite ? "f" : "",
          row.recent ? "r" : "",
          row.locked ? "l" : "",
          row.disabled ? "d" : "",
        ].join("\u0000");
      }).join("\n");
      if (signature !== view.rowsSignature) {
        view.rowsSignature = signature;
        rebuildRows();
      }
      clampCurrent();
      renderRowState(snapshot);
      renderList(snapshot);
      renderStatus(snapshot);
      renderActions(snapshot);
    }

    function beginPing(hosts) {
      const snapshot = ctx.store.get();
      const baseline = {};
      for (const host of hosts) {
        baseline[host.toLowerCase()] = liveRTTs(snapshot)[host.toLowerCase()];
      }
      view.pingBusy = true;
      view.pingSawBusy = false;
      view.pingHosts = hosts.slice();
      view.pingBaseline = baseline;
      view.pingAnswered = false;
      view.pingDoneSeq = Number(snapshot.ping_done_seq || 0);
      view.frozenLatency = latencySamples(snapshot);
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
          finishPing(false);
          render();
        }
        console.warn("kumiho: locations ping all refused: " + err);
      });
    }

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

    function onFavorite(row) {
      if (!row || row.fastest || row.disabled || !row.address) return;
      const favorite = !row.favorite;
      view.favoriteOverrides[row.address.toLowerCase()] = favorite;
      view.rowsSignature = "";
      render();
      ctx.send("favorite_location", {
        country_code: row.countryCode,
        city_code: row.cityCode,
        server: row.address,
        favorite: favorite,
      }).then(function (err) {
        if (!err) return;
        delete view.favoriteOverrides[row.address.toLowerCase()];
        view.rowsSignature = "";
        render();
        console.warn("kumiho: locations favorite refused: " + err);
      });
    }

    function onClearRecents() {
      ctx.send("clear_recents", {}).then(function (err) {
        if (err) console.warn("kumiho: locations clear_recents refused: " + err);
      });
    }

    function onLocked() {
      const snapshot = ctx.store.get();
      const next = snapshot.allow_locked ? "off" : "on";
      ctx.send("settings_set", { key: "allow_locked", value: next }).then(function (err) {
        if (err) console.warn("kumiho: locations allow_locked refused: " + err);
      });
    }

    function selectServer(row) {
      if (!row || row.disabled) return;
      if (row.fastest) {
        ctx.send("select_location", { fastest: true }).then(function (err) {
          if (err) console.warn("kumiho: locations select_location refused: " + err);
        });
        return;
      }
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
    ui.locked.addEventListener("click", onLocked);
    ui.filter.addEventListener("input", onFilter);
    ui.list.addEventListener("keydown", onListKey);
    ui.list.addEventListener("click", onListClick);

    const unsubscribe = ctx.store.subscribe(render);

    // Teardown: only the subscription outlives the screen's nodes (every
    // listener above dies with the router's replaceChildren).
    cleanup = function () {
      unsubscribe();
    };
  },

  unmount() {
    if (cleanup) cleanup();
    cleanup = null;
  },
};
