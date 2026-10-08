# Troubleshooting

Start with `kumiho doctor` — it checks the TUN device, capabilities, nftables,
systemd-resolved, Tailscale, port 1080, socket permissions, IPv6, clock skew
and reachability, and prints exact remediation steps.

## The daemon is unreachable

- `systemctl status kumiho` (installed) or `scripts/dev-daemon.sh log`.
- Your user must be in the `kumiho` group (the installer does this; a new
  login session picks it up).
- Only one daemon can run: stop `kumiho.service` before starting a dev daemon.

## After a crash

A killed daemon leaves the TUN device gone but its rules, nft table and DNS
link config behind. Both daemon start and `kumiho cleanup` remove all of it;
`sudo systemctl restart kumiho` is the simplest fix. A *clean* stop (SIGTERM,
`systemctl stop`) tears everything down by itself.

## DNS stops resolving while connected

The tunnel resolver falls back to DNS-over-TCP:53 through the tunnel if its
DoH provider is unreachable; watch for `[dns]` warnings in the logs. Custom
providers can be pinned in Settings (`doh_provider`, `dns_upstream`) or
disabled with `off` (then only TCP:53 is used). `resolvectl status foxy0`
shows what the system is actually using.

## Traffic is blocked but the state says CONNECTED

That is the kill switch working during an outage: while the upstream session
is dead, all unmarked traffic is blackholed so nothing leaks outside the
tunnel. Recovery is automatic (watchdog redial, or waiting for the network in
`WAITING_NETWORK`). If you want direct traffic instead, disconnect or turn the
kill switch off in Settings.

## Login says the account sent a challenge

Mozilla's edge anti-bot challenge (`406`) is not solvable by Kumiho yet
(PLAN.md §2.2); the error surfaces with this exact wording and retrying later
usually succeeds. No challenge has been observed in practice.

## Watchdog keeps rotating edges

`redial failed (failure N)` warns in the logs are normal during an outage; the
edge rotates every 2 failures, and after 3 consecutive failures the persisted
server selection is cleared so the next connect auto-picks a fresh location.
Use `kumiho locations --ping` to pick a healthy one.

## Where the logs are

- `journalctl -u kumiho -f` for the installed service.
- `kumiho logs -f` for the same stream over IPC (works for dev daemons too).
- The TUI Logs screen (`g`) with level filter, search and `e` export.
