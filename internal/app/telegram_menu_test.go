package app

import (
	"strings"
	"testing"
	"time"

	appengine "claude-tg-agent/internal/app/engine"
	"claude-tg-agent/internal/app/hermes"
)

// ── detectSmartIntent ────────────────────────────────────────────────────────

func TestDetectSmartIntentRetryPhrases(t *testing.T) {
	phrases := []string{
		"重跑失敗", "重試失敗", "重跑 review", "重試 review",
		"retry failed", "re-run failed", "重跑失敗的", "重試任務",
		"重新跑失敗", "重跑上次失敗", "retry review",
	}
	for _, p := range phrases {
		intent, ok := detectSmartIntent(p)
		if !ok || intent.kind != "retry" {
			t.Errorf("phrase %q: want retry intent, got ok=%v kind=%q", p, ok, intent.kind)
		}
	}
}

func TestDetectSmartIntentModelSwitchWithMode(t *testing.T) {
	cases := []struct {
		text string
		mode string
	}{
		{"切到 gpt deep", "gpt-deep"},
		{"切換到 gpt deep", "gpt-deep"},
		{"切 gpt smart", "gpt-smart"},
		{"switch to gpt deep", "gpt-deep"},
		{"切到 gpt fast", "gpt-fast"},
		{"切到 claude deep", "deep"},
		{"切 claude fast", "fast"},
		{"切換到 claude smart", "smart"},
		{"切到 claude", "smart"},
		{"switch to claude", "smart"},
		{"切換到 gpt", "gpt-smart"},
	}
	for _, tc := range cases {
		intent, ok := detectSmartIntent(tc.text)
		if !ok || intent.kind != "model" || intent.modelMode != tc.mode {
			t.Errorf("text %q: want model intent mode=%q, got ok=%v kind=%q mode=%q", tc.text, tc.mode, ok, intent.kind, intent.modelMode)
		}
	}
}

func TestDetectSmartIntentModelAmbiguous(t *testing.T) {
	phrases := []string{"換模型", "切換模型", "切換 model", "換 model", "change model", "switch model", "switch backend", "切換 backend", "換 backend"}
	for _, p := range phrases {
		intent, ok := detectSmartIntent(p)
		if !ok || intent.kind != "model" || intent.modelMode != "" {
			t.Errorf("phrase %q: want ambiguous model intent, got ok=%v kind=%q mode=%q", p, ok, intent.kind, intent.modelMode)
		}
	}
}

func TestDetectSmartIntentMenuPhrases(t *testing.T) {
	phrases := []string{"可以做什麼", "有什麼可以", "有什麼操作", "有什麼功能", "操作清單", "指令清單", "功能清單", "show menu", "what can i do", "what can you do", "help menu"}
	for _, p := range phrases {
		intent, ok := detectSmartIntent(p)
		if !ok || intent.kind != "menu" {
			t.Errorf("phrase %q: want menu intent, got ok=%v kind=%q", p, ok, intent.kind)
		}
	}
}

func TestDetectSmartIntentNoMatch(t *testing.T) {
	noMatch := []string{"", "   ", "請幫我分析這個 bug", "hello world", "今天天氣如何"}
	for _, p := range noMatch {
		_, ok := detectSmartIntent(p)
		if ok {
			t.Errorf("phrase %q: expected no match, got intent", p)
		}
	}
}

func TestDetectSmartIntentCaseInsensitive(t *testing.T) {
	intent, ok := detectSmartIntent("RETRY FAILED subtasks")
	if !ok || intent.kind != "retry" {
		t.Errorf("uppercase retry phrase: want retry, got ok=%v kind=%q", ok, intent.kind)
	}
}

// ── retryCandidateButtonText ─────────────────────────────────────────────────

func TestRetryCandidateButtonTextWithIssueNumber(t *testing.T) {
	c := retryTaskCandidate{
		ID:                "6b1960ba-1111-2222-3333-444444444444",
		GithubIssueNumber: 137,
		FailedCount:       3,
		Goal:              "修復登入流程",
	}
	text := retryCandidateButtonText(c)
	if !strings.Contains(text, "#137") {
		t.Errorf("button text should contain #137, got %q", text)
	}
	if !strings.Contains(text, "3 failed") {
		t.Errorf("button text should contain '3 failed', got %q", text)
	}
}

