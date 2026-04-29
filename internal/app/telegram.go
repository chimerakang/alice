package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	appengine "claude-tg-agent/internal/app/engine"
	"claude-tg-agent/internal/app/hermes"
	"claude-tg-agent/internal/app/security"
	"claude-tg-agent/internal/app/task"
)

// chatKey 用於識別獨立對話（支援 Forum Topics）
type chatKey struct {
	chatID   int64
	threadID int
}

// MediaBatch 表示一批媒體訊息（支援多張圖片批次處理）
type MediaBatch struct {
	Photos       []PhotoSize
	Caption      string
	MediaGroupID string
	UserID       int64
	ChatKey      chatKey
	MessageID    int // 第一條消息的 message ID，用於 PII 檢測上下文
	FirstSeen    time.Time
	LastSeen     time.Time
	timer        *time.Timer // 用於延遲處理的計時器
}

// PhotoSize Telegram 圖片大小資訊
type PhotoSize struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	FileSize     int    `json:"file_size,omitempty"`
}

// Voice Telegram 語音訊息資訊
type Voice struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Duration     int    `json:"duration"`
	MimeType     string `json:"mime_type,omitempty"`
	FileSize     int    `json:"file_size,omitempty"`
}

// Document Telegram 文件資訊
type Document struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	FileName     string `json:"file_name,omitempty"`
	MimeType     string `json:"mime_type,omitempty"`
	FileSize     int    `json:"file_size,omitempty"`
}

// TelegramMessage represents a message to be sent through the rate-limited queue
type TelegramMessage struct {
	Method         string                 `json:"method"`
	Params         map[string]interface{} `json:"params"`
	FallbackParams map[string]interface{} `json:"fallback_params,omitempty"` // used when parse_mode causes errors
	Retries        int                    `json:"retries"`
	MaxRetries     int                    `json:"max_retries"`
	CreatedAt      time.Time              `json:"created_at"`
}

type TelegramBot struct {
	agents   map[chatKey]*Agent // 每個 chat/topic 一個 agent
	agentsMu sync.RWMutex       // 保護 agents map 的讀寫鎖
	client   Client
	allowIDs map[int64]bool // 白名單
	config   *Config
	i18n     *I18nManager // 多國語系管理器

	// 媒體批次處理
	mediaBatches map[string]*MediaBatch // mediaGroupID 或 chatKey 作為 key
	batchMu      sync.RWMutex           // 保護 mediaBatches map
	batchTimeout time.Duration          // 批次收集超時時間

	// Rate limiting and message queue
	messageQueue chan *TelegramMessage // 訊息佇列
	queueCtx     context.Context       // 佇列上下文
	queueCancel  context.CancelFunc    // 佇列取消函數
	pollCtx      context.Context
	pollCancel   context.CancelFunc
	rateLimiter  *time.Ticker // 速率限制器

	apiHTTPClient      *http.Client
	longPollHTTPClient *http.Client
	downloadHTTPClient *http.Client

	// Model routing preferences per chat/thread
	chatContexts map[chatKey]*ChatContext // Shared conversation state per chat/topic
	prefMu       sync.RWMutex             // Protect chat context preferences

	// Language preferences per chat
	langPreferences map[int64]string // chatID -> language code
	langPrefMu      sync.RWMutex     // Protect language preferences

	// Track last used Topic for each chat (for @mention recovery when threadID=0)
	lastUsedThreadID map[int64]int // chatID -> last non-zero threadID
	lastUsedMu       sync.RWMutex  // Protect lastUsedThreadID

	// Screenshot manager for /preview command
	screenshotManager *ScreenshotManager

	// Hermes Brain-Executor coordinators (per chat)
	hermesCoords map[chatKey]*hermesCoord
	hermesMu     sync.RWMutex

	// Unified task graph — single TaskService instance shared across /retry,
	// Hermes coordinator, and dashboard read paths for a consistent view.
	taskSvc *task.Service
}

type telegramProgressSink struct {
	onUpdate func(string, bool)
}

func newTelegramProgressSink(onUpdate func(string, bool)) appengine.ProgressSink {
	return &telegramProgressSink{onUpdate: onUpdate}
}

func (s *telegramProgressSink) OnSubTaskStart(idx, total int, desc string) {
	// Agent.Run already emits the user-facing initial status. Keep this event
	// structural so Telegram does not send the raw user prompt as a status.
}

func (s *telegramProgressSink) OnToolUse(tool string, input map[string]any) {
	if s.onUpdate != nil && tool != "" {
		s.onUpdate(tool, true)
	}
}

func (s *telegramProgressSink) OnContent(kind, text string) {
	if s.onUpdate == nil || text == "" {
		return
	}
	s.onUpdate(text, kind != "status")
}

func (s *telegramProgressSink) OnSubTaskDone(idx int, result string) {}

func (s *telegramProgressSink) OnComplete(summary string) {}

const (
	hermesPreviousContextHeader = "[Previous conversation context]"
	hermesCurrentRequestHeader  = "[Current request]"
	hermesContextMaxChars       = 2000
	telegramAPITimeout          = 20 * time.Second
	telegramLongPollTimeout     = 75 * time.Second
	telegramDownloadTimeout     = 2 * time.Minute
)

var hermesFetchIssue = hermes.FetchIssue

var (
	tasksGitHubIssueListFunc = listGitHubIssuesForTasks
	tasksGitHubRepoURLFunc   = resolveGitHubRepoURL
)

var (
	errTasksGitHubAuthRequired = errors.New("tasks github auth required")
	errTasksNoGitHubRepo       = errors.New("tasks github repo unavailable")
)

// hermesCoord bundles the coordinator and its enabled flag for a single chat.
type hermesCoord struct {
	coord interface {
		TaskID() string
		IsRunning() bool
	}
	enabled             bool
	strictModeOverride  *bool
	tier                string        // "" or "claude" → Claude tier; "codex" → GPT tier (set by /ghermes)
	plannerSessionID    string        // cached Planner --resume session for the current tier
	plannerSessionTier  string        // tier that produced plannerSessionID
	executorSessionID   string        // cached Executor thread resume ID for the current tier
	executorSessionTier string        // tier that produced executorSessionID
	continueCh          chan struct{} // non-nil when coordinator is paused on a budget warning
	oneShot             bool          // true when launched via /hermes #N or /ghermes #N; disable on done
}

type interruptibleCoordinator interface {
	InterruptWith(messageID int64)
	IsRunning() bool
}

type abortTaskResult int

const (
	abortTaskNone abortTaskResult = iota
	abortTaskAborted
	abortTaskFinished
)

func (t *TelegramBot) abortActiveTask(key chatKey, messageID int64) abortTaskResult {
	t.hermesMu.RLock()
	hc := t.hermesCoords[key]
	var coord interface {
		TaskID() string
		IsRunning() bool
	}
	if hc != nil {
		coord = hc.coord
	}
	t.hermesMu.RUnlock()

	if coord != nil && coord.IsRunning() {
		if interrupter, ok := coord.(interruptibleCoordinator); ok {
			interrupter.InterruptWith(messageID)
			return abortTaskAborted
		}
		return abortTaskFinished
	}

	agent := t.getAgent(key)
	if !agent.IsProcessing() {
		return abortTaskNone
	}
	if agent.Abort() {
		return abortTaskAborted
	}
	return abortTaskFinished
}

func NewTelegramBot(config *Config, client Client) (*TelegramBot, error) {
	apiHTTPClient := &http.Client{Timeout: telegramAPITimeout}
	// 驗證 bot token
	resp, err := apiHTTPClient.Get(fmt.Sprintf("https://api.telegram.org/bot%s/getMe", config.TelegramToken))
	if err != nil {
		return nil, fmt.Errorf("telegram bot init: %w", err)
	}
	defer resp.Body.Close()

	var me struct {
		OK     bool `json:"ok"`
		Result struct {
			Username string `json:"username"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil || !me.OK {
		return nil, fmt.Errorf("telegram bot auth failed")
	}

	allowIDs := make(map[int64]bool)
	for _, id := range config.AllowedUserIDs {
		allowIDs[id] = true
	}

	log.Printf("[telegram] bot authorized: @%s", me.Result.Username)

	// Initialize i18n manager
	i18nManager, err := NewI18nManager("locales", "en")
	if err != nil {
		log.Printf("[telegram] warning: i18n initialization failed: %v", err)
		// Don't fail - we can still run without i18n
		i18nManager = nil
	}

	// Initialize context for message queue
	queueCtx, queueCancel := context.WithCancel(context.Background())
	pollCtx, pollCancel := context.WithCancel(context.Background())

	bot := &TelegramBot{
		agents:       make(map[chatKey]*Agent),
		client:       client,
		allowIDs:     allowIDs,
		config:       config,
		i18n:         i18nManager,
		mediaBatches: make(map[string]*MediaBatch),
		batchTimeout: 5 * time.Second, // 5秒批次收集窗口

		// Rate limiting - Telegram Bot API allows ~30 msg/sec for groups, ~3 msg/sec for private chats
		// We'll be conservative and use 2 msg/sec to avoid rate limits
		messageQueue: make(chan *TelegramMessage, 1000), // Large buffer for queuing
		queueCtx:     queueCtx,
		queueCancel:  queueCancel,
		pollCtx:      pollCtx,
		pollCancel:   pollCancel,
		rateLimiter:  time.NewTicker(500 * time.Millisecond), // 2 messages per second

		apiHTTPClient:      apiHTTPClient,
		longPollHTTPClient: &http.Client{Timeout: telegramLongPollTimeout},
		downloadHTTPClient: &http.Client{Timeout: telegramDownloadTimeout},

		// Model routing preferences
		chatContexts:     make(map[chatKey]*ChatContext),
		langPreferences:  make(map[int64]string),
		lastUsedThreadID: make(map[int64]int), // Track last used Topic for @mention recovery

		// Screenshot manager
		screenshotManager: NewScreenshotManager(),

		// Hermes coordinators
		hermesCoords: make(map[chatKey]*hermesCoord),

		// Unified task graph — initialized here so all read paths share one store.
		taskSvc: task.New(buildHermesTaskStore()),
	}

	// Start message queue worker
	go bot.runTrackedJob("telegram.message_queue", bot.messageQueueWorker)

	// 註冊 Telegram 指令選單
	bot.registerCommands()

	// Load persisted chat language preferences from database (background operation)
	if globalStorage != nil {
		go func() {
			done := globalJobTracker.Start("telegram.load_language_preferences")
			defer done(nil)
			// This is a best-effort load - we don't fail the bot if this doesn't work
			// Language preferences will be loaded on-demand if not cached
			log.Printf("[telegram] loading persisted chat language preferences...")
		}()
	}

	return bot, nil
}

func (t *TelegramBot) telegramAPIClient() *http.Client {
	if t != nil && t.apiHTTPClient != nil {
		return t.apiHTTPClient
	}
	return &http.Client{Timeout: telegramAPITimeout}
}

func (t *TelegramBot) telegramLongPollClient() *http.Client {
	if t != nil && t.longPollHTTPClient != nil {
		return t.longPollHTTPClient
	}
	return &http.Client{Timeout: telegramLongPollTimeout}
}

func (t *TelegramBot) telegramDownloadClient() *http.Client {
	if t != nil && t.downloadHTTPClient != nil {
		return t.downloadHTTPClient
	}
	return &http.Client{Timeout: telegramDownloadTimeout}
}

func (t *TelegramBot) Stop() {
	if t == nil {
		return
	}
	if t.pollCancel != nil {
		t.pollCancel()
	}
	if t.queueCancel != nil {
		t.queueCancel()
	}
}

func (t *TelegramBot) runTrackedJob(name string, fn func()) {
	done := globalJobTracker.Start(name)
	defer done(nil)
	fn()
}

// registerCommands 透過 Telegram Bot API 註冊指令自動完成選單
func (t *TelegramBot) registerCommands() {
	commands := []map[string]string{
		{"command": "project", "description": "Switch project directory"},
		{"command": "reset", "description": "Clear conversation history"},
		{"command": "menu", "description": "Open visual command menu"},
		{"command": "status", "description": "View current status"},
		{"command": "usage", "description": "View token usage"},
		{"command": "fast", "description": "Switch to fast mode (Haiku)"},
		{"command": "deep", "description": "Switch to configured deep model"},
		{"command": "auto", "description": "Auto routing mode (AI decides)"},
		{"command": "abort", "description": "Abort running task"},
		{"command": "dashboard", "description": "View system monitoring dashboard"},
		{"command": "checkpoints", "description": "View checkpoint status"},
		{"command": "multiagent", "description": "Multi-agent coordination management"},
		{"command": "agents", "description": "View specialized agent list"},
		{"command": "tasks", "description": "View to-do list"},
		{"command": "lang", "description": "Switch bot language"},
		{"command": "preview", "description": "Preview webpage screenshot"},
		{"command": "strict", "description": "Toggle strict review mode"},
		{"command": "retry", "description": "Retry reviewed low-score sub-task"},
		{"command": "help", "description": "Show help message"},
	}

	body, _ := json.Marshal(map[string]interface{}{"commands": commands})
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/setMyCommands", t.config.TelegramToken)
	resp, err := t.telegramAPIClient().Post(apiURL, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("[telegram] setMyCommands error: %v", err)
		return
	}
	resp.Body.Close()
	log.Printf("[telegram] bot commands registered (%d commands)", len(commands))
}

func (t *TelegramBot) getAgent(key chatKey) *Agent {
	// 先用讀鎖檢查是否已存在
	t.agentsMu.RLock()
	if agent, ok := t.agents[key]; ok {
		t.agentsMu.RUnlock()
		return agent
	}
	t.agentsMu.RUnlock()

	// 如果不存在，用寫鎖創建新的 agent
	t.agentsMu.Lock()
	defer t.agentsMu.Unlock()

	// 雙重檢查，防止在獲取寫鎖期間其他 goroutine 已創建
	if agent, ok := t.agents[key]; ok {
		return agent
	}

	// 從資料庫還原 topic 對應的專案目錄，找不到則用預設值
	projectDir := t.config.DefaultProjectDir
	if globalStorage != nil {
		if saved, err := globalStorage.GetTopicSetting(key.chatID, key.threadID); err == nil && saved != "" {
			projectDir = saved
			log.Printf("[telegram] restored project dir for chat=%d thread=%d: %s", key.chatID, key.threadID, saved)
		}
	}

	agent := NewAgentWithContext(t.client, t.getChatContext(key, projectDir))
	agent.cliTimeoutMinutes = t.config.CLITimeoutMinutes
	t.agents[key] = agent
	return agent
}

func (t *TelegramBot) getChatContext(key chatKey, projectDir string) *ChatContext {
	t.prefMu.Lock()
	defer t.prefMu.Unlock()

	if t.chatContexts == nil {
		t.chatContexts = make(map[chatKey]*ChatContext)
	}
	ctx := t.chatContexts[key]
	if ctx == nil {
		ctx = NewChatContext(key.chatID, key.threadID, projectDir)
		if globalStorage != nil {
			if pref, err := globalStorage.GetTopicModelPreference(key.chatID, key.threadID); err == nil && pref != "" {
				ctx.Pref = ModelPreference(pref)
				log.Printf("[telegram] restored model preference for chat=%d thread=%d: %s", key.chatID, key.threadID, pref)
			} else if err != nil {
				log.Printf("[telegram] failed to restore model preference for chat=%d thread=%d: %v", key.chatID, key.threadID, err)
			}
		}
		t.chatContexts[key] = ctx
	}
	if projectDir != "" {
		ctx.ProjectDir = projectDir
	}
	return ctx
}

// GetAgentsSafely 安全地獲取所有 agents 的副本，供 Web 介面使用
func (t *TelegramBot) GetAgentsSafely() map[chatKey]*Agent {
	t.agentsMu.RLock()
	defer t.agentsMu.RUnlock()

	// 創建副本以避免併發存取問題
	agents := make(map[chatKey]*Agent)
	for key, agent := range t.agents {
		agents[key] = agent
	}
	return agents
}

// GetAgentCount 安全地獲取 agent 數量
func (t *TelegramBot) GetAgentCount() int {
	t.agentsMu.RLock()
	defer t.agentsMu.RUnlock()
	return len(t.agents)
}

// getUserModelPreference 安全地獲取用戶的模型偏好設定
// 返回值: "fast", "deep", 或 "" (自動模式)
func (t *TelegramBot) getUserModelPreference(key chatKey) string {
	t.prefMu.RLock()
	defer t.prefMu.RUnlock()
	if ctx := t.chatContexts[key]; ctx != nil {
		return string(ctx.Pref)
	}
	return ""
}

// setUserModelPreference 安全地設定用戶的模型偏好設定
// mode: "fast", "deep", 或 "" (自動模式)
func (t *TelegramBot) setUserModelPreference(key chatKey, mode string) {
	t.prefMu.Lock()
	defer t.prefMu.Unlock()
	if t.chatContexts == nil {
		t.chatContexts = make(map[chatKey]*ChatContext)
	}
	ctx := t.chatContexts[key]
	if ctx == nil {
		ctx = NewChatContext(key.chatID, key.threadID, t.config.DefaultProjectDir)
		t.chatContexts[key] = ctx
	}
	ctx.Pref = ModelPreference(mode)
	if globalStorage != nil {
		if err := globalStorage.SaveTopicModelPreference(key.chatID, key.threadID, ctx.ProjectDir, mode); err != nil {
			log.Printf("[telegram] failed to save model preference for chat=%d thread=%d: %v", key.chatID, key.threadID, err)
		}
	}
}

// handleSavingsCommand 處理 /savings 指令 - 顯示本週路由統計和節省金額
// 支持 /savings 或 /savings <project_path>
func (t *TelegramBot) handleSavingsCommand(key chatKey, projectPath string) {
	if globalStorage == nil {
		t.send(key, t.getLocalizedMessage(key.chatID, "no_storage", nil))
		return
	}

	// 默認查詢最近 7 天的數據
	var report CostSavingsReport
	var err error
	if projectPath != "" {
		report, err = globalStorage.GetCostSavingsByProject(projectPath, 168)
	} else {
		report, err = globalStorage.GetCostSavings(168)
	}
	if err != nil {
		log.Printf("[telegram] failed to get cost savings: %v", err)
		msg := t.getLocalizedMessage(key.chatID, "error_get_cost", map[string]string{"error": err.Error()})
		t.send(key, msg)
		return
	}

	if report.TotalRequests == 0 {
		t.send(key, t.getLocalizedMessage(key.chatID, "no_routing_data", nil))
		return
	}

	// 組建回應訊息
	var msg strings.Builder
	titleMsg := t.getLocalizedMessage(key.chatID, "task_savings_title", nil)
	msg.WriteString(titleMsg)

	// 按模型分類
	for model, breakdown := range report.ByModel {
		modelIcon := "🟢"
		if model == "sonnet" {
			modelIcon = "🟡"
		} else if model == "opus" {
			modelIcon = "🔴"
		}

		savedAmount := breakdown.Saved
		savedSign := ""
		if savedAmount > 0 {
			savedSign = "✅"
		} else if savedAmount < 0 {
			savedSign = "⬆️"
		}

		breakdownMsg := t.getLocalizedMessage(key.chatID, "task_savings_model_breakdown", nil)
		breakdownMsg = strings.ReplaceAll(breakdownMsg, "{icon}", modelIcon)
		breakdownMsg = strings.ReplaceAll(breakdownMsg, "{model}", model)
		breakdownMsg = strings.ReplaceAll(breakdownMsg, "{calls}", fmt.Sprintf("%d", breakdown.Calls))
		breakdownMsg = strings.ReplaceAll(breakdownMsg, "{cost}", fmt.Sprintf("%.2f", breakdown.ActualCost))
		breakdownMsg = strings.ReplaceAll(breakdownMsg, "{sonnet_cost}", fmt.Sprintf("%.2f", breakdown.WouldHaveCost))
		breakdownMsg = strings.ReplaceAll(breakdownMsg, "{status}", savedSign)
		msg.WriteString(breakdownMsg)
	}

	// 節省統計
	costHeaderMsg := t.getLocalizedMessage(key.chatID, "task_savings_cost_header", nil)
	msg.WriteString(costHeaderMsg)

	actualMsg := t.getLocalizedMessage(key.chatID, "task_savings_actual_cost", nil)
	actualMsg = strings.ReplaceAll(actualMsg, "{cost}", fmt.Sprintf("%.2f", report.ActualCost))
	msg.WriteString(actualMsg)

	assumedMsg := t.getLocalizedMessage(key.chatID, "task_savings_assumed_cost", nil)
	assumedMsg = strings.ReplaceAll(assumedMsg, "{cost}", fmt.Sprintf("%.2f", report.DefaultModelCost))
	msg.WriteString(assumedMsg)

	amountMsg := t.getLocalizedMessage(key.chatID, "task_savings_amount", nil)
	amountMsg = strings.ReplaceAll(amountMsg, "{savings}", fmt.Sprintf("%.2f", report.SavingsCost))
	amountMsg = strings.ReplaceAll(amountMsg, "{percent}", fmt.Sprintf("%.1f", report.SavingsPercent))
	msg.WriteString(amountMsg)

	// 路由方式統計
	if len(report.RoutingMethodStat) > 0 {
		methodHeaderMsg := t.getLocalizedMessage(key.chatID, "task_savings_method_header", nil)
		msg.WriteString(methodHeaderMsg)
		for method, count := range report.RoutingMethodStat {
			percent := 0.0
			if report.TotalRequests > 0 {
				percent = float64(count) / float64(report.TotalRequests) * 100
			}
			methodItemMsg := t.getLocalizedMessage(key.chatID, "task_savings_method_item", nil)
			methodItemMsg = strings.ReplaceAll(methodItemMsg, "{method}", method)
			methodItemMsg = strings.ReplaceAll(methodItemMsg, "{count}", fmt.Sprintf("%d", count))
			methodItemMsg = strings.ReplaceAll(methodItemMsg, "{percent}", fmt.Sprintf("%.1f", percent))
			msg.WriteString(methodItemMsg)
		}
	}

	t.send(key, msg.String())
}

func (t *TelegramBot) isAllowed(userID int64) bool {
	if len(t.allowIDs) == 0 {
		return true // 沒設白名單就全部放行
	}
	return t.allowIDs[userID]
}

func (t *TelegramBot) Start() {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s", t.config.TelegramToken)

	log.Println("[telegram] listening for messages...")

	offset := 0
	for {
		select {
		case <-t.pollCtx.Done():
			log.Printf("[telegram] polling stopped")
			return
		default:
		}
		req, err := http.NewRequestWithContext(t.pollCtx, http.MethodGet, fmt.Sprintf("%s/getUpdates?offset=%d&timeout=60", apiURL, offset), nil)
		if err != nil {
			log.Printf("[telegram] getUpdates request error: %v", err)
			continue
		}
		resp, err := t.telegramLongPollClient().Do(req)
		if err != nil {
			if t.pollCtx.Err() != nil {
				log.Printf("[telegram] polling stopped")
				return
			}
			log.Printf("[telegram] getUpdates error: %v", err)
			continue
		}

		var result struct {
			OK     bool `json:"ok"`
			Result []struct {
				UpdateID int `json:"update_id"`
				Message  *struct {
					MessageID       int  `json:"message_id"`
					MessageThreadID int  `json:"message_thread_id"`
					IsTopicMessage  bool `json:"is_topic_message"`
					ReplyToMessage  *struct {
						MessageID       int `json:"message_id"`
						MessageThreadID int `json:"message_thread_id"`
					} `json:"reply_to_message"`
					From *struct {
						ID int64 `json:"id"`
					} `json:"from"`
					Chat *struct {
						ID int64 `json:"id"`
					} `json:"chat"`
					Text         string      `json:"text"`
					Caption      string      `json:"caption"`
					Photo        []PhotoSize `json:"photo"`
					Voice        *Voice      `json:"voice"`
					Document     *Document   `json:"document"`
					MediaGroupID string      `json:"media_group_id"`
				} `json:"message"`
				CallbackQuery *struct {
					ID   string `json:"id"`
					From *struct {
						ID int64 `json:"id"`
					} `json:"from"`
					Message *struct {
						MessageID       int `json:"message_id"`
						MessageThreadID int `json:"message_thread_id"`
						Chat            *struct {
							ID int64 `json:"id"`
						} `json:"chat"`
					} `json:"message"`
					Data string `json:"data"`
				} `json:"callback_query"`
			} `json:"result"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			log.Printf("[telegram] decode error: %v", err)
			continue
		}
		resp.Body.Close()

		if !result.OK {
			continue
		}

		for _, update := range result.Result {
			offset = update.UpdateID + 1

			// Handle regular messages
			if update.Message != nil && update.Message.Chat != nil && update.Message.From != nil {
				msg := update.Message
				key := chatKey{chatID: msg.Chat.ID, threadID: msg.MessageThreadID}

				// Recover threadID from reply_to_message (forum topic root message)
				// In Telegram forums, messages in topics reply to the topic root, which has the thread ID
				if key.threadID == 0 && msg.ReplyToMessage != nil {
					if msg.ReplyToMessage.MessageThreadID != 0 {
						key.threadID = msg.ReplyToMessage.MessageThreadID
						log.Printf("[telegram] Recovered threadID=%d from reply_to_message.message_thread_id", key.threadID)
					} else if msg.ReplyToMessage.MessageID != 0 && msg.IsTopicMessage {
						key.threadID = msg.ReplyToMessage.MessageID
						log.Printf("[telegram] Recovered threadID=%d from reply_to_message.message_id (topic message)", key.threadID)
					}
				}

				log.Printf("[telegram] Received message: text='%.50s', threadID=%d, chatID=%d, is_topic=%v",
					msg.Text, key.threadID, msg.Chat.ID, msg.IsTopicMessage)

				// Track last used topic per chat (for recovery when threadID=0)
				if key.threadID != 0 {
					t.lastUsedMu.Lock()
					t.lastUsedThreadID[key.chatID] = key.threadID
					t.lastUsedMu.Unlock()
				}
				go func() {
					done := globalJobTracker.Start("telegram.message")
					defer done(nil)
					t.handleMessage(key, msg.From.ID, msg.Text, msg.Caption, msg.Photo, msg.Voice, msg.Document, msg.MediaGroupID, msg.MessageID)
				}()
			}

			// Handle callback queries (inline keyboard button clicks)
			if update.CallbackQuery != nil && update.CallbackQuery.Message != nil {
				query := update.CallbackQuery
				key := chatKey{chatID: query.Message.Chat.ID, threadID: query.Message.MessageThreadID}
				go func() {
					done := globalJobTracker.Start("telegram.callback")
					defer done(nil)
					t.handleCallbackQuery(key, query.From.ID, query.ID, query.Data)
				}()
			}
		}
	}
}

func (t *TelegramBot) handleMessage(key chatKey, userID int64, text string, caption string, photo []PhotoSize, voice *Voice, document *Document, mediaGroupID string, messageID int) {
	// Recover from panics to prevent goroutine death leaving status messages stuck
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[telegram] PANIC in handleMessage (chat=%d, thread=%d): %v", key.chatID, key.threadID, r)
		}
	}()

	// 調試日誌
	var voiceInfo string = "none"
	if voice != nil {
		voiceInfo = fmt.Sprintf("duration=%ds,size=%d", voice.Duration, voice.FileSize)
	}
	var documentInfo string = "none"
	if document != nil {
		documentInfo = fmt.Sprintf("name=%s,size=%d,type=%s", document.FileName, document.FileSize, document.MimeType)
	}
	log.Printf("[telegram] handleMessage: userID=%d, text='%s', caption='%s', photo_count=%d, voice=%s, document=%s, chatID=%d, threadID=%d",
		userID, text, caption, len(photo), voiceInfo, documentInfo, key.chatID, key.threadID)

	// 權限檢查
	if !t.isAllowed(userID) {
		log.Printf("[telegram] user %d not allowed", userID)
		t.send(key, t.getLocalizedMessage(key.chatID, "permission_denied", nil))
		return
	}

	text = strings.TrimSpace(text)
	caption = strings.TrimSpace(caption)

	// Handle photo messages - 支援批次處理
	if len(photo) > 0 {
		log.Printf("[telegram] handling photo message with %d photos, mediaGroupID=%s", len(photo), mediaGroupID)
		t.handlePhotoMessageBatch(key, userID, photo, caption, mediaGroupID, messageID)
		return
	}

	// Handle voice messages
	if voice != nil {
		log.Printf("[telegram] handling voice message: duration=%ds, size=%d bytes", voice.Duration, voice.FileSize)
		t.handleVoiceMessage(key, userID, voice, caption, messageID)
		return
	}

	// Handle document messages
	if document != nil {
		log.Printf("[telegram] handling document message: name=%s, size=%d bytes, type=%s", document.FileName, document.FileSize, document.MimeType)
		t.handleDocumentMessage(key, userID, document, caption, messageID)
		return
	}

	if text == "" {
		return
	}

	// 安全檢查和 PII 檢測
	if security.Global() != nil {
		// 記錄用戶請求安全事件
		security.Global().LogSecurityEvent(security.SecurityEvent{
			EventType:   "telegram_message_received",
			Severity:    "low",
			Description: "User message received via Telegram",
			UserID:      userID,
			Details: map[string]interface{}{
				"message_length": len(text),
				"chat_id":        key.chatID,
				"has_commands":   strings.HasPrefix(text, "/"),
			},
		})

		// PII 檢測和過濾 (自動記錄事件)
		filteredText, detected := security.Global().DetectAndFilterPII(text, true, &security.PIIDetectionContext{
			ChatID:      key.chatID,
			UserID:      userID,
			MessageType: "text",
			SourceType:  "telegram",
			ProjectPath: t.config.DefaultProjectDir,
			MessageID:   messageID,
		})
		if len(detected) > 0 {
			// PII 事件已由 DetectAndFilterPII 自動記錄
			// 警告用戶並使用過濾後的文字
			t.send(key, t.getLocalizedMessage(key.chatID, "pii_detected", nil))
			text = filteredText
		}
	}

	// 處理指令
	if strings.HasPrefix(text, "/") {
		// Reject @mention commands in forum (Telegram doesn't provide threadID)
		// Force users to type commands directly in the topic they want to interact with
		if key.threadID == 0 {
			if strings.HasPrefix(text, "/tasks") {
				t.send(key, tasksNoRepoMessage(t.getChatLanguage(key.chatID)))
				return
			}
			log.Printf("[telegram] @mention command rejected (threadID=0): '%s' from user %d", text, userID)

			// Send helpful instruction in General topic
			msg := "❌ **@mention 命令不支援**\n\n" +
				"請在您要互動的主題（Topic）中**直接輸入命令**，例如：\n\n" +
				"```\n/help\n/status\n/usage\n```\n\n" +
				"這樣 Alice 才能在正確的主題中回應您。"
			t.send(key, msg)
			return
		}
		log.Printf("[telegram] handling command: %s, threadID=%d, chatID=%d", text, key.threadID, key.chatID)
		t.handleCommand(key, text)
		return
	}

	// Budget-warning continuation: if the coordinator is paused waiting for the
	// user to confirm, intercept messages that start with "繼續" (or "continue")
	// and signal the channel instead of routing as a new task.
	if t.trySignalBudgetContinue(key, text) {
		return
	}

	if t.config.Hermes.Enabled && isHermesIssueReferenceRequest(text) {
		projectDir := t.getAgent(key).ProjectDir()
		issueNum, _ := ParseIssueNumber(text)
		t.send(key, fmt.Sprintf("🔍 偵測到指定 Issue #%d，將以該 Issue 為準啟動 Hermes…", issueNum))
		go t.runTrackedJob("hermes.issue", func() {
			t.startHermesFromIssue(key, issueNum, projectDir)
		})
		return
	}

	if t.config.Hermes.Enabled && isHermesContinuationRequest(text) {
		projectDir := t.getAgent(key).ProjectDir()
		if task, ok := t.resolveHermesContinuationTask(key, projectDir); ok {
			mode := hermesContinuationModeFromRequest(text)
			t.send(key, fmt.Sprintf("🔁 偵測到你想%s Hermes 任務 %s，將只規劃剩餘工作…", hermesContinuationVerb(mode), shortHermesTaskID(task.ID)))
			go t.runTrackedJob("hermes."+mode, func() {
				t.startHermesContinuationTask(key, task, projectDir, mode)
			})
			return
		}
	}

	// Hermes mode: route to Brain-Executor coordinator instead of normal agent
	if t.isHermesEnabled(key) {
		projectDir := t.getAgent(key).ProjectDir()
		if issueNum, ok := ParseIssueNumber(text); ok {
			go t.runTrackedJob("hermes.issue", func() {
				t.startHermesFromIssue(key, issueNum, projectDir)
			})
			return
		}
		go t.runTrackedJob("hermes.task", func() {
			t.startHermesTask(key, text, projectDir)
		})
		return
	}

	// Complexity Gate auto-routing: if enabled, natural-language messages
	// classified as complex start Hermes automatically. Continuation messages
	// (pronouns, short follow-ups) stay on the regular routing path so we do
	// not hijack conversational context.
	if t.config.Hermes.Enabled && t.config.Hermes.AutoRouteComplex && !isContinuationMessage(text) {
		cl := ClassifyComplexity(text)
		if cl.Complexity == ComplexityComplex {
			projectDir := t.getAgent(key).ProjectDir()
			log.Printf("[telegram] auto-route to Hermes rule=%s chatID=%d: %.80s",
				cl.MatchedRule, key.chatID, text)

			// When the trigger was an action verb + issue reference (e.g. "處理 #250"),
			// route through startHermesFromIssue so the Planner sees the full Issue
			// body instead of the short user message — otherwise it falls back to a
			// research-only plan with no implementation sub-tasks.
			if strings.HasPrefix(cl.MatchedRule, "action-verb+issue-ref:") {
				if issueNum, ok := ParseIssueNumber(text); ok {
					t.send(key, fmt.Sprintf("🤖 判定為複雜任務（%s）— 拉取 Issue #%d 內容後啟動 Hermes", cl.MatchedRule, issueNum))
					go t.runTrackedJob("hermes.issue", func() {
						t.startHermesFromIssue(key, issueNum, projectDir)
					})
					return
				}
			}

			t.send(key, fmt.Sprintf("🤖 判定為複雜任務（%s）— 自動啟動 Hermes 模式", cl.MatchedRule))
			go t.runTrackedJob("hermes.task", func() {
				t.startHermesTask(key, text, projectDir)
			})
			return
		}
	}

	// 一般訊息 → agent 處理
	agent := t.getAgent(key)

	// Model routing: Three-tier priority system
	var modelOverride string
	if t.config.ModelRouting.EnableDynamicRouting {
		// Priority 1: User explicit preference (/fast, /smart, /deep, /gfast, /gsmart, /gdeep)
		userPref := t.getUserModelPreference(key)
		if userPref == "fast" {
			modelOverride = t.config.ModelRouting.FastModel
			log.Printf("[telegram] model routing: using fast model (user preference)")
		} else if userPref == "smart" {
			modelOverride = t.config.ModelRouting.SmartModel
			log.Printf("[telegram] model routing: using smart model (user preference)")
		} else if userPref == "deep" {
			modelOverride = t.config.ModelRouting.DeepModel
			log.Printf("[telegram] model routing: using deep model (user preference)")
		} else if userPref == "gpt-fast" {
			modelOverride = t.config.ModelRouting.CodexFastModel
			log.Printf("[telegram] model routing: using GPT fast model (user preference)")
		} else if userPref == "gpt-smart" {
			modelOverride = t.config.ModelRouting.CodexSmartModel
			log.Printf("[telegram] model routing: using GPT smart model (user preference)")
		} else if userPref == "gpt-deep" {
			modelOverride = t.config.ModelRouting.CodexDeepModel
			log.Printf("[telegram] model routing: using GPT deep model (user preference)")
		} else if userPref == "plan" {
			if t.config.ModelRouting.PlanModel != "" && t.config.ModelRouting.ExecuteModel != "" {
				agent.SetPlanMode(true, t.config.ModelRouting.PlanModel, t.config.ModelRouting.ExecuteModel)
				log.Printf("[telegram] model routing: using plan mode (user preference)")
			}
		} else if userPref != "" {
			modelOverride = userPref
			agent.SetPlanMode(false, "", "")
			log.Printf("[telegram] model routing: using custom model %s (user preference)", userPref)
		} else if t.isStickySession(agent) {
			// Priority 2: Sticky session — session active and not idle, skip triage entirely
			log.Printf("[telegram] model routing: sticky session active (last activity: %v ago), keeping current model + session",
				time.Since(agent.LastActivity()).Round(time.Second))
		} else if isContinuationMessage(text) {
			// Priority 3: Continuation message — inherit current model + session, skip triage
			log.Printf("[telegram] model routing: continuation message detected, keeping current model + session")
			// modelOverride stays empty → agent keeps lastUsedModel + sessionID unchanged
		} else {
			// Priority 4: Hybrid triage
			// Phase A: local heuristic for high-confidence cases (0ms)
			score := evaluateTaskComplexityScore(text)
			var complexity string
			if score <= 0 {
				complexity = "fast"
				log.Printf("[telegram] model routing: local score=%d → fast (skip Haiku triage)", score)
			} else if score >= 6 {
				complexity = "deep"
				log.Printf("[telegram] model routing: local score=%d → deep (skip Haiku triage)", score)
			} else {
				// Phase B: ambiguous — call Haiku for accurate classification
				log.Printf("[telegram] model routing: local score=%d → ambiguous, calling Haiku triage", score)
				complexity = t.triageWithHaiku(context.Background(), text)
			}
			switch complexity {
			case "deep":
				// Auto-activate Plan/Execute mode for deep tasks when both phase models are configured.
				if t.config.ModelRouting.PlanModel != "" && t.config.ModelRouting.ExecuteModel != "" {
					agent.SetPlanMode(true, t.config.ModelRouting.PlanModel, t.config.ModelRouting.ExecuteModel)
					log.Printf("[telegram] model routing: classified as deep → auto plan/execute (plan=%s, exec=%s)",
						t.config.ModelRouting.PlanModel, t.config.ModelRouting.ExecuteModel)
				} else {
					modelOverride = t.config.ModelRouting.DeepModel
					log.Printf("[telegram] model routing: classified as deep (model=%s)", modelOverride)
				}
			case "balanced":
				// Keep default model - no override needed.
				agent.SetPlanMode(false, "", "") // Ensure plan mode is off
				log.Printf("[telegram] model routing: classified as balanced (default model)")
			default: // "fast"
				agent.SetPlanMode(false, "", "") // Ensure plan mode is off
				modelOverride = t.config.ModelRouting.FastModel
				log.Printf("[telegram] model routing: classified as fast (Haiku)")
			}
		}

		// Apply model override to agent
		if modelOverride != "" {
			agent.SetModelOverride(modelOverride)
		}
	}

	// 發送「處理中」提示
	t.sendTyping(key)

	var response string
	var err error
	var statusMessageID int

	// Add language preference hint to the model prompt.
	userLang := t.getChatLanguage(key.chatID)
	userMessage := text
	if userLang == "en" {
		userMessage = "Please respond in English. Do NOT use Chinese characters or Chinese formatting in your response.\n\n" + text
	} else if userLang == "zh-TW" {
		userMessage = "請用繁體中文回應。\n\n" + text
	}

	// Create enhanced update callback with stop button support
	createUpdateCallback := func() func(string, bool) {
		var firstUpdate = true
		return func(update string, silent bool) {
			if firstUpdate && !silent {
				// Send first status message with stop button
				if msgID, msgErr := t.sendMessageWithStopButton(key, update); msgErr == nil {
					statusMessageID = msgID
				} else {
					// Fallback to regular message if button fails
					t.send(key, update)
				}
				firstUpdate = false
			} else {
				// Subsequent updates
				if silent {
					t.sendSilent(key, update)
				} else {
					t.send(key, update)
				}
			}
		}
	}

	// Check if multi-agent coordination should be used
	if globalAgentCoordinator.IsEnabled() && globalAgentCoordinator.ShouldUseMultiAgent(userMessage) {
		// Use coordinated multi-agent execution
		response, err = globalAgentCoordinator.ExecuteCoordinatedTask(userMessage, agent, createUpdateCallback())
	} else if globalAgentCoordinator.IsEnabled() {
		// Use single specialized agent based on task routing
		agentType := globalAgentCoordinator.RouteTask(userMessage)
		if agentType != GeneralAgent {
			specializedAgent := globalAgentCoordinator.GetOrCreateAgent(agentType, agent)
			msg := t.getLocalizedMessage(key.chatID, "using_agent", nil)
			msg = strings.ReplaceAll(msg, "{agent}", agentType.String())
			t.send(key, msg)

			response, err = specializedAgent.ExecuteSubTask(SubTask{
				ID:          fmt.Sprintf("single_%d", time.Now().Unix()),
				Description: userMessage,
				AgentType:   agentType,
				Status:      TaskStatusInProgress,
			}, createUpdateCallback())
		} else {
			// Fall back to regular agent
			result, runErr := appengine.NewDirectEngine(agent).Run(context.Background(), userMessage, agent.chatContext, newTelegramProgressSink(createUpdateCallback()))
			response, err = result.Text, runErr
		}
	} else if agent.IsPlanMode() {
		// Plan/Execute two-phase execution
		response, err = agent.RunWithPlan(userMessage, createUpdateCallback())
	} else {
		// Regular single agent execution
		result, runErr := appengine.NewDirectEngine(agent).Run(context.Background(), userMessage, agent.chatContext, newTelegramProgressSink(createUpdateCallback()))
		response, err = result.Text, runErr
	}

	// Remove stop button after completion
	if statusMessageID != 0 {
		var finalText string
		if err != nil {
			if strings.Contains(err.Error(), "agent aborted by user") {
				finalText = t.getLocalizedMessage(key.chatID, "execution_aborted", nil)
			} else if response != "" {
				finalText = t.getLocalizedMessage(key.chatID, "execution_partial", nil)
			} else {
				finalText = t.getLocalizedMessage(key.chatID, "execution_error", nil)
			}
		} else {
			finalText = t.getLocalizedMessage(key.chatID, "execution_completed", nil)
		}
		t.editMessageRemoveStopButton(key, statusMessageID, finalText)
	}

	if err != nil {
		if strings.Contains(err.Error(), "agent aborted by user") {
			// 已由 /abort 指令回饋，不再發送重複訊息
			return
		}
		if strings.Contains(err.Error(), "context deadline exceeded") {
			if response != "" {
				t.sendLongMarkdown(key, response)
			}
			msg := t.getLocalizedMessage(key.chatID, "execution_timeout", nil)
			t.send(key, msg)
			return
		}
		errCategory := classifyError(err.Error())
		log.Printf("[telegram] execution error: category=%s chat=%d err=%s", errCategory, key.chatID, err.Error())
		if response != "" {
			t.sendLongMarkdown(key, response)
			msg := t.getLocalizedMessage(key.chatID, "error_occurred", nil)
			msg = strings.ReplaceAll(msg, "{error}", extractErrorReason(err.Error()))
			t.send(key, msg)
			return
		}
		msg := t.getLocalizedMessage(key.chatID, "error_prefix", nil)
		msg = strings.ReplaceAll(msg, "{error}", extractErrorReason(err.Error()))
		t.send(key, msg)
		return
	}

	if response == "" {
		response = t.getLocalizedMessage(key.chatID, "no_reply", nil)
	}

	// 解析 Agent 回應中的 [SEND_FILE:path] 標記並發送對應檔案
	response = t.processAgentResponse(key, response, agent.ProjectDir())

	// 加上模型標籤
	modelTag := getModelTag(agent.lastUsedModel)
	response = modelTag + "\n\n" + response

	// Telegram 訊息限制 4096 字元，分段發送（使用 Markdown 格式）
	t.sendLongMarkdown(key, response)
}

