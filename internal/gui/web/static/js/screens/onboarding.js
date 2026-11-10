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
 * (login_email/login_password/login_2fa, plus login_start/login_status/
 * login_cancel/login_complete for the browser flow) and reads the
 * controller's Login projection: a terminal `done` reply advances to Health
 * check, an error renders on the status line, and a live session
 * (status.authenticated) always shows the signed-in page instead of the
 * credential fields — the daemon's "already signed in" refusal can never
 * dead-end the wizard (inventory §Onboarding wizard "Already-signed-in
 * handling").
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

import { t, tp, formatNumber, setLanguage, currentLanguage, hasTranslation } from "../i18n.js";

// The five steps, in order.
const STEP_WELCOME = 0;
const STEP_LOGIN = 1;
const STEP_HEALTH = 2;
const STEP_AUTOCONNECT = 3;
const STEP_FINISH = 4;
const STEP_COUNT = 5;

// The step titles (the page heading) and the primary button's label per step.
const STEP_TITLE_KEYS = [
  "onboarding.step.welcome",
  "onboarding.step.login",
  "onboarding.step.health",
  "onboarding.step.autoconnect",
  "onboarding.step.finish",
];
const PRIMARY_LABEL_KEYS = [
  "onboarding.primary.continue",
  "onboarding.primary.continue",
  "onboarding.primary.continue",
  "onboarding.primary.finish",
  "onboarding.primary.open",
];

function stepTitle(step) {
  return t(STEP_TITLE_KEYS[step]);
}

function primaryLabel(step) {
  return t(PRIMARY_LABEL_KEYS[step]);
}

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
const CONSENT_INTRO_KEY = "onboarding.consent.intro";
const CONSENT_GROUPS = [
  {
    headingKey: "onboarding.consent.group.does",
    itemKeys: [
      "onboarding.consent.item.signs_in",
      "onboarding.consent.item.tunnel",
      "onboarding.consent.item.udp",
    ],
  },
  {
    headingKey: "onboarding.consent.group.keep",
    itemKeys: [
      "onboarding.consent.item.unofficial",
      "onboarding.consent.item.kill_switch",
      "onboarding.consent.item.as_is",
    ],
  },
];
const CONSENT_TICK_KEY = "onboarding.consent.tick";

// The login pages' bodies (inventory §Sign in, extended by v0.6.0).
const LOGIN_CHOICE_BODY_KEY = "onboarding.login.choice_body";
const LOGIN_EMAIL_BODY_KEY = "onboarding.login.email_body";
const LOGIN_PASSWORD_BODY_KEY = "onboarding.login.password_body";
const LOGIN_CODE_BODY_KEY = "onboarding.login.code_body";
const LOGIN_LINK_BODY_KEY = "onboarding.login.link_body";
const LOGIN_SIGNED_BODY_KEY = "onboarding.login.signed_body";
const LOGIN_WAITING_BODY_KEY = "onboarding.login.waiting_body";

// The remaining steps' bodies and the shared sign-out dialog copy.
const HEALTH_BODY_KEY = "onboarding.health.body";
const AUTOCONNECT_BODY_KEY = "onboarding.autoconnect.body";
const FINISH_BODY_KEY = "onboarding.finish.body";
const SIGNOUT_QUESTION_KEY = "onboarding.login.sign_out_question";
const SIGNOUT_DETAIL_KEY = "onboarding.login.sign_out_detail";
const SIGNOUT_REVOKE_KEY = "onboarding.login.sign_out_revoke";

// Known daemon error codes get a translated summary/action; unknown codes
// use the generic summary and keep the raw English detail visible below.
const ERROR_KEYS = {
  bad_request: "error.bad_request",
  not_authenticated: "error.not_authenticated",
  region_blocked: "error.region_blocked",
  bootstrap_required: "error.bootstrap_required",
  forbidden: "error.forbidden",
  internal: "error.internal",
  not_implemented: "error.not_implemented",
  unsupported_setting: "error.unsupported_setting",
};

const HEALTH_STATUS_KEYS = {
  ok: "onboarding.health.status.ok",
  warn: "onboarding.health.status.warn",
  fail: "onboarding.health.status.fail",
  skip: "onboarding.health.status.skip",
};

const LANGUAGE_OPTION_KEYS = {
  system: "onboarding.language.system",
  en: "onboarding.language.english",
  fa: "onboarding.language.persian",
};

const LANGUAGE_NAME_KEYS = {
  en: "onboarding.language.english",
  fa: "onboarding.language.persian",
};

// The wizard's current language choice survives the router's remount (the
// picker lives only on Welcome, so the step and consent tick already do).
let pendingLanguageChoice = "";
let pendingLanguageError = "";

// Module-level session state: a router remount keeps the step and the
// consent tick (never persisted; a fresh launch reloads the module).
let currentStep = STEP_WELCOME;
let consentTicked = false;

// The browser URL opened once across remounts. Re-opening the same flow after
// a screen switch would open a second tab, so remember it; a new flow carries
// a new URL.
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
  const checksWord = tp("onboarding.health.check_count", checks.length, {
    checks: formatNumber(checks.length),
  });
  const warningsWord = tp("onboarding.health.warning_count", warned, {
    warnings: formatNumber(warned),
  });
  if (failed > 0) {
    return {
      text: t("onboarding.health.summary.failed", {
        checks: checksWord,
        failed: formatNumber(failed),
        warnings: warningsWord,
      }),
      tone: "bad",
    };
  }
  if (warned > 0) {
    return {
      text: t("onboarding.health.summary.warned", {
        checks: checksWord,
        ok: formatNumber(ok),
        warnings: warningsWord,
        skipped: formatNumber(skipped),
      }),
      tone: "warn",
    };
  }
  return { text: t("onboarding.health.summary.passed", { checks: checksWord }), tone: "good" };
}

