# glaw

`glaw` is a small gateway that reads trusted mail or Feishu messages and dispatches work to an external assistant command.  
`glaw` 是一个小型网关，用来读取受信任的邮件或飞书消息，并把任务分发给外部 assistant 命令。

## Core Rules / 核心规则

- Dispatch stays single-threaded.  
  dispatch 必须保持单线程。
- Email and Feishu share the same serialized dispatch path.  
  邮件和飞书必须复用同一个串行 dispatch 入口。
- The process should run from the repo root so it can access `gateway/` and `INIT.md`.  
  进程应从仓库根目录启动，这样才能访问 `gateway/` 和 `INIT.md`。

## Config / 配置

Copy `.env.example` to `.env`.  
把 `.env.example` 复制为 `.env`。

Required mail fields / 邮件必填项:

- `MAIL_USER`
- `MAIL_PASS`
- `MAIL_IMAP_SERVER`
- `AGENT_CMD`

Optional / 可选项:

- `MAIL_SMTP_SERVER`
- `MAIL_SMTP_PORT`
- `FEISHU_APP_ID`
- `FEISHU_APP_SECRET`
- `FEISHU_ALLOWED_OPEN_IDS`
- `FEISHU_ALLOWED_CHAT_IDS`

Mail sender allowlist is read from `mail_filter_senders.txt`, one sender per line. Blank lines and `#` comments are ignored.  
邮件发件人白名单从 `mail_filter_senders.txt` 读取，每行一个地址；空行和 `#` 注释会被忽略。

## Main Commands / 主要命令

Build / 编译:

```powershell
go build ./...
```

Run the gateway / 启动网关:

```powershell
go run ./cmd/glaw serve
```

Read only the latest mail from one sender / 只读取某个发件人的最新一封邮件:

```powershell
go run ./cmd/glaw mail latest --sender cjwhshuyao@163.com
```

Use explicit config files / 显式指定配置文件:

```powershell
go run ./cmd/glaw serve --env .\.env --mail-filter .\mail_filter_senders.txt --cron-config .\cron.json
```

Print the effective `serve` config and exit / 打印最终生效配置后退出:

```powershell
go run ./cmd/glaw serve --env .\.env --mail-filter .\mail_filter_senders.txt --cron-config .\cron.json --dry-run
```

Run one prompt through the configured assistant and exit / 用当前 assistant 配置执行一次 prompt 后退出:

```powershell
go run ./cmd/glaw serve --run-prompt "say hello"
```

Build a local executable / 编译本地可执行文件:

```powershell
go build -buildvcs=false -o ~/bin/glaw.exe ./cmd/glaw
```

## Cron / 定时任务

`serve` watches `cron.json` by default. If the file does not exist, the scheduler stays idle.  
`serve` 默认会监听 `cron.json`；如果文件不存在，scheduler 会保持空闲。

Supported schedules / 支持的调度:

- `hourly`
- `daily`

Supported task types / 支持的任务类型:

- `program` or empty `type`
- `ai`

Helpers / 辅助命令:

```powershell
go run ./cmd/glaw cron list --cron-config .\cron.json
go run ./cmd/glaw cron check --cron-config .\cron.json
go run ./cmd/glaw cron run --cron-config .\cron.json -name "daily-summary"
```

## Feishu / 飞书

When `FEISHU_APP_ID` and `FEISHU_APP_SECRET` are set, `glaw` starts a Feishu long-connection bot.  
当设置了 `FEISHU_APP_ID` 和 `FEISHU_APP_SECRET` 后，`glaw` 会启动飞书长连接 bot。

Current behavior / 当前行为:

- group plain text / image / file messages: ignore after raw logging  
  群聊普通文本 / 图片 / 文件消息：只做原始日志记录，不进入 dispatch
- group `@bot` text / image / file messages: dispatch inline  
  群聊中 `@bot` 的文本 / 图片 / 文件消息：直接 inline dispatch
- p2p text / image / file messages: dispatch inline  
  私聊文本 / 图片 / 文件消息：直接 inline dispatch

Useful helper commands / 常用辅助命令:

```powershell
~/bin/glaw.exe feishu list-messages -chat-id <chat_id> -page-size 20 -minutes 180
~/bin/glaw.exe feishu send -message-id <message_id> -text "收到，我去处理"
~/bin/glaw.exe feishu send -message-id <message_id> -image .\answer.png
~/bin/glaw.exe feishu send -message-id <message_id> -file .\answer.docx
```

## Mail Executor / 邮件执行链

`glaw serve --exec-subject-keyword <keyword>` can bypass normal dispatch for trusted senders and execute one attached `.py` or `.ps1`.  
`glaw serve --exec-subject-keyword <keyword>` 可以对受信任发件人绕过普通 dispatch，直接执行一个附件 `.py` 或 `.ps1`。

Current remote execution flow uses `claw-life-saver` as the main keyword.  
当前远端执行流默认使用 `claw-life-saver` 作为主关键词。

Behavior / 行为:

- older remote versions reply with separate `stdout.txt` and `stderr.txt`  
  旧版远端会分别回 `stdout.txt` 和 `stderr.txt`
- newer remote versions can parse absolute file paths from the body and return one zip attachment containing `stdout`, `stderr`, and requested files  
  新版远端可以解析正文中的绝对路径，并返回一个 zip，里面包含 `stdout`、`stderr` 和请求的文件

## Related Paths / 相关路径

- main CLI: `cmd/glaw/main.go`
- mail execution: `cmd/glaw/mail_exec.go`
- dispatcher: `internal/gateway/dispatch.go`
- mail executor skill: `.agents/skills/mail-script-executor/`
- Cloudflare log observer worker: `cloudflare-executor/`

## Remote Log Observer / 远端日志观测

The Cloudflare worker in `cloudflare-executor/` is now used as a remote log observer, not as a task executor.  
`cloudflare-executor/` 现在用于远端日志观测，不再承担任务执行队列。

Remote uploader / 远端上传脚本:

- `scripts/upload_remote_logs.py`

Local inspection helpers / 本地查看脚本:

- `scripts/list_remote_logs.py`
- `scripts/download_remote_log.py`
- `scripts/fetch_remote_log_bundle.py`
- `scripts/upload_artifact_bundle.py`

Uploads now return a 30-day signed `download_url`, so remote install flows can fetch one bundle without needing the long-lived admin token.

Remote install helper / 远端安装脚本:

- `scripts/install_remote_log_uploader.ps1`

Reference docs / 参考文档:

- `docs/log-observer.md`
