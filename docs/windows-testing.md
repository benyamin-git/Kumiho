# Windows 11 manual end-to-end checklist (v0.2.0 + v0.3.0 GUI + v0.4.0 backend parity)

This is the committed manual E2E checklist from spec criterion 10. Run it top
to bottom on a **fresh Windows 11 amd64 machine** (or VM) before tagging
`v0.2.0`, and record the result of every step. A step passes only when the
observed output matches "Expect"; if anything diverges, file it and stop the
release. Sections 1–15 are the v0.2.0 release checklist; section 16 is the
v0.3.0 GUI acceptance script (its design spec is linked there).

Section 18 is the v0.4.0 backend-parity delta (its design spec is linked
there). It can run after sections 1–17 on the same box — defer the uninstalls
in sections 15 and 16.10 to 18.10 — or on a box with a working v0.3.0
install; 18.1 upgrades that install rather than requiring a clean one.

Everything runs in PowerShell. The machine has no prior Kumiho install; if you
are re-running the list, finish section 15 first so the box is clean again.
`<physical>` below is the machine's physical adapter alias (`Wi-Fi` or
`Ethernet`); take it from section 8 and substitute it everywhere.

Criterion coverage and the sign-off table are at the end. Background reading:
[docs/usage.md](usage.md), [docs/install.md](install.md),
[docs/troubleshooting.md](troubleshooting.md), the GUI guide
[docs/gui.md](gui.md), the design specs at
[docs/superpowers/specs/2026-10-10-v0.2.0-windows-release-design.md](superpowers/specs/2026-10-10-v0.2.0-windows-release-design.md)
and
[docs/superpowers/specs/2026-10-15-gui-rewrite-design.md](superpowers/specs/2026-10-15-gui-rewrite-design.md).

## 1. Preconditions

- [ ] Windows 11 amd64:

      winver
      (Get-CimInstance Win32_OperatingSystem).Caption
      [System.Environment]::Is64BitOperatingSystem

  Expect: "Windows 11", and `True` for 64-bit.

- [ ] A working physical internet connection and a Firefox account with the
      built-in VPN entitlement. Record the no-VPN baseline now (exit IP and
      per-interface DNS); keep this elevated shell open for the whole list and
      re-run the `$physical`/`$baseline` lines if you have to restart it:

      $baseline = curl.exe -4 -s https://www.cloudflare.com/cdn-cgi/trace
      $baselineIP = ($baseline | Select-String '^ip=').Line
      $baselineIP
      $dnsBefore = Get-DnsClientServerAddress -AddressFamily IPv4 |
        Where-Object { $_.InterfaceAlias -ne 'foxy0' } | Out-String

  Expect: an `ip=...` line; note the address (call it the *physical IP*).

- [ ] The commit under test is green: the standard local gate passed
      (`make lint && make test && go test -race -count=1 ./... && make build`
      plus the Windows/Android cross-builds) and the GitHub Actions
      `build-test` and `windows` jobs are green on this commit (criterion 1,
      criterion 7 hygiene).

- [ ] The docs from criterion 8 exist (run from a checkout, or tick off the
      file list): `README.md`, `docs/install.md`, `docs/manual.md`,
      `docs/usage.md`, `docs/troubleshooting.md`, `AGENTS.md`, `CHANGELOG.md`,
      `docs/windows-testing.md`, `PLAN.md`.

- [ ] The release workflow (criterion 9) published
      `kumiho-v0.2.0-linux-amd64.zip`, `kumiho-v0.2.0-linux-arm64.zip`,
      `kumiho-v0.2.0-windows-amd64.zip` and `SHA256SUMS` on the GitHub
      release. The Windows zip is the install source for section 2.

- [ ] Open an **Administrator** PowerShell and stay elevated for the whole
      list (the installer, cleanup and service commands need it).

## 2. Artifacts (criterion 1, 9)

- [ ] Download `kumiho-v0.2.0-windows-amd64.zip` and `SHA256SUMS` from the
      release. Verify the download:

      (Get-FileHash .\kumiho-v0.2.0-windows-amd64.zip -Algorithm SHA256).Hash
      Get-Content .\SHA256SUMS | Select-String windows-amd64

  Expect: the two hashes match (case-insensitive).

- [ ] Extract with Explorer (right-click → Extract All) so the Mark of the
      Web is preserved, then list the contents:

      Get-ChildItem .\kumiho-v0.2.0-windows-amd64

  Expect: `kumiho.exe`, `wintun.dll`, `install.ps1`, `LICENSE`, `NOTICE`,
  and `third_party` (with `third_party\wintun\LICENSE.txt`).

- [ ] Observe the SmartScreen warning on the unsigned binary (D14):

      Start-Process .\kumiho.exe -ArgumentList 'version'

  Expect: **Windows protected your PC** → click **More info → Run anyway**; a
  console prints `kumiho ...` and closes. If SmartScreen is disabled on the
  test box, note that and continue.

## 3. Install (criterion 2, D13, D26)

- [ ] Run the installer (allow the script for this session and unblock it if
      it came from the downloaded zip):

      Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
      Unblock-File .\install.ps1, .\kumiho.exe, .\wintun.dll
      .\install.ps1

  Expect, in order: local group `kumiho` created/reused; `%ProgramData%\
  kumiho` and `logs` created with SYSTEM + Administrators ACLs; `kumiho.exe`
  and `wintun.dll` copied to `C:\Program Files\Kumiho`; the directory added to
  the machine PATH; a default `config.toml` written; service `kumiho` created
  (auto-start, LocalSystem); crash recovery `restart/5000/restart/30000/
  restart/60000`; the invoking user added to the `kumiho` group; service
  started.

- [ ] Verify the service registration:

      Get-Service kumiho | Format-List Name,Status,StartType
      sc.exe qc kumiho
      sc.exe qfailure kumiho
      Get-CimInstance Win32_Service -Filter "Name='kumiho'" |
        Select-Object Name,StartMode,StartName

  Expect: `Running` / `Automatic`; BINARY_PATH_NAME
  `"C:\Program Files\Kumiho\kumiho.exe" daemon`; RESET_PERIOD 86400 and
  failure actions restart 5000, 30000, 60000; `StartMode: Auto`,
  `StartName: LocalSystem`.

- [ ] Verify files, PATH and ACLs:

      Test-Path 'C:\Program Files\Kumiho\kumiho.exe'   # True
      Test-Path 'C:\Program Files\Kumiho\wintun.dll'   # True
      [Environment]::GetEnvironmentVariable('Path','Machine') -split ';' -contains 'C:\Program Files\Kumiho'
      Get-LocalGroupMember kumiho
      (Get-Acl 'C:\ProgramData\kumiho').Access |
        Format-Table IdentityReference,FileSystemRights,AccessControlType

  Expect: both `True`; the PATH expression `True`; your user listed in the
  group; only `NT AUTHORITY\SYSTEM` and `BUILTIN\Administrators` with Full
  Control.

- [ ] Open a **new** shell (PATH refresh) and confirm the binary on PATH:

      kumiho version

  Expect: `kumiho v0.2.0 ...`.

## 4. Doctor, pre-connect

- [ ] Run the diagnostics:

      kumiho doctor

  Expect (order matters; Linux-only checks are not listed on Windows):
  `OK platform: windows (full tunnel supported)`, `OK socks-port`,
  `OK clock-skew`, `OK endpoints` (fails if any of FxA, Guardian or Remote
  Settings is unreachable — retry if your DNS is flaky), then
  `OK windows-service: kumiho service is running; start type auto`,
  `OK wintun-dll: wintun.dll 0.14.1 beside the executable`,
  `OK wfp-service: Base Filtering Engine (BFE) is running`,
  `OK control-pipe: \\.\pipe\kumiho\control is reachable`,
  `SKIP tun-adapter` (adapter appears on connect), `SKIP adapter-dns`,
  `OK physical-default-route`, `OK elevation` when elevated. The summary must
  report `0 fail`; `kumiho doctor` exits 1 on any failure.

      kumiho doctor --json | ConvertFrom-Json | Select-Object failed,warned,skipped

  Expect: `failed` 0.

## 5. Login

- [ ] Sign in (2FA aware) and check the session:

      kumiho login
      kumiho status

  Expect: `Signed in as <email>.` after the email/password/(2FA) flow, and a
  status block with `account:` and `token expires:`; `state:` is not
  `UNAUTHENTICATED`.

## 6. Locations and ping

- [ ] List locations and measure latency:

      kumiho locations
      kumiho locations --ping

  Expect: a country table (`CODE NAME n cities`), then one line per server
  with `ms` or `unreachable` — proving TCP reachability to the Fastly edge.
  Note one country code to use later, e.g. `--to de`.

## 7. Proxy-only mode: SOCKS + exit IP (criterion 3)

- [ ] Snapshot routing, then connect proxy-only:

      $routesBefore = Get-NetRoute -DestinationPrefix 0.0.0.0/0 | Out-String
      kumiho connect --proxy-only

  Expect: `connecting (this can take up to ~30s)...`, then a status block
  with `state: PROXY_ONLY`. The exit check runs asynchronously; if
  `exit ip:` is still empty, re-run `kumiho status` after a few seconds
  (allow up to ~20 s).

