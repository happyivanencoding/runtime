# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param(
    [string]$Dotnet = 'dotnet',
    [string]$OutputDir = ''
)

$ErrorActionPreference = 'Stop'
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$project = Join-Path $repoRoot 'desktop\windows\runtime-control\Runtime.Control.csproj'
if ([string]::IsNullOrWhiteSpace($OutputDir)) {
    $OutputDir = Join-Path (Split-Path -Parent $repoRoot) 'runtime-core-artifacts\runtime-control'
}

if (-not (Test-Path -LiteralPath $project)) {
    throw "Runtime Control project not found: $project"
}

$dotnetCommand = Get-Command $Dotnet -ErrorAction SilentlyContinue
if (-not $dotnetCommand -and (Test-Path -LiteralPath $Dotnet)) {
    $dotnetCommand = Get-Item -LiteralPath $Dotnet
}
if (-not $dotnetCommand) {
    throw 'A .NET 8 SDK is required. Pass -Dotnet with the full path to dotnet.exe.'
}

$OutputDir = [IO.Path]::GetFullPath($OutputDir)
if (Test-Path -LiteralPath $OutputDir) {
    Remove-Item -LiteralPath $OutputDir -Recurse -Force
}
New-Item -ItemType Directory -Force $OutputDir | Out-Null

$env:DOTNET_CLI_TELEMETRY_OPTOUT = '1'
& $dotnetCommand.Source publish $project `
    -c Release `
    -r win-x64 `
    --self-contained true `
    -p:PublishSingleFile=true `
    -o $OutputDir
if ($LASTEXITCODE -ne 0) {
    throw "Runtime Control publish failed with exit code $LASTEXITCODE."
}

$binary = Join-Path $OutputDir 'Runtime.Control.exe'
if (-not (Test-Path -LiteralPath $binary)) {
    throw "Published Runtime Control binary not found: $binary"
}
$iconSource = Join-Path $repoRoot 'desktop\windows\runtime-control\Assets\runtime-control.ico'
$icon = Join-Path $OutputDir 'Runtime.Control.ico'
if (-not (Test-Path -LiteralPath $iconSource -PathType Leaf)) {
    throw "Runtime Control icon not found: $iconSource"
}
Copy-Item -LiteralPath $iconSource -Destination $icon -Force

[ordered]@{
    binary = $binary
    icon = $icon
    bytes = (Get-Item -LiteralPath $binary).Length
    target = 'win-x64'
    self_contained = $true
    single_file = $true
} | ConvertTo-Json
