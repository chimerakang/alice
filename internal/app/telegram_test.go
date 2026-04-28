package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-tg-agent/internal/app/hermes"
)

func TestExtractHermesActionableGoal(t *testing.T) {
	goal := `[Previous conversation context]
User: 先分析 webrtc-server
Assistant: 找到四個待修問題

[Current request]
好，請幫忙修正`

	if got := extractHermesActionableGoal(goal); got != "好，請幫忙修正" {
		t.Fatalf("expected actionable goal, got %q", got)
	}
}

func TestComposeHermesGoalWithContext(t *testing.T) {
	tasks := []hermes.TaskState{
		{
			Goal:        "先分析 webrtc-server",
			Accumulated: "列出 4 個待修問題：1. MediaSourceService 沒註冊到 HTTP gateway。2. JWK 環境變數名稱不一致。3. LOG_LEVEL 與 ENV 不同步。4. OBS 文件 API 路徑寫錯。",
			UpdatedAt:   time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC),
		},
	}
	recentMessages := []contextMessage{
		{Role: "user", Content: "先分析 webrtc-server"},
		{Role: "assistant", Content: "我找到 4 個待修問題：MediaSourceService、JWK、LOG_LEVEL、OBS 文件。"},
		{Role: "user", Content: "好，請幫忙修正"},
	}

	goal := composeHermesGoalWithContext("好，請幫忙修正", tasks, recentMessages)

	for _, want := range []string{
		hermesPreviousContextHeader,
		"MediaSourceService 沒註冊到 HTTP gateway",
		"JWK 環境變數名稱不一致",
		"LOG_LEVEL 與 ENV 不同步",
		"OBS 文件 API 路徑寫錯",
		"Assistant: 我找到 4 個待修問題：MediaSourceService、JWK、LOG_LEVEL、OBS 文件。",
		"User: 好，請幫忙修正",
		hermesCurrentRequestHeader,
		"好，請幫忙修正",
	} {
		if !strings.Contains(goal, want) {
			t.Fatalf("expected composed goal to contain %q, got:\n%s", want, goal)
		}
	}
}

