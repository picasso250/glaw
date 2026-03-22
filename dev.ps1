param()

$ErrorActionPreference = "Stop"

$RepoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$HomeDir = [Environment]::GetFolderPath("UserProfile")
$BinDir = Join-Path $HomeDir "bin"
$RunDir = Join-Path $HomeDir "my-claw"
$GatewayExe = Join-Path $BinDir "gateway.exe"

if (!(Test-Path $RunDir)) {
    throw "Run directory not found: $RunDir"
}

New-Item -ItemType Directory -Path $BinDir -Force | Out-Null

Write-Host "[dev] stopping existing gateway.exe (if any)..."
$running = Get-CimInstance Win32_Process |
    Where-Object { $_.Name -eq "gateway.exe" -and $_.ExecutablePath -eq $GatewayExe }
foreach ($proc in $running) {
    Stop-Process -Id $proc.ProcessId -Force
}

Push-Location $RepoRoot
try {
    Write-Host "[dev] building gateway.exe..."
    & go build -buildvcs=false -o $GatewayExe .\cmd\gateway
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }
    Write-Host "[dev] build complete: $GatewayExe"
} finally {
    Pop-Location
}

Push-Location $RunDir
try {
    Write-Host "[dev] starting gateway.exe in $RunDir ..."
    & $GatewayExe
} finally {
    Pop-Location
}
