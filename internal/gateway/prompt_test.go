package gateway

import (
	"strings"
	"testing"
)

func TestBuildBatchPrompt_EmailIncludesExecutionProtocol(t *testing.T) {
	got := buildBatchPrompt("C:\\init.md", []string{"a.txt"}, nil, nil)

	for _, want := range []string{
		"必须直接执行",
		"先生成对应文件，再发送",
		"邮件附件必须使用正确文件名和扩展名发送",
		"不要再次向发件人索取邮件里已有的信息",
		"默认先执行动作",
		".docx、.xlsx、.pdf 不得变成无扩展名或 .bin",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("email prompt missing %q\nprompt:\n%s", want, got)
		}
	}
}

func TestBuildBatchPrompt_FeishuIncludesExecutionProtocol(t *testing.T) {
	got := buildBatchPrompt("C:\\init.md", nil, []string{"hello"}, nil)

	for _, want := range []string{
		"必须直接基于当前消息执行",
		"不要把消息正文再反过来向用户索取一遍",
		"~/bin/glaw.exe feishu list-messages",
		"~/bin/glaw.exe feishu send -message-id <原消息MessageID> -file <本地文件路径>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("feishu prompt missing %q\nprompt:\n%s", want, got)
		}
	}
}

func TestBuildBatchPrompt_AIPromptIncludesExecutionProtocol(t *testing.T) {
	got := buildBatchPrompt("C:\\init.md", nil, nil, []string{"read INIT.md and DREAM.md and do"})

	for _, want := range []string{
		"把这些提示词视为已经下达的任务本身",
		"不要再反问",
		"若提示词要求整理、生成、归档、发送、汇总，就直接执行对应动作",
		"默认先执行动作",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("ai prompt missing %q\nprompt:\n%s", want, got)
		}
	}
}
