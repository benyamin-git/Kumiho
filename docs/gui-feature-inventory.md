# Kumiho Windows GUI — feature inventory (walk era, pre-reset)

Research-only inventory of the Windows desktop app as it exists at commit
`282d63c` (pre-reset). It describes **what the app does** as a user
experiences it: every screen, every visible element and its action, every
state, the interactions, the refresh behavior, and the exact user-visible
strings. Implementation mechanics (toolkits, window classes, painting) are
deliberately omitted. Sources: `internal/gui/app/*`, `internal/gui/vm/*`,
`internal/tray/*`, `cmd/kumiho-gui/*`, `internal/cli/gui_*.go`,
`internal/gui/prefs/*`, `internal/gui/theme/*`, `internal/gui/client/*`,
`docs/windows-guide.md`, `docs/windows-testing.md`, `docs/install.md`,
`docs/usage.md`, `docs/manual.md`, `docs/troubleshooting.md`.

Conventions in this document: quoted text in backticks is an **exact
user-visible string** (mind the `...` vs `…` differences — they are real).
"House" refers to the design system summarized later.

> **Post-rewrite note (v0.4.0).** The shipped WebView2 app follows this
> inventory as its behavioral authority with one deliberate delta: the
> Dashboard's **state chip** was dropped. The Connect/Disconnect row, the
> Connect options, the status card and the notice are unchanged; the shell
> header's status word (§Shell) is now the single state display, which also
> settles the header-tone issue in the list at the end. The **Background
> service** section now also carries the five v0.4.0 rows (TLS split, HTTP
> proxy, HTTP proxy port, Solve challenges, Keepalive profile), in the same
> order, kinds and enum cycles as the TUI. Everything else below describes
> the app as it exists at `282d63c`.

---

## Shell (always-visible frame)

**Reached:** the app starts with the shell. Window title `Kumiho`; taskbar and
tray icon is the Kumiho fox artwork.

**Layout, top to bottom:**

1. **Header (64-unit bar).**
   - Left: `Kumiho` (heading size, bold) and, next to it, the app version
     string (small, e.g. `v0.4.0` or `dev`; hidden only if empty).
   - Right-aligned: the **daemon status word**, driven by the shared status
     model on every UI tick:
     - `Loading...` until the first status snapshot arrives (neutral);
     - `Status unavailable` when the first status load failed with anything
       other than an unreachable daemon (shown in the bad/loss tone);
     - `Daemon unreachable` when the control pipe could not be dialed (warn
       tone);
     - otherwise the connection state word: `Off`, `Connected`, `Proxy only`,
       `Connecting`, `Reconnecting`, `Waiting for network`, `Not signed in`,
       `Error` (all in the normal text tone; the header does **not** tone
       these — see the Dashboard chip for toned state).
2. **A single 1-unit divider** between header and body.
3. **Body = navigation rail (left, fixed 200 units wide) + screen host
   (right).**

**Navigation rail:** six destinations, top-aligned, each a 40-unit-high,
200-wide selectable item with a decorative icon (only when a Segoe icon face
is installed) and the destination label; the label is always visible, so
selection never rests on colour alone. Items, top to bottom:

| Label | Icon shown (decorative) |
| --- | --- |
| `Dashboard` | home glyph |
| `Locations` | pin glyph |
| `Settings` | gear glyph |
| `Account` | person glyph |
| `Logs` | list/files glyph |
| `Diagnostics` | pulse/health glyph |

- **Selected item appearance:** tonal secondary-container fill plus a small
  accent bar on its left edge; hover lightens the fill; pressed darkens it;
  keyboard focus draws the accent focus ring.
- **Activation:** mouse click (release must land on the item), `Enter` or
  `Space` while focused. Accessible name is the label; the icon never joins
  it; the accessible default action reads "Show the <label> screen".
- Tab order in the window is: rail items first (top to bottom), then the
  current screen's controls in build order (title/labels are not focusable;
  buttons, inputs, lists and the consent checkbox are).

**Screen host:** exactly one page visible at a time; switching screens hides
the previous one in place. Screen margin 24 units; vertical gap between
groups 16 units, inside a group 8 units.

**Window:** default size 960×640; minimum 800×560. The window frame/title bar
follows the active house theme best-effort (dark chrome for Dark/OLED on
supporting Windows versions, otherwise stock). The last window position and
size are saved on every close and restored on launch, clamped into the
monitor work areas (if the saved rectangle intersects no attached monitor,
the default size/position is used instead).

**UI refresh:** a 250 ms pump drains daemon events (status snapshots, ping
results, live log lines) into the shared view-model and re-renders every
screen from it. There is no polling of status beyond this; uptime is
recomputed on every tick from the last snapshot, so it visibly ticks while
connected. Everything else is on demand: first show, explicit Refresh/Re-run,
or an action.

**Screens with lazy behavior on show:** Locations loads the server list on
first view; Settings fetches its snapshot on first view; Account fetches on
first view; Logs seeds from a snapshot and subscribes on every visit and
unsubscribes on leave; Diagnostics runs its check suite on its first visit
only. Re-visiting a screen never re-fetches Locations/Settings/Account/
Diagnostics unless you press the screen's Refresh / Re-run (Logs does
re-snapshot on every visit).

---

## Dashboard

**Reached:** the initial page after onboarding (or on every launch once
onboarding is done); rail item `Dashboard`.

**Visible elements, top to bottom:**

1. **Title row:** heading `Dashboard` and, next to it, the **state chip**
   (170×28 rounded chip, container fill + outline, word always present so
   state never rests on colour alone). Chip words/tones:
   - Before the first status, or whenever the daemon is unreachable: the same
     word as the header (`Loading...` neutral, `Status unavailable` bad,
     `Daemon unreachable` warn) — chip and header can never disagree.
   - After a status: `Connected` (good), `Off` (neutral), `Error` (bad),
     and everything else — `Proxy only`, `Connecting`, `Reconnecting`,
     `Waiting for network`, `Not signed in` — in the warn tone.
2. **Action row:** primary button `Connect` / `Disconnect` (200 wide) and
   tonal `Refresh` (110 wide).
   - The primary button's label comes from the state: `Disconnect` while the
     state is `Connected`, `Proxy only`, `Connecting`, `Reconnecting` or
     `Waiting for network`; `Connect` otherwise (including `Off`, `Error`,
     `Not signed in`, and before the first status).
   - While a connect or disconnect is in flight the primary button is
     disabled (double-clicks cannot double-submit); `Refresh` stays enabled.
   - `Connect` runs the daemon's connect (up to 90 s) then refreshes; on
     success the notice reads `Connected`.
   - `Disconnect` runs the daemon's disconnect (up to 90 s) then refreshes;
     on success the notice reads `Disconnected`.
   - `Refresh` reloads the status snapshot (up to 15 s) off-thread; failures
     surface as the notice.
