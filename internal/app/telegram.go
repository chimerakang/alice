package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
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
	Method    string                 `json:"method"`
	Params    map[string]interface{} `json:"params"`
	Retries   int                    `json:"retries"`
	MaxRetries int                   `json:"max_retries"`
	CreatedAt time.Time             `json:"created_at"`
}

type TelegramBot struct {
	agents        map[chatKey]*Agent // 每個 chat/topic 一個 agent
	agentsMu      sync.RWMutex       // 保護 agents map 的讀寫鎖
	client        Client
	allowIDs      map[int64]bool // 白名單
	config        *Config
	i18n          *I18nManager   // 多國語系管理器

	// 媒體批次處理
	mediaBatches  map[string]*MediaBatch // mediaGroupID 或 chatKey 作為 key
	batchMu       sync.RWMutex           // 保護 mediaBatches map
	batchTimeout  time.Duration          // 批次收集超時時間

	// Rate limiting and message queue
	messageQueue  chan *TelegramMessage  // 訊息佇列
	queueCtx      context.Context        // 佇列上下文
	queueCancel   context.CancelFunc     // 佇列取消函數
	rateLimiter   *time.Ticker          // 速率限制器

	// Model routing preferences per chat/thread
	modelPreferences map[chatKey]string // "fast", "deep", or ""
	prefMu           sync.RWMutex       // Protect model preferences

	// Language preferences per chat
	langPreferences map[int64]string // chatID -> language code
	langPrefMu      sync.RWMutex    // Protect language preferences
}

func NewTelegramBot(config *Config, client Client) (*TelegramBot, error) {
	// 驗證 bot token
	resp, err := http.Get(fmt.Sprintf("https://api.telegram.org/bot%s/getMe", config.TelegramToken))
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
		rateLimiter:  time.NewTicker(500 * time.Millisecond), // 2 messages per second

		// Model routing preferences
		modelPreferences: make(map[chatKey]string),
		langPreferences:  make(map[int64]string),
	}

	// Start message queue worker
	go bot.messageQueueWorker()

	// 註冊 Telegram 指令選單
	bot.registerCommands()

	// Load persisted chat language preferences from database (background operation)
	if globalStorage != nil {
		go func() {
			// This is a best-effort load - we don't fail the bot if this doesn't work
			// Language preferences will be loaded on-demand if not cached
			log.Printf("[telegram] loading persisted chat language preferences...")
		}()
	}

	return bot, nil
}

// registerCommands 透過 Telegram Bot API 註冊指令自動完成選單
func (t *TelegramBot) registerCommands() {
	commands := []map[string]string{
		{"command": "project", "description": "Switch project directory"},
		{"command": "reset", "description": "Clear conversation history"},
		{"command": "status", "description": "View current status"},
		{"command": "usage", "description": "View token usage"},
		{"command": "fast", "description": "Switch to fast mode (Haiku)"},
		{"command": "deep", "description": "Switch to deep mode (Opus)"},
		{"command": "auto", "description": "Auto routing mode (AI decides)"},
		{"command": "abort", "description": "Abort running task"},
		{"command": "dashboard", "description": "View system monitoring dashboard"},
		{"command": "checkpoints", "description": "View checkpoint status"},
		{"command": "multiagent", "description": "Multi-agent coordination management"},
		{"command": "agents", "description": "View specialized agent list"},
		{"command": "tasks", "description": "View to-do list"},
		{"command": "lang", "description": "Switch bot language"},
		{"command": "help", "description": "Show help message"},
	}

	body, _ := json.Marshal(map[string]interface{}{"commands": commands})
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/setMyCommands", t.config.TelegramToken)
	resp, err := http.Post(apiURL, "application/json", bytes.NewReader(body))
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

	agent := NewAgent(t.client, projectDir, key.chatID, key.threadID)
	t.agents[key] = agent
	return agent
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
	return t.modelPreferences[key]
}

