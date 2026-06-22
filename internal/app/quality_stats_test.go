package app

import (
	"strings"
	"testing"
	"time"
)

func TestQualityStatsAggregateUnifiedReviews(t *testing.T) {
	s := newTestSQLiteStorage(t)
	now := time.Now().UTC().Truncate(time.Second)

	insertQualityFixtureTask(t, s, "task-quality-1", now.Add(-2*time.Hour), "commit and push fix", []UnifiedSubTask{
		{ID: "task-quality-1:s1", Idx: 0, Description: "short"},
		{ID: "task-quality-1:s2", Idx: 1, Description: strings.Repeat("validate output ", 8), Model: "gpt-5.5"},
	}, "partial", 74, []string{"missing_validation"}, []UnifiedReviewSubTaskResult{
		{SubTaskID: "task-quality-1:s1", Score: 55, IssueTags: []string{"missing_validation"}, Feedback: "needs validation"},
		{SubTaskID: "task-quality-1:s2", Score: 82, IssueTags: []string{"scope_creep"}, Feedback: "too broad"},
	})
	insertQualityFixtureTask(t, s, "task-quality-2", now.Add(-90*time.Minute), "implement parser", []UnifiedSubTask{
		{ID: "task-quality-2:s1", Idx: 0, Description: strings.Repeat("clear bounded step ", 5), Model: "gpt-5.5"},
		{ID: "task-quality-2:s2", Idx: 1, Description: strings.Repeat("clear bounded validation ", 5), Model: "gpt-5.5"},
		{ID: "task-quality-2:s3", Idx: 2, Description: strings.Repeat("clear bounded docs ", 5), Model: "gpt-5.5"},
		{ID: "task-quality-2:s4", Idx: 3, Description: strings.Repeat("clear bounded tests ", 5), Model: "gpt-5.5"},
	}, "pass", 94, []string{"missing_validation"}, []UnifiedReviewSubTaskResult{
		{SubTaskID: "task-quality-2:s1", Score: 92},
		{SubTaskID: "task-quality-2:s2", Score: 95},
	})

	window := QualityWindow{Start: now.Add(-24 * time.Hour), End: now.Add(time.Hour)}
	decomp, err := s.GetQualityDecompositionStats(window)
	if err != nil {
		t.Fatalf("GetQualityDecompositionStats: %v", err)
	}
	if decomp.TaskCount != 2 || decomp.SubTaskCount != 6 {
		t.Fatalf("unexpected decomposition totals: %+v", decomp)
	}
	if decomp.BestGranularity == "" || len(decomp.DescriptionBuckets) == 0 || len(decomp.ToolHintStats) == 0 {
		t.Fatalf("expected populated decomposition details: %+v", decomp)
	}

	scores, err := s.GetQualityScoreStats(window)
	if err != nil {
		t.Fatalf("GetQualityScoreStats: %v", err)
	}
	if scores.ReviewCount != 2 || scores.VerdictDistribution["pass"] != 1 || scores.VerdictDistribution["partial"] != 1 {
		t.Fatalf("unexpected score distribution: %+v", scores)
	}
	if len(scores.TopIssueTags) == 0 || scores.TopIssueTags[0].Tag != "missing_validation" || scores.TopIssueTags[0].Count != 2 {
		t.Fatalf("unexpected top tags: %+v", scores.TopIssueTags)
	}
	if len(scores.LowScoringSubTasks) == 0 || scores.LowScoringSubTasks[0].TaskID != "task-quality-1" {
		t.Fatalf("unexpected low scoring subtasks: %+v", scores.LowScoringSubTasks)
	}
}

func TestQualityInsightsRules(t *testing.T) {
	s := newTestSQLiteStorage(t)
	now := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 3; i++ {
		taskID := "task-risk-" + string(rune('a'+i))
		insertQualityFixtureTask(t, s, taskID, now.Add(-time.Duration(i)*time.Hour), "deploy commit push", []UnifiedSubTask{
			{ID: taskID + ":s1", Idx: 0, Description: "tiny"},
		}, "partial", 61, []string{"missing_validation"}, []UnifiedReviewSubTaskResult{
			{SubTaskID: taskID + ":s1", Score: 45, IssueTags: []string{"missing_validation"}, Feedback: "missing checks"},
		})
	}

	insights, err := s.GetQualityInsights(QualityWindow{Start: now.Add(-24 * time.Hour), End: now.Add(time.Hour)})
	if err != nil {
		t.Fatalf("GetQualityInsights: %v", err)
	}
	names := map[string]bool{}
	for _, insight := range insights {
		names[insight.Name] = true
	}
	if !names["high_partial_rate"] || !names["risk_verbs_partial_rate"] || !names["short_description_fail_rate"] {
		t.Fatalf("expected rule insights, got: %+v", insights)
	}
}

func TestResolveQualityWindowRejectsInvalidWindow(t *testing.T) {
	if _, err := ResolveQualityWindow("14d", time.Now()); err == nil {
		t.Fatal("expected invalid window error")
	}
}

func insertQualityFixtureTask(t *testing.T, s *SQLiteStorage, taskID string, startedAt time.Time, goal string, subTasks []UnifiedSubTask, verdict string, score int, tags []string, subResults []UnifiedReviewSubTaskResult) {
	t.Helper()
	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:        taskID,
		Goal:      goal,
		Engine:    "plan-execute",
		Backend:   "codex",
		Status:    "done",
		StartedAt: startedAt,
	}); err != nil {
		t.Fatalf("UpsertUnifiedTask(%s): %v", taskID, err)
	}
	for _, subTask := range subTasks {
		subTask.TaskID = taskID
		subTask.Status = "done"
		subTask.StartedAt = startedAt.Add(time.Minute)
		if err := s.UpsertUnifiedSubTask(subTask); err != nil {
			t.Fatalf("UpsertUnifiedSubTask(%s): %v", subTask.ID, err)
		}
		if err := s.InsertUnifiedToolEvent(UnifiedToolEvent{
			SubTaskID: subTask.ID,
			ToolName:  "Bash",
			Status:    "success",
			Timestamp: startedAt.Add(2 * time.Minute),
		}); err != nil {
			t.Fatalf("InsertUnifiedToolEvent(%s): %v", subTask.ID, err)
		}
	}
	reviewID, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:        taskID,
		ReviewerModel: "gpt-5.5",
		Verdict:       verdict,
		OverallScore:  score,
		IssueTags:     tags,
		CreatedAt:     startedAt.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("InsertUnifiedReviewResult(%s): %v", taskID, err)
	}
	for _, result := range subResults {
		result.ReviewID = reviewID
		if err := s.InsertUnifiedReviewSubTaskResult(result); err != nil {
			t.Fatalf("InsertUnifiedReviewSubTaskResult(%s): %v", result.SubTaskID, err)
		}
	}
}
