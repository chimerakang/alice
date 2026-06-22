package graph

import (
	"context"

	"claude-tg-agent/internal/app/hermes"
)

// TerminalNode marks the task as Done and routes to RuntimeStepTerminal.
// It is the simplest possible Node and exists so γ slice 1 can exercise
// the dispatcher end-to-end without depending on planner/executor logic.
//
// Real callers will typically commit a TerminalBoundary directly rather
// than route through the graph, but TerminalNode is registered so that
// any future Node that returns NextStep=RuntimeStepTerminal lands here
// for a consistent metadata trail.
type TerminalNode struct {
	// Reason is used as the snapshot metadata reason on the final
	// commit. Defaults to "graph_terminal" when empty.
	Reason string
}

// Name implements Node.
func (n TerminalNode) Name() hermes.RuntimeStep {
	return hermes.RuntimeStepTerminal
}

// Handle implements Node. The walker treats RuntimeStepTerminal as a
// stopping condition and never actually calls Handle on it during normal
// dispatch — but registering one keeps the registry complete and lets
// callers use ForceTerminal as a deliberate "end now" routing target.
func (n TerminalNode) Handle(_ context.Context, state hermes.HermesState) (NodeOutput, error) {
	status := hermes.TaskStatusDone
	reason := n.Reason
	if reason == "" {
		reason = "graph_terminal"
	}
	return NodeOutput{
		Updates:  []hermes.StateUpdate{{Status: &status}},
		NextStep: hermes.RuntimeStepTerminal,
		Reason:   reason,
	}, nil
}
