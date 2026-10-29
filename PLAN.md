# Kumiho — Implementation Plan

**Status: implemented through v0.4.0 in source; v0.1.0–v0.3.0 are released
(v0.4.0 is not tagged).**
**Plan date:** 2026-10-05
**Repo:** `github.com/benyamin-git/kumiho`
**Binary:** `kumiho` (optional short alias `kum`)
**License:** MIT (with NOTICE crediting FoxyVPN and UjuiUjuMandan/firefox-vpn-client)

---

## 0. One-paragraph summary

Kumiho is an unofficial Linux CLI/TUI client for the free Firefox-account VPN proxy (the same entitlement FoxyVPN for Android uses). It signs into Firefox Accounts, mints a Guardian proxy pass, resolves the Mozilla server list, and tunnels system traffic over **HTTP/2 CONNECT streams to a Fastly edge** with a local SOCKS5 server in front. On Linux it adds a TUN device + userspace TCP/IP stack (gVisor via `xjasonlyu/tun2socks`), policy routing that cannot kill an existing SSH/Tailscale session, optional nftables kill switch, and a Bubble Tea TUI plus a systemd daemon so the tunnel survives SSH logouts.

**It is not WireGuard.** No WireGuard config exists for this entitlement. The transport is the same HTTP/2 CONNECT proxy FoxyVPN implements, and the live Fastly edge accepts **TCP 80 and 443 only** — CONNECT to 53, 853 and 444 is refused. Consequence: **TCP only on those two ports**; DNS is carried as DoH over HTTPS inside the tunnel, never as a separate plaintext DNS path, and all other UDP (QUIC/HTTP3, VoIP, games) is blocked on purpose so applications fall back to TCP.

---

## 1. Decisions log (all answers from the user)

| # | Topic | Decision |
|---|-------|----------|
| 1 | Language / TUI | **Go + Bubble Tea** (single static binary) |
| 2 | Tunnel scope | **Full TUN system tunnel + proxy-only mode toggle** |
| 3 | Process model | **systemd daemon + TUI client over Unix socket** (VPN survives SSH logout) |
| 4 | UDP policy | **Block all non-DNS UDP** (matches FoxyVPN; apps fall back to TCP) |
| 5 | Upstream transport | **HTTP/2 only** (no HTTP/3 in v1) |
| 6 | Login | **Email + password + email 2FA code** (no session-token mode in v1) |
| 7 | OS | **Ubuntu 24.04 LTS amd64**, full install |
| 8 | Features v1 | Locations+ping, quota/account, exit check, custom DNS, DoH provider choice, custom edge pinning, upstream proxy chaining, logs viewer, speed+totals, auto failover |
| 9 | Kill switch | Toggle; **remember last state** (first install = **ON**) |
| 10 | LAN / Tailscale | Tailscale = **SSH + peer access only** (no exit node, no subnet router); LAN and tailnet traffic must bypass the tunnel; SSH must never break |
| 11 | Split tunneling | **CIDR exclude list** (per-app later phase) |
| 12 | DNS | **Automatic, resolved-first**: configure `foxy0` via systemd-resolved; never touch `/etc/resolv.conf`; Tailscale MagicDNS split DNS preserved |
| 13 | IPv6 | **Blackhole only** (exact FoxyVPN behavior; no AAAA stripping) |
| 14 | Autostart | Toggle; **remember last state** (first install = **ON**) — user update 2026-10-05 (originally OFF) |
| 15 | SOCKS listener | **Loopback only** (`127.0.0.1:1080`) |
| 16 | Daemon privileges | **Dedicated system user `kumiho` + `CAP_NET_ADMIN` only** + polkit rule for resolved |
| 17 | Distribution | **Static binary + `install.sh` + systemd units** (no .deb in v1) |
| 18 | Name | **Kumiho** (Korean nine-tailed fox that shapeshifts to move unnoticed) |
| 19 | Repo | **GitHub public** (module path `github.com/<GH_USER>/kumiho`) |
| 20 | License | **MIT** |
| 21 | Account | A Firefox account with the browser-VPN entitlement (eligible region) |
| 22 | Forwarded traffic | **Host traffic only** (Docker/forwarded packets keep their normal route) |

> **2026-10-10 update:** decisions #3 (daemon + Unix socket), #7 (Ubuntu-only)
> and #16 (systemd/polkit privileges) describe the v0.1.0 Linux
> implementation and are superseded by the multiplatform design:
> [docs/superpowers/specs/2026-10-10-multiplatform-core-design.md](docs/superpowers/specs/2026-10-10-multiplatform-core-design.md).
>
> **2026-10-10 (v0.2.0) platform update:** the multiplatform core's first
> non-Linux release is Windows 11 amd64 — a LocalSystem Windows service,
> Wintun TUN, WFP kill switch and named-pipe IPC. See
> [docs/superpowers/specs/2026-10-10-v0.2.0-windows-release-design.md](docs/superpowers/specs/2026-10-10-v0.2.0-windows-release-design.md).

---

## 2. What FoxyVPN does — verified facts this plan is built on

All facts below were read directly from `Vauth/FoxyVPN` (Kotlin, MIT) and its credited reference `UjuiUjuMandan/firefox-vpn-client` (Go, **unlicensed — do not copy code; behavior reference only**).

### 2.1 Control plane