3. **Status card** (label column 140 units), one row per field. Rows are
   always present in this canonical order and hidden while the value is
   empty:
   - `Uptime` — live duration since connect (e.g. `1m23s`), only while
     connected.
   - `Location` — `CC` or `CC (server)`.
   - `Exit IP` — `IP` or `IP (country)`.
   - `Rates` — `up 1.5 MiB/s / down 3.0 MiB/s` (empty at zero rates).
   - `Totals` — `up 12.0 MiB / down 80.5 MiB`.
   - `Kill switch` — `On` (good tone) or `Off` (warn tone; off is a
     security downgrade worth noticing). Hidden until the first status.
   - `Autoconnect` — `On` / `Off`. Hidden until the first status.
   - `Token expiry` — RFC3339 UTC, e.g. `2026-11-01T12:00:00Z`.
   - `Pass expiry` — same format.
   - `Quota` — `X remaining of Y` (binary units, e.g. `4.2 GiB remaining of
     50.0 GiB`); `X remaining` if only remaining is known; `Y` if only the
     limit is known.
   - Below the Quota row, a slim (**8-unit**) rounded quota bar, filled
     proportionally to `remaining / limit` (fill = remaining, clamped 0..1),
     visible only when both numbers are known and the limit is positive.
   - `Quota resets` — date `YYYY-MM-DD` (quota reset date).
4. **Notice row** (bottom): a small single-line notice and, right-aligned,
   a tonal `Retry` button (110 wide) shown only in a specific case.
   - The notice shows the view-model's last action message: success
     (`Connected`, `Disconnected`, `Location saved`, `Kill switch on` …) or
     failure (`could not connect: <err>`, `could not refresh the status:
     <err>`, `could not save the location: <err>`, `sign-in failed: <err>`,
     …). It has **no expiry** — it persists until another action replaces
     it.
   - `Retry` (same action as Refresh) appears only when the first status
     never arrived, the notice is non-empty, and the daemon is **not**
     unreachable (the unreachable case is handled by the Set up Kumiho gate).

**States seen by the user:** loading (`Loading...` chip, empty card);
connected (chip `Connected`, filled rows, ticking uptime); transitional
(`Connecting` / `Reconnecting` / `Waiting for network` chips with the primary
button reading `Disconnect`); proxy-only (chip `Proxy only`, button
`Disconnect`); off (`Off`); error (`Error` chip in bad tone); status-load
failure (`Status unavailable` + notice + Retry); daemon unreachable (the
shell switches to the Set up Kumiho page).

**Interactions:** buttons are tab stops, activate with `Enter`/`Space` or
click; disabled buttons leave the tab order. No confirmations on this screen.

**Data sources:** everything on the card comes from the daemon status
snapshot/events. Nothing here is a local preference.

---

## Locations

**Reached:** rail item `Locations`. Loads the country → city → server tree on
first view (30 s timeout).

**Visible elements, top to bottom:**

1. **Title row:** heading `Locations`.
2. **Action row:** `Refresh` (tonal, 110) and `Test speed` (tonal, 140).
3. **Filter row:** label `Filter` and a single-line input with the cue
   placeholder `Country or city` (260 wide, house control height).
   Typing filters immediately (case-insensitive); the country names and
   codes, city names and codes are matched as substrings. While a filter is
   active every surviving country is auto-expanded so matching cities are
   visible. Clearing the box restores the user's earlier expansion state.
4. **Tree list** (card, minimum 200 units high, 36-unit rows; indentation by
   level: country 8, city 28, server 48 units):
   - Country row text: `Name (CC) · n cities` (`1 city` for one), preceded by
     an expand/collapse caret (▸ collapsed, ▾ expanded; drawn, not a glyph
     from the label font). **REC first** — the recommended random entry sorts
     first as served by the daemon; its row is whatever the live data spells
     (e.g. `Recomended Location (REC) · 1 city`).
   - City row text: `Name (CC)`, with its own caret.
   - Server row text: `host:port`, plus ` · 42 ms` or ` · timeout` when a
     speed test measured it, plus ` · selected` on the persisted selection.
   - Quarantined servers and servers without a connect target are never
     listed.
   - On load, the tree auto-reveals the persisted selection (expands its
     country and city).
   - **Appearance/state:** the current row is highlighted with a rounded
     container-step fill; the hovered row uses a lighter step; the keyboard
     focus draws a ring around the whole list. While the list is empty:
     either a centred quiet line `Loading locations…` (first fetch in
     flight) or `No locations match the filter.` (settled empty set), in
     place of the card.
5. **Status line** (bottom, single small line, tone-coloured), priority
   order:
   - `Loading locations...` (load in flight);
   - the raw load error (bad tone);
   - `Testing speed...` (ping in flight);
   - `Speed test failed: <err>` (bad tone, stays until the next run);
   - `No locations match the filter` (loaded, filter matched nothing);
   - `No locations loaded` (loaded, no data at all);
   - the view-model's last notice (e.g. `Location saved`, or
     `could not save the location: <err>`) — persists until replaced.

**Interactions:**

- **Click a country/city row** (press+release on the same row): expand or
  collapse. **Click a server row**: persist that selection via the daemon
  (`Location saved` notice; does **not** connect). Selection persists across
  restarts on the daemon side and the next connect uses it.
- **Keyboard:** `Up`/`Down` move the current row; `PageUp`/`PageDown` move by
  a viewport; `Home`/`End` jump to first/last; `Enter` or `Space` activates
  the current row (same as click). Any focus movement scrolls the current row
  into view.
- **Mouse wheel:** scrolls three rows per notch.
- **Refresh:** asks the daemon to re-download the server list (30 s
  timeout); a refresh keeps the stale rows visible while it runs, and a
  failed refresh keeps the rows and reports the error in the status line.
- **Test speed:** probes up to the first 16 visible server rows (filtered and
  expanded), one run at a time — while it runs the button is disabled and a
  re-click is ignored; results stream in individually as `42 ms` / `timeout`
  labels (10 s request timeout).

**Data sources:** tree/countries/selection from the daemon; RTTs from
streamed ping events; filter text and expansion state are session-local (not
persisted); the status notice comes from the shared view-model.

---

## Settings

**Reached:** rail item `Settings`. Fetches the daemon settings snapshot on
first view (15 s timeout). The list is divided into two sections with heading
rows (heading size, bold):

### `Kumiho app` (local GUI rows, from `gui.toml`; never overridden/reset)

| Row label (tooltip) | Control |
| --- | --- |
| `Theme` (`Kumiho's color theme; changes immediately`) | three choice buttons: `Light` `Dark` `OLED`; current one filled + ringed |
| `Accent` (`Kumiho's accent color; changes immediately`) | six choice buttons: `Blue` `Teal` `Green` `Orange` `Rose` `Violet`; current one filled + ringed |
| `Start at login` (`Start Kumiho in the tray when you sign in to Windows`) | toggle button showing `on`/`off`; click flips |

- Clicking a Theme or Accent option saves `gui.toml`, **closes and rebuilds
  the window with the new palette immediately** (the app returns to the same
  screen; connected state and daemon data survive because they come from the
  daemon), and the new choice is ringed. There is no cycling and no restart.
- Clicking `Start at login` writes/removes the Windows login entry
  immediately; success shows `Start at login saved` for 10 s; failure shows
  `could not change the login setting: <err>`.
- Choice buttons expose accessible names `<Row>: <Option>` plus
  `, selected` on the current one; toggles expose `<Row>: <value>`.

