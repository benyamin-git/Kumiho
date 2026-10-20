# Troubleshooting

Start with `kumiho doctor` and follow its remediation steps. On Linux it
checks the TUN device, capabilities, nftables, systemd-resolved, Tailscale,
port 1080, socket permissions, IPv6, clock skew and reachability; on Windows
it checks the service, `wintun.dll`, the Base Filtering Engine, the control
pipe, the `foxy0` adapter, its DNS and the physical default route.

## The daemon is unreachable

- Linux: `systemctl status kumiho`; Windows: `Get-Service kumiho`. For a dev
  daemon, `scripts/dev-daemon.sh log` (Linux).
- Your user must be in the `kumiho` group (the installer does this; a new
  login session picks it up) — on Windows that group guards the named pipe.
- Only one daemon can run: stop the installed service before starting a dev
  daemon (`sudo systemctl stop kumiho` on Linux, `Stop-Service kumiho` on
  Windows).

## Windows says "Windows protected your PC"

The Windows binary is not code-signed, so SmartScreen flags it on first run.
Click **More info → Run anyway** to continue. The installer must run from a
PowerShell started with "Run as administrator"; it stops with a message
otherwise.

## Windows service start/stop/recovery

    Get-Service kumiho              # state and start type
    Start-Service kumiho
    Stop-Service kumiho
    Restart-Service kumiho

The installer registers the service to start at boot and to restart after a
crash (after 5 s, 30 s, then 60 s). A foreground `kumiho daemon` cannot run
while the service is running.

## After a crash

A killed Linux daemon leaves the TUN device gone but its rules, nft table and
DNS link config behind. Both daemon start and `kumiho cleanup` remove all of
it; `sudo systemctl restart kumiho` is the simplest fix. A *clean* stop
(SIGTERM, `systemctl stop`) tears everything down by itself.

On Windows, a crash leaves routes and adapter DNS behind and drops the WFP
kill-switch filters, which belong to the dead process. Windows service
recovery restarts the daemon after 5 s; start-time cleanup removes the stale
routes and DNS, and the next connect re-applies the filters. To clean up by
hand, run `kumiho cleanup` from an Administrator shell (it also removes the
`foxy0` adapter).

## Windows: kill-switch crash caveat

The Windows kill switch is a dynamic WFP session: its filters exist only
while the daemon runs, so a crash removes the block until recovery (about 5 s
with the installer's service recovery settings). The Linux nftables kill
switch is a persistent table and is not affected. If that short unblocked
window matters, keep autoconnect on (the default) so the tunnel and filters
return as soon as the service restarts.

## DNS stops resolving while connected

The tunnel resolver is DoH-only and tries its endpoints in serial: the
configured provider (or a custom `dns_upstream`), then Cloudflare `1.1.1.1`,
then Quad9 `9.9.9.9`. A failed endpoint is skipped for 30 minutes, and if all
of them fail queries answer SERVFAIL — there is no plaintext DNS fallback
(the edge refuses CONNECT to port 53 anyway). Watch for `[dns]` warnings in
the logs. Custom providers can be pinned in Settings (`doh_provider`,
`dns_upstream`) or disabled with `off`; `off` without a custom upstream leaves
no DoH endpoints, so in-tunnel DNS returns SERVFAIL.

- Linux: `resolvectl status foxy0` shows what the system is using; the
  adapter should point at the in-TUN resolver.
- Windows: `Get-DnsClientServerAddress -InterfaceAlias foxy0` should show
  `10.8.0.2`, and `Resolve-DnsName example.com` checks resolution. An empty
  or stale list is cleared on the next connect.

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

- `journalctl -u kumiho -f` for the installed Linux service.
- On Windows: `%ProgramData%\kumiho\logs\kumiho.log`; at each service start a
  file larger than 5 MiB rotates to `kumiho.log.1`.
- `kumiho logs -f` for the same stream over IPC (works for dev daemons too).
- The TUI Logs screen (`g`) with level filter, search and `e` export.
