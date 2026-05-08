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

// recordingPlanFn captures the goal text the planner was called with,
// so tests can assert PlannerNode used the replan-augmented goal
// instead of state.Goal.
func recordingPlanFn(planJSON string, captured *string) hermes.CallPlanFunc {
	return func(ctx context.Context, message, projectDir, prevSession string) (hermes.CallPlanResult, error) {
		if captured != nil {
			*captured = message
		}
		return hermes.CallPlanResult{
			Text:         "```json\n" + planJSON + "\n```",
			InputTokens:  10,
			OutputTokens: 5,
		}, nil
	}
}

func TestPlannerNode_ReplanContextOverridesGoalAndMergesPreserved(t *testing.T) {
	var captured string
	planJSON := `[{"id":"new1","description":"replanned step","tool_hints":["Edit"]}]`
	planner := hermes.NewPlannerSession(recordingPlanFn(planJSON, &captured), 1, "")
	node := &PlannerNode{Planner: planner, MaxSubTasks: 15}

	state := hermes.HermesState{
		TaskID:      "task-replan",
		Goal:        "ORIGINAL_GOAL_SHOULD_NOT_REACH_PLANNER",
		ProjectDir:  "/repo",
		Accumulated: "STALE_ACCUMULATED",
		Replan: &hermes.ReplanContext{
			Goal:        "AUGMENTED_REPLAN_GOAL",
			Accumulated: "PRESERVED_PREFIX",
			PreservedSubTasks: []hermes.SubTask{
				{ID: "s1", Description: "kept", Status: hermes.SubTaskDone, Result: "ok"},
			},
			AttemptIdx: 1,
			Trigger:    "partial",
		},
	}
	out, err := node.Handle(context.Background(), state)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !strings.Contains(captured, "AUGMENTED_REPLAN_GOAL") {
		t.Errorf("planner received wrong goal: %q", captured)
	}
	if strings.Contains(captured, "ORIGINAL_GOAL_SHOULD_NOT_REACH_PLANNER") {
		t.Errorf("planner saw original goal: %q", captured)
	}
	u := out.Updates[0]
	if len(u.Plan) != 2 {
		t.Fatalf("merged plan size = %d, want 2 (1 preserved + 1 replanned)", len(u.Plan))
	}
	if u.Plan[0].ID != "s1" {
		t.Errorf("preserved should be first: %+v", u.Plan)
	}
	if u.Accumulated == nil || *u.Accumulated != "PRESERVED_PREFIX" {
		t.Errorf("Accumulated = %+v, want PRESERVED_PREFIX", u.Accumulated)
	}
	if !u.ClearReplan {
		t.Errorf("ClearReplan should be set so context does not bleed into next attempt")
	}
}

func TestPlannerNode_FullReplanResetsAccumulatedToEmpty(t *testing.T) {
	planJSON := `[{"id":"r1","description":"redo","tool_hints":["Edit"]}]`
	planner := hermes.NewPlannerSession(scriptedPlanFn(planJSON, ""), 1, "")
	node := &PlannerNode{Planner: planner, MaxSubTasks: 15}

	state := hermes.HermesState{
		TaskID:      "task-full-replan",
		Goal:        "orig",
		ProjectDir:  "/repo",
		Accumulated: "stale stuff that must be reset",
		Replan: &hermes.ReplanContext{
			Goal:        "augmented full replan",
			Accumulated: "",
			AttemptIdx:  1,
			Trigger:     "full",
		},
	}
	out, err := node.Handle(context.Background(), state)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	u := out.Updates[0]
	if u.Accumulated == nil || *u.Accumulated != "" {
		t.Errorf("expected empty Accumulated, got: %+v", u.Accumulated)
	}
	if !u.ClearReplan {
		t.Errorf("ClearReplan should be set")
	}
	if len(u.Plan) != 1 || u.Plan[0].ID != "r1" {
		t.Errorf("plan: %+v", u.Plan)
	}
}

func TestPlannerNode_NilReplanLeavesGoalAndAccumulatedAlone(t *testing.T) {
	var captured string
	planJSON := `[{"id":"s1","description":"do it","tool_hints":["Read"]}]`
	planner := hermes.NewPlannerSession(recordingPlanFn(planJSON, &captured), 1, "")
	node := &PlannerNode{Planner: planner, MaxSubTasks: 15}

	out, err := node.Handle(context.Background(), hermes.HermesState{
		TaskID: "t", Goal: "ORIG_GOAL", ProjectDir: "/repo", Accumulated: "keep me",
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !strings.Contains(captured, "ORIG_GOAL") {
		t.Errorf("planner did not receive original goal: %q", captured)
	}
	u := out.Updates[0]
	if u.Accumulated != nil {
		t.Errorf("Accumulated should not be touched when Replan is nil: %+v", u.Accumulated)
	}
	if u.ClearReplan {
		t.Errorf("ClearReplan should be false when Replan was already nil")
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
