# Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param([string]$RuntimeInstallDir = (Join-Path $env:LOCALAPPDATA 'RuntimeCore'))

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Security
$RuntimeInstallDir = [IO.Path]::GetFullPath($RuntimeInstallDir)
$install = Get-Content -LiteralPath (Join-Path $RuntimeInstallDir 'install.json') -Raw | ConvertFrom-Json
$public = Get-Content -LiteralPath (Join-Path $RuntimeInstallDir 'public-connection.json') -Raw | ConvertFrom-Json
$secrets = Join-Path $RuntimeInstallDir 'secrets'
$stateDir = Join-Path $RuntimeInstallDir 'public-edge'
New-Item -ItemType Directory -Force $stateDir | Out-Null

function Read-RuntimeSecret([string]$Name, [string]$Entropy) {
    $path = Join-Path $secrets $Name
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "Missing Runtime secret: $Name" }
    $encoded = [IO.File]::ReadAllText($path, [Text.Encoding]::UTF8).Trim()
    $plain = [Security.Cryptography.ProtectedData]::Unprotect(
        [Convert]::FromBase64String($encoded),
        [Text.Encoding]::UTF8.GetBytes($Entropy),
        [Security.Cryptography.DataProtectionScope]::CurrentUser)
    try { return [Text.Encoding]::UTF8.GetString($plain) }
    finally { [Array]::Clear($plain, 0, $plain.Length) }
}

$env:AGENTDOCK_HOME = [string]$install.runtime_home
$env:AGENTDOCK_DEFAULT_DIR = [string]$install.project_root
$env:AGENTDOCK_ACP_ENABLED = 'false'
$env:AGENTDOCK_BROWSER_ENABLED = 'true'
$env:AGENTDOCK_HOST = '127.0.0.1'
$env:AGENTDOCK_PORT = ([Uri]$public.local_origin).Port.ToString()
$env:AGENTDOCK_STDIO = 'false'
$env:AGENTDOCK_AUTH_TOKEN = Read-RuntimeSecret 'auth-token.dpapi' 'runtime.auth.bearer.v1'
$env:AGENTDOCK_OAUTH_ENABLED = 'true'
$env:AGENTDOCK_SERVER_URL = [string]$public.public_origin
$env:AGENTDOCK_OAUTH_PASSWORD = Read-RuntimeSecret 'oauth-password.dpapi' 'runtime.oauth.password.v1'
$env:AGENTDOCK_OAUTH_TOKEN_SECRET = Read-RuntimeSecret 'oauth-token-secret.dpapi' 'runtime.oauth.secret.v1'

$binary = [string]$install.binary
if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) { throw "Runtime binary missing: $binary" }
$process = Start-Process -FilePath $binary -PassThru -NoNewWindow
[IO.File]::WriteAllText((Join-Path $stateDir 'runtime-http.pid'), $process.Id.ToString(), [Text.UTF8Encoding]::new($false))
try { $process.WaitForExit(); exit $process.ExitCode }
finally {
    $pidPath = Join-Path $stateDir 'runtime-http.pid'
    try {
        if (([IO.File]::ReadAllText($pidPath).Trim()) -eq $process.Id.ToString()) {
            Remove-Item -LiteralPath $pidPath -Force -ErrorAction SilentlyContinue
        }
    } catch {}
    foreach($key in 'AGENTDOCK_AUTH_TOKEN','AGENTDOCK_OAUTH_PASSWORD','AGENTDOCK_OAUTH_TOKEN_SECRET') { Remove-Item "Env:$key" -ErrorAction SilentlyContinue }
}
