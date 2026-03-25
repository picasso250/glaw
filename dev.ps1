param(
    [string]$RunDir = (Get-Location).Path,
    [string]$AgentCmd = ""
)

$ErrorActionPreference = "Stop"

$RepoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$HomeDir = [Environment]::GetFolderPath("UserProfile")
$BinDir = Join-Path $HomeDir "bin"
$GatewayExe = Join-Path $BinDir "glaw.exe"

if (!(Test-Path $RunDir)) {
    throw "Run directory not found: $RunDir"
}

New-Item -ItemType Directory -Path $BinDir -Force | Out-Null

Write-Host "[dev] stopping existing glaw.exe (if any)..."
$running = Get-CimInstance Win32_Process |
    Where-Object { $_.Name -eq "glaw.exe" -and $_.ExecutablePath -eq $GatewayExe }
foreach ($proc in $running) {
    Stop-Process -Id $proc.ProcessId -Force
}

Push-Location $RepoRoot
try {
    $BuildCommand = "go build -buildvcs=false -o `"$GatewayExe`" .\cmd\glaw"
    Write-Host "[dev] building glaw.exe..."
    Write-Host "[dev] command: $BuildCommand"
    & go build -buildvcs=false -o $GatewayExe .\cmd\glaw
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }
    Write-Host "[dev] build complete: $GatewayExe"
} finally {
    Pop-Location
}

Push-Location $RunDir
try {
    Write-Host "[dev] starting glaw.exe serve in $RunDir ..."
    $ServeArgs = @("serve")
    if ($AgentCmd.Trim() -ne "") {
        $ServeArgs += @("--agent-cmd", $AgentCmd)
    }
    Write-Host "[dev] command: $GatewayExe $($ServeArgs -join ' ')"
    & $GatewayExe @ServeArgs
} finally {
    Pop-Location
}
