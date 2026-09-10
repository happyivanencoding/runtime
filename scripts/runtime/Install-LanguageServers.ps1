# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: Apache-2.0
[CmdletBinding()]
param(
    [string]$RuntimeHome,
    [string]$GoExecutable = 'go',
    [string]$NodeExecutable = 'node'
)
$ErrorActionPreference = 'Stop'
if (-not $RuntimeHome) {
    $settings = Join-Path (Split-Path $PSScriptRoot -Parent) 'install.json'
    if (-not (Test-Path -LiteralPath $settings)) { throw 'Pass -RuntimeHome or run this script from an installed RuntimeCore directory.' }
    $RuntimeHome = (Get-Content -LiteralPath $settings -Raw | ConvertFrom-Json).runtime_home
}
$RuntimeHome = [IO.Path]::GetFullPath($RuntimeHome)
$configPath = Join-Path $RuntimeHome 'lsp-servers.json'
if (Test-Path -LiteralPath $configPath) {
    throw "Preserved existing configuration: $configPath. This first-install script does not replace your selected servers; update them deliberately."
}
$go = (Get-Command $GoExecutable -ErrorAction Stop).Source
$node = (Get-Command $NodeExecutable -ErrorAction Stop).Source
$npm = Join-Path (Split-Path $node -Parent) 'npm.cmd'
if (-not (Test-Path -LiteralPath $npm)) { throw 'npm.cmd must be installed beside Node.js.' }
$nodeVersion = (& $node --version).TrimStart('v')
if ($LASTEXITCODE -ne 0 -or [version]$nodeVersion -lt [version]'22.22.2') { throw 'typescript-language-server 6.0.0 requires Node.js >=22.22.2.' }
$goDir = Join-Path $RuntimeHome 'tools\go'
$nodeDir = Join-Path $RuntimeHome 'tools\node'
New-Item -ItemType Directory -Force $goDir, $nodeDir | Out-Null
$oldGoBin = $env:GOBIN
$oldPath = $env:PATH
try {
    $env:GOBIN = $goDir
    $env:PATH = (Split-Path $go -Parent) + ';' + $env:PATH
    & $go install golang.org/x/tools/gopls@v0.23.0
    if ($LASTEXITCODE -ne 0) { throw 'gopls installation failed.' }
    & $npm install --prefix $nodeDir --save-exact typescript-language-server@6.0.0 typescript@5.9.3 pyright@1.1.414
    if ($LASTEXITCODE -ne 0) { throw 'Node language-server installation failed.' }
} finally {
    $env:GOBIN = $oldGoBin
    $env:PATH = $oldPath
}
$modules = Join-Path $nodeDir 'node_modules'
$config = [ordered]@{
    go = @{
        command = (Join-Path $goDir 'gopls.exe')
        args = @('serve')
        env = @{ PATH = ((Split-Path $go -Parent) + ';${PATH}') }
    }
    typescript = @{
        command = $node
        args = @((Join-Path $modules 'typescript-language-server\lib\cli.mjs'), '--stdio')
        initialization_options = @{ tsserver = @{ path = (Join-Path $modules 'typescript\lib\tsserver.js') } }
    }
    python = @{
        command = $node
        args = @((Join-Path $modules 'pyright\langserver.index.js'), '--stdio')
        settings = @{ python = @{ analysis = @{ diagnosticMode = 'openFilesOnly' } } }
    }
}
[IO.File]::WriteAllText($configPath, ($config | ConvertTo-Json -Depth 10), [Text.UTF8Encoding]::new($false))
Write-Output "Installed Go/TypeScript/Python language servers. Configuration: $configPath"
Write-Output 'TypeScript analysis uses the pinned 5.9.3 SDK, not your project compiler; still run project-native validation.'
