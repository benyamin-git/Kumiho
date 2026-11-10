import { t, tp, formatNumber } from "../i18n.js";

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
  return formatNumber(m) + ":" + formatNumber(s, { minimumIntegerDigits: 2 });
}

function relayText(result) {
  if (!result) return "";
  if (result.ok) {
    if (typeof result.latency_ms === "number") {
      return t("region.relay.latency", { latency: formatNumber(Math.round(result.latency_ms)) });
    }
    return t("region.relay.ok");
  }
  if (result.status === 401 || result.status === 403) return t("region.relay.rejected");
  return t("region.relay.unreachable");
}

function build(host) {
  const root = el("div", "region");

  // ------------------------------------------------------------ the check
  const check = el("section", "card region__check");
  const checkTitle = el("h2", "card__title", t("region.check.title"));
  const checkSummary = el("p", "region__summary", t("region.check.checking"));
  const hosts = el("ul", "region__hosts");
  const checkActions = el("div", "region__actions");
  const recheck = el("button", "button button--tonal", t("region.check.recheck"));
  recheck.type = "button";
  const signIn = el("button", "button button--filled", t("region.check.sign_in"));
  signIn.type = "button";
  checkActions.append(recheck, signIn);
  check.append(checkTitle, checkSummary, hosts, checkActions);

  // ---------------------------------------------------------- handoff card
  const handoff = el("section", "card region__handoff");
  const handoffTitle = el("h2", "card__title", t("region.handoff.title"));
  const handoffBody = el("p", "region__summary");
  const handoffActions = el("div", "region__actions");
  const prepare = el("button", "button button--filled", t("region.handoff.prepare"));
  prepare.type = "button";
  const connect = el("button", "button button--filled", t("region.handoff.connect"));
  connect.type = "button";
  const dashboard = el("button", "button button--tonal", t("region.handoff.dashboard"));
  dashboard.type = "button";
  handoffActions.append(prepare, connect, dashboard);
  handoff.append(handoffTitle, handoffBody, handoffActions);

  // ---------------------------------------------------------- option one
  const one = el("section", "card region__path");
  one.append(el("h2", "card__title", t("region.option1.title")));
  const oneList = el("ol", "region__steps");
  for (const text of [
    t("region.option1.step1"),
    t("region.option1.step2"),
    t("region.option1.step3"),
    t("region.option1.step4"),
    t("region.option1.step5"),
  ]) oneList.append(el("li", null, text));
  one.append(oneList, el("p", "field__hint", t("region.option1.hint")));

  // ---------------------------------------------------------- option two
  const two = el("section", "card region__path");
  two.append(el("h2", "card__title", t("region.option2.title")));
  const twoIntro = el("p", "region__summary",
    t("region.option2.intro"));
  const relayField = el("div", "field region__field");
  const relayLabel = el("label", "field__label", t("region.relay_url"));
  relayLabel.htmlFor = "region-relay-url";
  const relayInput = document.createElement("input");
  relayInput.className = "input";
  relayInput.id = "region-relay-url";
  relayInput.type = "url";
  relayInput.autocomplete = "off";
  relayInput.spellcheck = false;
  relayInput.placeholder = t("region.relay_url_placeholder");
  const relayApply = el("button", "button button--tonal", t("region.relay.apply"));
  relayApply.type = "button";
  const relayRow = el("div", "region__inline");
  relayRow.append(relayInput, relayApply);
  relayField.append(relayLabel, relayRow);

  const tokenField = el("div", "field region__field");
  const tokenLabel = el("label", "field__label", t("region.relay_token"));
  tokenLabel.htmlFor = "region-relay-token";
  const tokenInput = document.createElement("input");
  tokenInput.className = "input";
  tokenInput.id = "region-relay-token";
  tokenInput.type = "password";
  tokenInput.autocomplete = "new-password";
  tokenInput.spellcheck = false;
  tokenInput.placeholder = t("region.relay_token_placeholder");
  const tokenSave = el("button", "button button--tonal", t("region.relay.save_token"));
  tokenSave.type = "button";
  const tokenRow = el("div", "region__inline");
  tokenRow.append(tokenInput, tokenSave);
  tokenField.append(tokenLabel, tokenRow);

  const testRow = el("div", "region__inline");
  const test = el("button", "button button--tonal", t("region.relay.test"));
  test.type = "button";
  const testResult = el("span", "region__relay-result");
  testResult.setAttribute("aria-live", "polite");
  testRow.append(test, testResult);

  two.append(twoIntro, relayField, tokenField, testRow, el("p", "field__hint", t("region.relay.hint")));
  two.append(el("p", "field__hint", t("region.option2.browser_hint")));

  const status = el("p", "region__status");
  status.setAttribute("aria-live", "polite");
  status.hidden = true;
  const details = el("p", "region__details");
  details.dir = "ltr";
  details.hidden = true;
  const relayDetails = el("p", "region__details region__relay-details");
  relayDetails.dir = "ltr";
  relayDetails.hidden = true;
  testRow.append(relayDetails);

  root.append(check, handoff, one, two, status, details);
  host.append(root);

  return {
    checkSummary: checkSummary, hosts: hosts, recheck: recheck, signIn: signIn,
    handoff: handoff, handoffBody: handoffBody, prepare: prepare, connect: connect, dashboard: dashboard,
    relayInput: relayInput, relayApply: relayApply, tokenInput: tokenInput, tokenSave: tokenSave,
    test: test, testResult: testResult, relayDetails: relayDetails,
    status: status, details: details,
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
        if (err) {
          ui.status.textContent = t("region.error.daemon");
          ui.status.dataset.tone = "bad";
          ui.status.hidden = false;
          ui.details.textContent = err;
          ui.details.hidden = false;
        } else {
          ui.status.hidden = true;
          ui.details.hidden = true;
          delete ui.status.dataset.tone;
        }
        return err;
      });
    }

    function renderCheck(state) {
      const check = state.region_check;
      if (!check) {
        ui.checkSummary.textContent = t("region.check.checking");
        ui.hosts.replaceChildren();
        return;
      }
      const blocked = check.blocked || [];
      if (blocked.length === 0) {
        ui.checkSummary.textContent = t("region.check.reachable");
      } else {
        ui.checkSummary.textContent = tp("region.check.blocked", blocked.length, {
          blocked: formatNumber(blocked.length),
        });
      }
      const fragment = document.createDocumentFragment();
      for (const h of check.hosts || []) {
        const item = el("li", "region__host");
        item.append(el("span", "region__hostname", h.host));
        const chip = el("span", "chip", t(("region.host.") + (h.status || "unknown")));
        chip.dataset.tone = h.status === "blocked" ? "bad" : h.status === "unknown" ? "warn" : "good";
        item.append(chip);
        fragment.append(item);
      }
      ui.hosts.replaceChildren(fragment);
      if (check.browser_blocked) {
        ui.checkSummary.textContent += " " + t("region.check.browser");
      }
    }

    function renderHandoff(state) {
      const status = state.status || {};
      const prepared = state.prepare;
      const expires = status.pass_expires;
      const now = Date.now() / 1000;
      const connected = status.connected || status.proxy_only || status.state === "PROXY_ONLY";
      const authed = !!status.authenticated;
      ui.signIn.hidden = authed;
      ui.prepare.disabled = view.busy || !authed;
      ui.prepare.textContent = view.busy
        ? t("region.handoff.preparing")
        : (expires && expires > now ? t("region.handoff.prepare_again") : t("region.handoff.prepare"));
      ui.connect.hidden = !(expires && expires > now) || connected;
      if (expires && expires > now) {
        ui.handoff.hidden = false;
        ui.handoffBody.textContent = t("region.handoff.ready", { time: shortClock(expires - now) });
        ui.handoffBody.dataset.tone = expires - now < 60 ? "warn" : "good";
      } else if (prepared || (expires && expires <= now)) {
        ui.handoff.hidden = false;
        ui.handoffBody.textContent = t("region.handoff.expired");
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
        ? t("region.relay_token_saved")
        : t("region.relay_token_placeholder");
      const result = state.relay_test;
      if (result) {
        ui.testResult.textContent = relayText(result);
        ui.testResult.dataset.tone = result.ok ? "good" : "bad";
        const raw = !result.ok && (result.error || (result.status ? "HTTP " + result.status : ""));
        ui.relayDetails.textContent = raw || "";
        ui.relayDetails.hidden = !raw;
      } else {
        ui.testResult.textContent = "";
        delete ui.testResult.dataset.tone;
        ui.relayDetails.textContent = "";
        ui.relayDetails.hidden = true;
      }
    }

    function render() {
      const state = snapshot();
      renderCheck(state);
      renderHandoff(state);
      renderRelay(state);
    }

    ui.recheck.addEventListener("click", function () {
      ui.checkSummary.textContent = t("region.check.rechecking");
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
      ui.testResult.textContent = t("region.testing");
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
