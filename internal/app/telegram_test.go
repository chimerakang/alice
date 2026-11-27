package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chimerakang/alice-core/ghissue"
	"github.com/chimerakang/alice-core/issueops"
)

func writeTestExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestNormalizeImageGenerationQuality(t *testing.T) {
	tests := []struct {
		name    string
		model   string
		quality string
		want    string
	}{
		{
			name:  "default gpt image quality uses auto",
			model: "gpt-image-2",
			want:  "auto",
		},
		{
			name:    "legacy standard maps to auto for gpt image models",
			model:   "gpt-image-2",
			quality: "standard",
			want:    "auto",
		},
		{
			name:    "legacy hd maps to high for gpt image models",
			model:   "gpt-image-2",
			quality: "hd",
			want:    "high",
		},
		{
			name:    "explicit supported quality is preserved",
			model:   "gpt-image-2",
			quality: "medium",
			want:    "medium",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeImageGenerationQuality(tt.model, tt.quality); got != tt.want {
				t.Fatalf("normalizeImageGenerationQuality(%q, %q) = %q, want %q", tt.model, tt.quality, got, tt.want)
			}
		})
	}
}

func TestNormalizeImageGenerationModel(t *testing.T) {
	if got := normalizeImageGenerationModel(""); got != "gpt-image-2" {
		t.Fatalf("default image model = %q, want gpt-image-2", got)
	}
	if got := normalizeImageGenerationModel("  gpt-image-2  "); got != "gpt-image-2" {
		t.Fatalf("trimmed image model = %q, want gpt-image-2", got)
	}
	if got := normalizeImageGenerationModel("dall-e-3"); got != "gpt-image-2" {
		t.Fatalf("unsupported image model = %q, want gpt-image-2", got)
	}
}

