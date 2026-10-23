/* The app's single state object and its pure view helpers.
 *
 * State envelopes are complete snapshots (gui.EncodeState of the controller
 * projection), so set() replaces the whole object; no merging, no framework.
 * Render functions read store.get() and subscribe() to be re-run on every
 * envelope; they guard on the fields they own (the harness pushes every
 * change, including a per-second uptime tick while connected).
 */

const listeners = new Set();
let state = {};
let received = false;

export const store = {
  /** The current state object ({} until the first envelope). */
  get() {
    return state;
  },

  /** Whether at least one state envelope has arrived. */
  hasState() {
    return received;
  },

  /** Replace the app state with one host snapshot and notify subscribers. */
  set(next) {
    if (!next || typeof next !== "object") return;
    state = next;
    received = true;
    for (const listener of listeners) listener(state);
  },

  /** Subscribe to state changes; called once immediately, then per set(). */
  subscribe(listener) {
    listeners.add(listener);
    listener(state);
    return () => listeners.delete(listener);
  }
};

/**
 * The header's status word, the exact wording the tray tooltip uses
 * (host_windows.go trayStateWord; inventory §Shell):
 *
 *   Loading... (muted)            no envelope yet
 *   Daemon unreachable (warn)     the control pipe could not be dialed
 *   <state word> (plain)          the controller's projected label
 *   Status unavailable (bad)      the first status load failed otherwise
 *
 * The wire drops `reachable` when it is false (json omitempty), so once any
 * envelope has arrived, an absent reachable means unreachable.
 *
 * Returns {word, tone} with tone in "plain" | "muted" | "warn" | "bad"
 * (consumed as data-tone by the header and, later, by status chips).
 */
export function connectionStatus(snapshot, hasState) {
  if (!hasState) return { word: "Loading...", tone: "muted" };
  if (!snapshot.reachable) return { word: "Daemon unreachable", tone: "warn" };
  if (snapshot.status) {
    return { word: snapshot.status.label || "Off", tone: "plain" };
  }
  if (snapshot.status_error) return { word: "Status unavailable", tone: "bad" };
  return { word: "Loading...", tone: "muted" };
}

/**
 * The account row's display name: the user label when one is set, else the
 * email, else the opaque id. (The daemon's `accounts_list` rows always carry
 * an email; the id is only a defensive fallback.)
 */
export function accountName(account) {
  if (!account) return "";
  const label = String(account.label || "").trim();
  if (label) return label;
  return account.email || account.id || "";
}

/** The account row's method word (`Browser`/`Password`). */
export function accountMethodWord(method) {
  switch (String(method || "").toLowerCase()) {
    case "browser":
      return "Browser";
    case "password":
      return "Password";
    default:
      return method ? String(method) : "";
  }
}

/**
 * Percent of the quota remaining, rounded to a whole percent, or null when
 * the list snapshot has no complete quota. Values are clamped to 0–100: a
 * stale daemon sample can report remaining > limit after a plan change, and
 * the row must never print 118%.
 */
export function accountQuotaPercent(account) {
  const quota = account && account.quota;
  if (!quota || typeof quota.limit !== "number" || typeof quota.remaining !== "number" || quota.limit <= 0) {
    return null;
  }
  const pct = Math.round((quota.remaining / quota.limit) * 100);
  return Math.max(0, Math.min(100, pct));
}
