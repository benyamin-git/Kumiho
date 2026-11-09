/* The Blocked region guide (v0.11.0): the reusable first-run screen that
 * explains the two ways through an HTTP 451 region. It is not a wizard step:
 * the onboarding consent preflight, Settings → Region and the dashboard error
 * notice all navigate here, and the numbered wizard count stays four.
 *
 * Option 1 (another VPN) leads everywhere; the relay is the fallback and
 * keeps its honest boundary: it does not proxy the browser sign-in page.
 * The prepared card counts down the ten-minute pass from status.pass_expires
 * (an epoch, added for this screen) so the handoff survives a restart.
 */

const RELAY_HINT = "Only blocked Mozilla requests use the relay; VPN traffic never does.";

let cleanup = null;

function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function shortClock(seconds) {
  const total = Math.max(0, Math.floor(seconds));
  const m = Math.floor(total / 60);
  const s = total % 60;
  return m + ":" + String(s).padStart(2, "0");
}

function relayText(result) {
  if (!result) return "";
  if (result.ok) {
    if (typeof result.latency_ms === "number") return "Relay OK — " + Math.round(result.latency_ms) + " ms";
    return "Relay OK";
  }
  if (result.status === 401 || result.status === 403) return "Relay rejected the token";
  return "Relay unreachable: " + (result.error || "HTTP " + (result.status || "?"));
}

function build(host) {
  const root = el("div", "region");

  // ------------------------------------------------------------ the check
  const check = el("section", "card region__check");
  const checkTitle = el("h2", "card__title", "This network");
  const checkSummary = el("p", "region__summary", "Checking Mozilla's hosts…");
  const hosts = el("ul", "region__hosts");
  const checkActions = el("div", "region__actions");
  const recheck = el("button", "button button--tonal", "Recheck");
  recheck.type = "button";
  const signIn = el("button", "button button--filled", "Sign in");
  signIn.type = "button";
  checkActions.append(recheck, signIn);
  check.append(checkTitle, checkSummary, hosts, checkActions);

  // ---------------------------------------------------------- handoff card
  const handoff = el("section", "card region__handoff");
  const handoffTitle = el("h2", "card__title", "Prepared pass");
  const handoffBody = el("p", "region__summary");
  const handoffActions = el("div", "region__actions");
  const prepare = el("button", "button button--filled", "Prepare");
  prepare.type = "button";
  const connect = el("button", "button button--filled", "Connect");
  connect.type = "button";
  const dashboard = el("button", "button button--tonal", "Dashboard");
  dashboard.type = "button";
  handoffActions.append(prepare, connect, dashboard);
  handoff.append(handoffTitle, handoffBody, handoffActions);

  // ---------------------------------------------------------- option one
  const one = el("section", "card region__path");
  one.append(el("h2", "card__title", "Option 1: I have another VPN (recommended)"));
  const oneList = el("ol", "region__steps");
  for (const text of [
    "Turn on your other VPN.",
    "Sign in to Kumiho with the Firefox account that has the VPN.",
    "Choose Prepare. Kumiho fetches the server list, your profile, then the pass.",
    "When the pass is ready, turn the other VPN off.",
    "Choose Connect. Kumiho keeps renewing through its own tunnel.",
  ]) oneList.append(el("li", null, text));
  one.append(oneList, el("p", "field__hint",
    "You need the other VPN again only if Kumiho stays off for more than 10 minutes."));

  // ---------------------------------------------------------- option two
  const two = el("section", "card region__path");
  two.append(el("h2", "card__title", "Option 2: Use a relay"));
  const twoIntro = el("p", "region__summary",
    "Deploy docs/relay.md or use a relay you trust. The relay is only needed for blocked Mozilla requests.");
  const relayField = el("div", "field region__field");
  const relayLabel = el("label", "field__label", "Relay URL");
  relayLabel.htmlFor = "region-relay-url";
  const relayInput = document.createElement("input");
  relayInput.className = "input";
  relayInput.id = "region-relay-url";
  relayInput.type = "url";
  relayInput.autocomplete = "off";
  relayInput.spellcheck = false;
  relayInput.placeholder = "https://relay.example.test";
  const relayApply = el("button", "button button--tonal", "Apply URL");
  relayApply.type = "button";
  const relayRow = el("div", "region__inline");
  relayRow.append(relayInput, relayApply);
  relayField.append(relayLabel, relayRow);

  const tokenField = el("div", "field region__field");
  const tokenLabel = el("label", "field__label", "Relay token");
  tokenLabel.htmlFor = "region-relay-token";
  const tokenInput = document.createElement("input");
  tokenInput.className = "input";
  tokenInput.id = "region-relay-token";
  tokenInput.type = "password";
  tokenInput.autocomplete = "new-password";
  tokenInput.spellcheck = false;
  tokenInput.placeholder = "Paste token (write-only)";
  const tokenSave = el("button", "button button--tonal", "Save token");
  tokenSave.type = "button";
  const tokenRow = el("div", "region__inline");
  tokenRow.append(tokenInput, tokenSave);
  tokenField.append(tokenLabel, tokenRow);

  const testRow = el("div", "region__inline");
  const test = el("button", "button button--tonal", "Test relay");
  test.type = "button";
  const testResult = el("span", "region__relay-result");
  testResult.setAttribute("aria-live", "polite");
  testRow.append(test, testResult);

  two.append(twoIntro, relayField, tokenField, testRow, el("p", "field__hint", RELAY_HINT));
  two.append(el("p", "field__hint",
    "Browser sign-in still needs another VPN or another device; the relay does not proxy the browser page."));

  root.append(check, handoff, one, two);
  host.append(root);

  return {
    checkSummary: checkSummary, hosts: hosts, recheck: recheck, signIn: signIn,
    handoff: handoff, handoffBody: handoffBody, prepare: prepare, connect: connect, dashboard: dashboard,
    relayInput: relayInput, relayApply: relayApply, tokenInput: tokenInput, tokenSave: tokenSave,
    test: test, testResult: testResult,
  };
}

