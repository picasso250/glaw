from __future__ import annotations

import subprocess
import sys
from datetime import datetime

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8")


def main() -> int:
    print(f"GeneratedAt: {datetime.now().astimezone().isoformat()}")
    print("Probe: PowerShell processes whose command line contains glaw")
    print("")

    ps_script = r"""
$pattern = 'glaw'
$items = Get-CimInstance Win32_Process |
  Where-Object {
    $_.Name -match '^(pwsh|powershell)(\.exe)?$' -and
    $_.CommandLine -and
    $_.CommandLine.ToLower().Contains($pattern)
  } |
  Select-Object ProcessId, Name, ExecutablePath, CommandLine

if (-not $items) {
  Write-Output 'NO_MATCH'
  exit 0
}

$items | Format-List | Out-String -Width 4096
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
