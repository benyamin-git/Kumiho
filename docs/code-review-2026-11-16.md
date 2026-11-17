# Kumiho — full code review

**Date:** 2026-11-16
**Commit:** `b706328` (`main`, clean tree, tag `v1.1.1`)
**Scope:** every hand-written production source file — Go (`cmd/`, `internal/`, `mobile/`), Android Kotlin (`android/app/src/main`), GUI HTML/CSS/JS under `internal/gui/web`, build/CI/installer material (`Makefile`, `.github/workflows`, `scripts/`, `install.*`, `systemd/`, `contrib/`), plus docs for drift.
**Excluded:** `vendor/` (third-party; only license presence and vendored semantics checked), generated flag assets, screenshots, test suites (read for intent, not audited), `dist/`, Gradle/build outputs.

**Method:** eight independent deep reviews (engine, network helpers, platform, IPC/CLI/TUI, Windows GUI, Android app, gomobile facade, build/CI) followed by source verification of every major finding in this session. Findings not directly verified here are marked *suspected*. CodeRabbit CLI is not installed on this machine, so this is a manual review; the project gate was run instead.

**Gate result (all green on `b706328`):**

| Check | Result |
|---|---|
| `make lint` (gofmt + `go vet ./...`) | pass (0 unformatted files) |
| `make test` (`go test ./...`) | pass |
| `go test -race -count=1 ./...` | pass |
| `make build` (linux/amd64, version `v1.1.1`) | pass |
| `GOOS=windows go build ./...` | pass |
| `GOOS=android GOARCH=arm64 go build ./...` | pass |
| GUI i18n parity (`en`/`fa`) | 549/549 keys, none empty |
| Android i18n parity | 767/767 keys, none empty |

**Severity:** `critical` = security hole, data loss, or breaks a release for everyone. `major` = real bug or security weakness with a plausible trigger. `minor` = edge-case correctness, hardening, or a documented invariant that does not hold. `info` = quality/compliance observation.

**Summary:** 0 critical, 26 major, 57 minor, 23 info (106 findings). The codebase is in good shape structurally — clean layering, strong tests, careful privilege handling — but there are two clusters that deserve attention before the next release: **credential/session lifetime** (engine pass handling, Android sign-out, IPC close panic) and **lifecycle arbitration** (network config apply/cleanup races, connect-attempt identity, main-thread shutdown).

---

## Top risks (fix first)

1. **`ipc.Client.Close` can panic "send on closed channel"** — CLI/TUI crash on quit while events stream (I1).
2. **Concurrent `Apply`/`Cleanup` can leave a host silently offline or leaking** — kill-switch toggle racing a disconnect (P1, E5).
3. **Cross-account pass reuse** — quota rotation can hand account A's bearer pass to the edge for account B (E1).
4. **Overlapping connect attempts orphan a live edge session** (E2).
5. **Upstream CONNECT resets stall 20 s and leave the edge stream open** (N1, N2).
6. **TUI renders remote text without stripping terminal escapes** (I3).
7. **Windows GUI action bridge is injected into any document, with no navigation/origin guard; elevated helpers resolve through `SystemRoot`/`PATH`** (G1, G3).
8. **Android sign-out does not reliably clear the WebView session** (A2), and service teardown blocks the main thread (A1, M1).
9. **Logging sanitizer has a confirmed data race on the secret-redaction path** (P2).
10. **Release/CI asserts almost nothing about what it ships; documented install pulls mutable `main` as root** (B1, B3).

---

## 1. Engine core (`internal/engine`, `internal/api`, `internal/guardian`)

### E1 [major] In-memory proxy pass survives an account switch
- **Location:** `internal/engine/passfile.go:157-164`, `internal/engine/connect.go:131`, `internal/engine/rotation.go:547-555`, `internal/engine/engine.go:1726-1728`, `engine.go:2115-2124`
- **Evidence:** `cachedPass()` returns `c.pass` with no account check; only the file path is account-bound. Rotation (`publishRotation`) and login completion delete the file but leave `c.pass` set. `Connect` calls `selectAccount` (line 112) *before* `cachedPass()` (line 131).
- **Impact:** with quota rotation on (or after signing into another account), the previous account's Guardian pass is presented for the new account. The edge accepts the H2 session and rejects CONNECT → watchdog redial + up to a minute of no traffic, plus another account's credential sent to Mozilla.
- **Fix:** clear `c.pass`/`c.passPrepared` wherever the active account id changes (`publishRotation`, `dropActiveIf`, `completeLogin`, browser completion), or store the account id on the pass and verify it in `cachedPass`.

### E2 [major] Overlapping connect attempts publish on shared `state`, leaking one session
- **Location:** `internal/engine/connect.go:99-109`, `:243-253`, `:267-280`
- **Evidence:** both publish points gate on `c.state != StateConnecting`; the entry guard only rejects `session != nil || state == StateConnecting`. There is no attempt generation, unlike the watchdog's `installSession` which closes the old session.
- **Impact:** disconnect during a slow dial + immediate reconnect lets attempt A see B's `CONNECTING` state and publish `session = sess_A`; B overwrites it without closing A → one orphaned TLS/H2 edge session (fd + goroutines), and the loser can null the winner's live session pointer.
- **Fix:** add a `connectGen` bumped on connect entry and `stopTunnel`; require `gen == c.connectGen && state == CONNECTING` at both publish points; close any session being overwritten under the lock.

### E3 [major] `fetchPass` dereferences a nil token set after a concurrent sign-out
- **Location:** `internal/engine/connect.go:834-846`, `engine.go:2023`
- **Evidence:** after `refreshAccessToken`, `tokens = c.currentTokens()` is used as `tokens.AccessToken` with no nil check; `clearSessionIf` sets `c.tokens = nil` from logout at any time. Sibling paths (`pollQuotaOnce`, `Prepare`) do check.
- **Impact:** logout landing in that window panics the daemon (no `recover` in the IPC/daemon path), reachable from connect, watchdog redial and `ping`.
- **Fix:** nil-check `currentTokens()` and return `CodeNotAuthenticated`; snapshot the token once under `c.mu`.

### E4 [minor] A prepared pass can be re-written after logout
- **Location:** `internal/engine/passfile.go:58-66`, `region.go:157-165`, `engine.go:2051-2065`
- **Evidence:** `persistPass`/`Prepare` capture the account under `c.mu`, then write the file outside it with no `authGen` fence; `clearSessionIf` deletes the file under its generation check but cannot stop an in-flight writer.
- **Impact:** a logout/account removal documented to delete `pass.json` can leave a usable bearer pass (and `c.pass`) behind (*suspected*: narrow window between unlock and rename).
- **Fix:** capture `authGen` with the email and drop the write if it changed.

### E5 [minor] Kill-switch re-apply races tunnel teardown
- **Location:** `internal/engine/connect.go:374-388` vs `:337-355`
- **Evidence:** `ReapplyNetcfg` copies `r.nc` then calls `Apply` outside the lock; `Close` nils `r.nc` then calls `Cleanup` outside the lock. Nothing serializes the platform calls.
- **Impact:** a settings change landing after teardown leaves nftables kill-switch/routing rules installed on a disconnected host (also reported as P1 from the platform side).
- **Fix:** per-resource mutex + `closed` flag held across `Apply`/`Cleanup`.

### E6 [minor] Account mutations are published before persistence with partial rollback
- **Location:** `engine.go:1778-1786` (`UseAccount`), `:1690-1721` (`completeLogin`), contrast `rotation.go:547-557`
- **Evidence:** `setActiveLocked` mutates live state before `saveAccountsLocked`; on error `UseAccount` restores nothing, `completeLogin` restores only `activeID`/`Active` and leaves the appended row.
- **Impact:** a read-only/full disk makes the command report failure while in-memory state (and next connect) uses the change; it silently reverts on restart.
- **Fix:** mutate a copy and swap in after a successful write, as `publishRotation` does.

### E7 [minor] Account removal does not revoke the FxA refresh token
- **Location:** `engine.go:1793-1839` vs logout at `:1950-1999`
- **Evidence:** `Logout` calls `revokeRefreshTokens()`; `RemoveAccount` only drops the row and deletes files, including the last-account branch that clears the session.
- **Impact:** a live server-side FxA grant remains after "remove account", invisible to the user.
- **Fix:** best-effort `Destroy` for the removed account's refresh token before dropping it.

