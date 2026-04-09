from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.parse
import urllib.request

DEFAULT_WORKER_URL = "https://remote-executor.io99.xyz"
DEFAULT_USER_AGENT = "glaw-log-viewer/1.0 (+https://remote-executor.io99.xyz)"


def request_json(url: str, token: str) -> dict:
    req = urllib.request.Request(
        url,
        headers={
            "authorization": f"Bearer {token}",
            "user-agent": DEFAULT_USER_AGENT,
        },
    )
    with urllib.request.urlopen(req) as resp:
        return json.loads(resp.read().decode("utf-8"))


def main() -> int:
    parser = argparse.ArgumentParser(description="List uploaded remote log bundles.")
    parser.add_argument("--worker-url", default=DEFAULT_WORKER_URL)
    parser.add_argument("--token", default=os.environ.get("EXECUTOR_TOKEN", ""))
    parser.add_argument("--host", required=True)
    parser.add_argument("--service", required=True)
    parser.add_argument("--limit", type=int, default=10)
    args = parser.parse_args()
    if not args.token.strip():
        print("missing token; set EXECUTOR_TOKEN or pass --token", file=sys.stderr)
        return 2

    query = urllib.parse.urlencode(
        {"host": args.host, "service": args.service, "limit": args.limit}
    )
    payload = request_json(args.worker_url.rstrip("/") + "/logs/index?" + query, args.token)
    print(json.dumps(payload, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
