# Using Kumiho

Everything talks to the daemon over local IPC: `/run/kumiho/control.sock` on
Linux and the named pipe `\\.\pipe\kumiho\control` on Windows. Start it with
`sudo systemctl start kumiho` (installed Linux), `Start-Service kumiho`
(installed Windows) or `scripts/dev-daemon.sh start` (Linux development).

## CLI

    kumiho                      # TUI (default)
    kumiho daemon               # run the daemon in the foreground
    kumiho login                # prompt-driven Firefox Accounts sign-in
    kumiho logout               # clear tokens; disconnects
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

- **Locations** — `REC` first; `p` pings the visible servers; `/` filters;
  `enter` expands a country or selects a server (persisted). A server that
  fails to connect 3 times in a row is dropped and the next connect
  auto-picks (`REC` random, else the first country).
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
- **Account** — live Guardian lookup: email, uid, subscription, quota,
  limited-bandwidth flag, token expiry; `x` signs out (with confirmation).

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
unknown challenge type cannot be solved and fails with a clear error.
Disable it to get that immediate failure while diagnosing, or if a network
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
| `/var/lib/kumiho/tokens.json` | Firefox Accounts tokens (mode 0600) |
| `/var/lib/kumiho/state.json` | selection, failure counts, remembered kill-switch/autoconnect, runtime overrides |
| `/var/lib/kumiho/serverlist.json` | cached server list; only used when a fetch fails |
| `/run/kumiho/control.sock` | IPC socket (0660, group `kumiho`) |
| `~/.config/kumiho/tui.toml` | per-user TUI prefs (`theme = "dark"` or `"mono"`) |

On Windows the machine-wide state lives under `%ProgramData%\kumiho` (ACL:
LocalSystem and Administrators) and the control endpoint is a named pipe:

| Path | Contents |
|---|---|
| `%ProgramData%\kumiho\config.toml` | admin defaults (all keys optional) |
| `%ProgramData%\kumiho\tokens.json` | Firefox Accounts tokens |
| `%ProgramData%\kumiho\state.json` | selection, failure counts, remembered kill-switch/autoconnect, runtime overrides |
| `%ProgramData%\kumiho\serverlist.json` | cached server list; only used when a fetch fails |
| `%ProgramData%\kumiho\logs\kumiho.log` | daemon log; at each service start a file larger than 5 MiB rotates to `kumiho.log.1` |
| `\\.\pipe\kumiho\control` | named-pipe control endpoint (LocalSystem, Administrators, local group `kumiho`) |
| `%APPDATA%\kumiho\tui.toml` | per-user TUI prefs (`theme = "dark"` or `"mono"`) |

The installer puts `kumiho.exe` in `%ProgramFiles%\Kumiho` and adds that
directory to the machine `PATH`, so `kumiho` works in new shells. The
`KUMIHO_CONFIG`, `KUMIHO_STATE`, `KUMIHO_TOKENS` and `KUMIHO_RUNTIME_DIR`
overrides work the same as on Linux, except that on Windows
`KUMIHO_RUNTIME_DIR` relocates only the daemon log directory (default
`%ProgramData%\kumiho\logs`); the control endpoint stays the fixed
`\\.\pipe\kumiho\control` named pipe.

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
