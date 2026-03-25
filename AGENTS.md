# AGENTS.md

- Gemini dispatch 必须单线程，绝不并行。
- 原因不是实现偏好，而是同一轮任务可能读写同一份仓库文件；并行会制造竞争、重复回复和不可预测副作用。
- 邮件与飞书都必须复用同一个串行 dispatch 入口；不要绕过 dispatch 直接并发拉起多个 agent。
