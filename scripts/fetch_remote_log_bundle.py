from __future__ import annotations

import argparse
from datetime import datetime, timedelta, timezone
import os
import shutil
import subprocess
import sys
import zipfile
from pathlib import Path


def resolve_default_token() -> str:
    env_token = os.environ.get("EXECUTOR_TOKEN", "").strip()
    if env_token:
        return env_token

    token_path = Path.home() / ".glaw-executor-token.txt"
    if token_path.exists():
        return token_path.read_text(encoding="utf-8").strip()
    return ""


def build_log_key(host: str, service: str, dt: datetime) -> str:
    yyyy = f"{dt.year:04d}"
    mm = f"{dt.month:02d}"
    dd = f"{dt.day:02d}"
    hh = f"{dt.hour:02d}"
    file_name = f"{host}__{service}__{yyyy}-{mm}-{dd}__{hh}.zip"
    return f"logs/{host}/{service}/{yyyy}/{mm}/{dd}/{hh}/{file_name}"


def try_download(worker_url: str, token: str, key: str, output_zip: Path) -> bool:
    cmd = [
        "python",
        "scripts/download_object.py",
        "--worker-url",
        worker_url,
        "--token",
        token,
        "--key",
        key,
        "--output",
        str(output_zip),
    ]
    result = subprocess.run(
        cmd,
        check=False,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    return result.returncode == 0


def main() -> int:
    parser = argparse.ArgumentParser(description="Download and extract one remote log bundle.")
    parser.add_argument("--worker-url", default="https://file.io99.xyz")
    parser.add_argument("--token", default=resolve_default_token())
    parser.add_argument("--host", default="desktop-secpnpi")
    parser.add_argument("--service", default="shuyao")
    parser.add_argument("--key", default="")
    parser.add_argument("--output-dir", default="")
    parser.add_argument("--keep-zip", action="store_true")
    parser.add_argument("--lookback-hours", type=int, default=2)
    args = parser.parse_args()
    if not args.token.strip():
        print("missing token; set EXECUTOR_TOKEN or prepare ~/.glaw-executor-token.txt", file=sys.stderr)
        return 2

    safe_name = f"{args.host}-{args.service}"
    output_dir = Path(args.output_dir).expanduser() if args.output_dir.strip() else Path("tmp") / safe_name
    output_dir.mkdir(parents=True, exist_ok=True)

    zip_path = output_dir / f"{safe_name}.zip"
    extract_dir = output_dir / "expanded"
    if extract_dir.exists():
        shutil.rmtree(extract_dir)

    key = args.key.strip()
    if key:
        if not try_download(args.worker_url, args.token, key, zip_path):
            print(f"failed to download key: {key}", file=sys.stderr)
            return 1
    else:
        found = False
        now = datetime.now(timezone.utc).replace(minute=0, second=0, microsecond=0)
        for offset in range(args.lookback_hours):
            candidate_dt = now - timedelta(hours=offset)
            candidate_key = build_log_key(args.host, args.service, candidate_dt)
            if try_download(args.worker_url, args.token, candidate_key, zip_path):
                found = True
                break
        if not found:
            print("failed to find a recent log bundle by deterministic key", file=sys.stderr)
            return 1

    with zipfile.ZipFile(zip_path) as zf:
        zf.extractall(extract_dir)

    print(str(extract_dir.resolve()))
    if not args.keep_zip and zip_path.exists():
        zip_path.unlink()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
