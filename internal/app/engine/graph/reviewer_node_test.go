package graph

import (
	"context"
	"errors"
	"testing"

	"claude-tg-agent/internal/app/hermes"
)

type fakeTaskReviewer struct {
	calls  int
	result TaskReviewResult
	err    error
}

func (r *fakeTaskReviewer) ReviewTask(_ context.Context, _ hermes.HermesState) (TaskReviewResult, error) {
	r.calls++
	return r.result, r.err
}

func makeReviewerState() hermes.HermesState {
	return hermes.HermesState{
		TaskID:     "task-review",
		ChatID:     42,
		Goal:       "task review test",
		ProjectDir: "/repo",
		Status:     hermes.TaskStatusExecuting,
		Plan: []hermes.SubTask{
			{ID: "s1", Status: hermes.SubTaskDone, Result: "ok"},
			{ID: "s2", Status: hermes.SubTaskDone, Result: "also ok"},
		},
		CurrentIdx:  2,
		Accumulated: "ok\n\n## Sub-task 2\nalso ok\n",
	}
}

func TestReviewerNode_PassRoutesTerminal(t *testing.T) {
	rev := &fakeTaskReviewer{result: TaskReviewResult{
		Verdict:      "pass",
		OverallScore: 92,
		Model:        "reviewer-1",
		InputTokens:  100,
		OutputTokens: 50,
		CostUSD:      0.02,
	}}
	node := &ReviewerNode{Reviewer: rev}

	out, err := node.Handle(context.Background(), makeReviewerState())
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if rev.calls != 1 {
		t.Fatalf("reviewer calls = %d", rev.calls)
	}
	if out.NextStep != hermes.RuntimeStepTerminal {
		t.Errorf("NextStep = %q, want terminal", out.NextStep)
	}
	if out.Reason != "reviewer_pass" {
		t.Errorf("Reason = %q", out.Reason)
	}
	if len(out.Updates) != 1 {
		t.Fatalf("expected one update with telemetry, got %d", len(out.Updates))
	}
	u := out.Updates[0]
	if len(u.ModelUsages) != 1 || u.ModelUsages[0].Model != "reviewer-1" {
		t.Errorf("ModelUsages: %+v", u.ModelUsages)
	}
	if len(u.PhaseUsages) != 1 || u.PhaseUsages[0].Phase != "reviewer" {
		t.Errorf("PhaseUsages: %+v", u.PhaseUsages)
	}
	if u.TokenUsageDelta != 150 {
		t.Errorf("TokenUsageDelta = %d, want 150", u.TokenUsageDelta)
	}
	if u.Status != nil {
		t.Errorf("Status should not be set on pass: %+v", u.Status)
	}
}

func TestReviewerNode_PassWithoutTelemetryEmitsNoUpdate(t *testing.T) {
	rev := &fakeTaskReviewer{result: TaskReviewResult{Verdict: "pass"}}
	node := &ReviewerNode{Reviewer: rev}

	out, err := node.Handle(context.Background(), makeReviewerState())
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(out.Updates) != 0 {
		t.Errorf("expected no Updates on telemetry-free pass, got %d", len(out.Updates))
	}
	if out.NextStep != hermes.RuntimeStepTerminal {
		t.Errorf("NextStep = %q", out.NextStep)
	}
}

func TestReviewerNode_PartialAndAllowAccept(t *testing.T) {
	for _, verdict := range []string{"partial", "allow", "weird-unknown"} {
		t.Run(verdict, func(t *testing.T) {
			rev := &fakeTaskReviewer{result: TaskReviewResult{Verdict: verdict}}
			node := &ReviewerNode{Reviewer: rev}
			out, err := node.Handle(context.Background(), makeReviewerState())
			if err != nil {
				t.Fatalf("Handle: %v", err)
			}
			if out.NextStep != hermes.RuntimeStepTerminal {
				t.Errorf("NextStep = %q, want terminal", out.NextStep)
			}
			if out.Reason != "reviewer_pass" {
				t.Errorf("Reason = %q", out.Reason)
			}
		})
	}
}

