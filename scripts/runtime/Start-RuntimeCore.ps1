# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: Apache-2.0
[CmdletBinding()]
param(
    [ValidateSet('stdio', 'http')][string]$Transport = 'stdio',
    [ValidateRange(1024,65535)][int]$Port = 8766
)
$ErrorActionPreference = 'Stop'
$installRoot = Split-Path $PSScriptRoot -Parent
$settingsFile = Join-Path $installRoot 'install.json'
if (-not (Test-Path -LiteralPath $settingsFile)) { throw 'Run Install-RuntimeCore.ps1 first; this launcher is used from the installed scripts directory.' }
$settings = Get-Content -LiteralPath $settingsFile -Raw | ConvertFrom-Json
$env:AGENTDOCK_HOME = $settings.runtime_home
$env:AGENTDOCK_DEFAULT_DIR = $settings.project_root
$env:AGENTDOCK_ACP_ENABLED = 'false'
$env:AGENTDOCK_BROWSER_ENABLED = 'true'
$env:AGENTDOCK_HOST = '127.0.0.1'
$env:AGENTDOCK_PORT = $Port.ToString()
if ($Transport -eq 'stdio') {
    & $settings.binary --stdio
} else {
    $env:AGENTDOCK_STDIO = 'false'
    & $settings.binary
}
exit $LASTEXITCODE
