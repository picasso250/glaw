package main

import (
	"bufio"
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/emersion/go-imap"
	id "github.com/emersion/go-imap-id"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	gatewaypkg "glaw/internal/gateway"
)

type Config struct {
	MailUser           string
	MailPass           string
	MailImapServer     string
	MailSmtpServer     string
	MailSmtpPort       int
	FilterSenders      []string
	FilterListPath     string
	AgentCmd           string
	EnvPath            string
	ExecSubjectKeyword string
	Feishu             gatewaypkg.FeishuConfig
}

const defaultMailFilterListName = "mail_filter_senders.txt"

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

func parseFilterSenderLines(lines []string) []string {
	var filters []string
	for _, line := range lines {
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.ToLower(strings.TrimSpace(line))
		if line == "" {
			continue
		}
		filters = append(filters, line)
	}
	return filters
}

func parseCSV(val string) []string {
	var items []string
	for _, s := range strings.Split(val, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		items = append(items, s)
	}
	return items
}

func findEnvFiles() ([]string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	var envFiles []string

	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		homeEnv := filepath.Join(home, ".env")
		info, statErr := os.Stat(homeEnv)
		if statErr == nil && !info.IsDir() {
			envFiles = append(envFiles, homeEnv)
		}
	}

	var localCandidates []string
	dir := wd
	for {
		localCandidates = append(localCandidates, filepath.Join(dir, ".env"))
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	for i := len(localCandidates) - 1; i >= 0; i-- {
		candidate := localCandidates[i]
		if len(envFiles) > 0 && strings.EqualFold(envFiles[len(envFiles)-1], candidate) {
			continue
		}
		info, statErr := os.Stat(candidate)
		if statErr == nil && !info.IsDir() {
			envFiles = append(envFiles, candidate)
		}
	}

	if len(envFiles) == 0 {
		return nil, fmt.Errorf(".env not found in %s upward or at home directory", wd)
	}

	return envFiles, nil
}

func resolveEnvPaths(envPath string) ([]string, error) {
	envPath = strings.TrimSpace(envPath)
	if envPath == "" || strings.EqualFold(envPath, "auto") {
		return findEnvFiles()
	}

	info, err := os.Stat(envPath)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("env path is a directory: %s", envPath)
	}
	return []string{envPath}, nil
}

func loadEnvValues(envPath string) (map[string]string, error) {
	envPaths, err := resolveEnvPaths(envPath)
	if err != nil {
		return nil, err
	}

	values := make(map[string]string)
	for _, envPath := range envPaths {
		f, err := os.Open(envPath)
		if err != nil {
			return nil, err
		}

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			key := strings.TrimSpace(parts[0])
			key = strings.TrimPrefix(key, "\uFEFF")
			val := strings.TrimSpace(parts[1])
			values[key] = val
		}
		if err := scanner.Err(); err != nil {
			f.Close()
			return nil, err
		}
		f.Close()
	}

	return values, nil
}

func loadEnv(envPath string) (Config, error) {
	config := Config{
		MailImapServer: "imap.163.com",
		MailSmtpPort:   465,
		EnvPath:        strings.TrimSpace(envPath),
	}
	values, err := loadEnvValues(envPath)
	if err != nil {
		return config, err
	}

	if val, ok := values["MAIL_USER"]; ok {
		config.MailUser = val
	}
	if val, ok := values["MAIL_PASS"]; ok {
		config.MailPass = val
	}
	if val, ok := values["MAIL_IMAP_SERVER"]; ok {
		config.MailImapServer = val
	}
	if val, ok := values["MAIL_SMTP_SERVER"]; ok {
		config.MailSmtpServer = val
	}
	if val, ok := values["MAIL_SMTP_PORT"]; ok {
		port, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil {
			return config, fmt.Errorf("invalid MAIL_SMTP_PORT %q: %w", val, err)
		}
		config.MailSmtpPort = port
	}
	if val, ok := values["AGENT_CMD"]; ok {
		config.AgentCmd = val
	}
	if val, ok := values["FEISHU_APP_ID"]; ok {
		config.Feishu.AppID = val
	}
	if val, ok := values["FEISHU_APP_SECRET"]; ok {
		config.Feishu.AppSecret = val
	}
	if val, ok := values["FEISHU_ALLOWED_OPEN_IDS"]; ok {
		config.Feishu.AllowedOpenIDs = parseCSV(val)
	}
	if val, ok := values["FEISHU_ALLOWED_CHAT_IDS"]; ok {
		config.Feishu.AllowedChatIDs = parseCSV(val)
	}

	return config, nil
}

func resolveFilterListPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return defaultMailFilterListName, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			if strings.HasSuffix(path, `\`) || strings.HasSuffix(path, `/`) {
				return filepath.Join(path, defaultMailFilterListName), nil
			}
			return path, nil
		}
		return "", err
	}
	if info.IsDir() {
		return filepath.Join(path, defaultMailFilterListName), nil
	}
	return path, nil
}

func loadFilterSendersFromFile(path string) ([]string, string, error) {
	resolvedPath, err := resolveFilterListPath(path)
	if err != nil {
		return nil, "", err
	}

	f, err := os.Open(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, resolvedPath, nil
		}
		return nil, "", err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, "", err
	}

	return parseFilterSenderLines(lines), resolvedPath, nil
}

func signalDispatch(dispatchCh chan gatewaypkg.DispatchRequest, req gatewaypkg.DispatchRequest) {
	dispatchCh <- req
}

func connectMail(config Config) (*client.Client, error) {
	fmt.Printf("[*] [check_mail] Connecting to %s...\n", config.MailImapServer)
	c, err := client.DialTLS(config.MailImapServer+":993", nil)
	if err != nil {
		return nil, fmt.Errorf("check_mail connection error: %w", err)
	}

	fmt.Printf("[check_mail] >>> LOGIN %s\n", config.MailUser)
	if err := c.Login(config.MailUser, config.MailPass); err != nil {
		c.Logout()
		return nil, fmt.Errorf("check_mail login error: %w", err)
	}

	idClient := id.NewClient(c)
	if _, err := idClient.ID(id.ID{
		"name":    "iPhone Mail",
		"version": "15.4",
		"os":      "iOS",
		"vendor":  "Apple",
	}); err != nil {
		log.Printf("[check_mail] [!] ID error: %v", err)
	}

	return c, nil
}

func senderMatches(filters []string, emailAddr string) bool {
	for _, filter := range filters {
		if emailAddr == filter {
			return true
		}
	}
	return false
}

func fetchArchivedEmailByUID(c *client.Client, uid uint32, envelope *imap.Envelope) (*gatewaypkg.ArchivedEmail, error) {
	section := &imap.BodySectionName{}
	fullSeqSet := new(imap.SeqSet)
	fullSeqSet.AddNum(uid)

	fullMessages := make(chan *imap.Message, 1)
	bodyFetchErrCh := make(chan error, 1)
	go func() {
		bodyFetchErrCh <- c.UidFetch(fullSeqSet, []imap.FetchItem{section.FetchItem()}, fullMessages)
	}()

	fullMsg := <-fullMessages
	if err := <-bodyFetchErrCh; err != nil {
		return nil, fmt.Errorf("fetch body for uid %d: %w", uid, err)
	}
	if fullMsg == nil {
		return nil, nil
	}

	r := fullMsg.GetBody(section)
	if r == nil {
		return nil, nil
	}

	mr, err := mail.CreateReader(r)
	if err != nil {
		return nil, fmt.Errorf("create reader for uid %d: %w", uid, err)
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

	emailAddr := ""
	fromName := ""
	if envelope != nil && len(envelope.From) > 0 {
		emailAddr = strings.ToLower(envelope.From[0].MailboxName + "@" + envelope.From[0].HostName)
		fromName = envelope.From[0].PersonalName
	}

	return &gatewaypkg.ArchivedEmail{
		FromName:    fromName,
		FromEmail:   emailAddr,
		Subject:     envelope.Subject,
		Date:        envelope.Date,
		Body:        body,
		ImageFiles:  imageFiles,
		Attachments: attachmentFiles,
	}, nil
}

func checkAndProcessEmails(c *client.Client, config *Config, db *sql.DB, dispatchCh chan gatewaypkg.DispatchRequest) error {
	if err := os.MkdirAll(gatewaypkg.MediaDir, 0755); err != nil {
		log.Printf("[check_mail] [!] Create media dir error: %v", err)
		return nil
	}

	if filters, resolvedPath, err := loadFilterSendersFromFile(config.FilterListPath); err != nil {
		log.Printf("[check_mail] [!] Reload mail filter list failed: %v (keeping previous filter)", err)
	} else {
		config.FilterSenders = filters
		config.FilterListPath = resolvedPath
	}

	_, err := c.Select("INBOX", false)
	if err != nil {
		return fmt.Errorf("select inbox: %w", err)
	}

	criteria := imap.NewSearchCriteria()
	uids, err := c.UidSearch(criteria)
	if err != nil {
		return fmt.Errorf("search inbox: %w", err)
	}

	if len(uids) == 0 {
		return nil
	}

	var newUIDs []uint32
	for _, uid := range uids {
		_, err := gatewaypkg.LookupEmailState(db, uid)
		if err == sql.ErrNoRows {
			newUIDs = append(newUIDs, uid)
		}
	}

	if len(newUIDs) == 0 {
		return nil
	}

	fmt.Printf("[%s] [check_mail] Found %d new UIDs to check.\n", time.Now().Format("15:04:05"), len(newUIDs))

	seqset := new(imap.SeqSet)
	seqset.AddNum(newUIDs...)

	messages := make(chan *imap.Message, len(newUIDs))
	fetchErrCh := make(chan error, 1)
	go func() {
		fetchErrCh <- c.UidFetch(seqset, []imap.FetchItem{imap.FetchEnvelope, imap.FetchUid}, messages)
	}()

	for msg := range messages {
		if msg == nil || msg.Envelope == nil {
			continue
		}

		emailAddr := ""
		if len(msg.Envelope.From) > 0 {
			emailAddr = strings.ToLower(msg.Envelope.From[0].MailboxName + "@" + msg.Envelope.From[0].HostName)
		}

		isMatch := senderMatches(config.FilterSenders, emailAddr)

		if isMatch {
			subject := msg.Envelope.Subject
			fmt.Printf("    [*] [check_mail] Match Found (UID: %d): %s\n", msg.Uid, subject)
			archivedEmail, err := fetchArchivedEmailByUID(c, msg.Uid, msg.Envelope)
			if err != nil {
				return err
			}
			if archivedEmail == nil {
				continue
			}

			if subjectMatchesKeyword(subject, config.ExecSubjectKeyword) {
				log.Printf("[check_mail] [*] Subject matched exec keyword; bypass dispatch for uid=%d", msg.Uid)
				gatewaypkg.SaveEmailState(db, msg.Uid, emailAddr, subject, gatewaypkg.StateProcessed)
				configCopy := *config
				archivedEmailCopy := *archivedEmail
				go func(uid uint32, sender, matchedSubject string, email gatewaypkg.ArchivedEmail) {
					if err := processExecutionMail(&configCopy, uid, sender, matchedSubject, &email); err != nil {
						log.Printf("[check_mail] [!] Execution mail handling failed for uid=%d: %v", uid, err)
					}
				}(msg.Uid, emailAddr, subject, archivedEmailCopy)
				continue
			}

			archiveContent := gatewaypkg.BuildEmailArchiveContent(*archivedEmail)
			archiveFile, err := gatewaypkg.SavePendingEmail(msg.Uid, emailAddr, archiveContent, time.Now())
			if err == nil {
				fmt.Printf("    -> [check_mail] Saved to History: %s\n", archiveFile)
				signalDispatch(dispatchCh, gatewaypkg.DispatchRequest{
					Type:    "email",
					Message: archiveFile,
				})
				gatewaypkg.SaveEmailState(db, msg.Uid, emailAddr, subject, gatewaypkg.StateProcessed)
			}
		} else {
			gatewaypkg.SaveEmailState(db, msg.Uid, emailAddr, msg.Envelope.Subject, gatewaypkg.StateIgnored)
		}
	}

	if err := <-fetchErrCh; err != nil {
		return fmt.Errorf("fetch envelope: %w", err)
	}

	return nil
}

func dispatchLoop(dispatcher *gatewaypkg.Dispatcher, dispatchCh <-chan gatewaypkg.DispatchRequest, stopChan <-chan bool) {
	for {
		select {
		case <-stopChan:
			fmt.Println("[dispatch] Stopping...")
			return
		case req := <-dispatchCh:
			batch := []gatewaypkg.DispatchRequest{req}
		drain:
			for {
				select {
				case nextReq := <-dispatchCh:
					batch = append(batch, nextReq)
				default:
					break drain
				}
			}
			dispatcher.DispatchBatch(batch)
		}
	}
}

func mailLoop(config Config, db *sql.DB, dispatchCh chan gatewaypkg.DispatchRequest, stopChan <-chan bool) {
	if err := gatewaypkg.EnsureRuntimeDirs(); err != nil {
		log.Printf("[!] gateway init dirs error: %v", err)
		return
	}

	fmt.Println("[*] Gateway loop starting (check-mail: 10s, dispatch: signal-driven)...")
	checkTicker := time.NewTicker(10 * time.Second)
	defer checkTicker.Stop()

	var c *client.Client
	defer func() {
		if c != nil {
			c.Logout()
		}
	}()

	reconnectDelay := time.Second
	triggerCheck := true

	for {
		select {
		case <-stopChan:
			fmt.Println("[gateway] Stopping...")
			return
		default:
		}

		if c == nil {
			conn, err := connectMail(config)
			if err != nil {
				log.Printf("[check_mail] [!] %v", err)
				log.Printf("[check_mail] [*] Reconnecting in %s...", reconnectDelay)
				select {
				case <-stopChan:
					return
				case <-time.After(reconnectDelay):
				}
				if reconnectDelay < 30*time.Second {
					reconnectDelay *= 2
					if reconnectDelay > 30*time.Second {
						reconnectDelay = 30 * time.Second
					}
				}
				continue
			}

			c = conn
			reconnectDelay = time.Second
			triggerCheck = true
			log.Printf("[check_mail] [*] Mail connection ready.")
		}

		if triggerCheck {
			if err := checkAndProcessEmails(c, &config, db, dispatchCh); err != nil {
				log.Printf("[check_mail] [!] %v", err)
				log.Printf("[check_mail] [*] Mail connection lost. Reconnecting...")
				c.Logout()
				c = nil
				continue
			}
			if err := c.Noop(); err != nil {
				log.Printf("[check_mail] [!] noop failed: %v", err)
				log.Printf("[check_mail] [*] Mail connection lost. Reconnecting...")
				c.Logout()
				c = nil
				continue
			}
			triggerCheck = false
		}

		select {
		case <-stopChan:
			fmt.Println("[gateway] Stopping...")
			return
		case <-checkTicker.C:
			triggerCheck = true
		}
	}
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	agentCmd := fs.String("agent-cmd", "", "override AGENT_CMD from .env for this serve process")
	cronConfig := fs.String("cron-config", gatewaypkg.DefaultCronConfigPath, "path to scheduler config JSON")
	filterList := fs.String("mail-filter", defaultMailFilterListName, "mail sender allowlist file path, or a directory containing that file")
	envPath := fs.String("env", "auto", "env file path, or 'auto' to use upward lookup")
	execSubjectKeyword := fs.String("exec-subject-keyword", "", "if subject contains this keyword, bypass dispatch and execute the single attached .ps1/.py file")
	dryRun := fs.Bool("dry-run", false, "load and print effective configuration, then exit without starting services")
	runPrompt := fs.String("run-prompt", "", "run one prompt with the configured agent command, then exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments for serve: %s", strings.Join(fs.Args(), " "))
	}

	if info, err := os.Stat("INIT.md"); err != nil || info.IsDir() {
		return fmt.Errorf("current working directory must be glaw root: missing INIT.md")
	}

	config, err := loadEnv(*envPath)
	if err != nil {
		return fmt.Errorf("load .env: %w", err)
	}
	if strings.TrimSpace(*agentCmd) != "" {
		config.AgentCmd = *agentCmd
	}
	config.ExecSubjectKeyword = strings.TrimSpace(*execSubjectKeyword)
	filters, resolvedFilterPath, err := loadFilterSendersFromFile(*filterList)
	if err != nil {
		return fmt.Errorf("load mail filter list: %w", err)
	}
	config.FilterSenders = filters
	config.FilterListPath = resolvedFilterPath
	log.Printf("[serve] FilterSenders=%q", config.FilterSenders)
	log.Printf("[serve] FilterListPath=%s", config.FilterListPath)
	log.Printf("[serve] EnvPath=%s", strings.TrimSpace(*envPath))
	log.Printf("[serve] CronConfig=%s", strings.TrimSpace(*cronConfig))
	log.Printf("[serve] AgentCmd=%s", config.AgentCmd)
	log.Printf("[serve] ExecSubjectKeyword=%q", config.ExecSubjectKeyword)
	log.Printf("[serve] MailSMTPServer=%s", strings.TrimSpace(config.MailSmtpServer))
	log.Printf("[serve] MailSMTPPort=%d", config.MailSmtpPort)

	feishuEnabled := strings.TrimSpace(config.Feishu.AppID) != "" && strings.TrimSpace(config.Feishu.AppSecret) != ""
	config.Feishu.Enable = feishuEnabled
	log.Printf("[serve] FeishuEnabled=%t", feishuEnabled)

	if *dryRun {
		log.Printf("[serve] DryRun=true")
		return nil
	}
	if strings.TrimSpace(*runPrompt) != "" {
		log.Printf("[serve] RunPrompt=true")
		dispatcher := &gatewaypkg.Dispatcher{AgentCmd: config.AgentCmd}
		if !dispatcher.DispatchBatch([]gatewaypkg.DispatchRequest{{Type: "ai", Message: strings.TrimSpace(*runPrompt)}}) {
			return fmt.Errorf("run prompt failed")
		}
		return nil
	}

	db, err := gatewaypkg.InitDB()
	if err != nil {
		return fmt.Errorf("init db: %w", err)
	}
	defer db.Close()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	stopGateway := make(chan bool)
	dispatchCh := make(chan gatewaypkg.DispatchRequest, 100)

	fmt.Println(">>> Gateway starting (check-mail + dispatch)...")

	var feishuClient *lark.Client
	if feishuEnabled {
		feishuClient = lark.NewClient(config.Feishu.AppID, config.Feishu.AppSecret)
	}

	dispatcher := &gatewaypkg.Dispatcher{
		AgentCmd:     config.AgentCmd,
		FeishuClient: feishuClient,
	}

	go dispatchLoop(dispatcher, dispatchCh, stopGateway)
	go mailLoop(config, db, dispatchCh, stopGateway)
	go gatewaypkg.NewScheduler(*cronConfig, dispatchCh).Run(stopGateway)
	if feishuEnabled {
		go func() {
			if err := gatewaypkg.StartFeishuLongConn(config.Feishu, db, dispatchCh); err != nil {
				log.Printf("[feishu] [!] Long connection stopped: %v", err)
			}
		}()
	}

	<-sigChan
	fmt.Println("\n[*] Shutting down...")
	close(stopGateway)
	time.Sleep(1 * time.Second)
	return nil
}

func runFeishuListMessages(args []string) error {
	fs := flag.NewFlagSet("feishu list-messages", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	chatID := fs.String("chat-id", "", "Feishu chat_id, e.g. oc_xxx")
	pageSize := fs.Int("page-size", 20, "number of messages to fetch")
	minutes := fs.Int("minutes", 120, "look back this many minutes")
	envPath := fs.String("env", "auto", "env file path, or 'auto' to use upward lookup")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments for feishu list-messages: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(*chatID) == "" {
		return fmt.Errorf("missing -chat-id")
	}

	cfg, err := loadEnv(*envPath)
	if err != nil {
		return fmt.Errorf("load .env: %w", err)
	}
	if strings.TrimSpace(cfg.Feishu.AppID) == "" || strings.TrimSpace(cfg.Feishu.AppSecret) == "" {
		return fmt.Errorf("FEISHU_APP_ID or FEISHU_APP_SECRET is empty")
	}
	if err := gatewaypkg.EnsureRuntimeDirs(); err != nil {
		return fmt.Errorf("ensure runtime dirs: %w", err)
	}

	client := lark.NewClient(cfg.Feishu.AppID, cfg.Feishu.AppSecret)
	endTime := time.Now().Unix()
	startTime := time.Now().Add(-time.Duration(*minutes) * time.Minute).Unix()

	req := larkim.NewListMessageReqBuilder().
		ContainerIdType("chat").
		ContainerId(*chatID).
		StartTime(fmt.Sprintf("%d", startTime)).
		EndTime(fmt.Sprintf("%d", endTime)).
		SortType(larkim.SortTypeListMessageByCreateTimeDesc).
		PageSize(*pageSize).
		Build()

	resp, err := client.Im.V1.Message.List(context.Background(), req)
	if err != nil {
		return fmt.Errorf("list messages: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("list messages failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	if err := appendFeishuListMessagesRawLog(strings.TrimSpace(*chatID), *pageSize, *minutes, resp); err != nil {
		return fmt.Errorf("write feishu list-messages raw log: %w", err)
	}
	if resp.Data == nil || len(resp.Data.Items) == 0 {
		fmt.Println("no messages")
		return nil
	}

	var output strings.Builder
	for i, item := range resp.Data.Items {
		fmt.Fprintf(&output, "[%d] message_id=%s sender_type=%s sender_id_type=%s sender_id=%s msg_type=%s create_time=%s chat_id=%s\n",
			i,
			deref(item.MessageId),
			derefSenderType(item.Sender),
			derefSenderIDType(item.Sender),
			derefSenderID(item.Sender),
			deref(item.MsgType),
			formatMillis(deref(item.CreateTime)),
			deref(item.ChatId),
		)
		if item.Body != nil && item.Body.Content != nil {
			fmt.Fprintf(&output, "content=%s\n", deref(item.Body.Content))
		}
		output.WriteString("\n")
	}
	text := output.String()
	fmt.Print(text)

	savedPath, err := saveFeishuListMessagesSnapshot(text)
	if err != nil {
		return fmt.Errorf("save feishu history snapshot: %w", err)
	}
	fmt.Printf("also saved to %s, you can `rg` it later if you want.\n", filepath.ToSlash(savedPath))

	return nil
}

func saveFeishuListMessagesSnapshot(content string) (string, error) {
	dir := filepath.Join(gatewaypkg.HistoryDir, "feishu")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	sum := fmt.Sprintf("%x", md5.Sum([]byte(content)))
	path := filepath.Join(dir, sum[:6]+".txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}
	return path, nil
}

func newFeishuClientFromEnv(envPath string) (*lark.Client, error) {
	cfg, err := loadEnv(envPath)
	if err != nil {
		return nil, fmt.Errorf("load .env: %w", err)
	}
	if strings.TrimSpace(cfg.Feishu.AppID) == "" || strings.TrimSpace(cfg.Feishu.AppSecret) == "" {
		return nil, fmt.Errorf("FEISHU_APP_ID or FEISHU_APP_SECRET is empty")
	}
	return lark.NewClient(cfg.Feishu.AppID, cfg.Feishu.AppSecret), nil
}

func runFeishuSend(args []string) error {
	fs := flag.NewFlagSet("feishu send", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	messageID := fs.String("message-id", "", "Feishu message_id to reply to")
	text := fs.String("text", "", "short text reply")
	image := fs.String("image", "", "local image path to reply with")
	file := fs.String("file", "", "local file path to reply with")
	envPath := fs.String("env", "auto", "env file path, or 'auto' to use upward lookup")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments for feishu send: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(*messageID) == "" {
		return fmt.Errorf("missing -message-id")
	}

	type providedPayload struct {
		actionType string
		payload    string
		label      string
	}

	var provided []providedPayload
	if strings.TrimSpace(*text) != "" {
		provided = append(provided, providedPayload{actionType: "reply_feishu", payload: *text, label: "text"})
	}
	if strings.TrimSpace(*image) != "" {
		provided = append(provided, providedPayload{actionType: "reply_feishu_image", payload: *image, label: "image"})
	}
	if strings.TrimSpace(*file) != "" {
		provided = append(provided, providedPayload{actionType: "reply_feishu_file", payload: *file, label: "file"})
	}
	if len(provided) == 0 {
		return fmt.Errorf("one of -text, -image, -file is required")
	}
	if len(provided) > 1 {
		return fmt.Errorf("only one of -text, -image, -file may be used at a time")
	}

	client, err := newFeishuClientFromEnv(*envPath)
	if err != nil {
		return err
	}
	if err := gatewaypkg.EnsureRuntimeDirs(); err != nil {
		return fmt.Errorf("init gateway dirs: %w", err)
	}

	payload := provided[0]
	dispatcher := &gatewaypkg.Dispatcher{FeishuClient: client}
	replyMessageID, savedPath, err := dispatcher.SubmitReply(payload.actionType, *messageID, payload.payload)
	if err != nil {
		return fmt.Errorf("send %s reply to Feishu message %s: %w", payload.label, strings.TrimSpace(*messageID), err)
	}

	fmt.Printf("sent %s reply to message %s\n", payload.label, strings.TrimSpace(*messageID))
	fmt.Printf("reply_message_id=%s\n", replyMessageID)
	fmt.Printf("saved=%s\n", savedPath)
	return nil
}

func formatMillis(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var millis int64
	if _, err := fmt.Sscanf(raw, "%d", &millis); err != nil {
		return raw
	}
	return time.UnixMilli(millis).Format(time.RFC3339)
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func derefSenderType(sender *larkim.Sender) string {
	if sender == nil {
		return ""
	}
	return deref(sender.SenderType)
}

func derefSenderIDType(sender *larkim.Sender) string {
	if sender == nil {
		return ""
	}
	return deref(sender.IdType)
}

func derefSenderID(sender *larkim.Sender) string {
	if sender == nil {
		return ""
	}
	return deref(sender.Id)
}

func runFeishu(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing feishu subcommand")
	}

	switch args[0] {
	case "list-messages":
		return runFeishuListMessages(args[1:])
	case "send":
		return runFeishuSend(args[1:])
	default:
		return fmt.Errorf("unknown feishu subcommand %q", args[0])
	}
}

func runMail(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing mail subcommand")
	}

	switch args[0] {
	case "latest":
		return runMailLatest(args[1:])
	default:
		return fmt.Errorf("unknown mail subcommand %q", args[0])
	}
}

func runCronList(args []string) error {
	fs := flag.NewFlagSet("cron list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cronConfig := fs.String("cron-config", gatewaypkg.DefaultCronConfigPath, "path to scheduler config JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments for cron list: %s", strings.Join(fs.Args(), " "))
	}

	tasks, err := gatewaypkg.LoadScheduledTasks(*cronConfig)
	if err != nil {
		return fmt.Errorf("load cron config: %w", err)
	}
	if len(tasks) == 0 {
		fmt.Println("no scheduled tasks")
		return nil
	}

	for i, task := range tasks {
		fmt.Printf("[%d] name=%s enabled=%t type=%s schedule=%s\n", i, task.DisplayName(i), task.IsEnabled(), task.NormalizedType(), task.NormalizedSchedule())
		if len(task.Hours) > 0 {
			fmt.Printf("hours=%v\n", task.Hours)
		}
		if strings.TrimSpace(task.Command) != "" {
			fmt.Printf("command=%s\n", task.Command)
		}
		if len(task.Args) > 0 {
			fmt.Printf("args=%q\n", task.Args)
		}
		if strings.TrimSpace(task.Prompt) != "" {
			fmt.Printf("prompt=%s\n", task.Prompt)
		}
		fmt.Println()
	}
	return nil
}

func runCronCheck(args []string) error {
	fs := flag.NewFlagSet("cron check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cronConfig := fs.String("cron-config", gatewaypkg.DefaultCronConfigPath, "path to scheduler config JSON")
	at := fs.String("at", "", "check due tasks at RFC3339 time, default now")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments for cron check: %s", strings.Join(fs.Args(), " "))
	}

	now := time.Now()
	if strings.TrimSpace(*at) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*at))
		if err != nil {
			return fmt.Errorf("parse -at: %w", err)
		}
		now = parsed
	}

	tasks, err := gatewaypkg.LoadScheduledTasks(*cronConfig)
	if err != nil {
		return fmt.Errorf("load cron config: %w", err)
	}

	fmt.Printf("check_time=%s\n", now.Format(time.RFC3339))
	found := false
	for i, task := range tasks {
		slot, due, err := task.RunSlot(now)
		if err != nil {
			fmt.Printf("[%d] name=%s invalid: %v\n", i, task.DisplayName(i), err)
			continue
		}
		fmt.Printf("[%d] name=%s enabled=%t due=%t", i, task.DisplayName(i), task.IsEnabled(), task.IsEnabled() && due)
		if slot != "" {
			fmt.Printf(" slot=%s", slot)
		}
		fmt.Println()
		if task.IsEnabled() && due {
			found = true
		}
	}
	if !found {
		fmt.Println("no due tasks")
	}
	return nil
}

func runCronTask(task gatewaypkg.ScheduledTask, index int, dispatcher *gatewaypkg.Dispatcher) error {
	switch task.NormalizedType() {
	case "program":
		command := strings.TrimSpace(task.Command)
		if command == "" {
			return fmt.Errorf("task %s requires command", task.DisplayName(index))
		}
		resolvedCommand := gatewaypkg.ResolveScheduledCommand(command)
		cmd := exec.Command(resolvedCommand, task.Args...)
		if workDir := strings.TrimSpace(task.WorkDir); workDir != "" {
			cmd.Dir = workDir
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		fmt.Printf("[cron] [EXEC] %s (%s %v)\n", task.DisplayName(index), resolvedCommand, task.Args)
		return cmd.Run()
	case "ai":
		prompt := strings.TrimSpace(task.Prompt)
		if prompt == "" {
			return fmt.Errorf("task %s requires prompt", task.DisplayName(index))
		}
		fmt.Printf("[cron] [QUEUE] %s -> dispatch\n", task.DisplayName(index))
		if dispatcher == nil {
			return fmt.Errorf("dispatcher is required for ai task %s", task.DisplayName(index))
		}
		if !dispatcher.DispatchBatch([]gatewaypkg.DispatchRequest{{Type: "ai", Message: prompt}}) {
			return fmt.Errorf("dispatch failed for ai task %s", task.DisplayName(index))
		}
		return nil
	default:
		return fmt.Errorf("unsupported task type %q", task.Type)
	}
}

func selectCronTasks(tasks []gatewaypkg.ScheduledTask, name string, allDue bool, now time.Time) ([]int, error) {
	if strings.TrimSpace(name) != "" {
		var selected []int
		for i, task := range tasks {
			if strings.EqualFold(task.DisplayName(i), strings.TrimSpace(name)) {
				selected = append(selected, i)
			}
		}
		if len(selected) == 0 {
			return nil, fmt.Errorf("task %q not found", name)
		}
		return selected, nil
	}

	if allDue {
		var selected []int
		for i, task := range tasks {
			if !task.IsEnabled() {
				continue
			}
			_, due, err := task.RunSlot(now)
			if err != nil {
				continue
			}
			if due {
				selected = append(selected, i)
			}
		}
		return selected, nil
	}

	var selected []int
	for i := range tasks {
		selected = append(selected, i)
	}
	return selected, nil
}

func newDispatcherFromEnv(envPath string) (*gatewaypkg.Dispatcher, error) {
	cfg, err := loadEnv(envPath)
	if err != nil {
		return nil, fmt.Errorf("load .env: %w", err)
	}

	feishuEnabled := strings.TrimSpace(cfg.Feishu.AppID) != "" && strings.TrimSpace(cfg.Feishu.AppSecret) != ""
	var feishuClient *lark.Client
	if feishuEnabled {
		feishuClient = lark.NewClient(cfg.Feishu.AppID, cfg.Feishu.AppSecret)
	}
	return &gatewaypkg.Dispatcher{
		AgentCmd:     cfg.AgentCmd,
		FeishuClient: feishuClient,
	}, nil
}

func runCronRun(args []string) error {
	fs := flag.NewFlagSet("cron run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cronConfig := fs.String("cron-config", gatewaypkg.DefaultCronConfigPath, "path to scheduler config JSON")
	name := fs.String("name", "", "run one task by name")
	allDue := fs.Bool("all-due", false, "run only tasks due right now")
	at := fs.String("at", "", "used with -all-due, check due tasks at RFC3339 time")
	envPath := fs.String("env", "auto", "env file path, or 'auto' to use upward lookup")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments for cron run: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(*name) != "" && *allDue {
		return fmt.Errorf("use only one of -name or -all-due")
	}

	now := time.Now()
	if strings.TrimSpace(*at) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*at))
		if err != nil {
			return fmt.Errorf("parse -at: %w", err)
		}
		now = parsed
	}

	tasks, err := gatewaypkg.LoadScheduledTasks(*cronConfig)
	if err != nil {
		return fmt.Errorf("load cron config: %w", err)
	}

	selected, err := selectCronTasks(tasks, *name, *allDue, now)
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		return fmt.Errorf("no tasks selected")
	}
	slices.Sort(selected)

	needsDispatcher := false
	for _, i := range selected {
		if tasks[i].NormalizedType() == "ai" {
			needsDispatcher = true
			break
		}
	}

	var dispatcher *gatewaypkg.Dispatcher
	if needsDispatcher {
		dispatcher, err = newDispatcherFromEnv(*envPath)
		if err != nil {
			return err
		}
	}

	for _, i := range selected {
		task := tasks[i]
		if err := runCronTask(task, i, dispatcher); err != nil {
			return err
		}
	}
	return nil
}

func runCron(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing cron subcommand")
	}

	switch args[0] {
	case "list":
		return runCronList(args[1:])
	case "check":
		return runCronCheck(args[1:])
	case "run":
		return runCronRun(args[1:])
	default:
		return fmt.Errorf("unknown cron subcommand %q", args[0])
	}
}

func usage() string {
	return strings.TrimSpace(`Usage:
  glaw serve [--agent-cmd <command-prefix>] [--cron-config <path>] [--mail-filter <path-or-dir>] [--env <path|auto>] [--exec-subject-keyword <keyword>] [--dry-run] [--run-prompt <text>]
  glaw mail latest -sender <addr> [--env <path|auto>] [--max-sleep-seconds 60] [--poll-interval-seconds 2]
  glaw cron list [--cron-config <path>]
  glaw cron check [--cron-config <path>] [--at <rfc3339>]
  glaw cron run [--cron-config <path>] [-name <task-name> | --all-due] [--at <rfc3339>] [--env <path|auto>]
  glaw feishu list-messages -chat-id <chat_id> [-page-size 20] [-minutes 120] [--env <path|auto>]
  glaw feishu send -message-id <message_id> (-text <text> | -image <path> | -file <path>) [--env <path|auto>]`)
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usage())
		os.Exit(2)
	}

	var err error
	switch args[0] {
	case "serve":
		err = runServe(args[1:])
	case "mail":
		err = runMail(args[1:])
	case "cron":
		err = runCron(args[1:])
	case "feishu":
		err = runFeishu(args[1:])
	case "-h", "--help", "help":
		fmt.Println(usage())
		return
	default:
		err = fmt.Errorf("unknown command %q", args[0])
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func appendFeishuListMessagesRawLog(chatID string, pageSize, minutes int, resp *larkim.ListMessageResp) error {
	if err := os.MkdirAll(gatewaypkg.LogsDir, 0755); err != nil {
		return err
	}

	body, err := json.Marshal(map[string]any{
		"chat_id":   chatID,
		"page_size": pageSize,
		"minutes":   minutes,
		"response":  resp,
	})
	if err != nil {
		return err
	}

	logPath := filepath.Join(gatewaypkg.LogsDir, "feishu_list_messages_raw.jsonl")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(append(body, '\n')); err != nil {
		return err
	}
	return nil
}
