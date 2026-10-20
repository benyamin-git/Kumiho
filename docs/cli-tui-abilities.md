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

## `kumiho login` — interactive Firefox Accounts sign-in
- Invocation: `kumiho login` (no flags). Daemon required (the daemon runs the
  login state machine).
- Interactive terminal prompts via `golang.org/x/term`:
  - `Email: ` (plain read)
  - `Password: ` (hidden read)
  - `Code: ` (plain read) for the 2FA step
- Flow: `login_email` → `login_password` → optional `login_2fa` → `done`.
  - 2FA with `verification_method == "email"` (link flow): prints `Check your
    email and open the sign-in link, then press Enter (leave the code empty).`
    Otherwise prints `Enter the code from the email you received.`
  - Success prints `Signed in as <email>.` to stdout, exit 0.
  - Errors: `login: <message>` / `kumiho login: <err>` to stderr; loop bounded
    at 12 attempts → `kumiho login: too many attempts`, exit 1.
- Exit codes: 0 / 1 / 2 (flag parse).
- Interactivity: prompts, no editor launching.

## `kumiho logout`
- Invocation: `kumiho logout`. Daemon required (`logout` request).
- Clears stored tokens, disconnects the tunnel and returns to
  UNAUTHENTICATED. Prints `Signed out.` on success. Failure
  `kumiho logout: <err>`, exit 1.

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
Steps: `email` → `password` → `2fa` → done; the daemon state machine drives
step transitions (`login_state.step`).
- Shows: title `Sign in to Firefox Accounts`; email (dimmed once past the
  email step), password masked as `•` per rune with a trailing `_` cursor,
  2FA prompt with method-specific instructions (email-link text or
  "Enter the code from the email you received."), `contacting Mozilla
  Accounts...` while busy, red error line, footer `enter continue • esc cancel`.
- Keys: printable runes append to the active field (email / password / code);
  `backspace` edits; `enter` submits (validation: empty email →
  `email is required`; empty password → `password is required`; empty 2FA code
  is allowed and re-checks the email-link flow); `esc` cancels back to the
  Dashboard clearing password/code/error; `ctrl+c` quits.
- On `step=done` without error: back to Dashboard, notice
  `signed in as <email>`, password/code cleared, status refreshed. Server-side
  errors (wrong password, link not confirmed, challenge, …) show in the error
  line; the TUI imposes no attempt cap (the CLI caps at 12 submit rounds).
- The daemon refuses a second sign-in while signed in: `already signed in;
  log out first to switch accounts`.

## Locations screen
Shows a tree: countries (`Name (CODE) - N cities`, `REC` first), cities nested
under expanded countries (`City (code)`), servers under expanded cities
(`* host:port` plus `  <n> ms` in green or `  timeout` dimmed when pinged).
`*` marks the persisted selection (country row if it matches `status.location`,
server row if it matches `status.server`). `(no locations loaded)` when empty;
`loading locations...` while fetching; error line on failure. A filter line
`filter: <text>_` appears while filtering or when a filter is set.
Keys:
- arrows / `k` `j` — move cursor over visible rows.
- `enter` — on a country toggles expansion; on a city toggles expansion; on a
  server persists it via `select_location` (country code, city code,
  `host:port`) — **does not connect**; success notice `location saved`,
  failure `selection failed: <err>` (status refreshed either way).
- `p` — TCP-ping every **visible** server row (only expanded, filter-matched
  servers); results stream in and update rows live.
- `/` — filter mode: type to filter by country name or code (not city names;
  case-insensitive substring), `backspace` edits, `enter` keeps the filter and
  leaves filter mode, `esc` clears the filter, `ctrl+c` quits.
- `r` — force server-list refresh (`--refresh`).
- `esc` — back to Dashboard. Footer: `enter expand/select • p ping • r refresh
  • / filter • esc back • ? help`.
- On first successful load the persisted selection is auto-expanded (country
  and the city containing the selected server).
