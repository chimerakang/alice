package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	appengine "claude-tg-agent/internal/app/engine"
)

type retryCommandTestClient struct {
	streamCalls     int
	planCalls       int
	reviewSubTaskID string
}

type blockingRetryClient struct {
	started chan struct{}
}

func (c *retryCommandTestClient) Call(ctx context.Context, message, projectDir, sessionID, modelOverride string) (*CLIResponse, error) {
	return nil, nil
}

func (c *retryCommandTestClient) CallStream(ctx context.Context, message, projectDir, sessionID, modelOverride string, onToolUse func(toolName string, toolInput map[string]interface{}), onContent func(contentType, text string)) (*CLIResponse, error) {
	c.streamCalls++
	resp := &CLIResponse{
		Result:      "retry evidence",
		TextContent: "retry evidence",
		SessionID:   "retry-session",
	}
	resp.Usage.InputTokens = 10
	resp.Usage.OutputTokens = 5
	return resp, nil
}

func (c *retryCommandTestClient) CallPlan(ctx context.Context, message, projectDir, sessionID, modelOverride string, onContent func(contentType, text string)) (*CLIResponse, error) {
	c.planCalls++
	subTaskID := c.reviewSubTaskID
	if subTaskID == "" {
		subTaskID = "task-retry:s2"
	}
	reviewJSON := fmt.Sprintf(`{"verdict":"pass","overall_score":84,"feedback":"retry fixed validation","issue_tags":[],"sub_task_results":[{"sub_task_id":%q,"score":84,"feedback":"ok","issue_tags":[]}]}`, subTaskID)
	if onContent != nil {
		onContent("text", reviewJSON)
	}
	resp := &CLIResponse{
		Result:      reviewJSON,
		TextContent: reviewJSON,
	}
	resp.Usage.InputTokens = 7
	resp.Usage.OutputTokens = 3
	return resp, nil
}

func (c *retryCommandTestClient) GetModel() string {
	return "gpt-5.4"
}

func (c *blockingRetryClient) Call(ctx context.Context, message, projectDir, sessionID, modelOverride string) (*CLIResponse, error) {
	return nil, nil
}

func (c *blockingRetryClient) CallStream(ctx context.Context, message, projectDir, sessionID, modelOverride string, onToolUse func(toolName string, toolInput map[string]interface{}), onContent func(contentType, text string)) (*CLIResponse, error) {
	close(c.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *blockingRetryClient) CallPlan(ctx context.Context, message, projectDir, sessionID, modelOverride string, onContent func(contentType, text string)) (*CLIResponse, error) {
	return nil, ctx.Err()
}

func (c *blockingRetryClient) GetModel() string {
	return "gpt-5.5"
}

func seedRetryReview(t *testing.T, s *SQLiteStorage, taskID string, score int, source string) int64 {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:                taskID,
		ChatID:            42,
		ThreadID:          7,
		ProjectDir:        "/repo",
		GithubIssueNumber: 136,
		Goal:              "fix review debt",
		Engine:            "plan_execute",
		Backend:           "codex",
		Status:            "done",
		StartedAt:         now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertUnifiedTask: %v", err)
	}
	for idx, desc := range []string{"strong step", "weak step"} {
		if err := s.UpsertUnifiedSubTask(UnifiedSubTask{
			ID:          taskID + ":s" + string(rune('1'+idx)),
			TaskID:      taskID,
			Idx:         idx,
			Description: desc,
			Status:      "done",
			ResultText:  "done",
			StartedAt:   now.Add(-30 * time.Minute),
		}); err != nil {
			t.Fatalf("UpsertUnifiedSubTask: %v", err)
		}
	}
	reviewID, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:        taskID,
		ReviewerModel: "gpt-5.5",
		Verdict:       "partial",
		OverallScore:  score,
		FeedbackText:  "needs validation",
		IssueTags:     []string{"missing_validation"},
		Source:        source,
		CreatedAt:     now.Add(-10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("InsertUnifiedReviewResult: %v", err)
	}
	if err := s.InsertUnifiedReviewSubTaskResult(UnifiedReviewSubTaskResult{
		ReviewID:  reviewID,
		SubTaskID: taskID + ":s1",
		Score:     90,
		Feedback:  "ok",
	}); err != nil {
		t.Fatalf("InsertUnifiedReviewSubTaskResult strong: %v", err)
	}
	if err := s.InsertUnifiedReviewSubTaskResult(UnifiedReviewSubTaskResult{
		ReviewID:  reviewID,
		SubTaskID: taskID + ":s2",
		Score:     score,
		Feedback:  "run tests",
		IssueTags: []string{"missing_validation"},
	}); err != nil {
		t.Fatalf("InsertUnifiedReviewSubTaskResult weak: %v", err)
	}
	return reviewID
}

func waitRetryMessageContaining(t *testing.T, bot *TelegramBot, want string) string {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case msg := <-bot.messageQueue:
			text, _ := msg.Params["text"].(string)
			if strings.Contains(text, want) {
				return text
			}
		case <-deadline:
			t.Fatalf("timed out waiting for retry message containing %q", want)
		}
	}
}

func TestSelectRetryTaskCandidatesListsLatestFailedReviewTasks(t *testing.T) {
	s := newTestSQLiteStorage(t)
	seedRetryReview(t, s, "task-retry-candidate", 61, "review")

	candidates, err := s.selectRetryTaskCandidates(context.Background(), chatKey{chatID: 42, threadID: 7}, 5)
	if err != nil {
		t.Fatalf("selectRetryTaskCandidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1: %+v", len(candidates), candidates)
	}
	if candidates[0].ID != "task-retry-candidate" || candidates[0].GithubIssueNumber != 136 || candidates[0].FailedCount != 1 {
		t.Fatalf("unexpected candidate: %+v", candidates[0])
	}
}

