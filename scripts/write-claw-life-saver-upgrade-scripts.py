from __future__ import annotations

import pathlib
import subprocess
import sys
import textwrap

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8")


def run_git_pull(repo_dir: pathlib.Path) -> None:
    result = subprocess.run(
        ["git", "pull"],
        cwd=repo_dir,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        check=False,
    )
    print("===== git pull =====")
    print(f"cwd={repo_dir}")
    print(f"exit_code={result.returncode}")
    if result.stdout:
        print(result.stdout, end="" if result.stdout.endswith("\n") else "\n")
    if result.stderr:
        print("----- stderr -----")
        print(result.stderr, end="" if result.stderr.endswith("\n") else "\n")
    if result.returncode != 0:
        raise SystemExit(result.returncode)


def write_text(path: pathlib.Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def main() -> int:
    home = pathlib.Path.home()
    repo_dir = home / "glaw"
    run_dir = home / "claw-life-saver"
    repo_git_dir = repo_dir / ".git"
    dest_exe_path = home / "bin" / "claw-life-saver.exe"
    env_path = run_dir / ".env"
    mail_filter_path = run_dir / "mail_filter_senders.txt"
    cron_config_path = run_dir / "cron.json"
    log_dir = run_dir / "logs"
    stdout_log_path = log_dir / "claw-life-saver-stdout.log"
    stderr_log_path = log_dir / "claw-life-saver-stderr.log"
    upgrade_log_path = log_dir / "upgrade-claw-life-saver.log"
    start_script_path = run_dir / "start-claw-life-saver.ps1"
    upgrade_script_path = run_dir / "upgrade-claw-life-saver.ps1"

    if not repo_dir.is_dir():
        raise SystemExit(f"missing repo dir: {repo_dir}")
    if not repo_git_dir.exists():
        raise SystemExit(f"missing repo .git: {repo_git_dir}")
    if not run_dir.is_dir():
        raise SystemExit(f"missing run dir: {run_dir}")

    run_git_pull(repo_dir)

    start_script = textwrap.dedent(
        f"""\
        $ErrorActionPreference = "Stop"
        $RunDir = "{run_dir}"
        $ExePath = "{dest_exe_path}"
        $EnvPath = "{env_path}"
        $MailFilterPath = "{mail_filter_path}"
        $CronConfigPath = "{cron_config_path}"
        $LogDir = "{log_dir}"
        $StdoutLogPath = "{stdout_log_path}"
        $StderrLogPath = "{stderr_log_path}"

        New-Item -ItemType Directory -Force -Path $LogDir | Out-Null
        Set-Location $RunDir
        & $ExePath serve --env $EnvPath --mail-filter $MailFilterPath --cron-config $CronConfigPath --exec-subject-keyword claw-life-saver 1>> $StdoutLogPath 2>> $StderrLogPath
        """
    )

    upgrade_script = textwrap.dedent(
        f"""\
        $ErrorActionPreference = "Stop"

        $RepoDir = "{repo_dir}"
        $RunDir = "{run_dir}"
        $DestExePath = "{dest_exe_path}"
        $LogDir = "{log_dir}"
        $UpgradeLogPath = "{upgrade_log_path}"
        $StartScriptPath = "{start_script_path}"

        function Write-Log {{
            param([string]$Message)
            $line = "[{{0}}] {{1}}" -f (Get-Date -Format o), $Message
            Write-Host $line
            Add-Content -LiteralPath $UpgradeLogPath -Value $line -Encoding UTF8
        }}

        New-Item -ItemType Directory -Force -Path $LogDir | Out-Null
        Write-Log "Starting claw-life-saver upgrade"
        Write-Log "RepoDir=$RepoDir"
        Write-Log "RunDir=$RunDir"
        Write-Log "DestExePath=$DestExePath"
        Write-Log "StartScriptPath=$StartScriptPath"

        Start-Sleep -Seconds 5

        $targetProcs = @(Get-CimInstance Win32_Process | Where-Object {{ $_.ExecutablePath -eq $DestExePath }})
        Write-Log "Found $($targetProcs.Count) matching claw-life-saver.exe process(es)"
        foreach ($proc in $targetProcs) {{
            Write-Log ("Stopping PID={{0}} CommandLine={{1}}" -f $proc.ProcessId, $proc.CommandLine)
            Stop-Process -Id $proc.ProcessId -Force
        }}

        Start-Sleep -Seconds 5

        $restartedProc = @(Get-CimInstance Win32_Process | Where-Object {{ $_.ExecutablePath -eq $DestExePath }})
        if ($restartedProc.Count -gt 0) {{
            $details = $restartedProc | Select-Object ProcessId, Name, ExecutablePath, CommandLine | Format-List | Out-String
            throw "claw-life-saver.exe was observed again before build:`n$details"
        }}

        Set-Location $RepoDir
        Write-Log "Building $DestExePath"
        & go build -buildvcs=false -o $DestExePath .\\cmd\\glaw
        if ($LASTEXITCODE -ne 0) {{
            throw "go build failed with exit code $LASTEXITCODE"
        }}

        Write-Log "Launching detached starter with ExecutionPolicy Bypass"
        $starter = Get-Command pwsh -ErrorAction SilentlyContinue
        if ($null -eq $starter) {{
            $starter = Get-Command powershell -ErrorAction Stop
        }}
        Start-Process -FilePath $starter.Source `
            -ArgumentList @("-NoProfile", "-ExecutionPolicy", "Bypass", "-File", $StartScriptPath) `
            -WorkingDirectory $RunDir `
            -WindowStyle Hidden

        Write-Log "Upgrade script completed"
        """
    )

    write_text(start_script_path, start_script)
    write_text(upgrade_script_path, upgrade_script)

    print("")
    print("===== written files =====")
    for path in (start_script_path, upgrade_script_path):
        print(f"path={path}")
        print("----- begin -----")
        print(path.read_text(encoding="utf-8"), end="" if path.read_text(encoding="utf-8").endswith("\n") else "\n")
        print("----- end -----")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
