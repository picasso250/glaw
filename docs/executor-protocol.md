# Object Protocol

`cloudflare-file/` now exposes one generic object channel.

Detailed operator notes live in [log-observer.md](./log-observer.md).

## Fixed API

- `POST /objects/upload`
- `GET /objects/object`

## Auth

- Upload requires `Authorization: Bearer <EXECUTOR_TOKEN>`.
- Download supports either:
  - `Authorization: Bearer <EXECUTOR_TOKEN>`
  - a signed `download_url` returned at upload time

## Object Model

Upload payload fields:

- `prefix`
- `file_name`
- `file_base64`
- `content_type`
- `timestamp` (optional)

Upload response fields:

- `key`
- `sha256`
- `download_url`
- `expires_at`

## Naming

- artifacts and logs both use the same object API
- artifacts differ by chosen prefix and filename
- logs differ only by deterministic hourly naming, for example:
  - `logs/<host>/<service>/YYYY/MM/DD/HH/<host>__<service>__YYYY-MM-DD__HH.zip`

## Retention

- signed download URLs expire after 30 days by default
- bucket object lifecycle is also set to expire all objects after 30 days

## Remote Client

Use `scripts/upload_remote_logs.py` from the run directory cron entry. It uploads one bundle per service each hour and keeps a local state file so each bundle only includes new log bytes or the latest tail.
