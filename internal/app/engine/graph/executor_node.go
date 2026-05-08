package graph

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"claude-tg-agent/internal/app/hermes"
)

// SubTaskRunResult is the return shape from SubTaskRunner.RunSubTask.
// Models what PlanExecuteEngine currently extracts from
// DirectEngine.Run + LastCacheMetrics, but in a Node-friendly bundle so
// the migration does not couple ExecutorNode to engine internals.
type SubTaskRunResult struct {
	Text                     string
	Model                    string
	InputTokens              int
	UncachedInputTokens      int
	CacheReadInputTokens     int
	CacheCreationInputTokens int
	OutputTokens             int
	CostUSD                  float64
}

// TokenVolume is the same metric PlanExecuteEngine uses to charge token
// budget: full input volume + output. Mirrors hermes.TokenUsageBreakdown
// semantics so telemetry stays consistent across the legacy and graph
// paths during migration.
func (r SubTaskRunResult) TokenVolume() int {
	input := r.InputTokens
	if input == 0 {
		input = r.UncachedInputTokens + r.CacheReadInputTokens + r.CacheCreationInputTokens
	}
	return input + r.OutputTokens
}

// SubTaskRunner is the executor-call abstraction. The production wiring
// will pass a thin adapter over PlanExecuteEngine.direct (DirectEngine
// wrapping hermesExecutorRunner); tests use stubs. Implementations are
// expected to handle prompt building, walking-agent flag handling, and
// the actual LLM call, returning a SubTaskRunResult on success or any
// error encountered.
//
// γ3a keeps walking-agent + strict retry + operator hint OUT of the
// ExecutorNode itself — those are concerns that bind the runner's
// internal state machine. Subsequent slices may either move them into
// the runner (production keeps current behaviour) or surface them as
// dedicated nodes.
type SubTaskRunner interface {
	RunSubTask(ctx context.Context, state hermes.HermesState, idx int) (SubTaskRunResult, error)
}

// ExecutorNode dispatches one sub-task per Walker iteration. When the
// pointed-at sub-task is already Done or Skipped (resume case after a
// snapshot replay) the Node advances CurrentIdx without re-running.
// Otherwise it calls the Runner, marks the sub-task Done or Failed,
// updates accumulated text and telemetry, and routes to the next step.
//
// Routing rules for NextStep:
//
//	more sub-tasks remaining → RuntimeStepExecutor (continue loop)
//	last sub-task done + ReviewMode == per_task → RuntimeStepReviewer
//	last sub-task done + no review              → RuntimeStepTerminal
//
// What's deliberately NOT in γ3a (deferred to γ3b/γ3c/γ4):
//
//	- walking-agent slim prompt + watermark management
//	- strict per-sub-task review retry loop
//	- operator hint injection from failure-pause flow
//	- failure-pause callback (which becomes ApprovalNode in γ4)
type ExecutorNode struct {
	// Runner is required. It owns all the executor-side concerns the
	// Node intentionally does not encode (prompt template, walking
	// session, model selection, cache metrics extraction).
	Runner SubTaskRunner
	// ReviewModeIsPerTask routes the final sub-task's NextStep to the
	// reviewer when true; otherwise the Walker terminates after the
	// last sub-task. Mirrors PlanExecuteConfig.ReviewMode.
	ReviewModeIsPerTask bool
}

// Name implements Node.
func (n *ExecutorNode) Name() hermes.RuntimeStep {
	return hermes.RuntimeStepExecutor
}

