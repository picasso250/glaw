from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
import sys
import urllib.request
import zipfile
from datetime import datetime, timezone
from pathlib import Path

DEFAULT_WORKER_URL = "https://remote-executor.io99.xyz"
DEFAULT_USER_AGENT = "glaw-artifact-uploader/1.0 (+https://remote-executor.io99.xyz)"


def utc_now() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def build_zip(output: Path, inputs: list[Path], base_dir: Path | None) -> None:
    with zipfile.ZipFile(output, mode="w", compression=zipfile.ZIP_DEFLATED) as zf:
        for path in inputs:
            resolved = path.resolve()
            if not resolved.exists():
                raise SystemExit(f"missing input: {resolved}")
            if resolved.is_dir():
                for nested in resolved.rglob("*"):
                    if nested.is_file():
                        arcname = nested.relative_to(base_dir) if base_dir and nested.is_relative_to(base_dir) else nested.name
                        zf.write(nested, arcname.as_posix() if isinstance(arcname, Path) else str(arcname))
            else:
                arcname = resolved.relative_to(base_dir) if base_dir and resolved.is_relative_to(base_dir) else Path(resolved.name)
                zf.write(resolved, arcname.as_posix())


def post_json(url: str, token: str, payload: dict) -> dict:
    req = urllib.request.Request(
        url,
        data=json.dumps(payload).encode("utf-8"),
        method="POST",
        headers={
            "authorization": f"Bearer {token}",
            "content-type": "application/json; charset=utf-8",
            "user-agent": DEFAULT_USER_AGENT,
        },
    )
    with urllib.request.urlopen(req) as resp:
        return json.loads(resp.read().decode("utf-8"))


def main() -> int:
    parser = argparse.ArgumentParser(description="Upload a zip artifact bundle to the remote observer worker.")
    parser.add_argument("--worker-url", default=DEFAULT_WORKER_URL)
    parser.add_argument("--token", default=os.environ.get("EXECUTOR_TOKEN", ""))
    parser.add_argument("--channel", required=True)
    parser.add_argument("--zip", default="", help="existing zip file to upload")
    parser.add_argument("--file", action="append", default=[], help="file or directory to bundle; may repeat")
    parser.add_argument("--base-dir", default="", help="optional base directory for zip relative paths")
    parser.add_argument("--name", default="", help="override uploaded file name")
    parser.add_argument("--timestamp", default="")
    args = parser.parse_args()

    token = args.token.strip()
    if not token:
        print("missing token; set EXECUTOR_TOKEN or pass --token", file=sys.stderr)
        return 2

    temp_zip: Path | None = None
    if args.zip.strip():
        zip_path = Path(args.zip).resolve()
        if not zip_path.exists():
            print(f"zip not found: {zip_path}", file=sys.stderr)
            return 1
    else:
        if not args.file:
            print("provide --zip or at least one --file", file=sys.stderr)
            return 1
        zip_path = Path("tmp") / f"artifact-{datetime.now().strftime('%Y%m%dT%H%M%S')}.zip"
        zip_path.parent.mkdir(parents=True, exist_ok=True)
        temp_zip = zip_path.resolve()
        inputs = [Path(item).resolve() for item in args.file]
        base_dir = Path(args.base_dir).resolve() if args.base_dir.strip() else None
        build_zip(temp_zip, inputs, base_dir)
        zip_path = temp_zip

    payload_bytes = zip_path.read_bytes()
    sha256 = hashlib.sha256(payload_bytes).hexdigest()
    upload_name = args.name.strip() or zip_path.name
    payload = {
        "channel": args.channel,
        "timestamp": args.timestamp.strip() or utc_now(),
        "file_name": upload_name,
        "file_base64": base64.b64encode(payload_bytes).decode("ascii"),
        "content_type": "application/zip",
    }
    response = post_json(args.worker_url.rstrip("/") + "/artifacts/upload", token, payload)
    artifact = response.get("artifact") or {}
    artifact["local_sha256"] = sha256
    artifact["source_zip"] = str(zip_path)
    print(json.dumps({"ok": True, "artifact": artifact}, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
