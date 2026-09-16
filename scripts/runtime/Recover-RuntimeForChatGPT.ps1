# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param([string]$RuntimeInstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore'))

$ErrorActionPreference = 'Stop'
$RuntimeInstallDir = [IO.Path]::GetFullPath($RuntimeInstallDir)
$statusScript = Join-Path $RuntimeInstallDir 'scripts\Status-RuntimeForChatGPT.ps1'
$startScript = Join-Path $RuntimeInstallDir 'scripts\Start-RuntimeForChatGPT.ps1'
if (-not (Test-Path -LiteralPath $statusScript -PathType Leaf) -or -not (Test-Path -LiteralPath $startScript -PathType Leaf)) { return }

try {
    $live = (& $statusScript -RuntimeInstallDir $RuntimeInstallDir) | ConvertFrom-Json
    if ($live.status -eq 'connected') {
        [ordered]@{ status='connected'; action='none' } | ConvertTo-Json
        return
    }
    if ($live.status -eq 'unknown') {
        [ordered]@{ status='unknown'; action='none' } | ConvertTo-Json
        return
    }
    if ($live.status -eq 'stopped' -and $live.recorded_status -eq 'stopped') {
        [ordered]@{ status='inactive'; reason='explicitly_stopped'; action='none' } | ConvertTo-Json
        return
    }
    & $startScript -RuntimeInstallDir $RuntimeInstallDir
} catch {
    [ordered]@{ status='error'; action='recovery_failed'; error=$_.Exception.Message } | ConvertTo-Json
    exit 1
}
