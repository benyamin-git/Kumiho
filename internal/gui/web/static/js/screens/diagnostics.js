/* The Diagnostics screen (inventory §Diagnostics, D16/D17): the action row
 * (`Re-run checks`, `Copy details`, `Open log folder`, `Cleanup…`), the
 * service card (the SCM state and Start/Stop via UAC), the results list of
 * friendly check cards with status chips, and the status line.
 *
 * Everything renders from the store's state envelopes; actions go through
 * the transport and the screen re-renders from the next envelope — "" means
 * accepted, never completed (T1.7). The doctor suite runs in-process in the
 * controller (doctor_run) on the first visit only, and the previous report
 * survives later visits until `Re-run checks` (inventory §Diagnostics); the
 * first-visit decision waits for a state envelope so a page that already
 * carries a report never re-runs the suite.
 *
 * Copy details is UI-side: `navigator.clipboard.writeText` with the
 * controller-rendered payload (`doctor.details`: the app version, a blank
 * line, the full report JSON). Its success/failure notices and the cleanup
 * busy guard are screen-local; the cleanup/service outcomes arrive as the
 * controller's `diagnostics_notice`.
 *
 * The service card and the elevated actions are D17/D16: the Windows shell
 * owns the UAC calls and publishes `service` (installed/state/busy) and the
 * notices; the dev harness substitutes stub implementations. Start/Stop are
 * inert while an action is in flight (the wire's `service.busy`, plus a
 * click-to-wire guard), and the shell is the authoritative guard.
 *
 * Wording authority: the action labels and widths, the chip words (OK,
 * Warning, Failed, Skipped), the three status-line summary forms, the
 * placeholders (`Checking…`, `No checks yet.`), the copy notices and the
 * check-card anatomy (`Fix: <remediation>` only for Warning/Failed) come
 * from inventory §Diagnostics. The service card and cleanup dialog copy are
 * T3.3 rulings (D16/D17), worded around the CLI's cleanup result text.
 *
 * The router links css/screens/diagnostics.css from the registry entry.
 */

// Screen-local notice lifetime (the inventory's 10 s rule for copy results).
const COPY_NOTICE_MS = 10000;

// The cleanup busy guard's settle (the T2.2/T3.1 house pattern): a cleanup
// whose completion evidence repeats byte-for-byte stops blocking after this
// long; the shell's own in-flight guard stays authoritative.
const CLEANUP_SETTLE_MS = 3000;

// The click-to-wire guard for Start/Stop: covers the window between the
// click and the controller's `service.busy` reaching the screen.
const SERVICE_CLICK_MS = 1500;

// List scrolling: the wheel's three items per notch (inventory §Diagnostics
// "List keyboard"); smaller deltas are trackpad-scale and scroll natively.
const WHEEL_ITEMS = 3;
const WHEEL_NOTCH_MIN = 40;

// The service card's explanation, shown while a service is installed.
const SERVICE_DETAIL = "Runs the tunnel in the background, with or without the Kumiho window open.";

// unmount()'s teardown, assigned by mount (the router mounts one screen at a
// time, so one slot is enough).
let cleanup = null;

function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

/** The inventory's run summary: failed wording (bad), warning wording
 * (warn), or `All N check(s) passed` (good). */
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
      text: checksWord + ": " + failed + " failed, " + warningsWord +
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

/** One check block: title + status chip, then up to three small lines. */
function checkBlock(check) {
  const item = el("li", "diag__check");
  item.dataset.check = check.name || "";

  const head = el("div", "diag__check-head");
  head.append(el("p", "diag__check-title", check.title || check.name || ""));
  const chip = el("span", "chip diag__check-chip", check.status_word || "");
  if (check.tone) chip.dataset.tone = check.tone;
  head.append(chip);
  item.append(head);

  if (check.explanation) item.append(el("p", "diag__check-line", check.explanation));
  if (check.detail) item.append(el("p", "diag__check-line", check.detail));
  if (check.remediation && (check.status === "warn" || check.status === "fail")) {
    const fix = el("p", "diag__check-line diag__check-fix", "Fix: " + check.remediation);
    if (check.tone) fix.dataset.tone = check.tone;
    item.append(fix);
  }
  return item;
}

