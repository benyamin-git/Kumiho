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
then Quad9 `9.9.9.9`. A failed endpoint is skipped for 30 minutes and the next
query moves on to the following one. If every endpoint keeps failing, queries
answer SERVFAIL. There is no plaintext DNS fallback: the edge refuses CONNECT
to port 53 anyway.

Symptoms of that failover are: sites that need a name lookup fail while a
literal IP still works, and the log shows `[dns]` warnings naming the failed
endpoint (SERVFAIL appears once the chain is exhausted). Custom providers can
be pinned in Settings (`doh_provider`, `dns_upstream`) or disabled with `off`;
`off` without a custom upstream leaves no DoH endpoints, so in-tunnel DNS
returns SERVFAIL.

- Linux: `resolvectl status foxy0` shows what the system is using; the
  adapter should point at the in-TUN resolver.
- Windows: `Get-DnsClientServerAddress -InterfaceAlias foxy0` should show
  `10.8.0.2`, and `Resolve-DnsName example.com` checks resolution. An empty
  or stale list is cleared on the next connect.

## You are behind a proxy or a filtered network

**Upstream proxy.** Set `upstream_proxy` in Settings (`s` in the TUI, or the
GUI's Settings screen) to a SOCKS5, SOCKS5h or HTTP proxy URL:

    upstream_proxy = "socks5h://user:pass@proxy.example.net:1080"

The scheme sets the default port (1080 for both `socks5` and `socks5h`, 80
for `http`), `user:pass@` is optional, and the proxy resolves the edge
hostname. Only Kumiho's edge connection uses the hop: the control plane,
Remote Settings, DoH and bootstrap stay direct, and the local SOCKS5/HTTP
listeners are unaffected. A malformed URL is rejected when you save it;
errors at connect time name `upstream_proxy` and never echo the credentials.
To test, save the URL, reconnect, and watch `kumiho logs -f`: a rejection or
dial failure names `upstream_proxy` or `dial proxy <host:port>`, never the
credentials.

**Filtered TLS.** If the normal connection is reset or hangs on a network
that inspects TLS handshakes, try `tls_split=first-byte`, then `tls_split=sni`.
It is best effort, off by default, and applies on the next connect.

## Mozilla answers HTTP 451

Kumiho classifies a 451 as `region_blocked` and distinguishes the control
plane from the VPN edge.

**Start with the guide.** On a blocked network the first run opens **Blocked
region** (CLI/TUI: Enter on the notice or Settings → Region; GUI: the same
screen; Android phone/TV: setup, Settings → Region or the dashboard notice).
It lists the six host results and leads with Option 1:

1. Turn on another VPN (or use the relay).
2. Sign in.
3. Run `kumiho prepare` (or **Prepare**). It fetches the server list, the
   profile and a Guardian pass; only the pass is persisted, in `pass.json`
   beside `tokens.json` (0600, account-bound).
4. Turn the other VPN off when the pass is reported ready.
5. Connect. Kumiho renews through its own tunnel.

A prepared pass lasts about ten minutes. Connect and redial reuse it while
more than 60 seconds remain; a restart inside that window can connect again.
After ten minutes off, repeat the guide.

- **Control plane** (accounts, profile, Remote Settings, the server list) —
  while connected, requests dial through Kumiho's SOCKS5 listener first; if a
  request still answers 451 and a relay is configured, Kumiho retries it
  exactly once through the relay. A user who prepared before but can no longer
  fetch a pass gets `bootstrap_required` (open the guide); a first-time blocked
  user gets `region_blocked`. With no relay and no other VPN, the error is
  `This region is blocking Mozilla's servers (HTTP 451). Set a control-plane
  relay in Settings → Region, or deploy the relay from docs/relay.md, then
  retry.`
- **VPN edge** — the tunnel still originates from your network, so the error
  is `The VPN edge refused this region (HTTP 451). A control-plane relay
  cannot fix the VPN data path; connect from a supported region.` The
  watchdog parks the session instead of redialing.

For the relay path, set **Settings → Region → Control-plane relay** to the
Worker HTTPS origin and the write-only token, then use **Test relay** or
`kumiho relay test`. `Relay rejected the token` means the Worker's
`RELAY_TOKEN` and the stored token differ; `relay_url is not set` means the URL
is empty; `Relay unreachable: <detail>` is DNS, TLS, Worker or upstream. The
relay does not proxy `accounts.firefox.com`, so browser sign-in still needs
another VPN or another device. Deployment, allowlist and trust boundaries are
in [relay.md](relay.md).

## Fastest is disabled or Ping all finds nothing

**Fastest** needs a **fresh** finite RTT from a completed real-edge Ping all;
it is disabled with **Run Ping all to enable Fastest** when the 30-minute cache
has no usable sample. A timeout is a result but cannot be fastest. If every
probe fails, check connectivity and the selected servers; a transport or
run-level failure leaves the previous order and labels alone. Refreshing
restores country order and reloads the engine's fresh cached labels. `kumiho
locations --ping` is the CLI equivalent; it waits for the daemon's `ping_done`
event rather than a fixed timeout.

## A favorite or old recent location is missing

Favorites and recents are refs, not copied server records. Each resolves to
the exact server first, then to the first usable server in the same city. A
missing row usually means Mozilla removed or quarantined that server, the city
is gone, or a version filter now marks it locked. Turn on **Show locked
locations** / `allow_locked` to check whether the record is only locked; clear
recents if an old city keeps rotating in. Favorites and recents are shared
engine state in `state.json`, so they survive restarts and appear in every
client, but not on another device.

## HTTP proxy does not work

- Turn it on: `http_proxy=on` in Settings (or `http_proxy = true` in
  `config.toml`), then reconnect. The listener starts and stops with the
  tunnel, so a refused connection often just means the tunnel is down.
- Check the port: `http_proxy_port` defaults to 8080, must be 1–65535 and
  must be free; a collision is reported as a connect failure.
- Test it with a client that honours `-x`:

      curl -x http://127.0.0.1:8080 https://example.com

- It is loopback-only and unauthenticated. A client on another machine
  cannot use it, and no username or password is needed (or accepted).
- It does not intercept TLS: HTTPS goes through `CONNECT`.

## UDP or DNS in proxy-only mode

The SOCKS5 listener supports `UDP ASSOCIATE` for DNS only: send DNS queries
to port 53 through it and they are answered through the same in-tunnel DoH
chain. Non-DNS UDP — QUIC, games, calls — is blocked on purpose, and the
Fastly edge only accepts TCP 80 and 443, so UDP cannot be tunnelled. In full
mode, ordinary DNS through the tunnel still works through the local resolver,
including TCP; in proxy-only mode, HTTPS and DoH through the proxy are
unaffected.

## Connection drops on a slow link

The default slow keepalive profile allows 25 s for an unanswered ping. If you
switched to the 10 s `standard` profile, a very slow or lossy link can trip;
set `keepalive_profile=slow` and reconnect. It applies on the next connect. The
ping schedule itself is unchanged: 15 s idle before the first ping, checked
every 3 s.

## Traffic is blocked but the state says CONNECTED

That is the kill switch working during an outage: while the upstream session
is dead, all unmarked traffic is blackholed so nothing leaks outside the
tunnel. Recovery is automatic (watchdog redial, or waiting for the network in
`WAITING_NETWORK`). If you want direct traffic instead, disconnect or turn the
kill switch off in Settings.

## Login says the account sent a challenge

Mozilla's edge answers some control-plane requests with a `406` anti-bot
challenge (Fastly's proof-of-work, `PAT` or `clientmetrics`). With
`solve_challenges=on` (the default) Kumiho fetches the challenged origin,
falls back to the related apex when the origin serves no challenge page,
answers the challenge, installs the cookies and retries the request. The
Guardian path allows up to five attempts. The Firefox Accounts login first
tries the sibling host `oauth.accounts.firefox.com` — the same auth server on a
hostname that a Fastly hard block (an empty `406`, common on
`POST /v1/account/login` from flagged or datacenter addresses) does not cover —
and only spends a solve when every host still answers `406`. A dropped
connection on that sibling is retried once. `[fastly]` lines in the log show
each solve step; the sibling attempt is silent unless it also fails.

Token refresh does not use the legacy auth-server `/v1/oauth/token` route:
Fastly now answers that route on both hosts with an empty `406` hard block,
which has no challenge page and cannot be solved. Kumiho refreshes with the
OAuth server's standard form grant at
`https://api.accounts.firefox.com/v1/token` instead, falling back to the oauth
host only when the api host is challenged.

If it still fails the error is explicit: `could not solve the anti-bot
challenge; retry or disable solve_challenges`. A captcha or an unknown
challenge type cannot be solved and fails the same way. Setting
`solve_challenges=off` skips the solver: the challenged host then fails
immediately (the login still tries the sibling host), and when no host can be
cleared the error is
`anti-bot challenge solving is disabled; enable solve_challenges and try
again`; that is a diagnostic step to isolate the problem, not a fix.

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
