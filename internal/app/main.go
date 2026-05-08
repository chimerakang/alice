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

	"claude-tg-agent/internal/app/security"
)

const Version = "1.0.1"

type ModelPricingConfig struct {
	Haiku struct {
		Input  float64 `json:"input"`  // per 1M tokens
		Output float64 `json:"output"` // per 1M tokens
	} `json:"haiku"`
	Sonnet struct {
		Input  float64 `json:"input"`
		Output float64 `json:"output"`
	} `json:"sonnet"`
	Opus struct {
		Input  float64 `json:"input"`
		Output float64 `json:"output"`
	} `json:"opus"`
}

type Config struct {
	TelegramToken     string  `json:"telegram_token"`
	Model             string  `json:"model"`
	AnthropicKey      string  `json:"anthropic_key"`
	DefaultProjectDir string  `json:"default_project_dir"`
	AllowedUserIDs    []int64 `json:"allowed_user_ids"`

	// Web Interface Settings
	EnableWebInterface bool   `json:"enable_web_interface"`
	WebPort            string `json:"web_port"`
	WebStaticDir       string `json:"web_static_dir"`
	WebAPIToken        string `json:"web_api_token"` // Bearer token for control endpoints

	// Transparency Settings
	EnableDecisionLogging bool   `json:"enable_decision_logging"`
	DecisionLogLevel      string `json:"decision_log_level"` // "off", "basic", "detailed"

	// Multi-Agent Settings
	EnableMultiAgent bool `json:"enable_multi_agent"`

	// Performance Monitoring Settings
	EnablePerformanceMonitoring bool `json:"enable_performance_monitoring"`
	PerformanceMetricsRetention int  `json:"performance_metrics_retention"` // hours

	// Security Settings
	Security security.SecurityConfig `json:"security"`

	// Storage Settings
	EnablePersistence bool   `json:"enable_persistence"`
	DatabasePath      string `json:"database_path"`
	DataRetentionDays int    `json:"data_retention_days"`
	EnableDataCleanup bool   `json:"enable_data_cleanup"`

	// Multimedia Settings
	Multimedia MultimediaConfig `json:"multimedia"`

	// HTML Rendering Settings
	Rendering RenderingConfig `json:"rendering"`

	// Codex CLI observe-only interception.
	CodexInterception CodexInterceptionConfig `json:"codex_interception"`

	// Model Routing Settings
	ModelRouting ModelRoutingConfig `json:"model_routing"`

	// Model Pricing Settings
	ModelPricing ModelPricingConfig `json:"model_pricing"`

	// AI Backend Selection: "claude" (default), "codex", "api"
	AIBackend string `json:"ai_backend"`

	// CLI Settings
	MaxTurns          int `json:"max_turns"`           // max conversation turns per CLI invocation (default 50)
	CLITimeoutMinutes int `json:"cli_timeout_minutes"` // max execution time per CLI invocation in minutes (default 15, 0=unlimited)

	// Backend Execution Settings
	Backends BackendConfig `json:"backends"`

	// Hermes Settings
	Hermes HermesConfig `json:"hermes"`
}

