# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param(
    [string]$RuntimeInstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore'),
    [string]$AgentDockLog = (Join-Path $env:LOCALAPPDATA 'AgentDock\logs\agentdock.err.log')
)

$ErrorActionPreference = 'Stop'
$RuntimeInstallDir = [IO.Path]::GetFullPath($RuntimeInstallDir)
$installPath = Join-Path $RuntimeInstallDir 'install.json'
if (-not (Test-Path -LiteralPath $installPath -PathType Leaf)) { throw "Runtime install state is missing: $installPath" }
$install = Get-Content -LiteralPath $installPath -Raw | ConvertFrom-Json
$runtimeHome = [IO.Path]::GetFullPath([string]$install.runtime_home)
$telemetryDir = Join-Path $runtimeHome 'telemetry'
New-Item -ItemType Directory -Force $telemetryDir | Out-Null
$outputPath = Join-Path $telemetryDir 'agentdock-tool-usage.jsonl'
$statePath = Join-Path $telemetryDir 'agentdock-tool-usage.state.json'

if (-not (Test-Path -LiteralPath $AgentDockLog -PathType Leaf)) {
    [ordered]@{ status='missing_source'; source=$AgentDockLog; output=$outputPath } | ConvertTo-Json
    return
}

$sourceInfo = Get-Item -LiteralPath $AgentDockLog
$currentLength = [int64]$sourceInfo.Length
$offset = $currentLength
$initialized = $true
if (Test-Path -LiteralPath $statePath -PathType Leaf) {
    try {
        $state = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
        if ([string]$state.source -eq $AgentDockLog) {
            $offset = [int64]$state.offset
            $initialized = $false
        }
    } catch {
        $offset = $currentLength
        $initialized = $true
    }
}

if ($offset -lt 0 -or $offset -gt $currentLength) {
    $offset = 0
    $initialized = $false
}

if ($initialized) {
    $state = [ordered]@{
        schema_version = 1
        source = $AgentDockLog
        offset = $currentLength
        updated_at = [DateTimeOffset]::Now.ToString('o')
    }
    [IO.File]::WriteAllText($statePath, ($state | ConvertTo-Json -Depth 4), [Text.UTF8Encoding]::new($false))
    [ordered]@{ status='initialized'; source=$AgentDockLog; output=$outputPath; offset=$currentLength; imported=0 } | ConvertTo-Json
    return
}

function Get-ErrorKind([string]$Message) {
    $text = ([string]$Message).ToLowerInvariant()
    if ([string]::IsNullOrWhiteSpace($text)) { return 'tool_error' }
    if ($text.Contains('timeout') -or $text.Contains('timed out')) { return 'timeout' }
    if ($text.Contains('auth') -or $text.Contains('unauthorized') -or $text.Contains('forbidden')) { return 'auth' }
    if ($text.Contains('not found') -or $text.Contains('unknown tool')) { return 'not_found' }
    if ($text.Contains('invalid')) { return 'validation' }
    return 'tool_error'
}

$imported = 0
$utf8 = [Text.UTF8Encoding]::new($false)
$readShare = [IO.FileShare]::ReadWrite -bor [IO.FileShare]::Delete
$stream = [IO.File]::Open($AgentDockLog, [IO.FileMode]::Open, [IO.FileAccess]::Read, $readShare)
try {
    [void]$stream.Seek($offset, [IO.SeekOrigin]::Begin)
    $reader = [IO.StreamReader]::new($stream, $utf8, $true, 65536, $true)
    try {
        $writer = [IO.StreamWriter]::new($outputPath, $true, $utf8)
        try {
            while (($line = $reader.ReadLine()) -ne $null) {
                if ($line.IndexOf('"msg":"tool finished"', [StringComparison]::Ordinal) -lt 0) { continue }
                try { $entry = $line | ConvertFrom-Json } catch { continue }
                if ([string]$entry.msg -ne 'tool finished' -or [string]::IsNullOrWhiteSpace([string]$entry.tool)) { continue }
                $ok = $false
                if ($null -ne $entry.ok) { $ok = [bool]$entry.ok }
                $event = [ordered]@{
                    schema_version = 1
                    time = [string]$entry.time
                    event = 'tool_finished'
                    backend = 'agentdock'
                    source = 'agentdock'
                    transport = 'http'
                    tool = [string]$entry.tool
                    duration_ms = if($null -ne $entry.duration_ms){[int64]$entry.duration_ms}else{0}
                    ok = $ok
                    error_code = if($ok){$null}else{Get-ErrorKind ([string]$entry.error)}
                }
                $writer.WriteLine(($event | ConvertTo-Json -Compress -Depth 4))
                $imported++
            }
            $writer.Flush()
        } finally { $writer.Dispose() }
    } finally { $reader.Dispose() }
    $newOffset = [int64]$stream.Position
} finally { $stream.Dispose() }

$state = [ordered]@{
    schema_version = 1
    source = $AgentDockLog
    offset = $newOffset
    updated_at = [DateTimeOffset]::Now.ToString('o')
}
[IO.File]::WriteAllText($statePath, ($state | ConvertTo-Json -Depth 4), $utf8)
[ordered]@{ status='ok'; source=$AgentDockLog; output=$outputPath; offset=$newOffset; imported=$imported } | ConvertTo-Json
