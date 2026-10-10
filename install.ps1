# Kumiho installer for Windows (PLAN.md section 10).
#
#   .\install.ps1                     install or upgrade (idempotent)
#   .\install.ps1 -Uninstall          remove; keep %ProgramData%\kumiho state
#   .\install.ps1 -Uninstall -Purge   also remove state, tokens and the group
#
# Options:
#   -Binary PATH   binary to install (default: kumiho.exe beside this script
#                  when present, else dist\kumiho-windows-amd64.exe)
#   -User NAME     user to add to the kumiho group (default: $env:USERNAME)

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
$installDir = Join-Path $env:ProgramFiles 'Kumiho'
$stateDir = Join-Path $env:ProgramData 'kumiho'
$logsDir = Join-Path $stateDir 'logs'
$configFile = Join-Path $stateDir 'config.toml'

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

function Add-UserToGroup {
    param([string]$Group, [string]$UserName)
    try {
        $sid = ([Security.Principal.NTAccount]::new($UserName)).Translate([Security.Principal.SecurityIdentifier]).Value
    } catch {
        throw "user not found: $UserName ($($_.Exception.Message))"
    }
    foreach ($member in @(Get-LocalGroupMember -Group $Group)) {
        if ($member.SID.Value -eq $sid) {
            Write-Host "user $UserName is already in group $Group"
            return
        }
    }
    Add-LocalGroupMember -Group $Group -Member $UserName
    Write-Host "added $UserName to group $Group (sign out and back in for it to take effect)"
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
    $sourceWintun = Resolve-WintunDll
    $service = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
    if ($null -ne $service -and $service.Status -ne [System.ServiceProcess.ServiceControllerStatus]::Stopped) {
        Stop-Service -Name $serviceName -Force
        Write-Host "stopped service $serviceName to replace the binary"
    }
    New-Item -ItemType Directory -Force -Path $installDir | Out-Null
    Copy-Item -LiteralPath $sourceBinary -Destination (Join-Path $installDir $exeName) -Force
    Copy-Item -LiteralPath $sourceWintun -Destination (Join-Path $installDir 'wintun.dll') -Force
    Write-Host "copied $(Split-Path -Leaf $sourceBinary) and wintun.dll to $installDir"

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
    Write-Host 'next steps (in a new shell, PATH updated):'
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

    Write-Host '==> binary'
    if (Test-Path -LiteralPath $installDir) {
        Remove-Item -LiteralPath $installDir -Recurse -Force
        Write-Host "removed $installDir"
    } else {
        Write-Host "$installDir is not present"
    }

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