### `Background service` (daemon rows, from the daemon)

Order and controls (the row tooltips are tabulated after the list):

1. `Kill switch` — on/off toggle. Flip → applies immediately.
2. `Autoconnect` — on/off toggle. Flip → saved daemon-side; takes effect on
   the next daemon start / autoconnect decision (not applied immediately).
3. `Log level` — cycling button showing the current value; click advances
   `debug → info → warn → error → debug`; applies immediately.
4. `Redact targets` — on/off toggle; applies immediately.
5. `Exit check` — on/off toggle; next connect.
6. `DoH provider` — cycling button; `automatic → cloudflare → google →
   quad9 → off`; next connect.
7. `DNS upstream` — text input (cue `(provider default)`) + `Apply`; Enter in
   the field also applies; next connect.
8. `Edge address` — text input (cue `(resolve)`) + `Apply`; next connect.
9. `Upstream proxy` — text input (cue `(direct)`) + `Apply`; next connect.
10. `Exclude CIDRs` — text input (cue `(none)`, comma-separated) + `Apply`;
    next connect.
11. `Allow LAN` — on/off toggle; next connect.
12. `MTU` — number input + `Apply`; next connect.
13. `SOCKS port` — number input + `Apply`; next connect.
14. `Quota poll` — number input + `Apply`; next connect.
15. `TLS split` — cycling button; `off → first-byte → sni → off`; next
    connect.
16. `HTTP proxy` — on/off toggle; next connect.
17. `HTTP proxy port` — number input + `Apply`; next connect.
18. `Solve challenges` — on/off toggle; applies immediately.
19. `Keepalive profile` — cycling button; `standard → slow → standard`; next
    connect.

Tooltips (every row's hover text, verbatim; each states the row's effect and
when it applies):

| Row | Tooltip |
| --- | --- |
| `Kill switch` | `Block all tunnel traffic while the tunnel is down; applies immediately` |
| `Autoconnect` | `Connect automatically when the daemon starts` |
| `Log level` | `Minimum level recorded into the log ring; applies immediately` |
| `Redact targets` | `Mask hostnames and IPs in log messages; applies immediately` |
| `Exit check` | `Probe the public exit IP after connecting; next connect` |
| `DoH provider` | `In-tunnel DNS-over-HTTPS resolver; next connect` |
| `DNS upstream` | `Resolver IP for DoH; empty = provider default; next connect` |
| `Edge address` | `Pin the Fastly edge IP; empty = resolve normally; next connect` |
| `Upstream proxy` | `Chain the edge connection through a proxy URL; empty = direct; next connect` |
| `Exclude CIDRs` | `Comma-separated ranges that bypass the tunnel; next connect` |
| `Allow LAN` | `Keep the local subnet outside the tunnel; next connect` |
| `MTU` | `Tun device MTU; next connect` |
| `SOCKS port` | `Loopback SOCKS5 listener; next connect` |
| `Quota poll` | `Minutes between quota refreshes while connected; next connect` |
| `TLS split` | `Split the TLS ClientHello across writes; next connect` |
| `HTTP proxy` | `Loopback HTTP/CONNECT proxy through the tunnel; next connect` |
| `HTTP proxy port` | `Loopback HTTP proxy listener; next connect` |
| `Solve challenges` | `Solve FxA and edge challenges automatically; applies immediately` |
| `Keepalive profile` | `Keepalive ping timeout profile; next connect` |

Details of the daemon rows:

- A daemon row whose key is reported as overridden gains a small
  `overridden` mark and a tonal `Reset` button (80 wide). `Reset` drops the
  override so the administrator value applies again; the notice reads
  `<Row> reset to the configured value`. The snapshot exposes no defaults, so
  no value is guessed.
- Every daemon change runs at most one at a time (buttons are ignored while
  one is in flight), takes up to 15 s, then refreshes the status and reloads
  the row list so values/marks stay truthful. Success notice: `<Row> saved`
  (10 s).
- Failures appear on the status line: `could not change the setting: <err>`;
  load failures: `could not load the settings: <err>` (already-loaded rows
  stay visible).
- Text/number rows validate only by daemon response: e.g. a non-numeric
  `SOCKS port` shows `could not change the setting: ...` and `Refresh`
  restores the stored value.

**Other elements:** a `Refresh` button (tonal, 110) in the action row under
the title (`Settings`); a bottom status line with this priority:
`Applying...` → `Checking for updates...` → `Loading settings...` (only when
there are no settings and no error) → error → 10 s notice → empty.

**Rows pane states:** hidden entirely until the first successful snapshot;
after that it is scrollable. Loading/failure shows only the status line.

**Scrolling (deliberate, no scrollbar):** no scrollbar is drawn in any theme.
The mouse wheel over the rows or the gaps moves the list **three rows per
notch** and stops at both ends; `Tab` moves focus through every row and
brings the focused row into view, so every row stays reachable in a short
window.

**Keyboard:** `Tab`/`Shift+Tab` through toggle/choice/edit/Apply/Reset
controls; `Enter`/`Space` activate buttons; `Enter` in an edit field applies.

**Data sources:** `Kumiho app` rows from `gui.toml`; `Background service`
rows from the daemon snapshot (`kill_switch`, `autoconnect`, `log_level`,
`redact_targets`, `exit_check`, `doh_provider`, `dns_upstream`,
`edge_address`, `upstream_proxy`, `exclude_cidrs`, `allow_lan`, `mtu`,
`socks_port`, `quota_poll_minutes`, `tls_split`, `http_proxy`,
`http_proxy_port`, `solve_challenges`, `keepalive_profile`).

---

## Account

**Reached:** rail item `Account`. Fetches the account snapshot on first view
(35 s timeout; the daemon's online entitlement lookup may retry a token on
the way).

**Visible elements:**

1. Title row: heading `Account`.
2. Action row: tonal `Refresh`, filled `Add account`, and the session-wide
   tonal `Sign out` (shown once a local session or a stored account exists).
3. **Accounts** section (hidden/empty until the snapshot loads): heading
   `Accounts`, hint `Switch accounts here; Kumiho keeps up to five.`, and one
   row per account. A row shows label/email, method, active marker, quota
   percent and `re-auth required` when applicable. The row overflow menu
   offers `Use`, `Rename`, `Re-authenticate` (browser) and `Remove`.
4. **Browser waiting card** (one flow at a time): title `Waiting for browser
   sign-in`, body `Finish signing in in your browser, then paste the redirect
   URL from its address bar. Kumiho stores the sign-in tokens, never your
   password.`, the URL, **Open again**, a labelled **Redirect URL** field,
   **Continue** and **Cancel**. The URL is opened once automatically and
   polled every second; the card stays until the pasted URL completes the flow
   or it is cancelled.
5. **Add account dialog**: title `Add account`, body explaining browser is
   the default, buttons `Cancel`, `Use password` and filled **Continue in
   browser**. **Use password** reveals the onboarding-style email → password
   → 2FA form inline.
6. **Rename dialog**: title `Rename account`, body `Choose a short label for
   this account.`, label field, `Cancel` and `Save`.