// setUserModelPreference 安全地設定用戶的模型偏好設定
// mode: "fast", "deep", 或 "" (自動模式)
func (t *TelegramBot) setUserModelPreference(key chatKey, mode string) {
	t.prefMu.Lock()
	defer t.prefMu.Unlock()
	if mode == "" {
		delete(t.modelPreferences, key)
	} else {
		t.modelPreferences[key] = mode
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
		resp, err := http.Get(fmt.Sprintf("%s/getUpdates?offset=%d&timeout=60", apiURL, offset))
		if err != nil {
			log.Printf("[telegram] getUpdates error: %v", err)
			continue
		}

		var result struct {
			OK     bool `json:"ok"`
			Result []struct {
				UpdateID int `json:"update_id"`
				Message  *struct {
					MessageID       int    `json:"message_id"`
					MessageThreadID int    `json:"message_thread_id"`
					From            *struct {
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
					ID      string `json:"id"`
					From    *struct {
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
				go t.handleMessage(key, msg.From.ID, msg.Text, msg.Caption, msg.Photo, msg.Voice, msg.Document, msg.MediaGroupID, msg.MessageID)
			}

			// Handle callback queries (inline keyboard button clicks)
			if update.CallbackQuery != nil && update.CallbackQuery.Message != nil {
				query := update.CallbackQuery
				key := chatKey{chatID: query.Message.Chat.ID, threadID: query.Message.MessageThreadID}
				go t.handleCallbackQuery(key, query.From.ID, query.ID, query.Data)
			}
		}
	}
}

func (t *TelegramBot) handleMessage(key chatKey, userID int64, text string, caption string, photo []PhotoSize, voice *Voice, document *Document, mediaGroupID string, messageID int) {
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
	if globalSecurityManager != nil {
		// 記錄用戶請求安全事件
		globalSecurityManager.LogSecurityEvent(SecurityEvent{
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
		filteredText, detected := globalSecurityManager.DetectAndFilterPII(text, true, &PIIDetectionContext{
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
		log.Printf("[telegram] handling command: %s from user %d", text, userID)
		t.handleCommand(key, text)
		return
	}

	// 一般訊息 → agent 處理
	agent := t.getAgent(key)

	// Model routing: Three-tier priority system
	var modelOverride string
	if t.config.ModelRouting.EnableDynamicRouting {
		// Priority 1: User explicit preference (/fast or /deep)
		userPref := t.getUserModelPreference(key)
		if userPref == "fast" {
			modelOverride = t.config.ModelRouting.FastModel
			log.Printf("[telegram] model routing: using fast model (user preference)")
		} else if userPref == "deep" {
			modelOverride = t.config.ModelRouting.DeepModel
			log.Printf("[telegram] model routing: using deep model (user preference)")
		} else if isContinuationMessage(text) {
			// Priority 2: Continuation message — inherit current model + session, skip triage
			log.Printf("[telegram] model routing: continuation message detected, keeping current model + session")
			// modelOverride stays empty → agent keeps lastUsedModel + sessionID unchanged
		} else {
			// Priority 3: Hybrid triage
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
				modelOverride = t.config.ModelRouting.DeepModel
				log.Printf("[telegram] model routing: classified as deep (Opus)")
			case "balanced":
				// Keep default model (Sonnet) - no override needed
				log.Printf("[telegram] model routing: classified as balanced (Sonnet)")
			default: // "fast"
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

	// Add language preference hint to message for Claude
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
			response, err = agent.Run(userMessage, createUpdateCallback())
		}
	} else {
		// Regular single agent execution
		response, err = agent.Run(userMessage, createUpdateCallback())
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
		if response != "" {
			// Partial success: send accumulated content, then show error
			t.sendLong(key, response)
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

	// 加上模型標籤
	modelTag := getModelTag(agent.lastUsedModel)
	response = modelTag + "\n\n" + response

	// Telegram 訊息限制 4096 字元，分段發送
	t.sendLong(key, response)
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
	if len(errStr) > 200 {
		return errStr[:200] + "..."
	}
	return errStr
}

func (t *TelegramBot) handleCommand(key chatKey, text string) {
	parts := strings.Fields(text)
	cmd := strings.Split(parts[0], "@")[0] // 去掉 @botname 後綴
	log.Printf("[telegram] processing command: %s", cmd)

	switch cmd {
	case "/start", "/help":
		// Build help text using localized messages for both languages
		help := "🤖 *Claude Code Agent*\n\n"
		help += t.getLocalizedMessage(key.chatID, "help_intro", nil) + "\n\n"
		help += t.getLocalizedMessage(key.chatID, "help_forum_topics", nil) + "\n\n"
		help += t.getLocalizedMessage(key.chatID, "help_basic_commands", nil) + "\n"
		help += t.getLocalizedMessage(key.chatID, "help_project_desc", nil) + "\n"
		help += t.getLocalizedMessage(key.chatID, "help_reset_desc", nil) + "\n"
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
		help += t.getLocalizedMessage(key.chatID, "help_lang_desc", nil)
		t.sendMarkdown(key, help)

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
		} else if modelMode == "deep" {
			modelDisplay = t.getLocalizedMessage(key.chatID, "model_deep", nil)
			modelDisplay = strings.ReplaceAll(modelDisplay, "{model}", t.config.ModelRouting.DeepModel)
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
		agent := t.getAgent(key)
		if agent.IsProcessing() {
			if agent.Abort() {
				t.send(key, t.getLocalizedMessage(key.chatID, "task_aborted", nil))
			} else {
				t.send(key, t.getLocalizedMessage(key.chatID, "task_finished", nil))
			}
		} else {
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
		t.setUserModelPreference(key, "fast")
		msg := t.getLocalizedMessage(key.chatID, "mode_switched_fast", map[string]string{"model": t.config.ModelRouting.FastModel})
		t.send(key, msg)

	case "/deep":
		if !t.config.ModelRouting.EnableDynamicRouting {
			t.send(key, t.getLocalizedMessage(key.chatID, "routing_disabled", nil))
			return
		}
		t.setUserModelPreference(key, "deep")
		msg := t.getLocalizedMessage(key.chatID, "mode_switched_deep", map[string]string{"model": t.config.ModelRouting.DeepModel})
		t.send(key, msg)

	case "/auto":
		if !t.config.ModelRouting.EnableDynamicRouting {
			t.send(key, t.getLocalizedMessage(key.chatID, "routing_disabled", nil))
			return
		}
		t.setUserModelPreference(key, "")
		t.send(key, t.getLocalizedMessage(key.chatID, "mode_switched_auto", nil))

	case "/savings":
		var projectPath string
		if len(parts) > 1 {
			projectPath = strings.Join(parts[1:], " ")
		}
		t.handleSavingsCommand(key, projectPath)

	case "/lang":
		t.handleLangCommand(key, text)

	default:
		t.send(key, t.getLocalizedMessage(key.chatID, "unknown_command", nil))
	}
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
	resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
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
			OK          bool `json:"ok"`
			ErrorCode   int  `json:"error_code"`
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

	// Log other errors
	log.Printf("[telegram] %s failed (status %d): %s", msg.Method, resp.StatusCode, string(body))
	return false
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
	resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
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
			OK          bool `json:"ok"`
			ErrorCode   int  `json:"error_code"`
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

	params := url.Values{
		"chat_id":    {strconv.FormatInt(key.chatID, 10)},
		"text":       {cleanText},
		"parse_mode": {"Markdown"},
	}
	if key.threadID != 0 {
		params.Set("message_thread_id", strconv.Itoa(key.threadID))
		log.Printf("[telegram] sendMarkdown: setting message_thread_id=%d for chat_id=%d", key.threadID, key.chatID)
	} else {
		log.Printf("[telegram] sendMarkdown: WARNING - NO message_thread_id (threadID=0) for chat_id=%d", key.chatID)
	}

	// Use the API call which now goes through the queue
	t.apiCall("sendMessage", params)
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
		name  string
		key   string
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
					"text": refreshText,
					"callback_data": "refresh_dashboard",
				},
				{
					"text": checkpointText,
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
	case strings.HasPrefix(data, "stop_agent_"):
		// Handle stop button click
		agent := t.getAgent(key)
		if agent.IsProcessing() {
			if agent.Abort() {
				abortMsg := t.getLocalizedMessage(key.chatID, "callback_task_aborted", nil)
				t.answerCallbackQuery(queryID, abortMsg)
				log.Printf("Agent task stopped by user via callback button (chat: %d, thread: %d)", key.chatID, key.threadID)
			} else {
				failMsg := t.getLocalizedMessage(key.chatID, "callback_abort_failed", nil)
				t.answerCallbackQuery(queryID, failMsg)
			}
		} else {
			noTaskMsg := t.getLocalizedMessage(key.chatID, "callback_no_running_task", nil)
			t.answerCallbackQuery(queryID, noTaskMsg)
		}
	default:
		unknownMsg := t.getLocalizedMessage(key.chatID, "callback_unknown_operation", nil)
		t.answerCallbackQuery(queryID, unknownMsg)
	}
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

// editMessageRemoveStopButton removes the stop button from a message
func (t *TelegramBot) editMessageRemoveStopButton(key chatKey, messageID int, newText string) {
	params := map[string]interface{}{
		"chat_id":    key.chatID,
		"message_id": messageID,
		"text":       newText,
	}

	if key.threadID != 0 {
		params["message_thread_id"] = key.threadID
	}

	t.sendTelegram("editMessageText", params)
}

// sendTelegram sends JSON data to Telegram API via the message queue
func (t *TelegramBot) sendTelegram(method string, params map[string]interface{}) {
	// Queue the message instead of sending directly
	t.queueMessage(method, params)
}

// handleTasks 處理 /tasks 命令，顯示未完成的工作清單
func (t *TelegramBot) handleTasks(key chatKey) {
	agent := t.getAgent(key)
	projectDir := agent.ProjectDir()

	tasksFile := filepath.Join(projectDir, "docs", "MASTER_TASKS.md")
	data, err := os.ReadFile(tasksFile)
	if err != nil {
		errMsg := t.getLocalizedMessage(key.chatID, "tasks_read_failed", nil)
		errMsg = strings.ReplaceAll(errMsg, "{error}", err.Error())
		t.send(key, errMsg)
		return
	}

	// 提取 Phase Overview 部分（簡潔摘要）
	content := string(data)
	if idx := strings.Index(content, "## Phase Overview"); idx >= 0 {
		// 找出 Phase Overview 區塊的結尾（下一個 ## 或 --- 分隔符）
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

		extracted := rest[:endIdx]
		t.send(key, extracted)
	} else {
		errMsg := t.getLocalizedMessage(key.chatID, "tasks_format_invalid", nil)
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
			msg = strings.ReplaceAll(msg, "{size}", formatFileSize(targetPhoto.FileSize))
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
	if globalSecurityManager != nil {
		globalSecurityManager.LogSecurityEvent(SecurityEvent{
			EventType:   "telegram_batch_photos_received",
			Severity:    "medium",
			Description: fmt.Sprintf("Batch of %d photos received via Telegram", len(relativeImagePaths)),
			UserID:      userID,
			Details: map[string]interface{}{
				"photo_count":  len(relativeImagePaths),
				"has_caption":  caption != "",
				"caption_len":  len(caption),
				"chat_id":      key.chatID,
			},
		})

		// PII 檢測 caption (自動記錄事件)
		if caption != "" {
			filteredCaption, detected := globalSecurityManager.DetectAndFilterPII(caption, true, &PIIDetectionContext{
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

	response, err := agent.Run(promptWithLang, func(update string, silent bool) {
		if silent {
			t.sendSilent(key, update)
		} else {
			t.send(key, update)
		}
	})

	if err != nil {
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
		msg = strings.ReplaceAll(msg, "{size}", formatFileSize(targetPhoto.FileSize))
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
	if globalSecurityManager != nil {
		globalSecurityManager.LogSecurityEvent(SecurityEvent{
			EventType:   "telegram_photo_received",
			Severity:    "medium",
			Description: "Photo message received via Telegram",
			UserID:      userID,
			Details: map[string]interface{}{
				"file_size":    targetPhoto.FileSize,
				"width":        targetPhoto.Width,
				"height":       targetPhoto.Height,
				"has_caption":  caption != "",
				"caption_len":  len(caption),
				"chat_id":      key.chatID,
			},
		})

		// PII 檢測 caption (自動記錄事件)
		if caption != "" {
			filteredCaption, detected := globalSecurityManager.DetectAndFilterPII(caption, true, &PIIDetectionContext{
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

	response, err := agent.Run(promptWithLang, func(update string, silent bool) {
		if silent {
			t.sendSilent(key, update)
		} else {
			t.send(key, update)
		}
	})

	if err != nil {
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
		msg = strings.ReplaceAll(msg, "{size}", formatFileSize(targetPhoto.FileSize))
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
	if globalSecurityManager != nil {
		globalSecurityManager.LogSecurityEvent(SecurityEvent{
			EventType:   "telegram_photo_received",
			Severity:    "medium",
			Description: "Photo message received via Telegram",
			UserID:      userID,
			Details: map[string]interface{}{
				"file_size":    targetPhoto.FileSize,
				"width":        targetPhoto.Width,
				"height":       targetPhoto.Height,
				"has_caption":  caption != "",
				"caption_len":  len(caption),
				"chat_id":      key.chatID,
			},
		})

		// PII 檢測 caption (自動記錄事件)
		if caption != "" {
			filteredCaption, detected := globalSecurityManager.DetectAndFilterPII(caption, true, &PIIDetectionContext{
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

	response, err := agent.Run(promptWithLang, func(update string, silent bool) {
		if silent {
			t.sendSilent(key, update)
		} else {
			t.send(key, update)
		}
	})
	if err != nil {
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
func formatFileSize(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
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

	resp, err := http.Get(getFileURL)
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

	downloadResp, err := http.Get(downloadURL)
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
		msg = strings.ReplaceAll(msg, "{size}", formatFileSize(voice.FileSize))
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
	if globalSecurityManager != nil {
		globalSecurityManager.LogSecurityEvent(SecurityEvent{
			EventType:   "telegram_voice_received",
			Severity:    "medium",
			Description: "Voice message received via Telegram",
			UserID:      userID,
			Details: map[string]interface{}{
				"duration":     voice.Duration,
				"file_size":    voice.FileSize,
				"mime_type":    voice.MimeType,
				"has_caption":  caption != "",
				"caption_len":  len(caption),
				"chat_id":      key.chatID,
			},
		})

		// PII 檢測 caption (自動記錄事件)
		if caption != "" {
			filteredCaption, detected := globalSecurityManager.DetectAndFilterPII(caption, true, &PIIDetectionContext{
			ChatID:      key.chatID,
			UserID:      userID,
			MessageType: "voice",
			SourceType:  "telegram",
			ProjectPath: t.config.DefaultProjectDir,
			MessageID:   messageID,
		})
			if len(detected) > 0 {
				// 額外的 Telegram 上下文記錄 (降低嚴重性避免重複警告)
				globalSecurityManager.LogSecurityEvent(SecurityEvent{
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

	response, err := agent.Run(promptWithLang, func(update string, silent bool) {
		if silent {
			t.sendSilent(key, update)
		} else {
			t.send(key, update)
		}
	})
	if err != nil {
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
	switch {
	case strings.Contains(model, "haiku"):
		return "⚡ [Haiku]"
	case strings.Contains(model, "opus"):
		return "🧠 [Opus]"
	case strings.Contains(model, "sonnet"):
		return "🟡 [Sonnet]"
	default:
		return "🤖 [Default]"
	}
}

// isContinuationMessage 偵測是否為「繼續語」
// 繼續語是短且無實質新請求的訊息，應繼承當前 model 與 session，不觸發 triage
func isContinuationMessage(msg string) bool {
	msg = strings.TrimSpace(msg)
	// 超過 100 個字元不可能是純繼續語
	if len([]rune(msg)) > 100 {
		return false
	}
	// 含程式碼區塊代表有實質內容
	if strings.Contains(msg, "```") {
		return false
	}

	msgLower := strings.ToLower(msg)

	// 確定的繼續語詞彙清單（精確匹配）
	// 包括：單字確認、簡短繼續詞彙、修正指令
	continuationWords := []string{
		// 單字確認
		"好", "是", "對", "行", "嗯", "去",
		// 簡短英文
		"ok", "yes", "y", "go",
		// 帶詞綴的確認
		"好啊", "好的", "好了", "可以", "OK",
		// 繼續指令
		"繼續", "繼續吧", "繼續做", "繼續進行",
		"continue", "請繼續",
		// 修正指令
		"修正", "fix", "fix it",
		// 下一步指令
		"下一步", "next", "之後",
		// 允許指令
		"做吧", "沒問題", "proceed",
	}

	for _, word := range continuationWords {
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
	triageCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
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

	cmd := exec.CommandContext(triageCtx, "claude", args...)
	cmd.Env = cleanEnvForCLI()

	output, err := cmd.Output()
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
		"max_tokens":   10,
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
		msg = strings.ReplaceAll(msg, "{size}", formatFileSize(document.FileSize))
		msg = strings.ReplaceAll(msg, "{limit}", fmt.Sprintf("%d", t.config.Multimedia.MaxFileSizeMB))
		t.send(key, msg)
		return
	}

	// 安全檢查和事件記錄
	if globalSecurityManager != nil {
		globalSecurityManager.LogSecurityEvent(SecurityEvent{
			EventType:   "telegram_document_received",
			Severity:    "medium",
			Description: "Document file received via Telegram",
			UserID:      userID,
			Details: map[string]interface{}{
				"file_name":    document.FileName,
				"file_size":    document.FileSize,
				"mime_type":    document.MimeType,
				"has_caption":  caption != "",
				"caption_len":  len(caption),
				"chat_id":      key.chatID,
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

	response, err := agent.Run(promptWithLang, func(update string, silent bool) {
		if silent {
			t.sendSilent(key, update)
		} else {
			t.send(key, update)
		}
	})
	if err != nil {
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
		{"-", "_"},  // 連字號 vs 底線
		{"_", "-"},  // 底線 vs 連字號
		{" ", "_"},  // 空格 vs 底線
		{" ", "-"},  // 空格 vs 連字號
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
		"go.mod":        "Go",
		"package.json":  "Node.js",
		"Cargo.toml":    "Rust",
		"requirements.txt": "Python",
		"setup.py":      "Python",
		"pom.xml":       "Java",
		"Makefile":      "Make",
		"docker-compose.yml": "Docker",
		"Dockerfile":    "Docker",
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

// abs 計算絕對值
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

