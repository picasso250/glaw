from __future__ import annotations

import argparse
import subprocess
import zipfile
from pathlib import Path


def git_short_hash(repo_root: Path) -> str:
    result = subprocess.run(
        ["git", "rev-parse", "--short", "HEAD"],
        cwd=repo_root,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        check=True,
    )
    value = result.stdout.strip()
    if not value:
        raise SystemExit("unexpected empty short hash")
    return value


def tracked_files(repo_root: Path) -> list[Path]:
    result = subprocess.run(
        ["git", "ls-files", "*.go", "go.mod", "go.sum"],
        cwd=repo_root,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        check=True,
    )
    files: list[Path] = []
    for line in result.stdout.splitlines():
        rel = line.strip()
        if not rel:
            continue
        path = repo_root / rel
        if path.is_file():
            files.append(path)
    return files


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", default=".", help="repo root")
    parser.add_argument(
        "--output",
        default="dist/mail-upgrade",
        help="output zip path or output directory; default writes into dist/mail-upgrade with default short-hash in filename",
    )
    args = parser.parse_args()

    repo_root = Path(args.repo).resolve()
    short_hash = git_short_hash(repo_root)
    requested_output = Path(args.output)
    if requested_output.suffix.lower() == ".zip":
        output_path = requested_output.resolve()
    else:
        output_path = (requested_output / f"claw-life-saver-source-{short_hash}.zip").resolve()
    output_path.parent.mkdir(parents=True, exist_ok=True)

    files = tracked_files(repo_root)
    if not files:
        raise SystemExit("no tracked Go source files found")

    with zipfile.ZipFile(output_path, "w", compression=zipfile.ZIP_DEFLATED) as zf:
        for path in files:
            zf.write(path, arcname=path.relative_to(repo_root).as_posix())

    print(f"repo={repo_root}")
    print(f"short_hash={short_hash}")
    print(f"output={output_path}")
    print(f"files={len(files)}")
    for path in files:
        print(path.relative_to(repo_root).as_posix())
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
