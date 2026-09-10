# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$TunnelId,
    [string]$TunnelName = 'runtime',
    [ValidateSet('tunnel_pending','configured','connected','offline','error','stopped')][string]$Status = 'configured',
    [string]$RuntimeInstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore')
)

$ErrorActionPreference = 'Stop'
$RuntimeInstallDir = [IO.Path]::GetFullPath($RuntimeInstallDir)
$configPath = Join-Path $RuntimeInstallDir 'public-connection.json'
if (-not (Test-Path -LiteralPath $configPath)) { throw 'Runtime public connection is not initialized.' }
$config = Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json
$config.cloudflare_tunnel_name = $TunnelName.Trim()
$config.cloudflare_tunnel_id = $TunnelId.Trim()
$config.status = $Status
$config.updated_at = [DateTimeOffset]::Now.ToString('o')
[IO.File]::WriteAllText($configPath, ($config | ConvertTo-Json -Depth 8), [Text.UTF8Encoding]::new($false))
[ordered]@{ tunnel_name=$config.cloudflare_tunnel_name; tunnel_id=$config.cloudflare_tunnel_id; status=$config.status; public_mcp_url=$config.public_mcp_url } | ConvertTo-Json