func TestRetryCandidateButtonTextWithoutIssueNumber(t *testing.T) {
	c := retryTaskCandidate{
		ID:          "6b1960ba-aaaa-bbbb-cccc-dddddddddddd",
		FailedCount: 1,
		Goal:        "補充測試",
	}
	text := retryCandidateButtonText(c)
	if strings.Contains(text, "#") {
		t.Errorf("button text should not contain # for zero issue number, got %q", text)
	}
	if !strings.Contains(text, "補充測試") {
		t.Errorf("button text should contain goal, got %q", text)
	}
}

func TestRetryCandidateButtonTextEmptyGoal(t *testing.T) {
	c := retryTaskCandidate{
		ID:          "6b1960ba-0000-0000-0000-000000000000",
		FailedCount: 2,
		Goal:        "",
	}
	text := retryCandidateButtonText(c)
	if !strings.Contains(text, "untitled task") {
		t.Errorf("empty goal should show 'untitled task', got %q", text)
	}
}

func TestRetryCandidateButtonTextLongGoalTruncated(t *testing.T) {
	c := retryTaskCandidate{
		ID:          "6b1960ba-0000-0000-0000-111111111111",
		FailedCount: 1,
		Goal:        strings.Repeat("很長的任務描述文字 ", 20),
	}
	text := retryCandidateButtonText(c)
	if len([]rune(text)) > 80 {
		t.Errorf("button text too long (>80 runes), got %d: %q", len([]rune(text)), text)
	}
}

// ── sendAbortConfirmation ────────────────────────────────────────────────────

func TestSendAbortConfirmationQueuesStopAgentButton(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 1)}

	bot.sendAbortConfirmation(key)

	select {
	case msg := <-bot.messageQueue:
		markup, ok := msg.Params["reply_markup"].(map[string]interface{})
		if !ok {
			t.Fatalf("reply_markup missing: %#v", msg.Params)
		}
		rows, ok := markup["inline_keyboard"].([][]map[string]interface{})
		if !ok || len(rows) == 0 {
			t.Fatalf("inline_keyboard missing or empty: %#v", markup)
		}
		confirmData, _ := rows[0][0]["callback_data"].(string)
		if confirmData != "stop_agent_42_7" {
			t.Errorf("confirm button callback_data = %q, want stop_agent_42_7", confirmData)
		}
		cancelData, _ := rows[0][1]["callback_data"].(string)
		if cancelData != "menu:cancel" {
			t.Errorf("cancel button callback_data = %q, want menu:cancel", cancelData)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for abort confirmation message")
	}
}

// ── sendRetryConfirmation ────────────────────────────────────────────────────

