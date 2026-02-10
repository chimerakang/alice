package main

import (
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type TelegramBot struct {
	bot      *tgbotapi.BotAPI
	agents   map[int64]*Agent // 每個 chat 一個 agent
	client   *CLIClient
	allowIDs map[int64]bool // 白名單
	config   *Config
}

func NewTelegramBot(config *Config, client *CLIClient) (*TelegramBot, error) {
	bot, err := tgbotapi.NewBotAPI(config.TelegramToken)
	if err != nil {
		return nil, fmt.Errorf("telegram bot init: %w", err)
	}

	allowIDs := make(map[int64]bool)
	for _, id := range config.AllowedUserIDs {
		allowIDs[id] = true
	}

	log.Printf("[telegram] bot authorized: @%s", bot.Self.UserName)

	return &TelegramBot{
		bot:      bot,
		agents:   make(map[int64]*Agent),
		client:   client,
		allowIDs: allowIDs,
		config:   config,
	}, nil
}

func (t *TelegramBot) getAgent(chatID int64) *Agent {
	if agent, ok := t.agents[chatID]; ok {
		return agent
	}
	agent := NewAgent(t.client, t.config.DefaultProjectDir)
	t.agents[chatID] = agent
	return agent
}

func (t *TelegramBot) isAllowed(userID int64) bool {
	if len(t.allowIDs) == 0 {
		return true // 沒設白名單就全部放行
	}
	return t.allowIDs[userID]
}

func (t *TelegramBot) Start() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := t.bot.GetUpdatesChan(u)

	log.Println("[telegram] listening for messages...")

	for update := range updates {
		if update.Message == nil {
			continue
		}
		go t.handleMessage(update.Message)
	}
}

func (t *TelegramBot) handleMessage(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	userID := msg.From.ID

	// 權限檢查
	if !t.isAllowed(userID) {
		t.send(chatID, "⛔ 你沒有使用權限。")
		return
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	// 處理指令
	if strings.HasPrefix(text, "/") {
		t.handleCommand(chatID, text)
		return
	}

	// 一般訊息 → agent 處理
	agent := t.getAgent(chatID)

	// 發送「處理中」提示
	typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	t.bot.Send(typing)

	// 呼叫 Claude Code CLI
	response, err := agent.Run(text, func(update string) {
		t.send(chatID, update)
	})

	if err != nil {
		t.send(chatID, fmt.Sprintf("❌ 錯誤: %v", err))
		return
	}

	if response == "" {
		response = "（完成，無文字回覆）"
	}

	// Telegram 訊息限制 4096 字元，分段發送
	t.sendLong(chatID, response)
}

func (t *TelegramBot) handleCommand(chatID int64, text string) {
	parts := strings.Fields(text)
	cmd := parts[0]

	switch cmd {
	case "/start", "/help":
		help := `🤖 *Claude Code Agent*

直接傳送訊息，我就會在你的專案中工作。
使用 Claude Code CLI（Max 訂閱，無額外費用）。

*指令：*
/project <路徑> — 切換專案目錄
/reset — 清除對話歷史
/status — 查看目前狀態
/usage — 查看 token 用量
/help — 顯示此說明`
		t.sendMarkdown(chatID, help)

	case "/project":
		if len(parts) < 2 {
			t.send(chatID, "用法: /project /path/to/your/project")
			return
		}
		dir := parts[1]
		agent := t.getAgent(chatID)
		agent.SetProject(dir)
		t.send(chatID, fmt.Sprintf("✅ 專案已切換到: %s", dir))

	case "/reset":
		agent := t.getAgent(chatID)
		stats := agent.Stats()
		if stats.APICallCount > 0 {
			t.send(chatID, fmt.Sprintf("🔄 對話已清除\n本次用量: %dK in / %dK out (%d 次呼叫)",
				stats.TotalInputTokens/1000, stats.TotalOutputTokens/1000, stats.APICallCount))
		} else {
			t.send(chatID, "🔄 對話歷史已清除")
		}
		agent.Reset()

	case "/status":
		agent := t.getAgent(chatID)
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
		t.sendMarkdown(chatID, status)

	case "/usage":
		agent := t.getAgent(chatID)
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
		t.sendMarkdown(chatID, usage)

	default:
		t.send(chatID, "未知指令，輸入 /help 查看可用指令")
	}
}

// --- Send helpers ---

func (t *TelegramBot) send(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := t.bot.Send(msg); err != nil {
		log.Printf("[telegram] send error: %v", err)
	}
}

func (t *TelegramBot) sendMarkdown(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	if _, err := t.bot.Send(msg); err != nil {
		// fallback to plain text if markdown fails
		t.send(chatID, text)
	}
}

func (t *TelegramBot) sendLong(chatID int64, text string) {
	const maxLen = 4000 // 留一點 buffer

	if len(text) <= maxLen {
		t.send(chatID, text)
		return
	}

	// 按段落分割
	chunks := splitMessage(text, maxLen)
	for i, chunk := range chunks {
		if len(chunks) > 1 {
			chunk = fmt.Sprintf("(%d/%d)\n%s", i+1, len(chunks), chunk)
		}
		t.send(chatID, chunk)
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