// extractErrorReason extracts a human-readable error message from API error strings.
func extractErrorReason(errStr string) string {
	// Try to extract "message" field from JSON error response
	if idx := strings.Index(errStr, `"message":"`); idx != -1 {
		start := idx + len(`"message":"`)
		if end := strings.Index(errStr[start:], `"`); end != -1 {
			return errStr[start : start+end]
		}
	}
	// Remove common prefixes for cleaner output
	errStr = strings.TrimPrefix(errStr, "CLI call failed: ")
	errStr = strings.TrimPrefix(errStr, "CLI returned error: ")
	if len(errStr) > 500 {
		return errStr[:500] + "..."
	}
	return errStr
}

// classifyError categorizes an error for better user-facing messages and logging.
func classifyError(errStr string) string {
	switch {
	case strings.Contains(errStr, "context deadline exceeded"):
		return "timeout"
	case strings.Contains(errStr, "context canceled"),
		strings.Contains(errStr, "agent aborted by user"):
		return "cancelled"
	case strings.Contains(errStr, "file_patch") || strings.Contains(errStr, "unique match"):
		return "tool_file_patch"
	case strings.Contains(errStr, "permission denied") || strings.Contains(errStr, "access denied"):
		return "permission"
	case strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such file"):
		return "not_found"
	case strings.Contains(errStr, "rate limit") || strings.Contains(errStr, "429"):
		return "rate_limit"
	case strings.Contains(errStr, "overloaded") || strings.Contains(errStr, "529"):
		return "overloaded"
	default:
		return "unknown"
	}
}

func (t *TelegramBot) handleCommand(key chatKey, text string) {
	parts := strings.Fields(text)
	cmd := strings.Split(parts[0], "@")[0] // 去掉 @botname 後綴
	log.Printf("[telegram] processing command: %s", cmd)

	switch cmd {
	case "/start", "/help":
		// Build help text using localized messages for both languages
		help := "🤖 *Alice AI Agent*\n\n"
		help += t.getLocalizedMessage(key.chatID, "help_intro", nil) + "\n\n"
		help += t.getLocalizedMessage(key.chatID, "help_forum_topics", nil) + "\n\n"
		help += t.getLocalizedMessage(key.chatID, "help_basic_commands", nil) + "\n"
		help += t.getLocalizedMessage(key.chatID, "help_project_desc", nil) + "\n"
		help += t.getLocalizedMessage(key.chatID, "help_reset_desc", nil) + "\n"
		help += t.getLocalizedMessage(key.chatID, "help_clear_desc", nil) + "\n"
		help += t.getLocalizedMessage(key.chatID, "help_status_desc", nil) + "\n"
		help += t.getLocalizedMessage(key.chatID, "help_usage_desc", nil) + "\n\n"
		help += t.getLocalizedMessage(key.chatID, "help_routing_commands", nil) + "\n"
		help += t.getLocalizedMessage(key.chatID, "help_fast_desc", nil) + "\n"
		help += t.getLocalizedMessage(key.chatID, "help_deep_desc", nil) + "\n"
		help += t.getLocalizedMessage(key.chatID, "help_auto_desc", nil) + "\n"
		help += t.getLocalizedMessage(key.chatID, "help_savings_desc", nil) + "\n\n"
		help += t.getLocalizedMessage(key.chatID, "help_advanced_commands", nil) + "\n"
		help += t.getLocalizedMessage(key.chatID, "help_dashboard_desc", nil) + "\n"
		help += t.getLocalizedMessage(key.chatID, "help_checkpoints_desc", nil) + "\n"
		help += t.getLocalizedMessage(key.chatID, "help_abort_desc", nil) + "\n"
		help += t.getLocalizedMessage(key.chatID, "help_multiagent_desc", nil) + "\n"
		help += t.getLocalizedMessage(key.chatID, "help_agents_desc", nil) + "\n"
		help += t.getLocalizedMessage(key.chatID, "help_tasks_desc", nil) + "\n"
		help += t.getLocalizedMessage(key.chatID, "help_lang_desc", nil) + "\n"
		help += "/menu - 開啟視覺化操作選單\n"
		help += "/strict - 切換 strict review mode\n"
		help += t.getLocalizedMessage(key.chatID, "help_id_desc", nil)
		t.sendMarkdown(key, help)

	case "/menu":
		t.sendMenu(key)

	case "/project":
		if len(parts) < 2 {
			t.send(key, t.getLocalizedMessage(key.chatID, "project_usage", nil))
			return
		}
		dir := parts[1]
		// 相對路徑自動補上 default project dir 的父目錄
		if !strings.HasPrefix(dir, "/") {
			base := filepath.Dir(strings.TrimRight(t.config.DefaultProjectDir, "/"))
			dir = filepath.Join(base, dir)
		}

		// 驗證路徑是否存在和有效
		if err := t.validateProjectPath(dir); err != nil {
			// 路徑驗證失敗，提供錯誤訊息和建議
			errStr := err.Error()
			var errorMsg string

			// Parse the error key:details format
			if strings.HasPrefix(errStr, "path_not_exist:") {
				path := strings.TrimPrefix(errStr, "path_not_exist:")
				locMsg := t.getLocalizedMessage(key.chatID, "path_not_exist", nil)
				errorMsg = fmt.Sprintf("❌ %s", strings.ReplaceAll(locMsg, "{path}", path))
			} else if strings.HasPrefix(errStr, "path_access_denied:") {
				parts := strings.SplitN(strings.TrimPrefix(errStr, "path_access_denied:"), ":", 2)
				path := ""
				errDetail := ""
				if len(parts) > 0 {
					path = parts[0]
				}
				if len(parts) > 1 {
					errDetail = parts[1]
				}
				locMsg := t.getLocalizedMessage(key.chatID, "path_access_denied", nil)
				errorMsg = fmt.Sprintf("❌ %s", strings.ReplaceAll(strings.ReplaceAll(locMsg, "{path}", path), "{error}", errDetail))
			} else if strings.HasPrefix(errStr, "path_not_directory:") {
				path := strings.TrimPrefix(errStr, "path_not_directory:")
				locMsg := t.getLocalizedMessage(key.chatID, "path_not_directory", nil)
				errorMsg = fmt.Sprintf("❌ %s", strings.ReplaceAll(locMsg, "{path}", path))
			} else if strings.HasPrefix(errStr, "path_permission_denied:") {
				path := strings.TrimPrefix(errStr, "path_permission_denied:")
				locMsg := t.getLocalizedMessage(key.chatID, "path_permission_denied", nil)
				errorMsg = fmt.Sprintf("❌ %s", strings.ReplaceAll(locMsg, "{path}", path))
			} else {
				errorMsg = fmt.Sprintf("❌ %s", errStr)
			}

			// 嘗試提供相似路徑建議
			suggestions := t.suggestSimilarPaths(dir)
			if len(suggestions) > 0 {
				sugMsg := t.getLocalizedMessage(key.chatID, "project_similar_suggestion", nil)
				errorMsg += "\n\n" + sugMsg
				for _, suggestion := range suggestions {
					projectName := filepath.Base(suggestion)
					errorMsg += fmt.Sprintf("\n• %s", projectName)
				}
			}

			t.send(key, errorMsg)
			return
		}

		// 路徑驗證通過，設定專案
		agent := t.getAgent(key)
		agent.SetProject(dir)

		// 持久化 topic → project 對應
		if globalStorage != nil {
			if err := globalStorage.SaveTopicSetting(key.chatID, key.threadID, dir); err != nil {
				log.Printf("[telegram] failed to save topic setting: %v", err)
			}
		}

		// 偵測專案類型
		projectType := t.detectProjectType(key.chatID, dir)
		projectName := filepath.Base(dir)

		// 建構成功訊息
		successMsg := t.getLocalizedMessage(key.chatID, "project_set", nil)
		successMsg = strings.ReplaceAll(successMsg, "{name}", projectName)
		pathMsg := t.getLocalizedMessage(key.chatID, "project_path", nil)
		pathMsg = strings.ReplaceAll(pathMsg, "{path}", dir)
		successMsg += "\n" + pathMsg
		typeMsg := t.getLocalizedMessage(key.chatID, "project_type", nil)
		typeMsg = strings.ReplaceAll(typeMsg, "{type}", projectType)
		successMsg += "\n" + typeMsg

		// 檢查是否有 MASTER_TASKS.md (用於 /tasks 功能)
		tasksFile := filepath.Join(dir, "docs", "MASTER_TASKS.md")
		if _, err := os.Stat(tasksFile); err == nil {
			successMsg += "\n" + t.getLocalizedMessage(key.chatID, "project_available_commands", nil)
		} else {
			successMsg += "\n" + t.getLocalizedMessage(key.chatID, "project_available_commands_no_tasks", nil)
		}

		t.sendMarkdown(key, successMsg)

	case "/reset":
		agent := t.getAgent(key)
		stats := agent.Stats()
		if stats.APICallCount > 0 {
			msg := t.getLocalizedMessage(key.chatID, "conversation_cleared", nil)
			msg = strings.ReplaceAll(msg, "{tokens_in}", fmt.Sprintf("%d", stats.TotalInputTokens/1000))
			msg = strings.ReplaceAll(msg, "{tokens_out}", fmt.Sprintf("%d", stats.TotalOutputTokens/1000))
			msg = strings.ReplaceAll(msg, "{calls}", fmt.Sprintf("%d", stats.APICallCount))
			t.send(key, msg)
		} else {
			t.send(key, t.getLocalizedMessage(key.chatID, "conversation_cleared", nil))
		}
		agent.Reset()

	case "/status":
		agent := t.getAgent(key)
		stats := agent.Stats()
		sessionInfo := t.getLocalizedMessage(key.chatID, "session_none", nil)
		if agent.SessionID() != "" {
			sessionInfo = fmt.Sprintf("`%s`", agent.SessionID())
		}

		// Get current model mode
		modelMode := t.getUserModelPreference(key)
		var modelDisplay string
		if modelMode == "fast" {
			modelDisplay = t.getLocalizedMessage(key.chatID, "model_fast", nil)
			modelDisplay = strings.ReplaceAll(modelDisplay, "{model}", t.config.ModelRouting.FastModel)
		} else if modelMode == "smart" {
			modelDisplay = t.getLocalizedMessage(key.chatID, "model_smart", nil)
			modelDisplay = strings.ReplaceAll(modelDisplay, "{model}", t.config.ModelRouting.SmartModel)
		} else if modelMode == "deep" {
			modelDisplay = t.getLocalizedMessage(key.chatID, "model_deep", nil)
			modelDisplay = strings.ReplaceAll(modelDisplay, "{model}", t.config.ModelRouting.DeepModel)
		} else if modelMode == "gpt-fast" {
			modelDisplay = t.getLocalizedMessage(key.chatID, "model_gpt_fast", nil)
			modelDisplay = strings.ReplaceAll(modelDisplay, "{model}", t.config.ModelRouting.CodexFastModel)
		} else if modelMode == "gpt-smart" {
			modelDisplay = t.getLocalizedMessage(key.chatID, "model_gpt_smart", nil)
			modelDisplay = strings.ReplaceAll(modelDisplay, "{model}", t.config.ModelRouting.CodexSmartModel)
		} else if modelMode == "gpt-deep" {
			modelDisplay = t.getLocalizedMessage(key.chatID, "model_gpt_deep", nil)
			modelDisplay = strings.ReplaceAll(modelDisplay, "{model}", t.config.ModelRouting.CodexDeepModel)
		} else if modelMode == "plan" {
			modelDisplay = fmt.Sprintf("`%s → %s`", t.config.ModelRouting.PlanModel, t.config.ModelRouting.ExecuteModel)
		} else if modelMode != "" {
			modelDisplay = t.getLocalizedMessage(key.chatID, "model_default", nil)
			modelDisplay = strings.ReplaceAll(modelDisplay, "{model}", modelMode)
		} else {
			modelDisplay = t.getLocalizedMessage(key.chatID, "model_auto", nil)
			modelDisplay = strings.ReplaceAll(modelDisplay, "{model}", t.client.GetModel())
		}

		status := t.getLocalizedMessage(key.chatID, "status_format", nil)
		status = strings.ReplaceAll(status, "{project}", agent.projectDir)
		status = strings.ReplaceAll(status, "{model}", modelDisplay)
		status = strings.ReplaceAll(status, "{session}", sessionInfo)
		status = strings.ReplaceAll(status, "{calls}", fmt.Sprintf("%d", stats.APICallCount))
		status = strings.ReplaceAll(status, "{tokens_in}", fmt.Sprintf("%d", stats.TotalInputTokens/1000))
		status = strings.ReplaceAll(status, "{tokens_out}", fmt.Sprintf("%d", stats.TotalOutputTokens/1000))
		t.sendMarkdown(key, status)

	case "/usage":
		agent := t.getAgent(key)
		stats := agent.Stats()

		var msg strings.Builder
		usageMsg := t.getLocalizedMessage(key.chatID, "token_usage_format", nil)
		usageMsg = strings.ReplaceAll(usageMsg, "{input}", fmt.Sprintf("%d", stats.TotalInputTokens))
		usageMsg = strings.ReplaceAll(usageMsg, "{output}", fmt.Sprintf("%d", stats.TotalOutputTokens))
		usageMsg = strings.ReplaceAll(usageMsg, "{calls}", fmt.Sprintf("%d", stats.APICallCount))
		usageMsg = strings.ReplaceAll(usageMsg, "{cost}", fmt.Sprintf("%.4f", stats.TotalCostUSD))
		msg.WriteString(usageMsg)
		msg.WriteString("\n")

		// 按模型分類顯示（從資料庫查詢最近 7 天）
		if globalStorage != nil {
			// 支持按項目篩選：/usage 或 /usage <project_path>
			var projectPath string
			if len(parts) > 1 {
				projectPath = strings.Join(parts[1:], " ")
			}

			var report CostSavingsReport
			var err error
			if projectPath != "" {
				report, err = globalStorage.GetCostSavingsByProject(projectPath, 168)
			} else {
				report, err = globalStorage.GetCostSavings(168)
			}
			if err == nil && report.TotalRequests > 0 {
				byModelMsg := t.getLocalizedMessage(key.chatID, "usage_stats_by_model", nil)
				msg.WriteString(byModelMsg)
				for model, breakdown := range report.ByModel {
					modelIcon := "🟢"
					if model == "sonnet" {
						modelIcon = "🟡"
					} else if model == "opus" {
						modelIcon = "🔴"
					} else if model == "haiku" {
						modelIcon = "⚡"
					}
					itemMsg := t.getLocalizedMessage(key.chatID, "usage_stats_model_item", nil)
					itemMsg = strings.ReplaceAll(itemMsg, "{icon}", modelIcon)
					itemMsg = strings.ReplaceAll(itemMsg, "{model}", model)
					itemMsg = strings.ReplaceAll(itemMsg, "{calls}", fmt.Sprintf("%d", breakdown.Calls))
					itemMsg = strings.ReplaceAll(itemMsg, "{input}", fmt.Sprintf("%d", breakdown.InputTokens/1000))
					itemMsg = strings.ReplaceAll(itemMsg, "{output}", fmt.Sprintf("%d", breakdown.OutputTokens/1000))
					itemMsg = strings.ReplaceAll(itemMsg, "{cost}", fmt.Sprintf("%.4f", breakdown.ActualCost))
					msg.WriteString(itemMsg)
				}
				if report.SavingsPercent != 0 {
					savingsMsg := t.getLocalizedMessage(key.chatID, "usage_stats_routing_savings", nil)
					savingsMsg = strings.ReplaceAll(savingsMsg, "{percent}", fmt.Sprintf("%.1f", report.SavingsPercent))
					savingsMsg = strings.ReplaceAll(savingsMsg, "{actual}", fmt.Sprintf("%.4f", report.ActualCost))
					savingsMsg = strings.ReplaceAll(savingsMsg, "{default}", fmt.Sprintf("%.4f", report.DefaultModelCost))
					msg.WriteString(savingsMsg)
				}
			}
		}

		modeMsg := t.getLocalizedMessage(key.chatID, "usage_stats_mode", nil)
		msg.WriteString(modeMsg)
		t.sendMarkdown(key, msg.String())

	case "/dashboard":
		t.handleDashboard(key)

	case "/checkpoints":
		if len(parts) < 2 {
			t.handleCheckpointsList(key)
		} else {
			action := parts[1]
			switch action {
			case "list":
				t.handleCheckpointsList(key)
			case "stats":
				t.handleCheckpointsStats(key)
			default:
				t.send(key, t.getLocalizedMessage(key.chatID, "checkpoints_usage", nil))
			}
		}

	case "/multiagent":
		if len(parts) < 2 {
			// Show current status
			t.handleMultiAgentStatus(key)
		} else {
			action := parts[1]
			switch action {
			case "enable":
				globalAgentCoordinator.SetEnabled(true)
				t.send(key, t.getLocalizedMessage(key.chatID, "multiagent_enabled", nil))
			case "disable":
				globalAgentCoordinator.SetEnabled(false)
				t.send(key, t.getLocalizedMessage(key.chatID, "multiagent_disabled", nil))
			case "status":
				t.handleMultiAgentStatus(key)
			case "stats":
				t.handleMultiAgentStats(key)
			default:
				t.send(key, t.getLocalizedMessage(key.chatID, "multiagent_usage", nil))
			}
		}

	case "/abort":
		switch t.abortActiveTask(key, 0) {
		case abortTaskAborted:
			t.send(key, t.getLocalizedMessage(key.chatID, "task_aborted", nil))
		case abortTaskFinished:
			t.send(key, t.getLocalizedMessage(key.chatID, "task_finished", nil))
		default:
			t.send(key, t.getLocalizedMessage(key.chatID, "no_running_task", nil))
		}

	case "/agents":
		t.handleAgentsList(key)

	case "/tasks":
		t.handleTasks(key)

	case "/fast":
		if !t.config.ModelRouting.EnableDynamicRouting {
			t.send(key, t.getLocalizedMessage(key.chatID, "routing_disabled", nil))
			return
		}
		agent := t.getAgent(key)
		hasSession := agent.LastBackend() == BackendClaude && agent.SessionIDForModel(t.config.ModelRouting.FastModel) != ""
		t.setUserModelPreference(key, "fast")
		agent.SetPlanMode(false, "", "") // Disable plan mode
		msg := t.getLocalizedMessage(key.chatID, "mode_switched_fast", map[string]string{"model": t.config.ModelRouting.FastModel})
		if hasSession {
			msg += "\n\n" + t.getLocalizedMessage(key.chatID, "model_switch_context_reset", nil)
		}
		t.send(key, msg)

	case "/smart":
		if !t.config.ModelRouting.EnableDynamicRouting {
			t.send(key, t.getLocalizedMessage(key.chatID, "routing_disabled", nil))
			return
		}
		agent := t.getAgent(key)
		hasSession := agent.LastBackend() == BackendClaude && agent.SessionIDForModel(t.config.ModelRouting.SmartModel) != ""
		t.setUserModelPreference(key, "smart")
		agent.SetPlanMode(false, "", "") // Disable plan mode
		msg := t.getLocalizedMessage(key.chatID, "mode_switched_smart", map[string]string{"model": t.config.ModelRouting.SmartModel})
		if hasSession {
			msg += "\n\n" + t.getLocalizedMessage(key.chatID, "model_switch_context_reset", nil)
		}
		t.send(key, msg)

	case "/deep":
		if !t.config.ModelRouting.EnableDynamicRouting {
			t.send(key, t.getLocalizedMessage(key.chatID, "routing_disabled", nil))
			return
		}
		agent := t.getAgent(key)
		hasSession := agent.LastBackend() == BackendClaude && agent.SessionIDForModel(t.config.ModelRouting.DeepModel) != ""
		t.setUserModelPreference(key, "deep")
		agent.SetPlanMode(false, "", "") // Disable plan mode
		msg := t.getLocalizedMessage(key.chatID, "mode_switched_deep", map[string]string{"model": t.config.ModelRouting.DeepModel})
		if hasSession {
			msg += "\n\n" + t.getLocalizedMessage(key.chatID, "model_switch_context_reset", nil)
		}
		t.send(key, msg)

	case "/gfast":
		if !t.config.ModelRouting.EnableDynamicRouting {
			t.send(key, t.getLocalizedMessage(key.chatID, "routing_disabled", nil))
			return
		}
		if !t.codexTierAvailable(key) {
			return
		}
		agent := t.getAgent(key)
		t.setUserModelPreference(key, "gpt-fast")
		agent.SetPlanMode(false, "", "") // Disable plan mode
		msg := t.getLocalizedMessage(key.chatID, "mode_switched_gpt_fast", map[string]string{"model": t.config.ModelRouting.CodexFastModel})
		t.send(key, msg)

	case "/gsmart":
		if !t.config.ModelRouting.EnableDynamicRouting {
			t.send(key, t.getLocalizedMessage(key.chatID, "routing_disabled", nil))
			return
		}
		if !t.codexTierAvailable(key) {
			return
		}
		agent := t.getAgent(key)
		t.setUserModelPreference(key, "gpt-smart")
		agent.SetPlanMode(false, "", "") // Disable plan mode
		msg := t.getLocalizedMessage(key.chatID, "mode_switched_gpt_smart", map[string]string{"model": t.config.ModelRouting.CodexSmartModel})
		t.send(key, msg)

	case "/gdeep":
		if !t.config.ModelRouting.EnableDynamicRouting {
			t.send(key, t.getLocalizedMessage(key.chatID, "routing_disabled", nil))
			return
		}
		if !t.codexTierAvailable(key) {
			return
		}
		agent := t.getAgent(key)
		t.setUserModelPreference(key, "gpt-deep")
		agent.SetPlanMode(false, "", "") // Disable plan mode
		msg := t.getLocalizedMessage(key.chatID, "mode_switched_gpt_deep", map[string]string{"model": t.config.ModelRouting.CodexDeepModel})
		t.send(key, msg)

	case "/auto":
		// Also disable Hermes mode if active
		t.hermesMu.Lock()
		if hc := t.hermesCoords[key]; hc != nil {
			hc.enabled = false
		}
		t.hermesMu.Unlock()

		if !t.config.ModelRouting.EnableDynamicRouting {
			t.send(key, t.getLocalizedMessage(key.chatID, "routing_disabled", nil))
			return
		}
		t.setUserModelPreference(key, "")
		t.getAgent(key).SetPlanMode(false, "", "") // Disable plan mode
		t.send(key, t.getLocalizedMessage(key.chatID, "mode_switched_auto", nil))

	case "/clear":
		agent := t.getAgent(key)
		agent.ClearSession()
		t.send(key, t.getLocalizedMessage(key.chatID, "session_cleared", nil))

	case "/plan":
		if !t.config.ModelRouting.EnableDynamicRouting {
			t.send(key, t.getLocalizedMessage(key.chatID, "routing_disabled", nil))
			return
		}
		t.setUserModelPreference(key, "plan")
		agent := t.getAgent(key)
		agent.SetPlanMode(true, t.config.ModelRouting.PlanModel, t.config.ModelRouting.ExecuteModel)
		msg := t.getLocalizedMessage(key.chatID, "mode_switched_plan", map[string]string{
			"plan_model":    t.config.ModelRouting.PlanModel,
			"execute_model": t.config.ModelRouting.ExecuteModel,
		})
		t.send(key, msg)

	case "/savings":
		var projectPath string
		if len(parts) > 1 {
			projectPath = strings.Join(parts[1:], " ")
		}
		t.handleSavingsCommand(key, projectPath)

	case "/send-file":
		// 提取檔案路徑：/send-file <path>
		filePath := ""
		if len(parts) > 1 {
			filePath = strings.Join(parts[1:], " ")
		}
		t.handleSendFile(key, filePath)

	case "/test-photo":
		// 測試命令：直接從本地發送照片
		t.testPhotoUpload(key)

	case "/lang":
		t.handleLangCommand(key, text)

	case "/id":
		msg := t.getLocalizedMessage(key.chatID, "id_info", map[string]string{
			"{chat_id}":   strconv.FormatInt(key.chatID, 10),
			"{thread_id}": strconv.Itoa(key.threadID),
		})
		t.send(key, msg)

	case "/preview":
		// 解析 URL 參數
		if len(parts) < 2 {
			t.send(key, "❌ 使用方式：/preview <URL>\n例：/preview https://example.com\n或：/preview http://localhost:3939")
			return
		}

		targetURL := parts[1]
		t.handlePreviewCommand(key, targetURL)

	case "/strict":
		t.handleStrictCommand(key, parts)

	case "/cron":
		t.handleCronCommand(key, parts, text)

	case "/model":
		t.handleModelCommand(key, parts)

	case "/backend":
		t.handleBackendCommand(key, parts)

	case "/parallel":
		if len(parts) < 2 {
			t.send(key, t.getLocalizedMessage(key.chatID, "parallel_usage", nil))
			return
		}
		taskText := strings.TrimSpace(text[len("/parallel"):])
		t.handleParallelCommand(key, taskText)

	case "/skills":
		t.handleSkillsCommand(key)

	case "/skill":
		if len(parts) >= 3 && parts[1] == "delete" {
			t.handleSkillDeleteCommand(key, parts[2])
		} else {
			t.send(key, t.getLocalizedMessage(key.chatID, "skill_usage", nil))
		}

	case "/hermes":
		t.handleHermesCommand(key, parts, "")

	case "/ghermes":
		if !t.codexTierAvailable(key) {
			return
		}
		t.handleHermesCommand(key, parts, "codex")

	case "/hermes-stats":
		t.handleHermesStatsCommand(key, parts)

	case "/retry":
		t.handleRetryCommand(key, parts)

	default:
		t.send(key, t.getLocalizedMessage(key.chatID, "unknown_command", nil))
	}
}

func (t *TelegramBot) sendMenu(key chatKey) {
	text := "📋 Alice 操作選單\n\n選擇下一步要做的事："
	keyboard := map[string]interface{}{
		"inline_keyboard": [][]map[string]interface{}{
			{
				{"text": "狀態", "callback_data": "menu:status"},
				{"text": "Dashboard", "callback_data": "refresh_dashboard"},
			},
			{
				{"text": "Tasks", "callback_data": "menu:tasks"},
				{"text": "Hermes", "callback_data": "menu:hermes_status"},
			},
			{
				{"text": "Retry", "callback_data": "retry:menu"},
				{"text": "Model", "callback_data": "model:menu"},
			},
			{
				{"text": "Checkpoints", "callback_data": "show_checkpoints"},
				{"text": "Usage", "callback_data": "menu:usage"},
			},
			{
				{"text": "Help", "callback_data": "menu:help"},
				{"text": "Abort", "callback_data": "menu:abort_confirm"},
			},
		},
	}
	params := map[string]interface{}{
		"chat_id":      strconv.FormatInt(key.chatID, 10),
		"text":         sanitizeUTF8(text),
		"reply_markup": keyboard,
	}
	if key.threadID != 0 {
		params["message_thread_id"] = strconv.Itoa(key.threadID)
	}
	t.queueMessage("sendMessage", params)
}

func (t *TelegramBot) sendMenuMessage(key chatKey, text string, rows [][]map[string]interface{}) {
	params := map[string]interface{}{
		"chat_id": strconv.FormatInt(key.chatID, 10),
		"text":    sanitizeUTF8(text),
		"reply_markup": map[string]interface{}{
			"inline_keyboard": rows,
		},
	}
	if key.threadID != 0 {
		params["message_thread_id"] = strconv.Itoa(key.threadID)
	}
	t.queueMessage("sendMessage", params)
}

// handleHermesCommand enables or queries Hermes mode for this chat.
// tier: "" or "claude" → Claude tier (/hermes); "codex" → GPT tier (/ghermes).
//
//	/hermes          — enable Hermes mode (next message will trigger Brain-Executor)
//	/hermes issues   — list open GitHub Issues sorted by priority
//	/hermes status   — show current coordinator state
//	/hermes stop     — cancel current task and disable Hermes mode
//	/ghermes         — same as above but on the GPT/Codex tier
//
// findIssueRefInArgs scans command args for the first token shaped like #N and
// returns its parsed number plus the original token. Allows /ghermes 處理 #109
// to be treated like /ghermes #109.
func findIssueRefInArgs(args []string) (int, string, bool) {
	for _, a := range args {
		if !strings.HasPrefix(a, "#") {
			continue
		}
		num := 0
		if _, err := fmt.Sscanf(strings.TrimPrefix(a, "#"), "%d", &num); err != nil {
			return 0, a, true
		}
		return num, a, true
	}
	return 0, "", false
}