func TestComposeRetryPromptIncludesReviewContext(t *testing.T) {
	prompt := composeRetryPrompt("修正 webhook", "partial", 64, "缺少測試", []string{"missing_validation"}, "上一輪只有摘要")

	for _, want := range []string{
		"[Retry — 上一輪 review 給 partial 64/100]",
		"原 sub-task 描述：\n修正 webhook",
		"上一輪 sub-task 輸出：\n上一輪只有摘要",
		"Reviewer 找出的問題：\n缺少測試",
		"- missing_validation",
		"不要做超出範圍的改動",
		"關鍵檔案行號",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "<= 2 行") {
		t.Fatalf("retry prompt should not ask for a two-line report:\n%s", prompt)
	}
}

func TestSelectRetryTargetLatestChoosesLowestScoredSubTask(t *testing.T) {
	s := newTestSQLiteStorage(t)
	seedRetryReview(t, s, "task-retry", 64, "initial")

	selection, err := s.selectRetryTargetLatest(context.Background(), chatKey{chatID: 42, threadID: 7})
	if err != nil {
		t.Fatalf("selectRetryTargetLatest: %v", err)
	}
	if selection.Task.ID != "task-retry" || selection.SubTask.ID != "task-retry:s2" {
		t.Fatalf("unexpected selection: %+v", selection)
	}
	if selection.DisplaySubTaskIdx != 2 || selection.SubTaskReview.Score != 64 {
		t.Fatalf("unexpected subtask metadata: %+v", selection)
	}
}

func TestSelectRetryTargetsAllFailedDoesNotDeadlockWithSingleSQLiteConn(t *testing.T) {
	s := newTestSQLiteStorage(t)
	seedRetryReview(t, s, "task-retry-all", 64, "initial")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	selections, err := s.selectRetryTargetsAllFailed(ctx, "task-retry-all")
	if err != nil {
		t.Fatalf("selectRetryTargetsAllFailed: %v", err)
	}
	if len(selections) != 1 {
		t.Fatalf("selection count = %d, want 1", len(selections))
	}
	if selections[0].SubTask.ID != "task-retry-all:s2" || selections[0].SubTaskReview.Score != 64 {
		t.Fatalf("lowest selection first = %+v", selections[0])
	}
}

func TestRetryCountCountsRetrySourceOnly(t *testing.T) {
	s := newTestSQLiteStorage(t)
	seedRetryReview(t, s, "task-retry-count", 60, "initial")
	seedRetryReview(t, s, "task-retry-count", 58, "retry")

	selection, err := s.selectRetryTargetByIndex(context.Background(), "task-retry-count", 2)
	if err != nil {
		t.Fatalf("selectRetryTargetByIndex: %v", err)
	}
	if selection.RetryCount != 1 {
		t.Fatalf("RetryCount = %d, want 1", selection.RetryCount)
	}
	if selection.Review.Source != "retry" {
		t.Fatalf("latest review source = %q, want retry", selection.Review.Source)
	}
}

func TestRetrySelectionRemembersCodexReviewHistory(t *testing.T) {
	s := newTestSQLiteStorage(t)
	seedRetryReview(t, s, "task-codex-retry", 60, "initial")
	if _, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:        "task-codex-retry",
		ReviewerModel: "gpt-5.5",
		Verdict:       "partial",
		OverallScore:  65,
		Source:        "initial",
		CreatedAt:     time.Now().UTC().Add(-20 * time.Minute),
	}); err != nil {
		t.Fatalf("InsertUnifiedReviewResult codex marker: %v", err)
	}

	selection, err := s.selectRetryTargetByIndex(context.Background(), "task-codex-retry", 2)
	if err != nil {
		t.Fatalf("selectRetryTargetByIndex: %v", err)
	}
	if !selection.PreferCodex {
		t.Fatalf("PreferCodex = false, want true")
	}
}

func TestSelectRetryTargetByIndexAcceptsTaskIDPrefix(t *testing.T) {
	s := newTestSQLiteStorage(t)
	seedRetryReview(t, s, "12345678-aaaa-bbbb-cccc-000000000001", 61, "initial")

	selection, err := s.selectRetryTargetByIndex(context.Background(), "12345678", 2)
	if err != nil {
		t.Fatalf("selectRetryTargetByIndex prefix: %v", err)
	}
	if selection.Task.ID != "12345678-aaaa-bbbb-cccc-000000000001" || selection.DisplaySubTaskIdx != 2 {
		t.Fatalf("unexpected prefix selection: %+v", selection)
	}
}

func TestSelectRetryTargetByIndexAcceptsGitHubIssueRef(t *testing.T) {
	s := newTestSQLiteStorage(t)
	seedRetryReview(t, s, "issue-linked-task", 61, "initial")

	selection, err := s.selectRetryTargetByIndex(context.Background(), "#136", 2)
	if err != nil {
		t.Fatalf("selectRetryTargetByIndex issue ref: %v", err)
	}
	if selection.Task.ID != "issue-linked-task" || selection.Task.GithubIssueNumber != 136 || selection.DisplaySubTaskIdx != 2 {
		t.Fatalf("unexpected issue ref selection: %+v", selection)
	}
}

func TestSelectRetryTargetByIndexAcceptsLegacyGitHubIssueGoal(t *testing.T) {
	s := newTestSQLiteStorage(t)
	seedRetryReview(t, s, "legacy-issue-linked-task", 61, "initial")
	if _, err := s.db.Exec(`UPDATE tasks SET github_issue_number = 0, goal = ? WHERE id = ?`, "[GitHub #180] Fix retry", "legacy-issue-linked-task"); err != nil {
		t.Fatalf("patch legacy issue task: %v", err)
	}

	selection, err := s.selectRetryTargetByIndex(context.Background(), "#180", 2)
	if err != nil {
		t.Fatalf("selectRetryTargetByIndex legacy issue ref: %v", err)
	}
	if selection.Task.ID != "legacy-issue-linked-task" || selection.DisplaySubTaskIdx != 2 {
		t.Fatalf("unexpected legacy issue ref selection: %+v", selection)
	}
}

