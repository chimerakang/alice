package app

import (
	"context"
	"strings"
	"testing"
	"time"

	appengine "claude-tg-agent/internal/app/engine"
)

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

func TestComposeRetryPromptIncludesReviewContext(t *testing.T) {
	prompt := composeRetryPrompt("修正 webhook", "partial", 64, "缺少測試", []string{"missing_validation"})

	for _, want := range []string{
		"[Retry — 上一輪 review 給 partial 64/100]",
		"原 sub-task 描述：\n修正 webhook",
		"Reviewer 找出的問題：\n缺少測試",
		"- missing_validation",
		"不要做超出範圍的改動",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
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
