# Kumiho Multiplatform Core — Implementation Plan

**Spec:** [docs/superpowers/specs/2026-10-10-multiplatform-core-design.md](../specs/2026-10-10-multiplatform-core-design.md)
**Status:** Draft (gate 2)
**Date:** 2026-10-10
**Scope:** pure refactor — nothing user-visible ships; Linux behavior frozen.

## Global constraints (from the spec, verbatim)

- Zero Linux behavior change: identical nftables script, `ip rule`/`ip route`
  commands, `resolvectl` calls, IPC wire protocol (v1), file paths, UA string,
  CLI/TUI output.
- Go 1.27.1, module `github.com/benyamin-git/kumiho`, single module.
- No new dependencies; the vendored tree is unchanged (`vendor/` untouched).
- Platform interface lives in `internal/platform`; `platform/linux` wraps the
  existing `tun`/`netcfg`/netmon/chown code; `platform/windows` and
  `platform/android` are compile-only stubs this cycle.
- Full parity is the ship bar for later platform cycles; this cycle proves the
  interface with a capability checklist + mechanism map (spec).
- Commits: task-sized commits in main (D18).
- Every task ends green: `make lint`, `make test`, `go test -race -count=1
  ./...`, `make build` all pass.

## Frozen interface decisions (spec sketch, refined here)

