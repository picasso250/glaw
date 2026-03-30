$ErrorActionPreference = "Stop"

function Write-Step {
    param([string]$Message)
    Write-Host "[claw-life-saver] $Message" -ForegroundColor Cyan
}

function Write-Ok {
    param([string]$Message)
    Write-Host "[ok] $Message" -ForegroundColor Green
}

function Load-EnvMap {
    param([string]$Path)

    $result = @{}
    foreach ($rawLine in Get-Content -LiteralPath $Path) {
        $line = $rawLine.Trim()
        if ($line -eq "" -or $line.StartsWith("#") -or -not $line.Contains("=")) {
            continue
        }
        $parts = $line.Split("=", 2)
        $key = $parts[0].Trim()
        $value = $parts[1].Trim()
        $result[$key] = $value
    }
    return $result
}

$HomeDir = [Environment]::GetFolderPath("UserProfile")
$RepoDir = Join-Path $HomeDir "glaw"
$RootEnv = Join-Path $HomeDir ".env"
$RunDir = Join-Path $HomeDir "claw-life-saver"
$BinDir = Join-Path $HomeDir "bin"
$TargetExe = Join-Path $BinDir "glaw-life-saver.exe"
$EnvPath = Join-Path $RunDir ".env"
$MailFilterPath = Join-Path $RunDir "mail_filter_senders.txt"
$CronConfigPath = Join-Path $RunDir "cron.json"
$InitPath = Join-Path $RunDir "INIT.md"
$StartScriptPath = Join-Path $RunDir "start-claw-life-saver.ps1"
$LogsDir = Join-Path $RunDir "logs"
$GatewayDir = Join-Path $RunDir "gateway"

Write-Step "Preparing rescue workspace under $RunDir"

if (!(Test-Path $RepoDir)) {
    throw "Missing repo directory: $RepoDir"
}
if (!(Test-Path (Join-Path $RepoDir ".git"))) {
    throw "Missing git metadata in repo: $RepoDir"
}
if (!(Test-Path $RootEnv)) {
    throw "Missing source env file: $RootEnv"
}

$rootEnvValues = Load-EnvMap -Path $RootEnv
foreach ($requiredKey in @("MAIL_USER", "MAIL_PASS", "MAIL_IMAP_SERVER", "AGENT_CMD")) {
    if (-not $rootEnvValues.ContainsKey($requiredKey) -or [string]::IsNullOrWhiteSpace($rootEnvValues[$requiredKey])) {
        throw "Required key missing in ${RootEnv}: $requiredKey"
    }
}

New-Item -ItemType Directory -Force -Path $RunDir, $BinDir, $LogsDir, $GatewayDir | Out-Null
Write-Ok "Directories ready"

Write-Step "Running git pull in $RepoDir"
Push-Location $RepoDir
try {
    & git pull
    if ($LASTEXITCODE -ne 0) {
        throw "git pull failed with exit code $LASTEXITCODE"
    }

    Write-Step "Building $TargetExe"
    & go build -buildvcs=false -o $TargetExe .\cmd\glaw
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }
} finally {
    Pop-Location
}
Write-Ok "Built $TargetExe"

$envLines = @(
    "MAIL_USER=$($rootEnvValues["MAIL_USER"])",
    "MAIL_PASS=$($rootEnvValues["MAIL_PASS"])",
    "MAIL_IMAP_SERVER=$($rootEnvValues["MAIL_IMAP_SERVER"])",
    "AGENT_CMD=$($rootEnvValues["AGENT_CMD"])"
)
Set-Content -LiteralPath $EnvPath -Value $envLines -Encoding UTF8
Write-Ok "Wrote $EnvPath"

Set-Content -LiteralPath $MailFilterPath -Value "xi_aochi@163.com" -Encoding UTF8
Write-Ok "Wrote $MailFilterPath"

Set-Content -LiteralPath $CronConfigPath -Value "[]" -Encoding UTF8
Write-Ok "Wrote $CronConfigPath"

Set-Content -LiteralPath $InitPath -Value "Be cautious, verify assumptions, and prefer reversible actions." -Encoding UTF8
Write-Ok "Wrote $InitPath"

$startScript = @'
$ErrorActionPreference = "Stop"

$HomeDir = [Environment]::GetFolderPath("UserProfile")
$RunDir = Join-Path $HomeDir "claw-life-saver"
$ExePath = Join-Path $HomeDir "bin\glaw-life-saver.exe"
$EnvPath = Join-Path $RunDir ".env"
$MailFilterPath = Join-Path $RunDir "mail_filter_senders.txt"
$CronConfigPath = Join-Path $RunDir "cron.json"

if (!(Test-Path $ExePath)) {
    throw "Missing executable: $ExePath"
}

Set-Location $RunDir
& $ExePath serve --env $EnvPath --mail-filter $MailFilterPath --cron-config $CronConfigPath
'@
Set-Content -LiteralPath $StartScriptPath -Value $startScript -Encoding UTF8
Write-Ok "Wrote $StartScriptPath"

Write-Step "Starting rescue instance in a new PowerShell window"
$proc = Start-Process -FilePath "pwsh" -ArgumentList @("-NoExit", "-File", $StartScriptPath) -WorkingDirectory $RunDir -PassThru
Write-Ok "Started rescue window (PID: $($proc.Id))"

Write-Host ""
Write-Host "Rescue bundle finished." -ForegroundColor Yellow
Write-Host "Run directory: $RunDir" -ForegroundColor Yellow
Write-Host "Executable: $TargetExe" -ForegroundColor Yellow
Write-Host "Start script: $StartScriptPath" -ForegroundColor Yellow
