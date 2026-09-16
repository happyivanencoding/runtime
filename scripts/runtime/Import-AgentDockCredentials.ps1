# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param(
    [string]$AgentDockRoot = (Join-Path $env:LOCALAPPDATA 'AgentDock'),
    [string]$RuntimeInstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore')
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Security
$AgentDockRoot = [IO.Path]::GetFullPath($AgentDockRoot)
$RuntimeInstallDir = [IO.Path]::GetFullPath($RuntimeInstallDir)
$secretsDir = Join-Path $RuntimeInstallDir 'secrets'
New-Item -ItemType Directory -Force $secretsDir | Out-Null

function Read-DpapiText {
    param([string]$Path, [string]$Entropy)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { return '' }
    $encoded = [IO.File]::ReadAllText($Path, [Text.Encoding]::UTF8).Trim()
    if ([string]::IsNullOrWhiteSpace($encoded)) { return '' }
    $plain = [Security.Cryptography.ProtectedData]::Unprotect(
        [Convert]::FromBase64String($encoded),
        [Text.Encoding]::UTF8.GetBytes($Entropy),
        [Security.Cryptography.DataProtectionScope]::CurrentUser)
    try { return [Text.Encoding]::UTF8.GetString($plain) }
    finally { [Array]::Clear($plain, 0, $plain.Length) }
}

function Write-DpapiText {
    param([string]$Path, [string]$Value, [string]$Entropy)
    $plain = [Text.Encoding]::UTF8.GetBytes($Value)
    try {
        $protected = [Security.Cryptography.ProtectedData]::Protect(
            $plain,
            [Text.Encoding]::UTF8.GetBytes($Entropy),
            [Security.Cryptography.DataProtectionScope]::CurrentUser)
        $temp = $Path + '.tmp.' + [Guid]::NewGuid().ToString('N')
        try {
            [IO.File]::WriteAllText($temp, [Convert]::ToBase64String($protected), [Text.UTF8Encoding]::new($false))
            Move-Item -LiteralPath $temp -Destination $Path -Force
        }
        finally { Remove-Item -LiteralPath $temp -Force -ErrorAction SilentlyContinue }
    }
    finally { [Array]::Clear($plain, 0, $plain.Length) }
}

$items = @(
    @{ Source='auth-token.dpapi'; SourceEntropy='agentdock.startup.v1'; Target='auth-token.dpapi'; TargetEntropy='runtime.auth.bearer.v1'; Kind='bearer' },
    @{ Source='oauth-password.dpapi'; SourceEntropy='agentdock.oauth.password.v1'; Target='oauth-password.dpapi'; TargetEntropy='runtime.oauth.password.v1'; Kind='oauth_password' },
    @{ Source='oauth-token-secret.dpapi'; SourceEntropy='agentdock.oauth.secret.v1'; Target='oauth-token-secret.dpapi'; TargetEntropy='runtime.oauth.secret.v1'; Kind='oauth_signing_secret' },
    @{ Source='cloudflared-token.dpapi'; SourceEntropy='agentdock.cloudflare.tunnel.v1'; Target='imported-agentdock-cloudflared-token.dpapi'; TargetEntropy='runtime.import.agentdock.cloudflare.tunnel.v1'; Kind='agentdock_tunnel_reference' }
)

$result = [ordered]@{}
foreach ($item in $items) {
    $source = Join-Path $AgentDockRoot $item.Source
    $target = Join-Path $secretsDir $item.Target
    try {
        $value = Read-DpapiText -Path $source -Entropy $item.SourceEntropy
        if ([string]::IsNullOrWhiteSpace($value)) {
            $result[$item.Kind] = 'missing'
            continue
        }
        Write-DpapiText -Path $target -Value $value -Entropy $item.TargetEntropy
        $result[$item.Kind] = 'imported'
    }
    catch {
        $result[$item.Kind] = 'failed'
    }
    finally { $value = $null }
}

$metadata = [ordered]@{
    schema_version = 1
    source = 'AgentDock local DPAPI store'
    imported_at = [DateTimeOffset]::Now.ToString('o')
    bearer = $result.bearer
    oauth_password = $result.oauth_password
    oauth_signing_secret = $result.oauth_signing_secret
    agentdock_tunnel_reference = $result.agentdock_tunnel_reference
    active_runtime_tunnel_token = (Test-Path -LiteralPath (Join-Path $secretsDir 'cloudflared-token.dpapi'))
    note = 'The imported AgentDock tunnel token is reference-only because a Cloudflare tunnel token is tunnel-specific. Runtime must use its own independent tunnel token.'
}
[IO.File]::WriteAllText((Join-Path $secretsDir 'credentials.json'), ($metadata | ConvertTo-Json -Depth 4), [Text.UTF8Encoding]::new($false))

# Deliberately return status only. Secret values are never written to stdout.
$metadata | ConvertTo-Json -Depth 4