- [ ] Verify the system is untouched and the proxy works:

      Get-NetAdapter -Name foxy0 -ErrorAction SilentlyContinue   # nothing
      (Get-NetRoute -DestinationPrefix 0.0.0.0/0 | Out-String) -eq $routesBefore   # True
      curl.exe --proxy socks5h://127.0.0.1:1080 -s https://www.cloudflare.com/cdn-cgi/trace

  Expect: no `foxy0` adapter; the route table is byte-identical; the curl
  output has an `ip=` **different from the physical IP** and a `loc=` for the
  edge's country. `kumiho status` shows the same `exit ip:`.

- [ ] Tear down:

      kumiho disconnect

  Expect: `Disconnected.`

## 8. Full tunnel (criterion 3)

- [ ] Connect (first-run defaults: kill switch on, autoconnect on):

      kumiho connect

  Expect: state `CONNECTED`, a `location:`, `kill switch: on`,
  `autoconnect: on`, and an `exit ip: x.x.x.x (cc)` from the engine's exit
  check through the tunnel; it runs asynchronously, so re-run
  `kumiho status` after a few seconds if it is still empty (allow ~20 s).

- [ ] Verify the Wintun adapter and addressing:

      Get-NetAdapter -Name foxy0 | Format-List Name,Status,InterfaceIndex
      Get-NetIPAddress -InterfaceAlias foxy0 -AddressFamily IPv4 |
        Format-List IPAddress,PrefixLength
      Get-NetIPInterface -InterfaceAlias foxy0 -AddressFamily IPv4 |
        Select-Object NlMtu,InterfaceMetric

  Expect: adapter `foxy0`, `Status: Up`; address `10.8.0.2` / `32`; MTU
  `8500`, interface metric `0`.

- [ ] Verify the route takeover and the bypass half of the kill-switch rules:

      Get-NetRoute -InterfaceAlias foxy0 -AddressFamily IPv4 -DestinationPrefix 0.0.0.0/0
      Get-NetRoute -InterfaceAlias foxy0 -AddressFamily IPv6 -DestinationPrefix ::/0
      Get-NetRoute -AddressFamily IPv4 | Where-Object RouteMetric -eq 9999 |
        Format-Table InterfaceAlias,DestinationPrefix,NextHop,RouteMetric

  Expect: `0.0.0.0/0` and `::/0` through `foxy0` with route metric `0`;
  metric-9999 bypass routes on the physical adapter(s), including
  `100.64.0.0/10` (Tailscale) and the local subnet (allow-LAN).

- [ ] Record the physical adapter for later steps and confirm egress goes
      through the edge:

      $physical = (Get-NetRoute -AddressFamily IPv4 |
        Where-Object { $_.DestinationPrefix -eq '0.0.0.0/0' -and $_.NextHop -ne '0.0.0.0' } |
        Select-Object -First 1).InterfaceAlias
      $physical
      curl.exe -4 -s https://www.cloudflare.com/cdn-cgi/trace

  Expect: `$physical` is the Wi-Fi/Ethernet alias; the `ip=` line now equals
  the `exit ip:` from `kumiho status`, differs from the physical IP, and the
  `loc=` matches the selected location (a mismatch only warns in the logs).

- [ ] Verify the IPv6 blackhole:

      Test-NetConnection -ComputerName 2606:4700:4700::1111 -Port 443 -InformationLevel Quiet
      Resolve-DnsName example.com -Type AAAA | Select-Object Name,Type,IPAddress

  Expect: `False` for the TCP test (IPv6 has no path around the tunnel and
  times out; allow ~20 s), while the AAAA lookup still resolves — IPv6 is
  blackholed but DNS works.

## 9. DNS parity (criterion 4)

- [ ] The adapter points at the in-tunnel resolver:

      Get-DnsClientServerAddress -InterfaceAlias foxy0 -AddressFamily IPv4
      Resolve-DnsName example.com
      Resolve-DnsName -Name example.com -Server 10.8.0.2
      Resolve-DnsName -Name example.com -Server 10.8.0.2 -TcpOnly

  Expect: server `10.8.0.2`; all three queries return A records (the last one
  exercises the resolver's TCP listener; UDP truncation is retried over TCP
  transparently).

- [ ] No system-wide DNS settings were touched:

      (Get-DnsClientServerAddress -AddressFamily IPv4 |
        Where-Object { $_.InterfaceAlias -ne 'foxy0' } | Out-String) -eq $dnsBefore
      Get-DnsClientGlobalSetting | Select-Object SuffixSearchList,UseDevolution

  Expect: `True` — the non-tunnel adapters show exactly the pre-connect
  servers (DHCP/router); the tunnel did not alter global settings.

- [ ] DoH through the tunnel is in the logs:

      kumiho logs --level debug | Select-String -Pattern 'resolver|dns|doh'

  Expect: `resolver up on 10.8.0.2:53 (DoH through the tunnel)` and
  `resolver listening on ... (UDP+TCP)`; no `dns` errors. A normal first
  resolve adds no `[dns]` activity — the resolver otherwise logs only failures
  and successful half-open probes.

- [ ] Custom DNS and DoH off (TUI Settings, applies on the next connect):
      open `kumiho`, `s`, set `DoH provider` to `cloudflare`, set
      `DNS upstream` to `1.1.1.1` (enter; empty resets), `esc`, `space` to
      disconnect, `space` to reconnect, then:

      Clear-DnsClientCache
      Resolve-DnsName example.com

  Expect: resolves through the pinned DoH resolver; no `dns` errors. Then set
  `DoH provider` to `off` and clear `DNS upstream`: `enter` on that row, erase `1.1.1.1`
  (empty resets to the provider default), `enter` again (`d` resets the row
  directly). Both apply on the next connect: `esc`, `space` to disconnect,
  `space` to reconnect, then:

      Clear-DnsClientCache
      Resolve-DnsName example.com

  Expect: does not resolve (SERVFAIL) — with `off` and no custom upstream
  there are no DoH endpoints, and the in-tunnel resolver never falls back to
  plaintext DNS; a pinned upstream would still be used as DoH even with `off`.
  Finally set `DoH provider` back to `automatic`, `esc`, `space` to
  disconnect, `space` to reconnect so the live resolver is rebuilt, then:

      Clear-DnsClientCache
      Resolve-DnsName example.com

  Expect: resolves again; leave `DNS upstream` empty. Section 10 assumes DNS
  works.

## 10. Kill switch on/off (criterion 5)

- [ ] Connected with the kill switch on (section 8), a direct bind outside
      the tunnel is blocked (if curl rejects the adapter alias, use its IPv4
      address from `Get-NetIPAddress -InterfaceAlias $physical -AddressFamily
      IPv4` as the `--interface` value):

      curl.exe --interface $physical -s --max-time 8 https://www.cloudflare.com/cdn-cgi/trace

  Expect: no trace output — curl fails (exit code 7/28). The WFP catch-all
  blocks non-permitted traffic, not just routing.

- [ ] LAN exception still works (allow-LAN default on):

      $gw = (Get-NetRoute -DestinationPrefix 0.0.0.0/0 |
        Where-Object InterfaceAlias -eq $physical).NextHop
      ping $gw

  Expect: replies from the gateway — LAN bypass routes/WFP permits are in
  effect.

- [ ] Exclude-CIDR exception: in the TUI Settings press `s`, select
      `exclude CIDRs`, enter `1.1.1.1/32`; reconnect (disconnect then
      connect); then:

      Get-NetRoute -AddressFamily IPv4 | Where-Object DestinationPrefix -eq '1.1.1.1/32'
      curl.exe --interface $physical -s --max-time 8 https://1.1.1.1/cdn-cgi/trace

  Expect: a metric-9999 route for `1.1.1.1/32` on `$physical`, and a trace
  with the physical IP — the exclusion is honored at both layers. Reset the
  row with `d` afterwards.

- [ ] Kill switch off: in the TUI Settings, set `kill switch` to `off`
      (applies immediately), then repeat the direct-bind curl:

      curl.exe --interface $physical -s --max-time 8 https://www.cloudflare.com/cdn-cgi/trace

  Expect: the trace now succeeds and shows the physical IP. Set it back to
  `on` and reconnect before continuing.

## 11. Service lifecycle and crash recovery (criterion 2)

- [ ] Clean stop and start:

      Stop-Service kumiho
      Start-Sleep -Seconds 3
      Get-Service kumiho
      Get-NetAdapter -Name foxy0 -ErrorAction SilentlyContinue   # nothing
      Get-NetRoute -AddressFamily IPv4 | Where-Object RouteMetric -eq 9999   # nothing
      Start-Service kumiho

  Expect: `Stopped`; the adapter and all metric-9999 routes are gone (clean
  teardown). After `Start-Service`, autoconnect (default on, tokens present)
  brings the tunnel back: within ~15 s `kumiho status` is `CONNECTED` again.

- [ ] Crash recovery (kill the service process, not the TUI):

      $svcPid = (Get-CimInstance Win32_Service -Filter "Name='kumiho'").ProcessId
      taskkill /F /PID $svcPid
      Get-Service kumiho
      Start-Sleep -Seconds 15
      Get-Service kumiho
      kumiho status

  Expect: `Stopped`/`Start Pending` immediately, then `Running` within ~5 s
  (SCM recovery); status returns to `CONNECTED`. The watchdog/start-time
  cleanup removes the crashed run's stale routes and re-applies the filters:
  run `kumiho doctor` and confirm `0 fail`, and confirm exactly one
  `0.0.0.0/0` route on `foxy0`:

      (Get-NetRoute -InterfaceAlias foxy0 -DestinationPrefix 0.0.0.0/0).Count   # 1