func (t *TelegramBot) handleHermesCommand(key chatKey, parts []string, tier string) {
	if !t.config.Hermes.Enabled {
		t.send(key, "Hermes 模式未啟用。請在 config.json 中設定 hermes.enabled = true。")
		return
	}

	sub := ""
	if len(parts) > 1 {
		sub = strings.ToLower(parts[1])
	}

	// /hermes #N — fetch GitHub Issue and start task immediately.
	// Also accepts /hermes <verb> #N (e.g. /ghermes 處理 #109) by scanning all args
	// for the first #N token.
	if issueNum, raw, ok := findIssueRefInArgs(parts[1:]); ok {
		if issueNum <= 0 {
			projectDir := t.getAgent(key).ProjectDir()
			t.sendHermesIssueResolution(key, raw, projectDir, tier)
			return
		}
		t.send(key, fmt.Sprintf("🔍 正在讀取 GitHub Issue #%d…", issueNum))
		projectDir := t.getAgent(key).ProjectDir()
		t.setHermesTier(key, tier)
		go t.runTrackedJob("hermes.issue", func() {
			t.startHermesFromIssue(key, issueNum, projectDir)
		})
		return
	}

	switch sub {
	case "issues", "list":
		projectDir := t.getAgent(key).ProjectDir()
		go func() {
			done := globalJobTracker.Start("hermes.issues")
			var jobErr error
			defer func() { done(jobErr) }()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			items, err := hermes.ListIssues(ctx, projectDir, 15)
			if err != nil {
				jobErr = err
				t.send(key, fmt.Sprintf("❌ 無法取得 Issues：%v", err))
				return
			}
			t.send(key, hermes.FormatIssueList(items))
		}()

	case "status":
		t.sendHermesStatus(key, t.getAgent(key).ProjectDir())

	case "continue", "resume", "replan":
		projectDir := t.getAgent(key).ProjectDir()
		selector := ""
		if len(parts) > 2 {
			selector = parts[2]
		}
		task, ok, ambiguous := t.resolveHermesContinuationTaskBySelector(key, projectDir, selector)
		if ambiguous {
			t.send(key, fmt.Sprintf("⚠️ task id `%s` 對應到多個 Hermes 任務，請輸入更完整的 id。", selector))
			return
		}
		if !ok {
			if selector != "" {
				t.send(key, fmt.Sprintf("ℹ️ 找不到可接續的 Hermes 任務 `%s`。可以用 `/hermes status` 查看候選。", selector))
			} else {
				t.send(key, "ℹ️ 找不到可接續的 Hermes 任務。可以用 `/hermes restart <任務說明>` 開始新的任務。")
			}
			return
		}
		mode := "continue"
		if sub == "replan" {
			mode = "replan"
		}
		t.setHermesTier(key, tier)
		t.send(key, fmt.Sprintf("🔁 將根據任務 %s 的既有進度%s…", shortHermesTaskID(task.ID), hermesContinuationVerb(mode)))
		go t.runTrackedJob("hermes.continue", func() {
			t.startHermesContinuationTask(key, task, projectDir, mode)
		})

	case "restart":
		t.setHermesTier(key, tier)
		goal := strings.TrimSpace(strings.Join(parts[2:], " "))
		if goal == "" {
			t.send(key, "請提供要重新開始的任務說明，例如：`/hermes restart 修復登入流程`")
			return
		}
		projectDir := t.getAgent(key).ProjectDir()
		t.send(key, "🔄 已忽略既有 Hermes 進度，準備從頭開始。")
		go t.runTrackedJob("hermes.restart", func() {
			t.startHermesFreshTask(key, goal, projectDir)
		})

	case "stop":
		t.hermesMu.Lock()
		hc := t.hermesCoords[key]
		if hc != nil {
			hc.enabled = false
		}
		t.hermesMu.Unlock()
		t.send(key, "Hermes 模式已停用，切回一般模式。")

	default:
		if len(parts) > 1 {
			goal := strings.TrimSpace(strings.Join(parts[1:], " "))
			projectDir := t.getAgent(key).ProjectDir()
			t.setHermesTier(key, tier)
			go t.runTrackedJob("hermes.command", func() {
				t.startHermesGoalOrContinuation(key, goal, projectDir)
			})
			return
		}
		t.setHermesTier(key, tier)
		t.hermesMu.Lock()
		if hc := t.hermesCoords[key]; hc != nil {
			hc.enabled = true
		} else {
			t.hermesCoords[key] = &hermesCoord{enabled: true, tier: tier}
		}
		t.hermesMu.Unlock()
		if tier == "codex" {
			t.send(key, "✅ Hermes 模式已啟用（GPT tier）。\n\n正在載入待處理 Issues…")
		} else {
			t.send(key, "✅ Hermes 模式已啟用。\n\n正在載入待處理 Issues…")
		}
		projectDir := t.getAgent(key).ProjectDir()
		go func() {
			done := globalJobTracker.Start("hermes.issues")
			var jobErr error
			defer func() { done(jobErr) }()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			items, err := hermes.ListIssues(ctx, projectDir, 10)
			if err != nil {
				jobErr = err
				t.send(key, "（無法取得 Issues 清單，請直接輸入任務說明或使用 /hermes #<number>）")
				return
			}
			t.send(key, hermes.FormatIssueList(items))
		}()
	}
}

// handleHermesStatsCommand queries and reports Hermes task statistics.
//
//	/hermes-stats       — show most recent task summary
//	/hermes-stats week  — show aggregated stats for past 7 days
//	/hermes-stats chat  — show aggregated stats for this chat
func (t *TelegramBot) handleHermesStatsCommand(key chatKey, parts []string) {
	if !t.config.Hermes.Enabled {
		t.send(key, "Hermes 模式未啟用。")
		return
	}

	if len(parts) > 1 && strings.EqualFold(parts[1], "week") {
		if globalStorage == nil {
			t.send(key, "❌ 無法產生週報：storage 尚未初始化。")
			return
		}

		windowEnd := time.Now().UTC()
		windowStart := windowEnd.Add(-7 * 24 * time.Hour)
		lang := t.getChatLanguage(key.chatID)
		report, err := globalStorage.GetPlannerRulesWeeklyReport(windowStart, windowEnd, t.i18n, lang)
		if err != nil {
			t.send(key, fmt.Sprintf("❌ 無法產生 review 週報：%v", err))
			return
		}
		t.send(key, FormatPlannerRulesWeeklyReport(t.i18n, lang, report))
		return
	}

	t.send(key, "📊 使用方式：/hermes-stats week")
}

// isHermesEnabled reports whether Hermes mode is active for this chat.
func (t *TelegramBot) isHermesEnabled(key chatKey) bool {
	if !t.config.Hermes.Enabled {
		return false
	}
	t.hermesMu.RLock()
	hc := t.hermesCoords[key]
	t.hermesMu.RUnlock()
	return hc != nil && hc.enabled
}

// trySignalBudgetContinue checks whether the coordinator for this chat is paused
// on a budget warning. If the message starts with "繼續" or "continue" (case-
// insensitive), it signals the channel and returns true. Otherwise returns false.
func (t *TelegramBot) trySignalBudgetContinue(key chatKey, text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	isContinue := isHermesContinuationRequest(lower)
	if !isContinue {
		return false
	}

	t.hermesMu.RLock()
	hc := t.hermesCoords[key]
	t.hermesMu.RUnlock()

	if hc == nil || hc.continueCh == nil || hc.coord == nil || !hc.coord.IsRunning() {
		return false
	}

	select {
	case hc.continueCh <- struct{}{}:
		t.send(key, "▶️ 繼續執行中…")
		return true
	default:
		// Channel already full or already consumed — not waiting.
		return false
	}
}

func (t *TelegramBot) sendHermesIssueResolution(key chatKey, rawQuery, projectDir, tier string) {
	query := strings.TrimSpace(rawQuery)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	candidates, err := t.resolveTargets(ctx, ResolveTargetRequest{
		ChatID:     key.chatID,
		ThreadID:   key.threadID,
		ProjectDir: projectDir,
		Intent:     "hermes_issue",
		Query:      query,
		Kinds:      []TargetKind{TargetGitHubIssue},
		Limit:      3,
	})
	if err != nil {
		t.send(key, fmt.Sprintf("❌ 無法搜尋 GitHub Issues：%v", err))
		return
	}
	if len(candidates) == 0 {
		t.send(key, fmt.Sprintf("找不到符合 `%s` 的 Issue。請改用 `/hermes #<number>` 或 `/hermes issues`。", query))
		return
	}
	rows := make([][]map[string]interface{}, 0, len(candidates)+1)
	for _, candidate := range candidates {
		label := fmt.Sprintf("#%s · %.0f%% · %s", candidate.ID, candidate.Score*100, truncateForTelegram(candidate.Title, 34))
		rows = append(rows, []map[string]interface{}{
			{"text": label, "callback_data": "hermes:issue:" + candidate.ID + ":" + tier},
		})
	}
	rows = append(rows, []map[string]interface{}{{"text": t.getLocalizedMessage(key.chatID, "menu_btn_cancel", nil), "callback_data": "hermes:cancel"}})
	t.sendMenuMessage(key, fmt.Sprintf("我找到 %d 個可能的 issue：", len(candidates)), rows)
}

// startHermesFromIssue fetches a GitHub Issue and starts a Hermes task from it.
func (t *TelegramBot) startHermesFromIssue(key chatKey, issueNumber int, projectDir string) {
	ctx := context.Background()
	issue, err := hermesFetchIssue(ctx, projectDir, issueNumber)
	if err != nil {
		t.send(key, fmt.Sprintf("❌ 無法讀取 Issue #%d：%v", issueNumber, err))
		return
	}

	cfg := HermesDefaults(t.config.Hermes)
	ghCfg := t.config.Hermes.GithubIntegration

	// Apply complexity label budget overrides if present
	budget := HermesBudgetConfig{
		MaxTotalTokens:      cfg.Budget.MaxTotalTokens,
		MaxWallclockSeconds: cfg.Budget.MaxWallclockSeconds,
	}
	for _, label := range issue.Labels {
		if override, ok := ghCfg.ComplexityBudgetMap[label]; ok {
			budget = override
			log.Printf("[hermes] applying budget from label %q: %+v", label, override)
			break
		}
	}

	goal := hermes.BuildGoalFromIssue(issue)
	if task, decision, ok := t.resolveHermesIssueTask(key, projectDir, issueNumber); ok {
		switch decision {
		case hermesSimilarContinue:
			t.send(key, fmt.Sprintf("🔁 Issue #%d 已有 Hermes 任務 %s，將根據既有進度接續剩餘工作…", issueNumber, shortHermesTaskID(task.ID)))
			t.startHermesContinuationTask(key, task, projectDir, "continue")
			return
		case hermesSimilarCompleted:
			t.send(key, fmt.Sprintf("ℹ️ Issue #%d 在 24 小時內已有完成的 Hermes 任務 %s，為避免重複執行已先停止。\n若要重新開始，請使用 `/hermes restart %s`。", issueNumber, shortHermesTaskID(task.ID), goal))
			return
		}
	}
	t.send(key, fmt.Sprintf("🤖 **Hermes 啟動** — Issue #%d: %s\n%d 個待辦項目", issueNumber, issue.Title, func() int {
		n := 0
		for _, item := range issue.Checklist {
			if !item.Checked {
				n++
			}
		}
		return n
	}()))

	t.startHermesTaskWithIssue(key, goal, projectDir, issueNumber, budget, ghCfg)
}

// startHermesTask launches a Hermes coordinator for the given goal on the chat's current tier.
// Uses context.Background() so the task survives handler cancellation.
func (t *TelegramBot) startHermesTask(key chatKey, goal, projectDir string) {
	t.startHermesGoalOrContinuation(key, goal, projectDir)
}

func (t *TelegramBot) startHermesGoalOrContinuation(key chatKey, goal, projectDir string) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return
	}
	if task, decision, ok := t.resolveSimilarHermesTask(key, projectDir, goal); ok {
		switch decision {
		case hermesSimilarContinue:
			t.send(key, fmt.Sprintf("🔁 找到相似 Hermes 任務 %s，將根據既有進度接續剩餘工作…", shortHermesTaskID(task.ID)))
			t.startHermesContinuationTask(key, task, projectDir, "continue")
			return
		case hermesSimilarCompleted:
			t.send(key, fmt.Sprintf("ℹ️ 找到 24 小時內已完成的相似 Hermes 任務 %s，為避免重複執行已先停止。\n若要重新開始，請使用 `/hermes restart %s`。", shortHermesTaskID(task.ID), goal))
			return
		case hermesSimilarAmbiguous:
			t.sendHermesCandidateActions(key, fmt.Sprintf("⚠️ 找到可能相關的 Hermes 任務 %s，為避免接錯任務，這次未自動執行。\n可以直接按下方按鈕接續或重新規劃；若要開新任務，請使用 `/hermes restart %s`。", shortHermesTaskID(task.ID), goal), task)
			return
		}
	}
	t.startHermesTaskWithIssueTier(key, t.buildHermesGoalWithContext(key, goal), projectDir, 0, HermesBudgetConfig{}, GithubIntegrationConfig{}, t.hermesTierFor(key))
}

func (t *TelegramBot) startHermesFreshTask(key chatKey, goal, projectDir string) {
	t.startHermesTaskWithIssueTier(key, strings.TrimSpace(goal), projectDir, 0, HermesBudgetConfig{}, GithubIntegrationConfig{}, t.hermesTierFor(key))
}

func (t *TelegramBot) startHermesContinuationTask(key chatKey, task hermes.TaskState, projectDir, mode string) {
	goal := buildHermesContinuationGoal(task, mode)
	t.startHermesTaskWithIssueTier(key, goal, hermesContinuationProjectDir(task, projectDir), task.GithubIssueNumber, HermesBudgetConfig{}, t.config.Hermes.GithubIntegration, t.hermesTierFor(key))
}

// startHermesTaskWithIssue preserves the original signature for callers that don't
// care about tier — defaults to whatever tier the chat's hermesCoord is on.
func (t *TelegramBot) startHermesTaskWithIssue(key chatKey, goal, projectDir string, issueNumber int, budgetOverride HermesBudgetConfig, ghIntegration GithubIntegrationConfig) {
	t.startHermesTaskWithIssueTier(key, t.buildHermesGoalWithContext(key, goal), projectDir, issueNumber, budgetOverride, ghIntegration, t.hermesTierFor(key))
}

func (t *TelegramBot) buildHermesGoalWithContext(key chatKey, currentRequest string) string {
	currentRequest = strings.TrimSpace(currentRequest)
	if currentRequest == "" {
		return currentRequest
	}

	taskHistory := t.loadHermesContextTasks(key.chatID, currentRequest)
	var recentMessages []contextMessage
	recentMessages = t.getChatContext(key, "").RecentMessagesSnapshot()
	return composeHermesGoalWithContext(currentRequest, taskHistory, recentMessages)
}

func (t *TelegramBot) resolveHermesContinuationTask(key chatKey, projectDir string) (hermes.TaskState, bool) {
	task, ok, _ := t.resolveHermesContinuationTaskBySelector(key, projectDir, "")
	return task, ok
}

func (t *TelegramBot) resolveHermesContinuationTaskBySelector(key chatKey, projectDir, selector string) (hermes.TaskState, bool, bool) {
	tasks, err := t.taskSvc.ListForChat(key.chatID, 10)
	if err != nil {
		log.Printf("[hermes] failed to resolve continuation task for chat %d: %v", key.chatID, err)
		return hermes.TaskState{}, false, false
	}
	if strings.TrimSpace(selector) != "" {
		task, ok, ambiguous := selectHermesContinuationTaskByIDForSelectableScope(tasks, key.threadID, projectDir, selector)
		return task, ok, ambiguous
	}
	candidates := resolveTargetCandidates(ResolveTargetRequest{
		ChatID:     key.chatID,
		ThreadID:   key.threadID,
		ProjectDir: projectDir,
		Intent:     "hermes_continue",
		Kinds:      []TargetKind{TargetHermesTask},
		Limit:      1,
	}, targetResolverSources{Tasks: tasks, Now: time.Now()})
	if len(candidates) == 0 {
		return hermes.TaskState{}, false, false
	}
	task, err := t.taskSvc.GetTask(candidates[0].ID)
	if err != nil {
		log.Printf("[hermes] failed to load resolved continuation task %s: %v", candidates[0].ID, err)
		return hermes.TaskState{}, false, false
	}
	return task, true, false
}

func selectHermesContinuationTask(tasks []hermes.TaskState, projectDir string) (hermes.TaskState, bool) {
	return selectHermesContinuationTaskForScope(tasks, 0, projectDir)
}

func selectHermesContinuationTaskForScope(tasks []hermes.TaskState, threadID int, projectDir string) (hermes.TaskState, bool) {
	candidates := selectHermesContinuationTasksForScope(tasks, threadID, projectDir, 1)
	if len(candidates) == 0 {
		return hermes.TaskState{}, false
	}
	return candidates[0], true
}

func selectHermesContinuationTaskByID(tasks []hermes.TaskState, projectDir, selector string) (hermes.TaskState, bool, bool) {
	return selectHermesContinuationTaskByIDForScope(tasks, 0, projectDir, selector)
}

func selectHermesContinuationTaskByIDForScope(tasks []hermes.TaskState, threadID int, projectDir, selector string) (hermes.TaskState, bool, bool) {
	return selectHermesContinuationTaskByIDWithMatcher(tasks, threadID, projectDir, selector, hermesTaskMatchesScope)
}

func selectHermesContinuationTaskByIDForSelectableScope(tasks []hermes.TaskState, threadID int, projectDir, selector string) (hermes.TaskState, bool, bool) {
	return selectHermesContinuationTaskByIDWithMatcher(tasks, threadID, projectDir, selector, hermesTaskMatchesSelectableScope)
}

func selectHermesContinuationTaskByIDWithMatcher(tasks []hermes.TaskState, threadID int, projectDir, selector string, matches func(hermes.TaskState, int, string) bool) (hermes.TaskState, bool, bool) {
	selector = strings.ToLower(strings.TrimSpace(selector))
	if selector == "" {
		return hermes.TaskState{}, false, false
	}
	var matched hermes.TaskState
	matchCount := 0
	for _, task := range tasks {
		id := strings.ToLower(strings.TrimSpace(task.ID))
		if id == "" || !strings.HasPrefix(id, selector) {
			continue
		}
		if !matches(task, threadID, projectDir) || !hermesTaskIsContinuable(task) {
			continue
		}
		matched = task
		matchCount++
	}
	if matchCount == 0 {
		return hermes.TaskState{}, false, false
	}
	if matchCount > 1 {
		return hermes.TaskState{}, false, true
	}
	return matched, true, false
}

func selectHermesContinuationTasks(tasks []hermes.TaskState, projectDir string, limit int) []hermes.TaskState {
	return selectHermesContinuationTasksForScope(tasks, 0, projectDir, limit)
}

func selectHermesContinuationTasksForScope(tasks []hermes.TaskState, threadID int, projectDir string, limit int) []hermes.TaskState {
	if limit <= 0 {
		limit = 3
	}
	projectDir = strings.TrimSpace(projectDir)
	type rankedTask struct {
		task hermes.TaskState
		rank int
	}
	var ranked []rankedTask
	for _, task := range tasks {
		if !hermesTaskMatchesScope(task, threadID, projectDir) {
			continue
		}
		rank := hermesContinuationRank(task)
		if rank < 0 {
			continue
		}
		ranked = append(ranked, rankedTask{task: task, rank: rank})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].rank != ranked[j].rank {
			return ranked[i].rank < ranked[j].rank
		}
		return ranked[i].task.UpdatedAt.After(ranked[j].task.UpdatedAt)
	})
	out := make([]hermes.TaskState, 0, min(limit, len(ranked)))
	for i := 0; i < len(ranked) && i < limit; i++ {
		out = append(out, ranked[i].task)
	}
	return out
}

func selectHermesLegacyContinuationTasksForScope(tasks []hermes.TaskState, threadID int, projectDir string, limit int) []hermes.TaskState {
	if threadID == 0 {
		return nil
	}
	var legacy []hermes.TaskState
	for _, task := range tasks {
		if task.ThreadID != 0 {
			continue
		}
		legacy = append(legacy, task)
	}
	return selectHermesContinuationTasksForScope(legacy, 0, projectDir, limit)
}

type hermesSimilarDecision int

const (
	hermesSimilarNone hermesSimilarDecision = iota
	hermesSimilarContinue
	hermesSimilarCompleted
	hermesSimilarAmbiguous
)

func (t *TelegramBot) resolveSimilarHermesTask(key chatKey, projectDir, goal string) (hermes.TaskState, hermesSimilarDecision, bool) {
	tasks, err := t.taskSvc.ListForChat(key.chatID, 10)
	if err != nil {
		log.Printf("[hermes] failed to resolve similar task for chat %d: %v", key.chatID, err)
		return hermes.TaskState{}, hermesSimilarNone, false
	}
	task, decision, ok := selectSimilarHermesTaskForScope(tasks, key.threadID, projectDir, goal, time.Now())
	return task, decision, ok
}

func (t *TelegramBot) resolveHermesIssueTask(key chatKey, projectDir string, issueNumber int) (hermes.TaskState, hermesSimilarDecision, bool) {
	if issueNumber <= 0 {
		return hermes.TaskState{}, hermesSimilarNone, false
	}
	tasks, err := t.taskSvc.ListForChat(key.chatID, 10)
	if err != nil {
		log.Printf("[hermes] failed to resolve issue task for chat %d issue #%d: %v", key.chatID, issueNumber, err)
		return hermes.TaskState{}, hermesSimilarNone, false
	}
	return selectHermesIssueTaskForScope(tasks, key.threadID, projectDir, issueNumber)
}

func selectHermesIssueTask(tasks []hermes.TaskState, projectDir string, issueNumber int) (hermes.TaskState, hermesSimilarDecision, bool) {
	return selectHermesIssueTaskForScope(tasks, 0, projectDir, issueNumber)
}

func selectHermesIssueTaskForScope(tasks []hermes.TaskState, threadID int, projectDir string, issueNumber int) (hermes.TaskState, hermesSimilarDecision, bool) {
	projectDir = strings.TrimSpace(projectDir)
	var best hermes.TaskState
	bestDecision := hermesSimilarNone
	bestRank := 99
	for _, task := range tasks {
		if task.GithubIssueNumber != issueNumber {
			continue
		}
		if !hermesTaskMatchesScope(task, threadID, projectDir) {
			continue
		}
		decision := hermesSimilarContinue
		rank := hermesContinuationRank(task)
		if task.Status == hermes.TaskStatusDone && time.Since(task.UpdatedAt) <= 24*time.Hour {
			decision = hermesSimilarCompleted
			rank = 4
		} else if rank < 0 {
			continue
		}
		if bestDecision == hermesSimilarNone || rank < bestRank || (rank == bestRank && task.UpdatedAt.After(best.UpdatedAt)) {
			best = task
			bestDecision = decision
			bestRank = rank
		}
	}
	if bestDecision == hermesSimilarNone {
		return hermes.TaskState{}, hermesSimilarNone, false
	}
	return best, bestDecision, true
}

func selectSimilarHermesTask(tasks []hermes.TaskState, projectDir, goal string, now time.Time) (hermes.TaskState, hermesSimilarDecision, bool) {
	return selectSimilarHermesTaskForScope(tasks, 0, projectDir, goal, now)
}

func selectSimilarHermesTaskForScope(tasks []hermes.TaskState, threadID int, projectDir, goal string, now time.Time) (hermes.TaskState, hermesSimilarDecision, bool) {
	projectDir = strings.TrimSpace(projectDir)
	goal = strings.TrimSpace(extractHermesActionableGoal(goal))
	if goal == "" {
		return hermes.TaskState{}, hermesSimilarNone, false
	}

	var best hermes.TaskState
	bestScore := 0.0
	bestDecision := hermesSimilarNone
	for _, task := range tasks {
		if !hermesTaskMatchesScope(task, threadID, projectDir) {
			continue
		}
		taskGoal := strings.TrimSpace(extractHermesActionableGoal(task.Goal))
		if taskGoal == "" {
			continue
		}
		score := hermesGoalSimilarity(goal, taskGoal)
		if score < 0.45 {
			continue
		}
		decision := hermesSimilarContinue
		if task.Status == hermes.TaskStatusDone && now.Sub(task.UpdatedAt) <= 24*time.Hour {
			decision = hermesSimilarCompleted
		} else if hermesContinuationRank(task) < 0 {
			continue
		}
		if score < 0.75 {
			decision = hermesSimilarAmbiguous
		}
		if score > bestScore || (score == bestScore && task.UpdatedAt.After(best.UpdatedAt)) {
			best = task
			bestScore = score
			bestDecision = decision
		}
	}
	if bestDecision == hermesSimilarNone {
		return hermes.TaskState{}, hermesSimilarNone, false
	}
	return best, bestDecision, true
}

func hermesContinuationProjectDir(task hermes.TaskState, fallback string) string {
	if taskProjectDir := strings.TrimSpace(task.ProjectDir); taskProjectDir != "" {
		return taskProjectDir
	}
	return fallback
}

func hermesTaskMatchesProject(task hermes.TaskState, projectDir string) bool {
	projectDir = cleanHermesProjectDir(projectDir)
	taskProjectDir := cleanHermesProjectDir(task.ProjectDir)
	if projectDir == "" {
		return taskProjectDir == ""
	}
	return taskProjectDir == projectDir
}

func hermesTaskMatchesScope(task hermes.TaskState, threadID int, projectDir string) bool {
	return task.ThreadID == threadID && hermesTaskMatchesProject(task, projectDir)
}

func hermesTaskMatchesSelectableScope(task hermes.TaskState, threadID int, projectDir string) bool {
	if hermesTaskMatchesScope(task, threadID, projectDir) {
		return true
	}
	return threadID != 0 && task.ThreadID == 0 && hermesTaskMatchesProject(task, projectDir)
}

func cleanHermesProjectDir(projectDir string) string {
	projectDir = strings.TrimSpace(projectDir)
	if projectDir == "" {
		return ""
	}
	return filepath.Clean(projectDir)
}

func hermesGoalSimilarity(a, b string) float64 {
	aNorm := strings.ToLower(normalizeHermesGoal(a))
	bNorm := strings.ToLower(normalizeHermesGoal(b))
	if aNorm == "" || bNorm == "" {
		return 0
	}
	if aNorm == bNorm {
		return 1
	}
	if strings.Contains(aNorm, bNorm) || strings.Contains(bNorm, aNorm) {
		shorter := len([]rune(aNorm))
		longer := len([]rune(bNorm))
		if shorter > longer {
			shorter, longer = longer, shorter
		}
		if longer == 0 {
			return 0
		}
		return float64(shorter) / float64(longer)
	}

	aTokens := hermesGoalTokens(aNorm)
	bTokens := hermesGoalTokens(bNorm)
	if len(aTokens) == 0 || len(bTokens) == 0 {
		return 0
	}
	intersect := 0
	for token := range aTokens {
		if _, ok := bTokens[token]; ok {
			intersect++
		}
	}
	union := len(aTokens) + len(bTokens) - intersect
	if union == 0 {
		return 0
	}
	return float64(intersect) / float64(union)
}

func hermesGoalTokens(goal string) map[string]struct{} {
	fields := strings.FieldsFunc(goal, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' ||
			r == ',' || r == '.' || r == ':' || r == ';' ||
			r == '，' || r == '。' || r == '：' || r == '；' ||
			r == '(' || r == ')' || r == '（' || r == '）'
	})
	tokens := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if len([]rune(field)) < 2 {
			continue
		}
		tokens[field] = struct{}{}
	}
	return tokens
}

func hermesContinuationRank(task hermes.TaskState) int {
	switch task.Status {
	case hermes.TaskStatusPlanning, hermes.TaskStatusExecuting, hermes.TaskStatusValidating:
		return 0
	case hermes.TaskStatusInterrupted:
		return 1
	case hermes.TaskStatusFailed:
		if hermesTaskHasProgress(task) {
			return 2
		}
		return 3
	case hermes.TaskStatusDone:
		if time.Since(task.UpdatedAt) <= 24*time.Hour && hermesTaskHasProgress(task) {
			return 4
		}
	}
	return -1
}

func hermesTaskIsContinuable(task hermes.TaskState) bool {
	return hermesContinuationRank(task) >= 0
}

func hermesTaskHasProgress(task hermes.TaskState) bool {
	if strings.TrimSpace(task.Accumulated) != "" {
		return true
	}
	for _, sub := range task.Plan {
		if sub.Status == hermes.SubTaskDone || sub.Status == hermes.SubTaskSkipped || strings.TrimSpace(sub.Result) != "" {
			return true
		}
	}
	return false
}

func isHermesContinuationRequest(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	return strings.HasPrefix(lower, "繼續") ||
		strings.HasPrefix(lower, "接續") ||
		strings.HasPrefix(lower, "續做") ||
		strings.HasPrefix(lower, "重新規劃") ||
		strings.HasPrefix(lower, "continue") ||
		strings.HasPrefix(lower, "resume") ||
		strings.HasPrefix(lower, "replan")
}

func hermesContinuationModeFromRequest(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))
	if strings.HasPrefix(lower, "重新規劃") || strings.HasPrefix(lower, "重規") || strings.HasPrefix(lower, "replan") {
		return "replan"
	}
	return "continue"
}

func isHermesIssueReferenceRequest(text string) bool {
	if _, ok := ParseIssueNumber(text); !ok {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	return isHermesContinuationRequest(lower) ||
		strings.Contains(lower, "繼續") ||
		strings.Contains(lower, "接續") ||
		strings.Contains(lower, "續做") ||
		strings.HasPrefix(lower, "處理") ||
		strings.HasPrefix(lower, "請處理") ||
		strings.HasPrefix(lower, "開始") ||
		strings.HasPrefix(lower, "start") ||
		strings.HasPrefix(lower, "work on")
}

func buildHermesContinuationGoal(task hermes.TaskState, mode string) string {
	originalGoal := strings.TrimSpace(extractHermesActionableGoal(task.Goal))
	if originalGoal == "" {
		originalGoal = strings.TrimSpace(task.Goal)
	}

	var sb strings.Builder
	sb.WriteString("[Hermes continuation]\n")
	sb.WriteString("Mode: ")
	sb.WriteString(mode)
	sb.WriteString("\nTask ID: ")
	sb.WriteString(task.ID)
	sb.WriteString("\nTask status: ")
	sb.WriteString(string(task.Status))
	if task.GithubIssueNumber > 0 {
		sb.WriteString(fmt.Sprintf("\nGitHub issue: #%d", task.GithubIssueNumber))
	}
	sb.WriteString("\n\nOriginal goal:\n")
	sb.WriteString(originalGoal)
	sb.WriteString("\n\nCurrent progress:\n")
	sb.WriteString(buildHermesProgressSummary(task))
	sb.WriteString("\n\nInstructions:\n")
	sb.WriteString("- Treat completed/skipped subtasks as already handled.\n")
	sb.WriteString("- Do not repeat completed work unless the progress summary says it is invalid or incomplete.\n")
	sb.WriteString("- Re-plan only the remaining, failed, interrupted, or unverified work.\n")
	sb.WriteString("- Preserve useful context from accumulated progress and reviewer feedback.\n")
	sb.WriteString("- Return a concrete plan for the remaining work, then execute it.\n")
	return sb.String()
}

func buildHermesProgressSummary(task hermes.TaskState) string {
	var lines []string
	if acc := strings.TrimSpace(task.Accumulated); acc != "" {
		lines = append(lines, "Accumulated summary:\n"+clampHermesContext(acc, hermesContextMaxChars))
	}
	if len(task.Plan) > 0 {
		lines = append(lines, "Subtasks:")
		for i, sub := range task.Plan {
			desc := strings.TrimSpace(sub.Description)
			if desc == "" {
				desc = "(no description)"
			}
			line := fmt.Sprintf("%d. [%s] %s", i+1, sub.Status, desc)
			if result := strings.TrimSpace(sub.Result); result != "" {
				line += "\n   Result: " + clampHermesContext(result, 600)
			}
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return "(No stored progress details.)"
	}
	return strings.Join(lines, "\n")
}

func (t *TelegramBot) formatHermesStatus(key chatKey, projectDir string) string {
	text, _ := t.hermesStatusTextAndCandidates(key, projectDir)
	return text
}

func (t *TelegramBot) hermesStatusTextAndCandidates(key chatKey, projectDir string) (string, []hermes.TaskState) {
	t.hermesMu.RLock()
	hc := t.hermesCoords[key]
	t.hermesMu.RUnlock()

	var lines []string
	if hc == nil || !hc.enabled {
		lines = append(lines, "Hermes 模式：未啟用")
	} else if hc.coord != nil && hc.coord.IsRunning() {
		lines = append(lines, fmt.Sprintf("Hermes 模式：執行中（任務 %s）", shortHermesTaskID(hc.coord.TaskID())))
	} else {
		lines = append(lines, "Hermes 模式：已啟用，等待下一則訊息")
	}

	var candidates []hermes.TaskState
	var legacyCandidates []hermes.TaskState
	if tasks, err := t.taskSvc.ListForChat(key.chatID, 10); err == nil {
		candidates = selectHermesContinuationTasksForScope(tasks, key.threadID, projectDir, 3)
		if len(candidates) == 0 {
			legacyCandidates = selectHermesLegacyContinuationTasksForScope(tasks, key.threadID, projectDir, 3)
		}
	} else {
		log.Printf("[hermes] failed to format status candidates for chat %d: %v", key.chatID, err)
	}
	if len(candidates) > 0 || len(legacyCandidates) > 0 {
		lines = append(lines, "")
		if len(candidates) > 0 {
			lines = append(lines, "可接續任務候選：")
			for _, task := range candidates {
				lines = append(lines, formatHermesTaskLine(task))
			}
		} else {
			lines = append(lines, "可接續舊版任務候選（Topic 未記錄，請確認 id 後操作）：")
			for _, task := range legacyCandidates {
				lines = append(lines, formatHermesTaskLine(task))
			}
		}
		lines = append(lines, "可用操作：直接按下方按鈕，或使用 /hermes continue <id>、/hermes replan <id>、/hermes restart <任務說明>")
	}
	if len(candidates) == 0 {
		candidates = legacyCandidates
	}
	return strings.Join(lines, "\n"), candidates
}

func (t *TelegramBot) sendHermesStatus(key chatKey, projectDir string) {
	text, candidates := t.hermesStatusTextAndCandidates(key, projectDir)
	if len(candidates) == 0 {
		t.send(key, text)
		return
	}
	t.sendHermesActionsMessage(key, text, candidates)
}

func (t *TelegramBot) sendHermesCandidateActions(key chatKey, text string, task hermes.TaskState) {
	t.sendHermesActionsMessage(key, text, []hermes.TaskState{task})
}

func (t *TelegramBot) sendHermesActionsMessage(key chatKey, text string, tasks []hermes.TaskState) {
	keyboard := map[string]interface{}{
		"inline_keyboard": hermesCandidateActionRows(tasks),
	}
	params := map[string]interface{}{
		"chat_id":      strconv.FormatInt(key.chatID, 10),
		"text":         sanitizeUTF8(text),
		"reply_markup": keyboard,
	}
	if key.threadID != 0 {
		params["message_thread_id"] = strconv.Itoa(key.threadID)
	}
	t.queueMessage("sendMessage", params)
}

func hermesCandidateActionRows(tasks []hermes.TaskState) [][]map[string]interface{} {
	rows := make([][]map[string]interface{}, 0, len(tasks)+1)
	for _, task := range tasks {
		if strings.TrimSpace(task.ID) == "" {
			continue
		}
		id := task.ID
		shortID := shortHermesTaskID(id)
		rows = append(rows, []map[string]interface{}{
			{
				"text":          "▶️ 接續 " + shortID,
				"callback_data": "hermes:continue:" + id,
			},
			{
				"text":          "🧠 重規 " + shortID,
				"callback_data": "hermes:replan:" + id,
			},
		})
	}
	rows = append(rows, []map[string]interface{}{
		{
			"text":          "取消",
			"callback_data": "hermes:cancel",
		},
	})
	return rows
}

func formatHermesTaskLine(task hermes.TaskState) string {
	done, total := hermesTaskProgressCounts(task)
	goal := clampHermesContext(extractHermesActionableGoal(task.Goal), 120)
	if goal == "" {
		goal = "(無目標摘要)"
	}
	return fmt.Sprintf("- %s [%s] %d/%d：%s", shortHermesTaskID(task.ID), task.Status, done, total, goal)
}

func hermesTaskProgressCounts(task hermes.TaskState) (done, total int) {
	total = len(task.Plan)
	for _, sub := range task.Plan {
		if sub.Status == hermes.SubTaskDone || sub.Status == hermes.SubTaskSkipped {
			done++
		}
	}
	return done, total
}

func shortHermesTaskID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func hermesContinuationVerb(mode string) string {
	if mode == "replan" {
		return "重新規劃剩餘工作"
	}
	return "接續剩餘工作"
}

func (t *TelegramBot) loadHermesContextTasks(chatID int64, currentRequest string) []hermes.TaskState {
	var tasks []hermes.TaskState
	active, err := t.taskSvc.GetActiveForChat(chatID)
	switch {
	case err == nil:
		tasks = append(tasks, active)
	case err != nil && err != hermes.ErrNoTask:
		log.Printf("[hermes] failed to load active task context for chat %d: %v", chatID, err)
	}

	history, err := t.taskSvc.ListForChat(chatID, 3)
	if err != nil {
		log.Printf("[hermes] failed to list task context for chat %d: %v", chatID, err)
		return tasks
	}

	currentNorm := normalizeHermesGoal(currentRequest)
	seen := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		seen[task.ID] = struct{}{}
	}
	for _, task := range history {
		if _, ok := seen[task.ID]; ok {
			continue
		}
		if normalizeHermesGoal(extractHermesActionableGoal(task.Goal)) == currentNorm {
			continue
		}
		tasks = append(tasks, task)
		if len(tasks) >= 2 {
			break
		}
	}

	return tasks
}

func composeHermesGoalWithContext(currentRequest string, tasks []hermes.TaskState, recentMessages []contextMessage) string {
	currentRequest = strings.TrimSpace(currentRequest)
	if currentRequest == "" {
		return ""
	}

	var sections []string
	if taskSection := buildHermesTaskContextSection(tasks); taskSection != "" {
		sections = append(sections, taskSection)
	}
	if bridge := strings.TrimSpace(buildContextBridge(recentMessages)); bridge != "" {
		sections = append(sections, clampHermesContext(bridge, hermesContextMaxChars))
	}
	if len(sections) == 0 {
		return currentRequest
	}

	var sb strings.Builder
	sb.WriteString(hermesPreviousContextHeader)
	sb.WriteString("\n")
	for i, section := range sections {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(section)
	}
	sb.WriteString("\n\n")
	sb.WriteString(hermesCurrentRequestHeader)
	sb.WriteString("\n")
	sb.WriteString(currentRequest)
	return sb.String()
}

func buildHermesTaskContextSection(tasks []hermes.TaskState) string {
	if len(tasks) == 0 {
		return ""
	}

	var sections []string
	for _, task := range tasks {
		goal := strings.TrimSpace(extractHermesActionableGoal(task.Goal))
		summary := strings.TrimSpace(buildHermesTaskSummary(task))
		if goal == "" && summary == "" {
			continue
		}

		var sb strings.Builder
		sb.WriteString("Recent Hermes task")
		if !task.UpdatedAt.IsZero() {
			sb.WriteString(" (")
			sb.WriteString(task.UpdatedAt.Format(time.RFC3339))
			sb.WriteString(")")
		}
		sb.WriteString(":\n")
		if goal != "" {
			sb.WriteString("User: ")
			sb.WriteString(clampHermesContext(goal, hermesContextMaxChars))
			sb.WriteString("\n")
		}
		if summary != "" {
			sb.WriteString("Assistant: ")
			sb.WriteString(clampHermesContext(summary, hermesContextMaxChars))
		}
		sections = append(sections, strings.TrimSpace(sb.String()))
	}

	return strings.Join(sections, "\n\n")
}

func buildHermesTaskSummary(task hermes.TaskState) string {
	if task.Accumulated != "" {
		return task.Accumulated
	}

	lines := make([]string, 0, len(task.Plan))
	for _, sub := range task.Plan {
		result := strings.TrimSpace(sub.Result)
		if result != "" {
			lines = append(lines, result)
			continue
		}
		if sub.Status == hermes.SubTaskDone {
			lines = append(lines, sub.Description)
		}
	}
	return strings.Join(lines, "\n")
}

func extractHermesActionableGoal(goal string) string {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return ""
	}

	if strings.HasPrefix(goal, "[Hermes continuation]") {
		const originalHeader = "Original goal:"
		const progressHeader = "\n\nCurrent progress:"
		if start := strings.Index(goal, originalHeader); start >= 0 {
			actionable := strings.TrimSpace(goal[start+len(originalHeader):])
			if end := strings.Index(actionable, progressHeader); end >= 0 {
				actionable = strings.TrimSpace(actionable[:end])
			}
			if actionable != "" {
				return extractHermesActionableGoal(actionable)
			}
		}
	}

	idx := strings.LastIndex(goal, hermesCurrentRequestHeader)
	if idx < 0 {
		return goal
	}

	actionable := strings.TrimSpace(goal[idx+len(hermesCurrentRequestHeader):])
	if actionable == "" {
		return goal
	}
	return actionable
}