func TestSelectRetryTargetByIndexAllowsHighScoreManualRetry(t *testing.T) {
	s := newTestSQLiteStorage(t)
	seedRetryReview(t, s, "task-high-score-manual", 61, "initial")

	selection, err := s.selectRetryTargetByIndex(context.Background(), "task-high-score-manual", 1)
	if err != nil {
		t.Fatalf("selectRetryTargetByIndex high score: %v", err)
	}
	if selection.SubTask.ID != "task-high-score-manual:s1" || selection.SubTaskReview.Score != 90 {
		t.Fatalf("unexpected high-score selection: %+v", selection)
	}
	if !retrySelectionNeedsRetry(selection) {
		t.Fatalf("partial review selection should remain retryable: %+v", selection)
	}
}

func TestHandleRetryCommandIndexHighScorePassReturnsNoRetryNeeded(t *testing.T) {
	s := newTestSQLiteStorage(t)
	now := time.Now().UTC().Truncate(time.Second)
	taskID := "task-high-score-pass"
	subTaskID := taskID + ":s4"
	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:         taskID,
		ChatID:     42,
		ThreadID:   7,
		ProjectDir: "/repo",
		Goal:       "high score pass",
		Engine:     "plan_execute",
		Backend:    "codex",
		Status:     "done",
		StartedAt:  now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertUnifiedTask: %v", err)
	}
	if err := s.UpsertUnifiedSubTask(UnifiedSubTask{
		ID:          subTaskID,
		TaskID:      taskID,
		Idx:         3,
		Description: "already good",
		Status:      "done",
		ResultText:  "done",
		StartedAt:   now.Add(-30 * time.Minute),
	}); err != nil {
		t.Fatalf("UpsertUnifiedSubTask: %v", err)
	}
	if err := s.StoreReviewWithSource(context.Background(), taskID, appengine.ReviewResult{
		ReviewerModel: "gpt-5.5",
		Verdict:       appengine.VerdictPass,
		OverallScore:  93,
		Feedback:      "ok",
		SubTaskResults: []appengine.ReviewSubTaskResult{{
			SubTaskID: subTaskID,
			Score:     93,
			Feedback:  "ok",
		}},
	}, "review"); err != nil {
		t.Fatalf("StoreReviewWithSource: %v", err)
	}

	oldStorage := globalStorage
	globalStorage = s
	t.Cleanup(func() { globalStorage = oldStorage })

	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 1)}
	bot.handleRetryCommand(key, []string{"/retry", taskID, "4"})

	select {
	case msg := <-bot.messageQueue:
		text, _ := msg.Params["text"].(string)
		if !strings.Contains(text, "Sub-task #4 評分 93/100") || !strings.Contains(text, "無需 retry") {
			t.Fatalf("unexpected no-retry message: %q", text)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for no-retry message")
	}
}

func TestHandleRetryCommandIndexLowScoreExecutesRetry(t *testing.T) {
	s := newTestSQLiteStorage(t)
	taskID := "task-low-score-index-retry"
	seedRetryReview(t, s, taskID, 61, "review")

	oldStorage := globalStorage
	globalStorage = s
	t.Cleanup(func() { globalStorage = oldStorage })

	key := chatKey{chatID: 42, threadID: 7}
	client := &retryCommandTestClient{reviewSubTaskID: taskID + ":s2"}
	bot := &TelegramBot{
		config:       &Config{DefaultProjectDir: "/repo"},
		client:       client,
		agents:       make(map[chatKey]*Agent),
		chatContexts: make(map[chatKey]*ChatContext),
		messageQueue: make(chan *TelegramMessage, 8),
	}

	bot.handleRetryCommand(key, []string{"/retry", taskID, "2"})

	startText := waitRetryMessageContaining(t, bot, "Retrying sub-task #2")
	if !strings.Contains(startText, "原分數 61/100") {
		t.Fatalf("retry start message missing score: %q", startText)
	}
	doneText := waitRetryMessageContaining(t, bot, "Retry 驗證通過")
	if !strings.Contains(doneText, "原分數: 61 → 84") || !strings.Contains(doneText, "Review 回饋：\nok") {
		t.Fatalf("unexpected retry completion message: %q", doneText)
	}
	if client.streamCalls != 1 || client.planCalls != 1 {
		t.Fatalf("client calls = stream %d plan %d, want 1/1", client.streamCalls, client.planCalls)
	}
}

func TestRunSubTaskRetryReturnsBusyWhenAgentAlreadyProcessing(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	agent := NewAgent(&retryCommandTestClient{}, "/repo", key.chatID, key.threadID)
	agent.transitionExecution(appengine.ExecutionStateStarting, "test_busy")
	bot := &TelegramBot{
		config:       &Config{DefaultProjectDir: "/repo"},
		agents:       map[chatKey]*Agent{key: agent},
		chatContexts: map[chatKey]*ChatContext{key: agent.chatContext},
		messageQueue: make(chan *TelegramMessage, 4),
	}

	_, err := bot.runSubTaskRetry(context.Background(), key, nil, retrySelection{
		Task:          UnifiedTask{ProjectDir: "/repo"},
		SubTask:       UnifiedSubTask{Description: "retry blocked work"},
		SubTaskReview: UnifiedReviewSubTaskResult{Score: 61},
	})
	if err == nil || !strings.Contains(err.Error(), "bot 還在執行上一個請求") {
		t.Fatalf("runSubTaskRetry busy error = %v", err)
	}
}

