# Kumiho Multiplatform Core — Design Spec

**Status:** Draft
**Date:** 2026-10-10

## Objective / why

Kumiho v0.1.0 is Linux-only by construction: the engine reaches directly into
`internal/tun`, `internal/netcfg`, netlink monitoring, and a Unix-socket IPC
server, and its public API is typed in `internal/ipc` wire DTOs. The goal of
this cycle is a pure refactor that makes the engine **platform-pluggable and
embeddable** — the portable core (auth, server list, H2 transport, SOCKS, DNS,
state machine, settings, logging) isolated from OS networking behind explicit
interfaces — so that later cycles can add **Windows 11** (Windows service +
Wintun), **Android** (VpnService, in-process engine), and **other Linux
distros** without re-cutting the core again. Nothing user-visible ships this
cycle; Linux behavior must be byte-identical afterwards.

## Success criteria / definition of done

1. All existing tests pass unchanged on Linux: `make test`,
   `go test -race ./...`, `make vet` clean.
2. Zero Linux behavior change: identical nftables script, `ip rule`/`ip route`
   commands, `resolvectl` calls, IPC wire protocol (v1), file paths, UA string,
   CLI/TUI output. Verified by the existing golden tests plus a manual
   connect/disconnect/doctor checklist.
3. `internal/engine` can run a proxy-only connect **in-process** against fakes,
   with no IPC server involved.
4. `internal/platform` has a Linux provider with behavior identical to today's;
   `platform/windows` and `platform/android` compile-only stubs return clear
   "not implemented yet" errors; `GOOS=windows` and `GOOS=android`
   cross-compiles succeed in CI and select their own provider (Android no
   longer silently inherits the Linux implementation).
5. New engine unit tests run against a fake platform provider.
6. Docs: README gains a platform roadmap; PLAN.md notes superseded decisions;
   this spec and the implementation plan are committed.

## Scope

### In scope

- Extract `internal/engine` (Controller, connect/edge/failures/watchdog/
  autoconnect/locations/tokens, state machine) and `internal/api` (DTOs, error
  codes).
- Reduce `internal/ipc` to envelope + transport; add a transport seam
  (listen/dial/peer-auth) with the Unix-socket implementation only.
- New `internal/platform` with `Provider` + `Device`/`NetConfig` interfaces;
  `platform/linux` wraps the existing `tun`/`netcfg`/netmon/chown code;
  `platform/windows` and `platform/android` stubs; fix the
  `linux`-tag-satisfies-`android` trap (`linux && !android`).
- `internal/daemon` shrinks to process host: settings open, engine
  construction, IPC server, signals, supervision, control dispatch.
- UA becomes a provider field; Linux value unchanged.
- OS-aware default paths / control endpoint; env overrides kept.
- CI: cross-compile checks for windows/amd64 + android/arm64 (plus the
  existing linux amd64/arm64 builds).
- Docs updates (README roadmap, PLAN.md superseded notes).

### Non-goals (explicitly out)

- No Windows or Android functionality: stubs only — no Wintun, no VpnService,
  no named pipes, no Windows service code.
- No Linux behavior changes (nftables/rules/DNS/IPC/paths/UA/output frozen).
- No new dependencies; the vendored tree is unchanged.
- No UI work: the TUI is unchanged; the Android GUI (per the `/design` skill)
  is a later cycle.
- No macOS/iOS — not target platforms.
- No gomobile binding or mobile facade package yet.
- No release/version bump.

## Decision log

