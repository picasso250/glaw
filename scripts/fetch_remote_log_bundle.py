from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
import zipfile
from pathlib import Path


def run_download(args: argparse.Namespace, output_zip: Path) -> None:
    cmd = [
        "python",
        "scripts/download_remote_log.py",
        "--worker-url",
        args.worker_url,
        "--token",
        args.token,
        "--host",
        args.host,
        "--service",
        args.service,
        "--output",
        str(output_zip),
    ]
    if args.key.strip():
        cmd.extend(["--key", args.key.strip()])

    result = subprocess.run(
        cmd,
        check=False,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    if result.returncode != 0:
        sys.stderr.write(result.stderr or result.stdout)
        raise SystemExit(result.returncode)


def main() -> int:
    parser = argparse.ArgumentParser(description="Download and extract one remote log bundle.")
    parser.add_argument("--worker-url", default="https://remote-executor.io99.xyz")
    parser.add_argument("--token", required=True)
    parser.add_argument("--host", required=True)
    parser.add_argument("--service", required=True)
    parser.add_argument("--key", default="")
    parser.add_argument("--output-dir", default="")
    parser.add_argument("--keep-zip", action="store_true")
    args = parser.parse_args()

    safe_name = f"{args.host}-{args.service}"
    output_dir = Path(args.output_dir).expanduser() if args.output_dir.strip() else Path("tmp") / safe_name
    output_dir.mkdir(parents=True, exist_ok=True)

    zip_path = output_dir / f"{safe_name}.zip"
    extract_dir = output_dir / "expanded"
    if extract_dir.exists():
        shutil.rmtree(extract_dir)

    run_download(args, zip_path)

    with zipfile.ZipFile(zip_path) as zf:
        zf.extractall(extract_dir)

    print(str(extract_dir.resolve()))
    if not args.keep_zip and zip_path.exists():
        zip_path.unlink()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