/** The service card's rendering model for one State.Service snapshot. */
function serviceView(service) {
  const s = service || {};
  const state = String(s.state || "").toLowerCase();
  const installed = !!s.installed;
  const busy = !!s.busy;
  const view = { chip: "Unknown", tone: "", detail: SERVICE_DETAIL, start: false, stop: false };
  switch (state) {
    case "running":
      view.chip = "Running";
      view.tone = "good";
      view.stop = true;
      break;
    case "stopped":
      view.chip = "Stopped";
      view.tone = "warn";
      view.start = true;
      break;
    case "start pending":
      view.chip = "Starting";
      break;
    case "stop pending":
      view.chip = "Stopping";
      break;
    case "paused":
      view.chip = "Paused";
      view.tone = "warn";
      view.stop = true;
      break;
    case "not installed":
      view.chip = "Not installed";
      view.tone = "warn";
      view.detail = "Install it with the Kumiho setup.";
      break;
    case "":
      break;
    default:
      view.chip = state.charAt(0).toUpperCase() + state.slice(1);
      break;
  }
  if (busy || !installed) {
    view.start = false;
    view.stop = false;
  }
  return view;
}

/** The notice identity used by the cleanup busy guard. */
function noticeMark(notice) {
  return notice && notice.message ? notice.message + "\u0000" + (notice.tone || "") : "";
}

/** Build the screen's DOM and return its element references. */
function build(host) {
  const root = el("div", "diag");

  // Action row (inventory order, then the D16 Cleanup… action).
  const rerun = el("button", "button button--tonal diag__rerun", "Re-run checks");
  rerun.type = "button";
  rerun.id = "diag-rerun";
  const copy = el("button", "button button--tonal diag__copy", "Copy details");
  copy.type = "button";
  copy.id = "diag-copy";
  const openFolder = el("button", "button button--tonal diag__open", "Open log folder");
  openFolder.type = "button";
  openFolder.id = "diag-open-folder";
  const cleanupBtn = el("button", "button button--tonal diag__cleanup", "Cleanup…");
  cleanupBtn.type = "button";
  cleanupBtn.id = "diag-cleanup";
  const actions = el("div", "diag__actions");
  actions.append(rerun, copy, openFolder, cleanupBtn);

  // The service card (D17): state chip, explanation, Start/Stop.
  const serviceCard = el("section", "card diag__service");
  serviceCard.id = "diag-service";
  serviceCard.setAttribute("aria-label", "Kumiho service");
  const serviceHead = el("div", "diag__service-head");
  serviceHead.append(el("p", "diag__service-title", "Kumiho service"));
  const serviceChip = el("span", "chip diag__service-chip", "Unknown");
  serviceChip.id = "diag-service-chip";
  serviceHead.append(serviceChip);
  const serviceDetail = el("p", "diag__service-detail", SERVICE_DETAIL);
  serviceDetail.id = "diag-service-detail";
  const serviceActions = el("div", "diag__service-actions");
  const serviceStart = el("button", "button button--tonal diag__service-start", "Start");
  serviceStart.type = "button";
  serviceStart.id = "diag-service-start";
  const serviceStop = el("button", "button button--tonal diag__service-stop", "Stop");
  serviceStop.type = "button";
  serviceStop.id = "diag-service-stop";
  serviceActions.append(serviceStart, serviceStop);
  serviceCard.append(serviceHead, serviceDetail, serviceActions);

  // The results list: a focusable, scrollable card of check blocks. The
  // keyboard scrolls item-wise (inventory "List keyboard"), so the card is
  // the focus target and each block is one <li>.
  const list = el("ul", "card diag__list");
  list.id = "diag-list";
  list.tabIndex = 0;
  list.setAttribute("aria-label", "Diagnostic results");

  // The quiet placeholder in place of the card while there is nothing to
  // show: `Checking…` while a run is in flight, `No checks yet.` before the
  // first one.
  const empty = el("p", "diag__empty", "No checks yet.");
  empty.id = "diag-empty";

  const status = el("p", "diag__status");
  status.setAttribute("aria-live", "polite");
  status.hidden = true;

  // The cleanup confirmation: the shared house dialog pattern, driven
  // natively; the confirm changes system network state, so it carries the
  // loss colour (the account sign-out pattern).
  const dialog = el("dialog", "dialog diag__dialog");
  const title = el("h2", "dialog__title", "Cleanup");
  title.id = "diag-cleanup-title";
  const question = el("p", "dialog__body", "Remove stale network state left by a crashed Kumiho run?");
  question.id = "diag-cleanup-question";
  const detail = el("p", "dialog__body",
    "Kumiho removes leftover routes, firewall rules and DNS settings. Windows will ask for administrator approval.");
  detail.id = "diag-cleanup-detail";
  dialog.setAttribute("aria-labelledby", title.id);
  dialog.setAttribute("aria-describedby", question.id + " " + detail.id);
  const cancel = el("button", "button button--tonal diag__cleanup-cancel", "Cancel");
  cancel.type = "button";
  cancel.id = "diag-cleanup-cancel";
  const confirm = el("button", "button button--danger diag__cleanup-confirm", "Cleanup");
  confirm.type = "button";
  confirm.id = "diag-cleanup-confirm";
  const dialogActions = el("div", "dialog__actions");
  dialogActions.append(cancel, confirm);
  dialog.append(title, question, detail, dialogActions);

  root.append(actions, serviceCard, list, empty, status, dialog);
  host.append(root);

  return {
    root: root,
    rerun: rerun,
    copy: copy,
    openFolder: openFolder,
    cleanupBtn: cleanupBtn,
    serviceChip: serviceChip,
    serviceDetail: serviceDetail,
    serviceStart: serviceStart,
    serviceStop: serviceStop,
    list: list,
    empty: empty,
    status: status,
    dialog: dialog,
    cancel: cancel,
    confirm: confirm,
  };
}

