# Deferred review fixes — implementation plan (2026-11-17)

Source: [docs/code-review-2026-11-16.md](../../code-review-2026-11-16.md) §11 and the
findings it records as deliberately deferred. This plan covers the deferred
majors plus the adjacent minors that fold into the same change. It does not
re-open completed findings.

## Decisions taken (2026-11-17)

1. One phased plan, not four separate plans.
2. Scope: the 15 deferred majors plus the adjacent minors listed per task.
3. Android verification uses the `android` / `android-tv` CI jobs; no local
   Gradle or emulator in a T3 thread (docs/releasing.md). Changes under
   `android/`, `mobile/` and `internal/platform/android/` therefore leave the
   app gate to CI.
4. G1 uses both layers: a minimal additive patch to the vendored
   `go-webview2` wrapper (navigation/new-window guards and message-source
   filtering) and an authentication gate on the action bridge.
5. Phase order is security/desktop first; Android ships last, CI-verified.
6. No approval gates: the user asked for the plan and its execution. Commits
   follow the repository rules; nothing is pushed or tagged.

## Non-goals

- No protocol-v1 envelope changes.
- No new dependencies for signature infrastructure; B1 pins to a release tag
  and forces HTTPS, the report's first-listed option.
- No rework of findings already fixed in §11.

## Phase 1 — Windows GUI and installer

### Task 1: G1 — authenticate the bridge and constrain navigation
Files: `internal/gui/host_windows.go`, `internal/gui/host.go` (if a testable
helper is needed), `vendor/github.com/jchv/go-webview2/webview.go`,
`vendor/github.com/jchv/go-webview2/common.go`,
`vendor/github.com/jchv/go-webview2/pkg/edge/chromium.go`,
`vendor/github.com/jchv/go-webview2/pkg/edge/corewebview2.go`, new
`vendor/.../pkg/edge/ICoreWebView2NavigationStartingEventArgs.go`,
`...NavigationStartingEventHandler.go`, `...NewWindowRequestedEventArgs.go`,
`...NewWindowRequestedEventHandler.go`.

- Extend `Chromium.MessageCallback` to `func(source, message string)` and read
  `GetSource` in `MessageReceived`; free the returned string.
- Add `SetMessageSourceFilter(func(string) bool)` to the `WebView` interface and
  `webview`; `msgcb` drops a message whose source fails the filter.
- Add `NavigationStartingCallback`/`NewWindowRequestedCallback` to `Chromium`
  and register the handlers with the core web view; expose
  `SetNavigationGuard(func(uri string) bool)` and `SetNewWindowHandler` on the
  `WebView` interface. The guard sets `Cancel=true` for a rejected URI; the new
  window handler sets `Handled=true`.
- `host_windows.go` after `Bind` and before `Navigate`:
  - source filter: source must be exactly the asset server URL (prefix match
    on `h.server.URL()`), nothing else;
  - navigation guard: allow only `about:blank` and `h.server.URL()` prefixes;
  - cancel every new-window request.
- Record the vendor patch in `third_party/go-webview2/README.md` (new) so a
  future `go mod vendor` does not silently drop it; add the path to the
  repository docs where vendor policy is described.

Verification: `GOOS=windows go build ./...`, `GOOS=windows go vet ./...`, a new
host-level unit test for the source/URI predicate (platform-neutral helper),
and the full Go gate.
Commit: `fix: authenticate the windows gui bridge and block foreign navigation`

### Task 2: G2 — close the dev harness control surface
Files: `cmd/kumiho-gui-web/main.go`, `internal/gui/web/bridge.go`,
`internal/gui/web/harness_test.go`, `AGENTS.md`, `docs/development.md`,
`scripts/dev-gui-web.sh` (comment/URL output).

- Bind `127.0.0.1` by default; `KUMIHO_GUI_WEB_BIND` opts into another address
  (documented as LAN exposure).
- Require a per-run token whenever the bind address is not loopback; print the
  complete tokenized URL. Keep the loopback default token-free so the local
  dashboard link and bookmarks keep working.
- On `/action` and every `/dev/*` route, reject when `Origin`/`Sec-Fetch-Site`
  says cross-site. Accept absent headers (curl, tests).
- Tests: cross-site POST rejected; same-origin accepted; tokenized non-loopback
  server 404s without the token.

Verification: `go test ./internal/gui/... ./cmd/kumiho-gui-web/...` and the full
gate.
Commit: `fix: require same-origin and loopback by default in the gui harness`

### Task 3: B1 — stop running mutable-branch code as root
Files: `scripts/install-release.sh`, `README.md`, `README.fa.md`,
`docs/install.md`, `docs/development.md`, `docs/releasing.md`.

- Add `--proto '=https' --proto-redir '=https'` to every curl invocation.
- Document all one-command installs with the release tag pinned in the raw URL
  (`.../v1.1.1/scripts/install-release.sh`) instead of `main`; keep
  `--version` for selecting the installed release.
