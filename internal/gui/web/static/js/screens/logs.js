/* The Logs screen (inventory §Logs): the level button (debug → info → warn →
 * error), the Follow toggle, Refresh, the Search row with Clear / Export /
 * Open log folder right-aligned, the log console (card, 24-unit lines, newest
 * at the bottom, tinted by level) and the status line.
 *
 * Everything renders from the store's state envelopes (the controller
 * projection); actions go through the transport and the screen re-renders
 * from the next envelope — "" means accepted, never completed (T1.7). The
 * controller owns the retained buffer (the snapshot merged with live entries,
 * capped at 2000) and the subscription: on mount the screen seeds from the
 * ring (`logs_snapshot`) and subscribes (`logs_subscribe`) at the selected
 * level; on unmount it unsubscribes. The level filter and the search are
 * applied client-side for display (the walk model's rule), while the level
 * also drives the snapshot/subscribe level — changing it re-seeds the buffer
 * at the new level, per the inventory.
 *
 * The `logs_loaded` flag (T3.2) distinguishes "Loading logs..." from a
 * settled empty ring: `logs` is omitted from the wire both before the first
 * snapshot and when the loaded ring is empty, so the flag is the only honest
 * loading signal (the same shape LocationsLoaded has).
 *
 * Follow is the console's auto-tail: any scroll away from the tail turns it
 * off and reaching the tail turns it back on (the scroll position is the
 * single source of truth, so the button word always matches); the Follow
 * button, the level button and the search input jump back to the tail, which
 * turns it on. The console is a role=log region with aria-live off — a
 * streaming 2000-line console must not fire screen-reader announcements —
 * focusable for the inventory's keyboard scrolling. Rows wrap instead of
 * ellipsizing so a long message is never hidden.
 *
 * The screen keeps level/search/follow for the session (module-level state,
 * the walk-era "kept for the next visit" behavior): defaults each launch are
 * level info, follow on, empty search (inventory §Logs, never persisted).
 *
 * Wording authority: the button labels, the status-line priority, the count
 * texts, both placeholder lines and the exact notice strings come from
 * inventory §Logs; the export/open-folder notices and errors are the
 * controller's (T3.2 rulings).
 *
 * The router links css/screens/logs.css from the registry entry.
 */

// The level cycle order, most verbose first (inventory: debug → info → warn →
// error → debug).
const LEVELS = ["debug", "info", "warn", "error"];

// The three-letter level tag carried in every line (logging.Level.Tag3):
// severity never rests on colour alone.
const TAG3 = { debug: "DBG", info: "INF", warn: "WRN", error: "ERR" };

// Console metrics: the inventory's 24-unit rows and the wheel's three rows
// per notch; smaller deltas are trackpad-scale and scroll natively.
const ROW_PX = 24;
const WHEEL_ROWS = 3;
const WHEEL_NOTCH_MIN = 40;

// How close to the tail still counts as "at the tail" (sub-pixel rounding).
const TAIL_EPSILON_PX = 4;

// The session-local view state (inventory §Logs "Data sources": defaults each
// launch, never persisted).
const session = { level: "info", follow: true, search: "" };

// unmount()'s teardown, assigned by mount (the router mounts one screen at a
// time, so one slot is enough).
let cleanup = null;

function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

/** The next level in the inventory's cycle. */
function nextLevel(level) {
  const index = LEVELS.indexOf(level);
  return LEVELS[(index + 1) % LEVELS.length];
}

/** The level's filter rank; an unknown name sorts as info (never matches). */
function levelRank(level) {
  const index = LEVELS.indexOf(level);
  return index < 0 ? 1 : index;
}

/** The visible entries: level minimum + case-insensitive tag/message search. */
function filterEntries(entries, level, search) {
  const min = levelRank(level);
  const query = String(search || "").trim().toLowerCase();
  const rows = [];
  for (const entry of entries) {
    if (levelRank(entry.level) < min) continue;
    if (query !== "") {
      const haystack = String(entry.tag || "").toLowerCase() + " " + String(entry.msg || "").toLowerCase();
      if (haystack.indexOf(query) < 0) continue;
    }
    rows.push(entry);
  }
  return rows;
}

