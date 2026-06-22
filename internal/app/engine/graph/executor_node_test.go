package graph

import (
	"context"
	"errors"
	"testing"

	"claude-tg-agent/internal/app/hermes"
)

// fakeRunner is a SubTaskRunner stub for ExecutorNode tests. The Run
// hook is invoked once per RunSubTask call so tests can assert call
// counts and inspect the (state, idx) the Node passed in.
type fakeRunner struct {
	calls   int
	lastIdx int
	result  SubTaskRunResult
	err     error
	hook    func(state hermes.HermesState, idx int)
}

func (r *fakeRunner) RunSubTask(_ context.Context, state hermes.HermesState, idx int) (SubTaskRunResult, error) {
	r.calls++
	r.lastIdx = idx
	if r.hook != nil {
		r.hook(state, idx)
	}
	return r.result, r.err
}

func makeExecutorState(plan []hermes.SubTask, currentIdx int, accumulated string) hermes.HermesState {
	return hermes.HermesState{
		TaskID:      "task-exec",
		ChatID:      42,
		Goal:        "test goal",
		ProjectDir:  "/repo",
		Status:      hermes.TaskStatusExecuting,
		Plan:        append([]hermes.SubTask(nil), plan...),
		CurrentIdx:  currentIdx,
		Accumulated: accumulated,
	}
}

func TestExecutorNode_HandleHappyPathRoutesToNextSubTask(t *testing.T) {
	runner := &fakeRunner{
		result: SubTaskRunResult{
			Text:                "completed step",
			Model:               "claude-sonnet-4-6",
			UncachedInputTokens: 100,
			OutputTokens:        50,
			CostUSD:             0.012,
		},
	}
	node := &ExecutorNode{Runner: runner, ReviewModeIsPerTask: false}

	state := makeExecutorState([]hermes.SubTask{
		{ID: "s1", Description: "first", Status: hermes.SubTaskPending},
		{ID: "s2", Description: "second", Status: hermes.SubTaskPending},
	}, 0, "")

	out, err := node.Handle(context.Background(), state)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if runner.calls != 1 || runner.lastIdx != 0 {
		t.Errorf("runner calls=%d lastIdx=%d, want 1/0", runner.calls, runner.lastIdx)
	}
	if out.NextStep != hermes.RuntimeStepExecutor {
		t.Errorf("NextStep = %q, want executor (more sub-tasks remain)", out.NextStep)
	}
	if out.Reason != "subtask_done" {
		t.Errorf("Reason = %q, want subtask_done", out.Reason)
	}
	if len(out.Updates) != 1 {
		t.Fatalf("Updates = %d, want 1", len(out.Updates))
	}

	u := out.Updates[0]
	if u.CurrentIdx == nil || *u.CurrentIdx != 1 {
		t.Errorf("CurrentIdx = %+v, want 1", u.CurrentIdx)
	}
	if len(u.Plan) != 2 || u.Plan[0].Status != hermes.SubTaskDone || u.Plan[0].Result != "completed step" {
		t.Errorf("plan after success: %+v", u.Plan)
	}
	if u.Plan[0].TokensUsed != 150 || u.Plan[0].Attempts != 1 {
		t.Errorf("plan[0] tokens/attempts = %d/%d, want 150/1", u.Plan[0].TokensUsed, u.Plan[0].Attempts)
	}
	if u.Accumulated == nil || *u.Accumulated == "" {
		t.Errorf("Accumulated should be set on success: %+v", u.Accumulated)
	}
	if u.TokenUsageDelta != 150 {
		t.Errorf("TokenUsageDelta = %d, want 150", u.TokenUsageDelta)
	}
}

func TestExecutorNode_HandleLastSubTaskRoutesToReviewerWhenPerTask(t *testing.T) {
	runner := &fakeRunner{result: SubTaskRunResult{Text: "ok"}}
	node := &ExecutorNode{Runner: runner, ReviewModeIsPerTask: true}

	state := makeExecutorState([]hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskPending},
	}, 0, "")
	out, _ := node.Handle(context.Background(), state)
	if out.NextStep != hermes.RuntimeStepReviewer {
		t.Errorf("NextStep = %q, want reviewer", out.NextStep)
	}
}

