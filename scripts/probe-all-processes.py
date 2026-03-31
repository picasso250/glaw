from __future__ import annotations

import subprocess
import sys
from datetime import datetime

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8")


def main() -> int:
    print(f"GeneratedAt: {datetime.now().astimezone().isoformat()}")
    print("Probe: all processes as CSV")
    print("")

    ps_script = r"""
Get-CimInstance Win32_Process |
  Sort-Object ProcessId |
  Select-Object ProcessId, Name, ExecutablePath, CommandLine |
  ConvertTo-Csv -NoTypeInformation
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
