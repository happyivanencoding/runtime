# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param([string]$RuntimeInstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore'))

$ErrorActionPreference = 'Stop'
$RuntimeInstallDir = [IO.Path]::GetFullPath($RuntimeInstallDir)
$configPath = Join-Path $RuntimeInstallDir 'public-connection.json'
$stateDir = Join-Path $RuntimeInstallDir 'public-edge'
$secretsDir = Join-Path $RuntimeInstallDir 'secrets'
$config = Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json
$install = Get-Content -LiteralPath (Join-Path $RuntimeInstallDir 'install.json') -Raw | ConvertFrom-Json

function Test-OwnedPid([string]$PidFile, [string]$ExpectedExe) {
    if (-not (Test-Path -LiteralPath $PidFile)) { return $false }
    $raw = [IO.File]::ReadAllText($PidFile).Trim()
    $pidValue = 0
    if (-not [int]::TryParse($raw, [ref]$pidValue)) { return $false }
    try { $p = Get-Process -Id $pidValue -ErrorAction Stop; return $p.Path -ieq $ExpectedExe } catch { return $false }
}

$cloudflaredBinary = [string]$config.cloudflared_binary
if ([string]::IsNullOrWhiteSpace($cloudflaredBinary)) { $cloudflaredBinary = Join-Path $RuntimeInstallDir 'bin\cloudflared.exe' }
$runtimeUp = Test-OwnedPid (Join-Path $stateDir 'runtime-http.pid') ([string]$install.binary)
$cloudflareUp = Test-OwnedPid (Join-Path $stateDir 'cloudflared.pid') $cloudflaredBinary
$authReady = (Test-Path (Join-Path $secretsDir 'auth-token.dpapi')) -and (Test-Path (Join-Path $secretsDir 'oauth-password.dpapi')) -and (Test-Path (Join-Path $secretsDir 'oauth-token-secret.dpapi'))
$tunnelTokenReady = Test-Path (Join-Path $secretsDir 'cloudflared-token.dpapi')
$referenceTokenPresent = Test-Path (Join-Path $secretsDir 'imported-agentdock-cloudflared-token.dpapi')

[ordered]@{
    status = [string]$config.status
    public_mcp_url = [string]$config.public_mcp_url
    local_mcp_url = [string]$config.local_mcp_url
    cloudflare_tunnel_name = [string]$config.cloudflare_tunnel_name
    cloudflare_tunnel_id = [string]$config.cloudflare_tunnel_id
    runtime_http_running = $runtimeUp
    cloudflare_running = $cloudflareUp
    auth_credentials_ready = $authReady
    runtime_tunnel_token_ready = $tunnelTokenReady
    agentdock_tunnel_reference_present = $referenceTokenPresent
    openai_secure_mcp_tunnel = 'disabled_by_architecture'
    acp = 'dormant'
} | ConvertTo-Json