func normalizeHermesGoal(goal string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(goal)), " ")
}

func clampHermesContext(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return strings.TrimSpace(s)
	}
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + "..."
}

// hermesTierFor returns the active Hermes tier for this chat ("" or "codex"),
// derived from the chat's hermesCoord (set by /hermes vs /ghermes).
func (t *TelegramBot) hermesTierFor(key chatKey) string {
	t.hermesMu.RLock()
	defer t.hermesMu.RUnlock()
	if hc := t.hermesCoords[key]; hc != nil {
		return hc.tier
	}
	return ""
}

// setHermesTier updates the active tier for a chat and clears cached Planner
// resume state when the backend changes.
func (t *TelegramBot) setHermesTier(key chatKey, tier string) {
	t.hermesMu.Lock()
	defer t.hermesMu.Unlock()

	hc := t.hermesCoords[key]
	if hc == nil {
		hc = &hermesCoord{enabled: true}
		t.hermesCoords[key] = hc
	}
	if hc.tier != tier {
		hc.plannerSessionID = ""
		hc.plannerSessionTier = ""
		hc.executorSessionID = ""
		hc.executorSessionTier = ""
	}
	hc.tier = tier
}

// plannerSessionForTier returns the cached Planner --resume session if it was
// produced by the currently active tier.
func (t *TelegramBot) plannerSessionForTier(key chatKey, tier string) string {
	t.hermesMu.RLock()
	defer t.hermesMu.RUnlock()

	hc := t.hermesCoords[key]
	if hc == nil || hc.plannerSessionID == "" {
		return ""
	}
	if hc.tier != tier || hc.plannerSessionTier != tier {
		return ""
	}
	return hc.plannerSessionID
}

// recordPlannerSession caches the latest Planner --resume session for the tier
// that produced it. If the user already switched tiers, the cache is left alone.
func (t *TelegramBot) recordPlannerSession(key chatKey, tier, sessionID string) {
	if sessionID == "" {
		return
	}

	t.hermesMu.Lock()
	defer t.hermesMu.Unlock()

	hc := t.hermesCoords[key]
	if hc == nil {
		hc = &hermesCoord{enabled: true}
		t.hermesCoords[key] = hc
	}
	if hc.tier != tier {
		return
	}
	hc.plannerSessionID = sessionID
	hc.plannerSessionTier = tier
}

// executorSessionForTier returns the cached Executor thread resume ID if it was
// produced by the currently active tier.
func (t *TelegramBot) executorSessionForTier(key chatKey, tier string) string {
	t.hermesMu.RLock()
	defer t.hermesMu.RUnlock()

	hc := t.hermesCoords[key]
	if hc == nil || hc.executorSessionID == "" {
		return ""
	}
	if hc.tier != tier || hc.executorSessionTier != tier {
		return ""
	}
	return hc.executorSessionID
}

// recordExecutorSession caches the latest Executor thread resume ID for the tier
// that produced it. If the user already switched tiers, the cache is left alone.
func (t *TelegramBot) recordExecutorSession(key chatKey, tier, sessionID string) {
	if sessionID == "" {
		return
	}

	t.hermesMu.Lock()
	defer t.hermesMu.Unlock()

	hc := t.hermesCoords[key]
	if hc == nil {
		hc = &hermesCoord{enabled: true}
		t.hermesCoords[key] = hc
	}
	if hc.tier != tier {
		return
	}
	hc.executorSessionID = sessionID
	hc.executorSessionTier = tier
}

// startHermesTaskWithIssueTier is the common implementation that selects models
// based on the tier ("" or "claude" → Claude; "codex" → GPT/Codex).
func (t *TelegramBot) startHermesTaskWithIssueTier(key chatKey, goal, projectDir string, issueNumber int, budgetOverride HermesBudgetConfig, ghIntegration GithubIntegrationConfig, tier string) {
	// Update tier and clear session IDs if tier changed (Issue #109)
	t.setHermesTier(key, tier)

	ctx := context.Background()
	cfg := HermesDefaults(t.config.Hermes)
	strictCfg := t.resolveStrictModeConfig(key, goal)

	var plannerModel, executorModel, heavyExecutorModel string
	if tier == "codex" {
		plannerModel = cfg.CodexPlannerModel
		if plannerModel == "" {
			plannerModel = t.config.ModelRouting.CodexDeepModel
		}
		executorModel = cfg.CodexExecutorModel
		if executorModel == "" {
			executorModel = t.config.ModelRouting.CodexFastModel
		}
		heavyExecutorModel = cfg.CodexHeavyExecutorModel
		// No SmartModel default for codex; if unset, heavy stays empty (single-tier).
	} else {
		plannerModel = cfg.PlannerModel
		if plannerModel == "" {
			plannerModel = t.config.ModelRouting.DeepModel
		}
		executorModel = cfg.ExecutorModel
		if executorModel == "" {
			executorModel = t.config.ModelRouting.FastModel
		}
		heavyExecutorModel = cfg.HeavyExecutorModel
		if heavyExecutorModel == "" {
			heavyExecutorModel = t.config.ModelRouting.SmartModel
		}
	}
	var reviewModel string
	reviewBackend := appengine.ResolveStrictReviewBackend(appengine.BackendKindForModel(executorModel), strictCfg, appengine.BackendClaude, appengine.BackendCodex)
	switch reviewBackend {
	case appengine.BackendCodex:
		reviewModel = t.config.ModelRouting.CodexDeepModel
		if reviewModel == "" {
			reviewModel = plannerModel
			log.Printf("[hermes] codex_deep_model empty; reviewer falls back to planner model %q", reviewModel)
		}
	default:
		reviewModel = t.config.ModelRouting.DeepModel
		if reviewModel == "" {
			reviewModel = plannerModel
			log.Printf("[hermes] deep_model empty; reviewer falls back to planner model %q", reviewModel)
		}
	}
	reviewPhase := NewCLIReviewPhase(t.client, reviewModel)

	planFn := makePlanFn(t.client, plannerModel)

	taskStore := hermes.TaskStateStore(t.taskSvc)

	verbosity := hermes.ParseVerbosity(cfg.ProgressVerbosity)
	reporter := hermes.NewTextProgressReporter(verbosity, func(text string) {
		t.send(key, text)
	})

	plannerSessionID := t.plannerSessionForTier(key, tier)
	executorSessionID := t.executorSessionForTier(key, tier)

	// Budget: use override (from complexity label) or config default
	budgetTokens := cfg.Budget.MaxTotalTokens
	budgetSecs := cfg.Budget.MaxWallclockSeconds
	if budgetOverride.MaxTotalTokens > 0 {
		budgetTokens = budgetOverride.MaxTotalTokens
	}
	if budgetOverride.MaxWallclockSeconds > 0 {
		budgetSecs = budgetOverride.MaxWallclockSeconds
	}
	budget := hermes.TokenBudget{
		MaxTotalTokens:      budgetTokens,
		MaxWallclockSeconds: budgetSecs,
	}

	// Load role-specific operating rules from the prompts directory.
	// Falls back to embedded defaults if the directory is missing.
	promptsDir := cfg.PromptsDir
	if promptsDir == "" {
		promptsDir = "internal/app/hermes/prompts"
	}
	pb := hermes.LoadPromptBuilderForTier(promptsDir, tier)

	// Build GitHub integration config for coordinator
	ghCfg := hermes.GithubCfg{
		Enabled:         ghIntegration.Enabled,
		SyncChecklist:   ghIntegration.SyncChecklist,
		AutoCloseLabel:  ghIntegration.AutoCloseLabel,
		FailureLabel:    ghIntegration.FailureLabel,
		TriggerTaskSync: ghIntegration.TriggerTaskSync,
	}
	for _, ev := range ghIntegration.CommentOnEvents {
		switch ev {
		case "start":
			ghCfg.CommentOnStart = true
		case "complete":
			ghCfg.CommentOnDone = true
		case "fail":
			ghCfg.CommentOnFail = true
		case "budget_exceeded":
			ghCfg.CommentOnBudget = true
		}
	}

	onDoneHook := t.buildHermesOnDoneHook(key, goal)
	oneShot := issueNumber > 0
	onDone := func(doneCtx context.Context, state hermes.TaskState) {
		t.recordPlannerSession(key, tier, state.PlannerSessionID)
		if sess := t.getChatContext(key, projectDir).Session(appengine.BackendKindForModel(executorModel)); sess != "" {
			t.recordExecutorSession(key, tier, sess)
		}
		if oneShot {
			t.hermesMu.Lock()
			if hc := t.hermesCoords[key]; hc != nil {
				hc.enabled = false
			}
			t.hermesMu.Unlock()
		}
		if onDoneHook != nil {
			onDoneHook(doneCtx, state)
		}
	}
	onReview := func(_ context.Context, state hermes.TaskState, review appengine.ReviewResult, notification appengine.ReviewNotification) {
		t.send(key, notification.TelegramText())
		if globalWebSocketHub != nil {
			BroadcastReviewEvent(notification)
		}
	}
	onReviewSkipped := func(_ context.Context, state hermes.TaskState, reason error) {
		msg := "⚠️ 複審被略過"
		if reason != nil {
			msg += "：" + reason.Error()
		} else {
			msg += "（reviewer 未產生有效結果）"
		}
		t.send(key, msg)
	}
	onTaskRetry := func(_ context.Context, attempt, maxRetries int, review appengine.ReviewResult) {
		nextAttempt := attempt + 2 // attempt is zero-based; user sees 1-based "next round"
		total := maxRetries + 1
		t.send(key, fmt.Sprintf(
			"⚠️ 重審不通過（verdict=%s, %d/100）— 自動 re-plan 重新執行（第 %d/%d 輪）",
			review.Verdict, review.OverallScore, nextAttempt, total,
		))
	}

	continueCh := make(chan struct{}, 1)
	agent := t.getAgent(key)
	if oneShot {
		// Issue-launched tasks start with a fresh executor CLI session so the
		// previous task's transcript does not bloat the prompt and trigger
		// "Prompt is too long" on later subtasks.
		agent.ClearSessionForModel(executorModel)
	} else if executorSessionID != "" {
		agent.chatContext.SetSession(appengine.BackendKindForModel(executorModel), executorSessionID)
	}
	direct := appengine.NewDirectEngine(newHermesExecutorRunner(agent, executorModel, heavyExecutorModel))
	coord := appengine.NewPlanExecuteEngine(appengine.PlanExecuteConfig{
		ChatID:                key.chatID,
		ThreadID:              key.threadID,
		ProjectDir:            projectDir,
		PlannerModel:          plannerModel,
		MaxPlannerJSONRetries: cfg.MaxPlannerJSONRetries,
		InterruptPolicy:       hermes.InterruptPolicy(cfg.InterruptPolicy),
		Budget:                budget,
		AccumulatedCfg:        hermes.AccumulatedConfig{},
		PlannerRules:          pb.ForRole(hermes.RolePlanner),
		ExecutorRules:         pb.ForRole(hermes.RoleExecutor),
		PlannerSessionID:      plannerSessionID,
		GithubIssueNumber:     issueNumber,
		GithubCfg:             ghCfg,
		PostCompletionHook:    t.buildTaskSyncHook(ghIntegration.TriggerTaskSync, projectDir),
		ReviewPhase:           reviewPhase,
		ReviewStore:           globalStorage,
		ReviewMode:            reviewModeForStrict(strictCfg),
		StrictMode:            strictCfg,
		OnReview:              onReview,
		OnReviewSkipped:       onReviewSkipped,
		OnTaskRetry:           onTaskRetry,
		TaskRetry:             appengine.TaskRetryConfig(cfg.TaskRetry),
		ContinueCh:            continueCh,
		OnDone:                onDone,
	}, planFn, direct, taskStore, reporter)

	t.hermesMu.Lock()
	t.hermesCoords[key] = &hermesCoord{coord: coord, enabled: true, continueCh: continueCh, oneShot: oneShot}
	t.hermesMu.Unlock()

	taskID, err := coord.Start(ctx, goal, agent.chatContext)
	if err != nil {
		t.send(key, fmt.Sprintf("Hermes 啟動失敗：%v", err))
		return
	}
	log.Printf("[hermes] chat %d started task %s", key.chatID, taskID)
	displayTaskID := strings.TrimSpace(taskID)
	if len(displayTaskID) > 8 {
		displayTaskID = displayTaskID[:8]
	}
	t.send(key, fmt.Sprintf("📌 任務編號：%s", displayTaskID))
}

// buildTaskSyncHook returns a post-completion hook that refreshes MASTER_TASKS.md
// when triggerSync is true. It intentionally uses Claude CLI because /task-sync
// is a local Claude slash command, not a backend-neutral Alice command yet.
func (t *TelegramBot) buildTaskSyncHook(triggerSync bool, projectDir string) func(ctx context.Context) {
	if !triggerSync {
		return nil
	}
	return func(ctx context.Context) {
		out, err := runProcessCombinedOutput(ctx, ProcessOptions{
			Dir:     projectDir,
			Env:     cleanEnvForCLI(),
			Timeout: defaultAgentProcessTimeout,
		}, "claude", "--print", "--dangerously-skip-permissions", "/task-sync")
		if err != nil {
			log.Printf("[hermes] task-sync failed: %v (output: %s)", err, out)
		} else {
			log.Printf("[hermes] task-sync completed")
		}
	}
}

// buildHermesOnDoneHook returns a callback that writes the Hermes task result
// back into the agent's recentMessages so the next follow-up turn can
// reference what the previous Hermes task actually did (Issue #108).
func (t *TelegramBot) buildHermesOnDoneHook(key chatKey, originalGoal string) func(ctx context.Context, state hermes.TaskState) {
	return func(_ context.Context, state hermes.TaskState) {
		summary := state.Accumulated
		if summary == "" {
			// Fall back to sub-task results if no rolled-up summary exists.
			var parts []string
			for _, sub := range state.Plan {
				if sub.Result != "" {
					parts = append(parts, sub.Result)
				}
			}
			if len(parts) > 0 {
				summary = strings.Join(parts, "\n")
			}
		}
		if summary == "" {
			return
		}
		t.getChatContext(key, "").AddRecentMessage(originalGoal, summary)
		log.Printf("[hermes] wrote task result back to recentMessages for chat %d (%d chars)", key.chatID, len(summary))
	}
}

func (t *TelegramBot) handleStrictCommand(key chatKey, parts []string) {
	sub := ""
	if len(parts) > 1 {
		sub = strings.ToLower(strings.TrimSpace(parts[1]))
	}

	switch sub {
	case "", "toggle":
		enabled := !t.strictModeEnabled(key, "")
		t.setStrictModeOverride(key, &enabled)
		if enabled {
			t.send(key, "✅ strict review mode 已啟用")
		} else {
			t.send(key, "⛔ strict review mode 已停用")
		}
	case "on", "enable":
		enabled := true
		t.setStrictModeOverride(key, &enabled)
		t.send(key, "✅ strict review mode 已啟用")
	case "off", "disable":
		enabled := false
		t.setStrictModeOverride(key, &enabled)
		t.send(key, "⛔ strict review mode 已停用")
	case "status":
		if t.strictModeEnabled(key, "") {
			t.send(key, "strict review mode：已啟用")
		} else {
			t.send(key, "strict review mode：已停用")
		}
	default:
		t.send(key, "用法：/strict [on|off|status]")
	}
}

func (t *TelegramBot) strictModeEnabled(key chatKey, goal string) bool {
	t.hermesMu.RLock()
	hc := t.hermesCoords[key]
	t.hermesMu.RUnlock()
	if hc != nil && hc.strictModeOverride != nil {
		return *hc.strictModeOverride
	}

	if t.config != nil && t.config.Hermes.StrictModeEnabled {
		return true
	}

	return shouldAutoEnableStrict(goal)
}

func (t *TelegramBot) setStrictModeOverride(key chatKey, enabled *bool) {
	t.hermesMu.Lock()
	defer t.hermesMu.Unlock()

	hc := t.hermesCoords[key]
	if hc == nil {
		hc = &hermesCoord{}
		t.hermesCoords[key] = hc
	}
	if enabled == nil {
		hc.strictModeOverride = nil
		return
	}
	value := *enabled
	hc.strictModeOverride = &value
}

func shouldAutoEnableStrict(goal string) bool {
	goal = strings.ToLower(strings.TrimSpace(extractHermesActionableGoal(goal)))
	if goal == "" {
		return false
	}
	return containsAny(goal, []string{"commit", "push", "部署", "deploy", "ssh", "release"})
}

func reviewModeForStrict(strictCfg appengine.StrictModeConfig) appengine.ReviewMode {
	if strictCfg.Enabled {
		return appengine.ReviewModePerSubTask
	}
	return appengine.ReviewModePerTask
}

func (t *TelegramBot) resolveStrictModeConfig(key chatKey, goal string) appengine.StrictModeConfig {
	cfg := appengine.DefaultStrictModeConfig()
	cfg.Enabled = t.strictModeEnabled(key, goal)
	return cfg
}

// --- Send helpers (直接用 Telegram HTTP API 以支援 message_thread_id) ---

// sanitizeUTF8 cleans invalid UTF-8 bytes from text to prevent Telegram API errors
func sanitizeUTF8(text string) string {
	if utf8.ValidString(text) {
		return text
	}

	// Log the issue for debugging
	log.Printf("[telegram] UTF-8 cleaning applied to message (length: %d bytes)", len(text))

	// Use Go's standard library to replace invalid UTF-8 sequences
	return strings.ToValidUTF8(text, "\uFFFD")
}

// messageQueueWorker processes messages from the queue with rate limiting and retry logic
func (t *TelegramBot) messageQueueWorker() {
	log.Printf("[telegram] Message queue worker started (rate: 2 msg/sec)")

	for {
		select {
		case <-t.queueCtx.Done():
			log.Printf("[telegram] Message queue worker stopping")
			t.rateLimiter.Stop()
			return

		case msg := <-t.messageQueue:
			// Wait for rate limiter
			<-t.rateLimiter.C

			// Send the message
			success := t.sendMessageDirect(msg)

			// Handle retry logic for failed messages
			if !success && msg.Retries < msg.MaxRetries {
				msg.Retries++
				log.Printf("[telegram] Retrying message (attempt %d/%d)", msg.Retries, msg.MaxRetries)

				// Exponential backoff: 1s, 2s, 4s, 8s...
				backoffDelay := time.Duration(1<<uint(msg.Retries-1)) * time.Second
				if backoffDelay > 30*time.Second {
					backoffDelay = 30 * time.Second
				}

				go func(message *TelegramMessage) {
					time.Sleep(backoffDelay)
					select {
					case t.messageQueue <- message:
						// Successfully re-queued
					case <-t.queueCtx.Done():
						// Context cancelled, give up
					default:
						// Queue full, drop message
						log.Printf("[telegram] Failed to re-queue message: queue full")
					}
				}(msg)
			} else if !success {
				log.Printf("[telegram] Message failed after %d retries, giving up", msg.MaxRetries)
			}
		}
	}
}

// sendMessageDirect sends a message directly to Telegram API with 429 handling
func (t *TelegramBot) sendMessageDirect(msg *TelegramMessage) bool {
	// Marshal parameters to JSON
	jsonData, err := json.Marshal(msg.Params)
	if err != nil {
		log.Printf("[telegram] Error marshaling message params: %v", err)
		return false
	}

	// Make API call
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/%s", t.config.TelegramToken, msg.Method)
	resp, err := t.telegramAPIClient().Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("[telegram] %s error: %v", msg.Method, err)
		return false
	}
	defer resp.Body.Close()

	// Handle response
	if resp.StatusCode == 200 {
		return true // Success
	}

	// Read error response
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

	// Handle 429 Rate Limiting
	if resp.StatusCode == 429 {
		var errorResp struct {
			OK          bool   `json:"ok"`
			ErrorCode   int    `json:"error_code"`
			Description string `json:"description"`
			Parameters  struct {
				RetryAfter int `json:"retry_after"`
			} `json:"parameters"`
		}

		if err := json.Unmarshal(body, &errorResp); err == nil && errorResp.Parameters.RetryAfter > 0 {
			retryAfter := time.Duration(errorResp.Parameters.RetryAfter) * time.Second
			log.Printf("[telegram] Rate limited! Retry after %v (attempt %d/%d)",
				retryAfter, msg.Retries+1, msg.MaxRetries)

			// Sleep for the specified duration, then return false to trigger retry
			time.Sleep(retryAfter)
			return false
		}
	}

	// Handle 400 Bad Request — if it's a markdown parse error, fall back to plain text
	if resp.StatusCode == 400 && msg.FallbackParams != nil {
		bodyStr := string(body)
		if strings.Contains(bodyStr, "can't parse entities") || strings.Contains(bodyStr, "parse entities") {
			log.Printf("[telegram] Markdown parse error, retrying as plain text: %s", bodyStr)
			msg.Params = msg.FallbackParams
			msg.FallbackParams = nil
			return false // trigger retry with fallback params
		}
	}

	// Log other errors
	log.Printf("[telegram] %s failed (status %d): %s", msg.Method, resp.StatusCode, string(body))
	return false
}

// queueMarkdownMessage queues a message with Markdown parse_mode and a plain-text fallback.
func (t *TelegramBot) queueMarkdownMessage(method string, params map[string]interface{}) {
	// Build fallback params (same but without parse_mode)
	fallback := make(map[string]interface{}, len(params))
	for k, v := range params {
		if k != "parse_mode" {
			fallback[k] = v
		}
	}

	msg := &TelegramMessage{
		Method:         method,
		Params:         params,
		FallbackParams: fallback,
		Retries:        0,
		MaxRetries:     3,
		CreatedAt:      time.Now(),
	}

	select {
	case t.messageQueue <- msg:
	case <-time.After(1 * time.Second):
		log.Printf("[telegram] Message queue full, dropping %s message", method)
	}
}

// queueMessage adds a message to the rate-limited sending queue
func (t *TelegramBot) queueMessage(method string, params map[string]interface{}) {
	msg := &TelegramMessage{
		Method:     method,
		Params:     params,
		Retries:    0,
		MaxRetries: 3, // Allow up to 3 retries
		CreatedAt:  time.Now(),
	}

	select {
	case t.messageQueue <- msg:
		// Successfully queued
	case <-time.After(1 * time.Second):
		// Queue is full or blocked, log and drop
		log.Printf("[telegram] Message queue full, dropping %s message", method)
	}
}

// sendMessageSync sends a message synchronously and returns the response
// Use this only for messages that need to return data (like message ID)
func (t *TelegramBot) sendMessageSync(method string, params map[string]interface{}) (map[string]interface{}, error) {
	// Wait for rate limiter to avoid hitting rate limits
	<-t.rateLimiter.C

	jsonData, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("error marshaling params: %w", err)
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/%s", t.config.TelegramToken, method)
	resp, err := t.telegramAPIClient().Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("HTTP error: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response: %w", err)
	}

	// Handle 429 Rate Limiting
	if resp.StatusCode == 429 {
		var errorResp struct {
			OK          bool   `json:"ok"`
			ErrorCode   int    `json:"error_code"`
			Description string `json:"description"`
			Parameters  struct {
				RetryAfter int `json:"retry_after"`
			} `json:"parameters"`
		}

		if json.Unmarshal(body, &errorResp) == nil && errorResp.Parameters.RetryAfter > 0 {
			retryAfter := time.Duration(errorResp.Parameters.RetryAfter) * time.Second
			log.Printf("[telegram] Rate limited! Waiting %v before retry", retryAfter)
			time.Sleep(retryAfter)

			// Retry once
			return t.sendMessageSync(method, params)
		}
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("telegram API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("error parsing response: %w", err)
	}

	return result, nil
}

func (t *TelegramBot) apiCall(method string, params url.Values) {
	// Convert url.Values to map[string]interface{} for JSON
	jsonParams := make(map[string]interface{})
	for key, values := range params {
		if len(values) == 1 {
			jsonParams[key] = values[0]
		} else {
			jsonParams[key] = values
		}
	}

	// Queue the message instead of sending directly
	t.queueMessage(method, jsonParams)
}

func (t *TelegramBot) send(key chatKey, text string) {
	// Clean invalid UTF-8 characters to prevent API errors
	cleanText := sanitizeUTF8(text)

	params := url.Values{
		"chat_id": {strconv.FormatInt(key.chatID, 10)},
		"text":    {cleanText},
	}
	if key.threadID != 0 {
		params.Set("message_thread_id", strconv.Itoa(key.threadID))
	}
	t.apiCall("sendMessage", params)
}

func (t *TelegramBot) sendSilent(key chatKey, text string) {
	// Clean invalid UTF-8 characters to prevent API errors
	cleanText := sanitizeUTF8(text)

	params := url.Values{
		"chat_id":              {strconv.FormatInt(key.chatID, 10)},
		"text":                 {cleanText},
		"disable_notification": {"true"},
	}
	if key.threadID != 0 {
		params.Set("message_thread_id", strconv.Itoa(key.threadID))
	}
	t.apiCall("sendMessage", params)
}

func (t *TelegramBot) sendMarkdown(key chatKey, text string) {
	// Clean invalid UTF-8 characters to prevent API errors
	cleanText := sanitizeUTF8(text)

	params := map[string]interface{}{
		"chat_id":    strconv.FormatInt(key.chatID, 10),
		"text":       cleanText,
		"parse_mode": "Markdown",
	}
	if key.threadID != 0 {
		params["message_thread_id"] = strconv.Itoa(key.threadID)
	}

	// Use markdown queue with plain-text fallback on parse errors
	t.queueMarkdownMessage("sendMessage", params)
}

func (t *TelegramBot) sendHTML(key chatKey, htmlText string) {
	cleanText := sanitizeUTF8(htmlText)
	params := map[string]interface{}{
		"chat_id":    strconv.FormatInt(key.chatID, 10),
		"text":       cleanText,
		"parse_mode": "HTML",
	}
	if key.threadID != 0 {
		params["message_thread_id"] = strconv.Itoa(key.threadID)
	}
	t.queueMarkdownMessage("sendMessage", params)
}

func (t *TelegramBot) sendLongMarkdown(key chatKey, text string) {
	const maxLen = 3800 // conservative to account for HTML tag overhead

	if len(text) <= maxLen {
		t.sendHTML(key, markdownToTelegramHTML(text))
		return
	}

	chunks := splitMessage(text, maxLen)
	for i, chunk := range chunks {
		if len(chunks) > 1 {
			chunk = fmt.Sprintf("(%d/%d)\n%s", i+1, len(chunks), chunk)
		}
		t.sendHTML(key, markdownToTelegramHTML(chunk))
	}
}

// --- Markdown → Telegram HTML conversion ---

var (
	reTGInlineCode = regexp.MustCompile("`([^`\n]+)`")
	reTGBold       = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	reTGBoldUnder  = regexp.MustCompile(`__([^_\n]+)__`)
	reTGStrike     = regexp.MustCompile(`~~([^~\n]+)~~`)
	reTGLink       = regexp.MustCompile(`\[([^\]\n]+)\]\((https?://[^)\n ]+)\)`)
)

// markdownToTelegramHTML converts Claude's markdown output to Telegram HTML.
// Supports: code blocks, inline code, **bold**, ## headers, links, ~~strike~~, ---, tables
func markdownToTelegramHTML(text string) string {
	var sb strings.Builder
	inCode := false
	var codeLines []string

	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); {
		line := lines[i]
		if !inCode {
			if strings.HasPrefix(strings.TrimRight(line, " \t"), "```") {
				inCode = true
				codeLines = nil
				i++
				continue
			}
			// Detect markdown table (lines starting with |)
			if strings.HasPrefix(strings.TrimSpace(line), "|") {
				j := i
				for j < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[j]), "|") {
					j++
				}
				if j-i >= 2 { // at least header + separator
					sb.WriteString(tgRenderTable(lines[i:j]))
					sb.WriteByte('\n')
					i = j
					continue
				}
			}
			sb.WriteString(tgMarkdownLine(line))
			sb.WriteByte('\n')
		} else {
			if strings.TrimSpace(line) == "```" {
				sb.WriteString("<pre><code>")
				sb.WriteString(tgHTMLEscape(strings.Join(codeLines, "\n")))
				sb.WriteString("</code></pre>\n")
				inCode = false
				codeLines = nil
			} else {
				codeLines = append(codeLines, line)
			}
		}
		i++
	}
	// Flush unclosed code block
	if inCode && len(codeLines) > 0 {
		sb.WriteString("<pre><code>")
		sb.WriteString(tgHTMLEscape(strings.Join(codeLines, "\n")))
		sb.WriteString("</code></pre>\n")
	}
	return strings.TrimSpace(sb.String())
}

// tgCellWidth returns the display width of a string, counting CJK/emoji as 2 columns.
func tgCellWidth(s string) int {
	w := 0
	for _, r := range s {
		switch {
		case r >= 0x1100 && r <= 0x115F, // Hangul Jamo
			r >= 0x2E80 && r <= 0x303F,   // CJK Radicals/Symbols
			r >= 0x3040 && r <= 0x33FF,   // Japanese + CJK Symbols
			r >= 0x3400 && r <= 0x4DBF,   // CJK Extension A
			r >= 0x4E00 && r <= 0x9FFF,   // CJK Unified Ideographs
			r >= 0xAC00 && r <= 0xD7AF,   // Hangul Syllables
			r >= 0xF900 && r <= 0xFAFF,   // CJK Compatibility Ideographs
			r >= 0xFE30 && r <= 0xFE6F,   // CJK Compatibility Forms
			r >= 0xFF01 && r <= 0xFF60,   // Fullwidth Forms
			r >= 0xFFE0 && r <= 0xFFE6,   // Fullwidth Signs
			r >= 0x1F300 && r <= 0x1FAFF: // Emoji
			w += 2
		default:
			w += 1
		}
	}
	return w
}

func tgParseTableCells(line string) []string {
	s := strings.Trim(strings.TrimSpace(line), "|")
	parts := strings.Split(s, "|")
	cells := make([]string, len(parts))
	for i, p := range parts {
		cells[i] = strings.TrimSpace(p)
	}
	return cells
}

func tgIsTableSeparator(line string) bool {
	s := strings.TrimSpace(line)
	for _, c := range s {
		if c != '|' && c != '-' && c != ':' && c != ' ' {
			return false
		}
	}
	return strings.Contains(s, "-")
}

// tgRenderTable converts a slice of markdown table lines into a <pre> ASCII table.
func tgRenderTable(lines []string) string {
	var rows [][]string
	for _, line := range lines {
		if tgIsTableSeparator(line) {
			continue
		}
		rows = append(rows, tgParseTableCells(line))
	}
	if len(rows) == 0 {
		return ""
	}

	// Determine column count and widths
	numCols := 0
	for _, row := range rows {
		if len(row) > numCols {
			numCols = len(row)
		}
	}
	colW := make([]int, numCols)
	for _, row := range rows {
		for i, cell := range row {
			if i < numCols {
				if w := tgCellWidth(cell); w > colW[i] {
					colW[i] = w
				}
			}
		}
	}

	// Build border lines
	topBorder := "┌"
	midBorder := "├"
	botBorder := "└"
	for i, w := range colW {
		seg := strings.Repeat("─", w+2)
		if i < numCols-1 {
			topBorder += seg + "┬"
			midBorder += seg + "┼"
			botBorder += seg + "┴"
		} else {
			topBorder += seg + "┐"
			midBorder += seg + "┤"
			botBorder += seg + "┘"
		}
	}

	var sb strings.Builder
	for rowIdx, row := range rows {
		if rowIdx == 0 {
			sb.WriteString(topBorder + "\n")
		}
		sb.WriteString("│")
		for i := 0; i < numCols; i++ {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			padding := colW[i] - tgCellWidth(cell)
			sb.WriteString(" " + cell + strings.Repeat(" ", padding+1) + "│")
		}
		sb.WriteString("\n")
		if rowIdx == 0 {
			sb.WriteString(midBorder + "\n")
		}
	}
	sb.WriteString(botBorder)

	return "<pre>" + tgHTMLEscape(sb.String()) + "</pre>"
}

func tgMarkdownLine(line string) string {
	s := strings.TrimSpace(line)
	// Horizontal rules → blank line
	if s == "---" || s == "***" || s == "___" {
		return ""
	}
	// Headers (all levels → bold)
	for _, pfx := range []string{"#### ", "### ", "## ", "# "} {
		if strings.HasPrefix(line, pfx) {
			inner := strings.TrimPrefix(line, pfx)
			// Strip bold markers inside headers — Telegram HTML disallows nested <b><b>
			inner = reTGBold.ReplaceAllString(inner, "$1")
			inner = reTGBoldUnder.ReplaceAllString(inner, "$1")
			return "<b>" + tgMarkdownInline(inner) + "</b>"
		}
	}
	return tgMarkdownInline(line)
}

