package app

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
	WebAPIToken       string `json:"web_api_token"` // Bearer token for control endpoints

	// Transparency Settings
	EnableDecisionLogging bool `json:"enable_decision_logging"`
	DecisionLogLevel      string `json:"decision_log_level"` // "off", "basic", "detailed"

	// Multi-Agent Settings
	EnableMultiAgent bool `json:"enable_multi_agent"`

	// Performance Monitoring Settings
	EnablePerformanceMonitoring bool `json:"enable_performance_monitoring"`
	PerformanceMetricsRetention int  `json:"performance_metrics_retention"` // hours

	// Security Settings
	Security SecurityConfig `json:"security"`

	// Storage Settings
	EnablePersistence   bool   `json:"enable_persistence"`
	DatabasePath        string `json:"database_path"`
	DataRetentionDays   int    `json:"data_retention_days"`
	EnableDataCleanup   bool   `json:"enable_data_cleanup"`
}

func LoadConfig() (*Config, error) {
	config := &Config{
		Model:                 "sonnet",
		DefaultProjectDir:     ".",
		WebPort:               "8080",
		WebStaticDir:          "./static",
		EnableDecisionLogging:       true,
		DecisionLogLevel:            "detailed",
		EnableMultiAgent:            false, // Disabled by default (experimental)
		EnablePerformanceMonitoring: true,  // Enabled by default
		PerformanceMetricsRetention: 24,    // 24 hours default
		EnablePersistence:           true,  // Enable SQLite persistence by default
		DatabasePath:                "./data/alice.db", // Default database path
		DataRetentionDays:           30,    // Keep 30 days of data
		EnableDataCleanup:           true,  // Enable automatic cleanup
		Security: SecurityConfig{
			EnableRateLimiting:    true,
			RateLimitRPM:          120,  // 120 requests per minute (SPA makes many concurrent calls)
			RateLimitBurst:        30,   // 30 burst capacity (SPA initial load ~15 parallel requests)
			EnablePIIDetection:    true,
			EnableAuditLogging:    true,
			DataRetentionDays:     30,   // 30 days default
			RequireAuthentication: false, // Disabled by default
			SessionTimeoutMinutes: 60,   // 1 hour
			MaxConcurrentSessions: 100,  // 100 concurrent sessions
		},
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
	if v := os.Getenv("WEB_API_TOKEN"); v != "" {
		config.WebAPIToken = v
	}

	// Transparency Environment Variables
	if v := os.Getenv("ENABLE_DECISION_LOGGING"); v == "false" {
		config.EnableDecisionLogging = false
	}
	if v := os.Getenv("DECISION_LOG_LEVEL"); v != "" {
		config.DecisionLogLevel = v
	}

	// Multi-Agent Environment Variables
	if v := os.Getenv("ENABLE_MULTI_AGENT"); v == "true" {
		config.EnableMultiAgent = true
	}

	// Performance Monitoring Environment Variables
	if v := os.Getenv("ENABLE_PERFORMANCE_MONITORING"); v == "false" {
		config.EnablePerformanceMonitoring = false
	}
	if v := os.Getenv("PERFORMANCE_METRICS_RETENTION"); v != "" {
		if retention, err := strconv.Atoi(v); err == nil && retention > 0 {
			config.PerformanceMetricsRetention = retention
		}
	}

	// Security Environment Variables
	if v := os.Getenv("ENABLE_RATE_LIMITING"); v == "false" {
		config.Security.EnableRateLimiting = false
	}
	if v := os.Getenv("RATE_LIMIT_RPM"); v != "" {
		if rpm, err := strconv.Atoi(v); err == nil && rpm > 0 {
			config.Security.RateLimitRPM = rpm
		}
	}
	if v := os.Getenv("ENABLE_PII_DETECTION"); v == "false" {
		config.Security.EnablePIIDetection = false
	}
	if v := os.Getenv("ENABLE_AUDIT_LOGGING"); v == "false" {
		config.Security.EnableAuditLogging = false
	}
	if v := os.Getenv("ENCRYPTION_KEY"); v != "" {
		config.Security.EncryptionKey = v
	}
	if v := os.Getenv("ALLOWED_IPS"); v != "" {
		config.Security.AllowedIPs = strings.Split(v, ",")
	}
	if v := os.Getenv("BLOCKED_IPS"); v != "" {
		config.Security.BlockedIPs = strings.Split(v, ",")
	}

	// 驗證必要欄位
	if config.TelegramToken == "" {
		return nil, fmt.Errorf("missing TELEGRAM_BOT_TOKEN (env or config.json)")
	}

	// 驗證 Web 介面配置
	if err := validateWebConfig(config); err != nil {
		return nil, err
	}

	// 應用透明度設定
	applyTransparencyConfig(config)

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

func applyTransparencyConfig(config *Config) {
	// Configure decision logging based on settings
	if !config.EnableDecisionLogging || config.DecisionLogLevel == "off" {
		// This will be applied after globalDecisionLogger is available
		// We'll do this in main() after package initialization
		log.Printf("⚠️  Decision logging disabled")
	} else {
		log.Printf("✅ Decision logging enabled (level: %s)", config.DecisionLogLevel)
	}
}

func Main() {
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

	// Initialize Git integration
	InitGitIntegration()
	log.Printf("   Git integration: enabled")

	// Initialize Storage system
	if config.EnablePersistence {
		if err := InitStorage(config.DatabasePath); err != nil {
			log.Printf("⚠️ Warning: failed to initialize persistence layer: %v", err)
			log.Printf("   Continuing with in-memory storage only")
		} else {
			log.Printf("   Persistence: enabled (SQLite at %s)", config.DatabasePath)
			log.Printf("   Data retention: %d days", config.DataRetentionDays)
		}
	} else {
		log.Printf("   Persistence: disabled (in-memory storage only)")
	}

	// Apply transparency settings
	if !config.EnableDecisionLogging || config.DecisionLogLevel == "off" {
		globalDecisionLogger.SetEnabled(false)
		log.Printf("   Decision logging: disabled")
	} else {
		globalDecisionLogger.SetEnabled(true)
		log.Printf("   Decision logging: enabled (level: %s)", config.DecisionLogLevel)
	}

	// Apply multi-agent settings
	if config.EnableMultiAgent {
		globalAgentCoordinator.SetEnabled(true)
		log.Printf("   Multi-agent coordination: enabled")
	} else {
		globalAgentCoordinator.SetEnabled(false)
		log.Printf("   Multi-agent coordination: disabled")
	}

	// Initialize performance monitoring
	if config.EnablePerformanceMonitoring {
		InitPerformanceMonitor()
		log.Printf("   Performance monitoring: enabled (retention: %dh)", config.PerformanceMetricsRetention)
	} else {
		log.Printf("   Performance monitoring: disabled")
	}

	// Initialize WebSocket system
	InitWebSocket()
	log.Printf("   WebSocket real-time events: enabled")

	// Initialize Checkpoint system
	if config.EnablePersistence && globalStorage != nil && globalGitManager != nil {
		if err := InitCheckpointManager(globalStorage, globalGitManager); err != nil {
			log.Printf("⚠️ Warning: failed to initialize checkpoint system: %v", err)
			log.Printf("   Continuing without checkpoint functionality")
		} else {
			log.Printf("   Checkpoint system: enabled (auto-snapshots for dangerous operations)")
		}
	} else {
		log.Printf("   Checkpoint system: disabled (requires persistence and git integration)")
	}

	// Initialize security manager
	if err := InitSecurity(config.Security); err != nil {
		log.Printf("❌ Security initialization failed: %v", err)
		log.Fatalf("Unable to continue without security features")
	}
	log.Printf("   Security features: rate limiting=%v, PII detection=%v, audit logging=%v",
		config.Security.EnableRateLimiting,
		config.Security.EnablePIIDetection,
		config.Security.EnableAuditLogging,
	)

	client := NewClient(config.Model)

	tgBot, err := NewTelegramBot(config, client)
	if err != nil {
		log.Fatalf("❌ Telegram error: %v", err)
	}

	// Start web interface if enabled
	var webInterface *WebInterface
	if config.EnableWebInterface {
		webInterface = NewWebInterface(tgBot, config.WebPort, config.WebStaticDir, config.WebAPIToken)
		go func() {
			if err := webInterface.Start(); err != nil {
				log.Printf("❌ Web server error: %v", err)
			}
		}()
	}

	// Start periodic data cleanup if persistence is enabled
	if config.EnablePersistence && config.EnableDataCleanup && globalStorage != nil {
		go func() {
			ticker := time.NewTicker(24 * time.Hour) // Run cleanup daily
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					if err := globalStorage.CleanupOldData(config.DataRetentionDays); err != nil {
						log.Printf("❌ Data cleanup error: %v", err)
					}
				}
			}
		}()
		log.Printf("   Data cleanup: scheduled daily (retention: %d days)", config.DataRetentionDays)
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

	// Close database connection
	if globalStorage != nil {
		if err := globalStorage.Close(); err != nil {
			log.Printf("❌ Database shutdown error: %v", err)
		} else {
			log.Printf("✅ Database connection closed")
		}
	}

	log.Println("✅ Shutdown complete")
}