/** Local wall-clock HH:MM:SS (the inventory's line prefix). */
function localTime(ts) {
  const date = new Date(ts);
  if (isNaN(date.getTime())) return "--:--:--";
  const pad = (n) => String(n).padStart(2, "0");
  return pad(date.getHours()) + ":" + pad(date.getMinutes()) + ":" + pad(date.getSeconds());
}

/** One console line: `HH:MM:SS LVL [tag] message`. */
function lineText(entry) {
  const level = String(entry.level || "info");
  const tag = String(entry.tag || "");
  return localTime(entry.ts) + " " + (TAG3[level] || "INF") + " [" + tag + "] " + String(entry.msg || "");
}

/** One entry's identity for the buffer/row change keys. */
function entryKey(entry) {
  return (entry.ts || "") + "\u0000" + (entry.level || "") + "\u0000" + (entry.tag || "") + "\u0000" + (entry.msg || "");
}

/**
 * The raw buffer's change key: length plus the first and last entries. The
 * controller's buffer only ever appends (with a trim at capacity), clears or
 * is replaced by a snapshot, so equal length plus equal ends means the same
 * buffer — envelopes arrive as fresh object graphs (never aliases), which
 * rules out identity checks.
 */
function bufferKey(entries) {
  if (entries.length === 0) return "0";
  return entries.length + "|" + entryKey(entries[0]) + "|" + entryKey(entries[entries.length - 1]);
}

/** Build the screen's DOM and return its element references. */
function build(host) {
  const root = el("div", "logs");

  // Action row: Level, Follow, Refresh (inventory left-to-right order).
  const level = el("button", "button button--tonal logs__level");
  level.type = "button";
  level.id = "logs-level";
  const follow = el("button", "button button--tonal logs__follow");
  follow.type = "button";
  follow.id = "logs-follow";
  const refresh = el("button", "button button--tonal logs__refresh", "Refresh");
  refresh.type = "button";
  refresh.id = "logs-refresh";
  const actions = el("div", "logs__actions");
  actions.append(level, follow, refresh);

  // Search row: the labelled field (always visible), then the right-aligned
  // Clear / Export / Open log folder actions.
  const searchLabel = el("label", "field__label", "Search");
  searchLabel.htmlFor = "logs-search";
  const search = document.createElement("input");
  search.type = "text";
  search.id = "logs-search";
  search.className = "input logs__search-input";
  search.placeholder = "Tag or message";
  search.autocomplete = "off";
  search.spellcheck = false;
  search.setAttribute("aria-label", "Search the logs");
  const field = el("div", "field logs__field");
  field.append(searchLabel, search);
  const spacer = el("div", "logs__spacer");
  const clear = el("button", "button button--tonal logs__clear", "Clear");
  clear.type = "button";
  clear.id = "logs-clear";
  const exportBtn = el("button", "button button--tonal logs__export", "Export");
  exportBtn.type = "button";
  exportBtn.id = "logs-export";
  const openFolder = el("button", "button button--tonal logs__open", "Open log folder");
  openFolder.type = "button";
  openFolder.id = "logs-open-folder";
  const searchRow = el("div", "logs__search-row");
  searchRow.append(field, spacer, clear, exportBtn, openFolder);

  // The console: a card that scrolls inside its own region (newest at the
  // bottom). role=log is the console's semantic role; aria-live is off on
  // purpose (see the header note). Keyboard focus enables the inventory's
  // Up/Down/PageUp/PageDown/Home/End scrolling.
  const list = el("div", "card logs__list");
  list.id = "logs-list";
  list.tabIndex = 0;
  list.setAttribute("role", "log");
  list.setAttribute("aria-live", "off");
  list.setAttribute("aria-label", "Log entries");

  // The quiet placeholder in place of the card while the stream has no
  // entries at all.
  const empty = el("p", "logs__empty", "No log entries yet.");
  empty.id = "logs-empty";
  empty.hidden = true;

  const status = el("p", "logs__status");
  status.setAttribute("aria-live", "polite");
  status.hidden = true;

  root.append(actions, searchRow, list, empty, status);
  host.append(root);

  return {
    root: root,
    level: level,
    follow: follow,
    refresh: refresh,
    search: search,
    clear: clear,
    exportBtn: exportBtn,
    openFolder: openFolder,
    list: list,
    empty: empty,
    status: status,
  };
}

