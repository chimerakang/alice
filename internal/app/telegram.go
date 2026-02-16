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
	"regexp"
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
	client        *CLIClient
	allowIDs      map[int64]bool // 白名單
	config        *Config

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
}

func NewTelegramBot(config *Config, client *CLIClient) (*TelegramBot, error) {
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

	// Initialize context for message queue
	queueCtx, queueCancel := context.WithCancel(context.Background())

	bot := &TelegramBot{
		agents:       make(map[chatKey]*Agent),
		client:       client,
		allowIDs:     allowIDs,
		config:       config,
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
	}

	// Start message queue worker
	go bot.messageQueueWorker()

	// 註冊 Telegram 指令選單
	bot.registerCommands()

	return bot, nil
}

// registerCommands 透過 Telegram Bot API 註冊指令自動完成選單
func (t *TelegramBot) registerCommands() {
	commands := []map[string]string{
		{"command": "project", "description": "切換專案目錄"},
		{"command": "reset", "description": "清除對話歷史"},
		{"command": "status", "description": "查看目前狀態"},
		{"command": "usage", "description": "查看 token 用量"},
		{"command": "fast", "description": "切換至快速模式 (Haiku)"},
		{"command": "deep", "description": "切換至深度模式 (Opus)"},
		{"command": "auto", "description": "自動模式 (AI 路由)"},
		{"command": "abort", "description": "中斷正在執行的任務"},
		{"command": "dashboard", "description": "查看系統監控面板"},
		{"command": "checkpoints", "description": "查看檢查點狀態"},
		{"command": "multiagent", "description": "多代理協調管理"},
		{"command": "agents", "description": "查看專門化代理清單"},
		{"command": "tasks", "description": "查看待辦工作清單"},
		{"command": "help", "description": "顯示說明"},
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
func (t *TelegramBot) handleSavingsCommand(key chatKey) {
	if globalStorage == nil {
		t.send(key, "❌ 儲存系統不可用")
		return
	}

	// 默認查詢最近 7 天的數據
	report, err := globalStorage.GetCostSavings(168)
	if err != nil {
		log.Printf("[telegram] failed to get cost savings: %v", err)
		t.send(key, fmt.Sprintf("❌ 無法取得成本數據: %v", err))
		return
	}

	if report.TotalRequests == 0 {
		t.send(key, "📊 本週路由統計\n\n還沒有任何路由數據")
		return
	}

	// 組建回應訊息
	var msg strings.Builder
	msg.WriteString("📊 *本週智慧路由統計*\n\n")

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

		msg.WriteString(fmt.Sprintf("%s *%s*: %d 次\n", modelIcon, model, breakdown.Calls))
		msg.WriteString(fmt.Sprintf("  成本: $%.2f (假設 Sonnet: $%.2f) %s\n\n",
			breakdown.ActualCost, breakdown.WouldHaveCost, savedSign))
	}

	// 節省統計
	msg.WriteString("💰 *節省金額統計*\n")
	msg.WriteString(fmt.Sprintf("實際花費: $%.2f\n", report.ActualCost))
	msg.WriteString(fmt.Sprintf("假設全用 Sonnet: $%.2f\n", report.DefaultModelCost))
	msg.WriteString(fmt.Sprintf("節省金額: *$%.2f* (%.1f%%)\n\n",
		report.SavingsCost, report.SavingsPercent))

	// 路由方式統計
	if len(report.RoutingMethodStat) > 0 {
		msg.WriteString("🎯 *路由方式分佈*\n")
		for method, count := range report.RoutingMethodStat {
			percent := 0.0
			if report.TotalRequests > 0 {
				percent = float64(count) / float64(report.TotalRequests) * 100
			}
			msg.WriteString(fmt.Sprintf("• %s: %d 次 (%.1f%%)\n", method, count, percent))
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
				go t.handleMessage(key, msg.From.ID, msg.Text, msg.Caption, msg.Photo, msg.Voice, msg.Document, msg.MediaGroupID)
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

func (t *TelegramBot) handleMessage(key chatKey, userID int64, text string, caption string, photo []PhotoSize, voice *Voice, document *Document, mediaGroupID string) {
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
		t.send(key, "⛔ 你沒有使用權限。")
		return
	}

	text = strings.TrimSpace(text)
	caption = strings.TrimSpace(caption)

	// Handle photo messages - 支援批次處理
	if len(photo) > 0 {
		log.Printf("[telegram] handling photo message with %d photos, mediaGroupID=%s", len(photo), mediaGroupID)
		t.handlePhotoMessageBatch(key, userID, photo, caption, mediaGroupID)
		return
	}

	// Handle voice messages
	if voice != nil {
		log.Printf("[telegram] handling voice message: duration=%ds, size=%d bytes", voice.Duration, voice.FileSize)
		t.handleVoiceMessage(key, userID, voice, caption)
		return
	}

	// Handle document messages
	if document != nil {
		log.Printf("[telegram] handling document message: name=%s, size=%d bytes, type=%s", document.FileName, document.FileSize, document.MimeType)
		t.handleDocumentMessage(key, userID, document, caption)
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
		filteredText, detected := globalSecurityManager.DetectAndFilterPII(text, true)
		if len(detected) > 0 {
			// PII 事件已由 DetectAndFilterPII 自動記錄，這裡添加額外的 Telegram 特定資訊
			globalSecurityManager.LogSecurityEvent(SecurityEvent{
				EventType:   "pii_detected_telegram",
				Severity:    "medium", // 降低嚴重性，避免重複高優先級警告
				Description: fmt.Sprintf("Telegram message contained PII (filtered): %v", detected),
				UserID:      userID,
				Details: map[string]interface{}{
					"detected_types": detected,
					"chat_id":        key.chatID,
				},
			})

			// 警告用戶並使用過濾後的文字
			t.send(key, "⚠️ 偵測到敏感資訊已自動過濾，請注意保護隱私資料。")
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
		} else {
			// Priority 2: Local heuristic-based complexity evaluation (three-tier algorithm)
			// No external API call needed - fast and cheap
			complexity := evaluateTaskComplexity(text)
			switch complexity {
			case "deep":
				modelOverride = t.config.ModelRouting.DeepModel
				log.Printf("[telegram] model routing: complexity evaluation classified as deep (Opus)")
			case "balanced":
				// Keep default model (Sonnet) - no override needed
				log.Printf("[telegram] model routing: complexity evaluation classified as balanced (Sonnet)")
			default: // "fast"
				modelOverride = t.config.ModelRouting.FastModel
				log.Printf("[telegram] model routing: complexity evaluation classified as fast (Haiku)")
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
	if globalAgentCoordinator.IsEnabled() && globalAgentCoordinator.ShouldUseMultiAgent(text) {
		// Use coordinated multi-agent execution
		response, err = globalAgentCoordinator.ExecuteCoordinatedTask(text, agent, createUpdateCallback())
	} else if globalAgentCoordinator.IsEnabled() {
		// Use single specialized agent based on task routing
		agentType := globalAgentCoordinator.RouteTask(text)
		if agentType != GeneralAgent {
			specializedAgent := globalAgentCoordinator.GetOrCreateAgent(agentType, agent)
			t.send(key, fmt.Sprintf("🤖 使用 %s 代理處理此任務", agentType.String()))

			response, err = specializedAgent.ExecuteSubTask(SubTask{
				ID:          fmt.Sprintf("single_%d", time.Now().Unix()),
				Description: text,
				AgentType:   agentType,
				Status:      TaskStatusInProgress,
			}, createUpdateCallback())
		} else {
			// Fall back to regular agent
			response, err = agent.Run(text, createUpdateCallback())
		}
	} else {
		// Regular single agent execution
		response, err = agent.Run(text, createUpdateCallback())
	}

	// Remove stop button after completion
	if statusMessageID != 0 {
		finalText := "✅ 執行完成"
		if err != nil {
			if strings.Contains(err.Error(), "agent aborted by user") {
				finalText = "🛑 已中斷執行"
			} else if response != "" {
				finalText = "⚠️ 部分完成"
			} else {
				finalText = "❌ 執行錯誤"
			}
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
			t.send(key, fmt.Sprintf("⚠️ 過程中發生錯誤: %s", extractErrorReason(err.Error())))
			return
		}
		t.send(key, fmt.Sprintf("❌ 錯誤: %s", extractErrorReason(err.Error())))
		return
	}

	if response == "" {
		response = "（完成，無文字回覆）"
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
		help := `🤖 *Claude Code Agent*

直接傳送訊息，我就會在你的專案中工作。
使用 Claude Code CLI（Max 訂閱，無額外費用）。

*支援 Forum Topics：*
在群組開啟 Topics，每個 Topic 綁定一個專案，對話完全獨立。

*基本指令：*
/project <路徑> — 切換專案目錄
/reset — 清除對話歷史
/status — 查看目前狀態
/usage — 查看 token 用量

*模型路由指令：*
/fast — 切換至快速模式 ⚡ (Haiku)
/deep — 切換至深度模式 🧠 (Opus)
/auto — 自動路由模式 🤖
/savings — 查看本週路由節省金額 💰

*進階指令：*
/dashboard — 查看系統監控面板
/checkpoints — 查看檢查點狀態
/abort — 中斷正在執行的任務
/multiagent [enable|disable|status|stats] — 多代理協調管理
/agents — 查看專門化代理清單
/tasks — 查看待辦工作清單
/help — 顯示此說明`
		t.sendMarkdown(key, help)

	case "/project":
		if len(parts) < 2 {
			t.send(key, "用法: /project <路徑或專案名稱>")
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
			errorMsg := fmt.Sprintf("❌ %s", err.Error())

			// 嘗試提供相似路徑建議
			suggestions := t.suggestSimilarPaths(dir)
			if len(suggestions) > 0 {
				errorMsg += "\n\n💡 是否要設定這些相似的專案？"
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
		projectType := t.detectProjectType(dir)
		projectName := filepath.Base(dir)

		// 建構成功訊息
		successMsg := fmt.Sprintf("✅ 專案已設定為：%s", projectName)
		successMsg += fmt.Sprintf("\n📂 路徑：`%s`", dir)
		successMsg += fmt.Sprintf("\n🔧 類型：%s", projectType)

		// 檢查是否有 MASTER_TASKS.md (用於 /tasks 功能)
		tasksFile := filepath.Join(dir, "docs", "MASTER_TASKS.md")
		if _, err := os.Stat(tasksFile); err == nil {
			successMsg += "\n📋 可用指令：/tasks, /status, /checkpoints"
		} else {
			successMsg += "\n📋 可用指令：/status, /checkpoints"
		}

		t.sendMarkdown(key, successMsg)

	case "/reset":
		agent := t.getAgent(key)
		stats := agent.Stats()
		if stats.APICallCount > 0 {
			t.send(key, fmt.Sprintf("🔄 對話已清除\n本次用量: %dK in / %dK out (%d 次呼叫)",
				stats.TotalInputTokens/1000, stats.TotalOutputTokens/1000, stats.APICallCount))
		} else {
			t.send(key, "🔄 對話歷史已清除")
		}
		agent.Reset()

	case "/status":
		agent := t.getAgent(key)
		stats := agent.Stats()
		sessionInfo := "無"
		if agent.SessionID() != "" {
			sessionInfo = fmt.Sprintf("`%s`", agent.SessionID())
		}

		// Get current model mode
		modelMode := t.getUserModelPreference(key)
		var modelDisplay string
		if modelMode == "fast" {
			modelDisplay = fmt.Sprintf("`%s` (⚡ 快速模式)", t.config.ModelRouting.FastModel)
		} else if modelMode == "deep" {
			modelDisplay = fmt.Sprintf("`%s` (🧠 深度模式)", t.config.ModelRouting.DeepModel)
		} else {
			modelDisplay = fmt.Sprintf("`%s`", t.client.Model)
		}

		status := fmt.Sprintf(
			"📊 *狀態*\n"+
				"專案: `%s`\n"+
				"模型: %s\n"+
				"Session: %s\n"+
				"CLI 呼叫: %d 次\n"+
				"累計: %dK in / %dK out",
			agent.projectDir,
			modelDisplay,
			sessionInfo,
			stats.APICallCount,
			stats.TotalInputTokens/1000,
			stats.TotalOutputTokens/1000,
		)
		t.sendMarkdown(key, status)

	case "/usage":
		agent := t.getAgent(key)
		stats := agent.Stats()

		var msg strings.Builder
		msg.WriteString(fmt.Sprintf(
			"💰 *Token 用量*\n\n"+
				"*本次對話:*\n"+
				"  輸入: %d tokens\n"+
				"  輸出: %d tokens\n"+
				"  CLI 呼叫: %d 次\n"+
				"  CLI 費用: $%.4f\n",
			stats.TotalInputTokens,
			stats.TotalOutputTokens,
			stats.APICallCount,
			stats.TotalCostUSD,
		))

		// 按模型分類顯示（從資料庫查詢最近 7 天）
		if globalStorage != nil {
			report, err := globalStorage.GetCostSavings(168)
			if err == nil && report.TotalRequests > 0 {
				msg.WriteString("\n📊 *按模型分類（近 7 天）:*\n")
				for model, breakdown := range report.ByModel {
					modelIcon := "🟢"
					if model == "sonnet" {
						modelIcon = "🟡"
					} else if model == "opus" {
						modelIcon = "🔴"
					} else if model == "haiku" {
						modelIcon = "⚡"
					}
					msg.WriteString(fmt.Sprintf("  %s %s: %d 次 | %d in / %d out | $%.4f\n",
						modelIcon, model, breakdown.Calls,
						breakdown.InputTokens, breakdown.OutputTokens,
						breakdown.ActualCost))
				}
				if report.SavingsPercent != 0 {
					msg.WriteString(fmt.Sprintf("\n💡 *路由節省: %.1f%%* ($%.4f → $%.4f)\n",
						report.SavingsPercent, report.DefaultModelCost, report.ActualCost))
				}
			}
		}

		msg.WriteString("\n*模式: Claude Max 訂閱*\n")
		msg.WriteString("  月費固定 $200，無額外 token 費用")
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
				t.send(key, "用法: /checkpoints [list|stats]")
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
				t.send(key, "✅ 多代理協調已啟用")
			case "disable":
				globalAgentCoordinator.SetEnabled(false)
				t.send(key, "❌ 多代理協調已停用")
			case "status":
				t.handleMultiAgentStatus(key)
			case "stats":
				t.handleMultiAgentStats(key)
			default:
				t.send(key, "用法: /multiagent [enable|disable|status|stats]")
			}
		}

	case "/abort":
		agent := t.getAgent(key)
		if agent.IsProcessing() {
			if agent.Abort() {
				t.send(key, "🛑 已中斷正在執行的任務")
			} else {
				t.send(key, "⚠️ 任務已結束，無需中斷")
			}
		} else {
			t.send(key, "ℹ️ 目前沒有正在執行的任務")
		}

	case "/agents":
		t.handleAgentsList(key)

	case "/tasks":
		t.handleTasks(key)

	case "/fast":
		if !t.config.ModelRouting.EnableDynamicRouting {
			t.send(key, "⚠️ 動態模型路由功能未啟用")
			return
		}
		t.setUserModelPreference(key, "fast")
		t.send(key, fmt.Sprintf("✅ 已切換至快速模式\n模型: `%s`", t.config.ModelRouting.FastModel))

	case "/deep":
		if !t.config.ModelRouting.EnableDynamicRouting {
			t.send(key, "⚠️ 動態模型路由功能未啟用")
			return
		}
		t.setUserModelPreference(key, "deep")
		t.send(key, fmt.Sprintf("✅ 已切換至深度模式\n模型: `%s`", t.config.ModelRouting.DeepModel))

	case "/auto":
		if !t.config.ModelRouting.EnableDynamicRouting {
			t.send(key, "⚠️ 動態模型路由功能未啟用")
			return
		}
		t.setUserModelPreference(key, "")
		t.send(key, "✅ 已切換至自動路由模式")

	case "/savings":
		t.handleSavingsCommand(key)

	default:
		t.send(key, "未知指令，輸入 /help 查看可用指令")
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

	status := fmt.Sprintf("🤖 *多代理系統狀態*\n\n")

	if globalAgentCoordinator.IsEnabled() {
		status += "✅ *狀態*: 已啟用\n\n"
	} else {
		status += "❌ *狀態*: 已停用\n\n"
	}

	totalAgents := stats["total_agents"].(int)
	status += fmt.Sprintf("📊 *統計*:\n  總代理數: %d\n\n", totalAgents)

	if activeTask, hasTask := stats["active_task"]; hasTask && activeTask != nil {
		taskInfo := activeTask.(map[string]interface{})
		status += fmt.Sprintf("🔄 *執行中任務*:\n  ID: %s\n  狀態: %s\n\n",
			taskInfo["id"].(string), taskInfo["status"].(string))
	}

	status += "*可用代理類型*:\n"
	status += "• General - 通用代理\n"
	status += "• CodeReview - 程式碼審查\n"
	status += "• Testing - 測試專家\n"
	status += "• Documentation - 文件撰寫\n"
	status += "• Deployment - 部署專家\n"
	status += "• Debug - 除錯專家\n"

	t.sendMarkdown(key, status)
}

// handleMultiAgentStats shows detailed statistics about agent usage
func (t *TelegramBot) handleMultiAgentStats(key chatKey) {
	stats := globalAgentCoordinator.GetAgentStats()

	response := fmt.Sprintf("📊 *多代理使用統計*\n\n")

	if agents, hasAgents := stats["agents"]; hasAgents {
		agentStats := agents.(map[string]interface{})

		if len(agentStats) == 0 {
			response += "目前沒有活躍的專門化代理。\n"
		} else {
			for agentType, agentInfo := range agentStats {
				info := agentInfo.(map[string]interface{})
				taskCount := info["task_count"].(int)
				lastUsed := info["last_used"].(time.Time)

				response += fmt.Sprintf("🤖 *%s*\n", agentType)
				response += fmt.Sprintf("  任務數: %d\n", taskCount)
				response += fmt.Sprintf("  最後使用: %s\n\n",
					lastUsed.Format("2006-01-02 15:04:05"))
			}
		}
	}

	t.sendMarkdown(key, response)
}

// handleAgentsList shows available agent types and their capabilities
func (t *TelegramBot) handleAgentsList(key chatKey) {
	response := "🤖 *可用代理類型*\n\n"

	agentTypes := []struct {
		name        string
		description string
		skills      []string
	}{
		{"General", "通用代理", []string{"一般協助", "程式碼生成", "檔案操作"}},
		{"CodeReview", "程式碼審查專家", []string{"程式碼分析", "安全審查", "效能審查", "最佳實務"}},
		{"Testing", "測試專家", []string{"單元測試", "整合測試", "測試自動化", "覆蓋率分析"}},
		{"Documentation", "文件專家", []string{"API 文件", "README 撰寫", "程式碼註解", "使用指南"}},
		{"Deployment", "部署專家", []string{"CI/CD", "Docker", "Kubernetes", "雲端部署", "監控"}},
		{"Debug", "除錯專家", []string{"錯誤分析", "日誌分析", "效能除錯", "問題排解"}},
	}

	for _, agent := range agentTypes {
		response += fmt.Sprintf("**%s**\n", agent.name)
		response += fmt.Sprintf("描述: %s\n", agent.description)
		response += fmt.Sprintf("技能: %s\n\n", strings.Join(agent.skills, ", "))
	}

	response += "*使用方式*:\n"
	response += "• 直接描述任務，系統會自動選擇最適合的代理\n"
	response += "• 使用 `/multiagent enable` 啟用智慧路由\n"
	response += "• 複雜任務會自動協調多個代理協作\n"

	t.sendMarkdown(key, response)
}

// handleDashboard shows system dashboard information
func (t *TelegramBot) handleDashboard(key chatKey) {
	log.Printf("[telegram] handleDashboard called for chat %d", key.chatID)
	dashboard := "📊 *Alice AI Agent Dashboard*\n\n"

	// System Health
	dashboard += "🏥 *系統健康狀態*:\n"
	if globalWebSocketHub != nil {
		dashboard += "  ✅ WebSocket Hub: 運行中\n"
		connectedClients := globalWebSocketHub.GetConnectedClients()
		dashboard += fmt.Sprintf("  🔌 連接數: %d\n", connectedClients)
	}

	if globalCheckpointManager != nil && globalCheckpointManager.IsEnabled() {
		dashboard += "  ✅ 檢查點系統: 已啟用\n"
	} else {
		dashboard += "  ❌ 檢查點系統: 已停用\n"
	}

	if globalAgentCoordinator != nil && globalAgentCoordinator.IsEnabled() {
		dashboard += "  ✅ 多代理協調: 已啟用\n"
	} else {
		dashboard += "  ❌ 多代理協調: 已停用\n"
	}

	// Web Interface
	if t.config.EnableWebInterface {
		dashboard += fmt.Sprintf("\n🌐 *Web 監控介面*:\n")
		dashboard += fmt.Sprintf("  📊 主面板: http://localhost:%s/\n", t.config.WebPort)
		dashboard += fmt.Sprintf("  📈 Timeline: http://localhost:%s/timeline.html\n", t.config.WebPort)
		dashboard += fmt.Sprintf("  🧪 測試頁面: http://localhost:%s/test-timeline.html\n", t.config.WebPort)
	}

	// Storage Info
	if globalStorage != nil {
		dashboard += "\n💾 *資料存儲狀態*:\n"
		dashboard += fmt.Sprintf("  📁 資料庫: %s\n", t.config.DatabasePath)
		dashboard += "  ✅ SQLite: 運行中\n"
	}

	// Quick Actions
	dashboard += "\n🚀 *快速操作*:\n"
	dashboard += "• `/checkpoints` - 查看檢查點狀態\n"
	dashboard += "• `/status` - 查看代理狀態\n"
	dashboard += "• `/multiagent status` - 查看多代理系統\n"
	dashboard += "• 使用下方按鈕快速刷新或查看檢查點\n"

	// Send dashboard with Web App button
	t.sendDashboardWithWebApp(key, dashboard)
}

// handleCheckpointsList shows checkpoint information
func (t *TelegramBot) handleCheckpointsList(key chatKey) {
	if globalCheckpointManager == nil {
		t.send(key, "❌ 檢查點系統未啟用")
		return
	}

	agent := t.getAgent(key)
	projectDir := agent.ProjectDir()

	checkpoints, err := globalCheckpointManager.ListCheckpoints(projectDir, 10)
	if err != nil {
		t.send(key, fmt.Sprintf("❌ 獲取檢查點列表失敗: %v", err))
		return
	}

	response := "📸 *檢查點狀態*\n\n"
	response += fmt.Sprintf("📂 專案: `%s`\n", projectDir)
	response += fmt.Sprintf("📊 總數: %d 個檢查點\n\n", len(checkpoints))

	if len(checkpoints) == 0 {
		response += "🔍 目前沒有檢查點\n\n"
		response += "*提示*: 檢查點會在危險操作前自動創建"
	} else {
		response += "*最近的檢查點*:\n"
		for i, cp := range checkpoints {
			if i >= 5 { // 最多顯示 5 個
				break
			}
			response += fmt.Sprintf("• `%s`\n", cp.ID[:12])
			response += fmt.Sprintf("  📝 %s\n", cp.Description)
			response += fmt.Sprintf("  📅 %s\n", cp.Timestamp.Format("01/02 15:04"))
			response += fmt.Sprintf("  💾 %d bytes\n\n", cp.Size)
		}
	}

	t.sendMarkdown(key, response)
}

// handleCheckpointsStats shows checkpoint statistics
func (t *TelegramBot) handleCheckpointsStats(key chatKey) {
	if globalCheckpointManager == nil {
		t.send(key, "❌ 檢查點系統未啟用")
		return
	}

	agent := t.getAgent(key)
	projectDir := agent.ProjectDir()

	stats, err := globalCheckpointManager.GetCheckpointStats(projectDir)
	if err != nil {
		t.send(key, fmt.Sprintf("❌ 獲取檢查點統計失敗: %v", err))
		return
	}

	response := "📈 *檢查點統計*\n\n"
	response += fmt.Sprintf("📂 專案: `%s`\n\n", projectDir)

	if totalCheckpoints, ok := stats["total_checkpoints"].(int64); ok {
		response += fmt.Sprintf("📊 總檢查點: %d\n", totalCheckpoints)
	}

	if totalSize, ok := stats["total_size"].(int64); ok {
		response += fmt.Sprintf("💾 總大小: %d bytes\n", totalSize)
	}

	if avgSize, ok := stats["average_size"].(float64); ok {
		response += fmt.Sprintf("📏 平均大小: %.1f bytes\n", avgSize)
	}

	response += "\n🔄 *自動檢查點觸發*:\n"
	response += "• 檔案寫入/修改操作\n"
	response += "• 危險命令執行 (rm, mv 等)\n"
	response += "• 重要配置變更\n"

	t.sendMarkdown(key, response)
}

// sendDashboardWithWebApp sends dashboard with refresh button
func (t *TelegramBot) sendDashboardWithWebApp(key chatKey, text string) {
	// Clean invalid UTF-8 characters to prevent API errors
	cleanText := sanitizeUTF8(text)

	// Create inline keyboard with refresh button only (Web App requires HTTPS)
	keyboard := map[string]interface{}{
		"inline_keyboard": [][]map[string]interface{}{
			{
				{
					"text": "🔄 刷新狀態",
					"callback_data": "refresh_dashboard",
				},
				{
					"text": "📸 檢查檢查點",
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
		t.answerCallbackQuery(queryID, "⛔ 你沒有使用權限。")
		return
	}

	// Handle different callback data
	switch {
	case data == "refresh_dashboard":
		// Send updated dashboard
		t.handleDashboard(key)
		t.answerCallbackQuery(queryID, "✅ 狀態已刷新")
	case data == "show_checkpoints":
		// Show checkpoints for current project
		t.handleCheckpointsList(key)
		t.answerCallbackQuery(queryID, "📸 檢查點信息已更新")
	case strings.HasPrefix(data, "stop_agent_"):
		// Handle stop button click
		agent := t.getAgent(key)
		if agent.IsProcessing() {
			if agent.Abort() {
				t.answerCallbackQuery(queryID, "🛑 已中斷正在執行的任務")
				log.Printf("Agent task stopped by user via callback button (chat: %d, thread: %d)", key.chatID, key.threadID)
			} else {
				t.answerCallbackQuery(queryID, "❌ 無法中斷任務")
			}
		} else {
			t.answerCallbackQuery(queryID, "ℹ️ 沒有正在執行的任務")
		}
	default:
		t.answerCallbackQuery(queryID, "❓ 未知操作")
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

	keyboard := map[string]interface{}{
		"inline_keyboard": [][]map[string]interface{}{
			{
				{
					"text":          "🛑 中斷",
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
	projectName := filepath.Base(strings.TrimRight(projectDir, "/"))

	// 嘗試從 GitHub Issues API 取得任務（優先）
	repo, err := detectGitHubRepo(projectDir)
	if err == nil {
		milestones, fetchErr := fetchGitHubMilestones(repo)
		if fetchErr == nil && len(milestones) > 0 {
			t.handleTasksFromGitHub(key, projectName, repo, milestones)
			return
		}
		// 記錄 GitHub API 失敗原因，但不直接顯示給用戶
		log.Printf("[telegram] GitHub API failed for %s: %v", repo, fetchErr)
	} else {
		log.Printf("[telegram] Not a GitHub repo or detection failed: %v", err)
	}

	// Fallback: 從 MASTER_TASKS.md 解析
	t.handleTasksFromFile(key, projectDir, projectName)
}

// handleTasksFromGitHub 從 GitHub Issues + Milestones 顯示任務進度
func (t *TelegramBot) handleTasksFromGitHub(key chatKey, projectName, repo string, milestones []ghMilestone) {
	var response strings.Builder
	response.WriteString(fmt.Sprintf("📊 %s 專案進度\n", projectName))
	response.WriteString(fmt.Sprintf("(via GitHub Issues: %s)\n\n", repo))

	var totalOpen, totalClosed int
	var activePhases []string

	for _, ms := range milestones {
		total := ms.OpenIssues + ms.ClosedIssues
		progress := 0
		if total > 0 {
			progress = ms.ClosedIssues * 100 / total
		}
		totalOpen += ms.OpenIssues
		totalClosed += ms.ClosedIssues

		status := "📋"
		if total == 0 {
			status = "—"
		} else if progress == 100 {
			status = "✅"
		} else if progress > 0 {
			status = "🔄"
		}

		if ms.OpenIssues > 0 {
			activePhases = append(activePhases,
				fmt.Sprintf("%s %s (%d%%)\n  📋 %d open / ✅ %d closed",
					status, ms.Title, progress, ms.OpenIssues, ms.ClosedIssues))
		} else if total > 0 {
			// 已完成的 phase 簡短顯示
		} else {
			// 空 phase
		}

		_ = status // used in full display mode
	}

	totalAll := totalOpen + totalClosed
	completedPhases := 0
	for _, ms := range milestones {
		total := ms.OpenIssues + ms.ClosedIssues
		if total > 0 && ms.OpenIssues == 0 {
			completedPhases++
		}
	}

	if totalOpen == 0 {
		response.WriteString(fmt.Sprintf("🎉 所有階段已完成！\n\n"))
		response.WriteString(fmt.Sprintf("✅ %d 個階段 / %d 個 Issues 全部完成\n\n", completedPhases, totalClosed))
		response.WriteString("📊 階段摘要:\n")
		for _, ms := range milestones {
			total := ms.OpenIssues + ms.ClosedIssues
			if total > 0 {
				response.WriteString(fmt.Sprintf("  ✅ %s (%d issues)\n", ms.Title, total))
			}
		}
	} else {
		response.WriteString(fmt.Sprintf("📋 Open: %d  ✅ Closed: %d  Total: %d\n\n", totalOpen, totalClosed, totalAll))
		for _, ph := range activePhases {
			response.WriteString(ph + "\n\n")
		}
		if completedPhases > 0 {
			response.WriteString(fmt.Sprintf("✅ 另有 %d 個階段已完成\n", completedPhases))
		}
	}

	t.send(key, response.String())
}

// handleTasksFromFile 從 MASTER_TASKS.md 解析任務（fallback）
func (t *TelegramBot) handleTasksFromFile(key chatKey, projectDir, projectName string) {
	tasksFile := filepath.Join(projectDir, "docs", "MASTER_TASKS.md")
	phases, err := t.parseMasterTasks(tasksFile)
	if err != nil {
		// 檢查是否是 GitHub repo
		if _, repoErr := detectGitHubRepo(projectDir); repoErr == nil {
			// 是 GitHub repo 但讀取失敗，建議檢查 gh CLI 認證
			t.send(key, fmt.Sprintf("❌ 無法取得任務資料\n\n可能原因：\n• GitHub API 認證問題 - 請檢查 `gh auth status`\n• MASTER_TASKS.md 不存在 - 請執行 /task-sync\n\n詳細錯誤: %v", err))
		} else {
			// 不是 GitHub repo，建議初始化
			t.send(key, fmt.Sprintf("❌ 無法讀取任務清單: %v\n\n💡 請先執行 /task-init 設定 GitHub Milestones", err))
		}
		return
	}

	var pendingTasks []string
	var totalPendingCount int

	for _, phase := range phases {
		if len(phase.Tasks) > 0 {
			phaseHeader := fmt.Sprintf("📋 *%s* (%d%%, %s)",
				phase.Name, phase.Progress, phase.Status)
			var phaseTasks []string

			for _, task := range phase.Tasks {
				taskStatus := ""
				switch task.Status {
				case "📋":
					taskStatus = "📋 規劃中"
				case "🔄":
					taskStatus = "🔄 開發中"
				case "🧪":
					taskStatus = "🧪 測試中"
				default:
					continue
				}
				taskLine := fmt.Sprintf("  %s %s", taskStatus, task.Description)
				if task.IssueLink != "" {
					taskLine += fmt.Sprintf(" %s", task.IssueLink)
				}
				phaseTasks = append(phaseTasks, taskLine)
				totalPendingCount++
			}

			if len(phaseTasks) > 0 {
				pendingTasks = append(pendingTasks, phaseHeader)
				pendingTasks = append(pendingTasks, phaseTasks...)
				pendingTasks = append(pendingTasks, "")
			}
		}
	}

	var response string
	if totalPendingCount == 0 {
		completedPhases := 0
		for _, phase := range phases {
			if phase.Progress >= 100 {
				completedPhases++
			}
		}
		response = fmt.Sprintf("🎉 *%s - 專案完成！*\n\n", projectName)
		response += fmt.Sprintf("✅ 所有 %d 個開發階段已 100%% 完成\n\n", completedPhases)
		response += "📊 完成項目摘要:\n"
		for _, phase := range phases {
			response += fmt.Sprintf("• %s %s\n", phase.Name, phase.Status)
		}
		response += "\n📚 查看完整任務詳情: `docs/MASTER_TASKS.md`"
	} else {
		response = fmt.Sprintf("📋 *%s 待辦清單* _(from MD)_\n\n", projectName)
		response += fmt.Sprintf("📊 總計: %d 個待辦任務\n\n", totalPendingCount)
		response += strings.Join(pendingTasks, "\n")
		response += "\n💡 使用 /task-init 設定 GitHub Milestones 以啟用 Issues 同步"
	}

	t.sendMarkdown(key, response)
}

// --- GitHub API helpers ---

// ghMilestone represents a GitHub milestone
type ghMilestone struct {
	Number       int    `json:"number"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	State        string `json:"state"`
	OpenIssues   int    `json:"open_issues"`
	ClosedIssues int    `json:"closed_issues"`
}

// detectGitHubRepo extracts "owner/repo" from git remote origin URL
func detectGitHubRepo(projectDir string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = projectDir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repo or no remote: %w", err)
	}

	remote := strings.TrimSpace(string(output))

	// git@github.com:owner/repo.git
	if strings.HasPrefix(remote, "git@github.com:") {
		repo := strings.TrimPrefix(remote, "git@github.com:")
		repo = strings.TrimSuffix(repo, ".git")
		return repo, nil
	}

	// https://github.com/owner/repo.git
	if strings.Contains(remote, "github.com/") {
		idx := strings.Index(remote, "github.com/")
		repo := remote[idx+len("github.com/"):]
		repo = strings.TrimSuffix(repo, ".git")
		return repo, nil
	}

	return "", fmt.Errorf("not a GitHub repo: %s", remote)
}

// fetchGitHubMilestones queries GitHub API for all milestones using gh CLI
func fetchGitHubMilestones(repo string) ([]ghMilestone, error) {
	// 使用 gh api CLI 命令，利用已有的認證
	// 參數作為 URL 查詢參數傳遞
	apiURL := fmt.Sprintf("repos/%s/milestones?state=all&sort=title&direction=asc&per_page=100", repo)
	cmd := exec.Command("gh", "api", apiURL)

	output, err := cmd.Output()
	if err != nil {
		// 檢查是否是 gh CLI 認證問題
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh CLI error (status %d): %s", exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("gh CLI execution error: %w", err)
	}

	var milestones []ghMilestone
	if err := json.Unmarshal(output, &milestones); err != nil {
		return nil, fmt.Errorf("JSON decode error: %w", err)
	}

	return milestones, nil
}

// TaskInfo 表示單個任務資訊
type TaskInfo struct {
	Number      string // 任務編號 (如 "8.6")
	Description string // 任務描述
	IssueLink   string // GitHub Issue 連結
	Status      string // 狀態符號 (📋/🔄/🧪/✅)
}

// PhaseInfo 表示階段資訊
type PhaseInfo struct {
	Name     string     // 階段名稱
	Progress int        // 進度百分比
	Status   string     // 狀態符號
	Tasks    []TaskInfo // 任務列表
}

// parseMasterTasks 解析 MASTER_TASKS.md 檔案，支援多種格式
// 回傳所有 phase（含已完成的），caller 自行過濾待辦任務
func (t *TelegramBot) parseMasterTasks(filePath string) ([]PhaseInfo, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("無法開啟檔案 %s: %w", filePath, err)
	}

	lines := strings.Split(string(data), "\n")
	var phases []PhaseInfo
	var currentPhase *PhaseInfo
	isInOverview := false

	// 通用正規表達式
	// 進度表格行：任何包含 N% 的表格行
	progressInTable := regexp.MustCompile(`(\d+)%`)
	// Phase 標題格式1 (Alice): ## P1 - Core Backend (✅ 100%)
	phaseHeaderA := regexp.MustCompile(`^##\s+(P\d+(?:\.\d+)?\s*-\s*.+?)\s*\(([^)]+)\)`)
	// Phase 標題格式2 (通用): ## Phase 0: 專案初始化 ✅ (40h)
	phaseHeaderB := regexp.MustCompile(`^##\s+Phase\s+\d+.*?:\s*(.+)`)
	// Checkbox 任務: - [ ] 或 - [x]
	checkboxTask := regexp.MustCompile(`^-\s+\[([ x])\]\s+(.+)`)
	// Issue 連結
	issueLink := regexp.MustCompile(`\[#(\d+)\]\([^)]+\)`)
	// 表格任務行 (Alice): | 1.1 | desc | issue | status |
	tableTask := regexp.MustCompile(`^\|\s*(\d+\.\d+|)\s*\|\s*(.+?)\s*\|\s*(\[#\d+\]\([^)]+\)|—|TBD)\s*\|\s*([📋🔄🧪✅⏸️])\s*\|`)
	// 通用表格任務行: 最後一欄為狀態 emoji 的任何表格行
	genericTableTask := regexp.MustCompile(`^\|[^|]+\|\s*(.+?)\s*\|[^|]+\|\s*([📋🔄🧪✅⏸️❌])\s*\|`)

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)

		// 偵測總覽表格開始（支援多種格式）
		// "## Phase Overview" / "## 總覽" / "## 快速導覽"
		if strings.HasPrefix(line, "## ") && (strings.Contains(line, "Phase Overview") ||
			strings.Contains(line, "總覽") || strings.Contains(line, "快速導覽")) {
			isInOverview = true
			continue
		}

		// 解析總覽表格
		if isInOverview {
			if line == "---" || (strings.HasPrefix(line, "## ") && !strings.Contains(line, "---")) {
				isInOverview = false
				// 不 continue，讓下面的 phase header 邏輯也能處理
				if !strings.HasPrefix(line, "## ") {
					continue
				}
			} else if strings.HasPrefix(line, "|") && !strings.Contains(line, "---") {
				// 跳過表頭行
				cells := splitTableCells(line)
				if len(cells) < 3 {
					continue
				}
				// 嘗試找到進度百分比
				progressMatch := progressInTable.FindStringSubmatch(line)
				if len(progressMatch) < 2 {
					continue
				}
				progress, _ := strconv.Atoi(progressMatch[1])

				// 第一欄通常是 phase code
				phaseName := strings.TrimSpace(cells[0])
				// 如果有第二欄是名稱，合併顯示
				if len(cells) >= 2 {
					desc := strings.TrimSpace(cells[1])
					if desc != "" && desc != phaseName {
						phaseName = phaseName + " " + desc
					}
				}

				// 偵測狀態
				status := detectStatus(line)

				phases = append(phases, PhaseInfo{
					Name:     phaseName,
					Progress: progress,
					Status:   status,
				})
				continue
			}
		}

		// Phase 標題（進入某個 phase 的詳細區域）
		if strings.HasPrefix(line, "## ") {
			// 格式 A: ## P1 - Core Backend (✅ 100%)
			if m := phaseHeaderA.FindStringSubmatch(line); len(m) == 3 {
				phaseName := strings.TrimSpace(m[1])
				currentPhase = findPhaseByPrefix(&phases, phaseName)
				continue
			}
			// 格式 B: ## Phase 0: 專案初始化 ✅ (40h)
			if m := phaseHeaderB.FindStringSubmatch(line); len(m) == 2 {
				currentPhase = findPhaseByOverviewLine(&phases, line)
				continue
			}
			// 格式 C: 通用 fallback (例如 ## PHASE1 — 後端基礎設施層 ✅)
			currentPhase = findPhaseByOverviewLine(&phases, line)
			continue
		}

		if currentPhase == nil {
			continue
		}

		// 解析 checkbox 任務: - [ ] / - [x]
		if m := checkboxTask.FindStringSubmatch(line); len(m) == 3 {
			isDone := m[1] == "x"
			desc := strings.TrimSpace(m[2])

			// 提取 issue link
			link := ""
			if il := issueLink.FindString(desc); il != "" {
				link = il
				// 從描述中移除 issue link 讓顯示更乾淨
				desc = strings.TrimSpace(issueLink.ReplaceAllString(desc, ""))
			}

			status := "✅"
			if !isDone {
				status = "📋"
			}
			currentPhase.Tasks = append(currentPhase.Tasks, TaskInfo{
				Description: desc,
				IssueLink:   link,
				Status:      status,
			})
			continue
		}

		// 解析表格任務行
		if strings.HasPrefix(line, "|") && !strings.Contains(line, "Task") && !strings.Contains(line, "任務") && !strings.Contains(line, "---") {
			// 格式 A (Alice): | 1.1 | desc | issue | status |
			if m := tableTask.FindStringSubmatch(line); len(m) == 5 {
				currentPhase.Tasks = append(currentPhase.Tasks, TaskInfo{
					Number:      strings.TrimSpace(m[1]),
					Description: strings.TrimSpace(m[2]),
					IssueLink:   strings.TrimSpace(m[3]),
					Status:      strings.TrimSpace(m[4]),
				})
			} else if m := genericTableTask.FindStringSubmatch(line); len(m) == 3 {
				// 通用格式: 第二欄為描述，最後欄為狀態
				currentPhase.Tasks = append(currentPhase.Tasks, TaskInfo{
					Description: strings.TrimSpace(m[1]),
					Status:      strings.TrimSpace(m[2]),
				})
			}
		}
	}

	return phases, nil
}

// splitTableCells 分割 markdown 表格的一行為各欄
func splitTableCells(line string) []string {
	line = strings.Trim(line, "|")
	parts := strings.Split(line, "|")
	var cells []string
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}

// detectStatus 從一行文字偵測狀態 emoji
func detectStatus(line string) string {
	if strings.Contains(line, "✅") {
		return "✅"
	}
	if strings.Contains(line, "🔄") {
		return "🔄"
	}
	if strings.Contains(line, "🧪") {
		return "🧪"
	}
	if strings.Contains(line, "📋") {
		return "📋"
	}
	if strings.Contains(line, "⏸") {
		return "⏸️"
	}
	return "📋"
}

// findPhaseByPrefix 從 phases 中找到 name 前綴匹配的 phase
func findPhaseByPrefix(phases *[]PhaseInfo, name string) *PhaseInfo {
	prefix := strings.Split(name, " - ")[0]
	prefix = strings.Split(prefix, " ")[0] // 取 "P1" 部分
	// 精確匹配: prefix 後必須接空格、連字號或行尾，避免 "P1" 匹配 "P13"
	for i := range *phases {
		pName := (*phases)[i].Name
		if strings.HasPrefix(pName, prefix) {
			rest := pName[len(prefix):]
			if rest == "" || rest[0] == ' ' || rest[0] == '-' {
				return &(*phases)[i]
			}
		}
	}
	return nil
}

// findPhaseByOverviewLine 從 phase header line 找到對應的總覽 phase
// 使用計分制：匹配越多關鍵字的 phase 優先
func findPhaseByOverviewLine(phases *[]PhaseInfo, headerLine string) *PhaseInfo {
	headerLower := strings.ToLower(headerLine)
	bestIdx := -1
	bestScore := 0
	// 跳過常見的分隔符和狀態 emoji
	skipWords := map[string]bool{"—": true, "-": true, "|": true, "✅": true, "🔄": true, "🧪": true, "📋": true, "⏸️": true, "❌": true}
	for i := range *phases {
		nameParts := strings.Fields((*phases)[i].Name)
		score := 0
		for _, part := range nameParts {
			if len(part) <= 2 || skipWords[part] {
				continue
			}
			if strings.Contains(headerLower, strings.ToLower(part)) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	if bestIdx >= 0 {
		return &(*phases)[bestIdx]
	}
	return nil
}

// handlePhotoMessageBatch 處理圖片訊息，支援多張圖片批次處理
func (t *TelegramBot) handlePhotoMessageBatch(key chatKey, userID int64, photo []PhotoSize, caption string, mediaGroupID string) {
	// 檢查多媒體支援是否開啟
	if !t.config.Multimedia.EnablePhotoSupport {
		t.send(key, "📷 圖片分析功能目前未啟用。請聯繫管理員開啟 `enable_photo_support` 設定。")
		return
	}

	// 如果沒有 mediaGroupID，這是單張圖片的多個尺寸，直接處理單張圖片
	if mediaGroupID == "" {
		log.Printf("[telegram] single photo with %d size variants, processing as single image", len(photo))
		t.handleSinglePhoto(key, userID, photo, caption)
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
		t.send(batch.ChatKey, "📷 正在分析圖片...")
		t.handleSinglePhoto(batch.ChatKey, batch.UserID, []PhotoSize{batch.Photos[0]}, batch.Caption)
	} else {
		// 多張圖片，批次處理
		t.send(batch.ChatKey, fmt.Sprintf("📷 正在分析 %d 張圖片...", len(batch.Photos)))
		t.handleMultiplePhotos(batch.ChatKey, batch.UserID, batch.Photos, batch.Caption)
	}
}

// handleMultiplePhotos 處理多張圖片的批次分析
func (t *TelegramBot) handleMultiplePhotos(key chatKey, userID int64, photos []PhotoSize, caption string) {
	// 取得 Agent 和專案目錄
	agent := t.getAgent(key)
	projectDir := agent.ProjectDir()

	// 確保專案臨時目錄存在
	projectTempDir := filepath.Join(projectDir, "temp")
	if err := os.MkdirAll(projectTempDir, 0755); err != nil {
		log.Printf("[telegram] create project temp dir error: %v", err)
		t.send(key, "📷 建立專案臨時目錄失敗。")
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
			t.send(key, fmt.Sprintf("📷 第 %d 張圖片檔案過大（%s），限制為 %dMB。",
				i+1, formatFileSize(targetPhoto.FileSize), t.config.Multimedia.MaxFileSizeMB))
			continue
		}

		// 下載到 Alice 臨時目錄
		aliceImagePath, err := t.DownloadTelegramFile(targetPhoto.FileID, "photo")
		if err != nil {
			log.Printf("[telegram] download photo %d error: %v", i+1, err)
			t.send(key, fmt.Sprintf("📷 第 %d 張圖片下載失敗", i+1))
			continue
		}
		aliceImagePaths = append(aliceImagePaths, aliceImagePath)

		// 複製到專案臨時目錄
		fileName := filepath.Base(aliceImagePath)
		projectImagePath := filepath.Join(projectTempDir, fileName)
		if err := copyFile(aliceImagePath, projectImagePath); err != nil {
			log.Printf("[telegram] copy photo %d to project error: %v", i+1, err)
			t.send(key, fmt.Sprintf("📷 第 %d 張圖片複製失敗", i+1))
			continue
		}
		projectImagePaths = append(projectImagePaths, projectImagePath)

		// 記錄相對路徑
		relativePath := filepath.Join("temp", fileName)
		relativeImagePaths = append(relativeImagePaths, relativePath)
	}

	if len(relativeImagePaths) == 0 {
		t.send(key, "📷 所有圖片處理失敗，請稍後再試。")
		return
	}

	// 組合多張圖片的 prompt，caption 為主指令時優先
	imageList := ""
	for i, relativePath := range relativeImagePaths {
		imageList += fmt.Sprintf("圖片 %d: %s\n", i+1, relativePath)
	}

	var prompt string
	if caption != "" {
		prompt = fmt.Sprintf("%s\n\n（參考附件 %d 張圖片：\n%s）", caption, len(relativeImagePaths), imageList)
	} else {
		prompt = fmt.Sprintf("請分析這 %d 張圖片，並進行比較分析：\n%s", len(relativeImagePaths), imageList)
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
			filteredCaption, detected := globalSecurityManager.DetectAndFilterPII(caption, true)
			if len(detected) > 0 {
				// 額外的 Telegram 上下文記錄 (降低嚴重性避免重複警告)
				globalSecurityManager.LogSecurityEvent(SecurityEvent{
					EventType:   "pii_detected_batch_caption",
					Severity:    "low", // 降低嚴重性，主要事件已由 DetectAndFilterPII 記錄
					Description: fmt.Sprintf("Batch photo caption contained PII (filtered): %v", detected),
					UserID:      userID,
					Details: map[string]interface{}{
						"detected_types": detected,
						"chat_id":        key.chatID,
						"context":        "telegram_batch_photo",
					},
				})
				t.send(key, "⚠️ 圖片說明中偵測到敏感資訊已自動過濾。")
				caption = filteredCaption
				// 重新組合 prompt
				prompt = fmt.Sprintf("%s\n\n（參考附件 %d 張圖片：\n%s）", caption, len(relativeImagePaths), imageList)
			}
		}
	}

	// 發送給 Agent 處理 (使用現有會話，就像語音處理一樣)
	agent = t.getAgent(key)

	response, err := agent.Run(prompt, func(update string, silent bool) {
		if silent {
			t.sendSilent(key, update)
		} else {
			t.send(key, update)
		}
	})

	if err != nil {
		log.Printf("[telegram] batch photo analysis error: %v", err)
		t.send(key, "❌ 圖片批次分析失敗，請稍後再試。")
		return
	}

	if response != "" {
		t.sendLong(key, response)
	}
}

// handleSinglePhoto 處理單張圖片（保留原有邏輯但提取為獨立函數）
func (t *TelegramBot) handleSinglePhoto(key chatKey, userID int64, photo []PhotoSize, caption string) {
	// 取得最高解析度的圖片（通常是陣列最後一個）
	if len(photo) == 0 {
		return
	}
	targetPhoto := photo[len(photo)-1]

	// 檢查檔案大小限制
	maxSizeBytes := t.config.Multimedia.MaxFileSizeMB * 1024 * 1024
	if targetPhoto.FileSize > maxSizeBytes {
		t.send(key, fmt.Sprintf("📷 圖片檔案過大（%s），限制為 %dMB。",
			formatFileSize(targetPhoto.FileSize), t.config.Multimedia.MaxFileSizeMB))
		return
	}

	// 下載圖片到 Alice 臨時目錄
	aliceImagePath, err := t.DownloadTelegramFile(targetPhoto.FileID, "photo")
	if err != nil {
		log.Printf("[telegram] download photo error: %v", err)
		t.send(key, "📷 下載圖片失敗，請稍後再試。")
		return
	}

	// 取得 Agent 和專案目錄
	agent := t.getAgent(key)
	projectDir := agent.ProjectDir()

	// 確保專案臨時目錄存在
	projectTempDir := filepath.Join(projectDir, "temp")
	if err := os.MkdirAll(projectTempDir, 0755); err != nil {
		log.Printf("[telegram] create project temp dir error: %v", err)
		t.send(key, "📷 建立專案臨時目錄失敗。")
		os.Remove(aliceImagePath) // 清理 Alice 臨時檔案
		return
	}

	// 複製圖片到專案臨時目錄
	fileName := filepath.Base(aliceImagePath)
	projectImagePath := filepath.Join(projectTempDir, fileName)

	if err := copyFile(aliceImagePath, projectImagePath); err != nil {
		log.Printf("[telegram] copy photo to project error: %v", err)
		t.send(key, "📷 複製圖片到專案目錄失敗。")
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
		prompt = fmt.Sprintf("%s\n\n（參考附件圖片: %s）", caption, relativePath)
	} else {
		prompt = fmt.Sprintf("請分析這張圖片: %s", relativePath)
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
			filteredCaption, detected := globalSecurityManager.DetectAndFilterPII(caption, true)
			if len(detected) > 0 {
				// 額外的 Telegram 上下文記錄 (降低嚴重性避免重複警告)
				globalSecurityManager.LogSecurityEvent(SecurityEvent{
					EventType:   "pii_detected_photo_caption",
					Severity:    "low", // 降低嚴重性，主要事件已記錄
					Description: fmt.Sprintf("Photo caption contained PII (filtered): %v", detected),
					UserID:      userID,
					Details: map[string]interface{}{
						"detected_types": detected,
						"chat_id":        key.chatID,
						"context":        "telegram_photo",
					},
				})
				t.send(key, "⚠️ 圖片說明中偵測到敏感資訊已自動過濾。")
				caption = filteredCaption
				prompt = fmt.Sprintf("%s\n\n（參考附件圖片: %s）", caption, relativePath)
			}
		}
	}

	// 發送給 Agent 處理 (使用現有會話，就像語音處理一樣)
	agent = t.getAgent(key)
	t.send(key, "📷 正在分析圖片...")

	response, err := agent.Run(prompt, func(update string, silent bool) {
		if silent {
			t.sendSilent(key, update)
		} else {
			t.send(key, update)
		}
	})

	if err != nil {
		log.Printf("[telegram] single photo analysis error: %v", err)
		t.send(key, "❌ 圖片分析失敗，請稍後再試。")
		return
	}

	if response != "" {
		t.sendLong(key, response)
	}
}

// handlePhotoMessage 處理圖片訊息（原有函數，保留用於向後相容）
func (t *TelegramBot) handlePhotoMessage(key chatKey, userID int64, photo []PhotoSize, caption string) {
	// 檢查多媒體支援是否開啟
	if !t.config.Multimedia.EnablePhotoSupport {
		t.send(key, "📷 圖片分析功能目前未啟用。請聯繫管理員開啟 `enable_photo_support` 設定。")
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
		t.send(key, fmt.Sprintf("📷 圖片檔案過大（%s），限制為 %dMB。",
			formatFileSize(targetPhoto.FileSize), t.config.Multimedia.MaxFileSizeMB))
		return
	}

	// 下載圖片
	imagePath, err := t.DownloadTelegramFile(targetPhoto.FileID, "photo")
	if err != nil {
		log.Printf("[telegram] download photo error: %v", err)
		t.send(key, "📷 下載圖片失敗，請稍後再試。")
		return
	}

	// 確保在函數結束時清理臨時檔案
	defer func() {
		if err := os.Remove(imagePath); err != nil {
			log.Printf("[telegram] cleanup photo error: %v", err)
		}
	}()

	// 組合 prompt 讓 Claude 分析圖片
	prompt := fmt.Sprintf("請分析這張圖片: %s", imagePath)
	if caption != "" {
		prompt = fmt.Sprintf("%s\n\n用戶說明: %s", prompt, caption)
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
			filteredCaption, detected := globalSecurityManager.DetectAndFilterPII(caption, true)
			if len(detected) > 0 {
				// 額外的 Telegram 上下文記錄 (降低嚴重性避免重複警告)
				globalSecurityManager.LogSecurityEvent(SecurityEvent{
					EventType:   "pii_detected_photo_caption",
					Severity:    "low", // 降低嚴重性，主要事件已記錄
					Description: fmt.Sprintf("Single photo caption contained PII (filtered): %v", detected),
					UserID:      userID,
					Details: map[string]interface{}{
						"detected_types": detected,
						"chat_id":        key.chatID,
						"context":        "telegram_single_photo",
					},
				})
				t.send(key, "⚠️ 圖片說明中偵測到敏感資訊已自動過濾。")
				caption = filteredCaption
				prompt = fmt.Sprintf("請分析這張圖片: %s\n\n用戶說明: %s", imagePath, caption)
			}
		}
	}

	// 發送給 Agent 處理
	agent := t.getAgent(key)
	t.send(key, "📷 正在分析圖片...")

	response, err := agent.Run(prompt, func(update string, silent bool) {
		if silent {
			t.sendSilent(key, update)
		} else {
			t.send(key, update)
		}
	})
	if err != nil {
		log.Printf("[telegram] photo analysis error: %v", err)
		t.send(key, "📷 圖片分析失敗，請稍後再試。")
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
func (t *TelegramBot) handleVoiceMessage(key chatKey, userID int64, voice *Voice, caption string) {
	// 檢查語音支援是否開啟
	if !t.config.Multimedia.EnableVoiceSupport {
		t.send(key, "🎤 語音轉文字功能目前未啟用。請聯繫管理員開啟 `enable_voice_support` 設定。")
		return
	}

	// 檢查是否有 OpenAI API Key
	if t.config.Multimedia.OpenAIAPIKey == "" {
		t.send(key, "🎤 語音轉文字需要 OpenAI API Key，請聯繫管理員設定 `openai_api_key`。")
		return
	}

	// 檢查檔案大小限制
	maxSizeBytes := t.config.Multimedia.MaxFileSizeMB * 1024 * 1024
	if voice.FileSize > maxSizeBytes {
		t.send(key, fmt.Sprintf("🎤 語音檔案過大（%s），限制為 %dMB。",
			formatFileSize(voice.FileSize), t.config.Multimedia.MaxFileSizeMB))
		return
	}

	// 檢查語音長度限制（Whisper API 限制 25MB 或約 25 分鐘）
	if voice.Duration > 25*60 { // 25 分鐘
		t.send(key, fmt.Sprintf("🎤 語音訊息過長（%d 秒），限制為 25 分鐘。", voice.Duration))
		return
	}

	// 下載語音檔案
	t.send(key, "🎤 正在下載語音檔案...")
	voicePath, err := t.DownloadTelegramFile(voice.FileID, "voice")
	if err != nil {
		log.Printf("[telegram] download voice error: %v", err)
		t.send(key, "🎤 下載語音檔案失敗，請稍後再試。")
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
			filteredCaption, detected := globalSecurityManager.DetectAndFilterPII(caption, true)
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
				t.send(key, "⚠️ 語音說明中偵測到敏感資訊已自動過濾。")
				caption = filteredCaption
			}
		}
	}

	// 語音轉文字
	t.send(key, "🎤 正在轉錄語音內容...")
	transcribedText, err := t.transcribeVoiceWithWhisper(voicePath)
	if err != nil {
		log.Printf("[telegram] voice transcription error: %v", err)
		t.send(key, "🎤 語音轉錄失敗，請稍後再試。")
		return
	}

	if transcribedText == "" {
		t.send(key, "🎤 未能識別語音內容，請嘗試重新錄製。")
		return
	}

	// 顯示轉錄結果供用戶確認
	confirmationMsg := fmt.Sprintf("🎤 *語音轉錄結果*：\n\n「%s」\n\n正在傳送給 AI 分析...", transcribedText)
	t.sendMarkdown(key, confirmationMsg)

	// 組合 prompt 讓 Claude 處理轉錄的文字
	prompt := transcribedText
	if caption != "" {
		prompt = fmt.Sprintf("%s\n\n附加說明: %s", transcribedText, caption)
	}

	// 發送給 Agent 處理
	agent := t.getAgent(key)
	response, err := agent.Run(prompt, func(update string, silent bool) {
		if silent {
			t.sendSilent(key, update)
		} else {
			t.send(key, update)
		}
	})
	if err != nil {
		log.Printf("[telegram] voice analysis error: %v", err)
		t.send(key, "🎤 語音內容分析失敗，請稍後再試。")
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

// evaluateTaskComplexity 使用本地啟發式算法評估任務複雜度
// 基於多個信號判斷任務難度，無需外部 API 調用
// 返回值: "fast" (簡單任務) 或 "deep" (複雜任務)
func evaluateTaskComplexity(userMessage string) string {
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
		"跨",  // 跨檔案、跨模組
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
func (t *TelegramBot) handleDocumentMessage(key chatKey, userID int64, document *Document, caption string) {
	// 檢查檔案大小限制
	maxSizeBytes := t.config.Multimedia.MaxFileSizeMB * 1024 * 1024
	if document.FileSize > maxSizeBytes {
		t.send(key, fmt.Sprintf("📁 文件檔案過大（%s），限制為 %dMB。",
			formatFileSize(document.FileSize), t.config.Multimedia.MaxFileSizeMB))
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
	t.send(key, "📁 正在下載文件...")
	documentPath, err := t.DownloadTelegramFile(document.FileID, "document")
	if err != nil {
		log.Printf("[telegram] download document error: %v", err)
		t.send(key, "📁 下載文件失敗，請稍後再試。")
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
			t.send(key, "📁 建立臨時目錄失敗。")
			return
		}

		// 複製到專案目錄，保留原始檔名
		finalPath = filepath.Join(tempDir, document.FileName)
		if err := copyFile(documentPath, finalPath); err != nil {
			log.Printf("[telegram] copy document to project error: %v", err)
			t.send(key, "📁 複製文件到專案目錄失敗。")
			return
		}

		log.Printf("[telegram] document copied to project: %s", finalPath)
	} else {
		finalPath = documentPath
	}

	// 構建 Claude 的輸入提示
	prompt := fmt.Sprintf("用戶上傳了一個文件：%s", finalPath)
	if caption != "" {
		prompt += fmt.Sprintf("\n用戶說：%s", caption)
	}

	// 新增文件類型提示
	if document.MimeType != "" {
		prompt += fmt.Sprintf("\n文件類型：%s", document.MimeType)
	}

	prompt += "\n\n請分析這個文件並提供適當的協助。"

	log.Printf("[telegram] sending document analysis request: file=%s, size=%d, type=%s, prompt_len=%d",
		document.FileName, document.FileSize, document.MimeType, len(prompt))

	// 發送分析訊息
	t.send(key, "📁 正在分析文件...")
	response, err := agent.Run(prompt, func(update string, silent bool) {
		if silent {
			t.sendSilent(key, update)
		} else {
			t.send(key, update)
		}
	})
	if err != nil {
		log.Printf("[telegram] document analysis error: %v", err)
		t.send(key, "📁 文件分析失敗，請稍後再試。")
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
		return fmt.Errorf("路徑不存在：%s", projectPath)
	}
	if err != nil {
		return fmt.Errorf("無法存取路徑：%s (%v)", projectPath, err)
	}

	// 2. 檢查是否為目錄
	if !info.IsDir() {
		return fmt.Errorf("指定路徑不是目錄：%s", projectPath)
	}

	// 3. 檢查讀取權限
	if _, err := os.ReadDir(projectPath); err != nil {
		return fmt.Errorf("無法讀取目錄：%s (權限不足)", projectPath)
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
func (t *TelegramBot) detectProjectType(projectPath string) string {
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
		return "通用專案"
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

// abs 計算絕對值
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