func tgMarkdownInline(text string) string {
	// 1. Protect inline code spans
	var codePH []string
	out := reTGInlineCode.ReplaceAllStringFunc(text, func(m string) string {
		inner := m[1 : len(m)-1]
		codePH = append(codePH, "<code>"+tgHTMLEscape(inner)+"</code>")
		return fmt.Sprintf("\x01%d\x01", len(codePH)-1)
	})

	// 2. Protect links (before HTML-escaping to avoid double-encoding &)
	var linkPH []string
	out = reTGLink.ReplaceAllStringFunc(out, func(m string) string {
		parts := reTGLink.FindStringSubmatch(m)
		if len(parts) == 3 {
			linkPH = append(linkPH, fmt.Sprintf(`<a href="%s">%s</a>`,
				tgHTMLEscape(parts[2]), tgHTMLEscape(parts[1])))
		} else {
			linkPH = append(linkPH, m)
		}
		return fmt.Sprintf("\x02%d\x02", len(linkPH)-1)
	})

	// 3. HTML-escape remaining text
	out = tgHTMLEscape(out)

	// 4. Apply formatting (bold before strike)
	out = reTGBold.ReplaceAllString(out, "<b>$1</b>")
	out = reTGBoldUnder.ReplaceAllString(out, "<b>$1</b>")
	out = reTGStrike.ReplaceAllString(out, "<s>$1</s>")

	// 5. Restore placeholders
	for i, lnk := range linkPH {
		out = strings.ReplaceAll(out, fmt.Sprintf("\x02%d\x02", i), lnk)
	}
	for i, c := range codePH {
		out = strings.ReplaceAll(out, fmt.Sprintf("\x01%d\x01", i), c)
	}
	return out
}

func tgHTMLEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func (t *TelegramBot) sendTyping(key chatKey) {
	params := url.Values{
		"chat_id": {strconv.FormatInt(key.chatID, 10)},
		"action":  {"typing"},
	}
	if key.threadID != 0 {
		params.Set("message_thread_id", strconv.Itoa(key.threadID))
	}
	t.apiCall("sendChatAction", params)
}

func (t *TelegramBot) sendLong(key chatKey, text string) {
	const maxLen = 4000 // 留一點 buffer

	if len(text) <= maxLen {
		t.send(key, text)
		return
	}

	// 按段落分割
	chunks := splitMessage(text, maxLen)
	for i, chunk := range chunks {
		if len(chunks) > 1 {
			chunk = fmt.Sprintf("(%d/%d)\n%s", i+1, len(chunks), chunk)
		}
		t.send(key, chunk)
	}
}

func splitMessage(text string, maxLen int) []string {
	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			chunks = append(chunks, text)
			break
		}

		// 找最近的換行符來分割
		cutAt := maxLen
		if idx := strings.LastIndex(text[:maxLen], "\n"); idx > maxLen/2 {
			cutAt = idx
		}

		chunks = append(chunks, text[:cutAt])
		text = text[cutAt:]
	}
	return chunks
}

// handleMultiAgentStatus shows the current multi-agent system status
func (t *TelegramBot) handleMultiAgentStatus(key chatKey) {
	stats := globalAgentCoordinator.GetAgentStats()

	statusMsg := t.getLocalizedMessage(key.chatID, "multiagent_status_title", nil)
	status := statusMsg

	if globalAgentCoordinator.IsEnabled() {
		status += t.getLocalizedMessage(key.chatID, "multiagent_status_enabled", nil) + "\n\n"
	} else {
		status += t.getLocalizedMessage(key.chatID, "multiagent_status_disabled", nil) + "\n\n"
	}

	totalAgents := stats["total_agents"].(int)
	statsMsg := t.getLocalizedMessage(key.chatID, "multiagent_status_stats", nil)
	statsMsg = strings.ReplaceAll(statsMsg, "{count}", fmt.Sprintf("%d", totalAgents))
	status += statsMsg

	if activeTask, hasTask := stats["active_task"]; hasTask && activeTask != nil {
		taskInfo := activeTask.(map[string]interface{})
		runningMsg := t.getLocalizedMessage(key.chatID, "multiagent_status_running", nil)
		runningMsg = strings.ReplaceAll(runningMsg, "{id}", taskInfo["id"].(string))
		runningMsg = strings.ReplaceAll(runningMsg, "{status}", taskInfo["status"].(string))
		status += runningMsg
	}

	status += t.getLocalizedMessage(key.chatID, "multiagent_available_types", nil) + "\n"
	status += "• General - " + t.getLocalizedMessage(key.chatID, "agent_general", nil) + "\n"
	status += "• CodeReview - " + t.getLocalizedMessage(key.chatID, "agent_code_review", nil) + "\n"
	status += "• Testing - " + t.getLocalizedMessage(key.chatID, "agent_testing", nil) + "\n"
	status += "• Documentation - " + t.getLocalizedMessage(key.chatID, "agent_documentation", nil) + "\n"
	status += "• Deployment - " + t.getLocalizedMessage(key.chatID, "agent_deployment", nil) + "\n"
	status += "• Debug - " + t.getLocalizedMessage(key.chatID, "agent_debug", nil) + "\n"

	t.sendMarkdown(key, status)
}

// handleMultiAgentStats shows detailed statistics about agent usage
func (t *TelegramBot) handleMultiAgentStats(key chatKey) {
	stats := globalAgentCoordinator.GetAgentStats()

	titleMsg := t.getLocalizedMessage(key.chatID, "multiagent_usage_stats_title", nil)
	response := titleMsg

	if agents, hasAgents := stats["agents"]; hasAgents {
		agentStats := agents.(map[string]interface{})

		if len(agentStats) == 0 {
			response += t.getLocalizedMessage(key.chatID, "multiagent_no_active", nil) + "\n"
		} else {
			for agentType, agentInfo := range agentStats {
				info := agentInfo.(map[string]interface{})
				taskCount := info["task_count"].(int)
				lastUsed := info["last_used"].(time.Time)

				headerMsg := t.getLocalizedMessage(key.chatID, "multiagent_agent_header", nil)
				headerMsg = strings.ReplaceAll(headerMsg, "{agent_type}", agentType)
				response += headerMsg

				taskCountMsg := t.getLocalizedMessage(key.chatID, "multiagent_agent_task_count", nil)
				taskCountMsg = strings.ReplaceAll(taskCountMsg, "{count}", fmt.Sprintf("%d", taskCount))
				response += taskCountMsg

				lastUsedMsg := t.getLocalizedMessage(key.chatID, "multiagent_agent_last_used", nil)
				lastUsedMsg = strings.ReplaceAll(lastUsedMsg, "{time}", lastUsed.Format("2006-01-02 15:04:05"))
				response += lastUsedMsg
			}
		}
	}

	t.sendMarkdown(key, response)
}

// handleAgentsList shows available agent types and their capabilities
func (t *TelegramBot) handleAgentsList(key chatKey) {
	response := t.getLocalizedMessage(key.chatID, "multiagent_list_response", nil) + "\n\n"

	agents := []struct {
		name    string
		key     string
		descKey string
	}{
		{"General", "agent_general", "agent_general_desc"},
		{"CodeReview", "agent_code_review", "agent_code_review_desc"},
		{"Testing", "agent_testing", "agent_testing_desc"},
		{"Documentation", "agent_documentation", "agent_documentation_desc"},
		{"Deployment", "agent_deployment", "agent_deployment_desc"},
		{"Debug", "agent_debug", "agent_debug_desc"},
	}

	for _, agent := range agents {
		response += fmt.Sprintf("**%s**\n", agent.name)
		desc := t.getLocalizedMessage(key.chatID, agent.descKey, nil)
		response += fmt.Sprintf("Description: %s\n\n", desc)
	}

	response += t.getLocalizedMessage(key.chatID, "agent_usage_title", nil) + "\n"
	response += t.getLocalizedMessage(key.chatID, "agent_usage_auto", nil) + "\n"
	response += t.getLocalizedMessage(key.chatID, "agent_usage_enable", nil) + "\n"
	response += t.getLocalizedMessage(key.chatID, "agent_usage_complex", nil) + "\n"

	t.sendMarkdown(key, response)
}

// handleDashboard shows system dashboard information
func (t *TelegramBot) handleDashboard(key chatKey) {
	log.Printf("[telegram] handleDashboard called for chat %d", key.chatID)
	dashboard := "📊 *Alice AI Agent Dashboard*\n\n"

	// System Health
	healthTitle := t.getLocalizedMessage(key.chatID, "dashboard_health_title", nil)
	dashboard += healthTitle + "\n"
	if globalWebSocketHub != nil {
		websocketMsg := t.getLocalizedMessage(key.chatID, "dashboard_websocket_running", nil)
		dashboard += websocketMsg + "\n"
		connectedClients := globalWebSocketHub.GetConnectedClients()
		connMsg := t.getLocalizedMessage(key.chatID, "dashboard_status_connections", nil)
		connMsg = strings.ReplaceAll(connMsg, "{count}", fmt.Sprintf("%d", connectedClients))
		dashboard += connMsg
	}

	if globalCheckpointManager != nil && globalCheckpointManager.IsEnabled() {
		checkpointEnabledMsg := t.getLocalizedMessage(key.chatID, "dashboard_checkpoint_enabled", nil)
		dashboard += checkpointEnabledMsg + "\n"
	} else {
		checkpointDisabledMsg := t.getLocalizedMessage(key.chatID, "dashboard_checkpoint_disabled", nil)
		dashboard += checkpointDisabledMsg + "\n"
	}

	if globalAgentCoordinator != nil && globalAgentCoordinator.IsEnabled() {
		multiagentEnabledMsg := t.getLocalizedMessage(key.chatID, "dashboard_multiagent_enabled", nil)
		dashboard += multiagentEnabledMsg + "\n"
	} else {
		multiagentDisabledMsg := t.getLocalizedMessage(key.chatID, "dashboard_multiagent_disabled", nil)
		dashboard += multiagentDisabledMsg + "\n"
	}

	// Web Interface
	if t.config.EnableWebInterface {
		titleMsg := t.getLocalizedMessage(key.chatID, "dashboard_title", nil)
		dashboard += titleMsg

		mainMsg := t.getLocalizedMessage(key.chatID, "dashboard_main", nil)
		mainMsg = strings.ReplaceAll(mainMsg, "{port}", t.config.WebPort)
		dashboard += mainMsg

		timelineMsg := t.getLocalizedMessage(key.chatID, "dashboard_timeline", nil)
		timelineMsg = strings.ReplaceAll(timelineMsg, "{port}", t.config.WebPort)
		dashboard += timelineMsg

		testMsg := t.getLocalizedMessage(key.chatID, "dashboard_test", nil)
		testMsg = strings.ReplaceAll(testMsg, "{port}", t.config.WebPort)
		dashboard += testMsg
	}

	// Storage Info
	if globalStorage != nil {
		storageTitle := t.getLocalizedMessage(key.chatID, "dashboard_storage_title", nil)
		dashboard += storageTitle + "\n"
		dbMsg := t.getLocalizedMessage(key.chatID, "dashboard_database", nil)
		dbMsg = strings.ReplaceAll(dbMsg, "{path}", t.config.DatabasePath)
		dashboard += dbMsg
		sqliteMsg := t.getLocalizedMessage(key.chatID, "dashboard_sqlite_running", nil)
		dashboard += sqliteMsg + "\n"
	}

	// Quick Actions
	quickActionsTitle := t.getLocalizedMessage(key.chatID, "dashboard_quick_actions", nil)
	dashboard += quickActionsTitle + "\n"
	dashboard += t.getLocalizedMessage(key.chatID, "dashboard_quick_checkpoints", nil) + "\n"
	dashboard += t.getLocalizedMessage(key.chatID, "dashboard_quick_status", nil) + "\n"
	dashboard += t.getLocalizedMessage(key.chatID, "dashboard_quick_multiagent", nil) + "\n"
	dashboard += t.getLocalizedMessage(key.chatID, "dashboard_quick_button", nil) + "\n"

	// Send dashboard with Web App button
	t.sendDashboardWithWebApp(key, dashboard)
}

// handleCheckpointsList shows checkpoint information
func (t *TelegramBot) handleCheckpointsList(key chatKey) {
	if globalCheckpointManager == nil {
		t.send(key, t.getLocalizedMessage(key.chatID, "checkpoint_disabled", nil))
		return
	}

	agent := t.getAgent(key)
	projectDir := agent.ProjectDir()

	checkpoints, err := globalCheckpointManager.ListCheckpoints(projectDir, 10)
	if err != nil {
		msg := t.getLocalizedMessage(key.chatID, "checkpoint_list_failed", map[string]string{"error": err.Error()})
		t.send(key, msg)
		return
	}

	titleMsg := t.getLocalizedMessage(key.chatID, "checkpoint_list_title", nil)
	titleMsg = strings.ReplaceAll(titleMsg, "{path}", projectDir)
	response := titleMsg

	countMsg := t.getLocalizedMessage(key.chatID, "checkpoint_list_count", nil)
	countMsg = strings.ReplaceAll(countMsg, "{count}", fmt.Sprintf("%d", len(checkpoints)))
	response += countMsg

	if len(checkpoints) == 0 {
		response += t.getLocalizedMessage(key.chatID, "dashboard_no_checkpoints", nil) + "\n\n"
		response += t.getLocalizedMessage(key.chatID, "dashboard_checkpoint_tip", nil)
	} else {
		response += t.getLocalizedMessage(key.chatID, "dashboard_recent_checkpoints", nil) + "\n"
		for i, cp := range checkpoints {
			if i >= 5 { // 最多顯示 5 個
				break
			}
			itemMsg := t.getLocalizedMessage(key.chatID, "checkpoint_list_item", nil)
			itemMsg = strings.ReplaceAll(itemMsg, "{id}", cp.ID[:12])
			response += itemMsg

			descMsg := t.getLocalizedMessage(key.chatID, "checkpoint_list_description", nil)
			descMsg = strings.ReplaceAll(descMsg, "{description}", cp.Description)
			response += descMsg

			timeMsg := t.getLocalizedMessage(key.chatID, "checkpoint_list_timestamp", nil)
			timeMsg = strings.ReplaceAll(timeMsg, "{timestamp}", cp.Timestamp.Format("01/02 15:04"))
			response += timeMsg

			sizeMsg := t.getLocalizedMessage(key.chatID, "checkpoint_list_size", nil)
			sizeMsg = strings.ReplaceAll(sizeMsg, "{size}", fmt.Sprintf("%d", cp.Size))
			response += sizeMsg
		}
	}

	t.sendMarkdown(key, response)
}

// handleCheckpointsStats shows checkpoint statistics
func (t *TelegramBot) handleCheckpointsStats(key chatKey) {
	if globalCheckpointManager == nil {
		t.send(key, t.getLocalizedMessage(key.chatID, "checkpoint_disabled", nil))
		return
	}

	agent := t.getAgent(key)
	projectDir := agent.ProjectDir()

	stats, err := globalCheckpointManager.GetCheckpointStats(projectDir)
	if err != nil {
		errMsg := t.getLocalizedMessage(key.chatID, "checkpoint_stats_error", nil)
		errMsg = strings.ReplaceAll(errMsg, "{error}", err.Error())
		t.send(key, errMsg)
		return
	}

	statsTitle := t.getLocalizedMessage(key.chatID, "checkpoint_stats_title", nil)
	statsTitle = strings.ReplaceAll(statsTitle, "{path}", projectDir)
	response := statsTitle

	if totalCheckpoints, ok := stats["total_checkpoints"].(int64); ok {
		totalMsg := t.getLocalizedMessage(key.chatID, "checkpoint_stats_total", nil)
		totalMsg = strings.ReplaceAll(totalMsg, "{count}", fmt.Sprintf("%d", totalCheckpoints))
		response += totalMsg
	}

	if totalSize, ok := stats["total_size"].(int64); ok {
		sizeMsg := t.getLocalizedMessage(key.chatID, "checkpoint_stats_size", nil)
		sizeMsg = strings.ReplaceAll(sizeMsg, "{size}", fmt.Sprintf("%d", totalSize))
		response += sizeMsg
	}

	if avgSize, ok := stats["average_size"].(float64); ok {
		avgMsg := t.getLocalizedMessage(key.chatID, "checkpoint_stats_avg_size", nil)
		avgMsg = strings.ReplaceAll(avgMsg, "{size}", fmt.Sprintf("%.1f", avgSize))
		response += avgMsg
	}

	autoCheckpointTitle := t.getLocalizedMessage(key.chatID, "dashboard_auto_checkpoint_title", nil)
	autoCheckpointWrite := t.getLocalizedMessage(key.chatID, "dashboard_auto_checkpoint_write", nil)
	autoCheckpointDangerous := t.getLocalizedMessage(key.chatID, "dashboard_auto_checkpoint_dangerous", nil)
	autoCheckpointConfig := t.getLocalizedMessage(key.chatID, "dashboard_auto_checkpoint_config", nil)
	response += "\n" + autoCheckpointTitle + "\n"
	response += autoCheckpointWrite + "\n"
	response += autoCheckpointDangerous + "\n"
	response += autoCheckpointConfig + "\n"

	t.sendMarkdown(key, response)
}

// sendDashboardWithWebApp sends dashboard with refresh button
func (t *TelegramBot) sendDashboardWithWebApp(key chatKey, text string) {
	// Clean invalid UTF-8 characters to prevent API errors
	cleanText := sanitizeUTF8(text)

	// Create inline keyboard with refresh button only (Web App requires HTTPS)
	refreshText := t.getLocalizedMessage(key.chatID, "button_refresh_status", nil)
	checkpointText := t.getLocalizedMessage(key.chatID, "button_view_checkpoints", nil)
	keyboard := map[string]interface{}{
		"inline_keyboard": [][]map[string]interface{}{
			{
				{
					"text":          refreshText,
					"callback_data": "refresh_dashboard",
				},
				{
					"text":          checkpointText,
					"callback_data": "show_checkpoints",
				},
			},
		},
	}

	msg := map[string]interface{}{
		"chat_id":      key.chatID,
		"text":         cleanText,
		"parse_mode":   "Markdown",
		"reply_markup": keyboard,
	}

	if key.threadID != 0 {
		msg["message_thread_id"] = key.threadID
	}

	t.sendTelegram("sendMessage", msg)
}

// handleCallbackQuery handles inline keyboard button clicks
func (t *TelegramBot) handleCallbackQuery(key chatKey, userID int64, queryID, data string) {
	// Check permissions
	if !t.isAllowed(userID) {
		noPermMsg := t.getLocalizedMessage(key.chatID, "callback_no_permission", nil)
		t.answerCallbackQuery(queryID, noPermMsg)
		return
	}

	// Handle different callback data
	switch {
	case data == "refresh_dashboard":
		// Send updated dashboard
		t.handleDashboard(key)
		refreshMsg := t.getLocalizedMessage(key.chatID, "callback_refresh_success", nil)
		t.answerCallbackQuery(queryID, refreshMsg)
	case data == "show_checkpoints":
		// Show checkpoints for current project
		t.handleCheckpointsList(key)
		checkpointMsg := t.getLocalizedMessage(key.chatID, "callback_checkpoint_updated", nil)
		t.answerCallbackQuery(queryID, checkpointMsg)
	case strings.HasPrefix(data, "tasks:"):
		parts := strings.SplitN(data, ":", 3)
		state := "open"
		if len(parts) == 3 {
			if parts[2] == "open" || parts[2] == "closed" {
				state = parts[2]
			}
		}
		if err := t.sendTasksMessage(key, t.getAgent(key).ProjectDir(), state); err != nil {
			if errors.Is(err, errTasksGitHubAuthRequired) {
				t.answerCallbackQuery(queryID, tasksAuthRequiredMessage(t.getChatLanguage(key.chatID)))
				return
			}
			if errors.Is(err, errTasksNoGitHubRepo) {
				t.answerCallbackQuery(queryID, tasksNoRepoMessage(t.getChatLanguage(key.chatID)))
				return
			}
			log.Printf("[telegram] tasks callback failed: %v", err)
			t.answerCallbackQuery(queryID, tasksNoRepoMessage(t.getChatLanguage(key.chatID)))
			return
		}
		t.answerCallbackQuery(queryID, tasksStatusMessage(t.getChatLanguage(key.chatID), state))
	case strings.HasPrefix(data, "menu:"):
		t.handleMenuCallback(key, queryID, data)
	case strings.HasPrefix(data, "retry:"):
		t.handleRetryCallback(key, queryID, data)
	case strings.HasPrefix(data, "model:"):
		t.handleModelCallback(key, queryID, data)
	case strings.HasPrefix(data, "hermes:"):
		t.handleHermesCallback(key, queryID, data)
	case strings.HasPrefix(data, "stop_agent_"):
		// Handle stop button click
		switch t.abortActiveTask(key, 0) {
		case abortTaskAborted:
			abortMsg := t.getLocalizedMessage(key.chatID, "callback_task_aborted", nil)
			t.answerCallbackQuery(queryID, abortMsg)
			log.Printf("Task stopped by user via callback button (chat: %d, thread: %d)", key.chatID, key.threadID)
		case abortTaskFinished:
			failMsg := t.getLocalizedMessage(key.chatID, "callback_abort_failed", nil)
			t.answerCallbackQuery(queryID, failMsg)
		default:
			noTaskMsg := t.getLocalizedMessage(key.chatID, "callback_no_running_task", nil)
			t.answerCallbackQuery(queryID, noTaskMsg)
		}
	default:
		unknownMsg := t.getLocalizedMessage(key.chatID, "callback_unknown_operation", nil)
		t.answerCallbackQuery(queryID, unknownMsg)
	}
}

func (t *TelegramBot) handleMenuCallback(key chatKey, queryID, data string) {
	action := strings.TrimPrefix(data, "menu:")
	switch action {
	case "open":
		t.answerCallbackQuery(queryID, "顯示主選單")
		t.sendMenu(key)
	case "cancel":
		t.answerCallbackQuery(queryID, "已取消")
	case "status":
		t.answerCallbackQuery(queryID, "顯示狀態")
		t.handleCommand(key, "/status")
	case "usage":
		t.answerCallbackQuery(queryID, "顯示用量")
		t.handleCommand(key, "/usage")
	case "tasks":
		t.answerCallbackQuery(queryID, "顯示 Tasks 選單")
		t.sendTasksSelector(key)
	case "hermes_status":
		t.answerCallbackQuery(queryID, "顯示 Hermes 狀態")
		t.handleHermesCommand(key, []string{"/hermes", "status"}, "")
	case "help":
		t.answerCallbackQuery(queryID, "顯示說明")
		t.handleCommand(key, "/help")
	case "abort_confirm":
		t.answerCallbackQuery(queryID, "請確認中斷")
		t.sendAbortConfirmation(key)
	default:
		t.answerCallbackQuery(queryID, "無法辨識選單操作")
	}
}

func (t *TelegramBot) sendTasksSelector(key chatKey) {
	t.sendMenuMessage(key, "📌 Tasks\n\n選擇要查看的 task 狀態。", [][]map[string]interface{}{
		{
			{"text": "Open", "callback_data": "tasks:view:open"},
			{"text": "Closed", "callback_data": "tasks:view:closed"},
		},
		{
			{"text": "Refresh Open", "callback_data": "tasks:refresh:open"},
			{"text": "Refresh Closed", "callback_data": "tasks:refresh:closed"},
		},
		{
			{"text": "回主選單", "callback_data": "menu:open"},
		},
	})
}

func (t *TelegramBot) sendAbortConfirmation(key chatKey) {
	t.sendMenuMessage(key, "⚠️ 確定要中斷目前正在執行的任務嗎？", [][]map[string]interface{}{
		{
			{"text": "確認中斷", "callback_data": fmt.Sprintf("stop_agent_%d_%d", key.chatID, key.threadID)},
			{"text": "取消", "callback_data": "menu:cancel"},
		},
	})
}

func (t *TelegramBot) sendModelMenu(key chatKey) {
	current := t.getUserModelPreference(key)
	if current == "" {
		current = "auto"
	}
	text := fmt.Sprintf("🧭 Model / Backend\n\n目前模式：%s", current)
	t.sendMenuMessage(key, text, [][]map[string]interface{}{
		{
			{"text": "Claude Fast", "callback_data": "model:set:fast"},
			{"text": "Claude Smart", "callback_data": "model:set:smart"},
		},
		{
			{"text": "Claude Deep", "callback_data": "model:set:deep"},
			{"text": "Plan", "callback_data": "model:set:plan"},
		},
		{
			{"text": "GPT Fast", "callback_data": "model:set:gpt-fast"},
			{"text": "GPT Smart", "callback_data": "model:set:gpt-smart"},
		},
		{
			{"text": "GPT Deep", "callback_data": "model:set:gpt-deep"},
			{"text": "Auto", "callback_data": "model:set:auto"},
		},
		{
			{"text": "Backend 狀態", "callback_data": "model:backend"},
			{"text": "回主選單", "callback_data": "menu:open"},
		},
	})
}

func (t *TelegramBot) handleModelCallback(key chatKey, queryID, data string) {
	switch {
	case data == "model:menu":
		t.answerCallbackQuery(queryID, "顯示模型選單")
		t.sendModelMenu(key)
	case data == "model:backend":
		t.answerCallbackQuery(queryID, "顯示 backend")
		t.handleBackendCommand(key, []string{"/backend", "list"})
	case strings.HasPrefix(data, "model:set:"):
		mode := strings.TrimPrefix(data, "model:set:")
		command := map[string]string{
			"fast":      "/fast",
			"smart":     "/smart",
			"deep":      "/deep",
			"gpt-fast":  "/gfast",
			"gpt-smart": "/gsmart",
			"gpt-deep":  "/gdeep",
			"auto":      "/auto",
			"plan":      "/plan",
		}[mode]
		if command == "" {
			t.answerCallbackQuery(queryID, "無法辨識模型")
			return
		}
		t.answerCallbackQuery(queryID, "切換模型")
		t.handleCommand(key, command)
	default:
		t.answerCallbackQuery(queryID, "無法辨識模型操作")
	}
}

func (t *TelegramBot) sendRetryMenu(key chatKey) {
	rows := [][]map[string]interface{}{
		{
			{"text": "Retry latest", "callback_data": "retry:confirm:latest"},
			{"text": "回主選單", "callback_data": "menu:open"},
		},
	}
	text := "🔁 Retry\n\n選擇要重跑的 review sub-task。"
	store, ok := globalStorage.(*SQLiteStorage)
	if globalStorage == nil || !ok {
		text += "\n\nStorage 尚未啟用，暫時只能使用 slash command。"
		t.sendMenuMessage(key, text, rows)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), retrySelectionTimeout)
	defer cancel()
	candidates, err := store.selectRetryTaskCandidates(ctx, key, 5)
	if err != nil {
		text += "\n\n讀取候選任務失敗：" + err.Error()
		t.sendMenuMessage(key, text, rows)
		return
	}
	for _, candidate := range candidates {
		rows = append(rows, []map[string]interface{}{
			{"text": retryCandidateButtonText(candidate), "callback_data": "retry:task:" + candidate.ID},
		})
	}
	if len(candidates) == 0 {
		text += "\n\n目前沒有找到這個 topic 的 retry 候選任務。"
	}
	t.sendMenuMessage(key, text, rows)
}

func retryCandidateButtonText(candidate retryTaskCandidate) string {
	prefix := shortHermesTaskID(candidate.ID)
	if candidate.GithubIssueNumber > 0 {
		prefix = fmt.Sprintf("#%d %s", candidate.GithubIssueNumber, prefix)
	}
	goal := truncateForTelegram(strings.TrimSpace(candidate.Goal), 34)
	if goal == "" {
		goal = "untitled task"
	}
	return fmt.Sprintf("%s · %d failed · %s", prefix, candidate.FailedCount, goal)
}

func (t *TelegramBot) sendRetryAllFailedResolution(key chatKey) {
	ctx, cancel := context.WithTimeout(context.Background(), retrySelectionTimeout)
	defer cancel()
	candidates, err := t.resolveTargets(ctx, ResolveTargetRequest{
		ChatID:   key.chatID,
		ThreadID: key.threadID,
		Intent:   "retry_all_failed",
		Kinds:    []TargetKind{TargetReviewResult},
		Limit:    3,
	})
	if err != nil {
		t.send(key, "❌ 無法解析 retry 目標："+err.Error())
		return
	}
	if len(candidates) == 0 {
		t.send(key, "✅ 找不到低分或 partial/fail 的 task 可 retry。")
		return
	}
	if len(candidates) == 1 && candidates[0].Score >= 0.75 {
		candidate := candidates[0]
		text := fmt.Sprintf("準備 retry task `%s` 的所有 failed subtasks\n\n%s\n%s",
			shortHermesTaskID(candidate.ID),
			truncateForTelegram(candidate.Title, 120),
			candidate.Reason,
		)
		t.sendMenuMessage(key, text, [][]map[string]interface{}{
			{
				{"text": t.getLocalizedMessage(key.chatID, "menu_retry_confirm_btn", nil), "callback_data": "retry:run:all:" + candidate.ID},
				{"text": t.getLocalizedMessage(key.chatID, "menu_btn_cancel", nil), "callback_data": "retry:cancel"},
			},
			{
				{"text": t.getLocalizedMessage(key.chatID, "menu_btn_back", nil), "callback_data": "retry:menu"},
			},
		})
		return
	}
	rows := make([][]map[string]interface{}, 0, len(candidates)+1)
	for _, candidate := range candidates {
		label := fmt.Sprintf("%s · %.0f%% · %s", shortHermesTaskID(candidate.ID), candidate.Score*100, truncateForTelegram(candidate.Title, 34))
		rows = append(rows, []map[string]interface{}{{"text": label, "callback_data": "retry:task:" + candidate.ID}})
	}
	rows = append(rows, []map[string]interface{}{{"text": t.getLocalizedMessage(key.chatID, "menu_btn_cancel", nil), "callback_data": "retry:cancel"}})
	t.sendMenuMessage(key, "你要 retry 哪個 task？", rows)
}

func (t *TelegramBot) sendRetryTaskMenu(key chatKey, taskID string) {
	if globalStorage == nil {
		t.send(key, "❌ Storage 尚未啟用，無法讀取 review 結果。")
		return
	}
	store, ok := globalStorage.(*SQLiteStorage)
	if !ok {
		t.send(key, "❌ 目前 storage backend 不支援 retry 選單。")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), retrySelectionTimeout)
	defer cancel()
	selections, err := store.selectRetryTargetsAllFailed(ctx, taskID)
	if err != nil {
		t.send(key, "❌ "+err.Error())
		return
	}
	rows := [][]map[string]interface{}{
		{
			{"text": "最低分 sub-task", "callback_data": "retry:confirm:lowest:" + taskID},
			{"text": "全部失敗", "callback_data": "retry:confirm:all:" + taskID},
		},
	}
	for _, selection := range selections {
		rows = append(rows, []map[string]interface{}{
			{
				"text":          fmt.Sprintf("#%d · %d/100 · %s", selection.DisplaySubTaskIdx, selection.SubTaskReview.Score, truncateForTelegram(selection.SubTask.Description, 30)),
				"callback_data": fmt.Sprintf("retry:confirm:index:%s:%d", taskID, selection.DisplaySubTaskIdx),
			},
		})
	}
	rows = append(rows, []map[string]interface{}{{"text": "回 Retry", "callback_data": "retry:menu"}})
	t.sendMenuMessage(key, fmt.Sprintf("🔁 Retry task `%s`\n\n選擇要重跑的範圍。", shortHermesTaskID(taskID)), rows)
}

func (t *TelegramBot) sendRetryConfirmation(key chatKey, mode, taskID string, idx int) {
	label := "latest low-score sub-task"
	runData := "retry:run:latest"
	switch mode {
	case "lowest":
		label = "這個 task 的最低分 sub-task"
		runData = "retry:run:lowest:" + taskID
	case "all":
		label = "這個 task 的所有失敗 sub-task"
		runData = "retry:run:all:" + taskID
	case "index":
		label = fmt.Sprintf("這個 task 的 sub-task #%d", idx)
		runData = fmt.Sprintf("retry:run:index:%s:%d", taskID, idx)
	}
	t.sendMenuMessage(key, "⚠️ 確認執行 retry？\n\n將重跑 "+label+"。", [][]map[string]interface{}{
		{
			{"text": "確認執行", "callback_data": runData},
			{"text": "取消", "callback_data": "retry:cancel"},
		},
	})
}

func (t *TelegramBot) handleRetryCallback(key chatKey, queryID, data string) {
	switch {
	case data == "retry:menu":
		t.answerCallbackQuery(queryID, "顯示 retry 選單")
		t.sendRetryMenu(key)
	case data == "retry:cancel":
		t.answerCallbackQuery(queryID, "已取消")
	case data == "retry:confirm:latest":
		t.answerCallbackQuery(queryID, "請確認 retry")
		t.sendRetryConfirmation(key, "latest", "", 0)
	case strings.HasPrefix(data, "retry:task:"):
		taskID := strings.TrimPrefix(data, "retry:task:")
		t.answerCallbackQuery(queryID, "選擇 retry 範圍")
		t.sendRetryTaskMenu(key, taskID)
	case strings.HasPrefix(data, "retry:confirm:"):
		t.handleRetryConfirmCallback(key, queryID, data)
	case strings.HasPrefix(data, "retry:run:"):
		t.handleRetryRunCallback(key, queryID, data)
	default:
		t.answerCallbackQuery(queryID, "無法辨識 retry 操作")
	}
}

func (t *TelegramBot) handleRetryConfirmCallback(key chatKey, queryID, data string) {
	parts := strings.Split(data, ":")
	if len(parts) < 3 {
		t.answerCallbackQuery(queryID, "無法辨識 retry 操作")
		return
	}
	switch parts[2] {
	case "lowest":
		if len(parts) != 4 {
			t.answerCallbackQuery(queryID, "缺少 task id")
			return
		}
		t.answerCallbackQuery(queryID, "請確認 retry")
		t.sendRetryConfirmation(key, "lowest", parts[3], 0)
	case "all":
		if len(parts) != 4 {
			t.answerCallbackQuery(queryID, "缺少 task id")
			return
		}
		t.answerCallbackQuery(queryID, "請確認 retry all")
		t.sendRetryConfirmation(key, "all", parts[3], 0)
	case "index":
		if len(parts) != 5 {
			t.answerCallbackQuery(queryID, "缺少 sub-task 編號")
			return
		}
		idx, err := strconv.Atoi(parts[4])
		if err != nil || idx <= 0 {
			t.answerCallbackQuery(queryID, "sub-task 編號無效")
			return
		}
		t.answerCallbackQuery(queryID, "請確認 retry")
		t.sendRetryConfirmation(key, "index", parts[3], idx)
	default:
		t.answerCallbackQuery(queryID, "無法辨識 retry 操作")
	}
}

func (t *TelegramBot) handleRetryRunCallback(key chatKey, queryID, data string) {
	parts := strings.Split(data, ":")
	if len(parts) < 3 {
		t.answerCallbackQuery(queryID, "無法辨識 retry 操作")
		return
	}
	t.answerCallbackQuery(queryID, "開始 retry")
	switch parts[2] {
	case "latest":
		t.handleRetryCommand(key, []string{"/retry", "latest"})
	case "lowest":
		if len(parts) == 4 {
			t.handleRetryCommand(key, []string{"/retry", parts[3]})
		}
	case "all":
		if len(parts) == 4 {
			t.handleRetryCommand(key, []string{"/retry", parts[3], "all-failed"})
		}
	case "index":
		if len(parts) == 5 {
			t.handleRetryCommand(key, []string{"/retry", parts[3], parts[4]})
		}
	default:
		t.answerCallbackQuery(queryID, "無法辨識 retry 操作")
	}
}

func parseHermesCallbackData(data string) (mode string, taskID string, ok bool) {
	if data == "hermes:cancel" {
		return "cancel", "", true
	}
	if strings.HasPrefix(data, "hermes:issue:") {
		rest := strings.TrimPrefix(data, "hermes:issue:")
		issueID, tier, _ := strings.Cut(rest, ":")
		if strings.TrimSpace(issueID) == "" {
			return "", "", false
		}
		if tier != "" {
			return "issue:" + tier, issueID, true
		}
		return "issue", issueID, true
	}
	mode, taskID, found := strings.Cut(strings.TrimPrefix(data, "hermes:"), ":")
	if !found || taskID == "" {
		return "", "", false
	}
	switch mode {
	case "continue", "replan":
		return mode, taskID, true
	default:
		return "", "", false
	}
}