- Note in `docs/releasing.md` that the README's pinned tag moves with a release.
- B16 (adjacent): `install-release.sh` must parse release entries independently
  and never treat the first array entry as newest.

Verification: `bash -n`, `scripts/check-shell.sh` (or the repository's shell
lint), a fake-server test if the script has one, full gate.
Commit: `fix: pin install instructions and require https-only downloads`

## Phase 2 — engine, network, IPC, netcfg

### Task 4: E6 (+E7) — persistence-before-publication
Files: `internal/engine/engine.go`, `internal/engine/engine_test.go`
(or `accounts_test.go`).

- Snapshot the account file (deep copy) before a mutation; on a failed
  `saveAccountsLocked`, restore the snapshot and the active/session fields, as
  `publishRotation` does.
- Apply to `UseAccount` and `completeLogin`.
- E7: account removal best-effort destroys the removed account's FxA refresh
  token (same helper logout uses) before dropping it locally.

Verification: a test with an injected failing state writer asserts the command
returns an error and the in-memory active account/row set is unchanged; full
gate.
Commit: `fix: publish account changes only after persistence`

### Task 5: N3 (+N5 guard) — validate peer SETTINGS
Files: `internal/upstream/session.go`, `internal/upstream/stream.go`,
`internal/upstream/session_test.go`.

- `applySettings` returns an error for `SETTINGS_MAX_FRAME_SIZE` outside
  [16384, 16777215] and `SETTINGS_INITIAL_WINDOW_SIZE` above 2^31-1; the read
  loop fails the session with `ErrProtocol` (no panic).
- `stream.Write` refuses a non-positive frame chunk instead of spinning.
- Test: a crafted SETTINGS frame fails the session; `chunk` never loops at 0.

Verification: `go test ./internal/upstream/...`, race test, full gate.
Commit: `fix: reject invalid http2 settings from the peer`

### Task 6: N4 — bound filter-expression recursion
Files: `internal/serverlist/filter.go`, `internal/serverlist/filter_test.go`.

- Carry a depth counter in `parser`; `parseUnary` increments it and fails
  beyond 64, which feeds the existing fail-open path.
- Test: 100k nested parens and `!` chains return an error quickly; a normal
  expression still evaluates.

Verification: `go test ./internal/serverlist/...`, full gate.
Commit: `fix: cap filter expression nesting depth`

### Task 7: I2 (+I5) — bounded writes and an asynchronous broadcast
Files: `internal/ipc/server.go`, `internal/ipc/server_test.go`,
`internal/daemon/run.go`, `internal/daemon/run_test.go`.

- `Conn.write` sets a per-write deadline (5 s) and closes the connection on a
  timeout so the read loop reaps it.
- `broadcastStatus` becomes a coalescing trigger (capacity-1 channel) consumed
  by one daemon goroutine; an engine state change never waits on a client.
- I5 (adjacent): cap a single inbound frame (`ipc.MaxFrameSize`, 8 MiB) and
  return a protocol error instead of allocating without bound.
- Tests: a stalled client does not block a second client; oversized frame is
  rejected; broadcast coalesces.

Verification: `go test ./internal/ipc/... ./internal/daemon/...`,
`go test -race`, full gate.
Commit: `fix: bound ipc writes and decouple status broadcast`

### Task 8: I4 — take long commands off the connection loop
Files: `internal/daemon/run.go`, `internal/daemon/run_test.go`.

- Dispatch the long-running request types (connect, disconnect, prepare,
  region_check, relay_test, account, login/2fa/accounts mutations) on their own
  goroutine, using the daemon context rather than the connection context so a
  client that gives up does not cancel the work.
- Keep fast/stateful reads inline. Replies stay mutex-serialized in `Conn`.
- Test: a slow connect followed by a locations request on the same connection
  is answered before the connect completes; disconnecting the client does not
  cancel the connect (mock engine).

Verification: `go test ./internal/daemon/...`, `-race`, full gate.
Commit: `fix: dispatch long control commands off the connection loop`

### Task 9: P3 — verify Linux cleanup
Files: `internal/platform/platform.go`, `internal/netcfg/netcfg.go`,
`internal/netcfg/netcfg_test.go`, `internal/platform/linux/*`,
`internal/platform/windows/*`, `internal/platform/android/*`,
`internal/platform/unsupported/*`, `internal/engine/connect.go`,
`internal/cli/cleanup.go` (if it reports the result).

- `NetConfig.Cleanup` returns an error; Linux verifies that the nft table,
  fwmark rules 1100/1200 and tables 1100/1200 are gone after the commands.
- Delete the blackhole rule before flushing its table.
- The engine logs/surfaces residue on disconnect; `kumiho cleanup` reports it.
- Other platforms return nil (Android stays a no-op).

Verification: netcfg tests with a fake runner (residue present → error, clean →
nil); full gate including Windows/Android cross builds.
Commit: `fix: verify linux cleanup leaves no residue`

## Phase 3 — Android (CI gate)

