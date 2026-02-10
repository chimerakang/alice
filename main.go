package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	TelegramToken     string  `json:"telegram_token"`
	Model             string  `json:"model"`
	DefaultProjectDir string  `json:"default_project_dir"`
	AllowedUserIDs    []int64 `json:"allowed_user_ids"`
}

func LoadConfig() (*Config, error) {
	config := &Config{
		Model:             "sonnet",
		DefaultProjectDir: ".",
	}

	// 優先從 config.json 讀取
	if data, err := os.ReadFile("config.json"); err == nil {
		if err := json.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("parse config.json: %w", err)
		}
	}

	// 環境變數覆蓋（方便 Docker 部署）
	if v := os.Getenv("TELEGRAM_BOT_TOKEN"); v != "" {
		config.TelegramToken = v
	}
	if v := os.Getenv("CLAUDE_MODEL"); v != "" {
		config.Model = v
	}
	if v := os.Getenv("PROJECT_DIR"); v != "" {
		config.DefaultProjectDir = v
	}
	if v := os.Getenv("ALLOWED_USER_IDS"); v != "" {
		for _, idStr := range strings.Split(v, ",") {
			id, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)
			if err == nil {
				config.AllowedUserIDs = append(config.AllowedUserIDs, id)
			}
		}
	}

	// 驗證必要欄位
	if config.TelegramToken == "" {
		return nil, fmt.Errorf("missing TELEGRAM_BOT_TOKEN (env or config.json)")
	}

	return config, nil
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	config, err := LoadConfig()
	if err != nil {
		log.Fatalf("❌ Config error: %v", err)
	}

	log.Printf("🚀 Starting Claude TG Agent (CLI mode)")
	log.Printf("   Model: %s", config.Model)
	log.Printf("   Project: %s", config.DefaultProjectDir)
	log.Printf("   Allowed users: %v", config.AllowedUserIDs)

	client := NewClient(config.Model)

	tgBot, err := NewTelegramBot(config, client)
	if err != nil {
		log.Fatalf("❌ Telegram error: %v", err)
	}

	tgBot.Start()
}
