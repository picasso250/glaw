package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/emersion/go-imap"
	id "github.com/emersion/go-imap-id"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"

	gatewaypkg "g-claw/internal/gateway"
)

type Config struct {
	MailUser       string
	MailPass       string
	MailImapServer string
	FilterSenders  []string
	AgentWrapPath  string
}

func filenameFromContentType(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	case "text/html":
		return ".html"
	case "application/zip":
		return ".zip"
	default:
		return ".bin"
	}
}

func buildAttachmentFilename(contentType string, params map[string]string, filename string) string {
	if filename == "" {
		filename = params["name"]
	}
	if filename != "" {
		filename = filepath.Base(filename)
		return fmt.Sprintf("%d_%s", time.Now().UnixMilli(), filename)
	}
	return fmt.Sprintf("attachment_%d%s", time.Now().UnixNano(), filenameFromContentType(contentType))
}

func savePartToMediaDir(body io.Reader, contentType string, params map[string]string, filename string) (string, error) {
	savedName := buildAttachmentFilename(contentType, params, filename)
	filePath := filepath.Join(gatewaypkg.MediaDir, savedName)

	f, err := os.Create(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := io.Copy(f, body); err != nil {
		return "", err
	}

	return savedName, nil
}

func parseFilterSenders(val string) []string {
	var filters []string
	for _, s := range strings.Split(val, ",") {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		filters = append(filters, s)
	}
	return filters
}

func loadEnv() (Config, error) {
	config := Config{
		MailImapServer: "imap.163.com",
	}
	f, err := os.Open(".env")
	if err != nil {
		return config, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "MAIL_USER":
			config.MailUser = val
		case "MAIL_PASS":
			config.MailPass = val
		case "MAIL_IMAP_SERVER":
			config.MailImapServer = val
		case "MAIL_FILTER_SENDER":
			config.FilterSenders = parseFilterSenders(val)
		case "AGENT_WRAP_PATH":
			config.AgentWrapPath = val
		}
	}
	return config, nil
}

func reloadFilterSendersFromEnv() ([]string, error) {
	f, err := os.Open(".env")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(parts[0])
		if key != "MAIL_FILTER_SENDER" {
			continue
		}

		val := strings.TrimSpace(parts[1])
		return parseFilterSenders(val), nil
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return []string{}, nil
}

// --- Check Mail Logic ---

func checkAndProcessEmails(c *client.Client, config *Config, db *sql.DB) {
	if err := os.MkdirAll(gatewaypkg.PendingDir, 0755); err != nil {
		log.Printf("[check_mail] [!] Create pending dir error: %v", err)
		return
	}
	if err := os.MkdirAll(gatewaypkg.MediaDir, 0755); err != nil {
		log.Printf("[check_mail] [!] Create media dir error: %v", err)
		return
	}

	if filters, err := reloadFilterSendersFromEnv(); err != nil {
		log.Printf("[check_mail] [!] Reload MAIL_FILTER_SENDER from .env failed: %v (keeping previous filter)", err)
	} else {
		config.FilterSenders = filters
	}

	_, err := c.Select("INBOX", false)
	if err != nil {
		log.Printf("[check_mail] [!] SELECT INBOX error: %v", err)
		return
	}

	criteria := imap.NewSearchCriteria()
	uids, err := c.UidSearch(criteria)
	if err != nil {
		log.Printf("[check_mail] [!] Search error: %v", err)
		return
	}

	if len(uids) == 0 {
		return
	}

	var newUIDs []uint32
	for _, uid := range uids {
		_, err := gatewaypkg.LookupEmailState(db, uid)
		if err == sql.ErrNoRows {
			newUIDs = append(newUIDs, uid)
		}
	}

	if len(newUIDs) == 0 {
		return
	}

	fmt.Printf("[%s] [check_mail] Found %d new UIDs to check.\n", time.Now().Format("15:04:05"), len(newUIDs))

	seqset := new(imap.SeqSet)
	seqset.AddNum(newUIDs...)

	messages := make(chan *imap.Message, len(newUIDs))
	go func() {
		if err := c.UidFetch(seqset, []imap.FetchItem{imap.FetchEnvelope, imap.FetchUid}, messages); err != nil {
			log.Printf("[check_mail] [!] Fetch Envelope error: %v", err)
		}
	}()

	for msg := range messages {
		emailAddr := ""
		fromName := ""
		if len(msg.Envelope.From) > 0 {
			emailAddr = strings.ToLower(msg.Envelope.From[0].MailboxName + "@" + msg.Envelope.From[0].HostName)
			fromName = msg.Envelope.From[0].PersonalName
		}

		isMatch := false
		for _, filter := range config.FilterSenders {
			if emailAddr == filter {
				isMatch = true
				break
			}
		}

		if isMatch {
			subject := msg.Envelope.Subject
			fmt.Printf("    [*] [check_mail] Match Found (UID: %d): %s\n", msg.Uid, subject)

			section := &imap.BodySectionName{}
			fullSeqSet := new(imap.SeqSet)
			fullSeqSet.AddNum(msg.Uid)

			fullMessages := make(chan *imap.Message, 1)
			go func() {
				if err := c.UidFetch(fullSeqSet, []imap.FetchItem{section.FetchItem()}, fullMessages); err != nil {
					log.Printf("[check_mail] [!] Body fetch error: %v", err)
				}
			}()

			fullMsg := <-fullMessages
			if fullMsg == nil {
				continue
			}

			r := fullMsg.GetBody(section)
			if r == nil {
				continue
			}

			mr, err := mail.CreateReader(r)
			if err != nil {
				log.Printf("[check_mail] [!] CreateReader error: %v", err)
				continue
			}

			var body string
			var imageFiles []string
			var attachmentFiles []string
			for {
				p, err := mr.NextPart()
				if err == io.EOF {
					break
				} else if err != nil {
					break
				}

				var contentType string
				var params map[string]string
				var filename string

				switch h := p.Header.(type) {
				case *mail.InlineHeader:
					contentType, params, _ = h.ContentType()
					_, dispParams, _ := h.ContentDisposition()
					filename = dispParams["filename"]
				case *mail.AttachmentHeader:
					contentType, params, _ = h.ContentType()
					_, dispParams, _ := h.ContentDisposition()
					filename = dispParams["filename"]
				}

				isImage := strings.HasPrefix(contentType, "image/")
				isAttachment := false
				switch p.Header.(type) {
				case *mail.AttachmentHeader:
					isAttachment = true
				}

				if contentType == "text/plain" && body == "" {
					b, _ := io.ReadAll(p.Body)
					body = string(b)
				} else if isImage || isAttachment {
					savedName, err := savePartToMediaDir(p.Body, contentType, params, filename)
					if err != nil {
						log.Printf("[check_mail] [!] Save attachment error: %v", err)
						continue
					}

					if isImage {
						imageFiles = append(imageFiles, savedName)
						fmt.Printf("    -> [check_mail] Saved Image: %s\n", savedName)
					}
					if isAttachment && !isImage {
						attachmentFiles = append(attachmentFiles, savedName)
						fmt.Printf("    -> [check_mail] Saved Attachment: %s\n", savedName)
					}
				}
			}

			archiveContent := gatewaypkg.BuildEmailArchiveContent(gatewaypkg.ArchivedEmail{
				FromName:   fromName,
				FromEmail:  emailAddr,
				Subject:    subject,
				Date:       msg.Envelope.Date,
				Body:       body,
				ImageFiles: imageFiles,
				Attachments: attachmentFiles,
			})

			archiveFile, err := gatewaypkg.SavePendingEmail(msg.Uid, emailAddr, archiveContent, time.Now())
			if err == nil {
				fmt.Printf("    -> [check_mail] Saved to Pending: %s\n", archiveFile)
				gatewaypkg.SaveEmailState(db, msg.Uid, emailAddr, subject, gatewaypkg.StateProcessed)
			}
		} else {
			gatewaypkg.SaveEmailState(db, msg.Uid, emailAddr, msg.Envelope.Subject, gatewaypkg.StateIgnored)
		}
	}
}

