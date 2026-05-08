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

func TestRunViaGraph_ReplanLoopFromBlockReviewToPass(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	runner := &planExecuteRunner{}
	reporter := &planExecuteReporter{}

	plannerCalls := 0
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (hermes.CallPlanResult, error) {
		plannerCalls++
		return hermes.CallPlanResult{
			Text: "```json\n" +
				`[{"id":"s1","description":"first attempt","tool_hints":["Read"]},` +
				`{"id":"s2","description":"verify","tool_hints":["Bash"]}]` +
				"\n```",
			SessionID:    "planner-session",
			InputTokens:  10,
			OutputTokens: 5,
		}, nil
	}

	// First review: block (low score → retry); second review: pass.
	reviewPhase := &scriptedReviewPhase{
		results: []ReviewResult{
			{
				ReviewerModel: "reviewer-1",
				Verdict:       VerdictBlock,
				OverallScore:  20,
				Feedback:      "missing validation; needs replan",
				SubTaskResults: []ReviewSubTaskResult{
					{SubTaskID: "s1", Score: 15, Feedback: "shallow"},
					{SubTaskID: "s2", Score: 18, Feedback: "incomplete"},
				},
				InputTokens: 10, OutputTokens: 5,
			},
			{
				ReviewerModel: "reviewer-1",
				Verdict:       VerdictPass,
				OverallScore:  90,
				Feedback:      "looks good",
				SubTaskResults: []ReviewSubTaskResult{
					{SubTaskID: "s1", Score: 92, Feedback: "ok"},
					{SubTaskID: "s2", Score: 90, Feedback: "ok"},
				},
				InputTokens: 10, OutputTokens: 5,
			},
		},
	}

	engine := NewPlanExecuteEngine(PlanExecuteConfig{
		PlannerModel:          "planner-model",
		ProjectDir:            "/repo",
		ChatID:                42,
		MaxPlannerJSONRetries: 1,
		Budget:                hermes.TokenBudget{MaxTotalTokens: 1000},
		ReviewPhase:           reviewPhase,
		ReviewMode:            ReviewModePerTask,
		TaskRetry:             TaskRetryConfig{Enabled: true, MaxTaskRetries: 2, ScoreThreshold: 60},
	}, planFn, NewDirectEngine(runner), store, reporter)

	cc := NewChatContext(42, 0, "/repo")
	final, err := engine.RunViaGraph(context.Background(), "ship a small feature", cc)
	if err != nil {
		t.Fatalf("RunViaGraph: %v", err)
	}
	if final.NextStep != hermes.RuntimeStepTerminal {
		t.Fatalf("final NextStep = %q, want terminal", final.NextStep)
	}
	if plannerCalls != 2 {
		t.Errorf("planner calls = %d, want 2 (initial + replan)", plannerCalls)
	}
	if reviewPhase.calls != 2 {
		t.Errorf("review calls = %d, want 2 (initial block, replan pass)", reviewPhase.calls)
	}

	history, err := store.ListSnapshotHistory(final.TaskID)
	if err != nil {
		t.Fatalf("ListSnapshotHistory: %v", err)
	}
	sawReplanSetup := false
	plannerHops := 0
	for _, snap := range history {
		switch snap.SourceNode {
		case hermes.RuntimeStepReplanSetup:
			sawReplanSetup = true
		case hermes.RuntimeStepPlanner:
			plannerHops++
		}
	}
	if !sawReplanSetup {
		t.Errorf("snapshot history never went through replan_setup; SourceNodes: %v", history)
	}
	if plannerHops < 2 {
		t.Errorf("expected at least 2 planner hops in history, got %d", plannerHops)
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
		hermes.RuntimeStepReplanSetup,
		hermes.RuntimeStepApproval,
	} {
		if _, ok := registry.Lookup(step); !ok {
			t.Errorf("registry missing handler for %q", step)
		}
	}
}
