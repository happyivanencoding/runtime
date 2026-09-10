# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param([string]$RuntimeInstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore'))

$ErrorActionPreference = 'Stop'
$RuntimeInstallDir = [IO.Path]::GetFullPath($RuntimeInstallDir)
$configPath = Join-Path $RuntimeInstallDir 'public-connection.json'
$secretsDir = Join-Path $RuntimeInstallDir 'secrets'
$scriptsDir = Join-Path $RuntimeInstallDir 'scripts'
$stateDir = Join-Path $RuntimeInstallDir 'public-edge'
$logsDir = Join-Path $RuntimeInstallDir 'logs'
New-Item -ItemType Directory -Force $stateDir, $logsDir | Out-Null
if (-not (Test-Path -LiteralPath $configPath)) { throw 'Runtime public connection is not initialized.' }
foreach($name in 'auth-token.dpapi','oauth-password.dpapi','oauth-token-secret.dpapi','cloudflared-token.dpapi') {
    if (-not (Test-Path -LiteralPath (Join-Path $secretsDir $name))) { throw "Runtime public edge is not ready: missing $name" }
}
foreach($name in 'Run-RuntimePublic.ps1','Run-RuntimeCloudflared.ps1') {
    if (-not (Test-Path -LiteralPath (Join-Path $scriptsDir $name))) { throw "Runtime public helper is missing: $name" }
}

function Test-OwnedPid([string]$PidFile, [string]$ExpectedExe) {
    if (-not (Test-Path -LiteralPath $PidFile)) { return $false }
    $raw = [IO.File]::ReadAllText($PidFile).Trim()
    $pidValue = 0
    if (-not [int]::TryParse($raw, [ref]$pidValue)) { return $false }
    try {
        $p = Get-Process -Id $pidValue -ErrorAction Stop
        return $p.Path -ieq $ExpectedExe
    } catch { return $false }
}

$config = Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json
$install = Get-Content -LiteralPath (Join-Path $RuntimeInstallDir 'install.json') -Raw | ConvertFrom-Json
$runtimePidFile = Join-Path $stateDir 'runtime-http.pid'
$cloudflarePidFile = Join-Path $stateDir 'cloudflared.pid'
$runtimeBinary = [string]$install.binary
$cloudflaredBinary = [string]$config.cloudflared_binary
if ([string]::IsNullOrWhiteSpace($cloudflaredBinary)) { $cloudflaredBinary = Join-Path $RuntimeInstallDir 'bin\cloudflared.exe' }

if (-not (Test-OwnedPid $runtimePidFile $runtimeBinary)) {
    Start-Process powershell.exe -WindowStyle Hidden -ArgumentList @('-NoLogo','-NoProfile','-ExecutionPolicy','Bypass','-File',(Join-Path $scriptsDir 'Run-RuntimePublic.ps1'),'-RuntimeInstallDir',$RuntimeInstallDir) -RedirectStandardOutput (Join-Path $logsDir 'runtime-public.out.log') -RedirectStandardError (Join-Path $logsDir 'runtime-public.err.log') | Out-Null
}
if (-not (Test-OwnedPid $cloudflarePidFile $cloudflaredBinary)) {
    Start-Process powershell.exe -WindowStyle Hidden -ArgumentList @('-NoLogo','-NoProfile','-ExecutionPolicy','Bypass','-File',(Join-Path $scriptsDir 'Run-RuntimeCloudflared.ps1'),'-RuntimeInstallDir',$RuntimeInstallDir) -RedirectStandardOutput (Join-Path $logsDir 'runtime-cloudflared.out.log') -RedirectStandardError (Join-Path $logsDir 'runtime-cloudflared.err.log') | Out-Null
}

$deadline = [DateTime]::UtcNow.AddSeconds(6)
do {
    Start-Sleep -Milliseconds 250
    $runtimeUp = Test-OwnedPid $runtimePidFile $runtimeBinary
    $cloudflareUp = Test-OwnedPid $cloudflarePidFile $cloudflaredBinary
} while((!$runtimeUp -or !$cloudflareUp) -and [DateTime]::UtcNow -lt $deadline)

$config.status = if($runtimeUp -and $cloudflareUp) { 'connected' } else { 'error' }
$config.updated_at = [DateTimeOffset]::Now.ToString('o')
[IO.File]::WriteAllText($configPath, ($config | ConvertTo-Json -Depth 8), [Text.UTF8Encoding]::new($false))
if(-not $runtimeUp -or -not $cloudflareUp) { throw 'Runtime public edge did not start cleanly. Check Runtime Control logs.' }

[ordered]@{ status='connected'; public_mcp_url=[string]$config.public_mcp_url; runtime_http=$runtimeUp; cloudflare_tunnel=$cloudflareUp; acp='dormant' } | ConvertTo-Json
