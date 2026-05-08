package graph

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"claude-tg-agent/internal/app/hermes"
)

// scriptedPlanFn returns a CallPlanFunc that emits the configured plan
// JSON wrapped in fenced markdown so the planner's parser accepts it.
// Used by PlannerNode tests to avoid spinning up a real LLM CLI.
func scriptedPlanFn(planJSON string, sessionID string) hermes.CallPlanFunc {
	return func(ctx context.Context, message, projectDir, prevSession string) (hermes.CallPlanResult, error) {
		return hermes.CallPlanResult{
			Text:         "```json\n" + planJSON + "\n```",
			SessionID:    sessionID,
			InputTokens:  120,
			OutputTokens: 80,
		}, nil
	}
}

func TestPlannerNode_HandleEmitsPlanReadyUpdate(t *testing.T) {
	planJSON := `[{"id":"s1","description":"prep","tool_hints":["Read","Bash"]},{"id":"s2","description":"verify","tool_hints":["Bash"]}]`
	planner := hermes.NewPlannerSession(scriptedPlanFn(planJSON, "session-7"), 1, "")

	node := &PlannerNode{
		Planner:      planner,
		PlannerModel: "claude-sonnet-4-6",
		MaxSubTasks:  15,
	}

	state := hermes.HermesState{
		TaskID:     "task-planner",
		ChatID:     42,
		Goal:       "implement complex feature",
		ProjectDir: "/repo",
	}
	out, err := node.Handle(context.Background(), state)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if out.NextStep != hermes.RuntimeStepExecutor {
		t.Errorf("NextStep = %q, want executor", out.NextStep)
	}
	if out.Reason != "plan_ready" {
		t.Errorf("Reason = %q, want plan_ready", out.Reason)
	}
	if len(out.Updates) != 1 {
		t.Fatalf("Updates = %d, want 1", len(out.Updates))
	}

	u := out.Updates[0]
	if u.Status == nil || *u.Status != hermes.TaskStatusExecuting {
		t.Errorf("status = %+v, want executing", u.Status)
	}
	if u.CurrentIdx == nil || *u.CurrentIdx != 0 {
		t.Errorf("currentIdx = %+v, want 0", u.CurrentIdx)
	}
	if len(u.Plan) != 2 || u.Plan[0].ID != "s1" || u.Plan[1].ID != "s2" {
		t.Errorf("plan mismatch: %+v", u.Plan)
	}
	if u.PlannerSessionID == nil || *u.PlannerSessionID != "session-7" {
		t.Errorf("planner session id = %+v, want session-7", u.PlannerSessionID)
	}
	if len(u.ModelUsages) != 1 || u.ModelUsages[0].Model != "claude-sonnet-4-6" {
		t.Errorf("model usage = %+v", u.ModelUsages)
	}
	if u.ModelUsages[0].UncachedInputTokens != 120 || u.ModelUsages[0].OutputTokens != 80 {
		t.Errorf("model usage tokens wrong: %+v", u.ModelUsages[0])
	}
	if len(u.PhaseUsages) != 1 || u.PhaseUsages[0].Phase != "planner" {
		t.Errorf("phase usage = %+v", u.PhaseUsages)
	}
	if u.TokenUsageDelta != 200 {
		t.Errorf("TokenUsageDelta = %d, want 200", u.TokenUsageDelta)
	}
}

func TestPlannerNode_HandleEnforcesComplexityGate(t *testing.T) {
	// Build a 16-task plan with diverse leading verbs so PlannerSession's
	// own granularity gate accepts the plan; PlannerNode's complexity
	// gate (max 15) is what we want to trip here.
	verbs := []string{"implement", "verify", "refactor", "document"}
	var sb strings.Builder
	sb.WriteString("[")
	for i := 0; i < 16; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"id":"s%02d","description":"%s component %d at module M%d","tool_hints":["Bash"]}`,
			i, verbs[i%len(verbs)], i, i)
	}
	sb.WriteString("]")
	planner := hermes.NewPlannerSession(scriptedPlanFn(sb.String(), ""), 0, "")

	node := &PlannerNode{
		Planner:      planner,
		PlannerModel: "x",
		MaxSubTasks:  15,
	}

	_, err := node.Handle(context.Background(), hermes.HermesState{
		Goal: "implement complex feature with many components", ProjectDir: "/repo",
	})
	var cv *PlannerComplexityViolationError
	if !errors.As(err, &cv) {
		t.Fatalf("err = %v, want PlannerComplexityViolationError", err)
	}
	if cv.Got != 16 || cv.Max != 15 {
		t.Errorf("violation = %+v", cv)
	}
}

func TestPlannerNode_PropagatesPlannerError(t *testing.T) {
	failingPlanFn := func(ctx context.Context, message, projectDir, prevSession string) (hermes.CallPlanResult, error) {
		return hermes.CallPlanResult{Text: "garbage non-json"}, nil
	}
	planner := hermes.NewPlannerSession(failingPlanFn, 0, "")

	node := &PlannerNode{
		Planner:      planner,
		PlannerModel: "x",
	}

	_, err := node.Handle(context.Background(), hermes.HermesState{
		Goal: "x", ProjectDir: "/repo",
	})
	if err == nil {
		t.Fatal("expected planner JSON error")
	}
}

func TestPlannerNode_OmitsTelemetryWhenModelEmpty(t *testing.T) {
	planJSON := `[{"id":"s1","description":"only","tool_hints":["Bash"]}]`
	planner := hermes.NewPlannerSession(scriptedPlanFn(planJSON, "sid"), 1, "")

	node := &PlannerNode{
		Planner: planner,
		// PlannerModel intentionally empty: telemetry attribution is
		// opt-in via the field.
	}

	out, err := node.Handle(context.Background(), hermes.HermesState{
		Goal: "x", ProjectDir: "/repo",
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	u := out.Updates[0]
	if len(u.ModelUsages) != 0 || len(u.PhaseUsages) != 0 || u.TokenUsageDelta != 0 {
		t.Errorf("expected no telemetry when PlannerModel empty: model=%v phase=%v delta=%d", u.ModelUsages, u.PhaseUsages, u.TokenUsageDelta)
	}
	if u.PlannerSessionID == nil || *u.PlannerSessionID != "sid" {
		t.Errorf("session id should still be carried even without telemetry: %+v", u.PlannerSessionID)
	}
}

func TestPlannerNode_NameMatchesRuntimeStepPlanner(t *testing.T) {
	node := &PlannerNode{}
	if got := node.Name(); got != hermes.RuntimeStepPlanner {
		t.Errorf("Name() = %q, want %q", got, hermes.RuntimeStepPlanner)
	}
}

func TestPlannerNode_NilPlannerErrors(t *testing.T) {
	node := &PlannerNode{}
	_, err := node.Handle(context.Background(), hermes.HermesState{})
	if err == nil {
		t.Fatal("expected error for nil Planner")
	}
}