// HermesConfig controls Hermes Brain-Executor mode settings.
type HermesConfig struct {
	Enabled bool              `json:"enabled"`
	Hooks   HermesHooksConfig `json:"hooks"`

	// StrictModeEnabled enables hard review gating by default.
	StrictModeEnabled bool `json:"strict_mode_enabled"`

	// ReviewTimeoutSeconds caps the per-review CLI call. 0 = use engine default
	// (currently 120s). Operators bump this when reviewing on opus or with very
	// large prompts; lower it when running haiku. See issue #147.
	ReviewTimeoutSeconds int `json:"review_timeout_seconds"`

	// WalkingAgentEnabled keeps the same Claude session alive across consecutive
	// Executor sub-tasks of one Hermes task (when they share a model), instead of
	// the legacy "clear session per sub-task" pattern. Saves cache_creation
	// surcharge (1.25x rate) and cold-start latency. Round 2+ sub-tasks send
	// only the new sub-task description; the session transcript carries goal +
	// rules + prior sub-task outcomes. Falls back to a fresh session when the
	// model changes (heavy <-> light executor) or accumulated context exceeds
	// WalkingAgentMaxContextTokens. See issue #149 + docs/arch/hermes-walking-agent.md.
	WalkingAgentEnabled bool `json:"walking_agent_enabled"`

	// WalkingAgentMaxContextTokens is the watermark above which the walking
	// session is force-cleared to avoid hitting the 200K context window. 0 =
	// engine default 120000. See issue #149.
	WalkingAgentMaxContextTokens int `json:"walking_agent_max_context_tokens"`

	// Model overrides (defaults to ModelRoutingConfig values when empty)
	PlannerModel  string `json:"planner_model"`  // e.g. "claude-opus-4-7"
	ExecutorModel string `json:"executor_model"` // e.g. "claude-haiku-4-5-20251001"

	// HeavyExecutorModel overrides ExecutorModel for sub-tasks classified as
	// substantive code work (Edit/Write tool hints or implementation verbs in
	// the description). Defaults to ModelRoutingConfig.SmartModel when empty.
	// Set equal to ExecutorModel to disable the upgrade.
	HeavyExecutorModel string `json:"heavy_executor_model"`

	// Codex tier overrides — used by /ghermes to run Hermes on the GPT/Codex backend
	// instead of Claude. Each falls back to ModelRoutingConfig.Codex*Model when empty.
	CodexPlannerModel       string `json:"codex_planner_model"`
	CodexExecutorModel      string `json:"codex_executor_model"`
	CodexHeavyExecutorModel string `json:"codex_heavy_executor_model"`

	// Retry limits
	MaxRetriesPerSubtask  int `json:"max_retries_per_subtask"`  // default 3
	MaxPlannerJSONRetries int `json:"max_planner_json_retries"` // default 3

	// InterruptPolicy is retained as an unused field so existing config.json
	// files load without "unknown field" errors. Behaviour is fixed at "inject"
	// (incoming messages append to Accumulated as user feedback); the runtime
	// no longer reads this value.
	InterruptPolicy string `json:"interrupt_policy,omitempty"`

	// ProgressVerbosity is retained as an unused field so existing config.json
	// files load without "unknown field" errors. The reporter now emits a
	// fixed, minimal sequence (plan summary + failure diagnostics + OnDone);
	// operators rely on the dashboard for in-progress detail.
	ProgressVerbosity string `json:"progress_verbosity,omitempty"`

	// AutoRouteComplex enables Complexity Gate auto-routing: natural-language
	// messages classified as complex start Hermes automatically without
	// requiring the /hermes command. Moderate and trivial messages keep
	// going through the regular model routing path.
	AutoRouteComplex bool `json:"auto_route_complex"` // default false

	// Resource limits
	Budget HermesBudgetConfig `json:"budget"`

	// Preflight optionally runs a cheap completion check before starting an
	// issue-backed Hermes task. It can stop duplicate runs before planner/executor
	// spend Opus/Sonnet-scale tokens.
	Preflight HermesPreflightConfig `json:"preflight"`

	// TaskRetry controls task-level re-plan retry on low review score.
	// Disabled by default; opt in via "task_retry": {"enabled": true}.
	// See engine.TaskRetryConfig for field semantics.
	TaskRetry HermesTaskRetryConfig `json:"task_retry"`

	// PromptsDir is the directory containing planner_rules.md and executor_rules.md.
	// Defaults to "internal/app/hermes/prompts" relative to the working directory.
	PromptsDir string `json:"prompts_dir"`

	// Summary controls task completion summary reporting.
	Summary HermesSummaryConfig `json:"summary"`

	// GithubIntegration controls Hermes ↔ GitHub Issue integration.
	GithubIntegration GithubIntegrationConfig `json:"github_integration"`
}

// GithubIntegrationConfig controls how Hermes interacts with GitHub Issues.
type GithubIntegrationConfig struct {
	Enabled bool `json:"enabled"`

	// Comment events: "start", "complete", "fail", "budget_exceeded"
	CommentOnEvents []string `json:"comment_on_events"`

	// SyncChecklist updates Issue `- [ ]` items as SubTasks complete.
	SyncChecklist bool `json:"sync_checklist"`

	// AutoCloseLabel: if the Issue has this label, close it when all SubTasks succeed.
	AutoCloseLabel string `json:"auto_close_on_label"`

	// FailureLabel is added to the Issue when Hermes execution fails.
	FailureLabel string `json:"mark_failure_label"`

	// TriggerTaskSync runs the local /task-sync maintenance command on Hermes completion (best-effort).
	TriggerTaskSync bool `json:"trigger_task_sync_on_complete"`

	// ComplexityBudgetMap maps complexity labels (e.g. "complexity:small") to budget overrides.
	ComplexityBudgetMap map[string]HermesBudgetConfig `json:"complexity_budget_map"`
}