- [ ] Deterministic `kumiho cleanup` path (disable recovery temporarily so
      the crash is not healed before `kumiho cleanup` runs). Windows
      PowerShell 5.1 drops empty-string arguments to native commands, so
      clear the actions through `cmd.exe` (the documented `actions= ""` form)
      and verify the clear before killing:

      cmd.exe /c 'sc failure kumiho reset= 0 actions= ""'
      sc.exe qfailure kumiho
      $svcPid = (Get-CimInstance Win32_Service -Filter "Name='kumiho'").ProcessId
      taskkill /F /PID $svcPid
      Get-NetRoute -InterfaceAlias foxy0 -AddressFamily IPv4 -DestinationPrefix 0.0.0.0/0
      kumiho cleanup
      Get-NetAdapter -Name foxy0 -ErrorAction SilentlyContinue
      sc.exe failure kumiho reset= 86400 actions= restart/5000/restart/30000/restart/60000
      sc.exe qfailure kumiho
      Start-Service kumiho

  Expect: after the `cmd.exe` clear, `sc.exe qfailure kumiho` reports
  `RESET_PERIOD (in seconds) : 0` and `FAILURE_ACTIONS` with no entries
  (no `RESTART -- Delay` lines). If any `RESTART` lines are still listed, the
  clear did not take effect — repeat the `cmd.exe` line and re-check before
  killing the process. Record whether the killed run's `0.0.0.0/0` route and
  `foxy0` adapter survive the kill: Wintun ends the adapter session when the
  process dies, so they can disappear on their own, and either outcome is
  valid. If either is still present, `kumiho cleanup` must remove it. Cleanup
  prints `Stale kumiho routing, firewall and DNS state were removed (best
  effort).` either way. After the restore command, `sc.exe qfailure kumiho`
  lists the three `RESTART -- Delay` entries again (5000, 30000, 60000 ms) and
  `Start-Service kumiho` brings the service back.

## 12. Reboot autoconnect (criterion 2, 3)

- [ ] Reboot and check recovery without touching anything:

      Restart-Computer -Force

  After signing back in (the service runs as LocalSystem, so it also starts
  with nobody logged in):

      Get-Service kumiho
      kumiho status
      curl.exe -4 -s https://www.cloudflare.com/cdn-cgi/trace

  Expect: `Running`; `CONNECTED` within ~15 s of boot (autoconnect, remember-
  last-state); the trace shows the tunnel exit IP again.

## 13. TUI + CLI over the named pipe (criterion 6)

- [ ] CLI commands over the pipe:

      kumiho status --json
      kumiho locations --json
      kumiho logs -f      # Ctrl+C to stop; live stream over IPC

  Expect: valid JSON for the first two; the log stream prints live entries
  and Ctrl+C exits cleanly.

- [ ] TUI parity: run `kumiho` and exercise every screen:
      `?` help; `space` disconnect (`state: IDLE`) and connect again;
      `o` locations, `p` ping, `/` filter, `enter` select a country;
      `s` settings (cycle a value, `d` reset it); `a` account (email, quota);
      `g` logs (`v` level, `/` search, `f` follow, `c` clear);
      `e` export; `q` quit.

  Expect: all screens render and act on the daemon; `e` prints
  `exported to ...` and the file exists:

      Get-ChildItem "$env:LOCALAPPDATA\kumiho\logs"

  Expect: a `kumiho-logs-*.log` file.

- [ ] Pipe ACL: from a **standard (non-admin) account** that is in the local
      `kumiho` group (sign out and in after install), `kumiho status` works.
      From a standard account *not* in the group, `kumiho status` prints
      `cannot reach the daemon at \\.\pipe\kumiho\control` (access denied).
      Confirm membership with `Get-LocalGroupMember kumiho`.

- [ ] `shutdown` is administrators-only. From a non-admin member of the
      group, send a raw protocol-v1 request and read the reply:

      $pipe = [System.IO.Pipes.NamedPipeClientStream]::new('.', 'kumiho\control', [System.IO.Pipes.PipeDirection]::InOut)
      $pipe.Connect(3000)
      $writer = [System.IO.StreamWriter]::new($pipe); $writer.AutoFlush = $true
      $writer.WriteLine('{"v":1,"id":"1","type":"shutdown"}')
      $reader = [System.IO.StreamReader]::new($pipe); $reader.ReadLine()
      $pipe.Dispose()

  Expect: an `error` envelope with `"code":"forbidden"` and `"message":"only
  administrators may shut down the daemon"`; the daemon keeps running.

- [ ] From an elevated shell, repeat the snippet. Expect the `result`
      envelope `{"ok":true,"note":"shutting down"}`, after which the daemon
      exits and `Get-Service kumiho` shows `Stopped` (a clean IPC stop is not
      a crash, so the recovery actions do not relaunch it). Bring it back:

      Start-Service kumiho

## 14. Logs: file and viewer (D18, docs)

- [ ] The service writes its file log:

      Get-Item "$env:ProgramData\kumiho\logs\kumiho.log" | Select-Object Length,LastWriteTime
      Get-Content "$env:ProgramData\kumiho\logs\kumiho.log" -Tail 30

  Expect: the file exists, `LastWriteTime` moves while the daemon runs, and
  the tail has startup/autoconnect/tunnel lines. (Rotation at >5 MiB on
  service start is covered by `internal/daemon` tests; no need to fill 5 MiB
  by hand.)

- [ ] `kumiho logs` (ring snapshot) and the TUI Logs screen (`g`) show the
      same entries; `kumiho logs --level warn` filters, and the TUI export
      (`e`) from section 13 wrote the visible entries to
      `%LOCALAPPDATA%\kumiho\logs`.

## 15. Uninstall and residue (criterion 2, D13)

- [ ] Verify `-Purge` requires `-Uninstall`:

      .\install.ps1 -Purge

  Expect: an error, `-Purge requires -Uninstall`.

- [ ] Uninstall, keeping state:

      .\install.ps1 -Uninstall

  Expect: service stopped and deleted; `C:\Program Files\Kumiho` removed;
  the machine PATH entry removed; `%ProgramData%\kumiho` (tokens/state)
  kept. Verify:

      Get-Service kumiho -ErrorAction SilentlyContinue       # nothing
      sc.exe query kumiho                                     # "FAILED 1060"
      Test-Path 'C:\Program Files\Kumiho'                     # False
      [Environment]::GetEnvironmentVariable('Path','Machine') -split ';' -contains 'C:\Program Files\Kumiho'   # False
      Test-Path "$env:ProgramData\kumiho\tokens.json"         # True (kept)
      Get-LocalGroup kumiho                                   # still exists
      Get-NetAdapter -Name foxy0 -ErrorAction SilentlyContinue   # nothing
      Get-NetRoute -AddressFamily IPv4 | Where-Object RouteMetric -eq 9999   # nothing

  Expect: all as annotated — no service, binary, PATH entry, adapter or
  metric-9999 routes left; tokens and the group survive.

- [ ] Purge, removing everything:

      .\install.ps1 -Uninstall -Purge

  Expect: `%ProgramData%\kumiho` and the local group `kumiho` removed:

      Test-Path "$env:ProgramData\kumiho"          # False
      Get-LocalGroup kumiho -ErrorAction SilentlyContinue   # nothing
      Get-LocalGroupMember kumiho                  # group does not exist

  The per-user TUI prefs at `%APPDATA%\kumiho\tui.toml` are intentionally
  left alone (documented in docs/install.md). In a new shell, `kumiho` is no
  longer found:

      Get-Command kumiho -ErrorAction SilentlyContinue   # nothing

## 16. Windows GUI (v0.3.0)

This is the v0.3.0 GUI acceptance script (design spec criteria 1 and 3, at
[docs/superpowers/specs/2026-10-15-gui-rewrite-design.md](superpowers/specs/2026-10-15-gui-rewrite-design.md)).
Run it after section 15 has returned the box to a clean state. The companion
guide is [docs/gui.md](gui.md); `<physical>` comes from section 8, and section
15's `-Purge` left no account, so the wizard in 16.2 signs in afresh.

