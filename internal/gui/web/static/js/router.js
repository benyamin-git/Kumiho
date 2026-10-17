/* Kumiho hash router and the screen registry.
 *
 * Routes are `#/<screen>` with the capture-hint form `#/<screen>?state=<name>`
 * (scripts/visual/README.md): the router parses the screen id, passes the
 * hint through to the screen's context, and tolerates unknown hint values
 * (dialog and wizard states bind to them as T2.2–T3.4 land).
 *
 * The six rail destinations are the shell's navigation; the DOM rail in
 * index.html is the single source for their ids and titles. A route to an
 * unregistered screen renders a neutral scaffold empty-state built from the
 * shared primitives, so captures and deep links already work.
 *
 * The onboarding wizard (T3.4) is a registered screen that is not a rail
 * destination: while the wire's always-present `onboarded` flag is not true
 * the router gates every route to `onboarding`, so the first-run user can
 * never reach another destination (inventory §Onboarding wizard). The gate
 * lifts the moment {@link State.Onboarded} flips true (the wizard's `Open
 * Kumiho`), and the router's store subscription then shows whatever route
 * the hash asks for. While the wizard is the shown route the shell chrome
 * steps aside (`app--wizard`, css/screens/onboarding.css), so it occupies
 * the window; the wizard's hint selects its step for captures.
 *
 * The last route actually shown is remembered in sessionStorage (best effort;
 * the store may be unavailable). A theme or accent change reloads the
 * document at the bare host URL, which drops the fragment: on boot with no
 * explicit screen (an empty hash or a bare `#`) the router restores the
 * remembered route when it still parses to a registered screen, so the change
 * keeps the user on the screen they were on instead of booting to Dashboard.
 * An explicit hash (a deep link, or a capture hint such as
 * `#/settings?state=dark`) always wins and is never overridden.
 *
 * Screen modules (static/js/screens/<id>.js) default-export an object:
 *
 *   export default {
 *     mount(host, ctx) { … },   // render into host (an HTMLElement)
 *     unmount() { … }           // optional cleanup before the next screen
 *   };
 *
 * with ctx = { store, send, hint, navigate, title }. Register each module in
 * the table below as its task lands — one line, for example:
 *
 *   registerScreen("dashboard", () => import("./screens/dashboard.js"),
 *     "css/screens/dashboard.css");
 *
 * The table is static on purpose: probing `./screens/<id>.js` by import would
 * 404 for every unbuilt screen (console noise the capture run treats as an
 * error). A screen registered but failing to load falls back to the
 * scaffold.
 *
 * The registry entry's third argument is the screen's stylesheet path
 * (relative to the document, so the shell's per-run token segment stays
 * intact): the router links it when the screen mounts and removes it when
 * the screen unmounts, so screens never manage stylesheet elements
 * themselves and a mounted screen is exactly its own module plus its own
 * stylesheet.
 */

import { store } from "./store.js";

const ID_RE = /^[a-z0-9][a-z0-9-]*$/;
const MAX_ID = 32;
const DEFAULT_SCREEN = "dashboard";

// Namespaced sessionStorage key for the last route actually shown, so a
// fragment-less reload keeps the current screen instead of booting to
// Dashboard.
const LAST_ROUTE_KEY = "kumiho:last-route";

// The wizard's screen id (T3.4): the gate target and the one route that
// hides the shell chrome.
const WIZARD_SCREEN = "onboarding";

/** id -> { load: () => Promise<module>, css: string }; filled by registerScreen. */
const loaders = new Map();

/** Register a screen module loader and its stylesheet under a route id. */
export function registerScreen(id, load, css) {
  if (!ID_RE.test(id)) throw new Error("kumiho: bad screen id " + JSON.stringify(id));
  loaders.set(id, { load: load, css: css || "" });
}

// The screen table: one line per screen as its task lands.
registerScreen("dashboard", () => import("./screens/dashboard.js"), "css/screens/dashboard.css");
registerScreen("locations", () => import("./screens/locations.js"), "css/screens/locations.css");
registerScreen("settings", () => import("./screens/settings.js"), "css/screens/settings.css");
registerScreen("account", () => import("./screens/account.js"), "css/screens/account.css");
registerScreen("logs", () => import("./screens/logs.js"), "css/screens/logs.css");
registerScreen("diagnostics", () => import("./screens/diagnostics.js"), "css/screens/diagnostics.css");
registerScreen("onboarding", () => import("./screens/onboarding.js"), "css/screens/onboarding.css");

