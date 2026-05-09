package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"claude-tg-agent/internal/app/hermes"
)

func TestBuildReviewPromptIncludesRequiredSections(t *testing.T) {
	prompt := BuildReviewPrompt(ReviewRequest{
		TaskID:      "task-123",
		ProjectDir:  "/repo",
		Goal:        "修正登入流程並補測試",
		Accumulated: "目前已完成 cookie 修正",
		Plan: []hermes.SubTask{
			{
				ID:          "s1",
				Description: "修正 API 驗證",
				Status:      hermes.SubTaskDone,
				ToolHints:   []string{"Read", "Edit"},
			},
		},
		SubTaskResults: []ReviewSubTaskInput{
			{
				ID:          "s1",
				Index:       0,
				Description: "修正 API 驗證",
				Status:      "done",
				Result:      "已補上 cookie 驗證與測試",
				ToolHints:   []string{"Read", "Edit"},
			},
		},
		Artifacts: []Artifact{
			{Path: "internal/app/auth.go", Hash: "abc123", SubTaskID: "s1"},
		},
	})

	checks := []string{
		`"verdict":"pass|partial|fail"`,
		`ambiguous_goal`,
		`missing_context`,
		`"task_id": "task-123"`,
		`"project_dir": "/repo"`,
		`"goal": "修正登入流程並補測試"`,
		`"accumulated": "目前已完成 cookie 修正"`,
		`"sub_task_id":"..."`,
		`"path": "internal/app/auth.go"`,
		`return JSON only`,
	}
	for _, want := range checks {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q\n%s", want, prompt)
		}
	}
}

func TestBuildReviewPromptIncludesSubTaskScopeInstruction(t *testing.T) {
	prompt := BuildReviewPrompt(ReviewRequest{
		ReviewScope: "subtask",
		Goal:        "完成 item-117 到 item-128",
		Plan: []hermes.SubTask{
			{ID: "s0", Description: "驗證 item-117 與 item-118", Status: hermes.SubTaskDone},
		},
		SubTaskResults: []ReviewSubTaskInput{
			{ID: "s0", Description: "驗證 item-117 與 item-118", Status: "done", Result: "item-117/item-118 PASS"},
		},
	})

	checks := []string{
		`"review_scope": "subtask"`,
		`evaluate only the provided sub-task`,
		`do not fail or block because later plan items or unrelated issue checklist items remain pending`,
	}
	for _, want := range checks {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q\n%s", want, prompt)
		}
	}
}

func TestReviewInputsFromPlanCopiesPlanState(t *testing.T) {
	plan := []hermes.SubTask{
		{
			ID:          "s1",
			Description: "read context",
			Status:      hermes.SubTaskDone,
			Result:      "ok\n",
			ToolHints:   []string{"Read"},
		},
	}

	got := ReviewInputsFromPlan(plan)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].ID != "s1" || got[0].Index != 0 || got[0].Status != "done" || got[0].Result != "ok" {
		t.Fatalf("unexpected normalized input: %+v", got[0])
	}

	plan[0].ToolHints[0] = "Edit"
	if got[0].ToolHints[0] != "Read" {
		t.Fatalf("tool hints should be copied, got %+v", got[0].ToolHints)
	}
}

