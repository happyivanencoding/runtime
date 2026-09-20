# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$KeyFile,
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore')
)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Security

$source = (Resolve-Path -LiteralPath $KeyFile).Path
$key = [IO.File]::ReadAllText($source, [Text.Encoding]::UTF8).Trim()
if ([string]::IsNullOrWhiteSpace($key)) { throw 'TypeSafe API key file is empty.' }

$InstallDir = [IO.Path]::GetFullPath($InstallDir)
$secretsDir = Join-Path $InstallDir 'secrets'
New-Item -ItemType Directory -Force $secretsDir | Out-Null
$target = Join-Path $secretsDir 'typesafe-api-key.dpapi'
$plain = [Text.Encoding]::UTF8.GetBytes($key)
try {
    $protected = [Security.Cryptography.ProtectedData]::Protect(
        $plain,
        [Text.Encoding]::UTF8.GetBytes('runtime.typesafe.api.v1'),
        [Security.Cryptography.DataProtectionScope]::CurrentUser)
    $temp = $target + '.tmp.' + [Guid]::NewGuid().ToString('N')
    try {
        [IO.File]::WriteAllText($temp, [Convert]::ToBase64String($protected), [Text.UTF8Encoding]::new($false))
        Move-Item -LiteralPath $temp -Destination $target -Force
    } finally {
        Remove-Item -LiteralPath $temp -Force -ErrorAction SilentlyContinue
    }
} finally {
    if ($null -ne $plain) { [Array]::Clear($plain, 0, $plain.Length) }
    if ($null -ne $protected) { [Array]::Clear($protected, 0, $protected.Length) }
    Remove-Variable key -ErrorAction SilentlyContinue
}

[ordered]@{ typesafe_api_key = 'configured'; provider = 'dpapi_current_user' } | ConvertTo-Json
