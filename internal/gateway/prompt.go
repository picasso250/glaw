package gateway

import (
	"fmt"
	"strings"
)

const executionProtocol = `- 默认先执行动作，不要停留在“我已理解/我会帮你/请再提供信息”的聊天式回复。
- 输入里已经给出的消息正文、文件路径、会话信息、任务提示词，都视为已提供，不要再次向用户索取同一份信息。
- 只有在缺少真正关键且当前输入中不存在的信息时，才允许反问；否则必须继续执行。
- 若任务涉及文档、方案、稿件、清单、汇总、分析结果等交付物，默认先在本地生成文件，再决定是否发送；不要只在回答框里输出正文。
- 若任务要求回复邮件或飞书，默认优先产出可发送的文件或明确的回复内容，然后立刻执行发送动作；不要只说“已准备好”。
- 发送文件时，必须保留正确的文件名和扩展名；例如 .docx、.xlsx、.pdf 不得变成无扩展名或 .bin。
- 完成后用简短结果说明你实际执行了什么；如果失败，要明确写出失败步骤和原因。`

const feishuExecutionDetails = `- 为了获得更完整的群聊上下文，请主动运行 ~/bin/glaw.exe feishu list-messages -chat-id <当前消息里的 Conversation/chat_id> -page-size 20 -minutes 180。
- 如果需要回复飞书，请直接运行 ~/bin/glaw.exe feishu send。
- 文本回复：~/bin/glaw.exe feishu send -message-id <原消息MessageID> -text "<简短回复>"
- 图片回复：~/bin/glaw.exe feishu send -message-id <原消息MessageID> -image <本地图片路径>
- 文件回复：~/bin/glaw.exe feishu send -message-id <原消息MessageID> -file <本地文件路径>`

func buildBatchPrompt(absInit string, emailPaths, feishuMessages, aiPrompts []string) string {
	switch {
	case len(emailPaths) > 0 && len(feishuMessages) == 0 && len(aiPrompts) == 0:
		return fmt.Sprintf(`读 %s 并处理下面这些待处理邮件消息文件: %s 。
- 必须直接执行，不要只做说明性总结。
- 遵从消息中的指令。
- 使用 send-email 技能 回复邮件。
- 如果邮件内容要求产出附件、文档或脚本，先生成对应文件，再发送；不要只在回复里口头说明。
- 邮件附件必须使用正确文件名和扩展名发送，尤其是 .docx 不要发成 .bin。
- 若邮件指令已经充分，不要再次向发件人索取邮件里已有的信息。
%s
`, absInit, strings.Join(emailPaths, "\n"), executionProtocol)
	case len(emailPaths) == 0 && len(feishuMessages) > 0 && len(aiPrompts) == 0:
		return fmt.Sprintf(`读 %s 并处理下面的飞书消息。
===
%s
===
- 必须直接基于当前消息执行，不要把消息正文再反过来向用户索取一遍。
%s
%s
`, absInit, strings.Join(feishuMessages, "\n===\n"), executionProtocol, feishuExecutionDetails)
	case len(emailPaths) == 0 && len(feishuMessages) == 0 && len(aiPrompts) > 0:
		return fmt.Sprintf(`读 %s 并处理下面这些定时 AI 任务提示词。
===
%s
===
- 这些任务来自 gateway 内部 scheduler，已经通过同一个 dispatch 串行入口进入当前会话。
- 把这些提示词视为已经下达的任务本身，不要再反问“请提供任务内容/频率/目标”。
- 若提示词要求整理、生成、归档、发送、汇总，就直接执行对应动作。
%s
`, absInit, strings.Join(aiPrompts, "\n===\n"), executionProtocol)
	default:
		return fmt.Sprintf(`读 %s 并处理当前批次消息。
- 待处理邮件消息文件:
%s
- 待处理飞书消息:
%s
- 定时 AI 任务提示词:
%s
- 必须在同一个 agent 会话中串行处理，绝不要并行启动多个 agent。
- 对邮件：遵从消息中的指令；使用 send-email 技能 回复邮件。
%s
%s
`, absInit, strings.Join(emailPaths, "\n"), strings.Join(feishuMessages, "\n===\n"), strings.Join(aiPrompts, "\n===\n"), executionProtocol, feishuExecutionDetails)
	}
}