| Item | Value |
|---|---|
| FxA server | `https://api.accounts.firefox.com/v1` |
| OAuth client_id | `5882386c6d801776` |
| OAuth scope | `profile https://identity.mozilla.com/apps/vpn` |
| Protocol prefix | `identity.mozilla.com/picl/v1/` |
| authPW | `PBKDF2-SHA256(password, "identity.mozilla.com/picl/v1/quickStretch:"+email, 1000, 32)` then `HKDF-SHA256(ikm=quickStretch, info=prefix+"authPW", 32)` |
| Hawk | sessionToken hex → HKDF 64 bytes → id (hex of first 32), key (last 32); header `Hawk id="..", ts="..", nonce="..", mac=".."[, hash=".."]`; nonce = base64url(6 random bytes); **mac/hash in Base64** (FoxyVPN behavior; the spec-correct form) |
| Hawk normalized string | `hawk.1.header\n{ts}\n{nonce}\n{METHOD}\n{path}\n{host}\n{port}\n{payloadHash}\n\n`; payloadHash = `Base64(SHA256("hawk.1.payload\napplication/json\n"+body+"\n"))` |
| Login body | `{email, authPW: hex, verificationMethod: "email-2fa"}`; on errno 107 retry without `verificationMethod` |
| 2FA | `POST /session/verify_code` `{code}` with Hawk; if server answers method `email` (link flow) tell user to click the link and press Enter |
| OAuth grant | `POST /oauth/token` `{client_id, grant_type:"fxa-credentials", scope, access_type:"offline"}` with Hawk |
| Refresh | `POST /oauth/token` `{client_id, grant_type:"refresh_token", refresh_token, scope}` (no Hawk) |
| Token TTL | `expires_in` (typically 86400 s), treat invalid 60 s before expiry |
| Guardian | `https://vpn.mozilla.org` `GET /api/v1/fpn/token`, `GET /api/v1/fpn/status`, `POST /api/v1/fpn/activate`; `Authorization: Bearer <access_token>` |
| Proxy pass | response `{token}` JWT; expiry from `expires_at` or JWT `exp`; quota from `X-Quota-Limit`, `X-Quota-Remaining`, `X-Quota-Reset` |
| Guardian errors | 401/403 → refresh access token, if still rejected → activate, retry once; 429 → quota exhausted (fatal until reset); 406 → Fastly challenge |
| Server list | `GET https://firefox.settings.services.mozilla.com/v1/buckets/main/collections/vpn-serverlist/records`; records contain country (nested or direct); skip empty code/cities and `CatchAll Anycast`; `REC` = recommended |
| Server → proxy | per server take protocol `name=="connect"` → `host:port`; if protocols empty use `hostname:port`; skip `quarantined` |
| UA (all Mozilla APIs) | `MozillaVPN/2.35.0 (sys:linux; iap:true)` + `Accept: application/json` |

### 2.2 Fastly anti-bot solver (on HTTP 406)

`internal/fastly` is a stdlib-only port of FoxyVPN v1.0.7's
`FastlyChallengeSolver.kt` (MIT). Kumiho never uses browser automation and has
no interactive captcha UI.

- Trigger: HTTP 406 from the Firefox Accounts or Guardian control plane; the
  solver receives the challenged URL and works on that origin.
- Fetches the challenged origin's `/`, and only when that origin serves no
  challenge page retries the related apex (`api.accounts.firefox.com` →
  `accounts.firefox.com`). An HTTP error while fetching the page is a
  challenge-fetch failure, never "no challenge" (Ruling 16).
- Parses `/_fs-ch-*` and the last `init([...], "token", "prefix")`; answers
  `pow` (two-character suffix over the 62-character `a-zA-Z0-9` alphabet so
  `SHA256(base+suffix)` matches), `pat` (`POST {prefix}/pat?token=…`, empty auth
  accepted on 400/401) and `clientmetrics` (a static, non-bot payload).
- `POST {prefix}/fst-post-back` `{token, data:[…]}` for up to 3 rounds; on
  `status=="success"` it re-fetches `/` to verify the challenge is gone, then
  installs the cookies into the shared control-plane jar used by FxA, Guardian
  and Remote Settings.
- Each solve uses an isolated per-solve cookie jar and proceeds serially (one
  origin, one round at a time). One solve has a 60 s budget with 250–750 ms
  jitter between rounds; every attempt is logged at warn under the `fastly` tag.
- Unsupported types — captcha included — fail with a clear error naming the
  type. The Guardian client retries a challenged request up to 5 times.
  The Firefox Accounts client instead tries each host once without solving,
  then gives each challenged host one solve: a second 406 means that host
  cannot be cleared (Fastly also hard-blocks
  `POST api.accounts.firefox.com/v1/account/login` with an empty 406 that no
  challenge cookie can clear). The sibling host
  `https://oauth.accounts.firefox.com/v1` — the same auth-server build behind
  a hostname the block does not cover — therefore answers first in the common
  hard-block case, before any solve is spent. A transport-level failure on
  that sibling (the edge can reset the connection after challenging the
  primary) is retried once after 500 ms. `solve_challenges` defaults on and is
  read per control-plane request, so it applies immediately; off restores the
  immediate, clearly worded failure on the primary host, and the fallback is
  still tried.
- The solver sends a Chrome 126 (Windows) UA while Mozilla API requests keep the
  MozillaVPN UA (§2.1).

### 2.3 Data plane (HTTP/2 CONNECT)

