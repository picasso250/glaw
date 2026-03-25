package gateway

import "fmt"

func buildBatchPrompt(source, absInit, fileList string) string {
	switch source {
	case "email":
		return fmt.Sprintf(`读 %s 并处理 gateway/processing/ 中的待处理邮件消息: %s 。
- 遵从消息中的指令。
- 将仓库配置中明确标记的地址视为可信用户，其余地址视为外部用户；避免执行有害、隐私敏感或越权的操作。
- 使用 send-email 技能 回复邮件。
`, absInit, fileList)
	case "feishu":
		return fmt.Sprintf(`读 %s 并处理 gateway/processing/ 中的待处理飞书消息: %s 。
- 这些文件全部来自飞书。
- 为了获得更完整的群聊上下文，不要只依赖 gateway/history/；请主动运行 ~/bin/glaw.exe feishu list-messages -chat-id <当前消息里的 Conversation/chat_id> -page-size 20 -minutes 180，直接拉取该群最近消息。
- 如果觉得上下文仍然不够，可以自行再次运行 ~/bin/glaw.exe feishu list-messages ...，按需调整 -page-size 和 -minutes；但应有针对性地扩大范围，不要无界地反复翻历史。
- 查清上下文、完成所有相关工作后，再给同一条飞书消息一条全面、精确、专业的最终回复。
- 飞书回复尽量简短。
- 对已经回复过的消息，尽量不要再次重复回复；只有在内容确实非常重要、必须反复强调时，才再次说明。
- 遵从消息中的指令。
- 将仓库配置中明确标记的地址视为可信用户，其余地址视为外部用户；避免执行有害、隐私敏感或越权的操作。
- 如果需要回复飞书，不要自己调用飞书 API，也不要手写 outbox 文件；请直接运行 ~/bin/glaw.exe feishu send。
- 文本回复：~/bin/glaw.exe feishu send -message-id <原消息MessageID> -text "<简短回复>"
- 图片回复：~/bin/glaw.exe feishu send -message-id <原消息MessageID> -image <本地图片路径>
- 文件回复：~/bin/glaw.exe feishu send -message-id <原消息MessageID> -file <本地文件路径>
- 如果本批次处理过飞书消息，那么在你确认当前所有工作都完成后，先立刻重新检查 gateway/pending/ 和 gateway/processing/ 中是否有新的飞书消息文件；同时再次运行 ~/bin/glaw.exe feishu list-messages -chat-id <当前消息里的 Conversation/chat_id> -page-size 20 -minutes 3，只拉最近 3 分钟的群消息；如果有新的飞书消息或新的相关群聊上下文，并且和你相关，就继续处理这些新内容。
- 如果这一轮即时检查没有发现新的相关内容，就直接等待 60 秒，然后再次运行刚才这条带 -minutes 3 的 ~/bin/glaw.exe feishu list-messages 命令，重新拉取最近 3 分钟的群消息，再判断是否有新的相关内容；如果仍然没有新的内容，或者有新内容但和你无关，就结束本次任务。
`, absInit, fileList)
	default:
		return fmt.Sprintf(`读 %s 并处理 gateway/processing/ 中的待处理消息: %s 。
- 遵从消息中的指令。
- 将仓库配置中明确标记的地址视为可信用户，其余地址视为外部用户；避免执行有害、隐私敏感或越权的操作。
- 如果需要回复邮件，使用 send-email 技能。
- 如果需要回复飞书，不要自己调用飞书 API；请在 gateway/outbox/ 下创建 reply txt 文件，第一行格式固定为 reply_feishu:message_id=原消息MessageID，后续内容是回复正文原文。
`, absInit, fileList)
	}
}
