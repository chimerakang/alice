package app

import (
	"strings"
	"testing"
	"time"

	appengine "github.com/chimerakang/alice-core/engine"
)

func TestHandleMessageBareContinuationUsesNormalAgent(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	const userID int64 = 123
	const projectDir = "/tmp/alice-project"

	bot := &TelegramBot{
		agents: map[chatKey]*Agent{
			key: NewAgent(&mockClient{}, projectDir, key.chatID, key.threadID),
		},
		allowIDs:     map[int64]bool{userID: true},
		config:       &Config{},
		messageQueue: make(chan *TelegramMessage, 20),
	}

	bot.handleMessage(key, userID, "繼續處理", "", nil, nil, nil, "", 1, 0)

	for {
		select {
		case msg := <-bot.messageQueue:
			text, _ := msg.Params["text"].(string)
			if strings.Contains(text, "Hermes 任務") || strings.Contains(text, "偵測到你想") {
				t.Fatalf("bare continuation should stay on normal agent path, got queued text %q", text)
			}
		default:
			return
		}
	}
}

func TestAbortActiveTaskMarksAgentInterrupting(t *testing.T) {
	key := chatKey{chatID: 77, threadID: 3}
	agent := NewAgent(&mockClient{}, "/repo", key.chatID, key.threadID)
	agent.transitionExecution(appengine.ExecutionStateStarting, "test_busy")
	bot := &TelegramBot{
		config:       &Config{DefaultProjectDir: "/repo"},
		agents:       map[chatKey]*Agent{key: agent},
		chatContexts: map[chatKey]*ChatContext{key: agent.current().ctx},
	}

	if got := bot.abortActiveTask(key, 0); got != abortTaskFinished {
		t.Fatalf("abortActiveTask result = %v, want %v", got, abortTaskFinished)
	}
	if got := agent.current().ctx.StateSnapshot().State; got != string(appengine.ChatStateInterrupting) {
		t.Fatalf("chat state = %q, want %q", got, appengine.ChatStateInterrupting)
	}
}

func TestSendMenuQueuesInlineKeyboard(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config:       &Config{DefaultProjectDir: "/tmp"},
		agents:       map[chatKey]*Agent{},
		chatContexts: map[chatKey]*ChatContext{},
		messageQueue: make(chan *TelegramMessage, 1),
	}

	bot.sendMenu(key)

	select {
	case msg := <-bot.messageQueue:
		if msg.Method != "sendMessage" {
			t.Fatalf("unexpected method: %s", msg.Method)
		}
		if msg.Params["chat_id"] != "42" || msg.Params["message_thread_id"] != "7" {
			t.Fatalf("unexpected chat/thread params: %#v", msg.Params)
		}
		markup, ok := msg.Params["reply_markup"].(map[string]interface{})
		if !ok {
			t.Fatalf("reply_markup missing or wrong type: %#v", msg.Params["reply_markup"])
		}
		rows, ok := markup["inline_keyboard"].([][]map[string]interface{})
		if !ok {
			t.Fatalf("inline_keyboard missing or wrong type: %#v", markup["inline_keyboard"])
		}
		if len(rows) != 4 {
			t.Fatalf("expected 4 menu rows, got %d", len(rows))
		}
		if rows[1][0]["callback_data"] != "menu:tasks" {
			t.Fatalf("unexpected tasks button: %#v", rows[1][0])
		}
		if rows[1][1]["callback_data"] != "model:menu" {
			t.Fatalf("unexpected model button: %#v", rows[1][1])
		}
		if rows[3][1]["callback_data"] != "menu:abort_confirm" {
			t.Fatalf("unexpected abort button: %#v", rows[3][1])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for menu message")
	}
}

func TestHandleMenuCallbackOpenSendsMenuKeyboard(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config:       &Config{DefaultProjectDir: "/tmp"},
		agents:       map[chatKey]*Agent{},
		chatContexts: map[chatKey]*ChatContext{},
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

func TestHandleSmartIntentMenuOpensMainMenu(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config:       &Config{DefaultProjectDir: "/tmp"},
		agents:       map[chatKey]*Agent{},
		chatContexts: map[chatKey]*ChatContext{},
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

// Commands and buttons of the removed task system still reach the bot from
// old habits and old messages; each must answer with where to go instead.
func TestRemovedCommandsPointAtTheReplacement(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	i18n, err := NewI18nManager("../../locales", "en")
	if err != nil {
		t.Fatal(err)
	}
	for command, msgKey := range removedCommandKeys {
		bot := &TelegramBot{config: &Config{DefaultProjectDir: "/tmp"}, messageQueue: make(chan *TelegramMessage, 4), i18n: i18n}
		bot.handleCommand(key, command+" x")
		want := bot.getLocalizedMessage(key.chatID, msgKey, nil)
		select {
		case msg := <-bot.messageQueue:
			if got, _ := msg.Params["text"].(string); got != want || got == msgKey {
				t.Errorf("%s answered %q, want %q", command, got, want)
			}
		case <-time.After(time.Second):
			t.Errorf("%s: no answer", command)
		}
	}
}

func TestRemovedButtonsAreAnswered(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	for _, data := range []string{"retry:menu", "issueops:retry-close:1", "hermes:continue:abc", "menu:hermes_status", "model:hermes-tier:codex"} {
		bot := &TelegramBot{config: &Config{DefaultProjectDir: "/tmp"}, messageQueue: make(chan *TelegramMessage, 4)}
		bot.handleCallbackQuery(key, 0, "qid", data)
		select {
		case msg := <-bot.messageQueue:
			if msg.Method != "answerCallbackQuery" {
				t.Errorf("%s: method = %s", data, msg.Method)
			}
		case <-time.After(time.Second):
			t.Errorf("%s: no answer", data)
		}
	}
}
