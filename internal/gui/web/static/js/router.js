/* Kumiho hash router and the screen registry.
 *
 * Routes are `#/<screen>` with the capture-hint form `#/<screen>?state=<name>`
 * (scripts/visual/README.md): the router parses the screen id, passes the
 * hint through to the screen's context, and tolerates unknown hint values
 * (dialog and wizard states bind to them as T2.2–T3.4 land).
 *
 * The six rail destinations are the shell's navigation; the DOM rail in
 * index.html is the single source for their ids and titles. A route to an
 * unregistered screen (onboarding until T3.4, any destination before its
 * screen task) renders a neutral scaffold empty-state built from the shared
 * primitives, so captures and deep links already work.
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
 *   registerScreen("dashboard", () => import("./screens/dashboard.js"));
 *
 * The table is static on purpose: probing `./screens/<id>.js` by import would
 * 404 for every unbuilt screen (console noise the capture run treats as an
 * error). A screen registered but failing to load falls back to the
 * scaffold.
 */

import { store } from "./store.js";

const ID_RE = /^[a-z0-9][a-z0-9-]*$/;
const MAX_ID = 32;
const DEFAULT_SCREEN = "dashboard";

/** id -> () => Promise<module>; filled by registerScreen. */
const loaders = new Map();

/** Register a screen module loader under its route id. */
export function registerScreen(id, load) {
  if (!ID_RE.test(id)) throw new Error("kumiho: bad screen id " + JSON.stringify(id));
  loaders.set(id, load);
}

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

  let generation = 0;
  let current = null;

  async function show(route, moveFocus) {
    const gen = ++generation;

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
    host.replaceChildren();

    const load = loaders.get(route.id);
    if (!load) {
      renderScaffold(host, title);
      if (moveFocus) host.focus({ preventScroll: true });
      return;
    }

    let screen = null;
    try {
      const mod = await load();
      if (gen !== generation) return; // a newer route won the race
      screen = mod && mod.default ? mod.default : mod;
    } catch (err) {
      if (gen !== generation) return;
      console.error("kumiho: screen " + route.id + " failed to load", err);
    }

    if (screen && typeof screen.mount === "function") {
      try {
        screen.mount(host, { store: store, send: send, hint: route.hint, navigate: navigate, title: title });
        current = screen;
      } catch (err) {
        console.error("kumiho: screen " + route.id + " failed to mount", err);
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
    show(parseHash(location.hash), true);
  });

  const initial = parseHash(location.hash);
  if (location.hash !== routeHash(initial)) {
    history.replaceState(null, "", routeHash(initial));
  }
  show(initial, false);
}
