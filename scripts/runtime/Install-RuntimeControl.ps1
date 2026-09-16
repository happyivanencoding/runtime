# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Binary,
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore\control'),
    [switch]$NoDesktopShortcut,
    [switch]$RunAsAdministrator
)

$ErrorActionPreference = 'Stop'
$source = (Resolve-Path -LiteralPath $Binary).Path
if ([IO.Path]::GetExtension($source) -ine '.exe') {
    throw 'Runtime Control installer expects a Windows .exe.'
}

$InstallDir = [IO.Path]::GetFullPath($InstallDir)
$target = Join-Path $InstallDir 'Runtime.Control.exe'
New-Item -ItemType Directory -Force $InstallDir | Out-Null

$running = Get-Process -Name 'Runtime.Control' -ErrorAction SilentlyContinue | Where-Object {
    try { $_.Path -ieq $target } catch { $false }
}
if ($running) {
    throw 'The installed Runtime Control is running. Exit it from the tray menu, then rerun this installer.'
}

if ($source -ine $target) {
    Copy-Item -LiteralPath $source -Destination $target -Force
}

function New-RuntimeShortcut {
    param([Parameter(Mandatory = $true)][string]$Path)
    $directory = Split-Path -Parent $Path
    New-Item -ItemType Directory -Force $directory | Out-Null
    $shell = New-Object -ComObject WScript.Shell
    try {
        $shortcut = $shell.CreateShortcut($Path)
        $shortcut.TargetPath = $target
        $shortcut.WorkingDirectory = $InstallDir
        $shortcut.Description = 'Runtime local control center'
        $shortcut.Save()
        if ($RunAsAdministrator) {
            # Shell Link LinkFlags: SLDF_RUNAS_USER (0x2000). Keep other flags intact.
            $bytes = [IO.File]::ReadAllBytes($Path)
            $flags = [BitConverter]::ToUInt32($bytes, 0x14) -bor 0x2000
            [BitConverter]::GetBytes([uint32]$flags).CopyTo($bytes, 0x14)
            [IO.File]::WriteAllBytes($Path, $bytes)
        }
    }
    finally {
        [Runtime.InteropServices.Marshal]::FinalReleaseComObject($shell) | Out-Null
    }
}

$startMenu = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\Runtime\Runtime Control.lnk'
New-RuntimeShortcut -Path $startMenu

$administratorSessionShortcut = ''
if ($RunAsAdministrator) {
    $launcher = Join-Path (Split-Path -Parent $InstallDir) 'scripts\Start-RuntimeAdministratorSession.ps1'
    if (Test-Path -LiteralPath $launcher) {
        $administratorSessionShortcut = Join-Path (Split-Path -Parent $startMenu) 'Runtime Administrator Session.lnk'
        $shell = New-Object -ComObject WScript.Shell
        try {
            $shortcut = $shell.CreateShortcut($administratorSessionShortcut)
            $shortcut.TargetPath = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
            $shortcut.Arguments = '-NoProfile -ExecutionPolicy Bypass -File "' + $launcher + '"'
            $shortcut.WorkingDirectory = Split-Path -Parent $InstallDir
            $shortcut.WindowStyle = 7
            $shortcut.Description = 'Start Runtime and Chrome as administrator after exiting Chrome; requires UAC confirmation.'
            $shortcut.Save()
        }
        finally { [Runtime.InteropServices.Marshal]::FinalReleaseComObject($shell) | Out-Null }
    }
}

$desktopShortcut = ''
if (-not $NoDesktopShortcut) {
    $desktop = [Environment]::GetFolderPath([Environment+SpecialFolder]::DesktopDirectory)
    if (-not [string]::IsNullOrWhiteSpace($desktop)) {
        $desktopShortcut = Join-Path $desktop 'Runtime Control.lnk'
        New-RuntimeShortcut -Path $desktopShortcut
    }
}

[ordered]@{
    installed_binary = $target
    start_menu_shortcut = $startMenu
    desktop_shortcut = $desktopShortcut
    administrator_session_shortcut = $administratorSessionShortcut
    startup_enabled = $false
    admin_required = $false
    shortcut_run_as_administrator = [bool]$RunAsAdministrator
} | ConvertTo-Json
