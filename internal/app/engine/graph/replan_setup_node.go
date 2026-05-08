package graph

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"claude-tg-agent/internal/app/hermes"
)

// ReplanDecider is the abstract decision surface for the replan-setup
// step. Production wires an adapter over the engine package's
// buildPartialRetryPlan / buildReplanGoal / buildPartialReplanGoal so
// the existing partial-vs-full rule (sub-task score ≥ threshold AND
// status Done → preserve) keeps producing the same replan goal text and
// preserved-subtask set the legacy Run() did. Tests use stubs.
//
// The decider is called with the post-review state of the prior
// attempt. It must NOT consult engine internals beyond what HermesState
// already carries — the contract is "read state, return decision".
type ReplanDecider interface {
	DecideReplan(ctx context.Context, state hermes.HermesState) (ReplanDecision, error)
}

// ReplanDecision is the structured output ReplanSetupNode persists into
// HermesState.Replan. PlannerNode consumes it on the next attempt.
type ReplanDecision struct {
	// Goal is the augmented goal text. Required when Trigger != "".
	Goal string
	// Accumulated is the seed accumulated text. Empty string is a valid
	// "full reset" signal; non-empty means "preserve this prefix".
	Accumulated string
	// PreservedSubTasks is the list of high-score sub-tasks to merge in
	// front of the planner's new tasks. Empty for full retries.
	PreservedSubTasks []hermes.SubTask
	// AttemptIdx is the 1-based replan attempt counter. The decider
	// receives the prior attempt's state and is responsible for
	// computing the next attempt's index.
	AttemptIdx int
	// Trigger is "partial" / "full" — telemetry only.
	Trigger string
}

// mergePreservedSubTasks prepends preserved sub-tasks to a fresh
// planner output, renaming any colliding ids on the planner side so
// the executor's id-keyed lookups stay unique. Mirrors the engine's
// mergePartialRetryPlan rule: preserved tasks keep their identity,
// planner tasks with empty or duplicate ids get a synthetic
// "retry<attempt>-s<n>" id.
func mergePreservedSubTasks(preserved, replanned []hermes.SubTask, attemptIdx int) []hermes.SubTask {
	if len(preserved) == 0 {
		return replanned
	}
	merged := make([]hermes.SubTask, 0, len(preserved)+len(replanned))
	seen := make(map[string]bool, len(preserved)+len(replanned))
	for _, st := range preserved {
		merged = append(merged, st)
		if id := strings.TrimSpace(st.ID); id != "" {
			seen[id] = true
		}
	}
	for i, st := range replanned {
		id := strings.TrimSpace(st.ID)
		if id == "" || seen[id] {
			st.ID = fmt.Sprintf("retry%d-s%d", attemptIdx, i+1)
		}
		seen[st.ID] = true
		merged = append(merged, st)
	}
	return merged
}

// IsNoop reports whether the decision corresponds to "no replan
// needed". A zero-value decision (no goal, no preserved tasks, no
// trigger) is treated as a no-op so a decider that decides not to
// replan can return ReplanDecision{} without an error.
func (d ReplanDecision) IsNoop() bool {
	return strings.TrimSpace(d.Goal) == "" &&
		strings.TrimSpace(d.Accumulated) == "" &&
		len(d.PreservedSubTasks) == 0 &&
		strings.TrimSpace(d.Trigger) == ""
}

// ReplanSetupNode runs once before each replan attempt to decide
// whether the next planner pass should preserve high-score sub-tasks
// (partial retry) or reset accumulated state (full retry). The decision
// is materialised into state.Replan so the snapshot history records
// which branch was taken; PlannerNode consumes and clears the field on
// the next hop.
//
// Routing:
//
//	decider returns Trigger != ""  → install Replan, route planner
//	decider returns no-op decision → route planner unchanged
//	decider returns error          → propagate to Walker
//
// γ-style: ReplanSetupNode does not call the planner itself and does
// not loop. Each replan attempt is one snapshot transition through
// this node, recorded as a separate hop in the audit log.
type ReplanSetupNode struct {
	// Decider is required.
	Decider ReplanDecider
}

// Name implements Node.
func (n *ReplanSetupNode) Name() hermes.RuntimeStep {
	return hermes.RuntimeStepReplanSetup
}

// Handle implements Node.
func (n *ReplanSetupNode) Handle(ctx context.Context, state hermes.HermesState) (NodeOutput, error) {
	if n == nil || n.Decider == nil {
		return NodeOutput{}, errors.New("graph: ReplanSetupNode requires a Decider")
	}
	decision, err := n.Decider.DecideReplan(ctx, state)
	if err != nil {
		return NodeOutput{}, fmt.Errorf("graph: replan decider: %w", err)
	}
	if decision.IsNoop() {
		// Decider opted out — proceed straight to planner without an
		// installed Replan context. PlannerNode will fall back to
		// state.Goal / state.Accumulated.
		return NodeOutput{
			NextStep: hermes.RuntimeStepPlanner,
			Reason:   "replan_setup_noop",
		}, nil
	}
	rc := &hermes.ReplanContext{
		Goal:              decision.Goal,
		Accumulated:       decision.Accumulated,
		PreservedSubTasks: append([]hermes.SubTask(nil), decision.PreservedSubTasks...),
		AttemptIdx:        decision.AttemptIdx,
		Trigger:           decision.Trigger,
	}
	reason := "replan_setup_full"
	if decision.Trigger == "partial" {
		reason = "replan_setup_partial"
	}
	return NodeOutput{
		Updates: []hermes.StateUpdate{{
			Replan: rc,
		}},
		NextStep: hermes.RuntimeStepPlanner,
		Reason:   reason,
	}, nil
}
