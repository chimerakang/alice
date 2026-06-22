package graph

import (
	"context"
	"testing"
	"time"

	"claude-tg-agent/internal/app/hermes"
)

func makeApprovalState(plan []hermes.SubTask, currentIdx int, status hermes.TaskStatus, interrupt *hermes.HermesInterrupt) hermes.HermesState {
	return hermes.HermesState{
		TaskID:     "task-approval",
		ChatID:     42,
		Goal:       "approval test",
		ProjectDir: "/repo",
		Status:     status,
		Plan:       append([]hermes.SubTask(nil), plan...),
		CurrentIdx: currentIdx,
		Interrupt:  interrupt,
	}
}

func makePendingInterrupt(idx int, total int) *hermes.HermesInterrupt {
	return &hermes.HermesInterrupt{
		ID:         "iv-pending",
		SourceStep: hermes.RuntimeStepExecutor,
		ResumeStep: hermes.RuntimeStepExecutor,
		Reason:     "subtask_failure_pause",
		CreatedAt:  time.Now(),
		Payload: map[string]any{
			"sub_task_idx": idx,
			"total":        total,
		},
	}
}

func TestApprovalNode_FirstVisitHaltsWalker(t *testing.T) {
	node := &ApprovalNode{}
	state := makeApprovalState(
		[]hermes.SubTask{
			{ID: "s1", Status: hermes.SubTaskInProgress},
		},
		0,
		hermes.TaskStatusExecuting,
		makePendingInterrupt(0, 1),
	)
	out, err := node.Handle(context.Background(), state)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !out.Halt {
		t.Errorf("first visit should set Halt=true, got %+v", out)
	}
	if out.NextStep != hermes.RuntimeStepApproval {
		t.Errorf("NextStep = %q, want approval (re-enter on resume)", out.NextStep)
	}
	if len(out.Updates) != 0 {
		t.Errorf("first visit should not mutate state: %+v", out.Updates)
	}
	if out.Reason != "approval_pending" {
		t.Errorf("Reason = %q, want approval_pending", out.Reason)
	}
}

func TestApprovalNode_RetryResolutionRoutesBackToExecutor(t *testing.T) {
	// β2 retry resolution: ApplyInterruptResolution clears Interrupt only;
	// plan + currentIdx unchanged; sub-task is still InProgress so
	// ExecutorNode will re-run it.
	node := &ApprovalNode{}
	state := makeApprovalState(
		[]hermes.SubTask{
			{ID: "s1", Status: hermes.SubTaskInProgress},
		},
		0,
		hermes.TaskStatusExecuting,
		nil, // resolution cleared the interrupt
	)
	out, err := node.Handle(context.Background(), state)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if out.Halt {
		t.Errorf("resolved approval should not Halt")
	}
	if out.NextStep != hermes.RuntimeStepExecutor {
		t.Errorf("NextStep = %q, want executor (retry re-runs)", out.NextStep)
	}
	if out.Reason != "approval_resolved_continue" {
		t.Errorf("Reason = %q, want approval_resolved_continue", out.Reason)
	}
}

func TestApprovalNode_SkipResolutionAdvancesToNextSubTask(t *testing.T) {
	// β2 skip: ApplyInterruptResolution advanced CurrentIdx past the
	// failed sub-task and marked it Skipped. Routing back to executor
	// is correct; ExecutorNode will see plan[next].Pending and run it.
	node := &ApprovalNode{}
	state := makeApprovalState(
		[]hermes.SubTask{
			{ID: "s1", Status: hermes.SubTaskSkipped},
			{ID: "s2", Status: hermes.SubTaskPending},
		},
		1, // already advanced
		hermes.TaskStatusExecuting,
		nil,
	)
	out, _ := node.Handle(context.Background(), state)
	if out.NextStep != hermes.RuntimeStepExecutor {
		t.Errorf("NextStep = %q, want executor", out.NextStep)
	}
}

func TestApprovalNode_AbortResolutionRoutesToTerminal(t *testing.T) {
	// β2 abort: ApplyInterruptResolution called MarkTaskFailedDurable
	// which sets state.Status = Failed. Approval should observe this
	// and route straight to terminal so the Walker exits cleanly.
	node := &ApprovalNode{}
	state := makeApprovalState(
		[]hermes.SubTask{
			{ID: "s1", Status: hermes.SubTaskInProgress},
		},
		0,
		hermes.TaskStatusFailed,
		nil,
	)
	out, _ := node.Handle(context.Background(), state)
	if out.NextStep != hermes.RuntimeStepTerminal {
		t.Errorf("NextStep = %q, want terminal (abort)", out.NextStep)
	}
	if out.Reason != "approval_resolved_abort" {
		t.Errorf("Reason = %q, want approval_resolved_abort", out.Reason)
	}
}

func TestApprovalNode_SkipOnLastSubTaskRoutesToTerminal(t *testing.T) {
	// Skip on the last sub-task advances CurrentIdx past the end.
	// Approval routes to terminal (or reviewer if per-task review on).
	node := &ApprovalNode{}
	state := makeApprovalState(
		[]hermes.SubTask{
			{ID: "s1", Status: hermes.SubTaskSkipped},
		},
		1, // past end
		hermes.TaskStatusExecuting,
		nil,
	)
	out, _ := node.Handle(context.Background(), state)
	if out.NextStep != hermes.RuntimeStepTerminal {
		t.Errorf("NextStep = %q, want terminal", out.NextStep)
	}
	if out.Reason != "approval_resolved_plan_complete" {
		t.Errorf("Reason = %q, want approval_resolved_plan_complete", out.Reason)
	}
}