> **Out of scope — the "Set up Kumiho" gate page.** The inventory
> ([docs/gui-feature-inventory.md](gui-feature-inventory.md), line 515)
> describes a hidden page that runs the installer when the daemon is
> unreachable; it is **not** part of v0.3.0 and does not ship (`router.js`
> registers only the six rails plus `onboarding`). Do not test it. A first-run
> user (`onboarded = false`) is pinned to the wizard, so the Diagnostics
> service **Start**/**Stop** buttons — the only in-app service recovery — are
> unreachable until setup completes; recover a stopped `kumiho` service during
> first run from a shell (`Start-Service kumiho`) or by re-running
> `install.cmd`.

### 16.1 Install, pipe access and the launcher

- [ ] Download `kumiho-v0.3.0-windows-amd64.zip` and `SHA256SUMS`, verify and
      extract with Explorer (right-click → Extract All, so the Mark of the Web
      is preserved), then list it:

      (Get-FileHash .\kumiho-v0.3.0-windows-amd64.zip -Algorithm SHA256).Hash
      Get-Content .\SHA256SUMS | Select-String windows-amd64
      Get-ChildItem .\kumiho-v0.3.0-windows-amd64

  Expect: the two hashes match (case-insensitive), and the folder holds
  `kumiho.exe`, `kumiho-gui.exe`, `wintun.dll`, `install.cmd`, `install.ps1`,
  `LICENSE`, `NOTICE` and `third_party` (with `third_party\wintun\LICENSE.txt`).

- [ ] Install (allow the script and unblock the download):

      Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
      Unblock-File .\install.ps1, .\install.cmd, .\kumiho.exe, .\kumiho-gui.exe, .\wintun.dll
      .\install.ps1

  Expect, in order: group; state; binary (`kumiho.exe`, `kumiho-gui.exe` and
  `wintun.dll` copied to `C:\Program Files\Kumiho`); installer copies;
  Start Menu shortcut; PATH; config; service; group membership; pipe access;
  login entry; start. The `pipe access` line runs **before** the service
  (re)starts, so the GUI can connect without a sign-out.

- [ ] Verify the GUI files, shortcut and login entry:

      Test-Path 'C:\Program Files\Kumiho\kumiho-gui.exe'    # True
      Test-Path "$env:ProgramData\Microsoft\Windows\Start Menu\Programs\Kumiho.lnk"   # True
      (New-Object -ComObject WScript.Shell).CreateShortcut("$env:ProgramData\Microsoft\Windows\Start Menu\Programs\Kumiho.lnk").TargetPath
      Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' -Name 'Kumiho GUI'

  Expect: `True`, `True`; the shortcut's `TargetPath` is
  `C:\Program Files\Kumiho\kumiho-gui.exe`; the Run value's `Kumiho GUI` data
  is `"C:\Program Files\Kumiho\kumiho-gui.exe" --tray-start` (default on).

- [ ] Verify the control-pipe allowlist (the installer's immediate grant):

      Get-Content "$env:ProgramData\kumiho\pipe-users"
      (Get-Acl "$env:ProgramData\kumiho\pipe-users").Access |
        Format-Table IdentityReference,FileSystemRights
      (New-Object Security.Principal.NTAccount($env:USERNAME)).Translate('SecurityIdentifier').Value

  Expect: `pipe-users` holds your canonical SID, one per line; only
  `NT AUTHORITY\SYSTEM` and `BUILTIN\Administrators` have control (the file is
  admin-only); the last line equals the SID in the file.

- [ ] Launcher and version:

      kumiho gui
      Start-Sleep -Seconds 2
      Get-Process kumiho-gui -ErrorAction SilentlyContinue | Select-Object Id,ProcessName
      & 'C:\Program Files\Kumiho\kumiho-gui.exe' --version

  Expect: `kumiho gui` returns immediately (exit 0) and a `kumiho-gui` process
  runs; `--version` prints `kumiho-gui <version> <commit>` in the console and
  exits without opening a window. Quit the app before the wizard pass so 16.2
  starts clean: tray icon → **Quit** (or `Stop-Process -Name kumiho-gui`).

### 16.2 First run: the setup wizard

Launch the app from the Start Menu (`Kumiho`) so the shortcut is exercised.
The wizard has five steps and **no** rail or header; closing the window
mid-wizard and relaunching starts it again (the consent tick and the step are
never persisted).

- [ ] Step 1 — consent. Read the two panels (*What Kumiho does*, *What to keep
      in mind*) and try **Continue** before ticking.

  Expect: heading `Welcome to Kumiho`; **Continue** is disabled until the
  **I understand** box is ticked.

- [ ] Step 2 — sign in with the Firefox account that holds the VPN (email →
      password → 2FA code, or the "check your email and open the Mozilla
      sign-in link" page). If the daemon already holds a session, the step
      shows `Signed in as <email>` with **Continue** and **Sign out**.

  Expect: a successful sign-in advances the step; the daemon stores the tokens
  on this PC and never the password.

- [ ] Step 3 — health check. It runs automatically on first show.

  Expect: heading `Health check`; rows render as
  `Name: <status word>: <detail> — <remediation>` (remediation for
  Warning/Failed only); **Re-run checks** repeats the run; `Continue` is
  enabled with warnings, but a `Failed` `Kumiho service` row means fix it
  first.

- [ ] Step 4 — almost done.

  Expect: heading `Almost done`; an **Autoconnect** toggle showing the daemon's
  current value (`on` on a fresh install); click to flip it, then flip it back
  to `on`; **Finish** advances.

- [ ] Step 5 — you're all set.

  Expect: heading `You're all set`; **Open Kumiho** records setup complete and
  lands on the Dashboard. Relaunching the app now goes straight to the shell
  (no wizard).

### 16.3 Shell and Dashboard

- [ ] The shell frame.

  Expect: header title `Dashboard` on the left; the status word on the right
  (`Off`/`Connected`/`Proxy only`/`Connecting`/`Reconnecting`/`Waiting for
  network`/`Not signed in`/`Error`, or `Loading...`/`Daemon unreachable`/
  `Status unavailable`); the rail lists `Dashboard`, `Locations`, `Settings`,
  `Account`, `Logs`, `Diagnostics`.

- [ ] Connect from the Dashboard.

  Expect: click **Connect** (the button disables until the action settles; up
  to ~90 s); on success the header status reads `Connected` and the notice
  reads `Connected`. The **Options** popover offers **Location** (`Saved selection`
  plus countries as `Name (CC)`), **Proxy only** (*Keep system routing
  untouched; SOCKS5 proxy only.*), **Kill switch** (`On`/`Off`/`Last`, seeded
  from the setting) and its own **Connect**.

- [ ] The status card.

  Expect: rows appear as data arrives, in order `Uptime`, `Location` (`CC` or
  `CC (server)`), `Exit IP` (`IP` or `IP (country)`), `Rates`, `Totals`,
  `Kill switch` (`On`/`Off`), `Autoconnect` (`On`/`Off`), `Token expiry`,
  `Pass expiry`, `Quota` (with a remaining-quota bar), `Quota resets`. Re-run
  the section 8 egress check to confirm the GUI agrees:

      curl.exe -4 -s https://www.cloudflare.com/cdn-cgi/trace

  Expect: the `ip=` line equals the card's `Exit IP` and is not the physical
  IP from section 8.

- [ ] **Refresh** and the notice row.

  Expect: **Refresh** reloads the status; the notice row shows the latest
  daemon message, with a **Retry** button only when the first status load
  failed on a reachable daemon.

### 16.4 Locations

- [ ] Open `Locations`.

  Expect: **Refresh** and **Test speed**; a **Filter** box (placeholder
  `Country or city`) that narrows the country → city → server tree live as you
  type; the recommended region first; the persisted server marked
  `· selected`; latency shown as `· 42 ms` or `· timeout` after a test.

- [ ] Exercise the tree.

  Expect: **Test speed** probes the visible servers and streams latency labels
  (the button disables while it runs); clicking a country or city expands or
  collapses it; clicking a server row saves the selection (status
  `Location saved`) **without** connecting; **Refresh** reloads the list.

### 16.5 Settings

The screen has `Kumiho app` (per-user) and `Background service` (daemon) rows
and a **Refresh** action.

- [ ] `Kumiho app` — appearance. **Theme** offers `Light`/`Dark`/`OLED` and
      **Accent** offers `Blue`/`Teal`/`Green`/`Orange`/`Rose`/`Violet`. Pick
      `Dark`, then re-open `Settings` and pick `Light`; pick `Orange`, then
      re-open `Settings` and pick `Blue`.

  Expect: the palette changes immediately with the pick, and the native title
  bar follows the theme (`Dark` is dark, `Light` is light, `OLED` is dark on
  black). Because the shell reloads the view to apply the theme, the reload
  lands on the **Dashboard**, not on Settings; re-open Settings to confirm the
  choice stuck (connected state and daemon data survive — they come from the
  daemon). **Known divergence:** the theme reload returns to Dashboard instead
  of the screen you were on (inventory §Settings promises "returns to the same
  screen"); tracked for a follow-up fix.

- [ ] `Kumiho app` — start at login and update check.

  Expect: **Start at login** toggles `on`/`off`; turning it off removes the
  `Kumiho GUI` Run value, turning it on writes
  `"C:\Program Files\Kumiho\kumiho-gui.exe" --tray-start` (status
  `Start at login saved`). The **Version** row shows the running version;
  **Check for updates** shows `Checking for updates...` then either
  `Kumiho <v> is available` with **Open release page** (opens the browser) or
  `Kumiho is up to date`. The check is manual only.

- [ ] `Background service` rows.

  Expect, in order: `Kill switch`, `Autoconnect`, `Log level`
  (`debug`/`info`/`warn`/`error`), `Redact targets`, `Exit check`,
  `DoH provider` (`automatic`/`cloudflare`/`google`/`quad9`/`off`),
  `DNS upstream` (cue `(provider default)`), `Edge address` (cue `(resolve)`),
  `Upstream proxy` (cue `(direct)`), `Exclude CIDRs` (cue `(none)`),
  `Allow LAN`, `MTU`, `SOCKS port`, `Quota poll`. Set `Kill switch` to `off`
  and confirm the Dashboard `Kill switch` row follows immediately, then set it
  back to `on`; cycle `Log level` once (applies immediately); edit one text row
  and **Apply** — a changed row shows an `overridden` mark and a **Reset**
  button, and **Reset** returns it to the configured default (status
  `<Row> reset to the configured value`).

> **v0.4.0 rows.** v0.4.0 adds `TLS split`, `HTTP proxy`, `HTTP proxy port`,
> `Solve challenges` and `Keepalive profile` to the `Background service` rows
> and makes `Upstream proxy` functional. They are exercised in section 18.2;
> the steps above stay the v0.3.0 script.

### 16.6 Logs

- [ ] Open `Logs`.

  Expect: action row `Level: info`, `Follow: on`, **Refresh**; a **Search** box
  (placeholder `Tag or message`) with **Clear**, **Export** and
  **Open log folder**; the console shows `HH:MM:SS LVL [tag] message` lines,
  newest at the bottom, tinted by level.

- [ ] Exercise the controls.

  Expect: the level button cycles `debug → info → warn → error → debug` and
  re-filters; **Follow** toggles `Follow: on`/`Follow: off`; typing in
  **Search** filters live on tag or message; **Clear** empties the console
  (status `Logs cleared`); **Open log folder** opens
  `%LOCALAPPDATA%\kumiho\logs` (status `Opened <dir>`).

- [ ] Export.

  Expect: **Export** writes the visible entries and reports
  `Exported to <path>`:

      Get-ChildItem "$env:LOCALAPPDATA\kumiho\logs" |
        Sort-Object LastWriteTime -Descending | Select-Object -First 3

  Expect: a fresh `kumiho-logs-<YYYYMMDD-HHMMSS>.log` file exists.

### 16.7 Diagnostics

- [ ] Open `Diagnostics`. The check suite runs automatically on the first
      visit each launch.

  Expect: action row **Re-run checks**, **Copy details**, **Open log folder**,
  **Cleanup…**; the results list shows chips `OK`/`Warning`/`Failed`/`Skipped`
  with friendly titles such as `Platform`, `Local proxy port`, `System clock`,
  `Mozilla servers`, `Kumiho service`, `Tunnel driver`, `Windows firewall
  service`, `Service connection`, `Tunnel adapter`, `Tunnel DNS`,
  `Internet route`, `Administrator rights`; the summary line reports the
  counts. Compare against the shell:

      kumiho doctor

  Expect: the same checks and counts (`0 fail` when the box is healthy).

- [ ] **Re-run checks** and **Copy details**.

  Expect: re-run shows `Checking…` then a fresh report; **Copy details** puts
  the version plus the report JSON on the clipboard (status `Details copied to
  the clipboard`).

- [ ] The **Kumiho service** card (needs a UAC prompt).

  Expect: the card shows the service chip (`Running` when healthy) with
  **Start**/**Stop**. Click **Stop**; approve the UAC prompt; the chip goes
  `Stopping` → `Stopped`, the header flips to `Daemon unreachable`, and the
  notice reads `Service stopped`. Click **Start**; approve; the chip returns to
  `Running` and the notice reads `Service started`. Decline a UAC prompt once
  and confirm the action is cancelled with
  `could not stop the service: administrator approval was cancelled` and no
  partial change.

