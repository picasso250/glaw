from __future__ import annotations

import subprocess
import sys
from datetime import datetime

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8")


def main() -> int:
    print(f"GeneratedAt: {datetime.now().astimezone().isoformat()}")
    print("Probe: claw-life-saver post-upgrade")
    print("")

    ps_script = r"""
$targetExe = Join-Path $HOME 'bin\claw-life-saver.exe'
Get-CimInstance Win32_Process |
  Where-Object { $_.ExecutablePath -eq $targetExe } |
  Select-Object ProcessId, Name, ExecutablePath, CommandLine |
  Format-List | Out-String -Width 4096
"""

    result = subprocess.run(
        ["powershell", "-NoProfile", "-Command", ps_script],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        check=False,
    )

    if result.stdout:
        sys.stdout.write(result.stdout)
        if not result.stdout.endswith("\n"):
            sys.stdout.write("\n")
    if result.stderr:
        sys.stderr.write(result.stderr)
        if not result.stderr.endswith("\n"):
            sys.stderr.write("\n")
    return result.returncode


if __name__ == "__main__":
    raise SystemExit(main())
