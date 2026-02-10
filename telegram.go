package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

// chatKey 用於識別獨立對話（支援 Forum Topics）
type chatKey struct {
	chatID   int64
	threadID int
}

type TelegramBot struct {
	agents   map[chatKey]*Agent // 每個 chat/topic 一個 agent
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
	if agent, ok := t.agents[key]; ok {
		return agent
	}
	agent := NewAgent(t.client, t.config.DefaultProjectDir)
	t.agents[key] = agent
	return agent
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
			if update.Message == nil || update.Message.Chat == nil || update.Message.From == nil {
				continue
			}

			msg := update.Message
			key := chatKey{chatID: msg.Chat.ID, threadID: msg.MessageThreadID}

			go t.handleMessage(key, msg.From.ID, msg.Text)
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

	// 處理指令
	if strings.HasPrefix(text, "/") {
		t.handleCommand(key, text)
		return
	}

	// 一般訊息 → agent 處理
	agent := t.getAgent(key)

	// 發送「處理中」提示
	t.sendTyping(key)

	// 呼叫 Claude Code CLI（串流模式，工具呼叫即時回報）
	response, err := agent.Run(text, func(update string, silent bool) {
		if silent {
			t.sendSilent(key, update)
		} else {
			t.send(key, update)
		}
	})

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
