package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

type FeishuConfig struct {
	Enable         bool
	AppID          string
	AppSecret      string
	AllowedOpenIDs []string
	AllowedChatIDs []string
}

type feishuTextContent struct {
	Text string `json:"text"`
}

type feishuPostContent struct {
	Title   string                  `json:"title"`
	Content [][][]feishuPostElement `json:"content"`
}

type feishuPostElement struct {
	Tag  string `json:"tag"`
	Text string `json:"text"`
}

type feishuImageContent struct {
	ImageKey string `json:"image_key"`
}

type feishuFileContent struct {
	FileKey  string `json:"file_key"`
	FileName string `json:"file_name"`
}

var feishuEventLogMu sync.Mutex

func StartFeishuLongConn(config FeishuConfig, db *sql.DB, dispatchCh chan DispatchRequest) error {
	if !config.Enable {
		return nil
	}
	if strings.TrimSpace(config.AppID) == "" || strings.TrimSpace(config.AppSecret) == "" {
		return fmt.Errorf("FEISHU_APP_ID or FEISHU_APP_SECRET is empty")
	}

	client := lark.NewClient(config.AppID, config.AppSecret)

	handler := larkdispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			return handleFeishuEvent(client, config, db, dispatchCh, event)
		})

	wsClient := larkws.NewClient(
		config.AppID,
		config.AppSecret,
		larkws.WithEventHandler(handler),
	)

	log.Printf("[feishu] [*] Starting long connection client")
	return wsClient.Start(context.Background())
}

func handleFeishuEvent(client *lark.Client, config FeishuConfig, db *sql.DB, dispatchCh chan DispatchRequest, event *larkim.P2MessageReceiveV1) error {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return nil
	}

	message := event.Event.Message
	sender := event.Event.Sender

	messageID := derefString(message.MessageId)
	messageType := derefString(message.MessageType)
	chatID := derefString(message.ChatId)
	chatType := derefString(message.ChatType)
	senderOpenID := ""
	senderType := ""
	if sender != nil {
		senderType = derefString(sender.SenderType)
		if sender.SenderId != nil {
			senderOpenID = derefString(sender.SenderId.OpenId)
		}
	}

	appendFeishuEventLog(
		"message_id=%s message_type=%s chat_type=%s chat_id=%s sender_type=%s sender_open_id=%s mentions=%d",
		messageID,
		messageType,
		chatType,
		chatID,
		senderType,
		senderOpenID,
		len(message.Mentions),
	)
	appendFeishuEventRawLog(event)

	if !isSupportedFeishuMessageType(messageType) {
		log.Printf("[feishu] [*] Ignoring unsupported message %s of type %s", messageID, messageType)
		return nil
	}

	if !isFeishuMessageAllowed(config, senderOpenID, chatID) {
		log.Printf("[feishu] [*] Ignoring message %s from sender %s in chat %s", messageID, senderOpenID, chatID)
		return nil
	}

	if _, err := LookupMessageState(db, "feishu", messageID); err == nil {
		return nil
	} else if err != sql.ErrNoRows {
		return fmt.Errorf("lookup state for %s: %w", messageID, err)
	}

	body, attachments, err := extractFeishuMessage(client, messageID, messageType, derefString(message.Content))
	if err != nil {
		return fmt.Errorf("parse message content for %s: %w", messageID, err)
	}

	msgTime := parseFeishuTime(derefString(message.CreateTime))
	archiveContent := BuildMessageArchiveContent(ArchivedMessage{
		Source:         "feishu",
		SenderName:     senderType,
		SenderID:       senderOpenID,
		ConversationID: chatID,
		Subject:        chatType,
		MessageID:      messageID,
		Date:           msgTime,
		Mentions:       formatFeishuMentions(message.Mentions),
		Body:           body,
		Attachments:    attachments,
	})

	shouldDispatch := shouldDispatchFeishuMessage(chatType, message.Mentions)
	if shouldDispatch {
		if err := SaveMessageState(db, "feishu", messageID, senderOpenID, chatType, StateProcessed); err != nil {
			return fmt.Errorf("save state for %s: %w", messageID, err)
		}
		log.Printf("[feishu] [*] Queued inline dispatch for message %s", messageID)
		select {
		case dispatchCh <- DispatchRequest{
			Type:    "feishu",
			Message: archiveContent,
		}:
		default:
			log.Printf("[feishu] [!] Dispatch channel full, dropping inline message %s", messageID)
		}
	} else {
		log.Printf("[feishu] [*] Ignored message %s after filtering", messageID)
	}
	return nil
}

