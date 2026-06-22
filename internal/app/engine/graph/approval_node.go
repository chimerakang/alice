package graph

import (
	"context"
	"errors"
	"fmt"

	"claude-tg-agent/internal/app/hermes"
)

// ApprovalNode is the human-in-the-loop pause step. It implements the
// LangGraph-style "engine exits, telegram resumes" pattern that #169
// β4 deferred to phase γ:
//
//   - On first visit (state.Interrupt is set, no resolution yet) the
//     Node returns NodeOutput.Halt = true. The Walker commits the same
//     interrupt forward (no state change) and exits. The engine
//     goroutine returns; the Telegram inline button stays live.
//
//   - When the operator clicks retry / skip / abort, the click
//     handler calls hermes.SQLiteTaskStore.ApplyInterruptResolution to
//     persist the decision into the snapshot — Skip mutates the plan +
//     advances CurrentIdx + clears Interrupt; Retry just clears
//     Interrupt; Abort marks status=Failed + terminal. After the
//     resolution lands, the click handler re-enters the Walker.
//
//   - On resume the Walker dispatches ApprovalNode again. The Node now
//     sees state.Interrupt == nil (cleared by the resolution) and
//     routes to the next logical step based on the post-resolution
//     state: terminal if status went Failed (abort path), executor
//     otherwise (skip already advanced; retry kept the same idx).
//
// γ4 lands the Node + routing logic; γ6 wires PlanExecuteEngine to
// stop blocking on its in-process channel and use this pattern
// instead.
type ApprovalNode struct {
	// ReviewModeIsPerTask controls routing when an abort resolution
	// leaves status non-terminal but no more sub-tasks remain. Mirrors
	// ExecutorNode.ReviewModeIsPerTask.
	ReviewModeIsPerTask bool
}

// Name implements Node.
func (n *ApprovalNode) Name() hermes.RuntimeStep {
	return hermes.RuntimeStepApproval
}

// Handle implements Node.
//
// First visit: Interrupt is set. Halt + same-step routing tells the
// Walker to commit a no-op snapshot and exit. The engine goroutine
// returns control; future resume happens via an external trigger
// (Telegram callback calling Walker.Run again).
//
// Resume: Interrupt was cleared by ApplyInterruptResolution. Read the
// post-resolution state to decide where to go next.
func (n *ApprovalNode) Handle(_ context.Context, state hermes.HermesState) (NodeOutput, error) {
	if n == nil {
		return NodeOutput{}, errors.New("graph: nil ApprovalNode")
	}
	// Resolved already? — the resolution path (β2 ApplyInterruptResolution)
	// clears Interrupt in the snapshot. Route on the post-resolution state.
	if state.Interrupt == nil {
		return n.routeAfterResolution(state)
	}

	// First visit: pause is unresolved. Walker should exit so an
	// external trigger (telegram click) can apply the resolution. The
	// commit is essentially a no-op — same Interrupt, same NextStep —
	// but recording it keeps the snapshot history honest about the
	// pause boundary.
	return NodeOutput{
		Updates:  nil,
		NextStep: hermes.RuntimeStepApproval,
		Reason:   "approval_pending",
		Halt:     true,
	}, nil
}

// routeAfterResolution decides where the Walker goes once the operator
// has resolved a pause. Possible final states left by
// ApplyInterruptResolution:
//
//	abort → state.Status == TaskStatusFailed → RuntimeStepTerminal
//	skip  → plan[idx]=Skipped + CurrentIdx advanced → RuntimeStepExecutor
//	retry → CurrentIdx unchanged + plan[idx] still in_progress
//	        → RuntimeStepExecutor (re-run)
func (n *ApprovalNode) routeAfterResolution(state hermes.HermesState) (NodeOutput, error) {
	if state.Status == hermes.TaskStatusFailed {
		return NodeOutput{
			NextStep: hermes.RuntimeStepTerminal,
			Reason:   "approval_resolved_abort",
		}, nil
	}
	if len(state.Plan) == 0 {
		return NodeOutput{}, fmt.Errorf("graph: ApprovalNode resume with empty plan")
	}
	idx := state.CurrentIdx
	if idx < 0 || idx > len(state.Plan) {
		return NodeOutput{}, fmt.Errorf("graph: ApprovalNode CurrentIdx %d out of range (plan size %d)", idx, len(state.Plan))
	}
	// All sub-tasks consumed (skip on the last sub-task advances idx
	// past the end) → terminal or reviewer.
	if idx >= len(state.Plan) {
		next := hermes.RuntimeStepTerminal
		if n.ReviewModeIsPerTask {
			next = hermes.RuntimeStepReviewer
		}
		return NodeOutput{
			NextStep: next,
			Reason:   "approval_resolved_plan_complete",
		}, nil
	}
	// Otherwise hand back to the executor at the current idx. ExecutorNode
	// will re-run an in_progress sub-task (retry semantics) or skip a
	// Done/Skipped one and advance further.
	return NodeOutput{
		NextStep: hermes.RuntimeStepExecutor,
		Reason:   "approval_resolved_continue",
	}, nil
}
