/* The onboarding wizard (inventory §Onboarding wizard, D12/D16; T3.4): the
 * five-step first-run page — welcome + consent → sign in (email → password →
 * 2FA, or the already-signed-in variant) → health check → autoconnect →
 * all set.
 *
 * The wizard is not a rail destination: the router gates every route to this
 * screen while the state's `onboarded` flag is not true and hides the shell
 * chrome (`.app--wizard`, see the router and onboarding.css), so it occupies
 * the window. The step and the consent checkbox are never persisted; a
 * remount within the session resumes the step (module-level currentStep),
 * and a fresh launch starts at Welcome.
 *
 * Everything renders from the store's state envelopes; actions go through
 * the transport and the screen re-renders from the next envelope — "" means
 * accepted, never completed (T1.7). Login runs over the frozen actions
 * (login_email/login_password/login_2fa) and reads the controller's Login
 * projection: a terminal `done` reply advances to Health check, an error
 * renders on the status line, and a live session (status.authenticated)
 * always shows the signed-in page instead of the credential fields — the
 * daemon's "already signed in" refusal can never dead-end the wizard
 * (inventory §Onboarding wizard "Already-signed-in handling").
 *
 * The health step runs doctor_run once, when the page is first shown and no
 * report exists (the Diagnostics rule); Re-run checks repeats it. The
 * autoconnect step flips the daemon's setting through settings_set
 * (on|off). `Finish` opens the last page; `Open Kumiho` sends
 * prefs_set {"onboarded":true} and navigates to the Dashboard once the host
 * mirrors the flag back in State.Onboarded.
 *
 * Wording authority: every string, the consent text, the step titles and
 * button labels, the status-line priority (busy → local error → health
 * running → health summary → login error → view-model notice → empty), the
 * shared sign-out confirmation dialog, and the check-row format
 * (`Name: <status word>: <detail> — <remediation>`) come from inventory
 * §Onboarding wizard. The health summary reuses the Diagnostics wording. The
 * capture hints (`#/onboarding?state=welcome|login|health-check|autoconnect|
 * finish`) select the starting step, scripts/visual/README.md.
 *
 * Two documented conflicts resolve toward the specific per-step text: Back
 * exists on the last page (the step-5 bullet says it returns to the
 * autoconnect page; the anatomy line's "first and last pages" would
 * contradict it), and the health running/summary status lines render on the
 * Health check step (a global summary would mask every later login error).
 *
 * The router links css/screens/onboarding.css from the registry entry.
 */

// The five steps, in order.
const STEP_WELCOME = 0;
const STEP_LOGIN = 1;
const STEP_HEALTH = 2;
const STEP_AUTOCONNECT = 3;
const STEP_FINISH = 4;
const STEP_COUNT = 5;

// The step titles (the page heading) and the primary button's label per step.
const STEP_TITLES = [
  "Welcome to Kumiho",
  "Sign in",
  "Health check",
  "Almost done",
  "You're all set",
];
const PRIMARY_LABELS = ["Continue", "Continue", "Continue", "Finish", "Open Kumiho"];

// Capture hints -> starting step (scripts/visual/README.md).
const HINT_STEPS = {
  welcome: STEP_WELCOME,
  login: STEP_LOGIN,
  "health-check": STEP_HEALTH,
  autoconnect: STEP_AUTOCONNECT,
  finish: STEP_FINISH,
};

// The busy guards' settle (the T2.2/T3.x house pattern): a round trip whose
// completion evidence repeats byte-for-byte stops blocking after this long.
// The controller exposes no login/onboarding busy flag (projection gap), so
// these guards are the closest the data allows.
const SETTLE_MS = 3000;

// Browser sign-in polls the daemon once a second (the protocol's status
// cadence).
const POLL_MS = 1000;

// The consent copy: the D16 gate, transcribed from the inventory.
const CONSENT_INTRO =
  "Kumiho is an unofficial desktop client for the VPN proxy included with a " +
  "Firefox account. Please read this before you continue.";
const CONSENT_GROUPS = [
  {
    heading: "What Kumiho does",
    items: [
      "It signs in with your Firefox account and stores the resulting sign-in tokens on this PC. It never stores your password.",
      "It carries your traffic through Mozilla's servers using an HTTP/2 tunnel.",
      "The tunnel carries TCP and DNS only. Non-DNS UDP traffic (some games, video calls, and QUIC/HTTP3) is blocked on purpose, so apps either fall back to TCP or stop working.",
    ],
  },
  {
    heading: "What to keep in mind",
    items: [
      "Kumiho is not affiliated with or endorsed by Mozilla. Using an unofficial client is a gray area under Mozilla's terms of service.",
      "While the tunnel is up, the kill switch can block traffic that tries to leave outside it.",
      "Kumiho is provided as is; review the code before trusting it with your account.",
    ],
  },
];
const CONSENT_TICK = 'Tick "I understand" below to unlock Continue.';

// The login pages' bodies (inventory §Sign in, extended by v0.6.0).
const LOGIN_CHOICE_BODY =
  "Sign in with the Firefox account that has the VPN. Continue in your browser (recommended), or use your account password.";
const LOGIN_EMAIL_BODY =
  "Sign in with the Firefox account that has the VPN. Kumiho stores the sign-in tokens on this PC, never your password.";
const LOGIN_PASSWORD_BODY = "Enter your Firefox account password.";
const LOGIN_CODE_BODY = "Enter the verification code from the email Mozilla sent you.";
const LOGIN_LINK_BODY =
  "Check your email and open the Mozilla sign-in link, then choose Continue with the code empty to finish signing in.";
const LOGIN_SIGNED_BODY =
  "Kumiho already has a signed-in Firefox account. Choose Continue to finish setting up, or sign out to use a different account.";
const LOGIN_WAITING_BODY =
  "Finish signing in in your browser. This page continues automatically when Mozilla sends you back.";

// The remaining steps' bodies and the shared sign-out dialog copy.
const HEALTH_BODY =
  "Kumiho checked this PC. Fix anything marked Failed before connecting; warnings are usually fine.";
const AUTOCONNECT_BODY =
  "Kumiho can connect automatically whenever it starts. You can change this later in Settings.";