// HermesBudgetConfig sets task-level resource limits.
type HermesBudgetConfig struct {
	MaxTotalTokens      int `json:"max_total_tokens"`      // default 500000; 0 = unlimited
	MaxWallclockSeconds int `json:"max_wallclock_seconds"` // default 600; 0 = unlimited
}

type HermesPreflightConfig struct {
	Enabled             bool   `json:"enabled"`
	Model               string `json:"model"`                // default ModelRouting.FastModel / Haiku
	CompletionThreshold int    `json:"completion_threshold"` // default 90
	TimeoutSeconds      int    `json:"timeout_seconds"`      // default 20
	// Parallel runs the preflight Haiku check concurrently with the Planner
	// (Phase 2.6.A, OpenAI Agents SDK `run_in_parallel=True` pattern).
	// When tripwired (verdict says skip), the running task is interrupted.
	// When false (default), preflight blocks before the Planner kicks off
	// and its augmented goal is forwarded to the Planner.
	Parallel bool `json:"parallel"`
}

// HermesTaskRetryConfig is the JSON-config mirror of engine.TaskRetryConfig.
// Keep fields aligned with engine.TaskRetryConfig — telegram.go converts
// between the two via direct type conversion.
type HermesTaskRetryConfig struct {
	Enabled        bool `json:"enabled"`          // default false
	ScoreThreshold int  `json:"score_threshold"`  // default 60 (applied in engine)
	MaxTaskRetries int  `json:"max_task_retries"` // default 1 (applied in engine)
}

// HermesHooksConfig controls which tool execution hooks are active.
type HermesHooksConfig struct {
	PathGuard      bool     `json:"path_guard"`       // block writes to protected paths; auto-enabled in Hermes mode
	ExtraDenyPaths []string `json:"extra_deny_paths"` // additional path patterns to block
	PostValidators []string `json:"post_validators"`  // "go_build", "tsc_check", "json_lint"
	WorkDir        string   `json:"work_dir"`         // root for build validators; defaults to project dir
}

// CostRateConfig defines pricing for a model.
type CostRateConfig struct {
	InputPerMToken  float64 `json:"input_per_mtok"`  // cost per 1M input tokens
	OutputPerMToken float64 `json:"output_per_mtok"` // cost per 1M output tokens
}

// HermesSummaryConfig controls task completion summary reporting.
type HermesSummaryConfig struct {
	Enabled        bool                      `json:"enabled"`   // whether to generate summaries
	Verbosity      string                    `json:"verbosity"` // "minimal" or "detailed"
	IncludeCostEst bool                      `json:"include_cost_estimate"`
	Targets        []string                  `json:"targets"`    // where to push summaries ("telegram", "github")
	CostRates      map[string]CostRateConfig `json:"cost_rates"` // per-model pricing
}

// HermesDefaults returns a HermesConfig with sensible defaults filled in.
func HermesDefaults(cfg HermesConfig) HermesConfig {
	if cfg.MaxRetriesPerSubtask == 0 {
		cfg.MaxRetriesPerSubtask = 3
	}
	if cfg.MaxPlannerJSONRetries == 0 {
		cfg.MaxPlannerJSONRetries = 3
	}
	if cfg.Preflight.CompletionThreshold == 0 {
		cfg.Preflight.CompletionThreshold = 90
	}
	if cfg.Preflight.TimeoutSeconds == 0 {
		cfg.Preflight.TimeoutSeconds = 20
	}
	// Budget defaults intentionally not filled in: 0 means unlimited and
	// must flow through unchanged. Operators set explicit caps in config.json.
	if cfg.Summary.Verbosity == "" {
		cfg.Summary.Verbosity = "minimal"
	}
	if len(cfg.Summary.Targets) == 0 {
		cfg.Summary.Targets = []string{"telegram"}
	}
	if cfg.Summary.CostRates == nil {
		cfg.Summary.CostRates = make(map[string]CostRateConfig)
		cfg.Summary.CostRates["claude-opus-4-7"] = CostRateConfig{
			InputPerMToken:  15.0,
			OutputPerMToken: 75.0,
		}
		cfg.Summary.CostRates["claude-sonnet-4-6"] = CostRateConfig{
			InputPerMToken:  3.0,
			OutputPerMToken: 15.0,
		}
		cfg.Summary.CostRates["claude-haiku-4-5-20251001"] = CostRateConfig{
			InputPerMToken:  1.0,
			OutputPerMToken: 5.0,
		}
	}
	return cfg
}

