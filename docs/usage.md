# Using Kumiho

Everything talks to the daemon over local IPC: `/run/kumiho/control.sock` on
Linux and the named pipe `\\.\pipe\kumiho\control` on Windows. Start it with
`sudo systemctl start kumiho` (installed Linux), `Start-Service kumiho`
(installed Windows) or `scripts/dev-daemon.sh start` (Linux development).

The Android app does not use this daemon or IPC: the engine is embedded in the
app process. Its guide is [android.md](android.md).

## CLI

    kumiho                      # TUI (default)
    kumiho daemon               # run the daemon in the foreground
    kumiho login                # browser or password Firefox Accounts sign-in
    kumiho login --browser      # print/open the URL, then paste the redirect URL
    kumiho login --password     # email → password → 2FA/TOTP code
    kumiho accounts             # list accounts and the active one
    kumiho accounts add [--browser|--password] [--account <id|email>] [--no-open]
    kumiho accounts use <id|email>
    kumiho accounts remove <id|email> [--yes]
    kumiho accounts rename <id|email> <label>
    kumiho accounts rotation on|off
    kumiho accounts threshold <1-50>
    kumiho logout               # clear every account; disconnects
    kumiho connect [--to CODE|CITY|HOST] [--proxy-only] [--kill-switch=on|off|last]
    kumiho disconnect
    kumiho status [--json]      # state, location, quota, exit IP, rates
    kumiho locations [--ping] [--json] [--refresh]
    kumiho logs [-f] [--level debug|info|warn|error]
    kumiho doctor [--json]      # preflight diagnostics + remediation
    kumiho cleanup              # remove stale routes/rules/DNS artifacts
    kumiho version

`--proxy-only` keeps system routing untouched and only starts the SOCKS5
listener on `127.0.0.1:1080`; the full tunnel also creates `foxy0` and routes
TCP through it, with DNS carried as DoH over HTTPS inside the tunnel.

## TUI

`space` connect/disconnect, `o` locations, `s` settings, `a` account,
`g` logs, `l` login, `r` refresh, `?` help overlay, `q` quit. Inside screens:
arrows/`hjkl` move, `enter` select/change, `esc` back.

- **Locations** — `REC` first and country rows carry flags; `p` pings the
  visible servers, `P` pings every usable server in the loaded list, `x`
  expands/collapses every country and city, `/` filters; `enter` expands a
  country or selects a server (persisted). A server that fails to connect 3
  times in a row is dropped and the next connect auto-picks (`REC` random,
  else the first country).
- **Settings** — every runtime setting: kill switch, autoconnect, log level,
  redaction, exit check, DoH provider, custom DNS, pinned edge, upstream
  proxy, excluded CIDRs, allow-LAN, MTU, SOCKS port, quota poll, TLS split,
  HTTP proxy and its port, challenge solving, keepalive profile. `d` resets a
  row to the config default; `*` marks values changed from `config.toml`.
  The row list keeps a viewport and scrolls in short terminals (`pgup`/
  `pgdown`, `home`/`end`).
  Changes are persisted as overrides in `/var/lib/kumiho/state.json`
  (`%ProgramData%\kumiho\state.json` on Windows).
  `kill_switch`, `log_level`, `redact_targets` and `solve_challenges` apply
  immediately; connection-scoped settings (DNS, MTU, CIDRs, TLS split, the
  proxies, keepalive, …) apply on the next connect.
  Toggling `kill_switch` on a connected tunnel re-applies the live policy; if
  that re-apply fails the change is rejected with an error and the previous
  value is kept.

  The default MTU is 8500. `foxy0` feeds a gVisor userspace network stack
  that handles packets of that size itself, and Kumiho has no mobile kernel
  VPN path, so the reference app's mobile 1400 rationale does not apply. The
  value is configurable (576–65535) and a change applies on the next connect.
- **Logs** — the daemon's in-memory ring plus live streaming; `v` cycles the
  level filter, `/` searches, `c` clears, `f` toggles follow, `e` exports the
  visible entries to `~/.local/state/kumiho/kumiho-logs-<timestamp>.log`
  (`%LOCALAPPDATA%\kumiho\logs` on Windows).