### E8 [info] Nil log ring accepted but dereferenced in ~128 places
- **Location:** `engine.go:310-340` (nil tolerated), e.g. `connect.go:229`, `engine.go:2331`
- **Impact:** a future embedder calling `New(store, nil, …)` panics on the first log.
- **Fix:** normalise to a real ring in `New`, or centralise through a `logf` helper.

### E9 [info] Guardian error bodies echo up to 64 KiB into logs and client errors
- **Location:** `internal/guardian/guardian.go:24`, `:238`, `:255-257`
- **Impact:** a hostile/proxied endpoint can bloat the 2000-entry ring and bury diagnostics.
- **Fix:** truncate the echoed body (≈512 B).

---

## 2. Network helpers (`upstream`, `dns`, `serverlist`, `fastly`, `socks`, `httpproxy`, `tlsplit`, `relay`, `fxa`, `tun`)

### N1 [major] A pending `OpenStream` is not woken by RST_STREAM, GOAWAY or session death
- **Location:** `internal/upstream/session.go:295-302`, `stream.go:91-99`, `session.go:443-449`
- **Evidence:** `OpenStream` selects only on `ctx.Done()`, the 20 s timer and `st.resp`; `appendError` sets `st.err` and broadcasts a cond, but never calls `signalResp`. `Session.fail` loops `appendError` only.
- **Impact:** every pre-header reset (`REFUSED_STREAM`, `CANCEL`, GOAWAY, session teardown) becomes a 20 s stall and a `ConnectTimeout`; `ErrStreamRefused`/`ErrDraining` are unreachable for that case, SOCKS reports "TTL expired", and a proxy slot is held for the full timeout.
- **Fix:** deliver the error on `st.resp` from `appendError` when `!gotResp` (non-blocking, same guard as `signalResp`), or select on a per-stream done channel.

### N2 [major] Timeout/cancel abandons a stream without RST_STREAM
- **Location:** `internal/upstream/session.go:296-301`, `:319-336`
- **Evidence:** both timeout paths call `removeStream`, which deletes the map entry and refunds credit; the only RST writer is `stream.Close()`.
- **Impact:** the edge keeps a live CONNECT tunnel and fd; local slots are refunded while the peer still counts the stream against `MAX_CONCURRENT_STREAMS`, so repeated timeouts degrade and eventually exhaust the session.
- **Fix:** call `st.Close()` (sends `RST_STREAM(CANCEL)` once) instead of `removeStream` in both paths.

### N3 [major] Unvalidated SETTINGS lets a peer pin a core in `stream.Write`
- **Location:** `internal/upstream/session.go:573-574`, `stream.go:183-199`, `window.go:69-81`
- **Evidence:** `peerMaxFrameSize` is stored unchecked; `chunk` can become 0 and `tryTake(0)` returns 0 forever, so the loop `continue`s without I/O.
- **Impact:** a non-conforming/hostile authenticated edge can busy-loop one goroutine per writing flow (no I/O means TCP cannot fail it). RFC 9113 §6.5.2 requires a connection error for out-of-range values.
- **Fix:** validate `SETTINGS_MAX_FRAME_SIZE`/`INITIAL_WINDOW_SIZE` and fail the session; guard `chunk <= 0`.

### N4 [major] Unbounded parser recursion on remote `filter_expression` can abort the daemon
- **Location:** `internal/serverlist/filter.go:457-497`, fed from `serverlist.go:195` (response capped at 4 MiB, `:26`)
- **Evidence:** `parsePrimary` recurses through `parseOr` for every `(`, with no depth counter. A ~400 KB expression of nested parens grows the goroutine stack by ~512 MB (measured by the reviewer; input is well inside the 4 MiB cap).
- **Impact:** a compromised/erroneous Remote Settings record causes the runtime's fatal "goroutine stack exceeds 1 GB" abort — not recoverable, not the documented fail-open.
- **Fix:** carry a depth counter and fail beyond ~64; the existing fail-open path then includes the record and logs.