7. **Remove dialog**: title `Remove account`, body `Remove this account?` /
   `The stored sign-in tokens for this account are removed. Other accounts
   stay signed in.` If it is the last account, the daemon clears the session
   and disconnects; removing the active account of several selects the most
   recently used remaining one (shown by the next snapshot).
8. **Account rotation card**: heading `Account rotation`, on/off switch,
   body `Before connecting, switch to another account when the active one
   has less quota left than the threshold.`, and `Switch below (%)` (1–50,
   default 10). Saved immediately; disabled while the snapshot is loading or
   another account action is in flight.
9. **Card** (hidden until a successful load confirms a signed-in session),
   rows in order (label column 140):
   - `Email` — account email.
   - `UID` — Firefox Account UID from Guardian.
   - `Subscription` — `Yes` (good), `No`, or `Unknown` (warn tone when the
     online entitlement lookup failed).
   - `Quota` — `X remaining of Y`, or `X remaining`, or `Y per period`.
   - `Bandwidth` — `Limited` (warn tone) or `Unlimited`.
   - `Token expiry` — RFC3339 UTC.
   Rows with empty values are hidden; an anonymous session shows no card.
10. **`Sign out`** (tonal) clears **every** account's stored tokens and
    disconnects after confirmation (15 s timeout). While signing out the
    button is disabled. Per-account `Remove` is a different action and leaves
    the other sessions intact.
11. **Status line**, priority: `Signing out...` (neutral) → `Loading
    account...` → error (`could not load the account: <err>` or `could not
    sign out: <err>`, bad tone) → 10 s notice (`Signed out`, good tone) →
    `Loading account...` (never loaded) → `Not signed in` → `Subscription
    details unavailable: <err>` (warn; local rows stay valid) → empty.

**Interactions:** `Refresh`/`Retry` re-fetches; `Sign out` confirms first.
A successful sign-out reloads the snapshot (the card disappears and the
status shows `Not signed in` when the sign-out notice expires or is
replaced).

**Confirmation popup (shared house pattern; also used by onboarding):**
title `Sign out`, heading `Sign out`, body text:

> `Sign out of Kumiho?`
>
> `The stored sign-in tokens are cleared and the tunnel disconnects.`
>
> `Kumiho will also try to revoke its Mozilla sign-in; local sign-out completes offline.`

For more than one account the question reads `Sign out of every account?`, the
detail reads `All stored sign-in tokens are cleared and the tunnel disconnects.`
and the disclosure uses `its Mozilla sign-ins`. Buttons `Cancel` (focused by
default) and `Sign out` (or `Sign out all`). **Esc and Enter both
cancel** (Enter is treated as the default action = Cancel); Space activates
the focused button. The popup is modal (the main window is blocked while it
is open); it is centred over the window, clamped to the monitor, and rounded
on Windows 11 (square before).

**Data sources:** the daemon's `accounts_list` projection (accounts, labels,
rotation, threshold, last method and notice) plus local session facts and the
best-effort Guardian entitlement; nothing persisted locally.

---

## Logs

**Reached:** rail item `Logs`. On **every visit** the screen seeds from the
daemon's retained log ring (15 s snapshot) and subscribes live at the
selected level (10 s); on leaving it unsubscribes (5 s). The buffer is
bounded (2000 entries, screen and view-model alike).

**Visible elements, top to bottom:**

1. Title row: heading `Logs`.
2. Action row: `Level: info` (tonal, 110; cycles
   `debug → info → warn → error → debug`), `Follow: on` (tonal, 110; toggles),
   `Refresh` (tonal, 110).
3. Search row: label `Search`, input (220 wide, cue `Tag or message`,
   accessible name `Search the logs`), and right-aligned `Clear` (80),
   `Export` (80), `Open log folder` (140).
4. **Log list** (card, 24-unit rows — denser than the house table row;
   newest at the bottom). Each line:
   `HH:MM:SS LVL [tag] message` (local wall-clock time, 3-letter level tag,
   tag, message), tinted by level (Warn warning colour, Error loss colour,
   Debug outline, else normal). The level tag is always in the line, so
   severity never rests on colour alone. Empty stream → centred quiet line
   `No log entries yet.` in place of the card; a filter that hides everything
   inside a non-empty stream → `No entries match the filter` as the first
   line of the card.
5. **Status line**, priority: `Loading logs...` → error (`could not load the
   logs: <err>`, `could not subscribe to the logs: <err>`, `could not clear
   the logs: <err>`) → 10 s notice (`Logs cleared`, `Exported to <path>`,
   `Opened <dir>`, `Nothing to export`) → count text:
   - `No log entries` (buffer empty),
   - `No entries match the filter`,
   - `N of M entries` (filter active),
   - `1 entry`, `N entries`.

**Interactions:**

- **Level button:** advances the local minimum level, resets scroll to the
  tail, re-snapshots from the ring at the new level, and resubscribes at it.
- **Follow button:** toggles automatic tailing (`Follow: on` / `Follow:
  off`). Turning it on jumps to the tail.
- **Search:** filters case-insensitively on tag **or** message as you type;
  scroll resets to the tail.
- **List keyboard:** `Up` scrolls toward older lines, `Down` toward newer
  (any scroll away from the tail flips Follow off; scrolling back to the
  tail flips it on automatically, and the Follow button word follows);
  `PageUp`/`PageDown` by a page; `Home` jumps to the oldest line, `End` to
  the newest.
- **Mouse wheel:** three lines per notch.
- **Clear:** clears the daemon's log ring **and** the local list (5 s
  timeout); notice `Logs cleared`; the list empties. No confirmation.
- **Export:** writes the **currently visible (level+search filtered)**
  entries, one per line, to `%LOCALAPPDATA%\kumiho\logs\kumiho-logs-
  <YYYYMMDD-HHMMSS>.log` (0600); notice `Exported to <full path>`. With
  nothing visible: notice `Nothing to export`.
- **Open log folder:** creates the folder if needed and opens
  `%LOCALAPPDATA%\kumiho\logs` in File Explorer; notice `Opened <dir>`.
  (The daemon's own `%ProgramData%\kumiho\logs` is admin-only, so the
  per-user export folder is the open target.)

**Live behavior:** while the screen is visible, new entries appear at the
tail if Follow is on; entries stream from the daemon subscription and are
merged/deduplicated with the snapshot and the shared view-model buffer.

**Data sources:** entries from the daemon; level/search/follow/scroll are
session-local view state (defaults each launch: level `info`, follow on,
empty search — not persisted).

---

## Diagnostics

**Reached:** rail item `Diagnostics`, or the tray menu's `Diagnostics` (which
restores the window and switches to this screen). Runs the same check suite
as `kumiho doctor` on **first visit only** (the doctor run happens off the
UI thread); later visits keep the previous report until `Re-run checks`.

**Visible elements, top to bottom:**

1. Title row: heading `Diagnostics`.
2. Action row: `Re-run checks` (tonal, 140), `Copy details` (tonal, 110),
   `Open log folder` (tonal, 140), left-aligned in that order.
