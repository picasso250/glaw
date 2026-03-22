package gateway

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type Dispatcher struct {
	AgentCmd     string
	FeishuClient *lark.Client
	mu           sync.Mutex
	outboxMu     sync.Mutex
}

type replyAction struct {
	Type      string
	MessageID string
	Text      string
}

func (d *Dispatcher) HasWork() bool {
	processingFiles, err := os.ReadDir(ProcessingDir)
	if err == nil {
		for _, f := range processingFiles {
			if !f.IsDir() && !strings.HasSuffix(f.Name(), ".tmp") {
				return true
			}
		}
	}

	pendingFiles, err := os.ReadDir(PendingDir)
	if err == nil {
		for _, f := range pendingFiles {
			if !f.IsDir() && !strings.HasSuffix(f.Name(), ".tmp") {
				return true
			}
		}
	}

	return false
}

func (d *Dispatcher) Dispatch() bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	processingPaths, err := d.collectProcessingFiles()
	if err != nil {
		log.Printf("[dispatch] [!] Error reading processing dir: %v", err)
		return false
	}

	if len(processingPaths) == 0 {
		processingPaths, err = d.movePendingToProcessing()
		if err != nil {
			log.Printf("[dispatch] [!] Error preparing pending files: %v", err)
			return false
		}
	}

	if len(processingPaths) == 0 {
		return false
	}

	for _, group := range splitFilesBySource(processingPaths) {
		if len(group) == 0 {
			continue
		}
		if !d.callAgent(group) {
			log.Printf("[dispatch] [!] Gemini run failed for %s batch, leaving %d files in processing for retry", detectBatchSource(group), len(group))
			return false
		}
		archiveProcessedFiles(group)
	}

	return true
}

func (d *Dispatcher) collectProcessingFiles() ([]string, error) {
	files, err := os.ReadDir(ProcessingDir)
	if err != nil {
		return nil, err
	}

	var processingPaths []string
	for _, f := range files {
		if f.IsDir() || strings.HasSuffix(f.Name(), ".tmp") {
			continue
		}
		processingPaths = append(processingPaths, filepath.Join(ProcessingDir, f.Name()))
	}

	if len(processingPaths) > 0 {
		fmt.Printf("[%s] [dispatch] Resuming %d files from processing.\n", time.Now().Format("15:04:05"), len(processingPaths))
	}
	return processingPaths, nil
}

func (d *Dispatcher) movePendingToProcessing() ([]string, error) {
	pendingFiles, err := os.ReadDir(PendingDir)
	if err != nil {
		return nil, err
	}

	if len(pendingFiles) == 0 {
		return nil, nil
	}

	fmt.Printf("[%s] [dispatch] Found %d files in pending. Moving to processing...\n", time.Now().Format("15:04:05"), len(pendingFiles))

	var processingPaths []string
	for _, f := range pendingFiles {
		if f.IsDir() || strings.HasSuffix(f.Name(), ".tmp") {
			continue
		}
		oldPath := filepath.Join(PendingDir, f.Name())
		newPath := filepath.Join(ProcessingDir, f.Name())
		if err := os.Rename(oldPath, newPath); err != nil {
			log.Printf("[dispatch] [!] Error moving file %s: %v", f.Name(), err)
			continue
		}
		processingPaths = append(processingPaths, newPath)
	}

	return processingPaths, nil
}