- TLS 1.2/1.3, ALPN `h2`; TCP to edge `host:port`; TLS server name = server-list hostname even when dialing a pinned/resolved IP; system CA verification.
- Client SETTINGS: initial window 16 MiB, max frame 256 KiB, push disabled.
- Per flow: open stream, send `:method CONNECT`, `:authority host:port`, `proxy-authorization: Bearer <proxy_pass>`; 2xx → raw byte tunnel (DATA frames both ways); non-2xx → `UpstreamConnectRejectedException(status)`.
- Keepalive: check every 3 s; if idle ≥ 15 s send PING payload `0x466F787956504E`; if no ack within 10 s (`standard`) or 25 s (`slow`) → session dead. Answer server PINGs.
- `keepalive_profile` is read at connect time: `standard` (default) 10 s, `slow` 25 s; idle 15 s and the 3 s check are unchanged.
- `upstream_proxy` wraps only the edge TCP dial, after which TLS SNI and the H2 `:authority` remain the edge hostname. `socks5://`/`socks5h://` use SOCKS5 CONNECT and `http://` uses HTTP CONNECT; URL credentials become SOCKS5 user/pass or `Proxy-Authorization: Basic`; scheme default ports are 1080/80. The proxy resolves the edge hostname remotely. With `edge_address` pinned the proxy receives that IP while SNI/authority keep the hostname. Control-plane, Remote Settings, DoH and bootstrap dials never use this hop.
- `tls_split` (`off|first-byte|sni`, default off) is a best-effort ClientHello fragmentation wrapper applied to the final edge connection — after the proxy hop, before the TLS handshake — and to control-plane dials. DoH and bootstrap dials stay unsplit.
- `SETTINGS_MAX_CONCURRENT_STREAMS` honored (queue new flows up to 15 s for a slot).
- `GOAWAY` → no new streams, drain existing (idle 120 s cutoff).
- Proxy pass renewal: delay = min(half remaining, remaining − 30 s), clamp [15 s, 30 min], fallback 4 min if no expiry, retry 30 s; **swap bearer token in place** (live session keeps running).
- Fatal errors: HTTP 429 (quota), 401/403 from Guardian (token) → stop tunnel with message.

### 2.4 Connection lifecycle (FoxyVPN behavior we mirror)

- Selection: persisted server reused; after 3 consecutive connect failures the selection is cleared and a new location auto-picks (`REC` random, else first country).
- Initial dial: up to 4 attempts, full-jitter backoff 2–8 s, settle 600 ms; alternate edges: same city then same country, distinct, max 3.
- Watchdog every 2 s: on session down → redial with full-jitter 3–60 s; rotate edge every 2 failures; fatal errors stop.
- Session health: 10 distinct destination timeouts (no success in between) ⇒ session unhealthy ⇒ redial; a 401/403/407 on a stream ⇒ session unauthenticated ⇒ refuse locally and redial; 502/503/504 ⇒ remember destination as unreachable (TTL 30 s doubling per strike, cap 10 min, 3 strikes = treat as edge policy).
- Local SOCKS5: no-auth greeting, CONNECT, and DNS-only UDP ASSOCIATE (target port 53 or 0). One association per client, a loopback relay, datagrams accepted only from the TCP peer (and the requested port when one was named), only port-53 payloads, 32 in-flight resolver calls, 64 KiB datagrams, an 8 s per-query budget, SERVFAIL when the resolver fails, and teardown with the TCP control connection or server stop. It shares the in-tunnel DoH resolver (§3.5) and works in proxy-only mode. Handshake timeout 10 s; max 512 connections; half-close drain 2 min; local destinations refused (loopback/RFC1918/link-local/multicast/CGNAT/ULA).
- Optional local HTTP proxy (`http_proxy=false` by default, `http_proxy_port=8080`): loopback only, no authentication, started and stopped with SOCKS and the tunnel. CONNECT and absolute-form plain HTTP, one request per connection, 512 concurrent clients, 2 min half-close drain, and the same local-destination refusal policy as SOCKS.
- Exit check (optional): `https://www.cloudflare.com/cdn-cgi/trace` through the tunnel; parse `ip=`/`loc=`; compare to selected country (`REC` exempt); failure is non-fatal.

---

## 3. Linux networking design (the part Android did for FoxyVPN)

### 3.1 TUN device

- Name `foxy0`, created via `/dev/net/tun` (`IFF_TUN | IFF_NO_PI`), non-persistent (auto-destroyed on close/crash).
- IPv4 `10.8.0.2/32`; **no IPv6 address** (FoxyVPN parity: IPv6 blackholed).
- MTU **8500** (FoxyVPN parity). `foxy0` feeds the gVisor userspace netstack,
  which handles the packet sizes itself, so the larger MTU reduces per-packet
  overhead through that stack. Kumiho has no mobile kernel VPN path, so the
  reference app's mobile **1400** rationale does not apply; mobile/Android
  parity (including a 1400 default) is deliberately out of scope for v0.4.0.
  `mtu` remains configurable (576–65535) and applies on the next connect.
- Local DNS server binds `10.8.0.2:53` (UDP+TCP) — kernel local-delivery means DNS never traverses the tunnel stack.

### 3.2 Marking and policy routing

Design goal: full-system tunnel that **cannot kill an existing SSH session** (public or Tailscale) and never interferes with `tailscaled`.