func isSupportedFeishuMessageType(messageType string) bool {
	switch strings.TrimSpace(messageType) {
	case "text", "post", "image", "file":
		return true
	default:
		return false
	}
}

func extractFeishuMessage(client *lark.Client, messageID, messageType, raw string) (string, []string, error) {
	switch strings.TrimSpace(messageType) {
	case "text":
		body, err := extractFeishuText(raw)
		return body, nil, err
	case "post":
		body, err := extractFeishuPostText(raw)
		return body, nil, err
	case "image":
		path, err := downloadFeishuImageMessage(client, messageID, raw)
		if err != nil {
			return "", nil, err
		}
		return "[Feishu image]", []string{path}, nil
	case "file":
		body, path, err := downloadFeishuFileMessage(client, messageID, raw)
		if err != nil {
			return "", nil, err
		}
		return body, []string{path}, nil
	default:
		return "", nil, fmt.Errorf("unsupported message type %q", messageType)
	}
}

func extractFeishuText(raw string) (string, error) {
	var content feishuTextContent
	if err := json.Unmarshal([]byte(raw), &content); err != nil {
		return "", err
	}
	return strings.TrimSpace(content.Text), nil
}

func extractFeishuPostText(raw string) (string, error) {
	var content feishuPostContent
	if err := json.Unmarshal([]byte(raw), &content); err != nil {
		return "", err
	}

	var parts []string
	if strings.TrimSpace(content.Title) != "" {
		parts = append(parts, strings.TrimSpace(content.Title))
	}
	for _, localeBlocks := range content.Content {
		for _, line := range localeBlocks {
			var lineParts []string
			for _, element := range line {
				if element.Tag == "text" && strings.TrimSpace(element.Text) != "" {
					lineParts = append(lineParts, strings.TrimSpace(element.Text))
				}
			}
			if len(lineParts) > 0 {
				parts = append(parts, strings.Join(lineParts, ""))
			}
		}
	}

	return strings.TrimSpace(strings.Join(parts, "\n")), nil
}

func downloadFeishuImageMessage(client *lark.Client, messageID, raw string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("feishu client is nil")
	}

	var content feishuImageContent
	if err := json.Unmarshal([]byte(raw), &content); err != nil {
		return "", err
	}
	if strings.TrimSpace(content.ImageKey) == "" {
		return "", fmt.Errorf("image_key is empty")
	}

	path, err := downloadFeishuMessageResource(client, messageID, content.ImageKey, "image", "")
	if err == nil {
		return path, nil
	}

	// Fall back to direct image download for bot-uploaded images.
	resp, getErr := client.Im.V1.Image.Get(context.Background(), larkim.NewGetImageReqBuilder().
		ImageKey(content.ImageKey).
		Build())
	if getErr != nil {
		return "", fmt.Errorf("download image resource: %w (fallback get image: %v)", err, getErr)
	}
	if !resp.Success() {
		return "", fmt.Errorf("download image resource: %w (fallback get image failed: code=%d msg=%s)", err, resp.Code, resp.Msg)
	}

	return saveFeishuMedia(resp.File, resp.FileName, "feishu_image")
}

func downloadFeishuFileMessage(client *lark.Client, messageID, raw string) (string, string, error) {
	if client == nil {
		return "", "", fmt.Errorf("feishu client is nil")
	}

	var content feishuFileContent
	if err := json.Unmarshal([]byte(raw), &content); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(content.FileKey) == "" {
		return "", "", fmt.Errorf("file_key is empty")
	}

	path, err := downloadFeishuMessageResource(client, messageID, content.FileKey, "file", content.FileName)
	if err != nil {
		return "", "", err
	}

	body := "[Feishu file]"
	if strings.TrimSpace(content.FileName) != "" {
		body = "[Feishu file] " + strings.TrimSpace(content.FileName)
	}
	return body, path, nil
}

func downloadFeishuMessageResource(client *lark.Client, messageID, key, resourceType, fallbackName string) (string, error) {
	resp, err := client.Im.V1.MessageResource.Get(context.Background(), larkim.NewGetMessageResourceReqBuilder().
		MessageId(messageID).
		FileKey(key).
		Type(resourceType).
		Build())
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", fmt.Errorf("code=%d msg=%s", resp.Code, resp.Msg)
	}

	return saveFeishuMedia(resp.File, coalesceStrings(resp.FileName, fallbackName), "feishu_"+resourceType)
}

