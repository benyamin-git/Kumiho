/* The Account screen (inventory §Account): the entitlement card (Email, UID,
 * Subscription, Quota, Bandwidth, Token expiry) with its Yes/No/Unknown and
 * Limited/Unlimited words and tones, the tonal Sign out action, its modal
 * confirmation dialog (a native <dialog>: Cancel focused by default, Esc and
 * Enter cancel, the destructive confirm in the loss colour), and the status
 * line.
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
 * Limited/Unlimited words with their tones, the status-line priority, both
 * dialog body lines ("Sign out of Kumiho?" / "The stored sign-in tokens are
 * cleared and the tunnel disconnects."), and the capture hint
 * (`#/account?state=signout-confirm`, scripts/visual/README.md) all come
 * from inventory §Account. The shell header's title row supplies the
 * `Account` heading (T2.1; screens do not repeat it).
 *
 * The router links css/screens/account.css from the registry entry.
 */

// The sign-out guard's settle (the dashboard/locations/settings T2.2
// pattern): a logout whose completion evidence repeats byte-for-byte stops
// blocking after this long.
const SETTLE_MS = 3000;

// The card's rows in the inventory's canonical order.
const ROWS = [
  ["email", "Email"],
  ["uid", "UID"],
  ["subscription", "Subscription"],
  ["quota", "Quota"],
  ["bandwidth", "Bandwidth"],
  ["token_expiry", "Token expiry"],
];

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