1. nftables table `inet kumiho`, chain `kumiho_mark` at hook `output` (priority `mangle`, −150) with rules, in order (chain names carry the `kumiho_` prefix: nftables ≥ 1.0.9 reserves bare words like `mark` as keywords):
   1. `meta mark != 0 return` — never touch Tailscale's `0x80000`, our `0x2`, or any existing mark.
   2. `oifname "foxy0" return`.
   3. `ip daddr 10.8.0.2 return` (local DNS/TUN address).
   4. `ip daddr 100.64.0.0/10 return`, `ip6 daddr fd7a:115c:a1e0::/48 return` — Tailscale.
   5. `ip daddr {127.0.0.0/8, 169.254.0.0/16, 224.0.0.0/4, 255.255.255.255, RFC1918, auto-detected LAN subnets}` return; same for IPv6 (`::1/128`, `fe80::/10`, `fc00::/7`, `ff00::/8`).
   6. custom `exclude_cidrs` set → return.
   7. `ct state new meta mark set 0x1` — only **new, locally-generated** connections are marked.
2. `ip rule add fwmark 0x1/0xff lookup 1000 priority 1100`; `ip route add default dev foxy0 table 1000`; IPv6 mirror with `ip -6 rule/route`. **Kill-switch fallback:** `ip rule add fwmark 0x1/0xff lookup 1001 priority 1200` + `ip route replace blackhole default table 1001` (IPv6 mirror) — marked traffic that cannot resolve to the TUN is dropped by the routing layer itself (§3.3).
3. Replies to inbound connections are `ESTABLISHED`, not new ⇒ unmarked ⇒ default/main table ⇒ original interface. **Tailscale SSH and public SSH survive connect/disconnect/reconnect.**
4. Daemon's own sockets (control plane, edge TCP, bootstrap DoH) set `SO_MARK=0x2` ⇒ never marked ⇒ physical route; no loop.
5. Tailscale untouched: it marks its own sockets `0x80000/0xff0000`, uses table 52 and its own nft table; we never modify them.
6. Forwarded traffic (FORWARD hook) is not marked (host-only decision) ⇒ Docker/Tailscale subnet forwarding unaffected.

### 3.3 Kill switch (toggle, default per last state; first install ON)

Same nft table, chain `kumiho_guard` at `output`:
- drop marked non-DNS UDP (forces QUIC→TCP fallback);
- everything unmarked (Tailscale, LAN, daemon, established replies) untouched.

Enforcement for marked traffic lives in routing, not in the guard: policy rule 1100 is the
only rule marked traffic can take, and table 1000 only ever contains the default route via
the TUN. When that route is unavailable (TUN loss, crash residue, reconnect windows) the
§3.2 fallback rule (priority 1200 → table 1001 `blackhole default`) drops the packet. The
guard deliberately does **not** match `oifname`: the re-route performed by the `type route`
mark chain is not visible to later chains in the same output hook (verified live on Ubuntu
24.04 / nftables 1.0.9 — an `oifname "foxy0"` rule never matched and the guard dropped every
marked packet, killing the whole tunnel).

Cleanup: rules/routes/table removed on graceful stop, `ExecStopPost` cleanup, and idempotent cleanup on every daemon start (crash recovery).

### 3.4 DNS (systemd-resolved integration)

- On connect: `resolvectl dns foxy0 10.8.0.2`; `resolvectl domain foxy0 '~.'`; `resolvectl default-route foxy0 yes`.
- On disconnect: `resolvectl revert foxy0`; link removal also auto-purges settings.
- Tailscale MagicDNS keeps working because its `~ts.net`-style split domains are more specific than `~.`.
- If resolved is not the DNS manager: **do not touch `/etc/resolv.conf`**; TUI shows a prominent "DNS is not managed — queries may leak" warning.
- Daemon needs polkit permission; installer adds `/etc/polkit-1/rules.d/50-kumiho-resolved.rules` allowing user `kumiho` to call the resolved link-configuration actions (exact action IDs verified during implementation against `/usr/share/polkit-1/actions/org.freedesktop.resolve1.policy`).

### 3.5 DNS resolver (in-daemon)

Two roles:

1. **Bootstrap / edge resolution (outside tunnel, `SO_MARK 0x2`)**: DoH POST `application/dns-message` to the selected provider by IP literal; A first then AAAA; race providers; 3 s timeouts; cache 5 min; failing endpoint suppressed 30 min; fall back to system resolver (this outside-the-tunnel path keeps its TCP:53 fallback; the edge is not involved). (FoxyVPN `EdgeAddressResolver` parity.)
2. **User DNS (inside tunnel)**: listener `10.8.0.2:53` UDP+TCP (miekg/dns).
   DoH only — the TCP:53 path is gone from this resolver and no plaintext DNS
   path exists inside the tunnel. For each query:
   - serial endpoint chain, de-duplicated: the configured provider, or a custom
     `dns_upstream` as `https://<ip>/dns-query`, then Cloudflare `1.1.1.1`,
     then Quad9 `9.9.9.9`;
   - each endpoint is queried **through the tunnel** (HTTP POST over an H2
     CONNECT stream to `<endpoint-ip>:443`); TLS verifies the IP literal (works
     for CF/Google/Quad9, as FoxyVPN proves);
   - a transport/HTTP/unpack failure suppresses that endpoint for 30 min
     in-memory and the next one is tried within the same query budget; a valid
     DNS message (including NXDOMAIN or SERVFAIL) is returned as-is;
   - when every endpoint is suppressed, the one whose suppression expires first
     gets one bounded half-open probe, so a recovered network is noticed before
     the 30 minutes elapse (Ruling 10);
   - a query that exhausts the chain gets SERVFAIL — the edge refuses CONNECT to
     53, 853 and 444, so a plaintext fallback cannot work;
   - 8 s per query, 4 s per endpoint; cache with TTL (10 000 entries, min TTL
     5 s, max 1 h); UDP truncation handled by setting TC and letting clients
     retry TCP;
   - AAAA answers passed through unchanged (blackhole-only decision).
