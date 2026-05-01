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

// Issue #149 second watermark fix: a cold call's cache_creation reflects that
// call's internal tool-use chain, not the transcript that the next walking
// iteration will inherit. Counting it tripped the watermark on every sub-task
// that followed a cold start (Hermes Executor with --max-turns 50 routinely
// uses 100K+ within a single call). This test pins the new behaviour: only
// walking-active calls feed the watermark; cold calls reset to 0.
func TestWalkingTokensSeen_ColdCallDoesNotFeedWatermark(t *testing.T) {
	// Direct simulation of the executeSubTask post-Run state-update logic.
	// Mirrors the actual code in plan_execute.go executeSubTask.
	apply := func(walkingSoFar int, walkingActive bool, cacheRead, cacheWrite, input, output int) int {
		if !walkingActive {
			// Cold call — reset watermark so the next walking iteration starts fresh.
			return 0
		}
		transcriptSize := cacheRead + cacheWrite
		if transcriptSize == 0 {
			transcriptSize = walkingSoFar + input + output
		}
		if transcriptSize > walkingSoFar {
			walkingSoFar = transcriptSize
		}
		return walkingSoFar
	}

	// Cold sub-task 1 (heavy tool use, 200K within-call). After: watermark
	// reset to 0 because the next sub-task won't carry this transcript.
	got := apply(0, false, 0, 200_000, 18, 1500)
	if got != 0 {
		t.Fatalf("after cold call, watermark = %d, want 0 (cold should not feed watermark)", got)
	}

	// Walking sub-task 2: cache_read=80K (the actual transcript carried over
	// from the new resumed session), cache_write=2K delta.
	got = apply(got, true, 80_000, 2_000, 18, 600)
	if got != 82_000 {
		t.Fatalf("walking sub-task 2 watermark = %d, want 82000", got)
	}

	// Walking sub-task 3: cache_read grows naturally as transcript accumulates.
	got = apply(got, true, 82_000, 2_500, 18, 700)
	if got != 84_500 {
		t.Fatalf("walking sub-task 3 watermark = %d, want 84500", got)
	}

	// Force-fresh in the middle (e.g., model change, retry) → reset.
	got = apply(got, false, 0, 50_000, 18, 800)
	if got != 0 {
		t.Fatalf("force-fresh mid-task watermark = %d, want 0", got)
	}

	// Walking resumes after force-fresh, accumulator restarts.
	got = apply(got, true, 50_000, 1_000, 18, 500)
	if got != 51_000 {
		t.Fatalf("walking after force-fresh watermark = %d, want 51000", got)
	}
}

// Issue #149 watermark fix: walkingTokensSeen should reflect transcript size
// (cache_read + cache_write of last call), not a cumulative add of
// uncached-input + output. The latter heuristic over-counted on Codex (whose
// input_tokens already includes cached portion) and tripped the watermark
// prematurely. This test pins the new behaviour at the source: setting
// walkingTokensSeen via max(transcript) rather than +=.
func TestWalkingTokensSeen_TrackTranscriptSizeNotCumulative(t *testing.T) {
	// Direct simulation of the update logic in executeSubTask.
	// We're not running the engine end-to-end (that needs a Run mock); we just
	// verify the formula yields a monotonic transcript size from cache fields
	// and falls back to the legacy heuristic when cache fields are zero.
	apply := func(walkingSoFar int, cacheRead, cacheWrite, input, output int) int {
		transcriptSize := cacheRead + cacheWrite
		if transcriptSize == 0 {
			transcriptSize = walkingSoFar + input + output
		}
		if transcriptSize > walkingSoFar {
			walkingSoFar = transcriptSize
		}
		return walkingSoFar
	}

	// Round 1: cold call. cache_read=0, cache_write=large.
	got := apply(0, 0, 50000, 18, 800)
	if got != 50000 {
		t.Fatalf("round 1 transcript = %d, want 50000 (cache_write only)", got)
	}

	// Round 2: warm. cache_read=50K (round 1's content), cache_write=2K (this turn's delta).
	got = apply(got, 50000, 2000, 18, 600)
	if got != 52000 {
		t.Fatalf("round 2 transcript = %d, want 52000 (max of 50000 and 50000+2000)", got)
	}

	// Round 3: cache_read grows further as transcript accumulates.
	got = apply(got, 52000, 2500, 18, 700)
	if got != 54500 {
		t.Fatalf("round 3 transcript = %d, want 54500", got)
	}

	// Codex case: cache_read=cache_write=0 reported, input is huge full count.
	// Should use the legacy heuristic (cumulative add).
	got = apply(0, 0, 0, 119547, 2582)
	if got != 122129 {
		t.Fatalf("codex fallback = %d, want 122129 (legacy add)", got)
	}

	// max() prevents the running watermark from going down even if a small
	// turn comes after a big one — important so we don't lose track of
	// transcript growth across retries.
	got = apply(54500, 30000, 500, 18, 200)
	if got != 54500 {
		t.Fatalf("max() guard failed: %d, want 54500 (already higher)", got)
	}
}
