# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param(
    [string]$RuntimeInstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore'),
    [double]$Hours = 24,
    [int]$Limit = 500,
    [switch]$IncludeProbes
)

$ErrorActionPreference = 'Stop'
if ($Hours -le 0) { throw 'Hours must be positive.' }
if ($Limit -lt 1 -or $Limit -gt 10000) { throw 'Limit must be between 1 and 10000.' }
$RuntimeInstallDir = [IO.Path]::GetFullPath($RuntimeInstallDir)
$installPath = Join-Path $RuntimeInstallDir 'install.json'
if (-not (Test-Path -LiteralPath $installPath -PathType Leaf)) { throw "Runtime install state is missing: $installPath" }
$install = Get-Content -LiteralPath $installPath -Raw | ConvertFrom-Json
$runtimeHome = [IO.Path]::GetFullPath([string]$install.runtime_home)
$telemetryDir = Join-Path $runtimeHome 'telemetry'
$since = [DateTimeOffset]::Now.AddHours(-$Hours)
$paths = @(
    (Join-Path $telemetryDir 'runtime-routing.jsonl'),
    (Join-Path $telemetryDir 'agentdock-tool-usage.jsonl')
)

$events = [System.Collections.Generic.List[object]]::new()
foreach ($path in $paths) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { continue }
    foreach ($line in [IO.File]::ReadLines($path)) {
        if ([string]::IsNullOrWhiteSpace($line)) { continue }
        try { $entry = $line | ConvertFrom-Json } catch { continue }
        if ([string]$entry.event -ne 'tool_finished') { continue }
        if (-not $IncludeProbes -and [bool]$entry.probe) { continue }
        try { $timestamp = [DateTimeOffset]::Parse([string]$entry.time) } catch { continue }
        if ($timestamp -lt $since) { continue }
        $events.Add([pscustomobject]@{
            timestamp = $timestamp
            backend = [string]$entry.backend
            source = [string]$entry.source
            via = [string]$entry.via
            transport = [string]$entry.transport
            tool = [string]$entry.tool
            ok = if($null -eq $entry.ok){$null}else{[bool]$entry.ok}
            duration_ms = if($null -eq $entry.duration_ms){0}else{[int64]$entry.duration_ms}
            error_code = [string]$entry.error_code
            target_server = [string]$entry.target_server
            target_tool = [string]$entry.target_tool
            request_id = [string]$entry.request_id
        })
    }
}

$ordered = @($events | Sort-Object timestamp | Select-Object -Last $Limit)
$previousBackend = $null
foreach ($entry in $ordered) {
    $switchFrom = $null
    if (-not [string]::IsNullOrWhiteSpace($previousBackend) -and $previousBackend -ne $entry.backend) {
        $switchFrom = $previousBackend
    }
    [pscustomobject]@{
        time = $entry.timestamp.ToString('o')
        backend = $entry.backend
        source = $entry.source
        via = $entry.via
        tool = $entry.tool
        ok = $entry.ok
        duration_ms = $entry.duration_ms
        switch_from = $switchFrom
        target_server = $entry.target_server
        target_tool = $entry.target_tool
        error_code = $entry.error_code
        request_id = $entry.request_id
    }
    $previousBackend = $entry.backend
}
