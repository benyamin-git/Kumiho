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
  proxy, excluded CIDRs, allow-LAN, MTU, SOCKS port, quota poll. `d` resets a
  row to the config default; `*` marks values changed from `config.toml`.
  Changes are persisted as overrides in `/var/lib/kumiho/state.json`
  (`%ProgramData%\kumiho\state.json` on Windows).
  `kill_switch`, `log_level` and `redact_targets` apply immediately;
  connection-scoped settings (DNS, MTU, CIDRs, …) apply on the next connect.
  Toggling `kill_switch` on a connected tunnel re-applies the live policy; if
  that re-apply fails the change is rejected with an error and the previous
  value is kept.
- **Logs** — the daemon's in-memory ring plus live streaming; `v` cycles the
  level filter, `/` searches, `c` clears, `f` toggles follow, `e` exports the
  visible entries to `~/.local/state/kumiho/kumiho-logs-<timestamp>.log`
  (`%LOCALAPPDATA%\kumiho\logs` on Windows).
- **Account** — live Guardian lookup: email, uid, subscription, quota,
  limited-bandwidth flag, token expiry; `x` signs out (with confirmation).

## Files

| Path | Contents |
|---|---|
| `/etc/kumiho/config.toml` | admin defaults (all keys optional) |
| `/var/lib/kumiho/tokens.json` | Firefox Accounts tokens (mode 0600) |
| `/var/lib/kumiho/state.json` | selection, failure counts, remembered kill-switch/autoconnect, runtime overrides |
| `/run/kumiho/control.sock` | IPC socket (0660, group `kumiho`) |
| `~/.config/kumiho/tui.toml` | per-user TUI prefs (`theme = "dark"` or `"mono"`) |

On Windows the machine-wide state lives under `%ProgramData%\kumiho` (ACL:
LocalSystem and Administrators) and the control endpoint is a named pipe:

| Path | Contents |
|---|---|
| `%ProgramData%\kumiho\config.toml` | admin defaults (all keys optional) |
| `%ProgramData%\kumiho\tokens.json` | Firefox Accounts tokens |
| `%ProgramData%\kumiho\state.json` | selection, failure counts, remembered kill-switch/autoconnect, runtime overrides |
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
and 444 is refused). Non-DNS UDP (including QUIC/HTTP3) is dropped inside the
tunnel on purpose, so applications fall back to TCP.
While connected, DNS is served through the tunnel by the daemon's resolver as
DNS-over-HTTPS on 443 — never as plaintext DNS. It tries the configured
provider (or custom upstream) first, then Cloudflare `1.1.1.1`, then Quad9
`9.9.9.9`; a failed endpoint is skipped for 30 minutes, and when all of them
fail the query gets SERVFAIL instead of a plaintext fallback. systemd-resolved
points `foxy0` at that resolver on Linux (Tailscale's MagicDNS split domains
left intact), and Windows sets the adapter-scoped DNS server to `10.8.0.2`
without touching system-wide settings.
