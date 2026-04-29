package app

import (
	"context"
	"strings"
	"testing"
	"time"

	appengine "claude-tg-agent/internal/app/engine"
)

type retryCommandTestClient struct {
	streamCalls int
	planCalls   int
}

func (c *retryCommandTestClient) Call(ctx context.Context, message, projectDir, sessionID, modelOverride string) (*CLIResponse, error) {
	return nil, nil
}

func (c *retryCommandTestClient) CallStream(ctx context.Context, message, projectDir, sessionID, modelOverride string, onToolUse func(toolName string, toolInput map[string]interface{}), onContent func(contentType, text string)) (*CLIResponse, error) {
	c.streamCalls++
	return &CLIResponse{
		Result:      "retry evidence",
		TextContent: "retry evidence",
		SessionID:   "retry-session",
		Usage: struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		}{InputTokens: 10, OutputTokens: 5},
	}, nil
}

func (c *retryCommandTestClient) CallPlan(ctx context.Context, message, projectDir, modelOverride string, onContent func(contentType, text string)) (*CLIResponse, error) {
	c.planCalls++
	reviewJSON := `{"verdict":"pass","overall_score":84,"feedback":"retry fixed validation","issue_tags":[],"sub_task_results":[{"score":84,"feedback":"ok","issue_tags":[]}]}`
	if onContent != nil {
		onContent("text", reviewJSON)
	}
	return &CLIResponse{
		Result:      reviewJSON,
		TextContent: reviewJSON,
		Usage: struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		}{InputTokens: 7, OutputTokens: 3},
	}, nil
}

func (c *retryCommandTestClient) GetModel() string {
	return "gpt-5.4"
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
	client := &retryCommandTestClient{}
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
	doneText := waitRetryMessageContaining(t, bot, "Retry 完成")
	if !strings.Contains(doneText, "原分數: 61 → 84") || !strings.Contains(doneText, "Review 回饋：\nok") {
		t.Fatalf("unexpected retry completion message: %q", doneText)
	}
	if client.streamCalls != 1 || client.planCalls != 1 {
		t.Fatalf("client calls = stream %d plan %d, want 1/1", client.streamCalls, client.planCalls)
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

func TestSelectRetryTargetByIndexReportsMissingLatestReviewRow(t *testing.T) {
	s := newTestSQLiteStorage(t)
	seedRetryReview(t, s, "task-missing-review-row", 61, "initial")
	if _, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:        "task-missing-review-row",
		ReviewerModel: "gpt-5.5",
		Verdict:       "partial",
		OverallScore:  72,
		FeedbackText:  "latest summary omitted per-subtask rows",
		Source:        "review",
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("InsertUnifiedReviewResult latest without rows: %v", err)
	}

	_, err := s.selectRetryTargetByIndex(context.Background(), "task-missing-review-row", 2)
	if err == nil {
		t.Fatal("selectRetryTargetByIndex missing review row expected error")
	}
	if !strings.Contains(err.Error(), "最新 review 沒有 sub-task #2 的評分資料") ||
		!strings.Contains(err.Error(), "/retry task-mis all-failed") {
		t.Fatalf("unexpected missing review row error: %v", err)
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
		"✅ Retry 完成",
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