func TestRunRetryDirectWithWatchdogAbortsOnContextTimeout(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	client := &blockingRetryClient{started: make(chan struct{})}
	agent := NewAgent(client, "/repo", key.chatID, key.threadID)
	agent.cliTimeoutMinutes = 1
	bot := &TelegramBot{}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := bot.runRetryDirectWithWatchdog(ctx, agent, "retry prompt", nil)
	if err == nil {
		t.Fatal("runRetryDirectWithWatchdog error = nil, want timeout/cancel error")
	}
	select {
	case <-client.started:
	default:
		t.Fatal("blocking client was not started")
	}
	if agent.IsProcessing() {
		t.Fatal("agent still processing after watchdog abort")
	}
}

func TestHandleRetryCommandIndexMissingSubTaskReturnsNotFound(t *testing.T) {
	s := newTestSQLiteStorage(t)
	taskID := "task-index-not-found"
	seedRetryReview(t, s, taskID, 61, "review")

	oldStorage := globalStorage
	globalStorage = s
	t.Cleanup(func() { globalStorage = oldStorage })

	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 1)}
	bot.handleRetryCommand(key, []string{"/retry", taskID, "3"})

	text := waitRetryMessageContaining(t, bot, "找不到 task task-ind 的 sub-task #3")
	if !strings.HasPrefix(text, "❌ ") {
		t.Fatalf("missing idx should be returned as error message: %q", text)
	}
}

func TestHandleRetryCommandIndexWithoutSubTaskScoresShowsDiagnostic(t *testing.T) {
	s := newTestSQLiteStorage(t)
	now := time.Now().UTC().Truncate(time.Second)
	taskID := "task-missing-subtask-scores"
	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:         taskID,
		ChatID:     42,
		ThreadID:   7,
		ProjectDir: "/repo",
		Status:     "done",
		StartedAt:  now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertUnifiedTask: %v", err)
	}
	for idx := 0; idx < 2; idx++ {
		if err := s.UpsertUnifiedSubTask(UnifiedSubTask{
			ID:          fmt.Sprintf("%s:s%d", taskID, idx+1),
			TaskID:      taskID,
			Idx:         idx,
			Description: fmt.Sprintf("step %d", idx+1),
			Status:      "done",
			ResultText:  "done",
			StartedAt:   now.Add(-30 * time.Minute),
		}); err != nil {
			t.Fatalf("UpsertUnifiedSubTask %d: %v", idx+1, err)
		}
	}
	if _, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:        taskID,
		ReviewerModel: "gpt-5.5",
		Verdict:       "partial",
		OverallScore:  72,
		FeedbackText:  "latest summary omitted per-subtask rows",
		Source:        "review",
		CreatedAt:     now.Add(-10 * time.Minute),
	}); err != nil {
		t.Fatalf("InsertUnifiedReviewResult latest without rows: %v", err)
	}

	oldStorage := globalStorage
	globalStorage = s
	t.Cleanup(func() { globalStorage = oldStorage })

	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 1)}
	bot.handleRetryCommand(key, []string{"/retry", taskID, "2"})

	text := waitRetryMessageContaining(t, bot, "沒有 per-subtask 分數")
	if !strings.Contains(text, "⚠️ 最新 review") || !strings.Contains(text, "無法顯示低分 retry") {
		t.Fatalf("missing-score retry should return diagnostic error: %q", text)
	}
}

func TestRetryNoLowScoreDiagnosticResolvesTaskPrefix(t *testing.T) {
	s := newTestSQLiteStorage(t)
	now := time.Now().UTC().Truncate(time.Second)
	taskID := "12345678-missing-subtask-scores"
	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:        taskID,
		ChatID:    42,
		ThreadID:  7,
		Status:    "done",
		StartedAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertUnifiedTask: %v", err)
	}
	if _, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:        taskID,
		ReviewerModel: "gpt-5.5",
		Verdict:       "partial",
		OverallScore:  75,
		FeedbackText:  "summary without subtask rows",
		Source:        "review",
		CreatedAt:     now.Add(-10 * time.Minute),
	}); err != nil {
		t.Fatalf("InsertUnifiedReviewResult: %v", err)
	}

	diag, err := s.retryNoLowScoreDiagnostic(context.Background(), "12345678")
	if err != nil {
		t.Fatalf("retryNoLowScoreDiagnostic: %v", err)
	}
	if !strings.Contains(diag, "最新 review") || !strings.Contains(diag, "75/100") {
		t.Fatalf("diagnostic should resolve short task id, got %q", diag)
	}
}

func TestSelectRetryTargetByIndexReportsMissingSubTask(t *testing.T) {
	s := newTestSQLiteStorage(t)
	seedRetryReview(t, s, "task-missing-index", 61, "initial")

	_, err := s.selectRetryTargetByIndex(context.Background(), "task-missing-index", 3)
	if err == nil {
		t.Fatal("selectRetryTargetByIndex missing sub-task expected error")
	}
	if !strings.Contains(err.Error(), "找不到 task task-mis 的 sub-task #3") {
		t.Fatalf("unexpected missing sub-task error: %v", err)
	}
}

func TestSelectRetryTargetByIndexReportsMissingSubTaskScores(t *testing.T) {
	s := newTestSQLiteStorage(t)
	now := time.Now().UTC().Truncate(time.Second)
	taskID := "task-missing-review-row"
	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:        taskID,
		ChatID:    42,
		ThreadID:  7,
		Status:    "done",
		StartedAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertUnifiedTask: %v", err)
	}
	for idx := 0; idx < 2; idx++ {
		if err := s.UpsertUnifiedSubTask(UnifiedSubTask{
			ID:          fmt.Sprintf("%s:s%d", taskID, idx+1),
			TaskID:      taskID,
			Idx:         idx,
			Description: fmt.Sprintf("step %d", idx+1),
			Status:      "done",
			ResultText:  "done",
			StartedAt:   now.Add(-30 * time.Minute),
		}); err != nil {
			t.Fatalf("UpsertUnifiedSubTask %d: %v", idx+1, err)
		}
	}
	if _, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:        taskID,
		ReviewerModel: "gpt-5.5",
		Verdict:       "partial",
		OverallScore:  72,
		FeedbackText:  "latest summary omitted per-subtask rows",
		Source:        "review",
		CreatedAt:     now.Add(-10 * time.Minute),
	}); err != nil {
		t.Fatalf("InsertUnifiedReviewResult latest without rows: %v", err)
	}

	_, err := s.selectRetryTargetByIndex(context.Background(), taskID, 2)
	if err == nil {
		t.Fatal("selectRetryTargetByIndex fallback should fail when rows are missing")
	}
	if !strings.Contains(err.Error(), "沒有保存 sub-task #2 的細項評分") {
		t.Fatalf("unexpected missing-score diagnostic: %v", err)
	}
}

