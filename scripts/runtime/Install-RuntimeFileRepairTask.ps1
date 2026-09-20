# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param([string]$BackupRoot = (Join-Path $env:ProgramData 'RuntimeCoreRecovery'))
$ErrorActionPreference = 'Stop'
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
if(-not ([Security.Principal.WindowsPrincipal]$identity).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)){throw 'Run this installer with UAC elevation once.'}
$BackupRoot = [IO.Path]::GetFullPath($BackupRoot)
New-Item -ItemType Directory -Path $BackupRoot -Force | Out-Null
$acl = New-Object Security.AccessControl.DirectorySecurity
$administrators = [Security.Principal.SecurityIdentifier]::new('S-1-5-32-544')
$system = [Security.Principal.SecurityIdentifier]::new('S-1-5-18')
$acl.SetOwner($administrators)
$acl.SetAccessRuleProtection($true,$false)
foreach($sid in @($administrators,$system)){$acl.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new($sid,'FullControl','ContainerInherit,ObjectInherit','None','Allow'))}
$acl.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new($identity.User,'ReadAndExecute','ContainerInherit,ObjectInherit','None','Allow'))
Set-Acl -LiteralPath $BackupRoot -AclObject $acl
foreach($name in 'Backup-RuntimeCriticalFiles.ps1','Repair-RuntimeCriticalFiles.ps1') { Copy-Item -LiteralPath (Join-Path $PSScriptRoot $name) -Destination (Join-Path $BackupRoot $name) -Force }
& (Join-Path $BackupRoot 'Backup-RuntimeCriticalFiles.ps1') -BackupRoot $BackupRoot
$powershell = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
$command = '"'+$powershell+'" -NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "'+(Join-Path $BackupRoot 'Repair-RuntimeCriticalFiles.ps1')+'" -BackupRoot "'+$BackupRoot+'"'
$launcher = Join-Path $BackupRoot 'repair-hidden.vbs'
$vbs = "Option Explicit`r`nDim shell, result`r`nSet shell = CreateObject(""WScript.Shell"")`r`nresult = shell.Run(""" + $command.Replace('"','""') + """, 0, True)`r`nWScript.Quit result`r`n"
[IO.File]::WriteAllText($launcher,$vbs,[Text.UnicodeEncoding]::new($false,$true))
& icacls.exe $BackupRoot /setowner '*S-1-5-32-544' /T /C /Q | Out-Null
if($LASTEXITCODE -ne 0){throw 'Could not protect backup ownership.'}
$action = New-ScheduledTaskAction -Execute (Join-Path $env:SystemRoot 'System32\wscript.exe') -Argument ('"'+$launcher+'"')
$triggers = @((New-ScheduledTaskTrigger -AtLogOn -User $identity.Name),(New-ScheduledTaskTrigger -Once -At (Get-Date).AddMinutes(1) -RepetitionInterval (New-TimeSpan -Minutes 2) -RepetitionDuration (New-TimeSpan -Days 3650)))
$settings = New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -StartWhenAvailable -ExecutionTimeLimit (New-TimeSpan -Minutes 5) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
$principal = New-ScheduledTaskPrincipal -UserId $identity.Name -LogonType Interactive -RunLevel Highest
Register-ScheduledTask -TaskName 'Runtime and AgentDock File Repair' -Action $action -Trigger $triggers -Settings $settings -Principal $principal -Force | Out-Null
& (Join-Path $BackupRoot 'Repair-RuntimeCriticalFiles.ps1') -BackupRoot $BackupRoot
[ordered]@{installed=$true;task='Runtime and AgentDock File Repair';interval_minutes=2;backup_root=$BackupRoot;headless=$true} | ConvertTo-Json
