# Log Observer

This uses the `cloudflare-file/` worker deployment as a generic object channel, with logs as one naming convention on top.

## Goal

- remote machine uploads one zip per service every hour
- bundles land in R2 with deterministic hourly keys
- local operators can download bundles directly by key or signed URL

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

- `POST /objects/upload`
- `GET /objects/object?key=<object-key>`

Auth remains:

- `Authorization: Bearer <EXECUTOR_TOKEN>`
- uploads return a 30-day signed `download_url`
- object download supports either bearer auth or a valid signed `download_url`

## Upload Payload

```json
{
  "prefix": "logs/desktop-secpnpi/shuyao/2026/04/09/10",
  "timestamp": "2026-04-09T09:00:00Z",
  "file_name": "desktop-secpnpi__shuyao__2026-04-09__10.zip",
  "file_base64": "<base64 zip>",
  "content_type": "application/zip"
}
```

Upload responses now also include:

- `download_url`
- `expires_at`

## R2 Keys

- `logs/<host>/<service>/YYYY/MM/DD/HH/<host>__<service>__YYYY-MM-DD__HH.zip`
- `artifacts/<channel>/<file_name>`

Current bucket policy:

- bucket `glaw-executor-results` expires all objects after 30 days

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

- `python scripts/fetch_remote_log_bundle.py`
- `python scripts/download_object.py --key logs/... --output tmp\\one.zip`
- `python scripts/upload_artifact_bundle.py --channel glaw-log-observer --file scripts/upload_remote_logs.py`