### Task 10: A1 + M1 — non-blocking shutdown
Files: `mobile/mobile.go`, `mobile/commands.go`, `mobile/mobile_test.go`,
`android/.../vpn/KumihoVpnService.kt`,
`android/.../engine/EngineClient.kt`.

- `Command` takes the facade pointer under the read lock and releases it before
  dispatch; a closed facade returns `errNotStarted` (atomic/closed flag).
- `Close` detaches the facade, starts teardown, and does not join the wait group
  on the calling (main) thread; teardown remains idempotent and a later `Start`
  waits for the previous teardown if one is in flight.
- Kotlin `onDestroy` no longer calls `runBlocking`; it launches `engine.close()`
  on an application-scoped IO dispatcher.
- M4 (adjacent): `decodePaths` rejects non-absolute and empty required paths.

Verification: `GOOS=android go build ./...`, mobile Go tests; the Android app
gate is the `android` CI job.
Commit: `fix: stop blocking the main thread during android shutdown`

### Task 11: A2 — wait for cookie removal
Files: `android/.../ui/account/MozillaAccountWebView.kt`.

- `removeAllCookies { flush() }`, and delete DOM storage in the same completion
  path; the sign-out caller does not need to block.
Commit: `fix: clear webview cookies before sign-out completes`

### Task 12: M2 + M3 — bridge failures and honest kill-switch guidance
Files: `internal/platform/android/provider.go`,
`internal/platform/android/provider_test.go`, `mobile/doctor.go`,
`mobile/doctor_test.go`, `internal/platform/android/netconfig.go`.

- Wrap `AcquireFD` and `NetworkAvailable` in the same recover helper used by
  `Configure`; a bridge panic becomes an error (AcquireFD) or `true`
  (NetworkAvailable, fail-open for the link check).
- Doctor: lockdown off is a warning that says Android has no independent kill
  switch and names the OS lockdown setting; the no-op netconfig comment stays
  truthful.
- Add the fatal-path guidance to `docs/android.md`: a fatal teardown needs
  Always-on/lockdown for guaranteed blocking until the service is stopped.

Verification: `GOOS=android go build ./...`, `go test ./mobile/...
./internal/platform/android/...`; app gate in CI.
Commit: `fix: recover android bridge failures and correct kill-switch guidance`

## Gate and handoff

After every commit: `make lint && make test && go test -race -count=1 ./... &&
make build && GOOS=windows GOARCH=amd64 go vet ./... && GOOS=windows GOARCH=amd64
go build ./... && GOOS=android GOARCH=arm64 go build ./...` with
`PATH=$PATH:/usr/local/go/bin`.

Android-touching commits are verified by the `android` and `android-tv` CI jobs,
per docs/releasing.md. Do not run Gradle or the emulator in this thread.

Final: update `.ai-state`, the review's §11 status table, and the README/docs
claims affected by B1/B2. No push.

## Execution status (2026-11-17)

All twelve tasks are implemented. Fourteen commits sit on top of the plan
commit `b805562`:

| Task | Commit | Notes |
|---|---|---|
| 1 G1 | `62d4418` | vendored patch plus the bridge source/token guard |
| 2 G2 | `67d32b6` | loopback default, token on a LAN bind, same-origin guard |
| 3 B1 | `ae772da` | v1.1.1 tag pin, HTTPS-only, release checklist |
| 4 E6/E7 | `245131d` | snapshot/rollback; removed account token revoked |
| 5 N3 | `7706daa` | SETTINGS range validation plus the zero-chunk guard |
| 6 N4 | `35aaa4c` | parser depth capped at 64 |
| 7 I2/I5 | `4116b01` | 5 s write deadline, coalesced broadcast, 8 MiB inbound cap |
| 8 I4 | `907a6b2` | long commands on the daemon context |
| 9 P3 | `c5f2ebb` | `Cleanup` returns residue; the CLI reports it |
| 10 A1/M1 | `1684f09` | detached Close; Kotlin `onDestroy` off the main thread |
| 11 A2 | `ee08ca4` | flush and DOM purge in the removal callback |
| 12 M2/M3 | `263138d` | bridge panics become an error/fail-open; honest lockdown warning and docs |
| B2 | `dafe729` | AAR builds pinned `gomobile`/`gobind` from `go.mod`; docs corrected |

`96ecc12` also made the region-blocked connect test hermetic.

Verification: the full Go gate is green at `263138d` and after the B2
docs/script change. Android Kotlin/Compose changes are verified by the
`android` and `android-tv` CI jobs; this machine has no SDK/NDK.

B2 remains partial by design: `gomobile bind` still computes the module graph
from `go.mod`/`go.sum` (module cache, network on a cold runner), because vendor
mode cannot answer `go list -m all`; `cmd/gomobile` is not vendored. The build
script now compiles both tools from the pinned revision instead of trusting a
`gomobile` on `PATH`, so the AAR cannot silently use a different version. A
`go mod vendor` drift guard is not viable in the main tree: it rewrites the
patched `vendor/github.com/jchv/go-webview2` (G1);
`third_party/go-webview2/README.md` records the patch and the manual re-vendor
step.
