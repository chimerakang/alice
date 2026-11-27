package app

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		FROM tasks WHERE id LIKE ?`, "decision:sess-unified:%").
		Scan(&task.id, &task.chatID, &task.threadID, &task.goal, &task.engine,
			&task.backend, &task.status, &task.inputTokens, &task.outputTokens)
	if err != nil {
		t.Fatalf("query task: %v", err)
	}
	if !strings.HasPrefix(task.id, "decision:sess-unified:") {
		t.Fatalf("task id should embed timestamp: %s", task.id)
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
	if !strings.HasPrefix(graphs[0].ID, "decision:sess-unified:") || len(graphs[0].SubTasks) != 1 {
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
		FROM sub_tasks WHERE id = ?`, task.id+":1").
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

func TestUnifiedTaskQueryHasReviewFiltersTasks(t *testing.T) {
	s := newTestSQLiteStorage(t)
	now := time.Now().UTC().Truncate(time.Second)

	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:        "task-with-review",
		Goal:      "reviewed task",
		Engine:    "plan_execute",
		Backend:   "claude",
		Status:    "done",
		StartedAt: now,
	}); err != nil {
		t.Fatalf("UpsertUnifiedTask(reviewed): %v", err)
	}
	if err := s.UpsertUnifiedSubTask(UnifiedSubTask{
		ID:          "task-with-review:s1",
		TaskID:      "task-with-review",
		Idx:         0,
		Description: "review step",
		Status:      "done",
		StartedAt:   now,
	}); err != nil {
		t.Fatalf("UpsertUnifiedSubTask(reviewed): %v", err)
	}
	if _, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:        "task-with-review",
		ReviewerModel: "gpt-5.5",
		Verdict:       "pass",
		OverallScore:  95,
		FeedbackText:  "looks good",
		IssueTags:     []string{"clear_goal"},
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("InsertUnifiedReviewResult(reviewed): %v", err)
	}

	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:        "task-without-review",
		Goal:      "plain task",
		Engine:    "plan_execute",
		Backend:   "claude",
		Status:    "done",
		StartedAt: now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("UpsertUnifiedTask(unreviewed): %v", err)
	}

	onlyReviewed, err := s.ListUnifiedTaskGraphs(UnifiedTaskQuery{Limit: 10, HasReview: boolPtr(true)})
	if err != nil {
		t.Fatalf("ListUnifiedTaskGraphs(has review): %v", err)
	}
	if len(onlyReviewed) != 1 || onlyReviewed[0].ID != "task-with-review" {
		t.Fatalf("unexpected reviewed tasks: %+v", onlyReviewed)
	}
	reviewCount, err := s.CountUnifiedTasks(UnifiedTaskQuery{HasReview: boolPtr(true)})
	if err != nil {
		t.Fatalf("CountUnifiedTasks(has review): %v", err)
	}
	if reviewCount != 1 {
		t.Fatalf("CountUnifiedTasks(has review): got %d, want 1", reviewCount)
	}

	onlyUnreviewed, err := s.ListUnifiedTaskGraphs(UnifiedTaskQuery{Limit: 10, HasReview: boolPtr(false)})
	if err != nil {
		t.Fatalf("ListUnifiedTaskGraphs(no review): %v", err)
	}
	if len(onlyUnreviewed) != 1 || onlyUnreviewed[0].ID != "task-without-review" {
		t.Fatalf("unexpected unreviewed tasks: %+v", onlyUnreviewed)
	}
	unreviewedCount, err := s.CountUnifiedTasks(UnifiedTaskQuery{HasReview: boolPtr(false)})
	if err != nil {
		t.Fatalf("CountUnifiedTasks(no review): %v", err)
	}
	if unreviewedCount != 1 {
		t.Fatalf("CountUnifiedTasks(no review): got %d, want 1", unreviewedCount)
	}
}

