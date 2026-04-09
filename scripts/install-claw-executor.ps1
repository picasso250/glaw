param(
    [string]$RepoDir = (Join-Path $HOME "glaw"),
    [string]$RunDir = (Join-Path $HOME "claw-executor"),
    [string]$WorkerUrl = "https://file.io99.xyz",
    [string]$AgentId = "claw-executor",
    [string]$TokenSource = (Join-Path $HOME ".glaw-executor-token.txt"),
    [string]$ResultPath = (Join-Path (Get-Location) "install-claw-executor-result.txt")
)

$ErrorActionPreference = "Stop"

function Write-Step {
    param([string]$Message)
    Write-Host "[claw-executor-install] $Message" -ForegroundColor Cyan
}

function Require-Path {
    param(
        [string]$Path,
        [string]$Label
    )

    if (!(Test-Path -LiteralPath $Path)) {
        throw "Missing ${Label}: $Path"
    }
}

$SourceExecutorScript = Join-Path $RepoDir "scripts\claw_executor.py"
$SourceStartScript = Join-Path $RepoDir "scripts\start-claw-executor.ps1"
$TargetExecutorScript = Join-Path $RunDir "claw_executor.py"
$TargetStartScript = Join-Path $RunDir "start-claw-executor.ps1"
$TargetTokenFile = Join-Path $RunDir "glaw-executor-token.txt"
$StartLogPath = Join-Path $RunDir "claw-executor-start.txt"
$RuntimeLogPath = Join-Path $RunDir "claw-executor-runtime.log"

Require-Path -Path $RepoDir -Label "repo dir"
Require-Path -Path (Join-Path $RepoDir ".git") -Label "repo .git"
Require-Path -Path $SourceExecutorScript -Label "source executor script"
Require-Path -Path $SourceStartScript -Label "source start script"

Write-Step "Running git pull in $RepoDir"
Push-Location $RepoDir
try {
    & git pull
    if ($LASTEXITCODE -ne 0) {
        throw "git pull failed with exit code $LASTEXITCODE"
    }
} finally {
    Pop-Location
}

Write-Step "Preparing run dir $RunDir"
New-Item -ItemType Directory -Force -Path $RunDir | Out-Null

Write-Step "Copying latest executor files"
Copy-Item -LiteralPath $SourceExecutorScript -Destination $TargetExecutorScript -Force
Copy-Item -LiteralPath $SourceStartScript -Destination $TargetStartScript -Force

if (!(Test-Path -LiteralPath $TargetTokenFile)) {
    if (Test-Path -LiteralPath $TokenSource) {
        Write-Step "Copying token file into run dir"
        Copy-Item -LiteralPath $TokenSource -Destination $TargetTokenFile -Force
    } else {
        throw "Missing token file in both $TargetTokenFile and $TokenSource"
    }
}

$resultLines = @(
    "StartedAt: $(Get-Date -Format o)",
    "RepoDir: $RepoDir",
    "RunDir: $RunDir",
    "WorkerUrl: $WorkerUrl",
    "AgentId: $AgentId",
    "ExecutorScript: $TargetExecutorScript",
    "StartScript: $TargetStartScript",
    "TokenFile: $TargetTokenFile",
    "StartLogPath: $StartLogPath",
    "StartLogExists: $(Test-Path -LiteralPath $StartLogPath)",
    "RuntimeLogPath: $RuntimeLogPath",
    "RuntimeLogExists: $(Test-Path -LiteralPath $RuntimeLogPath)"
)

if (Test-Path -LiteralPath $StartLogPath) {
    $resultLines += ""
    $resultLines += "== claw-executor-start.txt =="
    $resultLines += Get-Content -LiteralPath $StartLogPath
}

$resultLines | Set-Content -LiteralPath $ResultPath -Encoding UTF8

Write-Host "install result: $ResultPath"
Write-Host "start log: $StartLogPath"
