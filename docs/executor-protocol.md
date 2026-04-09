# Log Observer Protocol

`cloudflare-executor/` is now repurposed as the remote log observer channel.

Detailed operator notes live in [log-observer.md](./log-observer.md).

## Fixed API

- `POST /logs/upload`
- `GET /logs/index`
- `GET /logs/latest`
- `GET /logs/object`
- `POST /artifacts/upload`
- `GET /artifacts/object`

## Auth

- Use `Authorization: Bearer <EXECUTOR_TOKEN>`.
- Keep the token in a Worker secret on Cloudflare.
- On the remote machine, prefer a token file such as `~/.glaw-log-observer-token.txt`.

## Storage

- R2 stores zip bundles under `logs/<host>/<service>/...`
- R2 also stores ad hoc deployment bundles under `artifacts/<channel>/...`
- KV stores recent metadata and latest pointers

## Artifact Safety

- There is no `latest`, list, or prefix browsing endpoint for artifacts.
- Download requires both a valid bearer token and the exact `artifact key`.
- Upload returns the exact `key` and `sha256`; pass both to the remote side over the trusted mail execution channel.

## Remote Client

Use `scripts/upload_remote_logs.py` from the run directory cron entry. It uploads one bundle per service each hour and keeps a local state file so each bundle only includes new log bytes or the latest tail.