func (t *TelegramBot) handleHermesCallback(key chatKey, queryID, data string) {
	mode, taskID, ok := parseHermesCallbackData(data)
	if !ok {
		t.answerCallbackQuery(queryID, "無法辨識 Hermes 操作")
		return
	}
	if mode == "cancel" {
		t.answerCallbackQuery(queryID, "已取消")
		return
	}
	if strings.HasPrefix(mode, "issue") {
		issueNumber, err := strconv.Atoi(taskID)
		if err != nil || issueNumber <= 0 {
			t.answerCallbackQuery(queryID, "Issue 編號無效")
			return
		}
		tier := ""
		if _, rawTier, found := strings.Cut(mode, ":"); found {
			tier = rawTier
		}
		projectDir := t.getAgent(key).ProjectDir()
		t.setHermesTier(key, tier)
		t.answerCallbackQuery(queryID, fmt.Sprintf("讀取 Issue #%d", issueNumber))
		t.send(key, fmt.Sprintf("🔍 正在讀取 GitHub Issue #%d…", issueNumber))
		go t.runTrackedJob("hermes.issue.callback", func() {
			t.startHermesFromIssue(key, issueNumber, projectDir)
		})
		return
	}

	hermesTask, err := t.taskSvc.GetTask(taskID)
	if err != nil {
		log.Printf("[hermes] callback task lookup failed (task=%s): %v", taskID, err)
		t.answerCallbackQuery(queryID, "找不到 Hermes 任務")
		return
	}
	task := hermesTask
	if task.ChatID != key.chatID {
		t.answerCallbackQuery(queryID, "此任務不屬於目前對話")
		return
	}
	projectDir := t.getAgent(key).ProjectDir()
	if !hermesTaskMatchesSelectableScope(task, key.threadID, projectDir) {
		t.answerCallbackQuery(queryID, "此任務不屬於目前 Topic")
		return
	}

	if !hermesTaskMatchesProject(task, projectDir) {
		t.answerCallbackQuery(queryID, "此任務不屬於目前專案")
		return
	}
	if !hermesTaskIsContinuable(task) {
		t.answerCallbackQuery(queryID, "此 Hermes 任務目前不可接續")
		return
	}

	t.answerCallbackQuery(queryID, "準備"+hermesContinuationVerb(mode))
	t.send(key, fmt.Sprintf("🔁 將根據任務 %s 的既有進度%s…", shortHermesTaskID(task.ID), hermesContinuationVerb(mode)))
	go t.runTrackedJob("hermes.callback", func() {
		t.startHermesContinuationTask(key, task, projectDir, mode)
	})
}

// answerCallbackQuery answers callback query to remove loading indicator
func (t *TelegramBot) answerCallbackQuery(queryID, text string) {
	params := map[string]interface{}{
		"callback_query_id": queryID,
		"text":              text,
	}
	t.sendTelegram("answerCallbackQuery", params)
}

// sendMessageWithStopButton sends a message with a stop button for ongoing operations
func (t *TelegramBot) sendMessageWithStopButton(key chatKey, text string) (int, error) {
	// Clean invalid UTF-8 characters to prevent API errors
	cleanText := sanitizeUTF8(text)

	abortText := t.getLocalizedMessage(key.chatID, "button_abort", nil)
	keyboard := map[string]interface{}{
		"inline_keyboard": [][]map[string]interface{}{
			{
				{
					"text":          abortText,
					"callback_data": fmt.Sprintf("stop_agent_%d_%d", key.chatID, key.threadID),
				},
			},
		},
	}

	params := map[string]interface{}{
		"chat_id":      key.chatID,
		"text":         cleanText,
		"reply_markup": keyboard,
	}

	if key.threadID != 0 {
		params["message_thread_id"] = key.threadID
	}

	// Send message synchronously to get message ID
	response, err := t.sendMessageSync("sendMessage", params)
	if err != nil {
		return 0, err
	}

	// Extract message ID from response
	if result, ok := response["result"].(map[string]interface{}); ok {
		if messageID, ok := result["message_id"].(float64); ok {
			return int(messageID), nil
		}
	}

	return 0, fmt.Errorf("failed to extract message_id from response")
}

// Close gracefully shuts down the TelegramBot and its message queue
func (t *TelegramBot) Close() {
	if t.queueCancel != nil {
		log.Printf("[telegram] Shutting down message queue...")
		t.queueCancel()
	}
}

// editMessageRemoveStopButton removes the stop button from a message.
// Uses synchronous send to guarantee delivery (async queue can drop messages under load).
func (t *TelegramBot) editMessageRemoveStopButton(key chatKey, messageID int, newText string) {
	params := map[string]interface{}{
		"chat_id":    key.chatID,
		"message_id": messageID,
		"text":       newText,
	}

	if key.threadID != 0 {
		params["message_thread_id"] = key.threadID
	}

	if _, err := t.sendMessageSync("editMessageText", params); err != nil {
		log.Printf("[telegram] editMessageRemoveStopButton error (chat=%d, msg=%d): %v", key.chatID, messageID, err)
	}
}

// runAgentWithStopButton runs agent.Run() with a stop button on the first status update,
// and cleans up the stop button after completion. Used by multimedia handlers (photo/voice/document).
func (t *TelegramBot) runAgentWithStopButton(key chatKey, agent *Agent, prompt string) (string, error) {
	var statusMessageID int
	var firstUpdate = true

	updateCallback := func(update string, silent bool) {
		if firstUpdate && !silent {
			if msgID, msgErr := t.sendMessageWithStopButton(key, update); msgErr == nil {
				statusMessageID = msgID
			} else {
				t.send(key, update)
			}
			firstUpdate = false
		} else {
			if silent {
				t.sendSilent(key, update)
			} else {
				t.send(key, update)
			}
		}
	}
	result, err := appengine.NewDirectEngine(agent).Run(context.Background(), prompt, agent.chatContext, newTelegramProgressSink(updateCallback))
	response := result.Text

	// Remove stop button after completion
	if statusMessageID != 0 {
		var finalText string
		if err != nil {
			if strings.Contains(err.Error(), "agent aborted by user") {
				finalText = t.getLocalizedMessage(key.chatID, "execution_aborted", nil)
			} else if response != "" {
				finalText = t.getLocalizedMessage(key.chatID, "execution_partial", nil)
			} else {
				finalText = t.getLocalizedMessage(key.chatID, "execution_error", nil)
			}
		} else {
			finalText = t.getLocalizedMessage(key.chatID, "execution_completed", nil)
		}
		t.editMessageRemoveStopButton(key, statusMessageID, finalText)
	}

	return response, err
}

// sendTelegram sends JSON data to Telegram API via the message queue
func (t *TelegramBot) sendTelegram(method string, params map[string]interface{}) {
	// Queue the message instead of sending directly
	t.queueMessage(method, params)
}

// sendMediaFile 通用媒體發送方法
// 支持 photo, video, document 類型
// caption: 檔案說明（支持多國語言模板變數）
func (t *TelegramBot) sendMediaFile(key chatKey, filePath, mediaType, caption string) error {
	// 驗證檔案存在
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file not found: %s", filePath)
		}
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// 驗證檔案大小
	maxSizeBytes := int64(t.config.Multimedia.MaxFileSizeMB) * 1024 * 1024
	if fileInfo.Size() > maxSizeBytes {
		return fmt.Errorf("file too large: %d bytes > %d MB limit", fileInfo.Size(), t.config.Multimedia.MaxFileSizeMB)
	}

	// 打開檔案
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// 根據媒體類型選擇 API 方法
	var apiMethod string
	var formFieldName string
	switch mediaType {
	case "photo":
		apiMethod = "sendPhoto"
		formFieldName = "photo"
	case "video":
		apiMethod = "sendVideo"
		formFieldName = "video"
	case "document", "file":
		apiMethod = "sendDocument"
		formFieldName = "document"
	default:
		return fmt.Errorf("unsupported media type: %s", mediaType)
	}

	// 構建 multipart form
	var b bytes.Buffer
	writer := multipart.NewWriter(&b)

	// 添加媒體檔案
	fw, err := writer.CreateFormFile(formFieldName, filepath.Base(filePath))
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}
	_, err = io.Copy(fw, file)
	if err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	// 添加必要的參數
	writer.WriteField("chat_id", fmt.Sprintf("%d", key.chatID))
	if key.threadID != 0 {
		writer.WriteField("message_thread_id", fmt.Sprintf("%d", key.threadID))
	}

	// 添加 caption（如果有）
	if caption != "" {
		writer.WriteField("caption", caption)
		writer.WriteField("parse_mode", "HTML")
	}

	// 必須在發送前關閉 writer，以便寫入 final boundary
	contentType := writer.FormDataContentType()
	writer.Close()

	log.Printf("[telegram] sendMediaFile request: method=%s, chat_id=%d, threadID=%d, form_size=%d bytes",
		apiMethod, key.chatID, key.threadID, b.Len())

	// 發送 API 請求
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/%s", t.config.TelegramToken, apiMethod)
	req, err := http.NewRequest("POST", apiURL, &b)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", contentType)
	req.ContentLength = int64(b.Len())

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	log.Printf("[telegram] sendMediaFile response: status=%d, response_len=%d, body='%s'",
		resp.StatusCode, len(body), string(body))

	if resp.StatusCode != http.StatusOK {
		log.Printf("[telegram] ❌ API error: status=%d, body=%s", resp.StatusCode, string(body))
		return fmt.Errorf("Telegram API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// sendPhoto 發送圖片
func (t *TelegramBot) sendPhoto(key chatKey, filePath, caption string) error {
	return t.sendMediaFile(key, filePath, "photo", caption)
}

// sendDocument 發送文件
func (t *TelegramBot) sendDocument(key chatKey, filePath, caption string) error {
	return t.sendMediaFile(key, filePath, "document", caption)
}

// sendVideo 發送影片
func (t *TelegramBot) sendVideo(key chatKey, filePath, caption string) error {
	return t.sendMediaFile(key, filePath, "video", caption)
}

// processAgentResponse 解析 Agent 回應中的 [SEND_FILE:path] 標記並發送對應檔案。
// 回傳移除標記後的純文字回應。
// Agent 可在回應中包含 [SEND_FILE:temp/output.png] 來觸發自動上傳。
func (t *TelegramBot) processAgentResponse(key chatKey, response string, projectPath string) string {
	const markerPrefix = "[SEND_FILE:"
	const markerSuffix = "]"

	result := response
	for {
		start := strings.Index(result, markerPrefix)
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], markerSuffix)
		if end == -1 {
			break
		}
		end = start + end + len(markerSuffix)

		marker := result[start:end]
		filePath := strings.TrimSpace(marker[len(markerPrefix) : len(marker)-len(markerSuffix)])

		if isPathAllowed(filePath, projectPath) {
			mediaType := inferMediaType(filePath)
			if err := t.sendMediaFile(key, filePath, mediaType, ""); err != nil {
				log.Printf("[telegram] processAgentResponse: failed to send file %q: %v", filePath, err)
				errMsg := t.getLocalizedMessage(key.chatID, "send_file_error", map[string]string{"{error}": err.Error()})
				t.send(key, errMsg)
			} else {
				log.Printf("[telegram] processAgentResponse: sent file %q as %s", filePath, mediaType)
			}
		} else {
			log.Printf("[telegram] processAgentResponse: rejected path %q (not in allowed dirs)", filePath)
		}

		// 移除標記（含前後空白行）
		result = strings.TrimSpace(result[:start]) + "\n" + strings.TrimSpace(result[end:])
		result = strings.TrimSpace(result)
	}
	return result
}

// isPathAllowed 檢查路徑是否被允許發送
// 規則: 只允許相對路徑（temp/, web/, project 目錄），禁止絕對路徑和 ../ 遍歷
func isPathAllowed(filePath, projectPath string) bool {
	// 規則 1: 禁止絕對路徑
	if filepath.IsAbs(filePath) {
		return false
	}

	// 規則 2: 禁止 ../ 路徑遍歷
	if strings.Contains(filePath, "..") {
		return false
	}

	// 規則 3: 只允許特定目錄的相對路徑
	normalizedPath := filepath.Clean(filePath)
	allowedPrefixes := []string{"temp/", "web/", "frontend/"}

	// 檢查是否符合允許的前綴
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(normalizedPath, prefix) {
			return true
		}
	}

	// 如果有 project path，也允許 project 目錄下的檔案
	if projectPath != "" {
		if strings.HasPrefix(normalizedPath, projectPath) {
			return true
		}
	}

	return false
}

// inferMediaType 推斷媒體類型
func inferMediaType(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))

	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
		return "photo"
	case ".mp4", ".avi", ".mov", ".mkv", ".flv", ".wmv", ".webm":
		return "video"
	case ".mp3", ".wav", ".ogg", ".m4a", ".aac", ".flac":
		return "audio"
	default:
		return "document"
	}
}

// handleSendFile 處理 /send-file 命令
func (t *TelegramBot) handleSendFile(key chatKey, filePath string) {
	if filePath == "" {
		msgKey := "send_file_usage"
		msg := t.getLocalizedMessage(key.chatID, msgKey, nil)
		t.send(key, msg)
		return
	}

	// 路徑安全檢查
	agent := t.getAgent(key)
	projectPath := agent.ProjectDir()

	if !isPathAllowed(filePath, projectPath) {
		msgKey := "send_file_forbidden_path"
		msg := t.getLocalizedMessage(key.chatID, msgKey, nil)
		t.send(key, msg)
		return
	}

	// 推斷媒體類型
	mediaType := inferMediaType(filePath)

	// 發送檔案
	if err := t.sendMediaFile(key, filePath, mediaType, ""); err != nil {
		msgKey := "send_file_error"
		msg := t.getLocalizedMessage(key.chatID, msgKey, map[string]string{
			"{error}": err.Error(),
		})
		t.send(key, msg)
		return
	}

	// 成功回報
	msgKey := "send_file_success"
	msg := t.getLocalizedMessage(key.chatID, msgKey, nil)
	t.send(key, msg)
}

// testPhotoUpload 測試命令：直接從本地上傳照片到聊天
func (t *TelegramBot) testPhotoUpload(key chatKey) {
	photoPath := "temp/media/photo_from_downloads.png"

	// 驗證文件存在
	fileInfo, err := os.Stat(photoPath)
	if err != nil {
		t.send(key, fmt.Sprintf("❌ 照片文件不存在: %s", photoPath))
		return
	}

	// 發送照片
	if err := t.sendPhoto(key, photoPath, ""); err != nil {
		t.send(key, fmt.Sprintf("❌ 上傳失敗: %v", err))
		log.Printf("[telegram] Photo upload failed: %v", err)
		return
	}

	// 發送成功確認
	fileSize := formatFileSize(int64(fileInfo.Size()))
	confirmMsg := fmt.Sprintf("✅ 照片已上傳！\n📁 文件: %s\n💾 大小: %s\n🎨 解像度: 1170x2532", photoPath, fileSize)
	t.send(key, confirmMsg)
	log.Printf("[telegram] Test photo uploaded successfully: %s (%s)", photoPath, fileSize)
}

type tasksGitHubIssue struct {
	Number    int
	Title     string
	Labels    []string
	Milestone string
	URL       string
}

type ghTasksIssueJSON struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Milestone *struct {
		Title string `json:"title"`
	} `json:"milestone"`
	URL string `json:"url"`
}

func taskLocalizedText(lang, zhTW, en string) string {
	if lang == "zh-TW" {
		return zhTW
	}
	return en
}

func tasksActionLabels(lang, state string) (refreshText, toggleText, openGitHubText string) {
	refreshText = taskLocalizedText(lang, "🔄 重新整理", "🔄 Refresh")
	openGitHubText = taskLocalizedText(lang, "🌐 在 GitHub 開啟", "🌐 Open in GitHub")
	if state == "closed" {
		toggleText = taskLocalizedText(lang, "📋 開放", "📋 Open")
	} else {
		toggleText = taskLocalizedText(lang, "📋 已關閉", "📋 Closed")
	}
	return refreshText, toggleText, openGitHubText
}

func tasksStatusMessage(lang, state string) string {
	switch state {
	case "closed":
		return taskLocalizedText(lang, "📋 顯示已關閉 Issues", "📋 Showing closed issues")
	default:
		return taskLocalizedText(lang, "📋 顯示開放 Issues", "📋 Showing open issues")
	}
}

func tasksAuthRequiredMessage(lang string) string {
	return taskLocalizedText(lang,
		"❌ 目前無法查詢 GitHub Issues。\n\n請先執行 `gh auth login` 完成認證後再試一次。",
		"❌ Unable to query GitHub Issues right now.\n\nPlease run `gh auth login` and try again.")
}

func tasksNoRepoMessage(lang string) string {
	return taskLocalizedText(lang,
		"⚠️ 這個 Topic 綁定的專案不是可用的 GitHub repository。\n\n請先在具體 project topic 使用 `/project` 綁定，或檢查 `gh` / git remote 設定。",
		"⚠️ The project bound to this topic is not a usable GitHub repository.\n\nPlease bind a project with `/project` in a concrete project topic, or check your `gh` / git remote settings.")
}

func tasksNoMilestoneLabel(lang string) string {
	return taskLocalizedText(lang, "未指定 Milestone", "No milestone")
}

func tasksLabelsLabel(lang string) string {
	return taskLocalizedText(lang, "標籤", "Labels")
}

func tasksMilestoneLabel(lang string) string {
	return taskLocalizedText(lang, "Milestone", "Milestone")
}

func tasksDisplayIssueCount(lang string, count int) string {
	return taskLocalizedText(lang,
		fmt.Sprintf("共 %d 筆，僅顯示前 20 筆", count),
		fmt.Sprintf("%d issues, showing up to 20", count))
}

func normalizeGitHubRepoURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	rawURL = strings.TrimSuffix(rawURL, ".git")
	if rawURL == "" {
		return ""
	}

	if strings.HasPrefix(rawURL, "git@") || strings.HasPrefix(rawURL, "ssh://") {
		if strings.HasPrefix(rawURL, "ssh://") {
			u, err := url.Parse(rawURL)
			if err == nil && u.Host != "" && u.Path != "" {
				return fmt.Sprintf("https://%s%s", u.Host, strings.TrimSuffix(u.Path, "/"))
			}
		}

		atIdx := strings.Index(rawURL, "@")
		colonIdx := strings.Index(rawURL, ":")
		if atIdx >= 0 && colonIdx > atIdx {
			host := rawURL[atIdx+1 : colonIdx]
			path := strings.TrimPrefix(rawURL[colonIdx+1:], "/")
			path = strings.Trim(path, "/")
			if host != "" && path != "" {
				return fmt.Sprintf("https://%s/%s", host, path)
			}
		}
	}

	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		u, err := url.Parse(rawURL)
		if err == nil && u.Host != "" {
			return fmt.Sprintf("https://%s%s", u.Host, strings.TrimSuffix(u.Path, "/"))
		}
	}

	return ""
}

func repoURLFromIssueURL(issueURL string) string {
	issueURL = strings.TrimSpace(issueURL)
	if issueURL == "" {
		return ""
	}
	if idx := strings.Index(issueURL, "/issues/"); idx > 0 {
		return strings.TrimSuffix(issueURL[:idx], "/")
	}
	return ""
}

func resolveGitHubRepoURL(projectDir string) (string, error) {
	tryRemote := func(remote string) string {
		return normalizeGitHubRepoURL(remote)
	}

	output, err := runProcessOutput(context.Background(), ProcessOptions{Dir: projectDir}, "git", "remote", "get-url", "origin")
	if err == nil {
		if repoURL := tryRemote(string(output)); repoURL != "" {
			return repoURL, nil
		}
	}

	output, err = runProcessOutput(context.Background(), ProcessOptions{Dir: projectDir}, "git", "remote")
	if err != nil {
		return "", fmt.Errorf("no remotes found: %w", err)
	}

	remotes := strings.Fields(strings.TrimSpace(string(output)))
	if len(remotes) == 0 {
		return "", fmt.Errorf("no remotes configured")
	}

	for _, remote := range remotes {
		output, err = runProcessOutput(context.Background(), ProcessOptions{Dir: projectDir}, "git", "remote", "get-url", remote)
		if err != nil {
			continue
		}
		if repoURL := tryRemote(string(output)); repoURL != "" {
			return repoURL, nil
		}
	}

	return "", fmt.Errorf("no GitHub remote found")
}

func listGitHubIssuesForTasks(ctx context.Context, projectDir, state string, limit int) ([]tasksGitHubIssue, error) {
	if state != "open" && state != "closed" {
		return nil, fmt.Errorf("unsupported issue state: %s", state)
	}
	if limit <= 0 {
		limit = 20
	}

	output, err := runProcessCombinedOutput(ctx, ProcessOptions{Dir: projectDir}, "gh", "issue", "list",
		"--state", state,
		"--limit", strconv.Itoa(limit),
		"--json", "number,title,labels,milestone,url",
	)
	if err != nil {
		lower := strings.ToLower(string(output) + " " + err.Error())
		switch {
		case strings.Contains(lower, "authentication required"),
			strings.Contains(lower, "not logged in"),
			strings.Contains(lower, "gh auth login"),
			strings.Contains(lower, "must authenticate"),
			strings.Contains(lower, "401 unauthorized"),
			strings.Contains(lower, "authentication failed"):
			return nil, errTasksGitHubAuthRequired
		case strings.Contains(lower, "could not resolve to a repository"),
			strings.Contains(lower, "not a git repository"),
			strings.Contains(lower, "no remotes configured"),
			strings.Contains(lower, "no remotes found"),
			strings.Contains(lower, "no github remote found"):
			return nil, errTasksNoGitHubRepo
		default:
			return nil, fmt.Errorf("gh issue list: %w (%s)", err, strings.TrimSpace(string(output)))
		}
	}

	var raw []ghTasksIssueJSON
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("parse issue list JSON: %w", err)
	}

	issues := make([]tasksGitHubIssue, 0, len(raw))
	for _, item := range raw {
		labels := make([]string, 0, len(item.Labels))
		for _, label := range item.Labels {
			if label.Name != "" {
				labels = append(labels, label.Name)
			}
		}

		milestone := ""
		if item.Milestone != nil {
			milestone = strings.TrimSpace(item.Milestone.Title)
		}

		issues = append(issues, tasksGitHubIssue{
			Number:    item.Number,
			Title:     strings.TrimSpace(item.Title),
			Labels:    labels,
			Milestone: milestone,
			URL:       strings.TrimSpace(item.URL),
		})
	}

	return issues, nil
}

func readLegacyTasksOverview(projectDir string) (string, error) {
	tasksFile := filepath.Join(projectDir, "docs", "MASTER_TASKS.md")
	data, err := os.ReadFile(tasksFile)
	if err != nil {
		return "", err
	}

	content := string(data)
	if idx := strings.Index(content, "## Phase Overview"); idx >= 0 {
		rest := content[idx:]
		endIdx := strings.Index(rest[20:], "\n## ")
		if endIdx < 0 {
			endIdx = strings.Index(rest[20:], "\n---")
		}
		if endIdx < 0 {
			endIdx = len(rest)
		} else {
			endIdx += 20
		}
		return rest[:endIdx], nil
	}

	return "", fmt.Errorf("MASTER_TASKS.md format is invalid")
}

func (t *TelegramBot) sendTasksMessage(key chatKey, projectDir, state string) error {
	lang := t.getChatLanguage(key.chatID)

	repoURL, repoErr := tasksGitHubRepoURLFunc(projectDir)
	if repoErr != nil {
		legacy, legacyErr := readLegacyTasksOverview(projectDir)
		if legacyErr == nil {
			t.send(key, legacy)
			return nil
		}
		return errTasksNoGitHubRepo
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	issues, err := tasksGitHubIssueListFunc(ctx, projectDir, state, 20)
	if err != nil {
		switch {
		case errors.Is(err, errTasksGitHubAuthRequired):
			t.send(key, tasksAuthRequiredMessage(lang))
			return err
		case errors.Is(err, errTasksNoGitHubRepo):
			legacy, legacyErr := readLegacyTasksOverview(projectDir)
			if legacyErr == nil {
				t.send(key, legacy)
				return nil
			}
			t.send(key, tasksNoRepoMessage(lang))
			return errTasksNoGitHubRepo
		default:
			legacy, legacyErr := readLegacyTasksOverview(projectDir)
			if legacyErr == nil {
				t.send(key, legacy)
				return nil
			}
			return err
		}
	}

	if repoURL == "" && len(issues) > 0 {
		repoURL = repoURLFromIssueURL(issues[0].URL)
	}

	text := t.buildTasksIssueMessage(key.chatID, state, issues)
	keyboard := t.buildTasksKeyboard(key.chatID, repoURL, state)
	msg := map[string]interface{}{
		"chat_id":      strconv.FormatInt(key.chatID, 10),
		"text":         sanitizeUTF8(text),
		"reply_markup": keyboard,
	}
	if key.threadID != 0 {
		msg["message_thread_id"] = strconv.Itoa(key.threadID)
	}
	t.sendTelegram("sendMessage", msg)
	return nil
}

func (t *TelegramBot) resolveTasksProjectDir(key chatKey) (string, error) {
	if key.threadID == 0 {
		return "", errTasksNoGitHubRepo
	}

	if globalStorage != nil {
		if saved, err := globalStorage.GetTopicSetting(key.chatID, key.threadID); err == nil && saved != "" {
			return saved, nil
		}
		return "", errTasksNoGitHubRepo
	}

	projectDir := t.getAgent(key).ProjectDir()
	if projectDir == "" {
		return "", errTasksNoGitHubRepo
	}
	return projectDir, nil
}

func (t *TelegramBot) buildTasksKeyboard(chatID int64, repoURL, state string) map[string]interface{} {
	lang := t.getChatLanguage(chatID)
	refreshText, toggleText, openGitHubText := tasksActionLabels(lang, state)
	rows := [][]map[string]interface{}{
		{
			{
				"text":          refreshText,
				"callback_data": fmt.Sprintf("tasks:refresh:%s", state),
			},
			{
				"text": toggleText,
				"callback_data": fmt.Sprintf("tasks:view:%s", func() string {
					if state == "closed" {
						return "open"
					}
					return "closed"
				}()),
			},
		},
	}
	if repoURL != "" {
		rows = append(rows, []map[string]interface{}{
			{
				"text": openGitHubText,
				"url":  fmt.Sprintf("%s/issues", strings.TrimSuffix(repoURL, "/")),
			},
		})
	}
	return map[string]interface{}{"inline_keyboard": rows}
}

func (t *TelegramBot) buildTasksIssueMessage(chatID int64, state string, issues []tasksGitHubIssue) string {
	lang := t.getChatLanguage(chatID)
	title := t.getLocalizedMessage(chatID, "tasks_command_title", nil)
	if title == "tasks_command_title" {
		title = taskLocalizedText(lang, "📋 Alice 待辦工作清單", "📋 Alice Task List")
	}
	noIssues := t.getLocalizedMessage(chatID, "tasks_empty", nil)
	if noIssues == "tasks_empty" {
		noIssues = taskLocalizedText(lang, "目前沒有符合條件的 GitHub Issues", "No GitHub Issues match this filter")
	}

	var sb strings.Builder
	sb.WriteString(title)
	sb.WriteString("\n")
	sb.WriteString(tasksStatusMessage(lang, state))
	sb.WriteString("\n")
	sb.WriteString(tasksDisplayIssueCount(lang, len(issues)))

	if len(issues) == 0 {
		sb.WriteString("\n\n")
		sb.WriteString(noIssues)
		return sb.String()
	}

	groupOrder := make([]string, 0)
	grouped := make(map[string][]tasksGitHubIssue)
	for _, issue := range issues {
		groupName := issue.Milestone
		if groupName == "" {
			groupName = tasksNoMilestoneLabel(lang)
		}
		if _, ok := grouped[groupName]; !ok {
			groupOrder = append(groupOrder, groupName)
		}
		grouped[groupName] = append(grouped[groupName], issue)
	}

	sb.WriteString("\n\n")
	for idx, milestone := range groupOrder {
		if idx > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("📌 ")
		sb.WriteString(tasksMilestoneLabel(lang))
		sb.WriteString(": ")
		sb.WriteString(milestone)
		sb.WriteString("\n")
		for _, issue := range grouped[milestone] {
			sb.WriteString(fmt.Sprintf("- #%d %s\n", issue.Number, issue.Title))
			labelText := taskLocalizedText(lang, "無", "None")
			if len(issue.Labels) > 0 {
				labelText = strings.Join(issue.Labels, ", ")
			}
			sb.WriteString(fmt.Sprintf("  %s: %s\n", tasksLabelsLabel(lang), labelText))
		}
	}

	return strings.TrimSpace(sb.String())
}

// handleTasks 處理 /tasks 命令，顯示目前 topic 綁定專案的 GitHub Issues
func (t *TelegramBot) handleTasks(key chatKey) {
	projectDir, err := t.resolveTasksProjectDir(key)
	if err != nil {
		t.send(key, tasksNoRepoMessage(t.getChatLanguage(key.chatID)))
		return
	}

	if err := t.sendTasksMessage(key, projectDir, "open"); err != nil {
		if errors.Is(err, errTasksGitHubAuthRequired) {
			return
		}
		if errors.Is(err, errTasksNoGitHubRepo) {
			t.send(key, tasksNoRepoMessage(t.getChatLanguage(key.chatID)))
			return
		}
		log.Printf("[telegram] handleTasks failed: %v", err)
		errMsg := t.getLocalizedMessage(key.chatID, "tasks_read_failed", map[string]string{"error": err.Error()})
		if errMsg == "tasks_read_failed" {
			errMsg = taskLocalizedText(t.getChatLanguage(key.chatID),
				fmt.Sprintf("❌ 無法讀取任務清單\n\n錯誤: %s", err.Error()),
				fmt.Sprintf("❌ Failed to read task list\n\nError: %s", err.Error()))
		}
		t.send(key, errMsg)
	}
}

// handlePhotoMessageBatch 處理圖片訊息，支援多張圖片批次處理
func (t *TelegramBot) handlePhotoMessageBatch(key chatKey, userID int64, photo []PhotoSize, caption string, mediaGroupID string, messageID int) {
	// 檢查多媒體支援是否開啟
	if !t.config.Multimedia.EnablePhotoSupport {
		t.send(key, t.getLocalizedMessage(key.chatID, "photo_disabled", nil))
		return
	}

	// 如果沒有 mediaGroupID，這是單張圖片的多個尺寸，直接處理單張圖片
	if mediaGroupID == "" {
		log.Printf("[telegram] single photo with %d size variants, processing as single image", len(photo))
		t.handleSinglePhoto(key, userID, photo, caption, messageID)
		return
	}

	// 有 mediaGroupID，這是真正的多張圖片批次
	now := time.Now()
	batchKey := mediaGroupID

	t.batchMu.Lock()
	defer t.batchMu.Unlock()

	// 檢查是否已有相同批次
	batch, exists := t.mediaBatches[batchKey]
	if !exists {
		// 創建新的批次
		batch = &MediaBatch{
			Photos:       make([]PhotoSize, 0),
			Caption:      caption,
			MediaGroupID: mediaGroupID,
			UserID:       userID,
			ChatKey:      key,
			MessageID:    messageID,
			FirstSeen:    now,
			LastSeen:     now,
		}
		t.mediaBatches[batchKey] = batch
		log.Printf("[telegram] created new media batch: %s", batchKey)
	} else {
		// 更新現有批次
		batch.LastSeen = now
		if caption != "" {
			batch.Caption = caption // 使用最新的 caption
		}
		log.Printf("[telegram] updated existing media batch: %s", batchKey)
	}

	// 將圖片加入批次（只取最高解析度的一張）
	if len(photo) > 0 {
		// 取最高解析度的圖片（通常是陣列最後一個）
		highestRes := photo[len(photo)-1]
		batch.Photos = append(batch.Photos, highestRes)
	}

	// 取消現有的計時器
	if batch.timer != nil {
		batch.timer.Stop()
	}

	// 設定新的處理計時器
	batch.timer = time.AfterFunc(t.batchTimeout, func() {
		t.processBatch(batchKey)
	})

	log.Printf("[telegram] added 1 photo (from %d size variants) to batch %s, total photos: %d",
		len(photo), batchKey, len(batch.Photos))
}

// processBatch 處理完整的媒體批次
func (t *TelegramBot) processBatch(batchKey string) {
	t.batchMu.Lock()
	batch, exists := t.mediaBatches[batchKey]
	if !exists {
		t.batchMu.Unlock()
		return
	}
	// 從 map 中移除批次（避免重複處理）
	delete(t.mediaBatches, batchKey)
	t.batchMu.Unlock()

	log.Printf("[telegram] processing media batch %s with %d photos", batchKey, len(batch.Photos))

	if len(batch.Photos) == 1 {
		// 單張圖片，使用原有邏輯
		t.send(batch.ChatKey, t.getLocalizedMessage(batch.ChatKey.chatID, "photo_analyzing_single", nil))
		t.handleSinglePhoto(batch.ChatKey, batch.UserID, []PhotoSize{batch.Photos[0]}, batch.Caption, batch.MessageID)
	} else {
		// 多張圖片，批次處理
		msg := t.getLocalizedMessage(batch.ChatKey.chatID, "photo_analyzing_batch", nil)
		msg = strings.ReplaceAll(msg, "{count}", fmt.Sprintf("%d", len(batch.Photos)))
		t.send(batch.ChatKey, msg)
		t.handleMultiplePhotos(batch.ChatKey, batch.UserID, batch.Photos, batch.Caption, batch.MessageID)
	}
}

// handleMultiplePhotos 處理多張圖片的批次分析
func (t *TelegramBot) handleMultiplePhotos(key chatKey, userID int64, photos []PhotoSize, caption string, messageID int) {
	// 取得 Agent 和專案目錄
	agent := t.getAgent(key)
	projectDir := agent.ProjectDir()

	// 確保專案臨時目錄存在
	projectTempDir := filepath.Join(projectDir, "temp")
	if err := os.MkdirAll(projectTempDir, 0755); err != nil {
		log.Printf("[telegram] create project temp dir error: %v", err)
		t.send(key, t.getLocalizedMessage(key.chatID, "photo_mkdir_failed", nil))
		return
	}

	// 下載並準備所有圖片
	aliceImagePaths := make([]string, 0, len(photos))    // Alice 臨時目錄
	projectImagePaths := make([]string, 0, len(photos))  // 專案臨時目錄
	relativeImagePaths := make([]string, 0, len(photos)) // 相對路徑

	defer func() {
		// 清理所有臨時檔案
		for _, path := range aliceImagePaths {
			if err := os.Remove(path); err != nil {
				log.Printf("[telegram] cleanup alice batch photo error: %v", err)
			}
		}
		for _, path := range projectImagePaths {
			if err := os.Remove(path); err != nil {
				log.Printf("[telegram] cleanup project batch photo error: %v", err)
			}
		}
	}()

	// 下載所有圖片並複製到專案目錄
	for i, photoSize := range photos {
		// 圖片已經是 PhotoSize 結構，直接使用
		targetPhoto := photoSize

		// 檢查檔案大小
		maxSizeBytes := t.config.Multimedia.MaxFileSizeMB * 1024 * 1024
		if targetPhoto.FileSize > maxSizeBytes {
			msg := t.getLocalizedMessage(key.chatID, "photo_file_too_large", nil)
			msg = strings.ReplaceAll(msg, "{index}", fmt.Sprintf("%d", i+1))
			msg = strings.ReplaceAll(msg, "{size}", formatFileSize(int64(targetPhoto.FileSize)))
			msg = strings.ReplaceAll(msg, "{limit}", fmt.Sprintf("%d", t.config.Multimedia.MaxFileSizeMB))
			t.send(key, msg)
			continue
		}

		// 下載到 Alice 臨時目錄
		aliceImagePath, err := t.DownloadTelegramFile(targetPhoto.FileID, "photo")
		if err != nil {
			log.Printf("[telegram] download photo %d error: %v", i+1, err)
			msg := t.getLocalizedMessage(key.chatID, "photo_download_failed", nil)
			msg = strings.ReplaceAll(msg, "{index}", fmt.Sprintf("%d", i+1))
			t.send(key, msg)
			continue
		}
		aliceImagePaths = append(aliceImagePaths, aliceImagePath)

		// 複製到專案臨時目錄
		fileName := filepath.Base(aliceImagePath)
		projectImagePath := filepath.Join(projectTempDir, fileName)
		if err := copyFile(aliceImagePath, projectImagePath); err != nil {
			log.Printf("[telegram] copy photo %d to project error: %v", i+1, err)
			msg := t.getLocalizedMessage(key.chatID, "photo_copy_failed", nil)
			msg = strings.ReplaceAll(msg, "{index}", fmt.Sprintf("%d", i+1))
			t.send(key, msg)
			continue
		}
		projectImagePaths = append(projectImagePaths, projectImagePath)

		// 記錄相對路徑
		relativePath := filepath.Join("temp", fileName)
		relativeImagePaths = append(relativeImagePaths, relativePath)
	}

	if len(relativeImagePaths) == 0 {
		msg := t.getLocalizedMessage(key.chatID, "photo_all_failed", nil)
		t.send(key, msg)
		return
	}

	// 組合多張圖片的 prompt，caption 為主指令時優先
	imageList := ""
	for i, relativePath := range relativeImagePaths {
		item := t.getLocalizedMessage(key.chatID, "photo_list_item", nil)
		item = strings.ReplaceAll(item, "{index}", fmt.Sprintf("%d", i+1))
		item = strings.ReplaceAll(item, "{path}", relativePath)
		imageList += item
	}

	var prompt string
	if caption != "" {
		refBatch := t.getLocalizedMessage(key.chatID, "photo_reference_batch", nil)
		refBatch = strings.ReplaceAll(refBatch, "{count}", fmt.Sprintf("%d", len(relativeImagePaths)))
		refBatch = strings.ReplaceAll(refBatch, "{list}", imageList)
		prompt = fmt.Sprintf("%s%s", caption, refBatch)
	} else {
		analyzePrompt := t.getLocalizedMessage(key.chatID, "photo_analyze_batch_prompt", nil)
		analyzePrompt = strings.ReplaceAll(analyzePrompt, "{count}", fmt.Sprintf("%d", len(relativeImagePaths)))
		analyzePrompt = strings.ReplaceAll(analyzePrompt, "{list}", imageList)
		prompt = analyzePrompt
	}

	// 安全檢查和事件記錄
	if security.Global() != nil {
		security.Global().LogSecurityEvent(security.SecurityEvent{
			EventType:   "telegram_batch_photos_received",
			Severity:    "medium",
			Description: fmt.Sprintf("Batch of %d photos received via Telegram", len(relativeImagePaths)),
			UserID:      userID,
			Details: map[string]interface{}{
				"photo_count": len(relativeImagePaths),
				"has_caption": caption != "",
				"caption_len": len(caption),
				"chat_id":     key.chatID,
			},
		})

		// PII 檢測 caption (自動記錄事件)
		if caption != "" {
			filteredCaption, detected := security.Global().DetectAndFilterPII(caption, true, &security.PIIDetectionContext{
				ChatID:      key.chatID,
				UserID:      userID,
				MessageType: "photo",
				SourceType:  "telegram",
				ProjectPath: t.config.DefaultProjectDir,
				MessageID:   messageID,
			})
			if len(detected) > 0 {
				// PII 事件已由 DetectAndFilterPII 自動記錄
				msg := t.getLocalizedMessage(key.chatID, "photo_caption_pii", nil)
				t.send(key, msg)
				caption = filteredCaption
				// 重新組合 prompt
				refBatch := t.getLocalizedMessage(key.chatID, "photo_reference_batch", nil)
				refBatch = strings.ReplaceAll(refBatch, "{count}", fmt.Sprintf("%d", len(relativeImagePaths)))
				refBatch = strings.ReplaceAll(refBatch, "{list}", imageList)
				prompt = fmt.Sprintf("%s%s", caption, refBatch)
			}
		}
	}

	// 發送給 Agent 處理 (使用現有會話，就像語音處理一樣)
	agent = t.getAgent(key)

	// Add language preference hint
	userLang := t.getChatLanguage(key.chatID)
	promptWithLang := prompt
	if userLang == "en" {
		promptWithLang = "Please respond in English. Do NOT use Chinese characters or Chinese formatting in your response.\n\n" + prompt
	} else if userLang == "zh-TW" {
		promptWithLang = "請用繁體中文回應。\n\n" + prompt
	}

	response, err := t.runAgentWithStopButton(key, agent, promptWithLang)

	if err != nil {
		if strings.Contains(err.Error(), "agent aborted by user") {
			return
		}
		log.Printf("[telegram] batch photo analysis error: %v", err)
		msg := t.getLocalizedMessage(key.chatID, "photo_analysis_failed", nil)
		t.send(key, msg)
		return
	}

	if response != "" {
		t.sendLong(key, response)
	}
}

// handleSinglePhoto 處理單張圖片（保留原有邏輯但提取為獨立函數）
func (t *TelegramBot) handleSinglePhoto(key chatKey, userID int64, photo []PhotoSize, caption string, messageID int) {
	// 取得最高解析度的圖片（通常是陣列最後一個）
	if len(photo) == 0 {
		return
	}
	targetPhoto := photo[len(photo)-1]

	// 檢查檔案大小限制
	maxSizeBytes := t.config.Multimedia.MaxFileSizeMB * 1024 * 1024
	if targetPhoto.FileSize > maxSizeBytes {
		msg := t.getLocalizedMessage(key.chatID, "photo_file_too_large", nil)
		msg = strings.ReplaceAll(msg, "{index}", "1")
		msg = strings.ReplaceAll(msg, "{size}", formatFileSize(int64(targetPhoto.FileSize)))
		msg = strings.ReplaceAll(msg, "{limit}", fmt.Sprintf("%d", t.config.Multimedia.MaxFileSizeMB))
		t.send(key, msg)
		return
	}

	// 下載圖片到 Alice 臨時目錄
	aliceImagePath, err := t.DownloadTelegramFile(targetPhoto.FileID, "photo")
	if err != nil {
		log.Printf("[telegram] download photo error: %v", err)
		msg := t.getLocalizedMessage(key.chatID, "photo_download_failed", nil)
		msg = strings.ReplaceAll(msg, "{index}", "1")
		t.send(key, msg)
		return
	}

	// 取得 Agent 和專案目錄
	agent := t.getAgent(key)
	projectDir := agent.ProjectDir()

	// 確保專案臨時目錄存在
	projectTempDir := filepath.Join(projectDir, "temp")
	if err := os.MkdirAll(projectTempDir, 0755); err != nil {
		log.Printf("[telegram] create project temp dir error: %v", err)
		msg := t.getLocalizedMessage(key.chatID, "photo_mkdir_failed", nil)
		t.send(key, msg)
		os.Remove(aliceImagePath) // 清理 Alice 臨時檔案
		return
	}

	// 複製圖片到專案臨時目錄
	fileName := filepath.Base(aliceImagePath)
	projectImagePath := filepath.Join(projectTempDir, fileName)

	if err := copyFile(aliceImagePath, projectImagePath); err != nil {
		log.Printf("[telegram] copy photo to project error: %v", err)
		msg := t.getLocalizedMessage(key.chatID, "photo_copy_failed", nil)
		msg = strings.ReplaceAll(msg, "{index}", "1")
		t.send(key, msg)
		os.Remove(aliceImagePath) // 清理 Alice 臨時檔案
		return
	}

	// 確保在函數結束時清理兩個臨時檔案
	defer func() {
		if err := os.Remove(aliceImagePath); err != nil {
			log.Printf("[telegram] cleanup alice temp photo error: %v", err)
		}
		if err := os.Remove(projectImagePath); err != nil {
			log.Printf("[telegram] cleanup project temp photo error: %v", err)
		}
	}()

	// 使用相對路徑，caption 為主指令時優先
	relativePath := filepath.Join("temp", fileName)
	var prompt string
	if caption != "" {
		refSingle := t.getLocalizedMessage(key.chatID, "photo_reference_single", nil)
		refSingle = strings.ReplaceAll(refSingle, "{path}", relativePath)
		prompt = fmt.Sprintf("%s%s", caption, refSingle)
	} else {
		analyzePrompt := t.getLocalizedMessage(key.chatID, "photo_analyze_single_prompt", nil)
		analyzePrompt = strings.ReplaceAll(analyzePrompt, "{path}", relativePath)
		prompt = analyzePrompt
	}

	// 安全檢查和 PII 檢測（與原有邏輯相同）
	if security.Global() != nil {
		security.Global().LogSecurityEvent(security.SecurityEvent{
			EventType:   "telegram_photo_received",
			Severity:    "medium",
			Description: "Photo message received via Telegram",
			UserID:      userID,
			Details: map[string]interface{}{
				"file_size":   targetPhoto.FileSize,
				"width":       targetPhoto.Width,
				"height":      targetPhoto.Height,
				"has_caption": caption != "",
				"caption_len": len(caption),
				"chat_id":     key.chatID,
			},
		})

		// PII 檢測 caption (自動記錄事件)
		if caption != "" {
			filteredCaption, detected := security.Global().DetectAndFilterPII(caption, true, &security.PIIDetectionContext{
				ChatID:      key.chatID,
				UserID:      userID,
				MessageType: "photo",
				SourceType:  "telegram",
				ProjectPath: t.config.DefaultProjectDir,
				MessageID:   messageID,
			})
			if len(detected) > 0 {
				msg := t.getLocalizedMessage(key.chatID, "photo_caption_pii", nil)
				t.send(key, msg)
				caption = filteredCaption
				refSingle := t.getLocalizedMessage(key.chatID, "photo_reference_single", nil)
				refSingle = strings.ReplaceAll(refSingle, "{path}", relativePath)
				prompt = fmt.Sprintf("%s%s", caption, refSingle)
			}
		}
	}

	// 發送給 Agent 處理 (使用現有會話，就像語音處理一樣)
	agent = t.getAgent(key)
	msg := t.getLocalizedMessage(key.chatID, "photo_analyzing_single", nil)
	t.send(key, msg)

	// Add language preference hint
	userLang := t.getChatLanguage(key.chatID)
	promptWithLang := prompt
	if userLang == "en" {
		promptWithLang = "Please respond in English. Do NOT use Chinese characters or Chinese formatting in your response.\n\n" + prompt
	} else if userLang == "zh-TW" {
		promptWithLang = "請用繁體中文回應。\n\n" + prompt
	}

	response, err := t.runAgentWithStopButton(key, agent, promptWithLang)

	if err != nil {
		if strings.Contains(err.Error(), "agent aborted by user") {
			return
		}
		log.Printf("[telegram] single photo analysis error: %v", err)
		errMsg := t.getLocalizedMessage(key.chatID, "photo_analysis_failed", nil)
		t.send(key, errMsg)
		return
	}

	if response != "" {
		t.sendLong(key, response)
	}
}

// handlePhotoMessage 處理圖片訊息（原有函數，保留用於向後相容）
func (t *TelegramBot) handlePhotoMessage(key chatKey, userID int64, photo []PhotoSize, caption string, messageID int) {
	// 檢查多媒體支援是否開啟
	if !t.config.Multimedia.EnablePhotoSupport {
		t.send(key, t.getLocalizedMessage(key.chatID, "photo_disabled", nil))
		return
	}

	// 取得最高解析度的圖片（通常是陣列最後一個）
	if len(photo) == 0 {
		return
	}
	targetPhoto := photo[len(photo)-1]

	// 檢查檔案大小限制
	maxSizeBytes := t.config.Multimedia.MaxFileSizeMB * 1024 * 1024
	if targetPhoto.FileSize > maxSizeBytes {
		msg := t.getLocalizedMessage(key.chatID, "photo_file_too_large", nil)
		msg = strings.ReplaceAll(msg, "{size}", formatFileSize(int64(targetPhoto.FileSize)))
		msg = strings.ReplaceAll(msg, "{limit}", fmt.Sprintf("%d", t.config.Multimedia.MaxFileSizeMB))
		msg = strings.ReplaceAll(msg, "{index}", "")
		t.send(key, msg)
		return
	}

	// 下載圖片
	imagePath, err := t.DownloadTelegramFile(targetPhoto.FileID, "photo")
	if err != nil {
		log.Printf("[telegram] download photo error: %v", err)
		msg := t.getLocalizedMessage(key.chatID, "photo_download_failed", nil)
		msg = strings.ReplaceAll(msg, "{index}", "")
		t.send(key, msg)
		return
	}

	// 確保在函數結束時清理臨時檔案
	defer func() {
		if err := os.Remove(imagePath); err != nil {
			log.Printf("[telegram] cleanup photo error: %v", err)
		}
	}()

	// 組合 prompt 讓 Claude 分析圖片
	prompt := fmt.Sprintf("Analyze this photo: %s", imagePath)
	if caption != "" {
		prompt = fmt.Sprintf("%s\n\nUser note: %s", prompt, caption)
	}

	// 安全檢查和 PII 檢測
	if security.Global() != nil {
		security.Global().LogSecurityEvent(security.SecurityEvent{
			EventType:   "telegram_photo_received",
			Severity:    "medium",
			Description: "Photo message received via Telegram",
			UserID:      userID,
			Details: map[string]interface{}{
				"file_size":   targetPhoto.FileSize,
				"width":       targetPhoto.Width,
				"height":      targetPhoto.Height,
				"has_caption": caption != "",
				"caption_len": len(caption),
				"chat_id":     key.chatID,
			},
		})

		// PII 檢測 caption (自動記錄事件)
		if caption != "" {
			filteredCaption, detected := security.Global().DetectAndFilterPII(caption, true, &security.PIIDetectionContext{
				ChatID:      key.chatID,
				UserID:      userID,
				MessageType: "photo",
				SourceType:  "telegram",
				ProjectPath: t.config.DefaultProjectDir,
				MessageID:   messageID,
			})
			if len(detected) > 0 {
				piiMsg := t.getLocalizedMessage(key.chatID, "photo_caption_pii", nil)
				t.send(key, piiMsg)
				caption = filteredCaption
				prompt = fmt.Sprintf("Analyze this photo: %s\n\nUser note: %s", imagePath, caption)
			}
		}
	}

	// 發送給 Agent 處理
	agent := t.getAgent(key)
	msg := t.getLocalizedMessage(key.chatID, "photo_analyzing_single", nil)
	t.send(key, msg)

	// Add language preference hint
	userLang := t.getChatLanguage(key.chatID)
	promptWithLang := prompt
	if userLang == "en" {
		promptWithLang = "Please respond in English. Do NOT use Chinese characters or Chinese formatting in your response.\n\n" + prompt
	} else if userLang == "zh-TW" {
		promptWithLang = "請用繁體中文回應。\n\n" + prompt
	}

	response, err := t.runAgentWithStopButton(key, agent, promptWithLang)
	if err != nil {
		if strings.Contains(err.Error(), "agent aborted by user") {
			return
		}
		log.Printf("[telegram] photo analysis error: %v", err)
		msg := t.getLocalizedMessage(key.chatID, "photo_analysis_failed", nil)
		t.send(key, msg)
		return
	}

	if response != "" {
		t.send(key, response)
	}
}

// copyFile 複製檔案
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	// 確保寫入磁碟
	return destFile.Sync()
}

