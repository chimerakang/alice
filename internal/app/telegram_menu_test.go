package app

import (
	"strings"
	"testing"
	"time"
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