/** Build the screen's DOM and return its element references. */
function build(host) {
  const root = el("div", "acct");

  const refresh = el("button", "button button--tonal acct__refresh", "Refresh");
  refresh.type = "button";
  const actions = el("div", "acct__actions");
  actions.append(refresh);

  const card = document.createElement("dl");
  card.className = "card acct__card";
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

  const signout = el("button", "button button--tonal acct__signout", "Sign out");
  signout.type = "button";
  signout.hidden = true;

  // The confirmation dialog: the shared house pattern (app.css .dialog),
  // driven natively — showModal gives the modal scrim, the focus trap and
  // Esc; the destructive confirm carries the loss colour (.button--danger).
  const dialog = el("dialog", "dialog acct__dialog");
  const title = el("h2", "dialog__title", "Sign out");
  title.id = "acct-confirm-title";
  const question = el("p", "dialog__body", "Sign out of Kumiho?");
  question.id = "acct-confirm-question";
  const detail = el("p", "dialog__body", "The stored sign-in tokens are cleared and the tunnel disconnects.");
  detail.id = "acct-confirm-detail";
  dialog.setAttribute("aria-labelledby", title.id);
  dialog.setAttribute("aria-describedby", question.id + " " + detail.id);
  const cancel = el("button", "button button--tonal acct__cancel", "Cancel");
  cancel.type = "button";
  const confirm = el("button", "button button--danger acct__confirm", "Sign out");
  confirm.type = "button";
  const dialogActions = el("div", "dialog__actions");
  dialogActions.append(cancel, confirm);
  dialog.append(title, question, detail, dialogActions);

  const status = el("p", "acct__status");
  status.setAttribute("aria-live", "polite");
  status.hidden = true;

  root.append(actions, card, signout, dialog, status);
  host.append(root);

  return {
    root: root,
    refresh: refresh,
    card: card,
    rows: rows,
    signout: signout,
    dialog: dialog,
    cancel: cancel,
    confirm: confirm,
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
      // The capture hint opens the dialog as soon as a signed-in snapshot
      // loads (the state may still be in flight on a cold page).
      pendingDialog: ctx.hint === "signout-confirm",
      statusText: null,
      statusTone: null,
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

    // The dialog's focus return: back to the triggered control when it can
    // still take focus (a hidden or busy trigger hands off to Refresh, so
    // keyboard focus never lands on the body).
    function returnFocus() {
      if (!ui.signout.hidden && !ui.signout.disabled) {
        ui.signout.focus();
        return;
      }
      if (!ui.refresh.disabled) ui.refresh.focus();
    }

    function openDialog() {
      if (ui.dialog.open) return;
      ui.dialog.showModal();
      // The inventory's default: Cancel takes focus when the popup opens.
      ui.cancel.focus();
    }

    function closeDialog(refocus) {
      if (ui.dialog.open) ui.dialog.close();
      if (refocus) returnFocus();
    }

    function render() {
      const snapshot = ctx.store.get();
      if (view.busy && fingerprint(snapshot) !== view.busyMark) endBusy();
      renderCard(snapshot);
      renderDialog(snapshot);
      renderStatus(snapshot);
    }

    function renderCard(snapshot) {
      const account = snapshot.account || null;
      const show = signedIn(account);
      if (ui.card.hidden !== !show) ui.card.hidden = !show;
      if (ui.signout.hidden !== !show) ui.signout.hidden = !show;
      // While signing out the button is disabled (inventory §Account).
      if (ui.signout.disabled !== view.busy) ui.signout.disabled = view.busy;
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

    function renderDialog(snapshot) {
      const show = signedIn(snapshot.account);
      if (view.pendingDialog && show) {
        view.pendingDialog = false;
        openDialog();
      }
      // The session can go anonymous underneath an open dialog (a CLI
      // sign-out): the confirmation has nothing left to confirm.
      if (ui.dialog.open && !show && !view.busy) closeDialog(true);
    }

    // The status line, the inventory's priority: Signing out... → error →
    // notice → Loading account... → Not signed in → Subscription details
    // unavailable → empty. The in-flight load has no wire flag, so the
    // Loading line is the account's absence without a notice; an error
    // notice outranks it so a failed load is never masked.
    function statusLine(snapshot) {
      const notice = snapshot.account_notice;
      const account = snapshot.account || null;
      if (view.busy) return { text: "Signing out...", tone: "" };
      if (notice && notice.tone === "bad") return { text: notice.message, tone: "bad" };
      if (notice && notice.message) {
        const tone = notice.tone === "good" || notice.tone === "warn" ? notice.tone : "";
        return { text: notice.message, tone: tone };
      }
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

    // The account's one action (the frozen vocabulary has no account verb,
    // T1.2): `refresh` re-runs the controller's autonomous loads.
    function onRefresh() {
      ctx.send("refresh", {}).then(function (err) {
        if (err) console.warn("kumiho: account refresh refused: " + err);
      });
    }

    function onSignout() {
      if (view.busy) return;
      openDialog();
    }

    function onCancel() {
      closeDialog(true);
    }

    // Confirm: close, guard the sign-out, send the controller's logout (its
    // exact contract: no payload). The trigger turns disabled and hidden
    // focus paths fall back to Refresh (returnFocus on the close event).
    function onConfirm() {
      closeDialog(false);
      beginBusy();
      ctx.send("logout", {}).then(function (err) {
        if (!err) return;
        endBusy();
        render();
        console.warn("kumiho: logout refused: " + err);
      });
    }

    ui.refresh.addEventListener("click", onRefresh);
    ui.signout.addEventListener("click", onSignout);
    ui.cancel.addEventListener("click", onCancel);
    ui.confirm.addEventListener("click", onConfirm);
    // Esc closes the dialog natively (cancel → close); every close returns
    // focus to the trigger.
    ui.dialog.addEventListener("close", function () {
      if (disposed) return;
      returnFocus();
      render();
    });

    const unsubscribe = ctx.store.subscribe(render);

    // Teardown: the timer, the subscription and an open dialog outlive the
    // screen's nodes (the router replaces the host's children next).
    cleanup = function () {
      disposed = true;
      unsubscribe();
      clearTimeout(view.busyTimer);
      if (ui.dialog.open) ui.dialog.close();
    };
  },

  unmount() {
    if (cleanup) cleanup();
    cleanup = null;
  },
};