func TestSQLiteStorageListsGeneralMemoryCardsFromDecisionTasks(t *testing.T) {
	storage := newTestSQLiteStorage(t)
	now := time.Now().Add(-time.Hour)
	if err := storage.InsertDecisionLog(DecisionLog{
		Timestamp:     now,
		SessionID:     "direct-session",
		ProjectPath:   "/repo",
		ChatID:        42,
		ThreadID:      7,
		UserPrompt:    "請分析文件並整理 #143 memory 架構",
		AgentResponse: "已整理出 MemoryResolver 後續要讀 general task memory。",
		Outcome: ExecutionOutcome{
			Success:      true,
			TaskType:     "analysis",
			Summary:      "整理 memory 架構",
			FilesChanged: []string{"internal/app/memory_resolver.go", "docs/arch/memory.md"},
		},
		Model: "gpt-5.5",
	}); err != nil {
		t.Fatalf("InsertDecisionLog: %v", err)
	}
	if err := storage.InsertDecisionLog(DecisionLog{
		Timestamp:     now.Add(30 * time.Minute),
		SessionID:     "other-session",
		ProjectPath:   "/repo",
		ChatID:        42,
		ThreadID:      7,
		UserPrompt:    "處理 #99 unrelated",
		AgentResponse: "不應進入 #143 memory。",
		Outcome: ExecutionOutcome{
			Success:  true,
			TaskType: "analysis",
		},
		Model: "gpt-5.5",
	}); err != nil {
		t.Fatalf("InsertDecisionLog(other): %v", err)
	}

	cards, err := storage.ListGeneralMemoryCards(context.Background(), MemoryRequest{
		ChatID:      42,
		ThreadID:    7,
		ProjectDir:  "/repo",
		UserMessage: "繼續 #143",
	}, 3)
	if err != nil {
		t.Fatalf("ListGeneralMemoryCards: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("cards = %d, want 1: %#v", len(cards), cards)
	}
	if !strings.Contains(cards[0].Result, "general task memory") {
		t.Fatalf("card result = %q, want #143 memory", cards[0].Result)
	}
	if len(cards[0].Files) != 2 {
		t.Fatalf("card files = %#v, want touched files", cards[0].Files)
	}
	if !containsString(cards[0].Files, "internal/app/memory_resolver.go") {
		t.Fatalf("card files missing memory_resolver.go: %#v", cards[0].Files)
	}
	if len(cards[0].ContinuationHints) == 0 {
		t.Fatalf("expected continuation hints for card: %#v", cards[0])
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type fakeIssueOps struct {
	readiness issueops.CloseReadinessResult
}

func (f *fakeIssueOps) LoadIssue(ctx context.Context, projectDir string, issueNumber int) (*ghissue.IssueContext, error) {
	return nil, nil
}

func (f *fakeIssueOps) LoadIssueChecklistMapping(ctx context.Context, projectDir string, issueNumber int, subtasks []issueops.WorkItem) (issueops.ChecklistMappingResult, error) {
	return issueops.ChecklistMappingResult{}, nil
}

func (f *fakeIssueOps) CommentDone(ctx context.Context, projectDir string, issueNumber int, summary ghissue.CompletionSummary, notes string) error {
	return nil
}

func (f *fakeIssueOps) CommentBudgetExceeded(ctx context.Context, projectDir string, issueNumber int, used, max int) error {
	return nil
}

func (f *fakeIssueOps) ApplyLabel(ctx context.Context, projectDir string, issueNumber int, label string) error {
	return nil
}

func (f *fakeIssueOps) PlanIssue(ctx context.Context, req issueops.PlanIssueRequest) error {
	return nil
}

func (f *fakeIssueOps) RecordEvidence(ctx context.Context, req issueops.RecordEvidenceRequest) error {
	return nil
}

func (f *fakeIssueOps) SyncChecklist(ctx context.Context, req issueops.SyncChecklistRequest) (issueops.SyncChecklistResult, error) {
	return issueops.SyncChecklistResult{}, nil
}

func (f *fakeIssueOps) AssessCloseReadiness(ctx context.Context, req issueops.AssessCloseReadinessRequest) (issueops.CloseReadinessResult, error) {
	return f.readiness, nil
}

func (f *fakeIssueOps) CloseIssue(ctx context.Context, req issueops.CloseIssueRequest) (issueops.CloseReadinessResult, error) {
	return f.readiness, nil
}

func TestSendTasksSelectorQueuesOpenClosedRefresh(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 1)}

	bot.sendTasksSelector(key)

	select {
	case msg := <-bot.messageQueue:
		markup, ok := msg.Params["reply_markup"].(map[string]interface{})
		if !ok {
			t.Fatalf("reply_markup missing or wrong type: %#v", msg.Params["reply_markup"])
		}
		rows, ok := markup["inline_keyboard"].([][]map[string]interface{})
		if !ok {
			t.Fatalf("inline_keyboard missing or wrong type: %#v", markup["inline_keyboard"])
		}
		if rows[0][0]["callback_data"] != "tasks:view:open" || rows[0][1]["callback_data"] != "tasks:view:closed" {
			t.Fatalf("unexpected task view row: %#v", rows[0])
		}
		if rows[1][0]["callback_data"] != "tasks:refresh:open" || rows[1][1]["callback_data"] != "tasks:refresh:closed" {
			t.Fatalf("unexpected task refresh row: %#v", rows[1])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tasks selector message")
	}
}

func TestSendModelMenuQueuesSelector(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config:       &Config{DefaultProjectDir: "/repo"},
		chatContexts: map[chatKey]*ChatContext{key: NewChatContext(42, 7, "/repo")},
		messageQueue: make(chan *TelegramMessage, 1),
	}
	bot.chatContexts[key].Pref = ModelPreference("gpt-deep")

	bot.sendModelMenu(key)

	select {
	case msg := <-bot.messageQueue:
		markup, ok := msg.Params["reply_markup"].(map[string]interface{})
		if !ok {
			t.Fatalf("reply_markup missing or wrong type: %#v", msg.Params["reply_markup"])
		}
		rows, ok := markup["inline_keyboard"].([][]map[string]interface{})
		if !ok {
			t.Fatalf("inline_keyboard missing or wrong type: %#v", markup["inline_keyboard"])
		}
		if len(rows) != 5 {
			t.Fatalf("expected 5 model rows, got %d", len(rows))
		}
		if rows[0][0]["callback_data"] != "model:set:fast" || rows[3][0]["callback_data"] != "model:set:gpt-deep" {
			t.Fatalf("unexpected model rows: %#v", rows)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for model menu message")
	}
}

func TestChatContextSharesRecentMessagesAcrossAgentAndHermes(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		chatContexts: map[chatKey]*ChatContext{},
		config:       &Config{DefaultProjectDir: "/tmp/alice-project"},
	}
	chatCtx := bot.getChatContext(key, "/tmp/alice-project")
	agent := NewAgentWithContext(&mockClient{}, chatCtx)

	agent.AddRecentMessage("用 /gsmart 分析", "找到一個設定問題")
	recent := bot.getChatContext(key, "").RecentMessagesSnapshot()

	if len(recent) != 2 {
		t.Fatalf("expected shared recent messages, got %d", len(recent))
	}
	if recent[0].Content != "用 /gsmart 分析" || recent[1].Content != "找到一個設定問題" {
		t.Fatalf("unexpected recent messages: %#v", recent)
	}
}

func TestChatContextKeepsBackendSessionsIndependent(t *testing.T) {
	ctx := NewChatContext(42, 7, "/tmp/alice-project")
	ctx.SetSession(BackendClaude, "claude-session")
	ctx.SetSession(BackendCodex, "codex-thread")

	if got := ctx.Session(BackendClaude); got != "claude-session" {
		t.Fatalf("claude session: got %q", got)
	}
	if got := ctx.Session(BackendCodex); got != "codex-thread" {
		t.Fatalf("codex session: got %q", got)
	}

	ctx.ClearSession(BackendCodex)
	if got := ctx.Session(BackendClaude); got != "claude-session" {
		t.Fatalf("clearing codex should not clear claude session, got %q", got)
	}
	if got := ctx.Session(BackendCodex); got != "" {
		t.Fatalf("codex session should be cleared, got %q", got)
	}
}

func TestManualModelSwitchCommandsWarnContextReset(t *testing.T) {
	tests := []struct {
		command       string
		lastUsedModel string
		want          string
	}{
		{command: "/fast", lastUsedModel: "claude-sonnet-4-6", want: "快速模式"},
		{command: "/smart", lastUsedModel: "claude-haiku-4-5", want: "智能模式"},
		{command: "/deep", lastUsedModel: "claude-sonnet-4-6", want: "深度模式"},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			key := chatKey{chatID: 42, threadID: 7}
			agent := NewAgent(&mockClient{}, "/tmp/alice-project", key.chatID, key.threadID)
			agent.current().ctx.SetSession(BackendClaude, "claude-session")
			agent.lastUsedModel = tt.lastUsedModel

			bot := &TelegramBot{
				agents: map[chatKey]*Agent{
					key: agent,
				},
				config: &Config{
					DefaultProjectDir: "/tmp/alice-project",
					ModelRouting: ModelRoutingConfig{
						EnableDynamicRouting: true,
						FastModel:            "claude-haiku-4-5",
						SmartModel:           "claude-sonnet-4-6",
						DeepModel:            "claude-opus-4-5",
					},
				},
				i18n:            newTestI18nManager(t),
				messageQueue:    make(chan *TelegramMessage, 1),
				langPreferences: map[int64]string{},
			}
			bot.setChatlanguage(key.chatID, "zh-TW")

			bot.handleCommand(key, tt.command)

			select {
			case msg := <-bot.messageQueue:
				text, _ := msg.Params["text"].(string)
				if !strings.Contains(text, tt.want) {
					t.Fatalf("message %q does not contain mode text %q", text, tt.want)
				}
				if !strings.Contains(text, "切換模型將在下一則訊息時開始新的 backend session") {
					t.Fatalf("message %q does not contain context reset warning", text)
				}
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for model switch response")
			}

			if got := agent.current().ctx.Session(BackendClaude); got != "" {
				t.Fatalf("claude session after %s = %q, want empty", tt.command, got)
			}
		})
	}
}

