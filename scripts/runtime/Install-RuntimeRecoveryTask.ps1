# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param(
    [string]$RuntimeInstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore'),
    [int]$IntervalMinutes = 2
)

$ErrorActionPreference = 'Stop'
if ($IntervalMinutes -lt 1 -or $IntervalMinutes -gt 60) { throw 'IntervalMinutes must be between 1 and 60.' }
$RuntimeInstallDir = [IO.Path]::GetFullPath($RuntimeInstallDir)
$recoveryScript = Join-Path $RuntimeInstallDir 'scripts\Recover-RuntimeForChatGPT.ps1'
if (-not (Test-Path -LiteralPath $recoveryScript -PathType Leaf)) { throw "Runtime recovery script is missing: $recoveryScript" }

$taskName = 'Runtime Public Edge Recovery'
$powershell = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
$arguments = '-NoLogo -NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File "' + $recoveryScript + '" -RuntimeInstallDir "' + $RuntimeInstallDir + '"'
$action = New-ScheduledTaskAction -Execute $powershell -Argument $arguments
$identity = [Security.Principal.WindowsIdentity]::GetCurrent().Name
$configPath = Join-Path $RuntimeInstallDir 'public-connection.json'
$config = if(Test-Path -LiteralPath $configPath -PathType Leaf){Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json}else{$null}
$runLevel = if($null -ne $config -and $config.execution_privilege -eq 'administrator'){'Highest'}else{'Limited'}
$logon = New-ScheduledTaskTrigger -AtLogOn -User $identity
$periodic = New-ScheduledTaskTrigger -Once -At (Get-Date).AddMinutes(1) -RepetitionInterval (New-TimeSpan -Minutes $IntervalMinutes) -RepetitionDuration (New-TimeSpan -Days 3650)
$settings = New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -StartWhenAvailable -ExecutionTimeLimit (New-TimeSpan -Minutes 5) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
$principal = New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel $runLevel
$reusedExisting = $false
try {
    Register-ScheduledTask -TaskName $taskName -Action $action -Trigger @($logon,$periodic) -Settings $settings -Principal $principal -Force -ErrorAction Stop | Out-Null
} catch {
    $existing = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    $periodicIso = 'PT' + $IntervalMinutes + 'M'
    $hasLogon = $false
    $hasPeriodic = $false
    if ($null -ne $existing) {
        foreach($trigger in $existing.Triggers) {
            if ($trigger.CimClass.CimClassName -eq 'MSFT_TaskLogonTrigger') { $hasLogon = $true }
            if ($null -ne $trigger.Repetition -and [string]$trigger.Repetition.Interval -eq $periodicIso) { $hasPeriodic = $true }
        }
    }
    $actionMatches = $null -ne $existing -and $existing.Actions.Count -eq 1 -and [string]$existing.Actions[0].Execute -ieq $powershell -and [string]$existing.Actions[0].Arguments -eq $arguments
    $principalMatches = $null -ne $existing -and [string]$existing.Principal.RunLevel -eq $runLevel -and ([string]$existing.Principal.UserId -eq $identity -or [string]$existing.Principal.UserId -eq $env:USERNAME)
    $settingsMatch = $null -ne $existing -and -not $existing.Settings.DisallowStartIfOnBatteries -and -not $existing.Settings.StopIfGoingOnBatteries -and [string]$existing.Settings.MultipleInstances -eq 'IgnoreNew' -and [string]$existing.Settings.ExecutionTimeLimit -eq 'PT5M'
    if (-not ($actionMatches -and $principalMatches -and $settingsMatch -and $hasLogon -and $hasPeriodic)) { throw }
    $reusedExisting = $true
}

[ordered]@{ task=$taskName; interval_minutes=$IntervalMinutes; user=$identity; run_level=$runLevel.ToLowerInvariant(); recovery_script=$recoveryScript; reused_existing=$reusedExisting } | ConvertTo-Json
