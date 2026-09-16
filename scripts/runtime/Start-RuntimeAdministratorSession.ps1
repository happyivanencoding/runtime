# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param([string]$RuntimeInstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore'))

$ErrorActionPreference = 'Stop'
$RuntimeInstallDir = [IO.Path]::GetFullPath($RuntimeInstallDir)
$isAdministrator = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdministrator) {
    $elevated = Start-Process powershell.exe -Verb RunAs -WindowStyle Hidden -Wait -PassThru -ArgumentList @('-NoLogo','-NoProfile','-ExecutionPolicy','Bypass','-File',('"' + $PSCommandPath + '"'),'-RuntimeInstallDir',('"' + $RuntimeInstallDir + '"'))
    if ($elevated.ExitCode -ne 0) { throw 'Administrator session did not start. Exit Chrome completely and check Runtime logs.' }
    return
}

# Chrome reuses an existing browser process even when the new launcher is elevated.
# Never terminate a user's browser or discard pending browser input from this helper.
if (Get-Process -Name chrome -ErrorAction SilentlyContinue) {
    throw 'Exit Chrome completely before starting the administrator session. No processes were changed.'
}
$chrome = Join-Path $env:ProgramFiles 'Google\Chrome\Application\chrome.exe'
if (-not (Test-Path -LiteralPath $chrome)) { throw 'Installed Google Chrome was not found.' }
$control = Join-Path $RuntimeInstallDir 'control\Runtime.Control.exe'
if (-not (Test-Path -LiteralPath $control)) { throw 'Runtime Control is not installed.' }

$env:AGENTDOCK_ACP_ENABLED = 'false'
& (Join-Path $RuntimeInstallDir 'scripts\Stop-RuntimeForChatGPT.ps1') -RuntimeInstallDir $RuntimeInstallDir
if (-not $?) { throw 'Could not stop the previous Runtime public edge.' }
$configPath = Join-Path $RuntimeInstallDir 'public-connection.json'
$config = Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json
$config | Add-Member -NotePropertyName execution_privilege -NotePropertyValue 'administrator' -Force
$config.updated_at = [DateTimeOffset]::Now.ToString('o')
[IO.File]::WriteAllText($configPath, ($config | ConvertTo-Json -Depth 8), [Text.UTF8Encoding]::new($false))
$recoveryTaskInstaller = Join-Path $RuntimeInstallDir 'scripts\Install-RuntimeRecoveryTask.ps1'
if (Test-Path -LiteralPath $recoveryTaskInstaller -PathType Leaf) {
    & $recoveryTaskInstaller -RuntimeInstallDir $RuntimeInstallDir | Out-Null
}

& (Join-Path $RuntimeInstallDir 'scripts\Start-RuntimeForChatGPT.ps1') -RuntimeInstallDir $RuntimeInstallDir
if (-not $?) { throw 'The administrator Runtime public edge is not ready.' }
foreach ($existing in (Get-Process -Name 'Runtime.Control' -ErrorAction SilentlyContinue)) {
    if ($existing.Path -ieq $control) { Stop-Process -Id $existing.Id }
}
$controlProcess = Start-Process -FilePath $control -PassThru
$chromeProcess = Start-Process -FilePath $chrome -ArgumentList '--restore-last-session' -PassThru
$state = [ordered]@{
    status = 'started'
    execution_privilege = 'administrator'
    chrome_pid = $chromeProcess.Id
    runtime_control_pid = $controlProcess.Id
    started_at = [DateTimeOffset]::Now.ToString('o')
    acp = 'dormant'
}
[IO.File]::WriteAllText((Join-Path $RuntimeInstallDir 'public-edge\administrator-session.json'), ($state | ConvertTo-Json), [Text.UTF8Encoding]::new($false))
$state | ConvertTo-Json
