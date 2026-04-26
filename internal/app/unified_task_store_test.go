package app

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	appengine "claude-tg-agent/internal/app/engine"
)

func TestInsertDecisionLogWritesUnifiedTaskGraph(t *testing.T) {
	s := newTestSQLiteStorage(t)
	now := time.Now().UTC().Truncate(time.Second)

	err := s.InsertDecisionLog(DecisionLog{
		Timestamp:     now,
		SessionID:     "sess-unified",
		ProjectPath:   "/repo",
		ChatID:        42,
		ThreadID:      7,
		UserPrompt:    "fix the failing test",
		AgentResponse: "done",
		ToolCalls: []ToolExecution{{
			Timestamp: now.Add(500 * time.Millisecond),
			ToolName:  "bash",
			Input:     map[string]interface{}{"cmd": "go test ./..."},
			Status:    "success",
		}},
		Outcome: ExecutionOutcome{
			Success:      true,
			FilesChanged: []string{"internal/app/foo.go"},
		},
		DurationMs: 2500,
		TokensUsed: TokenStats{
			TotalInputTokens:  11,
			TotalOutputTokens: 13,
			TotalCostUSD:      0.02,
		},
		Model:          "gpt-5.3-codex",
		RoutingReason:  "test",
		RoutingLatency: 9,
	})
	if err != nil {
		t.Fatalf("InsertDecisionLog: %v", err)
	}

	var task struct {
		id, goal, engine, backend, status string
		chatID                            int64
		threadID                          int
		inputTokens, outputTokens         int
	}
	err = s.db.QueryRow(`
		SELECT id, chat_id, thread_id, goal, engine, backend, status,
		       total_input_tokens, total_output_tokens
		FROM tasks WHERE id = ?`, "decision:sess-unified").
		Scan(&task.id, &task.chatID, &task.threadID, &task.goal, &task.engine,
			&task.backend, &task.status, &task.inputTokens, &task.outputTokens)
	if err != nil {
		t.Fatalf("query task: %v", err)
	}
	if task.engine != "direct" || task.backend != "codex" || task.status != "done" {
		t.Fatalf("unexpected task fields: %+v", task)
	}
	if task.chatID != 42 || task.threadID != 7 || task.goal != "fix the failing test" {
		t.Fatalf("unexpected task identity: %+v", task)
	}
	if task.inputTokens != 11 || task.outputTokens != 13 {
		t.Fatalf("unexpected token totals: %+v", task)
	}

	graphs, err := s.ListUnifiedTaskGraphs(UnifiedTaskQuery{Limit: 10})
	if err != nil {
		t.Fatalf("ListUnifiedTaskGraphs: %v", err)
	}
	if len(graphs) != 1 {
		t.Fatalf("graph count: got %d, want 1", len(graphs))
	}
	if graphs[0].ID != "decision:sess-unified" || len(graphs[0].SubTasks) != 1 {
		t.Fatalf("unexpected graph shape: %+v", graphs[0])
	}
	if len(graphs[0].SubTasks[0].ToolEvents) != 1 || len(graphs[0].SubTasks[0].Artifacts) != 1 {
		t.Fatalf("unexpected graph children: %+v", graphs[0].SubTasks[0])
	}

	count, err := s.CountUnifiedTasks(UnifiedTaskQuery{ProjectDir: "/repo"})
	if err != nil {
		t.Fatalf("CountUnifiedTasks: %v", err)
	}
	if count != 1 {
		t.Fatalf("CountUnifiedTasks: got %d, want 1", count)
	}

	var subModel, subResult, routingReason string
	var routingLatency int
	err = s.db.QueryRow(`
		SELECT model, result_text, routing_reason, routing_latency_ms
		FROM sub_tasks WHERE id = ?`, "decision:sess-unified:1").
		Scan(&subModel, &subResult, &routingReason, &routingLatency)
	if err != nil {
		t.Fatalf("query sub_task: %v", err)
	}
	if subModel != "gpt-5.3-codex" || subResult != "done" || routingReason != "test" || routingLatency != 9 {
		t.Fatalf("unexpected subtask mirror: model=%q result=%q reason=%q latency=%d", subModel, subResult, routingReason, routingLatency)
	}

	assertCount(t, s.db, "tool_events", 1)
	assertCount(t, s.db, "artifacts", 1)

	if err := s.DeleteDecisionLogsBySessionID("sess-unified"); err != nil {
		t.Fatalf("DeleteDecisionLogsBySessionID: %v", err)
	}
	assertCount(t, s.db, "tasks", 0)
	assertCount(t, s.db, "sub_tasks", 0)
	assertCount(t, s.db, "tool_events", 0)
	assertCount(t, s.db, "artifacts", 0)
}

