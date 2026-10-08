# Using Kumiho

Everything talks to the daemon over `/run/kumiho/control.sock`; start it with
`sudo systemctl start kumiho` (installed) or `scripts/dev-daemon.sh start`
(development).

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
    kumiho cleanup              # remove stale routes/rules/nft/DNS artifacts
    kumiho version

`--proxy-only` keeps system routing untouched and only starts the SOCKS5
listener on `127.0.0.1:1080`; the full tunnel also creates `foxy0` and routes
TCP + DNS through it.

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
  Changes are persisted as overrides in `/var/lib/kumiho/state.json`.
  `kill_switch`, `log_level` and `redact_targets` apply immediately;
  connection-scoped settings (DNS, MTU, CIDRs, …) apply on the next connect.
- **Logs** — the daemon's in-memory ring plus live streaming; `v` cycles the
  level filter, `/` searches, `c` clears, `f` toggles follow, `e` exports the
  visible entries to `~/.local/state/kumiho/kumiho-logs-<timestamp>.log`.
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

## Transport notes

Kumiho is **not WireGuard**. Traffic is TCP + DNS only, tunneled as HTTP/2
CONNECT streams to Mozilla's Fastly edge. Non-DNS UDP (including QUIC/HTTP3)
is dropped inside the tunnel on purpose, so applications fall back to TCP.
While connected, DNS is served through the tunnel by the daemon's resolver,
with systemd-resolved pointing `foxy0` at it and Tailscale's MagicDNS split
domains left intact.
