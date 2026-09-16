# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param([string]$RuntimeInstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore'))

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Security
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
    try {
        $p = Get-Process -Id $pidValue -ErrorAction Stop
        if ([string]::IsNullOrWhiteSpace($p.Path)) { return $null }
        return $p.Path -ieq $ExpectedExe
    } catch { return $false }
}

function Read-RuntimeSecret([string]$Name, [string]$Entropy) {
    $path = Join-Path $secretsDir $Name
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { return $null }
    try {
        $encoded = [IO.File]::ReadAllText($path, [Text.Encoding]::UTF8).Trim()
        $plain = [Security.Cryptography.ProtectedData]::Unprotect(
            [Convert]::FromBase64String($encoded),
            [Text.Encoding]::UTF8.GetBytes($Entropy),
            [Security.Cryptography.DataProtectionScope]::CurrentUser)
        try { return [Text.Encoding]::UTF8.GetString($plain) }
        finally { [Array]::Clear($plain, 0, $plain.Length) }
    } catch { return $null }
}

function Test-AuthenticatedContext([string]$Origin, [string]$Token, [int]$TimeoutSec = 4) {
    if ([string]::IsNullOrWhiteSpace($Origin) -or [string]::IsNullOrWhiteSpace($Token)) { return $false }
    try {
        $response = Invoke-WebRequest -Uri ($Origin.TrimEnd('/') + '/context') -Headers @{ Authorization = ('Bearer ' + $Token) } -UseBasicParsing -TimeoutSec $TimeoutSec -MaximumRedirection 0
        if ([int]$response.StatusCode -ne 200) { return $false }
        $payload = $response.Content | ConvertFrom-Json
        return $null -ne $payload.runtime -and -not [string]::IsNullOrWhiteSpace([string]$payload.runtime.version)
    } catch { return $false }
}

$cloudflaredBinary = [string]$config.cloudflared_binary
if ([string]::IsNullOrWhiteSpace($cloudflaredBinary)) { $cloudflaredBinary = Join-Path $RuntimeInstallDir 'bin\cloudflared.exe' }
$runtimeUp = Test-OwnedPid (Join-Path $stateDir 'runtime-http.pid') ([string]$install.binary)
$cloudflareUp = Test-OwnedPid (Join-Path $stateDir 'cloudflared.pid') $cloudflaredBinary
$authReady = (Test-Path (Join-Path $secretsDir 'auth-token.dpapi')) -and (Test-Path (Join-Path $secretsDir 'oauth-password.dpapi')) -and (Test-Path (Join-Path $secretsDir 'oauth-token-secret.dpapi'))
$tunnelTokenReady = Test-Path (Join-Path $secretsDir 'cloudflared-token.dpapi')
$referenceTokenPresent = Test-Path (Join-Path $secretsDir 'imported-agentdock-cloudflared-token.dpapi')
$token = Read-RuntimeSecret 'auth-token.dpapi' 'runtime.auth.bearer.v1'
try {
    $localReady = if($runtimeUp -eq $true) { Test-AuthenticatedContext ([string]$config.local_origin) $token } elseif($runtimeUp -eq $null) { $null } else { $false }
    $publicReady = if($runtimeUp -eq $true -and $cloudflareUp -eq $true -and $localReady -eq $true) { Test-AuthenticatedContext ([string]$config.public_origin) $token } elseif($runtimeUp -eq $null -or $cloudflareUp -eq $null) { $null } else { $false }
} finally {
    Remove-Variable token -ErrorAction SilentlyContinue
}

$liveStatus = if($runtimeUp -eq $false -and $cloudflareUp -eq $false) {
    'stopped'
} elseif($runtimeUp -eq $null -or $cloudflareUp -eq $null) {
    'unknown'
} elseif($runtimeUp -eq $true -and $localReady -eq $true -and $cloudflareUp -eq $true -and $publicReady -eq $true) {
    'connected'
} elseif($runtimeUp -eq $true -and $localReady -eq $true) {
    'degraded'
} else {
    'error'
}
$checkedAt = [DateTimeOffset]::Now.ToString('o')
$lastRecoveryAt = $null
if($null -ne $config.PSObject.Properties['last_recovery_at']) {
    $rawRecoveryAt = [string]$config.last_recovery_at
    $parsedRecoveryAt = [DateTimeOffset]::MinValue
    $lastRecoveryAt = if([DateTimeOffset]::TryParse($rawRecoveryAt, [ref]$parsedRecoveryAt)){$parsedRecoveryAt.ToString('o')}else{$rawRecoveryAt}
}

[ordered]@{
    status = $liveStatus
    recorded_status = [string]$config.status
    public_mcp_url = [string]$config.public_mcp_url
    local_mcp_url = [string]$config.local_mcp_url
    cloudflare_tunnel_name = [string]$config.cloudflare_tunnel_name
    cloudflare_tunnel_id = [string]$config.cloudflare_tunnel_id
    runtime_http_running = $runtimeUp
    cloudflare_running = $cloudflareUp
    local_context_ready = $localReady
    public_context_ready = $publicReady
    live_checked_at = $checkedAt
    last_recovery_at = $lastRecoveryAt
    last_recovery_reason = $(if($null -ne $config.PSObject.Properties['last_recovery_reason']){[string]$config.last_recovery_reason}else{$null})
    auth_credentials_ready = $authReady
    runtime_tunnel_token_ready = $tunnelTokenReady
    agentdock_tunnel_reference_present = $referenceTokenPresent
    execution_privilege = $(if($config.execution_privilege -eq 'administrator'){'administrator'}else{'standard'})
    openai_secure_mcp_tunnel = 'disabled_by_architecture'
    acp = 'dormant'
} | ConvertTo-Json