3. **Results list** (card; scrollable; each check is a block):
   - First line: friendly check title (body size) + a status chip on the
     right (92×20 rounded, word always present): `OK` (good), `Warning`
     (warn), `Failed` (bad), `Skipped` (neutral); unmapped checks show their
     raw name as title and an uppercased status.
   - Up to three small lines under it: the one-sentence explanation, the
     check's own result detail, and `Fix: <remediation>` (only when the
     status is Warning or Failed).
   - Placeholders while there is nothing to show: `Checking…` (run in
     flight) and `No checks yet.` (before the first run).
   - Friendly titles/explanations include: `Platform`, `Local proxy port`,
     `System clock`, `Mozilla servers`, `Kumiho service`, `Tunnel driver`,
     `Windows firewall service`, `Service connection`, `Tunnel adapter`,
     `Tunnel DNS`, `Internet route`, `Administrator rights` (and the Linux
     checks, which never appear on Windows).
4. **Status line**, priority: `Running health checks...` → error → 10 s
   notice → run summary (warn/bad-tone summary for failures):
   - `N check(s): F failed, N warning(s) — fix the failed checks before
     connecting` (bad tone),
   - `N check(s): K OK, N warning(s), S skipped` (warn tone),
   - `All N check(s) passed` (good tone).
   (The GUI words the summary differently from the CLI; only the counts
   must match.)

**Interactions:**

- `Re-run checks` starts a fresh run (button inert while one runs; a second
  click/no-op never interrupts). During the run the list clears and shows
  `Checking…`.
- `Copy details` (enabled only after a finished run) puts the app version
  plus the full report JSON on the clipboard:
  `Kumiho <version> diagnostics` then blank line then the pretty-printed
  report JSON and a trailing newline; notice `Details copied to the
  clipboard`. Failure: `Could not copy the details: <err>`.
- `Open log folder` behaves and notices exactly as on Logs.
- **List keyboard:** `Up`/`Down` one item, `PageUp`/`PageDown` a page,
  `Home` first, `End` last; wheel three items per notch. No selection or
  activation.

---

## Set up Kumiho (hidden gate page)

**Reached automatically (never a rail item):** whenever the view-model
reports the daemon **unreachable** (a dial failure — e.g. the service is
stopped), the shell switches to this page and remembers the screen you were
on; as soon as the daemon answers again it switches back to that remembered
screen.

**Visible elements:**

1. Title row: heading `Set up Kumiho`.
2. Card body (exact text):

   > Kumiho is not connected to its background service. The Kumiho setup
   > installs and starts it. Windows will ask for administrator approval; if
   > it shows a SmartScreen warning, choose "More info", then "Run anyway".
   > When the setup finishes, choose Retry.

3. Buttons: primary `Set up Kumiho` (200 wide) and tonal `Retry` (110).
4. Status line, priority: `Trying to reach the daemon...` → error →
   10 s notice → empty.

**Interactions:**

- `Set up Kumiho` locates `install.cmd` (preferred) or `install.ps1` **beside
  the running app** and launches it with the app's own folder as the working
  directory. `install.cmd` is started non-elevated (it elevates itself and
  captures the invoking user); if that launch fails it falls back to an
  elevated launch. `install.ps1` is always launched elevated. Success notice:
  `Setup started. Finish it, then choose Retry.`
  Error cases (status line): `Could not locate the Kumiho app to find the
  setup beside it.`, `Could not find install.cmd or install.ps1 next to the
  Kumiho app. Extract the full Kumiho download into one folder, then try
  again.`, `Could not start the setup: <err>`.
- `Retry` re-dials the daemon (15 s; the client retries the dial internally)
  while the button is disabled; on failure the status reads `The daemon is
  still unreachable. Finish the Kumiho setup, then choose Retry.`; on success
  the gate clears to the remembered screen.

---

## Onboarding wizard (hidden first-run page)

**Reached:** the initial page on every launch while `onboarded` is false in
`gui.toml` (first run). The wizard is not a rail destination, has no skip,
and closing the window mid-wizard and relaunching starts it again; the step
and the consent checkbox are never persisted. See the dedicated
[Onboarding wizard](#onboarding-wizard-detail) section below.

---

# Onboarding wizard (detail)

The wizard is a page with a heading (`Welcome to Kumiho` initially, then the
page title), a body text or step-specific controls inside a card, a bottom
status line, and the navigation buttons `Back` (tonal, 110; hidden on the
first and last pages) and a primary action whose label changes per page.

Steps (exact strings):

### 1. `Welcome to Kumiho` (consent, required)

- Body: scrolling read-only consent text behind a thin outline frame. The
  full text matters for parity (it is the D16 consent gate):

  > Kumiho is an unofficial desktop client for the VPN proxy included with a
  > Firefox account. Please read this before you continue.
  >
  > What Kumiho does
  >
  > - It signs in with your Firefox account and stores the resulting sign-in
  >   tokens on this PC. It never stores your password.
  > - It carries your traffic through Mozilla's servers using an HTTP/2
  >   tunnel.
  > - The tunnel carries TCP and DNS only. Non-DNS UDP traffic (some games,
  >   video calls, and QUIC/HTTP3) is blocked on purpose, so apps either fall
  >   back to TCP or stop working.
  >
  > What to keep in mind
  >
  > - Kumiho is not affiliated with or endorsed by Mozilla. Using an
  >   unofficial client is a gray area under Mozilla's terms of service.
  > - While the tunnel is up, the kill switch can block traffic that tries to
  >   leave outside it.
  > - Kumiho is provided as is; review the code before trusting it with your
  >   account.
  >
  > Tick "I understand" below to unlock Continue.

  *Note (v0.4.0): the consent text is verbatim from `onboarding.js`. The
  tunnel's DNS support includes the local UDP relay, while non-DNS UDP
  remains blocked.*

- `I understand` checkbox (house-drawn box + label; Space toggles while
  focused; clicking the box or the label toggles; the label is the
  accessible name; checked state is exposed to accessibility).
- **Validation:** `Continue` is disabled until the box is ticked; unticking
  disables it again. The gate is enforced by the model too.
- Action: `Continue`.

### 2. `Sign in` (browser or email → password → 2FA)

The step opens on the **email page** with the last successful method
preselected (browser on first run). Body: `Sign in with the Firefox account
that has the VPN. Continue in your browser (recommended), or use your account
password.` Field label `Email`, cue `you@example.com`; a tonal method button
reads `Use password` or `Continue in browser` and flips the choice. The
browser is the primary/`Continue` action; the password form is the secondary
path. `Back` returns to consent.

- **Browser waiting card.** Title/body describe completing sign-in in the
  browser, then pasting the redirect URL from its address bar; the card shows
  the engine URL, **Open again**, a **Redirect URL** field, **Continue** and
  **Cancel**. The GUI opens each unique URL once through `open_external` and
  polls `login_status` every second. Cancelling clears the waiter; the card
  does not complete on its own.
- **Password page.** Body: `Enter your Firefox account password.` Field
  label `Password`, masked input. `Back` returns to email. `Continue`
  submits.
- **2FA page.** Body depends on the daemon's requested verification method:
  - code: `Enter the verification code from the email Mozilla sent you.`
  - email link: `Check your email and open the Mozilla sign-in link, then
    choose Continue with the code empty to finish signing in.`
  Field label `Verification code`, normal input; submitting empty re-checks
  the email-link flow. `Back` returns to password.

While a round trip runs: the status line reads `Signing in...` and `Back` /
`Continue` are disabled. `Enter` in the sign-in field submits the page.
Carried-over text rules: the email is restored when returning to the email
page; passwords and codes are always cleared when leaving their page; a
failed email submit keeps the typed text so it can be fixed.

**Already-signed-in handling (never a dead-end):** if the daemon already
holds a session while on a login page (upgrade/CLI sign-in), the page shows
instead:

- Body: `Kumiho already has a signed-in Firefox account. Choose Continue to
  finish setting up, or sign out to use a different account.`
- Identity line `Signed in as <email>` (or `Signed in` when the email is
  unknown), a tonal `Sign out` button, `Continue` enabled. `Continue` asks
  for no credentials and advances straight to Health check.
- `Sign out` opens the same shared confirmation popup as Account (title/body
  as quoted above); on confirm (15 s) the session is cleared and the page
  flips back to the sign-in email field. Failure: `could not sign out:
  <err>`. While signing out the status reads `Signing out...`.

**Login failures** show the daemon/transport error on the status line (bad
tone), e.g. `cancel the browser sign-in first` or a rejected password; a fresh
authenticated status clears the stale error.

### 3. `Health check`

- Body: `Kumiho checked this PC. Fix anything marked Failed before
  connecting; warnings are usually fine.`
- The check suite runs automatically the first time this page is shown; its
  rows appear in a scrolling read-only list, one per check, formatted
  `Name: <status word>: <detail> — <remediation>` (remediation appended for
  Warning/Failed, e.g. `Kumiho service: OK: service is running`).
- A tonal `Re-run checks` button repeats the run (inert while one is in
  flight; a fixed problem can be re-checked without restarting the app).
- While running, the status line reads `Running health checks...`; when done
  it carries the same summary wording as the Diagnostics screen (`All N
  checks passed` / warn / failed). `Continue` advances; `Back` returns to
  the sign-in (email) page regardless of how the user got here.

### 4. `Almost done`

- Body: `Kumiho can connect automatically whenever it starts. You can change
  this later in Settings.`
- Row `Autoconnect` with an on/off toggle button showing the daemon's
  current effective value; clicking flips it immediately through the daemon
  (15 s). The daemon's first-install default is on.
- Action: `Finish` → advances to the last page.

### 5. `You're all set`

- Body: `Kumiho is ready. Press Connect on the Dashboard to start the
  tunnel.`
- Action: `Open Kumiho` → records `onboarded = true` in `gui.toml` and opens
  the Dashboard. This is the only path that suppresses the wizard on later
  launches. `Back` returns to the autoconnect page.

**Wizard status line priority:** busy (`Signing in...` / `Signing out...`) →
local error → health running → health summary → the view-model's login error
(bad) → the view-model's notice (e.g. `Signed in as <email>`) → empty.

---

# Tray

**Icon:** a single static fox icon in the notification area while the app
runs (no per-state icon variants; state lives in the tooltip and menu).

**Tooltip:** `Kumiho — <state word>` where the state word is exactly the
header's word (`Loading...`, `Status unavailable`, `Daemon unreachable`,
`Off`, `Connected`, `Proxy only`, `Connecting`, `Reconnecting`, `Waiting for
network`, `Not signed in`, `Error`).

