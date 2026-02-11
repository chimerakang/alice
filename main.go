package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Config struct {
	TelegramToken     string  `json:"telegram_token"`
	Model             string  `json:"model"`
	DefaultProjectDir string  `json:"default_project_dir"`
	AllowedUserIDs    []int64 `json:"allowed_user_ids"`

	// Web Interface Settings
	EnableWebInterface bool   `json:"enable_web_interface"`
	WebPort           string `json:"web_port"`
	WebStaticDir      string `json:"web_static_dir"`
}

func LoadConfig() (*Config, error) {
	config := &Config{
		Model:             "sonnet",
		DefaultProjectDir: ".",
		WebPort:           "8080",
		WebStaticDir:      "./static",
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

	// Web Interface Environment Variables
	if v := os.Getenv("ENABLE_WEB_INTERFACE"); v == "true" {
		config.EnableWebInterface = true
	}
	if v := os.Getenv("WEB_PORT"); v != "" {
		config.WebPort = v
	}
	if v := os.Getenv("WEB_STATIC_DIR"); v != "" {
		config.WebStaticDir = v
	}

	// 驗證必要欄位
	if config.TelegramToken == "" {
		return nil, fmt.Errorf("missing TELEGRAM_BOT_TOKEN (env or config.json)")
	}

	// 驗證 Web 介面配置
	if err := validateWebConfig(config); err != nil {
		return nil, err
	}

	return config, nil
}

func validateWebConfig(config *Config) error {
	if config.EnableWebInterface {
		if config.WebPort == "" {
			return fmt.Errorf("missing WEB_PORT when web interface enabled")
		}

		if config.WebStaticDir != "" {
			if _, err := os.Stat(config.WebStaticDir); os.IsNotExist(err) {
				log.Printf("⚠️  Static directory does not exist, will be created: %s", config.WebStaticDir)
			}
		}
	}

	return nil
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

	if config.EnableWebInterface {
		log.Printf("   Web interface: enabled on port %s", config.WebPort)
		log.Printf("   Static directory: %s", config.WebStaticDir)
	}

	client := NewClient(config.Model)

	tgBot, err := NewTelegramBot(config, client)
	if err != nil {
		log.Fatalf("❌ Telegram error: %v", err)
	}

	// Start web interface if enabled
	var webInterface *WebInterface
	if config.EnableWebInterface {
		webInterface = NewWebInterface(tgBot, config.WebPort, config.WebStaticDir)
		go func() {
			if err := webInterface.Start(); err != nil {
				log.Printf("❌ Web server error: %v", err)
			}
		}()
	}

	// Start Telegram bot in separate goroutine
	go tgBot.Start()

	// Wait for interrupt signals for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🛑 Shutting down...")

	// Graceful shutdown
	if webInterface != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := webInterface.Shutdown(ctx); err != nil {
			log.Printf("❌ Web interface shutdown error: %v", err)
		}
	}

	log.Println("✅ Shutdown complete")
}
