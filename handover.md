当前主程序已经统一为 `glaw`，`cmd/glaw/main.go` 同时承载 `serve` 和 `feishu list-messages` 子命令，旧的 `cmd/gateway`、`cmd/feishu-list-messages` 和 `cmd/debug-agent-cmd` 已删除并已推送到 `origin/main`。 
配置加载机制目前已恢复为 key 级覆盖：先读 `~/.env`，再从当前运行目录一路向上读各级 `.env`，后加载覆盖先加载，并额外处理了首行 UTF-8 BOM，最近一次相关提交已经推送。 
Agent 启动协议刚刚又简化了一次：`AGENT_PROMPT_ARG` 已删，`AGENT_CMD` 现在被当作“prompt 前面的完整命令前缀”，程序只会在最后追加 prompt 本身，因此 Gemini 应写成 `gemini --yolo -p`，opencode 应写成 `node ...\\opencode -m zhipuai-coding-plan/glm-5 run`；这一版代码已本地 `go build ./...` 通过，但尚未提交推送。 
运行目录方面，`dev.ps1` 现支持 `-RunDir` 且默认取当前目录，`~/glaw-ds` 已创建并从 `~/my-claw` 复制了 `gateway/`、若干 `.md/.txt` 文件以及一个本地 `.env`，其中 `AGENT_CMD` 已改成 node 直启 opencode 的 `run` 形式。 
下一步最合理的是先在 `~/glaw-ds` 下重新跑 `.\dev.ps1 -RunDir ~/glaw-ds` 验证 opencode 是否能正确吃到完整 prompt，如果正常再把这次“删除 AGENT_PROMPT_ARG 并简化 AGENT_CMD 约定”的改动提交并推送，然后再回头处理用户刚提到但尚未查看的 `https://termo.ai/skills/video-aroll-auto-editor` 技能前置条件问题。 
