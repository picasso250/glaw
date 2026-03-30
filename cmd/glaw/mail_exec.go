package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	multipart "mime/multipart"
	stdmail "net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	gatewaypkg "glaw/internal/gateway"
)

const mailExecTimeout = 5 * time.Minute

func normalizeReplySubject(subject string) string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return "Execution Result"
	}
	if strings.HasPrefix(strings.ToLower(subject), "re:") {
		return subject
	}
	return "Re: " + subject
}

func subjectMatchesKeyword(subject, keyword string) bool {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return false
	}
	return strings.Contains(strings.ToLower(subject), strings.ToLower(keyword))
}

func sanitizeFilenameToken(value string) string {
	value = strings.TrimSpace(value)
	value = strings.NewReplacer(":", "-", "/", "-", "\\", "-", " ", "_").Replace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func deriveSMTPServer(imapServer string) string {
	imapServer = strings.TrimSpace(imapServer)
	if imapServer == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(imapServer), "imap.") {
		return "smtp." + imapServer[5:]
	}
	return imapServer
}

func resolveSMTPConfig(config Config) (string, int, error) {
	server := strings.TrimSpace(config.MailSmtpServer)
	if server == "" {
		server = deriveSMTPServer(config.MailImapServer)
	}
	if server == "" {
		return "", 0, fmt.Errorf("MAIL_SMTP_SERVER is empty")
	}
	port := config.MailSmtpPort
	if port <= 0 {
		port = 465
	}
	return server, port, nil
}

func textprotoMIMEHeader(values map[string]string) textproto.MIMEHeader {
	header := make(textproto.MIMEHeader, len(values))
	for key, value := range values {
		header.Set(key, value)
	}
	return header
}

func writeBase64Lines(w io.Writer, src []byte) error {
	encoded := base64.StdEncoding.EncodeToString(src)
	for len(encoded) > 76 {
		if _, err := io.WriteString(w, encoded[:76]+"\r\n"); err != nil {
			return err
		}
		encoded = encoded[76:]
	}
	_, err := io.WriteString(w, encoded+"\r\n")
	return err
}

func buildMailWithAttachments(fromAddr, toAddr, subject, body string, attachmentPaths []string) ([]byte, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if _, err := fmt.Fprintf(&buf, "From: %s\r\n", (&stdmail.Address{Address: fromAddr}).String()); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(&buf, "To: %s\r\n", (&stdmail.Address{Address: toAddr}).String()); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(&buf, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject)); err != nil {
		return nil, err
	}
	if _, err := io.WriteString(&buf, "MIME-Version: 1.0\r\n"); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", writer.Boundary()); err != nil {
		return nil, err
	}

	bodyPart, err := writer.CreatePart(textprotoMIMEHeader(map[string]string{
		"Content-Type":              `text/plain; charset="utf-8"`,
		"Content-Transfer-Encoding": "8bit",
	}))
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(bodyPart, body); err != nil {
		return nil, err
	}

	for _, attachmentPath := range attachmentPaths {
		data, err := os.ReadFile(attachmentPath)
		if err != nil {
			return nil, err
		}
		fileName := filepath.Base(attachmentPath)
		contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(fileName)))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		part, err := writer.CreatePart(textprotoMIMEHeader(map[string]string{
			"Content-Type":              fmt.Sprintf(`%s; name="%s"`, contentType, fileName),
			"Content-Disposition":       fmt.Sprintf(`attachment; filename="%s"`, fileName),
			"Content-Transfer-Encoding": "base64",
		}))
		if err != nil {
			return nil, err
		}
		if err := writeBase64Lines(part, data); err != nil {
			return nil, err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func sendMailWithAttachments(config Config, toAddr, subject, body string, attachmentPaths []string) error {
	server, port, err := resolveSMTPConfig(config)
	if err != nil {
		return err
	}
	payload, err := buildMailWithAttachments(config.MailUser, toAddr, subject, body, attachmentPaths)
	if err != nil {
		return err
	}

	conn, err := tls.Dial("tcp", fmt.Sprintf("%s:%d", server, port), &tls.Config{ServerName: server})
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, server)
	if err != nil {
		return err
	}
	defer client.Quit()

	if ok, _ := client.Extension("AUTH"); ok {
		if err := client.Auth(smtp.PlainAuth("", config.MailUser, config.MailPass, server)); err != nil {
			return err
		}
	}
	if err := client.Mail(config.MailUser); err != nil {
		return err
	}
	if err := client.Rcpt(toAddr); err != nil {
		return err
	}
	wc, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := wc.Write(payload); err != nil {
		wc.Close()
		return err
	}
	return wc.Close()
}

