# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: Apache-2.0
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Binary,
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore'),
    [string]$RuntimeHome = (Join-Path $env:USERPROFILE '.runtime-core'),
    [string]$ProjectRoot = (Get-Location).Path
)
$ErrorActionPreference = 'Stop'
$source = (Resolve-Path -LiteralPath $Binary).Path
$InstallDir = [IO.Path]::GetFullPath($InstallDir)
$settingsPath = Join-Path $InstallDir 'install.json'
if (Test-Path -LiteralPath $settingsPath) {
    $previous = Get-Content -LiteralPath $settingsPath -Raw | ConvertFrom-Json
    if (-not $PSBoundParameters.ContainsKey('RuntimeHome')) { $RuntimeHome = $previous.runtime_home }
    if (-not $PSBoundParameters.ContainsKey('ProjectRoot')) { $ProjectRoot = $previous.project_root }
}
$RuntimeHome = [IO.Path]::GetFullPath($RuntimeHome)
$ProjectRoot = (Resolve-Path -LiteralPath $ProjectRoot).Path
$officialHome = [IO.Path]::GetFullPath((Join-Path $env:USERPROFILE '.agentdock'))
if ($RuntimeHome.TrimEnd('\') -ieq $officialHome.TrimEnd('\')) {
    throw 'Use an independent Runtime home. The installed upstream process must not write the same Task state.'
}
$target = Join-Path $InstallDir 'bin\runtime-core.exe'
$running = Get-Process -Name 'runtime-core' -ErrorAction SilentlyContinue | Where-Object { $_.Path -ieq $target }
if ($running) { throw 'Runtime Core is running. Disable its Dynamic MCP entry or close its foreground process, then rerun this installer. No process was killed.' }
$versionText = & $source version --json
if ($LASTEXITCODE -ne 0) { throw 'Source Runtime binary did not return build information.' }
$version = $versionText | ConvertFrom-Json
if ($version.version -notlike '*runtime-core*') { throw 'This installer expects a runtime-core build, not the official AgentDock executable.' }
New-Item -ItemType Directory -Force (Join-Path $InstallDir 'bin'), (Join-Path $InstallDir 'scripts'), $RuntimeHome | Out-Null
if ($source -ine $target) {
    if (Test-Path -LiteralPath $target) { Copy-Item -LiteralPath $target -Destination (Join-Path $InstallDir 'bin\runtime-core.previous.exe') -Force }
    Copy-Item -LiteralPath $source -Destination $target -Force
}
foreach ($name in @(
    'Start-RuntimeCore.ps1',
    'Install-LanguageServers.ps1',
    'Import-AgentDockCredentials.ps1',
    'Initialize-RuntimePublicConnection.ps1',
    'Set-RuntimeCloudflareToken.ps1',
    'Set-RuntimePublicTunnelState.ps1',
    'Run-RuntimePublic.ps1',
    'Run-RuntimeCloudflared.ps1',
    'Start-RuntimeForChatGPT.ps1',
    'Recover-RuntimeForChatGPT.ps1',
    'Install-RuntimeRecoveryTask.ps1',
    'Collect-AgentDockToolUsage.ps1',
    'Install-RuntimeToolUsageCollector.ps1',
    'Get-RuntimeToolTimeline.ps1',
    'Start-RuntimeAdministratorSession.ps1',
    'Stop-RuntimeForChatGPT.ps1',
    'Status-RuntimeForChatGPT.ps1'
)) {
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot $name) -Destination (Join-Path $InstallDir 'scripts') -Force
}
$sourceRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$extensionId = 'agidgjchdiodbkkaggifpflepjgoedff'
$extensionSource = Join-Path $sourceRoot 'extensions\runtime-chrome'
$extensionTarget = Join-Path $InstallDir 'chrome-extension'
if (-not (Test-Path -LiteralPath (Join-Path $extensionSource 'manifest.json'))) {
    throw "Runtime Chrome Bridge source is missing: $extensionSource"
}
New-Item -ItemType Directory -Force $extensionTarget | Out-Null
Get-ChildItem -LiteralPath $extensionTarget -Force -ErrorAction SilentlyContinue | Remove-Item -Recurse -Force
Copy-Item -Path (Join-Path $extensionSource '*') -Destination $extensionTarget -Recurse -Force

$nativeHostManifest = Join-Path $InstallDir 'chrome-native-host.json'
$nativeHost = [ordered]@{
    name = 'com.runtime.browser_bridge'
    description = 'Runtime Chrome Bridge native messaging host'
    path = $target
    type = 'stdio'
    allowed_origins = @("chrome-extension://$extensionId/")
}
[IO.File]::WriteAllText($nativeHostManifest, ($nativeHost | ConvertTo-Json -Depth 5), [Text.UTF8Encoding]::new($false))
$nativeHostRegistry = 'HKCU:\Software\Google\Chrome\NativeMessagingHosts\com.runtime.browser_bridge'
New-Item -Path $nativeHostRegistry -Force | Out-Null
Set-Item -Path $nativeHostRegistry -Value $nativeHostManifest

foreach ($name in @('LICENSE', 'NOTICE')) {
    $path = Join-Path $sourceRoot $name
    if (Test-Path -LiteralPath $path) { Copy-Item -LiteralPath $path -Destination $InstallDir -Force }
}
$settings = [ordered]@{
    runtime_home = $RuntimeHome
    project_root = $ProjectRoot
    binary = $target
    chrome_extension_path = $extensionTarget
    chrome_extension_id = $extensionId
    chrome_native_host_manifest = $nativeHostManifest
    build = $version
}
[IO.File]::WriteAllText($settingsPath, ($settings | ConvertTo-Json -Depth 8), [Text.UTF8Encoding]::new($false))
[ordered]@{
    installed_binary = $target
    runtime_home = $RuntimeHome
    version = $version
    stdio_launcher = (Join-Path $InstallDir 'scripts\Start-RuntimeCore.ps1')
    chrome_extension_path = $extensionTarget
    chrome_extension_id = $extensionId
    chrome_native_host_registered = $true
    service_installed = $false
    official_agentdock_modified = $false
} | ConvertTo-Json -Depth 8
