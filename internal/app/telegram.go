package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// chatKey 用於識別獨立對話（支援 Forum Topics）
type chatKey struct {
	chatID   int64
	threadID int
}

type TelegramBot struct {
	agents   map[chatKey]*Agent // 每個 chat/topic 一個 agent
	agentsMu sync.RWMutex       // 保護 agents map 的讀寫鎖
	client   *CLIClient
	allowIDs map[int64]bool // 白名單
	config   *Config
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

	return &TelegramBot{
		agents:   make(map[chatKey]*Agent),
		client:   client,
		allowIDs: allowIDs,
		config:   config,
	}, nil
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

	agent := NewAgent(t.client, t.config.DefaultProjectDir, key.chatID, key.threadID)
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
					Text string `json:"text"`
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
				go t.handleMessage(key, msg.From.ID, msg.Text)
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

func (t *TelegramBot) handleMessage(key chatKey, userID int64, text string) {
	// 權限檢查
	if !t.isAllowed(userID) {
		t.send(key, "⛔ 你沒有使用權限。")
		return
	}

	text = strings.TrimSpace(text)
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

		// PII 檢測和過濾
		filteredText, detected := globalSecurityManager.DetectAndFilterPII(text)
		if len(detected) > 0 {
			// 記錄 PII 檢測事件
			globalSecurityManager.LogSecurityEvent(SecurityEvent{
				EventType:   "pii_detected_telegram",
				Severity:    "high",
				Description: fmt.Sprintf("PII detected in Telegram message: %v", detected),
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

	// 發送「處理中」提示
	t.sendTyping(key)

	var response string
	var err error

	// Check if multi-agent coordination should be used
	if globalAgentCoordinator.IsEnabled() && globalAgentCoordinator.ShouldUseMultiAgent(text) {
		// Use coordinated multi-agent execution
		response, err = globalAgentCoordinator.ExecuteCoordinatedTask(text, agent, func(update string, silent bool) {
			if silent {
				t.sendSilent(key, update)
			} else {
				t.send(key, update)
			}
		})
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
			}, func(update string, silent bool) {
				if silent {
					t.sendSilent(key, update)
				} else {
					t.send(key, update)
				}
			})
		} else {
			// Fall back to regular agent
			response, err = agent.Run(text, func(update string, silent bool) {
				if silent {
					t.sendSilent(key, update)
				} else {
					t.send(key, update)
				}
			})
		}
	} else {
		// Regular single agent execution
		response, err = agent.Run(text, func(update string, silent bool) {
			if silent {
				t.sendSilent(key, update)
			} else {
				t.send(key, update)
			}
		})
	}

	if err != nil {
		t.send(key, fmt.Sprintf("❌ 錯誤: %v", err))
		return
	}

	if response == "" {
		response = "（完成，無文字回覆）"
	}

	// Telegram 訊息限制 4096 字元，分段發送
	t.sendLong(key, response)
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

*指令：*
/project <路徑> — 切換專案目錄
/reset — 清除對話歷史
/status — 查看目前狀態
/usage — 查看 token 用量
/dashboard — 查看系統監控面板
/checkpoints — 查看檢查點狀態
/multiagent [enable|disable|status|stats] — 多代理協調管理
/agents — 查看專門化代理清單
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
		agent := t.getAgent(key)
		agent.SetProject(dir)
		t.send(key, fmt.Sprintf("✅ 專案已切換到: %s", dir))

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
		status := fmt.Sprintf(
			"📊 *狀態*\n"+
				"專案: `%s`\n"+
				"模型: `%s`\n"+
				"Session: %s\n"+
				"CLI 呼叫: %d 次\n"+
				"累計: %dK in / %dK out",
			agent.projectDir,
			t.client.Model,
			sessionInfo,
			stats.APICallCount,
			stats.TotalInputTokens/1000,
			stats.TotalOutputTokens/1000,
		)
		t.sendMarkdown(key, status)

	case "/usage":
		agent := t.getAgent(key)
		stats := agent.Stats()
		usage := fmt.Sprintf(
			"💰 *Token 用量*\n\n"+
				"*本次對話:*\n"+
				"  輸入: %d tokens\n"+
				"  輸出: %d tokens\n"+
				"  CLI 呼叫: %d 次\n"+
				"  CLI 費用: $%.4f\n\n"+
				"*模式: Claude Max 訂閱*\n"+
				"  月費固定 $200，無額外 token 費用",
			stats.TotalInputTokens,
			stats.TotalOutputTokens,
			stats.APICallCount,
			stats.TotalCostUSD,
		)
		t.sendMarkdown(key, usage)

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

	case "/agents":
		t.handleAgentsList(key)

	default:
		t.send(key, "未知指令，輸入 /help 查看可用指令")
	}
}

// --- Send helpers (直接用 Telegram HTTP API 以支援 message_thread_id) ---

func (t *TelegramBot) apiCall(method string, params url.Values) {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/%s", t.config.TelegramToken, method)
	resp, err := http.PostForm(apiURL, params)
	if err != nil {
		log.Printf("[telegram] %s error: %v", method, err)
		return
	}
	resp.Body.Close()
}

func (t *TelegramBot) send(key chatKey, text string) {
	params := url.Values{
		"chat_id": {strconv.FormatInt(key.chatID, 10)},
		"text":    {text},
	}
	if key.threadID != 0 {
		params.Set("message_thread_id", strconv.Itoa(key.threadID))
	}
	t.apiCall("sendMessage", params)
}

func (t *TelegramBot) sendSilent(key chatKey, text string) {
	params := url.Values{
		"chat_id":              {strconv.FormatInt(key.chatID, 10)},
		"text":                 {text},
		"disable_notification": {"true"},
	}
	if key.threadID != 0 {
		params.Set("message_thread_id", strconv.Itoa(key.threadID))
	}
	t.apiCall("sendMessage", params)
}

func (t *TelegramBot) sendMarkdown(key chatKey, text string) {
	params := url.Values{
		"chat_id":    {strconv.FormatInt(key.chatID, 10)},
		"text":       {text},
		"parse_mode": {"Markdown"},
	}
	if key.threadID != 0 {
		params.Set("message_thread_id", strconv.Itoa(key.threadID))
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.config.TelegramToken)
	resp, err := http.PostForm(apiURL, params)
	if err != nil || resp.StatusCode != 200 {
		if resp != nil {
			resp.Body.Close()
		}
		// fallback to plain text
		t.send(key, text)
		return
	}
	resp.Body.Close()
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
		"text":         text,
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
	switch data {
	case "refresh_dashboard":
		// Send updated dashboard
		t.handleDashboard(key)
		t.answerCallbackQuery(queryID, "✅ 狀態已刷新")
	case "show_checkpoints":
		// Show checkpoints for current project
		t.handleCheckpointsList(key)
		t.answerCallbackQuery(queryID, "📸 檢查點信息已更新")
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

// sendTelegram sends JSON data to Telegram API
func (t *TelegramBot) sendTelegram(method string, params map[string]interface{}) {
	jsonData, err := json.Marshal(params)
	if err != nil {
		log.Printf("Error marshaling Telegram API params: %v", err)
		return
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/%s", t.config.TelegramToken, method)

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Error calling Telegram API: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Telegram API error: %s", string(body))
	}
}