3. Custom DNS and provider semantics: a user-provided `dns_upstream` IP is always
   queried as DoH at `https://<ip>/dns-query`, whether or not it belongs to a
   known DoH operator. `doh_provider=off` with no custom upstream leaves no
   in-tunnel endpoints, so every in-tunnel query gets SERVFAIL. The bootstrap
   resolver outside the tunnel (role 1) keeps its legacy DoH + TCP:53 fallback
   and is not affected by `doh_provider` semantics inside the tunnel.

### 3.6 IPv6

- Blackhole only: IPv6 default route into `foxy0` table; packets are silently dropped by the stack (FoxyVPN parity). No AAAA stripping.
- Kill switch drops marked IPv6 on non-tunnel interfaces.

### 3.7 Stack engine

- `xjasonlyu/tun2socks` (MIT, gVisor netstack) as the TCP engine, with a **custom proxy adapter** that dials via `UpstreamSession.OpenStream(host, port)` directly (no extra SOCKS hop).
- UDP inside TUN: rejected (the adapter returns "not supported") so the stack answers port-unreachable quickly; the only DNS path is the local listener. Datagram counts logged.
- Local SOCKS5 server stays because of proxy-only mode, health checks, and exit-check-over-proxy parity.

---

## 4. Daemon design

### 4.1 States

`UNAUTHENTICATED → IDLE → CONNECTING → CONNECTED ⇄ RECONNECTING / WAITING_NETWORK → IDLE`, plus `FATAL` (quota/token) and `PROXY_ONLY` as a CONNECTED variant.

### 4.2 Background workers

| Worker | Behavior |
|---|---|
| Watchdog | 2 s poll, redial with full-jitter 3–60 s, edge rotation every 2 failures, fatal stop |
| Pass renewal | half-life scheduling, in-place bearer swap, 30 s retry, 4 min fallback |
| Access-token refresh | refresh 2 min before expiry / on Guardian 401-403 |
| Quota poll | refresh pass headers every 15 min while connected; update TUI |
| Netlink monitor | link/route changes → re-apply interface config and redial if needed (covers Tailscale up/down, Wi-Fi/VPS reconfig) |
| Metrics | byte counters per direction from the stack + SOCKS; 2 s rate samples |
| Exit check | once per connect, non-fatal |

### 4.3 Crash/safety guarantees

- Start: cleanup any stale nft table, ip rules, routes, DNS link config.
- Stop (SIGTERM): stop workers → close tun (routes referencing it vanish) → delete rules/table → revert DNS → remove socket.
- `systemd` `Restart=on-failure`, `RuntimeDirectory=kumiho`, `StateDirectory=kumiho`.

---

## 5. IPC protocol (TUI/CLI ⇄ daemon)

