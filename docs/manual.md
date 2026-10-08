# Kumiho — simple manual

**What it is:** a program that connects your computer to the Firefox VPN.
You control everything from one menu.

## The one command you need

    kumiho

A screen opens. That's the whole app.

- `space` → connect / disconnect
- `q` → quit

## First-time setup (only once)

    sudo systemctl start kumiho     # start the background service
    kumiho login                    # enter your Firefox email + password (2FA too)
    kumiho doctor                   # checks everything and says what to fix

Done. From then on it starts by itself, even after a reboot.

## Everyday use (inside `kumiho`)

| Key | What happens |
|---|---|
| `space` | connect / disconnect |
| `o` | pick a country/server (`enter` select, `p` test speed, `esc` back) |
| `s` | settings (kill switch, autoconnect, DNS, MTU…) — `enter` change, `d` reset |
| `g` | live logs |
| `a` | your account: email, subscription, data left |
| `?` | help |
| `q` | quit |

The top of the screen always shows: connection state, location, how long
you've been connected, and how much data you have left.

## If you prefer typing commands

    kumiho connect        # connect
    kumiho disconnect     # disconnect
    kumiho status         # what's going on right now
    kumiho locations      # list countries and servers
    kumiho logs -f        # watch what the app is doing (ctrl+c stops)

## Starting / stopping the background service

    sudo systemctl stop kumiho      # stop it (tunnel comes down)
    sudo systemctl start kumiho     # start it (reconnects automatically)

## Something wrong?

    kumiho doctor                   # finds the problem, prints the fix
    journalctl -u kumiho -n 50      # recent messages

- **"cannot reach the daemon"** → `sudo systemctl start kumiho`
- **Internet seems dead while connected** → wait ~10 seconds; it reconnects
  by itself. Still stuck: `kumiho disconnect`, then `kumiho connect`.
- **Weird leftover blocking after a crash** → `sudo kumiho cleanup`

## Three things to know

1. **Only TCP and DNS traffic uses the VPN.** Some things (games, video
   calls, QUIC) intentionally don't work; apps fall back to normal
   connections.
2. **Kill switch is ON by default**: if the VPN drops, nothing leaks out —
   traffic just stops until it reconnects.
3. **SSH and Tailscale never get interrupted** by connecting or disconnecting.
