package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type config struct {
	FeishuAppID     string
	FeishuAppSecret string
}

func main() {
	chatID := flag.String("chat-id", "", "Feishu chat_id, e.g. oc_xxx")
	pageSize := flag.Int("page-size", 20, "number of messages to fetch")
	minutes := flag.Int("minutes", 120, "look back this many minutes")
	flag.Parse()

	if strings.TrimSpace(*chatID) == "" {
		log.Fatal("missing -chat-id")
	}

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if strings.TrimSpace(cfg.FeishuAppID) == "" || strings.TrimSpace(cfg.FeishuAppSecret) == "" {
		log.Fatal("FEISHU_APP_ID or FEISHU_APP_SECRET is empty")
	}

	client := lark.NewClient(cfg.FeishuAppID, cfg.FeishuAppSecret)
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
		log.Fatalf("list messages: %v", err)
	}
	if !resp.Success() {
		log.Fatalf("list messages failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data == nil || len(resp.Data.Items) == 0 {
		fmt.Println("no messages")
		return
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

func loadConfig() (config, error) {
	values, err := loadEnvValues()
	if err != nil {
		return config{}, err
	}
	return config{
		FeishuAppID:     values["FEISHU_APP_ID"],
		FeishuAppSecret: values["FEISHU_APP_SECRET"],
	}, nil
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
			values[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
		if err := scanner.Err(); err != nil {
			f.Close()
			return nil, err
		}
		f.Close()
	}
	return values, nil
}

func findEnvFiles() ([]string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	var candidates []string
	dir := wd
	for {
		candidates = append(candidates, filepath.Join(dir, ".env"))
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		homeEnv := filepath.Join(home, ".env")
		if !slices.Contains(candidates, homeEnv) {
			candidates = append(candidates, homeEnv)
		}
	}

	var envFiles []string
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			envFiles = append(envFiles, candidate)
		}
	}
	if len(envFiles) == 0 {
		return nil, fmt.Errorf(".env not found from %s upward or in home directory", wd)
	}

	slices.Reverse(envFiles)
	return envFiles, nil
}