func TestComposeHermesGoalWithContextStripsNestedInjectedGoal(t *testing.T) {
	tasks := []hermes.TaskState{
		{
			Goal: `[Previous conversation context]
User: 先分析 webrtc-server
Assistant: 找到 4 個待修問題

[Current request]
好，請幫忙修正`,
			Accumulated: "已修正 4 個問題。",
		},
	}

	goal := composeHermesGoalWithContext("再幫我補測試", tasks, nil)
	if strings.Count(goal, "好，請幫忙修正") != 1 {
		t.Fatalf("expected prior actionable goal to appear once, got:\n%s", goal)
	}
	if !strings.Contains(goal, "再幫我補測試") {
		t.Fatalf("expected current request in goal, got:\n%s", goal)
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

func TestStrictCommandDispatchAndToggle(t *testing.T) {
	key := chatKey{chatID: 7, threadID: 11}
	bot := &TelegramBot{
		config:       &Config{},
		hermesCoords: map[chatKey]*hermesCoord{},
		messageQueue: make(chan *TelegramMessage, 4),
	}

	bot.handleCommand(key, "/strict")
	assertQueuedMessageContains(t, bot.messageQueue, "strict review mode 已啟用")
	if !bot.strictModeEnabled(key, "") {
		t.Fatal("expected strict mode override to enable after /strict toggle")
	}

	bot.handleCommand(key, "/strict off")
	assertQueuedMessageContains(t, bot.messageQueue, "strict review mode 已停用")
	if bot.strictModeEnabled(key, "") {
		t.Fatal("expected strict mode override to disable after /strict off")
	}

	bot.handleCommand(key, "/strict status")
	assertQueuedMessageContains(t, bot.messageQueue, "strict review mode：已停用")
}

func TestStrictModeAutoEnableFromRiskVerbs(t *testing.T) {
	cases := []struct {
		name string
		goal string
		want bool
	}{
		{name: "commit", goal: "請 commit 這次修改", want: true},
		{name: "push", goal: "先整理後 push 到 main", want: true},
		{name: "deploy", goal: "完成部署到 production", want: true},
		{name: "ssh", goal: "ssh 到主機確認服務", want: true},
		{name: "release", goal: "release v1.2.3", want: true},
		{name: "non-risk", goal: "請幫我整理文件", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldAutoEnableStrict(tc.goal); got != tc.want {
				t.Fatalf("shouldAutoEnableStrict(%q) = %v, want %v", tc.goal, got, tc.want)
			}
		})
	}
}

func TestResolveStrictModeConfigAutoEnablesFromGoal(t *testing.T) {
	key := chatKey{chatID: 99, threadID: 3}
	bot := &TelegramBot{
		config: &Config{},
		hermesCoords: map[chatKey]*hermesCoord{
			key: &hermesCoord{},
		},
	}

	cfg := bot.resolveStrictModeConfig(key, "請 commit 並 push 變更")
	if !cfg.Enabled {
		t.Fatalf("expected strict config to auto-enable for risk verb goal, got %+v", cfg)
	}

	cfg = bot.resolveStrictModeConfig(key, "只是說明文件內容")
	if cfg.Enabled {
		t.Fatalf("expected strict config to stay disabled for non-risk goal, got %+v", cfg)
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

func TestHandleMessageHermesEnabledIssueRefFetchesIssue(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	const userID int64 = 123
	const projectDir = "/tmp/alice-project"

	called := make(chan struct{}, 1)
	oldFetchIssue := hermesFetchIssue
	hermesFetchIssue = func(ctx context.Context, gotProjectDir string, gotIssueNumber int) (*hermes.IssueContext, error) {
		if gotProjectDir != projectDir {
			t.Errorf("project dir: want %q, got %q", projectDir, gotProjectDir)
		}
		if gotIssueNumber != 109 {
			t.Errorf("issue number: want 109, got %d", gotIssueNumber)
		}
		called <- struct{}{}
		return nil, errors.New("stop after fetch")
	}
	defer func() { hermesFetchIssue = oldFetchIssue }()

	bot := &TelegramBot{
		agents: map[chatKey]*Agent{
			key: NewAgent(&mockClient{}, projectDir, key.chatID, key.threadID),
		},
		allowIDs: map[int64]bool{userID: true},
		config: &Config{
			Hermes: HermesConfig{Enabled: true},
		},
		hermesCoords: map[chatKey]*hermesCoord{
			key: {enabled: true},
		},
		messageQueue: make(chan *TelegramMessage, 10),
	}

	bot.handleMessage(key, userID, "接下來請處理 #109", "", nil, nil, nil, "", 1)

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("expected Hermes issue reference to fetch GitHub issue")
	}
}

func TestHermesPlannerSessionCacheReusesSameTier(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		hermesCoords: map[chatKey]*hermesCoord{
			key: {enabled: true, tier: "codex"},
		},
	}

	bot.recordPlannerSession(key, "codex", "sess-123")
	if got := bot.plannerSessionForTier(key, "codex"); got != "sess-123" {
		t.Fatalf("expected cached planner session, got %q", got)
	}
}

func TestHermesTierChangeClearsPlannerSessionCache(t *testing.T) {
	key := chatKey{chatID: 99, threadID: 3}
	bot := &TelegramBot{
		hermesCoords: map[chatKey]*hermesCoord{
			key: {
				enabled:            true,
				tier:               "claude",
				plannerSessionID:   "sess-old",
				plannerSessionTier: "claude",
			},
		},
	}

	bot.setHermesTier(key, "codex")
	hc := bot.hermesCoords[key]
	if hc == nil {
		t.Fatal("expected hermes coord to exist")
	}
	if hc.plannerSessionID != "" || hc.plannerSessionTier != "" {
		t.Fatalf("expected planner session cache to be cleared, got %+v", hc)
	}

	bot.recordPlannerSession(key, "claude", "sess-should-not-stick")
	if got := bot.plannerSessionForTier(key, "codex"); got != "" {
		t.Fatalf("expected no planner session for codex tier after switch, got %q", got)
	}
}

func TestHermesExecutorSessionCacheReusesSameTier(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		hermesCoords: map[chatKey]*hermesCoord{
			key: {enabled: true, tier: "codex"},
		},
	}

	bot.recordExecutorSession(key, "codex", "exec-sess-123")
	if got := bot.executorSessionForTier(key, "codex"); got != "exec-sess-123" {
		t.Fatalf("expected cached executor session, got %q", got)
	}
}

func TestHermesTierChangeClearsExecutorSessionCache(t *testing.T) {
	key := chatKey{chatID: 99, threadID: 3}
	bot := &TelegramBot{
		hermesCoords: map[chatKey]*hermesCoord{
			key: {
				enabled:             true,
				tier:                "claude",
				executorSessionID:   "exec-old",
				executorSessionTier: "claude",
			},
		},
	}

	bot.setHermesTier(key, "codex")
	hc := bot.hermesCoords[key]
	if hc == nil {
		t.Fatal("expected hermes coord to exist")
	}
	if hc.executorSessionID != "" || hc.executorSessionTier != "" {
		t.Fatalf("expected executor session cache to be cleared, got %+v", hc)
	}

	// A stale session recorded for the old tier must not be accepted
	bot.recordExecutorSession(key, "claude", "exec-should-not-stick")
	if got := bot.executorSessionForTier(key, "codex"); got != "" {
		t.Fatalf("expected no executor session for codex tier after switch, got %q", got)
	}
}

