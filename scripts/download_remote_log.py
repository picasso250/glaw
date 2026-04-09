from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.parse
import urllib.request
from pathlib import Path

DEFAULT_WORKER_URL = "https://remote-executor.io99.xyz"
DEFAULT_USER_AGENT = "glaw-log-viewer/1.0 (+https://remote-executor.io99.xyz)"


def request(url: str, token: str) -> bytes:
    req = urllib.request.Request(
        url,
        headers={
            "authorization": f"Bearer {token}",
            "user-agent": DEFAULT_USER_AGENT,
        },
    )
    with urllib.request.urlopen(req) as resp:
        return resp.read()


def request_json(url: str, token: str) -> dict:
    return json.loads(request(url, token).decode("utf-8"))


def main() -> int:
    parser = argparse.ArgumentParser(description="Download one uploaded remote log bundle.")
    parser.add_argument("--worker-url", default=DEFAULT_WORKER_URL)
    parser.add_argument("--token", default=os.environ.get("EXECUTOR_TOKEN", ""))
    parser.add_argument("--host", required=True)
    parser.add_argument("--service", required=True)
    parser.add_argument("--key", default="")
    parser.add_argument("--output", default="")
    args = parser.parse_args()
    if not args.token.strip():
        print("missing token; set EXECUTOR_TOKEN or pass --token", file=sys.stderr)
        return 2

    key = args.key.strip()
    if not key:
        query = urllib.parse.urlencode({"host": args.host, "service": args.service})
        latest = request_json(args.worker_url.rstrip("/") + "/logs/latest?" + query, args.token)
        entry = latest.get("entry") or {}
        key = str(entry.get("key") or "").strip()
        if not key:
            print("latest log key is missing", file=sys.stderr)
            return 1

    output = Path(args.output).expanduser() if args.output.strip() else Path(key.split("/")[-1])
    payload = request(
        args.worker_url.rstrip("/") + "/logs/object?" + urllib.parse.urlencode({"key": key}),
        args.token,
    )
    output.write_bytes(payload)
    print(str(output.resolve()))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