export default {
  mount(host, ctx) {
    const ui = build(host);

    // Per-mount render bookkeeping; the filter/follow state itself lives in
    // the module-level session (kept across visits within the launch).
    const view = {
      renderKey: null,
      entries: [],
      rows: [],
      statusText: null,
      statusTone: null,
    };

    // Set by unmount: a seed reply that lands after the screen left must not
    // follow through with the chained re-subscribe (no leaked subscription).
    let disposed = false;

    /** Whether the console is scrolled to (or beyond) the tail. */
    function atTail() {
      return ui.list.scrollHeight - ui.list.scrollTop - ui.list.clientHeight <= TAIL_EPSILON_PX;
    }

    function jumpToTail() {
      ui.list.scrollTop = ui.list.scrollHeight;
    }

    /** The scroll position is the follow state: away → off, tail → on. */
    function syncFollow() {
      const tail = atTail();
      if (tail !== session.follow) {
        session.follow = tail;
        renderFollow();
      }
    }

    function renderFollow() {
      const text = "Follow: " + (session.follow ? "on" : "off");
      if (ui.follow.textContent !== text) ui.follow.textContent = text;
      ui.follow.setAttribute("aria-pressed", session.follow ? "true" : "false");
    }

    function renderLevel() {
      const text = "Level: " + session.level;
      if (ui.level.textContent !== text) ui.level.textContent = text;
    }

    /** Rebuild the console rows for the current buffer and filters. */
    function rebuild() {
      const fragment = document.createDocumentFragment();
      if (view.rows.length === 0 && view.entries.length > 0) {
        // A filter that hides everything inside a non-empty stream: the
        // inventory's first line of the card (the empty-stream placeholder
        // replaces the whole card instead).
        fragment.append(el("div", "logs__nomatch", "No entries match the filter"));
      }
      for (const entry of view.rows) {
        const line = el("div", "logs__line", lineText(entry));
        line.dataset.level = entry.level || "info";
        fragment.append(line);
      }
      ui.list.replaceChildren(fragment);
      const emptyStream = view.entries.length === 0;
      ui.list.hidden = emptyStream;
      ui.empty.hidden = !emptyStream;
      if (!emptyStream && session.follow) jumpToTail();
    }

    /** The status line, the inventory's priority: error → loading → 10 s
     * notice → count text. A failed load outranks the loading line so an
     * error is never masked. */
    function statusLine(snapshot) {
      const notice = snapshot.logs_notice;
      if (notice && notice.tone === "bad") return { text: notice.message, tone: "bad" };
      if (!snapshot.logs_loaded) return { text: "Loading logs...", tone: "" };
      if (notice && notice.message) {
        const tone = notice.tone === "good" || notice.tone === "warn" ? notice.tone : "";
        return { text: notice.message, tone: tone };
      }
      return { text: countText(), tone: "" };
    }

    /** The walk model's count wording, exactly. */
    function countText() {
      const shown = view.rows.length;
      const total = view.entries.length;
      if (total === 0) return "No log entries";
      if (shown === 0) return "No entries match the filter";
      if (shown !== total) return shown + " of " + total + " entries";
      if (total === 1) return "1 entry";
      return total + " entries";
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

    function render() {
      const snapshot = ctx.store.get();
      const entries = snapshot.logs || [];
      const key = session.level + "\u0000" + session.search + "\u0000" + bufferKey(entries);
      if (key !== view.renderKey) {
        view.renderKey = key;
        view.entries = entries;
        view.rows = filterEntries(entries, session.level, session.search);
        rebuild();
      }
      renderLevel();
      renderFollow();
      renderStatus(snapshot);
    }

    // seed re-runs the visit sequence: a fresh ring snapshot at the selected
    // level, then the live subscription at it (the ruling's order; the daemon
    // replaces an existing subscription, so a re-seed never leaks one). A
    // reply that lands after unmount is dropped: the chained subscribe must
    // not resurrect a subscription the leave already cancelled.
    function seed() {
      ctx.send("logs_snapshot", { level: session.level }).then(function (err) {
        if (err) {
          console.warn("kumiho: logs snapshot refused: " + err);
          return;
        }
        if (disposed) return;
        ctx.send("logs_subscribe", { level: session.level }).then(function (err2) {
          if (err2) console.warn("kumiho: logs subscribe refused: " + err2);
        });
      });
    }

    function onLevel() {
      session.level = nextLevel(session.level);
      session.follow = true;
      render();
      seed();
    }

    function onFollow() {
      session.follow = !session.follow;
      renderFollow();
      if (session.follow) jumpToTail();
    }

    function onSearch() {
      session.search = ui.search.value;
      session.follow = true;
      render();
    }

    function onRefresh() {
      seed();
    }

    function onClear() {
      ctx.send("logs_clear", {}).then(function (err) {
        if (err) console.warn("kumiho: logs clear refused: " + err);
      });
    }

    // Export writes the currently visible entries: the screen's local level
    // and search ride the payload, and the controller filters its retained
    // buffer with the same rule and hands the rendered text to the host's
    // writer (T3.2 ruling).
    function onExport() {
      ctx.send("logs_export", { level: session.level, search: session.search }).then(function (err) {
        if (err) console.warn("kumiho: logs export refused: " + err);
      });
    }

    function onOpenFolder() {
      ctx.send("open_logs_folder", {}).then(function (err) {
        if (err) console.warn("kumiho: open logs folder refused: " + err);
      });
    }

    function onListKey(event) {
      if (event.altKey || event.ctrlKey || event.metaKey) return;
      switch (event.key) {
        case "ArrowUp":
          ui.list.scrollTop -= ROW_PX;
          break;
        case "ArrowDown":
          ui.list.scrollTop += ROW_PX;
          break;
        case "PageUp":
          ui.list.scrollTop -= ui.list.clientHeight;
          break;
        case "PageDown":
          ui.list.scrollTop += ui.list.clientHeight;
          break;
        case "Home":
          ui.list.scrollTop = 0;
          break;
        case "End":
          jumpToTail();
          break;
        default:
          return;
      }
      event.preventDefault();
      syncFollow();
    }

    function onListWheel(event) {
      if (Math.abs(event.deltaY) < WHEEL_NOTCH_MIN) return;
      event.preventDefault();
      ui.list.scrollTop += (event.deltaY > 0 ? 1 : -1) * WHEEL_ROWS * ROW_PX;
      syncFollow();
    }

    ui.level.addEventListener("click", onLevel);
    ui.follow.addEventListener("click", onFollow);
    ui.refresh.addEventListener("click", onRefresh);
    ui.search.addEventListener("input", onSearch);
    ui.clear.addEventListener("click", onClear);
    ui.exportBtn.addEventListener("click", onExport);
    ui.openFolder.addEventListener("click", onOpenFolder);
    ui.list.addEventListener("scroll", syncFollow);
    ui.list.addEventListener("keydown", onListKey);
    ui.list.addEventListener("wheel", onListWheel, { passive: false });

    // Restore the session's filters into the input before the first render.
    ui.search.value = session.search;

    const unsubscribe = ctx.store.subscribe(render);
    seed();

    // Teardown: the store subscription, then the daemon-side subscription
    // (fire-and-forget; the controller drops racing events once it clears
    // its flag, and the disposed guard drops an in-flight seed's chained
    // re-subscribe). No leaked subscriptions across navigation.
    cleanup = function () {
      disposed = true;
      unsubscribe();
      ctx.send("logs_unsubscribe", {});
    };
  },

  unmount() {
    if (cleanup) cleanup();
    cleanup = null;
  },
};
