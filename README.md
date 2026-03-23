# glaw

Minimal open-source starting point for a mail-driven assistant gateway.

## What it does

`glaw` polls an IMAP inbox or accepts Feishu bot messages over a long connection, archives matched content into `gateway/pending/`, and dispatches those files to an external assistant command.

The current implementation is intentionally small:

- `cmd/glaw/main.go`: unified CLI entrypoint
- `INIT.md`: initialization prompt file consumed by the assistant command
- `.env.example`: local configuration template

## Configuration

Copy `.env.example` to `.env` and fill in:

- `MAIL_USER`: IMAP login address
- `MAIL_PASS`: IMAP password or app password
- `MAIL_IMAP_SERVER`: IMAP host, for example `imap.example.com`
- `MAIL_FILTER_SENDER`: comma-separated trusted senders to process
- `AGENT_CMD`: assistant command prefix to execute; the gateway appends the prompt as the final argument. For example `gemini --yolo -p` or `node ...\\opencode -m zhipuai-coding-plan/glm-5 run`
- `FEISHU_APP_ID`: Feishu app ID for the bot-enabled custom app
- `FEISHU_APP_SECRET`: Feishu app secret for the bot-enabled custom app
- `FEISHU_ALLOWED_OPEN_IDS`: comma-separated trusted Feishu sender `open_id` values
- `FEISHU_ALLOWED_CHAT_IDS`: comma-separated trusted Feishu chat IDs

## Run

Build:

```powershell
go build ./...
```

Start:

```powershell
go run ./cmd/glaw serve
```

Temporarily override the assistant runner for one `serve` process:

```powershell
go run ./cmd/glaw serve --agent-cmd "gemini --yolo -p"
```

If the runner path itself contains spaces, wrap the whole argument in single quotes so PowerShell passes it through unchanged:

```powershell
go run ./cmd/glaw serve --agent-cmd '"C:\Program Files\nodejs\node.exe" C:\Users\MECHREV\AppData\Roaming\npm\node_modules\opencode-ai\bin\opencode -m zhipuai-coding-plan/glm-5 run'
```

Build a local executable:

```powershell
go build -o ~/bin/glaw.exe ./cmd/glaw
```

Then run it:

```powershell
~/bin/glaw.exe serve
```

Dev loop:

```powershell
.\dev.ps1
```

Dev loop with a one-off agent command override:

```powershell
.\dev.ps1 -AgentCmd "gemini --yolo -p"
```

The process expects to be started from the repository root so it can access `gateway/` and `INIT.md`.

## Feishu Bot

When both `FEISHU_APP_ID` and `FEISHU_APP_SECRET` are non-empty, the gateway starts a Feishu long-connection bot client using the official Go SDK. The current implementation handles inbound `im.message.receive_v1` text events.

Current Feishu routing rules:

- group plain text messages: archive only
- group `@bot` text messages: archive and dispatch
- p2p text messages: archive and dispatch

Required Feishu setup:

- enable the Bot ability for the app
- subscribe the `im.message.receive_v1` event and publish the app version
- grant `im:message:send_as_bot` so the gateway can reply as the bot
- grant `im:message.group_msg` if you want to receive normal group messages, not only `@bot` mentions
- grant the p2p message permission for bot chats if you want to receive all direct messages
- `im:message:readonly` is useful for message read access, but it does not replace the bot send scope above

If `im:message.group_msg` is missing, Feishu will typically only deliver group messages that explicitly mention the bot.

## Notes Before Open Source

- Replace the module path in `go.mod` with the final repository path.
- Review the prompt text in `internal/gateway/dispatch.go` for product-specific policy.
- The assistant command contract is still local and opinionated by design; if you want broader reuse, the next step is to abstract the assistant runner interface.

## Feishu Helper CLI

Use the built binary for ad hoc Feishu history pulls:

```powershell
~/bin/glaw.exe feishu list-messages -chat-id <chat_id> -page-size 20 -minutes 180
```

## Feishu Context Lessons

- Pulling extra Feishu group context inside the gateway is architecturally cleaner than asking the agent to run ad hoc shell commands, but it has an important tradeoff in the current runner model.
- Today the gateway launches the assistant through `AGENT_CMD` as a fresh CLI process each time. If the gateway performs a follow-up check and finds new context, the only available way to continue is to start a brand new assistant process again.
- In practice this is slow for heavy CLI runners such as Gemini CLI, because process startup itself can take around 20 seconds.
- It also means follow-up handling cannot reuse the previous assistant process memory or in-session reasoning state; it can only reconstruct context from files and prompt text.
- Because of that, keeping Feishu follow-up checks inside prompt instructions is currently simpler than moving them fully into gateway-triggered re-entry. A better long-term fix would be a persistent assistant session, a resumable runner protocol, or a local service API instead of one-shot CLI launches.
