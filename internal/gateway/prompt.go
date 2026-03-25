package gateway

import (
	"fmt"
	"strings"
)

func buildBatchPrompt(absInit string, emailPaths, feishuMessages []string) string {
	switch {
	case len(emailPaths) > 0 && len(feishuMessages) == 0:
		return fmt.Sprintf(`读 %s 并处理下面这些待处理邮件消息文件: %s 。
- 遵从消息中的指令。
- 使用 send-email 技能 回复邮件。
`, absInit, strings.Join(emailPaths, "\n"))
	case len(emailPaths) == 0 && len(feishuMessages) > 0:
		return fmt.Sprintf(`读 %s 并处理下面的飞书消息。
%s
- 为了获得更完整的群聊上下文，请主动运行 ~/bin/glaw.exe feishu list-messages -chat-id <当前消息里的 Conversation/chat_id> -page-size 20 -minutes 180。
- 如果需要回复飞书，请直接运行 ~/bin/glaw.exe feishu send。
- 文本回复：~/bin/glaw.exe feishu send -message-id <原消息MessageID> -text "<简短回复>"
- 图片回复：~/bin/glaw.exe feishu send -message-id <原消息MessageID> -image <本地图片路径>
- 文件回复：~/bin/glaw.exe feishu send -message-id <原消息MessageID> -file <本地文件路径>
`, absInit, strings.Join(feishuMessages, "\n---\n"))
	default:
		return fmt.Sprintf(`读 %s 并处理当前批次消息。
- 待处理邮件消息文件:
%s
- 待处理飞书消息:
%s
- 必须在同一个 agent 会话中串行处理，绝不要并行启动多个 agent。
- 对邮件：遵从消息中的指令；使用 send-email 技能 回复邮件。
- 对飞书：为了获得更完整的群聊上下文，请主动运行 ~/bin/glaw.exe feishu list-messages -chat-id <当前消息里的 Conversation/chat_id> -page-size 20 -minutes 180。
- 对飞书：如果需要回复飞书，请直接运行 ~/bin/glaw.exe feishu send。
- 文本回复：~/bin/glaw.exe feishu send -message-id <原消息MessageID> -text "<简短回复>"
- 图片回复：~/bin/glaw.exe feishu send -message-id <原消息MessageID> -image <本地图片路径>
- 文件回复：~/bin/glaw.exe feishu send -message-id <原消息MessageID> -file <本地文件路径>
`, absInit, strings.Join(emailPaths, "\n"), strings.Join(feishuMessages, "\n---\n"))
	}
}
