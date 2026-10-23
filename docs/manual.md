# Kumiho — simple manual

**What it is:** a program that connects your computer to the Firefox VPN.
You control everything from one menu.

## The one command you need

    kumiho

A screen opens. That's the whole app.

- `space` → connect / disconnect
- `q` → quit

## First-time setup (only once)

    sudo systemctl start kumiho     # Linux: start the background service
    kumiho login                    # continue in the browser or use password
    kumiho doctor                   # checks everything and says what to fix

On Windows, install first with `.\install.ps1` in an Administrator PowerShell
(see [install.md](install.md)); the installer starts the service for you, and
the other two commands are the same.

Done. From then on it starts by itself, even after a reboot.

## Everyday use (inside `kumiho`)

| Key | What happens |
|---|---|
| `space` | connect / disconnect |
| `o` | pick a country/server (`enter` select, `p` test speed, `esc` back) |
| `s` | settings (kill switch, autoconnect, DNS, MTU, TLS split, proxies, challenge solving, keepalive…) — `enter` change, `d` reset |
| `g` | live logs |
| `a` | accounts and your subscription: email, data left, add/use/rename, rotation |
| `?` | help |
| `q` | quit |

The top of the screen always shows: connection state, location, how long
you've been connected, and how much data you have left.

The default MTU is 8500. You only need to change it if a network has trouble
with large packets; a new value takes effect on the next connect.

## Signing in and keeping several accounts

`kumiho login` asks whether to sign in with your browser or with your
password. Browser sign-in is the easy choice: a Mozilla page opens where you
can enter the password, a code, or solve a captcha, and Kumiho finishes by
itself when you come back. Nothing about your password is stored.

On the Account screen (`a`):

- `↑`/`↓` choose an account, `u` makes it active, `n` renames it, `d` removes
  it.
- `a` adds another account (up to five), `p` adds one with a password, and `R`
  signs the selected account in again through the browser.
- `t` turns **account rotation** on or off, and `+`/`-` sets the percentage.
  When rotation is on, connecting switches away from the active account if it
  has less than that percentage of data left. It only switches before
  connecting, never during a connection.
- `x` signs out of **all** accounts and disconnects. Removing one account
  (`d`) leaves the others signed in.

Your Firefox account is unchanged either way; signing out only clears
Kumiho's stored sign-in. If you choose browser sign-in, the browser must run on
the same computer because Mozilla returns to a local address.

## New settings in v0.4.0

You don't need any of these to use Kumiho. They are for networks that get in
the way. Change them in settings (`s`); most take effect the next time you
connect, while solving challenges takes effect immediately.

- **Solve challenges** (on by default) — Kumiho answers Mozilla's automated
  "are you a robot?" check by itself. If a real picture captcha appears, the
  password sign-in stops with a clear message; use browser sign-in instead,
  where you can solve it on Mozilla's page. Turn the solver off only when you
  are diagnosing a login problem.
- **TLS split** (`off`, `first-byte` or `sni`) — try `first-byte`, then
  `sni`, if a network blocks the normal connection. It is best effort and
  nothing is guaranteed.
- **Upstream proxy** — for people who must go through a proxy, for example
  `socks5h://user:pass@proxy.example.net:1080`. Only Kumiho's connection to
  the VPN servers goes through it; the rest of the machine's traffic does
  not.
- **HTTP proxy** (`http_proxy` and `http_proxy_port`) — starts a proxy
  together with the tunnel:

      curl -x http://127.0.0.1:8080 https://example.com

  It is for this computer only (loopback), needs no password, and stops with
  the tunnel.
- **Keepalive profile** — choose `slow` if the connection keeps dropping on a
  slow link.

DNS stays encrypted. There is no plaintext fallback: if every encrypted
resolver fails, names stop resolving rather than leak. The technical detail
is in [usage.md](usage.md).

## If you prefer typing commands

    kumiho connect        # connect
    kumiho disconnect     # disconnect
    kumiho status         # what's going on right now
    kumiho locations      # list countries and servers
    kumiho logs -f        # watch what the app is doing (ctrl+c stops)

## Starting / stopping the background service

Linux:

    sudo systemctl stop kumiho      # stop it (tunnel comes down)
    sudo systemctl start kumiho     # start it (reconnects automatically)

Windows:

    Stop-Service kumiho
    Start-Service kumiho

## Something wrong?

    kumiho doctor                   # finds the problem, prints the fix
    journalctl -u kumiho -n 50      # Linux: recent messages
    Get-Service kumiho              # Windows: is the service running?

On Windows the log file is `%ProgramData%\kumiho\logs\kumiho.log`.

- **"cannot reach the daemon"** → `sudo systemctl start kumiho` (Linux) or
  `Start-Service kumiho` (Windows).
- **Internet seems dead while connected** → wait ~10 seconds; it reconnects
  by itself. Still stuck: `kumiho disconnect`, then `kumiho connect`.
- **Weird leftover blocking after a crash** → `sudo kumiho cleanup` (on
  Windows run `kumiho cleanup` from an Administrator PowerShell).

## Three things to know

1. **Only TCP traffic uses the VPN, on ports 80 and 443.** DNS is answered
   too, but as encrypted DoH inside the tunnel — never as a separate plaintext
   DNS connection. Some things (games, video calls, QUIC) intentionally don't
   work; apps fall back to normal connections.
2. **Kill switch is ON by default**: if the VPN drops, nothing leaks out —
   traffic just stops until it reconnects.
3. **On Linux, SSH and Tailscale never get interrupted** by connecting or
   disconnecting. On Windows, connecting may reset existing sessions first.
