param(
    [string]$RunDir = (Join-Path $HOME "g-claw")
)

$ErrorActionPreference = "Stop"

$ExePath = Join-Path $HOME "bin\glaw.exe"
$MailFilterPath = Join-Path $RunDir "config\mail.list"
$CronConfigPath = "cron.json"
$LogDir = Join-Path $RunDir "logs"
$LogPath = Join-Path $LogDir "start-shuyao.log"
$StdoutPath = Join-Path $LogDir "glaw-stdout.log"
$StderrPath = Join-Path $LogDir "glaw-stderr.log"

function Write-Log {
    param([string]$Message)
    $line = "[{0}] {1}" -f (Get-Date -Format o), $Message
    Add-Content -LiteralPath $LogPath -Value $line -Encoding UTF8
}

New-Item -ItemType Directory -Force -Path $LogDir | Out-Null

Write-Log "Starting shuyao"
Write-Log "RunDir=$RunDir"
Write-Log "ExePath=$ExePath"
Write-Log "MailFilterPath=$MailFilterPath"
Write-Log "StdoutPath=$StdoutPath"
Write-Log "StderrPath=$StderrPath"

if (!(Test-Path -LiteralPath $RunDir)) {
    throw "Missing RunDir: $RunDir"
}
if (!(Test-Path -LiteralPath $ExePath)) {
    throw "Missing ExePath: $ExePath"
}
if (!(Test-Path -LiteralPath $MailFilterPath)) {
    throw "Missing MailFilterPath: $MailFilterPath"
}
if (!(Test-Path -LiteralPath (Join-Path $RunDir $CronConfigPath))) {
    throw "Missing CronConfigPath under RunDir: $CronConfigPath"
}

Push-Location $RunDir
try {
    $proc = Start-Process -FilePath $ExePath `
        -ArgumentList @(
            "serve",
            "--mail-filter", $MailFilterPath,
            "--cron-config", $CronConfigPath
        ) `
        -WorkingDirectory $RunDir `
        -WindowStyle Hidden `
        -RedirectStandardOutput $StdoutPath `
        -RedirectStandardError $StderrPath `
        -PassThru
} finally {
    Pop-Location
}

Write-Log "Started PID $($proc.Id)"