### N5 [minor] Every EDNS0 response carries two OPT records and clears DO
- **Location:** `internal/dns/dns.go:552-559`
- **Evidence:** `resp` is the upstream DoH reply (which echoes the forwarded query's OPT) and `SetEdns0` appends another; the added one has `DO=false`.
- **Impact:** RFC 6891 requires exactly one OPT; strict stubs may reject/log it, and DNSSEC capability is misadvertised for DO=1 clients.
- **Fix:** strip existing OPT from `resp.Extra` before adding, and propagate `opt.Do()`.

### N6 [minor] DNS cache key ignores QCLASS and DO/CD bits
- **Location:** `internal/dns/dns.go:397-399`
- **Impact:** CHAOS/HS queries collide with IN answers; a DO=0 cached answer can be served to a DO=1 client without RRSIGs until TTL expiry.
- **Fix:** include Qclass and the DO/CD bits in the key.

### N7 [minor] DoH answers are cached without matching them to the query
- **Location:** `internal/dns/dns.go:322-327`
- **Impact:** the only anti-poisoning guarantee is TLS to an IP literal; a mismatched answer would be installed under the requested name.
- **Fix:** reject answers whose question/name/type differ before caching.

### N8 [minor] Fastly challenge count is unbounded within a round
- **Location:** `internal/fastly/fastly.go:222-235`
- **Impact:** a hostile challenge page can supply tens of thousands of `pow` entries (each ~3844 hashes) inside the 2 MiB body cap; the budget is only checked between rounds.
- **Fix:** cap challenge count and check budget/`ctx.Err()` inside the loop.

### N9 [info] Per-stream read buffer capacity is retained for the stream lifetime
- **Location:** `internal/upstream/stream.go:108-113`
- **Impact:** up to the 16 MiB stream window per idle stream; hundreds of MiB across 512 flows after a burst.
- **Fix:** nil/copy-down the buffer once fully drained.

**Verified clean:** SOCKS5 and HTTP proxies are loopback-only (`socks_bind` is hard-validated to `127.0.0.1`, `settings.go:135`), the HTTP proxy rejects origin-form requests, credentials/hop-by-hop headers are stripped, logs redact URIs, TLS uses `ServerName` + ALPN `h2`, `tlsplit` bounds-checks its parser, and relay/FxA crypto (Hawk, PKCE, HKDF) matches the specs.

---

## 3. Platform, netcfg, doctor, logging

### P1 [major] `Apply`/`Cleanup` are not serialized; the Windows comment claims they are
- **Location:** `internal/platform/windows/netcfg.go:56-58`, `:105-157`, `internal/platform/linux/netcfg.go:14-16`; reachable via `internal/engine/connect.go:374-388` and `:337-362`
- **Evidence:** `wfpMu` is taken only inside `applyKillSwitch`/`clearKillSwitch`, not around the address/MTU/route/DNS mutations; Linux's adapter has no lock at all. `ReapplyNetcfg` and `Disconnect` both detach the applier under `c.mu` and then call it outside the lock; IPC handles each connection in its own goroutine.
- **Impact:** a `settings_set kill_switch` racing a disconnect can interleave. On Linux the worst case leaves nft marking every packet while tables/rules are being flushed → host offline after "disconnect", with no error. On Windows the residue is routes (metric 9999, `/0` on a removed adapter).
- **Fix:** a per-instance mutex held across the whole `Apply` and `Cleanup`, or an ownership/closed flag that makes a post-teardown `Apply` fail.

### P2 [major] Logging sanitizer is a confirmed data race
- **Location:** `internal/logging/logging.go:154-168`
- **Evidence:** `Log` does `msg = r.sanitize(msg)` *before* taking `r.mu`; `SetSanitize` writes `r.sanitize` *under* `r.mu`. `SetSanitize` is called at runtime from `settings_set redact_targets` (`engine.go:2329-2334`) on a per-connection goroutine.
- **Impact:** torn read of a two-word func value → wrong sanitizer or a crash; the sanitizer is the redaction path for emails/JWTs/tokens. `go test -race` only passes because the test is sequential.
- **Fix:** snapshot the func under the lock (or `atomic.Pointer`); add a concurrent test.

### P3 [major] Linux `Cleanup` cannot fail and its result is never verified
- **Location:** `internal/netcfg/netcfg.go:156-177`, `:243-253`, `internal/engine/connect.go:354`
- **Evidence:** `Cleanup` logs each command error and returns; the interface contract says "never fails"; the engine has already set `StateIdle`.
- **Impact:** `ip rule del` returns the same error for "absent" (normal) and "failed"; if rule 1100/1200 or the blackhole survives, the user is told "disconnected" but has no network. The only recovery is `kumiho cleanup`.
- **Fix:** verify end state (`nft list table inet kumiho`, `ip rule show`), log/surface residue, and delete the blackhole rule before flushing its table.

### P4 [minor] Windows cleanup deletes any route with metric 9999 on a non-tunnel interface
- **Location:** `internal/platform/windows/netcfg.go:31`, `:389-398`
- **Impact:** ownership inferred from a magic metric; another VPN/operator route with 9999 is deleted on every connect and `kumiho cleanup`.
- **Fix:** record the exact `(prefix, next hop, LUID)` tuples created and delete only those.

### P5 [minor] Windows connect hard-fails on IPv6 configuration errors (*suspected*)
- **Location:** `internal/platform/windows/netcfg.go:109-113`, `:146-151`
- **Impact:** on hosts with IPv6 fully disabled, `setMTU(AF_INET6)`/`::/0` failures turn a usable IPv4 tunnel into a failed connect (and clear the kill switch).
- **Fix:** tolerate IPv6 failure when the host has no IPv6 default route, mirroring DNS tolerance.

### P6 [minor] Android tunnel DNS address duplicated in four places
- **Location:** `internal/platform/android/provider.go:66`, `mobile/routes.go:25`, `mobile/doctor.go:18`, `android/.../VpnInterfaceSpec.kt:29`
- **Impact:** one-line drift blackholes all DNS device-wide with no error; only the Go side has a test.
- **Fix:** send the address through the existing `Configure` JSON and delete the Kotlin constant; add a cross-check test.

### P7 [minor] `ipv6Filter.Read` drops terminal errors and can spin on `(0, nil)`
- **Location:** `internal/platform/android/device.go:34-42`
- **Impact:** an IPv6 packet returned together with an error silently discards the error; a `(0, nil)` read busy-loops a CPU core.
- **Fix:** return `err` when `n > 0`, and `return 0, nil` for empty reads.

### P8 [minor] `unsupported.DefaultPaths` ignores the cache-dir error and uses the shared temp dir
- **Location:** `internal/platform/unsupported/provider.go:34-44`, `internal/ipc/unix.go:18-31`
- **Impact:** with `$HOME` unset, `state.json`/`tokens.json` become relative paths under the daemon CWD; the control socket lands in `/tmp/kumiho`, which another local user can pre-create/own, enabling TOCTOU on a root-owned socket. Non-Linux/Windows builds only.
- **Fix:** fall back like the config branch, and use a per-user runtime dir (or `0700` + ownership check).

### P9 [minor] Doctor's `socks-port` check hardcodes 1080
- **Location:** `internal/doctor/doctor.go:431-447` vs `settings.go:80`, `:660`
- **Impact:** with `socks_port` overridden, doctor checks a port nobody will bind.
- **Fix:** probe the effective `SocksBind:SocksPort`.

### P10 [minor] Doctor remediation widens `/dev/net/tun` to 0666
- **Location:** `internal/doctor/doctor.go:192-198`
- **Impact:** copy-paste advice lets every local user create arbitrary TUN interfaces.
- **Fix:** recommend `chown root:kumiho` + `0660` or a udev rule.

### P11 [minor] `kumiho cleanup` has no liveness guard
- **Location:** `internal/cli/cleanup.go:43-67`
- **Evidence:** it builds its own `NetConfig` and calls `Cleanup`/`CleanupStale` unconditionally; it never probes the control endpoint.
- **Impact:** with the daemon connected, rules and routes are removed while status still says connected → traffic egresses unmarked (leak) on Linux, or the host is fully offline on Windows.
- **Fix:** refuse while the control endpoint answers, or route the request through IPC so the daemon disconnects.

### P12 [minor] A failed DNS step still reports a healthy connect
- **Location:** `internal/netcfg/netcfg.go:231-238`
- **Impact:** `resolvectl` failure leaves DNS outside the tunnel with only an info log; the engine reports connected.
- **Fix:** surface "resolvectl ran and failed" as a warning in the connect result/status.

### P13 [info] Kill-switch crash asymmetry (Linux persists, Windows is dynamic)
- **Location:** `internal/platform/windows/wfp.go:10-14`; Linux nft state in `netcfg.go:98-111`, `:144-149`
- **Impact:** a Windows daemon crash leaves traffic unprotected until restart; Linux leaves a persistent kill switch. Worth stating in user docs, not just code comments.

### P14 [info] Root helpers resolved through `PATH`; EPERM hints are English-only
- **Location:** `internal/netcfg/exec_linux.go:15-23`, `netcfg.go:223-235`, `:303-309`
- **Impact:** a daemon with an attacker-influenced `PATH` executes attacker binaries as root; non-English locales lose the permission hint.
- **Fix:** resolve absolute helper paths once; match `errors.Is(err, syscall.EPERM)`.

### P15 [info] nft script interpolation is latent injection
- **Location:** `internal/netcfg/netcfg.go:71-72`, `:83-89`, `:117-122`
- **Impact:** `TunAddr`/CIDRs are validated today (`settings.go:175-179`, constant tun address), so no live path; the exported generators are unsafe by construction.
- **Fix:** validate/format inside `NFTConfig`.

### P16 [info] Log sink write happens under the ring mutex
- **Location:** `internal/logging/logging.go:189-191`
- **Impact:** a stalled sink (journald/disk/share) stalls all logging and every snapshot caller.
- **Fix:** hand lines to a bounded writer goroutine.

### P17 [info] `RuleCommands` claims idempotence but uses `ip rule add`
- **Location:** `internal/netcfg/netcfg.go:124-152`, cleanup at `:164-177`
- **Impact:** surviving duplicates grow across crash/reconnect cycles; only route commands use `replace`.
- **Fix:** delete-until-absent or check existence before add.

### P18 [info] Doctor probes ignore marks and the kill switch
- **Location:** `internal/doctor/doctor.go:471-472`, `:525-526`
- **Impact:** while connected with the kill switch on, endpoints/clock-skew report FAIL for a healthy tunnel.
- **Fix:** probe via the daemon proxy, or mark "not evaluated while the tunnel is up".

---

## 4. IPC, daemon, CLI, TUI, settings

### I1 [major] `Client.Close` races the read loop and panics
- **Location:** `internal/ipc/client.go:139-142` vs `:156`
- **Evidence:** `readLoop` sends on `c.events` with no lock; `fail` (serialized only by `closeOnce`) closes `c.events`. Reproduced by the reviewer with an overlay test: `panic: send on closed channel`.
- **Impact:** CLI/TUI crash on quit while the daemon broadcasts (it broadcasts `status` to all connections on every state change, `daemon/run.go:159-161`) — a nondeterministic panic at the worst moment.
- **Fix:** guard sends and close with `c.mu`/a closed flag, or let `readLoop` be the sole closer.

### I2 [major] No socket write deadlines; status broadcast runs inline with engine state changes
- **Location:** `internal/ipc/server.go:144-147`, `daemon/run.go:159-161`
- **Evidence:** `Conn.write` encodes with no deadline; `broadcastStatus` is the engine's `OnChange` and writes to every connection sequentially.
- **Impact:** one client that stops reading (suspended laptop, stopped process, malicious group member) blocks `notify()` forever and stalls status delivery to everyone — a daemon-wide freeze.
- **Fix:** per-write deadline + drop on timeout; fan out through per-connection bounded queues.

### I3 [major] TUI prints remote-controlled text without stripping terminal escapes
- **Location:** `internal/tui/tui.go:1028`, `:951-957`, `:905-933`, `internal/tui/logs.go:300-310`, `locations.go:592`, `account.go:521-537`; CLI `logs.go:56,87`, `status.go:59-93`, `locations.go:97-103`
- **Evidence:** no sanitizer exists in `tui`/`cli`; the log sanitizer only redacts secrets/IPs. Remote bodies reach these paths verbatim: FxA/Guardian errors (`fxa/client.go:497` → `APIError.Message`), exit country (`engine/connect.go:1126-1130`), and server-list names.
- **Impact:** in the blocked-region scenario this app targets (hostile middlebox/proxy returning an error page, or a compromised list), an attacker can drive cursor/screen manipulation, title changes and OSC 52 clipboard writes in the user's terminal. GUI and Android are unaffected.
- **Fix:** strip C0/C1 and ESC-introduced sequences at the render boundary (and in CLI printers), ideally once in `logging.Ring.Log` plus display helpers.

### I4 [major] Per-connection synchronous dispatch causes head-of-line blocking
- **Location:** `internal/daemon/run.go:178-184` with `:500-519`, `:589-605`
- **Evidence:** `dispatch` runs inline; connect blocks up to ~30 s, prepare 90 s, region check 25 s, relay test 20 s. The TUI issues new calls on the same connection with 5–15 s budgets.
- **Impact:** pressing refresh/settings during connect reliably shows "context deadline exceeded" although the daemon is healthy; worse, a client that gives up cancels `connCtx` and therefore the connect itself.
- **Fix:** reply immediately and stream progress (status events already exist), or run long handlers off the connection loop with per-conn state; treat deadline-exceeded as "still running, re-check status".

### I5 [minor] No inbound frame-size limit and no connection cap
- **Location:** `internal/ipc/server.go:41-45`, `:88-94`
- **Impact:** any group member can grow the heap with one long line or exhaust fds.
- **Fix:** limited reader (~1 MiB/frame) + connection cap.

### I6 [minor] No protocol version gate, reply-type check, or required-payload check
- **Location:** `internal/ipc/ipc.go:94-101`, `client.go:100-107`
- **Evidence:** `env.V` is only written, never read; no client sends `hello`; empty payloads decode as success.
- **Impact:** a type/version mismatch is silent — e.g. an empty status reply prints an all-zero snapshot and exits 0.
- **Fix:** send `hello` and compare `Protocol`, verify `resp.Type`, error on missing required payloads.

### I7 [minor] Malformed frame closes the connection silently; handler errors are discarded
- **Location:** `internal/daemon/run.go:178-184`, `:44`
- **Impact:** no `error` envelope, no log line; protocol faults are undiagnosable.
- **Fix:** distinguish decode errors from EOF, reply `bad_request`, log with peer UID.

### I8 [minor] An unknown log level from a newer daemon breaks whole snapshots
- **Location:** `internal/logging/logging.go:74-86`, `daemon/run.go:570-572`, `cli/logs.go:50-54`
- **Impact:** adding a new severity is an inadvertent protocol break: `kumiho logs` prints one error and no lines.
- **Fix:** fall back to `Info` on unknown level names.

### I9 [minor] `controlErrorReply` forwards raw error text (paths, OS detail)
- **Location:** `internal/daemon/run.go:190-196`
- **Fix:** generic message to clients, full error to the ring.

### I10 [minor] Interactive password sign-in shares one 60 s context across prompts
- **Location:** `internal/cli/login.go:88` with calls at `:200-224`, `cli/client.go:28-30`
- **Impact:** the email-link 2FA path routinely exceeds it and the command dies mid-flow with "context deadline exceeded".
- **Fix:** mint a fresh bounded context per request, like the browser path.

### I11 [minor] Browser sign-in's 10-minute deadline is not enforced while waiting for input
- **Location:** `internal/cli/login.go:308-316`, `:359-369`
- **Impact:** `kumiho login --browser` blocks indefinitely on an idle terminal.
- **Fix:** read with a deadline or select on a timer.

### I12 [minor] `accounts remove` only confirms for the active account among several
- **Location:** `internal/cli/accounts.go:172-185`
- **Impact:** removing an inactive or last account destroys its tokens with no prompt, contrary to the `--yes` help.
- **Fix:** prompt for every removal unless `--yes`.

### I13 [minor] `dialDaemon` drops the transport error
- **Location:** `internal/cli/client.go:18-23`
- **Impact:** "permission denied" is indistinguishable from "daemon not running".
- **Fix:** include the error.

### I14 [minor] `proxy_only` is accepted and reported but never applied
- **Location:** `internal/settings/settings.go:81`, `:505`, `:548-576`; no reader in `effectiveConfigLocked` (`:414-489`); status display at `engine/engine.go:2159`
- **Impact:** `settings_set proxy_only on` returns success and status reports `proxy_only: true` while the daemon brings up the full tunnel and rewrites routing.
- **Fix:** apply it on connect when the request omits the field, expose it in settings views, or reject the key.

### I15 [minor] `State()` returns a shallow copy; slices/map alias live state
- **Location:** `internal/settings/settings.go:333-338`
- **Impact:** latent: callers iterate `Failures` outside the store lock while it is mutated under it; `ExcludeCIDRs` is defensively copied elsewhere, showing the hazard is known.
- **Fix:** deep-copy or document the view contract.

### I16 [minor] TUI Esc during a pending login does not cancel the daemon flow
- **Location:** `internal/tui/tui.go:571-581`, `:583-586`, `:744-783`
- **Impact:** the user is pulled back into the old password/2FA step when the late reply lands; Enter is ignored until then.
- **Fix:** reset `busy` and send `login_cancel`; ignore replies for a stale attempt.

### I17 [minor] TUI subscribes to logs after leaving the screen
- **Location:** `internal/tui/tui.go:332-342` vs `tui/logs.go:166-168`
- **Impact:** the daemon streams logs for the rest of the session to a screen nobody is viewing.
- **Fix:** gate the subscribe on the current screen; unsubscribe unconditionally on leave.

### I18 [info] `settings set relay_token <value>` puts the secret in argv
- **Location:** `internal/cli/settings.go:109-112`
- **Impact:** visible in `ps`/`/proc/<pid>/cmdline` and shell history; the prompt path exists.
- **Fix:** reject a positional value for `relay_token`.

### I19 [info] `PeerUID` is collected but unused; no audit trail of destructive commands
- **Location:** `internal/ipc/server.go:125-132`, `daemon/run.go:579-583`
- **Impact:** group members can run every state-changing command with no record of who did.
- **Fix:** log peer UID on connect and on destructive commands.

### I20 [info] Durability/corruption edges in settings
- **Location:** `internal/settings/settings.go:313-319`, `:817-845`
- **Impact:** no parent-directory fsync after rename; a corrupt `state.json` makes the daemon fail to start with no quarantine.
- **Fix:** fsync the dir; rename a bad file aside and start from defaults with a warning.

---

## 5. Windows GUI (`internal/gui`, `cmd/kumiho-gui*`)

### G1 [major] The `kumihoAction` bridge is installed in every document, with no navigation or origin guard
- **Location:** `internal/gui/host_windows.go:345`, `:424`; vendored `go-webview2/webview.go:459-478`
- **Evidence:** `Bind` installs the bridge via `AddScriptToExecuteOnDocumentCreated`, so it exists in *any* document the WebView renders. `handleAction(envelope string)` validates nothing about source/origin. A repo-wide grep finds no `NavigationStarting`/`NavigationCompleted`/`NewWindowRequested` handling and no `AllowExternalDrop` setter anywhere in the vendored wrapper (WebView2's default is external drop enabled).
- **Impact:** a dropped link/saved `.html` becomes a same-privilege document that can call `connect`, `disconnect`, `logout`, `settings_set`, `open_external`, `quit` and `service_start`/`service_stop`/`cleanup` (the latter raising UAC). The per-run URL token does not help — the binding, not HTTP, is the authority.
- **Fix:** cancel any `NavigationStarting` URI that is not exactly the loopback origin + session token, cancel `NewWindowRequested`, set `AllowExternalDrop(FALSE)`, and/or gate the message handler on the message source origin.

### G2 [major] Dev harness is an unauthenticated control surface on `0.0.0.0:7407`
- **Location:** `cmd/kumiho-gui-web/main.go:97-101`, `internal/gui/web/bridge.go:398-404`, `:505-512`
- **Evidence:** `Options.Token` is empty (gate disabled); `serveAction` checks only `Method == POST`; `prefs_set` persists to the real `gui.toml`; `/dev/*` endpoints are also exposed.
- **Impact:** anyone on the LAN, and any website the developer visits (simple `text/plain` POST, no preflight), can drive the harness and durably rewrite theme/language/autostart/onboarded state. Dev-only, but the project keeps it bound to all interfaces daily.
- **Fix:** require a per-run random token and an Origin/Host check, or default to `127.0.0.1` with an explicit opt-in for LAN.

### G3 [major] Elevated helper resolution trusts `SystemRoot` and `PATH`
- **Location:** `internal/gui/service_windows.go:86-94`, `:109-119`, `elevate_windows.go:79`
- **Evidence:** `scPath()` joins the inherited `SystemRoot` env var; `kumihoExe()` falls back to `exec.LookPath("kumiho.exe")`; both are launched with the `runas` verb after UAC consent.
- **Impact:** `setx SystemRoot C:\Users\me\evil` (or a modified shortcut) executes a user-writable `sc.exe` elevated; the portable-zip layout plus the PATH fallback repeats the same weakness for `kumiho.exe`.
- **Fix:** use `GetSystemDirectoryW`; accept only the installed-service path or validate the publisher signature before elevating.

### G4 [major] Background account refresh inherits a context cancelled on return
- **Location:** `internal/gui/controller.go:1082-1084`, `:1271`, `:1793-1805`; callers `:895-896`, `:1291`
- **Evidence:** `ensureAccount(ctx)` starts a goroutine deriving `context.WithTimeout(ctx, …)` from the caller's context, which `dispatchLogin`/`afterAccountChange`/`dispatchLogout` cancel on return.
- **Impact:** right after sign-in/sign-out/switch, the account fetch is aborted and the Account screen shows `could not load the account: context canceled` in the error tone; the next poll can short-circuit and leave it visible until auth changes.
- **Fix:** give `ensureAccount` its own background lifetime, and/or skip the notice for `context.Canceled`.

### G5 [minor] `settings_set` echoes the write-only relay token into broadcast state
- **Location:** `internal/gui/controller.go:836-845`, `internal/gui/web/static/js/screens/settings.js:678-693`
- **Evidence:** the saved notice carries `{"key": p.Key, "value": p.Value}`; for `relay_token` that copies the secret into `State`, defeating the daemon's write-only contract (`api/api.go:242`).
- **Impact:** the token becomes readable from the page/store and from anything observing the state stream (e.g. harness `/events`).
- **Fix:** special-case secret keys and omit the value.

### G6 [minor] Loopback server hardening gaps
- **Location:** `internal/gui/web/server.go:53`, `:219-221`, token fallback in `newSessionToken`
- **Evidence:** CSP lacks `frame-ancestors`/`base-uri`/`form-action`; no `Referrer-Policy`; the `Host` header is never validated; a `crypto/rand` failure returns the fixed token `"kumiho"`.
- **Impact:** defense-in-depth only (static GET/HEAD + 128-bit token), but the gate silently degrades instead of failing closed.
- **Fix:** extend CSP, add `Referrer-Policy`, fail startup on rand failure.

### G7 [info] `open_external` fires without a user gesture when a login URL appears
- **Location:** `internal/gui/web/static/js/screens/onboarding.js:1490-1493`, `account-section.js:801-804`
- **Impact:** an unattended session can be steered to an arbitrary https page (scheme is validated host-side).
- **Fix:** require one explicit click per login id if strict gesture semantics are wanted.

### G8 [info] `prefs_set` does disk and registry I/O on the WebView callback thread
- **Location:** `internal/gui/host_windows.go:905-965`
- **Impact:** a slow profile/AV scan freezes the UI on theme/language/autostart changes.
- **Fix:** run `applyPrefs` on a goroutine and publish the outcome, like `serviceAction`.

**Verified clean:** zero `innerHTML`/`insertAdjacentHTML`/`document.write`/`eval`/`new Function` in the assets; all server strings go through `textContent`/`setAttribute`; constant-time token compare; `fs.ValidPath` traversal rejection; nonce-based `<style` stripping; strict content types; no CORS; DevTools/context menus off; JSON-escaped `Eval`.

---

## 6. Android app (`android/app/src/main`)

### A1 [major] `onDestroy` blocks the main thread on a full native engine shutdown
- **Location:** `android/.../vpn/KumihoVpnService.kt:212-226`, `engine/EngineClient.kt:433-441`, `mobile/mobile.go:61-73`
- **Evidence:** `runBlocking { engine.close() }` in `onDestroy`; `close()` waits on `startLock` and runs `transport.close()` on `NonCancellable`; `Mobile.Close` joins `f.wg` after tearing down the session, listeners, DNS and pumps.
- **Impact:** service destruction (revocation, another VPN, Always-on restart) freezes the process/main thread past the 5 s ANR window, and the `NonCancellable` makes it uninterruptible.
- **Fix:** never `runBlocking` in `onDestroy`; call `Mobile.Close` from a detached/bounded context, or drop the join (calling close is what frees the fd; waiting is not required).

### A2 [major] WebView cookies are not reliably cleared on sign-out
- **Location:** `android/.../ui/account/MozillaAccountWebView.kt:130-134`
- **Evidence:** `removeAllCookies(null)` is asynchronous (the callback is the only completion signal), followed immediately by `flush()`.
- **Impact:** the jar can be flushed before removal applies, so an authenticated `accounts.firefox.com` session can outlive local sign-out — exactly what the doc comment promises it will not.
- **Fix:** `removeAllCookies { flush() }`, or make it `suspend` and await completion before finishing sign-out.

### A3 [minor] A failed update check still burns the 24-hour budget
- **Location:** `android/.../update/UpdateManager.kt:76-90`
- **Impact:** one offline launch suppresses launch checks for a day, contrary to the comment.
- **Fix:** persist the timestamp only on success.

### A4 [minor] Exported BROWSABLE `kumiho://tv/connect` auto-connects
- **Location:** `AndroidManifest.xml:57-62`, `MainActivity.kt:71-79`, `ui/tv/TvRoot.kt:46-60`, `ui/tv/dashboard/TvDashboardRoute.kt:49-61`
- **Impact:** any app or web page can bring the TV app forward and make it establish the tunnel once consent was granted (nuisance/resource use, unexpected state change).
- **Fix:** treat the link as navigation only and require a remote/keypress confirmation.

### A5 [minor] The "retry a fatal connect once per fresh start" rule is not implemented
- **Location:** `vpn/KumihoVpnService.kt:289-294`, `vpn/AlwaysOnPolicy.kt:26-32`
- **Impact:** `freshStart = true` is hardcoded; either FATAL is unobservable after process death (dead arm + misleading comment) or every restart re-attempts a fatal connect.
- **Fix:** persist the marker and pass the real value, or delete the parameter.

### A6 [minor] TV channel publishing does blocking provider I/O on the main thread
- **Location:** `MainActivity.kt:43`, `tv/TvChannelPublisher.kt:33-100`
- **Impact:** every TV cold start/recreation blocks the first frame on a TV Provider IPC; an ANR candidate if the provider hangs, silently swallowed by `runCatching`.
- **Fix:** `launch(Dispatchers.IO)` or make `publish` suspend with `Dispatchers.IO`.

### A7 [minor] The logs "safety net" unsubscribe is a no-op
- **Location:** `ui/logs/LogsViewModel.kt:96-111`, `:217-221`
- **Evidence:** `onCleared()` calls `onHidden()`, which `viewModelScope.launch`es after that scope is cancelled.
- **Impact:** destroying the activity with the log console visible (normal case: rotate/back, service keeps process alive) leaves the engine subscription and buffering running.
- **Fix:** unsubscribe through an app-scoped scope or a synchronous entry point.

### A8 [minor] Setup screens miss their initial `ON_RESUME`
- **Location:** `ui/setup/SetupFlow.kt:237-250` (same in `SetupHost.kt:144-157`)
- **Impact:** an observer added while already RESUMED never receives the event, so resumed-only permission polling stays off until the user leaves and returns.
- **Fix:** evaluate the current lifecycle state on registration or use `LifecycleResumeEffect`/`repeatOnLifecycle`.

### A9 [info] `setUnderlyingNetworks` is never called
- **Location:** `vpn/KumihoDeviceBridge.kt:60-93`
- **Impact:** metered/roaming and bandwidth capabilities reported to apps are wrong; routing still works via the system default.
- **Fix:** call `setUnderlyingNetworks(null)` or track the active network.

### A10 [info] Engine-keyed ViewModels accumulate per rebind
- **Location:** `ui/dashboard/DashboardScreen.kt:79-80` (same pattern in setup/account)
- **Impact:** slow, bounded leak per service rebind within one activity lifetime.
- **Fix:** attach/detach instead of rekeying, or clear superseded entries.

### A11 [info] Notification channels are re-created on every status tick
- **Location:** `vpn/NotificationFactory.kt:75`
- **Impact:** two binder calls per refresh; wasted work during idle.
- **Fix:** ensure once with an `AtomicBoolean`/`lazy`.

**Verified clean:** all `PendingIntent`s immutable; the account WebView has no JS bridge, a pure navigation classifier with main-frame check and one-shot redirect gate; cleartext off except loopback; backup/data-extraction excluded; FileProvider unexported with per-URI grants; the self-updater validates HTTPS GitHub path, exact `SHA256SUMS` entry, package/version/versionCode before hand-off.

---

## 7. gomobile facade and Android platform (`mobile/`, `internal/platform/android`)

### M1 [major] `Command` holds the read lock for a whole dispatch; `Close` waits unbounded, on the main thread
- **Location:** `mobile/mobile.go:29-75`, `mobile/commands.go:22-46`; consumer `KumihoVpnService.kt:212-226`
- **Evidence:** `Command` takes `mu.RLock` across the entire command (budgets up to 120 s for connect); `Close` takes `mu.Lock` and then `f.wg.Wait()` with no deadline; the app calls it from `onDestroy`.
- **Impact:** destroying the service during any command blocks the main thread for that command's budget plus ~10 s of engine teardown — well past the ANR threshold. (Same root as A1, from the facade side.)
- **Fix:** do not hold the facade lock across dispatch, make `Close` mark-and-detach then wait in the background, and never block the main thread in `onDestroy`.

### M2 [major] `AcquireFD` and `NetworkAvailable` exceptions cross the bridge unrecovered
- **Location:** `internal/platform/android/provider.go:86`, `:134`; recover exists only in `mobile/commands.go:514-523` for `Configure`
- **Evidence:** the Kotlin bridge throws (`require(mtu in range)`, `illegalState("configure() must be called before acquireFD()")`, `establish() == null`); gomobile surfaces that as a Go panic.
- **Impact:** a revoked consent or competing VPN during connect panics through `Connect`, leaving the controller in `connecting` with the session attached (only a manual disconnect recovers); a watchdog-goroutine call can crash the process.
- **Fix:** wrap both bridge calls in the same recover helper; add a deferred state reset in `Connect`.

### M3 [major] Android has no kill switch, but the doctor tells users it does
- **Location:** `internal/platform/android/netconfig.go:9-25`, `mobile/doctor.go:148-155`, `internal/engine/watchdog.go:302-309`
- **Evidence:** `netConfig.Apply/Cleanup` are no-ops documented as "the kill switch is the OS Always-on VPN / lockdown setting", while `lockdownCheck` prints "Kumiho's own kill switch still blocks leaks". On FATAL, `fatalStop` → `stopTunnel` closes the adopted tun fd while the service stays alive.
- **Impact:** with lockdown off (the default), a fatal teardown can leave the notification saying connected while traffic uses the physical network — a silent leak, contradicting the shipped guidance.
- **Fix:** keep the interface/fd open and drop packets on FATAL, or stop the service and surface "VPN is off"; correct the doctor text.

### M4 [minor] `decodePaths` validates only "absolute"
- **Location:** `mobile/paths.go:27-51`
- **Impact:** `..` and overlapping paths are accepted; a future/mis-built caller can point token storage anywhere (defense-in-depth only today).
- **Fix:** clean, reject `..`, reject duplicates/overlaps.

### M5 [minor] `revoke` tears down the data path but leaves the facade usable, and the name overpromises
- **Location:** `mobile/commands.go:462-468`
- **Impact:** another caller could reconnect after the OS revoked consent; the command does not revoke tokens.
- **Fix:** rename/document, and/or mark the facade closed so a fresh `Start` is required.

### M6 [minor] `rotatingWriter.Close` is not terminal
- **Location:** `mobile/logfile.go:64-72`, `:146-156`, `mobile/facade.go:131-140`
- **Impact:** a log line after shutdown recreates `kumiho.log` and keeps an fd for process life.
- **Fix:** a `closed` flag that makes `Write` fail.

### M7 [minor] Several commands create a timeout context and discard it
- **Location:** `mobile/commands.go:174-182`, `:184-199`, `:248-264`, `:284-366`, `:449-468`
- **Evidence:** `xctx, cancel := …; defer cancel(); _ = xctx` while the call takes no context.
- **Impact:** declared budgets are cosmetic; a blocked `accounts_*`/`disconnect`/`revoke` holds the read lock, reinforcing M1.
- **Fix:** pass the context through or delete the dead code.

### M8 [info] `Close` publishes `current = nil` before teardown completes
- **Location:** `mobile/mobile.go:61-73`
- **Impact:** a racing `Start` builds a second facade on the same settings/state files (narrow today because both lifecycle callbacks are on the main thread).
- **Fix:** keep a "closing" state that makes `Start` fail until teardown finishes.

### M9 [info] The Android provider's tests never run in CI
- **Location:** `internal/platform/android/provider_android_test.go:1`
- **Impact:** routing/DNS wiring is verified only on device runs.
- **Fix:** add a `GOOS=android` test step or an emulator-executed Go test.

---

## 8. Build, CI, installers, release

### B1 [major] The documented one-command install runs mutable-branch code as root and self-checks with a co-located checksum
- **Location:** `scripts/install-release.sh:5`, `:14`, `:74-105`; advertised in `README.md:91`, `README.fa.md:93`, `docs/install.md:27-33`
- **Evidence:** `curl -fsSL .../main/scripts/install-release.sh | sudo bash`; the script downloads the archive and `SHA256SUMS` from the same release origin.
- **Impact:** the checksum catches corruption, not a compromised branch/account/release — in all those cases the attacker controls both files and gets root.
- **Fix:** pin the documented script to a tag or ship a detached signature/attestation and verify the versioned checksum against it; add `--proto '=https'`.

### B2 [major] The Android AAR build needs the network and bypasses `vendor/`
- **Location:** `scripts/android-build-aar.sh:83-95`, `:107-112`; contradicted by `AGENTS.md:19-20` and `docs/install.md:43`
- **Evidence:** the script forces `-mod=readonly` because gomobile resolves the module graph (vendor mode cannot) and retries on network failures; `cmd/gomobile` is not vendored.
- **Impact:** the "no network needed to build or test" guarantee is false for the Android half; the AAR can resolve dependency versions different from the shipped desktop binaries, and nothing compares them.
- **Fix:** vendor the gomobile command as a `tool` directive (or pin a module-cache tarball), add a `go mod vendor && git diff --exit-code` guard, and correct the docs.

### B3 [major] The release workflow asserts only that files exist
- **Location:** `.github/workflows/release.yml:144-162`
- **Evidence:** `cp SHA256SUMS SHA256SUMS-<tag>` immediately followed by `cmp` (compares a file with its own copy — cannot fail); the loop only runs `test -s` on hard-coded names. No `version` smoke, no `sha256sum -c`, no archive extraction, no check that asset names contain the tag.
- **Impact:** a mis-tagged or wrong-commit binary, or an unversioned name, can reach a published release with a green workflow (the AGENTS filename rule and "binary embeds the tag" are conventions only).
- **Fix:** `./dist/kumiho-linux-amd64 version | grep -F "$GITHUB_REF_NAME"` (+ Windows/GUI), `sha256sum -c` in the assets dir, a name-contains-tag loop, and an unzip-and-run smoke; drop the tautological `cmp`.

### B4 [minor] `install.sh --purge` without `uninstall` reinstalls instead of purging
- **Location:** `install.sh:24-25`, `:34-52`; contrast `install.ps1:579-581`
- **Impact:** the obvious "remove everything" command performs a root install.
- **Fix:** reject `--purge` unless `mode=uninstall`; reject multiple positional modes.

### B5 [minor] Uninstall can leave the kill switch active after deleting the cleanup tool
- **Location:** `install.sh:161-162`, `install.ps1:514-522`
- **Impact:** if the service is SIGKILLed at `TimeoutStopSec` or `systemctl`/`sc` fails (swallowed by `|| true`), nftables/routing state survives and `kumiho cleanup` has just been deleted.
- **Fix:** run `<bin> cleanup` as root before deleting the binary, aborting on failure.

### B6 [minor] A failed Windows upgrade leaves the service stopped
- **Location:** `install.ps1:434-495`
- **Evidence:** `Stop-Service -Force` → copy/ACL/`sc.exe` under `$ErrorActionPreference='Stop'` → restart only at the end.
- **Impact:** any throw (locked file, disk full, `icacls`) exits with the VPN down.
- **Fix:** `try { … } finally { if ($wasRunning) { Start-Service } }`.

### B7 [minor] Windows uninstall only revokes the current `-User`
- **Location:** `install.ps1:22`, `:212-241`, `:551-572`
- **Impact:** over-the-shoulder UAC installs leave the original user's pipe grant in `%ProgramData%\kumiho\pipe-users` and their Run entry; grants accumulate across cycles.
- **Fix:** record/iterate every pipe SID on uninstall.

### B8 [minor] CI never enforces the formatter half of the documented gate
- **Location:** `.github/workflows/ci.yml:14-32`; `Makefile:94-96`
- **Impact:** unformatted Go can land on `main`; `make lint` is local-only.
- **Fix:** add a `make lint` (or `gofmt -l`) step.

### B9 [minor] Shell syntax CI skips sub-directory scripts; no shellcheck
- **Location:** `.github/workflows/ci.yml:19`
- **Impact:** `scripts/screenshots/capture.sh` and `scripts/flags/generate-android-flags.sh` are never syntax-checked; the `# shellcheck` directives are decorative.
- **Fix:** `bash -n scripts/*/*.sh` plus pinned `shellcheck`.

### B10 [minor] Third-party actions are mutable-tag pinned; release keeps a write token during third-party builds
- **Location:** `.github/workflows/ci.yml:16,22,101,108,136,148,213,225`; `release.yml:7-8,21-24,27,33`
- **Impact:** a hijacked tag or Gradle plugin runs with `contents: write` next to the Android signing secrets.
- **Fix:** pin `uses:` to SHAs (Dependabot), `persist-credentials: false`, scope `contents: write` to the `gh release` steps, protect signing secrets with an `environment:`.

### B11 [minor] `android-env.sh` JDK fallback uses `&&` where `||` is meant; clobbers `ANDROID_NDK_HOME`
- **Location:** `scripts/android-env.sh:10`, `:16`
- **Impact:** a partially installed `JAVA_HOME` is kept; an operator-pinned NDK is ignored.
- **Fix:** `if ! jdk_ok …`; `${ANDROID_NDK_HOME:-…}`.

### B12 [minor] `android-setup.sh` pins an x86_64-only JDK tarball
- **Location:** `scripts/android-setup.sh:34-35`
- **Impact:** `make android-setup` cannot bootstrap on aarch64 hosts.
- **Fix:** select the tarball/checksum by `uname -m`.

### B13 [minor] `capture.sh` runs `rm -rf` on an unvalidated environment path
- **Location:** `scripts/screenshots/capture.sh:91`, `:115`
- **Impact:** `KUMIHO_TUI_CAPTURE_DIR=$HOME` deletes the home directory.
- **Fix:** require an absolute non-empty path outside `/`, `$HOME` and the repo.

### B14 [minor] `watchdog-retest.sh` always exits 0
- **Location:** `scripts/watchdog-retest.sh:9`, `:76-94`
- **Impact:** a failed heal looks identical to a pass in the exit code.
- **Fix:** track a failure flag and exit non-zero, or document it as observational only.

### B15 [minor] `dev-daemon.sh` stop pattern matches every checkout
- **Location:** `scripts/dev-daemon.sh:25`, `:29-40`
- **Impact:** starting/stopping a dev daemon in one worktree kills other worktrees' daemons and their tunnel/DNS state.
- **Fix:** anchor on the resolved repo path or use a pidfile in `$KUMIHO_RUNTIME_DIR`.

### B16 [minor] `install-release.sh` treats the first API entry as newest and falls back on any download error
- **Location:** `scripts/install-release.sh:74-80`, `:93-98`
- **Impact:** GitHub orders by creation date, so a back-ported patch or re-created tag can be installed; transient failures produce a confusing second 404.
- **Fix:** sort tags by semver (or use `/releases/latest` with a prerelease fallback); fall back only on HTTP 404.

### B17 [minor] The release job cannot recover from a partial failure
- **Location:** `.github/workflows/release.yml:194-217`
- **Impact:** `gh release create` fails if a draft already exists; no `concurrency:` group.
- **Fix:** delete/reuse the draft, or `gh release upload --clobber`; add a concurrency group.

### B18 [minor] Third-party license texts are not shipped, and NOTICE omits several dependencies
- **Location:** `.github/workflows/release.yml:93`, `:105`; `NOTICE:25-26`, `:83-84`
- **Evidence:** both zips carry only `LICENSE` + `NOTICE`; `NOTICE` closes with "full license texts … available in the Go module cache", which is not a distribution. Apache-2.0 §4(a) and MIT/BSD notice requirements apply to the statically linked deps (gVisor, vishvananda/netlink, miekg/dns, BurntSushi/toml, Microsoft/go-winio, golang.org/x/sys). None of `vishvananda/netlink`, `miekg/dns`, `BurntSushi/toml`, `Microsoft/go-winio` or `golang.org/x/sys` is listed at all.
- **Impact:** the release archives are non-compliant for those dependencies.
- **Fix:** ship a `LICENSES/` directory (or append the texts) and add the missing attributions; add a CI check that every vendored `LICENSE*` has a NOTICE entry.

### B19 [info] `NOTICE` credits `charmbracelet/bubbles`, which is not a dependency
- **Location:** `NOTICE:56`
- **Impact:** inaccurate attribution file (harmless).
- **Fix:** drop it.

### B20 [info] The relay Worker compares the relay token with `!==`
- **Location:** `contrib/relay-worker/src/index.js:72`
- **Impact:** theoretical timing oracle on the only control-plane gate.
- **Fix:** compare digests in constant time, or document the accepted risk.

---

## 9. Documentation drift

### D1 [info] READMEs and install guide still advertise v1.1.0 after v1.1.1 shipped
- **Location:** `README.md:26-31`, `:72`, `:109`, `:126-128`; `README.fa.md:29-34`, `:110`, `:127-129`; `docs/install.md:89`
- **Evidence:** latest tag and `CHANGELOG.md:11` are `v1.1.1`; `docs/releasing.md:107-112` requires switching both README tables after publication. `.ai-state` already records this as pending.
- **Impact:** users following the README download the previous patch release.
- **Fix:** run the documented post-publication step (download tables, checksum link, What's-new block, `docs/install.md` filename).

### D2 [info] `PLAN.md` §5 documents 12 of the 46 protocol commands
- **Location:** `PLAN.md:299-320` vs `internal/ipc/ipc.go` (`Type*` constants)
- **Evidence:** the table omits accounts/*, login_start/status/complete/cancel, prepare, region_check, ping_done, favorite_location, clear_recents, select_location, relay_test, log_snapshot.
- **Impact:** the design doc understates the wire surface; `docs/cli-tui-abilities.md` is the accurate inventory.
- **Fix:** point §5 at the command inventory or complete the table.

---

## 10. What is solid (verified, not assumed)

- **Layering:** the OS-free engine consumes the frozen `platform.Provider` seam; the GUI, Android app and CLI are additive clients over protocol v1. No OS branches leaked into the engine.
- **Protocol discipline:** newline-delimited JSON envelopes, unknown fields ignored, per-command payload validation, peer credentials checked fail-closed (SO_PEERCRED / named-pipe token), `shutdown` admin-only.
- **Privilege/packaging:** both installers are idempotent and elevation-guarded; all `rm -rf` operands are constants or `mktemp -d` results; Android signing material is materialized for one step and removed by an `EXIT` trap plus an `always()` step; `sha256sum -c` passes for all four `third_party/*/SHA256SUMS`; the Android APK verifier checks ABI set, 16 KB alignment, arm64 `p_align`, cert fingerprint and universal size.
- **Platform teardown:** Linux TUN uses a poller-backed non-blocking fd so `Close` unblocks readers; Windows Wintun uses `SetEvent`-then-`Lock` and an idempotent close; WFP replaces its filter program transactionally and never clears protection before installing the replacement; adapter removal is guarded by name and driver service.
- **Network helpers:** loopback-only proxies with strict bind validation, origin-form rejection, header/credential stripping, verified TLS with ALPN `h2`, bounds-checked ClientHello parser, single-retry relay with a five-host allowlist.
- **Android security:** immutable `PendingIntent`s, no JS bridge in the sign-in WebView, main-frame navigation classifier with a one-shot redirect gate, cleartext limited to loopback, backup/extraction excluded, unexported FileProvider, self-update verifies HTTPS path + exact checksum + package/version.
- **GUI DOM safety:** no `innerHTML`/`eval`/`document.write`; all remote strings rendered via `textContent`/`setAttribute`; constant-time token compare; traversal-proof static serving.
- **i18n:** exact key parity in both GUI locales (549) and Android locales (767), no empty strings.
- **Gate:** lint, tests, `-race`, and linux/windows/android cross-builds all pass at `b706328`.

---

## 11. Suggested order of work

**Before the next release (P0):**
1. I1 IPC close panic — small, high blast radius.
2. P2 logging sanitizer race — small, on the secret path.
3. A2 cookie clear — small, security-adjacent.
4. A1/M1 main-thread shutdown — replace `runBlocking` with a detached close.
5. M3 Android kill-switch messaging/behavior.
6. G3 elevated helper resolution; G1 navigation guard.
7. E1/E2/E3 engine credential and attempt fencing.
8. N1/N2 upstream stream wake-up and RST.

**Next cycle (P1):**
- P1/E5/P3 network apply/cleanup serialization and verification (this is the one that can strand a user offline).
- I2/I4 IPC deadlines + non-blocking dispatch.
- I3 terminal escape sanitization.
- G2 dev-harness auth; G4 account refresh context.
- N3/N4 input validation and parser depth.
- B3 release assertions; B1 install integrity.

**Backlog (P2):** the remaining minors and infos, grouped by area.

---

## Appendix — review provenance

Eight read-only reviews were run in parallel over the domains listed at the top; each read every in-scope production file and reported file:line evidence. Every `major` finding above, plus a sample of minors, was re-read against the source in this session; three agent claims were **rejected after verification** and are not included: (a) that `vishvananda/netlink` ships without a LICENSE file (it exists in `vendor/`), (b) that `NOTICE` omits gVisor (it is listed), and (c) that the Windows installer lacks a `--purge` guard (it exists at `install.ps1:579`; only the Linux installer lacks one). Findings marked *suspected* could not be exercised on this machine (Windows IPv6-disabled host; pass-file write race timing).

Verification commands used: `make lint`, `make test`, `go test -race -count=1 ./...`, `make build`, `GOOS=windows go build ./...`, `GOOS=android GOARCH=arm64 go build ./...`, `gofmt -l`, `bash -n`, `sha256sum -c`, JSON/XML key-parity scripts, and targeted source reads.

## 11. Fix status (2026-11-17)

Fixed on top of `b706328`, one logical change per commit; the full gate
(`make lint`, `make test`, `go test -race`, `make build`, Windows and Android
cross-builds) is green at `219e5ab`. Regression tests were added where the bug
is reproducible on this machine.

| Finding | Commit | Fix |
|---|---|---|
| I1 | `df407cf` | Events are delivered and the channel is closed under one lock. |
| P2 | `53b06e7` | `Ring.Log` reads the sanitizer under the ring lock. |
| E1 | `0813634` | A cached pass is dropped whenever the active account changes. |
| E3 | `c00fce7` | A sign-out during the pass refresh returns `not_authenticated`. |
| N1, N2 | `f539270` | Stream death wakes a pending `OpenStream`; timeout/cancel sends RST_STREAM. |
| G4 | `d37b482` | The background account refresh has its own deadline. |
| P1, E5 | `e3de0e1`, `3c5ce37` | Netcfg apply/cleanup is serialized on both platforms, and a re-apply cannot land after teardown. |
| G3 | `7f4cb62` | `sc.exe` comes from `GetSystemDirectory`; no elevated helper is resolved from PATH. |
| B4, B5 | `414ea15` | `--purge` requires `uninstall`; uninstall runs `kumiho cleanup` before removing the binary. |
| B13, B14, B15 | `a93a9c6` | Capture cleanup paths are validated, the watchdog reports failures, and the dev-daemon pattern is anchored to this checkout. |
| B11, B12 | `6a39e5b` | JDK fallback logic and the aarch64 JDK tarball/checksum are correct. |
| B16 | `57cc20d` | The newest release is chosen by semver, and the asset fallback only fires on HTTP 404. |
| B8, B9 | `a45611a` | CI runs `make lint` and syntax-checks nested shell scripts. |
| D1, D2 | `3f99269` | READMEs install guide and PLAN point at v1.1.1 and the full command catalog. |
| E2 | `c3481a7` | Connect attempts carry a generation; a superseded attempt cannot publish. |
| I3 | `16c885c` | ESC/C0/C1 sequences are stripped in the log ring, TUI and CLI output. |
| E4 | `56e7601` | Prepared-pass writes are fenced by the session lock against sign-out. |
| B3 | `219e5ab` | The release job verifies `SHA256SUMS`, embedded versions and versioned asset names. |

Still open from this report:

- **B2 (partial):** the AAR bind still computes the module graph from
  `go.mod`/`go.sum` (module cache, network on a cold runner) and
  `cmd/gomobile` is not vendored. The tool is now built from the pinned
  revision instead of a `PATH` gomobile, and the docs no longer claim the
  Android build is hermetic; a `go mod vendor` drift guard would rewrite the
  patched `vendor/github.com/jchv/go-webview2` (G1).
- **Android UI minors (A3 and the rest) and the remaining minor/info items
  (B6, B7, B10, B17, B18 and others):** left for a follow-up.

### Deferred batch (2026-11-17)

The findings deferred above were fixed on top of `b805562`; the full Go gate is
green at `263138d` and after the final docs/script commit. Android app changes
are verified by the `android` and `android-tv` CI jobs.

| Finding | Commit | Fix |
|---|---|---|
| G1 | `62d4418` | Foreign navigation is cancelled and the action bridge only accepts messages from the tokenized asset URL; the vendor patch is documented. |
| G2 | `67d32b6` | The harness binds loopback by default; a non-loopback bind requires a per-run token, and `/action`/`/dev/*` reject cross-site requests. |
| B1 | `ae772da` | Install instructions pin the v1.1.1 tag and every download requires HTTPS. |
| N3 | `7706daa` | Peer SETTINGS outside the RFC ranges fail the session; a zero frame chunk cannot spin. |
| N4 | `35aaa4c` | Filter-expression nesting is capped at 64 and fails open. |
| I2, I5 | `4116b01` | Per-write deadline closes a stalled client, status broadcasts coalesce, and inbound frames are capped at 8 MiB. |
| I4 | `907a6b2` | Long commands run on the daemon context, so the connection stays readable and a disconnect can land. |
| P3 | `c5f2ebb` | `NetConfig.Cleanup` returns residue; Linux verifies the nft table, fwmark rules and tables 1000/1001, and `kumiho cleanup` reports it. |
| E6, E7 | `245131d` | Account mutations snapshot the store and roll back on a failed save; removing an account revokes its FxA refresh token. |
| A1, M1 | `1684f09` | `Close` detaches teardown and `Start` waits for it; Kotlin `onDestroy` no longer calls `runBlocking`. |
| A2 | `ee08ca4` | Cookie flush and the DOM-storage purge run in `removeAllCookies`' completion callback. |
| M2, M3 | `263138d` | Bridge panics become an error (AcquireFD) or fail open (link check); the doctor warns that Android has no independent kill switch and the docs explain the fatal path. |
| B2 | `dafe729` | The AAR build compiles `gomobile`/`gobind` from the `go.mod`-pinned revision and the hermeticity claims were corrected (partial). |

`96ecc12` also made the region-blocked connect test hermetic.