Unix socket `/run/kumiho/control.sock`, mode `0660`, owner `kumiho:kumiho` (installer optionally adds the operator's user to group `kumiho`). Newline-delimited JSON, `{"v":1,"id":…,"type":…}` request/response plus unsolicited events.

| C→S | Purpose |
|---|---|
| `hello` | version handshake |
| `login_email {email}` → `login_password {password}` → `login_2fa {code}` | interactive auth state machine |
| `logout` | clear tokens, disconnect |
| `connect {location_code?, city_code?, server?, proxy_only?}` | connect (defaults to persisted) |
| `disconnect` | graceful teardown |
| `status` | full snapshot |
| `locations {refresh}` | server list (cached) |
| `ping {hosts[]}` | TCP latency batch |
| `settings_get` / `settings_set {key,value}` | runtime settings |
| `logs_subscribe {level}` / `logs_unsubscribe` / `logs_clear` | live log stream |
| `shutdown` (root only) | stop daemon |

| S→C | Payload |
|---|---|
| `login_state {step, error?}` | credentials → 2FA → done |
| `status {state, location, server, since, rates, totals, quota, pass_expires, dns, kill_switch, ipv6, exit_ip?}` | dashboard |
| `locations {countries[]}` | list with city/server metadata |
| `ping_result {host, rtt_ms or null}` | latency |
| `settings {…}` | all settings |
| `log {ts, level, tag, msg}` | streamed entries |
| `error {code, message}` | recoverable errors |

---

## 6. TUI specification (Bubble Tea)

Screens: **Dashboard** (state, location, server host, uptime, rates, totals, quota bar + reset date, pass expiry countdown, exit IP/country, DNS/kill-switch/IPv6 badges; `space` connect/disconnect, `r` reconnect), **Locations** (countries → cities → servers, `REC` first, `/` filter, `p` ping visible, `enter` select/persist, fails clear after 3 connections), **Account** (email/uid, subscribed, quota, token expiry, logout), **Settings** (all toggles of §1–2 features; CIDR exclude editor; DoH provider; custom DNS; custom edge; upstream proxy; SOCKS port; exit check; theme), **Logs** (ring buffer, level filter, `/` search, `e` export locally, `f` follow), **Login modal** (masked password, 2FA step, actionable errors). `?` help overlay; arrows + `hjkl`; footer shows context keys. No mouse dependency. 16/256-color safe dark theme (orange/red fox accents).

---

## 7. CLI specification

```
kumiho                      # TUI (default)
kumiho daemon               # run daemon (systemd)
kumiho login                # browser or password login through the daemon
kumiho accounts             # list accounts; add/use/remove/rename/rotate
kumiho logout               # clear every account; disconnect
kumiho connect [--to CODE|CITY|HOST] [--proxy-only] [--kill-switch=on|off]
kumiho disconnect
kumiho status [--json]
kumiho locations [--ping] [--json]
kumiho logs [-f] [--level info|warn|error]
kumiho doctor               # preflight diagnostics
kumiho cleanup              # remove stale routes/rules/nft/DNS artifacts (crash recovery)
kumiho version
```

`kumiho doctor` checks: `/dev/net/tun`, effective `CAP_NET_ADMIN`, kernel nf_tables support and `nft` binary, systemd-resolved active + resolv.conf symlink, Tailscale detection (interface, `0x80000` rules, table 52, MagicDNS registration), IPv6 state, default route, DNS leak risk, port 1080 free, socket permissions, clock skew vs Mozilla `Date` header (Hawk/JWT), HTTPS reachability of FxA/Guardian/Remote Settings, and prints exact remediation steps.

---

## 8. Files, config and state

| Path | Contents |
|---|---|
| `/usr/local/bin/kumiho` | static binary |
| `/etc/kumiho/config.toml` | admin defaults (optional; see schema) |
| `/var/lib/kumiho/accounts.json` | v2 account store: active id, last method, up to five accounts with labels, method, FxA tokens and timestamps; the legacy opaque browser token is decode-only; mode 0600 |
| `/var/lib/kumiho/tokens.json` | rollback copy of the active account's `{access_token, refresh_token, expires_at, scope}` tokens, mode 0600; legacy installs migrate from it and old clients can still read it until logout |
| `/var/lib/kumiho/state.json` | persisted location, failure counts, kill-switch/autoconnect last states, runtime settings overrides |
| `/var/lib/kumiho/serverlist.json` | disposable raw server-list cache (0600, atomic; written after a successful fetch, read only when a fetch fails) |
| `/run/kumiho/control.sock` | IPC |
| `/etc/systemd/system/kumiho.service` | daemon unit |
| `/etc/polkit-1/rules.d/50-kumiho-resolved.rules` | DNS permission |
| `~/.config/kumiho/tui.toml` | per-user TUI prefs (theme) |

`config.toml` keys (defaults from `internal/settings.DefaultConfig`, checked by
`Validate`):

| Key | Default | Notes |
|---|---|---|
| `socks_bind` | `127.0.0.1` | loopback only in v1 |
| `socks_port` | `1080` | local SOCKS5 listener |
| `proxy_only` | `false` | skip the TUN; SOCKS/HTTP proxy only |
| `kill_switch` | `last` | `last\|on\|off`; first install ON |
| `autoconnect` | `last` | `last\|on\|off`; first install ON |
| `exit_check` | `true` | one non-fatal exit-IP check per connect |
| `doh_provider` | `automatic` | `automatic\|cloudflare\|google\|quad9\|off` |
| `dns_upstream` | unset | custom DoH IP; heads the in-tunnel chain |
| `edge_address` | unset | pinned edge IP; SNI/`:authority` keep the hostname |
| `upstream_proxy` | unset | `socks5://`, `socks5h://` or `http://`, optional URL credentials (SOCKS5 user/pass or `Proxy-Authorization: Basic`); edge dial only |
| `exclude_cidrs` | `[]` | CIDRs that bypass the tunnel |
| `allow_lan` | `true` | LAN and link-local stay outside the tunnel |
| `mtu` | `8500` | TUN MTU, 576–65535, next connect |
| `log_level` | `info` | `debug\|info\|warn\|error` |
| `redact_targets` | `true` | redact hostnames in logs |
| `quota_poll_minutes` | `15` | quota refresh interval while connected; failures back off to a 30-minute cap |
| `account_rotation` | `false` | opt-in before-connect rotation; never mid-session |
| `rotation_threshold` | `10` | switch eligible accounts below this percent left, 1–50 |
| `tls_split` | `off` | `off\|first-byte\|sni`; next connect |
| `http_proxy` | `false` | local HTTP proxy; next connect |
| `http_proxy_port` | `8080` | 1–65535 |
| `solve_challenges` | `true` | read per control-plane request |
| `keepalive_profile` | `standard` | `standard\|slow`; next connect |

The five v0.4.0 keys (`tls_split`, `http_proxy`, `http_proxy_port`,
`solve_challenges`, `keepalive_profile`) are additive runtime overrides in
`state.json` and optional additive `SettingsView` fields: no IPC v1 envelope,
command or existing-field change.

The v0.6.0 account work is additive at the protocol layer: new `login_*` and
`accounts_*` command types plus optional `AccountsView`/`AccountsSummary`
fields. `accounts.json` v2 is authoritative; a one-account install is migrated
from `tokens.json`, which stays as a synchronized rollback copy until logout.
The active account is never switched while the tunnel is up, and rotation is
opt-in, before connect only.

---

## 9. Repository layout

```
kumiho/
  cmd/kumiho/           # CLI/TUI/daemon composition root
  cmd/kumiho-gui/       # Windows WebView2 GUI (build-tagged)
  cmd/kumiho-gui-web/   # GUI dev harness (Linux, mock daemon)
  cmd/kumiho-gui-probe/ # Windows support probe
  internal/cli/         # subcommands, doctor, cleanup, service host
  internal/tui/         # bubbletea app, screens, widgets
  internal/ipc/         # protocol v1 codec, client, server
  internal/api/         # request/response and settings view types
  internal/engine/      # controller, state machine, workers, metrics
  internal/platform/    # platform seam: linux/, windows/, android/, auto/, unsupported/
  internal/daemon/      # service host and log sink
  internal/fxa/         # authPW, hawk, login, 2FA, oauth, refresh
  internal/fastly/      # Fastly challenge solver (pow/pat/clientmetrics, shared jar)
  internal/guardian/    # pass, status, activate, JWT + clock correction
  internal/serverlist/  # remote settings, selection, failover, ping, disk cache
  internal/upstream/    # h2 framer client, streams, keepalive, bearer swap
  internal/socks/       # local SOCKS5 (CONNECT + DNS UDP associate)
  internal/httpproxy/   # local HTTP proxy (CONNECT + absolute-form HTTP)
  internal/tlsplit/     # TLS ClientHello fragmentation wrapper
  internal/tun/         # tun device + stack engine + proxy adapter
  internal/dns/         # bootstrap + strict in-tunnel DoH resolver, cache
  internal/netcfg/      # routes, rules, marks, nft, resolved, cleanup
  internal/gui/         # Windows GUI shell/tray client, mock, theme, prefs
  internal/settings/    # config + state files, defaults, validation
  internal/doctor/      # shared preflight checks
  internal/logging/     # ring buffer, journald text, redaction
  README.md  LICENSE  NOTICE  Makefile  install.sh  install.ps1  systemd/  docs/
  .github/workflows/ci.yml
```

Dependencies (all permissive): bubbletea/lipgloss/bubbles, `golang.org/x/net` (http2), `golang.org/x/sys` (SO_MARK, ioctl), `vishvananda/netlink`, `xjasonlyu/tun2socks` (MIT, gVisor netstack), `miekg/dns`, `google/nftables` **or** `nft -f -` via exec (decided at implementation; doctor verifies whichever is used). No cgo.

---

## 10. Build, install, uninstall

- `make build` → `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X internal/version.Version=…"`.
- `make test`, `make vet`, `make lint`.
- `install.sh` (sudo): create user `kumiho` (system, home `/var/lib/kumiho`); install binary; `RuntimeDirectory`/`StateDirectory` handled by unit; write default config + polkit rule; install & enable unit; optionally `usermod -aG kumiho $SUDO_USER`; `systemctl daemon-reload`.
- `install.sh uninstall [--purge]`: stop/disable, remove unit/polkit/binary/config; `--purge` removes `/var/lib/kumiho` and the user.
- First run: `sudo systemctl start kumiho`, then `kumiho login` and `kumiho doctor`.

---

## 11. Testing

**Unit (runs on any OS, no account):** authPW regression vectors (computed independently with Python hashlib once), Hawk header format, JWT parse + clock correction, challenge regex + PoW, server-list fixtures, selection/failover logic, SOCKS5 handshake over `net.Pipe`, refusal cache/health tracker, H2 client against an in-test `http2.Framer` server (CONNECT echo, flow control, PING, GOAWAY, concurrent-stream limit), DNS cache/TTL/truncation, netcfg rule generation golden files, IPC codec, settings round-trip, redaction.

**Integration (build tag `integration`, real account, run manually):** login → refresh → server list → proxy pass → H2 connect → exit check; quota headers; challenge path if triggered.

**Manual E2E checklist (on the server):** doctor; login; locations+ping; proxy-only connect (no TUN) and curl exit IP; full-tunnel connect; Tailscale SSH stays alive through connect, disconnect, reconnect, daemon restart, reboot; DNS resolution + `resolvectl status` shows split domains; MagicDNS still resolves; QUIC fails fast and TCP works; kill switch on → disconnect → traffic blocked, no leaks; kill switch off → straight fail; IPv6 blackholed; uninstall leaves no routes/rules/nft/DNS residue.

**CI:** GitHub Actions on linux/amd64: `go vet`, `go test ./...`, build for amd64+arm64. No secrets in CI.

---

## 12. Milestones and acceptance criteria

| Milestone | Deliverable | Acceptance |
|---|---|---|
| M0 | Skeleton, module, CI, LICENSE/NOTICE, `version`, `doctor` | builds, doctor runs, CI green |
| M1 | Settings/logging/daemon/IPC/TUI shell + login/logout | login E2E, tokens persisted, 2FA handled |
| M2 | Server list + Locations TUI + ping | locations listed, ping RTTs shown, selection persisted |
| M3 | H2 session + proxy pass + SOCKS5 + proxy-only connect/disconnect + exit check + quota | `curl` through SOCKS shows Mozilla exit; no TUN needed yet |
| M4 | TUN + netcfg + DNS + IPv6 + kill switch + full tunnel | full tunnel; Tailscale SSH never drops; DNS/MagicDNS correct; no leaks |
| M5 | Watchdog/rotation/renewal + Settings/Logs/Account TUI + install.sh/systemd/polkit + docs | reboot-safe, fatal errors surface, clean install/uninstall |

M3 is deliberately testable before any routing work, so the risky M4 can be validated with a known-good tunnel.

---

## 13. Risks and mitigations

| Risk | Mitigation |
|---|---|
| Mozilla changes/removes the free browser entitlement or blocks unofficial clients | protocol isolated in packages; fast update path; TUI shows exact errors |
| Fastly challenge escalates to captcha or an unknown type | solver fails with a clear error naming the type; no browser automation; `solve_challenges=off` restores the immediate failure |
| A DoH provider is blocked or unavailable | serial failover (configured → Cloudflare → Quad9), 30 min endpoint suppression, half-open probe; exhausted chain → SERVFAIL, never plaintext |
| TLS fingerprint filtering blocks the edge or control plane | best-effort `tls_split` (`first-byte`, `sni`); malformed or SNI-less ClientHellos pass through unsplit; off by default |
| Upstream proxy misconfiguration or credential leak | validation at config load and dial time; edge-only hop; hostname resolved remotely; credentials never logged and the URL never echoed |
| Local HTTP proxy exposed beyond loopback | loopback bind only, no authentication, same local-destination refusal as SOCKS; off by default; disable with `http_proxy=off` |
| Server-list cache stale or corrupt | fallback only on a failed fetch, disposable 0600 file, corrupt files ignored and deleted |
| Tailscale changes marks/rules | doctor detects drift; exclusion by CGNAT + interface fallback; never overwrite nonzero marks |
| systemd-resolved/polkit variations | doctor diagnoses; resolved-first design; manual fallback never touches resolv.conf |
| MTU 8500 issues on some networks | 8500 is the default; `mtu` is configurable (576–65535, next connect) and 1500 is a tested fallback, not the default |
| Clock skew breaks Hawk/JWT | doctor compares to Mozilla `Date`; JWT time-correction like the reference |
| Kubernetes/VPS missing TUN | doctor fails early with remediation; proxy-only mode still works |
| Browser sign-in and automatic account rotation are a ToS gray area: Kumiho is unofficial and completes a direct Firefox Accounts OAuth flow with the project's existing Firefox OAuth client and Mozilla's account page, and rotating several accounts can multiply entitlement use | browser sign-in uses Mozilla's own page, so captcha/password/TOTP/unblock stay interactive and are never automated; PKCE and `state` are mandatory and the code is single-use; rotation is opt-in and off by default, runs only before connect, switches at most once per connect, and is documented in README/PLAN/docs. Users bring their own accounts and accept the risk; no password is stored and no captcha-solving service is used |
| A pasted redirect URL or its authorization code is copied, logged or reused | the flow accepts only the complete redirect URL for its own `state`; the PKCE verifier stays in daemon memory, the code is single-use, and the flow expires after ten minutes. The URL is never logged or persisted, and desktop has no listener to probe |
| `tokens.json` rollback copy is stale or confusing after migration | `accounts.json` is authoritative and written atomically (0600); `tokens.json` is rewritten from the active account on login/switch/refresh/remove and removed on logout. A malformed/missing `accounts.json` is reported without overwriting `tokens.json`, so an old release can still recover the last active session |
| Rotation marks an account re-auth or leaves the tunnel on an exhausted account | a browser account whose refresh token is permanently rejected gets the durable top-level `reauth_required` flag, password tokens refresh once, unusable accounts are skipped with a notice, and connect fails with a clear error only when every account is exhausted or needs sign-in |
| Using an unofficial client is a ToS gray area | documented in README; users bring their own Firefox-account entitlement |

---

## 14. Open items — status 2026-10-05 (all resolved; implementation complete)

1. **Preflight output from the server — collected 2026-10-05** (`kumiho doctor` on an Ubuntu 24.04 test server): `/dev/net/tun` present (mode 666); nftables v1.0.9; `CAP_NET_ADMIN` only via the daemon/systemd unit (a non-root shell is expected to fail that check); systemd-resolved active with stub `resolv.conf`; Tailscale detected (tailscale0, table 52 routes, fwmark `0x80000` rules, MagicDNS `~ts.net`); IPv6 enabled (blackholed at connect); default route via Wi-Fi; clock within 1 s of Mozilla; FxA/Guardian/Remote Settings reachable. No Fastly challenge observed during login.
2. **GitHub username — resolved:** `benyamin-git` (module `github.com/benyamin-git/kumiho`).
3. **First-run values — resolved:** kill switch **ON**; autoconnect **ON** (decided 2026-10-05; originally OFF). Both follow "remember last state".
4. **Build host — resolved:** cross-compile on a Windows dev box with a portable Go toolchain; GitHub Actions CI builds linux/amd64 + linux/arm64 too.

---

## 15. Attribution

- Protocol behavior reference: `Vauth/FoxyVPN` (MIT) — Kotlin Android client.
- Additional behavioral reference: `UjuiUjuMandan/firefox-vpn-client` (no license — **behavior studied only, no code copied**).
- TUN stack: `xjasonlyu/tun2socks` (MIT) / gVisor netstack (Apache-2.0).
- Not affiliated with or endorsed by Mozilla.
