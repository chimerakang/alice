package graph

import (
	"context"
	"errors"

	"claude-tg-agent/internal/app/hermes"
)

// TaskReviewer is the task-level reviewer abstraction used by
// ReviewerNode. Production wires a thin adapter over
// PlanExecuteEngine.runReview(ReviewModePerTask, ...); tests use
// stubs. The reviewer evaluates the whole completed plan, not a single
// sub-task (that is StrictReviewNode's job).
type TaskReviewer interface {
	ReviewTask(ctx context.Context, state hermes.HermesState) (TaskReviewResult, error)
}

// TaskReviewResult is the reviewer's verdict + telemetry. ReviewerNode
// routes on Verdict and Replan; all other fields are passthroughs for
// telemetry / engine-side notifications.
type TaskReviewResult struct {
	// Verdict is the canonical task-level outcome. Recognised values:
	// "pass" / "allow" → accept, route terminal
	// "fail"           → terminal failure, route terminal (engine side
	//                    decides whether to surface as failed task)
	// "partial"        → accept with caveats, route terminal
	// "block"          → reviewer rejected; engine recovery decides
	//                    whether to replan (see Replan flag)
	// Anything else is treated as "pass" defensively.
	Verdict string
	// Replan is the explicit "redo from planner" signal. Engine wiring
	// (γ6) sets this from RecoveryDecision when the recovery layer says
	// retry-with-replan; the Node honours it by routing to
	// RuntimeStepPlanner instead of terminal.
	Replan bool
	// Feedback is the reviewer's free-form text. Stored as-is for
	// telemetry; ReviewerNode does not parse it.
	Feedback     string
	OverallScore int
	// Model / token / cost telemetry mirrors SubTaskReviewResult so the
	// reducer-side ModelUsages / PhaseUsages aggregation stays consistent
	// with the strict-review path.
	Model        string
	InputTokens  int
	OutputTokens int
	CostUSD      float64
}

// ReviewerNode runs at the end of a plan attempt and routes based on
// the reviewer's task-level verdict. It is the graph counterpart to
// the runReview(ReviewModePerTask) call site at the top of the
// post-execution block in PlanExecuteEngine.run().
//
// Routing rules:
//
//	verdict == "block" AND Replan
//	  → route to RuntimeStepPlanner (replan attempt)
//
//	verdict == "fail"
//	  → mark Status=Failed, route terminal
//
//	otherwise (pass / allow / partial / unknown / block-without-replan)
//	  → route terminal (engine decides whether to flag block as failure)
//
// γ5 lands the Node in isolation. γ6 wires it as the ReviewModeIsPerTask
// terminal hop replacing the inline runReview call.
type ReviewerNode struct {
	// Reviewer is required.
	Reviewer TaskReviewer
}

// Name implements Node.
func (n *ReviewerNode) Name() hermes.RuntimeStep {
	return hermes.RuntimeStepReviewer
}

// Handle implements Node.
func (n *ReviewerNode) Handle(ctx context.Context, state hermes.HermesState) (NodeOutput, error) {
	if n == nil || n.Reviewer == nil {
		return NodeOutput{}, errors.New("graph: ReviewerNode requires a Reviewer")
	}

	res, err := n.Reviewer.ReviewTask(ctx, state)
	if err != nil {
		// Reviewer-call failure is treated as accept-and-continue so a
		// transient reviewer outage does not gate the whole task. Mirrors
		// StrictReviewNode's error-path semantics; engine side still
		// surfaces the error via runtime events.
		return NodeOutput{
			NextStep: hermes.RuntimeStepTerminal,
			Reason:   "reviewer_error_accept",
		}, nil
	}

	update := hermes.StateUpdate{}
	n.attachReviewerTelemetry(&update, res)

	switch res.Verdict {
	case "block":
		if res.Replan {
			out := NodeOutput{
				NextStep: hermes.RuntimeStepPlanner,
				Reason:   "reviewer_block_replan",
			}
			if hasTelemetry(update) {
				out.Updates = []hermes.StateUpdate{update}
			}
			return out, nil
		}
		out := NodeOutput{
			NextStep: hermes.RuntimeStepTerminal,
			Reason:   "reviewer_block_terminal",
		}
		if hasTelemetry(update) {
			out.Updates = []hermes.StateUpdate{update}
		}
		return out, nil

	case "fail":
		failed := hermes.TaskStatusFailed
		update.Status = &failed
		return NodeOutput{
			Updates:  []hermes.StateUpdate{update},
			NextStep: hermes.RuntimeStepTerminal,
			Reason:   "reviewer_fail",
		}, nil

	default:
		// pass / allow / partial / unknown → accept, terminal.
		out := NodeOutput{
			NextStep: hermes.RuntimeStepTerminal,
			Reason:   "reviewer_pass",
		}
		if hasTelemetry(update) {
			out.Updates = []hermes.StateUpdate{update}
		}
		return out, nil
	}
}

// attachReviewerTelemetry mirrors StrictReviewNode's helper: emit
// ModelUsages / PhaseUsages / TokenUsageDelta when the reviewer
// reported tokens, with phase="reviewer" so it lines up with the legacy
// commitTelemetryBoundary call in runReview.
func (n *ReviewerNode) attachReviewerTelemetry(update *hermes.StateUpdate, res TaskReviewResult) {
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
		Phase:               "reviewer",
		Model:               res.Model,
		InputTokens:         res.InputTokens,
		UncachedInputTokens: res.InputTokens,
		OutputTokens:        res.OutputTokens,
		CostUSD:             res.CostUSD,
	})
	update.TokenUsageDelta += tokens
}

// hasTelemetry reports whether an update carries any reviewer-emitted
// fields worth committing. Used to skip empty StateUpdates on the
// accept/terminal paths so the Walker does not record no-op writes.
func hasTelemetry(u hermes.StateUpdate) bool {
	return len(u.ModelUsages) > 0 || len(u.PhaseUsages) > 0 || u.TokenUsageDelta != 0
}
