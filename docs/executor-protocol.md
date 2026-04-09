# Log Observer Protocol

`cloudflare-executor/` is now repurposed as the remote log observer channel.

Detailed operator notes live in [log-observer.md](./log-observer.md).

## Fixed API

- `POST /logs/upload`
- `GET /logs/index`
- `GET /logs/latest`
- `GET /logs/object`

## Auth

- Use `Authorization: Bearer <EXECUTOR_TOKEN>`.
- Keep the token in a Worker secret on Cloudflare.
- On the remote machine, prefer a token file such as `~/.glaw-log-observer-token.txt`.

## Storage

- R2 stores zip bundles under `logs/<host>/<service>/...`
- KV stores recent metadata and latest pointers

## Remote Client

Use `scripts/upload_remote_logs.py` from the run directory cron entry. It uploads one bundle per service each hour and keeps a local state file so each bundle only includes new log bytes or the latest tail.
