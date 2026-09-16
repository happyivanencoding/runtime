# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param([string]$RuntimeInstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore'))

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Security
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
$isAdministrator = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if ($config.execution_privilege -eq 'administrator' -and -not $isAdministrator) {
    $elevated = Start-Process powershell.exe -Verb RunAs -WindowStyle Hidden -PassThru -Wait -ArgumentList @('-NoLogo','-NoProfile','-ExecutionPolicy','Bypass','-File',('"' + $PSCommandPath + '"'),'-RuntimeInstallDir',('"' + $RuntimeInstallDir + '"'))
    if ($elevated.ExitCode -ne 0) { throw 'Administrator startup failed or was cancelled.' }
    [ordered]@{ status=(Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json).status; execution_privilege='administrator'; acp='dormant' } | ConvertTo-Json
    return
}

function Read-RuntimeSecret([string]$Name, [string]$Entropy) {
    $path = Join-Path $secretsDir $Name
    $encoded = [IO.File]::ReadAllText($path, [Text.Encoding]::UTF8).Trim()
    $plain = [Security.Cryptography.ProtectedData]::Unprotect(
        [Convert]::FromBase64String($encoded),
        [Text.Encoding]::UTF8.GetBytes($Entropy),
        [Security.Cryptography.DataProtectionScope]::CurrentUser)
    try { return [Text.Encoding]::UTF8.GetString($plain) }
    finally { [Array]::Clear($plain, 0, $plain.Length) }
}

function Test-AuthenticatedContext([string]$Origin, [string]$Token, [int]$TimeoutSec = 4) {
    if ([string]::IsNullOrWhiteSpace($Origin) -or [string]::IsNullOrWhiteSpace($Token)) { return $false }
    try {
        $response = Invoke-WebRequest -Uri ($Origin.TrimEnd('/') + '/context') -Headers @{ Authorization = ('Bearer ' + $Token) } -UseBasicParsing -TimeoutSec $TimeoutSec -MaximumRedirection 0
        if ([int]$response.StatusCode -ne 200) { return $false }
        $payload = $response.Content | ConvertFrom-Json
        return $null -ne $payload.runtime -and -not [string]::IsNullOrWhiteSpace([string]$payload.runtime.version)
    } catch {
        return $false
    }
}

function Stop-OwnedProcess([string]$PidFile, [string]$ExpectedExe) {
    if (-not (Test-Path -LiteralPath $PidFile)) { return $false }
    $raw = [IO.File]::ReadAllText($PidFile).Trim()
    $pidValue = 0
    if (-not [int]::TryParse($raw, [ref]$pidValue)) { return $false }
    try {
        $p = Get-Process -Id $pidValue -ErrorAction Stop
        if ($p.Path -ine $ExpectedExe) { return $false }
        Stop-Process -Id $pidValue -Force
        return $true
    } catch { return $false }
}

function Start-RuntimeHttpWrapper {
    Start-Process powershell.exe -WindowStyle Hidden -ArgumentList @('-NoLogo','-NoProfile','-ExecutionPolicy','Bypass','-File',(Join-Path $scriptsDir 'Run-RuntimePublic.ps1'),'-RuntimeInstallDir',$RuntimeInstallDir) -RedirectStandardOutput (Join-Path $logsDir 'runtime-public.out.log') -RedirectStandardError (Join-Path $logsDir 'runtime-public.err.log') | Out-Null
}

function Start-RuntimeCloudflareWrapper {
    Start-Process powershell.exe -WindowStyle Hidden -ArgumentList @('-NoLogo','-NoProfile','-ExecutionPolicy','Bypass','-File',(Join-Path $scriptsDir 'Run-RuntimeCloudflared.ps1'),'-RuntimeInstallDir',$RuntimeInstallDir) -RedirectStandardOutput (Join-Path $logsDir 'runtime-cloudflared.out.log') -RedirectStandardError (Join-Path $logsDir 'runtime-cloudflared.err.log') | Out-Null
}

function Wait-ForContext([string]$Origin, [string]$Token, [int]$Seconds) {
    $deadline = [DateTime]::UtcNow.AddSeconds($Seconds)
    do {
        if (Test-AuthenticatedContext $Origin $Token) { return $true }
        Start-Sleep -Milliseconds 500
    } while([DateTime]::UtcNow -lt $deadline)
    return $false
}

function Set-ConfigValue([object]$Config, [string]$Name, [object]$Value) {
    $Config | Add-Member -NotePropertyName $Name -NotePropertyValue $Value -Force
}

