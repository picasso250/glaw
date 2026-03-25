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

type DispatchRequest struct {
	Type    string
	Message string
}

type replyAction struct {
	Type      string
	MessageID string
	Payload   string
}

func formatReplyAction(action replyAction) ([]byte, error) {
	if strings.TrimSpace(action.Type) == "" || strings.TrimSpace(action.MessageID) == "" || strings.TrimSpace(action.Payload) == "" {
		return nil, errors.New("invalid reply action")
	}

	header := fmt.Sprintf("%s:message_id=%s", strings.TrimSpace(action.Type), strings.TrimSpace(action.MessageID))
	payload := strings.TrimLeft(strings.ReplaceAll(action.Payload, "\r\n", "\n"), "\n")
	return []byte(header + "\n" + payload + "\n"), nil
}

func (d *Dispatcher) DispatchBatch(requests []DispatchRequest) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	var emailPaths []string
	var feishuMessages []string
	for _, req := range requests {
		switch strings.TrimSpace(req.Type) {
		case "email":
			if strings.TrimSpace(req.Message) != "" {
				emailPaths = append(emailPaths, strings.TrimSpace(req.Message))
			}
		case "feishu":
			if strings.TrimSpace(req.Message) != "" {
				feishuMessages = append(feishuMessages, req.Message)
			}
		default:
			log.Printf("[dispatch] [!] Unknown dispatch request type %q", req.Type)
		}
	}

	if len(emailPaths) == 0 && len(feishuMessages) == 0 {
		return false
	}

	if !d.callAgentBatch(emailPaths, feishuMessages) {
		if len(emailPaths) > 0 {
			log.Printf("[dispatch] [!] Gemini run failed for batch with %d email files", len(emailPaths))
		} else {
			log.Printf("[dispatch] [!] Gemini run failed for feishu batch")
		}
		return false
	}
	return true
}

func (d *Dispatcher) callAgentBatch(emailPaths, feishuMessages []string) bool {
	if len(emailPaths) == 0 && len(feishuMessages) == 0 {
		return true
	}
	fmt.Printf("\n%s AGENT SESSION START (GATEWAY BATCH) %s\n", strings.Repeat(">", 20), strings.Repeat("<", 20))
	absInit, _ := filepath.Abs("INIT.md")
	var absFiles []string
	for _, f := range emailPaths {
		af, _ := filepath.Abs(f)
		absFiles = append(absFiles, af)
	}
	prompt := buildBatchPrompt(absInit, absFiles, feishuMessages)
	return d.executeAgentPrompt(classifyPromptSource(absFiles, feishuMessages), prompt, strings.Join(absFiles, "\n"), "")
}

