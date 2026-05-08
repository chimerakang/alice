package graph

import (
	"context"
	"fmt"

	"claude-tg-agent/internal/app/hermes"
)

// PlannerNode wraps the existing hermes.PlannerSession.Plan call and
// returns a NodeOutput that the Walker commits as the plan_ready
// snapshot. Equivalent to the planner block at the top of every attempt
// in PlanExecuteEngine.run() before #169 phase γ:
//
//   - call planner.Plan(state.Goal, state.ProjectDir)
//   - apply complexity gate (≤ 15 sub-tasks)
//   - emit plan + status=executing + currentIdx=0 + telemetry as
//     reducer-friendly StateUpdate
//   - route to RuntimeStepExecutor next
//
// What stays in the engine wiring (not this Node):
//
//   - replan goal augmentation (buildReplanGoal / buildPartialReplanGoal)
//   - reporter.OnPlanReady, onPlanReady issue hooks (Telegram side
//     effects do not belong on the durable execution path)
//   - handlePlanningError typed-error routing — γ2 surfaces errors via
//     the Node return; the engine still owns ErrPlannerEmptyPlan vs
//     ErrPlannerJSONFailed user-visible reporting
//
// γ2 lands the Node in isolation (tests + dead-code in production).
// γ6 will replace the inline planner block with a Walker dispatch onto
// this Node.
type PlannerNode struct {
	// Planner is the shared PlannerSession that owns the LLM session
	// resume ID and JSON / quality retry budget. Required.
	Planner *hermes.PlannerSession
	// PlannerModel is the model name used for telemetry attribution.
	// Required when state.PhaseUsages should split by model.
	PlannerModel string
	// MaxSubTasks caps the planner's accepted sub-task count. The
	// historical limit is 15 — set 0 to disable.
	MaxSubTasks int
}

// PlannerComplexityViolationError is returned when the planner produces
// more sub-tasks than the configured complexity gate allows. Surfacing
// it as a typed error lets the engine distinguish from genuine planner
// failures when migration progresses.
type PlannerComplexityViolationError struct {
	Got int
	Max int
}

func (e *PlannerComplexityViolationError) Error() string {
	return fmt.Sprintf("planner produced %d sub-tasks (max %d)", e.Got, e.Max)
}

// Name implements Node.
func (n *PlannerNode) Name() hermes.RuntimeStep {
	return hermes.RuntimeStepPlanner
}

// Handle implements Node. Calls the planner, applies the complexity
// gate, and packs the resulting plan + telemetry into a single
// StateUpdate. Errors from the planner (JSON failure, empty plan after
// retries, plan-quality gate exhaustion) propagate to the Walker
// unchanged.
func (n *PlannerNode) Handle(ctx context.Context, state hermes.HermesState) (NodeOutput, error) {
	if n == nil || n.Planner == nil {
		return NodeOutput{}, fmt.Errorf("graph: PlannerNode requires a Planner")
	}
	tasks, planIn, planOut, planCost, err := n.Planner.Plan(ctx, state.Goal, state.ProjectDir)
	if err != nil {
		return NodeOutput{}, err
	}
	if n.MaxSubTasks > 0 && len(tasks) > n.MaxSubTasks {
		return NodeOutput{}, &PlannerComplexityViolationError{Got: len(tasks), Max: n.MaxSubTasks}
	}

	status := hermes.TaskStatusExecuting
	idx := 0
	update := hermes.StateUpdate{
		Status:     &status,
		CurrentIdx: &idx,
		Plan:       append([]hermes.SubTask(nil), tasks...),
	}

	if n.PlannerModel != "" && (planIn > 0 || planOut > 0 || planCost != 0) {
		update.ModelUsages = []hermes.ModelUsage{{
			Model:               n.PlannerModel,
			InputTokens:         planIn,
			UncachedInputTokens: planIn,
			OutputTokens:        planOut,
			CostUSD:             planCost,
		}}
		update.PhaseUsages = []hermes.PhaseUsage{{
			Phase:               "planner",
			Model:               n.PlannerModel,
			InputTokens:         planIn,
			UncachedInputTokens: planIn,
			OutputTokens:        planOut,
			CostUSD:             planCost,
		}}
		update.TokenUsageDelta = planIn + planOut
	}

	if sid := n.Planner.SessionID(); sid != "" {
		// Take address of a local copy so the snapshot keeps a stable
		// value even if the Planner's session rotates later.
		sessionID := sid
		update.PlannerSessionID = &sessionID
	}

	return NodeOutput{
		Updates:  []hermes.StateUpdate{update},
		NextStep: hermes.RuntimeStepExecutor,
		Reason:   "plan_ready",
	}, nil
}
