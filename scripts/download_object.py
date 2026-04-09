from __future__ import annotations

import argparse
import os
import sys
import urllib.parse
import urllib.request
from pathlib import Path

DEFAULT_WORKER_URL = "https://remote-executor.io99.xyz"
DEFAULT_USER_AGENT = "glaw-object-viewer/1.0 (+https://remote-executor.io99.xyz)"


def request(url: str, token: str) -> bytes:
    headers = {"user-agent": DEFAULT_USER_AGENT}
    if token.strip():
        headers["authorization"] = f"Bearer {token}"
    req = urllib.request.Request(url, headers=headers)
    with urllib.request.urlopen(req) as resp:
        return resp.read()


def main() -> int:
    parser = argparse.ArgumentParser(description="Download one object from the worker by exact key or signed URL.")
    parser.add_argument("--worker-url", default=DEFAULT_WORKER_URL)
    parser.add_argument("--token", default=os.environ.get("EXECUTOR_TOKEN", ""))
    parser.add_argument("--key", default="")
    parser.add_argument("--url", default="")
    parser.add_argument("--output", required=True)
    args = parser.parse_args()

    if not args.url.strip() and not args.key.strip():
        print("provide --url or --key", file=sys.stderr)
        return 1

    output = Path(args.output).expanduser()
    if args.url.strip():
        payload = request(args.url.strip(), "")
    else:
        payload = request(
            args.worker_url.rstrip("/") + "/objects/object?" + urllib.parse.urlencode({"key": args.key.strip()}),
            args.token,
        )
    output.write_bytes(payload)
    print(str(output.resolve()))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