**Menu** (opens on **left-click, right-click, or double-click** on the icon;
no separate default click action — see discrepancy list). Items in order:

| Item | Behavior |
| --- | --- |
| `Connect` / `Disconnect` (dynamic label, same rule as the Dashboard button) | Runs the action (90 s), then refreshes. Disabled while a tray action is in flight; a second click before the refresh is a no-op. |
| `Location: <CC>` or `Location: (none)` | Disabled informational entry; never disappears (so the command set stays fixed). |
| `Show Kumiho` | Restores a minimized window and focuses it. |
| `Diagnostics` | Restores/focuses the window and switches to the Diagnostics screen. |
| `Quit` | Closes the window and exits the process (icon removed, control connection closed); the daemon and any tunnel keep running. |

No separators, no checkmarks. `Quit` has **no confirmation**.

**Behaviors:**

- The menu/tooltip update from the same 250 ms refresh as the window.
- If Windows Explorer restarts, the icon disappears with the taskbar and
  comes back on its own (shell broadcasts `TaskbarCreated`).
- Documented quirk: the tray menu runs its own modal loop; if the app is
  ended while the menu is open, the icon/process can linger until the menu is
  dismissed (click elsewhere or Esc) — treated as expected USER32 behavior.
- The tray is created after the window, and removed on every exit path.

---

# Tray/app lifecycle

- **Start at login:** **on by default** (D10). `gui.toml`'s `autostart` flag
  is mirrored to the per-user Windows Run key entry `Kumiho GUI` with the
  quoted path of the running executable. The entry is written at every
  launch to match the stored flag, and toggling `Start at login` in Settings
  writes/removes it immediately. A failure to write surfaces in the Settings
  status line; the stored flag is kept either way. The entry lives in the
  hive of whoever launched the GUI.
- **Single instance:** **none**; two copies of the app can run
  simultaneously (the testing checklist explicitly asks for one at a time).
  There is no second-instance hand-off to the existing window.
- **Window close (X):** persists window geometry and exits the process; the
  tray icon is removed and the control connection closed. There is **no
  close-to-tray and no confirmation dialog**; the daemon is never stopped.
- **Tray Quit:** identical to closing the window.
- **Window rebuild for theme/accent:** selecting Theme/Accent closes the
  window and builds a fresh one with the new palette (no process restart),
  re-showing the same screen; window geometry is preserved. Screen-local
  state (filters, expansion, scroll) is not preserved across a rebuild;
  daemon-backed state is.
- **Exit code:** `Run()` returns the window's exit code; normal exits are 0.
- **Teardown:** tray removed, then the control connection closed (best
  effort). The daemon is deliberately never shut down by the GUI — the
  view-model has no shutdown verb.
- **Daemon start/stop:** the GUI never starts or stops the service except
  indirectly by running the installer from the Set up Kumiho page. If the
  service stops while the app runs, the next failing daemon call flips the
  app to the Set up Kumiho page.

---

# GUI-local preferences (`gui.toml`)

Location: `%APPDATA%\kumiho\gui.toml` (per user), overridable with the
`KUMIHO_GUI_CONFIG` environment variable. Written atomically, mode 0600;
missing/damaged file → defaults are used and written back. Unknown stored
theme/accent values resolve to defaults; a stored window smaller than
800×560 is treated as never recorded.

| Key | Values | Default | Effect |
| --- | --- | --- | --- |
| `theme` | `"light"`, `"dark"`, `"oled"` | The Windows app light/dark setting (`AppsUseLightTheme`); anything else (including any read failure) → `dark` on first run. Once the file exists it wins. | House theme; applies immediately with a window rebuild. |
| `accent` | `"blue"`, `"teal"`, `"green"`, `"orange"`, `"rose"`, `"violet"` | `"blue"` | House accent; applies immediately with a window rebuild. |
| `window` | `x`, `y`, `width`, `height` (96-DPI screen coordinates; multi-monitor may be negative) | zero (no geometry recorded) | Restored and clamped into the monitor work areas; saved on every close (minimized positions ignored). |
| `autostart` | `true`/`false` | `true` | Start the GUI at Windows login through the `Kumiho GUI` Run entry; mirrored immediately on change. |
| `onboarded` | `true`/`false` | `false` | When true, launch goes straight to the Dashboard; set only by `Open Kumiho` in the wizard. |