- **Account** — the five-account list (active marker, method, quota, re-auth
  warning) above the live Guardian entitlement. `↑/↓` select, `u` use, `d`
  remove, `a` add through the browser, `p` add with a password, `R` sign the
  selected account in again (browser), `n` rename, `t` toggle rotation,
  `+`/`-` change the threshold by one, `r` refresh and `x` sign out of every
  account (with confirmation). The login screen offers `b` browser and `p`
  password when no login is in progress.

## Sign-in and accounts

`kumiho login` with a terminal asks for a method the first time: **browser**
(recommended) or **password**. It remembers the last successful method, but the
browser remains the default until a password sign-in succeeds. Without a
terminal, `kumiho login` uses the password flow for script compatibility; pass
`--browser` explicitly to use the browser.

**Browser sign-in** starts a daemon-owned Firefox Accounts OAuth flow with
PKCE, `state` and the VPN scope. It prints (and, unless `--no-open` is given,
opens) a `https://accounts.firefox.com/authorization?...` URL, then prompts
`Redirect URL:` on stdin. Complete captcha, password, TOTP or unblock
challenges in the browser. Mozilla redirects to a
`urn:ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel?...` address, so the
browser shows its “can't open this address” page — that is expected. Copy the
complete URL from the browser's address bar and paste it at the prompt.
Kumiho exchanges the code, fetches the Firefox Accounts profile for the
UID/email and stores ordinary FxA access and refresh tokens; later refreshes
work like a password login, and a permanently rejected refresh token marks the
account for sign-in again.

There is no loopback listener, so the browser may run on any machine;
`--no-open` is useful over SSH or in a headless shell. The flow expires after
ten minutes and the CLI cancels the daemon-side flow on timeout. The redirect
URL, authorization code, PKCE verifier and tokens are never logged. The TUI
prints the URL (it has no browser opener) and takes the complete **Redirect
URL** in a field submitted with **Enter**; the Windows GUI uses a **Redirect
URL** field with **Continue**. The Android app completes it in its in-app
WebView.

**Password sign-in** sends the password only in the request that uses it; it
is never stored. Email-link 2FA and TOTP/unblock codes are supported. A real
interactive captcha cannot be solved by this flow, so use browser sign-in if
Firefox Accounts presents one.

`kumiho accounts` prints a table with the active marker, label, email, method,
quota and re-authentication state. `accounts add` uses the same browser or
password flow (without `--browser`/`--password`, a terminal asks; a script
uses the password flow). `--account <id|email>` re-authenticates an existing
account and, for browser, replaces its stored FxA tokens. Re-authentication
uses the stored email; signing in to a different account during that flow is
refused and the new tokens are discarded. `accounts use`
switches the active account and refuses while the tunnel is up. `accounts
remove` removes one account; when it was the active one the engine selects the
most recently used remaining account. `accounts rename` replaces the label.
There is no `accounts update` CLI verb, but clients use the `accounts_update`
IPC command for the same label change; an empty label from a client resets it
to the email. Up to five accounts can be stored; labels are preserved when the
same email is signed in again. Adding a new account with an email that matches
an existing one replaces its tokens and keeps the label.

On the first engine start after upgrading from v0.6.0, accounts created by the
old opaque browser sign-in are removed with the notice **“Old browser sign-in
removed. Add your Firefox account again.”** Password accounts and settings are
untouched; add the Firefox account again with the browser or password flow.

**Rotation** is off by default. `kumiho accounts rotation on` enables it and
`kumiho accounts threshold <1-50>` sets the percentage (default 10).
`accounts rotation on|off` leaves the threshold unchanged. Rotation evaluates
accounts only at connect, never mid-session:

- an account at or above the threshold keeps the active role;
- otherwise the engine picks the highest known remaining quota, using the
  least recently used account to break ties;
- unknown quotas are tried after known eligible accounts;
- browser accounts whose refresh token was permanently rejected, rejected
  tokens and accounts needing sign-in are skipped with a notice, and password
  tokens are refreshed once;
- if every account is at zero quota or needs sign-in, connect fails with the
  account names instead of opening a tunnel.

The Account notice appears in the TUI/GUI/Android and in `accounts list` as
the `notice` field. Rotation never changes accounts under live streams.

