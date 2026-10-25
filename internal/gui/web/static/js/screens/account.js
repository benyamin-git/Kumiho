/* The Account screen (inventory §Account, extended by v0.6.0): the account
 * list and rotation card above the entitlement card (Email, UID,
 * Subscription, Quota, Bandwidth, Token expiry) with its Yes/No/Unknown and
 * Limited/Unlimited words and tones, the tonal Sign out action, its modal
 * confirmation dialog (a native <dialog>: Cancel focused by default, Esc and
 * Enter cancel, the destructive confirm in the loss colour), and the status
 * line.
 *
 * The v0.6.0 rows are the daemon's `accounts_list` projection: label/email,
 * method word, active marker, re-auth warning and the quota percent left.
 * Each row's overflow menu dispatches Use, Rename, Re-authenticate and
 * Remove; Add account offers browser sign-in (the first choice) or the
 * password form reused from onboarding. A browser flow shows the waiting
 * card, opens the URL once through `open_external`, polls
 * `accounts_add_status` every second, finishes through `login_complete` with
 * the redirect URL pasted from the browser's address bar, and cancels through
 * `accounts_add_cancel`. Rotation is a switch plus a 1–50% threshold.
 *
 * Everything renders from the store's state envelopes (the controller
 * projection); actions go through the transport and the screen re-renders
 * from the next envelope — "" means accepted, never completed (T1.7). The
 * wire carries no account-loading or logout-in-flight flag (reported as
 * projection gaps): "Loading account..." is `account` absent with no
 * account notice, and the sign-out guard runs screen-side from the confirm
 * click until the logout's completion evidence — the account notice moving
 * ("Signed out" / "could not sign out: <err>") or the reloaded account
 * snapshot changing — with the T2.2 pattern's settle timer behind a
 * byte-identical repeat.
 *
 * The card gate is the inventory's: the card and the Sign out button appear
 * only for a loaded, signed-in snapshot (local session facts present). An
 * anonymous snapshot — the daemon answers "not signed in" — shows no card
 * and the status line's "Not signed in". A non-fatal entitlement-lookup
 * failure keeps the local rows (Subscription `Unknown`, warn) and warns on
 * the status line; the row values pass through the wire's ~raw projection,
 * so this screen formats the quota (humanBytes) and the expiry (RFC3339)
 * with the controller's exact wording.
 *
 * Wording authority: the row labels and order, the Yes/No/Unknown and
 * Limited/Unlimited words with their tones, the status-line priority, the
 * sign-out dialog body lines, and the capture hint
 * (`#/account?state=signout-confirm`, scripts/visual/README.md) come from
 * inventory §Account. The shell header's title row supplies the `Account`
 * heading (T2.1; screens do not repeat it).
 *
 * The router links css/screens/account.css from the registry entry.
 */

import { accountMethodWord, accountName, accountQuotaPercent } from "../store.js";

// The sign-out guard's settle (the dashboard/locations/settings T2.2
// pattern): a logout whose completion evidence repeats byte-for-byte stops
// blocking after this long.
const SETTLE_MS = 3000;

// Browser sign-in polls the daemon once a second (the protocol's status
// cadence).
const POLL_MS = 1000;

// The daemon's account cap; Add account is disabled at the limit.
const MAX_ACCOUNTS = 5;

// The card's rows in the inventory's canonical order.
const ROWS = [
  ["email", "Email"],
  ["uid", "UID"],
  ["subscription", "Subscription"],
  ["quota", "Quota"],
  ["bandwidth", "Bandwidth"],
  ["token_expiry", "Token expiry"],
];

// The sign-out confirmation's revoke disclosure (v0.6.0): sign-out best-effort
// revokes the Mozilla sign-in, and the local sign-out always completes.
const SIGNOUT_REVOKE =
  "Kumiho will also try to revoke its Mozilla sign-in; local sign-out completes offline.";
const SIGNOUT_REVOKE_ALL =
  "Kumiho will also try to revoke its Mozilla sign-ins; local sign-out completes offline.";

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

/** Binary units, the controller's humanBytes wording ("38.0 GiB", "512 B"). */
function humanBytes(n) {
  if (n < 1024) return n + " B";
  let div = 1024;
  let exp = 0;
  for (let v = Math.floor(n / 1024); v >= 1024; v = Math.floor(v / 1024)) {
    div *= 1024;
    exp += 1;
  }
  return (n / div).toFixed(1) + " " + "KMGTPE"[exp] + "iB";
}

/** RFC3339 UTC, the controller's time.RFC3339 ("2026-11-01T12:00:00Z"). */
function rfc3339(unixSeconds) {
  return new Date(unixSeconds * 1000).toISOString().replace(/\.\d{3}Z$/, "Z");
}

/** A loaded snapshot is signed in when its local session facts are present. */
function signedIn(account) {
  return !!account && (!!account.email || !!account.uid);
}

/** The Subscription row value and tone (inventory §Account). */
function subscriptionView(account) {
  if (account.error) return { text: "Unknown", tone: "warn" };
  if (account.subscribed === true) return { text: "Yes", tone: "good" };
  if (account.subscribed === false) return { text: "No", tone: "" };
  // The wire always carries the bool; an absent one is no answer to show,
  // not a "No".
  return { text: "", tone: "" };
}

/** The Quota row: "X remaining of Y", "X remaining" or "Y per period". */
function quotaText(account) {
  const remaining = account.quota_remaining;
  const limit = account.max_bytes;
  if (typeof remaining === "number" && typeof limit === "number") {
    return humanBytes(remaining) + " remaining of " + humanBytes(limit);
  }
  if (typeof remaining === "number") return humanBytes(remaining) + " remaining";
  if (typeof limit === "number") return humanBytes(limit) + " per period";
  return "";
}

/** A cheap identity for the account snapshot (see fingerprint). */
function accountSignature(account) {
  return account ? JSON.stringify(account) : "";
}

/** The notice identity used by the sign-out guard. */
function noticeMark(notice) {
  return notice && notice.message ? notice.message + "\u0000" + (notice.tone || "") : "";
}

