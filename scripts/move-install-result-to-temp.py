from __future__ import annotations

import pathlib
import shutil
import sys
import tempfile
from datetime import datetime

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8")


def main() -> int:
    home = pathlib.Path.home()
    repo_dir = home / "glaw"
    source_path = repo_dir / "install-claw-executor-result.txt"

    print(f"GeneratedAt: {datetime.now().astimezone().isoformat()}")
    print(f"RepoDir: {repo_dir}")
    print(f"SourcePath: {source_path}")
    print(f"SourceExistsBefore: {source_path.exists()}")

    if not repo_dir.exists():
        print("ERROR: repo dir missing")
        return 1

    if not source_path.exists():
        print("SKIP: source file does not exist")
        return 0

    temp_dir = pathlib.Path(tempfile.mkdtemp(prefix="glaw-install-result-"))
    target_path = temp_dir / source_path.name

    print(f"TempDir: {temp_dir}")
    print(f"TargetPath: {target_path}")

    shutil.move(str(source_path), str(target_path))

    print(f"SourceExistsAfter: {source_path.exists()}")
    print(f"TargetExistsAfter: {target_path.exists()}")

    if not target_path.exists():
        print("ERROR: move completed without target file")
        return 1

    print("OK: moved install-claw-executor-result.txt to temp dir")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