func TestSelectRetryTargetByIndexUsesLatestScoreForRequestedSubTask(t *testing.T) {
	s := newTestSQLiteStorage(t)
	taskID := "task-index-fallback-older-partial"
	seedRetryReview(t, s, taskID, 76, "initial")
	if _, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:        taskID,
		ReviewerModel: "gpt-5.4",
		Verdict:       "pass",
		OverallScore:  90,
		FeedbackText:  "retry fixed another sub-task",
		Source:        "retry",
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("InsertUnifiedReviewResult newer pass without rows: %v", err)
	}

	selection, err := s.selectRetryTargetByIndex(context.Background(), taskID, 2)
	if err != nil {
		t.Fatalf("selectRetryTargetByIndex should fall back to latest row for requested sub-task: %v", err)
	}
	if selection.SubTask.ID != taskID+":s2" || selection.SubTaskReview.Score != 76 {
		t.Fatalf("unexpected fallback selection: %+v", selection)
	}
}

func TestRetrySelectionAfterSingleSubTaskRetryKeepsOtherLowScoresVisible(t *testing.T) {
	s := newTestSQLiteStorage(t)
	now := time.Now().UTC().Truncate(time.Second)
	taskID := "task-retry-keeps-other-lows"
	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:                taskID,
		ChatID:            42,
		ThreadID:          7,
		ProjectDir:        "/repo",
		GithubIssueNumber: 134,
		Goal:              "two low subtasks",
		Engine:            "plan_execute",
		Backend:           "codex",
		Status:            "done",
		StartedAt:         now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertUnifiedTask: %v", err)
	}
	for idx := 0; idx < 6; idx++ {
		if err := s.UpsertUnifiedSubTask(UnifiedSubTask{
			ID:          fmt.Sprintf("%s:s%d", taskID, idx+1),
			TaskID:      taskID,
			Idx:         idx,
			Description: fmt.Sprintf("step %d", idx+1),
			Status:      "done",
			ResultText:  "done",
			StartedAt:   now.Add(-30 * time.Minute),
		}); err != nil {
			t.Fatalf("UpsertUnifiedSubTask %d: %v", idx+1, err)
		}
	}
	initialID, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:        taskID,
		ReviewerModel: "gpt-5.5",
		Verdict:       "partial",
		OverallScore:  78,
		Source:        "initial",
		CreatedAt:     now.Add(-10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("InsertUnifiedReviewResult initial: %v", err)
	}
	for idx, score := range []int{90, 85, 85, 85, 75, 65} {
		if err := s.InsertUnifiedReviewSubTaskResult(UnifiedReviewSubTaskResult{
			ReviewID:  initialID,
			SubTaskID: fmt.Sprintf("%s:s%d", taskID, idx+1),
			Score:     score,
			Feedback:  "initial review",
		}); err != nil {
			t.Fatalf("InsertUnifiedReviewSubTaskResult initial s%d: %v", idx+1, err)
		}
	}
	retryID, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:        taskID,
		ReviewerModel: "gpt-5.4",
		Verdict:       "pass",
		OverallScore:  82,
		Source:        "retry",
		CreatedAt:     now.Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("InsertUnifiedReviewResult retry: %v", err)
	}
	if err := s.InsertUnifiedReviewSubTaskResult(UnifiedReviewSubTaskResult{
		ReviewID:  retryID,
		SubTaskID: taskID + ":s6",
		Score:     82,
		Feedback:  "fixed s6",
	}); err != nil {
		t.Fatalf("InsertUnifiedReviewSubTaskResult retry s6: %v", err)
	}

	byIndex, err := s.selectRetryTargetByIndex(context.Background(), taskID, 5)
	if err != nil {
		t.Fatalf("selectRetryTargetByIndex should still find s5: %v", err)
	}
	if byIndex.SubTask.ID != taskID+":s5" || byIndex.SubTaskReview.Score != 75 {
		t.Fatalf("unexpected s5 retry selection: %+v", byIndex)
	}

	allFailed, err := s.selectRetryTargetsAllFailed(context.Background(), taskID)
	if err != nil {
		t.Fatalf("selectRetryTargetsAllFailed: %v", err)
	}
	if len(allFailed) != 1 || allFailed[0].SubTask.ID != taskID+":s5" {
		t.Fatalf("all-failed should include only still-low s5: %+v", allFailed)
	}

	candidates, err := s.selectRetryTaskCandidates(context.Background(), chatKey{chatID: 42, threadID: 7}, 5)
	if err != nil {
		t.Fatalf("selectRetryTaskCandidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].ID != taskID || candidates[0].FailedCount != 1 {
		t.Fatalf("candidate list should keep task visible with one remaining low score: %+v", candidates)
	}
}

