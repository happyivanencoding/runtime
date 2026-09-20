# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: Apache-2.0
[CmdletBinding()]
param(
    [ValidateSet('stdio', 'http')][string]$Transport = 'stdio',
    [ValidateRange(1024,65535)][int]$Port = 8766
)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Security
$installRoot = Split-Path $PSScriptRoot -Parent
$settingsFile = Join-Path $installRoot 'install.json'
if (-not (Test-Path -LiteralPath $settingsFile)) { throw 'Run Install-RuntimeCore.ps1 first; this launcher is used from the installed scripts directory.' }
$settings = Get-Content -LiteralPath $settingsFile -Raw | ConvertFrom-Json
function Read-RuntimeSecret([string]$Name, [string]$Entropy) {
    $path = Join-Path (Join-Path $installRoot 'secrets') $Name
    $encoded = [IO.File]::ReadAllText($path, [Text.Encoding]::UTF8).Trim()
    $plain = [Security.Cryptography.ProtectedData]::Unprotect(
        [Convert]::FromBase64String($encoded),
        [Text.Encoding]::UTF8.GetBytes($Entropy),
        [Security.Cryptography.DataProtectionScope]::CurrentUser)
    try { return [Text.Encoding]::UTF8.GetString($plain) }
    finally { [Array]::Clear($plain, 0, $plain.Length) }
}
$env:AGENTDOCK_HOME = $settings.runtime_home
$env:AGENTDOCK_DEFAULT_DIR = $settings.project_root
$env:AGENTDOCK_ACP_ENABLED = 'false'
$env:AGENTDOCK_BROWSER_ENABLED = 'true'
$env:AGENTDOCK_HOST = '127.0.0.1'
$env:AGENTDOCK_PORT = $Port.ToString()
$typesafeSecret = Join-Path (Join-Path $installRoot 'secrets') 'typesafe-api-key.dpapi'
if (Test-Path -LiteralPath $typesafeSecret -PathType Leaf) {
    $env:TYPESAFE_API_KEY = Read-RuntimeSecret 'typesafe-api-key.dpapi' 'runtime.typesafe.api.v1'
}
try {
    if ($Transport -eq 'stdio') {
        & $settings.binary --stdio
    } else {
        $env:AGENTDOCK_STDIO = 'false'
        & $settings.binary
    }
    $exitCode = $LASTEXITCODE
} finally {
    Remove-Item Env:TYPESAFE_API_KEY -ErrorAction SilentlyContinue
}
exit $exitCode