func TestReviewerNode_FailMarksStatusFailed(t *testing.T) {
	rev := &fakeTaskReviewer{result: TaskReviewResult{
		Verdict:  "fail",
		Feedback: "task did not meet acceptance criteria",
	}}
	node := &ReviewerNode{Reviewer: rev}
	out, err := node.Handle(context.Background(), makeReviewerState())
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if out.NextStep != hermes.RuntimeStepTerminal {
		t.Errorf("NextStep = %q", out.NextStep)
	}
	if out.Reason != "reviewer_fail" {
		t.Errorf("Reason = %q", out.Reason)
	}
	if len(out.Updates) != 1 {
		t.Fatalf("expected one update")
	}
	u := out.Updates[0]
	if u.Status == nil || *u.Status != hermes.TaskStatusFailed {
		t.Errorf("Status = %+v, want pointer to failed", u.Status)
	}
}

func TestReviewerNode_BlockWithReplanRoutesReplanSetup(t *testing.T) {
	rev := &fakeTaskReviewer{result: TaskReviewResult{
		Verdict:      "block",
		Replan:       true,
		OverallScore: 40,
		Model:        "reviewer-1",
		InputTokens:  10,
		OutputTokens: 5,
	}}
	node := &ReviewerNode{Reviewer: rev}
	out, err := node.Handle(context.Background(), makeReviewerState())
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if out.NextStep != hermes.RuntimeStepReplanSetup {
		t.Errorf("NextStep = %q, want replan_setup", out.NextStep)
	}
	if out.Reason != "reviewer_block_replan" {
		t.Errorf("Reason = %q", out.Reason)
	}
	if len(out.Updates) != 1 {
		t.Fatalf("expected telemetry update")
	}
	if out.Updates[0].PhaseUsages[0].Phase != "reviewer" {
		t.Errorf("phase: %+v", out.Updates[0].PhaseUsages)
	}
}

func TestReviewerNode_BlockWithoutReplanRoutesTerminal(t *testing.T) {
	rev := &fakeTaskReviewer{result: TaskReviewResult{Verdict: "block"}}
	node := &ReviewerNode{Reviewer: rev}
	out, err := node.Handle(context.Background(), makeReviewerState())
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if out.NextStep != hermes.RuntimeStepTerminal {
		t.Errorf("NextStep = %q", out.NextStep)
	}
	if out.Reason != "reviewer_block_terminal" {
		t.Errorf("Reason = %q", out.Reason)
	}
}

func TestReviewerNode_ReviewerErrorAcceptsAndContinues(t *testing.T) {
	rev := &fakeTaskReviewer{err: errors.New("reviewer timeout")}
	node := &ReviewerNode{Reviewer: rev}
	out, err := node.Handle(context.Background(), makeReviewerState())
	if err != nil {
		t.Fatalf("Handle should swallow reviewer error, got: %v", err)
	}
	if out.NextStep != hermes.RuntimeStepTerminal {
		t.Errorf("NextStep = %q", out.NextStep)
	}
	if out.Reason != "reviewer_error_accept" {
		t.Errorf("Reason = %q", out.Reason)
	}
	if len(out.Updates) != 0 {
		t.Errorf("no updates expected on reviewer error: %+v", out.Updates)
	}
}

func TestReviewerNode_NilReviewerRejected(t *testing.T) {
	node := &ReviewerNode{}
	if _, err := node.Handle(context.Background(), makeReviewerState()); err == nil {
		t.Fatalf("expected error when Reviewer is nil")
	}
}

func TestReviewerNode_NameMatches(t *testing.T) {
	node := &ReviewerNode{Reviewer: &fakeTaskReviewer{}}
	if got := node.Name(); got != hermes.RuntimeStepReviewer {
		t.Errorf("Name = %q, want reviewer", got)
	}
}
