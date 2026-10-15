/* Kumiho app boot: wire the transport into the store, render the header's
 * status word, and start the router. Loaded as the one module entry point;
 * transport.js is the classic <head> script that defines
 * window.kumihoReceive and exposes window.KumihoTransport before this runs.
 */

import { store, connectionStatus } from "./store.js";
import { initRouter } from "./router.js";

const transport = window.KumihoTransport;
const titleEl = document.getElementById("screen-title");
const statusEl = document.getElementById("conn-status");
const host = document.getElementById("screen-host");

// Host -> store: every state envelope replaces the app state.
transport.subscribe((state) => store.set(state));

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

initRouter({ host: host, title: titleEl, send: transport.send });