// formatFileSize 格式化檔案大小顯示
func formatFileSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	}
	if bytes < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	}
	return fmt.Sprintf("%.1f GB", float64(bytes)/(1024*1024*1024))
}

// CleanupTempMediaFiles 清理舊的臨時媒體檔案
func CleanupTempMediaFiles(tempDir string, maxAge time.Duration) {
	if tempDir == "" || tempDir == "." {
		return // 避免誤刪重要檔案
	}

	entries, err := os.ReadDir(tempDir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[multimedia] cleanup readdir error: %v", err)
		}
		return
	}

	cutoff := time.Now().Add(-maxAge)
	deletedCount := 0
	deletedSize := int64(0)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filePath := filepath.Join(tempDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		// 檢查檔案是否過期
		if info.ModTime().Before(cutoff) {
			size := info.Size()
			if err := os.Remove(filePath); err == nil {
				deletedCount++
				deletedSize += size
			} else {
				log.Printf("[multimedia] cleanup remove error: %v", err)
			}
		}
	}

	if deletedCount > 0 {
		log.Printf("[multimedia] cleanup: removed %d files (%.1f MB)",
			deletedCount, float64(deletedSize)/(1024*1024))
	}
}

// DownloadTelegramFile 通用 Telegram 檔案下載函數（支援圖片和語音）
func (t *TelegramBot) DownloadTelegramFile(fileID, fileType string) (string, error) {
	// 1. 取得檔案路徑
	getFileURL := fmt.Sprintf("https://api.telegram.org/bot%s/getFile?file_id=%s",
		t.config.TelegramToken, fileID)

	resp, err := t.telegramAPIClient().Get(getFileURL)
	if err != nil {
		return "", fmt.Errorf("getFile request failed: %w", err)
	}
	defer resp.Body.Close()

	var fileResp struct {
		OK     bool `json:"ok"`
		Result struct {
			FileID   string `json:"file_id"`
			FilePath string `json:"file_path"`
			FileSize int    `json:"file_size"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&fileResp); err != nil {
		return "", fmt.Errorf("parse getFile response failed: %w", err)
	}

	if !fileResp.OK || fileResp.Result.FilePath == "" {
		return "", fmt.Errorf("getFile failed: invalid response")
	}

	// 檢查檔案大小限制
	maxSizeBytes := t.config.Multimedia.MaxFileSizeMB * 1024 * 1024
	if fileResp.Result.FileSize > maxSizeBytes {
		return "", fmt.Errorf("file too large: %d bytes (limit: %d MB)",
			fileResp.Result.FileSize, t.config.Multimedia.MaxFileSizeMB)
	}

	// 2. 下載檔案內容
	downloadURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s",
		t.config.TelegramToken, fileResp.Result.FilePath)

	downloadResp, err := t.telegramDownloadClient().Get(downloadURL)
	if err != nil {
		return "", fmt.Errorf("download file failed: %w", err)
	}
	defer downloadResp.Body.Close()

	// 3. 確保臨時目錄存在
	tempDir := t.config.Multimedia.TempDownloadDir
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("create temp dir failed: %w", err)
	}

	// 4. 儲存到臨時檔案（保留原始副檔名）
	ext := filepath.Ext(fileResp.Result.FilePath)
	if ext == "" {
		// 根據檔案類型設定預設副檔名
		switch fileType {
		case "photo":
			ext = ".jpg"
		case "voice":
			ext = ".ogg"
		case "document":
			ext = ".txt" // 文件類型的預設擴展名
		default:
			ext = ".bin"
		}
	}

	tempFile := filepath.Join(tempDir, fmt.Sprintf("%s_%s_%d%s",
		fileType, fileID, time.Now().Unix(), ext))

	file, err := os.Create(tempFile)
	if err != nil {
		return "", fmt.Errorf("create temp file failed: %w", err)
	}
	defer file.Close()

	_, err = io.Copy(file, downloadResp.Body)
	if err != nil {
		os.Remove(tempFile) // 清理失敗的檔案
		return "", fmt.Errorf("save file failed: %w", err)
	}

	return tempFile, nil
}

// handleVoiceMessage 處理語音訊息
func (t *TelegramBot) handleVoiceMessage(key chatKey, userID int64, voice *Voice, caption string, messageID int) {
	// 檢查語音支援是否開啟
	if !t.config.Multimedia.EnableVoiceSupport {
		msg := t.getLocalizedMessage(key.chatID, "voice_disabled", nil)
		t.send(key, msg)
		return
	}

	// 檢查是否有 OpenAI API Key
	if t.config.Multimedia.OpenAIAPIKey == "" {
		msg := t.getLocalizedMessage(key.chatID, "voice_no_api_key", nil)
		t.send(key, msg)
		return
	}

	// 檢查檔案大小限制
	maxSizeBytes := t.config.Multimedia.MaxFileSizeMB * 1024 * 1024
	if voice.FileSize > maxSizeBytes {
		msg := t.getLocalizedMessage(key.chatID, "voice_file_too_large", nil)
		msg = strings.ReplaceAll(msg, "{size}", formatFileSize(int64(voice.FileSize)))
		msg = strings.ReplaceAll(msg, "{limit}", fmt.Sprintf("%d", t.config.Multimedia.MaxFileSizeMB))
		t.send(key, msg)
		return
	}

	// 檢查語音長度限制（Whisper API 限制 25MB 或約 25 分鐘）
	if voice.Duration > 25*60 { // 25 分鐘
		msg := t.getLocalizedMessage(key.chatID, "voice_too_long", nil)
		msg = strings.ReplaceAll(msg, "{duration}", fmt.Sprintf("%d", voice.Duration))
		t.send(key, msg)
		return
	}

	// 下載語音檔案
	downloadMsg := t.getLocalizedMessage(key.chatID, "voice_downloading", nil)
	t.send(key, downloadMsg)
	voicePath, err := t.DownloadTelegramFile(voice.FileID, "voice")
	if err != nil {
		log.Printf("[telegram] download voice error: %v", err)
		failMsg := t.getLocalizedMessage(key.chatID, "voice_download_failed", nil)
		t.send(key, failMsg)
		return
	}

	// 確保在函數結束時清理臨時檔案
	defer func() {
		if err := os.Remove(voicePath); err != nil {
			log.Printf("[telegram] cleanup voice error: %v", err)
		}
	}()

	// 安全檢查和事件記錄
	if security.Global() != nil {
		security.Global().LogSecurityEvent(security.SecurityEvent{
			EventType:   "telegram_voice_received",
			Severity:    "medium",
			Description: "Voice message received via Telegram",
			UserID:      userID,
			Details: map[string]interface{}{
				"duration":    voice.Duration,
				"file_size":   voice.FileSize,
				"mime_type":   voice.MimeType,
				"has_caption": caption != "",
				"caption_len": len(caption),
				"chat_id":     key.chatID,
			},
		})

		// PII 檢測 caption (自動記錄事件)
		if caption != "" {
			filteredCaption, detected := security.Global().DetectAndFilterPII(caption, true, &security.PIIDetectionContext{
				ChatID:      key.chatID,
				UserID:      userID,
				MessageType: "voice",
				SourceType:  "telegram",
				ProjectPath: t.config.DefaultProjectDir,
				MessageID:   messageID,
			})
			if len(detected) > 0 {
				// 額外的 Telegram 上下文記錄 (降低嚴重性避免重複警告)
				security.Global().LogSecurityEvent(security.SecurityEvent{
					EventType:   "pii_detected_voice_caption",
					Severity:    "low", // 降低嚴重性，主要事件已記錄
					Description: fmt.Sprintf("Voice caption contained PII (filtered): %v", detected),
					UserID:      userID,
					Details: map[string]interface{}{
						"detected_types": detected,
						"chat_id":        key.chatID,
						"context":        "telegram_voice",
					},
				})
				piiMsg := t.getLocalizedMessage(key.chatID, "voice_caption_pii_detected", nil)
				t.send(key, piiMsg)
				caption = filteredCaption
			}
		}
	}

	// 語音轉文字
	transcribingMsg := t.getLocalizedMessage(key.chatID, "voice_transcribing", nil)
	t.send(key, transcribingMsg)
	transcribedText, err := t.transcribeVoiceWithWhisper(voicePath)
	if err != nil {
		log.Printf("[telegram] voice transcription error: %v", err)
		failMsg := t.getLocalizedMessage(key.chatID, "voice_transcribe_failed", nil)
		t.send(key, failMsg)
		return
	}

	if transcribedText == "" {
		noRecognizeMsg := t.getLocalizedMessage(key.chatID, "voice_not_recognized", nil)
		t.send(key, noRecognizeMsg)
		return
	}

	// 顯示轉錄結果供用戶確認
	resultMsg := t.getLocalizedMessage(key.chatID, "voice_transcription_result", nil)
	resultMsg = strings.ReplaceAll(resultMsg, "{text}", transcribedText)
	t.sendMarkdown(key, resultMsg)

	// 組合 prompt 讓 Claude 處理轉錄的文字
	prompt := transcribedText
	if caption != "" {
		prompt = fmt.Sprintf("%s\n\nUser note: %s", transcribedText, caption)
	}

	// 發送給 Agent 處理
	agent := t.getAgent(key)

	// Add language preference hint
	userLang := t.getChatLanguage(key.chatID)
	promptWithLang := prompt
	if userLang == "en" {
		promptWithLang = "Please respond in English. Do NOT use Chinese characters or Chinese formatting in your response.\n\n" + prompt
	} else if userLang == "zh-TW" {
		promptWithLang = "請用繁體中文回應。\n\n" + prompt
	}

	response, err := t.runAgentWithStopButton(key, agent, promptWithLang)
	if err != nil {
		if strings.Contains(err.Error(), "agent aborted by user") {
			return
		}
		log.Printf("[telegram] voice analysis error: %v", err)
		analysisFailMsg := t.getLocalizedMessage(key.chatID, "voice_analyze_failed", nil)
		t.send(key, analysisFailMsg)
		return
	}

	if response != "" {
		t.send(key, response)
	}
}

// getModelTag 根據模型名稱返回對應的標籤
func getModelTag(model string) string {
	lower := strings.ToLower(model)
	switch {
	case strings.Contains(lower, "haiku"):
		return "⚡ [Haiku]"
	case strings.Contains(lower, "opus"):
		return "🧠 [Opus]"
	case strings.Contains(lower, "sonnet"):
		return "🟡 [Sonnet]"
	case strings.Contains(lower, "gpt-5.5-pro"):
		return "💎 [GPT-5.5 Pro]"
	case strings.Contains(lower, "gpt-5.5"):
		return "🧠 [GPT-5.5]"
	case strings.Contains(lower, "gpt-5.4-mini"):
		return "⚡ [GPT-5.4 mini]"
	case strings.Contains(lower, "gpt-5.4"):
		return "🟡 [GPT-5.4]"
	case strings.Contains(lower, "gpt-5.3-codex"):
		return "🟢 [GPT-5.3 Codex]"
	case strings.HasPrefix(lower, "gpt-"), strings.HasPrefix(lower, "o3"), strings.HasPrefix(lower, "o4"):
		return fmt.Sprintf("🤖 [%s]", model)
	default:
		return "🤖 [Default]"
	}
}

// isStickySession 判斷 agent 是否處於黏性 session 狀態（session 活躍且未閒置超時）
// 黏性 session 期間一律沿用當前模型，不重新 triage
func (t *TelegramBot) isStickySession(agent *Agent) bool {
	if !t.config.ModelRouting.StickySession {
		return false
	}
	if agent.SessionID() == "" {
		return false
	}
	timeoutMin := t.config.ModelRouting.SessionIdleTimeoutMin
	if timeoutMin <= 0 {
		timeoutMin = 5
	}
	return time.Since(agent.LastActivity()) < time.Duration(timeoutMin)*time.Minute
}

// isContinuationMessage 偵測是否為 follow-up（接續語或短問句）
// 當 sticky session 未啟用或 session 不活躍時，用此作為次要防線避免不必要的 triage
func isContinuationMessage(msg string) bool {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return false
	}
	// 含程式碼區塊代表有實質內容
	if strings.Contains(msg, "```") {
		return false
	}

	runes := []rune(msg)
	msgLower := strings.ToLower(msg)

	// 短訊息（< 15 字）預設視為 follow-up（排除程式碼和完整問題陳述）
	if len(runes) < 15 {
		return true
	}

	// 明確接續語氣詞（前綴匹配）
	continuationPrefixes := []string{
		// 中文接續詞
		"但是", "那", "繼續", "還有", "所以", "那…呢", "那呢",
		"另外", "接著", "然後", "再來", "而且", "不過", "可是",
		// 英文接續詞
		"but ", "and ", "also ", "continue", "furthermore", "moreover",
		"then ", "next ", "additionally",
	}
	for _, prefix := range continuationPrefixes {
		if strings.HasPrefix(msgLower, strings.ToLower(prefix)) {
			return true
		}
	}

	// 代名詞指涉開頭（短問句，< 50 字）
	if len(runes) < 50 {
		pronounPrefixes := []string{
			"這個", "那個", "它", "這樣", "那樣", "這裡", "那裡",
			"this ", "that ", "it ", "them ", "those ", "these ",
		}
		for _, prefix := range pronounPrefixes {
			if strings.HasPrefix(msgLower, strings.ToLower(prefix)) {
				return true
			}
		}
	}

	// 追問短句：以疑問詞開頭且 < 30 字
	if len(runes) < 30 {
		questionPrefixes := []string{
			"為什麼", "怎麼", "如何", "哪裡", "什麼時候",
			"why ", "how ", "where ", "when ",
		}
		for _, prefix := range questionPrefixes {
			if strings.HasPrefix(msgLower, strings.ToLower(prefix)) {
				return true
			}
		}
	}

	// 精確匹配的確認／繼續詞
	exactWords := []string{
		"好", "是", "對", "行", "嗯", "去", "做", "試試",
		"ok", "yes", "y", "go", "sure",
		"好啊", "好的", "好了", "可以", "ok",
		"繼續", "繼續吧", "繼續做", "繼續進行", "請繼續",
		"continue", "proceed",
		"修正", "fix", "fix it",
		"下一步", "next", "之後",
		"做吧", "沒問題",
	}
	for _, word := range exactWords {
		if msgLower == strings.ToLower(word) {
			return true
		}
	}

	return false
}

// evaluateTaskComplexityScore 計算任務複雜度原始分數（供 hybrid triage 使用）
// 分數區間：score <= 0 → clearly fast, 1-5 → ambiguous, >= 6 → clearly deep
func evaluateTaskComplexityScore(userMessage string) int {
	score := 0

	// 1. 消息長度 (越長越可能複雜)
	if len(userMessage) > 800 {
		score += 3
	} else if len(userMessage) > 500 {
		score += 2
	} else if len(userMessage) > 200 {
		score += 1
	}

	// 2. 代碼塊數量 (``` 標記)
	codeBlocks := strings.Count(userMessage, "```")
	if codeBlocks >= 6 {
		score += 4
	} else if codeBlocks >= 4 {
		score += 3
	} else if codeBlocks >= 2 {
		score += 2
	} else if codeBlocks >= 1 {
		score += 1
	}

	// 3. 深度複雜度關鍵詞 (Opus 層級)
	deepKeywords := []string{
		"refactor", "架構", "architecture", "設計", "design",
		"實現", "implement", "演算法", "algorithm",
		"性能", "optimize", "效能", "優化",
		"跨", // 跨檔案、跨模組
		"整合", "integration", "aggregate",
		"系統", "system", "framework",
		"複雜", "complex", "難度",
	}
	for _, kw := range deepKeywords {
		if strings.Contains(strings.ToLower(userMessage), kw) {
			score += 1
		}
	}

	// 4. 中等難度關鍵詞 (Sonnet 層級)
	balancedKeywords := []string{
		"功能", "feature", "添加", "add",
		"測試", "test", "testing", "改進", "improve",
		"檢查", "review", "分析", "analyze",
		"連接", "connect", "統計", "statistics",
	}
	for _, kw := range balancedKeywords {
		if strings.Contains(strings.ToLower(userMessage), kw) {
			score += 1
		}
	}

	// 5. 簡單度關鍵詞 (Haiku 層級)
	fastKeywords := []string{
		"翻譯", "translate", "explain", "解釋",
		"改寫", "rewrite", "轉換", "convert",
		"查看", "show", "list", "看", "列出",
		"格式", "format", "json", "csv", "yaml",
		"簡單", "quick", "快速", "一行",
	}
	for _, kw := range fastKeywords {
		if strings.Contains(strings.ToLower(userMessage), kw) {
			score -= 1
		}
	}

	// 6. 危險操作 (write, delete, modify files) - 很複雜
	dangerousOps := []string{
		"file_patch", "write_file", "delete file", "刪除檔案",
		"修改全部", "modify all", "update all", "所有檔案",
		"批量", "batch",
	}
	for _, op := range dangerousOps {
		if strings.Contains(strings.ToLower(userMessage), op) {
			score += 2
		}
	}

	// 7. 多檔案操作指標 (中到高複雜度)
	if strings.Contains(strings.ToLower(userMessage), "多個") ||
		strings.Contains(strings.ToLower(userMessage), "multiple") ||
		strings.Contains(strings.ToLower(userMessage), "all files") ||
		strings.Contains(strings.ToLower(userMessage), "所有") {
		score += 2
	}

	// 8. 調試/修復指標 (中複雜度)
	debugKeywords := []string{
		"bug", "error", "問題", "修復", "fix", "debug", "為什麼",
		"不工作", "not working", "失敗", "fail", "錯誤",
	}
	for _, kw := range debugKeywords {
		if strings.Contains(strings.ToLower(userMessage), kw) {
			score += 1
		}
	}

	return score
}

// evaluateTaskComplexity 使用本地啟發式算法評估任務複雜度
// 基於多個信號判斷任務難度，無需外部 API 調用
// 返回值: "fast", "balanced", 或 "deep"
func evaluateTaskComplexity(userMessage string) string {
	score := evaluateTaskComplexityScore(userMessage)
	// 決策門檻 (三層級)
	//    score <= 1  : Fast (Haiku) - 簡單任務
	//    2-5         : Balanced (Sonnet) - 中等難度
	//    >= 6        : Deep (Opus) - 複雜任務
	if score >= 6 {
		return "deep"
	} else if score >= 2 {
		return "balanced"
	}
	return "fast"
}

// triageWithHaiku 使用 Claude Haiku 模型判斷任務複雜度
// 比本地啟發式算法更準確，且無需額外 API 金鑰（使用已有的 Claude 授權）
// 返回值: "fast", "balanced", 或 "deep"
func (t *TelegramBot) triageWithHaiku(ctx context.Context, userMessage string) string {
	triageCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	prompt := `Classify this task complexity. Reply with EXACTLY ONE WORD only.

"fast" → simple tasks: translate, explain concept, rewrite text, format data, quick questions, lookup, summarize short content
"balanced" → medium tasks: add a feature, write tests, fix a simple bug, review code, analyze a function
"deep" → complex tasks: system architecture, large-scale refactoring, complex debugging across multiple files, performance optimization, algorithm design

Task to classify:
` + userMessage + `

Reply only with: fast / balanced / deep`

	args := []string{
		"-p",
		"--output-format", "json",
		"--model", "claude-haiku-4-5-20251001",
		"--dangerously-skip-permissions",
		"--max-turns", "1",
		prompt,
	}

	output, err := runProcessOutput(triageCtx, ProcessOptions{
		Env:     cleanEnvForCLI(),
		Timeout: 15 * time.Second,
	}, "claude", args...)
	if err != nil {
		log.Printf("[telegram] haiku triage failed, falling back to heuristic: %v", err)
		return evaluateTaskComplexity(userMessage)
	}

	var resp CLIResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		log.Printf("[telegram] haiku triage parse error, falling back to heuristic: %v", err)
		return evaluateTaskComplexity(userMessage)
	}

	result := strings.ToLower(strings.TrimSpace(resp.Result))
	switch {
	case strings.Contains(result, "deep"):
		log.Printf("[telegram] haiku triage → deep")
		return "deep"
	case strings.Contains(result, "balanced"):
		log.Printf("[telegram] haiku triage → balanced")
		return "balanced"
	case strings.Contains(result, "fast"):
		log.Printf("[telegram] haiku triage → fast")
		return "fast"
	default:
		log.Printf("[telegram] haiku triage unclear response %q, using heuristic", result)
		return evaluateTaskComplexity(userMessage)
	}
}

// triageWithGPT4oMini 使用 OpenAI GPT-4o-mini 判斷任務複雜度
// 返回值: "fast" (簡單任務) 或 "deep" (複雜任務)
func (t *TelegramBot) triageWithGPT4oMini(userMessage string) (string, error) {
	// 如果未配置 OpenAI API key，返回 fast（保守選擇）
	if t.config.Multimedia.OpenAIAPIKey == "" {
		return "fast", fmt.Errorf("OpenAI API key not configured")
	}

	systemPrompt := `你是任務複雜度分類器。根據用戶的訊息，判斷該任務的複雜度。

快速任務（fast）：
- 翻譯或語言轉換
- 簡單解釋或文字改寫
- 格式轉換（JSON, CSV, XML）
- 查看或理解現有程式碼
- 簡單的文字編輯或查詢

深度任務（deep）：
- 系統設計或架構規劃
- 跨多個檔案的重構或大規模修改
- 複雜的 bug 修復或診斷
- 演算法實現或邏輯設計
- 性能最佳化分析

只回覆一個詞：fast 或 deep，不要有其他文字。`

	payload := map[string]interface{}{
		"model": "gpt-4o-mini",
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userMessage},
		},
		"max_tokens":  10,
		"temperature": 0,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "fast", fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "fast", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+t.config.Multimedia.OpenAIAPIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "fast", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "fast", fmt.Errorf("OpenAI API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "fast", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "fast", fmt.Errorf("empty choices in response")
	}

	// 提取並規範化回應
	response := strings.ToLower(strings.TrimSpace(result.Choices[0].Message.Content))

	// 驗證回應格式
	if response == "fast" {
		return "fast", nil
	} else if response == "deep" {
		return "deep", nil
	}

	// 如果回應不清楚，預設為 fast（保守選擇）
	log.Printf("[telegram] AI router returned unclear response: %q, defaulting to fast", response)
	return "fast", nil
}

