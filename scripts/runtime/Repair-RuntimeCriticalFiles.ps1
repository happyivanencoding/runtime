# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param([string]$BackupRoot = (Join-Path $env:ProgramData 'RuntimeCoreRecovery'))
$ErrorActionPreference = 'Stop'
$BackupRoot = [IO.Path]::GetFullPath($BackupRoot).TrimEnd('\')
$manifest = Get-Content -LiteralPath (Join-Path $BackupRoot 'manifest.json') -Raw | ConvertFrom-Json
$statePath = Join-Path $BackupRoot 'repair-state.json'
$state = if(Test-Path -LiteralPath $statePath){Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json}else{[pscustomobject]@{}}
$events = @()
$groups = @()
$createdAt = [DateTimeOffset]::Parse([string]$manifest.created_at)
$missing = @($manifest.files | Where-Object {-not (Test-Path -LiteralPath $_.destination -PathType Leaf)})
$defender = @()
if($missing.Count -gt 0 -and (Get-Command Get-MpThreatDetection -ErrorAction SilentlyContinue)) {
    try { $defender = @(Get-MpThreatDetection -ErrorAction Stop | Where-Object {$_.InitialDetectionTime -ge $createdAt.LocalDateTime}) } catch {}
}
foreach($file in $missing) {
    try {
        $last = $state.PSObject.Properties[[string]$file.id]
        if($null -ne $last -and [DateTimeOffset]::Now - [DateTimeOffset]::Parse([string]$last.Value) -lt [TimeSpan]::FromMinutes(10)) {
            throw 'repeated_disappearance_requires_review'
        }
        if(@($defender | Where-Object {($_.Resources -join ' ').IndexOf([string]$file.destination,[StringComparison]::OrdinalIgnoreCase) -ge 0}).Count -gt 0) {throw 'antivirus_detection_requires_review'}
        $surfsharkLog = Join-Path $env:ProgramData 'Surfshark\antivirusLog.log'
        if(Test-Path -LiteralPath $surfsharkLog) {
            foreach($match in (Select-String -LiteralPath $surfsharkLog -SimpleMatch ([string]$file.destination) -ErrorAction SilentlyContinue)) {
                if($match.Line -notmatch 'Adding threat to quarantine|is not yet moved to quarantine'){continue}
                $detectedAt = [DateTimeOffset]::MinValue
                if($match.Line.Length -ge 24 -and [DateTimeOffset]::TryParse($match.Line.Substring(0,24),[ref]$detectedAt) -and $detectedAt -ge $createdAt) {throw 'antivirus_detection_requires_review'}
            }
        }
        $backup = [IO.Path]::GetFullPath((Join-Path $BackupRoot ([string]$file.backup)))
        if(-not $backup.StartsWith($BackupRoot+'\',[StringComparison]::OrdinalIgnoreCase)){throw 'invalid_backup_path'}
        if((Get-FileHash -LiteralPath $backup -Algorithm SHA256).Hash -ne $file.sha256){throw 'backup_hash_mismatch'}
        $destination = [string]$file.destination
        New-Item -ItemType Directory -Path (Split-Path -Parent $destination) -Force | Out-Null
        $staged = $destination + '.repair-' + [Guid]::NewGuid().ToString('N')
        try {
            [IO.File]::Copy($backup,$staged,$false)
            if((Get-FileHash -LiteralPath $staged -Algorithm SHA256).Hash -ne $file.sha256){throw 'staged_hash_mismatch'}
            # Move without replacement: a file recreated by its owner must never be overwritten.
            [IO.File]::Move($staged,$destination)
        } finally { if(Test-Path -LiteralPath $staged){Remove-Item -LiteralPath $staged -Force} }
        $state | Add-Member -NotePropertyName ([string]$file.id) -NotePropertyValue ([DateTimeOffset]::Now.ToString('o')) -Force
        $events += [ordered]@{file=$file.id;status='restored'}
        $groups += [string]$file.group
    } catch { $events += [ordered]@{file=$file.id;status='blocked';reason=$_.Exception.Message} }
}
[IO.File]::WriteAllText($statePath,($state|ConvertTo-Json -Depth 5),[Text.UTF8Encoding]::new($false))
# Use the existing process owners, preserving disabled tasks and the Runtime kill switch.
foreach($taskName in @(
    $(if($groups -contains 'runtime' -or $groups -contains 'launcher'){'Runtime Public Edge Recovery'}),
    $(if($groups -contains 'agentdock' -or $groups -contains 'node'){'AgentDock'})
)) {
    if([string]::IsNullOrWhiteSpace($taskName)){continue}
    try {
        $task = Get-ScheduledTask -TaskName $taskName -ErrorAction Stop
        if($task.State -ne 'Disabled' -and $task.State -ne 'Running'){Start-ScheduledTask -TaskName $taskName}
    } catch { $events += [ordered]@{task=$taskName;status='start_failed';reason=$_.Exception.Message} }
}
$result = [ordered]@{status=$(if(@($events|Where-Object {$_.status -in @('blocked','start_failed')}).Count){'attention_required'}else{'healthy'});checked_at=[DateTimeOffset]::Now.ToString('o');checked_files=@($manifest.files).Count;missing_files=$missing.Count;events=$events;acp='not_started'}
[IO.File]::WriteAllText((Join-Path $BackupRoot 'status.json'),($result|ConvertTo-Json -Depth 6),[Text.UTF8Encoding]::new($false))
if($events.Count){Add-Content -LiteralPath (Join-Path $BackupRoot 'repairs.jsonl') -Value ($result|ConvertTo-Json -Depth 6 -Compress) -Encoding UTF8}
$result | ConvertTo-Json -Depth 6