func TestExecutorNode_HandleLastSubTaskRoutesToTerminalWhenNoReview(t *testing.T) {
	runner := &fakeRunner{result: SubTaskRunResult{Text: "ok"}}
	node := &ExecutorNode{Runner: runner, ReviewModeIsPerTask: false}

	state := makeExecutorState([]hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskPending},
	}, 0, "")
	out, _ := node.Handle(context.Background(), state)
	if out.NextStep != hermes.RuntimeStepTerminal {
		t.Errorf("NextStep = %q, want terminal", out.NextStep)
	}
}

func TestExecutorNode_HandleRunnerErrorMarksFailedAndAdvances(t *testing.T) {
	runner := &fakeRunner{
		result: SubTaskRunResult{Model: "claude", UncachedInputTokens: 30, OutputTokens: 10, CostUSD: 0.001},
		err:    errors.New("CLI: deadline exceeded"),
	}
	node := &ExecutorNode{Runner: runner}

	state := makeExecutorState([]hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskPending},
		{ID: "s2", Status: hermes.SubTaskPending},
	}, 0, "")

	out, err := node.Handle(context.Background(), state)
	if err != nil {
		t.Fatalf("Handle should not propagate runner error (sub-task failure is captured in StateUpdate): %v", err)
	}
	u := out.Updates[0]
	if u.Plan[0].Status != hermes.SubTaskFailed {
		t.Errorf("plan[0] status = %q, want failed", u.Plan[0].Status)
	}
	if u.Plan[0].Result != "CLI: deadline exceeded" {
		t.Errorf("plan[0] result = %q, want runner error text", u.Plan[0].Result)
	}
	if out.Reason != "subtask_failed" {
		t.Errorf("Reason = %q, want subtask_failed", out.Reason)
	}
	if u.Accumulated != nil {
		t.Errorf("Accumulated should NOT be set on failure: %+v", u.Accumulated)
	}
	// Telemetry still emitted (runner spent tokens before error)
	if u.TokenUsageDelta != 40 {
		t.Errorf("TokenUsageDelta = %d, want 40 (failure-path telemetry)", u.TokenUsageDelta)
	}
	if out.NextStep != hermes.RuntimeStepExecutor {
		t.Errorf("NextStep = %q, want executor (continue with next sub-task)", out.NextStep)
	}
}

func TestExecutorNode_HandleSkipsAlreadyDoneSubTask(t *testing.T) {
	// Resume scenario: snapshot replay positions the walker on a sub-task
	// already marked Done. Node should advance without invoking runner.
	runner := &fakeRunner{}
	node := &ExecutorNode{Runner: runner}
	state := makeExecutorState([]hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskDone, Result: "earlier output"},
		{ID: "s2", Status: hermes.SubTaskPending},
	}, 0, "earlier output")

	out, err := node.Handle(context.Background(), state)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if runner.calls != 0 {
		t.Errorf("runner should not be invoked on done sub-task, got %d calls", runner.calls)
	}
	if out.Reason != "subtask_already_done" {
		t.Errorf("Reason = %q, want subtask_already_done", out.Reason)
	}
	if out.Updates[0].CurrentIdx == nil || *out.Updates[0].CurrentIdx != 1 {
		t.Errorf("CurrentIdx not advanced: %+v", out.Updates[0].CurrentIdx)
	}
}

func TestExecutorNode_HandleSkipsAlreadySkippedSubTask(t *testing.T) {
	// β2 / 3c skip resolution writes plan[idx]=Skipped; on resume the
	// walker lands here and should advance past it.
	runner := &fakeRunner{}
	node := &ExecutorNode{Runner: runner}
	state := makeExecutorState([]hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskSkipped},
	}, 0, "")
	out, _ := node.Handle(context.Background(), state)
	if runner.calls != 0 {
		t.Errorf("runner should not run on skipped sub-task, got %d calls", runner.calls)
	}
	if out.NextStep != hermes.RuntimeStepTerminal {
		t.Errorf("NextStep = %q, want terminal (last and skipped)", out.NextStep)
	}
}

func TestExecutorNode_HandleRejectsOutOfRangeIdx(t *testing.T) {
	runner := &fakeRunner{}
	node := &ExecutorNode{Runner: runner}
	state := makeExecutorState([]hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskPending},
	}, 5, "")
	_, err := node.Handle(context.Background(), state)
	if err == nil {
		t.Fatal("expected error for out-of-range CurrentIdx")
	}
}

