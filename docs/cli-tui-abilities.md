# Kumiho feature inventory — CLI, TUI and daemon control surface

Sources read: `internal/cli/*.go`, `internal/tui/*.go`, `internal/api/api.go`,
`internal/ipc/{ipc,client,server}.go`, `internal/daemon/run.go`,
`internal/engine/*.go`, `internal/settings/settings.go`,
`internal/doctor/*.go`, `internal/logging/logging.go`,
`internal/serverlist/serverlist.go`, `docs/*.md`, `PLAN.md`, `README.md`.
This is a research artifact; nothing in the repo was modified.

Legend:
- **Daemon** = the command requires a running daemon over IPC and fails with
  exit 1 and the message `kumiho: cannot reach the daemon at <endpoint>` plus
  a start hint when the daemon is down. Endpoints: `/run/kumiho/control.sock`
  (Linux), `\\.\pipe\kumiho\control` (Windows).
- **Standalone** = works without the daemon (may still read config/state or
  touch system networking).
- **GAP:** = a daemon-side ability a GUI **cannot** perform over the current
  IPC protocol without a protocol change (new request/response type,
  subscription or field). Nothing is invented below; gaps are flagged only.

Global CLI mechanics (apply to every subcommand):
- `flag` package parsing, `flag.ContinueOnError`; `-h`/`--help` on any
  subcommand prints that subcommand's usage to stderr and exits 0.
- Exit codes: 0 success; 1 runtime/daemon/operation failure; 2 usage/flag parse
  error and unknown command (`kumiho help` and `kumiho --help`/`-h` print the
  command list to stdout, exit 0).
- No command is a long-running editor; the only interactive commands are
  `login` (terminal prompts), the no-args TUI, and `logs -f` (streams until
  Ctrl+C).

---

# Kumiho CLI — every command and ability

## `kumiho` (no arguments) — TUI launcher
- Invocation: `kumiho` (also the zero-arg fallback of `Run`).
- What it does: dials the daemon (5 s timeout), then runs the Bubble Tea TUI
  in the alternate screen (`internal/tui.Run`).
- Output/exit: dial failure → `kumiho: cannot reach the daemon at <endpoint>`
  + start hint to stderr, exit 1. `tui.Run` error → `kumiho: <err>`, exit 1.
  Normal quit → exit 0.
- Daemon required. Interactive (all screens/keys below).

## `kumiho gui` — open the Windows desktop app
- Windows (`gui_windows.go`): locates `kumiho-gui.exe` next to the running
  executable; missing → `kumiho gui: <path> is missing; run install.cmd to
  install the desktop app`, exit 1; `cmd.Start` failure → `kumiho gui: cannot
  start the desktop app: <err>`, exit 1. Starts it detached (DETACHED_PROCESS)
  and returns immediately, exit 0.
- Non-Windows (`gui_other.go`): prints `the desktop app is Windows-only; run
  `kumiho` for the TUI` to stderr, exit 2.
- Standalone; no daemon contact, no IPC.

## `kumiho version` / `--version` / `-v`
- Prints `kumiho <Version> (commit <Commit>, built <Date>)` to stdout, exit 0.
  Version metadata injected at build time via ldflags (defaults `dev`,
  `unknown`, `unknown`).
