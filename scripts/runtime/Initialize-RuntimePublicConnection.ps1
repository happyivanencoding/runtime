# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param(
    [string]$RuntimeInstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore'),
    [ValidateRange(1024,65535)][int]$LocalPort = 8767,
    [string]$PublicOrigin = 'https://runtime.thegreatnovel.com'
)

$ErrorActionPreference = 'Stop'
$RuntimeInstallDir = [IO.Path]::GetFullPath($RuntimeInstallDir)
$PublicOrigin = $PublicOrigin.TrimEnd('/')
$configPath = Join-Path $RuntimeInstallDir 'public-connection.json'
$secretsDir = Join-Path $RuntimeInstallDir 'secrets'
New-Item -ItemType Directory -Force $RuntimeInstallDir, $secretsDir | Out-Null
$previous = if (Test-Path -LiteralPath $configPath) { Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json } else { $null }
$previousTunnelId = if ($null -ne $previous) { [string]$previous.cloudflare_tunnel_id } else { '' }
$previousTunnelName = if ($null -ne $previous -and -not [string]::IsNullOrWhiteSpace([string]$previous.cloudflare_tunnel_name)) { [string]$previous.cloudflare_tunnel_name } else { 'runtime' }
$previousStatus = if ($null -ne $previous -and -not [string]::IsNullOrWhiteSpace([string]$previous.status)) { [string]$previous.status } else { 'tunnel_pending' }

$config = [ordered]@{
    schema_version = 1
    architecture = 'public_https_cloudflare_named_tunnel'
    local_origin = "http://127.0.0.1:$LocalPort"
    local_mcp_url = "http://127.0.0.1:$LocalPort/mcp"
    public_origin = $PublicOrigin
    public_mcp_url = "$PublicOrigin/mcp"
    cloudflare_tunnel_name = $previousTunnelName
    cloudflare_tunnel_id = $previousTunnelId
    cloudflare_hostname = ([Uri]$PublicOrigin).Host
    cloudflared_binary = (Join-Path $RuntimeInstallDir 'bin\cloudflared.exe')
    auth_mode = 'oauth+bearer'
    status = $previousStatus
    openai_secure_mcp_tunnel = 'disabled_by_architecture'
    updated_at = [DateTimeOffset]::Now.ToString('o')
}
[IO.File]::WriteAllText($configPath, ($config | ConvertTo-Json -Depth 5), [Text.UTF8Encoding]::new($false))
$config | ConvertTo-Json -Depth 5