The spec's sketches are refined into these exact signatures. Two additions vs
the spec sketch are called out: `DNSListenAddr` (lets the fake-provider
full-tunnel test bind an ephemeral port instead of the privileged
`10.8.0.2:53`) and `HasDefaultRoute`/`ChownControlEndpoint`/`ControlEndpoint`/
`Paths` (replace the daemon's direct netcfg/netlink/chown/FHS knowledge).

```go
package platform // internal/platform/platform.go

type Provider interface {
    Name() string            // "linux", "windows", "android", "unsupported"
    UserAgent() string       // Mozilla API UA (Linux: current string, byte-identical)
    TunDefaults() TunDefaults // {Name, Addr}; zero on Android
    DNSListenAddr() string   // Linux: "10.8.0.2:53"; fake/tests: "127.0.0.1:0"
    OpenDevice(ctx context.Context, spec DeviceSpec) (Device, error)
    NetConfig() NetConfig    // Apply/Cleanup
    WatchLinks(ctx context.Context) (<-chan struct{}, error) // coalesced wake-ups
    HasDefaultRoute() bool   // replaces daemon's netlink RouteGet probe
    BypassDialer() *net.Dialer // Linux: SO_MARK 0x2; others: plain
    Paths() settings.Paths   // OS defaults + KUMIHO_* env overrides
    ControlEndpoint() string // opaque endpoint for ipc.Transport
    ChownControlEndpoint(endpoint string) // Linux: current sudo-chown logic; others no-op
}

type Device interface { io.ReadWriteCloser; Name() string }

type DeviceSpec struct {
    Name string
    MTU  int
    FD   int // >= 0: adopt an existing fd (Android, future); Linux errors
}

type TunDefaults struct{ Name, Addr string }

type NetConfig interface {
    Apply(ctx context.Context, spec NetConfigSpec) error
    Cleanup(ctx context.Context, spec NetConfigSpec) // never fails
}

type NetConfigSpec struct { // today's netcfg.Config, platform-neutral
    TunName, TunAddr string
    TunMTU           int
    KillSwitch       bool
    AllowLAN         bool
    ExcludeCIDRs     []string
    LANSubnets       []string // detected inside the Linux implementation
}
```

Selector: `internal/platform/auto`, `func Current() platform.Provider` —
build-tagged per OS, used only at composition roots (`daemon.Run`, `cli`).
Engines/tests receive the provider explicitly (D21).

Transport seam (T4):

```go
package ipc // internal/ipc/transport.go

type Transport interface {
    Listen(endpoint string) (*Server, error)
    Dial(ctx context.Context, endpoint string) (*Client, error)
    PeerUID(nc net.Conn) (uint32, bool)
}
```

`UnixTransport` is the only implementation this cycle (behavior identical:
MkdirAll 0700, stale-socket probe, `net.Listen("unix")`, chmod 0660,
`SO_PEERCRED`; `Server.Close` also removes the socket file, which run.go did).

## Tasks

### T1 — Extract `internal/api` (DTOs + error codes)

**Create**
- `internal/api/api.go`: package api. Move verbatim from `internal/ipc/ipc.go`:
  `RemoteError` (+`Error()`), `Result`, `HelloPayload`, `HelloResult`,
  `LoginEmailPayload`, `LoginPasswordPayload`, `Login2FAPayload`, `LoginState`,
  `Status`, `Quota`, `ByteCounters`, `ByteRates`, `SettingsView`,
  `SettingsSetPayload`, `LocationsPayload`, `PingPayload`, `PingResult`,
  `ConnectPayload`, `LocationsResult`, `SelectLocationPayload`,
  `LogsSubscribePayload`, `LogsSnapshotPayload`, `LogSnapshot`, `AccountInfo`,
  and the `Code*` error constants. Imports `logging` and `serverlist` (both
  portable); no OS/transport imports. JSON tags unchanged.

**Modify**
- `internal/ipc/ipc.go`: keep only `ProtocolVersion`, `Type*` constants,
  `Envelope`, `NewEnvelope`, `DecodePayload`. Delete the `ErrorPayload` alias
  (D19: no compatibility aliases).
- `internal/ipc/client.go`: decode errors into `api.RemoteError`.
- `internal/ipc/ipc_test.go`: update references.
- All `ipc.*` DTO references become `api.*` (mechanical import/selector
  change, no logic): `internal/daemon/{daemon,connect,run,locations,
  autoconnect,edge,failures,watchdog}.go` and daemon tests;
  `internal/cli/{connect,locations,login,logs,status}.go`;
  `internal/tui/{account,locations,logs,settings,tui}.go`, `tui_test.go`.

**Verification**
- `make lint && make test && go test -race -count=1 ./...` — all pass.
- `grep -rn 'ipc\.\(RemoteError\|ConnectPayload\|LoginState\|Status\b' --include='*.go' internal cmd | grep -v '^internal/ipc/'` — no matches (only `ipc.Type*`/`Envelope` remain).
- `./dist/kumiho-linux-amd64 version` after `make build` — unchanged output.

**Commit:** `refactor: move control-plane DTOs into internal/api`

### T2 — Extract `internal/engine`

**Move (git mv, package rename `daemon` → `engine`, imports updated)**
- `internal/daemon/daemon.go` → `internal/engine/engine.go`
- `connect.go`, `edge.go`, `failures.go`, `watchdog.go`, `autoconnect.go`,
  `locations.go`, `tokens.go` → `internal/engine/`
- `netmon_linux.go`, `netmon_other.go` → `internal/engine/` (temporary home;
  T3 moves them under `platform/linux`)
- Tests move with their subjects: `autoconnect_test.go`, `connect_test.go`,
  `daemon_test.go`, `edge_test.go`, `failures_test.go`, `netmon_test.go`,
  `watchdog_test.go`, `shutdown_test.go`, `integration_test.go`
  (`integration_test.go` doc path updated to `./internal/engine`).

**Keep in `internal/daemon`**
- `run.go` (process host + control dispatch), `chown_linux.go`,
  `chown_other.go`; update: `ctrl := engine.New(store, ring, fxa.NewClient())`,
  `control.ctrl *engine.Controller`; package doc updated to "process host".

**Interfaces (unchanged behavior)**
- Type name stays `Controller` (avoids `engine.Engine` stutter); constructor
  stays `func New(store *settings.Store, ring *logging.Ring, client *fxa.Client) *Controller`.
- No logic edits — moves and renames only.

**Verification**
- `make lint && make test && go test -race -count=1 ./...` — all pass.
- `go test ./internal/engine/... ./internal/daemon/...` — pass.
- `make build && ./dist/kumiho-linux-amd64 version` — unchanged.

**Commit:** `refactor: extract the embeddable engine package`

### T3 — `internal/platform` + Linux provider + engine rewiring

**Create**
- `internal/platform/platform.go` — interfaces/types above, package doc.
- `internal/platform/linux/doc.go` (untagged, package doc; keeps `./...`
  buildable on non-Linux GOOS), and `provider.go`
  (`//go:build linux && !android`): `New() *Provider`; methods:
  - `OpenDevice` → moved device code; `NetConfig` → adapter over
    `netcfg.Applier` (maps `NetConfigSpec` → `netcfg.Config`; LAN detection
    stays in the applier); `WatchLinks`/`HasDefaultRoute` → moved netmon code
    (netlink subscribe, `RouteGet`); `BypassDialer` → `netcfg.MarkedDialer()`;
    `TunDefaults` → `{netcfg.DefaultTunName, netcfg.DefaultTunAddr}`;
    `DNSListenAddr` → `net.JoinHostPort(netcfg.DefaultTunAddr, "53")`;
    `UserAgent` → `fxa.UserAgent`; `Paths` → `settings.DefaultPaths()`;
    `ControlEndpoint` → `filepath.Join(paths.Runtime, "control.sock")`;
    `ChownControlEndpoint` → moved `chown_linux.go` logic.
  - `OpenDevice` returns an error when `spec.FD >= 0` ("adopting a device fd
    is not supported on Linux").
- `internal/platform/linux/device.go` — moved from
  `internal/tun/device_linux.go` (same fd/poller/Close semantics; implements
  `platform.Device`).
- `internal/platform/linux/netcfg.go`, `netmon.go` (moved
  `internal/engine/netmon_linux.go`), `chown.go` (moved
  `internal/daemon/chown_linux.go`).
- `internal/platform/unsupported/provider.go` — fallback provider for any OS
  without a real one (used by `auto` until T5): functional non-networking bits
  (`Name`, `UserAgent` = `fxa.UserAgent` per D22, `Paths` =
  `settings.ApplyEnvOverrides` of `os.UserConfigDir()/kumiho`,
  `os.UserCacheDir()/kumiho`, `os.TempDir()/kumiho`,
  `ControlEndpoint` = `Runtime + "/control.sock"`, `BypassDialer` plain,
  `WatchLinks` never-firing channel, `HasDefaultRoute` true, chown no-op);
  `OpenDevice` and `NetConfig.Apply` return
  `errors.New("platform: not implemented yet")`; `NetConfig.Cleanup` is a
  no-op (D14: proxy-only still works).
- `internal/platform/auto/current_linux.go` (`//go:build linux && !android`),
  `current_other.go` (`//go:build !linux || android`; T5 retags once real
  stubs exist) — `func Current() platform.Provider`.
- `internal/engine/fake_provider_test.go` — `fakeProvider`, `fakeDevice`
  (blocking in-memory ReadWriteCloser), recording `NetConfig`; exported for
  use by all engine tests.

**Modify**
- Delete `internal/tun/device_linux.go`, `internal/tun/device_other.go`
  (`internal/tun` keeps the portable netstack: `stack.go`, `stack_test.go`).
- `internal/settings/settings.go`: add `ApplyEnvOverrides(base Paths) Paths`;
  `DefaultPaths()` = `ApplyEnvOverrides(linux base)` (Linux values unchanged).
- `internal/fxa/client.go`, `internal/guardian/guardian.go`: add
  `UserAgent string` field; at the header sites use
  `ua := c.UserAgent; if ua == "" { ua = UserAgent }`.
- `internal/engine`:
  - `New(store, ring, client, plat platform.Provider) *Controller`; panics if
    `plat == nil`; stores `plat`, sets `client.UserAgent` and
    `guardian.UserAgent` from `plat.UserAgent()`, keeps `userAgent` field for
    the exit check (connect.go:744).
  - Replace `applier *netcfg.Applier` with `nc platform.NetConfig`; replace
    `netcfg.Config` with `platform.NetConfigSpec` everywhere; `tun.Open` →
    `plat.OpenDevice(ctx, platform.DeviceSpec{Name: plat.TunDefaults().Name,
    MTU: cfg.MTU})`; `res.dev` becomes `platform.Device`; `dnsSrv.Addr` =
    `plat.DNSListenAddr()`; `CleanupStaleNetcfg` → `plat.NetConfig().Cleanup`;
    `Snapshot` DNS string = `plat.DNSListenAddr() + " (DoH through the tunnel)"`;
    `devName()` fallback = `plat.TunDefaults().Name`; `routeProbe` default =
    `plat.HasDefaultRoute`; watchdog link wake-ups via `plat.WatchLinks`.
  - Rename `UseSocketMarking()` → `UseBypassDialer()` (uses
    `plat.BypassDialer()`; bootstrap resolvers unchanged).
  - Engine no longer imports `netcfg` or the device files; still imports `tun`
    for the portable stack (`tun.Start`).
- `internal/daemon/run.go`: `plat := auto.Current()`; `settings.Open(plat.Paths())`;
  `engine.New(store, ring, fxa.NewClient(), plat)`;
  `srv, err := ipc.Listen(plat.ControlEndpoint())` (T4 replaces with
  transport); `plat.ChownControlEndpoint(...)`; drop the `runtime.GOOS ==
  "linux"` guard around `CleanupStaleNetcfg` (stub Cleanup is a no-op);
  `ctrl.UseBypassDialer()`. `Run(ctx, paths settings.Paths)` becomes
  `Run(ctx context.Context)` (composition root; `cli/daemon.go` updated).
- `internal/cli/doctor.go`: add a `platform` check reporting
  `auto.Current().Name()` (Linux: `linux`; stubs: detail says full tunnel is
  not implemented). Existing Linux probes unchanged.
- Engine tests: `newTestController` passes `fakeProvider`; add
  `TestConnectFullTunnelOverFakeProvider` — asserts the call order
  `OpenDevice → NetConfig.Apply → DNS start → SOCKS start` and teardown order
  `DNS stop → NetConfig.Cleanup → device Close → SOCKS stop` (spec testing
  section); keep all proxy-only tests green.
- `netmon_test.go` moves to `internal/platform/linux/netmon_test.go` with the
  same `linux && !android` tag.

**Verification**
- `make lint && make test && go test -race -count=1 ./...` — all pass.
- `GOOS=windows GOARCH=amd64 go build ./cmd/kumiho` and
  `GOOS=android GOARCH=arm64 go build ./cmd/kumiho` — succeed via the
  unsupported provider (interim).
- `make build && ./dist/kumiho-linux-amd64 version` — unchanged; manual
  connect smoke on the test server optional.
- `go test ./internal/engine/... -run FullTunnel -v` — new test passes
  without root.

**Commit:** `refactor: introduce internal/platform and rewire the engine`

### T4 — IPC transport seam + endpoint wiring

**Create**
- `internal/ipc/transport.go` — `Transport` interface (above).
- `internal/ipc/unix.go` — `UnixTransport` implementing `Listen` (body moved
  from `server.go:Listen`), `Dial` (body moved from `client.go:Dial`),
  `PeerUID` (calls `peerUID`, `peercred_linux.go`/`peercred_other.go` stay).

**Modify**
- `internal/ipc/server.go`: remove package-level `Listen`; `Server` carries
  `peerUID func(net.Conn) (uint32, bool)`; `Conn.PeerUID()` uses it (false
  when nil); `Server.Close()` closes the listener and removes the socket file
  (best-effort; replaces `defer os.Remove` in run.go).
- `internal/ipc/client.go`: remove package-level `Dial`.
- `internal/ipc/ipc_test.go`: use `UnixTransport{}.Listen/Dial`.
- `internal/daemon/run.go`: `srv, err := (ipc.UnixTransport{}).Listen(plat.ControlEndpoint())`;
  drop `defer os.Remove`; keep `defer srv.Close()`.
- `internal/cli/client.go`: dial via `UnixTransport` to
  `auto.Current().ControlEndpoint()`; error message uses the endpoint.
- `internal/cli/{doctor,cleanup}.go`: `settings.DefaultPaths()`/`SocketPath()`
  → `auto.Current().Paths()` / `ControlEndpoint()`.
- `internal/cli/daemon.go`: `daemon.Run(ctx)`.

**Verification**
- `make lint && make test && go test -race -count=1 ./...` — all pass.
- Manual dev smoke (optional, dev-env script):
  `KUMIHO_RUNTIME_DIR=/tmp/kumiho-dev ./dist/kumiho-linux-amd64 daemon &`
  then `./dist/kumiho-linux-amd64 status --json` reaches the daemon; kill and
  confirm `/tmp/kumiho-dev/control.sock` is removed.

**Commit:** `refactor: add the ipc transport seam and wire endpoints through the provider`

### T5 — Windows/Android stubs + android build-tag trap fix

**Create**
- `internal/platform/windows/{doc.go,provider.go}` (`//go:build windows`) —
  `New()` wraps `unsupported.New("windows", windowsPaths())`;
  `windowsPaths()` = `settings.ApplyEnvOverrides` of
  `%ProgramData%\kumiho\config.toml`, `%ProgramData%\kumiho\state.json`,
  `%ProgramData%\kumiho\tokens.json`, runtime `os.TempDir()/kumiho` (placeholder;
  the Windows cycle refines).
- `internal/platform/android/{doc.go,provider.go}` (`//go:build android`) —
  same, name `android`, paths from `os.UserConfigDir()`/`os.TempDir()`
  placeholders (the Android cycle replaces them with app-private dirs).
- `internal/platform/auto/current_windows.go` (`//go:build windows`),
  `current_android.go` (`//go:build android`).

**Modify (tag trap: `linux` is true on android)**
- `internal/platform/auto/current_other.go` → `//go:build !linux && !windows`
  (android now has its own file).
- `internal/ipc/peercred_linux.go` → `//go:build linux && !android`;
  `peercred_other.go` → `//go:build !linux || android`.
- `internal/netcfg/exec_linux.go`, `mark_linux.go` → `//go:build linux && !android`;
  `exec_other.go`, `mark_other.go` → `//go:build !linux || android`.

**Verification**
- `GOOS=windows GOARCH=amd64 go build ./...` and
  `GOOS=android GOARCH=arm64 go build ./...` — succeed.
- Provider selection: `GOOS=linux go list -f '{{.GoFiles}}' ./internal/platform/auto`
  → contains `current_linux.go`; `GOOS=windows ...` → `current_windows.go`;
  `GOOS=android ...` → `current_android.go` (android no longer picks Linux).
- `make lint && make test && go test -race -count=1 ./...` — all pass.

**Commit:** `feat: add windows and android platform stubs; fix the android tag trap`

### T6 — CI cross-compile matrix

**Modify**
- `.github/workflows/ci.yml`: add a step after "Build (linux/amd64 + linux/arm64)":
  ```yaml
  - name: Cross-compile (windows/amd64 + android/arm64)
    run: |
      CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...
      CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build ./...
  ```

**Verification**
- Run both commands locally — succeed.
- Push (or PR) and confirm the CI job is green; `make test` locally unchanged.

**Commit:** `ci: cross-compile windows and android in the build job`

### T7 — Docs: roadmap + superseded decisions

**Modify**
- `README.md`: replace "Target platform: **Ubuntu 24.04 LTS amd64/arm64** …"
  with "Current target: **Ubuntu 24.04 LTS amd64/arm64** …" and add after it:

  ```markdown
  ## Platform roadmap

  The core is platform-pluggable behind `internal/platform` (design:
  [docs/superpowers/specs/2026-10-10-multiplatform-core-design.md](docs/superpowers/specs/2026-10-10-multiplatform-core-design.md)).
  Linux ships today; each new platform must reach full parity before release:

  - **Windows 11** — Windows service + Wintun, TUI client over a named pipe.
  - **Android** — VpnService app with the engine embedded in-process, simple GUI.
  - **Other Linux distros/archs** — pluggable network-configuration backends.
  ```
- `PLAN.md`: after the §1 decision table, add:

  ```markdown
  > **2026-10-10 update:** decisions #3 (daemon + Unix socket), #7 (Ubuntu-only)
  > and #16 (systemd/polkit privileges) describe the v0.1.0 Linux
  > implementation and are superseded by the multiplatform design:
  > [docs/superpowers/specs/2026-10-10-multiplatform-core-design.md](docs/superpowers/specs/2026-10-10-multiplatform-core-design.md).
  ```

**Verification**
- `git diff` review: docs only; links resolve to existing files.
- `make lint && make test` — unchanged.

**Commit:** `docs: record the multiplatform roadmap and superseded decisions`

## Dependencies / ordering

T1 → T2 → T3 → T4 → T5 → T6 → T7, strictly sequential; every commit keeps the
tree green (`make lint`, `make test`, `go test -race`, `make build`).

- T1 unblocks T2 (engine API typed in `api`).
- T2 unblocks T3 (engine is the consumer of the provider).
- T3 unblocks T4/T5 (provider supplies paths/endpoint; stubs slot into `auto`).
- T5 unblocks T6 (CI cross-builds need stubs + tags).
- T7 is independent but lands last so docs describe the final state.

## Risks + rollback

- **Subtle Linux drift in T3** (largest task): mitigations — moves are
  verbatim; netcfg golden tests unchanged; string outputs
  (`10.8.0.2:53`, UA, socket path) asserted identical; fake-provider tests
  cover ordering; task-sized commits allow `git bisect`.
- **Test churn in T1/T2**: mechanical renames only; compiler-driven.
- **`./...` build on non-Linux**: mitigated by untagged `doc.go` files in
  every platform subpackage; verified by T5/T6 commands.
- **Rollback**: `git revert` the task commits in reverse order (T7→T1). No
  runtime state, protocol, or data migration is involved; v0.1.0 is untouched.

## Open questions

None.

## Self-review (spec → plan mapping)

| Spec section | Covered by |
|---|---|
| Objective / success criteria 1–2 (tests, zero drift) | T1–T5 verification |
| SC 3–5 (in-process engine, stubs, fake tests) | T3 |
| SC 4 (CI selects correct provider) | T5 + T6 |
| SC 6 (docs) | T7 + spec/plan commits |
| Architecture: layering, interfaces, transport seam, package moves, tag fix, paths | T2, T3, T4, T5 |
| Parity contract hooks | T3 (interface methods per checklist row) |
| Testing section | T3 (fake provider + full-tunnel ordering test) |
| CI section | T6 |
| D19–D22 assumed decisions | T1 (no aliases), T3/T4 (paths/endpoint/Current), T3/T5 (stub UA) |
