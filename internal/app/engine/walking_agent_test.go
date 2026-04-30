package engine

import (
	"strings"
	"testing"

	"claude-tg-agent/internal/app/hermes"
)

// Issue #149: walking-agent slim prompt drops rules / goal / accumulated for
// round 2+ since the persistent Claude session already carries them. Only the
// new sub-task description (and reviewer feedback on retry) belongs in the
// new turn.
func TestBuildSubTaskGoalVariant_SlimPromptOmitsRulesAndAccumulated(t *testing.T) {
	subTask := hermes.SubTask{ID: "s2", Description: "edit the README"}
	got := buildSubTaskGoalVariant(
		"## Executor Rules\n- Be concise.",
		"Refactor the docs",
		"  [1] Inspected docs/README.md and noted out-of-date sections.",
		1, 3, subTask, "",
		true, // walkingContinuation
	)
	for _, banned := range []string{
		"Executor Rules",
		"Original goal:",
		"Refactor the docs",
		"Completed sub-task results so far",
		"Inspected docs/README.md",
	} {
		if strings.Contains(got, banned) {
			t.Fatalf("slim prompt unexpectedly contains %q:\n%s", banned, got)
		}
	}
	if !strings.Contains(got, "Now do sub-task (2/3)") {
		t.Fatalf("slim prompt missing sub-task marker:\n%s", got)
	}
	if !strings.Contains(got, "edit the README") {
		t.Fatalf("slim prompt missing description:\n%s", got)
	}
}

// Walking-mode retry must still inject reviewer feedback even on slim prompts —
// the model can't reconstruct the feedback from session history.
func TestBuildSubTaskGoalVariant_SlimPromptCarriesRetryFeedback(t *testing.T) {
	subTask := hermes.SubTask{ID: "s2", Description: "edit the README"}
	got := buildSubTaskGoalVariant(
		"rules", "goal", "acc", 1, 3, subTask,
		"Reviewer noted: missing input validation on line 42.",
		true,
	)
	if !strings.Contains(got, "Reviewer feedback to address before retrying:") {
		t.Fatalf("slim retry prompt missing reviewer header:\n%s", got)
	}
	if !strings.Contains(got, "missing input validation on line 42") {
		t.Fatalf("slim retry prompt missing feedback body:\n%s", got)
	}
}

// Cold form (walkingContinuation=false) is unchanged — same shape callers have
// relied on since pre-#149.
func TestBuildSubTaskGoalVariant_ColdPromptUnchanged(t *testing.T) {
	subTask := hermes.SubTask{ID: "s1", Description: "describe the auth flow"}
	got := buildSubTaskGoalVariant(
		"## Executor Rules\n- Be concise.",
		"Audit the auth module",
		"",
		0, 2, subTask, "",
		false,
	)
	for _, want := range []string{
		"Executor Rules",
		"Original goal:",
		"Audit the auth module",
		"Current sub-task (1/2):",
		"describe the auth flow",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("cold prompt missing %q:\n%s", want, got)
		}
	}
}

// PredictExecutorModel mirrors hermesExecutorRunner.pickModel: heavy when
// IsHeavySubTask() is true and HeavyExecutorModel differs from ExecutorModel,
// otherwise light.
func TestPredictExecutorModel(t *testing.T) {
	e := &PlanExecuteEngine{cfg: PlanExecuteConfig{
		ExecutorModel:      "claude-haiku-4-5",
		HeavyExecutorModel: "claude-sonnet-4-5",
	}}
	light := hermes.SubTask{ID: "s1", Description: "Read auth.go and summarise"}
	heavy := hermes.SubTask{ID: "s2", Description: "Edit auth.go to add JWT validation", ToolHints: []string{"Edit"}}
	if got := e.predictExecutorModel(light); got != "claude-haiku-4-5" {
		t.Fatalf("light sub-task model = %q, want claude-haiku-4-5", got)
	}
	if got := e.predictExecutorModel(heavy); got != "claude-sonnet-4-5" {
		t.Fatalf("heavy sub-task model = %q, want claude-sonnet-4-5", got)
	}

	// When ExecutorModel is empty the engine has no way to predict.
	bare := &PlanExecuteEngine{cfg: PlanExecuteConfig{}}
	if got := bare.predictExecutorModel(heavy); got != "" {
		t.Fatalf("uninitialised predictor = %q, want empty", got)
	}

	// HeavyExecutorModel == ExecutorModel disables the upgrade.
	flat := &PlanExecuteEngine{cfg: PlanExecuteConfig{
		ExecutorModel:      "claude-sonnet-4-5",
		HeavyExecutorModel: "claude-sonnet-4-5",
	}}
	if got := flat.predictExecutorModel(heavy); got != "claude-sonnet-4-5" {
		t.Fatalf("flat predictor heavy = %q, want claude-sonnet-4-5", got)
	}
}

func TestWalkingMaxContextTokens_OverrideAndDefault(t *testing.T) {
	overridden := &PlanExecuteEngine{cfg: PlanExecuteConfig{WalkingAgentMaxContextTokens: 50_000}}
	if got := overridden.walkingMaxContextTokens(); got != 50_000 {
		t.Fatalf("override = %d, want 50000", got)
	}
	def := &PlanExecuteEngine{cfg: PlanExecuteConfig{}}
	if got := def.walkingMaxContextTokens(); got != defaultWalkingMaxContextTokens {
		t.Fatalf("default = %d, want %d", got, defaultWalkingMaxContextTokens)
	}
}
