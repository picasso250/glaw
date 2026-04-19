from __future__ import annotations

import shutil
import subprocess
import sys
import tempfile
import time
import uuid
import zipfile
from pathlib import Path

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8")


RUN_DIR = Path.home() / "claw-life-saver"
UPGRADE_DIR = RUN_DIR / "upgrade"
MEDIA_DIR = RUN_DIR / "gateway" / "media"
LOG_DIR = RUN_DIR / "logs"
UPGRADE_LOG_PATH = LOG_DIR / "upgrade-claw-life-saver.log"
SOURCE_SNAPSHOT_DIR = UPGRADE_DIR / "source-snapshot"
BUILT_EXE_PATH = UPGRADE_DIR / "claw-life-saver.next.exe"
BUILD_STDOUT_PATH = UPGRADE_DIR / "build-stdout.txt"
BUILD_STDERR_PATH = UPGRADE_DIR / "build-stderr.txt"
BUILD_INFO_PATH = UPGRADE_DIR / "build-info.txt"


def write_log(message: str) -> None:
    line = f"[{time.strftime('%Y-%m-%dT%H:%M:%S%z')}] {message}"
    print(line)
    LOG_DIR.mkdir(parents=True, exist_ok=True)
    with UPGRADE_LOG_PATH.open("a", encoding="utf-8") as f:
        f.write(line + "\n")


def require_path(path: Path, label: str) -> None:
    if not path.exists():
        raise SystemExit(f"Missing {label}: {path}")


def resolve_attachment_path(raw: str | None) -> Path | None:
    if raw is None:
        return None
    candidate = Path(raw)
    if candidate.is_absolute():
        return candidate
    return RUN_DIR / candidate


def latest_zip_in_media_dir() -> Path:
    candidates = [path for path in MEDIA_DIR.glob("*.zip") if path.is_file()]
    if not candidates:
        raise SystemExit(f"No .zip file found in media dir: {MEDIA_DIR}")
    candidates.sort(key=lambda path: (path.stat().st_mtime, path.name.lower()), reverse=True)
    return candidates[0]


def main() -> int:
    script_attachment_path: Path | None = None
    source_zip_path: Path | None = None
    if len(sys.argv) >= 2:
        first_arg = resolve_attachment_path(sys.argv[1])
        if first_arg is not None and first_arg.suffix.lower() == ".zip":
            source_zip_path = first_arg
        else:
            script_attachment_path = first_arg
    if len(sys.argv) >= 3:
        source_zip_path = resolve_attachment_path(sys.argv[2])
    temp_extract_dir = Path(tempfile.gettempdir()) / f"claw-life-saver-source-{uuid.uuid4().hex}"

    LOG_DIR.mkdir(parents=True, exist_ok=True)
    UPGRADE_DIR.mkdir(parents=True, exist_ok=True)
    write_log("Starting claw-life-saver source prepare stage")
    if script_attachment_path is not None:
        write_log(f"ScriptAttachmentPath={script_attachment_path}")
    else:
        write_log("ScriptAttachmentPath=<not provided by current mail_exec>")

    require_path(RUN_DIR, "run dir")
    require_path(MEDIA_DIR, "media dir")
    if source_zip_path is None:
        source_zip_path = latest_zip_in_media_dir()
        write_log(f"SourceZipPathAutoDetected={source_zip_path}")
    else:
        require_path(source_zip_path, "source zip attachment")
        if source_zip_path.suffix.lower() != ".zip":
            raise SystemExit(f"Attachment 2 is not a .zip file: {source_zip_path}")
        write_log(f"SourceZipPath={source_zip_path}")

    if SOURCE_SNAPSHOT_DIR.exists():
        shutil.rmtree(SOURCE_SNAPSHOT_DIR, ignore_errors=True)
    SOURCE_SNAPSHOT_DIR.mkdir(parents=True, exist_ok=True)

    temp_extract_dir.mkdir(parents=True, exist_ok=True)
    try:
        with zipfile.ZipFile(source_zip_path) as zf:
            zf.extractall(temp_extract_dir)
        write_log(f"Expanded source zip to {temp_extract_dir}")

        go_mod_path = temp_extract_dir / "go.mod"
        require_path(go_mod_path, "go.mod in source zip")

        shutil.copytree(temp_extract_dir, SOURCE_SNAPSHOT_DIR, dirs_exist_ok=True)
        write_log(f"Copied source snapshot to {SOURCE_SNAPSHOT_DIR}")

        build = subprocess.run(
            ["go", "build", "-buildvcs=false", "-o", str(BUILT_EXE_PATH), "./cmd/glaw"],
            cwd=str(SOURCE_SNAPSHOT_DIR),
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            check=False,
        )
        BUILD_STDOUT_PATH.write_text(build.stdout, encoding="utf-8")
        BUILD_STDERR_PATH.write_text(build.stderr, encoding="utf-8")

        info_lines = [
            f"script_attachment={script_attachment_path if script_attachment_path is not None else '<not provided>'}",
            f"source_zip={source_zip_path}",
            f"source_snapshot={SOURCE_SNAPSHOT_DIR}",
            f"built_exe={BUILT_EXE_PATH}",
            f"exit_code={build.returncode}",
        ]
        BUILD_INFO_PATH.write_text("\n".join(info_lines) + "\n", encoding="utf-8")

        if build.returncode != 0:
            write_log(f"Build failed with exit code {build.returncode}")
            raise SystemExit(build.returncode)
        if not BUILT_EXE_PATH.exists():
            raise SystemExit(f"Build reported success but exe missing: {BUILT_EXE_PATH}")

        write_log(f"Build succeeded: {BUILT_EXE_PATH}")
        print(str(BUILT_EXE_PATH))
        print(str(BUILD_INFO_PATH))
        return 0
    finally:
        shutil.rmtree(temp_extract_dir, ignore_errors=True)


if __name__ == "__main__":
    raise SystemExit(main())