func TestSelectRetryTargetsAllFailedFiltersLikeRetryableLowScoreSet(t *testing.T) {
	s := newTestSQLiteStorage(t)
	now := time.Now().UTC().Truncate(time.Second)
	taskID := "task-all-failed-filter"
	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:         taskID,
		ChatID:     42,
		ThreadID:   7,
		ProjectDir: "/repo",
		Goal:       "filter all failed",
		Engine:     "plan_execute",
		Backend:    "codex",
		Status:     "done",
		StartedAt:  now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertUnifiedTask: %v", err)
	}
	for idx, desc := range []string{"passed high score", "failed low score", "partial high score"} {
		if err := s.UpsertUnifiedSubTask(UnifiedSubTask{
			ID:          taskID + ":s" + string(rune('1'+idx)),
			TaskID:      taskID,
			Idx:         idx,
			Description: desc,
			Status:      "done",
			ResultText:  "done",
			StartedAt:   now.Add(-30 * time.Minute),
		}); err != nil {
			t.Fatalf("UpsertUnifiedSubTask %d: %v", idx+1, err)
		}
	}
	reviewID, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:        taskID,
		ReviewerModel: "gpt-5.5",
		Verdict:       "pass",
		OverallScore:  80,
		FeedbackText:  "one low score",
		Source:        "review",
		CreatedAt:     now.Add(-10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("InsertUnifiedReviewResult: %v", err)
	}
	for _, row := range []struct {
		subTaskID string
		score     int
	}{
		{taskID + ":s1", 93},
		{taskID + ":s2", 62},
		{taskID + ":s3", 88},
	} {
		if err := s.InsertUnifiedReviewSubTaskResult(UnifiedReviewSubTaskResult{
			ReviewID:  reviewID,
			SubTaskID: row.subTaskID,
			Score:     row.score,
			Feedback:  "review row",
		}); err != nil {
			t.Fatalf("InsertUnifiedReviewSubTaskResult %s: %v", row.subTaskID, err)
		}
	}

	selections, err := s.selectRetryTargetsAllFailed(context.Background(), taskID)
	if err != nil {
		t.Fatalf("selectRetryTargetsAllFailed: %v", err)
	}
	if len(selections) != 1 {
		t.Fatalf("selection count = %d, want 1: %+v", len(selections), selections)
	}
	if selections[0].SubTask.ID != taskID+":s2" || selections[0].DisplaySubTaskIdx != 2 || selections[0].SubTaskReview.Score != 62 {
		t.Fatalf("all-failed should include only low-score subtask: %+v", selections[0])
	}

	highSelection, err := s.selectRetryTargetByIndex(context.Background(), taskID, 1)
	if err != nil {
		t.Fatalf("selectRetryTargetByIndex high score: %v", err)
	}
	if retrySelectionNeedsRetry(highSelection) {
		t.Fatalf("high-score pass-equivalent selection should not be retryable: %+v", highSelection)
	}
	lowSelection, err := s.selectRetryTargetByIndex(context.Background(), taskID, 2)
	if err != nil {
		t.Fatalf("selectRetryTargetByIndex low score: %v", err)
	}
	if !retrySelectionNeedsRetry(lowSelection) {
		t.Fatalf("low-score index selection should be retryable: %+v", lowSelection)
	}
}

func TestSelectRetryTargetsAllFailedUsesLatestScorePerSubTask(t *testing.T) {
	s := newTestSQLiteStorage(t)
	now := time.Now().UTC().Truncate(time.Second)
	taskID := "task-all-failed-latest-per-subtask"
	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:        taskID,
		ChatID:    42,
		ThreadID:  7,
		Status:    "done",
		StartedAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertUnifiedTask: %v", err)
	}
	for idx := 0; idx < 3; idx++ {
		if err := s.UpsertUnifiedSubTask(UnifiedSubTask{
			ID:          fmt.Sprintf("%s:s%d", taskID, idx+1),
			TaskID:      taskID,
			Idx:         idx,
			Description: fmt.Sprintf("step %d", idx+1),
			Status:      "done",
			ResultText:  "done",
			StartedAt:   now.Add(-30 * time.Minute),
		}); err != nil {
			t.Fatalf("UpsertUnifiedSubTask %d: %v", idx+1, err)
		}
	}
	initialID, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:        taskID,
		ReviewerModel: "gpt-5.5",
		Verdict:       "partial",
		OverallScore:  84,
		Source:        "initial",
		CreatedAt:     now.Add(-10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("InsertUnifiedReviewResult initial: %v", err)
	}
	for _, row := range []struct {
		subTaskID string
		score     int
	}{
		{taskID + ":s1", 90},
		{taskID + ":s2", 76},
		{taskID + ":s3", 75},
	} {
		if err := s.InsertUnifiedReviewSubTaskResult(UnifiedReviewSubTaskResult{
			ReviewID:  initialID,
			SubTaskID: row.subTaskID,
			Score:     row.score,
			Feedback:  "initial review",
		}); err != nil {
			t.Fatalf("InsertUnifiedReviewSubTaskResult initial %s: %v", row.subTaskID, err)
		}
	}
	retryID, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:        taskID,
		ReviewerModel: "gpt-5.4",
		Verdict:       "pass",
		OverallScore:  90,
		Source:        "retry",
		CreatedAt:     now.Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("InsertUnifiedReviewResult retry: %v", err)
	}
	if err := s.InsertUnifiedReviewSubTaskResult(UnifiedReviewSubTaskResult{
		ReviewID:  retryID,
		SubTaskID: taskID + ":s2",
		Score:     90,
		Feedback:  "fixed",
	}); err != nil {
		t.Fatalf("InsertUnifiedReviewSubTaskResult retry: %v", err)
	}

	selections, err := s.selectRetryTargetsAllFailed(context.Background(), taskID)
	if err != nil {
		t.Fatalf("selectRetryTargetsAllFailed: %v", err)
	}
	if len(selections) != 1 {
		t.Fatalf("selection count = %d, want only still-low s3: %+v", len(selections), selections)
	}
	if selections[0].SubTask.ID != taskID+":s3" || selections[0].SubTaskReview.Score != 75 {
		t.Fatalf("unexpected remaining failed selection: %+v", selections[0])
	}

	selection, err := s.selectRetryTargetLowest(context.Background(), taskID)
	if err != nil {
		t.Fatalf("selectRetryTargetLowest: %v", err)
	}
	if selection.SubTask.ID != taskID+":s3" {
		t.Fatalf("lowest should skip fixed s2 and choose s3, got %+v", selection)
	}
}

