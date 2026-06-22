package graph

import (
	"context"
	"errors"
	"fmt"

	"claude-tg-agent/internal/app/hermes"
)

// SubTaskReviewer is the per-sub-task reviewer abstraction used by
// StrictReviewNode. Production wires a thin adapter over
// PlanExecuteEngine.runReview + ReviewDecisionFromStrictTags; tests use
// stubs. The Reviewer is expected to evaluate the sub-task identified
// by idx in state.Plan and return a verdict the Node can route on.
type SubTaskReviewer interface {
	ReviewSubTask(ctx context.Context, state hermes.HermesState, idx int) (SubTaskReviewResult, error)
}

// SubTaskReviewResult bundles the reviewer's verdict + structured
// feedback the Node uses to drive routing and (on retry) feed back to
// the Executor's runner.
type SubTaskReviewResult struct {
	// Verdict is the canonical strict-mode outcome:
	//   "block"   → reviewer rejected the result; retry if budget allows
	//   "pass"    → reviewer accepted; mark sub-task Done
	//   "partial" → reviewer accepted with caveats; mark sub-task Done
	//   "fail"    → terminal failure; mark sub-task Failed
	// Anything else is treated as "pass" defensively.
	Verdict string
	// Feedback is the human-readable reviewer note. When Verdict is
	// "block" the Node persists this into plan[idx].StrictRetryFeedback
	// so the Executor's next run can prepend it to the prompt.
	Feedback string
	// Score / BlockTags are passthroughs for telemetry — the Node does
	// not consult them for routing.
	Score     int
	BlockTags []string
	// Telemetry from the reviewer call (token usage, model attribution).
	Model        string
	InputTokens  int
	OutputTokens int
	CostUSD      float64
}

// StrictReviewNode reviews one sub-task per Walker iteration and routes
// based on the verdict + retry budget. Wired between ExecutorNode (which
// produces a candidate result) and either ExecutorNode (retry) or
// ExecutorNode advancing to the next sub-task.
//
// Routing rules:
//
//	verdict == "block" AND attempts < MaxRetriesPerSub
//	  → write StrictRetryFeedback into plan[idx]
//	  → keep plan[idx].Status = InProgress so ExecutorNode re-runs
//	  → route to RuntimeStepExecutor
//
//	verdict == "block" AND attempts ≥ MaxRetriesPerSub
//	  → mark plan[idx] Skipped with "PARTIAL\n<orig>\n\nReviewer
//	    feedback:\n<feedback>"
//	  → advance CurrentIdx
//	  → clear StrictRetryFeedback
//	  → route to RuntimeStepExecutor (next sub-task) or terminal
//
//	verdict == "fail"
//	  → mark plan[idx] Failed with reviewer feedback
//	  → advance CurrentIdx
//	  → route to RuntimeStepExecutor (next sub-task) or terminal
//
//	verdict == "pass" / "partial" / unknown
//	  → mark plan[idx] Done
//	  → clear StrictRetryFeedback
//	  → advance CurrentIdx
//	  → route to RuntimeStepExecutor (next sub-task) or terminal
type StrictReviewNode struct {
	// Reviewer is required.
	Reviewer SubTaskReviewer
	// MaxRetriesPerSub caps how many times one sub-task may be sent
	// back for retry. The Node compares against plan[idx].Attempts.
	// 0 means "no retries" — first block immediately becomes a partial
	// skip.
	MaxRetriesPerSub int
	// ReviewModeIsPerTask routes the final sub-task's NextStep to the
	// reviewer when true; otherwise advance terminates after the last
	// sub-task. Mirrors ExecutorNode.ReviewModeIsPerTask.
	ReviewModeIsPerTask bool
}

// Name implements Node.
func (n *StrictReviewNode) Name() hermes.RuntimeStep {
	return hermes.RuntimeStepStrictReview
}

