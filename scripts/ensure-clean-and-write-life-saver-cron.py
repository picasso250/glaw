from __future__ import annotations

import json
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
    run_dir = home / "claw-life-saver"
    cron_path = run_dir / "cron.json"

    print(f"GeneratedAt: {datetime.now().astimezone().isoformat()}")
    print(f"RepoDir: {repo_dir}")
    print(f"RunDir: {run_dir}")
    print(f"CronPath: {cron_path}")
    print(f"RepoExists: {repo_dir.exists()}")
    print(f"RunDirExists: {run_dir.exists()}")

    if not repo_dir.exists():
        print("ERROR: repo dir missing")
        return 1
    if not run_dir.exists():
        print("ERROR: run dir missing")
        return 1

    print("\n===== Repo Status =====")
    code, status = run(["git", "status", "--short"], repo_dir)
    print(f"ExitCode: {code}")
    sys.stdout.write(status)
    if code != 0:
        return code

    status_lines = [line for line in status.splitlines() if line.strip()]
    git_clean = len(status_lines) == 0

    print("\n===== Git Clean =====")
    print(f"GitClean: {git_clean}")

    if not git_clean:
        print("SKIP: repo is not clean, not touching cron.json")
        return 0

    print("\n===== Ensure cron.json =====")
    before_exists = cron_path.exists()
    print(f"CronExistsBefore: {before_exists}")
    if before_exists:
        print("CronContentBefore:")
        sys.stdout.write(cron_path.read_text(encoding="utf-8", errors="replace"))
        if not str(cron_path.read_text(encoding='utf-8', errors='replace')).endswith("\n"):
            print()

    cron_content = []
    cron_path.write_text(
        json.dumps(cron_content, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
        newline="\n",
    )

    print(f"CronExistsAfter: {cron_path.exists()}")
    print("CronContentAfter:")
    sys.stdout.write(cron_path.read_text(encoding="utf-8"))
    if not str(cron_path.read_text(encoding="utf-8")).endswith("\n"):
        print()

    print("\nOK: repo clean and cron.json ensured")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