**Quota polling** runs while connected at `quota_poll_minutes` (default 15,
range 1–1440). A success updates the active account's quota in the status and
Account snapshot. Failures back off exponentially from the configured interval
to a 30-minute cap; they never disconnect the tunnel. A browser account whose
refresh token is permanently rejected gets the durable `reauth_required` flag
and stops that poller; re-authenticate the account to clear it. A 429 is shown
as zero remaining and does not drop the connection.

`kumiho logout` asks Firefox Accounts to revoke every stored refresh token
(best effort, five seconds), clears every account (browser and password),
removes the token files and disconnects. An offline or failed revoke never
blocks the local sign-out. Removing one account leaves the others signed in and
does not revoke their tokens. Sign-out does not delete your Firefox Account;
you can sign in again at any time.

Browser sign-in uses the project's existing Firefox OAuth client and Mozilla's
account page; this and automatic account rotation are a ToS gray area for an
unofficial client. See [PLAN.md §13](../PLAN.md#13-risks-and-mitigations) and
the [README](../README.md) before using multiple accounts.

## v0.4.0 settings

The five settings added in v0.4.0, plus the now functional `upstream_proxy`,
are edited in the TUI settings screen (`s`) and in the Windows GUI's Settings
screen; there is no CLI settings command. Like the others they are persisted
as per-user overrides and can be set as defaults in `config.toml`.

| Setting | Values | Default | Applies |
|---|---|---|---|
| `tls_split` | `off`, `first-byte`, `sni` | `off` | next connect |
| `http_proxy` | on/off (Settings); `true`/`false` (config.toml) | off | next connect |
| `http_proxy_port` | 1–65535 | 8080 | next connect |
| `solve_challenges` | on/off (Settings); `true`/`false` (config.toml) | on | immediately |
| `keepalive_profile` | `standard`, `slow` | `standard` | next connect |
| `upstream_proxy` | `socks5://`, `socks5h://` or `http://` URL | empty (direct) | next connect |

**`tls_split`** is best-effort TLS ClientHello fragmentation for networks
that filter on the handshake text. `first-byte` writes the first byte of the
record separately; `sni` splits around the first hostname byte. ClientHello
contents are unchanged; the record is split across two TCP writes, and a
malformed or SNI-less ClientHello is not split.

**`http_proxy`** starts a loopback HTTP proxy together with the tunnel. Point
a client at it:

    curl -x http://127.0.0.1:8080 https://example.com

It is loopback-only and unauthenticated, accepts `CONNECT` and absolute-form
plain HTTP with one request per connection, and stops with the tunnel. It
does not intercept TLS: `curl -x` reaches HTTPS through `CONNECT`.
`http_proxy_port` is its listener port (default 8080, range 1–65535).

**`solve_challenges`** makes Kumiho answer Fastly's proof-of-work, `PAT` and
`clientmetrics` challenges for the Firefox Accounts and Guardian control
planes: on a `406` it fetches the challenged page, answers the challenge,
installs the returned cookies and retries the request. A captcha or an
unknown challenge type cannot be solved by this path and fails with a clear
error; browser sign-in lets Mozilla's page handle the captcha. Disable the
solver to get that immediate failure while diagnosing, or if a network
operator objects to the solver traffic.

**`keepalive_profile`** sets the keepalive ping timeout. The ping is sent
after 15 s idle and checked every 3 s in both profiles; an unanswered ping
times out after 10 s on `standard` and 25 s on `slow`. Use `slow` on lossy or
high-latency links.

**`upstream_proxy`** carries the edge connection through a proxy. The scheme
can be `socks5://`, `socks5h://` or `http://`; `user:pass@` is optional; the
default port is 1080 for both SOCKS schemes and 80 for HTTP. The proxy
resolves the edge hostname, so there is no local lookup for it. Only the edge
dial uses the hop: the control plane, Remote Settings, DoH and bootstrap stay
direct, and the local SOCKS5 and HTTP listeners are unaffected. Both SOCKS
schemes hand the hostname to the proxy unresolved, as does HTTP `CONNECT`.

For example, one of:

    # SOCKS5h with authentication
    upstream_proxy = "socks5h://user:pass@proxy.example.net:1080"

    # plain HTTP proxy
    upstream_proxy = "http://proxy.example.net:3128"

Replace the placeholder credentials with your own.

## DNS over SOCKS5 UDP

The local SOCKS5 listener accepts `UDP ASSOCIATE` for DNS only: the request's
target port must be 53 (`0` means any), and each datagram must carry a DNS
query sent to port 53. It works in both full and proxy-only mode, and the
queries use the same strict DoH chain as the TUN resolver. Non-DNS UDP,
foreign sources and fragmented datagrams are dropped on purpose. A client
that sends DNS through the SOCKS5 proxy rather than the system resolver needs
this UDP associate path; in full mode, ordinary DNS through the tunnel still
works through the local resolver, including TCP.

## Files

| Path | Contents |
|---|---|
| `/etc/kumiho/config.toml` | admin defaults (all keys optional) |
| `/var/lib/kumiho/accounts.json` | v2 account store: active account, last method, up to five labeled browser/password accounts with FxA tokens (mode 0600) |
| `/var/lib/kumiho/tokens.json` | rollback copy of the active account's tokens (mode 0600); migrated from legacy installs and removed on logout |
| `/var/lib/kumiho/state.json` | selection, failure counts, remembered kill-switch/autoconnect, runtime overrides |
| `/var/lib/kumiho/serverlist.json` | cached server list; only used when a fetch fails |
| `/run/kumiho/control.sock` | IPC socket (0660, group `kumiho`) |
| `~/.config/kumiho/tui.toml` | per-user TUI prefs (`theme = "dark"` or `"mono"`) |

On Windows the machine-wide state lives under `%ProgramData%\kumiho` (ACL:
LocalSystem and Administrators) and the control endpoint is a named pipe:

| Path | Contents |
|---|---|
| `%ProgramData%\kumiho\config.toml` | admin defaults (all keys optional) |
| `%ProgramData%\kumiho\accounts.json` | v2 account store (active id, last method, up to five accounts) |
| `%ProgramData%\kumiho\tokens.json` | rollback copy of the active account's tokens; removed on logout |
| `%ProgramData%\kumiho\state.json` | selection, failure counts, remembered kill-switch/autoconnect, runtime overrides |
| `%ProgramData%\kumiho\serverlist.json` | cached server list; only used when a fetch fails |
| `%ProgramData%\kumiho\logs\kumiho.log` | daemon log; at each service start a file larger than 5 MiB rotates to `kumiho.log.1` |
| `\\.\pipe\kumiho\control` | named-pipe control endpoint (LocalSystem, Administrators, local group `kumiho`) |
| `%APPDATA%\kumiho\tui.toml` | per-user TUI prefs (`theme = "dark"` or `"mono"`) |

The installer puts `kumiho.exe` in `%ProgramFiles%\Kumiho` and adds that
directory to the machine `PATH`, so `kumiho` works in new shells. The
`KUMIHO_CONFIG`, `KUMIHO_STATE`, `KUMIHO_TOKENS` and `KUMIHO_RUNTIME_DIR`
overrides work the same as on Linux (`accounts.json` stays beside the path
set by `KUMIHO_TOKENS`), except that on Windows
`KUMIHO_RUNTIME_DIR` relocates only the daemon log directory (default
`%ProgramData%\kumiho\logs`). The control endpoint is
`\\.\pipe\kumiho\control`; `KUMIHO_CONTROL_ENDPOINT` relocates it so a
development daemon can run beside the installed service.

## Transport notes

Kumiho is **not WireGuard**. Traffic is TCP over HTTP/2 CONNECT streams to
Mozilla's Fastly edge, which accepts ports 80 and 443 only (CONNECT to 53, 853
and 444 is refused). Applications that use the local SOCKS5 or HTTP proxy are
subject to the same rule. Non-DNS UDP (including QUIC/HTTP3) is dropped inside
the tunnel on purpose, so applications fall back to TCP.
While connected, DNS is served through the tunnel by the daemon's resolver as
DNS-over-HTTPS on 443 — never as plaintext DNS. It tries the configured
provider (or custom upstream) first, then Cloudflare `1.1.1.1`, then Quad9
`9.9.9.9`; a failed endpoint is skipped for 30 minutes, and when all of them
fail the query gets SERVFAIL instead of a plaintext fallback. systemd-resolved
points `foxy0` at that resolver on Linux (Tailscale's MagicDNS split domains
left intact), and Windows sets the adapter-scoped DNS server to `10.8.0.2`
without touching system-wide settings.
