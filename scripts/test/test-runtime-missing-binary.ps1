$ErrorActionPreference = 'Stop'
$repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$testRoot = Join-Path ([IO.Path]::GetTempPath()) ('runtime-missing-binary-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $testRoot, (Join-Path $testRoot 'scripts'), (Join-Path $testRoot 'secrets') | Out-Null
try {
    foreach($name in 'Start-RuntimeForChatGPT.ps1','Status-RuntimeForChatGPT.ps1','Recover-RuntimeForChatGPT.ps1','Run-RuntimePublic.ps1','Run-RuntimeCloudflared.ps1') {
        Copy-Item -LiteralPath (Join-Path $repo "scripts\runtime\$name") -Destination (Join-Path $testRoot 'scripts')
    }
    foreach($name in 'auth-token.dpapi','oauth-password.dpapi','oauth-token-secret.dpapi','cloudflared-token.dpapi') {
        Set-Content -LiteralPath (Join-Path $testRoot "secrets\$name") -Value 'not-a-real-secret'
    }
    @{binary=(Join-Path $testRoot 'bin\missing.exe')} | ConvertTo-Json | Set-Content (Join-Path $testRoot 'install.json')
    @{status='connected';local_origin='http://127.0.0.1:1';public_origin='https://invalid.invalid';updated_at='';execution_privilege='standard'} | ConvertTo-Json | Set-Content (Join-Path $testRoot 'public-connection.json')
    $live = (& (Join-Path $testRoot 'scripts\Status-RuntimeForChatGPT.ps1') -RuntimeInstallDir $testRoot) | ConvertFrom-Json
    if ($live.status -ne 'repair_required' -or $live.failure_reason -ne 'runtime_binary_missing') { throw 'Missing binary was not diagnosed.' }
    $recovery = (& (Join-Path $testRoot 'scripts\Recover-RuntimeForChatGPT.ps1') -RuntimeInstallDir $testRoot) | ConvertFrom-Json
    if ($recovery.action -ne 'none' -or $recovery.status -ne 'repair_required') { throw 'Recovery must not launch a missing executable.' }
    $failedAsExpected = $false
    try { & (Join-Path $testRoot 'scripts\Start-RuntimeForChatGPT.ps1') -RuntimeInstallDir $testRoot | Out-Null }
    catch { $failedAsExpected = $_.Exception.Message -like 'Runtime binary is missing.*' }
    if (-not $failedAsExpected) { throw 'Start did not fail at the binary preflight.' }
    if ((Get-Content (Join-Path $testRoot 'public-connection.json') -Raw | ConvertFrom-Json).status -ne 'repair_required') { throw 'Missing binary status was not persisted.' }
    if (Get-ChildItem (Join-Path $testRoot 'public-edge') -Filter '*.pid' -ErrorAction SilentlyContinue) { throw 'Unexpected process owner file.' }
    'PASS: missing binary is reported immediately; recovery launches nothing; Start fails before decrypting credentials.'
} finally {
    $resolved = [IO.Path]::GetFullPath($testRoot)
    $tempBase = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\') + '\'
    if (-not $resolved.StartsWith($tempBase,[StringComparison]::OrdinalIgnoreCase)) { throw 'Refusing cleanup outside the test temp directory.' }
    Remove-Item -LiteralPath $resolved -Recurse -Force
}
