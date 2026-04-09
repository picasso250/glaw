from __future__ import annotations

import argparse
import json
import os
import pathlib
import sys
import time
import urllib.request
from datetime import datetime, timezone

DEFAULT_USER_AGENT = "claw-executor/1.0 (+https://file.io99.xyz)"


def build_task_id() -> str:
    return "task_" + datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")


def post_json(url: str, token: str, payload: dict) -> dict:
    return request_json(url, token, payload=payload, method="POST")


def request_json(url: str, token: str, payload: dict | None = None, method: str = "GET") -> dict:
    data = None
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        headers={
            "content-type": "application/json; charset=utf-8",
            "authorization": f"Bearer {token}",
            "user-agent": DEFAULT_USER_AGENT,
        },
        method=method,
    )
    with urllib.request.urlopen(req) as resp:
        return json.loads(resp.read().decode("utf-8"))


def wait_for_task(worker_url: str, token: str, task_id: str, poll_interval: float) -> dict:
    task_url = worker_url.rstrip("/") + f"/tasks/{task_id}"
    while True:
        result = request_json(task_url, token, method="GET")
        state = result.get("state") or {}
        status = str(state.get("status") or "")
        if status in {"succeeded", "failed"}:
            return result
        time.sleep(poll_interval)


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Submit one script task to the Cloudflare executor worker."
    )
    parser.add_argument("--worker-url", required=True)
    parser.add_argument("--token", default=os.environ.get("EXECUTOR_TOKEN", ""))
    parser.add_argument("--lang", choices=["python", "powershell"], required=True)
    parser.add_argument("--file", required=True, help="local script file to upload as task content")
    parser.add_argument("--cwd", default="", help="optional remote working directory override")
    parser.add_argument("--timeout-sec", type=int, default=300)
    parser.add_argument("--task-id", default="")
    parser.add_argument("--poll-interval", type=float, default=2.0)
    parser.add_argument("--no-wait", action="store_true")
    args = parser.parse_args()
    if not args.token.strip():
        print("missing token; pass --token or set EXECUTOR_TOKEN", file=sys.stderr)
        return 2

    script_path = pathlib.Path(args.file).resolve()
    if not script_path.exists():
        print(f"script not found: {script_path}", file=sys.stderr)
        return 1

    payload = {
        "id": args.task_id.strip() or build_task_id(),
        "type": "script",
        "lang": args.lang,
        "filename": script_path.name,
        "content": script_path.read_text(encoding="utf-8"),
        "timeout_sec": args.timeout_sec,
    }
    if args.cwd.strip():
        payload["cwd"] = args.cwd.strip()
    result = post_json(args.worker_url.rstrip("/") + "/tasks", args.token, payload)
    print(json.dumps(result, ensure_ascii=False, indent=2))
    if args.no_wait:
        return 0

    task_id = payload["id"]
    print(f"waiting for task {task_id} ...", file=sys.stderr)
    final_result = wait_for_task(args.worker_url, args.token, task_id, args.poll_interval)
    print(json.dumps(final_result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