func saveFeishuMedia(reader io.Reader, fileName, prefix string) (string, error) {
	if err := os.MkdirAll(MediaDir, 0755); err != nil {
		return "", err
	}

	baseName := sanitizeMediaFilename(fileName)
	if baseName == "" {
		baseName = prefix + ".bin"
	}

	ext := filepath.Ext(baseName)
	nameOnly := strings.TrimSuffix(baseName, ext)
	savedName := fmt.Sprintf("%s_%d_%s%s", prefix, time.Now().UnixNano(), sanitizePathToken(nameOnly), ext)
	savedPath := filepath.Join(MediaDir, savedName)

	f, err := os.Create(savedPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := io.Copy(f, reader); err != nil {
		return "", err
	}
	return filepath.ToSlash(savedPath), nil
}

func sanitizeMediaFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = filepath.Base(name)
	name = strings.NewReplacer(":", "-", "/", "-", "\\", "-", "\t", "_", "\n", "_", "\r", "_").Replace(name)
	return name
}

func coalesceStrings(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseFeishuTime(values ...string) time.Time {
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		if ts, err := time.Parse(time.RFC3339, raw); err == nil {
			return ts
		}

		if unixMillis, err := parseUnixMillis(raw); err == nil {
			return unixMillis
		}
	}
	return time.Now()
}

func parseUnixMillis(raw string) (time.Time, error) {
	var millis int64
	if _, err := fmt.Sscanf(raw, "%d", &millis); err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(millis), nil
}

func isAllowed(allowlist []string, value string) bool {
	if len(allowlist) == 0 {
		return false
	}
	value = strings.TrimSpace(value)
	for _, allowed := range allowlist {
		if value == allowed {
			return true
		}
	}
	return false
}

func isFeishuMessageAllowed(config FeishuConfig, senderOpenID, chatID string) bool {
	if len(config.AllowedOpenIDs) == 0 && len(config.AllowedChatIDs) == 0 {
		return true
	}
	if isAllowed(config.AllowedOpenIDs, senderOpenID) {
		return true
	}
	if isAllowed(config.AllowedChatIDs, chatID) {
		return true
	}
	return false
}

func shouldDispatchFeishuMessage(chatType string, mentions []*larkim.MentionEvent) bool {
	if chatType == "p2p" {
		return true
	}
	if chatType != "group" && chatType != "topic_group" {
		return false
	}
	return len(mentions) > 0
}

func formatFeishuMentions(mentions []*larkim.MentionEvent) []string {
	var results []string
	for _, mention := range mentions {
		if mention == nil {
			continue
		}

		parts := []string{}
		if key := strings.TrimSpace(derefString(mention.Key)); key != "" {
			parts = append(parts, key)
		}
		if name := strings.TrimSpace(derefString(mention.Name)); name != "" {
			parts = append(parts, name)
		}

		openID := ""
		if mention.Id != nil {
			openID = strings.TrimSpace(derefString(mention.Id.OpenId))
		}
		if openID != "" {
			parts = append(parts, "<"+openID+">")
		}

		if len(parts) > 0 {
			results = append(results, strings.Join(parts, " "))
		}
	}
	return results
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func appendFeishuEventLog(format string, args ...any) {
	feishuEventLogMu.Lock()
	defer feishuEventLogMu.Unlock()

	if err := os.MkdirAll(LogsDir, 0755); err != nil {
		log.Printf("[feishu] [!] create log dir failed: %v", err)
		return
	}

	logPath := filepath.Join(LogsDir, "feishu_events.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("[feishu] [!] open event log failed: %v", err)
		return
	}
	defer f.Close()

	line := fmt.Sprintf("%s [feishu] [event] %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
	if _, err := f.WriteString(line); err != nil {
		log.Printf("[feishu] [!] write event log failed: %v", err)
	}
}

func appendFeishuEventRawLog(event *larkim.P2MessageReceiveV1) {
	feishuEventLogMu.Lock()
	defer feishuEventLogMu.Unlock()

	if err := os.MkdirAll(LogsDir, 0755); err != nil {
		log.Printf("[feishu] [!] create log dir failed: %v", err)
		return
	}

	body, err := json.Marshal(event)
	if err != nil {
		log.Printf("[feishu] [!] marshal raw event failed: %v", err)
		return
	}

	logPath := filepath.Join(LogsDir, "feishu_events_raw.jsonl")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("[feishu] [!] open raw event log failed: %v", err)
		return
	}
	defer f.Close()

	if _, err := f.Write(append(body, '\n')); err != nil {
		log.Printf("[feishu] [!] write raw event log failed: %v", err)
	}
}