const FINISH_BODY = "Kumiho is ready. Press Connect on the Dashboard to start the tunnel.";
const SIGNOUT_QUESTION = "Sign out of Kumiho?";
const SIGNOUT_DETAIL = "The stored sign-in tokens are cleared and the tunnel disconnects.";

// Module-level session state: a router remount keeps the step and the
// consent tick (never persisted; a fresh launch reloads the module).
let currentStep = STEP_WELCOME;
let consentTicked = false;

// The browser URL opened once across remounts. A loopback URL is one-shot,
// so re-opening the same flow after a screen switch would be a navigation
// error; a new flow carries a new port/state.
let openedBrowserURL = "";

// unmount()'s teardown, assigned by mount (the router mounts one screen at a
// time, so one slot is enough).
let cleanup = null;

function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

/** The inventory's run summary, the Diagnostics wording: failed (bad),
 * warning (warn), or `All N check(s) passed` (good). */
function summaryLine(checks) {
  let failed = 0;
  let warned = 0;
  let skipped = 0;
  for (const check of checks) {
    if (check.status === "fail") failed += 1;
    else if (check.status === "warn") warned += 1;
    else if (check.status === "skip") skipped += 1;
  }
  const ok = checks.length - failed - warned - skipped;
  const checksWord = checks.length === 1 ? "1 check" : checks.length + " checks";
  const warningsWord = warned === 1 ? "1 warning" : warned + " warnings";
  if (failed > 0) {
    return {
      text:
        checksWord + ": " + failed + " failed, " + warningsWord +
        " — fix the failed checks before connecting",
      tone: "bad",
    };
  }
  if (warned > 0) {
    return {
      text: checksWord + ": " + ok + " OK, " + warningsWord + ", " + skipped + " skipped",
      tone: "warn",
    };
  }
  return { text: "All " + checksWord + " passed", tone: "good" };
}

/** The 2FA body: TOTP, the unblock code, or the emailed link/code flow. */
function twoFactorBody(login) {
  const flow = String((login && login.method) || "").toLowerCase();
  const method = String((login && login.verification_method) || "").toLowerCase();
  const reason = String((login && login.verification_reason) || "").toLowerCase();
  if (flow === "totp") return "Enter the code from your authenticator app.";
  if (flow === "unblock" || reason === "unblock-code") {
    return "Enter the unblock code Mozilla emailed you.";
  }
  if (method === "email" || method.indexOf("link") >= 0) return LOGIN_LINK_BODY;
  return LOGIN_CODE_BODY;
}

/** A labeled input group (label always visible, never placeholder-only). */
function fieldBox(id, labelText, type, cue) {
  const wrap = el("div", "field onb__field");
  const label = el("label", "field__label", labelText);
  label.htmlFor = id;
  const input = document.createElement("input");
  input.className = "input";
  input.id = id;
  input.type = type;
  input.autocomplete = "off";
  input.spellcheck = false;
  if (cue) input.placeholder = cue;
  wrap.append(label, input);
  return { wrap: wrap, input: input };
}

