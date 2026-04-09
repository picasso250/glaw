from __future__ import annotations

import argparse
import base64
import io
import json
import os
import socket
import sys
import urllib.request
import zipfile
from datetime import datetime, timezone
from pathlib import Path

DEFAULT_USER_AGENT = "glaw-log-uploader/1.0 (+https://remote-executor.io99.xyz)"
DEFAULT_WORKER_URL = "https://remote-executor.io99.xyz"
DEFAULT_TOKEN_PATH = "~/.glaw-log-observer-token.txt"
DEFAULT_MAX_BYTES = 256 * 1024

DEFAULT_SERVICES = {
    "shuyao": [
        "~/g-claw/logs/start-shuyao.log",
        "~/g-claw/logs/glaw-stdout.log",
        "~/g-claw/logs/glaw-stderr.log",
    ],
    "mail-executor": [
        "~/claw-life-saver/logs/upgrade-claw-life-saver.log",
        "~/claw-life-saver/logs/claw-life-saver-stdout.log",
        "~/claw-life-saver/logs/claw-life-saver-stderr.log",
    ],
}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Upload remote log snapshots to the log observer worker.")
    parser.add_argument("--worker-url", default=DEFAULT_WORKER_URL)
    parser.add_argument("--token", default=os.environ.get("EXECUTOR_TOKEN", ""))
    parser.add_argument("--token-file", default=DEFAULT_TOKEN_PATH)
    parser.add_argument("--host", default="")
    parser.add_argument("--state-path", default="~/g-claw/tmp/log-upload-state.json")
    parser.add_argument("--status-path", default="~/g-claw/tmp/log-upload-last.json")
    parser.add_argument("--max-bytes", type=int, default=DEFAULT_MAX_BYTES)
    parser.add_argument(
        "--service",
        action="append",
        default=[],
        help="override service mapping as name=path1;path2;path3",
    )
    return parser.parse_args()


def resolve_token(args: argparse.Namespace) -> str:
    token = args.token.strip()
    if token:
        return token
    token_path = Path(args.token_file).expanduser()
    if token_path.exists():
        return token_path.read_text(encoding="utf-8").strip()
    return ""


def build_service_map(args: argparse.Namespace) -> dict[str, list[Path]]:
    if not args.service:
        return {
            name: [Path(item).expanduser() for item in items]
            for name, items in DEFAULT_SERVICES.items()
        }

    result: dict[str, list[Path]] = {}
    for raw in args.service:
        name, sep, rest = raw.partition("=")
        if not sep:
            raise SystemExit(f"invalid --service: {raw}")
        paths = [Path(part).expanduser() for part in rest.split(";") if part.strip()]
        if not paths:
            raise SystemExit(f"service has no paths: {raw}")
        result[name.strip()] = paths
    return result


def load_json(path: Path, fallback):
    if not path.exists():
        return fallback
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except Exception:
        return fallback


def save_json(path: Path, payload) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def utc_now() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def read_delta(path: Path, previous: dict, max_bytes: int) -> tuple[str, dict]:
    if not path.exists():
        return "", {"exists": False, "offset": 0, "size": 0}

    size = path.stat().st_size
    offset = int(previous.get("offset", 0) or 0)
    if offset < 0 or offset > size:
        offset = 0

    start = offset
    if size - start > max_bytes:
        start = max(0, size - max_bytes)

    with path.open("rb") as handle:
        handle.seek(start)
        raw = handle.read(max_bytes)

    text = raw.decode("utf-8", errors="replace")
    return text, {"exists": True, "offset": size, "size": size, "start": start}


def build_zip(service: str, host: str, timestamp: str, files: list[dict]) -> bytes:
    manifest = {
        "host": host,
        "service": service,
        "timestamp": timestamp,
        "files": [
            {
                "path": item["path"],
                "exists": item["exists"],
                "size": item["size"],
                "start": item["start"],
                "included_bytes": len(item["content"].encode("utf-8")),
            }
            for item in files
        ],
    }

    buffer = io.BytesIO()
    with zipfile.ZipFile(buffer, mode="w", compression=zipfile.ZIP_DEFLATED) as zf:
        zf.writestr("manifest.json", json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
        for item in files:
            name = item["zip_name"]
            payload = item["content"]
            if payload:
                zf.writestr(name, payload)
            else:
                zf.writestr(name, "")
    return buffer.getvalue()


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


def upload_one_service(
    worker_url: str,
    token: str,
    host: str,
    service: str,
    paths: list[Path],
    state: dict,
    max_bytes: int,
    timestamp: str,
) -> dict:
    service_state = state.setdefault(service, {})
    file_payloads = []
    changed_files = 0

    for index, path in enumerate(paths, start=1):
        previous = service_state.get(str(path), {})
        content, next_state = read_delta(path, previous, max_bytes)
        if next_state["exists"] and next_state["size"] != int(previous.get("size", -1)):
            changed_files += 1
        zip_name = f"{index:02d}_{path.name}"
        file_payloads.append(
            {
                "path": str(path),
                "zip_name": zip_name,
                "content": content,
                "exists": next_state["exists"],
                "size": next_state["size"],
                "start": next_state.get("start", 0),
            }
        )
        service_state[str(path)] = next_state

    archive = build_zip(service, host, timestamp, file_payloads)
    response = post_json(
        worker_url.rstrip("/") + "/logs/upload",
        token,
        {
            "host": host,
            "service": service,
            "timestamp": timestamp,
            "archive_name": f"{service}-logs.zip",
            "archive_base64": base64.b64encode(archive).decode("ascii"),
            "content_type": "application/zip",
            "summary": {
                "changed_files": changed_files,
                "file_count": len(paths),
                "max_bytes": max_bytes,
            },
        },
    )
    entry = response.get("entry") or {}
    return {
        "service": service,
        "key": entry.get("key", ""),
        "changed_files": changed_files,
        "size": entry.get("size", 0),
        "uploaded_at": entry.get("uploaded_at", timestamp),
        "download_url": entry.get("download_url", ""),
        "expires_at": entry.get("expires_at", ""),
    }


def main() -> int:
    if hasattr(sys.stdout, "reconfigure"):
        sys.stdout.reconfigure(encoding="utf-8")

    args = parse_args()
    token = resolve_token(args)
    if not token:
        print("missing token; pass --token or prepare token file", file=sys.stderr)
        return 2

    host = (args.host.strip() or socket.gethostname()).strip().lower()
    state_path = Path(args.state_path).expanduser()
    status_path = Path(args.status_path).expanduser()
    service_map = build_service_map(args)
    state = load_json(state_path, {})
    timestamp = utc_now()

    uploads = []
    for service, paths in service_map.items():
        uploads.append(
            upload_one_service(
                worker_url=args.worker_url,
                token=token,
                host=host,
                service=service,
                paths=paths,
                state=state,
                max_bytes=args.max_bytes,
                timestamp=timestamp,
            )
        )

    save_json(state_path, state)
    status_payload = {
        "host": host,
        "timestamp": timestamp,
        "uploads": uploads,
    }
    save_json(status_path, status_payload)
    print(json.dumps(status_payload, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