| # | Decision | Rationale | Status | Source |
|---|----------|-----------|--------|--------|
| D1 | Target platforms: Windows 11, Android, other Linux distros/archs; no macOS/iOS | User's devices | decided | user |
| D2 | Decompose: shared-abstraction plan first; no platform ships this cycle | Platform work is N sub-projects; core first | decided | user |
| D3 | UI: TUI on Windows/Linux; Android gets a simple GUI per `/design` (later cycle) | Terminal available on desktop | decided | user |
| D4 | Full parity required before any platform ships; capability checklist + mechanism map defined in this spec | Parity is the ship bar | decided | user |
| D5 | Platform seam: explicit interfaces | Clean, testable | decided | user |
| D6 | Interfaces live in central `internal/platform`; existing tun/netcfg become implementation details of `platform/linux` | Single OS-knowledge point | decided | user |
| D7 | Extract `internal/engine` + `internal/api`; ipc becomes transport-only; daemon = process host | Embeddable engine for Android | decided | user |
| D8 | Android drives the engine in-process (no sockets on Android) | VpnService requires the tunnel in the app process | decided | user |
| D9 | IPC transport seam now; Unix socket only this cycle; Windows named pipe in the Windows cycle | Avoids rework, defers the winio dependency | decided | user |
| D10 | Windows cycle model: Windows service + client over named pipe | systemd-daemon analog; parity | decided | user |
| D11 | Linux frozen this cycle: mechanism-neutral interface, Ubuntu-shaped implementation, zero behavior change | No regressions while refactoring | decided | user |
| D12 | UA = platform provider field; Linux string unchanged; platform cycles set theirs | Mozilla-facing behavior stays identical | decided | user |
| D13 | `platform/windows` + `platform/android` compile-only stubs now; CI cross-compile checks | Fix the android tag trap once; real compile checks | decided | user |
| D14 | Unsupported OSes: daemon starts; proxy-only works; full tunnel returns a clear error; doctor reports | Proxy-only needs no netcfg | decided | user |
| D15 | DoD: zero regression + fake-provider engine tests + CI matrix | Prove the refactor without shipping | decided | user |
| D16 | Docs: README roadmap + PLAN.md superseded notes + spec/plan | Keep public docs honest | decided | user |
| D17 | Topic name: `multiplatform-core` | — | decided | user |
| D18 | Commits: task-sized commits in main | User override of the squash convention | decided | user |
| D19 | DTOs move to `internal/api`; frontends import `api`; no compatibility aliases left behind | Mechanical, avoids two names for one type | assumed | recommendation |
| D20 | Paths become OS-aware; the control endpoint is opaque to the transport; env overrides kept | Windows has no `/run` | assumed | recommendation |
| D21 | Provider composition: `platform.Current()` at composition roots (cmd/cli/daemon) + explicit injection into engine options | Explicit deps, one OS switch | assumed | recommendation |
| D22 | Stub UA returns the current Linux string until its platform cycle defines it | No undefined Mozilla-facing behavior | assumed | recommendation |

## Architecture

### Layering

```
cmd/kumiho ─ cli ── tui           frontends; ipc client; platform.Current() for paths
                 └─ ipc client
daemon (process host) ── ipc server (transport seam)
      │
      └─ engine ── platform.Provider (injected; platform.Current() in cmd/daemon)
             │
             └── api (DTOs)          pure data; no OS, no transport
platform/linux   ── tun, netcfg, netmon, chown   (existing code, wrapped)
platform/windows ── stub (compile-only)
platform/android ── stub (compile-only)
```

- `engine` never imports `ipc` or `daemon`.
- `daemon` is composition + supervision + control dispatch (bridges ipc
  envelopes to engine methods).
- The future Android facade is just another engine consumer.
- Future platform providers are constructed with platform handles where
  needed (e.g., a Windows service context, an Android VpnService handle).

### Platform interfaces (sketch — exact signatures frozen by the plan)

