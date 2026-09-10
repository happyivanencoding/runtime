# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param([string]$RuntimeInstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore'))

$ErrorActionPreference = 'Stop'
$RuntimeInstallDir = [IO.Path]::GetFullPath($RuntimeInstallDir)
$configPath = Join-Path $RuntimeInstallDir 'public-connection.json'
$stateDir = Join-Path $RuntimeInstallDir 'public-edge'

function Stop-OwnedProcess([string]$PidFile, [string]$ExpectedExe) {
    if (-not (Test-Path -LiteralPath $PidFile)) { return $false }
    $raw = [IO.File]::ReadAllText($PidFile).Trim()
    $pidValue = 0
    if (-not [int]::TryParse($raw, [ref]$pidValue)) { Remove-Item $PidFile -Force -ErrorAction SilentlyContinue; return $false }
    try {
        $p = Get-Process -Id $pidValue -ErrorAction Stop
        if ($p.Path -ieq $ExpectedExe) {
            Stop-Process -Id $pidValue -Force
            $stopped = $true
        } else { $stopped = $false }
    } catch { $stopped = $false }
    Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
    return $stopped
}

$install = Get-Content -LiteralPath (Join-Path $RuntimeInstallDir 'install.json') -Raw | ConvertFrom-Json
$config = Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json
$runtimeBinary = [string]$install.binary
$cloudflaredBinary = [string]$config.cloudflared_binary
if ([string]::IsNullOrWhiteSpace($cloudflaredBinary)) { $cloudflaredBinary = Join-Path $RuntimeInstallDir 'bin\cloudflared.exe' }

$cloudflareStopped = Stop-OwnedProcess (Join-Path $stateDir 'cloudflared.pid') $cloudflaredBinary
$runtimeStopped = Stop-OwnedProcess (Join-Path $stateDir 'runtime-http.pid') $runtimeBinary
$config.status = 'stopped'
$config.updated_at = [DateTimeOffset]::Now.ToString('o')
[IO.File]::WriteAllText($configPath, ($config | ConvertTo-Json -Depth 8), [Text.UTF8Encoding]::new($false))

[ordered]@{ status='stopped'; runtime_http_stopped=$runtimeStopped; cloudflare_tunnel_stopped=$cloudflareStopped; agentdock_untouched=$true; chrome_untouched=$true } | ConvertTo-Json