// transcribeVoiceWithWhisper 使用 OpenAI Whisper API 轉錄語音
func (t *TelegramBot) transcribeVoiceWithWhisper(audioPath string) (string, error) {
	// 支援的格式：m4a, mp3, mp4, mpeg, mpga, wav, webm
	// Telegram 語音訊息通常是 .ogg 格式，Whisper 支援 webm 但不直接支援 ogg
	// 我們可以嘗試直接發送，如果不行則需要轉換格式

	file, err := os.Open(audioPath)
	if err != nil {
		return "", fmt.Errorf("failed to open audio file: %w", err)
	}
	defer file.Close()

	// 建立 multipart form
	var b bytes.Buffer
	writer := multipart.NewWriter(&b)

	// 添加音頻文件
	fw, err := writer.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	_, err = io.Copy(fw, file)
	if err != nil {
		return "", fmt.Errorf("failed to copy file: %w", err)
	}

	// 添加模型參數
	err = writer.WriteField("model", "whisper-1")
	if err != nil {
		return "", fmt.Errorf("failed to write model field: %w", err)
	}

	// 添加語言參數（自動檢測，支援中文）
	err = writer.WriteField("language", "zh")
	if err != nil {
		return "", fmt.Errorf("failed to write language field: %w", err)
	}

	// 添加響應格式
	err = writer.WriteField("response_format", "text")
	if err != nil {
		return "", fmt.Errorf("failed to write response_format field: %w", err)
	}

	err = writer.Close()
	if err != nil {
		return "", fmt.Errorf("failed to close writer: %w", err)
	}

	// 發送 API 請求
	req, err := http.NewRequest("POST", "https://api.openai.com/v1/audio/transcriptions", &b)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+t.config.Multimedia.OpenAIAPIKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("OpenAI API error (status %d): %s", resp.StatusCode, string(body))
	}

	// 讀取響應
	transcription, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return strings.TrimSpace(string(transcription)), nil
}

// handleDocumentMessage 處理文件訊息
func (t *TelegramBot) handleDocumentMessage(key chatKey, userID int64, document *Document, caption string, messageID int) {
	// 檢查檔案大小限制
	maxSizeBytes := t.config.Multimedia.MaxFileSizeMB * 1024 * 1024
	if document.FileSize > maxSizeBytes {
		msg := t.getLocalizedMessage(key.chatID, "document_file_too_large", nil)
		msg = strings.ReplaceAll(msg, "{size}", formatFileSize(int64(document.FileSize)))
		msg = strings.ReplaceAll(msg, "{limit}", fmt.Sprintf("%d", t.config.Multimedia.MaxFileSizeMB))
		t.send(key, msg)
		return
	}

	// 安全檢查和事件記錄
	if security.Global() != nil {
		security.Global().LogSecurityEvent(security.SecurityEvent{
			EventType:   "telegram_document_received",
			Severity:    "medium",
			Description: "Document file received via Telegram",
			UserID:      userID,
			Details: map[string]interface{}{
				"file_name":   document.FileName,
				"file_size":   document.FileSize,
				"mime_type":   document.MimeType,
				"has_caption": caption != "",
				"caption_len": len(caption),
				"chat_id":     key.chatID,
			},
		})
	}

	// 下載文件到 Alice 臨時目錄
	downloadMsg := t.getLocalizedMessage(key.chatID, "document_downloading", nil)
	t.send(key, downloadMsg)
	documentPath, err := t.DownloadTelegramFile(document.FileID, "document")
	if err != nil {
		log.Printf("[telegram] download document error: %v", err)
		errMsg := t.getLocalizedMessage(key.chatID, "document_download_failed", nil)
		t.send(key, errMsg)
		return
	}

	defer func() {
		// 清理下載的文件
		if err := os.Remove(documentPath); err != nil {
			log.Printf("[telegram] cleanup document error: %v", err)
		}
	}()

	// 取得 Agent 和專案目錄
	agent := t.getAgent(key)
	projectDir := agent.ProjectDir()

	// 如果有原始檔名，復制到專案目錄並使用原始名稱
	var finalPath string
	if document.FileName != "" {
		// 確保專案臨時目錄存在
		tempDir := filepath.Join(projectDir, "temp")
		if err := os.MkdirAll(tempDir, 0755); err != nil {
			log.Printf("[telegram] create temp dir error: %v", err)
			mkdirMsg := t.getLocalizedMessage(key.chatID, "document_mkdir_failed", nil)
			t.send(key, mkdirMsg)
			return
		}

		// 複製到專案目錄，保留原始檔名
		finalPath = filepath.Join(tempDir, document.FileName)
		if err := copyFile(documentPath, finalPath); err != nil {
			log.Printf("[telegram] copy document to project error: %v", err)
			copyMsg := t.getLocalizedMessage(key.chatID, "document_copy_failed", nil)
			t.send(key, copyMsg)
			return
		}

		log.Printf("[telegram] document copied to project: %s", finalPath)
	} else {
		finalPath = documentPath
	}

	// 構建 Claude 的輸入提示
	promptPrefix := t.getLocalizedMessage(key.chatID, "document_prompt_prefix", nil)
	promptPrefix = strings.ReplaceAll(promptPrefix, "{path}", finalPath)
	prompt := promptPrefix
	if caption != "" {
		userNote := t.getLocalizedMessage(key.chatID, "document_user_note", nil)
		userNote = strings.ReplaceAll(userNote, "{caption}", caption)
		prompt += userNote
	}

	// 新增文件類型提示
	if document.MimeType != "" {
		fileType := t.getLocalizedMessage(key.chatID, "document_file_type", nil)
		fileType = strings.ReplaceAll(fileType, "{mime}", document.MimeType)
		prompt += fileType
	}

	analyzePrompt := t.getLocalizedMessage(key.chatID, "document_analyzing", nil)
	prompt += analyzePrompt

	log.Printf("[telegram] sending document analysis request: file=%s, size=%d, type=%s, prompt_len=%d",
		document.FileName, document.FileSize, document.MimeType, len(prompt))

	// 發送分析訊息
	analyzeMsg := t.getLocalizedMessage(key.chatID, "document_analyzing", nil)
	t.send(key, analyzeMsg)

	// Add language preference hint
	userLang := t.getChatLanguage(key.chatID)
	promptWithLang := prompt
	if userLang == "en" {
		promptWithLang = "Please respond in English. Do NOT use Chinese characters or Chinese formatting in your response.\n\n" + prompt
	} else if userLang == "zh-TW" {
		promptWithLang = "請用繁體中文回應。\n\n" + prompt
	}

	response, err := t.runAgentWithStopButton(key, agent, promptWithLang)
	if err != nil {
		if strings.Contains(err.Error(), "agent aborted by user") {
			return
		}
		log.Printf("[telegram] document analysis error: %v", err)
		errMsg := t.getLocalizedMessage(key.chatID, "document_analysis_failed", nil)
		t.send(key, errMsg)
		return
	}

	if response != "" {
		t.send(key, response)
	}
}

// validateProjectPath 驗證專案路徑是否存在和有效
func (t *TelegramBot) validateProjectPath(projectPath string) error {
	// 1. 檢查路徑是否存在
	info, err := os.Stat(projectPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("path_not_exist:%s", projectPath)
	}
	if err != nil {
		return fmt.Errorf("path_access_denied:%s:%v", projectPath, err)
	}

	// 2. 檢查是否為目錄
	if !info.IsDir() {
		return fmt.Errorf("path_not_directory:%s", projectPath)
	}

	// 3. 檢查讀取權限
	if _, err := os.ReadDir(projectPath); err != nil {
		return fmt.Errorf("path_permission_denied:%s", projectPath)
	}

	return nil
}

// suggestSimilarPaths 搜尋相似的專案目錄
func (t *TelegramBot) suggestSimilarPaths(invalidPath string) []string {
	// 取得基礎路徑 (預設專案目錄的父目錄)
	baseDir := filepath.Dir(strings.TrimRight(t.config.DefaultProjectDir, "/"))

	// 提取用戶輸入的專案名稱
	targetName := filepath.Base(invalidPath)

	// 讀取基礎目錄下的所有子目錄
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		log.Printf("[telegram] failed to read base directory %s: %v", baseDir, err)
		return nil
	}

	var suggestions []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirName := entry.Name()
		// 檢查相似度
		if t.isSimilarPath(targetName, dirName) {
			suggestions = append(suggestions, filepath.Join(baseDir, dirName))
		}
	}

	// 限制建議數量
	if len(suggestions) > 3 {
		suggestions = suggestions[:3]
	}

	return suggestions
}

// isSimilarPath 檢查兩個路徑名稱是否相似
func (t *TelegramBot) isSimilarPath(target, candidate string) bool {
	target = strings.ToLower(target)
	candidate = strings.ToLower(candidate)

	// 完全相同 (應該不會發生，因為已經驗證失敗)
	if target == candidate {
		return false
	}

	// 檢查常見的命名差異
	variations := [][]string{
		{"-", "_"}, // 連字號 vs 底線
		{"_", "-"}, // 底線 vs 連字號
		{" ", "_"}, // 空格 vs 底線
		{" ", "-"}, // 空格 vs 連字號
	}

	for _, variation := range variations {
		modified := strings.ReplaceAll(target, variation[0], variation[1])
		if modified == candidate {
			return true
		}
	}

	// 檢查包含關係 (更寬鬆的匹配)
	if strings.Contains(candidate, target) || strings.Contains(target, candidate) {
		// 長度差異不能太大
		if abs(len(target)-len(candidate)) <= 3 {
			return true
		}
	}

	return false
}

// detectProjectType 偵測專案類型
func (t *TelegramBot) detectProjectType(chatID int64, projectPath string) string {
	projectFiles := map[string]string{
		"go.mod":             "Go",
		"package.json":       "Node.js",
		"Cargo.toml":         "Rust",
		"requirements.txt":   "Python",
		"setup.py":           "Python",
		"pom.xml":            "Java",
		"Makefile":           "Make",
		"docker-compose.yml": "Docker",
		"Dockerfile":         "Docker",
	}

	var detectedTypes []string
	for filename, projectType := range projectFiles {
		if _, err := os.Stat(filepath.Join(projectPath, filename)); err == nil {
			detectedTypes = append(detectedTypes, projectType)
		}
	}

	if len(detectedTypes) == 0 {
		return t.getLocalizedMessage(chatID, "generic_project", nil)
	}

	// 去重並返回
	unique := make(map[string]bool)
	var result []string
	for _, t := range detectedTypes {
		if !unique[t] {
			unique[t] = true
			result = append(result, t)
		}
	}

	if len(result) == 1 {
		return result[0]
	}
	return strings.Join(result[:min(2, len(result))], "/") // 最多顯示兩種類型
}

// handleLangCommand 處理 /lang 指令 - 切換 Bot 語言
func (t *TelegramBot) handleLangCommand(key chatKey, text string) {
	if t.i18n == nil {
		msg := t.getLocalizedMessage(key.chatID, "i18n_system_not_init", nil)
		t.send(key, msg)
		return
	}

	// 解析指令參數
	parts := strings.Fields(text)
	if len(parts) == 1 {
		// 沒有參數，顯示當前語言和可用語言
		currentLang := t.getChatLanguage(key.chatID)
		langName := t.i18n.GetLanguageName(currentLang)
		currentLangMsg := t.getLocalizedMessage(key.chatID, "lang_current", map[string]string{
			"lang": fmt.Sprintf("%s (%s)", langName, currentLang),
		})

		msg := fmt.Sprintf("🌐 %s\n\n", currentLangMsg)

		// 根據語言顯示支援語言標題
		if currentLang == "en" {
			msg += "Supported languages:\n"
		} else {
			msg += "支援語言：\n"
		}

		for code, name := range t.i18n.GetAvailableLanguages() {
			icon := " "
			if code == currentLang {
				icon = "✓"
			}
			msg += fmt.Sprintf("[%s] %s (%s)\n", icon, name, code)
		}

		usageMsg := t.getLocalizedMessage(key.chatID, "lang_usage", nil)
		msg += "\n" + usageMsg

		t.sendMarkdown(key, msg)
		return
	}

	// 有參數，嘗試切換語言
	requestedLang := parts[1]

	if !t.i18n.IsLanguageSupported(requestedLang) {
		errMsg := t.getLocalizedMessage(key.chatID, "lang_not_found", map[string]string{
			"lang": requestedLang,
		})
		t.send(key, errMsg)
		return
	}

	// 儲存語言偏好
	t.setChatlanguage(key.chatID, requestedLang)

	langName := t.i18n.GetLanguageName(requestedLang)
	msg := t.getLocalizedMessage(key.chatID, "lang_switched", map[string]string{
		"lang": fmt.Sprintf("%s (%s)", langName, requestedLang),
	})
	t.send(key, msg)

	log.Printf("[telegram] chat %d switched language to %s", key.chatID, requestedLang)
}

// getLocalizedMessage 根據 chat 的語言偏好取得本地化消息
func (t *TelegramBot) getLocalizedMessage(chatID int64, messageKey string, vars map[string]string) string {
	if t.i18n == nil {
		return messageKey // Fallback to key if i18n not initialized
	}
	lang := t.getChatLanguage(chatID)
	return t.i18n.GetMessage(lang, messageKey, vars)
}

// getChatLanguage 取得指定 chat 的語言偏好
func (t *TelegramBot) getChatLanguage(chatID int64) string {
	// First check in-memory cache
	t.langPrefMu.RLock()
	if lang, ok := t.langPreferences[chatID]; ok && lang != "" {
		t.langPrefMu.RUnlock()
		return lang
	}
	t.langPrefMu.RUnlock()

	// If not cached, try loading from database
	if globalStorage != nil {
		if lang, err := globalStorage.GetChatLanguage(chatID); err == nil && lang != "" {
			// Cache it for future use
			t.langPrefMu.Lock()
			t.langPreferences[chatID] = lang
			t.langPrefMu.Unlock()
			return lang
		}
	}

	// Fall back to system default language
	if t.i18n != nil {
		return t.i18n.GetDefaultLanguage()
	}

	return "en"
}

// setChatlanguage 設定指定 chat 的語言偏好
func (t *TelegramBot) setChatlanguage(chatID int64, lang string) {
	t.langPrefMu.Lock()
	defer t.langPrefMu.Unlock()

	t.langPreferences[chatID] = lang

	// 持久化到 SQLite
	if globalStorage != nil {
		if err := globalStorage.SaveChatLanguage(chatID, lang); err != nil {
			log.Printf("[telegram] failed to save chat language preference: %v", err)
		}
	}
}

// handlePreviewCommand 處理 /preview 命令，進行卡片式網頁預覽
func (t *TelegramBot) handlePreviewCommand(key chatKey, urlStr string) {
	// 發送"處理中"提示
	t.send(key, fmt.Sprintf("🔄 正在預覽 %s，請稍候...", urlStr))

	// 建立 context (45 秒超時，等待截圖)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// 截圖 + 提取 metadata（單一 browser 實例）
	screenshotPath, metadata, err := t.screenshotManager.CaptureScreenshotWithMetadata(ctx, urlStr)
	if err != nil {
		log.Printf("[telegram] preview CaptureScreenshotWithMetadata failed: %v", err)
		// 降級：只送文字卡片
		t.send(key, fmt.Sprintf("📄 *網頁預覽*\n────────────────\n🔗 [%s](%s)\n\n❌ 截圖失敗: %v", urlStr, urlStr, err))
		return
	}
	defer os.Remove(screenshotPath)

	// 構建 caption（截圖的說明文字）
	caption := t.buildPreviewCaption(urlStr, metadata)

	// 截圖當圖片、card 資訊當 caption，一條訊息
	if err := t.sendPhoto(key, screenshotPath, caption); err != nil {
		log.Printf("[telegram] preview sendPhoto failed: %v", err)
		// 降級：送文字卡片
		t.send(key, caption)
	}
}

// downloadImageToTemp 下載圖片到臨時目錄
func (t *TelegramBot) downloadImageToTemp(ctx context.Context, imageURL string) (string, error) {
	// 建立臨時目錄
	tempDir := "temp/preview_images"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("建立臨時目錄失敗: %w", err)
	}

	// 創建 HTTP 請求
	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return "", fmt.Errorf("建立請求失敗: %w", err)
	}

	// 設置 User-Agent
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Alice Bot)")

	// 執行請求
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("下載失敗: %w", err)
	}
	defer resp.Body.Close()

	// 檢查狀態碼
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// 限制檔案大小 (5 MB)
	limitedReader := io.LimitedReader{R: resp.Body, N: 5 * 1024 * 1024}
	imgData, err := io.ReadAll(&limitedReader)
	if err != nil {
		return "", fmt.Errorf("讀取圖片失敗: %w", err)
	}

	if len(imgData) == 0 {
		return "", fmt.Errorf("圖片為空")
	}

	// 生成檔名
	filename := fmt.Sprintf("preview_%d.jpg", time.Now().UnixMilli())
	filePath := filepath.Join(tempDir, filename)

	// 保存圖片
	if err := os.WriteFile(filePath, imgData, 0644); err != nil {
		return "", fmt.Errorf("保存圖片失敗: %w", err)
	}

	return filePath, nil
}

// buildPreviewCaption 構建截圖 caption（Telegram caption 上限 1024 字）
func (t *TelegramBot) buildPreviewCaption(urlStr string, metadata *WebMetadata) string {
	var sb strings.Builder

	// 標題
	if metadata != nil && metadata.Title != "" {
		title := metadata.Title
		if len([]rune(title)) > 50 {
			runes := []rune(title)
			title = string(runes[:50]) + "…"
		}
		sb.WriteString(fmt.Sprintf("*%s*\n", title))
	}

	// 描述
	if metadata != nil && metadata.Description != "" {
		desc := metadata.Description
		if len([]rune(desc)) > 150 {
			runes := []rune(desc)
			desc = string(runes[:150]) + "…"
		}
		sb.WriteString(fmt.Sprintf("%s\n", desc))
	}

	sb.WriteString(fmt.Sprintf("\n🔗 [開啟網址](%s)", urlStr))

	return sb.String()
}

// abs 計算絕對值
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// handleSkillsCommand lists all auto-skills for the current chat
func (t *TelegramBot) handleSkillsCommand(key chatKey) {
	if globalSkillManager == nil {
		t.send(key, t.getLocalizedMessage(key.chatID, "skill_disabled", nil))
		return
	}

	skills := globalSkillManager.GetSkillsForChat(key.chatID)
	if len(skills) == 0 {
		t.send(key, t.getLocalizedMessage(key.chatID, "skill_list_empty", nil))
		return
	}

	header := t.getLocalizedMessage(key.chatID, "skill_list_header", map[string]string{
		"{count}": fmt.Sprintf("%d", len(skills)),
	})

	var sb strings.Builder
	sb.WriteString(header + "\n\n")

	for i, skill := range skills {
		statusIcon := "✅"
		if skill.Status == "inactive" {
			statusIcon = "⏸"
		} else if skill.Status == "disabled" {
			statusIcon = "❌"
		}

		sb.WriteString(fmt.Sprintf("%s **%d. %s**\n", statusIcon, i+1, skill.Name))
		sb.WriteString(fmt.Sprintf("   ID: `%s`\n", skill.ID))
		sb.WriteString(fmt.Sprintf("   🔧 %d steps | ✅ %.0f%% | 🔄 %d uses | v%d\n",
			len(skill.ToolChain), skill.SuccessRate*100, skill.UseCount, skill.Version))

		if len(skill.Tags) > 0 {
			maxTags := 5
			if len(skill.Tags) < maxTags {
				maxTags = len(skill.Tags)
			}
			sb.WriteString(fmt.Sprintf("   🏷 %s\n", strings.Join(skill.Tags[:maxTags], ", ")))
		}
		sb.WriteString("\n")

		if i >= 19 { // Show max 20 skills
			sb.WriteString(fmt.Sprintf("... +%d more\n", len(skills)-20))
			break
		}
	}

	sb.WriteString(t.getLocalizedMessage(key.chatID, "skill_list_footer", nil))
	t.sendMarkdown(key, sb.String())
}

// handleSkillDeleteCommand deletes a skill by ID
func (t *TelegramBot) handleSkillDeleteCommand(key chatKey, skillID string) {
	if globalSkillManager == nil {
		t.send(key, t.getLocalizedMessage(key.chatID, "skill_disabled", nil))
		return
	}

	err := globalSkillManager.DeleteSkill(skillID, key.chatID)
	if err != nil {
		msg := t.getLocalizedMessage(key.chatID, "skill_delete_error", map[string]string{
			"{error}": err.Error(),
		})
		t.send(key, msg)
		return
	}

	msg := t.getLocalizedMessage(key.chatID, "skill_deleted", map[string]string{
		"{id}": skillID,
	})
	t.send(key, msg)
}

// handleParallelCommand handles /parallel command for concurrent task execution
func (t *TelegramBot) handleParallelCommand(key chatKey, taskText string) {
	if globalOrchestrator == nil {
		t.send(key, t.getLocalizedMessage(key.chatID, "parallel_disabled", nil))
		return
	}

	tasks := ParseParallelTasks(taskText)
	if len(tasks) < 2 {
		t.send(key, t.getLocalizedMessage(key.chatID, "parallel_min_tasks", nil))
		return
	}

	if len(tasks) > 5 {
		t.send(key, t.getLocalizedMessage(key.chatID, "parallel_max_tasks", nil))
		return
	}

	// Send initial status
	startMsg := t.getLocalizedMessage(key.chatID, "parallel_started", map[string]string{
		"{count}": fmt.Sprintf("%d", len(tasks)),
	})
	t.send(key, startMsg)

	// Execute in background
	go func() {
		done := globalJobTracker.Start("parallel.command")
		defer done(nil)
		execution := globalOrchestrator.ExecuteParallel(
			tasks,
			t.client,
			t.getAgent(key).ProjectDir(),
			key.chatID,
			key.threadID,
			func(taskID, status, result string) {
				// Send progress update
				progressMsg := t.getLocalizedMessage(key.chatID, "parallel_progress", map[string]string{
					"{task_id}": taskID,
					"{status}":  status,
				})
				t.send(key, progressMsg)
			},
			0, // use default timeout
		)

		// Send final results
		resultMsg := FormatParallelResults(execution)
		t.sendMarkdown(key, resultMsg)
	}()
}

// handleCronCommand handles all /cron subcommands
func (t *TelegramBot) handleCronCommand(key chatKey, parts []string, fullText string) {
	if globalCronScheduler == nil {
		t.send(key, t.getLocalizedMessage(key.chatID, "cron_disabled", nil))
		return
	}

	if len(parts) < 2 {
		t.send(key, t.getLocalizedMessage(key.chatID, "cron_usage", nil))
		return
	}

	subCmd := parts[1]
	switch subCmd {
	case "add":
		t.handleCronAdd(key, fullText)
	case "list":
		t.handleCronList(key)
	case "delete", "rm":
		if len(parts) < 3 {
			t.send(key, t.getLocalizedMessage(key.chatID, "cron_delete_usage", nil))
			return
		}
		t.handleCronDelete(key, parts[2])
	case "pause":
		if len(parts) < 3 {
			t.send(key, t.getLocalizedMessage(key.chatID, "cron_pause_usage", nil))
			return
		}
		t.handleCronPause(key, parts[2])
	case "resume":
		if len(parts) < 3 {
			t.send(key, t.getLocalizedMessage(key.chatID, "cron_resume_usage", nil))
			return
		}
		t.handleCronResume(key, parts[2])
	case "run":
		if len(parts) < 3 {
			t.send(key, t.getLocalizedMessage(key.chatID, "cron_run_usage", nil))
			return
		}
		t.handleCronRun(key, parts[2])
	default:
		t.send(key, t.getLocalizedMessage(key.chatID, "cron_usage", nil))
	}
}

// handleCronAdd adds a new scheduled task
// Format: /cron add <schedule> <command or prompt>
// Example: /cron add 每天早上九點 go test ./...
// Example: /cron add 0 9 * * * go test ./...
func (t *TelegramBot) handleCronAdd(key chatKey, fullText string) {
	// Strip "/cron add " prefix
	after := strings.TrimSpace(strings.TrimPrefix(fullText, "/cron add"))
	if after == "" {
		t.send(key, t.getLocalizedMessage(key.chatID, "cron_add_usage", nil))
		return
	}

	// Try to parse natural language schedule from the beginning
	var cronExpr, scheduleName, payload string
	var taskType string

	// Try natural language first
	cronExpr, scheduleName = ParseNaturalLanguageCron(after)
	if cronExpr != "" {
		// Find where the schedule description ends and payload begins
		// The payload is everything after the matched schedule keywords
		payload = extractPayloadAfterSchedule(after)
		if payload == "" {
			t.send(key, t.getLocalizedMessage(key.chatID, "cron_add_no_payload", nil))
			return
		}
	} else {
		// Try raw cron expression: first 5 fields are cron, rest is payload
		fields := strings.Fields(after)
		if len(fields) >= 6 {
			maybeCron := strings.Join(fields[:5], " ")
			if _, err := ParseNaturalLanguageCron(maybeCron); err == "" {
				// Direct cron expression check
				cronExpr = maybeCron
				scheduleName = "custom"
				payload = strings.Join(fields[5:], " ")
			}
		}
	}

	if cronExpr == "" || payload == "" {
		t.send(key, t.getLocalizedMessage(key.chatID, "cron_add_parse_error", nil))
		return
	}

	// Determine task type
	taskType = "prompt" // default to prompt
	if isShellCommand(payload) {
		taskType = "command"
	}

	taskID := fmt.Sprintf("cron_%d_%d", key.chatID, time.Now().UnixMilli())
	task := ScheduledTask{
		ID:        taskID,
		ChatID:    key.chatID,
		ThreadID:  key.threadID,
		Name:      truncateCronResult(payload, 50),
		CronExpr:  cronExpr,
		TaskType:  taskType,
		Payload:   payload,
		Enabled:   true,
		CreatedAt: time.Now(),
	}

	if err := globalCronScheduler.Add(task); err != nil {
		msg := t.getLocalizedMessage(key.chatID, "cron_add_error", map[string]string{
			"{error}": err.Error(),
		})
		t.send(key, msg)
		return
	}

	msg := t.getLocalizedMessage(key.chatID, "cron_added", map[string]string{
		"{name}":     task.Name,
		"{schedule}": scheduleName + " (" + cronExpr + ")",
		"{type}":     taskType,
		"{id}":       taskID,
	})
	t.send(key, msg)
}

// handleCronList lists all scheduled tasks for the current chat
func (t *TelegramBot) handleCronList(key chatKey) {
	tasks := globalCronScheduler.List(key.chatID)
	if len(tasks) == 0 {
		t.send(key, t.getLocalizedMessage(key.chatID, "cron_list_empty", nil))
		return
	}

	var sb strings.Builder
	header := t.getLocalizedMessage(key.chatID, "cron_list_header", map[string]string{
		"{count}": fmt.Sprintf("%d", len(tasks)),
	})
	sb.WriteString(header + "\n\n")

	for i, task := range tasks {
		icon := "✅"
		if !task.Enabled {
			icon = "⏸"
		}

		sb.WriteString(fmt.Sprintf("%s **%d. %s**\n", icon, i+1, task.Name))
		sb.WriteString(fmt.Sprintf("   ID: `%s`\n", task.ID))
		sb.WriteString(fmt.Sprintf("   ⏰ %s | 📋 %s\n", task.CronExpr, task.TaskType))
		sb.WriteString(fmt.Sprintf("   🔄 %d runs | ❌ %d fails\n", task.RunCount, task.FailCount))

		if task.LastStatus != "" {
			statusIcon := "✅"
			if task.LastStatus == "failed" {
				statusIcon = "❌"
			}
			sb.WriteString(fmt.Sprintf("   %s Last: %s\n", statusIcon, task.LastRunAt.Format("01/02 15:04")))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(t.getLocalizedMessage(key.chatID, "cron_list_footer", nil))
	t.sendMarkdown(key, sb.String())
}

// handleCronDelete deletes a scheduled task
func (t *TelegramBot) handleCronDelete(key chatKey, taskID string) {
	if err := globalCronScheduler.Remove(taskID, key.chatID); err != nil {
		msg := t.getLocalizedMessage(key.chatID, "cron_delete_error", map[string]string{
			"{error}": err.Error(),
		})
		t.send(key, msg)
		return
	}
	msg := t.getLocalizedMessage(key.chatID, "cron_deleted", map[string]string{"{id}": taskID})
	t.send(key, msg)
}

// handleCronPause pauses a scheduled task
func (t *TelegramBot) handleCronPause(key chatKey, taskID string) {
	if err := globalCronScheduler.Pause(taskID, key.chatID); err != nil {
		t.send(key, fmt.Sprintf("❌ %v", err))
		return
	}
	msg := t.getLocalizedMessage(key.chatID, "cron_paused", map[string]string{"{id}": taskID})
	t.send(key, msg)
}

// handleCronResume resumes a paused task
func (t *TelegramBot) handleCronResume(key chatKey, taskID string) {
	if err := globalCronScheduler.Resume(taskID, key.chatID); err != nil {
		t.send(key, fmt.Sprintf("❌ %v", err))
		return
	}
	msg := t.getLocalizedMessage(key.chatID, "cron_resumed", map[string]string{"{id}": taskID})
	t.send(key, msg)
}

// handleCronRun manually triggers a task
func (t *TelegramBot) handleCronRun(key chatKey, taskID string) {
	if err := globalCronScheduler.RunNow(taskID, key.chatID); err != nil {
		t.send(key, fmt.Sprintf("❌ %v", err))
		return
	}
	msg := t.getLocalizedMessage(key.chatID, "cron_run_triggered", map[string]string{"{id}": taskID})
	t.send(key, msg)
}

// extractPayloadAfterSchedule extracts the task payload after a natural language schedule
func extractPayloadAfterSchedule(input string) string {
	// Common schedule delimiters
	lower := strings.ToLower(input)

	// Find where schedule keywords end
	scheduleEnders := []string{
		"每天早上九點", "每天早上9點", "每天早上八點", "每天早上8點",
		"每天早上十點", "每天早上10點", "每天中午", "每天下午三點", "每天下午3點",
		"每天下班前", "每天下午六點", "每天下午6點", "每天凌晨", "每天凌晨兩點", "每天凌晨2點",
		"每週一早上", "每周一早上", "每週五下午", "每周五下午", "每週日", "每周日",
		"每月1號", "每月一號", "每月15號", "每月十五號",
		"每5分鐘", "每五分鐘", "每10分鐘", "每十分鐘", "每30分鐘", "每半小時",
		"每小時", "每一小時", "每6小時", "每六小時",
		"every day 9am", "every day 8am", "daily 9am", "daily 8am",
		"daily 9:00", "daily 8:00", "daily 10am", "daily 10:00",
		"daily noon", "daily 12:00", "daily 3pm", "daily 15:00",
		"daily 6pm", "daily 18:00", "daily 2am",
		"every monday morning", "weekly monday",
		"every friday afternoon", "every sunday",
		"every month 1st", "monthly", "every month 15th",
		"every 5 min", "every 10 min", "every 30 min",
		"every hour", "every 6 hour",
	}

	for _, ender := range scheduleEnders {
		idx := strings.Index(lower, ender)
		if idx >= 0 {
			afterSchedule := input[idx+len(ender):]
			return strings.TrimSpace(afterSchedule)
		}
	}

	return ""
}

// isShellCommand detects if the payload looks like a shell command
func isShellCommand(payload string) bool {
	cmdPrefixes := []string{
		"go ", "git ", "npm ", "yarn ", "make ", "docker ",
		"ls ", "cat ", "grep ", "find ", "curl ",
		"python ", "node ", "cargo ", "rustc ",
	}
	lower := strings.ToLower(strings.TrimSpace(payload))
	for _, prefix := range cmdPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// codexTierAvailable reports whether the active client supports codex/GPT models,
// sending a localized notice to the chat and returning false otherwise.
// Called by /gfast /gsmart /gdeep and /model gpt-* before mutating user preference.
func (t *TelegramBot) codexTierAvailable(key chatKey) bool {
	mb, ok := t.client.(*MultiBackendClient)
	if !ok {
		t.send(key, t.getLocalizedMessage(key.chatID, "codex_tier_requires_multi_backend", nil))
		return false
	}
	if !mb.HasCodex() {
		t.send(key, t.getLocalizedMessage(key.chatID, "codex_tier_no_openai_key", nil))
		return false
	}
	return true
}

// modelRequiresCodex reports whether a raw model name routes to the codex backend.
// Mirrors MultiBackendClient.routeFor's gpt-/o3/o4/codex prefix logic.
func modelRequiresCodex(model string) bool {
	lower := strings.ToLower(model)
	return strings.HasPrefix(lower, "gpt-") ||
		strings.HasPrefix(lower, "o3") ||
		strings.HasPrefix(lower, "o4") ||
		strings.Contains(lower, "codex")
}

// handleModelCommand handles /model command for explicit model switching.
// Usage: /model <model-name>  (e.g. /model gpt-5.5-pro, /model claude-sonnet-4-6, /model auto)
func (t *TelegramBot) handleModelCommand(key chatKey, parts []string) {
	if len(parts) < 2 {
		currentPref := t.getUserModelPreference(key)
		if currentPref == "" {
			currentPref = "auto"
		}
		t.send(key, t.getLocalizedMessage(key.chatID, "model_command_usage", map[string]string{"current": currentPref}))
		return
	}
	modelName := strings.TrimSpace(parts[1])
	agent := t.getAgent(key)
	if modelName == "auto" || modelName == "reset" {
		t.setUserModelPreference(key, "")
		agent.SetPlanMode(false, "", "")
		t.send(key, t.getLocalizedMessage(key.chatID, "mode_switched_auto", nil))
		return
	}
	// Reject codex-bound model names when codex tier is unavailable, instead of
	// silently storing a preference that will fail on the next message.
	if modelRequiresCodex(modelName) && !t.codexTierAvailable(key) {
		return
	}
	targetBackend := BackendKindForModel(modelName)
	hasSession := agent.LastBackend() == targetBackend && agent.SessionIDForModel(modelName) != ""
	if hasSession {
		agent.ClearSessionForModel(modelName)
	}
	t.setUserModelPreference(key, modelName)
	agent.SetPlanMode(false, "", "")
	msg := t.getLocalizedMessage(key.chatID, "model_command_switched", map[string]string{"model": modelName})
	if hasSession {
		msg += "\n\n" + t.getLocalizedMessage(key.chatID, "model_switch_context_reset", nil)
	}
	t.send(key, msg)
}

// handleBackendCommand handles /backend subcommands
func (t *TelegramBot) handleBackendCommand(key chatKey, parts []string) {
	if globalBackendManager == nil {
		t.send(key, t.getLocalizedMessage(key.chatID, "backend_disabled", nil))
		return
	}

	if len(parts) < 2 {
		// Default: show status
		t.handleBackendList(key)
		return
	}

	switch parts[1] {
	case "list", "ls":
		t.handleBackendList(key)
	case "switch", "set":
		if len(parts) < 3 {
			t.send(key, t.getLocalizedMessage(key.chatID, "backend_switch_usage", nil))
			return
		}
		t.handleBackendSwitch(key, parts[2])
	case "health", "ping":
		t.handleBackendHealth(key)
	default:
		t.send(key, t.getLocalizedMessage(key.chatID, "backend_usage", nil))
	}
}

// handleBackendList shows all registered backends
func (t *TelegramBot) handleBackendList(key chatKey) {
	backends := globalBackendManager.ListAll()
	defaultName := globalBackendManager.DefaultName()

	msg := t.getLocalizedMessage(key.chatID, "backend_list_header", map[string]string{
		"{count}": fmt.Sprintf("%d", len(backends)),
	})
	msg += "\n\n"

	for _, b := range backends {
		status := "✅"
		if !b.Available {
			status = "❌"
		}
		isDefault := ""
		if b.Name == defaultName {
			isDefault = " ⭐"
		}
		msg += fmt.Sprintf("%s %s [%s]%s\n   %s\n", status, b.Name, b.Type, isDefault, b.Details)
	}

	msg += "\n" + t.getLocalizedMessage(key.chatID, "backend_list_footer", nil)
	t.send(key, msg)
}

// handleBackendSwitch changes the default backend
func (t *TelegramBot) handleBackendSwitch(key chatKey, name string) {
	if err := globalBackendManager.SetDefault(name); err != nil {
		t.send(key, t.getLocalizedMessage(key.chatID, "backend_switch_error", map[string]string{"{error}": err.Error()}))
		return
	}
	t.send(key, t.getLocalizedMessage(key.chatID, "backend_switched", map[string]string{"{name}": name}))
}

// handleBackendHealth runs health checks on all backends
func (t *TelegramBot) handleBackendHealth(key chatKey) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results := globalBackendManager.HealthCheckAll(ctx)

	msg := t.getLocalizedMessage(key.chatID, "backend_health_header", nil)
	msg += "\n\n"

	for name, err := range results {
		if err == nil {
			msg += fmt.Sprintf("✅ %s — OK\n", name)
		} else {
			msg += fmt.Sprintf("❌ %s — %s\n", name, err.Error())
		}
	}

	t.send(key, msg)
}
