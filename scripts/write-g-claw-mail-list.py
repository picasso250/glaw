from __future__ import annotations

import pathlib
import sys
from datetime import datetime

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8")


def load_env_value(path: pathlib.Path, key: str) -> str | None:
    if not path.exists():
        return None
    for raw in path.read_text(encoding="utf-8", errors="replace").splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        current_key, value = line.split("=", 1)
        if current_key.strip() == key:
            return value.strip()
    return None


def main() -> int:
    home = pathlib.Path.home()
    source_env = home / ".env"
    run_dir = home / "g-claw"
    config_dir = run_dir / "config"
    list_path = config_dir / "mail.list"

    print(f"GeneratedAt: {datetime.now().astimezone().isoformat()}")
    print(f"SourceEnv: {source_env}")
    print(f"RunDir: {run_dir}")
    print(f"ConfigDir: {config_dir}")
    print(f"ListPath: {list_path}")
    print(f"SourceEnvExists: {source_env.exists()}")
    print(f"RunDirExists: {run_dir.exists()}")

    if not source_env.exists():
        print("ERROR: source env missing")
        return 1
    if not run_dir.exists():
        print("ERROR: g-claw run dir missing")
        return 1

    raw_value = load_env_value(source_env, "MAIL_FILTER_SENDER")
    print(f"MAIL_FILTER_SENDER found: {raw_value is not None}")
    if raw_value is None:
        print("ERROR: MAIL_FILTER_SENDER missing in ~/.env")
        return 1

    emails = [part.strip() for part in raw_value.split(",") if part.strip()]
    print("Emails:")
    for email in emails:
        print(email)

    config_dir.mkdir(parents=True, exist_ok=True)
    content = "\n".join(emails) + "\n"
    list_path.write_text(content, encoding="utf-8", newline="\n")

    print("\n===== Written mail.list =====")
    sys.stdout.write(list_path.read_text(encoding="utf-8"))
    print("OK: g-claw/config/mail.list written")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