/** The login projection's identity for the add-flow guards. */
function loginMark(login) {
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

/** The 2FA form body, including the TOTP and unblock-code fallbacks. */
function twoFactorBody(login) {
  const method = String((login && login.method) || "").toLowerCase();
  const verification = String((login && login.verification_method) || "").toLowerCase();
  const reason = String((login && login.verification_reason) || "").toLowerCase();
  if (method === "totp") return "Enter the code from your authenticator app.";
  if (method === "unblock" || reason === "unblock-code") {
    return "Enter the unblock code Mozilla emailed you.";
  }
  if (verification === "email-2fa" || method === "email" || verification.indexOf("link") >= 0) {
    return "Check your email and open the Mozilla sign-in link, then press Continue with the code empty to finish signing in.";
  }
  return "Enter the verification code Mozilla sent you.";
}

/** A labeled input group (label always visible, never placeholder-only). */
function fieldBox(id, labelText, type, autocomplete) {
  const wrap = el("div", "field acct__field");
  const label = el("label", "field__label", labelText);
  label.htmlFor = id;
  const input = document.createElement("input");
  input.className = "input";
  input.id = id;
  input.type = type;
  input.autocomplete = autocomplete || "off";
  input.spellcheck = false;
  wrap.append(label, input);
  return { wrap: wrap, input: input };
}

/** Build the screen's DOM and return its element references. */
function build(host) {
  const root = el("div", "acct");

  // Header actions: Refresh, Add account and the session-wide Sign out.
  const refresh = el("button", "button button--tonal acct__refresh", "Refresh");
  refresh.type = "button";
  const add = el("button", "button button--filled acct__add", "Add account");
  add.type = "button";
  const signout = el("button", "button button--tonal acct__signout", "Sign out");
  signout.type = "button";
  signout.hidden = true;
  const actions = el("div", "acct__actions");
  actions.append(refresh, add, signout);

  // The account rows, above the entitlement card.
  const accounts = el("section", "acct__accounts");
  const accountsHead = el("div", "acct__section-head");
  const accountsTitle = el("h2", "acct__section-title", "Accounts");
  accountsTitle.id = "acct-accounts-title";
  const accountsHint = el("p", "acct__section-hint", "Switch accounts here; Kumiho keeps up to five.");
  accountsHead.append(accountsTitle, accountsHint);
  const list = el("ul", "card acct__list");
  list.setAttribute("aria-labelledby", accountsTitle.id);
  const empty = el("p", "acct__empty", "No accounts yet.");
  empty.hidden = true;
  accounts.append(accountsHead, list, empty);

  // The browser waiting card (one sign-in flow at a time): the engine URL,
  // the redirect URL pasted from the browser, Open again and Cancel.
  const waiting = el("section", "card acct__waiting");
  waiting.hidden = true;
  const waitingTitle = el("h2", "acct__waiting-title", "Waiting for browser sign-in");
  waitingTitle.id = "acct-waiting-title";
  const waitingBody = el("p", "acct__waiting-body",
    "Finish signing in in your browser, then paste the redirect URL from its address bar. Kumiho stores the sign-in tokens, never your password.");
  const waitingURL = el("p", "acct__waiting-url");
  const redirect = fieldBox("acct-browser-redirect", "Redirect URL", "text", "off");
  redirect.input.placeholder = "Paste the complete URL";
  redirect.wrap.classList.add("acct__redirect");
  const redirectError = el("p", "acct__waiting-error");
  redirectError.id = "acct-browser-error";
  redirectError.setAttribute("aria-live", "polite");
  redirectError.hidden = true;
  redirect.input.setAttribute("aria-describedby", redirectError.id);
  const waitingActions = el("div", "acct__waiting-actions");
  const openAgain = el("button", "button button--tonal acct__open", "Open again");
  openAgain.type = "button";
  const cancelBrowser = el("button", "button button--text acct__cancel-browser", "Cancel");
  cancelBrowser.type = "button";
  const browserContinue = el("button", "button button--filled acct__browser-continue", "Continue");
  browserContinue.type = "button";
  waitingActions.append(openAgain, cancelBrowser, browserContinue);
  waiting.append(waitingTitle, waitingBody, waitingURL, redirect.wrap, redirectError, waitingActions);
  waiting.setAttribute("aria-labelledby", waitingTitle.id);

  // The password form (the onboarding credential flow, inline).
  const addform = el("section", "card acct__addform");
  addform.hidden = true;
  const addformTitle = el("h2", "acct__addform-title", "Sign in with password");
  addformTitle.id = "acct-addform-title";
  const addformBody = el("p", "acct__addform-body");
  const email = fieldBox("acct-add-email", "Email", "email", "username");
  email.input.placeholder = "you@example.com";
  const password = fieldBox("acct-add-password", "Password", "password", "current-password");
  const code = fieldBox("acct-add-code", "Verification code", "text", "one-time-code");
  password.wrap.hidden = true;
  code.wrap.hidden = true;
  const formStatus = el("p", "acct__form-status");
  formStatus.setAttribute("aria-live", "polite");
  formStatus.hidden = true;
  const formActions = el("div", "acct__form-actions");
  const formCancel = el("button", "button button--text acct__form-cancel", "Cancel");
  formCancel.type = "button";
  const formSubmit = el("button", "button button--filled acct__form-submit", "Continue");
  formSubmit.type = "button";
  formActions.append(formCancel, formSubmit);
  addform.append(addformTitle, addformBody, email.wrap, password.wrap, code.wrap, formStatus, formActions);
  addform.setAttribute("aria-labelledby", addformTitle.id);

  // The entitlement card (unchanged inventory rows).
  const planTitle = el("h2", "acct__section-title", "Plan");
  planTitle.id = "acct-plan-title";
  const card = document.createElement("dl");
  card.className = "card acct__card";
  card.setAttribute("aria-labelledby", planTitle.id);
  card.hidden = true;
  const rows = new Map();
  for (const [key, label] of ROWS) {
    const dt = el("dt", null, label);
    const dd = document.createElement("dd");
    const value = el("span", "acct__value");
    dd.append(value);
    // Rows are always present in canonical order and hidden while empty.
    dt.hidden = true;
    dd.hidden = true;
    card.append(dt, dd);
    rows.set(key, { dt: dt, dd: dd, value: value });
  }

  // Rotation: a switch and the 1–50% threshold, saved immediately.
  const rotation = el("section", "card acct__rotation");
  const rotationHead = el("div", "acct__rotation-head");
  const rotationTitle = el("h2", "acct__rotation-title", "Account rotation");
  rotationTitle.id = "acct-rotation-title";
  const rotationToggle = el("button", "button button--tonal acct__toggle", "off");
  rotationToggle.type = "button";
  rotationToggle.id = "acct-rotation-toggle";
  rotationToggle.setAttribute("aria-label", "Account rotation");
  rotationHead.append(rotationTitle, rotationToggle);
  const rotationBody = el("p", "acct__rotation-body",
    "Before connecting, switch to another account when the active one has less quota left than the threshold.");
  const threshold = fieldBox("acct-rotation-threshold", "Switch below (%)", "number", "off");
  threshold.input.min = "1";
  threshold.input.max = "50";
  threshold.input.step = "1";
  threshold.input.inputMode = "numeric";
  rotation.append(rotationHead, rotationBody, threshold.wrap);
  rotation.setAttribute("aria-labelledby", rotationTitle.id);

  // The sign-out confirmation (the existing dialog).
  const dialog = el("dialog", "dialog acct__dialog");
  const title = el("h2", "dialog__title", "Sign out");
  title.id = "acct-confirm-title";
  const question = el("p", "dialog__body", "Sign out of Kumiho?");
  question.id = "acct-confirm-question";
  const detail = el("p", "dialog__body", "The stored sign-in tokens are cleared and the tunnel disconnects.");
  detail.id = "acct-confirm-detail";
  const revoke = el("p", "dialog__body", SIGNOUT_REVOKE);
  revoke.id = "acct-confirm-revoke";
  dialog.setAttribute("aria-labelledby", title.id);
  dialog.setAttribute("aria-describedby", question.id + " " + detail.id + " " + revoke.id);
  const cancel = el("button", "button button--tonal acct__cancel", "Cancel");
  cancel.type = "button";
  const confirm = el("button", "button button--danger acct__confirm", "Sign out");
  confirm.type = "button";
  const dialogActions = el("div", "dialog__actions");
  dialogActions.append(cancel, confirm);
  dialog.append(title, question, detail, revoke, dialogActions);

  // Add account: the browser choice is the default and first.
  const addDialog = el("dialog", "dialog acct__add-dialog");
  const addTitle = el("h2", "dialog__title", "Add account");
  addTitle.id = "acct-add-title";
  const addBody = el("p", "dialog__body",
    "Sign in to another Firefox account with the VPN. Browser sign-in is the default; password sign-in is also available.");
  addBody.id = "acct-add-body";
  addDialog.setAttribute("aria-labelledby", addTitle.id);
  addDialog.setAttribute("aria-describedby", addBody.id);
  const addCancel = el("button", "button button--tonal acct__add-cancel", "Cancel");
  addCancel.type = "button";
  const addPassword = el("button", "button button--tonal acct__add-password", "Use password");
  addPassword.type = "button";
  const addBrowser = el("button", "button button--filled acct__add-browser", "Continue in browser");
  addBrowser.type = "button";
  const addActions = el("div", "dialog__actions");
  addActions.append(addCancel, addPassword, addBrowser);
  addDialog.append(addTitle, addBody, addActions);

  // Rename the selected account.
  const renameDialog = el("dialog", "dialog acct__rename-dialog");
  const renameTitle = el("h2", "dialog__title", "Rename account");
  renameTitle.id = "acct-rename-title";
  const renameBody = el("p", "dialog__body", "Choose a short label for this account.");
  renameBody.id = "acct-rename-body";
  renameDialog.setAttribute("aria-labelledby", renameTitle.id);
  renameDialog.setAttribute("aria-describedby", renameBody.id);
  const renameField = fieldBox("acct-rename-label", "Label", "text", "off");
  renameField.input.maxLength = 64;
  const renameCancel = el("button", "button button--tonal acct__rename-cancel", "Cancel");
  renameCancel.type = "button";
  const renameSave = el("button", "button button--filled acct__rename-save", "Save");
  renameSave.type = "button";
  const renameActions = el("div", "dialog__actions");
  renameActions.append(renameCancel, renameSave);
  renameDialog.append(renameTitle, renameBody, renameField.wrap, renameActions);

  // Remove the selected account.
  const removeDialog = el("dialog", "dialog acct__remove-dialog");
  const removeTitle = el("h2", "dialog__title", "Remove account");
  removeTitle.id = "acct-remove-title";
  const removeQuestion = el("p", "dialog__body", "Remove this account?");
  removeQuestion.id = "acct-remove-question";
  const removeDetail = el("p", "dialog__body",
    "The stored sign-in tokens for this account are removed. Other accounts stay signed in.");
  removeDetail.id = "acct-remove-detail";
  removeDialog.setAttribute("aria-labelledby", removeTitle.id);
  removeDialog.setAttribute("aria-describedby", removeQuestion.id + " " + removeDetail.id);
  const removeCancel = el("button", "button button--tonal acct__remove-cancel", "Cancel");
  removeCancel.type = "button";
  const removeConfirm = el("button", "button button--danger acct__remove-confirm", "Remove");
  removeConfirm.type = "button";
  const removeActions = el("div", "dialog__actions");
  removeActions.append(removeCancel, removeConfirm);
  removeDialog.append(removeTitle, removeQuestion, removeDetail, removeActions);

  const status = el("p", "acct__status");
  status.setAttribute("aria-live", "polite");
  status.hidden = true;

  root.append(
    actions,
    accounts,
    waiting,
    addform,
    planTitle,
    card,
    rotation,
    dialog,
    addDialog,
    renameDialog,
    removeDialog,
    status,
  );
  host.append(root);

  return {
    root: root,
    refresh: refresh,
    add: add,
    signout: signout,
    list: list,
    empty: empty,
    card: card,
    rows: rows,
    waiting: waiting,
    waitingURL: waitingURL,
    redirect: redirect,
    redirectError: redirectError,
    openAgain: openAgain,
    cancelBrowser: cancelBrowser,
    browserContinue: browserContinue,
    addform: addform,
    addformTitle: addformTitle,
    addformBody: addformBody,
    email: email,
    password: password,
    code: code,
    formStatus: formStatus,
    formCancel: formCancel,
    formSubmit: formSubmit,
    rotation: rotation,
    rotationToggle: rotationToggle,
    threshold: threshold,
    dialog: dialog,
    cancel: cancel,
    confirm: confirm,
    revoke: revoke,
    addDialog: addDialog,
    addCancel: addCancel,
    addPassword: addPassword,
    addBrowser: addBrowser,
    renameDialog: renameDialog,
    renameField: renameField,
    renameCancel: renameCancel,
    renameSave: renameSave,
    removeDialog: removeDialog,
    removeQuestion: removeQuestion,
    removeCancel: removeCancel,
    removeConfirm: removeConfirm,
    status: status,
  };
}

export default {
  mount(host, ctx) {
    const ui = build(host);

    // Per-mount view state. The store hands parsed snapshots (a fresh object
    // graph per envelope), so identity checks use signatures, never
    // references.
    const view = {
      busy: false,
      busyMark: "",
      busyTimer: 0,
      pendingDialog: ctx.hint === "signout-confirm",
      statusText: null,
      statusTone: null,
      // Add-account flow: browser waiting or the inline password pages.
      adding: false,
      addMethod: "",
      addStep: "start",
      addEmail: "",
      addError: "",
      formError: "",
      starting: false,
      startingMark: "",
      startTimer: 0,
      cancelling: false,
      // Browser completion: the pasted redirect URL's round trip and its
      // screen-local error.
      completing: false,
      completingMark: "",
      completingTimer: 0,
      redirectError: "",
      redirectID: "",
      loginMark: null,
      formFocusKey: "",
      // Account dialogs' target ids.
      renameID: "",
      removeID: "",
      // The last rendered row signature (rebuild only on row changes).
      rowsMark: null,
      // Browser polling.
      pollTimer: 0,
      pollID: "",
      pollInFlight: false,
      openedURL: "",
    };

    let disposed = false;

    // The logout's completion evidence: the account notice (success or
    // failure) or the reloaded account snapshot. A byte-identical repeat
    // moves neither and ends at the settle timer.
    function fingerprint(snapshot) {
      return noticeMark(snapshot.account_notice) + "\u0000" + accountSignature(snapshot.account);
    }

    function endBusy() {
      view.busy = false;
      clearTimeout(view.busyTimer);
      view.busyTimer = 0;
    }

    function beginBusy() {
      view.busy = true;
      view.busyMark = fingerprint(ctx.store.get());
      clearTimeout(view.busyTimer);
      view.busyTimer = setTimeout(function () {
        view.busyTimer = 0;
        if (view.busy) {
          endBusy();
          render();
        }
      }, SETTLE_MS);
      // Disable the button in the click's own turn: the trigger must be
      // disabled before the next envelope could re-enable anything.
      render();
    }

    function endStarting() {
      view.starting = false;
      clearTimeout(view.startTimer);
      view.startTimer = 0;
    }

    function beginStarting() {
      view.starting = true;
      view.startingMark = loginMark(ctx.store.get().login);
      clearTimeout(view.startTimer);
      view.startTimer = setTimeout(function () {
        view.startTimer = 0;
        if (view.starting) {
          endStarting();
          render();
        }
      }, SETTLE_MS);
      render();
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
      ctx.send("accounts_add_status", { login_id: login.id }).then(function (err) {
        view.pollInFlight = false;
        if (err) {
          stopPolling();
          console.warn("kumiho: browser sign-in status refused: " + err);
        }
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
      view.completingMark = loginMark(ctx.store.get().login);
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
      const url = ui.redirect.input.value.trim();
      if (!url) {
        view.redirectError = "Paste the complete redirect URL from the browser.";
        render();
        ui.redirect.input.focus();
        return;
      }
      view.redirectError = "";
      beginCompleting();
      ctx.send("login_complete", { login_id: login.id, redirect_url: url }).then(function (err) {
        if (!err) return;
        endCompleting();
        view.redirectError = err;
        render();
        console.warn("kumiho: login_complete refused: " + err);
      });
    }

    function openURL(url) {
      if (!url) return;
      ctx.send("open_external", { url: url }).then(function (err) {
        if (err) console.warn("kumiho: open browser sign-in refused: " + err);
      });
    }

    // The dialog's focus return: back to the trigger when it can still take
    // focus (hidden or busy triggers hand off to Refresh, so keyboard focus
    // never lands on the body).
    function returnFocus(preferred) {
      if (preferred && !preferred.hidden && !preferred.disabled) {
        preferred.focus();
        return;
      }
      if (!ui.add.hidden && !ui.add.disabled) {
        ui.add.focus();
        return;
      }
      if (!ui.signout.hidden && !ui.signout.disabled) {
        ui.signout.focus();
        return;
      }
      if (!ui.refresh.disabled) ui.refresh.focus();
    }

    function openDialog(dialog, focus) {
      if (dialog.open) return;
      dialog.showModal();
      if (focus) focus.focus();
    }

    function closeDialog(dialog, refocus, preferred) {
      if (dialog.open) dialog.close();
      if (refocus) returnFocus(preferred);
    }

    function findAccount(snapshot, id) {
      const viewAccounts = (snapshot.accounts && snapshot.accounts.accounts) || [];
      return viewAccounts.find(function (account) { return account.id === id; }) || null;
    }

    function accountCount(snapshot) {
      return ((snapshot.accounts && snapshot.accounts.accounts) || []).length;
    }

    // --------------------------------------------------------------- rows

    function quotaLabel(account) {
      const pct = accountQuotaPercent(account);
      if (pct === null) return "Quota unknown";
      return pct + "% left";
    }

    function rowNode(account, snapshot) {
      const name = accountName(account);
      const email = account.email || "";
      const item = el("li", "acct__row");
      item.dataset.id = account.id || "";
      if (account.active) item.dataset.active = "true";
      if (account.reauth_required) item.dataset.reauth = "true";

      const identity = el("div", "acct__identity");
      const nameNode = el("span", "acct__name", name);
      nameNode.title = email || name;
      const emailNode = el("span", "acct__email", email !== name ? email : "");
      if (email === name) emailNode.hidden = true;
      const marks = el("div", "acct__marks");
      if (account.active) {
        const active = el("span", "chip acct__mark", "Active");
        active.dataset.tone = "good";
        marks.append(active);
      }
      if (account.reauth_required) {
        const reauth = el("span", "chip acct__mark", "Sign in needed");
        reauth.dataset.tone = "warn";
        marks.append(reauth);
      }
      identity.append(nameNode, emailNode, marks);

      const meta = el("div", "acct__meta");
      const method = el("span", "acct__method", accountMethodWord(account.method));
      const quota = el("span", "acct__quota", quotaLabel(account));
      meta.append(method, quota);

      const menu = el("details", "acct__menu");
      const summary = el("summary", "acct__menu-summary", "More");
      summary.setAttribute("aria-label", "Actions for " + (name || email || "account"));
      const menuList = el("div", "acct__menu-list");
      const use = el("button", "button button--text acct__menu-action", "Use");
      use.type = "button";
      use.dataset.accountAction = "use";
      use.dataset.id = account.id || "";
      use.disabled = !!account.active;
      const rename = el("button", "button button--text acct__menu-action", "Rename");
      rename.type = "button";
      rename.dataset.accountAction = "rename";
      rename.dataset.id = account.id || "";
      const reauth = el("button", "button button--text acct__menu-action", "Re-authenticate");
      reauth.type = "button";
      reauth.dataset.accountAction = "reauth";
      reauth.dataset.id = account.id || "";
      const remove = el("button", "button button--text acct__menu-action", "Remove");
      remove.type = "button";
      remove.dataset.accountAction = "remove";
      remove.dataset.id = account.id || "";
      remove.dataset.tone = "bad";
      menuList.append(use, rename, reauth, remove);
      menu.append(summary, menuList);

      item.append(identity, meta, menu);
      return item;
    }

    function rowsSignature(snapshot) {
      const accounts = snapshot.accounts || null;
      return accounts ? JSON.stringify({ rows: accounts.accounts, active: accounts.active }) : "";
    }

    function rebuildRows(snapshot) {
      const accounts = snapshot.accounts || null;
      const rows = (accounts && accounts.accounts) || [];
      if (rows.length === 0) {
        ui.list.replaceChildren();
        ui.list.hidden = true;
        ui.empty.hidden = false;
        if (!accounts) ui.empty.textContent = "Loading accounts...";
        else ui.empty.textContent = "No accounts yet.";
        return;
      }
      ui.list.hidden = false;
      ui.empty.hidden = true;
      const fragment = document.createDocumentFragment();
      for (const account of rows) fragment.append(rowNode(account, snapshot));
      ui.list.replaceChildren(fragment);
    }

    function renderAccounts(snapshot) {
      const mark = rowsSignature(snapshot);
      if (mark !== view.rowsMark) {
        view.rowsMark = mark;
        rebuildRows(snapshot);
      }
      const count = accountCount(snapshot);
      const atLimit = count >= MAX_ACCOUNTS;
      const addDisabled = atLimit || !snapshot.accounts || view.busy || view.adding || view.starting;
      if (ui.add.disabled !== addDisabled) ui.add.disabled = addDisabled;
      const addTitle = atLimit ? "Kumiho keeps up to five accounts" : "Add another account";
      if (ui.add.title !== addTitle) ui.add.title = addTitle;

      const showSignout = signedIn(snapshot.account) || count > 0;
      if (ui.signout.hidden !== !showSignout) ui.signout.hidden = !showSignout;
      if (ui.signout.disabled !== view.busy) ui.signout.disabled = view.busy;
      const label = count > 1 ? "Sign out all" : "Sign out";
      if (ui.signout.textContent !== label) ui.signout.textContent = label;
      if (ui.signout.title !== label) ui.signout.title = label;
    }

    function renderRotation(snapshot) {
      const accounts = snapshot.accounts || null;
      const enabled = !!(accounts && accounts.rotation);
      const text = enabled ? "on" : "off";
      if (ui.rotationToggle.textContent !== text) ui.rotationToggle.textContent = text;
      if (accounts) ui.rotationToggle.setAttribute("aria-pressed", enabled ? "true" : "false");
      else ui.rotationToggle.removeAttribute("aria-pressed");
      const disabled = !accounts || view.busy;
      if (ui.rotationToggle.disabled !== disabled) ui.rotationToggle.disabled = disabled;
      if (ui.threshold.input.disabled !== disabled) ui.threshold.input.disabled = disabled;
      if (accounts && document.activeElement !== ui.threshold.input) {
        const value = String(accounts.rotation_threshold || 10);
        if (ui.threshold.input.value !== value) ui.threshold.input.value = value;
        ui.threshold.input.setAttribute("aria-valuetext", value + " percent left");
      }
      ui.threshold.input.setAttribute("aria-label", "Switch accounts below " +
        ((accounts && accounts.rotation_threshold) || 10) + " percent quota left");
    }

    // ------------------------------------------------------ browser waiting

    function renderWaiting(snapshot) {
      const login = snapshot.login || null;
      const waiting = !!(login && login.step === "browser");
      if (ui.waiting.hidden !== !waiting) ui.waiting.hidden = !waiting;
      if (!waiting) {
        stopPolling();
        endCompleting();
        // A one-shot URL's code must not linger in the DOM after the flow.
        ui.redirect.input.value = "";
        ui.redirectError.textContent = "";
        ui.redirectError.hidden = true;
        view.redirectID = "";
        view.redirectError = "";
        // A completed flow's URL may be one-shot; keep a new flow's URL free
        // to open even when the daemon happens to reuse it.
        view.openedURL = "";
        return;
      }
      const url = login.url || "";
      const text = url || "Waiting for the sign-in link…";
      if (ui.waitingURL.textContent !== text) {
        ui.waitingURL.textContent = text;
        ui.waitingURL.title = text;
      }
      // A new flow starts with an empty paste field, never the previous
      // flow's one-shot code.
      if ((login.id || "") !== view.redirectID) {
        view.redirectID = login.id || "";
        ui.redirect.input.value = "";
        view.redirectError = "";
      }
      const error = view.redirectError || login.error || "";
      if (ui.redirectError.textContent !== error) ui.redirectError.textContent = error;
      ui.redirectError.hidden = !error;
      if (error) ui.redirect.input.setAttribute("aria-invalid", "true");
      else ui.redirect.input.removeAttribute("aria-invalid");
      ui.openAgain.disabled = !url;
      ui.cancelBrowser.disabled = !login.id || view.cancelling || view.completing;
      ui.cancelBrowser.textContent = view.cancelling ? "Cancelling…" : "Cancel";
      ui.redirect.input.disabled = view.completing;
      ui.browserContinue.disabled = view.completing || !login.id;
      ui.browserContinue.textContent = view.completing ? "Signing in…" : "Continue";
      if (url && view.openedURL !== url && openedBrowserURL !== url) {
        openedBrowserURL = url;
        openURL(url);
      }
      view.openedURL = url;
      if (login.id) startPolling(login.id);
    }

    // ----------------------------------------------------- password adding

    function addformBody(login) {
      if (view.addStep === "password") {
        return "Enter the password for " + (view.addEmail || login.email || "this account") + ".";
      }
      if (view.addStep === "2fa") return twoFactorBody(login);
      return "Sign in with the Firefox account password. Kumiho stores the sign-in tokens, never your password.";
    }

    function renderAddForm(snapshot) {
      const showing = view.adding && view.addMethod === "password";
      if (ui.addform.hidden !== !showing) ui.addform.hidden = !showing;
      if (!showing) {
        view.formFocusKey = "";
        return;
      }
      const login = snapshot.login || null;
      const showEmail = view.addStep === "start";
      const showPassword = view.addStep === "password";
      const showCode = view.addStep === "2fa";
      if (ui.email.wrap.hidden !== !showEmail) ui.email.wrap.hidden = !showEmail;
      if (ui.password.wrap.hidden !== !showPassword) ui.password.wrap.hidden = !showPassword;
      if (ui.code.wrap.hidden !== !showCode) ui.code.wrap.hidden = !showCode;

      const body = addformBody(login);
      if (ui.addformBody.textContent !== body) ui.addformBody.textContent = body;
      const submitLabel = showCode ? "Verify" : showPassword ? "Sign in" : "Continue";
      if (ui.formSubmit.textContent !== submitLabel) ui.formSubmit.textContent = submitLabel;

      const error = view.formError || (view.addStep !== "start" && login && login.error ? login.error : "");
      if (ui.formStatus.textContent !== error) ui.formStatus.textContent = error;
      ui.formStatus.hidden = !error;
      const invalid = !!error;
      const active = showEmail ? ui.email.input : showPassword ? ui.password.input : ui.code.input;
      for (const input of [ui.email.input, ui.password.input, ui.code.input]) {
        if (input === active && invalid) input.setAttribute("aria-invalid", "true");
        else input.removeAttribute("aria-invalid");
        const disabled = view.starting || input !== active;
        if (input.disabled !== disabled) input.disabled = disabled;
      }
      if (ui.formSubmit.disabled !== view.starting) ui.formSubmit.disabled = view.starting;
      if (ui.formCancel.disabled !== view.starting) ui.formCancel.disabled = view.starting;

      const focusKey = view.addMethod + ":" + view.addStep;
      if (focusKey !== view.formFocusKey) {
        view.formFocusKey = focusKey;
        const target = showEmail ? ui.email.input : showPassword ? ui.password.input : ui.code.input;
        queueMicrotask(function () {
          if (!disposed && view.adding && view.addMethod === "password") target.focus();
        });
      }
    }

    // ------------------------------------------------------------- status

    // The status line's priority: Signing out... → bad account/add notice →
    // browser waiting → notice → account-list notice → Loading → Not signed in
    // → Subscription failure → empty. The in-flight loads have no wire flag,
    // so the Loading lines are absence without a notice; an error notice
    // outranks them so a failed load is never masked, and the one-time
    // account-list notice (legacy-browser removal) must survive the
    // no-account/loading-empty state that follows it.
    function statusLine(snapshot) {
      const notice = snapshot.account_notice;
      const account = snapshot.account || null;
      const accounts = snapshot.accounts || null;
      const login = snapshot.login || null;
      if (view.busy) return { text: "Signing out...", tone: "" };
      if (notice && notice.tone === "bad") return { text: notice.message, tone: "bad" };
      if (view.addError) return { text: view.addError, tone: "bad" };
      if (accounts && accounts.error) return { text: accounts.error, tone: "bad" };
      if (login && login.step === "browser") {
        return { text: "Waiting for sign-in in your browser...", tone: "" };
      }
      if (notice && notice.message) {
        const tone = notice.tone === "good" || notice.tone === "warn" ? notice.tone : "";
        return { text: notice.message, tone: tone };
      }
      if (accounts && accounts.notice) return { text: accounts.notice, tone: "" };
      if (!accounts) return { text: "Loading accounts...", tone: "" };
      if (!account) return { text: "Loading account...", tone: "" };
      if (!signedIn(account)) return { text: "Not signed in", tone: "" };
      if (account.error) {
        return { text: "Subscription details unavailable: " + account.error, tone: "warn" };
      }
      return { text: "", tone: "" };
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

    // ---------------------------------------------------------- entitlement

    function renderCard(snapshot) {
      const account = snapshot.account || null;
      const show = signedIn(account);
      if (ui.card.hidden !== !show) ui.card.hidden = !show;
      if (!show) return;
      const values = {
        email: account.email || "",
        uid: account.uid || "",
        subscription: subscriptionView(account),
        quota: quotaText(account),
        bandwidth: {
          text: account.limited_bandwidth ? "Limited" : "Unlimited",
          tone: account.limited_bandwidth ? "warn" : "",
        },
        token_expiry: account.token_expires > 0 ? rfc3339(account.token_expires) : "",
      };
      for (const [key, entry] of ui.rows) {
        const item = values[key];
        const text = typeof item === "string" ? item : item.text;
        const tone = typeof item === "string" ? "" : item.tone;
        if (entry.value.textContent !== text) entry.value.textContent = text;
        if (tone) entry.value.dataset.tone = tone;
        else delete entry.value.dataset.tone;
        const rowShown = text !== "";
        if (entry.dt.hidden !== !rowShown) {
          entry.dt.hidden = !rowShown;
          entry.dd.hidden = !rowShown;
        }
      }
    }

    // ---------------------------------------------------------- dialogs

    function renderDialog(snapshot) {
      const show = signedIn(snapshot.account);
      if (view.pendingDialog && show) {
        view.pendingDialog = false;
        openDialog(ui.dialog, ui.cancel);
      }
      // The session can go anonymous underneath an open dialog (a CLI
      // sign-out): the confirmation has nothing left to confirm.
      if (ui.dialog.open && !show && !view.busy) closeDialog(ui.dialog, true, ui.signout);

      const count = accountCount(snapshot);
      const every = count > 1 ? "Sign out of every account?" : "Sign out of Kumiho?";
      if (ui.dialog.querySelector(".dialog__body").textContent !== every) {
        ui.dialog.querySelector(".dialog__body").textContent = every;
      }
      const detail = count > 1
        ? "All stored sign-in tokens are cleared and the tunnel disconnects."
        : "The stored sign-in tokens are cleared and the tunnel disconnects.";
      const detailNode = ui.dialog.querySelectorAll(".dialog__body")[1];
      if (detailNode.textContent !== detail) detailNode.textContent = detail;
      const revoke = count > 1 ? SIGNOUT_REVOKE_ALL : SIGNOUT_REVOKE;
      if (ui.revoke.textContent !== revoke) ui.revoke.textContent = revoke;
      const confirmLabel = count > 1 ? "Sign out all" : "Sign out";
      if (ui.confirm.textContent !== confirmLabel) ui.confirm.textContent = confirmLabel;
    }

    // ----------------------------------------------------------- add flow

    function resetAddForm() {
      view.adding = false;
      view.addMethod = "";
      view.addStep = "start";
      view.addEmail = "";
      view.addError = "";
      view.formError = "";
      view.formFocusKey = "";
      ui.email.input.value = "";
      ui.password.input.value = "";
      ui.code.input.value = "";
      endStarting();
    }

    function syncAddState(snapshot) {
      const login = snapshot.login || null;
      const mark = loginMark(login);
      if (mark === view.loginMark) return;
      view.loginMark = mark;
      if (view.starting && mark !== view.startingMark) endStarting();
      if (!view.adding) return;
      const step = (login && login.step) || "";
      if (step === "done") {
        if (login && login.error) {
          // A terminal done with an error (account limit or a failed save)
          // is a failure, not a silent success: keep the user's password form
          // open with the engine error, and for a browser wait close it with
          // the error on the status line so Add account is usable again.
          view.addError = login.error;
          if (view.addMethod === "password") {
            view.formError = login.error;
            endStarting();
            return;
          }
          resetAddForm();
          view.addError = login.error;
          return;
        }
        resetAddForm();
        return;
      }
      if (step === "error") {
        resetAddForm();
        view.addError = (login && login.error) || "Sign-in failed";
        return;
      }
      if (step === "cancelled") {
        resetAddForm();
        return;
      }
      if (view.addMethod !== "password") return;
      if (step === "password") {
        if (view.addStep !== "password") ui.password.input.value = "";
        view.addStep = "password";
        view.addEmail = (login && login.email) || view.addEmail;
        view.formError = "";
      } else if (step === "2fa") {
        if (view.addStep !== "2fa") ui.code.input.value = "";
        view.addStep = "2fa";
        view.addEmail = (login && login.email) || view.addEmail;
      } else if (step === "email") {
        view.addStep = "start";
        view.formError = (login && login.error) || "";
      }
    }

    function startBrowser(accountID, email) {
      if (view.adding || view.busy) return;
      view.adding = true;
      view.addMethod = "browser";
      view.addStep = "browser";
      view.addEmail = email || "";
      view.addError = "";
      view.formError = "";
      view.openedURL = "";
      const payload = { method: "browser" };
      if (accountID) payload.account_id = accountID;
      beginStarting();
      ctx.send("accounts_add_start", payload).then(function (err) {
        if (!err) return;
        endStarting();
        view.adding = false;
        view.addError = err;
        render();
        console.warn("kumiho: accounts_add_start refused: " + err);
      });
    }

    function beginPasswordAdd() {
      closeDialog(ui.addDialog, false);
      view.adding = true;
      view.addMethod = "password";
      view.addStep = "start";
      view.addEmail = "";
      view.addError = "";
      view.formError = "";
      view.openedURL = "";
      render();
      ui.email.input.focus();
    }

    function submitPasswordStep() {
      if (!view.adding || view.addMethod !== "password" || view.starting) return;
      if (view.addStep === "start") {
        const address = ui.email.input.value.trim();
        if (!address) {
          view.formError = "Enter your email.";
          render();
          ui.email.input.focus();
          return;
        }
        view.addEmail = address;
        view.addError = "";
        view.formError = "";
        beginStarting();
        ctx.send("accounts_add_start", { method: "password", email: address }).then(function (err) {
          if (!err) return;
          endStarting();
          view.formError = err;
          render();
          console.warn("kumiho: accounts_add_start refused: " + err);
        });
        return;
      }
      let action = "";
      let payload = null;
      if (view.addStep === "password") {
        action = "login_password";
        payload = { password: ui.password.input.value };
      } else if (view.addStep === "2fa") {
        action = "login_2fa";
        payload = { code: ui.code.input.value };
      }
      if (!action) return;
      view.addError = "";
      view.formError = "";
      beginStarting();
      ctx.send(action, payload).then(function (err) {
        if (!err) return;
        endStarting();
        view.formError = err;
        render();
        console.warn("kumiho: " + action + " refused: " + err);
      });
    }

    function cancelBrowserAdd() {
      const login = ctx.store.get().login || null;
      if (!login || !login.id || view.cancelling) return;
      view.cancelling = true;
      render();
      ctx.send("accounts_add_cancel", { login_id: login.id }).then(function (err) {
        view.cancelling = false;
        if (err) {
          render();
          console.warn("kumiho: accounts_add_cancel refused: " + err);
        }
      });
    }

    // ------------------------------------------------------------- events

    function onRefresh() {
      if (view.busy) return;
      ctx.send("refresh", {}).then(function (err) {
        if (err) console.warn("kumiho: account refresh refused: " + err);
      });
      ctx.send("accounts_list", {}).then(function (err) {
        if (err) console.warn("kumiho: accounts refresh refused: " + err);
      });
    }

    function onAdd() {
      const snapshot = ctx.store.get();
      if (view.adding || view.busy || accountCount(snapshot) >= MAX_ACCOUNTS) return;
      openDialog(ui.addDialog, ui.addBrowser);
    }

    function onSignout() {
      if (view.busy) return;
      openDialog(ui.dialog, ui.cancel);
    }

    function onConfirm() {
      closeDialog(ui.dialog, false);
      beginBusy();
      ctx.send("logout", {}).then(function (err) {
        if (!err) return;
        endBusy();
        render();
        console.warn("kumiho: logout refused: " + err);
      });
    }

    function onAccountAction(event) {
      const button = event.target.closest("button[data-account-action]");
      if (!button) return;
      const menu = button.closest("details");
      if (menu) menu.open = false;
      const id = button.dataset.id;
      const account = findAccount(ctx.store.get(), id);
      if (!account) return;
      switch (button.dataset.accountAction) {
        case "use":
          ctx.send("accounts_use", { id: id }).then(function (err) {
            if (err) console.warn("kumiho: accounts_use refused: " + err);
          });
          break;
        case "rename":
          view.renameID = id;
          ui.renameField.input.value = account.label || "";
          openDialog(ui.renameDialog, ui.renameField.input);
          break;
        case "reauth":
          startBrowser(id, account.email || "");
          break;
        case "remove":
          view.removeID = id;
          ui.removeQuestion.textContent = "Remove " + (accountName(account) || account.email || "this account") + "?";
          openDialog(ui.removeDialog, ui.removeCancel);
          break;
      }
    }

    function onRenameSave() {
      const id = view.renameID;
      if (!id) return;
      const label = ui.renameField.input.value.trim();
      closeDialog(ui.renameDialog, false);
      view.renameID = "";
      ctx.send("accounts_update", { id: id, label: label }).then(function (err) {
        if (err) console.warn("kumiho: accounts_update refused: " + err);
      });
    }

    function onRemoveConfirm() {
      const id = view.removeID;
      if (!id) return;
      closeDialog(ui.removeDialog, false);
      view.removeID = "";
      ctx.send("accounts_remove", { id: id }).then(function (err) {
        if (err) console.warn("kumiho: accounts_remove refused: " + err);
      });
    }

    function onRotationToggle() {
      const accounts = ctx.store.get().accounts;
      if (!accounts || view.busy) return;
      const enabled = !accounts.rotation;
      const thresholdValue = Number(ui.threshold.input.value);
      const threshold = Number.isInteger(thresholdValue) && thresholdValue >= 1 && thresholdValue <= 50
        ? thresholdValue : (accounts.rotation_threshold || 10);
      ctx.send("accounts_set_rotation", { enabled: enabled, rotation_threshold: threshold }).then(function (err) {
        if (err) console.warn("kumiho: accounts_set_rotation refused: " + err);
      });
    }

    function onThresholdChange() {
      const accounts = ctx.store.get().accounts;
      if (!accounts || view.busy) return;
      const value = Number(ui.threshold.input.value);
      if (!Number.isInteger(value) || value < 1 || value > 50) {
        ui.threshold.input.value = String(accounts.rotation_threshold || 10);
        return;
      }
      ctx.send("accounts_set_rotation", { enabled: !!accounts.rotation, rotation_threshold: value }).then(function (err) {
        if (err) console.warn("kumiho: accounts_set_rotation refused: " + err);
      });
    }

    ui.refresh.addEventListener("click", onRefresh);
    ui.add.addEventListener("click", onAdd);
    ui.signout.addEventListener("click", onSignout);
    ui.cancel.addEventListener("click", function () {
      closeDialog(ui.dialog, true, ui.signout);
    });
    ui.confirm.addEventListener("click", onConfirm);
    ui.addCancel.addEventListener("click", function () {
      closeDialog(ui.addDialog, true, ui.add);
    });
    ui.addPassword.addEventListener("click", beginPasswordAdd);
    ui.addBrowser.addEventListener("click", function () {
      closeDialog(ui.addDialog, false);
      startBrowser("");
    });
    ui.renameCancel.addEventListener("click", function () {
      closeDialog(ui.renameDialog, true, ui.add);
    });
    ui.renameSave.addEventListener("click", onRenameSave);
    ui.removeCancel.addEventListener("click", function () {
      closeDialog(ui.removeDialog, true, ui.add);
    });
    ui.removeConfirm.addEventListener("click", onRemoveConfirm);
    ui.list.addEventListener("click", onAccountAction);
    // One row menu open at a time keeps the list readable.
    ui.list.addEventListener("toggle", function (event) {
      const open = event.target;
      if (!open.open) return;
      for (const menu of ui.list.querySelectorAll("details[open]")) {
        if (menu !== open) menu.open = false;
      }
    }, true);
    ui.openAgain.addEventListener("click", function () {
      openURL((ctx.store.get().login || {}).url || "");
    });
    ui.cancelBrowser.addEventListener("click", cancelBrowserAdd);
    ui.browserContinue.addEventListener("click", submitRedirect);
    ui.redirect.input.addEventListener("keydown", function (event) {
      if (event.key !== "Enter") return;
      event.preventDefault();
      submitRedirect();
    });
    ui.formCancel.addEventListener("click", function () {
      resetAddForm();
      render();
      returnFocus(ui.add);
    });
    ui.formSubmit.addEventListener("click", submitPasswordStep);
    for (const input of [ui.email.input, ui.password.input, ui.code.input]) {
      input.addEventListener("keydown", function (event) {
        if (event.key !== "Enter") return;
        event.preventDefault();
        submitPasswordStep();
      });
    }
    ui.rotationToggle.addEventListener("click", onRotationToggle);
    ui.threshold.input.addEventListener("change", onThresholdChange);

    // Every close returns focus to the trigger; the open handlers set the
    // initial focus (Cancel for destructive dialogs, browser for Add).
    for (const [dialog, preferred] of [
      [ui.dialog, ui.signout],
      [ui.addDialog, ui.add],
      [ui.renameDialog, ui.add],
      [ui.removeDialog, ui.add],
    ]) {
      dialog.addEventListener("close", function () {
        if (disposed) return;
        returnFocus(preferred);
        render();
      });
    }

    function settleGuards(snapshot) {
      if (view.busy && fingerprint(snapshot) !== view.busyMark) endBusy();
      syncAddState(snapshot);
      if (view.starting && loginMark(snapshot.login) !== view.startingMark) endStarting();
      if (view.completing && loginMark(snapshot.login) !== view.completingMark) endCompleting();
    }

    function render() {
      const snapshot = ctx.store.get();
      settleGuards(snapshot);
      renderAccounts(snapshot);
      renderCard(snapshot);
      renderRotation(snapshot);
      renderWaiting(snapshot);
      renderAddForm(snapshot);
      renderDialog(snapshot);
      renderStatus(snapshot);
    }

    const unsubscribe = ctx.store.subscribe(render);

    // The account list is a v0.6.0 projection; ask for it explicitly on
    // mount so the rows are fresh even when the controller's start-up load
    // raced the first envelope.
    ctx.send("accounts_list", {}).then(function (err) {
      if (err) console.warn("kumiho: accounts list refused: " + err);
    });

    cleanup = function () {
      disposed = true;
      unsubscribe();
      clearTimeout(view.busyTimer);
      clearTimeout(view.startTimer);
      clearTimeout(view.completingTimer);
      stopPolling();
      if (ui.dialog.open) ui.dialog.close();
      if (ui.addDialog.open) ui.addDialog.close();
      if (ui.renameDialog.open) ui.renameDialog.close();
      if (ui.removeDialog.open) ui.removeDialog.close();
    };
  },

  unmount() {
    if (cleanup) cleanup();
    cleanup = null;
  },
};