func TestApprovalNode_SkipOnLastSubTaskWithPerTaskReviewRoutesToReviewer(t *testing.T) {
	node := &ApprovalNode{ReviewModeIsPerTask: true}
	state := makeApprovalState(
		[]hermes.SubTask{
			{ID: "s1", Status: hermes.SubTaskSkipped},
		},
		1,
		hermes.TaskStatusExecuting,
		nil,
	)
	out, _ := node.Handle(context.Background(), state)
	if out.NextStep != hermes.RuntimeStepReviewer {
		t.Errorf("NextStep = %q, want reviewer", out.NextStep)
	}
}

func TestApprovalNode_NameMatchesRuntimeStepApproval(t *testing.T) {
	node := &ApprovalNode{}
	if got := node.Name(); got != hermes.RuntimeStepApproval {
		t.Errorf("Name() = %q, want %q", got, hermes.RuntimeStepApproval)
	}
}

func TestApprovalNode_RejectsInvalidStateOnResume(t *testing.T) {
	// Resolved approval (Interrupt nil) but no plan — should error
	// rather than route blindly.
	node := &ApprovalNode{}
	state := hermes.HermesState{TaskID: "x", Status: hermes.TaskStatusExecuting}
	_, err := node.Handle(context.Background(), state)
	if err == nil {
		t.Fatal("expected error for resume with empty plan")
	}
}

// Integration: ExecutorNode failure with FailurePauseEnabled emits an
// Interrupt and routes to approval; ApprovalNode then halts.
func TestExecutorAndApproval_FailurePauseHaltsWalker(t *testing.T) {
	runner := &fakeRunner{
		err: contextDeadlineErr,
	}
	exec := &ExecutorNode{Runner: runner, FailurePauseEnabled: true}

	state := makeApprovalState(
		[]hermes.SubTask{
			{ID: "s1", Description: "first", Status: hermes.SubTaskInProgress},
			{ID: "s2", Description: "second", Status: hermes.SubTaskPending},
		},
		0,
		hermes.TaskStatusExecuting,
		nil,
	)
	out, err := exec.Handle(context.Background(), state)
	if err != nil {
		t.Fatalf("ExecutorNode.Handle: %v", err)
	}
	if out.NextStep != hermes.RuntimeStepApproval {
		t.Errorf("ExecutorNode failure NextStep = %q, want approval", out.NextStep)
	}
	if out.Reason != "subtask_failure_pause" {
		t.Errorf("Reason = %q, want subtask_failure_pause", out.Reason)
	}
	if len(out.Updates) != 1 || out.Updates[0].Interrupt == nil {
		t.Fatalf("ExecutorNode failure should commit Interrupt: %+v", out.Updates)
	}
	// Plan entry stays InProgress so Retry resolution can re-run; only
	// attempts + result are stamped.
	if u := out.Updates[0]; u.Plan[0].Status != hermes.SubTaskInProgress {
		t.Errorf("plan[0] status = %q after pause, want still in_progress", u.Plan[0].Status)
	}
	if u := out.Updates[0]; u.Plan[0].Attempts != 1 {
		t.Errorf("plan[0] attempts = %d, want 1", u.Plan[0].Attempts)
	}

	// Now simulate the post-pause state landing in ApprovalNode while
	// unresolved (still has Interrupt). Should Halt.
	approval := &ApprovalNode{}
	postPauseState := state
	postPauseState.Interrupt = out.Updates[0].Interrupt
	postPauseState.Plan = out.Updates[0].Plan
	approvalOut, err := approval.Handle(context.Background(), postPauseState)
	if err != nil {
		t.Fatalf("ApprovalNode.Handle: %v", err)
	}
	if !approvalOut.Halt {
		t.Errorf("ApprovalNode unresolved should Halt")
	}
}

func TestExecutorNode_FailurePauseDisabledKeepsLegacyAdvanceBehaviour(t *testing.T) {
	// FailurePauseEnabled=false (γ3a default) — runner error should mark
	// sub-task Failed and advance, NOT emit Interrupt.
	runner := &fakeRunner{err: contextDeadlineErr}
	exec := &ExecutorNode{Runner: runner, FailurePauseEnabled: false}

	state := makeApprovalState(
		[]hermes.SubTask{
			{ID: "s1", Status: hermes.SubTaskPending},
			{ID: "s2", Status: hermes.SubTaskPending},
		},
		0,
		hermes.TaskStatusExecuting,
		nil,
	)
	out, _ := exec.Handle(context.Background(), state)
	if out.Updates[0].Interrupt != nil {
		t.Errorf("FailurePauseEnabled=false should NOT emit Interrupt: %+v", out.Updates[0].Interrupt)
	}
	if out.NextStep != hermes.RuntimeStepExecutor {
		t.Errorf("legacy advance NextStep = %q, want executor", out.NextStep)
	}
	if out.Updates[0].Plan[0].Status != hermes.SubTaskFailed {
		t.Errorf("legacy advance plan[0] status = %q, want failed", out.Updates[0].Plan[0].Status)
	}
}

// contextDeadlineErr is a stable error sentinel for failure-pause tests.
var contextDeadlineErr = &timeoutError{}

type timeoutError struct{}

func (timeoutError) Error() string { return "deadline exceeded" }
