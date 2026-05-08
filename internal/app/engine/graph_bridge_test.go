package engine

import (
	"context"
	"strings"
	"testing"

	"claude-tg-agent/internal/app/hermes"
)

// graph_bridge_test.go is the γ6 integration test: it drives RunViaGraph
// against a MemoryTaskStore + stubbed planner / direct runner end-to-end
// and asserts the snapshot history records each Walker hop.

func TestRunViaGraph_DrivesPlannerExecutorTerminal(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	runner := &planExecuteRunner{}
	reporter := &planExecuteReporter{}
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (hermes.CallPlanResult, error) {
		return hermes.CallPlanResult{
			Text: "```json\n" +
				`[{"id":"s1","description":"read context","tool_hints":["Read"]},` +
				`{"id":"s2","description":"edit code","tool_hints":["Edit"]}]` +
				"\n```",
			SessionID:    "planner-session",
			InputTokens:  11,
			OutputTokens: 7,
		}, nil
	}

	engine := NewPlanExecuteEngine(PlanExecuteConfig{
		PlannerModel:          "planner-model",
		ProjectDir:            "/repo",
		ChatID:                42,
		MaxPlannerJSONRetries: 1,
		Budget:                hermes.TokenBudget{MaxTotalTokens: 1000},
		DisableReview:         true,
	}, planFn, NewDirectEngine(runner), store, reporter)

	cc := NewChatContext(42, 0, "/repo")
	final, err := engine.RunViaGraph(context.Background(), "ship a small feature", cc)
	if err != nil {
		t.Fatalf("RunViaGraph: %v", err)
	}
	if final.NextStep != hermes.RuntimeStepTerminal {
		t.Fatalf("final NextStep = %q, want terminal", final.NextStep)
	}
	if len(final.State.Plan) != 2 {
		t.Fatalf("plan length = %d, want 2", len(final.State.Plan))
	}
	for i, sub := range final.State.Plan {
		if sub.Status != hermes.SubTaskDone {
			t.Fatalf("plan[%d].Status = %q, want done", i, sub.Status)
		}
	}
	if len(runner.prompts) != 2 {
		t.Fatalf("direct runner calls = %d, want 2", len(runner.prompts))
	}
	if !strings.Contains(runner.prompts[1], "Completed sub-task results so far") {
		t.Fatalf("second prompt did not include accumulated context:\n%s", runner.prompts[1])
	}

	taskID := final.TaskID
	history, err := store.ListSnapshotHistory(taskID)
	if err != nil {
		t.Fatalf("ListSnapshotHistory: %v", err)
	}
	if len(history) < 3 {
		t.Fatalf("snapshot history length = %d, want at least seed/plan/terminal", len(history))
	}
	gotSteps := make([]hermes.RuntimeStep, 0, len(history))
	for _, snap := range history {
		gotSteps = append(gotSteps, snap.NextStep)
	}
	terminalIdx := -1
	for i, step := range gotSteps {
		if step == hermes.RuntimeStepTerminal {
			terminalIdx = i
			break
		}
	}
	if terminalIdx == -1 {
		t.Fatalf("snapshot history never reached terminal: %#v", gotSteps)
	}
	plannerSeen := false
	executorSeen := false
	for _, step := range gotSteps[:terminalIdx+1] {
		switch step {
		case hermes.RuntimeStepPlanner:
			plannerSeen = true
		case hermes.RuntimeStepExecutor:
			executorSeen = true
		}
	}
	if !plannerSeen || !executorSeen {
		t.Fatalf("walker did not cover planner+executor: %#v", gotSteps)
	}
}

func TestRunViaGraph_RegistryHasAllNodes(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	runner := &planExecuteRunner{}
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (hermes.CallPlanResult, error) {
		return hermes.CallPlanResult{Text: "[]"}, nil
	}
	engine := NewPlanExecuteEngine(PlanExecuteConfig{ProjectDir: "/repo", ChatID: 1},
		planFn, NewDirectEngine(runner), store, &planExecuteReporter{})

	registry := engine.BuildGraphRegistry(NewChatContext(1, 0, "/repo"))
	for _, step := range []hermes.RuntimeStep{
		hermes.RuntimeStepPlanner,
		hermes.RuntimeStepExecutor,
		hermes.RuntimeStepStrictReview,
		hermes.RuntimeStepReviewer,
		hermes.RuntimeStepApproval,
	} {
		if _, ok := registry.Lookup(step); !ok {
			t.Errorf("registry missing handler for %q", step)
		}
	}
}