/** Build the screen's DOM and return its element references. */
function build(host) {
  const root = el("div", "onb");
  const panel = el("div", "card onb__panel");

  // Heading + the visual step progress (the heading carries the step's
  // identity; the progressbar is the accompanying position, exposed to AT).
  const head = el("div", "onb__head");
  const title = el("h2", "onb__title", STEP_TITLES[STEP_WELCOME]);
  const progress = el("div", "onb__progress");
  progress.setAttribute("role", "progressbar");
  progress.setAttribute("aria-valuemin", "1");
  progress.setAttribute("aria-valuemax", String(STEP_COUNT));
  const segments = [];
  for (let i = 0; i < STEP_COUNT; i++) {
    const segment = el("span", "onb__progress-segment");
    segment.setAttribute("aria-hidden", "true");
    segments.push(segment);
    progress.append(segment);
  }
  head.append(title, progress);
  panel.append(head);

  // Step 1: the consent gate (scrolling read-only text behind a thin frame).
  const welcome = el("section", "onb__step");
  welcome.dataset.step = "welcome";
  const consent = el("div", "onb__consent");
  consent.tabIndex = 0;
  consent.setAttribute("role", "region");
  consent.setAttribute("aria-label", "Consent");
  consent.append(el("p", "onb__consent-text", CONSENT_INTRO));
  for (const group of CONSENT_GROUPS) {
    const heading = el("h3", "onb__consent-heading", group.heading);
    const list = el("ul", "onb__consent-list");
    for (const item of group.items) list.append(el("li", null, item));
    consent.append(heading, list);
  }
  consent.append(el("p", "onb__consent-text", CONSENT_TICK));
  const consentToggle = el("button", "onb__consent-toggle");
  consentToggle.type = "button";
  consentToggle.id = "onb-consent";
  consentToggle.setAttribute("role", "checkbox");
  consentToggle.setAttribute("aria-checked", "false");
  const consentBox = el("span", "onb__consent-box");
  consentBox.setAttribute("aria-hidden", "true");
  consentToggle.append(consentBox, el("span", "onb__consent-label", "I understand"));
  welcome.append(consent, consentToggle);

  // Step 2: sign in (body + signed-in card + the three credential fields).
  const login = el("section", "onb__step");
  login.dataset.step = "login";
  login.hidden = true;
  const loginBody = el("p", "onb__body", LOGIN_CHOICE_BODY);
  // The first page offers both methods. The primary button is the default
  // (the last successful method, browser on first run); this secondary
  // choice is the other one.
  const methods = el("div", "onb__methods");
  const altMethod = el("button", "button button--tonal onb__method", "Use password");
  altMethod.type = "button";
  altMethod.id = "onb-method-alt";
  methods.append(altMethod);
  methods.hidden = true;
  const signed = el("div", "onb__signed");
  const signedIdentity = el("p", "onb__signed-identity");
  const signout = el("button", "button button--tonal onb__signout", "Sign out");
  signout.type = "button";
  signed.append(signedIdentity, signout);
  signed.hidden = true;
  const email = fieldBox("onb-email", "Email", "email", "you@example.com");
  const password = fieldBox("onb-password", "Password", "password", "");
  const code = fieldBox("onb-code", "Verification code", "text", "");
  password.wrap.hidden = true;
  code.wrap.hidden = true;
  // The browser waiting card: the loopback URL, Open again and Cancel.
  const browser = el("div", "onb__browser");
  browser.hidden = true;
  const browserBody = el("p", "onb__body", LOGIN_WAITING_BODY);
  const browserURL = el("p", "onb__browser-url");
  const browserActions = el("div", "onb__browser-actions");
  const browserOpen = el("button", "button button--tonal onb__browser-open", "Open again");
  browserOpen.type = "button";
  browserOpen.id = "onb-browser-open";
  const browserCancel = el("button", "button button--text onb__browser-cancel", "Cancel");
  browserCancel.type = "button";
  browserCancel.id = "onb-browser-cancel";
  browserActions.append(browserOpen, browserCancel);
  browser.append(browserBody, browserURL, browserActions);
  login.append(loginBody, methods, signed, email.wrap, password.wrap, code.wrap, browser);

  // Step 3: health check (the run summary lives on the status line; the
  // rows render compactly, one line per check).
  const health = el("section", "onb__step");
  health.dataset.step = "health";
  health.hidden = true;
  const healthBody = el("p", "onb__body", HEALTH_BODY);
  const healthActions = el("div", "onb__health-actions");
  const rerun = el("button", "button button--tonal onb__rerun", "Re-run checks");
  rerun.type = "button";
  rerun.id = "onb-rerun";
  healthActions.append(rerun);
  const checks = el("ul", "onb__checks");
  checks.id = "onb-checks";
  checks.tabIndex = 0;
  checks.setAttribute("aria-label", "Health check results");
  const checksEmpty = el("p", "onb__checks-empty", "Checking…");
  checksEmpty.id = "onb-checks-empty";
  health.append(healthBody, healthActions, checks, checksEmpty);

  // Step 4: autoconnect (the daemon's current effective value, flipped
  // immediately through settings_set).
  const autoconnect = el("section", "onb__step");
  autoconnect.dataset.step = "autoconnect";
  autoconnect.hidden = true;
  const autoBody = el("p", "onb__body", AUTOCONNECT_BODY);
  const autoRow = el("div", "onb__row");
  autoRow.append(el("span", "onb__row-label", "Autoconnect"));
  const autoToggle = el("button", "button button--tonal onb__toggle", "off");
  autoToggle.type = "button";
  autoToggle.id = "onb-autoconnect";
  autoToggle.setAttribute("aria-label", "Autoconnect");
  autoRow.append(autoToggle);
  autoconnect.append(autoBody, autoRow);

  // Step 5: all set.
  const finish = el("section", "onb__step");
  finish.dataset.step = "finish";
  finish.hidden = true;
  const finishBody = el("p", "onb__body", FINISH_BODY);
  finish.append(finishBody);

  panel.append(welcome, login, health, autoconnect, finish);

  // Status line (the wizard-global priority; owned by the screen).
  const status = el("p", "onb__status");
  status.setAttribute("aria-live", "polite");
  status.hidden = true;

  // Navigation: Back (tonal, hidden on the first and last pages) and the
  // per-step primary action.
  const nav = el("div", "onb__nav");
  const back = el("button", "button button--tonal onb__back", "Back");
  back.type = "button";
  back.id = "onb-back";
  const primary = el("button", "button button--filled onb__primary", PRIMARY_LABELS[STEP_WELCOME]);
  primary.type = "button";
  primary.id = "onb-primary";
  nav.append(back, primary);

  // The shared sign-out confirmation (the house dialog pattern, native;
  // Cancel focused by default, Esc and Enter cancel).
  const dialog = el("dialog", "dialog onb__dialog");
  const dialogTitle = el("h2", "dialog__title", "Sign out");
  dialogTitle.id = "onb-signout-title";
  const dialogQuestion = el("p", "dialog__body", SIGNOUT_QUESTION);
  dialogQuestion.id = "onb-signout-question";
  const dialogDetail = el("p", "dialog__body", SIGNOUT_DETAIL);
  dialogDetail.id = "onb-signout-detail";
  dialog.setAttribute("aria-labelledby", dialogTitle.id);
  dialog.setAttribute("aria-describedby", dialogQuestion.id + " " + dialogDetail.id);
  const cancel = el("button", "button button--tonal onb__dialog-cancel", "Cancel");
  cancel.type = "button";
  const confirm = el("button", "button button--danger onb__dialog-confirm", "Sign out");
  confirm.type = "button";
  const dialogActions = el("div", "dialog__actions");
  dialogActions.append(cancel, confirm);
  dialog.append(dialogTitle, dialogQuestion, dialogDetail, dialogActions);

  root.append(panel, status, nav, dialog);
  host.append(root);

  return {
    root: root,
    title: title,
    progress: progress,
    segments: segments,
    sections: [welcome, login, health, autoconnect, finish],
    consentToggle: consentToggle,
    loginBody: loginBody,
    methods: methods,
    altMethod: altMethod,
    signed: signed,
    signedIdentity: signedIdentity,
    signout: signout,
    email: email,
    password: password,
    code: code,
    browser: browser,
    browserURL: browserURL,
    browserOpen: browserOpen,
    browserCancel: browserCancel,
    rerun: rerun,
    checks: checks,
    checksEmpty: checksEmpty,
    autoToggle: autoToggle,
    back: back,
    primary: primary,
    status: status,
    dialog: dialog,
    cancel: cancel,
    confirm: confirm,
  };
}

