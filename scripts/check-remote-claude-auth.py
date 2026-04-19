from __future__ import annotations

import pathlib
import subprocess
import sys
from datetime import datetime

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8")


def run_command(args: list[str]) -> tuple[int, str]:
    try:
        result = subprocess.run(
            args,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            check=False,
        )
    except FileNotFoundError:
        return 127, "missing\n"

    output = result.stdout
    if result.stderr:
        output += result.stderr
    if not output.endswith("\n"):
        output += "\n"
    return result.returncode, output


def main() -> int:
    home = pathlib.Path.home()
    print(f"GeneratedAt: {datetime.now().astimezone().isoformat()}")
    print(f"HOME: {home}")
    print(f"SettingsPath: {home / '.claude' / 'settings.json'}")

    print("\n[claude.cmd --version]")
    print(run_command(["claude.cmd", "--version"])[1], end="")

    print("\n[claude.cmd auth status]")
    auth_code, auth_output = run_command(["claude.cmd", "auth", "status"])
    print(f"AuthExitCode: {auth_code}")
    print(auth_output, end="")

    print("\n[cmd.exe /c claude -p \"say ok\"]")
    print(run_command(["cmd.exe", "/c", "claude", "--permission-mode", "bypassPermissions", "-p", "say ok"])[1], end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
