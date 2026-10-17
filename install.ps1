# Kumiho installer for Windows (PLAN.md section 10).
#
#   .\install.ps1                     install or upgrade (idempotent)
#   .\install.ps1 -Uninstall          remove; keep %ProgramData%\kumiho state
#   .\install.ps1 -Uninstall -Purge   also remove state, tokens and the group
#
# Installs the service, the CLI and the GUI. For double-click use, install.cmd
# is the self-elevating wrapper; it passes the invoking user with -User because
# under an over-the-shoulder UAC prompt the elevated process is the admin, not
# the person who started the installer.
#
# Options:
#   -Binary PATH   CLI binary to install (default: kumiho.exe beside this
#                  script when present, else dist\kumiho-windows-amd64.exe)
#   -User NAME     user to add to the kumiho group, grant on the daemon pipe
#                  and give the login entry (default: $env:USERNAME)

param(
    [switch]$Uninstall,
    [switch]$Purge,
    [string]$Binary,
    [string]$User = $env:USERNAME
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$serviceName = 'kumiho'
$groupName = 'kumiho'
$exeName = 'kumiho.exe'
$guiExeName = 'kumiho-gui.exe'
$installDir = Join-Path $env:ProgramFiles 'Kumiho'
$stateDir = Join-Path $env:ProgramData 'kumiho'
$logsDir = Join-Path $stateDir 'logs'
$configFile = Join-Path $stateDir 'config.toml'
$pipeUsersFile = Join-Path $stateDir 'pipe-users'
$startMenuDir = Join-Path $env:ProgramData 'Microsoft\Windows\Start Menu\Programs'
$shortcutName = 'Kumiho.lnk'
$runSubKey = 'Software\Microsoft\Windows\CurrentVersion\Run'
$runValueName = 'Kumiho GUI'
$runFlag = '--tray-start'

function Test-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    $admin = [Security.Principal.WindowsBuiltInRole]::Administrator
    return $principal.IsInRole($admin)
}

function Resolve-InstallBinary {
    $path = $Binary
    if ([string]::IsNullOrEmpty($path)) {
        $beside = Join-Path $PSScriptRoot $exeName
        if (Test-Path -LiteralPath $beside -PathType Leaf) {
            $path = $beside
        } else {
            $path = Join-Path $PSScriptRoot 'dist\kumiho-windows-amd64.exe'
        }
    }
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "missing binary: $path (download a release zip, run 'make build-windows', or pass -Binary PATH)"
    }
    return (Resolve-Path -LiteralPath $path).Path
}

function Resolve-GuiBinary {
    $beside = Join-Path $PSScriptRoot $guiExeName
    if (Test-Path -LiteralPath $beside -PathType Leaf) {
        return (Resolve-Path -LiteralPath $beside).Path
    }
    $path = Join-Path $PSScriptRoot 'dist\kumiho-gui-windows-amd64.exe'
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "missing GUI binary: $path (download a release zip or run 'make build-windows')"
    }
    return (Resolve-Path -LiteralPath $path).Path
}

function Resolve-WintunDll {
    foreach ($candidate in @(
            (Join-Path $PSScriptRoot 'wintun.dll'),
            (Join-Path $PSScriptRoot 'third_party\wintun\amd64\wintun.dll'))) {
        if (Test-Path -LiteralPath $candidate -PathType Leaf) {
            return (Resolve-Path -LiteralPath $candidate).Path
        }
    }
    throw 'missing wintun.dll: expected beside install.ps1 (release zip) or third_party\wintun\amd64\wintun.dll (checkout)'
}

# Copy-InstallFile places one file beside the installed exes. The installer
# copies itself and install.cmd into $installDir, so when it is run from that
# directory the source and destination resolve to the same file; copying a
# file onto itself is an error even with -Force, so that case is a no-op.
function Copy-InstallFile {
    param([string]$Source, [string]$Name)
    $destination = Join-Path $installDir $Name
    if ((Test-Path -LiteralPath $destination -PathType Leaf) -and
            ((Resolve-Path -LiteralPath $Source).Path -eq (Resolve-Path -LiteralPath $destination).Path)) {
        Write-Host "$Name is already the installed copy; leaving it in place"
        return
    }
    Copy-Item -LiteralPath $Source -Destination $destination -Force
    Write-Host "copied $Name to $installDir"
}