- Empty filter shows all countries collapsed except auto-expanded ones; filter
  rows are re-evaluated live and the cursor resets to the top.

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
Shows (after load; `loading account...` before):
- not signed in → `not signed in — press l on the dashboard to sign in`
- otherwise `email:`, `uid:` (if present), `subscribed: yes/no`, or
  `subscribed: unknown (<error>)` when the online Guardian lookup failed
  (local fields remain valid), `quota:` (`<remaining> remaining of <max>` /
  `<remaining> remaining` / `<max> per period`), `bandwidth: limited` when
  flagged, `token expiry:` (UTC RFC3339).
- Confirmation line while pending: `sign out and clear the stored tokens?
  (y/N)`.
Keys:
- `r` — refresh (online entitlement lookup; up to 35 s).
- `x` — sign out (only when loaded and an email is present): asks the
  confirmation; `y`/`Y` confirms and issues `logout`; any other key cancels;
  on success the app returns to the Dashboard with notice `signed out`; the
  daemon clears tokens and disconnects.
- `esc` — back. Footer: `r refresh • x sign out • esc back • ? help • q quit`.

## Help overlay (`?`)
Full-screen replacement listing:
- `global       ? help • q / ctrl+c quit`
- `dashboard    space connect/disconnect • l login • o locations • s settings
  • a account • g logs • r refresh`
- `locations    arrows/hjkl move • enter expand/select • p ping • / filter •
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
- Login is always daemon-mediated (`login_email`/`login_password`/`login_2fa`);
  clients never see the password derivation. CLI prompts interactively;
  TUI/CLI/GUI can all drive the same state machine.
- Steps: email → password → optional 2FA (`email` link method or emailed code)
  → done. `login_state.error` carries friendly messages (wrong password,
  link-not-confirmed, anti-bot challenge wording, timeouts).
- Already signed in: `login_email` returns step `done` with error
  `already signed in; log out first to switch accounts`.
- Tokens live only daemon-side: `/var/lib/kumiho/tokens.json` mode 0600
  (Linux) / `%ProgramData%\kumiho\tokens.json` (Windows), fields
  `{access_token, refresh_token, expires_at, scope}`. Clients never read them.
- Access token: treated invalid 60 s before expiry; refreshed automatically at
  daemon start, before connect, on Guardian 401/403, and on Account lookups;
  permanent refresh rejection clears the session (sign-in required again).
- Logout (`logout`, TUI `x` with y/N confirmation, CLI `logout`) disconnects
  and removes tokens; the daemon writes the empty email back to state.
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
`redact_targets=true`, `quota_poll_minutes=15`.
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
- List: Remote Settings server list; `REC` (Recommended Location) first, then
  alphabetical; cities and servers nested; quarantined servers excluded;
  `CatchAll Anycast` records dropped. Daemon caches 30 min; `refresh`
  bypasses; failures fall back to the cache with a warning log.
- Selection: `select_location` persists country/city/server without
  connecting; shown as `*` in the TUI and used by the next connect (or
  auto-picked after 3 consecutive connect failures for that server).
- Filtering: TUI `/` filters countries by name/code (client-side). CLI `--to`
  accepts country code, city code, server host or `host:port`, host prefix,
  or unique country-name substring (client-side matching; the daemon accepts
  `location_code`/`city_code`/`server` directly).
- Ping: daemon TCP probes streamed per host (`ping_result`); CLI waits ≤6 s and
  prints ms/unreachable/no result; TUI pings visible servers and shows RTT or
  `timeout` live. No ping history or cancellation.

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
- Daemon-owned (for reference): `tokens.json`, `state.json`, `config.toml`
  (read-only), log sink, control socket/pipe, `pipe-users` (installer).
- Windows GUI (existing, outside CLI/TUI scope): `%APPDATA%\kumiho\gui.toml`
  (theme, accent, geometry, autostart, onboarding), log exports under
  `%LOCALAPPDATA%\kumiho\logs`.
