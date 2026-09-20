# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param(
    [string]$RuntimeInstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore'),
    [string]$AgentDockInstallDir = (Join-Path $env:LOCALAPPDATA 'AgentDock'),
    [string]$BackupRoot = (Join-Path $env:ProgramData 'RuntimeCoreRecovery'),
    [string]$NodeBinary = (Join-Path $env:ProgramFiles 'nodejs\node.exe')
)
$ErrorActionPreference = 'Stop'
$snapshotName = [DateTime]::UtcNow.ToString('yyyyMMdd-HHmmss') + '-' + [Guid]::NewGuid().ToString('N').Substring(0,8)
$snapshot = Join-Path $BackupRoot ('snapshots\' + $snapshotName)
New-Item -ItemType Directory -Path $snapshot -Force | Out-Null
$files = @()
function Add-BackupFile([string]$Source, [string]$Group, [string]$Relative) {
    if (-not (Test-Path -LiteralPath $Source -PathType Leaf)) { throw "Critical backup source missing: $Source" }
    $relativeBackup = 'snapshots\' + $snapshotName + '\' + $Group + '\' + $Relative
    $target = Join-Path $BackupRoot $relativeBackup
    New-Item -ItemType Directory -Path (Split-Path -Parent $target) -Force | Out-Null
    Copy-Item -LiteralPath $Source -Destination $target
    $hash = (Get-FileHash -LiteralPath $Source -Algorithm SHA256).Hash
    if ((Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash -ne $hash) { throw "Backup verification failed: $Source" }
    $script:files += [ordered]@{ id=($Group + '/' + $Relative.Replace('\','/')); group=$Group; destination=[IO.Path]::GetFullPath($Source); backup=$relativeBackup; sha256=$hash }
}
foreach($required in @(
    @((Join-Path $RuntimeInstallDir 'bin\runtime-core.exe'),'runtime','bin\runtime-core.exe'),
    @((Join-Path $RuntimeInstallDir 'bin\cloudflared.exe'),'runtime','bin\cloudflared.exe'),
    @((Join-Path $RuntimeInstallDir 'control\Runtime.Control.exe'),'runtime','control\Runtime.Control.exe'),
    @((Join-Path $AgentDockInstallDir 'bin\agentdock.exe'),'agentdock','bin\agentdock.exe'),
    @((Join-Path $AgentDockInstallDir 'bin\agentdock-tray.exe'),'agentdock','bin\agentdock-tray.exe'),
    @((Join-Path $AgentDockInstallDir 'bin\cloudflared.exe'),'agentdock','bin\cloudflared.exe'),
    @($NodeBinary,'node','node.exe')
)) { Add-BackupFile $required[0] $required[1] $required[2] }
foreach($name in 'install.json','public-connection.json','chatgpt-connection.json','chrome-native-host.json','LICENSE','NOTICE') {
    $source = Join-Path $RuntimeInstallDir $name
    if(Test-Path -LiteralPath $source -PathType Leaf){Add-BackupFile $source 'runtime' $name}
}
foreach($folder in 'scripts','chrome-extension','secrets') {
    $directory = Join-Path $RuntimeInstallDir $folder
    foreach($file in (Get-ChildItem -LiteralPath $directory -File -Recurse -ErrorAction SilentlyContinue)) {
        if($folder -eq 'secrets' -and $file.Extension -notin @('.dpapi','.json')){continue}
        if($folder -eq 'scripts' -and $file.Extension -notin @('.ps1','.vbs')){continue}
        Add-BackupFile $file.FullName 'runtime' ($folder + '\' + $file.FullName.Substring($directory.Length).TrimStart('\'))
    }
}
foreach($file in (Get-ChildItem -LiteralPath $AgentDockInstallDir -File)) {
    if($file.Extension -in @('.ps1','.vbs','.dpapi','.json','.txt')){Add-BackupFile $file.FullName 'agentdock' $file.Name}
}
$hiddenLauncher = Join-Path $env:LOCALAPPDATA 'BackgroundTasks\run-hidden.vbs'
if(Test-Path -LiteralPath $hiddenLauncher){Add-BackupFile $hiddenLauncher 'launcher' 'run-hidden.vbs'}
$manifest = [ordered]@{schema_version=1;created_at=[DateTimeOffset]::Now.ToString('o');files=$files}
$manifestPath = Join-Path $BackupRoot 'manifest.json'
$pending = $manifestPath + '.new'
[IO.File]::WriteAllText($pending,($manifest|ConvertTo-Json -Depth 8),[Text.UTF8Encoding]::new($false))
Move-Item -LiteralPath $pending -Destination $manifestPath -Force
[ordered]@{status='verified';files=$files.Count;snapshot=$snapshotName;backup_root=$BackupRoot} | ConvertTo-Json