function Set-DirectoryAcl {
    param([string]$Path)
    & icacls.exe $Path /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "icacls failed for $Path (exit code $LASTEXITCODE)"
    }
}

function Get-MachinePathEntries {
    $value = [Environment]::GetEnvironmentVariable('Path', 'Machine')
    if ([string]::IsNullOrEmpty($value)) {
        return @()
    }
    return @($value -split ';' | Where-Object { $_ -ne '' })
}

function Set-MachinePathEntries {
    param([string[]]$Entries)
    [Environment]::SetEnvironmentVariable('Path', ($Entries -join ';'), 'Machine')
}

function Test-PathEntry {
    param([string[]]$Entries, [string]$Directory)
    $target = $Directory.TrimEnd([char]'\')
    foreach ($entry in $Entries) {
        if ($entry.TrimEnd([char]'\') -eq $target) {
            return $true
        }
    }
    return $false
}

function Add-InstallPathEntry {
    $entries = @(Get-MachinePathEntries)
    if (Test-PathEntry -Entries $entries -Directory $installDir) {
        Write-Host "$installDir is already on the machine PATH"
        return
    }
    Set-MachinePathEntries -Entries ($entries + $installDir)
    Write-Host "added $installDir to the machine PATH (new shells only)"
}

function Remove-InstallPathEntry {
    $entries = @(Get-MachinePathEntries)
    if (-not (Test-PathEntry -Entries $entries -Directory $installDir)) {
        Write-Host "$installDir is not on the machine PATH"
        return
    }
    $target = $installDir.TrimEnd([char]'\')
    $kept = @($entries | Where-Object { $_.TrimEnd([char]'\') -ne $target })
    Set-MachinePathEntries -Entries $kept
    Write-Host "removed $installDir from the machine PATH (new shells only)"
}

function Resolve-UserSid {
    param([string]$UserName)
    try {
        return ([Security.Principal.NTAccount]::new($UserName)).Translate([Security.Principal.SecurityIdentifier]).Value
    } catch {
        throw "user not found: $UserName ($($_.Exception.Message))"
    }
}

function Add-UserToGroup {
    param([string]$Group, [string]$UserName)
    $sid = Resolve-UserSid -UserName $UserName
    foreach ($member in @(Get-LocalGroupMember -Group $Group)) {
        if ($member.SID.Value -eq $sid) {
            Write-Host "user $UserName is already in group $Group"
            return
        }
    }
    Add-LocalGroupMember -Group $Group -Member $UserName
    Write-Host "added $UserName to group $Group (sign out and back in for it to take effect)"
}

# Add-PipeUser appends the user's SID to the daemon pipe allowlist that
# internal/ipc reads when it creates the control pipe (T5.1), so the GUI
# reaches the pipe without a sign-out. The file lives in the admin-only state
# directory and inherits its SYSTEM + Administrators ACL: it is
# admin-controlled input, and every line grants a principal full control of
# the control pipe, so it is written before the service (re)starts below. The
# daemon parser reads one canonical SID per line, tolerates a UTF-8 BOM and
# skips blanks, so the append only has to stay canonical and deduplicated.
function Add-PipeUser {
    param([string]$UserName)
    $sid = Resolve-UserSid -UserName $UserName
    $prefix = ''
    if (Test-Path -LiteralPath $pipeUsersFile -PathType Leaf) {
        $raw = [System.IO.File]::ReadAllText($pipeUsersFile)
        if ($raw.Length -gt 0 -and $raw[0] -eq [char]0xFEFF) {
            $raw = $raw.Substring(1)
        }
        foreach ($line in @($raw -split "`r?`n")) {
            if ($line.Trim() -eq $sid) {
                Write-Host "user $UserName ($sid) is already granted on the daemon pipe"
                return
            }
        }
        if ($raw -ne '' -and -not $raw.EndsWith("`n")) {
            $prefix = [Environment]::NewLine
        }
    }
    Add-Content -LiteralPath $pipeUsersFile -Value ($prefix + $sid)
    Write-Host "granted user $UserName ($sid) on the daemon pipe (no sign-out needed; $pipeUsersFile is admin-only)"
}

function Remove-PipeUser {
    param([string]$UserName)
    if (-not (Test-Path -LiteralPath $pipeUsersFile -PathType Leaf)) {
        Write-Host "$pipeUsersFile is not present"
        return
    }
    try {
        $sid = Resolve-UserSid -UserName $UserName
    } catch {
        Write-Host "user not found: $UserName; leaving $pipeUsersFile untouched"
        return
    }
    $raw = [System.IO.File]::ReadAllText($pipeUsersFile)
    if ($raw.Length -gt 0 -and $raw[0] -eq [char]0xFEFF) {
        $raw = $raw.Substring(1)
    }
    $lines = @($raw -split "`r?`n" | ForEach-Object { $_.Trim() } | Where-Object { $_ -ne '' })
    $kept = @($lines | Where-Object { $_ -ne $sid })
    if ($kept.Count -eq $lines.Count) {
        Write-Host "user $UserName is not granted on the daemon pipe"
        return
    }
    if ($kept.Count -eq 0) {
        Remove-Item -LiteralPath $pipeUsersFile -Force
        Write-Host "removed $pipeUsersFile (its last grant)"
        return
    }
    Set-Content -LiteralPath $pipeUsersFile -Value $kept
    Write-Host "revoked the daemon pipe grant for $UserName"
}

function Add-StartMenuShortcut {
    param([string]$TargetPath)
    New-Item -ItemType Directory -Force -Path $startMenuDir | Out-Null
    $shortcutPath = Join-Path $startMenuDir $shortcutName
    $shell = New-Object -ComObject WScript.Shell
    $shortcut = $shell.CreateShortcut($shortcutPath)
    $shortcut.TargetPath = $TargetPath
    $shortcut.WorkingDirectory = $installDir
    $shortcut.IconLocation = "$TargetPath,0"
    $shortcut.Description = 'Kumiho VPN'
    $shortcut.Save()
    Write-Host "created Start Menu shortcut $shortcutPath"
}

function Remove-StartMenuShortcut {
    $shortcutPath = Join-Path $startMenuDir $shortcutName
    if (Test-Path -LiteralPath $shortcutPath -PathType Leaf) {
        Remove-Item -LiteralPath $shortcutPath -Force
        Write-Host "removed $shortcutPath"
    } else {
        Write-Host "$shortcutPath is not present"
    }
}

# Get-RunCommand renders the login entry's data: the quoted GUI path plus the
# flag that starts it hidden in the tray. It must match what the GUI writes
# (internal/gui autostartCommand); the uninstaller only removes a value whose
# data equals it.
function Get-RunCommand {
    return '"{0}" {1}' -f (Join-Path $installDir $guiExeName), $runFlag
}

# Invoke-InUserProfile runs $Action against a registry root inside the
# installing user's own hive. The elevated process is not necessarily that
# user (install.cmd elevates through UAC, which can sign in an administrator
# instead), so HKCU is not reliable: a signed-in user's hive is already
# mounted under HKEY_USERS, and a signed-out user's NTUSER.DAT is loaded for
# the duration of the action. Returns $true when the action ran; otherwise a
# cause is reported and the caller prints the manual fallback.
function Invoke-InUserProfile {
    param([string]$UserName, [scriptblock]$Action)
    try {
        $sid = Resolve-UserSid -UserName $UserName
    } catch {
        Write-Host "user not found: $UserName; leaving any '$runValueName' login entry alone"
        return $false
    }
    $mounted = "Registry::HKEY_USERS\$sid"
    if (Test-Path -LiteralPath $mounted) {
        try {
            & $Action $mounted
            return $true
        } catch {
            Write-Host "could not change $UserName's login entry: $($_.Exception.Message)"
            return $false
        }
    }
    $profilePath = Get-ItemPropertyValue -Path "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList\$sid" -Name ProfileImagePath -ErrorAction SilentlyContinue
    if ([string]::IsNullOrEmpty($profilePath)) {
        Write-Host "no profile found for $UserName ($sid)"
        return $false
    }
    $ntUser = Join-Path ([Environment]::ExpandEnvironmentVariables($profilePath)) 'NTUSER.DAT'
    if (-not (Test-Path -LiteralPath $ntUser -PathType Leaf)) {
        Write-Host "profile hive not found for $UserName at $ntUser"
        return $false
    }
    $hive = 'KumihoInstaller' + [Guid]::NewGuid().ToString('N')
    & reg.exe load "HKU\$hive" $ntUser 2>$null | Out-Null
    if ($LASTEXITCODE -ne 0) {
        Write-Host "could not open the profile of $UserName (is that user signed in?)"
        return $false
    }
    try {
        & $Action "Registry::HKEY_USERS\$hive"
        return $true
    } catch {
        Write-Host "could not change $UserName's login entry: $($_.Exception.Message)"
        return $false
    } finally {
        & reg.exe unload "HKU\$hive" | Out-Null
        if ($LASTEXITCODE -ne 0) {
            Write-Host "warning: could not unload HKU\$hive; it disappears at the next reboot"
        }
    }
}

# Add-RunEntry gives the installing user the per-user login entry (D11),
# default on: "<gui exe>" --tray-start, so a sign-in starts the GUI hidden in
# the tray. Only the installing user's hive is touched; the GUI re-syncs the
# entry from its own preference at every launch (its Settings toggle turns it
# off), so an over-the-shoulder install that cannot reach the hive still ends
# up correct after the user's first launch.
function Add-RunEntry {
    param([string]$UserName)
    if (Invoke-InUserProfile -UserName $UserName -Action {
            param([string]$Root)
            $keyPath = "$Root\$runSubKey"
            $command = Get-RunCommand
            $existing = Get-ItemPropertyValue -LiteralPath $keyPath -Name $runValueName -ErrorAction SilentlyContinue
            if ($null -ne $existing) {
                if ($existing -eq $command) {
                    Write-Host "the '$runValueName' login entry is already set"
                    return
                }
                Set-ItemProperty -LiteralPath $keyPath -Name $runValueName -Value $command -Type String
                Write-Host "updated the '$runValueName' login entry"
                return
            }
            New-Item -Path $keyPath -Force | Out-Null
            Set-ItemProperty -LiteralPath $keyPath -Name $runValueName -Value $command -Type String
            Write-Host "added the '$runValueName' login entry (starts hidden in the tray)"
        }) {
        return
    }
    Write-Host "the GUI adds the '$runValueName' login entry itself at $UserName's next launch"
}

# Remove-RunEntry removes the login entry only when its data is exactly the
# command this installer writes; anything else under the value name is left
# alone (it may be another tool's entry, or a hand-edited one).
function Remove-RunEntry {
    param([string]$UserName)
    if (Invoke-InUserProfile -UserName $UserName -Action {
            param([string]$Root)
            $keyPath = "$Root\$runSubKey"
            $existing = Get-ItemPropertyValue -LiteralPath $keyPath -Name $runValueName -ErrorAction SilentlyContinue
            if ($null -eq $existing) {
                Write-Host "the '$runValueName' login entry is not present"
                return
            }
            if ($existing -ne (Get-RunCommand)) {
                Write-Host "the '$runValueName' login entry does not start the installed GUI; leaving it alone"
                return
            }
            Remove-ItemProperty -LiteralPath $keyPath -Name $runValueName -Force
            Write-Host "removed the '$runValueName' login entry"
        }) {
        return
    }
    Write-Host "turn off 'Start Kumiho at login' in the GUI, or remove '$runValueName' from HKCU\Software\Microsoft\Windows\CurrentVersion\Run manually"
}

function Write-DefaultConfig {
    if (Test-Path -LiteralPath $configFile) {
        Write-Host "$configFile exists; leaving it untouched"
        return
    }
    $template = @'
# Kumiho admin defaults (PLAN.md section 8). Every key is optional; runtime
# settings changed from the TUI are persisted as overrides in
# %ProgramData%\kumiho\state.json and take precedence. Examples:
#
#   socks_port    = 1080
#   kill_switch   = "last"        # "on", "off" or "last" (remember)
#   autoconnect   = "last"
#   doh_provider  = "automatic"   # cloudflare | google | quad9 | off
#   dns_upstream  = ""            # resolver IP for DoH; "" = provider default
#   mtu           = 8500
#   allow_lan     = true
#   log_level     = "info"
'@
    [System.IO.File]::WriteAllText($configFile, $template)
    Write-Host "wrote default $configFile"
}

function Install-Kumiho {
    Write-Host '==> group'
    if (Get-LocalGroup -Name $groupName -ErrorAction SilentlyContinue) {
        Write-Host "local group $groupName already exists; reusing it"
    } else {
        New-LocalGroup -Name $groupName -Description 'Kumiho VPN users' | Out-Null
        Write-Host "created local group $groupName"
    }

    Write-Host '==> state'
    New-Item -ItemType Directory -Force -Path $stateDir | Out-Null
    New-Item -ItemType Directory -Force -Path $logsDir | Out-Null
    Set-DirectoryAcl -Path $stateDir
    Set-DirectoryAcl -Path $logsDir
    Write-Host "created $stateDir with SYSTEM and Administrators full control"

    Write-Host '==> binary'
    $sourceBinary = Resolve-InstallBinary
    $sourceGui = Resolve-GuiBinary
    $sourceWintun = Resolve-WintunDll
    $service = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
    if ($null -ne $service -and $service.Status -ne [System.ServiceProcess.ServiceControllerStatus]::Stopped) {
        Stop-Service -Name $serviceName -Force
        Write-Host "stopped service $serviceName to replace the binary"
    }
    New-Item -ItemType Directory -Force -Path $installDir | Out-Null
    Copy-InstallFile -Source $sourceBinary -Name $exeName
    Copy-InstallFile -Source $sourceGui -Name $guiExeName
    Copy-InstallFile -Source $sourceWintun -Name 'wintun.dll'

    Write-Host '==> installer copies'
    foreach ($name in @('install.cmd', 'install.ps1')) {
        $source = Join-Path $PSScriptRoot $name
        if (Test-Path -LiteralPath $source -PathType Leaf) {
            Copy-InstallFile -Source $source -Name $name
        } else {
            Write-Host "warning: $name is not beside this script; not copied (re-run it from the download to upgrade or uninstall)"
        }
    }

    Write-Host '==> shortcut'
    Add-StartMenuShortcut -TargetPath (Join-Path $installDir $guiExeName)

    Write-Host '==> PATH'
    Add-InstallPathEntry

    Write-Host '==> config'
    Write-DefaultConfig

    Write-Host '==> service'
    $exePath = Join-Path $installDir $exeName
    $binaryPathName = '"{0}" daemon' -f $exePath
    if ($null -eq $service) {
        New-Service -Name $serviceName -DisplayName 'Kumiho' -StartupType Automatic -BinaryPathName $binaryPathName | Out-Null
        Write-Host "created service $serviceName (auto-start, LocalSystem)"
    } else {
        Write-Host "service $serviceName already exists; keeping its registration"
    }
    & sc.exe failure $serviceName 'reset=' 86400 'actions=' 'restart/5000/restart/30000/restart/60000' | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "sc.exe failure failed for $serviceName (exit code $LASTEXITCODE)"
    }
    Write-Host 'configured crash recovery (restart after 5s, 30s, 60s)'

    Write-Host '==> group membership'
    Add-UserToGroup -Group $groupName -UserName $User

    Write-Host '==> pipe access'
    Add-PipeUser -UserName $User

    Write-Host '==> login entry'
    Add-RunEntry -UserName $User

    Write-Host '==> start'
    $service = Get-Service -Name $serviceName
    if ($service.Status -eq [System.ServiceProcess.ServiceControllerStatus]::Running) {
        Restart-Service -Name $serviceName -Force
        Write-Host "restarted service $serviceName with the new binary"
    } else {
        Start-Service -Name $serviceName
        Write-Host "started service $serviceName"
    }

    Write-Host ''
    Write-Host 'installed; service kumiho is running'
    Write-Host 'next steps:'
    Write-Host '  open Kumiho from the Start Menu'
    Write-Host "  $User is granted on the daemon pipe; no sign-out is needed for the GUI"
    Write-Host '  the kumiho group still applies to other users and other group uses'
    Write-Host 'in a new shell (PATH updated):'
    Write-Host "  kumiho login    # as $User"
    Write-Host '  kumiho doctor'
}

function Uninstall-Kumiho {
    Write-Host '==> service'
    $service = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
    if ($null -eq $service) {
        Write-Host "service $serviceName is not installed"
    } else {
        if ($service.Status -ne [System.ServiceProcess.ServiceControllerStatus]::Stopped) {
            Stop-Service -Name $serviceName -Force
            Write-Host "stopped service $serviceName"
        }
        & sc.exe delete $serviceName | Out-Null
        if ($LASTEXITCODE -ne 0) {
            throw "failed to delete service $serviceName (sc.exe exit code $LASTEXITCODE)"
        }
        Write-Host "deleted service $serviceName"
    }

    Write-Host '==> PATH'
    Remove-InstallPathEntry

    Write-Host '==> installer copies'
    foreach ($name in @('install.cmd', 'install.ps1')) {
        $copy = Join-Path $installDir $name
        if (Test-Path -LiteralPath $copy -PathType Leaf) {
            Remove-Item -LiteralPath $copy -Force
            Write-Host "removed $copy"
        } else {
            Write-Host "$copy is not present"
        }
    }

    Write-Host '==> binary'
    if (Test-Path -LiteralPath $installDir) {
        Remove-Item -LiteralPath $installDir -Recurse -Force
        Write-Host "removed $installDir (kumiho.exe, kumiho-gui.exe, wintun.dll and installer copies)"
    } else {
        Write-Host "$installDir is not present"
    }

    Write-Host '==> shortcut'
    Remove-StartMenuShortcut

    Write-Host '==> login entry'
    Remove-RunEntry -UserName $User

    Write-Host '==> pipe access'
    Remove-PipeUser -UserName $User

    if ($Purge) {
        Write-Host '==> purge: state and group'
        if (Test-Path -LiteralPath $stateDir) {
            Remove-Item -LiteralPath $stateDir -Recurse -Force
            Write-Host "removed $stateDir"
        } else {
            Write-Host "$stateDir is not present"
        }
        if (Get-LocalGroup -Name $groupName -ErrorAction SilentlyContinue) {
            Remove-LocalGroup -Name $groupName
            Write-Host "removed local group $groupName"
        } else {
            Write-Host "local group $groupName is not present"
        }
    } else {
        Write-Host "kept $stateDir (state and tokens); use -Purge to remove it"
    }
    Write-Host 'uninstalled'
}

if (-not (Test-Administrator)) {
    throw 'must run as administrator (start PowerShell with "Run as administrator")'
}
if ($Purge -and -not $Uninstall) {
    throw '-Purge requires -Uninstall'
}
if (-not $Uninstall -and [string]::IsNullOrEmpty($User)) {
    throw '-User is required (could not determine the invoking user)'
}

if ($Uninstall) {
    Uninstall-Kumiho
} else {
    Install-Kumiho
}