/** The 2FA body: TOTP, the unblock code, or the emailed link/code flow. */
function twoFactorBody(login) {
  const flow = String((login && login.method) || "").toLowerCase();
  const method = String((login && login.verification_method) || "").toLowerCase();
  const reason = String((login && login.verification_reason) || "").toLowerCase();
  if (flow === "totp") return t("onboarding.login.totp_body");
  if (flow === "unblock" || reason === "unblock-code") {
    return t("onboarding.login.unblock_body");
  }
  if (method === "email" || method.indexOf("link") >= 0) return t("onboarding.login.link_body");
  return t("onboarding.login.code_body");
}

/** The translated summary/action for a known login error code, or the
 * generic summary for an unknown one; the raw English detail stays separate. */
function errorSummary(login) {
  const code = login && login.error_code;
  const key = code && ERROR_KEYS[code];
  return t(key || "error.generic");
}

/** The translated notice text plus its raw English engine detail, if any. */
function noticePresentation(notice) {
  if (notice && notice.code) {
    const key = "notice." + notice.code;
    if (hasTranslation(key)) {
      const params = notice.params || {};
      const detail = typeof params.error === "string" ? params.error : "";
      return { text: t(key, params), detail: detail };
    }
  }
  return { text: (notice && notice.message) || "", detail: "" };
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

  // The language choice is the first thing on the consent step: switching
  // language never reloads, and the module-level step/tick survive the
  // router's remount. System shows the OS resolution as a small note.
  const language = el("div", "onb__language");
  language.setAttribute("role", "group");
  language.setAttribute("aria-label", t("onboarding.language.label"));
  const languageLabel = el("span", "onb__language-label", t("onboarding.language.label"));
  const languageOptions = el("div", "onb__language-options");
  const languageButtons = {};
  for (const value of ["system", "en", "fa"]) {
    const button = el("button", "button button--tonal onb__language-option", t(LANGUAGE_OPTION_KEYS[value]));
    button.type = "button";
    button.dataset.value = value;
    button.setAttribute("aria-pressed", "false");
    languageOptions.append(button);
    languageButtons[value] = button;
  }
  const languageNote = el("p", "onb__language-note");
  languageNote.hidden = true;
  language.append(languageLabel, languageOptions, languageNote);
  panel.append(language);

  // Heading + the visual step progress (the heading carries the step's
  // identity; the progressbar is the accompanying position, exposed to AT).
  const head = el("div", "onb__head");
  const title = el("h2", "onb__title", stepTitle(STEP_WELCOME));
  const progress = el("div", "onb__progress");
  progress.setAttribute("role", "progressbar");
  progress.setAttribute("aria-label", t("onboarding.progress_aria"));
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
  consent.setAttribute("aria-label", t("onboarding.consent.region"));
  consent.append(el("p", "onb__consent-text", t(CONSENT_INTRO_KEY)));
  for (const group of CONSENT_GROUPS) {
    const heading = el("h3", "onb__consent-heading", t(group.headingKey));
    const list = el("ul", "onb__consent-list");
    for (const itemKey of group.itemKeys) list.append(el("li", null, t(itemKey)));
    consent.append(heading, list);
  }
  consent.append(el("p", "onb__consent-text", t(CONSENT_TICK_KEY)));
  const consentToggle = el("button", "onb__consent-toggle");
  consentToggle.type = "button";
  consentToggle.id = "onb-consent";
  consentToggle.setAttribute("role", "checkbox");
  consentToggle.setAttribute("aria-checked", "false");
  const consentBox = el("span", "onb__consent-box");
  consentBox.setAttribute("aria-hidden", "true");
  consentToggle.append(consentBox, el("span", "onb__consent-label", t("onboarding.consent.understand")));
  welcome.append(consent, consentToggle);

  // Step 2: sign in (body + signed-in card + the three credential fields).
  const login = el("section", "onb__step");
  login.dataset.step = "login";
  login.hidden = true;
  const loginBody = el("p", "onb__body", t(LOGIN_CHOICE_BODY_KEY));
  // The first page offers both methods. The primary button is the default
  // (the last successful method, browser on first run); this secondary
  // choice is the other one.
  const methods = el("div", "onb__methods");
  const altMethod = el("button", "button button--tonal onb__method", t("onboarding.login.use_password"));
  altMethod.type = "button";
  altMethod.id = "onb-method-alt";
  methods.append(altMethod);
  methods.hidden = true;
  const signed = el("div", "onb__signed");
  const signedIdentity = el("p", "onb__signed-identity");
  const signout = el("button", "button button--tonal onb__signout", t("onboarding.login.sign_out"));
  signout.type = "button";
  signed.append(signedIdentity, signout);
  signed.hidden = true;
  const email = fieldBox("onb-email", t("onboarding.login.email_label"), "email", "you@example.com");
  email.input.dir = "ltr";
  const password = fieldBox("onb-password", t("onboarding.login.password_label"), "password", "");
  password.input.dir = "ltr";
  const code = fieldBox("onb-code", t("onboarding.login.code_label"), "text", "");
  code.input.dir = "ltr";
  password.wrap.hidden = true;
  code.wrap.hidden = true;
  // The browser waiting card: the engine URL, the pasted redirect URL,
  // Open again and Cancel.
  const browser = el("div", "onb__browser");
  browser.hidden = true;
  const browserBody = el("p", "onb__body", t(LOGIN_WAITING_BODY_KEY));
  const browserURL = el("p", "onb__browser-url");
  const browserRedirect = fieldBox("onb-browser-redirect", t("onboarding.login.redirect_label"), "text", t("onboarding.login.redirect_placeholder"));
  browserRedirect.input.dir = "auto";
  const browserError = el("p", "onb__browser-error");
  browserError.id = "onb-browser-error";
  browserError.setAttribute("aria-live", "polite");
  browserError.hidden = true;
  browserRedirect.input.setAttribute("aria-describedby", browserError.id);
  const browserActions = el("div", "onb__browser-actions");
  const browserOpen = el("button", "button button--tonal onb__browser-open", t("onboarding.login.open_again"));
  browserOpen.type = "button";
  browserOpen.id = "onb-browser-open";
  const browserCancel = el("button", "button button--text onb__browser-cancel", t("onboarding.login.cancel"));
  browserCancel.type = "button";
  browserCancel.id = "onb-browser-cancel";
  const browserContinue = el("button", "button button--filled onb__browser-continue", t("onboarding.login.continue"));
  browserContinue.type = "button";
  browserContinue.id = "onb-browser-continue";
  browserActions.append(browserOpen, browserCancel, browserContinue);
  browser.append(browserBody, browserURL, browserRedirect.wrap, browserError, browserActions);
  login.append(loginBody, methods, signed, email.wrap, password.wrap, code.wrap, browser);

  // Step 3: health check (the run summary lives on the status line; the
  // rows render compactly, one line per check).
  const health = el("section", "onb__step");
  health.dataset.step = "health";
  health.hidden = true;
  const healthBody = el("p", "onb__body", t(HEALTH_BODY_KEY));
  const healthActions = el("div", "onb__health-actions");
  const rerun = el("button", "button button--tonal onb__rerun", t("onboarding.health.rerun"));
  rerun.type = "button";
  rerun.id = "onb-rerun";
  healthActions.append(rerun);
  const checks = el("ul", "onb__checks");
  checks.id = "onb-checks";
  checks.tabIndex = 0;
  checks.setAttribute("aria-label", t("onboarding.health.results_aria"));
  const checksEmpty = el("p", "onb__checks-empty", t("onboarding.health.checking"));
  checksEmpty.id = "onb-checks-empty";
  health.append(healthBody, healthActions, checks, checksEmpty);

  // Step 4: autoconnect (the daemon's current effective value, flipped
  // immediately through settings_set).
  const autoconnect = el("section", "onb__step");
  autoconnect.dataset.step = "autoconnect";
  autoconnect.hidden = true;
  const autoBody = el("p", "onb__body", t(AUTOCONNECT_BODY_KEY));
  const autoRow = el("div", "onb__row");
  autoRow.append(el("span", "onb__row-label", t("onboarding.autoconnect.label")));
  const autoToggle = el("button", "button button--tonal onb__toggle", t("onboarding.autoconnect.off"));
  autoToggle.type = "button";
  autoToggle.id = "onb-autoconnect";
  autoToggle.setAttribute("aria-label", t("onboarding.autoconnect.label"));
  autoRow.append(autoToggle);
  autoconnect.append(autoBody, autoRow);

  // Step 5: all set.
  const finish = el("section", "onb__step");
  finish.dataset.step = "finish";
  finish.hidden = true;
  const finishBody = el("p", "onb__body", t(FINISH_BODY_KEY));
  finish.append(finishBody);

  panel.append(welcome, login, health, autoconnect, finish);

  // Status line (the wizard-global priority; owned by the screen) and the
  // raw English engine detail block (always LTR; never translated).
  const status = el("p", "onb__status");
  status.setAttribute("aria-live", "polite");
  status.hidden = true;
  const details = el("p", "onb__details");
  details.dir = "ltr";
  details.hidden = true;

  // Navigation: Back (tonal, hidden on the first and last pages) and the
  // per-step primary action.
  const nav = el("div", "onb__nav");
  const back = el("button", "button button--tonal onb__back", t("onboarding.back"));
  back.type = "button";
  back.id = "onb-back";
  const primary = el("button", "button button--filled onb__primary", primaryLabel(STEP_WELCOME));
  primary.type = "button";
  primary.id = "onb-primary";
  nav.append(back, primary);

  // The shared sign-out confirmation (the house dialog pattern, native;
  // Cancel focused by default, Esc and Enter cancel).
  const dialog = el("dialog", "dialog onb__dialog");
  const dialogTitle = el("h2", "dialog__title", t("onboarding.login.sign_out_title"));
  dialogTitle.id = "onb-signout-title";
  const dialogQuestion = el("p", "dialog__body", t(SIGNOUT_QUESTION_KEY));
  dialogQuestion.id = "onb-signout-question";
  const dialogDetail = el("p", "dialog__body", t(SIGNOUT_DETAIL_KEY));
  dialogDetail.id = "onb-signout-detail";
  const dialogRevoke = el("p", "dialog__body", t(SIGNOUT_REVOKE_KEY));
  dialogRevoke.id = "onb-signout-revoke";
  dialog.setAttribute("aria-labelledby", dialogTitle.id);
  dialog.setAttribute("aria-describedby", dialogQuestion.id + " " + dialogDetail.id + " " + dialogRevoke.id);
  const cancel = el("button", "button button--tonal onb__dialog-cancel", t("onboarding.login.sign_out_cancel"));
  cancel.type = "button";
  const confirm = el("button", "button button--danger onb__dialog-confirm", t("onboarding.login.sign_out_confirm"));
  confirm.type = "button";
  const dialogActions = el("div", "dialog__actions");
  dialogActions.append(cancel, confirm);
  dialog.append(dialogTitle, dialogQuestion, dialogDetail, dialogRevoke, dialogActions);

  root.append(panel, status, details, nav, dialog);
  host.append(root);

  return {
    root: root,
    language: language,
    languageButtons: languageButtons,
    languageNote: languageNote,
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
    browserRedirect: browserRedirect,
    browserError: browserError,
    browserOpen: browserOpen,
    browserCancel: browserCancel,
    browserContinue: browserContinue,
    rerun: rerun,
    checks: checks,
    checksEmpty: checksEmpty,
    autoToggle: autoToggle,
    back: back,
    primary: primary,
    status: status,
    details: details,
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
      // Browser completion: the pasted redirect URL's round trip and its
      // screen-local error.
      completing: false,
      completingMark: "",
      completingTimer: 0,
      redirectError: "",
      redirectErrorSummary: "",
      redirectErrorDetail: "",
      redirectID: "",
      languageChoice: pendingLanguageChoice,
      languageError: pendingLanguageError,
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
      // The consent step's region preflight: true while region_check is in
      // flight, and the branch navigates to the guide only once.
      regionChecking: false,
      regionChecked: false,
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
      if (!email) return t("onboarding.login.signed_in");
      // Keep the Latin email in its own left-to-right isolate inside the
      // Persian sentence; the engine email itself is never rewritten.
      return t("onboarding.login.signed_in_as", { email: "\u2066" + email + "\u2069" });
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
        login.error_code || "",
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
     * the daemon asks for them, the browser waiting card during a browser
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
      // state): the password/2FA pages and the signed-in variant. A
      // dispatch refusal is a shared onboarding notice, not a login
      // projection change; keep the submitted form so retry and the raw
      // detail stay together.
      const notice = ctx.store.get().notice;
      const refused = view.submitPage && notice && notice.tone === "bad" &&
        typeof notice.code === "string" && notice.code.indexOf("onboarding.sign_in.") === 0;
      if (refused) pinPage(view.submitPage);
      else view.page = derivePage(ctx.store.get());
      view.submitPage = "";
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
      const settings = snapshot.settings;
      if (pendingLanguageChoice && settings && settings.language === pendingLanguageChoice) {
        pendingLanguageChoice = "";
        view.languageChoice = "";
        view.languageError = "";
      }
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
      if (view.completing && loginMark(snapshot) !== view.completingMark) {
        endCompleting();
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

    /** The picker's saved preference; System is the pre-settings default. */
    function languagePreference(snapshot) {
      return (snapshot.settings && snapshot.settings.language) || "system";
    }

    /** The effective language for a preference, with the cache as the
     * pre-settings System fallback. */
    function effectiveLanguage(choice, snapshot) {
      if (choice === "en" || choice === "fa") return choice;
      if (snapshot.system_language === "en" || snapshot.system_language === "fa") {
        return snapshot.system_language;
      }
      return currentLanguage();
    }

    /** Switch immediately, persist the engine preference and cache the
     * effective language for the next first paint. A refusal rolls the UI
     * back and keeps the raw detail on the status line. */
    function onLanguageChoice(choice) {
      const snapshot = ctx.store.get() || {};
      const previous = currentLanguage();
      const effective = effectiveLanguage(choice, snapshot);
      pendingLanguageChoice = choice;
      pendingLanguageError = "";
      view.languageChoice = choice;
      view.languageError = "";
      render();
      setLanguage(effective).catch(function (err) {
        console.error("kumiho: language switch failed", err);
      });
      ctx.send("settings_set", { key: "language", value: choice }).then(function (err) {
        if (!err) {
          return ctx.send("prefs_set", { language: effective }).then(function (cacheErr) {
            if (cacheErr) console.warn("kumiho: could not cache the language: " + cacheErr);
          });
        }
        pendingLanguageChoice = "";
        pendingLanguageError = err;
        view.languageChoice = "";
        view.languageError = err;
        setLanguage(previous).catch(function (switchErr) {
          console.error("kumiho: language rollback failed", switchErr);
        });
        render();
        console.warn("kumiho: language change refused: " + err);
      });
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
          else if (page === "browser") target = ui.browserRedirect.input;
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

    function endCompleting() {
      view.completing = false;
      clearTimeout(view.completingTimer);
      view.completingTimer = 0;
    }

    // The pasted URL's round trip: the host accepts controller actions
    // immediately and their outcome arrives through the state stream, so the
    // field and buttons stay disabled until the login projection moves or the
    // settle timer releases the guard.
    function beginCompleting() {
      view.completing = true;
      view.completingMark = loginMark(ctx.store.get());
      clearTimeout(view.completingTimer);
      view.completingTimer = setTimeout(function () {
        view.completingTimer = 0;
        if (view.completing) {
          endCompleting();
          render();
        }
      }, SETTLE_MS);
      render();
    }

    // Continue sends the pasted redirect URL. A blank field is rejected
    // locally with the field's own error; the URL itself is never logged.
    function submitRedirect() {
      const login = ctx.store.get().login || null;
      if (!login || login.step !== "browser" || !login.id || view.completing) return;
      const url = ui.browserRedirect.input.value.trim();
      if (!url) {
        view.redirectError = "required";
        view.redirectErrorSummary = t("onboarding.login.redirect_required");
        view.redirectErrorDetail = "";
        render();
        ui.browserRedirect.input.focus();
        return;
      }
      view.redirectError = "";
      beginCompleting();
      ctx.send("login_complete", { login_id: login.id, redirect_url: url }).then(function (err) {
        if (!err) return;
        endCompleting();
        view.redirectError = err;
        view.redirectErrorSummary = t("onboarding.login.redirect_failed");
        view.redirectErrorDetail = err;
        render();
        console.warn("kumiho: login_complete refused: " + err);
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
      view.submitPage = page;
      view.pagePinned = false;
      beginBusy();
      ctx.send(action, payload).then(function (err) {
        if (err) {
          endBusy();
          // A refused submit keeps the field the user was on so the
          // Persian summary and the raw detail sit beside the retry.
          pinPage(page);
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
      // The doctor's name and facts stay English; isolate each Latin run so
      // the Persian status word between them cannot reorder the line.
      item.append(el("bdi", null, check.title || check.name || ""), ": ");
      const statusKey = HEALTH_STATUS_KEYS[check.status];
      const word = el("span", "onb__check-word", statusKey ? t(statusKey) : (check.status_word || ""));
      if (check.tone) word.dataset.tone = check.tone;
      item.append(word, ": ");
      item.append(el("bdi", null, check.detail || ""));
      if (check.remediation && (check.status === "warn" || check.status === "fail")) {
        item.append(" — ", el("bdi", null, check.remediation));
      }
      return item;
    }

    function rebuildChecks(doctor) {
      const running = !!(doctor && doctor.running);
      const list = (doctor && doctor.checks) || [];
      if (running || list.length === 0) {
        ui.checks.hidden = true;
        ui.checksEmpty.hidden = false;
        ui.checksEmpty.textContent = running ? t("onboarding.health.checking") : t("onboarding.health.empty");
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
          if (!consentTicked || view.regionChecking) return;
          {
            const snapshot = ctx.store.get() || {};
            const check = view.regionChecked ? null : snapshot.region_check;
            if (check) {
              view.regionChecked = true;
              if ((check.blocked && check.blocked.length) || check.browser_blocked) {
                ctx.navigate("blocked-region");
                return;
              }
              goTo(STEP_LOGIN);
              return;
            }
            view.regionChecking = true;
            render();
            ctx.send("region_check", {}).then(function (err) {
              if (!err) return;
              // A daemon that cannot probe must never trap the wizard.
              view.regionChecking = false;
              view.regionChecked = true;
              goTo(STEP_LOGIN);
              render();
            });
          }
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
      ui.language.hidden = currentStep !== STEP_WELCOME;
      ui.title.textContent = stepTitle(currentStep);
      ui.progress.setAttribute("aria-valuenow", String(currentStep + 1));
      ui.progress.setAttribute("aria-valuetext", t("onboarding.step_position", {
        current: formatNumber(currentStep + 1),
        total: formatNumber(STEP_COUNT),
      }));
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

      let body = t(LOGIN_SIGNED_BODY_KEY);
      if (showChoice) body = t(LOGIN_CHOICE_BODY_KEY);
      else if (showEmail) body = t(LOGIN_EMAIL_BODY_KEY);
      else if (showPassword) body = t(LOGIN_PASSWORD_BODY_KEY);
      else if (showCode) body = twoFactorBody(login);
      else if (showBrowser) body = t(LOGIN_WAITING_BODY_KEY);
      if (ui.loginBody.textContent !== body) ui.loginBody.textContent = body;

      // The first page's primary is the last-used method; the secondary
      // offers the other one.
      if (showChoice) {
        const alt = def === "password"
          ? t("onboarding.login.continue_in_browser")
          : t("onboarding.login.use_password");
        if (ui.altMethod.textContent !== alt) ui.altMethod.textContent = alt;
        const disabled = !!view.busy || !!view.logoutBusy;
        if (ui.altMethod.disabled !== disabled) ui.altMethod.disabled = disabled;
      }

      if (showBrowser) {
        const url = login.url || "";
        const text = url || t("onboarding.login.waiting_link");
        if (ui.browserURL.textContent !== text) {
          ui.browserURL.textContent = text;
          ui.browserURL.title = text;
        }
        // A new flow starts with an empty paste field, never the previous
        // flow's one-shot code.
        if ((login.id || "") !== view.redirectID) {
          view.redirectID = login.id || "";
          ui.browserRedirect.input.value = "";
          view.redirectError = "";
          view.redirectErrorSummary = "";
          view.redirectErrorDetail = "";
        }
        let error = "";
        if (view.redirectError) error = view.redirectErrorSummary || t("onboarding.login.redirect_failed");
        else if (login && login.error) error = errorSummary(login);
        if (ui.browserError.textContent !== error) ui.browserError.textContent = error;
        ui.browserError.hidden = !error;
        if (error) ui.browserRedirect.input.setAttribute("aria-invalid", "true");
        else ui.browserRedirect.input.removeAttribute("aria-invalid");
        // Open the browser URL once per flow (module-level so a screen switch
        // does not open a second tab). "Open again" is the explicit repeat.
        if (currentStep === STEP_LOGIN && url && view.openedURL !== url && openedBrowserURL !== url) {
          openedBrowserURL = url;
          openURL(url);
        }
        view.openedURL = url;
        if (currentStep === STEP_LOGIN && login.id) startPolling(login.id);
        else stopPolling();
        ui.browserOpen.disabled = !url;
        ui.browserCancel.disabled = !login.id || view.cancelling || view.completing;
        const cancelLabel = view.cancelling
          ? t("onboarding.login.cancelling")
          : t("onboarding.login.cancel");
        if (ui.browserCancel.textContent !== cancelLabel) ui.browserCancel.textContent = cancelLabel;
        ui.browserRedirect.input.disabled = !!view.busy || !!view.logoutBusy || view.completing;
        ui.browserContinue.disabled = !!view.busy || !!view.logoutBusy || view.completing || !login.id;
        const continueLabel = view.completing
          ? t("onboarding.login.signing_in")
          : t("onboarding.login.continue");
        if (ui.browserContinue.textContent !== continueLabel) ui.browserContinue.textContent = continueLabel;
      } else {
        stopPolling();
        endCompleting();
        // A one-shot URL's code must not linger in the DOM after the flow.
        ui.browserRedirect.input.value = "";
        ui.browserError.textContent = "";
        ui.browserError.hidden = true;
        view.redirectID = "";
        view.redirectError = "";
        view.redirectErrorSummary = "";
        view.redirectErrorDetail = "";
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
      const label = value === "on" ? t("onboarding.autoconnect.on") : t("onboarding.autoconnect.off");
      if (ui.autoToggle.textContent !== label) ui.autoToggle.textContent = label;
      ui.autoToggle.setAttribute("aria-label", t("onboarding.autoconnect.label") + ": " + label);
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
      if (view.busy) return { text: t("onboarding.login.signing_in_status"), tone: "", detail: "" };
      if (view.logoutBusy) return { text: t("onboarding.login.signing_out_status"), tone: "", detail: "" };
      if (view.finishError) {
        return {
          text: t("onboarding.finish.failed"),
          tone: "bad",
          detail: view.finishError,
        };
      }
      if (view.languageError) {
        return {
          text: t("onboarding.language.save_failed"),
          tone: "bad",
          detail: view.languageError,
        };
      }
      const accountNotice = snapshot.account_notice;
      if (currentStep === STEP_LOGIN && accountNotice && accountNotice.tone === "bad") {
        const notice = noticePresentation(accountNotice);
        return { text: notice.text, tone: "bad", detail: notice.detail };
      }
      const settingsNotice = snapshot.settings_notice;
      if (currentStep === STEP_AUTOCONNECT && settingsNotice && settingsNotice.tone) {
        const notice = noticePresentation(settingsNotice);
        if (notice.text) return { text: notice.text, tone: settingsNotice.tone || "", detail: notice.detail };
      }
      if (currentStep === STEP_HEALTH) {
        const doctor = snapshot.doctor || null;
        if (doctor && doctor.running) return { text: t("onboarding.health.running_status"), tone: "", detail: "" };
        const checks = (doctor && doctor.checks) || [];
        if (checks.length > 0) return summaryLine(checks);
      }
      if (currentStep === STEP_LOGIN && login && login.step === "browser") {
        return { text: t("onboarding.login.browser_waiting_status"), tone: "", detail: "" };
      }
      const notice = snapshot.notice;
      const stale =
        notice && notice.tone === "bad" &&
        notice.code === "onboarding.sign_in.failed" &&
        signedIn(snapshot);
      const presentation = notice && notice.tone && !stale
        ? noticePresentation(notice)
        : null;
      if (login && login.error) {
        if (login.error_code && ERROR_KEYS[login.error_code]) {
          return { text: errorSummary(login), tone: "bad", detail: login.error };
        }
        // A dispatch refusal is carried by the structured notice; it is
        // newer and more specific than a stale login projection error.
        if (presentation && notice.tone === "bad" && presentation.text) {
          return { text: presentation.text, tone: "bad", detail: presentation.detail };
        }
        return { text: errorSummary(login), tone: "bad", detail: login.error };
      }
      if (presentation && presentation.text) {
        return { text: presentation.text, tone: notice.tone || "", detail: presentation.detail };
      }
      return { text: "", tone: "", detail: "" };
    }

    function renderStatus(snapshot) {
      const line = statusLine(snapshot);
      const login = snapshot.login || null;
      const rawLogin = login && login.error ? login.error : "";
      const detail = line.detail || view.redirectErrorDetail || rawLogin || "";
      if (line.text === view.statusText && line.tone === view.statusTone && detail === view.statusDetail) return;
      view.statusText = line.text;
      view.statusTone = line.tone;
      view.statusDetail = detail;
      ui.status.textContent = line.text;
      ui.status.title = line.text;
      ui.status.hidden = line.text === "";
      if (line.tone === "good" || line.tone === "warn" || line.tone === "bad") {
        ui.status.dataset.tone = line.tone;
      } else {
        delete ui.status.dataset.tone;
      }
      ui.details.textContent = detail;
      ui.details.hidden = detail === "";
    }

    function renderNav() {
      let label = primaryLabel(currentStep);
      let waiting = false;
      if (currentStep === STEP_LOGIN) {
        const page = view.page || derivePage(ctx.store.get());
        if (page === "choice") {
          label = defaultMethod(ctx.store.get()) === "password"
            ? t("onboarding.login.use_password")
            : t("onboarding.login.continue_in_browser");
        } else if (page === "browser") {
          label = t("onboarding.login.waiting_button");
          waiting = true;
        } else {
          label = t("onboarding.login.continue");
        }
      }
      const busy = !!view.busy || view.logoutBusy || view.finishBusy || view.regionChecking;
      const consentOK = currentStep !== STEP_WELCOME || consentTicked;
      const disabled = busy || !consentOK || waiting;
      if (currentStep === STEP_WELCOME && view.regionChecking) label = t("onboarding.login.checking_hosts");
      if (ui.primary.textContent !== label) ui.primary.textContent = label;
      if (ui.primary.disabled !== disabled) ui.primary.disabled = disabled;
      if (ui.back.disabled !== (!!view.busy || view.logoutBusy)) {
        ui.back.disabled = !!view.busy || view.logoutBusy;
      }
    }

    function renderLanguage(snapshot) {
      const preference = view.languageChoice || languagePreference(snapshot);
      ui.language.hidden = currentStep !== STEP_WELCOME;
      for (const value of Object.keys(ui.languageButtons)) {
        const button = ui.languageButtons[value];
        const pressed = value === preference ? "true" : "false";
        if (button.getAttribute("aria-pressed") !== pressed) button.setAttribute("aria-pressed", pressed);
      }
      const systemCode = snapshot.system_language === "fa" || snapshot.system_language === "en"
        ? snapshot.system_language
        : currentLanguage();
      const systemName = t(LANGUAGE_NAME_KEYS[systemCode]);
      const note = t("onboarding.language.system_note", { language: systemName });
      if (ui.languageNote.textContent !== note) ui.languageNote.textContent = note;
      ui.languageNote.hidden = preference !== "system";
    }

    function render() {
      const snapshot = ctx.store.get();
      settleGuards(snapshot);
      autoAdvance(snapshot);
      maybeRegionBranch(snapshot);
      renderStep();
      renderConsent();
      renderLanguage(snapshot);
      renderLogin(snapshot);
      renderHealth(snapshot);
      renderAutoconnect(snapshot);
      renderStatus(snapshot);
      renderNav();
    }

    // maybeRegionBranch consumes the consent preflight once it lands: a
    // blocked or unknown browser page opens the reusable Blocked region guide
    // before sign-in; a clear region proceeds silently.
    function maybeRegionBranch(snapshot) {
      if (!view.regionChecking) return;
      const check = snapshot.region_check;
      if (!check) return;
      view.regionChecking = false;
      view.regionChecked = true;
      if ((check.blocked && check.blocked.length) || check.browser_blocked) {
        ctx.navigate("blocked-region");
        return;
      }
      goTo(STEP_LOGIN);
    }

    // -------------------------------------------------------------- events

    ui.consentToggle.addEventListener("click", onConsentToggle);
    for (const value of Object.keys(ui.languageButtons)) {
      ui.languageButtons[value].addEventListener("click", function () {
        onLanguageChoice(value);
      });
    }
    ui.primary.addEventListener("click", onPrimary);
    ui.altMethod.addEventListener("click", onSecondary);
    ui.browserOpen.addEventListener("click", function () {
      openURL((ctx.store.get().login || {}).url || "");
    });
    ui.browserCancel.addEventListener("click", cancelBrowserLogin);
    ui.browserContinue.addEventListener("click", submitRedirect);
    ui.browserRedirect.input.addEventListener("keydown", function (event) {
      if (event.key !== "Enter") return;
      event.preventDefault();
      submitRedirect();
    });
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
      clearTimeout(view.completingTimer);
      stopPolling();
      if (ui.dialog.open) ui.dialog.close();
    };
  },

  unmount() {
    if (cleanup) cleanup();
    cleanup = null;
  },
};