func runGateway(config Config, db *sql.DB, stopChan chan bool, errChan chan<- error) {
	if err := gatewaypkg.EnsureRuntimeDirs(); err != nil {
		select {
		case errChan <- fmt.Errorf("gateway init dirs error: %w", err):
		default:
		}
		return
	}

	dispatcher := gatewaypkg.Dispatcher{AgentWrapPath: config.AgentWrapPath}

	fmt.Printf("[*] [check_mail] Connecting to %s...\n", config.MailImapServer)
	c, err := client.DialTLS(config.MailImapServer+":993", nil)
	if err != nil {
		select {
		case errChan <- fmt.Errorf("check_mail connection error: %w", err):
		default:
		}
		return
	}
	defer c.Logout()

	fmt.Printf("[check_mail] >>> LOGIN %s\n", config.MailUser)
	if err := c.Login(config.MailUser, config.MailPass); err != nil {
		select {
		case errChan <- fmt.Errorf("check_mail login error: %w", err):
		default:
		}
		return
	}

	idClient := id.NewClient(c)
	idClient.ID(id.ID{
		"name":    "iPhone Mail",
		"version": "15.4",
		"os":      "iOS",
		"vendor":  "Apple",
	})

	fmt.Println("[*] Gateway loop starting (check-mail: 10s, dispatch: 1s)...")
	checkTicker := time.NewTicker(10 * time.Second)
	dispatchTicker := time.NewTicker(1 * time.Second)
	defer checkTicker.Stop()
	defer dispatchTicker.Stop()

	// Initial check keeps startup behavior close to the previous version.
	checkAndProcessEmails(c, &config, db)
	dispatcher.Dispatch()

	for {
		select {
		case <-stopChan:
			fmt.Println("[gateway] Stopping...")
			return
		case <-checkTicker.C:
			checkAndProcessEmails(c, &config, db)
			if err := c.Noop(); err != nil {
				select {
				case errChan <- fmt.Errorf("check_mail connection lost: %w", err):
				default:
				}
				return
			}
		case <-dispatchTicker.C:
			dispatcher.Dispatch()
		}
	}
}

func main() {
	if info, err := os.Stat("gateway"); err != nil || !info.IsDir() {
		log.Fatal("Current working directory must be g-claw root: missing gateway/")
	}

	config, err := loadEnv()
	if err != nil {
		log.Fatalf("Error loading .env: %v", err)
	}

	db, err := gatewaypkg.InitDB()
	if err != nil {
		log.Fatalf("Error initializing DB: %v", err)
	}
	defer db.Close()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	stopGateway := make(chan bool)
	errChan := make(chan error, 1)

	fmt.Println(">>> Gateway starting (check-mail + dispatch)...")

	go runGateway(config, db, stopGateway, errChan)

	select {
	case <-sigChan:
		fmt.Println("\n[*] Shutting down...")
		close(stopGateway)
		time.Sleep(1 * time.Second)
	case err := <-errChan:
		log.Printf("[!] Fatal gateway error: %v", err)
		close(stopGateway)
		time.Sleep(1 * time.Second)
		os.Exit(1)
	}
}