func TestReviewNotificationRetryIndexMatchesSelectRetryTargetByIndex(t *testing.T) {
	s := newTestSQLiteStorage(t)
	now := time.Now().UTC().Truncate(time.Second)
	taskID := "12345678-review-index"
	subTaskID := taskID + ":s5"
	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:         taskID,
		ChatID:     42,
		ThreadID:   7,
		ProjectDir: "/repo",
		Goal:       "single high-score review row",
		Engine:     "plan_execute",
		Backend:    "codex",
		Status:     "done",
		StartedAt:  now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertUnifiedTask: %v", err)
	}
	if err := s.UpsertUnifiedSubTask(UnifiedSubTask{
		ID:          subTaskID,
		TaskID:      taskID,
		Idx:         4,
		Description: "subtask stored at DB idx 4",
		Status:      "done",
		ResultText:  "done",
		StartedAt:   now.Add(-30 * time.Minute),
	}); err != nil {
		t.Fatalf("UpsertUnifiedSubTask: %v", err)
	}
	review := appengine.ReviewResult{
		ReviewerModel: "gpt-5.5",
		Verdict:       appengine.VerdictPass,
		OverallScore:  93,
		Feedback:      "ok",
		SubTaskResults: []appengine.ReviewSubTaskResult{{
			SubTaskID: subTaskID,
			Score:     93,
			Feedback:  "ok",
		}},
	}
	if err := s.StoreReviewWithSource(context.Background(), taskID, review, "review"); err != nil {
		t.Fatalf("StoreReviewWithSource: %v", err)
	}

	text := appengine.BuildReviewNotification(taskID, review).TelegramText()
	if !strings.Contains(text, "/retry 12345678 5") {
		t.Fatalf("telegram text did not use retryable DB display idx:\n%s", text)
	}
	selection, err := s.selectRetryTargetByIndex(context.Background(), "12345678", 5)
	if err != nil {
		t.Fatalf("selectRetryTargetByIndex notification idx: %v", err)
	}
	if selection.SubTask.ID != subTaskID || selection.SubTaskReview.Score != 93 {
		t.Fatalf("unexpected notification idx selection: %+v", selection)
	}
}

func TestRetryProjectDirFallsBackForLegacyEmptyTaskProject(t *testing.T) {
	bot := &TelegramBot{config: &Config{DefaultProjectDir: "/default"}}

	got := bot.retryProjectDir(retrySelection{
		Task: UnifiedTask{ProjectDir: ""},
	}, &Agent{projectDir: "/active"})
	if got != "/active" {
		t.Fatalf("retryProjectDir agent fallback = %q, want /active", got)
	}

	got = bot.retryProjectDir(retrySelection{
		Task: UnifiedTask{ProjectDir: ""},
	}, nil)
	if got != "/default" {
		t.Fatalf("retryProjectDir default fallback = %q, want /default", got)
	}
}

func TestParseRetryArgs(t *testing.T) {
	tests := []struct {
		parts      []string
		wantMode   string
		wantTaskID string
		wantIdx    int
		wantError  bool
	}{
		{[]string{"/retry"}, "latest", "", 0, false},
		{[]string{"/retry", "latest"}, "latest", "", 0, false},
		{[]string{"/retry", "task-1"}, "lowest", "task-1", 0, false},
		{[]string{"/retry", "task-1", "3"}, "index", "task-1", 3, false},
		{[]string{"/retry", "task-1", "all-failed"}, "all-failed", "task-1", 0, false},
		{[]string{"/retry", "#136", "1"}, "index", "#136", 1, false},
		{[]string{"/retry", "#136", "all-failed"}, "all-failed", "#136", 0, false},
		{[]string{"/retry", "task-1", "bad"}, "", "", 0, true},
	}
	for _, tt := range tests {
		mode, taskID, idx, err := parseRetryArgs(tt.parts)
		if tt.wantError {
			if err == nil {
				t.Fatalf("parseRetryArgs(%v) expected error", tt.parts)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseRetryArgs(%v): %v", tt.parts, err)
		}
		if mode != tt.wantMode || taskID != tt.wantTaskID || idx != tt.wantIdx {
			t.Fatalf("parseRetryArgs(%v) = (%q,%q,%d), want (%q,%q,%d)", tt.parts, mode, taskID, idx, tt.wantMode, tt.wantTaskID, tt.wantIdx)
		}
	}
}

func TestNormalizeRetryReviewForSubTaskFillsPersistableSubTaskID(t *testing.T) {
	review := normalizeRetryReviewForSubTask(appengine.ReviewResult{
		Verdict:      appengine.VerdictPartial,
		OverallScore: 72,
		Feedback:     "needs validation",
		IssueTags:    []appengine.ReviewTag{appengine.ReviewTagMissingValidation},
	}, "task:s2")
	if len(review.SubTaskResults) != 1 || review.SubTaskResults[0].SubTaskID != "task:s2" {
		t.Fatalf("unexpected normalized review: %+v", review)
	}

	review = normalizeRetryReviewForSubTask(appengine.ReviewResult{
		Verdict:      appengine.VerdictPartial,
		OverallScore: 72,
		SubTaskResults: []appengine.ReviewSubTaskResult{{
			SubTaskID: "direct",
			Score:     70,
		}},
	}, "task:s2")
	if review.SubTaskResults[0].SubTaskID != "task:s2" {
		t.Fatalf("direct subtask id was not normalized: %+v", review.SubTaskResults[0])
	}
}

func TestRetryExecutionModelUsesCodexSmartForCodexReview(t *testing.T) {
	bot := &TelegramBot{config: &Config{
		ModelRouting: ModelRoutingConfig{
			EnableDynamicRouting: true,
			CodexSmartModel:      "gpt-5.4",
			CodexDeepModel:       "gpt-5.5",
			SmartModel:           "claude-sonnet-4-6",
			DeepModel:            "claude-opus-4-6",
		},
	}}
	selection := retrySelection{
		PreferCodex: true,
		Review:      UnifiedReviewResult{ReviewerModel: "claude-opus-4-6"},
	}

	if got := bot.retryExecutionModel(chatKey{}, selection); got != bot.config.ModelRouting.CodexSmartModel {
		t.Fatalf("retryExecutionModel = %q, want %q", got, bot.config.ModelRouting.CodexSmartModel)
	}
}

func TestRetryExecutionModelHonorsGPTDeepPreference(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config: &Config{
			DefaultProjectDir: "/repo",
			ModelRouting: ModelRoutingConfig{
				EnableDynamicRouting: true,
				CodexSmartModel:      "gpt-5.4",
				CodexDeepModel:       "gpt-5.5",
				SmartModel:           "claude-sonnet-4-6",
				DeepModel:            "claude-opus-4-6",
			},
		},
		chatContexts: map[chatKey]*ChatContext{
			key: NewChatContext(key.chatID, key.threadID, "/repo"),
		},
	}
	bot.chatContexts[key].Pref = ModelPreference("gpt-deep")
	selection := retrySelection{
		PreferCodex: true,
		Review:      UnifiedReviewResult{ReviewerModel: "gpt-5.4"},
	}

	if got := bot.retryExecutionModel(key, selection); got != "gpt-5.5" {
		t.Fatalf("retryExecutionModel = %q, want gpt-5.5", got)
	}
}

