package gateway

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ArchivedEmail struct {
	FromName   string
	FromEmail  string
	Subject    string
	Date       time.Time
	Body       string
	ImageFiles []string
	Attachments []string
}

func EnsureRuntimeDirs() error {
	for _, dir := range []string{PendingDir, ProcessingDir, HistoryDir, MediaDir} {
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

func SavePendingEmail(uid uint32, sender string, content string, now time.Time) (string, error) {
	prefix := strings.ReplaceAll(sender, "@", "_at_")
	rawTimestamp := now.UTC().Format(time.RFC3339)
	timestamp := strings.ReplaceAll(strings.ReplaceAll(rawTimestamp, ":", "-"), ".", "-")
	archiveFile := filepath.Join(PendingDir, fmt.Sprintf("email_%s_%s_%d.txt", prefix, timestamp, uid))

	if err := os.WriteFile(archiveFile, []byte(content), 0644); err != nil {
		return "", err
	}
	return archiveFile, nil
}
