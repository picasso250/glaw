from __future__ import annotations

import argparse
import pathlib
import subprocess
import sys
import time

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8")


def run(args: list[str], cwd: pathlib.Path | None = None) -> int:
    result = subprocess.run(args, cwd=str(cwd) if cwd else None, check=False)
    return result.returncode


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Send an execution email, wait, then inspect the latest reply with glaw."
    )
    parser.add_argument("--to", required=True, help="Recipient email address")
    parser.add_argument("--subject", required=True, help="Mail subject, should include exec keyword")
    parser.add_argument("--body-file", required=True, help="Markdown body file path")
    parser.add_argument("--attachment", required=True, help="Single script attachment path")
    parser.add_argument("--reply-sender", required=True, help="Sender address to inspect with glaw mail latest")
    parser.add_argument("--repo", required=True, help="Path to the glaw repo used for mail latest")
    parser.add_argument("--wait-seconds", type=int, default=30, help="Seconds to wait before checking latest reply")
    parser.add_argument(
        "--send-email-script",
        default=str(pathlib.Path(__file__).resolve().parents[2] / "send-email" / "scripts" / "send_email.py"),
        help="Path to send_email.py",
    )
    args = parser.parse_args()

    repo = pathlib.Path(args.repo).resolve()
    body_file = pathlib.Path(args.body_file).resolve()
    attachment = pathlib.Path(args.attachment).resolve()
    send_email_script = pathlib.Path(args.send_email_script).resolve()

    if not repo.exists():
        raise SystemExit(f"repo not found: {repo}")
    if not body_file.exists():
        raise SystemExit(f"body file not found: {body_file}")
    if not attachment.exists():
        raise SystemExit(f"attachment not found: {attachment}")
    if not send_email_script.exists():
        raise SystemExit(f"send email script not found: {send_email_script}")

    send_cmd = [
        "python",
        str(send_email_script),
        "--to",
        args.to,
        "--subject",
        args.subject,
        "--markdown-body-file",
        str(body_file),
        "--attachments",
        str(attachment),
    ]
    print("=== send ===")
    print(" ".join(send_cmd))
    code = run(send_cmd, cwd=repo)
    if code != 0:
        raise SystemExit(code)

    print(f"=== sleep {args.wait_seconds}s ===")
    time.sleep(args.wait_seconds)

    latest_cmd = [
        "go",
        "run",
        "./cmd/glaw",
        "mail",
        "latest",
        "--sender",
        args.reply_sender,
    ]
    print("=== latest ===")
    print(" ".join(latest_cmd))
    code = run(latest_cmd, cwd=repo)
    if code != 0:
        raise SystemExit(code)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