func buildExecutionResultPaths(baseDir string, uid uint32, sender string, now time.Time) (string, string) {
	base := fmt.Sprintf(
		"mail_exec_%s_%s_%d",
		sanitizeFilenameToken(sender),
		now.UTC().Format("2006-01-02T15-04-05Z"),
		uid,
	)
	return filepath.Join(baseDir, base+".stdout.txt"), filepath.Join(baseDir, base+".stderr.txt")
}

func selectExecutableAttachment(savedNames []string) (string, error) {
	if len(savedNames) != 1 {
		return "", fmt.Errorf("expected exactly 1 attachment, got %d", len(savedNames))
	}
	fullPath := filepath.Join(gatewaypkg.MediaDir, savedNames[0])
	ext := strings.ToLower(filepath.Ext(fullPath))
	if ext != ".ps1" && ext != ".py" {
		return "", fmt.Errorf("unsupported attachment type %q", ext)
	}
	return fullPath, nil
}

func copyFile(srcPath, dstPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	return os.WriteFile(dstPath, data, 0644)
}

func executeMailAttachment(scriptPath string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mailExecTimeout)
	defer cancel()

	var cmd *exec.Cmd
	switch strings.ToLower(filepath.Ext(scriptPath)) {
	case ".ps1":
		cmd = exec.CommandContext(ctx, "pwsh", "-File", scriptPath)
	case ".py":
		cmd = exec.CommandContext(ctx, "python", scriptPath)
	default:
		return "", "", fmt.Errorf("unsupported attachment type %q", filepath.Ext(scriptPath))
	}

	cmd.Dir = filepath.Dir(scriptPath)
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		if stderrBuf.Len() > 0 {
			stderrBuf.WriteString("\n")
		}
		stderrBuf.WriteString("TIMEOUT")
		return stdoutBuf.String(), stderrBuf.String(), fmt.Errorf("attachment execution timed out after %s", mailExecTimeout)
	}
	return stdoutBuf.String(), stderrBuf.String(), err
}

func processExecutionMail(config *Config, uid uint32, sender, subject string, archivedEmail *gatewaypkg.ArchivedEmail) error {
	tempDir, err := os.MkdirTemp("", "glaw-mail-exec-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	stdoutPath, stderrPath := buildExecutionResultPaths(tempDir, uid, sender, time.Now())
	stdout := ""
	stderr := ""

	scriptPath, err := selectExecutableAttachment(archivedEmail.Attachments)
	if err != nil {
		stderr = err.Error() + "\n"
	} else {
		tempScriptPath := filepath.Join(tempDir, filepath.Base(scriptPath))
		if err := copyFile(scriptPath, tempScriptPath); err != nil {
			stderr = err.Error() + "\n"
		} else {
			stdout, stderr, err = executeMailAttachment(tempScriptPath)
		}
		if err != nil {
			if strings.TrimSpace(stderr) == "" {
				stderr = err.Error() + "\n"
			} else {
				stderr = strings.TrimRight(stderr, "\n") + "\n" + err.Error() + "\n"
			}
		}
	}

	if err := os.WriteFile(stdoutPath, []byte(stdout), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(stderrPath, []byte(stderr), 0644); err != nil {
		return err
	}

	return sendMailWithAttachments(
		*config,
		sender,
		normalizeReplySubject(subject),
		"Attached are stdout.txt and stderr.txt for the requested execution.\n",
		[]string{stdoutPath, stderrPath},
	)
}
