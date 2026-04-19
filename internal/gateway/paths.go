package gateway

import (
	"os"
	"path/filepath"
	"strings"
)

var (
	RuntimeDir = defaultRuntimeDir()
	HistoryDir = filepath.Join(RuntimeDir, "history")
	MediaDir   = filepath.Join(RuntimeDir, "media")
	OutboxDir  = filepath.Join(RuntimeDir, "outbox")
	DBFile     = filepath.Join(RuntimeDir, "message_state.db")
	LogsDir    = "logs"
)

func defaultRuntimeDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ".gateway"
	}
	return filepath.Join(home, ".gateway")
}
