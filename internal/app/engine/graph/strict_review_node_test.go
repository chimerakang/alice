package graph

import (
	"context"
	"errors"
	"strings"
	"testing"

	"claude-tg-agent/internal/app/hermes"
)

// fakeReviewer is a SubTaskReviewer stub. The Review hook is invoked
// once per ReviewSubTask call so tests can drive different verdicts and
// inspect (state, idx).
type fakeReviewer struct {
	calls   int
	lastIdx int
	result  SubTaskReviewResult
	err     error
}

func (r *fakeReviewer) ReviewSubTask(_ context.Context, _ hermes.HermesState, idx int) (SubTaskReviewResult, error) {
	r.calls++
	r.lastIdx = idx
	return r.result, r.err
}

func makeStrictState(plan []hermes.SubTask, idx int, accumulated string) hermes.HermesState {
	return hermes.HermesState{
		TaskID:      "task-strict",
		ChatID:      42,
		Goal:        "strict test",
		ProjectDir:  "/repo",
		Status:      hermes.TaskStatusExecuting,
		Plan:        append([]hermes.SubTask(nil), plan...),
		CurrentIdx:  idx,
		Accumulated: accumulated,
	}
}

func TestStrictReviewNode_BlockWithBudgetRoutesBackToExecutor(t *testing.T) {
	reviewer := &fakeReviewer{
		result: SubTaskReviewResult{
			Verdict:  "block",
			Feedback: "Add tests covering edge case X",
			Score:    62,
		},
	}
	node := &StrictReviewNode{Reviewer: reviewer, MaxRetriesPerSub: 2}
	state := makeStrictState([]hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskInProgress, Result: "first attempt", Attempts: 1},
		{ID: "s2", Status: hermes.SubTaskPending},
	}, 0, "")

	out, err := node.Handle(context.Background(), state)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if out.NextStep != hermes.RuntimeStepExecutor {
		t.Errorf("NextStep = %q, want executor (retry)", out.NextStep)
	}
	if out.Reason != "strict_review_block_retry" {
		t.Errorf("Reason = %q", out.Reason)
	}
	u := out.Updates[0]
	if u.CurrentIdx != nil {
		t.Errorf("CurrentIdx should NOT advance on retry: %+v", u.CurrentIdx)
	}
	if u.Plan[0].StrictRetryFeedback != "Add tests covering edge case X" {
		t.Errorf("feedback not persisted to plan[0]: %+v", u.Plan[0])
	}
	if u.Plan[0].Status != hermes.SubTaskInProgress {
		t.Errorf("status = %q, want still in_progress", u.Plan[0].Status)
	}
}

func TestStrictReviewNode_BlockBudgetExhaustedSkipsAsPartial(t *testing.T) {
	reviewer := &fakeReviewer{
		result: SubTaskReviewResult{
			Verdict:  "block",
			Feedback: "still missing validation",
		},
	}
	node := &StrictReviewNode{Reviewer: reviewer, MaxRetriesPerSub: 2}
	// Attempts == MaxRetriesPerSub → no more retries.
	state := makeStrictState([]hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskInProgress, Result: "third attempt content", Attempts: 2},
		{ID: "s2", Status: hermes.SubTaskPending},
	}, 0, "")

	out, err := node.Handle(context.Background(), state)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if out.NextStep != hermes.RuntimeStepExecutor {
		t.Errorf("NextStep = %q, want executor (advance to next)", out.NextStep)
	}
	if out.Reason != "strict_review_block_budget_exhausted" {
		t.Errorf("Reason = %q", out.Reason)
	}
	u := out.Updates[0]
	if u.CurrentIdx == nil || *u.CurrentIdx != 1 {
		t.Errorf("CurrentIdx = %+v, want 1 (advanced)", u.CurrentIdx)
	}
	if u.Plan[0].Status != hermes.SubTaskSkipped {
		t.Errorf("status = %q, want skipped (partial)", u.Plan[0].Status)
	}
	if !strings.HasPrefix(u.Plan[0].Result, "PARTIAL") {
		t.Errorf("Result should start with PARTIAL: %q", u.Plan[0].Result)
	}
	if !strings.Contains(u.Plan[0].Result, "third attempt content") {
		t.Errorf("Result should preserve original attempt text: %q", u.Plan[0].Result)
	}
	if !strings.Contains(u.Plan[0].Result, "Reviewer feedback") {
		t.Errorf("Result should contain reviewer feedback marker: %q", u.Plan[0].Result)
	}
	if u.Plan[0].StrictRetryFeedback != "" {
		t.Errorf("feedback should be cleared after partial: %q", u.Plan[0].StrictRetryFeedback)
	}
}

