$ErrorActionPreference = "Stop"

$UpgradeScriptPath = Join-Path $HOME "claw-life-saver\upgrade-claw-life-saver.ps1"

if (!(Test-Path -LiteralPath $UpgradeScriptPath)) {
    throw "Missing upgrade script: $UpgradeScriptPath"
}

$starter = Get-Command pwsh -ErrorAction SilentlyContinue
if ($null -eq $starter) {
    $starter = Get-Command powershell -ErrorAction Stop
}

Write-Host "Launching detached upgrade script: $UpgradeScriptPath"
Start-Process -FilePath $starter.Source `
    -ArgumentList @("-NoProfile", "-ExecutionPolicy", "Bypass", "-File", $UpgradeScriptPath) `
    -WorkingDirectory (Split-Path -Parent $UpgradeScriptPath) `
    -WindowStyle Hidden

Write-Host "Detached launch requested"