Not in `gui.toml` (session-only): logs level/search/follow/scroll,
Locations filter/expansion/scroll, Settings scroll offset, theme-independent
screen caches.

---

# Design system summary (names only)

- **Themes:** Light, Dark, OLED (OLED is true black `#000000` with stepped
  containers; Dark and OLED share dark family tones; first run follows the
  Windows app light/dark setting).
- **Accents (six, one per app):** Blue (default), Teal, Green, Orange, Rose,
  Violet. One accent per screen; the accent is reserved for the selected
  destination and a screen's single primary action; status colours
  (profit/loss/warning) are never accents.
- **Type:** one family (the native Windows system UI face); three steps:
  small 9 pt, body 10 pt, heading 15 pt (bold headings).
- **Spacing and metric canon (units at 96 DPI):** title rows 32; group gap
  16; intra-group gap 8; label column 140; standard control/status-row
  height 40; card radius 12 (static cards are square-cornered with a 1-unit
  outline ring; list/table cards keep the 12-unit rounded corners); button
  radius 8; nav rail 200 wide with 40-high items; state chips 170×28,
  radius 6; quota bar 8 high, radius 4.
- **Bans carried over:** no gradients, no drop shadows, no emoji, no
  literal colours outside the theme package, no status-by-colour-alone.
- **Accessibility floor:** every state word is present in text; focus rings
  on every interactive control; 40-unit targets; accessible names/roles for
  all controls (icons are decorative and never join names); the two
  confirmations focus `Cancel` by default; reduced-motion and logical
  properties N/A (native app, English-only).
- **Native chrome (best-effort):** dark title bar/frame/shadow for
  Dark/OLED on supported Windows versions; rounded confirmation popup on
  Windows 11; menus/tooltips follow the Windows app setting, not the house
  theme.

---

# Packaging & install (as the user experiences it)

**Artifacts:** `kumiho-v<version>-windows-amd64.zip` contains `kumiho.exe`
(service + CLI/TUI), `kumiho-gui.exe` (the desktop app), `wintun.dll`,
`install.cmd`, `install.ps1`, `LICENSE`, `NOTICE`,
`third_party\wintun\LICENSE.txt`.

**Install (`install.cmd`, double-clicked):**

1. SmartScreen warning for the unsigned download: **More info → Run anyway**.
2. The wrapper captures the invoking user, then elevates via UAC; declining
   leaves the machine untouched.
3. A console lists each step; copies both exes plus `wintun.dll` **and the
   installer copies** into `C:\Program Files\Kumiho`; creates the Start Menu
   shortcut `Kumiho.lnk` targeting `C:\Program Files\Kumiho\kumiho-gui.exe`
   (working directory `C:\Program Files\Kumiho`); adds the directory to the
   machine `PATH`; writes `%ProgramData%\kumiho\config.toml` if absent;
   creates the local group `kumiho`; registers the `kumiho` service
   (LocalSystem, auto-start, recovery restart after 5 s / 30 s / 60 s) and
   starts it; grants the installing user directly on the daemon pipe (no
   Windows sign-out needed for the GUI; group membership alone would take
   effect at next sign-in). Idempotent; re-running upgrades and restarts.
4. PowerShell-direct install (`install.ps1` from an **Administrator**
   shell) is the equivalent; the script refuses without elevation.

**Launching the app:** Start Menu `Kumiho`, or `kumiho gui` from a terminal:
the launcher starts `kumiho-gui.exe` **detached** (no console, outlives the
terminal) and returns immediately with exit 0. Errors:
`kumiho gui: cannot locate the running executable: <err>`;
`kumiho gui: <full path to kumiho-gui.exe> is missing; run install.cmd to
install the desktop app`; `kumiho gui: cannot start the desktop app: <err>`
(all exit 1). On non-Windows platforms, `kumiho gui` prints `the desktop app
is Windows-only; run \`kumiho\` for the TUI` and exits 2.
`kumiho-gui.exe --version` (or `-v`) prints `kumiho-gui <version>` on the
parent console if any and exits 0 without opening a window. Without
`--version` the app opens the window (and, on a first run, the wizard).

**Pipe access as experienced:** the pipe grants LocalSystem, Administrators,
the local `kumiho` group, and an installer-written allowlist
(`%ProgramData%\kumiho\pipe-users`). A standard account in the group works
after the next sign-in; the installing user works immediately. A standard
account not in the group gets "cannot reach the daemon" from clients. The
allowlist fails closed if unreadable/malformed. Windows `shutdown` over the
pipe is administrators-only (not a GUI feature; listed for parity context).

**Uninstall:**

- `install.cmd -Uninstall` (self-elevating; from the extracted folder):
  stops and deletes the service, removes `%ProgramFiles%\Kumiho` (including
  the copied installers), the machine `PATH` entry, the Start Menu shortcut,
  the `Kumiho GUI` login entry (in the invoking user's hive, removed by
  loading the hive when the user is signed out, with an explanatory failure
  message otherwise), and the user's pipe grant. The installer console
  reports each removal; exit code 0.
- `-Uninstall -Purge` additionally removes `%ProgramData%\kumiho` (tokens,
  config, state) and the local `kumiho` group.