func TestStrictReviewNode_FailVerdictMarksFailedAndAdvances(t *testing.T) {
	reviewer := &fakeReviewer{
		result: SubTaskReviewResult{
			Verdict:  "fail",
			Feedback: "fundamentally broken approach",
		},
	}
	node := &StrictReviewNode{Reviewer: reviewer, MaxRetriesPerSub: 5}
	state := makeStrictState([]hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskInProgress, Result: "broken impl", Attempts: 1},
	}, 0, "")

	out, _ := node.Handle(context.Background(), state)
	u := out.Updates[0]
	if u.Plan[0].Status != hermes.SubTaskFailed {
		t.Errorf("status = %q, want failed", u.Plan[0].Status)
	}
	if u.Plan[0].Result != "fundamentally broken approach" {
		t.Errorf("result = %q, want reviewer feedback", u.Plan[0].Result)
	}
	if out.NextStep != hermes.RuntimeStepTerminal {
		t.Errorf("NextStep = %q, want terminal (last sub-task done)", out.NextStep)
	}
}

func TestStrictReviewNode_PassVerdictMarksDoneAndAdvances(t *testing.T) {
	reviewer := &fakeReviewer{
		result: SubTaskReviewResult{
			Verdict: "pass",
			Score:   91,
		},
	}
	node := &StrictReviewNode{Reviewer: reviewer, MaxRetriesPerSub: 2}
	state := makeStrictState([]hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskInProgress, Result: "good output", Attempts: 1, StrictRetryFeedback: "stale"},
		{ID: "s2", Status: hermes.SubTaskPending},
	}, 0, "")

	out, _ := node.Handle(context.Background(), state)
	u := out.Updates[0]
	if u.Plan[0].Status != hermes.SubTaskDone {
		t.Errorf("status = %q, want done", u.Plan[0].Status)
	}
	if u.Plan[0].Result != "good output" {
		t.Errorf("Result should be preserved on pass: %q", u.Plan[0].Result)
	}
	if u.Plan[0].StrictRetryFeedback != "" {
		t.Errorf("feedback should be cleared on pass: %q", u.Plan[0].StrictRetryFeedback)
	}
	if u.Accumulated == nil {
		t.Errorf("Accumulated should be appended on pass")
	}
	if out.NextStep != hermes.RuntimeStepExecutor {
		t.Errorf("NextStep = %q, want executor (next sub-task)", out.NextStep)
	}
}

func TestStrictReviewNode_PartialVerdictAcceptsAsDone(t *testing.T) {
	// "partial" is a softer than "block" verdict; the legacy engine
	// treats it as accept rather than retry. Mirror that.
	reviewer := &fakeReviewer{
		result: SubTaskReviewResult{Verdict: "partial", Score: 75},
	}
	node := &StrictReviewNode{Reviewer: reviewer, MaxRetriesPerSub: 2}
	state := makeStrictState([]hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskInProgress, Result: "partial output"},
	}, 0, "")

	out, _ := node.Handle(context.Background(), state)
	if out.Updates[0].Plan[0].Status != hermes.SubTaskDone {
		t.Errorf("partial verdict should accept as Done")
	}
}

func TestStrictReviewNode_ReviewerErrorAcceptsConservatively(t *testing.T) {
	// A reviewer-call failure should not gate the whole task. Treat
	// it as accept-and-continue so a transient reviewer outage does
	// not block progress.
	reviewer := &fakeReviewer{err: errors.New("reviewer CLI: rate limit")}
	node := &StrictReviewNode{Reviewer: reviewer, MaxRetriesPerSub: 2}
	state := makeStrictState([]hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskInProgress, Result: "passed-through"},
	}, 0, "")
	out, err := node.Handle(context.Background(), state)
	if err != nil {
		t.Fatalf("Handle should not propagate reviewer err: %v", err)
	}
	if out.Updates[0].Plan[0].Status != hermes.SubTaskDone {
		t.Errorf("reviewer-error path should accept Done")
	}
	if out.Reason != "strict_review_reviewer_error_accept" {
		t.Errorf("Reason = %q", out.Reason)
	}
}

