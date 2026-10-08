# Dot-source this file to point kumiho at a local dev directory:
#   . .\scripts\dev-env.ps1
# Then run (each in a terminal with the env set):
#   .\dist\kumiho-windows-amd64.exe daemon    # terminal 1, leave running
#   .\dist\kumiho-windows-amd64.exe login     # terminal 2
#   .\dist\kumiho-windows-amd64.exe status
#   .\dist\kumiho-windows-amd64.exe           # TUI
#
# This avoids the production paths (/etc/kumiho, /var/lib/kumiho, /run/kumiho)
# until install.sh + systemd units land in milestone M5.

$dir = Join-Path $env:TEMP 'kumiho-dev'
New-Item -ItemType Directory -Force -Path $dir | Out-Null
$env:KUMIHO_CONFIG = Join-Path $dir 'config.toml'
$env:KUMIHO_STATE = Join-Path $dir 'state.json'
$env:KUMIHO_TOKENS = Join-Path $dir 'tokens.json'
$env:KUMIHO_RUNTIME_DIR = $dir
Write-Host "kumiho dev paths set under $dir"