export default {
  mount(host, ctx) {
    const ui = build(host);
    const view = { urlTouched: false, tokenTouched: false, busy: false, second: 0 };
    let timer = 0;

    function snapshot() {
      const state = ctx.store.get();
      return state && typeof state === "object" ? state : {};
    }

    function send(action, payload) {
      return ctx.send(action, payload).then(function (err) {
        if (err) ui.checkSummary.textContent = "Could not reach the daemon: " + err;
        return err;
      });
    }

    function renderCheck(state) {
      const check = state.region_check;
      if (!check) {
        ui.checkSummary.textContent = "Checking Mozilla's hosts…";
        ui.hosts.replaceChildren();
        return;
      }
      const blocked = check.blocked || [];
      if (blocked.length === 0) {
        ui.checkSummary.textContent = "Mozilla's hosts are reachable from this network.";
      } else {
        ui.checkSummary.textContent = "This network blocks " + blocked.length +
          " of Mozilla's hosts (HTTP 451). The guide below works around it.";
      }
      const fragment = document.createDocumentFragment();
      for (const h of check.hosts || []) {
        const item = el("li", "region__host");
        item.append(el("span", "region__hostname", h.host));
        const chip = el("span", "chip", h.status);
        chip.dataset.tone = h.status === "blocked" ? "bad" : h.status === "unknown" ? "warn" : "good";
        item.append(chip);
        fragment.append(item);
      }
      ui.hosts.replaceChildren(fragment);
      if (check.browser_blocked) {
        ui.checkSummary.textContent += " Browser sign-in needs another VPN or another device.";
      }
    }

    function renderHandoff(state) {
      const status = state.status || {};
      const prepared = state.prepare;
      const expires = status.pass_expires;
      const now = Date.now() / 1000;
      const connected = status.connected || status.label === "Connected" || status.label === "Proxy only";
      const authed = !!status.authenticated;
      ui.signIn.hidden = authed;
      ui.prepare.disabled = view.busy || !authed;
      ui.prepare.textContent = view.busy ? "Preparing…" : (expires && expires > now ? "Prepare again" : "Prepare");
      ui.connect.hidden = !(expires && expires > now) || connected;
      if (expires && expires > now) {
        ui.handoff.hidden = false;
        ui.handoffBody.textContent = "Pass ready — valid for " + shortClock(expires - now) +
          ". Turn off the other VPN, then Connect.";
        ui.handoffBody.dataset.tone = expires - now < 60 ? "warn" : "good";
      } else if (prepared || (expires && expires <= now)) {
        ui.handoff.hidden = false;
        ui.handoffBody.textContent = "The prepared pass expired. Turn on your other VPN and Prepare again.";
        ui.handoffBody.dataset.tone = "warn";
      } else {
        ui.handoff.hidden = true;
        ui.handoffBody.textContent = "";
        delete ui.handoffBody.dataset.tone;
      }
    }

    function renderRelay(state) {
      const settings = state.settings;
      if (settings && !view.urlTouched) ui.relayInput.value = settings.relay_url || "";
      ui.tokenInput.placeholder = settings && settings.relay_token_set
        ? "Token saved (write-only)"
        : "Paste token (write-only)";
      const result = state.relay_test;
      if (result) {
        ui.testResult.textContent = relayText(result);
        ui.testResult.dataset.tone = result.ok ? "good" : "bad";
      }
    }

    function render() {
      const state = snapshot();
      renderCheck(state);
      renderHandoff(state);
      renderRelay(state);
    }

    ui.recheck.addEventListener("click", function () {
      ui.checkSummary.textContent = "Rechecking…";
      send("region_check", {});
    });
    ui.signIn.addEventListener("click", function () {
      ctx.navigate("onboarding", "login");
    });
    ui.prepare.addEventListener("click", function () {
      if (view.busy) return;
      view.busy = true;
      render();
      send("prepare", {}).then(function () {
        view.busy = false;
        render();
      });
    });
    ui.connect.addEventListener("click", function () {
      send("connect", {}).then(function (err) {
        if (!err) ctx.navigate("dashboard");
      });
    });
    ui.dashboard.addEventListener("click", function () {
      ctx.navigate("dashboard");
    });
    ui.relayApply.addEventListener("click", function () {
      send("settings_set", { key: "relay_url", value: ui.relayInput.value.trim() });
    });
    ui.tokenSave.addEventListener("click", function () {
      if (!ui.tokenInput.value) return;
      send("settings_set", { key: "relay_token", value: ui.tokenInput.value }).then(function (err) {
        if (err) return;
        ui.tokenInput.value = "";
        view.tokenTouched = false;
      });
    });
    ui.relayInput.addEventListener("input", function () { view.urlTouched = true; });
    ui.tokenInput.addEventListener("input", function () { view.tokenTouched = true; });
    ui.test.addEventListener("click", function () {
      ui.testResult.textContent = "Testing…";
      send("relay_test", {});
    });

    const unsubscribe = ctx.store.subscribe(render);
    timer = window.setInterval(function () {
      view.second++;
      renderHandoff(snapshot());
    }, 1000);

    const state = snapshot();
    if (!state.region_check) send("region_check", {});
    if (!state.settings) send("settings_get", {});
    render();

    cleanup = function () {
      unsubscribe();
      window.clearInterval(timer);
    };
  },

  unmount() {
    if (cleanup) cleanup();
    cleanup = null;
  },
};
