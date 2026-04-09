# Log Observer

This reuses the old `cloudflare-executor/` worker deployment as a log observer channel.

## Goal

- remote machine uploads one zip per service every hour
- bundles land in R2
- KV stores only recent metadata for browsing
- local operators can list and download bundles on demand

## Services

Remote log paths come from [remote-state.md](./remote-state.md):

- `shuyao`
  - `C:\Users\cmwh\g-claw\logs\start-shuyao.log`
  - `C:\Users\cmwh\g-claw\logs\glaw-stdout.log`
  - `C:\Users\cmwh\g-claw\logs\glaw-stderr.log`
- `mail-executor`
  - `C:\Users\cmwh\claw-life-saver\logs\upgrade-claw-life-saver.log`
  - `C:\Users\cmwh\claw-life-saver\logs\claw-life-saver-stdout.log`
  - `C:\Users\cmwh\claw-life-saver\logs\claw-life-saver-stderr.log`

## HTTP API

- `POST /logs/upload`
- `GET /logs/index?host=<host>&service=<service>&limit=<n>`
- `GET /logs/latest?host=<host>&service=<service>`
- `GET /logs/object?key=<r2-key>`
- `POST /artifacts/upload`
- `GET /artifacts/object?key=<artifact-key>`

Auth remains:

- `Authorization: Bearer <EXECUTOR_TOKEN>`
- uploads return a 30-day signed `download_url`
- object download supports either bearer auth or a valid signed `download_url`
- artifact download does not support list/latest APIs; the caller must know the exact key

## Upload Payload

```json
{
  "host": "cmwh",
  "service": "shuyao",
  "timestamp": "2026-04-09T09:00:00Z",
  "archive_name": "shuyao-logs.zip",
  "archive_base64": "<base64 zip>",
  "content_type": "application/zip",
  "summary": {
    "changed_files": 2,
    "file_count": 3,
    "max_bytes": 262144
  }
}
```

Upload responses now also include:

- `download_url`
- `expires_at`

## R2 Keys

- `logs/<host>/<service>/YYYY/MM/DD/HH/<timestamp>_<archive_name>`
- `artifacts/<channel>/YYYY/MM/DD/HH/<timestamp>_<uuid>_<file_name>`

Current bucket policy:

- bucket `glaw-executor-results` expires all objects after 30 days

## KV Keys

- `log-index:<host>:<service>:latest`
- `log-index:<host>:<service>:recent`
- `log-index:<host>:<service>:<timestamp>`

## Remote Install Flow

1. Push repo changes.
2. Use the mail execution chain to run `scripts/install_remote_log_uploader.ps1`.
3. The script:
   - runs `git pull` in `~/glaw`
   - copies `scripts/upload_remote_logs.py` into `~/g-claw/scripts/`
   - writes one `hourly-log-upload` task into `~/g-claw/cron.json`
4. Legacy `claw_executor.py` cleanup is a separate mail-exec action:
   - observe first with `scripts/observe_legacy_claw_executor.ps1`
   - stop only after confirmation with `scripts/stop_legacy_claw_executor.ps1`

## Local Tools

- `python scripts/list_remote_logs.py --host cmwh --service shuyao`
- `python scripts/download_remote_log.py --host cmwh --service shuyao`
- `python scripts/fetch_remote_log_bundle.py --token <token> --host desktop-secpnpi --service shuyao`
- `python scripts/upload_artifact_bundle.py --channel glaw-log-observer --file scripts/upload_remote_logs.py --file scripts/install_remote_log_uploader.ps1`