// Handle implements Node.
func (n *ExecutorNode) Handle(ctx context.Context, state hermes.HermesState) (NodeOutput, error) {
	if n == nil || n.Runner == nil {
		return NodeOutput{}, errors.New("graph: ExecutorNode requires a Runner")
	}
	if len(state.Plan) == 0 {
		return NodeOutput{}, errors.New("graph: ExecutorNode dispatched with empty plan")
	}
	idx := state.CurrentIdx
	if idx < 0 || idx >= len(state.Plan) {
		return NodeOutput{}, fmt.Errorf("graph: ExecutorNode CurrentIdx %d out of range (plan size %d)", idx, len(state.Plan))
	}

	subTask := state.Plan[idx]
	// Resume case: sub-task was already resolved by an earlier commit
	// (slice 3c+3d snapshots make this state durable). Advance without
	// re-running.
	if subTask.Status == hermes.SubTaskDone || subTask.Status == hermes.SubTaskSkipped {
		return n.advanceWithoutRunning(state, idx, "subtask_already_done"), nil
	}

	res, runErr := n.Runner.RunSubTask(ctx, state, idx)
	tokens := res.TokenVolume()

	finalStatus := hermes.SubTaskDone
	finalText := strings.TrimSpace(res.Text)
	reason := "subtask_done"
	if runErr != nil {
		finalStatus = hermes.SubTaskFailed
		finalText = runErr.Error()
		reason = "subtask_failed"
	}

	plan := append([]hermes.SubTask(nil), state.Plan...)
	plan[idx].Status = finalStatus
	plan[idx].Result = finalText
	plan[idx].TokensUsed += tokens
	plan[idx].Attempts++

	nextIdx := idx + 1
	update := hermes.StateUpdate{
		Plan:       plan,
		CurrentIdx: &nextIdx,
		SubTaskResults: []hermes.SubTaskResult{{
			SubTaskID:  plan[idx].ID,
			Index:      idx,
			Status:     finalStatus,
			Result:     finalText,
			TokensUsed: plan[idx].TokensUsed,
			Attempts:   plan[idx].Attempts,
		}},
	}

	// Append result text to the running accumulated buffer when the
	// sub-task succeeded — same shape as commitExecutorBoundary's
	// accumulated update.
	if finalStatus == hermes.SubTaskDone && finalText != "" {
		completed := completedCount(plan)
		updated, _ := hermes.AppendResult(state.Accumulated, finalText, completed)
		update.Accumulated = &updated
	}

	// Telemetry: emit ModelUsage / PhaseUsage / TokenUsageDelta when
	// the runner reported cost-bearing tokens. Failure path still emits
	// because the runner may have spent tokens before the error.
	if res.Model != "" && tokens > 0 {
		update.ModelUsages = []hermes.ModelUsage{{
			Model:                    res.Model,
			InputTokens:              res.InputTokens,
			UncachedInputTokens:      res.UncachedInputTokens,
			CacheReadInputTokens:     res.CacheReadInputTokens,
			CacheCreationInputTokens: res.CacheCreationInputTokens,
			OutputTokens:             res.OutputTokens,
			CostUSD:                  res.CostUSD,
		}}
		update.PhaseUsages = []hermes.PhaseUsage{{
			Phase:                    "executor",
			Model:                    res.Model,
			InputTokens:              res.InputTokens,
			UncachedInputTokens:      res.UncachedInputTokens,
			CacheReadInputTokens:     res.CacheReadInputTokens,
			CacheCreationInputTokens: res.CacheCreationInputTokens,
			OutputTokens:             res.OutputTokens,
			CostUSD:                  res.CostUSD,
		}}
		update.TokenUsageDelta = tokens
	}

	return NodeOutput{
		Updates:  []hermes.StateUpdate{update},
		NextStep: n.nextStepAfter(nextIdx, len(state.Plan)),
		Reason:   reason,
	}, nil
}

// advanceWithoutRunning emits a no-op StateUpdate that just bumps the
// pointer past an already-resolved sub-task. Used when the Walker
// re-enters the executor step on a sub-task that a previous commit had
// already moved to Done or Skipped.
func (n *ExecutorNode) advanceWithoutRunning(state hermes.HermesState, idx int, reason string) NodeOutput {
	nextIdx := idx + 1
	return NodeOutput{
		Updates:  []hermes.StateUpdate{{CurrentIdx: &nextIdx}},
		NextStep: n.nextStepAfter(nextIdx, len(state.Plan)),
		Reason:   reason,
	}
}

// nextStepAfter selects the post-executor RuntimeStep based on whether
// any sub-tasks remain and whether per-task review is enabled.
func (n *ExecutorNode) nextStepAfter(nextIdx, total int) hermes.RuntimeStep {
	if nextIdx < total {
		return hermes.RuntimeStepExecutor
	}
	if n.ReviewModeIsPerTask {
		return hermes.RuntimeStepReviewer
	}
	return hermes.RuntimeStepTerminal
}

// completedCount returns the running tally of Done sub-tasks in plan,
// matching what PlanExecuteEngine.run() tracks via the `completed`
// counter and passes to hermes.AppendResult.
func completedCount(plan []hermes.SubTask) int {
	c := 0
	for _, st := range plan {
		if st.Status == hermes.SubTaskDone {
			c++
		}
	}
	return c
}
