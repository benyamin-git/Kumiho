# Windows 11 manual end-to-end checklist (v0.2.0)

This is the committed manual E2E checklist from spec criterion 10. Run it top
to bottom on a **fresh Windows 11 amd64 machine** (or VM) before tagging
`v0.2.0`, and record the result of every step. A step passes only when the
observed output matches "Expect"; if anything diverges, file it and stop the
release.

Everything runs in PowerShell. The machine has no prior Kumiho install; if you
are re-running the list, finish section 15 first so the box is clean again.
`<physical>` below is the machine's physical adapter alias (`Wi-Fi` or
`Ethernet`); take it from section 8 and substitute it everywhere.

Criterion coverage and the sign-off table are at the end. Background reading:
[docs/usage.md](usage.md), [docs/install.md](install.md),
[docs/troubleshooting.md](troubleshooting.md), the design spec at
[docs/superpowers/specs/2026-10-10-v0.2.0-windows-release-design.md](superpowers/specs/2026-10-10-v0.2.0-windows-release-design.md).

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

  Expect (Windows checks; order matters): `OK platform: windows (full tunnel
  supported)`, `OK windows-service: kumiho service is running; start type
  auto`, `OK wintun-dll: wintun.dll 0.14.1 beside the executable`,
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

  Expect: `resolver up on 10.8.0.2:53 (DoH through the tunnel)` and `[dns]`
  activity; no `dns` errors.

- [ ] Custom DNS and DoH off (TUI Settings, applies on the next connect):
      open `kumiho`, `s`, set `DoH provider` to `cloudflare`, set
      `DNS upstream` to `1.1.1.1` (enter; empty resets), `esc`, `space` to
      disconnect, `space` to reconnect, then:

      Resolve-DnsName example.com

  Expect: resolves; logs show the pinned DoH resolver. Change `DoH provider`
  to `off`, reconnect, `Resolve-DnsName example.com` still resolves (the
  fallback is DNS-over-TCP:53 through the tunnel). Set it back to
  `automatic` when done.

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
      the crash residue survives):

      sc.exe failure kumiho reset= 0 actions= ""
      $svcPid = (Get-CimInstance Win32_Service -Filter "Name='kumiho'").ProcessId
      taskkill /F /PID $svcPid
      Get-NetRoute -InterfaceAlias foxy0 -AddressFamily IPv4 -DestinationPrefix 0.0.0.0/0
      kumiho cleanup
      Get-NetAdapter -Name foxy0 -ErrorAction SilentlyContinue
      sc.exe failure kumiho reset= 86400 actions= restart/5000/restart/30000/restart/60000
      Start-Service kumiho

  Expect: the killed run leaves `0.0.0.0/0` behind (the service stays down);
  `kumiho cleanup` prints `Stale kumiho routing, nftables and DNS state were
  removed (best effort).` and removes the route and the `foxy0` adapter;
  recovery is restored afterwards and the service starts again.

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

## 16. Results and criterion map

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

Sign-off (fill before tagging):

- Date:
- Machine / Windows build:
- Commit / tag SHA under test:
- Result: PASS / FAIL
- Notes (failed steps, screenshots, log excerpts):
