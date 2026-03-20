$ErrorActionPreference = "Stop"

$DesktopPath = [Environment]::GetFolderPath("Desktop")
$ShortcutPath = Join-Path $DesktopPath "Gemini in g-claw.lnk"
$WorkingDirectory = Join-Path $HOME "g-claw"
$WindowsTerminal = Join-Path $env:LOCALAPPDATA "Microsoft\WindowsApps\wt.exe"
$GeminiPrompt = ' read INIT.md and do as he say '

if (-not (Test-Path $WindowsTerminal)) {
    throw "wt.exe was not found at $WindowsTerminal"
}

if (-not (Test-Path $WorkingDirectory)) {
    throw "Working directory was not found: $WorkingDirectory"
}

$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($ShortcutPath)
$shortcut.TargetPath = $WindowsTerminal
$shortcut.Arguments = "-d `"$WorkingDirectory`" pwsh -NoExit -Command `"& { gemini '$GeminiPrompt' }`""
$shortcut.WorkingDirectory = $WorkingDirectory
$shortcut.IconLocation = "$WindowsTerminal,0"
$shortcut.Save()

Write-Host "Created shortcut:"
Write-Host $ShortcutPath