// BackendConfig configures execution backends
type BackendConfig struct {
	Default string              `json:"default"` // default backend name
	Docker  DockerBackendConfig `json:"docker"`
	SSH     SSHBackendConfig    `json:"ssh"`
}

// DockerBackendConfig configures the Docker execution backend
type DockerBackendConfig struct {
	Enabled     bool   `json:"enabled"`
	Image       string `json:"image"`
	AutoCleanup bool   `json:"auto_cleanup"`
	Network     string `json:"network"`
	MemoryLimit string `json:"memory_limit"`
	CPULimit    string `json:"cpu_limit"`
}

// SSHBackendConfig configures SSH execution backends
type SSHBackendConfig struct {
	Enabled bool                     `json:"enabled"`
	Hosts   map[string]SSHHostConfig `json:"hosts"`
}

// SSHHostConfig configures a single SSH host
type SSHHostConfig struct {
	Host    string `json:"host"`
	User    string `json:"user"`
	KeyPath string `json:"key_path"`
	WorkDir string `json:"work_dir"`
}

func LoadConfig() (*Config, error) {
	config := &Config{
		Model:                       "sonnet",
		DefaultProjectDir:           ".",
		WebPort:                     "8080",
		WebStaticDir:                "./static",
		EnableDecisionLogging:       true,
		DecisionLogLevel:            "detailed",
		EnableMultiAgent:            false,             // Disabled by default (experimental)
		EnablePerformanceMonitoring: true,              // Enabled by default
		PerformanceMetricsRetention: 24,                // 24 hours default
		EnablePersistence:           true,              // Enable SQLite persistence by default
		DatabasePath:                "./data/alice.db", // Default database path
		DataRetentionDays:           30,                // Keep 30 days of data
		EnableDataCleanup:           true,              // Enable automatic cleanup
		Security: security.SecurityConfig{
			EnableRateLimiting:    true,
			RateLimitRPM:          120, // 120 requests per minute (SPA makes many concurrent calls)
			RateLimitBurst:        30,  // 30 burst capacity (SPA initial load ~15 parallel requests)
			EnablePIIDetection:    true,
			EnableAuditLogging:    true,
			DataRetentionDays:     30,    // 30 days default
			RequireAuthentication: false, // Disabled by default
			SessionTimeoutMinutes: 60,    // 1 hour
			MaxConcurrentSessions: 100,   // 100 concurrent sessions
		},
		Multimedia: MultimediaConfig{
			EnablePhotoSupport:  true,
			EnableVoiceSupport:  true,
			MaxFileSizeMB:       20,               // 20MB limit
			TempDownloadDir:     "./temp/media",   // Temporary download directory
			VoiceToTextProvider: "openai_whisper", // Default to OpenAI Whisper
		},
		Rendering: RenderingConfig{
			EnableHTMLScreenshots: true,
			CacheDir:              "./temp/renders",
			ChromeExecutable:      "", // Use system Chrome/Chromium by default
		},
		MaxTurns:          50, // Default 50 turns per CLI invocation
		CLITimeoutMinutes: 15, // Default 15 minutes per CLI invocation
		Backends: BackendConfig{
			Default: "local",
			Docker: DockerBackendConfig{
				Enabled:     false,
				Image:       "golang:1.22-alpine",
				AutoCleanup: true,
				Network:     "none",
				MemoryLimit: "512m",
				CPULimit:    "2",
			},
			SSH: SSHBackendConfig{
				Enabled: false,
				Hosts:   make(map[string]SSHHostConfig),
			},
		},
		ModelRouting: ModelRoutingConfig{
			EnableDynamicRouting:  false, // Disabled by default
			FastModel:             "claude-haiku-4-5-20251001",
			SmartModel:            "claude-sonnet-4-6", // /smart: configured balanced model
			DeepModel:             "claude-opus-4-6",
			PlanModel:             "claude-opus-4-6",            // Plan/Execute: planning phase model
			ExecuteModel:          "claude-sonnet-4-5-20250929", // Plan/Execute: execution phase model
			CodexFastModel:        "gpt-5.4-mini",               // /gfast: fast GPT tier
			CodexSmartModel:       "gpt-5.4",                    // /gsmart: balanced GPT tier
			CodexDeepModel:        "gpt-5.5",                    // /gdeep: powerful GPT tier (gpt-5.5-pro is API-key-only)
			UseGPT4oMini:          false,
			StickySession:         true,
			SessionIdleTimeoutMin: 1440, // 24h, see #170
			StickyMode:            true,
			SessionIdleTimeout:    1440,
		},
	}

	// Set default model pricing (per 1M tokens, USD)
	config.ModelPricing.Haiku.Input = 1.00
	config.ModelPricing.Haiku.Output = 5.00
	config.ModelPricing.Sonnet.Input = 3.00
	config.ModelPricing.Sonnet.Output = 15.00
	config.ModelPricing.Opus.Input = 15.00
	config.ModelPricing.Opus.Output = 75.00

	// 優先從 config.json 讀取
	if data, err := os.ReadFile("config.json"); err == nil {
		if err := json.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("parse config.json: %w", err)
		}
	}
	config.ModelRouting.Normalize()

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

	// Multimedia Environment Variables
	if v := os.Getenv("ENABLE_PHOTO_SUPPORT"); v == "false" {
		config.Multimedia.EnablePhotoSupport = false
	}
	if v := os.Getenv("ENABLE_VOICE_SUPPORT"); v == "false" {
		config.Multimedia.EnableVoiceSupport = false
	}
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		config.Multimedia.OpenAIAPIKey = v
	}
	if v := os.Getenv("MAX_FILE_SIZE_MB"); v != "" {
		if size, err := strconv.Atoi(v); err == nil && size > 0 {
			config.Multimedia.MaxFileSizeMB = size
		}
	}
	if v := os.Getenv("TEMP_DOWNLOAD_DIR"); v != "" {
		config.Multimedia.TempDownloadDir = v
	}
	if v := os.Getenv("VOICE_TO_TEXT_PROVIDER"); v != "" {
		config.Multimedia.VoiceToTextProvider = v
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

// isClaudeCodeEnvironment 檢測是否在 Claude Code 環境中運行
func isClaudeCodeEnvironment() bool {
	// Claude Code 環境有這些環境變數標誌
	if os.Getenv("CLAUDE_CODE_SESSION") != "" {
		return true
	}
	if os.Getenv("CLAUDE_CODE") != "" {
		return true
	}
	if os.Getenv("ANTHROPIC_HOME") != "" {
		return true
	}
	// 檢查是否在特殊進程下
	if os.Getenv("__CF_USER_TEXT_ENCODING") != "" && os.Getenv("TERM_SESSION_ID") != "" {
		// 可能在 Claude Code 環境中
		return true
	}
	return false
}

func Main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	config, err := LoadConfig()
	if err != nil {
		log.Fatalf("❌ Config error: %v", err)
	}
	runtimeLock, err := AcquireRuntimeLock(runtimeLockPath(config.DatabasePath))
	if err != nil {
		log.Fatalf("❌ Runtime lock error: %v", err)
	}
	defer runtimeLock.Close()

	log.Printf("🚀 Starting Claude TG Agent (CLI mode)")
	log.Printf("   Model: %s", config.Model)
	log.Printf("   Project: %s", config.DefaultProjectDir)
	log.Printf("   Allowed users: %v", config.AllowedUserIDs)

	if config.EnableWebInterface {
		log.Printf("   Web interface: enabled on port %s", config.WebPort)
		log.Printf("   Static directory: %s", config.WebStaticDir)
	}

	// Initialize model pricing from config
	InitModelPricing(&config.ModelPricing)
	log.Printf("   Model pricing: configured (Haiku: $%.2f/$%.2f, Sonnet: $%.2f/$%.2f, Opus: $%.2f/$%.2f per 1M tokens)",
		config.ModelPricing.Haiku.Input, config.ModelPricing.Haiku.Output,
		config.ModelPricing.Sonnet.Input, config.ModelPricing.Sonnet.Output,
		config.ModelPricing.Opus.Input, config.ModelPricing.Opus.Output)

	// Walking-agent (issue #149) forces 5m prompt-cache TTL on every claude
	// CLI invocation when enabled. Otherwise keep Anthropic's default
	// (currently 1h on most builds — 2x cache_write rate, fine for long
	// interactive sessions but expensive for short sub-task workflows).
	if config.Hermes.WalkingAgentEnabled {
		SetForcePromptCaching5m(true)
		log.Printf("   Hermes walking-agent: enabled (FORCE_PROMPT_CACHING_5M=1, max_context=%d)",
			func() int {
				if config.Hermes.WalkingAgentMaxContextTokens > 0 {
					return config.Hermes.WalkingAgentMaxContextTokens
				}
				return 120000
			}())
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

	// Load historical data from DB into in-memory caches
	if globalStorage != nil {
		globalToolLogger.LoadFromDB(globalStorage)
		globalDecisionLogger.LoadFromDB(globalStorage)
		if performanceMonitor != nil {
			performanceMonitor.LoadFromDB(globalStorage)
		}
		log.Printf("   Historical data: loaded from database")

		// Initialize Auto-Skill system
		InitSkillManager()
		log.Printf("   Auto-Skill system: enabled")

		// Initialize SubAgent Orchestrator for parallel execution
		InitOrchestrator(3) // max 3 concurrent CLI sessions
		log.Printf("   SubAgent Orchestrator: enabled (max_concurrent=3)")
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
	if err := security.Init(config.Security); err != nil {
		log.Printf("❌ Security initialization failed: %v", err)
		log.Fatalf("Unable to continue without security features")
	}
	// Wire persistence + broadcast callbacks to break the import cycle with app
	security.OnPersistEvent = func(e security.SecurityEvent) {
		if globalStorage == nil {
			return
		}
		if err := globalStorage.InsertSecurityEvent(e); err != nil {
			log.Printf("Warning: failed to persist security event to database: %v", err)
		}
	}
	security.OnBroadcastEvent = BroadcastSecurityEvent
	log.Printf("   Security features: rate limiting=%v, PII detection=%v, audit logging=%v",
		config.Security.EnableRateLimiting,
		config.Security.EnablePIIDetection,
		config.Security.EnableAuditLogging,
	)

	// Log model routing configuration
	if config.ModelRouting.EnableDynamicRouting {
		triageMethod := "user command only"
		if config.ModelRouting.UseGPT4oMini {
			triageMethod = "AI triage (GPT-4o-mini)"
		}
		log.Printf("   Model routing: enabled (triage: %s)", triageMethod)
		log.Printf("      Fast model: %s", config.ModelRouting.FastModel)
		log.Printf("      Deep model: %s", config.ModelRouting.DeepModel)
		log.Printf("      Default: %s", config.Model)
	} else {
		log.Printf("   Model routing: disabled (using default model: %s)", config.Model)
	}

	// Initialize multimedia support
	if config.Multimedia.EnablePhotoSupport || config.Multimedia.EnableVoiceSupport {
		// Ensure temp directory exists
		if err := os.MkdirAll(config.Multimedia.TempDownloadDir, 0755); err != nil {
			log.Printf("⚠️ Warning: failed to create temp download directory: %v", err)
			log.Printf("   Multimedia features may not work properly")
		} else {
			log.Printf("   Multimedia temp directory: %s", config.Multimedia.TempDownloadDir)
		}
		log.Printf("   Multimedia support: photo=%v, voice=%v (max size: %dMB)",
			config.Multimedia.EnablePhotoSupport,
			config.Multimedia.EnableVoiceSupport,
			config.Multimedia.MaxFileSizeMB,
		)
	}

	// 選擇 AI backend：ai_backend config 優先，其次環境自動偵測
	var client Client

	// Resolve OpenAI key: prefer env, otherwise reuse the Whisper key from multimedia config.
	openaiKey := os.Getenv("OPENAI_API_KEY")
	openaiKeySource := "env"
	if openaiKey == "" && config.Multimedia.OpenAIAPIKey != "" {
		openaiKey = config.Multimedia.OpenAIAPIKey
		openaiKeySource = "multimedia.openai_api_key"
	}

	switch config.AIBackend {
	case "multi":
		// MultiBackendClient: routes claude/* models to CLIClient, gpt-*/codex/* to CodexClient.
		// Enables /gfast /gsmart /gdeep cross-backend tier switching in a single session.
		log.Printf("[client-routing] Config ai_backend=multi - using MultiBackendClient dispatcher")
		cliClient := NewClient(config.Model)
		if config.MaxTurns > 0 {
			cliClient.MaxTurns = config.MaxTurns
		}
		var codexClient *CodexClient
		if openaiKey != "" {
			codexClient = NewCodexClient(config.ModelRouting.CodexSmartModel, openaiKey)
			log.Printf("[client-routing] MultiBackendClient: codex backend enabled (default model: %s, key from %s)", config.ModelRouting.CodexSmartModel, openaiKeySource)
		} else {
			log.Printf("[client-routing] MultiBackendClient: no OpenAI key (env OPENAI_API_KEY or multimedia.openai_api_key), codex tier disabled — /gfast /gsmart /gdeep will be rejected at runtime")
		}
		var apiClient *APIClient
		if config.AnthropicKey != "" {
			apiClient = NewAPIClient(config.AnthropicKey, config.Model)
		}
		client = NewMultiBackendClient(cliClient, codexClient, apiClient, cliClient)
		log.Printf("[client-routing] Using MultiBackendClient (CLIClient default, max-turns=%d)", config.MaxTurns)
	case "codex":
		log.Printf("[client-routing] Config ai_backend=codex - using Codex CLI")
		if openaiKey == "" {
			log.Fatalf("❌ OpenAI API key not set! Codex requires either env OPENAI_API_KEY or config.multimedia.openai_api_key.")
		}
		client = NewCodexClient(config.Model, openaiKey)
		log.Printf("[client-routing] Using CodexClient (Codex CLI, key from %s)", openaiKeySource)
	case "api":
		log.Printf("[client-routing] Config ai_backend=api - using Anthropic API direct")
		if config.AnthropicKey == "" {
			log.Fatalf("❌ Anthropic API Key not configured! Please set 'anthropic_key' in config.json")
		}
		client = NewAPIClient(config.AnthropicKey, config.Model)
		log.Printf("[client-routing] Using APIClient (Anthropic API direct)")
	case "claude":
		log.Printf("[client-routing] Config ai_backend=claude - using Claude Code CLI (explicit)")
		client = NewClient(config.Model)
		if cli, ok := client.(*CLIClient); ok && config.MaxTurns > 0 {
			cli.MaxTurns = config.MaxTurns
		}
		log.Printf("[client-routing] Using CLIClient (Claude Code CLI, max-turns=%d)", config.MaxTurns)
	default: // empty → auto-detect
		if isClaudeCodeEnvironment() {
			log.Printf("[client-routing] Detected Claude Code environment - using Anthropic API")
			if config.AnthropicKey == "" {
				log.Fatalf("❌ Anthropic API Key not configured! Please set 'anthropic_key' in config.json")
			}
			client = NewAPIClient(config.AnthropicKey, config.Model)
			log.Printf("[client-routing] Using APIClient (Anthropic API direct)")
		} else {
			log.Printf("[client-routing] Normal environment - using Claude Code CLI")
			client = NewClient(config.Model)
			if cli, ok := client.(*CLIClient); ok && config.MaxTurns > 0 {
				cli.MaxTurns = config.MaxTurns
			}
			log.Printf("[client-routing] Using CLIClient (Claude Code CLI, max-turns=%d)", config.MaxTurns)
		}
	}

	tgBot, err := NewTelegramBot(config, client)
	if err != nil {
		log.Fatalf("❌ Telegram error: %v", err)
	}

	// Initialize Backend Manager
	InitBackendManager(config.DefaultProjectDir)
	if config.Backends.Docker.Enabled {
		docker := NewDockerBackend(
			config.Backends.Docker.Image,
			config.DefaultProjectDir,
			config.Backends.Docker.Network,
			config.Backends.Docker.MemoryLimit,
			config.Backends.Docker.CPULimit,
		)
		globalBackendManager.Register("docker", docker)
	}
	if config.Backends.SSH.Enabled {
		for name, hostCfg := range config.Backends.SSH.Hosts {
			ssh := NewSSHBackend(hostCfg.Host, hostCfg.User, hostCfg.KeyPath, hostCfg.WorkDir)
			globalBackendManager.Register("ssh-"+name, ssh)
		}
	}
	if config.Backends.Default != "" && config.Backends.Default != "local" {
		if err := globalBackendManager.SetDefault(config.Backends.Default); err != nil {
			log.Printf("⚠️ Warning: failed to set default backend to %s: %v", config.Backends.Default, err)
		}
	}
	log.Printf("   Execution backends: %d registered (default=%s)", len(globalBackendManager.ListAll()), globalBackendManager.DefaultName())

	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()

	if watcher, err := StartCodexSessionWatcher(appCtx, config.CodexInterception); err != nil {
		log.Printf("⚠️ Warning: failed to start Codex session watcher: %v", err)
	} else if watcher != nil {
		log.Printf("   Codex session watcher: enabled")
	}

	// Initialize Cron Scheduler
	if config.EnablePersistence && globalStorage != nil {
		globalCronScheduler = NewCronScheduler(tgBot, client)
		if err := globalCronScheduler.Start(); err != nil {
			log.Printf("⚠️ Warning: failed to start cron scheduler: %v", err)
		} else {
			log.Printf("   Cron scheduler: enabled")
		}
	}

	// Start web interface if enabled
	var webInterface *WebInterface
	var webErrCh <-chan error
	if config.EnableWebInterface {
		webInterface = NewWebInterface(tgBot, config.WebPort, config.WebStaticDir, config.WebAPIToken)
		errCh := make(chan error, 1)
		webErrCh = errCh
		go func() {
			done := globalJobTracker.Start("web.server")
			var jobErr error
			defer func() { done(jobErr) }()
			if err := webInterface.Start(); err != nil {
				jobErr = err
				errCh <- err
			}
		}()
		select {
		case err := <-errCh:
			log.Fatalf("❌ Web interface failed to start: %v", err)
		case <-time.After(500 * time.Millisecond):
			log.Printf("   Web interface: listening on port %s", config.WebPort)
		}
	}

	// Start periodic data cleanup if persistence is enabled
	if config.EnablePersistence && config.EnableDataCleanup && globalStorage != nil {
		go func() {
			done := globalJobTracker.Start("cleanup.data")
			defer done(nil)
			ticker := time.NewTicker(24 * time.Hour) // Run cleanup daily
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					if err := globalStorage.CleanupOldData(config.DataRetentionDays); err != nil {
						log.Printf("❌ Data cleanup error: %v", err)
					}
				case <-appCtx.Done():
					return
				}
			}
		}()
		log.Printf("   Data cleanup: scheduled daily (retention: %d days)", config.DataRetentionDays)
	}

	// Start periodic multimedia file cleanup
	if config.Multimedia.EnablePhotoSupport || config.Multimedia.EnableVoiceSupport {
		go func() {
			done := globalJobTracker.Start("cleanup.media")
			defer done(nil)
			ticker := time.NewTicker(6 * time.Hour) // Clean temp files every 6 hours
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					CleanupTempMediaFiles(config.Multimedia.TempDownloadDir, 1*time.Hour) // Remove files older than 1 hour
				case <-appCtx.Done():
					return
				}
			}
		}()
		log.Printf("   Multimedia cleanup: scheduled every 6h (remove files older than 1h)")
	}

	// Start Telegram bot in separate goroutine
	go func() {
		done := globalJobTracker.Start("telegram.polling")
		defer done(nil)
		tgBot.Start()
	}()

	// Wait for interrupt signals for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	var shutdownReason string
	select {
	case sig := <-quit:
		shutdownReason = fmt.Sprintf("signal %s", sig)
	case err := <-webErrCh:
		shutdownReason = fmt.Sprintf("web interface error: %v", err)
	}

	log.Printf("🛑 Shutting down (%s)...", shutdownReason)

	cancelApp()
	tgBot.Stop()

	// Stop cron scheduler
	if globalCronScheduler != nil {
		globalCronScheduler.Stop()
		log.Printf("✅ Cron scheduler stopped")
	}

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
