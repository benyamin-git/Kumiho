/* Kumiho app boot: load the selected catalog before the shell paints, wire
 * the transport into the store, render the header's status word, and start
 * the router. Loaded as the one module entry point; transport.js is the
 * classic <head> script that defines window.kumihoReceive and exposes
 * window.KumihoTransport before this runs.
 *
 * Language order: `?lang=fa|en` (captures/dev), then the server-injected
 * `<meta name="kumiho-language">` (the gui.toml first-paint cache), then
 * English. A controller snapshot is authoritative afterwards: a language
 * changed from Settings, the CLI or another client switches this page in
 * place and refreshes the first-paint cache with prefs_set. The query
 * override deliberately skips that reconciliation so a capture can pin a
 * language regardless of the mock's engine state.
 */

import { store, connectionStatus } from "./store.js";
import { initI18n, setLanguage, currentLanguage } from "./i18n.js";
import { initRouter } from "./router.js";

const transport = window.KumihoTransport;
const titleEl = document.getElementById("screen-title");
const statusEl = document.getElementById("conn-status");
const host = document.getElementById("screen-host");

// Host -> store: every state envelope replaces the app state. Subscribe
// before the catalog loads so an early push is never missed.
transport.subscribe((state) => store.set(state));

function queryLanguage() {
  const value = (new URLSearchParams(location.search).get("lang") || "").toLowerCase();
  return value === "en" || value === "fa" ? value : "";
}

function metaLanguage() {
  const meta = document.querySelector('meta[name="kumiho-language"]');
  const value = meta ? String(meta.getAttribute("content") || "").toLowerCase() : "";
  return value === "en" || value === "fa" ? value : "";
}

const forcedLanguage = queryLanguage();
const firstPaintLanguage = forcedLanguage || metaLanguage() || "en";

async function boot() {
  await initI18n(firstPaintLanguage);
  document.body.classList.remove("app-booting");

  // Header status word, the tray's wording (store.connectionStatus). The DOM
  // is touched only when the word or tone actually changes: envelopes arrive
  // on every change, including the per-second uptime tick.
  let shown = null;
  store.subscribe((state) => {
    const status = connectionStatus(state, store.hasState());
    if (shown && shown.word === status.word && shown.tone === status.tone) return;
    shown = status;
    statusEl.textContent = status.word;
    statusEl.dataset.tone = status.tone;
  });

  const router = initRouter({ host: host, title: titleEl, send: transport.send });
  window.addEventListener("kumiho:languagechange", () => router.refresh());

  let appliedLanguage = currentLanguage();
  store.subscribe((state) => {
    if (forcedLanguage) return;
    const effective = state && (state.language === "fa" || state.language === "en") ? state.language : "";
    if (!effective || effective === appliedLanguage) return;
    appliedLanguage = effective;
    setLanguage(effective)
      .then(() => transport.send("prefs_set", { language: effective }))
      .then((err) => {
        if (err) console.warn("kumiho: could not cache the language: " + err);
      })
      .catch((err) => console.error("kumiho: language switch failed", err));
  });
}

boot().catch((err) => {
  // A failed catalog must never leave the shell invisible; English fallback
  // and the key names keep the UI usable.
  console.error("kumiho: boot failed", err);
  document.body.classList.remove("app-booting");
});