$install = Get-Content -LiteralPath (Join-Path $RuntimeInstallDir 'install.json') -Raw | ConvertFrom-Json
$runtimePidFile = Join-Path $stateDir 'runtime-http.pid'
$cloudflarePidFile = Join-Path $stateDir 'cloudflared.pid'
$runtimeBinary = [string]$install.binary
$cloudflaredBinary = [string]$config.cloudflared_binary
if ([string]::IsNullOrWhiteSpace($cloudflaredBinary)) { $cloudflaredBinary = Join-Path $RuntimeInstallDir 'bin\cloudflared.exe' }

$config.status = 'starting'
$config.updated_at = [DateTimeOffset]::Now.ToString('o')
[IO.File]::WriteAllText($configPath, ($config | ConvertTo-Json -Depth 8), [Text.UTF8Encoding]::new($false))

$token = Read-RuntimeSecret 'auth-token.dpapi' 'runtime.auth.bearer.v1'
try {
    $runtimeWasRunning = [bool](Test-OwnedPid $runtimePidFile $runtimeBinary)
    if (-not $runtimeWasRunning) { Start-RuntimeHttpWrapper }

    $localReady = Wait-ForContext ([string]$config.local_origin) $token 15
    $runtimeUp = [bool](Test-OwnedPid $runtimePidFile $runtimeBinary)
    if (-not $runtimeUp -or -not $localReady) {
        $config.status = 'error'
        $config.updated_at = [DateTimeOffset]::Now.ToString('o')
        [IO.File]::WriteAllText($configPath, ($config | ConvertTo-Json -Depth 8), [Text.UTF8Encoding]::new($false))
        throw 'Runtime local HTTP is not healthy; public tunnel was not recycled.'
    }

    $cloudflareWasRunning = [bool](Test-OwnedPid $cloudflarePidFile $cloudflaredBinary)
    $tunnelRecycled = $false
    if (-not $cloudflareWasRunning) {
        Start-RuntimeCloudflareWrapper
        if ($runtimeWasRunning) {
            Set-ConfigValue $config 'last_recovery_at' ([DateTimeOffset]::Now.ToString('o'))
            Set-ConfigValue $config 'last_recovery_reason' 'cloudflared_not_running'
        }
    }

    $publicReady = $false
    if ($cloudflareWasRunning) {
        for($attempt = 1; $attempt -le 3; $attempt++) {
            if (Test-AuthenticatedContext ([string]$config.public_origin) $token) { $publicReady = $true; break }
            if ($attempt -lt 3) { Start-Sleep -Milliseconds 500 }
        }
        if (-not $publicReady) {
            if (Stop-OwnedProcess $cloudflarePidFile $cloudflaredBinary) {
                Start-Sleep -Milliseconds 500
                Start-RuntimeCloudflareWrapper
                $tunnelRecycled = $true
                Set-ConfigValue $config 'last_recovery_at' ([DateTimeOffset]::Now.ToString('o'))
                Set-ConfigValue $config 'last_recovery_reason' 'public_context_failed'
            }
        }
    }

    if (-not $publicReady) { $publicReady = Wait-ForContext ([string]$config.public_origin) $token 30 }
    $cloudflareUp = [bool](Test-OwnedPid $cloudflarePidFile $cloudflaredBinary)
    $config.status = if($localReady -and $publicReady -and $cloudflareUp) { 'connected' } else { 'error' }
    $liveCheckAt = [DateTimeOffset]::Now.ToString('o')
    Set-ConfigValue $config 'last_live_check_at' $liveCheckAt
    $config.updated_at = $liveCheckAt
    [IO.File]::WriteAllText($configPath, ($config | ConvertTo-Json -Depth 8), [Text.UTF8Encoding]::new($false))
    if(-not $publicReady -or -not $cloudflareUp) { throw 'Runtime public edge is not ready after live authenticated checks and tunnel recovery.' }

    [ordered]@{ status='connected'; public_mcp_url=[string]$config.public_mcp_url; runtime_http=$runtimeUp; cloudflare_tunnel=$cloudflareUp; local_context_ready=$localReady; public_context_ready=$publicReady; tunnel_recycled=$tunnelRecycled; execution_privilege= $(if($isAdministrator){'administrator'}else{'standard'}); acp='dormant' } | ConvertTo-Json
} finally {
    Remove-Variable token -ErrorAction SilentlyContinue
}
