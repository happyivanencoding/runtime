# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param([string]$RuntimeInstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore'))

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Security
$RuntimeInstallDir = [IO.Path]::GetFullPath($RuntimeInstallDir)
$secretsDir = Join-Path $RuntimeInstallDir 'secrets'
$stateDir = Join-Path $RuntimeInstallDir 'public-edge'
New-Item -ItemType Directory -Force $stateDir | Out-Null
$config = Get-Content -LiteralPath (Join-Path $RuntimeInstallDir 'public-connection.json') -Raw | ConvertFrom-Json
$cloudflared = [string]$config.cloudflared_binary
if ([string]::IsNullOrWhiteSpace($cloudflared)) { $cloudflared = Join-Path $RuntimeInstallDir 'bin\cloudflared.exe' }
if (-not (Test-Path -LiteralPath $cloudflared -PathType Leaf)) { throw "Runtime cloudflared missing: $cloudflared" }

$tokenPath = Join-Path $secretsDir 'cloudflared-token.dpapi'
if (-not (Test-Path -LiteralPath $tokenPath -PathType Leaf)) { throw 'Runtime independent Cloudflare tunnel token is not configured.' }
$encoded = [IO.File]::ReadAllText($tokenPath, [Text.Encoding]::UTF8).Trim()
$plain = [Security.Cryptography.ProtectedData]::Unprotect(
    [Convert]::FromBase64String($encoded),
    [Text.Encoding]::UTF8.GetBytes('runtime.cloudflare.tunnel.v1'),
    [Security.Cryptography.DataProtectionScope]::CurrentUser)
try { $env:TUNNEL_TOKEN = [Text.Encoding]::UTF8.GetString($plain) }
finally { [Array]::Clear($plain, 0, $plain.Length) }

$process = Start-Process -FilePath $cloudflared -ArgumentList @('tunnel','--protocol','http2','run') -PassThru -NoNewWindow
[IO.File]::WriteAllText((Join-Path $stateDir 'cloudflared.pid'), $process.Id.ToString(), [Text.UTF8Encoding]::new($false))
try { $process.WaitForExit(); exit $process.ExitCode }
finally {
    $pidPath = Join-Path $stateDir 'cloudflared.pid'
    try {
        if (([IO.File]::ReadAllText($pidPath).Trim()) -eq $process.Id.ToString()) {
            Remove-Item -LiteralPath $pidPath -Force -ErrorAction SilentlyContinue
        }
    } catch {}
    Remove-Item Env:TUNNEL_TOKEN -ErrorAction SilentlyContinue
}