// Handle implements Node.
func (n *StrictReviewNode) Handle(ctx context.Context, state hermes.HermesState) (NodeOutput, error) {
	if n == nil || n.Reviewer == nil {
		return NodeOutput{}, errors.New("graph: StrictReviewNode requires a Reviewer")
	}
	if len(state.Plan) == 0 {
		return NodeOutput{}, errors.New("graph: StrictReviewNode dispatched with empty plan")
	}
	// CurrentIdx is set by ExecutorNode before routing here. The
	// sub-task at idx is the candidate the reviewer should evaluate;
	// it is still InProgress (ExecutorNode under StrictReviewEnabled
	// does not finalise the status — that decision is StrictReview's).
	idx := state.CurrentIdx
	if idx < 0 || idx >= len(state.Plan) {
		return NodeOutput{}, fmt.Errorf("graph: StrictReviewNode CurrentIdx %d out of range (plan size %d)", idx, len(state.Plan))
	}

	res, err := n.Reviewer.ReviewSubTask(ctx, state, idx)
	if err != nil {
		// Reviewer-call failure is treated as accept-and-continue so a
		// transient reviewer outage does not gate the whole task. The
		// engine still surfaces the error via runtime events.
		plan := finaliseSubTaskAsDone(state.Plan, idx, "" /* keep existing Result */)
		return n.advanceWith(state, plan, "strict_review_reviewer_error_accept"), nil
	}

	switch res.Verdict {
	case "block":
		attempts := state.Plan[idx].Attempts
		if attempts < n.MaxRetriesPerSub {
			// Retry: write feedback and route back to executor.
			plan := append([]hermes.SubTask(nil), state.Plan...)
			plan[idx].StrictRetryFeedback = res.Feedback
			update := hermes.StateUpdate{Plan: plan}
			n.attachReviewerTelemetry(&update, res)
			return NodeOutput{
				Updates:  []hermes.StateUpdate{update},
				NextStep: hermes.RuntimeStepExecutor,
				Reason:   "strict_review_block_retry",
			}, nil
		}
		// Budget exhausted — partial skip.
		plan := append([]hermes.SubTask(nil), state.Plan...)
		original := plan[idx].Result
		plan[idx].Status = hermes.SubTaskSkipped
		plan[idx].Result = formatPartialResult(original, res.Feedback)
		plan[idx].StrictRetryFeedback = ""
		nextIdx := idx + 1
		update := hermes.StateUpdate{
			Plan:       plan,
			CurrentIdx: &nextIdx,
			SubTaskResults: []hermes.SubTaskResult{{
				SubTaskID:  plan[idx].ID,
				Index:      idx,
				Status:     plan[idx].Status,
				Result:     plan[idx].Result,
				TokensUsed: plan[idx].TokensUsed,
				Attempts:   plan[idx].Attempts,
			}},
		}
		n.attachReviewerTelemetry(&update, res)
		return NodeOutput{
			Updates:  []hermes.StateUpdate{update},
			NextStep: n.routeAfterAdvance(nextIdx, len(state.Plan)),
			Reason:   "strict_review_block_budget_exhausted",
		}, nil

	case "fail":
		plan := append([]hermes.SubTask(nil), state.Plan...)
		plan[idx].Status = hermes.SubTaskFailed
		if feedback := res.Feedback; feedback != "" {
			plan[idx].Result = feedback
		}
		plan[idx].StrictRetryFeedback = ""
		nextIdx := idx + 1
		update := hermes.StateUpdate{
			Plan:       plan,
			CurrentIdx: &nextIdx,
			SubTaskResults: []hermes.SubTaskResult{{
				SubTaskID:  plan[idx].ID,
				Index:      idx,
				Status:     plan[idx].Status,
				Result:     plan[idx].Result,
				TokensUsed: plan[idx].TokensUsed,
				Attempts:   plan[idx].Attempts,
			}},
		}
		n.attachReviewerTelemetry(&update, res)
		return NodeOutput{
			Updates:  []hermes.StateUpdate{update},
			NextStep: n.routeAfterAdvance(nextIdx, len(state.Plan)),
			Reason:   "strict_review_fail",
		}, nil

	default:
		// pass / partial / unknown → accept Done.
		plan := finaliseSubTaskAsDone(state.Plan, idx, "")
		out := n.advanceWith(state, plan, "strict_review_pass")
		// Attach reviewer telemetry to the existing update.
		if len(out.Updates) > 0 {
			n.attachReviewerTelemetry(&out.Updates[0], res)
		}
		return out, nil
	}
}