func TestReviewResultValidate(t *testing.T) {
	valid := ReviewResult{
		Verdict:      VerdictPartial,
		OverallScore: 78,
		SubTaskResults: []ReviewSubTaskResult{
			{SubTaskID: "s1", Score: 80},
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate(valid): %v", err)
	}

	invalid := ReviewResult{
		Verdict:      "maybe",
		OverallScore: 101,
	}
	if err := invalid.Validate(); err == nil {
		t.Fatal("Validate(invalid) expected error")
	}
}

func TestParseReviewResult(t *testing.T) {
	got, err := ParseReviewResult("```json\n{\"verdict\":\"pass\",\"overall_score\":90,\"feedback\":\"ok\",\"issue_tags\":[],\"sub_task_results\":[]}\n```")
	if err != nil {
		t.Fatalf("ParseReviewResult: %v", err)
	}
	if got.Verdict != VerdictPass || got.OverallScore != 90 {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestBuildReviewNotificationAndTelegramText(t *testing.T) {
	review := ReviewResult{
		ReviewerModel: "gpt-5.5",
		Verdict:       VerdictPartial,
		OverallScore:  73,
		IssueTags:     []ReviewTag{ReviewTagMissingContext, ReviewTagMissingValidation},
		SubTaskResults: []ReviewSubTaskResult{
			{SubTaskID: "s1", Score: 81, Feedback: "ok"},
			{SubTaskID: "s2", Score: 45, Feedback: "needs more checks", IssueTags: []ReviewIssueTag{ReviewIssueTagScopeCreep}},
			{SubTaskID: "s3", Score: 65, Feedback: "missing runtime validation"},
		},
	}

	notification := BuildReviewNotification(" 1234567890abcdef ", review)
	if notification.TaskID != "1234567890abcdef" {
		t.Fatalf("task id = %q, want trimmed value", notification.TaskID)
	}
	if !notification.AdvisoryRetry || notification.FailingSubTasks != 2 {
		t.Fatalf("notification retry state = %+v", notification)
	}
	if notification.RetryNote != "建議人工評估後重跑 2 個失敗/低分子任務" {
		t.Fatalf("retry note = %q", notification.RetryNote)
	}

	text := notification.TelegramText()
	for _, want := range []string{
		"複審完成",
		"任務：12345678",
		"partial",
		"missing_context, missing_validation",
		"❌ #2 s2 (45/100) — scope_creep",
		"⚠️ #3 s3 (65/100)",
		"✅ #1 s1 (81/100)",
		"建議人工評估後重跑 2 個失敗/低分子任務",
		"/retry 12345678 2",
		"/retry 12345678 all-failed",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("telegram text missing %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "#2 s2") > strings.Index(text, "#3 s3") || strings.Index(text, "#3 s3") > strings.Index(text, "#1 s1") {
		t.Fatalf("subtask scores were not sorted low-first:\n%s", text)
	}
}

func TestStrictReviewDefaultsAndTagBlocking(t *testing.T) {
	cfg := DefaultStrictModeConfig()
	if cfg.MaxRetriesPerSub != 2 || cfg.ReviewTimeout != 120*time.Second || cfg.OpponentBackend != BackendAuto {
		t.Fatalf("unexpected strict defaults: %+v", cfg)
	}
	if len(cfg.BlockTags) != len(DefaultStrictBlockTags) {
		t.Fatalf("block tags = %+v, want defaults", cfg.BlockTags)
	}

	review := ReviewResult{
		Verdict: VerdictPass,
		IssueTags: []ReviewIssueTag{
			ReviewIssueTagMissingValidation,
			ReviewIssueTagMissingContext,
		},
		SubTaskResults: []ReviewSubTaskResult{{
			SubTaskID: "s1",
			IssueTags: []ReviewIssueTag{ReviewIssueTagScopeCreep},
		}},
	}

	decision := ReviewDecisionFromStrictTags(review, StrictModeConfig{Enabled: true}.WithDefaults())
	if decision.Verdict != VerdictBlock || len(decision.BlockTags) != 2 || !decision.Retryable {
		t.Fatalf("unexpected strict decision: %+v", decision)
	}
	if got := ReviewTags(review); len(got) != 3 {
		t.Fatalf("review tags = %+v, want 3 unique tags", got)
	}
}

func TestReviewDecisionFromStrictTags_HighScorePassOverridesTags(t *testing.T) {
	// Reviewer holistically signs off (verdict=pass, overall_score=85)
	// but adds incomplete_traceability as a procedural caveat. Without
	// the override rule this would still block on tags alone — that
	// failure mode trapped verification-only sub-tasks in retry loops.
	review := ReviewResult{
		Verdict:      VerdictPass,
		OverallScore: 85,
		Feedback:     "Subtask appears satisfied; no diff because work was pre-existing.",
		IssueTags: []ReviewIssueTag{
			ReviewIssueTagIncompleteTraceability,
		},
	}
	decision := ReviewDecisionFromStrictTags(review, StrictModeConfig{Enabled: true}.WithDefaults())
	if decision.Verdict != VerdictAllow {
		t.Errorf("verdict = %q, want allow (high-score pass should override tags)", decision.Verdict)
	}
	if decision.Retryable {
		t.Errorf("Retryable should be false on allow")
	}
	// Tags still recorded for telemetry even though they did not trigger block.
	if len(decision.BlockTags) != 1 {
		t.Errorf("BlockTags should still record matches for telemetry: %+v", decision.BlockTags)
	}
}

func TestReviewDecisionFromStrictTags_LowScorePassStillBlocksOnTags(t *testing.T) {
	// verdict=pass with low score is the LLM hedging — tags should
	// still block because the reviewer is not actually signing off.
	review := ReviewResult{
		Verdict:      VerdictPass,
		OverallScore: 65, // below SubTaskScoreFailingThreshold (80)
		IssueTags:    []ReviewIssueTag{ReviewIssueTagMissingValidation},
	}
	decision := ReviewDecisionFromStrictTags(review, StrictModeConfig{Enabled: true}.WithDefaults())
	if decision.Verdict != VerdictBlock {
		t.Errorf("verdict = %q, want block (low-score pass should not override tags)", decision.Verdict)
	}
}

func TestReviewDecisionFromStrictTags_PartialVerdictStillBlocksOnTags(t *testing.T) {
	// verdict=partial regardless of score should still hard-block on
	// matching tags — only verdict=pass + high score gets the override.
	review := ReviewResult{
		Verdict:      VerdictPartial,
		OverallScore: 90,
		IssueTags:    []ReviewIssueTag{ReviewIssueTagScopeCreep},
	}
	decision := ReviewDecisionFromStrictTags(review, StrictModeConfig{Enabled: true}.WithDefaults())
	if decision.Verdict != VerdictBlock {
		t.Errorf("verdict = %q, want block (only verdict=pass qualifies for override)", decision.Verdict)
	}
}

func TestResolveStrictReviewBackendFallsBackToExecutor(t *testing.T) {
	cfg := StrictModeConfig{Enabled: true}.WithDefaults()
	if got := ResolveStrictReviewBackend(BackendClaude, cfg, BackendClaude); got != BackendClaude {
		t.Fatalf("backend fallback = %v, want claude", got)
	}
	if got := ResolveStrictReviewBackend(BackendClaude, cfg, BackendCodex); got != BackendCodex {
		t.Fatalf("backend selection = %v, want codex", got)
	}
	if got := ResolveStrictReviewBackend(BackendCodex, cfg, BackendClaude); got != BackendClaude {
		t.Fatalf("backend selection = %v, want claude", got)
	}
}

func TestReviewPhaseWithTimeoutDowngradesToPass(t *testing.T) {
	phase := blockingReviewPhase{}
	review, err := ReviewPhaseWithTimeout(context.Background(), phase, ReviewRequest{}, 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if review.Verdict != VerdictPass {
		t.Fatalf("timeout verdict = %s, want pass", review.Verdict)
	}
}

type blockingReviewPhase struct{}

func (blockingReviewPhase) Review(ctx context.Context, req ReviewRequest) (ReviewResult, error) {
	<-ctx.Done()
	return ReviewResult{}, ctx.Err()
}
