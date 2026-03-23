package main

import (
	"bufio"
	"context"
	"database/sql"
	"flag"
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
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	gatewaypkg "glaw/internal/gateway"
)

type Config struct {
	MailUser       string
	MailPass       string
	MailImapServer string
	FilterSenders  []string
	AgentCmd       string
	Feishu         gatewaypkg.FeishuConfig
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

func loadEnvValues() (map[string]string, error) {
	envPaths, err := findEnvFiles()
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

func loadEnv() (Config, error) {
	config := Config{
		MailImapServer: "imap.163.com",
	}
	values, err := loadEnvValues()
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
	if val, ok := values["MAIL_FILTER_SENDER"]; ok {
		config.FilterSenders = parseFilterSenders(val)
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

func reloadFilterSendersFromEnv() ([]string, error) {
	values, err := loadEnvValues()
	if err != nil {
		return nil, err
	}

	val, ok := values["MAIL_FILTER_SENDER"]
	if !ok {
		return []string{}, nil
	}
	return parseFilterSenders(val), nil
}

func signalDispatch(dispatchCh chan struct{}) {
	select {
	case dispatchCh <- struct{}{}:
	default:
	}
}

func previewDispatchBatch() {
	dirs := []string{gatewaypkg.ProcessingDir, gatewaypkg.PendingDir}
	found := false

	for _, dir := range dirs {
		files, err := os.ReadDir(dir)
		if err != nil {
			log.Printf("[dispatch] [!] preview read %s failed: %v", dir, err)
			continue
		}

		for _, f := range files {
			if f.IsDir() || strings.HasSuffix(f.Name(), ".tmp") {
				continue
			}

			found = true
			path := filepath.Join(dir, f.Name())
			body, err := os.ReadFile(path)
			if err != nil {
				log.Printf("[dispatch] [!] preview read file %s failed: %v", path, err)
				continue
			}

			fmt.Printf("[dispatch] [skip] queued file: %s\n", path)
			fmt.Printf("[dispatch] [skip] content:\n%s\n", string(body))
		}
	}

	if !found {
		fmt.Println("[dispatch] [skip] no queued files")
	}
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

func checkAndProcessEmails(c *client.Client, config *Config, db *sql.DB, dispatchCh chan struct{}) error {
	if err := os.MkdirAll(gatewaypkg.PendingDir, 0755); err != nil {
		log.Printf("[check_mail] [!] Create pending dir error: %v", err)
		return nil
	}
	if err := os.MkdirAll(gatewaypkg.MediaDir, 0755); err != nil {
		log.Printf("[check_mail] [!] Create media dir error: %v", err)
		return nil
	}

	if filters, err := reloadFilterSendersFromEnv(); err != nil {
		log.Printf("[check_mail] [!] Reload MAIL_FILTER_SENDER from .env failed: %v (keeping previous filter)", err)
	} else {
		config.FilterSenders = filters
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
			bodyFetchErrCh := make(chan error, 1)
			go func() {
				bodyFetchErrCh <- c.UidFetch(fullSeqSet, []imap.FetchItem{section.FetchItem()}, fullMessages)
			}()

			fullMsg := <-fullMessages
			if err := <-bodyFetchErrCh; err != nil {
				return fmt.Errorf("fetch body for uid %d: %w", msg.Uid, err)
			}
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
				FromName:    fromName,
				FromEmail:   emailAddr,
				Subject:     subject,
				Date:        msg.Envelope.Date,
				Body:        body,
				ImageFiles:  imageFiles,
				Attachments: attachmentFiles,
			})

			archiveFile, err := gatewaypkg.SavePendingEmail(msg.Uid, emailAddr, archiveContent, time.Now())
			if err == nil {
				fmt.Printf("    -> [check_mail] Saved to Pending: %s\n", archiveFile)
				gatewaypkg.SaveEmailState(db, msg.Uid, emailAddr, subject, gatewaypkg.StateProcessed)
				signalDispatch(dispatchCh)
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

func dispatchLoop(dispatcher *gatewaypkg.Dispatcher, dispatchCh <-chan struct{}, stopChan <-chan bool, skipDispatch bool) {
	for {
		select {
		case <-stopChan:
			fmt.Println("[dispatch] Stopping...")
			return
		case <-dispatchCh:
			if skipDispatch {
				previewDispatchBatch()
				continue
			}
			dispatcher.Dispatch()
		}
	}
}

func outboxLoop(dispatcher *gatewaypkg.Dispatcher, stopChan <-chan bool, skipDispatch bool) {
	if skipDispatch {
		return
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			fmt.Println("[outbox] Stopping...")
			return
		case <-ticker.C:
			if err := dispatcher.ProcessOutbox(); err != nil {
				log.Printf("[outbox] [!] %v", err)
			}
		}
	}
}

func mailLoop(config Config, db *sql.DB, dispatchCh chan struct{}, stopChan <-chan bool) {
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
	skipDispatch := fs.Bool("skip-dispatch", false, "log queued message files instead of dispatching them")
	agentCmd := fs.String("agent-cmd", "", "override AGENT_CMD from .env for this serve process")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments for serve: %s", strings.Join(fs.Args(), " "))
	}

	if info, err := os.Stat("gateway"); err != nil || !info.IsDir() {
		return fmt.Errorf("current working directory must be glaw root: missing gateway/")
	}

	config, err := loadEnv()
	if err != nil {
		return fmt.Errorf("load .env: %w", err)
	}
	if strings.TrimSpace(*agentCmd) != "" {
		config.AgentCmd = *agentCmd
	}

	db, err := gatewaypkg.InitDB()
	if err != nil {
		return fmt.Errorf("init db: %w", err)
	}
	defer db.Close()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	stopGateway := make(chan bool)
	dispatchCh := make(chan struct{}, 1)

	if *skipDispatch {
		fmt.Println(">>> Gateway starting (check-mail + skip-dispatch preview)...")
	} else {
		fmt.Println(">>> Gateway starting (check-mail + dispatch)...")
	}

	feishuEnabled := strings.TrimSpace(config.Feishu.AppID) != "" && strings.TrimSpace(config.Feishu.AppSecret) != ""
	config.Feishu.Enable = feishuEnabled

	var feishuClient *lark.Client
	if feishuEnabled {
		feishuClient = lark.NewClient(config.Feishu.AppID, config.Feishu.AppSecret)
	}

	dispatcher := &gatewaypkg.Dispatcher{
		AgentCmd:     config.AgentCmd,
		FeishuClient: feishuClient,
	}
	if dispatcher.HasWork() {
		signalDispatch(dispatchCh)
	}

	go dispatchLoop(dispatcher, dispatchCh, stopGateway, *skipDispatch)
	go outboxLoop(dispatcher, stopGateway, *skipDispatch)
	go mailLoop(config, db, dispatchCh, stopGateway)
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments for feishu list-messages: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(*chatID) == "" {
		return fmt.Errorf("missing -chat-id")
	}

	cfg, err := loadEnv()
	if err != nil {
		return fmt.Errorf("load .env: %w", err)
	}
	if strings.TrimSpace(cfg.Feishu.AppID) == "" || strings.TrimSpace(cfg.Feishu.AppSecret) == "" {
		return fmt.Errorf("FEISHU_APP_ID or FEISHU_APP_SECRET is empty")
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
	if resp.Data == nil || len(resp.Data.Items) == 0 {
		fmt.Println("no messages")
		return nil
	}

	for i, item := range resp.Data.Items {
		fmt.Printf("[%d] message_id=%s sender_type=%s sender_id_type=%s sender_id=%s msg_type=%s create_time=%s chat_id=%s\n",
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
			fmt.Printf("content=%s\n", deref(item.Body.Content))
		}
		fmt.Println()
	}

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
	default:
		return fmt.Errorf("unknown feishu subcommand %q", args[0])
	}
}

func usage() string {
	return strings.TrimSpace(`Usage:
  glaw serve [--skip-dispatch] [--agent-cmd <command-prefix>]
  glaw feishu list-messages -chat-id <chat_id> [-page-size 20] [-minutes 120]`)
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