export default {
  mount(host, ctx) {
    const ui = build(host);

    // Per-mount view state. The store hands parsed snapshots (a fresh object
    // graph per envelope), so identity checks use signatures, never
    // references.
    const view = {
      shownStep: -1,
      page: null,
      pagePinned: false,
      pagePinMark: "",
      renderedPage: null,
      submitted: false,
      // The first page's default method (last successful method, browser on
      // first run) and the browser flow's timer state.
      defaultMethod: "",
      pollTimer: 0,
      pollID: "",
      pollInFlight: false,
      openedURL: "",
      cancelling: false,
      busy: null, // "signin" while a login round trip runs
      busyMark: "",
      busyTimer: 0,
      logoutBusy: false,
      logoutMark: "",
      logoutTimer: 0,
      autoBusy: false,
      autoPick: "",
      autoMark: "",
      autoTimer: 0,
      finishBusy: false,
      finishTimer: 0,
      finishError: "",
      checksStarted: false,
      listKey: null,
      statusText: null,
      statusTone: null,
    };

    let disposed = false;

    // The hint selects the entry step (captures and deep links).
    const hintStep = HINT_STEPS[ctx.hint];
    if (typeof hintStep === "number") currentStep = hintStep;

    // ------------------------------------------------------------ helpers

    /** Whether a live session exists (the already-signed-in presentation). */
    function signedIn(snapshot) {
      return !!(snapshot.status && snapshot.status.authenticated === true);
    }

    /** The signed-in identity line: `Signed in as <email>` or `Signed in`. */
    function identityLine(snapshot) {
      const account = snapshot.account || null;
      const status = snapshot.status || null;
      const email = (account && account.email) || (status && status.email) || "";
      return email ? "Signed in as " + email : "Signed in";
    }

    /** The login projection's identity for the busy guard. */
    function loginMark(snapshot) {
      const login = snapshot.login || null;
      if (!login) return "";
      return [
        login.step || "",
        login.email || "",
        login.method || "",
        login.verification_method || "",
        login.verification_reason || "",
        login.url || "",
        login.id || "",
        login.error || "",
      ].join("\u0000");
    }

    /** The notice identity used by the sign-out guard. */
    function noticeMark(notice) {
      return notice && notice.message ? notice.message + "\u0000" + (notice.tone || "") : "";
    }

    /** The autoconnect guard's completion evidence: the settings notice or
     * the reloaded settings snapshot moving. */
    function settingsMark(snapshot) {
      return noticeMark(snapshot.settings_notice) + "\u0000" +
        (snapshot.settings ? JSON.stringify(snapshot.settings) : "");
    }

    /**
     * The login page the session/step state calls for: password/2FA while
     * the daemon asks for them, the browser waiting card during a loopback
     * flow, the signed-in variant while a live session exists (any login
     * page — the refusal never dead-ends), the choice page otherwise. An
     * email-step error is the password form's failed start, so it stays on
     * the email field.
     */
    function derivePage(snapshot) {
      const login = snapshot.login || null;
      const step = (login && login.step) || "";
      if (step === "password") return "password";
      if (step === "2fa") return "2fa";
      if (signedIn(snapshot)) return "signedin";
      if (step === "browser") return "browser";
      if (step === "email" && login.error) return "email";
      return "choice";
    }

    // ------------------------------------------------------------- guards

    function endBusy() {
      view.busy = null;
      clearTimeout(view.busyTimer);
      view.busyTimer = 0;
      // Follow the reply (or, after a settle with no reply, the settled
      // state): the password/2FA pages and the signed-in variant.
      view.page = derivePage(ctx.store.get());
    }

    function beginBusy() {
      view.busy = "signin";
      view.busyMark = loginMark(ctx.store.get());
      clearTimeout(view.busyTimer);
      view.busyTimer = setTimeout(function () {
        view.busyTimer = 0;
        if (view.busy) {
          endBusy();
          render();
        }
      }, SETTLE_MS);
      render();
    }

    function endLogout() {
      view.logoutBusy = false;
      clearTimeout(view.logoutTimer);
      view.logoutTimer = 0;
    }

    function beginLogout() {
      view.logoutBusy = true;
      view.logoutMark = noticeMark(ctx.store.get().account_notice) + "\u0000" + signedIn(ctx.store.get());
      clearTimeout(view.logoutTimer);
      view.logoutTimer = setTimeout(function () {
        view.logoutTimer = 0;
        if (view.logoutBusy) {
          endLogout();
          render();
        }
      }, SETTLE_MS);
      render();
    }

    function endAuto() {
      view.autoBusy = false;
      view.autoPick = "";
      clearTimeout(view.autoTimer);
      view.autoTimer = 0;
    }

    function beginAuto(target) {
      view.autoBusy = true;
      view.autoPick = target;
      view.autoMark = settingsMark(ctx.store.get());
      clearTimeout(view.autoTimer);
      view.autoTimer = setTimeout(function () {
        view.autoTimer = 0;
        if (view.autoBusy) {
          endAuto();
          render();
        }
      }, SETTLE_MS);
      render();
    }

    /** Release every guard whose completion evidence moved (called first on
     * every envelope, before rendering). */
    function settleGuards(snapshot) {
      if (view.busy && loginMark(snapshot) !== view.busyMark) {
        endBusy();
      }
      if (view.logoutBusy && noticeMark(snapshot.account_notice) + "\u0000" + signedIn(snapshot) !== view.logoutMark) {
        endLogout();
        if (!signedIn(snapshot)) {
          // The session is cleared: the page flips back to the empty email
          // field for a different account (windows-testing 17.2).
          ui.email.input.value = "";
          view.submitted = false;
          view.page = "email";
        }
      }
      if (view.autoBusy && settingsMark(snapshot) !== view.autoMark) {
        endAuto();
      }
      const login = snapshot.login || null;
      if (view.cancelling && (!login || login.step !== "browser")) {
        view.cancelling = false;
      }
      if (login && (login.step === "error" || login.step === "cancelled")) {
        view.submitted = false;
      }
    }

    // --------------------------------------------------------------- flow

    /** Step transitions that do not depend on an action's reply: the login
     * completion advancing to Health, and the wizard finishing on the
     * onboarded flip. */
    function autoAdvance(snapshot) {
      // Never advance on a stale `done` from the previous flow: the start
      // request is in flight while busy, and its reply replaces the step.
      if (currentStep === STEP_LOGIN && view.submitted && !view.busy) {
        const login = snapshot.login || null;
        if (login && login.step === "done" && !login.error) {
          view.submitted = false;
          currentStep = STEP_HEALTH;
        }
      }
      if (currentStep === STEP_FINISH && view.finishBusy && snapshot.onboarded === true) {
        // The host mirrored prefs_set back: the wizard is done. The
        // Dashboard navigation; when the hash already asks for it, the
        // router's own gate release shows it.
        view.finishBusy = false;
        clearTimeout(view.finishTimer);
        view.finishTimer = 0;
        ctx.navigate("dashboard");
      }
    }

    function goTo(step) {
      currentStep = step;
      render();
    }

    /** The first focusable target of the current step (first field or the
     * primary action; the house focus management rule). */
    function focusStep() {
      const page = view.page || derivePage(ctx.store.get());
      let target = null;
      switch (currentStep) {
        case STEP_WELCOME:
          target = ui.consentToggle;
          break;
        case STEP_LOGIN:
          if (page === "password") target = ui.password.input;
          else if (page === "2fa") target = ui.code.input;
          else if (page === "email") target = ui.email.input;
          else if (page === "browser") target = ui.browserCancel;
          else target = ui.primary;
          break;
        case STEP_AUTOCONNECT:
          target = ui.autoToggle;
          break;
        default:
          target = ui.primary;
          break;
      }
      if (target && !target.disabled && !target.hidden) target.focus();
    }

    // --------------------------------------------------------------- login

    function openURL(url) {
      if (!url) return;
      ctx.send("open_external", { url: url }).then(function (err) {
        if (err) console.warn("kumiho: open browser sign-in refused: " + err);
      });
    }

    function stopPolling() {
      if (view.pollTimer) clearInterval(view.pollTimer);
      view.pollTimer = 0;
      view.pollID = "";
      view.pollInFlight = false;
    }

    function startPolling(loginID) {
      if (!loginID) {
        stopPolling();
        return;
      }
      if (view.pollTimer && view.pollID === loginID) return;
      stopPolling();
      view.pollID = loginID;
      view.pollTimer = setInterval(pollBrowser, POLL_MS);
    }

    function pollBrowser() {
      if (disposed || view.pollInFlight) return;
      const login = ctx.store.get().login || null;
      if (!login || login.step !== "browser" || !login.id) {
        stopPolling();
        return;
      }
      view.pollInFlight = true;
      ctx.send("login_status", { login_id: login.id }).then(function (err) {
        view.pollInFlight = false;
        if (err) {
          stopPolling();
          console.warn("kumiho: browser sign-in status refused: " + err);
        }
      });
    }

    /** The first page's default method: the last successful method, browser
     * when the daemon has no history. */
    function defaultMethod(snapshot) {
      if (!view.defaultMethod && snapshot.accounts) {
        view.defaultMethod = snapshot.accounts.last_method === "password" ? "password" : "browser";
      }
      return view.defaultMethod || "browser";
    }

    function startBrowserLogin() {
      if (view.busy) return;
      view.submitted = true;
      view.pagePinned = false;
      view.cancelling = false;
      beginBusy();
      ctx.send("login_start", { method: "browser" }).then(function (err) {
        if (!err) return;
        endBusy();
        render();
        console.warn("kumiho: login_start refused: " + err);
      });
    }

    function choosePassword() {
      view.submitted = false;
      view.cancelling = false;
      pinPage("email");
      render();
      focusStep();
    }

    function cancelBrowserLogin() {
      const login = ctx.store.get().login || null;
      if (!login || !login.id || view.cancelling) return;
      view.cancelling = true;
      view.submitted = false;
      render();
      ctx.send("login_cancel", { login_id: login.id }).then(function (err) {
        if (!err) return;
        view.cancelling = false;
        render();
        console.warn("kumiho: login_cancel refused: " + err);
      });
    }

    function submitLogin() {
      const page = view.page;
      let payload = null;
      let action = "";
      if (page === "email") {
        action = "login_email";
        payload = { email: ui.email.input.value.trim() };
      } else if (page === "password") {
        action = "login_password";
        payload = { password: ui.password.input.value };
      } else if (page === "2fa") {
        action = "login_2fa";
        payload = { code: ui.code.input.value };
      }
      if (!payload) return;
      view.submitted = true;
      view.pagePinned = false;
      beginBusy();
      ctx.send(action, payload).then(function (err) {
        if (err) {
          endBusy();
          render();
          console.warn("kumiho: " + action + " refused: " + err);
        }
      });
    }

    // -------------------------------------------------------------- health

    /** The inventory's first-shown rule: run the suite once, and only when
     * no report exists yet (the Diagnostics rule). */
    function maybeRunChecks(snapshot) {
      if (currentStep !== STEP_HEALTH || view.checksStarted) return;
      if (!ctx.store.hasState()) return;
      view.checksStarted = true;
      if (snapshot.doctor) return;
      ctx.send("doctor_run", {}).then(function (err) {
        if (err) console.warn("kumiho: doctor run refused: " + err);
      });
    }

    /** One check row: `Name: <status word>: <detail> — <remediation>`
     * (remediation only for Warning/Failed). */
    function checkRow(check) {
      const item = el("li", "onb__check");
      item.append(document.createTextNode((check.title || check.name || "") + ": "));
      const word = el("span", "onb__check-word", check.status_word || "");
      if (check.tone) word.dataset.tone = check.tone;
      item.append(word);
      let rest = ": " + (check.detail || "");
      if (check.remediation && (check.status === "warn" || check.status === "fail")) {
        rest += " — " + check.remediation;
      }
      item.append(document.createTextNode(rest));
      return item;
    }

    function rebuildChecks(doctor) {
      const running = !!(doctor && doctor.running);
      const list = (doctor && doctor.checks) || [];
      if (running || list.length === 0) {
        ui.checks.hidden = true;
        ui.checksEmpty.hidden = false;
        ui.checksEmpty.textContent = running ? "Checking…" : "No checks yet.";
        return;
      }
      const fragment = document.createDocumentFragment();
      for (const check of list) fragment.append(checkRow(check));
      ui.checks.replaceChildren(fragment);
      ui.checksEmpty.hidden = true;
      ui.checks.hidden = false;
      ui.checks.scrollTop = 0;
    }

    // ------------------------------------------------------------- actions

    function onPrimary() {
      if (ui.primary.disabled) return;
      switch (currentStep) {
        case STEP_WELCOME:
          // The consent gate is enforced here too, not only on the button.
          if (!consentTicked) return;
          goTo(STEP_LOGIN);
          break;
        case STEP_LOGIN: {
          const page = view.page || derivePage(ctx.store.get());
          if (page === "signedin") {
            goTo(STEP_HEALTH);
            return;
          }
          if (page === "browser") return;
          if (page === "choice") {
            if (defaultMethod(ctx.store.get()) === "password") choosePassword();
            else startBrowserLogin();
            return;
          }
          submitLogin();
          break;
        }
        case STEP_HEALTH:
          goTo(STEP_AUTOCONNECT);
          break;
        case STEP_AUTOCONNECT:
          goTo(STEP_FINISH);
          break;
        case STEP_FINISH:
          onOpenKumiho();
          break;
      }
    }

    /** The choice page's alternate method button (the primary is the
     * last-used method). */
    function onSecondary() {
      if (currentStep !== STEP_LOGIN) return;
      const page = view.page || derivePage(ctx.store.get());
      if (page !== "choice") return;
      if (defaultMethod(ctx.store.get()) === "password") startBrowserLogin();
      else choosePassword();
    }

    /** Pin the login page against the projection (Back's local override). */
    function pinPage(page) {
      view.page = page;
      view.pagePinned = true;
      view.pagePinMark = loginMark(ctx.store.get());
    }

    function onBack() {
      if (view.busy || view.logoutBusy) return;
      switch (currentStep) {
        case STEP_LOGIN: {
          const page = view.page || derivePage(ctx.store.get());
          if (page === "browser") {
            cancelBrowserLogin();
            return;
          }
          if (page === "password") {
            pinPage("email");
          } else if (page === "2fa") {
            pinPage("password");
          } else if (page === "email") {
            pinPage("choice");
          } else {
            goTo(STEP_WELCOME);
            return;
          }
          render();
          focusStep();
          break;
        }
        case STEP_HEALTH:
          // Back always returns to the sign-in page (inventory); the render
          // resolves it against the live session (the signed-in variant) and
          // the daemon's login step.
          goTo(STEP_LOGIN);
          break;
        case STEP_AUTOCONNECT:
          goTo(STEP_HEALTH);
          break;
        case STEP_FINISH:
          goTo(STEP_AUTOCONNECT);
          break;
      }
    }

    /** `Open Kumiho`: persist the flag through the host, then land on the
     * Dashboard once the state mirrors it (or immediately if the host
     * already did). A refusal shows on the status line. */
    function onOpenKumiho() {
      if (view.finishBusy) return;
      view.finishBusy = true;
      view.finishError = "";
      clearTimeout(view.finishTimer);
      view.finishTimer = setTimeout(function () {
        view.finishTimer = 0;
        if (view.finishBusy) {
          view.finishBusy = false;
          render();
        }
      }, SETTLE_MS);
      render();
      ctx.send("prefs_set", { onboarded: true }).then(function (err) {
        if (!err) return;
        clearTimeout(view.finishTimer);
        view.finishTimer = 0;
        view.finishBusy = false;
        view.finishError = err;
        render();
      });
    }

    function onAutoToggle() {
      if (view.autoBusy) return;
      const settings = ctx.store.get().settings;
      if (!settings) return;
      const target = settings.autoconnect_effective ? "off" : "on";
      beginAuto(target);
      ctx.send("settings_set", { key: "autoconnect", value: target }).then(function (err) {
        if (!err) return;
        endAuto();
        render();
        console.warn("kumiho: autoconnect change refused: " + err);
      });
    }

    function onConsentToggle() {
      consentTicked = !consentTicked;
      render();
    }

    function onRerun() {
      if (ui.rerun.disabled) return;
      ctx.send("doctor_run", {}).then(function (err) {
        if (err) console.warn("kumiho: doctor run refused: " + err);
      });
    }

    function onSignout() {
      if (ui.dialog.open) return;
      ui.dialog.showModal();
      // The inventory's default: Cancel takes focus when the popup opens.
      ui.cancel.focus();
    }

    function returnFocus() {
      if (!ui.signout.hidden && !ui.signout.disabled) ui.signout.focus();
      else if (!ui.primary.disabled) ui.primary.focus();
    }

    function closeDialog(refocus) {
      if (ui.dialog.open) ui.dialog.close();
      if (refocus) returnFocus();
    }

    function onSignoutConfirm() {
      closeDialog(false);
      beginLogout();
      ctx.send("logout", {}).then(function (err) {
        if (!err) return;
        endLogout();
        render();
        console.warn("kumiho: logout refused: " + err);
      });
    }

    // ------------------------------------------------------------- render

    function renderStep() {
      if (view.shownStep === currentStep) return;
      view.shownStep = currentStep;
      for (let i = 0; i < ui.sections.length; i++) ui.sections[i].hidden = i !== currentStep;
      ui.title.textContent = STEP_TITLES[currentStep];
      ui.progress.setAttribute("aria-valuenow", String(currentStep + 1));
      ui.progress.setAttribute("aria-valuetext", "Step " + (currentStep + 1) + " of " + STEP_COUNT);
      for (let i = 0; i < ui.segments.length; i++) {
        if (i <= currentStep) ui.segments[i].dataset.state = "done";
        else delete ui.segments[i].dataset.state;
      }
      // Back is hidden on the first page only: the step-5 bullet documents
      // `Back` returning to the autoconnect page, which the anatomy line's
      // "first and last pages" would contradict (inventory doc conflict,
      // resolved toward the specific per-step behavior).
      ui.back.hidden = currentStep === STEP_WELCOME;
      focusStep();
    }

    function renderConsent() {
      const checked = consentTicked ? "true" : "false";
      if (ui.consentToggle.getAttribute("aria-checked") !== checked) {
        ui.consentToggle.setAttribute("aria-checked", checked);
      }
      if (checked === "true") ui.consentToggle.dataset.checked = "true";
      else delete ui.consentToggle.dataset.checked;
    }

    function renderLogin(snapshot) {
      // The daemon's requested step is authoritative: the page follows the
      // login projection on every envelope (an externally started login, a
      // CLI sign-in or sign-out, a scenario switch), so the wizard can never
      // disagree with what the daemon asks for. Back pins the page the user
      // deliberately left until the step moves or a submit restarts the
      // flow.
      if (!(view.pagePinned && loginMark(snapshot) === view.pagePinMark)) {
        view.pagePinned = false;
        if (!view.busy) view.page = derivePage(snapshot);
      }
      const page = view.page;
      const login = snapshot.login || null;
      const def = defaultMethod(snapshot);

      // Carried-over text: the email is restored when the page is shown
      // again (only from a failed email reply, so a signed-out session's
      // stale email is never restored); passwords and codes are cleared
      // when their page is left. A page transition moves focus to the new
      // page's first field (or the primary action).
      const pageChanged = view.renderedPage !== page;
      if (pageChanged) {
        if (view.renderedPage === "password") ui.password.input.value = "";
        if (view.renderedPage === "2fa") ui.code.input.value = "";
        view.renderedPage = page;
      }
      if (page === "email" && !ui.email.input.value) {
        // Restore only from a step the user actually reached: a submitted
        // email (the daemon asking for password/2FA) or a failed email
        // submit (step email with the error). A bare email step without an
        // error is no flow state to carry.
        const inFlow =
          !!login &&
          (login.step === "password" || login.step === "2fa" ||
            (login.step === "email" && !!login.error));
        if (inFlow && login.email) ui.email.input.value = login.email;
      }

      const showChoice = page === "choice";
      const showBrowser = page === "browser" && !!login && login.step === "browser";
      const showEmail = page === "email";
      const showPassword = page === "password";
      const showCode = page === "2fa";
      const showSigned = page === "signedin";
      if (ui.methods.hidden !== !showChoice) ui.methods.hidden = !showChoice;
      if (ui.email.wrap.hidden !== !showEmail) ui.email.wrap.hidden = !showEmail;
      if (ui.password.wrap.hidden !== !showPassword) ui.password.wrap.hidden = !showPassword;
      if (ui.code.wrap.hidden !== !showCode) ui.code.wrap.hidden = !showCode;
      if (ui.signed.hidden !== !showSigned) ui.signed.hidden = !showSigned;
      if (ui.browser.hidden !== !showBrowser) ui.browser.hidden = !showBrowser;
      if (ui.loginBody.hidden !== showBrowser) ui.loginBody.hidden = showBrowser;
      if (showSigned) {
        const identity = identityLine(snapshot);
        if (ui.signedIdentity.textContent !== identity) ui.signedIdentity.textContent = identity;
      }

      let body = LOGIN_SIGNED_BODY;
      if (showChoice) body = LOGIN_CHOICE_BODY;
      else if (showEmail) body = LOGIN_EMAIL_BODY;
      else if (showPassword) body = LOGIN_PASSWORD_BODY;
      else if (showCode) body = twoFactorBody(login);
      else if (showBrowser) body = LOGIN_WAITING_BODY;
      if (ui.loginBody.textContent !== body) ui.loginBody.textContent = body;

      // The first page's primary is the last-used method; the secondary
      // offers the other one.
      if (showChoice) {
        const alt = def === "password" ? "Continue in browser" : "Use password";
        if (ui.altMethod.textContent !== alt) ui.altMethod.textContent = alt;
        const disabled = !!view.busy || !!view.logoutBusy;
        if (ui.altMethod.disabled !== disabled) ui.altMethod.disabled = disabled;
      }

      if (showBrowser) {
        const url = login.url || "";
        const text = url || "Waiting for the sign-in link…";
        if (ui.browserURL.textContent !== text) {
          ui.browserURL.textContent = text;
          ui.browserURL.title = text;
        }
        // Open the loopback URL once per flow (module-level so a screen
        // switch does not navigate a one-shot URL twice). "Open again" is
        // the explicit repeat.
        if (currentStep === STEP_LOGIN && url && view.openedURL !== url && openedBrowserURL !== url) {
          openedBrowserURL = url;
          openURL(url);
        }
        view.openedURL = url;
        if (currentStep === STEP_LOGIN && login.id) startPolling(login.id);
        else stopPolling();
        ui.browserOpen.disabled = !url;
        ui.browserCancel.disabled = !login.id || view.cancelling;
        const cancelLabel = view.cancelling ? "Cancelling…" : "Cancel";
        if (ui.browserCancel.textContent !== cancelLabel) ui.browserCancel.textContent = cancelLabel;
      } else {
        stopPolling();
        view.openedURL = "";
      }

      // The credential fields mirror the wire's login error (aria-invalid);
      // the message itself lives on the status line.
      const invalid = !!((showEmail || showPassword || showCode) && login && login.error);
      const activeField = showEmail ? ui.email.input : showPassword ? ui.password.input : showCode ? ui.code.input : null;
      for (const input of [ui.email.input, ui.password.input, ui.code.input]) {
        if (input === activeField && invalid) input.setAttribute("aria-invalid", "true");
        else input.removeAttribute("aria-invalid");
        const disabled = !!view.busy || !!view.logoutBusy || input !== activeField;
        if (input.disabled !== disabled) input.disabled = disabled;
      }

      // A page transition (a login reply, a session change) moves focus to
      // the new page's field; Back and the step changes already do.
      if (pageChanged && currentStep === STEP_LOGIN) focusStep();
    }

    function renderHealth(snapshot) {
      maybeRunChecks(snapshot);
      const doctor = snapshot.doctor || null;
      const running = !!(doctor && doctor.running);
      if (ui.rerun.disabled !== running) ui.rerun.disabled = running;
      const key = (running ? "1" : "0") + "\u0000" + JSON.stringify((doctor && doctor.checks) || []);
      if (key !== view.listKey) {
        view.listKey = key;
        rebuildChecks(doctor);
      }
    }

    function renderAutoconnect(snapshot) {
      const settings = snapshot.settings || null;
      let value = settings ? (settings.autoconnect_effective ? "on" : "off") : "off";
      let pressed = !!(settings && settings.autoconnect_effective);
      if (view.autoBusy && view.autoPick) {
        value = view.autoPick;
        pressed = view.autoPick === "on";
      }
      if (ui.autoToggle.textContent !== value) ui.autoToggle.textContent = value;
      if (settings) ui.autoToggle.setAttribute("aria-pressed", pressed ? "true" : "false");
      else ui.autoToggle.removeAttribute("aria-pressed");
      const disabled = view.autoBusy || !settings;
      if (ui.autoToggle.disabled !== disabled) ui.autoToggle.disabled = disabled;
    }

    /**
     * The wizard status line, the inventory's priority: busy (`Signing
     * in...` / `Signing out...`) → local error → health running → health
     * summary → the login error (bad) → the view-model's notice → empty.
     * The "local error" slot carries the finish refusal, the sign-out
     * failure (the account notice, on the sign-in page) and the
     * autoconnect outcome (the settings notice, on its page). The health
     * running/summary entries render on the Health check step only: the
     * controller keeps the last report for the session, and a global
     * summary would mask every later login error (the chain's literal
     * order) — the inventory's summary is the health page's own status.
     * A stale "sign-in failed" notice against a live session is masked:
     * the fresh authenticated status cleared the error it repeated
     * (inventory §"Login failures").
     */
    function statusLine(snapshot) {
      const login = snapshot.login || null;
      if (view.busy) return { text: "Signing in...", tone: "" };
      if (view.logoutBusy) return { text: "Signing out...", tone: "" };
      if (view.finishError) return { text: view.finishError, tone: "bad" };
      const accountNotice = snapshot.account_notice;
      if (currentStep === STEP_LOGIN && accountNotice && accountNotice.tone === "bad") {
        return { text: accountNotice.message, tone: "bad" };
      }
      const settingsNotice = snapshot.settings_notice;
      if (currentStep === STEP_AUTOCONNECT && settingsNotice && settingsNotice.message) {
        return { text: settingsNotice.message, tone: settingsNotice.tone || "" };
      }
      if (currentStep === STEP_HEALTH) {
        const doctor = snapshot.doctor || null;
        if (doctor && doctor.running) return { text: "Running health checks...", tone: "" };
        const checks = (doctor && doctor.checks) || [];
        if (checks.length > 0) return summaryLine(checks);
      }
      if (currentStep === STEP_LOGIN && login && login.step === "browser") {
        return { text: "Waiting for sign-in in your browser...", tone: "" };
      }
      if (login && login.error) return { text: login.error, tone: "bad" };
      const notice = snapshot.notice;
      const stale =
        notice && notice.tone === "bad" &&
        String(notice.message || "").indexOf("sign-in failed: ") === 0 &&
        signedIn(snapshot);
      if (notice && notice.message && !stale) return { text: notice.message, tone: notice.tone || "" };
      return { text: "", tone: "" };
    }

    function renderStatus(snapshot) {
      const line = statusLine(snapshot);
      if (line.text === view.statusText && line.tone === view.statusTone) return;
      view.statusText = line.text;
      view.statusTone = line.tone;
      ui.status.textContent = line.text;
      ui.status.title = line.text;
      ui.status.hidden = line.text === "";
      if (line.tone === "good" || line.tone === "warn" || line.tone === "bad") {
        ui.status.dataset.tone = line.tone;
      } else {
        delete ui.status.dataset.tone;
      }
    }

    function renderNav() {
      let label = PRIMARY_LABELS[currentStep];
      let waiting = false;
      if (currentStep === STEP_LOGIN) {
        const page = view.page || derivePage(ctx.store.get());
        if (page === "choice") {
          label = defaultMethod(ctx.store.get()) === "password" ? "Use password" : "Continue in browser";
        } else if (page === "browser") {
          label = "Waiting for sign-in...";
          waiting = true;
        } else {
          label = "Continue";
        }
      }
      if (ui.primary.textContent !== label) ui.primary.textContent = label;
      const busy = !!view.busy || view.logoutBusy || view.finishBusy;
      const consentOK = currentStep !== STEP_WELCOME || consentTicked;
      const disabled = busy || !consentOK || waiting;
      if (ui.primary.disabled !== disabled) ui.primary.disabled = disabled;
      if (ui.back.disabled !== (!!view.busy || view.logoutBusy)) {
        ui.back.disabled = !!view.busy || view.logoutBusy;
      }
    }

    function render() {
      const snapshot = ctx.store.get();
      settleGuards(snapshot);
      autoAdvance(snapshot);
      renderStep();
      renderConsent();
      renderLogin(snapshot);
      renderHealth(snapshot);
      renderAutoconnect(snapshot);
      renderStatus(snapshot);
      renderNav();
    }

    // -------------------------------------------------------------- events

    ui.consentToggle.addEventListener("click", onConsentToggle);
    ui.primary.addEventListener("click", onPrimary);
    ui.altMethod.addEventListener("click", onSecondary);
    ui.browserOpen.addEventListener("click", function () {
      openURL((ctx.store.get().login || {}).url || "");
    });
    ui.browserCancel.addEventListener("click", cancelBrowserLogin);
    ui.back.addEventListener("click", onBack);
    ui.rerun.addEventListener("click", onRerun);
    ui.autoToggle.addEventListener("click", onAutoToggle);
    ui.signout.addEventListener("click", onSignout);
    ui.cancel.addEventListener("click", function () {
      closeDialog(true);
    });
    ui.confirm.addEventListener("click", onSignoutConfirm);
    // Esc closes the dialog natively (cancel -> close); every close returns
    // focus to the trigger. Enter activates the focused button, and Cancel
    // holds focus on open, so Enter cancels (the inventory's rule).
    ui.dialog.addEventListener("close", function () {
      if (disposed) return;
      returnFocus();
      render();
    });
    for (const input of [ui.email.input, ui.password.input, ui.code.input]) {
      input.addEventListener("keydown", function (event) {
        if (event.key !== "Enter") return;
        event.preventDefault();
        onPrimary();
      });
    }

    const unsubscribe = ctx.store.subscribe(render);

    // The last-method hint rides the account list; ask for it explicitly so
    // the first page's default is right even when the controller's start-up
    // load raced this mount.
    ctx.send("accounts_list", {}).then(function (err) {
      if (err) console.warn("kumiho: accounts list refused: " + err);
    });

    // The first step's field takes focus after the router's own host focus
    // (a microtask runs after show()'s synchronous focus call, before paint).
    queueMicrotask(function () {
      if (!disposed) focusStep();
    });

    cleanup = function () {
      disposed = true;
      unsubscribe();
      clearTimeout(view.busyTimer);
      clearTimeout(view.logoutTimer);
      clearTimeout(view.autoTimer);
      clearTimeout(view.finishTimer);
      stopPolling();
      if (ui.dialog.open) ui.dialog.close();
    };
  },

  unmount() {
    if (cleanup) cleanup();
    cleanup = null;
  },
};