- Standalone. No update check exists anywhere (no self-update, no "new version
  available" query).

## `kumiho doctor [--json]`
- Runs all preflight checks in-process (`doctor.Run()`); no daemon contact.
- `--json`: prints `doctor.Report` JSON (indent 2): `{version, host, os, time,
  checks:[{name,status,detail,remediation?}], failed, warned, skipped}`.
- Human output:
  - header `kumiho doctor - <version>`, `host: <host> (<os>)` (os = `GOOS/GOARCH`), `time: <RFC3339 UTC>`
  - per check `  OK|WARN|FAIL|SKIP <name>: <detail>` and, for non-OK/non-SKIP
    with remediation, `       -> <remediation>`
  - summary `Summary: <ok> ok, <warn> warn, <fail> fail, <skip> skipped`
  - if any fail: `Some checks failed - follow the remediation steps above.`
- Exit codes: 1 if any check has status `fail`, else 0. Flag parse error → 2.
- Checks (common, in order): `platform` (OK on linux/windows full-tunnel
  provider; WARN "full tunnel is not implemented yet" elsewhere), `tun-device`,
  `cap-net-admin`, `nftables`, `systemd-resolved`, `dns-manager`, `tailscale`,
  `ipv6`, `default-route`, `socks-port` (probes `127.0.0.1:1080`; WARN when in
  use and a daemon appears to run), `control-socket`, `clock-skew` (compares
  against Mozilla's HTTP `Date`; >60 s FAIL, >30 s WARN), `endpoints`
  (reachability of FxA, Guardian status and Remote Settings).
- Off Linux the Linux-only checks are dropped entirely (not shown as SKIP):
  `tun-device`, `cap-net-admin`, `nftables`, `systemd-resolved`,
  `dns-manager`, `tailscale`, `ipv6`, `default-route`, `control-socket`.
- Windows-only checks appended: `windows-service` (installed? state/start
  type; absent/stopped warns), `wintun-dll` (present beside exe; version),
  `wfp-service` (Base Filtering Engine running; FAIL otherwise), `control-pipe`
  (dial result; SKIP when missing; WARN on access-denied/busy), `tun-adapter`
  (`foxy0` presence; SKIP when absent), `adapter-dns` (adapter DNS must be
  `10.8.0.2`; WARN otherwise), `physical-default-route`, `elevation`
  (WARN when not elevated).
- GAP: doctor has **no IPC request type**. The CLI and the existing Windows GUI
  call `internal/doctor` in-process. A GUI that cannot link the Go package
  would need a new `doctor` request/response.
- Standalone; needs no privileges, "Run sudo/Administrator for repair-level
  probes" is a prompt only (remediation text).

## `kumiho daemon` — run the daemon in the foreground
- Invocation: `kumiho daemon` (no flags). Runs until SIGINT/SIGTERM; owns the
  TUN, control socket, kill switch.
- Windows: when started by the Service Control Manager, the same command
  serves as the service entry point: reports StartPending → Running →
  StopPending → Stopped, accepts Stop/Shutdown, SCM exit code 0 on clean exit,
  1 when `daemon.Run` returned an error (error printed to the log sink, not a
  console).
- Output/exit: error path `kumiho daemon: <err>` to stderr, exit 1; clean → 0.
- GAP: this is the process itself; a GUI cannot start a foreground daemon via
  IPC (it uses the installed service/Services API instead).
- Standalone.

## `kumiho login` — Firefox Accounts sign-in
- Invocation: `kumiho login [--browser|--password] [--email <email>]
  [--no-open]`. Daemon required (the daemon runs the login state machine).
- Method choice: with a terminal and no method flag, prompts browser/password
  with the last successful method preselected (browser on first run). Without
  a terminal it uses the password flow for script compatibility; pass
  `--browser` explicitly to use the browser. `--email` skips the email prompt.
- **Browser:** `login_start` returns a Firefox Accounts URL and `login_id`.
  Unless `--no-open`, the CLI opens it with the platform opener. It prints
  `Open this URL to sign in:` followed by the URL, then the instruction to
  copy the complete redirect URL from the browser's address bar, and prompts
  `Redirect URL:`. The browser's “can't open this address” page is expected:
  Mozilla redirects to the registered `urn:` URL. The CLI submits the pasted
  URL with `login_complete` and polls `login_status` for the terminal state.
  The daemon owns a ten-minute flow with no listener; the CLI cancels the flow
  if its deadline expires and prints `timed out waiting for the browser
  sign-in`. Success prints `Signed in as <email>.`.
- **Password:** interactive prompts via `golang.org/x/term`:
  - `Email: ` (plain read)
  - `Password: ` (hidden read)
  - `Code: ` (plain read) for the 2FA/TOTP/unblock step
  Flow: `login_email` → `login_password` → optional `login_2fa` → `done`.
  2FA with `verification_method == "email"` (link flow) prints `Check your
  email and open the sign-in link, then press Enter (leave the code empty).`
  Otherwise prints `Enter the code from the email you received.`
- Errors: `login: <message>` / `kumiho login: <err>` to stderr; password loop
  bounded at 12 attempts → `kumiho login: too many attempts`, exit 1.
- Exit codes: 0 / 1 / 2 (flag parse).
- Interactivity: prompts and browser opener; no editor launching.
- Signing in while another account is already active adds or switches to the
  new account on completion; there is no sign-out-first requirement. Only an
  in-flight browser/password flow blocks a second start.

## `kumiho accounts` — list and manage stored accounts
- Invocation: `kumiho accounts [list]`.
- Prints one line per account: active marker `*`, email plus label, method,
  quota percent (`quota 42%`, `quota exhausted`, or `quota unknown`) and
  `re-auth required` when set. Ends with
  `Rotation: on|off (threshold N%)` and, when present, `Notice: <text>` and
  `Error: <text>`.
- `accounts add [--browser|--password] [--email <email>]
  [--account <id|email>] [--no-open]` reuses the login flow; `--account`
  re-authenticates an existing row and selects its stored method when no flag
  is given. A terminal asks for the method; a script uses password. Adding a
  new row prints `Added account <email>.`
- `accounts use <id|email>` switches the active account. The daemon refuses
  while the tunnel is up (`disconnect first; switching accounts needs the
  tunnel off`).
- `accounts remove <id|email> [--yes]` deletes one account. Removing the active
  account while others remain asks for y/N confirmation (skip with `--yes`);
  removing the last account clears the local session without prompting.
  Removing the active account selects the most recently used remaining one.
  `accounts remove` never performs `logout`'s best-effort refresh-token
  revocation.
- `accounts rename <id|email> <label>` replaces the label. The label is
  trimmed, control characters are rejected, an empty CLI label is rejected,
  and the 64-character limit is enforced. Clients use the `accounts_update`
  IPC command for the same operation; there an empty label resets it to the
  email.
- `accounts rotation on|off` toggles the opt-in before-connect rotation and
  leaves the threshold unchanged.
- `accounts threshold <1-50>` sets the percentage and preserves the current
  on/off state.
- There is no separate `accounts update` CLI verb; `rename` is the CLI face of
  `accounts_update`.
- Exit codes: 0 / 1 / 2 (usage or range).

## `kumiho logout`
- Invocation: `kumiho logout`. Daemon required (`logout` request).
- Clears **all** stored accounts (and the legacy `tokens.json` rollback copy),
  disconnects the tunnel and returns to UNAUTHENTICATED. Prints `Signed out.`
  on success. Failure `kumiho logout: <err>`, exit 1. Removing one account
  (`accounts remove`) is not a logout: the other accounts stay signed in.

## `kumiho connect [--to CODE|CITY|HOST] [--proxy-only] [--kill-switch=on|off|last]`
- Invocation example: `kumiho connect --to DE --proxy-only --kill-switch=last`.
- Daemon required. 90 s client timeout ("Connect can spend up to four dial
  attempts with backoff").
- `--kill-switch=on|off|last`: applied first via `settings_set`
  (`kill_switch`); a failure aborts with `kumiho connect: kill-switch: <err>`,
  exit 1. (`last` pins the current effective value.)
- `--to` resolution is **client-side** against the locations list (client
  calls `locations` and matches, case-insensitive):
  1. exact country code, 2. exact city code, 3. exact server host or
     `host:port`, or host prefix, 4. unique substring of a country **name**
     (0 matches → `no location or server matches "<to>"`; >1 →
     `"<to>" matches <codes>; use one country code`).
- `--proxy-only`: connect with `proxy_only: true` — keeps system routing
  untouched, starts only the SOCKS5 listener (`127.0.0.1:1080`). Without it the
  full tunnel (TUN `foxy0` + routes + DNS) comes up.
- Prints `connecting (this can take up to ~30s)...` to stderr, then the
  human-readable status snapshot (same as `status` without `--json`) to stdout
  on success. Failure: `kumiho connect: <err>`, exit 1. Exit 2 on flag parse.
- Notes: daemon rejects with `already connected; disconnect first`
  (`bad_request`) and `sign in first (kumiho login)` (`not_authenticated`).
  The chosen target is persisted to `state.json` and 3-consecutive-failure
  auto-clear applies (see connect/disconnect semantics).

## `kumiho disconnect`
- Invocation: `kumiho disconnect`. Daemon required. Tears the tunnel down
  (full or proxy-only). Prints `Disconnected.`; errors
  `kumiho disconnect: <err>` (including `not connected` when idle), exit 1.

## `kumiho cleanup`
- Invocation: `kumiho cleanup`. **Standalone**, usually run as root /
  Administrator. Contact with the daemon is neither required nor checked;
  docs instruct stopping the service first.
- Loads config/state paths, builds the platform `NetConfig` spec from
  effective config (tun name/addr, MTU, effective kill switch, allow-LAN,
  excluded CIDRs) and calls `Cleanup`; on platforms with the optional
  `CleanupStale` capability (Windows) also removes the `foxy0` adapter and
  default routes left by a crashed run.
- Output: informational `kumiho cleanup: <msg>` progress lines to stderr when
  the platform supports log forwarding; on success prints
  - Linux: `Stale kumiho routing, nftables and DNS state were removed (best effort).`
  - Windows: `Stale kumiho routing, firewall and DNS state were removed (best effort).`
- Exit codes: 0 on success (cleanup itself is best-effort and reports no
  per-step errors), 1 only if settings/config cannot be opened, 2 flag parse.
- GAP: cleanup has **no IPC request type** and requires OS privilege. A GUI
  must spawn the CLI elevated (UAC) or a new privileged `cleanup` request must
  be added.

## `kumiho locations [--ping] [--refresh] [--json]`
- Invocation examples: `kumiho locations`, `kumiho locations --ping --refresh`.
- Daemon required (`locations` request; daemon caches the list for 30 min and
  a failed refresh falls back to the cache).
- `--refresh`: force re-fetch from Remote Settings. `--json`: prints the raw
  countries array (indent 2, not wrapped). `--json` wins over `--ping`.
- Default output: one line per country `%-4s %-22s %d cities` → e.g.
  `REC  Recommended Location  3 cities`.
- `--ping`: collects every non-quarantined server's connect `host:port`,
  sends one `ping` request, then waits up to 6 s for streamed `ping_result`
  events and prints per target:
  - `%-4s %-6s %-45s %.0f ms` on success
  - `... unreachable` (nil RTT) or `... no result` (no event in time)
  - Daemon probes TCP, max 24 concurrent.
- Exit: 0 success; 1 daemon/list/decode/ping failure; 2 flag parse.

## `kumiho logs [-f] [--level debug|info|warn|error]`
- Invocation examples: `kumiho logs`, `kumiho logs -f --level debug`.
- Daemon required. `--level` default `info`; accepted names also include
  `warning` (alias for warn); invalid → `kumiho logs: unknown log level "<x>"`,
  exit 2.
- Without `-f`: fetches a `logs_snapshot` at the level and prints each entry
  as `RFC3339 TAG3 [tag] msg` (e.g. `2026-10-15T12:00:00Z INF [daemon] ...`),
  exit 0.
- With `-f`: prints the snapshot, then `logs_subscribe` and streams live
  `log` events in the same format until Ctrl+C (exit 0) or the connection
  ends (exit 0). Errors → exit 1.
- Interactivity: streaming only; Ctrl+C stops.

## `kumiho status [--json]`
- Daemon required (`status` request). `--json` prints the full `api.Status`
  JSON (indent 2). Human output prints only present fields, in this order:
  `state`, `account`, `token expires` (RFC3339), `location` (`CODE (server)`),
  `pass expires`, `quota` (`<remaining> remaining of <limit>` + `(resets ...)`),
  `exit ip` (`ip (country)`), `totals` (`up X / down Y`, binary units),
  `rates` (`up X/s / down Y/s`, sampled every 2 s), `kill switch: on|off`,
  `autoconnect: on|off`, `state since`.
- Exit: 0 / 1 (daemon error) / 2 (flag parse).

Command not present: there is no `kumiho shutdown`, no service
start/stop/restart command, no `kumiho tui` alias (no args is the TUI), no
update command.

---

# Kumiho TUI — every screen and ability

Launch: `kumiho` (requires daemon; see above). Runs in the alternate screen.
On start it loads `tui.toml` preferences, applies the theme, opens on the
Dashboard, and starts with the Logs state `follow = true, minLevel = info`.
It subscribes to daemon `status`, `ping_result` and `log` events, which
live-update the Dashboard, Locations and Logs screens. If the daemon
connection closes, the TUI quits; if a status fetch fails, the notice
`daemon error: <err>` shows on the Dashboard.

Global keys:
- `?` opens the Help overlay **except** while free-text input is active
  (login fields, settings inline edit, log search, location filter).
- `q` quits from every screen in normal (non-input) mode; while a text field
  is active (login, settings edit, log search, location filter) `q` is typed
  into the field instead. `ctrl+c` always quits, everywhere. In the sign-out
  confirmation `q` cancels the confirmation rather than quitting.
- `esc` goes back to the Dashboard from Locations / Settings / Logs / Account;
  cancels the login screen; cancels settings edit, log search and location
  filter; cancels the sign-out confirmation (any key other than `y`/`Y`).

## Dashboard (home)
Shows (when a status snapshot has arrived; otherwise `loading status...`):
- `state:` — color-coded: green for `IDLE`/`PROXY_ONLY`/`CONNECTED`, red for
  `FATAL`, yellow otherwise; `uptime:` (`time.Since`, rounded to seconds) only
  while `CONNECTED`/`PROXY_ONLY`; `account:`; `token expiry:` (UTC RFC3339)
- `location:` (`CODE (server)`), `pass expiry:` (UTC RFC3339)
- `quota:` with a 20-cell bar `[████░░░░]`, `remaining of limit`, and
  `quota reset:` date; `exit ip` (`ip (country)`); `totals` up/down;
  `rates` up/down per second; `dns:` and `ipv6:` badges (from status);
  `kill switch: on|off`; `autoconnect: on|off`
- notice line (green) after actions: `connected`, `disconnected`,
  `tunnel: <err>`, `signed in as <email>`, `signed out`, `location saved`,
  `selection failed: <err>`, `sign in first (l)`, `daemon error: <err>`
Keys:
- `space` — connect full tunnel (`ConnectPayload{}`) or disconnect. Toggle
  logic: state `PROXY_ONLY|CONNECTED|CONNECTING|RECONNECTING|WAITING_NETWORK`
  → disconnect; anything else → connect. Unauthenticated: notice
  `sign in first (l)` and no action. Calling connect while already connected
  is impossible through the toggle.
- `l` — open Login screen, but only when not authenticated (otherwise no-op).
- `o` — Locations (fetches once on first open).
- `s` — Settings (fetches on every open).
- `a` — Account (fetches on every open).
- `g` — Logs (fetches snapshot and subscribes on open).
- `r` — refresh status.
- Footer varies: authenticated → `space connect|disconnect • o locations • s
  settings • a account • g logs • r refresh • ? help • q quit`; unauthenticated
  → `l login • o locations • r refresh • ? help • q quit`.

## Login screen
With no flow in progress the screen first offers a method: `b browser • p
password` (browser on first run, the last successful method afterwards). `b`
calls `login_start` with method browser and shows the waiting card; `p` enters
the password flow. Steps in the password flow are `email` → `password` → `2fa`
→ done; the daemon state machine drives transitions (`login_state.step`).
- Shows: title `Sign in to Firefox Accounts`; email (dimmed once past the
  email step), password masked as `•` per rune with a trailing `_` cursor,
  2FA prompt with method-specific instructions (email-link text or
  "Enter the code from the email you received."), `contacting Mozilla
  Accounts...` while busy, red error line, footer `enter continue • esc cancel`.
- Browser waiting card: shows `Open this URL in any browser:`, the URL, the
  `Paste the complete redirect URL from the browser:` prompt, an editable
  `Redirect URL:` line and the browser's expected “can't open this address”
  note; `enter` submits the pasted URL and `esc` cancels the flow
  (`login_cancel`). The TUI has no browser opener and does not wait for a
  browser callback.
- Keys: printable runes append to the active field (email / password / code);
  `backspace` edits; `enter` submits (validation: empty email →
  `email is required`; empty password → `password is required`; empty 2FA code
  is allowed and re-checks the email-link flow); `esc` cancels back to the
  Dashboard clearing password/code/error; `ctrl+c` quits.
- On `step=done` without error: back to Dashboard, notice
  `signed in as <email>`, password/code cleared, status refreshed. Server-side
  errors (wrong password, link not confirmed, challenge, …) show in the error
  line; the TUI imposes no attempt cap (the CLI caps at 12 submit rounds).
- Starting a sign-in while another account is active adds or switches to the
  new account on completion; only an in-flight flow blocks a second start.

## Locations screen
Shows one flat server-only list. There are no country or city header rows,
carets or expansion state. Each row is one line:
`<flag> CC · City · host:port` (Recommended is `★ REC · host:port`; a normal
row omits the city when it adds nothing), followed by `  <n> ms` in green or
`  timeout` dimmed when pinged. There are no protocols on the row. `*` marks
the persisted selection by address. `no locations loaded` when empty;
`loading locations...` while fetching; error line on failure. A filter line
`filter: <text>_` appears while filtering or when a filter is set.

Keys:
- arrows / `k` `j` — move the cursor over visible rows.
- `enter` — persist the current server via `select_location` (country code,
  city code, connect target) — **does not connect**; success notice
  `location saved`, failure `selection failed: <err>` (status refreshed either
  way).
- `P` — Ping all: TCP-ping every usable server in the loaded list regardless
  of the filter, deduped by address and skipping quarantined/targetless
  records. Results stream in and update labels without reordering rows. When
  the run completes with at least one result, the list switches once to speed
  order: finite RTTs ascending, failures and untested last, Recommended first;
  the cursor returns to the top. A timeout counts as a result. `p` and `x` are
  no-ops.
- `/` — filter mode: matches country name/code, city name/code, hostname,
  `host:port` or protocol name, case-insensitively; `backspace` edits, `enter`
  keeps the filter and leaves filter mode, `esc` clears the filter, `ctrl+c`
  quits. Filtering never resets the order.
- `r` — force server-list refresh (`--refresh`); clears speed order, latency
  labels and the cursor and returns to country order.
- `esc` — back to Dashboard. Footer: `enter select • P ping all • / filter •
  r refresh • esc back • ? help • q quit`.
- Leaving and re-entering through `o` resets speed order and the cursor.

## Settings screen
Shows 19 rows (label, value; `*` marks a key whose effective value comes from
a persisted runtime override, i.e. differs from `config.toml`), with the
cursor `>`, and the current row's hint line. The list is a viewport over the
terminal content height (height − 9 lines of header/title/hint/footer, minus
2 for an error line and 2 more for a notice when present; a row budget of at
least 3 lines, at most 19); when rows are clipped it prints a dim `↑ more`
line above the list and/or a `↓ more` line below it (no native scrollbar). An unknown
terminal height (no `WindowSizeMsg` yet) shows every row. `loading
settings...` before the first fetch; `* changed from config` appended to the
footer legibly when any override exists. Footer: `enter change • d reset to
default • r refresh • esc back • ? help • q quit`.

Rows and edit behavior (all persisted daemon-side as overrides in
`state.json`; values shown are **effective**; the last column is the hint
line shown under the list for the focused row, verbatim):
| key | label | editor | accepted values | hint |
|---|---|---|---|---|
| `kill_switch` | kill switch | enum cycles on/off (`enter`/`space`) | on/off (effective bool shown; `last` not offered here) | block all tunnel traffic while the tunnel is down; applies immediately |
| `autoconnect` | autoconnect | enum cycles on/off | on/off | connect automatically when the daemon starts |
| `log_level` | log level | enum cycles debug→info→warn→error | debug/info/warn/error | minimum level recorded into the log ring; applies immediately |
| `redact_targets` | redact targets | bool toggle | on/off | mask hostnames and IPs in log messages; applies immediately |
| `exit_check` | exit check | bool toggle | on/off | probe the public exit IP after connecting; next connect |
| `doh_provider` | DoH provider | enum cycles automatic→cloudflare→google→quad9→off | same | in-tunnel DNS-over-HTTPS resolver; next connect |
| `dns_upstream` | DNS upstream | inline text edit | any IP; empty = provider default | resolver IP for DoH; empty = provider default; next connect |
| `edge_address` | edge address | inline text edit | any IP; empty = resolve normally | pin the Fastly edge IP; empty = resolve normally; next connect |
| `upstream_proxy` | upstream proxy | inline text edit | proxy URL; empty = direct | chain the edge connection through a proxy URL; empty = direct; next connect |
| `exclude_cidrs` | exclude CIDRs | inline text edit, comma-separated | CIDRs; empty = none | comma-separated ranges that bypass the tunnel; next connect |
| `allow_lan` | allow LAN | bool toggle | on/off | keep the local subnet outside the tunnel; next connect |
| `mtu` | MTU | inline number edit | 576–65535 | tun device MTU; next connect |
| `socks_port` | SOCKS port | inline number edit | 1–65535 | loopback SOCKS5 listener; next connect |
| `quota_poll_minutes` | quota poll | inline number edit | 1–1440 | minutes between quota refreshes while connected; next connect |
| `tls_split` | TLS split | enum cycles off→first-byte→sni | off/first-byte/sni | split the TLS ClientHello across writes; next connect |
| `http_proxy` | HTTP proxy | bool toggle | on/off | loopback HTTP/CONNECT proxy through the tunnel; next connect |
| `http_proxy_port` | HTTP proxy port | inline number edit | 1–65535 | loopback HTTP proxy listener; next connect |
| `solve_challenges` | solve challenges | bool toggle | on/off | solve FxA and edge challenges automatically; applies immediately |
| `keepalive_profile` | keepalive profile | enum cycles standard→slow | standard/slow | keepalive ping timeout profile; next connect |

Display specials: empty `dns_upstream` → `(provider default)`, empty
`edge_address` → `(resolve)`, empty `upstream_proxy` → `(direct)`, empty
`exclude_cidrs` → `(none)`.
Keys:
- arrows / `k` `j` — move cursor one row; the viewport scrolls to keep the
  cursor visible.
- `pgup` / `pgdown` — jump the cursor one screenful (visible rows − 1) and
  scroll the viewport with it; `home` / `end` jump to the first / last row.
- `enter` / `space` — activate: bool toggles and enum cycles apply
  immediately; text/int rows enter inline edit mode prefilled with the
  current effective value.
- Inline edit: printable runes append (editing uses trimmed value), `backspace`
  edits, `enter` saves — an empty value sends `reset: true` (drops the
  override, restoring the admin config/built-in default) with notice
  `<key> reset to default`; non-empty sends the value with notice
  `<key> updated`; `esc` cancels without applying; `ctrl+c` quits.
- `d` — reset the focused row to default immediately (no edit mode).
- `r` — re-fetch settings; `esc` — back.
- Apply timing (hint text and behavior): `kill_switch`, `log_level`,
  `redact_targets` apply immediately; toggling `kill_switch` on a connected
  full tunnel re-applies the live netcfg, and if that fails the change is
  rejected (`<err>` shown) with the previous value kept. `solve_challenges` is
  consulted per control-plane request, so it applies immediately too.
  `autoconnect` takes effect on the next daemon start/autoconnect decision.
  `tls_split`, `http_proxy`, `http_proxy_port` and `keepalive_profile` are
  connection-scoped, as are all other rows: they take effect on the **next
  connect**.
- Errors (validation rejected by the daemon) show in the error line; server
  error codes: `bad_request`, `unsupported_setting` (e.g. `socks_bind`).

## Logs screen
Shows: header `Logs  following|paused · level ≥ <level> · <N> entries`,
optional error line, `search: <text>_` while searching or when a search is
set, a window of entries formatted `HH:MM:SS TAG3 [tag] msg` (DBG dim, WRN
yellow, ERR red), `(no matching log entries)` when empty, export notice
`exported to <path>` or `export failed: <err>`. Visible height = window
height − 7, clamped to 5–40 rows. The client keeps a 2000-entry buffer,
snapshots with level `debug` from the daemon and subscribes at `debug`,
filtering client-side; the level filter starts at `info` (TUI default).
Keys:
- `f` — toggle follow (paused/following); turning follow on jumps to tail.
- `v` — cycle the display level filter debug→info→warn→error→debug.
- `/` — search mode: type to filter on `tag + " " + msg` (case-insensitive
  substring), backspace edits, `enter` keeps the search, `esc` clears it;
  `ctrl+c` quits.
- `c` — clear the daemon's retained ring (`logs_clear`) and the local buffer.
- `e` — export the currently visible (level- and search-filtered) entries to
  a local file: directory `~/.local/state/kumiho` (Linux) or
  `%LOCALAPPDATA%\kumiho\logs` (Windows), created mode 0700; file
  `kumiho-logs-YYYYMMDD-HHMMSS.log`, mode 0600, one `Entry.Line()` per row
  (RFC3339 TAG3 [tag] msg). Failure notice `export failed: <err>`.
- `up`/`k` scroll up (disables follow), `down`/`j` scroll down (re-enables
  follow at the tail), `pgup`/`pgdown` page (same follow behavior),
  `g`/`home` jump to the oldest, `G`/`end` jump to the tail and re-enable
  follow.
- `r` — re-fetch a fresh snapshot.
- `esc` — back to Dashboard and unsubscribe the live stream. `q`/`ctrl+c` quit.
- Note: leaving Logs unsubscribes; reopening fetches snapshot + subscribes
  again. Events arriving while not on the screen are still appended to the
  buffer only while subscribed.

## Account screen
Shows (after load; `loading accounts...` before):
- the account list, one row per stored account: selection marker, `*` for the
  active account, email (plus label when it differs), method, `N% left`,
  `quota exhausted` or `quota unknown`, and `needs sign-in` for a browser
  account requiring re-authentication. Empty: `No accounts on this device.`
- the live Guardian entitlement below the list: `email:`, `uid:` (if
  present), `subscribed: yes/no`, or `subscribed: unknown (<error>)` when the
  online lookup failed (local fields remain valid), `quota:` (`<remaining>
  remaining of <max>` / `<remaining> remaining` / `<max> per period`),
  `bandwidth: limited` when flagged, `token expiry:` (UTC RFC3339).
- pending confirmations: `remove <email>? (y/N)` (or the active/only-account
  wording) and `Sign out of every account? (y/N)` / `sign out and clear the
  stored tokens? (y/N)`.

Keys (idle):
- `↑`/`↓` or `k`/`j` — select an account.
- `u` — make the selected account active (`accounts_use`). Refused while the
  tunnel is up.
- `d` — remove the selected account after y/N. Removing the active account
  selects the most recently used remaining one; removing the last clears the
  local session and disconnects without logout's token revocation.
- `a` — add an account through browser sign-in; `p` — add through the
  password screen. Both open the browser/password flow with `adding` set.
- `R` — re-authenticate the selected account through the browser.
- `n` — rename the selected account (inline field; `enter` saves, `esc`
  cancels).
- `t` — toggle account rotation; `+`/`=` and `-`/`_` change the threshold by
  one within 1–50. The change applies on the next connect.
- `r` — refresh the list and online entitlement (up to 35 s).
- `x` — sign out of **every** account: asks the confirmation; `y`/`Y` confirms
  and issues `logout`; any other key cancels; on success the app returns to the
  Dashboard with notice `signed out`; the daemon clears the account files and
  disconnects. Per-account `d` is different: the others stay signed in.
- `esc` — back. Footer: `↑/↓ select • u use • d remove • a add browser • p add
  password • R sign in again • n rename` / `t rotation • +/- threshold • r
  refresh • x sign out • esc back • ? help • q quit`.
- Browser waiting card: shows `Open this URL in any browser:` and the URL,
  the `Paste the complete redirect URL from the browser:` prompt, an editable
  `Redirect URL:` line and the “can't open this address” note. `enter` submits
  the pasted URL; `esc` cancels through `accounts_add_cancel`. The TUI has no
  browser opener.

## Help overlay (`?`)
Full-screen replacement listing:
- `global       ? help • q / ctrl+c quit`
- `dashboard    space connect/disconnect • l login • o locations • s settings
  • a account • g logs • r refresh`
- `locations    arrows/hjkl move • enter select • P ping all • / filter •
  r refresh • esc back`
- `settings     arrows move • enter change • d reset to default • r refresh •
  esc back`
- `logs         f follow • v level • / search • c clear • e export • esc back`
- `account      r refresh • x sign out • esc back`
- `login        enter submits each step (password masked); esc back`
- footer `press any key to close (q quits)`; `q`/`ctrl+c` quits, any other key
  closes the overlay.

## TUI special states
- States rendered: `UNAUTHENTICATED`, `IDLE`, `CONNECTING`, `CONNECTED`,
  `RECONNECTING`, `WAITING_NETWORK`, `PROXY_ONLY`, `FATAL` (status events
  update live; wait/retry states show yellow).
- Dashboard connect hint changes from `space connect` to `space disconnect`
  in `PROXY_ONLY/CONNECTED/CONNECTING/RECONNECTING/WAITING_NETWORK`; in
  `FATAL` space attempts a fresh connect (TUI has no direct FATAL→IDLE).
- The TUI has **no proxy-only toggle**: `space` always sends
  `ConnectPayload{}` (full tunnel). The CLI's `--proxy-only` has no TUI
  equivalent; a `PROXY_ONLY` state can only be entered from outside the TUI
  (CLI connect `--proxy-only`, or a persisted hidden `proxy_only` override),
  but it is displayed and can be disconnected.
- No mouse support; no editor launching; terminal window resize is tracked
  (affects the logs viewport only).
- Prefs: `theme = "dark"` (default; 256-color fox accents) or `"mono"` (no
  color) from `~/.config/kumiho/tui.toml` (Linux) /
  `%APPDATA%\kumiho\tui.toml` (Windows); env override `KUMIHO_TUI_CONFIG`;
  missing/unreadable file → dark. The TUI only **reads** this file — it writes
  nothing except the log export.

---

# Daemon control surface (internal/api)

Protocol: newline-delimited JSON over the platform transport; every message is
`{"v":1,"id":"...","type":"...","payload":{...}}`. Requests/responses carry
`id`; unsolicited events carry empty `id`. Protocol version is 1 and is
carried in `hello`. Command replies are `result` (`{ok, note?}`) unless stated.

Requests (client → daemon), one line each:

| type | payload → reply | notes |
|---|---|---|
| `hello` | `{client_version?}` → `{version, protocol, os, arch}` | handshake; **no CLI/TUI client uses it** |
| `status` | (none) → `Status{state, authenticated, email?, since, token_expires?, kill_switch, autoconnect, location?, server?, proxy_only?, pass_expires?, quota?{limit?,remaining?,reset?}, dns?, ipv6?, exit_ip?, exit_country?, totals?{up,down}, rates?{up,down}}` | full dashboard snapshot |
| `locations` | `{refresh?}` → `{countries:[{code,name,cities:[{code,name,servers:[{hostname,port,quarantined,protocols:[{name,host,port,scheme}]}]}]}]}` | daemon cache TTL 30 min; failed refresh falls back to cache; error `internal` if no cache |
| `select_location` | `{country_code, city_code?, server}` → `{ok}` | persists without connecting; `bad_request` if country/server empty; broadcast status |
| `ping` | `{hosts:[...]}` → `{ok}` immediately, then one `ping_result{host, rtt_ms|null}` event per host **to the requesting connection only** | TCP probes, 24 concurrent; no batch completion event |
| `login_email` | `{email}` → `login_state{step: email\|password\|2fa\|done, email?, verification_method?, verification_reason?, error?}` | starts the auth state machine |
| `login_password` | `{password}` → `login_state` | derive authPW server-side |
| `login_2fa` | `{code}` → `login_state` | empty code re-checks the email-link flow via session status |
| `logout` | (none) → `{ok}` | clears tokens + disconnects; broadcast status |
| `connect` | `{location_code?, city_code?, server?, proxy_only?}` → `{ok}` | can take ~30 s+; errors `bad_request` / `not_authenticated` / `internal`; broadcast status |
| `disconnect` | (none) → `{ok}` | `bad_request "not connected"` when idle; broadcast status |
| `settings_get` | (none) → `SettingsView{socks_port, mtu, exit_check, doh_provider, dns_upstream?, edge_address?, upstream_proxy?, exclude_cidrs?, allow_lan, log_level, redact_targets, kill_switch("last\|on\|off"), autoconnect("last\|on\|off"), kill_switch_effective, autoconnect_effective, quota_poll_minutes, tls_split("off\|first-byte\|sni"), http_proxy, http_proxy_port, solve_challenges, keepalive_profile("standard\|slow"), overridden?[]}` | no `socks_bind`; no `proxy_only` field |
| `settings_set` | `{key, value, reset?}` → `{ok}` | keys/values below; `unsupported_setting` for unknown keys (e.g. `socks_bind`); `bad_request` on validation; `internal` if a `kill_switch` live re-apply fails (change rolled back); broadcast status |
| `logs_snapshot` | `{level?}` → `{entries:[{ts, level: "debug"\|"info"\|"warn"\|"error", tag, msg}]}` | default level debug (all); ring capacity 2000 |
| `logs_subscribe` | `{level?}` → `{ok}` then a live `log` event stream on this connection until unsubscribe/disconnect | default level info; a second subscribe replaces the connection's previous one |
| `logs_unsubscribe` | (none) → `{ok}` | stops the stream |
| `logs_clear` | (none) → `{ok}` | clears the daemon's retained ring |
| `account` | (none) → `AccountInfo{email?, uid?, subscribed, max_bytes?, limited_bandwidth?, quota_remaining?, token_expires?, error?}` | best-effort Guardian lookup (30 s daemon-side); local fields valid on lookup failure |
| `shutdown` | (none) → `{ok, note:"shutting down"}` | **admin only** (root on Linux, Administrators on Windows → else `forbidden`); triggers daemon exit. Not exposed by any CLI/TUI command |
| (unknown) | → error `bad_request "unknown request type ..."` | |

Error replies: type `error` with `RemoteError{code, message}`. Codes:
`bad_request`, `not_implemented` (declared, unused), `not_authenticated`,
`forbidden`, `unsupported_setting`, `internal`.

Events (server → client, empty id):
- `status` — full `Status`, broadcast to every connection on any visible
  change (state, settings, selection, metrics every 2 s while connected).
- `ping_result` — `{host, rtt_ms|null}` per probed host, only to the
  requesting connection.
- `log` — one `logging.Entry` while subscribed.

Runtime setting keys accepted by `settings_set` (all persisted as overrides in
`state.json`, `reset:true` drops the override):
`kill_switch` (on|off|last), `autoconnect` (on|off|last), `proxy_only`
(on|off|last — **hidden**: not in `SettingsView`, not in the TUI),
`exit_check`/`redact_targets`/`allow_lan`/`http_proxy`/`solve_challenges`
(on|off/true/false/yes/no),
`log_level` (debug|info|warn|error), `doh_provider`
(automatic|cloudflare|google|quad9|off), `dns_upstream` (IP or empty),
`edge_address` (IP or empty), `upstream_proxy` (URL or empty),
`exclude_cidrs` (comma-separated CIDRs), `mtu` (576–65535), `socks_port`
(1–65535), `quota_poll_minutes` (1–1440), `tls_split` (off|first-byte|sni),
`http_proxy_port` (1–65535), `keepalive_profile` (standard|slow).
`socks_bind` and any other key → `unsupported_setting`.

Gap flags (daemon-side abilities beyond the IPC surface):
- **doctor**: no request type — CLI/TUI/Windows GUI run `internal/doctor`
  in-process. A GUI that cannot link the package needs a new request.
- **cleanup**: no request type; requires OS privilege. Needs an elevated
  subprocess or a new privileged request.
- **service install/start/stop/uninstall**: not in IPC at all (installer
  scripts, systemd/SCM only).
- **foreground `kumiho daemon`**: the process itself; not an IPC operation.
- **`shutdown`**: exists in IPC (admin-only) but is exposed by neither the CLI
  nor the TUI; the GUI could call it, but the current Windows tray
  deliberately has no shutdown verb.
- **`hello`**: exists in IPC but unused by CLI/TUI; a GUI can read the daemon
  version via it with no protocol change.
- **proxy-only as a first-class setting**: connectable via the `connect`
  payload (CLI parity exists; TUI/current GUI do not offer it) and settable
  via the hidden `proxy_only` settings key, but `SettingsView` has no
  `proxy_only` field, so a Settings UI cannot display/restore it from
  `settings_get` without a field added to `SettingsView`.
- **log export**: purely client-side; the daemon provides entries only (no
  file-write request) — every client must implement its own export.
- **no config-file surface**: `config.toml` (admin defaults) is hand/installer
  edited; no IPC reads or writes it. Not needed for CLI/TUI parity, but a GUI
  cannot show raw admin defaults — only effective values and override keys.

---

# Cross-cutting abilities

## Auth / token handling
- Login is always daemon-mediated. The password flow uses
  `login_email`/`login_password`/`login_2fa`; the browser flow uses
  `login_start`, `login_status` and `login_cancel` (add/re-authenticate use
  the `accounts_*` aliases). Clients never see a password derivation, and no
  client stores tokens.
- Password steps: email → password → optional 2FA (`email` link method or
  emailed code/TOTP/unblock) → done. `login_state.error` carries friendly
  messages (wrong password, link-not-confirmed, anti-bot challenge wording,
  timeouts). The CLI caps a password attempt loop at 12.
- Browser steps: `login_start` returns `{method, login_id, url}` with step
  `browser`. Clients open the URL, ask for the complete redirect URL from the
  browser's address bar, send it with `login_complete`, and poll
  `login_status(login_id)` until `done`/`error`, or call `login_cancel`. The
  daemon runs a direct Firefox Accounts OAuth flow with PKCE and `state`, no
  listener and a ten-minute deadline. The redirect URL, code, verifier and
  tokens are never logged. Captcha, password, TOTP and unblock codes are
  handled by Mozilla's page, never automated by Kumiho.
- Signing in while an account is active adds/switches on completion; there is
  no sign-out-first requirement. Only another in-flight login is refused. The
  active account cannot change while the tunnel is up (`accounts_use` returns
  `disconnect first; switching accounts needs the tunnel off`).
- `accounts.json` v2 is authoritative and lives beside the token path (mode
  0600, atomic replace). It stores `version`, `active`, `last_method` and up
  to five accounts with `id`, `email`, `label`, `method`, ordinary
  `fxa{access_token, refresh_token, expires_at, scope}` tokens, a durable
  top-level `reauth_required`, `added_at`/`last_used_at` and an optional
  `last_error`. The FxA uid is not persisted. The legacy
  `browser{token, expires_at, reauth_required}` field is decode-only: engine
  start removes accounts that only have it, and the notice is **“Old browser
  sign-in removed. Add your Firefox account again.”**
- `tokens.json` is the legacy layout and is migrated to `accounts.json` on
  first load. It remains a synchronized rollback copy of the active account's
  `{access_token, refresh_token, expires_at, scope}` tokens (mode 0600) and is
  rewritten on login, active switch, refresh and account removal. Old clients
  can read it until logout. A malformed/missing `accounts.json` is reported
  without overwriting `tokens.json`, so the previous release can recover.
- FxA tokens are treated invalid 60 s before expiry and refreshed
  automatically at daemon start, before connect, on Guardian 401/403, and on
  Account lookups for both methods. A permanent refresh rejection marks only a
  **browser** account: it sets the durable top-level `reauth_required`, keeps
  the account listed, and requires `accounts add --account <id|email>` (or the
  row action) to sign in again. Password accounts never set that flag: an
  active rejection removes that account (or clears the session when it was the
  last), and a quota-poll rejection is retried with backoff.
- Rotation is opt-in (`account_rotation`, default off) and before connect
  only: below `rotation_threshold` (default 10, clamp 1–50) the engine
  picks the best eligible account, preferring higher remaining quota and then
  the least recently used account. Rejected and sign-in-required accounts are
  skipped with a notice; if every account is exhausted or unusable, connect
  fails without opening the tunnel. Rotation never switches mid-session.
- Quota polling while connected uses `quota_poll_minutes` (default 15); a
  success updates Account/status, failures back off exponentially to a
  30-minute cap, and a 429 presents zero remaining without disconnecting. A
  rejected **browser** refresh token stops that account's poller and sets
  `reauth_required`; a rejected password refresh token is an ordinary poll
  failure that backs off and retries without the flag.
- Logout (`logout`, TUI `x` with y/N confirmation, CLI `logout`) best-effort
  revokes every stored FxA refresh token, clears **all**
  accounts, removes `accounts.json` and `tokens.json`, disconnects and writes
  the empty email back to state. An offline or failed revoke never blocks the
  local sign-out. `accounts_remove` removes one row only:
  removing the active row selects the most recently used remaining account,
  and removing the last row clears the local session and disconnects without
  the refresh-token revocation that `logout` performs.
- Windows pipe access is the real auth boundary (LocalSystem, Administrators,
  local `kumiho` group, installer allowlist in `pipe-users`); Linux socket is
  0660 root/`kumiho` group. `shutdown` additionally requires admin/root.

## Config editing (which settings exist, values, defaults)
`config.toml` keys and built-in defaults (PLAN.md §8 / `DefaultConfig`):
`socks_bind="127.0.0.1"` (fixed, not runtime-changeable), `socks_port=1080`,
`proxy_only=false`, `kill_switch="last"`, `autoconnect="last"`,
`exit_check=true`, `doh_provider="automatic"`, `dns_upstream=""` (provider
default), `edge_address=""` (resolve), `upstream_proxy=""` (direct),
`exclude_cidrs=[]`, `allow_lan=true`, `mtu=8500`, `log_level="info"`,
`redact_targets=true`, `quota_poll_minutes=15`, `account_rotation=false`,
`rotation_threshold=10`, `tls_split="off"`, `http_proxy=false`,
`http_proxy_port=8080`, `solve_challenges=true`,
`keepalive_profile="standard"`.
- `account_rotation` and `rotation_threshold` are edited from the CLI
  (`accounts rotation`/`accounts threshold`), the TUI Account screen and the
  GUI/Android Account cards; they are not TUI Settings rows. The threshold is
  clamped to 1–50 and the setting applies on the next connect.
- Runtime edits from CLI/TUI are persisted as **overrides** in
  `state.json` (`/var/lib/kumiho/state.json` /
  `%ProgramData%\kumiho\state.json`) via `settings_set`; `reset` removes one
  override so config/built-in applies again. The TUI marks overridden rows
  with `*`.
- Effective values = admin config + override. First install kill switch and
  autoconnect are ON (`last` + null state → true).
- Apply timing: `kill_switch` (live re-apply of netcfg; failure rejects and
  rolls back), `log_level`, `redact_targets`, `solve_challenges` immediately;
  `autoconnect` at the next daemon start; every other connection-scoped key at
  the next connect.
- Clients cannot edit `config.toml` through IPC; it is edited by hand/installer.
- State also persists: selected location/city/server, email, per-server dial
  failure counts, remembered kill switch/autoconnect/proxy_only.

## Logs (sources, follow/streaming, export)
- Source of truth: daemon in-memory ring (2000 entries) plus a text sink —
  stderr → journald on Linux, `%ProgramData%\kumiho\logs\kumiho.log` on
  Windows (rotates to `.1` at 5 MiB on service start; admins-only directory).
- Levels debug/info/warn/error with 3-letter tags DBG/INF/WRN/ERR; line format
  `RFC3339 TAG3 [tag] msg` (`Entry.Line()`).
- Redaction always masks JWTs, long hex tokens and emails; when
  `redact_targets` is on (default), IPv4 addresses are masked too.
- CLI: `logs` snapshot, `logs -f` follow until Ctrl+C. TUI: level filter,
  search, follow toggle, scrollback, clear, export. All over
  `logs_snapshot`/`logs_subscribe`/`logs_unsubscribe`/`logs_clear`.
- Export is client-side (TUI `e`): visible filtered entries →
  `kumiho-logs-<timestamp>.log` under `~/.local/state/kumiho`
  (`%LOCALAPPDATA%\kumiho\logs` on Windows).

## Diagnostics / doctor
- Checks and output format are in the `kumiho doctor` section above. Statuses
  `ok|warn|fail|skip`; remediation shown for warn/fail. Exit code 1 if any
  fail; `--json` for machine consumption. Runs entirely in-process.
- Docs and remediation text also point at platform commands:
  `journalctl -u kumiho -f`, `Get-Service kumiho`, `resolvectl status foxy0`,
  `Get-DnsClientServerAddress -InterfaceAlias foxy0`, `kumiho cleanup`.
- Windows GUI parity: existing GUI renders the same report with plain language
  (`internal/gui/app/doctorview`) plus Copy details / Open log folder; a
  protocol request does not exist (flagged above).

## Locations (list, search/filter, selection, ping)
- List: Remote Settings server list; one flat server list with `REC`
  (Recommended Location) first, then countries A–Z, cities A–Z and hostnames
  A–Z; quarantined and targetless servers excluded; `CatchAll Anycast` records
  dropped. Daemon caches 30 min; `refresh` bypasses; failures fall back to the
  cache with a warning log.
- Selection: `select_location` persists country/city/server without
  connecting; shown as `*` in the TUI and used by the next connect (or
  auto-picked after 3 consecutive connect failures for that server).
- Filtering: TUI `/` matches country name/code, city name/code, hostname,
  `host:port` and protocol names (client-side). CLI `--to` accepts country
  code, city code, server host or `host:port`, host prefix, or unique
  country-name substring (client-side matching; the daemon accepts
  `location_code`/`city_code`/`server` directly).
- Ping: daemon TCP probes streamed per host (`ping_result`); CLI waits ≤6 s and
  prints ms/unreachable/no result; TUI Ping all probes every usable server and
  shows RTT or `timeout`, then switches to speed order once at completion. No
  ping history or cancellation.

## Connect / disconnect semantics and states
- States: `UNAUTHENTICATED`, `IDLE`, `CONNECTING`, `CONNECTED`, `RECONNECTING`,
  `WAITING_NETWORK`, `PROXY_ONLY`, `FATAL`. `since` tracks the last transition.
- Connect (full tunnel or `proxy_only`): requires a session, refreshes the
  access token, fetches locations, resolves the candidate (explicit payload →
  persisted selection → `REC` random → first country), acquires a Guardian
  pass (401/403 → refresh token → activate → retry), dials up to 4 attempts
  with 2–8 s full-jitter backoff alternating edges (same city then same
  country), persists the chosen selection, starts the data path (TUN + netcfg
  + DNS + SOCKS5, or SOCKS5 only), then starts metrics (2 s), pass renewal,
  watchdog, and an exit check when `exit_check` is on.
- Errors: not signed in → `not_authenticated "sign in first (kumiho login)"`;
  already connected/connecting → `bad_request "already connected; disconnect
  first"`; unknown server/location → `bad_request` with the refresh hint;
  quota exhausted → `FATAL` ("the account's VPN quota is exhausted; the tunnel
  stays off until the quota resets"); permanently rejected session → `FATAL`
  ("Mozilla rejected the account session; sign in again"); anti-bot challenge →
  clear error, no solver; other failures return to `IDLE` and log
  `connect failed: <err>` (plus per-attempt `dial ... attempt N/4 failed`).
- Disconnect: tears down DNS, netcfg (before device removal), TUN, SOCKS;
  returns to `IDLE` (or stays `UNAUTHENTICATED`); idempotent for `FATAL`;
  `not connected` error when idle; last session totals remain visible with
  zero rates.
- Watchdog (2 s poll): on session loss → `RECONNECTING`, redial with 3–60 s
  full-jitter, rotate edge every 2 failures; if the physical network has no
  default route it parks in `WAITING_NETWORK` and resumes on link/route events;
  quota/token fatals → `FATAL` stop. While down, the kill switch blocks
  unmarked traffic (documented user-visible behavior).
- Failure policy: a dial failure with a live route increments the server's
  failure count (cap 9, map 32); 3 consecutive failures clear the persisted
  selection so the next connect auto-picks; success clears the count.
- Pass renewal: swap the bearer in place at half remaining lifetime (clamped
  15 s–30 min, max expiry−30 s, 4 min fallback), retry every 30 s, fatal on
  fatal errors; quota headers refresh on each renewal and notify clients.
- Exit check: once per connect through the local SOCKS at
  `https://www.cloudflare.com/cdn-cgi/trace`; populates `exit_ip`/
  `exit_country`; a country mismatch only logs a warning; failures are
  non-fatal.
- Autoconnect (daemon start): when effective setting is on and a session was
  restored; retries at 5 s, 10 s, 30 s, 30 s, 60 s; stops on user-actionable
  errors (`RemoteError`/fatal) or after the schedule.
- Kill switch live toggle on a connected full tunnel re-applies netcfg;
  failure rejects the setting change and keeps the old value.

## Service management (install/start/stop/uninstall/cleanup), platform differences
- No CLI or TUI commands manage the service; mechanisms are installers and the
  OS service manager only.
- Linux: `sudo ./install.sh` (idempotent): user `kumiho`, binary
  `/usr/local/bin/kumiho`, systemd unit `/etc/systemd/system/kumiho.service`
  (CAP_NET_ADMIN, `Restart=on-failure`, RuntimeDirectory/StateDirectory),
  polkit rule for resolved, default `/etc/kumiho/config.toml`, enable + group
  membership. `sudo ./install.sh uninstall [--purge]`. Start/stop:
  `sudo systemctl start|stop|restart kumiho`, logs via `journalctl -u kumiho`.
- Windows: `install.cmd` (self-elevating) or `install.ps1` (Administrator
  shell): local group `kumiho`, binaries + `wintun.dll` in `%ProgramFiles%
  \Kumiho`, Start Menu shortcut, machine PATH, commented
  `%ProgramData%\kumiho\config.toml`, service `kumiho` (LocalSystem,
  auto-start, recovery 5 s/30 s/60 s), pipe grant. Options `-Binary PATH`,
  `-User NAME`; `-Uninstall`, `-Uninstall -Purge`. Service control:
  `Get-Service`/`Start-Service`/`Stop-Service`/`Restart-Service kumiho`.
  Uninstall keeps `%ProgramData%\kumiho` and `%APPDATA%\kumiho` unless purged.
- Cleanup: `kumiho cleanup` (root/Administrator) removes stale routes, rules,
  nftables, DNS link config and (Windows) the `foxy0` adapter; the daemon also
  runs startup cleanup, and a clean stop tears everything down. Only one
  daemon may run (TUN + port 1080 + socket); direct traffic vs kill switch
  caveat documented.
- Dev: `scripts/dev-env.sh`/`.ps1` (KUMIHO_* env overrides),
  `scripts/dev-daemon.sh start|stop|log` (Linux, root, file log),
  `scripts/watchdog-retest.sh` (live heal check).

## Version / update info display
- `kumiho version` prints the client build string (`Version`, git commit,
  build date) and never contacts the daemon; there is no update check,
  no version comparison and no self-update anywhere.
- The daemon's version is available over IPC (`hello` → `version, protocol,
  os, arch`) but unused by CLI/TUI; protocol version 1 is implied by every
  envelope `v` field.

## Files/state written outside the daemon
- TUI: log export files (paths/formats above); reads `tui.toml` only.
- CLI: nothing (stdout/stderr only).
- `kumiho cleanup`/daemon teardown: system networking state (routes, policy
  rules, nft tables, resolved link config, Windows adapter/routes/WFP filters)
  — outside both the daemon and IPC.
- Daemon-owned (for reference): `accounts.json` (v2 account store),
  `tokens.json` (synchronized rollback copy of the active account; migrated
  from legacy installs and removed on logout), `state.json`, `config.toml`
  (read-only), log sink, control socket/pipe, `pipe-users` (installer).
  `accounts.json` and `tokens.json` are rewritten atomically with 0600 on
  Unix; on Windows the `%ProgramData%\kumiho` ACL is the boundary. The
  Android app uses `noBackupFilesDir/accounts.json` and `tokens.json`.
- API clients (GUI/Android): `gui.toml` is Windows GUI-only; Android owns its
  DataStore preferences and split-tunneling file. Neither stores passwords.
- Windows GUI (existing, outside CLI/TUI scope): `%APPDATA%\kumiho\gui.toml`
  (theme, accent, geometry, autostart, onboarding), log exports under
  `%LOCALAPPDATA%\kumiho\logs`.