func TestClearAndResetCommandsResetSession(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{command: "/clear", want: "Session 上下文已清除"},
		{command: "/reset", want: "對話已清除"},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			key := chatKey{chatID: 42, threadID: 7}
			agent := NewAgent(&mockClient{}, "/tmp/alice-project", key.chatID, key.threadID)
			agent.current().ctx.SetSession(BackendClaude, "claude-session")
			agent.current().ctx.SetSession(BackendCodex, "codex-thread")
			agent.current().ctx.RecentMsgs = []contextMessage{{Role: "user", Content: "前一則訊息"}}
			agent.lastUsedModel = "claude-sonnet-4-6"

			bot := &TelegramBot{
				agents: map[chatKey]*Agent{
					key: agent,
				},
				config:          &Config{DefaultProjectDir: "/tmp/alice-project"},
				i18n:            newTestI18nManager(t),
				messageQueue:    make(chan *TelegramMessage, 1),
				langPreferences: map[int64]string{},
			}
			bot.setChatlanguage(key.chatID, "zh-TW")

			bot.handleCommand(key, tt.command)

			assertQueuedMessageContains(t, bot.messageQueue, tt.want)
			if got := agent.current().ctx.Session(BackendClaude); got != "" {
				t.Fatalf("claude session after %s = %q, want empty", tt.command, got)
			}
			if got := agent.current().ctx.Session(BackendCodex); got != "" {
				t.Fatalf("codex session after %s = %q, want empty", tt.command, got)
			}
			if got := len(agent.current().ctx.RecentMsgs); got != 0 {
				t.Fatalf("recent messages after %s = %d, want 0", tt.command, got)
			}
		})
	}
}

func TestCloseCommandClosesFetchedIssue(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	agent := NewAgent(&mockClient{}, "/tmp/alice-project", key.chatID, key.threadID)
	bot := &TelegramBot{
		agents: map[chatKey]*Agent{
			key: agent,
		},
		config:          &Config{DefaultProjectDir: "/tmp/alice-project"},
		messageQueue:    make(chan *TelegramMessage, 2),
		langPreferences: map[int64]string{},
	}

	oldFetch := ghFetchIssue
	oldClose := ghCloseIssue
	defer func() {
		ghFetchIssue = oldFetch
		ghCloseIssue = oldClose
	}()
	var closedProject string
	var closedIssue int
	ghFetchIssue = func(ctx context.Context, projectDir string, issueNumber int) (*ghissue.IssueContext, error) {
		if projectDir != "/tmp/alice-project" {
			t.Fatalf("projectDir = %q, want /tmp/alice-project", projectDir)
		}
		if issueNumber != 57 {
			t.Fatalf("issueNumber = %d, want 57", issueNumber)
		}
		return &ghissue.IssueContext{Number: issueNumber, Title: "Chat history", State: "OPEN"}, nil
	}
	ghCloseIssue = func(ctx context.Context, projectDir string, issueNumber int) error {
		closedProject = projectDir
		closedIssue = issueNumber
		return nil
	}

	bot.handleCommand(key, "/close 57")

	if closedProject != "/tmp/alice-project" || closedIssue != 57 {
		t.Fatalf("closed issue = (%q, %d), want (/tmp/alice-project, 57)", closedProject, closedIssue)
	}
	assertQueuedMessageContains(t, bot.messageQueue, "已關閉 Issue #57")
}

func TestCloseCommandSkipsAlreadyClosedIssue(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	agent := NewAgent(&mockClient{}, "/tmp/alice-project", key.chatID, key.threadID)
	bot := &TelegramBot{
		agents:       map[chatKey]*Agent{key: agent},
		config:       &Config{DefaultProjectDir: "/tmp/alice-project"},
		messageQueue: make(chan *TelegramMessage, 2),
	}

	oldFetch := ghFetchIssue
	oldClose := ghCloseIssue
	defer func() {
		ghFetchIssue = oldFetch
		ghCloseIssue = oldClose
	}()
	ghFetchIssue = func(ctx context.Context, projectDir string, issueNumber int) (*ghissue.IssueContext, error) {
		return &ghissue.IssueContext{Number: issueNumber, Title: "Already done", State: "CLOSED"}, nil
	}
	ghCloseIssue = func(ctx context.Context, projectDir string, issueNumber int) error {
		t.Fatalf("CloseIssue should not be called for closed issue")
		return nil
	}

	bot.handleCommand(key, "/close https://github.com/chimerakang/dumbledore/issues/57")

	assertQueuedMessageContains(t, bot.messageQueue, "已經是 closed")
}

