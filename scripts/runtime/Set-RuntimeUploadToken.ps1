# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param(
    [string]$RuntimeInstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore'),
    [switch]$Rotate,
    [switch]$CopyToClipboard
)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Security

$secretDir = Join-Path $RuntimeInstallDir 'secrets'
$secretPath = Join-Path $secretDir 'upload-token.dpapi'
$entropy = [Text.Encoding]::UTF8.GetBytes('runtime.upload.bearer.v1')
New-Item -ItemType Directory -Force $secretDir | Out-Null

if ($Rotate -or -not (Test-Path -LiteralPath $secretPath -PathType Leaf)) {
    $bytes = New-Object byte[] 32
    $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
    $token = [Convert]::ToBase64String($bytes).TrimEnd('=').Replace('+', '-').Replace('/', '_')
    [Array]::Clear($bytes, 0, $bytes.Length)
    $plain = [Text.Encoding]::UTF8.GetBytes($token)
    try {
        $encrypted = [Security.Cryptography.ProtectedData]::Protect(
            $plain, $entropy, [Security.Cryptography.DataProtectionScope]::CurrentUser)
        [IO.File]::WriteAllText($secretPath, [Convert]::ToBase64String($encrypted), [Text.UTF8Encoding]::new($false))
    } finally {
        [Array]::Clear($plain, 0, $plain.Length)
    }
    $wasCreated = $true
} else {
    $wasCreated = $false
}

if ($CopyToClipboard) {
    if (-not $wasCreated) {
        $encrypted = [Convert]::FromBase64String([IO.File]::ReadAllText($secretPath).Trim())
        $plain = [Security.Cryptography.ProtectedData]::Unprotect(
            $encrypted, $entropy, [Security.Cryptography.DataProtectionScope]::CurrentUser)
        try { $token = [Text.Encoding]::UTF8.GetString($plain) }
        finally { [Array]::Clear($plain, 0, $plain.Length) }
    }
    Set-Clipboard -Value $token
}

[ordered]@{
    upload_token_configured = (Test-Path -LiteralPath $secretPath)
    newly_generated = $wasCreated
    copied_to_clipboard = [bool]$CopyToClipboard
    scope = 'ZIP upload only; no MCP tool access'
    requires_runtime_restart = $wasCreated
} | ConvertTo-Json