// finaliseSubTaskAsDone returns a cloned plan with plan[idx] marked Done
// and StrictRetryFeedback cleared. Result text is preserved (or
// overridden when overrideResult is non-empty).
func finaliseSubTaskAsDone(plan []hermes.SubTask, idx int, overrideResult string) []hermes.SubTask {
	out := append([]hermes.SubTask(nil), plan...)
	out[idx].Status = hermes.SubTaskDone
	out[idx].StrictRetryFeedback = ""
	if overrideResult != "" {
		out[idx].Result = overrideResult
	}
	return out
}

// advanceWith builds a NodeOutput that finalises a sub-task and routes
// to the next step. Used by the accept paths in StrictReviewNode; the
// retry path builds its own NodeOutput because it needs to keep
// CurrentIdx unchanged.
func (n *StrictReviewNode) advanceWith(state hermes.HermesState, plan []hermes.SubTask, reason string) NodeOutput {
	idx := state.CurrentIdx
	nextIdx := idx + 1
	update := hermes.StateUpdate{
		Plan:       plan,
		CurrentIdx: &nextIdx,
		SubTaskResults: []hermes.SubTaskResult{{
			SubTaskID:  plan[idx].ID,
			Index:      idx,
			Status:     plan[idx].Status,
			Result:     plan[idx].Result,
			TokensUsed: plan[idx].TokensUsed,
			Attempts:   plan[idx].Attempts,
		}},
	}
	// Append accumulated text on accept (mirrors ExecutorNode behavior
	// for non-strict path).
	if plan[idx].Status == hermes.SubTaskDone && plan[idx].Result != "" {
		updated, _ := hermes.AppendResult(state.Accumulated, plan[idx].Result, completedCount(plan))
		update.Accumulated = &updated
	}
	return NodeOutput{
		Updates:  []hermes.StateUpdate{update},
		NextStep: n.routeAfterAdvance(nextIdx, len(state.Plan)),
		Reason:   reason,
	}
}

// routeAfterAdvance picks the post-advance step. Same rule as
// ExecutorNode.nextStepAfter to keep routing consistent across nodes.
func (n *StrictReviewNode) routeAfterAdvance(nextIdx, total int) hermes.RuntimeStep {
	if nextIdx < total {
		return hermes.RuntimeStepExecutor
	}
	if n.ReviewModeIsPerTask {
		return hermes.RuntimeStepReviewer
	}
	return hermes.RuntimeStepTerminal
}

// attachReviewerTelemetry attaches reviewer-call ModelUsages /
// PhaseUsages / TokenUsageDelta to the update when the reviewer
// reported tokens. Mirrors how ExecutorNode + commitTelemetryBoundary
// surface telemetry from the executor side.
func (n *StrictReviewNode) attachReviewerTelemetry(update *hermes.StateUpdate, res SubTaskReviewResult) {
	tokens := res.InputTokens + res.OutputTokens
	if res.Model == "" || tokens <= 0 {
		return
	}
	update.ModelUsages = append(update.ModelUsages, hermes.ModelUsage{
		Model:               res.Model,
		InputTokens:         res.InputTokens,
		UncachedInputTokens: res.InputTokens,
		OutputTokens:        res.OutputTokens,
		CostUSD:             res.CostUSD,
	})
	update.PhaseUsages = append(update.PhaseUsages, hermes.PhaseUsage{
		Phase:               "reviewer_strict",
		Model:               res.Model,
		InputTokens:         res.InputTokens,
		UncachedInputTokens: res.InputTokens,
		OutputTokens:        res.OutputTokens,
		CostUSD:             res.CostUSD,
	})
	update.TokenUsageDelta += tokens
}

// formatPartialResult composes the "PARTIAL\n<orig>\n\nReviewer
// feedback:\n<fb>" block engine.runReview produces today, so dashboards
// that already grep for "PARTIAL" / "Reviewer feedback:" keep working
// during the migration.
func formatPartialResult(originalResult, reviewerFeedback string) string {
	out := "PARTIAL"
	if originalResult != "" {
		out += "\n" + originalResult
	}
	if reviewerFeedback != "" {
		out += "\n\nReviewer feedback:\n" + reviewerFeedback
	}
	return out
}
