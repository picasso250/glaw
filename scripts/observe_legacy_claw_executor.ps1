param(
    [string]$ResultPath = (Join-Path (Get-Location) "observe-legacy-claw-executor-result.txt")
)

$ErrorActionPreference = "Stop"

$legacyProcesses = @()
$processes = Get-CimInstance Win32_Process | Where-Object {
    $_.CommandLine
}

foreach ($proc in $processes) {
    $name = ($proc.Name | ForEach-Object { $_.ToLower() })
    $cmd = ($proc.CommandLine | ForEach-Object { $_.ToLower() })
    $isLegacyPython = ($name -in @("python.exe", "pythonw.exe")) -and ($cmd -match '(^|["''\s\\/])claw_executor\.py($|["''\s])')
    $isLegacyPowerShell = ($name -in @("powershell.exe", "pwsh.exe")) -and ($cmd -match '-file\s+["'']?[^"'']*start-claw-executor\.ps1(["'']|\s|$)')
    if (-not ($isLegacyPython -or $isLegacyPowerShell)) {
        continue
    }
    $legacyProcesses += [PSCustomObject]@{
        ProcessId   = $proc.ProcessId
        Name        = $proc.Name
        CommandLine = $proc.CommandLine
    }
}

$result = @(
    "ObservedAt: $(Get-Date -Format o)",
    "MatchCount: $($legacyProcesses.Count)",
    "",
    ($legacyProcesses | ConvertTo-Json -Depth 6)
)

$result | Set-Content -LiteralPath $ResultPath -Encoding UTF8
Write-Host "result: $ResultPath"