func TestRetryExecutionModelIgnoresMisconfiguredClaudeCodexHeavyModel(t *testing.T) {
	bot := &TelegramBot{config: &Config{
		Hermes: HermesConfig{
			CodexHeavyExecutorModel: "claude-sonnet-4-6",
		},
		ModelRouting: ModelRoutingConfig{
			CodexSmartModel: "gpt-5.4",
			CodexDeepModel:  "gpt-5.5",
		},
	}}
	selection := retrySelection{PreferCodex: true}

	if got := bot.retryExecutionModel(chatKey{}, selection); got != "gpt-5.4" {
		t.Fatalf("retryExecutionModel = %q, want gpt-5.4", got)
	}
}

func TestRetryReviewPayloadIncludesPreviousAndRetryResults(t *testing.T) {
	selection := retrySelection{
		SubTask: UnifiedSubTask{ResultText: "previous evidence"},
		SubTaskReview: UnifiedReviewSubTaskResult{
			Feedback:  "missing file references",
			IssueTags: []string{"incomplete_traceability"},
		},
	}

	accumulated := buildRetryReviewAccumulated(selection, "retry evidence")
	for _, want := range []string{"missing file references", "incomplete_traceability", "previous evidence", "retry evidence"} {
		if !strings.Contains(accumulated, want) {
			t.Fatalf("accumulated payload missing %q:\n%s", want, accumulated)
		}
	}
}

func TestFormatRetryCompletionIncludesResultAndReviewFeedback(t *testing.T) {
	outcome := retryOutcome{
		Selection: retrySelection{
			SubTask: UnifiedSubTask{ID: "task:s2"},
			SubTaskReview: UnifiedReviewSubTaskResult{
				Score: 55,
			},
		},
		Result: appengine.Result{
			Text: "make proto pass; npm run typecheck pass",
		},
		Review: appengine.ReviewResult{
			Verdict:      appengine.VerdictPass,
			OverallScore: 82,
			SubTaskResults: []appengine.ReviewSubTaskResult{{
				SubTaskID: "task:s2",
				Score:     82,
				Feedback:  "validation evidence is now present",
				IssueTags: []appengine.ReviewIssueTag{appengine.ReviewIssueTagMissingValidation},
			}},
		},
		Duration: 2*time.Minute + 58*time.Second,
	}

	got := formatRetryCompletion(outcome)
	for _, want := range []string{
		"✅ Retry 驗證通過",
		"原分數: 55 → 82 (+27)",
		"執行結果：\nmake proto pass; npm run typecheck pass",
		"Review 回饋：\nvalidation evidence is now present",
		"Issue tags: missing_validation",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatRetryCompletion missing %q:\n%s", want, got)
		}
	}
}

func TestFormatRetryCompletionClarifiesFailedVerdict(t *testing.T) {
	outcome := retryOutcome{
		Selection: retrySelection{
			SubTask: UnifiedSubTask{ID: "task:s6"},
			SubTaskReview: UnifiedReviewSubTaskResult{
				Score: 82,
			},
		},
		Result: appengine.Result{
			Text: "runtime smoke found HTTP 501",
		},
		Review: appengine.ReviewResult{
			Verdict:      appengine.VerdictFail,
			OverallScore: 88,
			SubTaskResults: []appengine.ReviewSubTaskResult{{
				SubTaskID: "task:s6",
				Score:     88,
				Feedback:  "server-streaming path still returns 501",
				IssueTags: []appengine.ReviewIssueTag{"runtime_streaming_failure"},
			}},
		},
		Duration: 5*time.Minute + 6*time.Second,
	}

	got := formatRetryCompletion(outcome)
	for _, want := range []string{
		"⚠️ Retry 執行完成，但驗證仍未通過",
		"原分數: 82 → 88 (+6)",
		"驗證: fail",
		"判讀：Retry job 已結束，但 reviewer 仍判定未通過；這不代表 issue 已完成。",
		"下一步：依 Review 回饋和 issue tags 修正實作或補齊成功路徑驗證，再重新 retry。",
		"Issue tags: runtime_streaming_failure",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatRetryCompletion missing %q:\n%s", want, got)
		}
	}
}
