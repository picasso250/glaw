param(
    [string]$RunDir = (Join-Path $HOME "claw-executor"),
    [string]$WorkerUrl = "https://file.io99.xyz",
    [string]$AgentId = "claw-executor"
)

$ErrorActionPreference = "Stop"

$TokenFile = Join-Path $RunDir "glaw-executor-token.txt"
$ExecutorScript = Join-Path $RunDir "claw_executor.py"
$StartLogPath = Join-Path $RunDir "claw-executor-start.txt"
$RuntimeLogPath = Join-Path $RunDir "claw-executor-runtime.log"

New-Item -ItemType Directory -Force -Path $RunDir | Out-Null
Set-Location $RunDir

if (!(Test-Path -LiteralPath $TokenFile)) {
    throw "Missing token file: $TokenFile"
}
if (!(Test-Path -LiteralPath $ExecutorScript)) {
    throw "Missing executor script: $ExecutorScript"
}

$env:EXECUTOR_TOKEN = (Get-Content -LiteralPath $TokenFile -Raw).Trim()
if ([string]::IsNullOrWhiteSpace($env:EXECUTOR_TOKEN)) {
    throw "EXECUTOR_TOKEN is empty"
}

$pidLine = "PID: $PID"

@(
    "StartedAt: $(Get-Date -Format o)"
    "PWD: $((Get-Location).Path)"
    "ExecutorScript: $ExecutorScript"
    "WorkerURL: $WorkerUrl"
    "AgentId: $AgentId"
    "RuntimeLog: $RuntimeLogPath"
    $pidLine
) | Set-Content -LiteralPath $StartLogPath -Encoding UTF8

Write-Host "===== claw-executor start ====="
Get-Content -LiteralPath $StartLogPath
Write-Host ""
Write-Host "===== claw-executor runtime ====="
Write-Host "logging to: $RuntimeLogPath"
python -u $ExecutorScript --worker-url $WorkerUrl --agent-id $AgentId 2>&1 | Tee-Object -FilePath $RuntimeLogPath -Append

Write-Host "start log: $StartLogPath"