func TestStrictReviewNode_RetryAttachesReviewerTelemetry(t *testing.T) {
	reviewer := &fakeReviewer{
		result: SubTaskReviewResult{
			Verdict:      "block",
			Feedback:     "fix",
			Model:        "gpt-5.5",
			InputTokens:  300,
			OutputTokens: 100,
			CostUSD:      0.05,
		},
	}
	node := &StrictReviewNode{Reviewer: reviewer, MaxRetriesPerSub: 3}
	state := makeStrictState([]hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskInProgress, Attempts: 1},
	}, 0, "")
	out, _ := node.Handle(context.Background(), state)
	u := out.Updates[0]
	if len(u.ModelUsages) != 1 || u.ModelUsages[0].Model != "gpt-5.5" {
		t.Errorf("reviewer ModelUsage missing: %+v", u.ModelUsages)
	}
	if len(u.PhaseUsages) != 1 || u.PhaseUsages[0].Phase != "reviewer_strict" {
		t.Errorf("reviewer PhaseUsage missing: %+v", u.PhaseUsages)
	}
	if u.TokenUsageDelta != 400 {
		t.Errorf("TokenUsageDelta = %d, want 400", u.TokenUsageDelta)
	}
}

func TestStrictReviewNode_RejectsNilReviewer(t *testing.T) {
	node := &StrictReviewNode{}
	_, err := node.Handle(context.Background(), hermes.HermesState{})
	if err == nil {
		t.Fatal("expected error for nil Reviewer")
	}
}

func TestStrictReviewNode_RejectsEmptyPlan(t *testing.T) {
	node := &StrictReviewNode{Reviewer: &fakeReviewer{}}
	_, err := node.Handle(context.Background(), hermes.HermesState{})
	if err == nil {
		t.Fatal("expected error for empty plan")
	}
}

func TestStrictReviewNode_RejectsOutOfRangeIdx(t *testing.T) {
	node := &StrictReviewNode{Reviewer: &fakeReviewer{}}
	state := makeStrictState([]hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskInProgress},
	}, 5, "")
	_, err := node.Handle(context.Background(), state)
	if err == nil {
		t.Fatal("expected error for out-of-range idx")
	}
}

func TestStrictReviewNode_NameMatchesRuntimeStepStrictReview(t *testing.T) {
	node := &StrictReviewNode{}
	if got := node.Name(); got != hermes.RuntimeStepStrictReview {
		t.Errorf("Name() = %q, want %q", got, hermes.RuntimeStepStrictReview)
	}
}

// ExecutorNode strict-routing test: with StrictReviewEnabled the
// successful run hands off to StrictReview without finalising the
// sub-task.
func TestExecutorNode_StrictReviewEnabledRoutesToStrictReviewWithoutFinalise(t *testing.T) {
	runner := &fakeRunner{
		result: SubTaskRunResult{
			Text:                "candidate output",
			Model:               "claude-sonnet-4-6",
			UncachedInputTokens: 200,
			OutputTokens:        100,
			CostUSD:             0.02,
		},
	}
	node := &ExecutorNode{Runner: runner, StrictReviewEnabled: true}
	state := makeStrictState([]hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskInProgress},
		{ID: "s2", Status: hermes.SubTaskPending},
	}, 0, "")

	out, err := node.Handle(context.Background(), state)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if out.NextStep != hermes.RuntimeStepStrictReview {
		t.Errorf("NextStep = %q, want strict_review", out.NextStep)
	}
	if out.Reason != "subtask_candidate_for_strict_review" {
		t.Errorf("Reason = %q", out.Reason)
	}
	u := out.Updates[0]
	if u.CurrentIdx != nil {
		t.Errorf("CurrentIdx should not advance under StrictReviewEnabled: %+v", u.CurrentIdx)
	}
	if u.Plan[0].Status != hermes.SubTaskInProgress {
		t.Errorf("status = %q, want still in_progress (StrictReviewNode finalises)", u.Plan[0].Status)
	}
	if u.Plan[0].Result != "candidate output" {
		t.Errorf("Result not stored: %q", u.Plan[0].Result)
	}
	if u.Plan[0].Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", u.Plan[0].Attempts)
	}
	if u.TokenUsageDelta != 300 {
		t.Errorf("TokenUsageDelta = %d, want 300", u.TokenUsageDelta)
	}
}

func TestExecutorNode_StrictReviewDisabledStillFinalisesAsDone(t *testing.T) {
	// Sanity: leaving StrictReviewEnabled unset keeps γ3a behaviour.
	runner := &fakeRunner{result: SubTaskRunResult{Text: "ok"}}
	node := &ExecutorNode{Runner: runner /* StrictReviewEnabled: false */}
	state := makeStrictState([]hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskPending},
	}, 0, "")
	out, _ := node.Handle(context.Background(), state)
	if out.NextStep != hermes.RuntimeStepTerminal {
		t.Errorf("NextStep = %q, want terminal (no strict review)", out.NextStep)
	}
	if out.Updates[0].Plan[0].Status != hermes.SubTaskDone {
		t.Errorf("status = %q, want done", out.Updates[0].Plan[0].Status)
	}
}