export default {
  mount(host, ctx) {
    const ui = build(host);

    // Per-mount view state; the doctor report itself lives in the
    // controller (it survives screen visits).
    const view = {
      renderKey: null,
      statusText: null,
      statusTone: null,
      copyNotice: "",
      copyError: "",
      copyTimer: 0,
      cleanupBusy: false,
      cleanupMark: "",
      cleanupTimer: 0,
      serviceClick: false,
      serviceTimer: 0,
      firstVisit: true,
    };

    let disposed = false;

    // ------------------------------------------------------------- doctor

    /** Whether the wire says a doctor run is in flight. */
    function doctorRunning() {
      const doctor = ctx.store.get().doctor;
      return !!(doctor && doctor.running);
    }

    function runDoctor() {
      ctx.send("doctor_run", {}).then(function (err) {
        if (err) console.warn("kumiho: doctor run refused: " + err);
      });
    }

    /**
     * The inventory's first-visit rule: run the suite once, and only when no
     * report exists yet (later visits keep the previous report until Re-run).
     * The decision waits for the first state envelope, so a reload of a page
     * that already holds a report never re-runs the suite.
     */
    function maybeFirstRun() {
      if (!view.firstVisit) return;
      if (!ctx.store.hasState()) return;
      view.firstVisit = false;
      if (ctx.store.get().doctor) return;
      runDoctor();
    }

    /** The list's render key: the running flag plus the projected checks. */
    function listKey(doctor) {
      return (doctor && doctor.running ? "1" : "0") + "\u0000" +
        JSON.stringify((doctor && doctor.checks) || []);
    }

    function showPlaceholder(text) {
      ui.list.hidden = true;
      ui.empty.hidden = false;
      if (ui.empty.textContent !== text) ui.empty.textContent = text;
    }

    function rebuildList(running, checks) {
      if (running) {
        showPlaceholder("Checking…");
        return;
      }
      if (checks.length === 0) {
        showPlaceholder("No checks yet.");
        return;
      }
      const fragment = document.createDocumentFragment();
      for (const check of checks) fragment.append(checkBlock(check));
      ui.list.replaceChildren(fragment);
      ui.empty.hidden = true;
      ui.list.hidden = false;
      ui.list.scrollTop = 0;
    }

    function renderList(snapshot) {
      const doctor = snapshot.doctor || null;
      const key = listKey(doctor);
      if (key === view.renderKey) return;
      view.renderKey = key;
      rebuildList(!!(doctor && doctor.running), (doctor && doctor.checks) || []);
    }

    // --------------------------------------------------------------- copy

    function copyText(text) {
      if (!navigator.clipboard || typeof navigator.clipboard.writeText !== "function") {
        return Promise.reject(new Error("the clipboard is not available"));
      }
      return navigator.clipboard.writeText(text).then(
        function () {},
        function (err) {
          throw new Error((err && err.message) || "the clipboard refused the text");
        }
      );
    }

    function clearCopyState() {
      view.copyNotice = "";
      view.copyError = "";
      clearTimeout(view.copyTimer);
      view.copyTimer = 0;
    }

    function onCopy() {
      const doctor = ctx.store.get().doctor;
      if (!doctor || doctor.running || !(doctor.checks || []).length) return;
      copyText(doctor.details || "").then(
        function () {
          view.copyError = "";
          view.copyNotice = "Details copied to the clipboard";
          clearTimeout(view.copyTimer);
          view.copyTimer = setTimeout(function () {
            view.copyTimer = 0;
            view.copyNotice = "";
            render();
          }, COPY_NOTICE_MS);
          render();
        },
        function (err) {
          view.copyNotice = "";
          clearTimeout(view.copyTimer);
          view.copyTimer = 0;
          view.copyError = "Could not copy the details: " + err.message;
          render();
        }
      );
    }

    function onRerun() {
      if (doctorRunning()) return;
      clearCopyState();
      runDoctor();
    }

    function onOpenFolder() {
      ctx.send("open_logs_folder", { notice: "diagnostics" }).then(function (err) {
        if (err) console.warn("kumiho: open logs folder refused: " + err);
      });
    }

    // ------------------------------------------------------------ cleanup

    function endCleanup() {
      view.cleanupBusy = false;
      clearTimeout(view.cleanupTimer);
      view.cleanupTimer = 0;
    }

    function beginCleanup() {
      view.cleanupBusy = true;
      view.cleanupMark = noticeMark(ctx.store.get().diagnostics_notice);
      clearTimeout(view.cleanupTimer);
      view.cleanupTimer = setTimeout(function () {
        view.cleanupTimer = 0;
        if (view.cleanupBusy) {
          endCleanup();
          render();
        }
      }, CLEANUP_SETTLE_MS);
      render();
    }

    function openDialog() {
      if (ui.dialog.open) return;
      ui.dialog.showModal();
      // Cancel takes focus when the popup opens (the account dialog rule).
      ui.cancel.focus();
    }

    function closeDialog(refocus) {
      if (ui.dialog.open) ui.dialog.close();
      if (refocus) returnFocus();
    }

    /** Focus returns to the trigger when it can still take focus. */
    function returnFocus() {
      if (!ui.cleanupBtn.disabled) {
        ui.cleanupBtn.focus();
        return;
      }
      if (!ui.rerun.disabled) ui.rerun.focus();
    }

    function onCleanup() {
      if (ui.cleanupBtn.disabled) return;
      openDialog();
    }

    function onCleanupConfirm() {
      closeDialog(false);
      beginCleanup();
      ctx.send("cleanup", {}).then(function (err) {
        if (!err) return;
        endCleanup();
        render();
        console.warn("kumiho: cleanup refused: " + err);
      });
    }

    // ------------------------------------------------------------ service

    function endServiceClick() {
      view.serviceClick = false;
      clearTimeout(view.serviceTimer);
      view.serviceTimer = 0;
    }

    function beginServiceClick() {
      view.serviceClick = true;
      clearTimeout(view.serviceTimer);
      view.serviceTimer = setTimeout(function () {
        view.serviceTimer = 0;
        view.serviceClick = false;
        render();
      }, SERVICE_CLICK_MS);
      render();
    }

    function onService(start) {
      if (view.serviceClick) return;
      beginServiceClick();
      ctx.send(start ? "service_start" : "service_stop", {}).then(function (err) {
        if (!err) return;
        endServiceClick();
        render();
        console.warn("kumiho: service action refused: " + err);
      });
    }

    function renderService(snapshot) {
      const v = serviceView(snapshot.service);
      if (ui.serviceChip.textContent !== v.chip) ui.serviceChip.textContent = v.chip;
      if (v.tone) ui.serviceChip.dataset.tone = v.tone;
      else delete ui.serviceChip.dataset.tone;
      if (ui.serviceDetail.textContent !== v.detail) ui.serviceDetail.textContent = v.detail;
      const startDisabled = view.serviceClick || !v.start;
      const stopDisabled = view.serviceClick || !v.stop;
      if (ui.serviceStart.disabled !== startDisabled) ui.serviceStart.disabled = startDisabled;
      if (ui.serviceStop.disabled !== stopDisabled) ui.serviceStop.disabled = stopDisabled;
    }

    // ------------------------------------------------------------- status

    /**
     * The status line, the inventory's priority: Running health checks... →
     * error → 10 s notice → run summary. The copy failure is a persistent
     * error; a bad controller notice (cleanup/service) persists until the
     * next run replaces it. The line is empty before the first run.
     */
    function statusLine(snapshot) {
      const doctor = snapshot.doctor || null;
      if (doctor && doctor.running) return { text: "Running health checks...", tone: "" };
      if (view.copyError) return { text: view.copyError, tone: "bad" };
      const notice = snapshot.diagnostics_notice;
      if (notice && notice.tone === "bad") return { text: notice.message, tone: "bad" };
      if (notice && notice.message) {
        const tone = notice.tone === "good" || notice.tone === "warn" ? notice.tone : "";
        return { text: notice.message, tone: tone };
      }
      if (view.copyNotice) return { text: view.copyNotice, tone: "good" };
      const checks = (doctor && doctor.checks) || [];
      if (checks.length > 0) return summaryLine(checks);
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

    function renderButtons(snapshot) {
      const doctor = snapshot.doctor || null;
      const running = !!(doctor && doctor.running);
      const finished = !running && !!(doctor && (doctor.checks || []).length);
      if (ui.rerun.disabled !== running) ui.rerun.disabled = running;
      if (ui.copy.disabled !== !finished) ui.copy.disabled = !finished;
      if (ui.cleanupBtn.disabled !== view.cleanupBusy) ui.cleanupBtn.disabled = view.cleanupBusy;
    }

    function render() {
      const snapshot = ctx.store.get();
      const doctor = snapshot.doctor || null;
      // A fresh run clears the stale copy result (the walk-era run reset).
      if (doctor && doctor.running && (view.copyNotice || view.copyError)) clearCopyState();
      // The cleanup guard ends as soon as the outcome notice moves; the
      // service click guard hands over to the wire's busy flag.
      if (view.cleanupBusy && noticeMark(snapshot.diagnostics_notice) !== view.cleanupMark) endCleanup();
      if (view.serviceClick && snapshot.service && snapshot.service.busy) endServiceClick();
      renderList(snapshot);
      renderService(snapshot);
      renderButtons(snapshot);
      renderStatus(snapshot);
      maybeFirstRun();
    }

    // ----------------------------------------------------- list keyboard

    /** Item scroll offsets, relative to the list's own box. */
    function itemTops() {
      return Array.from(ui.list.children).map(function (node) {
        return node.offsetTop;
      });
    }

    /** Move the view one item up/down (the inventory's Up/Down step). */
    function stepItem(delta) {
      const tops = itemTops();
      if (tops.length === 0) return;
      const current = ui.list.scrollTop;
      if (delta > 0) {
        let target = ui.list.scrollHeight;
        for (const top of tops) {
          if (top > current + 1) {
            target = top;
            break;
          }
        }
        ui.list.scrollTop = target;
      } else {
        let target = 0;
        for (let i = tops.length - 1; i >= 0; i--) {
          if (tops[i] < current - 1) {
            target = tops[i];
            break;
          }
        }
        ui.list.scrollTop = target;
      }
    }

    function onListKey(event) {
      if (event.altKey || event.ctrlKey || event.metaKey) return;
      switch (event.key) {
        case "ArrowUp":
          stepItem(-1);
          break;
        case "ArrowDown":
          stepItem(1);
          break;
        case "PageUp":
          ui.list.scrollTop -= ui.list.clientHeight;
          break;
        case "PageDown":
          ui.list.scrollTop += ui.list.clientHeight;
          break;
        case "Home":
          ui.list.scrollTop = 0;
          break;
        case "End":
          ui.list.scrollTop = ui.list.scrollHeight;
          break;
        default:
          return;
      }
      event.preventDefault();
    }

    function onListWheel(event) {
      if (Math.abs(event.deltaY) < WHEEL_NOTCH_MIN) return;
      event.preventDefault();
      const step = event.deltaY > 0 ? 1 : -1;
      for (let i = 0; i < WHEEL_ITEMS; i++) stepItem(step);
    }

    ui.rerun.addEventListener("click", onRerun);
    ui.copy.addEventListener("click", onCopy);
    ui.openFolder.addEventListener("click", onOpenFolder);
    ui.cleanupBtn.addEventListener("click", onCleanup);
    ui.cancel.addEventListener("click", function () {
      closeDialog(true);
    });
    ui.confirm.addEventListener("click", onCleanupConfirm);
    // Esc closes the dialog natively (cancel → close); every close returns
    // focus to the trigger.
    ui.dialog.addEventListener("close", function () {
      if (disposed) return;
      returnFocus();
      render();
    });
    ui.serviceStart.addEventListener("click", function () {
      onService(true);
    });
    ui.serviceStop.addEventListener("click", function () {
      onService(false);
    });
    ui.list.addEventListener("keydown", onListKey);
    ui.list.addEventListener("wheel", onListWheel, { passive: false });

    // The capture hint opens the dialog on mount (`?state=cleanup-confirm`,
    // scripts/visual/README.md).
    if (ctx.hint === "cleanup-confirm") openDialog();

    const unsubscribe = ctx.store.subscribe(render);

    // Teardown: the timers, the store subscription and an open dialog
    // outlive the screen's nodes (the router replaces the host's children
    // next).
    cleanup = function () {
      disposed = true;
      unsubscribe();
      clearTimeout(view.copyTimer);
      clearTimeout(view.cleanupTimer);
      clearTimeout(view.serviceTimer);
      if (ui.dialog.open) ui.dialog.close();
    };
  },

  unmount() {
    if (cleanup) cleanup();
    cleanup = null;
  },
};