- **Always left alone:** per-user `%APPDATA%\kumiho` (`gui.toml` and the
  TUI's `tui.toml`).
- A running GUI blocks its own deletion, so quit the app first
  (tray → `Quit`).
- `-Purge` without `-Uninstall` errors with `-Purge requires -Uninstall`.

**Set up Kumiho repair path:** the hidden page's `Set up Kumiho` runs the
same installer from the app folder (installed copies live beside the exes),
covering a missing/stopped service.

---

# Behavioral inventory (retries, recovery, status semantics, notices)

**Connection and daemon reachability**

- The GUI never polls the daemon on a timer; the daemon **pushes** status
  events on every state change, ping results and (while subscribed) live log
  lines; the 250 ms UI pump applies them. `Refresh` actions fetch a snapshot
  on demand.
- The IPC client dials lazily with **3 attempts, 200 ms backoff doubling**;
  a dial failure is reported as "daemon unreachable" and flips the app to
  the Set up Kumiho page (remembering the prior screen). A broken pipe is
  dropped and the **next** request re-dials; requests are never re-sent
  (avoiding double-submitting login/settings/clear).
- Active timeouts: connect/disconnect 90 s; status refresh 15 s; locations
  load 30 s; ping request 10 s; location select 10 s; settings load/apply
  15 s; account load 35 s; logout 15 s; logs snapshot 15 s / subscribe 10 s
  / unsubscribe 5 s / clear 5 s; onboarding login 60 s and autoconnect 15 s;
  setup retry 15 s.
- Busy guards: connect/disconnect buttons, tray action, Settings mutation,
  Account sign-out/add/use/remove/rename/re-auth/rotation, Onboarding busy
  state, Diagnostics run, Test speed, Setup Retry, Logs reconcile — at most
  one in flight, repeat clicks ignored or buttons disabled.
- State words: `Off` (IDLE/empty), `Connected`, `Proxy only`, `Connecting`,
  `Reconnecting`, `Waiting for network`, `Not signed in`, `Error` (FATAL).
  The header additionally has `Loading...`, `Status unavailable`,
  `Daemon unreachable`.
- The GUI itself implements no reconnect logic; daemon-side watchdog
  redials/heals and the GUI reflects it (docs advise waiting ~10 s during an
  outage; the kill switch may block traffic meanwhile). The Dashboard
  `Retry` only exists for a failed initial status load (non-unreachable).
- Login is resilient: signing in while another account is active adds or
  switches accounts on completion; only an in-flight browser/password flow
  blocks a second start.

**Notices / toasts**

- Two kinds:
  1. **Screen-local notices** (Account, Settings, Logs, Setup, Diagnostics):
     10-second lifetime (`noticeTTL`), then the status line returns to its
     live state (retired by the 250 ms pump).
  2. **Shared view-model notices** (Dashboard, Locations, Onboarding): no
     timer; they persist until another action replaces them.
- Notice wording always prefixes the failure: `could not connect: ...`,
  `could not disconnect: ...`, `could not refresh the status: ...`,
  `could not change the setting: ...`, `could not load the logs: ...`, etc.

**Logs behavior**

- Ring snapshot + live subscription; client-side merge with index tracking
  that resyncs when either buffer trims at the 2000-entry capacity;
  deduplication anchors on the last known entry when the buffer overflowed.
- Level button re-snapshots and resubscribes; subscribe failures heal on the
  next successful snapshot (reconcile retries).
- Follow turns off as soon as the user scrolls away from the tail and back
  on when they return to it.

**Locations behavior**

- Ping results are event-driven labels (`42 ms` / `timeout`), keyed
  `host:port`; up to 16 visible server rows per run; one run at a time.
- Selection is persist-only (no connect), survives restarts, and the tree
  reveals it on load.
- Filtering auto-expands countries; expansion state is local; a failed
  refresh keeps stale rows.

**Settings behavior**

- Immediate-apply rows: kill switch, log level, redact targets, solve
  challenges. Next-connect rows: exit check, DoH provider, DNS upstream,
  edge address, upstream proxy, exclude CIDRs, allow LAN, MTU, SOCKS port,
  quota poll, TLS split, HTTP proxy, HTTP proxy port, keepalive profile.
  Autoconnect is saved daemon-side and takes effect on the next daemon
  start / autoconnect decision. (Tooltips state this per row.)
- Mutation success triggers a model status refresh **and** a settings
  reload, keeping override marks truthful; `overridden` marks and `Reset`
  never guess defaults.
- Theme/accent: immediate shell rebuild (window closes/reopens); `Start at
  login`: immediate registry effect.

**Account behavior**

- Online entitlement failure is non-fatal: subscription shows `Unknown`
  (warn) and the status line warns `Subscription details unavailable:
  <err>` while local rows stay.
- The list comes from `accounts_list`; use/remove/rename/re-auth each refresh
  the list and entitlement. Adding a browser account shows the waiting card,
  auto-opens each unique URL once and polls every second; password add reuses
  the onboarding form inline.
- Rotation runs only before connect. The switch saves immediately; the
  threshold field validates 1–50 and is disabled while busy or loading.
  Expired/exhausted accounts are skipped with the daemon's notice.
- Sign-out clears every account's tokens + disconnects only after
  confirmation; per-account remove leaves the other sessions in place.
  Success reloads the snapshot.

**Diagnostics behavior**

- Check run once on first visit; `Re-run checks` replaces the report;
  `Copy details` is enabled only after a finished run; the summary wording
  differs from the CLI (only counts must match).

**Window/rebuild behavior**

- Theme/accent rebuild preserves geometry and the current screen; connection
  and daemon data are unaffected. Screen-local view state resets.

---

# Open questions / doc-vs-code discrepancies (for the rewrite)

1. **Tray click semantics.** `docs/windows-guide.md` says "Right-click it for
   …", and `windows-testing.md` 17.7 checks a right-click. The code opens the
   same context menu on **left-click, left double-click and right-click**,
   with no other default action. Confirm the intended click model.
2. **Wizard suppression.** `windows-guide.md` ("The wizard does not run again
   after this") reads as if finishing always persists, but only `Open
   Kumiho` writes `onboarded = true`; closing the window mid-wizard and
   relaunching restarts the wizard (the testing doc explicitly expects
   this). Decide whether the docs or the behavior is the parity target.
3. **Autoconnect "on by default" in the wizard.** The guide says the
   "Almost done" step offers Autoconnect "on by default"; the GUI simply
   shows the daemon's current effective value (first-install default on) and
   never sets a wizard-specific default itself.
4. **Diagnostics "runs on entry".** `windows-testing.md` 17.9 says checks
   run "on entry"; the code runs them on the **first** visit only and keeps
   the previous report on later visits until `Re-run checks`. Confirm
   intended behavior.
5. **Disconnect availability.** Docs say the Dashboard button flips to
   `Disconnect` "while it is up"; the code shows `Disconnect` for every
   non-idle/loading state (`Connecting`, `Reconnecting`, `Waiting for
   network`, `Proxy only`) and disables it while an action is in flight.
6. **`Test speed` scope.** The guide says it measures "the servers shown";
   the code silently caps a run at the **first 16 visible server rows**. Is
   the cap acceptable/needed in the rewrite (or should it measure all
   visible)?
7. **Header tone inconsistency.** The Dashboard chip tones `Error`/`Connected`
   (bad/good) but the header shows the same word in the normal text tone
   (tone is only used for `Status unavailable` / `Daemon unreachable`).
   Decide whether the rewrite should tone header state words too.
   **Resolved (v0.3.0):** the rewrite dropped the Dashboard chip, so the
   header word is the only state display and keeps the normal text tone.
8. **Back from Health check.** The onboarding `Back` from Health always goes
   to the sign-in email page (the model's rule), not to the page the user
   actually came from (e.g. password/2FA or the signed-in variant). Confirm
   this is intended.
9. **`KUMIHO_GUI_CONFIG`** (the `gui.toml` path override) exists in code but
   is not documented in the Windows docs; decide whether portability parity
   requires it.
10. **Locations filter copy style.** The in-list empty placeholder is
    `No locations match the filter.` (with period) while the status line is
    `No locations match the filter` (no period); the loading placeholder uses
    a typographic ellipsis (`…`) while most other strings use `...`. Keep
    exact strings if parity is byte-level.
11. **Design-doc miscue:** `docs/windows-guide.md` describes the Logs screen
    as "level filter, search, follow, Clear and Refresh" — it also has
    `Export` and `Open log folder` (covered elsewhere in the guide); no
    functional gap, just wording.
12. **Uninstall while signed in under another account** (`windows-testing.md`
    17.11): removing `Kumiho GUI` from another user's hive fails with a
    message when that user is signed in; behavior is documented but is an
    installer edge to preserve or simplify in the rewrite.