func TestUnifiedReviewResultsIncludeSubTaskReviews(t *testing.T) {
	s := newTestSQLiteStorage(t)
	now := time.Now().UTC().Truncate(time.Second)

	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:        "task-review",
		Goal:      "review me",
		Engine:    "plan-execute",
		Backend:   "codex",
		Status:    "done",
		StartedAt: now,
	}); err != nil {
		t.Fatalf("UpsertUnifiedTask: %v", err)
	}
	if err := s.UpsertUnifiedSubTask(UnifiedSubTask{
		ID:          "task-review:s1",
		TaskID:      "task-review",
		Idx:         0,
		Description: "first",
		Status:      "done",
		StartedAt:   now,
	}); err != nil {
		t.Fatalf("UpsertUnifiedSubTask: %v", err)
	}
	reviewID, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:        "task-review",
		ReviewerModel: "gpt-5.5",
		Verdict:       "partial",
		OverallScore:  72,
		FeedbackText:  "needs more checks",
		IssueTags:     []string{"missing_context"},
		CreatedAt:     now,
	})
	if err != nil {
		t.Fatalf("InsertUnifiedReviewResult: %v", err)
	}
	if err := s.InsertUnifiedReviewSubTaskResult(UnifiedReviewSubTaskResult{
		ReviewID:  reviewID,
		SubTaskID: "task-review:s1",
		Score:     70,
		Feedback:  "subtask feedback",
		IssueTags: []string{"ambiguous_goal"},
	}); err != nil {
		t.Fatalf("InsertUnifiedReviewSubTaskResult: %v", err)
	}

	graphs, err := s.ListUnifiedTaskGraphs(UnifiedTaskQuery{ID: "task-review", Limit: 1})
	if err != nil {
		t.Fatalf("ListUnifiedTaskGraphs: %v", err)
	}
	if len(graphs) != 1 || len(graphs[0].Reviews) != 1 {
		t.Fatalf("unexpected review graph: %+v", graphs)
	}
	review := graphs[0].Reviews[0]
	if review.ID != reviewID || review.IssueTags[0] != "missing_context" {
		t.Fatalf("unexpected task review: %+v", review)
	}
	if len(review.SubTaskResults) != 1 || review.SubTaskResults[0].IssueTags[0] != "ambiguous_goal" {
		t.Fatalf("unexpected subtask review: %+v", review.SubTaskResults)
	}
}

func TestStoreReviewPersistsUnifiedReviewGraph(t *testing.T) {
	s := newTestSQLiteStorage(t)
	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:         "task-review-store",
		ChatID:     42,
		ThreadID:   1,
		ProjectDir: "/repo",
		Goal:       "review me",
		Engine:     "plan_execute",
		Backend:    "claude",
		Status:     "done",
		StartedAt:  time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("UpsertUnifiedTask: %v", err)
	}
	if err := s.UpsertUnifiedSubTask(UnifiedSubTask{
		ID:          "task-review-store:s1",
		TaskID:      "task-review-store",
		Idx:         0,
		Description: "review step",
		Model:       "claude",
		Status:      "done",
		StartedAt:   time.Now().UTC().Add(-30 * time.Second),
	}); err != nil {
		t.Fatalf("UpsertUnifiedSubTask: %v", err)
	}
	if err := s.StoreReview(context.Background(), "task-review-store", appengine.ReviewResult{
		ReviewerModel: "gpt-5.5",
		Verdict:       appengine.VerdictPartial,
		OverallScore:  73,
		Feedback:      "needs follow-up",
		IssueTags:     []appengine.ReviewTag{appengine.ReviewTagMissingContext},
		SubTaskResults: []appengine.ReviewSubTaskResult{
			{
				SubTaskID: "task-review-store:s1",
				Score:     72,
				Feedback:  "more context needed",
				IssueTags: []appengine.ReviewTag{appengine.ReviewTagMissingValidation},
			},
		},
		InputTokens:  9,
		OutputTokens: 4,
		CostUSD:      0.11,
	}); err != nil {
		t.Fatalf("StoreReview: %v", err)
	}

	graphs, err := s.ListUnifiedTaskGraphs(UnifiedTaskQuery{ID: "task-review-store", Limit: 1})
	if err != nil {
		t.Fatalf("ListUnifiedTaskGraphs: %v", err)
	}
	if len(graphs) != 1 || len(graphs[0].Reviews) != 1 {
		t.Fatalf("unexpected review graph: %+v", graphs)
	}
	review := graphs[0].Reviews[0]
	if review.ReviewerModel != "gpt-5.5" || review.InputTokens != 9 || review.OutputTokens != 4 {
		t.Fatalf("unexpected persisted review: %+v", review)
	}
	if len(review.SubTaskResults) != 1 || review.SubTaskResults[0].IssueTags[0] != "missing_validation" {
		t.Fatalf("unexpected persisted review subtask results: %+v", review.SubTaskResults)
	}
}

func TestStoreReviewRollsBackOnSubTaskInsertFailure(t *testing.T) {
	s := newTestSQLiteStorage(t)
	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:        "task-review-rollback",
		Goal:      "review me",
		Engine:    "plan_execute",
		Backend:   "claude",
		Status:    "done",
		StartedAt: time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("UpsertUnifiedTask: %v", err)
	}
	if err := s.StoreReview(context.Background(), "task-review-rollback", appengine.ReviewResult{
		ReviewerModel: "gpt-5.5",
		Verdict:       appengine.VerdictFail,
		OverallScore:  40,
		Feedback:      "missing subtask row",
		IssueTags:     []appengine.ReviewTag{appengine.ReviewTagMissingContext},
		SubTaskResults: []appengine.ReviewSubTaskResult{
			{
				SubTaskID: "task-review-rollback:missing",
				Score:     10,
				Feedback:  "cannot persist",
				IssueTags: []appengine.ReviewTag{appengine.ReviewTagUnderspecifiedInput},
			},
		},
	}); err == nil {
		t.Fatal("StoreReview: expected foreign key failure")
	}

	assertCount(t, s.db, "review_results", 0)
	assertCount(t, s.db, "review_subtask_results", 0)
}

