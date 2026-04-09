param(
    [string]$RepoDir = (Join-Path $HOME "glaw"),
    [string]$RunDir = (Join-Path $HOME "g-claw"),
    [string]$WorkerUrl = "https://remote-executor.io99.xyz",
    [string]$HostName = $env:COMPUTERNAME,
    [string]$TokenFile = (Join-Path $HOME ".glaw-log-observer-token.txt"),
    [string]$ResultPath = (Join-Path (Get-Location) "install-remote-log-uploader-result.txt"),
    [switch]$SkipGitPull
)

$ErrorActionPreference = "Stop"

function Require-Path {
    param(
        [string]$Path,
        [string]$Label
    )

    if (!(Test-Path -LiteralPath $Path)) {
        throw "Missing ${Label}: $Path"
    }
}

$SourceUploadScript = Join-Path $RepoDir "scripts\upload_remote_logs.py"
$TargetScriptDir = Join-Path $RunDir "scripts"
$TargetUploadScript = Join-Path $TargetScriptDir "upload_remote_logs.py"
$CronPath = Join-Path $RunDir "cron.json"
$TmpDir = Join-Path $RunDir "tmp"

Require-Path -Path $RepoDir -Label "repo dir"
Require-Path -Path (Join-Path $RepoDir ".git") -Label "repo .git"
Require-Path -Path $SourceUploadScript -Label "upload script"
Require-Path -Path $CronPath -Label "cron.json"
Require-Path -Path $TokenFile -Label "token file"

if (-not $SkipGitPull) {
    Push-Location $RepoDir
    try {
        & git pull
        if ($LASTEXITCODE -ne 0) {
            throw "git pull failed with exit code $LASTEXITCODE"
        }
    } finally {
        Pop-Location
    }
}

New-Item -ItemType Directory -Force -Path $TargetScriptDir | Out-Null
New-Item -ItemType Directory -Force -Path $TmpDir | Out-Null
Copy-Item -LiteralPath $SourceUploadScript -Destination $TargetUploadScript -Force

$cronTasks = Get-Content -LiteralPath $CronPath -Raw | ConvertFrom-Json
if ($null -eq $cronTasks) {
    $cronTasks = @()
}

$filteredTasks = @()
foreach ($task in $cronTasks) {
    if ($task.name -ne "hourly-log-upload") {
        $filteredTasks += $task
    }
}

$filteredTasks += [PSCustomObject]@{
    name     = "hourly-log-upload"
    schedule = "hourly"
    command  = "python"
    args     = @(
        "scripts/upload_remote_logs.py",
        "--worker-url", $WorkerUrl,
        "--host", $HostName.ToLower(),
        "--token-file", $TokenFile
    )
    workdir  = $RunDir
}

$filteredTasks | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $CronPath -Encoding UTF8

$result = @(
    "InstalledAt: $(Get-Date -Format o)",
    "RepoDir: $RepoDir",
    "RunDir: $RunDir",
    "CronPath: $CronPath",
    "TokenFile: $TokenFile",
    "TargetUploadScript: $TargetUploadScript",
    "WorkerUrl: $WorkerUrl",
    "HostName: $($HostName.ToLower())",
    "SkipGitPull: $SkipGitPull",
    "",
    "== cron.json ==",
    (Get-Content -LiteralPath $CronPath -Raw)
)

$result | Set-Content -LiteralPath $ResultPath -Encoding UTF8
Write-Host "result: $ResultPath"
