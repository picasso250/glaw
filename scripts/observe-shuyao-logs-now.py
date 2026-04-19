from __future__ import annotations

import pathlib
import subprocess
import sys
from datetime import datetime

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8")


def run_powershell(script: str) -> tuple[int, str]:
    result = subprocess.run(
        ["powershell.exe", "-NoProfile", "-Command", script],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        check=False,
    )
    output = result.stdout
    if result.stderr:
        output += result.stderr
    if not output.endswith("\n"):
        output += "\n"
    return result.returncode, output


def tail_text(path: pathlib.Path, limit: int = 120) -> str:
    if not path.exists():
        return "MISSING\n"
    lines = path.read_text(encoding="utf-8", errors="replace").splitlines()
    if not lines:
        return "EMPTY\n"
    return "\n".join(lines[-limit:]) + "\n"


def main() -> int:
    home = pathlib.Path.home()
    run_dir = home / "g-claw"
    log_dir = run_dir / "logs"
    start_log_path = log_dir / "start-shuyao.log"
    stdout_path = log_dir / "glaw-stdout.log"
    stderr_path = log_dir / "glaw-stderr.log"
    exe_path = home / "bin" / "glaw.exe"

    print(f"GeneratedAt: {datetime.now().astimezone().isoformat()}")
    print(f"RunDir: {run_dir}")
    print(f"ExePath: {exe_path}")

    ps = rf"""
$targetExe = '{exe_path}'
Get-CimInstance Win32_Process |
  Where-Object {{ $_.ExecutablePath -eq $targetExe }} |
  Select-Object ProcessId, Name, ExecutablePath, CommandLine |
  Format-List | Out-String
"""
    print("\n===== Running glaw.exe Processes =====")
    code, output = run_powershell(ps)
    print(f"ExitCode: {code}")
    sys.stdout.write(output)

    print("\n===== start-shuyao.log =====")
    sys.stdout.write(tail_text(start_log_path))

    print("\n===== glaw-stdout.log =====")
    sys.stdout.write(tail_text(stdout_path))

    print("\n===== glaw-stderr.log =====")
    sys.stdout.write(tail_text(stderr_path))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