func TestGetPlannerRulesWeeklyReportAggregatesTopIssueTags(t *testing.T) {
	s := newTestSQLiteStorage(t)
	now := time.Now().UTC().Truncate(time.Second)

	for _, taskID := range []string{"task-weekly-1", "task-weekly-2"} {
		if err := s.UpsertUnifiedTask(UnifiedTask{
			ID:        taskID,
			Goal:      "weekly review",
			Engine:    "plan_execute",
			Backend:   "codex",
			Status:    "done",
			StartedAt: now.Add(-2 * time.Hour),
		}); err != nil {
			t.Fatalf("UpsertUnifiedTask(%s): %v", taskID, err)
		}
		if err := s.UpsertUnifiedSubTask(UnifiedSubTask{
			ID:          taskID + ":s1",
			TaskID:      taskID,
			Idx:         0,
			Description: "run task",
			Status:      "done",
			StartedAt:   now.Add(-90 * time.Minute),
		}); err != nil {
			t.Fatalf("UpsertUnifiedSubTask(%s): %v", taskID, err)
		}
	}

	reviewID1, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:        "task-weekly-1",
		ReviewerModel: "gpt-5.5",
		Verdict:       "partial",
		OverallScore:  70,
		FeedbackText:  "needs more context",
		IssueTags:     []string{"missing_context", "missing_validation"},
		CreatedAt:     now.Add(-6 * time.Hour),
	})
	if err != nil {
		t.Fatalf("InsertUnifiedReviewResult(1): %v", err)
	}
	if err := s.InsertUnifiedReviewSubTaskResult(UnifiedReviewSubTaskResult{
		ReviewID:  reviewID1,
		SubTaskID: "task-weekly-1:s1",
		Score:     68,
		Feedback:  "validate output",
		IssueTags: []string{"missing_validation"},
	}); err != nil {
		t.Fatalf("InsertUnifiedReviewSubTaskResult(1): %v", err)
	}

	reviewID2, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:        "task-weekly-2",
		ReviewerModel: "gpt-5.5",
		Verdict:       "fail",
		OverallScore:  55,
		FeedbackText:  "missing context and vague goal",
		IssueTags:     []string{"missing_context", "ambiguous_goal"},
		CreatedAt:     now.Add(-3 * time.Hour),
	})
	if err != nil {
		t.Fatalf("InsertUnifiedReviewResult(2): %v", err)
	}
	if err := s.InsertUnifiedReviewSubTaskResult(UnifiedReviewSubTaskResult{
		ReviewID:  reviewID2,
		SubTaskID: "task-weekly-2:s1",
		Score:     50,
		Feedback:  "insufficient context",
		IssueTags: []string{"missing_context"},
	}); err != nil {
		t.Fatalf("InsertUnifiedReviewSubTaskResult(2): %v", err)
	}

	report, err := s.GetPlannerRulesWeeklyReport(now.Add(-7*24*time.Hour), now)
	if err != nil {
		t.Fatalf("GetPlannerRulesWeeklyReport: %v", err)
	}

	if report.ReviewCount != 2 {
		t.Fatalf("ReviewCount: got %d, want 2", report.ReviewCount)
	}
	if report.ReviewedSubTaskCount != 2 {
		t.Fatalf("ReviewedSubTaskCount: got %d, want 2", report.ReviewedSubTaskCount)
	}
	if report.VerdictCounts["partial"] != 1 || report.VerdictCounts["fail"] != 1 {
		t.Fatalf("unexpected verdict counts: %+v", report.VerdictCounts)
	}
	if len(report.TopIssueTags) != 3 {
		t.Fatalf("TopIssueTags len: got %d, want 3", len(report.TopIssueTags))
	}
	if report.TopIssueTags[0].Tag != "missing_context" || report.TopIssueTags[0].Count != 3 {
		t.Fatalf("top issue tag[0]: %+v", report.TopIssueTags[0])
	}
	if report.TopIssueTags[1].Tag != "missing_validation" || report.TopIssueTags[1].Count != 2 {
		t.Fatalf("top issue tag[1]: %+v", report.TopIssueTags[1])
	}
	if got := FormatPlannerRulesWeeklyReport(report); !strings.Contains(got, "Top issue tags") || !strings.Contains(got, "missing_context") {
		t.Fatalf("formatted report missing expected content:\n%s", got)
	}
	if len(report.Recommendations) == 0 {
		t.Fatal("expected recommendations")
	}
}

func assertCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("count %s: got %d, want %d", table, got, want)
	}
}