func TestHandleHermesStatsCommandWeekQueuesWeeklyReviewReport(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	s := newTestSQLiteStorage(t)
	oldStorage := globalStorage
	globalStorage = s
	defer func() { globalStorage = oldStorage }()

	now := time.Now().UTC().Truncate(time.Second)
	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:        "task-hermes-week",
		Goal:      "weekly stats",
		Engine:    "plan_execute",
		Backend:   "codex",
		Status:    "done",
		StartedAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertUnifiedTask: %v", err)
	}
	if err := s.UpsertUnifiedSubTask(UnifiedSubTask{
		ID:          "task-hermes-week:s1",
		TaskID:      "task-hermes-week",
		Idx:         0,
		Description: "step",
		Status:      "done",
		StartedAt:   now.Add(-30 * time.Minute),
	}); err != nil {
		t.Fatalf("UpsertUnifiedSubTask: %v", err)
	}
	reviewID, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:        "task-hermes-week",
		ReviewerModel: "gpt-5.5",
		Verdict:       "partial",
		OverallScore:  66,
		FeedbackText:  "more validation needed",
		IssueTags:     []string{"missing_validation"},
		CreatedAt:     now.Add(-20 * time.Minute),
	})
	if err != nil {
		t.Fatalf("InsertUnifiedReviewResult: %v", err)
	}
	if err := s.InsertUnifiedReviewSubTaskResult(UnifiedReviewSubTaskResult{
		ReviewID:  reviewID,
		SubTaskID: "task-hermes-week:s1",
		Score:     60,
		Feedback:  "run tests",
		IssueTags: []string{"missing_validation"},
	}); err != nil {
		t.Fatalf("InsertUnifiedReviewSubTaskResult: %v", err)
	}

	bot := &TelegramBot{
		config:          &Config{Hermes: HermesConfig{Enabled: true}},
		i18n:            newTestI18nManager(t),
		messageQueue:    make(chan *TelegramMessage, 10),
		langPreferences: map[int64]string{},
	}
	bot.setChatlanguage(key.chatID, "zh-TW")
	bot.handleHermesStatsCommand(key, []string{"/hermes-stats", "week"})

	select {
	case msg := <-bot.messageQueue:
		text, _ := msg.Params["text"].(string)
		if !strings.Contains(text, "Hermes Review 週報") || !strings.Contains(text, "missing_validation") || !strings.Contains(text, "Planner 建議") {
			t.Fatalf("unexpected message text:\n%s", text)
		}
	case <-time.After(time.Second):
		t.Fatal("expected weekly report message")
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

	bot.handleMessage(key, 123, "/tasks", "", nil, nil, nil, "", 1)

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

func TestHermesExecutorSessionReusesSameTierAndClearsOnSwitch(t *testing.T) {
	key := chatKey{chatID: 55, threadID: 2}
	bot := &TelegramBot{
		hermesCoords: map[chatKey]*hermesCoord{},
	}

	bot.setHermesTier(key, "codex")
	bot.recordExecutorSession(key, "codex", "exec-codex-1")
	if got := bot.executorSessionForTier(key, "codex"); got != "exec-codex-1" {
		t.Fatalf("expected cached codex executor session, got %q", got)
	}

	// Same tier re-selection must keep cached session
	bot.setHermesTier(key, "codex")
	if got := bot.executorSessionForTier(key, "codex"); got != "exec-codex-1" {
		t.Fatalf("expected same-tier executor session to persist, got %q", got)
	}

	// Tier switch clears executor session cache
	bot.setHermesTier(key, "claude")
	if got := bot.executorSessionForTier(key, "claude"); got != "" {
		t.Fatalf("expected cleared executor session after tier switch, got %q", got)
	}

	// Stale codex session must not be accepted after switch
	bot.recordExecutorSession(key, "codex", "exec-codex-stale")
	if got := bot.executorSessionForTier(key, "codex"); got != "" {
		t.Fatalf("expected no codex executor session after tier switch, got %q", got)
	}
}

func TestHermesPlannerSessionReusesSameTierAndClearsOnSwitch(t *testing.T) {
	key := chatKey{chatID: 123, threadID: 9}
	bot := &TelegramBot{
		hermesCoords: map[chatKey]*hermesCoord{},
	}

	bot.setHermesTier(key, "codex")
	bot.recordPlannerSession(key, "codex", "sess-codex-1")
	if got := bot.plannerSessionForTier(key, "codex"); got != "sess-codex-1" {
		t.Fatalf("expected cached codex planner session, got %q", got)
	}

	// Re-selecting the same backend must keep the cached session intact.
	bot.setHermesTier(key, "codex")
	if got := bot.plannerSessionForTier(key, "codex"); got != "sess-codex-1" {
		t.Fatalf("expected same-tier planner session to persist, got %q", got)
	}

	// Switching back to Claude clears the Codex planner session cache.
	bot.setHermesTier(key, "claude")
	if got := bot.plannerSessionForTier(key, "claude"); got != "" {
		t.Fatalf("expected cleared planner session after tier switch, got %q", got)
	}

	// A stale Codex resume ID must not be accepted after the tier flip.
	bot.recordPlannerSession(key, "codex", "sess-codex-stale")
	if got := bot.plannerSessionForTier(key, "codex"); got != "" {
		t.Fatalf("expected no codex planner session after tier switch, got %q", got)
	}
}