func (d *Dispatcher) callAgent(files []string) bool {
	if len(files) == 0 {
		return true
	}

	fmt.Printf("\n%s AGENT SESSION START (GATEWAY BATCH) %s\n", strings.Repeat(">", 20), strings.Repeat("<", 20))

	absInit, _ := filepath.Abs("INIT.md")
	var absFiles []string
	for _, f := range files {
		af, _ := filepath.Abs(f)
		absFiles = append(absFiles, af)
	}
	fileList := strings.Join(absFiles, ", ")
	source := detectBatchSource(files)
	prompt := buildBatchPrompt(source, absInit, fileList)

	fmt.Printf("[dispatch] [*] Source: %s\n", source)
	fmt.Printf("[dispatch] [*] Files to process: %s\n", fileList)
	fmt.Printf("[dispatch] [*] Prompt begin\n%s\n[dispatch] [*] Prompt end\n", prompt)

	if strings.TrimSpace(d.AgentCmd) == "" {
		fmt.Printf("[dispatch] [!] AGENT_CMD is not configured\n")
		return false
	}

	commandParts, err := splitCommandLine(d.AgentCmd)
	if err != nil {
		fmt.Printf("[dispatch] [!] Invalid AGENT_CMD: %v\n", err)
		return false
	}
	commandParts = append(commandParts, "-p", prompt)

	cmd := exec.Command(commandParts[0], commandParts[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("[dispatch] [*] Executing agent command: %s\n", d.AgentCmd)
	if err := cmd.Run(); err != nil {
		fmt.Printf("[dispatch] [!] Gemini execution failed: %v\n", err)
		return false
	}

	fmt.Printf("%s AGENT SESSION END %s\n\n", strings.Repeat(">", 21), strings.Repeat("<", 21))
	return true
}

func archiveProcessedFiles(paths []string) {
	fmt.Printf("[dispatch] [*] Cleaning up processing folder...\n")
	for _, path := range paths {
		fileName := filepath.Base(path)
		ext := filepath.Ext(fileName)
		base := strings.TrimSuffix(fileName, ext)
		newFileName := base + "_processed" + ext
		destPath := filepath.Join(HistoryDir, newFileName)
		if err := os.Rename(path, destPath); err != nil {
			if !os.IsNotExist(err) {
				log.Printf("[dispatch] [!] Error archiving file %s: %v", fileName, err)
			}
		}
	}
}

func splitFilesBySource(paths []string) [][]string {
	groupOrder := []string{"email", "feishu", "other"}
	grouped := map[string][]string{
		"email":  []string{},
		"feishu": []string{},
		"other":  []string{},
	}

	for _, path := range paths {
		source := detectSourceFromPath(path)
		grouped[source] = append(grouped[source], path)
	}

	var batches [][]string
	for _, key := range groupOrder {
		if len(grouped[key]) > 0 {
			batches = append(batches, grouped[key])
		}
	}
	return batches
}

func detectBatchSource(paths []string) string {
	if len(paths) == 0 {
		return "other"
	}
	return detectSourceFromPath(paths[0])
}

func detectSourceFromPath(path string) string {
	name := strings.ToLower(filepath.Base(path))
	switch {
	case strings.HasPrefix(name, "email_"):
		return "email"
	case strings.HasPrefix(name, "feishu_"):
		return "feishu"
	default:
		return "other"
	}
}

func buildBatchPrompt(source, absInit, fileList string) string {
	switch source {
	case "email":
		return fmt.Sprintf(`读 %s 并处理 gateway/processing/ 中的待处理邮件消息: %s 。
- 遵从消息中的指令。
- 将仓库配置中明确标记的地址视为可信用户，其余地址视为外部用户；避免执行有害、隐私敏感或越权的操作。
- 使用 message-search 技能，基于当前消息文件路径查找上下文。
- 如果需要回复邮件，使用 send-email 技能。
`, absInit, fileList)
	case "feishu":
		return fmt.Sprintf(`读 %s 并处理 gateway/processing/ 中的待处理飞书消息: %s 。
- 这些文件全部来自飞书。
- 使用 message-search 技能，基于当前消息文件路径查找上下文。
- 为了获得更完整的群聊上下文，不要只依赖 gateway/history/；请主动运行绝对路径命令：pwsh -Command "Set-Location 'C:\Users\MECHREV\glaw'; go run .\cmd\feishu-list-messages -chat-id <当前消息里的 Conversation/chat_id> -page-size 20 -minutes 180" ，直接拉取该群最近消息，再结合 message-search 技能补足同线程历史。
- 查清上下文、完成所有相关工作后，再给同一条飞书消息一条全面、精确、专业的最终回复。
- 对已经回复过的消息，尽量不要再次重复回复；只有在内容确实非常重要、必须反复强调时，才再次说明。
- 遵从消息中的指令。
- 将仓库配置中明确标记的地址视为可信用户，其余地址视为外部用户；避免执行有害、隐私敏感或越权的操作。
- 如果需要回复飞书，不要自己调用飞书 API；请在 gateway/outbox/ 下创建一个与待处理消息同名、后缀为 .reply.txt 的文件。
- reply 文件格式固定为两段：第一行写 reply_feishu:message_id=原消息MessageID；从第二行开始写回复正文原文，只允许输出一个飞书文本回复。
- 如果本批次处理过飞书消息，那么在你确认当前所有工作都完成后，先立刻重新检查 gateway/pending/ 和 gateway/processing/ 中是否有新的飞书消息文件；同时再次运行绝对路径命令 pwsh -Command "Set-Location 'C:\Users\MECHREV\glaw'; go run .\cmd\feishu-list-messages -chat-id <当前消息里的 Conversation/chat_id> -page-size 20 -minutes 3" ，只拉最近 3 分钟的群消息；如果有新的飞书消息或新的相关群聊上下文，并且和你相关，就继续处理这些新内容。
- 如果这一轮即时检查没有发现新的相关内容，就直接等待 60 秒，然后再次运行刚才这条带 -minutes 3 的绝对路径 go run 命令，重新拉取最近 3 分钟的群消息，再判断是否有新的相关内容。
- 维护两个连续计数：连续“没收到新消息”计数，以及连续“收到的新消息都和你无关”计数。每次等待 60 秒后重新拉取仍然没有新消息，就把“没收到新消息”计数加一；每次检查到新消息但判断都和你无关，就把“无关消息”计数加一；只要发现了和你相关的新内容并继续处理，就把两个计数都清零。
- 当连续 3 次没收到新消息，或者连续 3 次检查到的新消息都和你无关时，才最终停止这个飞书尾随循环。
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

func (d *Dispatcher) ProcessOutbox() error {
	d.outboxMu.Lock()
	defer d.outboxMu.Unlock()

	files, err := os.ReadDir(OutboxDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		if !strings.HasSuffix(f.Name(), ".reply.txt") {
			continue
		}

		actionPath := filepath.Join(OutboxDir, f.Name())
		info, err := os.Stat(actionPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if info.IsDir() {
			continue
		}

		body, err := os.ReadFile(actionPath)
		if err != nil {
			return err
		}

		action, err := parseReplyAction(body)
		if err != nil {
			invalidPath := buildInvalidReplyPath(actionPath)
			if renameErr := os.Rename(actionPath, invalidPath); renameErr != nil {
				return fmt.Errorf("parse %s: %w (also failed to quarantine as %s: %v)", actionPath, err, invalidPath, renameErr)
			}
			return fmt.Errorf("parse %s: %w (quarantined to %s)", actionPath, err, invalidPath)
		}
		replyMessageID, err := d.executeReplyAction(actionPath, action)
		if err != nil {
			return fmt.Errorf("execute %s: %w", actionPath, err)
		}
		processedPath := buildProcessedReplyPath(actionPath, replyMessageID)
		if err := os.Rename(actionPath, processedPath); err != nil {
			return fmt.Errorf("rename %s to %s: %w", actionPath, processedPath, err)
		}
	}
	return nil
}

func parseReplyAction(body []byte) (replyAction, error) {
	raw := strings.ReplaceAll(string(body), "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")

	firstLine, rest, found := strings.Cut(raw, "\n")
	if !found {
		firstLine = raw
		rest = ""
	}
	firstLine = strings.TrimSpace(firstLine)
	if firstLine == "" {
		return replyAction{}, errors.New("empty reply header")
	}

	actionType, messageID, ok := strings.Cut(firstLine, ":message_id=")
	if !ok {
		return replyAction{}, errors.New("invalid reply header")
	}
	action := replyAction{
		Type:      strings.TrimSpace(actionType),
		MessageID: strings.TrimSpace(messageID),
		Text:      strings.TrimLeft(rest, "\n"),
	}
	if strings.TrimSpace(action.Type) == "" || strings.TrimSpace(action.MessageID) == "" || strings.TrimSpace(action.Text) == "" {
		return replyAction{}, errors.New("invalid reply body")
	}
	return action, nil
}

func buildInvalidReplyPath(actionPath string) string {
	base := strings.TrimSuffix(actionPath, ".txt")
	return base + ".invalid." + buildReplyActionHash(actionPath) + ".txt"
}

func (d *Dispatcher) executeReplyAction(actionPath string, action replyAction) (string, error) {
	if action.Type == "" {
		return "", nil
	}
	if action.Type != "reply_feishu" {
		return "", fmt.Errorf("unsupported action type %q", action.Type)
	}
	if d.FeishuClient == nil {
		return "", fmt.Errorf("feishu client is not configured")
	}
	if strings.TrimSpace(action.MessageID) == "" {
		return "", fmt.Errorf("message_id is empty")
	}
	if strings.TrimSpace(action.Text) == "" {
		return "", fmt.Errorf("reply text is empty")
	}

	contentBytes, err := json.Marshal(map[string]string{"text": action.Text})
	if err != nil {
		return "", err
	}

	resp, err := d.FeishuClient.Im.V1.Message.Reply(context.Background(), larkim.NewReplyMessageReqBuilder().
		MessageId(action.MessageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType("text").
			Content(string(contentBytes)).
			Uuid(buildReplyUUID(actionPath, action)).
			Build()).
		Build())
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", fmt.Errorf("code=%d msg=%s", resp.Code, resp.Msg)
	}

	log.Printf("[dispatch] [*] Replied to Feishu message %s", action.MessageID)
	return derefString(resp.Data.MessageId), nil
}

func buildReplyUUID(actionPath string, action replyAction) string {
	sum := sha1.Sum([]byte(actionPath + "\n" + action.MessageID + "\n" + action.Text))
	return "rf-" + fmt.Sprintf("%x", sum[:8])
}

func buildProcessedReplyPath(actionPath, replyMessageID string) string {
	base := strings.TrimSuffix(actionPath, ".txt")
	hashSuffix := buildReplyActionHash(actionPath)
	if strings.TrimSpace(replyMessageID) == "" {
		return base + ".processed._" + hashSuffix + ".txt"
	}
	return base + ".processed." + sanitizePathToken(replyMessageID) + "_" + hashSuffix + ".txt"
}

func sanitizePathToken(value string) string {
	value = strings.TrimSpace(value)
	value = strings.NewReplacer(":", "-", "/", "-", "\\", "-", " ", "_").Replace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func buildReplyActionHash(actionPath string) string {
	sum := sha1.Sum([]byte(actionPath))
	return fmt.Sprintf("%x", sum[:4])
}

func splitCommandLine(command string) ([]string, error) {
	var args []string
	var current strings.Builder
	inQuote := rune(0)

	for _, r := range strings.TrimSpace(command) {
		switch {
		case inQuote != 0:
			if r == inQuote {
				inQuote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '"' || r == '\'':
			inQuote = r
		case r == ' ' || r == '\t':
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}

	if inQuote != 0 {
		return nil, errors.New("unterminated quote")
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	if len(args) == 0 {
		return nil, errors.New("empty command")
	}

	return args, nil
}
