# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$TokenFile,
    [string]$RuntimeInstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore')
)

$ErrorActionPreference = 'Stop'
$TokenFile = (Resolve-Path -LiteralPath $TokenFile).Path
$RuntimeInstallDir = [IO.Path]::GetFullPath($RuntimeInstallDir)
$secretsDir = Join-Path $RuntimeInstallDir 'secrets'
New-Item -ItemType Directory -Force $secretsDir | Out-Null

$token = [IO.File]::ReadAllText($TokenFile, [Text.Encoding]::UTF8).Trim()
if ([string]::IsNullOrWhiteSpace($token)) { throw 'Cloudflare tunnel token file is empty.' }
$plain = [Text.Encoding]::UTF8.GetBytes($token)
try {
    $protected = [Security.Cryptography.ProtectedData]::Protect(
        $plain,
        [Text.Encoding]::UTF8.GetBytes('runtime.cloudflare.tunnel.v1'),
        [Security.Cryptography.DataProtectionScope]::CurrentUser)
    $target = Join-Path $secretsDir 'cloudflared-token.dpapi'
    [IO.File]::WriteAllText($target, [Convert]::ToBase64String($protected), [Text.UTF8Encoding]::new($false))
}
finally {
    [Array]::Clear($plain, 0, $plain.Length)
    $token = $null
}

# Never echo the token.
[ordered]@{ stored = $true; scope = 'CurrentUser DPAPI'; target = 'Runtime independent Cloudflare tunnel' } | ConvertTo-Json
