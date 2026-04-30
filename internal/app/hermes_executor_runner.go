package app

import (
	"log"

	appengine "claude-tg-agent/internal/app/engine"
	"claude-tg-agent/internal/app/hermes"
)

// hermesExecutorRunner is the DirectRunner used for Hermes sub-task execution.
// It picks between a light executor model (e.g. Haiku) and a heavy executor
// model (e.g. Sonnet) per sub-task based on engine.IsHeavySubTask, then
// applies the chosen model to the underlying Agent for that one Run call.
//
// Implements appengine.DirectRunner, appengine.DirectRunnerMetrics, and
// appengine.SubTaskBindable. PlanExecuteEngine calls BindSubTask before
// every Run so the runner knows which classification to apply.
type hermesExecutorRunner struct {
	agent              *Agent
	executorModel      string // light tier, used by default
	heavyExecutorModel string // heavy tier, used for Edit/Write/refactor work
	current            hermes.SubTask
	// walkingEnabled keeps the same Claude session alive across consecutive
	// sub-tasks of one task that share a model. When true, ClearSessionForModel
	// is suppressed so prompt cache + transcript continuity persist between
	// sub-tasks. The PlanExecuteEngine drives prompt-format choices (full vs
	// slim) and force-fresh decisions (model change, token watermark exceeded).
	// See issue #149 + docs/arch/hermes-walking-agent.md.
	walkingEnabled bool
	// lastWalkingModel is the model used by the previous Run call within the
	// current task. When the next sub-task picks a different model, the runner
	// must clear that model's session even with walkingEnabled — model
	// boundaries are hard cache-key separators.
	lastWalkingModel string
}

func newHermesExecutorRunner(agent *Agent, executorModel, heavyExecutorModel string) *hermesExecutorRunner {
	return &hermesExecutorRunner{
		agent:              agent,
		executorModel:      executorModel,
		heavyExecutorModel: heavyExecutorModel,
	}
}

// SetWalkingEnabled toggles per-task session reuse. PlanExecuteEngine sets this
// at task start based on HermesConfig.WalkingAgentEnabled. Reset to false at
// task end so subsequent direct (non-Hermes) work doesn't accidentally inherit
// the flag.
func (r *hermesExecutorRunner) SetWalkingEnabled(v bool) {
	r.walkingEnabled = v
	if !v {
		r.lastWalkingModel = ""
	}
}

// BindSubTask records the sub-task that the next Run call will execute.
// Called by PlanExecuteEngine via DirectEngine.BindSubTask.
func (r *hermesExecutorRunner) BindSubTask(st hermes.SubTask) {
	r.current = st
}

// pickModel chooses the executor model for the bound sub-task. Heavy
// executor model wins for sub-tasks classified as substantive code work;
// otherwise the light executor runs.
func (r *hermesExecutorRunner) pickModel() string {
	if r.heavyExecutorModel != "" && r.heavyExecutorModel != r.executorModel && appengine.IsHeavySubTask(r.current) {
		return r.heavyExecutorModel
	}
	return r.executorModel
}

func (r *hermesExecutorRunner) Run(msg string, onUpdate func(string, bool)) (string, error) {
	model := r.pickModel()

	prevOverride := r.agent.currentModelOverride
	if model != "" {
		r.agent.currentModelOverride = model
	}
	// Suppress the general-memory + recent-message bridge for the duration of
	// this Hermes Executor call. The Hermes prompt already carries goal +
	// accumulated + current sub-task; the bridge cards re-inject prior runs'
	// Hermes prompts as "Persisted general work memory", causing prompt bloat
	// to grow exponentially across sub-tasks until codex hits its prompt
	// length limit.
	r.agent.SetSuppressMemoryBridge(true)
	defer func() {
		r.agent.currentModelOverride = prevOverride
		r.agent.SetSuppressMemoryBridge(false)
	}()

	// Session-clear policy depends on walking mode (#149):
	//
	// Legacy (walkingEnabled=false):
	//   Reset CLI session before each sub-task. The engine rebuilds full
	//   context (goal + accumulated + current subtask) into the prompt each
	//   call, so cross-subtask session continuity would balloon the transcript
	//   ("Prompt is too long" mode, gladsheim #108).
	//
	// Walking (walkingEnabled=true):
	//   Keep the session alive across same-model sub-tasks so prompt cache and
	//   conversational continuity persist; the engine emits a slim round-2+
	//   prompt that does NOT re-inject rules/goal/accumulated. Only force a
	//   fresh session when the model boundary is crossed (light <-> heavy),
	//   since cache key includes model fingerprint.
	if r.walkingEnabled {
		if model != "" && r.lastWalkingModel != "" && r.lastWalkingModel != model {
			log.Printf("[hermes.walking] model changed %q -> %q, clearing prior session", r.lastWalkingModel, model)
			r.agent.ClearSessionForModel(r.lastWalkingModel)
		}
		r.lastWalkingModel = model
	} else if model != "" {
		r.agent.ClearSessionForModel(model)
	} else {
		r.agent.ClearSession()
	}
	return r.agent.Run(msg, onUpdate)
}

// LastCallMetrics forwards Agent metrics so DirectEngine can attribute
// Executor tokens + cost to the actual model that ran each sub-task.
func (r *hermesExecutorRunner) LastCallMetrics() (string, int, int, float64) {
	return r.agent.LastCallMetrics()
}
