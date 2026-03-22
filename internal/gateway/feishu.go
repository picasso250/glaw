package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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

var feishuEventLogMu sync.Mutex

func StartFeishuLongConn(config FeishuConfig, db *sql.DB, dispatchCh chan struct{}) error {
	if !config.Enable {
		return nil
	}
	if strings.TrimSpace(config.AppID) == "" || strings.TrimSpace(config.AppSecret) == "" {
		return fmt.Errorf("FEISHU_APP_ID or FEISHU_APP_SECRET is empty")
	}

	handler := larkdispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			return handleFeishuEvent(config, db, dispatchCh, event)
		})

	client := larkws.NewClient(
		config.AppID,
		config.AppSecret,
		larkws.WithEventHandler(handler),
	)

	log.Printf("[feishu] [*] Starting long connection client")
	return client.Start(context.Background())
}

func handleFeishuEvent(config FeishuConfig, db *sql.DB, dispatchCh chan struct{}, event *larkim.P2MessageReceiveV1) error {
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

	body, err := extractFeishuMessageText(messageType, derefString(message.Content))
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
		Body:           body,
	})

	shouldDispatch := shouldDispatchFeishuMessage(chatType, message.Mentions)
	var archiveFile string
	if shouldDispatch {
		archiveFile, err = SavePendingMessage("feishu", messageID, senderOpenID, archiveContent, time.Now())
		if err != nil {
			return fmt.Errorf("save pending for %s: %w", messageID, err)
		}
	} else {
		archiveFile, err = SaveHistoryMessage("feishu", messageID, senderOpenID, archiveContent, time.Now())
		if err != nil {
			return fmt.Errorf("save history for %s: %w", messageID, err)
		}
	}

	if err := SaveMessageState(db, "feishu", messageID, senderOpenID, chatType, StateProcessed); err != nil {
		return fmt.Errorf("save state for %s: %w", messageID, err)
	}

	if shouldDispatch {
		log.Printf("[feishu] [*] Queued message %s for dispatch at %s", messageID, archiveFile)
		select {
		case dispatchCh <- struct{}{}:
		default:
		}
	} else {
		log.Printf("[feishu] [*] Archived message %s to %s", messageID, archiveFile)
	}
	return nil
}

func isSupportedFeishuMessageType(messageType string) bool {
	switch strings.TrimSpace(messageType) {
	case "text", "post":
		return true
	default:
		return false
	}
}

func extractFeishuMessageText(messageType, raw string) (string, error) {
	switch strings.TrimSpace(messageType) {
	case "text":
		return extractFeishuText(raw)
	case "post":
		return extractFeishuPostText(raw)
	default:
		return "", fmt.Errorf("unsupported message type %q", messageType)
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