```go
package platform

type Provider interface {
    Name() string                                              // "linux", "windows", "android"
    UserAgent() string
    TunDefaults() TunDefaults                                  // {Name, Addr}; zero on Android
    OpenDevice(ctx context.Context, spec DeviceSpec) (Device, error)
    NetConfig() NetConfig                                      // Apply/Cleanup
    WatchLinks(ctx context.Context) (<-chan struct{}, error)   // coalesced wake-ups
    BypassDialer() *net.Dialer                                 // control-plane dialer that avoids the tunnel
    Paths() settings.Paths                                     // OS defaults (env overrides applied)
}

type Device interface {
    io.ReadWriteCloser
    Name() string
}

type DeviceSpec struct {
    Name string
    MTU  int
    FD   int // >= 0: adopt an existing fd (Android VpnService, future)
}

type TunDefaults struct{ Name, Addr string }

type NetConfig interface {
    Apply(ctx context.Context, spec NetConfigSpec) error
    Cleanup(ctx context.Context, spec NetConfigSpec)
}

type NetConfigSpec struct { // today's netcfg.Config, renamed into platform terms
    TunName, TunAddr string
    TunMTU           int
    KillSwitch       bool
    AllowLAN         bool
    ExcludeCIDRs     []string
    LANSubnets       []string // detected by the implementation
}
```

Notes:

- `WatchLinks` mirrors today's coalesced netMonitor channel
  (`internal/daemon/netmon_linux.go:15-56`); stubs return a never-firing
  channel. The watchdog already tolerates missing events (2 s tick).
- `BypassDialer` = `SO_MARK 0x2` on Linux (today's `MarkedDialer`,
  `internal/netcfg/mark_linux.go`); stubs return a plain dialer.
- `TunDefaults` replaces the daemon's direct use of `netcfg.DefaultTunName` /
  `DefaultTunAddr`; Android returns zero values (VpnService assigns them).
- Stub providers are **functional for non-networking bits** (Name, Paths,
  BypassDialer, WatchLinks) and return "not implemented yet" from
  `OpenDevice`/`NetConfig`, which makes D14's proxy-only fallback work.

### IPC transport seam

```go
package ipc

type Transport interface {
    Listen(endpoint string) (Listener, error)
    Dial(ctx context.Context, endpoint string) (net.Conn, error)
    PeerUID(conn net.Conn) (uint32, bool) // Linux: SO_PEERCRED; others: (0,false)
}
```

The Unix implementation keeps today's behavior exactly: `net.Listen("unix",
path)`, stale-socket probe, chmod 0660, `SO_PEERCRED`. The endpoint string is
opaque and produced by `Provider.Paths()`. The wire protocol is unchanged
(v1) — this is a code seam, not a protocol change.

### Package moves

- `ipc.*` DTOs (`ConnectPayload`, `LoginState`, `Status`, `SettingsView`,
  `AccountInfo`, `Quota`, byte counters/rates, `RemoteError` + codes,
  locations/ping/log payloads) → `internal/api`. Envelope, `Type*` constants,
  and `ProtocolVersion` stay in `ipc` (wire-level concerns). Error codes move
  to `api` (engine returns typed errors); `ipc.ErrorPayload` references them.
- Controller and connect/edge/failures/watchdog/autoconnect/locations/tokens
  → `internal/engine`.
- `daemon/run.go` keeps the control dispatch (envelope → engine method);
  daemon keeps process-host duties only.
- Linux-only process-host helpers (netlink monitor, socket chown, stale-netcfg
  cleanup entry) relocate behind `platform/linux`.

### Build-tag trap fix

All Linux-only implementation files currently tagged `//go:build linux` become
`//go:build linux && !android`; `platform/android` is tagged `android` and
`platform/windows` is tagged `windows`. CI cross-compiles verify the correct
provider is selected.

### Paths / control endpoint

`settings.Paths` gains OS-aware defaults supplied by the provider. Linux is
unchanged (`/etc/kumiho`, `/var/lib/kumiho`, `/run/kumiho`, socket
`control.sock`). Stub OSes get reasonable placeholder defaults; Windows values
are defined in the Windows cycle. Env overrides (`KUMIHO_CONFIG`,
`KUMIHO_STATE`, `KUMIHO_TOKENS`, `KUMIHO_RUNTIME_DIR`) are unchanged.

### Parity contract (capability checklist + mechanism map)

Every platform must tick all capabilities before it ships (D4). The
"interface hook" column shows which provider method makes each capability
implementable — this is how the interface is proven sufficient.