- [ ] **Cleanup…** (needs a UAC prompt).

  Expect: the button opens a dialog — *Remove stale network state left by a
  crashed Kumiho run?* — with **Cancel**/**Cleanup**; approving the UAC prompt
  then runs the full `kumiho cleanup` and reports `Cleanup finished` (or the
  section 11 caveat wording when it is a no-op).

### 16.8 Tray, close-to-tray and single instance

- [ ] The tray icon and tooltip.

  Expect: a fox icon in the notification area whose hover text is
  `Kumiho — <status word>`, matching the header.

- [ ] The tray menu (right-click the icon).

  Expect: `Connect`/`Disconnect` (the same rule as the Dashboard button); a
  `Location` submenu of the daemon's locations (`Name (CC)`, recommended
  first), or a disabled `Location: (none)` when none are known; **Show
  Kumiho**; **Diagnostics** (restores the window and opens that screen);
  **Quit**. Picking a location persists the selection.

- [ ] Close-to-tray and the one-time hint.

  Expect: closing the window (the `X`) hides it and keeps the app (and the
  tunnel) running — the tray icon stays. The **first** close shows the
  notification *Kumiho is still running. Right-click the tray icon to connect
  or quit.* Clicking the tray icon (**Show Kumiho**) brings the window back.

- [ ] Geometry restore.

  Expect: move and resize the window, **Quit** from the tray, then relaunch
  from the Start Menu; the window reopens at the saved size and position
  (clamped into the monitor work area).

- [ ] Single instance.

  With the app running, start the exe a second time:

      & 'C:\Program Files\Kumiho\kumiho-gui.exe'

  Expect: no second window; the existing window is restored and focused; the
  second process exits 0.

### 16.9 Account

- [ ] Open `Account` (if you signed out earlier, sign in again from a shell
      with `kumiho login` — the wizard does not reopen after setup is
      complete).

  Expect: rows `Email`, `UID`, `Subscription` (`Yes`/`No`/`Unknown`), `Quota`
  (e.g. `38.0 GiB remaining of 512.0 GiB`), `Bandwidth`
  (`Limited`/`Unlimited`), `Token expiry`; a **Refresh** action.

- [ ] Sign out.

  Expect: **Sign out** opens a confirmation dialog — *Sign out of Kumiho?* /
  *The stored sign-in tokens are cleared and the tunnel disconnects.* — with
  **Cancel** focused by default. Confirm; you **stay on the Account screen**:
  the card disappears and the status line reads `Not signed in` (the wizard
  does **not** reopen, since setup is already complete). To restore a session
  for later steps, sign in again from a shell (the daemon session is shared):

      kumiho login

  Expect: after **Refresh**, the Account card reappears.

### 16.10 Uninstall and residue

Quit the app first (tray → **Quit**): a running GUI blocks its own deletion.

- [ ] Uninstall, keeping state:

      .\install.ps1 -Uninstall

  Expect the console to report, in order: service stopped and deleted (or not
  installed); the machine PATH entry removed; the installer copies and
  `%ProgramFiles%\Kumiho` removed (the CLI, `kumiho-gui.exe` and `wintun.dll`);
  the Start Menu shortcut removed; the `Kumiho GUI` login entry removed (only
  when its data is exactly the installed command); your SID **removed** from
  `%ProgramData%\kumiho\pipe-users`; `%ProgramData%\kumiho` **kept**. Verify:

      Get-Service kumiho -ErrorAction SilentlyContinue       # nothing
      Test-Path 'C:\Program Files\Kumiho\kumiho-gui.exe'      # False
      Test-Path "$env:ProgramData\Microsoft\Windows\Start Menu\Programs\Kumiho.lnk"   # False
      Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' -Name 'Kumiho GUI' -ErrorAction SilentlyContinue   # nothing
      Get-Content "$env:ProgramData\kumiho\pipe-users" -ErrorAction SilentlyContinue   # absent, or no line for you
      Test-Path "$env:ProgramData\kumiho\tokens.json"          # True (kept)
      Get-LocalGroup kumiho                                    # still exists

  Expect: all as annotated — no service, GUI exe, shortcut, Run entry or pipe
  grant; tokens and the group survive.

- [ ] Purge, removing everything:

      .\install.ps1 -Uninstall -Purge

  Expect: `%ProgramData%\kumiho` and the local group `kumiho` are removed:

      Test-Path "$env:ProgramData\kumiho"          # False
      Get-LocalGroup kumiho -ErrorAction SilentlyContinue   # nothing

  Per-user `%APPDATA%\kumiho\gui.toml` (window geometry, theme, autostart) is
  intentionally left alone; delete it by hand for a clean slate.

## 17. Results and criterion map

| Spec criterion | Covered by |
| --- | --- |
| 1. One `kumiho.exe` for service/CLI/TUI; build + CI green | 1, 2, 3, 5–14 |
| 2. Service install, LocalSystem, autostart, autoconnect, crash recovery, clean uninstall | 3, 11, 12, 15 |
| 3. Full tunnel: `foxy0` 10.8.0.2/32 MTU 8500, edge traffic, exit check, IPv6 blackhole | 7, 8, 12 |
| 4. DNS parity: adapter-scoped resolver, DoH through tunnel, custom DNS, truncation | 9 |
| 5. Kill switch: WFP on/off, LAN/exclude-CIDR exceptions, cleanup after crash | 8, 10, 11 |
| 6. TUI/CLI over the pipe, group ACL, admin-only `shutdown` | 13 |
| 7. Hygiene: dns race, CI action bumps, comments, netmon tests, transport coverage, Android paths | 1 (gate + CI evidence) |
| 8. Docs: README/docs/PLAN Windows content, AGENTS.md, CHANGELOG.md, docs/windows-testing.md, NOTICE | 1, this file |
| 9. Tag-triggered release builds three zips + `SHA256SUMS` | 1, 2 |
| 10. This checklist executed on a real Windows 11 box before the tag | all; sign off below |
| GUI 1. GUI parity + polish verified on Windows 11 hardware | 16 |
| GUI 2. Local proof: all screens in Light/Dark/OLED via the dev harness | harness/CI (not a Windows step) |
| GUI 3. Install experience (service, tray, autostart, pipe access without re-login) and no uninstall residue | 16.1, 16.8, 16.10 |
| GUI 4. v0.3.0 zip ships `kumiho-gui.exe` beside `kumiho.exe` with `SHA256SUMS`; gate and CI cross-build the GUI | 16.1, the gate |
| GUI 5. No collateral change: daemon, protocol v1, CLI and TUI behaviorally untouched | the gate |

Sign-off (fill before tagging):

- Date: 2026-10-14
- Machine / Windows build: DESKTOP-16KGA46 / Windows 11 amd64
- Commit / tag SHA under test: v0.2.0-rc.2 (c277aa2)
- Result: PASS (partial - see notes)
- Notes: verified on real hardware: `install.ps1` upgrade, the Windows service
  (auto-start, LocalSystem), `kumiho doctor`, `kumiho version`, and the TUI
  full tunnel with geo-blocked sites reaching the selected edge. The checklist
  was not run top-to-bottom before the `v0.2.0` tag (owner's call). The test
  network intermittently filters DNS for Mozilla control-plane hosts
  (`vpn.mozilla.org`, `firefox.settings.services.mozilla.com`), which surfaces
  as `endpoints`/`connect` "no such host" failures; this is environmental, not
  a client defect.

v0.3.0 GUI acceptance sign-off (fill before the `v0.3.0` tag):

- Date:
- Machine / Windows build:
- Commit / tag SHA under test:
- Result: PASS / FAIL
- Notes:

## 18. v0.4.0 backend parity acceptance

This is the v0.4.0 delta (design spec criteria 1–11 and 13, at
[docs/superpowers/specs/2026-10-20-v0.4.0-backend-parity-design.md](superpowers/specs/2026-10-20-v0.4.0-backend-parity-design.md)).
It runs top to bottom in an elevated Windows PowerShell 5.1 session. The
OpenSSH client in 18.5 is the only suggested external tool; that step is N/A
when you have no proxy, and Wireshark in 18.4 is optional. `<physical>` is the
physical adapter alias from section 8.

### 18.1 Upgrade and version

Run this on a box with a working v0.3.0 install. On the sections 1–17 box,
defer the uninstalls in 15 and 16.10 to 18.10. A clean install also works, but
the upgrade-specific lines below are then N/A — record that.

- [ ] Quit the GUI (tray → **Quit**): a running `kumiho-gui.exe` blocks its
      own replacement. Download `kumiho-v0.4.0-windows-amd64.zip` and
      `SHA256SUMS`, verify, and extract with Explorer (right-click → Extract
      All, so the Mark of the Web is preserved):

      (Get-FileHash .\kumiho-v0.4.0-windows-amd64.zip -Algorithm SHA256).Hash
      Get-Content .\SHA256SUMS | Select-String windows-amd64
      Get-ChildItem .\kumiho-v0.4.0-windows-amd64

  Expect: the two hashes match (case-insensitive), and the folder holds the
  v0.4.0 kit: `kumiho.exe`, `kumiho-gui.exe`, `wintun.dll`, `install.ps1`,
  `install.cmd`, `LICENSE`, `NOTICE` and `third_party` (with
  `third_party\wintun\LICENSE.txt` and
  `third_party\webview2-loader\LICENSE.txt`).

- [ ] Note the state, then run the installer from the extracted v0.4.0
      folder (it is idempotent and upgrades over v0.3.0):

      Test-Path "$env:ProgramData\kumiho\tokens.json"
      Get-ChildItem "$env:ProgramData\kumiho" -ErrorAction SilentlyContinue | Select-Object Name
      Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
      Unblock-File .\install.ps1, .\kumiho.exe, .\kumiho-gui.exe, .\wintun.dll
      .\install.ps1

  Expect, in order: the local group reused; `%ProgramData%\kumiho` and `logs`
  kept, with their ACLs reapplied; `stopped service kumiho to replace the binary`
  (absent if the service was already stopped); `copied kumiho.exe to
  C:\Program Files\Kumiho` (and the GUI and `wintun.dll`); the installer
  copies, shortcut and PATH refreshed; then `C:\ProgramData\kumiho\config.toml
  exists; leaving it untouched`; `service kumiho already exists; keeping its
  registration`; crash recovery reconfigured; group, pipe access and login
  entry refreshed; `restarted service kumiho with the new binary` (the upgrade
  stops the running service first; a stopped service prints `started service
  kumiho` instead); `installed; service kumiho is running`.

- [ ] Verify the upgraded install:

      Get-Service kumiho | Format-List Name,Status,StartType
      kumiho version
      kumiho doctor
      Test-Path "$env:ProgramData\kumiho\tokens.json"
      $cfg = "$env:ProgramData\kumiho\config.toml"
      $pattern = 'tls_split|http_proxy|solve_challenges|keepalive_profile'
      Select-String -Path $cfg -Pattern $pattern

  Expect: `Running`, `Automatic`; `kumiho v0.4.0 ...`; a `0 fail` doctor
  summary; `True` if you were signed in before the upgrade (state is kept).
  If `kumiho` is not found, open a new elevated shell first (the machine PATH
  applies to new shells). The `Select-String` finds nothing unless you added
  keys by hand: an existing `config.toml` is not rewritten, so the five new
  keys appear as built-in defaults (or as overrides in `state.json`) in the
  clients rather than being inserted into the file.

### 18.2 Settings surface

- [ ] Open the GUI `Settings` screen (and optionally the TUI `s` screen) and
      find the `Background service` rows. The five v0.4.0 rows follow
      `Quota poll`.

  Expect (GUI labels): `TLS split` (`off`/`first-byte`/`sni`), `HTTP proxy`
  (`on`/`off`), `HTTP proxy port` (number, default `8080`), `Solve
  challenges` (`on`/`off`, default `on`) and `Keepalive profile`
  (`standard`/`slow`). `Upstream proxy` shows the cue `(direct)` and
  `Exclude CIDRs` shows `(none)` when unset. The TUI shows the same rows with
  its own labels (`TLS split`, `HTTP proxy`, `HTTP proxy port`,
  `solve challenges`, `keepalive profile`) and scrolls with `down`/`pgdown`/
  `end`; `keepalive profile` is its last row.

- [ ] Edit `TLS split` to `first-byte`.

  Expect: the row shows the `overridden` mark and a **Reset** button, and
  the settings status line reads `TLS split saved`. Click **Reset**.

  Expect: the mark and button disappear; the status reads
  `TLS split reset to the configured value`; the row returns to the
  configured default (`off` on a stock config). In the TUI the equivalent
  notices are `tls_split updated` then `tls_split reset to default` (the TUI
  names the key, not the label).

- [ ] Confirm the apply timing. `Solve challenges` takes effect on save; the
      other four rows and `Upstream proxy` apply on the next connect, so a
      connected tunnel keeps its current values until you reconnect. 18.3 and
      18.6 exercise both paths.

### 18.3 Challenge solver on/off

- [ ] With `Solve challenges` on (the default), sign in and refresh the
      Account and Locations screens. If you are signed out, run `kumiho
      login` first.

  Expect: normal sign-in and refresh, with no challenge error. The solver
  logs `[fastly]` lines when a challenge occurred (they are warn-level, so
  visible at the default `info` level):

      kumiho logs --level debug | Select-String -Pattern 'fastly|challenge'

- [ ] Set `Solve challenges` off (it applies on save), then sign out and sign
      in again to force control-plane requests:

      kumiho logout
      kumiho login

  On a network known to answer with a Fastly `406`, expect an immediate,
  clear failure — `anti-bot challenge solving is disabled; enable
  solve_challenges and try again` — with no silent hang and no retry loop.
  With `Solve challenges` on again, repeat the sign-in.

  Expect: the solver answers the challenge and the request succeeds; the log
  shows `[fastly]` steps followed by the successful request.

- [ ] If no challenge can be provoked on this network, record that explicitly
      in the v0.4.0 notes and do not claim a solver success. Still verify the
      toggle persists (leave Settings, reopen it) and that normal sign-in and
      Account/Locations refresh work with both values.

### 18.4 TLS split

- [ ] Set `TLS split` to `first-byte`, disconnect and reconnect, then load a
      page through the tunnel:

      kumiho disconnect
      kumiho connect
      curl.exe -4 -s --max-time 20 https://www.cloudflare.com/cdn-cgi/trace
      kumiho status

  Expect: `state: CONNECTED`, and an `ip=` line equal to the status
  `exit ip:` — the tunnel, not the physical IP.

- [ ] Set `TLS split` to `sni`, reconnect, and repeat the trace and status
      check.

  Expect: the same result. Restore `TLS split` to `off` and reconnect:

      # Settings: TLS split = off
      kumiho disconnect
      kumiho connect

- [ ] Optional packet capture: with Wireshark on `$physical`, the first
      ClientHello record is split across two TCP segments (Wireshark
      reassembles the TLS message, so inspect the TCP segment lengths). Not
      mandatory; if the capture shows one segment, record that as a
      best-effort result rather than failing the section.

Note: the split is best effort, applies to the edge and control-plane HTTPS
handshakes, and is never applied to DoH or bootstrap. The ClientHello contents
are unchanged; only the way the record is written is split.

### 18.5 Upstream proxy

A practical local SOCKS5 proxy on Windows 11 is the built-in OpenSSH client.
If `ssh.exe` is missing, add the OpenSSH Client optional feature, or use any
SOCKS5 or HTTP proxy you control. All three schemes are accepted:
`socks5://`, `socks5h://` and `http://` (default ports 1080, 1080 and 80).
Use a port other than 1080 for the test proxy: 1080 is Kumiho's own SOCKS
listener, which is bound whenever the tunnel is up.

- [ ] Start the proxy in its own elevated PowerShell window and keep it open
      (`-v` shows each channel and the `host:443` it opens):

      ssh -N -D 1081 -v user@your-ssh-host

  Expect: no errors, and the window stays open. While it runs,
  `Test-NetConnection 127.0.0.1 -Port 1081 -InformationLevel Quiet` is
  `True`.

- [ ] Set `Upstream proxy` to `socks5h://127.0.0.1:1081`, reconnect, and
      verify the tunnel and the control plane:

      kumiho disconnect
      kumiho connect
      curl.exe -4 -s --max-time 20 https://www.cloudflare.com/cdn-cgi/trace
      kumiho locations
      kumiho status

  Expect: `CONNECTED`; the trace shows the tunnel exit IP; the locations list
  still loads. In the ssh window, edge connections to the edge hostname on
  port 443 appear — the proxy resolves the edge hostname, so there is no
  local lookup for it. Only the edge dial uses the hop: the control plane,
  Remote Settings, DoH and bootstrap stay direct.

- [ ] Set a deliberately wrong port, then reconnect and confirm there is no
      silent direct fallback:

      # Settings: Upstream proxy = socks5h://127.0.0.1:1
      kumiho disconnect
      kumiho connect
      kumiho status
      kumiho logs --level debug | Select-String -Pattern 'upstream_proxy|dial proxy'

  Expect: `kumiho connect` fails with a clear proxy/dial error (allow up to
  ~30 s); the state is not `CONNECTED`; the log names `upstream_proxy` or
  `dial proxy 127.0.0.1:1`, never credentials, and no direct edge dial
  follows. Restore the empty (`(direct)`) value and reconnect:

      # Settings: Upstream proxy = (empty)
      kumiho connect
      kumiho status

  Expect: `CONNECTED` and normal tunnel traffic again.

`user:pass@` is accepted in the URL (for example
`socks5h://user:pass@proxy.example.net:1080`, or an HTTP proxy such as
`http://proxy.example.net:3128`); omit it when your proxy has no auth. This
Windows step exercises one scheme; the gate's in-test SOCKS5 and HTTP proxies
cover all three schemes and credentials. If no proxy is available, mark this
step N/A in the notes; do not invent a pass. Stop the proxy shell (Ctrl+C)
once you are back on `(direct)`.

### 18.6 HTTP proxy

- [ ] Check the port is free, then set `HTTP proxy` on and `HTTP proxy port`
      to `8080` (or another free port), and reconnect:

      Get-NetTCPConnection -LocalPort 8080 -State Listen -ErrorAction SilentlyContinue
      # Settings: HTTP proxy = on, HTTP proxy port = 8080
      kumiho disconnect
      kumiho connect

  Expect: `CONNECTED`. If 8080 was busy, use a free port and say so in the
  notes; a collision is reported as a connect failure.

- [ ] Verify the listener and both request forms through the tunnel:

      Test-NetConnection 127.0.0.1 -Port 8080 -InformationLevel Quiet
      Get-NetTCPConnection -LocalPort 8080 -State Listen |
        Select-Object LocalAddress,LocalPort,OwningProcess
      curl.exe -x http://127.0.0.1:8080 -s --max-time 30 https://www.cloudflare.com/cdn-cgi/trace
      curl.exe -x http://127.0.0.1:8080 -s --max-time 30 http://example.com
      kumiho status

  Expect: `True`; the listener's `LocalAddress` is `127.0.0.1` (not
  `0.0.0.0` or a LAN address, so it is loopback-only); the HTTPS trace has an
  `ip=` line equal to the status `exit ip:` (re-run `kumiho status` after a
  few seconds if the exit check is still empty); the plain-HTTP request
  returns HTML.

- [ ] Confirm there is no auth challenge and no TLS interception:

      curl.exe -x http://user:pass@127.0.0.1:8080 -s --max-time 30 -o NUL -w "status %{http_code}\n" https://www.cloudflare.com/cdn-cgi/trace

  Expect: `status 200` whether or not credentials are in the proxy URL; the
  proxy never answers `407 Proxy Authentication Required`, and the
  `Proxy-Authorization` header is stripped before the tunnel. HTTPS goes
  through `CONNECT` end to end — the trace page's certificate is accepted by
  curl with no proxy interception.

- [ ] Set `HTTP proxy` off, reconnect, and confirm the listener is closed:

      # Settings: HTTP proxy = off
      kumiho disconnect
      kumiho connect
      Test-NetConnection 127.0.0.1 -Port 8080 -InformationLevel Quiet
      kumiho status

  Expect: `False` (a `WARNING: TCP connect ... failed` line is normal), and
  the tunnel is still `CONNECTED`. The listener starts and stops with the
  tunnel.

Note: the HTTP proxy is loopback-only and unauthenticated, accepts `CONNECT`
and absolute-form plain HTTP with one request per connection, and refuses
local-network IP targets. It does not intercept TLS.

### 18.7 DNS over SOCKS5 UDP

The relay exists in both full and proxy-only mode. This client runs on
Windows PowerShell 5.1, needs no module, and is copy-pasteable as one block:
it opens a TCP control connection to the local SOCKS5 listener, requests
`UDP ASSOCIATE`, sends one DNS A query for `example.com` to `1.1.1.1:53`
through the relay, and parses the reply. The destination address is a marker:
the relay answers from the same in-tunnel DoH chain, not from plaintext UDP to
`1.1.1.1`. Start from `DoH provider = automatic` (the default; section 9
restores it if you ran that section).

- [ ] With the tunnel connected in full or proxy-only mode, confirm the
      listener and run the client:

      Test-NetConnection 127.0.0.1 -Port 1080 -InformationLevel Quiet

      function Resolve-DnsViaSocksUdp {
          param(
              [string]$Name = 'example.com',
              [string]$Server = '1.1.1.1',
              [string]$Proxy = '127.0.0.1:1080'
          )
          $proxyHost, $proxyPort = $Proxy -split ':'
          $proxyPort = [int]$proxyPort

          # DNS query: ID 0x4b75, RD=1, one A/IN question.
          $q = [System.Collections.Generic.List[byte]]::new()
          foreach ($b in [byte[]](0x4b,0x75, 0x01,0x00, 0x00,0x01,
                  0x00,0x00, 0x00,0x00, 0x00,0x00)) { $q.Add($b) }
          foreach ($label in $Name -split '\.') {
              $b = [System.Text.Encoding]::ASCII.GetBytes($label)
              $q.Add([byte]$b.Length)
              foreach ($x in $b) { $q.Add($x) }
          }
          foreach ($b in [byte[]](0x00, 0x00,0x01, 0x00,0x01)) { $q.Add($b) }

          $tcp = [System.Net.Sockets.TcpClient]::new()
          $tcp.Connect($proxyHost, $proxyPort)
          $stream = $tcp.GetStream()
          $stream.ReadTimeout = 5000
          $stream.Write([byte[]](0x05,0x01,0x00), 0, 3)
          $greeting = [byte[]]::new(2)
          $got = 0
          while ($got -lt 2) {
              $n = $stream.Read($greeting, $got, 2 - $got)
              if ($n -le 0) { throw 'SOCKS5 greeting was truncated' }
              $got += $n
          }
          if ($greeting[0] -ne 5 -or $greeting[1] -ne 0) {
              throw 'SOCKS5 greeting was refused'
          }

          # UDP ASSOCIATE with 0.0.0.0:0 (the relay binds its own UDP socket).
          $stream.Write([byte[]](0x05,0x03,0x00,0x01, 0,0,0,0, 0,0), 0, 10)
          $reply = [byte[]]::new(10)
          $got = 0
          while ($got -lt 10) {
              $n = $stream.Read($reply, $got, 10 - $got)
              if ($n -le 0) { throw 'SOCKS5 UDP ASSOCIATE reply was truncated' }
              $got += $n
          }
          if ($reply[0] -ne 5 -or $reply[1] -ne 0 -or $reply[3] -ne 1) {
              throw ('SOCKS5 UDP ASSOCIATE failed: ' +
                  (($reply | ForEach-Object { $_.ToString('x2') }) -join ' '))
          }
          $relay = [System.Net.IPEndPoint]::new(
              [System.Net.IPAddress]::new([byte[]]($reply[4..7])),
              (($reply[8] -shl 8) -bor $reply[9]))

          # RFC 1928 UDP request: RSV=0, FRAG=0, ATYP=IPv4,
          # server:53, DNS payload.
          $dns = $q.ToArray()
          $packet = [byte[]]::new(10 + $dns.Length)
          $packet[3] = 1
          [Array]::Copy(
              [System.Net.IPAddress]::Parse($Server).GetAddressBytes(),
              0, $packet, 4, 4)
          $packet[9] = 53
          [Array]::Copy($dns, 0, $packet, 10, $dns.Length)

          $udp = [System.Net.Sockets.UdpClient]::new()
          $udp.Client.ReceiveTimeout = 15000
          [void]$udp.Send($packet, $packet.Length, $relay)
          $from = [System.Net.IPEndPoint]::new(
              [System.Net.IPAddress]::Any, 0)
          $response = $udp.Receive([ref]$from)
          $udp.Dispose(); $stream.Dispose(); $tcp.Dispose()

          if ($response.Length -lt 22 -or $response[0] -ne 0 -or
              $response[1] -ne 0 -or $response[2] -ne 0 -or
              $response[3] -ne 1) {
              throw 'SOCKS5 UDP reply header was invalid'
          }
          $dnsLen = $response.Length - 10
          $dnsResp = [byte[]]::new($dnsLen)
          [Array]::Copy($response, 10, $dnsResp, 0, $dnsLen)
          if ($dnsResp[0] -ne 0x4b -or $dnsResp[1] -ne 0x75) {
              throw 'DNS transaction ID did not match'
          }
          $rcode = $dnsResp[3] -band 0x0f
          $ancount = ($dnsResp[6] -shl 8) -bor $dnsResp[7]
          if ($rcode -ne 0 -or $ancount -lt 1) {
              throw "DNS response rcode=$rcode answers=$ancount"
          }

          $offset = 12
          while ($dnsResp[$offset] -ne 0) {
              $offset += $dnsResp[$offset] + 1
          }
          $offset += 5
          $addresses = @()
          for ($i = 0; $i -lt $ancount; $i++) {
              if (($dnsResp[$offset] -band 0xc0) -eq 0xc0) {
                  $offset += 2
              } else {
                  while ($dnsResp[$offset] -ne 0) {
                      $offset += $dnsResp[$offset] + 1
                  }
                  $offset += 1
              }
              $type = ($dnsResp[$offset] -shl 8) -bor $dnsResp[$offset + 1]
              $rdlen = ($dnsResp[$offset + 8] -shl 8) -bor $dnsResp[$offset + 9]
              $offset += 10
              if ($type -eq 1 -and $rdlen -eq 4) {
                  $a = [System.Net.IPAddress]::new(
                      [byte[]]($dnsResp[$offset..($offset + 3)]))
                  $addresses += $a.ToString()
              }
              $offset += $rdlen
          }
          if ($addresses.Count -lt 1) {
              throw 'no A record in the DNS response'
          }
          "UDP associate OK: $Name -> $($addresses -join ', ')"
      }
      Resolve-DnsViaSocksUdp

  Expect: `True`, then `UDP associate OK: example.com -> <address>, ...`
  with one or more A records. Allow up to 15 s for the DoH lookup. Repeat in
  the other transport mode if you have time; the relay exists in both. The
  function stays defined in this shell, so later steps can call it by name;
  re-paste the block if you restart the shell.

- [ ] Prove there is no plaintext fallback. Clear `DNS upstream` first (as in
      section 9's corrected step), set `DoH provider` to `off`, then
      disconnect and reconnect:

      # Settings: DNS upstream = (empty), DoH provider = off
      kumiho disconnect
      kumiho connect
      Resolve-DnsViaSocksUdp

  Expect: the client throws a non-zero rcode — SERVFAIL is
  `DNS response rcode=2 answers=0` — not an answer. That is the daemon's
  no-fallback path: the relay only resolves through DoH and never dials port
  53. Optionally set `Log level` to `debug` before reconnecting to also see
  the daemon line `answering SERVFAIL` (the default `info` level does not
  record it):

      kumiho logs --level debug | Select-String -Pattern 'SERVFAIL'

  Restore `DoH provider` to `automatic` (leave `DNS upstream` empty),
  disconnect and reconnect, then run the client once more:

      # Settings: DoH provider = automatic, DNS upstream = (empty)
      kumiho disconnect
      kumiho connect
      Resolve-DnsViaSocksUdp

  Expect: `UDP associate OK: example.com -> ...` again.

Note: the relay is DNS-only (target port 53 or 0). Non-DNS UDP and foreign
sources are dropped, so this is not QUIC, game or call support; a provider of
`off` answers SERVFAIL.

### 18.8 Keepalive profile

- [ ] Set `Keepalive profile` to `slow`, reconnect, and leave the tunnel
      idle past the ping window, then check traffic:

      kumiho disconnect
      kumiho connect
      Start-Sleep -Seconds 45
      kumiho status
      curl.exe -4 -s --max-time 20 https://www.cloudflare.com/cdn-cgi/trace

  Expect: still `CONNECTED` after the idle window, and the trace returns the
  tunnel exit IP. Restore `standard` and reconnect:

      # Settings: Keepalive profile = standard
      kumiho disconnect
      kumiho connect

The ping schedule is unchanged in both profiles (15 s idle, then a check every
3 s); only the unanswered-ping timeout changes: 10 s on `standard`, 25 s on
`slow`. A true lossy-link test is optional; do not claim the 25 s timeout was
observed unless it was.

### 18.9 Sign-out residue

- [ ] Connect, let some traffic flow (the trace above is enough), then sign
      out:

      kumiho logout
      kumiho status
      Test-Path "$env:ProgramData\kumiho\tokens.json"

  Expect: `Signed out.`; the tunnel is disconnected; `state:` is
  `UNAUTHENTICATED`; no `account:`, `token expires:`, `pass expires:`,
  `quota:`, `exit ip:`, `totals:` or `rates:` lines; `False` for
  `tokens.json`.

- [ ] Check the GUI: `Account` shows `Not signed in` (the card is gone) and
      `Dashboard` carries no account, quota, exit IP, totals or rates from
      the old session. The setup wizard does not reopen.

  Expect: as described (click **Refresh** if a screen still shows old data).
  Sign in again before the next step:

      kumiho login

Plain `kumiho disconnect` is not sign-out: it may keep the last session's
`totals:` until you sign out.

### 18.10 Uninstall and residue

- [ ] Disconnect and confirm the v0.4.0 listeners are gone:

      kumiho disconnect
      Get-NetTCPConnection -LocalPort 1080,8080 -State Listen -ErrorAction SilentlyContinue

  Expect: nothing for either port (adjust 1080 and 8080 if you changed the
  SOCKS or HTTP proxy port). The SOCKS listener exists in both full and
  proxy-only mode; the HTTP proxy listens only on `http_proxy_port`; both
  stop with the tunnel.

- [ ] Quit the GUI (tray → **Quit**), then run the section 15 uninstall
      path (section 16.10 when the GUI is installed) from the extracted
      v0.4.0 kit, keeping state, then repeat the listener check and the
      residue checks by reference:

      .\install.ps1 -Uninstall
      Get-NetTCPConnection -LocalPort 1080,8080 -State Listen -ErrorAction SilentlyContinue
      Get-Service kumiho -ErrorAction SilentlyContinue
      Test-Path 'C:\Program Files\Kumiho'
      Test-Path "$env:ProgramData\kumiho\tokens.json"

  Expect: the section 15/16.10 console lines and residue results (service,
  binaries, PATH, shortcut/login entry, adapter, metric-9999 routes,
  group/pipe grant), plus no listener on either port and `%ProgramData%\kumiho`
  kept with its tokens. `-Purge` additionally removes state/tokens and the
  group; per-user `%APPDATA%\kumiho\gui.toml` and `tui.toml` are
  intentionally left.

### 18.11 Results and sign-off

| v0.4.0 spec criterion | Covered by |
| --- | --- |
| 1. Strict in-tunnel DNS: no port-53 path, endpoint failover, 30-minute suppression | 9, 18.7 (failover and suppression by unit/integration tests) |
| 2. Server-list cache: atomic write, fallback, corrupt cache ignored | unit tests; not a Windows step (needs a forced fetch failure) |
| 3. Challenge solving: 406 handled, cookies installed, clear failure, no hang | 18.3 (when the network provides a challenge; error path in gate tests) |
| 4. TLS fragmentation: `off`/`first-byte`/`sni`, edge and control plane, not DoH/bootstrap | 18.4; unit test for the offset |
| 5. Upstream proxy: three schemes, credentials, edge-only, remote resolution | 18.5 (one available scheme; all three and auth in gate tests) |
| 6. Local HTTP proxy: loopback, CONNECT + absolute-form HTTP, stops with the tunnel | 18.6 |
| 7. DNS-only UDP ASSOCIATE: port 53/0, DoH chain, SERVFAIL when off, both modes | 18.7 (one transport mode; both modes in gate tests) |
| 8. Keepalive profile: `standard` 10 s / `slow` 25 s, next connect | 18.8 (profile applies; timeout values in unit tests) |
| 9. Settings surface complete: config, overrides, TUI scrolling, GUI rows, defaults, reset | 16.5, 18.2; validation and defaults in the gate |
| 10. Clean sign-out: quota, exit, rates, totals and pass expiry wiped | 18.9 |
| 11. Documentation is true: CHANGELOG, README, PLAN, docs, inventories, NOTICE, AGENTS | docs review (this file); not a Windows step |
| 13. GUI proof: new rows in the dev harness under Light/Dark/OLED; accessibility floor | harness/Playwright (not a Windows step); 18.2 on Windows |

Criteria 12 (gate green) and 14 (release framing) are release hygiene, not
Windows steps.

v0.4.0 backend parity acceptance sign-off (complete with the v0.4.1 bug-fix
pass):

- Date:
- Machine / Windows build:
- Commit / tag SHA under test:
- Result: PASS / FAIL
- Notes:

v0.4.0 was tagged before this Windows pass; the results here are completed
with the v0.4.1 fixes.