func (d *Dispatcher) executeAgentPrompt(source, prompt, fileList, feishuSummary string) bool {
	fmt.Printf("[dispatch] [*] Source: %s\n", source)
	if strings.TrimSpace(fileList) != "" {
		fmt.Printf("[dispatch] [*] Files to process: %s\n", fileList)
	}
	if strings.TrimSpace(feishuSummary) != "" {
		fmt.Printf("[dispatch] [*] Feishu messages:\n%s\n", feishuSummary)
	}
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
	commandParts = append(commandParts, prompt)

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

func classifyPromptSource(emailPaths, feishuMessages []string) string {
	if len(emailPaths) > 0 && len(feishuMessages) == 0 {
		return "email"
	}
	if len(emailPaths) == 0 && len(feishuMessages) > 0 {
		return "feishu"
	}
	return "mixed"
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

func (d *Dispatcher) SubmitReplyAction(action replyAction) (string, string, error) {
	d.outboxMu.Lock()
	defer d.outboxMu.Unlock()

	if err := os.MkdirAll(OutboxDir, 0755); err != nil {
		return "", "", err
	}

	body, err := formatReplyAction(action)
	if err != nil {
		return "", "", err
	}

	actionPath := filepath.Join(OutboxDir, buildReplyActionFileName(action))
	if err := os.WriteFile(actionPath, body, 0644); err != nil {
		return "", "", err
	}

	replyMessageID, err := d.executeReplyAction(actionPath, action)
	if err != nil {
		failedPath := buildFailedReplyPath(actionPath)
		if renameErr := os.Rename(actionPath, failedPath); renameErr != nil {
			return "", "", fmt.Errorf("execute %s: %w (also failed to mark as failed: %v)", actionPath, err, renameErr)
		}
		return "", failedPath, fmt.Errorf("execute %s: %w", actionPath, err)
	}

	processedPath := buildProcessedReplyPath(actionPath, replyMessageID)
	if err := os.Rename(actionPath, processedPath); err != nil {
		return "", "", fmt.Errorf("rename %s to %s: %w", actionPath, processedPath, err)
	}
	return replyMessageID, processedPath, nil
}

func (d *Dispatcher) SubmitReply(actionType, messageID, payload string) (string, string, error) {
	return d.SubmitReplyAction(replyAction{
		Type:      actionType,
		MessageID: messageID,
		Payload:   payload,
	})
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
		Payload:   strings.TrimLeft(rest, "\n"),
	}
	if strings.TrimSpace(action.Type) == "" || strings.TrimSpace(action.MessageID) == "" || strings.TrimSpace(action.Payload) == "" {
		return replyAction{}, errors.New("invalid reply body")
	}
	return action, nil
}

func buildInvalidReplyPath(actionPath string) string {
	base := strings.TrimSuffix(actionPath, ".txt")
	return base + ".invalid." + buildReplyActionHash(actionPath) + ".txt"
}

func buildFailedReplyPath(actionPath string) string {
	base := strings.TrimSuffix(actionPath, ".txt")
	return base + ".failed." + buildReplyActionHash(actionPath) + ".txt"
}

func buildReplyActionFileName(action replyAction) string {
	return fmt.Sprintf(
		"manual_%s_%s_%s.reply.txt",
		sanitizePathToken(action.Type),
		time.Now().UTC().Format("2006-01-02T15-04-05Z"),
		sanitizePathToken(action.MessageID),
	)
}

func (d *Dispatcher) executeReplyAction(actionPath string, action replyAction) (string, error) {
	if action.Type == "" {
		return "", nil
	}
	if d.FeishuClient == nil {
		return "", fmt.Errorf("feishu client is not configured")
	}
	if strings.TrimSpace(action.MessageID) == "" {
		return "", fmt.Errorf("message_id is empty")
	}

	switch action.Type {
	case "reply_feishu":
		if strings.TrimSpace(action.Payload) == "" {
			return "", fmt.Errorf("reply text is empty")
		}
		contentBytes, err := json.Marshal(map[string]string{"text": action.Payload})
		if err != nil {
			return "", err
		}
		return d.replyFeishuMessage(action.MessageID, "text", string(contentBytes), buildReplyUUID(actionPath, action))
	case "reply_feishu_image":
		content, err := d.buildFeishuImageReplyContent(strings.TrimSpace(action.Payload))
		if err != nil {
			return "", err
		}
		return d.replyFeishuMessage(action.MessageID, "image", content, buildReplyUUID(actionPath, action))
	case "reply_feishu_file":
		content, err := d.buildFeishuFileReplyContent(strings.TrimSpace(action.Payload))
		if err != nil {
			return "", err
		}
		return d.replyFeishuMessage(action.MessageID, "file", content, buildReplyUUID(actionPath, action))
	default:
		return "", fmt.Errorf("unsupported action type %q", action.Type)
	}
}

func (d *Dispatcher) replyFeishuMessage(messageID, msgType, content, uuid string) (string, error) {
	resp, err := d.FeishuClient.Im.V1.Message.Reply(context.Background(), larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType(msgType).
			Content(content).
			Uuid(uuid).
			Build()).
		Build())
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", fmt.Errorf("code=%d msg=%s", resp.Code, resp.Msg)
	}
	log.Printf("[dispatch] [*] Replied to Feishu message %s with %s", messageID, msgType)
	return derefString(resp.Data.MessageId), nil
}

func (d *Dispatcher) buildFeishuImageReplyContent(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	resp, err := d.FeishuClient.Im.V1.Image.Create(context.Background(), larkim.NewCreateImageReqBuilder().
		Body(larkim.NewCreateImageReqBodyBuilder().
			ImageType("message").
			Image(f).
			Build()).
		Build())
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", fmt.Errorf("upload image failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data == nil || strings.TrimSpace(derefString(resp.Data.ImageKey)) == "" {
		return "", fmt.Errorf("upload image failed: empty image_key")
	}

	return (&larkim.MessageImage{ImageKey: derefString(resp.Data.ImageKey)}).String()
}

func (d *Dispatcher) buildFeishuFileReplyContent(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	fileName := filepath.Base(path)
	fileType := strings.TrimPrefix(strings.ToLower(filepath.Ext(fileName)), ".")
	if fileType == "" {
		fileType = "stream"
	}

	resp, err := d.FeishuClient.Im.V1.File.Create(context.Background(), larkim.NewCreateFileReqBuilder().
		Body(larkim.NewCreateFileReqBodyBuilder().
			FileType(fileType).
			FileName(fileName).
			File(f).
			Build()).
		Build())
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", fmt.Errorf("upload file failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data == nil || strings.TrimSpace(derefString(resp.Data.FileKey)) == "" {
		return "", fmt.Errorf("upload file failed: empty file_key")
	}

	return (&larkim.MessageFile{FileKey: derefString(resp.Data.FileKey)}).String()
}

func buildReplyUUID(actionPath string, action replyAction) string {
	sum := sha1.Sum([]byte(actionPath + "\n" + action.Type + "\n" + action.MessageID + "\n" + action.Payload))
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
