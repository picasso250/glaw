$ErrorActionPreference = "Stop"

$RepoDir = Join-Path $HOME "glaw"
$RunDir = Join-Path $HOME "claw-life-saver"
$DestExePath = Join-Path $HOME "bin\claw-life-saver.exe"
$EnvPath = Join-Path $RunDir ".env"
$MailFilterPath = Join-Path $RunDir "mail_filter_senders.txt"
$CronConfigPath = Join-Path $RunDir "cron.json"
$LogDir = Join-Path $RunDir "logs"
$StdoutLogPath = Join-Path $LogDir "claw-life-saver-stdout.log"
$StderrLogPath = Join-Path $LogDir "claw-life-saver-stderr.log"
$UpgradeLogPath = Join-Path $LogDir "upgrade-claw-life-saver.log"
$StartScriptPath = Join-Path $RunDir "start-claw-life-saver.ps1"

function Write-Log {
    param([string]$Message)
    $line = "[{0}] {1}" -f (Get-Date -Format o), $Message
    Write-Host $line
    Add-Content -LiteralPath $UpgradeLogPath -Value $line -Encoding UTF8
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

New-Item -ItemType Directory -Force -Path $LogDir | Out-Null
Write-Log "Starting claw-life-saver upgrade"
Write-Log "RepoDir=$RepoDir"
Write-Log "RunDir=$RunDir"
Write-Log "DestExePath=$DestExePath"

Require-Path -Path $RepoDir -Label "repo dir"
Require-Path -Path (Join-Path $RepoDir ".git") -Label "repo .git"
Require-Path -Path $RunDir -Label "run dir"
Require-Path -Path $EnvPath -Label ".env"
Require-Path -Path $MailFilterPath -Label "mail_filter_senders.txt"
Require-Path -Path $CronConfigPath -Label "cron.json"

$startScript = @"
`$ErrorActionPreference = "Stop"
`$RunDir = "$RunDir"
`$ExePath = "$DestExePath"
`$EnvPath = "$EnvPath"
`$MailFilterPath = "$MailFilterPath"
`$CronConfigPath = "$CronConfigPath"
`$LogDir = "$LogDir"
`$StdoutLogPath = "$StdoutLogPath"
`$StderrLogPath = "$StderrLogPath"
New-Item -ItemType Directory -Force -Path `$LogDir | Out-Null
Set-Location `$RunDir
& `$ExePath serve --env `$EnvPath --mail-filter `$MailFilterPath --cron-config `$CronConfigPath --exec-subject-keyword claw-life-saver 1>> `$StdoutLogPath 2>> `$StderrLogPath
"@
Set-Content -LiteralPath $StartScriptPath -Value $startScript -Encoding UTF8
Write-Log "Wrote detached start script to $StartScriptPath"

Write-Log "Sleeping 5 seconds before stop"
Start-Sleep -Seconds 5

$targetProcs = @(Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -eq $DestExePath })
Write-Log "Found $($targetProcs.Count) matching claw-life-saver.exe process(es)"
foreach ($proc in $targetProcs) {
    Write-Log ("Stopping PID={0} CommandLine={1}" -f $proc.ProcessId, $proc.CommandLine)
    Stop-Process -Id $proc.ProcessId -Force
}

Write-Log "Sleeping 5 seconds after stop"
Start-Sleep -Seconds 5

$restartedProc = @(Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -eq $DestExePath })
if ($restartedProc.Count -gt 0) {
    $details = $restartedProc | Select-Object ProcessId, Name, ExecutablePath, CommandLine | Format-List | Out-String
    throw "claw-life-saver.exe was observed again before replace:`n$details"
}
Write-Log "Confirmed no claw-life-saver.exe process is running"

Push-Location $RepoDir
try {
    Write-Log "Running git pull in $RepoDir"
    & git pull
    if ($LASTEXITCODE -ne 0) {
        throw "git pull failed with exit code $LASTEXITCODE"
    }

    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $DestExePath) | Out-Null

    Write-Log "Building $DestExePath"
    & go build -buildvcs=false -o $DestExePath .\cmd\glaw
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }
} finally {
    Pop-Location
}

Write-Log "Launching detached starter with ExecutionPolicy Bypass"
$starter = Get-Command pwsh -ErrorAction SilentlyContinue
if ($null -eq $starter) {
    $starter = Get-Command powershell -ErrorAction Stop
}
Start-Process -FilePath $starter.Source `
    -ArgumentList @("-NoProfile", "-ExecutionPolicy", "Bypass", "-File", $StartScriptPath) `
    -WorkingDirectory $RunDir `
    -WindowStyle Hidden

Write-Log "Upgrade script completed"
