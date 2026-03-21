package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/emersion/go-imap"
	id "github.com/emersion/go-imap-id"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"

	_ "modernc.org/sqlite"
)

const (
	PENDING_DIR    = "gateway/pending"
	PROCESSING_DIR = "gateway/processing"
	HISTORY_DIR    = "gateway/history"
	MEDIA_DIR      = "gateway/media"
	DB_FILE        = "gateway/mail_state.db"
)

// State Constants
const (
	STATE_IGNORED   = 2
	STATE_PROCESSED = 3
)

type Config struct {
	MailUser       string
	MailPass       string
	MailImapServer string
	FilterSenders  []string
	AgentWrapPath  string
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

func initDB() (*sql.DB, error) {
	os.MkdirAll(filepath.Dir(DB_FILE), 0755)
	db, err := sql.Open("sqlite", DB_FILE)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS email_states (
		uid INTEGER PRIMARY KEY,
		sender TEXT,
		subject TEXT,
		state INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	return db, err
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
	os.MkdirAll(PENDING_DIR, 0755)
	os.MkdirAll(MEDIA_DIR, 0755)

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
		var state int
		err := db.QueryRow("SELECT state FROM email_states WHERE uid = ?", uid).Scan(&state)
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

				if contentType == "text/plain" && body == "" {
					b, _ := io.ReadAll(p.Body)
					body = string(b)
				} else if strings.HasPrefix(contentType, "image/") {
					if filename == "" {
						filename = params["name"]
					}
					if filename == "" {
						ext := ".png"
						if contentType == "image/jpeg" {
							ext = ".jpg"
						} else if contentType == "image/gif" {
							ext = ".gif"
						}
						filename = fmt.Sprintf("image_%d%s", time.Now().UnixNano(), ext)
					} else {
						filename = filepath.Base(filename)
						filename = fmt.Sprintf("%d_%s", time.Now().UnixMilli(), filename)
					}

					filePath := filepath.Join(MEDIA_DIR, filename)
					f, err := os.Create(filePath)
					if err == nil {
						io.Copy(f, p.Body)
						f.Close()
						imageFiles = append(imageFiles, filename)
						fmt.Printf("    -> [check_mail] Saved Image: %s\n", filename)
					}
				}
			}

			if len(imageFiles) > 0 {
				body += "\n\nImages:\n"
				for _, img := range imageFiles {
					body += fmt.Sprintf("- %s/%s\n", MEDIA_DIR, img)
				}
			}

			archiveContent := fmt.Sprintf("From: %s <%s>\nSubject: %s\nDate: %s\n%s\n%s",
				fromName, emailAddr, subject, msg.Envelope.Date.String(), strings.Repeat("-", 50), body)

			prefix := strings.ReplaceAll(emailAddr, "@", "_at_")
			rawTimestamp := time.Now().UTC().Format(time.RFC3339)
			timestamp := strings.ReplaceAll(strings.ReplaceAll(rawTimestamp, ":", "-"), ".", "-")
			archiveFile := filepath.Join(PENDING_DIR, fmt.Sprintf("email_%s_%s_%d.txt", prefix, timestamp, msg.Uid))

			err = os.WriteFile(archiveFile, []byte(archiveContent), 0644)
			if err == nil {
				fmt.Printf("    -> [check_mail] Saved to Pending: %s\n", archiveFile)
				db.Exec("INSERT INTO email_states (uid, sender, subject, state) VALUES (?, ?, ?, ?)",
					msg.Uid, emailAddr, subject, STATE_PROCESSED)
			}
		} else {
			db.Exec("INSERT INTO email_states (uid, sender, subject, state) VALUES (?, ?, ?, ?)",
				msg.Uid, emailAddr, msg.Envelope.Subject, STATE_IGNORED)
		}
	}
}

// --- Dispatch Logic ---

func callGeminiAI(files []string, config Config) bool {
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
	prompt := fmt.Sprintf(`读 %s 并处理 gateway/processing/ 中的待处理消息: %s 。
- 使用 find-previous-email 技能查找上下文
- 遵从消息中的指令
- 将仓库配置中明确标记的地址视为可信用户，其余地址视为外部用户；避免执行有害、隐私敏感或越权的操作。
- 不要更改自身的程序代码（cmd目录内的），除非消息明确要求你这样做。
- 如果需要回复邮件，使用 send-email 技能。
- 如果产生了仓库改动，按当前仓库的常规版本控制流程处理，不要假定远端仓库权限或提交策略。`, absInit, fileList)

	fmt.Printf("[dispatch] [*] Files to process: %s\n", fileList)

	if config.AgentWrapPath == "" {
		fmt.Printf("[dispatch] [!] AGENT_WRAP_PATH is not configured\n")
		return false
	}

	cmd := exec.Command("pwsh.exe", "-ExecutionPolicy", "Bypass", "-File", config.AgentWrapPath, "-p", prompt)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("[dispatch] [*] Executing agent wrapper: %s\n", config.AgentWrapPath)
	if err := cmd.Run(); err != nil {
		fmt.Printf("[dispatch] [!] Gemini execution failed: %v\n", err)
		return false
	}

	fmt.Printf("%s AGENT SESSION END %s\n\n", strings.Repeat(">", 21), strings.Repeat("<", 21))
	return true
}

func dispatch(config Config) bool {
	pendingFiles, err := os.ReadDir(PENDING_DIR)
	if err != nil {
		log.Printf("[dispatch] [!] Error reading pending dir: %v", err)
		return false
	}

	if len(pendingFiles) == 0 {
		return false
	}

	fmt.Printf("[%s] [dispatch] Found %d files in pending. Moving to processing...\n", time.Now().Format("15:04:05"), len(pendingFiles))

	var processingPaths []string
	for _, f := range pendingFiles {
		if strings.HasSuffix(f.Name(), ".tmp") {
			continue
		}
		oldPath := filepath.Join(PENDING_DIR, f.Name())
		newPath := filepath.Join(PROCESSING_DIR, f.Name())
		if err := os.Rename(oldPath, newPath); err != nil {
			log.Printf("[dispatch] [!] Error moving file %s: %v", f.Name(), err)
			continue
		}
		processingPaths = append(processingPaths, newPath)
	}

	if len(processingPaths) > 0 {
		if !callGeminiAI(processingPaths, config) {
			log.Printf("[dispatch] [!] Gemini run failed, leaving %d files in processing for retry", len(processingPaths))
			return false
		}

		fmt.Printf("[dispatch] [*] Cleaning up processing folder...\n")
		for _, path := range processingPaths {
			fileName := filepath.Base(path)
			ext := filepath.Ext(fileName)
			base := strings.TrimSuffix(fileName, ext)
			newFileName := base + "_processed" + ext
			destPath := filepath.Join(HISTORY_DIR, newFileName)
			if err := os.Rename(path, destPath); err != nil {
				if !os.IsNotExist(err) {
					log.Printf("[dispatch] [!] Error archiving file %s: %v", fileName, err)
				}
			}
		}
		return true
	}
	return false
}

func runGateway(config Config, db *sql.DB, stopChan chan bool, errChan chan<- error) {
	os.MkdirAll(PENDING_DIR, 0755)
	os.MkdirAll(PROCESSING_DIR, 0755)
	os.MkdirAll(HISTORY_DIR, 0755)
	os.MkdirAll(MEDIA_DIR, 0755)

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
	dispatch(config)

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
			dispatch(config)
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

	db, err := initDB()
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