func TestParseCloseIssueNumber(t *testing.T) {
	cases := []struct {
		text string
		want int
		ok   bool
	}{
		{text: "/close #57", want: 57, ok: true},
		{text: "/close 57", want: 57, ok: true},
		{text: "/close https://github.com/chimerakang/dumbledore/issues/57", want: 57, ok: true},
		{text: "/close nope", ok: false},
	}
	for _, tc := range cases {
		got, ok := parseCloseIssueNumber(strings.Fields(tc.text), tc.text)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("parseCloseIssueNumber(%q) = (%d, %v), want (%d, %v)", tc.text, got, ok, tc.want, tc.ok)
		}
	}
}

func TestChatContextRestoresModelPreferenceFromStorage(t *testing.T) {
	storage := newTestSQLiteStorage(t)
	if err := storage.SaveTopicModelPreference(42, 7, "/tmp/alice-project", "gpt-5.5"); err != nil {
		t.Fatalf("SaveTopicModelPreference: %v", err)
	}

	oldStorage := globalStorage
	globalStorage = storage
	t.Cleanup(func() { globalStorage = oldStorage })

	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		chatContexts: map[chatKey]*ChatContext{},
		config:       &Config{DefaultProjectDir: "/tmp/alice-project"},
	}

	ctx := bot.getChatContext(key, "/tmp/alice-project")
	if got := string(ctx.Pref); got != "gpt-5.5" {
		t.Fatalf("restored model preference = %q, want gpt-5.5", got)
	}
}

type previewCaptureTransport struct {
	path        string
	contentType string
	body        []byte
}

func (t *previewCaptureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.path = req.URL.Path
	t.contentType = req.Header.Get("Content-Type")
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	t.body = body
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":{}}`)),
		Request:    req,
	}, nil
}

type previewFailureTransport struct {
	path        string
	contentType string
	statusCode  int
	body        string
}

func (t *previewFailureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.path = req.URL.Path
	t.contentType = req.Header.Get("Content-Type")
	return &http.Response{
		StatusCode: t.statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(t.body)),
		Request:    req,
	}, nil
}

func TestPreviewCommandMissingURLQueuesUsage(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		messageQueue: make(chan *TelegramMessage, 1),
	}

	bot.handleCommand(key, "/preview")

	assertQueuedMessageContains(t, bot.messageQueue, "使用方式：/preview <URL>")
}

func TestPreviewCommandInvalidURLQueuesError(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		messageQueue: make(chan *TelegramMessage, 1),
	}

	bot.handleCommand(key, "/preview ftp://example.com")

	assertQueuedMessageContains(t, bot.messageQueue, "無效 URL")
}

func TestPreviewCommandFallsBackToTextWhenScreenshotFails(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	screenshotManager := NewScreenshotManager()
	screenshotManager.tempDir = t.TempDir()
	screenshotManager.commandRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("playwright unavailable")
	}

	bot := &TelegramBot{
		config: &Config{
			TelegramToken: "bot-token",
			Multimedia:    MultimediaConfig{MaxFileSizeMB: 5},
		},
		messageQueue:      make(chan *TelegramMessage, 2),
		screenshotManager: screenshotManager,
	}

	bot.handleCommand(key, "/preview https://example.com")

	assertQueuedMessageContains(t, bot.messageQueue, "正在預覽")
	assertQueuedMessageContains(t, bot.messageQueue, "截圖失敗")
}

func TestPreviewCommandSendsPhoto(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	screenshotManager := NewScreenshotManager()
	screenshotManager.tempDir = t.TempDir()
	screenshotManager.commandRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		outputPath := args[len(args)-1]
		if err := os.WriteFile(outputPath, testPNG, 0o644); err != nil {
			return nil, err
		}
		return []byte("navigating"), nil
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head>
  <title>Preview Title</title>
  <meta name="description" content="Preview description">
</head>
<body>ok</body>
</html>`)
	}))
	defer srv.Close()

	transport := &previewCaptureTransport{}
	bot := &TelegramBot{
		config: &Config{
			TelegramToken: "bot-token",
			Multimedia:    MultimediaConfig{MaxFileSizeMB: 5},
		},
		messageQueue:      make(chan *TelegramMessage, 1),
		screenshotManager: screenshotManager,
		mediaHTTPClient:   &http.Client{Transport: transport},
	}

	bot.handleCommand(key, "/preview "+srv.URL)

	assertQueuedMessageContains(t, bot.messageQueue, "正在預覽")
	if transport.path != "/botbot-token/sendPhoto" {
		t.Fatalf("sendPhoto path = %q, want /botbot-token/sendPhoto", transport.path)
	}
	if transport.contentType == "" || !strings.HasPrefix(transport.contentType, "multipart/form-data; boundary=") {
		t.Fatalf("sendPhoto content-type = %q, want multipart form", transport.contentType)
	}
	body := string(transport.body)
	for _, want := range []string{"name=\"photo\"", "Preview Title", "Preview description"} {
		if !strings.Contains(body, want) {
			t.Fatalf("sendPhoto body missing %q:\n%s", want, body)
		}
	}
}

func TestPreviewCommandFallsBackToTextWhenSendPhotoFails(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	screenshotManager := NewScreenshotManager()
	screenshotManager.tempDir = t.TempDir()
	screenshotManager.commandRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		outputPath := args[len(args)-1]
		if err := os.WriteFile(outputPath, testPNG, 0o644); err != nil {
			return nil, err
		}
		return []byte("navigating"), nil
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head>
  <title>Preview Title</title>
  <meta name="description" content="Preview description">
</head>
<body>ok</body>
</html>`)
	}))
	defer srv.Close()

	transport := &previewFailureTransport{
		statusCode: http.StatusInternalServerError,
		body:       `{"ok":false,"description":"internal error"}`,
	}
	bot := &TelegramBot{
		config: &Config{
			TelegramToken: "bot-token",
			Multimedia:    MultimediaConfig{MaxFileSizeMB: 5},
		},
		messageQueue:      make(chan *TelegramMessage, 2),
		screenshotManager: screenshotManager,
		mediaHTTPClient:   &http.Client{Transport: transport},
	}

	bot.handleCommand(key, "/preview "+srv.URL)

	assertQueuedMessageContains(t, bot.messageQueue, "正在預覽")
	assertQueuedMessageContains(t, bot.messageQueue, "Preview Title")
	if transport.path != "/botbot-token/sendPhoto" {
		t.Fatalf("sendPhoto path = %q, want /botbot-token/sendPhoto", transport.path)
	}
}