/** Parse a location hash into {id, hint}, tolerating anything malformed. */
export function parseHash(hash) {
  const raw = String(hash || "").replace(/^#\/?/, "");
  const qi = raw.indexOf("?");
  const path = (qi >= 0 ? raw.slice(0, qi) : raw).trim();
  const params = new URLSearchParams(qi >= 0 ? raw.slice(qi + 1) : "");
  const id = ID_RE.test(path) && path.length <= MAX_ID ? path : DEFAULT_SCREEN;
  let hint = params.get("state") || "";
  if (!ID_RE.test(hint) || hint.length > MAX_ID) hint = "";
  return { id: id, hint: hint };
}

/** The canonical hash for a route. */
export function routeHash(route) {
  return "#/" + route.id + (route.hint ? "?state=" + route.hint : "");
}

/** Navigate to a screen (optionally carrying a capture hint). */
export function navigate(id, hint) {
  location.hash = routeHash({ id: id, hint: hint || "" });
}

/** Whether the hash carries an explicit screen, as opposed to empty or a bare
 * `#`/`#/`, which let a remembered route apply on boot. Any other hash - a
 * deep link, a capture hint, even a malformed one - is explicit and wins. */
function hasExplicitScreen(hash) {
  return String(hash || "").replace(/^#\/?/, "").trim() !== "";
}

/** Remember the route just shown (best effort; sessionStorage may be off). */
function rememberRoute(route) {
  try {
    sessionStorage.setItem(LAST_ROUTE_KEY, JSON.stringify({ id: route.id, hint: route.hint || "" }));
  } catch (err) {
    // Private browsing or a disabled store: the memory is a convenience only.
  }
}

/** The remembered route, or null when absent, unparseable, or neither a valid
 * nor a registered screen id. */
function recallRoute() {
  let stored;
  try {
    stored = sessionStorage.getItem(LAST_ROUTE_KEY);
  } catch (err) {
    return null;
  }
  if (!stored) return null;
  let route;
  try {
    route = JSON.parse(stored);
  } catch (err) {
    return null;
  }
  if (!route || typeof route.id !== "string") return null;
  if (!ID_RE.test(route.id) || route.id.length > MAX_ID || !loaders.has(route.id)) return null;
  let hint = typeof route.hint === "string" ? route.hint : "";
  if (!ID_RE.test(hint) || hint.length > MAX_ID) hint = "";
  return { id: route.id, hint: hint };
}

function capitalize(id) {
  return id.charAt(0).toUpperCase() + id.slice(1);
}

function renderScaffold(host, title) {
  const wrap = document.createElement("div");
  wrap.className = "empty-state";
  const heading = document.createElement("p");
  heading.className = "empty-state__title";
  heading.textContent = "Nothing here yet";
  const detail = document.createElement("p");
  detail.className = "empty-state__detail";
  detail.textContent = "The " + title + " screen is not built yet.";
  wrap.append(heading, detail);
  host.append(wrap);
}

/**
 * Start routing. opts: { host, title, send } — the content element, the
 * header title element, and the transport's send(). Renders the initial
 * route immediately and re-renders on hash changes.
 */
export function initRouter(opts) {
  const host = opts.host;
  const titleEl = opts.title;
  const send = opts.send;
  const links = Array.from(document.querySelectorAll(".rail__item[data-screen]"));
  const titles = new Map(links.map((el) => [el.dataset.screen, el.textContent.trim()]));

  const appEl = document.querySelector(".app");

  let generation = 0;
  let current = null;
  let currentCSS = null; // the mounted screen's <link>, linked by the router
  let currentRoute = null; // the effective route actually shown

  /**
   * The first-run gate (T3.4/inventory §Onboarding wizard): while the state's
   * always-present `onboarded` flag is not true the wizard owns the window.
   * Before the first envelope arrives nothing is pinned, so a returning user
   * never sees a wizard flash while the initial push is in flight.
   */
  function wizardPending() {
    return store.hasState() && store.get().onboarded !== true;
  }

  /** The route actually shown: every request gates to the wizard while it
   * is pending, keeping the requested wizard hint so captures can select a
   * step. */
  function effectiveRoute(requested) {
    if (!wizardPending()) return requested;
    return { id: WIZARD_SCREEN, hint: requested.id === WIZARD_SCREEN ? requested.hint : "" };
  }

  function routeKey(route) {
    return route.id + (route.hint ? "?state=" + route.hint : "");
  }

  /** Resolve, compare and show; same-route requests are no-ops, so the
   * mounted wizard keeps its in-flight step across state pushes. */
  function routeTo(requested, moveFocus) {
    const route = effectiveRoute(requested);
    if (currentRoute && routeKey(route) === routeKey(currentRoute)) return;
    show(route, moveFocus);
  }

  function linkScreenCSS(id, href) {
    const link = document.createElement("link");
    link.id = "kumiho-screen-" + id;
    link.rel = "stylesheet";
    link.href = href;
    document.head.append(link);
    currentCSS = link;
  }

  function unlinkScreenCSS() {
    if (!currentCSS) return;
    currentCSS.remove();
    currentCSS = null;
  }

  async function show(route, moveFocus) {
    const gen = ++generation;
    currentRoute = route;
    // Remember only routes the hash asks for: a state push with a bare host
    // URL defaults to Dashboard and must not clobber the remembered screen.
    if (hasExplicitScreen(location.hash)) rememberRoute(route);

    // The wizard takes the whole window: the shell chrome hides while it is
    // the shown route (css/screens/onboarding.css owns the rule).
    if (appEl) appEl.classList.toggle("app--wizard", route.id === WIZARD_SCREEN);

    for (const link of links) {
      if (link.dataset.screen === route.id) link.setAttribute("aria-current", "page");
      else link.removeAttribute("aria-current");
    }
    const title = titles.get(route.id) || capitalize(route.id);
    titleEl.textContent = title;

    if (current && typeof current.unmount === "function") {
      try {
        current.unmount();
      } catch (err) {
        console.error("kumiho: " + route.id + " unmount failed", err);
      }
    }
    current = null;
    unlinkScreenCSS();
    host.replaceChildren();

    const entry = loaders.get(route.id);
    if (!entry) {
      renderScaffold(host, title);
      if (moveFocus) host.focus({ preventScroll: true });
      return;
    }

    let screen = null;
    try {
      const mod = await entry.load();
      if (gen !== generation) return; // a newer route won the race
      screen = mod && mod.default ? mod.default : mod;
    } catch (err) {
      if (gen !== generation) return;
      console.error("kumiho: screen " + route.id + " failed to load", err);
    }

    if (screen && typeof screen.mount === "function") {
      if (entry.css) linkScreenCSS(route.id, entry.css);
      try {
        screen.mount(host, { store: store, send: send, hint: route.hint, navigate: navigate, title: title });
        current = screen;
      } catch (err) {
        console.error("kumiho: screen " + route.id + " failed to mount", err);
        unlinkScreenCSS();
        host.replaceChildren();
        renderScaffold(host, title);
      }
    } else {
      renderScaffold(host, title);
    }
    // Move focus into the content so keyboard and screen reader users land
    // on the new screen (the shell's rail stays put).
    if (moveFocus) host.focus({ preventScroll: true });
  }

  window.addEventListener("hashchange", () => {
    routeTo(parseHash(location.hash), true);
  });

  // Every state envelope re-evaluates the gate: the wizard's `Open Kumiho`
  // flips `onboarded` mid-session (from the wizard's own navigation, or from
  // the hash the user was on when the wizard opened), and the router must
  // then show the route the hash asks for.
  store.subscribe(() => {
    routeTo(parseHash(location.hash), false);
  });

  // A fragment-less boot (the theme/accent reload) restores the last shown
  // route; an explicit hash always wins.
  const initial = hasExplicitScreen(location.hash) ? parseHash(location.hash) : recallRoute() || parseHash(location.hash);
  if (location.hash !== routeHash(initial)) {
    history.replaceState(null, "", routeHash(initial));
  }
  routeTo(initial, false);
}
