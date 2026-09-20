$ErrorActionPreference = 'Stop'
$repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$root = Join-Path ([IO.Path]::GetTempPath()) ('runtime-file-repair-' + [Guid]::NewGuid().ToString('N'))
$runtime = Join-Path $root 'runtime'
$agent = Join-Path $root 'agentdock'
$backup = Join-Path $root 'backup'
try {
    foreach($name in 'runtime\bin','runtime\control','runtime\scripts','runtime\chrome-extension','runtime\secrets','agentdock\bin','node') {
        New-Item -ItemType Directory -Path (Join-Path $root $name) -Force | Out-Null
    }
    foreach($name in 'runtime\bin\runtime-core.exe','runtime\bin\cloudflared.exe','runtime\control\Runtime.Control.exe','agentdock\bin\agentdock.exe','agentdock\bin\agentdock-tray.exe','agentdock\bin\cloudflared.exe','node\node.exe') {
        [IO.File]::WriteAllText((Join-Path $root $name),('fixture ' + $name))
    }
    & (Join-Path $repo 'scripts\runtime\Backup-RuntimeCriticalFiles.ps1') -RuntimeInstallDir $runtime -AgentDockInstallDir $agent -BackupRoot $backup -NodeBinary (Join-Path $root 'node\node.exe') | Out-Null
    $manifestPath = Join-Path $backup 'manifest.json'
    $manifest = Get-Content $manifestPath -Raw | ConvertFrom-Json
    # Test fixtures must never start the machine's real scheduled tasks.
    $manifest.files = @($manifest.files | Where-Object {$_.destination.StartsWith($root,[StringComparison]::OrdinalIgnoreCase)})
    foreach($file in $manifest.files){$file.group='fixture'}
    $manifest | ConvertTo-Json -Depth 8 | Set-Content $manifestPath
    $target = Join-Path $runtime 'bin\runtime-core.exe'
    $original = [IO.File]::ReadAllText($target)
    Remove-Item -LiteralPath $target
    $first = (& (Join-Path $repo 'scripts\runtime\Repair-RuntimeCriticalFiles.ps1') -BackupRoot $backup) | ConvertFrom-Json
    if($first.status -ne 'healthy' -or [IO.File]::ReadAllText($target) -ne $original){throw 'Missing-file repair failed.'}
    [IO.File]::WriteAllText($target,'newer owner content')
    & (Join-Path $repo 'scripts\runtime\Repair-RuntimeCriticalFiles.ps1') -BackupRoot $backup | Out-Null
    if([IO.File]::ReadAllText($target) -ne 'newer owner content'){throw 'Existing file was overwritten.'}
    Remove-Item -LiteralPath $target
    $repeated = (& (Join-Path $repo 'scripts\runtime\Repair-RuntimeCriticalFiles.ps1') -BackupRoot $backup) | ConvertFrom-Json
    if((Test-Path $target) -or $repeated.events[0].reason -ne 'repeated_disappearance_requires_review'){throw 'Repeated disappearance was not blocked.'}
    $node = $manifest.files | Where-Object {$_.id -eq 'node/node.exe'}
    Remove-Item -LiteralPath $node.destination
    [IO.File]::WriteAllText((Join-Path $backup $node.backup),'tampered backup')
    $corrupt = (& (Join-Path $repo 'scripts\runtime\Repair-RuntimeCriticalFiles.ps1') -BackupRoot $backup) | ConvertFrom-Json
    if((Test-Path $node.destination) -or -not @($corrupt.events | Where-Object {$_.reason -eq 'backup_hash_mismatch'}).Count){throw 'Corrupt backup was not blocked.'}
    'PASS: restores missing bytes, preserves existing files, blocks repeated disappearance, rejects corrupt backup.'
} finally {
    $resolved=[IO.Path]::GetFullPath($root)
    $tempBase=[IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\')+'\'
    if(-not $resolved.StartsWith($tempBase,[StringComparison]::OrdinalIgnoreCase)){throw 'Invalid test cleanup path.'}
    if(Test-Path $resolved){Remove-Item -LiteralPath $resolved -Recurse -Force}
}