func TestParseUnifiedTaskQueryHasReview(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/tasks?has_review=true", nil)
	query, err := parseUnifiedTaskQuery(req)
	if err != nil {
		t.Fatalf("parseUnifiedTaskQuery(true): %v", err)
	}
	if query.HasReview == nil || !*query.HasReview {
		t.Fatalf("expected has_review=true, got %+v", query.HasReview)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/tasks?has_review=false", nil)
	query, err = parseUnifiedTaskQuery(req)
	if err != nil {
		t.Fatalf("parseUnifiedTaskQuery(false): %v", err)
	}
	if query.HasReview == nil || *query.HasReview {
		t.Fatalf("expected has_review=false, got %+v", query.HasReview)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/tasks?has_review=maybe", nil)
	if _, err := parseUnifiedTaskQuery(req); err == nil {
		t.Fatal("parseUnifiedTaskQuery(invalid): expected error")
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func TestQualityDecompositionStatsAggregateTaskShape(t *testing.T) {
	s := newTestSQLiteStorage(t)
	now := time.Now().UTC().Truncate(time.Second)
	window := QualityWindow{Start: now.Add(-30 * 24 * time.Hour), End: now.Add(time.Hour), Label: "30d"}

	seedQualityTask(t, s, "quality-a", now.Add(-48*time.Hour), []qualitySeedSubTask{
		{ID: "quality-a:s1", Description: "tiny", Model: "", Status: "done", Tools: []string{"Edit", "Bash"}, Score: 60, Tags: []string{"missing_validation"}},
		{ID: "quality-a:s2", Description: strings.Repeat("validated step ", 8), Model: "sonnet", Status: "done", Tools: []string{"Bash"}, Score: 88},
	}, "partial", 74, []string{"missing_validation"})
	seedQualityTask(t, s, "quality-b", now.Add(-24*time.Hour), []qualitySeedSubTask{
		{ID: "quality-b:s1", Description: strings.Repeat("well scoped implementation ", 4), Model: "sonnet", Status: "done", Tools: []string{"Edit", "Bash"}, Score: 95},
		{ID: "quality-b:s2", Description: strings.Repeat("test coverage and validation ", 4), Model: "sonnet", Status: "done", Tools: []string{"Bash"}, Score: 92},
		{ID: "quality-b:s3", Description: strings.Repeat("documentation update ", 4), Model: "sonnet", Status: "done", Tools: []string{"Edit"}, Score: 90},
		{ID: "quality-b:s4", Description: strings.Repeat("final verification ", 4), Model: "sonnet", Status: "done", Tools: []string{"Bash"}, Score: 91},
	}, "pass", 92, []string{"test_gap"})

	stats, err := s.GetQualityDecompositionStats(window)
	if err != nil {
		t.Fatalf("GetQualityDecompositionStats: %v", err)
	}
	if stats.TaskCount != 2 || stats.AvgSubTasks != 3 {
		t.Fatalf("unexpected task shape: count=%d avg=%.1f", stats.TaskCount, stats.AvgSubTasks)
	}
	if stats.GranularityBuckets[0].Count != 1 || stats.GranularityBuckets[1].Count != 1 {
		t.Fatalf("unexpected distribution: %+v", stats.GranularityBuckets)
	}
	if stats.DescriptionBuckets[0].AvgScore != 60 {
		t.Fatalf("short description failure rate: %+v", stats.DescriptionBuckets)
	}
	if len(stats.ToolHintStats) == 0 || stats.ToolHintStats[0].ToolHints == "" {
		t.Fatalf("missing tool hint stats: %+v", stats.ToolHintStats)
	}
}

func TestQualityScoreStatsAndInsights(t *testing.T) {
	s := newTestSQLiteStorage(t)
	now := time.Now().UTC().Truncate(time.Second)
	window := QualityWindow{Start: now.Add(-14 * 24 * time.Hour), End: now.Add(time.Hour), Label: "14d"}

	seedQualityTask(t, s, "commit-deploy-change", now.Add(-72*time.Hour), []qualitySeedSubTask{
		{ID: "commit-deploy-change:s1", Description: "short", Model: "", Status: "done", Tools: []string{"Edit", "Bash"}, Score: 55, Tags: []string{"missing_validation"}},
	}, "partial", 60, []string{"missing_validation"})
	seedQualityTask(t, s, "push-release", now.Add(-48*time.Hour), []qualitySeedSubTask{
		{ID: "push-release:s1", Description: "short", Model: "", Status: "done", Tools: []string{"Bash"}, Score: 62, Tags: []string{"scope_creep"}},
	}, "partial", 65, []string{"scope_creep"})
	seedQualityTask(t, s, "quality-pass", now.Add(-24*time.Hour), []qualitySeedSubTask{
		{ID: "quality-pass:s1", Description: strings.Repeat("clear validated task ", 5), Model: "sonnet", Status: "done", Tools: []string{"Edit", "Bash"}, Score: 95},
	}, "pass", 95, nil)

	stats, err := s.GetQualityScoreStats(window)
	if err != nil {
		t.Fatalf("GetQualityScoreStats: %v", err)
	}
	if stats.ReviewCount != 3 || stats.VerdictDistribution["partial"] != 2 || int(stats.PassRate) != 33 {
		t.Fatalf("unexpected score stats: %+v", stats)
	}
	if len(stats.TopIssueTags) == 0 || stats.TopIssueTags[0].Count == 0 {
		t.Fatalf("missing issue tag stats: %+v", stats.TopIssueTags)
	}

	insights, err := s.GetQualityInsights(window)
	if err != nil {
		t.Fatalf("GetQualityInsights: %v", err)
	}
	if !hasQualityInsight(insights, "risk_verbs_partial_rate") {
		t.Fatalf("expected risk verb insight, got %+v", insights)
	}
	if !hasQualityInsight(insights, "missing_subtask_model") {
		t.Fatalf("expected missing model insight, got %+v", insights)
	}
}

type qualitySeedSubTask struct {
	ID          string
	Description string
	Model       string
	Status      string
	Tools       []string
	Score       int
	Tags        []string
}

func seedQualityTask(t *testing.T, s *SQLiteStorage, id string, startedAt time.Time, subTasks []qualitySeedSubTask, verdict string, score int, tags []string) {
	t.Helper()
	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:        id,
		Goal:      id,
		Engine:    "plan-execute",
		Backend:   "codex",
		Status:    "done",
		StartedAt: startedAt,
	}); err != nil {
		t.Fatalf("UpsertUnifiedTask(%s): %v", id, err)
	}
	for idx, subTask := range subTasks {
		if err := s.UpsertUnifiedSubTask(UnifiedSubTask{
			ID:          subTask.ID,
			TaskID:      id,
			Idx:         idx,
			Description: subTask.Description,
			Model:       subTask.Model,
			Status:      subTask.Status,
			StartedAt:   startedAt.Add(time.Duration(idx) * time.Minute),
		}); err != nil {
			t.Fatalf("UpsertUnifiedSubTask(%s): %v", subTask.ID, err)
		}
		for toolIdx, tool := range subTask.Tools {
			if err := s.InsertUnifiedToolEvent(UnifiedToolEvent{
				SubTaskID: subTask.ID,
				ToolName:  tool,
				Timestamp: startedAt.Add(time.Duration(toolIdx) * time.Second),
				Status:    "done",
			}); err != nil {
				t.Fatalf("InsertUnifiedToolEvent(%s): %v", subTask.ID, err)
			}
		}
	}
	reviewID, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:       id,
		Verdict:      verdict,
		OverallScore: score,
		IssueTags:    tags,
		CreatedAt:    startedAt.Add(30 * time.Minute),
	})
	if err != nil {
		t.Fatalf("InsertUnifiedReviewResult(%s): %v", id, err)
	}
	for _, subTask := range subTasks {
		if err := s.InsertUnifiedReviewSubTaskResult(UnifiedReviewSubTaskResult{
			ReviewID:  reviewID,
			SubTaskID: subTask.ID,
			Score:     subTask.Score,
			IssueTags: subTask.Tags,
		}); err != nil {
			t.Fatalf("InsertUnifiedReviewSubTaskResult(%s): %v", subTask.ID, err)
		}
	}
}

func hasQualityInsight(insights []QualityInsight, name string) bool {
	for _, insight := range insights {
		if insight.Name == name {
			return true
		}
	}
	return false
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

func newTestI18nManager(t *testing.T) *I18nManager {
	t.Helper()

	i18n, err := NewI18nManager(filepath.Join("..", "..", "locales"), "en")
	if err != nil {
		t.Fatalf("NewI18nManager: %v", err)
	}
	return i18n
}