| Capability | Linux (frozen, v0.1.0) | Windows (expected; Windows cycle confirms APIs) | Android (expected; Android cycle confirms) | Interface hook |
|---|---|---|---|---|
| TUN device | `/dev/net/tun`, `foxy0` | Wintun adapter | Adopt VpnService fd | `OpenDevice` (+`DeviceSpec.FD`) |
| System routing | nft mark + ip rules, tables 1000/1001 | Route table + interface metric | `VpnService.Builder` routes | `NetConfig.Apply` |
| Kill switch | nft guard + blackhole table | WFP filters | Always-on VPN + "block without VPN" | `NetConfigSpec.KillSwitch` |
| DNS | local listener `10.8.0.2:53` + `resolvectl` | NRPT + interface DNS | Builder DNS = local listener | `TunDefaults.Addr` + `NetConfig` |
| Split tunnel (CIDR) | nft exclude set | Route-based excludes | Routes (+ per-app later) | `NetConfigSpec.ExcludeCIDRs` |
| IPv6 | blackhole | platform-specific | Builder routes | `NetConfig` semantics |
| Autostart / survive logout | systemd unit | Windows service | Always-on VPN / app lifecycle | outside engine (packaging) |
| Control plane | Unix socket 0660 + peercred | Named pipe + ACL | in-process (none) | `ipc.Transport` |
| Control-plane bypass | `SO_MARK 0x2` | interface-bound sockets (cycle decides) | n/a (in-process) | `BypassDialer` |
| Link monitor | netlink subscribe | route-change notifications | ConnectivityManager (app) | `WatchLinks` |
| UA | `sys:linux` | `sys:windows` (cycle confirms) | `sys:android` (cycle confirms) | `UserAgent` |
| UI | TUI | TUI | simple GUI (`/design`) | frontends (outside engine) |

### CI

- Existing ubuntu job unchanged (vet, tests, race, linux amd64/arm64 builds).
- New cross-compile checks: `GOOS=windows GOARCH=amd64 go build ./...` and
  `GOOS=android GOARCH=arm64 go build ./...` with stubs in place.

### Testing

- Existing tests are the regression net and stay passing (import updates are
  mechanical; daemon tests that exercise engine behavior move with it).
- New engine tests with a **fake provider**: fake device (in-memory/net.Pipe),
  recording NetConfig, silent WatchLinks. At minimum: proxy-only connect /
  disconnect end-to-end over fakes; full-tunnel connect calls
  `OpenDevice` → `NetConfig.Apply` → DNS → SOCKS and tears down in the
  documented order (`internal/daemon/connect.go:222-249`).
- Manual Linux checklist (behavior freeze): `doctor`; login; full-tunnel
  connect; Tailscale SSH survives connect/disconnect/reconnect; kill switch
  on/off; DNS resolution; clean disconnect leaves no residue.

## Risks + rollback

- **Subtle Linux behavior drift during extraction** → no logic changes allowed;
  existing + golden tests; manual checklist; task-sized commits (D18) make
  bisect/revert easy.
- **DTO move churn / import cycles** → `api` is dependency-free pure data;
  moves are compiler-driven and mechanical.
- **Other files implicitly relying on the `linux` tag** → CI cross-compiles
  catch missing `!android` guards.
- **Stub providers bit-rot before platform cycles** → compile-checked in CI;
  clearly named "not implemented".
- **Rollback**: pure `git revert` of the refactor series; v0.1.0 remains the
  release; no runtime state, protocol, or data migrations are involved.

## Dependencies / ordering

Each step keeps the tree green (tests pass at every commit):

1. `internal/api` extraction (DTOs + codes).
2. `internal/engine` extraction (Controller + workers, still using Linux code
   paths directly).
3. `internal/platform` interfaces + `platform/linux` provider; engine consumes
   the provider.
4. `ipc` transport seam + daemon shrink to process host.
5. `platform/windows` + `platform/android` stubs + build-tag trap fix.
6. CI cross-compile matrix.
7. Docs (README roadmap, PLAN.md superseded notes).

The implementation plan carries the exact tasks, signatures, and per-task
verification.

## Open questions

None.