func assertQueuedMessageContains(t *testing.T, queue <-chan *TelegramMessage, want string) {
	t.Helper()

	select {
	case msg := <-queue:
		text, _ := msg.Params["text"].(string)
		if !strings.Contains(text, want) {
			t.Fatalf("queued message %q does not contain %q", text, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for queued message containing %q", want)
	}
}

func TestApplyExplicitUserModelPreferenceUsesGPTDeepForVoice(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config: &Config{
			DefaultProjectDir: "/repo",
			ModelRouting: ModelRoutingConfig{
				EnableDynamicRouting: true,
				CodexDeepModel:       "gpt-5.5",
			},
		},
		chatContexts: map[chatKey]*ChatContext{
			key: NewChatContext(key.chatID, key.threadID, "/repo"),
		},
	}
	bot.chatContexts[key].Pref = ModelPreference("gpt-deep")
	agent := NewAgent(nil, "/repo", key.chatID, key.threadID)
	agent.SetModelOverride("claude-haiku-4-5-20251001")

	if !bot.applyExplicitUserModelPreference(key, agent, "voice") {
		t.Fatalf("applyExplicitUserModelPreference returned false")
	}
	if agent.currentModelOverride != "gpt-5.5" {
		t.Fatalf("currentModelOverride = %q, want gpt-5.5", agent.currentModelOverride)
	}
	if agent.enablePlanMode {
		t.Fatalf("enablePlanMode = true, want false")
	}
}

func TestRunAgentForStopButtonUsesGPTDeepPreferenceForDocumentAnalysis(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	client := &modelRecordingClient{}
	bot := &TelegramBot{
		config: &Config{
			DefaultProjectDir: "/repo",
			ModelRouting: ModelRoutingConfig{
				EnableDynamicRouting: true,
				CodexDeepModel:       "gpt-5.5",
			},
		},
		chatContexts: map[chatKey]*ChatContext{
			key: NewChatContext(key.chatID, key.threadID, "/repo"),
		},
	}
	bot.chatContexts[key].Pref = ModelPreference("gpt-deep")
	agent := NewAgent(client, "/repo", key.chatID, key.threadID)
	agent.SetModelOverride("claude-haiku-4-5-20251001")

	got, err := bot.runAgentForStopButton(key, agent, "分析文件 temp/feedback.docx", nil)
	if err != nil {
		t.Fatalf("runAgentForStopButton: %v", err)
	}
	if got != "ok" {
		t.Fatalf("response = %q, want ok", got)
	}
	if len(client.calls) != 1 || client.calls[0] != "gpt-5.5" {
		t.Fatalf("model calls = %#v, want [gpt-5.5]", client.calls)
	}
	if agent.currentModelOverride != "gpt-5.5" {
		t.Fatalf("currentModelOverride = %q, want gpt-5.5", agent.currentModelOverride)
	}
}

func TestApplyExplicitUserModelPreferenceUsesPlanMode(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config: &Config{
			DefaultProjectDir: "/repo",
			ModelRouting: ModelRoutingConfig{
				EnableDynamicRouting: true,
				PlanModel:            "claude-opus-4-5-20251101",
				ExecuteModel:         "gpt-5.5",
			},
		},
		chatContexts: map[chatKey]*ChatContext{
			key: NewChatContext(key.chatID, key.threadID, "/repo"),
		},
	}
	bot.chatContexts[key].Pref = ModelPreference("plan")
	agent := NewAgent(nil, "/repo", key.chatID, key.threadID)

	if !bot.applyExplicitUserModelPreference(key, agent, "voice") {
		t.Fatalf("applyExplicitUserModelPreference returned false")
	}
	if !agent.enablePlanMode {
		t.Fatalf("enablePlanMode = false, want true")
	}
	if agent.planModel != "claude-opus-4-5-20251101" || agent.executeModel != "gpt-5.5" {
		t.Fatalf("plan/execute = %q/%q, want claude-opus-4-5-20251101/gpt-5.5", agent.planModel, agent.executeModel)
	}
}

