package app

import (
	"context"
	"errors"
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
