# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param(
    [string]$RuntimeInstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore'),
    [int]$IntervalMinutes = 2
)

$ErrorActionPreference = 'Stop'
if ($IntervalMinutes -lt 1 -or $IntervalMinutes -gt 60) { throw 'IntervalMinutes must be between 1 and 60.' }
$RuntimeInstallDir = [IO.Path]::GetFullPath($RuntimeInstallDir)
$collector = Join-Path $RuntimeInstallDir 'scripts\Collect-AgentDockToolUsage.ps1'
if (-not (Test-Path -LiteralPath $collector -PathType Leaf)) { throw "Runtime tool usage collector is missing: $collector" }

$taskName = 'Runtime Tool Usage Collector'
$powershell = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
$arguments = '-NoLogo -NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File "' + $collector + '" -RuntimeInstallDir "' + $RuntimeInstallDir + '"'
$action = New-ScheduledTaskAction -Execute $powershell -Argument $arguments
$identity = [Security.Principal.WindowsIdentity]::GetCurrent().Name
$logon = New-ScheduledTaskTrigger -AtLogOn -User $identity
$periodic = New-ScheduledTaskTrigger -Once -At (Get-Date).AddMinutes(1) -RepetitionInterval (New-TimeSpan -Minutes $IntervalMinutes) -RepetitionDuration (New-TimeSpan -Days 3650)
$settings = New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -StartWhenAvailable -ExecutionTimeLimit (New-TimeSpan -Minutes 1) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
$principal = New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel Limited
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger @($logon,$periodic) -Settings $settings -Principal $principal -Force | Out-Null
Start-ScheduledTask -TaskName $taskName

[ordered]@{
    task = $taskName
    interval_minutes = $IntervalMinutes
    collector = $collector
    user = $identity
    run_level = 'limited'
} | ConvertTo-Json
