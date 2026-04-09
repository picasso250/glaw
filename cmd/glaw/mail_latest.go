package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"

	gatewaypkg "glaw/internal/gateway"
)

func runMailLatest(args []string) error {
	fs := flag.NewFlagSet("mail latest", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	envPath := fs.String("env", "auto", "env file path, or 'auto' to use upward lookup")
	sender := fs.String("sender", "", "one sender email address to inspect")
	maxSleepSeconds := fs.Int("max-sleep-seconds", 60, "maximum seconds to wait for a newer matching mail before falling back to current latest")
	pollIntervalSeconds := fs.Int("poll-interval-seconds", 2, "seconds between inbox polls while waiting for newer matching mail")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments for mail latest: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(*sender) == "" {
		return fmt.Errorf("missing -sender")
	}
	if info, err := os.Stat("gateway"); err != nil || !info.IsDir() {
		return fmt.Errorf("current working directory must be glaw root: missing gateway/")
	}

	config, err := loadEnv(*envPath)
	if err != nil {
		return fmt.Errorf("load .env: %w", err)
	}
	config.FilterSenders = parseFilterSenders(*sender)
	if len(config.FilterSenders) != 1 {
		return fmt.Errorf("mail latest requires exactly one sender")
	}
	if *maxSleepSeconds < 0 {
		return fmt.Errorf("max-sleep-seconds must be >= 0")
	}
	if *pollIntervalSeconds <= 0 {
		return fmt.Errorf("poll-interval-seconds must be > 0")
	}

	if err := gatewaypkg.EnsureRuntimeDirs(); err != nil {
		return fmt.Errorf("ensure runtime dirs: %w", err)
	}

	c, err := connectMail(config)
	if err != nil {
		return err
	}
	defer c.Logout()

	_, err = c.Select("INBOX", false)
	if err != nil {
		return fmt.Errorf("select inbox: %w", err)
	}

	latestMsg, latestSender, err := findLatestMessageForSender(c, config.FilterSenders)
	if err != nil {
		return err
	}
	if latestMsg == nil {
		return fmt.Errorf("no mail found for sender %s", config.FilterSenders[0])
	}
	originalUID := latestMsg.Uid

	if *maxSleepSeconds > 0 {
		deadline := time.Now().Add(time.Duration(*maxSleepSeconds) * time.Second)
		pollInterval := time.Duration(*pollIntervalSeconds) * time.Second
		for time.Now().Before(deadline) {
			time.Sleep(pollInterval)
			latestCandidate, candidateSender, err := findLatestMessageForSender(c, config.FilterSenders)
			if err != nil {
				return err
			}
			if latestCandidate != nil && latestCandidate.Uid > originalUID {
				latestMsg = latestCandidate
				latestSender = candidateSender
				break
			}
		}
	}

	archivedEmail, err := fetchArchivedEmailByUID(c, latestMsg.Uid, latestMsg.Envelope)
	if err != nil {
		return err
	}
	if archivedEmail == nil {
		return fmt.Errorf("latest mail uid %d has empty body", latestMsg.Uid)
	}

	archiveContent := gatewaypkg.BuildEmailArchiveContent(*archivedEmail)
	archiveFile, err := gatewaypkg.SaveHistoryMessage("email_latest", fmt.Sprintf("%d", latestMsg.Uid), latestSender, archiveContent, time.Now())
	if err != nil {
		return err
	}

	fmt.Printf("latest_uid=%d\n", latestMsg.Uid)
	fmt.Printf("sender=%s\n", latestSender)
	fmt.Printf("subject=%s\n", latestMsg.Envelope.Subject)
	fmt.Printf("saved=%s\n", archiveFile)
	return nil
}

func findLatestMessageForSender(c *client.Client, filterSenders []string) (*imap.Message, string, error) {
	uids, err := c.UidSearch(imap.NewSearchCriteria())
	if err != nil {
		return nil, "", fmt.Errorf("search inbox: %w", err)
	}
	if len(uids) == 0 {
		return nil, "", fmt.Errorf("no mail in inbox")
	}

	seqset := new(imap.SeqSet)
	seqset.AddNum(uids...)
	messages := make(chan *imap.Message, len(uids))
	fetchErrCh := make(chan error, 1)
	go func() {
		fetchErrCh <- c.UidFetch(seqset, []imap.FetchItem{imap.FetchEnvelope, imap.FetchUid}, messages)
	}()

	var latestMsg *imap.Message
	var latestSender string
	for msg := range messages {
		if msg == nil || msg.Envelope == nil || len(msg.Envelope.From) == 0 {
			continue
		}
		emailAddr := strings.ToLower(msg.Envelope.From[0].MailboxName + "@" + msg.Envelope.From[0].HostName)
		if !senderMatches(filterSenders, emailAddr) {
			continue
		}
		if latestMsg == nil || msg.Uid > latestMsg.Uid {
			latestMsg = msg
			latestSender = emailAddr
		}
	}
	if err := <-fetchErrCh; err != nil {
		return nil, "", fmt.Errorf("fetch envelope: %w", err)
	}
	return latestMsg, latestSender, nil
}