func TestExecutorNode_HandleRejectsEmptyPlan(t *testing.T) {
	runner := &fakeRunner{}
	node := &ExecutorNode{Runner: runner}
	state := hermes.HermesState{TaskID: "x"}
	_, err := node.Handle(context.Background(), state)
	if err == nil {
		t.Fatal("expected error for empty plan")
	}
}

func TestExecutorNode_HandleRejectsNilRunner(t *testing.T) {
	node := &ExecutorNode{}
	_, err := node.Handle(context.Background(), hermes.HermesState{})
	if err == nil {
		t.Fatal("expected error for nil Runner")
	}
}

func TestExecutorNode_HandleAccumulatedAppendsConclusion(t *testing.T) {
	// AppendResult extracts the "結論" line from the result text and
	// appends it to accumulated. Verify the Node passes through the same
	// helper so accumulated grows on success.
	runner := &fakeRunner{
		result: SubTaskRunResult{
			Text: "**結論**：第一步驟完成\n\n**證據**：file.go modified",
		},
	}
	node := &ExecutorNode{Runner: runner}
	state := makeExecutorState([]hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskPending},
		{ID: "s2", Status: hermes.SubTaskPending},
	}, 0, "")

	out, _ := node.Handle(context.Background(), state)
	if out.Updates[0].Accumulated == nil {
		t.Fatal("Accumulated should be set")
	}
	got := *out.Updates[0].Accumulated
	if got == "" {
		t.Errorf("Accumulated empty, want non-empty after success")
	}
}

func TestExecutorNode_PropagatesWalkingStateFromRunner(t *testing.T) {
	walking := &hermes.WalkingAgentState{
		Enabled:           true,
		PrevExecutorModel: "claude-sonnet-4-6",
		TokensSeen:        4500,
		MaxContextTokens:  120_000,
	}
	runner := &fakeRunner{
		result: SubTaskRunResult{
			Text:         "ok",
			Model:        "claude-sonnet-4-6",
			OutputTokens: 50,
			WalkingState: walking,
		},
	}
	node := &ExecutorNode{Runner: runner}
	state := makeExecutorState([]hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskPending},
		{ID: "s2", Status: hermes.SubTaskPending},
	}, 0, "")
	out, err := node.Handle(context.Background(), state)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	u := out.Updates[0]
	if u.Walking == nil {
		t.Fatalf("expected Walking propagated to StateUpdate")
	}
	if u.Walking.PrevExecutorModel != "claude-sonnet-4-6" || u.Walking.TokensSeen != 4500 {
		t.Errorf("Walking = %+v", u.Walking)
	}
	if u.ClearWalking {
		t.Errorf("ClearWalking should be false when Walking is set")
	}
}

func TestExecutorNode_RunnerWalkingDisabledClearsState(t *testing.T) {
	runner := &fakeRunner{
		result: SubTaskRunResult{
			Text:         "ok",
			Model:        "claude-sonnet-4-6",
			OutputTokens: 50,
			// Enabled=false signals "explicit clear" to applyWalkingDelta.
			WalkingState: &hermes.WalkingAgentState{Enabled: false},
		},
	}
	node := &ExecutorNode{Runner: runner}
	state := makeExecutorState([]hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskPending},
	}, 0, "")
	out, _ := node.Handle(context.Background(), state)
	u := out.Updates[0]
	if !u.ClearWalking {
		t.Errorf("ClearWalking should be true when runner returns disabled walking")
	}
	if u.Walking != nil {
		t.Errorf("Walking should be nil on clear: %+v", u.Walking)
	}
}

func TestExecutorNode_NoWalkingStateLeavesUpdateAlone(t *testing.T) {
	runner := &fakeRunner{
		result: SubTaskRunResult{Text: "ok", Model: "m", OutputTokens: 5},
	}
	node := &ExecutorNode{Runner: runner}
	state := makeExecutorState([]hermes.SubTask{{ID: "s1", Status: hermes.SubTaskPending}}, 0, "")
	out, _ := node.Handle(context.Background(), state)
	u := out.Updates[0]
	if u.Walking != nil || u.ClearWalking {
		t.Errorf("expected no walking writes when runner returned nil WalkingState; got Walking=%+v ClearWalking=%v", u.Walking, u.ClearWalking)
	}
}
