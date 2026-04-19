from __future__ import annotations

import json
import subprocess
import sys
import time
from pathlib import Path

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8")


TARGET_EXE = Path.home() / "bin" / "glaw.exe"
WAIT_SECONDS = 5


def run_powershell_json(script: str) -> list[dict[str, str]]:
    result = subprocess.run(
        ["powershell", "-NoProfile", "-Command", script],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        check=False,
    )
    if result.returncode != 0:
        raise SystemExit(result.stderr.strip() or f"powershell failed with exit code {result.returncode}")
    text = result.stdout.strip()
    if not text:
        return []
    data = json.loads(text)
    if isinstance(data, list):
        return data
    return [data]


def list_pi_processes() -> list[dict[str, str]]:
    script = r"""
$items = Get-CimInstance Win32_Process | Where-Object {
  $_.Name -notin @('powershell.exe', 'pwsh.exe') -and
  $_.CommandLine -and (
    $_.CommandLine -match '(^|["''\s\\/])pi(\.cmd|\.exe)?\s+-p(\s|$)' -or
    $_.CommandLine -match 'pi-coding-agent' -or
    $_.CommandLine -match '@mariozechner\\pi-coding-agent'
  )
} | Select-Object ProcessId, Name, ExecutablePath, CommandLine

if ($null -eq $items) {
  '[]'
} else {
  @($items) | ConvertTo-Json -Compress -Depth 3
}
"""
    return run_powershell_json(script)


def list_glaw_processes() -> list[dict[str, str]]:
    target_exe = str(TARGET_EXE).replace("\\", "\\\\")
    script = rf"""
$targetExe = '{target_exe}'
$items = Get-CimInstance Win32_Process | Where-Object {{
  ($_.Name -ieq 'glaw.exe') -or
  ($_.ExecutablePath -eq $targetExe) -or
  ($_.ExecutablePath -and $_.ExecutablePath.ToLower().Contains('\bin\glaw.exe'))
}} | Select-Object ProcessId, Name, ExecutablePath, CommandLine

if ($null -eq $items) {{
  '[]'
}} else {{
  @($items) | ConvertTo-Json -Compress -Depth 3
}}
"""
    return run_powershell_json(script)


def stop_process(pid: str) -> None:
    result = subprocess.run(
        ["powershell", "-NoProfile", "-Command", f"Stop-Process -Id {pid} -Force"],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        check=False,
    )
    if result.returncode != 0:
        raise SystemExit(result.stderr.strip() or f"Stop-Process failed for PID {pid}")


def print_processes(title: str, items: list[dict[str, str]]) -> None:
    print(f"===== {title} ({len(items)}) =====")
    if not items:
        print("(none)")
        return
    for item in items:
        print(
            json.dumps(
                {
                    "ProcessId": item.get("ProcessId", ""),
                    "Name": item.get("Name", ""),
                    "ExecutablePath": item.get("ExecutablePath", ""),
                    "CommandLine": item.get("CommandLine", ""),
                },
                ensure_ascii=False,
            )
        )


def main() -> int:
    print(f"TargetExe={TARGET_EXE}")

    first_pi = list_pi_processes()
    print_processes("PI Processes Before Wait", first_pi)
    if first_pi:
        print(f"pi process detected; sleeping {WAIT_SECONDS}s before recheck")
        time.sleep(WAIT_SECONDS)
        second_pi = list_pi_processes()
        print_processes("PI Processes After Wait", second_pi)
        if second_pi:
            print("pi process still running; skip stopping glaw.exe")
            return 2

    glaw_processes = list_glaw_processes()
    print_processes("glaw.exe Processes Before Stop", glaw_processes)
    if not glaw_processes:
        print("no matching glaw.exe process to stop")
        return 0

    for item in glaw_processes:
        pid = str(item.get("ProcessId", "")).strip()
        if not pid:
            continue
        print(f"stopping glaw.exe pid={pid}")
        stop_process(pid)

    time.sleep(2)
    remaining = list_glaw_processes()
    print_processes("glaw.exe Processes After Stop", remaining)
    if remaining:
        raise SystemExit("glaw.exe still running after graceful stop attempt")

    print("graceful stop completed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
