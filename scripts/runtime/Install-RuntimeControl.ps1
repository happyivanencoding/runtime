# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Binary,
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore\control'),
    [switch]$NoDesktopShortcut
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
    }
    finally {
        [Runtime.InteropServices.Marshal]::FinalReleaseComObject($shell) | Out-Null
    }
}

$startMenu = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\Runtime\Runtime Control.lnk'
New-RuntimeShortcut -Path $startMenu

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
    startup_enabled = $false
    admin_required = $false
} | ConvertTo-Json