func TestSendRetryConfirmationLowestMode(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 1)}

	bot.sendRetryConfirmation(key, "lowest", "task-abc", 0)

	select {
	case msg := <-bot.messageQueue:
		markup := msg.Params["reply_markup"].(map[string]interface{})
		rows := markup["inline_keyboard"].([][]map[string]interface{})
		if rows[0][0]["callback_data"] != "retry:run:lowest:task-abc" {
			t.Errorf("run button = %v, want retry:run:lowest:task-abc", rows[0][0]["callback_data"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestSendRetryConfirmationIndexMode(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 1)}

	bot.sendRetryConfirmation(key, "index", "task-xyz", 3)

	select {
	case msg := <-bot.messageQueue:
		markup := msg.Params["reply_markup"].(map[string]interface{})
		rows := markup["inline_keyboard"].([][]map[string]interface{})
		if rows[0][0]["callback_data"] != "retry:run:index:task-xyz:3" {
			t.Errorf("run button = %v, want retry:run:index:task-xyz:3", rows[0][0]["callback_data"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestSendRetryConfirmationLatestMode(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 1)}

	bot.sendRetryConfirmation(key, "latest", "", 0)

	select {
	case msg := <-bot.messageQueue:
		markup := msg.Params["reply_markup"].(map[string]interface{})
		rows := markup["inline_keyboard"].([][]map[string]interface{})
		if rows[0][0]["callback_data"] != "retry:run:latest" {
			t.Errorf("run button = %v, want retry:run:latest", rows[0][0]["callback_data"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestSendRetryConfirmationAllMode(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 1)}

	bot.sendRetryConfirmation(key, "all", "task-abc", 0)

	select {
	case msg := <-bot.messageQueue:
		markup := msg.Params["reply_markup"].(map[string]interface{})
		rows := markup["inline_keyboard"].([][]map[string]interface{})
		if rows[0][0]["callback_data"] != "retry:run:all:task-abc" {
			t.Errorf("run button = %v, want retry:run:all:task-abc", rows[0][0]["callback_data"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestSendReviewNotificationIncludesAllFailedRetryAction(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 1)}

	bot.sendReviewNotification(key, appengine.ReviewNotification{
		TaskID:          "38bd507a",
		Verdict:         appengine.VerdictPartial,
		OverallScore:    70,
		AdvisoryRetry:   true,
		FailingSubTasks: 3,
		RetryNote:       "建議人工評估後重跑 3 個失敗/低分子任務",
	})

	select {
	case msg := <-bot.messageQueue:
		markup := msg.Params["reply_markup"].(map[string]interface{})
		rows := markup["inline_keyboard"].([][]map[string]interface{})
		if rows[0][0]["callback_data"] != "retry:confirm:all:38bd507a" {
			t.Errorf("all failed button = %v, want retry:confirm:all:38bd507a", rows[0][0]["callback_data"])
		}
		if rows[0][1]["callback_data"] != "retry:confirm:lowest:38bd507a" {
			t.Errorf("lowest button = %v, want retry:confirm:lowest:38bd507a", rows[0][1]["callback_data"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

// ── handleMenuCallback ───────────────────────────────────────────────────────

func TestHandleMenuCallbackCancelSendsAck(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 2)}

	bot.handleMenuCallback(key, "qid-1", "menu:cancel")

	select {
	case msg := <-bot.messageQueue:
		if msg.Method != "answerCallbackQuery" {
			t.Errorf("method = %q, want answerCallbackQuery", msg.Method)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ack")
	}
}

func TestHandleMenuCallbackOpenSendsMenuKeyboard(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config:       &Config{DefaultProjectDir: "/tmp"},
		agents:       map[chatKey]*Agent{},
		chatContexts: map[chatKey]*ChatContext{},
		hermesCoords: map[chatKey]*hermesCoord{},
		messageQueue: make(chan *TelegramMessage, 3),
	}

	bot.handleMenuCallback(key, "qid-2", "menu:open")

	var msgs []*TelegramMessage
	deadline := time.After(time.Second)
loop:
	for {
		select {
		case m := <-bot.messageQueue:
			msgs = append(msgs, m)
			if len(msgs) >= 2 {
				break loop
			}
		case <-deadline:
			break loop
		}
	}

	if len(msgs) < 2 {
		t.Fatalf("expected 2 messages (ack + menu), got %d", len(msgs))
	}
	methods := make(map[string]bool)
	for _, m := range msgs {
		methods[m.Method] = true
	}
	if !methods["answerCallbackQuery"] {
		t.Error("missing answerCallbackQuery")
	}
	if !methods["sendMessage"] {
		t.Error("missing sendMessage (menu)")
	}
}

func TestHandleMenuCallbackTasksSendsTasksSelector(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 3)}

	bot.handleMenuCallback(key, "qid-3", "menu:tasks")

	var menuMsg *TelegramMessage
	deadline := time.After(time.Second)
loop:
	for {
		select {
		case m := <-bot.messageQueue:
			if m.Method == "sendMessage" {
				menuMsg = m
				break loop
			}
		case <-deadline:
			break loop
		}
	}

	if menuMsg == nil {
		t.Fatal("timed out waiting for tasks selector message")
	}
	markup, ok := menuMsg.Params["reply_markup"].(map[string]interface{})
	if !ok {
		t.Fatalf("no reply_markup in tasks selector message")
	}
	rows := markup["inline_keyboard"].([][]map[string]interface{})
	if rows[0][0]["callback_data"] != "tasks:view:open" {
		t.Errorf("first tasks button = %v, want tasks:view:open", rows[0][0]["callback_data"])
	}
}

func TestHandleMenuCallbackAbortConfirmSendsStopButton(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 3)}

	bot.handleMenuCallback(key, "qid-4", "menu:abort_confirm")

	var menuMsg *TelegramMessage
	deadline := time.After(time.Second)
loop:
	for {
		select {
		case m := <-bot.messageQueue:
			if m.Method == "sendMessage" {
				menuMsg = m
				break loop
			}
		case <-deadline:
			break loop
		}
	}

	if menuMsg == nil {
		t.Fatal("timed out waiting for abort confirmation message")
	}
	markup := menuMsg.Params["reply_markup"].(map[string]interface{})
	rows := markup["inline_keyboard"].([][]map[string]interface{})
	confirmData, _ := rows[0][0]["callback_data"].(string)
	if !strings.HasPrefix(confirmData, "stop_agent_") {
		t.Errorf("confirm button = %q, want stop_agent_* prefix", confirmData)
	}
}

// ── handleRetryCallback ──────────────────────────────────────────────────────

func TestHandleRetryCallbackCancelAcksOnly(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 2)}

	bot.handleRetryCallback(key, "qid-5", "retry:cancel")

	select {
	case msg := <-bot.messageQueue:
		if msg.Method != "answerCallbackQuery" {
			t.Errorf("method = %q, want answerCallbackQuery", msg.Method)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ack")
	}
}

func TestHandleRetryCallbackConfirmLatestSendsRunButton(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 3)}

	bot.handleRetryCallback(key, "qid-6", "retry:confirm:latest")

	var menuMsg *TelegramMessage
	deadline := time.After(time.Second)
loop:
	for {
		select {
		case m := <-bot.messageQueue:
			if m.Method == "sendMessage" {
				menuMsg = m
				break loop
			}
		case <-deadline:
			break loop
		}
	}

	if menuMsg == nil {
		t.Fatal("timed out waiting for retry confirmation message")
	}
	markup := menuMsg.Params["reply_markup"].(map[string]interface{})
	rows := markup["inline_keyboard"].([][]map[string]interface{})
	if rows[0][0]["callback_data"] != "retry:run:latest" {
		t.Errorf("run button = %v, want retry:run:latest", rows[0][0]["callback_data"])
	}
}

func TestHandleRetryCallbackMenuRoutesToSendRetryMenu(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 3)}

	bot.handleRetryCallback(key, "qid-7", "retry:menu")

	var menuMsg *TelegramMessage
	deadline := time.After(time.Second)
loop:
	for {
		select {
		case m := <-bot.messageQueue:
			if m.Method == "sendMessage" {
				menuMsg = m
				break loop
			}
		case <-deadline:
			break loop
		}
	}

	if menuMsg == nil {
		t.Fatal("timed out waiting for retry menu message")
	}
	markup, ok := menuMsg.Params["reply_markup"].(map[string]interface{})
	if !ok {
		t.Fatal("no reply_markup in retry menu")
	}
	rows := markup["inline_keyboard"].([][]map[string]interface{})
	// First row always has "retry latest" and "back" buttons
	found := false
	for _, row := range rows {
		for _, btn := range row {
			if btn["callback_data"] == "retry:confirm:latest" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("retry:confirm:latest button not found in retry menu: %#v", rows)
	}
}

func TestHandleRetryCallbackUnknownAcksError(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 2)}

	bot.handleRetryCallback(key, "qid-8", "retry:bogus")

	select {
	case msg := <-bot.messageQueue:
		if msg.Method != "answerCallbackQuery" {
			t.Errorf("method = %q, want answerCallbackQuery", msg.Method)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

// ── handleModelCallback ──────────────────────────────────────────────────────

func TestHandleModelCallbackMenuRoutesToSendModelMenu(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config:       &Config{DefaultProjectDir: "/repo"},
		chatContexts: map[chatKey]*ChatContext{key: NewChatContext(42, 7, "/repo")},
		messageQueue: make(chan *TelegramMessage, 3),
	}

	bot.handleModelCallback(key, "qid-9", "model:menu")

	var menuMsg *TelegramMessage
	deadline := time.After(time.Second)
loop:
	for {
		select {
		case m := <-bot.messageQueue:
			if m.Method == "sendMessage" {
				menuMsg = m
				break loop
			}
		case <-deadline:
			break loop
		}
	}

	if menuMsg == nil {
		t.Fatal("timed out waiting for model menu message")
	}
	markup := menuMsg.Params["reply_markup"].(map[string]interface{})
	rows := markup["inline_keyboard"].([][]map[string]interface{})
	found := false
	for _, row := range rows {
		for _, btn := range row {
			if btn["callback_data"] == "model:set:fast" {
				found = true
			}
		}
	}
	if !found {
		t.Error("model:set:fast button not found in model menu")
	}
}

func TestHandleModelCallbackUnknownAcksError(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config:       &Config{},
		messageQueue: make(chan *TelegramMessage, 2),
	}

	bot.handleModelCallback(key, "qid-10", "model:bogus")

	select {
	case msg := <-bot.messageQueue:
		if msg.Method != "answerCallbackQuery" {
			t.Errorf("method = %q, want answerCallbackQuery", msg.Method)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestHandleModelCallbackInvalidSetModeAcksError(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config:       &Config{},
		messageQueue: make(chan *TelegramMessage, 2),
	}

	bot.handleModelCallback(key, "qid-11", "model:set:unknownmode")

	select {
	case msg := <-bot.messageQueue:
		if msg.Method != "answerCallbackQuery" {
			t.Errorf("method = %q, want answerCallbackQuery", msg.Method)
		}
		txt, _ := msg.Params["text"].(string)
		if !strings.Contains(txt, "無法辨識模型") {
			t.Errorf("ack text = %q, want '無法辨識模型'", txt)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

// ── handleSmartIntent ────────────────────────────────────────────────────────

func TestHandleSmartIntentMenuOpensMainMenu(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config:       &Config{DefaultProjectDir: "/tmp"},
		agents:       map[chatKey]*Agent{},
		chatContexts: map[chatKey]*ChatContext{},
		hermesCoords: map[chatKey]*hermesCoord{},
		messageQueue: make(chan *TelegramMessage, 3),
	}

	bot.handleSmartIntent(key, smartIntent{kind: "menu"})

	var menuMsg *TelegramMessage
	deadline := time.After(time.Second)
loop:
	for {
		select {
		case m := <-bot.messageQueue:
			if m.Method == "sendMessage" {
				menuMsg = m
				break loop
			}
		case <-deadline:
			break loop
		}
	}

	if menuMsg == nil {
		t.Fatal("timed out waiting for main menu message")
	}
	markup := menuMsg.Params["reply_markup"].(map[string]interface{})
	rows := markup["inline_keyboard"].([][]map[string]interface{})
	if len(rows) == 0 {
		t.Error("expected menu rows, got none")
	}
}

func TestHandleSmartIntentRetryOpensRetryMenu(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config:       &Config{DefaultProjectDir: "/tmp"},
		agents:       map[chatKey]*Agent{},
		chatContexts: map[chatKey]*ChatContext{},
		hermesCoords: map[chatKey]*hermesCoord{},
		messageQueue: make(chan *TelegramMessage, 4),
	}

	bot.handleSmartIntent(key, smartIntent{kind: "retry"})

	var menuMsg *TelegramMessage
	deadline := time.After(time.Second)
loop:
	for {
		select {
		case m := <-bot.messageQueue:
			if m.Method == "sendMessage" {
				markup, _ := m.Params["reply_markup"].(map[string]interface{})
				if markup != nil {
					menuMsg = m
					break loop
				}
			}
		case <-deadline:
			break loop
		}
	}

	if menuMsg == nil {
		t.Fatal("timed out waiting for retry menu message")
	}
	markup := menuMsg.Params["reply_markup"].(map[string]interface{})
	rows := markup["inline_keyboard"].([][]map[string]interface{})
	found := false
	for _, row := range rows {
		for _, btn := range row {
			if btn["callback_data"] == "retry:confirm:latest" {
				found = true
			}
		}
	}
	if !found {
		t.Error("retry:confirm:latest button not found in retry menu after smart intent")
	}
}

func TestHandleSmartIntentModelWithModeShowsConfirmDialog(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 3)}

	bot.handleSmartIntent(key, smartIntent{kind: "model", modelMode: "gpt-deep"})

	var menuMsg *TelegramMessage
	deadline := time.After(time.Second)
loop:
	for {
		select {
		case m := <-bot.messageQueue:
			if m.Method == "sendMessage" {
				menuMsg = m
				break loop
			}
		case <-deadline:
			break loop
		}
	}

	if menuMsg == nil {
		t.Fatal("timed out waiting for model confirm dialog")
	}
	markup := menuMsg.Params["reply_markup"].(map[string]interface{})
	rows := markup["inline_keyboard"].([][]map[string]interface{})
	// First row: confirm (model:set:gpt-deep) + cancel (menu:cancel)
	if len(rows) < 2 {
		t.Fatalf("expected at least 2 rows, got %d: %#v", len(rows), rows)
	}
	if rows[0][0]["callback_data"] != "model:set:gpt-deep" {
		t.Errorf("confirm button = %v, want model:set:gpt-deep", rows[0][0]["callback_data"])
	}
	if rows[0][1]["callback_data"] != "menu:cancel" {
		t.Errorf("cancel button = %v, want menu:cancel", rows[0][1]["callback_data"])
	}
	// Second row: all models button
	if rows[1][0]["callback_data"] != "model:menu" {
		t.Errorf("all models button = %v, want model:menu", rows[1][0]["callback_data"])
	}
}

func TestHandleSmartIntentModelAmbiguousOpensModelMenu(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config:       &Config{DefaultProjectDir: "/repo"},
		chatContexts: map[chatKey]*ChatContext{key: NewChatContext(42, 7, "/repo")},
		messageQueue: make(chan *TelegramMessage, 3),
	}

	bot.handleSmartIntent(key, smartIntent{kind: "model", modelMode: ""})

	var menuMsg *TelegramMessage
	deadline := time.After(time.Second)
loop:
	for {
		select {
		case m := <-bot.messageQueue:
			if m.Method == "sendMessage" {
				menuMsg = m
				break loop
			}
		case <-deadline:
			break loop
		}
	}

	if menuMsg == nil {
		t.Fatal("timed out waiting for model menu from ambiguous intent")
	}
	markup := menuMsg.Params["reply_markup"].(map[string]interface{})
	rows := markup["inline_keyboard"].([][]map[string]interface{})
	found := false
	for _, row := range rows {
		for _, btn := range row {
			if btn["callback_data"] == "model:set:fast" {
				found = true
			}
		}
	}
	if !found {
		t.Error("model:set:fast not found; expected full model menu for ambiguous intent")
	}
}

// ── hermesCandidateActionRows ─────────────────────────────────────────────────

func TestHermesCandidateActionRowsSingleTask(t *testing.T) {
	rows := hermesCandidateActionRows("zh-TW", nil, []hermes.TaskState{
		{ID: "task-abc-123456"},
	})

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows for single task, got %d", len(rows))
	}
	if rows[0][0]["callback_data"] != "hermes:continue:task-abc-123456" {
		t.Errorf("continue button = %v", rows[0][0]["callback_data"])
	}
	if rows[0][1]["callback_data"] != "hermes:replan:task-abc-123456" {
		t.Errorf("replan button = %v", rows[0][1]["callback_data"])
	}
	if rows[1][0]["callback_data"] != "hermes:cancel" {
		t.Errorf("cancel button = %v", rows[1][0]["callback_data"])
	}
}

func TestHermesCandidateActionRowsEmptyTasks(t *testing.T) {
	rows := hermesCandidateActionRows("zh-TW", nil, nil)
	if len(rows) == 0 {
		t.Fatal("expected at least cancel row for empty tasks")
	}
	last := rows[len(rows)-1]
	if last[0]["callback_data"] != "hermes:cancel" {
		t.Errorf("last row should be cancel, got %v", last[0]["callback_data"])
	}
}
