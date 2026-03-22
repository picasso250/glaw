package gateway

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ArchivedEmail struct {
	FromName    string
	FromEmail   string
	Subject     string
	Date        time.Time
	Body        string
	ImageFiles  []string
	Attachments []string
}

type ArchivedMessage struct {
	Source         string
	SenderName     string
	SenderID       string
	ConversationID string
	Subject        string
	MessageID      string
	Date           time.Time
	Mentions       []string
	Body           string
	Attachments    []string
}

func EnsureRuntimeDirs() error {
	for _, dir := range []string{PendingDir, ProcessingDir, HistoryDir, MediaDir, OutboxDir, LogsDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

func BuildEmailArchiveContent(email ArchivedEmail) string {
	body := email.Body
	if len(email.ImageFiles) > 0 {
		body += "\n\nImages:\n"
		for _, img := range email.ImageFiles {
			body += fmt.Sprintf("- %s/%s\n", MediaDir, img)
		}
	}
	if len(email.Attachments) > 0 {
		body += "\n\nAttachments:\n"
		for _, attachment := range email.Attachments {
			body += fmt.Sprintf("- %s/%s\n", MediaDir, attachment)
		}
	}

	return fmt.Sprintf(
		"From: %s <%s>\nSubject: %s\nDate: %s\n%s\n%s",
		email.FromName,
		email.FromEmail,
		email.Subject,
		email.Date.String(),
		strings.Repeat("-", 50),
		body,
	)
}

func BuildMessageArchiveContent(message ArchivedMessage) string {
	body := message.Body
	if len(message.Attachments) > 0 {
		body += "\n\nAttachments:\n"
		for _, attachment := range message.Attachments {
			body += fmt.Sprintf("- %s\n", attachment)
		}
	}

	mentionsLine := ""
	if len(message.Mentions) > 0 {
		mentionsLine = "\nMentions: " + strings.Join(message.Mentions, "; ")
	}

	return fmt.Sprintf(
		"Source: %s\nSender: %s <%s>\nConversation: %s\nSubject: %s\nMessageID: %s\nDate: %s%s\n%s\n%s",
		message.Source,
		message.SenderName,
		message.SenderID,
		message.ConversationID,
		message.Subject,
		message.MessageID,
		message.Date.Format(time.RFC3339),
		mentionsLine,
		strings.Repeat("-", 50),
		body,
	)
}

func SavePendingEmail(uid uint32, sender string, content string, now time.Time) (string, error) {
	return SavePendingMessage("email", fmt.Sprintf("%d", uid), sender, content, now)
}

func SavePendingMessage(source, externalID, sender, content string, now time.Time) (string, error) {
	return saveArchivedMessage(PendingDir, source, externalID, sender, content, now)
}

func SaveHistoryMessage(source, externalID, sender, content string, now time.Time) (string, error) {
	return saveArchivedMessage(HistoryDir, source, externalID, sender, content, now)
}

func saveArchivedMessage(dir, source, externalID, sender, content string, now time.Time) (string, error) {
	prefix := strings.NewReplacer("@", "_at_", ":", "-", "/", "-", "\\", "-", " ", "_").Replace(sender)
	if prefix == "" {
		prefix = "unknown"
	}
	rawTimestamp := now.UTC().Format(time.RFC3339)
	timestamp := strings.ReplaceAll(strings.ReplaceAll(rawTimestamp, ":", "-"), ".", "-")
	externalID = strings.NewReplacer(":", "-", "/", "-", "\\", "-", " ", "_").Replace(externalID)
	if externalID == "" {
		externalID = fmt.Sprintf("%d", now.UTC().UnixNano())
	}
	archiveFile := filepath.Join(dir, fmt.Sprintf("%s_%s_%s_%s.txt", source, prefix, timestamp, externalID))

	if err := os.WriteFile(archiveFile, []byte(content), 0644); err != nil {
		return "", err
	}
	return archiveFile, nil
}