func TestHandleTasksRendersGitHubIssuesWithKeyboard(t *testing.T) {
	key := chatKey{chatID: 88, threadID: 3}
	oldList := tasksGitHubIssueListFunc
	oldRepo := tasksGitHubRepoURLFunc
	tasksGitHubIssueListFunc = func(ctx context.Context, projectDir, state string, limit int) ([]tasksGitHubIssue, error) {
		if projectDir != "/tmp/alice-project" {
			t.Fatalf("unexpected projectDir: %s", projectDir)
		}
		if state != "open" {
			t.Fatalf("unexpected state: %s", state)
		}
		if limit != 20 {
			t.Fatalf("unexpected limit: %d", limit)
		}
		return []tasksGitHubIssue{
			{
				Number:    12,
				Title:     "Fix parser regression",
				Labels:    []string{"bug", "p1"},
				Milestone: "Sprint 1",
			},
			{
				Number: 13,
				Title:  "Add topic filter",
				Labels: []string{"enhancement"},
			},
		}, nil
	}
	tasksGitHubRepoURLFunc = func(projectDir string) (string, error) {
		return "https://github.com/acme/alice", nil
	}
	defer func() {
		tasksGitHubIssueListFunc = oldList
		tasksGitHubRepoURLFunc = oldRepo
	}()

	bot := &TelegramBot{
		agents: map[chatKey]*Agent{
			key: NewAgent(&mockClient{}, "/tmp/alice-project", key.chatID, key.threadID),
		},
		config:          &Config{DefaultProjectDir: "/tmp/alice-project"},
		i18n:            newTestI18nManager(t),
		messageQueue:    make(chan *TelegramMessage, 4),
		langPreferences: map[int64]string{},
	}
	bot.setChatlanguage(key.chatID, "zh-TW")

	bot.handleTasks(key)

	select {
	case msg := <-bot.messageQueue:
		text, _ := msg.Params["text"].(string)
		if !strings.Contains(text, "Alice 待辦工作清單") {
			t.Fatalf("unexpected title:\n%s", text)
		}
		if !strings.Contains(text, "顯示開放 Issues") {
			t.Fatalf("unexpected state line:\n%s", text)
		}
		if !strings.Contains(text, "Milestone: Sprint 1") || !strings.Contains(text, "#12 Fix parser regression") {
			t.Fatalf("missing milestone group:\n%s", text)
		}
		if !strings.Contains(text, "標籤: bug, p1") || !strings.Contains(text, "未指定 Milestone") {
			t.Fatalf("missing issue details:\n%s", text)
		}

		markup, ok := msg.Params["reply_markup"].(map[string]interface{})
		if !ok {
			t.Fatalf("reply_markup missing or wrong type: %#v", msg.Params["reply_markup"])
		}
		rows, ok := markup["inline_keyboard"].([][]map[string]interface{})
		if !ok {
			t.Fatalf("inline_keyboard missing or wrong type: %#v", markup["inline_keyboard"])
		}
		if len(rows) != 2 {
			t.Fatalf("expected 2 keyboard rows, got %d", len(rows))
		}
		if rows[0][0]["text"] != "🔄 重新整理" || rows[0][1]["text"] != "📋 已關閉" {
			t.Fatalf("unexpected first row buttons: %#v", rows[0])
		}
		if rows[1][0]["url"] != "https://github.com/acme/alice/issues" {
			t.Fatalf("unexpected GitHub URL button: %#v", rows[1][0])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for /tasks response")
	}
}

func TestHandleTasksRendersEmptyIssueListMessage(t *testing.T) {
	key := chatKey{chatID: 89, threadID: 3}
	oldList := tasksGitHubIssueListFunc
	oldRepo := tasksGitHubRepoURLFunc
	tasksGitHubIssueListFunc = func(ctx context.Context, projectDir, state string, limit int) ([]tasksGitHubIssue, error) {
		if state != "open" {
			t.Fatalf("unexpected state: %s", state)
		}
		return []tasksGitHubIssue{}, nil
	}
	tasksGitHubRepoURLFunc = func(projectDir string) (string, error) {
		return "https://github.com/acme/alice", nil
	}
	defer func() {
		tasksGitHubIssueListFunc = oldList
		tasksGitHubRepoURLFunc = oldRepo
	}()

	bot := &TelegramBot{
		agents: map[chatKey]*Agent{
			key: NewAgent(&mockClient{}, "/tmp/alice-project", key.chatID, key.threadID),
		},
		config:          &Config{DefaultProjectDir: "/tmp/alice-project"},
		i18n:            newTestI18nManager(t),
		messageQueue:    make(chan *TelegramMessage, 4),
		langPreferences: map[int64]string{},
	}
	bot.setChatlanguage(key.chatID, "zh-TW")

	bot.handleTasks(key)

	select {
	case msg := <-bot.messageQueue:
		text, _ := msg.Params["text"].(string)
		if !strings.Contains(text, "目前沒有符合條件的 GitHub Issues") {
			t.Fatalf("expected empty-issue message, got:\n%s", text)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for empty issue response")
	}
}

func TestHandleTasksGeneralTopicShowsNoRepoMessage(t *testing.T) {
	key := chatKey{chatID: 90, threadID: 0}
	bot := &TelegramBot{
		allowIDs:        map[int64]bool{123: true},
		config:          &Config{DefaultProjectDir: "/tmp/alice-project"},
		messageQueue:    make(chan *TelegramMessage, 2),
		langPreferences: map[int64]string{},
	}
	bot.setChatlanguage(key.chatID, "zh-TW")

	bot.handleMessage(key, 123, "/tasks", "", nil, nil, nil, "", 1, 0)

	select {
	case msg := <-bot.messageQueue:
		text, _ := msg.Params["text"].(string)
		if !strings.Contains(text, "具體 project topic") {
			t.Fatalf("expected general-topic guidance, got:\n%s", text)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for general topic response")
	}
}

func TestHandleTasksUnboundTopicShowsNoRepoMessage(t *testing.T) {
	key := chatKey{chatID: 91, threadID: 4}
	s := newTestSQLiteStorage(t)
	oldStorage := globalStorage
	globalStorage = s
	defer func() { globalStorage = oldStorage }()

	bot := &TelegramBot{
		agents:          map[chatKey]*Agent{key: NewAgent(&mockClient{}, "/tmp/alice-project", key.chatID, key.threadID)},
		config:          &Config{DefaultProjectDir: "/tmp/alice-project"},
		messageQueue:    make(chan *TelegramMessage, 2),
		langPreferences: map[int64]string{},
	}
	bot.setChatlanguage(key.chatID, "zh-TW")

	bot.handleTasks(key)

	select {
	case msg := <-bot.messageQueue:
		text, _ := msg.Params["text"].(string)
		if !strings.Contains(text, "具體 project topic") {
			t.Fatalf("expected unbound-topic guidance, got:\n%s", text)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for unbound topic response")
	}
}

func TestHandleTasksFallsBackToLegacyPhaseOverview(t *testing.T) {
	key := chatKey{chatID: 91, threadID: 4}
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, "docs"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	content := "preamble\n\n## Phase Overview\nlegacy content\n\n## Next\nmore"
	if err := os.WriteFile(filepath.Join(projectDir, "docs", "MASTER_TASKS.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	oldRepo := tasksGitHubRepoURLFunc
	tasksGitHubRepoURLFunc = func(projectDir string) (string, error) {
		return "", errTasksNoGitHubRepo
	}
	defer func() { tasksGitHubRepoURLFunc = oldRepo }()

	bot := &TelegramBot{
		agents: map[chatKey]*Agent{
			key: NewAgent(&mockClient{}, projectDir, key.chatID, key.threadID),
		},
		config:       &Config{DefaultProjectDir: projectDir},
		messageQueue: make(chan *TelegramMessage, 2),
	}

	bot.handleTasks(key)

	select {
	case msg := <-bot.messageQueue:
		text, _ := msg.Params["text"].(string)
		if !strings.Contains(text, "Phase Overview") || !strings.Contains(text, "legacy content") {
			t.Fatalf("expected legacy fallback text, got:\n%s", text)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for legacy fallback")
	}
}

func TestHandleTasksAuthFailurePromptsLogin(t *testing.T) {
	key := chatKey{chatID: 92, threadID: 5}
	oldList := tasksGitHubIssueListFunc
	oldRepo := tasksGitHubRepoURLFunc
	tasksGitHubRepoURLFunc = func(projectDir string) (string, error) {
		return "https://github.com/acme/alice", nil
	}
	tasksGitHubIssueListFunc = func(ctx context.Context, projectDir, state string, limit int) ([]tasksGitHubIssue, error) {
		return nil, errTasksGitHubAuthRequired
	}
	defer func() {
		tasksGitHubIssueListFunc = oldList
		tasksGitHubRepoURLFunc = oldRepo
	}()

	bot := &TelegramBot{
		agents: map[chatKey]*Agent{
			key: NewAgent(&mockClient{}, "/tmp/alice-project", key.chatID, key.threadID),
		},
		config:       &Config{DefaultProjectDir: "/tmp/alice-project"},
		messageQueue: make(chan *TelegramMessage, 2),
	}

	bot.handleTasks(key)

	select {
	case msg := <-bot.messageQueue:
		text, _ := msg.Params["text"].(string)
		if !strings.Contains(text, "gh auth login") {
			t.Fatalf("expected auth prompt, got:\n%s", text)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for auth prompt")
	}
}

type fakeInterruptibleCoord struct {
	running       bool
	interrupted   bool
	interruptFrom int64
}

func (f *fakeInterruptibleCoord) TaskID() string { return "task-1" }

func (f *fakeInterruptibleCoord) IsRunning() bool { return f.running }

func (f *fakeInterruptibleCoord) InterruptWith(messageID int64) {
	f.interrupted = true
	f.interruptFrom = messageID
}

func TestMarkdownToTelegramHTML(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		contains    []string
		notContains []string
	}{
		{
			name:     "heading becomes bold",
			input:    "# Title",
			contains: []string{"<b>Title</b>"},
		},
		{
			name:     "bold text",
			input:    "**bold**",
			contains: []string{"<b>bold</b>"},
		},
		{
			name:     "italic asterisk",
			input:    "*italic*",
			contains: []string{"<i>italic</i>"},
		},
		{
			name:     "italic underscore",
			input:    "_italic_",
			contains: []string{"<i>italic</i>"},
		},
		{
			name:     "inline code",
			input:    "`code`",
			contains: []string{"<code>code</code>"},
		},
		{
			name:     "link",
			input:    "[text](https://example.com)",
			contains: []string{`<a href="https://example.com">text</a>`},
		},
		{
			name:     "strikethrough",
			input:    "~~strike~~",
			contains: []string{"<s>strike</s>"},
		},
		{
			name:     "unordered list dash",
			input:    "- item one\n- item two",
			contains: []string{"• item one", "• item two"},
		},
		{
			name:     "ordered list",
			input:    "1. first\n2. second",
			contains: []string{"1. first", "2. second"},
		},
		{
			name:     "blockquote single line",
			input:    "> quoted text",
			contains: []string{"<blockquote>quoted text</blockquote>"},
		},
		{
			name:     "blockquote multiple lines grouped",
			input:    "> line one\n> line two",
			contains: []string{"<blockquote>line one\nline two</blockquote>"},
		},
		{
			name:     "code fence",
			input:    "```\nfmt.Println(\"hello\")\n```",
			contains: []string{"<pre><code>", "fmt.Println(", "</code></pre>"},
		},
		{
			name:        "horizontal rule is removed",
			input:       "before\n---\nafter",
			contains:    []string{"before", "after"},
			notContains: []string{"---"},
		},
		{
			name: "complex composite: heading list blockquote code inline link",
			input: "# Title\n\n**bold** and *italic* with [link](https://x.com)\n\n" +
				"- list item\n\n> quote\n\n```\ncode block\n```",
			contains: []string{
				"<b>Title</b>",
				"<b>bold</b>",
				"<i>italic</i>",
				`<a href="https://x.com">link</a>`,
				"• list item",
				"<blockquote>quote</blockquote>",
				"<pre><code>",
				"code block",
			},
		},
		{
			name:        "script tag is HTML-escaped",
			input:       "<script>alert('xss')</script> normal text",
			contains:    []string{"&lt;script&gt;", "&lt;/script&gt;", "normal text"},
			notContains: []string{"<script>", "</script>"},
		},
		{
			name:        "img tag is HTML-escaped",
			input:       `<img src="x" onerror="alert(1)">`,
			contains:    []string{"&lt;img"},
			notContains: []string{"<img"},
		},
		{
			name:        "div tag is HTML-escaped",
			input:       "<div>content</div>",
			notContains: []string{"<div>", "</div>"},
		},
		{
			name:        "h1 tag is HTML-escaped",
			input:       "<h1>heading</h1>",
			notContains: []string{"<h1>", "</h1>"},
		},
		{
			name:     "ampersand is escaped",
			input:    "A & B",
			contains: []string{"A &amp; B"},
		},
		{
			name:     "inline code protects content from bold regex",
			input:    "`**not bold**`",
			contains: []string{"<code>**not bold**</code>"},
		},
		{
			name:        "code fence protects content from inline regex",
			input:       "```\n**not bold**\n_not italic_\n```",
			contains:    []string{"**not bold**", "_not italic_"},
			notContains: []string{"<b>not bold</b>", "<i>not italic</i>"},
		},
		{
			name:     "double quote in text is escaped",
			input:    `say "hello"`,
			contains: []string{"say &quot;hello&quot;"},
		},
		{
			name:     "link with URL safe chars produces valid href attribute",
			input:    "[site](https://example.com/path?a=1&b=2)",
			contains: []string{`href="https://example.com/path?a=1&amp;b=2"`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := markdownToTelegramHTML(tc.input)
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Errorf("markdownToTelegramHTML(%q):\ngot:     %q\nmissing: %q", tc.input, got, want)
				}
			}
			for _, ban := range tc.notContains {
				if strings.Contains(got, ban) {
					t.Errorf("markdownToTelegramHTML(%q):\ngot:              %q\nforbidden string: %q", tc.input, got, ban)
				}
			}
		})
	}
}

func TestSplitMessageProducesBalancedHTMLTags(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		sb.WriteString(fmt.Sprintf("## Section %d\n\nParagraph %d with **bold** and *italic* text.\n\n", i+1, i+1))
	}
	long := sb.String()

	chunks := splitMessage(long, 500)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for long input, got %d", len(chunks))
	}

	for i, chunk := range chunks {
		html := markdownToTelegramHTML(chunk)
		for _, tag := range []string{"b", "i", "code", "pre"} {
			opens := strings.Count(html, "<"+tag+">") + strings.Count(html, "<"+tag+" ")
			closes := strings.Count(html, "</"+tag+">")
			if opens != closes {
				t.Errorf("chunk %d: unbalanced <%s> (opens=%d closes=%d)\nhtml: %q", i, tag, opens, closes, html)
			}
		}
		for _, bad := range []string{"<script", "<img", "<div", "<span", "<h1", "<h2"} {
			if strings.Contains(html, bad) {
				t.Errorf("chunk %d: forbidden tag %q leaked into rendered HTML", i, bad)
			}
		}
	}
}

func TestClassifyError_DelegatesTransientCategoriesToErrorclass(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"timeout via context deadline", "context deadline exceeded after 30s", "timeout"},
		{"timeout via i/o", "read tcp i/o timeout", "timeout"},
		{"rate limit explicit", "Got 429 too many requests", "rate_limit"},
		{"overloaded explicit", "529 overloaded — please retry", "overloaded"},
		{"overloaded via 503", "503 service unavailable", "overloaded"},
		{"cancelled by user", "agent aborted by user", "cancelled"},
		{"cancelled by ctx", "operation failed: context canceled", "cancelled"},
		{"file_patch", "file_patch unique match required", "tool_file_patch"},
		{"permission denied", "open /etc/passwd: permission denied", "permission"},
		{"not found", "no such file or directory", "not_found"},
		{"unknown", "syntax error at line 42", "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyError(tc.text); got != tc.want {
				t.Errorf("classifyError(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}
