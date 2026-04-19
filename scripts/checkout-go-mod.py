from __future__ import annotations

import pathlib
import subprocess
import sys
from datetime import datetime

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8")


def run(args: list[str], cwd: pathlib.Path) -> tuple[int, str]:
    result = subprocess.run(
        args,
        cwd=str(cwd),
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        check=False,
    )
    output = result.stdout
    if result.stderr:
        output += result.stderr
    if output and not output.endswith("\n"):
        output += "\n"
    return result.returncode, output


def main() -> int:
    home = pathlib.Path.home()
    repo_dir = home / "glaw"

    print(f"GeneratedAt: {datetime.now().astimezone().isoformat()}")
    print(f"RepoDir: {repo_dir}")
    print(f"Exists: {repo_dir.exists()}")

    if not repo_dir.exists():
        print("ERROR: repo dir missing")
        return 1

    print("\n===== Status Before =====")
    _, before = run(["git", "status", "--short"], repo_dir)
    sys.stdout.write(before)

    print("\n===== Checkout go.mod =====")
    code, checkout = run(["git", "checkout", "--", "go.mod"], repo_dir)
    print(f"ExitCode: {code}")
    sys.stdout.write(checkout)
    if code != 0:
        return code

    print("\n===== Status After =====")
    _, after = run(["git", "status", "--short"], repo_dir)
    sys.stdout.write(after)

    print("\n===== Git Clean =====")
    status_lines = [line for line in after.splitlines() if line.strip()]
    print(f"GitClean: {len(status_lines) == 0}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
