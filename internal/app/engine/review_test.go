package engine

import (
	"strings"
	"testing"

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
			{SubTaskID: "s1", Score: 65, Feedback: "needs more checks"},
			{SubTaskID: "s2", Score: 81, Feedback: "ok"},
		},
	}

	notification := BuildReviewNotification(" task-123 ", review)
	if notification.TaskID != "task-123" {
		t.Fatalf("task id = %q, want trimmed value", notification.TaskID)
	}
	if !notification.AdvisoryRetry || notification.FailingSubTasks != 1 {
		t.Fatalf("notification retry state = %+v", notification)
	}
	if notification.RetryNote != "建議人工評估後重跑 1 個失敗/低分子任務" {
		t.Fatalf("retry note = %q", notification.RetryNote)
	}

	text := notification.TelegramText()
	for _, want := range []string{"複審完成", "partial", "missing_context, missing_validation", "建議人工評估後重跑 1 個失敗/低分子任務"} {
		if !strings.Contains(text, want) {
			t.Fatalf("telegram text missing %q:\n%s", want, text)
		}
	}
}
