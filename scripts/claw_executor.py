from __future__ import annotations

import argparse
import json
import os
import pathlib
import subprocess
import sys
import time
import urllib.request

DEFAULT_USER_AGENT = "claw-executor/1.0 (+https://file.io99.xyz)"


def request_json(url: str, token: str, payload: dict | None = None, method: str = "POST") -> dict:
    data = None
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        method=method,
        headers={
            "authorization": f"Bearer {token}",
            "content-type": "application/json; charset=utf-8",
            "user-agent": DEFAULT_USER_AGENT,
        },
    )
    with urllib.request.urlopen(req) as resp:
        return json.loads(resp.read().decode("utf-8"))


def execute_task(task: dict) -> tuple[int, str, str]:
    raw_cwd = str(task.get("cwd") or "").strip()
    cwd = pathlib.Path(raw_cwd).expanduser() if raw_cwd else pathlib.Path.cwd()
    cwd.mkdir(parents=True, exist_ok=True)
    script_path = cwd / task["filename"]
    script_path.write_text(task["content"], encoding="utf-8", newline="\n")

    if task["lang"] == "python":
        cmd = ["python", str(script_path)]
    elif task["lang"] == "powershell":
        cmd = ["pwsh", "-File", str(script_path)]
    else:
        raise RuntimeError(f"unsupported lang: {task['lang']}")

    result = subprocess.run(
        cmd,
        cwd=str(cwd),
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        timeout=int(task.get("timeout_sec", 300)),
        check=False,
    )
    return result.returncode, result.stdout, result.stderr


def main() -> int:
    parser = argparse.ArgumentParser(description="Poll the Cloudflare task worker and execute one task.")
    parser.add_argument("--worker-url", required=True)
    parser.add_argument("--token", default=os.environ.get("EXECUTOR_TOKEN", ""))
    parser.add_argument("--agent-id", default="claw-executor")
    parser.add_argument("--poll-interval", type=float, default=30.0)
    parser.add_argument("--once", action="store_true")
    args = parser.parse_args()
    if not args.token.strip():
        print("missing token; pass --token or set EXECUTOR_TOKEN", file=sys.stderr)
        return 2

    claim_url = args.worker_url.rstrip("/") + "/tasks/claim"
    while True:
        claimed = request_json(claim_url, args.token, {"agent_id": args.agent_id})
        task = claimed.get("task")
        if not task:
            if args.once:
                return 0
            time.sleep(args.poll_interval)
            continue

        status = "failed"
        exit_code = None
        stdout = ""
        stderr = ""
        error = ""
        try:
            exit_code, stdout, stderr = execute_task(task)
            status = "succeeded" if exit_code == 0 else "failed"
        except subprocess.TimeoutExpired as exc:
            exit_code = -1
            stdout = exc.stdout or ""
            stderr = (exc.stderr or "") + "\nTIMEOUT"
            error = "timeout"
        except Exception as exc:
            exit_code = -1
            error = str(exc)

        result_url = args.worker_url.rstrip("/") + f"/tasks/{task['id']}/result"
        request_json(
            result_url,
            args.token,
            {
                "status": status,
                "exit_code": exit_code,
                "stdout": stdout,
                "stderr": stderr,
                "error": error,
            },
        )
        print(f"submitted result for {task['id']} status={status}")
        if args.once:
            return 0


if __name__ == "__main__":
    raise SystemExit(main())
